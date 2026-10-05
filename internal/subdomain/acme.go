package subdomain

// acme.go — alt alan vhost'unun ACME (HTTP-01) challenge kalıbı: TEK tanım,
// eski kalıbın onarımı, SSL öncesi ön doğrulama ve hata sınıflandırma.
//
// ADR (alt alan Let's Encrypt 404, 2026-10-05)
//
// Sorun: SSLKur acme.sh'ı root-sahipli /var/www/acme kökü ile çalıştırır; ama
// Olustur eski `vhost()` şablonuyla challenge'ı kiracı docroot'undan sunan
// vhost yazıyordu → token 404. (rebuildVhostDB/renderSubVhost zaten doğruydu;
// yani yalnız İLK yazım yolu eskiydi, vhost bir kez yeniden kurulunca düzelirdi.)
//
// Kararlar:
//  1. Tek üretici: Olustur artık renderSubVhost'tan geçer (vhost() ince sarmalayıcı).
//     Challenge location'ı acmeLocation() ile yalnız orada üretilir.
//  2. Onarım (açılış, HealAltAlanAcmeKalibi): yalnız location bloğu METİNSEL olarak
//     değiştirilir (tam yeniden render DEĞİL) — kiracının ayarlarıyla yapılmış
//     diğer her şey bayt bayt kalır. Yalnız root'u alt alanın docroot'u olan blok
//     "eski" sayılır; tanımadığımız her şeye DOKUNULMAZ, günlüğe yazılır.
//     nginx -t başarısızsa dosya geri yazılır; idempotent.
//  3. Ön doğrulama (SSLKur): acme.sh çağrılmadan önce vhost kontrol edilir; eski
//     blok → aynı metinsel onarım; blok yok/tanınmıyor/conf yok → rebuildVhost
//     (tek üretici). Böylece panel güncellenmeden/heal koşmadan da SSL çalışır.
//  4. Hata mesajı: acme.sh çıktısı istemciye ham dönmez (yönetici dahil); günlüğe
//     "ref" ile yazılır, istemciye sınıfa göre sade mesaj + ref gider.

import (
	"context"
	"database/sql"
	"fmt"
	"hash/crc32"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"girginospanel/internal/provisioner"
)

// acmeLocation — challenge location satırı (girintisiz, \n'siz). YALNIZ token
// biçimindeki adlara açık; kök root-sahipli; kısıtlamadan muaf.
func acmeLocation() string {
	return `location ~ "^/\.well-known/acme-challenge/[A-Za-z0-9_-]{16,128}$" { root ` + acmeKok() +
		`; auth_basic off; allow all; try_files $uri =404; }`
}

// Challenge durumları.
const (
	acmeDogru      = "dogru"      // tüm challenge blokları root-sahipli ACME kökünde
	acmeEski       = "eski"       // en az bir blok kiracı docroot'undan sunuyor (onarılabilir)
	acmeYok        = "yok"        // dosya yok ya da challenge bloğu yok
	acmeBilinmiyor = "bilinmiyor" // tanınmayan kök/biçim — dokunulmaz
	acmeDuzeltildi = "duzeltildi"
)

var reRootDirektif = regexp.MustCompile(`(?:^|[\s;{])root\s+([^\s;]+)\s*;`)

type acmeBlok struct{ bas, son int }

// acmeBloklari — `location … acme-challenge …` bloklarını (süslü parantez
// dengelenerek; çift tırnak içindekiler sayılmaz) satır başından kapanışın satır
// sonuna kadar döner.
func acmeBloklari(govde string) []acmeBlok {
	var out []acmeBlok
	i := 0
	for i < len(govde) {
		j := strings.IndexByte(govde[i:], '\n')
		var satir string
		next := len(govde)
		if j < 0 {
			satir = govde[i:]
		} else {
			satir = govde[i : i+j]
			next = i + j + 1
		}
		t := strings.TrimSpace(satir)
		if strings.HasPrefix(t, "location") && strings.Contains(t, "acme-challenge") {
			derin, basladi, k, tirnak := 0, false, i, false
			for ; k < len(govde); k++ {
				if govde[k] == '"' {
					tirnak = !tirnak // "…{16,128}…" regex'indeki süslüler sayılmaz
				}
				if tirnak {
					continue
				}
				if govde[k] == '{' {
					derin++
					basladi = true
				} else if govde[k] == '}' {
					derin--
				}
				if basladi && derin == 0 {
					break
				}
			}
			if basladi && derin == 0 {
				son := k + 1
				if e := strings.IndexByte(govde[son:], '\n'); e >= 0 && strings.TrimSpace(govde[son:son+e]) == "" {
					son += e + 1
				}
				out = append(out, acmeBlok{i, son})
				i = son
				continue
			}
		}
		i = next
	}
	return out
}

// acmeSinifla — vhost gövdesindeki challenge durumu. docroot: alt alanın
// kiracı docroot'u (eski kalıbın kökü).
func acmeSinifla(govde, docroot string) string {
	bl := acmeBloklari(govde)
	if len(bl) == 0 {
		return acmeYok
	}
	kok := acmeKok()
	eski := false
	for _, b := range bl {
		m := reRootDirektif.FindStringSubmatch(govde[b.bas:b.son])
		switch {
		case m == nil:
			return acmeBilinmiyor
		case m[1] == kok:
		case m[1] == docroot:
			eski = true
		default:
			return acmeBilinmiyor
		}
	}
	if eski {
		return acmeEski
	}
	return acmeDogru
}

// acmeDuzelt — eski blokları yeni kalıpla değiştirir (yalnız acmeEski için).
func acmeDuzelt(govde, docroot string) string {
	bl := acmeBloklari(govde)
	var b strings.Builder
	onceki := 0
	for _, x := range bl {
		m := reRootDirektif.FindStringSubmatch(govde[x.bas:x.son])
		if m == nil || m[1] != docroot {
			continue
		}
		b.WriteString(govde[onceki:x.bas])
		blok := govde[x.bas:x.son]
		girinti := blok[:len(blok)-len(strings.TrimLeft(blok, " \t"))]
		b.WriteString(girinti + acmeLocation() + "\n")
		onceki = x.son
	}
	b.WriteString(govde[onceki:])
	return b.String()
}

// vhostAcmeDuzelt — conf'taki eski challenge bloğunu yeni kalıba çevirir.
// Render kilidi altında; nginx -t / reload başarısızsa eski içerik geri yazılır.
// Dönüş: sınıf (acmeDuzeltildi dahil). Dosya yoksa acmeYok.
func vhostAcmeDuzelt(conf, docroot string) (string, error) {
	birak := provisioner.RenderKilidiAl()
	defer birak()
	eski, err := os.ReadFile(conf)
	if err != nil {
		return acmeYok, nil
	}
	durum := acmeSinifla(string(eski), docroot)
	if durum != acmeEski {
		return durum, nil
	}
	yeni := acmeDuzelt(string(eski), docroot)
	if err := os.WriteFile(conf, []byte(yeni), 0o644); err != nil {
		return durum, err
	}
	_, _ = komut("restorecon", conf)
	geri := func() { _ = os.WriteFile(conf, eski, 0o644) }
	if out, e := komut("nginx", "-t"); e != nil {
		geri()
		return durum, &nginxHata{strings.TrimSpace(string(out))}
	}
	if out, e := komut("systemctl", "reload", "nginx"); e != nil {
		geri()
		return durum, fmt.Errorf("nginx reload BASARISIZ, %s GERI ALINDI: %s", conf, strings.TrimSpace(string(out)))
	}
	return acmeDuzeltildi, nil
}

// acmeVhostHazir — SSL öncesi ön doğrulama (ADR madde 3).
func (h *Handlers) acmeVhostHazir(ctx context.Context, sid int64, sk, altAd, tamAd, phpSurum string) error {
	durum, err := vhostAcmeDuzelt(confPath(sk, altAd), docrootOf(sk, tamAd))
	if err != nil {
		return err
	}
	if durum == acmeDogru || durum == acmeDuzeltildi {
		return nil
	}
	return h.rebuildVhost(ctx, sid, sk, altAd, tamAd, phpSurum)
}

// AcmeSonuc — HealAltAlanAcmeKalibi özeti (günlük + test).
type AcmeSonuc struct {
	Dogru, Duzeltilen, Bilinmiyor, Yok, Hata int
	BilinmeyenAdlar                          []string
}

// HealAltAlanAcmeKalibi — açılışta: panelin ürettiği sub_*.conf dosyalarındaki
// eski challenge kalıbını onarır (ADR madde 2). Askıdaki hesaplar atlanır
// (askı vhost'u zaten ACME kökünde). Boot'u bloklamaz, idempotenttir.
func HealAltAlanAcmeKalibi(db *sql.DB) AcmeSonuc {
	var s AcmeSonuc
	if db == nil {
		return s
	}
	rows, err := db.Query(`SELECT s.alt_ad, s.tam_ad, d.sistem_kullanici
		FROM subdomanlar s JOIN domains d ON d.id = s.domain_id
		WHERE COALESCE(d.askida,0)=0`)
	if err != nil {
		return s
	}
	type alt struct{ altAd, tamAd, sk string }
	var liste []alt
	for rows.Next() {
		var a alt
		if rows.Scan(&a.altAd, &a.tamAd, &a.sk) == nil {
			liste = append(liste, a)
		}
	}
	rows.Close()
	for _, a := range liste {
		d, e := vhostAcmeDuzelt(confPath(a.sk, a.altAd), docrootOf(a.sk, a.tamAd))
		switch {
		case e != nil:
			s.Hata++
			log.Printf("🔴 alt alan ACME kalıbı onarılamadı (%s): %v — conf geri alındı", a.tamAd, e)
		case d == acmeDuzeltildi:
			s.Duzeltilen++
			log.Printf("✓ alt alan ACME challenge kalıbı onarıldı: %s", a.tamAd)
		case d == acmeDogru:
			s.Dogru++
		case d == acmeBilinmiyor:
			s.Bilinmiyor++
			s.BilinmeyenAdlar = append(s.BilinmeyenAdlar, a.tamAd)
			log.Printf("alt alan ACME kalıbı TANINMIYOR, dokunulmadı: %s", a.tamAd)
		default:
			s.Yok++
		}
	}
	if s.Duzeltilen+s.Bilinmiyor+s.Hata > 0 {
		log.Printf("alt alan ACME onarımı: düzeltilen=%d bilinmeyen=%d hata=%d doğru=%d", s.Duzeltilen, s.Bilinmiyor, s.Hata, s.Dogru)
	}
	return s
}

// ── acme.sh hata sınıflandırma ──

const (
	acmeSinifLimit     = "limit"
	acmeSinifDNS       = "dns"
	acmeSinifBaglanti  = "baglanti"
	acmeSinifChallenge = "challenge"
	acmeSinifGenel     = "genel"
)

// acmeHataSinifla — acme.sh çıktısını sınıf + sade kullanıcı mesajına çevirir.
// Sıra önemli: limit ve DNS, "invalid response" benzeri genel kalıplardan önce.
func acmeHataSinifla(out string) (sinif, mesaj string) {
	k := strings.ToLower(out)
	has := func(s ...string) bool {
		for _, x := range s {
			if strings.Contains(k, x) {
				return true
			}
		}
		return false
	}
	switch {
	case has("too many certificates", "too many failed authorizations", "ratelimited", "rate limit", "toomanyrequests"):
		return acmeSinifLimit, "Let's Encrypt istek sınırına ulaşıldı; bir süre sonra (genellikle 1 saat–1 hafta) tekrar deneyin"
	case has("nxdomain", "dns problem", "no valid ip addresses", "could not resolve", "no such host", "servfail", "looking up a"):
		return acmeSinifDNS, "Alan adı çözülemedi: alt alanın DNS A kaydı bu sunucuya yönlendirilmiş olmalı (yayılması zaman alabilir)"
	case has("timeout during connect", "connection refused", "connection timed out", "firewall problem"):
		return acmeSinifBaglanti, "Let's Encrypt sunucuya 80 numaralı porttan bağlanamadı: DNS kaydını ve güvenlik duvarını kontrol edin"
	case has("invalid response from http", "unauthorized", "acme-challenge"):
		return acmeSinifChallenge, "Doğrulama dosyasına erişilemedi (sunucu yapılandırması); yönetici uyarıldı, birkaç dakika sonra tekrar deneyin"
	}
	return acmeSinifGenel, "Let's Encrypt sertifikası alınamadı"
}

// acmeHataMesaji — sınıflar, ayrıntıyı ref ile günlüğe yazar; istemciye ham
// çıktı DÖNMEZ. kaynak/domainID günlük içindir.
func acmeHataMesaji(kaynak string, domainID int64, tamAd string, out []byte) string {
	ham := strings.TrimSpace(string(out))
	sinif, msg := acmeHataSinifla(ham)
	ref := fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte(fmt.Sprintf("%s|%d|%d", tamAd, domainID, time.Now().UnixNano()))))
	log.Printf("%s: Let's Encrypt başarısız ref=%s sinif=%s ad=%s domain=%d ayrıntı: %s", kaynak, ref, sinif, tamAd, domainID, sonSatirlar(ham, 15))
	return "Let's Encrypt alınamadı: " + msg + " (ref: " + ref + ")"
}

func sonSatirlar(s string, n int) string {
	l := strings.Split(s, "\n")
	if len(l) > n {
		l = l[len(l)-n:]
	}
	return strings.Join(l, " | ")
}
