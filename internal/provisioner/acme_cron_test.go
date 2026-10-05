package provisioner

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const eskiPanelSatir = "26 3,9,15,21 * * * /root/.acme.sh/acme.sh --cron --home /root/.acme.sh --config-home /opt/girginospanel/acme > /dev/null"

func acmeTestOrtam(t *testing.T, crontab string) (cronDosya, betik string, cron *string) {
	t.Helper()
	dir := t.TempDir()
	oB, oC, oO, oY, oE := acmeBetikYolu, acmeCronDosya, acmeCrontabOku, acmeCrontabYaz, acmeEtiketle
	t.Cleanup(func() {
		acmeBetikYolu, acmeCronDosya, acmeCrontabOku, acmeCrontabYaz, acmeEtiketle = oB, oC, oO, oY, oE
	})
	acmeBetikYolu = filepath.Join(dir, "bin", "girginospanel-acme-yenile")
	acmeCronDosya = filepath.Join(dir, "girginospanel-acme")
	acmeEtiketle = func(string) {}
	c := crontab
	acmeCrontabOku = func() (string, error) { return c, nil }
	acmeCrontabYaz = func(s string) error { c = s; return nil }
	return acmeCronDosya, acmeBetikYolu, &c
}

func TestAcmeCronIcerik_SarmalayiciyiCagirir(t *testing.T) {
	_, betik, _ := acmeTestOrtam(t, "")
	ic := acmeCronIcerik()
	if !strings.Contains(ic, " root "+betik+" ") {
		t.Fatalf("cron satırı sarmalayıcıyı root olarak çağırmalı:\n%s", ic)
	}
	if strings.Contains(ic, "--config-home") {
		t.Fatalf("cron satırı tek config-home'a bağlanmamalı (sarmalayıcı hepsini gezer):\n%s", ic)
	}
}

func TestAcmeCrontabTemizle_YalnizPanelSatiri(t *testing.T) {
	kullanici := "0 2 * * * /usr/local/bin/benim-isim\n# yorum: acme.sh --cron burada anılıyor\n17 5 * * * /root/.acme.sh/acme.sh --cron --home /root/.acme.sh --config-home /srv/musteri/acme\n"
	in := kullanici + eskiPanelSatir + "\n" +
		"5 1 * * * /root/.acme.sh/acme.sh --cron --home /root/.acme.sh > /dev/null\n" +
		"9 1 * * * /root/.acme.sh/acme.sh --cron --home /root/.acme.sh --config-home /var/lib/girginospanel/acme/ > /dev/null\n"
	cikti, n := acmeCrontabTemizle(in)
	if n != 3 {
		t.Fatalf("3 panel/acme.sh satırı silinmeliydi, silinen=%d\n%s", n, cikti)
	}
	if cikti != kullanici {
		t.Fatalf("kullanıcı satırları AYNEN kalmalı.\nbeklenen:\n%q\ngelen:\n%q", kullanici, cikti)
	}
	if c, n := acmeCrontabTemizle("0 2 * * * x\n"); n != 0 || c != "0 2 * * * x\n" {
		t.Fatalf("panel satırı yoksa değişiklik olmamalı: %q %d", c, n)
	}
}

func TestAcmeCronKur_EskiSatiriDegistirir_Idempotent(t *testing.T) {
	kullanici := "0 2 * * * /usr/local/bin/benim-isim\n"
	cronDosya, betik, cron := acmeTestOrtam(t, kullanici+eskiPanelSatir+"\n")
	yazimlar := 0
	acmeCrontabYaz = func(s string) error { yazimlar++; *cron = s; return nil }

	if err := acmeCronKurTest(); err != nil {
		t.Fatal(err)
	}
	if *cron != kullanici {
		t.Fatalf("eski satır kalkmalı, kullanıcı satırı kalmalı: %q", *cron)
	}
	b, err := os.ReadFile(cronDosya)
	if err != nil || string(b) != acmeCronIcerik() {
		t.Fatalf("cron.d içeriği yanlış: %v %q", err, b)
	}
	if st, _ := os.Stat(cronDosya); st.Mode().Perm() != 0o644 {
		t.Fatalf("cron.d 0644 olmalı: %v", st.Mode())
	}
	if st, _ := os.Stat(betik); st.Mode().Perm() != 0o755 {
		t.Fatalf("sarmalayıcı 0755 olmalı: %v", st.Mode())
	}
	if bb, _ := os.ReadFile(betik); string(bb) != string(acmeYenileBetik) {
		t.Fatal("sarmalayıcı gömülü kaynakla aynı olmalı")
	}
	mt1 := func(y string) time.Time { st, _ := os.Stat(y); return st.ModTime() }
	t1, t2 := mt1(cronDosya), mt1(betik)
	time.Sleep(20 * time.Millisecond)
	if err := acmeCronKurTest(); err != nil {
		t.Fatal(err)
	}
	if yazimlar != 1 {
		t.Fatalf("ikinci çalıştırma crontab'a yazmamalı (idempotent), yazım=%d", yazimlar)
	}
	if !mt1(cronDosya).Equal(t1) || !mt1(betik).Equal(t2) {
		t.Fatal("ikinci çalıştırma dosyaları yeniden yazmamalı")
	}
}

func TestAcmeCronKur_BozulmusDosyayiOnarir(t *testing.T) {
	cronDosya, betik, _ := acmeTestOrtam(t, "")
	if err := acmeCronKurTest(); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(cronDosya, []byte("bozuk\n"), 0o644)
	_ = os.WriteFile(betik, []byte("#!/bin/sh\nexit 0\n"), 0o755)
	if err := acmeCronKurTest(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(cronDosya); string(b) != acmeCronIcerik() {
		t.Fatal("cron.d onarılmalı")
	}
	if b, _ := os.ReadFile(betik); string(b) != string(acmeYenileBetik) {
		t.Fatal("sarmalayıcı onarılmalı")
	}
}

// acmeCronKurTest: test ortamında acme.sh kontrolü atlanır (acmeBetikYolu özel).
func acmeCronKurTest() error { return AcmeCronKur() }

// ---- sarmalayıcı betik ----

func bashYok(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash yok")
	}
}

func betikCalistir(t *testing.T, env []string, arg ...string) (string, error) {
	t.Helper()
	bashYok(t)
	yol := filepath.Join(t.TempDir(), "yenile")
	if err := os.WriteFile(yol, acmeYenileBetik, 0o755); err != nil {
		t.Fatal(err)
	}
	c := exec.Command("bash", append([]string{yol}, arg...)...)
	c.Env = append(os.Environ(), env...)
	out, err := c.CombinedOutput()
	return string(out), err
}

func kayitYaz(t *testing.T, home, ad string, renew int64) {
	t.Helper()
	d := filepath.Join(home, ad)
	_ = os.MkdirAll(d, 0o755)
	ic := fmt.Sprintf("Le_Domain='%s'\nLe_NextRenewTime='%d'\n", ad, renew)
	if err := os.WriteFile(filepath.Join(d, ad+".conf"), []byte(ic), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(d, ad+".csr.conf"), []byte("[ req ]\n"), 0o600)
}

func TestAcmeYenile_HataYalitimi_HerConfigHomeCalisir(t *testing.T) {
	bashYok(t)
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	bos := filepath.Join(dir, "bos")
	for _, h := range []string{a, b, bos} {
		_ = os.MkdirAll(h, 0o755)
	}
	kayitYaz(t, a, "bir.test", 1)
	kayitYaz(t, b, "iki.test", 1)
	cagri := filepath.Join(dir, "cagrilar.txt")
	sahte := filepath.Join(dir, "acme.sh")
	// a dizini için hata ile çık, b için başarılı
	_ = os.WriteFile(sahte, []byte("#!/bin/bash\necho \"$*\" >> "+cagri+"\ncase \"$*\" in *"+a+"*) echo patladi; exit 3;; esac\necho tamam\n"), 0o755)
	out, err := betikCalistir(t, []string{
		"GOSP_ACME_BIN=" + sahte, "GOSP_ACME_HOME=/h", "GOSP_ACME_CONFIG_HOMES=" + a + " " + b + " " + bos + " /yok/dizin",
		"GOSP_ACME_KILIT=" + filepath.Join(dir, "kilit"),
	})
	if err == nil {
		t.Fatalf("bir config-home hata verdiyse çıkış kodu sıfır olmamalı:\n%s", out)
	}
	c, _ := os.ReadFile(cagri)
	satirlar := strings.Split(strings.TrimSpace(string(c)), "\n")
	if len(satirlar) != 2 {
		t.Fatalf("yalnız kayıtlı iki config-home çağrılmalı (boş/yok atlanır), çağrı=%v", satirlar)
	}
	for i, h := range []string{a, b} {
		want := "--cron --home /h --config-home " + h
		if satirlar[i] != want {
			t.Fatalf("çağrı %d: %q beklenen %q", i, satirlar[i], want)
		}
	}
	if !strings.Contains(out, "HATA ["+a+"]") || !strings.Contains(out, "1 hatalı") {
		t.Fatalf("hata günlüğe/çıktıya yazılmalı:\n%s", out)
	}
}

func TestAcmeYenile_HepsiBasariliSifirCikis(t *testing.T) {
	bashYok(t)
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	kayitYaz(t, a, "bir.test", 1)
	sahte := filepath.Join(dir, "acme.sh")
	_ = os.WriteFile(sahte, []byte("#!/bin/bash\nexit 0\n"), 0o755)
	if out, err := betikCalistir(t, []string{"GOSP_ACME_BIN=" + sahte, "GOSP_ACME_CONFIG_HOMES=" + a, "GOSP_ACME_KILIT=" + filepath.Join(dir, "k")}); err != nil {
		t.Fatalf("başarılıysa sıfır çıkış: %v\n%s", err, out)
	}
}

func TestAcmeDurum_GecmisYaklasanTamam(t *testing.T) {
	bashYok(t)
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("openssl yok")
	}
	dir := t.TempDir()
	home := filepath.Join(dir, "h")
	simdi := time.Now().Unix()
	sert := func(ad string, gun int) string {
		d := filepath.Join(home, ad)
		_ = os.MkdirAll(d, 0o755)
		key, crt := filepath.Join(dir, ad+".key"), filepath.Join(d, "fullchain.cer")
		out, err := exec.Command("openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-keyout", key, "-out", crt,
			"-subj", "/CN="+ad, "-days", fmt.Sprint(gun)).CombinedOutput()
		if err != nil {
			t.Skipf("openssl sertifika üretemedi: %v %s", err, out)
		}
		return crt
	}
	// gecmis: yenileme zamanı 3 gün önce, bitiş uzak
	sert("gecmis.test", 60)
	kayitYaz(t, home, "gecmis.test", simdi-3*86400)
	// yaklasan: yenileme zamanı gelecekte ama bitişe 5 gün
	sert("yakin.test", 5)
	kayitYaz(t, home, "yakin.test", simdi+86400)
	// tamam
	sert("tamam.test", 60)
	kayitYaz(t, home, "tamam.test", simdi+30*86400)
	// ilk deneme başarısız (yenileme zamanı yok) → listede olmamalı
	_ = os.MkdirAll(filepath.Join(home, "hic.test"), 0o755)
	_ = os.WriteFile(filepath.Join(home, "hic.test", "hic.test.conf"), []byte("Le_Domain='hic.test'\n"), 0o600)
	// yenileme zamanı az önce geçti (cron aralığı içinde) → GECMIS sayılmamalı
	sert("taze.test", 60)
	kayitYaz(t, home, "taze.test", simdi-3600)

	out, err := betikCalistir(t, []string{"GOSP_ACME_CONFIG_HOMES=" + home}, "durum")
	if err != nil {
		t.Fatalf("durum salt-okuma, hata vermemeli: %v\n%s", err, out)
	}
	got := map[string]string{}
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		p := strings.Split(l, "|")
		if len(p) != 5 {
			t.Fatalf("beklenmeyen satır %q", l)
		}
		got[p[1]] = p[0]
	}
	want := map[string]string{"gecmis.test": "GECMIS", "yakin.test": "YAKLASAN", "tamam.test": "TAMAM", "taze.test": "TAMAM"}
	if len(got) != len(want) {
		t.Fatalf("kayıtlar %v beklenen %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %s beklenen %s", k, got[k], v)
		}
	}
}
