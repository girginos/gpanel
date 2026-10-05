package backups

// hedef_handlers.go — kökün uzak yedek hedefleri (backup_hedefler) için API
// uçları; bu sürümde yalnız "drive" (Google Drive) türü. Hepsi AdminOnly
// (main.go'da bağlanır); sahip_tur='kok', sahip_id=0.
//
//	GET    /api/v1/admin/backups/hedefler                    -> hedef listesi (SIRSIZ)
//	POST   /api/v1/admin/backups/hedefler/drive              -> Drive hedefi ekle (client_id/secret, klasör)
//	DELETE /api/v1/admin/backups/hedefler/{id}               -> sil (uzak kopya kaydı varsa 409; jeton iptal edilir)
//	POST   /api/v1/admin/backups/hedefler/{id}/yetki-baslat  -> OAuth cihaz kodu al (kod + google.com/device)
//	POST   /api/v1/admin/backups/hedefler/{id}/yetki-sorgu   -> kullanıcı onayladı mı? (onaylandıysa jeton şifreli saklanır)
//	POST   /api/v1/admin/backups/hedefler/{id}/baglantiyi-kes -> jetonu Google'da iptal et + sunucudan sil
//	POST   /api/v1/admin/backups/hedefler/{id}/test          -> uçtan uca bağlantı testi
//
// 🔴 Sırlar (OAuth istemci gizli anahtarı, yenileme jetonu) backup_hedefler.sirlar
// kolonunda gizli.SaklaBagli ile (satıra bağlı) ŞİFRELİ durur; hiçbir yanıtta,
// denetim kaydında ya da günlükte düz görünmez. Şifreleme anahtarı hazır
// değilse (gizli.SaklaBagli düz metne düşerdi) hedef EKLENMEZ.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"girginospanel/internal/backups/hedef"
	"girginospanel/internal/backups/hedef/gdrive"
	"girginospanel/internal/gizli"
	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"

	"github.com/go-chi/chi/v5"
)

// Test dikişleri: üretimde gerçek uygulamalar; testler değiştirir.
var (
	hedefSirSakla     = gizli.SaklaBagli
	hedefSirCoz       = gizli.CozBagli
	hedefSirHazir     = gizli.AnahtarHazir
	driveCihazIste    = gdrive.CihazYetkisiIste
	driveCihazSorgula = gdrive.CihazYetkisiSorgula
	driveJetonIptal   = gdrive.JetonIptal
	driveHedefKur     = func(ayar json.RawMessage, sirlar map[string]string, kaydet func(string)) (hedef.Hedef, error) {
		return gdrive.Yeni(ayar, sirlar, hedef.KurSecenek{}, gdrive.Secenek{YenilemeKaydet: kaydet})
	}
	// hedefDenetimYakala — yalnız testler: denetim yazmak yerine kaydı yakalar.
	hedefDenetimYakala func(eylem, hedefAd, detay string)
)

// hedefDenetimYaz — denetim kaydı (aktör istekten çözülür). Eylem kodu çağrı
// yerinde LITERAL olmalı (denetim.TestEylemKataloguKaynaktakiTumKodlariKapsar).
func hedefDenetimYaz(db *sql.DB, r *http.Request, eylem, hedefAd, detay string, basarili bool) {
	if hedefDenetimYakala != nil {
		hedefDenetimYakala(eylem, hedefAd, detay)
		return
	}
	uid, kul := middleware.Aktor(r)
	httpx.Denetim(db, r, uid, kul, eylem, hedefAd, detay, 0, basarili)
}

// Durum değerleri (backup_hedefler.son_durum, VARCHAR(16)).
const (
	hedefDurumYetkiYok     = "yetki-yok"     // yetkilendirme yapılmamış / bağlantı kesilmiş
	hedefDurumBagli        = "bagli"         // son test başarılı ya da jeton yeni alındı
	hedefDurumYetkiGerekli = "yetki-gerekli" // jeton geçersiz (iptal / test modunda 7 gün)
	hedefDurumHata         = "hata"          // başka hata (ağ, kota ...)
)

// HedefOzet — API yanıtı. SIR YOK: yalnız sırların var/yok bilgisi.
type HedefOzet struct {
	ID            int64  `json:"id"`
	Ad            string `json:"ad"`
	Tur           string `json:"tur"`
	Aktif         bool   `json:"aktif"`
	ClientID      string `json:"client_id,omitempty"`
	Klasor        string `json:"klasor,omitempty"`
	Durum         string `json:"durum"`
	YetkiVar      bool   `json:"yetki_var"`
	YetkiBekliyor bool   `json:"yetki_bekliyor"`
	HataKategori  string `json:"hata_kategori,omitempty"`
	HataAciklama  string `json:"hata_aciklama,omitempty"`
	SonDeneme     string `json:"son_deneme,omitempty"`
	SonBasari     string `json:"son_basari,omitempty"`
}

type hedefSatir struct {
	id        int64
	ad, tur   string
	ayar      json.RawMessage
	sirlar    string
	aktif     bool
	sonDurum  string
	hataKat   string
	sonDeneme sql.NullString
	sonBasari sql.NullString
}

const hedefSecim = `SELECT id, ad, tur, ayar, sirlar, aktif, son_durum, hata_kategori,
	DATE_FORMAT(son_deneme,'%Y-%m-%d %H:%i'), DATE_FORMAT(son_basari,'%Y-%m-%d %H:%i')
	FROM backup_hedefler WHERE sahip_tur='kok' AND sahip_id=0`

func hedefTara(sc interface{ Scan(...any) error }) (*hedefSatir, error) {
	var s hedefSatir
	var ayar string
	var aktif int
	if err := sc.Scan(&s.id, &s.ad, &s.tur, &ayar, &s.sirlar, &aktif, &s.sonDurum, &s.hataKat, &s.sonDeneme, &s.sonBasari); err != nil {
		return nil, err
	}
	s.ayar, s.aktif = json.RawMessage(ayar), aktif == 1
	return &s, nil
}

func hedefBaglam(id int64) string { return "backup_hedefler:" + strconv.FormatInt(id, 10) + ":sirlar" }

// sirlariAc — satırın şifreli sırlarını çözer. Boş sırlar boş harita.
func (s *hedefSatir) sirlariAc() (map[string]string, error) {
	m := map[string]string{}
	if s.sirlar == "" {
		return m, nil
	}
	duz := hedefSirCoz(s.sirlar, hedefBaglam(s.id))
	if duz == s.sirlar && s.sirlar != "" && !strings.HasPrefix(duz, "{") {
		return nil, errors.New("sırlar çözülemedi")
	}
	if err := json.Unmarshal([]byte(duz), &m); err != nil {
		return nil, errors.New("sırlar çözülemedi")
	}
	return m, nil
}

// sirlariYaz — sırları satıra bağlı şifreleyip yazar. Şifreleme gerçekten
// yapılmadıysa (anahtar yok) HİÇBİR ŞEY yazılmaz.
func hedefSirlariYaz(ctx context.Context, ex interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, id int64, m map[string]string) error {
	b, _ := json.Marshal(m)
	kapali := hedefSirSakla(string(b), hedefBaglam(id))
	if !gizli.SifreliMi(kapali) {
		return errors.New("şifreleme anahtarı hazır değil")
	}
	_, err := ex.ExecContext(ctx, `UPDATE backup_hedefler SET sirlar=? WHERE id=?`, kapali, id)
	return err
}

func (h *Handlers) hedefYukle(r *http.Request) (*hedefSatir, int, string) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		return nil, http.StatusBadRequest, "geçersiz hedef"
	}
	s, err := hedefTara(h.DB.QueryRowContext(r.Context(), hedefSecim+` AND id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, http.StatusNotFound, "hedef bulunamadı"
	}
	if err != nil {
		log.Printf("yedek hedef: okunamadı: %v", err)
		return nil, http.StatusInternalServerError, "hedef okunamadı"
	}
	if s.tur != gdrive.TurAdi {
		return nil, http.StatusBadRequest, "bu işlem yalnız Google Drive hedefi için"
	}
	return s, 0, ""
}

// ─── bekleyen cihaz yetkilendirmeleri (bellekte; yeniden başlatmada düşer) ───

type cihazBekleyen struct {
	cihazKodu     string // GİZLİ: yalnız sunucuda
	kullaniciKodu string
	adres         string
	bitis         time.Time
	sonSorgu      time.Time
	aralik        time.Duration
}

var cihazBekleyenler = struct {
	sync.Mutex
	m map[int64]*cihazBekleyen
}{m: map[int64]*cihazBekleyen{}}

func cihazBekliyorMu(id int64) bool {
	cihazBekleyenler.Lock()
	defer cihazBekleyenler.Unlock()
	b := cihazBekleyenler.m[id]
	if b != nil && time.Now().After(b.bitis) {
		delete(cihazBekleyenler.m, id)
		return false
	}
	return b != nil
}

func cihazBekleyenSil(id int64) {
	cihazBekleyenler.Lock()
	delete(cihazBekleyenler.m, id)
	cihazBekleyenler.Unlock()
}

// ─── özet ───────────────────────────────────────────────────────────────────

func (s *hedefSatir) ozet() HedefOzet {
	o := HedefOzet{ID: s.id, Ad: s.ad, Tur: s.tur, Aktif: s.aktif, Durum: s.sonDurum, HataKategori: s.hataKat,
		SonDeneme: s.sonDeneme.String, SonBasari: s.sonBasari.String, YetkiBekliyor: cihazBekliyorMu(s.id)}
	var a struct {
		ClientID string `json:"client_id"`
		Klasor   string `json:"klasor"`
	}
	_ = json.Unmarshal(s.ayar, &a)
	o.ClientID, o.Klasor = a.ClientID, a.Klasor
	if m, err := s.sirlariAc(); err == nil {
		o.YetkiVar = strings.TrimSpace(m[gdrive.SirYenileme]) != ""
	}
	switch {
	case !o.YetkiVar:
		o.Durum = hedefDurumYetkiYok
	case o.Durum == "":
		o.Durum = hedefDurumBagli
	}
	if k := hedef.Kategori(s.hataKat); k.Gecerli() && s.hataKat != "" {
		o.HataAciklama = k.Aciklama()
	}
	return o
}

// ─── uçlar ──────────────────────────────────────────────────────────────────

// HedefListe — GET /admin/backups/hedefler
func (h *Handlers) HedefListe(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(), hedefSecim+` ORDER BY id`)
	if err != nil {
		log.Printf("yedek hedef: liste okunamadı: %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, "hedefler listelenemedi")
		return
	}
	defer rows.Close()
	out := []HedefOzet{}
	for rows.Next() {
		s, err := hedefTara(rows)
		if err != nil {
			continue
		}
		out = append(out, s.ozet())
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

type driveEkleIstek struct {
	Ad           string `json:"ad"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Klasor       string `json:"klasor"`
}

func hedefAdiGecerli(s string) bool {
	if s == "" || s != strings.TrimSpace(s) || utf8.RuneCountInString(s) > 64 || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// HedefDriveEkle — POST /admin/backups/hedefler/drive
func (h *Handlers) HedefDriveEkle(w http.ResponseWriter, r *http.Request) {
	var g driveEkleIstek
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&g); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek gövdesi")
		return
	}
	g.Ad, g.ClientID, g.Klasor = strings.TrimSpace(g.Ad), strings.TrimSpace(g.ClientID), strings.TrimSpace(g.Klasor)
	g.ClientSecret = strings.TrimSpace(g.ClientSecret)
	if g.Klasor == "" {
		g.Klasor = gdrive.VarsayilanKlasor
	}
	switch {
	case !hedefAdiGecerli(g.Ad):
		httpx.WriteError(w, http.StatusBadRequest, "hedef adı 1-64 karakter olmalı")
		return
	case !gdrive.ClientIDGecerli(g.ClientID):
		httpx.WriteError(w, http.StatusBadRequest, "Client ID biçimi geçersiz (…apps.googleusercontent.com olmalı)")
		return
	case !gdrive.ClientSecretGecerli(g.ClientSecret):
		httpx.WriteError(w, http.StatusBadRequest, "Client Secret biçimi geçersiz")
		return
	case !gdrive.KlasorAdiGecerli(g.Klasor):
		httpx.WriteError(w, http.StatusBadRequest, "klasör adı geçersiz (\"/\" ve kontrol karakteri olamaz, en çok 100 karakter)")
		return
	}
	if err := hedefSirHazir(); err != nil {
		log.Printf("yedek hedef: şifreleme anahtarı hazır değil: %v", err)
		httpx.WriteError(w, http.StatusServiceUnavailable, "şifreleme anahtarı hazır değil; kimlik bilgileri güvenle saklanamaz")
		return
	}
	ayar, _ := json.Marshal(map[string]string{"client_id": g.ClientID, "klasor": g.Klasor})
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "hedef eklenemedi")
		return
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(r.Context(),
		`INSERT INTO backup_hedefler (ad, tur, ayar, sirlar, sahip_tur, sahip_id, son_durum) VALUES (?, ?, ?, '', 'kok', 0, ?)`,
		g.Ad, gdrive.TurAdi, string(ayar), hedefDurumYetkiYok)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			httpx.WriteError(w, http.StatusConflict, "bu adla bir hedef zaten var")
			return
		}
		log.Printf("yedek hedef: eklenemedi: %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, "hedef eklenemedi")
		return
	}
	id, _ := res.LastInsertId()
	if err := hedefSirlariYaz(r.Context(), tx, id, map[string]string{gdrive.SirClientSecret: g.ClientSecret}); err != nil {
		log.Printf("yedek hedef: sırlar yazılamadı: %v", err)
		httpx.WriteError(w, http.StatusServiceUnavailable, "kimlik bilgileri şifreli saklanamadı")
		return
	}
	if err := tx.Commit(); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "hedef eklenemedi")
		return
	}
	hedefDenetimYaz(h.DB, r, "yedek.hedef.ekle", g.Ad, "Google Drive hedefi eklendi", true)
	s, _ := hedefTara(h.DB.QueryRowContext(r.Context(), hedefSecim+` AND id=?`, id))
	if s == nil {
		httpx.WriteJSON(w, http.StatusCreated, map[string]any{"ok": true, "id": id})
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, s.ozet())
}

// kimlik — satırdan OAuth istemci kimliği (client_id ayardan, secret çözülmüş sırlardan).
func (s *hedefSatir) kimlik() (gdrive.Kimlik, map[string]string, error) {
	m, err := s.sirlariAc()
	if err != nil {
		return gdrive.Kimlik{}, nil, err
	}
	var a gdrive.Ayar
	if err := json.Unmarshal(s.ayar, &a); err != nil {
		return gdrive.Kimlik{}, nil, errors.New("ayar okunamadı")
	}
	return gdrive.Kimlik{ClientID: a.ClientID, ClientSecret: m[gdrive.SirClientSecret]}, m, nil
}

// HedefSil — DELETE /admin/backups/hedefler/{id}
func (h *Handlers) HedefSil(w http.ResponseWriter, r *http.Request) {
	s, kod, msg := h.hedefYukle(r)
	if s == nil {
		httpx.WriteError(w, kod, msg)
		return
	}
	var n int
	if err := h.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM backup_uzak_kopyalar WHERE hedef_id=?`, s.id).Scan(&n); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "hedef silinemedi")
		return
	}
	if n > 0 {
		httpx.WriteError(w, http.StatusConflict, "bu hedefe ait uzak yedek kayıtları var; önce o yedekler silinmeli")
		return
	}
	// Kimlik bilgisi silinmeden önce jeton Google'da iptal edilir (en iyi çaba).
	if k, m, err := s.kimlik(); err == nil && strings.TrimSpace(m[gdrive.SirYenileme]) != "" {
		ctx, iptal := context.WithTimeout(r.Context(), 30*time.Second)
		if err := driveJetonIptal(ctx, k, hedef.KurSecenek{}, m[gdrive.SirYenileme]); err != nil {
			log.Printf("yedek hedef: silerken jeton iptal edilemedi (id=%d, kategori=%s)", s.id, hedef.KategoriAl(err))
		}
		iptal()
	}
	if _, err := h.DB.ExecContext(r.Context(), `DELETE FROM backup_hedefler WHERE id=? AND sahip_tur='kok'`, s.id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "hedef silinemedi")
		return
	}
	cihazBekleyenSil(s.id)
	hedefDenetimYaz(h.DB, r, "yedek.hedef.sil", s.ad, "Google Drive hedefi silindi", true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// hedefHataYaniti — kullanıcıya yalnız kategori + sabit Türkçe açıklama; ham metin günlükte.
func hedefHataYaniti(w http.ResponseWriter, islem string, id int64, err error) {
	var he *hedef.Hata
	kat := hedef.KategoriAl(err)
	ayrinti := ""
	if errors.As(err, &he) {
		ayrinti = he.Ayrinti()
	}
	log.Printf("yedek hedef %s: id=%d kategori=%s ayrinti=%s", islem, id, kat, ayrinti)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": false, "kategori": string(kat), "aciklama": kat.Aciklama()})
}

// HedefYetkiBaslat — POST /admin/backups/hedefler/{id}/yetki-baslat
func (h *Handlers) HedefYetkiBaslat(w http.ResponseWriter, r *http.Request) {
	s, kod, msg := h.hedefYukle(r)
	if s == nil {
		httpx.WriteError(w, kod, msg)
		return
	}
	k, _, err := s.kimlik()
	if err != nil || !gdrive.ClientSecretGecerli(k.ClientSecret) {
		httpx.WriteError(w, http.StatusConflict, "hedefin kimlik bilgileri okunamadı; hedefi silip yeniden ekleyin")
		return
	}
	ctx, iptal := context.WithTimeout(r.Context(), 60*time.Second)
	defer iptal()
	c, err := driveCihazIste(ctx, k, hedef.KurSecenek{})
	if err != nil {
		hedefHataYaniti(w, "yetki-baslat", s.id, err)
		return
	}
	cihazBekleyenler.Lock()
	cihazBekleyenler.m[s.id] = &cihazBekleyen{cihazKodu: c.CihazKodu, kullaniciKodu: c.KullaniciKodu, adres: c.DogrulamaURL,
		bitis: time.Now().Add(c.Gecerlilik), aralik: c.Aralik}
	cihazBekleyenler.Unlock()
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "kullanici_kodu": c.KullaniciKodu, "dogrulama_adresi": c.DogrulamaURL,
		"gecerlilik_sn": int(c.Gecerlilik.Seconds()), "aralik_sn": int(c.Aralik.Seconds()),
	})
}

// HedefYetkiSorgu — POST /admin/backups/hedefler/{id}/yetki-sorgu
func (h *Handlers) HedefYetkiSorgu(w http.ResponseWriter, r *http.Request) {
	s, kod, msg := h.hedefYukle(r)
	if s == nil {
		httpx.WriteError(w, kod, msg)
		return
	}
	cihazBekleyenler.Lock()
	b := cihazBekleyenler.m[s.id]
	if b == nil {
		cihazBekleyenler.Unlock()
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "durum": "yok"})
		return
	}
	if time.Now().After(b.bitis) {
		delete(cihazBekleyenler.m, s.id)
		cihazBekleyenler.Unlock()
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "durum": string(gdrive.SorguSuresiDoldu)})
		return
	}
	// Google'ın sorgu aralığına uyulur (aksi halde slow_down): erken sorgu Google'a gitmez.
	if time.Since(b.sonSorgu) < b.aralik {
		cihazBekleyenler.Unlock()
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "durum": string(gdrive.SorguBekliyor)})
		return
	}
	b.sonSorgu = time.Now()
	cihazKodu := b.cihazKodu
	cihazBekleyenler.Unlock()

	k, m, err := s.kimlik()
	if err != nil {
		httpx.WriteError(w, http.StatusConflict, "hedefin kimlik bilgileri okunamadı; hedefi silip yeniden ekleyin")
		return
	}
	ctx, iptal := context.WithTimeout(r.Context(), 60*time.Second)
	defer iptal()
	sonuc, err := driveCihazSorgula(ctx, k, hedef.KurSecenek{}, cihazKodu)
	if err != nil {
		hedefHataYaniti(w, "yetki-sorgu", s.id, err)
		return
	}
	switch sonuc.Durum {
	case gdrive.SorguYavasla:
		cihazBekleyenler.Lock()
		if b := cihazBekleyenler.m[s.id]; b != nil {
			b.aralik += 5 * time.Second
		}
		cihazBekleyenler.Unlock()
	case gdrive.SorguReddedildi, gdrive.SorguSuresiDoldu:
		cihazBekleyenSil(s.id)
	case gdrive.SorguTamam:
		m[gdrive.SirYenileme] = sonuc.YenilemeJetonu
		if err := hedefSirlariYaz(r.Context(), h.DB, s.id, m); err != nil {
			log.Printf("yedek hedef: jeton saklanamadı (id=%d): %v", s.id, err)
			httpx.WriteError(w, http.StatusServiceUnavailable, "jeton şifreli saklanamadı")
			return
		}
		_, _ = h.DB.ExecContext(r.Context(), `UPDATE backup_hedefler SET son_durum=?, hata_kategori='' WHERE id=?`, hedefDurumBagli, s.id)
		cihazBekleyenSil(s.id)
		hedefDenetimYaz(h.DB, r, "yedek.hedef.yetkilendi", s.ad, "Google Drive yetkilendirildi", true)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "durum": string(sonuc.Durum)})
}

// HedefBaglantiKes — POST /admin/backups/hedefler/{id}/baglantiyi-kes
func (h *Handlers) HedefBaglantiKes(w http.ResponseWriter, r *http.Request) {
	s, kod, msg := h.hedefYukle(r)
	if s == nil {
		httpx.WriteError(w, kod, msg)
		return
	}
	k, m, err := s.kimlik()
	if err != nil {
		httpx.WriteError(w, http.StatusConflict, "hedefin kimlik bilgileri okunamadı; hedefi silip yeniden ekleyin")
		return
	}
	iptalBasarili := true
	if jeton := strings.TrimSpace(m[gdrive.SirYenileme]); jeton != "" {
		ctx, iptal := context.WithTimeout(r.Context(), 30*time.Second)
		if err := driveJetonIptal(ctx, k, hedef.KurSecenek{}, jeton); err != nil {
			iptalBasarili = false
			log.Printf("yedek hedef: jeton Google'da iptal edilemedi (id=%d, kategori=%s)", s.id, hedef.KategoriAl(err))
		}
		iptal()
	}
	// Sunucudaki jeton her durumda silinir (Google tarafı iptal edilemese bile
	// kullanıcı Google hesap ayarlarından erişimi kaldırabilir).
	delete(m, gdrive.SirYenileme)
	if err := hedefSirlariYaz(r.Context(), h.DB, s.id, m); err != nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "jeton silinemedi")
		return
	}
	_, _ = h.DB.ExecContext(r.Context(), `UPDATE backup_hedefler SET son_durum=?, hata_kategori='' WHERE id=?`, hedefDurumYetkiYok, s.id)
	cihazBekleyenSil(s.id)
	hedefDenetimYaz(h.DB, r, "yedek.hedef.baglanti_kesildi", s.ad, "Google Drive bağlantısı kesildi", true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "google_iptal": iptalBasarili})
}

// HedefTest — POST /admin/backups/hedefler/{id}/test
func (h *Handlers) HedefTest(w http.ResponseWriter, r *http.Request) {
	s, kod, msg := h.hedefYukle(r)
	if s == nil {
		httpx.WriteError(w, kod, msg)
		return
	}
	m, err := s.sirlariAc()
	if err != nil {
		httpx.WriteError(w, http.StatusConflict, "hedefin kimlik bilgileri okunamadı; hedefi silip yeniden ekleyin")
		return
	}
	if strings.TrimSpace(m[gdrive.SirYenileme]) == "" {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": false, "kategori": string(hedef.KategoriKimlik),
			"aciklama": "Google hesabı henüz yetkilendirilmedi", "yetki_gerekli": true})
		return
	}
	// Google yeni bir yenileme jetonu verirse (nadir) şifreli kalıcılaştırılır.
	kaydet := func(yeni string) {
		m[gdrive.SirYenileme] = yeni
		if err := hedefSirlariYaz(context.Background(), h.DB, s.id, m); err != nil {
			log.Printf("yedek hedef: yenilenen jeton saklanamadı (id=%d): %v", s.id, err)
		}
	}
	t, err := driveHedefKur(s.ayar, m, kaydet)
	var terr error
	if err != nil {
		terr = err
	} else {
		ctx, iptal := context.WithTimeout(r.Context(), 3*time.Minute)
		terr = t.Test(ctx)
		iptal()
	}
	if terr == nil {
		_, _ = h.DB.ExecContext(r.Context(),
			`UPDATE backup_hedefler SET son_durum=?, hata_kategori='', son_deneme=NOW(), son_basari=NOW() WHERE id=?`, hedefDurumBagli, s.id)
		hedefDenetimYaz(h.DB, r, "yedek.hedef.test", s.ad, "Bağlantı testi başarılı", true)
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	kat := hedef.KategoriAl(terr)
	durum := hedefDurumHata
	if kat == hedef.KategoriKimlik {
		durum = hedefDurumYetkiGerekli
	}
	_, _ = h.DB.ExecContext(r.Context(),
		`UPDATE backup_hedefler SET son_durum=?, hata_kategori=?, son_deneme=NOW() WHERE id=?`, durum, string(kat), s.id)
	hedefDenetimYaz(h.DB, r, "yedek.hedef.test", s.ad, fmt.Sprintf("Bağlantı testi başarısız (%s)", kat), false)
	var he *hedef.Hata
	if errors.As(terr, &he) {
		log.Printf("yedek hedef test: id=%d kategori=%s ayrinti=%s", s.id, kat, he.Ayrinti())
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": false, "kategori": string(kat), "aciklama": kat.Aciklama(),
		"yetki_gerekli": kat == hedef.KategoriKimlik})
}
