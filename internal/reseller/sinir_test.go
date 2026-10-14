package reseller

// sinir_test.go — VM doğrulaması 2026-10-13 (ajan D): sütun/algoritma sınırları 500 + ham SQL
// yerine 400 dönmeli.
//   - bayi paketi: aciklama > 255, INT sütunu taşması (max_domain, mail_*), negatif posta limiti;
//   - bayi parolası: bcrypt 72 bayt sınırı (73+ bayt "parola işlenemedi" 500 veriyordu).

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestBayiPaketi_SutunSinirlari400(t *testing.T) {
	db := kaDB(t)
	h := bpRouter(&Handlers{DB: db})
	for _, c := range []struct {
		ad string
		g  map[string]any
	}{
		{"açıklama 256", map[string]any{"ad": "Sinir A", "aciklama": strings.Repeat("ş", 256), "max_domain": 1, "fazla_satis": true}},
		{"max_domain taşma", map[string]any{"ad": "Sinir B", "max_domain": 2147483648, "fazla_satis": true}},
		{"mail_max_email taşma", map[string]any{"ad": "Sinir C", "max_domain": 1, "mail_max_email": 2147483648, "fazla_satis": true}},
		{"mail_kutu_kota_mb negatif", map[string]any{"ad": "Sinir D", "max_domain": 1, "mail_kutu_kota_mb": -1, "fazla_satis": true}},
	} {
		for _, yol := range []struct{ metod, yol string }{{"POST", "/reseller-plans"}, {"PUT", "/reseller-plans/1"}} {
			w := kaAdmin(t).istek(h, yol.metod, yol.yol, c.g)
			if w.Code != http.StatusBadRequest || strings.Contains(w.Body.String(), "Error 1") {
				t.Errorf("%s %s: 400 ve ham SQL'siz beklenir: %d %s", c.ad, yol.metod, w.Code, w.Body.String())
			}
		}
	}
	if w := kaAdmin(t).istek(h, "POST", "/reseller-plans", map[string]any{"ad": "Sinir E", "aciklama": strings.Repeat("ş", 255),
		"max_domain": 2147483647, "fazla_satis": true}); w.Code != http.StatusCreated {
		t.Errorf("sınırda (255 karakter, 2147483647) kabul edilmeli: %d %s", w.Code, w.Body.String())
	}
}

func TestBayiOlustur_Bcrypt72Bayt(t *testing.T) {
	db := opGeciciDB(t)
	h := &Handlers{DB: db}
	r := chi.NewRouter()
	r.Post("/resellers", h.Olustur)
	havuzKancaSifirla()
	kos := func(kul, pw string) *httptest.ResponseRecorder {
		b, _ := json.Marshal(map[string]any{"paket_id": 1, "kullanici": kul, "parola": pw})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/resellers", bytes.NewReader(b)))
		return w
	}
	for _, pw := range []string{"Ab1" + strings.Repeat("x", 70), strings.Repeat("ğ", 37)} { // 73 bayt, 74 bayt
		if w := kos("uzunpw", pw); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "72 bayt") {
			t.Errorf("%d baytlık parola: 400 '72 bayt' beklenir: %d %s", len(pw), w.Code, w.Body.String())
		}
	}
	if n := opSay(t, db, `SELECT COUNT(*) FROM users WHERE username='uzunpw'`); n != 0 {
		t.Fatal("reddedilen bayi oluşturuldu")
	}
	if w := kos("tamam72", "Ab1"+strings.Repeat("x", 69)); w.Code != http.StatusCreated {
		t.Fatalf("72 bayt kabul edilmeli: %d %s", w.Code, w.Body.String())
	}
}
