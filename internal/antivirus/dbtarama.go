package antivirus

// WordPress veritabanı tarayıcısı (Imunify Database Scanner muadili) + domain
// itibar/kara-liste kontrolü. wp-config.php'den DB kimliğini okur, DB'ye
// bağlanır ve wp_options (autoload) + wp_posts'ta enjekte zararlı içerik arar.
//
// 🔴 NEDEN: dosya tarayıcı yalnız DİSKTEKİ dosyalara bakar; birçok enjeksiyon
// (spam link, malicious redirect, base64 payload) yalnız VERİTABANINDA yaşar
// (wp_options autoloaded değer, injected post içeriği). Disk taraması bunları
// GÖRMEZ.

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"girginospanel/internal/bildirim"
	"girginospanel/internal/httpx"
)

var (
	reWPName = regexp.MustCompile(`define\(\s*['"]DB_NAME['"]\s*,\s*['"]([^'"]*)['"]`)
	reWPUser = regexp.MustCompile(`define\(\s*['"]DB_USER['"]\s*,\s*['"]([^'"]*)['"]`)
	reWPPass = regexp.MustCompile(`define\(\s*['"]DB_PASSWORD['"]\s*,\s*['"]([^'"]*)['"]`)
	reWPHost = regexp.MustCompile(`define\(\s*['"]DB_HOST['"]\s*,\s*['"]([^'"]*)['"]`)
	reWPPre  = regexp.MustCompile(`\$table_prefix\s*=\s*['"]([^'"]*)['"]`)

	// DB değerlerinde aranan zararlı örüntüler (dosya motoruyla tutarlı sinyaller).
	reDBZararli = regexp.MustCompile(`(?i)(eval\s*\(|base64_decode\s*\(|gzinflate\s*\(|gzuncompress\s*\(|str_rot13\s*\(|\bassert\s*\(|create_function\s*\(|preg_replace\s*\(\s*['"][^'"]*/e|move_uploaded_file|FilesMan|\bshell_exec\s*\(|\bsystem\s*\(|<script[^>]*>[^<]{0,200}?(eval|unescape|fromCharCode|atob)\s*\()`)

	// L1: tablo oneki yalniz [A-Za-z0-9_] (WordPress sanitize_key). Backtick
	// identifier-quote breakout'u onler.
	reDBPrefix = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
	// L2: DNS sorgusu oncesi domain label charset.
	reHostAd = regexp.MustCompile(`^[a-z0-9.-]+$`)
	// H1: wp_posts DUZ METIN (blog); reDBZararli prose "eval()" gibi FP uretir.
	// Post icin STRICT: gercek enjeksiyon vektoru sart. Prose ESLESMEZ.
	reDBPost = regexp.MustCompile(`(?i)(<\?php|<script[^>]*>[^<]{0,600}?(eval\s*\(|unescape\s*\(|String\.fromCharCode\s*\(|document\.write\s*\(\s*unescape)|base64_decode\s*\([^)]{16,}|\beval\s*\(\s*(base64_decode|gzinflate|str_rot13|gzuncompress|\$_|\$GLOBALS)|\bassert\s*\(\s*(\$_|base64_decode))`)
)

type dbBulgu struct {
	Tablo string
	Ad    string
	Satir int64
}

type wpKimlik struct{ ad, kul, pw, host, pre, yol string }

// wpConfigOku — wp-config.php'den DB kimliği + tablo öneki.
func wpConfigOku(yol string) (wpKimlik, bool) {
	b, err := os.ReadFile(yol)
	if err != nil {
		return wpKimlik{}, false
	}
	al := func(re *regexp.Regexp) string {
		if m := re.FindSubmatch(b); len(m) == 2 {
			return string(m[1])
		}
		return ""
	}
	k := wpKimlik{ad: al(reWPName), kul: al(reWPUser), pw: al(reWPPass), host: al(reWPHost), pre: al(reWPPre), yol: yol}
	if k.pre == "" {
		k.pre = "wp_"
	}
	if k.ad == "" || k.kul == "" {
		return k, false
	}
	return k, true
}

// wpKurulumlariBul — sk'nin ev dizininde wp-config.php arar (public_html + 1 alt).
func wpKurulumlariBul(sk string) []string {
	var out []string
	kok := "/home/" + sk + "/public_html"
	if _, err := os.Stat(filepath.Join(kok, "wp-config.php")); err == nil {
		out = append(out, filepath.Join(kok, "wp-config.php"))
	}
	if ents, err := os.ReadDir(kok); err == nil {
		for _, e := range ents {
			if e.IsDir() {
				c := filepath.Join(kok, e.Name(), "wp-config.php")
				if _, err := os.Stat(c); err == nil {
					out = append(out, c)
				}
			}
		}
	}
	return out
}

func mysqlSoket() string {
	for _, p := range []string{"/var/lib/mysql/mysql.sock", "/var/run/mysqld/mysqld.sock", "/run/mysqld/mysqld.sock", "/tmp/mysql.sock"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// dsnKur — 🔴 WP DB kullanicisi genelde YALNIZ 'localhost' (unix socket) icin
// yetkilidir, 127.0.0.1 (TCP) icin DEGIL. PHP de socket kullanir. Bu yuzden
// localhost'ta TCP'ye dusmek "access denied" verir; socket'e baglaniriz.
func dsnKur(k wpKimlik) string {
	const base = "?timeout=5s&readTimeout=8s&charset=utf8mb4"
	host := strings.TrimSpace(k.host)
	// DB_HOST='localhost:/path/to.sock' bicimi
	if i := strings.Index(host, ":/"); i >= 0 {
		return k.kul + ":" + k.pw + "@unix(" + host[i+1:] + ")/" + k.ad + base
	}
	if host == "" || host == "localhost" || strings.HasPrefix(host, "localhost:") {
		if sk := mysqlSoket(); sk != "" {
			return k.kul + ":" + k.pw + "@unix(" + sk + ")/" + k.ad + base
		}
		host = "127.0.0.1:3306"
	}
	if !strings.Contains(host, ":") {
		host += ":3306"
	}
	return k.kul + ":" + k.pw + "@tcp(" + host + ")/" + k.ad + base
}

// dbTaraKurulum — tek WP kurulumunun DB'sini tarar, BULGULARI DONER (yazmaz).
// Cagiran domain-bazli delete+insert yapar (H2).
func (h *Handlers) dbTaraKurulum(ctx context.Context, k wpKimlik) ([]dbBulgu, error) {
	if !reDBPrefix.MatchString(k.pre) {
		return nil, fmt.Errorf("gecersiz tablo oneki: %q", k.pre)
	}
	sdb, err := sql.Open("mysql", dsnKur(k))
	if err != nil {
		return nil, err
	}
	defer sdb.Close()
	sdb.SetMaxOpenConns(2)
	if err := sdb.PingContext(ctx); err != nil {
		return nil, err
	}
	var out []dbBulgu
	// wp_options: autoload serialized config; reDBZararli (dusuk FP).
	if rows, err := sdb.QueryContext(ctx,
		"SELECT option_id, option_name, option_value FROM `"+k.pre+"options` WHERE autoload IN ('yes','on','auto') LIMIT 5000"); err == nil {
		for rows.Next() {
			var id int64
			var ad, deger string
			if rows.Scan(&id, &ad, &deger) != nil {
				continue
			}
			if reDBZararli.MatchString(deger) {
				out = append(out, dbBulgu{k.pre + "options", ad, id})
			}
		}
		rows.Close()
	}
	// wp_posts: DUZ METIN blog; reDBPost STRICT (prose FP uretmez).
	if rows, err := sdb.QueryContext(ctx,
		"SELECT ID, post_content FROM `"+k.pre+"posts` WHERE post_status IN ('publish','draft','private') "+
			"AND (post_content LIKE '%base64_decode%' OR post_content LIKE '%<script%' OR post_content LIKE '%<?php%' OR post_content LIKE '%eval(%') LIMIT 5000"); err == nil {
		for rows.Next() {
			var id int64
			var icerik string
			if rows.Scan(&id, &icerik) != nil {
				continue
			}
			if reDBPost.MatchString(icerik) {
				out = append(out, dbBulgu{k.pre + "posts", "post", id})
			}
		}
		rows.Close()
	}
	return out, nil
}

// dbBulguYaz — DB bulgusunu av_bulgular'a yazar (motor=gosp/db).
func (h *Handlers) dbBulguYaz(domID int64, tablo, ad string, satirID int64) {
	yer := tablo + " #" + itoa64(satirID)
	if ad != "" && ad != "post" {
		yer += " (" + ad + ")"
	}
	_, _ = h.DB.Exec(
		`INSERT INTO av_bulgular (tarama_id, domain_id, dosya, imza, motor, seviye, puan, durum, orijinal_yol, karantina_yol)
		 VALUES (0,?,?,?,?,?,?,?,?,?)`,
		domID, "DB: "+yer, "GOSP-DB-ZARARLI", "gosp/db", "kritik", 100, "aktif", "", "")
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// AdminDBTara — POST /antivirus/db-tara: tüm WP kurulumlarının DB'sini tarar.
func (h *Handlers) AdminDBTara(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Minute)
	defer cancel()

	rows, err := h.DB.QueryContext(ctx, `SELECT id, sistem_kullanici FROM domains WHERE sistem_kullanici LIKE 'c_%'`)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type dm struct {
		id int64
		sk string
	}
	var dl []dm
	for rows.Next() {
		var d dm
		if rows.Scan(&d.id, &d.sk) == nil {
			dl = append(dl, d)
		}
	}
	rows.Close()

	tarananKurulum, toplamBulgu, hataliKurulum := 0, 0, 0
	for _, d := range dl {
		var found []dbBulgu
		hata, tarandi := false, false
		for _, cfg := range wpKurulumlariBul(d.sk) {
			k, ok := wpConfigOku(cfg)
			if !ok {
				continue
			}
			tarananKurulum++
			f, err := h.dbTaraKurulum(ctx, k)
			if err != nil {
				hata = true
				continue
			}
			tarandi = true
			found = append(found, f...)
		}
		// 🔴 H2: YALNIZ tam basarili taranan domain tazelenir. Erisilemeyen DB'nin
		// ONCEKI gercek bulgusu SILINMEZ (basarisizlik guven olarak render
		// olmasin). Global delete YOK → 0-penceresi de yok.
		if tarandi && !hata {
			_, _ = h.DB.Exec(`DELETE FROM av_bulgular WHERE motor='gosp/db' AND durum='aktif' AND domain_id=?`, d.id)
			for _, b := range found {
				h.dbBulguYaz(d.id, b.Tablo, b.Ad, b.Satir)
			}
			toplamBulgu += len(found)
		}
		if hata {
			hataliKurulum++
		}
	}
	if toplamBulgu > 0 {
		bildirim.Yaz(h.DB, "kritik", "antivirus", "Veritabanında zararlı içerik",
			"DB taraması "+itoa64(int64(toplamBulgu))+" zararlı kayıt buldu.", 0, "av_db", 0)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "taranan_kurulum": tarananKurulum, "bulunan": toplamBulgu, "hatali_kurulum": hataliKurulum,
	})
}

// ─── Kara-liste / itibar (Spamhaus DBL) ───

// AdminKaraListe — GET /antivirus/kara-liste: her domain için Spamhaus DBL kontrolü.
func (h *Handlers) AdminKaraListe(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(), `SELECT id, alan_adi FROM domains WHERE alan_adi<>'' ORDER BY alan_adi LIMIT 500`)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type dm struct {
		id  int64
		ad  string
	}
	var dl []dm
	for rows.Next() {
		var d dm
		if rows.Scan(&d.id, &d.ad) == nil {
			dl = append(dl, d)
		}
	}
	rows.Close()

	type sonuc struct {
		DomainID int64  `json:"domain_id"`
		Alan     string `json:"alan_adi"`
		Durum    string `json:"durum"` // temiz | listeli | kontrol_edilemedi
		Kaynak   string `json:"kaynak"`
	}
	out := make([]sonuc, 0, len(dl))
	rez := &net.Resolver{}
	for _, d := range dl {
		s := sonuc{DomainID: d.id, Alan: d.ad, Durum: "temiz", Kaynak: "Spamhaus DBL"}
		ad := dnsAd(d.ad)
		if !reHostAd.MatchString(ad) {
			s.Durum = "kontrol_edilemedi"
			out = append(out, s)
			continue
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		ips, err := rez.LookupHost(ctx, ad+".dbl.spamhaus.org")
		cancel()
		if err == nil && len(ips) > 0 {
			listeli := false
			for _, ip := range ips {
				if strings.HasPrefix(ip, "127.0.1.") {
					listeli = true
				} else if strings.HasPrefix(ip, "127.255.255.") {
					s.Durum = "kontrol_edilemedi" // resolver bloklu/limit
				}
			}
			if listeli {
				s.Durum = "listeli"
			}
		}
		out = append(out, s)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"kayitlar": out})
}

// dnsAd — alan adının www. önekini kırpar (DBL apex sorgular).
func dnsAd(a string) string {
	a = strings.ToLower(strings.TrimSpace(a))
	a = strings.TrimPrefix(a, "www.")
	return a
}
