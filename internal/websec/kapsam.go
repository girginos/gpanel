package websec

// Website Security Monitor GÖRÜNÜRLÜK KURALI — tek yerde (kapsamAl), atlanamaz:
//   - kök (admin) → tüm sunucu (bugünkü görünüm değişmez)
//   - bayi        → YALNIZ domains.reseller_id = kendi ResellerID'si olan domainler
//     (resellerDomainSahibi ile aynı anahtar). Kimliksiz bayi jetonu (ResellerID<=0)
//     HİÇBİR ŞEY görmez: başkasınınkini göstermektense boş.
//
// websec tablolarına reseller_id YAZILMAZ: kapsam her istekte domains JOIN'i ile canlı
// çözülür — devirde yeni sahip görür, eski görmez. Koşul istemci parametresiyle
// genişletilemez. Bayiye sayaç, liste, hata metni ve durum alanı dahil sunucu geneli
// ya da başka kiracı bilgisi gitmez; yabancı kayıt ile olmayan kayıt aynı yanıtı alır.
// (Not: middleware'e ileride ortak bir KapsamAl gelecek; şimdilik denetim deseni.)

import (
	"log"
	"net/http"

	"girginospanel/internal/auth"
	"girginospanel/internal/middleware"
)

// claimsAl: isteğin kimliği (testler değiştirir; üretimde middleware.ClaimsFrom).
var claimsAl = middleware.ClaimsFrom

// bayiMi: bayi jetonu mu (rol ya da sahiplik anahtarı — ikisinden biri yeter; biri
// eksik kalsa bile bayi yönetici görünürlüğüne DÜŞMEZ).
func bayiMi(c *auth.Claims) bool {
	return c != nil && (c.Role == "reseller" || c.ResellerID > 0)
}

// kapsam: isteğin görünürlüğü. bayi=false yalnız açık bir yönetici jetonunda olur;
// kimlik yoksa ya da rol tanınmıyorsa bayi+rid=0 (hiçbir şey) — fail-closed.
type kapsam struct {
	bayi bool
	rid  int64
}

func kapsamAl(r *http.Request) kapsam {
	c := claimsAl(r)
	if c != nil && c.Role == "admin" && !bayiMi(c) {
		return kapsam{}
	}
	k := kapsam{bayi: true}
	if c != nil {
		k.rid = c.ResellerID
	}
	return k
}

// kosul: domains tablosu (takma adı d) üzerinde görünürlük süzgeci; sorguya
// " WHERE 1=1" ardından eklenir. Kök → süzgeç yok.
func (k kapsam) kosul() (string, []any) {
	switch {
	case !k.bayi:
		return "", nil
	case k.rid <= 0:
		return " AND 1=0", nil
	default:
		return " AND d.reseller_id=?", []any{k.rid}
	}
}

// hata: 500 yanıtı. Bayiye ham SQL hatası (tablo/sütun/anahtar adı) DÖNMEZ; günlüğe
// yazılır. Kök yanıtı değişmedi.
func (k kapsam) hata(w http.ResponseWriter, err error) {
	if !k.bayi {
		hataMesaji(w, 500, err.Error())
		return
	}
	log.Printf("websec: bayi %d isteği DB hatası: %v", k.rid, err)
	hataMesaji(w, 500, "işlem tamamlanamadı")
}
