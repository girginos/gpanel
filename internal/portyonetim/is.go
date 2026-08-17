package portyonetim

// Async iş yönetimi — panelhost desenini birebir izler.
//
// Aynı anda YALNIZ BİR port değişikliği çalışabilir. Handler 409 döner.

import (
	"sync"
	"time"
)

type IsAdim struct {
	Etiket string    `json:"etiket"`
	OK     bool      `json:"ok"`
	Bilgi  string    `json:"bilgi,omitempty"`
	TS     time.Time `json:"ts"`
}

type Is struct {
	Tip       string    `json:"tip"` // "backend" | "dis"
	EskiPort  int       `json:"eski_port"`
	YeniPort  int       `json:"yeni_port"`
	BaslamaTS time.Time `json:"baslama_ts"`
	BitisTS   time.Time `json:"bitis_ts"`
	Basarili  bool      `json:"basarili"`
	Adimlar   []IsAdim  `json:"adimlar"`
	Rollback  bool      `json:"rollback"`
	SonHata   string    `json:"son_hata,omitempty"`
}

var (
	mu       sync.Mutex
	aktifIs  *Is
	tipKilit = map[string]bool{}
)

func TipKilitle(tip string) bool {
	mu.Lock()
	defer mu.Unlock()
	if tipKilit[tip] {
		return false
	}
	tipKilit[tip] = true
	return true
}
func TipSerbest(tip string) {
	mu.Lock()
	defer mu.Unlock()
	delete(tipKilit, tip)
}

func isBaslat(tip string, eskiPort, yeniPort int) *Is {
	mu.Lock()
	defer mu.Unlock()
	aktifIs = &Is{
		Tip:       tip,
		EskiPort:  eskiPort,
		YeniPort:  yeniPort,
		BaslamaTS: time.Now(),
		Adimlar:   []IsAdim{},
	}
	return aktifIs
}
func isAdim(i *Is, etiket string, ok bool, bilgi string) {
	mu.Lock()
	defer mu.Unlock()
	i.Adimlar = append(i.Adimlar, IsAdim{Etiket: etiket, OK: ok, Bilgi: bilgi, TS: time.Now()})
}
func isBitir(i *Is, basarili bool, sonHata string, rollback bool) {
	mu.Lock()
	defer mu.Unlock()
	i.BitisTS = time.Now()
	i.Basarili = basarili
	i.SonHata = sonHata
	i.Rollback = rollback
}

// isSnapshot — handler için data-race'siz deep copy.
func isSnapshot() *Is {
	mu.Lock()
	defer mu.Unlock()
	if aktifIs == nil {
		return nil
	}
	cp := *aktifIs
	cp.Adimlar = append([]IsAdim(nil), aktifIs.Adimlar...)
	return &cp
}

// isTrimAdimlar — poll dongusunde helper adimlarini yeniden yuklerken
// panel-tarafi ilk N adimi koru gerisini kes.
func isTrimAdimlar(i *Is, koru int) {
	mu.Lock()
	defer mu.Unlock()
	if koru < 0 || koru > len(i.Adimlar) {
		return
	}
	i.Adimlar = i.Adimlar[:koru]
}
