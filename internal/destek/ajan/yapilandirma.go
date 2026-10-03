package ajan

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"regexp"
	"strings"

	"girginospanel/internal/destek/protokol"
	"girginospanel/internal/lisans"
	"girginospanel/internal/sirdosya"
)

// GomuluMerkezAnahtari: ÜRETİM merkez açık anahtar(lar)ı (base64url, virgülle ayrık).
// Yayın derlemesinde -ldflags "-X girginospanel/internal/destek/ajan.GomuluMerkezAnahtari=…"
// ile gömülür. Boşsa yalnız yapılandırma dosyasındaki (geliştirme) anahtarlar geçerlidir;
// hiç anahtar yoksa ajan BAĞLANMAZ (meydan okuma doğrulanamaz → kapalı başarısız).
var GomuluMerkezAnahtari = ""

// Varsayılan yollar (testte Yapilandirma alanlarıyla değiştirilir).
const (
	VarsayilanYapilandirmaYolu = "/etc/girginospanel/destek.json"
	VarsayilanCihazYolu        = "/etc/girginospanel/destek-cihaz.key"
)

// Yapilandirma: ajanın çalışma ayarları.
type Yapilandirma struct {
	// MerkezURL: merkez tabanı (https://bot.girginos.io). WSS adresi buradan türetilir.
	MerkezURL string
	// MerkezAnahtarlari: meydan okuma ve imzalı mesajları doğrulayan SABİT anahtarlar.
	MerkezAnahtarlari []ed25519.PublicKey
	// BotKullaniciAdi: t.me/<bot>?start=<kod> bağlantısı için (boş → yalnız kod gösterilir).
	BotKullaniciAdi string
	// CihazAnahtarYolu: cihaz Ed25519 tohumunun dosyası.
	CihazAnahtarYolu string
	// GuvensizIzin: YALNIZ TEST — http/ws (TLS'siz) merkeze izin.
	GuvensizIzin bool
	// YoklamaZorla: WSS denenmeden uzun yoklama (test / WSS'i engelleyen ağ).
	YoklamaZorla bool
	// LisansIspati: kayit.lisans_ispati baytları (nil → lisans bileti dosyası).
	LisansIspati func() []byte
	// Hata: dosya okunurken görülen yapılandırma sorunu (arayüzde gösterilir).
	Hata string
}

type dosyaYapilandirma struct {
	MerkezURL         string   `json:"merkez_url"`
	MerkezAnahtarlari []string `json:"merkez_anahtarlari"`
	BotKullaniciAdi   string   `json:"bot_kullanici_adi"`
}

var botAdDeseni = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{3,30}[Bb][Oo][Tt]$`)

// YapilandirmaYukle: gömülü anahtar + /etc/girginospanel/destek.json (varsa).
// Dosya root'a ait olmalı ve grup/diğer YAZAMAMALI; değilse YOK SAYILIR (kapalı başarısız).
func YapilandirmaYukle(yol string) Yapilandirma {
	y := Yapilandirma{MerkezURL: "https://" + protokol.MerkezHost, CihazAnahtarYolu: VarsayilanCihazYolu}
	for _, s := range strings.Split(GomuluMerkezAnahtari, ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		a, err := protokol.AcikAnahtarCoz(s)
		if err != nil {
			y.Hata = "gömülü merkez anahtarı bozuk"
			log.Printf("destek: gömülü merkez anahtarı bozuk: %v", err)
			continue
		}
		y.MerkezAnahtarlari = append(y.MerkezAnahtarlari, a)
	}
	if yol == "" {
		yol = VarsayilanYapilandirmaYolu
	}
	b, st, err := sirdosya.Oku(yol, 64<<10)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			y.Hata = "yapılandırma dosyası okunamadı"
			log.Printf("destek: %s okunamadı: %v", yol, err)
		}
		return y
	}
	if uid, _, ok := sirdosya.Sahip(st); !ok || int(uid) != os.Getuid() || st.Mode().Perm()&0o022 != 0 {
		y.Hata = "yapılandırma dosyasının sahibi/izni güvensiz — yok sayıldı"
		log.Printf("destek: %s sahibi/izni güvensiz (mod %o) — YOK SAYILDI", yol, st.Mode().Perm())
		return y
	}
	var d dosyaYapilandirma
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		y.Hata = "yapılandırma dosyası çözülemedi"
		log.Printf("destek: %s çözülemedi: %v", yol, err)
		return y
	}
	if d.MerkezURL != "" {
		y.MerkezURL = strings.TrimRight(d.MerkezURL, "/")
	}
	for _, s := range d.MerkezAnahtarlari {
		a, err := protokol.AcikAnahtarCoz(strings.TrimSpace(s))
		if err != nil {
			y.Hata = "yapılandırmadaki merkez anahtarı bozuk"
			continue
		}
		y.MerkezAnahtarlari = append(y.MerkezAnahtarlari, a)
	}
	if d.BotKullaniciAdi != "" {
		if botAdDeseni.MatchString(d.BotKullaniciAdi) {
			y.BotKullaniciAdi = d.BotKullaniciAdi
		} else {
			y.Hata = "bot kullanıcı adı geçersiz"
		}
	}
	return y
}

// adresler: merkez tabanından WSS ve yoklama adresleri. Üretimde yalnız https.
func (y Yapilandirma) adresler() (ws *url.URL, taban *url.URL, err error) {
	u, err := url.Parse(y.MerkezURL)
	if err != nil || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return nil, nil, fmt.Errorf("merkez adresi geçersiz")
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !y.GuvensizIzin {
			return nil, nil, fmt.Errorf("merkez adresi https olmalı")
		}
	default:
		return nil, nil, fmt.Errorf("merkez adresi https olmalı")
	}
	taban = &url.URL{Scheme: u.Scheme, Host: u.Host}
	ws = &url.URL{Scheme: "wss", Host: u.Host, Path: protokol.WSSYolu}
	if u.Scheme == "http" {
		ws.Scheme = "ws"
	}
	return ws, taban, nil
}

// lisansIspati: kayit.lisans_ispati. Varsayılan: lisans sunucusunun imzalı çalışma
// bileti (lisans.BiletYolu; opak — merkez lic-eu ile doğrular). Bilet yoksa ya da
// sınırı aşıyorsa açık bir işaret gönderilir; kararı merkez verir.
func (y Yapilandirma) lisansIspati() []byte {
	if y.LisansIspati != nil {
		if b := y.LisansIspati(); len(b) > 0 && len(b) <= protokol.LisansIspatSiniri {
			return b
		}
		return []byte("ispat-yok")
	}
	b, _, err := sirdosya.Oku(lisans.BiletYolu, int64(protokol.LisansIspatSiniri))
	if err != nil || len(strings.TrimSpace(string(b))) == 0 {
		return []byte("bilet-yok")
	}
	return []byte(strings.TrimSpace(string(b)))
}
