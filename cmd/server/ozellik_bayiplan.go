package main

// ozellik_bayiplan.go — bayinin kendi hosting planları: varsayılan plan uzlaştırması
// (açılış), plan düzenlemesinde bağlı hostinglere yeniden uygulama bağlantısı ve
// ek /plans uçları. main.go yalnız bu iki fonksiyonu çağırır.

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/domains"
	"girginospanel/internal/middleware"
	"girginospanel/internal/plans"
)

// bayiPlanRotalari — AdminVeyaReseller (müşteri asla): yönetici her katalog planı,
// bayi yalnız kendi planı (kapsam handler içinde, plans/bayi_plan.go).
func bayiPlanRotalari(r chi.Router, ph *plans.Handlers) {
	r.With(middleware.AdminVeyaReseller).Get("/plans/bayi-sinir", ph.BayiSinir)
	r.With(middleware.AdminVeyaReseller).Post("/plans/{id}/varsayilan", ph.VarsayilanYap)
}

// bayiPlanAcilis — plan düzenlemesi bağlı hostinglere "Yeniden Uygula" yoluyla
// uygulanır (domains → plans içe aktarımı döngü olmasın diye burada bağlanır) ve
// varsayılan planı olmayan her bayiye plan sağlanır (idempotent). SeedSync ve
// ozelPlanAcilis'ten SONRA (şablon = kökün varsayılan global planı). Hata açılışı
// düşürmez; eksik kalan bayi bir sonraki açılışta / plansız taşımada yeniden denenir.
func bayiPlanAcilis(d *sql.DB) {
	plans.HostingUygula = domains.PlanSonrasiUygula
	ctx, iptal := context.WithTimeout(context.Background(), time.Minute)
	defer iptal()
	olusan, isaretlenen, err := plans.BayiVarsayilanUzlastir(ctx, d)
	if err != nil {
		log.Printf("🔴 bayi varsayılan plan uzlaştırması eksik kaldı: %v", err)
	}
	if olusan+isaretlenen > 0 {
		log.Printf("bayi varsayılan plan: %d plan oluşturuldu, %d plan varsayılan işaretlendi", olusan, isaretlenen)
	}
}
