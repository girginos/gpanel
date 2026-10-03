package tasima

// d3a_kota_test.go — D3a: bayi taşıması bayinin plan/havuz sınırının dışına
// çıkamaz. Kalem başına GERÇEK domains.resellerKotaKontrol (kilit.Bayi altında),
// fiili disk havuzu ve plan DB limiti. h5_insertOrtam düzeneği (sahte sağlama,
// sahte kaynak limiti); GOSP_TEST_GECICI_DSN yoksa ATLANIR.

import (
	"context"
	"database/sql"
	"errors"
	"girginospanel/internal/efektiflimit"
	"strings"
	"testing"

	"girginospanel/internal/domains"
)

func d3KotaOrtam(t *testing.T) (*Handlers, *[]string) {
	t.Helper()
	h, _ := h5_insertOrtam(t)
	saglanan, _, _ := ta_kalkan(t, nil)
	// ta_kalkan kotayı sahteler; burada GERÇEK kota denetimi + havuz kullanılır.
	kotaDenetle, diskHavuzuDenetle = domains.TasimaKotaKontrol, (*Handlers).bayiDiskHavuzu
	for _, q := range []string{
		`ALTER TABLE users ADD COLUMN status VARCHAR(16) NOT NULL DEFAULT 'active',
			ADD COLUMN max_domain INT NOT NULL DEFAULT 0, ADD COLUMN max_disk_mb BIGINT NOT NULL DEFAULT 0,
			ADD COLUMN max_trafik_mb BIGINT NOT NULL DEFAULT 0, ADD COLUMN fazla_satis TINYINT NOT NULL DEFAULT 0`,
		`ALTER TABLE domains ADD COLUMN boyut_kb BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE service_plans ADD COLUMN disk_kota_mb BIGINT NOT NULL DEFAULT 0,
			ADD COLUMN trafik_kota_mb BIGINT NOT NULL DEFAULT 0, ADD COLUMN max_db INT NOT NULL DEFAULT 0`,
		`CREATE TABLE db_accounts (id BIGINT AUTO_INCREMENT PRIMARY KEY, domain_id BIGINT NOT NULL, db_name VARCHAR(64) NOT NULL)`,
		efektiflimit.TabloDDL, // 0120: efektif limit (plan ⊕ üst yazım)
	} {
		if _, err := h.DB.Exec(q); err != nil {
			t.Fatalf("kota şeması: %v\n%s", err, q)
		}
	}
	return h, saglanan
}

func d3Aktar(h *Handlers, alan string, ay Ayarlar) error {
	_, err := h.HesapAktar(context.Background(), &Kaynak{}, Hesap{AlanAdi: alan, PHPSurum: "8.3"}, ay, Kapsam{RID: 7}, func(string, ...any) {})
	return err
}

// max_domain: bayi 7'nin 1 hostingi var (bayi7.test); limit 2 → ilk kalem açılır,
// ikinci kalem kota hatasıyla (sağlamaya VARMADAN) atlanır.
func TestD3Kota_MaxDomainKalemBasina(t *testing.T) {
	h, saglanan := d3KotaOrtam(t)
	if _, err := h.DB.Exec(`UPDATE users SET max_domain=2 WHERE id=7`); err != nil {
		t.Fatal(err)
	}
	if err := d3Aktar(h, "k1.test", Ayarlar{PlanID: 3}); err != nil {
		t.Fatalf("ilk kalem kota içinde: %v", err)
	}
	err := d3Aktar(h, "k2.test", Ayarlar{PlanID: 3})
	var kh *kotaHatasi
	if !errors.As(err, &kh) || !strings.Contains(err.Error(), "2/2") {
		t.Fatalf("ikinci kalem kota hatası vermeli (bayiye kendi sayıları): %v", err)
	}
	if kalemDurumu(err) != "atlandi" {
		t.Errorf("kota hatası kalemi 'atlandi' yapmalı")
	}
	if len(*saglanan) != 1 {
		t.Errorf("kota reddi sağlamadan ÖNCE olmalı: %v", *saglanan)
	}
	var n int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM domains WHERE reseller_id=7`).Scan(&n)
	if n != 2 {
		t.Errorf("bayi 7 limiti aşmamalı: %d hosting", n)
	}
}

// İş sırasında bayi askıya alınırsa kalan kalemler açılmaz.
func TestD3Kota_AskidakiBayi(t *testing.T) {
	h, saglanan := d3KotaOrtam(t)
	if _, err := h.DB.Exec(`UPDATE users SET status='suspended' WHERE id=7`); err != nil {
		t.Fatal(err)
	}
	err := d3Aktar(h, "a1.test", Ayarlar{PlanID: 3})
	var kh *kotaHatasi
	if !errors.As(err, &kh) || !strings.Contains(err.Error(), "askıda") || len(*saglanan) != 0 {
		t.Fatalf("askıdaki bayi site açamamalı: %v sağlanan=%v", err, *saglanan)
	}
}

// Fiili disk havuzu: taşınan bayt + öteki sitelerin kullanımı > max_disk_mb → ret.
func TestD3Kota_DiskHavuzu(t *testing.T) {
	h, _ := d3KotaOrtam(t)
	if _, err := h.DB.Exec(`UPDATE users SET max_disk_mb=10 WHERE id=7`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.DB.Exec(`UPDATE domains SET boyut_kb=8*1024 WHERE id=20`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := h.bayiDiskHavuzu(ctx, 7, 999, 2<<20); err != nil {
		t.Errorf("8+2 MB = 10 MB sınırda kabul: %v", err)
	}
	err := h.bayiDiskHavuzu(ctx, 7, 999, 2<<20+1)
	var kh *kotaHatasi
	if !errors.As(err, &kh) || !strings.Contains(err.Error(), "limit 10 MB") {
		t.Errorf("havuz aşımı reddedilmeli: %v", err)
	}
	// Taşınan sitenin kendi satırı (boyut_kb henüz eski) iki kez sayılmaz.
	if err := h.bayiDiskHavuzu(ctx, 7, 20, 9<<20); err != nil {
		t.Errorf("kendi satırı hariç tutulmalı: %v", err)
	}
	if _, err := h.DB.Exec(`UPDATE users SET max_disk_mb=0 WHERE id=7`); err != nil {
		t.Fatal(err)
	}
	if err := h.bayiDiskHavuzu(ctx, 7, 999, 1<<40); err != nil {
		t.Errorf("0 = sınırsız: %v", err)
	}
}

// Plan DB limiti: kaynaktaki DB sayısı planın max_db'sini aşarsa ret.
func TestD3Kota_PlanDBLimiti(t *testing.T) {
	h, _ := d3KotaOrtam(t)
	if _, err := h.DB.Exec(`UPDATE service_plans SET max_db=2 WHERE id=3`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.DB.Exec(`UPDATE domains SET plan_id=3 WHERE id=20`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.DB.Exec(`INSERT INTO db_accounts (domain_id, db_name) VALUES (20,'c_bayi7_x')`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := h.bayiDBKotasi(ctx, 20, 1); err != nil {
		t.Errorf("1+1 ≤ 2 kabul: %v", err)
	}
	var kh *kotaHatasi
	if err := h.bayiDBKotasi(ctx, 20, 2); !errors.As(err, &kh) {
		t.Errorf("1+2 > 2 reddedilmeli: %v", err)
	}
	if _, err := h.DB.Exec(`UPDATE service_plans SET max_db=0 WHERE id=3`); err != nil {
		t.Fatal(err)
	}
	if err := h.bayiDBKotasi(ctx, 20, 50); err != nil {
		t.Errorf("0 = sınırsız: %v", err)
	}
}

func TestKalemDurumu(t *testing.T) {
	if kalemDurumu(errors.New("x")) != "hata" || kalemDurumu(&kotaHatasi{mesaj: "k"}) != "atlandi" {
		t.Error("kalem durumu sınıflaması")
	}
	var _ = sql.ErrNoRows
}
