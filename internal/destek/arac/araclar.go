package arac

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/destek/protokol"
	"girginospanel/internal/nginxset"
)

// aracTanim — uygulama + zaman aşımı. Katalogdaki her R0 aracın burada bir
// karşılığı vardır (TestKatalogTamUygulandi).
type aracTanim struct {
	sure time.Duration
	fn   func(context.Context, *cagri) (*cikti, error)
}

var araclar map[string]aracTanim

func init() {
	araclar = map[string]aracTanim{
		"hesap_ozet":        {20 * time.Second, hesapOzet},
		"site_yokla":        {20 * time.Second, siteYokla},
		"log_oku":           {30 * time.Second, logOku},
		"sunucu_log_oku":    {30 * time.Second, sunucuLogOku},
		"servis_durum":      {20 * time.Second, servisDurum},
		"disk_kota":         {60 * time.Second, diskKota},
		"dns_kontrol":       {45 * time.Second, dnsKontrol},
		"ssl_durum":         {30 * time.Second, sslDurum},
		"nginx_test":        {30 * time.Second, nginxTest},
		"dogrula_ozet":      {150 * time.Second, dogrulaOzet},
		"wp_durum":          {120 * time.Second, wpDurum},
		"cron_liste":        {20 * time.Second, cronListe},
		"php_ayar_oku":      {20 * time.Second, phpAyarOku},
		"yedek_liste":       {20 * time.Second, yedekListe},
		"son_degisiklikler": {20 * time.Second, sonDegisiklikler},
		"onarim_onizle":     {150 * time.Second, onarimOnizle},
	}
}

// ── ortak ──────────────────────────────────────────────────────────────────

// hamSatirSiniri — günlükten okunan ham satır üst sınırı (süzgeçten önce).
const hamSatirSiniri = 2000

func (c *cagri) satirSayisi() int {
	if n, ok := c.g.Tamsayi("satir"); ok {
		return int(n)
	}
	return 100
}

// suz — ham satırları ÖNCE maskeler, SONRA düz alt dizge süzgecini (büyük/küçük
// duyarsız) uygular, son N'i tutar. 🔴 Süzgeç maskeden sonra: aksi halde
// "filtre=parola=a" ile sır karakter karakter yoklanabilirdi (satır var/yok kahini).
func (c *cagri) suz(ham []string) ([]string, bool) {
	for i := range ham {
		ham[i] = ansiSoy(ham[i])
	}
	m := c.y.mask.Satirlar(ham)
	f, _ := c.g.Dizge("filtre")
	f = strings.ToLower(f)
	var out []string
	for _, s := range m {
		if strings.TrimSpace(s) == "" {
			continue
		}
		if f == "" || strings.Contains(strings.ToLower(s), f) {
			out = append(out, s)
		}
	}
	n := c.satirSayisi()
	kesildi := len(out) > n
	if kesildi {
		out = out[len(out)-n:]
	}
	return out, kesildi
}

func satirlaraBol(b []byte) []string {
	s := strings.TrimRight(string(b), "\n")
	if s == "" {
		return nil
	}
	ln := strings.Split(s, "\n")
	if len(ln) > hamSatirSiniri {
		ln = ln[len(ln)-hamSatirSiniri:]
	}
	return ln
}

// ── rota sarmalayan araçlar ────────────────────────────────────────────────

func hesapOzet(ctx context.Context, c *cagri) (*cikti, error) {
	dv, err := c.rota(ctx, c.h, "", nil)
	if err != nil {
		return nil, err
	}
	kv, err := c.rota(ctx, c.h, "/kaynak", nil)
	if err != nil {
		return nil, err
	}
	d, k := nesne(dv), nesne(kv)
	sat := sec(d, "", "alan_adi", "durum", "askida", "php_surum", "plan_ad", "ssl", "ssl_bitis",
		"ssh_erisim", "is_demo", "olusturulma", "disk_kota_mb", "trafik_kota_mb")
	sat = append(sat, sec(k, "kullanim", "plan_adi", "disk_mb", "trafik_mb", "db_sayisi", "db_mb", "ftp_sayisi",
		"eposta_sayi", "domain_sayi", "inode_kullanim", "inode_limit", "dns_kayit", "cron_is", "yedek_sayisi", "yedek_mb")...)
	disk := nesne(k["disk_mb"])
	oz := fmt.Sprintf("%s: paket %s, PHP %s, durum %s, disk %s/%s MB (0 = sınırsız)",
		c.h.AlanAdi, metin(k, "plan_adi"), metin(d, "php_surum"), metin(d, "durum"), metin(disk, "kullanim"), metin(disk, "limit"))
	if metin(d, "askida") == "evet" {
		oz += ", HESAP ASKIDA"
	}
	return &cikti{ozet: oz, satirlar: sat}, nil
}

func sslDurum(ctx context.Context, c *cagri) (*cikti, error) {
	v, err := c.rota(ctx, c.h, "/ssl", nil)
	if err != nil {
		return nil, err
	}
	sat := duzlestir(v, "ssl")
	return &cikti{ozet: c.h.AlanAdi + " SSL durumu (" + strconv.Itoa(len(sat)) + " alan)", satirlar: sat}, nil
}

func cronListe(ctx context.Context, c *cagri) (*cikti, error) {
	v, err := c.rota(ctx, c.h, "/cron", nil)
	if err != nil {
		return nil, err
	}
	gorevler := dizi(v)
	var sat []string
	for i, e := range gorevler {
		m := nesne(e)
		durum := "etkin"
		if metin(m, "etkin") == "hayır" {
			durum = "devre dışı"
		}
		sat = append(sat, fmt.Sprintf("görev %d [%s] %s %s %s %s %s — %s", i+1, durum,
			metin(m, "dakika"), metin(m, "saat"), metin(m, "gun"), metin(m, "ay"), metin(m, "hafta"), metin(m, "komut")))
	}
	// Son çalışmalar: crond günlüğünde YALNIZ bu kiracının satırları ("(<sk>) CMD").
	if out, err := c.y.o.komut(ctx, "tail", "-n", "20000", "/var/log/cron"); err == nil {
		isaret := "(" + c.h.SK + ")"
		var son []string
		for _, ln := range satirlaraBolHepsi(out) {
			if strings.Contains(ln, isaret) {
				son = append(son, ln)
			}
		}
		if len(son) > 20 {
			son = son[len(son)-20:]
		}
		if len(son) > 0 {
			sat = append(sat, "— son çalışmalar (crond günlüğü) —")
			sat = append(sat, son...)
		}
	}
	return &cikti{ozet: fmt.Sprintf("%s: %d zamanlanmış görev", c.h.AlanAdi, len(gorevler)), satirlar: sat}, nil
}

// satirlaraBolHepsi — süzgeçten önce kiracı satırını seçecek çağıranlar için
// (hamSatirSiniri uygulanmaz; komut çıktısı zaten ciktiSiniri ile sınırlı).
func satirlaraBolHepsi(b []byte) []string {
	s := strings.TrimRight(string(b), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func phpAyarOku(ctx context.Context, c *cagri) (*cikti, error) {
	v, err := c.rota(ctx, c.h, "/php-settings", nil)
	if err != nil {
		return nil, err
	}
	m := nesne(v)
	ay := nesne(m["ayarlar"])
	sat := []string{"php_surum: " + metin(m, "php_surum")}
	if a, ok := c.g.Dizge("ayar"); ok {
		k := strings.ReplaceAll(a, ".", "_")
		if _, var_ := ay[k]; !var_ {
			sat = append(sat, a+": (panel bu ayarı yönetmiyor — PHP varsayılanı geçerli)")
		} else {
			sat = append(sat, duzlestir(ay[k], a)...)
		}
	} else {
		sat = append(sat, duzlestir(ay, "")...)
	}
	return &cikti{ozet: c.h.AlanAdi + ": PHP " + metin(m, "php_surum"), satirlar: sat}, nil
}

func yedekListe(ctx context.Context, c *cagri) (*cikti, error) {
	v, err := c.rota(ctx, c.h, "/backups", nil)
	if err != nil {
		return nil, err
	}
	liste := dizi(v)
	var sat []string
	for i, e := range liste {
		if i >= 50 {
			break
		}
		m := nesne(e)
		sat = append(sat, fmt.Sprintf("%s | %s | %s bayt | %s | konum: %s", metin(m, "olusturma"), metin(m, "tip"),
			metin(m, "boyut_b"), metin(m, "dosya"), metin(m, "mevcut")))
	}
	if pv, err := c.rota(ctx, c.h, "/backup-schedule", nil); err == nil {
		sat = append(sat, duzlestir(pv, "zamanlama")...)
	}
	oz := fmt.Sprintf("%s: %d yedek kaydı (indirme/geri yükleme yalnız panelden)", c.h.AlanAdi, len(liste))
	return &cikti{ozet: oz, satirlar: sat, kesildi: len(liste) > 50}, nil
}

var wpDizinRe = regexp.MustCompile(`^/[A-Za-z0-9._-]*$`)

func wpDurum(ctx context.Context, c *cagri) (*cikti, error) {
	v, err := c.rota(ctx, c.h, "/wordpress", nil)
	if err != nil {
		return nil, err
	}
	kurulumlar := dizi(v)
	if len(kurulumlar) == 0 {
		return &cikti{ozet: c.h.AlanAdi + ": WordPress kurulumu bulunamadı"}, nil
	}
	var sat []string
	for i, e := range kurulumlar {
		if i >= 3 {
			sat = append(sat, fmt.Sprintf("(+%d kurulum daha)", len(kurulumlar)-3))
			break
		}
		m := nesne(e)
		dizin := metin(m, "dizin")
		sat = append(sat, fmt.Sprintf("— kurulum %d: dizin %s, sürüm %s", i+1, dizin, metin(m, "surum")))
		if strings.HasPrefix(dizin, "/ ") || dizin == "/" {
			dizin = "/"
		}
		if !wpDizinRe.MatchString(dizin) {
			continue // tenant dosya sisteminden gelen ad biçim dışı → sorguya konmaz
		}
		q := url.Values{"dizin": {dizin}}
		if dv, err := c.rota(ctx, c.h, "/wordpress/durum", q); err == nil {
			sat = append(sat, duzlestir(dv, "durum")...)
		}
		if ev, err := c.rota(ctx, c.h, "/wordpress/eklentiler", q); err == nil {
			for _, p := range dizi(ev) {
				pm := nesne(p)
				sat = append(sat, fmt.Sprintf("eklenti %s: %s, sürüm %s, güncelleme %s", metin(pm, "name"),
					metin(pm, "status"), metin(pm, "version"), metin(pm, "update")))
			}
		}
	}
	return &cikti{ozet: fmt.Sprintf("%s: %d WordPress kurulumu", c.h.AlanAdi, len(kurulumlar)), satirlar: sat}, nil
}

// ── günlükler ──────────────────────────────────────────────────────────────

var laravelKokRe = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)

func logOku(ctx context.Context, c *cagri) (*cikti, error) {
	tur, _ := c.g.Dizge("tur")
	var ham []string
	switch tur {
	case "nginx_access", "nginx_error":
		dosya := strings.TrimPrefix(tur, "nginx_")
		v, err := c.rota(ctx, c.h, "/logs/oku", url.Values{"dosya": {dosya}, "son": {strconv.Itoa(hamSatirSiniri)}})
		if err != nil {
			return nil, err
		}
		ham = satirlarAl(v)
	case "php_debug":
		v, err := c.rota(ctx, c.h, "/php/debug-log", nil)
		if err != nil {
			return nil, err
		}
		ham = satirlarAl(v)
	case "php_fpm":
		// Kiracının KENDİ FPM birimi (provisioner tenantUnitName: php-fpm-<sk>.service).
		out, _ := c.y.o.komut(ctx, "journalctl", "--no-pager", "-o", "short-iso", "-n", strconv.Itoa(hamSatirSiniri),
			"-u", "php-fpm-"+c.h.SK+".service")
		ham = satirlaraBol(out)
	case "laravel":
		kok := "public_html"
		var ar sql.NullString
		if err := c.y.db.QueryRowContext(ctx, `SELECT app_root FROM cp_laravel_apps WHERE domain_id=?`, c.h.ID).Scan(&ar); err == nil && ar.String != "" {
			kok = ar.String
		}
		if !laravelKokRe.MatchString(kok) || strings.Contains("/"+kok+"/", "/../") || strings.Contains("/"+kok+"/", "/./") {
			return nil, hataf(protokol.HataKapsam, "uygulama kökü biçim dışı")
		}
		// 🔴 KİRACI kimliğinde okunur (kiraci.Komut): ev ağacı kiracının denetiminde,
		// bağ izleme kökün değil kiracının DAC'ıyla sınırlı kalır.
		out, _ := c.y.o.kiraciKomut(ctx, c.h.SK, "tail", "-n", strconv.Itoa(hamSatirSiniri), "--",
			"/home/"+c.h.SK+"/"+kok+"/storage/logs/laravel.log")
		ham = satirlaraBol(out)
	case "cron":
		out, _ := c.y.o.komut(ctx, "tail", "-n", "20000", "/var/log/cron")
		isaret := "(" + c.h.SK + ")"
		for _, ln := range satirlaraBolHepsi(out) {
			if strings.Contains(ln, isaret) {
				ham = append(ham, ln)
			}
		}
		if len(ham) > hamSatirSiniri {
			ham = ham[len(ham)-hamSatirSiniri:]
		}
	default:
		return nil, hataf(protokol.HataParam, "günlük türü")
	}
	sat, kes := c.suz(ham)
	oz := fmt.Sprintf("%s %s günlüğü: %d satır", c.h.AlanAdi, tur, len(sat))
	if len(ham) == 0 {
		oz = fmt.Sprintf("%s %s günlüğü boş ya da yok", c.h.AlanAdi, tur)
	}
	return &cikti{ozet: oz, satirlar: sat, kesildi: kes}, nil
}

func sunucuLogOku(ctx context.Context, c *cagri) (*cikti, error) {
	if err := c.kokZorunlu(); err != nil {
		return nil, err
	}
	kaynak, _ := c.g.Dizge("kaynak")
	v, err := c.rota(ctx, nil, "/api/v1/admin/system/loglar", url.Values{"kaynak": {kaynak}, "son": {"1000"}})
	if err != nil {
		return nil, err
	}
	sat, kes := c.suz(satirlarAl(v))
	return &cikti{ozet: fmt.Sprintf("sunucu günlüğü %s: %d satır", kaynak, len(sat)), satirlar: sat, kesildi: kes}, nil
}

// ── servisler / yapılandırma ───────────────────────────────────────────────

// servisBirimleri — katalog enum'u → systemd birimleri (system.servisAllow ile
// aynı küme + posta). Komut satırına YALNIZ bu tablodaki adlar girer.
var servisBirimleri = map[string][]string{
	"nginx":     {"nginx"},
	"httpd":     {"httpd"},
	"php_fpm":   {"php-fpm", "php82-php-fpm", "php74-php-fpm"},
	"mariadb":   {"mariadb"},
	"valkey":    {"valkey"},
	"named":     {"named"},
	"pure_ftpd": {"pure-ftpd"},
	"postfix":   {"postfix"},
	"dovecot":   {"dovecot"},
	"crond":     {"crond"},
}

var servisSira = []string{"nginx", "httpd", "php_fpm", "mariadb", "valkey", "named", "pure_ftpd", "postfix", "dovecot", "crond"}

func (c *cagri) birimDurum(ctx context.Context, birim string) string {
	out, _ := c.y.o.komut(ctx, "systemctl", "is-active", "--", birim)
	s := strings.TrimSpace(string(out))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		s = "unknown"
	}
	return s
}

func genelSaglik(d string) string {
	switch d {
	case "active", "reloading", "activating":
		return "çalışıyor"
	case "inactive", "unknown":
		return "çalışmıyor / kurulu değil"
	}
	return "sorunlu"
}

func servisDurum(ctx context.Context, c *cagri) (*cikti, error) {
	secili := servisSira
	if s, ok := c.g.Dizge("servis"); ok {
		secili = []string{s}
	}
	kok := c.k.Rol == protokol.RolKok
	var sat []string
	sorunlu := 0
	for _, ad := range secili {
		if ad == "php_fpm" && !kok {
			// Müşteri/bayi: yalnız KENDİ hesaplarının kiracı FPM birimleri (en çok 20).
			for i, id := range c.k.HesapIDleri {
				if i >= 20 {
					sat = append(sat, fmt.Sprintf("php_fpm: (+%d hesap gösterilmedi)", len(c.k.HesapIDleri)-20))
					break
				}
				h, err := c.y.hesapKapsam(ctx, c.k, id, c.b.Hesaplar[i].Etiket)
				if err != nil {
					continue
				}
				g := genelSaglik(c.birimDurum(ctx, "php-fpm-"+h.SK+".service"))
				if g != "çalışıyor" {
					sorunlu++
				}
				sat = append(sat, fmt.Sprintf("php_fpm (hesap %d, %s): %s", i+1, h.AlanAdi, g))
			}
			continue
		}
		for _, b := range servisBirimleri[ad] {
			d := c.birimDurum(ctx, b)
			if kok {
				sat = append(sat, fmt.Sprintf("%s (%s): %s", ad, b, d))
				if d != "active" {
					sorunlu++
				}
				continue
			}
			g := genelSaglik(d)
			if g == "sorunlu" {
				sorunlu++
			}
			sat = append(sat, ad+": "+g)
			break // M/B: servis başına tek genel satır
		}
	}
	oz := fmt.Sprintf("%d servis satırı, %d sorunlu", len(sat), sorunlu)
	return &cikti{ozet: oz, satirlar: sat}, nil
}

func nginxTest(ctx context.Context, c *cagri) (*cikti, error) {
	out, err := c.y.o.komut(ctx, "nginx", "-t")
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	basarili := err == nil
	ham := satirlaraBol(out)
	if c.k.Rol == protokol.RolKok {
		oz := "nginx -t BAŞARILI"
		if !basarili {
			oz = "nginx -t BAŞARISIZ"
		}
		return &cikti{ozet: oz, satirlar: ham}, nil
	}
	// Müşteri/bayi: YALNIZ bu hesabın vhost'una (dom_<sk>.conf) ait satırlar +
	// kendi ek direktiflerine ait [emerg] satırları (nginxset.DirektifHatasiOzet).
	// Komşu kiracıların yolları/adları dönmez.
	vhost := "/dom_" + c.h.SK + ".conf"
	vhostAlt := "/dom_" + c.h.SK + "_"
	var sat []string
	for _, ln := range ham {
		if strings.Contains(ln, vhost) || strings.Contains(ln, vhostAlt) {
			sat = append(sat, ln)
		}
	}
	if !basarili {
		var ek string
		_ = c.y.db.QueryRowContext(ctx, `SELECT COALESCE(ek_direktifler,'') FROM nginx_settings WHERE domain_id=?`, c.h.ID).Scan(&ek)
		if ek != "" {
			if oz := nginxset.DirektifHatasiOzet(string(out), ek); oz != "" {
				sat = append(sat, strings.Split(oz, "\n")...)
			}
		}
	}
	oz := "nginx yapılandırma testi başarılı"
	switch {
	case !basarili && len(sat) > 0:
		oz = "nginx yapılandırma testi BAŞARISIZ — bu hesaba ait satırlar aşağıda"
	case !basarili:
		oz = "nginx yapılandırma testi başarısız, ancak hata bu hesabın yapılandırmasında görünmüyor (sunucu yöneticisi ilgileniyor olmalı)"
	}
	return &cikti{ozet: oz, satirlar: sat}, nil
}

type dogrulaRapor struct {
	Gecen    int `json:"gecen"`
	Kritik   int `json:"kritik"`
	Uyari    int `json:"uyari"`
	Dusenler []struct {
		Seviye  string `json:"seviye"`
		Kontrol string `json:"kontrol"`
		Detay   string `json:"detay"`
	} `json:"dusenler"`
}

func dogrulaOzet(ctx context.Context, c *cagri) (*cikti, error) {
	out, _ := c.y.o.komut(ctx, "girginospanel-dogrula", "--json")
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var r dogrulaRapor
	i := strings.IndexByte(string(out), '{')
	if i < 0 || json.Unmarshal(out[i:], &r) != nil {
		return nil, hataf(protokol.HataIc, "doğrulama raporu okunamadı")
	}
	alan, sk := strings.ToLower(c.h.AlanAdi), strings.ToLower(c.h.SK)
	var kendi, diger []string
	for _, d := range r.Dusenler {
		s := d.Seviye + " " + d.Kontrol + ": " + d.Detay
		l := strings.ToLower(d.Kontrol + " " + d.Detay)
		if strings.Contains(l, alan) || strings.Contains(l, sk) {
			kendi = append(kendi, s)
		} else {
			diger = append(diger, "[sunucu] "+s)
		}
	}
	if c.k.Rol != protokol.RolKok {
		// Müşteri/bayi: YALNIZ bu hesabı ilgilendiren bulgular; sunucu geneli sayılar da yok.
		oz := fmt.Sprintf("%s: bu hesabı ilgilendiren %d bulgu", c.h.AlanAdi, len(kendi))
		return &cikti{ozet: oz, satirlar: kendi}, nil
	}
	oz := fmt.Sprintf("doğrulama: %d geçti, %d kritik, %d uyarı; %s ile ilgili %d bulgu", r.Gecen, r.Kritik, r.Uyari, c.h.AlanAdi, len(kendi))
	return &cikti{ozet: oz, satirlar: append(kendi, diger...)}, nil
}

func onarimOnizle(ctx context.Context, c *cagri) (*cikti, error) {
	if err := c.kokZorunlu(); err != nil {
		return nil, err
	}
	out, _ := c.y.o.komut(ctx, "girginospanel-repair", "--dry-run", "--only", "domains")
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var sat []string
	for _, ln := range satirlaraBolHepsi(out) {
		ln = ansiSoy(ln)
		if strings.Contains(ln, c.h.SK) || strings.Contains(ln, c.h.AlanAdi) {
			sat = append(sat, strings.TrimSpace(ln))
		}
	}
	oz := fmt.Sprintf("%s: kuru çalıştırmada %d onarım adımı (hiçbir şey değiştirilmedi)", c.h.AlanAdi, len(sat))
	if len(sat) == 0 {
		oz = c.h.AlanAdi + ": kuru çalıştırma bu hesap için onarım gerektirmiyor"
	}
	return &cikti{ozet: oz, satirlar: sat}, nil
}

// ── site / DNS / disk ──────────────────────────────────────────────────────

func siteYokla(_ context.Context, c *cagri) (*cikti, error) {
	yol, ok := c.g.Dizge("yol")
	if !ok || yol == "" {
		yol = "/"
	}
	proto, ok := c.g.Dizge("protokol")
	if !ok {
		proto = "https"
	}
	// 🔴 Ana makine YALNIZ DB'deki hesap alan adı; yol katalog deseninden
	// (^/[A-Za-z0-9._~/-]*$, '..' yok) geçti. Bağlantı SSRF kapısından (monitor).
	hedef := proto + "://" + c.h.AlanAdi + yol
	r := c.y.o.yokla(hedef)
	sat := []string{
		"url: " + hedef,
		"erisilebilir: " + deger(r.Erisilebilir),
		"durum_kodu: " + strconv.Itoa(r.DurumKodu),
		fmt.Sprintf("yanit_suresi_ms: %.0f", r.YanitSuresiMs),
	}
	if r.Server != "" {
		sat = append(sat, "sunucu: "+r.Server)
	}
	if r.Hata != "" {
		sat = append(sat, "hata: "+r.Hata)
	}
	if r.SSL != nil {
		sat = append(sat, fmt.Sprintf("ssl: gecerli=%s bitis=%s kalan_gun=%d veren=%s ozne=%s",
			deger(r.SSL.Gecerli), r.SSL.BitisTarihi, r.SSL.KalanGun, r.SSL.Cikaran, r.SSL.OznName))
	}
	oz := fmt.Sprintf("%s → HTTP %d (%.0f ms)", hedef, r.DurumKodu, r.YanitSuresiMs)
	if !r.Erisilebilir {
		oz = hedef + " → erişilemedi"
	}
	return &cikti{ozet: oz, satirlar: sat}, nil
}

func diskKota(ctx context.Context, c *cagri) (*cikti, error) {
	kv, err := c.rota(ctx, c.h, "/kaynak", nil)
	if err != nil {
		return nil, err
	}
	k := nesne(kv)
	sat := sec(k, "", "disk_mb", "inode_kullanim", "inode_limit", "db_mb", "yedek_mb")
	// En büyük dizinler: KİRACI kimliğinde du (yalnız onun erişebildiği), ev köküne göre.
	ev := "/home/" + c.h.SK
	if out, _ := c.y.o.kiraciKomut(ctx, c.h.SK, "du", "-x", "-m", "--max-depth=1", "--", ev); len(out) > 0 {
		type dz struct {
			mb  int64
			yol string
		}
		var dl []dz
		for _, ln := range satirlaraBolHepsi(out) {
			f := strings.SplitN(ln, "\t", 2)
			if len(f) != 2 {
				continue
			}
			n, err := strconv.ParseInt(strings.TrimSpace(f[0]), 10, 64)
			if err != nil || f[1] == ev {
				continue
			}
			dl = append(dl, dz{n, strings.TrimPrefix(f[1], ev+"/")})
		}
		sort.Slice(dl, func(i, j int) bool { return dl[i].mb > dl[j].mb })
		for i, d := range dl {
			if i >= 10 {
				break
			}
			sat = append(sat, fmt.Sprintf("dizin %s: %d MB", d.yol, d.mb))
		}
	}
	disk := nesne(k["disk_mb"])
	oz := fmt.Sprintf("%s: disk %s/%s MB, inode %s/%s (0 = sınırsız)", c.h.AlanAdi, metin(disk, "kullanim"), metin(disk, "limit"),
		metin(k, "inode_kullanim"), metin(k, "inode_limit"))
	return &cikti{ozet: oz, satirlar: sat}, nil
}

// kamuDNS — genel çözümleyiciler (dnsutil ile aynı küme); yetkili = yerel named.
var kamuDNS = []string{"1.1.1.1", "8.8.8.8"}

const yetkiliDNS = "127.0.0.1"

func dnsKontrol(ctx context.Context, c *cagri) (*cikti, error) {
	v, err := c.rota(ctx, c.h, "/dns", nil)
	if err != nil {
		return nil, err
	}
	alan := c.h.AlanAdi
	ad := alan
	if a, ok := c.g.Dizge("alt_ad"); ok && a != "" {
		ad = a + "." + alan
	}
	turler := []string{"A", "AAAA", "MX", "NS"}
	if ad != alan {
		turler = []string{"A", "AAAA", "CNAME", "TXT"}
	}
	if t, ok := c.g.Dizge("kayit_turu"); ok {
		turler = []string{t}
	}
	// Panel bölgesi: ad+tür → normal değerler.
	panel := map[string][]string{}
	for _, e := range dizi(v) {
		m := nesne(e)
		if metin(m, "aktif") == "hayır" {
			continue
		}
		fq := kayitAdi(metin(m, "ad"), alan)
		tur := strings.ToUpper(metin(m, "tip"))
		d := metin(m, "deger")
		if tur == "MX" {
			d = metin(m, "oncelik") + " " + d
		}
		if tur == "CNAME" || tur == "MX" || tur == "NS" {
			if !strings.HasSuffix(d, ".") && !strings.Contains(d, ".") {
				d += "." + alan
			}
		}
		k := fq + " " + tur
		panel[k] = append(panel[k], kayitNormal(d))
	}
	var sat []string
	uyusmaz := 0
	for _, tur := range turler {
		k := ad + " " + tur
		p := tekil(panel[k])
		yet, yerr := c.y.o.dig(ctx, yetkiliDNS, ad, tur)
		var kamu []string
		var kerr error = errors.New("yanıt yok")
		for _, ns := range kamuDNS {
			if kamu, kerr = c.y.o.dig(ctx, ns, ad, tur); kerr == nil {
				break
			}
		}
		durum := "uyumlu"
		switch {
		case kerr != nil:
			durum = "kamu DNS'e ulaşılamadı"
		case tur == "SOA":
			durum = "bilgi"
		case strings.Join(p, ",") != strings.Join(kamu, ","):
			durum = "UYUŞMAZ (panel ≠ kamu)"
			uyusmaz++
		}
		sat = append(sat, fmt.Sprintf("%s %s: panel=[%s] yetkili=[%s] kamu=[%s] → %s", tur, ad,
			strings.Join(p, ", "), digMetin(yet, yerr), strings.Join(kamu, ", "), durum))
	}
	oz := fmt.Sprintf("%s: %d tür denetlendi, %d uyuşmazlık", ad, len(turler), uyusmaz)
	return &cikti{ozet: oz, satirlar: sat}, nil
}

func digMetin(v []string, err error) string {
	if err != nil {
		return "ulaşılamadı"
	}
	return strings.Join(v, ", ")
}

func tekil(v []string) []string {
	g := map[string]bool{}
	var out []string
	for _, s := range v {
		if !g[s] {
			g[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// kayitAdi — bölge kaydı adının tam adı ("@"/"" → alan; "x." → x; "www" → www.alan).
func kayitAdi(ad, alan string) string {
	ad = strings.ToLower(strings.TrimSpace(ad))
	switch {
	case ad == "" || ad == "@" || ad == "-":
		return alan
	case strings.HasSuffix(ad, "."):
		return strings.TrimSuffix(ad, ".")
	case ad == alan || strings.HasSuffix(ad, "."+alan):
		return ad
	}
	return ad + "." + alan
}

// ── denetim ────────────────────────────────────────────────────────────────

func sonDegisiklikler(ctx context.Context, c *cagri) (*cikti, error) {
	gun := int64(7)
	if g, ok := c.g.Tamsayi("gun"); ok {
		gun = g
	}
	musteriAktor := "musteri:" + strconv.FormatInt(c.h.ID, 10)
	q := `SELECT DATE_FORMAT(ts,'%Y-%m-%d %H:%i'), actor_username, action, COALESCE(target,''), ok
	      FROM audit_log
	      WHERE ts >= NOW() - INTERVAL ? DAY AND (target = ? OR target LIKE ? OR adina_domain_id = ? OR actor_username = ?)`
	args := []any{gun, c.h.AlanAdi, "%." + c.h.AlanAdi, c.h.ID, musteriAktor}
	// 🔴 Kapsam: kök dışı YALNIZ hesabın kiracı kapsamındaki kayıtlar (audit_log.reseller_id =
	// domainin sahibi; DenetimDomain böyle yazar). Bayi kapsamı = kendi users.id.
	switch c.k.Rol {
	case protokol.RolBayi:
		q += ` AND reseller_id = ?`
		args = append(args, c.k.KullaniciID)
	case protokol.RolMusteri:
		q += ` AND reseller_id = ?`
		args = append(args, c.h.ResellerID)
	}
	q += ` ORDER BY id DESC LIMIT 100`
	rows, err := c.y.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, hataf(protokol.HataMesgul, "denetim kaydı okunamadı")
	}
	defer rows.Close()
	var sat []string
	for rows.Next() {
		var ts, aktor, eylem, hedef string
		var ok bool
		if err := rows.Scan(&ts, &aktor, &eylem, &hedef, &ok); err != nil {
			continue
		}
		if c.k.Rol == protokol.RolMusteri {
			// Müşteri yönetici/bayi giriş adlarını görmez (domains.musteriGorunumu ilkesi).
			switch {
			case aktor == musteriAktor:
				aktor = "siz (müşteri paneli)"
			case strings.HasPrefix(aktor, "destek-bot"):
			default:
				aktor = "panel yöneticisi"
			}
		}
		sonuc := "başarılı"
		if !ok {
			sonuc = "başarısız"
		}
		sat = append(sat, fmt.Sprintf("%s | %s | %s | %s | %s", ts, aktor, eylem, hedef, sonuc))
	}
	if err := rows.Err(); err != nil {
		return nil, hataf(protokol.HataMesgul, "denetim kaydı okunamadı")
	}
	oz := fmt.Sprintf("%s: son %d günde %d değişiklik", c.h.AlanAdi, gun, len(sat))
	return &cikti{ozet: oz, satirlar: sat, kesildi: len(sat) >= 100}, nil
}
