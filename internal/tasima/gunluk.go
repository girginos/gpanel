package tasima

// gunluk.go — bayi işinin iş günlüğü ve kalem notlarının SÜZGECİ.
//
// Taşıma adımları (aktar_*.go, mail.go) günlüğe ayrıntılı satır yazar: yerel/uzak
// dosya yolları, araç çıktıları, sağlama hataları. Kök bunları aynen görür. Bayi
// kendi iş günlüğünü ve kalem notlarını gördüğü için her satır bayiye gitmeden
// süzülür; HAM satır sunucu günlüğüne (journal) yazılır, kök oradan ayıklar.
//
// Kurallar (bayi):
//   - bilinen sırlar maskelenir (temizHata);
//   - mutlak dosya yolları "[yol]" olur (panel/kiracı dizin düzeni sızmaz);
//   - başka kiracıyı ya da sunucu yapılandırmasını ele verebilecek işaret içeren
//     satır (sahibi:, bayi #, nginx, known_hosts, reseller_id …) TÜMÜYLE genel
//     metinle değiştirilir.

import (
	"regexp"
	"strings"
)

// reMutlakYol — boşluk/tırnak/ayraçtan sonra gelen mutlak yol.
var reMutlakYol = regexp.MustCompile(`(^|[\s'"(=:,\[])/[^\s'"),;\]]*`)

// bayiYasakIsaretler — satırı tümüyle gizleten işaretler (küçük harf).
var bayiYasakIsaretler = []string{
	"sahibi:", "bayi #", "nginx", "known_hosts", "reseller_id", "customer_id",
	"/etc/", "sahibi kök", "sahibi kok",
}

// bayiGizliSatir — işaretli satırın yerine yazılan metin.
const bayiGizliSatir = "(ayrıntı sunucu günlüğüne yazıldı)"

// bayiSatirSuz — tek satırı bayiye gösterilebilir hâle getirir.
func bayiSatirSuz(s string, sirlar ...string) string {
	s = temizHata(s, sirlar...)
	kucuk := strings.ToLower(s)
	for _, is := range bayiYasakIsaretler {
		if strings.Contains(kucuk, is) {
			return bayiGizliSatir
		}
	}
	return reMutlakYol.ReplaceAllString(s, "${1}[yol]")
}

// bayiUyarilariSuz — kalem notları (uyarılar) için bayiSatirSuz.
func bayiUyarilariSuz(uyarilar []string, sirlar ...string) []string {
	out := make([]string, 0, len(uyarilar))
	for _, u := range uyarilar {
		out = append(out, bayiSatirSuz(u, sirlar...))
	}
	return out
}
