// destek-ajan — GirginOSPanel Telegram destek botunun SUNUCU TARAFI ajanı, panelden AYRI hizmet
// (girginospanel-destek-ajan.service). Panel çökse/durdurulsa bile bot sunucuyu teşhis edebilsin diye
// panel sürecinin içinde ÇALIŞMAZ.
//
//	girginospanel-destek-ajan
//
// Yaptıkları: merkeze (bot.girginos.io) bağlanır, imzalı iş emirlerini doğrular, R0 okuma araçlarını
// yürütür. Panel-bağımsız araçlar (servis_durum, nginx_test, dogrula_ozet, log_oku php_fpm/laravel/cron,
// site_yokla, son_degisiklikler, onarim_onizle) doğrudan komut/DB ile; panel-bağımlı araçlar panelin
// özel root-only Unix soket köprüsüyle (/run/girginospanel/destek-kopru.sock). Panel ile ajan arasındaki
// diğer tek bağ veritabanıdır (ayar/iptal yoklaması, canlılık nabzı).
//
// Sırlar: systemd LoadCredential (panel-env) — ortam değişkeni YOK. Cihaz kimliği
// (/etc/girginospanel/destek-cihaz.key) panelin eski gömülü ajanıyla AYNI dosya ve AYNI mühürleme
// (gizli.SaklaBagli + db.key) olduğundan güncellemede kimlik DEĞİŞMEZ.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"girginospanel/internal/config"
	"girginospanel/internal/db"
	"girginospanel/internal/destek/ajan"
	"girginospanel/internal/destek/arac"
	"girginospanel/internal/destek/kopru"
	"girginospanel/internal/gizli"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("destek-ajan: ")
	if err := calistir(); err != nil {
		log.Printf("çıkıyor: %v", err)
		os.Exit(1)
	}
}

func calistir() error {
	// 1) Tek örnek: aynı cihaz anahtarıyla iki ajan merkeze aynı anda bağlanamaz.
	serbest, err := ajan.TekOrnekKilidi("")
	if err != nil {
		return err
	}
	defer serbest()

	// 2) Asgari yapılandırma (DB + maske sırları). JWT sırrı vb. yalnız maske için bellekte.
	cfg, err := config.AjanYukle()
	if err != nil {
		return err
	}
	log.Printf("sırlar: %s", cfg.SirKaynagi)

	// 3) 🔴 db.key hazır değilse BAŞLAMA: gizli paketi okunamayan anahtarı yenisiyle ÜRETİP üstüne
	// yazardı → cihaz kimliği sessizce değişir, panel sırları çözülemez olurdu.
	if err := gizli.AnahtarHazir(); err != nil {
		return err
	}

	// 4) Veritabanı (MariaDB gecikmesinde bekle; systemd Restart=always yedeği var).
	basla := time.Now()
	d, err := db.Open(cfg.DBDsn)
	for err != nil {
		if time.Since(basla) >= 5*time.Minute {
			return err
		}
		log.Printf("db bağlanılamadı, 3 sn sonra yeniden: %v", err)
		time.Sleep(3 * time.Second)
		d, err = db.Open(cfg.DBDsn)
	}
	defer d.Close()

	// 5) Ajan + araç yürütücüsü (panel uçları köprü istemcisiyle).
	ist := &kopru.Istemci{}
	a := ajan.Yeni(d, ajan.YapilandirmaYukle(""))
	a.BilinenSirlar = cfg.Sirlar
	a.KopruPing = ist.Ping
	a.YurutucuAyarla(arac.YeniKopruli(d, ist, cfg.Sirlar))

	ctx, durdur := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer durdur()
	a.Baslat(ctx)
	log.Printf("başladı (pid %d)", os.Getpid())
	<-ctx.Done()
	log.Printf("durduruluyor")
	dctx, iptal := context.WithTimeout(context.Background(), 3*time.Second)
	defer iptal()
	a.Durdur(dctx)
	return nil
}
