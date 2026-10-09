package provisioner

// Hosting HESABI askısı — provisioner tarafı: askı durumunu tek kaynaktan okuma
// (hesaplar.HesapAskida), askı vhost gövdesi (ana + alt alan adı AYNI şablon),
// PHP-FPM havuzlarını askıya alma / geri getirme ve havuz başlatan yolların kapısı.

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"girginospanel/internal/hesaplar"
)

// HesapAskida — sk hesabı askıda mı? Paket DB'si (Init) üzerinden TEK kaynağa
// (hesaplar.HesapAskida) sorar. Init'ten önce (pkgDB yok) false.
func HesapAskida(sk string) (bool, error) {
	return hesaplar.HesapAskida(context.Background(), pkgDB, sk)
}

// HesapAskiKapisi — hesabı yeniden çalışır hâle getirebilecek (PHP havuzunu
// yazıp başlatan) yolların girişinde çağrılır. Hesap askıdaysa
// hesaplar.ErrHesapAskida (errors.Is ile tanınır); durum okunamazsa o hata —
// belirsizlikte havuz BAŞLATILMAZ. Hiçbir yan etki yoktur.
func HesapAskiKapisi(sk string) error {
	askida, err := HesapAskida(sk)
	if err != nil {
		return err
	}
	if askida {
		return fmt.Errorf("%w — PHP havuzu askıdan alınınca yeniden başlar", hesaplar.ErrHesapAskida)
	}
	return nil
}

// askiVhostVeri — suspendedVhostTmpl verisi. Ana vhost ve alt alan adı vhost'u
// AYNI şablondan üretilir: 503 askı sayfası, marka varlıkları ve ACME HTTP-01
// muafiyeti (/.well-known/acme-challenge/ → root-sahipli /var/www/acme) iki yolda
// birebir aynıdır; askıdayken sertifika yenilemesi her ikisinde de çalışır.
type askiVhostVeri struct {
	AlanAdi      string // günlük dosyası adı + başlık
	SunucuAdlari string // server_name değeri
	CertPath     string
	KeyPath      string
	SSL          bool
}

// AskiVhostGovde — askıdaki hesabın bir alan adı için 503 vhost gövdesi.
// sunucuAdlari server_name değeridir (ana: "a.com www.a.com", alt: "b.a.com").
// Sertifika (ikisi de dolu) verilirse 443 bloğu da 503 döner.
func AskiVhostGovde(alanAdi, sunucuAdlari, certPath, keyPath string) (string, error) {
	var buf bytes.Buffer
	if err := suspendedVhostTmpl.Execute(&buf, askiVhostVeri{
		AlanAdi: alanAdi, SunucuAdlari: sunucuAdlari,
		CertPath: certPath, KeyPath: keyPath, SSL: certPath != "" && keyPath != "",
	}); err != nil {
		return "", fmt.Errorf("askı vhost şablonu: %w", err)
	}
	return buf.String(), nil
}

// ── PHP-FPM havuzlarının askısı ─────────────────────────────────────────────

// AskiHavuzEki — askıdaki hesabın paylaşılan-master havuz dosyası bu ekle
// saklanır: php-fpm yalnız "*.conf" içerir (.bak ile aynı ilke) → havuz YÜKLENMEZ,
// içerik ise bayt bayt korunur ve askıdan alınınca aynen geri gelir.
const AskiHavuzEki = ".askida"

// Dış etkili adımlar (systemctl, restorecon, havuz dizinleri, per-tenant
// servis durumu). Testler sahteyle değiştirir: ana makinenin php-fpm'ine ve
// /etc dizinlerine dokunmadan sınanır. Üretimde değerler SABİTTİR.
var (
	askiKomut       = func(ad string, arg ...string) ([]byte, error) { return exec.Command(ad, arg...).CombinedOutput() }
	askiTenantAktif = TenantFPMActive
	askiSoketBekle  = func(sk string) bool { return waitForSocket(tenantSocket(sk), 6*time.Second) }
	askiPaylasilana = RollbackToSharedFPM
	askiHavuzDizini = paylasilanHavuzDizinleri
)

// paylasilanHavuzDizinleri — paylaşılan php-fpm master'larının havuz dizini →
// servis adı (her kurulu olabilecek sürüm; aynı dizin bir kez).
func paylasilanHavuzDizinleri() map[string]string {
	m := map[string]string{}
	for _, ay := range phpMap {
		if ay.PoolDir != "" && ay.Service != "" {
			m[ay.PoolDir] = ay.Service
		}
	}
	return m
}

// gecerliHavuzAdi — havuz adı dosya adına girer: hesabın kendisi (sk) ya da
// hesabın alt alan havuzu (php.SubPoolName: sk + "_s" + id; id ondalık, baştaki
// sıfırsız). 🔴 TAM eşleşme: önek denetimi komşu kiracıyı yakalardı — c_shop için
// "c_shop_store" (shop-store.com) ya da "c_shop_s5x" hesabın havuzu DEĞİLDİR.
func gecerliHavuzAdi(sk, ad string) bool {
	if !gecerliSKAdi(ad) {
		return false
	}
	if ad == sk {
		return true
	}
	id, ok := strings.CutPrefix(ad, sk+"_s")
	if !ok || id == "" || id[0] == '0' {
		return false
	}
	for _, c := range id {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// askiHesapVar — ad bir sistem kullanıcısı (yaşayan hesap) mı? Testler sahteler.
var askiHesapVar = userExists

// PHPHavuzAski — hesabın TÜM PHP-FPM havuzlarını askıya alır (askida=true) ya da
// geri getirir. İdempotenttir.
//
//   - Per-tenant FPM (Seçenek A): `systemctl disable --now` — unit dosyası
//     KALIR (TenantFPMActive true kalır → vhost soketi değişmez) ama servis ne
//     şimdi ne sunucu açılışında çalışır. Geri: `enable --now` + soket beklenir;
//     başlamazsa mevcut güvenli desen (RollbackToSharedFPM) — site paylaşılan
//     master'da açılır.
//   - Paylaşılan master havuzları: <PoolDir>/<ad>.conf → <ad>.conf.askida +
//     master reload. ad ∈ {sk} ∪ ekHavuzlar (alt alan adı havuzları). Geri: ters
//     yeniden adlandırma + reload. (pkill tek başına yetmez: master havuzu
//     yüklü kaldıkça işçileri yeniden doğurur; pm=static/dynamic ise istek
//     beklemeden.)
//
// Askıdayken havuzu yeniden başlatabilecek yollar kapalıdır: HesapAskiKapisi
// (writePoolValidated — SetPHPVersion/açılış onarımı dahil —, EnableTenantFPM,
// RollbackToSharedFPM, drift onarımları), tenantFPMBakimi (açılış taraması, watchdog); PHP ayarı
// (php.PutAyarlar) ve alt alan üreticisi (subdomain) askıyı tek kaynaktan okur.
func PHPHavuzAski(db *sql.DB, domainID int64, sk, surum string, ekHavuzlar []string, askida bool) error {
	if !strings.HasPrefix(sk, "c_") || !gecerliSKAdi(sk) {
		return fmt.Errorf("geçersiz sistem kullanıcısı: %q", sk)
	}
	adlar := []string{sk}
	for _, a := range ekHavuzlar {
		if !gecerliHavuzAdi(sk, a) {
			return fmt.Errorf("geçersiz havuz adı: %q", a)
		}
		adlar = append(adlar, a)
	}
	var hatalar []error
	if err := paylasilanHavuzlarAski(adlar, askida); err != nil {
		hatalar = append(hatalar, err)
	}
	if askiTenantAktif(sk) {
		if err := tenantFPMAski(db, domainID, sk, surum, askida); err != nil {
			hatalar = append(hatalar, err)
		}
	}
	return errors.Join(hatalar...)
}

// paylasilanHavuzlarAski — paylaşılan master havuz dosyalarını ekle/ekten çıkar;
// değişen her master'ı bir kez reload eder.
func paylasilanHavuzlarAski(adlar []string, askida bool) error {
	dizinler := askiHavuzDizini()
	sirali := make([]string, 0, len(dizinler))
	for d := range dizinler {
		sirali = append(sirali, d)
	}
	sort.Strings(sirali)
	var hatalar []error
	for _, dizin := range sirali {
		degisti := false
		for _, ad := range adlar {
			etkin := filepath.Join(dizin, ad+".conf")
			sakli := etkin + AskiHavuzEki
			kaynak, hedef := etkin, sakli
			if !askida {
				kaynak, hedef = sakli, etkin
			}
			if _, err := os.Lstat(kaynak); err != nil {
				continue // bu sürümde bu havuz yok / zaten istenen durumda
			}
			if _, err := os.Lstat(hedef); err == nil {
				// İki kopya: hedefteki GÜNCELDİR (askıdan alırken başka bir yol
				// .conf yazmış olabilir; askıya alırken .askida eski bir artıktır).
				if askida {
					if err := os.Rename(kaynak, hedef); err != nil {
						hatalar = append(hatalar, fmt.Errorf("php havuzu %s askıya alınamadı: %w", ad, err))
						continue
					}
				} else if err := os.Remove(kaynak); err != nil {
					hatalar = append(hatalar, fmt.Errorf("php havuzu %s artığı silinemedi: %w", ad, err))
					continue
				}
				degisti = true
				continue
			}
			if err := os.Rename(kaynak, hedef); err != nil {
				hatalar = append(hatalar, fmt.Errorf("php havuzu %s: %w", ad, err))
				continue
			}
			degisti = true
		}
		if degisti {
			svc := dizinler[dizin]
			if out, err := askiKomut("systemctl", "reload-or-restart", svc); err != nil {
				hatalar = append(hatalar, fmt.Errorf("php-fpm (%s) yeniden yüklenemedi: %s: %w", svc, strings.TrimSpace(string(out)), err))
			}
		}
	}
	return errors.Join(hatalar...)
}

// askiHavuzArtiklariniSil — hesap silinirken askıda saklanan havuz dosyalarını
// (<sk>.conf.askida ve alt alan <sk>_s<id>.conf.askida) her paylaşılan master
// dizininden kaldırır. 🔴 sk YENİDEN KULLANILABİLİR (Deprovision): artık kalırsa
// aynı sk'yi alan yeni kiracı askıdan alınınca eski kiracının havuzu (.askida →
// .conf) paylaşılan master'a yüklenirdi. Yüklenmeyen dosyalar olduğundan reload
// gerekmez.
//
// 🔴 Kiracılar arası silme yok: glob adayları gecerliHavuzAdi ile TAM süzülür
// (c_shop silinirken c_shop_store.conf.askida'ya dokunulmaz). Ayrıca alt havuz
// adı sk_s<id> başka bir hesabın adıyla çakışabilir (foo.com → c_foo'nun 5
// numaralı alt havuzu, foo-s5.com → c_foo_s5 hesabı: ikisi de c_foo_s5.conf):
// o ada sahip hesap yaşıyorsa dosya ONUN sayılır ve silinmez (en kötü ihtimalle
// artık kalır, komşunun askıdaki havuzu kaybolmaz).
func askiHavuzArtiklariniSil(sk string) {
	if !strings.HasPrefix(sk, "c_") || !gecerliSKAdi(sk) {
		return
	}
	for dizin := range askiHavuzDizini() {
		adaylar, _ := filepath.Glob(filepath.Join(dizin, sk+"_s*.conf"+AskiHavuzEki))
		adaylar = append(adaylar, filepath.Join(dizin, sk+".conf"+AskiHavuzEki))
		for _, yol := range adaylar {
			ad := strings.TrimSuffix(filepath.Base(yol), ".conf"+AskiHavuzEki)
			if !gecerliHavuzAdi(sk, ad) || (ad != sk && askiHesapVar(ad)) {
				continue
			}
			if err := os.Remove(yol); err != nil && !errors.Is(err, os.ErrNotExist) {
				log.Printf("hesap silme: askıdaki havuz artığı silinemedi (%s): %v", ad, err)
			}
		}
	}
}

// tenantFPMAski — per-tenant FPM servisini durdurur+devre dışı bırakır / geri açar.
func tenantFPMAski(db *sql.DB, domainID int64, sk, surum string, askida bool) error {
	unit := tenantUnitName(sk)
	if askida {
		// Watchdog (K2) askıdaki hesabı zaten atlar; yine de durdurma penceresinde
		// "ölü servis" sanmasın.
		fpmInflight.Store(sk, struct{}{})
		defer fpmInflight.Delete(sk)
		fpmAskiGecis.Store(sk, struct{}{})
		defer fpmAskiGecis.Delete(sk)
		if out, err := askiKomut("systemctl", "disable", "--now", unit); err != nil {
			return fmt.Errorf("php-fpm servisi durdurulamadı: %s: %w", strings.TrimSpace(string(out)), err)
		}
		return nil
	}
	// Askidan alma: servis baslayip soket gorunene kadar sk "ucusta" + "aski gecisinde": K1 izleyicisi ve K2
	// watchdog bu aralikta servisin gecici olarak OLU gorunmesini crash-loop sanip tenant'i paylasilan FPM'e
	// indirmesin (bayat surum + izolasyon kaybi). Askiya alma yonu (yukarida) fpmInflight'i zaten tasir.
	fpmInflight.Store(sk, struct{}{})
	defer fpmInflight.Delete(sk)
	fpmAskiGecis.Store(sk, struct{}{})
	defer fpmAskiGecis.Delete(sk)
	if out, err := askiKomut("systemctl", "enable", "--now", unit); err != nil {
		log.Printf("askıdan alma: %s başlatılamadı (%s) → paylaşılan FPM'e geri alınıyor", sk, strings.TrimSpace(string(out)))
		if rerr := askiPaylasilana(db, domainID, sk, surum, "askidan alma: systemctl enable --now basarisiz"); rerr != nil {
			return fmt.Errorf("php-fpm servisi başlatılamadı ve paylaşılan düzene alınamadı: %w", rerr)
		}
		return nil
	}
	if !askiSoketBekle(sk) {
		log.Printf("askıdan alma: %s soketi oluşmadı → paylaşılan FPM'e geri alınıyor", sk)
		if rerr := askiPaylasilana(db, domainID, sk, surum, "askidan alma: FPM socket olusmadi"); rerr != nil {
			return fmt.Errorf("php-fpm soketi oluşmadı ve paylaşılan düzene alınamadı: %w", rerr)
		}
		return nil
	}
	// fcontext kuralı EnableTenantFPM'de kalıcı kaydedildi; yeni runtime dizinini etiketle.
	_, _ = askiKomut("restorecon", "-R", tenantRunDir(sk))
	return nil
}
