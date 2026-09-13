package backups

import "testing"

// CWE-78 (Corgea, GERCEK): yedek "genel/admin" hedefinde UzakHost lftp betigine
// tirnaksiz girip `;`+`!` kabuk-kacisi (RCE) acabiliyordu. Fix: genelAyarDogrula
// host allowlist + lftpURL fail-closed. Bu testler o iki savunmayi kilitler.

func ornekGecerliGenel() *GenelAyar {
	return &GenelAyar{
		UzakAktif:     true,
		UzakTip:       "sftp",
		UzakHost:      "backup.example.com",
		UzakPort:      22,
		UzakKullanici: "yedek",
		UzakParola:    "p",
		UzakDizin:     "/yedek",
	}
}

func TestGenelAyarDogrulaHostAllowlist(t *testing.T) {
	if msg := genelAyarDogrula(ornekGecerliGenel()); msg != "" {
		t.Fatalf("gecerli ayar reddedildi: %s", msg)
	}
	// lftp betik enjeksiyonu tasiyan hostlar reddedilmeli
	kotu := []string{
		`h; !bash -c 'id'`,
		`h; !touch /tmp/x #`,
		`h ;!x`,
		`h"`,
		`h/x`,
		`h:22`,
		`h@evil`,
		`h evil`,
		`sftp://x`,
		``,
	}
	for _, h := range kotu {
		g := ornekGecerliGenel()
		g.UzakHost = h
		if genelAyarDogrula(g) == "" {
			t.Errorf("kotu host KABUL edildi (reddedilmeliydi): %q", h)
		}
	}
	// gecerli host/IP kabul
	for _, h := range []string{"backup.example.com", "1.2.3.4", "s1.yedek-host.net", "host123"} {
		g := ornekGecerliGenel()
		g.UzakHost = h
		if msg := genelAyarDogrula(g); msg != "" {
			t.Errorf("gecerli host reddedildi: %q -> %s", h, msg)
		}
	}
	// kullanici '-' onek (ssh opsiyon-enjeksiyonu) reddedilmeli
	g := ornekGecerliGenel()
	g.UzakKullanici = "-oProxyCommand=evil"
	if genelAyarDogrula(g) == "" {
		t.Errorf("'-' onekli kullanici KABUL edildi (reddedilmeliydi)")
	}
}

func TestLftpURLFailClosed(t *testing.T) {
	// gecerli host -> normal url
	if u := lftpURL(&Destination{Tip: "sftp", Host: "backup.example.com", Port: 22}); u != "sftp://backup.example.com:22" {
		t.Errorf("gecerli sftp url yanlis: %q", u)
	}
	if u := lftpURL(&Destination{Tip: "ftp", Host: "1.2.3.4", Port: 21}); u != "ftp://1.2.3.4:21" {
		t.Errorf("gecerli ftp url yanlis: %q", u)
	}
	// kotu host -> BOS (fail-closed): cagiranlar bunu guvenlik hatasi sayar
	for _, h := range []string{`h; !bash`, `h ;!x`, `h"`, `h/x`, `h@e`, ``} {
		if u := lftpURL(&Destination{Tip: "sftp", Host: h, Port: 22}); u != "" {
			t.Errorf("kotu host BOS donmedi (enjeksiyon riski): %q -> %q", h, u)
		}
	}
}
