package domains

// ftp_oturum.go — FTP parolası değişince o hesabın AÇIK FTP oturumlarını düşürme
// çağrısının TEK handler noktası. Parola yazımı BAŞARILI olduktan sonra çağrılır;
// düşürme hatası parola değişimini başarısız YAPMAZ: günlüğe + denetime (ok=false)
// yazılır. Çekirdek: hesaplar.FTPOturumlariDusur (pure-ftpwho + SIGTERM, ad doğrulamalı).

import (
	"log"
	"net/http"
	"strconv"

	"girginospanel/internal/hesaplar"
	"girginospanel/internal/httpx"
)

// Dikiş: testler gerçek pure-ftpd'ye dokunmaz.
var ftpOturumDusur = hesaplar.FTPOturumlariDusur

// ftpOturumlariDusur: kullanici (ana ya da ek FTP hesabı) açık oturumlarını düşürür.
// Hiç oturum yoksa denetim yazılmaz (gürültü yok).
func (h *Handlers) ftpOturumlariDusur(r *http.Request, uid int64, aktor, alan, kullanici string, domainID int64) {
	n, err := ftpOturumDusur(kullanici)
	if err != nil {
		log.Printf("GÜVENLİK: FTP parolası değişti ama açık oturumlar düşürülemedi domain=%d kullanici=%q (düşen=%d): %v",
			domainID, kullanici, n, err)
		httpx.DenetimDomain(h.DB, r, uid, aktor, "ftp.oturum_dusur", alan,
			"kullanici="+kullanici+" hata=açık oturumlar sonlandırılamadı", domainID, false)
		return
	}
	if n > 0 {
		httpx.DenetimDomain(h.DB, r, uid, aktor, "ftp.oturum_dusur", alan,
			"kullanici="+kullanici+" sonlandirilan="+strconv.Itoa(n), domainID, true)
	}
}
