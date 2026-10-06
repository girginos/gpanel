package domains

// api_parola.go — faturalama otomasyonu (WHMCS ChangePassword) için FTP ve DB
// parolası uçları. API anahtarıyla (ftp.parola / db.parola izni) ya da kök/bayi
// oturumuyla çağrılır; bayi YALNIZ kendi hosting hesaplarında (rota: SahipYonetim
// / MusteriScope), kök hepsinde.
//
//	PUT /domains/{id}/ftp/password                          (anahtar dalı; ana FTP)
//	PUT /domains/{id}/ftp-accounts/{kullanici}/password     (FTP hesabı)
//	PUT /domains/{id}/databases/users/{kullanici}/password  (DB kullanıcısı)
//
// Gövde {"password":"..."} ("parola" da kabul). Boş/eksikse sunucu güçlü parola
// üretir ve yanıtta YALNIZ BİR KEZ döner — günlüğe/denetime YAZILMAZ.
//
// 🔴 FTP ve DB parolası BAĞIMSIZ (kullanıcı kararı 2026-10-03): FTP ucu DB'ye, DB
// ucu FTP'ye dokunmaz. Bildirim GÖNDERİLMEZ (kullanıcı kararı); denetim kaydı VAR.
//
// Askıdaki hesap: parola DEĞİŞEBİLİR (yönetici/bayi askıdaki hesabı yönetebilir —
// mevcut askı kuralı yalnız müşteri oturumunu keser). Askı bozulmaz: ftp_accounts.
// status'a dokunulmaz (FTP girişi kapalı kalır), SSH eşitlemesi yalnız status=
// 'active' satırdan okur (askıda no-op), ALTER USER … IDENTIFIED BY MariaDB hesap
// kilidini (ACCOUNT LOCK) kaldırmaz.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"girginospanel/internal/gizli"
	"girginospanel/internal/hesaplar"
	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"

	"github.com/go-chi/chi/v5"
)

// Dikişler (testler canlı MariaDB hesabına / chpasswd'ye dokunmaz).
var (
	apiFTPParolaYaz = hesaplar.FTPHesapParolaDegistir
	apiDBParolaYaz  = hesaplar.DBKullaniciParolaDegistir
	apiSSHEsitle    = hesaplar.SyncSSHPassword
	apiParolaUret   = guvenliParolaUret
)

// MaksParolaUzunluk: üst sınır (kötüye kullanım / crypt girdisi).
const MaksParolaUzunluk = 128

type apiParolaIstek struct {
	Password string `json:"password"`
	Parola   string `json:"parola"`
}

// guvenliParolaUret: panel kuralını (ParolaGucluMu) geçen 20 karakterlik parola.
func guvenliParolaUret() string {
	for i := 0; i < 64; i++ {
		pw := hesaplar.RandomParola(20)
		if pw == "" {
			return ""
		}
		if ok, _ := hesaplar.ParolaGucluMu(pw); ok {
			return pw
		}
	}
	return ""
}

// apiParolaOku: gövdeden parolayı alır ya da üretir. Hata yanıtını kendisi yazar.
func apiParolaOku(w http.ResponseWriter, r *http.Request) (pw string, uretildi bool, ok bool) {
	var req apiParolaIstek
	if r.Body != nil {
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
		if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek gövdesi")
			return "", false, false
		}
	}
	if req.Password != "" && req.Parola != "" && req.Password != req.Parola {
		httpx.WriteError(w, http.StatusBadRequest, "password ve parola alanları farklı")
		return "", false, false
	}
	pw = req.Password
	if pw == "" {
		pw = req.Parola
	}
	if pw == "" {
		pw = apiParolaUret()
		if pw == "" {
			httpx.WriteError(w, http.StatusInternalServerError, "parola üretilemedi")
			return "", false, false
		}
		return pw, true, true
	}
	// 🔴 Başka bir satırın şifreli değeri parola olarak yazılamaz (şifre-çözme
	// kâhini — SetDatabasePassword ile aynı kapı).
	if gizli.SifreliMi(pw) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz parola")
		return "", false, false
	}
	if len([]rune(pw)) > MaksParolaUzunluk {
		httpx.WriteError(w, http.StatusBadRequest, "parola en çok 128 karakter olmalı")
		return "", false, false
	}
	if guclu, neden := hesaplar.ParolaGucluMu(pw); !guclu {
		httpx.WriteError(w, http.StatusBadRequest, neden)
		return "", false, false
	}
	return pw, false, true
}

func apiParolaYanit(w http.ResponseWriter, govde map[string]any, pw string, uretildi bool) {
	w.Header().Set("Cache-Control", "no-store")
	govde["ok"] = true
	govde["uretildi"] = uretildi
	if uretildi {
		govde["password"] = pw // YALNIZ BİR KEZ; hiçbir yerde saklanmaz
	}
	httpx.WriteJSON(w, http.StatusOK, govde)
}

// FTPHesapParola: PUT /domains/{id}/ftp-accounts/{kullanici}/password
func (h *Handlers) FTPHesapParola(w http.ResponseWriter, r *http.Request) {
	h.apiFTPParola(w, r, chi.URLParam(r, "kullanici"))
}

// apiFTPParola: kullanici "" → hostingin ana FTP hesabı (sistem kullanıcısı).
func (h *Handlers) apiFTPParola(w http.ResponseWriter, r *http.Request, kullanici string) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if len(kullanici) > 64 {
		httpx.WriteError(w, http.StatusNotFound, "FTP hesabı bulunamadı")
		return
	}
	pw, uretildi, ok := apiParolaOku(w, r)
	if !ok {
		return
	}
	var alan, sk string
	var isDemo, sshOn int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT alan_adi, sistem_kullanici, is_demo, COALESCE(ssh_erisim,0) FROM domains WHERE id=?`, id).
		Scan(&alan, &sk, &isDemo, &sshOn)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "domain okunamadı")
		return
	}
	if isDemo == 1 {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin FTP parolası değiştirilemez")
		return
	}
	if kullanici == "" {
		kullanici = sk
	}
	uid, aktor := middleware.Aktor(r)
	// Denetim eylemi: API anahtarıyla yapılan değişiklik ayrı kodla (literal —
	// denetim kataloğu tarayıcısı çözebilsin).
	eylem := "ftp.parola"
	if middleware.AnahtarFrom(r) != nil {
		eylem = "ftp.parola_api"
	}
	if err := apiFTPParolaYaz(h.DB, id, kullanici, pw); err != nil {
		if errors.Is(err, hesaplar.ErrFTPHesabiYok) {
			httpx.WriteError(w, http.StatusNotFound, "FTP hesabı bulunamadı")
			return
		}
		log.Printf("api parola: FTP domain=%d kullanici=%q: %v", id, kullanici, err)
		httpx.DenetimDomain(h.DB, r, uid, aktor, eylem, alan, "kullanici="+kullanici, id, false)
		httpx.WriteError(w, http.StatusInternalServerError, "FTP parolası değiştirilemedi")
		return
	}
	// Parola yazıldı: bu FTP hesabının AÇIK oturumları düşer (hata parolayı bozmaz).
	h.ftpOturumlariDusur(r, uid, aktor, alan, kullanici, id)
	// Ana FTP hesabı = hostingin Linux kullanıcısı: SSH açıksa aynı hesabın SSH
	// parolası eşitlenir (FTP parolasının panel sözleşmesi). DB'ye DOKUNULMAZ.
	if kullanici == sk && sshOn == 1 {
		if err := apiSSHEsitle(h.DB, sk); err != nil {
			log.Printf("api parola: SSH eşitleme domain=%d: %v", id, err)
		}
	}
	httpx.DenetimDomain(h.DB, r, uid, aktor, eylem, alan, "kullanici="+kullanici, id, true)
	apiParolaYanit(w, map[string]any{"id": id, "alan_adi": alan, "kullanici": kullanici, "username": kullanici}, pw, uretildi)
}

// DBKullaniciParola: PUT /domains/{id}/databases/users/{kullanici}/password
func (h *Handlers) DBKullaniciParola(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	kullanici := chi.URLParam(r, "kullanici")
	pw, uretildi, ok := apiParolaOku(w, r)
	if !ok {
		return
	}
	var alan string
	var isDemo int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT alan_adi, is_demo FROM domains WHERE id=?`, id).Scan(&alan, &isDemo)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "domain okunamadı")
		return
	}
	if isDemo == 1 {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin DB parolası değiştirilemez")
		return
	}
	uid, aktor := middleware.Aktor(r)
	eylem := "db.parola"
	if middleware.AnahtarFrom(r) != nil {
		eylem = "db.parola_api"
	}
	if err := apiDBParolaYaz(h.DB, id, kullanici, pw); err != nil {
		switch {
		case errors.Is(err, hesaplar.ErrDBKullaniciYok), errors.Is(err, hesaplar.ErrSistemDBHesabi):
			// Sistem hesabı adı da "yok" sayılır (sunucu hesabı yoklanamasın).
			httpx.WriteError(w, http.StatusNotFound, "veritabanı kullanıcısı bulunamadı")
			return
		case strings.HasPrefix(err.Error(), "güvenlik:"):
			log.Printf("api parola: DB domain=%d kullanici=%q reddedildi: %v", id, kullanici, err)
			httpx.DenetimDomain(h.DB, r, uid, aktor, eylem, alan, "kullanici="+kullanici, id, false)
			httpx.WriteError(w, http.StatusConflict, "bu veritabanı kullanıcısının parolası panelden değiştirilemez (yönetici incelemesi gerekli)")
			return
		}
		log.Printf("api parola: DB domain=%d kullanici=%q: %v", id, kullanici, err)
		httpx.DenetimDomain(h.DB, r, uid, aktor, eylem, alan, "kullanici="+kullanici, id, false)
		httpx.WriteError(w, http.StatusInternalServerError, "veritabanı parolası değiştirilemedi")
		return
	}
	httpx.DenetimDomain(h.DB, r, uid, aktor, eylem, alan, "kullanici="+kullanici, id, true)
	apiParolaYanit(w, map[string]any{"id": id, "alan_adi": alan, "db_kullanici": kullanici}, pw, uretildi)
}
