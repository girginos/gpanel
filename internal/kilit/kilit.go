// Package kilit: bayi-bazli surec-ici kilit.
//
// 🔴 NEDEN: bir bayinin hosting'lerini etkileyen islemler (yeni hosting, plan
// degisimi, aski zinciri, silme) BIRBIRIYLE yarisiyordu:
//   - aski sirasinda acilan hosting ASKIDAN MUAF canli doguyordu,
//   - iki paralel plan yukseltmesi ayni eski toplami okuyup kotayi asiyordu,
//   - silme ile aski zinciri carpisip YETIM nginx vhost birakiyordu.
//
// Panel tek-node oldugu icin surec-ici kilit yeterli; kilit BAYI BAZINDA,
// farkli bayiler birbirini beklemez.
package kilit

import "sync"

var (
	mu       sync.Mutex
	kilitler = map[int64]*sync.Mutex{}
	hesaplar = map[string]*sync.Mutex{}
	dbler    = map[int64]*sync.Mutex{}
)

// Hesap: verilen hosting hesabi (sistem kullanicisi) icin paylasilan kilit.
// Hesap askisi (hesapaski) adimlarini hesap basina serilestirir: ayni hesaba
// eszamanli askiya alma / askidan alma / acilis supurmesi ic ice gecip DB ile
// gercek durumu ayristirmasin. Bayi kilidiyle birlikte alinacaksa SIRA: once
// Bayi, sonra Hesap.
func Hesap(sk string) *sync.Mutex {
	mu.Lock()
	defer mu.Unlock()
	k, ok := hesaplar[sk]
	if !ok {
		k = &sync.Mutex{}
		hesaplar[sk] = k
	}
	return k
}

// Bayi: verilen bayi icin paylasilan kilit. rid<=0 (kok) icin de tek bir kilit
// doner — kok islemleri de kendi arasinda sirali olur.
func Bayi(rid int64) *sync.Mutex {
	mu.Lock()
	defer mu.Unlock()
	k, ok := kilitler[rid]
	if !ok {
		k = &sync.Mutex{}
		kilitler[rid] = k
	}
	return k
}

// DBOlustur: hosting (domain) başına "DB sayısı kapısı + DB oluşturma" kilidi.
// kota.CheckDBEklenebilir (COUNT) ile MariaDB/db_accounts yazımı arasındaki TOCTOU
// yarışını kapatır: eşzamanlı N istek aynı COUNT'u okuyup hepsi geçerek max_db sınırını
// aşıyordu. Panel tek-node olduğundan süreç-içi kilit yeterlidir; kilit domain BAZINDA
// (farklı hostingler birbirini beklemez). Askı/silme zincirlerinin kilitlerinden (Bayi,
// Hesap) AYRIDIR: bu kilidin altında onlar alınmaz → kilit sırası çakışması yok.
func DBOlustur(domainID int64) *sync.Mutex {
	mu.Lock()
	defer mu.Unlock()
	k, ok := dbler[domainID]
	if !ok {
		k = &sync.Mutex{}
		dbler[domainID] = k
	}
	return k
}
