package tasima

import "testing"

// Taşıma kiracı adımları (rsync yerleşimi, cp, tar) kiracının kaynak diliminde
// koşar; slice adı panel sk'sından türer (Kul testte ayrıcalıksız kimlik olabilir).
func TestKiraciAl_KaynakDilimi(t *testing.T) {
	eski := kiraciKul
	kiraciKul = func(string) string { return "nobody" }
	t.Cleanup(func() { kiraciKul = eski })
	ki, err := kiraciAl("c_ornek_com")
	if err != nil {
		t.Fatal(err)
	}
	if !ki.Dilim || ki.DilimSK != "c_ornek_com" || ki.Kul != "nobody" {
		t.Fatalf("kiracı: %+v", ki)
	}
	if _, err := kiraciAl("root"); err == nil {
		t.Fatal("panel biçimi dışı sk reddedilmeli")
	}
}
