// Package files: domain ev dizinine chroot edilmis dosya yoneticisi API'si
package files

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

const MaxUploadBytes = 10 * 1024 * 1024 * 1024 // 100 MB

type Handlers struct {
	DB *sql.DB
}

// home: domain id -> /home/c_<user>
func (h *Handlers) home(r *http.Request) (string, string, error) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var sk string
	var isDemo int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici, is_demo FROM domains WHERE id=?`, id).
		Scan(&sk, &isDemo)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", os.ErrNotExist
	}
	if err != nil {
		return "", "", err
	}
	if isDemo == 1 {
		return "", "", errDemo
	}
	if !strings.HasPrefix(sk, "c_") {
		return "", "", errBadUser
	}
	return "/home/" + sk, sk, nil
}

var (
	errDemo    = errors.New("demo aboneliğin dosyaları yönetilemez")
	errBadUser = errors.New("güvenlik: geçersiz sistem kullanıcısı")
	errEscape  = errors.New("güvenlik: ev dizini dışına çıkış engellendi")
)


type Entry struct {
	Adi     string `json:"adi"`
	Yol     string `json:"yol"`     // home'a goreceli (panel UI icin)
	Tip     string `json:"tip"`     // "klasor" | "dosya" | "sembolik"
	BoyutB  int64  `json:"boyut_b"`
	Mod     string `json:"mod"`     // "0644"
	Degisme string `json:"degisme"` // RFC3339
}

func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	home, _, err := h.home(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	rel := r.URL.Query().Get("yol")
	if rel == "" {
		rel = "/"
	}
	abs, err := jailJoinStrict(home, rel)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	dir, err := os.ReadDir(abs)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "okuma: "+err.Error())
		return
	}
	out := make([]Entry, 0, len(dir))
	for _, e := range dir {
		info, err := e.Info()
		if err != nil {
			continue
		}
		tip := "dosya"
		if info.IsDir() {
			tip = "klasor"
		} else if info.Mode()&os.ModeSymlink != 0 {
			tip = "sembolik"
		}
		out = append(out, Entry{
			Adi:     e.Name(),
			Yol:     filepath.ToSlash(filepath.Join(rel, e.Name())),
			Tip:     tip,
			BoyutB:  info.Size(),
			Mod:     "0" + strconv.FormatInt(int64(info.Mode().Perm()), 8),
			Degisme: info.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
		})
	}
	// klasörler önce, sonra alfabetik
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].Tip == "klasor") != (out[j].Tip == "klasor") {
			return out[i].Tip == "klasor"
		}
		return strings.ToLower(out[i].Adi) < strings.ToLower(out[j].Adi)
	})

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"yol":      filepath.ToSlash(rel),
		"icerik":   out,
		"toplam":   len(out),
	})
}

// Dosya icerigini ham olarak donder (download)
func (h *Handlers) Download(w http.ResponseWriter, r *http.Request) {
	home, _, err := h.home(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	rel := r.URL.Query().Get("yol")
	abs, err := jailJoinStrict(home, rel)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "bulunamadı")
		return
	}
	if info.IsDir() {
		httpx.WriteError(w, http.StatusBadRequest, "klasör indirilemez")
		return
	}
	f, err := os.Open(abs)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "açılamadı: "+err.Error())
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+info.Name()+"\"")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	_, _ = io.Copy(w, f)
}

// Metin dosyasini okuma (editor icin)
func (h *Handlers) Read(w http.ResponseWriter, r *http.Request) {
	home, _, err := h.home(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	rel := r.URL.Query().Get("yol")
	abs, err := jailJoinStrict(home, rel)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "bulunamadı")
		return
	}
	if info.Size() > 2*1024*1024 {
		httpx.WriteError(w, http.StatusBadRequest, "dosya 2 MB'tan büyük; düzenleme için uygun değil")
		return
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"yol":    rel,
		"icerik": string(data),
		"boyut":  info.Size(),
	})
}

type mkdirReq struct {
	Yol string `json:"yol"`
}

func (h *Handlers) Mkdir(w http.ResponseWriter, r *http.Request) {
	home, sk, err := h.home(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	var req mkdirReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	abs, err := jailJoinStrict(home, req.Yol)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := os.MkdirAll(abs, 0755); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "mkdir: "+err.Error())
		return
	}
	chown(abs, sk)
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"ok": true, "yol": req.Yol})
}

func (h *Handlers) Delete(w http.ResponseWriter, r *http.Request) {
	home, _, err := h.home(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	rel := r.URL.Query().Get("yol")
	abs, err := jailJoinStrict(home, rel)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if abs == home {
		httpx.WriteError(w, http.StatusBadRequest, "ana ev dizini silinemez")
		return
	}
	if err := os.RemoveAll(abs); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "silme: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "silinen": rel})
}

func (h *Handlers) Upload(w http.ResponseWriter, r *http.Request) {
	home, sk, err := h.home(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	rel := r.URL.Query().Get("yol")
	if rel == "" {
		rel = "/"
	}
	if err := r.ParseMultipartForm(MaxUploadBytes); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "form parse: "+err.Error())
		return
	}
	file, fh, err := r.FormFile("dosya")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "dosya alanı bulunamadı: "+err.Error())
		return
	}
	defer file.Close()
	if fh.Size > MaxUploadBytes {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "dosya çok büyük (max 100 MB)")
		return
	}
	abs, err := jailJoinStrict(home, filepath.Join(rel, fh.Filename))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	out, err := os.Create(abs)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "yazma: "+err.Error())
		return
	}
	defer out.Close()
	written, err := io.Copy(out, file)
	if err != nil {
		_ = os.Remove(abs)
		httpx.WriteError(w, http.StatusInternalServerError, "kopya: "+err.Error())
		return
	}
	chown(abs, sk)
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"ok":     true,
		"yol":    filepath.ToSlash(filepath.Join(rel, fh.Filename)),
		"boyut":  written,
		"isim":   fh.Filename,
	})
}

func statusFromErr(err error) int {
	switch err {
	case os.ErrNotExist:
		return http.StatusNotFound
	case errDemo:
		return http.StatusForbidden
	case errBadUser, errEscape:
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

// chown helper — user adıyla
func chown(path, sistemKullanici string) {
	uu, err := userLookup(sistemKullanici)
	if err != nil {
		return
	}
	_ = osChown(path, uu.UID, uu.GID)
}
