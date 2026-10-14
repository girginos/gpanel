package provisioner

// Laravel Toolkit artıkları — kuyruk işleyicisi birimi (girginos-laravel-queue-<id>.service,
// User=<sk>) ve zamanlanmış görev (/etc/cron.d/girginos-laravel-<id>, kullanıcı <sk>).
//
// Üç ayrı sorun, tek kaynak (ölçüm: 181, tek Laravel kuyruk biriminde NRestarts=147.961):
//  1. Birim şablonunda StartLimitIntervalSec=/StartLimitBurst= [Service] bölümündeydi. systemd
//     StartLimitIntervalSec'i yalnız [Unit]'te tanır; [Service]'te "Unknown key" diyerek YOK SAYAR
//     (StartLimitBurst ise eski takma adıyla [Service]'te de UYGULANIR — gerçek VPS'te, systemd
//     257'de ölçüldü: StartLimitIntervalUSec=10s Burst=10). Etkin aralık varsayılan 10 sn kalır;
//     RestartSec=5 ile aralığa en çok 2 başlatma sığar → patlama (10) ASLA aşılmaz → çöken
//     işleyici (Restart=always) sonsuza dek 5 sn'de bir yeniden başlar.
//     Şablon laravel.queueUnit'te düzeltildi; KURULU birimler açılışta yamalanır
//     (laravelBirimYamala: satırlar [Service]'ten [Unit]'e taşınır, değerleri korunur).
//  2. Hesap silinince (Deprovision → userdel) birim ve cron dosyası KALIYORDU; User= artık
//     olmadığından birim status=217/USER ile sonsuz döngüye giriyordu. Kaldırma yalnız Laravel
//     sayfasındaki "kaldır" yolunda vardı. Deprovision artık LaravelArtiklariniSil çağırır —
//     userdel'den ÖNCE: çalışan bir işleyici varken `userdel -r` "kullanıcı süreç kullanıyor"
//     diye (hatası yutulduğundan) sessizce başarısız olurdu.
//  3. Bu düzeltmeden önce silinmiş hesapların kalıntıları: açılışta kullanıcısı sistemde artık
//     olmayan birim ve cron dosyaları süpürülür (LaravelArtiklariniSupurOnStartup).
//
// Kapsam bilerek DAR: yalnız girginos-laravel-queue-<id>.service ile girginos-laravel-<id>
// adlı dosyalar, kullanıcısı c_ önekli geçerli bir hesap adı olanlar. "Kullanıcı yok" kararı
// YALNIZ os/user.UnknownUserError ile verilir: /etc/passwd'in geçici okunamaması canlı bir
// işleyiciyi sildirmez.

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
)

// Laravel Toolkit'in (internal/laravel) yazdığı dosyaların adlandırması ve dizinleri.
// provisioner laravel'i import EDEMEZ (laravel → provisioner yönü var); değerlerin iki tarafta
// aynı kaldığını laravel paketindeki TestLaravelArtik_ProvisionerAdlandirmasiTutarli kilitler.
const (
	// LaravelKuyrukBirimOnEki: birim adı = önek + <domain id> + ".service" (laravel.queueUnitName).
	LaravelKuyrukBirimOnEki = "girginos-laravel-queue-"
	// LaravelCronDosyaOnEki: cron.d dosya adı = önek + <domain id> (laravel.cronDPath).
	LaravelCronDosyaOnEki = "girginos-laravel-"
	// LaravelBirimDizini: kuyruk işleyicisi birimlerinin dizini (laravel.queueUnitDizini).
	LaravelBirimDizini = "/etc/systemd/system"
	// LaravelCronDizini: zamanlanmış görev dizini (laravel.cronDDizin).
	LaravelCronDizini = "/etc/cron.d"
	// LaravelCronAskiDizini: askıdaki hesabın cron dosyasının saklandığı dizin (laravel.laravelCronAskiDizini).
	LaravelCronAskiDizini = "/var/lib/girginospanel/laravel-cron-askida"
)

// Dış etkili adımlar. Testler geçici dizine / sahte kullanıcı sorgusuna çevirir; üretimde SABİT.
// Birim dizini ve systemctl için hesap_birim_aski.go'daki hesapBirimDizini / askiKomut kullanılır.
var (
	laravelCronDizini     = LaravelCronDizini
	laravelCronAskiDizini = LaravelCronAskiDizini
	laravelKullaniciBul   = user.Lookup
)

var (
	reLaravelKuyrukBirim = regexp.MustCompile(`^` + regexp.QuoteMeta(LaravelKuyrukBirimOnEki) + `[0-9]{1,19}\.service$`)
	reLaravelCronDosya   = regexp.MustCompile(`^` + regexp.QuoteMeta(LaravelCronDosyaOnEki) + `[0-9]{1,19}$`)
	reCronEnvSatiri      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*\s*=`)
)

// Okunan dosyaların boyut sınırı (panelin yazdığı dosyalar bir kaç yüz bayttır).
const (
	laravelBirimBoyutSiniri = 1 << 20
	laravelCronBoyutSiniri  = 64 << 10
)

// laravelHesapKullanicisi — panelin hesap adı biçimi (c_ önekli, dar karakter kümesi). Bunun dışındaki
// bir User= değerine (root, nginx, hostuyg app-… kullanıcıları) ASLA dokunulmaz.
func laravelHesapKullanicisi(sk string) bool {
	return strings.HasPrefix(sk, "c_") && gecerliSKAdi(sk)
}

// laravelKullaniciYok — kullanıcı sistemde KESİN yok mu? Yalnız UnknownUserError "yok" sayılır;
// başka her hata (passwd okunamadı…) "belirsiz" = false → dokunma.
func laravelKullaniciYok(sk string) bool {
	_, err := laravelKullaniciBul(sk)
	var yok user.UnknownUserError
	return errors.As(err, &yok)
}

// laravelStartLimitAnahtari — satır `StartLimitIntervalSec=` / `StartLimitBurst=` ise anahtar adı, değilse "".
func laravelStartLimitAnahtari(satir string) string {
	k, _, ok := strings.Cut(strings.TrimSpace(satir), "=")
	if !ok {
		return ""
	}
	switch k = strings.TrimSpace(k); k {
	case "StartLimitIntervalSec", "StartLimitBurst":
		return k
	}
	return ""
}

// laravelBirimYamala — [Service] bölümüne yanlış yazılmış StartLimitIntervalSec=/StartLimitBurst=
// satırlarını [Unit] bölümünün sonuna TAŞIR (değerler aynen korunur; operatör elle değiştirmişse
// onun değeri gider). [Unit]'te o anahtar zaten varsa [Service]'teki kopya yalnız silinir.
// Değişiklik yoksa gövde aynen döner (false); [Unit] bölümü yoksa güvenle dokunulmaz.
// İdempotent: yamalı gövdede ikinci çağrı değişiklik yapmaz.
func laravelBirimYamala(govde string) (string, bool) {
	satirlar := strings.Split(govde, "\n")
	var (
		bolum       string
		unitVar     bool
		unitSon     = -1 // [Unit] bölümünün son boş-olmayan satırı (başlık dahil)
		unitAnahtar = map[string]bool{}
		tasinacak   []int
		deger       = map[string]string{} // anahtar → [Service]'teki (son) satır
	)
	for i, s := range satirlar {
		t := strings.TrimSpace(s)
		if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			bolum = t
			if t == "[Unit]" {
				unitVar, unitSon = true, i
			}
			continue
		}
		a := laravelStartLimitAnahtari(t)
		switch bolum {
		case "[Unit]":
			if t != "" {
				unitSon = i
			}
			if a != "" {
				unitAnahtar[a] = true
			}
		case "[Service]":
			if a != "" {
				tasinacak = append(tasinacak, i)
				deger[a] = t
			}
		}
	}
	if !unitVar || len(tasinacak) == 0 {
		return govde, false
	}
	var eklenecek []string
	for _, a := range []string{"StartLimitIntervalSec", "StartLimitBurst"} {
		if l, ok := deger[a]; ok && !unitAnahtar[a] {
			eklenecek = append(eklenecek, l)
		}
	}
	atla := make(map[int]bool, len(tasinacak))
	for _, i := range tasinacak {
		atla[i] = true
	}
	cikti := make([]string, 0, len(satirlar)+len(eklenecek))
	for i, s := range satirlar {
		if atla[i] {
			continue
		}
		cikti = append(cikti, s)
		if i == unitSon {
			cikti = append(cikti, eklenecek...)
		}
	}
	return strings.Join(cikti, "\n"), true
}

// LaravelKuyrukBirimiYamala — laravelBirimYamala'nın dışa açık hali (laravel paketinin şablon uyum testi için).
func LaravelKuyrukBirimiYamala(govde string) (string, bool) { return laravelBirimYamala(govde) }

// laravelCronKullanicilari — cron.d gövdesindeki görev satırlarının kullanıcı alanları (yorum ve
// ortam değişkeni satırları atlanır; `@reboot` gibi kısaltmalarda kullanıcı 2. alandır).
func laravelCronKullanicilari(govde string) []string {
	var out []string
	for _, s := range strings.Split(govde, "\n") {
		s = strings.TrimSpace(s)
		if s == "" || strings.HasPrefix(s, "#") || reCronEnvSatiri.MatchString(s) {
			continue
		}
		f := strings.Fields(s)
		i := 5
		if strings.HasPrefix(f[0], "@") {
			i = 1
		}
		if len(f) > i+1 { // kullanıcı + komut; eksik satır görev sayılmaz
			out = append(out, f[i])
		}
	}
	return out
}

type laravelCronDosya struct {
	yol          string
	kullanicilar []string
}

// laravelCronDosyalari — dizindeki panel adlı (girginos-laravel-<id>) düz dosyalar ve görev kullanıcıları.
// Kullanıcısı çözülemeyen (görev satırı olmayan) dosya listeye GİRMEZ.
func laravelCronDosyalari(dizin string) []laravelCronDosya {
	girdiler, _ := os.ReadDir(dizin)
	var out []laravelCronDosya
	for _, g := range girdiler {
		if !g.Type().IsRegular() || !reLaravelCronDosya.MatchString(g.Name()) {
			continue
		}
		yol := filepath.Join(dizin, g.Name())
		b, err := sinirliDosyaOku(yol, laravelCronBoyutSiniri)
		if err != nil {
			continue
		}
		if kul := laravelCronKullanicilari(string(b)); len(kul) > 0 {
			out = append(out, laravelCronDosya{yol: yol, kullanicilar: kul})
		}
	}
	return out
}

// laravelKuyrukBiriminiKaldir — işleyiciyi durdurup devre dışı bırakır, birim dosyasını ve drop-in
// dizinini siler. daemon-reload / reset-failed ÇAĞIRANIN işidir (toplu). Durdurma hatası dosyanın
// silinmesini engellemez (birim yüklenmemiş ya da zaten durmuş olabilir); dosya silinemezse HATA.
func laravelKuyrukBiriminiKaldir(birim string) error {
	if !reLaravelKuyrukBirim.MatchString(birim) {
		return fmt.Errorf("Laravel kuyruk birimi adı değil: %q", birim)
	}
	if out, err := askiKomut("systemctl", "disable", "--now", birim); err != nil {
		log.Printf("laravel artık: %s durdurulamadı (dosya yine silinecek): %s: %v", birim, strings.TrimSpace(string(out)), err)
	}
	yol := filepath.Join(hesapBirimDizini, birim)
	if err := os.Remove(yol); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s silinemedi: %w", birim, err)
	}
	if st, err := os.Lstat(yol + ".d"); err == nil && st.IsDir() {
		_ = os.RemoveAll(yol + ".d") // askı koruması vb. drop-in'ler birimle birlikte gider
	}
	return nil
}

// laravelSystemdYenile — tek daemon-reload, ardından (kaldırılan) birimler için reset-failed:
// dosyası silinmiş "failed" birim kaydı systemd'de kalmasın.
func laravelSystemdYenile(kaldirilan []string) error {
	var hata error
	if out, err := askiKomut("systemctl", "daemon-reload"); err != nil {
		hata = fmt.Errorf("systemd yeniden yüklenemedi: %s: %w", strings.TrimSpace(string(out)), err)
	}
	for _, b := range kaldirilan {
		_, _ = askiKomut("systemctl", "reset-failed", b)
	}
	return hata
}

// LaravelArtiklariniSil — hesap silinirken Laravel kuyruk işleyicisini (User=<sk>) ve zamanlanmış
// görevini (cron.d + askı deposu) kaldırır. 🔴 sk YENİDEN KULLANILABİLİR: artık kalırsa aynı sk'yi
// alan yeni kiracıya miras kalır. userdel'den ÖNCE çağrılmalı (çalışan işleyici userdel'i bozar).
func LaravelArtiklariniSil(sk string) {
	if !laravelHesapKullanicisi(sk) {
		return
	}
	birimler, err := hesapBirimleri(sk)
	if err != nil {
		log.Printf("hesap silme: Laravel kuyruk birimleri taranamadı (%s): %v", sk, err)
	}
	var kaldirilan []string
	for _, b := range birimler {
		if !reLaravelKuyrukBirim.MatchString(b) {
			continue
		}
		if err := laravelKuyrukBiriminiKaldir(b); err != nil {
			log.Printf("hesap silme: %v", err)
			continue
		}
		kaldirilan = append(kaldirilan, b)
	}
	for _, dizin := range []string{laravelCronDizini, laravelCronAskiDizini} {
		for _, f := range laravelCronDosyalari(dizin) {
			if !laravelHepsiBu(f.kullanicilar, sk) {
				continue
			}
			if err := os.Remove(f.yol); err != nil && !errors.Is(err, os.ErrNotExist) {
				log.Printf("hesap silme: Laravel zamanlanmış görevi silinemedi (%s): %v", f.yol, err)
			}
		}
	}
	if len(kaldirilan) > 0 {
		if err := laravelSystemdYenile(kaldirilan); err != nil {
			log.Printf("hesap silme: %v", err)
		}
	}
}

func laravelHepsiBu(kullanicilar []string, sk string) bool {
	for _, k := range kullanicilar {
		if k != sk {
			return false
		}
	}
	return len(kullanicilar) > 0
}

// laravelHepsiYok — görev kullanıcılarının HEPSİ panel biçiminde ve sistemde kesin yok.
func laravelHepsiYok(kullanicilar []string) bool {
	for _, k := range kullanicilar {
		if !laravelHesapKullanicisi(k) || !laravelKullaniciYok(k) {
			return false
		}
	}
	return len(kullanicilar) > 0
}

// laravelArtikOzet: bir süpürmenin sonucu (günlük ve testler için).
type laravelArtikOzet struct {
	YetimBirim int // kullanıcısı artık olmayan işleyici birimi: durduruldu + silindi
	YetimCron  int // kullanıcısı artık olmayan zamanlanmış görev (cron.d + askı deposu): silindi
	Yamalanan  int // StartLimit* [Service]'ten [Unit]'e taşınan birim
	Hata       int
}

// laravelArtiklariniSupur — açılış süpürmesi (idempotent):
//   - kullanıcısı sistemde artık olmayan kuyruk birimi → durdur + sil (sonsuz 217/USER döngüsü biter);
//   - kullanıcısı olan, eski biçimli birim → StartLimit satırları [Unit]'e taşınır (çalışan işleyici
//     YENİDEN BAŞLATILMAZ; yalnız daemon-reload, yeni tavan sonraki başlatmalarda geçerli);
//   - kullanıcısı olmayan zamanlanmış görev dosyası (cron.d ve askı deposu) → sil.
func laravelArtiklariniSupur() laravelArtikOzet {
	var oz laravelArtikOzet
	var kaldirilan []string
	yenile := false

	girdiler, err := os.ReadDir(hesapBirimDizini)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		oz.Hata++
		log.Printf("laravel artık süpürmesi: birim dizini okunamadı: %v", err)
	}
	for _, g := range girdiler {
		ad := g.Name()
		if !g.Type().IsRegular() || !reLaravelKuyrukBirim.MatchString(ad) {
			continue
		}
		yol := filepath.Join(hesapBirimDizini, ad)
		b, err := sinirliDosyaOku(yol, laravelBirimBoyutSiniri)
		if err != nil {
			oz.Hata++
			log.Printf("laravel artık süpürmesi: %s okunamadı: %v", ad, err)
			continue
		}
		kul := birimKullanicisiOku(bytes.NewReader(b))
		if !laravelHesapKullanicisi(kul) {
			continue
		}
		if laravelKullaniciYok(kul) {
			if err := laravelKuyrukBiriminiKaldir(ad); err != nil {
				oz.Hata++
				log.Printf("laravel artık süpürmesi: %v", err)
				continue
			}
			oz.YetimBirim++
			kaldirilan = append(kaldirilan, ad)
			yenile = true
			continue
		}
		yeni, tasindi := laravelBirimYamala(string(b))
		if !tasindi {
			continue
		}
		yazildi, err := casYaz(yol, b, []byte(yeni))
		if err != nil {
			oz.Hata++
			log.Printf("laravel artık süpürmesi: %s yamalanamadı: %v", ad, err)
			continue
		}
		if yazildi {
			oz.Yamalanan++
			yenile = true
		}
	}

	for _, dizin := range []string{laravelCronDizini, laravelCronAskiDizini} {
		for _, f := range laravelCronDosyalari(dizin) {
			if !laravelHepsiYok(f.kullanicilar) {
				continue
			}
			if err := os.Remove(f.yol); err != nil && !errors.Is(err, os.ErrNotExist) {
				oz.Hata++
				log.Printf("laravel artık süpürmesi: %s silinemedi: %v", f.yol, err)
				continue
			}
			oz.YetimCron++
		}
	}

	if yenile {
		if err := laravelSystemdYenile(kaldirilan); err != nil {
			oz.Hata++
			log.Printf("laravel artık süpürmesi: %v", err)
		}
	}
	if oz.YetimBirim+oz.YetimCron+oz.Yamalanan+oz.Hata > 0 {
		log.Printf("laravel artık süpürmesi: %d yetim kuyruk birimi + %d yetim zamanlanmış görev silindi, %d birimde StartLimit [Unit]'e taşındı, %d hata",
			oz.YetimBirim, oz.YetimCron, oz.Yamalanan, oz.Hata)
	}
	return oz
}

// LaravelArtiklariniSupurOnStartup: arka planda koşar (provisioner.Init'i BLOKLAMAZ: sonsuz döngüdeki
// birimin durdurulması saniyeler sürebilir) ve çökmeye karşı korumalıdır. Her açılışta güvenle tekrarlanır.
func LaravelArtiklariniSupurOnStartup() {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("laravel artık süpürmesi çöktü: %v", r)
			}
		}()
		laravelArtiklariniSupur()
	}()
}
