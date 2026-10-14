package dns

// ek_zone.go — EK (addon) alan adı DNS bölgesi.
//
// Ek alan adı, hosting hesabının (tek sistem kullanıcısı) altındaki ayrı bir alan
// adıdır ve KENDİ bölgesine sahiptir. Kayıtları ek_dns_kayitlari tablosundadır:
// dns_records ile KARIŞMAZ (hesabın ana bölgesi etkilenmez, ana bölgenin DNS
// düzenleme sayfaları ek kayıtları görmez). Bölge dosyası ana alan adıyla AYNI
// yazıcıdan geçer (zoneDosyaYaz: geçici dosya + named-checkzone + checkconf'lu
// include + reload).
//
// E-POSTA v1'de ek alan adı için KAPALIDIR: şablondan MX/SPF/DKIM/DMARC/SRV ve
// posta ana makinesi A kayıtları (mail/webmail/smtp/imap/pop/autoconfig/
// autodiscover) ek bölgeye ALINMAZ — olmayan bir posta hizmetine işaret eden
// kayıt yanıltıcı olurdu.

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
)

// ekSablonUygun — şablon satırı ek alan adı bölgesine girer mi? Yalnız NS ve
// web/ad sunucusu A kayıtları (@, www, ns1, ns2).
func ekSablonUygun(t TemplateRow) bool {
	if !t.Aktif {
		return false
	}
	switch strings.ToUpper(strings.TrimSpace(t.Tip)) {
	case "NS":
		return true
	case "A":
		switch strings.ToLower(strings.TrimSpace(t.Ad)) {
		case "@", "www", "ns1", "ns2":
			return true
		}
	}
	return false
}

// EkKayitSatiri — ek bölgeye eklenecek kayıt (saf; DB yok).
type EkKayitSatiri struct {
	Ad, Tip, Deger string
	TTL, Oncelik   int
}

// EkSablonKayitlari — şablondan ek alan adı kayıtlarını üretir (saf). Şablon NS ya
// da @ A kaydı içermiyorsa (bozuk/özel şablon) güvenli varsayılan eklenir:
// named-checkzone NS'siz bölgeyi reddeder.
func EkSablonKayitlari(rows []TemplateRow, alanAdi, ipv4 string) []EkKayitSatiri {
	if ipv4 == "" {
		ipv4 = "127.0.0.1"
	}
	var out []EkKayitSatiri
	ns, kok := 0, 0
	for _, t := range rows {
		if !ekSablonUygun(t) {
			continue
		}
		tip := strings.ToUpper(strings.TrimSpace(t.Tip))
		ad := subst(strings.TrimSpace(t.Ad), alanAdi, ipv4, "", "")
		deger := subst(strings.TrimSpace(t.Deger), alanAdi, ipv4, "", "")
		if ad == "" || deger == "" {
			continue
		}
		if KayitAlanlariGecerli(ad, deger) != nil {
			continue
		}
		if tip == "NS" {
			ns++
		}
		if tip == "A" && ad == "@" {
			kok++
		}
		ttl := t.TTL
		if ttl <= 0 {
			ttl = 3600
		}
		out = append(out, EkKayitSatiri{Ad: ad, Tip: tip, Deger: deger, TTL: ttl})
	}
	if kok == 0 {
		out = append(out, EkKayitSatiri{Ad: "@", Tip: "A", Deger: ipv4, TTL: 3600},
			EkKayitSatiri{Ad: "www", Tip: "A", Deger: ipv4, TTL: 3600})
	}
	if ns == 0 {
		out = append(out,
			EkKayitSatiri{Ad: "@", Tip: "NS", Deger: "ns1." + alanAdi, TTL: 86400},
			EkKayitSatiri{Ad: "@", Tip: "NS", Deger: "ns2." + alanAdi, TTL: 86400},
			EkKayitSatiri{Ad: "ns1", Tip: "A", Deger: ipv4, TTL: 3600},
			EkKayitSatiri{Ad: "ns2", Tip: "A", Deger: ipv4, TTL: 3600})
	}
	return out
}

// EkSeed — ek alan adının bölge kayıtlarını merkezi şablondan (bayinin şablonu
// varsa o) üretir. İdempotent: aynı ad+tip+değer varsa atlanır.
func EkSeed(ctx context.Context, db *sql.DB, ekID int64, alanAdi, ipv4 string, resellerID int64) (int, error) {
	rows, err := LoadTemplateFor(ctx, db, resellerID)
	if err != nil || len(rows) == 0 {
		rows = builtinDefaults()
	}
	eklenen := 0
	for _, k := range EkSablonKayitlari(rows, alanAdi, ipv4) {
		var n int
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM ek_dns_kayitlari WHERE ek_alan_id=? AND ad=? AND tip=? AND deger=?`,
			ekID, k.Ad, k.Tip, k.Deger).Scan(&n); err != nil {
			return eklenen, err
		}
		if n > 0 {
			continue
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO ek_dns_kayitlari(ek_alan_id, ad, tip, deger, ttl, oncelik, aktif) VALUES(?,?,?,?,?,?,1)`,
			ekID, k.Ad, k.Tip, k.Deger, k.TTL, k.Oncelik); err != nil {
			return eklenen, err
		}
		eklenen++
	}
	return eklenen, nil
}

// EkKayitlari — ek alan adının kayıtları (salt okuma).
func EkKayitlari(ctx context.Context, db *sql.DB, ekID int64) ([]Kayit, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, ek_alan_id, ad, tip, deger, ttl, oncelik, aktif, DATE_FORMAT(created_at,'%Y-%m-%d %H:%i')
		   FROM ek_dns_kayitlari WHERE ek_alan_id=? ORDER BY tip, ad`, ekID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Kayit, 0)
	for rows.Next() {
		k, err := scan(rows) // DomainID alanına ek_alan_id yazılır (aynı şekil)
		if err == nil {
			out = append(out, k)
		}
	}
	return out, rows.Err()
}

// WriteEkZone — ek alan adının bölge dosyasını DB'den yazar (idempotent).
func WriteEkZone(ctx context.Context, db *sql.DB, ekID int64) error {
	var alanAdi string
	if err := db.QueryRowContext(ctx, `SELECT alan_adi FROM ek_alan_adlari WHERE id=?`, ekID).Scan(&alanAdi); err != nil {
		return err
	}
	// 🔴 path traversal: ana bölgedeki kapının aynısı (DB'den geri okunan ad).
	if strings.ContainsAny(alanAdi, "/\\") || strings.Contains(alanAdi, "..") {
		return fmt.Errorf("geçersiz alan adı: %q", alanAdi)
	}
	kayitlar, err := EkKayitlari(ctx, db, ekID)
	if err != nil {
		return err
	}
	aktif := kayitlar[:0]
	for _, k := range kayitlar {
		if k.Aktif {
			aktif = append(aktif, k)
		}
	}
	if len(aktif) == 0 {
		return nil
	}
	soa := defaultSOA(alanAdi)
	m := LoadTemplateMeta(ctx, db)
	if m.SOARefresh > 0 {
		soa.Refresh = m.SOARefresh
	}
	if m.SOARetry > 0 {
		soa.Retry = m.SOARetry
	}
	if m.SOAExpire > 0 {
		soa.Expire = m.SOAExpire
	}
	if m.SOAMinimum > 0 {
		soa.Minimum = m.SOAMinimum
	}
	if m.SOATTL > 0 {
		soa.TTL = m.SOATTL
	}
	return zoneDosyaYaz(ctx, db, alanAdi, soa, aktif)
}

// DeleteEkZone — ek alan adının bölge dosyasını kaldırır ve include'u yeniden üretir.
// Satırlar ÖNCE silinmiş olmalıdır (include DB'den üretilir; eski sıra yetim include
// bırakırdı — DeleteZone ile aynı ders).
func DeleteEkZone(ctx context.Context, db *sql.DB, alanAdi string) error {
	return DeleteZone(ctx, db, alanAdi)
}

// ekZoneIncludeAdlari — kaydı olan ek alan adlarının bölge adları. Tablo yoksa
// (0160 koşmamış) boş döner.
func ekZoneIncludeAdlari(ctx context.Context, db sorgucu) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT e.alan_adi FROM ek_alan_adlari e
	  WHERE EXISTS (SELECT 1 FROM ek_dns_kayitlari r WHERE r.ek_alan_id=e.id AND r.aktif=1) ORDER BY e.alan_adi`)
	if err != nil {
		if strings.Contains(err.Error(), "1146") || strings.Contains(strings.ToLower(err.Error()), "doesn't exist") {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		if rows.Scan(&a) != nil {
			continue
		}
		// Include dosyasına yazılan ad: kaçış/enjeksiyon savunması (tırnak, ';', boşluk).
		if strings.ContainsAny(a, "\"'; \t\r\n{}/\\") || strings.Contains(a, "..") {
			log.Printf("dns ek zone include: geçersiz ad atlandı: %q", a)
			continue
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
