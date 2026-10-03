package tasima

import (
	"context"
	"io"
)

// uzakKabuk — kaynak sunucuya giden TÜM uzak çalıştırmaların dikişi.
//
// Üretimde *Kaynak uygular (ssh / sshpass / rsync; bkz. tasima.go). Kaynak.kabuk
// doluysa üç yöntem de ona devreder: testler buraya kaydedici bir sahte takar,
// komut dizgeleri ağa çıkmadan yakalanır ve komut_altin_test.go bunların
// sha256'sını sabitler. Uzak komut metninde (keşif, tespit, dump, DNS, sertifika,
// posta) yapılan HER değişiklik altın testi kırar — bilinçli değişiklikte tablo
// aynı committe güncellenir.
type uzakKabuk interface {
	// Calistir — uzak komutu çalıştırır, stdout'u döndürür.
	Calistir(ctx context.Context, uzakKomut string) (string, error)
	// RsyncCek — uzak dizini yerele kopyalar.
	RsyncCek(ctx context.Context, uzakYol, yerelYol string, ekstra ...string) (string, error)
	// AkisCalistir — uzak komutun stdout'unu belleğe almadan w'ye akıtır (dump).
	// Döndürür: ham (maskesiz) stderr + hata. Komut hiç başlatılamadıysa (yerel
	// hazırlık, ör. anahtar dosyası) hata *akisHazirlikHatasi'dır.
	AkisCalistir(ctx context.Context, uzakKomut string, w io.Writer) (string, error)
}

var _ uzakKabuk = (*Kaynak)(nil)

// akisHazirlikHatasi — AkisCalistir uzak komutu BAŞLATAMADI (yerel hazırlık
// hatası). Çağıran bunu uzak komut hatasından ayırır: hazırlık hatası olduğu gibi
// döner, uzak hata stderr'den yorumlanır.
type akisHazirlikHatasi struct{ err error }

func (e *akisHazirlikHatasi) Error() string { return e.err.Error() }
func (e *akisHazirlikHatasi) Unwrap() error { return e.err }
