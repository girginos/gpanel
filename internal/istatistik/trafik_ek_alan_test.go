package istatistik

// trafik_ek_alan_test.go — ek alan adı ve alt alan access log'ları hesabın aylık
// trafiğine sayılır (imleç anahtarı: hesap + log dosyası). DB: GOSP_TEST_GECICI_DSN.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"girginospanel/internal/logs"
)

func trafikSatir(gun time.Time, bayt int) string {
	return fmt.Sprintf(`1.2.3.4 - - [%s +0000] "GET /x HTTP/1.1" 200 %d "-" "ua"`+"\n", gun.Format("02/Jan/2006:15:04:05"), bayt)
}

func TestAggregateDomain_EkAlanVeAltAlanSayilir(t *testing.T) {
	db := tg_geciciDB(t)
	for _, q := range []string{
		`ALTER TABLE domains ADD COLUMN trafik_kb BIGINT NOT NULL DEFAULT 0`,
		`CREATE TABLE domain_trafik_imlec (domain_id BIGINT UNSIGNED NOT NULL PRIMARY KEY, ofset BIGINT NOT NULL DEFAULT 0, boyut BIGINT NOT NULL DEFAULT 0)`,
		`CREATE TABLE domain_trafik_imlec_dosya (domain_id BIGINT UNSIGNED NOT NULL, log_ad VARCHAR(253) NOT NULL,
			ofset BIGINT NOT NULL DEFAULT 0, boyut BIGINT NOT NULL DEFAULT 0, PRIMARY KEY (domain_id, log_ad))`,
		`CREATE TABLE ek_alan_adlari (id BIGINT UNSIGNED PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL, alan_adi VARCHAR(253) NOT NULL)`,
		`CREATE TABLE subdomanlar (id INT PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL, tam_ad VARCHAR(253) NOT NULL)`,
		`INSERT INTO ek_alan_adlari VALUES (10,42,'test5.com'),(20,43,'baska.com')`,
		`INSERT INTO subdomanlar VALUES (100,42,'blog.musteri.test')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("şema: %v", err)
		}
	}
	eski := logs.LogDizin
	logs.LogDizin = t.TempDir()
	defer func() { logs.LogDizin = eski }()
	yaz := func(ad, icerik string, ekle bool) {
		fl := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
		if ekle {
			fl = os.O_CREATE | os.O_WRONLY | os.O_APPEND
		}
		f, err := os.OpenFile(filepath.Join(logs.LogDizin, ad), fl, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.WriteString(icerik)
		_ = f.Close()
	}
	simdi := time.Now().UTC()
	ay := simdi.Format("2006-01")
	yaz("musteri.test.access.log", trafikSatir(simdi, 1000), false)
	yaz("test5.com.access.log", trafikSatir(simdi, 20000), false)
	yaz("blog.musteri.test.access.log", trafikSatir(simdi, 300000), false)
	yaz("baska.com.access.log", trafikSatir(simdi, 7777777), false) // başka hesap: 42'ye girmez

	topla := func() (b int64, kb int64) {
		_ = db.QueryRow(`SELECT COALESCE(SUM(bytes),0) FROM domain_trafik WHERE domain_id=42 AND yil_ay=?`, ay).Scan(&b)
		_ = db.QueryRow(`SELECT trafik_kb FROM domains WHERE id=42`).Scan(&kb)
		return
	}
	aggregateDomain(db, 42, "musteri.test")
	if b, kb := topla(); b != 321000 || kb != 321000/1024 {
		t.Fatalf("ana+ek+alt = %d (kb %d), beklenen 321000", b, kb)
	}
	// ikinci tur: yeni satır yok -> çift sayım yok
	aggregateDomain(db, 42, "musteri.test")
	if b, _ := topla(); b != 321000 {
		t.Fatalf("çift sayım: %d", b)
	}
	// yalnız ek alana yeni trafik: artımlı eklenir
	yaz("test5.com.access.log", trafikSatir(simdi, 5), true)
	aggregateDomain(db, 42, "musteri.test")
	if b, _ := topla(); b != 321005 {
		t.Fatalf("artımlı ek alan: %d", b)
	}
	// ek alan silinince imleci temizlenir
	if _, err := db.Exec(`DELETE FROM ek_alan_adlari WHERE id=10`); err != nil {
		t.Fatal(err)
	}
	aggregateDomain(db, 42, "musteri.test")
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM domain_trafik_imlec_dosya WHERE domain_id=42 AND log_ad='test5.com'`).Scan(&n)
	if n != 0 {
		t.Fatalf("silinen ek alanın imleci kaldı")
	}
}
