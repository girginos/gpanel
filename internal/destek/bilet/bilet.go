// Package bilet — portalın (girginos.io lisans sunucusu) imzaladığı LİSANS BİLETİNİN
// merkez tarafı doğrulayıcısı.
//
// Kablo biçimi lisans sunucusundaki bilet.go (modules/license) ile BİREBİR aynıdır
// (panelin internal/lisans/bilet_dogrula.go'su da aynı biçimi okur):
//
//	bilet = base64url(govde_json) "." base64url(imza)     (dolgusuz)
//	imza  = Ed25519(portal özel anahtarı, govde_json BAYTLARI)
//	govde = {"fp","urun","durum","bitis","askiya","iat","nonce","pkt"}
//
// 🔴 NEDEN AYRI KOD (panelin lisans paketi kullanılmıyor): o paketin ayrıştırıcısı
// dışa açık değil, açık anahtarı derleme sabiti ve git'te izlenmiyor (internal/lisans
// kopyalanarak taşınan, ayrı sürümlü paket); merkez ise portal anahtar(lar)ını
// YAPILANDIRMADAN alır (anahtar döndürme). Kurallar panelin biletGecerlikSeti'yle
// aynıdır — burada yalnız ek olarak durum listesi ve iat sağlaması vardır.
//
// Doğrulama: imza → bitiş (bitis>0 ZORUNLU: aynı anahtarla imzalı başka tür
// yanıtların bilet diye geçmesini önler) → parmak izi bağı → durum → askı anı.
package bilet

import (
	"crypto/ed25519"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// EnBuyukBoyut: kabul edilen bilet metni üst sınırı (protokol.LisansIspatSiniri ile uyumlu).
const EnBuyukBoyut = 4096

// iatSapma: biletin veriliş anı gelecekte en çok bu kadar olabilir (saat farkı payı).
const iatSapma = 10 * time.Minute

// Bilet durumları (lisans sunucusunun yazdığı değerler).
const (
	DurumAktif    = "aktif"
	DurumOdenmedi = "odenmedi" // süre doldu, 7 günlük pay içinde: hizmet sürer
	DurumAskida   = "askida"
)

// Hatalar (hiçbiri bilet içeriğini taşımaz — log/yanıtta güvenle gösterilir).
var (
	ErrBicim       = errors.New("bilet biçimi geçersiz")
	ErrImza        = errors.New("bilet imzası doğrulanamadı")
	ErrSuresiDoldu = errors.New("bilet süresi dolmuş")
	ErrParmakIzi   = errors.New("bilet başka bir parmak izine ait")
	ErrDurum       = errors.New("bilet durumu hizmete izin vermiyor")
	ErrAskida      = errors.New("bilet askı anı geçmiş")
	ErrAnahtarYok  = errors.New("portal bilet anahtarı yapılandırılmamış")
)

// Govde: biletin imzalı içeriği.
type Govde struct {
	FP     string `json:"fp"`
	Urun   string `json:"urun"`
	Durum  string `json:"durum"`
	Bitis  int64  `json:"bitis"`
	Askiya int64  `json:"askiya"`
	Iat    int64  `json:"iat"`
	Nonce  string `json:"nonce"`
	Pkt    string `json:"pkt"`
}

// Dogrulayici: portal açık anahtarlarıyla (birden çok: anahtar döndürme) bilet doğrular.
type Dogrulayici struct {
	anahtarlar []ed25519.PublicKey
}

// Yeni: açık anahtarlar base64url (dolgusuz), 32 bayt. Boş liste geçerlidir ama hiçbir
// bilet doğrulanamaz (kapalı başarısız).
func Yeni(acikAnahtarlar []ed25519.PublicKey) *Dogrulayici {
	d := &Dogrulayici{}
	for _, a := range acikAnahtarlar {
		if len(a) == ed25519.PublicKeySize {
			d.anahtarlar = append(d.anahtarlar, append(ed25519.PublicKey(nil), a...))
		}
	}
	return d
}

// AnahtarSayisi: yapılandırılmış anahtar sayısı (tanı).
func (d *Dogrulayici) AnahtarSayisi() int { return len(d.anahtarlar) }

// Dogrula: bilet, beklenenFP (64 küçük hex) için bu anda geçerli mi. Dönen Sonuc.Bitis,
// doğrulamanın DÜŞECEĞİ an: min(bilet.bitis, bilet.askiya>0 ise askiya).
func (d *Dogrulayici) Dogrula(bilet []byte, beklenenFP string, simdi time.Time) (Sonuc, error) {
	if len(d.anahtarlar) == 0 {
		return Sonuc{}, ErrAnahtarYok
	}
	if len(bilet) == 0 || len(bilet) > EnBuyukBoyut {
		return Sonuc{}, ErrBicim
	}
	parca := strings.Split(strings.TrimSpace(string(bilet)), ".")
	if len(parca) != 2 {
		return Sonuc{}, ErrBicim
	}
	b64 := base64.RawURLEncoding.Strict()
	govde, err := b64.DecodeString(parca[0])
	if err != nil || len(govde) == 0 {
		return Sonuc{}, ErrBicim
	}
	imza, err := b64.DecodeString(parca[1])
	if err != nil || len(imza) != ed25519.SignatureSize {
		return Sonuc{}, ErrBicim
	}
	// 🔴 İmza doğrulanmadan gövdeye BAKILMAZ.
	ok := false
	for _, a := range d.anahtarlar {
		if ed25519.Verify(a, govde, imza) {
			ok = true
			break
		}
	}
	if !ok {
		return Sonuc{}, ErrImza
	}
	var g Govde
	if err := json.Unmarshal(govde, &g); err != nil {
		return Sonuc{}, ErrBicim
	}
	if g.Bitis <= 0 || g.Iat <= 0 || g.Urun == "" || len(g.Urun) > 64 {
		return Sonuc{}, ErrBicim
	}
	if !simdi.Before(time.Unix(g.Bitis, 0)) {
		return Sonuc{}, ErrSuresiDoldu
	}
	if g.Iat > simdi.Add(iatSapma).Unix() {
		return Sonuc{}, ErrBicim
	}
	if len(beklenenFP) != 64 || g.FP == "" || subtle.ConstantTimeCompare([]byte(g.FP), []byte(beklenenFP)) != 1 {
		return Sonuc{}, ErrParmakIzi
	}
	if g.Durum != DurumAktif && g.Durum != DurumOdenmedi {
		return Sonuc{}, ErrDurum
	}
	bitis := g.Bitis
	if g.Askiya > 0 {
		if !simdi.Before(time.Unix(g.Askiya, 0)) {
			return Sonuc{}, ErrAskida
		}
		if g.Askiya < bitis {
			bitis = g.Askiya
		}
	}
	return Sonuc{Bitis: time.Unix(bitis, 0), Durum: g.Durum, Urun: g.Urun, Iat: time.Unix(g.Iat, 0)}, nil
}

// Sonuc: başarılı doğrulamanın özeti.
type Sonuc struct {
	Bitis time.Time // doğrulamanın düşeceği an
	Durum string
	Urun  string
	Iat   time.Time
}
