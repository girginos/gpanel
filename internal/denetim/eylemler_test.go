package denetim

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"girginospanel/internal/auth"
	"girginospanel/internal/sahtedb"
)

// Etiketler insan dilinde: boş değil, kodun kendisi değil, ayraç/ham kod parçası yok.
func TestEylemKatalogu_EtiketlerOkunur(t *testing.T) {
	for kod, e := range eylemKatalogu {
		for dil, et := range map[string]string{"tr": e.TR, "en": e.EN} {
			if strings.TrimSpace(et) == "" || et == kod || strings.ContainsAny(et, "._") {
				t.Errorf("%s %s etiketi okunur değil: %q", kod, dil, et)
			}
			if len([]rune(et)) > 48 {
				t.Errorf("%s %s etiketi çok uzun (%d): %q", kod, dil, len([]rune(et)), et)
			}
		}
	}
}

func TestEylemEtiketi_BilinmeyenKodOkunurGeriDonus(t *testing.T) {
	for kod, bek := range map[string]string{
		"musteri.adina_yeni": "Musteri adina yeni",
		"x.y-z":              "X y z",
		"tek":                "Tek",
		"":                   "",
	} {
		if e := eylemEtiketi(kod); e.TR != bek || e.EN != bek {
			t.Errorf("eylemEtiketi(%q) = %+v, beklenen %q", kod, e, bek)
		}
	}
	if e := eylemEtiketi("musteri.adina_cikis"); e.TR != "Müşteri panelinden çıkıldı" {
		t.Errorf("katalog etiketi beklenirdi: %+v", e)
	}
}

func TestNotSade_TekrarEdenAciklamaGizlenir(t *testing.T) {
	for _, c := range []struct{ kod, not, bek string }{
		{"musteri.adina_cikis", "müşteri adına oturum kapatıldı", ""},
		{"musteri.adina_oturum", "müşteri adına oturum açıldı", ""},
		{"musteri.adina_kod", "müşteri paneline giriş kodu üretildi", ""},
		{"websec.tara", "sirada=3", "sirada=3"}, // bilgi taşır: aciklama.go "3 alan adı tarama sırasına alındı"
		{"winajan.sil", "windows ajanı silindi", ""},
		{"hosting.plan", "Plan değiştirildi", ""}, // etiketin aynısı
		{"hosting.askiya_al", "toplu işlem", "toplu işlem"},
		{"bayi.guncelle", "ad, e-posta", "ad, e-posta"},
		{"bilinmeyen.kod", "  ayrıntı  ", "ayrıntı"},
	} {
		if g := notSade(c.kod, c.not); g != c.bek {
			t.Errorf("notSade(%q,%q) = %q, beklenen %q", c.kod, c.not, g, c.bek)
		}
	}
}

// Liste: aktör ve "kimin adına" ayrı alanlarda (eski tek-dizge kayıt da), ham
// müşteri kimliği yanıtta yok, etiketler yanıtla gelir, teknik ara adım
// varsayılan listede gizlenir.
func TestListe_AktorAdinaAyriVeEtiketli(t *testing.T) {
	sd, db := sahtedb.Yeni()
	eski := claimsAl
	t.Cleanup(func() { claimsAl = eski })
	claimsAl = func(*http.Request) *auth.Claims { return &auth.Claims{UserID: 1, Username: "root", Role: "admin"} }
	sd.Satirlar("LEFT JOIN users ru",
		// eski kayıt: adına bilgisi aktör dizgesinde
		[]any{int64(3), "2026-10-03 10:00:00", "girgin (adına musteri:999999038)", int64(0), "reseller", "1.2.3.4",
			"musteri.adina_cikis", "musteri-alan.test", `{"not":"müşteri adına oturum kapatıldı"}`, int64(1), int64(7), "girgin"},
		// yeni kayıt: ayrı kolon
		[]any{int64(2), "2026-10-03 09:59:00", "girgin", int64(999999038), "reseller", "1.2.3.4",
			"erisim.ayar", "musteri-alan.test", `{"not":"aktif=true"}`, int64(1), int64(7), "girgin"},
		// müşterinin kendi işlemi
		[]any{int64(1), "2026-10-03 09:58:00", "musteri:999999038", int64(0), "", "5.6.7.8",
			"yeni.bir_eylem", "musteri-alan.test", "", int64(1), int64(7), "girgin"},
		// hostingi silinmiş müşterinin işlemi: alan adı çözülemez
		[]any{int64(0), "2026-10-03 09:57:00", "musteri:888888017", int64(0), "", "5.6.7.8",
			"auth.login", "", "", int64(1), int64(7), "girgin"},
		// API anahtarıyla yapılan işlem: aktör = sahip, anahtar adı ayrı alanda
		[]any{int64(-1), "2026-10-03 09:56:00", "girgin (API anahtarı WHMCS)", int64(0), "reseller", "9.9.9.9",
			"hosting.olustur", "yeni.test", `{"not":"plan=3"}`, int64(1), int64(7), "girgin"},
	)
	sd.Satirlar("SELECT id, alan_adi FROM domains", []any{int64(999999038), "musteri-alan.test"})
	sd.Satirlar("SELECT DISTINCT a.action", []any{"musteri.adina_kod"}, []any{"musteri.adina_cikis"})

	h := &Handlers{DB: db}
	rr := httptest.NewRecorder()
	h.Liste(rr, httptest.NewRequest("GET", "/denetim", nil))
	if rr.Code != 200 {
		t.Fatalf("200 beklenirdi: %d %s", rr.Code, rr.Body)
	}
	if b := rr.Body.String(); strings.Contains(b, "999999038") || strings.Contains(b, "888888017") || strings.Contains(b, "adına musteri") {
		t.Fatalf("yanıtta ham müşteri kimliği / eski ek olmamalı: %s", rr.Body)
	}
	var y struct {
		Kayitlar  []Kayit                `json:"kayitlar"`
		Etiketler map[string]EylemEtiket `json:"etiketler"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &y); err != nil || len(y.Kayitlar) != 5 {
		t.Fatalf("yanıt çözülemedi: %v %s", err, rr.Body)
	}
	for i, k := range y.Kayitlar[:2] {
		if k.Aktor != "girgin" || !k.Adina || k.AdinaAlan != "musteri-alan.test" || k.AktorRol != "reseller" {
			t.Errorf("kayıt %d: aktör/adına ayrı olmalı: %+v", i, k)
		}
	}
	if y.Kayitlar[0].Detay != "" {
		t.Errorf("etiketi tekrarlayan açıklama gizlenmeli: %q", y.Kayitlar[0].Detay)
	}
	if y.Kayitlar[1].Detay != "aktif=true" {
		t.Errorf("bilgi taşıyan açıklama kalmalı: %q", y.Kayitlar[1].Detay)
	}
	if a := y.Kayitlar[1].Aciklama; a == nil || a.TR != "Erişim kısıtlaması açık" || a.EN != "Access restriction on" {
		t.Errorf("sade TR/EN açıklama yanıtta olmalı: %+v", a)
	}
	if y.Kayitlar[0].Aciklama != nil {
		t.Errorf("gizlenen açıklamanın sade hâli de olmamalı: %+v", y.Kayitlar[0].Aciklama)
	}
	if m := y.Kayitlar[2]; m.Aktor != "musteri-alan.test" || m.AktorRol != "müşteri" || m.Adina {
		t.Errorf("müşteri kaydı alan adıyla gösterilmeli: %+v", m)
	}
	if m := y.Kayitlar[4]; m.Aktor != "girgin" || m.APIAnahtar != "WHMCS" || m.AktorRol != "reseller" || m.Adina {
		t.Errorf("API anahtarı kaydı: aktör sahip, anahtar adı ayrı olmalı: %+v", m)
	}
	if m := y.Kayitlar[4]; m.Aciklama == nil || m.Aciklama.TR != "Plan: #3" {
		t.Errorf("API anahtarı kaydının açıklaması sadeleşmeli: %+v", m.Aciklama)
	}
	if m := y.Kayitlar[3]; m.Aktor != "" || m.AktorRol != "müşteri" {
		t.Errorf("hostingi silinmiş müşteri kaydında ham kimlik yerine boş aktör (ön yüz \"Müşteri\") beklenir: %+v", m)
	}
	for kod, bek := range map[string]string{
		"musteri.adina_cikis": "Müşteri panelinden çıkıldı",
		"musteri.adina_kod":   "Müşteri paneline giriş",
		"erisim.ayar":         "Erişim ayarı değişti",
		"yeni.bir_eylem":      "Yeni bir eylem",
	} {
		if y.Etiketler[kod].TR != bek || y.Etiketler[kod].EN == "" {
			t.Errorf("etiket %s = %+v, beklenen %q", kod, y.Etiketler[kod], bek)
		}
	}
	if !sd.Sorgulandi("NOT (a.action IN (?) AND a.ok=1)") {
		t.Error("varsayılan listede teknik ara adım (giriş kodu) gizlenmeli")
	}

	// Süzgeçte açıkça seçilince gizleme yok.
	sd2, db2 := sahtedb.Yeni()
	rr = httptest.NewRecorder()
	(&Handlers{DB: db2}).Liste(rr, httptest.NewRequest("GET", "/denetim?eylem=musteri.adina_kod", nil))
	if rr.Code != 200 || sd2.Sorgulandi("NOT (a.action IN") {
		t.Fatalf("eylem seçiliyken teknik gizleme uygulanmamalı: %d", rr.Code)
	}
}
