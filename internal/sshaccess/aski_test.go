package sshaccess

// aski_test.go — hesap askısı: SSH/SFTP girişi (hesap süresi) askıda kapalı,
// askıdan alınca askı öncesi değer AYNEN döner. getent/chage sahte (bellekteki
// shadow alanı), saklama dizini geçici — ana makinenin /etc/shadow'una dokunulmaz.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type ha_shadow struct {
	sure     map[string]string // sk → shadow 8. alan
	komutlar []string
}

func ha_dikis(t *testing.T, sure map[string]string) *ha_shadow {
	t.Helper()
	e1, e2 := girisKomut, girisDepo
	t.Cleanup(func() { girisKomut, girisDepo = e1, e2 })
	girisDepo = filepath.Join(t.TempDir(), "ssh-askida")
	s := &ha_shadow{sure: sure}
	girisKomut = func(ad string, arg ...string) ([]byte, error) {
		s.komutlar = append(s.komutlar, ad+" "+strings.Join(arg, " "))
		switch {
		case ad == "getent" && len(arg) == 2 && arg[0] == "shadow":
			v, ok := s.sure[arg[1]]
			if !ok {
				return nil, errors.New("exit status 2")
			}
			return []byte(arg[1] + ":$6$x:19000:0:99999:7::" + v + ":\n"), nil
		case ad == "chage" && len(arg) == 3 && arg[0] == "-E":
			if arg[1] == "-1" {
				s.sure[arg[2]] = ""
			} else {
				s.sure[arg[2]] = arg[1]
			}
			return nil, nil
		}
		return nil, errors.New("beklenmeyen komut")
	}
	return s
}

func TestHesapGirisKilidi_GidisDonus(t *testing.T) {
	s := ha_dikis(t, map[string]string{"c_hassh": "", "c_hasure": "21000"})
	for sk, once := range map[string]string{"c_hassh": "", "c_hasure": "21000"} {
		if err := HesapGirisKilidi(sk, true); err != nil {
			t.Fatalf("%s kilit: %v", sk, err)
		}
		if s.sure[sk] != askiGunu {
			t.Fatalf("%s: askıda hesap süresi dolmuş olmalı (sshd anahtar dahil reddeder): %q", sk, s.sure[sk])
		}
		// İdempotent: ikinci kilit saklanan askı-öncesi değeri EZMEZ.
		if err := HesapGirisKilidi(sk, true); err != nil {
			t.Fatal(err)
		}
		if err := HesapGirisKilidi(sk, false); err != nil {
			t.Fatalf("%s açma: %v", sk, err)
		}
		if s.sure[sk] != once {
			t.Fatalf("%s: askıdan alınca askı öncesi değer (%q) dönmeli: %q", sk, once, s.sure[sk])
		}
		if _, err := os.Stat(filepath.Join(girisDepo, sk)); err == nil {
			t.Fatalf("%s: saklanan değer temizlenmeli", sk)
		}
		// İkinci açma zararsız.
		if err := HesapGirisKilidi(sk, false); err != nil || s.sure[sk] != once {
			t.Fatalf("%s: ikinci açma zararsız olmalı: %v %q", sk, err, s.sure[sk])
		}
	}
	for _, k := range s.komutlar {
		if strings.Contains(k, "usermod") || strings.Contains(k, "gpasswd") {
			t.Fatalf("askı SSH ayarına (kabuk/grup) dokunmamalı: %s", k)
		}
	}
}

// Askıdayken yönetici süreyi elle değiştirdiyse askıdan alma ona dokunmaz.
func TestHesapGirisKilidi_ElleDegisenSureyeDokunmaz(t *testing.T) {
	s := ha_dikis(t, map[string]string{"c_hassh": ""})
	if err := HesapGirisKilidi("c_hassh", true); err != nil {
		t.Fatal(err)
	}
	s.sure["c_hassh"] = "30000"
	if err := HesapGirisKilidi("c_hassh", false); err != nil || s.sure["c_hassh"] != "30000" {
		t.Fatalf("elle konan süre korunmalı: %v %q", err, s.sure["c_hassh"])
	}
}

// Bozuk saklanan değer chage'e geçmez; hesap askıda kalır.
func TestHesapGirisKilidi_BozukSaklananDegerAskidaBirakir(t *testing.T) {
	s := ha_dikis(t, map[string]string{"c_hassh": ""})
	if err := HesapGirisKilidi("c_hassh", true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(girisDepo, "c_hassh"), []byte("1; rm -rf /"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := HesapGirisKilidi("c_hassh", false); err == nil || s.sure["c_hassh"] != askiGunu {
		t.Fatalf("bozuk değer reddedilip hesap askıda kalmalı: %v %q", err, s.sure["c_hassh"])
	}
}

func TestHesapGirisKilidi_SistemKullanicisiReddedilir(t *testing.T) {
	s := ha_dikis(t, map[string]string{"root": ""})
	if err := HesapGirisKilidi("root", true); err == nil || len(s.komutlar) != 0 {
		t.Fatalf("c_ dışı kullanıcı reddedilmeli, komut çalışmamalı: %v %v", err, s.komutlar)
	}
}
