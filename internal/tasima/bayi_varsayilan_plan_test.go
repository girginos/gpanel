package tasima

import (
	"net/http"
	"strings"
	"testing"
)

// Plan seçmeden başlatılan bayi taşıması bayinin KENDİ varsayılan planını kullanır;
// planı var ama varsayılanı yoksa plans.BayiVarsayilanIsaretle en eskisini işaretler
// (D3a "varsayılan planı olmayan bayi taşıma yapamıyor" açığı). Bayi 9'un planı 2 var
// ama varsayılan değil → plan 2 işaretlenir ve iş kabul edilir. Bayiye plan
// OLUŞTURULMAZ (TestD3_BaslatBayiKendiPlaniYoksaGenelVarsayilan).
func TestD3_BaslatBayiVarsayilanPlanSaglanir(t *testing.T) {
	o := d3Kur(t)
	o.exec(t, `ALTER TABLE users ADD reseller_plan_id BIGINT NOT NULL DEFAULT 0`)
	o.bayiAc(t, true)
	ot := o.bayiOturum(t, 9, []Hesap{{KaynakHesap: "u", AlanAdi: "v1.test", WebRoot: "/home/u/v1"}})
	kod, g := o.istek(t, "bayi9", http.MethodPost, "/tasima/baslat", o.baslatGovde(ot, []Hesap{{AlanAdi: "v1.test"}}, Ayarlar{}))
	if kod != http.StatusAccepted {
		t.Fatalf("plansız istek bayinin varsayılan planıyla kabul edilmeli: %d %s", kod, g)
	}
	var v2, v3, v1 int
	_ = o.db.QueryRow(`SELECT varsayilan FROM service_plans WHERE id=2`).Scan(&v2)
	_ = o.db.QueryRow(`SELECT varsayilan FROM service_plans WHERE id=3`).Scan(&v3)
	_ = o.db.QueryRow(`SELECT varsayilan FROM service_plans WHERE id=1`).Scan(&v1)
	if v2 != 1 || v3 != 1 || v1 != 0 {
		t.Errorf("varsayılan işaretleri: bayi9 plan2=%d (beklenen 1), bayi7 plan3=%d (1), kök plan1=%d (0)", v2, v3, v1)
	}
	var ayar string
	_ = o.db.QueryRow(`SELECT CAST(ayarlar AS CHAR) FROM tasima_isleri WHERE sahip_rid=9 ORDER BY id DESC LIMIT 1`).Scan(&ayar)
	if !strings.Contains(ayar, `"plan_id":2`) {
		t.Errorf("iş bayinin varsayılan planıyla (2) kaydedilmeli: %s", ayar)
	}
}

// Kendi planı OLMAYAN bayinin plansız taşıması kökün paylaşılan (global) varsayılan
// planını kullanır; bayiye "<bayi> Varsayılan" planı AÇILMAZ (kullanıcı bildirimi
// 2026-10-12: "100 bayi = 100 hosting planı").
func TestD3_BaslatBayiKendiPlaniYoksaGenelVarsayilan(t *testing.T) {
	o := d3Kur(t)
	o.exec(t, `UPDATE service_plans SET reseller_id=7 WHERE id=2`) // bayi 9'un kendi planı kalmaz
	o.exec(t, `UPDATE service_plans SET varsayilan=1 WHERE id=1`)  // kökün global varsayılanı
	o.bayiAc(t, true)
	var once int
	_ = o.db.QueryRow(`SELECT COUNT(*) FROM service_plans`).Scan(&once)
	ot := o.bayiOturum(t, 9, []Hesap{{KaynakHesap: "u", AlanAdi: "g1.test", WebRoot: "/home/u/g1"}})
	kod, g := o.istek(t, "bayi9", http.MethodPost, "/tasima/baslat", o.baslatGovde(ot, []Hesap{{AlanAdi: "g1.test"}}, Ayarlar{}))
	if kod != http.StatusAccepted {
		t.Fatalf("plansız istek kökün varsayılan planıyla kabul edilmeli: %d %s", kod, g)
	}
	var sonra, kendi int
	_ = o.db.QueryRow(`SELECT COUNT(*) FROM service_plans`).Scan(&sonra)
	_ = o.db.QueryRow(`SELECT COUNT(*) FROM service_plans WHERE reseller_id=9`).Scan(&kendi)
	if sonra != once || kendi != 0 {
		t.Errorf("bayiye plan açıldı: toplam %d → %d, bayi 9'un planı %d", once, sonra, kendi)
	}
	var ayar string
	_ = o.db.QueryRow(`SELECT CAST(ayarlar AS CHAR) FROM tasima_isleri WHERE sahip_rid=9 ORDER BY id DESC LIMIT 1`).Scan(&ayar)
	if !strings.Contains(ayar, `"plan_id":1`) {
		t.Errorf("iş kökün varsayılan planıyla (1) kaydedilmeli: %s", ayar)
	}
}
