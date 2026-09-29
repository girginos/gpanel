package tasima

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	"girginospanel/internal/dns"
)

// ---------------------------------------------------------------------------
// DNS aktarimi
// ---------------------------------------------------------------------------

// kaynakUstunTipler — kaynagin degeri panel varsayilanini EZMELIDIR. MX/TXT
// (SPF, DKIM, DMARC) mustericinin e-posta akisidir; varsayilanla degistirilirse
// tasima aninda tum e-posta durur.
var kaynakUstunTipler = map[string]bool{"MX": true, "TXT": true, "CNAME": true, "SRV": true, "CAA": true}

func (h *Handlers) dnsAktar(ctx context.Context, k *Kaynak, domainID int64, alanAdi string, log func(string, ...any)) (int, error) {
	if _, err := dns.SeedDefaults(ctx, h.DB, domainID, alanAdi, h.sunucuIP()); err != nil {
		log("uyari: DNS varsayilanlari yazilamadi: %v", err)
	}

	kayitlar := kaynakZoneOku(ctx, k, alanAdi)
	if len(kayitlar) == 0 {
		if err := dns.WriteZone(ctx, h.DB, domainID); err != nil {
			return 0, err
		}
		return 0, fmt.Errorf("kaynak DNS kayitlari okunamadi")
	}
	yeniIP := h.sunucuIP()
	eskiIP := ""
	for _, kt := range kayitlar {
		if kt.Tip == "A" && kt.Ad == "@" {
			eskiIP = kt.Deger
			break
		}
	}
	if eskiIP == "" && net.ParseIP(k.Host) != nil {
		eskiIP = k.Host
	}

	eklenen := 0
	temizlenen := map[string]bool{}
	for _, kt := range kayitlar {
		if kt.Tip == "AAAA" || kt.Tip == "NS" {
			continue // eski IPv6 gecersiz; NS panelin kendi sunuculari olmali
		}
		deger := kt.Deger
		if kt.Tip == "A" && (deger == eskiIP || kt.Ad == "@" || kt.Ad == "www") {
			deger = yeniIP
		}
		anahtar := kt.Ad + "|" + kt.Tip
		if kt.Tip == "CNAME" {
			// 🔴 CNAME bir isim icin TEK kayit olmali (RFC 1034): ayni isimde A/TXT
			// vb. OLAMAZ. SeedDefaults'un olusturdugu 'www A' gibi kayitlari da sil,
			// yoksa named-checkzone "CNAME and other data" ile zone'u REDDEDER.
			if !temizlenen[kt.Ad+"|*"] {
				_, _ = h.DB.ExecContext(ctx,
					`DELETE FROM dns_records WHERE domain_id=? AND ad=?`, domainID, kt.Ad)
				temizlenen[kt.Ad+"|*"] = true
			}
		} else if kaynakUstunTipler[kt.Tip] {
			// Kaynak bu (ad,tip) icin kayit veriyorsa panel varsayilanini bir
			// kez temizle, sonra kaynagin TUM kayitlarini ekle.
			if !temizlenen[anahtar] {
				_, _ = h.DB.ExecContext(ctx,
					`DELETE FROM dns_records WHERE domain_id=? AND ad=? AND tip=?`,
					domainID, kt.Ad, kt.Tip)
				temizlenen[anahtar] = true
			}
		} else {
			// CNAME olan bir isme baska kayit EKLEME (cakisma).
			var cn int
			_ = h.DB.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM dns_records WHERE domain_id=? AND ad=? AND tip='CNAME'`,
				domainID, kt.Ad).Scan(&cn)
			if cn > 0 {
				continue
			}
			var v int
			_ = h.DB.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM dns_records WHERE domain_id=? AND ad=? AND tip=?`,
				domainID, kt.Ad, kt.Tip).Scan(&v)
			if v > 0 {
				continue
			}
		}
		var ayni int
		_ = h.DB.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM dns_records WHERE domain_id=? AND ad=? AND tip=? AND deger=?`,
			domainID, kt.Ad, kt.Tip, deger).Scan(&ayni)
		if ayni > 0 {
			continue
		}
		if _, err := h.DB.ExecContext(ctx,
			`INSERT INTO dns_records (domain_id, ad, tip, deger, ttl, oncelik, aktif)
			 VALUES (?,?,?,?,?,?,1)`,
			domainID, kt.Ad, kt.Tip, deger, kt.TTL, kt.Oncelik); err == nil {
			eklenen++
		}
	}
	if err := dns.WriteZone(ctx, h.DB, domainID); err != nil {
		return eklenen, fmt.Errorf("zone yazilamadi: %w", err)
	}
	log("DNS: %d kayit aktarildi", eklenen)
	return eklenen, nil
}

// kaynakZoneOku — kaynagin DNS kayitlarini uzaktan okur (yalniz okuma; DB'ye ve
// yerel zone'a dokunmaz). Okunamazsa bos doner.
func kaynakZoneOku(ctx context.Context, k *Kaynak, alanAdi string) []zoneKayit {
	q := shQuote(alanAdi)
	var kayitlar []zoneKayit

	// Plesk DNS'i zone DOSYASINDA degil kendi deposunda tutar → `plesk bin dns
	// --info` ile cekilir (FQDN TYPE [pri] deger bicimi). cPanel/DA icin ham
	// BIND zone dosyasi okunur.
	if k.Tip == "plesk" {
		if ham, err := k.Calistir(ctx, "plesk bin dns --info "+q+" 2>/dev/null"); err == nil {
			kayitlar = pleskDNSAyristir(ham, alanAdi)
		}
	}
	if len(kayitlar) == 0 {
		komut := "cat /var/named/" + q + ".db 2>/dev/null || " +
			"cat /var/named/run-root/var/named/" + q + ".db 2>/dev/null || " +
			"cat /var/lib/named/var/named/" + q + ".db 2>/dev/null || " +
			"cat /etc/bind/db." + q + " 2>/dev/null || " +
			"cat /var/named/data/" + q + ".db 2>/dev/null"
		if ham, err := k.Calistir(ctx, komut); err == nil && strings.TrimSpace(ham) != "" {
			kayitlar = zoneAyristir(ham, alanAdi)
		}
	}
	return kayitlar
}

type zoneKayit struct {
	Ad, Tip, Deger string
	TTL, Oncelik   int
}

var tasinabilirTipler = map[string]bool{
	"A": true, "CNAME": true, "MX": true, "TXT": true, "SRV": true, "CAA": true,
}

// pleskDNSAyristir — `plesk bin dns --info <domain>` ciktisini kayitlara cevirir.
// Bicim satir basina: "<fqdn>. <TYPE> [<pri> [<weight> <port>]] <deger>"
func pleskDNSAyristir(ham, alanAdi string) []zoneKayit {
	var out []zoneKayit
	for _, satir := range strings.Split(ham, "\n") {
		f := strings.Fields(strings.TrimSpace(satir))
		if len(f) < 3 {
			continue
		}
		tip := strings.ToUpper(f[1])
		if !tasinabilirTipler[tip] {
			continue
		}
		ad := strings.TrimSuffix(f[0], ".")
		ad = strings.TrimSuffix(ad, "."+alanAdi)
		if ad == alanAdi || ad == "" {
			ad = "@"
		}
		oncelik := 0
		var deger string
		switch tip {
		case "MX":
			if len(f) < 4 {
				continue
			}
			oncelik, _ = strconv.Atoi(f[2])
			deger = strings.TrimSuffix(f[3], ".")
		case "SRV":
			if len(f) < 6 {
				continue
			}
			oncelik, _ = strconv.Atoi(f[2])
			deger = f[3] + " " + f[4] + " " + strings.TrimSuffix(f[5], ".")
		case "CNAME":
			deger = strings.TrimSuffix(f[2], ".")
		default: // A, AAAA, TXT, CAA
			deger = strings.Join(f[2:], " ")
		}
		if deger == "" || len(deger) > 2048 || len(ad) > 100 {
			continue
		}
		out = append(out, zoneKayit{Ad: ad, Tip: tip, Deger: deger, TTL: 3600, Oncelik: oncelik})
	}
	return out
}

// zoneAyristir — BIND zone metnini kayitlara cevirir.
//
// 🔴 Yorum ayiklama TIRNAK FARKINDA olmali: DMARC/DKIM/SPF degerlerinde ';'
// VERIDIR. Onceki surum ilk ';' karakterinde kesip "v=DMARC1" birakiyordu.
// Ayrica bolunmus tirnakli dizeler ("v=DKIM1;" "p=MIG…") birlestirilir.
func zoneAyristir(ham, alanAdi string) []zoneKayit {
	var out []zoneKayit
	sonAd := "@"
	parenDerinlik := 0

	for _, hamSatir := range strings.Split(ham, "\n") {
		satir := yorumAyikla(hamSatir)
		if parenDerinlik > 0 {
			parenDerinlik += strings.Count(satir, "(") - strings.Count(satir, ")")
			continue // SOA gibi cok satirli blogun govdesi
		}
		if strings.TrimSpace(satir) == "" || strings.HasPrefix(strings.TrimSpace(satir), "$") {
			continue
		}
		if strings.Contains(strings.ToUpper(satir), "SOA") {
			parenDerinlik += strings.Count(satir, "(") - strings.Count(satir, ")")
			continue
		}
		if a := strings.Count(satir, "(") - strings.Count(satir, ")"); a > 0 {
			parenDerinlik += a
			continue
		}

		alanlar := strings.Fields(satir)
		if len(alanlar) < 3 {
			continue
		}
		ad := sonAd
		i := 0
		if !strings.HasPrefix(hamSatir, " ") && !strings.HasPrefix(hamSatir, "\t") {
			ad = alanlar[0]
			i = 1
		}
		ttl := 3600
		for ; i < len(alanlar); i++ {
			u := strings.ToUpper(alanlar[i])
			if u == "IN" {
				continue
			}
			if n, err := strconv.Atoi(alanlar[i]); err == nil {
				ttl = n
				continue
			}
			break
		}
		if i >= len(alanlar) {
			continue
		}
		tip := strings.ToUpper(alanlar[i])

		// Ad normalizasyonu — desteklenmeyen tipte de guncellenmeli, yoksa
		// sonraki girintili satir YANLIS sahibe yazilir.
		adNorm := strings.TrimSuffix(ad, ".")
		adNorm = strings.TrimSuffix(adNorm, "."+alanAdi)
		if adNorm == alanAdi || adNorm == "" {
			adNorm = "@"
		}
		sonAd = adNorm

		if !tasinabilirTipler[tip] {
			continue
		}
		kalan := alanlar[i+1:]
		if len(kalan) == 0 {
			continue
		}
		oncelik := 0
		if tip == "MX" || tip == "SRV" {
			if n, err := strconv.Atoi(kalan[0]); err == nil {
				oncelik = n
				kalan = kalan[1:]
			}
		}
		if len(kalan) == 0 {
			continue
		}
		// Tek satirda tamamlanan parantezli rdata:  ( "v=DKIM1;" "p=MIG…" )
		deger := tirnakBirlestir(parenSoy(strings.Join(kalan, " ")))
		if deger == "" || len(deger) > 500 || len(adNorm) > 100 {
			continue
		}
		out = append(out, zoneKayit{Ad: adNorm, Tip: tip, Deger: deger, TTL: ttl, Oncelik: oncelik})
	}
	return out
}

// yorumAyikla — tirnak disindaki ilk ';' karakterinden itibarasini atar.
func yorumAyikla(s string) string {
	tirnakta := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			tirnakta = !tirnakta
		case ';':
			if !tirnakta {
				return s[:i]
			}
		}
	}
	return s
}

// tirnakBirlestir — `"v=DKIM1; k=rsa" "p=MIG…"` → `v=DKIM1; k=rsa p=MIG…`
// (BIND 255 bayt sinirini asan TXT'leri boler; birlestirilmezse DKIM bozulur.)
func tirnakBirlestir(s string) string {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "\"") {
		return s
	}
	var b strings.Builder
	tirnakta := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' {
			tirnakta = !tirnakta
			continue
		}
		if !tirnakta && c == ' ' {
			continue // tirnaklar arasi bosluk
		}
		b.WriteByte(c)
	}
	return strings.TrimSpace(b.String())
}

// parenSoy — tek satirda tamamlanan parantezli rdata blogunun dis parantezlerini
// atar. BIND cok-satirli/uzun TXT kayitlarini parantezle sarar; birakilirsa deger
// "(v=DKIM1; k=rsa; p=MIG…)" olarak kaydedilir ve DKIM dogrulamasi COKER.
func parenSoy(s string) string {
	s = strings.TrimSpace(s)
	for strings.HasPrefix(s, "(") {
		s = strings.TrimSpace(s[1:])
	}
	for strings.HasSuffix(s, ")") {
		s = strings.TrimSpace(s[:len(s)-1])
	}
	return strings.TrimSpace(s)
}
