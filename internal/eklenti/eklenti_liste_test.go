package eklenti

// eklenti_liste_test.go — GL-08 / BAYI-UI-05: GET /eklentiler bayiye (ve
// müşteriye) sunucunun tam eklenti envanterini DÖNDÜRMEZ:
//   - yalnız yöneticiye ait eklentiler (katalog: SadeceAdmin, ör. whitelabel) yok;
//   - satırlarda yalnız ön yüzün kullandığı ad + aktif dolu (sürüm/sağlık/etiket/
//     menü bilgisi boş), anahtar kümesi aynı;
//   - yönetici listesi DEĞİŞMEDİ.
// Gerçek RequireAuth + imzalı jetonlar; DB sahtedb; katalog gerçek (lisans.YerelBul).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/auth"
	"girginospanel/internal/lisans"
	"girginospanel/internal/middleware"
	"girginospanel/internal/sahtedb"
)

var ek_sir = []byte("eklenti-liste-test-anahtari-0123456789ab")

// cp_eklentiler satırları: ad, etiket, surum, aktif, ui, saglik, musteri_erisim,
// nav_yol, nav_grup, nav_ikon, nav_sira.
var ek_satirlar = [][]any{
	{"calistirici", "Uygulama Çalıştırıcı", "2.0.1", int64(1), int64(1), "saglikli", int64(0), "/uygulamalar", "araclar", "kutu", int64(40)},
	{"whitelabel", "Marka", "9.9.9", int64(1), int64(1), "saglksiz", int64(0), "/marka", "sistem", "boya", int64(90)},
	{"mail", "Mail Sunucu", "3.4.1", int64(0), int64(1), "saglikli", int64(1), "/mail-sunucu", "posta", "zarf", int64(10)},
}

func ek_liste(t *testing.T, tok string) ([]map[string]any, string) {
	t.Helper()
	sd, db := sahtedb.Yeni()
	sd.Satirlar("FROM cp_eklentiler ORDER BY etiket", ek_satirlar...)
	r := chi.NewRouter()
	r.With(middleware.RequireAuth(ek_sir)).Get("/eklentiler", (&Handlers{DB: db}).Liste)
	req := httptest.NewRequest(http.MethodGet, "/eklentiler", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /eklentiler: %d %s", rec.Code, rec.Body.String())
	}
	var l []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &l); err != nil {
		t.Fatalf("yanıt çözülemedi: %v", err)
	}
	return l, rec.Body.String()
}

func ek_tokenlar(t *testing.T) (admin, bayi, musteri string) {
	t.Helper()
	var err error
	if admin, err = auth.IssueAt(ek_sir, 3600, 1, "root", "admin", 0, ""); err != nil {
		t.Fatal(err)
	}
	if bayi, err = auth.IssueResellerAt(ek_sir, 3600, 7, "bayi7", 7, 0, ""); err != nil {
		t.Fatal(err)
	}
	if musteri, _, err = auth.GenerateMusteri(ek_sir, auth.MusteriClaims{FTPHesapID: 420, DomainID: 42, Kullanici: "m", AlanAdi: "bayialan.test"}, 3600); err != nil {
		t.Fatal(err)
	}
	return
}

func ek_adlar(l []map[string]any) []string {
	var out []string
	for _, e := range l {
		out = append(out, e["ad"].(string))
	}
	sort.Strings(out)
	return out
}

func ek_anahtarlar(m map[string]any) string {
	var k []string
	for a := range m {
		k = append(k, a)
	}
	sort.Strings(k)
	return strings.Join(k, ",")
}

func TestListe_BayiYoneticiEklentisiniVeEnvanterAyrintisiniGormez(t *testing.T) {
	if u, ok := lisans.YerelBul("whitelabel"); !ok || !u.SadeceAdmin {
		t.Fatalf("ön koşul: katalogda whitelabel SadeceAdmin olmalı (ok=%v)", ok)
	}
	admin, bayi, musteri := ek_tokenlar(t)
	yl, _ := ek_liste(t, admin)

	bl, bgovde := ek_liste(t, bayi)
	if got := strings.Join(ek_adlar(bl), ","); got != "calistirici,mail" {
		t.Errorf("bayi yalnız girebileceği eklentileri görmeli (calistirici,mail), gelen %s", got)
	}
	for _, iz := range []string{"whitelabel", "Marka", "9.9.9", "3.4.1", "2.0.1", "saglikli", "saglksiz", "/mail-sunucu", "/uygulamalar"} {
		if strings.Contains(bgovde, iz) {
			t.Errorf("bayi yanıtında envanter ayrıntısı %q var: %s", iz, bgovde)
		}
	}
	// Ön yüzün okuduğu alanlar korunur: ad + aktif (mail aktif=0, calistirici aktif=1).
	for _, e := range bl {
		want := e["ad"] == "calistirici"
		if e["aktif"] != want {
			t.Errorf("bayi: %v aktif=%v olmalı, gelen %v", e["ad"], want, e["aktif"])
		}
		if ek_anahtarlar(e) != ek_anahtarlar(yl[0]) {
			t.Errorf("anahtar kümesi değişmemeli: bayi %s / yönetici %s", ek_anahtarlar(e), ek_anahtarlar(yl[0]))
		}
	}

	ml, mgovde := ek_liste(t, musteri)
	if got := strings.Join(ek_adlar(ml), ","); got != "mail" {
		t.Errorf("müşteri yalnız musteri_erisim=1 olanı görmeli (mail), gelen %s", got)
	}
	if strings.Contains(mgovde, "3.4.1") || strings.Contains(mgovde, "saglikli") {
		t.Errorf("müşteri yanıtında envanter ayrıntısı var: %s", mgovde)
	}
}

// Yönetici davranışı DEĞİŞMEDİ: tam liste, tam alanlar.
func TestListe_YoneticiTamEnvanteriGorur(t *testing.T) {
	admin, _, _ := ek_tokenlar(t)
	l, govde := ek_liste(t, admin)
	if got := strings.Join(ek_adlar(l), ","); got != "calistirici,mail,whitelabel" {
		t.Fatalf("yönetici tüm eklentileri görmeli, gelen %s", got)
	}
	for _, iz := range []string{"Marka", "9.9.9", "3.4.1", "saglksiz", "/marka", `"musteri_erisim":true`} {
		if !strings.Contains(govde, iz) {
			t.Errorf("yönetici yanıtında %q olmalı: %s", iz, govde)
		}
	}
}
