package domains

// ftp_ek.go — müşterinin BELİRLİ DİZİNLERE ÖZEL ek FTP hesapları.
//
//	GET    /domains/{id}/ftp-ek                       liste (parola/hash YOK) + azami/kullanım
//	POST   /domains/{id}/ftp-ek                       oluştur {ek, dizin, dizin_olustur, parola?}
//	PUT    /domains/{id}/ftp-ek/{kullanici}           {dizin?, dizin_olustur?, etkin?}
//	PUT    /domains/{id}/ftp-ek/{kullanici}/password  {parola?} (boş → üretilir)
//	DELETE /domains/{id}/ftp-ek/{kullanici}
//
// Kapı: MusteriScope (müşteri yalnız kendi alan adı, bayi kendi, kök hepsi) + işleyicide
// DomainSahibiMi ikinci kilidi. API anahtarı bu uçlara GİREMEZ (izin tablosunda yok →
// varsayılan-reddet; parola ucu ayrıca KimlikUclari'nda). Mevcut otomasyon ucu
// PUT /domains/{id}/ftp-accounts/{kullanici}/password DEĞİŞMEDİ (ek hesaba da uygulanır).
//
// 🔴 Parola: oluşturma/parola yanıtında YALNIZ sunucu ürettiyse BİR KEZ döner; denetime,
// günlüğe ve liste yanıtına HİÇ yazılmaz. Saklama ana hesapla aynı: $6$ crypt.

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"girginospanel/internal/ftpek"
	"girginospanel/internal/gizli"
	"girginospanel/internal/hesaplar"
	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"

	"github.com/go-chi/chi/v5"
)

// ekKapi: ortak giriş — domain id, sahiplik (ikinci kilit), API anahtarı yasağı.
func (h *Handlers) ekKapi(w http.ResponseWriter, r *http.Request) (int64, bool) {
	if middleware.AnahtarFrom(r) != nil || middleware.DestekFrom(r) != nil {
		httpx.WriteError(w, http.StatusForbidden, "bu işlem için yetkiniz yok")
		return 0, false
	}
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if id <= 0 || !middleware.DomainSahibiMi(r, id) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return 0, false
	}
	return id, true
}

// ekHata: ftpek.Hata → HTTP yanıtı (ayrıntı sızdırmaz).
func ekHata(w http.ResponseWriter, err error) {
	var he *ftpek.Hata
	if errors.As(err, &he) {
		httpx.WriteError(w, he.HTTP, he.Msg)
		return
	}
	httpx.WriteError(w, http.StatusInternalServerError, "işlem başarısız")
}

func ekHizSiniri(w http.ResponseWriter, id int64) bool {
	if izin, bekle := ftpek.HizIzin(id); !izin {
		w.Header().Set("Retry-After", strconv.Itoa(int(bekle.Seconds())+1))
		httpx.WriteError(w, http.StatusTooManyRequests, "çok fazla işlem yapıldı, biraz sonra tekrar deneyin")
		return false
	}
	return true
}

func (h *Handlers) ekAlanAdi(id int64) string {
	var alan string
	_ = h.DB.QueryRow(`SELECT alan_adi FROM domains WHERE id=?`, id).Scan(&alan)
	return alan
}

// ekParola: boş → güçlü üretim (uretildi=true); doluysa panel kuralı (>=12, harf+rakam).
func ekParola(girilen string) (pw string, uretildi bool, err error) {
	if girilen == "" {
		pw = apiParolaUret()
		if pw == "" {
			return "", false, &ftpek.Hata{HTTP: http.StatusInternalServerError, Msg: "parola üretilemedi"}
		}
		return pw, true, nil
	}
	if gizli.SifreliMi(girilen) {
		return "", false, &ftpek.Hata{HTTP: http.StatusBadRequest, Msg: "geçersiz parola"}
	}
	if len([]rune(girilen)) > MaksParolaUzunluk {
		return "", false, &ftpek.Hata{HTTP: http.StatusBadRequest, Msg: "parola en çok 128 karakter olmalı"}
	}
	if ok, neden := hesaplar.ParolaGucluMu(girilen); !ok {
		return "", false, &ftpek.Hata{HTTP: http.StatusBadRequest, Msg: neden}
	}
	return girilen, false, nil
}

func ekGovde(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(v); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek gövdesi")
		return false
	}
	return true
}

// FTPEkListe: GET /domains/{id}/ftp-ek
func (h *Handlers) FTPEkListe(w http.ResponseWriter, r *http.Request) {
	id, ok := h.ekKapi(w, r)
	if !ok {
		return
	}
	l, err := ftpek.Listele(r.Context(), h.DB, id)
	if err != nil {
		ekHata(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, l)
}

type ekOlusturIstek struct {
	Ek           string `json:"ek"`
	Dizin        string `json:"dizin"`
	DizinOlustur bool   `json:"dizin_olustur"`
	Parola       string `json:"parola"`
}

// FTPEkOlustur: POST /domains/{id}/ftp-ek
func (h *Handlers) FTPEkOlustur(w http.ResponseWriter, r *http.Request) {
	id, ok := h.ekKapi(w, r)
	if !ok {
		return
	}
	var req ekOlusturIstek
	if !ekGovde(w, r, &req) {
		return
	}
	pw, uretildi, err := ekParola(req.Parola)
	if err != nil {
		ekHata(w, err)
		return
	}
	if !ekHizSiniri(w, id) {
		return
	}
	uid, aktor := middleware.Aktor(r)
	hs, err := ftpek.Olustur(r.Context(), h.DB, ftpek.OlusturGirdi{
		DomainID: id, Ek: req.Ek, Dizin: req.Dizin, DizinOlustur: req.DizinOlustur, Parola: pw})
	if err != nil {
		var he *ftpek.Hata
		if errors.As(err, &he) && he.HTTP >= 500 {
			log.Printf("ftp-ek: oluşturma domain=%d: %v", id, err)
			httpx.DenetimDomain(h.DB, r, uid, aktor, "ftp.ek_olustur", h.ekAlanAdi(id), "kullanici="+strings.ToLower(strings.TrimSpace(req.Ek)), id, false)
		}
		ekHata(w, err)
		return
	}
	httpx.DenetimDomain(h.DB, r, uid, aktor, "ftp.ek_olustur", h.ekAlanAdi(id), "kullanici="+hs.Kullanici+" yol="+hs.Dizin, id, true)
	w.Header().Set("Cache-Control", "no-store")
	govde := map[string]any{"ok": true, "hesap": hs, "uretildi": uretildi}
	if uretildi {
		govde["parola"] = pw // YALNIZ BİR KEZ; hiçbir yerde saklanmaz
	}
	httpx.WriteJSON(w, http.StatusCreated, govde)
}

type ekGuncelleIstek struct {
	Dizin        *string `json:"dizin"`
	DizinOlustur bool    `json:"dizin_olustur"`
	Etkin        *bool   `json:"etkin"`
	Izin         *string `json:"izin"`
}

// FTPEkGuncelle: PUT /domains/{id}/ftp-ek/{kullanici}
func (h *Handlers) FTPEkGuncelle(w http.ResponseWriter, r *http.Request) {
	id, ok := h.ekKapi(w, r)
	if !ok {
		return
	}
	var req ekGuncelleIstek
	if !ekGovde(w, r, &req) {
		return
	}
	if req.Izin != nil && *req.Izin != "rw" {
		httpx.WriteError(w, http.StatusBadRequest, "salt okuma izni henüz desteklenmiyor")
		return
	}
	if !ekHizSiniri(w, id) {
		return
	}
	kullanici := chi.URLParam(r, "kullanici")
	uid, aktor := middleware.Aktor(r)
	hs, err := ftpek.Guncelle(r.Context(), h.DB, ftpek.GuncelleGirdi{
		DomainID: id, Kullanici: kullanici, Dizin: req.Dizin, DizinOlustur: req.DizinOlustur, Etkin: req.Etkin})
	if err != nil {
		ekHata(w, err)
		return
	}
	detay := "kullanici=" + hs.Kullanici + " yol=" + hs.Dizin
	if req.Etkin != nil {
		detay += " etkin=" + strconv.FormatBool(*req.Etkin)
	}
	httpx.DenetimDomain(h.DB, r, uid, aktor, "ftp.ek_guncelle", h.ekAlanAdi(id), detay, id, true)
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "hesap": hs})
}

type ekParolaIstek struct {
	Parola string `json:"parola"`
}

// FTPEkParola: PUT /domains/{id}/ftp-ek/{kullanici}/password
func (h *Handlers) FTPEkParola(w http.ResponseWriter, r *http.Request) {
	id, ok := h.ekKapi(w, r)
	if !ok {
		return
	}
	var req ekParolaIstek
	if r.Body != nil {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil && err.Error() != "EOF" {
			httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek gövdesi")
			return
		}
	}
	pw, uretildi, err := ekParola(req.Parola)
	if err != nil {
		ekHata(w, err)
		return
	}
	if !ekHizSiniri(w, id) {
		return
	}
	kullanici := chi.URLParam(r, "kullanici")
	uid, aktor := middleware.Aktor(r)
	if err := ftpek.ParolaDegistir(r.Context(), h.DB, id, kullanici, pw); err != nil {
		ekHata(w, err)
		return
	}
	// Denetim: kim/hangi hesap; parola ve özeti ASLA yazılmaz.
	httpx.DenetimDomain(h.DB, r, uid, aktor, "ftp.ek_parola", h.ekAlanAdi(id), "kullanici="+kullanici, id, true)
	w.Header().Set("Cache-Control", "no-store")
	govde := map[string]any{"ok": true, "kullanici": kullanici, "uretildi": uretildi}
	if uretildi {
		govde["parola"] = pw
	}
	httpx.WriteJSON(w, http.StatusOK, govde)
}

// FTPEkSil: DELETE /domains/{id}/ftp-ek/{kullanici}
func (h *Handlers) FTPEkSil(w http.ResponseWriter, r *http.Request) {
	id, ok := h.ekKapi(w, r)
	if !ok {
		return
	}
	if !ekHizSiniri(w, id) {
		return
	}
	kullanici := chi.URLParam(r, "kullanici")
	uid, aktor := middleware.Aktor(r)
	if err := ftpek.Sil(r.Context(), h.DB, id, kullanici); err != nil {
		ekHata(w, err)
		return
	}
	httpx.DenetimDomain(h.DB, r, uid, aktor, "ftp.ek_sil", h.ekAlanAdi(id), "kullanici="+kullanici, id, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
