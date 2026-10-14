package dbgovernor

// halka.go — kayan pencere muhasebesi (saf, MariaDB/DB gerektirmez).

import "time"

// ornek: tek bir yoklamanın bir DB kullanıcısı için ölçümü.
type ornek struct {
	t     time.Time
	dt    float64 // önceki yoklamadan beri geçen süre (sn)
	cpu   float64 // bu aralıkta TAMAMLANAN deyimlerin CPU saniyesi (fiziksel üst sınırla kırpılmış)
	canli float64 // o an CPU yakan uzun deyim sayısı (anlık gösterge)
}

// halka: bir kullanıcının kayan penceredeki örnekleri.
type halka struct {
	kimlik   Kimlik
	ornekler []ornek
}

// ekle: örneği ekler, penceredan eskileri atar.
func (h *halka) ekle(o ornek, pencere time.Duration) {
	h.ornekler = append(h.ornekler, o)
	h.buda(o.t, pencere)
}

func (h *halka) buda(simdi time.Time, pencere time.Duration) {
	sinir := simdi.Add(-pencere)
	i := 0
	for i < len(h.ornekler) && !h.ornekler[i].t.After(sinir) {
		i++
	}
	if i > 0 {
		h.ornekler = append(h.ornekler[:0], h.ornekler[i:]...)
	}
}

// oranlar: penceredeki (a) CPU hızı = tamamlanan CPU saniyesi / pencere (çekirdek) ve (b) anlık
// göstergenin zaman ağırlıklı ortalaması (çekirdek: CPU yakan her uzun deyim ≈ 1 çekirdek).
//
// Pencere HER ZAMAN tam uzunluğa bölünür: yeni başlamış (az örnekli) bir pencere az gösterir, fazla
// değil. NOT: tek bir örnek, makine çekirdek sayısı büyükse (NCPU*dt/pencere > eşik) tek başına eşiği
// aşabilir; uzun deyim bitişi kredi ile (Motor.krediDus) çift sayımdan arındırılır, kalan sıçrama
// yalnız 1. seviyeyi tetikler (tırmanma kesintisiz aşım ister).
func (h *halka) oranlar(simdi time.Time, pencere time.Duration) (cpuHizi, canliOrt float64) {
	h.buda(simdi, pencere)
	p := pencere.Seconds()
	if p <= 0 {
		return 0, 0
	}
	var c, g float64
	for _, o := range h.ornekler {
		c += o.cpu
		g += o.canli * o.dt
	}
	return c / p, g / p
}

// bos: pencerede hiç etkinlik kalmadıysa true (halka silinebilir).
func (h *halka) bos() bool {
	for _, o := range h.ornekler {
		if o.cpu > 0 || o.canli > 0 {
			return false
		}
	}
	return true
}
