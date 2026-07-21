package httpx

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
)

type ErrorBody struct {
	Hata string `json:"hata"`
}

func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func WriteError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, ErrorBody{Hata: msg})
}

// ClientIP — gerçek istemci IP'si.
//
// GÜVENLİK: Proxy başlıklarına YALNIZCA istek yerel ters-vekilden (nginx, 127.0.0.1)
// geldiğinde güvenilir. Aksi halde istemci kendi X-Forwarded-For'unu uydurup hem giriş
// hız-sınırını atlar hem denetim kaydını sahte IP'lerle zehirler.
//
// Başlık önceliği (nginx davranışına göre):
//   - X-Real-IP: nginx `$remote_addr` ile OTORİTER yazar; istemcinin gönderdiği değer
//     EZİLİR → güvenilir kaynak budur.
//   - X-Forwarded-For: nginx `$proxy_add_x_forwarded_for` ile istemci değerinin SONUNA
//     ekler; bu yüzden İLK değil SON eleman güvenilirdir.
func ClientIP(r *http.Request) string {
	uzak := hostOnly(r.RemoteAddr)
	if !yerelVekil(uzak) {
		return uzak // doğrudan bağlantı — başlıklara güvenme
	}
	if v := strings.TrimSpace(r.Header.Get("X-Real-IP")); v != "" {
		return v
	}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		parca := strings.Split(v, ",")
		if son := strings.TrimSpace(parca[len(parca)-1]); son != "" {
			return son
		}
	}
	return uzak
}

// yerelVekil — adres loopback mı (bizim nginx'imiz).
func yerelVekil(ip string) bool {
	p := net.ParseIP(ip)
	return p != nil && p.IsLoopback()
}

// hostOnly — "ip:port" → "ip" (IPv6 dahil).
func hostOnly(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return addr
}
