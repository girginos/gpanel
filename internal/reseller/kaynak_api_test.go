package reseller

// kaynak_api_test.go — bayi kaynak havuzu API'si: GERÇEK kimlik zinciri (RequireAuth + AdminOnly /
// AdminVeyaReseller, main.go ile aynı sarmalama), kapsam (bayi yalnız KENDİ havuzunu görür, kimliği
// istekten veremez), doğrulama, kalıcılık ve kancalar. Sunucuya dokunan eylemler (uzlaştırma,
// grubby) sahtedir (kanca_test.go): hiçbir test gerçek rootflags yazamaz. Geçici MariaDB ister.

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/auth"
	"girginospanel/internal/bayikaynak"
	"girginospanel/internal/middleware"
)

var kaSir = []byte("kaynak-api-test-anahtari-0123456789abcdef")

const (
	kaBayiA = int64(10)
	kaBayiB = int64(11)
	kaBayiC = int64(12) // havuzsuz bayi
)

// kaDB: opGeciciDB + göç 0219'un GERÇEK dosyası (idempotent: tablo zaten sütunlu) + paket/hosting şeması.
func kaDB(t *testing.T) *sql.DB {
	t.Helper()
	db := opGeciciDB(t)
	for _, q := range []string{
		`ALTER TABLE reseller_plans MODIFY id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			ADD COLUMN aciklama VARCHAR(255) NOT NULL DEFAULT '', ADD COLUMN fiyat_kurus BIGINT NOT NULL DEFAULT 0,
			ADD COLUMN varsayilan TINYINT NOT NULL DEFAULT 0, ADD COLUMN created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP`,
		`CREATE TABLE domains (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, alan_adi VARCHAR(253) NOT NULL DEFAULT '',
			sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '', reseller_id BIGINT NOT NULL DEFAULT 0, boyut_kb BIGINT NOT NULL DEFAULT 0)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}
	b, err := os.ReadFile("../../migrations/0219_bayi_kaynak_havuzu.sql")
	if err != nil {
		t.Fatal(err)
	}
	var govde strings.Builder
	for _, ln := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(ln), "--") {
			govde.WriteString(ln + "\n")
		}
	}
	for _, q := range strings.Split(govde.String(), ";") {
		if strings.TrimSpace(q) == "" {
			continue
		}
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("göç: %v\n%s", err, q)
		}
	}
	for _, q := range []string{
		`INSERT INTO users (id, username, role, max_disk_mb, kaynak_cpu_cekirdek, kaynak_ram_mb, kaynak_io_agirlik) VALUES
			(10,'bayia','reseller',5000,2,1024,200), (11,'bayib','reseller',9000,4,2048,0), (12,'bayic','reseller',0,0,0,0),
			(20,'musteri1','user',0,0,0,0)`,
		// A bayisinin son noktası 150 milli, B'ninki 777 milli: sızıntı bu sayılarla yakalanır.
		`INSERT INTO bayi_kaynak_kullanim (bayi_id, ts, cpu_milli, cpu_sinir_milli, ram_mb, ram_sinir_mb, disk_mb, disk_sinir_mb, kaynak)
			VALUES (10, UTC_TIMESTAMP() - INTERVAL 10 MINUTE, 150, 200, 300, 1024, 100, 5000, 'cgroup'),
			       (11, UTC_TIMESTAMP() - INTERVAL 10 MINUTE, 777, 400, 800, 2048, 200, 9000, 'cgroup')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}
	return db
}

// kaYonlendirici: main.go'daki sarmalamanın birebir kopyası (RequireAuth + rota başına rol kapısı).
func kaYonlendirici(h *Handlers) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequireAuth(kaSir))
	a, ar := middleware.AdminOnly, middleware.AdminVeyaReseller
	r.With(a).Get("/bayi-kaynak", h.KaynakListe)
	r.With(a).Get("/bayi-kaynak/disk-kotasi", h.DiskKotasiDurum)
	r.With(a).Post("/bayi-kaynak/disk-kotasi/etkinlestir", h.DiskKotasiEtkinlestir)
	r.With(a).Get("/bayi-kaynak/{id}", h.KaynakDetay)
	r.With(a).Get("/bayi-kaynak/{id}/kullanim", h.KaynakKullanim)
	r.With(a).Post("/bayi-kaynak/{id}/uygula", h.KaynakUygula)
	r.With(ar).Get("/reseller/kaynak", h.KaynakBen)
	r.With(ar).Get("/reseller/kaynak/kullanim", h.KaynakBenKullanim)
	r.With(a).Put("/resellers/{id}", h.Guncelle)
	r.With(a).Post("/resellers", h.Olustur)
	r.With(a).Get("/reseller-plans", h.PaketListe)
	r.With(a).Get("/reseller-plans/{id}", h.PaketDetay)
	r.With(a).Post("/reseller-plans", h.PaketOlustur)
	r.With(a).Put("/reseller-plans/{id}", h.PaketGuncelle)
	return r
}

type kaJeton string

func kaAdmin(t *testing.T) kaJeton {
	t.Helper()
	s, err := auth.IssueAt(kaSir, 3600, 1, "root", "admin", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	return kaJeton(s)
}

func kaBayi(t *testing.T, id int64, ad string) kaJeton {
	t.Helper()
	s, err := auth.IssueResellerAt(kaSir, 3600, id, ad, id, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	return kaJeton(s)
}

func kaMusteri(t *testing.T) kaJeton {
	t.Helper()
	s, _, err := auth.GenerateMusteri(kaSir, auth.MusteriClaims{FTPHesapID: 1, DomainID: 1, Kullanici: "k", AlanAdi: "a.test"}, 3600)
	if err != nil {
		t.Fatal(err)
	}
	return kaJeton(s)
}

func (j kaJeton) istek(h http.Handler, yontem, yol string, govde any) *httptest.ResponseRecorder {
	var b bytes.Buffer
	if govde != nil {
		switch v := govde.(type) {
		case string:
			b.WriteString(v)
		default:
			_ = json.NewEncoder(&b).Encode(v)
		}
	}
	rq := httptest.NewRequest(yontem, yol, &b)
	rq.Header.Set("Authorization", "Bearer "+string(j))
	rq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, rq)
	return w
}

func kaJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("JSON değil: %v: %s", err, w.Body.String())
	}
	return m
}

func kaIc(m map[string]any, yol ...string) any {
	var c any = m
	for _, k := range yol {
		mm, ok := c.(map[string]any)
		if !ok {
			return nil
		}
		c = mm[k]
	}
	return c
}

// ── Kapsam: bayi yalnız KENDİ havuzunu görür ─────────────────────────────────────

func TestKaynakBen_YalnizKendiHavuzu(t *testing.T) {
	db := kaDB(t)
	h := kaYonlendirici(&Handlers{DB: db})
	a, b := kaBayi(t, kaBayiA, "bayia"), kaBayi(t, kaBayiB, "bayib")

	// İstek parametresiyle başka bayinin kimliğini göndermek hiçbir şeyi değiştirmez.
	for _, yol := range []string{"/reseller/kaynak", "/reseller/kaynak?id=11", "/reseller/kaynak?bayi_id=11&reseller_id=11"} {
		w := a.istek(h, "GET", yol, nil)
		if w.Code != 200 {
			t.Fatalf("%s → %d %s", yol, w.Code, w.Body.String())
		}
		m := kaJSON(t, w)
		if kaIc(m, "bayi_id") != float64(kaBayiA) || kaIc(m, "havuz", "cpu_cekirdek") != float64(2) ||
			kaIc(m, "havuz", "ram_mb") != float64(1024) || kaIc(m, "disk", "sinir_mb") != float64(5000) {
			t.Errorf("%s: A'nın havuzu bekleniyordu: %s", yol, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "777") || strings.Contains(w.Body.String(), "bayib") {
			t.Errorf("%s: B bayisinin verisi sızdı: %s", yol, w.Body.String())
		}
		// Sunucu iç ayrıntıları bayiye gösterilmez.
		if kaIc(m, "disk", "mod") != "" || kaIc(m, "disk", "mesaj") != "" || kaIc(m, "yeniden_baslatma_gerekli") != false {
			t.Errorf("%s: bayiye sunucu iç ayrıntısı gösterildi: %s", yol, w.Body.String())
		}
	}
	// Kullanım serisi: A yalnız kendi noktasını (150) görür; ?id=11 B'nin serisini AÇMAZ.
	for _, yol := range []string{"/reseller/kaynak/kullanim?aralik=24s", "/reseller/kaynak/kullanim?aralik=24s&id=11&bayi_id=11"} {
		w := a.istek(h, "GET", yol, nil)
		if w.Code != 200 {
			t.Fatalf("%s → %d %s", yol, w.Code, w.Body.String())
		}
		m := kaJSON(t, w)
		pts, _ := m["noktalar"].([]any)
		if kaIc(m, "bayi_id") != float64(kaBayiA) || len(pts) != 1 || strings.Contains(w.Body.String(), "777") {
			t.Errorf("%s: A'nın serisi bekleniyordu: %s", yol, w.Body.String())
		}
		if kaIc(m, "adim_sn") != float64(300) {
			t.Errorf("seri adımı 300 sn olmalı: %s", w.Body.String())
		}
	}
	// Simetri: B kendi noktasını görür, A'nınkini görmez.
	w := b.istek(h, "GET", "/reseller/kaynak/kullanim?aralik=7g&id=10", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "777") || strings.Contains(w.Body.String(), `"cpu_milli":150`) {
		t.Errorf("B serisi: %d %s", w.Code, w.Body.String())
	}
	// Havuzsuz bayi: 200, sınırsız.
	c := kaBayi(t, kaBayiC, "bayic").istek(h, "GET", "/reseller/kaynak", nil)
	if c.Code != 200 || kaIc(kaJSON(t, c), "disk", "mod") != "" {
		t.Errorf("havuzsuz bayi: %d %s", c.Code, c.Body.String())
	}
	// Verisi olmayan bayinin serisi BOŞ DİZİ döner (null değil: arayüz .map'te patlamasın); aralık
	// verilmezse varsayılan 24s; geçersiz aralık 400.
	cj := kaBayi(t, kaBayiC, "bayic")
	w = cj.istek(h, "GET", "/reseller/kaynak/kullanim", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"noktalar":[]`) || kaIc(kaJSON(t, w), "aralik") != "24s" {
		t.Errorf("boş seri / varsayılan aralık: %d %s", w.Code, w.Body.String())
	}
	if w := cj.istek(h, "GET", "/reseller/kaynak/kullanim?aralik=90g", nil); w.Code != http.StatusBadRequest {
		t.Errorf("geçersiz aralık → %d", w.Code)
	}
}

// Yönetici uçlarına bayi/müşteri GİREMEZ; bayi uçlarına yönetici (kimliği yok) 400, müşteri reddedilir.
func TestKaynakAPI_RolKapilari(t *testing.T) {
	db := kaDB(t)
	h := kaYonlendirici(&Handlers{DB: db})
	eylemKancaSifirla(nil)
	havuzKancaSifirla()
	a := kaBayi(t, kaBayiA, "bayia")
	m := kaMusteri(t)

	yonetici := []struct{ yontem, yol, govde string }{
		{"GET", "/bayi-kaynak", ""}, {"GET", "/bayi-kaynak/11", ""}, {"GET", "/bayi-kaynak/11/kullanim", ""},
		{"POST", "/bayi-kaynak/11/uygula", ""}, {"GET", "/bayi-kaynak/disk-kotasi", ""},
		{"POST", "/bayi-kaynak/disk-kotasi/etkinlestir", `{"onayla":true,"asim_kabul":true}`},
		{"PUT", "/resellers/11", `{"cpu_cekirdek":1}`},
	}
	for _, e := range yonetici {
		for ad, j := range map[string]kaJeton{"bayi": a, "musteri": m} {
			w := j.istek(h, e.yontem, e.yol, e.govde)
			if w.Code != http.StatusForbidden {
				t.Errorf("%s %s %s → %d (403 beklenir): %s", ad, e.yontem, e.yol, w.Code, w.Body.String())
			}
		}
		if w := kaJeton("").istek(h, e.yontem, e.yol, e.govde); w.Code != http.StatusUnauthorized {
			t.Errorf("anonim %s %s → %d (401 beklenir)", e.yontem, e.yol, w.Code)
		}
	}
	// Reddedilen isteklerin HİÇBİRİ sunucuya dokunmadı, DB'yi değiştirmedi.
	if u, e := eylemKancaOku(); u != 0 || len(e) != 0 {
		t.Errorf("yetkisiz istek eylem tetikledi: uzlastir=%d etkinlestir=%v", u, e)
	}
	if n, _ := havuzKancaOku(); n != 0 {
		t.Errorf("yetkisiz istek havuz kancasını tetikledi")
	}
	if opSay(t, db, `SELECT COUNT(*) FROM users WHERE id=11 AND kaynak_cpu_cekirdek=4`) != 1 {
		t.Error("yetkisiz PUT bayi kaydını değiştirdi")
	}
	// Bayi uçları: yönetici → 400 (bayi kimliği yok), müşteri → 403.
	adm := kaAdmin(t)
	for _, yol := range []string{"/reseller/kaynak", "/reseller/kaynak/kullanim"} {
		if w := adm.istek(h, "GET", yol, nil); w.Code != http.StatusBadRequest {
			t.Errorf("yönetici %s → %d (400 beklenir)", yol, w.Code)
		}
		if w := m.istek(h, "GET", yol, nil); w.Code != http.StatusForbidden {
			t.Errorf("müşteri %s → %d (403 beklenir)", yol, w.Code)
		}
	}
}

// ── Yönetici uçları ───────────────────────────────────────────────────────────────

func TestKaynakAPI_YoneticiOkuma(t *testing.T) {
	db := kaDB(t)
	h := kaYonlendirici(&Handlers{DB: db})
	adm := kaAdmin(t)

	w := adm.istek(h, "GET", "/bayi-kaynak", nil)
	var liste []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &liste); err != nil || w.Code != 200 {
		t.Fatalf("liste: %d %v %s", w.Code, err, w.Body.String())
	}
	if len(liste) != 3 {
		t.Fatalf("3 bayi beklenir (müşteri/yönetici listede olmaz): %s", w.Body.String())
	}
	gor := map[string]map[string]any{}
	for _, o := range liste {
		gor[fmt.Sprint(o["kullanici"])] = o
	}
	if kaIc(gor["bayia"], "havuz", "cpu_cekirdek") != float64(2) || kaIc(gor["bayib"], "havuz", "ram_mb") != float64(2048) ||
		kaIc(gor["bayic"], "dilim_aktif") != false {
		t.Errorf("liste içeriği: %s", w.Body.String())
	}

	if w := adm.istek(h, "GET", "/bayi-kaynak/11", nil); w.Code != 200 || kaIc(kaJSON(t, w), "havuz", "cpu_cekirdek") != float64(4) {
		t.Errorf("detay: %d %s", w.Code, w.Body.String())
	}
	// Bayi olmayan (müşteri/kök) / olmayan / geçersiz kimlikler.
	for yol, kod := range map[string]int{
		"/bayi-kaynak/20": 404, "/bayi-kaynak/1": 404, "/bayi-kaynak/9999": 404,
		"/bayi-kaynak/abc": 400, "/bayi-kaynak/0": 400, "/bayi-kaynak/-5": 400,
		"/bayi-kaynak/20/kullanim": 404, "/bayi-kaynak/abc/kullanim": 400,
	} {
		if w := adm.istek(h, "GET", yol, nil); w.Code != kod {
			t.Errorf("%s → %d (%d beklenir): %s", yol, w.Code, kod, w.Body.String())
		}
	}
	// Seri: yönetici istediği bayiyi görür; aralık doğrulanır.
	if w := adm.istek(h, "GET", "/bayi-kaynak/11/kullanim?aralik=30g", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "777") {
		t.Errorf("seri: %d %s", w.Code, w.Body.String())
	}
	for _, a := range []string{"1y", "24", "'; DROP TABLE users;--", "0s", "24s' OR '1'='1"} {
		if w := adm.istek(h, "GET", "/bayi-kaynak/11/kullanim?aralik="+url.QueryEscape(a), nil); w.Code != 400 {
			t.Errorf("aralik=%q → %d (400 beklenir)", a, w.Code)
		}
	}
	if opSay(t, db, `SELECT COUNT(*) FROM users`) != 5 {
		t.Error("kullanıcı tablosu değişti")
	}
}

func TestKaynakUygula_YoneticiTetikler(t *testing.T) {
	db := kaDB(t)
	h := kaYonlendirici(&Handlers{DB: db})
	adm := kaAdmin(t)
	eylemKancaSifirla(nil)
	havuzKancaSifirla()

	if w := adm.istek(h, "POST", "/bayi-kaynak/9999/uygula", nil); w.Code != 404 {
		t.Errorf("olmayan bayi → %d", w.Code)
	}
	if u, _ := eylemKancaOku(); u != 0 {
		t.Error("olmayan bayi için uzlaştırma çalıştı")
	}
	w := adm.istek(h, "POST", "/bayi-kaynak/10/uygula", nil)
	if w.Code != 200 {
		t.Fatalf("uygula → %d %s", w.Code, w.Body.String())
	}
	if u, _ := eylemKancaOku(); u != 1 {
		t.Errorf("uzlaştırma %d kez çalıştı", u)
	}
	if n, _ := havuzKancaOku(); n != 1 {
		t.Errorf("disk/havuz kancası %d kez çalıştı", n)
	}
	if opSay(t, db, `SELECT COUNT(*) FROM audit_log WHERE action='bayi.kaynak_uygula' AND target='10' AND ok=1`) != 1 {
		t.Error("denetim kaydı yazılmadı")
	}
}

// ── Disk kotasını etkinleştirme: AÇIK onay, aşım uyarısı, hata yolu ───────────────────

func TestDiskKotasiEtkinlestir_OnayVeAsimKapilari(t *testing.T) {
	db := kaDB(t)
	h := kaYonlendirici(&Handlers{DB: db})
	adm := kaAdmin(t)
	yol := "/bayi-kaynak/disk-kotasi/etkinlestir"

	// A bayisi havuzunu (5000 MB) AŞIYOR (kullanım ölçümü sahte: gerçek ölçüm bayikaynak testlerinde).
	asimli := bayikaynak.AsimBayi{BayiID: kaBayiA, Kullanici: "bayia", KullanilanMB: 6000, SinirMB: 5000}

	// 1) Onay yok → 400, grubby yok.
	eylemKancaSifirla(nil, asimli)
	for _, g := range []string{`{}`, `{"onayla":false}`, `{"onayla":false,"asim_kabul":true}`, `{"onay":true}`, `bozuk`} {
		if w := adm.istek(h, "POST", yol, g); w.Code != 400 {
			t.Errorf("%s → %d (400 beklenir)", g, w.Code)
		}
	}
	if _, e := eylemKancaOku(); len(e) != 0 {
		t.Fatalf("onaysız istek disk eylemi çalıştırdı: %v", e)
	}

	// 2) Aşan bayi var, asim_kabul yok → 409 + bayi listesi; grubby yok.
	w := adm.istek(h, "POST", yol, `{"onayla":true}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("aşım uyarısı yok: %d %s", w.Code, w.Body.String())
	}
	if m := kaJSON(t, w); m["asimdaki_bayiler"] == nil || !strings.Contains(w.Body.String(), "bayia") {
		t.Errorf("aşan bayiler listelenmedi: %s", w.Body.String())
	}
	if _, e := eylemKancaOku(); len(e) != 0 {
		t.Fatalf("aşım kabul edilmeden eylem çalıştı: %v", e)
	}

	// 3) Aşımı bilerek kabul → eylem TAM BİR KEZ, uygula=true; denetim ok=1.
	w = adm.istek(h, "POST", yol, `{"onayla":true,"asim_kabul":true}`)
	if w.Code != 200 {
		t.Fatalf("kabullü istek → %d %s", w.Code, w.Body.String())
	}
	if _, e := eylemKancaOku(); len(e) != 1 || !e[0] {
		t.Errorf("etkinleştirme çağrıları: %v", e)
	}
	if opSay(t, db, `SELECT COUNT(*) FROM audit_log WHERE action='bayi.disk_kotasi_etkinlestir' AND ok=1`) != 1 {
		t.Error("başarılı etkinleştirme denetlenmedi")
	}

	// 4) Aşım yokken yalnız onay yeter.
	eylemKancaSifirla(nil)
	if w := adm.istek(h, "POST", yol, `{"onayla":true}`); w.Code != 200 {
		t.Errorf("aşımsız onaylı istek → %d %s", w.Code, w.Body.String())
	}
	if _, e := eylemKancaOku(); len(e) != 1 || !e[0] {
		t.Errorf("çağrılar: %v", e)
	}

	// 5) Eylem hata verirse 422 + denetim ok=0 (başarı SAYILMAZ).
	eylemKancaSifirla(fmt.Errorf("grubby yok"))
	w = adm.istek(h, "POST", yol, `{"onayla":true}`)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "grubby yok") {
		t.Errorf("hata yolu: %d %s", w.Code, w.Body.String())
	}
	if opSay(t, db, `SELECT COUNT(*) FROM audit_log WHERE action='bayi.disk_kotasi_etkinlestir' AND ok=0`) != 1 {
		t.Error("başarısız etkinleştirme ok=0 ile denetlenmedi")
	}

	// 6) GET önizleme yalnız KURU çalıştırma ister (uygula=false).
	eylemKancaSifirla(nil)
	if w := adm.istek(h, "GET", "/bayi-kaynak/disk-kotasi", nil); w.Code != 200 {
		t.Errorf("önizleme → %d", w.Code)
	}
	if _, e := eylemKancaOku(); len(e) != 1 || e[0] {
		t.Errorf("önizleme kuru çalıştırma değil: %v", e)
	}
}

// ── Bayi kaydı: doğrulama, kalıcılık, kanca ──────────────────────────────────────────

func TestBayiGuncelle_KaynakDogrulamaVeKalicilik(t *testing.T) {
	db := kaDB(t)
	h := kaYonlendirici(&Handlers{DB: db})
	adm := kaAdmin(t)
	havuzKancaSifirla()

	// Sınır dışı / bozuk değerler: 400, DB aynı, kanca çağrılmaz.
	for _, g := range []string{
		`{"cpu_cekirdek":0.05}`, `{"cpu_cekirdek":-1}`, `{"cpu_cekirdek":5000}`,
		`{"ram_mb":10}`, `{"ram_mb":-5}`, `{"io_agirlik":20000}`, `{"io_agirlik":-1}`,
		`{"max_disk_mb":-1}`, `{"cpu_kota":2}`, `{"cpu_cekirdek":"iki"}`,
	} {
		if w := adm.istek(h, "PUT", "/resellers/12", g); w.Code != 400 {
			t.Errorf("%s → %d (400 beklenir): %s", g, w.Code, w.Body.String())
		}
	}
	if n, _ := havuzKancaOku(); n != 0 {
		t.Errorf("geçersiz istek kanca tetikledi (%d)", n)
	}
	if opSay(t, db, `SELECT COUNT(*) FROM users WHERE id=12 AND kaynak_cpu_cekirdek=0 AND kaynak_ram_mb=0 AND kaynak_io_agirlik=0 AND max_disk_mb=0`) != 1 {
		t.Fatal("geçersiz istek bayi kaydını değiştirdi")
	}

	// Geçerli: kalıcı + kanca bir kez + denetimde önceki→yeni değerler.
	if w := adm.istek(h, "PUT", "/resellers/12", `{"cpu_cekirdek":1.5,"ram_mb":512,"io_agirlik":300,"max_disk_mb":2048}`); w.Code != 200 {
		t.Fatalf("geçerli güncelleme → %d %s", w.Code, w.Body.String())
	}
	var cpu float64
	var ram, disk int64
	var io int
	if err := db.QueryRow(`SELECT kaynak_cpu_cekirdek, kaynak_ram_mb, kaynak_io_agirlik, max_disk_mb FROM users WHERE id=12`).Scan(&cpu, &ram, &io, &disk); err != nil {
		t.Fatal(err)
	}
	if cpu != 1.5 || ram != 512 || io != 300 || disk != 2048 {
		t.Errorf("kalıcılık: cpu=%v ram=%d io=%d disk=%d", cpu, ram, io, disk)
	}
	if n, _ := havuzKancaOku(); n != 1 {
		t.Errorf("havuz kancası %d kez (1 beklenir)", n)
	}
	var detay string
	_ = db.QueryRow(`SELECT detail FROM audit_log WHERE action='bayi.guncelle' ORDER BY id DESC LIMIT 1`).Scan(&detay)
	if !strings.Contains(detay, "cpu_cekirdek 0→1.5") || !strings.Contains(detay, "ram_mb 0→512") {
		t.Errorf("denetim ayrıntısı: %q", detay)
	}

	// Yalnız ad değişimi havuz kancasını TETİKLEMEZ.
	if w := adm.istek(h, "PUT", "/resellers/12", `{"ad_soyad":"Yeni Ad"}`); w.Code != 200 {
		t.Fatalf("ad güncelle → %d", w.Code)
	}
	if n, _ := havuzKancaOku(); n != 1 {
		t.Errorf("ad değişimi havuz kancasını tetikledi (%d)", n)
	}
	// Alan gönderilmezse mevcut havuz KORUNUR (nil = dokunma).
	if opSay(t, db, `SELECT COUNT(*) FROM users WHERE id=12 AND kaynak_cpu_cekirdek=1.5 AND kaynak_ram_mb=512`) != 1 {
		t.Error("ilgisiz güncelleme havuzu sıfırladı")
	}
	// 0 = sınırsıza dönüş açık bir eylemdir ve kancayı tetikler (dilim kaldırılmalı).
	if w := adm.istek(h, "PUT", "/resellers/12", `{"cpu_cekirdek":0,"ram_mb":0,"io_agirlik":0,"max_disk_mb":0}`); w.Code != 200 {
		t.Fatalf("sıfırlama → %d", w.Code)
	}
	if opSay(t, db, `SELECT COUNT(*) FROM users WHERE id=12 AND kaynak_cpu_cekirdek=0 AND kaynak_ram_mb=0 AND kaynak_io_agirlik=0 AND max_disk_mb=0`) != 1 {
		t.Error("sınırsıza dönüş yazılmadı")
	}
	if n, _ := havuzKancaOku(); n != 2 {
		t.Errorf("sınırsıza dönüşte kanca %d (2 beklenir)", n)
	}
	// Olmayan bayi: 404.
	if w := adm.istek(h, "PUT", "/resellers/9999", `{"cpu_cekirdek":1}`); w.Code != 404 {
		t.Errorf("olmayan bayi → %d", w.Code)
	}
}

// ── Paket: alanlar kaydedilir, doğrulanır ve bayiye KOPYALANIR ────────────────────────

func TestBayiPaketi_KaynakAlanlariVeBayiyeKopyalama(t *testing.T) {
	db := kaDB(t)
	h := kaYonlendirici(&Handlers{DB: db})
	adm := kaAdmin(t)
	havuzKancaSifirla()

	paket := func(cpu float64, ram int64, io int, disk int64) map[string]any {
		return map[string]any{"ad": fmt.Sprintf("P%d", time.Now().UnixNano()), "max_domain": 10, "max_disk_mb": disk, "max_trafik_mb": 1000000,
			"cpu_cekirdek": cpu, "ram_mb": ram, "io_agirlik": io, "asim_ilkesi": "yok", "fazla_satis": true}
	}
	// Doğrulama: PaketOlustur ve PaketGuncelle aynı kuralı uygular.
	for _, p := range []map[string]any{paket(0.05, 0, 0, 100000), paket(1, 10, 0, 100000), paket(1, 0, 20000, 100000), paket(-1, 0, 0, 100000)} {
		if w := adm.istek(h, "POST", "/reseller-plans", p); w.Code != 400 {
			t.Errorf("geçersiz paket %v → %d", p, w.Code)
		}
	}
	if opSay(t, db, `SELECT COUNT(*) FROM reseller_plans WHERE id>1`) != 0 {
		t.Fatal("geçersiz paket kaydedildi")
	}
	w := adm.istek(h, "POST", "/reseller-plans", paket(2.5, 4096, 400, 100000))
	if w.Code != http.StatusCreated {
		t.Fatalf("paket oluştur → %d %s", w.Code, w.Body.String())
	}
	id := int64(kaJSON(t, w)["id"].(float64))
	d := kaJSON(t, adm.istek(h, "GET", fmt.Sprintf("/reseller-plans/%d", id), nil))
	if d["cpu_cekirdek"] != 2.5 || d["ram_mb"] != float64(4096) || d["io_agirlik"] != float64(400) || d["max_disk_mb"] != float64(100000) {
		t.Errorf("paket ayrıntısı: %v", d)
	}
	// Güncelle + liste.
	g := paket(3, 8192, 0, 200000)
	g["ad"] = d["ad"]
	if w := adm.istek(h, "PUT", fmt.Sprintf("/reseller-plans/%d", id), g); w.Code != 200 {
		t.Fatalf("paket güncelle → %d %s", w.Code, w.Body.String())
	}
	if bad := paket(1, 10, 0, 100000); true {
		if w := adm.istek(h, "PUT", fmt.Sprintf("/reseller-plans/%d", id), bad); w.Code != 400 {
			t.Errorf("geçersiz paket güncellemesi → %d", w.Code)
		}
	}
	var liste []map[string]any
	_ = json.Unmarshal(adm.istek(h, "GET", "/reseller-plans", nil).Body.Bytes(), &liste)
	var bulundu bool
	for _, p := range liste {
		if p["id"] == float64(id) {
			bulundu = true
			if p["cpu_cekirdek"] != float64(3) || p["ram_mb"] != float64(8192) || p["io_agirlik"] != float64(0) {
				t.Errorf("listede paket: %v", p)
			}
		}
	}
	if !bulundu {
		t.Error("paket listede yok")
	}
	// Bayiye paket atamak havuzu (CPU/RAM/G-Ç/disk) KOPYALAR ve uzlaştırmayı tetikler.
	if w := adm.istek(h, "PUT", "/resellers/12", fmt.Sprintf(`{"paket_id":%d}`, id)); w.Code != 200 {
		t.Fatalf("paket ata → %d %s", w.Code, w.Body.String())
	}
	var cpu float64
	var ram, disk int64
	var io int
	_ = db.QueryRow(`SELECT kaynak_cpu_cekirdek, kaynak_ram_mb, kaynak_io_agirlik, max_disk_mb FROM users WHERE id=12`).Scan(&cpu, &ram, &io, &disk)
	if cpu != 3 || ram != 8192 || io != 0 || disk != 200000 {
		t.Errorf("bayiye kopyalanan havuz: cpu=%v ram=%d io=%d disk=%d", cpu, ram, io, disk)
	}
	if n, _ := havuzKancaOku(); n != 1 {
		t.Errorf("paket atamada kanca %d kez", n)
	}
	// Anlık görüntü modeli: paketi sonradan değiştirmek mevcut bayiyi DEĞİŞTİRMEZ.
	g2 := paket(1, 0, 0, 200000)
	g2["ad"] = d["ad"]
	if w := adm.istek(h, "PUT", fmt.Sprintf("/reseller-plans/%d", id), g2); w.Code != 200 {
		t.Fatalf("paket ikinci güncelleme → %d", w.Code)
	}
	if opSay(t, db, `SELECT COUNT(*) FROM users WHERE id=12 AND kaynak_cpu_cekirdek=3 AND kaynak_ram_mb=8192`) != 1 {
		t.Error("paket güncellemesi mevcut bayinin havuzunu değiştirdi (anlık görüntü modeli bozuldu)")
	}
}

// Hiç bayi yokken liste BOŞ DİZİ döner (null değil: arayüz liste üzerinde dolaşabilsin).
func TestKaynakListe_BosListeDizidir(t *testing.T) {
	db := kaDB(t)
	if _, err := db.Exec(`DELETE FROM users WHERE role='reseller'`); err != nil {
		t.Fatal(err)
	}
	w := kaAdmin(t).istek(kaYonlendirici(&Handlers{DB: db}), "GET", "/bayi-kaynak", nil)
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Errorf("boş liste: %d %q", w.Code, w.Body.String())
	}
}

// Havuzu etkileyen HER alan tek başına da uzlaştırmayı tetikler; havuzu etkilemeyen alanlar tetiklemez.
func TestBayiGuncelle_HerAlanTekBasinaKancayiTetikler(t *testing.T) {
	db := kaDB(t)
	h := kaYonlendirici(&Handlers{DB: db})
	adm := kaAdmin(t)
	for govde, bek := range map[string]int{
		`{"cpu_cekirdek":1}`: 1, `{"ram_mb":256}`: 1, `{"io_agirlik":50}`: 1, `{"max_disk_mb":1000}`: 1,
		`{"max_domain":5}`: 0, `{"max_trafik_mb":100}`: 0, `{"ad_soyad":"X"}`: 0,
	} {
		havuzKancaSifirla()
		if w := adm.istek(h, "PUT", "/resellers/12", govde); w.Code != 200 {
			t.Fatalf("%s → %d %s", govde, w.Code, w.Body.String())
		}
		if n, _ := havuzKancaOku(); n != bek {
			t.Errorf("%s: havuz kancası %d kez (%d beklenir)", govde, n, bek)
		}
	}
}

// Bayi oluşturma: paketin kaynak havuzu geçersizse 400 (bayi açılmaz); yalnız disk havuzu olan paket
// uzlaştırmayı tetikler; hiç havuzu olmayan paket tetiklemez.
func TestBayiOlustur_PaketHavuzuDogrulanirVeKancaKosullu(t *testing.T) {
	db := kaDB(t)
	h := kaYonlendirici(&Handlers{DB: db})
	adm := kaAdmin(t)
	for _, q := range []string{
		`INSERT INTO reseller_plans (id, ad, max_domain, max_disk_mb, max_trafik_mb, kaynak_cpu_cekirdek) VALUES (7,'Bozuk',5,1000,1000,0.05)`,
		`INSERT INTO reseller_plans (id, ad, max_domain, max_disk_mb, max_trafik_mb) VALUES (8,'YalnizDisk',5,5000,1000)`,
		`INSERT INTO reseller_plans (id, ad, max_domain, max_disk_mb, max_trafik_mb) VALUES (9,'Havuzsuz',5,0,0)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	olustur := func(paket int, kul string) *httptest.ResponseRecorder {
		return adm.istek(h, "POST", "/resellers", map[string]any{"paket_id": paket, "kullanici": kul, "parola": "test-parola-123"})
	}
	havuzKancaSifirla()
	if w := olustur(7, "bozukbayi"); w.Code != 400 || !strings.Contains(w.Body.String(), "kaynak") {
		t.Errorf("geçersiz paket havuzu → %d %s", w.Code, w.Body.String())
	}
	if opSay(t, db, `SELECT COUNT(*) FROM users WHERE username='bozukbayi'`) != 0 {
		t.Error("geçersiz paket havuzuyla bayi oluşturuldu")
	}
	if n, _ := havuzKancaOku(); n != 0 {
		t.Errorf("reddedilen istek kanca tetikledi (%d)", n)
	}
	if w := olustur(8, "yalnizdisk"); w.Code != http.StatusCreated {
		t.Fatalf("disk-havuzlu paket → %d %s", w.Code, w.Body.String())
	}
	if n, _ := havuzKancaOku(); n != 1 {
		t.Errorf("yalnız disk havuzu olan bayi uzlaştırmayı tetiklemedi (%d)", n)
	}
	havuzKancaSifirla()
	if w := olustur(9, "havuzsuz"); w.Code != http.StatusCreated {
		t.Fatalf("havuzsuz paket → %d %s", w.Code, w.Body.String())
	}
	if n, _ := havuzKancaOku(); n != 0 {
		t.Errorf("havuzsuz bayi için gereksiz uzlaştırma (%d)", n)
	}
}

// Mevcut bayiye kaynak havuzu geçersiz bir paket atanamaz: 400, kayıt ve havuz değişmez.
func TestBayiGuncelle_GecersizPaketHavuzuReddedilir(t *testing.T) {
	db := kaDB(t)
	h := kaYonlendirici(&Handlers{DB: db})
	if _, err := db.Exec(`INSERT INTO reseller_plans (id, ad, max_domain, max_disk_mb, max_trafik_mb, kaynak_ram_mb) VALUES (7,'Bozuk',5,1000,1000,10)`); err != nil {
		t.Fatal(err)
	}
	havuzKancaSifirla()
	w := kaAdmin(t).istek(h, "PUT", "/resellers/10", `{"paket_id":7}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "kaynak") {
		t.Fatalf("geçersiz paket havuzu → %d %s", w.Code, w.Body.String())
	}
	if opSay(t, db, `SELECT COUNT(*) FROM users WHERE id=10 AND reseller_plan_id=0 AND kaynak_cpu_cekirdek=2 AND kaynak_ram_mb=1024 AND max_disk_mb=5000`) != 1 {
		t.Error("reddedilen paket ataması bayi kaydını değiştirdi")
	}
	if n, _ := havuzKancaOku(); n != 0 {
		t.Errorf("reddedilen istek kanca tetikledi (%d)", n)
	}
}
