package httpx

import (
	"net/http/httptest"
	"testing"
)

// TestClientIPKesin: yetki kararı için IP — kanıtsız loopback isteğin X-Real-IP'sine
// ASLA güvenilmez (proxy gizlisi kurulu olsun olmasın; ClientIP'in "gizli yoksa
// eski davranış" yedeği burada YOK).
func TestClientIPKesin(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "198.51.100.4:5000"
	r.Header.Set("X-Real-IP", "203.0.113.9")
	if got := ClientIPKesin(r); got != "198.51.100.4" {
		t.Fatalf("doğrudan bağlantı soket adresini dönmeli: %q", got)
	}
	r = httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "127.0.0.1:5000"
	r.Header.Set("X-Real-IP", "203.0.113.9")
	r.Header.Set("X-Gosp-Proxy", "sahte-gizli-degeri-0123456789abcdef0123456789")
	if got := ClientIPKesin(r); got != "" {
		t.Fatalf("kanıtsız loopback isteğin başlığına güvenildi: %q", got)
	}
	r.RemoteAddr = "[::1]:5000"
	if got := ClientIPKesin(r); got != "" {
		t.Fatalf("kanıtsız ::1 isteğin başlığına güvenildi: %q", got)
	}
}
