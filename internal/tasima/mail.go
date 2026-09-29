// Site taşımada POSTA (kutular + mail verisi) aktarımı — Plesk kaynaktan.
//
// Akış: kaynak Plesk'ten domainin gerçek kutuları keşfedilir (mail.postbox='true'),
// düz-metin parolalar mail_auth_view'dan alınır (kullanıcılar parolalarını KORUR),
// kutular gPanel mail eklentisine (yerel UNIX soket) oluşturulur, ardından maildir
// verisi rsync ile taşınır (/var/qmail/mailnames/<d>/<n>/Maildir/ → /var/vmail/<d>/<n>/).
//
// 🔴 Kaynaktan gelen HER ŞEY güvenilmezdir (kutu adları, parolalar, maildir içeriği):
//   - kutu adı ^[a-z0-9][a-z0-9._+-]{0,63}$ ve ".." yasak (mailYerelGecerli);
//   - hedef dizin filepath.Rel ile alan dizininin tek bileşenli çocuğu, var olan
//     alan/kutu dizini sembolik bağ değil (mailHedefDizin, Lstat);
//   - sieve betikleri ve dovecot iç dosyaları taşınmaz (mailRsyncHaric);
//   - eklenti çağrıları domain sahibinin kimliğiyle yapılır (mailKimlikSahibe);
//   - kutu açmadan plan limitleri eklentiye senkronlanır, kutu kotası plandan.
//
// 🔴 Mail eklentisi (paralı) kurulu+etkin değilse hata döner → adım "taşınamadı"
// uyarısı bırakır, site taşımanın geri kalanı ETKİLENMEZ.
package tasima

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/domains"
)

// mailEklentiSoket — core→eklenti güvenilir yerel kanal (aynı kutu, out-of-process).
// Değişken: testler sahte eklentiyi geçici bir sokete bağlar.
var mailEklentiSoket = "/run/girginospanel/eklenti-mail.sock"

// vmailKok — Dovecot mail_location = maildir:/var/vmail/%d/%n kökü (testler değiştirir).
var vmailKok = "/var/vmail"

// reMailYerel — içe aktarılan kutu adının (e-postanın @ öncesi) izinli biçimi. Ad
// hem yerel yola (/var/vmail/<alan>/<ad>) hem uzak rsync yoluna girer.
var reMailYerel = regexp.MustCompile(`^[a-z0-9][a-z0-9._+-]{0,63}$`)

// mailRsyncHaric — maildir aktarımında TAŞINMAYANLAR. Dovecot iç dosyaları
// (dovecot*, maildirsize) hedefte yeniden üretilir. 🔴 Sieve betikleri (.dovecot.sieve
// etkin betik, .dovecot.svbin derlenmişi, sieve betik dizini) kaynağın denetimindeki
// KURALLARDIR (yönlendirme, otomatik yanıt, sunucu eklentileriyle program çalıştırma):
// taşınsalar hedefte vmail kimliğiyle yürürlerdi. Kutunun sieve'ini eklenti kendi
// ayarlarından üretir.
var mailRsyncHaric = []string{
	"--exclude=dovecot*", "--exclude=maildirsize",
	"--exclude=.dovecot.sieve", "--exclude=.dovecot.svbin", "--exclude=sieve",
}

type mailHesap struct {
	Yerel  string // e-postanın @ öncesi (mail_name)
	Parola string // düz metin (mail_auth_view) — boşsa çağıran yeni üretir
}

// mailKimlik — eklentiye iletilen güvenilir kimlik başlıkları (eklenti.go Proxy
// deseni: X-Gosp-Rol / X-Gosp-Uid / X-Gosp-Sahip). Eklenti bayi rolünde her uçta
// hedef domainin X-Gosp-Sahip listesinde olmasını şart koşar; admin kısıtsızdır.
type mailKimlik struct {
	Rol   string // "admin" | "reseller"
	Uid   int64  // bayi: users.id
	Sahip string // bayi: boşlukla ayrılmış alan adları
}

// mailKimlikSahibe — domain bir bayiye aitse eklenti çağrıları BAYİ kimliğiyle ve
// kapsam YALNIZ bu alan adı olacak şekilde yapılır (en az yetki: sabit admin
// başlığı eklentinin sahiplik kilidini devre dışı bırakıyordu). Kökün ve kökün
// müşterilerinin domainleri admin (eklentide müşteri rolü kutu açamaz).
func mailKimlikSahibe(resellerID int64, alanAdi string) mailKimlik {
	if resellerID > 0 {
		return mailKimlik{Rol: "reseller", Uid: resellerID, Sahip: alanAdi}
	}
	return mailKimlik{Rol: "admin"}
}

func (m mailKimlik) basliklar(req *http.Request) {
	req.Header.Set("X-Gosp-Rol", m.Rol)
	if m.Uid > 0 {
		req.Header.Set("X-Gosp-Uid", strconv.FormatInt(m.Uid, 10))
	}
	if m.Sahip != "" {
		req.Header.Set("X-Gosp-Sahip", m.Sahip)
	}
}

// mailEklentiPOST — mail eklentisine yerel soket üzerinden JSON POST'lar.
func mailEklentiPOST(ctx context.Context, kim mailKimlik, yol string, govde any) (int, []byte, error) {
	b, _ := json.Marshal(govde)
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", mailEklentiSoket)
	}}
	cl := &http.Client{Transport: tr, Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "POST", "http://mail"+yol, bytes.NewReader(b))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	kim.basliklar(req)
	resp, err := cl.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	return resp.StatusCode, rb, nil
}

// mailEklentiGET — mail eklentisinden yerel soket uzerinden JSON GET okur.
func mailEklentiGET(ctx context.Context, kim mailKimlik, yol string) (int, []byte, error) {
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", mailEklentiSoket)
	}}
	cl := &http.Client{Transport: tr, Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", "http://mail"+yol, nil)
	if err != nil {
		return 0, nil, err
	}
	kim.basliklar(req)
	resp, err := cl.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, rb, nil
}

// eklentiHata — eklenti hata yanıtından ({"hata":"..."}) kısa, tek satırlık metin;
// ham gövde iletiye/günlüğe taşınmaz.
func eklentiHata(kod int, govde []byte) string {
	var y struct {
		Hata string `json:"hata"`
	}
	if json.Unmarshal(govde, &y) == nil && y.Hata != "" {
		return fmt.Sprintf("%d: %s", kod, kisalt(temizHata(y.Hata), 120))
	}
	return fmt.Sprintf("HTTP %d", kod)
}

// mailDomainID — domainin mail eklentisindeki kimliği (GET /domainler); yoksa 0.
// Yanit sekli: {"domainler":[{"id":N,"ad":"...","kutu_sayisi":N}]}.
func mailDomainID(ctx context.Context, kim mailKimlik, alanAdi string) (int, error) {
	kod, govde, err := mailEklentiGET(ctx, kim, "/domainler")
	if err != nil {
		return 0, err
	}
	if kod != http.StatusOK {
		return 0, fmt.Errorf("domain listesi: %s", eklentiHata(kod, govde))
	}
	var yanit struct {
		Domainler []struct {
			ID int    `json:"id"`
			Ad string `json:"ad"`
		} `json:"domainler"`
	}
	if json.Unmarshal(govde, &yanit) != nil {
		return 0, errors.New("domain listesi okunamadı")
	}
	for _, d := range yanit.Domainler {
		if strings.EqualFold(d.Ad, alanAdi) {
			return d.ID, nil
		}
	}
	return 0, nil
}

// mailKutulari — domainin eklentide ZATEN var olan kutuları (GET /hesaplar).
// 🔴 "Zaten var" kararı VARLIKTAN verilir, yanıt kodundan/metninden DEĞİL: eklenti
// kutu limiti dolunca da 409 döner (çift kayıtta 500) — eski 409/"zaten" eşleşmesi
// açılamayan kutuyu "var" sayıp hesabı olmayan maildir'e veri yazıyordu.
func mailKutulari(ctx context.Context, kim mailKimlik, mailDomainID int) (map[string]bool, error) {
	kod, govde, err := mailEklentiGET(ctx, kim, "/hesaplar?domain="+strconv.Itoa(mailDomainID))
	if err != nil {
		return nil, err
	}
	if kod != http.StatusOK {
		return nil, fmt.Errorf("kutu listesi: %s", eklentiHata(kod, govde))
	}
	var yanit struct {
		Hesaplar []struct {
			Email string `json:"email"`
		} `json:"hesaplar"`
	}
	if json.Unmarshal(govde, &yanit) != nil {
		return nil, errors.New("kutu listesi okunamadı")
	}
	m := make(map[string]bool, len(yanit.Hesaplar))
	for _, h := range yanit.Hesaplar {
		m[strings.ToLower(h.Email)] = true
	}
	return m, nil
}

// mailYerelGecerli — içe aktarılan kutu adı kabul edilebilir mi.
func mailYerelGecerli(s string) bool {
	return reMailYerel.MatchString(s) && !strings.Contains(s, "..")
}

// mailHedefDizin — kutunun yerel maildir'i (/var/vmail/<alan>/<kutu>) ve alan
// dizini. Ad kuralından BAĞIMSIZ ikinci kilit (her kaynak türü bu yoldan geçer):
// iki bileşen de tek ve kanonik olmalı; var olan alan/kutu dizini sembolik bağ ya
// da dizin-dışı bir öğe olamaz (Lstat) — yoksa root'un MkdirAll/rsync/chown'u bağı
// izleyip komşu kutuya ya da sistem yoluna yazardı.
func mailHedefDizin(alanAdi, yerel string) (domainDir, hedef string, err error) {
	if domainDir, err = dogrudanCocuk(vmailKok, alanAdi); err != nil {
		return "", "", err
	}
	if hedef, err = dogrudanCocuk(domainDir, yerel); err != nil {
		return "", "", err
	}
	for _, y := range []string{domainDir, hedef} {
		if err := dizinVeyaYok(y); err != nil {
			return "", "", err
		}
	}
	return domainDir, hedef, nil
}

// dogrudanCocuk — kok'un TEK bileşenli doğrudan çocuğu. filepath.Rel kanonik
// göreli yolu verir: girdiyle birebir aynı değilse ("a/../b", "b/", "./b", "") ya
// da tek bileşen değilse (".", "..", "a/b") ret.
func dogrudanCocuk(kok, ad string) (string, error) {
	yol := filepath.Join(kok, ad)
	rel, err := filepath.Rel(kok, yol)
	if err != nil || rel != ad || rel != filepath.Base(rel) || rel == "." || rel == ".." {
		return "", fmt.Errorf("güvensiz yol bileşeni %.64q", ad)
	}
	return yol, nil
}

// dizinVeyaYok — yol yoksa nil (MkdirAll oluşturur); varsa sembolik bağ OLMAYAN
// gerçek bir dizin olmalı (Lstat bağı izlemez).
func dizinVeyaYok(yol string) error {
	fi, err := os.Lstat(yol)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%q dizin değil ya da sembolik bağ", filepath.Base(yol))
	}
	return nil
}

// mailAktar — domainin kutularını + mail verisini kaynaktan taşır. Döndürür:
// taşınan kutu sayısı, uyarılar, hata (yalnız kesif/eklenti erişim hatasında).
func (h *Handlers) mailAktar(ctx context.Context, k *Kaynak, alanAdi string, log func(string, ...any)) (int, []string, error) {
	var uyarilar []string

	hesaplar, err := pleskMailKesfet(ctx, k, alanAdi)
	if err != nil {
		return 0, nil, fmt.Errorf("kaynak posta kesfi: %w", err)
	}
	if len(hesaplar) == 0 {
		return 0, []string{"Posta: kaynakta kutu yok — taşınmadı"}, nil
	}

	// Hedef domain satırı: gPanel kimliği (limit senkronu), sahibi (eklenti
	// kimliği) ve planın kutu başına kotası — kaynaktan/istekten DEĞİL satırdan.
	var domainID, resellerID int64
	var kotaMB int
	if err := h.DB.QueryRowContext(ctx,
		`SELECT d.id, d.reseller_id, COALESCE(p.mail_kutu_kota_mb,0)
		   FROM domains d LEFT JOIN service_plans p ON p.id=d.plan_id WHERE d.alan_adi=?`,
		alanAdi).Scan(&domainID, &resellerID, &kotaMB); err != nil {
		return 0, nil, fmt.Errorf("hedef domain okunamadı: %w", err)
	}
	kim := mailKimlikSahibe(resellerID, alanAdi)

	// Mail domainini gPanel'de GARANTI et — ZATEN VARSA dokunma (idempotent).
	// 🔴 Mail eklentisi domainEkle duplicate'te 409 DEGIL 500 doner; koda guvenme,
	// once VARLIGA bak (GET /domainler), yoksa ekle, sonra tekrar dogrula. Onceden
	// mevcut domain (ornek: islam-tr.org zaten kayitli) tum posta adimini "taşınamadı"
	// yapiyordu; keşif kutulari BULMUS olsa bile.
	mid, err := mailDomainID(ctx, kim, alanAdi)
	if err != nil {
		return 0, nil, fmt.Errorf("mail eklentisine ulaşılamadı (kurulu/etkin mi?): %w", err)
	}
	if mid == 0 {
		kod, govde, err := mailEklentiPOST(ctx, kim, "/domainler", map[string]string{"ad": alanAdi})
		if err != nil {
			return 0, nil, fmt.Errorf("mail eklentisine ulaşılamadı (kurulu/etkin mi?): %w", err)
		}
		if mid, err = mailDomainID(ctx, kim, alanAdi); err != nil || mid == 0 {
			return 0, nil, fmt.Errorf("mail domaini eklenemedi (%s)", eklentiHata(kod, govde))
		}
	}

	// 🔴 Kutu AÇMADAN plan limitleri eklentiye: kutu sayısı sınırı (max_email) ve
	// kutu başına kota eklentide uygulanır. Yeni mail domaininde limit yoktu →
	// kaynak kaç kutu verirse sınırsız kotayla açılıyordu.
	domains.MailKutuLimitSenkron(ctx, h.DB, domainID, alanAdi)

	mevcut, err := mailKutulari(ctx, kim, mid)
	if err != nil {
		return 0, nil, fmt.Errorf("mevcut posta kutuları okunamadı: %w", err)
	}

	sayi := 0
	for _, hs := range hesaplar {
		if !mailYerelGecerli(hs.Yerel) {
			uyarilar = append(uyarilar, fmt.Sprintf("Posta: geçersiz kutu adı atlandı: %.64q", hs.Yerel))
			continue
		}
		email := hs.Yerel + "@" + alanAdi
		domainDir, hedef, err := mailHedefDizin(alanAdi, hs.Yerel)
		if err != nil {
			log("Posta: %s atlandı: %v", email, err)
			uyarilar = append(uyarilar, "Posta: "+email+" hedef dizini güvenli değil — atlandı")
			continue
		}

		if !mevcut[email] {
			parola := hs.Parola
			if len(parola) < 6 {
				parola = rastgeleParola(16)
				uyarilar = append(uyarilar, "Posta: "+email+" kaynak parolası alınamadı — yeni parola üretildi")
			}
			// Kota PLANDAN (0 = sınırsız); kaynak belirlemez.
			kod, govde, err := mailEklentiPOST(ctx, kim, "/hesaplar", map[string]any{
				"email": email, "parola": parola, "quota_mb": kotaMB,
			})
			if err != nil {
				uyarilar = append(uyarilar, "Posta: "+email+" oluşturulamadı (bağlantı)")
				continue
			}
			if kod != http.StatusCreated {
				uyarilar = append(uyarilar, "Posta: "+email+" oluşturulamadı ("+eklentiHata(kod, govde)+")")
				continue
			}
		}

		// Maildir verisini taşı: Plesk (qmail düzeni) → gPanel (dovecot maildir).
		uzak := "/var/qmail/mailnames/" + alanAdi + "/" + hs.Yerel + "/Maildir/"
		if err := os.MkdirAll(hedef, 0o700); err != nil {
			uyarilar = append(uyarilar, "Posta: "+email+" hedef maildir hazırlanamadı")
		} else if _, err := k.RsyncCek(ctx, uzak, hedef+"/", mailRsyncHaric...); err != nil {
			uyarilar = append(uyarilar, "Posta: "+email+" mesajları kısmen/hiç taşınamadı")
		}
		// 🔴 Sahiplik: DOMAIN dizini + kutu dizini vmail:vmail. MkdirAll domain parent'ını
		// root:root 0700 yaratır → vmail(dovecot) TRAVERSE EDEMEZ → sieve+teslim "Permission
		// denied". Yalnız leaf'i chown YETMEZ; parent'ı da düzelt. Kutu yolu sonda "/"
		// OLMADAN verilir: -R (varsayılan -P) bağı izlemez, "/" ise çözdürürdü.
		_ = exec.CommandContext(ctx, "chown", "vmail:vmail", domainDir).Run()
		_ = exec.CommandContext(ctx, "chown", "-R", "vmail:vmail", hedef).Run()

		sayi++
		log("Posta: %s taşındı", email)
	}
	return sayi, uyarilar, nil
}

// pleskMailKesfet — kaynak Plesk'ten domainin GERÇEK kutularını + düz parolalarını alır.
func pleskMailKesfet(ctx context.Context, k *Kaynak, alanAdi string) ([]mailHesap, error) {
	// 🔴 Savunma katmanı: alanAdi çağıran zincirde (aktar.go, reAlanAdi) doğrulanır,
	// ama burada uzak kabuk komutuna VE SQL'e tek tırnak içinde gömülüyor. Tırnak/
	// metakarakter içeren bir değer kaynak sunucuda komut çalıştırırdı; kaynakCertOku
	// ile aynı yerel kontrol burada da uygulanır.
	if !reAlanAdi.MatchString(alanAdi) {
		return nil, fmt.Errorf("gecersiz alan adi: %q", alanAdi)
	}
	// postbox='true' = gerçek kutu (yalnız forward/alias olan kayıtlar hariç).
	q := `plesk db -Ne "SELECT m.mail_name FROM mail m JOIN domains d ON m.dom_id=d.id WHERE d.name='` +
		alanAdi + `' AND m.postbox='true'" 2>/dev/null`
	ham, err := k.Calistir(ctx, q)
	if err != nil {
		return nil, err
	}
	yereller := map[string]bool{}
	for _, satir := range strings.Split(ham, "\n") {
		if s := strings.ToLower(strings.TrimSpace(satir)); s != "" {
			yereller[s] = true
		}
	}
	if len(yereller) == 0 {
		return nil, nil
	}

	// mail_auth_view TÜM hesapların düz parolasını verir; bu domaine ait olanları süz.
	pw, _ := k.Calistir(ctx, "/usr/local/psa/admin/sbin/mail_auth_view 2>/dev/null")
	parolalar := parseMailAuthView(pw, alanAdi)

	var out []mailHesap
	for yerel := range yereller {
		out = append(out, mailHesap{Yerel: yerel, Parola: parolalar[yerel]})
	}
	return out, nil
}

// parseMailAuthView — mail_auth_view çıktısından <yerelkısım>→parola eşlemesi
// (yalnız verilen domain). Biçim sürümden sürüme değişir: '|' ayraçlı tablo VEYA
// boşluk ayraçlı — ikisi de denenir (email + parola sütunları).
func parseMailAuthView(cikti, alanAdi string) map[string]string {
	m := map[string]string{}
	sonek := "@" + strings.ToLower(alanAdi)
	for _, satir := range strings.Split(cikti, "\n") {
		if !strings.Contains(strings.ToLower(satir), sonek) {
			continue
		}
		var email, parola string
		if strings.Contains(satir, "|") {
			var alanlar []string
			for _, x := range strings.Split(satir, "|") {
				if x = strings.TrimSpace(x); x != "" {
					alanlar = append(alanlar, x)
				}
			}
			if len(alanlar) >= 2 {
				email, parola = alanlar[0], alanlar[1]
			}
		} else if f := strings.Fields(satir); len(f) >= 2 {
			email, parola = f[0], f[1]
		}
		email = strings.ToLower(strings.TrimSpace(email))
		if strings.HasSuffix(email, sonek) {
			m[strings.TrimSuffix(email, sonek)] = parola
		}
	}
	return m
}

// rastgeleParola — kaynak parolası alınamayan kutular için güvenli yedek.
func rastgeleParola(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "Gosp!" + hex.EncodeToString([]byte(time.Now().String()))[:12]
	}
	return "Gosp!" + hex.EncodeToString(b)[:n]
}
