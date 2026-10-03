package marka

// yaz.go — markanın YAZMA yolu (eski whitelabel eklentisinden çekirdeğe taşındı).
//
// Kök markası bugünkü yerinde (Kok/marka.json + görseller) kalır; bayi markası
// görselleri Kok/bayi/<anahtar>/ altındadır (metinler DB'de, depo.go).
//
// 🔴 Görsel güvenliği (tek kapı: GorselDogrula):
//   - İstemcinin Content-Type'ına ve dosya adına GÜVENİLMEZ; tip SİHİRLİ BAYT ile
//     tespit edilir, SVG/HTML hiçbir dala uymaz → reddedilir.
//   - PNG/JPEG/GIF image.DecodeConfig ile ayrıştırılır (boyut ≤4096 px, piksel
//     ≤16M); PNG ve JPEG YENİDEN KODLANIR — EXIF/GPS ve dosya sonuna eklenmiş
//     poliglot kuyruk (HTML/zip) atılır.
//   - WEBP/ICO başlık doğrulamasıyla kalır (golang.org/x/image go.mod'da yok).
//   - Boyut ≤ LogoMaks (512 KB). Dosya adı sunucunundur (logo.bin + logo.tip).
//   - Servis tarafı ayrıca nosniff + CSP sandbox + MIME izin listesi uygular.

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"sync"
)

// Yuva — yüklenebilir bir görsel yuvası.
type Yuva struct {
	Ad    string // logo | banner | favicon
	Veri  string // dosya adı (uzantısız; ad sunucunun)
	TipAd string
}

var yuvalar = map[string]Yuva{
	"logo":    {"logo", LogoDosya, TipDosya},
	"banner":  {"banner", BannerDosya, BannerTip},
	"favicon": {"favicon", FaviconDosya, FaviconTip},
}

// YuvaBul — ad → yuva. Bilinmeyen ad (yol kaçışı dahil) false.
func YuvaBul(ad string) (Yuva, bool) {
	y, ok := yuvalar[ad]
	return y, ok
}

// yazKilidi — süreç içi tek yazar (kök dosyası + görsel yuvaları).
var yazKilidi sync.Mutex

const (
	gorselMaksPx     = 4096
	gorselMaksPiksel = 16 * 1024 * 1024
)

// Görsel doğrulama hataları — kullanıcıya dönük, ham ayrıntı içermez.
var (
	ErrGorselBos   = errors.New("Dosya boş.")
	ErrGorselBuyuk = errors.New("Görsel en fazla 512 KB olabilir.")
	ErrGorselTip   = errors.New("Yalnız PNG, JPEG, WEBP, GIF ve ICO kabul edilir. (SVG güvenlik nedeniyle kabul edilmez.)")
	ErrGorselBozuk = errors.New("Görsel okunamadı ya da boyutu çok büyük (en fazla 4096×4096 piksel).")
)

// TipTespit — SİHİRLİ BAYT ile tip. Tanınmayan her şey (SVG, HTML, metin) "".
func TipTespit(b []byte) string {
	switch {
	case len(b) >= 8 && bytes.Equal(b[:8], []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}):
		return "image/png"
	case len(b) >= 3 && b[0] == 0xff && b[1] == 0xd8 && b[2] == 0xff:
		return "image/jpeg"
	case len(b) >= 6 && (bytes.Equal(b[:6], []byte("GIF87a")) || bytes.Equal(b[:6], []byte("GIF89a"))):
		return "image/gif"
	case len(b) >= 16 && bytes.Equal(b[:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP")) &&
		bytes.Equal(b[12:15], []byte("VP8")):
		return "image/webp"
	case len(b) >= 6 && b[0] == 0x00 && b[1] == 0x00 && b[2] == 0x01 && b[3] == 0x00:
		return "image/x-icon"
	}
	return ""
}

// GorselDogrula — yüklenen baytları doğrular; servis edilecek (gerekirse yeniden
// kodlanmış) baytları ve MIME tipini döndürür.
func GorselDogrula(ham []byte) ([]byte, string, error) {
	if len(ham) == 0 {
		return nil, "", ErrGorselBos
	}
	if len(ham) > LogoMaks {
		return nil, "", ErrGorselBuyuk
	}
	tip := TipTespit(ham)
	switch tip {
	case "image/png", "image/jpeg", "image/gif":
		cfg, _, err := image.DecodeConfig(bytes.NewReader(ham))
		if err != nil || cfg.Width <= 0 || cfg.Height <= 0 ||
			cfg.Width > gorselMaksPx || cfg.Height > gorselMaksPx ||
			cfg.Width*cfg.Height > gorselMaksPiksel {
			return nil, "", ErrGorselBozuk
		}
		if tip == "image/gif" {
			// Animasyon korunur; çözümlenebildiğini kanıtla (bozuk çerçeve reddi).
			if _, err := gif.DecodeAll(bytes.NewReader(ham)); err != nil {
				return nil, "", ErrGorselBozuk
			}
			return ham, tip, nil
		}
		img, _, err := image.Decode(bytes.NewReader(ham))
		if err != nil {
			return nil, "", ErrGorselBozuk
		}
		var b bytes.Buffer
		if tip == "image/png" {
			err = (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&b, img)
		} else {
			err = jpeg.Encode(&b, img, &jpeg.Options{Quality: 90})
		}
		if err != nil {
			return nil, "", ErrGorselBozuk
		}
		// Yeniden kodlama bazen büyütür; sınır SERVİS edilen baytlara uygulanır.
		if b.Len() > LogoMaks {
			return nil, "", ErrGorselBuyuk
		}
		return b.Bytes(), tip, nil
	case "image/webp":
		// RIFF boyutu dosyayla tutarlı olmalı (kesik/eklenmiş kuyruk reddi).
		n := binary.LittleEndian.Uint32(ham[4:8])
		if int(n)+8 != len(ham) {
			return nil, "", ErrGorselBozuk
		}
		return ham, tip, nil
	case "image/x-icon":
		adet := int(binary.LittleEndian.Uint16(ham[4:6]))
		if adet < 1 || adet > 64 || len(ham) < 6+16*adet {
			return nil, "", ErrGorselBozuk
		}
		for i := 0; i < adet; i++ {
			g := ham[6+16*i:]
			boy := binary.LittleEndian.Uint32(g[8:12])
			ofs := binary.LittleEndian.Uint32(g[12:16])
			if boy == 0 || uint64(ofs)+uint64(boy) > uint64(len(ham)) {
				return nil, "", ErrGorselBozuk
			}
		}
		return ham, tip, nil
	}
	return nil, "", ErrGorselTip
}

// dosyaYaz — geçici dosya (BENZERSİZ ad) + fsync + rename. Okuyan ya eski ya
// yeni içeriği görür. (webmail.go'daki eski atomikYaz sabit ".yeni" adı
// kullanır ve eşzamanlı yazarda karışır; yeni yazma yolu bunu kullanır.)
func dosyaYaz(yol string, veri []byte, izin os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(yol), 0o755); err != nil {
		return err
	}
	tf, err := os.CreateTemp(filepath.Dir(yol), "."+filepath.Base(yol)+".*.tmp")
	if err != nil {
		return err
	}
	gec := tf.Name()
	temizle := func(e error) error {
		tf.Close()
		os.Remove(gec)
		return e
	}
	if _, err := tf.Write(veri); err != nil {
		return temizle(err)
	}
	if err := tf.Sync(); err != nil {
		return temizle(err)
	}
	if err := tf.Chmod(izin); err != nil {
		return temizle(err)
	}
	if err := tf.Close(); err != nil {
		os.Remove(gec)
		return err
	}
	return os.Rename(gec, yol)
}

// gorselDurumDiz — diskteki gerçeklik: yuva dolu mu + sürüm (mtime ms).
// Milisaniye: JS'e 2^53 altında gider, aynı saniyedeki iki yükleme ayrışır.
func gorselDurumDiz(dizin string, y Yuva) (bool, int64) {
	fi, err := os.Stat(filepath.Join(dizin, y.Veri))
	if err != nil || fi.Size() == 0 || fi.Size() > LogoMaks {
		return false, 0
	}
	if !LogoTipGecerli(tipOkuDiz(dizin, y.TipAd)) {
		return false, 0
	}
	return true, fi.ModTime().UnixMilli()
}

// gorselleriOlc — Marka'nın görsel bayraklarını DİSKTEN doldurur.
func gorselleriOlc(m Marka, dizin string) Marka {
	m.Logo, m.LogoSurum = gorselDurumDiz(dizin, yuvalar["logo"])
	m.Banner, m.BannerSurum = gorselDurumDiz(dizin, yuvalar["banner"])
	m.Favicon, m.FaviconSurum = gorselDurumDiz(dizin, yuvalar["favicon"])
	return m
}

// gorselYazDiz — doğrulanmış baytları yuvaya yazar (önce veri, sonra tip).
func gorselYazDiz(dizin string, y Yuva, veri []byte, tip string) error {
	if err := dosyaYaz(filepath.Join(dizin, y.Veri), veri, 0o644); err != nil {
		return err
	}
	return dosyaYaz(filepath.Join(dizin, y.TipAd), []byte(tip), 0o644)
}

// gorselSilDiz — yuvayı boşaltır (yoksa hata değil).
func gorselSilDiz(dizin string, y Yuva) error {
	for _, ad := range []string{y.Veri, y.TipAd} {
		if err := os.Remove(filepath.Join(dizin, ad)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// KokOkuYonetim — kök markasının YÖNETİM görünümü (webmail_marka dahil);
// görsel bayrakları diskten ölçülür.
func KokOkuYonetim() (Marka, error) {
	m, err := OkuKesin()
	if err != nil {
		return m, err
	}
	return gorselleriOlc(m, Kok), nil
}

// KokYaz — kök markasını doğrulayıp yazar. Çağıran yazKilidi'ni TUTMALI.
// Görsel bayrakları gövdeden değil DİSKTEN gelir.
func kokYazKilitli(m Marka) (Marka, error) {
	m = gorselleriOlc(Normalize(m), Kok)
	ham, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return m, err
	}
	if err := dosyaYaz(filepath.Join(Kok, AyarDosya), ham, 0o644); err != nil {
		return m, err
	}
	return m, nil
}
