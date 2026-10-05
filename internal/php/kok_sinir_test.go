package php

// kok_sinir_test.go — PUT/GET /domains/{id}/php-settings kök sınırı (kok_sinir.go):
//   - müşteri ve bayi kökün iç alanlarını (efektiflimit.PHPAyarIcAnahtarlari) yazamaz:
//     anahtar VARSA (null/aynı değer, harf katlaması dahil) 403, hiçbir şey yazılmaz;
//   - izolasyon alanlarını (disable_functions, open_basedir, …) değiştiremez (403),
//     aynı değeri geri gönderebilir; değerler kayıttan korunur;
//   - GET yanıtında iç alanlar müşteri/bayiye dönmez, yöneticiye döner;
//   - paylaşılan havuz plan tavanıyla render edilir (yönetici değeri de aşamaz).
// Dış etkili adımlar sahte (pa_ dikişleri gibi); DB sahtedb.

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/auth"
	"girginospanel/internal/efektiflimit"
	"girginospanel/internal/middleware"
	"girginospanel/internal/phpsurum"
	"girginospanel/internal/sahtedb"
)

var ks_sir = []byte("php-kok-sinir-test-anahtari-0123456789ab")

func ks_tokenlar(t *testing.T) map[string]string {
	t.Helper()
	admin, err := auth.IssueAt(ks_sir, 3600, 1, "root", "admin", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	bayi, err := auth.IssueResellerAt(ks_sir, 3600, 7, "bayi7", 7, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	musteri, _, err := auth.GenerateMusteri(ks_sir, auth.MusteriClaims{FTPHesapID: 420, DomainID: 42, Kullanici: "m", AlanAdi: "kok.test"}, 3600)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{"admin": admin, "bayi": bayi, "müşteri": musteri}
}

// ks_kayit — domain 42'nin kayıtlı php_settings satırı (Get sırasıyla).
var ks_kayit = []any{"256M", int64(30), int64(60), "64M", "32M", int64(1), "exec,system",
	int64(0), int64(1), int64(1), int64(1), int64(0), "E_ALL", ".:/usr/share/php", "", "", "",
	"dynamic", int64(6), int64(500), int64(2), int64(1), int64(3), "", int64(0)}

// ks_cagir — PUT ya da GET; paylaşılan havuz yolu (tenant=false). havuz: havuza
// gönderilen (render edilecek) ayarlar; nil = havuz çağrılmadı.
func ks_cagir(t *testing.T, yontem, tok, govde string) (int, string, *sahtedb.DB, *Settings) {
	t.Helper()
	e1, e2, e3, e4 := tenantFPMAktif, tenantFPMUygula, havuzUygula, vhostUygula
	t.Cleanup(func() { tenantFPMAktif, tenantFPMUygula, havuzUygula, vhostUygula = e1, e2, e3, e4 })
	var havuz *Settings
	tenantFPMAktif = func(string) bool { return false }
	tenantFPMUygula = func(*sql.DB, int64, string, string) (string, error) { return "/run/sahte.sock", nil }
	havuzUygula = func(_ string, _ string, s Settings) (string, error) { havuz = &s; return "/run/sahte.sock", nil }
	vhostUygula = func(*sql.DB, int64, string, string) error { return nil }
	e5 := tumSurumler
	t.Cleanup(func() { tumSurumler = e5 })
	tumSurumler = func() []phpsurum.Surum { return nil }

	sd, db := sahtedb.Yeni()
	sd.Satirlar("SELECT sistem_kullanici, php_surum, is_demo FROM domains", []any{"c_kok", "8.3", int64(0)})
	sd.Satirlar("SELECT alan_adi, sistem_kullanici, php_surum FROM domains", []any{"kok.test", "c_kok", "8.3"})
	sd.Satirlar("FROM php_settings WHERE domain_id", ks_kayit)
	h := &Handlers{DB: db}
	r := chi.NewRouter()
	r.With(middleware.RequireAuth(ks_sir)).Put("/domains/{id}/php-settings", h.PutAyarlar)
	r.With(middleware.RequireAuth(ks_sir)).Get("/domains/{id}/php-settings", h.GetAyarlar)
	req := httptest.NewRequest(yontem, "/domains/42/php-settings", strings.NewReader(govde))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String(), sd, havuz
}

func TestPHPAyar_KokIcAlani_MusteriBayi403_DegerKorunur(t *testing.T) {
	tok := ks_tokenlar(t)
	govdeler := []string{
		`{"ayarlar":{"pm_max_children":500}}`,
		`{"ayarlar":{"pm_max_children":6}}`, // kayıttaki AYNI değer de ret (kâhin yok)
		`{"ayarlar":{"pm_start_servers":null}}`,
		`{"ayarlar":{"PM_MAX_SPARE_SERVERS":3}}`,
		`{"ayarlar":{"pm_ſtart_servers":9}}`, // ſ → s: encoding/json katlaması
		`{"php_surum":"8.3","ayarlar":{"memory_limit":"512M","pm_min_spare_servers":1}}`,
	}
	for _, rol := range []string{"müşteri", "bayi"} {
		for _, g := range govdeler {
			kod, govde, sd, havuz := ks_cagir(t, http.MethodPut, tok[rol], g)
			if kod != http.StatusForbidden || !strings.Contains(govde, "yöneticinize aittir") {
				t.Errorf("%s %s: 403 beklenirdi: %d %s", rol, g, kod, govde)
			}
			if sd.Yurutuldu("INSERT INTO php_settings") || havuz != nil {
				t.Errorf("%s %s: ret sonrası yazım/havuz olmamalı", rol, g)
			}
		}
	}
}

func TestPHPAyar_KokKorumali_MusteriBayiDegistiremez(t *testing.T) {
	tok := ks_tokenlar(t)
	for _, rol := range []string{"müşteri", "bayi"} {
		for _, g := range []string{
			`{"ayarlar":{"disable_functions":""}}`,
			`{"ayarlar":{"open_basedir":"/"}}`,
			`{"ayarlar":{"session_save_path":"/home/c_komsu/tmp"}}`,
			`{"ayarlar":{"mail_force_extra_parameters":"-X /home/c_kok/x.log"}}`,
			`{"ayarlar":{"Disable_Functions":"exec"}}`,
		} {
			kod, govde, sd, _ := ks_cagir(t, http.MethodPut, tok[rol], g)
			if kod != http.StatusForbidden || sd.Yurutuldu("INSERT INTO php_settings") {
				t.Errorf("%s %s: 403 + yazımsız beklenirdi: %d %s", rol, g, kod, govde)
			}
		}
		if rol == "müşteri" {
			// Müşteri için disable_functions görünmez: aynı değer bile 403 (kâhin yok);
			// gövdede YOKSA kayıt yazılır ve değer kayıttan korunur.
			if kod, govde, sd, _ := ks_cagir(t, http.MethodPut, tok[rol], `{"ayarlar":{"disable_functions":"exec,system"}}`); kod != http.StatusForbidden || sd.Yurutuldu("INSERT INTO php_settings") {
				t.Errorf("müşteri: aynı disable_functions değeri de 403 olmalı: %d %s", kod, govde)
			}
			kod, govde, sd, havuz := ks_cagir(t, http.MethodPut, tok[rol], `{"ayarlar":{"open_basedir":"","memory_limit":"512M","max_execution_time":3000,"pm_strategy":"static"}}`)
			if kod != http.StatusOK || havuz == nil || !sd.Yurutuldu("INSERT INTO php_settings") {
				t.Fatalf("müşteri: disable_functions'sız kayıt 200 beklenirdi: %d %s", kod, govde)
			}
			if !strings.HasPrefix(havuz.DisableFunctions, "exec,system") || !strings.Contains(havuz.DisableFunctions, "pcntl_exec") {
				t.Errorf("müşteri: df kayıttan korunmalı + zorunlu taban: %q", havuz.DisableFunctions)
			}
			continue
		}
		// Görünen değeri geri göndermek serbest; değerler kayıttan korunur.
		kod, govde, sd, havuz := ks_cagir(t, http.MethodPut, tok[rol],
			`{"ayarlar":{"disable_functions":"exec,system","open_basedir":"","memory_limit":"512M","max_execution_time":3000,"pm_strategy":"static"}}`)
		if kod != http.StatusOK || havuz == nil || !sd.Yurutuldu("INSERT INTO php_settings") {
			t.Fatalf("%s: aynı değerle kayıt 200 beklenirdi: %d %s", rol, kod, govde)
		}
		if havuz.MemoryLimit != "512M" || havuz.PMStrategy != "static" {
			t.Errorf("%s: müşterinin yazabildiği alanlar uygulanmalı: %+v", rol, *havuz)
		}
		if !strings.HasPrefix(havuz.DisableFunctions, "exec,system") || !strings.Contains(havuz.DisableFunctions, "pcntl_exec") {
			t.Errorf("%s: df kayıttan korunmalı + zorunlu taban: %q", rol, havuz.DisableFunctions)
		}
		// Plan tavanı: süre sabit tavana, işçi sayısı plana (sahtedb: plan yok → 8).
		if havuz.MaxExecutionTime != 300 || havuz.PMMaxChildren != 8 || havuz.PMStartServers != 2 || havuz.PMMaxSpareServers != 4 {
			t.Errorf("%s: havuz plan tavanıyla render edilmeli: %+v", rol, *havuz)
		}
	}
}

func TestPHPAyar_Yonetici_YazarAmaHavuzPlaniAsamaz(t *testing.T) {
	tok := ks_tokenlar(t)
	kod, govde, sd, havuz := ks_cagir(t, http.MethodPut, tok["admin"],
		`{"ayarlar":{"pm_max_children":500,"pm_start_servers":100,"disable_functions":"","open_basedir":"/home/c_kok/:/tmp/","pm_strategy":"bilinmez"}}`)
	if kod != http.StatusOK || havuz == nil {
		t.Fatalf("yönetici 200 beklenirdi: %d %s", kod, govde)
	}
	if !sd.Yurutuldu("INSERT INTO php_settings") {
		t.Fatal("yönetici kaydı yazılmalı")
	}
	if havuz.PMMaxChildren != 8 || havuz.PMStartServers != 2 || havuz.PMStrategy != "ondemand" {
		t.Errorf("paylaşılan havuz planı aşmamalı: %+v", *havuz)
	}
	if havuz.DisableFunctions == "" || !strings.Contains(havuz.DisableFunctions, "symlink") {
		t.Errorf("boş df'de bile zorunlu taban render edilmeli: %q", havuz.DisableFunctions)
	}
	if havuz.OpenBasedir != "/home/c_kok/:/tmp/" {
		t.Errorf("yöneticinin open_basedir'i korunmalı: %q", havuz.OpenBasedir)
	}
}

func TestPHPAyar_GET_IcAlanMusteriBayiyeDonmez(t *testing.T) {
	tok := ks_tokenlar(t)
	for rol, t2 := range tok {
		kod, govde, _, _ := ks_cagir(t, http.MethodGet, t2, "")
		if kod != http.StatusOK {
			t.Fatalf("%s GET: %d %s", rol, kod, govde)
		}
		var y struct {
			Ayarlar map[string]any `json:"ayarlar"`
		}
		if err := json.Unmarshal([]byte(govde), &y); err != nil {
			t.Fatal(err)
		}
		for _, k := range efektiflimit.PHPAyarIcAnahtarlari() {
			_, var_ := y.Ayarlar[k]
			if rol == "admin" && !var_ {
				t.Errorf("yönetici %s görmeli", k)
			}
			if rol != "admin" && var_ {
				t.Errorf("%s kökün iç alanı %s görmemeli: %v", rol, k, y.Ayarlar[k])
			}
		}
		if _, var_ := y.Ayarlar["disable_functions"]; var_ == (rol == "müşteri") {
			t.Errorf("%s: disable_functions görünürlüğü yanlış (müşteri görmemeli, diğerleri görmeli): %v", rol, y.Ayarlar)
		}
		if rol != "müşteri" && y.Ayarlar["disable_functions"] != "exec,system" || y.Ayarlar["memory_limit"] != "256M" {
			t.Errorf("%s: görünür alanlar dönmeli: %v", rol, y.Ayarlar)
		}
	}
}

// Müşteri ön yüzünün döngüsü: GET'teki (maskeli) ayarları değiştirip geri gönderir.
func TestPHPAyar_MusteriGETGovdesiniGeriGonderir(t *testing.T) {
	tok := ks_tokenlar(t)
	_, govde, _, _ := ks_cagir(t, http.MethodGet, tok["müşteri"], "")
	var y struct {
		Ayarlar map[string]any `json:"ayarlar"`
	}
	if err := json.Unmarshal([]byte(govde), &y); err != nil {
		t.Fatal(err)
	}
	y.Ayarlar["upload_max_filesize"] = "128M"
	b, _ := json.Marshal(map[string]any{"php_surum": "8.3", "ayarlar": y.Ayarlar})
	kod, g, _, havuz := ks_cagir(t, http.MethodPut, tok["müşteri"], string(b))
	if kod != http.StatusOK || havuz == nil || havuz.UploadMaxFilesize != "128M" {
		t.Fatalf("maskeli GET gövdesi geri gönderilebilmeli: %d %s", kod, g)
	}
}

// Ön yüz gövde süzgeci (frontend/src/lib/phpAyar.ts) sunucunun listeleriyle AYNI
// anahtarları taşımalı: kayarsa müşteri ekranı ya 403 alır ya da alanı gösterir.
func TestPHPAyar_OnYuzListesiSunucuylaAyni(t *testing.T) {
	b, err := os.ReadFile("../../frontend/src/lib/phpAyar.ts")
	if err != nil {
		t.Skipf("ön yüz kaynağı yok: %v", err)
	}
	satir := func(ad string) string {
		for _, s := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(s, "export const "+ad+" = ") {
				return s
			}
		}
		t.Fatalf("%s bulunamadı", ad)
		return ""
	}
	liste := func(l []string) string {
		var q []string
		for _, k := range l {
			q = append(q, "'"+k+"'")
		}
		return "[" + strings.Join(q, ", ") + "] as const"
	}
	if got, want := satir("PHP_IC_ANAHTARLAR"), liste(efektiflimit.PHPAyarIcAnahtarlari()); !strings.HasSuffix(got, want) {
		t.Errorf("PHP_IC_ANAHTARLAR kaydı:\n got %s\nwant …%s", got, want)
	}
	if got, want := satir("PHP_KOK_KORUMALI"), liste(kokKorumaliAnahtarlar); !strings.HasSuffix(got, want) {
		t.Errorf("PHP_KOK_KORUMALI kaydı:\n got %s\nwant …%s", got, want)
	}
}
