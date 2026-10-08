package backups

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// Yetim içe alma: yalnız <sk>-*.tar.gz düz dosyaları, kayıtlı olanları ve symlink'leri atlar.
func TestYetimIceAl_SadeceUygunDosyalariAlir(t *testing.T) {
	db := silinenDB(t)
	kok := silinenOrtam(t)
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (9141,'yetim.test','c_yetim')`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, tip, dosya, boyut_b) VALUES (9141,'oto','c_yetim-kayitli.tar.gz',1)`)
	silinenDosya(t, kok, "c_yetim", "c_yetim-eski1.tar.gz", 48*time.Hour)
	silinenDosya(t, kok, "c_yetim", "c_yetim-kayitli.tar.gz", time.Hour)
	silinenDosya(t, kok, "c_yetim", "baska-hesap.tar.gz", time.Hour) // önek uymuyor
	silinenDosya(t, kok, "c_yetim", "c_yetim-not.txt", time.Hour)    // tar.gz değil
	if err := os.Symlink("/etc/passwd", filepath.Join(kok, "c_yetim", "c_yetim-link.tar.gz")); err != nil {
		t.Fatal(err)
	}

	h := &Handlers{DB: db}
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", "9141")
	req := httptest.NewRequest(http.MethodPost, "/domains/9141/backups/yetim-ice-al", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rc))
	rec := httptest.NewRecorder()
	h.YetimIceAl(rec, req)
	if rec.Code != 200 {
		t.Fatalf("kod=%d %s", rec.Code, rec.Body.String())
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM backups WHERE domain_id=9141`).Scan(&n)
	if n != 2 { // kayıtlı + eski1
		t.Fatalf("2 kayıt olmalı (kayıtlı + eski1), n=%d", n)
	}
	var notlar string
	_ = db.QueryRow(`SELECT notlar FROM backups WHERE domain_id=9141 AND dosya='c_yetim-eski1.tar.gz'`).Scan(&notlar)
	if notlar == "" {
		t.Fatal("içe alınan kayıt işaretlenmeli")
	}
	// ikinci çağrı yinelemez
	rec = httptest.NewRecorder()
	h.YetimIceAl(rec, req)
	_ = db.QueryRow(`SELECT COUNT(*) FROM backups WHERE domain_id=9141`).Scan(&n)
	if n != 2 {
		t.Fatalf("idempotent olmalı, n=%d", n)
	}
}
