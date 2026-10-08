package reseller

// sil_test.go — bayi silme: altındaki hosting hesapları ve müşteri kullanıcıları
// ile BİRLİKTE gider (eskiden hosting varsa 409 dönüyordu). Demo hosting'ler
// kullanılır: sistemde userdel/nginx çalışmaz, yalnız panel kayıtları sınanır.
// Geçici MariaDB (GOSP_TEST_GECICI_DSN yoksa atlanır).

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"
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
	kok, err := sql.Open("mysql", cfg.FormatDSN())
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
	w := silIstek(db, 7)
	if w.Code != http.StatusOK {
		t.Fatalf("hostingli bayi silinemedi → %d %s", w.Code, w.Body.String())
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
