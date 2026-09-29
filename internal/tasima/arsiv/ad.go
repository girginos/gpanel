package arsiv

import (
	"archive/tar"
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

// bagAgaci: seçimdeki symlink'lerin ad ağacı (seçime göre adlar). Arşivdeki sıradan ve
// bağın yazılıp yazılmayacağından bağımsız olarak envanterden bir kez kurulur; yalnız
// bir bağa giden dizinler düğümdür. Yürüme adımı yalnız bileşen adını özetler → bir bağın
// denetimi hedef + ad uzunluğunda doğrusal.
type bagAgaci struct {
	alt map[string]*bagAgaci
	bag bool
}

func bagAgaciKur(uyeler []Uye, onek string) *bagAgaci {
	k := &bagAgaci{}
	for _, u := range uyeler {
		rel, ok := altinda(u.Ad, onek)
		if !ok || rel == "" || u.Tur != tar.TypeSymlink {
			continue
		}
		d := k
		for _, p := range strings.Split(rel, "/") {
			if d.alt == nil {
				d.alt = map[string]*bagAgaci{}
			}
			c := d.alt[p]
			if c == nil {
				c = &bagAgaci{}
				d.alt[p] = c
			}
			d = c
		}
		d.bag = true
	}
	return k
}

// cocuk: p adlı alt düğüm; ağaçta yoksa nil (nil düğümün çocuğu da nil).
func (d *bagAgaci) cocuk(p string) *bagAgaci {
	if d == nil {
		return nil
	}
	return d.alt[p]
}

// bagAltinda: rel'in üst bileşenlerinden (rel'in kendisi hariç) biri seçimde bağ mı?
// (tar böyle bir üyeyi bağı izleyerek yazardı.)
func (k *bagAgaci) bagAltinda(rel string) bool {
	parcalar := strings.Split(rel, "/")
	d := k
	for _, p := range parcalar[:len(parcalar)-1] {
		if d = d.cocuk(p); d == nil {
			return false
		}
		if d.bag {
			return true
		}
	}
	return false
}

// Symlink atlama nedenleri.
const (
	bagDisari = "mutlak ya da seçili kök dışına bağ"
	bagZincir = "başka bir bağdan geçen bağ"
)

// bagNedeni: seçime göre rel konumundaki symlink yazılabilirse "", değilse atlama nedeni.
// Hedef göreli olmalı ve rel'in dizininden bileşen bileşen yürünür: ".." seçili kökün
// üstüne çıkamaz; yol seçimdeki HİÇBİR bağdan (ara ya da son bileşen) geçemez. Sözcüksel
// Clean yetmez: "d/l -> .." ile "x -> d/l/.." sözcüksel olarak içeride, diskte ise
// kökün üstündedir. Arşivin kurduğu ağaçta hiçbir bağdan geçilmediği için diskteki
// çözüm yürünen yolla aynıdır (backups/granular.go restoreArsivTara politikası; kök =
// seçim). Hedef kökte ÖNCEDEN duran bağlar kiracınındır; onların verdiği erişim zaten
// kiracıdadır.
func (k *bagAgaci) bagNedeni(rel, hedef string) string {
	if hedef == "" || path.IsAbs(hedef) || strings.Contains(hedef, `\`) {
		return bagDisari
	}
	yigin := []*bagAgaci{k} // yigin[0] seçili kök; nil = ağaçta olmayan dizin
	parcalar := strings.Split(rel, "/")
	for _, p := range parcalar[:len(parcalar)-1] {
		yigin = append(yigin, yigin[len(yigin)-1].cocuk(p))
	}
	for _, p := range strings.Split(hedef, "/") {
		switch p {
		case "", ".":
			continue
		case "..":
			if len(yigin) == 1 {
				return bagDisari
			}
			yigin = yigin[:len(yigin)-1]
			continue
		}
		c := yigin[len(yigin)-1].cocuk(p)
		if c != nil && c.bag {
			return bagZincir
		}
		yigin = append(yigin, c)
	}
	return ""
}
