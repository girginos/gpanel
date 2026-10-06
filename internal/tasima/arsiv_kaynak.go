package tasima

// arsiv_kaynak.go — ARŞİV kaynak sürücüleri (arsiv-cpanel, arsiv-directadmin) ve keşif.
//
// Mevcut taşıma altyapısına (kuyruk, ilerleme, geri alma, denetim) yeni bir kaynak türü
// olarak takılır — paralel yol YOK: canlı SSH kaynakları (`cpanel`, `directadmin`) ile
// KARIŞMAZ, ayrı kodla ayrı kapatılır. Kaynak ağdan değil arşivden okunur
// (internal/tasima/arsiv); iş kuyruğu/kalem/geri alma HesapAktar'ın kendisidir ve adımlar
// (dosya/DB/DNS/posta/cron) `k.arsiv != nil` iken arşivden beslenir (arsiv_aktar.go).
//
// Keşif ASENKRONdur: yüzlerce GiB'lık arşivin envanteri dakikalar-saatler sürer, HTTP
// isteği tutulmaz. POST /tasima/arsiv/kesif → 202 + oturum; GET .../kesif/{id} yoklanır.
// Sonuç ('oturum' satırı: kesif_json = hesaplar, ayarlar = arsivOturumMeta) iki saat yaşar;
// başlatma YALNIZ bu sunucu kopyasından yapılır (istemci gövdesi yetki değildir).

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

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/httpx"
	"girginospanel/internal/tasima/arsiv"
)

// arsivBaglam — işin açık arşivi (kalemler arası paylaşılır; salt okunur).
type arsivBaglam struct {
	A         *arsiv.Arsiv
	M         *arsiv.Manifest
	Yol       string
	Boyut     int64
	DumpSinir int64 // tek veritabanı dökümü üst sınırı (SecenekYol/SecenekYukleme)

	// Cron: arşivden çevrilecek satırlar alan adına atanmış (cronAta, iş başında bir kez);
	// CronRed alana atanamayan/seçilmeyen alana düşen satırlardır ve İLK kalemin raporuna
	// yazılır (alan bazlı raporlar o kalemin kendi satırlarını ayrıca ekler).
	CronAtama      map[string][]string
	CronRed        []cronRed
	cronRaporlandi bool
	cronNot        map[string]bool
}

// arsivHostAdi — iş/oturum satırlarında kaynak_host yerine yazılan sabit etiket (yol/ad
// satıra, günlüğe ve bayiye gösterilmez).
const arsivHostAdi = "arsiv"

func init() {
	kaynakKaydet(&kaynakSurucu{
		Kod: "arsiv-cpanel", Etiket: "cPanel yedek arşivi (cpmove)", Olgunluk: olgunlukDeneysel, Sira: 50,
		VarsayilanEtkin: false, Arsiv: true,
		Yetenekler: Yetenek{Dosya: true, Veritabani: true, DNS: true, Posta: true, Cron: true},
		kesif:      arsivKesfet,
	})
	kaynakKaydet(&kaynakSurucu{
		Kod: "arsiv-directadmin", Etiket: "DirectAdmin yedek arşivi", Olgunluk: olgunlukDeneysel, Sira: 60,
		VarsayilanEtkin: false, Arsiv: true,
		// DNS: yedek biçiminde bölge okunmuyor (arşiv paketi); varsayılan şablon kurulur.
		Yetenekler: Yetenek{Dosya: true, Veritabani: true, Posta: true, Cron: true},
		kesif:      arsivKesfet,
	})
}

// arsivTuruUyar — sürücü kodu ile arşivin tanınan türü tutarlı mı.
func arsivTuruUyar(tip string, m *arsiv.Manifest) error {
	ad := map[arsiv.Tur]string{arsiv.TurCPanel: "cPanel", arsiv.TurDirectAdmin: "DirectAdmin"}
	switch {
	case tip == "arsiv-cpanel" && m.Tur != arsiv.TurCPanel:
		return fmt.Errorf("bu dosya cPanel yedeği değil (%s olarak tanındı); doğru kaynak türünü seçin", ad[m.Tur])
	case tip == "arsiv-directadmin" && m.Tur != arsiv.TurDirectAdmin:
		return fmt.Errorf("bu dosya DirectAdmin yedeği değil (%s olarak tanındı); doğru kaynak türünü seçin", ad[m.Tur])
	}
	return nil
}

// arsivKesfet — kaynakSurucu.kesif: açık arşivden site listesi.
func arsivKesfet(_ context.Context, k *Kaynak) ([]Hesap, error) {
	if k.arsiv == nil || k.arsiv.M == nil {
		return nil, fmt.Errorf("arşiv açık değil")
	}
	return arsivHesaplari(k.arsiv.M), nil
}

// arsivHesaplari — manifest → taşınabilir siteler. Ana/ek/alt alan adları ayrı sitedir
// (canlı cPanel keşfiyle aynı model); park alan adları taşınmaz (ana alana not düşülür).
// Veritabanları hesap geneli olduğundan YALNIZ ana alana atanır. Güvenlik kuralıyla
// reddedilmiş döküm de listelenir: iş o veritabanında NEDENİYLE hata verir (sessizce
// atlanmaz; kullanıcı veritabanı seçeneğini kapatıp sitenin dosyalarını taşıyabilir).
func arsivHesaplari(m *arsiv.Manifest) []Hesap {
	var dbler []string
	var red []string
	for _, v := range m.Veritabanlari {
		if !reDBAd.MatchString(v.Ad) {
			continue
		}
		dbler = append(dbler, v.Ad)
		if v.Reddedildi != "" {
			red = append(red, v.Ad)
		}
	}
	var parklar []string
	for _, a := range m.Alanlar {
		if a.Tur == "park" {
			parklar = append(parklar, a.Ad)
		}
	}
	var out []Hesap
	for _, a := range m.Alanlar {
		if a.Tur == "park" || !reAlanAdi.MatchString(a.Ad) {
			continue
		}
		hs := Hesap{KaynakHesap: m.Kullanici, AlanAdi: a.Ad, WebRoot: a.BelgeKoku}
		var notlar []string
		switch a.Tur {
		case "ana":
			hs.DBler = dbler
			if len(parklar) > 0 {
				notlar = append(notlar, "park alan adları taşınmaz: "+kisaltAlan(strings.Join(parklar, ", "), 160))
			}
			if len(red) > 0 {
				notlar = append(notlar, "güvenlik kuralıyla reddedilen döküm: "+kisaltAlan(strings.Join(red, ", "), 120))
			}
		case "ek":
			notlar = append(notlar, "ek (addon) alan adı — veritabanı ana alan adıyla taşınır")
		case "alt":
			notlar = append(notlar, "alt alan adı — ayrı site olarak açılır")
		}
		if a.BelgeKoku == "" {
			notlar = append(notlar, "belge kökü yedekte yok — dosya taşınmaz")
		}
		hs.Not = strings.Join(notlar, "; ")
		out = append(out, hs)
	}
	return out
}

// ---------------------------------------------------------------------------
// Oturum (keşif sonucu) — tasima_isleri 'oturum' satırı
// ---------------------------------------------------------------------------

type arsivOzet struct {
	ArsivBayt   int64 `json:"arsiv_bayt"`
	UyeSayisi   int   `json:"uye_sayisi"`
	DosyaSayisi int   `json:"dosya_sayisi"`
	ToplamBayt  int64 `json:"toplam_bayt"`
	DBSayisi    int   `json:"db_sayisi"`
	PostaSayisi int   `json:"posta_sayisi"`
	CronSayisi  int   `json:"cron_sayisi"`
	DNSSayisi   int   `json:"dns_sayisi"`
}

type arsivOnkontrol struct {
	ArsivBayt  int64  `json:"arsiv_bayt"`
	GerekliBos int64  `json:"gerekli_bos_bayt"`
	Yeterli    bool   `json:"yeterli"`
	Neden      string `json:"neden,omitempty"`
}

// arsivOturumMeta — oturum satırının ayarlar sütunu. Yol/yükleme kimliği YALNIZ burada
// (sunucu tarafı); istemciye dönen yanıta GİRMEZ.
type arsivOturumMeta struct {
	Durum        string         `json:"durum"` // kesif | hazir | hata
	Hata         string         `json:"hata,omitempty"`
	Tip          string         `json:"tip"`
	Yol          string         `json:"yol,omitempty"`
	YuklemeID    int64          `json:"yukleme_id,omitempty"`
	ParmakIzi    arsivParmakIzi `json:"parmak_izi"`
	Baslangic    int64          `json:"baslangic"`
	Uyarilar     []string       `json:"uyarilar,omitempty"`
	Tasinmayacak []string       `json:"tasinmayacak,omitempty"`
	Ozet         arsivOzet      `json:"ozet"`
	Onkontrol    arsivOnkontrol `json:"onkontrol"`
}

const (
	arsivKesifTimeout = 6 * time.Hour
	arsivMaxUyari     = 100 // oturum satırına yazılan uyarı sayısı (ayarlar TEXT 64 KiB)
	arsivUyariUzunluk = 240
)

var (
	// arsivKesifSem — eşzamanlı arşiv incelemesi (her biri tek geçişlik ağır okuma).
	arsivKesifSem = make(chan struct{}, 2)
	// arsivKesifAktif — çalışan keşfin oturum kimlikleri (panel yeniden başlayınca 'kesif'
	// durumunda kalan satırı yetim olarak ayırt eder).
	arsivKesifAktif sync.Map
)

func cronGorevSayisi(satirlar []string) int {
	n := 0
	for _, s := range satirlar {
		if !reCronOrtam.MatchString(s) {
			n++
		}
	}
	return n
}

func kisaMetin(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func kisaListe(l []string, max int) []string {
	if len(l) > max {
		l = l[:max]
	}
	out := make([]string, len(l))
	for i, s := range l {
		out[i] = kisaMetin(s, arsivUyariUzunluk)
	}
	return out
}

func arsivStatus(err error) int {
	switch {
	case errors.Is(err, errArsivYolBay):
		return http.StatusForbidden
	case errors.Is(err, errArsivYok):
		return http.StatusNotFound
	case errors.Is(err, errArsivBoyut):
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}

type arsivKesifGirdi struct {
	Tip       string `json:"tip"`
	Yol       string `json:"yol"`
	YuklemeID int64  `json:"yukleme_id"`
}

// ArsivKesif — POST /tasima/arsiv/kesif → 202 {oturum_id, durum:"kesif"}
func (h *Handlers) ArsivKesif(w http.ResponseWriter, r *http.Request) {
	kp, ok := h.arsivKapi(w, r)
	if !ok {
		return
	}
	if !bayiHizIzni(kp.Kapsam) {
		httpx.WriteError(w, http.StatusTooManyRequests, "çok sık deneme — bir dakika sonra yeniden deneyin")
		return
	}
	var g arsivKesifGirdi
	if err := decodeJSON(w, r, 8192, &g); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz istek")
		return
	}
	g.Tip = strings.ToLower(strings.TrimSpace(g.Tip))
	sur, ok := surucuBul(g.Tip)
	if !ok || !sur.Arsiv {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz kaynak tipi")
		return
	}
	ctx := r.Context()
	if err := h.kaynakKullanilabilir(ctx, kp.Kapsam, g.Tip); err != nil {
		kod := http.StatusBadRequest
		if err == errTasimaKapali {
			kod = http.StatusForbidden
		}
		httpx.WriteError(w, kod, err.Error())
		return
	}
	bilgi, err := h.arsivKaynakCoz(ctx, kp.Kapsam, strings.TrimSpace(g.Yol), g.YuklemeID)
	if err != nil {
		httpx.WriteError(w, arsivStatus(err), hataMetni(kp.Yonetici(), err).Error())
		return
	}
	select {
	case arsivKesifSem <- struct{}{}:
	default:
		httpx.WriteError(w, http.StatusConflict, "başka bir arşiv inceleniyor; bitince yeniden deneyin")
		return
	}
	// Yol YALNIZ kök yolu girdisinde saklanır (yükleme dosyasının yolu kimlikten türetilir).
	meta := arsivOturumMeta{Durum: "kesif", Tip: g.Tip, YuklemeID: bilgi.YuklemeID,
		ParmakIzi: arsivParmakIzi{Boyut: bilgi.Boyut, MtimeNs: bilgi.MtimeNs}, Baslangic: time.Now().Unix()}
	if bilgi.YuklemeID == 0 {
		meta.Yol = bilgi.Yol
	}
	mj, _ := json.Marshal(meta)
	// Aynı kapsamın eski arşiv oturumları silinir (tek taze oturum).
	_, _ = h.DB.ExecContext(ctx, `DELETE FROM tasima_isleri WHERE durum='oturum' AND kaynak_host=? AND sahip_rid=?`, arsivHostAdi, kp.RID)
	res, err := h.DB.ExecContext(ctx,
		`INSERT INTO tasima_isleri (kaynak_tip, kaynak_host, kaynak_port, kaynak_kullanici, durum, toplam, ayarlar,
		   son_kullanim, gecerlilik, sahip_rid, baslatan_uid, arsiv_yukleme_id)
		 VALUES (?,?,0,?, 'oturum', 0, ?, NOW(), (NOW() + INTERVAL ? SECOND), ?, NULLIF(?,0), NULLIF(?,0))`,
		g.Tip, arsivHostAdi, arsivHostAdi, string(mj), int((arsivKesifTimeout + oturumTTL).Seconds()), kp.RID, kp.UID, bilgi.YuklemeID)
	if err != nil {
		<-arsivKesifSem
		log.Printf("tasima: arsiv oturumu kaydedilemedi: %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, "keşif başlatılamadı")
		return
	}
	id, _ := res.LastInsertId()
	arsivKesifAktif.Store(id, true)
	go h.arsivKesifCalis(id, kp.Kapsam, g.Tip, bilgi, meta.Baslangic)
	httpx.Denetim(h.DB, r, kp.UID, kp.Kul, "tasima_arsiv_kesif", strconv.FormatInt(id, 10), g.Tip, kp.RID, true)
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"oturum_id": id, "durum": "kesif"})
}

// arsivAcSecenek — Oku seçenekleri; testler ucuzlatır.
var arsivAc = arsiv.AcHesap

func (h *Handlers) arsivKesifCalis(id int64, kp Kapsam, tip string, bilgi *arsivKaynakBilgi, baslangic int64) {
	defer func() { <-arsivKesifSem; arsivKesifAktif.Delete(id) }()
	meta := arsivOturumMeta{Tip: tip, YuklemeID: bilgi.YuklemeID, Baslangic: baslangic,
		ParmakIzi: arsivParmakIzi{Boyut: bilgi.Boyut, MtimeNs: bilgi.MtimeNs}}
	if bilgi.YuklemeID == 0 {
		meta.Yol = bilgi.Yol
	}
	hataYaz := func(err error) {
		log.Printf("tasima: arsiv kesfi basarisiz (oturum=%d bayi=%d): %v", id, kp.RID, err)
		meta.Durum, meta.Hata = "hata", kisaMetin(hataMetni(kp.Yonetici(), err).Error(), 400)
		mj, _ := json.Marshal(meta)
		_, _ = h.DB.Exec(`UPDATE tasima_isleri SET ayarlar=?, son_kullanim=NOW() WHERE id=? AND durum='oturum'`, string(mj), id)
	}
	defer func() {
		if rec := recover(); rec != nil {
			hataYaz(fmt.Errorf("beklenmeyen hata: %v", rec))
		}
	}()
	ctx, iptal := context.WithTimeout(context.Background(), arsivKesifTimeout)
	defer iptal()
	_, m, err := arsivAc(ctx, bilgi.Yol, bilgi.Secenek)
	if err != nil {
		hataYaz(err)
		return
	}
	if err := arsivTuruUyar(tip, m); err != nil {
		hataYaz(err)
		return
	}
	// Okuma sırasında dosya değiştiyse sonuç güvenilmez.
	if yeni, err := h.arsivKaynakCoz(ctx, kp, meta.Yol, meta.YuklemeID); err != nil || !yeni.parmakIziEslesir(meta.ParmakIzi) {
		hataYaz(errArsivDegisti)
		return
	}
	hesaplar := arsivHesaplari(m)
	meta.Durum = "hazir"
	meta.Uyarilar = kisaListe(m.Uyarilar, arsivMaxUyari)
	meta.Tasinmayacak = kisaListe(m.Tasinmayacak, 20)
	meta.Ozet = arsivOzet{ArsivBayt: bilgi.Boyut, UyeSayisi: m.UyeSayisi, DosyaSayisi: m.DosyaSayisi, ToplamBayt: m.ToplamBayt,
		DBSayisi: len(m.Veritabanlari), PostaSayisi: len(m.Posta), CronSayisi: cronGorevSayisi(m.Cron), DNSSayisi: len(m.DNS)}
	meta.Onkontrol = h.arsivOnkontrol(ctx, kp, bilgi.Boyut, m.ToplamBayt)
	mj, _ := json.Marshal(meta)
	hj, _ := json.Marshal(hesaplar)
	if _, err := h.DB.Exec(
		`UPDATE tasima_isleri SET ayarlar=?, kesif_json=?, toplam=?, son_kullanim=NOW(),
		   gecerlilik=(NOW() + INTERVAL ? SECOND) WHERE id=? AND durum='oturum'`,
		string(mj), string(hj), len(hesaplar), int(oturumTTL.Seconds()), id); err != nil {
		hataYaz(fmt.Errorf("sonuç kaydedilemedi: %w", err))
	}
}

// arsivOnkontrol — hedef disk (arşiv × 2,5) + bayi disk havuzu (açılmış boyut). Neden metni
// sabittir (sayı/sunucu bilgisi içermez).
func (h *Handlers) arsivOnkontrol(ctx context.Context, kp Kapsam, arsivBayt, acilmisBayt int64) arsivOnkontrol {
	o := arsivOnkontrol{ArsivBayt: arsivBayt, GerekliBos: gerekliBosBayt(arsivBayt), Yeterli: true}
	if err := arsivDiskOnkontrol(arsivBayt); err != nil {
		o.Yeterli, o.Neden = false, errArsivDisk.Error()
		if !errors.Is(err, errArsivDisk) {
			log.Printf("tasima: disk onkontrolu: %v", err)
			o.Neden = "hedef disk ölçülemedi"
		}
		return o
	}
	if kp.Bayi() {
		if kalan, sinirli, err := h.bayiDiskKalan(ctx, kp.RID); err != nil || (sinirli && acilmisBayt > kalan) {
			o.Yeterli, o.Neden = false, errArsivKota.Error()
		}
	}
	return o
}

// ArsivKesifDurum — GET /tasima/arsiv/kesif/{id}
func (h *Handlers) ArsivKesifDurum(w http.ResponseWriter, r *http.Request) {
	kp, ok := h.arsivKapi(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		oturumBulunamadi(w)
		return
	}
	// 🔴 Sıra önemli: çalışıyor mu bilgisi DB okumasından ÖNCE alınır. Goroutine sonucu DB'ye
	// yazar, SONRA aktif işaretini siler; ters sırada okumak, tamamlanma anında yoklayan
	// kullanıcıya "panel yeniden başlatıldı" yanlış hatasını gösterirdi.
	_, aktif := arsivKesifAktif.Load(id)
	var ayarlar, kesifJSON string
	err = h.DB.QueryRowContext(r.Context(),
		`SELECT COALESCE(ayarlar,''), COALESCE(kesif_json,'') FROM tasima_isleri
		  WHERE id=? AND durum='oturum' AND kaynak_host=? AND sahip_rid=? AND (gecerlilik IS NULL OR gecerlilik > NOW())`,
		id, arsivHostAdi, kp.RID).Scan(&ayarlar, &kesifJSON)
	if err == sql.ErrNoRows {
		oturumBulunamadi(w)
		return
	}
	var meta arsivOturumMeta
	if err != nil || json.Unmarshal([]byte(ayarlar), &meta) != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "oturum okunamadi")
		return
	}
	_, _ = h.DB.ExecContext(r.Context(), `UPDATE tasima_isleri SET son_kullanim=NOW() WHERE id=? AND sahip_rid=?`, id, kp.RID)
	y := map[string]any{"oturum_id": id, "durum": meta.Durum, "gecen_sn": time.Now().Unix() - meta.Baslangic}
	if meta.Durum == "kesif" && !aktif {
		y["durum"], y["hata"] = "hata", "panel yeniden başlatıldı; arşivi yeniden keşfedin"
	}
	if meta.Hata != "" {
		y["hata"] = meta.Hata
	}
	if meta.Durum == "hazir" {
		var hesaplar []Hesap
		_ = json.Unmarshal([]byte(kesifJSON), &hesaplar)
		out := h.kesifIsaretle(r.Context(), kp.Kapsam, hesaplar)
		y["hesaplar"], y["toplam"] = out, len(out)
		y["uyarilar"], y["tasinmayacak"] = nz(meta.Uyarilar), nz(meta.Tasinmayacak)
		y["ozet"], y["onkontrol"] = meta.Ozet, meta.Onkontrol
	}
	httpx.WriteJSON(w, http.StatusOK, y)
}

// nz — nil dilimi [] olarak yazdırır.
func nz(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
