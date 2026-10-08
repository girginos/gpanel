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

func TestSilinenKaydet_DizinTasinirKayitAcilir(t *testing.T) {
	db := silinenDB(t)
	kok := silinenOrtam(t)
	ctx := context.Background()

	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, backup_retention) VALUES (9101,'silinen1.test','c_silinen1',3)`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, tip, dosya, boyut_b) VALUES (9101,'oto','c_silinen1-20261001.tar.gz',5)`)
	if err := os.MkdirAll(filepath.Join(kok, "c_silinen1"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kok, "c_silinen1", "c_silinen1-20261001.tar.gz"), []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := SilinenKaydet(ctx, db, 9101, "silinen1.test", "c_silinen1"); err != nil {
		t.Fatalf("SilinenKaydet: %v", err)
	}
	if _, err := os.Stat(filepath.Join(kok, "c_silinen1")); !os.IsNotExist(err) {
		t.Fatalf("eski dizin yerinde kalmamalı: %v", err)
	}
	var id int64
	var dizin string
	var gun int
	var adet int
	if err := db.QueryRow(`SELECT id, dizin, DATEDIFF(son_kullanma, silinme), dosya_sayisi FROM cp_silinen_yedek WHERE eski_domain_id=9101`).
		Scan(&id, &dizin, &gun, &adet); err != nil {
		t.Fatal(err)
	}
	// slot 3 < alt sınır 7 → 7 gün
	if gun != 7 || adet != 1 || !strings.HasPrefix(dizin, "_silinen/") {
		t.Fatalf("gun=%d adet=%d dizin=%q", gun, adet, dizin)
	}
	if b, err := os.ReadFile(filepath.Join(kok, dizin, "c_silinen1-20261001.tar.gz")); err != nil || string(b) != "12345" {
		t.Fatalf("yedek dosyası saklamada olmalı: %v %q", err, b)
	}
}

func TestSilinenKaydet_AyarSureyiBelirler(t *testing.T) {
	db := silinenDB(t)
	silinenOrtam(t)
	ctx := context.Background()
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, backup_retention) VALUES (9102,'silinen2.test','c_silinen2',14)`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, tip, dosya, boyut_b) VALUES (9102,'oto','c_silinen2-1.tar.gz',5)`)

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
	// ayarsız → slot (14)
	if err := SilinenKaydet(ctx, db, 9102, "silinen2.test", "c_silinen2"); err != nil {
		t.Fatal(err)
	}
	if g, n := gunOku(); n || g != 14 {
		t.Fatalf("slot: gun=%d null=%v", g, n)
	}
	// sabit 30
	g113_exec(t, db, `INSERT INTO cp_ayarlar (anahtar, deger) VALUES ('silinen_yedek_gun','30') ON DUPLICATE KEY UPDATE deger='30'`)
	if err := SilinenKaydet(ctx, db, 9102, "silinen2.test", "c_silinen2"); err != nil {
		t.Fatal(err)
	}
	if g, n := gunOku(); n || g != 30 {
		t.Fatalf("sabit: gun=%d null=%v", g, n)
	}
	// 0 → otomatik silme yok (NULL)
	g113_exec(t, db, `UPDATE cp_ayarlar SET deger='0' WHERE anahtar='silinen_yedek_gun'`)
	if err := SilinenKaydet(ctx, db, 9102, "silinen2.test", "c_silinen2"); err != nil {
		t.Fatal(err)
	}
	if _, n := gunOku(); !n {
		t.Fatal("0 = son_kullanma NULL olmalı")
	}
}

func TestSilinenKaydet_SaklanacakYoksaKayitAcmaz(t *testing.T) {
	db := silinenDB(t)
	silinenOrtam(t)
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (9103,'bos.test','c_bos')`)
	if err := SilinenKaydet(context.Background(), db, 9103, "bos.test", "c_bos"); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM cp_silinen_yedek WHERE eski_domain_id=9103`).Scan(&n)
	if n != 0 {
		t.Fatalf("kayıt açılmamalı, n=%d", n)
	}
}

func TestSilinenKaydet_GecersizSKDokunmaz(t *testing.T) {
	db := silinenDB(t)
	kok := silinenOrtam(t)
	_ = os.MkdirAll(filepath.Join(kok, "disari"), 0o700)
	for _, sk := range []string{"", "root", "c_../disari", "c_a/b"} {
		if err := SilinenKaydet(context.Background(), db, 9104, "x.test", sk); err != nil {
			t.Fatalf("sk=%q: %v", sk, err)
		}
	}
	if _, err := os.Stat(filepath.Join(kok, "disari")); err != nil {
		t.Fatal("geçersiz sk dışarıya dokunmamalı")
	}
}

func TestSilinenSupur_YalnizSuresiDolaniTemizler(t *testing.T) {
	db := silinenDB(t)
	kok := silinenOrtam(t)
	ctx := context.Background()
	mk := func(id int64, alan, sk string, dolmus bool) string {
		g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (?,?,?)`, id, alan, sk)
		g113_exec(t, db, `INSERT INTO backups (domain_id, tip, dosya, boyut_b) VALUES (?,'oto',?,3)`, id, sk+"-1.tar.gz")
		_ = os.MkdirAll(filepath.Join(kok, sk), 0o700)
		_ = os.WriteFile(filepath.Join(kok, sk, sk+"-1.tar.gz"), []byte("abc"), 0o600)
		if err := SilinenKaydet(ctx, db, id, alan, sk); err != nil {
			t.Fatal(err)
		}
		if dolmus {
			g113_exec(t, db, `UPDATE cp_silinen_yedek SET son_kullanma=DATE_SUB(NOW(), INTERVAL 1 HOUR) WHERE eski_domain_id=?`, id)
		}
		var dizin string
		_ = db.QueryRow(`SELECT dizin FROM cp_silinen_yedek WHERE eski_domain_id=?`, id).Scan(&dizin)
		return filepath.Join(kok, dizin)
	}
	dolan := mk(9111, "dolan.test", "c_dolan", true)
	taze := mk(9112, "taze.test", "c_taze", false)

	SilinenSupur(ctx, db)

	if _, err := os.Stat(dolan); !os.IsNotExist(err) {
		t.Fatalf("süresi dolan dizin silinmeli: %v", err)
	}
	if _, err := os.Stat(taze); err != nil {
		t.Fatalf("taze dizin KALMALI: %v", err)
	}
	var d1, d2 string
	_ = db.QueryRow(`SELECT durum FROM cp_silinen_yedek WHERE eski_domain_id=9111`).Scan(&d1)
	_ = db.QueryRow(`SELECT durum FROM cp_silinen_yedek WHERE eski_domain_id=9112`).Scan(&d2)
	if d1 != "temizlendi" || d2 != "saklaniyor" {
		t.Fatalf("durumlar: %s %s", d1, d2)
	}
}

func TestSilinenAbsDizin_YolKacisiReddedilir(t *testing.T) {
	silinenOrtam(t)
	for _, k := range []string{"", "../etc", "_silinen/../../etc", "/etc/passwd", "c_x", "_silinen", "_silinen/.."} {
		if g := silinenAbsDizin(k); g != "" {
			t.Errorf("%q kabul edildi: %q", k, g)
		}
	}
	if g := silinenAbsDizin("_silinen/5-c_x"); g == "" || !strings.HasSuffix(g, "_silinen/5-c_x") {
		t.Errorf("geçerli yol reddedildi: %q", g)
	}
}
