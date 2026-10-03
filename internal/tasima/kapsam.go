package tasima

// kapsam.go — taşıma uçlarında İSTEK KAPSAMI (kök / bayi) ve bayi hız sınırı.
//
// Tek handler seti, kapsam handler içinde zorlanır (domains.Create deseni):
//   - admin (bayi kimliği taşımayan) → kök kapsamı (RID 0): her şeyi görür.
//   - reseller → RID = kendi users.id; rid<=0 olan bayi jetonu HİÇBİR ŞEY
//     yapamaz (403) — aksi halde RID 0 = kök kapsamına düşerdi.
//   - başka her rol (müşteri) → 403 (rota da AdminVeyaReseller ile kapalı).
//
// İşin kapsamı tasima_isleri.sahip_rid sütunundadır ve YALNIZ buradan yazılır;
// istemci gövdesindeki hiçbir alan kapsamı belirlemez.

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"girginospanel/internal/auth"
	"girginospanel/internal/gizli"
	"girginospanel/internal/middleware"
)

// claimsAl — istek kimliği (testler değiştirir; denetim paketiyle aynı dikiş).
var claimsAl = middleware.ClaimsFrom

// istekKapsami — çözülmüş istek kimliği + kapsam.
type istekKapsami struct {
	Kapsam
	UID int64
	Kul string
}

// kapsamAl — isteğin kapsamını çözer. Döner: kapsam, HTTP kodu (0 = geçerli), mesaj.
func kapsamAl(r *http.Request) (istekKapsami, int, string) {
	c := claimsAl(r)
	if c == nil {
		return istekKapsami{}, http.StatusForbidden, "yetkiniz yok"
	}
	return kapsamClaims(c)
}

func kapsamClaims(c *auth.Claims) (istekKapsami, int, string) {
	ik := istekKapsami{UID: c.UserID, Kul: c.Username}
	switch c.Role {
	case "admin":
		// Bayi kimliği taşıyan yönetici jetonu (ör. bayi görünümü) DAR kapsamla çalışır.
		if c.ResellerID > 0 {
			ik.RID = c.ResellerID
		}
		return ik, 0, ""
	case "reseller":
		if c.ResellerID <= 0 {
			return istekKapsami{}, http.StatusForbidden, "yetkiniz yok"
		}
		ik.RID = c.ResellerID
		return ik, 0, ""
	}
	return istekKapsami{}, http.StatusForbidden, "yetkiniz yok"
}

// kimlikSifrele / kimlikCoz — kaynak kimliğinin at-rest şifrelemesi (AES-GCM,
// internal/gizli). Testler panelin gerçek anahtar dosyasına dokunmamak için değiştirir.
var (
	kimlikSifrele = gizli.SaklaBagli
	kimlikCoz     = gizli.CozBagli
)

// kimlikBaglam — kaynak kimliğinin at-rest şifreleme bağlamı. Kök: host (eski
// satırlar çözülmeye devam eder). Bayi: "r<rid>|<host>" → bir kiracının şifreli
// metni başka kiracının (ya da kökün) bağlamında ÇÖZÜLEMEZ.
func kimlikBaglam(kp Kapsam, host string) string {
	if kp.Bayi() {
		return fmt.Sprintf("r%d|%s", kp.RID, host)
	}
	return host
}

// ---------------------------------------------------------------------------
// Bayi hız sınırı — test / keşif / başlat (bağlantı açan uçlar). Kaynak listesi,
// durum ve günlük okumaları SAYILMAZ: ön yüzün odak/aralık yenilemesi hakkı
// tüketmesin. İç ağa tarama ve kaba kuvvet denemesini keser.
// ---------------------------------------------------------------------------

const (
	bayiHizPencere = time.Minute
	bayiHizSiniri  = 10
)

type hizKova struct {
	bas  time.Time
	sayi int
}

var (
	hizMu    sync.Mutex
	hizKovas = map[int64]*hizKova{}
	hizSaat  = time.Now // testler değiştirir
)

// bayiHizIzni — bayi bu pencerede bir bağlantı ucu daha çağırabilir mi? Kök sınırsız.
func bayiHizIzni(kp Kapsam) bool {
	if !kp.Bayi() {
		return true
	}
	hizMu.Lock()
	defer hizMu.Unlock()
	simdi := hizSaat()
	kv := hizKovas[kp.RID]
	if kv == nil || simdi.Sub(kv.bas) >= bayiHizPencere {
		// Pencere bitti: kova sıfırlanır; eski kovalar da burada temizlenir.
		for rid, k := range hizKovas {
			if simdi.Sub(k.bas) >= bayiHizPencere {
				delete(hizKovas, rid)
			}
		}
		kv = &hizKova{bas: simdi}
		hizKovas[kp.RID] = kv
	}
	if kv.sayi >= bayiHizSiniri {
		return false
	}
	kv.sayi++
	return true
}
