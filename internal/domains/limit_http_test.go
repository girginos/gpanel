package domains

// limit_http_test.go — D1 hostinge özel limitler (GET/PUT/DELETE /domains/{id}/limitler),
// cmd/server zinciriyle (RequireAuth + SahipYonetim):
//
//   - Kapsam/IDOR: yönetici her hostingte; bayi YALNIZ kendi hostingte; başka bayinin /
//     kökün / olmayan hosting AYNI 403 gövdesi; müşteri asla.
//   - Bayi kapıları: disk 0 yasak, efektif disk havuzu (üst yazım dahil toplam),
//     posta paket tavanı, fiili kullanım, azaltma her zaman serbest.
//   - Kök kilidi + GL-06: yöneticinin yazdığı alanı bayi değiştiremez; GL-06 sınıfı
//     kilitli alanın ve kökün global plan iç ayarının değeri bayiye dönmez.
//   - Plan değişimi üst yazımı KORUR (kullanıcı kararı); eski POST /ozel-plan → 410.
//
// Gerçek MariaDB (GOSP_TEST_GECICI_DSN) gerekir; yoksa atlanır. Sistem yan etkisi
// YOK: arka plan uygulayıcıları (ozelLimitUygula, planSonrasiUygula) sahte.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/auth"
	"girginospanel/internal/efektiflimit"
	"girginospanel/internal/middleware"
)

var ol_sir = []byte("domains-ozel-limit-test-anahtari-012345678")

func ol_db(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li test atlandı")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = ""
	kok, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	ad := fmt.Sprintf("gosp_test_ozellimit_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad); _ = kok.Close() })
	cfg.DBName = ad
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var sp []string
	for _, a := range efektiflimit.Alanlar {
		sp = append(sp, "`"+a.Sutun+"` INT NOT NULL DEFAULT 0")
	}
	for _, q := range []string{
		"CREATE TABLE service_plans (id BIGINT UNSIGNED PRIMARY KEY, ad VARCHAR(64) NOT NULL UNIQUE, aciklama VARCHAR(255) NOT NULL DEFAULT '', " +
			"reseller_id BIGINT NOT NULL DEFAULT 0, domain_id BIGINT UNSIGNED NULL, varsayilan TINYINT NOT NULL DEFAULT 0, " +
			"waf_enabled TINYINT NOT NULL DEFAULT 0, waf_mode VARCHAR(10) NOT NULL DEFAULT 'on', waf_paranoia TINYINT NOT NULL DEFAULT 1, " +
			strings.Join(sp, ", ") + ") ENGINE=InnoDB",
		`CREATE TABLE users (id BIGINT UNSIGNED PRIMARY KEY, role VARCHAR(16) NOT NULL DEFAULT 'reseller',
			status VARCHAR(16) NOT NULL DEFAULT 'active', max_disk_mb BIGINT NOT NULL DEFAULT 0,
			max_trafik_mb BIGINT NOT NULL DEFAULT 0, fazla_satis TINYINT NOT NULL DEFAULT 0,
			reseller_plan_id BIGINT NOT NULL DEFAULT 0, token_gecersiz_ts BIGINT NOT NULL DEFAULT 0) ENGINE=InnoDB`,
		`CREATE TABLE ftp_accounts (id BIGINT UNSIGNED PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL,
			status VARCHAR(16) NOT NULL DEFAULT 'active', token_gecersiz_ts BIGINT NOT NULL DEFAULT 0) ENGINE=InnoDB`,
		`INSERT INTO ftp_accounts (id, domain_id) VALUES (1, 20)`,
		`CREATE TABLE reseller_plans (id BIGINT PRIMARY KEY, mail_max_email INT NOT NULL DEFAULT 0,
			mail_saatlik_limit INT NOT NULL DEFAULT 0, mail_kutu_kota_mb INT NOT NULL DEFAULT 0) ENGINE=InnoDB`,
		`CREATE TABLE domains (id BIGINT UNSIGNED PRIMARY KEY, alan_adi VARCHAR(253) NOT NULL UNIQUE,
			sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '', reseller_id BIGINT NOT NULL DEFAULT 0,
			plan_id BIGINT UNSIGNED NULL, boyut_kb BIGINT NOT NULL DEFAULT 0,
			disk_kota_mb INT NOT NULL DEFAULT 0, inode_kota INT NOT NULL DEFAULT 0,
			waf_enabled TINYINT NULL, waf_mode VARCHAR(10) NULL, waf_paranoia TINYINT NULL) ENGINE=InnoDB`,
		efektiflimit.TabloDDL,
		`CREATE TABLE audit_log (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, actor_user_id BIGINT NULL,
			actor_username VARCHAR(64) NULL, ip VARCHAR(64) NULL, action VARCHAR(64) NULL, target VARCHAR(255) NULL,
			detail TEXT NULL, ok TINYINT NULL, reseller_id BIGINT NOT NULL DEFAULT 0) ENGINE=InnoDB`,
		`INSERT INTO users (id, role, max_disk_mb, max_trafik_mb, reseller_plan_id) VALUES
			(1,'admin',0,0,0), (2,'reseller',10000,100000,5), (3,'reseller',10000,100000,0)`,
		`INSERT INTO reseller_plans VALUES (5, 10, 100, 1000)`,
		"INSERT INTO service_plans (id, ad, reseller_id, disk_kota_mb, trafik_kota_mb, cpu_yuzde, ram_mb, max_process, io_agirlik, " +
			"io_read_mbps, db_max_query_seconds, max_email, saatlik_mail_limiti, mail_kutu_kota_mb) VALUES " +
			"(1,'Kok Standart',0, 2000,20000, 100,2048,60,100, 40,30, 5,50,500), " +
			"(200,'A Plani',2, 3000,30000, 200,1024,50,100, 0,0, 5,50,500), " +
			"(300,'B Plani',3, 1000,10000, 100,1024,50,100, 0,0, 5,50,500)",
		`INSERT INTO domains (id, alan_adi, reseller_id, plan_id) VALUES
			(10,'kok.test',0,1), (20,'a1.test',2,1), (21,'a2.test',2,200), (30,'b1.test',3,300)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("şema: %v\n%s", err, q)
		}
	}
	middleware.Init(db)
	t.Cleanup(func() { middleware.Init(nil) })
	// Arka plan uygulayıcıları sahte: systemd/nginx/mail eklentisine dokunulmaz.
	var mu sync.Mutex
	eskiL, eskiP := ozelLimitUygula, planSonrasiUygula
	ozelLimitUygula = func(*sql.DB, int64, string) { mu.Lock(); mu.Unlock() }
	planSonrasiUygula = func(*sql.DB, int64, string) { mu.Lock(); mu.Unlock() }
	t.Cleanup(func() { ozelLimitUygula, planSonrasiUygula = eskiL, eskiP })
	return db
}

func ol_router(db *sql.DB) http.Handler {
	dh := &Handlers{DB: db}
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(ol_sir))
		r.With(middleware.SahipYonetim).Get("/domains/{id}/limitler", dh.LimitGetir)
		r.With(middleware.SahipYonetim).Put("/domains/{id}/limitler", dh.LimitKaydet)
		r.With(middleware.SahipYonetim).Delete("/domains/{id}/limitler", dh.LimitSifirla)
		r.With(middleware.SahipYonetim).Put("/domains/{id}/plan", dh.SetPlan)
		r.With(middleware.SahipYonetim).Post("/domains/{id}/ozel-plan", dh.OzelPlan)
	})
	return r
}

func ol_jeton(t *testing.T, kim string) string {
	t.Helper()
	var s string
	var err error
	switch kim {
	case "admin":
		s, err = auth.IssueAt(ol_sir, 3600, 1, "root", "admin", 0, "")
	case "A":
		s, err = auth.IssueResellerAt(ol_sir, 3600, 2, "bayiA", 2, 0, "")
	case "B":
		s, err = auth.IssueResellerAt(ol_sir, 3600, 3, "bayiB", 3, 0, "")
	case "musteri":
		s, _, err = auth.GenerateMusteri(ol_sir, auth.MusteriClaims{FTPHesapID: 1, DomainID: 20, Kullanici: "m", AlanAdi: "a1.test"}, 3600)
	}
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func ol_istek(t *testing.T, h http.Handler, kim, yontem, yol string, govde any) (int, string) {
	t.Helper()
	var b []byte
	if govde != nil {
		b, _ = json.Marshal(govde)
	}
	req := httptest.NewRequest(yontem, yol, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+ol_jeton(t, kim))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func ol_ust(m map[string]any) map[string]any { return map[string]any{"ust": m} }

func ol_oku(t *testing.T, db *sql.DB, did int64) efektiflimit.Sonuc {
	t.Helper()
	s, err := efektiflimit.Oku(context.Background(), db, did)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type ol_alan struct {
	Ad      string `json:"ad"`
	Plan    *int   `json:"plan"`
	Ust     *int   `json:"ust"`
	Efektif *int   `json:"efektif"`
	Gizli   bool   `json:"gizli"`
	Kilitli bool   `json:"kilitli"`
}

func ol_alanlar(t *testing.T, govde string) map[string]ol_alan {
	t.Helper()
	var y struct {
		Alanlar []ol_alan `json:"alanlar"`
	}
	if err := json.Unmarshal([]byte(govde), &y); err != nil {
		t.Fatalf("yanıt çözülemedi: %v (%s)", err, govde)
	}
	m := map[string]ol_alan{}
	for _, a := range y.Alanlar {
		m[a.Ad] = a
	}
	return m
}

// ── Kapsam / IDOR ───────────────────────────────────────────────────────────

func TestOzelLimit_KapsamIDOR(t *testing.T) {
	db := ol_db(t)
	h := ol_router(db)
	for _, did := range []int64{10, 20, 30} {
		if kod, g := ol_istek(t, h, "admin", "GET", fmt.Sprintf("/domains/%d/limitler", did), nil); kod != 200 {
			t.Errorf("yönetici GET %d → %d %s", did, kod, g)
		}
	}
	for _, did := range []int64{20, 21} {
		if kod, g := ol_istek(t, h, "A", "GET", fmt.Sprintf("/domains/%d/limitler", did), nil); kod != 200 {
			t.Errorf("bayi A kendi hostingi %d → %d %s", did, kod, g)
		}
	}
	// Başka bayinin, kökün ve OLMAYAN hosting: aynı kod, aynı gövde (varlık sızmaz).
	var ilk string
	for _, yol := range []string{"/domains/30/limitler", "/domains/10/limitler", "/domains/999/limitler"} {
		for _, yon := range []string{"GET", "PUT", "DELETE"} {
			kod, g := ol_istek(t, h, "A", yon, yol, ol_ust(map[string]any{"ram_mb": 9999}))
			if kod != http.StatusForbidden {
				t.Errorf("bayi A %s %s → %d %s (beklenen 403)", yon, yol, kod, g)
			}
			if ilk == "" {
				ilk = g
			} else if g != ilk {
				t.Errorf("IDOR: %s %s gövdesi farklı: %q vs %q", yon, yol, g, ilk)
			}
		}
	}
	if s := ol_oku(t, db, 30); s.UstVar {
		t.Error("bayi A başka bayinin hostingine üst yazım yazdı")
	}
	if kod, g := ol_istek(t, h, "B", "PUT", "/domains/20/limitler", ol_ust(map[string]any{"ram_mb": 1})); kod != http.StatusForbidden {
		t.Errorf("bayi B, A'nın hostingine yazdı → %d %s", kod, g)
	}
	// Müşteri: kendi hostinginde bile hiçbir yöntem geçmez.
	for _, yon := range []string{"GET", "PUT", "DELETE"} {
		if kod, g := ol_istek(t, h, "musteri", yon, "/domains/20/limitler", ol_ust(map[string]any{"ram_mb": 1})); kod == 200 || strings.Contains(g, "alanlar") {
			t.Errorf("müşteri %s → %d %s", yon, kod, g)
		}
	}
	if s := ol_oku(t, db, 20); s.UstVar {
		t.Error("yetkisiz istek üst yazım bıraktı")
	}
	// Bilinmeyen alan / SQL adı / bilinmeyen gövde anahtarı → 400.
	for _, gv := range []any{
		ol_ust(map[string]any{"nginx_ek_direktifler": 1}),
		ol_ust(map[string]any{"ram_mb`=1, cpu_yuzde": 1}),
		map[string]any{"ust": map[string]any{}, "reseller_id": 3},
	} {
		if kod, g := ol_istek(t, h, "admin", "PUT", "/domains/20/limitler", gv); kod != http.StatusBadRequest {
			t.Errorf("geçersiz gövde %v → %d %s", gv, kod, g)
		}
	}
}

// ── Bayi kapıları ───────────────────────────────────────────────────────────

func TestOzelLimit_BayiKapilari(t *testing.T) {
	db := ol_db(t)
	h := ol_router(db)
	put := func(did int64, m map[string]any) (int, string) {
		return ol_istek(t, h, "A", "PUT", fmt.Sprintf("/domains/%d/limitler", did), ol_ust(m))
	}
	// A'nın taahhüdü: 20 (plan 1: 2000) + 21 (plan 200: 3000) = 5000 / 10000 MB.
	if kod, g := put(20, map[string]any{"disk_kota_mb": 0}); kod != http.StatusBadRequest {
		t.Errorf("bayi disk=0 (sınırsız) → %d %s", kod, g)
	}
	if kod, g := put(20, map[string]any{"disk_kota_mb": 7000}); kod != http.StatusForbidden || !strings.Contains(g, "disk kotanız") {
		t.Errorf("havuz aşımı (5000-2000+7000 ≥ 10000) → %d %s", kod, g)
	}
	if kod, g := put(20, map[string]any{"disk_kota_mb": 6000}); kod != 200 {
		t.Fatalf("havuz içinde (9000) → %d %s", kod, g)
	}
	if e := ol_oku(t, db, 20).Efektif("disk_kota_mb", -1); e != 6000 {
		t.Errorf("efektif disk %d, beklenen 6000", e)
	}
	// 🔴 Havuz toplamı ÜST YAZIMI saymalı: 9000 - 3000 + 4500 = 10500 ≥ 10000 → red.
	// (Toplam yalnız plandan okunsaydı 5000 - 3000 + 4500 = 6500 geçerdi.)
	if kod, g := put(21, map[string]any{"disk_kota_mb": 4500}); kod != http.StatusForbidden {
		t.Errorf("üst yazımlı taahhüt havuzu aşıyor → %d %s", kod, g)
	}
	// Posta tavanı (bayi paketi: 10 kutu / 100 saatlik / 1000 MB).
	if kod, g := put(20, map[string]any{"disk_kota_mb": 6000, "max_email": 50}); kod != http.StatusBadRequest {
		t.Errorf("posta tavanı aşımı → %d %s", kod, g)
	}
	if kod, g := put(20, map[string]any{"disk_kota_mb": 6000, "max_email": 8}); kod != 200 {
		t.Errorf("tavan içinde posta → %d %s", kod, g)
	}
	// Fiili kullanım: 21'de 5000 MB veri varken disk 4000'e indirilemez.
	if _, err := db.Exec(`UPDATE domains SET boyut_kb=? WHERE id=21`, 5000*1024); err != nil {
		t.Fatal(err)
	}
	if kod, g := put(21, map[string]any{"disk_kota_mb": 4000}); kod != http.StatusConflict {
		t.Errorf("kullanımın altına disk → %d %s", kod, g)
	}
	// Azaltma havuz aşılmış olsa bile serbest.
	if _, err := db.Exec(`UPDATE users SET max_disk_mb=1000 WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	if kod, g := put(20, map[string]any{"disk_kota_mb": 5000, "max_email": 8}); kod != 200 {
		t.Errorf("azaltma (havuz zaten dolu) → %d %s", kod, g)
	}
	// Artış dolu havuzda reddedilir.
	if kod, g := put(20, map[string]any{"disk_kota_mb": 5500, "max_email": 8}); kod != http.StatusForbidden {
		t.Errorf("dolu havuzda artış → %d %s", kod, g)
	}
	// Plana dönüş (DELETE) her zaman serbest bırakmaz: plan 2000 < 5000 → azaltma, geçer.
	if kod, g := ol_istek(t, h, "A", "DELETE", "/domains/20/limitler", nil); kod != 200 {
		t.Errorf("plana dönüş → %d %s", kod, g)
	}
	if ol_oku(t, db, 20).UstVar {
		t.Error("DELETE üst yazımı silmedi")
	}
}

// ── Kök kilidi + GL-06 ──────────────────────────────────────────────────────

func TestOzelLimit_KokKilidiVeGL06(t *testing.T) {
	db := ol_db(t)
	h := ol_router(db)
	// Kökün global planı (1): bayiye iç ayarlar (io/governor/process) gizli.
	_, g := ol_istek(t, h, "A", "GET", "/domains/20/limitler", nil)
	al := ol_alanlar(t, g)
	if a := al["io_read_mbps"]; a.Plan != nil || a.Efektif != nil || !a.Gizli {
		t.Errorf("GL-06: kökün planındaki io_read_mbps bayiye döndü: %+v", a)
	}
	if a := al["db_max_query_seconds"]; a.Plan != nil || !a.Gizli {
		t.Errorf("GL-06: db_max_query_seconds bayiye döndü: %+v", a)
	}
	if a := al["disk_kota_mb"]; a.Plan == nil || *a.Plan != 2000 || a.Gizli {
		t.Errorf("özet alanı (disk) bayiye görünmeli: %+v", a)
	}
	// Bayinin kendi planı (200): hepsi görünür.
	_, g = ol_istek(t, h, "A", "GET", "/domains/21/limitler", nil)
	if a := ol_alanlar(t, g)["io_read_mbps"]; a.Gizli || a.Plan == nil {
		t.Errorf("bayinin kendi planı gizlendi: %+v", a)
	}
	// Yönetici A'nın hostingine yazar → alanlar kilitlenir.
	if kod, g := ol_istek(t, h, "admin", "PUT", "/domains/20/limitler", ol_ust(map[string]any{"io_read_mbps": 77, "ram_mb": 4096})); kod != 200 {
		t.Fatalf("yönetici PUT → %d %s", kod, g)
	}
	_, g = ol_istek(t, h, "A", "GET", "/domains/20/limitler", nil)
	if strings.Contains(g, "77") {
		t.Errorf("GL-06: kökün kilitli io değeri bayi yanıtında: %s", g)
	}
	al = ol_alanlar(t, g)
	if a := al["io_read_mbps"]; a.Ust != nil || a.Efektif != nil || !a.Kilitli || !a.Gizli {
		t.Errorf("kilitli gizli alan: %+v", a)
	}
	if a := al["ram_mb"]; a.Ust == nil || *a.Ust != 4096 || !a.Kilitli {
		t.Errorf("kilitli özet alanı (ram) bayiye kilitli görünmeli: %+v", a)
	}
	// Bayi kilitli alanı değiştiremez / silemez.
	if kod, g := ol_istek(t, h, "A", "PUT", "/domains/20/limitler", ol_ust(map[string]any{"ram_mb": 1000})); kod != http.StatusForbidden {
		t.Errorf("bayi kilitli ram'i değiştirdi → %d %s", kod, g)
	}
	if kod, g := ol_istek(t, h, "A", "PUT", "/domains/20/limitler", ol_ust(map[string]any{"io_read_mbps": nil})); kod != http.StatusForbidden {
		t.Errorf("bayi kilitli io'yu sildi → %d %s", kod, g)
	}
	// Kilitli alanları göndermeden kendi alanını yazabilir; kilitliler korunur.
	if kod, g := ol_istek(t, h, "A", "PUT", "/domains/20/limitler", ol_ust(map[string]any{"cpu_yuzde": 150})); kod != 200 {
		t.Fatalf("bayi kendi alanı → %d %s", kod, g)
	}
	s := ol_oku(t, db, 20)
	if s.Efektif("io_read_mbps", -1) != 77 || s.Efektif("ram_mb", -1) != 4096 || s.Efektif("cpu_yuzde", -1) != 150 ||
		s.Degerler["cpu_yuzde"].Kok || !s.Degerler["ram_mb"].Kok {
		t.Errorf("kilit/bayi alanı karışık: io=%d ram=%d cpu=%d kok=%v", s.Efektif("io_read_mbps", -1),
			s.Efektif("ram_mb", -1), s.Efektif("cpu_yuzde", -1), s.KokAlanlar)
	}
	// Bayinin "plana dön"ü yalnız kendi alanlarını siler.
	if kod, g := ol_istek(t, h, "A", "DELETE", "/domains/20/limitler", nil); kod != 200 {
		t.Fatalf("bayi DELETE → %d %s", kod, g)
	}
	s = ol_oku(t, db, 20)
	if s.Degerler["cpu_yuzde"].Ust != nil || s.Efektif("io_read_mbps", -1) != 77 || s.Efektif("ram_mb", -1) != 4096 {
		t.Errorf("bayi DELETE kilitli alanlara dokundu ya da kendi alanını silmedi")
	}
	// Yönetici her şeyi görür; denetim kaydı bayi kapsamına GL-06 değeri yazmaz.
	_, g = ol_istek(t, h, "admin", "GET", "/domains/20/limitler", nil)
	if a := ol_alanlar(t, g)["io_read_mbps"]; a.Ust == nil || *a.Ust != 77 {
		t.Errorf("yönetici kilitli değeri görmeli: %+v", a)
	}
	var detay string
	_ = db.QueryRow(`SELECT detail FROM audit_log WHERE action='hosting.limit' AND reseller_id=2 ORDER BY id LIMIT 1`).Scan(&detay)
	if !strings.Contains(detay, "io_read_mbps: değişti") || strings.Contains(detay, "77") || !strings.Contains(detay, "ram_mb: plan→4096") {
		t.Errorf("denetim detayı: %s", detay)
	}
}

// ── Plan değişimi + eski uç ─────────────────────────────────────────────────

func TestOzelLimit_PlanDegisimindeKorunurVeEskiUc410(t *testing.T) {
	db := ol_db(t)
	h := ol_router(db)
	if kod, g := ol_istek(t, h, "A", "PUT", "/domains/21/limitler", ol_ust(map[string]any{"ram_mb": 3072})); kod != 200 {
		t.Fatalf("PUT → %d %s", kod, g)
	}
	// Bayi kendi hostingini kökün global planına geçirir: üst yazım KORUNUR.
	if kod, g := ol_istek(t, h, "A", "PUT", "/domains/21/plan", map[string]any{"plan_id": 1}); kod != 200 {
		t.Fatalf("plan değişimi → %d %s", kod, g)
	}
	s := ol_oku(t, db, 21)
	if s.PlanID == nil || *s.PlanID != 1 || s.Efektif("ram_mb", -1) != 3072 || s.Efektif("cpu_yuzde", -1) != 100 {
		t.Errorf("plan değişiminde üst yazım korunmadı: plan=%v ram=%d cpu=%d", s.PlanID, s.Efektif("ram_mb", -1), s.Efektif("cpu_yuzde", -1))
	}
	// Plan değişiminde havuz kapısı korunan üst yazımı sayar: disk üst yazımı 7000
	// iken (taahhüt 2000+7000=9000) hosting 20'yi 3000'lik plana geçirmek 10000'e
	// ulaşır → red (yalnız plan sayılsaydı 2000-2000+3000 geçerdi).
	if _, err := db.Exec(`INSERT INTO hosting_limit_ust (domain_id, disk_kota_mb) VALUES (21, 7000)
		ON DUPLICATE KEY UPDATE disk_kota_mb=7000`); err != nil {
		t.Fatal(err)
	}
	if kod, g := ol_istek(t, h, "A", "PUT", "/domains/20/plan", map[string]any{"plan_id": 200}); kod != http.StatusForbidden {
		t.Errorf("üst yazımlı taahhütte plan yükseltmesi havuzu aştı → %d %s", kod, g)
	}
	// Eski "tek tıkla özel plan" ucu 410 ve plan üretmez.
	var once int
	_ = db.QueryRow(`SELECT COUNT(*) FROM service_plans`).Scan(&once)
	for _, kim := range []string{"admin", "A"} {
		if kod, g := ol_istek(t, h, kim, "POST", "/domains/20/ozel-plan", map[string]any{}); kod != http.StatusGone {
			t.Errorf("%s POST /ozel-plan → %d %s (beklenen 410)", kim, kod, g)
		}
	}
	var sonra int
	_ = db.QueryRow(`SELECT COUNT(*) FROM service_plans`).Scan(&sonra)
	if sonra != once {
		t.Errorf("eski uç plan üretti: %d → %d", once, sonra)
	}
}

// Sınırlı havuzlu bayi, kökün SINIRSIZ (disk 0) planındaki hostingini üst yazımı
// silerek sınırsıza geri döndüremez (havuz dışına çıkmak = artış).
func TestOzelLimit_BayiSinirsizaDonemez(t *testing.T) {
	db := ol_db(t)
	h := ol_router(db)
	if _, err := db.Exec(`UPDATE service_plans SET disk_kota_mb=0 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	// Sınırsızdan 3000'e: azaltma, geçer.
	if kod, g := ol_istek(t, h, "A", "PUT", "/domains/20/limitler", ol_ust(map[string]any{"disk_kota_mb": 3000})); kod != 200 {
		t.Fatalf("sınırsızdan limitliye → %d %s", kod, g)
	}
	// Plana dön = sınırsız: red.
	if kod, g := ol_istek(t, h, "A", "DELETE", "/domains/20/limitler", nil); kod != http.StatusForbidden || !strings.Contains(g, "sınırsız") {
		t.Errorf("bayi sınırsıza döndü → %d %s", kod, g)
	}
	if e := ol_oku(t, db, 20).Efektif("disk_kota_mb", -1); e != 3000 {
		t.Errorf("reddedilen istek üst yazımı değiştirdi: %d", e)
	}
	// Yönetici serbest.
	if kod, g := ol_istek(t, h, "admin", "DELETE", "/domains/20/limitler", nil); kod != 200 {
		t.Errorf("yönetici plana dönüş → %d %s", kod, g)
	}
}
