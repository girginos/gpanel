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
	"io"
	"strings"
	"testing"
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
	if !icerir("--defaults-extra-file=/var/tmp/x.cnf") {
		t.Errorf("--defaults-extra-file eksik: %v", a)
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
	if err == nil || !strings.Contains(err.Error(), "sandbox") {
		t.Fatalf("desteklenmeyen istemcide bayi işi sandbox hatası vermeli, alınan: %v", err)
	}

	// İstemci destekliyorsa gate geçilir; geçersiz DB adı atlanır → hata yok.
	sandboxKontrol = func() bool { return true }
	if _, _, err := h.veritabanlariniAktar(nil, &Kaynak{}, Hesap{DBler: []string{"gecersiz ad!"}},
		"c_x", "", Kapsam{RID: 7}, &AktarSonuc{}, func(string, ...any) {}); err != nil {
		t.Fatalf("destekli istemcide gate geçilmeli: %v", err)
	}
}
