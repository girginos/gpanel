package domains

// SSL kurulumu ASENKRON + adım-adım ilerleme.
//
// TASARIM: SSL çekimi (özellikle mail SSL — 7 ayrı SAN'ın her biri ayrı LE
// HTTP-01 doğrulaması) dakikayı bulabilir. Senkron istekte:
//   • tarayıcı zaman aşımına uğrar, kullanıcı hangi adımda olduğunu göremez,
//   • 🔴 sekme kapanınca istek düşer ve kurulum YARIDA KALIRDI.
// Çözüm: istekten BAĞIMSIZ bir goroutine (context.Background) işi sonuna kadar
// yürütür; her adım kaydedilir; UI /domains/{id}/ssl/ilerleme ucundan ~1.5sn'de
// bir çeker. Sayfa kapansa/yenilense bile kurulum sürer ve tekrar açıldığında
// ilerleme kaldığı yerden görünür. (Eklenti kurulumundaki desenin aynısı.)

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/dns"
	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
	"girginospanel/internal/nginxset"
	"girginospanel/internal/provisioner"
)

// Dış etkili adımlar (sertifika çekimi/üretimi + vhost render + SUNUCU GENELİ
// `nginx -t`; posta sunucusu, webmail vhost'u, DKIM). Testler sahteyle
// değiştirir: ilerleme kaydına ne yazıldığı ve kime ne gösterildiği ana
// makineye (acme.sh, nginx, posta eklentisi, named) dokunmadan sınanır.
//
// 🔴 Sunucunun VARSAYILAN (SNI'sız) posta sertifikası (provisioner.MailVarsayilan*,
// /etc/pki/mail/mail.crt+key) bu listede YOK ve bu paketten ÇAĞRILMAZ: o,
// SUNUCU GENELİ bir kimliktir (dovecot/postfix + paylaşımlı webmail vhost'u —
// bütün kiracılar). Bu paketteki SSL uçları müşteri/bayi kapsamlıdır
// (MusteriScope); kiracı yalnız KENDİ alanının SNI sertifikasını kurar.
// Varsayılanın tek kaynağı panel host sertifikasıdır (panelhost SSL akışı,
// AdminOnly + provisioner.MailVarsayilanNobetci). Yapısal regresyon testi:
// TestPostaSSL_KiraciYolu_SunucuVarsayilaninaBasvurmaz.
var (
	sslSelfSigned   = provisioner.EnableSelfSigned
	sslLetsEncrypt  = provisioner.EnableLetsEncrypt
	mailSertAl      = provisioner.MailSertifikaAl
	mailSertGonder  = provisioner.MailSertifikaGonder
	webmailVhostYaz = provisioner.WebmailVhostDomainYaz
	mailDKIMAl      = provisioner.MailDKIMTXTAl
	mailDKIMYayinla = dns.MailDKIMYayinla
)

// SSLAdim — tek SSL adımı.
type SSLAdim struct {
	Ad     string `json:"ad"`
	Etiket string `json:"etiket"`
	Durum  string `json:"durum"` // bekliyor|calisiyor|tamam|uyari|hata
	Mesaj  string `json:"mesaj"`
	Sure   string `json:"sure"`

	// genel — yönetici OLMAYAN okuyucuya Mesaj yerine gösterilen metin (bkz.
	// Goruntu). Ham araç çıktısı taşımaz.
	genel string
}

// sslIsi — bir domain için SSL kurulum oturumu.
type sslIsi struct {
	mu       sync.Mutex
	DomainID int64
	AlanAdi  string
	Durum    string // calisiyor|tamam|hata
	Hata     string
	Adimlar  []SSLAdim
	Basladi  time.Time
	Bitti    time.Time

	// Toplam — bu oturumun ÇALIŞTIRMAYI PLANLADIĞI adım sayısı.
	//
	// 🔴 Adımlar append-only üretilir (bkz. adim): gelen `Adimlar` listesi
	// "şimdiye kadar BAŞLAMIŞ adım"dır, "toplam" değil. Arayüz yüzdeyi
	// bitmis/len(Adimlar) ile hesaplasaydı kurulum boyunca hep ~%95-100
	// gösterirdi. Posta SSL'i açık olan kurulumda 7, yalnız web SSL'inde 2
	// adım koşar; sayı BAŞLARKEN bilinir (bkz. sslBaslat).
	//
	// Doğrulama düşerse sonraki 3 adım atlanır ve gerçek sayı Toplam'ın
	// altında kalır: çubuk %99'da durur, iş bitince arayüz 100'e çeker.
	// Eksik göstermek, olmayan bir ilerlemeyi uydurmaktan iyidir.
	Toplam int

	// hataGenel — yönetici olmayan okuyucuya Hata yerine gösterilen metin.
	hataGenel string
}

// SSLGoruntu — kilitsiz JSON kopyası.
type SSLGoruntu struct {
	DomainID int64     `json:"domain_id"`
	AlanAdi  string    `json:"alan_adi"`
	Durum    string    `json:"durum"`
	Hata     string    `json:"hata"`
	Adimlar  []SSLAdim `json:"adimlar"`
	Basladi  string    `json:"basladi"`
	Bitti    string    `json:"bitti"`
	Toplam   int       `json:"toplam"`
}

// Goruntu — oturumun okuyucuya dönecek kopyası.
//
// 🔴 Adım mesajları ve iş hatası HAM araç çıktısı taşır: vhost render → SUNUCU
// GENELİ `nginx -t` (komşu kiracıların /etc/pki/girginospanel/<alan>/…
// sertifika ve dom_<sk>.conf yolları), webmail vhost'u, posta eklentisi,
// named. Kayıt, işi BAŞLATANDAN bağımsız olarak
// alan adına erişen herkesçe okunur (yönetici başlatır → bayi/müşteri izler;
// bayi başlatır → yönetici izler). Bu yüzden süzme YAZARKEN başlatanın rolüne
// göre değil, OKURKEN okuyanın rolüne göre yapılır: yazarken hem ham hem genel
// metin saklanır, ham metin yalnız yöneticiye verilir (arıza ayıklama).
func (k *sslIsi) Goruntu(yonetici bool) SSLGoruntu {
	k.mu.Lock()
	defer k.mu.Unlock()
	a := make([]SSLAdim, len(k.Adimlar))
	copy(a, k.Adimlar)
	hata := k.Hata
	if !yonetici {
		for i := range a {
			a[i].Mesaj = a[i].genel
		}
		hata = k.hataGenel
	}
	g := SSLGoruntu{DomainID: k.DomainID, AlanAdi: k.AlanAdi, Durum: k.Durum, Hata: hata, Adimlar: a, Toplam: k.Toplam}
	if !k.Basladi.IsZero() {
		g.Basladi = k.Basladi.UTC().Format(time.RFC3339)
	}
	if !k.Bitti.IsZero() {
		g.Bitti = k.Bitti.UTC().Format(time.RFC3339)
	}
	return g
}

// sslUyari — HAM araç çıktısı taşıyan UYARI (iş durmaz). ham: öncekiyle
// birebir aynı metin (yönetici görür, günlüğe yazılır); genel: aynı önek, ham
// çıktının yerinde günlük notu (öteki okuyucular görür).
type sslUyari struct{ ham, genel string }

func (u *sslUyari) Error() string { return u.ham }

// sslUyariHam — "onek: <ham hata><ek>" uyarısı. ek: güvenli sonek (ör. kiracının
// kendi atlanan alt adları) — iki metinde de aynen kalır.
func sslUyariHam(onek string, e error, ek string) (string, bool, error) {
	return "", true, &sslUyari{
		ham:   onek + ": " + e.Error() + ek,
		genel: onek + " — ayrıntı sunucu günlüğüne yazıldı" + ek,
	}
}

// adim — adımı işler; fn (mesaj, uyari, err) döner. err → 'hata' + iş durur;
// uyari=true → 'uyari' + iş DEVAM eder (mail SSL web SSL'i bloklamaz).
//
// 🔴 Ham araç çıktısı taşıyan uyarı DÜZ mesajla dönmez: sslUyariHam ile döner
// (err = *sslUyari → uyarı; ham yalnız yöneticiye). Düz uyarı/tamam mesajı
// herkese gösterilir; hata adımının metni yalnız yöneticiye (bkz. Goruntu).
func (k *sslIsi) adim(ad, etiket string, fn func() (string, bool, error)) error {
	k.mu.Lock()
	k.Adimlar = append(k.Adimlar, SSLAdim{Ad: ad, Etiket: etiket, Durum: "calisiyor"})
	i := len(k.Adimlar) - 1
	k.mu.Unlock()
	log.Printf("ssl-kurulum[%s]: %s başlıyor", k.AlanAdi, ad)

	basla := time.Now()
	mesaj, uyari, err := fn()
	sure := time.Since(basla).Round(100 * time.Millisecond).String()
	genel := ""
	var uy *sslUyari
	if errors.As(err, &uy) {
		mesaj, genel, uyari, err = uy.ham, uy.genel, true, nil
	}

	k.mu.Lock()
	k.Adimlar[i].Sure = sure
	switch {
	case err != nil:
		k.Adimlar[i].Durum, k.Adimlar[i].Mesaj = "hata", sslKirp(err.Error(), 800)
		k.Adimlar[i].genel = nginxset.SunucuHatasiGenel
	case uyari:
		if genel == "" {
			genel = mesaj
		}
		k.Adimlar[i].Durum, k.Adimlar[i].Mesaj = "uyari", sslKirp(mesaj, 800)
		k.Adimlar[i].genel = sslKirp(genel, 800)
	default:
		k.Adimlar[i].Durum, k.Adimlar[i].Mesaj = "tamam", sslKirp(mesaj, 800)
		k.Adimlar[i].genel = k.Adimlar[i].Mesaj
	}
	k.mu.Unlock()

	if err != nil {
		log.Printf("ssl-kurulum[%s]: %s HATA (%s): %v", k.AlanAdi, ad, sure, err)
		return err
	}
	if uyari {
		// 🔴 UYARI "tamam" DEĞİLDİR. mail-dogrula başarısızlığı bilerek uyarıya
		// çevriliyor (web SSL'i bloklamasın), ama onu "tamam" diye loglamak
		// başarısızlığı görünmez kılıyordu. Ayrı etiket + TAM mesaj (160 değil):
		// acme.sh'in asıl hata satırı journal'da da okunabilsin.
		log.Printf("ssl-kurulum[%s]: %s UYARI (%s): %s", k.AlanAdi, ad, sure, mesaj)
		return nil
	}
	log.Printf("ssl-kurulum[%s]: %s tamam (%s) %s", k.AlanAdi, ad, sure, sslKirp(mesaj, 160))
	return nil
}

func (k *sslIsi) bitir(err error) {
	k.mu.Lock()
	k.Bitti = time.Now()
	if err != nil {
		k.Durum, k.Hata, k.hataGenel = "hata", sslKirp(err.Error(), 400), nginxset.SunucuHatasiGenel
	} else {
		k.Durum, k.Hata, k.hataGenel = "tamam", "", ""
	}
	k.mu.Unlock()
}

func sslKirp(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	// 🔴 ORTADAN kırp, baştan DEĞİL. acme.sh'in gerçek hata satırı
	// ("Verify error detail…", "error:urn:ietf:params:acme:error:…") çıktının
	// SONUNDADIR; baştan kırpmak tam o teşhisi yok ediyordu (kullanıcı
	// "…Getti…" diye kesilen bir metin görüyordu). Baş yarı normal mesajlar
	// için, son yarı acme kuyruğu için tutulur.
	if n < 16 {
		return s[:n]
	}
	bas := n / 2
	son := n - bas - 1
	return s[:bas] + "…" + s[len(s)-son:]
}

// ── Kayıt defteri ───────────────────────────────────────────────────────────

var (
	sslMu     sync.Mutex
	sslIsleri = map[int64]*sslIsi{}
)

// SSLDurumu — domain için mevcut SSL oturumu (yoksa nil).
func SSLDurumu(id int64) *sslIsi {
	sslMu.Lock()
	defer sslMu.Unlock()
	return sslIsleri[id]
}

// SSLSuruyor — bu domain için SSL kurulumu şu an çalışıyor mu.
func SSLSuruyor(id int64) bool {
	k := SSLDurumu(id)
	if k == nil {
		return false
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.Durum == "calisiyor"
}

// sslBaslat — asenkron SSL kurulumunu başlatır ve oturumu döner. Goroutine
// istekten bağımsız context ile sonuna kadar çalışır (sekme kapansa da sürer).
func (h *Handlers) sslBaslat(id int64, alanAdi, sk, phpSurum, backend, tip string, mailSSL bool, mailAltlar []string) *sslIsi {
	// Adım sayısı: her koşulda "cert" + "kayit"; posta SSL'i devredeyse
	// mail / mail-dogrula / mail-kur / webmail / dkim-dns de eklenir.
	// (mailEklentiAktif burada ölçülemez — ctx goroutine içinde açılıyor —
	// ama mailSSL false ise posta dalı HİÇ girilmez, o yüzden bu ayrım
	// ilerleme çubuğu için yeterli.)
	toplam := 2
	if mailSSL && tip == "letsencrypt" {
		toplam = 7
	}
	k := &sslIsi{DomainID: id, AlanAdi: alanAdi, Durum: "calisiyor", Basladi: time.Now(), Toplam: toplam}
	sslMu.Lock()
	sslIsleri[id] = k
	sslMu.Unlock()

	go func() {
		ctx, iptal := context.WithTimeout(context.Background(), 20*time.Minute)
		defer iptal()

		var certYol, keyYol string

		// 1) Sertifika çekimi.
		etiket := "Sertifika alınıyor (Let's Encrypt)"
		if tip == "self-signed" {
			etiket = "Kendinden imzalı sertifika oluşturuluyor"
		}
		err := k.adim("cert", etiket, func() (string, bool, error) {
			var e error
			if tip == "self-signed" {
				certYol, keyYol, e = sslSelfSigned(alanAdi, sk, phpSurum, backend)
			} else {
				certYol, keyYol, e = sslLetsEncrypt(alanAdi, sk, phpSurum, backend)
			}
			if e != nil {
				return "", false, e
			}
			return "sertifika alındı ve web sunucusuna kuruldu", false, nil
		})
		if err != nil {
			k.bitir(err)
			return
		}

		// 2) Kaydet — GERÇEK kaynağı diskteki cert'ten oku (LE başarısız olup
		//    self-signed'a düşülmüşse dürüst kaydet).
		err = k.adim("kayit", "Sertifika kaydediliyor", func() (string, bool, error) {
			varsayilan := time.Now().Add(365 * 24 * time.Hour)
			if tip == "letsencrypt" {
				varsayilan = time.Now().Add(90 * 24 * time.Hour)
			}
			kaynak, bitis, gercekLE := sertifikaGercek(certYol, tip, varsayilan)
			dbCtx, c := context.WithTimeout(context.Background(), 15*time.Second)
			defer c()
			if _, e := h.DB.ExecContext(dbCtx,
				`UPDATE domains SET ssl_aktif=1, ssl_kaynak=?, cert_path=?, key_path=?, ssl_bitis=?
				 WHERE id=?`, kaynak, certYol, keyYol, bitis, id); e != nil {
				return "", false, e
			}
			if tip == "letsencrypt" && !gercekLE {
				return "Let's Encrypt ALINAMADI; geçici kendinden imzalı sertifika kuruldu " +
					"(alan adı bu sunucuya çözülmüyor ya da ACME doğrulaması başarısız olabilir)", true, nil
			}
			return "kaynak: " + kaynak + ", bitiş: " + bitis.Format("2006-01-02"), false, nil
		})
		if err != nil {
			k.bitir(err)
			return
		}

		// 3) Mail SSL (yalnız LE + mail eklentisi aktif). Başarısızlık web SSL'i
		//    BLOKLAMAZ → uyarı olarak raporlanır (uyari=true).
		//    Bu best-effort adimlarin adim() donusu bilerek yok sayilir (_ =):
		//    hata uyariya cevrilip k.Adimlar'a yazilir, web SSL akisini durdurmaz.
		if mailSSL && tip == "letsencrypt" {
			if !h.mailEklentiAktif(ctx) {
				_ = k.adim("mail", "Posta SSL", func() (string, bool, error) {
					return "mail eklentisi etkin değil — posta SSL atlandı", true, nil
				})
			} else {
				var mc, mk string
				dogruTamam := k.adim("mail-dogrula",
					"Posta alt-alanları doğrulanıyor ve sertifika alınıyor (mail, webmail, smtp, imap, pop, autoconfig, autodiscover)",
					func() (string, bool, error) {
						c, key, kapsam, atlanan, e := mailSertAl(alanAdi, sk, mailAltlar)
						if e != nil {
							ek := ""
							if len(atlanan) > 0 {
								ek = " | atlanan: " + strings.Join(atlanan, ", ")
							}
							return sslUyariHam("posta sertifikası alınamadı", e, ek) // uyari — web SSL etkilenmez
						}
						mc, mk = c, key
						m := "kapsam: " + strings.Join(kapsam, ", ")
						if len(atlanan) > 0 {
							m += " | atlanan (DNS/doğrulama yok): " + strings.Join(atlanan, ", ")
						}
						return m, false, nil
					}) == nil

				if dogruTamam && mc != "" {
					_ = k.adim("mail-kur", "Sertifika posta sunucusuna kuruluyor", func() (string, bool, error) {
						if e := mailSertGonder(alanAdi, mc, mk); e != nil {
							return sslUyariHam("sertifika alındı ama posta sunucusuna gönderilemedi", e, "")
						}
						// 🔴 Yalnız bu alanın SNI sertifikası. Varsayılan (SNI'sız)
						// sertifika SUNUCU GENELİDİR; kiracının sertifikası oraya
						// YAZILMAZ (eskiden ilk LE posta SSL'i isteyen kiracı sunucunun
						// kimliğini ele geçiriyordu). Varsayılan panel host
						// sertifikasından beslenir (bkz. dosya başındaki not).
						return "posta sunucusu (IMAP/POP/SMTP) sertifikaya geçirildi", false, nil
					})
					_ = k.adim("webmail", "Webmail ve otomatik kurulum (autodiscover) yapılandırılıyor", func() (string, bool, error) {
						if e := webmailVhostYaz(alanAdi, mc, mk); e != nil {
							return sslUyariHam("webmail vhost güncellenemedi", e, "")
						}
						return "webmail.<d>, autoconfig ve autodiscover güvenli sertifikayla yapılandırıldı", false, nil
					})
					// DKIM DNS senkronu: mail eklentisinin imzalamada kullandığı GERÇEK
					// mail._domainkey key'ini panel DNS'e yayınla (uyumsuz default._domainkey
					// kaldırılır). Aksi halde alıcı public key'i bulamaz, DKIM doğrulanamaz.
					_ = k.adim("dkim-dns", "DKIM imzası DNS'e yayınlanıyor (mail._domainkey)", func() (string, bool, error) {
						txt, e := mailDKIMAl(alanAdi)
						if e != nil {
							return sslUyariHam("DKIM anahtarı eklentiden alınamadı", e, "")
						}
						if txt == "" {
							return "eklenti DKIM anahtarı vermedi — DNS'e yazılmadı", true, nil
						}
						if e := mailDKIMYayinla(ctx, h.DB, id, txt); e != nil {
							return sslUyariHam("DKIM DNS'e yazılamadı", e, "")
						}
						return "DKIM public key mail._domainkey olarak yayınlandı (imzalayan gerçek anahtar)", false, nil
					})
				}
			}
		}

		k.bitir(nil)
	}()

	return k
}

// SSLIlerleme — GET /domains/{id}/ssl/ilerleme. Devam eden/biten SSL oturumu.
func (h *Handlers) SSLIlerleme(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	k := SSLDurumu(id)
	if k == nil {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"durum": "yok"})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, k.Goruntu(middleware.RolFrom(r) == "admin"))
}
