package dns

// zone_hata_test.go — bayi kapsamı (son ham hata yerleri): zone yazımı hatası.
// WriteZone'un include adımı (updateZoneIncludes) SUNUCUDAKİ TÜM zone'ları
// named-checkconf ile sınar; çıktısı komşu kiracıların zone adlarını ve
// /var/named/<başka-alan>.zone yollarını taşır. DNSSEC (POST /dns/dnssec) ve
// içe aktarma (POST /dns/ice-aktar) uçları — MusteriScope — bu metni bayiye ve
// müşteriye ham DÖNDÜRMEZ. Kiracının KENDİ zone'unun named-checkzone hatası
// herkese aynen döner (kaydını düzeltebilsin). Yöneticinin metni birebir aynı.
//
// zoneUygula dikişle sahte — /var/named, /etc/named ve named'e dokunulmaz.
// Zone yazımı artık DB işlemiyle birlikte atomik olduğundan (zoneliIslem) DNSSEC ve
// içe aktarma testleri geçici MariaDB ister (GOSP_TEST_GECICI_DSN yoksa ATLANIR;
// yardımcılar template_kapsam_test.go). Zone hatası → DB değişikliği geri alınır.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/auth"
	"girginospanel/internal/middleware"
)

var zh_sir = []byte("dns-zone-hata-test-anahtari-0123456789abc")

func zh_tokenlar(t *testing.T) map[string]string {
	t.Helper()
	admin, err := auth.IssueAt(zh_sir, 3600, 1, "root", "admin", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	bayi, err := auth.IssueResellerAt(zh_sir, 3600, 7, "bayi7", 7, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	musteri, _, err := auth.GenerateMusteri(zh_sir, auth.MusteriClaims{
		FTPHesapID: 420, DomainID: 42, Kullanici: "m", AlanAdi: "bayialan.test"}, 3600)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{"admin": admin, "bayi": bayi, "müşteri": musteri}
}

// named-checkconf çıktısı biçiminde (updateZoneIncludes): komşu zone'lar.
var zh_include = fmt.Errorf("zone-include geçersiz (canlıya inmedi): %s",
	`/etc/named/girginospanel-zones.conf.tmp:14: zone 'komsu-kok.test': already exists previous definition: /etc/named/girginospanel-zones.conf.tmp:9
/etc/named/girginospanel-zones.conf.tmp:21: key-directory '/var/named/dynamic/komsubayi-magaza.test' does not exist`)

var zh_izler = []string{"komsu-kok.test", "komsubayi-magaza", "/etc/named", "/var/named/dynamic"}

// Kiracının kendi zone'unun named-checkzone hatası (yalnız kendi kayıtları).
var zh_kendi = &zoneGecersizHatasi{alan: "bayialan.test",
	cikti: "/var/named/bayialan.test.zone.tmp:9: www.bayialan.test: bad owner name (check-names)\nzone bayialan.test/IN: loading from master file failed: bad owner name"}

func zh_dikis(t *testing.T, hata error) {
	t.Helper()
	eski := zoneUygula
	t.Cleanup(func() { zoneUygula = eski })
	zoneUygula = func(context.Context, sorgucu, int64) (func(), bool, error) {
		if hata != nil {
			return nil, false, hata
		}
		return func() {}, false, nil
	}
}

func zh_cagir(t *testing.T, db *sql.DB, tok, yontem, desen, yol, tip, govde string, isle func(*Handlers) http.HandlerFunc) (int, string) {
	t.Helper()
	r := chi.NewRouter()
	r.With(middleware.RequireAuth(zh_sir)).Method(yontem, desen, isle(&Handlers{DB: db}))
	req := httptest.NewRequest(yontem, yol, strings.NewReader(govde))
	req.Header.Set("Authorization", "Bearer "+tok)
	if tip != "" {
		req.Header.Set("Content-Type", tip)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func zh_alan(t *testing.T, govde, alan string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(govde), &m); err != nil {
		t.Fatalf("yanıt çözülemedi: %v (%s)", err, govde)
	}
	s, _ := m[alan].(string)
	return s
}

func zh_izYok(t *testing.T, neresi, metin string) {
	t.Helper()
	for _, iz := range zh_izler {
		if strings.Contains(metin, iz) {
			t.Errorf("%s: yabancı iz %q var: %s", neresi, iz, metin)
		}
	}
}

func TestDNSSEC_ZoneHatasi_BayiMusteriyeKomsuZoneSizmaz_KendiHatasiKalir(t *testing.T) {
	tok := zh_tokenlar(t)
	db := zh_db(t)
	cagir := func(rol string, hata error) (int, string) {
		zh_dikis(t, hata)
		return zh_cagir(t, db, tok[rol], http.MethodPost, "/domains/{id}/dns/dnssec", "/domains/42/dns/dnssec",
			"", `{"aktif":false}`, func(h *Handlers) http.HandlerFunc { return h.PostDNSSEC })
	}
	const onek = "zone güncellenemedi: "
	// 1) Sunucu geneli include hatası: bayi/müşteri genel mesaj; yönetici ham.
	for _, rol := range []string{"bayi", "müşteri"} {
		kod, govde := cagir(rol, zh_include)
		if kod != http.StatusInternalServerError {
			t.Fatalf("%s: 500 beklenirdi: %d %s", rol, kod, govde)
		}
		zh_izYok(t, rol+" yanıtı", govde)
		if m := zh_alan(t, govde, "hata"); !strings.HasPrefix(m, onek) || !strings.Contains(m, "günlüğüne") {
			t.Errorf("%s: genel mesaj beklenirdi: %q", rol, m)
		}
	}
	if kod, govde := cagir("admin", zh_include); kod != http.StatusInternalServerError || zh_alan(t, govde, "hata") != onek+zh_include.Error() {
		t.Errorf("yönetici metni birebir aynı kalmalı: %d %s", kod, govde)
	}
	// 2) Kendi zone'unun named-checkzone hatası: herkes aynen görür.
	for _, rol := range []string{"bayi", "müşteri", "admin"} {
		if kod, govde := cagir(rol, zh_kendi); kod != http.StatusInternalServerError || zh_alan(t, govde, "hata") != onek+zh_kendi.Error() {
			t.Errorf("%s: kendi zone hatası aynen görünmeli: %d %s", rol, kod, govde)
		}
	}
}

// zh_db — geçici veritabanı (işlem gerektiren içe aktarma için).
func zh_db(t *testing.T) *sql.DB {
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
	ad := fmt.Sprintf("gosp_test_zonehata_%d", time.Now().UnixNano())
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
		is_demo TINYINT(1) NOT NULL DEFAULT 0, reseller_id BIGINT NOT NULL DEFAULT 0) ENGINE=InnoDB`)
	dk_goc(t, db, "0007_dns.sql")
	dk_goc(t, db, "0025_dns_soa.sql")
	dk_goc(t, db, "0032_dns_dnssec.sql")
	dk_exec(t, db, `INSERT INTO domains (id, alan_adi, reseller_id) VALUES (42, 'bayialan.test', 7)`)
	return db
}

func TestIceAktar_ZoneHatasi_BayiMusteriyeKomsuZoneSizmaz_KendiHatasiKalir(t *testing.T) {
	db := zh_db(t)
	tok := zh_tokenlar(t)
	cagir := func(rol string, hata error) (int, string) {
		zh_dikis(t, hata)
		return zh_cagir(t, db, tok[rol], http.MethodPost, "/domains/{id}/dns/ice-aktar", "/domains/42/dns/ice-aktar?mod=degistir",
			"text/plain", "$ORIGIN bayialan.test.\n@ 3600 IN A 203.0.113.10\nwww 3600 IN A 203.0.113.10\n",
			func(h *Handlers) http.HandlerFunc { return h.IceAktar })
	}
	// Zone yazımı başarısız → içe aktarma TÜMÜYLE geri alınır ve istek HATA döner
	// (eskiden 200 + "uyari" dönüp bozuk kayıtlar DB'de kalıyordu).
	const onek = zoneGeriAlindi
	for _, rol := range []string{"bayi", "müşteri"} {
		kod, govde := cagir(rol, zh_include)
		if kod != http.StatusInternalServerError {
			t.Fatalf("%s: 500 beklenirdi: %d %s", rol, kod, govde)
		}
		zh_izYok(t, rol+" yanıtı", govde)
		if m := zh_alan(t, govde, "hata"); !strings.HasPrefix(m, onek) || !strings.Contains(m, "günlüğüne") {
			t.Errorf("%s: genel hata beklenirdi: %q", rol, m)
		}
	}
	if kod, govde := cagir("admin", zh_include); kod != http.StatusInternalServerError || zh_alan(t, govde, "hata") != onek+zh_include.Error() {
		t.Errorf("yönetici metni birebir aynı kalmalı: %d %s", kod, govde)
	}
	for _, rol := range []string{"bayi", "müşteri", "admin"} {
		if kod, govde := cagir(rol, zh_kendi); kod != http.StatusUnprocessableEntity || zh_alan(t, govde, "hata") != onek+zh_kendi.Error() {
			t.Errorf("%s: kendi zone hatası aynen görünmeli (422): %d %s", rol, kod, govde)
		}
	}
	// Hiçbir deneme kayıt bırakmadı (işlem geri alındı).
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM dns_records WHERE domain_id=42`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("zone hatasında içe aktarılan kayıtlar geri alınmalıydı: %d (%v)", n, err)
	}
	// Zone sağlamsa aynı istek kayıtları ekler.
	if kod, govde := cagir("bayi", nil); kod != http.StatusOK {
		t.Fatalf("başarılı içe aktarma 200 beklenirdi: %d %s", kod, govde)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM dns_records WHERE domain_id=42`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("içe aktarılan kayıt sayısı 2 beklenirdi: %d (%v)", n, err)
	}
}

// zoneGecersizHatasi metni eski fmt.Errorf metniyle birebir aynı (yönetici ve
// kiracı aynı metni görmeye devam eder).
func TestZoneGecersizHatasi_MetinAyni(t *testing.T) {
	e := &zoneGecersizHatasi{alan: "a.test", cikti: "satir"}
	if e.Error() != fmt.Sprintf("zone geçersiz (%s): %s", "a.test", "satir") {
		t.Fatalf("metin değişti: %q", e.Error())
	}
	var zg *zoneGecersizHatasi
	if !errors.As(fmt.Errorf("sar: %w", e), &zg) {
		t.Fatalf("sarılı hata tanınmalı")
	}
}
