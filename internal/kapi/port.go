package kapi

// Panel portunu okuyan diğer paketler (portyonetim, panelhost, guvenlikduvari) için
// dosya düzeyi yardımcılar. Karar mantığı nginx.go'daki saf fonksiyonlardadır.

import (
	"errors"
	"os"
	"sort"
)

// GenelPanelPortuOku: GenelPanelPortu'nun dosya okuyan hâli (vhost + kapi.json).
// kapi.json VAR ama okunamıyorsa 0 döner: rolleri bilmeden tahmin etmek admin portunu
// sızdırabilirdi; çağıran kendi varsayılanına düşer.
func GenelPanelPortuOku(vhostYolu string) int {
	return genelPanelPortuOku(vhostYolu, AyarYolu)
}

func genelPanelPortuOku(vhostYolu, ayarYolu string) int {
	b, err := os.ReadFile(vhostYolu)
	if err != nil {
		return 0
	}
	a, err := Oku(ayarYolu)
	if err != nil {
		if !errors.Is(err, ErrYok) {
			return 0
		}
		a = nil
	}
	return GenelPanelPortu(string(b), a)
}

// PanelPortlari: panelin korunması gereken tüm dış portları (artan, tekrarsız): vhost'un
// genel listen'ları + kapı ayarındaki rol portları + açık eski portlar. Güvenlik duvarı
// bunlara "kapat" kuralı yazdırmaz (panel kendini kilitlemesin).
func PanelPortlari(vhostYolu string, simdi int64) []int {
	return panelPortlari(vhostYolu, AyarYolu, simdi)
}

func panelPortlari(vhostYolu, ayarYolu string, simdi int64) []int {
	set := map[int]bool{}
	if b, err := os.ReadFile(vhostYolu); err == nil {
		for _, p := range GenelPortlar(string(b)) {
			set[p] = true
		}
	}
	if a, err := Oku(ayarYolu); err == nil {
		for p := range a.PortHaritasi(simdi) {
			set[p] = true
		}
	}
	out := make([]int, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}
