package main

import "testing"

func TestKapiAdresURL(t *testing.T) {
	cases := []struct {
		host string
		port int
		bek  string
	}{
		{"panel.ornek.com:8443", 21001, "https://panel.ornek.com:21001"},
		{"1.2.3.4:8443", 21001, "https://1.2.3.4:21001"},
		{"[2001:db8::1]:8443", 21001, "https://[2001:db8::1]:21001"},
		{"panel.ornek.com", 21001, "https://panel.ornek.com:21001"},
	}
	for _, c := range cases {
		if got := kapiAdresURL(c.host, c.port); got != c.bek {
			t.Errorf("kapiAdresURL(%q,%d)=%q, beklenen %q", c.host, c.port, got, c.bek)
		}
	}
}

// Kullanım hataları dosyaya DOKUNMADAN 2 döner (kurulum betiği yanlış çağırırsa
// kapi.json'u bozmasın).
func TestKapiCLIKullanimHatalari(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"bilinmeyen"},
		{"gecis", "abc"},
		{"gecis", "0"},
		{"uzat"},
		{"uzat", "0"},
		{"port"},
		{"rol-port"},
		{"rol-port", "root"},
		{"rol-port", "admin", "abc"},
		{"rol-port", "musteri", "0"},
	} {
		if kod := kapiCLI(args); kod != 2 {
			t.Errorf("kapiCLI(%v)=%d, beklenen 2 (kullanım)", args, kod)
		}
	}
}
