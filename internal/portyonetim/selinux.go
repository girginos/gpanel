package portyonetim

// SELinux port etiketleri — rol kapıları (ADR-GP-001).
//
// 🔴 nginx (httpd_t) yalnız http_port_t etiketli portlara bind edebilir; rastgele kapı
// portları (20000-32767) varsayılan politikada unreserved_port_t'dir. Enforcing sunucuda
// etiketsiz bir porta yazılan listen'da reload SESSİZCE düşer (systemctl reload yalnız
// sinyal gönderir ve 0 döner), sonraki nginx restart'ı ise HİÇ açılmaz → tüm siteler
// kapanır. `nginx -t` bunu yakalamaz (soket açmaz; panelden unconfined bağlamda koşar).
// Etiketleme provisioner'da listen yazılmadan ÖNCE yapılır; burada seçim sırasında başka
// bir servise ayrılmış portlar elenir.

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// SELinuxModu: "" (kapalı / yok) | "permissive" | "enforcing".
func SELinuxModu() string {
	b, err := os.ReadFile("/sys/fs/selinux/enforce")
	if err != nil {
		return ""
	}
	if strings.TrimSpace(string(b)) == "1" {
		return "enforcing"
	}
	return "permissive"
}

// SELinuxPortListesi: `semanage port -l` çıktısı (politika + yerel özelleştirmeler).
func SELinuxPortListesi() (string, error) {
	out, err := exec.Command("semanage", "port", "-l").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("semanage port -l: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// SELinuxHttpEtiketle: tcp portunu http_port_t olarak ekler (yerel özelleştirme).
func SELinuxHttpEtiketle(p int) error {
	out, err := exec.Command("semanage", "port", "-a", "-t", "http_port_t", "-p", "tcp", strconv.Itoa(p)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("semanage port -a %d: %v: %s", p, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// genelPortTurleri: geniş aralıklarla tanımlı, hiçbir servise AYRILMAMIŞ port türleri —
// bunlara düşen port http_port_t olarak eklenebilir.
var genelPortTurleri = map[string]bool{
	"unreserved_port_t":  true,
	"reserved_port_t":    true,
	"hi_reserved_port_t": true,
	"ephemeral_port_t":   true,
	"port_t":             true,
}

// SELinuxPortDurumu: `semanage port -l` çıktısına göre tcp portunun durumu. http: port
// zaten http_port_t; baska: portun ayrıldığı başka bir servis türü ("" = yok).
func SELinuxPortDurumu(liste string, p int) (http bool, baska string) {
	for _, ln := range strings.Split(liste, "\n") {
		f := strings.Fields(ln)
		if len(f) < 3 || f[1] != "tcp" {
			continue
		}
		for _, parca := range strings.Split(strings.Join(f[2:], ""), ",") {
			alt, ust, ok := portAraligi(parca)
			if !ok || p < alt || p > ust {
				continue
			}
			switch {
			case f[0] == "http_port_t":
				http = true
			case !genelPortTurleri[f[0]] && baska == "":
				baska = f[0]
			}
		}
	}
	return http, baska
}

// portAraligi: "8443" | "1024-32767" → alt, üst.
func portAraligi(s string) (int, int, bool) {
	a, u := s, s
	if i := strings.IndexByte(s, '-'); i > 0 {
		a, u = s[:i], s[i+1:]
	}
	x, e1 := strconv.Atoi(a)
	y, e2 := strconv.Atoi(u)
	return x, y, e1 == nil && e2 == nil && x <= y
}

// selinuxDenetci: port seçiminde başka bir servise ayrılmış portu eleyen denetçi (tür adı
// döner, "" = uygun). SELinux kapalıysa ya da etiketler okunamıyorsa her portu geçirir:
// kesin karar, listen yazılmadan önceki etiketlemededir (provisioner).
func selinuxDenetci() func(int) string {
	if SELinuxModu() == "" {
		return func(int) string { return "" }
	}
	liste, err := SELinuxPortListesi()
	if err != nil {
		return func(int) string { return "" }
	}
	return func(p int) string {
		_, baska := SELinuxPortDurumu(liste, p)
		return baska
	}
}
