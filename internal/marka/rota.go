package marka

// rota.go — marka rotalarının TEK kayıt yeri (main.go yalnız iki satırla çağırır;
// rotalar dağılmasın, chi'nin aynı deseni sessizce üstüne yazması riski olmasın).

import (
	"github.com/go-chi/chi/v5"

	"girginospanel/internal/middleware"
)

// GenelRotalar — /api/v1 alt yönlendiricisinde, RequireAuth grubunun DIŞINDA.
// Giriş sayfası markayı kimlik doğrulamadan ÖNCE okur.
func (h *Handlers) GenelRotalar(r chi.Router) {
	r.Get("/marka", h.Genel)
	r.Get("/marka/gorsel/{anahtar}/{yuva}", h.Gorsel)
	// Geri uyum (eski istemciler/önbellekteki paket): yalnız kök görselleri.
	r.Get("/marka/logo", LogoHandler)
	r.Get("/marka/banner", BannerHandler)
	r.Get("/marka/favicon", FaviconHandler)
}

// KimlikliRotalar — /api/v1 altındaki RequireAuth grubunun İÇİNDE.
func (h *Handlers) KimlikliRotalar(r chi.Router) {
	r.Get("/marka/oturum", h.Oturum)

	r.With(middleware.AdminVeyaReseller).Get("/marka/yonet", h.YonetGetir)
	r.With(middleware.AdminVeyaReseller).Put("/marka/yonet", h.YonetKaydet)
	r.With(middleware.AdminVeyaReseller).Post("/marka/yonet/gorsel/{yuva}", h.GorselYukle)
	r.With(middleware.AdminVeyaReseller).Delete("/marka/yonet/gorsel/{yuva}", h.GorselSil)
	r.With(middleware.AdminVeyaReseller).Post("/marka/yonet/sifirla", h.Sifirla)

	r.With(middleware.AdminVeyaReseller).Get("/marka/alan-adi", h.AlanGetir)
	r.With(middleware.AdminVeyaReseller).Post("/marka/alan-adi", h.AlanEkle)
	r.With(middleware.AdminVeyaReseller).Post("/marka/alan-adi/dogrula", h.AlanDogrula)
	r.With(middleware.AdminVeyaReseller).Post("/marka/alan-adi/uygula", h.AlanUygula)
	r.With(middleware.AdminVeyaReseller).Delete("/marka/alan-adi", h.AlanSil)

	r.With(middleware.AdminOnly).Get("/marka/bayiler", h.Bayiler)
	r.With(middleware.AdminOnly).Put("/marka/bayiler/{id}", h.BayiIzin)
	r.With(middleware.AdminOnly).Post("/marka/bayiler/{id}/sifirla", h.BayiSifirla)
	r.With(middleware.AdminOnly).Delete("/marka/bayiler/{id}/alan-adi", h.BayiAlanSil)
	r.With(middleware.AdminOnly).Put("/marka/ayar", h.AyarKaydet)
}
