package wordpress

// wpdrop_test.go — tenant-arası WP DB DROP koruması testi.
// Katman 1+2: dbAdiWPGuard (GecerliDBKimlik + wp_ öneki) → kötücül/kaçışlı adları reddeder.
// Katman 3: dropIzinli (+ sahiplik) → meşru ad olsa bile BAŞKA tenant'ın DB'sini reddeder.

import (
	"strings"
	"testing"
)

func TestDbAdiWPGuard_KotucullReddedilir(t *testing.T) {
	kotu := []string{
		"",                              // boş
		"mysql",                         // wp_ değil (sistem DB)
		"information_schema",            // wp_ değil
		"baskatenant",                   // wp_ değil
		"wp-x",                          // tire → identifier değil
		"wp_ x",                         // boşluk
		"wp_x;",                         // ; ile ifade sonu
		"wp_x`",                         // backtick (identifier kaçışı)
		"`wp_x`",                        // backtick sarmalı
		"wp_x'",                         // tek tırnak
		"wp_x\" ",                       // çift tırnak + boşluk
		"wp_x`; DROP DATABASE mysql;--", // SQLi denemesi
		"wp_" + strings.Repeat("a", 70), // 64 char sınırı aşıyor
		"wp_x\n",                        // newline
	}
	for _, s := range kotu {
		if dbAdiWPGuard(s) {
			t.Errorf("kötücül dbName KABUL EDİLDİ (reddedilmeliydi): %q", s)
		}
	}
}

func TestDbAdiWPGuard_MesruKabul(t *testing.T) {
	iyi := []string{"wp_a1b2c3d4", "wp_deadbeef", "wp_0", "wp_kendi_slug"}
	for _, s := range iyi {
		if !dbAdiWPGuard(s) {
			t.Errorf("meşru dbName REDDEDİLDİ (kabul edilmeliydi): %q", s)
		}
	}
}

func TestDropIzinli_SahiplikZorunlu(t *testing.T) {
	// Sahte sahiplik: yalnız (wp_kendi, domain 1) bu domaine ait.
	sahip := func(dbName string, domainID int64) (bool, error) {
		return domainID == 1 && dbName == "wp_kendi", nil
	}

	// Meşru: kendi DB'si, doğru domain → izinli.
	if !dropIzinli("wp_kendi", 1, sahip) {
		t.Error("kendi DB'sinin DROP'u ENGELLENDİ (izinli olmalıydı)")
	}
	// Kötücül: başka tenant'ın DB'si (geçerli ad ama sahiplik yok) → REDDEDİLMELİ.
	if dropIzinli("wp_baskatenant", 1, sahip) {
		t.Error("BAŞKA tenant'ın DB'sinin DROP'una İZİN VERİLDİ (cross-tenant açık!)")
	}
	// Doğru ad ama yanlış domain → reddedilmeli.
	if dropIzinli("wp_kendi", 2, sahip) {
		t.Error("yanlış domain'den DROP'a izin verildi")
	}
	// Ad-guard'ı geçemeyen kötücül ad → sahiplik sorgusuna bile gitmeden reddedilmeli.
	if dropIzinli("wp_x`; DROP DATABASE mysql;--", 1, func(string, int64) (bool, error) {
		return true, nil // sahiplik "true" dönse bile ad-guard durdurmalı
	}) {
		t.Error("kaçışlı dbName ad-guard'ı geçti")
	}
}
