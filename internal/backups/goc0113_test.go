package backups

// goc0113_test.go — 0113_yedek_uzak_hedefler göçü: iki (üç) kez koşunca
// idempotent, veriyi korur; şema kısıtları (tekillik, CHECK, hedef silinince
// CASCADE, yedek/domain silinince SET NULL = sahipsiz kopya, hibrit age alıcısı,
// tek etkin anahtar) gerçekten çalışır.
//
// Gerçek MariaDB gerekir: YALNIZ geçici sunucu (GOSP_TEST_GECICI_DSN); yoksa
// DB'li testler ATLANIR. Test kendi açtığı gosp_test_goc0113_<zaman>
// veritabanını kullanır ve sonunda DROP eder; canlı panel DB'sine dokunmaz.

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/go-sql-driver/mysql"
)

const g113_goc = "../../migrations/0113_yedek_uzak_hedefler.sql"

// Koşucu yalnız başında "--" olan satırları atar ve ";" ile böler: satır içi
// yorum ya da yorumda ";" ifadeyi bozar. DB gerektirmez.
func TestGoc0113_KosucuUyumu(t *testing.T) {
	body, err := os.ReadFile(g113_goc)
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
	for _, tablo := range []string{"backup_hedefler", "backup_uzak_kopyalar", "backup_sifre_anahtarlari"} {
		if !strings.Contains(string(body), "CREATE TABLE IF NOT EXISTS "+tablo+" (") {
			t.Errorf("%s idempotent (IF NOT EXISTS) oluşturulmuyor", tablo)
		}
	}
}

func g113_db(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li test atlandı (canlı MariaDB'ye ASLA gidilmez)")
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
	ad := fmt.Sprintf("gosp_test_goc0113_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		_ = kok.Close()
		t.Fatalf("geçici veritabanı: %v", err)
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
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	// 0008 göçünün ürettiği domains/backups iskeleti (FK hedefi).
	g113_exec(t, db, `CREATE TABLE domains (id BIGINT UNSIGNED NOT NULL PRIMARY KEY) ENGINE=InnoDB`)
	g113_exec(t, db, `CREATE TABLE backups (
		id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
		domain_id BIGINT UNSIGNED NOT NULL,
		dosya VARCHAR(255) NOT NULL,
		KEY ix_backup_domain (domain_id),
		CONSTRAINT fk_backup_domain FOREIGN KEY (domain_id) REFERENCES domains(id) ON DELETE CASCADE
	) ENGINE=InnoDB`)
	return db
}

func g113_exec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("SQL hatası: %v\n%s", err, q)
	}
}

func g113_red(t *testing.T, db *sql.DB, neden, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err == nil {
		t.Errorf("%s: ifade REDDEDİLMELİYDİ: %s %v", neden, q, args)
	}
}

func g113_say(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("sayım: %v\n%s", err, q)
	}
	return n
}

// g113_uygula — cmd/server runMigrations ile AYNI ayrıştırma ve hata toleransı.
func g113_uygula(t *testing.T, db *sql.DB) {
	t.Helper()
	body, err := os.ReadFile(g113_goc)
	if err != nil {
		t.Fatal(err)
	}
	var temiz []string
	for _, satir := range strings.Split(string(body), "\n") {
		s := strings.TrimSpace(satir)
		if s == "" || strings.HasPrefix(s, "--") {
			continue
		}
		temiz = append(temiz, satir)
	}
	for _, ifade := range strings.Split(strings.Join(temiz, "\n"), ";") {
		s := strings.TrimSpace(ifade)
		if s == "" {
			continue
		}
		if _, err := db.Exec(s); err != nil {
			m := strings.ToLower(err.Error())
			if strings.Contains(m, "duplicate column") || strings.Contains(m, "duplicate key") ||
				strings.Contains(m, "already exists") || strings.Contains(m, "duplicate entry") {
				continue
			}
			t.Fatalf("GÖÇ HATASI: %v\n%s", err, s)
		}
	}
}

func TestGoc0113_IdempotentVeKisitlar(t *testing.T) {
	db := g113_db(t)
	g113_uygula(t, db)
	g113_uygula(t, db) // ikinci koşu: hata yok

	// semaDogrula'nın beklediği kolonlar var.
	for _, k := range [][2]string{
		{"backup_hedefler", "ic_ag_izni"}, {"backup_uzak_kopyalar", "hedef_anahtar"},
		{"backup_sifre_anahtarlari", "etkin_tekil"},
	} {
		if g113_say(t, db, `SELECT COUNT(*) FROM information_schema.COLUMNS
			WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?`, k[0], k[1]) != 1 {
			t.Fatalf("kolon yok: %s.%s", k[0], k[1])
		}
	}

	g113_exec(t, db, `INSERT INTO domains (id) VALUES (1)`)
	g113_exec(t, db, `INSERT INTO backups (id, domain_id, dosya) VALUES (10, 1, 'c_x.tar.gz'), (11, 1, 'c_y.tar.gz')`)
	g113_exec(t, db, `INSERT INTO backup_hedefler (id, ad, tur, ayar, sirlar) VALUES (5, 'minio', 's3', '{"uc":"https://s3.ornek"}', '')`)
	g113_exec(t, db, `INSERT INTO backup_hedefler (id, ad, tur, ayar, sirlar) VALUES (6, 'nas', 'smb', '{}', '')`)

	// Hedef satırı: varsayılanlar (kök sahibi, age, iç ağ kapalı).
	var sahip, sifreleme string
	var icAg int
	if err := db.QueryRow(`SELECT sahip_tur, sifreleme, ic_ag_izni FROM backup_hedefler WHERE id=5`).Scan(&sahip, &sifreleme, &icAg); err != nil {
		t.Fatal(err)
	}
	if sahip != "kok" || sifreleme != "age" || icAg != 0 {
		t.Errorf("varsayılanlar: sahip=%s sifreleme=%s ic_ag=%d", sahip, sifreleme, icAg)
	}
	g113_red(t, db, "geçersiz JSON ayar", `INSERT INTO backup_hedefler (ad, tur, ayar, sirlar) VALUES ('x', 's3', 'json degil', '')`)
	g113_red(t, db, "geçersiz sahip", `INSERT INTO backup_hedefler (ad, tur, ayar, sirlar, sahip_tur) VALUES ('y', 's3', '{}', '', 'x')`)
	g113_red(t, db, "aynı sahipte aynı ad", `INSERT INTO backup_hedefler (ad, tur, ayar, sirlar) VALUES ('minio', 'webdav', '{}', '')`)

	// Kopyalar: genel/domain kaynaklı kopya hedef_id NULL iken de TEKİL.
	g113_exec(t, db, `INSERT INTO backup_uzak_kopyalar (backup_id, domain_id, kaynak, uzak_ad) VALUES (10, 1, 'genel', '2026-08-25/c_x.tar.gz')`)
	g113_exec(t, db, `INSERT INTO backup_uzak_kopyalar (backup_id, domain_id, kaynak, uzak_ad) VALUES (10, 1, 'domain', 'c_x.tar.gz')`)
	g113_red(t, db, "yinelenen genel kopya", `INSERT INTO backup_uzak_kopyalar (backup_id, domain_id, kaynak) VALUES (10, 1, 'genel')`)
	g113_exec(t, db, `INSERT INTO backup_uzak_kopyalar (backup_id, domain_id, kaynak, hedef_id) VALUES (10, 1, 'hedef', 5), (10, 1, 'hedef', 6), (11, 1, 'hedef', 5)`)
	g113_red(t, db, "yinelenen hedef kopyası", `INSERT INTO backup_uzak_kopyalar (backup_id, domain_id, kaynak, hedef_id) VALUES (10, 1, 'hedef', 5)`)
	g113_red(t, db, "hedef kaynağı hedef_id'siz", `INSERT INTO backup_uzak_kopyalar (backup_id, domain_id, kaynak) VALUES (11, 1, 'hedef')`)
	g113_red(t, db, "genel kaynak hedef_id'li", `INSERT INTO backup_uzak_kopyalar (backup_id, domain_id, kaynak, hedef_id) VALUES (11, 1, 'genel', 5)`)
	g113_red(t, db, "geçersiz kaynak", `INSERT INTO backup_uzak_kopyalar (backup_id, domain_id, kaynak) VALUES (11, 1, 'ftp')`)
	g113_red(t, db, "olmayan yedek", `INSERT INTO backup_uzak_kopyalar (backup_id, domain_id, kaynak) VALUES (999, 1, 'genel')`)
	g113_red(t, db, "olmayan hedef", `INSERT INTO backup_uzak_kopyalar (backup_id, domain_id, kaynak, hedef_id) VALUES (11, 1, 'hedef', 999)`)
	g113_red(t, db, "geçersiz kopya durumu", `INSERT INTO backup_uzak_kopyalar (backup_id, domain_id, kaynak, durum) VALUES (11, 1, 'domain', 'bilinmez')`)
	// domain_id zorunlu (sahipsiz kopyanın domain hedefini bulmak için); sql_mode'dan bağımsız şema denetimi.
	if n := g113_say(t, db, `SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE()
		AND TABLE_NAME = 'backup_uzak_kopyalar' AND COLUMN_NAME = 'domain_id' AND IS_NULLABLE = 'NO' AND COLUMN_DEFAULT IS NULL`); n != 1 {
		t.Errorf("domain_id NOT NULL ve varsayılansız olmalı")
	}

	// Göç veri varken yeniden koşar ve veriye dokunmaz.
	g113_uygula(t, db)
	if n := g113_say(t, db, `SELECT COUNT(*) FROM backup_uzak_kopyalar`); n != 5 {
		t.Fatalf("yeniden koşu sonrası kopya sayısı %d, beklenen 5", n)
	}

	// Hedef silinince YALNIZ onun kopya kayıtları gider.
	g113_exec(t, db, `DELETE FROM backup_hedefler WHERE id=5`)
	if n := g113_say(t, db, `SELECT COUNT(*) FROM backup_uzak_kopyalar`); n != 3 {
		t.Errorf("hedef silme CASCADE: %d kayıt kaldı, beklenen 3", n)
	}

	// 🔴 Yerel yedek satırı silinince kopya kayıtları KALIR, sahipsiz olur (backup_id NULL).
	// backups.go'daki uzagi_yoksay=1 ve lftp-yok yolları uzak kopyayı silmeden
	// DELETE FROM backups yapıyor: CASCADE olsaydı uzaktaki dosya iz bırakmadan kalırdı.
	g113_exec(t, db, `UPDATE backup_uzak_kopyalar SET durum='tamam', sha256_sifreli=REPEAT('a', 64) WHERE backup_id=10`)
	g113_exec(t, db, `DELETE FROM backups WHERE id=10`)
	if n := g113_say(t, db, `SELECT COUNT(*) FROM backup_uzak_kopyalar WHERE backup_id=10`); n != 0 {
		t.Errorf("silinen yedeğe bağlı %d kayıt kaldı", n)
	}
	if n := g113_say(t, db, `SELECT COUNT(*) FROM backup_uzak_kopyalar WHERE backup_id IS NULL AND domain_id=1
		AND durum='tamam' AND sha256_sifreli=REPEAT('a', 64) AND kaynak IN ('genel', 'domain', 'hedef')`); n != 3 {
		t.Errorf("yedek silinince sahipsiz kopya sayısı %d, beklenen 3 (kayıt ve alanları korunmalı)", n)
	}
	var uzakAd string
	if err := db.QueryRow(`SELECT uzak_ad FROM backup_uzak_kopyalar WHERE backup_id IS NULL AND kaynak='genel'`).Scan(&uzakAd); err != nil || uzakAd != "2026-08-25/c_x.tar.gz" {
		t.Errorf("sahipsiz kopyanın uzak adı korunmadı: %q %v", uzakAd, err)
	}
	// Aynı kaynaktan İKİNCİ sahipsiz kopya: UNIQUE NULL'ları eşit saysaydı SET NULL
	// ve dolayısıyla DELETE FROM backups hata verirdi.
	g113_exec(t, db, `INSERT INTO backups (id, domain_id, dosya) VALUES (12, 1, 'c_z.tar.gz')`)
	g113_exec(t, db, `INSERT INTO backup_uzak_kopyalar (backup_id, domain_id, kaynak, uzak_ad) VALUES (12, 1, 'genel', 'c_z.tar.gz')`)
	g113_exec(t, db, `DELETE FROM backups WHERE id=12`)
	if n := g113_say(t, db, `SELECT COUNT(*) FROM backup_uzak_kopyalar WHERE backup_id IS NULL AND kaynak='genel'`); n != 2 {
		t.Errorf("iki sahipsiz genel kopya bekleniyordu, %d", n)
	}
	g113_exec(t, db, `UPDATE backup_uzak_kopyalar SET durum='sahipsiz' WHERE backup_id IS NULL`)
	// Domain silinince (domains → backups CASCADE) kopyalar yine sahipsiz kalır.
	g113_exec(t, db, `INSERT INTO backup_uzak_kopyalar (backup_id, domain_id, kaynak, hedef_id) VALUES (11, 1, 'hedef', 6)`)
	onceki := g113_say(t, db, `SELECT COUNT(*) FROM backup_uzak_kopyalar`)
	g113_exec(t, db, `DELETE FROM domains WHERE id=1`)
	if n := g113_say(t, db, `SELECT COUNT(*) FROM backup_uzak_kopyalar WHERE backup_id IS NULL AND domain_id=1`); n != onceki {
		t.Errorf("domain silinince %d/%d kopya kaydı sahipsiz kaldı", n, onceki)
	}

	// Şifre anahtarları: gerçek hibrit (ML-KEM-768 + X25519) alıcı dizgesi alici kolonuna sığar.
	hibrit, err := age.GenerateHybridIdentity()
	if err != nil {
		t.Fatal(err)
	}
	hAlici := hibrit.Recipient().String()
	if len(hAlici) <= 128 {
		t.Fatalf("hibrit alıcı beklenenden kısa (%d): test varsayımı bozuk", len(hAlici))
	}
	g113_exec(t, db, `INSERT INTO backup_sifre_anahtarlari (parmak, alici, gizli_anahtar, durum) VALUES ('ph', ?, 'sifreli', 'emekli')`, hAlici)
	var geri string
	if err := db.QueryRow(`SELECT alici FROM backup_sifre_anahtarlari WHERE parmak='ph'`).Scan(&geri); err != nil || geri != hAlici {
		t.Errorf("hibrit alıcı (%d karakter) kesildi ya da okunamadı: %d karakter geri geldi, %v", len(hAlici), len(geri), err)
	}

	// Şifre anahtarları: aynı anda en çok BİR etkin.
	g113_exec(t, db, `INSERT INTO backup_sifre_anahtarlari (parmak, alici, gizli_anahtar) VALUES ('p1', 'age1a', 'x'), ('p2', 'age1b', 'y')`)
	g113_exec(t, db, `UPDATE backup_sifre_anahtarlari SET durum='etkin' WHERE parmak='p1'`)
	g113_red(t, db, "ikinci etkin anahtar", `UPDATE backup_sifre_anahtarlari SET durum='etkin' WHERE parmak='p2'`)
	g113_exec(t, db, `UPDATE backup_sifre_anahtarlari SET durum='emekli' WHERE parmak='p1'`)
	g113_exec(t, db, `UPDATE backup_sifre_anahtarlari SET durum='etkin' WHERE parmak='p2'`)
	g113_exec(t, db, `INSERT INTO backup_sifre_anahtarlari (parmak, alici, gizli_anahtar, durum) VALUES ('p3', 'age1c', 'z', 'emekli')`)
	g113_red(t, db, "yinelenen parmak", `INSERT INTO backup_sifre_anahtarlari (parmak, alici, gizli_anahtar) VALUES ('p1', 'age1d', 'w')`)
	g113_red(t, db, "geçersiz durum", `INSERT INTO backup_sifre_anahtarlari (parmak, alici, gizli_anahtar, durum) VALUES ('p9', 'age1e', 'v', 'acik')`)
	if n := g113_say(t, db, `SELECT COUNT(*) FROM backup_sifre_anahtarlari WHERE durum='etkin'`); n != 1 {
		t.Errorf("etkin anahtar sayısı %d", n)
	}
}
