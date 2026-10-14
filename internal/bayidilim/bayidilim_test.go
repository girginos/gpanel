package bayidilim

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func gecici(t *testing.T) string {
	t.Helper()
	eski := UyelikDizini
	UyelikDizini = filepath.Join(t.TempDir(), "uyelik")
	t.Cleanup(func() { UyelikDizini = eski })
	return UyelikDizini
}

// Birim adı, yol ve komut satırına giren her kimlik allowlist'ten geçer: enjeksiyon biçimleri reddedilir.
func TestGecerliSK_Allowlist(t *testing.T) {
	gecerli := []string{"c_a", "c_site_com", "c_0", "c_" + strings.Repeat("x", 60)}
	for _, s := range gecerli {
		if !GecerliSK(s) {
			t.Errorf("geçerli sk reddedildi: %q", s)
		}
	}
	gecersiz := []string{"", "c_", "C_x", "x_site", "c_a-b", "c_a/b", "c_../x", "c_a b", "c_a.slice", "c_a\n", "c_a;rm", "c_" + strings.Repeat("x", 61), "c_ş"}
	for _, s := range gecersiz {
		if GecerliSK(s) {
			t.Errorf("geçersiz sk kabul edildi: %q", s)
		}
	}
	for _, id := range []int64{1, 12, EnBuyukBayiID} {
		if !GecerliBayiID(id) {
			t.Errorf("geçerli bayi kimliği reddedildi: %d", id)
		}
	}
	for _, id := range []int64{0, -1, EnBuyukBayiID + 1} {
		if GecerliBayiID(id) {
			t.Errorf("geçersiz bayi kimliği kabul edildi: %d", id)
		}
	}
}

func TestDilimAdlari_VeAyristirma(t *testing.T) {
	if got := BayiDilim(12); got != "girginos-bayi12.slice" {
		t.Errorf("BayiDilim = %q", got)
	}
	if got := UyeDilim(12, "c_x"); got != "girginos-bayi12-c_x.slice" {
		t.Errorf("UyeDilim = %q", got)
	}
	if got := EskiDilim("c_x"); got != "girginos-c_x.slice" {
		t.Errorf("EskiDilim = %q", got)
	}
	if BayiDilim(0) != "" || UyeDilim(0, "c_x") != "" || UyeDilim(3, "c_x-y") != "" || EskiDilim("x") != "" {
		t.Error("geçersiz girdi için boş ad dönmeli")
	}
	if id, ok := BayiDilimindenID("girginos-bayi12.slice"); !ok || id != 12 {
		t.Errorf("BayiDilimindenID = %d %v", id, ok)
	}
	if id, sk, ok := UyeDilimindenAyir("girginos-bayi12-c_x.slice"); !ok || id != 12 || sk != "c_x" {
		t.Errorf("UyeDilimindenAyir = %d %q %v", id, sk, ok)
	}
	// Biçim dışı adlar ayrıştırılmaz: yanlış dosyaya dokunulmaz.
	for _, ad := range []string{"girginos-bayi12.service", "girginos-bayi0.slice", "girginos-bayi.slice", "girginos-bayi12-.slice",
		"girginos-bayi12-c_.slice", "girginos-bayi12-x.slice", "girginos-c_x.slice", "girginos-bayi1234567890.slice", "bayi12.slice", "girginos-bayi12.slice.bak"} {
		if _, ok := BayiDilimindenID(ad); ok {
			t.Errorf("BayiDilimindenID kabul etti: %q", ad)
		}
		if _, _, ok := UyeDilimindenAyir(ad); ok {
			t.Errorf("UyeDilimindenAyir kabul etti: %q", ad)
		}
	}
	// Üye dilim adının ebeveyni havuz dilimidir: systemd hiyerarşiyi addan kurar.
	if !strings.HasPrefix(UyeDilim(12, "c_x"), strings.TrimSuffix(BayiDilim(12), ".slice")+"-") {
		t.Error("üye dilim adı havuz diliminin altına yuvalanmıyor")
	}
}

func TestUyelik_AdiVeCgroupYolu(t *testing.T) {
	gecici(t)
	if Uyelik("c_x") != 0 || Adi("c_x") != "girginos-c_x.slice" {
		t.Fatal("üyelik yokken eski ad beklenir")
	}
	if d, err := UyelikYaz("c_x", 7); err != nil || !d {
		t.Fatalf("ilk yazım: %v %v", d, err)
	}
	if d, err := UyelikYaz("c_x", 7); err != nil || d {
		t.Fatalf("aynı üyelik değişti sayıldı: %v %v", d, err)
	}
	if Uyelik("c_x") != 7 || Adi("c_x") != "girginos-bayi7-c_x.slice" {
		t.Errorf("üye adı: %q", Adi("c_x"))
	}
	if got := CgroupGoreli("c_x"); got != "girginos.slice/girginos-bayi7.slice/girginos-bayi7-c_x.slice" {
		t.Errorf("CgroupGoreli = %q", got)
	}
	if got := BayiCgroupGoreli(7); got != "girginos.slice/girginos-bayi7.slice" {
		t.Errorf("BayiCgroupGoreli = %q", got)
	}
	// Bayi değişince (devir) yeni ad.
	if d, _ := UyelikYaz("c_x", 9); !d || Adi("c_x") != "girginos-bayi9-c_x.slice" {
		t.Errorf("devir sonrası ad: %q", Adi("c_x"))
	}
	if d, err := UyelikSil("c_x"); err != nil || !d {
		t.Fatalf("silme: %v %v", d, err)
	}
	if d, err := UyelikSil("c_x"); err != nil || d {
		t.Fatalf("olmayan üyelik silindi sayıldı: %v %v", d, err)
	}
	if Adi("c_x") != "girginos-c_x.slice" || CgroupGoreli("c_x") != "girginos.slice/girginos-c_x.slice" {
		t.Error("silinen üyelik eski ada dönmeli")
	}
	// Geçersiz girdi yazılmaz.
	if _, err := UyelikYaz("c_x/../y", 7); err == nil {
		t.Error("geçersiz sk yazıldı")
	}
	if _, err := UyelikYaz("c_x", 0); err == nil {
		t.Error("geçersiz bayi kimliği yazıldı")
	}
	if _, err := UyelikSil("../etc"); err == nil {
		t.Error("geçersiz sk silindi")
	}
}

// Bozuk üyelik dosyası güvenli tarafa düşer: eski ad (asla boş ya da yabancı ad).
func TestUyelik_BozukDosyaGuvenliVarsayilan(t *testing.T) {
	dir := gecici(t)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, ic := range []string{"", "abc\n", "-5\n", "0\n", "99999999999\n", "7 8\n", "../x\n"} {
		if err := os.WriteFile(filepath.Join(dir, "c_x"), []byte(ic), 0o644); err != nil {
			t.Fatal(err)
		}
		if Uyelik("c_x") != 0 || Adi("c_x") != "girginos-c_x.slice" {
			t.Errorf("bozuk içerik %q: Uyelik=%d Adi=%q", ic, Uyelik("c_x"), Adi("c_x"))
		}
	}
	// Laravel gibi daha gevşek doğrulayan çağıranlar geçersiz sk verse de ad ASLA boş olmaz (boş Slice= = sınırsız).
	if AdiYedekli("c_Bad-Name") == "" || Adi("c_Bad-Name") != "" {
		t.Errorf("AdiYedekli boş / Adi geçersiz sk için dolu: %q %q", AdiYedekli("c_Bad-Name"), Adi("c_Bad-Name"))
	}
	if AdiYedekli("c_ok") != "girginos-c_ok.slice" {
		t.Errorf("AdiYedekli geçerli sk = %q", AdiYedekli("c_ok"))
	}
}

func TestUyelikListe_YabanciDosyalariAtlar(t *testing.T) {
	dir := gecici(t)
	_, _ = UyelikYaz("c_a", 1)
	_, _ = UyelikYaz("c_b", 2)
	_ = os.WriteFile(filepath.Join(dir, "c_b.tmp"), []byte("2\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "notlar.txt"), []byte("1\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "c_bozuk"), []byte("x\n"), 0o644)
	_ = os.Mkdir(filepath.Join(dir, "c_dizin"), 0o755)
	l := UyelikListe()
	if len(l) != 2 || l["c_a"] != 1 || l["c_b"] != 2 {
		t.Errorf("UyelikListe = %v", l)
	}
}

func TestDropin_IcerikVeYasamDongusu(t *testing.T) {
	gecici(t)
	birimDizin := t.TempDir()
	const birim = "php-fpm-c_x.service"
	yol := filepath.Join(birimDizin, birim+".d", DropinAd)

	if got := DropinIcerik(7, "c_x"); !strings.Contains(got, "[Service]\nSlice=girginos-bayi7-c_x.slice\n") {
		t.Errorf("drop-in içeriği: %q", got)
	}
	// Üye değil: dosya yaratılmaz.
	if d, err := DropinUygula(birimDizin, birim, "c_x"); err != nil || d {
		t.Fatalf("üye değilken: %v %v", d, err)
	}
	if _, err := os.Stat(yol); err == nil {
		t.Fatal("üye olmayan için drop-in yazıldı")
	}
	// Üye: yazılır, ikinci çağrı idempotent.
	_, _ = UyelikYaz("c_x", 7)
	if d, err := DropinUygula(birimDizin, birim, "c_x"); err != nil || !d {
		t.Fatalf("üyeyken: %v %v", d, err)
	}
	if d, err := DropinUygula(birimDizin, birim, "c_x"); err != nil || d {
		t.Fatalf("idempotent değil: %v %v", d, err)
	}
	b, _ := os.ReadFile(yol)
	if string(b) != DropinIcerik(7, "c_x") {
		t.Errorf("drop-in: %q", b)
	}
	// Bayi değişti: yeniden yazılır.
	_, _ = UyelikYaz("c_x", 9)
	if d, _ := DropinUygula(birimDizin, birim, "c_x"); !d {
		t.Error("devirde drop-in yenilenmedi")
	}
	if b, _ := os.ReadFile(yol); !strings.Contains(string(b), "girginos-bayi9-c_x.slice") {
		t.Errorf("devir sonrası drop-in: %q", b)
	}
	// Başka bir drop-in (StartLimit) varken havuzdan çıkış: yalnız bizimki silinir, .d dizini kalır.
	diger := filepath.Join(birimDizin, birim+".d", "10-gpanel-startlimit.conf")
	_ = os.WriteFile(diger, []byte("[Unit]\nStartLimitBurst=5\n"), 0o644)
	_, _ = UyelikSil("c_x")
	if d, err := DropinUygula(birimDizin, birim, "c_x"); err != nil || !d {
		t.Fatalf("çıkışta: %v %v", d, err)
	}
	if _, err := os.Stat(yol); err == nil {
		t.Error("havuzdan çıkan hostingin drop-in'i kaldı")
	}
	if _, err := os.Stat(diger); err != nil {
		t.Error("başka drop-in silindi")
	}
	// Yalnız bizim drop-in'imiz varsa boş .d dizini de kalkar.
	_ = os.Remove(diger)
	_, _ = UyelikYaz("c_x", 7)
	_, _ = DropinUygula(birimDizin, birim, "c_x")
	_, _ = UyelikSil("c_x")
	_, _ = DropinUygula(birimDizin, birim, "c_x")
	if _, err := os.Stat(filepath.Join(birimDizin, birim+".d")); err == nil {
		t.Error("boş .d dizini kaldı")
	}
}

func TestDropin_GecersizHedefReddedilir(t *testing.T) {
	gecici(t)
	d := t.TempDir()
	for _, c := range []struct{ birim, sk string }{
		{"../php-fpm-c_x.service", "c_x"}, {"a/b.service", "c_x"}, {"", "c_x"}, {".gizli.service", "c_x"},
		{"php-fpm-c_x.service", "c_x/../y"}, {"php-fpm-c_x.service", ""},
	} {
		if _, err := DropinUygula(d, c.birim, c.sk); err == nil {
			t.Errorf("geçersiz hedef kabul edildi: %+v", c)
		}
	}
	if ents, _ := os.ReadDir(d); len(ents) != 0 {
		t.Errorf("geçersiz çağrılar dizin yarattı: %v", ents)
	}
}
