package plans_test

// kapsam_test.go — kapsam.go'nun dışa açık yardımcıları (BaglanabilirMi,
// DevirPlanTasi) ve uç durumlar. Fikstür/yan-etki güvenceleri kapsam_http_test.go'da.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"girginospanel/internal/efektiflimit"
	"girginospanel/internal/plans"
)

func TestBaglanabilirMi(t *testing.T) {
	db := pk_geciciDB(t)
	ctx := context.Background()
	for _, tc := range []struct {
		ad            string
		rid, plan, dm int64
		bek           error
	}{
		{"global katalog → kendi hostingi", 2, 1, 22, nil},
		{"global katalog → yeni hosting", 2, 1, 0, nil},
		{"kendi katalog", 2, 201, 21, nil},
		{"kendi özel planı → ait olduğu hosting", 2, 210, 20, nil},
		{"kendi özel planı → başka hosting", 2, 210, 21, plans.ErrOzelPlanBaskaHosting},
		{"kendi özel planı → yeni hosting", 2, 210, 0, plans.ErrOzelPlanBaskaHosting},
		{"kökün özel planı", 2, 100, 20, plans.ErrPlanKapsamDisi},
		{"kökün özel planı → yeni hosting", 2, 100, 0, plans.ErrPlanKapsamDisi},
		{"başka bayinin planı", 2, 300, 20, plans.ErrPlanKapsamDisi},
		{"olmayan plan", 2, 9999, 20, plans.ErrPlanKapsamDisi},
		{"yönetici: kök özel", 0, 100, 20, nil},
		{"yönetici: bayi planı başka hostinge", 0, 300, 20, nil},
	} {
		if err := plans.BaglanabilirMi(ctx, db, tc.rid, tc.plan, tc.dm); !errors.Is(err, tc.bek) {
			t.Errorf("%s: BaglanabilirMi(rid=%d plan=%d domain=%d) = %v, beklenen %v", tc.ad, tc.rid, tc.plan, tc.dm, err, tc.bek)
		}
	}
}

// Bayinin KENDİ hostingine ait özel plan başka bir sahipte kalmışsa (eski veri:
// devirden önce oluşmuş) bayi onu yalnız özet olarak görür, düzenleyemez; o
// planın sahibi (başka bayi) ise artık göremez.
func TestPlanKapsam_KendiHostinginYabanciSahipliOzelPlani(t *testing.T) {
	db := pk_geciciDB(t)
	pk_exec(t, db, `UPDATE service_plans SET reseller_id=3 WHERE id=210`)
	h := pk_router(db)
	p, n := pk_detay(t, pk_istek(t, h, "A", "GET", "/plans/210", nil))
	if p["ozet"] != true || p["domain_id"] != float64(20) || p["duzenlenebilir"] != false || n != 1 {
		t.Errorf("bayi A kendi hostinginin yabancı sahipli özel planı: %v (sayı %d)", p, n)
	}
	if _, var_ := p["nginx_ek_direktifler"]; var_ {
		t.Errorf("özet iç ayrıntı taşıyor: %v", p)
	}
	govde := map[string]any{"ad": "x", "disk_kota_mb": 1, "trafik_kota_mb": 1, "nginx_ek_direktifler": ""}
	if y := pk_istek(t, h, "A", "PUT", "/plans/210", govde); y.kod != http.StatusForbidden {
		t.Errorf("bayi A yabancı sahipli planı düzenledi → %d %s", y.kod, y.govde)
	}
	if y := pk_istek(t, h, "B", "GET", "/plans/210", nil); y.kod != http.StatusNotFound {
		t.Errorf("bayi B başka bayinin hostingine özel planı gördü → %d %s", y.kod, y.govde)
	}
	if y := pk_istek(t, h, "B", "PUT", "/plans/210", govde); y.kod != http.StatusForbidden {
		t.Errorf("bayi B başka bayinin hostingine özel planı düzenledi → %d %s", y.kod, y.govde)
	}
}

// D1: devir KOPYA PLAN ÜRETMEZ. Eski sahibin katalog planındaki hosting, yeni
// sahibin görebildiği en yakın katalog planına geçer; farklı her alan hostinge özel
// üst yazım olur (kökün kilidi altında) ve efektif limitler birebir aynı kalır.
func TestDevirPlanTasi_KopyaYokUstYazim(t *testing.T) {
	db := pk_geciciDB(t)
	ctx := context.Background()
	pk_exec(t, db, `UPDATE domains SET reseller_id=3 WHERE id=21`) // devir: A'nın plan 200'ü → B
	planSay := func() int {
		var n int
		_ = db.QueryRow(`SELECT COUNT(*) FROM service_plans`).Scan(&n)
		return n
	}
	devret := func(did, yeni int64) string {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		not, err := plans.DevirPlanTasi(ctx, tx, did, yeni)
		if err != nil {
			t.Fatalf("DevirPlanTasi: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		return not
	}
	n0 := planSay()
	once, err := efektiflimit.Oku(ctx, db, 21)
	if err != nil {
		t.Fatal(err)
	}
	not := devret(21, 3)
	sonra, _ := efektiflimit.Oku(ctx, db, 21)
	if f := efektiflimit.Esit(once, sonra, nil); len(f) > 0 {
		t.Errorf("devir efektif limitleri değiştirdi: %v", f)
	}
	if planSay() != n0 {
		t.Errorf("devir kopya plan üretti: %d → %d plan", n0, planSay())
	}
	// B'nin görebildiği katalog: 1 (kök global) ve 300 (B). 200'e en yakın: 300 (4 alan).
	if sonra.PlanID == nil || *sonra.PlanID != 300 {
		t.Fatalf("taban plan %v, beklenen 300 (not: %s)", sonra.PlanID, not)
	}
	for _, k := range []string{"disk_kota_mb", "trafik_kota_mb", "cpu_yuzde", "ram_mb"} {
		if d := sonra.Degerler[k]; d.Kaynak != efektiflimit.KaynakUst || !d.Kok {
			t.Errorf("%s: kaynak=%s kök kilidi=%v (beklenen üst yazım + kilit)", k, d.Kaynak, d.Kok)
		}
	}
	if !strings.Contains(not, "#300") || !strings.Contains(not, "4 alan") {
		t.Errorf("devir notu: %q", not)
	}
	// Geri devir (B → A): yine kopya yok, efektif aynı.
	pk_exec(t, db, `UPDATE domains SET reseller_id=2 WHERE id=21`)
	devret(21, 2)
	geri, _ := efektiflimit.Oku(ctx, db, 21)
	if f := efektiflimit.Esit(once, geri, nil); len(f) > 0 || planSay() != n0 {
		t.Errorf("geri devir: fark %v, plan sayısı %d (beklenen %d)", f, planSay(), n0)
	}
	if geri.PlanID == nil || *geri.PlanID == 300 {
		t.Errorf("geri devirde hosting B'nin planında kaldı: %v", geri.PlanID)
	}
}

// Eşzamanlı devir: bayinin yazma kapısı geçtikten SONRA devir planın sahipliğini
// taşırsa, bayinin UPDATE'i (satır kilidi bırakılınca) güncel sahibe bakar ve
// planı DEĞİŞTİRMEZ. Yan etki yok: hosting 20 A'da kalır, plan B'ye geçer →
// wafPlanReapply (sahip≠hosting sahibi) hiçbir hosting seçmez.
func TestPlanKapsam_EszamanliDevirdeEskiSahipYazamaz(t *testing.T) {
	db := pk_geciciDB(t)
	h := pk_router(db)
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	// Devir (işlem AÇIK): A'nın hosting 20'ye özel planı 210 → B.
	if _, err := plans.DevirPlanTasi(ctx, tx, 20, 3); err != nil {
		t.Fatal(err)
	}
	var baglanti int64
	_ = tx.QueryRow(`SELECT CONNECTION_ID()`).Scan(&baglanti)

	var wg sync.WaitGroup
	var y pk_yanit
	wg.Add(1)
	go func() {
		defer wg.Done()
		y = pk_istek(t, h, "A", "PUT", "/plans/210", map[string]any{"ad": "a-site.test — Özel", "disk_kota_mb": 500,
			"trafik_kota_mb": 500, "cpu_yuzde": 5, "ram_mb": 64, "nginx_ek_direktifler": ""})
	}()
	// A'nın UPDATE'i devrin satır kilidini beklemeye girene dek bekle.
	son := time.Now().Add(10 * time.Second)
	for {
		var n int
		_ = db.QueryRow(`SELECT COUNT(*) FROM information_schema.PROCESSLIST
			WHERE DB = DATABASE() AND ID <> CONNECTION_ID() AND ID <> ?
			  AND COMMAND IN ('Query','Execute') AND INFO LIKE 'UPDATE service_plans SET ad=%' AND TIME_MS > 100`, baglanti).Scan(&n)
		if n > 0 {
			break
		}
		if time.Now().After(son) {
			_ = tx.Rollback()
			wg.Wait()
			t.Fatalf("bayi UPDATE'i kilit beklemeye girmedi (yanıt %d %s) — senaryo kurulamadı", y.kod, y.govde)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	var cpu, ram int
	var sahip int64
	if err := db.QueryRow(`SELECT cpu_yuzde, ram_mb, reseller_id FROM service_plans WHERE id=210`).Scan(&cpu, &ram, &sahip); err != nil {
		t.Fatal(err)
	}
	if cpu != 100 || ram != 1024 || sahip != 3 {
		t.Errorf("GL-03 yarışı: eski sahip (PUT → %d) devredilen planı değiştirdi: cpu=%d ram=%d sahip=%d", y.kod, cpu, ram, sahip)
	}
}
