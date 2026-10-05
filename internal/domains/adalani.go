package domains

// adalani.go — alan adı AD-ALANI (namespace) doğrulayıcısı. Sunucuda alan adı
// (hosting) açan HER yol bunu kullanır: POST /domains (Create) ve site taşıma
// (tasima.HesapAktar). Başka bir yol eklenirse o da AdAlaniEkle üzerinden geçmeli.
//
// NEDEN (GL-02 / YK-01): aynı BIND'de en özgül zone kazanır, nginx'te tam
// server_name regex'i (paylaşımlı webmail vhost'u) yener, günlük dosyaları ada göre
// adlanır (/var/log/nginx/<ad>.access.log). Başka bir kiracının adının ALTINDA ya da
// ÜSTÜNDE açılan bir hosting, o adın DNS yetkisini, HTTP trafiğini, sertifikasını,
// yerel posta teslimini ve günlüklerini ele geçirir. Eskiden yalnız birebir
// `domains.alan_adi` eşleşmesine bakılıyordu.
//
// KURALLAR (ad = yeni alan adı; kiraci = OLUŞACAK domainin reseller_id'si, 0 = kök):
//  1. Panelin kendi adları (_panel.conf server_name + FQDN makine adı): ad bunlardan
//     biri ya da altındaysa → HERKES için red. Bayi ayrıca bunların ÜST adını açamaz.
//  2. subdomanlar.tam_ad == ad → herkes için red (aynı ad iki vhost/günlük; ters yön
//     subdomain.Olustur'da zaten reddediliyor).
//  3. domains:
//     a. ad birebir kayıtlı → red (UNIQUE ile de korunur).
//     b. ad, sahibi kiraci OLMAYAN bir domainin alt adı ya da üst adı → red.
//     c. ad = <hizmet öneki>.<mevcut domain> (www/mail/webmail/smtp/imap/pop/
//     autoconfig/autodiscover — domain vhost'u ve paylaşımlı webmail vhost'u bu
//     adları zaten karşılar) → sahibi kim olursa olsun red; www ikizi iki yönde.
//
// AYNI kiracının kendi alan adının (başka) alt adıyla ayrı hosting açması SERBEST:
// taşıma cPanel/Plesk alt alanlarını ayrı domain olarak getirir (ör. wp.<ana>) ve
// canlıda böyle bir kayıt var; reddetmek mevcut kullanımı bozardı. Bu durum başka
// kiracıya dokunmaz (iki ad da aynı sahibin).
//
// MESAJ: bayiye giden metin başka kiracının adını/sahibini İÇERMEZ (AdAlaniHatasi.
// Genel); ayrıntı yalnız yöneticiye (Mesaj(true)).

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"

	"girginospanel/internal/panelhost"
)

// AdAlaniHatasi: ad-alanı çakışması (HTTP 409).
type AdAlaniHatasi struct {
	Genel   string // bayiye gösterilebilir: başka kiracının adını/sahibini içermez
	Ayrinti string // yalnız yöneticiye: çakışan ad + sahibi
}

func (e *AdAlaniHatasi) Error() string {
	if e.Ayrinti == "" {
		return e.Genel
	}
	return e.Genel + " (" + e.Ayrinti + ")"
}

// Mesaj: yöneticiye ayrıntılı, diğerlerine genel metin.
func (e *AdAlaniHatasi) Mesaj(yonetici bool) string {
	if yonetici {
		return e.Error()
	}
	return e.Genel
}

const (
	adAlaniMsjKayitli = "bu alan adı zaten kayıtlı"
	adAlaniMsjCapraz  = "bu alan adı, sunucuda başka bir hesaba ait bir alan adıyla aynı ad alanında (alt ya da üst alan adı); sunucu yöneticinize başvurun"
	adAlaniMsjPanel   = "bu ad panel sunucusunun kendi adına ayrılmış; hosting olarak açılamaz"
	adAlaniMsjAltAlan = "bu ad sunucuda alt alan adı olarak kullanımda"
)

// adAlaniHizmetOnekleri: her domain için panelin kendiliğinden karşıladığı adlar
// (domain vhost'u www.<d>; paylaşımlı webmail vhost'u — provisioner.webmailRegexAd).
var adAlaniHizmetOnekleri = []string{"www", "mail", "webmail", "smtp", "imap", "pop", "autoconfig", "autodiscover"}

// adAlaniSorgucu: *sql.DB ve *sql.Tx.
type adAlaniSorgucu interface {
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
}

// panelAdlariAl: panelin kendi ana makine adları (testler değiştirir).
var panelAdlariAl = varsayilanPanelAdlari

func varsayilanPanelAdlari() []string {
	adlar := append([]string{}, panelhost.DurumOku().Izinliler...)
	if h, err := os.Hostname(); err == nil {
		adlar = append(adlar, h)
	}
	return adlar
}

// adAlaniNormal: karşılaştırma biçimi (küçük harf, boşluksuz, sondaki nokta yok).
func adAlaniNormal(s string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".")
}

// altAdMi: a, b'nin (kesin) alt adı mı — etiket sınırında.
func altAdMi(a, b string) bool {
	return b != "" && len(a) > len(b)+1 && strings.HasSuffix(a, "."+b)
}

// hizmetAdiMi: a == <hizmet öneki>.b mi.
func hizmetAdiMi(a, b string) bool {
	if !altAdMi(a, b) {
		return false
	}
	onek := a[:len(a)-len(b)-1]
	for _, o := range adAlaniHizmetOnekleri {
		if onek == o {
			return true
		}
	}
	return false
}

// ustAdlar: "a.b.c.com" → ["b.c.com", "c.com", "com"].
func ustAdlar(ad string) []string {
	var out []string
	for i := strings.IndexByte(ad, '.'); i >= 0; {
		ust := ad[i+1:]
		if ust == "" {
			break
		}
		out = append(out, ust)
		j := strings.IndexByte(ust, '.')
		if j < 0 {
			break
		}
		i += j + 1
	}
	return out
}

func sahipMetni(rid int64) string {
	if rid <= 0 {
		return "kök"
	}
	return fmt.Sprintf("bayi #%d", rid)
}

// panelAdlariDenetle: kural 1 (saf — DB yok).
func panelAdlariDenetle(ad string, kiraci int64, panelAdlari []string) *AdAlaniHatasi {
	for _, h := range panelAdlari {
		h = adAlaniNormal(h)
		// Yalnız gerçek ana makine adları: noktalı, IP değil (localhost/IP atlanır).
		if !strings.Contains(h, ".") || net.ParseIP(h) != nil {
			continue
		}
		if ad == h || altAdMi(ad, h) {
			return &AdAlaniHatasi{Genel: adAlaniMsjPanel, Ayrinti: "panel adı: " + h}
		}
		if kiraci > 0 && altAdMi(h, ad) {
			return &AdAlaniHatasi{Genel: adAlaniMsjPanel, Ayrinti: "panel adının üst adı: " + h}
		}
	}
	return nil
}

type adAlaniKayit struct {
	ad  string
	rid int64
}

// AdAlaniDenetle: ad, kiraci adına açılabilir mi? Çakışmada *AdAlaniHatasi, sorgu
// hatasında düz hata döner (çağıran reddetmeli — fail-closed). Yeni oluşturma
// dışında (mevcut domaine yazma) ÇAĞRILMAZ.
func AdAlaniDenetle(ctx context.Context, db adAlaniSorgucu, ad string, kiraci int64) error {
	return adAlaniDenetle(ctx, db, ad, kiraci, false)
}

// AdAlaniDenetleMarka: bayinin ÖZEL PANEL ADRESİ akışı (marka.AdAlaniKanca) için AdAlaniDenetle.
// Tek fark Kural 1b: bayinin KENDİ cp_marka_alanadi kaydı çakışma sayılmaz.
//
// 🔴 NEDEN: "Uygula" kaydı (bekleyen dahil) yazar; sonraki her denetim — "Tekrar dene" ve 30 sn'lik
// DNS yoklama döngüsü — o kaydı bulup "bayi panel adresi → herkese red" derdi. Bayi KENDİ adresini hiç
// ilerletemez, hata mesajı da "panel sunucusunun kendi adına ayrılmış" gibi yanıltıcı olurdu.
// Başka bayinin kaydı ve hosting yolları (AdAlaniDenetle) AYNEN reddeder.
func AdAlaniDenetleMarka(ctx context.Context, db adAlaniSorgucu, ad string, rid int64) error {
	return adAlaniDenetle(ctx, db, ad, rid, true)
}

func adAlaniDenetle(ctx context.Context, db adAlaniSorgucu, ad string, kiraci int64, kendiMarkaAtla bool) error {
	ad = adAlaniNormal(ad)
	if ad == "" {
		return &AdAlaniHatasi{Genel: "alan adı boş"}
	}
	if kiraci < 0 {
		kiraci = 0
	}
	if h := panelAdlariDenetle(ad, kiraci, panelAdlariAl()); h != nil {
		return h
	}

	// 3a + 3b(üst) + 3c(www/hizmet): ad'ın kendisi ve üst adları.
	adaylar := append([]string{ad}, ustAdlar(ad)...)
	yer := strings.TrimSuffix(strings.Repeat("?,", len(adaylar)), ",")
	args := make([]any, len(adaylar))
	for i, a := range adaylar {
		args[i] = a
	}
	ustler, err := adAlaniOku(ctx, db,
		`SELECT alan_adi, COALESCE(reseller_id,0) FROM domains WHERE alan_adi IN (`+yer+`)`, args...)
	if err != nil {
		return err
	}
	// 3b(alt) + www ikizi: ad'ın altındaki mevcut domainler.
	altlar, err := adAlaniOku(ctx, db,
		`SELECT alan_adi, COALESCE(reseller_id,0) FROM domains
		  WHERE CHAR_LENGTH(alan_adi) > ? AND RIGHT(alan_adi, ?) = ?`,
		len(ad)+1, len(ad)+1, "."+ad)
	if err != nil {
		return err
	}

	// Önce birebir, sonra BAŞKA KİRACI (genel mesaj), en son aynı kiracının kendi
	// adlarıyla çakışma (ayrıntılı mesaj bayinin yalnız KENDİ adını içerir).
	for _, k := range ustler {
		if k.ad == ad {
			return &AdAlaniHatasi{Genel: adAlaniMsjKayitli, Ayrinti: "sahibi: " + sahipMetni(k.rid)}
		}
	}
	for _, k := range ustler {
		if k.rid != kiraci {
			return &AdAlaniHatasi{Genel: adAlaniMsjCapraz,
				Ayrinti: fmt.Sprintf("%s alan adının alt adı; sahibi: %s", k.ad, sahipMetni(k.rid))}
		}
	}
	for _, k := range altlar {
		if k.rid != kiraci {
			return &AdAlaniHatasi{Genel: adAlaniMsjCapraz,
				Ayrinti: fmt.Sprintf("%s alan adının üst adı; sahibi: %s", k.ad, sahipMetni(k.rid))}
		}
	}
	for _, k := range ustler {
		if hizmetAdiMi(ad, k.ad) {
			return &AdAlaniHatasi{Genel: fmt.Sprintf(
				"bu ad, %s alan adının hizmet adı (www/mail/webmail/smtp/imap/pop/autoconfig/autodiscover); ayrı hosting olarak açılamaz", k.ad)}
		}
	}
	for _, k := range altlar {
		if k.ad == "www."+ad {
			return &AdAlaniHatasi{Genel: fmt.Sprintf(
				"%s ayrı bir hosting olarak kayıtlı; bu alan adının www adı onunla çakışır", k.ad)}
		}
	}

	// Kural 1b (D2 whitelabel): bayilerin özel panel adresleri (cp_marka_alanadi,
	// BEKLEYEN kayıtlar dahil — tek doğruluk kaynağı DB). Ad bir panel adresinin
	// kendisi ya da altıysa herkese red (panel vhost'u/ACME ele geçirilmesin);
	// üstüyse yalnız sahibi açabilir (bayi kendi bayi.com hostingini açar).
	if h, err := markaAdlariDenetle(ctx, db, ad, kiraci, kendiMarkaAtla); err != nil {
		return err
	} else if h != nil {
		return h
	}

	// Kural 2: alt alan adı (subdomanlar) — kimin olursa olsun aynı ad iki kez olmaz.
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subdomanlar WHERE tam_ad=?`, ad).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return &AdAlaniHatasi{Genel: adAlaniMsjAltAlan}
	}

	// Kural 4: ek (addon) alan adları (ek_alan_adlari, 0160). Ek alan adı kendi bölgesinin
	// ve vhost'unun sahibidir: domains/subdomanlar ile AYNI ad-alanı kuralları geçerlidir.
	return ekAlanAdlariDenetle(ctx, db, ad, kiraci)
}

const adAlaniMsjEk = "bu alan adı sunucuda ek alan adı olarak kullanımda"

// ekAlanAdlariDenetle: Kural 4. Tablo yoksa (0160 koşmamış) atlanır; öteki sorgu
// hataları fail-closed.
//   - ad birebir bir ek alan adı → herkese red (kimin olursa olsun).
//   - ad, SAHİBİ kiraci OLMAYAN bir ek alan adının alt adı ya da üst adı → red
//     (başka kiracının bölgesi/vhost'u/sertifikası ele geçirilemez).
//   - ad = <hizmet öneki>.<mevcut ek alan adı> → red.
func ekAlanAdlariDenetle(ctx context.Context, db adAlaniSorgucu, ad string, kiraci int64) error {
	adaylar := append([]string{ad}, ustAdlar(ad)...)
	yer := strings.TrimSuffix(strings.Repeat("?,", len(adaylar)), ",")
	args := make([]any, 0, len(adaylar)+3)
	for _, a := range adaylar {
		args = append(args, a)
	}
	ustler, err := adAlaniOku(ctx, db,
		`SELECT e.alan_adi, COALESCE(d.reseller_id,0) FROM ek_alan_adlari e JOIN domains d ON d.id=e.domain_id
		  WHERE e.alan_adi IN (`+yer+`)`, args...)
	if err != nil {
		if strings.Contains(err.Error(), "1146") || strings.Contains(strings.ToLower(err.Error()), "doesn't exist") {
			return nil
		}
		return err
	}
	altlar, err := adAlaniOku(ctx, db,
		`SELECT e.alan_adi, COALESCE(d.reseller_id,0) FROM ek_alan_adlari e JOIN domains d ON d.id=e.domain_id
		  WHERE CHAR_LENGTH(e.alan_adi) > ? AND RIGHT(e.alan_adi, ?) = ?`,
		len(ad)+1, len(ad)+1, "."+ad)
	if err != nil {
		return err
	}
	for _, k := range ustler {
		if k.ad == ad {
			return &AdAlaniHatasi{Genel: adAlaniMsjEk, Ayrinti: "sahibi: " + sahipMetni(k.rid)}
		}
	}
	for _, k := range ustler {
		if k.rid != kiraci {
			return &AdAlaniHatasi{Genel: adAlaniMsjCapraz,
				Ayrinti: fmt.Sprintf("%s ek alan adının alt adı; sahibi: %s", k.ad, sahipMetni(k.rid))}
		}
	}
	for _, k := range altlar {
		if k.rid != kiraci {
			return &AdAlaniHatasi{Genel: adAlaniMsjCapraz,
				Ayrinti: fmt.Sprintf("%s ek alan adının üst adı; sahibi: %s", k.ad, sahipMetni(k.rid))}
		}
	}
	for _, k := range ustler {
		if hizmetAdiMi(ad, k.ad) {
			return &AdAlaniHatasi{Genel: fmt.Sprintf(
				"bu ad, %s alan adının hizmet adı (www/mail/webmail/smtp/imap/pop/autoconfig/autodiscover); ayrı hosting olarak açılamaz", k.ad)}
		}
	}
	for _, k := range altlar {
		if k.ad == "www."+ad {
			return &AdAlaniHatasi{Genel: fmt.Sprintf(
				"%s ek alan adı olarak kayıtlı; bu alan adının www adı onunla çakışır", k.ad)}
		}
	}
	return nil
}

func adAlaniOku(ctx context.Context, db adAlaniSorgucu, q string, args ...any) ([]adAlaniKayit, error) {
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []adAlaniKayit
	for rows.Next() {
		var k adAlaniKayit
		if err := rows.Scan(&k.ad, &k.rid); err != nil {
			return nil, err
		}
		k.ad = adAlaniNormal(k.ad)
		out = append(out, k)
	}
	return out, rows.Err()
}

// adAlaniMu: denetim + INSERT'i tüm alan adı açan yollar için atomik yapar.
// Kiracı kilitleri (bayiKilidi) kiracı başınadır; iki FARKLI kiracının eşzamanlı
// "kurban.com" / "shop.kurban.com" açması ancak bu ortak kilitle sıralanır.
// Kilit altında başka kilit ALINMAZ (kilitlenme yok); yalnız kısa DB işi yapılır.
var adAlaniMu sync.Mutex

// AdAlaniEkle: kilit altında AdAlaniDenetle + ekle (domains INSERT). Denetim
// başarısızsa ekle ÇAĞRILMAZ ve denetim hatası döner.
func AdAlaniEkle(ctx context.Context, db adAlaniSorgucu, ad string, kiraci int64, ekle func() error) error {
	adAlaniMu.Lock()
	defer adAlaniMu.Unlock()
	if err := AdAlaniDenetle(ctx, db, ad, kiraci); err != nil {
		return err
	}
	return ekle()
}

// markaAdlariDenetle: Kural 1b. Tablo yoksa (0123 koşmamış) kural atlanır —
// alan adı açmayı bütünüyle kilitlemek orantısız olurdu; öteki sorgu hataları
// fail-closed.
func markaAdlariDenetle(ctx context.Context, db adAlaniSorgucu, ad string, kiraci int64, kendiAtla bool) (*AdAlaniHatasi, error) {
	adaylar := append([]string{ad}, ustAdlar(ad)...)
	yer := strings.TrimSuffix(strings.Repeat("?,", len(adaylar)), ",")
	args := make([]any, 0, len(adaylar)+3)
	for _, a := range adaylar {
		args = append(args, a)
	}
	args = append(args, len(ad)+1, len(ad)+1, "."+ad)
	kayitlar, err := adAlaniOku(ctx, db,
		`SELECT alan_adi, reseller_id FROM cp_marka_alanadi
		  WHERE alan_adi IN (`+yer+`) OR (CHAR_LENGTH(alan_adi) > ? AND RIGHT(alan_adi, ?) = ?)`, args...)
	if err != nil {
		if strings.Contains(err.Error(), "1146") || strings.Contains(strings.ToLower(err.Error()), "doesn't exist") {
			return nil, nil
		}
		return nil, err
	}
	for _, k := range kayitlar {
		if kendiAtla && k.rid == kiraci {
			continue // bayinin kendi özel adresi (bkz. AdAlaniDenetleMarka)
		}
		pa := adAlaniNormal(k.ad)
		if ad == pa || altAdMi(ad, pa) {
			return &AdAlaniHatasi{Genel: adAlaniMsjPanel, Ayrinti: "bayi panel adresi; sahibi: " + sahipMetni(k.rid)}, nil
		}
		if altAdMi(pa, ad) && k.rid != kiraci {
			return &AdAlaniHatasi{Genel: adAlaniMsjCapraz, Ayrinti: "bayi panel adresinin üst adı; sahibi: " + sahipMetni(k.rid)}, nil
		}
	}
	return nil, nil
}
