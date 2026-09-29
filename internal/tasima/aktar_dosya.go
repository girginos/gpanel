package tasima

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// dosyalariAktar — HesapAktar'in 2. adimi: belge kokunu (ve docroot-disi
// uygulama dizinlerini) kaynaktan hedefe rsync ile ceker, sahiplik/SELinux
// baglamini duzeltir, Laravel sonrasi duzeltmeleri uygular. Hata donerse
// HesapAktar kalemi basarisiz sayar (yeni olusturulan hesap geri alinir).
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
		if err := os.MkdirAll(webRoot, 0o750); err != nil {
			return fmt.Errorf("hedef dizin: %w", err)
		}
		log("dosyalar aktariliyor: %s → %s", uzak, webRoot)
		if _, err := k.RsyncCek(ctx, uzak+"/", webRoot+"/",
			"--exclude=.git/", "--exclude=*.sock", "--exclude=.cpanel/"); err != nil {
			return fmt.Errorf("dosya aktarimi: %w", err)
		}
		if b, err := dizinBoyut(webRoot); err == nil {
			sonuc.DosyaBayt = b
		}
		_ = exec.CommandContext(ctx, "chown", "-R", sk+":"+sk, webRoot).Run()
		_ = exec.CommandContext(ctx, "restorecon", "-RF", webRoot).Run()
		log("dosyalar tamam (%.1f MB)", float64(sonuc.DosyaBayt)/(1024*1024))
		// 🔴 DOCROOT-DISI BAGIMLILIK (Laravel/Symfony): uygulama kodu belge kokunun
		// DISINDA durur (public_html/index.php -> ../laravel_11/vendor/autoload.php).
		// Yalniz docroot tasinirsa site 500 verir. index.php'deki "../<dizin>/"
		// referanslarini tara, kaynaktan o dizinleri de getir.
		for _, ust := range docrootDisiBagimlilik(webRoot) {
			uzakUst := filepath.Join(filepath.Dir(uzak), ust)
			hedefUst := filepath.Join("/home", sk, ust)
			if !gecerliUzakYol(uzakUst) {
				continue
			}
			log("uygulama dizini (belge koku disi) aktariliyor: %s → %s", uzakUst, hedefUst)
			if err := os.MkdirAll(hedefUst, 0o750); err != nil {
				continue
			}
			if _, err := k.RsyncCek(ctx, uzakUst+"/", hedefUst+"/", "--exclude=.git/", "--exclude=*.sock"); err != nil {
				log("uyari: %s aktarilamadi: %v", ust, kisalt(temizHata(err.Error(), k.Parola), 120))
				sonuc.Uyarilar = append(sonuc.Uyarilar, "Uygulama dizini eksik: "+ust)
				continue
			}
			_ = exec.CommandContext(ctx, "chown", "-R", sk+":"+sk, hedefUst).Run()
			_ = exec.CommandContext(ctx, "restorecon", "-RF", hedefUst).Run()
			// Laravel ise: APP_URL'i hedef domaine cek, cache temizle, .env'i 0600 yap.
			laravelSonrasiDuzelt(hedefUst, alanAdi, sk, log)
		}
		// Docroot'un KENDISI Laravel ise (public_html/artisan) ayni duzeltmeler.
		laravelSonrasiDuzelt(webRoot, alanAdi, sk, log)
	}
	return nil
}
