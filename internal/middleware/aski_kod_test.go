package middleware

// aski_kod_test.go — askı 403'lerinin TÜMÜ makine okunur kod taşır ({"kod":"hesap_askida"}).
// Ön yüz metin eşleştirmez, bu koda bakıp tam ekran askı sayfasına geçer; bir kapı kodu
// unutursa askıdaki kullanıcı yine paneli (boş/hatalı) görür. Üç kapı: bayi oturumu
// (oturumKarar), müşteri oturumu (musteriKarar), abonelik askısı (domainAskidaKod).
// Askı dışı redler (401 oturum sonlandı, 503 doğrulanamadı, 403 başka kiracı) kod TAŞIMAZ.

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"girginospanel/internal/auth"
	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

// askiRouter: üretimdeki /domains/{id} zinciri; iptal kontrolleri stub'lı (DB'siz).
func askiRouter(t *testing.T) http.Handler {
	t.Helper()
	scopeDB = &sql.DB{} // yalnız nil-kontrolünü geçmek için; sorgular stub'lanıyor
	eskiO, eskiM, eskiD := oturumSorguFn, musteriSorguFn, domainAskiSorguFn
	t.Cleanup(func() {
		scopeDB = nil
		oturumSorguFn, musteriSorguFn, domainAskiSorguFn = eskiO, eskiM, eskiD
		oturumCacheMu.Lock()
		oturumCache = map[int64]oturumDurum{}
		oturumCacheMu.Unlock()
		musteriCacheMu.Lock()
		musteriCache = map[int64]musteriDurum{}
		musteriCacheMu.Unlock()
		domainAskiCacheMu.Lock()
		domainAskiCache = map[int64]domainAskiDurum{}
		domainAskiCacheMu.Unlock()
	})
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(RequireAuth(mwSecret))
		r.With(MusteriScope).Get("/domains/{id}", mwOK)
	})
	return r
}

func govdeOku(t *testing.T, rec *httptest.ResponseRecorder) httpx.ErrorBody {
	t.Helper()
	var b httpx.ErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("gövde JSON değil: %q", rec.Body)
	}
	return b
}

func askiBekle(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	b := govdeOku(t, rec)
	if rec.Code != http.StatusForbidden || b.Kod != httpx.KodHesapAskida || b.Hata != httpx.HesapAskidaMesaj {
		t.Fatalf("askı 403 + kod beklenir: %d %q", rec.Code, rec.Body)
	}
}

func TestAskiKodu_BayiOturumu(t *testing.T) {
	h := askiRouter(t)
	oturumSorguFn = func(context.Context, int64) (bool, string, int64, error) { return true, "suspended", 0, nil }
	tok, err := auth.IssueResellerAt(mwSecret, 3600, 7, "bayi7", 7, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	askiBekle(t, mwDo(h, http.MethodGet, "/domains/42", "Bearer "+tok))
}

func TestAskiKodu_MusteriOturumu(t *testing.T) {
	h := askiRouter(t)
	musteriSorguFn = func(context.Context, int64) (bool, string, int64, error) { return true, "suspended", 0, nil }
	askiBekle(t, mwDo(h, http.MethodGet, "/domains/42", "Bearer "+mwMusteriToken(t, 5, 42, mwSecret)))
}

func TestAskiKodu_AbonelikAskisi(t *testing.T) {
	h := askiRouter(t)
	musteriSorguFn = func(context.Context, int64) (bool, string, int64, error) { return true, "active", 0, nil }
	domainAskiSorguFn = func(context.Context, int64) (bool, bool, error) { return true, true, nil }
	askiBekle(t, mwDo(h, http.MethodGet, "/domains/42", "Bearer "+mwMusteriToken(t, 5, 42, mwSecret)))
}

// redYaz'dan geçen ama askı OLMAYAN 403'ler ("hesap bulunamadı": oturum_cache.go ve
// musteri_oturum.go) kodsuz kalır. redYaz yalnız 403 durumuna bakıp koda çevirirse silinmiş
// hesabın oturumu da askı sayfasına düşerdi; ayrım (403 + HesapAskidaMesaj) mesajdadır.
func TestAskiKodu_HesapBulunamadiKodsuz(t *testing.T) {
	h := askiRouter(t)

	// Bayi: users satırı yok → 403 "hesap bulunamadı", kodsuz.
	oturumSorguFn = func(context.Context, int64) (bool, string, int64, error) { return false, "", 0, nil }
	tok, err := auth.IssueResellerAt(mwSecret, 3600, 7, "bayi7", 7, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	rec := mwDo(h, http.MethodGet, "/domains/42", "Bearer "+tok)
	if b := govdeOku(t, rec); rec.Code != http.StatusForbidden || b.Kod != "" || b.Hata != "hesap bulunamadı" {
		t.Fatalf("bayi hesap-yok 403 kodsuz olmalı: %d %q", rec.Code, rec.Body)
	}

	// Müşteri: ftp_accounts satırı yok → 403 "hesap bulunamadı", kodsuz.
	musteriSorguFn = func(context.Context, int64) (bool, string, int64, error) { return false, "", 0, nil }
	rec = mwDo(h, http.MethodGet, "/domains/42", "Bearer "+mwMusteriToken(t, 5, 42, mwSecret))
	if b := govdeOku(t, rec); rec.Code != http.StatusForbidden || b.Kod != "" || b.Hata != "hesap bulunamadı" {
		t.Fatalf("müşteri hesap-yok 403 kodsuz olmalı: %d %q", rec.Code, rec.Body)
	}
}

// Askı dışı redler kodsuz kalır: ön yüz onları askı sayfasına çevirmemeli.
func TestAskiKodu_AskiDisiRedlerKodsuz(t *testing.T) {
	h := askiRouter(t)
	tok := mwMusteriToken(t, 5, 42, mwSecret)

	// Başka kiracının aboneliği: 403 ama askı değil.
	musteriSorguFn = func(context.Context, int64) (bool, string, int64, error) { return true, "active", 0, nil }
	domainAskiSorguFn = func(context.Context, int64) (bool, bool, error) { return true, false, nil }
	if rec := mwDo(h, http.MethodGet, "/domains/99", "Bearer "+tok); rec.Code != http.StatusForbidden || govdeOku(t, rec).Kod != "" {
		t.Fatalf("başka kiracı 403 kodsuz olmalı: %d %q", rec.Code, rec.Body)
	}
	// Askıda olmayan kendi aboneliği geçer.
	if rec := mwDo(h, http.MethodGet, "/domains/42", "Bearer "+tok); rec.Code != http.StatusOK {
		t.Fatalf("aktif abonelik geçmeli: %d %q", rec.Code, rec.Body)
	}
	// Oturum sonlandırıldı (parola değişti): 401, kodsuz.
	musteriSorguFn = func(context.Context, int64) (bool, string, int64, error) { return true, "active", 1 << 40, nil }
	if rec := mwDo(h, http.MethodGet, "/domains/42", "Bearer "+tok); rec.Code != http.StatusUnauthorized || govdeOku(t, rec).Kod != "" {
		t.Fatalf("sonlandırılmış oturum 401 kodsuz olmalı: %d %q", rec.Code, rec.Body)
	}
	// Durum doğrulanamıyor (DB hatası, önbellek yok): 503, kodsuz — askı sayılmaz.
	musteriSorguFn = func(context.Context, int64) (bool, string, int64, error) { return false, "", 0, sql.ErrConnDone }
	tok2 := mwMusteriToken(t, 77, 42, mwSecret)
	if rec := mwDo(h, http.MethodGet, "/domains/42", "Bearer "+tok2); rec.Code != http.StatusServiceUnavailable || govdeOku(t, rec).Kod != "" {
		t.Fatalf("doğrulanamayan durum 503 kodsuz olmalı: %d %q", rec.Code, rec.Body)
	}
}
