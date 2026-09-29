package wordpress

// wpdbdusur_test.go — WordPress kaldırma (DB_SIL): satırdaki DB kullanıcısı KOŞULSUZ DROP
// USER edilmemeli. Kullanıcı adı yedekten geri yüklemede kiracının wp-config'inden satıra
// yazılmış olabilir (komşunun kullanıcısı, servis hesabı). Sahiplik denetimi de harf
// duyarlı olmalı (DB adları MariaDB'de harf duyarlı; db_accounts utf8mb4_unicode_ci).
//
// Gerçek MariaDB gerekir: YALNIZ geçici sunucu (GOSP_TEST_GECICI_DSN); yoksa ATLANIR.
// `mysql` CLI PATH'e konan sarmalayıcıyla yalnız o sunucuya gider (@@socket doğrulanır).

import (
	"context"
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

	"girginospanel/internal/hesaplar"
)

type wpoOrtam struct {
	h   *Handlers
	kok *sql.DB
	cfg *mysql.Config
	on  string
}

func wpo_ortam(t *testing.T) *wpoOrtam {
	t.Helper()
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li test atlandı")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("DSN çözülemedi: %v", err)
	}
	if cfg.Net != "unix" || cfg.Addr == "" {
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
		t.Fatalf("mysql sarmalayıcısı geçici sunucuya gitmiyor (%q, %v) — test durduruldu", strings.TrimSpace(string(out)), err)
	}
	kcfg := cfg.Clone()
	kcfg.DBName = ""
	kok, err := sql.Open("mysql", kcfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = kok.Close() })
	on := "wpo" + strconv.FormatInt(time.Now().UnixNano()%1e12, 36)
	ad := "gosp_test_" + on
	if _, err := kok.Exec("CREATE DATABASE " + ad); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, desen := range []string{on + "%", "wp\\_" + on + "%"} {
			for _, q := range []string{`SELECT User FROM mysql.user WHERE User LIKE ?`, `SELECT SCHEMA_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME LIKE ?`} {
				rows, err := kok.Query(q, desen)
				if err != nil {
					continue
				}
				var l []string
				for rows.Next() {
					var s string
					if rows.Scan(&s) == nil {
						l = append(l, s)
					}
				}
				rows.Close()
				for _, s := range l {
					if strings.Contains(q, "mysql.user") {
						_, _ = kok.Exec("DROP USER IF EXISTS '" + s + "'@'localhost'")
					} else {
						_, _ = kok.Exec("DROP DATABASE IF EXISTS `" + s + "`")
					}
				}
			}
		}
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
		`CREATE TABLE domains (id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
			sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '') ENGINE=InnoDB`,
		`CREATE TABLE db_accounts (
			id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
			domain_id BIGINT UNSIGNED NOT NULL,
			db_name VARCHAR(64) NOT NULL UNIQUE,
			db_user VARCHAR(64) NOT NULL,
			db_pass_plain VARCHAR(255) NOT NULL,
			db_host VARCHAR(64) NOT NULL DEFAULT 'localhost',
			notlar VARCHAR(255) NOT NULL DEFAULT ''
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		fmt.Sprintf(`INSERT INTO domains (id, sistem_kullanici) VALUES (1,'c_%s'), (2,'c_%s_x')`, on, on),
	} {
		if _, err := panel.Exec(q); err != nil {
			t.Fatalf("şema: %v\n%s", err, q)
		}
	}
	return &wpoOrtam{h: &Handlers{DB: panel}, kok: kok, cfg: cfg, on: on}
}

func (o *wpoOrtam) exec(t *testing.T, q string) {
	t.Helper()
	if _, err := o.kok.Exec(q); err != nil {
		t.Fatalf("root SQL: %v\n%s", err, q)
	}
}

func (o *wpoOrtam) sayi(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("sayım: %v (%s)", err, q)
	}
	return n
}

func TestWPDBDusur_YabanciKullaniciyiDusurmez(t *testing.T) {
	o := wpo_ortam(t)
	on := o.on
	ctx := context.Background()
	// Komşunun (domain 2) kayıtlı kullanıcısı saldırganın WP satırına yazılmış.
	kurban, kurbanDB := on+"_x_db", on+"_x_main"
	if err := hesaplar.MySQLCreateDB(o.h.DB, 2, kurbanDB, kurban, "KurbanParola123"); err != nil {
		t.Fatal(err)
	}
	wp1 := "wp_" + on + "a"
	o.exec(t, "CREATE DATABASE `"+wp1+"`")
	if _, err := o.h.DB.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (1,?,?,'x')`, wp1, kurban); err != nil {
		t.Fatal(err)
	}
	if err := o.h.wpDBDusur(ctx, wp1, 1); err != nil {
		t.Fatal(err)
	}
	if o.sayi(t, o.kok, `SELECT COUNT(*) FROM mysql.user WHERE User=?`, kurban) != 1 {
		t.Errorf("komşunun kullanıcısı WordPress kaldırmada DROP USER ile düşürüldü")
	}
	if o.sayi(t, o.kok, `SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME=?`, wp1) != 0 {
		t.Errorf("WP veritabanı düşürülmedi")
	}

	// Meşru: panelin açtığı wp_/wpu_ çifti tamamen kalkar.
	wp2, wpu2 := "wp_"+on+"b", on+"_wpu"
	if err := hesaplar.MySQLCreateDB(o.h.DB, 1, wp2, wpu2, "WpParola123456"); err != nil {
		t.Fatal(err)
	}
	if err := o.h.wpDBDusur(ctx, wp2, 1); err != nil {
		t.Fatal(err)
	}
	if o.sayi(t, o.kok, `SELECT COUNT(*) FROM mysql.user WHERE User=?`, wpu2) != 0 {
		t.Errorf("meşru WP kullanıcısı kaldırılmadı")
	}
}

// dbSahipMi harf duyarlı: kayıtlı "wp_<on>c" varken wp-config'teki "wp_<ON>C" (MariaDB'de
// AYRI şema) bu domaine ait sayılmamalı.
func TestDBSahipMi_HarfDuyarli(t *testing.T) {
	o := wpo_ortam(t)
	ad := "wp_" + o.on + "c"
	if _, err := o.h.DB.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (1,?,?,'')`, ad, o.on+"_wpu"); err != nil {
		t.Fatal(err)
	}
	if ok, err := o.h.dbSahipMi(context.Background(), ad, 1); err != nil || !ok {
		t.Fatalf("pozitif kontrol: kayıtlı ad sahip sayılmadı (%v, %v)", ok, err)
	}
	if u := o.h.dbKullaniciAl(context.Background(), ad, 1); u != o.on+"_wpu" {
		t.Fatalf("pozitif kontrol: kayıtlı kullanıcı okunmadı (%q)", u)
	}
	if ok, _ := o.h.dbSahipMi(context.Background(), "wp_"+strings.ToUpper(o.on)+"C", 1); ok {
		t.Errorf("harf varyantı (ayrı şema) bu domaine ait sayıldı")
	}
	if u := o.h.dbKullaniciAl(context.Background(), "wp_"+strings.ToUpper(o.on)+"C", 1); u != "" {
		t.Errorf("harf varyantı için kayıtlı kullanıcı döndü: %q", u)
	}
}
