package kaynaklimit

import "testing"

// DBBaglantiKancasi: nil → plan değeri AYNEN (davranış değişmez); bağlıysa kısıcının değeri;
// negatif dönüş 0'a sıkıştırılır.
func TestBaglantiKanca(t *testing.T) {
	eski := DBBaglantiKancasi
	t.Cleanup(func() { DBBaglantiKancasi = eski })

	DBBaglantiKancasi = nil
	if got := baglantiKanca("u", 15); got != 15 {
		t.Fatalf("kanca yokken plan değeri aynen: %d", got)
	}
	var gelen string
	DBBaglantiKancasi = func(u string, plan int) int { gelen = u; return 2 }
	if got := baglantiKanca("u1", 15); got != 2 || gelen != "u1" {
		t.Fatalf("kanca değeri kullanılmalı: %d %q", got, gelen)
	}
	DBBaglantiKancasi = func(string, int) int { return -5 }
	if got := baglantiKanca("u", 15); got != 0 {
		t.Fatalf("negatif 0'a sıkışmalı: %d", got)
	}
}
