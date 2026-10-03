package efektiflimit

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func ptr(n int) *int { return &n }

func TestIfade_SiraVeMiras(t *testing.T) {
	if got := Ifade("ram_mb"); got != "COALESCE(u.`ram_mb`, p.`ram_mb`)" {
		t.Errorf("ram_mb: %s", got)
	}
	// disk/inode: üst yazım > domains.* (0037, >0) > plan
	if got := Ifade("disk_kota_mb"); got != "COALESCE(u.`disk_kota_mb`, NULLIF(d.`disk_kota_mb`,0), p.`disk_kota_mb`)" {
		t.Errorf("disk: %s", got)
	}
	if got := IfadePlanla("trafik_kota_mb", "np"); got != "COALESCE(u.`trafik_kota_mb`, np.`trafik_kota_mb`)" {
		t.Errorf("planla: %s", got)
	}
	for _, f := range []func(){
		func() { Ifade("nginx_ek_direktifler") },
		func() { Ifade("x; DROP TABLE domains") },
		func() { IfadePlanla("ram_mb", "p; --") },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("bilinmeyen alan/takma ad panik vermedi")
				}
			}()
			f()
		}()
	}
	if len(Alanlar) != 22 {
		t.Errorf("üst yazılabilir alan sayısı %d, beklenen 22", len(Alanlar))
	}
}

func TestDogrula(t *testing.T) {
	durumlar := []struct {
		ad   string
		ust  map[string]*int
		bayi bool
		hata bool
	}{
		{"boş", map[string]*int{}, false, false},
		{"nil = plandan", map[string]*int{"ram_mb": nil}, true, false},
		{"bilinmeyen alan", map[string]*int{"php_surum": ptr(8)}, false, true},
		{"sql adı", map[string]*int{"ram_mb`=1,cpu_yuzde": ptr(1)}, false, true},
		{"negatif", map[string]*int{"disk_kota_mb": ptr(-1)}, false, true},
		{"cpu 0 yasak", map[string]*int{"cpu_yuzde": ptr(0)}, false, true},
		{"io_agirlik üst sınır", map[string]*int{"io_agirlik": ptr(10001)}, false, true},
		{"kök disk 0 serbest", map[string]*int{"disk_kota_mb": ptr(0)}, false, false},
		{"bayi disk 0 yasak", map[string]*int{"disk_kota_mb": ptr(0)}, true, true},
		{"bayi trafik 0 yasak", map[string]*int{"trafik_kota_mb": ptr(0)}, true, true},
		{"bayi inode 0 serbest", map[string]*int{"inode_kota": ptr(0)}, true, false},
		{"geçerli", map[string]*int{"ram_mb": ptr(4096), "disk_kota_mb": ptr(20480)}, true, false},
	}
	for _, d := range durumlar {
		err := Dogrula(d.ust, d.bayi)
		if (err != nil) != d.hata {
			t.Errorf("%s: hata=%v, beklenen hata=%v", d.ad, err, d.hata)
		}
	}
}

// ── DB'li: birleştirme tablosu + Kaydet ─────────────────────────────────────

func geciciDB(t *testing.T) *sql.DB {
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
	ad := fmt.Sprintf("gosp_test_efl_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad); err != nil {
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
	for _, a := range Alanlar {
		sp = append(sp, "`"+a.Sutun+"` INT NOT NULL DEFAULT 0")
	}
	for _, q := range []string{
		"CREATE TABLE service_plans (id BIGINT UNSIGNED PRIMARY KEY, ad VARCHAR(64) NOT NULL DEFAULT '', reseller_id BIGINT NOT NULL DEFAULT 0, domain_id BIGINT UNSIGNED NULL, " + strings.Join(sp, ", ") + ") ENGINE=InnoDB",
		"CREATE TABLE domains (id BIGINT UNSIGNED PRIMARY KEY, reseller_id BIGINT NOT NULL DEFAULT 0, plan_id BIGINT UNSIGNED NULL, disk_kota_mb INT NOT NULL DEFAULT 0, inode_kota INT NOT NULL DEFAULT 0) ENGINE=InnoDB",
		TabloDDL,
		"INSERT INTO service_plans (id, ad, disk_kota_mb, inode_kota, ram_mb, cpu_yuzde, max_email) VALUES (1, 'Temel', 1000, 50000, 2048, 100, 5)",
		// 1: planlı, üst yazımsız   2: planlı + ram üst yazım + 0037 disk   3: plansız + üst yazım   4: sarkık plan_id   5: plansız
		"INSERT INTO domains (id, plan_id, disk_kota_mb) VALUES (1, 1, 0), (2, 1, 7000), (3, NULL, 0), (4, 99, 0), (5, NULL, 0)",
		"INSERT INTO hosting_limit_ust (domain_id, ram_mb, kok_alanlar) VALUES (2, 4096, 'ram_mb,olmayan')",
		"INSERT INTO hosting_limit_ust (domain_id, disk_kota_mb, inode_kota) VALUES (3, 300, 0)",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}
	return db
}

func TestOku_BirlestirmeTablosu(t *testing.T) {
	db := geciciDB(t)
	ctx := context.Background()
	oku := func(did int64) Sonuc {
		s, err := Oku(ctx, db, did)
		if err != nil {
			t.Fatalf("Oku(%d): %v", did, err)
		}
		return s
	}
	type bek struct {
		did     int64
		sutun   string
		efektif *int
		kaynak  string
	}
	for _, b := range []bek{
		{1, "ram_mb", ptr(2048), KaynakPlan},
		{1, "disk_kota_mb", ptr(1000), KaynakPlan},
		{2, "ram_mb", ptr(4096), KaynakUst},
		{2, "disk_kota_mb", ptr(7000), KaynakAlan}, // 0037 mirası plandan önce
		{2, "cpu_yuzde", ptr(100), KaynakPlan},
		{3, "disk_kota_mb", ptr(300), KaynakUst},
		{3, "inode_kota", ptr(0), KaynakUst}, // 0 = açıkça sınırsız üst yazım
		{3, "ram_mb", nil, KaynakYok},
		{4, "ram_mb", nil, KaynakYok}, // sarkık plan_id → plan yok
		{5, "disk_kota_mb", nil, KaynakYok},
	} {
		d := oku(b.did).Degerler[b.sutun]
		if d.Kaynak != b.kaynak || (d.Efektif == nil) != (b.efektif == nil) || (d.Efektif != nil && *d.Efektif != *b.efektif) {
			t.Errorf("hosting %d %s: efektif=%v kaynak=%s, beklenen %v/%s", b.did, b.sutun, d.Efektif, d.Kaynak, b.efektif, b.kaynak)
		}
	}
	s2 := oku(2)
	if !s2.UstVar || !s2.KokAlanlar["ram_mb"] || s2.KokAlanlar["olmayan"] || !s2.Degerler["ram_mb"].Kok {
		t.Errorf("üst yazım/kök kilidi çözümü yanlış: %+v", s2.KokAlanlar)
	}
	if oku(1).UstVar || !oku(1).PlanVar || oku(4).PlanVar || oku(4).PlanID == nil {
		t.Error("UstVar/PlanVar/PlanID yanlış")
	}
	if s2.Efektif("io_agirlik", 100) != 0 { // plan değeri 0 (plan var) → 0, varsayılan değil
		t.Error("plan var iken varsayılan kullanıldı")
	}
	if oku(5).Efektif("io_agirlik", 100) != 100 {
		t.Error("plansız hostingte varsayılan kullanılmadı")
	}
	if _, err := Oku(ctx, db, 404); err != sql.ErrNoRows {
		t.Errorf("olmayan hosting: %v", err)
	}
	// Ifade SQL'de aynı sonucu vermeli (havuz toplamları bunu kullanır).
	var top int
	if err := db.QueryRow("SELECT SUM(" + IfadeVarsayilan("disk_kota_mb", 0) + ") FROM domains d" + Join).Scan(&top); err != nil {
		t.Fatal(err)
	}
	if top != 1000+7000+300 {
		t.Errorf("efektif disk toplamı %d, beklenen %d", top, 8300)
	}
}

func TestKaydet_TamDegistirmeVeSilme(t *testing.T) {
	db := geciciDB(t)
	ctx := context.Background()
	if err := Kaydet(ctx, db, 1, map[string]*int{"ram_mb": ptr(3000), "cpu_yuzde": ptr(150)}, map[string]bool{"cpu_yuzde": true, "disk_kota_mb": true}, 7, nil); err != nil {
		t.Fatal(err)
	}
	s, _ := Oku(ctx, db, 1)
	if s.Efektif("ram_mb", 0) != 3000 || s.Efektif("cpu_yuzde", 0) != 150 || !s.KokAlanlar["cpu_yuzde"] || s.KokAlanlar["disk_kota_mb"] {
		t.Fatalf("kayıt: ram=%d cpu=%d kok=%v", s.Efektif("ram_mb", 0), s.Efektif("cpu_yuzde", 0), s.KokAlanlar)
	}
	// Tam değiştirme: gönderilmeyen alan plana döner.
	if err := Kaydet(ctx, db, 1, map[string]*int{"ram_mb": ptr(3500)}, nil, 7, nil); err != nil {
		t.Fatal(err)
	}
	s, _ = Oku(ctx, db, 1)
	if s.Efektif("cpu_yuzde", 0) != 100 || s.Degerler["cpu_yuzde"].Kaynak != KaynakPlan || len(s.KokAlanlar) != 0 {
		t.Errorf("tam değiştirme: cpu=%d kaynak=%s kok=%v", s.Efektif("cpu_yuzde", 0), s.Degerler["cpu_yuzde"].Kaynak, s.KokAlanlar)
	}
	// Hepsi nil → satır silinir.
	if err := Kaydet(ctx, db, 1, map[string]*int{"ram_mb": nil}, nil, 7, nil); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = db.QueryRow("SELECT COUNT(*) FROM hosting_limit_ust WHERE domain_id=1").Scan(&n)
	if n != 0 {
		t.Errorf("boş üst yazım satırı silinmedi")
	}
	if err := Kaydet(ctx, db, 1, map[string]*int{"nginx_ek_direktifler": ptr(1)}, nil, 7, nil); err == nil {
		t.Error("bilinmeyen alan kaydedildi")
	}
}

func TestEsit(t *testing.T) {
	a := Sonuc{Degerler: map[string]Deger{"ram_mb": {Efektif: ptr(1)}, "cpu_yuzde": {Efektif: nil}}}
	b := Sonuc{Degerler: map[string]Deger{"ram_mb": {Efektif: ptr(2)}, "cpu_yuzde": {Efektif: ptr(0)}}}
	f := Esit(a, b, map[string]bool{"cpu_yuzde": true})
	if len(f) != 1 || f[0] != "ram_mb" {
		t.Errorf("Esit: %v", f)
	}
}
