package bayikaynak

// testyardim_test.go — ortak test iskeleti: sunucuya dokunan TÜM dikişler (systemd birim dizini,
// /run/systemd/system.control, üyelik/proje işaret dizinleri, ev kökü, cgroup kökü, dış komutlar)
// geçici dizinlere / sahte komut yürütücüsüne çevrilir. Hiçbir test gerçek systemctl, xfs_quota,
// grubby ya da /etc dosyasına DOKUNMAZ.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/bayidilim"
	"girginospanel/internal/cgroup"
)

// sahte: kaydeden sahte komut yürütücüsü. Yanit nil değilse önce o sorulur
// (handled=false → varsayılan davranış).
type sahte struct {
	mu    sync.Mutex
	cagri []string
	Yanit func(satir string) (cikti string, err error, handled bool)
}

var hataCikis3 = errors.New("exit status 3")

func (s *sahte) calistir(_ context.Context, ad string, arg ...string) ([]byte, error) {
	satir := ad + " " + strings.Join(arg, " ")
	s.mu.Lock()
	s.cagri = append(s.cagri, satir)
	f := s.Yanit
	s.mu.Unlock()
	if f != nil {
		if c, err, ok := f(satir); ok {
			return []byte(c), err
		}
	}
	switch {
	case strings.HasPrefix(satir, "systemctl is-active"):
		return []byte("inactive\n"), hataCikis3
	}
	return nil, nil
}

func (s *sahte) Satirlar() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.cagri...)
}

// Say: verilen önekle başlayan çağrı sayısı.
func (s *sahte) Say(onek string) int {
	n := 0
	for _, l := range s.Satirlar() {
		if strings.HasPrefix(l, onek) {
			n++
		}
	}
	return n
}

func (s *sahte) Sifirla() {
	s.mu.Lock()
	s.cagri = nil
	s.mu.Unlock()
}

// reYikici: çalışan hizmeti etkileyen systemctl alt komutları. Uzlaştırma bunlardan HİÇBİRİNİ çağırmaz —
// tek istisna: süreç sayısı 0 olduğu DOĞRULANMIŞ boş dilimin durdurulması (bosDilimiDurdur; sahte
// yürütücü TasksCurrent sorusuna yanıt vermedikçe bu yol hiç çalışmaz).
var reYikici = regexp.MustCompile(`^systemctl (restart|stop|kill|start|try-restart|reload-or-restart|disable|enable|isolate|reboot|poweroff)\b`)

func (s *sahte) YikiciVar() string {
	for _, l := range s.Satirlar() {
		if reYikici.MatchString(l) {
			return l
		}
	}
	return ""
}

type ortam struct {
	Birim, Run, Uyelik, Proje, Ev, Sentinel, Cmdline, Cgroup string
	Komut                                                    *sahte
	Etiket                                                   []string // restorecon çağrıları
	durdurHata                                               error    // bosDilimSahte: "systemctl stop" yanıtı
}

func ortamKur(t *testing.T) *ortam {
	t.Helper()
	k := t.TempDir()
	o := &ortam{
		Birim: filepath.Join(k, "systemd"), Run: filepath.Join(k, "run"), Uyelik: filepath.Join(k, "uyelik"),
		Proje: filepath.Join(k, "proje"), Ev: filepath.Join(k, "home"), Sentinel: filepath.Join(k, "reboot-required"),
		Cmdline: filepath.Join(k, "cmdline"), Cgroup: filepath.Join(k, "cgroup"), Komut: &sahte{},
	}
	for _, d := range []string{o.Birim, o.Run, o.Ev, o.Cgroup} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(o.Cmdline, []byte("BOOT_IMAGE=(hd0,gpt2)/vmlinuz root=/dev/vda4 ro rootflags=uquota\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	eskiB, eskiR, eskiU, eskiP, eskiE, eskiS, eskiC, eskiK := birimDizini, runKontrolDizini, bayidilim.UyelikDizini, projeIsaretDizini, evKoku, diskSentinelYolu, procCmdline, cgroup.Kok
	eskiKomut, eskiUzun, eskiRC, eskiHook, eskiYol := komutCalistir, uzunKomut, restorecon, hookAl(), yolBul
	yolBul = func(ad string) (string, error) { return "/usr/sbin/" + ad, nil }
	birimDizini, runKontrolDizini, bayidilim.UyelikDizini = o.Birim, o.Run, o.Uyelik
	projeIsaretDizini, evKoku, diskSentinelYolu, procCmdline, cgroup.Kok = o.Proje, o.Ev, o.Sentinel, o.Cmdline, o.Cgroup
	komutCalistir, uzunKomut = o.Komut.calistir, o.Komut.calistir
	var mu sync.Mutex
	restorecon = func(yol string) { mu.Lock(); o.Etiket = append(o.Etiket, yol); mu.Unlock() }
	HookKur(Hooklar{})
	t.Cleanup(func() {
		birimDizini, runKontrolDizini, bayidilim.UyelikDizini = eskiB, eskiR, eskiU
		projeIsaretDizini, evKoku, diskSentinelYolu, procCmdline, cgroup.Kok = eskiP, eskiE, eskiS, eskiC, eskiK
		komutCalistir, uzunKomut, restorecon, yolBul = eskiKomut, eskiUzun, eskiRC, eskiYol
		HookKur(eskiHook)
	})
	return o
}

func (o *ortam) Oku(t *testing.T, yol string) string {
	t.Helper()
	b, err := os.ReadFile(yol)
	if err != nil {
		t.Fatalf("okunamadı %s: %v", yol, err)
	}
	return string(b)
}

func (o *ortam) Var(yol string) bool { _, err := os.Lstat(yol); return err == nil }

func (o *ortam) BirimYaz(t *testing.T, ad, icerik string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(o.Birim, ad), []byte(icerik), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ── Geçici MariaDB ────────────────────────────────────────────────────────────

func geciciDB(t *testing.T) *sql.DB {
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
	kokCfg := cfg.Clone()
	kokCfg.Params = map[string]string{"lock_wait_timeout": "10"}
	kok, err := sql.Open("mysql", kokCfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	ad := fmt.Sprintf("gosp_test_bkay_%d", time.Now().UnixNano())
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
	for _, q := range []string{
		`CREATE TABLE users (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, username VARCHAR(64) NOT NULL UNIQUE,
			role ENUM('admin','reseller','user') NOT NULL DEFAULT 'user', max_disk_mb BIGINT NOT NULL DEFAULT 0) ENGINE=InnoDB`,
		`CREATE TABLE reseller_plans (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, ad VARCHAR(64) NOT NULL DEFAULT '') ENGINE=InnoDB`,
		`CREATE TABLE domains (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, alan_adi VARCHAR(253) NOT NULL DEFAULT '',
			sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '', reseller_id BIGINT NOT NULL DEFAULT 0, boyut_kb BIGINT NOT NULL DEFAULT 0) ENGINE=InnoDB`,
		`INSERT INTO users (id, username, role) VALUES (100, 'root', 'admin')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}
	migrasyonUygula(t, db)
	return db
}

// migrasyonUygula: GERÇEK 0219 dosyasını koşucunun kurallarıyla uygular ("--" ile başlayan satırlar
// atılır, gövde ';' ile bölünür). İki kez çağrılabilir (idempotentlik testi).
func migrasyonUygula(t *testing.T, db *sql.DB) {
	t.Helper()
	b, err := os.ReadFile("../../migrations/0219_bayi_kaynak_havuzu.sql")
	if err != nil {
		t.Fatal(err)
	}
	var govde strings.Builder
	for _, ln := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "--") {
			continue
		}
		govde.WriteString(ln + "\n")
	}
	n := 0
	for _, q := range strings.Split(govde.String(), ";") {
		if strings.TrimSpace(q) == "" {
			continue
		}
		n++
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("göç ifadesi başarısız: %v\n%s", err, q)
		}
	}
	if n < 7 {
		t.Fatalf("göçte %d ifade bulundu (en az 7 beklenir)", n)
	}
}

func bayiEkle(t *testing.T, db *sql.DB, id int64, kul string, cpu float64, ramMB, diskMB int64, io int) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO users (id, username, role, max_disk_mb, kaynak_cpu_cekirdek, kaynak_ram_mb, kaynak_io_agirlik)
		VALUES (?,?,'reseller',?,?,?,?)`, id, kul, diskMB, cpu, ramMB, io); err != nil {
		t.Fatal(err)
	}
}

func hostingEkle(t *testing.T, db *sql.DB, id int64, alan, sk string, bayi, boyutKB int64) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id, boyut_kb) VALUES (?,?,?,?,?)`,
		id, alan, sk, bayi, boyutKB); err != nil {
		t.Fatal(err)
	}
}

func say(t *testing.T, db *sql.DB, q string, a ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, a...).Scan(&n); err != nil {
		t.Fatalf("%v: %s", err, q)
	}
	return n
}

// simdiSabit: testlerde sabit, 5 dk'ya hizalı UTC anı.
func simdiSabit() time.Time { return time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC) }
