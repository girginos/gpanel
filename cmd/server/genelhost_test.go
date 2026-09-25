package main

import (
	"net"
	"testing"
)

func ipNetler(t *testing.T, cidrler ...string) []net.Addr {
	t.Helper()
	var out []net.Addr
	for _, c := range cidrler {
		ip, n, err := net.ParseCIDR(c)
		if err != nil {
			t.Fatal(err)
		}
		n.IP = ip
		out = append(out, n)
	}
	return out
}

// KP-3: NAT arkasındaki sunucuda arayüzde yalnız özel/CGNAT IP varken genel adres
// UYDURULMAZ — dışarıdan erişilemeyen adres "kullanıcılara bildirin" diye gösterilirdi.
func TestGenelHostSecOzelIPGondermez(t *testing.T) {
	sablon := "server {\n    listen 8443 ssl default_server;\n    server_name _;\n}"
	cases := []struct {
		ad       string
		adresler []string
	}{
		{"RFC1918 10/8", []string{"127.0.0.1/8", "10.0.0.5/8"}},
		{"RFC1918 172.16/12 (docker)", []string{"172.17.0.1/16"}},
		{"RFC1918 192.168/16", []string{"192.168.1.20/24"}},
		{"CGNAT", []string{"100.64.3.4/10"}},
		{"link-local", []string{"169.254.1.1/16"}},
		{"belgeleme/ayrılmış", []string{"203.0.113.97/24", "198.18.0.1/15", "240.0.0.1/4"}},
		{"yalnız IPv6", []string{"2a01:4f8::1/64"}},
		{"arayüz yok", nil},
	}
	for _, c := range cases {
		if got := genelHostSec(sablon, "", ipNetler(t, c.adresler...)); got != "" {
			t.Errorf("%s: genel adres olmamalı, got %q", c.ad, got)
		}
	}
}

func TestGenelHostSecKaynakSirasi(t *testing.T) {
	nat := ipNetler(t, "127.0.0.1/8", "10.0.0.5/8")
	karma := ipNetler(t, "127.0.0.1/8", "172.17.0.1/16", "203.0.113.97/24", "148.251.169.181/29", "5.9.1.1/32")
	adli := "server {\n    listen 21001 ssl;\n    server_name Panel.Ornek.com 10.0.0.5 localhost 127.0.0.1;\n}"
	cases := []struct {
		ad, conf, env string
		adresler      []net.Addr
		beklenen      string
	}{
		// panelhost yakala-hepsi bloğu bilinmeyen Host'u 444'ler: hostname her zaman önce.
		{"hostname NAT'ta", adli, "", nat, "panel.ornek.com"},
		{"hostname env'den önce", adli, "198.51.99.7", karma, "panel.ornek.com"},
		{"env açık yapılandırma", "server_name _;", "198.51.99.7", nat, "198.51.99.7"},
		{"env boşluklu", "", " 198.51.99.7\n", nat, "198.51.99.7"},
		{"env loopback yok sayılır", "", "127.0.0.1", nat, ""},
		{"env çöp yok sayılır", "", "panel", karma, "148.251.169.181"},
		// İlk arayüz adresi DEĞİL, ilk GENEL adres (docker ve belgeleme bloğu atlanır).
		{"ilk genel arayüz", "server_name _;", "", karma, "148.251.169.181"},
		{"ip'li server_name hostname değildir", "server_name 10.0.0.5 localhost 127.0.0.1;", "", nat, ""},
		{"yorumlu server_name sayılmaz", "# server_name eski.ornek.com;\nserver_name _;", "", nat, ""},
		{"yalnız ilk server_name", "server_name _;\nserver_name baska.ornek.com;", "", nat, ""},
		{"joker/regex ad değildir", "server_name *.ornek.com ~^x\\.ornek\\.com$;", "", nat, ""},
	}
	for _, c := range cases {
		if got := genelHostSec(c.conf, c.env, c.adresler); got != c.beklenen {
			t.Errorf("%s: got %q, beklenen %q", c.ad, got, c.beklenen)
		}
	}
}
