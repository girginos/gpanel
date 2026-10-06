package main

import (
	"net/http/httptest"
	"testing"

	"girginospanel/internal/panelhost"
)

func TestSSOHostAdi(t *testing.T) {
	for in, want := range map[string]string{
		"panel.example.com":      "panel.example.com",
		"panel.example.com:8443": "panel.example.com",
		"203.0.113.5:9443":       "203.0.113.5",
		"[2001:db8::1]":          "2001:db8::1",
		"[2001:db8::1]:8443":     "2001:db8::1",
	} {
		if got := ssoHostAdi(in); got != want {
			t.Errorf("ssoHostAdi(%q)=%q want %q", in, got, want)
		}
	}
}

// Kapı zorlaması kapalı (test ortamında kapi.json yok): tek panel portu + isteğin host'u.
func TestSSOMusteriAdresi_TekPort(t *testing.T) {
	tek := "https://panel.example.com:" + panelhost.PanelPort() // ortamdaki _panel.conf portu
	r := httptest.NewRequest("POST", "/api/v1/domains/1/sso", nil)
	r.Host = "panel.example.com"
	adres, ok := ssoMusteriAdresi(r)
	if !ok || adres != tek {
		t.Fatalf("adres=%q ok=%v", adres, ok)
	}
	r.Host = "panel.example.com:9999"
	if adres, _ := ssoMusteriAdresi(r); adres != tek {
		t.Fatalf("istek portu URL'ye sızmamalı: %q", adres)
	}
	r.Host = ""
	if _, ok := ssoMusteriAdresi(r); ok {
		t.Fatal("host yoksa adres çözülemez")
	}
}
