package domains

// db_kota_yaris_test.go — "Yeni Veritabanı" DB SAYISI kapısı eşzamanlı isteklerle aşılamaz.
//
// kota.CheckDBEklenebilir (COUNT) ile oluşturma arasında kilit yokken, aynı hostinge
// eşzamanlı N istek aynı COUNT'u okuyup hepsi geçiyor ve max_db sınırı aşılıyordu.
// Gerçek MariaDB gerekir (GOSP_TEST_GECICI_DSN); yoksa ATLANIR.

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
)

func TestCreateDatabase_EszamanliIstekMaxDBAsmaz(t *testing.T) {
	o := dbo_ortam(t)
	for _, q := range []string{
		`INSERT INTO service_plans (id, max_db) VALUES (1, 2)`,
		`UPDATE domains SET plan_id=1 WHERE id=1`,
	} {
		if _, err := o.h.DB.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	const n = 8
	kodlar := make([]int, n)
	var wg sync.WaitGroup
	basla := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-basla
			kodlar[i], _ = o.istek(t, http.MethodPost, "/domains/1/databases",
				dboYeniDB(fmt.Sprintf("kt%d", i), fmt.Sprintf("ku%d", i), "GucluParola12345"))
		}(i)
	}
	close(basla)
	wg.Wait()

	basari := 0
	for _, k := range kodlar {
		if k == http.StatusCreated {
			basari++
		}
	}
	var kayit int
	if err := o.h.DB.QueryRow(`SELECT COUNT(*) FROM db_accounts WHERE domain_id=1`).Scan(&kayit); err != nil {
		t.Fatal(err)
	}
	if basari != 2 || kayit != 2 {
		t.Fatalf("max_db=2: %d istek başarılı, %d kayıt (beklenen 2/2) — kodlar=%v", basari, kayit, kodlar)
	}
}
