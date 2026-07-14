// Package sitekopya: sitenin dosyalarını aynı ev dizininde zaman-damgalı bir
// staging kopyaya klonlar (public_html → ~/kopyalar/kopya_<ts>/). Değişiklik
// öncesi anlık-görüntü almak için. Sadece DOSYALAR (veritabanı dahil değil).
// Güvenlik: aynı ev dizini, cross-user yok, bind-mount yok, fuser YOK;
// silme yalnız /home/c_*/kopyalar/ altında + isim regex + prefix guard.
package sitekopya

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

type Handlers struct{ DB *sql.DB }

const maxKopyaBayt = 3 * 1024 * 1024 * 1024 // 3 GB üstü → Yedekler'e yönlendir

var reKopyaAd = regexp.MustCompile(`^kopya_[0-9]{8}_[0-9]{6}$`)

type Kopya struct {
	Ad      string `json:"ad"`
	BoyutMB int64  `json:"boyut_mb"`
	Tarih   string `json:"tarih"`
}

func (h *Handlers) domain(r *http.Request) (id int64, sk string, demo, ok bool) {
	id, _ = strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var isDemo int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici, COALESCE(is_demo,0) FROM domains WHERE id=?`, id).Scan(&sk, &isDemo); err != nil {
		return id, "", false, false
	}
	return id, sk, isDemo == 1, true
}

// GET /domains/{id}/kopya
func (h *Handlers) Liste(w http.ResponseWriter, r *http.Request) {
	_, sk, _, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	dir := "/home/" + sk + "/kopyalar"
	out := []Kopya{}
	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() || !reKopyaAd.MatchString(e.Name()) {
				continue
			}
			k := Kopya{Ad: e.Name()}
			if fi, err := e.Info(); err == nil {
				k.Tarih = fi.ModTime().Format("2006-01-02 15:04")
			}
			k.BoyutMB = dirBoyutMB(filepath.Join(dir, e.Name()))
			out = append(out, k)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ad > out[j].Ad }) // en yeni üstte
	httpx.WriteJSON(w, http.StatusOK, out)
}

// POST /domains/{id}/kopya  → yeni staging kopya oluştur
func (h *Handlers) Olustur(w http.ResponseWriter, r *http.Request) {
	_, sk, demo, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde kullanılamaz")
		return
	}
	if !strings.HasPrefix(sk, "c_") {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz kullanıcı")
		return
	}
	home := "/home/" + sk
	kaynak := home + "/public_html"
	if _, err := os.Stat(kaynak); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "public_html bulunamadı")
		return
	}
	if b := dirBoyutBayt(kaynak); b > maxKopyaBayt {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "site 3 GB'den büyük — lütfen Yedekler aracını kullanın")
		return
	}
	kopyaDir := home + "/kopyalar"
	if err := os.MkdirAll(kopyaDir, 0o711); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "kopya dizini oluşturulamadı")
		return
	}
	_ = exec.Command("chown", sk+":"+sk, kopyaDir).Run() // kopyalar dizini de müşteriye ait olsun
	ad := "kopya_" + time.Now().Format("20060102_150405")
	hedef := kopyaDir + "/" + ad + "/public_html"
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	if err := os.MkdirAll(hedef, 0o755); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "hedef oluşturulamadı")
		return
	}
	// rsync (trailing slash: içeriği kopyala). --delete YOK (yıkıcı değil).
	if out, err := exec.CommandContext(ctx, "rsync", "-a", "--no-owner", "--no-group", kaynak+"/", hedef+"/").CombinedOutput(); err != nil {
		_ = os.RemoveAll(kopyaDir + "/" + ad)
		httpx.WriteError(w, http.StatusInternalServerError, "kopyalama başarısız: "+strings.TrimSpace(string(out)))
		return
	}
	// Sahiplik domain kullanıcısına
	_ = exec.Command("chown", "-R", sk+":"+sk, kopyaDir+"/"+ad).Run()
	_ = exec.Command("restorecon", "-R", kopyaDir+"/"+ad).Run()
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "ad": ad, "boyut_mb": dirBoyutMB(kopyaDir + "/" + ad)})
}

// DELETE /domains/{id}/kopya/{ad}
func (h *Handlers) Sil(w http.ResponseWriter, r *http.Request) {
	_, sk, _, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if !strings.HasPrefix(sk, "c_") {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz kullanıcı")
		return
	}
	ad := chi.URLParam(r, "ad")
	if !reKopyaAd.MatchString(ad) { // katı isim doğrulama → path traversal imkânsız
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz kopya adı")
		return
	}
	kopyaDir := "/home/" + sk + "/kopyalar"
	hedef := filepath.Join(kopyaDir, ad)
	// Çok katmanlı guard: prefix + Clean sonrası hâlâ kopyalar/ altında + dizin mi
	clean := filepath.Clean(hedef)
	if !strings.HasPrefix(clean, kopyaDir+"/") {
		httpx.WriteError(w, http.StatusBadRequest, "yol dışında")
		return
	}
	fi, err := os.Stat(clean)
	if err != nil || !fi.IsDir() {
		httpx.WriteError(w, http.StatusNotFound, "kopya bulunamadı")
		return
	}
	if err := os.RemoveAll(clean); err != nil { // normal dizin, bind-mount değil; fuser YOK
		httpx.WriteError(w, http.StatusInternalServerError, "silinemedi")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func dirBoyutBayt(p string) int64 {
	out, err := exec.Command("du", "-sb", p).Output()
	if err != nil {
		return 0
	}
	f := strings.Fields(string(out))
	if len(f) == 0 {
		return 0
	}
	n, _ := strconv.ParseInt(f[0], 10, 64)
	return n
}

func dirBoyutMB(p string) int64 {
	return dirBoyutBayt(p) / (1024 * 1024)
}
