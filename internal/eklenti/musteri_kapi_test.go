package eklenti

// musteri_kapi_test.go — cp_eklentiler.musteri_erisim bayrağı eklentinin kendi
// bildirimine (/saglik → "musteri_kapisi") bağlıdır:
//   - kapıyı bildiren + sağlıklı + panelin tanıdığı sürüm → 1;
//   - bildirmeyen (eski sürüm) / bilinmeyen sürüm / bozuk yanıt / ulaşılamayan → 0;
//   - panelin yönetmediği eklentiye (whitelabel, marka...) HİÇ dokunulmaz;
//   - yönetilen eklentiler: mail ve calistirici — her biri kendi bildirimiyle, birbirinden bağımsız;
//   - müşteri /eklentiler listesinde mail ve calistirici yalnız bayrak 1 iken görünür; proxy kapısı da aynı.
// Gerçek unix soketli sahte eklenti + sahte DB; canlı hiçbir şeye dokunulmaz.

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/auth"
	"girginospanel/internal/middleware"
	"girginospanel/internal/sahtedb"
)

// ek_guncelle — uzlaştırmanın yazdığı UPDATE'in sahtedb kaydı: "<sorgu> | <argümanlar>".
// Argümanlar (istenen, ad, istenen) → ör. mail için "1mail1" / "0mail0".
const ek_guncelle = "SET musteri_erisim=? WHERE ad=? AND COALESCE(musteri_erisim,0)<>? | "

// ek_soket — /saglik'e verilen işleyiciyle unix soketi dinleyen sahte eklenti.
func ek_soket(t *testing.T, saglik http.HandlerFunc) string {
	t.Helper()
	dizin, err := os.MkdirTemp("/tmp", "ek")
	if err != nil {
		t.Skip("tmp yok")
	}
	t.Cleanup(func() { os.RemoveAll(dizin) })
	yol := filepath.Join(dizin, "s")
	ln, err := net.Listen("unix", yol)
	if err != nil {
		t.Skipf("unix soket: %v", err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/saglik" && r.Method == http.MethodGet {
			saglik(w, r)
			return
		}
		http.NotFound(w, r)
	})}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return yol
}

func ek_json(govde string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(govde))
	}
}

// ek_tara — tek aktif eklenti satırıyla SaglikTara koşar; sahte DB'yi döner.
func ek_tara(t *testing.T, ad, soket string) *sahtedb.DB {
	t.Helper()
	sd, db := sahtedb.Yeni()
	sd.Satirlar("WHERE aktif=1", []any{ad, soket})
	(&Handlers{DB: db}).SaglikTara(t.Context())
	return sd
}

// ek_yonetilen — panelin bayrağını yönettiği eklentiler (musteriKapisiYonetilen ile birebir).
var ek_yonetilen = []string{"mail", "calistirici"}

func TestSaglikTara_CalistiriciKapiyiBildiriyorsaBayrak1(t *testing.T) {
	soket := ek_soket(t, ek_json(`{"durum":"ok","surum":"0.3.0","db":true,"musteri_kapisi":1}`))
	sd := ek_tara(t, "calistirici", soket)
	if !sd.Yurutuldu(ek_guncelle + "1calistirici1") {
		t.Errorf("kapıyı bildiren sağlıklı calistirici için musteri_erisim=1 yazılmalı")
	}
	if sd.Yurutuldu(ek_guncelle+"0calistirici0") || sd.Yurutuldu(ek_guncelle+"1mail1") {
		t.Errorf("yanlış bayrak/eklenti yazıldı")
	}
	if !sd.Yurutuldu("SET saglik=?") {
		t.Errorf("mevcut sağlık güncellemesi bozuldu")
	}
}

func TestSaglikTara_MailKapiyiBildiriyorsaBayrak1(t *testing.T) {
	soket := ek_soket(t, ek_json(`{"durum":"saglikli","surum":"0.3.10","db":true,"musteri_kapisi":1}`))
	sd := ek_tara(t, "mail", soket)
	if !sd.Yurutuldu(ek_guncelle + "1mail1") {
		t.Errorf("kapıyı bildiren sağlıklı mail için musteri_erisim=1 yazılmalı")
	}
	if sd.Yurutuldu(ek_guncelle + "0mail0") {
		t.Errorf("bayrak 0'a da yazılmış")
	}
	if !sd.Yurutuldu("SET saglik=?") {
		t.Errorf("mevcut sağlık güncellemesi bozuldu")
	}
}

func TestSaglikTara_EskiEklentideBayrakAsla1Olmaz(t *testing.T) {
	for _, ad := range ek_yonetilen {
		ek_eskiEklenti(t, ad)
	}
}

func ek_eskiEklenti(t *testing.T, eklenti string) {
	t.Helper()
	// Kapıyı BİLMEYEN (eski) eklenti: musteri_kapisi alanı yok.
	eski := ek_soket(t, ek_json(`{"durum":"saglikli","surum":"0.3.9","db":true}`))
	if sd := ek_tara(t, eklenti, eski); !sd.Yurutuldu(ek_guncelle+"0"+eklenti+"0") || sd.Yurutuldu(ek_guncelle+"1"+eklenti+"1") {
		t.Errorf("%s: eski eklentide bayrak 0 olmalı (1 yazılmamalı)", eklenti)
	}
	for ad, govde := range map[string]string{
		"sıfır":            `{"durum":"saglikli","musteri_kapisi":0}`,
		"bilinmeyen sürüm": `{"durum":"saglikli","musteri_kapisi":2}`,
		"negatif":          `{"durum":"saglikli","musteri_kapisi":-1}`,
		"metin":            `{"durum":"saglikli","musteri_kapisi":"1"}`,
		"mantıksal":        `{"durum":"saglikli","musteri_kapisi":true}`,
		"ondalık":          `{"durum":"saglikli","musteri_kapisi":1.5}`,
		"null":             `{"durum":"saglikli","musteri_kapisi":null}`,
		"bozuk json":       `{"durum":"saglikli","musteri_kapisi":1`,
		"json değil":       `<html>musteri_kapisi 1</html>`,
		"boş":              ``,
		"alan başka adla":  `{"durum":"saglikli","musteri_kapisi_x":1}`,
		"iç içe":           `{"durum":"saglikli","x":{"musteri_kapisi":1}}`,
		"çok büyük gövde":  `{"x":"` + strings.Repeat("a", 70<<10) + `","musteri_kapisi":1}`,
		"büyük, alan önde": `{"musteri_kapisi":1,"x":"` + strings.Repeat("a", 70<<10) + `"}`,
	} {
		soket := ek_soket(t, ek_json(govde))
		sd := ek_tara(t, eklenti, soket)
		if sd.Yurutuldu(ek_guncelle + "1" + eklenti + "1") {
			t.Errorf("%s/%s: bayrak 1 yazıldı (0 olmalı)", eklenti, ad)
		}
		if !sd.Yurutuldu(ek_guncelle + "0" + eklenti + "0") {
			t.Errorf("%s/%s: bayrak 0'a getirilmeli", eklenti, ad)
		}
	}
	// Başarısız HTTP yanıtı.
	hata := ek_soket(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"musteri_kapisi":1}`))
	})
	if sd := ek_tara(t, eklenti, hata); sd.Yurutuldu(ek_guncelle + "1" + eklenti + "1") {
		t.Errorf("%s: 500 yanıtıyla bayrak 1 yazıldı", eklenti)
	}
}

func TestSaglikTara_UlasilamayanEklentideBayrak0(t *testing.T) {
	for _, ad := range ek_yonetilen {
		dizin, _ := os.MkdirTemp("/tmp", "ek")
		t.Cleanup(func() { os.RemoveAll(dizin) })
		sd := ek_tara(t, ad, filepath.Join(dizin, "yok.sock"))
		if sd.Yurutuldu(ek_guncelle+"1"+ad+"1") || !sd.Yurutuldu(ek_guncelle+"0"+ad+"0") {
			t.Errorf("%s: soketi ölü eklentide bayrak 0 olmalı", ad)
		}
		// Soket tanımsız ('bilinmiyor') de aynı.
		sd = ek_tara(t, ad, "")
		if sd.Yurutuldu(ek_guncelle + "1" + ad + "1") {
			t.Errorf("%s: soketsiz eklentide bayrak 1 olmamalı", ad)
		}
	}
}

// Yönetilmeyen eklenti kapıyı bildirse de panel bayrağına DOKUNMAZ.
func TestSaglikTara_YonetilmeyenEklentiyeDokunmaz(t *testing.T) {
	soket := ek_soket(t, ek_json(`{"durum":"saglikli","musteri_kapisi":1}`))
	for _, ad := range []string{"whitelabel", "marka", "ai", "mail-x", "Mail", "Calistirici", "calistirici-x", "calistirici ", ""} {
		sd := ek_tara(t, ad, soket)
		if sd.Yurutuldu("musteri_erisim") {
			t.Errorf("%q: yönetilmeyen eklentinin musteri_erisim'i yazıldı", ad)
		}
	}
	if len(musteriKapisiYonetilen) != 2 || musteriKapisiYonetilen["mail"] != 1 || musteriKapisiYonetilen["calistirici"] != 1 {
		t.Errorf("yönetilen eklenti kümesi değişti: %v", musteriKapisiYonetilen)
	}
	for _, ad := range ek_yonetilen {
		if musteriKapisiYonetilen[ad] != 1 {
			t.Errorf("%s yönetilen listede değil", ad)
		}
	}
}

// Her eklentinin bayrağı YALNIZ kendi bildiriminden gelir: biri öbürünün bayrağını açamaz.
func TestSaglikTara_EklentilerBirbirindenBagimsiz(t *testing.T) {
	kapiVar := ek_soket(t, ek_json(`{"durum":"ok","musteri_kapisi":1}`))
	kapiYok := ek_soket(t, ek_json(`{"durum":"ok"}`))
	sd := ek_tara(t, "mail", kapiYok)
	if sd.Yurutuldu(ek_guncelle+"1calistirici1") || sd.Yurutuldu(ek_guncelle+"0calistirici0") || sd.Yurutuldu(ek_guncelle+"1mail1") {
		t.Errorf("mail taraması calistirici bayrağına/kendi bayrağını 1'e dokundu")
	}
	sd = ek_tara(t, "calistirici", kapiVar)
	if sd.Yurutuldu(ek_guncelle+"1mail1") || sd.Yurutuldu(ek_guncelle+"0mail0") {
		t.Errorf("calistirici taraması mail bayrağına dokundu")
	}
	if !sd.Yurutuldu(ek_guncelle + "1calistirici1") {
		t.Errorf("calistirici bayrağı açılmadı")
	}
}

// ── Müşteri görünürlüğü: liste + proxy kapısı bayrağa bağlı ─────────────────

func ek_musteriListe(t *testing.T, tok string, bayrak, calBayrak int64) []map[string]any {
	t.Helper()
	sd, db := sahtedb.Yeni()
	sd.Satirlar("FROM cp_eklentiler ORDER BY etiket",
		[]any{"calistirici", "Uygulama Çalıştırıcı", "2.0.1", int64(1), int64(1), "saglikli", calBayrak, "/uygulamalar", "araclar", "kutu", int64(40)},
		[]any{"mail", "E-posta", "0.3.10", int64(1), int64(0), "saglikli", bayrak, "/mail-sunucu", "Sunucu Yonetimi", "mail", int64(60)})
	r := chi.NewRouter()
	r.With(middleware.RequireAuth(ek_sir)).Get("/eklentiler", (&Handlers{DB: db}).Liste)
	req := httptest.NewRequest(http.MethodGet, "/eklentiler", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /eklentiler: %d %s", rec.Code, rec.Body.String())
	}
	return ek_jsonListe(t, rec.Body.Bytes())
}

func TestListe_MusteriEklentileriYalnizBayrak1Iken(t *testing.T) {
	_, _, musteri := ek_tokenlar(t)
	if l := ek_musteriListe(t, musteri, 0, 0); len(l) != 0 {
		t.Errorf("bayraklar 0: müşteri hiçbir eklenti görmemeli, gelen %v", ek_adlar(l))
	}
	for _, c := range []struct {
		mail, cal int64
		bekle     string
	}{
		{1, 0, "mail"}, {0, 1, "calistirici"}, {1, 1, "calistirici,mail"},
	} {
		l := ek_musteriListe(t, musteri, c.mail, c.cal)
		if got := strings.Join(ek_adlar(l), ","); got != c.bekle {
			t.Errorf("mail=%d cal=%d: müşteri %q görmeli, gelen %q", c.mail, c.cal, c.bekle, got)
		}
	}
}

// Proxy rol kapısı: müşteri yalnız bayrak 1 + kapsamı belli iken geçer; bayi/yönetici değişmez.
func TestRolKapisi_MusteriYalnizBayrak1(t *testing.T) {
	h := &Handlers{}
	tokenlar := func(alanAdi string) (musteri string) {
		tok, _, err := auth.GenerateMusteri(ek_sir, auth.MusteriClaims{FTPHesapID: 420, DomainID: 42, Kullanici: "m", AlanAdi: alanAdi}, 3600)
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}
	admin, bayi, _ := ek_tokenlar(t)
	kapi := func(tok string, e *Eklenti) int {
		r := chi.NewRouter()
		r.With(middleware.RequireAuth(ek_sir)).Get("/x", func(w http.ResponseWriter, r *http.Request) {
			if h.rolKapisi(w, r, e) {
				w.WriteHeader(http.StatusNoContent)
			}
		})
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}
	for _, eklenti := range ek_yonetilen {
		acik, kapali := &Eklenti{Ad: eklenti, Aktif: true, MusteriErisim: true}, &Eklenti{Ad: eklenti, Aktif: true}
		if c := kapi(tokenlar("bayialan.test"), acik); c != http.StatusNoContent {
			t.Errorf("%s: bayrak 1 + kapsam var: müşteri geçmeli, %d", eklenti, c)
		}
		if c := kapi(tokenlar("bayialan.test"), kapali); c != http.StatusForbidden {
			t.Errorf("%s: bayrak 0: müşteri 403 almalı, %d", eklenti, c)
		}
		if c := kapi(tokenlar(""), acik); c != http.StatusForbidden {
			t.Errorf("%s: alan adı boş (kapsam belirsiz): 403 olmalı, %d", eklenti, c)
		}
		for ad, tok := range map[string]string{"yönetici": admin, "bayi": bayi} {
			if c := kapi(tok, kapali); c != http.StatusNoContent {
				t.Errorf("%s: %s davranışı değişti: bayrak 0 iken %d", eklenti, ad, c)
			}
		}
	}
}

func ek_jsonListe(t *testing.T, b []byte) []map[string]any {
	t.Helper()
	var l []map[string]any
	if err := json.Unmarshal(b, &l); err != nil {
		t.Fatalf("yanıt çözülemedi: %v", err)
	}
	return l
}
