// Package winajan — Windows ajanlarinin panel tarafindaki kaydi ve kesfi.
//
// Panel (Linux) Windows hostlarini bu kayitlar uzerinden tanir. Ajanin
// /saglik ucundan surum/kanal/yetenekler okunur; site islemleri ancak ajan
// YetSite ilan edince (kendini-sina gecince) yonlendirilecek.
//
// 🔴 GUVENLIK KARARLARI:
//   - Jeton DB'de DUZ METIN DEGIL: gizli.SaklaBagli(jeton, adres) ile AEAD;
//     baglam=adres oldugu icin bir satirin ciphertext'i baska satira tasinamaz.
//     (GVM denetimindeki "admin token duz metin" bulgusunun dersi burada
//     bastan uygulaniyor.)
//   - Ajanla konusma TLS + SERTIFIKA IGNELEME: ajan self-signed sertifika
//     sunar; panel ilk kayitta (TOFU) sertifikanin SHA256 parmak izini ogrenip
//     DB'ye yazar ve sonraki HER cagrida zorlar. Degisen sertifika cagriyi
//     keser, durum 'parmak-izi-uyusmazligi' olur — igne SESSIZCE guncellenmez,
//     operator ajani dogrulayip yeniden kaydeder.
//   - Adres kapisi (ozel ag / loopback) TLS'e RAGMEN kaliyor: derinlemesine
//     savunma — yonetim duzlemi kamu internetine hic acilmasin.
package winajan

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/gizli"
	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
)

type Handlers struct{ DB *sql.DB }

func New(db *sql.DB) *Handlers { return &Handlers{DB: db} }

type Ajan struct {
	ID            int64  `json:"id"`
	Ad            string `json:"ad"`
	Adres         string `json:"adres"`
	Surum         string `json:"surum"`
	Kanal         string `json:"kanal"`
	Yetenekler    uint32 `json:"yetenekler"`
	Durum         string `json:"durum"`
	SonGorulme    string `json:"son_gorulme"`
	ParmakIziKisa string `json:"parmak_izi_kisa"`
}

// errParmakIzi — DB'deki igne ile ajanin sundugu sertifika uyusmuyor.
// 🔴 Bu SESSIZCE duzeltilecek bir durum DEGIL: araya girme (MITM) ya da ajanin
// habersiz yeniden kurulumu olabilir. Cozum operatorun bilincli yeniden kaydi.
var errParmakIzi = errors.New("ajan sertifikası değişti — güvenlik: yeniden kaydedin")

// parmakIziKisa — yanit/listede gosterilecek kisa bicim (ilk 16 hex karakter).
// Tam deger DB'de kalir; UI karsilastirmasina onek yeter, tam degeri her
// yanita tasimaya gerek yok.
func parmakIziKisa(p string) string {
	if len(p) > 16 {
		return p[:16]
	}
	return p
}

// adresGecerli — "host:port" bicimini ve OZEL AG sartini dogrular.
// 🔴 TLS VAR AMA KAPI KALIYOR: sifreleme jetonu yolda korur, kapi ise saldiri
// yuzeyini kucultur (derinlemesine savunma) — yonetim duzlemi kamu internetine
// hic acilmasin.
func adresGecerli(adres string) error {
	host, port, err := net.SplitHostPort(adres)
	if err != nil || host == "" || port == "" {
		return fmt.Errorf("adres 'host:port' biciminde olmali")
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// Alan adi cozumlemesi degisken sonuc verir (DNS rebinding); simdilik
		// yalniz IP kabul ediyoruz — kesin ve denetlenebilir.
		return fmt.Errorf("simdilik yalniz IP adresi kabul ediliyor (alan adi degil)")
	}
	if !ip.IsLoopback() && !ip.IsPrivate() {
		// 🔴 BILINCLI GECIT — VARSAYILAN KAPALI. Kamu IP'sine yalniz operatorun
		// panel ortamina ACIKCA yazdigi bayrakla izin verilir (ornegin ozel agi
		// olmayan test VM'leri). Guvenligi tasiyan sey TLS + TOFU igneleme:
		// jeton, sertifika parmak izi dogrulanmadan tek bayt gonderilmez.
		// Uretimde bayrak yazilmadigi surece kapi aynen durur.
		if os.Getenv("GOSP_WINAJAN_KAMU_IZIN") == "1" {
			return nil
		}
		return fmt.Errorf("ajan adresi ozel ag veya loopback olmali (%s kamu adresi) — TLS olsa da yonetim duzlemi ozel agda kalir; bilerek acmak icin panel ortamina GOSP_WINAJAN_KAMU_IZIN=1 yazin", host)
	}
	return nil
}

// sertifikaParmakIzi — adresteki TLS sunucusunun yaprak sertifikasinin SHA256
// parmak izini (hex, kucuk harf, ayracsiz) okur. TOFU'nun "ilk bakis" adimi.
//
// 🔴 InsecureSkipVerify BURADA BILINCLI VE ZARARSIZ: bu baglanti YALNIZ
// sertifikayi OKUMAK icin kurulur; uzerinden tek bayt istek/jeton GITMEZ.
// Jeton tasiyan asil cagri hemen ardindan tam da bu parmak izine ignelenmis
// istemciyle yapilir (bkz. ignelenmisIstemci).
func sertifikaParmakIzi(adres string) (string, error) {
	baglayici := &net.Dialer{Timeout: 5 * time.Second}
	baglanti, err := tls.DialWithDialer(baglayici, "tcp", adres, &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // G402 bilinçli: bu bağlantı YALNIZ sertifika parmak izini OKUR (TOFU ilk-bakış), üzerinden jeton/istek GİTMEZ; asıl çağrı ignelenmisIstemci ile parmak-izine iğnelenir.
		MinVersion:         tls.VersionTLS12,
	})
	if err != nil {
		return "", fmt.Errorf("ajana TLS ile baglanilamadi: %w", err)
	}
	defer baglanti.Close()
	sertifikalar := baglanti.ConnectionState().PeerCertificates
	if len(sertifikalar) == 0 {
		return "", fmt.Errorf("ajan sertifika sunmadi")
	}
	ozet := sha256.Sum256(sertifikalar[0].Raw)
	return hex.EncodeToString(ozet[:]), nil
}

// ignelenmisIstemci — yalniz verilen SHA256 parmak izine sahip sunucuyla
// konusan http istemcisi.
//
// 🔴 NEDEN InsecureSkipVerify + VerifyPeerCertificate BIRLIKTE: ajan
// self-signed sertifika sunar, CA zinciri dogrulamasi tanim geregi basarisiz
// olurdu. Onun yerine BILINCLI IGNELEME: InsecureSkipVerify zincir kontrolunu
// kapatir, VerifyPeerCertificate ham yaprak sertifikanin SHA256'sini beklenen
// igneyle karsilastirir. Uymayan sertifikada el sikisma, jeton daha
// GONDERILMEDEN kesilir.
func ignelenmisIstemci(parmakIzi string) *http.Client {
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec // G402 bilinçli sertifika iğneleme: self-signed ajan; zincir doğrulaması yerine VerifyPeerCertificate ham yaprak SHA256'sını beklenen iğneyle karşılaştırır, uymazsa jeton GÖNDERİLMEDEN el sıkışma kesilir.
				MinVersion:         tls.VersionTLS12,
				// 🔴 G123 — oturum devam ettirme (session resumption) igneyi (pinning)
				// atlayabilir: kisaltilmis (abbreviated) el sikismada sunucu sertifikayi
				// YENIDEN sunmaz ve VerifyPeerCertificate CAGRILMAYABILIR. Iki katmanli
				// savunma: (a) SessionTicketsDisabled=true + nil ClientSessionCache
				// varsayilani ile resumption tamamen kapatilir; (b) VerifyConnection Go
				// tarafindan TUM baglantilarda (resume dahil) cagrildigi icin igne resume
				// durumunda da zorlanir. mTLS jeton tasiyan istemci — bu yuzden kritik.
				SessionTicketsDisabled: true,
				VerifyConnection: func(cs tls.ConnectionState) error {
					if len(cs.PeerCertificates) == 0 {
						return fmt.Errorf("ajan sertifika sunmadi")
					}
					ozet := sha256.Sum256(cs.PeerCertificates[0].Raw)
					if hex.EncodeToString(ozet[:]) != parmakIzi {
						return errParmakIzi
					}
					return nil
				},
				VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
					if len(rawCerts) == 0 {
						return fmt.Errorf("ajan sertifika sunmadi")
					}
					ozet := sha256.Sum256(rawCerts[0])
					if hex.EncodeToString(ozet[:]) != parmakIzi {
						return errParmakIzi
					}
					return nil
				},
			},
		},
	}
}

// hataParmakIziMi — istemci hatasinin kokeni igne uyusmazligi mi?
// http istemcisi el sikisma hatasini birkac katman sarar (url.Error, OpError);
// errors.Is sarmal zincirini cozer, metin karsilastirmasi emniyet payidir.
func hataParmakIziMi(err error) bool {
	return errors.Is(err, errParmakIzi) || strings.Contains(err.Error(), errParmakIzi.Error())
}

type saglikYaniti struct {
	Platform   string `json:"platform"`
	Surum      string `json:"surum"`
	Kanal      string `json:"kanal"`
	Yetenekler uint32 `json:"yetenekler"`
	OrtamHata  string `json:"ortam_hata"`
}

// saglikSor — ajanin /saglik ucunu https uzerinden, verilen parmak izine
// IGNELENMIS istemciyle jetonla yoklar. 5 sn zaman asimi: erisilemeyen ajan
// panel istegini surundurmemeli.
func saglikSor(adres, jeton, parmakIzi string) (*saglikYaniti, error) {
	istemci := ignelenmisIstemci(parmakIzi)
	req, err := http.NewRequest(http.MethodGet, "https://"+adres+"/saglik", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Gosp-Jeton", jeton)
	yanit, err := istemci.Do(req)
	if err != nil {
		if hataParmakIziMi(err) {
			return nil, errParmakIzi
		}
		return nil, fmt.Errorf("ajana erisilemedi: %w", err)
	}
	defer yanit.Body.Close()
	if yanit.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("ajan jetonu reddetti (401) — GOSP_AGENT_JETON eslesmiyor")
	}
	if yanit.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ajan beklenmedik durum dondu: %d", yanit.StatusCode)
	}
	var s saglikYaniti
	if err := json.NewDecoder(yanit.Body).Decode(&s); err != nil {
		return nil, fmt.Errorf("saglik yaniti okunamadi: %w", err)
	}
	if s.Platform != "windows" {
		return nil, fmt.Errorf("beklenen platform windows, gelen: %q", s.Platform)
	}
	return &s, nil
}

// List — GET /windows-ajanlar
func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT id, ad, adres, surum, kanal, yetenekler, durum, parmak_izi,
		       COALESCE(DATE_FORMAT(son_gorulme, '%Y-%m-%d %H:%i:%s'), '')
		  FROM windows_ajanlar ORDER BY ad`)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "ajanlar okunamadı: "+err.Error())
		return
	}
	defer rows.Close()
	out := []Ajan{}
	for rows.Next() {
		var a Ajan
		var parmakIzi string
		if err := rows.Scan(&a.ID, &a.Ad, &a.Adres, &a.Surum, &a.Kanal, &a.Yetenekler, &a.Durum, &parmakIzi, &a.SonGorulme); err == nil {
			// Listeye tam iz degil onek: operator karsilastirmasi icin yeterli.
			a.ParmakIziKisa = parmakIziKisa(parmakIzi)
			out = append(out, a)
		}
	}
	if err := rows.Err(); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "ajan listesi yarım kaldı")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ajanlar": out})
}

// Ekle — POST /windows-ajanlar {ad, adres, jeton}
// Kayittan ONCE ajanla konusulur: ulasilaamayan/yanlis-jetonlu ajan hic
// kaydedilmez — olu kayit listede "belki calisir" diye durmasin.
//
// 🔴 TOFU (ilk kullanimda guven): once ajanin sertifikasi yalnizca OKUNUR ve
// SHA256 parmak izi cikarilir; saglik cagrisi BU IZE ignelenmis istemciyle
// yapilir, basarida iz DB'ye yazilir. Ilk temas anindaki araya girmeyi TOFU
// tanim geregi yakalayamaz — operator bunu, ajanin acilis logundaki parmak
// iziyle buradaki degeri karsilastirarak kapatir. Sonraki her cagri kilitlidir.
func (h *Handlers) Ekle(w http.ResponseWriter, r *http.Request) {
	var ist struct {
		Ad    string `json:"ad"`
		Adres string `json:"adres"`
		Jeton string `json:"jeton"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&ist); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "istek gövdesi okunamadı")
		return
	}
	ist.Ad = strings.TrimSpace(ist.Ad)
	ist.Adres = strings.TrimSpace(ist.Adres)
	if ist.Ad == "" || len(ist.Ad) > 64 {
		httpx.WriteError(w, http.StatusBadRequest, "ad boş olamaz (en çok 64 karakter)")
		return
	}
	if ist.Jeton == "" {
		httpx.WriteError(w, http.StatusBadRequest, "jeton boş olamaz")
		return
	}
	if err := adresGecerli(ist.Adres); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	parmakIzi, err := sertifikaParmakIzi(ist.Adres)
	if err != nil {
		httpx.WriteError(w, http.StatusBadGateway, err.Error())
		return
	}
	s, err := saglikSor(ist.Adres, ist.Jeton, parmakIzi)
	if err != nil {
		httpx.WriteError(w, http.StatusBadGateway, err.Error())
		return
	}
	res, err := h.DB.ExecContext(r.Context(), `
		INSERT INTO windows_ajanlar (ad, adres, jeton_sifreli, parmak_izi, surum, kanal, yetenekler, durum, son_gorulme)
		VALUES (?,?,?,?,?,?,?, 'saglikli', NOW())`,
		ist.Ad, ist.Adres, gizli.SaklaBagli(ist.Jeton, ist.Adres), parmakIzi, s.Surum, s.Kanal, s.Yetenekler)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			httpx.WriteError(w, http.StatusConflict, "bu adres zaten kayıtlı")
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "ajan kaydedilemedi: "+err.Error())
		return
	}
	id, _ := res.LastInsertId()
	uid, kullanici := middleware.Aktor(r)
	httpx.Denetim(h.DB, r, uid, kullanici, "winajan.ekle", ist.Ad,
		"windows ajanı eklendi ("+ist.Adres+", sürüm="+s.Surum+", parmak izi="+parmakIziKisa(parmakIzi)+")", 0, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id, "surum": s.Surum, "yetenekler": s.Yetenekler, "ortam_hata": s.OrtamHata, "parmak_izi_kisa": parmakIziKisa(parmakIzi)})
}

// Sina — POST /windows-ajanlar/{id}/sina : sagligi yeniden yoklar, kaydi tazeler.
// Cagri DB'deki parmak izine ignelenmis istemciyle yapilir; 0099 oncesi eski
// kayitlarda (parmak_izi bos) mevcut sertifika TOFU ile ogrenilip basarida
// DB'ye yazilir.
func (h *Handlers) Sina(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var adres, jetonSifreli, parmakIzi string
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT adres, jeton_sifreli, parmak_izi FROM windows_ajanlar WHERE id=?`, id).Scan(&adres, &jetonSifreli, &parmakIzi)
	if err == sql.ErrNoRows {
		httpx.WriteError(w, http.StatusNotFound, "ajan bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "ajan okunamadı")
		return
	}
	if parmakIzi == "" {
		// 🔴 0099 oncesi eski kayit: igne yok. Mevcut sertifika TOFU ile SIMDI
		// ogrenilir; asagida basarili sinayla DB'ye yazilir ve sonraki her
		// cagri kilitlenir. Bir kereye mahsus gecis yoludur.
		if parmakIzi, err = sertifikaParmakIzi(adres); err != nil {
			_, _ = h.DB.ExecContext(r.Context(),
				`UPDATE windows_ajanlar SET durum='erisilemez' WHERE id=?`, id)
			httpx.WriteError(w, http.StatusBadGateway, err.Error())
			return
		}
	}
	s, sErr := saglikSor(adres, gizli.CozBagli(jetonSifreli, adres), parmakIzi)
	if sErr != nil {
		// 🔴 Uyusmazlik 'erisilemez'den AYRI isaretlenir: biri gecici ag
		// sorunu, digeri guvenlik olayi — UI ikisini ayni gosteremez. Igne
		// sessizce tazelenmez; operator ajani dogrulayip yeniden kaydeder.
		durum := "erisilemez"
		if errors.Is(sErr, errParmakIzi) {
			durum = "parmak-izi-uyusmazligi"
		}
		_, _ = h.DB.ExecContext(r.Context(),
			`UPDATE windows_ajanlar SET durum=? WHERE id=?`, durum, id)
		httpx.WriteError(w, http.StatusBadGateway, sErr.Error())
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `
		UPDATE windows_ajanlar SET surum=?, kanal=?, yetenekler=?, parmak_izi=?, durum='saglikli', son_gorulme=NOW() WHERE id=?`,
		s.Surum, s.Kanal, s.Yetenekler, parmakIzi, id); err != nil {
		log.Printf("winajan: sağlık/son_gorulme güncellenemedi (id=%s): %v — sağlıklı ajan panelde bayat görünebilir", id, err)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "surum": s.Surum, "yetenekler": s.Yetenekler, "ortam_hata": s.OrtamHata, "parmak_izi_kisa": parmakIziKisa(parmakIzi)})
}

// Sil — DELETE /windows-ajanlar/{id}
func (h *Handlers) Sil(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var ad string
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT ad FROM windows_ajanlar WHERE id=?`, id).Scan(&ad); err == sql.ErrNoRows {
		httpx.WriteError(w, http.StatusNotFound, "ajan bulunamadı")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `DELETE FROM windows_ajanlar WHERE id=?`, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "ajan silinemedi")
		return
	}
	uid, kullanici := middleware.Aktor(r)
	httpx.Denetim(h.DB, r, uid, kullanici, "winajan.sil", ad, "windows ajanı silindi", 0, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ── ajan vekil uclari (Faz 1: olay gunlugu + zamanlanmis gorevler) ──────────
//
// Panel, ajanin salt-okunur uclarini AYNI ignelenmis TLS istemcisiyle vekil
// eder. Taze veri her seferinde ajandan cekilir; panelde onbellek YOK —
// olay gunlugu eskimis veriyle yanlis teshis koydurur.

// ajanBilgi — id ile kayitli ajanin baglanti uclusunu getirir.
func (h *Handlers) ajanBilgi(r *http.Request, id string) (adres, jeton, parmakIzi string, hata error) {
	var jetonSifreli string
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT adres, jeton_sifreli, parmak_izi FROM windows_ajanlar WHERE id=?`, id).
		Scan(&adres, &jetonSifreli, &parmakIzi)
	if err == sql.ErrNoRows {
		return "", "", "", fmt.Errorf("ajan bulunamadı")
	}
	if err != nil {
		return "", "", "", fmt.Errorf("ajan okunamadı")
	}
	if parmakIzi == "" {
		// Eski kayit: igne yok. Okuma uclari IGNESIZ CALISMAZ — once Sina
		// kosulmali (TOFU orada). Sessizce ignesiz baglanmak butun modeli deler.
		return "", "", "", fmt.Errorf("bu ajanın sertifika iğnesi yok — önce Sına çalıştırın")
	}
	return adres, gizli.CozBagli(jetonSifreli, adres), parmakIzi, nil
}

// vekilGet — ajanin verilen yolunu ignelenmis istemciyle ceker ve yaniti
// oldugu gibi aktarir (1 MB sinirla — olay mesajlari sisebilir).
func (h *Handlers) vekilGet(w http.ResponseWriter, r *http.Request, yolVeSorgu string) {
	id := chi.URLParam(r, "id")
	adres, jeton, parmakIzi, err := h.ajanBilgi(r, id)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, err.Error())
		return
	}
	istemci := ignelenmisIstemci(parmakIzi)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, "https://"+adres+yolVeSorgu, nil)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "istek kurulamadı")
		return
	}
	req.Header.Set("X-Gosp-Jeton", jeton)
	yanit, err := istemci.Do(req)
	if err != nil {
		if hataParmakIziMi(err) {
			// Okuma yolunda da igne uyusmazligi GUVENLIK olayidir — Sina ile
			// ayni sekilde isaretlenir, sessiz gecilmez.
			_, _ = h.DB.ExecContext(r.Context(),
				`UPDATE windows_ajanlar SET durum='parmak-izi-uyusmazligi' WHERE id=?`, id)
			httpx.WriteError(w, http.StatusBadGateway, errParmakIzi.Error())
			return
		}
		httpx.WriteError(w, http.StatusBadGateway, "ajana erisilemedi: "+err.Error())
		return
	}
	defer yanit.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(yanit.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(yanit.Body, 1<<20))
}

// Olaylar — GET /windows-ajanlar/{id}/olaylar?gunluk=System&adet=50
// Gunluk beyaz listesi PANELDE DE dogrulanir: ajan zaten dogruluyor ama vekil
// keyfi sorgu tasiyicisina donusmemeli (derinlemesine savunma).
func (h *Handlers) Olaylar(w http.ResponseWriter, r *http.Request) {
	gunluk := r.URL.Query().Get("gunluk")
	if gunluk == "" {
		gunluk = "System"
	}
	if gunluk != "System" && gunluk != "Application" && gunluk != "Security" {
		httpx.WriteError(w, http.StatusBadRequest, "gunluk System, Application veya Security olmalı")
		return
	}
	adet := r.URL.Query().Get("adet")
	if adet == "" {
		adet = "50"
	}
	for _, c := range adet {
		if c < '0' || c > '9' {
			httpx.WriteError(w, http.StatusBadRequest, "adet sayı olmalı")
			return
		}
	}
	h.vekilGet(w, r, "/olaylar?gunluk="+gunluk+"&adet="+adet)
}

// Gorevler — GET /windows-ajanlar/{id}/gorevler?tumu=0|1
func (h *Handlers) Gorevler(w http.ResponseWriter, r *http.Request) {
	tumu := "0"
	if r.URL.Query().Get("tumu") == "1" {
		tumu = "1"
	}
	h.vekilGet(w, r, "/gorevler?tumu="+tumu)
}
