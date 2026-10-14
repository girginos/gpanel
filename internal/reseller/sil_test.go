package reseller

// sil_test.go — bayi silme: altındaki hosting hesapları ve müşteri kullanıcıları
// ile BİRLİKTE gider (eskiden hosting varsa 409 dönüyordu). Demo hosting'ler
// kullanılır: sistemde userdel/nginx çalışmaz, yalnız panel kayıtları sınanır.
// Geçici MariaDB (GOSP_TEST_GECICI_DSN yoksa atlanır).

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/toplu"
)

func silGeciciDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li test atlandı")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = ""
	// DROP DATABASE, açık kalmış bir işlemin metadata kilidini bekleyebilir (varsayılan 1 yıl): sızıntıyı gösteren bir test
	// başarısızlığı zaman aşımına dönüşmesin diye bekleme 10 sn ile sınırlı.
	kokCfg := cfg.Clone()
	kokCfg.Params = map[string]string{"lock_wait_timeout": "10"}
	kok, err := sql.Open("mysql", kokCfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	ad := fmt.Sprintf("gosp_test_bsil_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad); _ = kok.Close() })
	cfg.DBName = ad
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, q := range []string{
		`CREATE TABLE users (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, username VARCHAR(64) NOT NULL,
			role ENUM('admin','reseller','user') NOT NULL DEFAULT 'user', reseller_id BIGINT UNSIGNED NULL,
			CONSTRAINT fk_users_reseller FOREIGN KEY (reseller_id) REFERENCES users (id) ON DELETE SET NULL)`,
		`CREATE TABLE domains (id BIGINT PRIMARY KEY, alan_adi VARCHAR(253) NOT NULL, sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '',
			is_demo TINYINT NOT NULL DEFAULT 0, reseller_id BIGINT NOT NULL DEFAULT 0, plan_id BIGINT NULL, customer_id BIGINT NULL)`,
		`CREATE TABLE service_plans (id BIGINT PRIMARY KEY, reseller_id BIGINT NOT NULL DEFAULT 0, domain_id BIGINT NULL)`,
		`CREATE TABLE dns_template (id BIGINT AUTO_INCREMENT PRIMARY KEY, reseller_id BIGINT NOT NULL DEFAULT 0)`,
		`CREATE TABLE audit_log (id BIGINT AUTO_INCREMENT PRIMARY KEY, reseller_id BIGINT NOT NULL DEFAULT 0)`,
		// 0217 sonrası backups (yalnız bu testlerin okuduğu kolonlar): yetim = domain_id=0 AND silinme IS NOT NULL
		`CREATE TABLE backups (id BIGINT AUTO_INCREMENT PRIMARY KEY, domain_id BIGINT NOT NULL, dosya VARCHAR(255) NOT NULL DEFAULT '',
			alan_adi VARCHAR(253) NOT NULL DEFAULT '', sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '', reseller_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
			silinme DATETIME NULL DEFAULT NULL, KEY ix_backup_domain (domain_id))`,
		`CREATE TABLE cp_toplu_isler (id BIGINT AUTO_INCREMENT PRIMARY KEY, tip VARCHAR(24) NOT NULL, durum VARCHAR(16) NOT NULL DEFAULT 'calisiyor',
			toplam INT NOT NULL DEFAULT 0, tamamlanan INT NOT NULL DEFAULT 0, basari INT NOT NULL DEFAULT 0, hata INT NOT NULL DEFAULT 0,
			aktif_domain VARCHAR(255) NOT NULL DEFAULT '', parametre TEXT, detay MEDIUMTEXT, baslatan VARCHAR(64) NOT NULL DEFAULT '',
			baslangic TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, bitis TIMESTAMP NULL)`,
		`INSERT INTO users (id, username, role) VALUES (1,'root','admin'),(7,'bayi7','reseller'),(8,'bayi8','reseller')`,
		`INSERT INTO users (id, username, role, reseller_id) VALUES (70,'mus70','user',7),(71,'mus71','user',7),(80,'mus80','user',8)`,
		`INSERT INTO domains (id, alan_adi, reseller_id, is_demo, customer_id) VALUES
			(1,'b7-a.test',7,1,70),(2,'b7-b.test',7,1,71),(3,'b8-a.test',8,1,80),(4,'kok.test',0,1,NULL)`,
		`INSERT INTO service_plans (id, reseller_id) VALUES (1,7),(2,8)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}
	return db
}

func silIstek(db *sql.DB, id int) *httptest.ResponseRecorder {
	h := &Handlers{DB: db}
	r := chi.NewRouter()
	r.Delete("/resellers/{id}", h.Sil)
	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/resellers/%d", id), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == http.StatusAccepted { // arka plan işi: bitene kadar bekle
		for i := 0; i < 200; i++ {
			var d string
			if err := db.QueryRow(`SELECT durum FROM cp_toplu_isler ORDER BY id DESC LIMIT 1`).Scan(&d); err == nil && d != "calisiyor" {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	return w
}

func silSay(t *testing.T, db *sql.DB, q string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestBayiSil_HostingVeMusterilerleBirlikteGider(t *testing.T) {
	db := silGeciciDB(t)
	havuzKancaSifirla()
	w := silIstek(db, 7)
	if w.Code != http.StatusAccepted {
		t.Fatalf("hostingli bayi silme işi başlamadı → %d %s", w.Code, w.Body.String())
	}
	// Silinen bayinin havuz kalıntıları (dilim, proje sınırı, kullanım serisi) temizlendi — TAM bir kez, doğru bayi için.
	if _, tem := havuzKancaOku(); len(tem) != 1 || tem[0] != 7 {
		t.Errorf("havuz temizliği çağrıları: %v (beklenen [7])", tem)
	}
	var durum string
	var toplam, tamam, basari int
	if err := db.QueryRow(`SELECT durum, toplam, tamamlanan, basari FROM cp_toplu_isler ORDER BY id DESC LIMIT 1`).Scan(&durum, &toplam, &tamam, &basari); err != nil {
		t.Fatal(err)
	}
	// 2 hosting + son adım (bayi kaydı) = 3; hepsi tamamlanmış olmalı.
	if durum != "tamam" || toplam != 3 || tamam != 3 || basari != 3 {
		t.Errorf("iş sonucu: durum=%s toplam=%d tamamlanan=%d basari=%d (beklenen tamam 3/3/3)", durum, toplam, tamam, basari)
	}
	if n := silSay(t, db, `SELECT COUNT(*) FROM domains WHERE reseller_id=7`); n != 0 {
		t.Errorf("bayinin hostingleri kaldı: %d", n)
	}
	if n := silSay(t, db, `SELECT COUNT(*) FROM users WHERE id=7 OR reseller_id=7 OR reseller_id IS NULL AND id IN (70,71)`); n != 0 {
		t.Errorf("bayi ya da müşterileri kaldı (sahipsiz kullanıcı): %d", n)
	}
	// Başka bayiye ve köke DOKUNULMAZ.
	if n := silSay(t, db, `SELECT COUNT(*) FROM domains WHERE id IN (3,4)`); n != 2 {
		t.Errorf("başka bayinin/kökün hostingi silindi: kalan %d", n)
	}
	if n := silSay(t, db, `SELECT COUNT(*) FROM users WHERE id IN (8,80)`); n != 2 {
		t.Errorf("başka bayi/müşterisi silindi: kalan %d", n)
	}
	if n := silSay(t, db, `SELECT COUNT(*) FROM service_plans WHERE reseller_id=7`); n != 0 {
		t.Errorf("bayi planı kaldı: %d", n)
	}
}

func TestBayiSil_BayiOlmayanReddedilir(t *testing.T) {
	db := silGeciciDB(t)
	if w := silIstek(db, 70); w.Code != http.StatusNotFound {
		t.Errorf("müşteri kullanıcısı bayi gibi silindi → %d", w.Code)
	}
	if n := silSay(t, db, `SELECT COUNT(*) FROM domains`); n != 4 {
		t.Errorf("hosting silindi: %d", n)
	}
}

// Çalışan başka bir toplu iş varken bayi silme BAŞLAMAZ (409) ve hiçbir şey silinmez.
func TestBayiSil_MesgulkenReddedilir(t *testing.T) {
	db := silGeciciDB(t)
	id, err := toplu.IsAc(db, "ssl_kur", 1, "{}", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer toplu.IsBitir(db, id, "tamam", nil)
	if w := silIstek(db, 7); w.Code != http.StatusConflict {
		t.Errorf("meşgulken bayi silme → %d (beklenen 409)", w.Code)
	}
	if n := silSay(t, db, `SELECT COUNT(*) FROM domains WHERE reseller_id=7`); n != 2 {
		t.Errorf("meşgulken hosting silindi: kalan %d", n)
	}
}

// Bir hosting silinemezse (burada tetikleyici engeller): DİĞERLERİ silinir, bayi ve
// müşteri kayıtları KALIR, iş 'kismi' biter — sorun giderilince silme tekrarlanabilir.
func TestBayiSil_HostingSilinemezseBayiKalir(t *testing.T) {
	db := silGeciciDB(t)
	havuzKancaSifirla()
	if _, err := db.Exec(`CREATE TRIGGER engel BEFORE DELETE ON domains FOR EACH ROW
		BEGIN IF OLD.id = 2 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'engel'; END IF; END`); err != nil {
		t.Fatal(err)
	}
	if w := silIstek(db, 7); w.Code != http.StatusAccepted {
		t.Fatalf("iş başlamadı → %d", w.Code)
	}
	var durum string
	var hata int
	if err := db.QueryRow(`SELECT durum, hata FROM cp_toplu_isler ORDER BY id DESC LIMIT 1`).Scan(&durum, &hata); err != nil {
		t.Fatal(err)
	}
	if durum != "kismi" || hata != 1 {
		t.Errorf("iş durumu=%s hata=%d (beklenen kismi/1)", durum, hata)
	}
	if n := silSay(t, db, `SELECT COUNT(*) FROM domains WHERE id=1`); n != 0 {
		t.Errorf("silinebilen hosting silinmedi")
	}
	if n := silSay(t, db, `SELECT COUNT(*) FROM domains WHERE id=2`); n != 1 {
		t.Errorf("engelli hosting silinmiş görünüyor")
	}
	if n := silSay(t, db, `SELECT COUNT(*) FROM users WHERE id IN (7,70,71)`); n != 3 {
		t.Errorf("hata varken bayi/müşteri kaydı silindi: kalan %d (beklenen 3)", n)
	}
	// Bayi kaydı kaldığı için havuzu da KALIR (yarım temizlik yok).
	if _, tem := havuzKancaOku(); len(tem) != 0 {
		t.Errorf("bayi kaydı kalırken havuz temizlendi: %v", tem)
	}
}

// O4: bayi silinince onun silinmiş hesaplarının YETİM yedekleri sahipsiz (reseller_id=0) olur — bayi kimliği ileride yeniden
// kullanılırsa yeni bayi eski bayinin yetimlerini (alan adı + sahip eşleşmesiyle) yakalayamasın. Başka bayiye, köke, canlı
// yedeklere ve silinme izi olmayan satırlara DOKUNULMAZ.
func TestBayiSil_YetimYedeklerinSahipligiNotrlenir(t *testing.T) {
	db := silGeciciDB(t)
	for _, q := range []string{
		// bayi 7'nin eski (silinmiş) hesaplarının yetimleri
		`INSERT INTO backups (domain_id, dosya, alan_adi, sistem_kullanici, reseller_id, silinme) VALUES
			(0,'c_eski-20261001-000000.tar.gz','eski.test','c_eski',7,NOW()),(0,'c_eski-20261002-000000.tar.gz','eski.test','c_eski',7,NOW()),
			(0,'c_baska-20261001-000000.tar.gz','baska.test','c_baska',8,NOW()),(0,'c_kok-20261001-000000.tar.gz','kok.test','c_kok',0,NOW())`,
		// silinme izi olmayan (yetim DEĞİL) ve canlı satırlar: sahibi 7 yazılı olsa bile dokunulmaz
		`INSERT INTO backups (domain_id, dosya, reseller_id, silinme) VALUES (0,'yarim-20261001-000000.tar.gz',7,NULL),(3,'c_b8-20261001-000000.tar.gz',8,NULL)`,
		// canlı domaine bağlı satır: bayat bir silinme izi taşısa da yetim DEĞİL (domain_id>0) → sahibi nötrlenmez
		`INSERT INTO backups (domain_id, dosya, reseller_id, silinme) VALUES (3,'c_b8-bayat-20261001-000000.tar.gz',7,NOW())`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}
	if w := silIstek(db, 7); w.Code != http.StatusAccepted {
		t.Fatalf("bayi silme işi başlamadı → %d %s", w.Code, w.Body.String())
	}
	if n := silSay(t, db, `SELECT COUNT(*) FROM users WHERE id=7`); n != 0 {
		t.Fatalf("bayi silinmeliydi: %d", n)
	}
	if n := silSay(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=0 AND reseller_id=7 AND silinme IS NOT NULL`); n != 0 {
		t.Errorf("🔴 silinen bayinin yetimleri sahibini taşımaya devam ediyor (kimlik yeniden kullanılırsa yeni bayi yakalar): %d", n)
	}
	if n := silSay(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=0 AND silinme IS NOT NULL AND sistem_kullanici='c_eski' AND reseller_id=0 AND alan_adi='eski.test'`); n != 2 {
		t.Errorf("bayi 7'nin yetimleri sahipsiz (0) olarak KALMALI (silinmez, alan adı korunur): %d", n)
	}
	if n := silSay(t, db, `SELECT COUNT(*) FROM backups WHERE reseller_id=8 AND silinme IS NOT NULL`); n != 1 {
		t.Errorf("başka bayinin yetimine dokunulmamalı: %d", n)
	}
	if n := silSay(t, db, `SELECT COUNT(*) FROM backups WHERE reseller_id=7 AND silinme IS NULL`); n != 1 {
		t.Errorf("silinme izi olmayan satıra dokunulmamalı: %d", n)
	}
	if n := silSay(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=3 AND reseller_id=8`); n != 1 {
		t.Errorf("canlı domainin satırına dokunulmamalı: %d", n)
	}
	if n := silSay(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=3 AND reseller_id=7 AND silinme IS NOT NULL`); n != 1 {
		t.Errorf("canlı domaine bağlı satır (domain_id>0) yetim sayılıp nötrlenmemeli: %d", n)
	}
}

// Yetim kolonları olmayan şemada (0217 uygulanmamış) bayi silme ENGELLENMEZ (yalnız günlüğe düşer).
func TestBayiSil_BackupsKolonlariYoksaEngellenmez(t *testing.T) {
	db := silGeciciDB(t)
	if _, err := db.Exec(`DROP TABLE backups`); err != nil {
		t.Fatal(err)
	}
	if w := silIstek(db, 7); w.Code != http.StatusAccepted {
		t.Fatalf("iş başlamadı → %d", w.Code)
	}
	if n := silSay(t, db, `SELECT COUNT(*) FROM users WHERE id=7`); n != 0 {
		t.Errorf("backups tablosu yokken de bayi silinmeli: %d", n)
	}
}

// O4 + BLD-1: sahiplik nötrleme ifadesi bir kilit çakışmasına düşerse (burada: başka bir işlem yetim satırı kilitli tutuyor →
// 1205) bayi silme "başarılı" DÖNMEZ — işlem yarım/geri alınmıştır, Commit boş dönüp silmeyi başarılı gösterirdi. Bayi ve müşteri
// kayıtları KALIR (silme yeniden denenebilir); kilit kalkınca aynı silme tamamlanır ve yetim sahipsiz olur.
func TestBayiSil_NotrlemeKilitCakismasi_BayiSilmeDusmeliSonraTamamlanir(t *testing.T) {
	db := silGeciciDB(t)
	if _, err := db.Exec(`INSERT INTO backups (domain_id, dosya, alan_adi, sistem_kullanici, reseller_id, silinme)
		VALUES (0,'c_eski-20261001-000000.tar.gz','eski.test','c_eski',7,NOW())`); err != nil {
		t.Fatal(err)
	}
	// Silmeyi yapan havuz: kilit bekleme süresi 1 sn (1205'e hızlı düşsün; havuzun her bağlantısına uygulanır).
	var adi string
	if err := db.QueryRow(`SELECT DATABASE()`).Scan(&adi); err != nil {
		t.Fatal(err)
	}
	cfg, err := mysql.ParseDSN(os.Getenv("GOSP_TEST_GECICI_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = adi
	cfg.Params = map[string]string{"innodb_lock_wait_timeout": "1"}
	kisa, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = kisa.Close() })

	// Başka bir işlem (ör. eşzamanlı bağlama/süpürme) yetim satırı kilitli tutuyor.
	tutan, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tutan.Rollback() })
	if _, err := tutan.Exec(`SELECT id FROM backups WHERE domain_id=0 AND silinme IS NOT NULL FOR UPDATE`); err != nil {
		t.Fatal(err)
	}

	h := &Handlers{DB: kisa}
	err = h.bayiKaydiniSil(context.Background(), 7)
	if err == nil || !strings.Contains(err.Error(), "nötrlenemedi") {
		t.Fatalf("🔴 kilit çakışmasında bayi silme DÜŞMELİYDİ (yarım işlem 'başarılı' görünmemeli): %v", err)
	}
	if st := kisa.Stats(); st.InUse != 0 {
		t.Fatalf("🔴 başarısız bayi silme işlemi AÇIK bıraktı (Rollback yok): kullanımda %d bağlantı — kilitler ve metadata kilidi tutulur", st.InUse)
	}
	if n := silSay(t, db, `SELECT COUNT(*) FROM users WHERE id IN (7,70,71)`); n != 3 {
		t.Errorf("başarısız silmede bayi/müşteri kayıtları KALMALI (işlem geri alındı): kalan %d (beklenen 3)", n)
	}
	if n := silSay(t, db, `SELECT COUNT(*) FROM service_plans WHERE reseller_id=7`); n != 1 {
		t.Errorf("başarısız silmede bayi planı kalmalı: %d", n)
	}
	if n := silSay(t, db, `SELECT COUNT(*) FROM backups WHERE reseller_id=7 AND silinme IS NOT NULL`); n != 1 {
		t.Errorf("başarısız silmede yetimin sahibi değişmemeli: %d", n)
	}

	// kilit kalkınca aynı silme tamamlanır
	_ = tutan.Rollback()
	if err := h.bayiKaydiniSil(context.Background(), 7); err != nil {
		t.Fatalf("kilit kalkınca bayi silme tamamlanmalı: %v", err)
	}
	if n := silSay(t, db, `SELECT COUNT(*) FROM users WHERE id=7`); n != 0 {
		t.Errorf("bayi silinmeliydi: %d", n)
	}
	if n := silSay(t, db, `SELECT COUNT(*) FROM backups WHERE reseller_id=0 AND silinme IS NOT NULL AND sistem_kullanici='c_eski'`); n != 1 {
		t.Errorf("yetim sahipsiz (0) olmalı: %d", n)
	}
}
