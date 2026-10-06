// Package kiracitest — kiracı kimliğinde çocuk süreç başlatan yolların panel
// sırlarını çocuğa GEÇİRMEDİĞİNİ kilitleyen testlerin ortak düzeneği.
//
// Kur, PATH'in başına sahte runuser/sudo koyar ve süreç ortamına SAHTE panel
// sırları (SahteSirlar) yerleştirir. Sahte komut kimlik düşürmez, kiracı
// komutunu ÇALIŞTIRMAZ; yalnız argv'sini ve ALDIĞI ortamı kayıt dizinine yazar
// ve 0 ile çıkar. Böylece bir çağrı yolunun çocuğa ne verdiği root gerekmeden,
// gerçek kiracı kullanıcısı olmadan incelenir. Gerçek runuser ile kimlik
// düşürme + ortam: internal/kiraci testleri (root, 181).
//
// Yalnız testler içe aktarır; üretim ikilisine girmez.
package kiracitest

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// SahteSirlar — testte panel sürecine konan sahte sırlar (üretim değerleri
// DEĞİL). Adlar gerçek panel sır değişkenleridir: sızıntı adla da değerle de
// aranır.
var SahteSirlar = map[string]string{
	"PANEL_JWT_SECRET":       "sahte-jwt-sirri-7c1e0b3a9d2f4e6a8b0c1d2e3f4a5b6c",
	"PANEL_DB_DSN":           "panel:sahte-db-parolasi-51f0@tcp(127.0.0.1:3306)/panel",
	"PANEL_REDIS_ADMIN_PASS": "sahte-redis-admin-parolasi-9e4d",
}

// Kayit — sahte komutların kayıt dizini.
type Kayit struct{ dir string }

// Cagri — sahte komutun bir çağrılışı.
type Cagri struct {
	Argv []string
	Env  []string
}

// Kur: adlar (boşsa runuser ve sudo) için sahte komutları PATH'in başına koyar
// ve sahte panel sırlarını süreç ortamına yerleştirir (t.Setenv: test bitince
// geri alınır; paralel testte kullanılamaz).
func Kur(t testing.TB, adlar ...string) Kayit {
	t.Helper()
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("/bin/sh yok")
	}
	if len(adlar) == 0 {
		adlar = []string{"runuser", "sudo", "systemd-run"}
	}
	tmp := t.TempDir()
	bin, kayit := filepath.Join(tmp, "bin"), filepath.Join(tmp, "kayit")
	for _, d := range []string{bin, kayit} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, ad := range adlar {
		// Her çağrı kendi alt dizinine: argv (satır başına bir argüman) + env.
		// Mutlak yollar: çocuğun PATH'i temizlenmiş olabilir.
		betik := "#!/bin/sh\n" +
			"d=$(/usr/bin/mktemp -d '" + filepath.Join(kayit, ad) + ".XXXXXX') || exit 97\n" +
			"printf '%s\\n' \"$@\" > \"$d/argv\"\n" +
			"/usr/bin/env > \"$d/env\"\n" +
			"/bin/cat > /dev/null\n" +
			"exit 0\n"
		if ad == "systemd-run" {
			// Sahte systemd-run (--scope kipi): argv/ortamı kaydeder, "--"dan sonraki
			// komutu AYNI süreçte çalıştırır (gerçek --scope gibi; çıkış kodu, stdout/
			// stderr, stdin aynen geçer). Komut bu dizindeki sahte bir araçsa (runuser)
			// onu mutlak yolla çağırır: çocuğun PATH'i temizlenmiştir.
			betik = "#!/bin/sh\n" +
				"d=$(/usr/bin/mktemp -d '" + filepath.Join(kayit, ad) + ".XXXXXX') || exit 97\n" +
				"printf '%s\\n' \"$@\" > \"$d/argv\"\n" +
				"/usr/bin/env > \"$d/env\"\n" +
				"while [ $# -gt 0 ]; do a=$1; shift; [ \"$a\" = \"--\" ] && break; done\n" +
				"[ $# -gt 0 ] || exit 96\n" +
				"if [ -x '" + bin + "'/\"$1\" ]; then c='" + bin + "'/$1; shift; exec \"$c\" \"$@\"; fi\n" +
				"exec \"$@\"\n"
		}
		if err := os.WriteFile(filepath.Join(bin, ad), []byte(betik), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for k, v := range SahteSirlar {
		t.Setenv(k, v)
	}
	return Kayit{dir: kayit}
}

// Cagrilar: ad komutunun kaydedilen çağrıları (sırası belirsiz).
func (k Kayit) Cagrilar(t testing.TB, ad string) []Cagri {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join(k.dir, ad+".*"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(dirs)
	var out []Cagri
	for _, d := range dirs {
		argv, err := os.ReadFile(filepath.Join(d, "argv"))
		if err != nil {
			t.Fatal(err)
		}
		env, err := os.ReadFile(filepath.Join(d, "env"))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, Cagri{
			Argv: strings.Split(strings.TrimSuffix(string(argv), "\n"), "\n"),
			Env:  strings.Split(strings.TrimSuffix(string(env), "\n"), "\n"),
		})
	}
	return out
}

// Sizinti: env satırlarından panel sırrı taşıyanlar (adı ya da değeri).
func Sizinti(env []string) []string {
	var out []string
	for _, satir := range env {
		ad, deger, _ := strings.Cut(satir, "=")
		for k, v := range SahteSirlar {
			if ad == k || strings.Contains(deger, v) {
				out = append(out, k)
			}
		}
	}
	return out
}

// SizintiYok: ad en az bir kez çağrıldı ve HİÇBİR çağrısının ortamında panel
// sırrı yok. Her çağrının ilk argümanları beklenenOnEk ile başlamalı (ör. -u
// <kullanıcı> --): kimlik düşürme atlanmamış olmalı.
func (k Kayit) SizintiYok(t testing.TB, ad string, beklenenOnEk ...string) []Cagri {
	t.Helper()
	cs := k.Cagrilar(t, ad)
	if len(cs) == 0 {
		t.Fatalf("%s hiç çağrılmadı (kiracı komutu kimlik düşürmeden mi çalıştı?)", ad)
	}
	for i, c := range cs {
		if s := Sizinti(c.Env); len(s) > 0 {
			t.Errorf("%s çağrısı #%d (argv %q): panel sırları çocuğa GEÇTİ: %v", ad, i, c.Argv, s)
		}
		if len(c.Argv) < len(beklenenOnEk) {
			t.Errorf("%s çağrısı #%d argv kısa: %q", ad, i, c.Argv)
			continue
		}
		for j, a := range beklenenOnEk {
			if c.Argv[j] != a {
				t.Errorf("%s çağrısı #%d argv %q, %q ile başlamalı", ad, i, c.Argv, beklenenOnEk)
				break
			}
		}
	}
	return cs
}

// KomutYaz: sahte komutun (ör. runuser) betiğini ÜZERİNE yazar — çıkış kodu/çıktı
// sözleşmesini sınayan testler için (kayıt tutmaz). Betik sh betiğidir.
func (k Kayit) KomutYaz(t testing.TB, ad, betik string) {
	t.Helper()
	yol := filepath.Join(filepath.Dir(k.dir), "bin", ad)
	if err := os.WriteFile(yol, []byte("#!/bin/sh\n"+betik+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}
