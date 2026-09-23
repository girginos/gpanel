//go:build ignore

// Test yardimcisi — panelin KENDI auth paketiyle jeton uretir.
//
// Neden gerekli: panelde admin girisi yalniz `root` + SISTEM parolasi ile
// yapiliyor (auth.Login → rootParolaDogrula); veritabanindaki role='admin'
// satirlari giris yapamaz. Otomatik testte sistem root parolasini kullanmak
// dogru olmadigi icin jeton, panelin kendi imzalama fonksiyonuyla uretilir.
// Sir zaten calisan surecin ortamindan okunur — yeni bir yetki OLUSTURMAZ.
//
// Rol kapilari (ADR-GP-001): kapi modunda token alindigi kapiya baglidir ("kpi");
// zorlama acikken bagsiz token rol kapilarinda 401 alir. -kapi:
//
//	oto (varsayilan)         scripts/e2e-jeton.py ile AYNI kural: kapi ayari
//	                         (/etc/girginospanel/kapi.json) varsa rolun KENDI kapisi
//	                         (reseller → bayi, digerleri → admin; giriste de boyle
//	                         baglanir), yoksa bagsiz — kapi modu yokken bagsiz token
//	                         her yerde gecer, gecis sonrasi da eski kapiya uyar.
//	admin|bayi|musteri|eski  o kapiya bagli token.
//	"" (-kapi=)              bagsiz (e2e-jeton.py --kapisiz; eski kapi testleri).
package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"

	"girginospanel/internal/auth"
	"girginospanel/internal/kapi"
)

func main() {
	rol := flag.String("rol", "admin", "rol")
	kul := flag.String("kullanici", "testadmin", "kullanici adi")
	uid := flag.Int64("uid", 1, "kullanici id")
	rid := flag.Int64("rid", 0, "reseller id (rol=reseller icin)")
	kapiB := flag.String("kapi", "oto", `token'in bagli oldugu kapi: oto|admin|bayi|musteri|eski|"" (bagsiz)`)
	ayar := flag.String("kapi-ayar", kapi.AyarYolu, "-kapi oto icin bakilan kapi ayari (kum havuzu testi icin)")
	flag.Parse()

	k, err := kapiSec(*kapiB, *rol, *ayar)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	sir := []byte(os.Getenv("PANEL_JWT_SECRET"))
	if len(sir) < 32 {
		fmt.Fprintln(os.Stderr, "PANEL_JWT_SECRET yok/kisa")
		os.Exit(1)
	}
	var t string
	if *rol == "reseller" {
		t, err = auth.IssueResellerAt(sir, 3600, *uid, *kul, *rid, 0, k)
	} else {
		t, err = auth.IssueAt(sir, 3600, *uid, *kul, *rol, 0, k)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(t)
}

// kapiSec: -kapi degerini token'a yazilacak kapi adina cevirir (bkz. dosya basi).
func kapiSec(deger, rol, ayarYolu string) (string, error) {
	switch deger {
	case "", kapi.Admin, kapi.Bayi, kapi.Musteri, kapi.Eski:
		return deger, nil
	case "oto":
		if _, err := os.Stat(ayarYolu); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return "", nil
			}
			// Kapi modu var mi bilinmiyor: tahmin edip yanlis bagli token uretme.
			return "", fmt.Errorf("kapi ayari denetlenemedi (%s): %v — -kapi ile acikca verin", ayarYolu, err)
		}
		if rol == "reseller" {
			return kapi.Bayi, nil
		}
		return kapi.Admin, nil
	}
	return "", fmt.Errorf("gecersiz -kapi %q (oto|admin|bayi|musteri|eski|\"\")", deger)
}
