package provisioner

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// SSL yeniden-cekim teardown fix'i (Let's Encrypt rate-limit dayanikligi)
//
// KOK PROBLEM: acme.sh cert-cekimi 429 (LE rate-limit: "too many certificates
// already issued for this exact set of identifiers in the last 168h") alinca panel
// bunu "tam basarisizlik" sayip elindeki GECERLI cert'i yok sayiyor, vhost'u HTTP-only
// yeniden yaziyor → origin'de 443 dinleyen kalmiyor → Cloudflare Full/strict altinda
// 522/525 → site KOMPLE dusuyordu. Oysa gecerli cert acme store'da (/root/.acme.sh) ve/
// veya /etc/pki/girginospanel'de 90 gun GECERLI DURUYOR.
//
// COZUM (3 parca, hepsi installer+update yapisinda startup-heal ile):
//  1. Reuse-before-issue: gecerli cert varsa yeni cekim YAPMA, onu deploy et (429'a hic girmez).
//  2. Fail-safe: cekim basarisizsa (429 dahil) mevcut/self-signed cert ile 443'u KORU —
//     HICBIR durumda vhost'u HTTP-only'ye dusurme.
//  3. Startup-heal: her acilista SSL-etkin domain'lerin 443 blogu + cert'ini onar.
// ---------------------------------------------------------------------------

// acmeStoreCandidates: acme.sh'in bir domain icin cert sakladigi (fullchain, key) aday
// ciftleri. RSA cekiminde "<domain>", ECC cekiminde "<domain>_ecc" dizini kullanilir;
// her ikisini de aday olarak dondururuz.
func acmeStoreCandidates(domain string) [][2]string {
	base := "/root/.acme.sh"
	var out [][2]string
	for _, d := range []string{
		filepath.Join(base, domain),
		filepath.Join(base, domain+"_ecc"),
	} {
		out = append(out, [2]string{
			filepath.Join(d, "fullchain.cer"),
			filepath.Join(d, domain+".key"),
		})
	}
	return out
}

// certDosyaVar: dosya var mi (cert/key mevcudiyet kontrolu).
func certDosyaVar(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

// leafOku: cert+key PEM ciftini yukler. Key, cert'in public key'iyle ESLESMELI
// (tls.LoadX509KeyPair bunu dogrular → yanlis key ile hic yuklenmez). Fullchain ise
// ilk (leaf) sertifikayi doner.
func leafOku(certPath, keyPath string) (*x509.Certificate, bool) {
	if !certDosyaVar(certPath) || !certDosyaVar(keyPath) {
		return nil, false
	}
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, false // key eslesmiyor veya parse hatasi
	}
	leaf := pair.Leaf
	if leaf == nil && len(pair.Certificate) > 0 {
		leaf, _ = x509.ParseCertificate(pair.Certificate[0])
	}
	if leaf == nil {
		return nil, false
	}
	return leaf, true
}

// certKapsar: leaf sertifika verilen host'u SAN (DNSNames) veya CN ile kapsiyor mu.
func certKapsar(leaf *x509.Certificate, host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	for _, n := range leaf.DNSNames {
		if strings.EqualFold(n, host) {
			return true
		}
	}
	return strings.EqualFold(leaf.Subject.CommonName, host)
}

// certGecerliMi: cert+key gecerli mi — key eslesir, notAfter > now+minGun, ve verilen
// TUM host'lari (ornek: domain + www.domain) kapsar.
func certGecerliMi(certPath, keyPath string, minGun int, hostlar ...string) bool {
	leaf, ok := leafOku(certPath, keyPath)
	if !ok {
		return false
	}
	if time.Now().Add(time.Duration(minGun) * 24 * time.Hour).After(leaf.NotAfter) {
		return false
	}
	for _, h := range hostlar {
		if !certKapsar(leaf, h) {
			return false
		}
	}
	return true
}

// enIyiCertBul: bir domain icin en iyi GECERLI cert'i (key eslesen, minGun gecerli,
// domain+www kapsayan) acme.sh store + /etc/pki adaylari arasindan secer. Oncelik:
// gercek CA (Let's Encrypt) > self-signed; ayni sinifta daha gec notAfter kazanir.
// gercekCA doner = secilen cert gercek CA imzali mi (self-signed degil).
func enIyiCertBul(domain string, minGun int) (certPath, keyPath string, gercekCA bool) {
	type aday struct{ cert, key string }
	var adaylar []aday
	for _, c := range acmeStoreCandidates(domain) {
		adaylar = append(adaylar, aday{c[0], c[1]})
	}
	sysDir := certSystemDir(domain)
	adaylar = append(adaylar, aday{
		filepath.Join(sysDir, domain+".crt"),
		filepath.Join(sysDir, domain+".key"),
	})

	var bestCert, bestKey string
	var bestReal bool
	var bestNotAfter time.Time
	for _, a := range adaylar {
		if !certGecerliMi(a.cert, a.key, minGun, domain, "www."+domain) {
			continue
		}
		leaf, ok := leafOku(a.cert, a.key)
		if !ok {
			continue
		}
		real := leaf.Issuer.String() != leaf.Subject.String() // self-signed'da Issuer==Subject
		// Secim: gercek CA self-signed'i DAIMA yener; ayni sinifta gec notAfter kazanir.
		if bestCert == "" ||
			(real && !bestReal) ||
			(real == bestReal && leaf.NotAfter.After(bestNotAfter)) {
			bestCert, bestKey, bestReal, bestNotAfter = a.cert, a.key, real, leaf.NotAfter
		}
	}
	return bestCert, bestKey, bestReal
}

// certiPkiyeKur: srcCert/srcKey'i /etc/pki/girginospanel/<domain>/'e kopyalar (cert 0644,
// key 0600, root-owned, restorecon → cert_t). Kaynak zaten hedefse yalniz izin/baglam
// uygular (idempotent). Doner: hedef cert/key yolu.
func certiPkiyeKur(domain, srcCert, srcKey string) (certPath, keyPath string, err error) {
	sslDir := certSystemDir(domain)
	if e := os.MkdirAll(sslDir, 0755); e != nil {
		return "", "", fmt.Errorf("pki dizin: %w", e)
	}
	certPath = filepath.Join(sslDir, domain+".crt")
	keyPath = filepath.Join(sslDir, domain+".key")
	if srcCert != certPath {
		if !copyFile(srcCert, certPath, 0644) {
			return "", "", fmt.Errorf("cert kopyalanamadi: %s", srcCert)
		}
	}
	if srcKey != keyPath {
		if !copyFile(srcKey, keyPath, 0600) {
			return "", "", fmt.Errorf("key kopyalanamadi: %s", srcKey)
		}
	}
	yazCertKurulumu(sslDir, certPath, keyPath)
	return certPath, keyPath, nil
}

// selfSignedUret: /etc/pki/girginospanel/<domain>/'e 1 yillik self-signed cert uretir
// (SAN: domain + www.domain). Vhost RENDER ETMEZ — cagiran sslVhostYaz ile baglar.
func selfSignedUret(domain string) (certPath, keyPath string, err error) {
	if verr := ValidateDomain(domain); verr != nil {
		return "", "", verr // path guvenligi (/ veya .. yok)
	}
	sslDir := certSystemDir(domain)
	if e := os.MkdirAll(sslDir, 0755); e != nil {
		return "", "", fmt.Errorf("pki dizin: %w", e)
	}
	certPath = filepath.Join(sslDir, domain+".crt")
	keyPath = filepath.Join(sslDir, domain+".key")
	subj := fmt.Sprintf("/C=TR/ST=Local/L=GirginOSPanel/O=%s/CN=%s", domain, domain)
	args := []string{
		"req", "-x509", "-nodes",
		"-newkey", "rsa:2048",
		"-keyout", keyPath,
		"-out", certPath,
		"-days", "365",
		"-subj", subj,
		"-addext", "subjectAltName=DNS:" + domain + ",DNS:www." + domain,
	}
	if out, e := exec.Command("openssl", args...).CombinedOutput(); e != nil {
		return "", "", fmt.Errorf("openssl: %s: %w", strings.TrimSpace(string(out)), e)
	}
	yazCertKurulumu(sslDir, certPath, keyPath)
	return certPath, keyPath, nil
}

// sslVhostYaz: verilen cert/key ile domain'in vhost'unu SSL-li render eder (443 blogu
// dahil). renderAndReload nginx -t + rollback fail-safe'i icerir (bozuk config diske
// kalmaz). Askiya-alma/per-tenant FPM tutarliligi renderAndReload icinde saglanir.
func sslVhostYaz(alanAdi, sk, phpSurum, backend, certPath, keyPath, kaynak string) error {
	_, socket, _ := phpPoolPath(sk, phpSurum)
	home := "/home/" + sk
	return renderAndReload(VhostOpts{
		AlanAdi:   alanAdi,
		WebRoot:   filepath.Join(home, "public_html"),
		PHPSocket: socket,
		PHPSurum:  phpSurum,
		CertPath:  certPath,
		KeyPath:   keyPath,
		SSLKaynak: kaynak,
		Backend:   backend,
	}, sk)
}

// sslFailSafe: LE cekimi basarisiz oldugunda (429 dahil) 443'u KORUR — HICBIR zaman
// HTTP-only'ye dusmez. Gecerli cert (acme store veya /etc/pki, not-expired, domain+www,
// key eslesen) varsa onu deploy eder; hic yoksa self-signed uretir. Her iki halde de
// vhost SSL-li (443 dinler) render edilir.
func sslFailSafe(alanAdi, sk, phpSurum, backend, sebep string) (certPath, keyPath string, err error) {
	if src, srcKey, real := enIyiCertBul(alanAdi, 0); src != "" {
		if cp, kp, e := certiPkiyeKur(alanAdi, src, srcKey); e == nil {
			kaynak := "self-signed"
			if real {
				kaynak = "letsencrypt"
			}
			if e := sslVhostYaz(alanAdi, sk, phpSurum, backend, cp, kp, kaynak); e != nil {
				return "", "", e
			}
			log.Printf("ssl fail-safe: %s LE cekimi basarisiz (%s) — mevcut %s cert ile 443 KORUNDU", alanAdi, sebep, kaynak)
			return cp, kp, nil
		}
	}
	// Hic gecerli cert yok → self-signed fallback (443 yine dinler, teardown YOK).
	cp, kp, e := selfSignedUret(alanAdi)
	if e != nil {
		return "", "", fmt.Errorf("%s + self-signed fallback: %w", sebep, e)
	}
	if e := sslVhostYaz(alanAdi, sk, phpSurum, backend, cp, kp, "self-signed"); e != nil {
		return "", "", e
	}
	log.Printf("ssl fail-safe: %s LE cekimi basarisiz (%s) — self-signed uretildi, 443 KORUNDU", alanAdi, sebep)
	return cp, kp, nil
}

// HealSSLVhost443OnStartup: SSL-etkin (ssl_aktif=1) her domain'in vhost'unda 443 blogu
// + isaret ettigi cert dosyalari mevcut mu dogrular. Degilse: en iyi cert'i (LE > self-
// signed; hic yoksa taze self-signed) /etc/pki'ye koyar, DB cert_path/key_path'i repoint
// eder ve vhost'u SSL-li yeniden render eder (renderAndReload icinde nginx -t + rollback
// → fail-closed: patlarsa eski hal korunur). Idempotent — saglikli domain'e DOKUNMAZ.
// Init'ten her boot cagrilir → mevcut bozuk kurulumlar (443 dusmus / cert silinmis) ilk
// update+restart'ta otomatik onarilir.
func HealSSLVhost443OnStartup() {
	if pkgDB == nil {
		return
	}
	rows, err := pkgDB.Query(`SELECT id, alan_adi, sistem_kullanici, COALESCE(php_surum,'8.3'), COALESCE(cert_path,''), COALESCE(key_path,'')
		FROM domains WHERE ssl_aktif=1`)
	if err != nil {
		return
	}
	type dom struct {
		id                          int64
		alanAdi, sk, php, cert, key string
	}
	var list []dom
	for rows.Next() {
		var x dom
		if e := rows.Scan(&x.id, &x.alanAdi, &x.sk, &x.php, &x.cert, &x.key); e == nil {
			list = append(list, x)
		}
	}
	rows.Close()
	if len(list) == 0 {
		return
	}
	var onar, hata, saglikli int
	for _, x := range list {
		if x.alanAdi == "" || ValidateDomain(x.alanAdi) != nil {
			continue // path guvenligi
		}
		vpath := "/etc/nginx/conf.d/dom_" + x.sk + ".conf"
		data, rerr := os.ReadFile(vpath)
		has443 := rerr == nil && strings.Contains(string(data), "listen 443")
		certVar := certDosyaVar(x.cert) && certDosyaVar(x.key)
		if has443 && certVar {
			saglikli++
			continue // saglikli — dokunma
		}

		useCert, useKey := x.cert, x.key
		// Cert dosyalari eksikse en iyi cert'i bul/uret ve /etc/pki'ye kur.
		if !certVar {
			src, srcKey, _ := enIyiCertBul(x.alanAdi, 0)
			if src == "" {
				cp, kp, e := selfSignedUret(x.alanAdi)
				if e != nil {
					log.Printf("ssl 443 heal: %s cert yok + self-signed uretilemedi: %v", x.alanAdi, e)
					hata++
					continue
				}
				src, srcKey = cp, kp
			}
			cp, kp, e := certiPkiyeKur(x.alanAdi, src, srcKey)
			if e != nil {
				log.Printf("ssl 443 heal: %s cert /etc/pki'ye kurulamadi: %v", x.alanAdi, e)
				hata++
				continue
			}
			useCert, useKey = cp, kp
		}
		// DB repoint (degistiyse) — ApplyVhostForDomain cert'i DB'den okur.
		if useCert != x.cert || useKey != x.key {
			if _, e := pkgDB.Exec(`UPDATE domains SET cert_path=?, key_path=? WHERE id=?`, useCert, useKey, x.id); e != nil {
				log.Printf("ssl 443 heal: %s DB repoint hata: %v", x.alanAdi, e)
				hata++
				continue
			}
		}
		socket, _ := PHPSocketFor(x.sk, x.php)
		if e := ApplyVhostForDomain(pkgDB, x.id, socket, x.php); e != nil {
			log.Printf("ssl 443 heal: %s vhost 443 re-render hata (eski hali korundu): %v", x.alanAdi, e)
			hata++
			continue
		}
		onar++
		log.Printf("ssl 443 heal: %s 443 blogu + cert onarildi", x.alanAdi)
	}
	if onar > 0 || hata > 0 {
		log.Printf("ssl 443 heal: %d onarildi / %d hata / %d saglikli (toplam %d SSL domain)", onar, hata, saglikli, len(list))
	}
}
