package httpx

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
)

type ErrorBody struct {
	Hata string `json:"hata"`
	// Kod: makine okunur hata kodu (isteğe bağlı). Ön yüz bir durumu metin
	// eşleştirerek değil bu alanla tanır; boşsa gövdeye yazılmaz (eski biçim aynen).
	Kod string `json:"kod,omitempty"`
}

// Hesap askısı — TÜM askı 403'lerinin (giriş uçları + oturum/abonelik kapıları) ortak
// yanıtı. Ön yüz bu kodu görünce paneli kapatıp tam ekran "Hesabınız askıya alındı"
// sayfasını gösterir. 🔴 Giriş uçlarında YALNIZ parola doğrulandıktan sonra dönülür:
// yanlış parolada askı bilgisi sızarsa hesap varlığı/askı durumu numaralandırılabilir.
const (
	KodHesapAskida   = "hesap_askida"
	HesapAskidaMesaj = "hesabınız askıya alınmış"
)

func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func WriteError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, ErrorBody{Hata: msg})
}

// WriteErrorKod: WriteError + makine okunur kod ({"hata":…,"kod":…}).
func WriteErrorKod(w http.ResponseWriter, status int, msg, kod string) {
	WriteJSON(w, status, ErrorBody{Hata: msg, Kod: kod})
}

// WriteHesapAskida: askıdaki hesabın tek biçimli 403 yanıtı.
func WriteHesapAskida(w http.ResponseWriter) {
	WriteErrorKod(w, http.StatusForbidden, HesapAskidaMesaj, KodHesapAskida)
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
//
// DenetimIP: denetim kaydi icin istemci IP'si. Gercek kullanici (nginx uzerinden)
// her zaman gercek IP'sini alir; baglanti GERCEKTEN ayni kutudan (loopback) gelmis
// ic/otomatik islemlerde yaniltici 127.0.0.1 yerine 'sistem' yazilir.
func DenetimIP(r *http.Request) string {
	ip := ClientIP(r)
	if (ip == "127.0.0.1" || ip == "::1") && yerelVekil(hostOnly(r.RemoteAddr)) {
		return "sistem"
	}
	return ip
}

func ClientIP(r *http.Request) string {
	uzak := hostOnly(r.RemoteAddr)
	if !yerelVekil(uzak) {
		return uzak // doğrudan bağlantı — başlıklara güvenme
	}
	// Loopback: nginx VEYA ayni-kutu kiraci sureci olabilir (ikisi de 127.0.0.1).
	// Gizli KURULMUSSA (normal durum) yalniz nginx'in ekledigi X-Gosp-Proxy'yi tasiyan
	// istegin basliklarina guven; aksi halde uzak(127.0.0.1) — kiraci sahte X-Real-IP
	// ile hiz-sinirini atlayamaz. Gizli yoksa (ilk boot/rollback) eski davranis korunur.
	if sec := ProxySecret(); sec != "" && !gizliVekilBasligi(r, sec) {
		return uzak
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
