package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"time"

	"girginospanel/internal/destek/ajan"
	"girginospanel/internal/destek/kopru"
	"girginospanel/internal/lisans"
)

// destekKur: Telegram destek — PANEL tarafı. Ajan (merkeze bağlanan, iş emri yürüten
// süreç) artık panelin içinde ÇALIŞMAZ: ayrı hizmet girginospanel-destek-ajan.service
// (cmd/destek-ajan). Panel çökse de destek sürer; ajan çökse de panel etkilenmez.
//
// 🔴 2026-10-04 ürün kararı: panelde Telegram Destek özelliği YOKTUR (menü, sayfa, HTTP ucu,
// panel anahtarı yok). Eşleştirme/açma/kapama/iptal yalnız müşteri portalında (app.girginos.io).
// Ajan, ilk anahtar varsa her zaman bağlanır; tek yerel kapatma: root'un
// /etc/girginospanel/destek-kapali dosyası (ya da systemd stop/disable).
//
// Panelde kalanlar:
//   - iptal kancası: kimlik olaylarında (parola/2FA/hosting silme…) bağlantı DB'de iptal
//     edilir, denetim yazılır; merkeze bildirimi ajan yapar.
//   - özel köprü (destekKopruBaslat): panel-bağımlı araçların GET çağrıları için.
func destekKur(d *sql.DB) {
	ajan.PanelKancasiKur(d)
	// Güncelleme sonrası eski (0644) lisans bileti dosyaları bir sonraki yazımı beklemeden sıkılaşsın.
	go lisans.BiletIzinleriniDuzelt()
}

// destekKopruBaslat: /run/girginospanel/destek-kopru.sock köprüsünü açar (yönlendirici TÜM
// rotalarıyla kurulduktan SONRA çağrılır). Yalnız uid 0 eşler; kurulamazsa panel çalışmaya
// devam eder (ajan panel-bağımlı araçlarda panel_yanit_vermiyor raporlar) ve 30 sn'de bir yeniden dener.
func destekKopruBaslat(ctx context.Context, h http.Handler) {
	s := &kopru.Sunucu{Router: h}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("🔴 destek köprü: panik: %v", r)
			}
		}()
		for ctx.Err() == nil {
			if err := s.Dinle(ctx); err != nil {
				log.Printf("destek köprü kurulamadı (30 sn sonra yeniden): %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(30 * time.Second):
			}
		}
	}()
}
