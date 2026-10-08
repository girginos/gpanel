package domains

// yik_yetim_test.go — hosting silinince domains(id)'e FK'si olmayan hesap-kapsamlı
// tabloların satırları (yetim alt alan adı, dizin koruması, uygulama/WP kurulum kaydı)
// da silinir.

import (
	"context"
	"testing"

	"girginospanel/internal/sahtedb"
)

func TestHostingYik_FKsizTablolariTemizler(t *testing.T) {
	s, db := sahtedb.Yeni()
	defer db.Close()
	// isDemo=1: sistem tarafı (Deprovision/userdel) atlanır; yalnız DB temizliği koşar.
	if _, err := HostingYik(context.Background(), db, 42, "yik.test", "c_yik", 1); err != nil {
		t.Fatal(err)
	}
	for _, tablo := range []string{"subdomanlar", "korumali_dizinler", "cp_uygulama_kurulumlar", "cp_wp_kurulum"} {
		if !s.Yurutuldu("DELETE FROM " + tablo + " WHERE domain_id=? | 42") {
			t.Errorf("hosting silinince %s satırları silinmedi (yetim kayıt)", tablo)
		}
	}
	if !s.Yurutuldu("DELETE FROM domains WHERE id=? | 42") {
		t.Error("domains satırı silinmedi")
	}
	// Demo hesabın yedeği saklanmaz: backups FK'si (eski CASCADE) kalktığı için satırlar elle silinir.
	if !s.Yurutuldu("DELETE FROM backups WHERE domain_id=? | 42") {
		t.Error("demo hesabın yedek satırları silinmedi (FK kalktı, yetim kalırdı)")
	}
}
