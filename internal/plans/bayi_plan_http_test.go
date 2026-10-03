package plans_test

// bayi_plan_http_test.go — bayinin KENDİ planları (bayi_plan.go), gerçek rota
// zinciriyle (RequireAuth + AdminVeyaReseller) ve gerçek MariaDB'yle:
//
//   - Açılış uzlaştırması: planı olmayan bayiye "<bayi> Varsayılan" oluşur (değer
//     kuralı: kök şablonu ⊓ hosting başına havuz payı ⊓ posta tavanı; kök iç ayarları
//     şablondan sızmaz), planı olan ama varsayılanı olmayana en eskisi işaretlenir;
//     idempotent; hostinglerin planına dokunmaz.
//   - Bayi kendi planını oluşturur/düzenler/siler: paket havuzu + bağlı hostinglerin
//     taahhüdü; kök alanları (IO/DB governor…) bayi isteğiyle değişmez; varsayılan
//     işareti kaldırılamaz, VarsayilanYap ile değişir; son plan silinemez; silinen
//     varsayılanın yerine en eski plan geçer; düzenleme bağlı hostinglere uygulanır.
//   - IDOR: bayi A, bayi C'nin planını göremez/düzenleyemez/silemez/varsayılan
//     yapamaz; müşteri hiçbir plan ucuna yazamaz; kökün paylaşılan planı bayiye salt
//     okunur; yönetici her planı düzenler (varsayılan kapsamı planın sahibi).
//
// Fikstür pk_geciciDB (kapsam_http_test.go) + bayi C (4): planı yok, havuz 10000 MB
// disk / 50000 MB trafik, 5 hosting hakkı, fazla satış KAPALI, posta tavanı 10 kutu.

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/auth"
	"girginospanel/internal/middleware"
	"girginospanel/internal/plans"
)

func bp_geciciDB(t *testing.T) *sql.DB {
	t.Helper()
	db := pk_geciciDB(t)
	for _, q := range []string{
		`ALTER TABLE users ADD username VARCHAR(64) NOT NULL DEFAULT '', ADD max_domain INT NOT NULL DEFAULT 0`,
		`UPDATE users SET username='root' WHERE id=1`,
		`UPDATE users SET username='bayiA' WHERE id=2`,
		`UPDATE users SET username='bayiB' WHERE id=3`,
		`CREATE TABLE reseller_plans (id BIGINT UNSIGNED PRIMARY KEY, mail_max_email INT NOT NULL DEFAULT 0,
			mail_saatlik_limit INT NOT NULL DEFAULT 0, mail_kutu_kota_mb INT NOT NULL DEFAULT 0) ENGINE=InnoDB`,
		`INSERT INTO reseller_plans (id, mail_max_email) VALUES (9, 10)`,
		`INSERT INTO users (id, role, username, max_domain, max_disk_mb, max_trafik_mb, fazla_satis, reseller_plan_id)
			VALUES (4, 'reseller', 'bayic', 5, 10000, 50000, 0, 9)`,
	} {
		pk_exec(t, db, q)
	}
	return db
}

// bp_router — main.go /plans zinciri + ozellik_bayiplan.go uçları.
func bp_router(db *sql.DB) http.Handler {
	ph := &plans.Handlers{DB: db}
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(pk_sir))
		r.With(middleware.AdminVeyaReseller).Get("/plans", ph.List)
		r.With(middleware.AdminVeyaReseller).Get("/plans/bayi-sinir", ph.BayiSinir)
		r.With(middleware.AdminVeyaReseller).Get("/plans/{id}", ph.Get)
		r.With(middleware.AdminVeyaReseller).Post("/plans", ph.Create)
		r.With(middleware.AdminVeyaReseller).Put("/plans/{id}", ph.Update)
		r.With(middleware.AdminVeyaReseller).Delete("/plans/{id}", ph.Delete)
		r.With(middleware.AdminVeyaReseller).Post("/plans/{id}/varsayilan", ph.VarsayilanYap)
	})
	return r
}

// bp_istek — pk_istek + bayi C ve müşteri kimlikleri.
func bp_istek(t *testing.T, h http.Handler, kim, yontem, yol string, govde any) pk_yanit {
	t.Helper()
	switch kim {
	case "C", "musteri":
		var tok string
		var err error
		if kim == "C" {
			tok, err = auth.IssueResellerAt(pk_sir, 3600, 4, "bayic", 4, 0, "")
		} else {
			tok, _, err = auth.GenerateMusteri(pk_sir, auth.MusteriClaims{FTPHesapID: 1, DomainID: 40, Kullanici: "m", AlanAdi: "c-site.test"}, 3600)
		}
		if err != nil {
			t.Fatal(err)
		}
		return pk_istekJeton(t, h, tok, yontem, yol, govde)
	}
	return pk_istek(t, h, kim, yontem, yol, govde)
}

func pk_istekJeton(t *testing.T, h http.Handler, tok, yontem, yol string, govde any) pk_yanit {
	t.Helper()
	var b []byte
	if govde != nil {
		b, _ = json.Marshal(govde)
		if strings.Contains(string(b), `"nginx_ek_direktifler":"`) && !strings.Contains(string(b), `"nginx_ek_direktifler":""`) {
			t.Fatalf("güvenlik: test gövdesi nginx direktifi taşıyamaz: %s", b)
		}
	}
	req := httptestIstek(yontem, yol, b)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptestKayit()
	h.ServeHTTP(rec, req)
	return pk_yanit{rec.Code, rec.Body.String()}
}

type bp_plan struct {
	id                      int64
	ad                      string
	disk, trafik, maxEmail  int
	ioRead, dbSn, proc, cpu int
	varsayilan              int
	sahip                   int64
	aciklama, nginx         string
	waf                     int
}

func bp_oku(t *testing.T, db *sql.DB, id int64) bp_plan {
	t.Helper()
	var p bp_plan
	var ng sql.NullString
	err := db.QueryRow(`SELECT id, ad, disk_kota_mb, trafik_kota_mb, max_email, io_read_mbps, db_max_query_seconds,
		max_process, cpu_yuzde, varsayilan, reseller_id, aciklama, nginx_ek_direktifler, waf_enabled FROM service_plans WHERE id=?`, id).
		Scan(&p.id, &p.ad, &p.disk, &p.trafik, &p.maxEmail, &p.ioRead, &p.dbSn, &p.proc, &p.cpu, &p.varsayilan, &p.sahip, &p.aciklama, &ng, &p.waf)
	if err != nil {
		t.Fatalf("plan %d okunamadı: %v", id, err)
	}
	p.nginx = ng.String
	return p
}

func bp_varsayilanlar(t *testing.T, db *sql.DB, sahip int64) []int64 {
	t.Helper()
	rows, err := db.Query(`SELECT id FROM service_plans WHERE reseller_id=? AND varsayilan=1 ORDER BY id`, sahip)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		out = append(out, id)
	}
	return out
}

func bp_sayi(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%v: %s", err, q)
	}
	return n
}

// bp_uygulayici — plans.HostingUygula sahtesi (sistem yan etkisi yok): çağrılan hostingler.
func bp_uygulayici(t *testing.T) func(time.Duration, int) []int64 {
	var mu sync.Mutex
	var cagri []int64
	eski := plans.HostingUygula
	plans.HostingUygula = func(_ *sql.DB, did int64, _ string) { mu.Lock(); cagri = append(cagri, did); mu.Unlock() }
	t.Cleanup(func() { plans.HostingUygula = eski })
	return func(bekle time.Duration, adet int) []int64 {
		son := time.Now().Add(bekle)
		for {
			mu.Lock()
			n := len(cagri)
			mu.Unlock()
			if n >= adet || time.Now().After(son) {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		mu.Lock()
		defer mu.Unlock()
		out := append([]int64(nil), cagri...)
		cagri = nil
		return out
	}
}

func bp_uzlastir(t *testing.T, db *sql.DB) (int, int) {
	t.Helper()
	o, i, err := plans.BayiVarsayilanUzlastir(t.Context(), db)
	if err != nil {
		t.Fatalf("uzlaştırma: %v", err)
	}
	return o, i
}

func bp_cPlani(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(`SELECT id FROM service_plans WHERE reseller_id=4 AND varsayilan=1`).Scan(&id); err != nil {
		t.Fatalf("bayi C varsayılan planı yok: %v", err)
	}
	return id
}

// ── Açılış uzlaştırması ─────────────────────────────────────────────────────

func TestBayiPlan_VarsayilanUzlastirma(t *testing.T) {
	db := bp_geciciDB(t)
	planOnce := bp_sayi(t, db, `SELECT COALESCE(SUM(plan_id*id),0) FROM domains`)

	olusan, isaretlenen := bp_uzlastir(t, db)
	if olusan != 1 || isaretlenen != 2 {
		t.Fatalf("uzlaştırma: %d oluştu / %d işaretlendi (beklenen 1 / 2: C oluşur, A ve B işaretlenir)", olusan, isaretlenen)
	}
	// Bayi C: yeni "<bayi> Varsayılan" planı — değer kuralı.
	cid := bp_cPlani(t, db)
	p := bp_oku(t, db, cid)
	if p.ad != "bayic Varsayılan" || p.sahip != 4 || p.varsayilan != 1 {
		t.Errorf("C varsayılan planı: %+v", p)
	}
	// Şablon = kökün varsayılan global planı "Standart" (10240/51200); hosting başına
	// pay = (havuz-1)/hak → disk 9999/5=1999, trafik 49999/5=9999; posta tavanı 10.
	if p.disk != 1999 || p.trafik != 9999 || p.maxEmail != 10 || p.cpu != 100 {
		t.Errorf("değer kuralı: disk=%d trafik=%d max_email=%d cpu=%d (beklenen 1999/9999/10/100)", p.disk, p.trafik, p.maxEmail, p.cpu)
	}
	// Kökün iç ayarları (IO 40 MB/s, KILL 30 sn, açıklama, nginx, WAF) sızmaz.
	if p.ioRead != 0 || p.dbSn != 0 || p.proc != 50 || strings.Contains(p.aciklama, "KOK") || p.nginx != "" || p.waf != 0 {
		t.Errorf("GL-06: kök şablonunun iç ayarı bayi planına sızdı: %+v", p)
	}
	// A: en eski kendi katalog planı (200) varsayılan; B: 300. Kökün varsayılanı (1) yerinde.
	if v := bp_varsayilanlar(t, db, 2); len(v) != 1 || v[0] != 200 {
		t.Errorf("A varsayılanları: %v (beklenen [200])", v)
	}
	if v := bp_varsayilanlar(t, db, 3); len(v) != 1 || v[0] != 300 {
		t.Errorf("B varsayılanları: %v (beklenen [300])", v)
	}
	if v := bp_varsayilanlar(t, db, 0); len(v) != 1 || v[0] != 1 {
		t.Errorf("kökün varsayılanı değişti: %v", v)
	}
	// Hostinglerin planına dokunulmadı.
	if s := bp_sayi(t, db, `SELECT COALESCE(SUM(plan_id*id),0) FROM domains`); s != planOnce {
		t.Errorf("uzlaştırma hosting planlarını değiştirdi")
	}
	if n := bp_sayi(t, db, `SELECT COUNT(*) FROM audit_log WHERE action='plan.varsayilan_olustur' AND reseller_id=4`); n != 1 {
		t.Errorf("denetim kaydı (plan.varsayilan_olustur, kapsam C): %d", n)
	}
	// İdempotent.
	toplam := bp_sayi(t, db, `SELECT COUNT(*) FROM service_plans`)
	if o, i := bp_uzlastir(t, db); o != 0 || i != 0 || bp_sayi(t, db, `SELECT COUNT(*) FROM service_plans`) != toplam {
		t.Errorf("ikinci uzlaştırma iş yaptı: %d/%d", o, i)
	}
	// Ad çakışması: kökün "bayid Varsayılan" adlı planı varken yeni bayi D yine planını alır.
	pk_exec(t, db, `INSERT INTO service_plans (id, ad, disk_kota_mb, trafik_kota_mb, reseller_id) VALUES (900, 'bayid Varsayılan', 1, 1, 0)`)
	pk_exec(t, db, `INSERT INTO users (id, role, username, max_disk_mb, max_trafik_mb) VALUES (5, 'reseller', 'bayid', 0, 0)`)
	id, durum, err := plans.BayiVarsayilanSagla(t.Context(), db, 5, 1, "root")
	if err != nil || durum != plans.VarsayilanOlusturuldu {
		t.Fatalf("D sağlama: %v %s", err, durum)
	}
	if d := bp_oku(t, db, id); d.ad != "bayid Varsayılan #5" || d.sahip != 5 || d.disk <= 0 || d.trafik <= 0 {
		t.Errorf("D (çakışma + sınırsız havuz → şablon değeri): %+v", d)
	}
	// Oluşan varsayılan plan bayinin kendi doğrulamasından geçer (aynı değerle PUT).
	h := bp_router(db)
	bp_uygulayici(t)
	if y := bp_istek(t, h, "C", "PUT", fmt.Sprintf("/plans/%d", cid), map[string]any{"ad": p.ad, "disk_kota_mb": p.disk,
		"trafik_kota_mb": p.trafik, "max_email": p.maxEmail, "nginx_ek_direktifler": ""}); y.kod != http.StatusOK {
		t.Errorf("C kendi varsayılan planını aynen kaydedemedi → %d %s", y.kod, y.govde)
	}
	// Bayi olmayan kimlik.
	if _, _, err := plans.BayiVarsayilanSagla(t.Context(), db, 1, 0, "sistem"); err != plans.ErrBayiYok {
		t.Errorf("yönetici kimliği için sağlama: %v", err)
	}
}

// ── Bayi kendi planını yönetir ──────────────────────────────────────────────

func TestBayiPlan_KendiPlaniniYonetir(t *testing.T) {
	db := bp_geciciDB(t)
	bp_uzlastir(t, db)
	h := bp_router(db)
	uygulanan := bp_uygulayici(t)
	varsayilanID := bp_cPlani(t, db)
	govde := func(ad string, disk, trafik int, ek map[string]any) map[string]any {
		g := map[string]any{"ad": ad, "disk_kota_mb": disk, "trafik_kota_mb": trafik, "max_email": 10, "nginx_ek_direktifler": ""}
		for k, v := range ek {
			g[k] = v
		}
		return g
	}

	// Paket havuzu: havuza ulaşan/aşan ya da sınırsız plan reddedilir.
	for _, g := range []map[string]any{govde("C Buyuk", 10000, 5000, nil), govde("C Buyuk", 3000, 50000, nil),
		govde("C Sinirsiz", 0, 5000, nil), govde("C Sinirsiz", 3000, 0, nil)} {
		if y := bp_istek(t, h, "C", "POST", "/plans", g); y.kod != http.StatusBadRequest {
			t.Errorf("paket sınırı: POST %v → %d %s (beklenen 400)", g, y.kod, y.govde)
		}
	}
	// Posta tavanı (paket: 10 kutu).
	if y := bp_istek(t, h, "C", "POST", "/plans", govde("C Posta", 3000, 5000, map[string]any{"max_email": 50})); y.kod != http.StatusBadRequest {
		t.Errorf("posta tavanı → %d %s", y.kod, y.govde)
	}
	// Geçerli plan: kök alanları gövdeden ALINMAZ (panel varsayılanı).
	y := bp_istek(t, h, "C", "POST", "/plans", govde("C Ikinci", 3000, 5000, map[string]any{
		"io_read_mbps": 999, "db_max_query_seconds": 5, "max_process": 9999, "cpu_yuzde": 200, "max_email": 10}))
	if y.kod != http.StatusCreated || !strings.Contains(y.govde, `"duzenlenebilir":true`) {
		t.Fatalf("C yeni plan → %d %s", y.kod, y.govde)
	}
	var yeni struct{ ID int64 }
	_ = json.Unmarshal([]byte(y.govde), &yeni)
	x := bp_oku(t, db, yeni.ID)
	if x.ioRead != 0 || x.dbSn != 0 || x.proc != 50 || x.cpu != 200 || x.sahip != 4 || x.varsayilan != 0 {
		t.Errorf("C yeni plan: kök alanı yazıldı ya da özet alanı düşmedi: %+v", x)
	}
	if n := bp_sayi(t, db, `SELECT COUNT(*) FROM audit_log WHERE action='plan.olustur' AND reseller_id=4`); n != 1 {
		t.Errorf("plan.olustur denetimi: %d", n)
	}

	// Plana iki hosting bağla: taahhüt 2×3000 = 6000 / havuz 10000.
	pk_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, plan_id, reseller_id) VALUES
		(40,'c-site.test','c_c1',?,4), (41,'c-iki.test','c_c2',?,4)`, yeni.ID, yeni.ID)
	yol := fmt.Sprintf("/plans/%d", yeni.ID)
	// Artış 4000 → 8000 < 10000: geçer ve bağlı hostinglere uygulanır.
	if y := bp_istek(t, h, "C", "PUT", yol, govde("C Ikinci", 4000, 5000, nil)); y.kod != http.StatusOK {
		t.Fatalf("C PUT 4000 → %d %s", y.kod, y.govde)
	}
	if c := uygulanan(3*time.Second, 2); len(c) != 2 {
		t.Errorf("düzenleme bağlı hostinglere uygulanmadı: %v (beklenen 40, 41)", c)
	}
	// Artış 5000 → 10000 havuza ULAŞIR: 409, plan değişmez.
	if y := bp_istek(t, h, "C", "PUT", yol, govde("C Ikinci", 5000, 5000, nil)); y.kod != http.StatusConflict || !strings.Contains(y.govde, "taahhüd") {
		t.Errorf("taahhüt kapısı → %d %s", y.kod, y.govde)
	}
	if bp_oku(t, db, yeni.ID).disk != 4000 {
		t.Errorf("reddedilen düzenleme planı değiştirdi")
	}
	// Değişmeyen limitle kaydetme yeniden uygulamayı tetiklemez.
	if y := bp_istek(t, h, "C", "PUT", yol, govde("C Ikinci Ad", 4000, 5000, nil)); y.kod != http.StatusOK {
		t.Errorf("ad değişikliği → %d %s", y.kod, y.govde)
	}
	if c := uygulanan(300*time.Millisecond, 1); len(c) != 0 {
		t.Errorf("limit değişmeden yeniden uygulandı: %v", c)
	}

	// Kök alanları: bayi değiştiremez; yönetici değiştirir; bayi kaydı yöneticininkini korur.
	if y := bp_istek(t, h, "C", "PUT", yol, govde("C Ikinci", 4000, 5000, map[string]any{"io_read_mbps": 777})); y.kod != http.StatusOK || bp_oku(t, db, yeni.ID).ioRead != 0 {
		t.Errorf("bayi kök alanını (io_read_mbps) değiştirdi → %d %s", y.kod, y.govde)
	}
	if y := pk_istek(t, h, "admin", "PUT", yol, govde("C Ikinci", 4000, 5000, map[string]any{"io_read_mbps": 50, "max_process": 80})); y.kod != http.StatusOK {
		t.Fatalf("yönetici bayi planını düzenleyemedi → %d %s", y.kod, y.govde)
	}
	uygulanan(time.Second, 2)
	if y := bp_istek(t, h, "C", "PUT", yol, govde("C Ikinci", 3500, 5000, nil)); y.kod != http.StatusOK {
		t.Fatalf("C PUT → %d %s", y.kod, y.govde)
	}
	uygulanan(time.Second, 2)
	if x := bp_oku(t, db, yeni.ID); x.ioRead != 50 || x.proc != 80 || x.disk != 3500 {
		t.Errorf("bayi kaydı yöneticinin kök alanlarını ezdi: %+v", x)
	}

	// Varsayılan: kaldırılamaz; VarsayilanYap ile değişir (kapsam yalnız C).
	if y := bp_istek(t, h, "C", "PUT", fmt.Sprintf("/plans/%d", varsayilanID), map[string]any{"ad": "bayic Varsayılan",
		"disk_kota_mb": 1999, "trafik_kota_mb": 9999, "max_email": 10, "varsayilan": false, "nginx_ek_direktifler": ""}); y.kod != http.StatusOK {
		t.Fatalf("C varsayılan planını kaydedemedi → %d %s", y.kod, y.govde)
	}
	if bp_oku(t, db, varsayilanID).varsayilan != 1 {
		t.Errorf("bayinin varsayılan işareti düzenlemeyle kaldırıldı")
	}
	if y := bp_istek(t, h, "C", "POST", yol+"/varsayilan", nil); y.kod != http.StatusOK {
		t.Fatalf("C varsayılan yap → %d %s", y.kod, y.govde)
	}
	if v := bp_varsayilanlar(t, db, 4); len(v) != 1 || v[0] != yeni.ID {
		t.Errorf("C varsayılanları %v (beklenen [%d])", v, yeni.ID)
	}
	if v := bp_varsayilanlar(t, db, 0); len(v) != 1 || v[0] != 1 {
		t.Errorf("bayinin varsayılan yapması kökünkünü değiştirdi: %v", v)
	}
	if n := bp_sayi(t, db, `SELECT COUNT(*) FROM audit_log WHERE action='plan.varsayilan' AND reseller_id=4`); n != 1 {
		t.Errorf("plan.varsayilan denetimi: %d", n)
	}

	// Silme: kullanılan plan silinemez; kullanılmayan silinir; son plan silinemez;
	// silinen varsayılanın yerine en eski plan geçer.
	if y := bp_istek(t, h, "C", "DELETE", yol, nil); y.kod != http.StatusConflict {
		t.Errorf("kullanılan plan silindi → %d %s", y.kod, y.govde)
	}
	if y := bp_istek(t, h, "C", "DELETE", fmt.Sprintf("/plans/%d", varsayilanID), nil); y.kod != http.StatusOK {
		t.Fatalf("C kullanılmayan planını silemedi → %d %s", y.kod, y.govde)
	}
	pk_exec(t, db, `DELETE FROM domains WHERE id IN (40,41)`)
	if y := bp_istek(t, h, "C", "DELETE", yol, nil); y.kod != http.StatusConflict || !strings.Contains(y.govde, "son plan") {
		t.Errorf("C son planını sildi → %d %s", y.kod, y.govde)
	}
	if y := pk_istek(t, h, "admin", "DELETE", yol, nil); y.kod != http.StatusConflict || !strings.Contains(y.govde, "son plan") {
		t.Errorf("yönetici bayinin son planını sildi → %d %s", y.kod, y.govde)
	}
	y = bp_istek(t, h, "C", "POST", "/plans", govde("C Ucuncu", 1000, 1000, nil))
	var ucuncu struct{ ID int64 }
	_ = json.Unmarshal([]byte(y.govde), &ucuncu)
	if y.kod != http.StatusCreated {
		t.Fatalf("C üçüncü plan → %d %s", y.kod, y.govde)
	}
	if y := bp_istek(t, h, "C", "DELETE", yol, nil); y.kod != http.StatusOK {
		t.Fatalf("C varsayılan (ama son olmayan) planını silemedi → %d %s", y.kod, y.govde)
	}
	if v := bp_varsayilanlar(t, db, 4); len(v) != 1 || v[0] != ucuncu.ID {
		t.Errorf("silinen varsayılanın yerine geçen: %v (beklenen [%d])", v, ucuncu.ID)
	}
	if n := bp_sayi(t, db, `SELECT COUNT(*) FROM audit_log WHERE action='plan.sil' AND reseller_id=4`); n != 2 {
		t.Errorf("plan.sil denetimi: %d", n)
	}

	// Paket ipuçları.
	var s map[string]any
	y = bp_istek(t, h, "C", "GET", "/plans/bayi-sinir", nil)
	_ = json.Unmarshal([]byte(y.govde), &s)
	if y.kod != http.StatusOK || s["bayi"] != true || s["max_disk_mb"] != float64(10000) || s["fazla_satis"] != false ||
		s["mail_max_email"] != float64(10) || !strings.Contains(y.govde, "io_read_mbps") {
		t.Errorf("C bayi-sinir → %d %s", y.kod, y.govde)
	}
	if y := pk_istek(t, h, "admin", "GET", "/plans/bayi-sinir", nil); y.kod != http.StatusOK || !strings.Contains(y.govde, `"bayi":false`) || strings.Contains(y.govde, "max_disk_mb") {
		t.Errorf("yönetici bayi-sinir → %d %s", y.kod, y.govde)
	}
}

// ── IDOR / kapsam ───────────────────────────────────────────────────────────

func TestBayiPlan_KapsamIDOR(t *testing.T) {
	db := bp_geciciDB(t)
	bp_uzlastir(t, db)
	h := bp_router(db)
	bp_uygulayici(t)
	cid := bp_cPlani(t, db)
	cYol := fmt.Sprintf("/plans/%d", cid)
	once := bp_oku(t, db, cid)
	kokOnce := bp_oku(t, db, 1)
	g := map[string]any{"ad": "ele gecirildi", "disk_kota_mb": 5, "trafik_kota_mb": 5, "nginx_ek_direktifler": ""}

	// Bayi A, bayi C'nin planını göremez / düzenleyemez / silemez / varsayılan yapamaz.
	if y := pk_istek(t, h, "A", "GET", cYol, nil); y.kod != http.StatusNotFound || strings.Contains(y.govde, "bayic") {
		t.Errorf("IDOR: A GET C planı → %d %s", y.kod, y.govde)
	}
	if p := pk_planlar(t, pk_istek(t, h, "A", "GET", "/plans", nil)); p[cid] != nil {
		t.Errorf("IDOR: A listesinde C planı")
	}
	for _, c := range []struct{ yontem, yol string }{{"PUT", cYol}, {"DELETE", cYol}, {"POST", cYol + "/varsayilan"}} {
		if y := pk_istek(t, h, "A", c.yontem, c.yol, g); y.kod != http.StatusForbidden || strings.Contains(y.govde, "bayic") {
			t.Errorf("IDOR: A %s %s → %d %s (beklenen 403)", c.yontem, c.yol, y.kod, y.govde)
		}
	}
	// Olmayan plan bayiye yabancı planla AYNI yanıt (varlık sızmaz).
	if y := pk_istek(t, h, "A", "POST", "/plans/99999/varsayilan", nil); y.kod != http.StatusForbidden {
		t.Errorf("A olmayan plan varsayılan → %d (beklenen 403)", y.kod)
	}
	// Müşteri hiçbir plan ucuna yazamaz.
	for _, c := range []struct{ yontem, yol string }{{"POST", "/plans"}, {"PUT", cYol}, {"DELETE", cYol}, {"POST", cYol + "/varsayilan"}, {"GET", "/plans/bayi-sinir"}} {
		if y := bp_istek(t, h, "musteri", c.yontem, c.yol, g); y.kod < 400 {
			t.Errorf("müşteri %s %s → %d %s", c.yontem, c.yol, y.kod, y.govde)
		}
	}
	if sonra := bp_oku(t, db, cid); sonra != once {
		t.Errorf("yetkisiz istekler C planını değiştirdi: %+v → %+v", once, sonra)
	}
	if n := bp_sayi(t, db, `SELECT COUNT(*) FROM service_plans WHERE ad='ele gecirildi'`); n != 0 {
		t.Errorf("yetkisiz plan oluştu")
	}
	// Kökün paylaşılan planı bayiye SALT OKUNUR.
	for _, c := range []struct{ yontem, yol string }{{"PUT", "/plans/1"}, {"DELETE", "/plans/1"}, {"POST", "/plans/1/varsayilan"}} {
		if y := bp_istek(t, h, "C", c.yontem, c.yol, g); y.kod != http.StatusForbidden {
			t.Errorf("paylaşılan plan: C %s %s → %d %s (beklenen 403)", c.yontem, c.yol, y.kod, y.govde)
		}
	}
	if kok := bp_oku(t, db, 1); kok != kokOnce {
		t.Errorf("paylaşılan plan bayi isteğiyle değişti")
	}
	if v := bp_varsayilanlar(t, db, 4); len(v) != 1 || v[0] != cid {
		t.Errorf("C varsayılanı bozuldu: %v", v)
	}
	// Yönetici her planı düzenler; bayi planını varsayılan yapınca kapsam planın SAHİBİ.
	if y := pk_istek(t, h, "admin", "PUT", cYol, map[string]any{"ad": "bayic Varsayılan", "disk_kota_mb": 1500,
		"trafik_kota_mb": 9999, "varsayilan": true, "nginx_ek_direktifler": ""}); y.kod != http.StatusOK {
		t.Fatalf("yönetici C planını düzenleyemedi → %d %s", y.kod, y.govde)
	}
	if v := bp_varsayilanlar(t, db, 0); len(v) != 1 || v[0] != 1 {
		t.Errorf("yönetici bayi planını varsayılan yapınca kökün varsayılanı sıfırlandı: %v", v)
	}
	if y := pk_istek(t, h, "admin", "POST", "/plans/201/varsayilan", nil); y.kod != http.StatusOK {
		t.Fatalf("yönetici A planını varsayılan yapamadı → %d %s", y.kod, y.govde)
	}
	if v := bp_varsayilanlar(t, db, 2); len(v) != 1 || v[0] != 201 {
		t.Errorf("A varsayılanları %v (beklenen [201])", v)
	}
	if v := bp_varsayilanlar(t, db, 0); len(v) != 1 || v[0] != 1 {
		t.Errorf("kökün varsayılanı değişti: %v", v)
	}
	if v := bp_varsayilanlar(t, db, 4); len(v) != 1 || v[0] != cid {
		t.Errorf("başka bayinin varsayılanı değişti: %v", v)
	}
	// Hostinge özel (eski kopya) plan varsayılan yapılamaz.
	if y := pk_istek(t, h, "admin", "POST", "/plans/210/varsayilan", nil); y.kod != http.StatusBadRequest {
		t.Errorf("özel plan varsayılan → %d %s", y.kod, y.govde)
	}
}

func httptestIstek(yontem, yol string, b []byte) *http.Request {
	r := httptest.NewRequest(yontem, yol, bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	return r
}

func httptestKayit() *httptest.ResponseRecorder { return httptest.NewRecorder() }
