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

	"girginospanel/internal/dnsutil"
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
	Tip        string   `json:"tip"`                   // "self-signed" | "letsencrypt"
	MailSSL    bool     `json:"mail_ssl,omitempty"`    // mail eklentisi aktifse: mail.<d>+webmail.<d> cert al + mail stack'e kur
	MailAltlar []string `json:"mail_altlar,omitempty"` // secilen mail alt-alan prefix'leri (bos=tumu)
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
	var kaynak, certYol, keyYol, bitisDB string
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT ssl_aktif, ssl_kaynak, cert_path, key_path,
		   COALESCE(DATE_FORMAT(ssl_bitis,'%Y-%m-%dT%H:%i:%s'),'')
		 FROM domains WHERE id=?`, id).
		Scan(&aktif, &kaynak, &certYol, &keyYol, &bitisDB)
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
		BitisISO: h.sslBitisISO(certYol, kaynak, bitisDB),
		CertYol:  certYol,
		KeyYol:   keyYol,
	})
}

// sslBitisISO — SSL bitis tarihinin TEK GERCEK KAYNAGI: DISKTEKI sertifikanin NotAfter'i.
//
// 🔴 NEDEN (ayni ekranda iki farkli tarih): "Mevcut Durum" DB'deki YAKLASIK degeri
// okuyordu — domains/handlers.go oto-SSL yolu `ssl_bitis=DATE_ADD(NOW(), INTERVAL 90 DAY)`
// yaziyor (sunucu-YEREL NOW, gercek NotAfter degil) — ve burasi bunu
// `DATE_FORMAT(...,'...Z')` ile SAHTE `Z` (UTC) etiketiyle donduruyordu. "Kapsam" ucu
// (ssl_kapsam.go sertifikaOku) ise diskteki GERCEK sertifikanin
// `c.NotAfter.UTC().Format(time.RFC3339)` degerini gosteriyordu → iki farkli tarih.
//
// Cozum: ayni cert_path'ten ayni degeri ayni formatta uret. Sertifika okunamazsa DB
// degerine DUSULUR, ama sahte `Z` ARTIK YOK: DB degeri sunucu-yerel kabul edilip
// (DATE_ADD(NOW()) oyle yaziyor) UTC'ye CEVRILIR.
func (h *Handlers) sslBitisISO(certYol, kaynak, bitisDB string) string {
	// 1) Gercek sertifika (ssl_kapsam ile AYNI dosya: domains.cert_path).
	if certYol != "" {
		if _, gercekBitis, _ := sertifikaGercek(certYol, kaynak, time.Time{}); !gercekBitis.IsZero() {
			// ssl_kapsam.go:sertifikaOku ile BIREBIR ayni format → iki uc ayni dizeyi doner.
			return gercekBitis.UTC().Format(time.RFC3339)
		}
	}
	// 2) Fallback: DB degeri (yaklasik). Sunucu-yerel → UTC.
	if bitisDB == "" {
		return ""
	}
	if t, e := time.ParseInLocation("2006-01-02T15:04:05", bitisDB, time.Local); e == nil {
		return t.UTC().Format(time.RFC3339)
	}
	// Ayristirilamadi: sahte `Z` eklemektense ZAMAN DILIMSIZ don (yalan etiket yok).
	return bitisDB
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
	// 🔴 KALICI FIX: issuance ÖNCESİ yerel çözümleyici önbelleğini temizle —
	// domain bu sunucuya yeni taşındıysa eski IP önbellekte kalıp ön-doğrulamayı
	// düşürebilir ya da mail daemon/oto-yapılandırmaya yanlış IP verebilir.
	// (Kamu-DNS ön-kontrolüyle birlikte bayat-önbellek sınıfını kapatır.)
	if req.Tip == "letsencrypt" {
		dnsutil.OnbellekTemizle(alanAdi)
	}

	mailSSL := (req.MailSSL || h.mailEklentiAktif(r.Context())) && req.Tip == "letsencrypt"
	h.sslBaslat(id, alanAdi, sk, phpSurum, backend, req.Tip, mailSSL, req.MailAltlar)

	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{
		"ok":    true,
		"durum": "basladi",
		"mesaj": "SSL kurulumu başladı — ilerleme aşağıda görünecek. Sayfayı kapatsanız bile kurulum arka planda sürer.",
	})
}

// SSLDnsYenile — POST /domains/{id}/ssl/dns-yenile
//
// Yerel çözümleyici önbelleğini temizler. Bir domaini bu sunucuya YENİ
// taşıdığında, sunucunun kendi çözümleyicisi (unbound/systemd-resolved) eski IP'yi
// önbellekte tutabilir; bu yüzden SSL kapsam ekranı "DNS yok" gösterir ve mail
// sertifikası çıkmaz — oysa kamu DNS doğrudur. Kullanıcı "DNS'i yenile" butonuyla
// çağırır; ardından kapsam yeniden okunur ve SSL "Yeniden çıkar" ile denenir.
func (h *Handlers) SSLDnsYenile(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var alanAdi string
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT alan_adi FROM domains WHERE id=?`, id).Scan(&alanAdi); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	dnsutil.OnbellekTemizle(alanAdi)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"mesaj": "DNS önbelleği temizlendi. Kayıtlar kamu DNS'ten yeniden okunacak — birkaç saniye sonra “Yeniden çıkar” ile SSL'i deneyin.",
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
