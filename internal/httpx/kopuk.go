package httpx

import (
	"context"
	"net/http"
	"time"
)

// KopukBaglam — yan etkili, uzun süren bir işin istek bağlamından KOPARILMIŞ
// bağlamı: istemci (tarayıcı zaman aşımı, sekme kapanması, ağ kopması) bağlantıyı
// bıraktığında r.Context() iptal olur; iş o bağlamla yürüyorsa YARIDA kalır
// (ör. 2026-10-03 181: hosting oluşturma kullanıcı/vhost/FPM'i kurdu, DNS tohumu
// "context canceled" ile düştü → zone'suz alan adı).
//
// Kural: ilk yan etkiden (useradd, dosya/nginx yazımı, DB INSERT) ÖNCE bu bağlama
// geçilir ve işin geri kalanı yalnız onu kullanır. Değerler (istek kimliği vb.)
// korunur, iptal sinyali taşınmaz; üst süre sonsuz takılmayı keser.
//
// Çağıran defer iptal() yapmalıdır.
func KopukBaglam(r *http.Request, sure time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(r.Context()), sure)
}
