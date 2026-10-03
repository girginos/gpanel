package main

// ozellik_ozelplan.go — D1 "özel plan = ÜST YAZIM": rotalar + açılış dönüşümü.
// main.go yalnız bu iki fonksiyonu çağırır (sıcak dosyada küçük, ayrık değişiklik).

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

// ozelPlanRotalari — hostinge özel limitler. SahipYonetim: yönetici her hostingte,
// bayi yalnız kendi hostingte, müşteri asla.
func ozelPlanRotalari(r chi.Router, dh *domains.Handlers) {
	r.With(middleware.SahipYonetim).Get("/domains/{id}/limitler", dh.LimitGetir)
	r.With(middleware.SahipYonetim).Put("/domains/{id}/limitler", dh.LimitKaydet)
	r.With(middleware.SahipYonetim).Delete("/domains/{id}/limitler", dh.LimitSifirla)
}

// ozelPlanAcilis — eski "<alan> — Özel" kopyalarını üst yazıma dönüştürür. Göçlerden
// ve SeedSync'ten SONRA, HealTenantFPM/HealKotaOnStartup'tan ÖNCE eşzamanlı koşar
// (efektif değerler birebir korunduğu için slice/kota/FPM değişmez). Hata açılışı
// düşürmez; dönüşemeyen kopya yerinde kalır ve loglanır.
func ozelPlanAcilis(d *sql.DB) {
	ctx, iptal := context.WithTimeout(context.Background(), 2*time.Minute)
	defer iptal()
	rapor, err := plans.OzelPlanlariDonustur(ctx, d)
	if err != nil {
		log.Printf("🔴 ozel plan donusumu calismadi: %v", err)
		return
	}
	if n := int64(len(rapor.Donusen)+len(rapor.Atlanan)+len(rapor.Temizlenen)) + rapor.BayatUst + rapor.BayatArsiv; n > 0 {
		log.Printf("ozel plan donusumu: %d donustu, %d atlandi (taban belirsiz/paylasilan), %d kullanilmayan kopya arsivlendi, %d/%d bayat ust yazim/arsiv (geri yukleme kalintisi) silindi",
			len(rapor.Donusen), len(rapor.Atlanan), len(rapor.Temizlenen), rapor.BayatUst, rapor.BayatArsiv)
	}
}
