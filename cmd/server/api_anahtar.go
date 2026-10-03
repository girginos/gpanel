package main

import (
	"database/sql"

	"girginospanel/internal/apianahtar/uc"
	"girginospanel/internal/middleware"

	"github.com/go-chi/chi/v5"
)

// apiAnahtarRotalari: API anahtarı yönetim uçları (D4). RequireAuth grubunun
// İÇİNDE çağrılır. Yönetim uçları yalnız etkileşimli oturuma açıktır
// (YalnizOturum: bir anahtar anahtar üretemez/döndüremez/iptal edemez); izin
// tablosu da bunları KimlikUclari'nda tutar (çift kilit).
func apiAnahtarRotalari(r chi.Router, d *sql.DB) {
	h := &uc.Handlers{DB: d}
	yonetim := r.With(middleware.AdminVeyaReseller, middleware.YalnizOturum)
	yonetim.Get("/api-anahtarlari", h.Liste)
	yonetim.Get("/api-anahtarlari/izin-gruplari", h.Katalog)
	yonetim.Post("/api-anahtarlari", h.Olustur)
	yonetim.Post("/api-anahtarlari/{id}/yenile", h.Yenile)
	yonetim.Delete("/api-anahtarlari/{id}", h.Iptal)
	// Yalnız anahtarla (JWT → 404).
	r.With(middleware.AdminVeyaReseller).Get("/api-anahtari/ben", h.Ben)
}
