// Package kapi — panelin rol başına giriş kapıları (ADR-GP-001).
//
// 🔴 NEDEN: tüm roller (admin / bayi / müşteri) TEK ve herkesçe bilinen porttan
// (8443) giriyordu; rol ayrımı yalnız giriş SONRASI token'daydı. Bilinen port =
// toplu tarama + kaba-kuvvet hedefi; tek kapı = müşteriye açık bir portta yönetici
// giriş yüzeyi de açık demekti. Bu paket her role AYRI, kuruluma özel rastgele bir
// port verir ve:
//   - giriş ucunu kapıya kilitler (müşteri portunda yönetici girişi YOKTUR → 404),
//   - token'ı alındığı kapıya bağlar (başka kapıda 401 → çalınan token taşınamaz).
//
// 🔴 GÜVEN ZİNCİRİ: kapı, nginx'in `map $server_port $gp_kapi` ile ürettiği
// X-GP-Kapi başlığından okunur. `$server_port` bağlantıyı kabul eden soketten
// gelir, istemci seçemez. Ama başlığa YALNIZ istek nginx'ten geldiği KANITLANIRSA
// (loopback + paylaşımlı X-Gosp-Proxy gizlisi) güvenilir: aksi halde aynı kutudaki
// bir kiracı süreci :8080'e sahte başlıkla bağlanıp kapıyı kendisi seçerdi.
// Zorlama bayrağı YALNIZ Heal uzlaştırması nginx'in başlığı gönderdiğini GARANTİ
// ettikten sonra açılır (proxy.secret ile aynı fail-safe sıralama).
//
// Rastgele port bir GİZLEME katmanıdır (toplu tarama gürültüsünü keser); asıl
// güvenlik sınırı rol ayrımı + kapıya bağlı token'dır. Kerckhoffs: bu tasarım
// açık olabilir; gizli olan kuruluma özel port değerleridir.
package kapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"girginospanel/internal/httpx"
)

// Kapı adları. Token'a, nginx map'ine ve X-GP-Kapi başlığına AYNEN yazılır.
const (
	Admin      = "admin"
	Bayi       = "bayi"
	Musteri    = "musteri"
	Eski       = "eski"       // geçiş süresince açık kalan eski ortak port (her rol girer)
	Bilinmeyen = "bilinmeyen" // zorlama açık ama kapı kanıtlanamadı → her şey reddedilir
)

// Roller: rol kapılarının sabit sırası (çıktı/doğrulama için).
var Roller = []string{Admin, Bayi, Musteri}

const (
	// AyarYolu: rol portlarının TEK doğruluk kaynağı. nginx map'i ve _panel.conf
	// listen'ları bundan TÜRETİLİR; ikisi de ezilse/silinse Heal buradan onarır.
	AyarYolu = "/etc/girginospanel/kapi.json"
	// MapYolu: http-context map dosyası. 00- öneki → _panel.conf'tan ÖNCE yüklenir.
	MapYolu = "/etc/nginx/conf.d/00-gosp-kapi.conf"
	// Baslik: nginx'in backend'e ilettiği kapı başlığı.
	Baslik = "X-GP-Kapi"
	// VarsayilanGecisGun: otomatik geçişte eski portun açık kalacağı süre.
	VarsayilanGecisGun = 60
)

// Ayar: /etc/girginospanel/kapi.json şeması.
type Ayar struct {
	Surum   int `json:"surum"`
	Admin   int `json:"admin"`
	Bayi    int `json:"bayi"`
	Musteri int `json:"musteri"`
	// EskiPortlar: geçişten ÖNCE panelin dinlediği genel portlar (ör. 8443). Geçiş
	// süresince açık kalır ve "eski" kapısı olarak her rolü kabul eder.
	EskiPortlar []int `json:"eski_portlar,omitempty"`
	// EskiBitis: eski portların en erken kapanabileceği an (unix sn).
	EskiBitis int64 `json:"eski_bitis,omitempty"`
	// AdminDogrulandi: admin'in YENİ admin kapısından ilk başarılı girişi (unix sn).
	// 🔴 Eski portlar bu olmadan KAPANMAZ: panel, sağlayıcının dış firewall'unu
	// (bulut güvenlik grubu vb.) göremez. Rastgele port orada kapalıysa ve eski port
	// da kapanırsa herkes kilitlenir. Dışarıdan erişilebilirliğin tek gerçek kanıtı,
	// admin'in o porttan gerçekten girmiş olmasıdır.
	AdminDogrulandi int64 `json:"admin_dogrulandi,omitempty"`
	Olusturma       int64 `json:"olusturma,omitempty"`
}

// RolPortu: rolün kapı portu (bilinmeyen rol → 0).
func (a *Ayar) RolPortu(rol string) int {
	switch rol {
	case Admin:
		return a.Admin
	case Bayi:
		return a.Bayi
	case Musteri:
		return a.Musteri
	}
	return 0
}

// EskiAcik: eski portlar şu an açık KALMALI mı? Süre dolmadıysa ya da admin yeni
// kapıyı henüz doğrulamadıysa açık kalır (kilitlenme kilidi).
func (a *Ayar) EskiAcik(simdi int64) bool {
	if len(a.EskiPortlar) == 0 {
		return false
	}
	return simdi < a.EskiBitis || a.AdminDogrulandi == 0
}

// Gecerli: portlar tanımlı, aralıkta ve birbirinden farklı mı?
func (a *Ayar) Gecerli() error {
	gorulen := map[int]string{}
	for _, rol := range Roller {
		p := a.RolPortu(rol)
		if p < 1024 || p > 65535 {
			return fmt.Errorf("%s portu geçersiz: %d", rol, p)
		}
		if onceki, ok := gorulen[p]; ok {
			return fmt.Errorf("%s ve %s aynı portu kullanıyor: %d", onceki, rol, p)
		}
		gorulen[p] = rol
	}
	for _, p := range a.EskiPortlar {
		if p < 1 || p > 65535 {
			return fmt.Errorf("eski port geçersiz: %d", p)
		}
		if rol, ok := gorulen[p]; ok {
			return fmt.Errorf("eski port %d aynı zamanda %s kapısı", p, rol)
		}
	}
	return nil
}

// PortHaritasi: port → kapı adı (eski portlar yalnız açıkken dahil).
func (a *Ayar) PortHaritasi(simdi int64) map[int]string {
	m := map[int]string{a.Admin: Admin, a.Bayi: Bayi, a.Musteri: Musteri}
	if a.EskiAcik(simdi) {
		for _, p := range a.EskiPortlar {
			m[p] = Eski
		}
	}
	return m
}

// SiraliPortlar: haritadaki portlar artan sırada (deterministik çıktı için).
func SiraliPortlar(m map[int]string) []int {
	out := make([]int, 0, len(m))
	for p := range m {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}

// ErrYok: kapı modu yapılandırılmamış (kapi.json yok).
var ErrYok = errors.New("kapı modu yapılandırılmamış")

// Oku: ayarı okur ve doğrular. Dosya yoksa ErrYok.
func Oku(yol string) (*Ayar, error) {
	b, err := os.ReadFile(yol)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrYok
	}
	if err != nil {
		return nil, err
	}
	var a Ayar
	if err := json.Unmarshal(b, &a); err != nil {
		return nil, fmt.Errorf("kapi.json bozuk: %w", err)
	}
	if err := a.Gecerli(); err != nil {
		return nil, fmt.Errorf("kapi.json geçersiz: %w", err)
	}
	return &a, nil
}

var yazMu sync.Mutex

// Yaz: ayarı atomik yazar (geçici dosya + rename; yarım yazılmış dosya okunmaz).
func Yaz(yol string, a *Ayar) error {
	if err := a.Gecerli(); err != nil {
		return err
	}
	yazMu.Lock()
	defer yazMu.Unlock()
	if a.Surum == 0 {
		a.Surum = 1
	}
	b, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(yol), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(yol), ".kapi-*.json")
	if err != nil {
		return err
	}
	tmpAd := tmp.Name()
	defer os.Remove(tmpAd) // rename başarılıysa artık yok, sessizce geçer
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpAd, yol)
}

// Guncelle: oku → değiştir → yaz (tek kilit altında; eşzamanlı güncellemeler kaybolmaz).
func Guncelle(yol string, fn func(*Ayar) error) error {
	guncelleMu.Lock()
	defer guncelleMu.Unlock()
	a, err := Oku(yol)
	if err != nil {
		return err
	}
	if err := fn(a); err != nil {
		return err
	}
	return Yaz(yol, a)
}

var guncelleMu sync.Mutex

// ── Çalışma zamanı zorlama bayrağı ────────────────────────────────────────

var zorlama atomic.Bool

// ZorlamaAktif: kapı kuralları uygulanıyor mu?
func ZorlamaAktif() bool { return zorlama.Load() }

// ZorlamaAyarla: YALNIZ Heal uzlaştırması (ve testler) çağırır.
func ZorlamaAyarla(b bool) { zorlama.Store(b) }

// nginxtenMi: testlerde değiştirilebilir güven kontrolü.
var nginxtenMi = httpx.NginxtenMi

// Istek: isteğin geldiği kapı.
//   - zorlama kapalı → "" (eski davranış: tek kapı, her rol)
//   - istek nginx'ten geldiği kanıtlanamıyor → Bilinmeyen (doğrudan :8080 = reddet)
//   - başlık yok / çoklu / tanımsız değer → Bilinmeyen
func Istek(r *http.Request) string {
	if !ZorlamaAktif() {
		return ""
	}
	if !nginxtenMi(r) {
		return Bilinmeyen
	}
	v := r.Header.Values(Baslik)
	if len(v) != 1 {
		return Bilinmeyen
	}
	switch k := strings.TrimSpace(v[0]); k {
	case Admin, Bayi, Musteri, Eski:
		return k
	}
	return Bilinmeyen
}

// GirisIzinli: bu kapıdan bu rol giriş yapabilir mi?
//   - "" (zorlama kapalı) ve Eski (geçiş) → her rol (bugünkü davranış)
//   - rol kapısı → yalnız kendi rolü
func GirisIzinli(istekKapi, rol string) bool {
	switch istekKapi {
	case "", Eski:
		return true
	case Bilinmeyen:
		return false
	}
	return istekKapi == rol
}

// TokenUyumlu: token'ın bağlı olduğu kapı, isteğin kapısında geçerli mi?
//   - "" (zorlama kapalı) → her token (geri alma/ilk açılışta kimse atılmasın)
//   - Eski → bağsız (geçiş öncesi) ve eski-kapısı token'ları
//   - rol kapısı → YALNIZ aynı kapıda alınmış token
func TokenUyumlu(istekKapi, tokenKapi string) bool {
	switch istekKapi {
	case "":
		return true
	case Bilinmeyen:
		return false
	case Eski:
		return tokenKapi == "" || tokenKapi == Eski
	}
	return tokenKapi == istekKapi
}
