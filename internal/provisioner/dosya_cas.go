package provisioner

// Küçük dosya yardımcıları (açılış süpürmeleri için): boyut sınırlı okuma ve karşılaştırmalı (CAS)
// atomik yazım. Süpürmeler arka planda, kullanıcı uçlarıyla EŞZAMANLI koşar; başka bir yazıcının
// az önce yazdığı içeriği ezmemek için okunan içerik yazım anında yeniden doğrulanır.

import (
	"bytes"
	"fmt"
	"io"
	"os"
)

// sinirliDosyaOku — dosyayı en çok `sinir` bayt okur; büyüğü hata (bellek şişirmesin). Tam sınırdaki dosya okunur.
func sinirliDosyaOku(yol string, sinir int64) ([]byte, error) {
	f, err := os.Open(yol)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, sinir+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > sinir {
		return nil, fmt.Errorf("beklenenden büyük (> %d bayt)", sinir)
	}
	return b, nil
}

// casYaz — dosya hâlâ `once` ise `yeni` ile ATOMİK (temp+rename) değiştirir, kipi korur. İçerik bu arada
// değişmişse (başka bir yazıcı) DOKUNMAZ. true = yazıldı.
func casYaz(yol string, once, yeni []byte) (bool, error) {
	cur, err := os.ReadFile(yol)
	if err != nil {
		return false, err
	}
	if !bytes.Equal(cur, once) {
		return false, nil
	}
	mod := os.FileMode(0o644)
	if st, err := os.Stat(yol); err == nil {
		mod = st.Mode().Perm()
	}
	return acmeAtomikYaz(yol, yeni, mod)
}
