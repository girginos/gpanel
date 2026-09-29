package arsiv

import (
	"fmt"
	"path"
	"strings"
)

const (
	// maxAdUzunluk: üye adı ve bağ hedefi için PATH_MAX.
	maxAdUzunluk = 4096
	// maxDerinlik: üye adındaki en çok bileşen sayısı.
	maxDerinlik = 64
)

// kisalt: hata metnine giren (arşivden gelen) adı sınırlar; %q ile basılır, denetim
// karakterleri kaçışlanır.
func kisalt(s string) string {
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}

func adHatasi(ham string) error {
	return fmt.Errorf("%w: %q", ErrUyeAdi, kisalt(ham))
}

// adTemizle: ham tar üye adını denetler ve normalize eder ("./", boş ve "." bileşenler
// atılır, sondaki "/" düşer). "" → arşiv kökü. Mutlak yol, ".." bileşeni, ters bölü,
// 4096 bayttan uzun ya da 64 bileşenden derin ad → ErrUyeAdi (arşivin tamamı reddedilir).
// NUL burada aranmaz: tar.Reader ad alanlarını NUL'da keser ve NUL içeren PAX yolunu
// ErrHeader ile reddeder (TestAc_NULluPAXYoluReddedilir bu varsayımı kilitler).
func adTemizle(ham string) (string, error) {
	if len(ham) > maxAdUzunluk || strings.HasPrefix(ham, "/") || strings.Contains(ham, `\`) {
		return "", adHatasi(ham)
	}
	parcalar := make([]string, 0, 8)
	for _, p := range strings.Split(ham, "/") {
		switch p {
		case "", ".":
			continue
		case "..":
			return "", adHatasi(ham)
		}
		parcalar = append(parcalar, p)
	}
	if len(parcalar) > maxDerinlik {
		return "", adHatasi(ham)
	}
	return strings.Join(parcalar, "/"), nil
}

// altinda: ad onek'in altında mı? Evet ise oneke göre göreli ad ("" = onek'in kendisi).
// onek "" → arşivin tamamı.
func altinda(ad, onek string) (string, bool) {
	switch {
	case onek == "":
		return ad, true
	case ad == onek:
		return "", true
	case strings.HasPrefix(ad, onek+"/"):
		return ad[len(onek)+1:], true
	}
	return "", false
}

// bagKokIcinde: seçime göre rel konumundaki symlink'in hedefi göreli mi ve seçili kökün
// İÇİNDE mi çözülür? (backups/granular.go restoreArsivTara politikası; kök = seçim.)
func bagKokIcinde(rel, hedef string) bool {
	if hedef == "" || path.IsAbs(hedef) || strings.Contains(hedef, `\`) {
		return false
	}
	c := path.Clean(path.Join(path.Dir(rel), hedef))
	return c != ".." && !strings.HasPrefix(c, "../")
}
