package avpaket

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

// 🔴 İMZA KAPISI TESTİ: paket doğru anahtarla açılır, YANLIŞ anahtar ya da
// KURCALANMIŞ paket REDDEDİLİR. Bu, sahte kural enjeksiyonuna karşı asıl savunma.
func TestPaketImzaKapisi(t *testing.T) {
	pk, sk, _ := ed25519.GenerateKey(rand.Reader)
	set := []byte(`{"surum":5,"kurallar":[{"id":"X","desen":"eval"}]}`)

	paket, err := Olustur(set, sk, 5, "2026-08-18T00:00:00Z")
	if err != nil {
		t.Fatalf("olustur: %v", err)
	}

	t.Run("dogru anahtar acar", func(t *testing.T) {
		b, govde, err := Ac(paket, pk)
		if err != nil {
			t.Fatalf("ac: %v", err)
		}
		if b.Surum != 5 {
			t.Errorf("surum 5 bekleniyor, %d", b.Surum)
		}
		if string(govde) != string(set) {
			t.Errorf("govde round-trip bozuk")
		}
	})

	t.Run("YANLIS anahtar reddeder", func(t *testing.T) {
		digerPK, _, _ := ed25519.GenerateKey(rand.Reader)
		if _, _, err := Ac(paket, digerPK); err == nil {
			t.Fatal("yanlis anahtarla acildi — imza kapisi ACIK")
		}
	})

	t.Run("KURCALANMIS baslik reddeder", func(t *testing.T) {
		k := make([]byte, len(paket))
		copy(k, paket)
		// başlık bölgesinde bir bayt çevir (sihir+u32'den sonra)
		k[15] ^= 0xff
		if _, _, err := Ac(k, pk); err == nil {
			t.Fatal("kurcalanmis baslik kabul edildi")
		}
	})

	t.Run("KURCALANMIS govde reddeder", func(t *testing.T) {
		k := make([]byte, len(paket))
		copy(k, paket)
		k[len(k)-5] ^= 0xff // şifreli gövdenin sonu
		if _, _, err := Ac(k, pk); err == nil {
			t.Fatal("kurcalanmis govde kabul edildi (GCM/sha kapisi acik)")
		}
	})

	t.Run("bozuk sihir reddeder", func(t *testing.T) {
		if _, _, err := Ac([]byte("XXXXXXXX....."), pk); err == nil {
			t.Fatal("bozuk sihir kabul edildi")
		}
	})
}
