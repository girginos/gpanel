package marka

// Üretimde domains.alan_adi utf8mb4_unicode_ci, ek_alan_adlari.alan_adi utf8mb4_general_ci. UNION'da kolasyon
// eşitlenmezse MariaDB 1271 verir → "Özel adres doğrulanamadı." (2026-10-05, gpanel.girginos.cloud).
// Gerçek MariaDB gerekir (GOSP_TEST_GECICI_DSN; yoksa ATLANIR).

import (
	"context"
	"testing"
)

func TestYerelBolgeBul_KolasyonKarisikUnion(t *testing.T) {
	o := uyKur(t)
	if _, err := o.db.Exec(`ALTER TABLE ek_alan_adlari MODIFY alan_adi VARCHAR(253) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// bilinmeyen ad: hata YOK, bölge yok
	y, err := yerelBolgeBul(ctx, o.db, "gpanel.bilinmeyen.example.org", 7)
	if err != nil || y.Var {
		t.Fatalf("bilinmeyen ad: Var=%v err=%v", y.Var, err)
	}
	// ek alan altındaki ad: ek bölge bulunur
	y, err = yerelBolgeBul(ctx, o.db, "panel.ek-alfa.test", 7)
	if err != nil || !y.Var || !y.Ek || y.Rel != "panel" || y.Sahip != 7 {
		t.Fatalf("ek alan altı ad: %+v err=%v", y, err)
	}
	// ana alan altındaki ad
	y, err = yerelBolgeBul(ctx, o.db, "gpanel.alfa.test", 7)
	if err != nil || !y.Var || y.Ek || y.Rel != "gpanel" {
		t.Fatalf("ana alan altı ad: %+v err=%v", y, err)
	}
}
