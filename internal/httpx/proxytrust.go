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
const ProxySecretPath = "/etc/girginospanel/proxy.secret"

var (
	proxyOnce sync.Once
	proxyVal  string
)

// ProxySecret: kalici gizliyi (varsa) doner; yoksa "". SALT-OKUR, uretmez —
// uretim+nginx-senkron provisioner Heal'inin isi (fail-safe siralama icin).
func ProxySecret() string {
	proxyOnce.Do(func() {
		if b, err := os.ReadFile(ProxySecretPath); err == nil {
			if t := strings.TrimSpace(string(b)); len(t) >= 32 {
				proxyVal = t
			}
		}
	})
	return proxyVal
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
