package opsbetik

// gpanel ip change — IP doğrulaması ve SINIRLI değiştirme gerileme testleri.
//
// Hata: eski IP alt dize olarak aranıp değiştiriliyordu (grep -F, sed s/IP/…/g, SQL
// REPLACE/LIKE) ve '.' regex jokeri sabitlenmiyordu. 1.2.3.4 → 5.6.7.8 değişiminde
// 11.2.3.45, 1.2.3.40, sürüm "1.2.3.4.5" ve "1x2x3x4" de bozuluyordu; doğrulama
// 999.1.1.1'i kabul ediyordu.
//
// CANLI SİSTEME DOKUNMAZ: betik GOSP_GPANEL_KUTUPHANE=1 ile kütüphane olarak yüklenir,
// yalnız t.TempDir altındaki dosyalar değiştirilir.

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
)

func gpanelYolu(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs("../../assets/ops/gpanel")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// gpanelKos: betiği kütüphane olarak yükleyip verilen kabuk kodunu koşar.
func gpanelKos(t *testing.T, kod string, arg ...string) (string, error) {
	t.Helper()
	betik := `f=$1; shift; set --  "$@"; source "$f" || exit 90; ` + kod
	cmd := exec.Command("bash", append([]string{"-c", betik, "bash", gpanelYolu(t)}, arg...)...)
	cmd.Env = append(os.Environ(), "GOSP_GPANEL_KUTUPHANE=1", "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestGpanelGecerliIP(t *testing.T) {
	for ip, bekl := range map[string]bool{
		"1.2.3.4": true, "0.0.0.0": true, "255.255.255.255": true, "10.0.0.1": true, "192.168.100.200": true,
		"999.1.1.1": false, "256.1.1.1": false, "1.2.3.256": false, "1.2.3": false, "1.2.3.4.5": false,
		"01.2.3.4": false, "1.02.3.4": false, "a.b.c.d": false, "1.2.3.4 ": false, "": false, "1..2.3": false,
	} {
		_, err := gpanelKos(t, `gecerli_ip "$1"`, ip)
		if (err == nil) != bekl {
			t.Errorf("gecerli_ip(%q) = %v, beklenen %v", ip, err == nil, bekl)
		}
	}
}

func TestGpanelDosyadaDegistir_Sinirli(t *testing.T) {
	girdi := strings.Join([]string{
		"listen 1.2.3.4:80;",
		"server 11.2.3.45;",
		"x 1.2.3.40 y",
		"surum=1.2.3.4.5",
		"joker a1x2x3x4b",
		"1.2.3.4 1.2.3.4,1.2.3.4",
		"1.2.3.4",
		"ip=1.2.3.4.",
		"(1.2.3.4)",
		"9.1.2.3.4",
	}, "\n") + "\n"
	bekl := strings.Join([]string{
		"listen 5.6.7.8:80;",
		"server 11.2.3.45;",
		"x 1.2.3.40 y",
		"surum=1.2.3.4.5",
		"joker a1x2x3x4b",
		"5.6.7.8 5.6.7.8,5.6.7.8",
		"5.6.7.8",
		"ip=5.6.7.8.",
		"(5.6.7.8)",
		"9.1.2.3.4",
	}, "\n") + "\n"
	f := filepath.Join(t.TempDir(), "conf")
	if err := os.WriteFile(f, []byte(girdi), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := gpanelKos(t, `dosyada_degistir "$1" 1.2.3.4 5.6.7.8`, f); err != nil {
		t.Fatalf("dosyada_degistir: %v %s", err, out)
	}
	b, _ := os.ReadFile(f)
	if string(b) != bekl {
		t.Fatalf("sonuç:\n%s\nbeklenen:\n%s", b, bekl)
	}
	// Arama deseni de aynı sınırı kullanır (sayım/yedek/doğrulama adımları).
	out, err := gpanelKos(t, `grep -cE "$(sinirli_ere 1.2.3.4)" "$1"`, f)
	if err == nil || strings.TrimSpace(out) != "0" {
		t.Fatalf("değişimden sonra sınırlı arama hâlâ eşleşiyor: %q %v", out, err)
	}
}

// MariaDB tarafı: aynı sınır PCRE ile REGEXP_REPLACE'te (gerçek sunucu).
func TestGpanelSinirliPCRE_MariaDB(t *testing.T) {
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li test atlandı")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = ""
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	desen, err := gpanelKos(t, `sinirli_pcre 1.2.3.4`)
	if err != nil {
		t.Fatal(err)
	}
	for girdi, bekl := range map[string]string{
		"1.2.3.4":                 "5.6.7.8",
		"v=spf1 ip4:1.2.3.4 -all": "v=spf1 ip4:5.6.7.8 -all",
		"11.2.3.45":               "11.2.3.45",
		"1.2.3.40":                "1.2.3.40",
		"1.2.3.4.5":               "1.2.3.4.5",
		"1x2x3x4":                 "1x2x3x4",
		"11.2.3.4":                "11.2.3.4",
		"9.1.2.3.4":               "9.1.2.3.4",
		"1.2.3.4,1.2.3.4":         "5.6.7.8,5.6.7.8",
	} {
		var g string
		if err := db.QueryRow(`SELECT REGEXP_REPLACE(?, ?, '5.6.7.8')`, girdi, desen).Scan(&g); err != nil {
			t.Fatalf("REGEXP_REPLACE(%q): %v", girdi, err)
		}
		if g != bekl {
			t.Errorf("REGEXP_REPLACE(%q) = %q, beklenen %q", girdi, g, bekl)
		}
	}
}
