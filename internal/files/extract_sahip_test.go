package files

// extract_sahip_test.go — YK-04: arşiv ÇIKARMA ilerleme ucu işin sahipliğini
// denetler (arşivleme ucundaki ArchiveProgress ile aynı desen). is_id'yi ele
// geçiren başka bir kiracı/bayi, KENDİ alan adının URL'si üzerinden yabancı işin
// durumunu ve hata metnini (yabancının dosya yolları) okuyamaz; sahibi okur.
// Gerçek RequireAuth + imzalı jetonlar; DB sahtedb (istek alan adının sk'si).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/auth"
	"girginospanel/internal/middleware"
	"girginospanel/internal/sahtedb"
)

var ex_sir = []byte("files-extract-sahip-test-anahtari-012345")

// ex_isEkle — sahip kiracının iş haritasına bir çıkarma işi koyar; test sonunda siler.
func ex_isEkle(t *testing.T, id, sahip string) {
	t.Helper()
	extractIslerMu.Lock()
	extractIsler[id] = &extractIs{Durum: "hata", Toplam: 3, Cikan: 1,
		Hata: "/home/" + sahip + "/public_html/gizli-musteri-listesi.zip: bozuk arşiv", sahip: sahip}
	extractIslerMu.Unlock()
	t.Cleanup(func() {
		extractIslerMu.Lock()
		delete(extractIsler, id)
		extractIslerMu.Unlock()
	})
}

// ex_ilerleme — GET /domains/{did}/files/extract-progress?id=... ; did'nin sk'si istekSK.
func ex_ilerleme(t *testing.T, tok, yol, istekSK string) (int, string) {
	t.Helper()
	sd, db := sahtedb.Yeni()
	sd.Satirlar("SELECT sistem_kullanici, is_demo FROM domains", []any{istekSK, int64(0)})
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(ex_sir))
		h := &Handlers{DB: db}
		r.Get("/domains/{id}/files/extract-progress", h.ExtractProgress)
		r.Get("/domains/{id}/subdomain/{sid}/files/extract-progress", h.ExtractProgress)
	})
	req := httptest.NewRequest(http.MethodGet, yol, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestExtractProgress_YabanciIsOkunamaz(t *testing.T) {
	admin, err := auth.IssueAt(ex_sir, 3600, 1, "root", "admin", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	bayi, err := auth.IssueResellerAt(ex_sir, 3600, 7, "bayi7", 7, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	musteri, _, err := auth.GenerateMusteri(ex_sir, auth.MusteriClaims{FTPHesapID: 430, DomainID: 43, Kullanici: "m", AlanAdi: "bayialan.test"}, 3600)
	if err != nil {
		t.Fatal(err)
	}
	const yabanciIs = "0123456789abcdef0123456789abcdef"
	ex_isEkle(t, yabanciIs, "c_yabanci")

	// Bayi / müşteri KENDİ alan adı (sk=c_bayialan) üzerinden yabancı işi soruyor.
	for rol, tok := range map[string]string{"bayi": bayi, "müşteri": musteri, "yönetici": admin} {
		for _, yol := range []string{
			"/domains/43/files/extract-progress?id=" + yabanciIs,
			"/domains/43/subdomain/9/files/extract-progress?id=" + yabanciIs,
		} {
			kod, govde := ex_ilerleme(t, tok, yol, "c_bayialan")
			if kod != http.StatusNotFound {
				t.Errorf("%s %s: yabancı iş 404 olmalı (\"yok\" ile aynı), gelen %d %s", rol, yol, kod, govde)
			}
			if strings.Contains(govde, "c_yabanci") || strings.Contains(govde, "gizli-musteri-listesi") {
				t.Errorf("%s %s: yabancı işin verisi sızdı: %s", rol, yol, govde)
			}
		}
	}
}

// Sahibi (hangi rolle gelirse gelsin, işin alan adı üzerinden) okumaya devam eder.
func TestExtractProgress_SahibiOkur(t *testing.T) {
	admin, err := auth.IssueAt(ex_sir, 3600, 1, "root", "admin", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	bayi, err := auth.IssueResellerAt(ex_sir, 3600, 7, "bayi7", 7, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	const kendiIs = "fedcba9876543210fedcba9876543210"
	ex_isEkle(t, kendiIs, "c_bayialan")
	for rol, tok := range map[string]string{"bayi": bayi, "yönetici": admin} {
		kod, govde := ex_ilerleme(t, tok, "/domains/43/files/extract-progress?id="+kendiIs, "c_bayialan")
		if kod != http.StatusOK {
			t.Fatalf("%s: kendi işi 200 olmalı: %d %s", rol, kod, govde)
		}
		var m map[string]any
		_ = json.Unmarshal([]byte(govde), &m)
		if m["durum"] != "hata" || m["toplam"] != float64(3) || m["cikan"] != float64(1) {
			t.Errorf("%s: iş durumu aynen dönmeli: %s", rol, govde)
		}
	}
}

// Extract'ın kaydettiği iş sahibine bağlıdır: sahibi okur, başka kiracı okuyamaz.
func TestExtractProgress_BaslatilanIsSahibineBagli(t *testing.T) {
	bayi, err := auth.IssueResellerAt(ex_sir, 3600, 7, "bayi7", 7, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	id, _ := extractIsBaslat("c_bayialan")
	t.Cleanup(func() {
		extractIslerMu.Lock()
		delete(extractIsler, id)
		extractIslerMu.Unlock()
	})
	if kod, govde := ex_ilerleme(t, bayi, "/domains/43/files/extract-progress?id="+id, "c_bayialan"); kod != http.StatusOK {
		t.Errorf("sahibi kendi işini okumalı: %d %s", kod, govde)
	}
	if kod, govde := ex_ilerleme(t, bayi, "/domains/44/files/extract-progress?id="+id, "c_baskasi"); kod != http.StatusNotFound {
		t.Errorf("başka kiracının alan adı üzerinden 404 olmalı: %d %s", kod, govde)
	}
}
