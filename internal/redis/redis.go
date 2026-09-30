// Package redis: per-tenant izole Valkey/Redis cache yönetimi.
// Tek Valkey instance + her domain'e ACL user (~<sk>:* key-prefix + @dangerous/@admin reddedilir).
// Böylece siteler birbirinin cache'ini göremez. ACL yönetimi valkey-cli ile (ek Go bağımlılığı yok).
package redis

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"

	"girginospanel/internal/gizli"
	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

type Handlers struct{ DB *sql.DB }

// sistem_kullanici güvenli karakter seti (valkey-cli arg enjeksiyonu önlemi)
var reSK = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)

const (
	redisHost = "127.0.0.1"
	redisPort = 6379
)

// adminParola — Valkey yönetici parolası. config.Load onu (PANEL_REDIS_ADMIN_PASS)
// okuyup süreç ortamından SİLER; main AdminParolaAyarla ile buraya verir.
// Ortamdan okunmaz: silinmiş olur ve ortamda kalsaydı her çocuk süreç miras alırdı.
var adminParola atomic.Value // string

// AdminParolaAyarla: başlangıçta config'ten okunan yönetici parolasını verir.
func AdminParolaAyarla(p string) { adminParola.Store(p) }

func adminPass() string {
	p, _ := adminParola.Load().(string)
	return p
}

// kiraciCalistir: argv'yi kiracı (sk) kimliğinde, panel ortamı OLMADAN (yalnız
// PATH/HOME/LANG) çalıştırır; birleşik çıktı döner. Kiracının dosyalarına dokunan
// her komut (wp-cli, cp, rm) bundan geçer. (Hotfix: entegrasyondaki kiraci.Komut'un
// eşdeğeri; main'de kiraci paketi yok.)
func kiraciCalistir(sk string, argv ...string) ([]byte, error) {
	if len(argv) == 0 || argv[0] == "" {
		return nil, errKiraciKomut
	}
	if u, err := user.Lookup(sk); err != nil || u.Uid == "0" {
		return nil, errKiraciKomut
	}
	cmd := exec.Command("runuser", append([]string{"-u", sk, "--"}, argv...)...)
	cmd.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/home/" + sk,
		"LANG=C",
	}
	cmd.Dir = "/"
	return cmd.CombinedOutput()
}

var errKiraciKomut = errors.New("kiracı komutu: geçersiz kullanıcı ya da boş komut")

// cli: valkey-cli'yi admin parolasıyla çalıştırır (parola argv'de değil, REDISCLI_AUTH env'de).
func cli(args ...string) (string, error) {
	cmd := exec.Command("valkey-cli", args...)
	cmd.Env = append(os.Environ(), "REDISCLI_AUTH="+adminPass())
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func genPass() string {
	b := make([]byte, 18)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// enableUser: tenant ACL — sadece <sk>:* key/channel; tehlikeli & admin komutları reddedilir.
// İzolasyon KORUNUR (flushall/flushdb/keys/config/swapdb → NOPERM), ANCAK WP Redis Object Cache
// eklentisinin ihtiyaç duyduğu read-only diagnostik komutları geri açılır (info/dbsize sadece
// toplam istatistik sızdırır, başka tenant'ın key'lerini DEĞİL — shared cache için kabul edilebilir).
func enableUser(sk, pass string) error {
	if _, err := cli("ACL", "SETUSER", sk, "on", ">"+pass,
		"resetkeys", "~"+sk+":*", "resetchannels", "&"+sk+":*",
		"+@all", "-@dangerous", "-@admin", "-scan", "-randomkey",
		"+info", "+dbsize", "+command", "+ping", "+echo", "+client|no-evict"); err != nil {
		return err
	}
	_, err := cli("ACL", "SAVE")
	return err
}

func disableUser(sk string) {
	_, _ = cli("ACL", "DELUSER", sk)
	_, _ = cli("ACL", "SAVE")
}

// ---- WordPress otomatik bağlama (wp-cli, domain kullanıcısı olarak) ----

const wpBin = "/usr/local/bin/wp"

// wpKomut: wp-cli'yi kiracı kimliğinde çalıştırır. wp-cli kiracının wp-config.php'sini
// ve eklentilerini YÜKLER (kiracı PHP'si) → panel ortamı ASLA verilmez (kiraciCalistir).
func wpKomut(sk string, args ...string) ([]byte, error) {
	return kiraciCalistir(sk, append([]string{"/usr/bin/php", "-d", "memory_limit=512M", wpBin}, args...)...)
}

// wpDizinler: <sk>/public_html + bir seviye alt dizinlerde wp-config.php olan WP kurulumları.
func wpDizinler(sk string) []string {
	root := "/home/" + sk + "/public_html"
	adaylar := []string{root}
	if entries, err := os.ReadDir(root); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				adaylar = append(adaylar, filepath.Join(root, e.Name()))
			}
		}
	}
	var out []string
	for _, d := range adaylar {
		if _, err := os.Stat(filepath.Join(d, "wp-config.php")); err == nil {
			out = append(out, d)
		}
	}
	return out
}

// wpBagla: WP kurulumlarına Redis'i otomatik bağlar. Bağlanan kurulum sayısını döner (best-effort).
// NOT: `wp redis enable` KULLANILMAZ — o komut Predis+FLUSHDB çağırıyor, ACL'imiz (haklı olarak)
// flushdb'yi reddediyor. Bunun yerine drop-in ELLE kopyalanır; ACL auth için WP_REDIS_PASSWORD
// dizi formatında [kullanıcı, parola] verilir; selective flush ile runtime flush = scan+unlink.
func wpBagla(sk, pass string) int {
	n := 0
	for _, dir := range wpDizinler(sk) {
		cfg := filepath.Join(dir, "wp-config.php")
		bak := cfg + ".gosp-redis-bak"
		// 🔴 VERİ-KAYBI GUARD: `wp config set` wp-config'i truncate+rewrite eder;
		// disk/kota dolunca 0 bayta düşer. Önce yedekle, işlem sonunda boş/bozuk
		// kaldıysa yedekten geri yükle (aşağıda).
		_, _ = kiraciCalistir(sk, "cp", "-p", cfg, bak)
		set := func(k, v string, raw bool) {
			a := []string{"config", "set", k, v, "--type=constant", "--path=" + dir}
			if raw {
				a = append(a, "--raw")
			}
			_, _ = wpKomut(sk, a...)
		}
		set("WP_REDIS_HOST", redisHost, false)
		set("WP_REDIS_PORT", strconv.Itoa(redisPort), true)
		// ACL auth: WP_REDIS_PASSWORD = array('<sk>', '<pass>')  (drop-in bu formatta ACL yapar)
		set("WP_REDIS_PASSWORD", "array('"+sk+"','"+pass+"')", true)
		set("WP_REDIS_PREFIX", sk+":", false)
		set("WP_REDIS_SELECTIVE_FLUSH", "true", true)
		set("WP_REDIS_CLIENT", "phpredis", false)
		set("WP_CACHE", "true", true)
		// eski (yanlış) tek-string USERNAME/PASSWORD kalıntısı varsa temizle
		_, _ = wpKomut(sk, "config", "delete", "WP_REDIS_USERNAME", "--path="+dir)

		if _, err := wpKomut(sk, "plugin", "install", "redis-cache", "--activate", "--path="+dir); err != nil {
			continue
		}
		// drop-in'i elle kur (wp redis enable'ın flushdb'sini atla)
		src := filepath.Join(dir, "wp-content/plugins/redis-cache/includes/object-cache.php")
		dst := filepath.Join(dir, "wp-content/object-cache.php")
		if _, err := kiraciCalistir(sk, "cp", "-f", src, dst); err != nil {
			continue
		}
		// 🔴 GUARD DEVAMI: wp-config boş/okunamaz kaldıysa (disk dolu → truncate)
		// yedekten geri yükle. Başarılıysa yedeği sil.
		if st, e := os.Stat(cfg); e != nil || st.Size() == 0 {
			_, _ = kiraciCalistir(sk, "cp", "-p", bak, cfg)
			_, _ = kiraciCalistir(sk, "rm", "-f", bak)
			continue // bu kurulum başarısız — wp-config kurtarıldı, Redis bağlanmadı
		}
		_, _ = kiraciCalistir(sk, "rm", "-f", bak)
		// bağlantı doğrula — status "Connected" içeriyorsa başarılı say
		if out, err := wpKomut(sk, "redis", "status", "--path="+dir); err == nil && strings.Contains(string(out), "Connected") {
			n++
		}
	}
	return n
}

// wpCozdur: WP kurulumlarında Redis'i kapatır — drop-in'i kaldırır + sabitleri siler.
// `wp redis disable` KULLANILMAZ (o da flushdb deneyebilir); drop-in elle silinir.
func wpCozdur(sk string) {
	for _, dir := range wpDizinler(sk) {
		_, _ = kiraciCalistir(sk, "rm", "-f",
			filepath.Join(dir, "wp-content/object-cache.php"))
		for _, k := range []string{"WP_REDIS_HOST", "WP_REDIS_PORT", "WP_REDIS_USERNAME",
			"WP_REDIS_PASSWORD", "WP_REDIS_PREFIX", "WP_REDIS_SELECTIVE_FLUSH", "WP_REDIS_CLIENT", "WP_CACHE"} {
			_, _ = wpKomut(sk, "config", "delete", k, "--path="+dir)
		}
	}
}

type durumResp struct {
	Aktif      bool   `json:"aktif"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Kullanici  string `json:"kullanici"`
	Parola     string `json:"parola,omitempty"`
	Prefix     string `json:"prefix"`
	WPSnippet  string `json:"wp_snippet,omitempty"`
	WPBaglandi int    `json:"wp_baglandi,omitempty"`
}

func (h *Handlers) domainSK(r *http.Request) (id int64, sk string, ok bool) {
	id, _ = strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici FROM domains WHERE id=?`, id).Scan(&sk); err != nil {
		return id, "", false
	}
	return id, sk, reSK.MatchString(sk)
}

func wpSnippet(sk, pass string) string {
	return "// GirginOSPanel Redis object cache\n" +
		"define( 'WP_REDIS_HOST', '" + redisHost + "' );\n" +
		"define( 'WP_REDIS_PORT', " + strconv.Itoa(redisPort) + " );\n" +
		"define( 'WP_REDIS_PASSWORD', array( '" + sk + "', '" + pass + "' ) );\n" +
		"define( 'WP_REDIS_PREFIX', '" + sk + ":' );\n" +
		"define( 'WP_REDIS_SELECTIVE_FLUSH', true );\n" +
		"define( 'WP_REDIS_CLIENT', 'phpredis' );\n" +
		"define( 'WP_CACHE', true );"
}

// GET /domains/{id}/redis
func (h *Handlers) Durum(w http.ResponseWriter, r *http.Request) {
	id, sk, ok := h.domainSK(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	var aktif int
	var rowSK, pass string
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT aktif, sk, redis_pass FROM cp_domain_redis WHERE domain_id=?`, id).Scan(&aktif, &rowSK, &pass)
	if err != nil || aktif == 0 {
		httpx.WriteJSON(w, http.StatusOK, durumResp{Aktif: false, Host: redisHost, Port: redisPort, Kullanici: sk, Prefix: sk + ":"})
		return
	}
	// 🔴 CWE-200: parola + WP snippet DURUM yanitinda DONMEZ; sahip
	// /domains/{id}/redis/parola ucundan alir. (rowSK/pass yalniz aktiflik icin okundu.)
	_ = rowSK
	_ = pass
	httpx.WriteJSON(w, http.StatusOK, durumResp{
		Aktif: true, Host: redisHost, Port: redisPort, Kullanici: sk,
		Prefix: sk + ":",
	})
}

// POST /domains/{id}/redis — tenant cache'i etkinleştir
func (h *Handlers) Ac(w http.ResponseWriter, r *http.Request) {
	id, sk, ok := h.domainSK(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if adminPass() == "" {
		httpx.WriteError(w, http.StatusServiceUnavailable, "Redis yapılandırılmamış (PANEL_REDIS_ADMIN_PASS yok)")
		return
	}
	pass := genPass()
	if err := enableUser(sk, pass); err != nil {
		log.Printf("redis: ACL oluşturulamadı (sk=%s): %v", sk, err)
		httpx.WriteError(w, http.StatusInternalServerError, "Redis ACL oluşturulamadı")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO cp_domain_redis (domain_id, sk, redis_pass, aktif) VALUES (?,?,?,1)
		 ON DUPLICATE KEY UPDATE sk=VALUES(sk), redis_pass=VALUES(redis_pass), aktif=1`,
		id, sk, gizli.SaklaBagli(pass, sk)); err != nil {
		disableUser(sk) // DB başarısızsa ACL'i geri al
		log.Printf("redis: kayıt yazılamadı (sk=%s): %v", sk, err)
		httpx.WriteError(w, http.StatusInternalServerError, "kaydedilemedi")
		return
	}
	// WordPress kurulumları varsa otomatik bağla (best-effort — WP yoksa snippet elle kalır)
	baglandi := wpBagla(sk, pass)
	// 🔴 CWE-200: yeni parola + WP snippet AC yanitinda DONMEZ; sahip
	// hemen /domains/{id}/redis/parola ucundan alir (baglandi bilgisi kalir).
	httpx.WriteJSON(w, http.StatusOK, durumResp{
		Aktif: true, Host: redisHost, Port: redisPort, Kullanici: sk,
		Prefix: sk + ":", WPBaglandi: baglandi,
		Parola: pass, WPSnippet: wpSnippet(sk, pass), // tek-seferlik: aktifleştirmede gösterilir
	})
}

// DELETE /domains/{id}/redis — tenant cache'i kapat
func (h *Handlers) Kapat(w http.ResponseWriter, r *http.Request) {
	id, sk, ok := h.domainSK(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	wpCozdur(sk) // önce WP'de kapat (creds hâlâ geçerliyken drop-in kaldırılır)
	disableUser(sk)
	if _, err := h.DB.ExecContext(r.Context(), `DELETE FROM cp_domain_redis WHERE domain_id=?`, id); err != nil {
		log.Printf("redis.Kapat: DB kaydı silinemedi (domain=%d): %v — cache kapatıldı ama kayıt orphan", id, err)
		httpx.WriteError(w, http.StatusInternalServerError, "redis kaydı silinemedi: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// KapatDomain: domain SİLİNİRKEN çağrılır (domains.Delete). WP drop-in kaldır +
// Valkey ACL user sil + cp_domain_redis satırını temizle. cp_domain_redis'te
// ON DELETE CASCADE FK olmadığı için domain silinince satır orphan kalıyordu.
func KapatDomain(db *sql.DB, id int64, sk string) {
	wpCozdur(sk)
	disableUser(sk)
	if _, err := db.Exec(`DELETE FROM cp_domain_redis WHERE domain_id=?`, id); err != nil {
		log.Printf("redis.KapatDomain: orphan temizlenemedi (domain=%d): %v", id, err)
	}
}

// HealScanAcl: mevcut TUM kiraci ACL kullanicilarina "-scan -randomkey" ekler.
// SETUSER birlestirmeli calisir → parola/anahtar deseni korunur, yalnizca bu iki
// komut geri alinir. Panel acilisinda cagrilir; eski surumden gelen kiracilar da
// komsu key-ismi enumerasyonuna karsi kapatilir.
func HealScanAcl() {
	out, err := cli("ACL", "LIST")
	if err != nil {
		return
	}
	n := 0
	for _, satir := range strings.Split(out, "\n") {
		f := strings.Fields(satir)
		if len(f) < 2 || f[0] != "user" {
			continue
		}
		kul := f[1]
		if kul == "default" || !strings.HasPrefix(kul, "c_") {
			continue
		}
		if _, e := cli("ACL", "SETUSER", kul, "-scan", "-randomkey"); e == nil {
			n++
		}
	}
	if n > 0 {
		_, _ = cli("ACL", "SAVE")
		log.Printf("redis: %d kiraci ACL kullanicisina -scan/-randomkey uygulandi", n)
	}
}
