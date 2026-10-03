package httpx

import (
	"net"
	"net/http"
	"strings"
)

// ClientIPKesin: YETKİ kararı (API anahtarı IP izin listesi) için istemci IP'si.
//
// 🔴 ClientIP'ten FARKI: ClientIP proxy gizlisi kurulu değilken (ilk açılış/geri
// alma) loopback'ten gelen X-Real-IP'ye güvenir — aynı kutudaki bir kiracı süreci
// :8080'e sahte X-Real-IP ile bağlanıp izin listesini geçebilirdi. Burada:
//   - doğrudan (loopback olmayan) bağlantı → soket adresi
//   - loopback → YALNIZ nginx'ten geldiği KANITLANIRSA (NginxtenMi) X-Real-IP;
//     kanıt yoksa "" (çağıran: liste doluysa reddet)
func ClientIPKesin(r *http.Request) string {
	uzak := hostOnly(r.RemoteAddr)
	if !yerelVekil(uzak) {
		return uzak
	}
	if !NginxtenMi(r) {
		return ""
	}
	v := strings.TrimSpace(r.Header.Get("X-Real-IP"))
	if net.ParseIP(v) == nil {
		return ""
	}
	return v
}
