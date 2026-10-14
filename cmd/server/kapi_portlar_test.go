package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// /kapi/portlar müşteri + bayi kapısı portlarını döner, YÖNETİCİ portunu dönmez; ayar
// okunamazsa boş nesne (WHMCS modülü eski davranışa düşer).
func TestKapiPortlar_MusteriVeBayiDonerYoneticiDonmez(t *testing.T) {
	eski := kapiAyarYolu
	t.Cleanup(func() { kapiAyarYolu = eski })
	kapiAyarYolu = filepath.Join(t.TempDir(), "kapi.json")
	if err := os.WriteFile(kapiAyarYolu, []byte(`{"admin":28564,"bayi":22199,"musteri":25723}`), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	kapiPortlar(rec, httptest.NewRequest(http.MethodGet, "/api/v1/kapi/portlar", nil))
	var d map[string]int
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatalf("yanıt JSON değil: %v %s", err, rec.Body.String())
	}
	if d["musteri"] != 25723 || d["bayi"] != 22199 {
		t.Errorf("müşteri/bayi portu yanlış: %v", d)
	}
	if _, varmi := d["admin"]; varmi || strings.Contains(rec.Body.String(), "28564") {
		t.Errorf("yönetici portu dönmemeli: %s", rec.Body.String())
	}

	kapiAyarYolu = filepath.Join(t.TempDir(), "yok.json")
	rec = httptest.NewRecorder()
	kapiPortlar(rec, httptest.NewRequest(http.MethodGet, "/api/v1/kapi/portlar", nil))
	if s := strings.TrimSpace(rec.Body.String()); s != "{}" {
		t.Errorf("ayar yokken boş nesne beklenirdi: %s", s)
	}
}
