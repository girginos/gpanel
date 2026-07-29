// Package dns — porta.go: BIND zone dosyasi ICE/DISA aktarma (Plesk paritesi).
//
//	Disa aktar (export): domain'in DNS kayitlarini + SOA'yi standart BIND zone
//	  dosyasi olarak dondurur (indirilebilir). Baska panele/saglayiciya tasinabilir.
//	Ice aktar (import): yuklenen BIND zone dosyasini ayristirip kayitlari ekler
//	  (birlestir) veya mevcutlarin yerine koyar (degistir); sonra zone yeniden
//	  render + named-checkzone dogrulamasi + reload (WriteZone).
package dns

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

// tipSirasi: export'ta okunabilir gruplama sirasi.
var tipSirasi = map[string]int{
	"NS": 0, "A": 1, "AAAA": 2, "CNAME": 3, "MX": 4, "TXT": 5,
	"SRV": 6, "CAA": 7, "PTR": 8, "DS": 9, "TLSA": 10, "SSHFP": 11, "NAPTR": 12,
}

// ---------------- DISA AKTAR (export) ----------------

func (h *Handlers) DisaAktar(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	alanAdi, _, err := h.lookup(r)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	soa := LoadSOA(r.Context(), h.DB, id, alanAdi)
	rows, err := h.DB.QueryContext(r.Context(), selectAll+" WHERE domain_id=? AND aktif=1", id)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	var ks []Kayit
	for rows.Next() {
		if k, e := scan(rows); e == nil {
			ks = append(ks, k)
		}
	}
	zone := renderBindZone(alanAdi, soa, ks)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+alanAdi+`.zone"`)
	_, _ = w.Write([]byte(zone))
}

// renderBindZone: kayitlar + SOA'dan standart, tasinabilir BIND zone metni uretir.
func renderBindZone(alanAdi string, soa SOA, ks []Kayit) string {
	origin := strings.TrimSuffix(alanAdi, ".") + "."
	var b strings.Builder
	fmt.Fprintf(&b, "$ORIGIN %s\n$TTL %d\n", origin, soa.TTL)
	fmt.Fprintf(&b, "@\tIN\tSOA\t%s %s (\n", soaHost(soa.PrimaryNS), soaMail(soa.Hostmaster))
	fmt.Fprintf(&b, "\t\t\t%s ; serial\n", time.Now().UTC().Format("20060102")+"01")
	fmt.Fprintf(&b, "\t\t\t%d ; refresh\n", soa.Refresh)
	fmt.Fprintf(&b, "\t\t\t%d ; retry\n", soa.Retry)
	fmt.Fprintf(&b, "\t\t\t%d ; expire\n", soa.Expire)
	fmt.Fprintf(&b, "\t\t\t%d ; minimum\n\t\t\t)\n", soa.Minimum)

	sort.SliceStable(ks, func(i, j int) bool {
		ti, tj := tipSirasi[ks[i].Tip], tipSirasi[ks[j].Tip]
		if ti != tj {
			return ti < tj
		}
		return ks[i].Ad < ks[j].Ad
	})
	sonTip := ""
	for _, k := range ks {
		if k.Tip != sonTip {
			fmt.Fprintf(&b, "\n; %s\n", k.Tip)
			sonTip = k.Tip
		}
		ad := k.Ad
		if ad == "" {
			ad = "@"
		}
		onc := ""
		if k.Tip == "MX" || k.Tip == "SRV" {
			onc = strconv.Itoa(k.Oncelik) + " "
		}
		fmt.Fprintf(&b, "%s\t%d\tIN\t%s\t%s%s\n", ad, k.TTL, k.Tip, onc, rdata(k.Tip, k.Deger))
	}
	return b.String()
}

// ---------------- ICE AKTAR (import) ----------------

func (h *Handlers) IceAktar(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	alanAdi, isDemo, err := h.lookup(r)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if isDemo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin DNS'i değiştirilemez")
		return
	}

	var icerik []byte
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20) // zone dosyasi kucuktur
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		if e := r.ParseMultipartForm(2 << 20); e != nil {
			httpx.WriteError(w, http.StatusBadRequest, "yükleme okunamadı")
			return
		}
		f, _, e := r.FormFile("dosya")
		if e != nil {
			httpx.WriteError(w, http.StatusBadRequest, "dosya alanı bulunamadı")
			return
		}
		defer f.Close()
		icerik, _ = io.ReadAll(f)
	} else {
		icerik, _ = io.ReadAll(r.Body)
	}
	if len(strings.TrimSpace(string(icerik))) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "boş zone içeriği")
		return
	}

	ks, soaP := parseBindZone(string(icerik), alanAdi)
	if len(ks) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "geçerli DNS kaydı bulunamadı (dosya biçimini kontrol edin)")
		return
	}

	degistir := r.URL.Query().Get("mod") == "degistir"

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()

	if degistir {
		if _, e := tx.ExecContext(r.Context(), `DELETE FROM dns_records WHERE domain_id=?`, id); e != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "eski kayıtlar silinemedi: "+e.Error())
			return
		}
	}
	eklenen, atlanan := 0, 0
	for _, k := range ks {
		if !degistir {
			var n int
			_ = tx.QueryRowContext(r.Context(),
				`SELECT COUNT(*) FROM dns_records WHERE domain_id=? AND ad=? AND tip=? AND deger=?`,
				id, k.Ad, k.Tip, k.Deger).Scan(&n)
			if n > 0 {
				atlanan++
				continue
			}
		}
		if _, e := tx.ExecContext(r.Context(),
			`INSERT INTO dns_records(domain_id, ad, tip, deger, ttl, oncelik, aktif)
			 VALUES(?,?,?,?,?,?, 1)`,
			id, k.Ad, k.Tip, k.Deger, k.TTL, oncelikNormalize(k.Tip, k.Oncelik)); e != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "kayıt eklenemedi: "+e.Error())
			return
		}
		eklenen++
	}
	if soaP != nil {
		_, _ = tx.ExecContext(r.Context(),
			`INSERT INTO dns_soa(domain_id, primary_ns, hostmaster, refresh, retry, expire, minimum, ttl)
			 VALUES(?,?,?,?,?,?,?,?)
			 ON DUPLICATE KEY UPDATE primary_ns=VALUES(primary_ns), hostmaster=VALUES(hostmaster),
			   refresh=VALUES(refresh), retry=VALUES(retry), expire=VALUES(expire),
			   minimum=VALUES(minimum), ttl=VALUES(ttl)`,
			id, soaP.PrimaryNS, soaP.Hostmaster, soaP.Refresh, soaP.Retry, soaP.Expire, soaP.Minimum, soaP.TTL)
	}
	if e := tx.Commit(); e != nil {
		httpx.WriteError(w, http.StatusInternalServerError, e.Error())
		return
	}

	zoneUyari := ""
	if zerr := WriteZone(r.Context(), h.DB, id); zerr != nil {
		zoneUyari = "kayıtlar kaydedildi ancak zone doğrulaması uyarı verdi: " + zerr.Error()
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"eklenen": eklenen,
		"atlanan": atlanan,
		"mod":     map[bool]string{true: "değiştir", false: "birleştir"}[degistir],
		"uyari":   zoneUyari,
	})
}

// ---------------- BIND ayristirici ----------------

// parseBindZone: BIND zone metnini ayristirir → kayitlar + (varsa) SOA.
// $ORIGIN/$TTL, @ apex, goreli/mutlak ad, ( ) cok-satir, ; yorum, TXT tirnak
// birlestirme, MX/SRV oncelik desteklenir. Desteklenmeyen/gecersiz satirlar atlanir.
func parseBindZone(text, alanAdi string) ([]Kayit, *SOA) {
	origin := strings.TrimSuffix(alanAdi, ".") + "."
	defaultTTL := 3600
	var out []Kayit
	var soa *SOA
	sonAd := "@"

	for _, ham := range logicalLines(text) {
		satir := parenSil(ham) // yorum zaten logicalLines'ta ayiklandi
		if strings.TrimSpace(satir) == "" {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(satir), "$") {
			f := strings.Fields(satir)
			switch strings.ToUpper(f[0]) {
			case "$ORIGIN":
				if len(f) >= 2 {
					origin = f[1]
					if !strings.HasSuffix(origin, ".") {
						origin += "."
					}
				}
			case "$TTL":
				if len(f) >= 2 {
					if n, e := strconv.Atoi(f[1]); e == nil {
						defaultTTL = n
					}
				}
			}
			continue
		}

		// ad: satir bosluk/tab ile basliyorsa onceki ad tekrar; degilse ilk token
		var ad, rest string
		if satir[0] == ' ' || satir[0] == '\t' {
			ad = sonAd
			rest = strings.TrimLeft(satir, " \t")
		} else {
			ff := strings.Fields(satir)
			ad = ff[0]
			rest = strings.TrimSpace(satir[len(ad):])
		}
		sonAd = ad
		adRel := relName(ad, origin)

		toks := strings.Fields(rest)
		if len(toks) == 0 {
			continue
		}
		// opsiyonel TTL + class (herhangi sirada)
		ttl := defaultTTL
		i := 0
		for i < len(toks) {
			if n, e := strconv.Atoi(toks[i]); e == nil {
				ttl = n
				i++
				continue
			}
			up := strings.ToUpper(toks[i])
			if up == "IN" || up == "CH" || up == "HS" {
				i++
				continue
			}
			break
		}
		if i >= len(toks) {
			continue
		}
		tip := strings.ToUpper(toks[i])
		rdataToks := toks[i+1:]

		if tip == "SOA" {
			if s := parseSOA(rdataToks, ttl); s != nil {
				soa = s
			}
			continue
		}
		if !gecerliTip(tip) || len(rdataToks) == 0 {
			continue
		}

		k := Kayit{Ad: adRel, Tip: tip, TTL: ttl}
		switch tip {
		case "MX":
			if len(rdataToks) >= 2 {
				k.Oncelik, _ = strconv.Atoi(rdataToks[0])
				k.Deger = trimDot(rdataToks[1])
			} else {
				k.Deger = trimDot(rdataToks[0])
			}
		case "SRV":
			if len(rdataToks) >= 4 {
				k.Oncelik, _ = strconv.Atoi(rdataToks[0])
				k.Deger = rdataToks[1] + " " + rdataToks[2] + " " + trimDot(rdataToks[3])
			} else {
				k.Deger = strings.Join(rdataToks, " ")
			}
		case "TXT":
			k.Deger = unquoteTXT(rdataToks)
		case "CNAME", "NS", "PTR":
			k.Deger = trimDot(rdataToks[0])
		default:
			k.Deger = strings.Join(rdataToks, " ")
		}
		if k.Deger == "" || strings.ContainsAny(k.Deger, "\r\n") || strings.ContainsAny(k.Ad, " \t\r\n") {
			continue
		}
		out = append(out, k)
	}
	return out, soa
}

// parseSOA: SOA rdata (mname rname serial refresh retry expire minimum) → SOA.
func parseSOA(t []string, ttl int) *SOA {
	if len(t) < 7 {
		return nil
	}
	atoi := func(s string) int { n, _ := strconv.Atoi(s); return n }
	return &SOA{
		PrimaryNS:  trimDot(t[0]),
		Hostmaster: soaMailToEmail(t[1]),
		Refresh:    atoi(t[3]),
		Retry:      atoi(t[4]),
		Expire:     atoi(t[5]),
		Minimum:    atoi(t[6]),
		TTL:        ttl,
	}
}

// ---------------- yardimcilar ----------------

// logicalLines: her fiziksel satirdan yorumu (;) ayiklar, sonra ( ) devam
// bloklarini tek mantiksal satira birlestirir (tirnak-farkinda).
func logicalLines(text string) []string {
	var temiz []string
	for _, ln := range strings.Split(text, "\n") {
		temiz = append(temiz, yorumSil(strings.TrimRight(ln, "\r")))
	}
	var res []string
	var cur strings.Builder
	paren := 0
	for _, ln := range temiz {
		a, k := sayParen(ln)
		if paren > 0 {
			cur.WriteByte(' ')
			cur.WriteString(ln)
			paren += a - k
			if paren <= 0 {
				res = append(res, cur.String())
				cur.Reset()
				paren = 0
			}
			continue
		}
		if a > k {
			cur.WriteString(ln)
			paren = a - k
			continue
		}
		res = append(res, ln)
	}
	if cur.Len() > 0 {
		res = append(res, cur.String())
	}
	return res
}

// yorumSil: tirnak-disi ilk ';'ten satir sonuna kadar olan yorumu siler (parantez KALIR).
func yorumSil(ln string) string {
	inq := false
	for i := 0; i < len(ln); i++ {
		if ln[i] == '"' {
			inq = !inq
		} else if !inq && ln[i] == ';' {
			return ln[:i]
		}
	}
	return ln
}

// sayParen: tirnak-disi ( ve ) sayilari (yorum zaten ayiklandi).
func sayParen(ln string) (acik, kapali int) {
	inq := false
	for i := 0; i < len(ln); i++ {
		c := ln[i]
		if c == '"' {
			inq = !inq
			continue
		}
		if inq {
			continue
		}
		if c == '(' {
			acik++
		} else if c == ')' {
			kapali++
		}
	}
	return
}

// parenSil: tirnak-disi ( ve ) karakterlerini boslukla degistirir.
func parenSil(ln string) string {
	inq := false
	var b strings.Builder
	for i := 0; i < len(ln); i++ {
		c := ln[i]
		if c == '"' {
			inq = !inq
			b.WriteByte(c)
			continue
		}
		if !inq && (c == '(' || c == ')') {
			b.WriteByte(' ')
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// relName: mutlak/@/goreli adi panelin sakladigi goreli-label'a cevirir.
func relName(name, origin string) string {
	name = strings.TrimSpace(name)
	o := strings.TrimSuffix(origin, ".")
	if name == "@" || name == origin || name == o {
		return "@"
	}
	if strings.HasSuffix(name, ".") {
		n := strings.TrimSuffix(name, ".")
		if n == o {
			return "@"
		}
		if strings.HasSuffix(n, "."+o) {
			return strings.TrimSuffix(n, "."+o)
		}
		return n
	}
	return name
}

// trimDot: hedef adin sonundaki noktayi kaldirir (render fqdn tekrar ekler).
func trimDot(s string) string { return strings.TrimSuffix(strings.TrimSpace(s), ".") }

// unquoteTXT: TXT char-string token'larini birlestirip tirnaklari cikarir.
func unquoteTXT(toks []string) string {
	joined := strings.Join(toks, " ")
	var b strings.Builder
	inq := false
	sawQuote := false
	for i := 0; i < len(joined); i++ {
		c := joined[i]
		if c == '"' {
			inq = !inq
			sawQuote = true
			continue
		}
		if inq {
			b.WriteByte(c)
		}
	}
	if !sawQuote {
		return strings.TrimSpace(joined) // tirnaksiz TXT
	}
	return b.String()
}

// soaMailToEmail: zone RNAME (admin.alan.com.) -> e-posta (admin@alan.com).
func soaMailToEmail(rname string) string {
	r := trimDot(rname)
	if i := strings.Index(r, "."); i >= 0 {
		return r[:i] + "@" + r[i+1:]
	}
	return r
}
