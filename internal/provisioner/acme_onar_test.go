package provisioner

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func onarKayit(t *testing.T, home, ad string, ecc bool, webroot string) string {
	t.Helper()
	d := ad
	if ecc {
		d = ad + "_ecc"
	}
	dir := filepath.Join(home, d)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, ad+".conf")
	ic := "Le_Domain='" + ad + "'\nLe_Alt='no'\n" + webroot + "\nLe_NextRenewTime='1'\n"
	if err := os.WriteFile(f, []byte(ic), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

func onarOrtam(t *testing.T) string {
	home := t.TempDir()
	o, g := acmeOnarHomes, acmeOnarGunluk
	t.Cleanup(func() { acmeOnarHomes, acmeOnarGunluk = o, g })
	acmeOnarHomes = func() []string { return []string{home} }
	acmeOnarGunluk = func(string) {}
	return home
}

func onarOku(t *testing.T, f string) string {
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAcmeKayitOnar_MusteriDocrootuDuzelir_DigerleriDokunulmaz(t *testing.T) {
	home := onarOrtam(t)
	a := onarKayit(t, home, "test1.girginos.app", true, "Le_Webroot='/home/c_x/subdomains/test1.girginos.app'")
	b := onarKayit(t, home, "test2.girginos.app", true, "Le_Webroot='/home/c_y/public_html'")
	c := onarKayit(t, home, "eski.com", false, "Le_Webroot='/home/c_z/public_html'") // ECDSA olmayan dizin
	dogru := onarKayit(t, home, "ok.com", true, "Le_Webroot='/var/www/acme'")
	dns := onarKayit(t, home, "dns.com", true, "Le_Webroot='dns_cf'")
	no := onarKayit(t, home, "no.com", true, "Le_Webroot='no'")
	nginx := onarKayit(t, home, "ng.com", true, "Le_Webroot='nginx'")
	panel := onarKayit(t, home, "lab.com", true, "Le_Webroot='/var/www/gpanel-acme-challenge'")
	dokunulmaz := map[string]string{}
	for _, f := range []string{dogru, dns, no, nginx, panel} {
		dokunulmaz[f] = onarOku(t, f)
	}
	s := AcmeKayitOnar()
	if len(s.Duzeltilen) != 3 || len(s.Atlanan) != 0 {
		t.Fatalf("3 düzeltme/0 atlama beklenirdi: %+v", s)
	}
	for _, f := range []string{a, b, c} {
		ic := onarOku(t, f)
		if !strings.Contains(ic, "\nLe_Webroot='/var/www/acme'\n") || strings.Contains(ic, "/home/") {
			t.Fatalf("%s düzelmedi:\n%s", f, ic)
		}
		if !strings.Contains(ic, "Le_NextRenewTime='1'") || !strings.HasPrefix(ic, "Le_Domain=") {
			t.Fatalf("diğer satırlar bozuldu:\n%s", ic)
		}
		st, _ := os.Stat(f)
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("izin korunmadı: %v", st.Mode())
		}
		if _, err := os.Stat(f + ".onar.tmp"); err == nil {
			t.Fatal("temp dosya kaldı")
		}
	}
	for f, once := range dokunulmaz {
		if onarOku(t, f) != once {
			t.Fatalf("dokunulmamalıydı: %s", f)
		}
		if _, err := os.Stat(f + ".webroot-yedek"); err == nil {
			t.Fatalf("yedek oluşmamalıydı: %s", f)
		}
	}
}

func TestAcmeKayitOnar_YedekOlusurEzilmez_Idempotent(t *testing.T) {
	home := onarOrtam(t)
	f := onarKayit(t, home, "a.com", true, "Le_Webroot='/home/c_a/public_html'")
	orijinal := onarOku(t, f)
	if s := AcmeKayitOnar(); len(s.Duzeltilen) != 1 {
		t.Fatalf("%+v", s)
	}
	y := f + ".webroot-yedek"
	if onarOku(t, y) != orijinal {
		t.Fatal("yedek orijinali içermeli")
	}
	if st, _ := os.Stat(y); st.Mode().Perm() != 0o600 {
		t.Fatalf("yedek izni 0600 olmalı: %v", st.Mode())
	}
	duzgun := onarOku(t, f)
	if s := AcmeKayitOnar(); len(s.Duzeltilen) != 0 || len(s.Atlanan) != 0 {
		t.Fatalf("ikinci koşu no-op olmalı: %+v", s)
	}
	if onarOku(t, f) != duzgun || onarOku(t, y) != orijinal {
		t.Fatal("ikinci koşu içerik değiştirdi")
	}
	// kayıt yeniden eski değere dönerse (eski yedek VAR) yedek EZİLMEZ
	if err := os.WriteFile(f, []byte("Le_Webroot='/home/c_b/public_html'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	AcmeKayitOnar()
	if onarOku(t, y) != orijinal {
		t.Fatal("var olan yedek ezildi")
	}
	if onarOku(t, f) != "Le_Webroot='/var/www/acme'\n" {
		t.Fatalf("yeniden düzelmedi: %q", onarOku(t, f))
	}
}

func TestAcmeKayitOnar_BozukSatirlarDokunulmazRaporlanir(t *testing.T) {
	home := onarOrtam(t)
	alintisiz := onarKayit(t, home, "q.com", true, "Le_Webroot=/home/c_q/public_html")
	bosluk := onarKayit(t, home, "s.com", true, "Le_Webroot='/home/c q/public_html'")
	cift := onarKayit(t, home, "c.com", true, "Le_Webroot='/home/c_c/x'\nLe_Webroot='/home/c_c/y'")
	enj := onarKayit(t, home, "e.com", true, "Le_Webroot='/home/x'; rm -rf /'")
	iyi := onarKayit(t, home, "iyi.com", true, "Le_Webroot='/home/c_i/public_html'")
	once := map[string]string{}
	for _, f := range []string{alintisiz, bosluk, cift, enj} {
		once[f] = onarOku(t, f)
	}
	s := AcmeKayitOnar()
	if len(s.Duzeltilen) != 1 || s.Duzeltilen[0] != iyi {
		t.Fatalf("yalnız sağlam kayıt düzelmeliydi (hata yalıtımı): %+v", s)
	}
	if len(s.Atlanan) != 4 {
		t.Fatalf("4 raporlanan beklenirdi: %+v", s.Atlanan)
	}
	for f, ic := range once {
		if onarOku(t, f) != ic {
			t.Fatalf("bozuk kayda dokunuldu: %s", f)
		}
	}
}

func TestAcmeKayitOnar_SembolikBagReddedilir(t *testing.T) {
	home := onarOrtam(t)
	hedef := filepath.Join(t.TempDir(), "disarida.conf")
	if err := os.WriteFile(hedef, []byte("Le_Webroot='/home/c/public_html'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "l.com_ecc")
	_ = os.MkdirAll(dir, 0o700)
	if err := os.Symlink(hedef, filepath.Join(dir, "l.com.conf")); err != nil {
		t.Fatal(err)
	}
	// sembolik dizin
	gercek := t.TempDir()
	_ = os.WriteFile(filepath.Join(gercek, "m.com.conf"), []byte("Le_Webroot='/home/c/public_html'\n"), 0o600)
	if err := os.Symlink(gercek, filepath.Join(home, "m.com_ecc")); err != nil {
		t.Fatal(err)
	}
	s := AcmeKayitOnar()
	if len(s.Duzeltilen) != 0 || len(s.Atlanan) != 2 {
		t.Fatalf("2 reddedilen beklenirdi: %+v", s)
	}
	if !strings.Contains(onarOku(t, hedef), "/home/c/public_html") || !strings.Contains(onarOku(t, filepath.Join(gercek, "m.com.conf")), "/home/") {
		t.Fatal("sembolik hedef değiştirilmemeli")
	}
}

func TestAcmeKayitOnar_KayitDisiDosyalarVeHesapConfDokunulmaz(t *testing.T) {
	home := onarOrtam(t)
	hesap := filepath.Join(home, "account.conf")
	_ = os.WriteFile(hesap, []byte("Le_Webroot='/home/x/y'\n"), 0o600)
	dir := filepath.Join(home, "z.com_ecc")
	_ = os.MkdirAll(dir, 0o700)
	csr := filepath.Join(dir, "z.com.csr.conf")
	diger := filepath.Join(dir, "baska.conf")
	_ = os.WriteFile(csr, []byte("Le_Webroot='/home/x/y'\n"), 0o600)
	_ = os.WriteFile(diger, []byte("Le_Webroot='/home/x/y'\n"), 0o600)
	if s := AcmeKayitOnar(); len(s.Duzeltilen)+len(s.Atlanan) != 0 {
		t.Fatalf("%+v", s)
	}
}

func TestAcmeKayitOnar_HomeYoksaSessiz(t *testing.T) {
	o, g := acmeOnarHomes, acmeOnarGunluk
	defer func() { acmeOnarHomes, acmeOnarGunluk = o, g }()
	acmeOnarHomes = func() []string { return []string{"/yok/boyle/dizin"} }
	acmeOnarGunluk = func(string) {}
	if s := AcmeKayitOnar(); len(s.Duzeltilen)+len(s.Atlanan) != 0 {
		t.Fatalf("%+v", s)
	}
}

// Sarmalayıcının `onar` alt komutu: aynı kurallar + hata yalıtımı.
func TestAcmeSarmalayici_Onar(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash yok")
	}
	home := t.TempDir()
	betik := filepath.Join(t.TempDir(), "yenile")
	if err := os.WriteFile(betik, acmeYenileBetik, 0o755); err != nil {
		t.Fatal(err)
	}
	a := onarKayit(t, home, "a.com", true, "Le_Webroot='/home/c_a/public_html'")
	bozuk := onarKayit(t, home, "b.com", true, "Le_Webroot=/home/c_b/public_html")
	cift := onarKayit(t, home, "c.com", true, "Le_Webroot='/home/c/x'\nLe_Webroot='/home/c/y'")
	dns := onarKayit(t, home, "d.com", true, "Le_Webroot='dns_cf'")
	panel := onarKayit(t, home, "p.com", true, "Le_Webroot='/var/www/gpanel-acme-challenge'")
	e := onarKayit(t, home, "e.com", false, "Le_Webroot='/home/c_e/public_html'")
	hedef := filepath.Join(t.TempDir(), "x.conf")
	_ = os.WriteFile(hedef, []byte("Le_Webroot='/home/c/public_html'\n"), 0o600)
	_ = os.MkdirAll(filepath.Join(home, "l.com_ecc"), 0o700)
	_ = os.Symlink(hedef, filepath.Join(home, "l.com_ecc", "l.com.conf"))
	once := map[string]string{}
	for _, f := range []string{bozuk, cift, dns, panel, hedef} {
		once[f] = onarOku(t, f)
	}
	calis := func() {
		c := exec.Command("bash", betik, "onar")
		c.Env = append(os.Environ(), "GOSP_ACME_CONFIG_HOMES="+home)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("onar çıkış hatası: %v\n%s", err, out)
		}
	}
	calis()
	for _, f := range []string{a, e} {
		if onarOku(t, f) != strings.Replace(onarOku(t, f+".webroot-yedek"), strings.Split(strings.Split(onarOku(t, f+".webroot-yedek"), "Le_Webroot='")[1], "'")[0], "/var/www/acme", 1) {
			t.Fatalf("düzelme/yedek tutarsız: %s\n%s", f, onarOku(t, f))
		}
		if !strings.Contains(onarOku(t, f), "Le_Webroot='/var/www/acme'") {
			t.Fatalf("%s düzelmedi", f)
		}
		if st, _ := os.Stat(f + ".webroot-yedek"); st.Mode().Perm() != 0o600 {
			t.Fatalf("yedek izni: %v", st.Mode())
		}
	}
	for f, ic := range once {
		if onarOku(t, f) != ic {
			t.Fatalf("dokunulmamalıydı: %s", f)
		}
	}
	// idempotent
	s1 := onarOku(t, a)
	calis()
	if onarOku(t, a) != s1 {
		t.Fatal("ikinci koşu değiştirdi")
	}
}
