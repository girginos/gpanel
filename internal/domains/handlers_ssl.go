package domains

import (
	"context"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"os"
	"strconv"
	"time"

	"girginospanel/internal/httpx"
	"girginospanel/internal/provisioner"

	"github.com/go-chi/chi/v5"
)

// sertifikaGercek — KURULAN cert dosyasını okuyup GERÇEK kaynağı ve bitişi döner.
//
// 🔴 NEDEN: EnableLetsEncrypt, LE çekimi başarısız olunca içeride SESSİZCE
// self-signed fail-safe'e düşüp err=nil döner (443'ü ayakta tutmak için, bilinçli).
// Handler istenen tipe (req.Tip) güvenip 'ssl_kaynak=letsencrypt' + 90 gün yazarsa,
// panel kendinden imzalı sertifikayı "Let's Encrypt · KORUMALI · 90 gün" diye
// YALAN raporlar (mail yolu dürüsttü, web yolu değildi — feedback_failure_renders_as_reassurance).
// Tek doğru kaynak: diskteki cert'in KENDİSİ. issuer==subject → self-signed.
// Ayrıştırılamazsa istenen tipe düşülür (davranış bozulmaz), bitiş varsayılan.
func sertifikaGercek(certYol, istenenTip string, varsayilanBitis time.Time) (kaynak string, bitis time.Time, gercekLE bool) {
	kaynak, bitis = istenenTip, varsayilanBitis
	b, err := os.ReadFile(certYol)
	if err != nil {
		return
	}
	blk, _ := pem.Decode(b)
	if blk == nil {
		return
	}
	c, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		return
	}
	bitis = c.NotAfter
	// Kendinden imzalı: issuer == subject. Gerçek CA (LE) böyle DEĞİLDİR.
	if c.Issuer.String() == c.Subject.String() {
		return "self-signed", bitis, false
	}
	return "letsencrypt", bitis, true
}

type sslIssueReq struct {
	Tip     string `json:"tip"`                // "self-signed" | "letsencrypt"
	MailSSL bool   `json:"mail_ssl,omitempty"` // mail eklentisi aktifse: mail.<d>+webmail.<d> cert al + mail stack'e kur
}

// mailEklentiAktif — mail eklentisi kurulu ve etkin mi (paralı/lisans gate).
func (h *Handlers) mailEklentiAktif(ctx context.Context) bool {
	var aktif int
	err := h.DB.QueryRowContext(ctx, `SELECT aktif FROM cp_eklentiler WHERE ad='mail'`).Scan(&aktif)
	return err == nil && aktif == 1
}

type sslDurumResp struct {
	Aktif    bool   `json:"aktif"`
	Kaynak   string `json:"kaynak"`
	BitisISO string `json:"bitis_iso,omitempty"`
	CertYol  string `json:"cert_yol,omitempty"`
	KeyYol   string `json:"key_yol,omitempty"`
}

func (h *Handlers) SSLDurum(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var aktif int
	var kaynak, certYol, keyYol, bitis string
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT ssl_aktif, ssl_kaynak, cert_path, key_path,
		   COALESCE(DATE_FORMAT(ssl_bitis,'%Y-%m-%dT%H:%i:%sZ'),'')
		 FROM domains WHERE id=?`, id).
		Scan(&aktif, &kaynak, &certYol, &keyYol, &bitis)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "okuma: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, sslDurumResp{
		Aktif:    aktif == 1,
		Kaynak:   kaynak,
		BitisISO: bitis,
		CertYol:  certYol,
		KeyYol:   keyYol,
	})
}

func (h *Handlers) SSLIssue(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var req sslIssueReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	if req.Tip == "" {
		req.Tip = "self-signed"
	}
	if req.Tip != "self-signed" && req.Tip != "letsencrypt" {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz tip (self-signed|letsencrypt)")
		return
	}
	var alanAdi, sk, phpSurum, backend string
	var isDemo int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT alan_adi, sistem_kullanici, php_surum, is_demo, COALESCE(web_backend,'php-fpm') FROM domains WHERE id=?`, id).
		Scan(&alanAdi, &sk, &phpSurum, &isDemo, &backend)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	// 🔴 ErrNoRows DIŞI bir Scan hatası (ör. eksik kolon/DB arızası) EskiDEN
	// YUTULUYORDU → alanAdi="" ile devam edilip openssl'e
	// `-addext subjectAltName=DNS:,` gidiyor ("invalid null value") ya da yanlış
	// vhost'a dokunuluyordu. Artık açıkça 500 döner.
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "okuma: "+err.Error())
		return
	}
	if alanAdi == "" || sk == "" {
		httpx.WriteError(w, http.StatusInternalServerError, "domain kaydı eksik (alan adı/sistem kullanıcısı boş)")
		return
	}
	if isDemo == 1 {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğe SSL kurulamaz")
		return
	}

	var certYol, keyYol string
	switch req.Tip {
	case "self-signed":
		certYol, keyYol, err = provisioner.EnableSelfSigned(alanAdi, sk, phpSurum, backend)
	case "letsencrypt":
		certYol, keyYol, err = provisioner.EnableLetsEncrypt(alanAdi, sk, phpSurum, backend)
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "SSL kurulum: "+err.Error())
		return
	}

	// Varsayılan bitiş (cert okunamazsa): LE=90g, self-signed=365g.
	varsayilan := time.Now().Add(365 * 24 * time.Hour)
	if req.Tip == "letsencrypt" {
		varsayilan = time.Now().Add(90 * 24 * time.Hour)
	}
	// 🔴 GERÇEĞİ diskteki cert'ten oku — istenen tipe DEĞİL. LE başarısız olup
	// self-signed fail-safe'e düşülmüşse burada yakalanır ve DÜRÜST kaydedilir.
	kaynak, bitis, gercekLE := sertifikaGercek(certYol, req.Tip, varsayilan)

	// 🔴 KOPUK CONTEXT: LE+mail çekimi 30sn'yi aşabilir; tarayıcı (axios) o sırada
	// isteği kesip bağlantıyı kapatırsa r.Context() İPTAL olur. Cert nginx'e kurulup
	// yeniden yüklenmiş (site HTTPS) AMA bu UPDATE r.Context() ile yapılırsa
	// "context canceled" ile DÜŞER → cert diskte ama panel "KORUMASIZ" der (durum
	// diskle çelişir). Sonucu KALICI kılmak için ayrık context kullanılır.
	dbCtx, iptal := context.WithTimeout(context.Background(), 15*time.Second)
	defer iptal()
	if _, err := h.DB.ExecContext(dbCtx,
		`UPDATE domains SET ssl_aktif=1, ssl_kaynak=?, cert_path=?, key_path=?, ssl_bitis=?
		 WHERE id=?`, kaynak, certYol, keyYol, bitis, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "DB güncelleme: "+err.Error())
		return
	}

	yanit := map[string]any{
		"ok":    true,
		"id":    id,
		"tip":   kaynak, // GERÇEK kaynak (istenen değil)
		"cert":  certYol,
		"key":   keyYol,
		"bitis": bitis.Format("2006-01-02"),
	}
	// LE istendi ama self-signed'a düşüldü → SESSİZ GEÇME, açıkça söyle
	// (mail yolu gibi dürüst). Kullanıcı "KORUMALI/Let's Encrypt" sanmasın.
	if req.Tip == "letsencrypt" && !gercekLE {
		yanit["ssl_uyari"] = "Let's Encrypt sertifikası ALINAMADI; site geçici olarak " +
			"kendinden imzalı (self-signed) sertifikayla ayakta tutuluyor — tarayıcı " +
			"“güvenli değil” uyarısı verebilir. En sık sebep: alan adı bu sunucuya " +
			"çözülmüyor ya da ACME challenge (/.well-known/acme-challenge/) doğrulanamıyor. " +
			"DNS’i bu sunucuya yöneltip tekrar deneyin."
	}

	// Mail SSL — yalnız Let's Encrypt + mail eklentisi aktifse. mail.<d>+webmail.<d>
	// için AYRI cert alınır, mail stack'e (eklenti /sertifika) kurulur ve
	// webmail vhost'u GERÇEK sertifikaya geçirilir. Başarısızlık web SSL'i
	// ETKİLEMEZ (uyarı döner — sessiz başarı yok).
	//
	// 🔴 SAN TUZAĞI provisioner'da çözülür: mail./webmail. adları SAN'a
	// girmeden ÖNCE tek tek sınanır (DNS + challenge 200). Doğrulanamayan ad
	// SAN'dan ÇIKARILIR ve burada 'mail_ssl_atlanan' olarak RAPORLANIR —
	// aksi halde tek bir ad yüzünden TÜM LE siparişi düşerdi.
	if req.MailSSL && req.Tip == "letsencrypt" {
		if !h.mailEklentiAktif(r.Context()) {
			yanit["mail_ssl_uyari"] = "mail eklentisi etkin değil — mail SSL atlandı"
		} else if mc, mk, kapsam, atlanan, e := provisioner.MailSertifikaAl(alanAdi, sk); e != nil {
			yanit["mail_ssl_uyari"] = "mail cert alınamadı: " + e.Error()
			if len(atlanan) > 0 {
				yanit["mail_ssl_atlanan"] = atlanan
			}
		} else {
			yanit["mail_ssl"] = true
			yanit["mail_kapsam"] = kapsam
			if len(atlanan) > 0 {
				yanit["mail_ssl_atlanan"] = atlanan
			}
			if e := provisioner.MailSertifikaGonder(alanAdi, mc, mk); e != nil {
				yanit["mail_ssl_uyari"] = "mail cert alındı ama eklentiye gönderilemedi: " + e.Error()
			}
			// Webmail'i self-signed'dan GERÇEK sertifikaya geçir.
			if e := provisioner.WebmailVhostDomainYaz(alanAdi, mc, mk); e != nil {
				yanit["webmail_vhost_uyari"] = "webmail vhost'u güncellenemedi: " + e.Error()
			} else {
				yanit["webmail_https"] = "https://webmail." + alanAdi
			}
		}
	}

	httpx.WriteJSON(w, http.StatusOK, yanit)
}

func (h *Handlers) SSLDisable(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var alanAdi, sk, phpSurum, backend string
	var isDemo int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT alan_adi, sistem_kullanici, php_surum, is_demo, COALESCE(web_backend,'php-fpm') FROM domains WHERE id=?`, id).
		Scan(&alanAdi, &sk, &phpSurum, &isDemo, &backend)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	// SSLIssue ile aynı sınıf: yutulan Scan hatası boş alanAdi ile DisableSSL'e
	// gidip yanlış/eksik vhost yolunu işleyebilirdi.
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "okuma: "+err.Error())
		return
	}
	if alanAdi == "" || sk == "" {
		httpx.WriteError(w, http.StatusInternalServerError, "domain kaydı eksik (alan adı/sistem kullanıcısı boş)")
		return
	}
	if isDemo == 1 {
		httpx.WriteError(w, http.StatusForbidden, "demo abonelik dokunulamaz")
		return
	}
	if err := provisioner.DisableSSL(alanAdi, sk, phpSurum, backend); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "SSL kapat: "+err.Error())
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE domains SET ssl_aktif=0, ssl_kaynak='', cert_path='', key_path='', ssl_bitis=NULL
		 WHERE id=?`, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "DB güncelleme: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
