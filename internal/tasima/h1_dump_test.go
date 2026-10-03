package tasima

// h1_dump_test.go — H1 içe aktarma sertleştirmesi:
//   - dumpFiltre düşman dökümün dosya sistemine erişen istemci komutlarını yakalayıp
//     akışı KESER (Reddedildi); meşru dökümde DEFINER'ı söker, dump-sonu damgasını izler.
//   - tehlikeliDumpSatiri satır sınıflandırıcısı: gerçek mysqldump çıktısında yanlış
//     pozitif üretmez, düşman satırları (\!, system, source, \u, LOAD DATA LOCAL) yakalar.
//   - importArgs kısıtlı istemci bayraklarını (--sandbox --binary-mode --local-infile=0) verir.
//   - veritabanlariniAktar: istemci --sandbox desteklemiyorsa bayi işi kapalı.
//
// Ağa/canlı sisteme/DB'ye dokunmaz (saf birim testleri).

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func drainFiltre(t *testing.T, girdi string) (*dumpFiltre, string) {
	t.Helper()
	f := &dumpFiltre{}
	r := f.Sar(strings.NewReader(girdi))
	out, _ := io.ReadAll(r)
	f.Bekle()
	return f, string(out)
}

func TestDumpFiltreMesruDokum(t *testing.T) {
	girdi := "CREATE TABLE `t` (id INT);\n" +
		"/*!50013 DEFINER=`kok`@`localhost` SQL SECURITY DEFINER */\n" +
		"INSERT INTO `t` VALUES (1),(2);\n" +
		"-- Dump completed on 2026-09-29\n"
	f, out := drainFiltre(t, girdi)
	if f.Reddedildi {
		t.Fatalf("meşru döküm reddedildi")
	}
	if !f.Tamam {
		t.Errorf("dump-sonu damgası tanınmadı")
	}
	if strings.Contains(out, "DEFINER") {
		t.Errorf("DEFINER sökülmedi: %q", out)
	}
	if !strings.Contains(out, "CREATE TABLE") || !strings.Contains(out, "INSERT INTO") {
		t.Errorf("meşru SQL geçmedi: %q", out)
	}
}

func TestDumpFiltreDusmanDokumReddedilir(t *testing.T) {
	// Tehlikeli satır BAŞTA: hiçbir SQL mysql'e ulaşmadan akış kesilir (yan etki yok).
	girdi := "\\! touch /tmp/gosp_pwned\n" +
		"CREATE TABLE `arka_kapi` (id INT);\n" +
		"-- Dump completed\n"
	f, out := drainFiltre(t, girdi)
	if !f.Reddedildi {
		t.Fatalf("düşman döküm reddedilmedi")
	}
	if strings.Contains(out, "CREATE TABLE") || strings.Contains(out, "arka_kapi") {
		t.Errorf("reddedilen dökümden SQL mysql'e sızdı: %q", out)
	}
	if f.Tamam {
		t.Errorf("reddedilen döküm 'tamam' işaretlendi")
	}
}

// 🔴 REGRESYON (kilitlenme): tüketici (mysql) girdinin tamamını okumadan çıkarsa —
// ilk SQL hatası ya da ctx iptali/zaman aşımı — Bekle yine DÖNMELİ. Dönmezse
// HesapAktar asılı kalır, tek global iş yuvası (aktifIsID) kilitlenir. Girdi, işletim
// sistemi borusu (64 KiB) + io.Pipe tamponundan büyük olmalı ki süzgeç pw.Write'ta
// beklesin; dbAktar'daki sıra (Stdin=Sar, Run, Bekle) birebir izlenir.
func TestDumpFiltreErkenCikanTuketici(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh yok")
	}
	satir := "INSERT INTO `t` VALUES ('" + strings.Repeat("x", 1000) + "');\n"
	buyuk := strings.Repeat(satir, 2048) // ~2 MB
	for _, c := range []struct {
		ad    string
		komut func(ctx context.Context) *exec.Cmd
	}{
		// mysql ilk SQL hatasında (--force yok) girdinin kalanını okumadan çıkar.
		{"erken hata çıkışı", func(ctx context.Context) *exec.Cmd {
			return exec.CommandContext(ctx, "sh", "-c", "head -c 10 >/dev/null; exit 1")
		}},
		// ctx iptali / hesapTimeout alt süreci öldürür (girdi hiç okunmaz).
		{"ctx iptali", func(ctx context.Context) *exec.Cmd {
			return exec.CommandContext(ctx, "sleep", "30")
		}},
	} {
		ctx, iptal := context.WithTimeout(context.Background(), 300*time.Millisecond)
		f := &dumpFiltre{}
		cmd := c.komut(ctx)
		cmd.Stdin = f.Sar(strings.NewReader(buyuk))
		if err := cmd.Run(); err == nil {
			t.Errorf("%s: alt süreç hatayla dönmeliydi", c.ad)
		}
		iptal()
		dondu := make(chan struct{})
		go func() { f.Bekle(); close(dondu) }()
		select {
		case <-dondu:
		case <-time.After(10 * time.Second):
			t.Fatalf("%s: Bekle dönmedi — süzgeç goroutine'i pw.Write'ta takıldı", c.ad)
		}
		if f.Tamam || f.Reddedildi {
			t.Errorf("%s: yarım akış tamam/red işaretlenmemeli (tamam=%v red=%v)", c.ad, f.Tamam, f.Reddedildi)
		}
	}
}

func TestTehlikeliDumpSatiri(t *testing.T) {
	tehlikeli := []string{
		`\! rm -rf /`,
		`  \!id`,
		`\. /etc/passwd`,
		`\u mysql`,
		`system cat /etc/shadow`,
		`  SYSTEM  echo x`,
		`source /root/.my.cnf`,
		`SOURCE /tmp/x.sql`,
		`LOAD DATA LOCAL INFILE '/etc/passwd' INTO TABLE t`,
		"\tload data local infile 'x'",
	}
	for _, s := range tehlikeli {
		if !tehlikeliDumpSatiri(s) {
			t.Errorf("tehlikeli sayılmalıydı: %q", s)
		}
	}
	guvenli := []string{
		"CREATE TABLE `source_log` (id INT);",             // 'source' önekli tanımlayıcı
		"INSERT INTO t VALUES ('system call'),('\\! x');", // veri içinde geçer, satır başında değil
		"INSERT INTO t VALUES ('load data local');",       // veri
		"-- systemd notes",                                // yorum
		"/*!40101 SET NAMES utf8mb4 */;",
		"USE `db1`;", // SQL USE (istemci \u değil) — meşru mysqldump --databases çıktısı
		"",
		"  ",
	}
	for _, s := range guvenli {
		if tehlikeliDumpSatiri(s) {
			t.Errorf("güvenli sayılmalıydı: %q", s)
		}
	}
}

func TestImportArgs(t *testing.T) {
	a := importArgs("/var/tmp/x.cnf", "hedef_db")
	if a[len(a)-1] != "hedef_db" {
		t.Errorf("hedef DB son argüman olmalı: %v", a)
	}
	icerir := func(bayrak string) bool {
		for _, x := range a {
			if x == bayrak {
				return true
			}
		}
		return false
	}
	if !icerir("--binary-mode") {
		t.Errorf("--binary-mode eksik: %v", a)
	}
	if !icerir("--local-infile=0") {
		t.Errorf("--local-infile=0 eksik: %v", a)
	}
	// 🔴 --defaults-file İLK argüman ve DIŞLAYICI: --defaults-extra-file'da yöneticinin
	// /root/.my.cnf'i sonradan okunur, kısıtlı kimliği root ile ezerdi.
	if a[0] != "--defaults-file=/var/tmp/x.cnf" {
		t.Errorf("ilk argüman --defaults-file olmalı: %v", a)
	}
	for _, x := range a {
		if strings.HasPrefix(x, "--defaults-extra-file") || x == "--no-defaults" {
			t.Errorf("kapsayıcı olmayan seçenek dosyası argümanı: %v", a)
		}
	}
	// 181'de istemci --sandbox destekler → bayrak eklenmeli. (neg_control: bayrak
	// eklemesi silinirse burada yakalanır.)
	if sandboxKontrol() && !icerir("--sandbox") {
		t.Errorf("istemci --sandbox destekliyor ama bayrak eklenmedi: %v", a)
	}
}

func TestVeritabanlariBayiSandboxKapali(t *testing.T) {
	eski := sandboxKontrol
	t.Cleanup(func() { sandboxKontrol = eski })

	// İstemci --sandbox DESTEKLEMİYOR + bayi işi → reddedilir (DB'ye dokunmadan).
	sandboxKontrol = func() bool { return false }
	h := &Handlers{} // nil DB: gate DB'den önce döner
	_, _, err := h.veritabanlariniAktar(nil, &Kaynak{}, Hesap{DBler: []string{"gecersiz ad!"}},
		"c_x", "", Kapsam{RID: 7}, &AktarSonuc{}, func(string, ...any) {})
	if !errors.Is(err, errBayiSandboxYok) {
		t.Fatalf("desteklenmeyen istemcide bayi işi sandbox hatası vermeli, alınan: %v", err)
	}

	// İstemci destekliyorsa gate geçilir; geçersiz DB adı atlanır → hata yok.
	sandboxKontrol = func() bool { return true }
	if _, _, err := h.veritabanlariniAktar(nil, &Kaynak{}, Hesap{DBler: []string{"gecersiz ad!"}},
		"c_x", "", Kapsam{RID: 7}, &AktarSonuc{}, func(string, ...any) {}); err != nil {
		t.Fatalf("destekli istemcide gate geçilmeli: %v", err)
	}
}
