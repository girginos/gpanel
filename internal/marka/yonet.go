package marka

// yonet.go — markayı YÖNETEN uçlar (AdminVeyaReseller / AdminOnly).
//
// 🔴 SAHİP HİÇBİR ZAMAN İSTEKTEN ALINMAZ: admin jetonu → kök markası (dosya);
// bayi jetonu → ResellerIDFrom (kendi satırı). Gövde DisallowUnknownFields ile
// çözülür: `reseller_id` ya da bilinmeyen alan → 400; sorgu parametreleri hiç
// okunmaz. Kimlik taşıyan {id} yolları yalnız AdminOnly (moderasyon).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
)

// MarkaYama — kısmi güncelleme. nil alan DEĞİŞMEZ. Görsel bayrakları YOK
// (yalnız görsel uçları belirler).
type MarkaYama struct {
	PanelAdi      *string `json:"panel_adi"`
	Baslik        *string `json:"baslik"`
	KisaAd        *string `json:"kisa_ad"`
	VurguRenk     *string `json:"vurgu_renk"`
	GirisBaslik   *string `json:"giris_baslik"`
	GirisAlt      *string `json:"giris_alt"`
	GirisDipnot   *string `json:"giris_dipnot"`
	Footer        *string `json:"footer"`
	SurumGoster   *bool   `json:"surum_goster"`
	DestekURL     *string `json:"destek_url"`
	DestekEposta  *string `json:"destek_eposta"`
	DestekTelefon *string `json:"destek_telefon"`
	WebmailMarka  *bool   `json:"webmail_marka"`
	BannerMetin   *string `json:"banner_metin"`
	BannerURL     *string `json:"banner_url"`
}

// Uygula — yamayı markanın üzerine bindirir.
func (y MarkaYama) Uygula(m Marka) Marka {
	s := func(h *string, p *string) {
		if p != nil {
			*h = *p
		}
	}
	s(&m.PanelAdi, y.PanelAdi)
	s(&m.Baslik, y.Baslik)
	s(&m.KisaAd, y.KisaAd)
	s(&m.VurguRenk, y.VurguRenk)
	s(&m.GirisBaslik, y.GirisBaslik)
	s(&m.GirisAlt, y.GirisAlt)
	s(&m.GirisDipnot, y.GirisDipnot)
	s(&m.Footer, y.Footer)
	s(&m.DestekURL, y.DestekURL)
	s(&m.DestekEposta, y.DestekEposta)
	s(&m.DestekTelefon, y.DestekTelefon)
	s(&m.BannerMetin, y.BannerMetin)
	s(&m.BannerURL, y.BannerURL)
	if y.SurumGoster != nil {
		m.SurumGoster = *y.SurumGoster
	}
	if y.WebmailMarka != nil {
		m.WebmailMarka = *y.WebmailMarka
	}
	return m
}

// Dogrula — kullanıcıya dönük hata (hangi alan, neden). Geçerliyse Normalize'lı.
func Dogrula(m Marka) (Marka, string) {
	if metinKirp(m.PanelAdi, 40) == "" {
		return m, "Panel adı boş olamaz."
	}
	if !RenkGecerli(m.VurguRenk) {
		return m, "Vurgu rengi #rrggbb biçiminde olmalı (örn. #2563eb)."
	}
	if !URLGecerli(m.DestekURL) {
		return m, "Destek adresi http:// veya https:// ile başlamalı."
	}
	if !URLGecerli(m.BannerURL) {
		return m, "Bant bağlantısı http:// veya https:// ile başlamalı."
	}
	if !EpostaGecerli(m.DestekEposta) {
		return m, "Destek e-postası geçerli bir adres olmalı (örn. destek@ornek.com)."
	}
	if !TelefonGecerli(m.DestekTelefon) {
		return m, "Destek telefonu yalnız rakam, boşluk, ( ) - ve baştaki + içerebilir (5-20 karakter)."
	}
	return Normalize(m), ""
}

// yonetGorunum — yönetim ekranının okuduğu yanıt.
type yonetGorunum struct {
	Sahip       string `json:"sahip"` // "kok" | "bayi"
	Anahtar     string `json:"anahtar"`
	Kayitli     bool   `json:"kayitli"`
	Surum       int64  `json:"surum"`
	KokKapatti  bool   `json:"kok_kapatti"`
	AlanadiIzni bool   `json:"alanadi_izni"`
	Marka       Marka  `json:"marka"`
	// MusteriAdresi — müşteri kapısının kökü (kapı modu kapalıyken boş → aynı
	// köken). Bayi markalı müşteri giriş bağlantısını (/cp/giris?m=…) bununla kurar.
	MusteriAdresi string `json:"musteri_adresi,omitempty"`
}

// MusteriAdresKanca — müşteri kapısı adresi (main.go: musteriKapiAdresi).
var MusteriAdresKanca func(r *http.Request) (string, bool)

func kokGorunum() (yonetGorunum, error) {
	m, err := KokOkuYonetim()
	return yonetGorunum{Sahip: "kok", Anahtar: KokAnahtar, Kayitli: true, Marka: m}, err
}

func bayiGorunum(k BayiKayit) yonetGorunum {
	m := k.Ayar
	if !k.Kayitli {
		// Henüz kaydedilmemiş: form nötr değerlerle açılır (kökün iletişim
		// bilgisi bayinin formuna kopyalanmasın); görseller diskten.
		m = Notr()
		m.Logo, m.LogoSurum = k.Ayar.Logo, k.Ayar.LogoSurum
		m.Banner, m.BannerSurum = k.Ayar.Banner, k.Ayar.BannerSurum
		m.Favicon, m.FaviconSurum = k.Ayar.Favicon, k.Ayar.FaviconSurum
	}
	m.WebmailMarka = false
	return yonetGorunum{Sahip: "bayi", Anahtar: k.Anahtar, Kayitli: k.Kayitli, Surum: k.Surum,
		KokKapatti: k.KokKapatti, AlanadiIzni: k.AlanadiIzni, Marka: m}
}

const izinKapaliMesaj = "Marka ayarları bu hesap için kapalı. Hizmet sağlayıcınızla iletişime geçin."

// sahip — jetondan: (rid, true)=bayi; (0, true)=kök; false=yetkisiz (yazıldı).
func sahip(w http.ResponseWriter, r *http.Request) (int64, bool) {
	c := middleware.ClaimsFrom(r)
	switch {
	case c != nil && c.Role == "admin":
		return 0, true
	case c != nil && c.Role == "reseller" && c.ResellerID > 0:
		return c.ResellerID, true
	}
	yazHata(w, http.StatusForbidden, "yetkiniz yok")
	return 0, false
}

// bayiYetkili — bayinin satırı (taze, önbelleksiz) + izin kontrolü.
func (h *Handlers) bayiYetkili(w http.ResponseWriter, r *http.Request, rid int64) (BayiKayit, bool) {
	k, err := bayiOkuDB(r.Context(), h.DB, rid)
	if err != nil {
		log.Printf("marka: bayi kaydı okunamadı (rid=%d): %v", rid, err)
		yazHata(w, http.StatusInternalServerError, "Marka ayarları okunamadı.")
		return k, false
	}
	if !k.Izin {
		yazHata(w, http.StatusForbidden, izinKapaliMesaj)
		return k, false
	}
	return k, true
}

func (h *Handlers) denetim(r *http.Request, eylem, hedef, detay string, kapsam int64, ok bool) {
	uid, kul := middleware.Aktor(r)
	httpx.Denetim(h.DB, r, uid, kul, eylem, hedef, detay, kapsam, ok)
}

// YonetGetir — GET /marka/yonet
func (h *Handlers) YonetGetir(w http.ResponseWriter, r *http.Request) {
	rid, ok := sahip(w, r)
	if !ok {
		return
	}
	if rid == 0 {
		g, err := kokGorunum()
		if err != nil {
			log.Printf("marka: kök markası okunamadı: %v", err)
		}
		httpx.WriteJSON(w, http.StatusOK, g)
		return
	}
	k, ok := h.bayiYetkili(w, r, rid)
	if !ok {
		return
	}
	g := bayiGorunum(k)
	if MusteriAdresKanca != nil {
		g.MusteriAdresi, _ = MusteriAdresKanca(r)
	}
	httpx.WriteJSON(w, http.StatusOK, g)
}

type yonetGovde struct {
	MarkaYama
	Surum *int64 `json:"surum"`
}

func govdeCoz(r *http.Request, hedef any) error {
	d := json.NewDecoder(io.LimitReader(r.Body, 64*1024))
	d.DisallowUnknownFields()
	if err := d.Decode(hedef); err != nil {
		return err
	}
	if d.More() {
		return errors.New("fazla veri")
	}
	return nil
}

// ErrSurum — iyimser eşzamanlılık çakışması.
var ErrSurum = errors.New("surum")

// bayiKaydet — bayi markasını yazar (etkin=1). beklenen: istemcinin gördüğü sürüm.
func bayiKaydet(ctx context.Context, db *sql.DB, k BayiKayit, m Marka, beklenen int64) error {
	m = Normalize(m)
	m.WebmailMarka = false
	m.Logo, m.LogoSurum, m.Banner, m.BannerSurum, m.Favicon, m.FaviconSurum = false, 0, false, 0, false, 0
	ham, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if !k.SatirVar {
		an, err := YeniAnahtar()
		if err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, `INSERT INTO cp_marka (reseller_id, anahtar, ayar_json, surum, etkin)
			VALUES (?, ?, ?, 1, 1)`, k.RID, an, string(ham))
		if err != nil {
			return ErrSurum // eşzamanlı ilk kayıt (PK çakışması) → istemci tazelesin
		}
		bayiDusur(k)
		return nil
	}
	res, err := db.ExecContext(ctx, `UPDATE cp_marka SET ayar_json=?, etkin=1, surum=surum+1
		WHERE reseller_id=? AND surum=?`, string(ham), k.RID, beklenen)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrSurum
	}
	bayiDusur(k)
	return nil
}

// bayiSatirGarantile — görsel yüklemesi için satır (ve anahtar) yoksa açar (etkin=0).
func bayiSatirGarantile(ctx context.Context, db *sql.DB, k BayiKayit) (BayiKayit, error) {
	if k.SatirVar {
		return k, nil
	}
	an, err := YeniAnahtar()
	if err != nil {
		return k, err
	}
	if _, err := db.ExecContext(ctx, `INSERT IGNORE INTO cp_marka (reseller_id, anahtar, ayar_json, surum, etkin)
		VALUES (?, ?, '{}', 1, 0)`, k.RID, an); err != nil {
		return k, err
	}
	bayiDusur(k)
	return bayiOkuDB(ctx, db, k.RID)
}

// YonetKaydet — PUT /marka/yonet
func (h *Handlers) YonetKaydet(w http.ResponseWriter, r *http.Request) {
	rid, ok := sahip(w, r)
	if !ok {
		return
	}
	var g yonetGovde
	if err := govdeCoz(r, &g); err != nil {
		yazHata(w, http.StatusBadRequest, "geçersiz istek gövdesi")
		return
	}
	if rid == 0 {
		yazKilidi.Lock()
		defer yazKilidi.Unlock()
		mevcut, err := KokOkuYonetim()
		if err != nil {
			// 🔴 Okunamayan dosyanın üstüne varsayılanla yazmak markayı SİLERDİ.
			log.Printf("marka: kök okunamadı, yazma reddedildi: %v", err)
			yazHata(w, http.StatusInternalServerError, "Mevcut marka okunamadı; kayıt yapılmadı.")
			return
		}
		m, hata := Dogrula(g.Uygula(mevcut))
		if hata != "" {
			yazHata(w, http.StatusBadRequest, hata)
			return
		}
		if _, err := kokYazKilitli(m); err != nil {
			log.Printf("marka: kök yazılamadı: %v", err)
			h.denetim(r, "marka.guncelle", "kok", "", 0, false)
			yazHata(w, http.StatusInternalServerError, "Marka kaydedilemedi.")
			return
		}
		h.denetim(r, "marka.guncelle", "kok", "", 0, true)
		gor, _ := kokGorunum()
		httpx.WriteJSON(w, http.StatusOK, gor)
		return
	}
	if g.WebmailMarka != nil {
		yazHata(w, http.StatusBadRequest, "Webmail markası yalnız panel yöneticisi tarafından ayarlanır.")
		return
	}
	k, ok := h.bayiYetkili(w, r, rid)
	if !ok {
		return
	}
	temel := k.Ayar
	if !k.Kayitli {
		temel = Notr()
	}
	m, hata := Dogrula(g.Uygula(temel))
	if hata != "" {
		yazHata(w, http.StatusBadRequest, hata)
		return
	}
	beklenen := k.Surum
	if g.Surum != nil {
		beklenen = *g.Surum
	}
	if err := bayiKaydet(r.Context(), h.DB, k, m, beklenen); err != nil {
		if errors.Is(err, ErrSurum) {
			yazHata(w, http.StatusConflict, "Marka başka bir oturumda değiştirildi; sayfayı yenileyip tekrar deneyin.")
			return
		}
		log.Printf("marka: bayi markası yazılamadı (rid=%d): %v", rid, err)
		h.denetim(r, "marka.guncelle", "bayi", "", rid, false)
		yazHata(w, http.StatusInternalServerError, "Marka kaydedilemedi.")
		return
	}
	h.denetim(r, "marka.guncelle", "bayi", "", rid, true)
	k2, _ := bayiOkuDB(r.Context(), h.DB, rid)
	httpx.WriteJSON(w, http.StatusOK, bayiGorunum(k2))
}

// GorselYukle — POST /marka/yonet/gorsel/{yuva} (multipart "dosya", ≤512 KB).
func (h *Handlers) GorselYukle(w http.ResponseWriter, r *http.Request) {
	h.gorselIslem(w, r, true)
}

// GorselSil — DELETE /marka/yonet/gorsel/{yuva}
func (h *Handlers) GorselSil(w http.ResponseWriter, r *http.Request) {
	h.gorselIslem(w, r, false)
}

func (h *Handlers) gorselIslem(w http.ResponseWriter, r *http.Request, yukle bool) {
	rid, ok := sahip(w, r)
	if !ok {
		return
	}
	y, ok := YuvaBul(chi.URLParam(r, "yuva"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	var veri []byte
	var tip string
	if yukle {
		r.Body = http.MaxBytesReader(w, r.Body, LogoMaks+64*1024)
		if err := r.ParseMultipartForm(LogoMaks + 1024); err != nil {
			yazHata(w, http.StatusBadRequest, "Dosya okunamadı (en fazla 512 KB).")
			return
		}
		defer func() {
			if r.MultipartForm != nil {
				_ = r.MultipartForm.RemoveAll()
			}
		}()
		f, _, err := r.FormFile("dosya")
		if err != nil {
			yazHata(w, http.StatusBadRequest, "dosya alanı bulunamadı")
			return
		}
		ham, err := io.ReadAll(io.LimitReader(f, LogoMaks+1))
		f.Close()
		if err != nil {
			yazHata(w, http.StatusBadRequest, "Dosya okunamadı.")
			return
		}
		veri, tip, err = GorselDogrula(ham)
		if err != nil {
			yazHata(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	eylem := "marka.gorsel.sil"
	if yukle {
		eylem = "marka.gorsel.yukle"
	}
	yazKilidi.Lock()
	defer yazKilidi.Unlock()
	if rid == 0 {
		mevcut, err := KokOkuYonetim()
		if err != nil {
			yazHata(w, http.StatusInternalServerError, "Mevcut marka okunamadı; işlem yapılmadı.")
			return
		}
		if err := gorselYazVeyaSil(Kok, y, yukle, veri, tip); err != nil {
			log.Printf("marka: kök görseli (%s): %v", y.Ad, err)
			h.denetim(r, eylem, "kok/"+y.Ad, "", 0, false)
			yazHata(w, http.StatusInternalServerError, "Görsel kaydedilemedi.")
			return
		}
		if _, err := kokYazKilitli(mevcut); err != nil {
			log.Printf("marka: kök yazılamadı: %v", err)
		}
		h.denetim(r, eylem, "kok/"+y.Ad, "", 0, true)
		gor, _ := kokGorunum()
		httpx.WriteJSON(w, http.StatusOK, gor)
		return
	}
	k, ok := h.bayiYetkili(w, r, rid)
	if !ok {
		return
	}
	k, err := bayiSatirGarantile(r.Context(), h.DB, k)
	if err != nil || BayiDizin(k.Anahtar) == "" {
		log.Printf("marka: bayi satırı açılamadı (rid=%d): %v", rid, err)
		yazHata(w, http.StatusInternalServerError, "Görsel kaydedilemedi.")
		return
	}
	if err := gorselYazVeyaSil(BayiDizin(k.Anahtar), y, yukle, veri, tip); err != nil {
		log.Printf("marka: bayi görseli (rid=%d %s): %v", rid, y.Ad, err)
		h.denetim(r, eylem, "bayi/"+y.Ad, "", rid, false)
		yazHata(w, http.StatusInternalServerError, "Görsel kaydedilemedi.")
		return
	}
	bayiDusur(k)
	h.denetim(r, eylem, "bayi/"+y.Ad, "", rid, true)
	k2, _ := bayiOkuDB(r.Context(), h.DB, rid)
	httpx.WriteJSON(w, http.StatusOK, bayiGorunum(k2))
}

func gorselYazVeyaSil(dizin string, y Yuva, yukle bool, veri []byte, tip string) error {
	if yukle {
		return gorselYazDiz(dizin, y, veri, tip)
	}
	return gorselSilDiz(dizin, y)
}

// bayiSifirla — bayinin markasını kaldırır (izinler ve anahtar korunur).
func bayiSifirla(ctx context.Context, db *sql.DB, k BayiKayit) error {
	if !k.SatirVar {
		return nil
	}
	if d := BayiDizin(k.Anahtar); d != "" {
		if err := os.RemoveAll(d); err != nil {
			return err
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE cp_marka SET ayar_json='{}', etkin=0, surum=surum+1 WHERE reseller_id=?`, k.RID); err != nil {
		return err
	}
	bayiDusur(k)
	return nil
}

// Sifirla — POST /marka/yonet/sifirla (yalnız kendi markası).
func (h *Handlers) Sifirla(w http.ResponseWriter, r *http.Request) {
	rid, ok := sahip(w, r)
	if !ok {
		return
	}
	yazKilidi.Lock()
	defer yazKilidi.Unlock()
	if rid == 0 {
		for _, y := range yuvalar {
			_ = gorselSilDiz(Kok, y)
		}
		if _, err := kokYazKilitli(Varsayilan()); err != nil {
			log.Printf("marka: kök sıfırlanamadı: %v", err)
			h.denetim(r, "marka.sifirla", "kok", "", 0, false)
			yazHata(w, http.StatusInternalServerError, "Marka sıfırlanamadı.")
			return
		}
		h.denetim(r, "marka.sifirla", "kok", "", 0, true)
		gor, _ := kokGorunum()
		httpx.WriteJSON(w, http.StatusOK, gor)
		return
	}
	k, ok := h.bayiYetkili(w, r, rid)
	if !ok {
		return
	}
	if err := bayiSifirla(r.Context(), h.DB, k); err != nil {
		log.Printf("marka: bayi sıfırlanamadı (rid=%d): %v", rid, err)
		h.denetim(r, "marka.sifirla", "bayi", "", rid, false)
		yazHata(w, http.StatusInternalServerError, "Marka sıfırlanamadı.")
		return
	}
	h.denetim(r, "marka.sifirla", "bayi", "", rid, true)
	k2, _ := bayiOkuDB(r.Context(), h.DB, rid)
	httpx.WriteJSON(w, http.StatusOK, bayiGorunum(k2))
}

// ── kök moderasyonu (AdminOnly) ─────────────────────────────────────────────

type bayiSatiri struct {
	ID          int64  `json:"id"`
	Kullanici   string `json:"kullanici"`
	Durum       string `json:"durum"`
	Anahtar     string `json:"anahtar"`
	Kayitli     bool   `json:"kayitli"`
	PanelAdi    string `json:"panel_adi"`
	VurguRenk   string `json:"vurgu_renk"`
	Logo        bool   `json:"logo"`
	LogoSurum   int64  `json:"logo_surum"`
	Izin        bool   `json:"izin"`
	AlanadiIzni bool   `json:"alanadi_izni"`
	KokKapatti  bool   `json:"kok_kapatti"`
	AlanAdi     string `json:"alan_adi"`
	AlanDurum   string `json:"alan_durum"`
	AlanHata    string `json:"alan_hata"`
}

// Bayiler — GET /marka/bayiler (AdminOnly).
func (h *Handlers) Bayiler(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(), `SELECT u.id, u.username, u.status,
		COALESCE(a.alan_adi,''), COALESCE(a.durum,''), COALESCE(a.son_hata_genel,'')
		FROM users u LEFT JOIN cp_marka_alanadi a ON a.reseller_id=u.id
		WHERE u.role='reseller' ORDER BY u.username`)
	if err != nil {
		log.Printf("marka: bayi listesi: %v", err)
		yazHata(w, http.StatusInternalServerError, "Bayi listesi alınamadı.")
		return
	}
	var liste []bayiSatiri
	for rows.Next() {
		var s bayiSatiri
		if rows.Scan(&s.ID, &s.Kullanici, &s.Durum, &s.AlanAdi, &s.AlanDurum, &s.AlanHata) == nil {
			liste = append(liste, s)
		}
	}
	rows.Close()
	out := make([]bayiSatiri, 0, len(liste))
	for _, s := range liste {
		k, err := bayiOkuDB(r.Context(), h.DB, s.ID)
		if err == nil {
			s.Anahtar, s.Kayitli, s.Izin, s.AlanadiIzni, s.KokKapatti = k.Anahtar, k.Kayitli, k.Izin, k.AlanadiIzni, k.KokKapatti
			if k.Kayitli {
				s.PanelAdi, s.VurguRenk = k.Ayar.PanelAdi, k.Ayar.VurguRenk
			}
			s.Logo, s.LogoSurum = k.Ayar.Logo, k.Ayar.LogoSurum
		}
		out = append(out, s)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"bayiler": out,
		"yedek":   yedekPolitika(r.Context(), h.DB),
	})
}

// bayiMi — id gerçekten bir bayi mi (moderasyon hedefi doğrulaması).
func (h *Handlers) bayiMi(ctx context.Context, id int64) bool {
	var n int
	err := h.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE id=? AND role='reseller'`, id).Scan(&n)
	return err == nil && n == 1
}

func (h *Handlers) hedefBayi(w http.ResponseWriter, r *http.Request) (BayiKayit, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 || !h.bayiMi(r.Context(), id) {
		yazHata(w, http.StatusNotFound, "bayi bulunamadı")
		return BayiKayit{}, false
	}
	k, err := bayiOkuDB(r.Context(), h.DB, id)
	if err != nil {
		yazHata(w, http.StatusInternalServerError, "Marka kaydı okunamadı.")
		return k, false
	}
	return k, true
}

type izinGovde struct {
	Izin        *bool `json:"izin"`
	AlanadiIzni *bool `json:"alanadi_izni"`
	KokKapatti  *bool `json:"kok_kapatti"`
}

// BayiIzin — PUT /marka/bayiler/{id} (AdminOnly): izin / özel alan adı izni / moderasyon.
func (h *Handlers) BayiIzin(w http.ResponseWriter, r *http.Request) {
	k, ok := h.hedefBayi(w, r)
	if !ok {
		return
	}
	var g izinGovde
	if err := govdeCoz(r, &g); err != nil {
		yazHata(w, http.StatusBadRequest, "geçersiz istek gövdesi")
		return
	}
	izin, ai, kk := k.Izin, k.AlanadiIzni, k.KokKapatti
	if g.Izin != nil {
		izin = *g.Izin
	}
	if g.AlanadiIzni != nil {
		ai = *g.AlanadiIzni
	}
	if g.KokKapatti != nil {
		kk = *g.KokKapatti
	}
	b := func(v bool) int {
		if v {
			return 1
		}
		return 0
	}
	an := k.Anahtar
	if !k.SatirVar {
		var err error
		if an, err = YeniAnahtar(); err != nil {
			yazHata(w, http.StatusInternalServerError, "İzin kaydedilemedi.")
			return
		}
	}
	_, err := h.DB.ExecContext(r.Context(), `INSERT INTO cp_marka (reseller_id, anahtar, ayar_json, surum, etkin, izin, alanadi_izni, kok_kapatti)
		VALUES (?, ?, '{}', 1, 0, ?, ?, ?)
		ON DUPLICATE KEY UPDATE izin=VALUES(izin), alanadi_izni=VALUES(alanadi_izni), kok_kapatti=VALUES(kok_kapatti)`,
		k.RID, an, b(izin), b(ai), b(kk))
	detay := "izin=" + strconv.Itoa(b(izin)) + " alanadi=" + strconv.Itoa(b(ai)) + " kapali=" + strconv.Itoa(b(kk))
	if err != nil {
		log.Printf("marka: izin yazılamadı (rid=%d): %v", k.RID, err)
		h.denetim(r, "marka.izin", "bayi:"+strconv.FormatInt(k.RID, 10), detay, 0, false)
		yazHata(w, http.StatusInternalServerError, "İzin kaydedilemedi.")
		return
	}
	bayiDusur(k)
	hostTemizle()
	h.denetim(r, "marka.izin", "bayi:"+strconv.FormatInt(k.RID, 10), detay, 0, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// BayiSifirla — POST /marka/bayiler/{id}/sifirla (AdminOnly).
func (h *Handlers) BayiSifirla(w http.ResponseWriter, r *http.Request) {
	k, ok := h.hedefBayi(w, r)
	if !ok {
		return
	}
	yazKilidi.Lock()
	err := bayiSifirla(r.Context(), h.DB, k)
	yazKilidi.Unlock()
	hedef := "bayi:" + strconv.FormatInt(k.RID, 10)
	if err != nil {
		log.Printf("marka: bayi sıfırlanamadı (rid=%d): %v", k.RID, err)
		h.denetim(r, "marka.sifirla", hedef, "kök moderasyonu", 0, false)
		yazHata(w, http.StatusInternalServerError, "Marka sıfırlanamadı.")
		return
	}
	h.denetim(r, "marka.sifirla", hedef, "kök moderasyonu", 0, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// AyarKaydet — PUT /marka/ayar (AdminOnly): {"bayi_yedek":"kok"|"notr"}.
func (h *Handlers) AyarKaydet(w http.ResponseWriter, r *http.Request) {
	var g struct {
		BayiYedek string `json:"bayi_yedek"`
	}
	if err := govdeCoz(r, &g); err != nil || (g.BayiYedek != YedekKok && g.BayiYedek != YedekNotr) {
		yazHata(w, http.StatusBadRequest, "bayi_yedek 'kok' ya da 'notr' olmalı")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `INSERT INTO cp_ayarlar (anahtar, deger) VALUES ('marka_bayi_yedek', ?)
		ON DUPLICATE KEY UPDATE deger=VALUES(deger)`, g.BayiYedek); err != nil {
		yazHata(w, http.StatusInternalServerError, "Ayar kaydedilemedi.")
		return
	}
	obAyar.sil("marka_bayi_yedek")
	h.denetim(r, "marka.ayar", "bayi_yedek", g.BayiYedek, 0, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
