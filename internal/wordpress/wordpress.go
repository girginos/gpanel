// Package wordpress: 1-tıkla WordPress kurulum + yönetim (wp-cli, domain kullanıcısı olarak).
// Güvenlik: kiraci.Komut (runuser -u c_<slug>, panel ortamı yok), yol public_html'e kilitli, kök-site silme yasak.
package wordpress

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/gizli"
	"girginospanel/internal/hesaplar"
	"girginospanel/internal/httpx"
	"girginospanel/internal/kiraci"
	"girginospanel/internal/kota"
	"girginospanel/internal/middleware"
	"girginospanel/internal/sinirlidosya"

	"github.com/go-chi/chi/v5"
)

type Handlers struct{ DB *sql.DB }

const wpBin = "/usr/local/bin/wp"

var (
	reAltDizin = regexp.MustCompile(`^[a-z0-9]([a-z0-9_-]{0,30}[a-z0-9])?$`)
	reAdmin    = regexp.MustCompile(`^[A-Za-z0-9._@-]{3,60}$`)
	reEmail    = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	reDBName   = regexp.MustCompile(`define\(\s*['"]DB_NAME['"]\s*,\s*['"]([^'"]+)['"]`)
)

type Kurulum struct {
	Dizin          string `json:"dizin"`
	SiteURL        string `json:"site_url"`
	AdminURL       string `json:"admin_url"`
	Surum          string `json:"surum"`
	AdminKullanici string `json:"admin_kullanici,omitempty"`
	ParolaVar      bool   `json:"parola_var"` // kurulumda uretilen parola saklandi mi (iste-goster)
	// Hesap geneli listede (GET .../wordpress/hesap) hangi alan adına ait olduğu.
	AlanAdi   string `json:"alan_adi,omitempty"`
	Tur       string `json:"tur,omitempty"` // ana | ek | alt
	EkAlanID  int64  `json:"ek_alan_id,omitempty"`
	AltAlanID int64  `json:"alt_alan_id,omitempty"`
}

// kopukSure — istemciden kopuk (httpx.KopukBaglam) yan etki zincirlerinin üst süresi.
const kopukSure = 15 * time.Minute

func (h *Handlers) domain(r *http.Request) (id int64, sk, alanAdi string, ssl, demo, ok bool) {
	id, _ = strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var cert string
	var isDemo int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici, alan_adi, COALESCE(cert_path,''), COALESCE(is_demo,0) FROM domains WHERE id=?`, id).
		Scan(&sk, &alanAdi, &cert, &isDemo); err != nil {
		return id, "", "", false, false, false
	}
	return id, sk, alanAdi, cert != "", isDemo == 1, true
}

// wp <args> komutunu domain kullanıcısı olarak çalıştırır (HOME set, shell yok).
// php'yi doğrudan -d memory_limit=512M ile çağır (ham .phar shebang'i WP_CLI_PHP_ARGS'ı
// okumaz; arşiv çıkarımı 128M default'a takılır).
//
// 🔴 GÜVENLİK: wp-cli kiracının wp-config.php'sini ve eklentilerini YÜKLER (kiracı
// PHP'si) → panel ortamı ASLA verilmez: kiracıKomut (kiraci.Komut) yalnız
// PATH/HOME/LANG verir. Önceden `env HOME=...` ortamı AYNEN aktarıyordu; kiracı PHP'si
// getenv('PANEL_JWT_SECRET') ile yönetici JWT'si üretebiliyordu.
func wpKomut(sk string, args ...string) ([]byte, error) {
	cmd, err := kiraciKomut(context.Background(), sk, wpArgv(args)...)
	if err != nil {
		return nil, err
	}
	return cmd.CombinedOutput()
}

// wpArgv: wp-cli'nin tam argv'si (php doğrudan, 512M bellek).
func wpArgv(args []string) []string {
	return append([]string{"/usr/bin/php", "-d", "memory_limit=512M", wpBin}, args...)
}

// kiraciKomut: argv'yi kiracı (sk) kimliğinde, panel ortamı OLMADAN hazırlar;
// süreç ağacı kiracının kaynak dilimine girer (Dilim: wp-cli/rm -rf kiracı yükü).
func kiraciKomut(ctx context.Context, sk string, argv ...string) (*exec.Cmd, error) {
	return kiraci.Kiraci{Kul: sk, Ev: "/home/" + sk, Dilim: true}.Komut(ctx, argv...)
}

// guvenliSilTenant: hedefi TENANT kimliğinde (runuser -u sk) siler.
// 🔴 GÜVENLİK (CWE-59 link-following): os.RemoveAll KÖK olarak çağrılınca yol'daki
// ara-bileşen symlink'lerini izler (Go os.RemoveAll parent'ı plain open() ile açar).
// Tenant kendi public_html'ine `x -> /home/baskatenant` symlink'i kurup (RAR çıkarma
// veya PHP symlink) silme ucunu tetikleyerek KÖK yetkisiyle komşu tenant'ın sitesini
// sildirebiliyordu. Silmeyi tenant UID'sinde yapınca kurbanın 0710 home'una erişim
// imkânsız (DAC). shell yok (argv). Bkz. internal/files/safeio.go (aynı tehdit, openat2).
func guvenliSilTenant(sk, hedef string) error {
	if !strings.HasPrefix(sk, "c_") {
		return fmt.Errorf("güvenlik: geçersiz kullanıcı")
	}
	if hedef == "" || !strings.HasPrefix(hedef, "/home/"+sk+"/") {
		return fmt.Errorf("güvenlik: hedef tenant dizini dışında")
	}
	cmd, err := kiraciKomut(context.Background(), sk, "rm", "-rf", "--", hedef)
	if err != nil {
		return fmt.Errorf("silme: %w", err)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("silme: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (h *Handlers) scheme(ssl bool) string {
	if ssl {
		return "https://"
	}
	return "http://"
}

// GET /domains/{id}/wordpress — kurulu WP'leri keşfet (docroot + 1 alt dizin).
// Hedef URL'den çözülür: ana alan (public_html), ek alan ({eid}) ya da alt alan ({sid}).
//
// Hız: sürüm dosyadan (wp-cli yok); siteurl wp-cli ister → önbellekte yoksa alan adından
// türetilen adres hemen döner ve gerçek değer arka planda önbelleğe alınır (sonraki
// istekte gerçek). Eskiden kurulum başına 2 SIRALI wp-cli çağrısı (~1-5 sn) vardı.
func (h *Handlers) Liste(w http.ResponseWriter, r *http.Request) {
	id, sk, alanAdi, ssl, _, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	kp, ok := h.kapsam(r, sk)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, errHedefYok.Error())
		return
	}
	host, hssl := alanAdi, ssl
	if kp.Tur != TurAna {
		host, hssl = kp.Host, kp.SSL
	}
	out := h.kokKurulumlari(r.Context(), id, sk, kp.Root, host, hssl, h.adminKayitlari(r.Context(), id))
	for i := range out {
		out[i].AlanAdi, out[i].Tur, out[i].EkAlanID, out[i].AltAlanID = host, kp.Tur, kp.EkID, kp.Alt
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// GET /domains/{id}/wordpress/hesap — hesabın TÜM alan adlarındaki (ana + ek + alt) kurulu
// WordPress'ler tek listede; her kayıt ait olduğu alan adını + türünü taşır.
func (h *Handlers) HesapListe(w http.ResponseWriter, r *http.Request) {
	id, sk, alanAdi, ssl, demo, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	akullar := h.adminKayitlari(r.Context(), id)
	out := []Kurulum{}
	for _, hd := range h.hedefler(r.Context(), id, sk, alanAdi, ssl, demo) {
		if hd.DocrootDurum == DocYok {
			continue
		}
		for _, k := range h.kokKurulumlari(r.Context(), id, sk, hd.root, hd.AlanAdi, hd.SSL, akullar) {
			k.AlanAdi, k.Tur, k.EkAlanID, k.AltAlanID = hd.AlanAdi, hd.Tur, hd.EkAlanID, hd.AltAlanID
			out = append(out, k)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// adminKayitlari: panel-DB'deki admin kullanıcı kaydı (hedef dizin → kullanıcı): tek sorgu.
func (h *Handlers) adminKayitlari(ctx context.Context, id int64) map[string]string {
	akullar := map[string]string{}
	if rows, err := h.DB.QueryContext(ctx,
		`SELECT hedef, admin_kullanici FROM cp_wp_kurulum WHERE domain_id=?`, id); err == nil {
		for rows.Next() {
			var hd, ak string
			if rows.Scan(&hd, &ak) == nil {
				akullar[hd] = ak
			}
		}
		rows.Close()
	}
	return akullar
}

// altDizinAzami: docroot altında taranan alt dizin sayısı üst sınırı.
const altDizinAzami = 500

// kokKurulumlari: root ve doğrudan alt dizinlerindeki WordPress kurulumlarını keşfeder.
// host/ssl: bu docroot'un alan adı ve SSL durumu (türetilmiş URL için).
func (h *Handlers) kokKurulumlari(ctx context.Context, id int64, sk, root, host string, ssl bool, akullar map[string]string) []Kurulum {
	out := []Kurulum{}
	adaylar := []string{root}
	if entries, err := os.ReadDir(root); err == nil {
		n := 0
		for _, e := range entries {
			if e.IsDir() {
				if n++; n > altDizinAzami {
					break
				}
				adaylar = append(adaylar, filepath.Join(root, e.Name()))
			}
		}
	}
	for _, dir := range adaylar {
		if _, err := os.Stat(filepath.Join(dir, "wp-config.php")); err != nil {
			continue
		}
		k := Kurulum{Dizin: "/" + strings.TrimPrefix(strings.TrimPrefix(dir, root), "/")}
		if k.Dizin == "/" {
			k.Dizin = "/ (kök)"
		}
		k.Surum = surumOku(sk, dir)
		k.SiteURL = siteURLAl(sk, dir, h.scheme(ssl)+host, strings.TrimPrefix(strings.TrimPrefix(dir, root), "/"))
		k.AdminURL = k.SiteURL + "/wp-admin"
		if akul, varMi := akullar[dir]; varMi {
			k.ParolaVar = true
			k.AdminKullanici = akul
		}
		out = append(out, k)
	}
	return out
}

// siteURLAl: önbellekteki gerçek siteurl; yoksa taban+alt-dizin (türetilmiş) ve arka
// planda gerçek değeri doldurur. wp-cli çıktısı güvenilmez → yalnız http(s) URL kabul.
func siteURLAl(sk, dir, taban, altDizin string) string {
	key := onbAnahtar(sk, dir, "siteurl")
	if d, ok := siteOnb.Bak(key); ok {
		if s, _ := d.(string); s != "" {
			return s
		}
	}
	siteOnb.Arka(key, 30*time.Second, func(c context.Context) (any, error) {
		b, e := wpOku(c, sk, "option", "get", "siteurl", "--path="+dir)
		if e != nil {
			return nil, e
		}
		s := strings.TrimSpace(string(b))
		if u, err := url.Parse(s); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(s) > 300 {
			return "", nil
		}
		return s, nil
	})
	if altDizin != "" {
		return taban + "/" + altDizin
	}
	return taban
}

// TumKurulum: tüm domainlerdeki tek WP kurulumunun özeti (aggregate tablo satırı).
type TumKurulum struct {
	DomainID      int64  `json:"domain_id"`
	AlanAdi       string `json:"alan_adi"`
	Dizin         string `json:"dizin"`
	Surum         string `json:"surum"`
	SonSurum      string `json:"son_surum"`      // güncelleme varsa hedef sürüm
	Durum         string `json:"durum"`          // "guncel" | "eski" | "bilinmiyor"
	KurulumTarihi string `json:"kurulum_tarihi"` // wp-config.php mtime, YYYY-MM-DD
	SiteURL       string `json:"site_url"`
	AdminURL      string `json:"admin_url"`
}

type wpAday struct {
	domainID    int64
	sk, alanAdi string
	ssl         bool
	dir, root   string
}

// GET /wordpress/tumu — TÜM domainlerdeki kurulu WordPress'leri tarar (sürüm + güncelleme + kurulum tarihi).
// AdminOnly. wp-cli ÇAĞRILMAZ: sürüm dosyadan, güncelleme sürüm başına önbellekli HTTP kontrolünden.
func (h *Handlers) TumListe(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, sistem_kullanici, alan_adi, COALESCE(cert_path,'') FROM domains ORDER BY alan_adi`)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "domainler listelenemedi")
		return
	}
	var adaylar []wpAday
	for rows.Next() {
		var id int64
		var sk, alanAdi, cert string
		if err := rows.Scan(&id, &sk, &alanAdi, &cert); err != nil {
			continue
		}
		if !strings.HasPrefix(sk, "c_") {
			continue
		}
		root := "/home/" + sk + "/public_html"
		dizinler := []string{root}
		if entries, err := os.ReadDir(root); err == nil {
			for _, e := range entries {
				if e.IsDir() {
					dizinler = append(dizinler, filepath.Join(root, e.Name()))
				}
			}
		}
		for _, dir := range dizinler {
			if _, err := os.Stat(filepath.Join(dir, "wp-config.php")); err != nil {
				continue
			}
			adaylar = append(adaylar, wpAday{id, sk, alanAdi, cert != "", dir, root})
		}
	}
	_ = rows.Err()
	rows.Close()

	// 1) Dosya sisteminden sürümler (wp-cli YOK, mikrosaniye).
	surumler := make([]string, len(adaylar))
	uniq := map[string]struct{}{}
	for i, a := range adaylar {
		surumler[i] = surumOku(a.sk, a.dir)
		if surumler[i] != "" {
			uniq[surumler[i]] = struct{}{}
		}
	}
	// 2) Güncelleme kontrolü: SÜRÜM başına en çok bir HTTP isteği (önbellekli, paralel).
	gunc := make(map[string]cekGunc, len(uniq))
	var gmu sync.Mutex
	var wg sync.WaitGroup
	for s := range uniq {
		wg.Add(1)
		go func(s string) {
			defer wg.Done()
			g := cekKontrol.sorgu(r.Context(), s)
			gmu.Lock()
			gunc[s] = g
			gmu.Unlock()
		}(s)
	}
	wg.Wait()
	out := make([]TumKurulum, len(adaylar))
	for i, a := range adaylar {
		out[i] = h.incele(a, surumler[i], gunc[surumler[i]])
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// incele: tek WP kurulumunun satırı (sürüm + güncelleme durumu + kurulum tarihi).
// wp-cli çağırmaz; girdiler önceden doğrulanmış sürüm ve önbellekli kontrol sonucudur.
func (h *Handlers) incele(a wpAday, surum string, g cekGunc) TumKurulum {
	dizinYol := strings.TrimPrefix(strings.TrimPrefix(a.dir, a.root), "/")
	base := h.scheme(a.ssl) + a.alanAdi
	if dizinYol != "" {
		base += "/" + dizinYol
	}
	dizinEt := "/" + dizinYol
	if dizinEt == "/" {
		dizinEt = "/ (kök)"
	}
	k := TumKurulum{
		DomainID: a.domainID, AlanAdi: a.alanAdi, Dizin: dizinEt,
		SiteURL: base, AdminURL: base + "/wp-admin", Durum: "bilinmiyor", Surum: surum,
	}
	switch {
	case surum == "" || !g.Bilinen:
	case g.Var:
		k.Durum, k.SonSurum = "eski", g.Hedef
	default:
		k.Durum = "guncel"
	}
	// kurulum tarihi: wp-config.php değişme zamanı (kuruluşta yazılır, nadir değişir)
	if fi, err := os.Stat(filepath.Join(a.dir, "wp-config.php")); err == nil {
		k.KurulumTarihi = fi.ModTime().Format("2006-01-02")
	}
	return k
}

// wpStdout: wp-cli'yi domain kullanıcısı olarak, context-timeout ile çalıştırır; SADECE stdout döner
// (stderr yutulur → JSON çıktısı deprecation-warning ile bozulmaz).
func wpStdout(ctx context.Context, sk string, args ...string) ([]byte, error) {
	cmd, err := kiraciKomut(ctx, sk, wpArgv(args)...)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	err = cmd.Run()
	return out.Bytes(), err
}

// wpKurulumKilit: eşzamanlı WordPress kurulumlarını hedef-yola göre serileştirir
// (müşterinin çift-tık / hızlı-tekrar yarışı). Değer önemsiz; anahtar = mutlak hedef dizin.
var wpKurulumKilit sync.Map

// Kur'un dış etkili adımları (testler sahteyle değiştirir; üretimde değerler SABİTTİR).
var (
	wpKomutFn     = wpKomut
	dbOlusturFn   = hesaplar.MySQLCreateDB
	dbDusurFn     = hesaplar.MySQLDropDB
	kotaFn        = kota.CheckDBEklenebilir
	sistemKomutFn = func(ad string, arg ...string) { _ = exec.Command(ad, arg...).Run() }
)

// kurulumZatenVar: hedef dizinde zaten bir kurulum/içerik var mı? Varsa (mesaj, true)
// döner ve kurulum DURDURULUR (mevcut içerik asla ezilmez). Boş ya da yalnız
// placeholder/sistem dosyası içeren dizin "temiz" sayılır. wp-config.php = kesin
// WordPress işareti.
func kurulumZatenVar(hedef string) (string, bool) {
	if _, err := os.Stat(filepath.Join(hedef, "wp-config.php")); err == nil {
		return "bu dizinde zaten bir WordPress kurulu (mevcut kurulum korunuyor)", true
	}
	entries, err := os.ReadDir(hedef)
	if err != nil {
		return "", false // dizin yok = temiz
	}
	for _, e := range entries {
		if zararsizDosya(e.Name()) {
			continue // zararsız placeholder / sistem dosyası — "boş" say
		}
		return "hedef dizin boş değil — mevcut içerik/kurulum korunuyor (üzerine yazılmaz). Boş bir alt dizin seçin", true
	}
	return "", false
}

// POST /domains/{id}/wordpress — kur
func (h *Handlers) Kur(w http.ResponseWriter, r *http.Request) {
	id, sk, alanAdi, ssl, demo, ok := h.domain(r)
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
	var req struct {
		AltDizin       string `json:"alt_dizin"`
		SiteBasligi    string `json:"site_basligi"`
		AdminKullanici string `json:"admin_kullanici"`
		AdminEmail     string `json:"admin_email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	req.AltDizin = strings.Trim(strings.TrimSpace(req.AltDizin), "/")
	req.SiteBasligi = strings.TrimSpace(req.SiteBasligi)
	req.AdminKullanici = strings.TrimSpace(req.AdminKullanici)
	req.AdminEmail = strings.TrimSpace(req.AdminEmail)
	if req.SiteBasligi == "" || len(req.SiteBasligi) > 120 {
		httpx.WriteError(w, http.StatusBadRequest, "site başlığı gerekli (≤120)")
		return
	}
	if !reAdmin.MatchString(req.AdminKullanici) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz admin kullanıcı adı")
		return
	}
	if !reEmail.MatchString(req.AdminEmail) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz e-posta")
		return
	}
	if req.AltDizin != "" && !reAltDizin.MatchString(req.AltDizin) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz alt dizin (küçük harf/rakam/-)")
		return
	}
	kp, kok := h.kapsam(r, sk)
	if !kok {
		httpx.WriteError(w, http.StatusNotFound, errHedefYok.Error())
		return
	}
	root := kp.Root
	// 🔴 Kök yetkili adımlardan (MkdirAll/chown) ÖNCE: docroot sembolik bağla hesap dışına
	// çıkamaz (kiracı domains/<alan> ya da subdomains dizinini yönlendirmiş olabilir).
	if gerr := docrootGuvenli(sk, root); gerr != nil {
		httpx.WriteError(w, http.StatusBadRequest, "hedef dizin güvenli değil")
		return
	}
	// Ek alan adı docroot'u ek alan oluşturulurken açılır; yoksa (kiracı sildi / bozuk) kurulum
	// yapılmaz — ek alan adı sayfasından kontrol edilmeli (sessizce yeni dizin açılmaz).
	if kp.Tur == TurEk && docrootDurum(sk, root) == DocYok {
		httpx.WriteError(w, http.StatusConflict, "ek alan adı dizini bulunamadı veya güvenli değil — ek alan adı sayfasından kontrol edin")
		return
	}
	hedef := root
	if req.AltDizin != "" {
		hedef = filepath.Join(root, req.AltDizin)
	}
	// ÇİFT-KURULUM KORUMASI (#369) — idempotent guard. Müşterinin aynı kurulumu
	// 2. kez (çift-tık / hızlı tekrar) yapıp MEVCUT kurulumu EZMESİNİ engeller.
	// (a) eşzamanlı kurulum kilidi: ilk istek bitmeden gelen 2. istek 409 alır —
	//     wp-config.php henüz yazılmamışken bile yarış kapatılır.
	defer wpOnbellekSil(sk, hedef)
	if _, sur := wpKurulumKilit.LoadOrStore(hedef, struct{}{}); sur {
		httpx.WriteError(w, http.StatusConflict, "bu dizine kurulum zaten sürüyor — lütfen bekleyin")
		return
	}
	defer wpKurulumKilit.Delete(hedef)
	// (b) hedef zaten kurulu / dolu mu → EZME, net hata dön.
	if msg, kurulu := kurulumZatenVar(hedef); kurulu {
		httpx.WriteError(w, http.StatusConflict, msg)
		return
	}
	if gerr := docrootGuvenli(sk, hedef); gerr != nil { // alt dizin de hesap içinde kalmalı
		httpx.WriteError(w, http.StatusBadRequest, "hedef dizin güvenli değil")
		return
	}
	// 🔴 PLAN LIMITI: Kur her cagrisinda YENI bir wp_<slug> veritabani acar,
	// ama kota kapisi (kota.CheckDBEklenebilir) bu pakette HIC cagrilmiyordu →
	// "domain olustur + WP kur" akisi plan max_db limitini (or. 1) sessizce 2'ye
	// cikariyordu. Kapi, DB acilmadan (ve dizin hazirlanmadan) ONCE calisir;
	// MEVCUT kurulumlar etkilenmez — yalniz yeni DB acan bu yol kontrol edilir.
	if kerr := kotaFn(r.Context(), h.DB, id); kerr != nil {
		httpx.WriteError(w, http.StatusForbidden,
			"WordPress kurulumu yeni bir veritabanı gerektiriyor; "+kerr.Error())
		return
	}
	// Yan etki burada başlar (dizin, DB, wp-cli 20-90 sn). İstemci 30 sn'de kopsa da
	// kurulum kaydı (şifreli admin parolası) yazılmalı — yoksa kurulu sitenin
	// parolasına hiçbir yoldan ulaşılamaz. Kopuk bağlam.
	kctx, iptal := httpx.KopukBaglam(r, kopukSure)
	defer iptal()
	siteHost, siteSSL := alanAdi, ssl
	if kp.Tur != TurAna {
		siteHost, siteSSL = kp.Host, kp.SSL
	}
	if err := os.MkdirAll(hedef, 0o755); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "hedef dizin oluşturulamadı")
		return
	}
	// 🔴 TOCTOU savunmasi: musteri (kendi kabugu var) MkdirAll ile chown arasinda
	// hedefi symlink'le degistirebilir. Lstat kapisi + chown -h (symlink'in KENDISINI
	// degistirir, isaret ettigi agaca inmez) birlikte kacisi kapatir.
	if fi, lerr := os.Lstat(hedef); lerr != nil || !fi.IsDir() {
		httpx.WriteError(w, http.StatusInternalServerError, "hedef dizin doğrulanamadı")
		return
	}
	sistemKomutFn("chown", "-Rh", sk+":"+sk, hedef)
	sistemKomutFn("restorecon", "-R", hedef)

	// Panelin yeni ek alan/hesap için yazdığı karşılama index.html'i (kullanıcı dosyası DEĞİL)
	// kaldırılır; WordPress index.php'si öne geçer. Tanınmayan index.html'e dokunulmaz.
	karsilamaTemizle(sk, hedef, siteHost)

	// DB oluştur
	slug := randSlug()
	dbName := "wp_" + slug
	dbUser := "wpu_" + slug
	dbPass := hesaplar.RandomParola(24)
	if slug == "" || dbPass == "" {
		// 🔴 crypto/rand basarisiz — tahmin edilebilir kimlikle DEVAM ETME.
		httpx.WriteError(w, http.StatusInternalServerError, "güvenli rastgele üretim başarısız")
		return
	}
	if err := dbOlusturFn(h.DB, id, dbName, dbUser, dbPass); err != nil {
		// 🔴 ham err.Error() musteri yanitina degil loga (mysql cikti detaylari).
		log.Printf("wp kurulum: veritabanı oluşturulamadı (db=%s): %v", dbName, err)
		httpx.WriteError(w, http.StatusInternalServerError, "veritabanı oluşturulamadı")
		return
	}
	basarisiz := func(asama string, out []byte) {
		// 🔴 panel DB kullanicisinin tenant DB'lerinde DROP yetkisi YOK (Error 1044) —
		// eski hali her basarisiz kurulumda yetim wp_<slug> DB + wpu_<slug> kullanici
		// + db_accounts satiri biriktiriyordu. MySQLDropDB root-socket ile duser,
		// metadata satirini da siler.
		if derr := dbDusurFn(h.DB, dbName, dbUser); derr != nil {
			log.Printf("wp kurulum: başarısız-kurulum temizliği düşmedi (db=%s): %v", dbName, derr)
		}
		if req.AltDizin != "" { // sadece kendi oluşturduğumuz alt dizini temizle
			_ = guvenliSilTenant(sk, hedef)
		} else {
			// kok kurulumda dizin silinmez; kurulumZatenVar bos-kok garantiledigi
			// icin yalniz wp-* artiklarini kaldirmak guvenli.
			_ = wpKokTemizle(sk, hedef)
		}
		msg := strings.TrimSpace(string(out))
		if len(msg) > 600 {
			msg = msg[len(msg)-600:]
		}
		httpx.WriteError(w, http.StatusInternalServerError, asama+" başarısız: "+msg)
	}

	if out, err := wpKomutFn(sk, "core", "download", "--path="+hedef, "--locale=tr_TR"); err != nil {
		basarisiz("WordPress indirme", out)
		return
	}
	if out, err := wpKomutFn(sk, "config", "create", "--dbname="+dbName, "--dbuser="+dbUser,
		"--dbpass="+dbPass, "--dbhost=localhost", "--locale=tr_TR", "--path="+hedef, "--skip-check"); err != nil {
		basarisiz("wp-config oluşturma", out)
		return
	}
	url := h.scheme(siteSSL) + siteHost
	if req.AltDizin != "" {
		url += "/" + req.AltDizin
	}
	adminParola := randParola()
	if adminParola == "" {
		basarisiz("yönetici parolası üretimi", nil)
		return
	}
	if out, err := wpKomutFn(sk, "core", "install", "--url="+url, "--title="+req.SiteBasligi,
		"--admin_user="+req.AdminKullanici, "--admin_password="+adminParola,
		"--admin_email="+req.AdminEmail, "--skip-email", "--path="+hedef); err != nil {
		basarisiz("WordPress kurulum", out)
		return
	}
	sistemKomutFn("chown", "-Rh", sk+":"+sk, hedef) // -h: symlink'e inme (TOCTOU)
	sistemKomutFn("restorecon", "-R", hedef)

	surum := ""
	if b, err := wpKomutFn(sk, "core", "version", "--path="+hedef); err == nil {
		surum = strings.TrimSpace(string(b))
	}
	// 🔴 CWE-200: uretilen admin parolasi YANITTA DONMEZ; sifreli saklanir,
	// sahip /domains/{id}/wordpress/admin-parola ucundan iste-goster ile alir.
	if _, e := h.DB.ExecContext(kctx,
		`INSERT INTO cp_wp_kurulum (domain_id, hedef, admin_kullanici, admin_parola) VALUES (?,?,?,?)
		 ON DUPLICATE KEY UPDATE admin_kullanici=VALUES(admin_kullanici), admin_parola=VALUES(admin_parola), olusturulma=CURRENT_TIMESTAMP`,
		id, hedef, req.AdminKullanici, gizli.SaklaBagli(adminParola, sk)); e != nil {
		log.Printf("wp kurulum parolasi saklanamadi (domain=%d hedef=%s): %v", id, hedef, e)
	}
	// Denetim: sade etiket + hedef alan adı; parola/DB bilgisi YOK.
	uid, kul := middleware.Aktor(r)
	httpx.DenetimDomain(h.DB, r, uid, kul, "wordpress.kur", strings.TrimPrefix(strings.TrimPrefix(url, "https://"), "http://"), "hesap="+sk+" tur="+kp.Tur, id, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "site_url": url, "admin_url": url + "/wp-admin",
		"admin_kullanici": req.AdminKullanici, "admin_parola": adminParola,
		"surum": surum, "db_adi": dbName,
	})
}

// POST /domains/{id}/wordpress/guncelle  {dizin}
func (h *Handlers) Guncelle(w http.ResponseWriter, r *http.Request) {
	_, sk, _, _, demo, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde kullanılamaz")
		return
	}
	var greq struct {
		Dizin string `json:"dizin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&greq); err != nil && !errors.Is(err, io.EOF) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	kp, kok := h.kapsam(r, sk)
	if !kok {
		httpx.WriteError(w, http.StatusNotFound, errHedefYok.Error())
		return
	}
	dir, err := cozDizin(kp.Root, greq.Dizin)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer wpMutasyonSonu(sk, dir)
	out1, e1 := wpKomut(sk, "core", "update", "--path="+dir)
	out2, _ := wpKomut(sk, "core", "update-db", "--path="+dir)
	if e1 != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "güncelleme: "+strings.TrimSpace(string(out1)))
		return
	}
	surum := ""
	if b, err := wpKomut(sk, "core", "version", "--path="+dir); err == nil {
		surum = strings.TrimSpace(string(b))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "surum": surum,
		"cikti": strings.TrimSpace(string(out1)) + "\n" + strings.TrimSpace(string(out2))})
}

// DELETE /domains/{id}/wordpress  {dizin, db_sil}
// wpKokGirdileri — WordPress'in kök kurulumda getirdiği kendi dosya/dizinleri.
// Kökte dizinin TAMAMI silinemez (public_html giderdi), bu yüzden yalnız
// bunlar kaldırılır. Listede olmayan hiçbir şeye dokunulmaz: kullanıcının
// yedek klasörü, başka uygulaması veya kendi dosyaları yerinde kalır.
var wpKokGirdileri = []string{
	"wp-admin", "wp-includes", "wp-content",
	"index.php", "xmlrpc.php", "readme.html", "license.txt", ".htaccess",
}

// wpKokTemizle — kökteki WordPress dosyalarını kaldırır, dizinin kendisini
// KORUR. Sabit listeye ek olarak `wp-*.php` kalıbındaki dosyalar da silinir
// (wp-config.php, wp-load.php, wp-settings.php … sürümden sürüme değişir).
func wpKokTemizle(sk, dir string) error {
	// 🔴 GÜVENLİK (CWE-59): dir kök symlink olabilir (tenant public_html'ini komşuya
	// symlink'lemiş). Son bileşen symlink ise reddet; silmeleri tenant UID'sinde yap
	// (guvenliSilTenant) → kök-yetkili symlink-takibi ile cross-tenant silme imkânsız.
	if fi, err := os.Lstat(dir); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("güvenlik: kök dizin sembolik bağlantı")
	}
	for _, ad := range wpKokGirdileri {
		if err := guvenliSilTenant(sk, filepath.Join(dir, ad)); err != nil {
			return err
		}
	}
	girdiler, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, g := range girdiler {
		ad := g.Name()
		if g.IsDir() || !strings.HasPrefix(ad, "wp-") || !strings.HasSuffix(ad, ".php") {
			continue
		}
		if err := guvenliSilTenant(sk, filepath.Join(dir, ad)); err != nil {
			return err
		}
	}
	return nil
}

// dbKullaniciAl — bu DB'ye ait MySQL kullanicisini PANELIN KENDI kaydindan
// okur. 🔴 wp-config.php'den OKUNMAZ: o dosya musterinin kontrolunde ve
// oraya baska bir tenant'in kullanici adi yazilabilir.
func (h *Handlers) dbKullaniciAl(ctx context.Context, dbName string, domainID int64) string {
	var u string
	_ = h.DB.QueryRowContext(ctx,
		`SELECT db_user FROM db_accounts WHERE BINARY db_name=? AND domain_id=?`,
		dbName, domainID).Scan(&u)
	return u
}

// wpDBDusur — WordPress'in veritabanini VE ona ait MySQL kullanicisini
// kaldirir, db_accounts kaydini siler.
//
// 🔴 Eskiden `MySQLDropDBKeepUser` kullaniliyordu: veritabani gidiyor ama
// `wpu_<slug>` kullanicisi ve GRANT'i KALIYORDU. db_accounts satiri da
// silindigi icin panelin o kullanicidan haberi kalmiyor -> her kaldirmada
// bir yetim birikiyor. Ayni adla yeni bir DB yaratilirsa eski kullanici
// ona ANINDA tam yetkiyle erisir (gizli yetki).
//
// Kullanici adi belirlenemezse (eski kurulum, kayit yok) yalniz DB dusurulur
// — tahmin ederek rastgele bir MySQL kullanicisi SILMEYIZ.
//
// 🔴 Satirdaki kullanici da KOSULSUZ guvenilir degil: yedekten geri yuklemede kiracinin
// wp-config'inden kurtarilan ad satira yazilabiliyordu (komsunun kullanicisi, roundcube,
// pma…); eskiden burada KOSULSUZ MySQLDropDB → DROP USER calisiyordu. MySQLDropDBSahipli
// kullaniciyi yalniz guvenle bu domainin hesabiysa dusurur, aksi halde yalniz DB'yi.
func (h *Handlers) wpDBDusur(ctx context.Context, dbName string, domainID int64) error {
	dbUser := h.dbKullaniciAl(ctx, dbName, domainID)
	if dbUser == "" {
		log.Printf("wordpress sil: %s icin db_accounts'ta kullanici yok — yalniz DB dusuruluyor", dbName)
	}
	return hesaplar.MySQLDropDBSahipli(h.DB, domainID, dbName, dbUser)
}

func (h *Handlers) Sil(w http.ResponseWriter, r *http.Request) {
	domID, sk, anaAd, _, demo, ok := h.domain(r)
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
	var sreq struct {
		Dizin string `json:"dizin"`
		DBSil bool   `json:"db_sil"`
	}
	if err := json.NewDecoder(r.Body).Decode(&sreq); err != nil && !errors.Is(err, io.EOF) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	kp, kok := h.kapsam(r, sk)
	if !kok {
		httpx.WriteError(w, http.StatusNotFound, errHedefYok.Error())
		return
	}
	wpRoot := kp.Root
	dir, err := cozDizin(wpRoot, sreq.Dizin)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer wpOnbellekSil(sk, dir)
	root := wpRoot
	// KÖK-SİTE KORUMASI: public_html'in KENDİSİ asla silinmez (tüm site gider).
	// Ama kökteki WordPress'i kaldırmak meşru ve yaygın bir istek — bu yüzden
	// kökte dizin yerine WordPress'in KENDİ dosyaları temizlenir; kullanıcının
	// dizine koyduğu diğer şeyler (yedek klasörü, başka dosyalar) KORUNUR.
	kokKurulum := dir == root
	// 🔴 DB silme sonucu KULLANICIYA bildirilir. Eskiden DROP hatasi
	// `_, _ = h.DB.Exec(...)` ile yok sayiliyordu: veritabani yerinde
	// kaliyor ama panel "ok" diyordu. Kullanici sildigini saniyordu.
	dbSonuc := ""
	// DB düşürme dosya silmeden ÖNCE ve istemciden KOPUK: iptal edilmiş bağlamla
	// sahiplik sorgusu düşer, DB yetim kalır, dosyalar yine silinirdi.
	kctx, iptal := httpx.KopukBaglam(r, kopukSure)
	defer iptal()
	if sreq.DBSil {
		dbSonuc = "wp-config.php okunamadı — veritabanı adı bulunamadı"
		if b, err := sinirlidosya.Oku(filepath.Join(dir, "wp-config.php"), 1<<20); err == nil {
			dbSonuc = "wp-config.php içinde DB_NAME bulunamadı"
			if m := reDBName.FindSubmatch(b); len(m) == 2 {
				dbName := string(m[1])
				// GÜVENLİK (tenant-arası DROP koruması): dbName müşterinin KENDİ
				// wp-config.php'sinden okunur → GÜVENİLMEZ. İki katman gerekir:
				//  1) dbAdiWPGuard: GecerliDBKimlik(^[A-Za-z0-9_]{1,64}$) + "wp_" öneki →
				//     backtick/tırnak/boşluk/; kaçışları ve wp_-dışı adlar REDDEDİLİR.
				//  2) SAHİPLİK: db_accounts'ta (db_name=? AND domain_id=?) satırı olmalı →
				//     müşteri wp-config'e "wp_baskatenant" yazsa bile o DB bu domaine kayıtlı
				//     değilse DROP YAPILMAZ (başka tenant'ın DB'sini düşürme engellenir).
				sahip := func(n string, d int64) (bool, error) { return h.dbSahipMi(kctx, n, d) }
				if !dropIzinli(dbName, domID, sahip) {
					dbSonuc = "veritabanı '" + dbName + "' bu domaine kayıtlı değil — güvenlik gereği düşürülmedi"
					// 🔴 `h.DB.Exec("DROP DATABASE ...")` CALISMIYOR: panelin MySQL
					// kullanicisi tenant veritabanlarinda DROP yetkisine sahip degil
					// (Error 1044). Panelin baska yerlerde kullandigi ve GERCEKTEN
					// calisan yol `hesaplar.MySQLDropDBKeepUser`: root soketi
					// uzerinden `mysql -e` calistirir VE `db_accounts` metadata
					// satirini da siler (aksi halde yetim kayit kalirdi).
				} else if derr := h.wpDBDusur(kctx, dbName, domID); derr != nil {
					dbSonuc = "veritabanı '" + dbName + "' düşürülemedi: " + derr.Error()
					log.Printf("wordpress sil: DROP DATABASE %s basarisiz: %v", dbName, derr)
				} else {
					dbSonuc = ""
					log.Printf("wordpress sil: veritabani %s dusuruldu (domain=%d)", dbName, domID)
				}
			}
		}
	}
	if kokKurulum {
		if err := wpKokTemizle(sk, dir); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "WordPress dosyaları temizlenemedi: "+err.Error())
			return
		}
	} else if err := guvenliSilTenant(sk, dir); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "silinemedi")
		return
	}
	// Dosyalar silindi; DB kismi basarisizsa bunu SAKLAMA — kullanici
	// veritabaninin durdugunu bilmeli ki elle temizleyebilsin.
	silAd := anaAd
	if kp.Tur != TurAna {
		silAd = kp.Host
	}
	suid, skul := middleware.Aktor(r)
	httpx.DenetimDomain(h.DB, r, suid, skul, "wordpress.sil", silAd, "hesap="+sk+" tur="+kp.Tur, domID, true)
	yanit := map[string]any{"ok": true}
	if dbSonuc != "" {
		yanit["db_uyari"] = dbSonuc
	}
	httpx.WriteJSON(w, http.StatusOK, yanit)
}

// cozDizin: {dizin} değerini güvenli mutlak yola çevirir (public_html içinde + wp-config var).
func cozDizin(root, dizinStr string) (string, error) {
	d := strings.TrimPrefix(strings.TrimSpace(dizinStr), "/ (kök)")
	rel := strings.Trim(strings.TrimSpace(d), "/")
	dir := root
	if rel != "" && rel != "(kök)" {
		dir = filepath.Join(root, rel)
	}
	clean := filepath.Clean(dir)
	if clean != root && !strings.HasPrefix(clean, root+"/") {
		return "", fmt.Errorf("yol domain dizini dışında")
	}
	if _, err := os.Stat(filepath.Join(clean, "wp-config.php")); err != nil {
		return "", fmt.Errorf("bu dizinde WordPress bulunamadı")
	}
	return clean, nil
}

// dbAdiWPGuard: DROP için AD-güvenlik kapısı (sahiplikten ayrı, saf → birim-test edilebilir).
// GecerliDBKimlik (^[A-Za-z0-9_]{1,64}$) → backtick/tırnak/boşluk/;/SQLi kaçışı kapalı;
// ayrıca yalnız bizim oluşturduğumuz "wp_" önekli DB'leri hedefler.
func dbAdiWPGuard(dbName string) bool {
	return hesaplar.GecerliDBKimlik(dbName) && strings.HasPrefix(dbName, "wp_")
}

// dropIzinli: dbName DROP edilebilir mi? Ad-guard'ı geçmeli VE sahiplik-sorgusu bu DB'nin
// gerçekten bu domaine ait olduğunu doğrulamalı. sahip enjekte edilebilir (birim-test için).
func dropIzinli(dbName string, domainID int64, sahip func(string, int64) (bool, error)) bool {
	if !dbAdiWPGuard(dbName) {
		return false
	}
	ok, err := sahip(dbName, domainID)
	return err == nil && ok
}

// dbSahipMi: dbName GERÇEKTEN bu domain'e ait mi? db_accounts sahiplik kontrolü. Panel'in
// oluşturduğu WP DB'leri hesaplar.MySQLCreateDB ile domain_id'li db_accounts'a kaydedilir.
// BINARY: wp-config'teki "wp_ABC" kayıtlı "wp_abc" ile eşleşip AYRI bir MariaDB şemasını
// (DB adları harf duyarlı) düşürtmemeli.
func (h *Handlers) dbSahipMi(ctx context.Context, dbName string, domainID int64) (bool, error) {
	var n int
	err := h.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM db_accounts WHERE BINARY db_name=? AND domain_id=?`, dbName, domainID).Scan(&n)
	return n > 0, err
}

func randSlug() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "" // 🔴 tahmin edilebilir slug uretme — cagiran bos gorup durdurur
	}
	return hex.EncodeToString(b) // 8 hex char
}

func randParola() string {
	const alfabe = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789"
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "" // 🔴 tahmin edilebilir parola uretme — cagiran bos gorup durdurur
	}
	out := make([]byte, 18)
	for i, c := range b {
		out[i] = alfabe[int(c)%len(alfabe)]
	}
	return string(out)
}
