//go:build windows

// site_liste_windows_test.go — B-08: SiteListe ayristiricisinin (siteSatirlariCoz)
// birim testi. Ayristirma web katmanindan platform'a tasindi; SAF oldugu icin
// IIS'siz sinanir (appcmd ciktisi ornekle beslenir).
package platform

import "testing"

func TestSiteSatirlariCoz(t *testing.T) {
	// appcmd list site ornek ciktisi (coklu baglama + kaliba uymayan satir dahil).
	out := "SITE \"a.com\" (id:1,bindings:http/*:80:a.com,state:Started)\r\n" +
		"beklenmeyen satir — atlanmali\n" +
		"SITE \"b.net\" (id:2,bindings:http/*:80:b.net,https/*:443:b.net,state:Stopped)\n"
	liste := siteSatirlariCoz(out)
	if len(liste) != 2 {
		t.Fatalf("2 site bekleniyordu, %d geldi: %+v", len(liste), liste)
	}
	if liste[0].Ad != "a.com" || liste[0].Durum != "Started" || liste[0].Baglama != "http/*:80:a.com" {
		t.Errorf("1. site yanlis: %+v", liste[0])
	}
	// coklu baglama tek dizge olarak korunmali (regex .* acgozlu, state'e kadar).
	if liste[1].Ad != "b.net" || liste[1].Durum != "Stopped" ||
		liste[1].Baglama != "http/*:80:b.net,https/*:443:b.net" {
		t.Errorf("2. site (coklu baglama) yanlis: %+v", liste[1])
	}
	if len(siteSatirlariCoz("")) != 0 {
		t.Error("bos cikti bos liste vermeli")
	}
}
