package toplu

// toplu_plan_uygun_test.go — toplu plan değişiminde plan sahipliği (planHedeflereUygun).
// 🔴 Eskiden yalnız planın varlığına bakılıyordu: kök, A bayisinin planını kendi ya da
// B bayisinin hostinglerine bağlayabiliyordu. Kökün planı herkese uygun; bayi planı
// yalnız o bayinin hostinglerine. Harness: devir_test.go (dt_geciciDB).

import (
	"context"
	"net/http"
	"testing"
)

func TestPlanHedeflereUygun_BayiPlaniYalnizKendiHostinglerine(t *testing.T) {
	db := dt_geciciDB(t)
	dt_exec(t, db, `INSERT INTO domains (id, alan_adi, reseller_id) VALUES
		(10,'a1.test',5), (11,'a2.test',5), (12,'kok.test',0), (13,'b1.test',6)`)
	dt_exec(t, db, `INSERT INTO service_plans (id, ad, reseller_id, domain_id) VALUES
		(1,'Kok Standart',0,NULL), (2,'A Bayi Plani',5,NULL), (3,'a1 ozel kopya',5,10)`)
	ctx := context.Background()
	for _, c := range []struct {
		ad   string
		plan int64
		ids  []int64
		kod  int
	}{
		{"kök planı herkese", 1, []int64{10, 12, 13}, 0},
		{"bayi planı kendi hostinglerine", 2, []int64{10, 11}, 0},
		{"bayi planı kök hostingine", 2, []int64{10, 12}, http.StatusBadRequest},
		{"bayi planı başka bayinin hostingine", 2, []int64{13}, http.StatusBadRequest},
		{"hostinge özel kopya toplu bağlanamaz", 3, []int64{10}, http.StatusBadRequest},
		{"olmayan plan", 999, []int64{10}, http.StatusBadRequest},
	} {
		if kod, mesaj := planHedeflereUygun(ctx, db, c.plan, c.ids); kod != c.kod {
			t.Errorf("%s: kod=%d (%s); beklenen %d", c.ad, kod, mesaj, c.kod)
		}
	}
}
