package ajan

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"girginospanel/internal/destek/iptal"
	p "girginospanel/internal/destek/protokol"
	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"

	"github.com/go-chi/chi/v5"
)

// Rotalar: "Telegram Destek" panel uçları. RequireAuth grubunun İÇİNDE çağrılır.
// Hepsi yalnız etkileşimli oturuma açıktır (YalnizOturum: API anahtarı bağlantı yönetemez;
// izin tablosunda da KimlikUclari'nda — çift kilit) ve YALNIZ KÖK (admin) içindir.
//
// 🔴 EŞLEŞTİRME BU PANELDE YAPILMAZ (2026-10-04 ürün kararı): sunucu sahibi müşteri
// portalında (app.girginos.io) sunucuyu seçer, aydınlatmayı kabul eder ve merkezden kod
// alır; panel kapalıyken de çalışsın diye. Burada yalnız durum, kök şalteri, bağlantı
// listesi ve iptal vardır. Bayi/hosting-müşteri bu uçlara erişemez.
func (a *Ajan) Rotalar(r chi.Router) {
	g := r.With(middleware.YalnizOturum)
	g.Get("/destek/durum", a.DurumUcu)
	g.Put("/destek/ayar", a.AyarUcu)
	g.Get("/destek/baglantilar", a.BaglantilarUcu)
	g.Post("/destek/baglantilar/{kimlik}/iptal", a.IptalUcu)
}

// eslestirmeURL: eşleştirmenin yapıldığı yer (arayüz yönlendirmesi).
const eslestirmeURL = "https://app.girginos.io"

// bekleyenOmru: eski ('bekliyor') kayıtların onay süresi — süpürge bunları kapatır.
const bekleyenOmru = 30 * time.Minute

type istekKimligi struct {
	uid   int64
	aktor string
}

// istekSahibi: isteği yapan kök yönetici olmalı; bayi ve müşteri 403.
func (a *Ajan) istekSahibi(r *http.Request) (istekKimligi, int, string) {
	uid, aktor := middleware.Aktor(r)
	if c := middleware.ClaimsFrom(r); c != nil && c.Role == "admin" {
		return istekKimligi{uid: uid, aktor: aktor}, 0, ""
	}
	if middleware.ClaimsFrom(r) == nil && middleware.MusteriClaimsFrom(r) == nil {
		return istekKimligi{}, http.StatusUnauthorized, "yetkilendirme gerekli"
	}
	return istekKimligi{}, http.StatusForbidden, "yetkiniz yok"
}

func jsonCoz(r *http.Request, hedef any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 16<<10))
	dec.DisallowUnknownFields()
	return dec.Decode(hedef)
}

func tabloYoksa(w http.ResponseWriter, err error) bool {
	if iptal.TabloYok(err) {
		httpx.WriteErrorKod(w, http.StatusServiceUnavailable, "destek modülü veritabanı göçü uygulanmamış", "destek_goc_yok")
		return true
	}
	return false
}

// ---- GET /destek/durum ----

func (a *Ajan) DurumUcu(w http.ResponseWriter, r *http.Request) {
	_, kod, mesaj := a.istekSahibi(r)
	if kod != 0 {
		httpx.WriteError(w, kod, mesaj)
		return
	}
	ctx := r.Context()
	kok, err := ayarOku(ctx, a.DB, 0)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "ayar okunamadı")
		return
	}
	d := a.DurumAl()
	if a.Yap.Hata != "" && d.SonHata == "" {
		d.SonHata = a.Yap.Hata
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"rol":            p.RolKok,
		"modul_acik":     minDuzey(kok.ModulDuzey) != p.DuzeyKapali,
		"tavan":          Tavan(ctx, a.DB, p.RolKok, 0),
		"merkez_bagli":   a.Bagli(),
		"ayar":           kok,
		"baglanti":       d,
		"eslestirme_url": eslestirmeURL,
	})
}

// ---- PUT /destek/ayar ----

type ayarIstek struct {
	ModulDuzey   *p.Duzey `json:"modul_duzey"`
	BayiDuzey    *p.Duzey `json:"bayi_duzey"`
	MusteriDuzey *p.Duzey `json:"musteri_duzey"`
}

func (a *Ajan) AyarUcu(w http.ResponseWriter, r *http.Request) {
	ik, kod, mesaj := a.istekSahibi(r)
	if kod != 0 {
		httpx.WriteError(w, kod, mesaj)
		return
	}
	var req ayarIstek
	if err := jsonCoz(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek")
		return
	}
	gecerli := func(d *p.Duzey) bool { return d == nil || d.Gecerli() }
	if !gecerli(req.ModulDuzey) || !gecerli(req.BayiDuzey) || !gecerli(req.MusteriDuzey) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz düzey")
		return
	}
	ctx := r.Context()
	mevcut, err := ayarOku(ctx, a.DB, 0)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "ayar okunamadı")
		return
	}
	yeni := mevcut
	if req.ModulDuzey != nil {
		yeni.ModulDuzey = *req.ModulDuzey
	}
	if req.BayiDuzey != nil {
		yeni.BayiDuzey = *req.BayiDuzey
	}
	if req.MusteriDuzey != nil {
		yeni.MusteriDuzey = *req.MusteriDuzey
	}
	if err := ayarYaz(ctx, a.DB, 0, yeni, ik.aktor); err != nil {
		if tabloYoksa(w, err) {
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "ayar kaydedilemedi")
		return
	}
	detay := fmt.Sprintf("modul=%s→%s bayi=%s→%s musteri=%s→%s", mevcut.ModulDuzey, yeni.ModulDuzey,
		mevcut.BayiDuzey, yeni.BayiDuzey, mevcut.MusteriDuzey, yeni.MusteriDuzey)
	httpx.Denetim(a.DB, r, ik.uid, ik.aktor, dAyar, "telegram-destek", detay, 0, true)
	a.Durt()
	// Etkin düzey değişmiş olabilir: bağlı ise aktif bağlantıların durumunu tazele.
	if a.Bagli() {
		go a.tumDurumlariBildir(context.WithoutCancel(ctx))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "ayar": yeni})
}

func (a *Ajan) tumDurumlariBildir(ctx context.Context) {
	bs, err := baglantilarSorgu(ctx, a.DB, `durum='aktif' ORDER BY id LIMIT 1000`)
	if err != nil {
		return
	}
	for _, b := range bs {
		a.durumBildir(ctx, b)
	}
}

// ---- GET /destek/baglantilar ----

type baglantiGorunum struct {
	Kimlik          string  `json:"kimlik"`
	Rol             p.Rol   `json:"rol"`
	TgKullaniciAdi  string  `json:"tg_kullanici_adi"`
	TgKisa          string  `json:"tg_kisa"`
	Durum           string  `json:"durum"`
	Duzey           p.Duzey `json:"duzey"`
	EtkinDuzey      p.Duzey `json:"etkin_duzey"`
	SureGun         int     `json:"sure_gun"`
	Bitis           int64   `json:"bitis"`
	Hedef           string  `json:"hedef"`
	Olusturan       string  `json:"olusturan"`
	Onaylayan       string  `json:"onaylayan"`
	OnayAt          int64   `json:"onay_at"`
	AydinlatmaSurum string  `json:"aydinlatma_surum"`
	SonKullanim     int64   `json:"son_kullanim"`
	IptalNedeni     string  `json:"iptal_nedeni"`
	IptalEden       string  `json:"iptal_eden"`
	IptalAt         int64   `json:"iptal_at"`
	OlusturmaAt     int64   `json:"olusturma_at"`
}

func (a *Ajan) BaglantilarUcu(w http.ResponseWriter, r *http.Request) {
	_, kod, mesaj := a.istekSahibi(r)
	if kod != 0 {
		httpx.WriteError(w, kod, mesaj)
		return
	}
	ctx := r.Context()
	bs, err := baglantilarSorgu(ctx, a.DB, `(durum<>'iptal' OR iptal_at >= NOW() - INTERVAL 30 DAY) ORDER BY id DESC LIMIT 200`)
	if err != nil {
		if tabloYoksa(w, err) {
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "bağlantılar okunamadı")
		return
	}
	out := make([]baglantiGorunum, 0, len(bs))
	for _, b := range bs {
		g := baglantiGorunum{Kimlik: b.Kimlik, Rol: b.Rol, TgKullaniciAdi: b.TgKullaniciAdi, TgKisa: tgKisa(b.TgKullaniciAdi, b.TgUserID),
			Durum: b.Durum, Duzey: b.Duzey, EtkinDuzey: p.DuzeyKapali, SureGun: b.SureGun, Bitis: b.Bitis, Olusturan: b.Olusturan,
			Onaylayan: b.Onaylayan, OnayAt: b.OnayAt, AydinlatmaSurum: b.AydinlatmaSurum, SonKullanim: b.SonKullanim,
			IptalNedeni: b.IptalNedeni, IptalEden: b.IptalEden, IptalAt: b.IptalAt, OlusturmaAt: b.OlusturmaAt}
		if b.Durum == durumAktif {
			g.EtkinDuzey = EtkinDuzey(ctx, a.DB, b.Rol, b.ResellerID, b.Duzey)
		}
		g.Hedef = a.hedefGorunen(ctx, b)
		out = append(out, g)
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"baglantilar": out})
}

// hedefGorunen: listede bağlantının kapsamı (müşteri: alan adı; bayi: bayi adı; kök: "kök").
func (a *Ajan) hedefGorunen(ctx context.Context, b *baglanti) string {
	var ad string
	switch b.Rol {
	case p.RolMusteri:
		_ = a.DB.QueryRowContext(ctx, `SELECT alan_adi FROM domains WHERE id=?`, b.DomainID).Scan(&ad)
	case p.RolBayi:
		_ = a.DB.QueryRowContext(ctx, `SELECT username FROM users WHERE id=?`, b.PanelUserID).Scan(&ad)
	default:
		ad = "kök"
	}
	if ad == "" {
		ad = "#" + strconv.FormatInt(b.DomainID+b.PanelUserID, 10)
	}
	return ad
}

func (a *Ajan) bulGorunur(w http.ResponseWriter, r *http.Request) *baglanti {
	kimlik := chi.URLParam(r, "kimlik")
	if !kimlikDeseniYerel.MatchString(kimlik) {
		httpx.WriteError(w, http.StatusNotFound, "bağlantı bulunamadı")
		return nil
	}
	bs, err := baglantilarSorgu(r.Context(), a.DB, `kimlik=?`, kimlik)
	if err != nil {
		if !tabloYoksa(w, err) {
			httpx.WriteError(w, http.StatusInternalServerError, "bağlantı okunamadı")
		}
		return nil
	}
	if len(bs) != 1 {
		httpx.WriteError(w, http.StatusNotFound, "bağlantı bulunamadı")
		return nil
	}
	return bs[0]
}

// ---- POST /destek/baglantilar/{kimlik}/iptal ----

func (a *Ajan) IptalUcu(w http.ResponseWriter, r *http.Request) {
	ik, kod, mesaj := a.istekSahibi(r)
	if kod != 0 {
		httpx.WriteError(w, kod, mesaj)
		return
	}
	b := a.bulGorunur(w, r)
	if b == nil {
		return
	}
	if b.Durum == durumIptal {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "zaten": true})
		return
	}
	ctx := r.Context()
	if !iptal.Tek(ctx, a.DB, b.ID, p.IptalKullanici, ik.aktor) {
		var guncel string
		if err := a.DB.QueryRowContext(ctx, `SELECT durum FROM destek_baglantilari WHERE id=?`, b.ID).Scan(&guncel); err == nil && guncel == durumIptal {
			httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "zaten": true})
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "iptal edilemedi")
		return
	}
	httpx.Denetim(a.DB, r, ik.uid, ik.aktor, dBaglantiIptal, a.hedefAdi(ctx, b), "neden=kullanici kaynak=panel", 0, true)
	a.temizleIz(b.Kimlik)
	a.bildir(&p.BaglantiIptal{BaglantiID: b.Kimlik, Neden: p.IptalKullanici})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
