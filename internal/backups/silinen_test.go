package backups

// silinen_test.go — silinen hosting yedeklerinin "yetim yedek" tasarımı (silinen.go + 0217 göçü).
// Gerçek BackupRoot'a YAZILMAZ: yedekKok geçici dizine çevrilir. Geçici MariaDB gerekir
// (GOSP_TEST_GECICI_DSN yoksa DB'li testler atlanır).

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const goc0217 = "../../migrations/0217_silinen_yedek.sql"

// goc0217V1 — bu göçün ilk (5b27194) sürümü, yorumsuz: 181 gibi geliştirme sunucularında uygulanmıştı.
// domain_id>0 + silinme dolu + domain yok = yetim; eski_domain_id / iz_id / backup_yetim_hedefler YOK; TIMESTAMP.
const goc0217V1 = `
DROP TABLE IF EXISTS cp_silinen_yedek;
ALTER TABLE backups DROP FOREIGN KEY IF EXISTS fk_backup_domain;
ALTER TABLE backups
  ADD COLUMN IF NOT EXISTS alan_adi VARCHAR(253) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS reseller_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS silinme TIMESTAMP NULL DEFAULT NULL,
  ADD COLUMN IF NOT EXISTS son_kullanma TIMESTAMP NULL DEFAULT NULL,
  ADD KEY IF NOT EXISTS ix_backups_silinen (sistem_kullanici, silinme);
UPDATE backups b JOIN domains d ON d.id = b.domain_id
  SET b.alan_adi = d.alan_adi, b.sistem_kullanici = d.sistem_kullanici, b.reseller_id = COALESCE(d.reseller_id, 0)
  WHERE b.sistem_kullanici = '';
`

// goc0217V2 — bu göçün ikinci (e449f18) sürümü, yorumsuz: eski_domain_id var, iz_id yok; anlık görüntü
// tablosunda eski_domain_id UNIQUE; TIMESTAMP; toplu dolgu UPDATE'i var.
const goc0217V2 = `
DROP TABLE IF EXISTS cp_silinen_yedek;
ALTER TABLE backups DROP FOREIGN KEY IF EXISTS fk_backup_domain;
ALTER TABLE backups
  ADD COLUMN IF NOT EXISTS alan_adi VARCHAR(253) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS reseller_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS silinme TIMESTAMP NULL DEFAULT NULL,
  ADD COLUMN IF NOT EXISTS son_kullanma TIMESTAMP NULL DEFAULT NULL,
  ADD COLUMN IF NOT EXISTS eski_domain_id BIGINT UNSIGNED NOT NULL DEFAULT 0;
ALTER TABLE backups ADD KEY IF NOT EXISTS ix_backups_silinen (sistem_kullanici, silinme);
ALTER TABLE backups ADD KEY IF NOT EXISTS ix_backups_eski_domain (eski_domain_id);
CREATE TABLE IF NOT EXISTS backup_yetim_hedefler (
  id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  eski_domain_id BIGINT UNSIGNED NOT NULL,
  tip            VARCHAR(8)   NOT NULL DEFAULT 'sftp',
  host           VARCHAR(253) NOT NULL,
  port           INT          NOT NULL DEFAULT 22,
  kullanici      VARCHAR(128) NOT NULL,
  parola         VARCHAR(255) NOT NULL,
  uzak_dizin     VARCHAR(255) NOT NULL DEFAULT '/',
  olusturma      TIMESTAMP    NOT NULL DEFAULT current_timestamp(),
  PRIMARY KEY (id),
  UNIQUE KEY uk_yetim_hedef_domain (eski_domain_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
UPDATE backups b JOIN domains d ON d.id = b.domain_id
  SET b.alan_adi = d.alan_adi, b.sistem_kullanici = d.sistem_kullanici, b.reseller_id = COALESCE(d.reseller_id, 0)
  WHERE b.sistem_kullanici = '';
UPDATE backups
  SET eski_domain_id = domain_id, domain_id = 0
  WHERE domain_id > 0 AND silinme IS NOT NULL AND NOT EXISTS (SELECT 1 FROM domains d WHERE d.id = backups.domain_id);
`

// silinenDBHam: g113_db iskeleti (FK'li backups) + bu testlerin okuduğu kolonlar; 0217 UYGULANMAMIŞ.
func silinenDBHam(t *testing.T) *sql.DB {
	t.Helper()
	db := g113_db(t)
	db.SetMaxOpenConns(8) // Liste: satırlar açıkken ikinci sorgu
	g113_exec(t, db, `ALTER TABLE domains ADD COLUMN alan_adi VARCHAR(253) NOT NULL DEFAULT '', ADD COLUMN sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '',
		ADD COLUMN reseller_id BIGINT UNSIGNED NULL, ADD COLUMN backup_retention TINYINT NOT NULL DEFAULT 7, ADD COLUMN is_demo TINYINT NOT NULL DEFAULT 0`)
	g113_exec(t, db, `ALTER TABLE backups ADD COLUMN tip VARCHAR(16) NOT NULL DEFAULT 'oto', ADD COLUMN boyut_b BIGINT NOT NULL DEFAULT 0, ADD COLUMN notlar VARCHAR(255) NOT NULL DEFAULT '', ADD COLUMN created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP`)
	g113_exec(t, db, `CREATE TABLE cp_ayarlar (anahtar VARCHAR(64) PRIMARY KEY, deger TEXT NOT NULL)`)
	// Gerçek şema (migrations/0023): domain silinince hedef CASCADE ile GİDER — S1'in nedeni.
	g113_exec(t, db, `CREATE TABLE backup_destinations (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL, tip VARCHAR(8) NOT NULL DEFAULT 'sftp',
		host VARCHAR(253) NOT NULL, port INT NOT NULL DEFAULT 22, kullanici VARCHAR(128) NOT NULL, parola VARCHAR(255) NOT NULL, uzak_dizin VARCHAR(255) NOT NULL DEFAULT '/',
		aktif TINYINT NOT NULL DEFAULT 1, son_yukleme TIMESTAMP NULL DEFAULT NULL, son_durum VARCHAR(32) NOT NULL DEFAULT '', son_hata VARCHAR(512) NOT NULL DEFAULT '',
		UNIQUE KEY uk_domain (domain_id), CONSTRAINT fk_bdest_domain FOREIGN KEY (domain_id) REFERENCES domains (id) ON DELETE CASCADE) ENGINE=InnoDB`)
	izYenidenDenemeBekleme = 5 * time.Millisecond // hata dalı testleri 750 ms beklemesin
	baglaBekleme = time.Millisecond
	t.Cleanup(func() {
		izYenidenDenemeBekleme = 750 * time.Millisecond
		baglaBekleme = 60 * time.Millisecond
	})
	return db
}

// gocIfadeleri: göç metnini cmd/server runMigrations koşucusuyla AYNI ayrıştırmayla ifadelere böler
// (yalnız başı "--" olan satırlar atılır, ';' ile bölünür).
func gocIfadeleri(body string) []string {
	var temiz, out []string
	for _, satir := range strings.Split(body, "\n") {
		if x := strings.TrimSpace(satir); x == "" || strings.HasPrefix(x, "--") {
			continue
		}
		temiz = append(temiz, satir)
	}
	for _, ifade := range strings.Split(strings.Join(temiz, "\n"), ";") {
		if x := strings.TrimSpace(ifade); x != "" {
			out = append(out, x)
		}
	}
	return out
}

// gocMetniUygula: ifadeleri sırayla koşar. Koşucunun "zararsız hata" süzgeci BURADA YOK: temiz veritabanında
// (ve tekrar koşularda) HİÇBİR ifade hata vermemeli — idempotentlik IF NOT EXISTS'e bağlı kalır, hata yutmaya değil.
func gocMetniUygula(t *testing.T, db *sql.DB, body string) {
	t.Helper()
	for _, ifade := range gocIfadeleri(body) {
		g113_exec(t, db, ifade)
	}
}

// silinenGocUygula: 0217'yi cmd/server koşucusuyla AYNI ayrıştırmayla uygular.
func silinenGocUygula(t *testing.T, db *sql.DB) {
	t.Helper()
	body, err := os.ReadFile(goc0217)
	if err != nil {
		t.Fatal(err)
	}
	gocMetniUygula(t, db, string(body))
}

func silinenDB(t *testing.T) *sql.DB {
	t.Helper()
	db := silinenDBHam(t)
	silinenGocUygula(t, db)
	return db
}

// devral: YetimleriDevral (POST /domains `yedekleri_devral:true`) — hata → 0. Y1 sonrası GET yedek listesi hiçbir
// şeyi bağlamaz; "eşleşen yetimleri bağla" adımı testlerde budur.
func devral(ctx context.Context, db *sql.DB, domainID int64) int64 {
	n, _ := YetimleriDevral(ctx, db, domainID)
	return n
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

// silinenYetim: yetim yedek satırı ekler (S4 biçimi: domain_id=0, eski id eski_domain_id'de, iz_id boş: anlık görüntüsüz).
// eskiID: silinen domainin eski kimliği. sonKullanma: SQL ifadesi ("NULL" ya da "DATE_SUB(NOW(), INTERVAL 1 DAY)").
func silinenYetim(t *testing.T, db *sql.DB, eskiID int64, alan, sk, dosya string, rid int64, sonKullanma string) int64 {
	t.Helper()
	r, err := db.Exec(`INSERT INTO backups (domain_id, eski_domain_id, tip, dosya, boyut_b, alan_adi, sistem_kullanici, reseller_id, silinme, son_kullanma)
		VALUES (0,?,'oto',?,5,?,?,?,NOW(),`+sonKullanma+`)`, eskiID, dosya, alan, sk, rid)
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

// sekilDogrula — 0217 SONRASI beklenen şema (V0/V1/V2'den gelsin, kaç kez koşmuş olursa olsun AYNI):
// kolonlar + türler (silinme/son_kullanma DATETIME), indeksler, fk_backup_domain YOK, anlık görüntü tablosu iz_id UNIQUE.
func sekilDogrula(t *testing.T, db *sql.DB) {
	t.Helper()
	tur := func(tablo, kolon string) string {
		var s sql.NullString
		if err := db.QueryRow(`SELECT COLUMN_TYPE FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND COLUMN_NAME=?`, tablo, kolon).Scan(&s); err != nil {
			return "YOK"
		}
		return strings.ToLower(s.String)
	}
	for k, bek := range map[string]string{
		"alan_adi": "varchar(253)", "sistem_kullanici": "varchar(64)", "reseller_id": "bigint(20) unsigned",
		"silinme": "datetime", "son_kullanma": "datetime", "eski_domain_id": "bigint(20) unsigned", "iz_id": "varchar(32)",
	} {
		if g := tur("backups", k); g != bek {
			t.Errorf("backups.%s türü %q, %q beklenirdi", k, g, bek)
		}
	}
	for k, bek := range map[string]string{
		"iz_id": "varchar(32)", "eski_domain_id": "bigint(20) unsigned", "tip": "varchar(8)", "host": "varchar(253)", "port": "int(11)",
		"kullanici": "varchar(128)", "parola": "varchar(255)", "uzak_dizin": "varchar(255)", "olusturma": "datetime",
	} {
		if g := tur("backup_yetim_hedefler", k); g != bek {
			t.Errorf("backup_yetim_hedefler.%s türü %q, %q beklenirdi", k, g, bek)
		}
	}
	indeks := func(tablo, ad string) (var1 bool, benzersiz bool, kolonlar string) {
		rows, err := db.Query(`SELECT NON_UNIQUE, COLUMN_NAME FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND INDEX_NAME=? ORDER BY SEQ_IN_INDEX`, tablo, ad)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var ks []string
		for rows.Next() {
			var nu int
			var k string
			if err := rows.Scan(&nu, &k); err != nil {
				t.Fatal(err)
			}
			var1, benzersiz = true, nu == 0
			ks = append(ks, k)
		}
		return var1, benzersiz, strings.Join(ks, ",")
	}
	if v, u, k := indeks("backups", "ix_backups_silinen"); !v || u || k != "sistem_kullanici,silinme" {
		t.Errorf("ix_backups_silinen: var=%v benzersiz=%v kolonlar=%q", v, u, k)
	}
	if v, u, k := indeks("backups", "ix_backups_iz"); !v || u || k != "iz_id" {
		t.Errorf("ix_backups_iz: var=%v benzersiz=%v kolonlar=%q", v, u, k)
	}
	if v, _, _ := indeks("backups", "ix_backups_eski_domain"); v {
		t.Error("ix_backups_eski_domain KALKMALI (eski_domain_id yalnız bilgi, hiçbir sorgu onu aramaz)")
	}
	if v, u, k := indeks("backup_yetim_hedefler", "uk_yetim_hedef_iz"); !v || !u || k != "iz_id" {
		t.Errorf("uk_yetim_hedef_iz: var=%v benzersiz=%v kolonlar=%q", v, u, k)
	}
	if v, _, _ := indeks("backup_yetim_hedefler", "uk_yetim_hedef_domain"); v {
		t.Error("uk_yetim_hedef_domain KALKMALI (eski_domain_id benzersiz olamaz: kimlik yeniden kullanılır)")
	}
	if v, u, _ := indeks("backup_yetim_hedefler", "ix_yetim_hedef_eski"); !v || u {
		t.Errorf("ix_yetim_hedef_eski: var=%v benzersiz=%v", v, u)
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM information_schema.TABLE_CONSTRAINTS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='backups' AND CONSTRAINT_TYPE='FOREIGN KEY'`) != 0 {
		t.Error("backups üzerinde FOREIGN KEY KALMAMALI (fk_backup_domain düştü)")
	}
}

// tabloDokumu — tablonun tüm satırları (karşılaştırma için).
func tabloDokumu(t *testing.T, db *sql.DB, tablo string) string {
	t.Helper()
	return dgDokumSorgu(t, db, tablo, "SELECT * FROM "+tablo+" ORDER BY 1")
}

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
		"ADD COLUMN IF NOT EXISTS reseller_id", "ADD COLUMN IF NOT EXISTS silinme DATETIME", "ADD COLUMN IF NOT EXISTS son_kullanma DATETIME",
		"ADD COLUMN IF NOT EXISTS eski_domain_id BIGINT UNSIGNED NOT NULL DEFAULT 0", "ADD COLUMN IF NOT EXISTS iz_id VARCHAR(32) NOT NULL DEFAULT ''",
		"MODIFY COLUMN silinme DATETIME", "MODIFY COLUMN son_kullanma DATETIME",
		"ADD KEY IF NOT EXISTS ix_backups_silinen", "ADD KEY IF NOT EXISTS ix_backups_iz", "DROP KEY IF EXISTS ix_backups_eski_domain",
		"CREATE TABLE IF NOT EXISTS backup_yetim_hedefler (", "UNIQUE KEY uk_yetim_hedef_iz (iz_id)",
		"DROP KEY IF EXISTS uk_yetim_hedef_domain", "ADD UNIQUE KEY IF NOT EXISTS uk_yetim_hedef_iz (iz_id)"} {
		if !strings.Contains(m, parca) {
			t.Errorf("göçte %q yok (idempotent olmalı)", parca)
		}
	}
	if strings.Contains(m, "CREATE TABLE cp_silinen_yedek") || strings.Contains(m, "CREATE TABLE IF NOT EXISTS cp_silinen_yedek") {
		t.Error("cp_silinen_yedek tasarımı kaldırıldı: göç bu tabloyu oluşturmamalı")
	}
	// Veri KAYBETTİREN ya da toplu yazan ifade olmamalı: anlık görüntü tablosu asla düşürülmez, canlı satırlar domains'ten doldurulmaz (O6).
	for _, ifade := range gocIfadeleri(m) {
		u := strings.ToUpper(strings.Join(strings.Fields(ifade), " "))
		if strings.HasPrefix(u, "DROP TABLE") && strings.Contains(u, "BACKUP_YETIM_HEDEFLER") {
			t.Errorf("anlık görüntü tablosu düşürülemez (parola/veri kaybı, ikinci koşuda yeniden oluşur): %s", ifade)
		}
		if strings.HasPrefix(u, "UPDATE BACKUPS B JOIN DOMAINS") || strings.Contains(u, "SET B.ALAN_ADI = D.ALAN_ADI") {
			t.Errorf("canlı satırları domains'ten toplu dolduran UPDATE geri geldi (500 bin satırda ~29 sn, yazmaları bloke eder): %s", ifade)
		}
		if strings.HasPrefix(u, "UPDATE") && !strings.Contains(u, "WHERE") {
			t.Errorf("koşulsuz UPDATE: %s", ifade)
		}
	}
	// Kilit/süre güvenliği: kolon ekleme ile indeks ekleme AYNI ALTER'de olmamalı (tablo yeniden yazımı) ve
	// her ADD KEY çevrimiçi (INPLACE, LOCK=NONE) istenmeli: desteklenmezse sessiz kopya yerine HATA versin.
	for _, ifade := range gocIfadeleri(m) {
		if strings.Contains(ifade, "ADD COLUMN") && strings.Contains(ifade, "ADD KEY") {
			t.Errorf("ADD COLUMN ve ADD KEY aynı ALTER'de (büyük backups tablosunu yeniden yazar): %s", ifade)
		}
		if strings.Contains(ifade, "ALTER TABLE backups ADD KEY") && !strings.Contains(ifade, "ALGORITHM=INPLACE, LOCK=NONE") {
			t.Errorf("backups'a indeks eklerken ALGORITHM=INPLACE, LOCK=NONE istenmeli: %s", ifade)
		}
	}
	// "yazmalar bloke olmaz" iddiası yalnız doğru olan yerde yapılmalı: dosya ADD KEY için çevrimiçi diyebilir, ama
	// "her ifade ayrı olduğundan yazmalar bloke olmaz" biçimindeki eski yanlış genelleme geri gelmemeli.
	if strings.Contains(m, "yazmalar bloke olmaz") && !strings.Contains(m, "ALGORITHM=INPLACE") {
		t.Error("'yazmalar bloke olmaz' iddiası dayanaksız")
	}
}

// V0: gerçek eski şema (FK'li, 0217 öncesi) üzerine temiz kurulum — yeni şekil, DOLGU YOK (canlı satırların
// alan_adi/sistem_kullanici/reseller_id boş kalır), mevcut veri korunur, üç kez koşunca değişmez.
func TestGoc0217_V0_TemizKurulum_YeniSekilDolguYokIdempotent(t *testing.T) {
	db := silinenDBHam(t)
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES (9001,'eski.test','c_eski',77),(9002,'baska.test','c_baska',NULL)`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya, tip, boyut_b, notlar) VALUES (9001,'c_eski-20260101-000000.tar.gz','oto',11,'a'),(9001,'c_eski-20260102-000000.tar.gz','tam',22,'b'),(9002,'c_baska-20260101-000000.tar.gz','oto',33,'c')`)
	once := tabloDokumu(t, db, "backups")
	if g113_say(t, db, `SELECT COUNT(*) FROM information_schema.TABLE_CONSTRAINTS WHERE TABLE_SCHEMA=DATABASE() AND CONSTRAINT_NAME='fk_backup_domain'`) != 1 {
		t.Fatal("kurulum: V0'da fk_backup_domain olmalı")
	}
	silinenGocUygula(t, db)
	sekilDogrula(t, db)
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE alan_adi<>'' OR sistem_kullanici<>'' OR reseller_id<>0 OR silinme IS NOT NULL OR eski_domain_id<>0 OR iz_id<>''`) != 0 {
		t.Error("canlı satırlar domains'ten DOLDURULMAMALI (silme anında iz.Yaz yazar): dolgu UPDATE'i kaldırıldı")
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id IN (9001,9002)`) != 3 {
		t.Error("canlı satırlar yerinde kalmalı")
	}
	sonra := tabloDokumu(t, db, "backups")
	// id, domain_id, tip, dosya, boyut_b, notlar aynı; yeni kolonlar yalnız ekli (varsayılan değerli)
	for _, s := range []string{"c_eski-20260101-000000.tar.gz", "c_eski-20260102-000000.tar.gz", "c_baska-20260101-000000.tar.gz"} {
		if !strings.Contains(once, s) || !strings.Contains(sonra, s) {
			t.Errorf("%s göçle kayboldu", s)
		}
	}
	for i := 0; i < 2; i++ { // idempotent: veri kaybı/değişimi YOK
		silinenGocUygula(t, db)
		sekilDogrula(t, db)
		if g := tabloDokumu(t, db, "backups"); g != sonra {
			t.Fatalf("koşu %d: tablo içeriği değişti\n--- önce ---\n%s--- sonra ---\n%s", i+2, sonra, g)
		}
	}
	// domain silinince satır artık KALIR (FK yok)
	g113_exec(t, db, `DELETE FROM domains WHERE id=9001`)
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=9001`) != 2 {
		t.Fatal("domain silinince yedek satırı KALMALI (FK kalktı)")
	}
}

// V1 (5b27194; 181'de uygulanmış): domain_id>0 + silinme dolu + domain yok → yetim sekline (domain_id=0, eski_domain_id) çevrilir;
// TIMESTAMP → DATETIME; yeni kolon/indeks/tablo gelir; canlı satırlara DOKUNULMAZ; tekrar koşularda veri kaybı yok.
func TestGoc0217_V1_EskiRevizyonYeniSekle(t *testing.T) {
	db := silinenDBHam(t)
	gocMetniUygula(t, db, goc0217V1)
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES (9501,'canli.test','c_canli',5)`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya, alan_adi, sistem_kullanici, reseller_id, silinme, son_kullanma) VALUES
		(9500,'c_eski-20261001-000001.tar.gz','eski.test','c_eski',5,NOW(),DATE_ADD(NOW(), INTERVAL 7 DAY)),
		(9500,'c_eski-20261002-000001.tar.gz','eski.test','c_eski',5,NOW(),NULL),
		(9501,'c_canli-20261001-000001.tar.gz','canli.test','c_canli',5,NULL,NULL),
		(9501,'c_canli-20261002-000001.tar.gz','canli.test','c_canli',5,NOW(),NULL)`)
	if g113_say(t, db, `SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='backups' AND COLUMN_NAME IN ('eski_domain_id','iz_id')`) != 0 {
		t.Fatal("kurulum: V1'de eski_domain_id/iz_id olmamalı")
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='backups' AND COLUMN_NAME='silinme' AND DATA_TYPE='timestamp'`) != 1 {
		t.Fatal("kurulum: V1'de silinme TIMESTAMP olmalı")
	}
	silinenGocUygula(t, db)
	sekilDogrula(t, db)
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=0 AND eski_domain_id=9500 AND silinme IS NOT NULL AND iz_id=''`) != 2 {
		t.Error("V1 yetimleri (domain_id=9500, domains'te yok) yeni biçime (domain_id=0, eski_domain_id=9500) çevrilmeli")
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=9501 AND eski_domain_id=0`) != 2 {
		t.Error("canlı domainin satırlarına (silinme dolu olsa bile) dokunulmamalı")
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=0 AND eski_domain_id=9500 AND son_kullanma IS NULL`) != 1 || g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=0 AND son_kullanma IS NOT NULL`) != 1 {
		t.Error("son_kullanma değerleri TIMESTAMP→DATETIME dönüşümünde korunmalı")
	}
	once := tabloDokumu(t, db, "backups")
	for i := 0; i < 2; i++ {
		silinenGocUygula(t, db)
		sekilDogrula(t, db)
		if g := tabloDokumu(t, db, "backups"); g != once {
			t.Fatalf("koşu %d: tablo içeriği değişti\n%s\n---\n%s", i+2, once, g)
		}
	}
	// ve dönüştürülen satırlar yeni kodla normal yaşam döngüsüne girer
	r := advRouter(db, &Handlers{DB: db})
	if _, g := advIstek(r, "GET", "/s", ""); !strings.Contains(g, `"sk":"c_eski"`) {
		t.Errorf("dönüştürülen yetimler yönetici listesinde görünmeli: %s", g)
	}
}

// V2 (e449f18): eski_domain_id var, iz_id yok; anlık görüntü eski_domain_id UNIQUE. Yeni dosya anlık görüntüleri KORUR (parola dahil),
// iz_id verir (e<eski_domain_id>), ayni eski_domain_id'li yetimleri ona bağlar, UNIQUE'i iz_id'ye taşır, TIMESTAMP→DATETIME.
func TestGoc0217_V2_EskiRevizyonYeniSekle_AnlikGoruntuKorunur(t *testing.T) {
	db := silinenDBHam(t)
	gocMetniUygula(t, db, goc0217V2)
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES (9601,'canli.test','c_canli',5)`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, eski_domain_id, dosya, alan_adi, sistem_kullanici, reseller_id, silinme, son_kullanma) VALUES
		(0,9600,'c_a-20261001-000001.tar.gz','a.test','c_a',5,NOW(),DATE_ADD(NOW(), INTERVAL 7 DAY)),
		(0,9600,'c_a-20261002-000001.tar.gz','a.test','c_a',5,NOW(),NULL),
		(0,9602,'c_b-20261001-000001.tar.gz','b.test','c_b',6,NOW(),NULL),
		(9601,0,'c_canli-20261001-000001.tar.gz','canli.test','c_canli',5,NULL,NULL)`)
	g113_exec(t, db, `INSERT INTO backup_yetim_hedefler (eski_domain_id, host, kullanici, parola) VALUES (9600,'ftp.a.invalid','ak','GIZLI-A')`)
	if g113_say(t, db, `SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='backup_yetim_hedefler' AND COLUMN_NAME='iz_id'`) != 0 {
		t.Fatal("kurulum: V2'de anlık görüntü tablosunda iz_id olmamalı")
	}
	silinenGocUygula(t, db)
	sekilDogrula(t, db)
	if g113_say(t, db, `SELECT COUNT(*) FROM backup_yetim_hedefler WHERE eski_domain_id=9600 AND iz_id='e9600' AND parola='GIZLI-A' AND host='ftp.a.invalid'`) != 1 {
		t.Error("V2 anlık görüntüsü (parola dahil) korunmalı ve iz_id almalı")
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=0 AND eski_domain_id=9600 AND iz_id='e9600'`) != 2 {
		t.Error("anlık görüntüsü olan eski kimliğin yetimleri iz_id'ye bağlanmalı")
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE (domain_id=0 AND eski_domain_id=9602 AND iz_id='') OR (domain_id=9601 AND iz_id='')`) != 2 {
		t.Error("anlık görüntüsü olmayan yetim ve canlı satır iz_id almamalı")
	}
	// yeni kodla çalışır: yetimin uzak silmesi anlık görüntüyü (iz_id üzerinden) bulur
	d, err := yetimHedefOku(context.Background(), db, "e9600")
	if err != nil || d == nil || d.Host != "ftp.a.invalid" {
		t.Errorf("dönüştürülen anlık görüntü iz_id ile okunamadı: %+v %v", d, err)
	}
	// kimlik yeniden kullanılır: aynı eski_domain_id için İKİNCİ anlık görüntü artık yazılabilir (UNIQUE iz_id'de)
	if _, err := db.Exec(`INSERT INTO backup_yetim_hedefler (iz_id, eski_domain_id, host, kullanici, parola) VALUES ('rastgele-iz-2', 9600, 'ftp.b.invalid','bk','GIZLI-B')`); err != nil {
		t.Errorf("aynı eski_domain_id için ikinci anlık görüntü yazılamadı (eski UNIQUE kalmış): %v", err)
	}
	g113_exec(t, db, `DELETE FROM backup_yetim_hedefler WHERE iz_id='rastgele-iz-2'`)
	once := tabloDokumu(t, db, "backups") + tabloDokumu(t, db, "backup_yetim_hedefler")
	for i := 0; i < 2; i++ {
		silinenGocUygula(t, db)
		sekilDogrula(t, db)
		if g := tabloDokumu(t, db, "backups") + tabloDokumu(t, db, "backup_yetim_hedefler"); g != once {
			t.Fatalf("koşu %d: veri değişti\n%s\n---\n%s", i+2, once, g)
		}
	}
}

// V3 (bu sürüm) üzerine tekrar koşu veri KAYBETTİRMEZ: güncel şekilde duran yetimler, anlık görüntüler (parola dahil) ve canlı satırlar aynen kalır.
func TestGoc0217_V3_TekrarKosuVeriKaybettirmez(t *testing.T) {
	db := silinenDB(t)
	silinenOrtam(t)
	kHesapTemel := func(id int64, alan, sk string) {
		g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES (?,?,?,5)`, id, alan, sk)
		g113_exec(t, db, `INSERT INTO backup_destinations (domain_id, tip, host, port, kullanici, parola, uzak_dizin) VALUES (?,'sftp','ftp.x.invalid',22,'k','GIZLI-P','/')`, id)
		g113_exec(t, db, `INSERT INTO backups (domain_id, dosya, boyut_b) VALUES (?,?,5)`, id, fmt.Sprintf("%s-20261001-000001.tar.gz", sk))
	}
	kHesapTemel(9701, "a.test", "c_a")
	kHesapTemel(9702, "b.test", "c_b")
	kHesapTemel(9703, "canli.test", "c_canli")
	ctx := context.Background()
	for _, h := range []struct {
		id       int64
		alan, sk string
	}{{9701, "a.test", "c_a"}, {9702, "b.test", "c_b"}} {
		iz := SilinenIzHazirla(ctx, db, h.id, h.alan, h.sk)
		g113_exec(t, db, `DELETE FROM domains WHERE id=?`, h.id)
		iz.Yaz(ctx, db)
	}
	once := tabloDokumu(t, db, "backups") + tabloDokumu(t, db, "backup_yetim_hedefler") + tabloDokumu(t, db, "backup_destinations")
	if g113_say(t, db, `SELECT COUNT(*) FROM backup_yetim_hedefler`) != 2 {
		t.Fatal("kurulum: 2 anlık görüntü")
	}
	for i := 0; i < 3; i++ {
		silinenGocUygula(t, db)
		sekilDogrula(t, db)
		if g := tabloDokumu(t, db, "backups") + tabloDokumu(t, db, "backup_yetim_hedefler") + tabloDokumu(t, db, "backup_destinations"); g != once {
			t.Fatalf("koşu %d: veri kaybı/değişimi\n--- önce ---\n%s--- sonra ---\n%s", i+1, once, g)
		}
	}
}

// Büyük tablo: V0 şeması üzerinde 300 bin canlı yedek satırıyla göç hızlı biter (toplu dolgu yok), ikinci koşu anlıktır ve
// kolon ekleme gerçekten ANLIK (ALGORITHM=INSTANT) ve ADD KEY çevrimiçi (INPLACE, LOCK=NONE) kabul edilir.
func TestGoc0217_BuyukTablo_HizliVeCevrimici(t *testing.T) {
	if testing.Short() {
		t.Skip("kısa modda atlandı")
	}
	db := silinenDBHam(t)
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) SELECT seq, CONCAT('d', seq, '.test'), CONCAT('c_d', seq) FROM seq_1_to_300`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya, boyut_b) SELECT 1+MOD(seq,300), CONCAT('c_d', 1+MOD(seq,300), '-20260101-', LPAD(seq,6,'0'), '.tar.gz'), 5 FROM seq_1_to_300000`)
	// kolon ekleme anlık mı? (ayrı kopya tabloda açıkça ALGORITHM=INSTANT: tablo yeniden yazılacaksa HATA verir)
	g113_exec(t, db, `CREATE TABLE backups_kopya LIKE backups`)
	g113_exec(t, db, `ALTER TABLE backups_kopya DROP FOREIGN KEY IF EXISTS fk_backup_domain`)
	g113_exec(t, db, `ALTER TABLE backups_kopya ADD COLUMN alan_adi VARCHAR(253) NOT NULL DEFAULT '', ADD COLUMN iz_id VARCHAR(32) NOT NULL DEFAULT '', ADD COLUMN silinme DATETIME NULL DEFAULT NULL, ALGORITHM=INSTANT`)
	g113_exec(t, db, `DROP TABLE backups_kopya`)

	t0 := time.Now()
	silinenGocUygula(t, db)
	ilk := time.Since(t0)
	t1 := time.Now()
	silinenGocUygula(t, db)
	ikinci := time.Since(t1)
	t.Logf("göç süresi: ilk koşu %v, ikinci koşu %v (300 bin satır)", ilk, ikinci)
	sekilDogrula(t, db)
	if ilk > 30*time.Second {
		t.Errorf("ilk koşu %v sürdü: toplu satır yazan bir ifade var mı?", ilk)
	}
	if ikinci > 10*time.Second {
		t.Errorf("ikinci koşu %v sürdü: idempotent ifadeler (özellikle MODIFY COLUMN) tabloyu yeniden yazıyor olabilir", ikinci)
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE alan_adi<>'' OR sistem_kullanici<>''`) != 0 {
		t.Error("canlı satırlar doldurulmamalı")
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

	if n := g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=0 AND eski_domain_id=9101 AND alan_adi='silinen1.test' AND sistem_kullanici='c_silinen1' AND reseller_id=5 AND silinme IS NOT NULL`); n != 2 {
		t.Fatalf("2 satır yetim işaretlenmeliydi (domain_id=0, eski_domain_id=9101), %d", n)
	}
	if n := g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=9101`); n != 0 {
		t.Errorf("silinen domainin id'sini taşıyan satır kalmamalı (S4): %d", n)
	}
	// slot 3 < alt sınır 7 → 7 gün
	if g := g113_say(t, db, `SELECT DATEDIFF(son_kullanma, silinme) FROM backups WHERE eski_domain_id=9101 LIMIT 1`); g != 7 {
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
		if n := g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE eski_domain_id=? AND domain_id=0 AND silinme IS NOT NULL AND sistem_kullanici='c_ayar'`, id); n != 1 {
			t.Errorf("ayar %q: hedef domainin satırı işaretlenmedi", c.ayar)
		}
		var g *int
		if err := db.QueryRow(`SELECT DATEDIFF(son_kullanma, silinme) FROM backups WHERE eski_domain_id=?`, id).Scan(&g); err != nil {
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
	// 🔴 domain_id<=0: Yaz'ın WHERE domain_id=? koşulu MEVCUT yetimleri (domain_id=0) vururdu.
	if SilinenIzHazirla(ctx, db, 0, "x.test", "c_x") != nil || SilinenIzHazirla(ctx, db, -5, "x.test", "c_x") != nil {
		t.Error("domainID<=0 için iz nil olmalı")
	}
	// kapalı DB'de de panik yok
	kapali := silinenDB(t)
	iz2 := &SilinenIz{alanAdi: "k.test", sk: "c_kapali", domainID: 1, slot: 7}
	_ = kapali.Close()
	iz2.Yaz(ctx, kapali)
}

func TestSkDosyaAdindan(t *testing.T) {
	ok := map[string]string{
		"c_abc-20261001-123456.tar.gz":                             "c_abc",
		"c_a_b1-20261001-123456.tar.gz":                            "c_a_b1",
		"c_ornek_alan-20260101-000000.tar.gz":                      "c_ornek_alan",
		"c_abc-20261001-123456-planli.tar.gz":                      "c_abc", // eski bash zamanlayıcısı eki
		"c_abc-20261001-123456-auto.tar.gz":                        "c_abc",
		"c_" + strings.Repeat("a", 40) + "-20261001-123456.tar.gz": "c_" + strings.Repeat("a", 40),
	}
	for d, sk := range ok {
		if g, o := skDosyaAdindan(d); !o || g != sk {
			t.Errorf("%s → %q,%v", d, g, o)
		}
	}
	for _, d := range []string{"", "x-20261001-123456.tar.gz", "c_abc-2026-123456.tar.gz", "c_abc-20261001-123456.tar", "../c_abc-20261001-123456.tar.gz", "c_ABC-20261001-123456.tar.gz", "c_abc-20261001-123456.tar.gz/x", "c_" + strings.Repeat("a", 41) + "-20261001-123456.tar.gz", "c_abc-20261001-123456-x.tar.gz"} {
		if g, o := skDosyaAdindan(d); o {
			t.Errorf("%q geçersiz olmalı, %q", d, g)
		}
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

	if n := devral(ctx, db, 9210); n != 1 {
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
	if n := devral(ctx, db, 9210); n != 0 {
		t.Errorf("ikinci çağrı 0 bağlamalı: %d", n)
	}
	yet0 := silinenYetim(t, db, 9205, "adm.test", "c_adm", "c_adm-20261001-000000.tar.gz", 0, "NULL")
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (9211,'adm.test','c_adm')`)
	if n := devral(ctx, db, 9211); n != 1 || g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE id=? AND domain_id=9211`, yet0) != 1 {
		t.Errorf("sahipsiz (reseller NULL=0) eşleşmesi: %d", n)
	}
	if devral(ctx, db, 424242) != 0 {
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
