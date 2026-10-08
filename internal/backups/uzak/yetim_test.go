package uzak

// yetim_test.go — backups.domain_id FK'si kalktı (0217): domain silinince yedek satırı KALIR (yetim).
// Yetim satırlar uzak gönderim akışına (toplu aday, tekil kuyruğa alma) ve yerel-silme taramasına
// GİRMEMELİ; domaini canlı satırlar etkilenmemeli.

import (
	"context"
	"errors"
	"testing"
)

func TestYetimYedek_UzakAkislaraGirmez(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(nil)
	canli := o.yedek(1, "c_canli", "20260901-020000", sahteIcerik(10, 1), "oto", nil)
	yetim := o.yedek(2, "c_yetim", "20260901-020000", sahteIcerik(10, 2), "oto", nil)
	// 0217: FK kalkar; domain silinince satır kalır.
	if _, err := o.db.Exec(`ALTER TABLE backups DROP FOREIGN KEY fk_backup_domain`); err != nil {
		t.Fatal(err)
	}
	if _, err := o.db.Exec(`DELETE FROM domains WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = o.db.QueryRow(`SELECT COUNT(*) FROM backups WHERE id=?`, yetim).Scan(&n)
	if n != 1 {
		t.Fatal("kurulum: yetim satır kalmalı")
	}

	ids, on, err := TopluAdaylar(context.Background(), o.db, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != canli || on.Domain != 1 {
		t.Errorf("toplu aday yalnız canlı domainin yedeği olmalı: %v %+v", ids, on)
	}
	if _, _, err := KuyrugaAl(context.Background(), o.db, yetim); !errors.Is(err, ErrYedekYok) {
		t.Errorf("yetim yedek kuyruğa alınmamalı (ErrYedekYok): %v", err)
	}
	if _, _, err := KuyrugaAl(context.Background(), o.db, canli); err != nil {
		t.Errorf("canlı yedek kuyruğa alınabilmeli: %v", err)
	}
	kayitlar, err := yerelKayitlari(context.Background(), o.db, o.kok, 7, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range kayitlar {
		if k.yedekID == yetim {
			t.Error("yerel-silme taraması yetim yedeği içermemeli")
		}
	}
}
