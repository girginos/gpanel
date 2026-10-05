package backups

// uzak_handlers.go — uzak kopya (şifreli) API uçları. Hepsi AdminOnly (main.go);
// API anahtarına KAPALI (apianahtar.Rotalar'da yok).
//
//	GET  /api/v1/admin/backups/uzak-kopya                      -> durum + ayar + anahtarlar + özet + hedefler
//	PUT  /api/v1/admin/backups/uzak-kopya/ayar                 -> ayar (aç/kapat, hedef, klasör, saklama, bant)
//	POST /api/v1/admin/backups/uzak-kopya/anahtar-uret         -> {hibrit} yeni anahtar; kurtarma dosyası TEK KEZ döner
//	POST /api/v1/admin/backups/uzak-kopya/anahtar-onayla       -> {parmak, sakladim, indirildi} anahtarı etkinleştirir
//	POST /api/v1/admin/backups/uzak-kopya/anahtar-ice-aktar    -> {alici} yalnız açık anahtar
//	GET  /api/v1/admin/backups/uzak-kopya/isler?sayfa=&adet=&durum=
//	POST /api/v1/admin/backups/uzak-kopya/simdi-gonder/{yedek_id}
//	POST /api/v1/admin/backups/uzak-kopya/yeniden-dene/{is_id}
//	POST /api/v1/admin/backups/uzak-kopya/iptal/{is_id}
//	GET  /api/v1/admin/backups/uzak-kopya/saklama-onizle       -> silinecek kopyalar (uzağa dokunmaz)
//
//	POST /api/v1/admin/backups/uzak-kopya/anahtar-yenile       -> {hibrit} (Gelişmiş) yeni anahtarı ANINDA etkinleştirir
//	POST /api/v1/admin/backups/uzak-kopya/kurtarma-anahtari    -> kurtarma dosyası (attachment); isteğe bağlı, her zaman indirilebilir
//	GET  /api/v1/admin/backups/uzak-kopya/mevcutlari-gonder?n= -> önizleme (adet, boyut toplamı)
//	POST /api/v1/admin/backups/uzak-kopya/mevcutlari-gonder    -> {n} son n kök yedeği (domain başına) sıraya al
//	GET  /api/v1/admin/backups/uzak-kopya/yedek-durumlari?ids= -> yedek satırı rozetleri
//	GET  /api/v1/admin/backups/uzak-kopya/ilerleme              -> canlı ilerleme + kuyruk ETA (bellek + tek özet sorgusu)
//
// Sade akış: ayar kaydında hedef seçilip açılınca etkin anahtar yoksa SUNUCU klasik anahtar
// üretir (kullanıcı anahtar üretmez/onaylamaz). Kurtarma anahtarı kullanıcı isterse indirilir.
//
// 🔴 Özel anahtar yalnız anahtar-uret ve kurtarma-anahtari yanıtlarında (kurtarma dosyası
// metni) döner, Cache-Control: no-store ile; hiçbir günlük/denetim/başka yanıtta yer almaz.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/backups/uzak"
	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

// uzakDenetimYaz — hedefDenetimYaz ile aynı yakalama dikişi.
func uzakDenetimYaz(db *sql.DB, r *http.Request, eylem, nesne, detay string, basarili bool) {
	hedefDenetimYaz(db, r, eylem, nesne, detay, basarili)
}

func uzakJSON(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 64<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	return d.Decode(v)
}

func uzakHataYaz(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, uzak.ErrAyarGecersiz), errors.Is(err, uzak.ErrAliciGecersiz):
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, uzak.ErrAnahtarYok), errors.Is(err, uzak.ErrHedefYok), errors.Is(err, uzak.ErrKapali),
		errors.Is(err, uzak.ErrDurumUygunDegil), errors.Is(err, uzak.ErrZatenGonderildi), errors.Is(err, uzak.ErrKokDegil),
		errors.Is(err, uzak.ErrAnahtarVar), errors.Is(err, uzak.ErrAnahtarBulunamadi), errors.Is(err, uzak.ErrSifreHazirDegil):
		httpx.WriteError(w, http.StatusConflict, err.Error())
	case errors.Is(err, uzak.ErrIsYok), errors.Is(err, uzak.ErrYedekYok):
		httpx.WriteError(w, http.StatusNotFound, err.Error())
	default:
		log.Printf("uzak kopya: istek başarısız: %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, "işlem başarısız")
	}
}

// UzakKopyaDurum — GET /admin/backups/uzak-kopya
func (h *Handlers) UzakKopyaDurum(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ayar, err := uzak.AyarOku(ctx, h.DB)
	if err != nil {
		uzakHataYaz(w, err)
		return
	}
	anahtarlar, err := uzak.Anahtarlar(ctx, h.DB)
	if err != nil {
		uzakHataYaz(w, err)
		return
	}
	ozet, err := uzak.Ozet(ctx, h.DB)
	if err != nil {
		uzakHataYaz(w, err)
		return
	}
	var etkin *uzak.AnahtarBilgi
	bekleyen := false
	for k := range anahtarlar {
		switch anahtarlar[k].Durum {
		case "etkin":
			etkin = &anahtarlar[k]
		case "bekliyor":
			bekleyen = true
		}
	}
	hedefler := []HedefOzet{}
	if rows, err := h.DB.QueryContext(ctx, hedefSecim+` ORDER BY id`); err == nil {
		defer rows.Close()
		for rows.Next() {
			if s, err := hedefTara(rows); err == nil && s.aktif {
				o := s.ozet()
				if o.YetkiVar {
					hedefler = append(hedefler, o)
				}
			}
		}
	}
	hedefAd := ""
	for _, o := range hedefler {
		if o.ID == ayar.HedefID {
			hedefAd = o.Ad
		}
	}
	gonderim, err := uzak.GonderimOzeti(ctx, h.DB)
	if err != nil {
		uzakHataYaz(w, err)
		return
	}
	kurtarma := map[string]any{"var": etkin != nil && etkin.SunucudaOzel, "indirildi": ""}
	if etkin != nil {
		for _, k := range anahtarlar {
			if k.SunucudaOzel && k.KurtarmaIndirildi != "" {
				kurtarma["indirildi"] = k.KurtarmaIndirildi
				break
			}
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"gonderim":         gonderim,
		"kurtarma":         kurtarma,
		"ayar":             ayar,
		"hedef_ad":         hedefAd,
		"hedefler":         hedefler,
		"etkin_anahtar":    etkin,
		"anahtarlar":       anahtarlar,
		"bekleyen_anahtar": bekleyen,
		"ozet":             ozet,
		"age_surum":        uzak.AgeSurum(),
	})
}

// UzakKopyaIlerleme — GET /admin/backups/uzak-kopya/ilerleme
// Canlı ilerleme + kuyruk ETA'sı: bellek izleyicisi + küçük özet sorgusu (ağır iş yok).
// Hız/ETA yalnız gerçek ölçümden; yanıtta sır/anahtar/yerel yol yoktur.
func (h *Handlers) UzakKopyaIlerleme(w http.ResponseWriter, r *http.Request) {
	ayar, err := uzak.AyarOku(r.Context(), h.DB)
	if err != nil {
		uzakHataYaz(w, err)
		return
	}
	var iz *uzak.Izleyici
	if i := uzakIsci.Load(); i != nil {
		iz = i.Izle()
	}
	o, err := uzak.IlerlemeOku(r.Context(), h.DB, iz, ayar.Aktif)
	if err != nil {
		uzakHataYaz(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, o)
}

// UzakKopyaAyarYaz — PUT /admin/backups/uzak-kopya/ayar
func (h *Handlers) UzakKopyaAyarYaz(w http.ResponseWriter, r *http.Request) {
	var a uzak.Ayar
	if err := uzakJSON(r, &a); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek")
		return
	}
	onceki, _ := uzak.AyarOku(r.Context(), h.DB)
	uretildi, err := uzak.AyarYazAnahtarli(r.Context(), h.DB, a)
	if err != nil {
		uzakHataYaz(w, err)
		return
	}
	if uretildi {
		uzakDenetimYaz(h.DB, r, "yedek.uzak.anahtar_otomatik", "uzak kopya", "Şifreleme anahtarı otomatik oluşturuldu", true)
	}
	switch {
	case a.Aktif && !onceki.Aktif:
		uzakDenetimYaz(h.DB, r, "yedek.uzak.ac", "uzak kopya", "Uzak kopya açıldı", true)
	case !a.Aktif && onceki.Aktif:
		uzakDenetimYaz(h.DB, r, "yedek.uzak.kapat", "uzak kopya", "Uzak kopya kapatıldı", true)
	default:
		uzakDenetimYaz(h.DB, r, "yedek.uzak.ayar", "uzak kopya", "Saklama / hedef / bant ayarı güncellendi", true)
	}
	if i := uzakIsci.Load(); i != nil && a.Aktif {
		i.Tetikle()
	}
	h.UzakKopyaDurum(w, r)
}

// UzakKopyaAnahtarUret — POST /admin/backups/uzak-kopya/anahtar-uret
func (h *Handlers) UzakKopyaAnahtarUret(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Hibrit bool `json:"hibrit"`
	}
	if r.ContentLength != 0 {
		if err := uzakJSON(r, &in); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek")
			return
		}
	}
	ad, metin, b, err := uzak.AnahtarUret(r.Context(), h.DB, in.Hibrit, time.Now())
	if err != nil {
		uzakHataYaz(w, err)
		return
	}
	uzakDenetimYaz(h.DB, r, "yedek.uzak.anahtar_uret", "anahtar "+b.KisaParmak, "Yeni şifreleme anahtarı üretildi ("+b.Sifreleme+"), henüz onaylanmadı", true)
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"parmak": b.Parmak, "kisa_parmak": b.KisaParmak, "alici": b.Alici, "sifreleme": b.Sifreleme,
		"dosya_adi": ad, "kurtarma_dosyasi": metin,
		"uyari": "Bu özel anahtar bir daha GÖSTERİLMEZ. Anahtar olmadan uzak kopyalar geri yüklenemez.",
	})
}

// UzakKopyaAnahtarOnayla — POST /admin/backups/uzak-kopya/anahtar-onayla
func (h *Handlers) UzakKopyaAnahtarOnayla(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Parmak    string `json:"parmak"`
		Sakladim  bool   `json:"sakladim"`
		Indirildi bool   `json:"indirildi"`
	}
	if err := uzakJSON(r, &in); err != nil || in.Parmak == "" {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek")
		return
	}
	if !in.Sakladim {
		httpx.WriteError(w, http.StatusBadRequest, "özel anahtarı sakladığınızı onaylamalısınız")
		return
	}
	if err := uzak.AnahtarOnayla(r.Context(), h.DB, in.Parmak, in.Indirildi); err != nil {
		uzakHataYaz(w, err)
		return
	}
	uzakDenetimYaz(h.DB, r, "yedek.uzak.anahtar_onay", "anahtar "+uzak.KisaParmak(in.Parmak), "Şifreleme anahtarı etkinleştirildi", true)
	h.UzakKopyaDurum(w, r)
}

// UzakKopyaAnahtarIceAktar — POST /admin/backups/uzak-kopya/anahtar-ice-aktar
func (h *Handlers) UzakKopyaAnahtarIceAktar(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Alici string `json:"alici"`
	}
	if err := uzakJSON(r, &in); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek")
		return
	}
	b, err := uzak.AnahtarIceAktar(r.Context(), h.DB, in.Alici)
	if err != nil {
		uzakHataYaz(w, err)
		return
	}
	uzakDenetimYaz(h.DB, r, "yedek.uzak.anahtar_ice", "anahtar "+b.KisaParmak, "Açık anahtar içe aktarıldı ("+b.Sifreleme+"); özel anahtar kullanıcıda", true)
	h.UzakKopyaDurum(w, r)
}

// UzakKopyaIsler — GET /admin/backups/uzak-kopya/isler
func (h *Handlers) UzakKopyaIsler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sayfa, _ := strconv.Atoi(q.Get("sayfa"))
	adet, _ := strconv.Atoi(q.Get("adet"))
	durum := strings.TrimSpace(q.Get("durum"))
	liste, toplam, err := uzak.IsListe(r.Context(), h.DB, sayfa, adet, durum)
	if err != nil {
		uzakHataYaz(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"isler": liste, "toplam": toplam})
}

func uzakID(r *http.Request, ad string) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, ad), 10, 64)
	return id, err == nil && id > 0
}

// UzakKopyaSimdiGonder — POST /admin/backups/uzak-kopya/simdi-gonder/{yedek_id}
func (h *Handlers) UzakKopyaSimdiGonder(w http.ResponseWriter, r *http.Request) {
	yid, ok := uzakID(r, "yedek_id")
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz yedek")
		return
	}
	isID, err := uzak.SimdiGonder(r.Context(), h.DB, yid)
	if err != nil {
		uzakHataYaz(w, err)
		return
	}
	uzakDenetimYaz(h.DB, r, "yedek.uzak.simdi", "yedek "+strconv.FormatInt(yid, 10), "Yedek uzağa gönderilmek üzere sıraya alındı", true)
	if i := uzakIsci.Load(); i != nil {
		i.Tetikle()
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"ok": true, "is_id": isID})
}

// UzakKopyaYenidenDene — POST /admin/backups/uzak-kopya/yeniden-dene/{is_id}
func (h *Handlers) UzakKopyaYenidenDene(w http.ResponseWriter, r *http.Request) {
	id, ok := uzakID(r, "is_id")
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz iş")
		return
	}
	if err := uzak.YenidenDene(r.Context(), h.DB, id); err != nil {
		uzakHataYaz(w, err)
		return
	}
	uzakDenetimYaz(h.DB, r, "yedek.uzak.yeniden", "iş "+strconv.FormatInt(id, 10), "Uzak kopya işi yeniden denenecek", true)
	if i := uzakIsci.Load(); i != nil {
		i.Tetikle()
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

// UzakKopyaIptal — POST /admin/backups/uzak-kopya/iptal/{is_id}
func (h *Handlers) UzakKopyaIptal(w http.ResponseWriter, r *http.Request) {
	id, ok := uzakID(r, "is_id")
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz iş")
		return
	}
	i := uzakIsci.Load()
	if i == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "uzak kopya işçisi çalışmıyor")
		return
	}
	if err := i.Iptal(r.Context(), id); err != nil {
		uzakHataYaz(w, err)
		return
	}
	uzakDenetimYaz(h.DB, r, "yedek.uzak.iptal", "iş "+strconv.FormatInt(id, 10), "Uzak kopya işi iptal edildi", true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// UzakKopyaSaklamaOnizle — GET /admin/backups/uzak-kopya/saklama-onizle
func (h *Handlers) UzakKopyaSaklamaOnizle(w http.ResponseWriter, r *http.Request) {
	ayar, err := uzak.AyarOku(r.Context(), h.DB)
	if err != nil {
		uzakHataYaz(w, err)
		return
	}
	var adaylar []uzak.Aday
	if ayar.HedefID > 0 {
		adaylar, err = uzak.SaklamaAdaylari(r.Context(), h.DB, ayar)
		if err != nil {
			uzakHataYaz(w, err)
			return
		}
	}
	if adaylar == nil {
		adaylar = []uzak.Aday{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"silinecek": adaylar})
}

// UzakKopyaAnahtarYenile — POST /admin/backups/uzak-kopya/anahtar-yenile (Gelişmiş).
// Yeni anahtarı onaysız etkinleştirir; eski anahtar 'emekli' olarak sunucuda kalır.
func (h *Handlers) UzakKopyaAnahtarYenile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Hibrit bool `json:"hibrit"`
	}
	if r.ContentLength != 0 {
		if err := uzakJSON(r, &in); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek")
			return
		}
	}
	b, _, err := uzak.EtkinKur(r.Context(), h.DB, in.Hibrit, true)
	if err != nil {
		uzakHataYaz(w, err)
		return
	}
	uzakDenetimYaz(h.DB, r, "yedek.uzak.anahtar_yenile", "anahtar "+b.KisaParmak, "Şifreleme anahtarı yenilendi ("+b.Sifreleme+")", true)
	h.UzakKopyaDurum(w, r)
}

// UzakKopyaKurtarmaAnahtari — POST /admin/backups/uzak-kopya/kurtarma-anahtari.
// Sunucuda saklı anahtarları (etkin + eski) indirilebilir dosya olarak verir.
// Özel anahtar YALNIZ yanıt gövdesindedir: günlük/denetim/başka yanıt yok.
func (h *Handlers) UzakKopyaKurtarmaAnahtari(w http.ResponseWriter, r *http.Request) {
	ad, metin, _, err := uzak.KurtarmaAnahtarlari(r.Context(), h.DB, time.Now())
	if err != nil {
		uzakHataYaz(w, err)
		return
	}
	uzakDenetimYaz(h.DB, r, "yedek.uzak.kurtarma_indir", "uzak kopya", "Kurtarma anahtarı indirildi", true)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+ad+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(metin))
}

func topluN(r *http.Request, v string) (int, bool) {
	if v == "" {
		return uzak.TopluVarsayilanN, true
	}
	n, err := strconv.Atoi(v)
	return n, err == nil
}

// UzakKopyaMevcutlariOnizle — GET /admin/backups/uzak-kopya/mevcutlari-gonder?n=7
func (h *Handlers) UzakKopyaMevcutlariOnizle(w http.ResponseWriter, r *http.Request) {
	n, ok := topluN(r, r.URL.Query().Get("n"))
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz yedek sayısı")
		return
	}
	_, on, err := uzak.TopluAdaylar(r.Context(), h.DB, n)
	if err != nil {
		uzakHataYaz(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, on)
}

// UzakKopyaMevcutlariGonder — POST /admin/backups/uzak-kopya/mevcutlari-gonder {n}
func (h *Handlers) UzakKopyaMevcutlariGonder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		N int `json:"n"`
	}
	if r.ContentLength != 0 {
		if err := uzakJSON(r, &in); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek")
			return
		}
	}
	if in.N == 0 {
		in.N = uzak.TopluVarsayilanN
	}
	kuyruga, atlanan, err := uzak.TopluKuyrugaAl(r.Context(), h.DB, in.N)
	if err != nil {
		uzakHataYaz(w, err)
		return
	}
	uzakDenetimYaz(h.DB, r, "yedek.uzak.toplu", "uzak kopya",
		strconv.Itoa(kuyruga)+" mevcut yedek uzağa gönderilmek üzere sıraya alındı", true)
	if i := uzakIsci.Load(); i != nil && kuyruga > 0 {
		i.Tetikle()
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"ok": true, "kuyruga": kuyruga, "atlanan": atlanan})
}

// UzakKopyaYedekDurumlari — GET /admin/backups/uzak-kopya/yedek-durumlari?ids=1,2,3
func (h *Handlers) UzakKopyaYedekDurumlari(w http.ResponseWriter, r *http.Request) {
	var ids []int64
	for _, p := range strings.Split(r.URL.Query().Get("ids"), ",") {
		if id, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64); err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	ayar, err := uzak.AyarOku(r.Context(), h.DB)
	if err != nil {
		uzakHataYaz(w, err)
		return
	}
	d, err := uzak.YedekDurumlari(r.Context(), h.DB, ids)
	if err != nil {
		uzakHataYaz(w, err)
		return
	}
	out := map[string]string{}
	for id, v := range d {
		out[strconv.FormatInt(id, 10)] = v
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"aktif": ayar.Aktif, "durumlar": out})
}
