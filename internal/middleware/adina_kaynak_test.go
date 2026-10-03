package middleware_test

import (
	"testing"

	"girginospanel/internal/hesapaski"
	"girginospanel/internal/middleware"
)

// Adına-oturumun askı kuralı hesapaski'nin bayi-açabilir kaynaklarıyla AYNI olmalı.
func TestAdinaAskiKaynaklari_HesapaskiIleAyni(t *testing.T) {
	d := func(kaynak string) int {
		kod, _ := middleware.AdinaKararKaynak(7, kaynak)
		return kod
	}
	if d(hesapaski.KaynakBayiElle) != 0 || d(hesapaski.KaynakBayi) != 0 {
		t.Fatal("bayinin açabildiği askı kaynaklarında adına-oturum açılmalı")
	}
	if d(hesapaski.KaynakKok) == 0 {
		t.Fatal("kök askısında bayi adına-oturum açamamalı")
	}
}
