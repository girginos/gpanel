package domains

// yaris_test.go — hosting oluşturulur oluşturulmaz silinince oluşturma sonrası arka plan
// uygulaması (kaynakUygula → EnableTenantFPM) silinen hesabın vhost'unu yeniden yazıp
// yetim bırakıyordu (VM doğrulaması 2026-10-13). İki taraf kilit.Hesap ile seridir;
// silinmiş hesaba uygulama yapılmaz.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"girginospanel/internal/kilit"
)

func yr_olustur(t *testing.T, db *sql.DB, h *Handlers, alan string) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"alan_adi": alan})
	req := httptest.NewRequest(http.MethodPost, "/domains", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+aa_yonetici(t))
	rec := httptest.NewRecorder()
	aa_router(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("oluşturma: %d %s", rec.Code, rec.Body.String())
	}
}

func TestCreate_ArkaPlanUygulamasi_SilinenHesabaUygulanmaz(t *testing.T) {
	for _, sil := range []bool{true, false} {
		db := ok_db(t)
		aa_panel(t)
		ok_dikisler(t, func() {})
		cagri := make(chan int64, 2)
		olusturKaynakUygula = func(_ context.Context, _ *sql.DB, id int64) error {
			cagri <- id
			return nil
		}
		h := &Handlers{DB: db, IPv4: "203.0.113.10"}
		hk := kilit.Hesap("c_kopuktest") // ok_dikisler'in sağlaması bu hesabı döndürür
		hk.Lock()
		yr_olustur(t, db, h, "yaris.test")
		select {
		case <-cagri:
			hk.Unlock()
			t.Fatal("arka plan uygulaması hesap kilidini beklemeli")
		case <-time.After(150 * time.Millisecond):
		}
		if sil {
			aa_exec(t, db, `DELETE FROM domains WHERE alan_adi='yaris.test'`)
		}
		hk.Unlock()
		select {
		case <-cagri:
			if sil {
				t.Error("silinen hesaba kaynak/izolasyon uygulandı (yetim vhost/birim)")
			}
		case <-time.After(2 * time.Second):
			if !sil {
				t.Error("silinmeyen hesaba uygulama yapılmadı")
			}
		}
	}
}

func TestHostingYik_HesapKilidiniBekler(t *testing.T) {
	db := ok_db(t)
	var id int64
	var alan string
	if err := db.QueryRow(`SELECT id, alan_adi FROM domains ORDER BY id LIMIT 1`).Scan(&id, &alan); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE domains SET sistem_kullanici='c_yiktest' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	hk := kilit.Hesap("c_yiktest")
	hk.Lock()
	bitti := make(chan error, 1)
	go func() {
		_, err := HostingYik(context.Background(), db, id, alan, "c_yiktest", 1) // demo: sistem adımları yok
		bitti <- err
	}()
	select {
	case err := <-bitti:
		hk.Unlock()
		t.Fatalf("HostingYik hesap kilidini beklemeli (döndü: %v)", err)
	case <-time.After(150 * time.Millisecond):
	}
	hk.Unlock()
	select {
	case err := <-bitti:
		if err != nil {
			t.Fatalf("HostingYik: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("HostingYik kilit bırakılınca bitmedi")
	}
	if n := aa_sayi(t, db, `SELECT COUNT(*) FROM domains WHERE id=?`, id); n != 0 {
		t.Error("hosting satırı silinmedi")
	}
}
