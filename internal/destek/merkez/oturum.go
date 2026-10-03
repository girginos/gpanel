package merkez

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strconv"
	"sync"
	"time"

	"girginospanel/internal/destek/protokol"
)

var (
	ErrMesgul    = errors.New("sunucu eşzamanlı oturum sınırında")
	ErrIsleniyor = errors.New("önceki mesaj işleniyor")
)

// Oturum: bir Telegram kullanıcısının bir bağlantı üzerindeki Claude konuşması.
// Kapsam (Baglam) oturum başında bağlantı kaydından ANLIK alınır; bağlantı
// güncellenirse oturum kapatılır (yeni kapsam yeni oturumla gelir).
type Oturum struct {
	ID         string
	TgUserID   int64
	BaglantiID string
	Sunucu     string
	Baglam     protokol.OturumBaglami
	Baslangic  time.Time

	mesajlar     []apiMesaj
	aracSayisi   int
	tokenToplam  int64 // ücretli eşdeğer (ucretliEsdeger)
	sonIstem     int64 // son isteğin toplam istem tokenı (giriş + cache yazma + cache okuma)
	sikistirilan int   // bu indekse kadarki mesajların araç sonuçları sıkıştırıldı

	// yönetici kilidi altında
	son     time.Time
	isliyor bool
	kapali  bool
}

func (o *Oturum) anahtar() string { return oturumAnahtari(o.TgUserID, o.BaglantiID) }

func oturumAnahtari(tg int64, baglanti string) string {
	return strconv.FormatInt(tg, 10) + "|" + baglanti
}

func yeniKimlik() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// OturumYoneticisi: sunucu başına eşzamanlı oturum sınırı + oturum başına tek işlem.
type OturumYoneticisi struct {
	mu        sync.Mutex
	sinir     int
	bosta     time.Duration
	saat      func() time.Time
	oturumlar map[string]*Oturum
}

func yeniOturumYoneticisi(sinir int, bosta time.Duration, saat func() time.Time) *OturumYoneticisi {
	return &OturumYoneticisi{sinir: sinir, bosta: bosta, saat: saat, oturumlar: map[string]*Oturum{}}
}

func (y *OturumYoneticisi) etkin(o *Oturum, simdi time.Time) bool {
	return !o.kapali && (o.isliyor || simdi.Sub(o.son) < y.bosta)
}

// Al: oturumu işleme alır (isliyor). Yoksa açar — sunucunun etkin oturumu
// sınırdaysa ErrMesgul. Aynı oturum işleniyorsa ErrIsleniyor.
func (y *OturumYoneticisi) Al(b Baglanti, dil string) (*Oturum, error) {
	y.mu.Lock()
	defer y.mu.Unlock()
	simdi := y.saat()
	for a, o := range y.oturumlar {
		if !y.etkin(o, simdi) {
			delete(y.oturumlar, a)
		}
	}
	a := oturumAnahtari(b.TgUserID, b.BaglantiID)
	if o, ok := y.oturumlar[a]; ok {
		if o.isliyor {
			return nil, ErrIsleniyor
		}
		o.isliyor = true
		o.son = simdi
		return o, nil
	}
	n := 0
	for _, o := range y.oturumlar {
		if o.Sunucu == b.SunucuKimligi {
			n++
		}
	}
	if n >= y.sinir {
		return nil, ErrMesgul
	}
	o := &Oturum{
		ID: yeniKimlik(), TgUserID: b.TgUserID, BaglantiID: b.BaglantiID, Sunucu: b.SunucuKimligi,
		Baglam:    protokol.OturumBaglami{Rol: b.Rol, Duzey: b.Duzey, Hesaplar: append([]protokol.HesapEtiketi(nil), b.Hesaplar...), Dil: dil},
		Baslangic: simdi, son: simdi, isliyor: true,
	}
	y.oturumlar[a] = o
	return o, nil
}

// Birak: işlem bitti; bitir=true → oturum silinir (ret, hata, devir sonrası temiz başlangıç).
func (y *OturumYoneticisi) Birak(o *Oturum, bitir bool) {
	y.mu.Lock()
	defer y.mu.Unlock()
	o.isliyor = false
	o.son = y.saat()
	if bitir || o.kapali {
		if y.oturumlar[o.anahtar()] == o {
			delete(y.oturumlar, o.anahtar())
		}
	}
}

// KapatBaglanti: bağlantı değişti/iptal → o bağlantının oturumları kapanır.
func (y *OturumYoneticisi) KapatBaglanti(baglanti string) {
	y.mu.Lock()
	defer y.mu.Unlock()
	for a, o := range y.oturumlar {
		if o.BaglantiID == baglanti {
			o.kapali = true
			if !o.isliyor {
				delete(y.oturumlar, a)
			}
		}
	}
}

// Kapali: oturum işlenirken bağlantısı değişti/iptal edildi mi (KapatBaglanti/KapatKullanici).
// Kapalı oturumun Baglam'ı BAYATTIR: modelin "hesap N"i artık başka hesabı gösterebilir.
func (y *OturumYoneticisi) Kapali(o *Oturum) bool {
	y.mu.Lock()
	defer y.mu.Unlock()
	return o.kapali
}

// KapatKullanici: /unutbeni.
func (y *OturumYoneticisi) KapatKullanici(tg int64) {
	y.mu.Lock()
	defer y.mu.Unlock()
	for a, o := range y.oturumlar {
		if o.TgUserID == tg {
			o.kapali = true
			if !o.isliyor {
				delete(y.oturumlar, a)
			}
		}
	}
}

// Sayi: etkin oturum sayısı (metrik).
func (y *OturumYoneticisi) Sayi() int {
	y.mu.Lock()
	defer y.mu.Unlock()
	simdi := y.saat()
	n := 0
	for _, o := range y.oturumlar {
		if y.etkin(o, simdi) {
			n++
		}
	}
	return n
}
