// Package hedef — yedeklerin UZAK kopyaları için ortak depolama sözleşmesi.
//
// Her uzak depo türü (S3/uyumlu, WebDAV, SMB, Dropbox, Google Drive ve mevcut
// FTP/SFTP yolunun adaptörü) Hedef arayüzünü uygular. Orkestratör (kuyruk,
// şifreleme, manifest, saklama) yalnız bu arayüzü bilir; tür ayrıntısı paket
// dışına sızmaz.
//
// Sözleşmenin çalıştırılabilir tanımı hedeftest.Uyum'dur: yeni bir tür ancak
// Uyum'dan geçerse orkestratöre bağlanır.
//
// Uzak adlar (anahtarlar) hedefin TABAN dizinine göre görelidir, ayırıcı "/"dir:
//
//	"2026-08-25/c_ornek-20260825-233538.tar.gz.age"
//
// Adlar AdDogrula ile denetlenir (boş, mutlak, "..", ters bölü, kontrol
// karakteri YOK); boşluk ve Türkçe karakter serbesttir.
package hedef

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"girginospanel/internal/agkapi"
)

// Yetenek — bir hedef türünün sözleşme ÜSTÜ yetenekleri (bit bayrakları).
type Yetenek uint32

const (
	// YetenekAtomikYukle — Yukle hata/iptal ile biterse nihai adda dosya KALMAZ
	// (geçici ad + yeniden adlandırma ya da sağlayıcının atomik yüklemesi).
	// hedeftest.Uyum bunu ZORUNLU tutar; mevcut lftp adaptörü henüz sağlamaz
	// (orkestratör manifesti en son yükleyerek "tamam" işaretler).
	YetenekAtomikYukle Yetenek = 1 << iota
	// YetenekOnekListele — Listele dizin içi ad önekiyle çoklu sonuç döner;
	// yoksa yalnız TAM ad sorgusu desteklenir.
	YetenekOnekListele
	// YetenekSunucuSha256 — Nesne.Sha256 sağlayıcının kendi özetinden gelir.
	YetenekSunucuSha256
	// YetenekBantSiniri — YukleSecenek.BantKBps uygulanır.
	YetenekBantSiniri
	// YetenekIlerleme — YukleSecenek.Ilerleme aktarım SIRASINDA çağrılır.
	YetenekIlerleme
)

var yetenekAdlari = []struct {
	y  Yetenek
	ad string
}{
	{YetenekAtomikYukle, "atomik-yukle"},
	{YetenekOnekListele, "onek-listele"},
	{YetenekSunucuSha256, "sunucu-sha256"},
	{YetenekBantSiniri, "bant-siniri"},
	{YetenekIlerleme, "ilerleme"},
}

// Var — b'deki TÜM bayraklar var mı?
func (y Yetenek) Var(b Yetenek) bool { return b != 0 && y&b == b }

func (y Yetenek) String() string {
	var adlar []string
	for _, a := range yetenekAdlari {
		if y.Var(a.y) {
			adlar = append(adlar, a.ad)
		}
	}
	return strings.Join(adlar, ",")
}

// Nesne — uzak hedefteki bir dosya.
type Nesne struct {
	Ad      string    // tabana göre tam anahtar (Listele'ye verilen önekten bağımsız)
	Boyut   int64     // bayt
	Degisim time.Time // sağlayıcı bildirmiyorsa sıfır
	Sha256  string    // küçük harf hex; yalnız YetenekSunucuSha256 ise dolu
}

// YukleSecenek — Yukle'nin isteğe bağlı ayarları.
type YukleSecenek struct {
	// Boyut — yerel dosyanın bilinen boyutu (<=0 → uygulama kendisi okur).
	Boyut int64
	// Sha256 — yerel dosyanın küçük harf hex özeti. Sağlayıcı destekliyorsa
	// sunucuya iletilir (bozuk aktarımı sağlayıcı reddeder).
	Sha256 string
	// BantKBps — yükleme hız sınırı, KiB/sn (0 = sınırsız). YetenekBantSiniri.
	BantKBps int
	// Ilerleme — o ana dek aktarılan bayt; aktarım SIRASINDA en az her 1 MiB'de
	// bir çağrılır (YetenekIlerleme). Uzun süren iş yapmamalı.
	Ilerleme func(aktarilan int64)
}

// Hedef — bir uzak depo. Uygulamalar eşzamanlı kullanıma güvenli olmalıdır.
//
// Tüm hata dönüşleri *Hata'dır (ya da onu sarar): Error() yalnız kategori taşır,
// ham sağlayıcı metni maskelenip Hata.Ayrinti()'de kalır.
type Hedef interface {
	// Tur — kayıt defterindeki tür adı ("s3", "webdav", "ftp" ...).
	Tur() string
	// Yetenekler — sözleşme üstü yetenekler.
	Yetenekler() Yetenek
	// Test — erişimi uçtan uca kanıtlar: dizin oluştur → yaz → oku → sil.
	Test(ctx context.Context) error
	// Yukle — yerel dosyayı uzak ada yükler; aynı ad varsa üzerine yazar.
	// YetenekAtomikYukle: hata/iptal ⇒ nihai adda dosya YOK.
	Yukle(ctx context.Context, yerelYol, uzakAd string, sec YukleSecenek) error
	// Listele — önek eşleşen DOSYALARI döner (alt dizinlere inmez): önekin
	// dizin kısmındaki doğrudan çocuklar, adı önekin son bileşeniyle başlayanlar.
	// Tam ad da bir önektir ("x.age" → "x.age.manifest.json" de gelebilir):
	// çağıran Ad eşitliğine bakmalı. YetenekOnekListele yoksa yalnız tam ad
	// sorgusu desteklenir. Eşleşme yoksa boş liste, hata DEĞİL.
	Listele(ctx context.Context, onek string) ([]Nesne, error)
	// Indir — uzak dosyayı yerelYol'a yazar. Hata/iptal ⇒ yerelYol'a yarım
	// içerik düşmez (geçici ad + yeniden adlandırma; bkz. AtomikYaz).
	// Dosya yoksa KategoriBulunamadi.
	Indir(ctx context.Context, uzakAd, yerelYol string) error
	// Sil — uzak dosyayı siler; dosya zaten yoksa nil.
	Sil(ctx context.Context, uzakAd string) error
}

var (
	// ErrGecersizAd — uzak ad AdDogrula'dan geçmedi.
	ErrGecersizAd = errors.New("hedef: geçersiz uzak ad")
	// ErrDesteklenmiyor — istenen işlem bu türde yok (ör. önekle listeleme).
	ErrDesteklenmiyor = errors.New("hedef: bu türde desteklenmiyor")
	// ErrBilinmeyenTur — kayıt defterinde olmayan tür.
	ErrBilinmeyenTur = errors.New("hedef: bilinmeyen tür")
)

// AzamiAdUzunlugu — backup_uzak_kopyalar.uzak_ad VARCHAR(512) ile uyumlu (bayt).
const AzamiAdUzunlugu = 512

// AdDogrula — uzak adı denetler: boş değil, geçerli UTF-8, en çok 512 bayt,
// "/" ile başlamaz/bitmez, ters bölü ve kontrol karakteri içermez, hiçbir
// bileşeni boş, "." ya da ".." değildir.
func AdDogrula(ad string) error {
	if ad == "" || len(ad) > AzamiAdUzunlugu || !utf8.ValidString(ad) {
		return ErrGecersizAd
	}
	if strings.HasPrefix(ad, "/") || strings.HasSuffix(ad, "/") || strings.Contains(ad, `\`) {
		return ErrGecersizAd
	}
	for _, r := range ad {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || r == utf8.RuneError {
			return ErrGecersizAd
		}
	}
	for _, p := range strings.Split(ad, "/") {
		if p == "" || p == "." || p == ".." || strings.TrimSpace(p) == "" {
			return ErrGecersizAd
		}
	}
	return nil
}

// AdBol — geçerli bir adı dizin ve dosya adına ayırır ("a/b/c" → "a/b", "c";
// "c" → "", "c").
func AdBol(ad string) (dizin, dosya string) {
	if i := strings.LastIndexByte(ad, '/'); i >= 0 {
		return ad[:i], ad[i+1:]
	}
	return "", ad
}

// OnekBol — Listele öneki: "" (taban), "dizin/" (dizinin tümü) ya da
// "dizin/adOneki". Dönen dizin "" olabilir; adOneki boş olabilir.
func OnekBol(onek string) (dizin, adOneki string, err error) {
	if onek == "" {
		return "", "", nil
	}
	if strings.HasSuffix(onek, "/") {
		d := strings.TrimSuffix(onek, "/")
		if err := AdDogrula(d); err != nil {
			return "", "", err
		}
		return d, "", nil
	}
	if err := AdDogrula(onek); err != nil {
		return "", "", err
	}
	d, a := AdBol(onek)
	return d, a, nil
}

// Katla — taban dizin ile göreli adı birleştirir ("/yedek" + "a/b" → "/yedek/a/b").
func Katla(taban, ad string) string {
	if ad == "" {
		if taban == "" {
			return ""
		}
		return path.Clean(taban)
	}
	if taban == "" {
		return ad
	}
	return path.Clean(taban + "/" + ad)
}

// ─── Tür kayıt defteri ──────────────────────────────────────────────────────

// KurSecenek — kurucuya panelin verdiği bağlam.
type KurSecenek struct {
	// Kapi — tüm ağ bağlantılarının geçeceği SSRF kapısı. nil → katı kapı
	// (yalnız herkese açık IP). Kökün "iç ağa izin" onayı burada taşınır.
	Kapi *agkapi.Kapi
}

// KapiAl — seçenekteki kapı ya da katı varsayılan.
func (s KurSecenek) KapiAl() *agkapi.Kapi {
	if s.Kapi != nil {
		return s.Kapi
	}
	return agkapi.Yeni(agkapi.Secenek{})
}

// Kurucu — bir türün Hedef kurucusu. ayar: sırsız JSON (backup_hedefler.ayar);
// sirlar: çözülmüş sırlar (backup_hedefler.sirlar). Kurucu ağ bağlantısı
// AÇMAMALI (yalnız doğrula ve kur); bağlantı ilk işlemde açılır.
type Kurucu func(ayar json.RawMessage, sirlar map[string]string, sec KurSecenek) (Hedef, error)

var turAdiRe = regexp.MustCompile(`^[a-z][a-z0-9]{1,15}$`)

type defter struct {
	mu sync.RWMutex
	m  map[string]Kurucu
}

func (d *defter) kaydet(tur string, k Kurucu) {
	if !turAdiRe.MatchString(tur) || k == nil {
		panic(fmt.Sprintf("hedef: geçersiz tür kaydı %q", tur))
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.m == nil {
		d.m = map[string]Kurucu{}
	}
	if _, vardi := d.m[tur]; vardi {
		panic(fmt.Sprintf("hedef: tür iki kez kaydedildi %q", tur))
	}
	d.m[tur] = k
}

func (d *defter) kur(tur string, ayar json.RawMessage, sirlar map[string]string, sec KurSecenek) (Hedef, error) {
	d.mu.RLock()
	k, ok := d.m[tur]
	d.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrBilinmeyenTur, tur)
	}
	return k(ayar, sirlar, sec)
}

func (d *defter) turler() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]string, 0, len(d.m))
	for t := range d.m {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

var varsayilanDefter = &defter{}

// Kaydet — türü kayıt defterine ekler; yalnız init()'te çağrılır. Geçersiz ya
// da yinelenen tür adı panic (programlama hatası, açılışta görünür).
func Kaydet(tur string, k Kurucu) { varsayilanDefter.kaydet(tur, k) }

// Kur — kayıtlı türden bir Hedef kurar.
func Kur(tur string, ayar json.RawMessage, sirlar map[string]string, sec KurSecenek) (Hedef, error) {
	return varsayilanDefter.kur(tur, ayar, sirlar, sec)
}

// Turler — kayıtlı türler (sıralı).
func Turler() []string { return varsayilanDefter.turler() }
