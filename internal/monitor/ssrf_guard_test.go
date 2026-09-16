package monitor

import (
	"context"
	"net"
	"strings"
	"testing"
)

func TestIpDahiliMi(t *testing.T) {
	dahili := []string{
		"127.0.0.1", "127.9.9.9", "::1", // loopback
		"169.254.169.254", "169.254.1.1", // link-local / bulut metadata
		"10.0.0.5", "172.16.0.1", "172.31.255.255", "192.168.1.1", // özel
		"100.64.0.1",         // CGNAT
		"0.0.0.0",            // unspecified
		"224.0.0.1",          // multicast
		"::ffff:10.0.0.1",    // v4-eşlemli özel
		"fc00::1", "fe80::1", // ULA / link-local v6
		"240.0.0.1", "255.255.255.255", // rezerve/broadcast
		"198.18.0.1", // benchmark
	}
	for _, s := range dahili {
		if ip := net.ParseIP(s); ip == nil || !ipDahiliMi(ip) {
			t.Errorf("dahili SAYILMALI ama sayilmadi: %s", s)
		}
	}
	public := []string{
		"8.8.8.8", "1.1.1.1", "148.251.169.181", // gercek public
		"2606:4700:4700::1111", // public v6
		"93.184.216.34",        // example.com
	}
	for _, s := range public {
		if ip := net.ParseIP(s); ip == nil || ipDahiliMi(ip) {
			t.Errorf("public SAYILMALI ama dahili sayildi: %s", s)
		}
	}
}

func TestGuvenliDialContext_IPLiteralDahiliRed(t *testing.T) {
	// IP-literal dahili hedef DNS'siz reddedilmeli (baglanti denemesi bile yok).
	for _, addr := range []string{"127.0.0.1:80", "169.254.169.254:80", "10.0.0.1:443", "[::1]:80"} {
		c, err := guvenliDialContext(context.Background(), "tcp", addr)
		if err == nil {
			c.Close()
			t.Errorf("dahili IP-literal reddedilmeliydi: %s", addr)
			continue
		}
		if !strings.Contains(err.Error(), "ssrf-guard") {
			t.Errorf("%s: ssrf-guard hatasi bekleniyordu, alindi: %v", addr, err)
		}
	}
}

func TestGuvenliDialContext_LocalhostIsimRed(t *testing.T) {
	// localhost -> 127.0.0.1 cozulur -> reddedilmeli (rebinding/hosts sinifi).
	if c, err := guvenliDialContext(context.Background(), "tcp", "localhost:80"); err == nil {
		c.Close()
		t.Error("localhost dahiliye cozulup reddedilmeliydi")
	}
}
