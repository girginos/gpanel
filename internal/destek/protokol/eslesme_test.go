package protokol

import (
	"bytes"
	"strings"
	"testing"
)

func TestEslesmeKodu(t *testing.T) {
	kod, err := EslesmeKoduUret(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(kod) != EslesmeKoduUzunlugu {
		t.Fatalf("uzunluk %d", len(kod))
	}
	n, ok := EslesmeKoduNormal(kod)
	if !ok || n != kod {
		t.Fatalf("normal: %q %v", n, ok)
	}
	oz, ok := EslesmeKoduOzet(kod)
	if !ok || !hex64Deseni.MatchString(oz) {
		t.Fatalf("özet: %q", oz)
	}
	// Elle yazım: küçük harf + tire + boşluk aynı özeti verir.
	g := EslesmeKoduGorunen(kod)
	if strings.Count(g, "-") != 3 {
		t.Fatalf("görünen: %q", g)
	}
	oz2, ok := EslesmeKoduOzet(" " + strings.ToLower(g) + " ")
	if !ok || oz2 != oz {
		t.Fatalf("elle yazım özeti farklı: %q", oz2)
	}
	for _, kotu := range []string{"", "ABCD", kod + "A", strings.Replace(kod, kod[:1], "0", 1), "İ" + kod[1:], kod[:15] + "1"} {
		if _, ok := EslesmeKoduNormal(kotu); ok {
			t.Errorf("geçersiz kod kabul edildi: %q", kotu)
		}
	}
	// Sapmasız: 248 ve üstü bayt atılır.
	r := bytes.NewReader(append(bytes.Repeat([]byte{255}, 4), bytes.Repeat([]byte{0}, 16)...))
	k, err := EslesmeKoduUret(r)
	if err != nil || k != strings.Repeat("A", 16) {
		t.Fatalf("sapmasız seçim: %q %v", k, err)
	}
}
