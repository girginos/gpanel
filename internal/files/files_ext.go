package files

// files_ext.go — Yaz/Rename/Chmod + symlink-aware jail
// (jailJoin orijinali files.go'da; bu dosya ek handler'lar + sıkılastirma)

import (
	"io"
	"os/exec"
	"fmt"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"girginospanel/internal/httpx"
)

// jailJoinStrict: symlink-aware. Parent dizini EvalSymlinks ile resolve eder,
// sonra leaf'i join eder. Symlink ile dis-cikis engellenir.
func jailJoinStrict(home, rel string) (string, error) {
	rel = filepath.Clean("/" + rel)
	wanted := filepath.Clean(filepath.Join(home, rel))

	// homeResolved
	homeResolved, err := filepath.EvalSymlinks(home)
	if err != nil {
		homeResolved = home
	}

	// wanted'in resolve edilebilir kismini bul
	test := wanted
	for {
		if r, err := filepath.EvalSymlinks(test); err == nil {
			// test bulundu, kalan path'i ekle ve kontrol et
			rest := strings.TrimPrefix(wanted, test)
			full := filepath.Clean(filepath.Join(r, rest))
			if full == homeResolved || strings.HasPrefix(full, homeResolved+string(filepath.Separator)) {
				return full, nil
			}
			return "", errEscape
		}
		// yoksa parent'a cik
		parent := filepath.Dir(test)
		if parent == test {
			// kök, devam edemez
			break
		}
		test = parent
	}
	// hicbir ata resolve olmadi (cok nadir); plain check
	if wanted == homeResolved || strings.HasPrefix(wanted, homeResolved+string(filepath.Separator)) {
		return wanted, nil
	}
	return "", errEscape
}

// ----- Yaz (editor save) -----

type yazReq struct {
	Yol    string `json:"yol"`
	Icerik string `json:"icerik"`
}

func (h *Handlers) Yaz(w http.ResponseWriter, r *http.Request) {
	home, sk, err := h.home(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	var req yazReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	abs, err := jailJoinStrict(home, req.Yol)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Icerik) > 5*1024*1024 {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "5 MB üstü editor ile kaydedilemez")
		return
	}
	// Var olan dosyanın izinleri korunsun (varsa)
	mode := os.FileMode(0644)
	if info, err := os.Stat(abs); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(abs, []byte(req.Icerik), mode); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "yazma: "+err.Error())
		return
	}
	chown(abs, sk)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"yol":   req.Yol,
		"boyut": len(req.Icerik),
	})
}

// ----- Rename / Move -----

type renameReq struct {
	Eski string `json:"eski"`
	Yeni string `json:"yeni"`
}

func (h *Handlers) Rename(w http.ResponseWriter, r *http.Request) {
	home, sk, err := h.home(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	var req renameReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	eski, err := jailJoinStrict(home, req.Eski)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "kaynak: "+err.Error())
		return
	}
	yeni, err := jailJoinStrict(home, req.Yeni)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "hedef: "+err.Error())
		return
	}
	if eski == home || yeni == home {
		httpx.WriteError(w, http.StatusBadRequest, "ana ev dizini taşınamaz")
		return
	}
	if _, err := os.Stat(eski); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "kaynak yok")
		return
	}
	// hedef dizini garanti et
	_ = os.MkdirAll(filepath.Dir(yeni), 0755)
	chown(filepath.Dir(yeni), sk)
	if err := os.Rename(eski, yeni); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "rename: "+err.Error())
		return
	}
	chown(yeni, sk)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "eski": req.Eski, "yeni": req.Yeni})
}

// ----- Chmod -----

type chmodReq struct {
	Yol string `json:"yol"`
	Mod string `json:"mod"` // "0644" gibi octal string
}

func (h *Handlers) Chmod(w http.ResponseWriter, r *http.Request) {
	home, _, err := h.home(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	var req chmodReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	abs, err := jailJoinStrict(home, req.Yol)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	mod := strings.TrimPrefix(req.Mod, "0")
	n, err := strconv.ParseUint(mod, 8, 32)
	if err != nil || n > 0o777 {
		httpx.WriteError(w, http.StatusBadRequest, "mod oktal olmalı (0000-0777)")
		return
	}
	if err := os.Chmod(abs, os.FileMode(n)); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "chmod: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "yol": req.Yol, "mod": req.Mod})
}

var _ = errors.New // keep import

// ----- Extract (ZIP / TAR / TAR.GZ aç) -----

type extractReq struct {
	Yol    string `json:"yol"`     // arşivin yolu
	Hedef  string `json:"hedef"`   // çıkarılacak dizin (opsiyonel; boşsa arşivin dizini)
}

func (h *Handlers) Extract(w http.ResponseWriter, r *http.Request) {
	home, sk, err := h.home(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	var req extractReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	abs, err := jailJoinStrict(home, req.Yol)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		httpx.WriteError(w, http.StatusBadRequest, "dosya bulunamadı veya klasör")
		return
	}

	hedef := req.Hedef
	if hedef == "" {
		hedef = filepath.Dir(req.Yol)
	}
	hedefAbs, err := jailJoinStrict(home, hedef)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "hedef: "+err.Error())
		return
	}
	if err := os.MkdirAll(hedefAbs, 0755); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "mkdir hedef: "+err.Error())
		return
	}

	low := strings.ToLower(abs)
	var cmd *exec.Cmd
	switch {
	case strings.HasSuffix(low, ".zip"):
		cmd = exec.Command("unzip", "-o", "-q", abs, "-d", hedefAbs)
	case strings.HasSuffix(low, ".tar.gz") || strings.HasSuffix(low, ".tgz"):
		cmd = exec.Command("tar", "-xzf", abs, "-C", hedefAbs)
	case strings.HasSuffix(low, ".tar.bz2") || strings.HasSuffix(low, ".tbz2"):
		cmd = exec.Command("tar", "-xjf", abs, "-C", hedefAbs)
	case strings.HasSuffix(low, ".tar.xz") || strings.HasSuffix(low, ".txz"):
		cmd = exec.Command("tar", "-xJf", abs, "-C", hedefAbs)
	case strings.HasSuffix(low, ".tar"):
		cmd = exec.Command("tar", "-xf", abs, "-C", hedefAbs)
	case strings.HasSuffix(low, ".gz"):
		// tek dosya gunzip
		cmd = exec.Command("bash", "-lc", fmt.Sprintf("gunzip -k -c %q > %q", abs, filepath.Join(hedefAbs, strings.TrimSuffix(filepath.Base(abs), ".gz"))))
	default:
		httpx.WriteError(w, http.StatusBadRequest, "desteklenmeyen format (zip, tar, tar.gz/tgz, tar.bz2, tar.xz, gz)")
		return
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "extract: "+strings.TrimSpace(string(out)))
		return
	}

	// İzole ortam: çıkartılan tüm dosyaları domain user'ına chown
	_, _ = exec.Command("chown", "-R", sk+":"+sk, hedefAbs).CombinedOutput()
	_, _ = exec.Command("restorecon", "-R", hedefAbs).CombinedOutput()

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"yol":   req.Yol,
		"hedef": hedef,
	})
}

// ----- Copy / Move (toplu) -----

type bulkMoveCopyReq struct {
	Kaynaklar []string `json:"kaynaklar"`
	Hedef     string   `json:"hedef"` // hedef KLASÖR (içine konulacak)
}

func (h *Handlers) Copy(w http.ResponseWriter, r *http.Request) {
	h.bulkMoveCopy(w, r, false)
}

func (h *Handlers) Move(w http.ResponseWriter, r *http.Request) {
	h.bulkMoveCopy(w, r, true)
}

func (h *Handlers) bulkMoveCopy(w http.ResponseWriter, r *http.Request, move bool) {
	home, sk, err := h.home(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	var req bulkMoveCopyReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	hedefAbs, err := jailJoinStrict(home, req.Hedef)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "hedef: "+err.Error())
		return
	}
	info, err := os.Stat(hedefAbs)
	if err != nil || !info.IsDir() {
		httpx.WriteError(w, http.StatusBadRequest, "hedef klasör değil")
		return
	}

	basarili := 0
	hatalar := []string{}
	for _, k := range req.Kaynaklar {
		kAbs, err := jailJoinStrict(home, k)
		if err != nil {
			hatalar = append(hatalar, k+": "+err.Error())
			continue
		}
		dst := filepath.Join(hedefAbs, filepath.Base(kAbs))
		if dst == kAbs {
			hatalar = append(hatalar, k+": kaynak ve hedef aynı")
			continue
		}
		var op error
		if move {
			op = os.Rename(kAbs, dst)
		} else {
			op = copyAny(kAbs, dst)
		}
		if op != nil {
			hatalar = append(hatalar, k+": "+op.Error())
			continue
		}
		_, _ = exec.Command("chown", "-R", sk+":"+sk, dst).CombinedOutput()
		basarili++
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": len(hatalar) == 0, "basarili": basarili, "hatalar": hatalar,
	})
}

func copyAny(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(src, dst)
	}
	return copyFile(src, dst, info.Mode().Perm())
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func copyDir(src, dst string) error {
	si, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, si.Mode().Perm()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		if err := copyAny(s, d); err != nil {
			return err
		}
	}
	return nil
}

// ----- Arşivle (seçili dosyaları zip yap) -----

type archiveReq struct {
	Kaynaklar []string `json:"kaynaklar"`
	CiktiYol  string   `json:"cikti_yol"` // örn /public_html/yedek.zip
	Format    string   `json:"format"`    // zip | tar.gz
}

func (h *Handlers) Archive(w http.ResponseWriter, r *http.Request) {
	home, sk, err := h.home(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	var req archiveReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	if len(req.Kaynaklar) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "kaynak yok")
		return
	}
	if req.Format == "" {
		req.Format = "zip"
	}
	ciktiAbs, err := jailJoinStrict(home, req.CiktiYol)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "cikti: "+err.Error())
		return
	}
	_ = os.MkdirAll(filepath.Dir(ciktiAbs), 0755)

	// Tum kaynaklarin home-altinda abs yolunu hazirla, relative isimlerle arşivle
	// Stratejisi: ortak parent'i bul, oradan çalış
	var args []string
	if req.Format == "zip" {
		args = []string{"-r", "-q", ciktiAbs}
		for _, k := range req.Kaynaklar {
			kAbs, err := jailJoinStrict(home, k)
			if err != nil {
				continue
			}
			// chdir + relative isim
			args = append(args, kAbs)
		}
		out, err := exec.Command("zip", args...).CombinedOutput()
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "zip: "+strings.TrimSpace(string(out)))
			return
		}
	} else { // tar.gz
		args = []string{"-czf", ciktiAbs}
		for _, k := range req.Kaynaklar {
			kAbs, err := jailJoinStrict(home, k)
			if err != nil {
				continue
			}
			args = append(args, kAbs)
		}
		out, err := exec.Command("tar", args...).CombinedOutput()
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "tar: "+strings.TrimSpace(string(out)))
			return
		}
	}
	_, _ = exec.Command("chown", sk+":"+sk, ciktiAbs).CombinedOutput()
	_, _ = exec.Command("restorecon", ciktiAbs).CombinedOutput()

	info, _ := os.Stat(ciktiAbs)
	var boyut int64
	if info != nil {
		boyut = info.Size()
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "cikti_yol": req.CiktiYol, "boyut": boyut,
	})
}

// ----- Yeni boş dosya -----

type yeniDosyaReq struct {
	Yol string `json:"yol"`
}

func (h *Handlers) YeniDosya(w http.ResponseWriter, r *http.Request) {
	home, sk, err := h.home(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	var req yeniDosyaReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	abs, err := jailJoinStrict(home, req.Yol)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := os.Stat(abs); err == nil {
		httpx.WriteError(w, http.StatusConflict, "dosya zaten var")
		return
	}
	f, err := os.OpenFile(abs, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0644)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "yazma: "+err.Error())
		return
	}
	f.Close()
	chown(abs, sk)
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"ok": true, "yol": req.Yol})
}

// ----- Boyut hesapla (du -sb) -----

func (h *Handlers) BoyutHesapla(w http.ResponseWriter, r *http.Request) {
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
	out, err := exec.Command("du", "-sb", abs).CombinedOutput()
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "du: "+strings.TrimSpace(string(out)))
		return
	}
	parts := strings.Fields(string(out))
	if len(parts) < 1 {
		httpx.WriteError(w, http.StatusInternalServerError, "du çıktı parse edilemedi")
		return
	}
	var b int64
	for _, c := range parts[0] {
		if c < '0' || c > '9' {
			break
		}
		b = b*10 + int64(c-'0')
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"yol":     rel,
		"boyut_b": b,
	})
}

// ----- Arama (recursive find by name pattern) -----

func (h *Handlers) Ara(w http.ResponseWriter, r *http.Request) {
	home, _, err := h.home(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"icerik": []any{}, "toplam": 0})
		return
	}
	rel := r.URL.Query().Get("yol")
	if rel == "" {
		rel = "/"
	}
	baseAbs, err := jailJoinStrict(home, rel)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Güvenlik: q sadece dosya adı pattern, shell injection olmaması için iname kullan
	q = strings.ReplaceAll(q, "*", "")
	q = strings.ReplaceAll(q, "?", "")
	pattern := "*" + q + "*"

	out, _ := exec.Command("find", baseAbs, "-iname", pattern, "-printf", "%p\t%s\t%y\t%T@\n").Output()
	results := []Entry{}
	for _, ln := range strings.Split(string(out), "\n") {
		if ln == "" {
			continue
		}
		parts := strings.SplitN(ln, "\t", 4)
		if len(parts) < 4 {
			continue
		}
		absp := parts[0]
		size := int64(0)
		for _, c := range parts[1] {
			if c < '0' || c > '9' {
				break
			}
			size = size*10 + int64(c-'0')
		}
		tip := "dosya"
		if parts[2] == "d" {
			tip = "klasor"
		} else if parts[2] == "l" {
			tip = "sembolik"
		}
		// rel yol home altina goreceli
		yolRel := strings.TrimPrefix(absp, home)
		if yolRel == "" {
			yolRel = "/"
		}
		info, _ := os.Stat(absp)
		mod := "0644"
		var degisme string
		if info != nil {
			mod = "0" + strconv.FormatInt(int64(info.Mode().Perm()), 8)
			degisme = info.ModTime().UTC().Format("2006-01-02T15:04:05Z")
		}
		results = append(results, Entry{
			Adi: filepath.Base(absp), Yol: filepath.ToSlash(yolRel),
			Tip: tip, BoyutB: size, Mod: mod, Degisme: degisme,
		})
		if len(results) >= 500 {
			break
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"icerik": results, "toplam": len(results), "q": q,
	})
}

