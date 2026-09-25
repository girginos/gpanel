package hesaplar

// ftp_db_test.go — ftp_accounts.password_md5'e yazan hesaplar yolları (FTPCreate:
// domain oluşturma + cPanel taşıma; FTPUpdatePassword: "FTP parola değiştir")
// değeri $6$ crypt olarak yazmalı ve Pure-FTPd'nin `MYSQLCrypt crypt`
// doğrulamasından (libc crypt(3): crypt(girilen, saklanan) == saklanan) geçmeli.
// Düz metin yazan yol FTP'de 530 üretir (bkz. toplu devir, FTP-DEVIR-DUZMETIN).
//
// Gerçek MariaDB gerekir; DB yoksa ATLANIR:
//
//	GOSP_TEST_GECICI_DSN='root@unix(/tmp/gecici-db/sock)/' go test ./internal/hesaplar/

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func ht_geciciDB(t *testing.T) *sql.DB {
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
	ad := fmt.Sprintf("gosp_test_hesaplar_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad); err != nil {
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
		`CREATE TABLE domains (id BIGINT UNSIGNED NOT NULL PRIMARY KEY) ENGINE=InnoDB`,
		// 0003 + 0104 (password_md5 VARCHAR(255))
		`CREATE TABLE ftp_accounts (
			id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
			domain_id BIGINT UNSIGNED NOT NULL,
			username VARCHAR(64) NOT NULL UNIQUE,
			password_md5 VARCHAR(255) NOT NULL,
			home_dir VARCHAR(255) NOT NULL,
			uid_n INT NOT NULL, gid_n INT NOT NULL,
			status ENUM('active','suspended') DEFAULT 'active',
			CONSTRAINT fk_ftp_domain FOREIGN KEY (domain_id) REFERENCES domains(id) ON DELETE CASCADE
		) ENGINE=InnoDB`,
		`INSERT INTO domains (id) VALUES (5)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("şema: %v", err)
		}
	}
	return db
}

// ht_libcCrypt — Pure-FTPd crypt doğrulaması: crypt(girilen, saklanan) == saklanan.
func ht_libcCrypt(t *testing.T, girilen, saklanan string) bool {
	t.Helper()
	if _, err := exec.LookPath("perl"); err != nil {
		t.Skip("perl yok — libc crypt(3) doğrulaması yapılamıyor")
	}
	cmd := exec.Command("perl", "-e", `print crypt($ENV{HT_P}, $ENV{HT_H}) // ""`)
	cmd.Env = append(os.Environ(), "HT_P="+girilen, "HT_H="+saklanan)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("perl crypt: %v", err)
	}
	return string(out) == saklanan
}

func TestFTPYazicilari_CryptHashYazar(t *testing.T) {
	db := ht_geciciDB(t)
	const kul = "c_hesaplartest"
	oku := func() string {
		var v string
		if err := db.QueryRow(`SELECT password_md5 FROM ftp_accounts WHERE username=?`, kul).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	if ht_libcCrypt(t, "DuzParola12345", "DuzParola12345") {
		t.Fatalf("crypt yardımcısı düz metni kabul etti — test geçersiz")
	}

	ilk := RandomParola(20)
	if err := FTPCreate(db, 5, kul, ilk, 1000, 1000); err != nil {
		t.Fatalf("FTPCreate: %v", err)
	}
	if v := oku(); !IsFTPHash(v) || v == ilk || !ht_libcCrypt(t, ilk, v) {
		t.Fatalf("FTPCreate password_md5'e crypt ile doğrulanabilir $6$ yazmadı (başı=%.3q)", v)
	}

	yeni := RandomParola(20)
	if err := FTPUpdatePassword(db, kul, yeni); err != nil {
		t.Fatalf("FTPUpdatePassword: %v", err)
	}
	v := oku()
	if !IsFTPHash(v) || v == yeni || !ht_libcCrypt(t, yeni, v) {
		t.Fatalf("FTPUpdatePassword password_md5'e crypt ile doğrulanabilir $6$ yazmadı (başı=%.3q)", v)
	}
	if ht_libcCrypt(t, ilk, v) {
		t.Fatalf("eski parola güncellemeden sonra hâlâ geçiyor")
	}
}
