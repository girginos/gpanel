package provisioner

// Hosting HESABI askısı — hesabın kendi systemd birimleri (User=<sk>).
//
// Hesabın kodunu PHP-FPM dışında çalıştıran uzun ömürlü birimler: Uygulama
// Çalıştırıcı eklentisinin gosp-app-<sk>-<ad>[-iN|-b].service birimleri
// (Node/Python/konteyner; User=<sk>, Restart=on-failure) ve Laravel kuyruk
// işleyicisi (girginos-laravel-queue-<id>.service, User=<sk>). pkill tek başına
// yetmez: SIGKILL temiz olmayan çıkıştır, systemd birimi RestartSec sonra yeniden
// başlatır; eklenti de açılışta birimleri enable eder, oto-dağıtım
// `systemctl restart` çalıştırır.
//
// Birimler ADINDAN değil, birim dosyasındaki [Service] User= değerinin sk'ye TAM
// eşitliğinden bulunur: ad öneki komşu kiracıyı yakalardı (c_shop'un deseni
// "gosp-app-c_shop-*", c_shop_store'un birimlerini de kapsar). Adım User=<sk>
// olan HER birimi kapsar; hostuyg birimleri kendi app-… kullanıcılarıyla koşar,
// hesaba bağlı değildir → kapsam dışı kalır.
//
// Askıda her birim için önce koruma drop-in'i yazılır:
// <birim>.d/zz-gpanel-hesap-aski.conf, içinde `ConditionPathExists=!<kendisi>`
// → dosya durdukça birim NE yeniden başlatmayla, NE `systemctl start/restart`
// ile, NE sunucu açılışıyla başlar (koşul tutmaz, systemd atlar). Drop-in aynı
// zamanda saklanan önceki durumdur (hesap, etkin, çalışıyor). Sonra birim
// `disable --now` edilir. Askıdan alınca drop-in silinir; YALNIZ önceden etkin
// olan enable, önceden çalışan start edilir.

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// birimAskiDropIn — koruma + saklanan durum dosyasının adı (her birimin .d dizininde).
const birimAskiDropIn = "zz-gpanel-hesap-aski.conf"

// hesapBirimDizini — panelin ve eklentilerin birim yazdığı dizin. Üretimde
// SABİT; testler geçici dizine çevirir (ana makinenin systemd'sine dokunulmaz).
var hesapBirimDizini = "/etc/systemd/system"

// reHesapBirimAdi — işlenebilir birim adı: yol parçası yok, şablon (@) yok.
var reHesapBirimAdi = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:_.-]*\.service$`)

type birimAskiKaydi struct {
	birim, hesap     string
	etkin, calisiyor bool
}

func birimAskiYolu(birim string) string {
	return filepath.Join(hesapBirimDizini, birim+".d", birimAskiDropIn)
}

func birimAskiGovde(k birimAskiKaydi) string {
	bit := func(b bool) string {
		if b {
			return "1"
		}
		return "0"
	}
	return "# gPanel hesap askısı: bu dosya durdukça birim BAŞLAMAZ; askıdan alınınca silinir.\n" +
		"# hesap=" + k.hesap + "\n" +
		"# etkin=" + bit(k.etkin) + "\n" +
		"# calisiyor=" + bit(k.calisiyor) + "\n" +
		"[Unit]\nConditionPathExists=!" + birimAskiYolu(k.birim) + "\n"
}

// birimAskiOku — birimin askı kaydı (drop-in yoksa false).
func birimAskiOku(birim string) (birimAskiKaydi, bool) {
	b, err := os.ReadFile(birimAskiYolu(birim))
	if err != nil {
		return birimAskiKaydi{}, false
	}
	k := birimAskiKaydi{birim: birim}
	for _, s := range strings.Split(string(b), "\n") {
		switch {
		case strings.HasPrefix(s, "# hesap="):
			k.hesap = strings.TrimPrefix(s, "# hesap=")
		case s == "# etkin=1":
			k.etkin = true
		case s == "# calisiyor=1":
			k.calisiyor = true
		}
	}
	return k, true
}

// birimKullanicisi — birim dosyasının [Service] bölümündeki (son) User= değeri.
func birimKullanicisi(yol string) string {
	f, err := os.Open(yol)
	if err != nil {
		return ""
	}
	defer f.Close()
	bolum, kul := "", ""
	sc := bufio.NewScanner(io.LimitReader(f, 1<<20))
	for sc.Scan() {
		s := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(s, "["):
			bolum = s
		case bolum == "[Service]" && strings.HasPrefix(s, "User="):
			kul = strings.TrimSpace(strings.TrimPrefix(s, "User="))
		}
	}
	return kul
}

// hesapBirimleri — birim dizinindeki User=<sk> birimleri (ada göre sıralı).
func hesapBirimleri(sk string) ([]string, error) {
	girdiler, err := os.ReadDir(hesapBirimDizini)
	if err != nil {
		return nil, fmt.Errorf("systemd birimleri okunamadı: %w", err)
	}
	var out []string
	for _, g := range girdiler {
		if !g.Type().IsRegular() || !reHesapBirimAdi.MatchString(g.Name()) {
			continue
		}
		if birimKullanicisi(filepath.Join(hesapBirimDizini, g.Name())) == sk {
			out = append(out, g.Name())
		}
	}
	return out, nil
}

// systemctlDurum — `systemctl is-enabled|is-active` çıktısının ilk satırı.
func systemctlDurum(soru, birim string) string {
	out, _ := askiKomut("systemctl", soru, birim)
	s, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return strings.TrimSpace(s)
}

// HesapBirimleriAski — hesabın User=<sk> systemd birimlerini askıya alır
// (askida=true) ya da önceki durumlarına döndürür. İdempotenttir: yeniden
// askıya almada saklanan durum EZİLMEZ (birim o an bizim yüzümüzden kapalıdır).
func HesapBirimleriAski(sk string, askida bool) error {
	if !strings.HasPrefix(sk, "c_") || !gecerliSKAdi(sk) {
		return fmt.Errorf("geçersiz sistem kullanıcısı: %q", sk)
	}
	if askida {
		return hesapBirimleriKapat(sk)
	}
	return hesapBirimleriAc(sk)
}

func hesapBirimleriKapat(sk string) error {
	birimler, err := hesapBirimleri(sk)
	if err != nil {
		return err
	}
	var hatalar []error
	var korunan []string
	yeni := false
	for _, b := range birimler {
		if k, vardi := birimAskiOku(b); vardi && k.hesap == sk {
			korunan = append(korunan, b) // önceki durum zaten saklı
			continue
		}
		durum := systemctlDurum("is-active", b)
		k := birimAskiKaydi{birim: b, hesap: sk,
			etkin:     systemctlDurum("is-enabled", b) == "enabled",
			calisiyor: durum == "active" || durum == "activating" || durum == "reloading",
		}
		if err := askiDropInYaz(birimAskiYolu(b), birimAskiGovde(k)); err != nil {
			// Durum saklanamadı: devre dışı BIRAKILMAZ (etkinlik bilgisi kaybolurdu;
			// bir sonraki deneme doğru kaydeder), yalnız şimdi durdurulur.
			hatalar = append(hatalar, fmt.Errorf("uygulama birimi %s askıya alınamadı: %w", b, err))
			if out, serr := askiKomut("systemctl", "stop", b); serr != nil {
				hatalar = append(hatalar, fmt.Errorf("uygulama birimi %s durdurulamadı: %s: %w", b, strings.TrimSpace(string(out)), serr))
			}
			continue
		}
		yeni = true
		korunan = append(korunan, b)
	}
	if yeni {
		// Koruma durdurmadan ÖNCE yürürlükte: arada gelen restart da başlatamaz.
		if out, err := askiKomut("systemctl", "daemon-reload"); err != nil {
			hatalar = append(hatalar, fmt.Errorf("systemd yeniden yüklenemedi: %s: %w", strings.TrimSpace(string(out)), err))
		}
	}
	for _, b := range korunan {
		if out, err := askiKomut("systemctl", "disable", "--now", b); err != nil {
			hatalar = append(hatalar, fmt.Errorf("uygulama birimi %s durdurulamadı: %s: %w", b, strings.TrimSpace(string(out)), err))
		}
	}
	return errors.Join(hatalar...)
}

// askiKayitlariniKaldir — sk'nin askı drop-in'lerini siler (boş .d dizini de);
// silinen kayıtları döner. Kayıt dosyanın içindeki hesap= satırıyla TAM
// eşleşir (birim adı önekine bakılmaz).
func askiKayitlariniKaldir(sk string) ([]birimAskiKaydi, []error) {
	yollar, err := filepath.Glob(filepath.Join(hesapBirimDizini, "*.service.d", birimAskiDropIn))
	if err != nil {
		return nil, []error{err}
	}
	var kayitlar []birimAskiKaydi
	var hatalar []error
	for _, y := range yollar {
		birim := strings.TrimSuffix(filepath.Base(filepath.Dir(y)), ".d")
		if !reHesapBirimAdi.MatchString(birim) {
			continue
		}
		k, ok := birimAskiOku(birim)
		if !ok || k.hesap != sk {
			continue
		}
		if err := os.Remove(y); err != nil && !errors.Is(err, os.ErrNotExist) {
			hatalar = append(hatalar, fmt.Errorf("uygulama birimi %s askı koruması kaldırılamadı: %w", birim, err))
			continue
		}
		_ = os.Remove(filepath.Dir(y)) // yalnız boşsa silinir
		kayitlar = append(kayitlar, k)
	}
	return kayitlar, hatalar
}

func hesapBirimleriAc(sk string) error {
	kayitlar, hatalar := askiKayitlariniKaldir(sk)
	if len(kayitlar) == 0 {
		return errors.Join(hatalar...)
	}
	if out, err := askiKomut("systemctl", "daemon-reload"); err != nil {
		hatalar = append(hatalar, fmt.Errorf("systemd yeniden yüklenemedi: %s: %w", strings.TrimSpace(string(out)), err))
	}
	for _, k := range kayitlar {
		if _, err := os.Lstat(filepath.Join(hesapBirimDizini, k.birim)); err != nil {
			continue // birim askıdayken kaldırılmış (uygulama silindi)
		}
		if k.etkin {
			if out, err := askiKomut("systemctl", "enable", k.birim); err != nil {
				hatalar = append(hatalar, fmt.Errorf("uygulama birimi %s etkinleştirilemedi: %s: %w", k.birim, strings.TrimSpace(string(out)), err))
			}
		}
		if k.calisiyor {
			_, _ = askiKomut("systemctl", "reset-failed", k.birim)
			if out, err := askiKomut("systemctl", "start", k.birim); err != nil {
				hatalar = append(hatalar, fmt.Errorf("uygulama birimi %s başlatılamadı: %s: %w", k.birim, strings.TrimSpace(string(out)), err))
			}
		}
	}
	return errors.Join(hatalar...)
}

// askiBirimArtiklariniSil — hesap silinirken askı korumalarını kaldırır. 🔴 sk
// YENİDEN KULLANILABİLİR: kalan koruma, aynı sk'yi alan yeni kiracının aynı
// adlı biriminin hiç başlamamasına yol açardı.
func askiBirimArtiklariniSil(sk string) {
	if !strings.HasPrefix(sk, "c_") || !gecerliSKAdi(sk) {
		return
	}
	kayitlar, hatalar := askiKayitlariniKaldir(sk)
	for _, e := range hatalar {
		log.Printf("hesap silme: %v", e)
	}
	if len(kayitlar) > 0 {
		_, _ = askiKomut("systemctl", "daemon-reload")
	}
}

// askiDropInYaz — drop-in'i atomik yazar (yarım dosya systemd'ye görünmez).
func askiDropInYaz(yol, govde string) error {
	if err := os.MkdirAll(filepath.Dir(yol), 0o755); err != nil {
		return err
	}
	gecici := yol + ".yeni"
	if err := os.WriteFile(gecici, []byte(govde), 0o644); err != nil {
		return err
	}
	if err := os.Rename(gecici, yol); err != nil {
		_ = os.Remove(gecici)
		return err
	}
	return nil
}
