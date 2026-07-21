// Package phpsurum: dinamik PHP surum kesfi + kur/kaldir
package phpsurum

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/httpx"
)

// DesteklenenSurumler: panelin sunduğu PHP sürümleri. 🔴 5.6/7.0-7.3 EOL ve AlmaLinux 10
// Remi'de SAĞLANMAZ → listeden ÇIKARILDI (aksi halde "dnf No match for argument: php73-php-fpm").
// AlmaLinux 10 Remi'nin gerçekten sağladığı: 7.4, 8.0-8.6 (8.6 alpha) + AppStream native 8.3.
// Gerçek kurulabilirlik ayrıca RUNTIME'da dnf ile doğrulanır (Kurulabilir alanı, cache'li) →
// bir sürüm OS'tan kalkarsa panel zarif biçimde "kurulamaz" gösterir, ham dnf hatası patlamaz.
var DesteklenenSurumler = []SurumMeta{
	{"7.4", "74", "remi"},
	{"8.0", "80", "remi"},
	{"8.1", "81", "remi"},
	{"8.2", "82", "remi"},
	{"8.3", "", "appstream"}, // AppStream native
	{"8.3", "83", "remi"},
	{"8.4", "84", "remi"},
	{"8.5", "85", "remi"},
	{"8.6", "86", "remi"},
}

type SurumMeta struct {
	Surum  string `json:"surum"`
	Kod    string `json:"kod"`    // "74", "82" — Remi paket prefix
	Kaynak string `json:"kaynak"` // "remi" | "appstream"
}

type Surum struct {
	SurumMeta
	Yuklu       bool   `json:"yuklu"`
	Kurulabilir bool   `json:"kurulabilir"` // dnf'te (Remi/AppStream) mevcut mu — cache'li
	PoolDir     string `json:"pool_dir,omitempty"`
	SockDir     string `json:"sock_dir,omitempty"`
	Service     string `json:"service,omitempty"`
	PHPBin      string `json:"php_bin,omitempty"`
	GercekSurum string `json:"gercek_surum,omitempty"` // örn "8.3.31"
	ModulSayi   int    `json:"modul_sayi,omitempty"`
	Aciklama    string `json:"aciklama,omitempty"`
}

// ---- Kurulabilirlik cache'i ----
// 🔴 PERF: dnf shell-out'u pahalı (paket başına ~0.85s) ve dnf kilitli/yavaşken (ör. panel
// update dnf çalıştırırken) SANİYELERCE asılabilir. Eskiden paketMevcut() bunu İSTEK
// PATH'inde (senkron, 20s timeout) yapıyordu → TumSurumler() çağıran her endpoint (özellikle
// Domains sayfasının /php/versions'ı) takılıyordu. Artık dnf SADECE arka-plan sweeper'da
// (display için) ya da install-gate'te (canlı otoriter) çağrılır; DISPLAY istek path'i yalnızca
// cache OKUR, ASLA bloklamaz.
//
// 🔴 YANLIŞ-NEGATİF FIX: dnf probe'u ARTIK ÜÇ DURUMLU (available, checked). "dnf kesin YOK dedi"
// (checked=true, available=false) ile "dnf'e SORAMADIM" (checked=false: timeout/kilit/hata) AYRI.
// Önceden ikisi de tek false'a düşüyordu → geçici bir dnf kilidi TÜM cache'i false'a çeviriyor,
// kullanıcı kurulabilir bir sürümü kurmak isteyince YANLIŞ "EOL/yok" 409'u alıyordu.
var (
	availMu    sync.Mutex
	availCache = map[string]bool{} // pkg -> KESİNLEŞMİŞ kurulabilir mi (yalnız checked=true değerler yazılır)
	availAt    time.Time           // son BAŞARILI (en az bir paket checked) sweep zamanı
	sweeperOnce sync.Once

	// dnfProbe: arka-plan sweep sondası (display cache'i doldurur). Test için enjekte edilebilir.
	// Dönüş: (available, checked). checked=false → dnf'e sorulamadı, önceki değeri KORU.
	dnfProbe = func(pkg string) (available bool, checked bool) {
		return dnfProbeCekirdek(pkg, dnfTimeout)
	}
	// dnfCanliProbe: install-gate'in CANLI OTORİTER sondası (uzun timeout). Test için enjekte edilebilir.
	dnfCanliProbe = func(pkg string) (available bool, checked bool) {
		return dnfProbeCekirdek(pkg, dnfAuthTimeout)
	}
)

const (
	availTTL       = 10 * time.Minute // arka-plan sweep periyodu
	dnfTimeout     = 25 * time.Second // sweep sondası per-paket üst sınırı (3s→25s: dnf yavaş/metadata ilk yüklerken 3s çok kısaydı → sürekli yanlış-negatif)
	dnfAuthTimeout = 30 * time.Second // install-gate canlı otoriter sondası üst sınırı
)

// StartAvailabilitySweeper: arka-plan dnf sweep döngüsünü (bir kez) başlatır. Sunucu
// açılışında main'den çağrılır; idempotent. İlk sweep ile periyodik yenilemeyi goroutine'de yapar.
// 🔴 Ayrıca açılışta ASENKRON `dnf makecache` çalıştırır (sweep'i aç bırakmasın) → ilk sweep
// bayat/eksik metadata yüzünden timeout'a düşüp yanlış-negatif üretmesin.
func StartAvailabilitySweeper() {
	sweeperOnce.Do(func() {
		go warmMetadata()
		go sweepLoop()
	})
}

// warmMetadata: açılışta bir kez `dnf makecache` — metadata'yı önden ısıt. Fire-and-forget;
// sweep'ten BAĞIMSIZ goroutine olduğundan sweep'i açlığa düşürmez.
func warmMetadata() {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, "dnf", "-q", "makecache").Run()
}

// sweepLoop: açılışta bir kez + her availTTL'de bir tüm Remi paketlerinin kurulabilirliğini
// dnf ile tarar ve availCache'i günceller. İstek path'inden BAĞIMSIZ çalışır.
func sweepLoop() {
	sweepOnce()
	t := time.NewTicker(availTTL)
	defer t.Stop()
	for range t.C {
		sweepOnce()
	}
}

// sweepOnce: tek bir dnf tarama turu.
// 🔴 ATOMİK-WIPE YOK: yalnızca dnf'in KESİN yanıt verdiği (checked=true) paketleri günceller;
// "sorulamadı" (checked=false: timeout/kilit) paketlerde ÖNCEKİ cache değerini KORUR. Böylece
// geçici bir başarısız tur, önceki doğru true'ları false'a ÇEVİRMEZ (son-bilinen-iyi korunur).
func sweepOnce() {
	// Mevcut cache'in kopyasıyla başla — checked=false olanlar bu değerlerinde kalır.
	availMu.Lock()
	yeni := make(map[string]bool, len(availCache))
	for k, v := range availCache {
		yeni[k] = v
	}
	availMu.Unlock()

	seen := map[string]bool{}
	anyChecked := false
	for _, m := range DesteklenenSurumler {
		if m.Kaynak != "remi" {
			continue // appstream daima mevcut; dnf'e sormaya gerek yok
		}
		pkg := "php" + m.Kod + "-php-fpm"
		if seen[pkg] {
			continue
		}
		seen[pkg] = true
		available, checked := dnfProbe(pkg)
		if checked {
			yeni[pkg] = available // kesin sonuç → yaz
			anyChecked = true
		}
		// checked=false → yeni[pkg] önceki değerinde KALIR (varsa); yoksa yine bilinmiyor.
	}

	availMu.Lock()
	availCache = yeni
	if anyChecked {
		availAt = time.Now()
	}
	availMu.Unlock()
}

// dnfProbeCekirdek: TEK paket için ÜÇ DURUMLU dnf sondası. Dönüş (available, checked):
//   - (true,  true)  → dnf çalıştı ve paketi listeledi (kurulu VEYA depoda mevcut) = KESİN VAR.
//   - (false, true)  → dnf çalıştı ve "No match" dedi = KESİN YOK (EOL/kaldırılmış).
//   - (false, false) → dnf'e SORULAMADI (timeout/kilit/metadata hatası) = BİLİNMİYOR.
//
// "Kesin yok" ile "sorulamadı"yı AYIRT ETMEK bu paketin ÖZÜ: timeout != unavailable.
func dnfProbeCekirdek(pkg string, timeout time.Duration) (available bool, checked bool) {
	// 1) Kurulu mu? (hızlı yol) — başarılıysa kesin mevcut.
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if exec.CommandContext(ctx, "dnf", "-q", "list", "--installed", pkg).Run() == nil {
		return true, true
	}

	// 2) Depoda mevcut mu? Çıktı + ctx ile "kesin yok" ile "sorulamadı"yı ayır.
	ctx2, cancel2 := context.WithTimeout(context.Background(), timeout)
	defer cancel2()
	out, err := exec.CommandContext(ctx2, "dnf", "-q", "list", "--available", pkg).CombinedOutput()
	if err == nil {
		return true, true // dnf çalıştı ve paketi listeledi → kesin mevcut
	}
	// dnf sıfır-olmayan döndü / hata. Timeout/kilit mi yoksa gerçek "No match" mı?
	if ctx2.Err() == context.DeadlineExceeded {
		return false, false // zaman aşımı → sorulamadı
	}
	low := strings.ToLower(string(out))
	if strings.Contains(low, "no match") || strings.Contains(low, "no matching") {
		return false, true // dnf net konuştu: paket yok (EOL/kaldırılmış)
	}
	// Kilit ("waiting for process", "another app is currently holding"), metadata/ağ hatası vb.
	// → emin değiliz. Yanlış-negatif basmamak için "sorulamadı" say.
	return false, false
}

// paketMevcut: phpXX-php-fpm paketi bu OS'ta (Remi) kurulabilir/kurulu mu? — DISPLAY hint'i.
// 🔴 İSTEK PATH'i — ASLA dnf çağırmaz, yalnızca cache okur. AppStream daima var.
// Cache boşsa (ilk boot, sweep henüz bitmemiş): makul varsayılan (false = "henüz bilinmiyor")
// döner ve sweeper'ı garanti eder; istek ASLA saniyelerce beklemez. Sweep bitince gerçek
// değer cache'e yazılır, sonraki istekler doğru sonucu anında alır.
// ⚠️ Bu YALNIZ görüntüleme (Surum.Kurulabilir) içindir; KUR gate'i buna GÜVENMEZ — canlı dnf'e sorar.
func paketMevcut(m SurumMeta) bool {
	if m.Kaynak == "appstream" {
		return true // sistem default her zaman mevcut
	}
	StartAvailabilitySweeper() // idempotent; boot'ta main zaten başlatır, burada güvence
	pkg := "php" + m.Kod + "-php-fpm"
	availMu.Lock()
	v, ok := availCache[pkg]
	availMu.Unlock()
	if ok {
		return v
	}
	// Cache henüz dolmadı → istek bloklanmaz; varsayılan false. Sweep tamamlanınca düzelir.
	return false
}

// kurulabilirlikDenetle: KUR gate'i için CANLI OTORİTER kurulabilirlik kontrolü (uzun timeout).
// Cache DEĞİL — dnf'e o an sorar. Dönüş (available, checked):
//   - checked=true,  available=false → dnf KESİN "No match" dedi → güvenle "EOL/yok" mesajı verilebilir.
//   - checked=false                  → dnf'e sorulamadı (kilit/meşgul) → ASLA "EOL/yok" deme (yanlış-negatif!).
// AppStream daima mevcut.
func kurulabilirlikDenetle(m SurumMeta) (available bool, checked bool) {
	if m.Kaynak == "appstream" {
		return true, true
	}
	pkg := "php" + m.Kod + "-php-fpm"
	return dnfCanliProbe(pkg)
}

// Yollar(meta): yuklenmis olsa olsa nerede olur
func yollar(m SurumMeta) (poolDir, sockDir, service, phpBin string) {
	if m.Kaynak == "appstream" {
		return "/etc/php-fpm.d", "/run/php-fpm", "php-fpm", "/usr/bin/php"
	}
	pre := "/opt/remi/php" + m.Kod + "/root"
	return "/etc/opt/remi/php" + m.Kod + "/php-fpm.d",
		"/var/opt/remi/php" + m.Kod + "/run/php-fpm",
		"php" + m.Kod + "-php-fpm",
		pre + "/usr/bin/php"
}

// Discover: tek bir sürümün dolu metadata'sini doldur
func Discover(m SurumMeta) Surum {
	s := Surum{SurumMeta: m}
	s.PoolDir, s.SockDir, s.Service, s.PHPBin = yollar(m)
	// PHP binary varsa yüklü kabul
	if _, err := os.Stat(s.PHPBin); err == nil {
		s.Yuklu = true
		// Modül sayısı + gerçek sürüm
		if out, err := exec.Command(s.PHPBin, "-v").Output(); err == nil {
			line := strings.SplitN(string(out), "\n", 2)[0]
			// "PHP 8.3.31 (cli) ..."
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				s.GercekSurum = parts[1]
			}
		}
		if out, err := exec.Command(s.PHPBin, "-m").Output(); err == nil {
			lines := strings.Split(string(out), "\n")
			n := 0
			for _, ln := range lines {
				ln = strings.TrimSpace(ln)
				if ln != "" && !strings.HasPrefix(ln, "[") {
					n++
				}
			}
			s.ModulSayi = n
		}
	}
	if m.Kaynak == "appstream" {
		s.Aciklama = "Sistem default (AlmaLinux AppStream)"
	} else {
		s.Aciklama = "Remi modular — geliştirme/test/legacy"
	}
	// Kurulabilirlik (DISPLAY): yüklüyse zaten kurulabilir; değilse cache'e bak (non-blocking).
	// Not: cache "false" dese bile KUR gate'i canlı dnf ile doğrular (yanlış-negatif önleme).
	s.Kurulabilir = s.Yuklu || paketMevcut(m)
	return s
}

// TumSurumler: desteklenen tüm sürümleri tara
func TumSurumler() []Surum {
	out := make([]Surum, 0, len(DesteklenenSurumler))
	for _, m := range DesteklenenSurumler {
		out = append(out, Discover(m))
	}
	// Yüklüleri öne, sonra sürüm sıralı (büyükten küçüğe)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Yuklu != out[j].Yuklu {
			return out[i].Yuklu
		}
		return surumKarsi(out[i].Surum, out[j].Surum) > 0
	})
	return out
}

func surumKarsi(a, b string) int {
	pa := strings.Split(a, ".")
	pb := strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		ia, ib := 0, 0
		fmt.Sscanf(pa[i], "%d", &ia)
		fmt.Sscanf(pb[i], "%d", &ib)
		if ia != ib {
			return ia - ib
		}
	}
	return 0
}

// Default extension bundle (modern PHP icin)
var DefaultBundle = []string{
	"php-fpm",
	"php-cli",
	"php-mysqlnd",
	"php-mbstring",
	"php-bcmath",
	"php-intl",
	"php-gd",
	"php-soap",
	"php-opcache",
	"php-pdo",
	"php-xml",
	"php-zip",
	"php-pgsql",
	"php-ldap",
}

// PaketAdlari: bir sürüm için tüm paket isimlerini hazırla
func PaketAdlari(m SurumMeta) []string {
	pre := "php"
	if m.Kaynak == "remi" {
		pre = "php" + m.Kod + "-php"
	}
	out := make([]string, 0, len(DefaultBundle))
	for _, p := range DefaultBundle {
		out = append(out, strings.Replace(p, "php", pre, 1))
	}
	return out
}

// dnfHataOzet: dnf çıktısından anlamlı son satır(lar)ı süzer (tüm ham dökümü değil).
// "No match for argument" / "Error:" satırlarını öne çıkarır; hiçbiri yoksa son satır.
func dnfHataOzet(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var son string
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		son = ln
		low := strings.ToLower(ln)
		if strings.Contains(low, "no match") || strings.HasPrefix(low, "error") || strings.Contains(low, "nothing provides") {
			return ln
		}
	}
	if son == "" {
		return "bilinmeyen dnf hatası"
	}
	return son
}

// ============================================================================
// Detached kurulum/kaldırma — systemd-run transient unit (PID 1 altında).
//
// 🔴 NEDEN: Eskiden Kur/Kaldir, `dnf install`i HTTP isteğinin goroutine'inde
// r.Context()'e BAĞLI çalıştırıyordu. Kullanıcı sekmeyi kapatınca r.Context()
// iptal olup dnf'i İŞLEM ORTASINDA SIGKILL ediyordu → yarım kurulum + dnf/rpm
// kilidi asılı kalabiliyordu. Artık iş, panelin systemd cgroup'unda DEĞİL,
// `systemd-run` ile PID 1 altında AYRI transient unit'te koşar (istekten
// BAĞIMSIZ). İstemci bağlantısı kopsa da iş tamamlanır. guncelleme.go /
// optimize.go ile AYNI desen; endpoint 202 döner, durum/log ile izlenir.
//
// 🔴 KİLİT GÜVENLİĞİ: wrapper devam eden GERÇEK dnf/rpm transaction'ını ASLA
// öldürmez / kilidi ZORLA kaldırmaz (rpmdb bozulması = sunucu çöker). Yalnızca
// güvenli dnf zamanlayıcılarını (makecache/automatic) durdurur ve gerçek işlem
// varsa BİTMESİNİ bekler; dnf zaten kilit beklemesini kendi yapar.
// ============================================================================

const (
	phpOpLogDir         = "/opt/girginospanel/logs"
	phpOpMarker         = "/opt/girginospanel/logs/php-op.json"
	phpKurUnitPrefix    = "girginospanel-php-kur-"
	phpKaldirUnitPrefix = "girginospanel-php-kaldir-"
)

// phpOpMark: o an aktif olan TEK PHP işini işaretler (resume-on-reopen + tek-iş
// serileştirmesi için — dnf tek transaction'a izin verir).
type phpOpMark struct {
	Islem  string `json:"islem"` // "kur" | "kaldir"
	Surum  string `json:"surum"`
	Kaynak string `json:"kaynak"`
	Unit   string `json:"unit"`
	Log    string `json:"log"`
	Bas    string `json:"bas"`
}

// phpOpKey: sürüm+kaynak için benzersiz, systemd-güvenli birim/dosya anahtarı.
// remi 8.3 → "83-remi", appstream 8.3 → "83-appstream".
func phpOpKey(m SurumMeta) string {
	kod := m.Kod
	if kod == "" {
		kod = strings.ReplaceAll(m.Surum, ".", "")
	}
	return kod + "-" + m.Kaynak
}

func phpSystemctlActive(unit string) string {
	b, _ := exec.Command("systemctl", "is-active", unit).CombinedOutput()
	return strings.TrimSpace(string(b))
}

// phpOpOku: marker'ı oku ve unit hâlâ çalışıyor mu döndür. Marker yoksa/bozuksa
// (mark boş, aktif=false).
func phpOpOku() (mark phpOpMark, aktif bool) {
	b, err := os.ReadFile(phpOpMarker)
	if err != nil {
		return phpOpMark{}, false
	}
	if json.Unmarshal(b, &mark) != nil || mark.Unit == "" {
		return phpOpMark{}, false
	}
	d := phpSystemctlActive(mark.Unit)
	aktif = d == "active" || d == "activating"
	return mark, aktif
}

func phpOpMarkerYaz(mark phpOpMark) error {
	b, _ := json.Marshal(mark)
	tmp := phpOpMarker + ".tmp"
	if err := os.WriteFile(tmp, b, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, phpOpMarker) // atomik
}

// wrapperYaz: SABİT wrapper scriptini atomik yazar (0700, panel-özel).
func wrapperYaz(yol, icerik string) error {
	tmp := yol + ".tmp"
	if err := os.WriteFile(tmp, []byte(icerik), 0o700); err != nil {
		return err
	}
	return os.Rename(tmp, yol)
}

// phpKilitBekleSnippet: SABİT bash — (a) dnf zamanlayıcılarını durdur (kilit
// kaynakları, SADECE PHP kurulum yolunda), (b) devam eden GERÇEK dnf/rpm işlemini
// ÖLDÜRMEDEN bekle. Kullanıcı girdisi İÇERMEZ. Kilidi ASLA zorla kaldırmaz.
const phpKilitBekleSnippet = `echo "▶ dnf otomatik görevleri durduruluyor (kilit kaynakları — sadece PHP kurulum yolu)"
systemctl disable --now dnf-makecache.timer dnf-automatic.timer dnf-automatic-install.timer >/dev/null 2>&1 || true
echo "  ✓ zamanlayıcılar durduruldu (mevcut olmayanlar atlandı)"
echo
echo "▶ Devam eden dnf/rpm işlemi kontrol ediliyor (varsa BEKLENİR — ASLA öldürülmez)"
paket_mesgul() {
  pgrep -x dnf-3       >/dev/null 2>&1 && return 0
  pgrep -x dnf         >/dev/null 2>&1 && return 0
  pgrep -x dnf5        >/dev/null 2>&1 && return 0
  pgrep -x packagekitd >/dev/null 2>&1 && return 0
  if command -v fuser >/dev/null 2>&1 && fuser /var/lib/rpm/.rpm.lock >/dev/null 2>&1; then return 0; fi
  return 1
}
bekle=0; max=600
while [ "$bekle" -lt "$max" ]; do
  if paket_mesgul; then
    echo "  ⏳ Aktif paket işlemi var — bitmesi bekleniyor (öldürülmüyor) ${bekle}s…"
    sleep 5; bekle=$((bekle+5))
  else
    break
  fi
done
if [ "$bekle" -ge "$max" ]; then
  echo "  ⚠ ${max}s doldu — dnf kendi kilit beklemesiyle sürdürecek (kilit ZORLA kaldırılmadı)"
fi
echo "  ✓ devam ediliyor"
echo
`

// phpKurWrapper: bir sürüm için SABİT kurulum scripti. Değerler (paket listesi,
// pool/php.d dizini, servis adı) DesteklenenSurumler allowlist'inden türetilir —
// ham kullanıcı girdisi argümana geçmez. Adım sırası: timer-durdur → kilit-bekle
// → dnf install → mevcut Kur'un TÜM kurulum-sonrası yapılandırması.
func phpKurWrapper(m SurumMeta) string {
	pd, _, svc, _ := yollar(m)
	phpdDir := "/etc/php.d"
	if m.Kaynak == "remi" {
		phpdDir = "/etc/opt/remi/php" + m.Kod + "/php.d"
	}
	pkgs := strings.Join(PaketAdlari(m), " ")

	remiWww := ""
	if m.Kaynak == "remi" {
		// Mevcut davranış: www.conf.disabled varsa ve www.conf yoksa aktive et.
		remiWww = fmt.Sprintf(`# Remi: www.conf.disabled → www.conf (yoksa)
if [ -f '%[1]s/www.conf.disabled' ] && [ ! -f '%[1]s/www.conf' ]; then
  mv '%[1]s/www.conf.disabled' '%[1]s/www.conf' && echo "  ✓ www.conf etkinleştirildi" || true
fi
`, pd)
	}

	return fmt.Sprintf(`#!/usr/bin/env bash
set -uo pipefail
echo "════════ PHP %[1]s (%[2]s) kurulumu — $(date "+%%Y-%%m-%%d %%H:%%M:%%S") ════════"
echo
%[3]secho "▶ Paketler kuruluyor:"
echo "    %[4]s"
if ! dnf install -y %[4]s; then
  echo
  echo "✗ HATA: dnf install başarısız — kurulum durduruldu."
  exit 1
fi
echo "  ✓ paketler kuruldu"
echo
echo "▶ GirginOSPanel yapılandırması"
mkdir -p '%[5]s' 2>/dev/null || true
%[6]smkdir -p '%[7]s' 2>/dev/null || true
cat > '%[7]s/99-girginospanel-input.ini' <<'GOSPINI'
; GirginOSPanel: buyuk form/import (phpMyAdmin, WordPress) - takilma onler
max_input_vars = 10000
GOSPINI
echo "  ✓ max_input_vars ayarı yazıldı"
if systemctl enable --now '%[8]s'; then
  echo "  ✓ %[8]s etkin ve başlatıldı"
else
  echo "  ⚠ %[8]s başlatılamadı (paketler kuruldu; servis elle kontrol edilebilir)"
fi
echo
echo "════════ ✓ PHP %[1]s kurulumu tamamlandı ════════"
`,
		m.Surum,              // 1
		m.Kaynak,             // 2
		phpKilitBekleSnippet, // 3
		pkgs,                 // 4
		pd,                   // 5
		remiWww,              // 6
		phpdDir,              // 7
		svc,                  // 8
	)
}

// phpKaldirWrapper: bir Remi sürümü için SABİT kaldırma scripti. FPM'i durdur →
// timer-durdur/kilit-bekle → dnf remove. Glob dnf'e (bash değil) çözdürülür.
func phpKaldirWrapper(m SurumMeta) string {
	_, _, svc, _ := yollar(m)
	glob := "php" + m.Kod + "-*"
	return fmt.Sprintf(`#!/usr/bin/env bash
set -uo pipefail
echo "════════ PHP %[1]s (remi) kaldırılıyor — $(date "+%%Y-%%m-%%d %%H:%%M:%%S") ════════"
echo
echo "▶ FPM servisi durduruluyor: %[2]s"
systemctl disable --now '%[2]s' >/dev/null 2>&1 || true
echo "  ✓ servis durduruldu"
echo
%[3]secho "▶ Paketler kaldırılıyor: %[4]s"
if ! dnf remove -y '%[4]s'; then
  echo
  echo "✗ HATA: dnf remove başarısız."
  exit 1
fi
echo "  ✓ paketler kaldırıldı"
echo
echo "════════ ✓ PHP %[1]s kaldırıldı ════════"
`,
		m.Surum,              // 1
		svc,                  // 2
		phpKilitBekleSnippet, // 3
		glob,                 // 4
	)
}

// ----- HTTP -----

type Handlers struct {
	DB *sql.DB
}

func (h *Handlers) Liste(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"surumler": TumSurumler(),
	})
}

type opReq struct {
	Surum  string `json:"surum"`
	Kaynak string `json:"kaynak"`
}

func (h *Handlers) Kur(w http.ResponseWriter, r *http.Request) {
	var req opReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz govde")
		return
	}
	var m SurumMeta
	for _, d := range DesteklenenSurumler {
		if d.Surum == req.Surum && d.Kaynak == req.Kaynak {
			m = d
			break
		}
	}
	if m.Surum == "" {
		httpx.WriteError(w, http.StatusBadRequest, "desteklenmeyen surum")
		return
	}

	// 🔴 Zarif ön-kontrol — CANLI OTORİTER dnf (cache DEĞİL). Amaç: yanlış-negatifi ÖNLEMEK.
	// "EOL/yok" mesajını YALNIZCA dnf KESİN "No match" derse (checked && !available) veririz.
	// dnf'e sorulamadıysa (kilit/meşgul) ASLA "yok" demeyiz — ayrı bir "doğrulanamadı" mesajı
	// döneriz; kullanıcıyı yanıltmayız. Böylece geçici dnf kilidi artık yanlış 409 üretmez.
	available, checked := kurulabilirlikDenetle(m)
	if checked && !available {
		httpx.WriteError(w, http.StatusConflict,
			fmt.Sprintf("PHP %s bu işletim sisteminde sağlanmıyor (Remi deposunda yok — büyük olasılıkla EOL). Kurulabilir bir sürüm seçin.", req.Surum))
		return
	}
	if !checked {
		httpx.WriteError(w, http.StatusConflict,
			fmt.Sprintf("PHP %s kurulabilirliği şu an doğrulanamadı (dnf meşgul/kilitli olabilir — ör. başka bir kurulum sürüyor). Lütfen birkaç dakika sonra tekrar deneyin.", req.Surum))
		return
	}

	// Tek PHP işi (dnf tek transaction). Devam eden varsa reddet.
	if mark, aktif := phpOpOku(); aktif {
		httpx.WriteError(w, http.StatusConflict,
			fmt.Sprintf("Başka bir PHP işlemi sürüyor (PHP %s — %s). Bitince tekrar deneyin.", mark.Surum, mark.Islem))
		return
	}

	// 🔴 DETACHED: dnf install artık istekte DEĞİL, systemd-run ile PID 1 altında
	// ayrı transient unit'te koşar → sekme kapansa (r.Context iptal olsa) bile iş
	// tamamlanır. Wrapper: timer-durdur → kilit-bekle → dnf install → kurulum-sonrası.
	key := phpOpKey(m)
	unit := phpKurUnitPrefix + key
	logYol := phpOpLogDir + "/php-kur-" + key + ".log"
	wrapperYol := "/opt/girginospanel/php-kur-" + key + ".sh"

	_ = os.MkdirAll(phpOpLogDir, 0o750)
	if err := wrapperYaz(wrapperYol, phpKurWrapper(m)); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "hazırlanamadı: "+err.Error())
		return
	}
	bas := fmt.Sprintf("=== PHP %s (%s) kurulumu başlatıldı: %s ===\n",
		m.Surum, m.Kaynak, time.Now().Format("2006-01-02 15:04:05"))
	if err := os.WriteFile(logYol, []byte(bas), 0o640); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "log açılamadı: "+err.Error())
		return
	}
	// systemd-run: PID 1 altında transient unit; çıktı append: ile log dosyasına
	// (shell string YOK — tüm argümanlar sabit, r.Context() KULLANILMAZ).
	cmd := exec.Command("systemd-run",
		"--collect",
		"--unit", unit,
		"--description", "GirginOSPanel PHP "+m.Surum+" kurulum",
		"-p", "StandardOutput=append:"+logYol,
		"-p", "StandardError=append:"+logYol,
		wrapperYol)
	if out, err := cmd.CombinedOutput(); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "başlatılamadı: "+strings.TrimSpace(string(out)))
		return
	}
	_ = phpOpMarkerYaz(phpOpMark{
		Islem: "kur", Surum: m.Surum, Kaynak: m.Kaynak,
		Unit: unit, Log: logYol, Bas: time.Now().Format("2006-01-02 15:04:05"),
	})
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{
		"baslatildi": true,
		"surum":      m.Surum,
		"kaynak":     m.Kaynak,
		"islem":      "kur",
		"unit":       unit,
	})
}

func (h *Handlers) Kaldir(w http.ResponseWriter, r *http.Request) {
	var req opReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz govde")
		return
	}
	if req.Kaynak == "appstream" {
		httpx.WriteError(w, http.StatusForbidden,
			"AppStream PHP sistemin default'u, kaldirilamaz")
		return
	}
	var m SurumMeta
	for _, d := range DesteklenenSurumler {
		if d.Surum == req.Surum && d.Kaynak == req.Kaynak {
			m = d
			break
		}
	}
	if m.Surum == "" || m.Kaynak != "remi" {
		httpx.WriteError(w, http.StatusBadRequest, "desteklenmeyen surum")
		return
	}

	// Bu sürümü kullanan domain var mı?
	var count int
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM domains WHERE php_surum=?`, req.Surum).Scan(&count)
	if count > 0 {
		httpx.WriteError(w, http.StatusConflict,
			fmt.Sprintf("Bu surumu kullanan %d domain var, once baska bir surume gec.", count))
		return
	}

	// Tek PHP işi (dnf tek transaction). Devam eden varsa reddet.
	if mark, aktif := phpOpOku(); aktif {
		httpx.WriteError(w, http.StatusConflict,
			fmt.Sprintf("Başka bir PHP işlemi sürüyor (PHP %s — %s). Bitince tekrar deneyin.", mark.Surum, mark.Islem))
		return
	}

	// 🔴 DETACHED: FPM durdurma + dnf remove artık istekte DEĞİL, systemd-run ile
	// PID 1 altında ayrı transient unit'te koşar → sekme kapansa bile iş tamamlanır.
	key := phpOpKey(m)
	unit := phpKaldirUnitPrefix + key
	logYol := phpOpLogDir + "/php-kaldir-" + key + ".log"
	wrapperYol := "/opt/girginospanel/php-kaldir-" + key + ".sh"

	_ = os.MkdirAll(phpOpLogDir, 0o750)
	if err := wrapperYaz(wrapperYol, phpKaldirWrapper(m)); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "hazırlanamadı: "+err.Error())
		return
	}
	bas := fmt.Sprintf("=== PHP %s (remi) kaldırma başlatıldı: %s ===\n",
		m.Surum, time.Now().Format("2006-01-02 15:04:05"))
	if err := os.WriteFile(logYol, []byte(bas), 0o640); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "log açılamadı: "+err.Error())
		return
	}
	cmd := exec.Command("systemd-run",
		"--collect",
		"--unit", unit,
		"--description", "GirginOSPanel PHP "+m.Surum+" kaldırma",
		"-p", "StandardOutput=append:"+logYol,
		"-p", "StandardError=append:"+logYol,
		wrapperYol)
	if out, err := cmd.CombinedOutput(); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "başlatılamadı: "+strings.TrimSpace(string(out)))
		return
	}
	_ = phpOpMarkerYaz(phpOpMark{
		Islem: "kaldir", Surum: m.Surum, Kaynak: m.Kaynak,
		Unit: unit, Log: logYol, Bas: time.Now().Format("2006-01-02 15:04:05"),
	})
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{
		"baslatildi": true,
		"surum":      m.Surum,
		"kaynak":     m.Kaynak,
		"islem":      "kaldir",
		"unit":       unit,
	})
}

// OpDurum: GET /php-surumler/durum — o an çalışan PHP işi (kur/kaldir) var mı,
// hangi sürüm. Sayfa yeniden açılınca devam eden işi yakalamak (resume-on-reopen)
// ve tek-iş serileştirmesi için. guncelleme/optimize durum endpoint'iyle aynı.
func (h *Handlers) OpDurum(w http.ResponseWriter, r *http.Request) {
	mark, aktif := phpOpOku()
	resp := map[string]any{"calisiyor": aktif}
	if mark.Unit != "" {
		resp["surum"] = mark.Surum
		resp["kaynak"] = mark.Kaynak
		resp["islem"] = mark.Islem
		resp["durum"] = phpSystemctlActive(mark.Unit)
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

// OpLog: GET /php-surumler/log — aktif/son PHP işinin log kuyruğu + durum. Log
// dosyası diskte kaldığı için, sekme kapanıp açılsa da kaldığı yerden okunur.
func (h *Handlers) OpLog(w http.ResponseWriter, r *http.Request) {
	mark, aktif := phpOpOku()
	var logStr string
	if mark.Log != "" {
		if b, err := os.ReadFile(mark.Log); err == nil {
			logStr = string(b)
			if len(logStr) > 60000 { // son 60KB yeter
				logStr = logStr[len(logStr)-60000:]
			}
		}
	}
	resp := map[string]any{"log": logStr, "calisiyor": aktif}
	if mark.Unit != "" {
		resp["surum"] = mark.Surum
		resp["kaynak"] = mark.Kaynak
		resp["islem"] = mark.Islem
		resp["durum"] = phpSystemctlActive(mark.Unit)
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}
