package backups

// hazirlik_hata_test.go — YK-02: geri yükleme ve içerik uçları, yedeğin
// YÖNETİCİNİN sistem geneli uzak hedefinden indirilmesi başarısız olunca ham
// lftp çıktısını (kök deposunun host adı, dizin şeması) bayiye/müşteriye
// döndürmez; yönetici metni birebir aynı görür. Hazırlık dikişle (yedekHazirla)
// sahteye çevrilir — ağa/lftp'ye çıkılmaz. DB sahtedb.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/auth"
	"girginospanel/internal/middleware"
	"girginospanel/internal/sahtedb"
)

var hh_sir = []byte("backups-hazirlik-test-anahtari-0123456789")

// 181'de lftp 4.9.2 ile sahte FTP'ye karşı alınan gerçek çıktılar (denetim kanıtı).
var (
	hh_lftpDizin = errors.New("uzaktan indirme: cd: Access failed: 550 Failed to change directory. (/admin-yedek-deposu/sunucu181/2026-09-24)")
	hh_lftpHost  = errors.New("uzaktan indirme: open: yedek-deposu.gecersiz-ornek.invalid: Name or service not known")
)

var hh_yabanciIzler = []string{"admin-yedek-deposu", "sunucu181", "yedek-deposu.gecersiz-ornek", "Access failed", "Name or service"}

type hh_uc struct {
	ad, metot, yol string
	satir          []any
	kayit          func(r chi.Router, h *Handlers)
}

var hh_uclar = []hh_uc{
	{
		ad: "geri yükle", metot: http.MethodPost, yol: "/domains/42/backups/5/geriyukle",
		satir: []any{"c_bayialan", "bayialan.test", int64(0), "c_bayialan-20260924-010101.tar.gz"},
		kayit: func(r chi.Router, h *Handlers) { r.Post("/domains/{id}/backups/{bid}/geriyukle", h.Restore) },
	},
	{
		ad: "içerik", metot: http.MethodGet, yol: "/domains/42/backups/5/icerik",
		satir: []any{"c_bayialan", "c_bayialan-20260924-010101.tar.gz"},
		kayit: func(r chi.Router, h *Handlers) { r.Get("/domains/{id}/backups/{bid}/icerik", h.Icerik) },
	},
}

func hh_cagir(t *testing.T, uc hh_uc, tok string, hazirHata error) (int, string) {
	t.Helper()
	eski := yedekHazirla
	t.Cleanup(func() { yedekHazirla = eski })
	yedekHazirla = func(context.Context, *sql.DB, string, string) (string, error) { return "", hazirHata }
	sd, db := sahtedb.Yeni()
	sd.Satirlar("FROM backups b", uc.satir)
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(hh_sir))
		uc.kayit(r, &Handlers{DB: db})
	})
	var govde *strings.Reader
	if uc.metot == http.MethodPost {
		govde = strings.NewReader(`{"mod":"tam"}`)
	} else {
		govde = strings.NewReader("")
	}
	req := httptest.NewRequest(uc.metot, uc.yol, govde)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func hh_tokenlar(t *testing.T) (admin, bayi, musteri string) {
	t.Helper()
	var err error
	if admin, err = auth.IssueAt(hh_sir, 3600, 1, "root", "admin", 0, ""); err != nil {
		t.Fatal(err)
	}
	if bayi, err = auth.IssueResellerAt(hh_sir, 3600, 7, "bayi7", 7, 0, ""); err != nil {
		t.Fatal(err)
	}
	if musteri, _, err = auth.GenerateMusteri(hh_sir, auth.MusteriClaims{FTPHesapID: 420, DomainID: 42, Kullanici: "m", AlanAdi: "bayialan.test"}, 3600); err != nil {
		t.Fatal(err)
	}
	return
}

func hh_hata(govde string) string {
	var m map[string]string
	_ = json.Unmarshal([]byte(govde), &m)
	return m["hata"]
}

func TestHazirlikHatasi_HamLftpCiktisiBayiMusteriyeDonmez(t *testing.T) {
	admin, bayi, musteri := hh_tokenlar(t)
	for _, uc := range hh_uclar {
		for _, ham := range []error{hh_lftpDizin, hh_lftpHost} {
			for rol, tok := range map[string]string{"bayi": bayi, "müşteri": musteri} {
				kod, govde := hh_cagir(t, uc, tok, ham)
				if kod != http.StatusNotFound {
					t.Fatalf("%s / %s: 404 beklenirdi: %d %s", uc.ad, rol, kod, govde)
				}
				for _, iz := range hh_yabanciIzler {
					if strings.Contains(govde, iz) {
						t.Errorf("%s / %s: kökün uzak deposunun izi %q yanıtta: %s", uc.ad, rol, iz, govde)
					}
				}
				if !strings.Contains(hh_hata(govde), "günlüğüne") {
					t.Errorf("%s / %s: genel mesaj beklenirdi: %s", uc.ad, rol, govde)
				}
			}
			// Yönetici: metin öncekiyle BİREBİR aynı.
			kod, govde := hh_cagir(t, uc, admin, ham)
			if kod != http.StatusNotFound || hh_hata(govde) != ham.Error() {
				t.Errorf("%s: yönetici ham ayrıntıyı aynen görmeli: %d %s", uc.ad, kod, govde)
			}
		}
	}
}

// Kullanıcıya anlamlı ve sunucuya dair ayrıntı taşımayan tek hazırlık hatası
// ("diskte bulunamadı") bayiye aynen gider — mesaj körleşmez.
func TestHazirlikHatasi_DiskteYokMesajiKorunur(t *testing.T) {
	_, bayi, _ := hh_tokenlar(t)
	for _, uc := range hh_uclar {
		kod, govde := hh_cagir(t, uc, bayi, errYedekDiskteYok)
		if kod != http.StatusNotFound || hh_hata(govde) != "yedek dosyası diskte bulunamadı" {
			t.Errorf("%s: 'yedek dosyası diskte bulunamadı' aynen dönmeli: %d %s", uc.ad, kod, govde)
		}
	}
}
