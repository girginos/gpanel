package domains

// silme_onay_test.go — DELETE /domains/{id} onay metni: bayi için ZORUNLU ve alan
// adıyla eşleşmeli; yönetici verirse doğrulanır, vermezse eski davranış.

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/auth"
	"girginospanel/internal/middleware"
)

func TestSilmeOnayi(t *testing.T) {
	sec := []byte("silme-onay-test-jwt-anahtari-0123456789")
	middleware.Init(nil)
	r := chi.NewRouter()
	r.Use(middleware.RequireAuth(sec))
	r.Delete("/d", func(w http.ResponseWriter, r *http.Request) {
		kod, _ := silmeOnayi(r, "Ornek.com")
		_, _ = w.Write([]byte(strconv.Itoa(kod)))
	})
	bayi, _ := auth.IssueReseller(sec, 600, 7, "bayi", 7)
	kok, _ := auth.Issue(sec, 600, 1, "root", "admin")
	for _, c := range []struct {
		ad, tok, sorgu, bek string
	}{
		{"bayi onaysız", bayi, "", "400"},
		{"bayi yanlış", bayi, "?onay=baska.com", "400"},
		{"bayi doğru (büyük/küçük harf)", bayi, "?onay=ornek.COM", "0"},
		{"yönetici onaysız (geriye uyum)", kok, "", "0"},
		{"yönetici yanlış", kok, "?onay=baska.com", "400"},
		{"yönetici doğru", kok, "?onay=ornek.com", "0"},
	} {
		req := httptest.NewRequest(http.MethodDelete, "/d"+c.sorgu, nil)
		req.Header.Set("Authorization", "Bearer "+c.tok)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Body.String() != c.bek {
			t.Errorf("%s: %q beklenen %q", c.ad, rec.Body.String(), c.bek)
		}
	}
}

func TestPlanKullanimSigar(t *testing.T) {
	db := mg_geciciDB(t)
	for _, q := range []string{
		`INSERT INTO service_plans (id, ad, disk_kota_mb) VALUES (1,'kucuk',100), (2,'buyuk',1000), (3,'sinirsiz',0)`,
		`UPDATE domains SET boyut_kb=512000 WHERE id=42`, // 500 MB
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	h := &Handlers{DB: db}
	ctx := httptest.NewRequest(http.MethodGet, "/", nil).Context()
	if err := h.planKullanimSigar(ctx, 42, 1); err == nil {
		t.Fatal("500 MB kullanım 100 MB planına sığmamalı")
	}
	if err := h.planKullanimSigar(ctx, 42, 2); err != nil {
		t.Fatalf("1000 MB planına sığmalı: %v", err)
	}
	if err := h.planKullanimSigar(ctx, 42, 3); err != nil {
		t.Fatalf("sınırsız plana sığmalı: %v", err)
	}
}
