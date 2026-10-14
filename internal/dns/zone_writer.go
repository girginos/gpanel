package dns

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"girginospanel/internal/nginxset"
)

const (
	ZoneDir          = "/var/named"
	NamedConfInclude = "/etc/named/girginospanel-zones.conf"
	// DNSSECKeyDir: dnssec-policy anahtar dizini. /var/named/dynamic zaten named_cache_t
	// SELinux etiketli ve named'e yazılabilir (ekstra SELinux ayarı gerektirmez).
	DNSSECKeyDir = "/var/named/dynamic"
)

// zoneDizini / includeYolu — zone ve include dosyalarının yazıldığı yerler. Üretimde
// ZoneDir / NamedConfInclude'dır; testler geçici dizine çevirir (gerçek /var/named'e
// ve named'e dokunmadan yazım/geri alma yolları sınanır).
var (
	zoneDizini  = ZoneDir
	includeYolu = NamedConfInclude
)

// komutCalistir — zone araçlarını (named-checkzone / named-checkconf / rndc /
// systemctl / chown / restorecon) çalıştıran TEK dikiş. Çıktı birleşik (stdout+stderr).
// Testler sahteyle değiştirir.
var komutCalistir = func(ad string, arg ...string) ([]byte, error) {
	// Zaman aşımı: zone yazımı domainKilidi ve süreç geneli includeKilidi altında koşar;
	// takılan bir rndc/systemctl/named-checkconf o domainin (include için TÜM domainlerin)
	// DNS yazımlarını süresiz bekletirdi.
	ctx, iptal := context.WithTimeout(context.Background(), komutZamanAsimi)
	defer iptal()
	out, err := exec.CommandContext(ctx, ad, arg...).CombinedOutput()
	if ctx.Err() != nil {
		return out, fmt.Errorf("%s %v sürede bitmedi: %w", ad, komutZamanAsimi, ctx.Err())
	}
	return out, err
}

// komutZamanAsimi — zone aracı başına üst süre (named yeniden başlatma dahil).
var komutZamanAsimi = 2 * time.Minute

// sorgucu — *sql.DB ve *sql.Tx'in ortak okuma/yazma yüzü. Zone üretimi bir işlemin
// İÇİNDEN (henüz commit edilmemiş değişiklikleri görerek) de çalışabilsin diye.
type sorgucu interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// ErrZoneMotoruYok — named-checkzone çalıştırılamadı (BIND kurulu değil). DNS kayıtları
// yalnız DB'de tutulur; işlem istemcilere hata olarak YANSITILMAZ (bkz. zoneUygulaGercek).
var ErrZoneMotoruYok = errors.New("zone motoru (named-checkzone) bulunamadı — BIND kurulu değil")

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
	// İçteki tırnak kaçışla yayınlanır (DB'deki metinle aynı). Ters bölü içeren eski
	// değerlerde kaçış zinciri bozulmasın diye eski davranış sürer: tırnak silinir.
	if strings.Contains(t, `\`) {
		t = strings.ReplaceAll(t, "\"", "")
	} else {
		t = strings.ReplaceAll(t, "\"", `\"`)
	}
	if len(t) <= 255 {
		return "\"" + t + "\""
	}
	var b strings.Builder
	for len(t) > 255 {
		kes := txtKesim(t, 255)
		b.WriteString("\"" + t[:kes] + "\" ")
		t = t[kes:]
	}
	b.WriteString("\"" + t + "\"")
	return b.String()
}

// txtKesim — en çok sinir baytlık ilk parçanın sonu: '\X' ve '\DDD' kaçışını ortadan
// bölmez (parça sonunda kalan tek '\' kapanış tırnağını kaçırıp zone'u bozardı;
// bölünmüş '\DDD' başka bayta çözülürdü).
func txtKesim(t string, sinir int) int {
	i := 0
	for i < len(t) {
		n := 1
		if t[i] == '\\' {
			n = 2
			if i+3 < len(t) && rakamMi(t[i+1]) && rakamMi(t[i+2]) && rakamMi(t[i+3]) {
				n = 4
			}
		}
		if i+n > len(t) {
			n = len(t) - i
		}
		if i+n > sinir {
			break
		}
		i += n
	}
	return i
}

func rakamMi(c byte) bool { return c >= '0' && c <= '9' }

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

// soaMail: hostmaster e-postasını SOA RNAME biçimine çevirir ('@' -> '.', sona nokta eklenir).
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

// zoneGecersizHatasi — named-checkzone YALNIZ bu alan adının zone dosyasını
// sınar; çıktısı kiracının KENDİ kayıtlarını anlatır ve istemciye aynen
// gösterilir (kaydını düzeltebilsin). Metin öncekiyle birebir aynı; kayıt
// eşlemesi bulunabildiyse sona "sorunlu kayıt" eklenir (DB'de ZATEN bozuk kayıt
// varsa hangisi olduğu söylenir).
type zoneGecersizHatasi struct {
	alan, cikti string
	kayitlar    []string // named-checkzone'un satır numarasından eşlenen kayıtlar
	ipucu       string
}

func (e *zoneGecersizHatasi) Error() string {
	s := fmt.Sprintf("zone geçersiz (%s): %s", e.alan, e.cikti)
	if len(e.kayitlar) > 0 {
		s += " — sorunlu kayıt: " + strings.Join(e.kayitlar, "; ")
	}
	if e.ipucu != "" {
		s += " — " + e.ipucu
	}
	return s
}

// tmpSatirRe — named-checkzone çıktısındaki "<zone>.tmp:<satır>:" konumları.
var tmpSatirRe = regexp.MustCompile(`\.tmp:(\d+):`)

// zoneGecersizOlustur — named-checkzone hatasını kayıtlarla eşler. Zone şablonunda
// başlık sabit sayıda satırdır, ardından her kayıt TEK satırdır; bu yüzden hata
// satırı → kayıt dizini hesaplanabilir. Satır sayısı tutmuyorsa (kayıt değeri satır
// sonu taşıyor) eşleme yapılmaz.
func zoneGecersizOlustur(alan, cikti, render string, baslikSatir int, kayitlar []Kayit) *zoneGecersizHatasi {
	e := &zoneGecersizHatasi{alan: alan, cikti: strings.TrimSpace(cikti)}
	if strings.Contains(e.cikti, "has no NS records") {
		e.ipucu = "bölgede en az bir etkin NS kaydı bulunmalı"
	}
	if strings.Count(render, "\n") != baslikSatir+len(kayitlar) {
		return e
	}
	goruldu := map[int]bool{}
	for _, m := range tmpSatirRe.FindAllStringSubmatch(e.cikti, -1) {
		n, err := strconv.Atoi(m[1])
		idx := n - baslikSatir - 1
		if err != nil || idx < 0 || idx >= len(kayitlar) || goruldu[idx] {
			continue
		}
		goruldu[idx] = true
		k := kayitlar[idx]
		deger := k.Deger
		if len(deger) > 60 {
			deger = deger[:60] + "…"
		}
		e.kayitlar = append(e.kayitlar, fmt.Sprintf("#%d %s %s %s", k.ID, k.Ad, k.Tip, deger))
		if len(e.kayitlar) >= 5 {
			break
		}
	}
	return e
}

// zoneHatasi — WriteZone hatasının istemciye dönen metni.
//
// 🔴 updateZoneIncludes SUNUCUDAKİ TÜM zone'ları tek include dosyasında
// named-checkconf ile sınar; çıktısı komşu kiracıların zone adlarını ve
// /var/named/<başka-alan>.zone yollarını taşır. Bu metin DNSSEC ve zone içe
// aktarma uçlarından (MusteriScope) bayiye/müşteriye ham dönüyordu. Artık
// yalnız kendi zone'unun named-checkzone hatası herkese aynen döner; öteki
// hatalar yönetici olmayana genel mesaj, ayrıntı günlüğe (nginxset.SunucuHatasi).
// Yöneticinin gördüğü metin değişmedi.
func zoneHatasi(r *http.Request, domainID int64, err error) string {
	var zg *zoneGecersizHatasi
	if errors.As(err, &zg) {
		return err.Error()
	}
	return nginxset.SunucuHatasi(r, "dns.zone", domainID, err)
}

// WriteZone — domain'in zone dosyasını DB'den yazar ve named'i yeniden yükler.
// Dosya düzeyinde ATOMİKTİR (bkz. zoneDosyaHazirla): hata dönerse zone ve include
// dosyaları önceki geçerli hâlindedir. DB değişikliğiyle birlikte geri alma isteyen
// DNS uçları zoneliIslem kullanır (zone_islem.go).
func WriteZone(ctx context.Context, db *sql.DB, domainID int64) error {
	// Aynı domainin zoneliIslem yazımlarıyla seri: zone .tmp dosyası ve geri alma anlığı
	// domain başınadır (bkz. domainKilidi).
	k := domainKilidi(domainID)
	k.Lock()
	defer k.Unlock()
	g, err := zoneHazirla(ctx, db, domainID)
	if err != nil {
		return err
	}
	if g.Degisti() {
		reloadNamed()
	}
	return nil
}

// zoneHazirla — WriteZone'un reload'suz hâli; kayıtları q üzerinden (işlem içindeki
// commit edilmemiş değişiklikler dahil) okur. Dönen zoneGeri dosyaları önceki
// hâline döndürebilir; hata dönerse dosyalar zaten önceki hâldedir.
func zoneHazirla(ctx context.Context, q sorgucu, domainID int64) (*zoneGeri, error) {
	var alanAdi string
	if err := q.QueryRowContext(ctx, `SELECT alan_adi FROM domains WHERE id=?`, domainID).Scan(&alanAdi); err != nil {
		return nil, err
	}
	// 🔴 GUVENLIK (path traversal): alan_adi eklemede alanAdiRe'den gecer ama
	// burada DB'den geri okunur. "/" veya ".." iceren bir deger zone yolunu
	// ZoneDir disina tasir (or. ../../etc/x.zone yazimi + .bak/.tmp yan yollari).
	// Yaprak ad ayirici iceremez — savunma derinligi olarak reddet.
	if strings.ContainsAny(alanAdi, "/\\") || strings.Contains(alanAdi, "..") {
		return nil, fmt.Errorf("geçersiz alan adı: %q", alanAdi)
	}
	rows, err := q.QueryContext(ctx,
		`SELECT id, domain_id, ad, tip, deger, ttl, oncelik, aktif,
		   DATE_FORMAT(created_at,'%Y-%m-%d %H:%i') FROM dns_records
		 WHERE domain_id=? AND aktif=1 ORDER BY tip, ad`, domainID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	kayitlar := make([]Kayit, 0)
	for rows.Next() {
		k, err := scan(rows)
		if err == nil {
			kayitlar = append(kayitlar, k)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close() // aynı bağlantıda (işlem) sıradaki sorgudan ÖNCE kapat
	if len(kayitlar) == 0 {
		return &zoneGeri{}, nil
	}

	soa := LoadSOA(ctx, q, domainID, alanAdi)
	return zoneDosyaHazirla(ctx, q, alanAdi, soa, kayitlar)
}

// zoneDosyaYaz: zone dosyasını geçici dosyaya yazar + named-checkzone ile doğrular,
// geçerliyse canlının üzerine taşır, include'u günceller ve named'i yeniden yükler.
// Ana alan adı (WriteZone) ve ek alan adı (WriteEkZone) bölgeleri AYNI yolu kullanır.
func zoneDosyaYaz(ctx context.Context, db *sql.DB, alanAdi string, soa SOA, kayitlar []Kayit) error {
	if _, err := zoneDosyaHazirla(ctx, db, alanAdi, soa, kayitlar); err != nil {
		return err
	}
	reloadNamed()
	return nil
}

// dosyaAnlik — bir dosyanın değişiklikten ÖNCEKİ hâli (yoksa vardi=false).
type dosyaAnlik struct {
	yol    string
	sahip  string // chown hedefi ("" = dokunma)
	mod    os.FileMode
	icerik []byte
	vardi  bool
}

func anlikAl(yol, sahip string, mod os.FileMode) dosyaAnlik {
	b, err := os.ReadFile(yol)
	return dosyaAnlik{yol: yol, sahip: sahip, mod: mod, icerik: b, vardi: err == nil}
}

// geriYukle — dosyayı anlıktaki hâline döndürür (önceden yoksa siler). Yazım normal
// akıştaki gibidir: geçici dosya + rename, ardından sahip + SELinux etiketi.
func (a dosyaAnlik) geriYukle() error {
	if !a.vardi {
		if err := os.Remove(a.yol); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	tmp := a.yol + ".geri"
	if err := os.WriteFile(tmp, a.icerik, a.mod); err != nil {
		return err
	}
	if err := os.Rename(tmp, a.yol); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if a.sahip != "" {
		_, _ = komutCalistir("chown", a.sahip, a.yol)
	}
	_, _ = komutCalistir("restorecon", a.yol)
	return nil
}

// zoneGeri — zone yazımının dosya düzeyindeki geri alma kolu. Değişiklik yapılmadıysa
// (kayıt yok) Degisti() false'dur.
type zoneGeri struct {
	anliklar []dosyaAnlik
	degisti  bool
}

func (g *zoneGeri) Degisti() bool { return g != nil && g.degisti }

// Geri — zone dosyasını yazımdan ÖNCEKİ hâline döndürür. Include anlıktan geri
// YAZILMAZ (tüm domainlerce paylaşılır); bkz. includeTazele.
func (g *zoneGeri) Geri() {
	if !g.Degisti() {
		return
	}
	for i := len(g.anliklar) - 1; i >= 0; i-- {
		if err := g.anliklar[i].geriYukle(); err != nil {
			log.Printf("dns: %s önceki hâline döndürülemedi: %v", g.anliklar[i].yol, err)
		}
	}
	g.degisti = false
}

// zoneDosyaHazirla — zone dosyasını yazar ve include'u günceller; named'i YENİDEN
// YÜKLEMEZ (çağıran karar verir). Hata dönerse dosyalar önceki hâlindedir.
func zoneDosyaHazirla(ctx context.Context, q sorgucu, alanAdi string, soa SOA, kayitlar []Kayit) (*zoneGeri, error) {
	_ = os.MkdirAll(zoneDizini, 0750)
	zonePath := filepath.Join(zoneDizini, alanAdi+".zone")

	// serial: MONOTON ARTAN (mevcut serial'i baz alır). Aynı dakikada 2 düzenlemede bile
	// ilerler → BIND değişikliği görür, DNSSEC yeniden-imzalama tetiklenir.
	serial := strconv.FormatUint(uint64(nextSerial(readZoneSerial(zonePath))), 10)

	var buf bytes.Buffer
	if err := zoneTmpl.Execute(&buf, zoneCtx{AlanAdi: alanAdi, Serial: serial, SOA: soa, Kayitlar: kayitlar}); err != nil {
		return nil, err
	}
	var baslik bytes.Buffer
	_ = zoneTmpl.Execute(&baslik, zoneCtx{AlanAdi: alanAdi, Serial: serial, SOA: soa})

	// Önce GEÇİCİ dosyaya yaz + named-checkzone ile doğrula; SADECE geçerliyse
	// canlı zone dosyasının üzerine taşı. Böylece hatalı bir kayıt (ör. A kaydına
	// yanlışlıkla öncelik) asla çalışan zone'u bozmaz ve SSH'tan elle düzeltme gerektirmez.
	tmpPath := zonePath + ".tmp"
	if err := os.WriteFile(tmpPath, buf.Bytes(), 0640); err != nil {
		return nil, err
	}
	if out, err := komutCalistir("named-checkzone", alanAdi, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		if errors.Is(err, exec.ErrNotFound) {
			return nil, ErrZoneMotoruYok
		}
		return nil, zoneGecersizOlustur(alanAdi, string(out), buf.String(), strings.Count(baslik.String(), "\n"), kayitlar)
	}
	// Geri alma anlığı: canlı dosyaya dokunmadan HEMEN önce. Include'un anlığı ALINMAZ:
	// include tüm domainlerce paylaşılır; anlıktan geri yazmak arada başka bir domainin
	// yaptığı değişikliği silerdi. Geri almadan sonra include commit edilmiş DB'den
	// yeniden üretilir (includeTazele).
	g := &zoneGeri{degisti: true, anliklar: []dosyaAnlik{
		anlikAl(zonePath, "named:named", 0640),
	}}
	// .bak yedeği: atomik-rename mevcut dosyanın üzerine yazar; taşımadan ÖNCE bir kopya
	// bırak ki hatalı bir düzenlemeden geri dönüş (veya denetim) mümkün olsun.
	if a := g.anliklar[0]; a.vardi {
		_ = os.WriteFile(zonePath+".bak", a.icerik, 0640)
	}
	if err := os.Rename(tmpPath, zonePath); err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}
	_, _ = komutCalistir("chown", "named:named", zonePath)
	_, _ = komutCalistir("restorecon", zonePath)

	if err := updateZoneIncludes(ctx, q); err != nil {
		g.Geri()
		return nil, err
	}
	return g, nil
}

// zoneUygula — zoneliIslem'in kullandığı zone yazımı (dikiş: testler sahteyle değiştirir).
// q işlemdir; dönen geri() zone dosyasını önceki hâline döndürür (commit başarısız
// olursa çağrılır). tazele true ise zone/include diske yazılmıştır: çağıran commit ya
// da geri alma SONRASI includeTazele ile include'u commit edilmiş DB'den uzlaştırır
// (geri alma yolunda named'i de yeniden yükler). Hata dönerse zone dosyası zaten
// önceki hâldedir.
var zoneUygula = zoneUygulaGercek

func zoneUygulaGercek(ctx context.Context, q sorgucu, domainID int64) (geri func(), tazele bool, err error) {
	g, err := zoneHazirla(ctx, q, domainID)
	if err != nil {
		if errors.Is(err, ErrZoneMotoruYok) {
			// BIND yok: kayıtlar yalnız DB'de tutulur (eski davranış); hata değildir.
			log.Printf("dns: %v (domain=%d) — zone yazımı atlandı", err, domainID)
			return func() {}, false, nil
		}
		return nil, false, err
	}
	if !g.Degisti() {
		return func() {}, false, nil
	}
	if err := reloadNamedErr(); err != nil {
		g.Geri()
		// Include işlemin görünümüyle yazıldı: çağıran geri aldıktan sonra commit edilmiş
		// durumla uzlaştırıp önceki (geçerli) hâli yükler (includeTazele).
		return nil, true, fmt.Errorf("named yeniden yüklenemedi: %w", err)
	}
	return g.Geri, true, nil
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
func reloadNamed() { _ = reloadNamedErr() }

// reloadNamedErr — reloadNamed'in hatayı DÖNEN hâli: üç yolun da başarısızlığı hata sayılır.
func reloadNamedErr() error {
	if _, err := komutCalistir("rndc", "reload"); err == nil {
		return nil
	}
	if _, err := komutCalistir("systemctl", "reload", "named"); err == nil {
		return nil
	}
	if _, err := komutCalistir("named-checkconf"); err != nil {
		log.Printf("dns reloadNamed: named-checkconf başarısız → 'systemctl restart named' ATLANDI (çalışan named korunuyor): %v", err)
		return fmt.Errorf("rndc/systemctl reload başarısız ve named-checkconf geçersiz: %w", err)
	}
	if _, err := komutCalistir("systemctl", "restart", "named"); err != nil {
		return fmt.Errorf("named yeniden başlatılamadı: %w", err)
	}
	return nil
}

// buildZoneIncludes: aktif kaydı olan tüm zone'lar için named include içeriğini üretir.
//   - allow-transfer { none; } HER ZAMAN → secondary yok, AXFR ile zone enumerasyonu engellenir.
//   - dnssec_aktif=1 ise BIND 9.18 gömülü "default" dnssec-policy (CSK/ECDSAP256SHA256,
//     inline-signing, CDS/CDNSKEY otomatik) + anahtar dizini eklenir.
func buildZoneIncludes(ctx context.Context, q sorgucu) (string, error) {
	rows, err := q.QueryContext(ctx, `SELECT d.alan_adi, d.dnssec_aktif FROM domains d
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
	if err := rows.Err(); err != nil {
		return "", err
	}
	rows.Close() // ek bölge sorgusundan ÖNCE (aynı bağlantı olabilir: işlem)
	// Ek (addon) alan adı bölgeleri (0160): DNSSEC yok, AXFR kilidi aynı.
	ekler, err := ekZoneIncludeAdlari(ctx, q)
	if err != nil {
		return "", err
	}
	for _, alanAdi := range ekler {
		fmt.Fprintf(&sb, `zone "%s" { type master; file "%s/%s.zone"; allow-query { any; }; allow-transfer { none; }; };`+"\n",
			alanAdi, ZoneDir, alanAdi)
	}
	return sb.String(), nil
}

// updateZoneIncludes: include dosyasını CHECKCONF-GATE ile atomik günceller.
// Önce .tmp'ye yaz → named-checkconf ile doğrula → SADECE geçerliyse os.Rename ile
// canlının üzerine taşı + restorecon. Geçersizse .tmp silinir ve hata döner; canlı
// include'a HİÇ DOKUNULMAZ → bozuk bir zone-statement asla named'i düşüremez.
func updateZoneIncludes(ctx context.Context, q sorgucu) error {
	includeKilidi.Lock()
	defer includeKilidi.Unlock()
	content, err := buildZoneIncludes(ctx, q)
	if err != nil {
		return err
	}
	return includeYaz(content)
}

// includeKilidi — include'un üretimi ve yazımı süreç genelinde seri: include tüm
// domainlerce paylaşılır (domainKilidi yetmez; ortak geçici dosya ezilirdi).
var includeKilidi sync.Mutex

// includeYaz — includeKilidi altında çağrılır: benzersiz geçici dosya → named-checkconf
// → rename. Geçersizse geçici dosya silinir, canlı include'a dokunulmaz.
func includeYaz(content string) error {
	f, err := os.CreateTemp(filepath.Dir(includeYolu), filepath.Base(includeYolu)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.WriteString(content)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, 0644)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if out, cerr := komutCalistir("named-checkconf", tmp); cerr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("zone-include geçersiz (canlıya inmedi): %s", strings.TrimSpace(string(out)))
	}
	if err := os.Rename(tmp, includeYolu); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	_, _ = komutCalistir("restorecon", includeYolu)
	return nil
}

// includeTazele — include'u COMMIT EDİLMİŞ DB durumundan uzlaştırır (içerik farklıysa
// yazar) ve gerekirse named'i yeniden yükler. zoneliIslem include'u işlemin henüz commit
// edilmemiş görünümünden yazar: eşzamanlı başka bir domainin işlemi include'u kendi
// (bu işlemi görmeyen) görünümüyle üzerine yazmış olabilir (son yazan kazanır) ya da
// işlem geri alınmış olabilir. Commit ya da geri alma SONRASI çağrılır; yukle=true
// (geri alma: zone dosyası önceki hâline döndü) named'i her durumda yeniden yükler.
func includeTazele(ctx context.Context, db *sql.DB, yukle bool) {
	ctx, iptal := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	defer iptal()
	degisti, err := includeUzlastir(ctx, db)
	if err != nil {
		log.Printf("dns: include commit edilmiş durumla uzlaştırılamadı: %v", err)
	}
	if yukle || degisti {
		reloadNamed()
	}
}

// includeUzlastir — includeTazele'nin dosya kolu: içerik aynıysa dokunmaz (false).
func includeUzlastir(ctx context.Context, db *sql.DB) (bool, error) {
	includeKilidi.Lock()
	defer includeKilidi.Unlock()
	content, err := buildZoneIncludes(ctx, db)
	if err != nil {
		return false, err
	}
	if mevcut, err := os.ReadFile(includeYolu); err == nil && string(mevcut) == content {
		return false, nil
	}
	if err := includeYaz(content); err != nil {
		return false, err
	}
	return true, nil
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
	// 🔴 GUVENLIK (path traversal): alanAdi caller'in DB Scan'inden gelir —
	// WriteZone'daki kapinin aynisi (savunma derinligi, DB-tamper senaryosu).
	// Regex ekleme yolunda "/" zaten giremez; bu kapi bozuk DB satirinin
	// ".zone" son ekiyle keyfi dosya sildirmesini keser.
	if strings.ContainsAny(alanAdi, "/\\") || strings.Contains(alanAdi, "..") {
		return fmt.Errorf("geçersiz alan adı: %q", alanAdi)
	}
	_ = os.Remove(filepath.Join(zoneDizini, alanAdi+".zone"))
	_ = updateZoneIncludes(ctx, db)
	reloadNamed()
	return nil
}
