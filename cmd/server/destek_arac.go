package main

import (
	"database/sql"
	"net/http"

	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/config"
	"girginospanel/internal/destek/arac"
)

// destekYurutucuKur — Telegram destek botunun R0 araç yürütücüsü. Ajan
// (internal/destek/ajan) bağlantısını kuran kod, yönlendirici (r) TÜM rotalarıyla
// kurulduktan SONRA bunu çağırıp sonucu ajana verir:
//
//	yur := destekYurutucuKur(d, r, cfg)
//
// Panel sırları çıktı maskesine birebir verilir (JWT sırrı, DB DSN'i ve parolası,
// Redis yönetici parolası); maske ayrıca desen/kişisel veri katmanlarını uygular.
func destekYurutucuKur(d *sql.DB, h http.Handler, cfg *config.Config) arac.Yurutucu {
	return arac.Yeni(d, h, destekPanelSirlari(cfg))
}

func destekPanelSirlari(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	var s []string
	if len(cfg.JWTSecret) > 0 {
		s = append(s, string(cfg.JWTSecret))
	}
	if cfg.DBDsn != "" {
		s = append(s, cfg.DBDsn)
		if dc, err := mysql.ParseDSN(cfg.DBDsn); err == nil && dc.Passwd != "" {
			s = append(s, dc.Passwd)
		}
	}
	if cfg.RedisAdminPass != "" {
		s = append(s, cfg.RedisAdminPass)
	}
	return s
}
