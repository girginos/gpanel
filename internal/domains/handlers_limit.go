// Hostinge özel limitler (üst yazım) — D1 "özel plan = ÜST YAZIM".
//
//	GET    /domains/{id}/limitler   plan değeri + üst yazım + efektif, alan başına
//	PUT    /domains/{id}/limitler   {"ust":{"disk_kota_mb":20480,"ram_mb":null,...}} — TAM değiştirme
//	DELETE /domains/{id}/limitler   plan değerlerine dön
//
// Hepsi SahipYonetim altında: yönetici her hostingte, bayi YALNIZ kendi hostingte
// (başka hosting ile olmayan id aynı 403), müşteri ASLA. Hosting katalog planında
// kalır; katalogda kopya plan ÜRETİLMEZ.
//
// Bayi kuralları:
//   - Kökün İÇ AYARLARI (efektiflimit.IcAlan: inode, process, IO, FPM, MySQL
//     bağlantı + governor) bayi tarafından YAZILAMAZ: istekte bu alanlardan biri
//     (değeri ne olursa olsun, null dahil) varsa 403; mevcut değerleri (kökün ya da
//     eski bir üst yazımın) bayinin PUT/DELETE'inde aynen korunur.
//   - Kökün kilitlediği alanlar (hosting_limit_ust.kok_alanlar) değiştirilemez;
//     GL-06 sınıfı (plans.PlanOzet'te olmayan) kilitli alanın değeri bayiye dönmez.
//   - Plan bayinin değilse (kökün global planı / başka sahibin planı) GL-06 sınıfı
//     alanların plan değeri bayiye dönmez (gizli:true) — bugünkü PlanOzet kuralı.
//   - Disk/trafik 0 (sınırsız) yazılamaz; efektif disk/trafik bayi havuzunu aşamaz
//     (artış reddedilir, azaltma her zaman geçer); efektif posta limitleri bayi
//     paketinin tavanını aşamaz; efektif disk fiili kullanımın altına inemez.
package domains

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
	"time"

	"girginospanel/internal/efektiflimit"
	"girginospanel/internal/httpx"
	"girginospanel/internal/kaynaklimit"
	"girginospanel/internal/kilit"
	"girginospanel/internal/middleware"
	"girginospanel/internal/plans"

	"github.com/go-chi/chi/v5"
)

// limitAlanYanit — tek alanın istemci görünümü.
type limitAlanYanit struct {
	Ad      string `json:"ad"`
	Grup    string `json:"grup"`
	Birim   string `json:"birim"`
	Sifir   string `json:"sifir"`
	Min     int    `json:"min"`
	Max     int    `json:"max"`
	Plan    *int   `json:"plan"`
	Ust     *int   `json:"ust"`
	Efektif *int   `json:"efektif"`
	Kaynak  string `json:"kaynak"`
	Gizli   bool   `json:"gizli,omitempty"`   // değer bu rol için gösterilmez (GL-06)
	Kilitli bool   `json:"kilitli,omitempty"` // yönetici kilitledi — bayi değiştiremez
	Ic      bool   `json:"ic,omitempty"`      // kökün iç ayarı — bayi hiç yazamaz (efektiflimit.IcAlan)
}

type limitYanit struct {
	Plan *struct {
		ID int64  `json:"id"`
		Ad string `json:"ad"`
	} `json:"plan"`
	// EskiOzelKopya: hosting dönüştürülemeyen eski "<alan> — Özel" kopyasında
	// (açılış dönüşümü tabanı belirleyemedi; yönetici bir katalog planı seçmeli).
	EskiOzelKopya bool             `json:"eski_ozel_kopya,omitempty"`
	OzelLimit     bool             `json:"ozel_limit"`
	Alanlar       []limitAlanYanit `json:"alanlar"`
}

// limitGorunum — Sonuc'u çağıranın rolüne göre maskeler. rid>0 = bayi.
func limitGorunum(s efektiflimit.Sonuc, rid int64) limitYanit {
	var y limitYanit
	if s.PlanVar && s.PlanID != nil {
		y.Plan = &struct {
			ID int64  `json:"id"`
			Ad string `json:"ad"`
		}{*s.PlanID, s.PlanAd}
	}
	y.EskiOzelKopya = s.PlanDomain != nil
	// Bayiye plan değerleri yalnız planın sahibiyse tam görünür (plans.gorunum ile aynı).
	planGizli := rid > 0 && s.PlanSahip != rid
	for _, a := range efektiflimit.Alanlar {
		d := s.Degerler[a.Sutun]
		ay := limitAlanYanit{Ad: a.Sutun, Grup: a.Grup, Birim: a.Birim, Sifir: a.Sifir, Min: a.Min, Max: a.Max,
			Plan: d.Plan, Ust: d.Ust, Efektif: d.Efektif, Kaynak: d.Kaynak}
		if rid > 0 {
			ay.Kilitli = d.Kok
			if efektiflimit.IcAlan(a.Sutun) {
				ay.Ic = true
				if planGizli {
					ay.Plan = nil
					ay.Gizli = true
				}
				// Kökün kilitli değeri ya da 0037 domain değeri (yöneticinin) gizli.
				if d.Kok || d.Kaynak == efektiflimit.KaynakAlan {
					ay.Ust = nil
					ay.Gizli = true
				}
				// Efektif değer gizli kaynaktan geliyorsa (kökün planı / kilidi / 0037) dönmez;
				// bayinin kendi üst yazımı görünür.
				if d.Kok || (ay.Gizli && d.Kaynak != efektiflimit.KaynakUst) {
					ay.Efektif = nil
				}
			}
		}
		y.Alanlar = append(y.Alanlar, ay)
	}
	if rid > 0 {
		// "Özel limit" rozeti bayiye yalnız görebildiği bir üst yazım varsa.
		for _, a := range y.Alanlar {
			if a.Ust != nil || a.Kilitli {
				y.OzelLimit = true
				break
			}
		}
	} else {
		y.OzelLimit = s.UstVar
	}
	return y
}

// LimitGetir — GET /domains/{id}/limitler
func (h *Handlers) LimitGetir(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	s, err := efektiflimit.Oku(r.Context(), h.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if err != nil {
		log.Printf("limitler: okunamadı (domain=%d): %v", id, err)
		httpx.WriteError(w, http.StatusInternalServerError, "limitler okunamadı")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, limitGorunum(s, middleware.ResellerIDFrom(r)))
}

type limitReq struct {
	Ust map[string]*int `json:"ust"`
}

// LimitKaydet — PUT /domains/{id}/limitler
func (h *Handlers) LimitKaydet(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var req limitReq
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	if req.Ust == nil {
		req.Ust = map[string]*int{}
	}
	h.limitYaz(w, r, req.Ust)
}

// LimitSifirla — DELETE /domains/{id}/limitler (plan değerlerine dön). Bayide
// yöneticinin kilitlediği alanlar kalır.
func (h *Handlers) LimitSifirla(w http.ResponseWriter, r *http.Request) {
	h.limitYaz(w, r, map[string]*int{})
}

// limitHata — kullanıcıya dönen kapı hatası (kod + mesaj; yabancı veri içermez).
type limitHata struct {
	kod   int
	mesaj string
}

func (e *limitHata) Error() string { return e.mesaj }

func esitInt(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func intYaz(p *int) string {
	if p == nil {
		return "plan"
	}
	return strconv.Itoa(*p)
}

func (h *Handlers) limitYaz(w http.ResponseWriter, r *http.Request, istek map[string]*int) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	rid := middleware.ResellerIDFrom(r)
	if err := efektiflimit.Dogrula(istek, rid > 0); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if rid > 0 {
		// Kökün iç ayarları bayi isteğinde YOK SAYILMAZ, reddedilir (kullanıcı kararı
		// 2026-10-03). Varlığı yeter: aynı değer/null da reddedilir — gizli kök
		// değerini deneme-yanılmayla ölçen bir kâhin (oracle) oluşmasın.
		if ic := efektiflimit.IcAlanlarIcinde(istek); len(ic) > 0 {
			httpx.WriteError(w, http.StatusForbidden, efektiflimit.IcAlanRedMesaji(ic))
			return
		}
	}
	if rid > 0 {
		// Havuz kapısı: iki paralel istek aynı eski toplamı okuyup havuzu aşmasın
		// (SetPlan/Create ile aynı kilit).
		k := kilit.Bayi(rid)
		k.Lock()
		defer k.Unlock()
	}
	ctx := r.Context()
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "işlem başlatılamadı")
		return
	}
	defer func() { _ = tx.Rollback() }()
	// Satır kilidi: aynı hostinge eşzamanlı iki yazım birbirini ezmesin.
	var kilitID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM domains WHERE id=? FOR UPDATE`, id).Scan(&kilitID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "limitler okunamadı")
		return
	}
	once, err := efektiflimit.Oku(ctx, tx, id)
	if err != nil {
		log.Printf("limitler: okunamadı (domain=%d): %v", id, err)
		httpx.WriteError(w, http.StatusInternalServerError, "limitler okunamadı")
		return
	}
	yeni, kok, herr := limitBirlestir(once, istek, rid)
	if herr != nil {
		httpx.WriteError(w, herr.kod, herr.mesaj)
		return
	}
	if rid > 0 {
		if herr := h.bayiLimitKapilari(ctx, tx, rid, id, once, yeni); herr != nil {
			httpx.WriteError(w, herr.kod, herr.mesaj)
			return
		}
	}
	uid, kul := middleware.Aktor(r)
	if err := efektiflimit.Kaydet(ctx, tx, id, yeni, kok, uid, nil); err != nil {
		log.Printf("limitler: kaydedilemedi (domain=%d): %v", id, err)
		httpx.WriteError(w, http.StatusInternalServerError, "limitler kaydedilemedi")
		return
	}
	if err := tx.Commit(); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "limitler kaydedilemedi")
		return
	}
	// Denetim: yalnız değişen alanlar. Yöneticinin bir bayi hostingindeki GL-06
	// sınıfı alan değerleri kayıtta yazılmaz (kayıt bayinin kapsamına düşer).
	var degisen []string
	for _, a := range efektiflimit.Alanlar {
		e, n := once.Degerler[a.Sutun].Ust, yeni[a.Sutun]
		if esitInt(e, n) {
			continue
		}
		if rid <= 0 && once.ResellerID > 0 && efektiflimit.IcAlan(a.Sutun) {
			degisen = append(degisen, a.Sutun+": değişti")
			continue
		}
		degisen = append(degisen, fmt.Sprintf("%s: %s→%s", a.Sutun, intYaz(e), intYaz(n)))
	}
	var alanAdi string
	_ = h.DB.QueryRowContext(ctx, `SELECT alan_adi FROM domains WHERE id=?`, id).Scan(&alanAdi)
	detay := strings.Join(degisen, ", ")
	if detay == "" {
		detay = "değişiklik yok"
	}
	httpx.DenetimDomain(h.DB, r, uid, kul, "hosting.limit", alanAdi, detay, id, true)
	if len(degisen) > 0 {
		go ozelLimitUygula(h.DB, id, alanAdi)
	}
	sonra, err := efektiflimit.Oku(ctx, h.DB, id)
	if err != nil {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, limitGorunum(sonra, rid))
}

// limitBirlestir — istek + mevcut durum → yazılacak tam üst yazım ve kök kilitleri.
//
// Yönetici: istek TAM değiştirmedir. Bayi hostinginde yöneticinin yeni yazdığı ya da
// değiştirdiği alan kilitlenir; değeri aynı kalan bayi alanı bayinin kalır.
// Bayi: iç alanlar (efektiflimit.IcAlan) ve kilitli alanlar mevcut değerinde kalır
// (iç alan istekte hiç olmaz — limitYaz reddeder; kilitli alanda farklı değer → 403);
// diğer alanlar istekle TAM değiştirilir.
func limitBirlestir(once efektiflimit.Sonuc, istek map[string]*int, rid int64) (map[string]*int, map[string]bool, *limitHata) {
	yeni := map[string]*int{}
	kok := map[string]bool{}
	for _, a := range efektiflimit.Alanlar {
		mevcut := once.Degerler[a.Sutun]
		v, var_ := istek[a.Sutun]
		if rid > 0 {
			if efektiflimit.IcAlan(a.Sutun) {
				// Bayinin "plana dön"ü / tam değiştirmesi kökün iç ayarını silmez.
				yeni[a.Sutun] = mevcut.Ust
				kok[a.Sutun] = mevcut.Kok
				continue
			}
			if mevcut.Kok {
				if var_ && !esitInt(v, mevcut.Ust) {
					return nil, nil, &limitHata{http.StatusForbidden, a.Sutun + " yöneticiniz tarafından belirlendi — değiştirilemez"}
				}
				yeni[a.Sutun] = mevcut.Ust
				kok[a.Sutun] = true
				continue
			}
			yeni[a.Sutun] = v
			continue
		}
		yeni[a.Sutun] = v
		if v != nil && once.ResellerID > 0 && (mevcut.Kok || !esitInt(v, mevcut.Ust)) {
			kok[a.Sutun] = true
		}
	}
	return yeni, kok, nil
}

// yeniEfektif — yazılacak üst yazımla alanın efektif değeri (plan + 0037 mirası
// once'tan). nil = sınırsız/plan yok → 0.
func yeniEfektif(once efektiflimit.Sonuc, yeni map[string]*int, sutun string) int {
	if v := yeni[sutun]; v != nil {
		return *v
	}
	d := once.Degerler[sutun]
	if d.Alan != nil {
		return *d.Alan
	}
	if d.Plan != nil {
		return *d.Plan
	}
	return 0
}

// bayiLimitKapilari — bayinin yazımı havuzu/paket tavanını/fiili kullanımı aşamaz.
func (h *Handlers) bayiLimitKapilari(ctx context.Context, tx *sql.Tx, rid, did int64, once efektiflimit.Sonuc, yeni map[string]*int) *limitHata {
	// Posta tavanı: yalnız posta alanı değiştiyse (bayinin dokunmadığı plan değeri
	// başka bir alanın kaydını engellemesin).
	postaDegisti := false
	for _, k := range []string{"max_email", "saatlik_mail_limiti", "mail_kutu_kota_mb"} {
		if !esitInt(once.Degerler[k].Ust, yeni[k]) {
			postaDegisti = true
		}
	}
	if postaDegisti {
		if err := plans.BayiPostaTavanKontrol(ctx, h.DB, rid,
			yeniEfektif(once, yeni, "max_email"), yeniEfektif(once, yeni, "saatlik_mail_limiti"),
			yeniEfektif(once, yeni, "mail_kutu_kota_mb")); err != nil {
			return &limitHata{http.StatusBadRequest, err.Error()}
		}
	}
	eskiDisk, yeniDisk := int64(once.Efektif("disk_kota_mb", 0)), int64(yeniEfektif(once, yeni, "disk_kota_mb"))
	eskiTr, yeniTr := int64(once.Efektif("trafik_kota_mb", 0)), int64(yeniEfektif(once, yeni, "trafik_kota_mb"))
	// Fiili kullanım: efektif disk kullanımın altına inemez (hesap anında kota aşımına düşerdi).
	if yeniDisk > 0 && yeniDisk != eskiDisk {
		var boyutKB int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(boyut_kb,0) FROM domains WHERE id=?`, did).Scan(&boyutKB); err != nil {
			return &limitHata{http.StatusInternalServerError, "disk kullanımı okunamadı"}
		}
		if boyutKB > yeniDisk*1024 {
			return &limitHata{http.StatusConflict, fmt.Sprintf("mevcut disk kullanımı (%d MB) yeni disk sınırını (%d MB) aşıyor", boyutKB/1024, yeniDisk)}
		}
	}
	// Artış: limitliden sınırsıza (0) geçiş ya da daha büyük limit. Zaten sınırsızdan
	// limitliye geçiş azaltmadır.
	diskArtis := (yeniDisk == 0 && eskiDisk != 0) || (eskiDisk > 0 && yeniDisk > eskiDisk)
	trArtis := (yeniTr == 0 && eskiTr != 0) || (eskiTr > 0 && yeniTr > eskiTr)
	if !diskArtis && !trArtis {
		return nil // azaltma/aynı: havuz kapısı gerekmez
	}
	var maxDisk, maxTr int64
	var fazla int
	if err := tx.QueryRowContext(ctx,
		`SELECT max_disk_mb, max_trafik_mb, fazla_satis FROM users WHERE id=? AND role='reseller'`, rid).
		Scan(&maxDisk, &maxTr, &fazla); err != nil {
		return &limitHata{http.StatusForbidden, "bayi kaydı bulunamadı"}
	}
	if fazla == 1 {
		return nil // "fazla satışa izinli" bayide taahhüt kapısı atlanır (create/plan ile aynı)
	}
	var topDisk, topTr int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(`+efektiflimit.IfadeVarsayilan("disk_kota_mb", 0)+`),0),
		        COALESCE(SUM(`+efektiflimit.IfadeVarsayilan("trafik_kota_mb", 0)+`),0)
		   FROM domains d`+efektiflimit.Join+`WHERE d.reseller_id=?`, rid).Scan(&topDisk, &topTr); err != nil {
		return &limitHata{http.StatusInternalServerError, "kota hesaplanamadı"}
	}
	// Sınır kuralı create/plan değişimiyle AYNI: ">=" (sınıra ulaşmak doludur).
	if maxDisk > 0 && diskArtis {
		if yeniDisk == 0 {
			return &limitHata{http.StatusForbidden, "disk havuzunuz sınırlı — bu hosting sınırsız diske geçirilemez"}
		}
		if (topDisk-eskiDisk)+yeniDisk >= maxDisk {
			return &limitHata{http.StatusForbidden, fmt.Sprintf("disk kotanız yetersiz (taahhüt %d MB - mevcut %d MB + yeni %d MB, limit %d MB)", topDisk, eskiDisk, yeniDisk, maxDisk)}
		}
	}
	if maxTr > 0 && trArtis {
		if yeniTr == 0 {
			return &limitHata{http.StatusForbidden, "trafik havuzunuz sınırlı — bu hosting sınırsız trafiğe geçirilemez"}
		}
		if (topTr-eskiTr)+yeniTr >= maxTr {
			return &limitHata{http.StatusForbidden, fmt.Sprintf("trafik kotanız yetersiz (taahhüt %d MB - mevcut %d MB + yeni %d MB, limit %d MB)", topTr, eskiTr, yeniTr, maxTr)}
		}
	}
	return nil
}

// ozelLimitUygula — özel limit değişince (arka planda) cgroup/FPM/kota/governor ve
// posta limitlerini yeniden uygular. Değişken: testler sistem yan etkisi olmadan değiştirir.
var ozelLimitUygula = func(db *sql.DB, did int64, ad string) {
	c, iptal := context.WithTimeout(context.Background(), 5*time.Minute)
	defer iptal()
	if err := kaynaklimit.UygulaHepsi(c, db, did); err != nil {
		log.Printf("kaynaklimit apply (ozel limit) domain=%d: %v", did, err)
	}
	mailKutuLimitSenkron(c, db, did, ad)
}
