package ajan

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"girginospanel/internal/destek/iptal"
	p "girginospanel/internal/destek/protokol"
	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"

	"github.com/go-chi/chi/v5"
	qrcode "github.com/skip2/go-qrcode"
)

// Rotalar: "Telegram Destek" panel uçları. RequireAuth grubunun İÇİNDE çağrılır.
// Hepsi yalnız etkileşimli oturuma açıktır (YalnizOturum: API anahtarı bot bağlantısı
// kuramaz/onaylayamaz; izin tablosunda da KimlikUclari'nda — çift kilit). Kapsam
// (kök / bayi / müşteri) her uçta istekSahibi'nden kurulur.
func (a *Ajan) Rotalar(r chi.Router) {
	g := r.With(middleware.YalnizOturum)
	g.Get("/destek/durum", a.DurumUcu)
	g.Put("/destek/ayar", a.AyarUcu)
	g.Post("/destek/eslestir", a.EslestirUcu)
	g.Get("/destek/baglantilar", a.BaglantilarUcu)
	g.Post("/destek/baglantilar/{kimlik}/onayla", a.OnaylaUcu)
	g.Post("/destek/baglantilar/{kimlik}/iptal", a.IptalUcu)
}

type istekKimligi struct {
	sahip  sahip
	uid    int64
	aktor  string
	adina  bool // müşteri ADINA oturum (bayi/yönetici) — kimlik işlemi yapamaz
	kapsam int64
}

// istekSahibi: isteği yapanın destek kimliği. Müşteri için domainin bayisi DB'den okunur.
func (a *Ajan) istekSahibi(r *http.Request) (istekKimligi, int, string) {
	uid, aktor := middleware.Aktor(r)
	if c := middleware.ClaimsFrom(r); c != nil {
		switch c.Role {
		case "admin":
			return istekKimligi{sahip: sahip{Rol: p.RolKok, PanelUserID: c.UserID}, uid: uid, aktor: aktor}, 0, ""
		case "reseller":
			return istekKimligi{sahip: sahip{Rol: p.RolBayi, PanelUserID: c.UserID, ResellerID: c.UserID}, uid: uid, aktor: aktor, kapsam: c.UserID}, 0, ""
		}
		return istekKimligi{}, http.StatusForbidden, "yetkiniz yok"
	}
	mc := middleware.MusteriClaimsFrom(r)
	if mc == nil || mc.DomainID <= 0 || mc.FTPHesapID <= 0 {
		return istekKimligi{}, http.StatusUnauthorized, "yetkilendirme gerekli"
	}
	var rid int64
	if err := a.DB.QueryRowContext(r.Context(), `SELECT COALESCE(reseller_id,0) FROM domains WHERE id=?`, mc.DomainID).Scan(&rid); err != nil {
		return istekKimligi{}, http.StatusForbidden, "hesap bulunamadı"
	}
	return istekKimligi{sahip: sahip{Rol: p.RolMusteri, ResellerID: rid, DomainID: mc.DomainID, FTPHesapID: mc.FTPHesapID},
		uid: uid, aktor: aktor, adina: mc.Adina(), kapsam: rid}, 0, ""
}

// gorunurlukKosulu: listede/iptalde görülebilen bağlantılar. Kök: tümü; bayi: kendi +
// müşterileri; müşteri: yalnız kendi FTP hesabının bağlantıları.
func gorunurlukKosulu(s sahip) (string, []any) {
	switch s.Rol {
	case p.RolKok:
		return `1=1`, nil
	case p.RolBayi:
		return `reseller_id=? AND rol IN ('bayi','musteri')`, []any{s.PanelUserID}
	default:
		return `rol='musteri' AND domain_id=? AND ftp_hesap_id=?`, []any{s.DomainID, s.FTPHesapID}
	}
}

// sahibiMi: bağlantıyı bu kimlik mi kurdu (ikinci onayı yalnız sahibi verir).
func (b *baglanti) sahibiMi(s sahip) bool {
	if b.Rol != s.Rol {
		return false
	}
	if s.Rol == p.RolMusteri {
		return b.DomainID == s.DomainID && b.FTPHesapID == s.FTPHesapID
	}
	return b.PanelUserID == s.PanelUserID
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
	ik, kod, mesaj := a.istekSahibi(r)
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
	govde := map[string]any{
		"rol":               ik.sahip.Rol,
		"adina":             ik.adina,
		"modul_acik":        minDuzey(kok.ModulDuzey) != p.DuzeyKapali,
		"tavan":             Tavan(ctx, a.DB, ik.sahip.Rol, ik.sahip.ResellerID),
		"bot_kullanici_adi": a.Yap.BotKullaniciAdi,
		"aydinlatma":        AydinlatmaMetni(),
		"sure_secenekleri":  []int{1, 7, 30},
		"en_uzun_gun":       enUzunSureGun,
		"varsayilan_gun":    varsayilanSure,
		"merkez_bagli":      a.Bagli(),
		"kod_omru_sn":       int(p.EslesmeKoduOmru / time.Second),
	}
	switch ik.sahip.Rol {
	case p.RolKok:
		govde["ayar"] = kok
		d := a.DurumAl()
		if a.Yap.Hata != "" && d.SonHata == "" {
			d.SonHata = a.Yap.Hata
		}
		govde["baglanti"] = d
	case p.RolBayi:
		bayi, err := ayarOku(ctx, a.DB, ik.sahip.PanelUserID)
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "ayar okunamadı")
			return
		}
		govde["ayar"] = map[string]any{"musteri_duzey": bayi.MusteriDuzey}
		// Bayi müşterilerine kökün izin verdiğinden fazlasını veremez (bilgi).
		govde["musteri_ust_sinir"] = minDuzey(kok.ModulDuzey, kok.MusteriDuzey)
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, govde)
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
	if ik.sahip.Rol == p.RolMusteri {
		httpx.WriteError(w, http.StatusForbidden, "yetkiniz yok")
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
	rid := int64(0)
	if ik.sahip.Rol == p.RolBayi {
		if req.ModulDuzey != nil || req.BayiDuzey != nil || req.MusteriDuzey == nil {
			httpx.WriteError(w, http.StatusForbidden, "bayi yalnız müşteri iznini değiştirebilir")
			return
		}
		rid = ik.sahip.PanelUserID
	}
	mevcut, err := ayarOku(ctx, a.DB, rid)
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
	if rid > 0 {
		yeni.ModulDuzey, yeni.BayiDuzey = p.DuzeyKapali, p.DuzeyKapali
	}
	if err := ayarYaz(ctx, a.DB, rid, yeni, ik.aktor); err != nil {
		if tabloYoksa(w, err) {
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "ayar kaydedilemedi")
		return
	}
	detay := fmt.Sprintf("musteri=%s→%s", mevcut.MusteriDuzey, yeni.MusteriDuzey)
	if rid == 0 {
		detay = fmt.Sprintf("modul=%s→%s bayi=%s→%s musteri=%s→%s", mevcut.ModulDuzey, yeni.ModulDuzey,
			mevcut.BayiDuzey, yeni.BayiDuzey, mevcut.MusteriDuzey, yeni.MusteriDuzey)
	}
	httpx.Denetim(a.DB, r, ik.uid, ik.aktor, dAyar, "telegram-destek", detay, ik.kapsam, true)
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

// ---- POST /destek/eslestir ----

type eslestirIstek struct {
	Duzey   p.Duzey `json:"duzey"`
	SureGun int     `json:"sure_gun"`
}

func (a *Ajan) EslestirUcu(w http.ResponseWriter, r *http.Request) {
	ik, kod, mesaj := a.istekSahibi(r)
	if kod != 0 {
		httpx.WriteError(w, kod, mesaj)
		return
	}
	if ik.adina {
		httpx.WriteError(w, http.StatusForbidden, "müşteri adına oturumda Telegram bağlantısı kurulamaz")
		return
	}
	var req eslestirIstek
	if err := jsonCoz(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek")
		return
	}
	if req.Duzey != p.DuzeyOkuma && req.Duzey != p.DuzeyOnarim && req.Duzey != p.DuzeyTam {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz düzey")
		return
	}
	// Süresiz bağlantı YOK (kök dahil): 1..30 gün.
	if req.SureGun < 1 || req.SureGun > enUzunSureGun {
		httpx.WriteErrorKod(w, http.StatusBadRequest, fmt.Sprintf("süre 1-%d gün olmalı; süresiz bağlantı seçilemez", enUzunSureGun), "sure_gecersiz")
		return
	}
	ctx := r.Context()
	if Tavan(ctx, a.DB, ik.sahip.Rol, ik.sahip.ResellerID) == p.DuzeyKapali {
		httpx.WriteErrorKod(w, http.StatusForbidden, "Telegram destek bu hesap için kapalı", string(p.HataDevreDisi))
		return
	}
	k, err := p.EslesmeKoduUret(nil)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "sunucu hatası")
		return
	}
	oz, _ := p.EslesmeKoduOzet(k)
	bitis := a.Saat().Add(p.EslesmeKoduOmru)
	if !a.kodlar.sakla(oz, kodKaydi{sahip: ik.sahip, duzey: req.Duzey, sureGun: req.SureGun, olusturan: ik.aktor, bitis: bitis}) {
		httpx.WriteError(w, http.StatusTooManyRequests, "çok fazla geçerli kod var; birkaç dakika sonra yeniden deneyin")
		return
	}
	bildirildi := a.bildir(&p.KodOzet{KodOzet: oz, SonGecerlilik: bitis.Unix()})
	httpx.Denetim(a.DB, r, ik.uid, ik.aktor, dEslesmeKod, "telegram-destek",
		fmt.Sprintf("düzey=%s gün=%d", req.Duzey, req.SureGun), ik.kapsam, true)
	govde := map[string]any{
		"kod":          k,
		"gorunen":      p.EslesmeKoduGorunen(k),
		"bitis":        bitis.Unix(),
		"gecerlilik":   int(p.EslesmeKoduOmru / time.Second),
		"merkez_bagli": bildirildi,
		"baglanti_url": "",
		"qr":           "",
	}
	if a.Yap.BotKullaniciAdi != "" {
		u := "https://t.me/" + url.PathEscape(a.Yap.BotKullaniciAdi) + "?start=" + k
		govde["baglanti_url"] = u
		if png, err := qrcode.Encode(u, qrcode.Medium, 240); err == nil {
			govde["qr"] = "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, govde)
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
	Benim           bool    `json:"benim"`
}

func (a *Ajan) BaglantilarUcu(w http.ResponseWriter, r *http.Request) {
	ik, kod, mesaj := a.istekSahibi(r)
	if kod != 0 {
		httpx.WriteError(w, kod, mesaj)
		return
	}
	ctx := r.Context()
	kosul, args := gorunurlukKosulu(ik.sahip)
	bs, err := baglantilarSorgu(ctx, a.DB, `(`+kosul+`) AND (durum<>'iptal' OR iptal_at >= NOW() - INTERVAL 30 DAY) ORDER BY id DESC LIMIT 200`, args...)
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
			IptalNedeni: b.IptalNedeni, IptalEden: b.IptalEden, IptalAt: b.IptalAt, OlusturmaAt: b.OlusturmaAt, Benim: b.sahibiMi(ik.sahip)}
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

// ---- POST /destek/baglantilar/{kimlik}/onayla ----

type onayIstek struct {
	AydinlatmaSurum string `json:"aydinlatma_surum"`
	Kabul           bool   `json:"kabul"`
}

func (a *Ajan) bulGorunur(w http.ResponseWriter, r *http.Request, ik istekKimligi) *baglanti {
	kimlik := chi.URLParam(r, "kimlik")
	if !kimlikDeseniYerel.MatchString(kimlik) {
		httpx.WriteError(w, http.StatusNotFound, "bağlantı bulunamadı")
		return nil
	}
	kosul, args := gorunurlukKosulu(ik.sahip)
	bs, err := baglantilarSorgu(r.Context(), a.DB, `kimlik=? AND (`+kosul+`)`, append([]any{kimlik}, args...)...)
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

// OnaylaUcu: İKİNCİ ONAY (her rol, müşteri dahil). Yalnız bağlantıyı kuran kimlik verir;
// KVKK aydınlatma metninin GEÇERLİ sürümü kabul edilmelidir.
func (a *Ajan) OnaylaUcu(w http.ResponseWriter, r *http.Request) {
	ik, kod, mesaj := a.istekSahibi(r)
	if kod != 0 {
		httpx.WriteError(w, kod, mesaj)
		return
	}
	if ik.adina {
		httpx.WriteError(w, http.StatusForbidden, "müşteri adına oturumda Telegram bağlantısı onaylanamaz")
		return
	}
	var req onayIstek
	if err := jsonCoz(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek")
		return
	}
	if !req.Kabul || req.AydinlatmaSurum != aydinlatmaSurum {
		httpx.WriteErrorKod(w, http.StatusBadRequest, "aydınlatma metninin güncel sürümü kabul edilmeli", "aydinlatma")
		return
	}
	b := a.bulGorunur(w, r, ik)
	if b == nil {
		return
	}
	// Başkasının (ör. bayinin müşterisinin) bekleyen bağlantısını onaylamak YOK —
	// görünürlük yetmez, sahiplik gerekir. Varlık sızmasın diye 404.
	if !b.sahibiMi(ik.sahip) {
		httpx.WriteError(w, http.StatusNotFound, "bağlantı bulunamadı")
		return
	}
	if b.Durum != durumBekliyor {
		httpx.WriteError(w, http.StatusConflict, "bağlantı onay beklemiyor")
		return
	}
	ctx := r.Context()
	if a.Saat().Unix()-b.OlusturmaAt >= int64(bekleyenOmru/time.Second) {
		a.otomatikIptal(ctx, b, p.IptalSuresiDoldu)
		httpx.WriteError(w, http.StatusConflict, "onay süresi doldu; yeniden eşleştirin")
		return
	}
	if Tavan(ctx, a.DB, b.Rol, b.ResellerID) == p.DuzeyKapali {
		httpx.WriteErrorKod(w, http.StatusForbidden, "Telegram destek bu hesap için kapalı", string(p.HataDevreDisi))
		return
	}
	kd, err := kimlikIzi(ctx, a.DB, b.Rol, b.DomainID, b.FTPHesapID, b.PanelUserID)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "hesap durumu okunamadı")
		return
	}
	if kd.neden != "" {
		a.otomatikIptal(ctx, b, kd.neden)
		httpx.WriteError(w, http.StatusConflict, "hesap durumu bağlantıya izin vermiyor")
		return
	}
	res, err := a.DB.ExecContext(ctx, `UPDATE destek_baglantilari SET durum='aktif', onaylayan=?, onay_at=CURRENT_TIMESTAMP,
		bitis=CURRENT_TIMESTAMP + INTERVAL sure_gun DAY, aydinlatma_surum=?, aydinlatma_zamani=CURRENT_TIMESTAMP, durum_izi=?, iki_fa=?
		WHERE id=? AND durum='bekliyor'`, kesRune(ik.aktor, 64), aydinlatmaSurum, kd.iz, kd.ikiFA, b.ID)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "onaylanamadı")
		return
	}
	if n, _ := res.RowsAffected(); n != 1 {
		httpx.WriteError(w, http.StatusConflict, "bağlantı onay beklemiyor")
		return
	}
	httpx.Denetim(a.DB, r, ik.uid, ik.aktor, dEslesmeOnay, a.hedefAdi(ctx, b),
		fmt.Sprintf("düzey=%s gün=%d aydınlatma=%s", b.Duzey, b.SureGun, aydinlatmaSurum), ik.kapsam, true)
	if yeni, err := baglantiKimlikle(ctx, a.DB, b.Kimlik); err == nil {
		a.durumBildir(context.WithoutCancel(ctx), yeni)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- POST /destek/baglantilar/{kimlik}/iptal ----

func (a *Ajan) IptalUcu(w http.ResponseWriter, r *http.Request) {
	ik, kod, mesaj := a.istekSahibi(r)
	if kod != 0 {
		httpx.WriteError(w, kod, mesaj)
		return
	}
	b := a.bulGorunur(w, r, ik)
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
	httpx.Denetim(a.DB, r, ik.uid, ik.aktor, dBaglantiIptal, a.hedefAdi(ctx, b), "neden=kullanici kaynak=panel", ik.kapsam, true)
	a.temizleIz(b.Kimlik)
	a.bildir(&p.BaglantiIptal{BaglantiID: b.Kimlik, Neden: p.IptalKullanici})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
