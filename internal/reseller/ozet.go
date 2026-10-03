package reseller

import (
	"context"
	"net/http"

	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
)

// Ozet: GET /reseller/ozet — bayinin kendi panosu (hosting adedi, disk/trafik havuzu,
// plan sayisi, askidaki hesaplar). Admin cagirirsa 400 (adminin kendi panosu ayri).
func (h *Handlers) Ozet(w http.ResponseWriter, r *http.Request) {
	rid := middleware.ResellerIDFrom(r)
	if rid == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "bu özet yalnız bayi hesapları içindir")
		return
	}
	var maxDomain int
	var maxDisk, maxTrafik int64
	var paketAd string
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT u.max_domain, u.max_disk_mb, u.max_trafik_mb,
		        COALESCE((SELECT ad FROM reseller_plans rp WHERE rp.id=u.reseller_plan_id),'')
		   FROM users u WHERE u.id=?`, rid).
		Scan(&maxDomain, &maxDisk, &maxTrafik, &paketAd)

	var adet, askida int
	var diskKB, trafikKB int64
	var taahhutDisk, taahhutTrafik int64
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*), COALESCE(SUM(boyut_kb),0), COALESCE(SUM(trafik_kb),0),
		        COALESCE(SUM(COALESCE(askida,0)),0)
		   FROM domains WHERE reseller_id=?`, rid).
		Scan(&adet, &diskKB, &trafikKB, &askida)
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT COALESCE(SUM(COALESCE(p.disk_kota_mb,0)),0), COALESCE(SUM(COALESCE(p.trafik_kota_mb,0)),0)
		   FROM domains d LEFT JOIN service_plans p ON p.id=d.plan_id WHERE d.reseller_id=?`, rid).
		Scan(&taahhutDisk, &taahhutTrafik)

	var planAdet, sablonSatir int
	_ = h.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM service_plans WHERE reseller_id=?`, rid).Scan(&planAdet)
	_ = h.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM dns_template WHERE reseller_id=?`, rid).Scan(&sablonSatir)

	son, dikkat := h.panoListeleri(r.Context(), rid)

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"paket_ad":           paketAd,
		"hosting_adet":       adet,
		"hosting_limit":      maxDomain,
		"askida_adet":        askida,
		"disk_kullanim_kb":   diskKB,
		"disk_taahhut_mb":    taahhutDisk,
		"disk_limit_mb":      maxDisk,
		"trafik_kullanim_kb": trafikKB,
		"trafik_taahhut_mb":  taahhutTrafik,
		"trafik_limit_mb":    maxTrafik,
		"plan_adet":          planAdet,
		"dns_sablon_ozel":    sablonSatir > 0,
		"son_domainler":      son,
		"dikkat":             dikkat,
	})
}

// SonDomain: bayi panosundaki "son oluşturulan alan adları" satırı.
type SonDomain struct {
	ID          int64  `json:"id"`
	AlanAdi     string `json:"alan_adi"`
	PlanAd      string `json:"plan_ad"`
	BoyutKB     int64  `json:"boyut_kb"`
	DiskKotaMB  int64  `json:"disk_kota_mb"`
	Askida      bool   `json:"askida"`
	Olusturulma string `json:"olusturulma"`
}

// Dikkat: panodaki "dikkat gerektirenler" kalemi. Metni arayüz kurar (TR/EN).
//   - ssl_alinamadi (kritik): SSL açık ama sertifikanın süresi DOLMUŞ — acme.sh
//     otomatik yenilemesi başarısız olmuş demektir (yenilenen sertifika bitmez).
//   - disk_yuksek (uyari): fiili kullanım plan kotasının %90'ına ulaştı.
//   - askida (bilgi): askıdaki hesaplar (adet + ilk alan adları).
type Dikkat struct {
	Tur      string   `json:"tur"`
	Onem     string   `json:"onem"`
	DomainID int64    `json:"domain_id,omitempty"`
	AlanAdi  string   `json:"alan_adi,omitempty"`
	Yuzde    int      `json:"yuzde,omitempty"`
	Adet     int      `json:"adet,omitempty"`
	Ornekler []string `json:"ornekler,omitempty"`
}

// panoListeleri: son 10 alan adı (oluşturulma yeni→eski) + dikkat kalemleri.
// Yalnız bayinin KENDİ hostingleri (reseller_id=rid). Hata panoyu düşürmez:
// ilgili liste boş döner (özet sayıları yine gelir).
func (h *Handlers) panoListeleri(ctx context.Context, rid int64) ([]SonDomain, []Dikkat) {
	son := make([]SonDomain, 0, 10)
	if rows, err := h.DB.QueryContext(ctx,
		`SELECT d.id, d.alan_adi, COALESCE(p.ad,''), COALESCE(d.boyut_kb,0), COALESCE(p.disk_kota_mb,0),
		        COALESCE(d.askida,0), DATE_FORMAT(d.olusturulma,'%Y-%m-%d')
		   FROM domains d LEFT JOIN service_plans p ON p.id=d.plan_id
		  WHERE d.reseller_id=?
		  ORDER BY d.olusturulma DESC, d.id DESC LIMIT 10`, rid); err == nil {
		for rows.Next() {
			var s SonDomain
			var askida int
			if rows.Scan(&s.ID, &s.AlanAdi, &s.PlanAd, &s.BoyutKB, &s.DiskKotaMB, &askida, &s.Olusturulma) == nil {
				s.Askida = askida == 1
				son = append(son, s)
			}
		}
		rows.Close()
	}

	dikkat := make([]Dikkat, 0, 8)
	if rows, err := h.DB.QueryContext(ctx,
		`SELECT id, alan_adi FROM domains
		  WHERE reseller_id=? AND ssl_aktif=1 AND ssl_bitis IS NOT NULL AND ssl_bitis < CURDATE()
		    AND COALESCE(askida,0)=0 AND COALESCE(is_demo,0)=0
		  ORDER BY ssl_bitis LIMIT 20`, rid); err == nil {
		for rows.Next() {
			var d Dikkat
			if rows.Scan(&d.DomainID, &d.AlanAdi) == nil {
				d.Tur, d.Onem = "ssl_alinamadi", "kritik"
				dikkat = append(dikkat, d)
			}
		}
		rows.Close()
	}
	if rows, err := h.DB.QueryContext(ctx,
		`SELECT d.id, d.alan_adi, COALESCE(d.boyut_kb,0), p.disk_kota_mb
		   FROM domains d JOIN service_plans p ON p.id=d.plan_id
		  WHERE d.reseller_id=? AND p.disk_kota_mb > 0 AND d.boyut_kb >= p.disk_kota_mb*1024*0.9
		  ORDER BY d.boyut_kb/(p.disk_kota_mb*1024) DESC LIMIT 20`, rid); err == nil {
		for rows.Next() {
			var d Dikkat
			var kb, kota int64
			if rows.Scan(&d.DomainID, &d.AlanAdi, &kb, &kota) == nil && kota > 0 {
				d.Tur, d.Onem = "disk_yuksek", "uyari"
				d.Yuzde = int(kb * 100 / (kota * 1024))
				dikkat = append(dikkat, d)
			}
		}
		rows.Close()
	}
	var askiAdet int
	_ = h.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM domains WHERE reseller_id=? AND COALESCE(askida,0)=1`, rid).Scan(&askiAdet)
	if askiAdet > 0 {
		d := Dikkat{Tur: "askida", Onem: "bilgi", Adet: askiAdet}
		if rows, err := h.DB.QueryContext(ctx,
			`SELECT alan_adi FROM domains WHERE reseller_id=? AND COALESCE(askida,0)=1 ORDER BY alan_adi LIMIT 3`, rid); err == nil {
			for rows.Next() {
				var a string
				if rows.Scan(&a) == nil {
					d.Ornekler = append(d.Ornekler, a)
				}
			}
			rows.Close()
		}
		dikkat = append(dikkat, d)
	}
	return son, dikkat
}
