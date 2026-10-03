package ajan

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"girginospanel/internal/destek/protokol"
	"girginospanel/internal/gizli"
	"girginospanel/internal/sirdosya"
)

const cihazBaglam = "destek-cihaz-v1"

// cihazAnahtariYukle: cihazın Ed25519 anahtarı. Dosya yoksa üretilir (0600, panelin
// kullanıcısına ait; içerik gizli.SaklaBagli ile db.key'e bağlı şifreli). Dosya
// başkasına aitse / grup-diğer erişebiliyorsa REDDEDİLİR (kimlik sızmış sayılır).
// İçerik çözülemiyorsa (db.key değişti) eski dosya .eski olarak saklanır ve yeni
// kimlik üretilir — eşleştirmeler yereldir, etkilenmez; merkez yeni cihazı görür.
func cihazAnahtariYukle(yol string) (*protokol.AnahtarCifti, error) {
	if yol == "" {
		yol = VarsayilanCihazYolu
	}
	b, st, err := sirdosya.Oku(yol, 4096)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return cihazUret(yol)
	case err != nil:
		return nil, fmt.Errorf("cihaz anahtarı okunamadı: %w", err)
	}
	if uid, _, ok := sirdosya.Sahip(st); !ok || int(uid) != os.Getuid() || st.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("cihaz anahtarı dosyasının sahibi/izni güvensiz (mod %o)", st.Mode().Perm())
	}
	duz := gizli.CozBagli(strings.TrimSpace(string(b)), cihazBaglam)
	tohum, err := base64.RawURLEncoding.Strict().DecodeString(duz)
	if err == nil {
		if c, err := protokol.AnahtarCiftiTohumdan(tohum); err == nil {
			return c, nil
		}
	}
	log.Printf("🔴 destek: cihaz anahtarı çözülemedi (db.key değişmiş olabilir) — yeni kimlik üretiliyor, eskisi %s.eski", yol)
	if err := os.Rename(yol, yol+".eski"); err != nil {
		return nil, fmt.Errorf("bozuk cihaz anahtarı kenara alınamadı: %w", err)
	}
	return cihazUret(yol)
}

func cihazUret(yol string) (*protokol.AnahtarCifti, error) {
	c, err := protokol.AnahtarCiftiUret(nil)
	if err != nil {
		return nil, err
	}
	icerik := gizli.SaklaBagli(base64.RawURLEncoding.EncodeToString(c.Ozel.Seed()), cihazBaglam)
	if err := sirdosya.Yaz(yol, []byte(icerik+"\n"), 0o600, os.Getuid(), os.Getgid()); err != nil {
		return nil, fmt.Errorf("cihaz anahtarı yazılamadı: %w", err)
	}
	log.Printf("destek: yeni cihaz kimliği üretildi (%s)", c.Kimlik)
	return c, nil
}
