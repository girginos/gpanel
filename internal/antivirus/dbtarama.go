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
)

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

// dbTaraKurulum — tek WP kurulumunun DB'sini tarar, bulgu sayısı döner.
func (h *Handlers) dbTaraKurulum(ctx context.Context, domID int64, k wpKimlik) (int, error) {
	sdb, err := sql.Open("mysql", dsnKur(k))
	if err != nil {
		return 0, err
	}
	defer sdb.Close()
	sdb.SetMaxOpenConns(2)
	if err := sdb.PingContext(ctx); err != nil {
		return 0, err
	}
	bulunan := 0
	// wp_options: autoload='yes' — her istekte yüklenir, en tehlikeli enjeksiyon yeri.
	if rows, err := sdb.QueryContext(ctx,
		"SELECT option_id, option_name, option_value FROM `"+k.pre+"options` WHERE autoload IN ('yes','on','auto') LIMIT 5000"); err == nil {
		for rows.Next() {
			var id int64
			var ad, deger string
			if rows.Scan(&id, &ad, &deger) != nil {
				continue
			}
			if reDBZararli.MatchString(deger) {
				h.dbBulguYaz(domID, k.pre+"options", ad, id)
				bulunan++
			}
		}
		rows.Close()
	}
	// wp_posts: yayın/taslak içeriğinde enjekte script/eval.
	if rows, err := sdb.QueryContext(ctx,
		"SELECT ID, post_content FROM `"+k.pre+"posts` WHERE post_status IN ('publish','draft','private') "+
			"AND (post_content LIKE '%base64_decode%' OR post_content LIKE '%<script%' OR post_content LIKE '%eval(%' OR post_content LIKE '%gzinflate%') LIMIT 5000"); err == nil {
		for rows.Next() {
			var id int64
			var icerik string
			if rows.Scan(&id, &icerik) != nil {
				continue
			}
			if reDBZararli.MatchString(icerik) {
				h.dbBulguYaz(domID, k.pre+"posts", "post", id)
				bulunan++
			}
		}
		rows.Close()
	}
	return bulunan, nil
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

	// Önceki DB bulgularını (aktif) temizle — taze durum.
	_, _ = h.DB.Exec(`DELETE FROM av_bulgular WHERE motor='gosp/db' AND durum='aktif'`)

	tarananKurulum, toplamBulgu, hataliKurulum := 0, 0, 0
	for _, d := range dl {
		for _, cfg := range wpKurulumlariBul(d.sk) {
			k, ok := wpConfigOku(cfg)
			if !ok {
				continue
			}
			tarananKurulum++
			n, err := h.dbTaraKurulum(ctx, d.id, k)
			if err != nil {
				// 🔴 Baglanamayan DB'yi 'temiz' sayma (basarisizlik guven
				// olarak render). Hatali olarak raporla.
				hataliKurulum++
				continue
			}
			toplamBulgu += n
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
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		ips, err := rez.LookupHost(ctx, dnsAd(d.ad)+".dbl.spamhaus.org")
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
