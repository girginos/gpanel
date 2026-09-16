//go:build windows

// hesap_windows.go — YEREL PANEL HESAPLARI: Admin / Reseller (bayi) / Hosting
// cok-katmanli yapinin Windows karsiligi (Linux paneldeki users tablosunun esi).
//
// 🔴 ADMIN buraya GIRMEZ: admin kimligi ajan.json PanelParolaHash'te kalir —
// `panel-parola` CLI kurtarma yolu ve mevcut giris davranisi AYNEN korunur
// (geriye uyum + kilitlenme riski yok). Bu depo YALNIZ reseller + hosting tutar.
//
// Depolama: C:\ProgramData\girginospanel\hesaplar.json (dizin ACL'i E duzeltmesiyle
// SYSTEM+Administrators'a kilitli → dosya miras alir). Atomik yaz (temp+rename).
package platform

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// hesaplarYolu — var (const degil) ki birim testi gecici dizine yonlendirebilsin.
var hesaplarYolu = `C:\ProgramData\girginospanel\hesaplar.json`

// Roller. Admin ajan.json'da; depo yalniz reseller/hosting saklar ama Rol sabitleri
// (oturum/rol-kapsam icin) burada merkezidir.
const (
	RolAdmin    = "admin"
	RolReseller = "reseller"
	RolHosting  = "hosting"
)

// kullaniciAdiRe — hesap kullanici adi: kucuk harf/rakam/._- , 3..32. "admin"
// ayrilmistir (ajan.json'daki tek admin ile cakismasin).
var kullaniciAdiRe = regexp.MustCompile(`^[a-z0-9._-]{3,32}$`)

// Hesap — tek reseller/hosting hesabi. ParolaHash cagirana DONERKEN siyrilir
// (HesaplariGetir/HesapDogrula bosaltir); diske omomit degil, tam yazilir.
type Hesap struct {
	KullaniciAdi  string `json:"kullaniciAdi"`
	ParolaHash    string `json:"parolaHash,omitempty"`
	Rol           string `json:"rol"`           // reseller | hosting
	BayiKullanici string `json:"bayiKullanici"` // hosting'in bagli oldugu reseller (bos = admin altinda)
	Durum         string `json:"durum"`         // aktif | askida
	AdSoyad       string `json:"adSoyad"`
	Eposta        string `json:"eposta"`
	MaxSite       int    `json:"maxSite"`    // reseller kotasi (0 = sinirsiz)
	MaxDiskMB     int64  `json:"maxDiskMB"`  // reseller kotasi (0 = sinirsiz)
	FazlaSatis    bool   `json:"fazlaSatis"` // reseller sahip oldugundan fazla satabilir mi
	Olusturulma   string `json:"olusturulma"`
}

type hesapDosya struct {
	Hesaplar []Hesap `json:"hesaplar"`
}

func hesapOku() (hesapDosya, error) {
	d := hesapDosya{Hesaplar: []Hesap{}}
	b, err := os.ReadFile(hesaplarYolu)
	if err != nil {
		if os.IsNotExist(err) {
			return d, nil
		}
		return d, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return d, nil
	}
	if err := json.Unmarshal(b, &d); err != nil {
		return d, fmt.Errorf("hesaplar.json bozuk: %w", err)
	}
	if d.Hesaplar == nil {
		d.Hesaplar = []Hesap{}
	}
	return d, nil
}

func hesapYaz(d hesapDosya) error {
	if err := os.MkdirAll(filepath.Dir(hesaplarYolu), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	gecici := hesaplarYolu + ".yeni"
	if err := os.WriteFile(gecici, b, 0o600); err != nil {
		return err
	}
	return os.Rename(gecici, hesaplarYolu)
}

func hesapBulInd(d hesapDosya, kullanici string) int {
	for i := range d.Hesaplar {
		if strings.EqualFold(d.Hesaplar[i].KullaniciAdi, kullanici) {
			return i
		}
	}
	return -1
}

// RolGecerli — depoya yazilabilir rol (admin HARIÇ; admin ajan.json'da).
func RolGecerli(r string) bool { return r == RolReseller || r == RolHosting }

func kullaniciDogrula(k string) error {
	if k == "admin" {
		return fmt.Errorf("'admin' ayrilmis kullanici adi: %w", ErrGecersizIstek)
	}
	if !kullaniciAdiRe.MatchString(k) {
		return fmt.Errorf("kullanici adi gecersiz (3-32 kucuk harf/rakam/._-): %w", ErrGecersizIstek)
	}
	return nil
}

// HesaplariGetir — tum reseller/hosting hesaplari, ParolaHash SIYRILMIS.
func HesaplariGetir() ([]Hesap, error) {
	d, err := hesapOku()
	if err != nil {
		return nil, err
	}
	out := make([]Hesap, len(d.Hesaplar))
	for i, h := range d.Hesaplar {
		h.ParolaHash = ""
		out[i] = h
	}
	return out, nil
}

// HesapGetir — tek hesap (ParolaHash siyrilmis); bulunmazsa ok=false.
func HesapGetir(kullanici string) (Hesap, bool) {
	d, err := hesapOku()
	if err != nil {
		return Hesap{}, false
	}
	i := hesapBulInd(d, strings.TrimSpace(strings.ToLower(kullanici)))
	if i < 0 {
		return Hesap{}, false
	}
	h := d.Hesaplar[i]
	h.ParolaHash = ""
	return h, true
}

// HesapDogrula — kullanici+parola dogrular. Aktif hesap + bcrypt eslesmesi ise
// hesabi (ParolaHash siyrilmis) + true doner. Askidaki/olmayan/yanlis → false.
// 🔴 admin BURADA yok — cagiran once admin'i ajan.json ile dener.
func HesapDogrula(kullanici, parola string) (Hesap, bool) {
	kullanici = strings.TrimSpace(strings.ToLower(kullanici))
	d, err := hesapOku()
	if err != nil {
		return Hesap{}, false
	}
	i := hesapBulInd(d, kullanici)
	if i < 0 {
		return Hesap{}, false
	}
	h := d.Hesaplar[i]
	if h.Durum != "aktif" {
		return Hesap{}, false
	}
	if bcrypt.CompareHashAndPassword([]byte(h.ParolaHash), []byte(parola)) != nil {
		return Hesap{}, false
	}
	h.ParolaHash = ""
	return h, true
}

// HesapOlustur — yeni reseller/hosting hesabi (parola DUZ gelir, burada hashlenir).
// Ayni kullanici adi varsa reddeder. BayiKullanici (hosting icin) bos gecilebilir.
func HesapOlustur(h Hesap, parola string) error {
	h.KullaniciAdi = strings.TrimSpace(strings.ToLower(h.KullaniciAdi))
	if err := kullaniciDogrula(h.KullaniciAdi); err != nil {
		return err
	}
	if !RolGecerli(h.Rol) {
		return fmt.Errorf("gecersiz rol %q (reseller|hosting): %w", h.Rol, ErrGecersizIstek)
	}
	if len(parola) < 8 || len(parola) > 200 {
		return fmt.Errorf("parola 8-200 karakter olmali: %w", ErrGecersizIstek)
	}
	if h.MaxSite < 0 || h.MaxDiskMB < 0 {
		return fmt.Errorf("negatif kota olamaz: %w", ErrGecersizIstek)
	}
	d, err := hesapOku()
	if err != nil {
		return err
	}
	if hesapBulInd(d, h.KullaniciAdi) >= 0 {
		return fmt.Errorf("kullanici adi zaten var: %q: %w", h.KullaniciAdi, ErrGecersizIstek)
	}
	// hosting bir bayiye baglanacaksa o bayi var + reseller olmali.
	if h.Rol == RolHosting && h.BayiKullanici != "" {
		bi := hesapBulInd(d, strings.ToLower(h.BayiKullanici))
		if bi < 0 || d.Hesaplar[bi].Rol != RolReseller {
			return fmt.Errorf("bayi bulunamadi veya reseller degil: %q: %w", h.BayiKullanici, ErrGecersizIstek)
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(parola), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("parola hashlenemedi: %w", err)
	}
	h.ParolaHash = string(hash)
	if h.Durum == "" {
		h.Durum = "aktif"
	}
	d.Hesaplar = append(d.Hesaplar, h)
	return hesapYaz(d)
}

// HesapGuncelle — mevcut hesabin meta/kota alanlarini gunceller (parola HARIÇ,
// rol HARIÇ). Bulunmazsa hata.
func HesapGuncelle(kullanici string, adSoyad, eposta, durum string, maxSite int, maxDiskMB int64, fazlaSatis bool) error {
	kullanici = strings.TrimSpace(strings.ToLower(kullanici))
	if durum != "aktif" && durum != "askida" {
		return fmt.Errorf("gecersiz durum %q (aktif|askida): %w", durum, ErrGecersizIstek)
	}
	if maxSite < 0 || maxDiskMB < 0 {
		return fmt.Errorf("negatif kota olamaz: %w", ErrGecersizIstek)
	}
	d, err := hesapOku()
	if err != nil {
		return err
	}
	i := hesapBulInd(d, kullanici)
	if i < 0 {
		return fmt.Errorf("hesap bulunamadi: %q: %w", kullanici, ErrGecersizIstek)
	}
	d.Hesaplar[i].AdSoyad = adSoyad
	d.Hesaplar[i].Eposta = eposta
	d.Hesaplar[i].Durum = durum
	d.Hesaplar[i].MaxSite = maxSite
	d.Hesaplar[i].MaxDiskMB = maxDiskMB
	d.Hesaplar[i].FazlaSatis = fazlaSatis
	return hesapYaz(d)
}

// HesapParolaDegistir — hesabin parolasini yeniler (yeni parola DUZ gelir).
func HesapParolaDegistir(kullanici, yeniParola string) error {
	kullanici = strings.TrimSpace(strings.ToLower(kullanici))
	if len(yeniParola) < 8 || len(yeniParola) > 200 {
		return fmt.Errorf("parola 8-200 karakter olmali: %w", ErrGecersizIstek)
	}
	d, err := hesapOku()
	if err != nil {
		return err
	}
	i := hesapBulInd(d, kullanici)
	if i < 0 {
		return fmt.Errorf("hesap bulunamadi: %q: %w", kullanici, ErrGecersizIstek)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(yeniParola), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("parola hashlenemedi: %w", err)
	}
	d.Hesaplar[i].ParolaHash = string(hash)
	return hesapYaz(d)
}

// HesapSil — hesabi siler. Bir RESELLER siliniyorsa ve altinda hosting varsa
// reddeder (once hostingler tasinmali/silinmeli).
func HesapSil(kullanici string) error {
	kullanici = strings.TrimSpace(strings.ToLower(kullanici))
	d, err := hesapOku()
	if err != nil {
		return err
	}
	i := hesapBulInd(d, kullanici)
	if i < 0 {
		return fmt.Errorf("hesap bulunamadi: %q: %w", kullanici, ErrGecersizIstek)
	}
	if d.Hesaplar[i].Rol == RolReseller {
		for _, h := range d.Hesaplar {
			if h.Rol == RolHosting && strings.EqualFold(h.BayiKullanici, kullanici) {
				return fmt.Errorf("bayinin altinda hosting var (%s); once onlari kaldirin: %w", h.KullaniciAdi, ErrGecersizIstek)
			}
		}
	}
	d.Hesaplar = append(d.Hesaplar[:i], d.Hesaplar[i+1:]...)
	return hesapYaz(d)
}
