package dns

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"
)

const (
	ZoneDir          = "/var/named"
	NamedConfInclude = "/etc/named/girginospanel-zones.conf"
	// DNSSECKeyDir: dnssec-policy anahtar dizini. /var/named/dynamic zaten named_cache_t
	// SELinux etiketli ve named'e yazılabilir (ekstra SELinux ayarı gerektirmez).
	DNSSECKeyDir = "/var/named/dynamic"
)

// fqdn: hedef alan adi (NS/MX/CNAME/SRV) trailing nokta ile bitmeliki BIND
// "relative" yorumlamasın (yoksa zone adi append eder ve "host.X.Y.X.Y" gibi olur).
func fqdn(tip, deger string) string {
	t := strings.ToUpper(strings.TrimSpace(tip))
	d := strings.TrimSpace(deger)
	if t == "NS" || t == "MX" || t == "CNAME" || t == "SRV" || t == "PTR" {
		if !strings.HasSuffix(d, ".") {
			d = d + "."
		}
	}
	return d
}

var zoneTmpl = template.Must(template.New("z").Funcs(template.FuncMap{
	"fqdn":    fqdn,
	"rdata":   rdata,
	"soaHost": soaHost,
	"soaMail": soaMail,
}).Parse(`$TTL {{.SOA.TTL}}
@   IN  SOA {{soaHost .SOA.PrimaryNS}} {{soaMail .SOA.Hostmaster}} (
    {{.Serial}}  ; serial
    {{.SOA.Refresh}}  ; refresh
    {{.SOA.Retry}}  ; retry
    {{.SOA.Expire}}  ; expire
    {{.SOA.Minimum}}  ; minimum
)
{{range .Kayitlar}}{{.Ad}}	{{.TTL}}	IN	{{.Tip}}	{{if or (eq .Tip "MX") (eq .Tip "SRV")}}{{.Oncelik}} {{end}}{{rdata .Tip .Deger}}
{{end}}`))

// rdata: kayit tipine gore zone rdata uretir. TXT icin tirnak/parcalama (255+ ise),
// digerleri icin fqdn (NS/MX/CNAME/SRV/PTR trailing nokta) uygular.
// TXT tirnaklanmazsa named-checkzone bosluklu SPF/DMARC/DKIM'i coklu char-string olarak
// yorumlar veya reddedebilir; DKIM 2048-bit anahtar 255 karakteri asar → parcalanmali.
func rdata(tip, deger string) string {
	if strings.ToUpper(strings.TrimSpace(tip)) == "TXT" {
		return txtQuote(deger)
	}
	return fqdn(tip, deger)
}

// txtQuote: TXT degerini gecerli zone formatina cevirir.
// Zaten tirnakli ise oldugu gibi birakir; degilse 255 karakterlik parcalara bolup
// her parcayi tirnaklar (BIND bitisik tirnakli stringleri birlestirir).
func txtQuote(s string) string {
	t := strings.TrimSpace(s)
	if strings.HasPrefix(t, "\"") {
		return t
	}
	t = strings.ReplaceAll(t, "\"", "")
	if len(t) <= 255 {
		return "\"" + t + "\""
	}
	var b strings.Builder
	for len(t) > 255 {
		b.WriteString("\"" + t[:255] + "\" ")
		t = t[255:]
	}
	b.WriteString("\"" + t + "\"")
	return b.String()
}

// soaHost: primary NS'e trailing nokta ekle (BIND relative yorumlamasın).
func soaHost(ns string) string {
	ns = strings.TrimSpace(ns)
	if ns == "" {
		return "."
	}
	if !strings.HasSuffix(ns, ".") {
		ns += "."
	}
	return ns
}

// soaMail: hostmaster e-postasını zone formatına çevir (admin@x.com -> admin.x.com.).
func soaMail(hm string) string {
	hm = strings.TrimSpace(hm)
	if i := strings.Index(hm, "@"); i >= 0 {
		hm = hm[:i] + "." + hm[i+1:]
	}
	if hm == "" {
		return "."
	}
	if !strings.HasSuffix(hm, ".") {
		hm += "."
	}
	return hm
}

type zoneCtx struct {
	AlanAdi  string
	Serial   string
	SOA      SOA
	Kayitlar []Kayit
}

// readZoneSerial: mevcut zone dosyasından SOA serial'ini oku (yoksa/okunamazsa 0 dön).
// Şablon çıktısı "    2026071815  ; serial" formatındadır.
func readZoneSerial(path string) uint32 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.Contains(line, "; serial") {
			continue
		}
		for _, f := range strings.Fields(line) {
			if n, perr := strconv.ParseUint(f, 10, 32); perr == nil {
				return uint32(n)
			}
		}
	}
	return 0
}

// nextSerial: MONOTON ARTAN SOA serial üretir (uint32 sınırı içinde, DNS RFC1982).
// Taban = yyyymmdd00 (günlük sayaç için 2 hane). Aynı gün (veya eski format yyyymmddHH)
// içinde tekrar yazımda mevcut serial'i +1 artırır → aynı dakikada 2 düzenlemede bile
// serial ilerler; BIND değişikliği görür ve DNSSEC yeniden-imzalaması tetiklenir.
// (Eski yyyymmddHH şeması aynı saat içinde sabit kalıyor, DNSSEC re-sign kaçıyordu.)
func nextSerial(old uint32) uint32 {
	now := time.Now().UTC()
	base := uint32(now.Year()*1000000 + int(now.Month())*10000 + now.Day()*100)
	if old >= base {
		return old + 1
	}
	return base
}

func WriteZone(ctx context.Context, db *sql.DB, domainID int64) error {
	var alanAdi string
	if err := db.QueryRowContext(ctx, `SELECT alan_adi FROM domains WHERE id=?`, domainID).Scan(&alanAdi); err != nil {
		return err
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id, domain_id, ad, tip, deger, ttl, oncelik, aktif,
		   DATE_FORMAT(created_at,'%Y-%m-%d %H:%i') FROM dns_records
		 WHERE domain_id=? AND aktif=1 ORDER BY tip, ad`, domainID)
	if err != nil {
		return err
	}
	defer rows.Close()
	kayitlar := make([]Kayit, 0)
	for rows.Next() {
		k, err := scan(rows)
		if err == nil {
			kayitlar = append(kayitlar, k)
		}
	}
	if len(kayitlar) == 0 {
		return nil
	}

	_ = os.MkdirAll(ZoneDir, 0750)
	zonePath := filepath.Join(ZoneDir, alanAdi+".zone")

	// serial: MONOTON ARTAN (mevcut serial'i baz alır). Aynı dakikada 2 düzenlemede bile
	// ilerler → BIND değişikliği görür, DNSSEC yeniden-imzalama tetiklenir.
	serial := strconv.FormatUint(uint64(nextSerial(readZoneSerial(zonePath))), 10)

	soa := LoadSOA(ctx, db, domainID, alanAdi)
	var buf bytes.Buffer
	if err := zoneTmpl.Execute(&buf, zoneCtx{AlanAdi: alanAdi, Serial: serial, SOA: soa, Kayitlar: kayitlar}); err != nil {
		return err
	}

	// Önce GEÇİCİ dosyaya yaz + named-checkzone ile doğrula; SADECE geçerliyse
	// canlı zone dosyasının üzerine taşı. Böylece hatalı bir kayıt (ör. A kaydına
	// yanlışlıkla öncelik) asla çalışan zone'u bozmaz ve SSH'tan elle düzeltme gerektirmez.
	tmpPath := zonePath + ".tmp"
	if err := os.WriteFile(tmpPath, buf.Bytes(), 0640); err != nil {
		return err
	}
	if out, err := exec.Command("named-checkzone", alanAdi, tmpPath).CombinedOutput(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("zone geçersiz (%s): %s", alanAdi, strings.TrimSpace(string(out)))
	}
	// .bak yedeği: atomik-rename mevcut dosyanın üzerine yazar; taşımadan ÖNCE bir kopya
	// bırak ki hatalı bir düzenlemeden geri dönüş (veya denetim) mümkün olsun.
	if _, statErr := os.Stat(zonePath); statErr == nil {
		if prev, rerr := os.ReadFile(zonePath); rerr == nil {
			_ = os.WriteFile(zonePath+".bak", prev, 0640)
		}
	}
	if err := os.Rename(tmpPath, zonePath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	_, _ = exec.Command("chown", "named:named", zonePath).CombinedOutput()
	_, _ = exec.Command("restorecon", zonePath).CombinedOutput()

	if err := updateZoneIncludes(ctx, db); err != nil {
		return err
	}
	reloadNamed()
	return nil
}

// reloadNamed: zone'ları yeniden yükle. Önce rndc reload dener; bazı kurulumlarda
// rndc anahtarı yok/başarısız olur → o zaman systemctl reload.
// (Eskiden sadece "rndc reload" vardı; başarısızsa değişiklik etki etmiyordu ve
// operatör elle "systemctl restart named" yapmak zorunda kalıyordu.)
//
// 🔴 GÜVENLİK: son çare "systemctl restart named" named'i DURDURUP tekrar başlatır;
// config geçersizse named AYAĞA KALKMAZ → tüm DNS çöker (outage). checkconf-gate'li
// updateZoneIncludes bozuk include'un canlıya inmesini zaten engeller; yine de
// defense-in-depth olarak restart'a TIRMANMADAN ÖNCE named-checkconf ile doğrula.
func reloadNamed() {
	if err := exec.Command("rndc", "reload").Run(); err == nil {
		return
	}
	if err := exec.Command("systemctl", "reload", "named").Run(); err == nil {
		return
	}
	if err := exec.Command("named-checkconf").Run(); err != nil {
		log.Printf("dns reloadNamed: named-checkconf başarısız → 'systemctl restart named' ATLANDI (çalışan named korunuyor): %v", err)
		return
	}
	_ = exec.Command("systemctl", "restart", "named").Run()
}

// buildZoneIncludes: aktif kaydı olan tüm zone'lar için named include içeriğini üretir.
//   - allow-transfer { none; } HER ZAMAN → secondary yok, AXFR ile zone enumerasyonu engellenir.
//   - dnssec_aktif=1 ise BIND 9.18 gömülü "default" dnssec-policy (CSK/ECDSAP256SHA256,
//     inline-signing, CDS/CDNSKEY otomatik) + anahtar dizini eklenir.
func buildZoneIncludes(ctx context.Context, db *sql.DB) (string, error) {
	rows, err := db.QueryContext(ctx, `SELECT d.alan_adi, d.dnssec_aktif FROM domains d
	  WHERE EXISTS (SELECT 1 FROM dns_records r WHERE r.domain_id=d.id AND r.aktif=1)`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var sb strings.Builder
	sb.WriteString("// girginospanel — otomatik üretildi\n")
	for rows.Next() {
		var alanAdi string
		var dnssec int
		if serr := rows.Scan(&alanAdi, &dnssec); serr != nil {
			continue
		}
		fmt.Fprintf(&sb, `zone "%s" { type master; file "%s/%s.zone"; allow-query { any; }; allow-transfer { none; };`,
			alanAdi, ZoneDir, alanAdi)
		if dnssec == 1 {
			fmt.Fprintf(&sb, ` dnssec-policy default; key-directory "%s"; inline-signing yes;`, DNSSECKeyDir)
		}
		sb.WriteString(" };\n")
	}
	return sb.String(), rows.Err()
}

// updateZoneIncludes: include dosyasını CHECKCONF-GATE ile atomik günceller.
// Önce .tmp'ye yaz → named-checkconf ile doğrula → SADECE geçerliyse os.Rename ile
// canlının üzerine taşı + restorecon. Geçersizse .tmp silinir ve hata döner; canlı
// include'a HİÇ DOKUNULMAZ → bozuk bir zone-statement asla named'i düşüremez.
func updateZoneIncludes(ctx context.Context, db *sql.DB) error {
	content, err := buildZoneIncludes(ctx, db)
	if err != nil {
		return err
	}
	tmp := NamedConfInclude + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		return err
	}
	if out, cerr := exec.Command("named-checkconf", tmp).CombinedOutput(); cerr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("zone-include geçersiz (canlıya inmedi): %s", strings.TrimSpace(string(out)))
	}
	if err := os.Rename(tmp, NamedConfInclude); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	_, _ = exec.Command("restorecon", NamedConfInclude).CombinedOutput()
	return nil
}

// HealZoneIncludes: başlangıç iyileştirmesi (startup heal). Mevcut tüm zone'lara güncel
// include şablonunu (AXFR-kilit + varsa DNSSEC) checkconf-gate'li olarak uygular ve
// named'i yeniden yükler. Böylece kural yalnız bir sonraki DNS düzenlemesinde değil,
// server açılışında da tüm eski zone'lara işler.
func HealZoneIncludes(ctx context.Context, db *sql.DB) error {
	if err := updateZoneIncludes(ctx, db); err != nil {
		return err
	}
	reloadNamed()
	return nil
}

func DeleteZone(ctx context.Context, db *sql.DB, alanAdi string) error {
	_ = os.Remove(filepath.Join(ZoneDir, alanAdi+".zone"))
	_ = updateZoneIncludes(ctx, db)
	reloadNamed()
	return nil
}
