package provisioner

// dbadalani_test.go — ÖNCEDEN KAPMA: kısa önekli kiracı (c_<on>) <on>-q.com henüz yokken
// "<on>_q_main" / "<on>_q_db" adlarını açabilir (ad-alanı kapısı yalnız VAR OLAN domainlere
// bakar). Yeni domain yine c_<on>_q alsaydı varsayılan DB'si yalnız-yeni oluşturmada
// reddedilir, ad saldırganda kalırdı. BenzersizKullanici artık sonraki sayı ekine geçer.
//
// Gerçek MariaDB gerekir: YALNIZ geçici sunucu (GOSP_TEST_GECICI_DSN); yoksa ATLANIR.
// `mysql` CLI PATH'e konan sarmalayıcıyla yalnız o sunucuya gider (@@socket doğrulanır).
// Sistem kullanıcısı / /home denetimleri yalnız OKUR (useradd yok).

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func TestBenzersizKullanici_DBAdAlaniKapilmissa(t *testing.T) {
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li test atlandı")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || cfg.Net != "unix" || cfg.Addr == "" {
		t.Skip("geçici DSN unix soketi değil")
	}
	gercek, err := exec.LookPath("mysql")
	if err != nil {
		t.Skip("mysql istemcisi yok")
	}
	dir := t.TempDir()
	betik := "#!/bin/sh\nexec '" + gercek + "' --no-defaults --protocol=socket --socket='" + cfg.Addr + "' --user=root \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "mysql"), []byte(betik), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if out, err := exec.Command("mysql", "-N", "-B", "-e", "SELECT @@socket").CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != cfg.Addr {
		t.Fatalf("mysql sarmalayıcısı geçici sunucuya gitmiyor (%q, %v)", strings.TrimSpace(string(out)), err)
	}
	kcfg := cfg.Clone()
	kcfg.DBName = ""
	kok, err := sql.Open("mysql", kcfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = kok.Close() })
	on := "prv" + strconv.FormatInt(time.Now().UnixNano()%1e12, 36)
	ad := "gosp_test_" + on
	if _, err := kok.Exec("CREATE DATABASE " + ad); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = kok.Exec("DROP USER IF EXISTS '" + on + "_r_db'@'localhost'")
		_, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad)
	})
	pcfg := cfg.Clone()
	pcfg.DBName = ad
	panel, err := sql.Open("mysql", pcfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = panel.Close() })
	for _, q := range []string{
		`CREATE TABLE domains (id BIGINT UNSIGNED NOT NULL PRIMARY KEY, sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '') ENGINE=InnoDB`,
		`CREATE TABLE db_accounts (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL,
			db_name VARCHAR(64) NOT NULL UNIQUE, db_user VARCHAR(64) NOT NULL, db_pass_plain VARCHAR(255) NOT NULL)
			ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		fmt.Sprintf(`INSERT INTO domains (id, sistem_kullanici) VALUES (1,'c_%s')`, on),
	} {
		if _, err := panel.Exec(q); err != nil {
			t.Fatalf("şema: %v\n%s", err, q)
		}
	}
	eski := pkgDB
	pkgDB = panel
	t.Cleanup(func() { pkgDB = eski })

	if u := BenzersizKullanici(on + "-q.test"); u != "c_"+on+"_q" {
		t.Fatalf("boş ad-alanında beklenen c_%s_q, alınan %q", on, u)
	}
	// Kısa önekli kiracı komşunun gelecekteki varsayılan DB adını önceden açmış.
	if _, err := panel.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (1,?,?,'x')`,
		on+"_q_main", on+"_zz"); err != nil {
		t.Fatal(err)
	}
	if u := BenzersizKullanici(on + "-q.test"); u != "c_"+on+"_q2" {
		t.Errorf("önceden kapılmış DB ad-alanı atlanmadı: %q", u)
	}
	// MariaDB'de önceden açılmış kullanıcı (panelde kayıtsız).
	if _, err := kok.Exec("CREATE USER '" + on + "_r_db'@'localhost' IDENTIFIED BY 'RParola12345'"); err != nil {
		t.Fatal(err)
	}
	if u := BenzersizKullanici(on + "-r.test"); u != "c_"+on+"_r2" {
		t.Errorf("MariaDB'de önceden açılmış kullanıcı atlanmadı: %q", u)
	}
}
