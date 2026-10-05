package domains

// ek_adalani_test.go — ek (addon) alan adları ad-alanı denetimine KATILIR: yeni bir
// hosting (domains), başka kiracının ek alan adının kendisini/alt adını/üst adını
// açamaz (kimin olursa olsun birebir aynı ad iki kez olmaz); aynı kiracınınkine
// (kendi ek alan adının alt adı) serbest; tablo yoksa (0160 koşmamış) kural atlanır.

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestAdAlaniDenetle_EkAlanAdlari(t *testing.T) {
	db := aa_db(t)
	body, err := os.ReadFile("../../migrations/0160_ek_alan_adlari.sql")
	if err != nil {
		t.Fatal(err)
	}
	var temiz []string
	for _, s := range strings.Split(string(body), "\n") {
		if st := strings.TrimSpace(s); st != "" && !strings.HasPrefix(st, "--") {
			temiz = append(temiz, s)
		}
	}
	for _, ifade := range strings.Split(strings.Join(temiz, "\n"), ";") {
		if s := strings.TrimSpace(ifade); s != "" {
			aa_exec(t, db, s)
		}
	}
	// domain 10 (kök hesap) ek alan adı: ek-kok.test ; domain 20 (bayi 2) ek: ek-bayi2.test
	aa_exec(t, db, `INSERT INTO ek_alan_adlari (domain_id, alan_adi) VALUES (10,'ek-kok.test'), (20,'ek-bayi2.test')`)
	ctx := context.Background()
	hata := func(ad string, kiraci int64) *AdAlaniHatasi {
		err := AdAlaniDenetle(ctx, db, ad, kiraci)
		if err == nil {
			return nil
		}
		h, ok := err.(*AdAlaniHatasi)
		if !ok {
			t.Fatalf("%s: düz hata: %v", ad, err)
		}
		return h
	}
	for _, c := range []struct {
		ad       string
		kiraci   int64
		red      bool
		aciklama string
	}{
		{"ek-kok.test", 2, true, "başka kiracının ek alan adı birebir"},
		{"ek-kok.test", 0, true, "kendi ek alan adıyla birebir (iki kez olmaz)"},
		{"shop.ek-kok.test", 2, true, "başka kiracının ek alan adının alt adı"},
		{"a.b.ek-bayi2.test", 0, true, "bayinin ek alan adının derin alt adı (kök açamaz)"},
		{"www.ek-kok.test", 0, true, "ek alan adının www/hizmet adı"},
		{"mail.ek-bayi2.test", 2, true, "ek alan adının hizmet adı (sahibi bile)"},
		{"shop.ek-bayi2.test", 2, false, "aynı kiracının ek alan adının alt adı serbest"},
		{"bagimsiz.test", 2, false, "ilgisiz ad serbest"},
	} {
		h := hata(c.ad, c.kiraci)
		if c.red && h == nil {
			t.Errorf("%s (%s, kiracı %d): reddedilmeliydi", c.aciklama, c.ad, c.kiraci)
		}
		if !c.red && h != nil {
			t.Errorf("%s (%s): serbest olmalıydı: %v", c.aciklama, c.ad, h)
		}
		if h != nil && c.kiraci == 2 && strings.Contains(h.Genel, "ek-kok") {
			t.Errorf("%s: bayiye giden metin başka kiracının adını sızdırıyor: %s", c.aciklama, h.Genel)
		}
	}
}
