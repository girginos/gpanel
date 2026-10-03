package tasima

// rotalar.go — taşıma uçlarının TEK kayıt yeri (main.go yalnız Rotalar(r) çağırır).
//
//   /tasima/*         AdminVeyaReseller — kapsam handler içinde (kapsam.go).
//   PUT /tasima/ayar, PUT /tasima/kaynaklar/{kod}   AdminOnly (kökün anahtarları).
//   /system/tasima/*  bir sürüm boyunca AdminOnly TAKMA AD (eski ön yüz/betikler);
//                     aynı handler'lar → kök kapsamı.

import (
	"github.com/go-chi/chi/v5"

	"girginospanel/internal/middleware"
)

// Rotalar — kimlik doğrulanmış (RequireAuth) /api/v1 grubuna kaydeder.
func (h *Handlers) Rotalar(r chi.Router) {
	ar := r.With(middleware.AdminVeyaReseller)
	ar.Get("/tasima/kaynaklar", h.Kaynaklar)
	ar.Get("/tasima/anahtar", h.PanelAnahtar)
	ar.Post("/tasima/test", h.Test)
	ar.Post("/tasima/kesif", h.Kesif)
	ar.Post("/tasima/baslat", h.Baslat)
	ar.Get("/tasima", h.Durum)
	ar.Get("/tasima/oturumlar", h.OturumListe)
	ar.Get("/tasima/oturum/{id}", h.OturumGetir)
	ar.Delete("/tasima/oturum/{id}", h.OturumSil)
	ar.Get("/tasima/{id}", h.Detay)
	ar.Get("/tasima/{id}/log", h.Log)
	ar.Post("/tasima/{id}/iptal", h.Iptal)

	yr := r.With(middleware.AdminOnly)
	yr.Put("/tasima/ayar", h.AyarGuncelle)
	yr.Put("/tasima/kaynaklar/{kod}", h.KaynakGuncelle)

	// Eski yollar (takma ad, yalnız yönetici).
	yr.Post("/system/tasima/test", h.Test)
	yr.Post("/system/tasima/kesif", h.Kesif)
	yr.Get("/system/tasima/anahtar", h.PanelAnahtar)
	yr.Post("/system/tasima/baslat", h.Baslat)
	yr.Get("/system/tasima", h.Durum)
	yr.Get("/system/tasima/{id}", h.Detay)
	yr.Get("/system/tasima/{id}/log", h.Log)
	yr.Get("/system/tasima/oturumlar", h.OturumListe)
	yr.Get("/system/tasima/oturum/{id}", h.OturumGetir)
	yr.Delete("/system/tasima/oturum/{id}", h.OturumSil)
	yr.Post("/system/tasima/{id}/iptal", h.Iptal)
}
