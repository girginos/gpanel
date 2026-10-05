package subdomain

// hata_kapsam_test.go — bayi kapsamı (son ham hata yerleri): alt alan uçlarında
// SUNUCU GENELİ komut çıktısı (`nginx -t` / `httpd -t`: komşu kiracıların
// sertifika ve vhost yolları; acme.sh/openssl/systemctl: sunucunun ACME hesap
// dizini ve servis durumu) bayiye ve müşteriye ham DÖNMEZ; yöneticinin gördüğü
// metin birebir aynıdır. Denetim kaydı (işlemi yapana /denetim'de gösterilir)
// bayi/müşteri eyleminde ham çıktı taşımaz.
//
// Dış etkili adımlar (vhostYenidenKur, komut, acmeKoku) dikişlerle sahte; ev
// dizini ve nginx conf.d geçici dizine çevrilir — test ana makinenin /home,
// /etc/nginx dizinlerine, nginx'ine, acme.sh'ine dokunmaz. DB sahtedb.

import (
	"context"
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

var hk_sir = []byte("subdomain-hata-kapsam-test-anahtari-0123")

func hk_tokenlar(t *testing.T) map[string]string {
	t.Helper()
	admin, err := auth.IssueAt(hk_sir, 3600, 1, "root", "admin", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	bayi, err := auth.IssueResellerAt(hk_sir, 3600, 7, "bayi7", 7, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	musteri, _, err := auth.GenerateMusteri(hk_sir, auth.MusteriClaims{
		FTPHesapID: 420, DomainID: 42, Kullanici: "m", AlanAdi: "bayialan.test"}, 3600)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{"admin": admin, "bayi": bayi, "müşteri": musteri}
}

// Sunucu geneli `nginx -t` çıktısı: ssl_stapling uyarısı her SSL'li alan adı
// için basılır; küresel bozulmada komşunun vhost yolu görünür.
const hk_nginxT = `nginx: [warn] "ssl_stapling" ignored, no OCSP responder URL in the certificate "/etc/pki/girginospanel/komsu-kok.test/komsu-kok.test.crt"
nginx: [emerg] unknown directive "fastcgi_cahe" in /etc/nginx/conf.d/dom_c_komsubayi.conf:12
nginx: configuration file /etc/nginx/nginx.conf test failed`

// Sunucu geneli `httpd -t` çıktısı (apache backend'li alt alan): komşu bayinin
// alt alan vhost yolu. "/etc/nginx" İÇERMEZ — eski kisaHata kesmiyordu.
const hk_httpdT = `httpd doğrulanamadı: AH00526: Syntax error on line 7 of /etc/httpd/conf.d/sub_apache_c_komsubayi_magaza.conf:
Invalid command 'SetHandlr', perhaps misspelled or defined by a module not included in the server configuration`

// Sunucu geneli acme.sh çıktısı: panelin ACME hesap dizini ve hesap parmak izi.
const hk_acme = `[Thu Sep 25 07:10:01 UTC 2026] Using CA: https://acme-v02.api.letsencrypt.org/directory
[Thu Sep 25 07:10:02 UTC 2026] ACCOUNT_THUMBPRINT='kokHesapParmakIzi0123'
[Thu Sep 25 07:10:03 UTC 2026] Reload error for : '/var/lib/girginospanel/acme/komsu-kok.test'`

// Bayi/müşteri yanıtında (ve denetim kaydında) GÖRÜNMEMESİ gereken izler.
var hk_izler = []string{"komsu-kok.test", "c_komsubayi", "/etc/pki", "/etc/nginx", "/etc/httpd",
	"kokHesapParmakIzi", "/var/lib/girginospanel"}

// Alt alan vhost yeniden kurulum hatası: nginx -t + httpd -t izleri birlikte.
var hk_ham = fmt.Errorf("%s\n%s: %w", hk_httpdT, hk_nginxT, errors.New("exit status 1"))

// hk_dikisler — dış etkili adımları sahteye çevirir; ev dizini ve nginx conf.d
// geçici dizine taşınır. kur: vhost yeniden kurulumunun döneceği hata;
// komutHata: ad → (çıktı, hata) — listede olmayan komut başarılı sayılır.
func hk_dikisler(t *testing.T, kur error, komutHata map[string]string) *[]string {
	t.Helper()
	e1, e2, e3, e4, e5 := vhostYenidenKur, komut, acmeKoku, evKoku, nginxConfD
	t.Cleanup(func() { vhostYenidenKur, komut, acmeKoku, evKoku, nginxConfD = e1, e2, e3, e4, e5 })
	kok := t.TempDir()
	evKoku, nginxConfD = kok+"/home", kok
	calisan := &[]string{}
	vhostYenidenKur = func(context.Context, *sql.DB, int64, string, string, string, string) error { return kur }
	acmeKoku = func() string { return kok + "/acme" }
	komut = func(ad string, arg ...string) ([]byte, error) {
		*calisan = append(*calisan, ad)
		if cikti, ok := komutHata[ad]; ok {
			return []byte(cikti + "\n"), errors.New("exit status 1")
		}
		return nil, nil
	}
	return calisan
}

// hk_db — alt alan uçlarının okuduğu satırlar (üst alan + alt alan 5).
func hk_db() (*sahtedb.DB, *sql.DB) {
	sd, db := sahtedb.Yeni()
	sd.Satirlar("SELECT sistem_kullanici, alan_adi, COALESCE(php_surum,'8.3'), COALESCE(is_demo,0) FROM domains",
		[]any{"c_bayialan", "bayialan.test", "8.3", int64(0)})
	sd.Satirlar("SELECT alt_ad, tam_ad, COALESCE(php_surum,'8.3') FROM subdomanlar",
		[]any{"blog", "blog.bayialan.test", "8.3"})
	sd.Satirlar("SELECT alt_ad, tam_ad, php_surum FROM subdomanlar",
		[]any{"blog", "blog.bayialan.test", "8.3"})
	sd.Satirlar("SELECT alt_ad, tam_ad FROM subdomanlar", []any{"blog", "blog.bayialan.test"})
	sd.Satirlar("SELECT tam_ad, COALESCE(php_surum,'8.3') FROM subdomanlar", []any{"blog.bayialan.test", "8.3"})
	sd.Satirlar("SELECT tip, cidr, aciklama, sira FROM subdomain_erisim_kurallari",
		[]any{"izin", "203.0.113.45/32", "", int64(10)})
	return sd, db
}

type hk_uc struct {
	ad, yontem, desen, yol, govde string
	isle                          func(*Handlers) http.HandlerFunc
}

func hk_cagir(t *testing.T, u hk_uc, tok string) (int, string, *sahtedb.DB) {
	t.Helper()
	sd, db := hk_db()
	r := chi.NewRouter()
	r.With(middleware.RequireAuth(hk_sir)).Method(u.yontem, u.desen, u.isle(&Handlers{DB: db}))
	req := httptest.NewRequest(u.yontem, u.yol, strings.NewReader(u.govde))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String(), sd
}

func hk_hataMetni(t *testing.T, govde string) string {
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

func hk_izYok(t *testing.T, neresi, metin string) {
	t.Helper()
	for _, iz := range hk_izler {
		if strings.Contains(metin, iz) {
			t.Errorf("%s: yabancı / sunucu geneli iz %q var: %s", neresi, iz, metin)
		}
	}
}

// hk_rolDongusu — bayi ve müşteri genel mesaj (önek korunur) görür, iz yok;
// yönetici metni want ile BİREBİR aynı.
func hk_rolDongusu(t *testing.T, u hk_uc, kod int, onek, want string, hazirla func()) {
	t.Helper()
	tok := hk_tokenlar(t)
	for _, rol := range []string{"bayi", "müşteri"} {
		hazirla()
		k, govde, _ := hk_cagir(t, u, tok[rol])
		if k != kod {
			t.Fatalf("%s: %d beklenirdi: %d %s", rol, kod, k, govde)
		}
		hk_izYok(t, rol+" yanıtı", govde)
		if m := hk_hataMetni(t, govde); !strings.HasPrefix(m, onek) || !strings.Contains(m, "günlüğüne") {
			t.Errorf("%s: genel mesaj (%q … günlüğüne …) beklenirdi: %q", rol, onek, m)
		}
	}
	hazirla()
	k, govde, _ := hk_cagir(t, u, tok["admin"])
	if k != kod {
		t.Fatalf("yönetici: %d beklenirdi: %d %s", kod, k, govde)
	}
	if m := hk_hataMetni(t, govde); m != want {
		t.Errorf("yönetici metni birebir aynı kalmalı:\n  got: %q\n want: %q", m, want)
	}
}

var (
	hk_phpDegistir = hk_uc{ad: "PHPDegistir", yontem: http.MethodPut,
		desen: "/domains/{id}/subdomain/{sid}/php", yol: "/domains/42/subdomain/5/php",
		govde: `{"php_surum":"8.3"}`, isle: func(h *Handlers) http.HandlerFunc { return h.PHPDegistir }}
	hk_phpAyarPut = hk_uc{ad: "PHPAyarPut", yontem: http.MethodPut,
		desen: "/domains/{id}/subdomain/{sid}/php-settings", yol: "/domains/42/subdomain/5/php-settings",
		govde: `{"ayarlar":{}}`, isle: func(h *Handlers) http.HandlerFunc { return h.PHPAyarPut }}
	hk_sslKurSelf = hk_uc{ad: "SSLKur", yontem: http.MethodPost,
		desen: "/domains/{id}/subdomain/{sid}/ssl", yol: "/domains/42/subdomain/5/ssl",
		govde: `{"tip":"self-signed"}`, isle: func(h *Handlers) http.HandlerFunc { return h.SSLKur }}
	hk_sslKurLE = hk_uc{ad: "SSLKurLE", yontem: http.MethodPost,
		desen: "/domains/{id}/subdomain/{sid}/ssl", yol: "/domains/42/subdomain/5/ssl",
		govde: `{"tip":"letsencrypt"}`, isle: func(h *Handlers) http.HandlerFunc { return h.SSLKur }}
	hk_sslKaldir = hk_uc{ad: "SSLKaldir", yontem: http.MethodDelete,
		desen: "/domains/{id}/subdomain/{sid}/ssl", yol: "/domains/42/subdomain/5/ssl",
		isle: func(h *Handlers) http.HandlerFunc { return h.SSLKaldir }}
	hk_olustur = hk_uc{ad: "Olustur", yontem: http.MethodPost,
		desen: "/domains/{id}/subdomain", yol: "/domains/42/subdomain",
		govde: `{"alt_ad":"yeni","php_surum":"8.3"}`, isle: func(h *Handlers) http.HandlerFunc { return h.Olustur }}
	hk_erisimKaydet = hk_uc{ad: "ErisimKaydet", yontem: http.MethodPut,
		desen: "/domains/{id}/subdomain/{sid}/erisim", yol: "/domains/42/subdomain/5/erisim",
		govde: `{"aktif":true,"varsayilan":"red"}`, isle: func(h *Handlers) http.HandlerFunc { return h.ErisimKaydet }}
	hk_kuralEkle = hk_uc{ad: "ErisimKuralEkle", yontem: http.MethodPost,
		desen: "/domains/{id}/subdomain/{sid}/erisim/kural", yol: "/domains/42/subdomain/5/erisim/kural",
		govde: `{"tip":"izin","cidr":"198.51.100.7"}`, isle: func(h *Handlers) http.HandlerFunc { return h.ErisimKuralEkle }}
	hk_kuralSil = hk_uc{ad: "ErisimKuralSil", yontem: http.MethodDelete,
		desen: "/domains/{id}/subdomain/{sid}/erisim/kural/{kid}", yol: "/domains/42/subdomain/5/erisim/kural/9",
		isle: func(h *Handlers) http.HandlerFunc { return h.ErisimKuralSil }}
)

// Vhost yeniden kurulum hatası (php-fpm havuzu + `httpd -t` + `nginx -t` +
// reload): PHP sürümü, PHP ayarları, SSL kur/kaldır uçları.
func TestAltAlanVhostHatasi_BayiMusteriyeHamCiktiDonmez_YoneticiAyni(t *testing.T) {
	for _, u := range []hk_uc{hk_phpDegistir, hk_phpAyarPut, hk_sslKurSelf, hk_sslKaldir} {
		t.Run(u.ad, func(t *testing.T) {
			hk_rolDongusu(t, u, http.StatusInternalServerError, "", hk_ham.Error(),
				func() { hk_dikisler(t, hk_ham, nil) })
		})
	}
}

// SSL kurulumunun kendi komutları: acme.sh --install-cert ve openssl
// (acme.sh --issue artık sınıflanır: acme_test.go)
// çıktısı (sunucunun ACME hesap dizini, hesap parmak izi, reload çıktısı).
func TestAltAlanSSLKomutHatasi_BayiMusteriyeHamCiktiDonmez_YoneticiAyni(t *testing.T) {
	durumlar := []struct {
		ad        string
		u         hk_uc
		komutHata map[string]string
		kod       int
		onek      string
	}{
		{"openssl", hk_sslKurSelf, map[string]string{"openssl": hk_acme}, http.StatusInternalServerError, "openssl: "},
	}
	for _, d := range durumlar {
		t.Run(d.ad, func(t *testing.T) {
			hk_rolDongusu(t, d.u, d.kod, d.onek, d.onek+hk_acme,
				func() { hk_dikisler(t, nil, d.komutHata) })
		})
	}
	// install-cert: --issue başarılı, yerleştirme (reloadcmd) başarısız.
	t.Run("acme-install-cert", func(t *testing.T) {
		hazirla := func() {
			hk_dikisler(t, nil, nil)
			ilk := true
			komut = func(ad string, arg ...string) ([]byte, error) {
				if ad != "/root/.acme.sh/acme.sh" {
					return nil, nil
				}
				if ilk { // --issue
					ilk = false
					return nil, nil
				}
				return []byte(hk_acme + "\n"), errors.New("exit status 1")
			}
		}
		hk_rolDongusu(t, hk_sslKurLE, http.StatusInternalServerError, "cert yerleştirilemedi: ",
			"cert yerleştirilemedi: "+hk_acme, hazirla)
	})
}

// Alt alan oluşturma: ilk vhost yazımından sonra SUNUCU GENELİ `nginx -t`.
func TestAltAlanOlustur_NginxT_BayiMusteriyeHamCiktiDonmez_YoneticiAyni(t *testing.T) {
	hk_rolDongusu(t, hk_olustur, http.StatusInternalServerError, "nginx doğrulanamadı: ",
		"nginx doğrulanamadı: "+hk_nginxT, func() { hk_dikisler(t, nil, map[string]string{"nginx": hk_nginxT}) })
}

// Erişim kısıtlama uçları: kisaHata yalnız İLK "/etc/nginx" sonrasını kesiyordu;
// `httpd -t` satırı (komşunun /etc/httpd yolu) ve öncesi aynen dönüyordu.
// Yönetici eski kisaHata metnini birebir görür.
func TestAltAlanErisim_RenderHatasi_BayiMusteriyeHamCiktiDonmez_YoneticiAyni(t *testing.T) {
	durumlar := []struct {
		u    hk_uc
		onek string
	}{
		{hk_erisimKaydet, "ayar uygulanamadı, önceki durum geri yüklendi: "},
		{hk_kuralEkle, "kural uygulanamadı, eklenmedi: "},
		{hk_kuralSil, "kural silinemedi, geri yüklendi: "},
	}
	for _, d := range durumlar {
		t.Run(d.u.ad, func(t *testing.T) {
			hk_rolDongusu(t, d.u, http.StatusInternalServerError, d.onek, d.onek+kisaHata(hk_ham),
				func() { hk_dikisler(t, hk_ham, nil) })
		})
	}
}

// Erişim ayarı başarısızlığının DENETİM KAYDI: bayi/müşteri eyleminde ham çıktı
// yok; yönetici eyleminde ham ayrıntı aynen kalır.
func TestAltAlanErisim_DenetimKaydi_BayiMusteriEylemindeHamYok(t *testing.T) {
	tok := hk_tokenlar(t)
	for _, rol := range []string{"bayi", "müşteri", "admin"} {
		hk_dikisler(t, hk_ham, nil)
		_, _, sd := hk_cagir(t, hk_erisimKaydet, tok[rol])
		if !sd.Yurutuldu("audit_log") {
			t.Fatalf("%s: denetim kaydı yazılmadı", rol)
		}
		if rol == "admin" {
			// JSON içinde (detail {"not": ...}) — satır sonu kaçışlı; yol parçası yeter.
			if !sd.Yurutuldu("sub_apache_c_komsubayi_magaza.conf") {
				t.Errorf("yönetici eyleminin denetim kaydı ham ayrıntıyı korumalı")
			}
			continue
		}
		for _, iz := range hk_izler {
			if sd.Yurutuldu(iz) {
				t.Errorf("%s eyleminin denetim kaydında yabancı iz %q var", rol, iz)
			}
		}
	}
}

// Negatif kontrol dikişi: dikişler gerçekten devrede — hiçbir dış komut, sahte
// dışında çalışmıyor (ev dizini geçici).
func TestAltAlanDikisler_AnaMakineyeDokunmaz(t *testing.T) {
	calisan := hk_dikisler(t, hk_ham, nil)
	tok := hk_tokenlar(t)
	_, _, _ = hk_cagir(t, hk_sslKurSelf, tok["bayi"])
	if len(*calisan) == 0 {
		t.Fatalf("komut dikişi devrede değil (openssl sahteye gitmedi)")
	}
	if !strings.HasPrefix(sslDir("c_bayialan"), evKoku) || evKoku == "/home" {
		t.Fatalf("ev dizini dikişi devrede değil: %s", sslDir("c_bayialan"))
	}
}
