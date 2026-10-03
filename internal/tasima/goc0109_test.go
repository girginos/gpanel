package tasima

// goc0109_test.go — 0109_tasima_kaynak_kapsam.sql göçü ve oturumKaydet'in hata yolu.
//
// Taban şema GERÇEK göç dosyalarından kurulur (0051 + 0055 + 0096 = 181'deki canlı
// tasima_isleri/tasima_kalemleri/cp_ayarlar). Bağlantı katı kiptedir: canlıda
// @@GLOBAL.sql_mode STRICT_TRANS_TABLES içerir; gevşek kipte geçersiz ENUM değeri
// sessizce '' olur ve gpanel sınaması boşa yeşil yanardı.
//
// Gerçek MariaDB gerekir; GOSP_TEST_GECICI_DSN yoksa ATLANIR. Test yalnız kendi açtığı
// gosp_test_g109_<zaman> veritabanını kullanır ve sonunda siler.

import (
	"bytes"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

const g109Goc = "0109_tasima_kaynak_kapsam.sql"

// g109Taban: 0109'dan önceki canlı şemayı kuran göçler.
var g109Taban = []string{"0051_tasima.sql", "0055_eklenti_lisans.sql", "0096_tasima_oturum.sql"}

func g109DB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li test atlandı")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("DSN çözülemedi: %v", err)
	}
	kcfg := cfg.Clone()
	kcfg.DBName = ""
	kok, err := sql.Open("mysql", kcfg.FormatDSN())
	if err != nil {
		t.Fatalf("db açılamadı: %v", err)
	}
	ad := fmt.Sprintf("gosp_test_g109_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		_ = kok.Close()
		t.Fatalf("geçici veritabanı oluşturulamadı: %v", err)
	}
	t.Cleanup(func() {
		_, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad)
		_ = kok.Close()
	})
	c := cfg.Clone()
	c.DBName = ad
	p := map[string]string{}
	for k, v := range c.Params {
		p[k] = v
	}
	p["sql_mode"] = "'STRICT_TRANS_TABLES'"
	c.Params = p
	db, err := sql.Open("mysql", c.FormatDSN())
	if err != nil {
		t.Fatalf("db açılamadı: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var kip string
	if err := db.QueryRow(`SELECT @@SESSION.sql_mode`).Scan(&kip); err != nil || !strings.Contains(kip, "STRICT_TRANS_TABLES") {
		t.Fatalf("bağlantı katı kipte değil (%q, %v) — sınama canlıyı yansıtmazdı", kip, err)
	}
	return db
}

// g109GocUygula: cmd/server runMigrations ile aynı ayrıştırma ("--" ile başlayan satırlar
// atılır, ";" ile bölünür) ama koşucunun zararsız saydığı "zaten var" türü hatalar dahil
// HİÇBİR hata yutulmaz: 0109 ikinci koşuda da tek hata vermeden geçmeli.
func g109GocUygula(t *testing.T, db *sql.DB, dosya string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "migrations", dosya))
	if err != nil {
		t.Fatalf("göç okunamadı (%s): %v", dosya, err)
	}
	var temiz []string
	for _, satir := range strings.Split(string(body), "\n") {
		if s := strings.TrimSpace(satir); s == "" || strings.HasPrefix(s, "--") {
			continue
		}
		temiz = append(temiz, satir)
	}
	for _, ifade := range strings.Split(strings.Join(temiz, "\n"), ";") {
		if s := strings.TrimSpace(ifade); s != "" {
			if _, err := db.Exec(s); err != nil {
				t.Fatalf("GÖÇ HATASI (%s): %v\n%s", dosya, err, s)
			}
		}
	}
}

func g109Taban0109Oncesi(t *testing.T) *sql.DB {
	t.Helper()
	db := g109DB(t)
	for _, f := range g109Taban {
		g109GocUygula(t, db, f)
	}
	return db
}

// g109Gunluk: std log çıktısını test boyunca tampona alır.
func g109Gunluk(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	eski := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(eski) })
	return &buf
}

// Kaynak kimliği boş: gizli.SaklaBagli'ye (anahtar dosyası) hiç inilmez.
var g109Girdi = kaynakGirdi{Tip: "gpanel", Host: "203.0.113.7", Port: 22, Kullanici: "root"}

// Hata: 0109'dan önce kaynak_tip ENUM('cpanel','plesk','directadmin') gpanel'i reddeder ve
// oturumKaydet bunu sessizce 0'a çeviriyordu. 0109 sonrası aynı çağrı satırı yazar.
func TestGoc0109_GpanelOturumuYazilir(t *testing.T) {
	db := g109Taban0109Oncesi(t)
	h := &Handlers{DB: db}
	hesaplar := []Hesap{{AlanAdi: "ornek.test"}}
	gunluk := g109Gunluk(t)

	if id := h.oturumKaydet(g109Girdi, hesaplar); id != 0 {
		t.Fatalf("0109 öncesi şemada gpanel oturumu yazıldı (id=%d) — taban canlıyı yansıtmıyor", id)
	}
	if g := gunluk.String(); !strings.Contains(g, "oturum kaydedilemedi") || !strings.Contains(g, "kaynak_tip") {
		t.Errorf("oturumKaydet INSERT hatasını yuttu, günlükte iz yok: %q", g)
	}

	g109GocUygula(t, db, g109Goc)
	gunluk.Reset()
	id := h.oturumKaydet(g109Girdi, hesaplar)
	if id <= 0 {
		t.Fatalf("0109 sonrası gpanel oturumu yazılamadı; günlük: %q", gunluk.String())
	}
	var tip string
	var rid uint64
	var uid sql.NullInt64
	if err := db.QueryRow(`SELECT kaynak_tip, sahip_rid, baslatan_uid FROM tasima_isleri WHERE id=?`, id).
		Scan(&tip, &rid, &uid); err != nil {
		t.Fatalf("oturum satırı okunamadı: %v", err)
	}
	if tip != "gpanel" || rid != 0 || uid.Valid {
		t.Fatalf("oturum satırı = (tip=%q sahip_rid=%d baslatan_uid=%v), beklenen (gpanel, 0, NULL)", tip, rid, uid)
	}
	if gunluk.Len() != 0 {
		t.Errorf("başarılı kayıtta günlüğe hata düştü: %q", gunluk.String())
	}
}

// Tablo hiç yoksa hem eski oturum temizliği hem INSERT düşer; ikisi de günlüğe yazılır.
func TestOturumKaydet_HataGunlugeYazilir(t *testing.T) {
	h := &Handlers{DB: g109DB(t)}
	gunluk := g109Gunluk(t)
	if id := h.oturumKaydet(g109Girdi, nil); id != 0 {
		t.Fatalf("tablo yokken id=%d döndü", id)
	}
	g := gunluk.String()
	for _, iz := range []string{"eski oturum silinemedi", "oturum kaydedilemedi", "tasima_isleri"} {
		if !strings.Contains(g, iz) {
			t.Errorf("günlükte %q yok: %q", iz, g)
		}
	}
}

func g109Sema(t *testing.T, db *sql.DB) string {
	t.Helper()
	var b strings.Builder
	for _, tablo := range []string{"tasima_isleri", "tasima_kalemleri", "tasima_kaynak_ayar"} {
		var ad, ddl string
		if err := db.QueryRow("SHOW CREATE TABLE "+tablo).Scan(&ad, &ddl); err != nil {
			t.Fatalf("SHOW CREATE TABLE %s: %v", tablo, err)
		}
		b.WriteString(ddl + "\n")
	}
	return b.String()
}

func g109Ayar(t *testing.T, db *sql.DB) string {
	t.Helper()
	var v string
	if err := db.QueryRow(`SELECT deger FROM cp_ayarlar WHERE anahtar='tasima_bayi_acik'`).Scan(&v); err != nil {
		t.Fatalf("tasima_bayi_acik okunamadı: %v", err)
	}
	return v
}

// İki kez koşunca şema aynı kalır, göç öncesi (AdminOnly döneminden) satırlar kök kapsamına
// (sahip_rid=0) düşer ve kökün sonradan açtığı bayi anahtarı yeniden koşuda ezilmez.
func TestGoc0109_IkiKezKosuncaAyni(t *testing.T) {
	db := g109Taban0109Oncesi(t)
	res, err := db.Exec(`INSERT INTO tasima_isleri (kaynak_tip, kaynak_host, durum, baslatan)
		VALUES ('plesk', '198.51.100.9', 'tamam', 'admin')`)
	if err != nil {
		t.Fatal(err)
	}
	eskiID, _ := res.LastInsertId()
	if _, err := db.Exec(`INSERT INTO tasima_kalemleri (is_id, kaynak_hesap, alan_adi, durum)
		VALUES (?, 'u1', 'eski.test', 'tamam')`, eskiID); err != nil {
		t.Fatal(err)
	}

	g109GocUygula(t, db, g109Goc)
	if v := g109Ayar(t, db); v != "0" {
		t.Fatalf("tasima_bayi_acik tohumu = %q, beklenen '0' (bayide varsayılan kapalı)", v)
	}
	ilk := g109Sema(t, db)
	if _, err := db.Exec(`UPDATE cp_ayarlar SET deger='1' WHERE anahtar='tasima_bayi_acik'`); err != nil {
		t.Fatal(err)
	}

	g109GocUygula(t, db, g109Goc)
	if ikinci := g109Sema(t, db); ikinci != ilk {
		t.Fatalf("ikinci koşu şemayı değiştirdi:\n--- ilk\n%s\n--- ikinci\n%s", ilk, ikinci)
	}
	if v := g109Ayar(t, db); v != "1" {
		t.Fatalf("yeniden koşu kökün anahtar kararını ezdi: %q", v)
	}
	var tip, durum string
	var rid uint64
	var uid sql.NullInt64
	if err := db.QueryRow(`SELECT kaynak_tip, durum, sahip_rid, baslatan_uid FROM tasima_isleri WHERE id=?`, eskiID).
		Scan(&tip, &durum, &rid, &uid); err != nil {
		t.Fatalf("eski iş okunamadı: %v", err)
	}
	if tip != "plesk" || durum != "tamam" || rid != 0 || uid.Valid {
		t.Fatalf("eski iş = (%q, %q, sahip_rid=%d, baslatan_uid=%v), beklenen (plesk, tamam, 0, NULL)", tip, durum, rid, uid)
	}
	var kalem int
	var hj sql.NullString
	if err := db.QueryRow(`SELECT COUNT(*), MAX(hesap_json) FROM tasima_kalemleri WHERE is_id=?`, eskiID).Scan(&kalem, &hj); err != nil {
		t.Fatalf("eski kalem okunamadı: %v", err)
	}
	if kalem != 1 || hj.Valid {
		t.Fatalf("eski kalem = (sayı=%d, hesap_json=%v), beklenen (1, NULL)", kalem, hj)
	}
}

// Sonraki paketlerin dayandığı sözleşme: kolon tipleri, kuyruk durumu, kapsam indeksi
// sırası ve kaynak ayar tablosunun birincil anahtarı.
func TestGoc0109_SemaSozlesmesi(t *testing.T) {
	db := g109Taban0109Oncesi(t)
	g109GocUygula(t, db, g109Goc)

	kolonlar := []struct{ tablo, kolon, tip, bos string }{
		{"tasima_isleri", "kaynak_tip", "varchar(32)", "NO"},
		{"tasima_isleri", "sahip_rid", "bigint(20) unsigned", "NO"},
		{"tasima_isleri", "baslatan_uid", "bigint(20) unsigned", "YES"},
		{"tasima_kalemleri", "hesap_json", "mediumtext", "YES"},
		{"tasima_kaynak_ayar", "kod", "varchar(32)", "NO"},
		{"tasima_kaynak_ayar", "etkin", "tinyint(1)", "NO"},
		{"tasima_kaynak_ayar", "guncelleyen_uid", "bigint(20) unsigned", "YES"},
		{"tasima_kaynak_ayar", "guncelleme", "datetime", "NO"},
	}
	for _, k := range kolonlar {
		var tip, bos string
		err := db.QueryRow(`SELECT COLUMN_TYPE, IS_NULLABLE FROM information_schema.COLUMNS
			WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND COLUMN_NAME=?`, k.tablo, k.kolon).Scan(&tip, &bos)
		if err != nil {
			t.Errorf("%s.%s okunamadı: %v", k.tablo, k.kolon, err)
			continue
		}
		if tip != k.tip || bos != k.bos {
			t.Errorf("%s.%s = (%s, null=%s), beklenen (%s, null=%s)", k.tablo, k.kolon, tip, bos, k.tip, k.bos)
		}
	}

	res, err := db.Exec(`INSERT INTO tasima_isleri (kaynak_tip, kaynak_host, durum) VALUES ('directadmin', '192.0.2.4', 'kuyrukta')`)
	if err != nil {
		t.Fatalf("'kuyrukta' durumu yazılamadı: %v", err)
	}
	id, _ := res.LastInsertId()
	var rid uint64
	if err := db.QueryRow(`SELECT sahip_rid FROM tasima_isleri WHERE id=?`, id).Scan(&rid); err != nil || rid != 0 {
		t.Fatalf("sahip_rid varsayılanı = %d (%v), beklenen 0 (kök)", rid, err)
	}

	indeks := func(tablo, ad string) string {
		t.Helper()
		var s sql.NullString
		if err := db.QueryRow(`SELECT GROUP_CONCAT(COLUMN_NAME ORDER BY SEQ_IN_INDEX) FROM information_schema.STATISTICS
			WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND INDEX_NAME=?`, tablo, ad).Scan(&s); err != nil {
			t.Fatalf("indeks okunamadı (%s.%s): %v", tablo, ad, err)
		}
		return s.String
	}
	if got := indeks("tasima_isleri", "ix_tasima_kapsam"); got != "sahip_rid,durum,id" {
		t.Errorf("ix_tasima_kapsam = %q, beklenen sahip_rid,durum,id", got)
	}
	if got := indeks("tasima_kaynak_ayar", "PRIMARY"); got != "kod" {
		t.Errorf("tasima_kaynak_ayar birincil anahtarı = %q, beklenen kod", got)
	}
}
