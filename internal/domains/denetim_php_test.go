package domains

// denetim_php_test.go — denetim kaydı (audit_log) + php_surum doğrulaması.
//
// Kapsam (gerçek MariaDB; GOSP_TEST_GECICI_DSN yoksa ATLANIR — adalani_create_test.go
// şeması üstüne audit_log + domains kolonları eklenir):
//
//	PUT    /domains/{id}/php               SetPHP        → hosting.php_surum
//	POST   /domains/{id}/ssl/issue         SSLIssue      → ssl.kur + ssl.kur_sonuc (asenkron)
//	POST   /domains/{id}/ssl/dns-yenile    SSLDnsYenile  → ssl.dns_yenile
//	DELETE /domains/{id}/ssl               SSLDisable    → ssl.kaldir
//	POST   /domains                        Create        → php_surum doğrulaması (400)
//
// Her uçta: eylem kodu, hedef (alan adı), kapsam (domains.reseller_id — İŞLEMİ YAPANIN
// değil domainin sahibi), aktör, başarı/başarısızlık bayrağı ve detayda sır
// (sertifika yolu, ham araç çıktısı, sistem kullanıcısı) OLMADIĞI doğrulanır.
//
// Dış etkili her adım (php havuzu, per-tenant birim, nginx/ssl) SAHTE dikişlerle
// değişir; ana makineye dokunulmaz.

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/hesaplar"
	"girginospanel/internal/middleware"
	"girginospanel/internal/provisioner"
)

// dnp_db: aa_db şeması + SetPHP/SSL'in okuduğu domains kolonları + audit_log.
//
//	domains 10 kokdomain.test (kök, rid 0) · 20 bayi2.test (bayi 2) · 30 bayi3.test (bayi 3)
//	hepsi: php 8.2, ssl_aktif=1, sertifika yolları dolu, sistem kullanıcısı c_t<id>
func dnp_db(t *testing.T) *sql.DB {
	t.Helper()
	db := aa_db(t)
	aa_exec(t, db, `ALTER TABLE domains
		ADD COLUMN php_surum VARCHAR(16) NOT NULL DEFAULT '8.3',
		ADD COLUMN is_demo TINYINT NOT NULL DEFAULT 0,
		ADD COLUMN web_backend VARCHAR(16) NULL,
		ADD COLUMN ssl_aktif TINYINT NOT NULL DEFAULT 0,
		ADD COLUMN ssl_kaynak VARCHAR(32) NOT NULL DEFAULT '',
		ADD COLUMN cert_path VARCHAR(255) NOT NULL DEFAULT '',
		ADD COLUMN key_path VARCHAR(255) NOT NULL DEFAULT '',
		ADD COLUMN ssl_bitis DATETIME NULL`)
	aa_exec(t, db, `CREATE TABLE audit_log (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
		ts TIMESTAMP NULL DEFAULT current_timestamp(), actor_user_id BIGINT UNSIGNED NULL,
		actor_username VARCHAR(64) NOT NULL DEFAULT '', adina_domain_id BIGINT UNSIGNED NULL,
		ip VARCHAR(45) NOT NULL DEFAULT '', action VARCHAR(64) NOT NULL,
		target VARCHAR(255) NOT NULL DEFAULT '', detail LONGTEXT NULL,
		ok TINYINT(1) NOT NULL DEFAULT 1, reseller_id BIGINT UNSIGNED NOT NULL DEFAULT 0)`)
	aa_exec(t, db, `UPDATE domains SET sistem_kullanici=CONCAT('c_t', id), php_surum='8.2', ssl_aktif=1,
		ssl_kaynak='letsencrypt', cert_path='/etc/ssl/gizli/x.crt', key_path='/etc/ssl/gizli/x.key'`)
	return db
}

func dnp_router(h *Handlers) http.Handler {
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(aa_sir))
		r.Post("/domains", h.Create)
		r.Put("/domains/{id}/php", h.SetPHP)
		r.Post("/domains/{id}/ssl/issue", h.SSLIssue)
		r.Post("/domains/{id}/ssl/dns-yenile", h.SSLDnsYenile)
		r.Delete("/domains/{id}/ssl", h.SSLDisable)
	})
	return r
}

func dnp_istek(t *testing.T, h *Handlers, yontem, yol, tok, govde string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(yontem, yol, bytes.NewReader([]byte(govde)))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	dnp_router(h).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// ── denetim okuma yardımcıları ───────────────────────────────────────────────

type dnp_kayit struct {
	Eylem, Hedef, Not, Aktor, Ham string
	Ok                            int
	Kapsam                        int64
}

func dnp_kayitlar(t *testing.T, db *sql.DB, eylem string) []dnp_kayit {
	t.Helper()
	rows, err := db.Query(`SELECT action, target, COALESCE(detail,''), actor_username, ok, reseller_id
		FROM audit_log WHERE action=? ORDER BY id`, eylem)
	if err != nil {
		t.Fatalf("audit_log okunamadı: %v", err)
	}
	defer rows.Close()
	var out []dnp_kayit
	for rows.Next() {
		var k dnp_kayit
		if err := rows.Scan(&k.Eylem, &k.Hedef, &k.Ham, &k.Aktor, &k.Ok, &k.Kapsam); err != nil {
			t.Fatal(err)
		}
		var m map[string]string
		_ = json.Unmarshal([]byte(k.Ham), &m)
		k.Not = m["not"]
		out = append(out, k)
	}
	return out
}

func dnp_kayitSayisi(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	return aa_sayi(t, db, `SELECT COUNT(*) FROM audit_log`)
}

// dnp_kayitBekle: asenkron denetim kaydı (SSL sonucu) için en çok 5 sn bekler.
func dnp_kayitBekle(t *testing.T, db *sql.DB, eylem string, n int) []dnp_kayit {
	t.Helper()
	son := time.Now().Add(5 * time.Second)
	for {
		k := dnp_kayitlar(t, db, eylem)
		if len(k) >= n {
			return k
		}
		if time.Now().After(son) {
			t.Fatalf("%s kaydı %d adet beklendi, gelen %d", eylem, n, len(k))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// dnp_tek: eylem için TAM BİR kayıt olmalı ve alanları beklenenle aynı olmalı.
func dnp_tek(t *testing.T, db *sql.DB, eylem, hedef, not string, ok int, kapsam int64, aktor string) dnp_kayit {
	t.Helper()
	k := dnp_kayitlar(t, db, eylem)
	if len(k) != 1 {
		t.Fatalf("%s: tam 1 denetim kaydı beklenir, alınan %d: %+v", eylem, len(k), k)
	}
	g := k[0]
	if g.Hedef != hedef {
		t.Errorf("%s: hedef %q, beklenen %q", eylem, g.Hedef, hedef)
	}
	if g.Not != not {
		t.Errorf("%s: detay %q, beklenen %q", eylem, g.Not, not)
	}
	if g.Ok != ok {
		t.Errorf("%s: ok=%d, beklenen %d", eylem, g.Ok, ok)
	}
	if g.Kapsam != kapsam {
		t.Errorf("%s: kapsam (reseller_id) %d, beklenen %d (domainin sahibi)", eylem, g.Kapsam, kapsam)
	}
	if g.Aktor != aktor {
		t.Errorf("%s: aktör %q, beklenen %q", eylem, g.Aktor, aktor)
	}
	// Sır sızıntısı: sertifika/anahtar yolu, sistem kullanıcısı, ham araç çıktısı.
	for _, s := range []string{"/etc/", ".key", ".crt", "c_t", "nginx", "emerg", "privkey"} {
		if strings.Contains(g.Ham, s) {
			t.Errorf("%s: denetim detayında sır/iç ayrıntı (%q) var: %s", eylem, s, g.Ham)
		}
	}
	return g
}

func dnp_phpSurumu(t *testing.T, db *sql.DB, id int64) string {
	t.Helper()
	var s string
	if err := db.QueryRow(`SELECT php_surum FROM domains WHERE id=?`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

// ── SetPHP dikişleri ─────────────────────────────────────────────────────────

type dnp_php struct {
	uyg         []string // phpSurumUygula çağrıları: "<alan>|<sk>|<surum>"
	tenant      []string // tenantFPMUygula çağrıları: "<id>|<sk>|<surum>"
	uygHata     error
	tenantAktif bool
	tenantHata  error
	uygIcinde   func() // sahte uygulama sırasında çalışır
}

func dnp_phpDikis(t *testing.T) *dnp_php {
	t.Helper()
	f := &dnp_php{}
	e1, e2, e3 := phpSurumUygula, tenantFPMAktif, tenantFPMUygula
	t.Cleanup(func() { phpSurumUygula, tenantFPMAktif, tenantFPMUygula = e1, e2, e3 })
	phpSurumUygula = func(alan, sk, surum, cert, key, kaynak, backend string) (string, error) {
		f.uyg = append(f.uyg, alan+"|"+sk+"|"+surum)
		if f.uygIcinde != nil {
			f.uygIcinde()
		}
		if f.uygHata != nil {
			return "", f.uygHata
		}
		return "/run/php-fpm/" + sk + ".sock", nil
	}
	tenantFPMAktif = func(string) bool { return f.tenantAktif }
	tenantFPMUygula = func(_ *sql.DB, id int64, sk, surum string) (string, error) {
		f.tenant = append(f.tenant, fmt.Sprintf("%d|%s|%s", id, sk, surum))
		if f.tenantHata != nil {
			return "", f.tenantHata
		}
		return "/run/php-fpm/tenant-" + sk + ".sock", nil
	}
	return f
}

// Geçersiz php_surum: 400 + desteklenen liste; HİÇBİR yan etki (havuz, birim, DB, denetim).
func TestSetPHP_GecersizSurum400_YanEtkisiz(t *testing.T) {
	db := dnp_db(t)
	f := dnp_phpDikis(t)
	h := &Handlers{DB: db}
	for _, c := range []struct{ ad, govde, mesaj string }{
		{"olmayan 8.30", `{"php_surum":"8.30"}`, "8.6"},
		{"paket adı + olmayan sürüm", `{"php_surum":"php8.9"}`, "8.6"},
		{"gelecek 9.0", `{"php_surum":"9.0"}`, "8.6"},
		{"iki haneli küçük sürüm", `{"php_surum":"8.33"}`, "8.6"},
		{"enjeksiyon", `{"php_surum":"8.3;id"}`, "8.6"},
		{"boş", `{"php_surum":""}`, "zorunlu"},
		{"yalnız boşluk", `{"php_surum":"   "}`, "zorunlu"},
		{"alan yok", `{}`, "zorunlu"},
	} {
		kod, govde := dnp_istek(t, h, http.MethodPut, "/domains/20/php", aa_yonetici(t), c.govde)
		if kod != http.StatusBadRequest {
			t.Errorf("%s: 400 beklenir, alınan %d %s", c.ad, kod, govde)
		}
		if !strings.Contains(govde, c.mesaj) {
			t.Errorf("%s: yanıt %q içermeli: %s", c.ad, c.mesaj, govde)
		}
	}
	if len(f.uyg) != 0 || len(f.tenant) != 0 {
		t.Errorf("geçersiz istekte havuz/birim çağrıldı: uyg=%v tenant=%v", f.uyg, f.tenant)
	}
	if s := dnp_phpSurumu(t, db, 20); s != "8.2" {
		t.Errorf("domains.php_surum değişti: %q", s)
	}
	if n := dnp_kayitSayisi(t, db); n != 0 {
		t.Errorf("reddedilen istek denetim kaydı yazdı: %d", n)
	}
}

// Başarı: sürüm yazılır, TEK denetim kaydı — kapsam domainin sahibi, aktör istek sahibi.
func TestSetPHP_Basari_DenetimKaydi(t *testing.T) {
	for _, c := range []struct {
		ad     string
		tok    func(*testing.T) string
		id     string
		alan   string
		kapsam int64
		aktor  string
	}{
		{"yönetici → bayi2 domaini", aa_yonetici, "20", "bayi2.test", 2, "root"},
		{"bayi2 kendi domaini", func(t *testing.T) string { return aa_bayi(t, 2) }, "20", "bayi2.test", 2, "bayi2"},
		{"yönetici → kök domain", aa_yonetici, "10", "kokdomain.test", 0, "root"},
	} {
		t.Run(c.ad, func(t *testing.T) {
			db := dnp_db(t)
			f := dnp_phpDikis(t)
			h := &Handlers{DB: db}
			kod, govde := dnp_istek(t, h, http.MethodPut, "/domains/"+c.id+"/php", c.tok(t), `{"php_surum":" 8.4 "}`)
			if kod != http.StatusOK {
				t.Fatalf("200 beklenir: %d %s", kod, govde)
			}
			if !strings.Contains(govde, `"php_surum":"8.4"`) {
				t.Errorf("yanıt kanonik sürümü taşımalı: %s", govde)
			}
			if len(f.uyg) != 1 || !strings.HasSuffix(f.uyg[0], "|8.4") || len(f.tenant) != 0 {
				t.Errorf("paylaşılan havuz tek kez 8.4 ile çağrılmalı: uyg=%v tenant=%v", f.uyg, f.tenant)
			}
			var id int64
			fmt.Sscan(c.id, &id)
			if s := dnp_phpSurumu(t, db, id); s != "8.4" {
				t.Errorf("domains.php_surum = %q, beklenen kırpılmış 8.4", s)
			}
			dnp_tek(t, db, "hosting.php_surum", c.alan, "eski=8.2 yeni=8.4", 1, c.kapsam, c.aktor)
		})
	}
}

// Havuz uygulaması düşerse: 500, DB değişmez, ok=0 kayıt (ham hata detaya GİRMEZ).
func TestSetPHP_UygulamaHatasi_BasarisizKayit(t *testing.T) {
	db := dnp_db(t)
	f := dnp_phpDikis(t)
	f.uygHata = errors.New("nginx -t: emerg /etc/nginx/conf.d/c_t20.conf:12 privkey.pem")
	h := &Handlers{DB: db}
	kod, govde := dnp_istek(t, h, http.MethodPut, "/domains/20/php", aa_yonetici(t), `{"php_surum":"8.4"}`)
	if kod != http.StatusInternalServerError {
		t.Fatalf("500 beklenir: %d %s", kod, govde)
	}
	if s := dnp_phpSurumu(t, db, 20); s != "8.2" {
		t.Errorf("başarısız uygulamada DB değişmemeli: %q", s)
	}
	dnp_tek(t, db, "hosting.php_surum", "bayi2.test", "eski=8.2 yeni=8.4", 0, 2, "root")
}

// DB güncellemesi düşerse (havuz uygulanmış): 500 + ok=0 kayıt.
func TestSetPHP_DBYazimHatasi_BasarisizKayit(t *testing.T) {
	db := dnp_db(t)
	f := dnp_phpDikis(t)
	f.uygIcinde = func() { // eski sürüm okunduktan SONRA UPDATE'i bozar
		aa_exec(t, db, `ALTER TABLE domains RENAME COLUMN php_surum TO php_x`)
	}
	h := &Handlers{DB: db}
	kod, govde := dnp_istek(t, h, http.MethodPut, "/domains/20/php", aa_yonetici(t), `{"php_surum":"8.4"}`)
	if kod != http.StatusInternalServerError {
		t.Fatalf("500 beklenir: %d %s", kod, govde)
	}
	dnp_tek(t, db, "hosting.php_surum", "bayi2.test", "eski=8.2 yeni=8.4", 0, 2, "root")
}

// Per-tenant FPM kurulu hesapta SetPHP, UI yolu (php-settings) gibi birimi yeni sürümle
// yeniden kurar; paylaşılan havuz yazılmaz (sahipsiz havuz + eski birim ayrışması).
func TestSetPHP_TenantFPM_BirimeYonlendirir(t *testing.T) {
	t.Run("başarı", func(t *testing.T) {
		db := dnp_db(t)
		f := dnp_phpDikis(t)
		f.tenantAktif = true
		h := &Handlers{DB: db}
		kod, govde := dnp_istek(t, h, http.MethodPut, "/domains/20/php", aa_yonetici(t), `{"php_surum":"8.4"}`)
		if kod != http.StatusOK {
			t.Fatalf("200 beklenir: %d %s", kod, govde)
		}
		if len(f.tenant) != 1 || f.tenant[0] != "20|c_t20|8.4" {
			t.Errorf("per-tenant birim (id, sk, sürüm) ile çağrılmalı: %v", f.tenant)
		}
		if len(f.uyg) != 0 {
			t.Errorf("per-tenant hesapta paylaşılan havuz yazılmamalı: %v", f.uyg)
		}
		if !strings.Contains(govde, "tenant-c_t20.sock") {
			t.Errorf("yanıt per-tenant soketi taşımalı: %s", govde)
		}
		if s := dnp_phpSurumu(t, db, 20); s != "8.4" {
			t.Errorf("php_surum = %q", s)
		}
		dnp_tek(t, db, "hosting.php_surum", "bayi2.test", "eski=8.2 yeni=8.4", 1, 2, "root")
	})
	t.Run("birim hatası", func(t *testing.T) {
		db := dnp_db(t)
		f := dnp_phpDikis(t)
		f.tenantAktif = true
		f.tenantHata = errors.New("systemctl: emerg")
		h := &Handlers{DB: db}
		kod, _ := dnp_istek(t, h, http.MethodPut, "/domains/20/php", aa_yonetici(t), `{"php_surum":"8.4"}`)
		if kod != http.StatusInternalServerError {
			t.Fatalf("500 beklenir: %d", kod)
		}
		if len(f.uyg) != 0 {
			t.Errorf("hata yolunda da paylaşılan havuz yazılmamalı: %v", f.uyg)
		}
		if s := dnp_phpSurumu(t, db, 20); s != "8.2" {
			t.Errorf("birim düşünce DB değişmemeli: %q", s)
		}
		dnp_tek(t, db, "hosting.php_surum", "bayi2.test", "eski=8.2 yeni=8.4", 0, 2, "root")
	})
}

// Askıdaki hesap: 409, hiçbir şeye dokunulmadı → denetim kaydı YOK; 404/demo da kayıtsız.
func TestSetPHP_Askida404Demo_KayitYazmaz(t *testing.T) {
	for _, tenant := range []bool{false, true} {
		t.Run(fmt.Sprintf("askı tenant=%v", tenant), func(t *testing.T) {
			db := dnp_db(t)
			f := dnp_phpDikis(t)
			f.tenantAktif = tenant
			f.uygHata = fmt.Errorf("havuz: %w", hesaplar.ErrHesapAskida)
			f.tenantHata = f.uygHata
			h := &Handlers{DB: db}
			kod, govde := dnp_istek(t, h, http.MethodPut, "/domains/20/php", aa_yonetici(t), `{"php_surum":"8.4"}`)
			if kod != http.StatusConflict {
				t.Fatalf("409 beklenir: %d %s", kod, govde)
			}
			if n := dnp_kayitSayisi(t, db); n != 0 {
				t.Errorf("askıda denetim kaydı yazılmamalı: %d", n)
			}
			if s := dnp_phpSurumu(t, db, 20); s != "8.2" {
				t.Errorf("php_surum değişti: %q", s)
			}
		})
	}
	t.Run("bilinmeyen domain", func(t *testing.T) {
		db := dnp_db(t)
		f := dnp_phpDikis(t)
		h := &Handlers{DB: db}
		kod, _ := dnp_istek(t, h, http.MethodPut, "/domains/999/php", aa_yonetici(t), `{"php_surum":"8.4"}`)
		if kod != http.StatusNotFound || len(f.uyg)+len(f.tenant) != 0 || dnp_kayitSayisi(t, db) != 0 {
			t.Errorf("404, yan etkisiz ve kayıtsız olmalı: kod=%d uyg=%v tenant=%v", kod, f.uyg, f.tenant)
		}
	})
	t.Run("demo", func(t *testing.T) {
		db := dnp_db(t)
		f := dnp_phpDikis(t)
		aa_exec(t, db, `UPDATE domains SET is_demo=1 WHERE id=20`)
		h := &Handlers{DB: db}
		kod, _ := dnp_istek(t, h, http.MethodPut, "/domains/20/php", aa_yonetici(t), `{"php_surum":"8.4"}`)
		if kod != http.StatusForbidden || len(f.uyg)+len(f.tenant) != 0 || dnp_kayitSayisi(t, db) != 0 {
			t.Errorf("403, yan etkisiz ve kayıtsız olmalı: kod=%d", kod)
		}
	})
}

// ── Create: php_surum doğrulaması ────────────────────────────────────────────

func TestCreate_PHPSurumDogrulama(t *testing.T) {
	db := aa_db(t)
	// domain_id: hostinge ÖZEL plan sütunu (üretim şeması); kök plan kapısı okur.
	aa_exec(t, db, `ALTER TABLE service_plans ADD COLUMN domain_id BIGINT UNSIGNED NULL`)
	aa_exec(t, db, `INSERT INTO service_plans (id, ad, php_surum) VALUES (101,'gecerli','8.4'), (102,'bozuk','8.30'), (103,'bos','')`)
	var cagri []string // sağlamaya giden php sürümleri
	e1, e2 := saglama, saglamaGeriAl
	t.Cleanup(func() { saglama, saglamaGeriAl = e1, e2 })
	saglama = func(alan, php string) (*provisioner.Result, error) {
		cagri = append(cagri, php)
		return nil, errors.New("sahte sağlama: durduruldu") // 500; INSERT'e hiç varılmaz
	}
	saglamaGeriAl = func(string, string) error { return nil }
	h := &Handlers{DB: db, IPv4: "203.0.113.10"}
	once := aa_sayi(t, db, `SELECT COUNT(*) FROM domains`)
	n := 0
	istek := func(php string, plan int) (int, string) {
		n++
		govde := map[string]any{"alan_adi": fmt.Sprintf("php-dogrula-%d.test", n)}
		if php != "-" {
			govde["php_surum"] = php
		}
		if plan > 0 {
			govde["plan_id"] = plan
		}
		b, _ := json.Marshal(govde)
		return dnp_istek(t, h, http.MethodPost, "/domains", aa_yonetici(t), string(b))
	}

	// Geçersiz: 400, sağlamaya HİÇ varılmaz (useradd/nginx yok), mesaj listeyi söyler.
	for _, v := range []string{"8.30", "php8.9", "9.9", "8.33", "8,3", "8.3;rm -rf /", "../8.3"} {
		cagri = nil
		kod, govde := istek(v, 0)
		if kod != http.StatusBadRequest || !strings.Contains(govde, "7.4") || !strings.Contains(govde, "8.6") {
			t.Errorf("php_surum=%q: 400 + desteklenen liste beklenir: %d %s", v, kod, govde)
		}
		if len(cagri) != 0 {
			t.Errorf("php_surum=%q sağlamaya ulaştı: %v", v, cagri)
		}
	}
	// İstemci değeri plan değerine üstündür: geçersiz istemci değeri plan geçerli olsa da 400.
	cagri = nil
	if kod, _ := istek("8.30", 101); kod != http.StatusBadRequest || len(cagri) != 0 {
		t.Errorf("geçersiz istemci değeri + geçerli plan: 400 beklenir (kod=%d, sağlama=%v)", kod, cagri)
	}

	// Geçerli / boş / plan: sağlamaya GİDEN sürüm.
	for _, c := range []struct {
		ad, php string
		plan    int
		bekle   string
	}{
		{"açık 8.2", "8.2", 0, "8.2"},
		{"kırpılır", " 8.4 ", 0, "8.4"},
		{"alan yok → varsayılan", "-", 0, "8.3"},
		{"boş → varsayılan", "", 0, "8.3"},
		{"boş + geçerli plan → plan", "", 101, "8.4"},
		{"alan yok + geçerli plan → plan", "-", 101, "8.4"},
		{"boş + BOZUK plan değeri → 8.3 (ham yazılmaz)", "", 102, "8.3"},
		{"boş + boş plan değeri → 8.3", "", 103, "8.3"},
		{"istemci + plan → istemci", "8.2", 101, "8.2"},
		{"WHMCS yazımı php81 → 8.1", "php81", 0, "8.1"},
		{"WHMCS yazımı PHP 8.2 → 8.2", "PHP 8.2", 0, "8.2"},
		{"yama sürümü 8.4.12 → 8.4", "8.4.12", 0, "8.4"},
	} {
		cagri = nil
		kod, govde := istek(c.php, c.plan)
		if kod != http.StatusInternalServerError || !strings.Contains(govde, "sağlama başarısız") {
			t.Errorf("%s: sahte sağlama hatası (500) beklenir: %d %s", c.ad, kod, govde)
			continue
		}
		if len(cagri) != 1 || cagri[0] != c.bekle {
			t.Errorf("%s: sağlamaya %q gitmeli, gidenler: %v", c.ad, c.bekle, cagri)
		}
	}
	if sonra := aa_sayi(t, db, `SELECT COUNT(*) FROM domains`); sonra != once {
		t.Errorf("domains satır sayısı değişti %d → %d", once, sonra)
	}
}

// ── SSL ──────────────────────────────────────────────────────────────────────

// dnp_sertifika: diske PEM yazar. kendinden=true → issuer==subject (Let's Encrypt DEĞİL);
// false → bir CA'nın imzaladığı yaprak sertifika (gerçek CA gibi: issuer≠subject).
func dnp_sertifika(t *testing.T, kendinden bool) string {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	simdi := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Test CA"},
		NotBefore: simdi.Add(-time.Hour), NotAfter: simdi.Add(90 * 24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	if !kendinden {
		yaprakKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		yaprak := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "bayi2.test"},
			NotBefore: simdi.Add(-time.Hour), NotAfter: simdi.Add(90 * 24 * time.Hour), DNSNames: []string{"bayi2.test"}}
		if der, err = x509.CreateCertificate(rand.Reader, yaprak, ca, &yaprakKey.PublicKey, caKey); err != nil {
			t.Fatal(err)
		}
	}
	yol := filepath.Join(t.TempDir(), "sert.crt")
	if err := os.WriteFile(yol, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return yol
}

func TestSSLIssue_Denetim(t *testing.T) {
	sert := func(yol string) func(string, string, string, string) (string, string, error) {
		return func(string, string, string, string) (string, string, error) { return yol, yol + ".key", nil }
	}
	for _, c := range []struct {
		ad      string
		govde   string
		kur     func(t *testing.T)
		kurNot  string // ssl.kur detayı
		sonuc   string // ssl.kur_sonuc detayı
		sonucOk int
	}{
		{"self-signed başarı", `{"tip":"self-signed"}`, func(t *testing.T) {},
			"tip=self-signed", "tip=self-signed durum=tamam", 1},
		{"varsayılan tip = self-signed", `{}`, func(t *testing.T) {},
			"tip=self-signed", "tip=self-signed durum=tamam", 1},
		{"let's encrypt başarı (CA imzalı)", `{"tip":"letsencrypt"}`, func(t *testing.T) { sslLetsEncrypt = sert(dnp_sertifika(t, false)) },
			"tip=letsencrypt", "tip=letsencrypt durum=tamam", 1},
		{"let's encrypt + posta SSL", `{"tip":"letsencrypt","mail_ssl":true}`, func(t *testing.T) { sslLetsEncrypt = sert(dnp_sertifika(t, false)) },
			"tip=letsencrypt mail_ssl=true", "tip=letsencrypt durum=tamam", 1},
		{"let's encrypt alınamadı → kendinden imzalı yedek BAŞARISIZ sayılır", `{"tip":"letsencrypt"}`,
			func(t *testing.T) { sslLetsEncrypt = sert(dnp_sertifika(t, true)) },
			"tip=letsencrypt", "tip=letsencrypt durum=kendinden-imzali-yedek", 0},
		{"sertifika adımı hatası", `{"tip":"letsencrypt"}`,
			func(t *testing.T) {
				sslLetsEncrypt = func(string, string, string, string) (string, string, error) {
					return "", "", errors.New("acme: /etc/letsencrypt/live/x/privkey.pem emerg nginx")
				}
			},
			"tip=letsencrypt", "tip=letsencrypt durum=hata", 0},
	} {
		t.Run(c.ad, func(t *testing.T) {
			si_dikisler(t)
			si_temizle(t, 20)
			c.kur(t)
			db := dnp_db(t)
			h := &Handlers{DB: db}
			kod, govde := dnp_istek(t, h, http.MethodPost, "/domains/20/ssl/issue", aa_bayi(t, 2), c.govde)
			if kod != http.StatusAccepted {
				t.Fatalf("202 beklenir: %d %s", kod, govde)
			}
			si_isBekle(t, 20)
			dnp_kayitBekle(t, db, "ssl.kur_sonuc", 1)
			dnp_tek(t, db, "ssl.kur", "bayi2.test", c.kurNot, 1, 2, "bayi2")
			dnp_tek(t, db, "ssl.kur_sonuc", "bayi2.test", c.sonuc, c.sonucOk, 2, "bayi2")
		})
	}

	t.Run("kayıt adımı (DB) hatası → sonuç hata", func(t *testing.T) {
		si_dikisler(t)
		si_temizle(t, 20)
		db := dnp_db(t)
		aa_exec(t, db, `ALTER TABLE domains DROP COLUMN ssl_bitis`) // sonuç UPDATE'i düşer
		h := &Handlers{DB: db}
		if kod, govde := dnp_istek(t, h, http.MethodPost, "/domains/20/ssl/issue", aa_yonetici(t), `{"tip":"self-signed"}`); kod != http.StatusAccepted {
			t.Fatalf("202 beklenir: %d %s", kod, govde)
		}
		si_isBekle(t, 20)
		dnp_kayitBekle(t, db, "ssl.kur_sonuc", 1)
		dnp_tek(t, db, "ssl.kur", "bayi2.test", "tip=self-signed", 1, 2, "root")
		dnp_tek(t, db, "ssl.kur_sonuc", "bayi2.test", "tip=self-signed durum=hata", 0, 2, "root")
	})

	// Kabul edilmeyen istekler iş başlatmaz → kayıt yazmaz.
	t.Run("reddedilen istekler kayıtsız", func(t *testing.T) {
		si_dikisler(t)
		si_temizle(t, 20)
		si_temizle(t, 10)
		db := dnp_db(t)
		aa_exec(t, db, `UPDATE domains SET is_demo=1 WHERE id=30`)
		h := &Handlers{DB: db}
		for _, r := range []struct {
			ad, yol, govde string
			kod            int
		}{
			{"geçersiz tip", "/domains/20/ssl/issue", `{"tip":"wildcard"}`, http.StatusBadRequest},
			{"bozuk gövde", "/domains/20/ssl/issue", `{`, http.StatusBadRequest},
			{"bilinmeyen domain", "/domains/999/ssl/issue", `{"tip":"self-signed"}`, http.StatusNotFound},
			{"demo", "/domains/30/ssl/issue", `{"tip":"self-signed"}`, http.StatusForbidden},
		} {
			if kod, govde := dnp_istek(t, h, http.MethodPost, r.yol, aa_yonetici(t), r.govde); kod != r.kod {
				t.Errorf("%s: %d beklenir, alınan %d %s", r.ad, r.kod, kod, govde)
			}
		}
		// Zaten sürüyor: 409.
		sslMu.Lock()
		sslIsleri[10] = &sslIsi{DomainID: 10, Durum: "calisiyor"}
		sslMu.Unlock()
		if kod, _ := dnp_istek(t, h, http.MethodPost, "/domains/10/ssl/issue", aa_yonetici(t), `{"tip":"self-signed"}`); kod != http.StatusConflict {
			t.Errorf("süren iş: 409 beklenir, alınan %d", kod)
		}
		if n := dnp_kayitSayisi(t, db); n != 0 {
			t.Errorf("reddedilen isteklerde denetim kaydı yazılmamalı: %d", n)
		}
	})
}

func TestSSLDnsYenile_Denetim(t *testing.T) {
	db := dnp_db(t)
	h := &Handlers{DB: db}
	if kod, govde := dnp_istek(t, h, http.MethodPost, "/domains/20/ssl/dns-yenile", aa_yonetici(t), ``); kod != http.StatusOK {
		t.Fatalf("200 beklenir: %d %s", kod, govde)
	}
	dnp_tek(t, db, "ssl.dns_yenile", "bayi2.test", "", 1, 2, "root")
	if kod, _ := dnp_istek(t, h, http.MethodPost, "/domains/999/ssl/dns-yenile", aa_yonetici(t), ``); kod != http.StatusNotFound {
		t.Errorf("bilinmeyen domain: 404 beklenir, alınan %d", kod)
	}
	if n := dnp_kayitSayisi(t, db); n != 1 {
		t.Errorf("404 kayıt yazmamalı: toplam %d", n)
	}
}

func TestSSLDisable_Denetim(t *testing.T) {
	kapatDikis := func(t *testing.T, hata error, icinde func()) *[]string {
		t.Helper()
		var cagri []string
		eski := sslKapat
		t.Cleanup(func() { sslKapat = eski })
		sslKapat = func(alan, sk, php, backend string) error {
			cagri = append(cagri, alan)
			if icinde != nil {
				icinde()
			}
			return hata
		}
		return &cagri
	}
	aktif := func(t *testing.T, db *sql.DB, id int64) int64 {
		return aa_sayi(t, db, `SELECT ssl_aktif FROM domains WHERE id=?`, id)
	}

	t.Run("başarı", func(t *testing.T) {
		db := dnp_db(t)
		cagri := kapatDikis(t, nil, nil)
		h := &Handlers{DB: db}
		if kod, govde := dnp_istek(t, h, http.MethodDelete, "/domains/20/ssl", aa_bayi(t, 2), ``); kod != http.StatusOK {
			t.Fatalf("200 beklenir: %d %s", kod, govde)
		}
		if len(*cagri) != 1 || aktif(t, db, 20) != 0 {
			t.Errorf("SSL kapatılmalı: çağrı=%v ssl_aktif=%d", *cagri, aktif(t, db, 20))
		}
		dnp_tek(t, db, "ssl.kaldir", "bayi2.test", "", 1, 2, "bayi2")
	})
	t.Run("kapatma hatası", func(t *testing.T) {
		db := dnp_db(t)
		kapatDikis(t, errors.New("nginx -t emerg /etc/nginx/c_t20.conf"), nil)
		h := &Handlers{DB: db}
		kod, _ := dnp_istek(t, h, http.MethodDelete, "/domains/20/ssl", aa_yonetici(t), ``)
		if kod != http.StatusInternalServerError || aktif(t, db, 20) != 1 {
			t.Errorf("500 ve ssl_aktif=1 kalmalı: kod=%d aktif=%d", kod, aktif(t, db, 20))
		}
		dnp_tek(t, db, "ssl.kaldir", "bayi2.test", "", 0, 2, "root")
	})
	t.Run("DB güncelleme hatası", func(t *testing.T) {
		db := dnp_db(t)
		kapatDikis(t, nil, func() { aa_exec(t, db, `ALTER TABLE domains DROP COLUMN ssl_bitis`) })
		h := &Handlers{DB: db}
		if kod, _ := dnp_istek(t, h, http.MethodDelete, "/domains/20/ssl", aa_yonetici(t), ``); kod != http.StatusInternalServerError {
			t.Fatalf("500 beklenir: %d", kod)
		}
		dnp_tek(t, db, "ssl.kaldir", "bayi2.test", "", 0, 2, "root")
	})
	t.Run("bilinmeyen domain ve demo kayıtsız", func(t *testing.T) {
		db := dnp_db(t)
		cagri := kapatDikis(t, nil, nil)
		aa_exec(t, db, `UPDATE domains SET is_demo=1 WHERE id=30`)
		h := &Handlers{DB: db}
		k1, _ := dnp_istek(t, h, http.MethodDelete, "/domains/999/ssl", aa_yonetici(t), ``)
		k2, _ := dnp_istek(t, h, http.MethodDelete, "/domains/30/ssl", aa_yonetici(t), ``)
		if k1 != http.StatusNotFound || k2 != http.StatusForbidden || len(*cagri) != 0 || dnp_kayitSayisi(t, db) != 0 {
			t.Errorf("404/403, yan etkisiz ve kayıtsız olmalı: %d %d çağrı=%v", k1, k2, *cagri)
		}
	})
}
