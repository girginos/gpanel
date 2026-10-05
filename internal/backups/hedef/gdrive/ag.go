// Package gdrive — Google Drive (API v3, YALNIZ drive.file kapsamı) yedek hedefi.
//
// Hedef arayüzünü (hedef.Hedef) uygular ve "drive" türünü kayıt defterine ekler.
// Bağımlılık eklenmedi: düz net/http + encoding/json (golang.org/x/oauth2 cihaz
// akışı ve hata eşlemesi için fazla soyut; refresh/cihaz akışı burada 100 satır).
//
// 🔴 Güvenlik sözleşmesi:
//   - Ağ çıkışı YALNIZ agkapi (SSRF kapısı) üzerinden ve ayrıca alan adı izin
//     listesinden geçer (*.googleapis.com, accounts.google.com); https zorunlu.
//     Uç adresleri ayar JSON'undan OKUNMAZ (ayar bilinmeyen alanı reddeder).
//   - OAuth istemci gizli anahtarı ve yenileme jetonu yalnız sirlar haritasında
//     gelir; hata metinlerine/günlüğe düz sızmaz (hedef.YeniHata maskeler,
//     istek URL'leri hata metninden ayıklanır).
//   - Yalnız "https://www.googleapis.com/auth/drive.file" kapsamı: uygulamanın
//     KENDİ oluşturduğu dosyalar; kullanıcının diğer Drive dosyalarını göremez.
package gdrive

import (
	"fmt"
	"net"
	"net/http"
	"strings"

	"girginospanel/internal/agkapi"
	"girginospanel/internal/backups/hedef"
)

// Sabitler.
const (
	// TurAdi — kayıt defterindeki tür adı (backup_hedefler.tur).
	TurAdi = "drive"
	// Kapsam — istenen TEK OAuth kapsamı. Daha genişi (drive, drive.readonly)
	// ASLA istenmez; testle kilitli.
	Kapsam = "https://www.googleapis.com/auth/drive.file"
	// ParcaBoyutu — resumable yükleme parçası (256 KiB'nin katı olmak ZORUNDA).
	ParcaBoyutu = 8 << 20
	// VarsayilanKlasor — Drive kökünde açılan yedek klasörü.
	VarsayilanKlasor = "GirginOSPanel Yedek"

	klasorMime = "application/vnd.google-apps.folder"
)

// uclar — Google uç adresleri. Üretimde sabit; yalnız testler (aynı paket)
// değiştirir. Ayar JSON'undan ya da dışarıdan ASLA ayarlanamaz.
type uclar struct {
	api, yukleme, token, cihaz, iptal string
}

const (
	googleAPI   = "https://www.googleapis.com"
	googleOAuth = "https://oauth2.googleapis.com"
)

var uretimUclari = uclar{
	api:     googleAPI + "/drive/v3",
	yukleme: googleAPI + "/upload/drive/v3",
	token:   googleOAuth + "/token",
	cihaz:   googleOAuth + "/device/code",
	iptal:   googleOAuth + "/revoke",
}

// HostIzinliMi — alan adı Google'ın OAuth/Drive uçlarından biri mi?
// İzinli: googleapis.com ve alt alanları, accounts.google.com. IP-literal ve
// benzer görünümlü adlar ("evilgoogleapis.com", "googleapis.com.evil.com")
// REDDEDİLİR.
func HostIzinliMi(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	if h == "" || net.ParseIP(h) != nil {
		return false
	}
	if h == "accounts.google.com" || h == "googleapis.com" {
		return true
	}
	return strings.HasSuffix(h, ".googleapis.com")
}

// kapiTransport — her isteği (yönlendirmeler dahil) alan adı izin listesinden ve
// https zorunluluğundan geçirir, sonra SSRF kapısının transport'una verir.
type kapiTransport struct {
	taban    http.RoundTripper
	hostIzin func(string) bool
	httpIzin bool // yalnız testler (httptest sunucusu)
}

func (t *kapiTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u := req.URL
	if u.Scheme != "https" && !t.httpIzin {
		return nil, fmt.Errorf("%w: yalnız https", agkapi.ErrReddedildi)
	}
	if !t.hostIzin(u.Hostname()) {
		return nil, fmt.Errorf("%w: alan adı izinli değil", agkapi.ErrReddedildi)
	}
	return t.taban.RoundTrip(req)
}

// yeniHTTP — kapıdan + izin listesinden geçen istemci. Zaman aşımı istek
// bağlamıyla (ctx) verilir; Client.Timeout gövde akışını keserdi.
func yeniHTTP(sec hedef.KurSecenek, hostIzin func(string) bool, httpIzin bool) *http.Client {
	kapi := sec.KapiAl()
	return &http.Client{
		Transport:     &kapiTransport{taban: kapi.HTTPTransport(), hostIzin: hostIzin, httpIzin: httpIzin},
		CheckRedirect: kapi.YonlendirmeGuard,
	}
}
