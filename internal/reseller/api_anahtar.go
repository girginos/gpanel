package reseller

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"

	"girginospanel/internal/apianahtar"
	"girginospanel/internal/httpx"
)

// bayiAnahtarlariniIptal: bayinin TÜM geçerli API anahtarlarını iptal eder
// (parola sıfırlama / silme — kullanıcı kararı 2026-10-03; askı İPTAL ETMEZ,
// anahtar askıda kalır — bkz. middleware/anahtar.go). Asıl işlemi
// bozmaz; sonuç denetime yazılır (iptal edilemediyse ok=0 — operatör görsün).
func bayiAnahtarlariniIptal(r *http.Request, db *sql.DB, bayiID, eden int64, kul, bayiAd, neden string) {
	n, err := apianahtar.SahibinTumunuIptal(r.Context(), db, bayiID, eden, neden)
	// Silinen bayinin kimliği yeniden kullanılırsa yeni bayi bu kaydı görmesin
	// (Sil denetim kapsamını zaten köke çeker).
	kapsam := bayiID
	if neden == "bayi_silindi" {
		kapsam = 0
	}
	if err != nil {
		httpx.Denetim(db, r, eden, kul, "api_anahtar.toplu_iptal", bayiAd,
			fmt.Sprintf("neden=%s HATA: anahtarlar iptal edilemedi", neden), kapsam, false)
		return
	}
	if n > 0 {
		httpx.Denetim(db, r, eden, kul, "api_anahtar.toplu_iptal", bayiAd,
			fmt.Sprintf("neden=%s iptal edilen=%d", neden, n), kapsam, true)
	}
}

// bayiAnahtarlariniIptalSistem: bayiAnahtarlariniIptal'in ARKA PLAN (istek yok) kopyasi —
// bayi silme zinciri istek bittikten sonra da calisir.
func bayiAnahtarlariniIptalSistem(ctx context.Context, db *sql.DB, bayiID, eden int64, kul, bayiAd, neden string) {
	n, err := apianahtar.SahibinTumunuIptal(ctx, db, bayiID, eden, neden)
	if err != nil {
		httpx.DenetimSistem(db, eden, kul, "api_anahtar.toplu_iptal", bayiAd,
			fmt.Sprintf("neden=%s HATA: anahtarlar iptal edilemedi", neden), 0, false)
		return
	}
	if n > 0 {
		httpx.DenetimSistem(db, eden, kul, "api_anahtar.toplu_iptal", bayiAd,
			fmt.Sprintf("neden=%s iptal edilen=%d", neden, n), 0, true)
	}
}
