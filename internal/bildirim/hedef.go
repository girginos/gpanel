package bildirim

// hedef.go — bildirim YAZICI çekirdeği (D3b): hedefler, zaman pencereli bastırma,
// aktörün kendi eylemi ve kiracıya giden mesajın temizliği.
//
// Hedefler (görünürlük gorunurluk() ile AYNI sözlükte kalır, yeni kural yok):
//
//	YazOlay(domainID>0) → domain sahibi (bayi ya da yönetici) + o domainin müşterisi
//	YazOlay(domainID=0) → panel geneli (yalnız yönetici)
//	YazBayi(rid)        → domain_id NULL, reseller_id=rid: YALNIZ o bayi görür
//	                      (yönetici NULL/0 kapsamını, müşteri domain_id'yi görür → sızmaz)
//
// Tekilleştirme iki katmandır:
//  1. Görüntüleyene göre (eski Yaz): yeni satırı görecek herkes aynı olayı hâlâ
//     okunmamış tutuyorsa yazılmaz.
//  2. Zaman penceresi (bastir>0): aynı olay (kategori+başlık+ref+hedef) son
//     `bastir` içinde yazıldıysa — okunmuş olsa bile — yazılmaz. Kısa sürede tekrar
//     eden olay (ör. art arda düşen yedek, tekrar tekrar taranan bulgu) tek bildirim olur.
//
// Kalıcı durumlar (kota, SSL) için tekilleştirme YETMEZ: internal/pano kenar-tetikli
// durum tablosu (cp_esik_durum) kullanır, bildirim yalnız eşiğin yukarı geçişinde yazılır.

import (
	"database/sql"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Olay — yazılacak bildirimin içeriği.
type Olay struct {
	Seviye   string // bilgi | uyari | kritik
	Kategori string // tercih.go Kategoriler sözlüğünden
	Baslik   string
	Mesaj    string
	RefTur   string
	RefID    int64
}

// Aktör rolleri (YazOlay aktor parametresi): olayı panelden kendisi tetikleyen
// görüntüleyenin kopyası baştan OKUNMUŞ yazılır (kendi eylemi için rozet yok);
// öteki görüntüleyenler (ör. müşteri) okunmamış alır.
const (
	AktorYok      = ""
	AktorYonetici = "admin"
	AktorBayi     = "bayi"
)

// mesajSiniri — bildirim mesajının en fazla rune sayısı (TEXT sütun; zil/liste için yeter).
const mesajSiniri = 1000

// evOneki — /home/<sistem kullanıcısı>/ öneki: kiracının KENDİ dosya yolu göreli
// kalır (AV bulgusunda hangi dosya olduğu bilgisi kaybolmasın; ön yüzün eski
// yolGizle davranışıyla aynı), sistem kullanıcı adı ve mutlak yol düşer.
var evOneki = regexp.MustCompile(`/home/[^/\s]+/`)

// sunucuYolu — kiracıya görünen mesajda maskelenecek mutlak sunucu yolları.
// /var, /opt, /etc, /root, /usr, /tmp, /srv, /run, /backup… — tar/mysqldump
// stderr'i bunları taşır (eleştiri 14).
// Yol yalnız metin başında ya da ayırıcıdan sonra başlarsa eşleşir: göreli bir
// yolun içindeki "uploads/tmp/x" maskelenmez (Go regexp'te geriye bakış yok →
// ayırıcı yakalanıp geri yazılır).
var sunucuYolu = regexp.MustCompile(`(^|[\s'"=:(\[])/(?:home|var|opt|etc|root|usr|tmp|srv|run|backup|backups|mnt)(?:/[^\s'",;:()\[\]]*)?`)

// Guvenli — kiracıya (bayi/müşteri) gidecek mesajı temizler: /home/<kul>/ öneki
// düşer (göreli yol kalır), öteki mutlak sunucu yolları "[sunucu yolu]" olur, CR/LF tek boşluğa iner, uzunluk sınırlanır.
// Ham hata ayrıntısı günlükte kalmalıdır; bildirim kullanıcı metnidir.
func Guvenli(m string) string {
	m = evOneki.ReplaceAllString(m, "")
	m = sunucuYolu.ReplaceAllString(m, "${1}[sunucu yolu]")
	m = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(m)
	if utf8.RuneCountInString(m) > mesajSiniri {
		r := []rune(m)
		m = string(r[:mesajSiniri]) + "…"
	}
	return m
}

// YazOlay — domain hedefli (domainID>0) ya da panel geneli (0) bildirim.
// aktor: AktorYonetici / AktorBayi → sahibin kopyası, sahip o rolse okunmuş yazılır.
// bastir>0 → aynı olay bu süre içinde yazıldıysa yeniden yazılmaz.
// Dönen değer yeni satırın kimliği (yazılmadıysa 0).
func YazOlay(db *sql.DB, o Olay, domainID int64, aktor string, bastir time.Duration) int64 {
	if db == nil {
		return 0
	}
	var dom, rid any
	gorenler := []okuyan{sahipOkuyan(0)}
	if domainID > 0 {
		dom = domainID
		var r int64
		if db.QueryRow(`SELECT reseller_id FROM domains WHERE id=?`, domainID).Scan(&r) == nil && r > 0 {
			rid = r // reseller-sahipli; 0 ise admin-sahipli → NULL (admin görür)
		}
		gorenler = []okuyan{sahipOkuyan(r), {okuyanMusteri, domainID}}
		// Kiracıya görünür → mesaj temizliği zorunlu (tek nokta, eleştiri 14).
		o.Mesaj = Guvenli(o.Mesaj)
	}
	var okunmus []okuyan
	if aktor != AktorYok && gorenler[0].tur == aktor {
		okunmus = append(okunmus, gorenler[0])
	}
	return yazGenel(db, o, dom, rid, gorenler, okunmus, bastir)
}

// YazBayi — bayinin kendisine (alan adsız) bildirim: havuz eşiği, taşıma bitti...
// Görenler YALNIZ [bayi rid]; yönetici ve müşteriler görmez.
func YazBayi(db *sql.DB, rid int64, o Olay, bastir time.Duration) int64 {
	if db == nil || rid <= 0 {
		return 0
	}
	o.Mesaj = Guvenli(o.Mesaj)
	return yazGenel(db, o, nil, rid, []okuyan{{okuyanBayi, rid}}, nil, bastir)
}

// yazGenel — ortak yazıcı (eski Yaz gövdesi + bastırma + aktör okunmuşu).
func yazGenel(db *sql.DB, o Olay, dom, rid any, gorenler, okunmus []okuyan, bastir time.Duration) int64 {
	if bastir > 0 {
		var n int
		if db.QueryRow(`SELECT COUNT(*) FROM cp_bildirim
		  WHERE kategori=? AND baslik=? AND ref_tur=? AND ref_id=? AND domain_id <=> ? AND reseller_id <=> ?
		    AND created_at > NOW() - INTERVAL ? SECOND`,
			o.Kategori, o.Baslik, o.RefTur, o.RefID, dom, rid, int64(bastir/time.Second)).Scan(&n) == nil && n > 0 {
			return 0
		}
	}
	// 🔴 DEDUP (görüntüleyene göre): aynı (kategori, başlık, ref, domain) bildirimi
	// yeni satırı görecek HERKES hâlâ okunmamış tutuyorsa YENİDEN YAZMA. Kalıcı
	// bir durum (swap yok, SSL hatası, ...) her panel açılışında / monitoring
	// turunda yeni kayıt üretip bildirim merkezini spam'liyordu (kullanıcı 11
	// özdeş "Swap alanı yok" gördü). Farklı olaylar (farklı başlık/ref/domain)
	// BİRLEŞTİRİLMEZ — yalnız birebir tekrar.
	//
	// Okuyan ve durum sürüyorsa YALNIZ o yeniden uyarılır: yeni satır yazılır ve
	// elinde hâlâ okunmamış kopyası olan görüntüleyen için baştan okundu işaretlenir
	// (rozetine ikinci kopya binmez). "Hâlâ okunmamış" GÜNCEL sahibin gördüğü
	// satırlara bakar: devirden kalmış, sahibin göremediği bir satır yöneticiyi/bayiyi
	// artık susturmaz (B4).
	var bekleyen []okuyan
	for _, g := range gorenler {
		var n int
		if err := ayniOkunmamis(db, g, o.Kategori, o.Baslik, o.RefTur, o.RefID, dom).Scan(&n); err != nil {
			// cp_bildirim_okundu okunamıyor (0105 göçü uygulanmamış?) → eski ortak
			// sütunla tekilleştir: bildirim merkezi her turda spam'lenmesin.
			if db.QueryRow(`SELECT COUNT(*) FROM cp_bildirim
			  WHERE okundu=0 AND kategori=? AND baslik=? AND ref_tur=? AND ref_id=? AND domain_id <=> ?`,
				o.Kategori, o.Baslik, o.RefTur, o.RefID, dom).Scan(&n) == nil && n > 0 {
				return 0
			}
			bekleyen = nil
			break
		}
		if n > 0 {
			bekleyen = append(bekleyen, g)
		}
	}
	if len(bekleyen) == len(gorenler) {
		return 0
	}
	res, err := db.Exec(
		`INSERT INTO cp_bildirim (seviye, kategori, baslik, mesaj, domain_id, reseller_id, ref_tur, ref_id)
		 VALUES (?,?,?,?,?,?,?,?)`, o.Seviye, o.Kategori, o.Baslik, o.Mesaj, dom, rid, o.RefTur, o.RefID)
	if err != nil {
		return 0
	}
	yeni, err := res.LastInsertId()
	if err != nil || yeni <= 0 {
		return 0
	}
	for _, g := range append(bekleyen, okunmus...) {
		_, _ = db.Exec(`INSERT IGNORE INTO cp_bildirim_okundu (bildirim_id, okuyan_tur, okuyan_id) VALUES (?,?,?)`,
			yeni, g.tur, g.id)
	}
	return yeni
}
