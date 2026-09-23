package httpx

import (
	"net/http/httptest"
	"testing"
)

// NginxtenMi yetki kararı verir (panel kapısı): kanıtsız isteğe GÜVENMEMELİ.

func TestNginxtenMiDisAdresGuvenilmez(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.5:4444"
	r.Header.Set("X-Gosp-Proxy", ProxySecret()) // doğru gizli bile olsa
	if NginxtenMi(r) {
		t.Fatal("dış adresten gelen istek nginx sayılmamalı")
	}
}

func TestNginxtenMiKiraciSureciGuvenilmez(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "127.0.0.1:5555" // aynı kutudaki kiracı da loopback'ten gelir
	r.Header.Set("X-Gosp-Proxy", "kesinlikle-yanlis-gizli")
	if NginxtenMi(r) {
		t.Fatal("yanlış gizliyle gelen loopback isteği nginx sayılmamalı")
	}
	r.Header.Del("X-Gosp-Proxy")
	if NginxtenMi(r) {
		t.Fatal("gizlisiz loopback isteği nginx sayılmamalı (ClientIP'in gevşek yedeği burada YOK)")
	}
}

func TestNginxtenMiDogruGizli(t *testing.T) {
	sec := ProxySecret()
	if sec == "" {
		t.Skip("bu makinede proxy gizlisi kurulu değil")
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "127.0.0.1:5555"
	r.Header.Set("X-Gosp-Proxy", sec)
	if !NginxtenMi(r) {
		t.Fatal("doğru gizli + loopback nginx sayılmalı")
	}
}
