package uzak

// izleyici.go — uzak kopya işçisinin CANLI ilerleme izleyicisi.
//
// İşçi ilerlemeyi bellekte tutar (kilit altında, DB'ye dokunmaz); arayüz ucu
// (GET /admin/backups/uzak-kopya/ilerleme) bunu + tek özet sorgusunu birleştirir.
// Hız ve ETA YALNIZ gerçek ölçümden gelir: örnek yoksa eta_sn=null ("hesaplanıyor").
//
// ADR (evre ağırlıkları): bir iş = şifreleme → yükleme → doğrulama (geri indirme).
// Yüzde için ağırlık: şifreleme %10, yükleme %45, doğrulama %45. Gerekçe: Drive gibi
// sunucu özeti vermeyen hedeflerde doğrulama yüklenen dosyanın TAMAMEN geri indirilmesidir
// (yükleme kadar bayt, genelde benzer ağ süresi); şifreleme yerel disk/CPU işidir ve
// ağdan çok daha hızlıdır. Sunucu özeti veren hedefte doğrulama indirme yapmaz →
// ağırlık 0'a düşer (10/55, 45/55). Ağırlıklar yalnız YÜZDE içindir; ETA ise evre
// başına ÖLÇÜLEN hızdan hesaplanır (kalan_bayt_evre / evre_hizi toplamı); hiç ölçülmemiş
// bir evre için ağırlık oranıyla ölçülmüş evrelerden türetilir.

import (
	"math"
	"sync"
	"time"
)

// İzlenen evreler (backup_uzak_kopyalar.durum değerleriyle aynı).
const (
	evreSifre = 0
	evreYukle = 1
	evreDogru = 2
)

var evreAdlari = [3]string{DurumSifreleniyor, DurumYukleniyor, DurumDogrulaniyor}

// Evre ağırlıkları (yüzde için).
var evreAgirlik = [3]float64{10, 45, 45}

// İzleyici ayarları.
const (
	ornekAralik      = time.Second      // hız örneği en sık bu aralıkla alınır
	emaAlfa          = 0.3              // üstel hareketli ortalama katsayısı
	etaMinSure       = 10 * time.Second // ölçüm başladıktan sonra ETA için en az süre
	etaMinOrnek      = 3                // ETA için en az toplam örnek
	hizBayatlik      = 15 * time.Second // bu kadar süredir ilerleme yoksa hız 0 gösterilir
	yeniOlcumBosluk  = 2 * time.Minute  // bu kadar boşta kalındıysa ölçüm sıfırlanır
	TamamlandiSuresi = 10 * time.Minute // biten turun özeti bu süre görünür
	DBYazmaAraligi   = 5 * time.Second  // yuklenen_b için en sık DB yazma
)

type evreOlcum struct {
	ema   float64 // bayt/sn
	ornek int
	sonT  time.Time
	sonB  int64
}

type aktifIs struct {
	isID, yedekID, domainID int64
	domain, dosya           string
	boyut                   int64 // düz yedek boyutu (iş büyüklüğü)
	evre                    int
	bayt, toplam            int64 // evre içi
	sonBildirim             time.Time
}

// Izleyici — işçinin bellek içi ilerleme durumu. Sıfır değeri kullanılabilir.
type Izleyici struct {
	mu        sync.Mutex
	saat      func() time.Time
	calisiyor bool
	akt       *aktifIs
	olc       [3]evreOlcum
	olcumBasi time.Time
	sonBitis  time.Time
	indirmeli bool // doğrulama geri indirme yapar mı (varsayılan evet)
	indirSet  bool
	sonYazma  time.Time

	// tur (kuyruk özeti kapsamı)
	turBas   string    // veritabanı saatiyle "YYYY-MM-DD HH:MM:SS"; boş = tur yok
	turBitis time.Time // tur boşa çıktığı an (sıfır = sürüyor)
}

// SaatKur — test için sahte saat.
func (z *Izleyici) SaatKur(f func() time.Time) {
	z.mu.Lock()
	z.saat = f
	z.mu.Unlock()
}

func (z *Izleyici) simdi() time.Time {
	if z.saat != nil {
		return z.saat()
	}
	return time.Now()
}

// Calisiyor — işçi döngüsü canlı mı (Calistir başlar/biter).
func (z *Izleyici) Calisiyor(v bool) {
	z.mu.Lock()
	z.calisiyor = v
	z.mu.Unlock()
}

// DogrulamaIndirir — hedef doğrulamada dosyayı geri indiriyor mu (sunucu özeti yoksa evet).
func (z *Izleyici) DogrulamaIndirir(v bool) {
	z.mu.Lock()
	z.indirmeli, z.indirSet = v, true
	z.mu.Unlock()
}

func (z *Izleyici) agirliklar() [3]float64 {
	w := evreAgirlik
	if z.indirSet && !z.indirmeli {
		w[evreDogru] = 0
	}
	t := w[0] + w[1] + w[2]
	for i := range w {
		w[i] = w[i] / t
	}
	return w
}

// Baslat — işçi bir işi aldı (şifreleme evresi). Uzun süre boşta kalındıysa ölçüm sıfırlanır.
func (z *Izleyici) Baslat(isID, yedekID, domainID int64, domain, dosya string) {
	z.mu.Lock()
	defer z.mu.Unlock()
	now := z.simdi()
	if z.olcumBasi.IsZero() || (z.akt == nil && !z.sonBitis.IsZero() && now.Sub(z.sonBitis) > yeniOlcumBosluk) {
		z.olc = [3]evreOlcum{}
		z.olcumBasi = now
	}
	z.akt = &aktifIs{isID: isID, yedekID: yedekID, domainID: domainID, domain: domain, dosya: dosya, evre: evreSifre, sonBildirim: now}
	z.olc[evreSifre].sonT, z.olc[evreSifre].sonB = now, 0
	z.sonYazma = time.Time{}
}

// Boyut — işin düz boyutu ve şifreleme evresinin toplamı.
func (z *Izleyici) Boyut(n int64) {
	z.mu.Lock()
	if z.akt != nil {
		z.akt.boyut = n
		if z.akt.evre == evreSifre {
			z.akt.toplam = n
		}
	}
	z.mu.Unlock()
}

// EvreGec — yeni evre (sifreleniyor/yukleniyor/dogrulaniyor) ve evrenin toplam baytı.
func (z *Izleyici) EvreGec(durum string, toplam int64) {
	z.mu.Lock()
	defer z.mu.Unlock()
	if z.akt == nil {
		return
	}
	for i, ad := range evreAdlari {
		if ad == durum {
			now := z.simdi()
			z.akt.evre, z.akt.bayt, z.akt.toplam, z.akt.sonBildirim = i, 0, toplam, now
			z.olc[i].sonT, z.olc[i].sonB = now, 0
			return
		}
	}
}

// Bildir — o ana dek aktarılan evre baytı. Ucuzdur (bellek); en çok ornekAralik'ta bir örnek alır.
func (z *Izleyici) Bildir(bayt int64) {
	z.mu.Lock()
	defer z.mu.Unlock()
	a := z.akt
	if a == nil {
		return
	}
	now := z.simdi()
	a.bayt, a.sonBildirim = bayt, now
	o := &z.olc[a.evre]
	if o.sonT.IsZero() {
		o.sonT, o.sonB = now, 0
	}
	dt := now.Sub(o.sonT)
	if dt < ornekAralik {
		return
	}
	rate := float64(bayt-o.sonB) / dt.Seconds()
	if rate < 0 {
		rate = 0
	}
	if o.ornek == 0 {
		o.ema = rate
	} else {
		o.ema = emaAlfa*rate + (1-emaAlfa)*o.ema
	}
	o.ornek++
	o.sonT, o.sonB = now, bayt
}

// Bitir — iş bitti (başarı, hata ya da iptal).
func (z *Izleyici) Bitir() {
	z.mu.Lock()
	z.akt = nil
	z.sonBitis = z.simdi()
	z.mu.Unlock()
}

// YazmaIzni — yuklenen_b'nin DB'ye yazılması için sınırlayıcı: en sık DBYazmaAraligi'nda bir true.
func (z *Izleyici) YazmaIzni() bool {
	z.mu.Lock()
	defer z.mu.Unlock()
	now := z.simdi()
	if !z.sonYazma.IsZero() && now.Sub(z.sonYazma) < DBYazmaAraligi {
		return false
	}
	z.sonYazma = now
	return true
}

// ─── çıktı ──────────────────────────────────────────────────────────────────

// AktifIsOzet — şu an işlenen iş (sırsız; yalnız alan adı + dosya adı).
type AktifIsOzet struct {
	IsID           int64   `json:"is_id"`
	YedekID        int64   `json:"yedek_id"`
	Domain         string  `json:"domain"`
	Dosya          string  `json:"dosya"`
	Evre           string  `json:"evre"`
	EvreYuzde      float64 `json:"evre_yuzde"`
	IsYuzde        float64 `json:"is_yuzde"`
	BaytGonderilen int64   `json:"bayt_gonderilen"`
	BaytToplam     int64   `json:"bayt_toplam"`
	HizBps         float64 `json:"hiz_bps"`
}

// KuyrukOzet — tur (art arda kuyruğa alınan işler) toplamı.
type KuyrukOzet struct {
	ToplamIs         int     `json:"toplam_is"`
	Tamam            int     `json:"tamam"`
	Hata             int     `json:"hata"`
	Bekleyen         int     `json:"bekleyen"`
	YenidenDenenecek int     `json:"yeniden_denenecek"`
	ToplamBayt       int64   `json:"toplam_bayt"`
	TamamlananBayt   int64   `json:"tamamlanan_bayt"`
	KalanBayt        int64   `json:"kalan_bayt"`
	Yuzde            float64 `json:"yuzde"`
	HizBps           float64 `json:"hiz_bps"`
	EtaSn            *int64  `json:"eta_sn"`
}

// Ilerleme — uç yanıtı.
type Ilerleme struct {
	Aktif      *AktifIsOzet `json:"aktif"`
	Kuyruk     KuyrukOzet   `json:"kuyruk"`
	AktifMi    bool         `json:"aktif_mi"`
	Tamamlandi bool         `json:"tamamlandi"`
}

// Sayim — veritabanından tek sorguyla gelen tur toplamları.
type Sayim struct {
	ToplamIs, Tamam, Hata, Hazir, Yeniden, Kosan int
	ToplamB, TamamB, HazirB                      int64
}

func yuvarla1(x float64) float64 { return math.Round(x*10) / 10 }

func oran(a, t int64) float64 {
	if t <= 0 {
		return 1
	}
	v := float64(a) / float64(t)
	if v > 1 {
		return 1
	}
	if v < 0 {
		return 0
	}
	return v
}

// hesapla — SAF hesap: tracker durumu + DB sayımı → yanıt. z.mu tutulurken çağrılır.
func (z *Izleyici) hesapla(s Sayim, ayarAktif bool, tamamlandi bool) Ilerleme {
	now := z.simdi()
	w := z.agirliklar()
	out := Ilerleme{}
	k := &out.Kuyruk
	k.ToplamIs, k.Tamam, k.Hata, k.Bekleyen, k.YenidenDenenecek = s.ToplamIs, s.Tamam, s.Hata, s.Hazir, s.Yeniden
	k.ToplamBayt = s.ToplamB
	out.Tamamlandi = tamamlandi
	out.AktifMi = z.calisiyor && ayarAktif && (s.Hazir > 0 || s.Kosan > 0 || z.akt != nil)

	var aktifBayt float64
	var hizAktif float64
	if a := z.akt; a != nil {
		isY := 0.0
		for i := 0; i < a.evre; i++ {
			isY += w[i]
		}
		fr := oran(a.bayt, a.toplam)
		if a.toplam <= 0 && a.evre == evreSifre && a.boyut > 0 {
			fr = 0
		}
		isY += w[a.evre] * fr
		o := z.olc[a.evre]
		if o.ornek > 0 && now.Sub(a.sonBildirim) <= hizBayatlik {
			hizAktif = o.ema
		}
		out.Aktif = &AktifIsOzet{IsID: a.isID, YedekID: a.yedekID, Domain: a.domain, Dosya: a.dosya, Evre: evreAdlari[a.evre],
			EvreYuzde: yuvarla1(fr * 100), IsYuzde: yuvarla1(isY * 100), BaytGonderilen: a.bayt, BaytToplam: a.toplam, HizBps: hizAktif}
		aktifBayt = isY * float64(a.boyut)
	}
	k.HizBps = hizAktif
	k.TamamlananBayt = s.TamamB + int64(aktifBayt)
	if k.TamamlananBayt > k.ToplamBayt {
		k.TamamlananBayt = k.ToplamBayt
	}
	k.KalanBayt = k.ToplamBayt - k.TamamlananBayt
	switch {
	case k.ToplamBayt > 0:
		k.Yuzde = yuvarla1(float64(k.TamamlananBayt) / float64(k.ToplamBayt) * 100)
	case k.ToplamIs > 0:
		k.Yuzde = yuvarla1(float64(k.Tamam+k.Hata) / float64(k.ToplamIs) * 100)
	}
	if tamamlandi {
		k.Yuzde, k.KalanBayt, k.TamamlananBayt = 100, 0, k.ToplamBayt
		return out
	}
	k.EtaSn = z.eta(s, w, now)
	return out
}

// eta — evre başına ölçülen hızdan kalan süre (sn); yeterli ölçüm yoksa nil.
func (z *Izleyici) eta(s Sayim, w [3]float64, now time.Time) *int64 {
	toplamOrnek := 0
	for _, o := range z.olc {
		toplamOrnek += o.ornek
	}
	if toplamOrnek < etaMinOrnek || z.olcumBasi.IsZero() || now.Sub(z.olcumBasi) < etaMinSure {
		return nil
	}
	// kalan bayt (evre başına)
	var rem [3]float64
	for i := range rem {
		rem[i] = float64(s.HazirB)
	}
	if a := z.akt; a != nil {
		for i := range rem {
			switch {
			case i < a.evre:
			case i == a.evre:
				rem[i] += math.Max(0, float64(a.toplam-a.bayt))
			default:
				rem[i] += float64(a.boyut)
			}
		}
	}
	// evre başına bayt başına süre (sn/bayt): ölçülmüşse 1/ema, değilse ağırlık oranıyla türet
	var spb [3]float64
	var ok [3]bool
	for i, o := range z.olc {
		if o.ornek > 0 && o.ema > 0 {
			spb[i], ok[i] = 1/o.ema, true
		}
	}
	for i := range spb {
		if ok[i] || w[i] == 0 {
			continue
		}
		var top float64
		var n int
		for j := range spb {
			if ok[j] && w[j] > 0 {
				top += spb[j] * w[i] / w[j]
				n++
			}
		}
		if n == 0 {
			return nil
		}
		spb[i] = top / float64(n)
	}
	var sure float64
	for i := range rem {
		if w[i] == 0 {
			continue
		}
		sure += rem[i] * spb[i]
	}
	v := int64(math.Ceil(sure))
	return &v
}
