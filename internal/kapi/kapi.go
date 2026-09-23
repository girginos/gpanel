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
// X-GP-Kapi başlığından okunur ("<kapı> <$server_port> <proxy gizlisi>").
// `$server_port` bağlantıyı kabul eden soketten gelir, istemci seçemez. Başlığa YALNIZ
// istek nginx'ten geldiği KANITLANIRSA (loopback + X-Gosp-Proxy) ve değer gizliyi
// taşıyorsa güvenilir: aksi halde aynı kutudaki bir kiracı süreci :8080'e sahte
// başlıkla bağlanıp — ya da kapı satırı olmayan bir location'dan geçirip — kapıyı
// kendisi seçerdi. Zorlama bayrağı YALNIZ Heal uzlaştırması nginx'in başlığı
// gönderdiğini GARANTİ ettikten sonra açılır (proxy.secret ile aynı fail-safe sıralama).
//
// Rastgele port bir GİZLEME katmanıdır (toplu tarama gürültüsünü keser); asıl
// güvenlik sınırı rol ayrımı + kapıya bağlı token'dır. Kerckhoffs: bu tasarım
// açık olabilir; gizli olan kuruluma özel port değerleridir.
package kapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
	// OlcumBaslik: zincir ölçümünün tek kullanımlık değeri (nonce); bkz. OlcumKaniti.
	OlcumBaslik = "X-GP-Olcum"
	// ErisimModuYolu: girginospanel-erisim'in kayıtlı modu ("web" | "masaustu").
	ErisimModuYolu = "/etc/girginospanel/erisim-modu"
	// VarsayilanGecisGun: otomatik geçişte eski portun açık kalacağı süre.
	VarsayilanGecisGun = 60
)

// masaustuModu: dikiş (testler).
var masaustuModu = func() bool {
	b, err := os.ReadFile(ErisimModuYolu)
	return err == nil && strings.TrimSpace(string(b)) == "masaustu"
}

// MasaustuModu: operatör yönetici yüzeyini internete kapatmış mı (girginospanel-erisim
// masaustu)? Bu modda yönetici kapısı yalnız 127.0.0.1'de dinler, tek yolu SSH tünelidir.
func MasaustuModu() bool { return masaustuModu() }

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
	// IlkGiris: bayi/müşteri kapısından ilk başarılı dış giriş (rol → unix sn). Kapanış
	// şartı DEĞİLDİR (hiç müşterisi olmayan sunucuda eski kapı sonsuza dek açık kalırdı);
	// arayüz, eski kapıyı kapatmadan önce hangi rolün yeni kapısının kullanıldığını gösterir.
	IlkGiris  map[string]int64 `json:"ilk_giris,omitempty"`
	Olusturma int64            `json:"olusturma,omitempty"`
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

// Dikişler (testler): nginx kanıtı, paylaşılan proxy gizlisi, istemci IP'si.
var (
	nginxtenMi = httpx.NginxtenMi
	proxyGizli = httpx.ProxySecret
	istemciIP  = httpx.ClientIP
)

// kanitliKapiPort: nginx'ten geldiği KANITLANAN isteğin kapısı ve isteği kabul eden
// GERÇEK port — zorlamadan bağımsız. Başlık biçimi (nginx yazar, bkz. KapiSatiri):
//
//	X-GP-Kapi: <kapı> <$server_port> <proxy gizlisi>
//
// 🔴 Gizli başlığın İÇİNDE: kapı satırı olmayan bir location'a (eski bir yedeğin geri
// yüklenmesi gibi yapılandırma kaymasında) istemcinin kendi gönderdiği X-GP-Kapi
// ulaşsa bile gizliyi bilmediği için geçersizdir — kapı taklit edilemez. Port da
// nginx'in soketinden gelir: reload sırasında eski porttan işlenen bayat istek, yeni
// portun kanıtı SAYILMAZ (GirisiKaydet). Kanıt yoksa, başlık yok/çoklu/bozuksa ("", 0).
func kanitliKapiPort(r *http.Request) (string, int) {
	if !nginxtenMi(r) {
		return "", 0
	}
	v := r.Header.Values(Baslik)
	if len(v) != 1 {
		return "", 0
	}
	f := strings.Fields(v[0])
	gizli := proxyGizli()
	if len(f) != 3 || gizli == "" || subtle.ConstantTimeCompare([]byte(f[2]), []byte(gizli)) != 1 {
		return "", 0
	}
	port, err := strconv.Atoi(f[1])
	if err != nil || port < 1 || port > 65535 {
		return "", 0
	}
	switch f[0] {
	case Admin, Bayi, Musteri, Eski:
		return f[0], port
	}
	return "", 0
}

// KanitliKapi: kanitliKapiPort'un yalnız kapı adı. Açılış ölçümü bunu kullanır:
// zorlama açılmadan ÖNCE zincirin uçtan uca çalıştığı ölçülebilsin.
func KanitliKapi(r *http.Request) string {
	k, _ := kanitliKapiPort(r)
	return k
}

// istemciLoopback: istemci aynı makineden mi (SSH tüneli, açılış ölçümü, yerel araç)?
// istemciIP nginx kanıtı varsa X-Real-IP'ye, yoksa soket adresine bakar.
func istemciLoopback(r *http.Request) bool {
	ip := net.ParseIP(istemciIP(r))
	return ip != nil && ip.IsLoopback()
}

// ZincirKopuk: zorlama açıkken kapısı kanıtlanamayan bir istek geldiğinde çağrılır.
// provisioner bağlar (seyreltilmiş yeniden uzlaştırma + ölçüm); nil → yok.
var ZincirKopuk func()

// Istek: isteğin geldiği kapı (yetki kararları bunu kullanır).
//   - zorlama kapalı → "" (eski davranış: tek kapı, her rol)
//   - kanıtsız istek (doğrudan :8080) / çoklu / tanımsız başlık → Bilinmeyen
//   - nginx'ten geldiği KANITLI ama kapı başlığı HİÇ yok → "" (aşağıya bkz.)
//
// 🔴 KİLİTLENME KORUMASI: kanıtlı istekte başlığın hiç olmaması ancak yapılandırma
// kaymasıyla olur — _panel.conf başlıksız bir sürümle değişmiş (şablonla yeniden
// kurulum, eski yedeğin geri yüklenmesi). Kapı modunda nginx başlığı her zaman yazar;
// istemci onu ne üretebilir ne silebilir (değer gizliyi taşır). Reddetmek, bir sonraki
// ölçüme dek HERKESİ kilitlerdi. Eski davranışa düşülür ve zincir hemen yeniden
// uzlaştırılıp ölçülür (ZincirKopuk). Başlık VAR ama geçersizse (gizlisiz sahte değer)
// reddedilir.
func Istek(r *http.Request) string {
	if !ZorlamaAktif() {
		return ""
	}
	if k := KanitliKapi(r); k != "" {
		return k
	}
	if f := ZincirKopuk; f != nil {
		f()
	}
	if nginxtenMi(r) && len(r.Header.Values(Baslik)) == 0 {
		return ""
	}
	return Bilinmeyen
}

// TipHandler: GET /api/v1/kapi (kimlik doğrulamasız). Arayüz hangi giriş formunu
// göstereceğini buradan öğrenir: "musteri" | "yonetim" | "karma".
//
// 🔴 Admin ve bayi kapıları AYNI yanıtı alır ("yonetim"): hangi portun yönetici
// portu olduğu bu uçtan ayırt EDİLEMEZ. Müşteri portunun müşteri portu olduğu zaten
// gösterdiği arayüzden bellidir.
//
// Zorlama KAPALIYKEN dışarıya "karma" döner: sunucu o sırada her girişi her kapıda
// kabul eder; arayüz tek-port güvenli modunda formu gizleyip kurtarma yolunu
// kapatmamalı. Açılış ölçümü (provisioner, 127.0.0.1'den) gerçek tipi her zaman görür —
// zorlamayı açmadan önce zinciri ölçebilmesi için. Ölçüm OlcumBaslik gönderirse yanıta
// OlcumKaniti eklenir (yalnız loopback istemciye).
func TipHandler(w http.ResponseWriter, r *http.Request) {
	tip := "karma"
	k, port := kanitliKapiPort(r)
	if ZorlamaAktif() || istemciLoopback(r) {
		switch k {
		case Musteri:
			tip = "musteri"
		case Admin, Bayi:
			tip = "yonetim"
		}
	}
	yanit := map[string]string{"tip": tip}
	if n := r.Header.Get(OlcumBaslik); k != "" && olcumNonceGecerli(n) && istemciLoopback(r) {
		yanit["kanit"] = OlcumKaniti(proxyGizli(), n, k, port)
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, yanit)
}

// OlcumKaniti: ölçüm yanıtının kanıtı — HMAC-SHA256(proxy gizlisi, nonce · kapı · port).
// Yanıtı veren tarafın GERÇEKTEN nginx→panel zinciri olduğunu kanıtlar: rol portunu
// nginx dışında bir süreç tutuyorsa (bind yarışı, uygulanamamış reload) ya da isteği
// başka bir porta aktarıyorsa kanıt tutmaz. Kapı adı yanıtta AÇIK dönmez (admin/bayi
// ayırt edilemezliği korunur); ölçen taraf beklediği kapı ve portla doğrular.
func OlcumKaniti(gizli, nonce, kapi string, port int) string {
	m := hmac.New(sha256.New, []byte(gizli))
	fmt.Fprintf(m, "gp-kapi-olcum\x00%s\x00%s\x00%d", nonce, kapi, port)
	return hex.EncodeToString(m.Sum(nil))
}

// olcumNonceGecerli: 32-128 onaltılık karakter (ölçüm 32 bayt üretir).
func olcumNonceGecerli(n string) bool {
	if len(n) < 32 || len(n) > 128 {
		return false
	}
	_, err := hex.DecodeString(n)
	return err == nil
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

// GirisiKaydet: rolün başarılı girişini o rolün kapısının DIŞARIDAN erişilebilir olduğunun
// kanıtı olarak işler — yalnız istek nginx'ten KANITLI olarak rolün ŞU ANKİ portundan
// (başlıktaki gerçek $server_port) ve loopback OLMAYAN bir istemciden geldiyse:
//   - admin: Ayar.AdminDogrulandi — eski kapının kapanabilmesinin şartı. SSH tünelinden
//     ya da reload sırasında eski porttan işlenen bayat istekten gelen giriş kanıt değildir.
//     İstisna: masaüstü modunda yönetici kapısının TEK yolu zaten SSH tünelidir; orada
//     loopback giriş kapının (amaçlandığı biçimde) erişilebilir olduğunun kanıtıdır.
//   - bayi/müşteri: Ayar.IlkGiris[rol] — arayüz "bu kapıdan giriş görüldü mü" gösterir.
//
// Her kayıt ilk seferde bir kez yazılır; sonraki girişler yalnız okur. Hata girişi
// ENGELLEMEZ: kanıt yazılamazsa eski kapı açık kalır (güvenli başarısızlık).
func GirisiKaydet(yol, rol string, r *http.Request, simdi int64) error {
	k, port := kanitliKapiPort(r)
	if k != rol || port == 0 {
		return nil
	}
	if istemciLoopback(r) && (rol != Admin || !masaustuModu()) {
		return nil
	}
	kayitli := func(a *Ayar) bool {
		if rol == Admin {
			return a.AdminDogrulandi != 0
		}
		return a.IlkGiris[rol] != 0
	}
	a, err := Oku(yol)
	if err != nil {
		return err
	}
	if a.RolPortu(rol) != port || kayitli(a) {
		return nil
	}
	return Guncelle(yol, func(x *Ayar) error {
		if x.RolPortu(rol) != port || kayitli(x) {
			return nil
		}
		if rol == Admin {
			x.AdminDogrulandi = simdi
			return nil
		}
		if x.IlkGiris == nil {
			x.IlkGiris = map[string]int64{}
		}
		x.IlkGiris[rol] = simdi
		return nil
	})
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
