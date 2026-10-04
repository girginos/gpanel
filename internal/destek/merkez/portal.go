package merkez

// Portal iç API'si (/ic/v1/*) — müşteri portalı (app.girginos.io) eşleştirmeyi BURADAN başlatır.
//
// Güvenlik modeli:
//   - Yalnız iç dinleyicide (ic_dinle; yapılandırma doğrulaması loopback dışını reddeder).
//     Portala erişim ayrı katmandır (SSH tünel / allowlist'li ters vekil) — bu fazın dışında.
//   - Her istek "Authorization: Bearer <portal-ic-anahtar>" ister; karşılaştırma iki tarafın
//     SHA-256'sı üzerinde sabit zamanlıdır (uzunluk sızmaz). Sır yapılandırılmamışsa uçlar 503.
//   - Gövde ≤4 KiB, katı JSON (bilinmeyen alan → 400); genel ve kod başına oran sınırı.
//   - Kodu MERKEZ üretir; kod yalnız bu yanıtta döner, bellekte yalnız özeti durur, ≤15 dk,
//     tek kullanımlık. Doğrulanmamış / çevrimdışı / parmak izi çakışmalı sunucuya kod VERİLMEZ.
//   - Konuşma içeriği bu API'den ASLA dönmez; bağlantı listesi yalnız üstveri taşır.
//   - Hata gövdesi {"hata":"<kod>"}; hiçbir yanıt/log sır ya da bilet içeriği taşımaz.

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"girginospanel/internal/destek/protokol"
)

const (
	icGovdeSiniri   = 4 << 10
	icGenelDakika   = 300 // tüm /ic/v1 istekleri / dk
	icKodDakika     = 60  // eslesme-kodu toplam / dk
	icKodSunucuSaat = 20  // eslesme-kodu, sunucu (parmak izi) başına / saat
	icListeSiniri   = 200
)

var (
	parmakIziDeseni  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	baglantiIDDeseni = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
)

type portalAPI struct {
	m         *Merkez
	genel     *kayanPencere
	kodGenel  *kayanPencere
	kodSunucu *kayanPencere
}

func yeniPortalAPI(m *Merkez) *portalAPI {
	return &portalAPI{m: m,
		genel:     yeniKayanPencere(icGenelDakika, time.Minute),
		kodGenel:  yeniKayanPencere(icKodDakika, time.Minute),
		kodSunucu: yeniKayanPencere(icKodSunucuSaat, time.Hour),
	}
}

func (p *portalAPI) rotalar(mux *http.ServeMux) {
	mux.HandleFunc("GET /ic/v1/sunucu", p.yetki(p.sunucu))
	mux.HandleFunc("POST /ic/v1/eslesme-kodu", p.yetki(p.eslesmeKodu))
	mux.HandleFunc("GET /ic/v1/baglantilar", p.yetki(p.baglantilar))
	mux.HandleFunc("POST /ic/v1/baglanti/iptal", p.yetki(p.baglantiIptal))
}

func icYaz(w http.ResponseWriter, durum int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(durum)
	_ = json.NewEncoder(w).Encode(v)
}

func icHata(w http.ResponseWriter, durum int, kod string) {
	icYaz(w, durum, map[string]string{"hata": kod})
}

// yetki: Bearer doğrulaması + genel oran sınırı. Başarısız denemeler sayılır (metrik: ic_yetkisiz).
func (p *portalAPI) yetki(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sir := p.m.sir.PortalIcAnahtar
		if sir == "" {
			icHata(w, http.StatusServiceUnavailable, "ic_api_kapali")
			return
		}
		v := r.Header.Get("Authorization")
		const onek = "Bearer "
		if len(v) <= len(onek) || !strings.EqualFold(v[:len(onek)], onek) {
			p.m.say("ic_yetkisiz")
			icHata(w, http.StatusUnauthorized, "yetkisiz")
			return
		}
		a, b := sha256.Sum256([]byte(v[len(onek):])), sha256.Sum256([]byte(sir))
		if subtle.ConstantTimeCompare(a[:], b[:]) != 1 {
			p.m.say("ic_yetkisiz")
			icHata(w, http.StatusUnauthorized, "yetkisiz")
			return
		}
		if izin, _ := p.genel.Izin("ic", p.m.saat()); !izin {
			icHata(w, http.StatusTooManyRequests, "kota")
			return
		}
		h(w, r)
	}
}

// govde: ≤4 KiB, katı JSON, tek nesne.
func govdeCoz(w http.ResponseWriter, r *http.Request, hedef any) bool {
	b, err := io.ReadAll(io.LimitReader(r.Body, icGovdeSiniri+1))
	if err != nil {
		icHata(w, http.StatusBadRequest, "gecersiz_istek")
		return false
	}
	if len(b) > icGovdeSiniri {
		icHata(w, http.StatusRequestEntityTooLarge, "govde_buyuk")
		return false
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if d.Decode(hedef) != nil || d.More() {
		icHata(w, http.StatusBadRequest, "gecersiz_istek")
		return false
	}
	return true
}

// sunucuDurumu: parmak izine karşılık gelen sunucunun anlık görünümü.
type sunucuDurumu struct {
	bulundu    bool
	cevrimici  bool
	dogrulandi bool
	cakisma    bool // aynı parmak izini DOĞRULANMIŞ iki cihaz aynı anda çevrimiçi iddia ediyor
	cihaz      Cihaz
}

// sunucuCoz: parmak izi → cihaz. Önce doğrulanmış çevrimiçi cihaz, sonra en son görülen
// doğrulanmış, sonra en son görülen herhangi bir cihaz. DOĞRULANMAMIŞ cihaz parmak izini
// "sahiplenemez": doğrulanmış cihaz varken seçilmez.
func (m *Merkez) sunucuCoz(parmakIzi string) sunucuDurumu {
	simdi := m.saat()
	cs := m.depo.CihazlarParmakIzi(parmakIzi)
	var d sunucuDurumu
	if len(cs) == 0 {
		return d
	}
	d.bulundu = true
	var dogrulanan, cevrimici []Cihaz
	for _, c := range cs {
		if c.Dogrulandi(simdi) {
			dogrulanan = append(dogrulanan, c)
			if m.ajan.Cevrimici(c.SunucuKimligi) {
				cevrimici = append(cevrimici, c)
			}
		}
	}
	sonGorulen := func(l []Cihaz) Cihaz {
		sort.Slice(l, func(i, j int) bool { return l[i].SonGorulme.After(l[j].SonGorulme) })
		return l[0]
	}
	switch {
	case len(cevrimici) > 1:
		d.cakisma = true
		d.cihaz = sonGorulen(cevrimici)
	case len(cevrimici) == 1:
		d.cihaz = cevrimici[0]
	case len(dogrulanan) > 0:
		d.cihaz = sonGorulen(dogrulanan)
	default:
		d.cihaz = sonGorulen(cs)
	}
	d.dogrulandi = d.cihaz.Dogrulandi(simdi)
	d.cevrimici = m.ajan.Cevrimici(d.cihaz.SunucuKimligi)
	return d
}

// ---- GET /ic/v1/sunucu?parmak_izi=<64hex> ----

func (p *portalAPI) sunucu(w http.ResponseWriter, r *http.Request) {
	fp := r.URL.Query().Get("parmak_izi")
	if !parmakIziDeseni.MatchString(fp) {
		icHata(w, http.StatusBadRequest, "gecersiz_istek")
		return
	}
	d := p.m.sunucuCoz(fp)
	y := map[string]any{"bulundu": d.bulundu, "cevrimici": false, "dogrulandi": false, "cakisma": false,
		"sunucu_kimligi": "", "panel_surumu": "", "son_gorulme": int64(0)}
	if d.bulundu {
		y["cevrimici"], y["dogrulandi"], y["cakisma"] = d.cevrimici, d.dogrulandi, d.cakisma
		y["sunucu_kimligi"], y["panel_surumu"], y["son_gorulme"] = d.cihaz.SunucuKimligi, d.cihaz.PanelSurumu, d.cihaz.SonGorulme.Unix()
	}
	icYaz(w, http.StatusOK, y)
}

// ---- POST /ic/v1/eslesme-kodu ----

type kodIstegi struct {
	ParmakIzi       string `json:"parmak_izi"`
	Duzey           string `json:"duzey"`
	SureGun         int    `json:"sure_gun"`
	PortalKullanici string `json:"portal_kullanici_id"`
	AydinlatmaSurum string `json:"aydinlatma_surum"`
}

var (
	portalKullaniciDeseniIc = regexp.MustCompile(`^[A-Za-z0-9_.:@-]{1,64}$`)
	aydinlatmaDeseniIc      = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)
)

func (p *portalAPI) eslesmeKodu(w http.ResponseWriter, r *http.Request) {
	var req kodIstegi
	if !govdeCoz(w, r, &req) {
		return
	}
	duzey := protokol.Duzey(req.Duzey)
	if !parmakIziDeseni.MatchString(req.ParmakIzi) || (duzey != protokol.DuzeyOkuma && duzey != protokol.DuzeyOnarim && duzey != protokol.DuzeyTam) ||
		req.SureGun < 1 || req.SureGun > protokol.EslesmeEnCokSureGun ||
		!portalKullaniciDeseniIc.MatchString(req.PortalKullanici) || !aydinlatmaDeseniIc.MatchString(req.AydinlatmaSurum) {
		icHata(w, http.StatusBadRequest, "gecersiz_istek")
		return
	}
	simdi := p.m.saat()
	if izin, _ := p.kodGenel.Izin("*", simdi); !izin {
		icHata(w, http.StatusTooManyRequests, "kota")
		return
	}
	if izin, _ := p.kodSunucu.Izin(req.ParmakIzi, simdi); !izin {
		icHata(w, http.StatusTooManyRequests, "kota")
		return
	}
	d := p.m.sunucuCoz(req.ParmakIzi)
	switch {
	case !d.bulundu:
		icHata(w, http.StatusNotFound, "sunucu_yok")
		return
	case d.cakisma:
		icHata(w, http.StatusConflict, "parmak_izi_cakisma")
		return
	case !d.dogrulandi:
		icHata(w, http.StatusForbidden, "dogrulanmadi")
		return
	case !d.cevrimici:
		icHata(w, http.StatusConflict, "cevrimdisi")
		return
	}
	kod, bitis, err := p.m.ajan.kodUret(d.cihaz.SunucuKimligi, duzey, req.SureGun, req.PortalKullanici, req.AydinlatmaSurum)
	if errors.Is(err, ErrKodKota) {
		icHata(w, http.StatusTooManyRequests, "kota")
		return
	}
	if err != nil {
		p.m.log.Error("portal kodu üretilemedi", "hata", err)
		icHata(w, http.StatusInternalServerError, "ic_hata")
		return
	}
	p.m.say("ic_kod")
	p.m.log.Info("portal eşleştirme kodu verildi", "sunucu", d.cihaz.SunucuKimligi, "portal_kullanici", req.PortalKullanici, "duzey", duzey, "sure_gun", req.SureGun)
	botURL := ""
	if p.m.yap.BotKullaniciAdi != "" {
		botURL = "https://t.me/" + url.PathEscape(p.m.yap.BotKullaniciAdi) + "?start=" + kod
	}
	icYaz(w, http.StatusOK, map[string]any{"kod": kod, "gorunen": protokol.EslesmeKoduGorunen(kod), "bitis": bitis.Unix(),
		"bot_url": botURL, "sunucu_kimligi": d.cihaz.SunucuKimligi})
}

// ---- GET /ic/v1/baglantilar?parmak_izi= ----

// sunucuKumesi: parmak izini iddia eden tüm cihazların kimlikleri (yeniden kurulumla anahtarı
// değişmiş eski cihazların bağlantıları da yönetilebilsin).
func (m *Merkez) sunucuKumesi(parmakIzi string) map[string]bool {
	k := map[string]bool{}
	for _, c := range m.depo.CihazlarParmakIzi(parmakIzi) {
		k[c.SunucuKimligi] = true
	}
	return k
}

func (p *portalAPI) baglantilar(w http.ResponseWriter, r *http.Request) {
	fp := r.URL.Query().Get("parmak_izi")
	if !parmakIziDeseni.MatchString(fp) {
		icHata(w, http.StatusBadRequest, "gecersiz_istek")
		return
	}
	out := []map[string]any{}
	var tum []Baglanti
	for s := range p.m.sunucuKumesi(fp) {
		tum = append(tum, p.m.depo.SunucuBaglantilari(s)...)
	}
	sort.Slice(tum, func(i, j int) bool { return tum[i].Guncelleme.After(tum[j].Guncelleme) })
	if len(tum) > icListeSiniri {
		tum = tum[:icListeSiniri]
	}
	for _, b := range tum {
		out = append(out, map[string]any{
			"baglanti_id": b.BaglantiID, "sunucu_kimligi": b.SunucuKimligi, "tg_kisa": tgKisa(b.TgUserID),
			"durum": b.Durum, "rol": b.Rol, "duzey": b.Duzey,
			"kvkk_kabul": !b.KVKKKabul.IsZero(), "kvkk_surum": b.KVKKSurum, "kvkk_kaynak": b.KVKKKaynak,
			"guncelleme": b.Guncelleme.Unix(),
		})
	}
	icYaz(w, http.StatusOK, map[string]any{"baglantilar": out})
}

// ---- POST /ic/v1/baglanti/iptal ----

type iptalIstegi struct {
	ParmakIzi  string `json:"parmak_izi"`
	BaglantiID string `json:"baglanti_id"`
}

func (p *portalAPI) baglantiIptal(w http.ResponseWriter, r *http.Request) {
	var req iptalIstegi
	if !govdeCoz(w, r, &req) {
		return
	}
	if !parmakIziDeseni.MatchString(req.ParmakIzi) || !baglantiIDDeseni.MatchString(req.BaglantiID) {
		icHata(w, http.StatusBadRequest, "gecersiz_istek")
		return
	}
	b, ok := p.m.depo.BaglantiAl(req.BaglantiID)
	// Başka sunucunun bağlantısı varlığı sızdırmadan 404
	if !ok || !p.m.sunucuKumesi(req.ParmakIzi)[b.SunucuKimligi] {
		icHata(w, http.StatusNotFound, "baglanti_yok")
		return
	}
	iletildi := p.m.ajan.portalIptal(b)
	p.m.say("ic_iptal")
	p.m.log.Info("portal bağlantıyı iptal etti", "sunucu", b.SunucuKimligi, "ajana_iletildi", iletildi)
	icYaz(w, http.StatusOK, map[string]any{"ok": true, "ajana_iletildi": iletildi})
}
