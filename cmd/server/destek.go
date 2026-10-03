package main

import (
	"context"
	"database/sql"

	"girginospanel/internal/config"
	"girginospanel/internal/destek/ajan"
)

// destekKur: Telegram destek ajanı (Faz 1). Modül varsayılan KAPALI: ajan yalnız kök
// "Telegram Destek" sayfasından şalteri açınca merkeze bağlanır. Merkez açık anahtarı
// yoksa (üretim anahtarı -ldflags ile gömülü / geliştirmede /etc/girginospanel/destek.json)
// hiç bağlanmaz. Panik/hata panel sürecini etkilemez.
//
// Araç yürütücüsü (internal/destek/arac) main() içinde, yönlendirici TÜM rotalarıyla
// kurulduktan sonra destekA.YurutucuAyarla(destekYurutucuKur(d, r, cfg)) ile bağlanır.
// Bağlanmadıkça iş emirleri "arac_yok" ile reddedilir; eşleştirme çalışır.
func destekKur(ctx context.Context, d *sql.DB, cfg *config.Config) *ajan.Ajan {
	a := ajan.Yeni(d, ajan.YapilandirmaYukle(""))
	// Panel sırları araç çıktısında BİREBİR maskelenir (ikinci geçiş; ilk geçiş yürütücüde).
	a.BilinenSirlar = destekPanelSirlari(cfg)
	a.Baslat(ctx)
	return a
}
