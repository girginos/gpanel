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
	bagil, err := filepath.Rel(wpKok, yol)
	if err != nil || strings.HasPrefix(bagil, "..") {
		return "", 0, false
	}
	bagil = filepath.ToSlash(bagil)

	tablo, ok := kaynak.Saglamalar(wpKok)
	if !ok || len(tablo) == 0 {
		// A.1: kurulum VAR ama sağlama YOK -> körleme şüphesi. version.php
		// üzerinden OLAY (kurulum başına bir dosya). Puan 60 = şüpheli
		// (ağ kesintisinde de tetiklenir, kritik değil).
		if bagil == "wp-includes/version.php" {
			// 100 = KRITIK (gorunur). Korleme (var-olmayan surum/bozuk
			// version.php) buradan gecer. Ag kesintisinde de tetiklenir ama
			// nadir, C.4 ile tekrar denenir ve version.php ASLA oto-karantinaya
			// girmez (ajan cekirdek istisnasi) -> zararsiz.
			return "GOSP-WP-SAGLAMA-KOR", 100, true
		}
		return "", 0, false
	}

	beklenen, kayitli := tablo[bagil]
	if kayitli {
		// Kapsam TABLODAN: tabloda anahtarı olan HER yol (670 wp-content
		// çekirdek dosyası dahil), yalnız wp-includes/wp-admin değil.
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
			return "", 0, false
		}
		// Sağlama TUTMADI. AMA resmî checksum tablosu wp-content altındaki
		// VARSAYILAN tema/eklenti dosyalarını da içerir (twentytwentytwo blok
		// desenleri, akismet, hello.php). WordPress bunları ÇEKİRDEKTEN BAĞIMSIZ
		// otomatik günceller → dosya, kurulu çekirdek sürümünün sağlamasıyla
		// tutmaz ve bu MEŞRUDUR, tahrifat değil. wp-content'te sağlama-tutmazlığını
		// "çekirdek değişti" saymak = yüzlerce YANLIŞ POZİTİF + oto-karantina ile
		// CANLI SİTE KIRILMASI (twentytwentytwo/inc/patterns → 149 dosya olayı).
		// Sağlama-tutmazlığı yalnız GERÇEK çekirdek ağacında (wp-includes/wp-admin/
		// kök wp-*.php) yüksek-sinyaldir; wp-content İÇERİĞE bakan kural motoruna
		// düşer (gerçek enjeksiyon eval/base64/shell imzasıyla yine yakalanır).
		if wpCekirdekAgacinda(bagil) {
			return "GOSP-WP-CEKIRDEK-DEGISMIS", 100, true
		}
		return "", 0, false
	}

	// Tabloda yok: yalnız ÇEKİRDEK AĞACINDA olmaması gereken dosya "yabancı".
	// wp-content'te tabloda olmayan dosya meşru eklenti/tema -> kural motoruna.
	if wpCekirdekAgacinda(bagil) {
		return "GOSP-WP-YABANCI-DOSYA", 100, true
	}
	return "", 0, false
}

// wpCekirdekAgacinda — yol GERÇEK WordPress çekirdek ağacında mı: wp-includes/,
// wp-admin/ veya kökteki wp-*.php (wp-config.php hariç). wp-content (tema/eklenti/
// yükleme) çekirdek DEĞİLDİR — resmî sağlaması olsa bile bağımsız güncellenir, bu
// yüzden oradaki sağlama-tutmazlığı tahrifat kanıtı sayılmaz.
func wpCekirdekAgacinda(bagil string) bool {
	if strings.HasPrefix(bagil, "wp-includes/") || strings.HasPrefix(bagil, "wp-admin/") {
		return true
	}
	taban := filepath.Base(bagil)
	if !strings.Contains(bagil, "/") && strings.HasPrefix(taban, "wp-") &&
		strings.HasSuffix(taban, ".php") && taban != "wp-config.php" {
		return true
	}
	return false
}
