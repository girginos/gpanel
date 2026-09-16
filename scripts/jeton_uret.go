//go:build ignore

// Test yardimcisi — panelin KENDI auth paketiyle jeton uretir.
//
// Neden gerekli: panelde admin girisi yalniz `root` + SISTEM parolasi ile
// yapiliyor (auth.Login → rootParolaDogrula); veritabanindaki role='admin'
// satirlari giris yapamaz. Otomatik testte sistem root parolasini kullanmak
// dogru olmadigi icin jeton, panelin kendi imzalama fonksiyonuyla uretilir.
// Sir zaten calisan surecin ortamindan okunur — yeni bir yetki OLUSTURMAZ.
package main

import (
	"flag"
	"fmt"
	"os"

	"girginospanel/internal/auth"
)

func main() {
	rol := flag.String("rol", "admin", "rol")
	kul := flag.String("kullanici", "testadmin", "kullanici adi")
	uid := flag.Int64("uid", 1, "kullanici id")
	rid := flag.Int64("rid", 0, "reseller id (rol=reseller icin)")
	flag.Parse()

	sir := []byte(os.Getenv("PANEL_JWT_SECRET"))
	if len(sir) < 32 {
		fmt.Fprintln(os.Stderr, "PANEL_JWT_SECRET yok/kisa")
		os.Exit(1)
	}
	var t string
	var err error
	if *rol == "reseller" {
		t, err = auth.IssueReseller(sir, 3600, *uid, *kul, *rid)
	} else {
		t, err = auth.Issue(sir, 3600, *uid, *kul, *rol)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(t)
}
