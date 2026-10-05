package subdomain

// acme_test.go — alt alan ACME challenge kalıbı: tek şablon, eski kalıbın
// onarımı (idempotent, bilinmeyene dokunmaz, nginx -t geri alma), SSL öncesi
// ön doğrulama ve hata sınıflandırma. Pure testler DB'siz; DB'li olanlar
// GOSP_TEST_GECICI_DSN yoksa atlanır. Canlı nginx/acme'ye dokunulmaz.

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	ac_regex = `location ~ "^/\.well-known/acme-challenge/[A-Za-z0-9_-]{16,128}$" {`
	ac_ad    = "test.girginos.cloud"
	ac_docr  = "/home/c_girginos/subdomains/test.girginos.cloud"
)

// ac_eski — Olustur'un eski şablonunun (181'de ölçülen) challenge bloğu.
func ac_eski(docroot string) string {
	return "server {\n    listen 80;\n    server_name " + ac_ad + ";\n    root " + docroot + ";\n\n" +
		"    location /.well-known/acme-challenge/ {\n        auth_basic off;\n        root " + docroot + ";\n        try_files $uri =404;\n    }\n\n" +
		"    location / { try_files $uri $uri/ /index.php?$query_string; }\n}\n"
}

func ac_kok(t *testing.T) string {
	t.Helper()
	e := acmeKoku
	t.Cleanup(func() { acmeKoku = e })
	acmeKoku = func() string { return "/var/www/acme" }
	return "/var/www/acme"
}

// ac_dikisler — komut kaydı + nginx -t / reload hatası seçimi; conf geçici dizinde.
func ac_dikisler(t *testing.T, nginxT, reload bool) (conf string, cagri *[]string) {
	t.Helper()
	ac_kok(t)
	e := komut
	t.Cleanup(func() { komut = e })
	cagri = &[]string{}
	komut = func(ad string, arg ...string) ([]byte, error) {
		*cagri = append(*cagri, ad+" "+strings.Join(arg, " "))
		if ad == "nginx" && nginxT {
			return []byte("nginx: [emerg] boom"), errors.New("exit 1")
		}
		if ad == "systemctl" && reload {
			return []byte("reload boom"), errors.New("exit 1")
		}
		return nil, nil
	}
	return filepath.Join(t.TempDir(), "sub_c_x_test.conf"), cagri
}

func ac_yeniKalip(t *testing.T, govde, docroot, neresi string) {
	t.Helper()
	if !strings.Contains(govde, ac_regex) {
		t.Errorf("%s: token-regex'li challenge location'ı yok:\n%s", neresi, govde)
	}
	if !strings.Contains(govde, "root "+acmeKok()+"; auth_basic off; allow all; try_files $uri =404; }") {
		t.Errorf("%s: challenge root-sahipli ACME kökü (%s) + auth_basic off + allow all olmalı", neresi, acmeKok())
	}
	if acmeSinifla(govde, docroot) != acmeDogru {
		t.Errorf("%s: sınıf dogru olmalı: %s", neresi, acmeSinifla(govde, docroot))
	}
	for _, b := range acmeBloklari(govde) {
		if strings.Contains(govde[b.bas:b.son], docroot) {
			t.Errorf("%s: challenge bloğu kiracı docroot'undan sunuyor", neresi)
		}
	}
}

// Şablon: yeni (vhost), SSL'siz/SSL'li, backend varyantları, erişim kısıtlı.
func TestAcmeSablon_TumUreticilerAyniKalip(t *testing.T) {
	ac_kok(t)
	kisit := "    allow 203.0.113.5;\n    deny all;\n"
	durumlar := map[string]string{
		"olustur-vhost": vhost(ac_ad, ac_docr, "/run/php-fpm/x.sock", ""),
		"http-php":      renderSubVhost(subVhostOpts{TamAd: ac_ad, DocRoot: ac_docr, Socket: "/s", Backend: "php-fpm", N: subNginxDefaults()}),
		"http-apache":   renderSubVhost(subVhostOpts{TamAd: ac_ad, DocRoot: ac_docr, Backend: "apache", N: subNginxDefaults()}),
		"http-static":   renderSubVhost(subVhostOpts{TamAd: ac_ad, DocRoot: ac_docr, Backend: "static", N: subNginxDefaults()}),
		"http-kisitli":  renderSubVhost(subVhostOpts{TamAd: ac_ad, DocRoot: ac_docr, Socket: "/s", Backend: "php-fpm", N: subNginxDefaults(), Kisit: kisit}),
		"ssl-php":       renderSubVhost(subVhostOpts{TamAd: ac_ad, DocRoot: ac_docr, Socket: "/s", Backend: "php-fpm", SSL: true, Crt: "/c", Key: "/k", N: subNginxDefaults()}),
		"ssl-kisitli":   renderSubVhost(subVhostOpts{TamAd: ac_ad, DocRoot: ac_docr, Socket: "/s", Backend: "php-fpm", SSL: true, Crt: "/c", Key: "/k", N: subNginxDefaults(), Kisit: kisit}),
	}
	for ad, g := range durumlar {
		ac_yeniKalip(t, g, ac_docr, ad)
	}
}

func TestAcmeSinifla_Tablo(t *testing.T) {
	ac_kok(t)
	yeni := vhost(ac_ad, ac_docr, "/s", "")
	tablo := []struct{ ad, govde, want string }{
		{"eski", ac_eski(ac_docr), acmeEski},
		{"yeni", yeni, acmeDogru},
		{"yok", "server { listen 80; }\n", acmeYok},
		{"bilinmeyen-kok", strings.ReplaceAll(ac_eski(ac_docr), "        root "+ac_docr+";\n", "        root /opt/elle;\n"), acmeBilinmiyor},
		{"rootsuz", "server {\n    location /.well-known/acme-challenge/ {\n        return 200;\n    }\n}\n", acmeBilinmiyor},
		{"karisik-eski+bilinmeyen", ac_eski(ac_docr) + "server {\n    location /.well-known/acme-challenge/ {\n        root /opt/elle;\n    }\n}\n", acmeBilinmiyor},
		{"baska-docroot", ac_eski("/home/baska/x"), acmeBilinmiyor},
	}
	for _, c := range tablo {
		if got := acmeSinifla(c.govde, ac_docr); got != c.want {
			t.Errorf("%s: %s beklenirdi, %s geldi", c.ad, c.want, got)
		}
	}
}

// Eski → yeni: yalnız blok değişir, geri kalan bayt bayt aynı; ikinci koşu no-op.
func TestVhostAcmeDuzelt_EskiYeniyeIdempotent(t *testing.T) {
	conf, cagri := ac_dikisler(t, false, false)
	eski := ac_eski(ac_docr)
	if err := os.WriteFile(conf, []byte(eski), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := vhostAcmeDuzelt(conf, ac_docr)
	if err != nil || d != acmeDuzeltildi {
		t.Fatalf("düzeltildi beklenirdi: %s %v", d, err)
	}
	b, _ := os.ReadFile(conf)
	ac_yeniKalip(t, string(b), ac_docr, "onarilmis")
	// blok dışı her şey aynı
	bl := acmeBloklari(eski)[0]
	if !strings.HasPrefix(string(b), eski[:bl.bas]) || !strings.HasSuffix(string(b), eski[bl.son:]) {
		t.Fatalf("blok dışı içerik değişti:\n%s", b)
	}
	if !strings.Contains(strings.Join(*cagri, "\n"), "nginx -t") || !strings.Contains(strings.Join(*cagri, "\n"), "systemctl reload nginx") {
		t.Fatalf("nginx -t + reload beklenirdi: %v", *cagri)
	}
	*cagri = nil
	d, err = vhostAcmeDuzelt(conf, ac_docr)
	b2, _ := os.ReadFile(conf)
	if err != nil || d != acmeDogru || string(b2) != string(b) || len(*cagri) != 0 {
		t.Fatalf("ikinci koşu no-op olmalı: %s %v çağrı=%v", d, err, *cagri)
	}
}

func TestVhostAcmeDuzelt_BilinmeyeneDokunmaz(t *testing.T) {
	conf, cagri := ac_dikisler(t, false, false)
	elle := strings.ReplaceAll(ac_eski(ac_docr), "        root "+ac_docr+";\n", "        root /opt/elle;\n")
	_ = os.WriteFile(conf, []byte(elle), 0o644)
	d, err := vhostAcmeDuzelt(conf, ac_docr)
	b, _ := os.ReadFile(conf)
	if err != nil || d != acmeBilinmiyor || string(b) != elle || len(*cagri) != 0 {
		t.Fatalf("bilinmeyen biçime dokunulmamalı: %s %v çağrı=%v", d, err, *cagri)
	}
	d, err = vhostAcmeDuzelt(filepath.Join(t.TempDir(), "yok.conf"), ac_docr)
	if err != nil || d != acmeYok {
		t.Fatalf("dosya yoksa acmeYok: %s %v", d, err)
	}
}

func TestVhostAcmeDuzelt_NginxTBasarisiz_GeriAlir(t *testing.T) {
	conf, _ := ac_dikisler(t, true, false)
	eski := ac_eski(ac_docr)
	_ = os.WriteFile(conf, []byte(eski), 0o644)
	_, err := vhostAcmeDuzelt(conf, ac_docr)
	b, _ := os.ReadFile(conf)
	if err == nil || string(b) != eski {
		t.Fatalf("nginx -t hatasında eski içerik geri yazılmalı (err=%v):\n%s", err, b)
	}
}

func TestVhostAcmeDuzelt_ReloadBasarisiz_GeriAlir(t *testing.T) {
	conf, _ := ac_dikisler(t, false, true)
	eski := ac_eski(ac_docr)
	_ = os.WriteFile(conf, []byte(eski), 0o644)
	_, err := vhostAcmeDuzelt(conf, ac_docr)
	b, _ := os.ReadFile(conf)
	if err == nil || string(b) != eski {
		t.Fatalf("reload hatasında eski içerik geri yazılmalı (err=%v)", err)
	}
}

func TestAcmeHataSinifla_Tablo(t *testing.T) {
	tablo := []struct{ ad, out, want string }{
		{"404", "Invalid response from http://test.girginos.cloud/.well-known/acme-challenge/abc: 404", acmeSinifChallenge},
		{"unauthorized", "urn:ietf:params:acme:error:unauthorized", acmeSinifChallenge},
		{"dns-nxdomain", "DNS problem: NXDOMAIN looking up A for x.test", acmeSinifDNS},
		{"dns-ip", "no valid IP addresses found for x.test", acmeSinifDNS},
		{"timeout", "Timeout during connect (likely firewall problem)", acmeSinifBaglanti},
		{"limit", "urn:ietf:params:acme:error:rateLimited: too many certificates", acmeSinifLimit},
		{"limit-ve-404", "rateLimited ... Invalid response from http://x: 404", acmeSinifLimit},
		{"genel", "Some strange error", acmeSinifGenel},
		{"bos", "", acmeSinifGenel},
	}
	for _, c := range tablo {
		got, msg := acmeHataSinifla(c.out)
		if got != c.want || msg == "" {
			t.Errorf("%s: %s beklenirdi, %s geldi", c.ad, c.want, got)
		}
	}
	// DNS mesajı 404'te çıkmamalı (yanıltıcı sabit metin sorunu)
	_, m := acmeHataSinifla("Invalid response from http://x/.well-known/acme-challenge/y: 404")
	if strings.Contains(m, "DNS") || !strings.Contains(m, "Doğrulama dosyasına erişilemedi") {
		t.Errorf("404 mesajı DNS'e yönlendirmemeli: %s", m)
	}
}

func TestAcmeHataMesaji_HamCiktiSizmaz(t *testing.T) {
	ham := []byte("Invalid response from http://x/.well-known/acme-challenge/tok: 404\nACCOUNT_THUMBPRINT='gizli123' /var/lib/girginospanel/acme")
	m := acmeHataMesaji("subdomain.ssl-le", 1, "x.test", ham)
	for _, yasak := range []string{"gizli123", "/var/lib/girginospanel", "ACCOUNT_THUMBPRINT"} {
		if strings.Contains(m, yasak) {
			t.Errorf("ham acme çıktısı mesajda: %s", m)
		}
	}
	if !strings.Contains(m, "(ref: ") {
		t.Errorf("ref yok: %s", m)
	}
}

// ── DB'li: gerçek akışlar ──

// Yeni alt alan / SSL kur+kaldır / PHP sürümü: hepsi aynı kalıpla vhost yazar.
func TestAcmeAkislari_HerYoldaYeniKalip(t *testing.T) {
	db := ha_db(t)
	ha_dikisler(t)
	tam := "blog." + ha_alan
	kod, g := ha_cagir(t, db, http.MethodPost, "/domains/{id}/subdomain", fmt.Sprintf("/domains/%d/subdomain", ha_domID),
		`{"alt_ad":"blog"}`, func(h *Handlers) http.HandlerFunc { return h.Olustur })
	if kod != http.StatusOK {
		t.Fatalf("oluştur: %d %s", kod, g)
	}
	docr := docrootOf(ha_sk, tam)
	ac_yeniKalip(t, ha_conf(t, "blog"), docr, "olustur")
	var sid int64
	_ = db.QueryRow(`SELECT id FROM subdomanlar WHERE alt_ad='blog'`).Scan(&sid)
	if err := os.MkdirAll(sslDir(ha_sk), 0o750); err != nil {
		t.Fatal(err)
	}
	adimlar := []struct {
		ad, yontem, desen, yol, govde string
		isle                          func(*Handlers) http.HandlerFunc
	}{
		{"php", http.MethodPut, "/domains/{id}/subdomain/{sid}/php", "php", `{"php_surum":"8.3"}`, func(h *Handlers) http.HandlerFunc { return h.PHPDegistir }},
		{"ssl-kur", http.MethodPost, "/domains/{id}/subdomain/{sid}/ssl", "ssl", `{"tip":"self-signed"}`, func(h *Handlers) http.HandlerFunc { return h.SSLKur }},
		{"php-ssl", http.MethodPut, "/domains/{id}/subdomain/{sid}/php", "php", `{"php_surum":"8.3"}`, func(h *Handlers) http.HandlerFunc { return h.PHPDegistir }},
		{"ssl-kaldir", http.MethodDelete, "/domains/{id}/subdomain/{sid}/ssl", "ssl", ``, func(h *Handlers) http.HandlerFunc { return h.SSLKaldir }},
	}
	for _, a := range adimlar {
		k, g := ha_cagir(t, db, a.yontem, a.desen, fmt.Sprintf("/domains/%d/subdomain/%d/%s", ha_domID, sid, a.yol), a.govde, a.isle)
		if k != http.StatusOK {
			t.Fatalf("%s: %d %s", a.ad, k, g)
		}
		ac_yeniKalip(t, ha_conf(t, "blog"), docr, a.ad)
	}
}

// SSL öncesi ön doğrulama: eski vhost varken acme.sh çağrıldığında conf ZATEN
// doğru olmalı; challenge 404 düşerse sade mesaj (ham çıktı yok, DNS yok).
func TestSSLKur_LE_EskiVhostAcmeOncesiOnarilir(t *testing.T) {
	db := ha_db(t)
	ha_dikisler(t)
	sid := ha_altEkle(t, db, "eski")
	tam := "eski." + ha_alan
	docr := docrootOf(ha_sk, tam)
	_ = os.WriteFile(confPath(ha_sk, "eski"), []byte(ac_eski(docr)), 0o644)
	var acmeAninda string
	base := komut
	komut = func(ad string, arg ...string) ([]byte, error) {
		if ad == "/root/.acme.sh/acme.sh" && len(arg) > 0 && arg[0] == "--issue" {
			b, _ := os.ReadFile(confPath(ha_sk, "eski"))
			acmeAninda = string(b)
			return []byte("Invalid response from http://" + tam + "/.well-known/acme-challenge/tok: 404\nACCOUNT_THUMBPRINT='gizli123'"), errors.New("exit status 1")
		}
		return base(ad, arg...)
	}
	kod, g := ha_cagir(t, db, http.MethodPost, "/domains/{id}/subdomain/{sid}/ssl",
		fmt.Sprintf("/domains/%d/subdomain/%d/ssl", ha_domID, sid), `{"tip":"letsencrypt"}`,
		func(h *Handlers) http.HandlerFunc { return h.SSLKur })
	ac_yeniKalip(t, acmeAninda, docr, "acme.sh çağrıldığı an")
	if kod != http.StatusBadRequest || strings.Contains(g, "gizli123") || strings.Contains(g, "DNS") ||
		!strings.Contains(g, "Doğrulama dosyasına erişilemedi") || !strings.Contains(g, "ref: ") {
		t.Fatalf("sade challenge mesajı beklenirdi: %d %s", kod, g)
	}
}

// Challenge bloğu hiç olmayan/tanınmayan vhost → SSL öncesi tek üreticiyle yeniden yazılır.
func TestSSLKur_LE_BilinmeyenVhost_YenidenKurulur(t *testing.T) {
	db := ha_db(t)
	ha_dikisler(t)
	sid := ha_altEkle(t, db, "elle")
	tam := "elle." + ha_alan
	_ = os.WriteFile(confPath(ha_sk, "elle"), []byte("server { listen 80; server_name "+tam+"; }\n"), 0o644)
	var acmeAninda string
	base := komut
	komut = func(ad string, arg ...string) ([]byte, error) {
		if ad == "/root/.acme.sh/acme.sh" {
			b, _ := os.ReadFile(confPath(ha_sk, "elle"))
			acmeAninda = string(b)
			return nil, errors.New("exit status 1")
		}
		return base(ad, arg...)
	}
	ha_cagir(t, db, http.MethodPost, "/domains/{id}/subdomain/{sid}/ssl",
		fmt.Sprintf("/domains/%d/subdomain/%d/ssl", ha_domID, sid), `{"tip":"letsencrypt"}`,
		func(h *Handlers) http.HandlerFunc { return h.SSLKur })
	ac_yeniKalip(t, acmeAninda, docrootOf(ha_sk, tam), "yeniden kurulan")
}

// Açılış onarımı: eski → düzelir, doğru/bilinmeyen/askıdaki hesap dokunulmaz; ikinci koşu no-op.
func TestHealAltAlanAcmeKalibi_Sayim(t *testing.T) {
	db := ha_db(t)
	ha_dikisler(t)
	for _, a := range []string{"eski1", "eski2", "dogru", "elle"} {
		ha_altEkle(t, db, a)
	}
	yaz := func(alt, g string) { _ = os.WriteFile(confPath(ha_sk, alt), []byte(g), 0o644) }
	yaz("eski1", ac_eski(docrootOf(ha_sk, "eski1."+ha_alan)))
	yaz("eski2", ac_eski(docrootOf(ha_sk, "eski2."+ha_alan)))
	yaz("dogru", vhost("dogru."+ha_alan, docrootOf(ha_sk, "dogru."+ha_alan), "/s", ""))
	elle := "server {\n    location /.well-known/acme-challenge/ {\n        root /opt/elle;\n    }\n}\n"
	yaz("elle", elle)
	s := HealAltAlanAcmeKalibi(db)
	if s.Duzeltilen != 2 || s.Dogru != 1 || s.Bilinmiyor != 1 || s.Hata != 0 {
		t.Fatalf("sayım yanlış: %+v", s)
	}
	if b := ha_conf(t, "elle"); b != elle {
		t.Fatalf("bilinmeyen biçime dokunuldu")
	}
	ac_yeniKalip(t, ha_conf(t, "eski1"), docrootOf(ha_sk, "eski1."+ha_alan), "eski1")
	s = HealAltAlanAcmeKalibi(db)
	if s.Duzeltilen != 0 || s.Dogru != 3 {
		t.Fatalf("ikinci koşu no-op olmalı: %+v", s)
	}
	// askıdaki hesap atlanır
	ha_askiya(t, db, 1)
	yaz("eski1", ac_eski(docrootOf(ha_sk, "eski1."+ha_alan)))
	if s = HealAltAlanAcmeKalibi(db); s.Duzeltilen != 0 {
		t.Fatalf("askıdaki hesaba dokunulmamalı: %+v", s)
	}
}
