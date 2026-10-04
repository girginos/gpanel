package system

import "testing"

func TestOsAdiAyikla(t *testing.T) {
	cases := []struct{ ad, in, bek string }{
		{"alma", "NAME=\"AlmaLinux\"\nVERSION=\"10.1 (Heliotrope Lion)\"\nPRETTY_NAME=\"AlmaLinux 10.1 (Heliotrope Lion)\"\nID=almalinux\n", "AlmaLinux 10.1 (Heliotrope Lion)"},
		{"tirnaksiz", "PRETTY_NAME=Arch Linux\n", "Arch Linux"},
		{"tek tirnak", "PRETTY_NAME='Debian GNU/Linux 12'\n", "Debian GNU/Linux 12"},
		{"crlf", "PRETTY_NAME=\"Rocky Linux 9.4\"\r\nID=rocky\r\n", "Rocky Linux 9.4"},
		{"yorum+bosluk", "# PRETTY_NAME=\"Yanlis\"\n  PRETTY_NAME = \"Ubuntu 24.04 LTS\"  \n", "Ubuntu 24.04 LTS"},
		{"ilk PRETTY kazanir", "PRETTY_NAME=\"A\"\nPRETTY_NAME=\"B\"\n", "A"},
		{"bos deger atlanir", "PRETTY_NAME=\"\"\nNAME=\"Foo\"\nVERSION_ID=\"1\"\n", "Foo 1"},
		{"yalniz NAME", "NAME=\"Foo\"\n", "Foo"},
		{"bos dosya", "", ""},
		{"esitlik yok", "garip satir\n", ""},
		{"tirnak dengesiz", "PRETTY_NAME=\"Foo\n", "\"Foo"},
	}
	for _, c := range cases {
		if g := osAdiAyikla(c.in); g != c.bek {
			t.Errorf("%s: %q, beklenen %q", c.ad, g, c.bek)
		}
	}
}

func TestMimariAdi(t *testing.T) {
	for in, bek := range map[string]string{
		"amd64": "x86_64", "x86_64": "x86_64", "X86_64\n": "x86_64", "arm64": "aarch64", "aarch64": "aarch64",
		"386": "i686", "i686": "i686", "riscv64": "riscv64", "": "",
	} {
		if g := mimariAdi(in); g != bek {
			t.Errorf("%q → %q, beklenen %q", in, g, bek)
		}
	}
}
