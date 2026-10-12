package domains

// handlers_dbpw_test.go — PUT /databases/{dbid}/password, "kullanıcısı olmayan
// veritabanı" dalı (db_user=''; granular geri yükleme böyle satır üretir).
// Bayi, KENDİ hostingindeki boş-kullanıcılı DB kaydı üzerinden "kullanıcı
// oluştur" bahanesiyle şunları DEVRALAMAZ ya da KAPAMAZ:
//   - sunucu geneli MariaDB hesabı (root, mysql, panel, pma, pureftpd, roundcube …),
//   - başka kiracının panelde kayıtlı kullanıcısı,
//   - başka kiracının ad alanındaki ad (<önek>_…, c_<sk>_…; en uzun önek sahibi),
//   - MariaDB'de zaten var olan (panelin kaydetmediği) hesap.
// Meşru kullanım bozulmaz: kendi ad alanındaki ya da kimseye ait olmayan ad
// (wpu_*, taşınmış orijinal adlar — wp-config DB_USER) ile kullanıcı oluşur.
//
// Zincir main.go ile AYNI: RequireAuth → DBSahipligiMusteri("dbid") → SetDatabasePassword
// (silme ve optimize de DBSahipligiMusteri — handlers_dbmusteri_test.go).
// Gerçek MariaDB gerekir (GOSP_TEST_GECICI_DSN yoksa ATLANIR). MariaDB hesap
// işlemi SAHTE (dbKullaniciEkle dikişi): canlı sunucunun hesaplarına dokunulmaz.

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/auth"
	"girginospanel/internal/hesaplar"
	"girginospanel/internal/middleware"
)

var dp_sir = []byte("domains-dbpw-test-anahtari-0123456789abcd")

// dp_sahteMariaDB: dbKullaniciEkle'nin sahtesi. mevcut = MariaDB'de zaten var
// olan hesaplar (gerçek MySQLKullaniciEkle bunlarda ErrDBKullaniciMevcut döner).
type dp_sahteMariaDB struct {
	mu          sync.Mutex
	mevcut      map[string]bool
	cagri       []string // "db:kullanici"
	parolaCagri []string // dbParolaDegistir sahtesine gelen kullanıcılar (ALTER USER olacaktı)
}

func (s *dp_sahteMariaDB) parola(_ *sql.DB, _ int64, dbUser, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.parolaCagri = append(s.parolaCagri, dbUser)
	return errors.New("test: parola değişimi sahte (ALTER USER gönderilmez)")
}

func (s *dp_sahteMariaDB) parolaSayi() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.parolaCagri)
}

func (s *dp_sahteMariaDB) ekle(dbName, dbUser, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cagri = append(s.cagri, dbName+":"+dbUser)
	if s.mevcut[dbUser] {
		return hesaplar.ErrDBKullaniciMevcut
	}
	s.mevcut[dbUser] = true
	return nil
}

func (s *dp_sahteMariaDB) sayi() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.cagri)
}

func dp_exec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("SQL hatası: %v\n%s", err, q)
	}
}

// dp_db: geçici veritabanı + şema + başlangıç verisi.
//
//	users   : 1 kök, 2 ve 3 etkin bayiler
//	domains : 20 bayi2.test (c_bayi2, bayi 2)   21 root.test (c_root, bayi 2)
//	          30 kurban.test (c_kurban, bayi 3) 31 bayi2-ek.test (c_bayi2_ek, bayi 3)
//	          22 c-bayi2.test (c_c_bayi2, bayi 3 — c'siz öneki "c_bayi2", bayi2'nin
//	          sk'siyle AYNI kök → eşit kökte TAM-sk sahibi (bayi2) kazanır)
//	db_accounts: 200/201/202 bayi2'nin boş-kullanıcılı DB'leri, 220 c-bayi2.test'in
//	          (bayi 3) boş-kullanıcılı DB'si, 210 root.test'in
//	          boş-kullanıcılı DB'si, 300 kurbanın kayıtlı kullanıcısı (kurban_db),
//	          301 kurbanın boş-kullanıcılı DB'si, 302 kurbanın ad alanı DIŞINDA
//	          kayıtlı kullanıcısı (wpu_kurban1 — WordPress kurulumu adı), 310/311
//	          bayi2-ek.test'in (bayi 3, uzun kök) boş-kullanıcılı DB'leri.
func dp_db(t *testing.T) *sql.DB {
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
	ad := fmt.Sprintf("gosp_test_dbpw_%d", time.Now().UnixNano())
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
	for _, q := range []string{
		`CREATE TABLE users (id BIGINT UNSIGNED PRIMARY KEY, username VARCHAR(64) NOT NULL DEFAULT '',
			role VARCHAR(16) NOT NULL DEFAULT 'user', status VARCHAR(16) NOT NULL DEFAULT 'active',
			token_gecersiz_ts BIGINT NULL, last_activity_ts BIGINT NOT NULL DEFAULT 0)`,
		`CREATE TABLE domains (id BIGINT UNSIGNED PRIMARY KEY, alan_adi VARCHAR(253) NOT NULL,
			sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '', reseller_id BIGINT NOT NULL DEFAULT 0,
			is_demo TINYINT NOT NULL DEFAULT 0, askida TINYINT NOT NULL DEFAULT 0,
			askida_kaynak VARCHAR(32) NOT NULL DEFAULT '')`,
		// Müşteri oturumu (RequireAuth → musteriOturumGecerli) FTP hesap satırına bakar.
		`CREATE TABLE ftp_accounts (id BIGINT UNSIGNED PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL,
			status VARCHAR(16) NOT NULL DEFAULT 'active', token_gecersiz_ts BIGINT NULL)`,
		`INSERT INTO ftp_accounts (id, domain_id) VALUES (900, 30), (901, 20)`,
		`CREATE TABLE db_accounts (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
			domain_id BIGINT UNSIGNED NOT NULL, db_name VARCHAR(64) NOT NULL UNIQUE,
			db_user VARCHAR(64) NOT NULL, db_pass_plain TEXT NOT NULL,
			db_host VARCHAR(64) NOT NULL DEFAULT 'localhost', notlar VARCHAR(255) NULL)`,
		`INSERT INTO users (id, username, role) VALUES (1,'root','admin'), (2,'bayi2','reseller'), (3,'bayi3','reseller')`,
		`INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES
			(20,'bayi2.test','c_bayi2',2), (21,'root.test','c_root',2),
			(22,'c-bayi2.test','c_c_bayi2',3),
			(30,'kurban.test','c_kurban',3), (31,'bayi2-ek.test','c_bayi2_ek',3)`,
		`INSERT INTO db_accounts (id, domain_id, db_name, db_user, db_pass_plain) VALUES
			(200,20,'bayi2_geri','',''), (201,20,'bayi2_geri2','',''), (202,20,'bayi2_geri3','',''),
			(203,20,'bayi2_geri4','',''),
			(210,21,'root_geri','',''), (220,22,'c_c_bayi2_geri','',''), (221,22,'c_c_bayi2_geri2','',''),
			(300,30,'kurban_main','kurban_db',''), (301,30,'kurban_bos','',''),
			(302,30,'wp_kurban1','wpu_kurban1',''),
			(310,31,'bayi2_ek_geri','',''), (311,31,'bayi2_ek_geri2','','')`,
	} {
		dp_exec(t, db, q)
	}
	return db
}

func dp_router(h *Handlers) http.Handler {
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(dp_sir))
		r.With(middleware.DBSahipligiMusteri("dbid")).Delete("/databases/{dbid}", h.DeleteDatabase)
		r.With(middleware.DBSahipligiMusteri("dbid")).Put("/databases/{dbid}/password", h.SetDatabasePassword)
		r.With(middleware.DBSahipligiMusteri("dbid")).Post("/databases/{dbid}/optimize", h.OptimizeDatabase)
	})
	return r
}

func dp_bayi(t *testing.T, rid int64) string {
	t.Helper()
	tok, err := auth.IssueResellerAt(dp_sir, 3600, rid, fmt.Sprintf("bayi%d", rid), rid, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func dp_istek(t *testing.T, h *Handlers, tok string, dbid int64, kullanici string) (int, string) {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"parola": "Saldirgan-Parola-1", "kullanici": kullanici})
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/databases/%d/password", dbid), bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	dp_router(h).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func dp_dbUser(t *testing.T, db *sql.DB, dbid int64) string {
	t.Helper()
	var u string
	if err := db.QueryRow(`SELECT db_user FROM db_accounts WHERE id=?`, dbid).Scan(&u); err != nil {
		t.Fatalf("db_accounts %d okunamadı: %v", dbid, err)
	}
	return u
}

// dp_kur: geçici DB + kapsam DB'si + sahte MariaDB.
func dp_kur(t *testing.T) (*Handlers, *dp_sahteMariaDB) {
	t.Helper()
	db := dp_db(t)
	middleware.Init(db)
	t.Cleanup(func() { middleware.Init(nil) })
	sahte := &dp_sahteMariaDB{mevcut: map[string]bool{
		// Sunucuda gerçekten var olan hesaplar (181: root, mysql, mariadb.sys,
		// panel, pma, pureftpd, roundcube, mailadmin, mailuser) + kurbanın kayıtlı
		// kullanıcısı + panelin kaydetmediği bir yetim hesap.
		"root": true, "mysql": true, "panel": true, "pma": true, "pureftpd": true,
		"roundcube": true, "mailadmin": true, "mailuser": true,
		"kurban_db": true, "wpu_kurban1": true, "yetim_hesap": true,
	}}
	// 🔴 İKİ dikiş de sahte: kayıt bir mutasyonla dolarsa sonraki istek "dolu
	// kullanıcı" dalına (ALTER USER) düşer — o yol da canlı MariaDB'ye İNMEZ.
	eskiEkle, eskiParola := dbKullaniciEkle, dbParolaDegistir
	dbKullaniciEkle, dbParolaDegistir = sahte.ekle, sahte.parola
	t.Cleanup(func() { dbKullaniciEkle, dbParolaDegistir = eskiEkle, eskiParola })
	return &Handlers{DB: db, IPv4: "203.0.113.10"}, sahte
}

// 🔴 ÇEKİRDEK: bayi, boş-kullanıcılı KENDİ DB kaydı üzerinden sunucu geneli ya da
// başka kiracıya ait bir adı "oluşturamaz": MariaDB katmanına HİÇ inilmez
// (sahte çağrılmaz), panel kaydı değişmez.
func TestSetDatabasePassword_BosKullanici_YabanciAdDevralinamaz(t *testing.T) {
	h, sahte := dp_kur(t)
	bayi := dp_bayi(t, 2)
	for _, c := range []struct {
		ad   string
		dbid int64
		kul  string
	}{
		{"root", 200, "root"},
		{"Root (büyük harf)", 200, "Root"},
		{"mysql", 200, "mysql"},
		{"panelin kendi DB hesabı", 200, "panel"},
		{"phpMyAdmin kontrol hesabı", 200, "pma"},
		{"pure-ftpd hesabı", 200, "pureftpd"},
		{"roundcube", 200, "roundcube"},
		{"posta yönetim hesabı", 200, "mailadmin"},
		{"posta hesabı", 200, "mailuser"},
		{"kendi ad alanı 'root' olsa bile sistem hesabı (root.test → c_root)", 210, "root"},
		{"başka kiracının panelde kayıtlı kullanıcısı", 200, "kurban_db"},
		{"başka kiracının ad alanı dışındaki kayıtlı kullanıcısı (wpu_)", 200, "wpu_kurban1"},
		{"başka kiracının ad alanı (önek_)", 200, "kurban_wp"},
		{"başka kiracının ad alanı (c_sk_ — taşıma adı)", 200, "c_kurban_db"},
		{"daha özgül önek başka kiracıda (bayi2_ek_ ⊂ bayi2_)", 200, "bayi2_ek_wp"},
	} {
		once := sahte.sayi()
		kod, govde := dp_istek(t, h, bayi, c.dbid, c.kul)
		if kod != http.StatusConflict {
			t.Errorf("%s (%q): 409 beklenir, alınan %d %s", c.ad, c.kul, kod, govde)
		}
		if sahte.sayi() != once {
			t.Errorf("%s (%q): MariaDB katmanına inildi (CREATE/ALTER USER gönderilecekti)", c.ad, c.kul)
		}
		if u := dp_dbUser(t, h.DB, c.dbid); u != "" {
			t.Errorf("%s (%q): panel kaydı değişti: db_user=%q", c.ad, c.kul, u)
		}
	}
	// 🔴 EŞİT KÖK: c-bayi2.test (c_c_bayi2, bayi 3) c'siz kökü "c_bayi2" ile bayi2'nin
	// TAM-sk ad-alanına (taşıma adı c_bayi2_db, uygulama c_bayi2_<kod>_<id>) giremez.
	bayi3 := dp_bayi(t, 3)
	for _, kul := range []string{"c_bayi2_db", "c_bayi2_wp_5", "C_BAYI2_DB"} {
		once := sahte.sayi()
		if kod, govde := dp_istek(t, h, bayi3, 220, kul); kod != http.StatusConflict {
			t.Errorf("eşit kök (%q): 409 beklenir, alınan %d %s", kul, kod, govde)
		}
		if sahte.sayi() != once || dp_dbUser(t, h.DB, 220) != "" {
			t.Errorf("eşit kök (%q): MariaDB katmanına inildi ya da kayıt değişti", kul)
		}
	}
	// 🔴 KÖKÜN KENDİSİ (inceleme (b)): bayi2-ek.test (c_bayi2_ek, bayi 3) kendi tam
	// sk'sını / c'siz kökünü açamaz — bunlar kısa köklü bayi2.test'in türettiği adlar
	// (taşıma c_bayi2_<ek>, sağlama bayi2_<ek>) biçimindedir; açılırsa kurbanın
	// taşıması/sağlaması kilitlenirdi. Bayi 2 de kendi kökünün kendisini açamaz.
	for _, c := range []struct {
		bayi int64
		dbid int64
		kul  string
	}{
		{3, 310, "c_bayi2_ek"},
		{3, 310, "bayi2_ek"},
		{3, 310, "C_BAYI2_EK"},
		{2, 200, "c_bayi2"},
		{2, 200, "bayi2"},
	} {
		once := sahte.sayi()
		if kod, govde := dp_istek(t, h, dp_bayi(t, c.bayi), c.dbid, c.kul); kod != http.StatusConflict {
			t.Errorf("kökün kendisi (%q): 409 beklenir, alınan %d %s", c.kul, kod, govde)
		}
		if sahte.sayi() != once || dp_dbUser(t, h.DB, c.dbid) != "" {
			t.Errorf("kökün kendisi (%q): MariaDB katmanına inildi ya da kayıt değişti", c.kul)
		}
	}
	if u := dp_dbUser(t, h.DB, 300); u != "kurban_db" {
		t.Errorf("kurbanın kaydı değişti: %q", u)
	}
	if u := dp_dbUser(t, h.DB, 302); u != "wpu_kurban1" {
		t.Errorf("kurbanın kaydı değişti: %q", u)
	}
	if n := sahte.parolaSayi(); n != 0 {
		t.Errorf("ALTER USER yoluna %d kez inildi: %v", n, sahte.parolaCagri)
	}
}

// MariaDB'de zaten var olan (panelin kaydetmediği) hesap: alt katman
// ErrDBKullaniciMevcut döner → 409, panel kaydı YAZILMAZ.
func TestSetDatabasePassword_BosKullanici_VarOlanHesap409(t *testing.T) {
	h, sahte := dp_kur(t)
	once := sahte.sayi()
	kod, govde := dp_istek(t, h, dp_bayi(t, 2), 200, "yetim_hesap")
	if kod != http.StatusConflict {
		t.Errorf("var olan hesap: 409 beklenir, alınan %d %s", kod, govde)
	}
	if sahte.sayi() != once+1 {
		t.Errorf("var olan hesap kararı MariaDB katmanında verilmeliydi (çağrı=%d)", sahte.sayi()-once)
	}
	if u := dp_dbUser(t, h.DB, 200); u != "" {
		t.Errorf("var olan hesap panel kaydına yazıldı: %q", u)
	}
}

// Başka bayinin boş-kullanıcılı DB kaydına hiç ulaşılamaz (404, varlık sızmaz).
func TestSetDatabasePassword_BaskaBayininKaydi404(t *testing.T) {
	h, sahte := dp_kur(t)
	kod, govde := dp_istek(t, h, dp_bayi(t, 2), 301, "bayi2_x")
	if kod != http.StatusNotFound {
		t.Errorf("başka bayinin DB kaydı: 404 beklenir, alınan %d %s", kod, govde)
	}
	if sahte.sayi() != 0 || dp_dbUser(t, h.DB, 301) != "" {
		t.Errorf("başka bayinin kaydına dokunuldu")
	}
}

// Meşru kullanım: kendi ad alanındaki ad ve kimseye ait olmayan wp-config adı
// (wpu_*) ile kullanıcı oluşur, panel kaydı güncellenir.
func TestSetDatabasePassword_BosKullanici_MesruAdlar(t *testing.T) {
	h, sahte := dp_kur(t)
	for _, c := range []struct {
		bayi   int64
		dbid   int64
		dbName string
		kul    string
	}{
		{2, 201, "bayi2_geri2", "bayi2_blog"},
		{2, 202, "bayi2_geri3", "wpu_ab12cd"},
		// Eşit kökte (c_bayi2 = c_c_bayi2'nin c'siz kökü) TAM-sk sahibi kendi taşıma adını
		// kullanabilir; c-bayi2.test ise kendi tam-sk ad-alanında serbesttir.
		{2, 203, "bayi2_geri4", "c_bayi2_db"},
		{3, 221, "c_c_bayi2_geri2", "c_c_bayi2_db"},
		// Uzun köklü kiracı kökünün kendisini değil, kendi ad-alanındaki adı kullanır.
		{3, 311, "bayi2_ek_geri2", "c_bayi2_ek_db"},
	} {
		kod, govde := dp_istek(t, h, dp_bayi(t, c.bayi), c.dbid, c.kul)
		if kod != http.StatusOK {
			t.Errorf("%q: 200 beklenir, alınan %d %s", c.kul, kod, govde)
			continue
		}
		if u := dp_dbUser(t, h.DB, c.dbid); u != c.kul {
			t.Errorf("%q: panel kaydı güncellenmedi (db_user=%q)", c.kul, u)
		}
		sahte.mu.Lock()
		son := sahte.cagri[len(sahte.cagri)-1]
		sahte.mu.Unlock()
		if son != c.dbName+":"+c.kul {
			t.Errorf("%q: MariaDB çağrısı yanlış hedef: %s", c.kul, son)
		}
	}
}
