// Package hostuyg — host-level (sunucu-genelinde) native uygulama installer.
//
// Faz 1 (internal/uygulama) tenant-level PHP CMS'ler için. Bu paket ADMİN-ONLY
// sistem-level native binary uygulamalar için: TeamSpeak, WireGuard, Vaultwarden,
// Gitea, Minecraft vb.
//
// Docker YOK — systemd unit + hardening + cgroup limits + per-app system user
// + dinamik port havuzu + firewall entegrasyonu.
//
// Kurulum modeli:
//   1. Recipe seç (katalogdan)
//   2. Port rezerv (7000-7999 havuzundan)
//   3. Sistem user yarat (gpanel-app-<kod>)
//   4. Binary indir + SHA256 doğrula + kurulum dizinine aç
//   5. Config şablonu render (port, secret, dizin değişkenleri)
//   6. Systemd unit yaz + daemon-reload + enable + start
//   7. Firewall port aç
//   8. Healthcheck
//   9. Rollback: 7→1 tersine (unit stop+disable, user del, dizin sil, port serbest)

package hostuyg

import (
	"errors"
	"regexp"
	"strings"
)

// Tarif — bir uygulamanın kurulum reçetesi.
type Tarif struct {
	Kod      string // "vaultwarden" — unique katalog anahtarı
	Ad       string // "Vaultwarden" — kullanıcı görünür
	Aciklama string
	Kategori string // "vpn" | "iletisim" | "gelistirme" | "oyun" | "medya" | "diger"
	Ikon     string // frontend için (emoji veya isim)
	// LogoURL — markanin gercek SVG logosu (SimpleIcons CDN). Bos ise
	// arayuz Ikon emojisine duser. CSP img-src cdn.simpleicons.org
	// icermeli (bkz. assets/nginx/_panel.conf).
	LogoURL string `json:"logo_url,omitempty"`
	Surum   string // "1.32.5"

	// Kaynak
	IndirmeURL string // https://github.com/dani-garcia/vaultwarden/releases/...
	SHA256     string // binary/tarball hash
	IcerikTuru string // "binary" | "tarball_gz" | "tarball_xz" | "zip"
	BinaryYol  string // tarball içinde binary yolu (relative), binary tek dosya ise ""

	// Runtime
	CalistirKomutu []string          // ["{kurulum}/vaultwarden"] — {kurulum}, {port}, {config} template
	CalismaDizini  string            // "" ise {kurulum}
	CevreDegisken  map[string]string // ENV — {port}, {secret} template

	// Kaynak sınırı (systemd)
	MemoryMax string // "512M" — boş ise 512M default
	CPUQuota  string // "50%" — boş ise 50% default
	TasksMax  int    // 200 default

	// Port
	Portlar []PortTarifi

	// Config dosyaları — kurulumdan sonra {kurulum} altına yazılır
	ConfigDosyalar []ConfigDosyaTarifi

	// Sağlık kontrolü — kurulumdan sonra
	SaglikTCP   int    // TCP port bağlan → OK
	SaglikHTTP  string // "http://127.0.0.1:{port}/alive" — GET 200/204 → OK
	SaglikBekle int    // saniye — start sonrası ilk kontrolden önce bekle (default 3)

	// Web app'ler için nginx reverse proxy
	NginxProxy *NginxProxyTarifi // nil ise proxy yok (native TCP/UDP app)

	// Backup ayarları (recipe-özel override)
	BackupExclude   []string // tar --exclude pattern'ları (log, cache vs.)
	BackupTutSayisi int      // rotasyon: kaç yedek tut (0 → global default YedekTutSayisi)
}

type PortTarifi struct {
	Ad       string // "web", "voice", "file_transfer" — açıklama
	Protokol string // "tcp" | "udp"
	Zorunlu  int    // 0 = dinamik havuzdan; !=0 = sabit port zorunlu (ör. WG 51820)

	// DisAcik — NginxProxy varsa DEFAULT olarak port sadece 127.0.0.1'de
	// bilinir (nginx proxy'ler). Ancak bazı app'ler (Syncthing sync, MC,
	// TeamSpeak voice) bu porta DIŞARIDAN P2P/UDP erişimi gerektirir.
	// DisAcik=true → NginxProxy varsa bile bu port firewalld/nftables'a
	// eklenir.
	DisAcik bool
}

type ConfigDosyaTarifi struct {
	Yol    string // {kurulum} altında relative — "config.toml"
	Icerik string // template — {port}, {secret16}, {secret32}, {kurulum} placeholders
	Izin   uint32 // 0600 gibi
	Sahip  string // "app" ise sistem user; "root" ise root
}

type NginxProxyTarifi struct {
	Subdomain     bool   // true → subdomain.panelhost, false → subpath
	SubdomainOn   string // "vault" ise vault.panelhost
	SubPathOn     string // "/vaultwarden" ise panelhost/vaultwarden
	UpgradeWS     bool   // Websocket upgrade header'ları
	MaxBodySize   string // "512m" gibi
	ExtraDirektif string // ekstra nginx location direktifleri
}

// kodDeseni — recipe kodu güvenli karakter seti (path/user adında güvenli).
var kodDeseni = regexp.MustCompile(`^[a-z][a-z0-9-]{1,30}[a-z0-9]$`)

// ornekAdDeseni — kullanıcı input; kod ile birleştirilip path/unit adı olur.
// C1 fix: path traversal (../, absolute path) engellenir.
var ornekAdDeseni = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)

// OrnekAdDogrula — handler.Kur ve KurAsync başında çağır. ornek_ad'ı
// path traversal + shell injection'a karşı sertleştir.
//
// N2 fix: kod+ornek_ad birlikte 32 char kullanıcı adı sınırını aşarsa
// KullaniciAdi() truncate eder → farklı ornek_ad'lar aynı user'a düşer
// (collision). Reddet.
func OrnekAdDogrula(ad string) error {
	if ad == "" {
		return errors.New("ornek_ad boş")
	}
	if !ornekAdDeseni.MatchString(ad) {
		return errors.New("ornek_ad geçersiz: küçük harf/rakam/tire, 1-31 char, harfle/rakamla başla")
	}
	return nil
}

// OrnekAdVeKodDogrula — kod+ornek_ad birlikte truncation'sız 32 char
// sistem user'a sığmalı. Kod hardcoded katalog'dan geliyor, ornek_ad
// kullanıcı input; ikisi birlikte kontrol edilmeli.
func OrnekAdVeKodDogrula(kod, ornek string) error {
	if err := OrnekAdDogrula(ornek); err != nil {
		return err
	}
	// KullaniciAdi truncate ediyorsa reddet
	tam := KullaniciPrefix
	if ornek != "" && ornek != kod {
		tam += kod + "-" + ornek
	} else {
		tam += kod
	}
	if len(tam) > 32 {
		return errors.New("kod+ornek_ad birlikte çok uzun — sistem user adı 32 char'ı aşamaz, ornek_ad'ı kısalt")
	}
	return nil
}

// Dogrula — recipe temel sanity check. Kurulum başlamadan çağrılmalı.
func (t *Tarif) Dogrula() error {
	if !kodDeseni.MatchString(t.Kod) {
		return errors.New("kod geçersiz: küçük harf/rakam/tire, 3-32 char, harfle başla")
	}
	if t.Ad == "" {
		return errors.New("ad boş olamaz")
	}
	if t.IndirmeURL == "" {
		return errors.New("indirme URL boş")
	}
	if t.SHA256 == "" {
		return errors.New("SHA256 zorunlu (supply chain koruması)")
	}
	if len(t.SHA256) != 64 {
		return errors.New("SHA256 64 hex karakter olmalı")
	}
	// I7 fix + genişletme: tekrarlı-tek-char placeholder'ları reddet
	// ("0000...", "aaaa...", "1111...", "ffff...", "dead...beef" vb.)
	lower := strings.ToLower(t.SHA256)
	tekChar := true
	if len(lower) > 0 {
		first := lower[0]
		for i := 1; i < len(lower); i++ {
			if lower[i] != first {
				tekChar = false
				break
			}
		}
	}
	if tekChar {
		return errors.New("SHA256 placeholder (tek karakter tekrarı) — gerçek hash gerekli")
	}
	if t.IcerikTuru != "binary" && t.IcerikTuru != "tarball_gz" &&
		t.IcerikTuru != "tarball_xz" && t.IcerikTuru != "tarball_bz2" && t.IcerikTuru != "zip" {
		return errors.New("icerik_turu: binary|tarball_gz|tarball_xz|tarball_bz2|zip")
	}
	if len(t.CalistirKomutu) == 0 {
		return errors.New("calistir_komutu boş")
	}
	if len(t.Portlar) == 0 && t.NginxProxy == nil {
		return errors.New("en az bir port veya nginx proxy tanımlanmalı")
	}
	for _, p := range t.Portlar {
		if p.Protokol != "tcp" && p.Protokol != "udp" {
			return errors.New("port protokolü: tcp|udp")
		}
		if p.Zorunlu != 0 && (p.Zorunlu < 1 || p.Zorunlu > 65535) {
			return errors.New("zorunlu port 1-65535 aralığında olmalı")
		}
	}
	return nil
}

// KatalogAra — verilen kod ile eşleşen tarif.
func KatalogAra(kod string) *Tarif {
	for i := range Katalog {
		if Katalog[i].Kod == kod {
			t := Katalog[i]
			return &t
		}
	}
	return nil
}

// KatalogListe — tümü + Dogrula() geçenler için hazir bayrağı ekle.
// UI hazir=false olanları "yakında" olarak gösterir; kur butonu disabled.
func KatalogListe() []KatalogSatir {
	out := make([]KatalogSatir, 0, len(Katalog))
	for i := range Katalog {
		t := Katalog[i]
		hazir := t.Dogrula() == nil
		out = append(out, KatalogSatir{Tarif: t, Hazir: hazir})
	}
	return out
}

// KatalogSatir — API için: Tarif + Hazir bayrağı.
// Not: Tarif alanları da JSON'a lowercase serialize edilsin diye Tarif
// struct'ında explicit json tag yok — Go embedding default olarak alan adını
// aynen kullanır. `hazir` küçük harf tag ile frontend uyumu garanti.
type KatalogSatir struct {
	Tarif
	Hazir bool `json:"hazir"`
}
