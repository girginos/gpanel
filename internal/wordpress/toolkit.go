// toolkit.go — WordPress Toolkit: eklenti/tema/kullanıcı yönetimi, parola sıfırlama,
// çekirdek onarım (verify-checksums + reinstall), bakım modu, önbellek, toplu güncelleme.
// Tüm komutlar domain kullanıcısı olarak (wpKomut/wpStdout), yol public_html'e kilitli (cozDizin).
package wordpress

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/httpx"
)

// eklenti/tema slug'ları: arg-injection'ı önlemek için katı doğrulama (başında '-' olamaz).
var reSlug = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,80}$`)

// dizinQP: GET isteğinde ?dizin= parametresini güvenli mutlak yola çevirir.
func (h *Handlers) dizinQP(r *http.Request, sk string) (string, error) {
	d := r.URL.Query().Get("dizin")
	if d == "" {
		d = "/"
	}
	return cozDizin(sk, d)
}

// gonderJSON: wp-cli'nin JSON çıktısını (bir dizi) olduğu gibi ilet; hata/boşsa [] döndür.
func gonderJSON(w http.ResponseWriter, raw []byte, err error) {
	bt := strings.TrimSpace(string(raw))
	if err != nil || bt == "" {
		httpx.WriteJSON(w, http.StatusOK, []any{})
		return
	}
	var arr []json.RawMessage
	if json.Unmarshal([]byte(bt), &arr) != nil {
		httpx.WriteJSON(w, http.StatusOK, []any{})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, arr)
}

// GET /domains/{id}/wordpress/durum?dizin= — çekirdek sürüm/güncelleme + PHP + DB boyutu + bakım
func (h *Handlers) Durum(w http.ResponseWriter, r *http.Request) {
	_, sk, _, _, _, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	dir, err := h.dizinQP(r, sk)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// 5 wp-cli çağrısını PARALEL çalıştır → gecikme = en yavaş tek çağrı (~check-update),
	// toplam ~3s yerine ~1.5s. Her sonuç kendi alanına yazılır (yarış yok).
	out := map[string]any{"surum": "", "guncelleme_var": false, "hedef_surum": "",
		"php": "", "db_mb": "", "bakim": false}
	var wg sync.WaitGroup
	wg.Add(5)
	go func() {
		defer wg.Done()
		if b, e := wpStdout(ctx, sk, "core", "version", "--path="+dir); e == nil {
			out["surum"] = strings.TrimSpace(string(b))
		}
	}()
	go func() {
		defer wg.Done()
		if b, e := wpStdout(ctx, sk, "core", "check-update", "--path="+dir, "--format=json"); e == nil {
			bt := strings.TrimSpace(string(b))
			if bt != "" && bt != "[]" {
				var ups []struct {
					Version string `json:"version"`
				}
				if json.Unmarshal([]byte(bt), &ups) == nil && len(ups) > 0 {
					out["guncelleme_var"] = true
					out["hedef_surum"] = ups[0].Version
				}
			}
		}
	}()
	go func() {
		defer wg.Done()
		if b, e := wpStdout(ctx, sk, "eval", "echo PHP_VERSION;", "--path="+dir); e == nil {
			out["php"] = strings.TrimSpace(string(b))
		}
	}()
	go func() {
		defer wg.Done()
		if b, e := wpStdout(ctx, sk, "db", "size", "--size_format=mb", "--path="+dir); e == nil {
			out["db_mb"] = strings.TrimSpace(string(b))
		}
	}()
	go func() {
		defer wg.Done()
		if b, e := wpStdout(ctx, sk, "maintenance-mode", "status", "--path="+dir); e == nil {
			out["bakim"] = strings.Contains(string(b), "is active")
		}
	}()
	wg.Wait()
	httpx.WriteJSON(w, http.StatusOK, out)
}

// GET /domains/{id}/wordpress/eklentiler?dizin=
func (h *Handlers) Eklentiler(w http.ResponseWriter, r *http.Request) {
	_, sk, _, _, _, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	dir, err := h.dizinQP(r, sk)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	b, e := wpStdout(ctx, sk, "plugin", "list", "--path="+dir, "--format=json",
		"--fields=name,status,version,update,update_version")
	gonderJSON(w, b, e)
}

// GET /domains/{id}/wordpress/temalar?dizin=
func (h *Handlers) Temalar(w http.ResponseWriter, r *http.Request) {
	_, sk, _, _, _, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	dir, err := h.dizinQP(r, sk)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	b, e := wpStdout(ctx, sk, "theme", "list", "--path="+dir, "--format=json",
		"--fields=name,status,version,update,update_version")
	gonderJSON(w, b, e)
}

// GET /domains/{id}/wordpress/kullanicilar?dizin=
func (h *Handlers) Kullanicilar(w http.ResponseWriter, r *http.Request) {
	_, sk, _, _, _, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	dir, err := h.dizinQP(r, sk)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	b, e := wpStdout(ctx, sk, "user", "list", "--path="+dir, "--format=json",
		"--fields=ID,user_login,user_email,display_name,roles")
	gonderJSON(w, b, e)
}

// demoRet: mutasyon işlemlerinde ortak domain+demo+dizin çözümü. ok=false ise yanıt yazılmıştır.
func (h *Handlers) mutasyonHazir(w http.ResponseWriter, r *http.Request, dizin string) (sk, dir string, ok bool) {
	_, sk, _, _, demo, dok := h.domain(r)
	if !dok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return "", "", false
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde kullanılamaz")
		return "", "", false
	}
	d, err := cozDizin(sk, dizin)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return "", "", false
	}
	return sk, d, true
}

// POST /domains/{id}/wordpress/eklenti  {dizin, islem: guncelle|tumunu-guncelle|aktif|pasif, ad}
func (h *Handlers) EklentiIslem(w http.ResponseWriter, r *http.Request) {
	var req struct{ Dizin, Islem, Ad string }
	_ = json.NewDecoder(r.Body).Decode(&req)
	sk, dir, ok := h.mutasyonHazir(w, r, req.Dizin)
	if !ok {
		return
	}
	h.paketIslem(w, sk, dir, "plugin", req.Islem, req.Ad)
}

// POST /domains/{id}/wordpress/tema  {dizin, islem: guncelle|tumunu-guncelle|aktif, ad}
func (h *Handlers) TemaIslem(w http.ResponseWriter, r *http.Request) {
	var req struct{ Dizin, Islem, Ad string }
	_ = json.NewDecoder(r.Body).Decode(&req)
	sk, dir, ok := h.mutasyonHazir(w, r, req.Dizin)
	if !ok {
		return
	}
	h.paketIslem(w, sk, dir, "theme", req.Islem, req.Ad)
}

// paketIslem: plugin/theme için ortak güncelle/etkinleştir/devre-dışı yürütücü.
func (h *Handlers) paketIslem(w http.ResponseWriter, sk, dir, tur, islem, ad string) {
	var args []string
	switch islem {
	case "tumunu-guncelle":
		args = []string{tur, "update", "--all", "--path=" + dir}
	case "guncelle":
		if !reSlug.MatchString(ad) {
			httpx.WriteError(w, http.StatusBadRequest, "geçersiz ad")
			return
		}
		args = []string{tur, "update", ad, "--path=" + dir}
	case "aktif":
		if !reSlug.MatchString(ad) {
			httpx.WriteError(w, http.StatusBadRequest, "geçersiz ad")
			return
		}
		args = []string{tur, "activate", ad, "--path=" + dir}
	case "pasif":
		if tur != "plugin" || !reSlug.MatchString(ad) {
			httpx.WriteError(w, http.StatusBadRequest, "geçersiz işlem")
			return
		}
		args = []string{tur, "deactivate", ad, "--path=" + dir}
	default:
		httpx.WriteError(w, http.StatusBadRequest, "bilinmeyen işlem")
		return
	}
	out, err := wpKomut(sk, args...)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, kisalt(string(out)))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "cikti": kisalt(string(out))})
}

// POST /domains/{id}/wordpress/kullanici-parola  {dizin, user_id, parola?}
// parola boşsa güvenli bir parola üretir ve döndürür.
func (h *Handlers) KullaniciParola(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dizin  string `json:"dizin"`
		UserID int    `json:"user_id"`
		Parola string `json:"parola"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	sk, dir, ok := h.mutasyonHazir(w, r, req.Dizin)
	if !ok {
		return
	}
	if req.UserID <= 0 {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz kullanıcı")
		return
	}
	parola := strings.TrimSpace(req.Parola)
	if parola == "" {
		parola = randParola()
	} else if len(parola) < 8 || len(parola) > 100 {
		httpx.WriteError(w, http.StatusBadRequest, "parola 8-100 karakter olmalı")
		return
	}
	out, err := wpKomut(sk, "user", "update", strconv.Itoa(req.UserID),
		"--user_pass="+parola, "--skip-email", "--path="+dir)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, kisalt(string(out)))
		return
	}
	// kullanıcı adını da döndür (UI'da göstermek için)
	login := ""
	if b, e := wpKomut(sk, "user", "get", strconv.Itoa(req.UserID), "--field=user_login", "--path="+dir); e == nil {
		login = strings.TrimSpace(string(b))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "parola": parola, "kullanici": login})
}

// POST /domains/{id}/wordpress/onar  {dizin}
// Çekirdek bütünlüğünü doğrular (verify-checksums), çekirdek dosyalarını yeniden indirir
// (wp-content'e dokunmadan), DB'yi günceller, tekrar doğrular.
func (h *Handlers) Onar(w http.ResponseWriter, r *http.Request) {
	var req struct{ Dizin string }
	_ = json.NewDecoder(r.Body).Decode(&req)
	sk, dir, ok := h.mutasyonHazir(w, r, req.Dizin)
	if !ok {
		return
	}
	onceOut, onceErr := wpKomut(sk, "core", "verify-checksums", "--path="+dir)
	once := "temiz"
	if onceErr != nil {
		once = "sorun-var"
	}
	// mevcut sürümü öğren → aynı sürümü yeniden indir (istenmeyen yükseltme olmasın)
	surum := ""
	if b, e := wpKomut(sk, "core", "version", "--path="+dir); e == nil {
		surum = strings.TrimSpace(string(b))
	}
	dlArgs := []string{"core", "download", "--force", "--skip-content", "--path=" + dir}
	if surum != "" {
		dlArgs = append(dlArgs, "--version="+surum)
	}
	// çekirdeği yeniden indir (içerik/eklenti/tema korunur), DB güncelle
	dlOut, dlErr := wpKomut(sk, dlArgs...)
	if dlErr != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "çekirdek indirilemedi: "+kisalt(string(dlOut)))
		return
	}
	_, _ = wpKomut(sk, "core", "update-db", "--path="+dir)
	sonraOut, sonraErr := wpKomut(sk, "core", "verify-checksums", "--path="+dir)
	sonra := "temiz"
	if sonraErr != nil {
		sonra = "sorun-var"
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "once": once, "sonra": sonra,
		"cikti": kisalt(strings.TrimSpace(string(onceOut)) + "\n---\n" + strings.TrimSpace(string(sonraOut))),
	})
}

// POST /domains/{id}/wordpress/arac  {dizin, islem: bakim-ac|bakim-kapat|cache-temizle|tumunu-guncelle}
func (h *Handlers) AracIslem(w http.ResponseWriter, r *http.Request) {
	var req struct{ Dizin, Islem string }
	_ = json.NewDecoder(r.Body).Decode(&req)
	sk, dir, ok := h.mutasyonHazir(w, r, req.Dizin)
	if !ok {
		return
	}
	var out []byte
	var err error
	switch req.Islem {
	case "bakim-ac":
		out, err = wpKomut(sk, "maintenance-mode", "activate", "--path="+dir)
	case "bakim-kapat":
		out, err = wpKomut(sk, "maintenance-mode", "deactivate", "--path="+dir)
	case "cache-temizle":
		out, err = wpKomut(sk, "cache", "flush", "--path="+dir)
	case "tumunu-guncelle":
		var b strings.Builder
		o1, e1 := wpKomut(sk, "core", "update", "--path="+dir)
		b.Write(o1)
		o2, _ := wpKomut(sk, "plugin", "update", "--all", "--path="+dir)
		b.WriteString("\n")
		b.Write(o2)
		o3, _ := wpKomut(sk, "theme", "update", "--all", "--path="+dir)
		b.WriteString("\n")
		b.Write(o3)
		_, _ = wpKomut(sk, "core", "update-db", "--path="+dir)
		out, err = []byte(b.String()), e1
	default:
		httpx.WriteError(w, http.StatusBadRequest, "bilinmeyen işlem")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, kisalt(string(out)))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "cikti": kisalt(string(out))})
}

// kisalt: uzun wp-cli çıktısını son 600 karaktere kırpar (hata mesajı için yeterli).
func kisalt(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 600 {
		s = "…" + s[len(s)-600:]
	}
	return s
}
