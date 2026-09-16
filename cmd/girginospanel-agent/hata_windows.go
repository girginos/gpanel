//go:build windows

// hata_windows.go — STANDART HATA MODELI (stabilite rehberi #9).
//
// 🔴 NEDEN: eskiden her uc kendi hata sekli + kendi status eslemesini uretiyordu
// ({"hata":string}, uc ayri esleme). Istemci Turkce serbest metni string-eslemek
// zorundaydi ve "yeniden denenebilir mi" bilinmiyordu. Bu dosya TEK zarf + makine
// -okunur kod + korelasyon kimligi (istek_id) + retryable getirir.
//
// 🔴 GERIYE DONUK UYUM: zarf `hata` alanini KORUR — gomulu webui yaniti
// `veri.hata` ile okuyor ve `hata.kod = yanit.status` (HTTP DURUMU) ile 409/422/401
// dallaniyor. Bu yuzden (a) `hata` alani ve (b) HTTP DURUM KODLARI aynen korunur;
// yeni alanlar (kod/istek_id/yeniden) yalniz EKLENIR. Boylece UI kirilmaz.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"girginospanel/internal/platform"
)

// Kod — makine-okunur hata kodu. Retryable'i ve HTTP durumu kodCoz'da sabittir.
type Kod string

const (
	KodGecersizIstek  Kod = "GECERSIZ_ISTEK"  // 400 — cagiran hatasi (retry ETME)
	KodDesteklenmiyor Kod = "DESTEKLENMIYOR"  // 422 — platform yetenegi yok
	KodParolaGerekli  Kod = "PAROLA_GEREKLI"  // 422 — motor parola ayari bekliyor
	KodKorumali       Kod = "KORUMALI"        // 422 — sistem kaynagi, dokunulmaz
	KodKurulumSuruyor Kod = "KURULUM_SURUYOR" // 409 — tek-ucus (retry EDILEBILIR)
	KodKurulumAsili   Kod = "KURULUM_ASILI"   // 409 — asili kurulum, operator temizlemeli
	KodKurulamaz      Kod = "KURULAMAZ"       // 422 — taninmiyor/zaten kurulu
	KodYontem         Kod = "YONTEM_YOK"      // 405
	KodGovde          Kod = "GOVDE_OKUNAMADI" // 400
	KodCSRF           Kod = "CSRF_RED"        // 403 — capraz-kaynak istek (Origin != Host)
	KodIcHata         Kod = "IC_HATA"         // 500 (retry EDILEBILIR)
)

// HataZarfi — TUM hata yanitlarinin tek sozlesmesi.
type HataZarfi struct {
	Kod     Kod    `json:"kod"`
	Mesaj   string `json:"mesaj"`
	Hata    string `json:"hata"`     // = Mesaj; eski istemci uyumlulugu (webui veri.hata)
	IstekID string `json:"istek_id"` // korelasyon: yanit basligi + log ile ayni
	Yeniden bool   `json:"yeniden"`  // retryable
}

// istekIDBaslik — yanit + log korelasyonu icin baslik adi (istemci gonderirse korunur).
const istekIDBaslik = "X-Istek-Id"

// istekID — istegin korelasyon kimligi: basliktan gelirse o, yoksa 8 bayt uret.
func istekID(r *http.Request) string {
	if r != nil {
		if v := r.Header.Get(istekIDBaslik); v != "" {
			return v
		}
	}
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "0000000000000000"
	}
	return hex.EncodeToString(b)
}

// kodCoz — platform sentinel'ini (kod, HTTP durum, retryable) uclusune cevirir.
// 🔴 Sentinel'lerin HTTP durumlari SABIT ve webui'nin bagli oldugu degerlerdir
// (409 kurulum-suruyor, 422 parola/korumali) — DEGISTIRME. Sentinel disi hata
// cagiranin verdigi varsayilanDurum'u alir (okuma ucu 500, mutasyon 422 gibi).
func kodCoz(err error, varsayilanDurum int) (Kod, int, bool) {
	switch {
	case errors.Is(err, platform.ErrGecersizIstek):
		return KodGecersizIstek, http.StatusBadRequest, false
	case errors.Is(err, platform.ErrVeriParolaGerekli):
		return KodParolaGerekli, http.StatusUnprocessableEntity, false
	case errors.Is(err, platform.ErrVeriKorumali):
		return KodKorumali, http.StatusUnprocessableEntity, false
	case errors.Is(err, platform.ErrKurulumSuruyor):
		return KodKurulumSuruyor, http.StatusConflict, true
	case errors.Is(err, platform.ErrKurulumAsili):
		return KodKurulumAsili, http.StatusConflict, false // 409, temizlik gerekli (kor retry ETME)
	case errors.Is(err, platform.ErrKurulamaz):
		return KodKurulamaz, http.StatusUnprocessableEntity, false
	case errors.Is(err, platform.ErrDesteklenmiyor):
		return KodDesteklenmiyor, http.StatusUnprocessableEntity, false
	default:
		return KodIcHata, varsayilanDurum, varsayilanDurum >= 500
	}
}

// zarfYaz — hata zarfini yazar + loglar (istek_id ile korelasyon). r nil olabilir.
func zarfYaz(w http.ResponseWriter, r *http.Request, durum int, kod Kod, mesaj string, yeniden bool) {
	id := istekID(r)
	yontem, yol := "-", "-"
	if r != nil {
		yontem, yol = r.Method, r.URL.Path
	}
	log.Printf("[%s] %s %s -> %d %s: %s", id, yontem, yol, durum, kod, mesaj)
	w.Header().Set(istekIDBaslik, id)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(durum)
	_ = json.NewEncoder(w).Encode(HataZarfi{Kod: kod, Mesaj: mesaj, Hata: mesaj, IstekID: id, Yeniden: yeniden})
}

// hataYaz — TEK hata yazici: sentinel'den kod+durum+retryable turetir; sentinel
// disi hata varsayilanDurum'u alir. Tum uclarin ortak hata yolu.
func hataYaz(w http.ResponseWriter, r *http.Request, varsayilanDurum int, err error) {
	kod, durum, yeniden := kodCoz(err, varsayilanDurum)
	zarfYaz(w, r, durum, kod, err.Error(), yeniden)
}

// yontemHatasi — 405 (yanlis HTTP metodu) ortak zarfla.
func yontemHatasi(w http.ResponseWriter, r *http.Request) {
	zarfYaz(w, r, http.StatusMethodNotAllowed, KodYontem, "yontem desteklenmiyor", false)
}

// govdeHatasi — 400 (JSON govde cozulemedi) ortak zarfla.
func govdeHatasi(w http.ResponseWriter, r *http.Request) {
	zarfYaz(w, r, http.StatusBadRequest, KodGovde, "govde okunamadi", false)
}

// ── denetim + korelasyon (B-09) ──────────────────────────────────────────────

// durumYakala — yanit durum kodunu yakalar (denetim logu icin). WriteHeader hic
// cagrilmazsa net/http varsayilani 200'dur; kod da 200 baslar.
type durumYakala struct {
	http.ResponseWriter
	kod int
}

func (d *durumYakala) WriteHeader(k int) {
	d.kod = k
	d.ResponseWriter.WriteHeader(k)
}

// denetimli — ayricalikli (mutasyon) ucu sarar (denetim 04/5): her istege
// korelasyon kimligi (istek_id) atar, hem YANIT basligina hem baslangic+sonuc
// loguna yazar; boylece "KIM (aktor RemoteAddr) ne zaman NEYI (ad) yapti, sonuc
// (durum) ne oldu" iz birakir ve istek handler→platform→komut boyunca ayni id
// ile izlenebilir. r.Header'a id yazilir ki hata zarfi (zarfYaz) AYNI id'yi
// kullansin. Yalniz durum-degistiren uclara uygulanir; okuma uclari gurultu
// yapmasin diye sarilmaz.
// csrfKontrol — CSRF derinlemesine savunma (denetim 04/6). SameSite=Strict tek
// katmandi; 0.0.0.0 bind'de ayni-host-farkli-port bir sayfa "same-site" sayilip
// cerezi tasiyabiliyordu. Cozum: mutasyon istegi bir Origin/Referer TASIYORSA,
// host'u istegin Host basligiyla ESLESMELI (hangi host'tan erisilirse erisilsin
// dogru calisir). Origin/Referer YOKSA (tarayici disi arac) SameSite + oturum
// yeterli kabul edilir. Tarayici, ayni-origin POST/DELETE'te Origin gonderir;
// saldirganin sayfasi FARKLI origin gonderir → yakalanir.
func csrfKontrol(r *http.Request) error {
	kaynak := r.Header.Get("Origin")
	if kaynak == "" {
		kaynak = r.Header.Get("Referer")
	}
	if kaynak == "" {
		return nil // tarayici degil (arac); SameSite + oturum yeterli
	}
	u, err := url.Parse(kaynak)
	if err != nil || u.Host == "" {
		return fmt.Errorf("gecersiz Origin/Referer basligi")
	}
	if !strings.EqualFold(u.Host, r.Host) {
		return fmt.Errorf("capraz-kaynak istek reddedildi (origin %q != host %q)", u.Host, r.Host)
	}
	return nil
}

func denetimli(ad string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := istekID(r)
		r.Header.Set(istekIDBaslik, id)
		w.Header().Set(istekIDBaslik, id)
		// 🔴 CSRF kapisi (B-10): capraz-kaynak mutasyonu h'ye ULASMADAN reddet.
		if err := csrfKontrol(r); err != nil {
			log.Printf("[%s] DENETIM %s CSRF-RED aktor=%s: %v", id, ad, r.RemoteAddr, err)
			zarfYaz(w, r, http.StatusForbidden, KodCSRF, err.Error(), false)
			return
		}
		dy := &durumYakala{ResponseWriter: w, kod: http.StatusOK}
		basla := time.Now()
		log.Printf("[%s] DENETIM %s BASLADI aktor=%s %s %s", id, ad, r.RemoteAddr, r.Method, r.URL.Path)
		h(dy, r)
		log.Printf("[%s] DENETIM %s BITTI durum=%d sure=%v", id, ad, dy.kod, time.Since(basla).Round(time.Millisecond))
	}
}
