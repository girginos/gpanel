package ajan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"girginospanel/internal/denetim"
	p "girginospanel/internal/destek/protokol"
	"girginospanel/internal/maske"
)

func TestMinDuzey(t *testing.T) {
	cases := []struct {
		girdi []p.Duzey
		bek   p.Duzey
	}{
		{[]p.Duzey{p.DuzeyTam, p.DuzeyOkuma, p.DuzeyOnarim}, p.DuzeyOkuma},
		{[]p.Duzey{p.DuzeyTam, p.DuzeyTam}, p.DuzeyTam},
		{[]p.Duzey{p.DuzeyOnarim, p.DuzeyTam}, p.DuzeyOnarim},
		{[]p.Duzey{p.DuzeyTam, p.DuzeyKapali}, p.DuzeyKapali},
		{[]p.Duzey{p.DuzeyTam, "bilinmeyen"}, p.DuzeyKapali}, // geçersiz → kapalı (kapalı başarısız)
		{[]p.Duzey{""}, p.DuzeyKapali},
	}
	for _, c := range cases {
		if g := minDuzey(c.girdi...); g != c.bek {
			t.Errorf("minDuzey(%v) = %s, beklenen %s", c.girdi, g, c.bek)
		}
	}
}

func TestGeriCekilmeSinirlar(t *testing.T) {
	for d := 1; d < 20; d++ {
		for i := 0; i < 20; i++ {
			g := geriCekilme(d)
			taban := 2 * time.Second << (d - 1)
			if d > 9 || taban > 5*time.Minute {
				taban = 5 * time.Minute
			}
			if g < taban/2 || g > taban {
				t.Fatalf("deneme %d: %s ∉ [%s, %s]", d, g, taban/2, taban)
			}
		}
	}
}

func TestSonucTemizleMaskeVeSinirlar(t *testing.T) {
	kanarya := "KANARYA-SIR-0123456789abcdef"
	a := Yeni(nil, Yapilandirma{})
	a.maske = maske.Yeni(maske.Secenek{BilinenSirlar: []string{kanarya}, IPKirp: true, EpostaMaskele: true, EvOnekSoy: true})
	satirlar := []string{"password=gizli123 giriş", "x " + kanarya, "ip 203.0.113.77 bağlandı", "\x1b[31mrenk‮", strings.Repeat("é", 800)}
	for i := 0; i < 400; i++ {
		satirlar = append(satirlar, "satır")
	}
	s := a.sonucTemizle("ABCDEFGH", p.IsSonucu{IsKodu: "BASKAKOD", Ok: true, HataKodu: p.HataIc,
		Ozet: "özet\n" + kanarya, Satirlar: satirlar, Kanit: "biçimsiz kanıt!"})
	if s.IsKodu != "ABCDEFGH" {
		t.Fatalf("is_kodu zorlanmadı: %s", s.IsKodu)
	}
	if err := s.Dogrula(); err != nil {
		t.Fatalf("temizlenmiş sonuç geçersiz: %v", err)
	}
	hepsi := s.Ozet + "\n" + strings.Join(s.Satirlar, "\n")
	for _, yasak := range []string{kanarya, "gizli123", "203.0.113.77", "\x1b", "‮"} {
		if strings.Contains(hepsi, yasak) {
			t.Errorf("sonuçta %q kaldı", yasak)
		}
	}
	if len(s.Satirlar) != p.SatirSayisiSiniri || !s.Kesildi || s.Kanit != "" || s.HataKodu != "" {
		t.Fatalf("sınırlar: %d satır kesildi=%v kanıt=%q hata=%q", len(s.Satirlar), s.Kesildi, s.Kanit, s.HataKodu)
	}
	if len(s.Satirlar[4]) > p.SatirSiniri {
		t.Fatalf("satır kesilmedi: %d", len(s.Satirlar[4]))
	}
	// Ok=false + bilinmeyen kod → ic_hata
	s2 := a.sonucTemizle("ABCDEFGH", p.IsSonucu{HataKodu: "uydurma"})
	if s2.Ok || s2.HataKodu != p.HataIc {
		t.Fatalf("bilinmeyen hata kodu: %+v", s2)
	}
}

// Denetim kataloğu: ajanın yazdığı her destek.* kodu ve her aracın denetim kodu etiketli.
func TestDenetimKataloguDestekKodlari(t *testing.T) {
	for _, k := range []string{dEslesmePortal, dBaglantiIptal, dAyar, dR0Ozet, dIsRed} {
		if !denetim.EylemEtiketiVar(k) {
			t.Errorf("denetim kataloğunda etiket yok: %s", k)
		}
	}
	for _, a := range p.Katalog() {
		if !denetim.EylemEtiketiVar(a.DenetimKodu()) {
			t.Errorf("araç denetim kodu etiketsiz: %s", a.DenetimKodu())
		}
	}
}

func TestYapilandirmaDosyasi(t *testing.T) {
	cift, _ := p.AnahtarCiftiUret(nil)
	yol := filepath.Join(t.TempDir(), "destek.json")
	icerik := `{"merkez_url":"https://merkez.test","merkez_anahtarlari":["` + p.AcikAnahtarKodla(cift.Acik) + `"],"bot_kullanici_adi":"GirginTestBot"}`
	if err := os.WriteFile(yol, []byte(icerik), 0o600); err != nil {
		t.Fatal(err)
	}
	y := YapilandirmaYukle(yol)
	if y.Hata != "" || len(y.MerkezAnahtarlari) != 1 || y.BotKullaniciAdi != "GirginTestBot" || y.MerkezURL != "https://merkez.test" {
		t.Fatalf("yapılandırma: %+v", y)
	}
	ws, taban, err := y.adresler()
	if err != nil || ws.String() != "wss://merkez.test/v1/ajan/ws" || taban.String() != "https://merkez.test" {
		t.Fatalf("adresler: %v %v %v", ws, taban, err)
	}
	// Grup/diğer yazabiliyorsa YOK SAYILIR.
	_ = os.Chmod(yol, 0o666)
	y = YapilandirmaYukle(yol)
	if len(y.MerkezAnahtarlari) != 0 || y.Hata == "" {
		t.Fatalf("güvensiz izinli dosya kabul edildi: %+v", y)
	}
	// Bilinmeyen alan → ret; http → yalnız GuvensizIzin ile.
	_ = os.WriteFile(yol, []byte(`{"merkez_url":"http://x.test","fazla":1}`), 0o600)
	_ = os.Chmod(yol, 0o600)
	if y = YapilandirmaYukle(yol); y.Hata == "" {
		t.Fatal("bilinmeyen alan kabul edildi")
	}
	y = Yapilandirma{MerkezURL: "http://x.test"}
	if _, _, err := y.adresler(); err == nil {
		t.Fatal("TLS'siz merkez kabul edildi")
	}
	y.MerkezURL = "https://u:p@x.test/yol"
	if _, _, err := y.adresler(); err == nil {
		t.Fatal("kullanıcı bilgili/yollu merkez adresi kabul edildi")
	}
	// Gömülü anahtar (ldflags) okunur.
	eski := GomuluMerkezAnahtari
	GomuluMerkezAnahtari = p.AcikAnahtarKodla(cift.Acik)
	defer func() { GomuluMerkezAnahtari = eski }()
	if y = YapilandirmaYukle(filepath.Join(t.TempDir(), "yok.json")); len(y.MerkezAnahtarlari) != 1 || y.Hata != "" {
		t.Fatalf("gömülü anahtar: %+v", y)
	}
}

func TestCihazAnahtariKaliciVeIzin(t *testing.T) {
	yol := filepath.Join(t.TempDir(), "cihaz.key")
	c1, err := cihazAnahtariYukle(yol)
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(yol)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("izin: %v %v", st, err)
	}
	b, _ := os.ReadFile(yol)
	if strings.Contains(string(b), string(c1.Ozel.Seed())) {
		t.Fatal("tohum ham yazılmış")
	}
	c2, err := cihazAnahtariYukle(yol)
	if err != nil || c2.Kimlik != c1.Kimlik {
		t.Fatalf("kimlik kalıcı değil: %v", err)
	}
	_ = os.Chmod(yol, 0o640)
	if _, err := cihazAnahtariYukle(yol); err == nil {
		t.Fatal("grup okuyabilen cihaz anahtarı kabul edildi")
	}
}

func TestIzDenetle(t *testing.T) {
	b := &baglanti{Rol: p.RolMusteri, ResellerID: 7, DurumIzi: izOzet("musteri", "h1")}
	if n := izDenetle(b, kimlikDurumu{iz: izOzet("musteri", "h1"), resellerID: 7}); n != "" {
		t.Fatalf("geçerli iz iptal: %s", n)
	}
	if n := izDenetle(b, kimlikDurumu{iz: izOzet("musteri", "h2"), resellerID: 7}); n != p.IptalFTPParola {
		t.Fatalf("parola değişimi: %s", n)
	}
	if n := izDenetle(b, kimlikDurumu{iz: izOzet("musteri", "h1"), resellerID: 8}); n != p.IptalAlanSilme {
		t.Fatalf("devir: %s", n)
	}
	bb := &baglanti{Rol: p.RolBayi, DurumIzi: izOzet("bayi", "x"), IkiFA: true}
	if n := izDenetle(bb, kimlikDurumu{iz: izOzet("bayi", "x"), ikiFA: false}); n != p.IptalIkiFA {
		t.Fatalf("2FA: %s", n)
	}
	if n := izDenetle(bb, kimlikDurumu{neden: p.IptalBayi}); n != p.IptalBayi {
		t.Fatalf("askı: %s", n)
	}
}
