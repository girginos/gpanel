package laravel

import (
	"encoding/json"
	"errors"
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

// errKimlikYok: kiracının sistem kullanıcısı çözülemedi ya da root/sistem kimliğine çıktı.
var errKimlikYok = errors.New("sistem kullanıcısı bulunamadı")

// onbellekDosyalari: 6. adımda bootstrap/cache içinden silinen (eski sunucu yollu) dosyalar.
var onbellekDosyalari = []string{"config.php", "services.php", "packages.php", "routes-v7.php"}

// otomatikDosya: OtomatikHazirla'nın ROOT olarak yaptığı TÜM dosya işlemleri (1. adım .env
// üretimi, .env 0600, 5. adım dizin/sahiplik, 6. adım önbellek silme). Her yöntem ev köküne
// çapalı ve symlink izlemeden çalışır (evAlti*, runner.go); DB/runuser gerektirmez, bu
// yüzden t.TempDir sahte evinde uçtan uca test edilir. Handler dosya işlemini yalnız bu
// tip üzerinden yapar; kiracı uid'si handler'a hiç inmez (yalnız bu yapıda taşınır).
type otomatikDosya struct {
	home, appRel string // <evKoku>/<sk> ve köke göre uygulama dizini
	uid, gid     int    // kiracı kimliği (sahiplik denetimi + devir)
}

// otomatikDosyaHazirla: sk + mutlak uygulama dizini → otomatikDosya. Uygulama dizini ev
// dışındaysa hata; kimlik çözülemez ya da root/sistem kimliğine (<=0) çıkarsa errKimlikYok
// (uid 0 ile sahiplik denetimi root dosyalarını "kiracıya ait" sayardı).
func otomatikDosyaHazirla(sk, appDir string) (otomatikDosya, error) {
	home, rel, err := evGoreli(sk, appDir)
	if err != nil {
		return otomatikDosya{}, err
	}
	uid, gid, err := kimlikBul(sk)
	if err != nil || uid <= 0 || gid <= 0 {
		return otomatikDosya{}, errKimlikYok
	}
	return otomatikDosya{home: home, appRel: rel, uid: uid, gid: gid}, nil
}

// envUret: 1. adım — .env yoksa .env.example'dan (yalnız kiracıya ait düzenli dosya,
// symlink izlenmez); içerik yaz ile (üretimde envYaz: kiracı uid'siyle tee) yazılır.
func (o otomatikDosya) envUret(yaz func(string) error) (calisti, ok bool, not string) {
	return envOrnektenUret(o.home, o.appRel, o.uid, yaz)
}

// envKilitle: .env → 0600; yalnız kiracıya ait düzenli dosyada (symlink hedefi değişmez).
func (o otomatikDosya) envKilitle() error {
	return evAltiChmod(o.home, path.Join(o.appRel, ".env"), o.uid, 0o600)
}

// izinler: 5. adım — storage + bootstrap/cache eksikse symlink-güvenli yaratılır, ağaç
// symlink-güvenli özyinelemeli chown ile kiracıya devredilir.
func (o otomatikDosya) izinler() (ok bool, not string) {
	ok, not = true, "storage + bootstrap/cache"
	for _, d := range []string{"storage", path.Join("bootstrap", "cache")} {
		rel := path.Join(o.appRel, d)
		_ = evAltiMkdirHepsi(o.home, rel, o.uid, o.gid, 0o755)
		if e := evAltiSahiplikVer(o.home, rel, o.uid, o.gid); e != nil {
			ok, not = false, d+": "+e.Error()
		}
	}
	return ok, not
}

// onbellekSil: 6. adım — bootstrap/cache içindeki onbellekDosyalari (Unlinkat; ara bileşen
// symlink'i izlenmez, dosya symlink'se yalnız link silinir).
func (o otomatikDosya) onbellekSil() error {
	return evAltiSil(o.home, path.Join(o.appRel, "bootstrap", "cache"), onbellekDosyalari)
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
// chown, önbellek silme) yalnız otomatikDosya/envOku üzerinden, ev köküne çapalı ve
// symlink izlemeden yapılır (evAlti*).
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

	// Adımlar (composer install, key:generate, optimize) istemciden KOPUK: 30 sn'de
	// kopan tarayıcı .env yazılmış ama vendor/ yarım bir uygulama bırakmasın.
	ctx, iptal := httpx.KopukBaglam(r, kopukSure)
	defer iptal()
	php := phpBin(phpSurum)
	var adimlar []map[string]any
	kaydet := func(ad string, ok bool, not string) {
		adimlar = append(adimlar, map[string]any{"adim": ad, "ok": ok, "not": not})
	}

	// 🔴 Root'un bu dizindeki DOSYALAR üzerindeki tüm işlemleri YALNIZ otomatikDosya
	// üzerinden (ev köküne çapalı, symlink izlemeden) yapılır. Eskiden os.ReadFile/
	// os.Chmod/os.Remove/chown -R yol ile root olarak symlink izlerdi → tenant'ın
	// .env.example/.env/bootstrap symlink'iyle başka tenant'ın ya da sistemin dosyası
	// okunur, izni değişir, silinir ya da tenant'a devredilirdi. Bu gövdede yol tabanlı
	// os.*/exec çağrısı kaynak koruma testiyle yasaktır (otomatik_dosya_test.go).
	od, err := otomatikDosyaHazirla(sk, appDir)
	if err != nil {
		kod := http.StatusBadRequest
		if errors.Is(err, errKimlikYok) {
			kod = http.StatusInternalServerError
		}
		httpx.WriteError(w, kod, err.Error())
		return
	}

	// 1) .env yoksa ornekten uret (.env.example yalnız kiracıya ait düzenli dosyaysa)
	if calisti, ok1, not := od.envUret(func(icerik string) error {
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
	_ = od.envKilitle()
	// 5) izin/sahiplik — eksik dizin symlink-güvenli yaratılıp tenant'a verilir; göç
	// kalıntısı root-sahipli içerik symlink-güvenli özyinelemeli chown ile tenant'a geçer.
	izinOK, izinNot := od.izinler()
	kaydet("izinler ayarlandı", izinOK, izinNot)
	// 6) onbellek temizligi (eski sunucu yollari)
	_ = od.onbellekSil()
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
