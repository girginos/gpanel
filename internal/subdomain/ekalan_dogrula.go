package subdomain

// ek_alan_dogrula.go — ek (addon) alan adı biçim doğrulaması ve güvenli yol/dosya adı
// üreticileri (saf; DB/dosya sistemi yok). Alan adı doğrulandıktan sonra nginx
// server_name'e, vhost dosya adına, günlük dosyası adına, bölge dosyası adına ve
// docroot yoluna girer: BURADAKİ kapı tüm bu sızıntı yollarının tek savunmasıdır.

import (
	"errors"
	"net"
	"path/filepath"
	"regexp"
	"strings"

	"girginospanel/internal/provisioner"

	"golang.org/x/net/idna"
)

// reEkAd — doğrulanmış alan adının DOSYA/CONFIG güvenli biçimi: küçük harf/rakam/'-'
// etiketleri, noktayla ayrılmış, son etiket yalnız harf. '_' '/' '\' ' ' ';' '{' yok.
var reEkAd = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,24}$`)

// reEkSK — sistem kullanıcısı adı dosya adına girer: c_ + [a-z0-9_].
var reEkSK = regexp.MustCompile(`^c_[a-z0-9_]{1,62}$`)

// ekYasakliSonekler — ek alan adı olamayacak ad alanları (iç ağ / ters DNS / yerel).
var ekYasakliSonekler = []string{
	".local", ".localhost", ".internal", ".localdomain", ".lan", ".home.arpa", ".arpa", ".invalid", ".onion",
}

var errEkAdGecersiz = errors.New("geçersiz alan adı")

// ekAdGuvenli — ad, dosya adı/nginx/zone için güvenli (ekAlanAdiDogrula çıktısı) mi?
func ekAdGuvenli(ad string) bool {
	return len(ad) <= 253 && reEkAd.MatchString(ad) && !strings.Contains(ad, "..")
}

// ekAlanAdiDogrula — kullanıcı girişini normalleştirir (küçük harf, IDN → punycode)
// ve doğrular. Dönen ad güvenlidir; hata metni kullanıcıya gösterilebilir.
//
// Reddedilenler: boş, URL/yol/port/joker/e-posta, boşluk ve kontrol karakteri, IP,
// tek etiket, uzun etiket/ad, baş/son '-' ya da '.', '_', "www." ile başlayan,
// yerel/iç ad alanları, panelin yasaklı alan adı listesi (marka koruması).
func ekAlanAdiDogrula(girdi string) (string, error) {
	ad := strings.TrimSpace(girdi)
	ad = strings.TrimSuffix(ad, ".")
	if ad == "" {
		return "", errors.New("alan adı boş")
	}
	for _, c := range ad {
		if c <= ' ' || c == 0x7f {
			return "", errors.New("alan adı boşluk ya da kontrol karakteri içeremez")
		}
	}
	if strings.ContainsAny(ad, "/\\:@*?#%&=+,;\"'<>(){}[]|^`~!$_") {
		return "", errors.New("yalnız alan adını yazın (http://, yol, port, joker ya da e-posta olmadan)")
	}
	if net.ParseIP(ad) != nil {
		return "", errors.New("IP adresi alan adı olarak eklenemez")
	}
	ascii := ad
	for _, c := range ad {
		if c > 0x7f { // IDN: punycode'a çevir
			p, err := idna.Lookup.ToASCII(strings.ToLower(ad))
			if err != nil {
				return "", errors.New("alan adı uluslararası (IDN) biçimde geçersiz")
			}
			ascii = p
			break
		}
	}
	ascii = strings.ToLower(ascii)
	if len(ascii) > 253 {
		return "", errors.New("alan adı en çok 253 karakter olabilir")
	}
	if !strings.Contains(ascii, ".") {
		return "", errors.New("alan adı en az iki etiketten oluşmalı (örnek: ornek.com)")
	}
	for _, et := range strings.Split(ascii, ".") {
		if et == "" {
			return "", errors.New("alan adında boş etiket var (art arda nokta)")
		}
		if len(et) > 63 {
			return "", errors.New("alan adı etiketi en çok 63 karakter olabilir")
		}
		if et[0] == '-' || et[len(et)-1] == '-' {
			return "", errors.New("alan adı etiketi '-' ile başlayıp bitemez")
		}
	}
	if !reEkAd.MatchString(ascii) {
		return "", errors.New("geçersiz alan adı biçimi (örnek: ornek.com)")
	}
	if strings.HasPrefix(ascii, "www.") {
		return "", errors.New("'www.' ile başlayan ad eklenemez; alan adını www olmadan ekleyin (www otomatik karşılanır)")
	}
	if ascii == "localhost" {
		return "", errors.New("bu alan adı kullanılamaz")
	}
	for _, s := range ekYasakliSonekler {
		if strings.HasSuffix(ascii, s) {
			return "", errors.New("bu ad alanı (yerel/iç ağ) alan adı olarak eklenemez")
		}
	}
	// provisioner.ValidateDomain: TLD biçimi + sunucu yöneticisinin yasaklı listesi.
	if err := provisioner.ValidateDomain(ascii); err != nil {
		return "", err
	}
	return ascii, nil
}

// ── güvenli yol üreticileri ──

func ekDocrootTaban(sk string) string { return evKoku + "/" + sk + "/ek-alanlar" }

// ekDocroot — hesap dizini altında: <ev>/<sk>/ek-alanlar/<alan>/public_html.
func ekDocroot(sk, ad string) string { return ekDocrootTaban(sk) + "/" + ad + "/public_html" }

// ekConfPath — nginx vhost dosyası: ek_<sk>_<alan>.conf. Alan adı '_' içermez
// (reEkAd) → ek_c_shop_x.com.conf ile ek_c_shop_store_x.com.conf ayrıştırılabilir.
func ekConfPath(sk, ad string) string { return nginxConfD + "/ek_" + sk + "_" + ad + ".conf" }

// ekYolGuvenli — yolun (docroot ya da alan dizini) gerçekten hesap dizini altında
// kaldığını doğrular: sembolik bağlantı çözülmüş hâli de ev dizininin altında olmalı.
// Var olmayan yol için EN YAKIN var olan üst dizin çözülür. Kiracı kendi ek-alanlar
// dizinini sembolik bağlantıyla değiştirip kök işlemlerini (chown/RemoveAll) dışarı
// yönlendiremez.
func ekYolGuvenli(sk, yol string) error {
	taban := filepath.Clean(evKoku + "/" + sk)
	temiz := filepath.Clean(yol)
	if temiz != taban && !strings.HasPrefix(temiz, taban+string(filepath.Separator)) {
		return errors.New("yol hesap dizini dışında")
	}
	gercek, err := ekGercekYol(temiz)
	if err != nil {
		return err
	}
	gTaban, err := ekGercekYol(taban)
	if err != nil {
		return err
	}
	if gercek != gTaban && !strings.HasPrefix(gercek, gTaban+string(filepath.Separator)) {
		return errors.New("yol sembolik bağlantıyla hesap dizini dışına çıkıyor")
	}
	return nil
}

// ekGercekYol — yolun sembolik bağlantıları çözülmüş hâli. Var olmayan kısım için EN YAKIN
// var olan üst dizin çözülür ve kalan bileşenler eklenir.
func ekGercekYol(p string) (string, error) {
	kalan := ""
	for {
		if g, err := filepath.EvalSymlinks(p); err == nil {
			if kalan == "" {
				return g, nil
			}
			return filepath.Join(g, kalan), nil
		}
		ust := filepath.Dir(p)
		if ust == p {
			return "", errors.New("yol çözümlenemedi")
		}
		kalan = filepath.Join(filepath.Base(p), kalan)
		p = ust
	}
}
