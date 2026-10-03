package domainip

// Package domainip — TEK domainin IPv4'ünü değiştiren TEK KAYNAK çekirdek.
//
// (ipyonetim içinde olamaz: ipyonetim ← portyonetim ← provisioner ← nginxset ← dns
// döngüsü.) Kullananlar (HEPSİ yalnız yönetici): PUT /domains/{id}/ip,
// PUT /admin/domains/{id}/ipv4 (eski yönetici ucu) ve toplu işlem "ip"
// (internal/toplu). Sunucu çapında IP değişimi ayrı araçtır (gpanel ip change).
//
// NE DEĞİŞİR (recon: toplu-ip/kesif):
//   - domains.ipv4; ftp_host / db_host ESKİ IP'ye eşitse onlar da (CLI ile aynı kural).
//   - Domainin dns_records'ındaki A kayıtları: değeri TAM OLARAK eski IP olanlar
//     (@, www, mail, ns1, alt alan adları… — elle başka IP'ye yöneltilmiş kayda
//     dokunulmaz).
//   - SPF (TXT "v=spf1 …"): "ip4:<eski>" varsa "ip4:<yeni>" EKLENİR, eski SİLİNMEZ —
//     giden posta kaynağı (sunucu varsayılanı / mail eklentisi dedicated IP) bu
//     işlemle değişmez; eskiyi silmek teslimatı bozabilirdi.
//   - Zone dosyası dns.WriteZone ile yeniden üretilir: serial monoton artar,
//     named-checkzone geçici dosyada doğrular, include checkconf'lu, named reload.
// NE DEĞİŞMEZ: nginx vhost'ları IP-agnostik (listen 80/443, IP yok) → render/reload
// gerekmez. AAAA (panel v4-only). Mail eklentisinin giden IP ataması.
//
// GÜVENLİK: hedef IP YALNIZ sunucunun gerçekten sahip olduğu global IPv4'lerden
// olabilir (ip -o -4 addr; istemcinin listesine güvenilmez). Zone yazımı başarısız
// olursa DB değişiklikleri geri alınır ve zone eski içerikle yeniden üretilir.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"

	"girginospanel/internal/dns"
	"girginospanel/internal/ipyonetim"
)

var (
	ErrGecersizIP  = errors.New("geçersiz IPv4 adresi")
	ErrSunucudaYok = errors.New("bu IP sunucuda tanımlı değil")
	ErrDomainYok   = errors.New("domain bulunamadı")
)

// Test kancaları: üretimde gerçek arayüz listesi ve gerçek zone yazıcı.
var (
	sunucuIPOku = ipyonetim.SunucuIPler
	zoneYazFn   = dns.WriteZone
)

// domainIPKilit: aynı süreçte iki IP değişimi zone/DB üzerinde yarışmasın.
var domainIPKilit sync.Mutex

// DegisimSonuc — tek domain IP değişiminin özeti (arayüz + toplu iş detayı).
type DegisimSonuc struct {
	DomainID int64    `json:"domain_id"`
	AlanAdi  string   `json:"alan_adi"`
	EskiIP   string   `json:"eski_ip"`
	YeniIP   string   `json:"yeni_ip"`
	Degisti  bool     `json:"degisti"`
	DNSKayit int      `json:"dns_kayit"` // güncellenen A kaydı
	SPFKayit int      `json:"spf_kayit"` // yeni IP eklenen SPF kaydı
	FTPHost  bool     `json:"ftp_host"`
	DBHost   bool     `json:"db_host"`
	Uyarilar []string `json:"uyarilar,omitempty"`
}

// SunucudaMi — ip sunucunun global IPv4'lerinden biri mi?
func SunucudaMi(ip string) bool {
	for _, s := range sunucuIPOku() {
		if s.IP == ip {
			return true
		}
	}
	return false
}

// NormIP — IPv4 normalizasyonu (v4 değilse ""); ipyonetim ile aynı kural.
func NormIP(s string) string {
	ip := net.ParseIP(strings.TrimSpace(s))
	if ip == nil || ip.To4() == nil {
		return ""
	}
	return ip.To4().String()
}

type dnsEski struct {
	id    int64
	deger string
}

// spfIP4Ekle — SPF değerinde "ip4:<eski>" (ya da /32) varsa ve "ip4:<yeni>" yoksa
// yeniyi eskinin hemen arkasına ekler. Değişmediyse ("", false).
func spfIP4Ekle(deger, eski, yeni string) (string, bool) {
	govde := strings.Trim(strings.TrimSpace(deger), `"`)
	if !strings.HasPrefix(strings.ToLower(govde), "v=spf1") || eski == "" {
		return "", false
	}
	parcalar := strings.Fields(govde)
	eskiVar, yeniVar, konum := false, false, -1
	for i, p := range parcalar {
		switch strings.ToLower(p) {
		case "ip4:" + eski, "ip4:" + eski + "/32", "+ip4:" + eski, "+ip4:" + eski + "/32":
			eskiVar, konum = true, i
		case "ip4:" + yeni, "ip4:" + yeni + "/32", "+ip4:" + yeni, "+ip4:" + yeni + "/32":
			yeniVar = true
		}
	}
	if !eskiVar || yeniVar {
		return "", false
	}
	out := make([]string, 0, len(parcalar)+1)
	out = append(out, parcalar[:konum+1]...)
	out = append(out, "ip4:"+yeni)
	out = append(out, parcalar[konum+1:]...)
	yeniDeger := strings.Join(out, " ")
	if strings.HasPrefix(strings.TrimSpace(deger), `"`) {
		yeniDeger = `"` + yeniDeger + `"`
	}
	return yeniDeger, true
}

// DomainIPDegistir — domainin IPv4'ünü yeniIP yapar (idempotent: zaten o IP ise
// Degisti=false döner, hiçbir şeye dokunmaz). Hedef IP sunucuda değilse
// ErrSunucudaYok. Yetki/kapsam kararı ÇAĞIRANDADIR.
func DomainIPDegistir(ctx context.Context, db *sql.DB, domainID int64, yeniIP string) (DegisimSonuc, error) {
	s := DegisimSonuc{DomainID: domainID}
	yeni := NormIP(yeniIP)
	if yeni == "" {
		return s, ErrGecersizIP
	}
	s.YeniIP = yeni
	if !SunucudaMi(yeni) {
		return s, ErrSunucudaYok
	}

	domainIPKilit.Lock()
	defer domainIPKilit.Unlock()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return s, err
	}
	defer func() { _ = tx.Rollback() }()

	var eski, ftpHost, dbHost string
	if err := tx.QueryRowContext(ctx,
		`SELECT alan_adi, COALESCE(ipv4,''), COALESCE(ftp_host,''), COALESCE(db_host,'') FROM domains WHERE id=? FOR UPDATE`,
		domainID).Scan(&s.AlanAdi, &eski, &ftpHost, &dbHost); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return s, ErrDomainYok
		}
		return s, err
	}
	s.EskiIP = eski
	if eski == yeni {
		return s, nil // zaten bu IP'de
	}

	// DNS kayıtları: yalnız bu domainin satırları.
	var eskiler []dnsEski
	type guncelle struct {
		id    int64
		deger string
	}
	var yeniler []guncelle
	if eski != "" {
		rows, err := tx.QueryContext(ctx,
			`SELECT id, tip, deger FROM dns_records WHERE domain_id=? AND ((tip='A' AND deger=?) OR tip='TXT')`,
			domainID, eski)
		if err != nil {
			return s, err
		}
		for rows.Next() {
			var id int64
			var tip, deger string
			if err := rows.Scan(&id, &tip, &deger); err != nil {
				rows.Close()
				return s, err
			}
			switch tip {
			case "A":
				eskiler = append(eskiler, dnsEski{id, deger})
				yeniler = append(yeniler, guncelle{id, yeni})
				s.DNSKayit++
			case "TXT":
				if d, ok := spfIP4Ekle(deger, eski, yeni); ok {
					eskiler = append(eskiler, dnsEski{id, deger})
					yeniler = append(yeniler, guncelle{id, d})
					s.SPFKayit++
				}
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return s, err
		}
	} else {
		s.Uyarilar = append(s.Uyarilar, "domainin kayıtlı eski IP'si yoktu; DNS A kayıtları değiştirilmedi")
	}
	for _, g := range yeniler {
		if _, err := tx.ExecContext(ctx, `UPDATE dns_records SET deger=? WHERE id=? AND domain_id=?`, g.deger, g.id, domainID); err != nil {
			return s, err
		}
	}
	s.FTPHost = eski != "" && ftpHost == eski
	s.DBHost = eski != "" && dbHost == eski
	if _, err := tx.ExecContext(ctx,
		`UPDATE domains SET ipv4=?, ftp_host=IF(?, ?, ftp_host), db_host=IF(?, ?, db_host) WHERE id=?`,
		yeni, s.FTPHost, yeni, s.DBHost, yeni, domainID); err != nil {
		return s, err
	}
	if err := tx.Commit(); err != nil {
		return s, err
	}
	s.Degisti = true

	if len(yeniler) == 0 {
		return s, nil
	}
	// Zone'u yeni kayıtlarla üret. Başarısızsa (named-checkzone/checkconf) DB'yi
	// eski haline döndür ve zone'u eski kayıtlarla yeniden üret → DB ile yayındaki
	// DNS hiçbir an ayrışık kalmaz.
	if zerr := zoneYazFn(ctx, db, domainID); zerr != nil {
		if gerr := geriAl(context.WithoutCancel(ctx), db, domainID, eski, ftpHost, dbHost, eskiler); gerr != nil {
			return s, fmt.Errorf("DNS zone yazılamadı (%v) ve GERİ ALMA BAŞARISIZ: %w", zerr, gerr)
		}
		_ = zoneYazFn(context.WithoutCancel(ctx), db, domainID)
		s.Degisti = false
		return s, fmt.Errorf("DNS zone yazılamadı, değişiklik geri alındı: %w", zerr)
	}
	return s, nil
}

func geriAl(ctx context.Context, db *sql.DB, domainID int64, ipv4, ftpHost, dbHost string, eskiler []dnsEski) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, e := range eskiler {
		if _, err := tx.ExecContext(ctx, `UPDATE dns_records SET deger=? WHERE id=? AND domain_id=?`, e.deger, e.id, domainID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE domains SET ipv4=?, ftp_host=?, db_host=? WHERE id=?`, ipv4, ftpHost, dbHost, domainID); err != nil {
		return err
	}
	return tx.Commit()
}

// IPSecenek — hedef IP seçim listesinin bir satırı.
type IPSecenek struct {
	IP       string `json:"ip"`
	Iface    string `json:"iface"`
	Primary  bool   `json:"primary"`
	PanelIP  bool   `json:"panel_ip"`
	Kullanim int    `json:"kullanim"` // bu IP'deki domain sayısı
}

// IPSecenekListesi — yöneticinin hedef olarak seçebileceği IP'ler: sunucunun TÜM
// global IPv4'leri; Kullanim = o IP'deki domain sayısı. (Bayiye IP seçimi yok —
// kullanıcı kararı 2026-10-03; eski bayi kapsamı kaldırıldı.)
func IPSecenekListesi(ctx context.Context, db *sql.DB) ([]IPSecenek, error) {
	sunucu := sunucuIPOku()
	kullanim := map[string]int{}
	rows, err := db.QueryContext(ctx, `SELECT ipv4, COUNT(*) FROM domains WHERE ipv4<>'' GROUP BY ipv4`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var ip string
		var n int
		if rows.Scan(&ip, &n) == nil {
			kullanim[ip] = n
		}
	}
	rows.Close()
	out := make([]IPSecenek, 0, len(sunucu))
	gor := map[string]bool{}
	for _, s := range sunucu {
		if gor[s.IP] {
			continue
		}
		gor[s.IP] = true
		out = append(out, IPSecenek{IP: s.IP, Iface: s.Iface, Primary: s.PrimaryMi, PanelIP: s.PanelIP, Kullanim: kullanim[s.IP]})
	}
	return out, nil
}
