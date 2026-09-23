package kapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// KanitliKapi zorlamadan BAĞIMSIZ çalışır: açılış ölçümü zorlama açılmadan önce
// zinciri test edebilmeli.
func TestKanitliKapiZorlamadanBagimsiz(t *testing.T) {
	testOrtami(t)
	ZorlamaAyarla(false)
	r := httptest.NewRequest(http.MethodGet, "/api/v1/kapi", nil)
	r.Header.Set(Baslik, kapiBasligi(Musteri, 23003))
	if k := KanitliKapi(r); k != Musteri {
		t.Fatalf("zorlama kapalıyken de kanıtlı kapı okunmalı, got %q", k)
	}
	if k := Istek(r); k != "" {
		t.Fatalf("Istek ise zorlama kapalıyken \"\" dönmeli, got %q", k)
	}
	r.Header.Set(Baslik, kapiBasligi(Bilinmeyen, 23003)) // map varsayılanı
	if k := KanitliKapi(r); k != "" {
		t.Fatalf("tanımsız değer → \"\", got %q", k)
	}
}

// Başlık gizliyi taşımalı: kapı satırı olmayan bir location'dan (yapılandırma kayması)
// istemcinin kendi X-GP-Kapi'si geçse bile kapı taklit edilemez.
func TestKanitliKapiGizliVePort(t *testing.T) {
	testOrtami(t)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	cases := map[string]string{
		kapiBasligi(Admin, 21001):                  Admin,
		"admin":                                    "", // eski biçim / istemci sahtesi (gizlisiz)
		"admin 21001 yanlis-gizli":                 "",
		"admin 21001":                              "",
		"admin 99999 " + testGizli:                 "", // geçersiz port
		"admin abc " + testGizli:                   "",
		"admin 21001 " + testGizli + " fazla-alan": "",
	}
	for v, bek := range cases {
		r.Header.Set(Baslik, v)
		if k := KanitliKapi(r); k != bek {
			t.Errorf("%q → %q beklenir, got %q", v, bek, k)
		}
	}
	r.Header.Set(Baslik, kapiBasligi(Musteri, 23003))
	if k, p := kanitliKapiPort(r); k != Musteri || p != 23003 {
		t.Fatalf("kapı+port okunmalı: %q %d", k, p)
	}
}

func TestTipHandler(t *testing.T) {
	guven, ip := testOrtami(t)
	tipAl := func(baslik string) (string, *httptest.ResponseRecorder) {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/kapi", nil)
		if baslik != "" {
			r.Header.Set(Baslik, baslik)
		}
		rec := httptest.NewRecorder()
		TipHandler(rec, r)
		var v struct {
			Tip string `json:"tip"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&v); err != nil {
			t.Fatalf("JSON: %v", err)
		}
		return v.Tip, rec
	}
	ZorlamaAyarla(true)
	cases := map[string]string{
		kapiBasligi(Musteri, 23003): "musteri", kapiBasligi(Admin, 21001): "yonetim",
		kapiBasligi(Bayi, 22002): "yonetim", kapiBasligi(Eski, 8443): "karma", "": "karma",
	}
	for baslik, bek := range cases {
		if got, rec := tipAl(baslik); got != bek {
			t.Errorf("başlık %q → %q beklenir, got %q", baslik, bek, got)
		} else if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("tip önbelleğe alınmamalı (port değişince bayat kalır)")
		}
	}
	// Admin ve bayi portu AYNI yanıtı almalı: yönetici portu bu uçtan ayırt edilemez.
	a, _ := tipAl(kapiBasligi(Admin, 21001))
	b, _ := tipAl(kapiBasligi(Bayi, 22002))
	if a != b {
		t.Fatalf("admin (%q) ve bayi (%q) ayırt edilebilir", a, b)
	}
	// Zorlama KAPALI: dışarıya karma (sunucu her girişi her kapıda kabul ederken arayüz
	// formu gizleyip kurtarma yolunu kapatmasın) — ama loopback ölçümü gerçeği görür.
	ZorlamaAyarla(false)
	if got, _ := tipAl(kapiBasligi(Musteri, 23003)); got != "karma" {
		t.Fatalf("zorlama kapalı + dış istemci → karma, got %q", got)
	}
	*ip = "127.0.0.1"
	if got, _ := tipAl(kapiBasligi(Musteri, 23003)); got != "musteri" {
		t.Fatalf("zorlama kapalı + loopback ölçüm → gerçek tip, got %q", got)
	}
	ZorlamaAyarla(true)
	*guven = false
	if got, _ := tipAl(kapiBasligi(Musteri, 23003)); got != "karma" {
		t.Fatalf("nginx kanıtı yoksa başlığa güvenilmez → karma, got %q", got)
	}
}

// Ölçüm kanıtı: yalnız loopback + kanıtlı istek + geçerli nonce; kapı ve porta bağlı.
func TestTipHandlerOlcumKaniti(t *testing.T) {
	guven, ip := testOrtami(t)
	const nonce = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	kanitAl := func(baslik, n string) string {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/kapi", nil)
		r.Header.Set(Baslik, baslik)
		if n != "" {
			r.Header.Set(OlcumBaslik, n)
		}
		rec := httptest.NewRecorder()
		TipHandler(rec, r)
		var v map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&v); err != nil {
			t.Fatal(err)
		}
		return v["kanit"]
	}
	*ip = "127.0.0.1"
	if got := kanitAl(kapiBasligi(Admin, 21001), nonce); got != OlcumKaniti(testGizli, nonce, Admin, 21001) {
		t.Fatalf("loopback ölçüm kanıtı hatalı: %q", got)
	}
	// Aynı nonce, bayi kapısı: kanıt FARKLI (ölçüm admin/bayi etiket kaymasını yakalar)
	// ama yanıtın kendisinden kapı adı okunamaz.
	if kanitAl(kapiBasligi(Bayi, 22002), nonce) == OlcumKaniti(testGizli, nonce, Admin, 22002) {
		t.Fatal("kanıt kapı adına bağlı olmalı")
	}
	// Başka bir porta aktarılan istek beklenen portun kanıtını üretemez.
	if kanitAl(kapiBasligi(Admin, 23003), nonce) == OlcumKaniti(testGizli, nonce, Admin, 21001) {
		t.Fatal("kanıt porta bağlı olmalı")
	}
	for ad, n := range map[string]string{"nonce yok": "", "kısa": "abcd", "hex değil": "zz" + nonce[2:]} {
		if got := kanitAl(kapiBasligi(Admin, 21001), n); got != "" {
			t.Errorf("%s → kanıt dönmemeli, got %q", ad, got)
		}
	}
	*ip = "203.0.113.7"
	if got := kanitAl(kapiBasligi(Admin, 21001), nonce); got != "" {
		t.Fatal("dış istemciye kanıt dönmemeli")
	}
	*ip, *guven = "127.0.0.1", false
	if got := kanitAl(kapiBasligi(Admin, 21001), nonce); got != "" {
		t.Fatal("nginx kanıtı olmayan isteğe (doğrudan :8080) kanıt dönmemeli")
	}
}
