// Package wordpress: 1-tıkla WordPress kurulum + yönetim (wp-cli, domain kullanıcısı olarak).
// Güvenlik: runuser -u c_<slug> (domain kullanıcısı), yol public_html'e kilitli, kök-site silme yasak.
package wordpress

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/hesaplar"
	"girginospanel/internal/httpx"

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
	Dizin    string `json:"dizin"`
	SiteURL  string `json:"site_url"`
	AdminURL string `json:"admin_url"`
	Surum    string `json:"surum"`
}

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
func wpKomut(sk string, args ...string) ([]byte, error) {
	full := append([]string{"-u", sk, "--", "env", "HOME=/home/" + sk,
		"/usr/bin/php", "-d", "memory_limit=512M", wpBin}, args...)
	cmd := exec.Command("runuser", full...)
	return cmd.CombinedOutput()
}

func (h *Handlers) scheme(ssl bool) string {
	if ssl {
		return "https://"
	}
	return "http://"
}

// GET /domains/{id}/wordpress — kurulu WP'leri keşfet (public_html + 1 alt dizin)
func (h *Handlers) Liste(w http.ResponseWriter, r *http.Request) {
	_, sk, _, _, _, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	root, _, _ := h.wpKapsam(r, sk)
	out := []Kurulum{}
	adaylar := []string{root}
	if entries, err := os.ReadDir(root); err == nil {
		for _, e := range entries {
			if e.IsDir() {
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
		if b, err := wpKomut(sk, "core", "version", "--path="+dir); err == nil {
			k.Surum = strings.TrimSpace(string(b))
		}
		if b, err := wpKomut(sk, "option", "get", "siteurl", "--path="+dir); err == nil {
			k.SiteURL = strings.TrimSpace(string(b))
			k.AdminURL = k.SiteURL + "/wp-admin"
		}
		out = append(out, k)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
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
// AdminOnly. wp-cli çağrıları worker-pool (4) ile paralel, her çağrı context-timeout'lu.
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

	out := make([]TumKurulum, len(adaylar))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i := range adaylar {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, a wpAday) {
			defer wg.Done()
			defer func() { <-sem }()
			out[i] = h.incele(r.Context(), a)
		}(i, adaylar[i])
	}
	wg.Wait()
	httpx.WriteJSON(w, http.StatusOK, out)
}

// incele: tek WP kurulumu için sürüm + güncelleme durumu + kurulum tarihini toplar.
func (h *Handlers) incele(ctx context.Context, a wpAday) TumKurulum {
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
		SiteURL: base, AdminURL: base + "/wp-admin", Durum: "bilinmiyor",
	}
	// mevcut sürüm
	c1, cancel1 := context.WithTimeout(ctx, 15*time.Second)
	if b, err := wpStdout(c1, a.sk, "core", "version", "--path="+a.dir); err == nil {
		k.Surum = strings.TrimSpace(string(b))
	}
	cancel1()
	// güncelleme kontrolü (wordpress.org API'sine gider → timeout şart)
	c2, cancel2 := context.WithTimeout(ctx, 25*time.Second)
	if b, err := wpStdout(c2, a.sk, "core", "check-update", "--path="+a.dir, "--format=json"); err == nil {
		bt := bytes.TrimSpace(b)
		if len(bt) == 0 || string(bt) == "[]" {
			k.Durum = "guncel"
		} else {
			var ups []struct {
				Version string `json:"version"`
			}
			if json.Unmarshal(bt, &ups) == nil {
				if len(ups) > 0 {
					k.Durum = "eski"
					k.SonSurum = ups[0].Version
				} else {
					k.Durum = "guncel"
				}
			}
		}
	}
	cancel2()
	// kurulum tarihi: wp-config.php değişme zamanı (kuruluşta yazılır, nadir değişir)
	if fi, err := os.Stat(filepath.Join(a.dir, "wp-config.php")); err == nil {
		k.KurulumTarihi = fi.ModTime().Format("2006-01-02")
	}
	return k
}

// wpStdout: wp-cli'yi domain kullanıcısı olarak, context-timeout ile çalıştırır; SADECE stdout döner
// (stderr yutulur → JSON çıktısı deprecation-warning ile bozulmaz).
func wpStdout(ctx context.Context, sk string, args ...string) ([]byte, error) {
	full := append([]string{"-u", sk, "--", "env", "HOME=/home/" + sk,
		"/usr/bin/php", "-d", "memory_limit=512M", wpBin}, args...)
	cmd := exec.CommandContext(ctx, "runuser", full...)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	return out.Bytes(), err
}

// wpKurulumKilit: eşzamanlı WordPress kurulumlarını hedef-yola göre serileştirir
// (müşterinin çift-tık / hızlı-tekrar yarışı). Değer önemsiz; anahtar = mutlak hedef dizin.
var wpKurulumKilit sync.Map

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
		switch strings.ToLower(e.Name()) {
		// zararsız placeholder / sistem dosyaları — "boş" say
		case "index.html", "index.htm", "favicon.ico", "robots.txt",
			"error_log", ".user.ini", ".well-known", "cgi-bin",
			".ftpquota", ".htaccess", ".git", ".gitkeep":
			continue
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
	root, wpHost, wpAlt := h.wpKapsam(r, sk)
	hedef := root
	if req.AltDizin != "" {
		hedef = filepath.Join(root, req.AltDizin)
	}
	// ÇİFT-KURULUM KORUMASI (#369) — idempotent guard. Müşterinin aynı kurulumu
	// 2. kez (çift-tık / hızlı tekrar) yapıp MEVCUT kurulumu EZMESİNİ engeller.
	// (a) eşzamanlı kurulum kilidi: ilk istek bitmeden gelen 2. istek 409 alır —
	//     wp-config.php henüz yazılmamışken bile yarış kapatılır.
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
	if err := os.MkdirAll(hedef, 0o755); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "hedef dizin oluşturulamadı")
		return
	}
	_ = exec.Command("chown", "-R", sk+":"+sk, hedef).Run()
	_ = exec.Command("restorecon", "-R", hedef).Run()

	// DB oluştur
	slug := randSlug()
	dbName := "wp_" + slug
	dbUser := "wpu_" + slug
	dbPass := hesaplar.RandomParola(24)
	if err := hesaplar.MySQLCreateDB(h.DB, id, dbName, dbUser, dbPass); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "veritabanı oluşturulamadı: "+err.Error())
		return
	}
	basarisiz := func(asama string, out []byte) {
		_, _ = h.DB.Exec("DROP DATABASE IF EXISTS `" + dbName + "`")
		_, _ = h.DB.Exec("DROP USER IF EXISTS '" + dbUser + "'@'localhost'")
		if req.AltDizin != "" { // sadece kendi oluşturduğumuz alt dizini temizle
			_ = os.RemoveAll(hedef)
		}
		msg := strings.TrimSpace(string(out))
		if len(msg) > 600 {
			msg = msg[len(msg)-600:]
		}
		httpx.WriteError(w, http.StatusInternalServerError, asama+" başarısız: "+msg)
	}

	if out, err := wpKomut(sk, "core", "download", "--path="+hedef, "--locale=tr_TR"); err != nil {
		basarisiz("WordPress indirme", out)
		return
	}
	if out, err := wpKomut(sk, "config", "create", "--dbname="+dbName, "--dbuser="+dbUser,
		"--dbpass="+dbPass, "--dbhost=localhost", "--locale=tr_TR", "--path="+hedef, "--skip-check"); err != nil {
		basarisiz("wp-config oluşturma", out)
		return
	}
	siteHost := alanAdi
	siteSSL := ssl
	if wpAlt {
		siteHost = wpHost
		siteSSL = false
	}
	url := h.scheme(siteSSL) + siteHost
	if req.AltDizin != "" {
		url += "/" + req.AltDizin
	}
	adminParola := randParola()
	if out, err := wpKomut(sk, "core", "install", "--url="+url, "--title="+req.SiteBasligi,
		"--admin_user="+req.AdminKullanici, "--admin_password="+adminParola,
		"--admin_email="+req.AdminEmail, "--skip-email", "--path="+hedef); err != nil {
		basarisiz("WordPress kurulum", out)
		return
	}
	_ = exec.Command("chown", "-R", sk+":"+sk, hedef).Run()
	_ = exec.Command("restorecon", "-R", hedef).Run()

	surum := ""
	if b, err := wpKomut(sk, "core", "version", "--path="+hedef); err == nil {
		surum = strings.TrimSpace(string(b))
	}
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
	_ = json.NewDecoder(r.Body).Decode(&greq)
	wpRoot, _, _ := h.wpKapsam(r, sk)
	dir, err := cozDizin(wpRoot, greq.Dizin)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
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
func wpKokTemizle(dir string) error {
	for _, ad := range wpKokGirdileri {
		if err := os.RemoveAll(filepath.Join(dir, ad)); err != nil {
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
		if err := os.Remove(filepath.Join(dir, ad)); err != nil {
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
		`SELECT db_user FROM db_accounts WHERE db_name=? AND domain_id=?`,
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
func (h *Handlers) wpDBDusur(ctx context.Context, dbName string, domainID int64) error {
	dbUser := h.dbKullaniciAl(ctx, dbName, domainID)
	if dbUser == "" {
		log.Printf("wordpress sil: %s icin db_accounts'ta kullanici yok — yalniz DB dusuruluyor", dbName)
		return hesaplar.MySQLDropDBKeepUser(h.DB, dbName)
	}
	return hesaplar.MySQLDropDB(h.DB, dbName, dbUser)
}

func (h *Handlers) Sil(w http.ResponseWriter, r *http.Request) {
	domID, sk, _, _, demo, ok := h.domain(r)
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
	_ = json.NewDecoder(r.Body).Decode(&sreq)
	wpRoot, _, _ := h.wpKapsam(r, sk)
	dir, err := cozDizin(wpRoot, sreq.Dizin)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
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
	if sreq.DBSil {
		dbSonuc = "wp-config.php okunamadı — veritabanı adı bulunamadı"
		if b, err := os.ReadFile(filepath.Join(dir, "wp-config.php")); err == nil {
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
				sahip := func(n string, d int64) (bool, error) { return h.dbSahipMi(r.Context(), n, d) }
				if !dropIzinli(dbName, domID, sahip) {
					dbSonuc = "veritabanı '" + dbName + "' bu domaine kayıtlı değil — güvenlik gereği düşürülmedi"
					// 🔴 `h.DB.Exec("DROP DATABASE ...")` CALISMIYOR: panelin MySQL
					// kullanicisi tenant veritabanlarinda DROP yetkisine sahip degil
					// (Error 1044). Panelin baska yerlerde kullandigi ve GERCEKTEN
					// calisan yol `hesaplar.MySQLDropDBKeepUser`: root soketi
					// uzerinden `mysql -e` calistirir VE `db_accounts` metadata
					// satirini da siler (aksi halde yetim kayit kalirdi).
				} else if derr := h.wpDBDusur(r.Context(), dbName, domID); derr != nil {
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
		if err := wpKokTemizle(dir); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "WordPress dosyaları temizlenemedi: "+err.Error())
			return
		}
	} else if err := os.RemoveAll(dir); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "silinemedi")
		return
	}
	// Dosyalar silindi; DB kismi basarisizsa bunu SAKLAMA — kullanici
	// veritabaninin durdugunu bilmeli ki elle temizleyebilsin.
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
func (h *Handlers) dbSahipMi(ctx context.Context, dbName string, domainID int64) (bool, error) {
	var n int
	err := h.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM db_accounts WHERE db_name=? AND domain_id=?`, dbName, domainID).Scan(&n)
	return n > 0, err
}

func randSlug() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b) // 8 hex char
}

func randParola() string {
	const alfabe = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789"
	b := make([]byte, 18)
	_, _ = rand.Read(b)
	out := make([]byte, 18)
	for i, c := range b {
		out[i] = alfabe[int(c)%len(alfabe)]
	}
	return string(out)
}
