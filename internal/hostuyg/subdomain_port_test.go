package hostuyg

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type subdomainSahte struct {
	dir      string
	cagrilar []string
	tHata    error
}

func subdomainTestKur(t *testing.T, dosyalar map[string]string) *subdomainSahte {
	t.Helper()
	s := &subdomainSahte{dir: t.TempDir()}
	eskiDizin, eskiNginx := subdomainDizini, subdomainNginx
	t.Cleanup(func() { subdomainDizini, subdomainNginx = eskiDizin, eskiNginx })
	subdomainDizini = s.dir
	subdomainNginx = func(arg ...string) ([]byte, error) {
		c := strings.Join(arg, " ")
		s.cagrilar = append(s.cagrilar, c)
		if c == "-t" && s.tHata != nil {
			return []byte("emerg"), s.tHata
		}
		return nil, nil
	}
	for ad, icerik := range dosyalar {
		if err := os.WriteFile(filepath.Join(s.dir, ad), []byte(icerik), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func (s *subdomainSahte) oku(t *testing.T, ad string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(s.dir, ad))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const uygVhost = "server {\n    listen 8443 ssl;\n    listen [::]:8443 ssl;\n    server_name gitea.panel.test;\n}\n"

// Geçiş bitip 8443 kapanınca uygulama vhost'ları müşteri kapısına taşınır; 443'e elle
// alınmış vhost'a, acme vhost'una ve başka dosyalara dokunulmaz.
func TestSubdomainVhostlariCanliPortaTasi(t *testing.T) {
	elle443 := "server {\n    listen 443 ssl;\n    server_name n8n.panel.test;\n}\n"
	s := subdomainTestKur(t, map[string]string{
		"gpanel-app-gitea.conf":      uygVhost,
		"gpanel-app-n8n.conf":        elle443,
		"gpanel-app-gitea-acme.conf": uygVhost,
		"dom_c_ornek.conf":           uygVhost,
	})
	canli := map[int]bool{21001: true, 22002: true, 23003: true}
	if err := SubdomainVhostlariCanliPortaTasi(23003, canli); err != nil {
		t.Fatal(err)
	}
	g := s.oku(t, "gpanel-app-gitea.conf")
	if !strings.Contains(g, "listen 23003 ssl;") || !strings.Contains(g, "listen [::]:23003 ssl;") || strings.Contains(g, "8443") {
		t.Fatalf("kapanan porttaki uygulama vhost'u genel porta taşınmalı:\n%s", g)
	}
	if s.oku(t, "gpanel-app-n8n.conf") != elle443 {
		t.Error("443'teki vhost'a dokunulmamalı")
	}
	if s.oku(t, "gpanel-app-gitea-acme.conf") != uygVhost || s.oku(t, "dom_c_ornek.conf") != uygVhost {
		t.Error("acme vhost'u ve uygulama dışı dosyalar değişmemeli")
	}
	if strings.Join(s.cagrilar, "|") != "-t|-s reload" {
		t.Errorf("nginx -t + reload bekleniyordu: %v", s.cagrilar)
	}

	// Geçişte 8443 hâlâ canlı: taşıma YOK, nginx'e dokunulmaz.
	s = subdomainTestKur(t, map[string]string{"gpanel-app-gitea.conf": uygVhost})
	canli[8443] = true
	if err := SubdomainVhostlariCanliPortaTasi(23003, canli); err != nil {
		t.Fatal(err)
	}
	if s.oku(t, "gpanel-app-gitea.conf") != uygVhost || len(s.cagrilar) != 0 {
		t.Error("canlı porttaki vhost taşınmamalı, nginx çağrılmamalı")
	}
}

func TestSubdomainVhostlariCanliPortaTasiGeriAlir(t *testing.T) {
	s := subdomainTestKur(t, map[string]string{"gpanel-app-gitea.conf": uygVhost})
	s.tHata = errors.New("emerg")
	if err := SubdomainVhostlariCanliPortaTasi(23003, map[int]bool{23003: true}); err == nil {
		t.Fatal("nginx -t düşünce hata dönmeli")
	}
	if s.oku(t, "gpanel-app-gitea.conf") != uygVhost {
		t.Fatal("nginx -t düşünce vhost ÖZGÜN haline geri alınmalı")
	}
}

// Refaktör sonrası dış port değişimi (Feature 6) aynı davranmalı.
func TestSubdomainVhostlariPortGuncelle(t *testing.T) {
	s := subdomainTestKur(t, map[string]string{"gpanel-app-gitea.conf": uygVhost})
	if err := SubdomainVhostlariPortGuncelle(8443, 7443); err != nil {
		t.Fatal(err)
	}
	if g := s.oku(t, "gpanel-app-gitea.conf"); !strings.Contains(g, "listen 7443 ssl;") || !strings.Contains(g, "listen [::]:7443 ssl;") {
		t.Fatalf("eski→yeni taşınmalı:\n%s", g)
	}
	if err := SubdomainVhostlariPortGuncelle(9999, 7000); err != nil || len(s.cagrilar) != 2 {
		t.Fatalf("eşleşme yoksa nginx'e dokunulmamalı: err=%v çağrılar=%v", err, s.cagrilar)
	}
}
