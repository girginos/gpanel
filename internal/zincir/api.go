package zincir

// FAZ 3b — API Security (Initial Access). audit_log'da panel BRUTE-FORCE BAŞARISI
// tespit eder → av_olay 'giris' (reseller-seviye, domain_id NULL).
//
// 🔴 TANIM = "başarısız SEL + ardından BAŞARILI giriş" (yalnız başarısız sel DEĞİL).
// Neden: hesap-seviye giris'in bir zincire tek katkısı "+1 ayrık aşama"dır (dosya
// yolu/pid'i olmadığından ASLA nedensel bağ kuramaz). Salt "başarısız sel" kullansak,
// stale-parola tekrar eden bir bot/parola-yöneticisi SÜREKLİ başarısız olur →
// o hesabın HER domainine KALICI giris öneki takılırdı → dedektör "3 rastlantı gerek"
// yerine "2 rastlantı gerek"e sessizce düşerdi (kök için tüm sunucu). BAŞARI şartı:
//   • sürekli-başarısız bot ASLA başaramaz → kalıcı-önek riski YOK,
//   • bilinmeyen-kullanıcı spam'i (reseller_id=0'a damgalanıp kökü şişirir) başaramaz → YOK,
//   • panel-vektörlü GERÇEK ele geçirme (shell yüklemek için giriş ŞART) → başarır → TP korunur.
//
// 🔴 TEK BAŞINA ZİNCİR OLUŞTURMAZ (seviye uyari): domainde AYNI pencerede dosya+
// çalıştırma da varsa zincire "Initial Access" ucu ekler.

import (
	"database/sql"
	"fmt"
	"log"
	"net"
)

const girisEsigi = 5 // pencerede reseller başına başarısız panel girişi eşiği

type girisBurst struct {
	rid int64
	n   int // başarısız deneme sayısı
	ip  string
	ipn int // ayrık IP sayısı (dağıtık/tek-kaynak sinyali)
}

func apiTara(db *sql.DB) {
	// Başarısız SEL (≥eşik) VE ardından (>= ilk başarısızlık zamanı) BAŞARILI giriş.
	rows, err := db.Query(`SELECT f.reseller_id, f.n, f.ip, f.ipn
		FROM (
			SELECT reseller_id, COUNT(*) n,
			       SUBSTRING_INDEX(GROUP_CONCAT(ip ORDER BY ts DESC SEPARATOR ','), ',', 1) ip,
			       COUNT(DISTINCT ip) ipn,
			       MIN(ts) ilk
			FROM audit_log
			WHERE action='auth.login' AND ok=0 AND ts >= (NOW() - INTERVAL ? MINUTE)
			GROUP BY reseller_id HAVING COUNT(*) >= ?
		) f
		JOIN (
			SELECT reseller_id, MAX(ts) son_ok
			FROM audit_log
			WHERE action='auth.login' AND ok=1 AND ts >= (NOW() - INTERVAL ? MINUTE)
			GROUP BY reseller_id
		) s ON s.reseller_id = f.reseller_id AND s.son_ok >= f.ilk`,
		pencereDk, girisEsigi, pencereDk)
	if err != nil {
		// 🔴 Sessizce yutma: şema kayması bu sorguyu bozarsa dedektör GERÇEK ataklarda
		// sıfır giris üretir ve "temiz" gibi görünür. Gürültülü logla (guard-measures).
		log.Printf("apiTara sorgu HATASI (giris dedektörü çalışmıyor olabilir): %v", err)
		return
	}
	var bursts []girisBurst
	for rows.Next() {
		var b girisBurst
		var ip sql.NullString
		if scanErr := rows.Scan(&b.rid, &b.n, &ip, &b.ipn); scanErr != nil {
			log.Printf("apiTara satır scan HATASI: %v", scanErr)
			continue
		}
		b.ip = ip.String
		bursts = append(bursts, b)
	}
	rows.Close()

	for _, b := range bursts {
		if girisVar(db, b.rid) {
			continue // dedup: bu reseller için son girisYenidenDk içinde giris var
		}
		var rid any
		if b.rid > 0 {
			rid = b.rid // 0=root → NULL (admin reseller_id IS NULL kapsamıyla tutarlı)
		}
		if _, exErr := db.Exec(`INSERT INTO av_olay (domain_id, reseller_id, kaynak, asama, seviye, ozet)
			VALUES (NULL, ?, 'api', 'giris', 'uyari', ?)`, rid, girisOzet(b)); exErr != nil {
			log.Printf("apiTara INSERT HATASI [reseller=%d]: %v", b.rid, exErr)
			continue
		}
		log.Printf("API-GİRİŞ [reseller=%d] %d başarısız + BAŞARILI giriş (%d ayrık IP, en son %s)",
			b.rid, b.n, b.ipn, ipGuvenli(b.ip))
	}
}

// girisOzet — operatöre gösterilecek özet. IP DOĞRULANIR (client X-Forwarded-For
// enjekte edebilir); geçersizse "bilinmiyor". Dağıtıksa ayrık IP sayısı eklenir.
func girisOzet(b girisBurst) string {
	ipk := ""
	if b.ipn > 1 {
		ipk = fmt.Sprintf(" +%d farklı IP", b.ipn-1)
	}
	return fmt.Sprintf("%d başarısız denemeden sonra BAŞARILI panel girişi (IP %s%s)",
		b.n, ipGuvenli(b.ip), ipk)
}

func ipGuvenli(s string) string {
	if net.ParseIP(s) == nil {
		return "bilinmiyor"
	}
	return s
}

// girisVar — bu reseller için son girisYenidenDk içinde giris av_olay üretildi mi (dedup).
// 🔴 Scan hatasında FAIL-SAFE: true döndür (yeniden-yayını BASTIR) — hatada 0 sayıp
// false dönmek her tikte INSERT selini açardı. Hata gürültülü loglanır.
func girisVar(db *sql.DB, rid int64) bool {
	var ridArg any
	if rid > 0 {
		ridArg = rid
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM av_olay
		WHERE asama='giris' AND reseller_id <=> ? AND created_at >= (NOW() - INTERVAL ? MINUTE)`,
		ridArg, girisYenidenDk).Scan(&n); err != nil {
		log.Printf("girisVar dedup sorgu HATASI [reseller=%d] (fail-safe: bastırıldı): %v", rid, err)
		return true // fail-safe: sel açma
	}
	return n > 0
}
