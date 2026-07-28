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
)

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
