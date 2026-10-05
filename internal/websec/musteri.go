package websec

// Website Security — HOSTING MÜŞTERİSİ (son kullanıcı) uçları.
//
// ADR: kök/bayi uçları (/websec/*) kapsamı domains.reseller_id üzerinden çözer ve
// müşteri jetonu o uçlara hiç giremez (AdminVeyaReseller). Onları müşteriye açmak
// kapsam kuralını (kapsamAl) ve yanıt biçimlerini (kök/bayi'ye özel alanlar, ham
// install_path, kaynak, kuyruk toplamı) her değişiklikte yeniden denetletirdi. Bu
// yüzden AYRI müşteri uçları var: /domains/{id}/guvenlik[/ozet|/bulgular|/tara].
//
// Kimlik: YALNIZ müşteri jetonu (olağan müşteri oturumu ya da bayi/yöneticinin
// "adına oturum"u — ikisi de MusteriClaims taşır). Domain kimliği URL'den değil
// JETONDAN alınır; URL'deki {id} jetonla eşleşmezse 404 (olmayan domainle aynı yanıt).
// Kök/bayi doğrudan jetonu bu uçlarda 403 alır (kendi sayfaları /websec). Hesabın
// yalnız ana alan adı ve ek alan adları listelenir (alt alanlar taranmıyor: keşif
// ana docroot + ek alan docroot'larını kapsar). API anahtarı bu uçlara GİREMEZ
// (izin tablosunda yok → varsayılan reddet; ayrıca anahtar müşteri jetonu üretmez).
//
// Sızıntı yok: yanıtta başka hesap/kök verisi, mutlak yol (install_path), tarama
// kaynağı, ham feed hatası, kuyruk toplamı, next_estimate YOKTUR. Hata gövdeleri genel.
//
// Tara hakkı (hesap başına): hedef (ana/ek alan) başına 15 dk soğuma
// (domainSoguma), hesap başına kuyrukta en çok musteriKuyrukSiniri hedef ve son 24
// saatte en çok musteriGunlukSinir kuyruğa alış. Aşımda 429 + kod.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
)

const (
	// musteriGunlukSinir — hesap başına 24 saatte kuyruğa alınabilecek tarama sayısı.
	musteriGunlukSinir   = 30
	musteriGunlukPencere = 24 * time.Hour
	// musteriKuyrukSiniri — hesabın kuyrukta aynı anda bekleyebilecek hedef sayısı.
	musteriKuyrukSiniri = 20
	// musteriSahipTaban — kuyruktaki sahip anahtarı: bayi kimlikleriyle ÇAKIŞMASIN
	// (bayi users.id küçük sayıdır); hesap = taban + domain_id.
	musteriSahipTaban = int64(1) << 40
	musteriBulguMax   = 100
)

// Testlerin değiştirdiği dikişler (üretimde middleware).
var (
	musteriAl = middleware.MusteriClaimsFrom
	aktorAl   = middleware.Aktor
)

func musteriSahip(domainID int64) int64 { return musteriSahipTaban + domainID }

// MusteriKapi — URL'deki {id} müşteri jetonundaki domainle eşleşmezse 404 (yabancı
// domain = olmayan domain; MusteriScope'un 403'ünden ÖNCE çalışır). Müşteri jetonu
// yoksa dokunmaz (sonraki katman karar verir).
func MusteriKapi(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mc := musteriAl(r); mc != nil && !urlDomainEsles(r, mc.DomainID) {
			hataMesaji(w, http.StatusNotFound, "bulunamadı")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func urlDomainEsles(r *http.Request, domainID int64) bool {
	s := chi.URLParam(r, "id")
	return s == "" || s == strconv.FormatInt(domainID, 10)
}

// musteriDomain — istek sahibi müşterinin domain kimliği; yoksa yanıtı yazar.
func musteriDomain(w http.ResponseWriter, r *http.Request) (int64, bool) {
	mc := musteriAl(r)
	if mc == nil || mc.DomainID <= 0 {
		hataMesaji(w, http.StatusForbidden, "yetkiniz yok")
		return 0, false
	}
	if !urlDomainEsles(r, mc.DomainID) {
		hataMesaji(w, http.StatusNotFound, "bulunamadı")
		return 0, false
	}
	return mc.DomainID, true
}

func musteriHata(w http.ResponseWriter, err error) {
	log.Printf("websec: müşteri isteği DB hatası: %v", err)
	hataMesaji(w, http.StatusInternalServerError, "işlem tamamlanamadı")
}

/* ---------------- kuyruk: müşteri hakkı ---------------- */

// musteriEkleSay — hesabın (sahipliği doğrulanmış) hedeflerini müşteri sınırlarıyla
// kuyruğa yazar. Dönüş bayiEkleSay ile aynı + gunluk: ret nedeni günlük sınırsa true.
func (k *kuyruk) musteriEkleSay(domainID int64, ids []int64) (sirada, yeni int, bekle time.Duration, gunluk bool) {
	if domainID <= 0 {
		return 0, 0, 0, false
	}
	sahip := musteriSahip(domainID)
	simdi := saat()
	k.mu.Lock()
	defer k.mu.Unlock()
	k.budaKilitli(simdi)
	bekleyen := 0
	for _, s := range k.bekleyen {
		if s == sahip {
			bekleyen++
		}
	}
	var soguma time.Duration
	doldu := false
	for _, id := range ids {
		if _, zaten := k.bekleyen[id]; zaten {
			sirada++
			continue
		}
		if onceki, ok := k.sonIstek[id]; ok && onceki.rid == sahip {
			if kalan := domainSoguma - simdi.Sub(onceki.t); kalan > 0 {
				if soguma == 0 || kalan < soguma {
					soguma = kalan
				}
				continue
			}
		}
		if g := k.musteriGun[sahip]; len(g) >= musteriGunlukSinir {
			gunluk = true
			if b := musteriGunlukPencere - simdi.Sub(g[0]); b > 0 && (soguma == 0 || b < soguma) {
				soguma = b
			}
			continue
		}
		if bekleyen >= musteriKuyrukSiniri {
			doldu = true
			continue
		}
		k.ekleKilitli(id, sahip)
		k.sonIstek[id] = bayiIstek{rid: sahip, t: simdi}
		k.musteriGun[sahip] = append(k.musteriGun[sahip], simdi)
		bekleyen++
		sirada++
		yeni++
	}
	if yeni > 0 {
		k.tetikle()
	}
	switch {
	case sirada > 0:
		return sirada, yeni, 0, false
	case gunluk:
		if soguma == 0 {
			soguma = musteriGunlukPencere
		}
		return 0, 0, soguma, true
	case doldu:
		return 0, 0, kuyrukDoluBekleme, false
	default:
		return 0, 0, soguma, false
	}
}

// musteriGunlukKalan — hesabın son 24 saatteki kalan tarama hakkı.
func (k *kuyruk) musteriGunlukKalan(domainID int64) int {
	simdi := saat()
	k.mu.Lock()
	defer k.mu.Unlock()
	n := 0
	for _, z := range k.musteriGun[musteriSahip(domainID)] {
		if simdi.Sub(z) < musteriGunlukPencere {
			n++
		}
	}
	return max(0, musteriGunlukSinir-n)
}

/* ---------------- liste + özet ---------------- */

type musteriOnem struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
}

func (o *musteriOnem) ekle(onem string, n int) {
	switch onem {
	case "critical":
		o.Critical += n
	case "high":
		o.High += n
	case "medium":
		o.Medium += n
	case "low":
		o.Low += n
	}
}

func (o musteriOnem) toplam() int { return o.Critical + o.High + o.Medium + o.Low }

// musteriSatir — müşterinin tablo satırı. Alan adları WebsiteSecurity (bayi) yanıtıyla
// aynıdır (domain_id, ek_alan_id, durum, domain_son_tarama, son_sonuc): arayüzün
// geri bildirim/yoklama yardımcıları (lib/websecTarama.ts) aynen kullanılır.
type musteriSatir struct {
	DomainID        int64       `json:"domain_id"`
	EkAlanID        int64       `json:"ek_alan_id,omitempty"`
	AlanAdi         string      `json:"alan_adi"`
	Ek              bool        `json:"ek"`
	AppType         string      `json:"app_type"`
	AppVersion      string      `json:"app_version"`
	UygulamaSayisi  int         `json:"uygulama_sayisi"`
	PaketSayisi     int         `json:"paket_sayisi"`
	BulguSayisi     int         `json:"bulgu_sayisi"`
	Onem            musteriOnem `json:"onem"`
	DomainSonTarama string      `json:"domain_son_tarama"`
	SonSonuc        *sonSonuc   `json:"son_sonuc,omitempty"`
	Durum           string      `json:"durum"`
}

func (s musteriSatir) anahtar() int64 {
	if s.EkAlanID > 0 {
		return -s.EkAlanID
	}
	return s.DomainID
}

type musteriOzet struct {
	musteriOnem
	Toplam    int    `json:"toplam"`
	SonTarama string `json:"son_tarama"`
	Taraniyor bool   `json:"taraniyor"`
	Hedef     int    `json:"hedef_sayisi"`
	// Durum: acik | temiz | desteklenmiyor | beklemede (hiç taranmamış).
	Durum     string `json:"durum"`
	FeedUyari bool   `json:"feed_uyari"`
}

type musteriTaraHakki struct {
	SogumaDk    int `json:"soguma_dk"`
	GunlukSinir int `json:"gunluk_sinir"`
	GunlukKalan int `json:"gunluk_kalan"`
}

type musteriListeYanit struct {
	Ozet  musteriOzet      `json:"ozet"`
	Tara  musteriTaraHakki `json:"tara"`
	Items []musteriSatir   `json:"items"`
	// FeedUyari — özet içinde de var; arayüz şeridi için üst düzeyde tekrar.
	FeedUyari bool `json:"feed_uyari"`
}

const zamanSQL = `COALESCE(DATE_FORMAT(%s,'%%Y-%%m-%%d %%H:%%i:%%s'),'')`

// musteriHesapla — hesabın satırları + özet. Tüm sorgular domain_id=? ile sınırlı;
// domain yoksa sql.ErrNoRows.
func (h *Handler) musteriHesapla(ctx context.Context, domainID int64) ([]musteriSatir, musteriOzet, error) {
	var anaAd, anaSon string
	var anaTaranmis bool
	err := h.DB.QueryRowContext(ctx, `
		SELECT COALESCE(d.alan_adi,''), `+fmt.Sprintf(zamanSQL, "t.son_tarama")+`, (t.domain_id IS NOT NULL)
		  FROM domains d
		  LEFT JOIN cp_websec_domain_tarama t ON t.domain_id = d.id
		 WHERE d.id = ?`, domainID).Scan(&anaAd, &anaSon, &anaTaranmis)
	if err != nil {
		return nil, musteriOzet{}, err
	}
	satirlar := []musteriSatir{{DomainID: domainID, AlanAdi: anaAd, DomainSonTarama: anaSon}}
	taranmis := map[int64]bool{domainID: anaTaranmis}
	idx := map[int64]int{0: 0}

	ekRows, err := h.DB.QueryContext(ctx, `
		SELECT e.id, COALESCE(e.alan_adi,''), `+fmt.Sprintf(zamanSQL, "t.son_tarama")+`, (t.ek_alan_id IS NOT NULL)
		  FROM ek_alan_adlari e
		  LEFT JOIN cp_websec_ek_tarama t ON t.ek_alan_id = e.id
		 WHERE e.domain_id = ?
		 ORDER BY e.alan_adi`, domainID)
	if err != nil {
		return nil, musteriOzet{}, err
	}
	for ekRows.Next() {
		var s musteriSatir
		var tr bool
		if err := ekRows.Scan(&s.EkAlanID, &s.AlanAdi, &s.DomainSonTarama, &tr); err != nil {
			_ = ekRows.Close()
			return nil, musteriOzet{}, err
		}
		s.DomainID, s.Ek = domainID, true
		idx[s.EkAlanID] = len(satirlar)
		taranmis[-s.EkAlanID] = tr
		satirlar = append(satirlar, s)
	}
	err = ekRows.Err()
	_ = ekRows.Close()
	if err != nil {
		return nil, musteriOzet{}, err
	}

	// Uygulamalar (hedef başına birleştirilir): en çok bulgulu uygulama satırın türü/sürümü.
	appRows, err := h.DB.QueryContext(ctx, `
		SELECT COALESCE(ek_alan_id,0), app_type, COALESCE(app_version,''), paket_sayisi
		  FROM cp_websec_apps
		 WHERE domain_id = ?
		 ORDER BY bulgu_sayisi DESC, app_type, id`, domainID)
	if err != nil {
		return nil, musteriOzet{}, err
	}
	for appRows.Next() {
		var ek int64
		var tur, surum string
		var paket int
		if err := appRows.Scan(&ek, &tur, &surum, &paket); err != nil {
			_ = appRows.Close()
			return nil, musteriOzet{}, err
		}
		i, ok := idx[ek]
		if !ok {
			continue // hesabın ek alanı olmayan (artık silinmiş) kayıt
		}
		if satirlar[i].UygulamaSayisi == 0 {
			satirlar[i].AppType, satirlar[i].AppVersion = tur, surum
		}
		satirlar[i].UygulamaSayisi++
		satirlar[i].PaketSayisi += paket
	}
	err = appRows.Err()
	_ = appRows.Close()
	if err != nil {
		return nil, musteriOzet{}, err
	}

	// Önem sayaçları (bulgu satırlarından; liste ucuyla aynı kümeden).
	bRows, err := h.DB.QueryContext(ctx, `
		SELECT COALESCE(ek_alan_id,0), severity, COUNT(*)
		  FROM cp_websec_findings
		 WHERE domain_id = ?
		 GROUP BY COALESCE(ek_alan_id,0), severity`, domainID)
	if err != nil {
		return nil, musteriOzet{}, err
	}
	for bRows.Next() {
		var ek int64
		var onem string
		var n int
		if err := bRows.Scan(&ek, &onem, &n); err != nil {
			_ = bRows.Close()
			return nil, musteriOzet{}, err
		}
		if i, ok := idx[ek]; ok {
			satirlar[i].Onem.ekle(onem, n)
		}
	}
	err = bRows.Err()
	_ = bRows.Close()
	if err != nil {
		return nil, musteriOzet{}, err
	}

	kosuyor, taranSet := TaramaDurumu()
	bekleyen := kuy.bekleyenKume()
	oz := musteriOzet{Hedef: len(satirlar), FeedUyari: feedUyariBayrak.Load()}
	appVar, hicTaranmis := false, false
	for i := range satirlar {
		s := &satirlar[i]
		s.BulguSayisi = s.Onem.toplam()
		a := s.anahtar()
		switch {
		case kosuyor && (len(taranSet) == 0 || taranSet[a]):
			s.Durum = "taraniyor"
		case bekleyen[a]:
			s.Durum = "sirada"
		case s.BulguSayisi > 0:
			s.Durum = "acik"
		case s.AppType != "":
			s.Durum = "temiz"
		case taranmis[a]:
			s.Durum = "desteklenmiyor"
		default:
			s.Durum = "beklemede"
		}
		if s.DomainSonTarama != "" {
			s.SonSonuc = &sonSonuc{UygulamaSayisi: s.UygulamaSayisi, BulguSayisi: s.BulguSayisi}
			hicTaranmis = true
			if s.DomainSonTarama > oz.SonTarama { // sabit biçim: metin sırası = zaman sırası
				oz.SonTarama = s.DomainSonTarama
			}
		}
		if s.AppType != "" {
			appVar = true
		}
		if s.Durum == "taraniyor" || s.Durum == "sirada" {
			oz.Taraniyor = true
		}
		oz.Critical += s.Onem.Critical
		oz.High += s.Onem.High
		oz.Medium += s.Onem.Medium
		oz.Low += s.Onem.Low
	}
	oz.Toplam = oz.musteriOnem.toplam()
	switch {
	case oz.Toplam > 0:
		oz.Durum = "acik"
	case appVar:
		oz.Durum = "temiz"
	case hicTaranmis:
		oz.Durum = "desteklenmiyor"
	default:
		oz.Durum = "beklemede"
	}
	return satirlar, oz, nil
}

// MusteriListe — GET /domains/{id}/guvenlik
func (h *Handler) MusteriListe(w http.ResponseWriter, r *http.Request) {
	domainID, ok := musteriDomain(w, r)
	if !ok {
		return
	}
	satirlar, oz, err := h.musteriHesapla(r.Context(), domainID)
	if errors.Is(err, sql.ErrNoRows) {
		hataMesaji(w, http.StatusNotFound, "bulunamadı")
		return
	}
	if err != nil {
		musteriHata(w, err)
		return
	}
	jsonYaz(w, http.StatusOK, musteriListeYanit{
		Ozet: oz, Items: satirlar, FeedUyari: oz.FeedUyari,
		Tara: musteriTaraHakki{
			SogumaDk: int(domainSoguma / time.Minute), GunlukSinir: musteriGunlukSinir,
			GunlukKalan: kuy.musteriGunlukKalan(domainID),
		},
	})
}

// MusteriOzet — GET /domains/{id}/guvenlik/ozet (ana sayfa kartı için küçük yanıt).
func (h *Handler) MusteriOzet(w http.ResponseWriter, r *http.Request) {
	domainID, ok := musteriDomain(w, r)
	if !ok {
		return
	}
	_, oz, err := h.musteriHesapla(r.Context(), domainID)
	if errors.Is(err, sql.ErrNoRows) {
		hataMesaji(w, http.StatusNotFound, "bulunamadı")
		return
	}
	if err != nil {
		musteriHata(w, err)
		return
	}
	jsonYaz(w, http.StatusOK, oz)
}

/* ---------------- bulgular + öneri ---------------- */

type musteriBulgu struct {
	ID       int64   `json:"id"`
	EkAlanID int64   `json:"ek_alan_id,omitempty"`
	AlanAdi  string  `json:"alan_adi"`
	Ek       bool    `json:"ek"`
	AppType  string  `json:"app_type"`
	Paket    string  `json:"paket"`
	Surum    string  `json:"surum"`
	CVE      string  `json:"cve"`
	Onem     string  `json:"onem"`
	CVSS     float64 `json:"cvss,omitempty"`
	Baslik   string  `json:"baslik"`
	Duzeltme string  `json:"duzeltme_surumu"`
	// OneriKod arayüzün dilinde metin üretmesi için; Oneri sade Türkçe yedek metin.
	OneriKod   string `json:"oneri_kod"`
	Oneri      string `json:"oneri"`
	Baglanti   string `json:"baglanti,omitempty"`
	SonGorulme string `json:"son_gorulme"`
}

type musteriBulguYanit struct {
	Toplam     int            `json:"toplam"`
	Sayfa      int            `json:"sayfa"`
	SayfaBoyut int            `json:"sayfa_boyut"`
	Items      []musteriBulgu `json:"items"`
}

var (
	reCVE  = regexp.MustCompile(`^CVE-\d{4}-\d{4,9}$`)
	reGHSA = regexp.MustCompile(`^ghsa(-[0-9a-z]{4}){3}$`)
)

// bulguBaglanti — yalnız tanınan kimlik biçimlerine kamuya açık kaynak bağlantısı
// (serbest metinden URL üretilmez).
func bulguBaglanti(id string) string {
	switch {
	case reCVE.MatchString(id):
		return "https://nvd.nist.gov/vuln/detail/" + id
	case reGHSA.MatchString(strings.ToLower(id)):
		return "https://github.com/advisories/GHSA" + strings.ToLower(id[4:])
	}
	return ""
}

// paketTuru — install_path'in '#' sonrası ("plugin:slug", "npm:ad") türü; yol HİÇ
// dışarı çıkmaz.
func paketTuru(installPath string) string {
	i := strings.LastIndex(installPath, "#")
	if i < 0 {
		return ""
	}
	tur, _, ok := strings.Cut(installPath[i+1:], ":")
	if !ok {
		return ""
	}
	switch tur {
	case "plugin", "theme", "npm", "composer":
		return tur
	}
	return ""
}

// oneriUret — bulgu için sade öneri (kod + Türkçe metin). Düzeltme sürümü varsa
// "güncelleyin", yoksa "yama yok" genel önerisi.
func oneriUret(tur, paket, duzeltme string) (kod, metin string) {
	if duzeltme != "" {
		switch tur {
		case "plugin":
			return "guncelle_eklenti", fmt.Sprintf("%s eklentisini %s sürümüne ya da daha yenisine güncelleyin.", paket, duzeltme)
		case "theme":
			return "guncelle_tema", fmt.Sprintf("%s temasını %s sürümüne ya da daha yenisine güncelleyin.", paket, duzeltme)
		default:
			return "guncelle_paket", fmt.Sprintf("%s paketini %s sürümüne ya da daha yenisine yükseltin.", paket, duzeltme)
		}
	}
	switch tur {
	case "plugin":
		return "yama_yok_eklenti", "Henüz yama yok; eklentiyi devre dışı bırakmayı ya da başka bir eklentiyle değiştirmeyi düşünün."
	case "theme":
		return "yama_yok_tema", "Henüz yama yok; temayı değiştirmeyi düşünün."
	default:
		return "yama_yok_paket", "Henüz yama yok; paketi kaldırmayı ya da güvenli bir alternatifle değiştirmeyi düşünün."
	}
}

func kisalt(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// MusteriBulgular — GET /domains/{id}/guvenlik/bulgular[?ek_alan_id=&severity=&page=&page_size=]
// ek_alan_id yok → hesabın tüm bulguları; 0 → yalnız ana alan adı; N → o ek alan
// (hesabın değilse 404).
func (h *Handler) MusteriBulgular(w http.ResponseWriter, r *http.Request) {
	domainID, ok := musteriDomain(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	where := ` WHERE f.domain_id = ? AND (f.ek_alan_id IS NULL OR e.id IS NOT NULL)`
	args := []any{domainID}
	if ek := q.Get("ek_alan_id"); ek != "" {
		n, err := strconv.ParseInt(ek, 10, 64)
		switch {
		case err != nil || n < 0:
			hataMesaji(w, http.StatusNotFound, "bulunamadı")
			return
		case n == 0:
			where += ` AND f.ek_alan_id IS NULL`
		default:
			var one int
			err := h.DB.QueryRowContext(r.Context(),
				`SELECT 1 FROM ek_alan_adlari WHERE id = ? AND domain_id = ?`, n, domainID).Scan(&one)
			if errors.Is(err, sql.ErrNoRows) {
				hataMesaji(w, http.StatusNotFound, "bulunamadı")
				return
			}
			if err != nil {
				musteriHata(w, err)
				return
			}
			where += ` AND f.ek_alan_id = ?`
			args = append(args, n)
		}
	}
	switch sev := q.Get("severity"); sev {
	case "":
	case "critical", "high", "medium", "low":
		where += ` AND f.severity = ?`
		args = append(args, sev)
	default:
		where += ` AND 1=0`
	}
	sayfa, boyut := 1, 50
	if n, _ := strconv.Atoi(q.Get("page")); n > 0 {
		sayfa = n
	}
	if n, err := strconv.Atoi(q.Get("page_size")); err == nil && n > 0 && n <= musteriBulguMax {
		boyut = n
	}
	const dis = ` FROM cp_websec_findings f
		  JOIN domains d ON d.id = f.domain_id
		  LEFT JOIN ek_alan_adlari e ON e.id = f.ek_alan_id AND e.domain_id = f.domain_id`
	var toplam int
	if err := h.DB.QueryRowContext(r.Context(), `SELECT COUNT(*)`+dis+where, args...).Scan(&toplam); err != nil { //nolint:gosec // G202/G701: where SABİT parçalar; değerler ? ile bağlanır.
		musteriHata(w, err)
		return
	}
	liste := append(append([]any{}, args...), boyut, (sayfa-1)*boyut)
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT f.id, COALESCE(f.ek_alan_id,0), COALESCE(e.alan_adi, d.alan_adi, ''), f.app_type,
		       f.package_name, f.installed_version, f.cve_id, f.severity, IFNULL(f.cvss,0),
		       COALESCE(f.title,''), COALESCE(f.fixed_in,''), COALESCE(f.install_path,''),
		       COALESCE(DATE_FORMAT(f.last_seen,'%Y-%m-%d %H:%i:%s'),'')`+dis+where+`
		 ORDER BY FIELD(f.severity,'critical','high','medium','low',''), f.last_seen DESC, f.id
		 LIMIT ? OFFSET ?`, liste...) //nolint:gosec // G202/G701: where SABİT parçalar; değerler ? ile bağlanır.
	if err != nil {
		musteriHata(w, err)
		return
	}
	defer rows.Close()
	items := []musteriBulgu{}
	for rows.Next() {
		var b musteriBulgu
		var yol string
		if err := rows.Scan(&b.ID, &b.EkAlanID, &b.AlanAdi, &b.AppType, &b.Paket, &b.Surum,
			&b.CVE, &b.Onem, &b.CVSS, &b.Baslik, &b.Duzeltme, &yol, &b.SonGorulme); err != nil {
			musteriHata(w, err)
			return
		}
		b.Ek = b.EkAlanID > 0
		b.Baslik = kisalt(b.Baslik, 300)
		b.OneriKod, b.Oneri = oneriUret(paketTuru(yol), b.Paket, b.Duzeltme)
		b.Baglanti = bulguBaglanti(b.CVE)
		items = append(items, b)
	}
	if err := rows.Err(); err != nil {
		musteriHata(w, err)
		return
	}
	jsonYaz(w, http.StatusOK, musteriBulguYanit{Toplam: toplam, Sayfa: sayfa, SayfaBoyut: boyut, Items: items})
}

/* ---------------- tara ---------------- */

type musteriTaraGovde struct {
	EkAlanID int64 `json:"ek_alan_id"`
}

// MusteriTara — POST /domains/{id}/guvenlik/tara  gövde {"ek_alan_id": N} (0/yok = ana
// alan adı). Tek hedef; kuyruk anahtarı semantiği (>0 ana, <0 ek alan), tekrar tıklama
// "zaten sırada" kapısı, hedef başına 15 dk soğuma, günlük sınır. Yanıt: 202
// {"sirada":n[,"zaten":true]} ya da 429 {"error":..., "kod":"soguma|gunluk_sinir"}.
func (h *Handler) MusteriTara(w http.ResponseWriter, r *http.Request) {
	domainID, ok := musteriDomain(w, r)
	if !ok {
		return
	}
	var g musteriTaraGovde
	if r.ContentLength != 0 && r.Body != nil {
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&g); err != nil {
			hataMesaji(w, http.StatusBadRequest, "geçersiz istek")
			return
		}
	}
	anahtar, hedefAd := domainID, ""
	if g.EkAlanID < 0 {
		hataMesaji(w, http.StatusNotFound, "bulunamadı")
		return
	}
	if g.EkAlanID > 0 {
		err := h.DB.QueryRowContext(r.Context(),
			`SELECT alan_adi FROM ek_alan_adlari WHERE id = ? AND domain_id = ?`, g.EkAlanID, domainID).Scan(&hedefAd)
		if errors.Is(err, sql.ErrNoRows) {
			hataMesaji(w, http.StatusNotFound, "bulunamadı")
			return
		}
		if err != nil {
			musteriHata(w, err)
			return
		}
		anahtar = -g.EkAlanID
	} else if err := h.DB.QueryRowContext(r.Context(),
		`SELECT alan_adi FROM domains WHERE id = ?`, domainID).Scan(&hedefAd); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			hataMesaji(w, http.StatusNotFound, "bulunamadı")
			return
		}
		musteriHata(w, err)
		return
	}
	// Şu an taranan hedef yeniden kuyruğa girmez.
	if calisiyorMu(anahtar) {
		jsonYaz(w, http.StatusAccepted, map[string]any{"sirada": 1, "zaten": true})
		return
	}
	sirada, yeni, bekle, gunluk := kuy.musteriEkleSay(domainID, []int64{anahtar})
	if bekle > 0 {
		if gunluk {
			saatSayi := max(1, int((bekle+time.Hour-1)/time.Hour))
			jsonYaz(w, http.StatusTooManyRequests, map[string]string{
				"error": fmt.Sprintf("Günlük tarama sınırına ulaşıldı; %d saat sonra tekrar deneyin", saatSayi),
				"kod":   "gunluk_sinir",
			})
			return
		}
		jsonYaz(w, http.StatusTooManyRequests, map[string]string{
			"error": fmt.Sprintf("%d dk sonra tekrar deneyin", dakikaYukari(bekle)),
			"kod":   "soguma",
		})
		return
	}
	if yeni == 0 {
		// Zaten sırada: denetime ikinci satır YAZILMAZ.
		jsonYaz(w, http.StatusAccepted, map[string]any{"sirada": sirada, "zaten": true})
		return
	}
	uid, kul := aktorAl(r)
	httpx.DenetimDomain(h.DB, r, uid, kul, "websec.tara", hedefAd, fmt.Sprintf("sirada=%d", sirada), domainID, true)
	jsonYaz(w, http.StatusAccepted, map[string]int{"sirada": sirada})
}
