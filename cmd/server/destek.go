package main

import (
	"context"
	"database/sql"

	"girginospanel/internal/config"
	"girginospanel/internal/destek/ajan"

	"github.com/go-sql-driver/mysql"
)

// destekKur: Telegram destek ajanı (Faz 1). Modül varsayılan KAPALI: ajan yalnız kök
// "Telegram Destek" sayfasından şalteri açınca merkeze bağlanır. Merkez açık anahtarı
// yoksa (üretim anahtarı -ldflags ile gömülü / geliştirmede /etc/girginospanel/destek.json)
// hiç bağlanmaz. Panik/hata panel sürecini etkilemez.
//
// Araç yürütücüsü (internal/destek/arac — PARÇA B) burada bağlanır:
//
//	a.Yurutucu = arac.Yeni(...)
//
// Bağlanmadıkça iş emirleri "arac_yok" ile reddedilir; eşleştirme çalışır.
func destekKur(ctx context.Context, d *sql.DB, cfg *config.Config) *ajan.Ajan {
	a := ajan.Yeni(d, ajan.YapilandirmaYukle(""))
	// Panel sırları araç çıktısında BİREBİR maskelenir (ikinci geçiş).
	sirlar := []string{string(cfg.JWTSecret), cfg.RedisAdminPass}
	if dc, err := mysql.ParseDSN(cfg.DBDsn); err == nil && dc.Passwd != "" {
		sirlar = append(sirlar, dc.Passwd)
	}
	a.BilinenSirlar = sirlar
	a.Baslat(ctx)
	return a
}
