package panelhost

// Async iş yönetimi — panel hostname değişimi ve SSL kurma dakikalarca
// sürebilir (DNS TTL, LE issue akışı). HTTP handler bloklanamaz; iş bir
// goroutine'de yürür, front-end job ID ile durum sorgular.
//
// 🔴 Bellek-içi tutuluyor — panel restart olursa iş kaybolur ve kullanıcı
// bunu görür ("iş kayboldu, yeniden başlat"). Persistent job için DB tablosu
// gerekir (sonraki iterasyon). Bugünlük yeter: iş 5-30 sn arası.

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

type Adim struct {
	Zaman   time.Time `json:"zaman"`
	Mesaj   string    `json:"mesaj"`
	Basari  bool      `json:"basari"`
}

type Is struct {
	ID       string    `json:"id"`
	Tip      string    `json:"tip"` // "ayarla" | "sslkur"
	Hostname string    `json:"hostname"`
	Durum    string    `json:"durum"` // "kosuyor" | "bitti" | "hata"
	Basla    time.Time `json:"basla"`
	Bitis    time.Time `json:"bitis"`
	Adimlar  []Adim    `json:"adimlar"`
	Hata     string    `json:"hata"`

	mu sync.Mutex
}

var (
	isMu      sync.RWMutex
	isKayit   = map[string]*Is{}
	sonTemiz  time.Time

	// 🔴 Kritik: aynı tip iş (ayarla/sslkur) için EŞZAMANLI ÇALIŞMA YASAK.
	// İki paralel `girginospanel-panelhost ayarla` bash süreci aynı vhost
	// dosyasına sed uygular, farklı yedekler bırakır, ikisi de reload dener,
	// `sifirla` son yedeğe döner (bozuk olabilir).
	//
	// Tip başına tek-kilit yeterli: bir kullanıcı hostname değiştirirken
	// başka biri SSL kuramaz, çünkü ikinci işlemin ön-koşulu (vhost'ta
	// hostname var mı) tutarsız olur.
	isTipKilit = map[string]bool{} // tip → çalışıyor mu
	isTipMu    sync.Mutex
)

// TipKilitle — bu tip iş şu an çalışıyorsa false, aksi halde kilidi al ve true.
// Serbest bırakma: TipSerbest çağır.
func TipKilitle(tip string) bool {
	isTipMu.Lock()
	defer isTipMu.Unlock()
	if isTipKilit[tip] {
		return false
	}
	isTipKilit[tip] = true
	return true
}

func TipSerbest(tip string) {
	isTipMu.Lock()
	delete(isTipKilit, tip)
	isTipMu.Unlock()
}

func yeniIsID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// IsBaslat — yeni bir iş oluştur, kaydet, döndür (henüz kosuyor).
func IsBaslat(tip, hostname string) *Is {
	temizle()
	is := &Is{
		ID:       yeniIsID(),
		Tip:      tip,
		Hostname: hostname,
		Durum:    "kosuyor",
		Basla:    time.Now(),
		Adimlar:  []Adim{},
	}
	isMu.Lock()
	isKayit[is.ID] = is
	isMu.Unlock()
	IsPersistYaz(is) // DB set edilmişse INSERT, yoksa NO-OP
	return is
}

// IsGetir — İş kaydının SNAPSHOT'ını döner (data race güvenli).
//
// 🔴 KRİTİK: pointer'ı doğrudan dönmek → handler JSON serialize ederken
// goroutine `AdimEkle` ile slice'a append yapar → -race panic veya çöp okuma.
// Snapshot: alanları kilit altında kopyala, slice'ı deep-copy et.
func IsGetir(id string) *Is {
	isMu.RLock()
	orig := isKayit[id]
	isMu.RUnlock()
	if orig == nil {
		return nil
	}
	orig.mu.Lock()
	defer orig.mu.Unlock()
	kopya := &Is{
		ID:       orig.ID,
		Tip:      orig.Tip,
		Hostname: orig.Hostname,
		Durum:    orig.Durum,
		Basla:    orig.Basla,
		Bitis:    orig.Bitis,
		Hata:     orig.Hata,
	}
	if len(orig.Adimlar) > 0 {
		kopya.Adimlar = make([]Adim, len(orig.Adimlar))
		copy(kopya.Adimlar, orig.Adimlar)
	} else {
		kopya.Adimlar = []Adim{}
	}
	return kopya
}

// AdimEkle — kilit-güvenli adım kaydı.
func (i *Is) AdimEkle(mesaj string, basari bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.Adimlar = append(i.Adimlar, Adim{Zaman: time.Now(), Mesaj: mesaj, Basari: basari})
}

func (i *Is) Bitir(hata string) {
	i.mu.Lock()
	i.Bitis = time.Now()
	if hata != "" {
		i.Durum = "hata"
		i.Hata = hata
	} else {
		i.Durum = "bitti"
	}
	i.mu.Unlock()
	IsPersistGuncelle(i) // DB set edilmişse UPDATE
}

// temizle — 1 saatten eski işleri düşür (bellek büyümesin).
func temizle() {
	now := time.Now()
	if now.Sub(sonTemiz) < 5*time.Minute {
		return
	}
	isMu.Lock()
	defer isMu.Unlock()
	sonTemiz = now
	for id, is := range isKayit {
		if now.Sub(is.Basla) > time.Hour {
			delete(isKayit, id)
		}
	}
}

// TemizleLoop — panel açılışta bir kez `go panelhost.TemizleLoop()` çağrılır.
// IsBaslat tetiği yeterli ama determinism için 5dk periodic ticker.
func TemizleLoop() {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for range t.C {
		temizle()
	}
}
