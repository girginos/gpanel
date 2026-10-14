// Package uzak — kökün sistem yedeklerinin UZAK kopya orkestratörü.
//
// Yerel yedek "tamam" olunca (backups satırı yazıldı) iş kuyruğa (backup_uzak_kopyalar)
// yazılır; TEK işçi dosyayı age ile şifreleyip seçili uzak hedefe (hedef.Hedef)
// gönderir, uzaktan geri okuyarak doğrular, sha256 manifestini yanına yazar ve
// saklama politikasını yalnız kendi manifestli kopyaları üzerinde uygular.
//
// Bu paket backups paketini İÇE AKTARMAZ (döngü olmasın): hedef kurma ve yerel
// yol gibi bağımlılıklar Isci alanlarından gelir.
//
// 🔴 Özel anahtar hiçbir günlüğe, hata metnine, denetim kaydına ya da API
// yanıtına girmez; yalnız anahtar üretiminin TEK yanıtında kurtarma dosyası olarak
// döner (bir daha gösterilmez).
package uzak

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"runtime/debug"
	"strings"
	"time"

	"filippo.io/age"
)

// Şifreleme türleri (backup_uzak_kopyalar.sifreleme, VARCHAR(8)).
const (
	SifrelemeX25519 = "x25519"
	SifrelemeHibrit = "hibrit"
)

const (
	onekHibritAlici  = "age1pq1"
	ageSurumYedek    = "v1.3.2"
	hibritAciklama   = "ML-KEM-768 + X25519 (age >= 1.3.0 gerekir)"
	x25519Aciklama   = "X25519"
	kurtarmaDosyaOn  = "gosp-yedek-anahtar-"
	kurtarmaDosyaSon = ".txt"
)

// AgeSurum — derlemedeki filippo.io/age sürümü (okunamazsa go.mod sürümü).
func AgeSurum() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, m := range bi.Deps {
			if m.Path == "filippo.io/age" && m.Version != "" {
				return m.Version
			}
		}
	}
	return ageSurumYedek
}

// HibritAlici — alıcı hibrit (post-kuantum) mi?
func HibritAlici(alici string) bool {
	return strings.HasPrefix(strings.TrimSpace(alici), onekHibritAlici)
}

// SifrelemeTuru — alıcıdan şifreleme türü.
func SifrelemeTuru(alici string) string {
	if HibritAlici(alici) {
		return SifrelemeHibrit
	}
	return SifrelemeX25519
}

// Parmak — alıcının (genel anahtar) SHA-256 parmak izi, küçük harf hex.
func Parmak(alici string) string {
	s := sha256.Sum256([]byte(strings.TrimSpace(alici)))
	return hex.EncodeToString(s[:])
}

// KisaParmak — arayüzde gösterilen ilk 16 hex karakter.
func KisaParmak(p string) string {
	if len(p) > 16 {
		return p[:16]
	}
	return p
}

// ErrAliciGecersiz — açık anahtar age alıcısı olarak çözülemedi.
var ErrAliciGecersiz = errors.New("geçersiz age açık anahtarı")

// AliciCoz — age alıcısını çözer (klasik X25519 ya da hibrit).
func AliciCoz(alici string) (age.Recipient, error) {
	alici = strings.TrimSpace(alici)
	if HibritAlici(alici) {
		r, err := age.ParseHybridRecipient(alici)
		if err != nil {
			return nil, ErrAliciGecersiz
		}
		return r, nil
	}
	r, err := age.ParseX25519Recipient(alici)
	if err != nil {
		return nil, ErrAliciGecersiz
	}
	return r, nil
}

// Uretilen — yeni üretilmiş anahtar çifti. Ozel YALNIZ üretim yanıtında kullanılır.
type Uretilen struct {
	Alici  string
	Ozel   string
	Hibrit bool
}

// AnahtarCifti — yeni age anahtar çifti (hibrit=false → klasik X25519).
func AnahtarCifti(hibrit bool) (Uretilen, error) {
	if hibrit {
		k, err := age.GenerateHybridIdentity()
		if err != nil {
			return Uretilen{}, err
		}
		return Uretilen{Alici: k.Recipient().String(), Ozel: k.String(), Hibrit: true}, nil
	}
	k, err := age.GenerateX25519Identity()
	if err != nil {
		return Uretilen{}, err
	}
	return Uretilen{Alici: k.Recipient().String(), Ozel: k.String()}, nil
}

// KurtarmaDosyasi — kullanıcıya TEK KEZ verilen kurtarma dosyasının metni. Sürüm
// bilgisi hibritte çözme aracını da söyler.
func KurtarmaDosyasi(u Uretilen, simdi time.Time) (ad, metin string) {
	tur, acik := SifrelemeX25519, x25519Aciklama
	if u.Hibrit {
		tur, acik = SifrelemeHibrit, hibritAciklama
	}
	p := Parmak(u.Alici)
	var b strings.Builder
	b.WriteString("# GirginOSPanel yedek kurtarma anahtarı (age)\n")
	b.WriteString("# Bu anahtar OLMADAN uzak yedek kopyaları GERİ YÜKLENEMEZ. Güvenli bir yerde saklayın.\n")
	fmt.Fprintf(&b, "# oluşturma: %s\n", simdi.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "# şifreleme: age %s, %s\n", tur, acik)
	fmt.Fprintf(&b, "# age sürümü: %s\n", AgeSurum())
	fmt.Fprintf(&b, "# açık anahtar (alıcı): %s\n", u.Alici)
	fmt.Fprintf(&b, "# parmak izi: %s\n", p)
	b.WriteString("# Çözme: girginospanel-server yedek-coz <dosya.age> --anahtar <bu dosya>   (ya da: age -d -i <bu dosya> <dosya.age>)\n")
	b.WriteString(u.Ozel + "\n")
	return kurtarmaDosyaOn + KisaParmak(p) + kurtarmaDosyaSon, b.String()
}

// sayanYazici — geçen baytı sayar ve özetler.
type sayanYazici struct {
	w io.Writer
	h interface {
		io.Writer
		Sum([]byte) []byte
	}
	n int64
}

func (s *sayanYazici) Write(p []byte) (int, error) {
	n, err := s.w.Write(p)
	if n > 0 {
		s.n += int64(n)
		_, _ = s.h.Write(p[:n])
	}
	return n, err
}

// SifreleSonuc — Sifrele'nin ölçümleri.
type SifreleSonuc struct {
	DuzBoyut, SifreliBoyut int64
	DuzSha, SifreliSha     string
}

// Sifrele — src'yi alıcıya age ile şifreleyip dst'ye akıtır (diske ikinci düz
// kopya YAZILMAZ). Düz ve şifreli boyut/sha256 akış sırasında hesaplanır.
func Sifrele(dst io.Writer, src io.Reader, alici age.Recipient) (SifreleSonuc, error) {
	duz := &sayanYazici{w: io.Discard, h: sha256.New()}
	sif := &sayanYazici{w: dst, h: sha256.New()}
	w, err := age.Encrypt(sif, alici)
	if err != nil {
		return SifreleSonuc{}, err
	}
	if _, err := io.Copy(io.MultiWriter(w, duz), src); err != nil {
		return SifreleSonuc{}, err
	}
	if err := w.Close(); err != nil {
		return SifreleSonuc{}, err
	}
	return SifreleSonuc{
		DuzBoyut: duz.n, SifreliBoyut: sif.n,
		DuzSha: hex.EncodeToString(duz.h.Sum(nil)), SifreliSha: hex.EncodeToString(sif.h.Sum(nil)),
	}, nil
}

// Coz — age şifreli src'yi kimliklerle (anahtar dosyası içeriği; yorum satırları
// serbest, klasik ve hibrit) çözüp dst'ye yazar. Yanlış anahtar → ErrAnahtarUymuyor.
func Coz(dst io.Writer, src io.Reader, anahtarMetni io.Reader) error {
	kimlikler, err := age.ParseIdentities(anahtarMetni)
	if err != nil {
		return ErrAnahtarOkunamadi
	}
	r, err := age.Decrypt(src, kimlikler...)
	if err != nil {
		var yanlis *age.NoIdentityMatchError
		if errors.As(err, &yanlis) {
			return ErrAnahtarUymuyor
		}
		return fmt.Errorf("could not decrypt: %w", err)
	}
	_, err = io.Copy(dst, r)
	return err
}

var (
	// ErrAnahtarOkunamadi — anahtar dosyasında geçerli bir age özel anahtarı yok.
	ErrAnahtarOkunamadi = errors.New("the key file has no valid age private key")
	// ErrAnahtarUymuyor — verilen anahtar bu dosyayı çözmüyor.
	ErrAnahtarUymuyor = errors.New("this key does not decrypt the file (wrong key)")
)
