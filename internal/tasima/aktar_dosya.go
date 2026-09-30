package tasima

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"girginospanel/internal/kiraci"
)

// dosyalariAktar — HesapAktar'in 2. adimi: belge kokunu (ve docroot-disi
// uygulama dizinlerini) kaynaktan hedefe tasir (dizinAktar), Laravel sonrasi
// duzeltmeleri uygular. Hata donerse HesapAktar kalemi basarisiz sayar (yeni
// olusturulan hesap geri alinir).
//
// 🔴 Hedef (webRoot, ev dizini) kiracının ağacıdır ve içe aktarılan her şey
// güvenilmezdir: kök bu ağaca yol dizgesiyle YAZMAZ — yerleşim kiracı
// kimliğinde, sonrası ev köküne kilitli os.Root altında (bkz. internal/kiraci).
func (h *Handlers) dosyalariAktar(ctx context.Context, k *Kaynak, hs Hesap, ay Ayarlar, sk, webRoot, alanAdi string,
	sonuc *AktarSonuc, log func(string, ...any)) error {
	if ay.Dosyalar && strings.TrimSpace(hs.WebRoot) == "" {
		// 🔴 KENDİ web kök dizini OLMAYAN alan adı (yönlendirme/hosting-siz alt alan).
		// Kesif eskiden ANA domainin httpdocs'una düşüyordu → alt alan ANA domainin
		// TÜM dosyalarını (ör. 15GB WP) alıyordu (canlı bug: wp.girgin.net.tr ana
		// girgin.net.tr'yi aldı). Artık boş bırakılır; burada SESSİZCE ATLA + uyar.
		log("bu alan adinin kendi web kok dizini yok (yonlendirme/hosting-siz) — dosya tasinmadi")
		sonuc.Uyarilar = append(sonuc.Uyarilar, "Dosya yok — taşınmadı")
	} else if ay.Dosyalar {
		uzak := strings.TrimSpace(hs.WebRoot)
		if !gecerliUzakYol(uzak) {
			return fmt.Errorf("kaynak web kok dizini gecersiz")
		}
		ki, err := kiraciAl(sk)
		if err != nil {
			return err
		}
		kok, err := ki.Goreli(webRoot)
		if err != nil {
			return fmt.Errorf("hedef web kok dizini kiracinin ev dizininde degil")
		}
		uid, gid, err := uidGid(ki.Kul)
		if err != nil {
			return fmt.Errorf("hedef sistem kullanicisi bulunamadi")
		}
		ev, err := ki.EvAc()
		if err != nil {
			return fmt.Errorf("hedef ev dizini acilamadi")
		}
		defer ev.Close()

		log("dosyalar aktariliyor: %s → %s", uzak, webRoot)
		bayt, atlanan, err := dizinAktar(ctx, k, ki, uid, gid, uzak, kok,
			"--exclude=.git/", "--exclude=*.sock", "--exclude=.cpanel/")
		ozelDosyaUyar(atlanan, sonuc, log)
		if err != nil {
			return fmt.Errorf("dosya aktarimi: %w", err)
		}
		sonuc.DosyaBayt = bayt
		log("dosyalar tamam (%.1f MB)", float64(sonuc.DosyaBayt)/(1024*1024))
		// 🔴 DOCROOT-DISI BAGIMLILIK (Laravel/Symfony): uygulama kodu belge kokunun
		// DISINDA durur (public_html/index.php -> ../laravel_11/vendor/autoload.php).
		// Yalniz docroot tasinirsa site 500 verir. index.php'deki "../<dizin>/"
		// referanslarini tara, kaynaktan o dizinleri de getir (ev'in <dizin>'ine).
		for _, ust := range docrootDisiBagimlilik(ev, kok, uid) {
			uzakUst := filepath.Join(filepath.Dir(uzak), ust)
			if !gecerliUzakYol(uzakUst) {
				continue
			}
			log("uygulama dizini (belge koku disi) aktariliyor: %s → %s", uzakUst, filepath.Join(ki.Ev, ust))
			_, atlanan, err := dizinAktar(ctx, k, ki, uid, gid, uzakUst, ust, "--exclude=.git/", "--exclude=*.sock")
			ozelDosyaUyar(atlanan, sonuc, log)
			if err != nil {
				log("uyari: %s aktarilamadi: %v", ust, kisalt(temizHata(err.Error(), k.Parola), 120))
				sonuc.Uyarilar = append(sonuc.Uyarilar, "Uygulama dizini eksik: "+ust)
				continue
			}
			// Laravel ise: APP_URL'i hedef domaine cek, cache temizle, .env'i 0600 yap.
			laravelSonrasiDuzelt(ev, ust, alanAdi, uid, log)
		}
		// Docroot'un KENDISI Laravel ise (public_html/artisan) ayni duzeltmeler.
		laravelSonrasiDuzelt(ev, kok, alanAdi, uid, log)
	}
	return nil
}

// ozelDosyaUyar — kiracıya devredilmeden silinen özel dosyaları (aygıt/fifo/
// soket) görünür kılar. Adlar kaynaktan gelir (güvenilmez) → yalnız sayı.
func ozelDosyaUyar(n int, sonuc *AktarSonuc, log func(string, ...any)) {
	if n == 0 {
		return
	}
	log("uyari: kaynaktan gelen %d ozel dosya (aygit/fifo/soket) tasinmadi", n)
	sonuc.Uyarilar = append(sonuc.Uyarilar, fmt.Sprintf("%d özel dosya (aygıt/fifo/soket) taşınmadı", n))
}

// ---------------------------------------------------------------------------
// Hazırlık alanı: uzak rsync → sarmalayıcı (kök, 0700) içine → süz + devret →
// kiracı rsync
// ---------------------------------------------------------------------------

// hazirlikKoku — uzak rsync'in indiği alan: kiracı ağaçlarının DIŞINDA ama
// /home ile aynı dosya sisteminde. Kök sahipli 0711: kiracı listeleyemez,
// yalnız kendisine devredilmiş iş dizinine adıyla girer. Testler değiştirir.
var hazirlikKoku = "/home/.gosp-tasima"

// hazirlikIcerik — iş dizininin (sarmalayıcı) içindeki rsync hedefi.
//
// 🔴 Uzak rsync kök olarak `-a` ile koşar; sahiplik taşındığında (--numeric-ids)
// hedef dizinin KENDİ sahibini ve alt girdilerin sahiplerini KAYNAK seçer
// (181'de ölçüldü: 0700 0:0 açılan hedef aktarımdan sonra 4242:4242). Seçilen
// uid yerel bir hesaba denk gelirse o hesap devirden önce ağaca girip ara
// dizini bağa çevirebilir; kökün yol tabanlı devir yürüyüşü (lchown) bağı
// izlerdi. Bu yüzden rsync hedefi sarmalayıcının İÇİNDEDİR: rsync sarmalayıcıya
// dokunmaz, sarmalayıcı devrin sonuna kadar yalnız kökündür (0700) → rsync
// bayraklarından (sahiplik taşınsa da taşınmasa da) bağımsız olarak içerikte ne
// sahip/izin olursa olsun başka hiçbir hesap içeri giremez.
const hazirlikIcerik = "d"

// hazirlikOmru — adındaki zamandan bu kadar eski hazırlık dizini yarıda kalmış
// (panel yeniden başlamış) bir işindir: canlı bir kalem hesapTimeout'u aşamaz.
const hazirlikOmru = hesapTimeout + time.Hour

// lchown — hazırlık alanının sahiplik devri. Testler çağrı SIRASINI kaydeder.
var lchown = os.Lchown

// Yerleşim adımlarının süre sınırı.
//
// 🔴 Yerleşim KİRACI kimliğinde koşar: kiracı kendi sürecini SIGSTOP ile
// dondurabilir. Tek sınır hesap bağlamı (hesapTimeout) olsaydı tek taşıma yuvası
// saatlerce dolu kalırdı. Çocuğu durunca kök runuser da kendini durdurur
// (181'de ölçüldü: runuser durum T), SIGTERM beklemede kalır; adım süresi
// dolunca kiraci.Komut iptalBekleme sonunda runuser'ı SIGKILL'le kapatır ve
// adım hata ile döner (durmuş kiracı süreci kiracının kendi sorunudur). Süre:
// sabit taban + boyuta orantılı pay (yerel diskten diske kopyaya göre çok
// kötümser hız), hesap bağlamının yarısıyla sınırlı; gerçek üst sınır
// süre + iptalBekleme.
const (
	yerlesimTaban = 10 * time.Minute
	yerlesimHizi  = 10 << 20 // bayt/sn
	yerlesimTavan = hesapTimeout / 2
)

// yerlesimSuresi — bayt büyüklüğündeki içeriğin kiracı adımı için süre. Testler
// kısaltır.
var yerlesimSuresi = func(bayt int64) time.Duration {
	return min(yerlesimTaban+time.Duration(bayt/yerlesimHizi)*time.Second, yerlesimTavan)
}

var errYerlesimSuresi = errors.New("yerlesim adimi zaman asimina ugradi")

// kiraciAdim — argv'yi kiracı kimliğinde en çok sure boyunca çalıştırır.
func kiraciAdim(ctx context.Context, sure time.Duration, ki kiraci.Kiraci, argv ...string) error {
	ctx, iptal := context.WithTimeout(ctx, sure)
	defer iptal()
	err := ki.Komut(ctx, argv...).Run()
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errYerlesimSuresi
	}
	return err
}

// dizinAktar — uzak dizini kiracının ev'e göre hedefRel dizinine iki adımda taşır:
//  1. kök, uzak rsync'i YALNIZ kendisinin erişebildiği yeni iş dizininin
//     (sarmalayıcı, 0700) içine (hazirlikIcerik) çeker — bu sırada kiracı ya da
//     kaynağın sahip yaptığı herhangi bir yerel hesap hiçbir şeye dokunamaz,
//     kök kiracı ağacında yol çözmez;
//  2. içerik süzülüp kiracıya devredilir (hazirlikDevret) ve KİRACI kimliğinde
//     rsync ile hedefe yerleştirilir: kiracının kurduğu bağlar/takaslar
//     çekirdeğin DAC'ı ile kiracının kendi yetkisine iner; dosyalar doğrudan
//     kiracıya ait oluşur (chown yok, disk kotası kiracıya işler).
//
// 🔴 KOTA TEK KAT: devirle birlikte hazırlık içeriği (S bayt, N inode) kiracının
// kotasına yazılır; kiracı rsync'i aynı içeriği bir kez daha yazsaydı tepe
// kullanım mevcut+2S olurdu → ~Q/2 üstü siteler EDQUOT ile düşer, yeni hesap geri
// alınırdı. Bu yüzden yerleşim --remove-source-files ile TAŞIR: her dosya hedefe
// yazılıp doğrulandıktan sonra hazırlıktan silinir → tepe ≈ mevcut + S + rsync
// boru hattında uçuştaki içerik (181'de 32 MB'lık 64 dosyada ~7 MB; S'ye değil
// tamponlara bağlı); boşalan dizinleri iş sonunda hazırlık silinirken kök süpürür.
// Aynı fs içinde yeniden adlandırma (rename) seçilmedi: yalnız hedef yokken/boşken
// uygulanır (sağlanan public_html ve üzerine yazma kipinde hedef dolu → yine bu
// yol gerekir), --no-perms'in yeni dosyalardaki izin süzmesini (umask, özel
// bitler) atlayıp kaynağın izinlerini aynen kiracı ağacına taşır ve farklı
// bağlama noktasında EXDEV geri dönüşü ister; tek kod yolu tüm kipleri kapsar,
// tepe farkı yalnız boru hattındaki birkaç MB'tır.
//
// Hedefte SİLME yapılmaz (üzerine yazma modunda hedef canlı sitedir); var olan
// dosyaların izni korunur (--no-perms). Döner: kaynaktan aktarılan bayt,
// devredilmeden silinen özel dosya sayısı.
func dizinAktar(ctx context.Context, k *Kaynak, ki kiraci.Kiraci, uid, gid int, uzak, hedefRel string, ekstra ...string) (int64, int, error) {
	hedef := filepath.Join(ki.Ev, hedefRel)
	hazirlik, err := hazirlikAc(ki, time.Now())
	if err != nil {
		return 0, 0, fmt.Errorf("hazirlik alani acilamadi")
	}
	defer os.RemoveAll(hazirlik)

	icerik := filepath.Join(hazirlik, hazirlikIcerik)
	if _, err := k.RsyncCek(ctx, uzak+"/", icerik+"/", ekstra...); err != nil {
		return 0, 0, err
	}
	bayt, _ := dizinBoyut(icerik)
	atlanan, err := hazirlikDevret(hazirlik, uid, gid)
	if err != nil {
		return bayt, atlanan, fmt.Errorf("hazirlik alani kiraciya devredilemedi")
	}
	// Kiracı olarak: bağlı/erişilemez bir ara bileşen EACCES verir, kök izlemez.
	if err := kiraciAdim(ctx, yerlesimTaban, ki, "mkdir", "-p", "-m", "0750", "--", hedef); err != nil {
		return bayt, atlanan, fmt.Errorf("hedef dizin olusturulamadi")
	}
	// Hata metnine yalnız çıkış kodu girer (stderr panel yollarını içerir).
	sure := yerlesimSuresi(bayt)
	if err := kiraciAdim(ctx, sure, ki, "rsync", "-a", "--no-perms", "--no-owner", "--no-group",
		"--remove-source-files", icerik+"/", hedef+"/"); err != nil {
		return bayt, atlanan, fmt.Errorf("dosyalar hedefe yerlestirilemedi (rsync: %v)", err)
	}
	_ = kiraciAdim(ctx, sure, ki, "restorecon", "-R", hedef)
	return bayt, atlanan, nil
}

// hazirlikAc — hazırlık kökünü (kök sahipli 0711) sağlar, yarıda kalmış eski
// dizinleri süpürür ve bu aktarıma özel yeni iş dizinini (sarmalayıcı: 0700,
// yalnız kök) açar. Adı <sk>-<unix zaman>-<rastgele>: zaman damgasını kök
// yazar, kiracı (dizin devredildikten sonra bile) adı değiştiremez → süpürme
// ona güvenir.
func hazirlikAc(ki kiraci.Kiraci, simdi time.Time) (string, error) {
	if err := os.MkdirAll(hazirlikKoku, 0o711); err != nil {
		return "", err
	}
	if err := os.Chmod(hazirlikKoku, 0o711); err != nil {
		return "", err
	}
	hazirlikSupur(simdi)
	return os.MkdirTemp(hazirlikKoku, filepath.Base(ki.Ev)+"-"+strconv.FormatInt(simdi.Unix(), 10)+"-")
}

// hazirlikSupur — adındaki zaman damgası hazirlikOmru'ndan eski (ya da adı
// çözülemeyen) hazırlık dizinlerini siler.
func hazirlikSupur(simdi time.Time) {
	girdiler, err := os.ReadDir(hazirlikKoku)
	if err != nil {
		return
	}
	for _, g := range girdiler {
		parca := strings.Split(g.Name(), "-")
		if len(parca) == 3 {
			if sn, err := strconv.ParseInt(parca[1], 10, 64); err == nil && simdi.Sub(time.Unix(sn, 0)) < hazirlikOmru {
				continue
			}
		}
		_ = os.RemoveAll(filepath.Join(hazirlikKoku, g.Name()))
	}
}

// hazirlikDevret — kök rsync'in doldurduğu iş dizinini (sarmalayıcı) kiracıya
// devreder.
//
// Önkoşul, işe başlamadan DOĞRULANIR: sarmalayıcı hâlâ panelin (kök) bağ
// olmayan 0700 dizinidir — değilse içerik yarışa açıktır, devir yapılmaz
// (kapalı hata). Böylece ağaca ancak EN SON adımda (sarmalayıcının kendi
// sahipliği) kiracı girebilir; o ana kadar içerikteki sahip/izinler ne olursa
// olsun başka hesap içeri giremez ve yol tabanlı yürüyüş yarışsızdır.
// Aygıt/fifo/soket SİLİNİR: kiracıya devredilen bir aygıt düğümü ham
// disk/bellek erişimi demektir (kaynak rsync -a ile yaratabilir). Kalan
// girdilerin (dizin/dosya/bağ, içerik dizininin kendisi dahil) sahipliği bağ
// İZLENMEDEN kiracıya geçer; kaynaktan gelen uid'ler böylece silinir.
// Sahibin okuyamadığı modlar (0000 dosya, u+x'siz dizin) devirden önce sahibe
// açılır (hazirlikSahibeAc): aksi halde kiracı rsync'i okuyamayıp 23 ile çıkar,
// kalem düşer. Döner: silinen özel dosya sayısı.
func hazirlikDevret(sarmalayici string, uid, gid int) (int, error) {
	fi, err := os.Lstat(sarmalayici)
	if err != nil {
		return 0, err
	}
	if fi.Mode() != fs.ModeDir|0o700 || fi.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) {
		return 0, fmt.Errorf("hazirlik sarmalayicisi yalniz panele acik degil")
	}
	var yollar []string
	atlanan := 0
	err = filepath.WalkDir(filepath.Join(sarmalayici, hazirlikIcerik), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch t := d.Type(); {
		case t.IsDir():
			// rwx: okuma/geçme + yerleşimin (--remove-source-files) dosya silmesi.
			err = hazirlikSahibeAc(p, d, 0o700)
		case t.IsRegular():
			err = hazirlikSahibeAc(p, d, 0o400)
		case t&fs.ModeSymlink != 0:
		default:
			atlanan++
			return os.Remove(p)
		}
		if err == nil {
			yollar = append(yollar, p)
		}
		return err
	})
	if err != nil {
		return atlanan, err
	}
	for _, p := range yollar {
		if err := lchown(p, uid, gid); err != nil {
			return atlanan, err
		}
	}
	// EN SON: kiracıya kapıyı açan adım (bundan önce içeri giremez).
	return atlanan, lchown(sarmalayici, uid, gid)
}

// hazirlikSahibeAc — hazırlıktaki dizin ya da düzenli dosyaya (bağ DEĞİL: tür
// Lstat'tan) sahibin gerek bitlerini ekler; diğer bitler korunur. Yol tabanlı
// chmod yalnız sarmalayıcı hâlâ kökün 0700 dizini iken (hazirlikDevret önkoşulu)
// güvenlidir: başka hiçbir hesap girdiyi bağa çeviremez.
func hazirlikSahibeAc(p string, d fs.DirEntry, gerek fs.FileMode) error {
	fi, err := d.Info()
	if err != nil {
		return err
	}
	m := fi.Mode()
	if m.Perm()&gerek == gerek {
		return nil
	}
	return os.Chmod(p, m.Perm()|gerek|m&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky))
}
