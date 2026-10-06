package middleware

// adina.go — bayi/yöneticinin müşteri ADINA açtığı oturumun (cPanel tarzı "panele
// gir") sunucu-taraflı kuralları. Token'ı üreten uç: internal/adina.
//
// 🔴 GÜVEN MODELİ: adına-oturum token'ı bir müşteri token'ıdır (müşteri kapısında
// geçer, müşteri uçlarını çağırır) ama KİMLİĞİ müşterinin değil aktörün
// (bayi/yönetici) kimliğidir. Bu yüzden:
//   - geçerlilik FTP hesabına değil AKTÖRÜN hesabına bağlıdır: bayi askıya alınır,
//     silinir ya da oturumları düşürülürse adına-oturum da düşer;
//   - her istekte domain sahipliği YENİDEN doğrulanır: domain devredilirse
//     (reseller_id değişirse) eski bayinin oturumu anında düşer (IDOR yok);
//   - müşteri askısı (domains.askida) oturumu kesmez — aktör askıdaki hesabı
//     inceleyebilmeli (askı sayfasına düşmemeli). İSTİSNA: kökün aldığı askı
//     (askida_kaynak='manuel'): bayi o askıyı kaldıramadığı gibi (hesapaski.
//     ErrKokAskisi) hesabın içine de giremez — kökün askı yetkisi aşılmaz;
//   - kimlik işlemleri (müşteri parolası vb.) AdinaYasak ile 403;
//   - denetim kaydına aktör yazılır (Aktor).

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"time"

	"girginospanel/internal/auth"
	"girginospanel/internal/httpx"
)

// AdinaYasakMesaj: adına-oturumda kimlik işlemine verilen 403 metni.
const AdinaYasakMesaj = "müşteri adına oturumda bu işlem yapılamaz (kimlik işlemi)"

// Askı kaynakları — hesapaski.KaynakBayiElle / KaynakBayi ile AYNI değerler
// (hesapaski bu paketi dolaylı içe aktardığından sabitler burada tekrarlanır;
// eşitliği adina_test.go sınar).
const (
	askiKaynakBayiElle = "bayi_el"
	askiKaynakBayi     = "bayi"
)

// adinaDomain: adına-oturum kararının domain tarafı.
type adinaDomain struct {
	bulundu    bool
	resellerID int64
	askida     bool
	kaynak     string
}

// adinaDomainSorguFn: testte stub edilebilsin diye değişken.
var adinaDomainSorguFn = adinaDomainSorgu

func adinaDomainSorgu(ctx context.Context, domainID int64) (adinaDomain, error) {
	const sorgu = `SELECT COALESCE(reseller_id,0), COALESCE(askida,0), COALESCE(askida_kaynak,'') FROM domains WHERE id=?`
	var son error
	for deneme := 0; deneme <= oturumRetryAdet; deneme++ {
		if deneme > 0 {
			select {
			case <-ctx.Done():
				return adinaDomain{}, ctx.Err()
			case <-time.After(oturumRetryBekle):
			}
		}
		var d adinaDomain
		var askida int
		err := scopeDB.QueryRowContext(ctx, sorgu, domainID).Scan(&d.resellerID, &askida, &d.kaynak)
		if err == nil {
			d.bulundu, d.askida = true, askida == 1
			return d, nil
		}
		if errors.Is(err, sql.ErrNoRows) {
			return adinaDomain{}, nil
		}
		son = err
	}
	return adinaDomain{}, son
}

// AdinaKarar: aktörün bu domaine adına-oturumla girmesine izin var mı? (0,"") = evet.
// Token üretimi (internal/adina) ve her istek AYNI kararı kullanır.
func AdinaKarar(rol string, aktorRID int64, d adinaDomain) (int, string) {
	if !d.bulundu {
		return http.StatusForbidden, "domain bulunamadı"
	}
	switch rol {
	case "admin":
		return 0, ""
	case "reseller":
		if aktorRID <= 0 || d.resellerID != aktorRID {
			return http.StatusForbidden, "bu domaine erişiminiz yok"
		}
		if d.askida && d.kaynak != askiKaynakBayiElle && d.kaynak != askiKaynakBayi {
			return http.StatusForbidden, "hesap yönetici tarafından askıya alındı — müşteri paneline girilemez"
		}
		return 0, ""
	}
	return http.StatusForbidden, "yetkiniz yok"
}

// AdinaDomainKarar: AdinaKarar'ın DB'li hâli (internal/adina token üretiminde).
func AdinaDomainKarar(ctx context.Context, rol string, aktorRID, domainID int64) (int, string) {
	if scopeDB == nil {
		return http.StatusServiceUnavailable, "hesap durumu şu anda doğrulanamıyor"
	}
	d, err := adinaDomainSorguFn(ctx, domainID)
	if err != nil {
		log.Printf("adina: domain %d okunamadi: %v — REDDEDILDI (fail-closed)", domainID, err)
		return http.StatusServiceUnavailable, "hesap durumu şu anda doğrulanamıyor, birazdan tekrar deneyin"
	}
	return AdinaKarar(rol, aktorRID, d)
}

// AdinaOturumGecerli: adinaOturumGecerli'nin dışa açık adı (internal/adina token'ı
// vermeden önce aynı kontrolü koşar).
func AdinaOturumGecerli(ctx context.Context, mc *auth.MusteriClaims) (int, string) {
	return adinaOturumGecerli(ctx, mc)
}

// adinaOturumGecerli: adına-oturum token'ının her istekteki sunucu-taraflı kontrolü.
// (0,"") = geçerli. Fail-CLOSED (DB okunamazsa 503).
func adinaOturumGecerli(ctx context.Context, mc *auth.MusteriClaims) (int, string) {
	if mc.AktorRol != "admin" && mc.AktorRol != "reseller" {
		return http.StatusUnauthorized, "geçersiz oturum"
	}
	// Derinlemesine savunma: adına-oturum kısa ömürlüdür; daha uzun ömürlü bir
	// adına-token (imza sızıntısı/yanlış üretim) kabul edilmez.
	if mc.IssuedAt == nil || mc.ExpiresAt == nil ||
		mc.ExpiresAt.Sub(mc.IssuedAt.Time) > time.Duration(auth.AdinaOmurSn+5)*time.Second {
		return http.StatusUnauthorized, "geçersiz oturum"
	}
	if scopeDB == nil {
		return 0, ""
	}
	// 1) Aktörün hesabı: askı / silinme / oturum düşürme (bayi tokeniyle AYNI kapı,
	//    önbellek + fail-closed dahil).
	ak := &auth.Claims{UserID: mc.AktorUID, Role: mc.AktorRol, ResellerID: mc.AktorRID}
	ak.IssuedAt = mc.IssuedAt
	if kod, mesaj := oturumGecerli(ctx, ak); kod != 0 {
		if kod == http.StatusForbidden && mesaj == httpx.HesapAskidaMesaj {
			// Askıdaki BAYİ: müşteri kapısında askı sayfası değil, oturum sonu.
			return http.StatusUnauthorized, "oturum sonlandırıldı"
		}
		return kod, mesaj
	}
	// 2) Domain sahipliği + kök askısı — her istekte yeniden.
	if kod, mesaj := AdinaDomainKarar(ctx, mc.AktorRol, mc.AktorRID, mc.DomainID); kod != 0 {
		return kod, mesaj
	}
	// 3) API anahtarıyla başlatılan müşteri girişi (SSO): hesap askıdaysa oturum
	//    sürmez (olağan müşteri oturumuyla aynı davranış; kodlu 403 → askı sayfası).
	if mc.SSO {
		d, err := adinaDomainSorguFn(ctx, mc.DomainID)
		if err != nil {
			return http.StatusServiceUnavailable, "hesap durumu şu anda doğrulanamıyor, birazdan tekrar deneyin"
		}
		if d.askida {
			return http.StatusForbidden, httpx.HesapAskidaMesaj
		}
	}
	return 0, ""
}

// AdinaYasak: müşteri ADINA açılmış oturumda kimlik işlemlerini (müşteri parolası,
// panele giriş kimliği…) 403 ile reddeder. Olağan müşteri, bayi ve yönetici
// token'ları etkilenmez.
func AdinaYasak(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if MusteriClaimsFrom(r).Adina() {
			httpx.WriteError(w, http.StatusForbidden, AdinaYasakMesaj)
			return
		}
		next.ServeHTTP(w, r)
	})
}
