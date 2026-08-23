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
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/httpx"
	"girginospanel/internal/provisioner"
	"girginospanel/internal/dns"
)

// SSLAdim — tek SSL adımı.
type SSLAdim struct {
	Ad     string `json:"ad"`
	Etiket string `json:"etiket"`
	Durum  string `json:"durum"` // bekliyor|calisiyor|tamam|uyari|hata
	Mesaj  string `json:"mesaj"`
	Sure   string `json:"sure"`
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
}

func (k *sslIsi) Goruntu() SSLGoruntu {
	k.mu.Lock()
	defer k.mu.Unlock()
	a := make([]SSLAdim, len(k.Adimlar))
	copy(a, k.Adimlar)
	g := SSLGoruntu{DomainID: k.DomainID, AlanAdi: k.AlanAdi, Durum: k.Durum, Hata: k.Hata, Adimlar: a}
	if !k.Basladi.IsZero() {
		g.Basladi = k.Basladi.UTC().Format(time.RFC3339)
	}
	if !k.Bitti.IsZero() {
		g.Bitti = k.Bitti.UTC().Format(time.RFC3339)
	}
	return g
}

// adim — adımı işler; fn (mesaj, uyari, err) döner. err → 'hata' + iş durur;
// uyari=true → 'uyari' + iş DEVAM eder (mail SSL web SSL'i bloklamaz).
func (k *sslIsi) adim(ad, etiket string, fn func() (string, bool, error)) error {
	k.mu.Lock()
	k.Adimlar = append(k.Adimlar, SSLAdim{Ad: ad, Etiket: etiket, Durum: "calisiyor"})
	i := len(k.Adimlar) - 1
	k.mu.Unlock()
	log.Printf("ssl-kurulum[%s]: %s başlıyor", k.AlanAdi, ad)

	basla := time.Now()
	mesaj, uyari, err := fn()
	sure := time.Since(basla).Round(100 * time.Millisecond).String()

	k.mu.Lock()
	k.Adimlar[i].Sure = sure
	switch {
	case err != nil:
		k.Adimlar[i].Durum, k.Adimlar[i].Mesaj = "hata", sslKirp(err.Error(), 800)
	case uyari:
		k.Adimlar[i].Durum, k.Adimlar[i].Mesaj = "uyari", sslKirp(mesaj, 800)
	default:
		k.Adimlar[i].Durum, k.Adimlar[i].Mesaj = "tamam", sslKirp(mesaj, 800)
	}
	k.mu.Unlock()

	if err != nil {
		log.Printf("ssl-kurulum[%s]: %s HATA (%s): %v", k.AlanAdi, ad, sure, err)
		return err
	}
	log.Printf("ssl-kurulum[%s]: %s tamam (%s) %s", k.AlanAdi, ad, sure, sslKirp(mesaj, 160))
	return nil
}

func (k *sslIsi) bitir(err error) {
	k.mu.Lock()
	k.Bitti = time.Now()
	if err != nil {
		k.Durum, k.Hata = "hata", sslKirp(err.Error(), 400)
	} else {
		k.Durum, k.Hata = "tamam", ""
	}
	k.mu.Unlock()
}

func sslKirp(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
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
	k := &sslIsi{DomainID: id, AlanAdi: alanAdi, Durum: "calisiyor", Basladi: time.Now()}
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
				certYol, keyYol, e = provisioner.EnableSelfSigned(alanAdi, sk, phpSurum, backend)
			} else {
				certYol, keyYol, e = provisioner.EnableLetsEncrypt(alanAdi, sk, phpSurum, backend)
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
		if mailSSL && tip == "letsencrypt" {
			if !h.mailEklentiAktif(ctx) {
				k.adim("mail", "Posta SSL", func() (string, bool, error) {
					return "mail eklentisi etkin değil — posta SSL atlandı", true, nil
				})
			} else {
				var mc, mk string
				dogruTamam := k.adim("mail-dogrula",
					"Posta alt-alanları doğrulanıyor ve sertifika alınıyor (mail, webmail, smtp, imap, pop, autoconfig, autodiscover)",
					func() (string, bool, error) {
						c, key, kapsam, atlanan, e := provisioner.MailSertifikaAl(alanAdi, sk, mailAltlar)
						if e != nil {
							m := "posta sertifikası alınamadı: " + e.Error()
							if len(atlanan) > 0 {
								m += " | atlanan: " + strings.Join(atlanan, ", ")
							}
							return m, true, nil // uyari — web SSL etkilenmez
						}
						mc, mk = c, key
						m := "kapsam: " + strings.Join(kapsam, ", ")
						if len(atlanan) > 0 {
							m += " | atlanan (DNS/doğrulama yok): " + strings.Join(atlanan, ", ")
						}
						return m, false, nil
					}) == nil

				if dogruTamam && mc != "" {
					k.adim("mail-kur", "Sertifika posta sunucusuna kuruluyor", func() (string, bool, error) {
						if e := provisioner.MailSertifikaGonder(alanAdi, mc, mk); e != nil {
							return "sertifika alındı ama posta sunucusuna gönderilemedi: " + e.Error(), true, nil
						}
						// Varsayılan (SNI-siz) sertifikayı da geçerli yap: Outlook gibi
						// SNI göndermeyen istemciler self-signed görüp şifre soruyordu.
						if degisti, e := provisioner.MailVarsayilanKur(mc, mk); e != nil {
							return "posta SNI sertifikası kuruldu; varsayılan (SNI-siz) güncellenemedi: " + e.Error(), true, nil
						} else if degisti {
							return "posta sunucusu sertifikaya geçirildi (SNI + varsayılan — SNI göndermeyen istemciler de güvenli)", false, nil
						}
						return "posta sunucusu (IMAP/POP/SMTP) sertifikaya geçirildi", false, nil
					})
					k.adim("webmail", "Webmail ve otomatik kurulum (autodiscover) yapılandırılıyor", func() (string, bool, error) {
						if e := provisioner.WebmailVhostDomainYaz(alanAdi, mc, mk); e != nil {
							return "webmail vhost güncellenemedi: " + e.Error(), true, nil
						}
						return "webmail.<d>, autoconfig ve autodiscover güvenli sertifikayla yapılandırıldı", false, nil
					})
					// DKIM DNS senkronu: mail eklentisinin imzalamada kullandığı GERÇEK
					// mail._domainkey key'ini panel DNS'e yayınla (uyumsuz default._domainkey
					// kaldırılır). Aksi halde alıcı public key'i bulamaz, DKIM doğrulanamaz.
					k.adim("dkim-dns", "DKIM imzası DNS'e yayınlanıyor (mail._domainkey)", func() (string, bool, error) {
						txt, e := provisioner.MailDKIMTXTAl(alanAdi)
						if e != nil {
							return "DKIM anahtarı eklentiden alınamadı: " + e.Error(), true, nil
						}
						if txt == "" {
							return "eklenti DKIM anahtarı vermedi — DNS'e yazılmadı", true, nil
						}
						if e := dns.MailDKIMYayinla(ctx, h.DB, id, txt); e != nil {
							return "DKIM DNS'e yazılamadı: " + e.Error(), true, nil
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
	httpx.WriteJSON(w, http.StatusOK, k.Goruntu())
}
