package hedef

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestUzakGeciciAd_AyniDizinVeTanima(t *testing.T) {
	simdi := time.Unix(1790000000, 0)
	for _, ad := range []string{
		"c_ornek-20260825-233538.tar.gz.age",
		"2026-08-25/c_ornek.tar.gz.age",
		"Yedek Klasörü/müşteri ğüşıöç İŞ yedeği.age",
	} {
		g, err := uzakGeciciAd(ad, simdi)
		if err != nil {
			t.Fatalf("%q: %v", ad, err)
		}
		if err := AdDogrula(g); err != nil {
			t.Errorf("geçici ad geçersiz: %q", g)
		}
		dizin, _ := AdBol(ad)
		gDizin, gDosya := AdBol(g)
		if gDizin != dizin {
			t.Errorf("geçici ad AYNI dizinde olmalı: %q → %q", ad, g)
		}
		if !strings.HasPrefix(gDosya, UzakGeciciOnek) || !GeciciMi(g) {
			t.Errorf("GeciciMi tanımadı: %q", g)
		}
		if GeciciMi(ad) {
			t.Errorf("nihai ad geçici sanıldı: %q", ad)
		}
		if z, ok := GeciciZamani(g); !ok || !z.Equal(simdi) {
			t.Errorf("GeciciZamani(%q) = %v %v, beklenen %v", g, z, ok, simdi)
		}
		if g2, _ := uzakGeciciAd(ad, simdi); g2 == g {
			t.Errorf("aynı anda üretilen iki geçici ad aynı: %q", g)
		}
	}
	if g, err := UzakGeciciAd("a/b.age"); err != nil || !GeciciMi(g) {
		t.Errorf("UzakGeciciAd: %q %v", g, err)
	} else if z, ok := GeciciZamani(g); !ok || time.Since(z) > time.Minute || time.Since(z) < -time.Minute {
		t.Errorf("UzakGeciciAd zamanı şimdi değil: %v %v", z, ok)
	}
}

func TestUzakGeciciAd_UzunAdKisaltilir(t *testing.T) {
	// Çok baytlı karakterle sınırda: kısaltma rune'u bölmemeli, toplam 512 baytı aşmamalı.
	dizin := strings.Repeat("d", 100)
	dosya := strings.Repeat("ğ", (AzamiAdUzunlugu-len(dizin)-1)/2) // nihai ad ~512 bayt
	ad := dizin + "/" + dosya
	if err := AdDogrula(ad); err != nil {
		t.Fatalf("test verisi geçersiz: %v", err)
	}
	g, err := UzakGeciciAd(ad)
	if err != nil {
		t.Fatal(err)
	}
	if len(g) > AzamiAdUzunlugu || !utf8.ValidString(g) || AdDogrula(g) != nil || !GeciciMi(g) {
		t.Errorf("kısaltılmış geçici ad bozuk: %d bayt, geçerli UTF-8=%v", len(g), utf8.ValidString(g))
	}
	// Sonda boşluk/nokta bırakmaz.
	// Geçici önek (dizinsiz) 36 bayt → dosya adı 476 bayta kısalır ve kesim "   .." + " " üstüne düşer.
	ad2 := strings.Repeat("x", 470) + "   ..   " + strings.Repeat("y", 30)
	g2, err := UzakGeciciAd(ad2)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasSuffix(g2, " ") || strings.HasSuffix(g2, ".") || !strings.HasSuffix(g2, "x") {
		t.Errorf("kısaltılmış ad sonda boşluk/nokta bıraktı: %q", g2[len(g2)-12:])
	}
	// Dizin tek başına sığmıyorsa hata (sessizce başka dizine yazılmaz).
	uzunDizin := strings.Repeat("d", AzamiAdUzunlugu-3) + "/ab"
	if _, err := UzakGeciciAd(uzunDizin); !errors.Is(err, ErrGecersizAd) {
		t.Errorf("sığmayan dizin kabul edildi: %v", err)
	}
}

func TestUzakGeciciAd_Gecersizler(t *testing.T) {
	for _, ad := range []string{"", "/mutlak", "a/../b", "son/", `ters\bolu`} {
		if _, err := UzakGeciciAd(ad); !errors.Is(err, ErrGecersizAd) {
			t.Errorf("geçersiz ad kabul edildi: %q", ad)
		}
	}
	g, _ := UzakGeciciAd("a/b.age")
	if _, err := UzakGeciciAd(g); !errors.Is(err, ErrGecersizAd) {
		t.Errorf("geçici adın geçici adı üretildi: %q", g)
	}
	// Yalnız DİZİN bileşeni önekle başlıyorsa geçici değildir.
	if GeciciMi(UzakGeciciOnek + "dizin/x.age") {
		t.Error("dizin öneki geçici sayıldı")
	}
	for _, ad := range []string{"x.age", UzakGeciciOnek, UzakGeciciOnek + "abc-def", UzakGeciciOnek + "-1-x", "d/" + UzakGeciciOnek + "0-000000000000-x"} {
		if _, ok := GeciciZamani(ad); ok {
			t.Errorf("GeciciZamani zaman uydurdu: %q", ad)
		}
	}
}
