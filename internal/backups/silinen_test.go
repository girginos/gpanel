package backups

// silinen_test.go — silinen hosting yedeklerinin "yetim yedek" tasarımı (silinen.go + 0217 göçü).
// Gerçek BackupRoot'a YAZILMAZ: yedekKok geçici dizine çevrilir. Geçici MariaDB gerekir
// (GOSP_TEST_GECICI_DSN yoksa DB'li testler atlanır).

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const goc0217 = "../../migrations/0217_silinen_yedek.sql"

// silinenDBHam: g113_db iskeleti (FK'li backups) + bu testlerin okuduğu kolonlar; 0217 UYGULANMAMIŞ.
func silinenDBHam(t *testing.T) *sql.DB {
	t.Helper()
	db := g113_db(t)
	db.SetMaxOpenConns(8) // Liste: satırlar açıkken ikinci sorgu
	g113_exec(t, db, `ALTER TABLE domains ADD COLUMN alan_adi VARCHAR(253) NOT NULL DEFAULT '', ADD COLUMN sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '',
		ADD COLUMN reseller_id BIGINT UNSIGNED NULL, ADD COLUMN backup_retention TINYINT NOT NULL DEFAULT 7, ADD COLUMN is_demo TINYINT NOT NULL DEFAULT 0`)
	g113_exec(t, db, `ALTER TABLE backups ADD COLUMN tip VARCHAR(16) NOT NULL DEFAULT 'oto', ADD COLUMN boyut_b BIGINT NOT NULL DEFAULT 0, ADD COLUMN notlar VARCHAR(255) NOT NULL DEFAULT '', ADD COLUMN created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP`)
	g113_exec(t, db, `CREATE TABLE cp_ayarlar (anahtar VARCHAR(64) PRIMARY KEY, deger TEXT NOT NULL)`)
	g113_exec(t, db, `CREATE TABLE backup_destinations (id BIGINT AUTO_INCREMENT PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL, tip VARCHAR(8) NOT NULL,
		host VARCHAR(253) NOT NULL, port INT NOT NULL, kullanici VARCHAR(128) NOT NULL, parola VARCHAR(255) NOT NULL, uzak_dizin VARCHAR(255) NOT NULL)`)
	return db
}

// silinenGocUygula: 0217'yi cmd/server koşucusuyla AYNI ayrıştırmayla uygular.
func silinenGocUygula(t *testing.T, db *sql.DB) {
	t.Helper()
	body, err := os.ReadFile(goc0217)
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
}

func silinenDB(t *testing.T) *sql.DB {
	t.Helper()
	db := silinenDBHam(t)
	silinenGocUygula(t, db)
	return db
}

// silinenUzakHedef: genel uzak hedefi (ulaşılamaz host) tanımlar → uzak silme HATA verir.
func silinenUzakHedef(t *testing.T, db *sql.DB) {
	t.Helper()
	g113_exec(t, db, `CREATE TABLE backup_genel_ayar (id INT PRIMARY KEY, aktif TINYINT NOT NULL DEFAULT 1, min_bos_gb INT NOT NULL DEFAULT 0, max_depo_gb INT NOT NULL DEFAULT 0,
		uzak_aktif TINYINT NOT NULL DEFAULT 1, uzak_tip VARCHAR(8) NOT NULL DEFAULT 'ftp', uzak_host VARCHAR(253) NOT NULL DEFAULT '', uzak_port INT NOT NULL DEFAULT 21,
		uzak_kullanici VARCHAR(128) NOT NULL DEFAULT '', uzak_parola VARCHAR(512) NOT NULL DEFAULT '', uzak_dizin VARCHAR(255) NOT NULL DEFAULT '',
		uzak_yerel_sil TINYINT NOT NULL DEFAULT 0, son_yukleme TIMESTAMP NULL, son_durum VARCHAR(16) NOT NULL DEFAULT '', son_hata VARCHAR(1000) NOT NULL DEFAULT '')`)
	g113_exec(t, db, `INSERT INTO backup_genel_ayar (id, uzak_host) VALUES (1,'ornek.invalid')`)
}

func silinenOrtam(t *testing.T) (kok string) {
	t.Helper()
	kok = t.TempDir()
	eski := yedekKok
	yedekKok = kok
	t.Cleanup(func() { yedekKok = eski })
	return kok
}

func silinenDosya(t *testing.T, kok, sk, ad string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(kok, sk), 0o700); err != nil {
		t.Fatal(err)
	}
	y := filepath.Join(kok, sk, ad)
	if err := os.WriteFile(y, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	return y
}

func varMi(yol string) bool { _, err := os.Lstat(yol); return err == nil }

// silinenYetim: domain satırı OLMAYAN yetim yedek satırı ekler. sonKullanma: SQL ifadesi ("NULL" ya da "DATE_SUB(NOW(), INTERVAL 1 DAY)").
func silinenYetim(t *testing.T, db *sql.DB, domainID int64, alan, sk, dosya string, rid int64, sonKullanma string) int64 {
	t.Helper()
	r, err := db.Exec(`INSERT INTO backups (domain_id, tip, dosya, boyut_b, alan_adi, sistem_kullanici, reseller_id, silinme, son_kullanma)
		VALUES (?,'oto',?,5,?,?,?,NOW(),`+sonKullanma+`)`, domainID, dosya, alan, sk, rid)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := r.LastInsertId()
	return id
}

func satirVarMi(t *testing.T, db *sql.DB, id int64) bool {
	t.Helper()
	return g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE id=?`, id) == 1
}

// ---------------------------------------------------------------- göç

func TestGoc0217_KosucuUyumu(t *testing.T) {
	body, err := os.ReadFile(goc0217)
	if err != nil {
		t.Fatal(err)
	}
	for i, satir := range strings.Split(string(body), "\n") {
		s := strings.TrimSpace(satir)
		if strings.HasPrefix(s, "--") {
			if strings.Contains(s, ";") {
				t.Errorf("satır %d: yorumda ';' var", i+1)
			}
			continue
		}
		if strings.Contains(s, "--") || strings.Contains(s, "/*") || strings.Contains(s, "#") {
			t.Errorf("satır %d: satır içi yorum: %s", i+1, s)
		}
	}
	m := string(body)
	for _, parca := range []string{"DROP FOREIGN KEY IF EXISTS fk_backup_domain", "ADD COLUMN IF NOT EXISTS alan_adi", "ADD COLUMN IF NOT EXISTS sistem_kullanici",
		"ADD COLUMN IF NOT EXISTS reseller_id", "ADD COLUMN IF NOT EXISTS silinme", "ADD COLUMN IF NOT EXISTS son_kullanma", "ADD KEY IF NOT EXISTS"} {
		if !strings.Contains(m, parca) {
			t.Errorf("göçte %q yok (idempotent olmalı)", parca)
		}
	}
	if strings.Contains(m, "CREATE TABLE") {
		t.Error("cp_silinen_yedek tasarımı kaldırıldı: göç tablo oluşturmamalı")
	}
}

func TestGoc0217_FKKalkarVeIdempotentVeDoldurur(t *testing.T) {
	db := silinenDBHam(t)
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES (9001,'eski.test','c_eski',77)`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya) VALUES (9001,'c_eski-20260101-000000.tar.gz')`)
	// Göçten ÖNCE FK var: domain silinince satır giderdi (kanıt).
	silinenGocUygula(t, db)
	silinenGocUygula(t, db) // ikinci koşu hata vermemeli
	var alan, sk string
	var rid int64
	if err := db.QueryRow(`SELECT alan_adi, sistem_kullanici, reseller_id FROM backups WHERE domain_id=9001`).Scan(&alan, &sk, &rid); err != nil {
		t.Fatal(err)
	}
	if alan != "eski.test" || sk != "c_eski" || rid != 77 {
		t.Fatalf("mevcut satırlar domains'ten doldurulmadı: %q %q %d", alan, sk, rid)
	}
	// Üçüncü koşu elle değiştirilmiş (dolu) değerleri EZMEZ.
	g113_exec(t, db, `UPDATE backups SET alan_adi='elle.test' WHERE domain_id=9001`)
	silinenGocUygula(t, db)
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE alan_adi='elle.test'`) != 1 {
		t.Error("göç dolu kolonu ezdi (yalnız boş olanlar doldurulmalı)")
	}
	g113_exec(t, db, `DELETE FROM domains WHERE id=9001`)
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=9001`) != 1 {
		t.Fatal("domain silinince yedek satırı KALMALI (FK kalktı)")
	}
}

// ---------------------------------------------------------------- silme izi

func TestSilinenIsaretle_SatirlarKalirAlanlarDolu(t *testing.T) {
	db := silinenDB(t)
	silinenOrtam(t)
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id, backup_retention) VALUES (9101,'silinen1.test','c_silinen1',5,3)`)
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (9199,'canli.test','c_canli')`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya) VALUES (9101,'c_silinen1-20261001-000000.tar.gz'),(9101,'c_silinen1-20261002-000000.tar.gz'),(9199,'c_canli-20261001-000000.tar.gz')`)

	iz := SilinenIzHazirla(context.Background(), db, 9101, "silinen1.test", "c_silinen1")
	g113_exec(t, db, `DELETE FROM domains WHERE id=9101`)
	iz.Yaz(context.Background(), db)

	if n := g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=9101 AND alan_adi='silinen1.test' AND sistem_kullanici='c_silinen1' AND reseller_id=5 AND silinme IS NOT NULL`); n != 2 {
		t.Fatalf("2 satır yetim işaretlenmeliydi, %d", n)
	}
	// slot 3 < alt sınır 7 → 7 gün
	if g := g113_say(t, db, `SELECT DATEDIFF(son_kullanma, silinme) FROM backups WHERE domain_id=9101 LIMIT 1`); g != 7 {
		t.Errorf("süre 7 gün olmalı (slot alt sınırı): %d", g)
	}
	if n := g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=9199 AND silinme IS NOT NULL`); n != 0 {
		t.Error("başka (canlı) domainin satırı işaretlendi")
	}
}

func TestSilinenIsaretle_AyarSureyiBelirler(t *testing.T) {
	db := silinenDB(t)
	silinenOrtam(t)
	ctx := context.Background()
	// slot 14 (ayarsız), sonra sabit 30, sonra 0 (=NULL), sonra boş ayar (tekrar slot)
	cases := []struct {
		ayar     string
		beklenen int // -1 = NULL
	}{{"", 14}, {"30", 30}, {"0", -1}, {"99999", 14}, {"abc", 14}}
	for i, c := range cases {
		id := int64(9110 + i)
		g113_exec(t, db, `DELETE FROM cp_ayarlar`)
		if c.ayar != "" {
			g113_exec(t, db, `INSERT INTO cp_ayarlar (anahtar, deger) VALUES ('silinen_yedek_gun', ?)`, c.ayar)
		}
		g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, backup_retention) VALUES (?,?,?,14)`, id, "a.test", "c_ayar")
		g113_exec(t, db, `INSERT INTO backups (domain_id, dosya) VALUES (?, 'c_ayar-20261001-000000.tar.gz')`, id)
		g113_exec(t, db, `INSERT INTO backups (domain_id, dosya) VALUES (?, 'c_yanci-20261001-000000.tar.gz')`, 8000+id) // başka domainin satırı
		iz := SilinenIzHazirla(ctx, db, id, "a.test", "c_ayar")
		g113_exec(t, db, `DELETE FROM domains WHERE id=?`, id)
		iz.Yaz(ctx, db)
		if n := g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=? AND silinme IS NOT NULL`, 8000+id); n != 0 {
			t.Errorf("ayar %q: başka domainin satırı işaretlendi", c.ayar)
		}
		if n := g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=? AND silinme IS NOT NULL AND sistem_kullanici='c_ayar'`, id); n != 1 {
			t.Errorf("ayar %q: hedef domainin satırı işaretlenmedi", c.ayar)
		}
		var g *int
		if err := db.QueryRow(`SELECT DATEDIFF(son_kullanma, silinme) FROM backups WHERE domain_id=?`, id).Scan(&g); err != nil {
			t.Fatal(err)
		}
		if c.beklenen == -1 {
			if g != nil {
				t.Errorf("ayar %q: son_kullanma NULL olmalı (0 = otomatik silme yok), %d", c.ayar, *g)
			}
		} else if g == nil || *g != c.beklenen {
			t.Errorf("ayar %q: %d gün beklenirdi, %v", c.ayar, c.beklenen, g)
		}
	}
}

func TestSilinenIz_HataSilmeyiEngellemezPanikYutulur(t *testing.T) {
	db := silinenDB(t)
	ctx := context.Background()
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (9120,'h.test','c_hata')`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya) VALUES (9120,'c_hata-20261001-000000.tar.gz')`)
	iz := SilinenIzHazirla(ctx, db, 9120, "h.test", "c_hata")
	g113_exec(t, db, `DELETE FROM domains WHERE id=9120`)
	g113_exec(t, db, `ALTER TABLE backups CHANGE silinme silinme_bozuk TIMESTAMP NULL DEFAULT NULL`) // UPDATE patlasın
	iz.Yaz(ctx, db)                                                                                  // hata/panik YOK, dönmeli
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=9120`) != 1 {
		t.Fatal("satır kaybolmamalı")
	}
	var nilIz *SilinenIz
	nilIz.Yaz(ctx, db) // nil iz sessizce yok sayılır
	if SilinenIzHazirla(ctx, db, 1, "x.test", "../etc") != nil || SilinenIzHazirla(ctx, db, 1, "x.test", "") != nil {
		t.Error("geçersiz sk için iz nil olmalı")
	}
	// kapalı DB'de de panik yok
	kapali := silinenDB(t)
	iz2 := &SilinenIz{alanAdi: "k.test", sk: "c_kapali", domainID: 1, slot: 7}
	_ = kapali.Close()
	iz2.Yaz(ctx, kapali)
}

func TestSkDosyaAdindan(t *testing.T) {
	ok := map[string]string{
		"c_abc-20261001-123456.tar.gz":        "c_abc",
		"c_a_b1-20261001-123456.tar.gz":       "c_a_b1",
		"c_ornek_alan-20260101-000000.tar.gz": "c_ornek_alan",
	}
	for d, sk := range ok {
		if g, o := skDosyaAdindan(d); !o || g != sk {
			t.Errorf("%s → %q,%v", d, g, o)
		}
	}
	for _, d := range []string{"", "x-20261001-123456.tar.gz", "c_abc-2026-123456.tar.gz", "c_abc-20261001-123456.tar", "../c_abc-20261001-123456.tar.gz", "c_ABC-20261001-123456.tar.gz", "c_abc-20261001-123456.tar.gz/x"} {
		if g, o := skDosyaAdindan(d); o {
			t.Errorf("%q geçersiz olmalı, %q", d, g)
		}
	}
}

func TestSilinenIsaretsizTani(t *testing.T) {
	db := silinenDB(t)
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (9130,'canli.test','c_canli')`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya) VALUES (9130,'c_canli-20261001-000000.tar.gz'),(9131,'c_yok-20261001-000000.tar.gz'),(9132,'garip.tar.gz')`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya, silinme) VALUES (9133,'c_yetim-20261001-000000.tar.gz', NOW())`)
	top, tur := SilinenIsaretsizTani(context.Background(), db)
	if top != 2 || tur != 1 {
		t.Errorf("işaretsiz sayısı: toplam=%d türetilebilir=%d (2,1 beklenir)", top, tur)
	}
}

// ---------------------------------------------------------------- otomatik bağlama

func TestYetimleriOtoBagla_YalnizAyniAlanSahipSK(t *testing.T) {
	db := silinenDB(t)
	ctx := context.Background()
	d := silinenYetim(t, db, 9201, "ayni.test", "c_ayni", "c_ayni-20261001-000000.tar.gz", 5, "NULL")
	farkliSahip := silinenYetim(t, db, 9202, "ayni.test", "c_ayni", "c_ayni-20261002-000000.tar.gz", 6, "NULL")
	farkliAlan := silinenYetim(t, db, 9203, "baska.test", "c_ayni", "c_ayni-20261003-000000.tar.gz", 5, "NULL")
	farkliSK := silinenYetim(t, db, 9204, "ayni.test", "c_baska", "c_baska-20261001-000000.tar.gz", 5, "NULL")
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES (9210,'ayni.test','c_ayni',5)`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya) VALUES (9210,'c_ayni-20261101-000000.tar.gz')`) // yaşayan yedek
	canli := satirID(t, db, 9210)
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES (9212,'x.test','c_x',5)`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya, alan_adi, sistem_kullanici, reseller_id) VALUES (9212,'c_ayni-20261201-000000.tar.gz','ayni.test','c_ayni',5)`) // başka CANLI domainin satırı
	baskaCanli := satirID(t, db, 9212)

	if n := yetimleriOtoBagla(ctx, db, 9210); n != 1 {
		t.Fatalf("yalnız birebir eşleşen 1 satır bağlanmalı: %d", n)
	}
	var did int64
	var sil *string
	_ = db.QueryRow(`SELECT domain_id, DATE_FORMAT(silinme,'%Y') FROM backups WHERE id=?`, d).Scan(&did, &sil)
	if did != 9210 || sil != nil {
		t.Errorf("bağlanan satır: domain=%d silinme=%v", did, sil)
	}
	for ad, id := range map[string]int64{"farklı sahip": farkliSahip, "farklı alan": farkliAlan, "farklı sk": farkliSK} {
		_ = db.QueryRow(`SELECT domain_id FROM backups WHERE id=?`, id).Scan(&did)
		if did == 9210 || g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE id=? AND silinme IS NOT NULL`, id) != 1 {
			t.Errorf("%s: OTOMATİK BAĞLANMAMALIYDI (başka müşteriye sızıntı)", ad)
		}
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE id=? AND domain_id=9210`, canli) != 1 {
		t.Error("yaşayan yedek bozuldu")
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE id=? AND domain_id=9212`, baskaCanli) != 1 {
		t.Error("başka canlı domainin satırı (yetim değil) OTOMATİK BAĞLANDI")
	}
	// Mevcut Restore/Sil/İndir sorgusu (JOIN domains) bağlanan satırı bulur.
	var sk, dosya string
	if err := db.QueryRow(`SELECT d.sistem_kullanici, b.dosya FROM backups b JOIN domains d ON d.id=b.domain_id WHERE b.id=? AND b.domain_id=?`, d, 9210).Scan(&sk, &dosya); err != nil || sk != "c_ayni" {
		t.Errorf("bağlanan yedek JOIN domains ile bulunamadı: %v", err)
	}
	// İkinci çağrı değişiklik yapmaz; sahibi NULL (admin) olan domainde reseller_id=0 eşleşir.
	if n := yetimleriOtoBagla(ctx, db, 9210); n != 0 {
		t.Errorf("ikinci çağrı 0 bağlamalı: %d", n)
	}
	yet0 := silinenYetim(t, db, 9205, "adm.test", "c_adm", "c_adm-20261001-000000.tar.gz", 0, "NULL")
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (9211,'adm.test','c_adm')`)
	if n := yetimleriOtoBagla(ctx, db, 9211); n != 1 || g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE id=? AND domain_id=9211`, yet0) != 1 {
		t.Errorf("sahipsiz (reseller NULL=0) eşleşmesi: %d", n)
	}
	if yetimleriOtoBagla(ctx, db, 424242) != 0 {
		t.Error("olmayan domain için bağlama olmamalı")
	}
}

func satirID(t *testing.T, db *sql.DB, domainID int64) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(`SELECT id FROM backups WHERE domain_id=? ORDER BY id DESC LIMIT 1`, domainID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// ---------------------------------------------------------------- ayar uçları

func TestSilinenAyar_SinirlarVeKalicilik(t *testing.T) {
	db := silinenDB(t)
	h := &Handlers{DB: db}
	r := advRouter(db, h)
	if kod, _ := advIstek(r, "PUT", "/s/ayar", `{"gun":-1}`); kod != 400 {
		t.Errorf("-1 reddedilmeli: %d", kod)
	}
	if kod, _ := advIstek(r, "PUT", "/s/ayar", `{"gun":3651}`); kod != 400 {
		t.Errorf("3651 reddedilmeli: %d", kod)
	}
	if kod, _ := advIstek(r, "PUT", "/s/ayar", `{"gun":`); kod != 400 {
		t.Errorf("bozuk gövde: %d", kod)
	}
	if kod, _ := advIstek(r, "PUT", "/s/ayar", `{"gun":30}`); kod != 200 {
		t.Fatalf("30 kabul: %d", kod)
	}
	if _, g := advIstek(r, "GET", "/s/ayar", ""); !strings.Contains(g, `"gun":30`) {
		t.Errorf("ayar okunamadı: %s", g)
	}
	if kod, _ := advIstek(r, "PUT", "/s/ayar", `{"gun":0}`); kod != 200 {
		t.Fatal("0 kabul")
	}
	if _, g := advIstek(r, "GET", "/s/ayar", ""); !strings.Contains(g, `"gun":0`) {
		t.Errorf("0 okunamadı: %s", g)
	}
	if kod, _ := advIstek(r, "PUT", "/s/ayar", `{"gun":null}`); kod != 200 {
		t.Fatal("null kabul")
	}
	if _, g := advIstek(r, "GET", "/s/ayar", ""); !strings.Contains(g, `"gun":null`) {
		t.Errorf("null okunamadı: %s", g)
	}
}

var _ = time.Now

func TestSilinenSKGecerli(t *testing.T) {
	for _, sk := range []string{"c_a", "c_abc_1", "c_" + strings.Repeat("a", 40)} {
		if !silinenSKGecerli(sk) {
			t.Errorf("%q geçerli olmalı", sk)
		}
	}
	for _, sk := range []string{"", "c_", "a", "x_abc", "c_A", "c_a/b", "c_a.b", "c_..", "../c_a", "c_a\n", "c_a b", "c_a;b", "c_" + strings.Repeat("a", 41), "c_a\x00"} {
		if silinenSKGecerli(sk) {
			t.Errorf("%q geçersiz olmalı", sk)
		}
	}
}
