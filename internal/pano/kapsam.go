// Package pano — pano istatistik widget'ları (GET /pano/w/{id}) ve arka plan
// örnekleyicisi (günlük anlık görüntü, disk ölçümü, eşik/olay bildirimleri).
//
// 🔴 KAPSAM TEK NOKTA: widget fonksiyonları *http.Request GÖRMEZ, yalnız
// jetondan çözülen Kapsam alır. İstemci parametresiyle (?reseller_id=, ?kapsam=)
// kapsam genişletmek yapısal olarak imkânsızdır. Bayi yalnız kendi rid'inin
// domainlerini görür; bayi jetonunda rid<=0 → 403 (fail-closed).
package pano

import (
	"net/http"

	"girginospanel/internal/auth"
	"girginospanel/internal/middleware"
)

// Kapsam — widget sorgularının görünürlüğü.
type Kapsam struct {
	Tum bool  // yönetici: tüm sunucu
	RID int64 // bayi: kendi users.id
}

// kapsamCoz — jetondan kapsam. ok=false → 403. Yönetici yalnız AÇIK bir admin
// jetonunda (rol admin VE bayi anahtarı yok); bayi rolü ya da bayi anahtarı olan
// her jeton bayi sayılır ve rid>0 olmalıdır (websec.kapsamAl ile aynı ilke).
func kapsamCoz(r *http.Request) (Kapsam, bool) {
	return claimsKapsam(middleware.ClaimsFrom(r))
}

// claimsKapsam — kapsamCoz'un saf çekirdeği (birim testi).
func claimsKapsam(c *auth.Claims) (Kapsam, bool) {
	if c == nil {
		return Kapsam{}, false
	}
	if c.Role == "admin" && c.ResellerID == 0 {
		return Kapsam{Tum: true}, true
	}
	if (c.Role == "reseller" || c.Role == "admin") && c.ResellerID > 0 {
		return Kapsam{RID: c.ResellerID}, true
	}
	return Kapsam{}, false
}

// kosul — domains (takma ad a) üzerinde görünürlük koşulu: tek SQL ifadesi.
func (k Kapsam) kosul(a string) (string, []any) {
	if k.Tum {
		return "1=1", nil
	}
	return a + ".reseller_id = ?", []any{k.RID}
}

// kimlik — pano_gunluk.kapsam_id (0 = tüm sunucu).
func (k Kapsam) kimlik() int64 {
	if k.Tum {
		return 0
	}
	return k.RID
}
