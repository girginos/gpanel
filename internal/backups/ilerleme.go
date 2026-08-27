// Yedek/geri-yukleme ilerleme takibi — "ne oluyor" gorunurlugu.
//
// 🔴 Neden: tek-domain yedek ve geri yukleme uzun surer (5 GB'lik bir tenant'ta
// dakikalar). Musteri o sure boyunca yalniz "Yedekleniyor…" yazisi goruyordu;
// hangi asamada oldugu, ilerleyip ilerlemedigi belli degildi. Daha kotusu, uzak
// hedefteki bir yedegi geri yuklerken istek "indirme basladi, tekrar deneyin"
// donuyordu — kullanici bir kez tikliyor, indirme arkada bitiyor ama GERI
// YUKLEME HIC BASLAMIYORDU (uretimde yasandi: kullanici "yine olmadi" dedi).
//
// Cozum: uzun islemler arka plan gorevine alinir, bu kayit da asama + yuzde +
// SONUC tasir. Musteri sayfasi tek uctan hepsini gosterir.
//
// Kayit bellekte ve DOMAIN BASINA tektir (zaten tek-ucus kilidi var). Is bitince
// hemen silinmez: istemcinin sonucu okuyabilmesi icin kisa sure tutulur.
package backups

import (
	"database/sql"
	"os"
	"sync"
	"time"
)

// sonucSaklamaSuresi: is bittikten sonra kaydin ne kadar okunabilir kalacagi.
// Istemci 1.5-2 sn'de bir yoklar; 2 dakika fazlasiyla yeterli, sayfa yenilense
// bile son sonuc gorulur.
const sonucSaklamaSuresi = 2 * time.Minute

// Ilerleme: bir domainde suren (veya yeni bitmis) yedek/geri-yukleme durumu.
type Ilerleme struct {
	Aktif   bool   `json:"aktif"`   // kayit var mi (suren VEYA yeni bitmis)
	Bitti   bool   `json:"bitti"`   // is tamamlandi mi
	Islem   string `json:"islem"`   // "yedek" | "geri"
	Asama   string `json:"asama"`   // kullaniciya gosterilecek asama metni
	Yapilan int64  `json:"yapilan"` // yazilan/indirilen bayt
	Toplam  int64  `json:"toplam"`  // beklenen toplam (0 = bilinmiyor)
	Yuzde   int    `json:"yuzde"`   // 0-99 (toplam bilinmiyorsa 0)
	GecenSn int    `json:"gecen_sn"`
	Sonuc   string `json:"sonuc,omitempty"` // basarili bitis mesaji
	Hata    string `json:"hata,omitempty"`  // basarisiz bitis mesaji
}

type ilerlemeKayit struct {
	mu        sync.Mutex
	islem     string
	asama     string
	yapilan   int64
	toplam    int64
	baslangic time.Time
	bitti     bool
	bitisAn   time.Time
	sonuc     string
	hata      string
	dur       chan struct{}
}

var ilerlemeler sync.Map // domainID -> *ilerlemeKayit

func kayitAl(domainID int64) *ilerlemeKayit {
	if v, ok := ilerlemeler.Load(domainID); ok {
		if k, tamam := v.(*ilerlemeKayit); tamam {
			return k
		}
	}
	return nil
}

// IlerlemeAktifMi: bu domainde SUREN (bitmemis) bir is var mi.
func IlerlemeAktifMi(domainID int64) bool {
	k := kayitAl(domainID)
	if k == nil {
		return false
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	return !k.bitti
}

// IlerlemeBaslat: yeni bir is icin takibi baslatir.
// islem: "yedek" | "geri". toplam=0 ise yuzde gosterilmez.
func IlerlemeBaslat(domainID int64, islem, asama string, toplam int64) {
	IlerlemeDosyaDur(domainID)
	ilerlemeler.Store(domainID, &ilerlemeKayit{
		islem:     islem,
		asama:     asama,
		toplam:    toplam,
		baslangic: time.Now(),
	})
}

// IlerlemeAsama: asama metnini gunceller ve sayaci sifirlar (her asama kendi
// ilerlemesini olcer). toplam=0 gecilirse onceki toplam korunur.
func IlerlemeAsama(domainID int64, asama string, toplam int64) {
	IlerlemeDosyaDur(domainID)
	if k := kayitAl(domainID); k != nil {
		k.mu.Lock()
		k.asama = asama
		k.yapilan = 0
		k.toplam = toplam
		k.mu.Unlock()
	}
}

// IlerlemeDosyaIzle: bir dosyanin buyumesini saniyede bir ornekleyerek
// "yapilan"i gunceller. tar/gzip/lftp harici surecler oldugu icin ilerlemeyi
// baska turlu olcemiyoruz; hedef dosyanin boyutu en dogrudan gostergedir.
func IlerlemeDosyaIzle(domainID int64, yol string) {
	k := kayitAl(domainID)
	if k == nil {
		return
	}
	k.mu.Lock()
	if k.dur != nil {
		close(k.dur)
	}
	dur := make(chan struct{})
	k.dur = dur
	k.mu.Unlock()

	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-dur:
				return
			case <-t.C:
				if fi, err := os.Stat(yol); err == nil {
					k.mu.Lock()
					k.yapilan = fi.Size()
					k.mu.Unlock()
				}
			}
		}
	}()
}

// IlerlemeDosyaDur: dosya ornekleyicisini durdurur.
func IlerlemeDosyaDur(domainID int64) {
	if k := kayitAl(domainID); k != nil {
		k.mu.Lock()
		if k.dur != nil {
			close(k.dur)
			k.dur = nil
		}
		k.mu.Unlock()
	}
}

// IlerlemeBitir: isi sonlandirir ve SONUCU kayitta birakir; kayit
// sonucSaklamaSuresi kadar okunabilir kalir, sonra kendi kendine silinir.
func IlerlemeBitir(domainID int64, sonuc string, hata error) {
	IlerlemeDosyaDur(domainID)
	k := kayitAl(domainID)
	if k == nil {
		return
	}
	k.mu.Lock()
	k.bitti = true
	k.bitisAn = time.Now()
	k.asama = "tamamlandı"
	if hata != nil {
		k.hata = hata.Error()
		k.asama = "başarısız"
	} else {
		k.sonuc = sonuc
	}
	k.mu.Unlock()

	go func() {
		time.Sleep(sonucSaklamaSuresi)
		if kk := kayitAl(domainID); kk != nil {
			kk.mu.Lock()
			eski := kk.bitti && time.Since(kk.bitisAn) >= sonucSaklamaSuresi
			kk.mu.Unlock()
			if eski {
				ilerlemeler.Delete(domainID)
			}
		}
	}()
}

// IlerlemeOku: anlik durum. Aktif=false ise kayit yok.
func IlerlemeOku(domainID int64) Ilerleme {
	k := kayitAl(domainID)
	if k == nil {
		return Ilerleme{}
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	i := Ilerleme{
		Aktif:   true,
		Bitti:   k.bitti,
		Islem:   k.islem,
		Asama:   k.asama,
		Yapilan: k.yapilan,
		Toplam:  k.toplam,
		GecenSn: int(time.Since(k.baslangic).Seconds()),
		Sonuc:   k.sonuc,
		Hata:    k.hata,
	}
	if k.bitti {
		i.Yuzde = 100
		return i
	}
	// 🔴 Yuzde %99'da tutulur: arsivleme bitse bile dogrulama/checksum/yukleme
	// asamalari kalir. Erken "%100" yanlis biterlik hissi verir ve kullanici
	// islem surerken sayfayi kapatir.
	if k.toplam > 0 && k.yapilan > 0 {
		p := int(k.yapilan * 100 / k.toplam)
		if p > 99 {
			p = 99
		}
		i.Yuzde = p
	}
	return i
}

// oncekiYedekBoyutu: bu domainin en son yedeginin boyutu — yedek alirken yuzde
// TAHMINI icin. Yedek boyutlari gunden gune az degistigi icin iyi bir tahmindir
// (buyuk bir silme/ekleme sonrasi ilk yedekte sasar, sonra oturur).
func oncekiYedekBoyutu(db *sql.DB, domainID int64) int64 {
	var b int64
	_ = db.QueryRow(`SELECT boyut_b FROM backups WHERE domain_id=? AND boyut_b>0 ORDER BY id DESC LIMIT 1`,
		domainID).Scan(&b)
	return b
}
