//go:build windows

// windows_birim_test.go — Windows saglayicisinin SAF (yan etkisiz) mantiginin
// birim testleri: kimlik turetme, girdi dogrulama, SQL kacisi, cikti ayristirma.
//
// 🔴 STABILITE REHBERI #2: "her bug fix bir regression testi de eklemeli."
// Bu dosyadaki her test, denetimde bulunup duzeltilen somut bir zafiyeti
// KILITLER — ayni hata sessizce geri gelirse test kirilir. IIS/MSSQL GEREKMEZ;
// yalniz saf fonksiyonlar sinanir, bu yuzden capraz derlenen test binary'si
// (`GOOS=windows go test -c`) sunucuda IIS olmadan da kosar.
package platform

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// TestSistemKullaniciBicimVeUzunluk — sk 20 karakterlik SAM sinirina UYMALI ve
// sabit bicimde olmali. Denetim 03: govde 8 + tam 32-bit hash (8 hex).
func TestSistemKullaniciBicimVeUzunluk(t *testing.T) {
	bicim := regexp.MustCompile(`^gp_[a-z0-9]{1,8}[0-9a-f]{8}$`)
	ornekler := []string{
		"a.com",
		"example.com",
		"cok-uzun-bir-alan-adi-ornegi-buraya.com.tr",
		"1234567890abcdefghij.net",
		"xn--ate-turkce.com",
	}
	for _, alan := range ornekler {
		sk := sistemKullanici(alan)
		if len(sk) > 20 {
			t.Errorf("%q -> %q: SAM siniri asildi (%d > 20)", alan, sk, len(sk))
		}
		if !bicim.MatchString(sk) {
			t.Errorf("%q -> %q: beklenen bicime uymuyor (gp_ + <=8 govde + 8 hex)", alan, sk)
		}
	}
}

// TestSistemKullaniciKararli — ayni alan HER ZAMAN ayni sk'yi vermeli; yoksa
// silme (sk yeniden hesapladiginda) gercek kullaniciyi bulamaz.
func TestSistemKullaniciKararli(t *testing.T) {
	for _, alan := range []string{"example.com", "test.org", "musteri.com.tr"} {
		if a, b := sistemKullanici(alan), sistemKullanici(alan); a != b {
			t.Errorf("%q kararli degil: %q != %q", alan, a, b)
		}
	}
}

// TestSistemKullaniciCakismaGenisligi — denetim 03/1: govde kirpildigi icin
// ayni onEki paylasan alanlar yalniz hash'le ayrilir. Eski 16-bit hash saldirganca
// cakistirilabiliyordu; tam 32-bit'te ayni onEkli farkli alanlar FARKLI sk almali.
func TestSistemKullaniciCakismaGenisligi(t *testing.T) {
	// Ikisinin de govdesi "myreallyl" (ilk 8) olur; ayirt edici yalniz hash.
	a := sistemKullanici("myreallylongcompany.com")
	b := sistemKullanici("myreallylongdomain.net")
	if a == b {
		t.Fatalf("ayni onEkli iki farkli alan ayni sk'ye cakisti: %q", a)
	}
	// Genis bir kume uzerinde hic cakisma OLMAMALI (32-bit alanda 500 ornek
	// icin cakisma olasiligi ihmal edilebilir).
	gorulen := map[string]string{}
	for i := 0; i < 500; i++ {
		alan := fmt.Sprintf("site-numarasi-%d.example.com", i)
		sk := sistemKullanici(alan)
		if onceki, ok := gorulen[sk]; ok {
			t.Fatalf("cakisma: %q ve %q ayni sk %q", onceki, alan, sk)
		}
		gorulen[sk] = alan
	}
}

// TestAlanAdiRe — gecerli alan adlari kabul, tehlikeli/bicimsiz olanlar RED.
// Bu desen site uclarinin TEK girdi savunmasi (appcmd'ye arguman gider).
func TestAlanAdiRe(t *testing.T) {
	gecerli := []string{"a.com", "ornek.com", "alt.ornek.com.tr", "x1-2.net", "abc"}
	// 🔴 SOZLESME: alanAdiRe yalniz (a) bas/son'un alfanumerik olmasini ve
	// (b) izinli karakter kumesini ([a-z0-9.-]) zorlar. Ic yapiyi (ardisik nokta,
	// tire-nokta) KISITLAMAZ — "son-.com"/"nokta..com" bicimsizdir ama enjeksiyon
	// riski DEGIL (IIS/DNS zaten reddeder); bu yuzden gecersiz listesinde yok.
	// Testin isi guvenlik sinirini (buyuk harf/bosluk/`;"'/` /alt cizgi/asiri uzun)
	// kilitlemek.
	gecersiz := []string{
		"", "-bas.com", "BUYUK.com", "bosluk li.com",
		"a_b.com", `a;calc.com`, `a"b.com`, `a/b.com`,
		strings.Repeat("a", 254) + ".com", // 253 ustu
	}
	for _, s := range gecerli {
		if !alanAdiRe.MatchString(s) {
			t.Errorf("gecerli alan reddedildi: %q", s)
		}
	}
	for _, s := range gecersiz {
		if alanAdiRe.MatchString(s) {
			t.Errorf("gecersiz alan KABUL edildi (guvenlik): %q", s)
		}
	}
}

// TestVtSatirlariCoz — "ad<ayrac>boyut" ayristirmasi: bosluklu ad korunmali
// (SON ayracta bolunur), bicimsiz satir sessizce atlanmali.
func TestVtSatirlariCoz(t *testing.T) {
	cikti := "musteri_db|128\r\nbos satir atlanir\nBir Ad|0\n|5\nsonhata|abc\n"
	liste := vtSatirlariCoz(cikti, "|")
	if len(liste) != 2 {
		t.Fatalf("2 gecerli satir bekleniyordu, %d geldi: %+v", len(liste), liste)
	}
	if liste[0].Ad != "musteri_db" || liste[0].BoyutMB != 128 {
		t.Errorf("1. satir yanlis: %+v", liste[0])
	}
	if liste[1].Ad != "Bir Ad" || liste[1].BoyutMB != 0 {
		t.Errorf("bosluklu ad korunmadi: %+v", liste[1])
	}
}

// TestVeritabaniOlusturParolaEnjeksiyon — HIGH denetim bulgusu kilidi: sqlcmd -Q
// metnini CLIENT-SIDE satir-satir isler (tek "GO" batch ayiraci, "$(...)" degisken
// ikamesi); tirnak-ciftleme yalniz SUNUCU parse'inda korur. CR/LF veya "$(" iceren
// parola sqlcmd'ye ULASMADAN reddedilmeli. Gecerli motor/ad/kullanici verilir ki
// akis parola kontrolune ulassin; kotu parola erken doner (MSSQL GEREKMEZ).
func TestVeritabaniOlusturParolaEnjeksiyon(t *testing.T) {
	kotu := []string{
		"x\nGO\nEXEC(0x00)\nGO\n", // LF batch ayirici enjeksiyonu
		"pw\rGO\r",                // CR ayirici
		"a$(hostname)b",           // sqlcmd degisken ikamesi
	}
	for _, p := range kotu {
		err := VeritabaniOlustur("mssql", "testdb", "testu", p)
		if err == nil {
			t.Fatalf("kotu parola %q KABUL edildi — reddedilmeliydi", p)
		}
		if !errors.Is(err, ErrGecersizIstek) {
			t.Fatalf("kotu parola %q icin ErrGecersizIstek beklendi, gelen: %v", p, err)
		}
	}
}
