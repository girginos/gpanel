package tasima

// arsiv_baslat.go — POST /tasima/arsiv/baslat: arşiv kaynaklı işi KUYRUĞA yazar.
//
// Mevcut Baslat ile aynı tablolar/kuyruk/dağıtıcı/kalem yapısı (paralel yol yok); farklar:
//   - kimlik yok (kaynak_parola/anahtar NULL); kaynak dosyası SUNUCU tarafındaki keşif
//     oturumundan (arsivOturumMeta) çözülür — istemcinin yol/yukleme_id alanı yok sayılır;
//   - seçilen siteler oturumdaki sunucu kopyasıyla kesişir (kök dahil herkes için);
//   - hedef disk önkontrolü (boş alan ≥ arşiv × 2,5) ve bayi disk havuzu BAŞLAMADAN;
//   - bayide mevcut Baslat'ın zorlamaları aynen: kendi hesabı, sahip taşıma yok, üzerine
//     yazma yok, plan kapsamlı + zorunlu, kalan hosting hakkı; ssl istemci değeri yok sayılır.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"girginospanel/internal/httpx"
)

type arsivBaslatGirdi struct {
	Tip      string  `json:"tip"`
	Mod      string  `json:"mod"`
	OturumID int64   `json:"oturum_id"`
	Ayarlar  Ayarlar `json:"ayarlar"`
	Secilen  []Hesap `json:"secilen"`
}

// ArsivBaslat — POST /tasima/arsiv/baslat
func (h *Handlers) ArsivBaslat(w http.ResponseWriter, r *http.Request) {
	kp, ok := h.arsivKapi(w, r)
	if !ok {
		return
	}
	if !bayiHizIzni(kp.Kapsam) {
		httpx.WriteError(w, http.StatusTooManyRequests, "çok sık deneme — bir dakika sonra yeniden deneyin")
		return
	}
	var g arsivBaslatGirdi
	if err := decodeJSON(w, r, 4<<20, &g); err != nil || g.OturumID <= 0 {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz istek (oturum_id zorunlu)")
		return
	}
	ctx := r.Context()
	g.Tip = strings.ToLower(strings.TrimSpace(g.Tip))
	sur, ok := surucuBul(g.Tip)
	if !ok || !sur.Arsiv {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz kaynak tipi")
		return
	}
	if err := h.kaynakKullanilabilir(ctx, kp.Kapsam, g.Tip); err != nil {
		kod := http.StatusBadRequest
		if errors.Is(err, errTasimaKapali) {
			kod = http.StatusForbidden
		}
		httpx.WriteError(w, kod, err.Error())
		return
	}

	// 🔴 Oturum SUNUCUDAKİ keşif sonucudur; yalnız aynı kapsamın, hazır ve aynı tipte olanı.
	var ayarlar, kesifJSON string
	err := h.DB.QueryRowContext(ctx,
		`SELECT COALESCE(ayarlar,''), COALESCE(kesif_json,'') FROM tasima_isleri
		  WHERE id=? AND durum='oturum' AND kaynak_host=? AND sahip_rid=? AND (gecerlilik IS NULL OR gecerlilik > NOW())`,
		g.OturumID, arsivHostAdi, kp.RID).Scan(&ayarlar, &kesifJSON)
	var meta arsivOturumMeta
	if err == sql.ErrNoRows || (err == nil && json.Unmarshal([]byte(ayarlar), &meta) != nil) {
		httpx.WriteError(w, http.StatusBadRequest, "önce arşivi keşfedin (oturum bulunamadı ya da süresi doldu)")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "is kaydi olusturulamadi")
		return
	}
	if meta.Durum != "hazir" || meta.Tip != g.Tip {
		httpx.WriteError(w, http.StatusConflict, "arşiv keşfi tamamlanmadı ya da kaynak türü uyuşmuyor")
		return
	}

	// Dosya hâlâ aynı mı + hedefte yeterli boş alan (arşiv × 2,5) — başlamadan reddet.
	bilgi, err := h.arsivKaynakCoz(ctx, kp.Kapsam, meta.Yol, meta.YuklemeID)
	if err != nil {
		httpx.WriteError(w, arsivStatus(err), hataMetni(kp.Yonetici(), err).Error())
		return
	}
	if !bilgi.parmakIziEslesir(meta.ParmakIzi) {
		httpx.WriteError(w, http.StatusConflict, errArsivDegisti.Error())
		return
	}
	if err := arsivDiskOnkontrol(bilgi.Boyut); err != nil {
		if errors.Is(err, errArsivDisk) {
			httpx.WriteError(w, http.StatusInsufficientStorage, errArsivDisk.Error())
		} else {
			log.Printf("tasima: arsiv disk onkontrolu: %v", err)
			httpx.WriteError(w, http.StatusInternalServerError, "hedef disk ölçülemedi")
		}
		return
	}
	// Bayi disk havuzu: arşivin AÇILMIŞ boyutu havuzun kalanını aşıyorsa iş açılmaz.
	if kp.Bayi() {
		if kalan, sinirli, err := h.bayiDiskKalan(ctx, kp.RID); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "kota okunamadı")
			return
		} else if sinirli && meta.Ozet.ToplamBayt > kalan {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, errArsivKota.Error())
			return
		}
	}

	if len(g.Secilen) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "tasinacak hesap secilmedi")
		return
	}
	if len(g.Secilen) > 500 {
		httpx.WriteError(w, http.StatusBadRequest, "tek seferde en fazla 500 site tasinabilir")
		return
	}
	mod := "toplu"
	if g.Mod == "tekil" || len(g.Secilen) == 1 {
		mod = "tekil"
	}
	// Seçim = oturumdaki sunucu kopyası ∩ istek (istemcinin WebRoot/DBler/Sahip alanları yok sayılır).
	var oturumHesaplari []Hesap
	_ = json.Unmarshal([]byte(kesifJSON), &oturumHesaplari)
	sunucu := map[string]Hesap{}
	for _, hs := range oturumHesaplari {
		sunucu[strings.ToLower(hs.AlanAdi)] = hs
	}
	var gecerli []Hesap
	gorulen := map[string]bool{}
	for _, hs := range g.Secilen {
		a := strings.ToLower(strings.TrimSpace(hs.AlanAdi))
		if s, ok := sunucu[a]; ok && !gorulen[a] && reAlanAdi.MatchString(a) && strings.Contains(a, ".") {
			gorulen[a] = true
			s.AlanAdi = a
			gecerli = append(gecerli, s)
		}
	}
	if len(gecerli) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "gecerli hesap bulunamadi")
		return
	}

	ay := g.Ayarlar
	ay.SahipEsle = nil
	ay.SahipleriTasi = false // arşivde sahip/reseller bilgisi taşınmaz (iddia, yetki değildir)
	ay.SSL = false           // arşivde kullanılabilir sertifika yok (yeniden düzenlenir)
	ay.Hesaplar = nil
	if kp.Bayi() {
		ay.ResellerID, ay.CustomerID = kp.RID, 0
		ay.Ustune = false
		planID, kod, msj := h.bayiPlanCoz(ctx, kp.RID, ay.PlanID)
		if kod != 0 {
			httpx.WriteError(w, kod, msj)
			return
		}
		ay.PlanID = planID
		if kod, msj := h.bayiKalanDenetle(ctx, kp.RID, len(gecerli)); kod != 0 {
			httpx.WriteError(w, kod, msj)
			return
		}
	}
	kayit := isAyarKayit{Ayarlar: ay, Arsiv: &isArsivKaynak{Yol: meta.Yol, YuklemeID: meta.YuklemeID, ParmakIzi: meta.ParmakIzi}}
	ayarJSON, _ := json.Marshal(kayit)

	baslatMu.Lock()
	var acik int
	if err := h.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasima_isleri WHERE sahip_rid=? AND durum IN ('kuyrukta','calisiyor')`, kp.RID).Scan(&acik); err != nil {
		baslatMu.Unlock()
		httpx.WriteError(w, http.StatusInternalServerError, "is kaydi olusturulamadi")
		return
	}
	if acik > 0 {
		baslatMu.Unlock()
		httpx.WriteError(w, http.StatusConflict, "devam eden ya da sırada bekleyen bir taşımanız var")
		return
	}
	res, err := h.DB.ExecContext(ctx,
		`INSERT INTO tasima_isleri (kaynak_tip, kaynak_host, kaynak_port, kaynak_kullanici,
		   tasima_modu, durum, toplam, ayarlar, baslatan, sahip_rid, baslatan_uid, arsiv_yukleme_id)
		 VALUES (?,?,0,?,?, 'kuyrukta', ?, ?, ?, ?, NULLIF(?,0), NULLIF(?,0))`,
		g.Tip, arsivHostAdi, arsivHostAdi, mod, len(gecerli), string(ayarJSON), kp.Kul, kp.RID, kp.UID, meta.YuklemeID)
	if err != nil {
		baslatMu.Unlock()
		log.Printf("tasima: arsiv is kaydi olusturulamadi: %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, "is kaydi olusturulamadi")
		return
	}
	isID, _ := res.LastInsertId()
	for _, hs := range gecerli {
		hj, _ := json.Marshal(hs)
		if _, err := h.DB.ExecContext(ctx,
			`INSERT INTO tasima_kalemleri (is_id, kaynak_hesap, alan_adi, durum, hesap_json)
			 VALUES (?,?,?, 'bekliyor', ?)`, isID, hs.KaynakHesap, hs.AlanAdi, string(hj)); err != nil {
			log.Printf("tasima: arsiv kalemi yazilamadi (is=%d): %v", isID, err)
		}
	}
	baslatMu.Unlock()

	httpx.Denetim(h.DB, r, kp.UID, kp.Kul, "tasima_baslat", g.Tip, fmt.Sprintf("%d site", len(gecerli)), kp.RID, true)
	h.dagit()
	durum := "kuyrukta"
	_ = h.DB.QueryRowContext(ctx, `SELECT durum FROM tasima_isleri WHERE id=?`, isID).Scan(&durum)
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"is_id": isID, "toplam": len(gecerli), "durum": durum})
}
