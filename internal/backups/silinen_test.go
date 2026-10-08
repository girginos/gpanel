package backups

// silinen_test.go — silinen hosting yedeklerinin saklanması (çöp kutusu). Gerçek
// BackupRoot'a YAZILMAZ: yedekKok geçici dizine çevrilir. Geçici MariaDB gerekir.

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// silinenDB: g113_db iskeletine bu testlerin okuduğu kolonları + 0217 göçünü ekler.
func silinenDB(t *testing.T) *sql.DB {
	t.Helper()
	db := g113_db(t)
	g113_exec(t, db, `ALTER TABLE domains ADD COLUMN alan_adi VARCHAR(253) NOT NULL DEFAULT '', ADD COLUMN sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '',
		ADD COLUMN reseller_id BIGINT UNSIGNED NOT NULL DEFAULT 0, ADD COLUMN backup_retention TINYINT NOT NULL DEFAULT 7`)
	g113_exec(t, db, `ALTER TABLE backups ADD COLUMN tip VARCHAR(16) NOT NULL DEFAULT 'oto', ADD COLUMN boyut_b BIGINT NOT NULL DEFAULT 0`)
	g113_exec(t, db, `CREATE TABLE cp_ayarlar (anahtar VARCHAR(64) PRIMARY KEY, deger TEXT NOT NULL)`)
	g113_exec(t, db, `CREATE TABLE backup_destinations (id BIGINT AUTO_INCREMENT PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL, tip VARCHAR(8) NOT NULL,
		host VARCHAR(253) NOT NULL, port INT NOT NULL, kullanici VARCHAR(128) NOT NULL, parola VARCHAR(255) NOT NULL, uzak_dizin VARCHAR(255) NOT NULL)`)
	body, err := os.ReadFile("../../migrations/0217_silinen_yedek.sql")
	if err != nil {
		t.Fatal(err)
	}
	var temiz []string
	for _, satir := range strings.Split(string(body), "\n") {
		if x := strings.TrimSpace(satir); x == "" || strings.HasPrefix(x, "--") {
			continue
		}
		temiz = append(temiz, satir)
	}
	for _, ifade := range strings.Split(strings.Join(temiz, "\n"), ";") {
		if x := strings.TrimSpace(ifade); x != "" {
			g113_exec(t, db, x)
		}
	}
	return db
}

func silinenOrtam(t *testing.T) (kok string) {
	t.Helper()
	kok = t.TempDir()
	eski := yedekKok
	yedekKok = kok
	t.Cleanup(func() { yedekKok = eski })
	return kok
}

func silinenDosya(t *testing.T, kok, sk, ad string, yas time.Duration) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(kok, sk), 0o700); err != nil {
		t.Fatal(err)
	}
	y := filepath.Join(kok, sk, ad)
	if err := os.WriteFile(y, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := time.Now().Add(-yas)
	if err := os.Chtimes(y, m, m); err != nil {
		t.Fatal(err)
	}
}

func TestSilinenIsaretle_DizineDokunmazIzBirakir(t *testing.T) {
	db := silinenDB(t)
	kok := silinenOrtam(t)
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, backup_retention) VALUES (9101,'silinen1.test','c_silinen1',3)`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, tip, dosya, boyut_b) VALUES (9101,'oto','c_silinen1-20261001.tar.gz',5)`)
	silinenDosya(t, kok, "c_silinen1", "c_silinen1-20261001.tar.gz", time.Hour)

	SilinenIsaretle(context.Background(), db, 9101, "silinen1.test", "c_silinen1")

	if b, err := os.ReadFile(filepath.Join(kok, "c_silinen1", "c_silinen1-20261001.tar.gz")); err != nil || string(b) != "12345" {
		t.Fatalf("yedek dosyası OLDUĞU YERDE kalmalı: %v", err)
	}
	var dizin, js string
	var gun int
	if err := db.QueryRow(`SELECT dizin, dosyalar, DATEDIFF(son_kullanma, silinme) FROM cp_silinen_yedek WHERE eski_domain_id=9101`).Scan(&dizin, &js, &gun); err != nil {
		t.Fatal(err)
	}
	// slot 3 < alt sınır 7 → 7 gün
	if gun != 7 || dizin != "c_silinen1" || !strings.Contains(js, "c_silinen1-20261001.tar.gz") {
		t.Fatalf("gun=%d dizin=%q js=%s", gun, dizin, js)
	}
}

func TestSilinenIsaretle_AyarSureyiBelirler(t *testing.T) {
	db := silinenDB(t)
	silinenOrtam(t)
	ctx := context.Background()
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, backup_retention) VALUES (9102,'silinen2.test','c_silinen2',14)`)
	gunOku := func() (gun int, null bool) {
		var g *int
		if err := db.QueryRow(`SELECT DATEDIFF(son_kullanma, silinme) FROM cp_silinen_yedek WHERE eski_domain_id=9102 ORDER BY id DESC LIMIT 1`).Scan(&g); err != nil {
			t.Fatal(err)
		}
		if g == nil {
			return 0, true
		}
		return *g, false
	}
	SilinenIsaretle(ctx, db, 9102, "silinen2.test", "c_silinen2")
	if g, n := gunOku(); n || g != 14 {
		t.Fatalf("slot: gun=%d null=%v", g, n)
	}
	g113_exec(t, db, `INSERT INTO cp_ayarlar (anahtar, deger) VALUES ('silinen_yedek_gun','30') ON DUPLICATE KEY UPDATE deger='30'`)
	SilinenIsaretle(ctx, db, 9102, "silinen2.test", "c_silinen2")
	if g, n := gunOku(); n || g != 30 {
		t.Fatalf("sabit: gun=%d null=%v", g, n)
	}
	g113_exec(t, db, `UPDATE cp_ayarlar SET deger='0' WHERE anahtar='silinen_yedek_gun'`)
	SilinenIsaretle(ctx, db, 9102, "silinen2.test", "c_silinen2")
	if _, n := gunOku(); !n {
		t.Fatal("0 = son_kullanma NULL olmalı")
	}
}

func TestSilinenIsaretle_GecersizSKIzAcmaz(t *testing.T) {
	db := silinenDB(t)
	kok := silinenOrtam(t)
	_ = os.MkdirAll(filepath.Join(kok, "disari"), 0o700)
	for _, sk := range []string{"", "root", "c_../disari", "c_a/b", "c_BUYUK", "c_" + strings.Repeat("a", 60)} {
		SilinenIsaretle(context.Background(), db, 9104, "x.test", sk)
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM cp_silinen_yedek`).Scan(&n)
	if n != 0 {
		t.Fatalf("geçersiz sk için kayıt açılmamalı, n=%d", n)
	}
	if _, err := os.Stat(filepath.Join(kok, "disari")); err != nil {
		t.Fatal("dışarıya dokunulmamalı")
	}
}

// Silme yolu yedek altyapısı bozuk olsa da ASLA hata/panik vermemeli (silme yedeklerden bağımsız).
func TestSilinenIsaretle_DBBozuksaSessizGecer(t *testing.T) {
	db := silinenDB(t)
	silinenOrtam(t)
	g113_exec(t, db, `DROP TABLE cp_silinen_yedek`)
	g113_exec(t, db, `DROP TABLE backup_destinations`)
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (9105,'bozuk.test','c_bozuk')`)
	SilinenIsaretle(context.Background(), db, 9105, "bozuk.test", "c_bozuk")  // panik/sonsuz bekleme olmamalı
	SilinenIsaretle(context.Background(), nil, 9105, "bozuk.test", "c_bozuk") // nil db: panik yutulur
}

func silinenKayit(t *testing.T, db *sql.DB, id int64, alan, sk string, dolmus bool) {
	t.Helper()
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (?,?,?)`, id, alan, sk)
	SilinenIsaretle(context.Background(), db, id, alan, sk)
	g113_exec(t, db, `DELETE FROM domains WHERE id=?`, id) // hosting silindi
	g113_exec(t, db, `UPDATE cp_silinen_yedek SET silinme=DATE_SUB(NOW(), INTERVAL 10 DAY) WHERE eski_domain_id=?`, id)
	if dolmus {
		g113_exec(t, db, `UPDATE cp_silinen_yedek SET son_kullanma=DATE_SUB(NOW(), INTERVAL 1 HOUR) WHERE eski_domain_id=?`, id)
	}
}

func TestSilinenSupur_YalnizSuresiDolaniTemizler(t *testing.T) {
	db := silinenDB(t)
	kok := silinenOrtam(t)
	silinenDosya(t, kok, "c_dolan", "c_dolan-1.tar.gz", 20*24*time.Hour)
	silinenDosya(t, kok, "c_taze", "c_taze-1.tar.gz", 20*24*time.Hour)
	silinenKayit(t, db, 9111, "dolan.test", "c_dolan", true)
	silinenKayit(t, db, 9112, "taze.test", "c_taze", false)

	SilinenSupur(context.Background(), db)

	if _, err := os.Stat(filepath.Join(kok, "c_dolan")); !os.IsNotExist(err) {
		t.Fatalf("süresi dolanın dosyası ve boş dizini silinmeli: %v", err)
	}
	if _, err := os.Stat(filepath.Join(kok, "c_taze", "c_taze-1.tar.gz")); err != nil {
		t.Fatalf("taze KALMALI: %v", err)
	}
	var d1, d2 string
	_ = db.QueryRow(`SELECT durum FROM cp_silinen_yedek WHERE eski_domain_id=9111`).Scan(&d1)
	_ = db.QueryRow(`SELECT durum FROM cp_silinen_yedek WHERE eski_domain_id=9112`).Scan(&d2)
	if d1 != "temizlendi" || d2 != "saklaniyor" {
		t.Fatalf("durumlar: %s %s", d1, d2)
	}
}

// sk yeniden kullanılırsa (aynı alan adı yeniden açıldı) yeni müşterinin yedekleri ASLA silinmez/sızmaz.
func TestSilinenSupur_SKDevralindiysaYeniHostingeDokunmaz(t *testing.T) {
	db := silinenDB(t)
	kok := silinenOrtam(t)
	silinenDosya(t, kok, "c_ayni", "c_ayni-eski.tar.gz", 20*24*time.Hour) // silinen hesabın
	silinenKayit(t, db, 9121, "ayni.test", "c_ayni", true)
	// aynı alan adı yeniden açıldı: yeni domain + kendi yedekleri
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (9122,'ayni.test','c_ayni')`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, tip, dosya, boyut_b) VALUES (9122,'oto','c_ayni-yeni-kayitli.tar.gz',5)`)
	silinenDosya(t, kok, "c_ayni", "c_ayni-yeni-kayitli.tar.gz", time.Hour)
	silinenDosya(t, kok, "c_ayni", "c_ayni-yeni-kayitsiz.tar.gz", time.Minute) // silinmeden SONRA yazılmış

	dos, devralindi := silinenDosyalari(context.Background(), db, "c_ayni", time.Now().Add(-10*24*time.Hour))
	if !devralindi || len(dos) != 1 {
		t.Fatalf("yalnız eski dosya sayılmalı: devralindi=%v dos=%v", devralindi, dos)
	}
	if _, ok := dos["c_ayni-eski.tar.gz"]; !ok {
		t.Fatalf("eski dosya listede olmalı: %v", dos)
	}

	SilinenSupur(context.Background(), db)

	for ad, var_ := range map[string]bool{"c_ayni-eski.tar.gz": false, "c_ayni-yeni-kayitli.tar.gz": true, "c_ayni-yeni-kayitsiz.tar.gz": true} {
		_, err := os.Stat(filepath.Join(kok, "c_ayni", ad))
		if (err == nil) != var_ {
			t.Errorf("%s: var=%v olmalıydı (err=%v)", ad, var_, err)
		}
	}
}

func TestSilinenDosyalari_SadeceTarGzVeYolKacisiYok(t *testing.T) {
	db := silinenDB(t)
	kok := silinenOrtam(t)
	silinenDosya(t, kok, "c_x", "c_x-1.tar.gz", time.Hour)
	silinenDosya(t, kok, "c_x", "notlar.txt", time.Hour)
	_ = os.MkdirAll(filepath.Join(kok, "c_x", "alt.tar.gz"), 0o700)
	dos, _ := silinenDosyalari(context.Background(), db, "c_x", time.Time{})
	if len(dos) != 1 {
		t.Fatalf("yalnız düz *.tar.gz dosyası: %v", dos)
	}
	for _, sk := range []string{"", "../etc", "c_x/../../etc", "/etc", "c_X"} {
		if d, _ := silinenDosyalari(context.Background(), db, sk, time.Time{}); len(d) != 0 {
			t.Errorf("%q kabul edildi: %v", sk, d)
		}
	}
}
