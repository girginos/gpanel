package provisioner

// tenantfpm_sokum.go — per-tenant PHP-FPM biriminin YAŞAM SONU.
//
// 🔴 OLAY (181, 2026-10-03): php-fpm-c_e2ebayi1_test.service 1.800.131 kez yeniden
// başladı. Kullanıcısı da domains kaydı da YOK; birim, cfg dizini ve log dizini
// kalmış, `enabled`. Hata metni yanıltıcı ("php-fpm: No such file or directory")
// — ikili duruyor; düşen adım NAMESPACE: BindPaths=/home/<sk> artık yok.
//
// KÖK NEDEN — üç delik üst üste:
//  1. SÖKÜM YARIŞI: oluşturma ve plan değişikliği EnableTenantFPM'i arka planda
//     (go kaynaklimit.UygulaHepsi) çağırır; hesap o sırada silinirse
//     Deprovision→TeardownTenantFPM birim henüz yazılmamışken koşar, ardından
//     EnableTenantFPM birimi yazıp enable eder. (181 denetim kaydı: dgrplan.test
//     17:27:03 oluştur+plan → 17:27:09 sil → 17:27:12 SİLİNMİŞ hesap için
//     "izolasyon kaybı"; cfg dizini bugün hâlâ duruyor.)
//  2. KÖR GÖZCÜ: açılış taraması ve K2 watchdog YALNIZ domains satırlarını gezer
//     — kaydı silinmiş hesabın birimine hiçbir mekanizma bakmaz.
//  3. TAVANSIZ DÖNGÜ: Restart=on-failure + RestartSec=2, systemd varsayılan
//     StartLimit (5 / 10 sn) ile ASLA tetiklenmez (her tur ≥2 sn → 10 sn'de en
//     çok 5 başlatma) → sonsuz döngü (bkz tenantfpm.go "StartLimitIntervalUSec").
//
// ÇÖZÜM: (1) yaşam kilidi + söküm işareti (EnableTenantFPM/TeardownTenantFPM/
// RepairTenantUnitDrift aynı sk kilidinde), (2) sahipsiz birim uzlaştırması
// (açılış + bakım döngüsü), (3) drop-in StartLimit tavanı.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrTenantSokuldu — hesap sökülüyor/silindi ya da sistem kullanıcısı yok:
// per-tenant FPM KURULMAZ, paylaşılana da indirilmez.
var ErrTenantSokuldu = errors.New("hesap sökülüyor/silinmiş — per-tenant PHP-FPM kurulmaz")

// Dış etkili adımlar (testler sahteyle değiştirir; üretimde değerler SABİTTİR).
var (
	sokumKomut = func(ad string, arg ...string) ([]byte, error) { return exec.Command(ad, arg...).CombinedOutput() }

	// tenantKullaniciVar — sistem kullanıcısı var mı (getpwnam/NSS). YALNIZ "böyle
	// kullanıcı yok" yanıtı false döner; başka her hata (NSS erişilemedi …) VAR
	// sayılır: belirsizlikte hiçbir şey sökülmez.
	tenantKullaniciVar = func(sk string) bool {
		_, err := user.Lookup(sk)
		if err == nil {
			return true
		}
		var yok user.UnknownUserError
		return !errors.As(err, &yok)
	}

	// TenantSliceSilKancasi — girginos-<sk>.slice sökümü. kaynaklimit init'te
	// SystemdSliceSil'e bağlanır (kaynaklimit provisioner'ı içe aktardığı için
	// doğrudan çağrı döngüsel import olurdu). nil → slice'a dokunulmaz.
	TenantSliceSilKancasi func(sk string) error

	// sahipsizMinYas — en yeni izi bundan TAZE olan aday atlanır (yarım kalmış
	// bir oluşturmanın ortasına girmemek için). sahipsizSimdi testte sabitlenir.
	sahipsizMinYas = 10 * time.Minute
	sahipsizSimdi  = time.Now
)

// sahipsizTaramaTik — bakım döngüsünde uzlaştırma kaç watchdog tikinde bir koşar
// (fpmWatchdogTick=60 sn → 30 dk).
const sahipsizTaramaTik = 30

// ── Yaşam kilidi + söküm işareti ───────────────────────────────────────────────

var (
	fpmYasamKilitleri sync.Map // sk -> *sync.Mutex
	tenantSokulen     sync.Map // sk -> time.Time (söküm anı)
)

// fpmYasamKilidi — sk başına kurulum/söküm kilidi. fpmSkLock'tan AYRI: o kilit
// rollback yarışını korur ve guardPostStart içinden alınır; yaşam kilidini tutan
// yollar fpmSkLock ALMAZ (kilit sırası her zaman fpmSkLock → yaşam).
func fpmYasamKilidi(sk string) *sync.Mutex {
	v, _ := fpmYasamKilitleri.LoadOrStore(sk, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// TenantSokumde — sk bu panel sürecinde söküldü mü (ve o günden beri yeniden
// sağlanmadı mı)? kaynaklimit.UygulaHepsi slice yazmadan önce de sorar.
func TenantSokumde(sk string) bool {
	_, ok := tenantSokulen.Load(sk)
	return ok
}

// tenantSokumIsaretiKaldir — sk yeniden sağlandı (Provision: kullanıcı açıldı).
func tenantSokumIsaretiKaldir(sk string) { tenantSokulen.Delete(sk) }

// ── Söküm ────────────────────────────────────────────────────────────────────

func izVar(yol string) bool {
	_, err := os.Lstat(yol)
	return err == nil
}

func tenantUnitDropinDir(sk string) string { return tenantUnitPath(sk) + ".d" }

// tenantFPMArtiklariniSok — sk'nin per-tenant FPM izlerini kaldırır: servis
// durdur+disable (wants bağı), birim dosyası, drop-in dizini, daemon-reload,
// reset-failed (rate-limit/failed kaydı), cfg / run / log dizinleri, slice.
// İdempotent. Kilit ALMAZ — çağıran yaşam kilidini tutar (ya da kilidi tutan
// EnableTenantFPM içinden RollbackToSharedFPM ile gelinir). Döner: kaldırılan izler.
func tenantFPMArtiklariniSok(sk string) []string {
	if !strings.HasPrefix(sk, "c_") || !gecerliSKAdi(sk) {
		return nil
	}
	var giden []string
	unit := tenantUnitName(sk)
	// disable --now: durdur + [Install] bağlarını kaldır. Dosya silinmeden ÖNCE
	// (sonra systemd birimi tanımaz, wants bağı yetim kalır).
	_, _ = sokumKomut("systemctl", "disable", "--now", unit)
	if izVar(tenantUnitPath(sk)) {
		if err := os.Remove(tenantUnitPath(sk)); err == nil {
			giden = append(giden, tenantUnitPath(sk))
		} else {
			log.Printf("tenant FPM sokum: %s birim silinemedi: %v", sk, err)
		}
	}
	if d := tenantUnitDropinDir(sk); izVar(d) {
		if err := os.RemoveAll(d); err == nil {
			giden = append(giden, d)
		}
	}
	_, _ = sokumKomut("systemctl", "daemon-reload")
	// Döngüde kalmış birim "failed / not-found" olarak yüklü kalır → temizle.
	_, _ = sokumKomut("systemctl", "reset-failed", unit)
	for _, d := range []string{tenantCfgDir(sk), tenantRunDir(sk), tenantPerSkLogDir(sk)} {
		if izVar(d) {
			if err := os.RemoveAll(d); err == nil {
				giden = append(giden, d)
			}
		}
	}
	if TenantSliceSilKancasi != nil {
		if err := TenantSliceSilKancasi(sk); err != nil {
			log.Printf("tenant FPM sokum: %s slice silinemedi: %v", sk, err)
		}
	}
	return giden
}

// paylasilanHavuzArtiklariniSil — paylaşılan master havuz dosyası + .bak artığı;
// yalnız gerçekten havuzu olan sürümün master'ı yeniden yüklenir.
func paylasilanHavuzArtiklariniSil(sk string) []string {
	if !strings.HasPrefix(sk, "c_") || !gecerliSKAdi(sk) {
		return nil
	}
	var giden []string
	for dizin, servis := range askiHavuzDizini() {
		aktif := filepath.Join(dizin, sk+".conf")
		vardi := izVar(aktif)
		if vardi && os.Remove(aktif) == nil {
			giden = append(giden, aktif)
		}
		if bak := aktif + ".bak"; izVar(bak) && os.Remove(bak) == nil {
			giden = append(giden, bak)
		}
		if vardi && servis != "" {
			_, _ = sokumKomut("systemctl", "reload-or-restart", servis)
		}
	}
	return giden
}

// ── Yeniden başlatma tavanı (StartLimit) ────────────────────────────────────

// Tavan: 300 sn içinde 10 başlatma. Gerekçe:
//   - Varsayılan 5/10 sn, RestartSec=2 ile YAPISAL olarak tetiklenmez (yukarıdaki
//     kök neden 3); 300 sn pencerede ≤30 sn'lik her çökme döngüsü (anında NAMESPACE
//     hatası ~20 sn'de, crash@3s profili ~55 sn'de) 10. denemede durur → birim
//     "failed" olur, watchdog/guard onu zaten "ölü" sayıp ele alır.
//   - Sağlıklı birimler etkilenmez: tavan yalnız BAŞLATMA sayar; çalışan süreç için
//     hiçbir şey değişmez. Panelin kendi restart'ları (EnableTenantFPM, drift
//     onarımı, açılış) öncesinde reset-failed sayacı sıfırlar → yönetici işlemleri
//     tavana TAKILMAZ.
//   - Değerler laravel kuyruk birimiyle aynı (gorevkuyruk.go) — panelde tek ölçü.
//   - Ana birim yerine DROP-IN: ana birimin damgası (renderTenantUnit) değişseydi
//     RepairTenantUnitDrift açılışta TÜM tenant'ları yeniden başlatırdı. Drop-in
//     daemon-reload ile etkin olur, süreç yeniden başlamaz.
const (
	tenantStartLimitAralikSn = 300
	tenantStartLimitPatlama  = 10
	tenantStartLimitDropinAd = "10-gpanel-startlimit.conf"
)

func tenantStartLimitDropin() string {
	return fmt.Sprintf("# gpanel-startlimit: v1 — GirginOSPanel yonetir (tenantfpm_sokum.go); elle degistirme\n"+
		"[Unit]\nStartLimitIntervalSec=%d\nStartLimitBurst=%d\n", tenantStartLimitAralikSn, tenantStartLimitPatlama)
}

// tenantStartLimitYaz — drop-in'i yazar (yoksa/farklıysa). true = değişti (çağıran
// daemon-reload yapar). Birim dosyası yoksa no-op.
func tenantStartLimitYaz(sk string) (bool, error) {
	if !izVar(tenantUnitPath(sk)) {
		return false, nil
	}
	dizin := tenantUnitDropinDir(sk)
	yol := filepath.Join(dizin, tenantStartLimitDropinAd)
	want := tenantStartLimitDropin()
	if cur, err := os.ReadFile(yol); err == nil && string(cur) == want {
		return false, nil
	}
	if err := os.MkdirAll(dizin, 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(yol, []byte(want), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// tenantStartLimitGeriDoldur — kurulu TÜM panel birimlerine (askıdakiler ve
// yetimler dahil: yalnız tavan, başlatma YOK) drop-in'i yazar; değişen varsa TEK
// daemon-reload. Döner: güncellenen birim sayısı.
func tenantStartLimitGeriDoldur() int {
	n := 0
	for _, sk := range panelTenantBirimleri() {
		if ok, err := tenantStartLimitYaz(sk); err != nil {
			log.Printf("StartLimit drop-in %s: %v", sk, err)
		} else if ok {
			n++
		}
	}
	if n > 0 {
		_, _ = sokumKomut("systemctl", "daemon-reload")
		log.Printf("tenant FPM: %d birime yeniden baslatma tavani eklendi (%d/%dsn, restart YOK)",
			n, tenantStartLimitPatlama, tenantStartLimitAralikSn)
	}
	return n
}

// ── Panel imzası ─────────────────────────────────────────────────────────────

var (
	reTenantUnitAdi  = regexp.MustCompile(`^php-fpm-(c_[a-z0-9_]{1,30})\.service$`)
	reTenantSliceAdi = regexp.MustCompile(`^girginos-(c_[a-z0-9_]{1,30})\.slice$`)
)

func tenantSlicePath(sk string) string {
	return filepath.Join(tenantUnitDir, "girginos-"+sk+".slice")
}

// panelBirimiMi — birim dosyası panelin renderTenantUnit'inden mi? Açıklama satırı
// VE ExecStart'taki --fpm-config yolu bu sk'ye ait olmalı (damga satırı eski
// sürümlerde yoktu → tek başına aranmaz).
func panelBirimiMi(sk, icerik string) bool {
	return strings.Contains(icerik, "\nDescription=GirginOSPanel per-tenant PHP-FPM — "+sk+"\n") &&
		strings.Contains(icerik, " --fpm-config "+tenantCfgDir(sk)+"/php-fpm.conf\n")
}

// panelTenantBirimleri — birim dizinindeki panel imzalı php-fpm-c_*.service sk'leri.
func panelTenantBirimleri() []string {
	ents, err := os.ReadDir(tenantUnitDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		m := reTenantUnitAdi.FindStringSubmatch(e.Name())
		if m == nil || e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(tenantUnitDir, e.Name()))
		if err == nil && panelBirimiMi(m[1], string(b)) {
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out
}

// tenantIzleri — sk'nin diskteki per-tenant izleri. imzali: en az bir iz panelin;
// yabanci: VAR OLAN bir iz panelin DEĞİL (o zaman sk'ye hiç dokunulmaz).
func tenantIzleri(sk string) (izler []string, imzali, yabanci bool, enYeni time.Time) {
	not := func(yol string, panelin bool) {
		fi, err := os.Lstat(yol)
		if err != nil {
			return
		}
		izler = append(izler, yol)
		if fi.ModTime().After(enYeni) {
			enYeni = fi.ModTime()
		}
		if panelin {
			imzali = true
		} else {
			yabanci = true
		}
	}
	if b, err := os.ReadFile(tenantUnitPath(sk)); err == nil {
		not(tenantUnitPath(sk), panelBirimiMi(sk, string(b)))
	} else {
		not(tenantUnitPath(sk), false) // okunamayan/özel dosya → yabancı say
	}
	if izVar(tenantCfgDir(sk)) {
		b, _ := os.ReadFile(filepath.Join(tenantCfgDir(sk), "php-fpm.conf"))
		not(tenantCfgDir(sk), strings.Contains(string(b), "\ninclude="+tenantCfgDir(sk)+"/pool.conf\n"))
	}
	if izVar(tenantSlicePath(sk)) {
		b, _ := os.ReadFile(tenantSlicePath(sk))
		not(tenantSlicePath(sk), strings.HasPrefix(string(b), "# GirginOSPanel per-domain resource slice — "+sk+"\n"))
	}
	// run/log dizinleri imza taşımaz: yalnız yaş için (tek başına aday yapmaz).
	for _, d := range []string{tenantRunDir(sk), tenantPerSkLogDir(sk)} {
		if fi, err := os.Lstat(d); err == nil {
			izler = append(izler, d)
			if fi.ModTime().After(enYeni) {
				enYeni = fi.ModTime()
			}
		}
	}
	return
}

// SurumuKullananTenantBirimleri — ExecStart'ı bu PHP sürümünün php-fpm ikilisini
// gösteren panel birimlerinin sk'leri (sürüm kaldırma kapısı için). Sahipsizler de
// listelenir: kaldırma onları önce uzlaştırır.
func SurumuKullananTenantBirimleri(surum string) []string {
	bin := phpMap[normalizePHP(surum)].FpmBin
	if bin == "" {
		return nil
	}
	var out []string
	for _, sk := range panelTenantBirimleri() {
		b, err := os.ReadFile(tenantUnitPath(sk))
		if err == nil && strings.Contains(string(b), "\nExecStart="+bin+" ") {
			out = append(out, sk)
		}
	}
	return out
}

// ── Sahipsiz birim uzlaştırması ───────────────────────────────────────────────

// SahipsizTenantFPM — uzlaştırmanın söktüğü bir hesabın kaydı.
type SahipsizTenantFPM struct {
	SK         string   `json:"sistem_kullanici"`
	Kaldirilan []string `json:"kaldirilan"`
}

// sahipsizAdaylar — birim, cfg dizini ve slice adlarından türeyen sk kümesi.
func sahipsizAdaylar() []string {
	kume := map[string]bool{}
	if ents, err := os.ReadDir(tenantUnitDir); err == nil {
		for _, e := range ents {
			if m := reTenantUnitAdi.FindStringSubmatch(e.Name()); m != nil {
				kume[m[1]] = true
			} else if m := reTenantSliceAdi.FindStringSubmatch(e.Name()); m != nil {
				kume[m[1]] = true
			}
		}
	}
	if ents, err := os.ReadDir(tenantCfgRoot); err == nil {
		for _, e := range ents {
			if e.IsDir() && strings.HasPrefix(e.Name(), "c_") {
				kume[e.Name()] = true
			}
		}
	}
	out := make([]string, 0, len(kume))
	for sk := range kume {
		if gecerliSKAdi(sk) {
			out = append(out, sk)
		}
	}
	sort.Strings(out)
	return out
}

// tenantSahipligi — hesabın yaşadığına dair kanıt: domains satırı (askıdakiler
// dahil — askı satırı SİLMEZ) YA DA sistem kullanıcısı. Biri bile varsa sahipli.
// Sorgu hatası çağırana döner: belirsizlikte hiçbir şey sökülmez.
func tenantSahipligi(db *sql.DB, sk string) (sahipli bool, kanit string, err error) {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM domains WHERE sistem_kullanici=?`, sk).Scan(&n); err != nil {
		return true, "", fmt.Errorf("domains okunamadı: %w", err)
	}
	kul := tenantKullaniciVar(sk)
	kanit = fmt.Sprintf("domains=%d passwd=%v", n, kul)
	return n > 0 || kul, kanit, nil
}

// SahipsizTenantFPMTemizle — kullanıcısı VE domains kaydı olmayan (yalnız panelin
// ürettiği, imzası doğrulanmış) per-tenant FPM izlerini söker, denetime yazar.
//
// Asla dokunulmayanlar: domains satırı olan her hesap (askıdakiler dahil), sistem
// kullanıcısı olan her hesap (yarım-sahipsiz — yalnız günlüğe yazılır: oluşturma
// ortası ya da başarısız userdel olabilir, karar operatörün), imzası tutmayan ya da
// panelin olmayan bir izi bulunan sk, izleri sahipsizMinYas'tan taze olan sk ve
// EnableTenantFPM'i süren sk. Kanıt kilit ALTINDA yeniden alınır.
// DB okunamazsa hiçbir şey sökülmez ve hata döner. İdempotent.
func SahipsizTenantFPMTemizle(db *sql.DB) ([]SahipsizTenantFPM, error) {
	if db == nil {
		return nil, errors.New("panel DB yok — sahipsiz FPM uzlaştırması atlandı")
	}
	var out []SahipsizTenantFPM
	for _, sk := range sahipsizAdaylar() {
		if _, mesgul := fpmInflight.Load(sk); mesgul {
			continue
		}
		_, imzali, yabanci, enYeni := tenantIzleri(sk)
		if !imzali {
			continue
		}
		if yabanci {
			log.Printf("sahipsiz FPM: %s atlandi — panelin olmayan iz var (elle incele)", sk)
			continue
		}
		if sahipsizSimdi().Sub(enYeni) < sahipsizMinYas {
			continue
		}
		sahipli, kanit, err := tenantSahipligi(db, sk)
		if err != nil {
			return out, err
		}
		if sahipli {
			if strings.HasPrefix(kanit, "domains=0 ") {
				log.Printf("sahipsiz FPM: %s yarim-sahipsiz (%s) — kullanici var, domains kaydi yok; DOKUNULMADI", sk, kanit)
			}
			continue
		}
		var kaldirilan []string
		func() {
			yk := fpmYasamKilidi(sk)
			yk.Lock()
			defer yk.Unlock()
			// Kilit beklenirken hesap yeniden sağlanmış olabilir → kanıtı TAZELE.
			s2, k2, e2 := tenantSahipligi(db, sk)
			if e2 != nil || s2 {
				err, kanit = e2, k2
				return
			}
			kaldirilan = tenantFPMArtiklariniSok(sk)
			kaldirilan = append(kaldirilan, paylasilanHavuzArtiklariniSil(sk)...)
		}()
		if err != nil {
			return out, err
		}
		if len(kaldirilan) == 0 {
			continue
		}
		log.Printf("🧹 sahipsiz FPM: %s (%s) sokuldu: %v", sk, kanit, kaldirilan)
		auditSahipsizTemizlik(db, sk, kanit, kaldirilan)
		out = append(out, SahipsizTenantFPM{SK: sk, Kaldirilan: kaldirilan})
	}
	return out, nil
}

// sahipsizTaramasiniKos — açılış/bakım döngüsü sarmalayıcısı (hata yalnız günlüğe).
func sahipsizTaramasiniKos(db *sql.DB, nerede string) {
	if db == nil {
		return
	}
	temiz, err := SahipsizTenantFPMTemizle(db)
	if err != nil {
		log.Printf("sahipsiz FPM uzlastirmasi (%s): %v — hicbir sey sokulmedi/yarida kesildi", nerede, err)
		return
	}
	if len(temiz) > 0 {
		log.Printf("sahipsiz FPM uzlastirmasi (%s): %d hesabin izleri sokuldu", nerede, len(temiz))
	}
}

// auditSahipsizTemizlik — audit_log satırı (detail JSON: json_valid CHECK).
func auditSahipsizTemizlik(db *sql.DB, sk, kanit string, kaldirilan []string) {
	detay, err := json.Marshal(map[string]any{
		"sistem_kullanici": sk,
		"kanit":            kanit,
		"kaldirilan":       kaldirilan,
		"neden":            "sahipsiz per-tenant PHP-FPM izi (sistem kullanicisi ve domains kaydi yok) — Restart dongusu onlemi",
	})
	if err != nil {
		return
	}
	if _, err := db.Exec(
		`INSERT INTO audit_log(actor_user_id, actor_username, ip, action, target, detail, ok, reseller_id)
		 VALUES(NULL, 'sistem', '', 'sistem.fpm_sahipsiz_temizlendi', ?, ?, 1, 0)`,
		sk, string(detay)); err != nil {
		log.Printf("audit fpm_sahipsiz yazilamadi (%s): %v", sk, err)
	}
}
