package laravel

import (
	"encoding/json"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"girginospanel/internal/httpx"
)

// laravelKokAdaylari: public_html + 1 seviye alt dizinlerde 'artisan' içeren (yani
// Laravel projesi kökü olan) yolları döndürür (home'a göre relative).
func laravelKokAdaylari(sk string) []string {
	home := "/home/" + sk
	base := filepath.Join(home, "public_html")
	out := []string{}
	ekle := func(rel string) {
		for _, v := range out {
			if v == rel {
				return
			}
		}
		out = append(out, rel)
	}
	// 1) public_html'in kendisi
	if _, err := os.Stat(filepath.Join(base, "artisan")); err == nil {
		ekle("public_html")
	}
	// 2) public_html altinda 1 seviye
	if ents, err := os.ReadDir(base); err == nil {
		for _, e := range ents {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			if _, err := os.Stat(filepath.Join(base, e.Name(), "artisan")); err == nil {
				ekle("public_html/" + e.Name())
			}
		}
	}
	// 3) 🔴 HOME ALTINDA (public_html'in KARDESI) — Laravel/Symfony'de uygulama kodu
	// belge kokunun DISINDA durur (public_html/index.php -> ../laravel_11/vendor/...).
	// Eskiden yalniz public_html taraniyordu; bu yuzden TASINMIS Laravel siteleri
	// "kurulu degil" gorunup Toolkit yonetim ekrani yerine "kur" ekrani aciliyordu.
	if ents, err := os.ReadDir(home); err == nil {
		for _, e := range ents {
			ad := e.Name()
			if !e.IsDir() || strings.HasPrefix(ad, ".") || ad == "public_html" ||
				ad == "logs" || ad == "tmp" || ad == "ssl" {
				continue
			}
			if _, err := os.Stat(filepath.Join(home, ad, "artisan")); err == nil {
				ekle(ad)
			}
		}
	}
	return out
}

// GET /domains/{id}/laravel/app-adaylar — mevcut app_root + algılanan Laravel kökleri
//
// 🔴 OTOMATIK ALGILAMA: kayitli app_root'ta artisan YOKSA ama sunucuda bir Laravel
// koku bulunduysa, onu otomatik sahiplenir. Boylece TASINMIS Laravel siteleri
// (uygulama kodu belge kokunun disinda) kullaniciya "kur" ekrani yerine dogrudan
// YONETIM ekranini acar — elle "app_root" girmeye gerek kalmaz.
func (h *Handlers) AppAdaylar(w http.ResponseWriter, r *http.Request) {
	id, sk, _, _, ok := h.lookup(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	k := h.getKayit(r.Context(), id)
	mevcut := k.AppRoot
	if mevcut == "" {
		mevcut = "public_html"
	}
	adaylar := laravelKokAdaylari(sk)

	otomatik := ""
	if kurulu, _ := laravelKurulu(filepath.Join("/home", sk, mevcut)); !kurulu && len(adaylar) > 0 {
		// Mevcut kokte Laravel yok; ilk adayi sahiplen (idempotent, veri degistirmez).
		if _, err := h.DB.ExecContext(r.Context(),
			`UPDATE cp_laravel_apps SET app_root=? WHERE domain_id=?`, adaylar[0], id); err == nil {
			mevcut, otomatik = adaylar[0], adaylar[0]
		}
	}
	appDir := filepath.Join("/home", sk, mevcut)
	artisan, composerJSON := laravelKurulu(appDir)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"mevcut":             mevcut,
		"adaylar":            adaylar,
		"otomatik_algilandi": otomatik,
		"kurulu":             artisan,
		"eksikler":           laravelEksikler(sk, appDir, artisan, composerJSON),
	})
}

// laravelEksikler — uygulamanin calismasi icin eksik bilesenler (frontend "Otomatik
// hazirla" butonunu bunlara gore gosterir).
func laravelEksikler(sk, appDir string, artisan, composerJSON bool) []string {
	var eks []string
	if !artisan {
		return eks // Laravel degil — eksik listesi anlamsiz
	}
	if _, err := os.Stat(filepath.Join(appDir, "vendor", "autoload.php")); err != nil {
		eks = append(eks, "vendor")
	}
	// .env root olarak yalnız symlink izlemeden okunur (envOku) — .env symlink'iyle
	// başka dosyanın içeriği hakkında bilgi (APP_KEY var/yok) sızdırılamaz.
	if s, err := envOku(sk, appDir); err != nil {
		eks = append(eks, "env")
	} else if !strings.Contains(s, "APP_KEY=base64:") {
		eks = append(eks, "app_key")
	}
	for _, d := range []string{"storage", filepath.Join("bootstrap", "cache")} {
		p := filepath.Join(appDir, d)
		if st, err := os.Stat(p); err != nil || st.Mode().Perm()&0o200 == 0 {
			eks = append(eks, "izin:"+d)
		}
	}
	_ = composerJSON
	return eks
}

// PUT /domains/{id}/laravel/app-root {"app_root":"public_html/uygulama"}
// Toolkit'in yöneteceği Laravel proje kökünü değiştirir (alt klasör / mevcut kurulumu
// sahiplenme). public_html içine confine (guvenliAppDir). Yeni kökte public varsa
// belge kökü otomatik <app_root>/public'e ayarlanır.
func (h *Handlers) SetAppRoot(w http.ResponseWriter, r *http.Request) {
	id, sk, _, demo, ok := h.lookup(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde değiştirilemez")
		return
	}
	var req struct {
		AppRoot string `json:"app_root"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	appDir, err := guvenliAppDir(sk, req.AppRoot) // '..'/symlink/public_html-dışı reddi
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	appRoot := strings.Trim(strings.TrimSpace(req.AppRoot), "/")
	if appRoot == "" {
		appRoot = "public_html"
	}
	if _, err := os.Stat(appDir); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "dizin bulunamadı: "+appRoot)
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE cp_laravel_apps SET app_root=? WHERE domain_id=?`, appRoot, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "kaydedilemedi: "+err.Error())
		return
	}
	artisan, _ := laravelKurulu(appDir)
	// yeni kökte public varsa belge kökünü de taşı
	if artisan {
		if _, e := os.Stat(filepath.Join(appDir, "public")); e == nil {
			_ = h.setDocroot(r.Context(), id, sk, altDizinPublic(appRoot))
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "app_root": appRoot, "kurulu": artisan,
	})
}

// OtomatikHazirla: POST /domains/{id}/laravel/otomatik-hazirla
// Algilanan Laravel uygulamasinin EKSIK bilesenlerini tamamlar:
//
//	vendor yoksa   → composer install --no-dev -o
//	.env yoksa     → .env.example'dan kopyala
//	APP_KEY yoksa  → php artisan key:generate
//	APP_URL yanlis → hedef domaine cek (tasima kalintisi http://127.0.0.1 vb.)
//	izin/sahiplik  → storage + bootstrap/cache tenant'a, .env 0600
//	onbellek       → config/route/view cache temizle (eski sunucu yollari)
//
// Komutlar (composer/artisan/.env yazımı/mkdir) TENANT kullanicisi olarak calisir.
// Root'ta kalan dosya işlemleri (.env.example/.env okuma, .env chmod, göç kalıntısı
// chown, önbellek silme) ev köküne çapalı ve symlink izlemeden yapılır (evAlti*).
func (h *Handlers) OtomatikHazirla(w http.ResponseWriter, r *http.Request) {
	id, sk, phpSurum, demo, ok := h.lookup(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde çalıştırılamaz")
		return
	}
	appDir, err := h.appDizin(r, id, sk)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if artisan, _ := laravelKurulu(appDir); !artisan {
		httpx.WriteError(w, http.StatusBadRequest, "bu dizinde Laravel uygulaması yok (artisan bulunamadı)")
		return
	}
	var alanAdi string
	_ = h.DB.QueryRowContext(r.Context(), `SELECT alan_adi FROM domains WHERE id=?`, id).Scan(&alanAdi)

	ctx := r.Context()
	php := phpBin(phpSurum)
	var adimlar []map[string]any
	kaydet := func(ad string, ok bool, not string) {
		adimlar = append(adimlar, map[string]any{"adim": ad, "ok": ok, "not": not})
	}

	// 🔴 Root'un bu dizindeki DOSYALAR üzerindeki tüm işlemleri ev köküne çapalı ve
	// symlink izlemeden yapılır (evAlti* yardımcıları, runner.go). Eskiden os.ReadFile/
	// os.Chmod/os.Remove/chown -R yol ile root olarak symlink izlerdi → tenant'ın
	// .env.example/.env/bootstrap symlink'iyle başka tenant'ın ya da sistemin dosyası
	// okunur, izni değişir, silinir ya da tenant'a devredilirdi.
	home, appRel, err := evGoreli(sk, appDir)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	uid, gid, err := tenantKimlik(sk)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "sistem kullanıcısı bulunamadı")
		return
	}

	// 1) .env yoksa ornekten uret (.env.example yalnız kiracıya ait düzenli dosyaysa)
	if calisti, ok1, not := envOrnektenUret(home, appRel, uid, func(icerik string) error {
		return envYaz(sk, appDir, icerik)
	}); calisti {
		kaydet(".env oluşturuldu", ok1, not)
	}
	// 2) vendor yoksa composer install
	if _, e := os.Stat(filepath.Join(appDir, "vendor", "autoload.php")); e != nil {
		out, ok2 := TenantExec(ctx, sk, appDir, "composer", "install", "--no-dev", "--optimize-autoloader", "--no-interaction")
		kaydet("composer install", ok2, sonSatirKisa(out))
	}
	// 3) APP_KEY yoksa uret
	if s, e := envOku(sk, appDir); e == nil && !strings.Contains(s, "APP_KEY=base64:") {
		out, ok2 := TenantExec(ctx, sk, appDir, php, "artisan", "key:generate", "--force")
		kaydet("APP_KEY üretildi", ok2, sonSatirKisa(out))
	}
	// 4) APP_URL'i hedef domaine cek
	if s, e := envOku(sk, appDir); e == nil && alanAdi != "" {
		yeni := reAppURL.ReplaceAllString(s, "APP_URL=https://"+alanAdi)
		if yeni != s {
			if e2 := envYaz(sk, appDir, yeni); e2 == nil {
				kaydet("APP_URL güncellendi", true, "https://"+alanAdi)
			}
		}
	}
	_ = evAltiChmod(home, path.Join(appRel, ".env"), uid, 0o600)
	// 5) izin/sahiplik — eksik dizin symlink-güvenli yaratılıp tenant'a verilir; göç
	// kalıntısı root-sahipli içerik symlink-güvenli özyinelemeli chown ile tenant'a geçer.
	izinOK, izinNot := true, "storage + bootstrap/cache"
	for _, d := range []string{"storage", path.Join("bootstrap", "cache")} {
		_ = evAltiMkdirHepsi(home, path.Join(appRel, d), uid, gid)
		if e := evAltiSahiplikVer(home, path.Join(appRel, d), uid, gid); e != nil {
			izinOK, izinNot = false, d+": "+e.Error()
		}
	}
	kaydet("izinler ayarlandı", izinOK, izinNot)
	// 6) onbellek temizligi (eski sunucu yollari)
	_ = evAltiSil(home, path.Join(appRel, "bootstrap", "cache"),
		[]string{"config.php", "services.php", "packages.php", "routes-v7.php"})
	out, ok2 := TenantExec(ctx, sk, appDir, php, "artisan", "optimize:clear")
	kaydet("önbellek temizlendi", ok2, sonSatirKisa(out))

	artisan, _ := laravelKurulu(appDir)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "adimlar": adimlar, "kurulu": artisan,
		"eksikler": laravelEksikler(sk, appDir, artisan, true),
	})
}

var reAppURL = regexp.MustCompile(`(?m)^APP_URL=.*$`)

func sonSatirKisa(s string) string {
	s = strings.TrimSpace(ansiTemizle(s))
	if s == "" {
		return ""
	}
	p := strings.Split(s, "\n")
	son := strings.TrimSpace(p[len(p)-1])
	if len(son) > 160 {
		son = son[:160]
	}
	return son
}
