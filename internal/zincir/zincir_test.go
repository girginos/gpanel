package zincir

import "testing"

func ol(asama, seviye string) Olay { return Olay{Asama: asama, Seviye: seviye} }

// Tek aşama (aynı aşamada çok olay) → zincir DEĞİL (yanlış-pozitif önler).
func TestZincirTekAsamaYetersiz(t *testing.T) {
	if _, _, ok := ZincirPuanla([]Olay{ol("dosya_yazma", "kritik"), ol("dosya_yazma", "uyari")}); ok {
		t.Fatal("tek aşama zincir sayıldı")
	}
}

// İki aşama (yaz + çalıştır) → 60 + kritik 5 + yaz&çalıştır 5 = 70.
func TestZincirIkiAsama(t *testing.T) {
	g, as, ok := ZincirPuanla([]Olay{ol("dosya_yazma", "kritik"), ol("calistirma", "kritik")})
	if !ok || g != 70 {
		t.Fatalf("2 aşama güven=%d ok=%v (70 bekleniyor)", g, ok)
	}
	if len(as) != 2 || as[0] != "dosya_yazma" || as[1] != "calistirma" {
		t.Fatalf("aşama sırası: %v", as)
	}
}

// Dört aşama → cap 99, sıralı.
func TestZincirDortAsama(t *testing.T) {
	g, as, ok := ZincirPuanla([]Olay{ol("c2", "uyari"), ol("giris", "uyari"), ol("dosya_yazma", "kritik"), ol("calistirma", "kritik")})
	if !ok || g != 99 {
		t.Fatalf("4 aşama güven=%d (99 bekleniyor)", g)
	}
	if len(as) != 4 || as[0] != "giris" || as[3] != "c2" {
		t.Fatalf("kill-chain sırası bozuk: %v", as)
	}
}

// Bilinmeyen aşama yok sayılır → geriye tek geçerli aşama kalırsa zincir DEĞİL.
func TestZincirBilinmeyenAsama(t *testing.T) {
	if _, _, ok := ZincirPuanla([]Olay{ol("dosya_yazma", "uyari"), ol("zzz", "uyari")}); ok {
		t.Fatal("bilinmeyen aşama geçerli sayıldı")
	}
}

func TestZincirImza(t *testing.T) {
	a := ZincirImza(5, []string{"dosya_yazma", "calistirma"})
	b := ZincirImza(5, []string{"dosya_yazma", "calistirma"})
	c := ZincirImza(6, []string{"dosya_yazma", "calistirma"})
	d := ZincirImza(5, []string{"dosya_yazma", "c2"})
	if a != b {
		t.Fatal("aynı girdi farklı imza")
	}
	if a == c || a == d {
		t.Fatal("farklı girdi aynı imza")
	}
	if len(a) != 32 {
		t.Fatalf("imza uzunluğu %d", len(a))
	}
}

func TestAsamaOzet(t *testing.T) {
	if s := AsamaOzet([]string{"dosya_yazma", "calistirma", "c2"}); s != "File Write → Execution → C2" {
		t.Fatalf("özet: %q", s)
	}
}
