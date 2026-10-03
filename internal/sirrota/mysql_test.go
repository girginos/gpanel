package sirrota

// Gerçek MariaDB (go_test db:true → ağa kapalı GEÇİCİ sunucu) ile ALTER USER yolu.
// Canlı DB'ye dokunmaz: mysql sarmalayıcısı geçici sokete kilitlenir ve doğrulanır.

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func geciciMySQL(t *testing.T) (*mysql.Config, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li test atlandı")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
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
	out, err := exec.Command("mysql", "-N", "-B", "-e", "SELECT @@socket").CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != cfg.Addr {
		t.Fatalf("mysql sarmalayıcısı geçici sunucuya gitmiyor (%q, %v)", strings.TrimSpace(string(out)), err)
	}
	k := cfg.Clone()
	k.DBName = ""
	kok, err := sql.Open("mysql", k.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { kok.Close() })
	return cfg, kok
}

func kullaniciDSN(cfg *mysql.Config, kul, pw string) string {
	return kul + ":" + pw + "@unix(" + cfg.Addr + ")/?parseTime=true"
}

func TestMySQLCLI_TumHostlarYenilenir(t *testing.T) {
	cfg, kok := geciciMySQL(t)
	kul := fmt.Sprintf("gosprota%d", time.Now().UnixNano()%1e9)
	for _, h := range []string{"localhost", "127.0.0.1"} {
		if _, err := kok.Exec("CREATE USER '" + kul + "'@'" + h + "' IDENTIFIED BY 'eskiparola1'"); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, h := range []string{"localhost", "127.0.0.1"} {
			_, _ = kok.Exec("DROP USER IF EXISTS '" + kul + "'@'" + h + "'")
		}
	})
	m := MySQLCLI{}
	ctx := context.Background()
	hostlar, err := m.Hostlar(ctx, kul)
	if err != nil || strings.Join(hostlar, ",") != "127.0.0.1,localhost" {
		t.Fatalf("hostlar=%v err=%v", hostlar, err)
	}
	if err := m.Dogrula(ctx, kullaniciDSN(cfg, kul, "eskiparola1")); err != nil {
		t.Fatalf("eski parola doğrulanamadı: %v", err)
	}

	// Uçtan uca: gerçek MariaDB + sahte Valkey + geçici env
	dir := t.TempDir()
	env := filepath.Join(dir, "env")
	eskiD := kullaniciDSN(cfg, kul, "eskiparola1")
	if err := os.WriteFile(env, []byte("PANEL_DB_DSN="+eskiD+"\nPANEL_JWT_SECRET="+eskiJWT+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &Rotator{EnvYolu: env, BekleyenYolu: filepath.Join(dir, "bek"), EkDizin: dir, DB: m, Log: t.Logf}
	s, err := r.Yenile(ctx, []string{DB})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(env)
	var yeniD string
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "PANEL_DB_DSN=") {
			yeniD = strings.TrimPrefix(l, "PANEL_DB_DSN=")
		}
	}
	if yeniD == eskiD || m.Dogrula(ctx, yeniD) != nil {
		t.Fatal("yeni DSN ile bağlanılamıyor")
	}
	if m.Dogrula(ctx, eskiD) == nil {
		t.Fatal("eski parola hâlâ geçerli (localhost)")
	}
	// 127.0.0.1 tanımı da değişti mi? (soketten sınanamaz → kimlik doğrulama dizgesi)
	var a, b2 string
	if err := kok.QueryRow("SELECT authentication_string FROM mysql.user WHERE User=? AND Host='localhost'", kul).Scan(&a); err != nil {
		t.Fatal(err)
	}
	if err := kok.QueryRow("SELECT authentication_string FROM mysql.user WHERE User=? AND Host='127.0.0.1'", kul).Scan(&b2); err != nil {
		t.Fatal(err)
	}
	if a == "" || a != b2 {
		t.Error("127.0.0.1 host tanımı yeni parolaya geçmedi")
	}
	// GeriDon gerçek MariaDB'de eski parolayı geri koyar
	if err := r.GeriDon(ctx, s); err != nil {
		t.Fatal(err)
	}
	if m.Dogrula(ctx, eskiD) != nil {
		t.Fatal("GeriDon sonrası eski parola geçerli değil")
	}
}
