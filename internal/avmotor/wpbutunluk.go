package avmotor

// WORDPRESS ÇEKİRDEK BÜTÜNLÜĞÜ — barındırmadaki EN GÜÇLÜ tek tespit yöntemi.
//
// 🔴 NEDEN İMZALARDAN DAHA İYİ: imza, saldırganın kodunu TANIMAYI gerektirir.
// Bütünlük denetimi tam tersini yapar — MEŞRU dosyanın ne olması gerektiğini
// bilir. `wp-includes/load.php` dosyası resmî sağlamayla uyuşmuyorsa, içine ne
// yazıldığı önemsizdir: kimse WordPress çekirdeğini meşru sebeple değiştirmez.
// Bu yüzden:
//   - hiç görülmemiş, tamamen yeni bir arka kapıyı da yakalar
//   - yanlış pozitif oranı ~sıfırdır
//   - saldırgan obfuscation ile kaçamaz (dosya farklıysa farklıdır)
//
// 🔴 KAPSAM SINIRI DÜRÜSTÇE: yalnız ÇEKİRDEK dosyalarını kapsar. wp-content
// altındaki tema/eklenti dosyalarının resmî sağlaması yoktur (sürüm başına
// binlerce paket). Oraya kural motoru + konum sezgileri bakar.

import (
	"crypto/md5"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// SaglamaKaynagi — bir WordPress kurulumu için "yol → md5" tablosu sağlar.
//
// Arayüz olmasının sebebi: sağlamalar WP.org API'sinden gelir ama ağ her zaman
// yoktur. Uygulama katmanı bunu önbellekler; motor kaynağın nereden geldiğini
// bilmez ve bilmemeli.
//
// 🔴 Sağlama YOKSA nil dönmeli, BOŞ HARİTA DEĞİL. Boş harita "hiçbir dosya
// eşleşmiyor" anlamına gelir ve tüm çekirdeği zararlı ilan ederdi — ölçemediğin
// şeyi "kirli" saymak da "temiz" saymak kadar yanlıştır.
type SaglamaKaynagi interface {
	// Saglamalar: wpKok için yol→md5 tablosu. Bilinmiyorsa (nil, false).
	Saglamalar(wpKok string) (map[string]string, bool)
}

// wpButunlukKontrol — dosya WordPress çekirdeğine aitse sağlamasını doğrular.
//
// Dönüş: (kuralID, puan, tespitEdildi)
func wpButunlukKontrol(yol, wpKok string, kaynak SaglamaKaynagi) (string, int, bool) {
	tablo, ok := kaynak.Saglamalar(wpKok)
	if !ok || len(tablo) == 0 {
		// 🔴 Ölçüm YAPILAMADI. Bu bir tespit DEĞİLDİR — sessizce geçiyoruz
		// ama "temiz" de demiyoruz; karar diğer katmanlara kalır.
		return "", 0, false
	}

	bagil, err := filepath.Rel(wpKok, yol)
	if err != nil || strings.HasPrefix(bagil, "..") {
		return "", 0, false // kurulum dışında
	}
	bagil = filepath.ToSlash(bagil)

	// Yalnız çekirdek ağaçları. wp-content kapsam dışı (yukarıdaki not).
	if !strings.HasPrefix(bagil, "wp-includes/") && !strings.HasPrefix(bagil, "wp-admin/") {
		// Kökteki wp-*.php dosyaları da çekirdektir (wp-login.php, wp-config
		// HARİÇ — o kuruluma özeldir ve sağlamada yer almaz).
		taban := filepath.Base(bagil)
		if !strings.Contains(bagil, "/") && strings.HasPrefix(taban, "wp-") &&
			strings.HasSuffix(taban, ".php") && taban != "wp-config.php" {
			// devam
		} else {
			return "", 0, false
		}
	}

	beklenen, kayitli := tablo[bagil]
	if !kayitli {
		// 🔴 Çekirdek ağacında OLMAMASI GEREKEN bir dosya. Saldırganlar arka
		// kapıyı sıklıkla wp-includes altına, meşru görünen bir adla atar
		// (örn. wp-includes/js/jquery/jquery.min.php). Resmî listede yoksa
		// oraya ait değildir.
		return "GOSP-WP-YABANCI-DOSYA", 100, true
	}

	f, err := os.Open(yol)
	if err != nil {
		return "", 0, false
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", 0, false
	}
	if hex.EncodeToString(h.Sum(nil)) == beklenen {
		return "", 0, false // temiz
	}
	return "GOSP-WP-CEKIRDEK-DEGISMIS", 100, true
}
