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
//
// Bellek: düğüm başına tek harita girdisi ve bir bayt. Düğüm int32 indekstir (0 = seçili
// kök); kenar adı envanterdeki adın alt dizgesidir (kopya yok). Düğüm sayısı seçimdeki
// bağ adlarının toplam bileşen sayısını geçemez; o da Ac'de Sinirlar.BagBilesen ile
// sınırlıdır (TestBagAgaci_Bellek en kötü fikstürü ölçer).
type bagAgaci struct {
	cocuklar map[bagKenar]int32
	bag      []bool // düğüm → seçimde symlink mi
}

type bagKenar struct {
	ust int32
	ad  string
}

// yokDugum: ağaçta olmayan dizin; çocuğu da yokDugum'dur.
const yokDugum int32 = -1

func bagAgaciKur(uyeler []Uye, onek string) *bagAgaci {
	secilen := func(u Uye) (string, bool) {
		rel, ok := altinda(u.Ad, onek)
		return rel, ok && rel != "" && u.Tur == tar.TypeSymlink
	}
	// Düğüm sayısının üst sınırıyla önceden ayrılır: büyüyen haritanın eski tabloları
	// ölçülen toplam ayırmayı yaklaşık iki katına çıkarıyordu.
	n := 0
	for _, u := range uyeler {
		if rel, ok := secilen(u); ok {
			n += strings.Count(rel, "/") + 1
		}
	}
	k := &bagAgaci{cocuklar: make(map[bagKenar]int32, n), bag: make([]bool, 1, n+1)}
	for _, u := range uyeler {
		rel, ok := secilen(u)
		if !ok {
			continue
		}
		d := int32(0)
		for kalan, devam := rel, true; devam; { // Cut: Split'in dilimini ayırmaz
			var p string
			p, kalan, devam = strings.Cut(kalan, "/")
			c, bulundu := k.cocuklar[bagKenar{d, p}]
			if !bulundu {
				c = int32(len(k.bag))
				k.bag = append(k.bag, false)
				k.cocuklar[bagKenar{d, p}] = c
			}
			d = c
		}
		k.bag[d] = true
	}
	return k
}

// cocuk: d'nin p adlı alt düğümü; ağaçta yoksa yokDugum.
func (k *bagAgaci) cocuk(d int32, p string) int32 {
	if d == yokDugum {
		return yokDugum
	}
	if c, ok := k.cocuklar[bagKenar{d, p}]; ok {
		return c
	}
	return yokDugum
}

// bagAltinda: rel'in üst bileşenlerinden (rel'in kendisi hariç) biri seçimde bağ mı?
// (tar böyle bir üyeyi bağı izleyerek yazardı.)
func (k *bagAgaci) bagAltinda(rel string) bool {
	parcalar := strings.Split(rel, "/")
	d := int32(0)
	for _, p := range parcalar[:len(parcalar)-1] {
		if d = k.cocuk(d, p); d == yokDugum {
			return false
		}
		if k.bag[d] {
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
	yigin := []int32{0} // yigin[0] seçili kök; yokDugum = ağaçta olmayan dizin
	parcalar := strings.Split(rel, "/")
	for _, p := range parcalar[:len(parcalar)-1] {
		yigin = append(yigin, k.cocuk(yigin[len(yigin)-1], p))
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
		c := k.cocuk(yigin[len(yigin)-1], p)
		if c != yokDugum && k.bag[c] {
			return bagZincir
		}
		yigin = append(yigin, c)
	}
	return ""
}
