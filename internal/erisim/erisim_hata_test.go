package erisim

// erisim_hata_test.go — bayi kapsamı: erişim kısıtlama uçlarında (ayar kaydet,
// kural ekle/sil) vhost render hatası. kisaHata yalnız İLK "/etc/nginx"
// sonrasını kesiyordu; ondan ÖNCE gelen sunucu geneli ssl_stapling uyarıları
// (komşu kiracının alan adı + /etc/pki/girginospanel/<başka-alan>/… yolu) bayiye
// ve müşteriye dönüyordu — hem yanıtta hem de işlemi yapana /denetim'de
// gösterilen denetim kaydı ayrıntısında. Yöneticinin yanıt metni birebir aynı.
//
// Dış etkili adım (erisimUygula) dikişle sahte — ana makinenin nginx'ine
// dokunulmaz. DB sahtedb (Exec argümanları kaydedilir). Gerçek RequireAuth.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/auth"
	"girginospanel/internal/middleware"
	"girginospanel/internal/sahtedb"
)

var er_sir = []byte("erisim-kapsam-test-anahtari-0123456789abc")

func er_tokenlar(t *testing.T) map[string]string {
	t.Helper()
	admin, err := auth.IssueAt(er_sir, 3600, 1, "root", "admin", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	bayi, err := auth.IssueResellerAt(er_sir, 3600, 7, "bayi7", 7, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	musteri, _, err := auth.GenerateMusteri(er_sir, auth.MusteriClaims{
		FTPHesapID: 420, DomainID: 42, Kullanici: "m", AlanAdi: "bayialan.test"}, 3600)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{"admin": admin, "bayi": bayi, "müşteri": musteri}
}

// Gerçek `nginx -t` çıktısı biçiminde; stapling uyarısı "/etc/nginx"ten ÖNCE gelir.
var er_ham = fmt.Errorf("nginx -t başarısız: %s: %w",
	`nginx: [warn] "ssl_stapling" ignored, no OCSP responder URL in the certificate "/etc/pki/girginospanel/komsu-kok.test/komsu-kok.test.crt"
nginx: [emerg] unknown directive "fastcgi_cahe" in /etc/nginx/conf.d/dom_c_komsubayi.conf:12
nginx: configuration file /etc/nginx/nginx.conf test failed`,
	errors.New("exit status 1"))

var er_izler = []string{"komsu-kok.test", "c_komsubayi", "/etc/pki", "/etc/nginx"}

type er_durum struct {
	ad, yontem, yol, desen, govde, onek string
	isle                                func(*Handlers) http.HandlerFunc
}

var er_durumlar = []er_durum{
	{ad: "Kaydet", yontem: http.MethodPut, yol: "/domains/42/erisim", desen: "/domains/{id}/erisim",
		govde: `{"aktif":true,"varsayilan":"red"}`, onek: "ayar uygulanamadı, önceki durum geri yüklendi: ",
		isle: func(h *Handlers) http.HandlerFunc { return h.Kaydet }},
	{ad: "KuralEkle", yontem: http.MethodPost, yol: "/domains/42/erisim/kural", desen: "/domains/{id}/erisim/kural",
		govde: `{"tip":"red","cidr":"203.0.113.9"}`, onek: "kural uygulanamadı, eklenmedi: ",
		isle: func(h *Handlers) http.HandlerFunc { return h.KuralEkle }},
	{ad: "KuralSil", yontem: http.MethodDelete, yol: "/domains/42/erisim/kural/9", desen: "/domains/{id}/erisim/kural/{kid}",
		onek: "kural silinemedi, geri yüklendi: ",
		isle: func(h *Handlers) http.HandlerFunc { return h.KuralSil }},
}

func er_cagir(t *testing.T, d er_durum, tok string) (*sahtedb.DB, int, string) {
	t.Helper()
	eski := erisimUygula
	t.Cleanup(func() { erisimUygula = eski })
	erisimUygula = func(*sql.DB, int64) error { return er_ham }

	sd, db := sahtedb.Yeni()
	sd.Satirlar("SELECT sistem_kullanici FROM domains", []any{"c_bayialan"})
	sd.Satirlar("SELECT tip, cidr, aciklama, sira FROM domain_erisim_kurallari",
		[]any{"red", "203.0.113.9", "", int64(10)})
	r := chi.NewRouter()
	r.With(middleware.RequireAuth(er_sir)).Method(d.yontem, d.desen, d.isle(&Handlers{DB: db}))
	req := httptest.NewRequest(d.yontem, d.yol, strings.NewReader(d.govde))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return sd, rec.Code, rec.Body.String()
}

func er_hataMetni(t *testing.T, govde string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(govde), &m); err != nil {
		t.Fatalf("yanıt çözülemedi: %v (%s)", err, govde)
	}
	for _, k := range []string{"error", "hata", "mesaj"} {
		if s, ok := m[k].(string); ok {
			return s
		}
	}
	t.Fatalf("yanıtta hata metni yok: %s", govde)
	return ""
}

// Yanıt: bayi/müşteri genel mesaj (önek korunur), yabancı iz yok; yönetici metni
// öncekiyle (kisaHata) birebir aynı.
func TestErisimRenderHatasi_BayiMusteriyeHamCiktiDonmez(t *testing.T) {
	tok := er_tokenlar(t)
	for _, d := range er_durumlar {
		t.Run(d.ad, func(t *testing.T) {
			for _, rol := range []string{"bayi", "müşteri"} {
				_, kod, govde := er_cagir(t, d, tok[rol])
				if kod != http.StatusInternalServerError {
					t.Fatalf("%s: 500 beklenirdi: %d %s", rol, kod, govde)
				}
				for _, iz := range er_izler {
					if strings.Contains(govde, iz) {
						t.Errorf("%s yanıtında yabancı iz %q var: %s", rol, iz, govde)
					}
				}
				if m := er_hataMetni(t, govde); !strings.HasPrefix(m, d.onek) || !strings.Contains(m, "günlüğüne") {
					t.Errorf("%s: genel mesaj (%q …) beklenirdi: %q", rol, d.onek, m)
				}
			}
			_, kod, govde := er_cagir(t, d, tok["admin"])
			if kod != http.StatusInternalServerError {
				t.Fatalf("yönetici: 500 beklenirdi: %d %s", kod, govde)
			}
			if m, want := er_hataMetni(t, govde), d.onek+kisaHata(er_ham); m != want {
				t.Errorf("yönetici metni birebir aynı kalmalı:\n  got: %q\n want: %q", m, want)
			}
		})
	}
}

// Denetim kaydı ayrıntısı işlemi yapana /denetim'de aynen gösterilir: bayinin
// başarısız ayar kaydının ayrıntısında komşu izi olmamalı; yönetici eyleminde
// ham ayrıntı (önceki davranış) korunur.
func TestErisimKaydet_DenetimAyrintisindaKomsuIziYok(t *testing.T) {
	tok := er_tokenlar(t)
	d := er_durumlar[0]
	for _, rol := range []string{"bayi", "müşteri"} {
		sd, _, _ := er_cagir(t, d, tok[rol])
		if !sd.Yurutuldu("INSERT INTO audit_log") {
			t.Fatalf("%s: denetim kaydı yazılmalı", rol)
		}
		for _, iz := range er_izler {
			if sd.Yurutuldu(iz) {
				t.Errorf("%s: denetim kaydı ayrıntısında yabancı iz %q var", rol, iz)
			}
		}
	}
	sd, _, _ := er_cagir(t, d, tok["admin"])
	if !sd.Yurutuldu("komsu-kok.test") {
		t.Error("yönetici eyleminin denetim ayrıntısı ham kalmalı (önceki davranış)")
	}
}
