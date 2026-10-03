package protokol

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Tasarım §4.1 R0 listesi (Faz 1). Ekleme/çıkarma bilinçli olmalı.
var faz1Araclari = []string{
	"hesap_ozet", "site_yokla", "log_oku", "sunucu_log_oku", "servis_durum", "disk_kota",
	"dns_kontrol", "ssl_durum", "nginx_test", "dogrula_ozet", "wp_durum", "cron_liste",
	"php_ayar_oku", "yedek_liste", "son_degisiklikler", "onarim_onizle",
}

func TestKatalogTutarlilik(t *testing.T) {
	var adlar []string
	for _, a := range Katalog() {
		adlar = append(adlar, a.Ad)
		if !aracAdiDeseni.MatchString(a.Ad) {
			t.Errorf("%s: ad biçimi", a.Ad)
		}
		if a.Risk != R0 {
			t.Errorf("%s: Faz 1 yalnız R0", a.Ad)
		}
		if strings.TrimSpace(a.Aciklama.TR) == "" || strings.TrimSpace(a.Aciklama.EN) == "" {
			t.Errorf("%s: TR/EN açıklama", a.Ad)
		}
		if len(a.Roller) == 0 {
			t.Errorf("%s: rol yok", a.Ad)
		}
		for _, r := range a.Roller {
			if !r.Gecerli() {
				t.Errorf("%s: rol %q", a.Ad, r)
			}
		}
		if !strings.HasPrefix(a.DenetimKodu(), "destek.") {
			t.Errorf("%s: denetim kodu", a.Ad)
		}
		pad := map[string]bool{}
		for _, p := range a.Parametreler {
			if pad[p.Ad] || !regexp.MustCompile(`^[a-z][a-z_]{1,31}$`).MatchString(p.Ad) {
				t.Errorf("%s.%s: ad", a.Ad, p.Ad)
			}
			pad[p.Ad] = true
			if p.Aciklama.TR == "" || p.Aciklama.EN == "" {
				t.Errorf("%s.%s: açıklama", a.Ad, p.Ad)
			}
			if (p.Ad == "hesap") != p.Hesap || (p.Hesap && (p.Tur != TurTamsayi || !p.Zorunlu)) {
				t.Errorf("%s.%s: hesap parametresi kuralı", a.Ad, p.Ad)
			}
			if len(p.Enum) > 0 && p.Tur != TurDizge || len(p.TamsayiEnum) > 0 && p.Tur != TurTamsayi {
				t.Errorf("%s.%s: enum türü", a.Ad, p.Ad)
			}
			if p.Tur == TurTamsayi && !p.Hesap && len(p.TamsayiEnum) == 0 {
				t.Errorf("%s.%s: serbest tamsayı yasak (enum gerekli)", a.Ad, p.Ad)
			}
			if p.Desen != "" {
				if _, err := regexp.Compile(p.Desen); err != nil || desenAl(a.Ad, p.Ad) == nil {
					t.Errorf("%s.%s: desen", a.Ad, p.Ad)
				}
			}
		}
		if (a.Ad == "sunucu_log_oku" || a.Ad == "onarim_onizle") && !slices.Equal(a.Roller, []Rol{RolKok}) {
			t.Errorf("%s yalnız kök olmalı", a.Ad)
		}
	}
	if !slices.Equal(adlar, faz1Araclari) {
		t.Fatalf("katalog = %v\nbeklenen %v", adlar, faz1Araclari)
	}
	if _, ok := AracBul("kabuk"); ok {
		t.Fatal("bilinmeyen araç bulundu")
	}
}

func TestKatalogOzeti(t *testing.T) {
	o := KatalogOzeti()
	if !hex64Deseni.MatchString(o) || o != KatalogOzeti() {
		t.Fatalf("özet %q", o)
	}
}

func TestDuzeyRisk(t *testing.T) {
	durum := map[Duzey][]Risk{
		DuzeyKapali: {},
		DuzeyOkuma:  {R0},
		DuzeyOnarim: {R0, R1},
		DuzeyTam:    {R0, R1, R2},
	}
	for d, izin := range durum {
		for _, r := range []Risk{R0, R1, R2, "R3"} {
			if d.IzinVerir(r) != slices.Contains(izin, r) {
				t.Errorf("%s/%s", d, r)
			}
		}
	}
}
