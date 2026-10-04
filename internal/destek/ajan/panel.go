package ajan

// panel.go — PANEL SÜRECİ tarafı. Panel ajanı çalıştırmaz; yalnız DB'ye yazar:
// kimlik olayları (parola değişimi, 2FA kapatma, hosting silme/askı, bayi askı/silme)
// internal/destek/iptal ile bağlantıyı DB'de iptal eder ve bu kanca denetim kaydını
// yazar. Merkeze bildirim + oturum kapatma işini ajan süreci DB yoklamasıyla yapar
// (izleme.go iptalYokla).

import (
	"context"
	"database/sql"

	"girginospanel/internal/destek/iptal"
	"girginospanel/internal/httpx"
)

// PanelKancasiKur: iptal.Kanca'yı denetim-yalnız kancaya bağlar (panel main'i çağırır).
func PanelKancasiKur(db *sql.DB) {
	iptal.Kanca = func(ctx context.Context, aktorUID int64, aktorAd string, kayitlar []iptal.Kayit) {
		for _, k := range kayitlar {
			// Silinen bayinin kimliği yeniden kullanılırsa yeni bayi bu kaydı görmesin
			// (reseller.Sil denetim kapsamını köke çeker; aynı kural).
			kapsam := k.ResellerID
			if kapsam > 0 {
				var var_ int
				if db.QueryRowContext(ctx, `SELECT 1 FROM users WHERE id=?`, kapsam).Scan(&var_) != nil {
					kapsam = 0
				}
			}
			httpx.DenetimSistem(db, aktorUID, aktorAd, dBaglantiIptal, hedefTg(k.TgKullaniciAdi, k.TgUserID),
				"neden="+string(k.Neden)+" kaynak=otomatik", kapsam, true)
		}
	}
}

// PanelKancasiKur: yöntem biçimi (a.DB ile).
func (a *Ajan) PanelKancasiKur() { PanelKancasiKur(a.DB) }
