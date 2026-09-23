package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebhookYeniURL(t *testing.T) {
	canli := map[int]bool{21001: true, 23003: true}
	cases := []struct {
		adres, bek string
		ok         bool
	}{
		{"https://1.2.3.4:8443/api/v1/git-webhook/abc", "https://1.2.3.4:23003/api/v1/git-webhook/abc", true},
		{"https://[2001:db8::1]:8443/api/v1/git-webhook/abc", "https://[2001:db8::1]:23003/api/v1/git-webhook/abc", true},
		{"https://1.2.3.4:23003/api/v1/git-webhook/abc", "", false},   // zaten genel
		{"https://1.2.3.4:21001/api/v1/git-webhook/abc", "", false},   // canlı port
		{"https://panel.ornek.com/api/v1/git-webhook/abc", "", false}, // portsuz (443)
		{"http://1.2.3.4:8443/x", "", false},                          // https değil
		{"::bozuk::", "", false},
	}
	for _, c := range cases {
		got, ok := webhookYeniURL(c.adres, 23003, canli)
		if ok != c.ok || got != c.bek {
			t.Errorf("webhookYeniURL(%q) = %q,%v; beklenen %q,%v", c.adres, got, ok, c.bek, c.ok)
		}
	}
}

// sahteGitHub: /repos/o/r/hooks/7/config — GET mevcut adresi döner, PATCH gövdeyi kaydeder.
func sahteGitHub(t *testing.T, mevcutURL string, getKod int) (*map[string]string, *int) {
	t.Helper()
	var patch map[string]string
	patchSayac := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/hooks/7/config" || r.Header.Get("Authorization") != "Bearer tok" {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case "GET":
			w.WriteHeader(getKod)
			_ = json.NewEncoder(w).Encode(map[string]string{"url": mevcutURL, "insecure_ssl": "1"})
		case "PATCH":
			patchSayac++
			_ = json.NewDecoder(r.Body).Decode(&patch)
			_ = json.NewEncoder(w).Encode(map[string]string{"url": patch["url"]})
		}
	}))
	t.Cleanup(srv.Close)
	eski := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = eski })
	return &patch, &patchSayac
}

func TestWebhookTasi(t *testing.T) {
	canli := map[int]bool{23003: true}
	patch, n := sahteGitHub(t, "https://1.2.3.4:8443/api/v1/git-webhook/gizli1", 200)
	son, degisti, err := webhookTasi(context.Background(), "tok", "o/r", 7, 23003, canli)
	if err != nil || !degisti || son != "https://1.2.3.4:23003/api/v1/git-webhook/gizli1" {
		t.Fatalf("son=%q degisti=%v err=%v", son, degisti, err)
	}
	if *n != 1 || (*patch)["url"] != son || (*patch)["secret"] != "gizli1" {
		t.Fatalf("PATCH yeni adres + imza gizlisiyle gitmeli: %v", *patch)
	}
	if _, ok := (*patch)["insecure_ssl"]; ok {
		t.Error("kullanıcının TLS doğrulama seçimine dokunulmamalı")
	}
}

// Kullanıcı GitHub'da adresi elle canlı bir adrese çevirmişse PATCH YOK; kayıt ona eşitlenir.
func TestWebhookTasiElleDuzeltilmis(t *testing.T) {
	_, n := sahteGitHub(t, "https://panel.ornek.com/api/v1/git-webhook/gizli1", 200)
	son, degisti, err := webhookTasi(context.Background(), "tok", "o/r", 7, 23003, map[int]bool{23003: true})
	if err != nil || degisti || son != "https://panel.ornek.com/api/v1/git-webhook/gizli1" || *n != 0 {
		t.Fatalf("elle düzeltilmiş adrese dokunulmamalı: son=%q degisti=%v err=%v patch=%d", son, degisti, err, *n)
	}
}

// Elle host değiştirilmiş ama port ölü: host KORUNUR, yalnız port taşınır.
func TestWebhookTasiHostKorunur(t *testing.T) {
	patch, _ := sahteGitHub(t, "https://panel.ornek.com:8443/api/v1/git-webhook/gizli1", 200)
	son, degisti, err := webhookTasi(context.Background(), "tok", "o/r", 7, 23003, map[int]bool{23003: true})
	if err != nil || !degisti || son != "https://panel.ornek.com:23003/api/v1/git-webhook/gizli1" || (*patch)["url"] != son {
		t.Fatalf("host korunmalı: son=%q err=%v patch=%v", son, err, *patch)
	}
}

func TestWebhookTasiTokenGecersiz(t *testing.T) {
	_, n := sahteGitHub(t, "https://1.2.3.4:8443/api/v1/git-webhook/gizli1", 401)
	if _, _, err := webhookTasi(context.Background(), "tok", "o/r", 7, 23003, map[int]bool{23003: true}); err == nil || *n != 0 {
		t.Fatalf("401'de hata dönmeli, PATCH denenmemeli: err=%v patch=%d", err, *n)
	}
	if _, _, err := webhookTasi(context.Background(), "", "o/r", 7, 23003, nil); err == nil {
		t.Fatal("token yoksa hata")
	}
}
