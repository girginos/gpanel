package domains

// adalani_marka_test.go — Kural 1b (D2 whitelabel): bayi özel panel adresleri
// (cp_marka_alanadi, bekleyen kayıt dahil) hosting ad alanını kısıtlar. Gerçek SQL.

import (
	"context"
	"errors"
	"testing"
)

func TestAdAlaniDenetle_Kural1bMarkaAdresi(t *testing.T) {
	db := aa_db(t)
	aa_panel(t, "panel.hostingadresi.test")
	ctx := context.Background()
	// Tablo yokken kural atlanır (göç koşmamış kurulum alan adı açabilmeli).
	if err := AdAlaniDenetle(ctx, db, "markali.test", 3); err != nil {
		t.Fatalf("tablo yokken izin beklenir: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE cp_marka_alanadi (alan_adi VARCHAR(253) PRIMARY KEY, reseller_id BIGINT UNSIGNED NOT NULL,
		dogrulama CHAR(32) NOT NULL DEFAULT '', durum VARCHAR(16) NOT NULL DEFAULT 'dns_bekliyor')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cp_marka_alanadi (alan_adi, reseller_id) VALUES ('panel.markali.test', 2)`); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		ad     string
		kiraci int64
		tur    string
	}{
		{"panel.markali.test", 2, "panel"}, {"panel.markali.test", 3, "panel"}, {"panel.markali.test", 0, "panel"},
		{"x.panel.markali.test", 3, "panel"}, {"x.panel.markali.test", 2, "panel"},
		{"markali.test", 2, ""}, {"markali.test", 3, "capraz"}, {"markali.test", 0, "capraz"},
		{"diger.markali.test", 3, ""},
	} {
		err := AdAlaniDenetle(ctx, db, c.ad, c.kiraci)
		var ah *AdAlaniHatasi
		switch {
		case c.tur == "" && err != nil:
			t.Errorf("%q (kiracı %d): izin beklenir: %v", c.ad, c.kiraci, err)
		case c.tur == "panel" && (!errors.As(err, &ah) || ah.Genel != adAlaniMsjPanel):
			t.Errorf("%q (kiracı %d): panel reddi beklenir: %v", c.ad, c.kiraci, err)
		case c.tur == "capraz" && (!errors.As(err, &ah) || ah.Genel != adAlaniMsjCapraz):
			t.Errorf("%q (kiracı %d): çapraz reddi beklenir: %v", c.ad, c.kiraci, err)
		}
	}
}
