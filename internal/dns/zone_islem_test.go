package dns

// zone_islem_test.go — DNS yazma uçları: kısmi güncelleme, sahiplik (404), tür bazlı
// doğrulama, zone yazımı hatasında DB geri alma ve denetim kaydı.
//
// Gerçek MariaDB gerekir; GOSP_TEST_GECICI_DSN yoksa ATLANIR (go_test db=true).
// Zone yazımı zoneUygula dikişiyle SAHTELENİR: /var/named, named ve rndc'ye dokunulmaz.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

// zi_db — domains(42 bayialan.test bayi 7, 43 baska.test bayi 9) + dns_records + dns_soa
// + dns_template(+meta) + audit_log. Şema gerçek göç dosyalarından.
func zi_db(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li test atlandı")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("DSN çözülemedi: %v", err)
	}
	cfg.DBName = ""
	kok, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	ad := fmt.Sprintf("gosp_test_dnsislem_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		_ = kok.Close()
		t.Fatalf("geçici veritabanı oluşturulamadı: %v", err)
	}
	t.Cleanup(func() {
		_, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad)
		_ = kok.Close()
	})
	cfg.DBName = ad
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	dk_exec(t, db, `CREATE TABLE domains (
		id BIGINT UNSIGNED PRIMARY KEY, alan_adi VARCHAR(253) NOT NULL UNIQUE,
		is_demo TINYINT(1) NOT NULL DEFAULT 0, ipv4 VARCHAR(45) NOT NULL DEFAULT '',
		reseller_id BIGINT NOT NULL DEFAULT 0) ENGINE=InnoDB`)
	dk_goc(t, db, "0007_dns.sql")
	dk_goc(t, db, "0025_dns_soa.sql")
	dk_goc(t, db, "0027_dns_template.sql")
	dk_goc(t, db, "0032_dns_dnssec.sql")
	dk_exec(t, db, `ALTER TABLE dns_template ADD COLUMN IF NOT EXISTS reseller_id BIGINT NOT NULL DEFAULT 0`)
	dk_exec(t, db, `CREATE TABLE audit_log (
		id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, ts TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		actor_user_id BIGINT UNSIGNED NULL, actor_username VARCHAR(64) NOT NULL DEFAULT '',
		adina_domain_id BIGINT UNSIGNED NULL DEFAULT NULL, ip VARCHAR(45) NOT NULL DEFAULT '',
		action VARCHAR(64) NOT NULL, target VARCHAR(255) NOT NULL DEFAULT '', detail JSON NULL,
		ok TINYINT(1) NOT NULL DEFAULT 1, reseller_id BIGINT UNSIGNED NOT NULL DEFAULT 0) ENGINE=InnoDB`)
	dk_exec(t, db, `INSERT INTO domains (id, alan_adi, ipv4, reseller_id) VALUES
		(42, 'bayialan.test', '203.0.113.1', 7), (43, 'baska.test', '203.0.113.2', 9)`)
	// DKIM yok: EnsureDKIM (/etc/opendkim yazar) bu testlerde ÇAĞRILMAZ.
	dk_exec(t, db, `UPDATE dns_template_meta SET dkim_aktif=0 WHERE id=1`)
	return db
}

func zi_ekle(t *testing.T, db *sql.DB, domainID int64, ad, tip, deger string, ttl, oncelik, aktif int) int64 {
	t.Helper()
	res, err := db.Exec(`INSERT INTO dns_records(domain_id, ad, tip, deger, ttl, oncelik, aktif) VALUES(?,?,?,?,?,?,?)`,
		domainID, ad, tip, deger, ttl, oncelik, aktif)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

// zi_kayit — DB'deki kaydı okur; yoksa (nil, false).
func zi_kayit(t *testing.T, db *sql.DB, rid int64) (Kayit, bool) {
	t.Helper()
	k, err := scan(db.QueryRow(selectAll+" WHERE id=?", rid))
	if errors.Is(err, sql.ErrNoRows) {
		return Kayit{}, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return k, true
}

// zi_durum — domainin DNS durumunun parmak izi (kayıtlar + SOA satırı + DNSSEC bayrağı).
func zi_durum(t *testing.T, db *sql.DB, domainID int64) string {
	t.Helper()
	var sb strings.Builder
	rows, err := db.Query(`SELECT id, ad, tip, deger, ttl, oncelik, aktif FROM dns_records WHERE domain_id=? ORDER BY id`, domainID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var ad, tip, deger string
		var ttl, onc, ak int
		if err := rows.Scan(&id, &ad, &tip, &deger, &ttl, &onc, &ak); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&sb, "%d|%s|%s|%s|%d|%d|%d\n", id, ad, tip, deger, ttl, onc, ak)
	}
	fmt.Fprintf(&sb, "soa=%d dnssec=%d", dk_sayi(t, db, `SELECT COUNT(*) FROM dns_soa WHERE domain_id=?`, domainID),
		dk_sayi(t, db, `SELECT dnssec_aktif FROM domains WHERE id=?`, domainID))
	return sb.String()
}

func zi_cagir(t *testing.T, db *sql.DB, rol, yontem, desen, yol, govde string, isle func(*Handlers) http.HandlerFunc) (int, string) {
	t.Helper()
	return zh_cagir(t, db, zh_tokenlar(t)[rol], yontem, desen, yol, "application/json", govde, isle)
}

func zi_json(t *testing.T, govde string) Kayit {
	t.Helper()
	var k Kayit
	if err := json.Unmarshal([]byte(govde), &k); err != nil {
		t.Fatalf("yanıt çözülemedi: %v (%s)", err, govde)
	}
	return k
}

const (
	ziKayitDesen = "/domains/{id}/dns/{rid}"
	ziListeDesen = "/domains/{id}/dns"
)

func zi_put(t *testing.T, db *sql.DB, rid, govde string) (int, string) {
	t.Helper()
	return zi_cagir(t, db, "admin", http.MethodPut, ziKayitDesen, "/domains/42/dns/"+rid, govde,
		func(h *Handlers) http.HandlerFunc { return h.Update })
}

// ---------------------------------------------------------------- 1) KISMİ GÜNCELLEME

func TestUpdate_KismiGuncelleme_GovdedeOlmayanAlanlariKorur(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	type bas struct {
		ad, tip, deger      string
		ttl, oncelik, aktif int
	}
	www := bas{"www", "A", "203.0.113.5", 600, 0, 1}
	pasifA := bas{"eski", "A", "203.0.113.6", 300, 0, 0}
	mx := bas{"@", "MX", "mail.bayialan.test", 900, 10, 1}
	tablo := []struct {
		ad     string
		bas    bas
		govde  string
		bekle  Kayit
		dahaYk string
	}{
		{"yalnız deger", www, `{"deger":"203.0.113.9"}`, Kayit{Ad: "www", Tip: "A", Deger: "203.0.113.9", TTL: 600, Aktif: true}, ""},
		{"yalnız ttl", www, `{"ttl":120}`, Kayit{Ad: "www", Tip: "A", Deger: "203.0.113.5", TTL: 120, Aktif: true}, ""},
		{"yalnız aktif=false", www, `{"aktif":false}`, Kayit{Ad: "www", Tip: "A", Deger: "203.0.113.5", TTL: 600, Aktif: false}, ""},
		{"yalnız ad", www, `{"ad":"api"}`, Kayit{Ad: "api", Tip: "A", Deger: "203.0.113.5", TTL: 600, Aktif: true}, ""},
		{"boş gövde hiçbir şeyi değiştirmez", www, `{}`, Kayit{Ad: "www", Tip: "A", Deger: "203.0.113.5", TTL: 600, Aktif: true}, ""},
		{"null alanlar yok sayılır", www, `{"aktif":null,"ttl":null,"deger":null,"ad":null}`, Kayit{Ad: "www", Tip: "A", Deger: "203.0.113.5", TTL: 600, Aktif: true}, ""},
		{"pasif kayıt pasif kalır", pasifA, `{"deger":"203.0.113.7"}`, Kayit{Ad: "eski", Tip: "A", Deger: "203.0.113.7", TTL: 300, Aktif: false}, ""},
		{"pasif kayıt yalnız aktif=true ile açılır", pasifA, `{"aktif":true}`, Kayit{Ad: "eski", Tip: "A", Deger: "203.0.113.6", TTL: 300, Aktif: true}, ""},
		{"MX önceliği ttl değişince korunur", mx, `{"ttl":300}`, Kayit{Ad: "@", Tip: "MX", Deger: "mail.bayialan.test", TTL: 300, Oncelik: 10, Aktif: true}, ""},
		{"MX önceliği yalnız oncelik ile değişir", mx, `{"oncelik":20}`, Kayit{Ad: "@", Tip: "MX", Deger: "mail.bayialan.test", TTL: 900, Oncelik: 20, Aktif: true}, ""},
		{"tip değişimi", www, `{"tip":"txt","deger":"v=spf1 -all"}`, Kayit{Ad: "www", Tip: "TXT", Deger: "v=spf1 -all", TTL: 600, Aktif: true}, ""},
		{"ön yüzün tam nesnesi uygulanır", www, `{"id":1,"domain_id":42,"ad":"web","tip":"A","deger":"203.0.113.8","ttl":60,"oncelik":0,"aktif":false,"olusturma":""}`,
			Kayit{Ad: "web", Tip: "A", Deger: "203.0.113.8", TTL: 60, Aktif: false}, ""},
	}
	for _, c := range tablo {
		t.Run(c.ad, func(t *testing.T) {
			rid := zi_ekle(t, db, 42, c.bas.ad, c.bas.tip, c.bas.deger, c.bas.ttl, c.bas.oncelik, c.bas.aktif)
			kod, govde := zi_put(t, db, fmt.Sprint(rid), c.govde)
			if kod != http.StatusOK {
				t.Fatalf("200 beklenirdi: %d %s", kod, govde)
			}
			yanit := zi_json(t, govde)
			db_, _ := zi_kayit(t, db, rid)
			for ad, k := range map[string]Kayit{"yanıt": yanit, "db": db_} {
				if k.Ad != c.bekle.Ad || k.Tip != c.bekle.Tip || k.Deger != c.bekle.Deger ||
					k.TTL != c.bekle.TTL || k.Oncelik != c.bekle.Oncelik || k.Aktif != c.bekle.Aktif {
					t.Errorf("%s beklenen %+v, gelen %+v", ad, c.bekle, k)
				}
			}
		})
	}
}

// ---------------------------------------------------------------- 1b) SAHİPLİK / 404

func TestUpdate_YabanciVeOlmayanRid_404(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	yabanci := zi_ekle(t, db, 43, "www", "A", "203.0.113.50", 600, 0, 1) // başka hesabın kaydı
	once := zi_durum(t, db, 43)
	for _, rid := range []string{fmt.Sprint(yabanci), "999999", "abc", "0", "-5"} {
		kod, govde := zi_put(t, db, rid, `{"deger":"203.0.113.99","aktif":false}`)
		if kod != http.StatusNotFound {
			t.Errorf("rid=%s: 404 beklenirdi, gelen %d %s", rid, kod, govde)
		}
		if m := zh_alan(t, govde, "hata"); m == "" {
			t.Errorf("rid=%s: açık hata iletisi beklenirdi: %s", rid, govde)
		}
	}
	if sonra := zi_durum(t, db, 43); sonra != once {
		t.Errorf("yabancı hesabın kaydı DEĞİŞTİ:\nönce:\n%s\nsonra:\n%s", once, sonra)
	}
	// Kendi kaydı hâlâ güncellenebilir (404 yalnız sahiplik içindir).
	kendi := zi_ekle(t, db, 42, "www", "A", "203.0.113.5", 600, 0, 1)
	if kod, govde := zi_put(t, db, fmt.Sprint(kendi), `{"ttl":60}`); kod != http.StatusOK {
		t.Errorf("kendi kaydı güncellenmeliydi: %d %s", kod, govde)
	}
}

// ---------------------------------------------------------------- Create varsayılanları

func TestCreate_Varsayilanlar_Degismedi(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	kod, govde := zi_cagir(t, db, "admin", http.MethodPost, ziListeDesen, "/domains/42/dns",
		`{"tip":"a","deger":"203.0.113.1"}`, func(h *Handlers) http.HandlerFunc { return h.Create })
	if kod != http.StatusCreated {
		t.Fatalf("201 beklenirdi: %d %s", kod, govde)
	}
	k := zi_json(t, govde)
	if k.Ad != "@" || k.TTL != 3600 || !k.Aktif || k.Tip != "A" || k.ID == 0 {
		t.Errorf("varsayılanlar (ad '@', ttl 3600, aktif true) bozuldu: %+v", k)
	}
	kod, govde = zi_cagir(t, db, "admin", http.MethodPost, ziListeDesen, "/domains/42/dns",
		`{"ad":"x","tip":"A","deger":"203.0.113.2","aktif":false,"ttl":0}`, func(h *Handlers) http.HandlerFunc { return h.Create })
	if k := zi_json(t, govde); kod != http.StatusCreated || k.Aktif || k.TTL != 3600 {
		t.Errorf("aktif=false korunmalı, ttl=0 → 3600: %d %+v", kod, k)
	}
}

// ---------------------------------------------------------------- 2) DOĞRULAMA — uçların HEPSİ

func TestDogrulama_UclarinHepsiAyniDogrulayicidan(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	ekle := func(govde string) (int, string) {
		return zi_cagir(t, db, "admin", http.MethodPost, ziListeDesen, "/domains/42/dns", govde,
			func(h *Handlers) http.HandlerFunc { return h.Create })
	}
	// Create: kötü değerler 400 + açık ileti, DB'ye hiçbir şey yazılmaz.
	once := zi_durum(t, db, 42)
	for _, c := range []struct{ govde, iz string }{
		{`{"tip":"A","deger":"abc"}`, "IPv4"},
		{`{"tip":"A","deger":"2a01:4f8::1"}`, "IPv4"},
		{`{"tip":"AAAA","deger":"203.0.113.1"}`, "IPv6"},
		{`{"tip":"A","deger":""}`, "boş"},
		{`{"tip":"TXT","deger":"   "}`, "boş"},
		{`{"tip":"CNAME","ad":"www","deger":"203.0.113.1"}`, "IP adresi"},
		{`{"tip":"CNAME","ad":"@","deger":"x.test"}`, "CNAME"},
		{`{"tip":"MX","deger":"10 mail.x.test"}`, "MX"},
		{`{"tip":"MX","deger":"mail.x.test","oncelik":70000}`, "65535"},
		{`{"tip":"MX","deger":"mail.x.test","oncelik":-1}`, "65535"},
		{`{"tip":"NS","deger":"ns 1.x.test"}`, "boşluk"},
		{`{"tip":"TXT","deger":"\"abc\" ("}`, "tırnak"},
		{`{"tip":"TXT","deger":"abc\\"}`, "ters eğik"},
		{`{"tip":"SRV","deger":"5 99999 sip.x.test","oncelik":1}`, "port"},
		{`{"tip":"A","ad":"a b","deger":"203.0.113.1"}`, "boşluk"},
		{`{"tip":"A","ad":"w;w","deger":"203.0.113.1"}`, "karakterini"},
		{`{"tip":"A","deger":"203.0.113.1\n$INCLUDE /etc/passwd"}`, "satır sonu"},
		{`{"tip":"A","ad":"w","deger":"203.0.113.1","ttl":2147483648}`, "TTL"},
		{`{"tip":"BOGUS","deger":"x"}`, "tip"},
	} {
		kod, govde := ekle(c.govde)
		if kod != http.StatusBadRequest || !strings.Contains(zh_alan(t, govde, "hata"), c.iz) {
			t.Errorf("Create %s: 400 + %q beklenirdi, gelen %d %s", c.govde, c.iz, kod, govde)
		}
	}
	if sonra := zi_durum(t, db, 42); sonra != once {
		t.Fatalf("reddedilen Create DB'yi değiştirdi:\n%s\n→\n%s", once, sonra)
	}

	// Update: kötü değer 400, kayıt AYNEN kalır; geçerli değer uygulanır.
	rid := zi_ekle(t, db, 42, "www", "A", "203.0.113.5", 600, 0, 1)
	for _, govde := range []string{`{"deger":"abc"}`, `{"deger":""}`, `{"tip":"MX","deger":"203.0.113.5"}`, `{"tip":"AAAA"}`, `{"ad":"bad name"}`} {
		kod, yanit := zi_put(t, db, fmt.Sprint(rid), govde)
		if kod != http.StatusBadRequest {
			t.Errorf("Update %s: 400 beklenirdi: %d %s", govde, kod, yanit)
		}
	}
	if k, _ := zi_kayit(t, db, rid); k.Deger != "203.0.113.5" || k.Tip != "A" || k.Ad != "www" || k.TTL != 600 || !k.Aktif {
		t.Errorf("reddedilen Update kaydı değiştirdi: %+v", k)
	}

	// toplu-durum: bozuk (eski) kayıt etkinleştirilemez, pasife çekilebilir.
	bozukPasif := zi_ekle(t, db, 42, "kotu", "A", "bu-ip-degil", 300, 0, 0)
	bozukAktif := zi_ekle(t, db, 42, "kotu2", "A", "bu-da-degil", 300, 0, 1)
	durum := func(aktif bool, ids ...int64) (int, string) {
		b, _ := json.Marshal(map[string]any{"ids": ids, "aktif": aktif})
		return zi_cagir(t, db, "admin", http.MethodPost, "/domains/{id}/dns/toplu-durum", "/domains/42/dns/toplu-durum", string(b),
			func(h *Handlers) http.HandlerFunc { return h.TopluDurum })
	}
	if kod, govde := durum(true, rid, bozukPasif); kod != http.StatusBadRequest || !strings.Contains(zh_alan(t, govde, "hata"), fmt.Sprintf("#%d", bozukPasif)) {
		t.Errorf("toplu-durum: bozuk kaydı etkinleştirme 400 + kayıt kimliği beklenirdi: %d %s", kod, govde)
	}
	if k, _ := zi_kayit(t, db, bozukPasif); k.Aktif {
		t.Error("reddedilen toplu-durum bozuk kaydı etkinleştirdi")
	}
	if kod, govde := durum(false, bozukAktif); kod != http.StatusOK {
		t.Errorf("toplu-durum: bozuk kaydı PASİFE çekmek serbest olmalı: %d %s", kod, govde)
	}
	if k, _ := zi_kayit(t, db, bozukAktif); k.Aktif {
		t.Error("bozuk kayıt pasife çekilemedi")
	}

	// şablon: geçersiz şablon satırı atlanır ve bildirilir; geçerliler eklenir.
	dk_exec(t, db, `INSERT INTO dns_template (ad, tip, deger, ttl, oncelik, sira, aktif, reseller_id) VALUES
		('@', 'A', '{IP}', 3600, 0, 10, 1, 0), ('sablon-kotu', 'A', 'ip-degil', 3600, 0, 20, 1, 0),
		('@', 'NS', 'ns1.{DOMAIN}', 86400, 0, 30, 1, 0)`)
	kod, govde := zi_cagir(t, db, "admin", http.MethodPost, "/domains/{id}/dns/sablon", "/domains/42/dns/sablon", "",
		func(h *Handlers) http.HandlerFunc { return h.ApplyTemplate })
	var sy struct {
		Eklenen int      `json:"eklenen"`
		Atlanan []string `json:"atlanan_gecersiz"`
	}
	_ = json.Unmarshal([]byte(govde), &sy)
	if kod != http.StatusOK || sy.Eklenen != 2 || len(sy.Atlanan) != 1 || !strings.Contains(sy.Atlanan[0], "sablon-kotu") {
		t.Errorf("şablon: 2 geçerli satır eklenmeli, 1 geçersiz bildirilmeli: %d %s", kod, govde)
	}
	if n := dk_sayi(t, db, `SELECT COUNT(*) FROM dns_records WHERE domain_id=42 AND ad='sablon-kotu'`); n != 0 {
		t.Error("geçersiz şablon satırı DB'ye yazıldı")
	}

	// ice-aktar: tek bozuk satır TÜM içe aktarmayı reddeder (mod=degistir mevcut kayıtları korur).
	once = zi_durum(t, db, 42)
	ice := func(zone string) (int, string) {
		return zh_cagir(t, db, zh_tokenlar(t)["admin"], http.MethodPost, "/domains/{id}/dns/ice-aktar", "/domains/42/dns/ice-aktar?mod=degistir",
			"text/plain", zone, func(h *Handlers) http.HandlerFunc { return h.IceAktar })
	}
	kod, govde = ice("$ORIGIN bayialan.test.\n@ 3600 IN A 203.0.113.10\nkotu 3600 IN A 999.1.1.1\nmx 3600 IN MX 10 mail.bayialan.test.\n")
	if kod != http.StatusBadRequest || !strings.Contains(zh_alan(t, govde, "hata"), "kotu A") {
		t.Errorf("ice-aktar: bozuk satır 400 + satır adı beklenirdi: %d %s", kod, govde)
	}
	if sonra := zi_durum(t, db, 42); sonra != once {
		t.Errorf("reddedilen ice-aktar DB'yi değiştirdi (mod=degistir eski kayıtları SİLMEMELİ):\n%s\n→\n%s", once, sonra)
	}
	kod, govde = ice("$ORIGIN bayialan.test.\n@ 3600 IN A 203.0.113.10\nmx 3600 IN MX 10 mail.bayialan.test.\n")
	if kod != http.StatusOK {
		t.Errorf("ice-aktar: geçerli dosya 200 beklenirdi: %d %s", kod, govde)
	}
}

// ---------------------------------------------------------------- 3) ZONE HATASI → GERİ ALMA

func TestZoneHatasi_TumYazmaUclariDBYiGeriAlir(t *testing.T) {
	db := zi_db(t)
	rid := zi_ekle(t, db, 42, "www", "A", "203.0.113.5", 600, 0, 1)
	rid2 := zi_ekle(t, db, 42, "m", "A", "203.0.113.6", 600, 0, 0)
	dk_exec(t, db, `INSERT INTO dns_template (ad, tip, deger, ttl, oncelik, sira, aktif, reseller_id) VALUES ('@', 'A', '{IP}', 3600, 0, 10, 1, 0)`)
	once := zi_durum(t, db, 42)

	zg := &zoneGecersizHatasi{alan: "bayialan.test", cikti: "bayialan.test.zone.tmp:9: www.bayialan.test: bad dotted quad"}
	sunucu := errors.New("named yeniden yüklenemedi: rndc: connection refused /var/named/komsu.zone")
	ids := fmt.Sprintf(`{"ids":[%d,%d],"aktif":true}`, rid, rid2)
	idsSil := fmt.Sprintf(`{"ids":[%d,%d]}`, rid, rid2)
	uclar := []struct {
		ad, yontem, desen, yol, govde string
		isle                          func(*Handlers) http.HandlerFunc
	}{
		{"create", http.MethodPost, ziListeDesen, "/domains/42/dns", `{"tip":"A","ad":"yeni","deger":"203.0.113.77"}`, func(h *Handlers) http.HandlerFunc { return h.Create }},
		{"update", http.MethodPut, ziKayitDesen, fmt.Sprintf("/domains/42/dns/%d", rid), `{"deger":"203.0.113.88","aktif":false}`, func(h *Handlers) http.HandlerFunc { return h.Update }},
		{"delete", http.MethodDelete, ziKayitDesen, fmt.Sprintf("/domains/42/dns/%d", rid), ``, func(h *Handlers) http.HandlerFunc { return h.Delete }},
		{"toplu-sil", http.MethodPost, "/domains/{id}/dns/toplu-sil", "/domains/42/dns/toplu-sil", idsSil, func(h *Handlers) http.HandlerFunc { return h.TopluSil }},
		{"toplu-durum", http.MethodPost, "/domains/{id}/dns/toplu-durum", "/domains/42/dns/toplu-durum", ids, func(h *Handlers) http.HandlerFunc { return h.TopluDurum }},
		{"sablon", http.MethodPost, "/domains/{id}/dns/sablon", "/domains/42/dns/sablon", ``, func(h *Handlers) http.HandlerFunc { return h.ApplyTemplate }},
		{"soa", http.MethodPut, "/domains/{id}/dns/soa", "/domains/42/dns/soa", `{"primary_ns":"ns9.bayialan.test","refresh":111}`, func(h *Handlers) http.HandlerFunc { return h.PutSOA }},
		{"dnssec", http.MethodPost, "/domains/{id}/dns/dnssec", "/domains/42/dns/dnssec", `{"aktif":true}`, func(h *Handlers) http.HandlerFunc { return h.PostDNSSEC }},
		{"ice-aktar", http.MethodPost, "/domains/{id}/dns/ice-aktar", "/domains/42/dns/ice-aktar?mod=degistir", "$ORIGIN bayialan.test.\n@ 3600 IN A 203.0.113.10\n", func(h *Handlers) http.HandlerFunc { return h.IceAktar }},
	}
	eylemler := map[string]string{"create": "dns.kayit.ekle", "update": "dns.kayit.guncelle", "delete": "dns.kayit.sil",
		"toplu-sil": "dns.toplu.sil", "toplu-durum": "dns.toplu.durum", "sablon": "dns.sablon.uygula",
		"soa": "dns.soa.guncelle", "dnssec": "dns.dnssec.degistir", "ice-aktar": "dns.ice_aktar"}
	for _, u := range uclar {
		for _, hata := range []error{zg, sunucu} {
			t.Run(fmt.Sprintf("%s/%T", u.ad, hata), func(t *testing.T) {
				zh_dikis(t, hata)
				basarisizOnce, _, _ := zi_denetim(t, db, eylemler[u.ad], 0)
				kod, govde := zh_cagir(t, db, zh_tokenlar(t)["bayi"], u.yontem, u.desen, u.yol, "application/json", u.govde, u.isle)
				if kod < 400 {
					t.Fatalf("zone yazımı başarısızken istek BAŞARILI döndü: %d %s", kod, govde)
				}
				onek := zoneGeriAlindi
				if u.ad == "dnssec" {
					onek = "zone güncellenemedi: " // DNSSEC ucunun eski iletisi korunur
				}
				if m := zh_alan(t, govde, "hata"); !strings.HasPrefix(m, onek) || len(m) <= len(onek) {
					t.Errorf("hata iletisi %q ile başlamalı ve ayrıntı taşımalı: %s", onek, govde)
				}
				if hata == zg {
					if m := zh_alan(t, govde, "hata"); !strings.Contains(m, "bayialan.test") {
						t.Errorf("kiracının kendi zone hatası ayrıntısı görünmeli: %s", govde)
					}
				}
				if hata == sunucu {
					if kod != http.StatusInternalServerError {
						t.Errorf("sunucu tarafı zone hatası 500 olmalı: %d", kod)
					}
					zh_izYok(t, "bayi yanıtı", govde) // komşu zone izi sızmaz
					if strings.Contains(govde, "komsu.zone") {
						t.Errorf("sunucu ayrıntısı bayiye sızdı: %s", govde)
					}
				} else if beklenen := map[bool]int{true: http.StatusInternalServerError, false: http.StatusUnprocessableEntity}[u.ad == "dnssec"]; kod != beklenen {
					// DNSSEC uç noktası eskiden de 500 dönüyordu; diğerleri kiracının kendi zone hatasında 422.
					t.Errorf("kiracının kendi zone'u geçersizse %d beklenirdi: %d", beklenen, kod)
				}
				if sonra := zi_durum(t, db, 42); sonra != once {
					t.Fatalf("DB değişikliği GERİ ALINMADI:\nönce:\n%s\nsonra:\n%s", once, sonra)
				}
				// Geri alınan her işlem BAŞARISIZ denetim satırı bırakır (kapsam: domainin sahibi).
				adet, hedef, kapsam := zi_denetim(t, db, eylemler[u.ad], 0)
				if adet != basarisizOnce+1 {
					t.Errorf("%s: zone hatasında 1 yeni ok=0 denetim satırı beklenirdi: %d → %d", eylemler[u.ad], basarisizOnce, adet)
				} else if hedef != "bayialan.test" || kapsam != 7 {
					t.Errorf("%s: ok=0 satırı hedef/kapsam yanlış: %q %d", eylemler[u.ad], hedef, kapsam)
				}
			})
		}
	}

	// Hesap kilitlenmez: zone düzelince aynı istekler normal çalışır.
	zh_dikis(t, nil)
	kod, govde := zh_cagir(t, db, zh_tokenlar(t)["bayi"], http.MethodPost, ziListeDesen, "/domains/42/dns", "application/json",
		`{"tip":"A","ad":"yeni","deger":"203.0.113.77"}`, func(h *Handlers) http.HandlerFunc { return h.Create })
	if kod != http.StatusCreated {
		t.Fatalf("zone düzelince Create çalışmalı: %d %s", kod, govde)
	}
}

// Commit başarısız olursa zone da önceki hâline dönmelidir (geri() çağrılır).
func TestZoneIslem_CommitHatasindaZoneGeriAlinir(t *testing.T) {
	db := zi_db(t)
	eski := zoneUygula
	t.Cleanup(func() { zoneUygula = eski })
	geriCagri := 0
	zoneUygula = func(_ context.Context, q sorgucu, _ int64) (func(), bool, error) {
		// Zone "yazıldı"; işlemi dışarıdan bitirip Commit'i başarısız kıl.
		_ = q.(*sql.Tx).Rollback()
		return func() { geriCagri++ }, false, nil
	}
	kod, govde := zi_cagir(t, db, "admin", http.MethodPost, ziListeDesen, "/domains/42/dns",
		`{"tip":"A","ad":"yeni","deger":"203.0.113.77"}`, func(h *Handlers) http.HandlerFunc { return h.Create })
	if kod != http.StatusInternalServerError || !strings.Contains(govde, "kaydedilemedi") {
		t.Fatalf("commit hatasında 500 + açık ileti beklenirdi: %d %s", kod, govde)
	}
	if geriCagri != 1 {
		t.Errorf("commit hatasında zone geri() tam 1 kez çağrılmalı: %d", geriCagri)
	}
	if n := dk_sayi(t, db, `SELECT COUNT(*) FROM dns_records WHERE domain_id=42`); n != 0 {
		t.Errorf("commit edilmeyen kayıt DB'de: %d", n)
	}
}

// ---------------------------------------------------------------- 4) DENETİM

func zi_denetim(t *testing.T, db *sql.DB, eylem string, ok int) (adet int64, hedef string, kapsam int64) {
	t.Helper()
	adet = dk_sayi(t, db, `SELECT COUNT(*) FROM audit_log WHERE action=? AND ok=?`, eylem, ok)
	if adet > 0 {
		_ = db.QueryRow(`SELECT target, reseller_id FROM audit_log WHERE action=? AND ok=? ORDER BY id DESC LIMIT 1`, eylem, ok).Scan(&hedef, &kapsam)
	}
	return
}

func TestDenetim_DNSYazmaUclariAuditLogYazar(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	dk_exec(t, db, `INSERT INTO dns_template (ad, tip, deger, ttl, oncelik, sira, aktif, reseller_id) VALUES ('@', 'A', '{IP}', 3600, 0, 10, 1, 0)`)
	cagir := func(yontem, desen, yol, govde string, isle func(*Handlers) http.HandlerFunc) {
		t.Helper()
		kod, yanit := zh_cagir(t, db, zh_tokenlar(t)["bayi"], yontem, desen, yol, "application/json", govde, isle)
		if kod >= 400 {
			t.Fatalf("%s %s: %d %s", yontem, yol, kod, yanit)
		}
	}
	cagir(http.MethodPost, ziListeDesen, "/domains/42/dns", `{"tip":"A","ad":"w1","deger":"203.0.113.21"}`, func(h *Handlers) http.HandlerFunc { return h.Create })
	var rid int64
	_ = db.QueryRow(`SELECT id FROM dns_records WHERE ad='w1'`).Scan(&rid)
	cagir(http.MethodPut, ziKayitDesen, fmt.Sprintf("/domains/42/dns/%d", rid), `{"ttl":77}`, func(h *Handlers) http.HandlerFunc { return h.Update })
	cagir(http.MethodDelete, ziKayitDesen, fmt.Sprintf("/domains/42/dns/%d", rid), ``, func(h *Handlers) http.HandlerFunc { return h.Delete })
	r2 := zi_ekle(t, db, 42, "w2", "A", "203.0.113.22", 300, 0, 1)
	cagir(http.MethodPost, "/domains/{id}/dns/toplu-durum", "/domains/42/dns/toplu-durum", fmt.Sprintf(`{"ids":[%d],"aktif":false}`, r2), func(h *Handlers) http.HandlerFunc { return h.TopluDurum })
	cagir(http.MethodPost, "/domains/{id}/dns/toplu-sil", "/domains/42/dns/toplu-sil", fmt.Sprintf(`{"ids":[%d]}`, r2), func(h *Handlers) http.HandlerFunc { return h.TopluSil })
	cagir(http.MethodPost, "/domains/{id}/dns/sablon", "/domains/42/dns/sablon", ``, func(h *Handlers) http.HandlerFunc { return h.ApplyTemplate })
	cagir(http.MethodPut, "/domains/{id}/dns/soa", "/domains/42/dns/soa", `{"refresh":1234}`, func(h *Handlers) http.HandlerFunc { return h.PutSOA })
	cagir(http.MethodPost, "/domains/{id}/dns/dnssec", "/domains/42/dns/dnssec", `{"aktif":false}`, func(h *Handlers) http.HandlerFunc { return h.PostDNSSEC })
	cagir(http.MethodPost, "/domains/{id}/dns/ice-aktar", "/domains/42/dns/ice-aktar?mod=birlestir", "$ORIGIN bayialan.test.\nice 3600 IN A 203.0.113.30\n", func(h *Handlers) http.HandlerFunc { return h.IceAktar })

	for _, eylem := range []string{"dns.kayit.ekle", "dns.kayit.guncelle", "dns.kayit.sil", "dns.toplu.durum", "dns.toplu.sil",
		"dns.sablon.uygula", "dns.soa.guncelle", "dns.dnssec.degistir", "dns.ice_aktar"} {
		adet, hedef, kapsam := zi_denetim(t, db, eylem, 1)
		if adet != 1 {
			t.Errorf("%s: 1 başarılı denetim satırı beklenirdi: %d", eylem, adet)
			continue
		}
		if hedef != "bayialan.test" || kapsam != 7 {
			t.Errorf("%s: hedef/kapsam yanlış: %q %d (beklenen bayialan.test, bayi 7)", eylem, hedef, kapsam)
		}
	}

	// Aktör: işlemi yapan bayi (kimlik ve kullanıcı adı) denetim satırına yazılır.
	var aktorID int64
	var aktorAd string
	if err := db.QueryRow(`SELECT actor_user_id, actor_username FROM audit_log WHERE action='dns.kayit.ekle' AND ok=1 ORDER BY id DESC LIMIT 1`).
		Scan(&aktorID, &aktorAd); err != nil || aktorID != 7 || aktorAd != "bayi7" {
		t.Errorf("denetim aktörü bayi (7, bayi7) olmalı: %d %q %v", aktorID, aktorAd, err)
	}

	// Zone hatasında BAŞARISIZ denetim satırı (ok=0) düşer.
	zh_dikis(t, errors.New("named patladı"))
	kod, _ := zh_cagir(t, db, zh_tokenlar(t)["bayi"], http.MethodPost, ziListeDesen, "/domains/42/dns", "application/json",
		`{"tip":"A","ad":"w9","deger":"203.0.113.29"}`, func(h *Handlers) http.HandlerFunc { return h.Create })
	if kod < 400 {
		t.Fatalf("zone hatasında hata beklenirdi: %d", kod)
	}
	if adet, _, _ := zi_denetim(t, db, "dns.kayit.ekle", 0); adet != 1 {
		t.Errorf("zone hatasında dns.kayit.ekle ok=0 satırı beklenirdi: %d", adet)
	}
	// Reddedilen (400) istek denetim satırı yazmaz.
	zh_dikis(t, nil)
	n0 := dk_sayi(t, db, `SELECT COUNT(*) FROM audit_log`)
	zh_cagir(t, db, zh_tokenlar(t)["bayi"], http.MethodPost, ziListeDesen, "/domains/42/dns", "application/json",
		`{"tip":"A","deger":"abc"}`, func(h *Handlers) http.HandlerFunc { return h.Create })
	if n1 := dk_sayi(t, db, `SELECT COUNT(*) FROM audit_log`); n1 != n0 {
		t.Errorf("doğrulamada reddedilen istek denetim satırı yazmamalı: %d → %d", n0, n1)
	}
}

// ---------------------------------------------------------------- Update: eskiden kalmış bozuk kayıt

// DB'de ZATEN bozuk kayıt varsa: etkinleştirilemez (zone'a girmesin), ama pasife çekilebilir
// (düzeltme yolu) — hesap kilitlenmesin.
func TestUpdate_BozukEskiKayit_EtkinlestirilemezAmaPasifeCekilir(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	bozukPasif := zi_ekle(t, db, 42, "kotu", "A", "bu-ip-degil", 300, 0, 0)
	kod, govde := zi_put(t, db, fmt.Sprint(bozukPasif), `{"aktif":true}`)
	if kod != http.StatusBadRequest || !strings.Contains(zh_alan(t, govde, "hata"), "IPv4") {
		t.Errorf("bozuk kaydı etkinleştirme 400 olmalı: %d %s", kod, govde)
	}
	if k, _ := zi_kayit(t, db, bozukPasif); k.Aktif {
		t.Error("reddedilen istek kaydı etkinleştirdi")
	}
	// Etkinleştirirken değeri DÜZELTİRSE geçer.
	if kod, govde := zi_put(t, db, fmt.Sprint(bozukPasif), `{"aktif":true,"deger":"203.0.113.44"}`); kod != http.StatusOK {
		t.Errorf("değeri düzeltip etkinleştirme çalışmalı: %d %s", kod, govde)
	}
	bozukAktif := zi_ekle(t, db, 42, "kotu2", "A", "bu-da-degil", 300, 0, 1)
	if kod, govde := zi_put(t, db, fmt.Sprint(bozukAktif), `{"aktif":false}`); kod != http.StatusOK {
		t.Errorf("bozuk kaydı pasife çekmek serbest olmalı: %d %s", kod, govde)
	}
	if k, _ := zi_kayit(t, db, bozukAktif); k.Aktif {
		t.Error("bozuk kayıt pasife çekilemedi")
	}
}

// ---------------------------------------------------------------- domain başına seri çalışma

func TestZoneIslem_AyniDomaindeSeriCalisir(t *testing.T) {
	db := zi_db(t)
	eski := zoneUygula
	t.Cleanup(func() { zoneUygula = eski })
	var icerde, enCok int32
	zoneUygula = func(context.Context, sorgucu, int64) (func(), bool, error) {
		n := atomic.AddInt32(&icerde, 1)
		for {
			m := atomic.LoadInt32(&enCok)
			if n <= m || atomic.CompareAndSwapInt32(&enCok, m, n) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		atomic.AddInt32(&icerde, -1)
		return func() {}, false, nil
	}
	tok := zh_tokenlar(t)["admin"]
	var wg sync.WaitGroup
	kodlar := make([]int, 6)
	for i := range kodlar {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			kodlar[i], _ = zh_cagir(t, db, tok, http.MethodPost, ziListeDesen, "/domains/42/dns", "application/json",
				fmt.Sprintf(`{"tip":"A","ad":"h%d","deger":"203.0.113.%d"}`, i, 10+i), func(h *Handlers) http.HandlerFunc { return h.Create })
		}(i)
	}
	wg.Wait()
	for i, kod := range kodlar {
		if kod != http.StatusCreated {
			t.Errorf("istek %d: 201 beklenirdi: %d", i, kod)
		}
	}
	if enCok != 1 {
		t.Errorf("aynı domainin zone yazımları SERİ olmalı (aynı .tmp dosyası): eşzamanlı en çok %d", enCok)
	}
}

// ---------------------------------------------------------------- mutasyon sonrası eklenen sınır testleri

// Yalnız TTL değişince içerik doğrulaması çalışmaz; üst sınır ayrıca denetlenir.
func TestUpdate_TTLUstSiniri(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	rid := zi_ekle(t, db, 42, "www", "A", "203.0.113.5", 600, 0, 1)
	once := zi_durum(t, db, 42)
	kod, govde := zi_put(t, db, fmt.Sprint(rid), `{"ttl":2147483648}`)
	if kod != http.StatusBadRequest || !strings.Contains(zh_alan(t, govde, "hata"), "TTL") {
		t.Errorf("TTL üst sınırı aşımı 400 olmalı: %d %s", kod, govde)
	}
	if sonra := zi_durum(t, db, 42); sonra != once {
		t.Errorf("reddedilen istek kaydı değiştirdi:\n%s\n%s", once, sonra)
	}
	if kod, govde := zi_put(t, db, fmt.Sprint(rid), `{"ttl":2147483647}`); kod != http.StatusOK {
		t.Errorf("sınır değeri (2^31-1) kabul edilmeli: %d %s", kod, govde)
	}
	// ttl=0 → Create ile aynı varsayılan (3600); negatif 400 (eskiden sessizce 3600).
	if kod, govde := zi_put(t, db, fmt.Sprint(rid), `{"ttl":0}`); kod != http.StatusOK || zi_json(t, govde).TTL != 3600 {
		t.Errorf("ttl=0 varsayılan 3600 olmalı: %d %s", kod, govde)
	}
	if kod, govde := zi_put(t, db, fmt.Sprint(rid), `{"ttl":-5}`); kod != http.StatusBadRequest {
		t.Errorf("ttl<0 400 olmalı: %d %s", kod, govde)
	}
}

// Update: geçersiz tip 400 + açık ileti, kayıt değişmez; küçük harf tip kabul edilir.
func TestUpdate_GecersizTip(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	rid := zi_ekle(t, db, 42, "www", "A", "203.0.113.5", 600, 0, 1)
	once := zi_durum(t, db, 42)
	for _, tip := range []string{"BOGUS", "", "  ", "A;B"} {
		kod, govde := zi_put(t, db, fmt.Sprint(rid), fmt.Sprintf(`{"tip":%q}`, tip))
		if kod != http.StatusBadRequest || !strings.Contains(zh_alan(t, govde, "hata"), "tip") {
			t.Errorf("tip=%q: 400 + tip iletisi beklenirdi: %d %s", tip, kod, govde)
		}
	}
	if sonra := zi_durum(t, db, 42); sonra != once {
		t.Errorf("geçersiz tip kaydı değiştirdi:\n%s\n%s", once, sonra)
	}
}

// Create ve Update değeri kırpar: kenardaki boşluklar zone'a ve DB'ye girmez.
func TestDeger_BoslukKirpilir(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	kod, govde := zi_cagir(t, db, "admin", http.MethodPost, ziListeDesen, "/domains/42/dns",
		`{"tip":"A","ad":"kirp","deger":"  203.0.113.9  "}`, func(h *Handlers) http.HandlerFunc { return h.Create })
	if kod != http.StatusCreated {
		t.Fatalf("kenarda boşluklu değer kırpılıp kabul edilmeli: %d %s", kod, govde)
	}
	if k := zi_json(t, govde); k.Deger != "203.0.113.9" {
		t.Errorf("Create değeri kırpmadı: %q", k.Deger)
	}
	rid := zi_ekle(t, db, 42, "www", "A", "203.0.113.5", 600, 0, 1)
	kod, govde = zi_put(t, db, fmt.Sprint(rid), `{"deger":"\t203.0.113.7 \n"}`)
	if kod != http.StatusOK {
		t.Fatalf("Update: kenarda boşluklu değer kırpılıp kabul edilmeli: %d %s", kod, govde)
	}
	if k, _ := zi_kayit(t, db, rid); k.Deger != "203.0.113.7" {
		t.Errorf("Update değeri kırpmadı: %q", k.Deger)
	}
}

// Olmayan / yabancı kaydı silmek 404 döner (Update ile aynı), denetim satırı yazmaz ve
// yabancı kaydı silmez.
func TestDelete_OlmayanVeYabanciKayit_DenetimYazmaz(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	yabanci := zi_ekle(t, db, 43, "www", "A", "203.0.113.50", 600, 0, 1)
	once := zi_durum(t, db, 43)
	for _, rid := range []string{fmt.Sprint(yabanci), "999999"} {
		kod, govde := zi_cagir(t, db, "admin", http.MethodDelete, ziKayitDesen, "/domains/42/dns/"+rid, ``,
			func(h *Handlers) http.HandlerFunc { return h.Delete })
		if kod != http.StatusNotFound {
			t.Errorf("rid=%s: 404 beklenirdi: %d %s", rid, kod, govde)
		}
	}
	if sonra := zi_durum(t, db, 43); sonra != once {
		t.Errorf("yabancı hesabın kaydı silindi/değişti:\n%s\n%s", once, sonra)
	}
	if n := dk_sayi(t, db, `SELECT COUNT(*) FROM audit_log WHERE action='dns.kayit.sil'`); n != 0 {
		t.Errorf("silinmeyen kayıt için denetim satırı yazılmamalı: %d", n)
	}
	// Gerçek silme denetlenir.
	kendi := zi_ekle(t, db, 42, "sil", "A", "203.0.113.5", 600, 0, 1)
	zi_cagir(t, db, "admin", http.MethodDelete, ziKayitDesen, fmt.Sprintf("/domains/42/dns/%d", kendi), ``,
		func(h *Handlers) http.HandlerFunc { return h.Delete })
	if n := dk_sayi(t, db, `SELECT COUNT(*) FROM audit_log WHERE action='dns.kayit.sil' AND ok=1`); n != 1 {
		t.Errorf("gerçek silme 1 denetim satırı yazmalı: %d", n)
	}
}

// Update: boş ad Create'teki gibi '@' olur.
func TestUpdate_BosAdApexOlur(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	rid := zi_ekle(t, db, 42, "www", "A", "203.0.113.5", 600, 0, 1)
	kod, govde := zi_put(t, db, fmt.Sprint(rid), `{"ad":""}`)
	if kod != http.StatusOK || zi_json(t, govde).Ad != "@" {
		t.Errorf("boş ad '@' olmalı: %d %s", kod, govde)
	}
}

// İçerik doğrulaması yalnız içerik değişince (ya da kayıt zone'a yeniden girerken) çalışır:
// doğrulayıcımızın BIND'den katı kaldığı eski bir kaydın yalnız TTL'si değiştirilebilir;
// zone aşaması (named-checkzone) o kayıt için karar verir.
func TestUpdate_EskiKayitinTTLDegisimiIcerikDogrulamasiniTetiklemez(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	rid := zi_ekle(t, db, 42, "eski", "TXT", `a\"b`, 600, 0, 1) // düz metin + ters bölü + tırnak: doğrulayıcı reddeder, BIND kabul eder
	if err := KayitGecerli(Kayit{Ad: "eski", Tip: "TXT", Deger: `a\"b`, TTL: 600, Aktif: true}); err == nil {
		t.Fatal("ön koşul: doğrulayıcı bu değeri reddetmeli")
	}
	if kod, govde := zi_put(t, db, fmt.Sprint(rid), `{"ttl":120}`); kod != http.StatusOK {
		t.Errorf("yalnız TTL değişimi içerik doğrulamasına takılmamalı: %d %s", kod, govde)
	}
	if kod, govde := zi_put(t, db, fmt.Sprint(rid), `{"aktif":true,"ttl":90}`); kod != http.StatusOK {
		t.Errorf("zaten aktif kaydı aktif=true ile kaydetmek yeniden etkinleştirme değildir: %d %s", kod, govde)
	}
	if kod, govde := zi_put(t, db, fmt.Sprint(rid), `{"deger":"a\\\"b c"}`); kod != http.StatusBadRequest {
		t.Errorf("içerik değişince doğrulama çalışmalı: %d %s", kod, govde)
	}
}

// ---------------------------------------------------------------- toplu uçlar: kapsam, sınır, ileti

func zi_toplu(t *testing.T, db *sql.DB, uc, govde string, isle func(*Handlers) http.HandlerFunc) (int, string) {
	t.Helper()
	return zi_cagir(t, db, "admin", http.MethodPost, "/domains/{id}/dns/"+uc, "/domains/42/dns/"+uc, govde, isle)
}

func TestTopluSil_BosListeVeKapsam(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	sil := func(govde string) (int, string) {
		return zi_toplu(t, db, "toplu-sil", govde, func(h *Handlers) http.HandlerFunc { return h.TopluSil })
	}
	kendi := zi_ekle(t, db, 42, "a", "A", "203.0.113.1", 300, 0, 1)
	yabanci := zi_ekle(t, db, 43, "b", "A", "203.0.113.2", 300, 0, 1)
	once := zi_durum(t, db, 43)
	for _, g := range []string{`{"ids":[]}`, `{}`} {
		if kod, govde := sil(g); kod != http.StatusBadRequest {
			t.Errorf("boş liste 400 olmalı (%s): %d %s", g, kod, govde)
		}
	}
	kod, govde := sil(fmt.Sprintf(`{"ids":[%d,%d]}`, kendi, yabanci))
	if kod != http.StatusOK || !strings.Contains(govde, `"silinen":1`) {
		t.Errorf("yalnız kendi kaydı silinmeli (silinen=1): %d %s", kod, govde)
	}
	if _, bulundu := zi_kayit(t, db, kendi); bulundu {
		t.Error("kendi kaydı silinmedi")
	}
	if sonra := zi_durum(t, db, 43); sonra != once {
		t.Errorf("YABANCI hesabın kaydı toplu-sil ile silindi:\n%s\n%s", once, sonra)
	}
}

func TestTopluDurum_BosListeKapsamEslemeVeCokluBozuk(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	durum := func(aktif bool, ids ...int64) (int, string) {
		b, _ := json.Marshal(map[string]any{"ids": ids, "aktif": aktif})
		return zi_toplu(t, db, "toplu-durum", string(b), func(h *Handlers) http.HandlerFunc { return h.TopluDurum })
	}
	if kod, govde := durum(true); kod != http.StatusBadRequest {
		t.Errorf("boş liste 400 olmalı: %d %s", kod, govde)
	}

	pasif := zi_ekle(t, db, 42, "p", "A", "203.0.113.1", 300, 0, 0)
	aktif := zi_ekle(t, db, 42, "a", "A", "203.0.113.2", 300, 0, 1)
	yabanciBozuk := zi_ekle(t, db, 43, "x", "A", "ip-degil", 300, 0, 0) // başka hesabın bozuk kaydı
	yabanciPasif := zi_ekle(t, db, 43, "y", "A", "203.0.113.3", 300, 0, 0)
	yabanciAktif := zi_ekle(t, db, 43, "z", "A", "203.0.113.4", 300, 0, 1)
	once := zi_durum(t, db, 43)

	// Etkinleştirme: kendi kaydı AKTİF olur; yabancı kayıtlar (bozuk olsa da) ne engeller ne değişir.
	kod, govde := durum(true, pasif, yabanciBozuk, yabanciPasif)
	if kod != http.StatusOK || !strings.Contains(govde, `"guncellenen":1`) {
		t.Fatalf("etkinleştirme 200 + guncellenen=1 beklenirdi: %d %s", kod, govde)
	}
	if k, _ := zi_kayit(t, db, pasif); !k.Aktif {
		t.Error("toplu-durum aktif=true kaydı etkinleştirmedi")
	}
	// Pasife çekme: yalnız kendi kaydı.
	kod, govde = durum(false, aktif, yabanciAktif)
	if kod != http.StatusOK || !strings.Contains(govde, `"guncellenen":1`) {
		t.Fatalf("pasife çekme 200 + guncellenen=1 beklenirdi: %d %s", kod, govde)
	}
	if k, _ := zi_kayit(t, db, aktif); k.Aktif {
		t.Error("toplu-durum aktif=false kaydı pasife çekmedi")
	}
	if sonra := zi_durum(t, db, 43); sonra != once {
		t.Errorf("YABANCI hesabın kayıtları toplu-durum ile değişti:\n%s\n%s", once, sonra)
	}

	// Birden çok bozuk kayıt: ileti HEPSİNİ adlandırır, hiçbiri etkinleşmez.
	b1 := zi_ekle(t, db, 42, "k1", "A", "bozuk-bir", 300, 0, 0)
	b2 := zi_ekle(t, db, 42, "k2", "A", "bozuk-iki", 300, 0, 0)
	kod, govde = durum(true, b1, b2, pasif)
	h := zh_alan(t, govde, "hata")
	if kod != http.StatusBadRequest || !strings.Contains(h, fmt.Sprintf("#%d", b1)) || !strings.Contains(h, fmt.Sprintf("#%d", b2)) {
		t.Errorf("ileti her iki bozuk kaydı da adlandırmalı: %d %s", kod, govde)
	}
	for _, id := range []int64{b1, b2} {
		if k, _ := zi_kayit(t, db, id); k.Aktif {
			t.Errorf("bozuk kayıt #%d etkinleşti", id)
		}
	}
}

func TestSablon_AtlananSatirDenetimNotuVeIdempotans(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	dk_exec(t, db, `INSERT INTO dns_template (ad, tip, deger, ttl, oncelik, sira, aktif, reseller_id) VALUES
		('@', 'A', '{IP}', 3600, 0, 10, 1, 0), ('kotu', 'A', 'ip-degil', 3600, 0, 20, 1, 0)`)
	sablon := func() (int, string) {
		return zi_toplu(t, db, "sablon", ``, func(h *Handlers) http.HandlerFunc { return h.ApplyTemplate })
	}
	if kod, govde := sablon(); kod != http.StatusOK || !strings.Contains(govde, `"eklenen":1`) {
		t.Fatalf("ilk uygulama 1 kayıt eklemeli: %d %s", kod, govde)
	}
	var detay string
	if err := db.QueryRow(`SELECT detail FROM audit_log WHERE action='dns.sablon.uygula' AND ok=1 ORDER BY id DESC LIMIT 1`).Scan(&detay); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detay, "1 geçersiz şablon satırı atlandı") {
		t.Errorf("denetim notu atlanan satırı bildirmeli: %q", detay)
	}
	// İdempotent: ikinci uygulama yinelenen kayıt eklemez.
	if kod, govde := sablon(); kod != http.StatusOK || !strings.Contains(govde, `"eklenen":0`) {
		t.Errorf("ikinci uygulama kayıt eklememeli: %d %s", kod, govde)
	}
	if n := dk_sayi(t, db, `SELECT COUNT(*) FROM dns_records WHERE domain_id=42 AND ad='@' AND tip='A'`); n != 1 {
		t.Errorf("şablon yinelenen kayıt üretti: %d", n)
	}
}

// ---------------------------------------------------------------- ice-aktar davranışı

func TestIceAktar_DogrulamaSinirlariSOAVeModlar(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	ice := func(mod, zone string) (int, string) {
		return zh_cagir(t, db, zh_tokenlar(t)["admin"], http.MethodPost, "/domains/{id}/dns/ice-aktar", "/domains/42/dns/ice-aktar?mod="+mod,
			"text/plain", zone, func(h *Handlers) http.HandlerFunc { return h.IceAktar })
	}
	baslik := "$ORIGIN bayialan.test.\n"
	for _, bos := range []string{"", "  \n\t\n"} {
		if kod, govde := ice("degistir", bos); kod != http.StatusBadRequest || !strings.Contains(zh_alan(t, govde, "hata"), "boş") {
			t.Errorf("boş içerik 400 olmalı: %d %s", kod, govde)
		}
	}
	if kod, govde := ice("degistir", baslik+"; yalnız yorum\n"); kod != http.StatusBadRequest || !strings.Contains(zh_alan(t, govde, "hata"), "bulunamadı") {
		t.Errorf("kaydı olmayan dosya 400 olmalı: %d %s", kod, govde)
	}

	// Bozuk kayıt sayısı: ileti en çok 5 kaydı adlandırır, fazlası "(+N kayıt daha)" ile özetlenir.
	kotular := func(n int) string {
		var sb strings.Builder
		sb.WriteString(baslik)
		for i := 1; i <= n; i++ {
			fmt.Fprintf(&sb, "k%d 3600 IN A 999.1.1.%d\n", i, i)
		}
		return sb.String()
	}
	for _, c := range []struct {
		n       int
		iz      string
		izOlmaz string
	}{
		{5, "k5 A", "daha"},
		{6, "(+1 kayıt daha)", "k6 A"},
		{8, "(+3 kayıt daha)", "k8 A"},
	} {
		kod, govde := ice("birlestir", kotular(c.n))
		h := zh_alan(t, govde, "hata")
		if kod != http.StatusBadRequest || !strings.Contains(h, c.iz) || strings.Contains(h, c.izOlmaz) || !strings.Contains(h, "k1 A") {
			t.Errorf("%d bozuk kayıt: 400 + %q (ve %q olmadan) beklenirdi: %d %s", c.n, c.iz, c.izOlmaz, kod, govde)
		}
	}

	// Dosyadaki geçersiz SOA tüm içe aktarmayı reddeder.
	once := zi_durum(t, db, 42)
	kod, govde := ice("degistir", baslik+"@ 3600 IN SOA ns1$kotu.bayialan.test. admin.bayialan.test. 1 7200 3600 1209600 300\n@ 3600 IN A 203.0.113.10\n")
	if kod != http.StatusBadRequest || !strings.Contains(zh_alan(t, govde, "hata"), "SOA") {
		t.Errorf("geçersiz SOA 400 olmalı: %d %s", kod, govde)
	}
	if sonra := zi_durum(t, db, 42); sonra != once {
		t.Errorf("reddedilen içe aktarma DB'yi değiştirdi:\n%s\n%s", once, sonra)
	}

	// mod=birlestir: var olanlar korunur, aynı kayıt atlanır, yenisi eklenir, SOA yazılır.
	zi_ekle(t, db, 42, "www", "A", "203.0.113.5", 600, 0, 1)
	zi_ekle(t, db, 42, "eski", "A", "203.0.113.6", 600, 0, 1)
	kod, govde = ice("birlestir", baslik+"@ 3600 IN SOA ns1.bayialan.test. admin.bayialan.test. 1 7200 3600 1209600 300\nwww 3600 IN A 203.0.113.5\nyeni 3600 IN A 203.0.113.7\n")
	if kod != http.StatusOK || !strings.Contains(govde, `"eklenen":1`) || !strings.Contains(govde, `"atlanan":1`) {
		t.Fatalf("birleştir: eklenen=1 atlanan=1 beklenirdi: %d %s", kod, govde)
	}
	if n := dk_sayi(t, db, `SELECT COUNT(*) FROM dns_records WHERE domain_id=42 AND ad='www' AND tip='A'`); n != 1 {
		t.Errorf("birleştir yinelenen kayıt üretti: %d", n)
	}
	if n := dk_sayi(t, db, `SELECT COUNT(*) FROM dns_records WHERE domain_id=42 AND ad='eski'`); n != 1 {
		t.Errorf("birleştir mevcut kaydı silmemeli: %d", n)
	}
	if n := dk_sayi(t, db, `SELECT refresh FROM dns_soa WHERE domain_id=42`); n != 7200 {
		t.Errorf("içe aktarılan SOA yazılmadı: refresh=%d", n)
	}

	// mod=degistir: mevcut kayıtlar silinir, dosyadakiler eklenir.
	kod, govde = ice("degistir", baslik+"son 3600 IN A 203.0.113.8\n")
	if kod != http.StatusOK || !strings.Contains(govde, `"eklenen":1`) {
		t.Fatalf("değiştir: eklenen=1 beklenirdi: %d %s", kod, govde)
	}
	if n := dk_sayi(t, db, `SELECT COUNT(*) FROM dns_records WHERE domain_id=42`); n != 1 {
		t.Errorf("değiştir yalnız dosyadaki kayıtları bırakmalı: %d", n)
	}
	// Başka hesabın kayıtlarına dokunulmaz.
	yabanci := zi_ekle(t, db, 43, "www", "A", "203.0.113.50", 600, 0, 1)
	ice("degistir", baslik+"son 3600 IN A 203.0.113.8\n")
	if _, bulundu := zi_kayit(t, db, yabanci); !bulundu {
		t.Error("mod=degistir YABANCI hesabın kaydını sildi")
	}
}

// ---------------------------------------------------------------- SOA ve DNSSEC

func TestPutSOA_VarsayilanlarDogrulamaVeYazim(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	soa := func(govde string) (int, string) {
		return zi_cagir(t, db, "admin", http.MethodPut, "/domains/{id}/dns/soa", "/domains/42/dns/soa", govde,
			func(h *Handlers) http.HandlerFunc { return h.PutSOA })
	}
	d := defaultSOA("bayialan.test")
	// Boş gövde: tüm alanlar varsayılan.
	kod, govde := soa(`{}`)
	var y SOA
	_ = json.Unmarshal([]byte(govde), &y)
	if kod != http.StatusOK || y != d {
		t.Fatalf("boş gövde varsayılan SOA yazmalı: %d %s (beklenen %+v)", kod, govde, d)
	}
	var db_ SOA
	if err := db.QueryRow(`SELECT primary_ns, hostmaster, refresh, retry, expire, minimum, ttl FROM dns_soa WHERE domain_id=42`).
		Scan(&db_.PrimaryNS, &db_.Hostmaster, &db_.Refresh, &db_.Retry, &db_.Expire, &db_.Minimum, &db_.TTL); err != nil || db_ != d {
		t.Errorf("DB'deki SOA varsayılan olmalı: %+v %v", db_, err)
	}
	// Kısmi gövde: gelenler uygulanır (kırpılır), gelmeyenler varsayılan.
	kod, govde = soa(`{"primary_ns":"  ns9.bayialan.test ","refresh":111,"expire":-5}`)
	_ = json.Unmarshal([]byte(govde), &y)
	beklenen := d
	beklenen.PrimaryNS, beklenen.Refresh = "ns9.bayialan.test", 111
	if kod != http.StatusOK || y != beklenen {
		t.Errorf("kısmi gövde: beklenen %+v, gelen %d %s", beklenen, kod, govde)
	}
	// Var olan SOA satırı GÜNCELLENİR (yalnız ilk yazım değil).
	if err := db.QueryRow(`SELECT primary_ns, hostmaster, refresh, retry, expire, minimum, ttl FROM dns_soa WHERE domain_id=42`).
		Scan(&db_.PrimaryNS, &db_.Hostmaster, &db_.Refresh, &db_.Retry, &db_.Expire, &db_.Minimum, &db_.TTL); err != nil || db_ != beklenen {
		t.Errorf("ikinci PUT DB'deki SOA'yı güncellemeli: %+v %v", db_, err)
	}
	// Geçersiz alan 400, DB değişmez.
	once := zi_durum(t, db, 42)
	var refOnce int
	_ = db.QueryRow(`SELECT refresh FROM dns_soa WHERE domain_id=42`).Scan(&refOnce)
	for _, g := range []string{`{"hostmaster":"a b@x.test","refresh":5}`, `{"primary_ns":"ns1.x.test\n$INCLUDE /etc/passwd","refresh":5}`} {
		if kod, govde := soa(g); kod != http.StatusBadRequest {
			t.Errorf("geçersiz SOA 400 olmalı (%s): %d %s", g, kod, govde)
		}
	}
	var refSonra int
	_ = db.QueryRow(`SELECT refresh FROM dns_soa WHERE domain_id=42`).Scan(&refSonra)
	if refSonra != refOnce || zi_durum(t, db, 42) != once {
		t.Errorf("reddedilen SOA DB'yi değiştirdi: refresh %d → %d", refOnce, refSonra)
	}
	// Olmayan domain 404.
	if kod, _ := zi_cagir(t, db, "admin", http.MethodPut, "/domains/{id}/dns/soa", "/domains/9999/dns/soa", `{}`,
		func(h *Handlers) http.HandlerFunc { return h.PutSOA }); kod != http.StatusNotFound {
		t.Errorf("olmayan domain 404 olmalı: %d", kod)
	}
}

func TestDNSSEC_BayrakYazimi(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	dnssec := func(govde string) (int, string) {
		return zi_cagir(t, db, "admin", http.MethodPost, "/domains/{id}/dns/dnssec", "/domains/42/dns/dnssec", govde,
			func(h *Handlers) http.HandlerFunc { return h.PostDNSSEC })
	}
	bayrak := func() int64 { return dk_sayi(t, db, `SELECT dnssec_aktif FROM domains WHERE id=42`) }
	if kod, govde := dnssec(`{"aktif":true}`); kod != http.StatusOK || !strings.Contains(govde, `"aktif":true`) || bayrak() != 1 {
		t.Errorf("aktif=true bayrağı 1 yapmalı: %d %s bayrak=%d", kod, govde, bayrak())
	}
	if kod, govde := dnssec(`{"aktif":false}`); kod != http.StatusOK || !strings.Contains(govde, `"aktif":false`) || bayrak() != 0 {
		t.Errorf("aktif=false bayrağı 0 yapmalı: %d %s bayrak=%d", kod, govde, bayrak())
	}
	// Diğer domainin bayrağına dokunulmaz.
	dk_exec(t, db, `UPDATE domains SET dnssec_aktif=1 WHERE id=43`)
	dnssec(`{"aktif":false}`)
	if n := dk_sayi(t, db, `SELECT dnssec_aktif FROM domains WHERE id=43`); n != 1 {
		t.Errorf("başka domainin DNSSEC bayrağı değişti: %d", n)
	}
	if kod, _ := dnssec(`{bozuk`); kod != http.StatusBadRequest {
		t.Errorf("bozuk gövde 400 olmalı: %d", kod)
	}
}

// Demo aboneliğin DNS'i hiçbir yazma ucundan değiştirilemez (403, DB ve denetim değişmez).
func TestDemoDomain_TumYazmaUclari403(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	dk_exec(t, db, `UPDATE domains SET is_demo=1 WHERE id=43`)
	rid := zi_ekle(t, db, 43, "www", "A", "203.0.113.50", 600, 0, 1)
	once := zi_durum(t, db, 43)
	n0 := dk_sayi(t, db, `SELECT COUNT(*) FROM audit_log`)
	r := fmt.Sprint(rid)
	uclar := []struct {
		ad, yontem, desen, yol, govde string
		isle                          func(*Handlers) http.HandlerFunc
	}{
		{"create", http.MethodPost, ziListeDesen, "/domains/43/dns", `{"tip":"A","ad":"y","deger":"203.0.113.77"}`, func(h *Handlers) http.HandlerFunc { return h.Create }},
		{"update", http.MethodPut, ziKayitDesen, "/domains/43/dns/" + r, `{"ttl":60}`, func(h *Handlers) http.HandlerFunc { return h.Update }},
		{"delete", http.MethodDelete, ziKayitDesen, "/domains/43/dns/" + r, ``, func(h *Handlers) http.HandlerFunc { return h.Delete }},
		{"toplu-sil", http.MethodPost, "/domains/{id}/dns/toplu-sil", "/domains/43/dns/toplu-sil", `{"ids":[` + r + `]}`, func(h *Handlers) http.HandlerFunc { return h.TopluSil }},
		{"toplu-durum", http.MethodPost, "/domains/{id}/dns/toplu-durum", "/domains/43/dns/toplu-durum", `{"ids":[` + r + `],"aktif":false}`, func(h *Handlers) http.HandlerFunc { return h.TopluDurum }},
		{"sablon", http.MethodPost, "/domains/{id}/dns/sablon", "/domains/43/dns/sablon", ``, func(h *Handlers) http.HandlerFunc { return h.ApplyTemplate }},
		{"soa", http.MethodPut, "/domains/{id}/dns/soa", "/domains/43/dns/soa", `{"refresh":111}`, func(h *Handlers) http.HandlerFunc { return h.PutSOA }},
		{"dnssec", http.MethodPost, "/domains/{id}/dns/dnssec", "/domains/43/dns/dnssec", `{"aktif":true}`, func(h *Handlers) http.HandlerFunc { return h.PostDNSSEC }},
		{"ice-aktar", http.MethodPost, "/domains/{id}/dns/ice-aktar", "/domains/43/dns/ice-aktar?mod=degistir", "$ORIGIN bayialan2.test.\n@ 3600 IN A 203.0.113.10\n", func(h *Handlers) http.HandlerFunc { return h.IceAktar }},
	}
	for _, u := range uclar {
		t.Run(u.ad, func(t *testing.T) {
			kod, govde := zi_cagir(t, db, "admin", u.yontem, u.desen, u.yol, u.govde, u.isle)
			if kod != http.StatusForbidden {
				t.Errorf("demo domain: 403 beklenirdi: %d %s", kod, govde)
			}
		})
	}
	if sonra := zi_durum(t, db, 43); sonra != once {
		t.Errorf("demo domainin DNS'i değişti:\n%s\n%s", once, sonra)
	}
	if n1 := dk_sayi(t, db, `SELECT COUNT(*) FROM audit_log`); n1 != n0 {
		t.Errorf("reddedilen demo istekleri denetim satırı yazmamalı: %d → %d", n0, n1)
	}
}

func TestKayitOzeti(t *testing.T) {
	uzun := strings.Repeat("x", 130)
	for _, c := range []struct {
		k   Kayit
		bek string
	}{
		{Kayit{ID: 7, Ad: "www", Tip: "A", Deger: " 203.0.113.5 ", TTL: 600, Aktif: true}, "#7 www A 203.0.113.5 ttl=600"},
		{Kayit{ID: 8, Ad: "@", Tip: "MX", Deger: "mail.x.test", TTL: 300, Oncelik: 10, Aktif: true}, "#8 @ MX mail.x.test ttl=300 oncelik=10"},
		{Kayit{ID: 9, Ad: "_sip", Tip: "SRV", Deger: "5 5060 sip.x.test", TTL: 60, Oncelik: 1, Aktif: true}, "#9 _sip SRV 5 5060 sip.x.test ttl=60 oncelik=1"},
		{Kayit{ID: 10, Ad: "eski", Tip: "A", Deger: "203.0.113.6", TTL: 60, Oncelik: 5}, "#10 eski A 203.0.113.6 ttl=60 pasif"},
		{Kayit{ID: 11, Ad: "t", Tip: "TXT", Deger: uzun, TTL: 60, Aktif: true}, "#11 t TXT " + strings.Repeat("x", 120) + "… ttl=60"},
		{Kayit{ID: 12, Ad: "t", Tip: "TXT", Deger: strings.Repeat("z", 121), TTL: 60, Aktif: true}, "#12 t TXT " + strings.Repeat("z", 120) + "… ttl=60"},
	} {
		if got := kayitOzeti(c.k); got != c.bek {
			t.Errorf("kayitOzeti:\n got %q\nwant %q", got, c.bek)
		}
	}
	if got := kayitOzeti(Kayit{ID: 1, Ad: "a", Tip: "A", Deger: strings.Repeat("y", 120), TTL: 1, Aktif: true}); strings.Contains(got, "…") {
		t.Errorf("tam 120 karakter kırpılmamalı: %q", got)
	}
}

// Farklı domainlerin zone yazımları birbirini BEKLETMEZ (kilit domain başınadır).
func TestZoneIslem_FarkliDomainlerParalelCalisir(t *testing.T) {
	db := zi_db(t)
	eski := zoneUygula
	t.Cleanup(func() { zoneUygula = eski })
	girdi := make(chan struct{}, 1)
	birak := make(chan struct{})
	zoneUygula = func(_ context.Context, _ sorgucu, id int64) (func(), bool, error) {
		if id == 42 {
			girdi <- struct{}{}
			<-birak
		}
		return func() {}, false, nil
	}
	ekle := func(domain, ad string) int {
		kod, _ := zi_cagir(t, db, "admin", http.MethodPost, ziListeDesen, "/domains/"+domain+"/dns",
			fmt.Sprintf(`{"tip":"A","ad":%q,"deger":"203.0.113.60"}`, ad), func(h *Handlers) http.HandlerFunc { return h.Create })
		return kod
	}
	kod42 := make(chan int, 1)
	go func() { kod42 <- ekle("42", "a") }()
	<-girdi // 42, zone yazımında kilit elinde bekliyor
	kod43 := make(chan int, 1)
	go func() { kod43 <- ekle("43", "b") }()
	select {
	case k := <-kod43:
		if k != http.StatusCreated {
			t.Errorf("43 için 201 beklenirdi: %d", k)
		}
	case <-time.After(5 * time.Second):
		t.Error("43 numaralı domainin yazımı 42'nin zone yazımını bekledi (kilit domain başına olmalı)")
	}
	close(birak)
	if k := <-kod42; k != http.StatusCreated {
		t.Errorf("42 için 201 beklenirdi: %d", k)
	}
}

// Doğrulama/sahiplik hataları (işlem içinde, zone'a hiç varmadan) BAŞARISIZ denetim satırı yazmaz:
// ok=0 satırı yalnız "zone yazılamadı, geri alındı" içindir.
func TestDenetim_IslemIciRetlerSatirYazmaz(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	rid := zi_ekle(t, db, 42, "www", "A", "203.0.113.5", 600, 0, 1)
	kotu := zi_ekle(t, db, 42, "kotu", "A", "ip-degil", 600, 0, 0)
	n0 := dk_sayi(t, db, `SELECT COUNT(*) FROM audit_log`)
	if kod, _ := zi_put(t, db, fmt.Sprint(rid), `{"deger":"abc"}`); kod != http.StatusBadRequest {
		t.Fatalf("400 beklenirdi: %d", kod)
	}
	if kod, _ := zi_put(t, db, "999999", `{"ttl":60}`); kod != http.StatusNotFound {
		t.Fatalf("404 beklenirdi: %d", kod)
	}
	b, _ := json.Marshal(map[string]any{"ids": []int64{kotu}, "aktif": true})
	if kod, _ := zi_toplu(t, db, "toplu-durum", string(b), func(h *Handlers) http.HandlerFunc { return h.TopluDurum }); kod != http.StatusBadRequest {
		t.Fatalf("400 beklenirdi: %d", kod)
	}
	if n1 := dk_sayi(t, db, `SELECT COUNT(*) FROM audit_log`); n1 != n0 {
		t.Errorf("işlem içinde reddedilen istekler denetim satırı yazmamalı: %d → %d", n0, n1)
	}
}

// Arayüz formu tüm alanları gönderir: değerler aynıysa içerik değişmemiştir, eski kaydın
// TTL'si formdan değişebilir; değer gerçekten değişirse doğrulama çalışır.
func TestUpdate_FormAyniDegerlerleEskiKayitTTLDegisebilir(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	rid := zi_ekle(t, db, 42, "eski", "TXT", `a\"b`, 600, 0, 1)
	form := func(deger string, ttl int) string {
		b, _ := json.Marshal(map[string]any{"ad": "eski", "tip": "txt", "deger": deger, "ttl": ttl, "oncelik": 0, "aktif": true})
		return string(b)
	}
	if kod, govde := zi_put(t, db, fmt.Sprint(rid), form(`a\"b`, 120)); kod != http.StatusOK {
		t.Errorf("aynı değerli form (yalnız TTL farklı) doğrulamaya takılmamalı: %d %s", kod, govde)
	}
	if n := dk_sayi(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM dns_records WHERE id=%d AND ttl=120`, rid)); n != 1 {
		t.Error("TTL güncellenmedi")
	}
	if kod, govde := zi_put(t, db, fmt.Sprint(rid), form(`c\"d`, 120)); kod != http.StatusBadRequest {
		t.Errorf("değer değişince doğrulama çalışmalı: %d %s", kod, govde)
	}
}
