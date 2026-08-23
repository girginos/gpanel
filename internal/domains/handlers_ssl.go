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
	if SSLSuruyor(id) {
		httpx.WriteError(w, http.StatusConflict, "Bu alan adı için SSL kurulumu zaten sürüyor.")
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

	// ASENKRON: SSL çekimi (özellikle mail SSL — 7 SAN) uzun sürer; iş arka planda
	// yürür (sekme kapansa da). İlerleme /domains/{id}/ssl/ilerleme'den izlenir.
	// Mail eklentisi aktifse Posta SSL OTOMATIK dahil (kutu isaretsiz olsa da):
	// aksi halde mail sunucusu kendinden imzali sertifikada kalir ve Outlook/
	// istemciler her baglantida sifre sorar. Basarisizligi web SSL'i bloklamaz.
	mailSSL := (req.MailSSL || h.mailEklentiAktif(r.Context())) && req.Tip == "letsencrypt"
	h.sslBaslat(id, alanAdi, sk, phpSurum, backend, req.Tip, mailSSL)

	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{
		"ok":    true,
		"durum": "basladi",
		"mesaj": "SSL kurulumu başladı — ilerleme aşağıda görünecek. Sayfayı kapatsanız bile kurulum arka planda sürer.",
	})
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
