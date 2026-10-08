package eklenti

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/auth"
	"girginospanel/internal/middleware"
)

// İstemci "Connection: X-Gosp-Rol" ile çekirdeğin eklenti için yazdığı güvenilir kimlik
// başlığını düşürememeli (ReverseProxy hop-by-hop silmeyi Director'dan SONRA yapar).
func TestVekil_ConnectionIleKimlikBasligiDusurulemez(t *testing.T) {
	// unix yolu uzunluk sınırı (≈104): kısa dizin
	dizin, err := os.MkdirTemp("/tmp", "ek")
	if err != nil {
		t.Skip("tmp yok")
	}
	t.Cleanup(func() { os.RemoveAll(dizin) })
	soket := filepath.Join(dizin, "s")
	ln, err := net.Listen("unix", soket)
	if err != nil {
		t.Skipf("unix soket: %v", err)
	}
	var gelen http.Header
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gelen = r.Header.Clone()
		w.WriteHeader(200)
	})}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	sir := []byte("eklenti-vekil-test-anahtari-0123456789abcdef")
	tok, err := auth.IssueAt(sir, 3600, 1, "root", "admin", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	h := &Handlers{}
	r := chi.NewRouter()
	r.With(middleware.RequireAuth(sir)).Handle("/x", h.vekilKur("ai", soket))

	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Connection", "X-Gosp-Rol, X-Gosp-Uid, keep-alive")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("kod %d: %s", rec.Code, rec.Body.String())
	}
	if gelen.Get("X-Gosp-Rol") != "admin" || gelen.Get("X-Gosp-Uid") != "1" {
		t.Errorf("kimlik başlıkları düşürüldü: %v", gelen)
	}
}
