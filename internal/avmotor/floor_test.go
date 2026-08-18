package avmotor

import "testing"

// 🔴 B.1b: ince bir uzak set taban kurallarini KALDIRAMAZ (floor).
func TestTabanFloorUnion(t *testing.T) {
	taban := TabanSet()
	tabanN := len(taban.Kurallar)

	// Kotu niyetli uzak set: yuksek surum, TEK etkisiz kural (backdoor
	// kurallarini silmeye calisiyor).
	uzak := KuralSeti{
		Surum:    9999,
		Kurallar: []Kural{{ID: "ETKISIZ", Desen: "asla-eslesmez-xyzzy", Puan: 1}},
	}
	sonuc := tabanFloorBirlestir(taban, uzak)

	// Taban kurallarinin HEPSI korunmali + 1 yeni.
	if len(sonuc.Kurallar) != tabanN+1 {
		t.Fatalf("floor bozuldu: %d kural (taban %d + 1 bekleniyordu)", len(sonuc.Kurallar), tabanN)
	}
	// Kritik taban kurali hala VAR mi (backdoor yakalayan)?
	varmi := false
	for _, k := range sonuc.Kurallar {
		if k.ID == "GOSP-PHP-EVAL-B64" {
			varmi = true
		}
	}
	if !varmi {
		t.Error("taban kurali GOSP-PHP-EVAL-B64 KALDIRILDI — floor ihlali")
	}
	if sonuc.Surum != 9999 {
		t.Errorf("surum uzaktan gelmeli: %d", sonuc.Surum)
	}
}

// Uzak set AYNI ID'yi override edebilmeli (yeni desen/puan).
func TestTabanFloorOverride(t *testing.T) {
	taban := TabanSet()
	tabanN := len(taban.Kurallar)
	uzak := KuralSeti{Surum: 100, Kurallar: []Kural{
		{ID: "GOSP-PHP-EVAL-B64", Desen: "guncellenmis-desen", Puan: 120},
	}}
	sonuc := tabanFloorBirlestir(taban, uzak)
	if len(sonuc.Kurallar) != tabanN {
		t.Errorf("override kural sayisini degistirmemeli: %d != %d", len(sonuc.Kurallar), tabanN)
	}
	for _, k := range sonuc.Kurallar {
		if k.ID == "GOSP-PHP-EVAL-B64" && k.Puan != 120 {
			t.Errorf("override uygulanmadi, puan %d", k.Puan)
		}
	}
}
