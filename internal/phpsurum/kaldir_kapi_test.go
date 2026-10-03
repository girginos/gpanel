package phpsurum

// kaldir_kapi_test.go — PHP sürümü kaldırma kapısı: o sürüme bağlı alt alan adı ya
// da per-tenant PHP-FPM birimi varken kaldırma REDDEDİLİR (aksi halde birimler
// ikilisiz kalıp Restart döngüsüne girerdi). Kapı yolları systemd-run'a / dnf'e
// ULAŞMAZ; birim dizini sahte. Geçici MariaDB ister (go_test db:true).

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/provisioner"
)

func kk_panel(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li test atlandı")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || cfg.Net != "unix" || cfg.Addr == "" {
		t.Skip("geçici DSN unix soketi değil")
	}
	switch filepath.Clean(cfg.Addr) {
	case "/var/lib/mysql/mysql.sock", "/run/mysqld/mysqld.sock", "/var/run/mysqld/mysqld.sock", "/tmp/mysql.sock":
		t.Fatalf("GOSP_TEST_GECICI_DSN canlı MariaDB soketini gösteriyor — reddedildi")
	}
	kcfg := cfg.Clone()
	kcfg.DBName = ""
	kok, err := sql.Open("mysql", kcfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = kok.Close() })
	ad := fmt.Sprintf("gosp_test_phpkaldir_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad) })
	pcfg := cfg.Clone()
	pcfg.DBName = ad
	db, err := sql.Open("mysql", pcfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, q := range []string{
		`CREATE TABLE domains (id BIGINT PRIMARY KEY, php_surum VARCHAR(8))`,
		`CREATE TABLE subdomanlar (id INT PRIMARY KEY, domain_id BIGINT, php_surum VARCHAR(8))`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

// kk_dikis — birim kapısı sahte; çağrı sırası kaydedilir. systemd-run'a erişim
// (kapı delinirse) PATH boşaltılarak engellenir.
func kk_dikis(t *testing.T, temizleHata error, birimler []string) *[]string {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	e1, e2 := tenantSahipsizTemizle, tenantBirimleriSurumle
	t.Cleanup(func() { tenantSahipsizTemizle, tenantBirimleriSurumle = e1, e2 })
	sira := &[]string{}
	tenantSahipsizTemizle = func(*sql.DB) ([]provisioner.SahipsizTenantFPM, error) {
		*sira = append(*sira, "temizle")
		return nil, temizleHata
	}
	tenantBirimleriSurumle = func(s string) []string {
		*sira = append(*sira, "birimler:"+s)
		return birimler
	}
	return sira
}

func kk_iste(h *Handlers) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/php-surumler/kaldir", strings.NewReader(`{"surum":"8.2","kaynak":"remi"}`))
	w := httptest.NewRecorder()
	h.Kaldir(w, r)
	return w
}

func TestKaldir_AltAlanAdiKullaniyorsaReddeder(t *testing.T) {
	db := kk_panel(t)
	sira := kk_dikis(t, nil, nil)
	if _, err := db.Exec(`INSERT INTO subdomanlar VALUES (1, 9, '8.2')`); err != nil {
		t.Fatal(err)
	}
	w := kk_iste(&Handlers{DB: db})
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "1 alt alan adi") {
		t.Fatalf("alt alan adı kullanırken 409 beklenir: %d %s", w.Code, w.Body.String())
	}
	if len(*sira) != 0 {
		t.Fatalf("kullanım reddinde birim kapısına gelinmemeli: %v", *sira)
	}
}

func TestKaldir_TenantBirimiKullaniyorsaReddeder(t *testing.T) {
	db := kk_panel(t)
	sira := kk_dikis(t, nil, []string{"c_eski82"})
	w := kk_iste(&Handlers{DB: db})
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "c_eski82") {
		t.Fatalf("sürümü kullanan per-tenant birim varken 409 beklenir: %d %s", w.Code, w.Body.String())
	}
	// Önce sahipsiz uzlaştırması, SONRA kalan birimler.
	if strings.Join(*sira, ",") != "temizle,birimler:8.2" {
		t.Fatalf("çağrı sırası: %v", *sira)
	}
}

func TestKaldir_UzlastirmaHatasindaDurur(t *testing.T) {
	db := kk_panel(t)
	sira := kk_dikis(t, errors.New("domains okunamadı"), nil)
	w := kk_iste(&Handlers{DB: db})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("uzlaştırma hatasında 500 beklenir: %d %s", w.Code, w.Body.String())
	}
	if strings.Join(*sira, ",") != "temizle" {
		t.Fatalf("uzlaştırma hatasında devam edilmemeli: %v", *sira)
	}
}
