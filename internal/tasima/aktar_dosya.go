package tasima

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
// Hazırlık alanı: uzak rsync → (kök, 0700) → süz + devret → kiracı rsync
// ---------------------------------------------------------------------------

// hazirlikKoku — uzak rsync'in indiği alan: kiracı ağaçlarının DIŞINDA ama
// /home ile aynı dosya sisteminde. Kök sahipli 0711: kiracı listeleyemez,
// yalnız kendisine devredilmiş iş dizinine adıyla girer. Testler değiştirir.
var hazirlikKoku = "/home/.gosp-tasima"

// hazirlikOmru — adındaki zamandan bu kadar eski hazırlık dizini yarıda kalmış
// (panel yeniden başlamış) bir işindir: canlı bir kalem hesapTimeout'u aşamaz.
const hazirlikOmru = hesapTimeout + time.Hour

// lchown — hazırlık alanının sahiplik devri. Testler çağrı SIRASINI kaydeder.
var lchown = os.Lchown

// dizinAktar — uzak dizini kiracının ev'e göre hedefRel dizinine iki adımda taşır:
//  1. kök, uzak rsync'i YALNIZ kendisinin erişebildiği yeni hazırlık dizinine
//     (0700) çeker — bu sırada kiracı hiçbir şeye dokunamaz, kök kiracı
//     ağacında yol çözmez;
//  2. içerik süzülüp kiracıya devredilir (hazirlikDevret) ve KİRACI kimliğinde
//     rsync ile hedefe yerleştirilir: kiracının kurduğu bağlar/takaslar
//     çekirdeğin DAC'ı ile kiracının kendi yetkisine iner; dosyalar doğrudan
//     kiracıya ait oluşur (chown yok, disk kotası kiracıya işler).
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

	if _, err := k.RsyncCek(ctx, uzak+"/", hazirlik+"/", ekstra...); err != nil {
		return 0, 0, err
	}
	bayt, _ := dizinBoyut(hazirlik)
	atlanan, err := hazirlikDevret(hazirlik, uid, gid)
	if err != nil {
		return bayt, atlanan, fmt.Errorf("hazirlik alani kiraciya devredilemedi")
	}
	// Kiracı olarak: bağlı/erişilemez bir ara bileşen EACCES verir, kök izlemez.
	if err := ki.Komut(ctx, "mkdir", "-p", "-m", "0750", "--", hedef).Run(); err != nil {
		return bayt, atlanan, fmt.Errorf("hedef dizin olusturulamadi")
	}
	// Hata metnine yalnız çıkış kodu girer (stderr panel yollarını içerir).
	if err := ki.Komut(ctx, "rsync", "-a", "--no-perms", "--no-owner", "--no-group",
		hazirlik+"/", hedef+"/").Run(); err != nil {
		return bayt, atlanan, fmt.Errorf("dosyalar hedefe yerlestirilemedi (rsync: %v)", err)
	}
	_ = ki.Komut(ctx, "restorecon", "-R", hedef).Run()
	return bayt, atlanan, nil
}

// hazirlikAc — hazırlık kökünü (kök sahipli 0711) sağlar, yarıda kalmış eski
// dizinleri süpürür ve bu aktarıma özel yeni dizini (0700, yalnız kök) açar.
// Adı <sk>-<unix zaman>-<rastgele>: zaman damgasını kök yazar, kiracı (dizin
// devredildikten sonra bile) adı değiştiremez → süpürme ona güvenir.
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

// hazirlikDevret — kök rsync'in doldurduğu hazırlık dizinini kiracıya devreder.
//
// Kiracı dizine ancak EN SON adımda (hazırlık dizininin kendi sahipliği)
// girebilir; o ana kadar ağaç yalnız köke açıktır (0700), bu yüzden yol tabanlı
// yürüyüş yarışsızdır. Aygıt/fifo/soket SİLİNİR: kiracıya devredilen bir aygıt
// düğümü ham disk/bellek erişimi demektir (kaynak rsync -a ile yaratabilir).
// Kalan girdilerin (dizin/dosya/bağ) sahipliği bağ İZLENMEDEN kiracıya geçer;
// uzak uid'ler (--numeric-ids) böylece silinir. Döner: silinen özel dosya sayısı.
func hazirlikDevret(dizin string, uid, gid int) (int, error) {
	var yollar []string
	atlanan := 0
	err := filepath.WalkDir(dizin, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if t := d.Type(); t.IsDir() || t.IsRegular() || t&fs.ModeSymlink != 0 {
			yollar = append(yollar, p)
			return nil
		}
		atlanan++
		return os.Remove(p)
	})
	if err != nil {
		return atlanan, err
	}
	// yollar[0] hazırlık dizininin kendisidir (WalkDir kökü önce verir).
	for _, p := range yollar[1:] {
		if err := lchown(p, uid, gid); err != nil {
			return atlanan, err
		}
	}
	// EN SON: kiracıya kapıyı açan adım (bundan önce içeri giremez).
	return atlanan, lchown(dizin, uid, gid)
}
