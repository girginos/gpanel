// Package dnsutil — SSL/LE için DNS çözümlemesini YEREL (önbellekli) çözümleyici
// yerine KAMU DNS'ten yapar ve gerektiğinde yerel çözümleyici önbelleğini temizler.
//
// 🔴 NEDEN (kalıcı fix): SSL kapsam kontrolü (ssl_kapsam.go) ve mail sertifikası
// ön-doğrulaması (mailssl.go) `net.LookupHost` ile SİSTEM çözümleyicisini
// (/etc/resolv.conf → 127.0.0.53 → unbound/systemd-resolved/named) kullanıyordu.
// Bir domain bu sunucuya taşındığında yerel çözümleyici ESKİ IP'yi önbellekte
// tutabiliyor: panel "alan bu sunucuya çözülmüyor" sanıp Let's Encrypt'i HİÇ
// istemiyor — oysa KAMU DNS (Let's Encrypt'in HTTP-01'de göreceği kayıt) DOĞRU.
// Kamu DNS'ten çözmek LE'nin gerçekte göreceğini yansıtır ve bayat-önbellek
// tuzağını KALICI kapatır. (Canlıda ölçüldü: .180/gpanel.dnshosting.me,
// unbound mail.girginos.io'yu eski Plesk IP'sine çözüyordu; kamu DNS doğruydu.)
package dnsutil

import (
	"context"
	"net"
	"os/exec"
	"strings"
	"time"
)

// kamuNS — sırayla denenecek genel çözümleyiciler (Cloudflare, Google, Quad9).
var kamuNS = []string{"1.1.1.1", "8.8.8.8", "9.9.9.9"}

// Cozumle — host'u ÖNCE kamu DNS'ten çözer (LE'nin göreceği kayıt); hepsi
// ulaşılamazsa son çare sistem çözümleyicisine düşer. IPv4+IPv6 döner, boşsa nil.
//
// 🔴 NEDEN `dig` (net.Resolver DEĞİL): Go'nun net.Resolver.LookupHost'u —
// PreferGo + özel Dial dahil — DNS sunucusuna sormadan ÖNCE /etc/hosts'u okur.
// Sistem hostname'i bir alan adıysa OS oraya `127.0.0.1 <hostname>` yazar; özel
// Dial'a rağmen 127.0.0.1 dönerdi ve panel "bu sunucuya çözülmüyor" derdi (yeni
// sunucuda ölçüldü). `dig` /etc/hosts'u OKUMAZ, doğrudan kamu NS'e sorar.
func Cozumle(host string) []string {
	if out := digCoz(host); len(out) > 0 {
		return out
	}
	// dig yok / kamu NS ulaşılamadı → son çare sistem çözümleyici (imperfect —
	// /etc/hosts'u okur, ama dig'siz kutuda hiç yoktan iyidir).
	ctx, iptal := context.WithTimeout(context.Background(), 3*time.Second)
	defer iptal()
	if ips, err := net.DefaultResolver.LookupHost(ctx, host); err == nil {
		return ips
	}
	return nil
}

// digCoz — `dig` ile kamu NS'lerden A+AAAA çeker (ilk cevap veren NS kazanır).
// dig yoksa nil. /etc/hosts'u ATLAR.
func digCoz(host string) []string {
	if _, err := exec.LookPath("dig"); err != nil {
		return nil
	}
	for _, ns := range kamuNS {
		var out []string
		seen := map[string]bool{}
		for _, tip := range []string{"A", "AAAA"} {
			ctx, iptal := context.WithTimeout(context.Background(), 4*time.Second)
			b, _ := exec.CommandContext(ctx, "dig", "+short", "+time=3", "+tries=1",
				"@"+ns, tip, host).Output()
			iptal()
			for _, satir := range strings.Fields(string(b)) {
				if net.ParseIP(satir) != nil && !seen[satir] {
					seen[satir] = true
					out = append(out, satir)
				}
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

// BuSunucuya — host, verilen sunucu IP'lerinden BİRİNE (kamu DNS'e göre)
// çözülüyor mu? sunucuIP boşsa false.
func BuSunucuya(host string, sunucuIP ...string) bool {
	if host == "" {
		return false
	}
	set := map[string]bool{}
	for _, s := range sunucuIP {
		if s != "" {
			set[s] = true
		}
	}
	if len(set) == 0 {
		return false
	}
	for _, ip := range Cozumle(host) {
		if set[ip] {
			return true
		}
	}
	return false
}

// OnbellekTemizle — yerel çözümleyici önbelleğini EN İYİ ÇABAYLA temizler
// (unbound / named / systemd-resolved / nscd). Bir domaini bu sunucuya
// taşıdıktan sonra eski IP'nin önbellekte kalıp mail daemon'a/oto-yapılandırmaya
// yanlış IP vermesini önler. Kurulu olmayan araçlar sessizce atlanır; hata yutulur.
func OnbellekTemizle(domain string) {
	kos := func(ad string, arg ...string) {
		ctx, iptal := context.WithTimeout(context.Background(), 5*time.Second)
		defer iptal()
		_ = exec.CommandContext(ctx, ad, arg...).Run()
	}
	if domain != "" {
		kos("unbound-control", "flush_zone", domain)
		kos("rndc", "flushname", domain)
	}
	kos("rndc", "flush")
	kos("resolvectl", "flush-caches")
	kos("nscd", "-i", "hosts")
}
