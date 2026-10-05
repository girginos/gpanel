package websec

// Tarama kuyruğu — HTTP uçları taramayı DOĞRUDAN başlatmaz (TaraDomains'i çağırmaz),
// buraya yazar; Dongusu'nun başlattığı TEK işçi sırayla işler (aynı anda tek tarama).
//
//   - Tam tarama isteği (zamanlayıcı, kökün domain_id'siz taraması) bir BAYRAKtır ve
//     işçi önce onu koşar. Zamanlayıcı her turda bayrağı koyar: kuyrukta bayi istekleri
//     olsa da, bir tarama sürse de periyodik tur ATLANMAZ (eskiden kilit meşgulse
//     tur atlanıyordu).
//   - Kısmi istekler bekleyen domain kümesidir (geliş sırası); işçi en çok partiBoyut
//     domainlik partiler alır.
//   - 409 yok: bir tarama sürerken gelen istek sıraya girer.
//
// Bayi sınırları (sabit; kökte sınır yok): domain başına domainSoguma (bayinin o
// domaini son kuyruğa alışından), bayi başına kuyrukta en çok bayiKuyrukSiniri domain,
// "Domainlerimi Tara" (domain_id'siz) bayi başına bayiTumuAralik'ta bir. Aşımda
// 429 + "N dk sonra tekrar deneyin"; süre yalnız bayinin kendi isteklerinden hesaplanır.
//
// 🔴 TaraDomains'te boş/nil liste TÜM SUNUCU demektir. Bayi isteği tam tarama
// bayrağını ASLA koyamaz (bayiEkle yalnız domain ekler) ve kısmi iş ASLA boş listeyle
// koşmaz (isCalistir).

import (
	"context"
	"database/sql"
	"log"
	"sync"
	"time"
)

const (
	partiBoyut       = 200
	domainSoguma     = 15 * time.Minute
	bayiKuyrukSiniri = 50
	bayiTumuAralik   = 30 * time.Minute
	// kuyrukDoluBekleme — bayinin kuyruk payı doluyken önerilen bekleme. Kuyruğun ne
	// zaman boşalacağı bilinmez (önünde tam tarama olabilir); kaba tahmindir.
	kuyrukDoluBekleme = 5 * time.Minute
)

// taramaIsi — işçinin tek işi: tam tarama ya da domain partisi.
type taramaIsi struct {
	tam bool
	ids []int64
}

type kuyruk struct {
	mu       sync.Mutex
	tam      bool                // tam tarama istendi
	sira     []int64             // bekleyen domainler, geliş sırası
	bekleyen map[int64]int64     // domain → kuyruğa alan bayi (0 = kök)
	sonIstek map[int64]bayiIstek // bayi soğuması: domain → son kuyruğa alış
	sonTumu  map[int64]time.Time // bayi → son "Domainlerimi Tara"
	tetik    chan struct{}       // işçiyi uyandırır (kapasite 1, bloklamaz)
}

// bayiIstek — domain soğuması KİMİN isteğine ait: soğuma yalnız aynı bayiye
// uygulanır. Devirden sonra yeni sahip, eski sahibin isteğinin süresini (429
// metninden) öğrenemez; kalan süre hep bayinin kendi verisidir.
type bayiIstek struct {
	rid int64
	t   time.Time
}

func yeniKuyruk() *kuyruk {
	return &kuyruk{
		bekleyen: map[int64]int64{},
		sonIstek: map[int64]bayiIstek{},
		sonTumu:  map[int64]time.Time{},
		tetik:    make(chan struct{}, 1),
	}
}

// kuy — süreç geneli tek kuyruk: handler'lar yazar, Dongusu'nun işçisi okur.
var kuy = yeniKuyruk()

// saat — şimdiki zaman (testler sınırları sınamak için değiştirir).
var saat = time.Now

// taraFn — işçinin tarama çağrısı (testler sahteler: gerçek tarama wp-cli/dosya
// okuma/dış feed koşar).
var taraFn = TaraDomains

func (k *kuyruk) tetikle() {
	select {
	case k.tetik <- struct{}{}:
	default:
	}
}

// tamIste — tam tarama bayrağı (zamanlayıcı, kök).
func (k *kuyruk) tamIste() {
	k.mu.Lock()
	k.tam = true
	k.mu.Unlock()
	k.tetikle()
}

// ekleKilitli — domaini (zaten beklemiyorsa) sıraya ekler; k.mu tutulurken çağrılır.
func (k *kuyruk) ekleKilitli(id, rid int64) {
	if _, zaten := k.bekleyen[id]; zaten {
		return
	}
	k.bekleyen[id] = rid
	k.sira = append(k.sira, id)
}

// kokEkle — kökün kısmi isteği; sınır yok.
func (k *kuyruk) kokEkle(ids []int64) {
	k.mu.Lock()
	for _, id := range ids {
		if id > 0 {
			k.ekleKilitli(id, 0)
		}
	}
	k.mu.Unlock()
	k.tetikle()
}

// bayiEkle — bayinin (sahipliği handler'da DOĞRULANMIŞ, tekil) domainlerini
// sınırlarla kuyruğa yazar. tumu: "Domainlerimi Tara".
//
// Dönüş (sirada, bekle): sirada = bu isteğin domainlerinden kuyrukta bekleyen sayısı
// (yeni eklenen + zaten sıradaki). bekle > 0 → hiçbiri sıraya giremedi: 429.
// Soğumadaki ya da kuyruk payını aşan domain atlanır; diğerleri sıraya girer.
func (k *kuyruk) bayiEkle(rid int64, ids []int64, tumu bool) (int, time.Duration) {
	sirada, _, bekle := k.bayiEkleSay(rid, ids, tumu)
	return sirada, bekle
}

// bayiEkleSay — bayiEkle + bu çağrıyla GERÇEKTEN yeni sıraya eklenen domain sayısı
// (yeni=0, sirada>0 → hepsi zaten sıradaydı: çağıran denetime ikinci satır yazmaz).
func (k *kuyruk) bayiEkleSay(rid int64, ids []int64, tumu bool) (sirada, yeni int, bekle time.Duration) {
	if rid <= 0 {
		return 0, 0, 0
	}
	simdi := saat()
	k.mu.Lock()
	defer k.mu.Unlock()
	k.budaKilitli(simdi)
	if t, ok := k.sonTumu[rid]; tumu && ok {
		if kalan := bayiTumuAralik - simdi.Sub(t); kalan > 0 {
			return 0, 0, kalan
		}
	}
	bayiBekleyen := 0
	for _, sahip := range k.bekleyen {
		if sahip == rid {
			bayiBekleyen++
		}
	}
	eklenen := 0
	var soguma time.Duration
	doldu := false
	for _, id := range ids {
		if _, zaten := k.bekleyen[id]; zaten {
			sirada++
			continue
		}
		if onceki, ok := k.sonIstek[id]; ok && onceki.rid == rid {
			if kalan := domainSoguma - simdi.Sub(onceki.t); kalan > 0 {
				if soguma == 0 || kalan < soguma {
					soguma = kalan
				}
				continue
			}
		}
		if bayiBekleyen >= bayiKuyrukSiniri {
			doldu = true
			continue
		}
		k.ekleKilitli(id, rid)
		k.sonIstek[id] = bayiIstek{rid: rid, t: simdi}
		bayiBekleyen++
		sirada++
		eklenen++
	}
	if eklenen > 0 {
		if tumu {
			k.sonTumu[rid] = simdi
		}
		k.tetikle()
	}
	switch {
	case sirada > 0:
		return sirada, eklenen, 0
	case doldu:
		return 0, 0, kuyrukDoluBekleme
	default:
		return 0, 0, soguma
	}
}

// budaKilitli — süresi geçmiş soğuma kayıtlarını atar (bellek sınırlı kalsın).
func (k *kuyruk) budaKilitli(simdi time.Time) {
	for id, onceki := range k.sonIstek {
		if simdi.Sub(onceki.t) >= domainSoguma {
			delete(k.sonIstek, id)
		}
	}
	for rid, t := range k.sonTumu {
		if simdi.Sub(t) >= bayiTumuAralik {
			delete(k.sonTumu, rid)
		}
	}
}

// bekleyenKume — sırada bekleyen domainlerin kopyası (Apps "sirada", bayi running).
func (k *kuyruk) bekleyenKume() map[int64]bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	kopya := make(map[int64]bool, len(k.bekleyen))
	for id := range k.bekleyen {
		kopya[id] = true
	}
	return kopya
}

// sonrakiIs — önce tam tarama (bekleyen kısmi işleri de kapsar, onlar düşer), yoksa
// en çok partiBoyut domainlik parti. İş yoksa ok=false.
func (k *kuyruk) sonrakiIs() (taramaIsi, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.tam {
		k.tam = false
		k.sira = nil
		k.bekleyen = map[int64]int64{}
		return taramaIsi{tam: true}, true
	}
	if len(k.sira) == 0 {
		return taramaIsi{}, false
	}
	n := min(len(k.sira), partiBoyut)
	ids := append([]int64(nil), k.sira[:n]...)
	k.sira = append([]int64(nil), k.sira[n:]...)
	for _, id := range ids {
		delete(k.bekleyen, id)
	}
	return taramaIsi{ids: ids}, true
}

// isci — TEK tarama işçisi (Dongusu başlatır). Uyandırılınca kuyruk boşalana dek işler.
func (k *kuyruk) isci(ctx context.Context, db *sql.DB) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-k.tetik:
		}
		for ctx.Err() == nil {
			is, ok := k.sonrakiIs()
			if !ok {
				break
			}
			isCalistir(ctx, db, is)
		}
	}
}

// isCalistir — tek işi koşar. Boş kısmi iş koşmaz: TaraDomains'e boş liste gitse
// TÜM SUNUCU taranırdı (bayi isteğinin tam taramaya dönüşmesi).
func isCalistir(ctx context.Context, db *sql.DB, is taramaIsi) {
	var ids []int64
	switch {
	case is.tam:
	case len(is.ids) > 0:
		ids = is.ids
	default:
		return
	}
	if err := taraFn(ctx, db, ids); err != nil {
		log.Printf("websec: tarama atlandı/başarısız (tam=%v, %d domain): %v", is.tam, len(ids), err)
	}
}
