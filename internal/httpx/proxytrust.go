package httpx

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"os"
	"strings"
	"sync"
)

// ProxySecretPath: nginx ile paylasilan gizli. Provisioner Heal'i, nginx'e
// `proxy_set_header X-Gosp-Proxy "<gizli>"` enjekte ettikten ve `nginx -t` GECTIKTEN
// SONRA bu dosyayi yazar. Boylece dosya varsa nginx'in de basligi gonderdigi GARANTIdir
// (yoksa ClientIP eski loopback-guven davranisina duser → kilitlenme yok).
const ProxySecretPath = "/etc/girginospanel/proxy.secret" //nolint:gosec // G101 yanlış-pozitif: gömülü sır DEĞİL, bir dosya YOLU (değişken adı "Secret" içerdiği için regex takılıyor); asıl gizli çalışma anında bu dosyadan okunur / NewProxySecret'te crypto/rand ile üretilir.

var (
	proxyOnce sync.Once
	proxyVal  string
)

// ProxySecret: kalici gizliyi (varsa) doner; yoksa "". SALT-OKUR, uretmez —
// uretim+nginx-senkron provisioner Heal'inin isi (fail-safe siralama icin).
func ProxySecret() string {
	proxyOnce.Do(func() { proxyVal = proxyGizliOku(ProxySecretPath) })
	return proxyVal
}

// proxyGizliOku: yol'daki gizliyi (kırpılmış) döner; dosya yoksa/okunamıyorsa ya da
// 32 karakterden kısaysa "" (zayıf/yarım yazılmış gizliye güvenilmez). Yol parametre →
// testler gerçek /etc/girginospanel/proxy.secret'a dokunmadan sınar.
func proxyGizliOku(yol string) string {
	b, err := os.ReadFile(yol)
	if err != nil {
		return ""
	}
	if t := strings.TrimSpace(string(b)); len(t) >= 32 {
		return t
	}
	return ""
}

// NewProxySecret: 256-bit rastgele hex gizli uretir (provisioner Heal kullanir).
func NewProxySecret() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return hex.EncodeToString(buf)
}

// gizliVekilBasligi: istek gercekten nginx'ten mi (paylasimli gizliyi tasiyor mu)?
func gizliVekilBasligi(r *http.Request, sec string) bool {
	got := strings.TrimSpace(r.Header.Get("X-Gosp-Proxy"))
	return got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(sec)) == 1
}

// NginxtenMi: istek GERCEKTEN bizim nginx'imizden mi (loopback + paylasimli gizli)?
//
// 🔴 ClientIP'ten FARKI: gizli kurulu degilse ClientIP "eski loopback-guven"
// davranisina duser (ilk acilista kimse kilitlenmesin diye). Bu fonksiyon ise YETKI
// karari veren cagiranlar icindir (panel kapisi): kanit yoksa false. Ayni kutudaki
// bir kiraci sureci de 127.0.0.1'den :8080'e baglanabilir; tek ayirt edici nginx'in
// ekledigi, kiracinin okuyamadigi gizlidir.
func NginxtenMi(r *http.Request) bool {
	return nginxtenMiGizli(r, ProxySecret())
}

// nginxtenMiGizli: NginxtenMi'nin SAF çekirdeği — gizli parametre olarak gelir (G/Ç yok),
// böylece güven zincirinin kökü makinedeki gizli dosyasından bağımsız, deterministik
// sınanır. Boş gizli (kurulu değil) → false: yetki kararında kanıt yoksa güven yok.
func nginxtenMiGizli(r *http.Request, gizli string) bool {
	if !yerelVekil(hostOnly(r.RemoteAddr)) {
		return false
	}
	return gizli != "" && gizliVekilBasligi(r, gizli)
}
