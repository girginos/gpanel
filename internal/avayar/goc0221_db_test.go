package avayar

// goc0221_db_test.go — 0221 göçü (GOSP_TEST_GECICI_DSN yoksa ATLANIR): 0078'in eski varsayılan hariç
// tutma listesi yeni varsayılana çekilir; yönetici listesine dokunulmaz; ikinci koşu bir şey değiştirmez.

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

const (
	eskiVarsayilan = "/proc\n/sys\n/dev\n/run\n/var/lib/mysql\n/var/lib/containers\n/var/cache\n/var/backups\n/opt/girginospanel/frontend-dist"
	yeniVarsayilan = eskiVarsayilan + "\n.git/\nnode_modules/\n/wp-content/cache/\n/wp-content/uploads/cache/"
)

func gocDosyaUygula(t *testing.T, db *sql.DB, dosya string) {
	t.Helper()
	b, err := os.ReadFile("../../migrations/" + dosya)
	if err != nil {
		t.Fatalf("göç okunamadı: %v", err)
	}
	var temiz []string
	for _, ln := range strings.Split(string(b), "\n") {
		if s := strings.TrimSpace(ln); s == "" || strings.HasPrefix(s, "--") {
			continue
		}
		temiz = append(temiz, ln)
	}
	for _, q := range strings.Split(strings.Join(temiz, "\n"), ";") {
		if q = strings.TrimSpace(q); q == "" {
			continue
		}
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("GÖÇ HATASI (%s): %v\n%s", dosya, err, q)
		}
	}
}

func geciciAVDB(t *testing.T) *sql.DB {
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
	ad := fmt.Sprintf("gosp_test_avgoc_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad); _ = kok.Close() })
	cfg.DBName = ad
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func haricOku(t *testing.T, db *sql.DB) string {
	t.Helper()
	var s string
	if err := db.QueryRow("SELECT haric_yollar FROM av_ayarlar WHERE id=1").Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestGoc0221_EskiVarsayilanGuncellenirOzelListeyeDokunulmaz(t *testing.T) {
	db := geciciAVDB(t)
	gocDosyaUygula(t, db, "0078_av_ayarlar.sql")
	if got := haricOku(t, db); got != yeniVarsayilan {
		t.Fatalf("0078 bugünkü varsayılanı yazmalı (test sabiti 0078 ile uyumsuz): %q", got)
	}

	// 0078'i 2026-09-16'dan önce uygulamış sunucu: satır eski varsayılanda.
	if _, err := db.Exec("UPDATE av_ayarlar SET haric_yollar=? WHERE id=1", eskiVarsayilan); err != nil {
		t.Fatal(err)
	}
	gocDosyaUygula(t, db, "0221_av_haric_varsayilan.sql")
	if got := haricOku(t, db); got != yeniVarsayilan {
		t.Fatalf("eski varsayılan yeni varsayılana çekilmeliydi: %q", got)
	}
	gocDosyaUygula(t, db, "0221_av_haric_varsayilan.sql") // idempotent
	if got := haricOku(t, db); got != yeniVarsayilan {
		t.Fatalf("ikinci koşu değiştirmemeli: %q", got)
	}

	// Yöneticinin özelleştirdiği liste (eski varsayılan + kendi yolu) aynen kalır.
	ozel := eskiVarsayilan + "\n/srv/yedekler"
	if _, err := db.Exec("UPDATE av_ayarlar SET haric_yollar=? WHERE id=1", ozel); err != nil {
		t.Fatal(err)
	}
	gocDosyaUygula(t, db, "0221_av_haric_varsayilan.sql")
	if got := haricOku(t, db); got != ozel {
		t.Fatalf("özelleştirilmiş listeye dokunulmamalı: %q", got)
	}
}
