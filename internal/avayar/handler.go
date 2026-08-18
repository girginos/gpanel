package avayar

// Panel API — antivirüs platform ayarları ve kaynak limitleri.
//
// 🔴 Yanıt ÜÇ ŞEYİ BİRDEN döner ve bu kasıtlı:
//	ayarlar  — operatörün yazdığı (0 = otomatik olabilir)
//	etkin    — "0 = otomatik" çözüldükten sonraki gerçek değerler
//	cekirdek — systemd'nin ŞU AN UYGULADIĞI
// Üçü ayrı gösterilmezse operatör "%50 yazdım" der ama sistemde ne olduğunu
// bilemez. Bu projede defalarca yaşanan "yazdım == oldu" varsayımını burada
// baştan kapatıyoruz.

import (
	"database/sql"
	"encoding/json"
	"net/http"
)

type Handlers struct{ DB *sql.DB }

type EtkinDegerler struct {
	CPUYuzde    int `json:"cpu_yuzde"`
	RAMMb       int `json:"ram_mb"`
	IsParcacigi int `json:"is_parcacigi"`
}

type Yanit struct {
	Ayarlar  Ayarlar           `json:"ayarlar"`
	Kapasite Kapasite          `json:"kapasite"`
	Etkin    EtkinDegerler     `json:"etkin"`
	Cekirdek map[string]string `json:"cekirdek"`
	Kokler   []string          `json:"tarama_kokleri"`
}

// Getir — GET /api/v1/antivirus/ayarlar
func (h *Handlers) Getir(w http.ResponseWriter, r *http.Request) {
	a, err := Oku(r.Context(), h.DB)
	if err != nil {
		avHata(w, 500, "ayarlar okunamadi: "+err.Error())
		return
	}
	k := SunucuKapasitesi()
	c, m, i := a.Etkin(k)
	avJSON(w, 200, Yanit{
		Ayarlar:  a,
		Kapasite: k,
		Etkin:    EtkinDegerler{CPUYuzde: c, RAMMb: m, IsParcacigi: i},
		Cekirdek: LimitDurumu(),
		Kokler:   a.TaramaKokleri(),
	})
}

// Kaydet — PUT /api/v1/antivirus/ayarlar
func (h *Handlers) Kaydet(w http.ResponseWriter, r *http.Request) {
	var a Ayarlar
	if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
		avHata(w, 400, "govde ayristirilamadi: "+err.Error())
		return
	}
	// 🔴 Eşik motorla tutarlı kalmalı; sıfır/negatif eşik "her dosya kritik"
	// demek olurdu ve tüm siteyi karantinaya alırdı.
	if a.EsikKritik < 20 {
		avHata(w, 400, "esik_kritik en az 20 olmali (dusuk esik tum siteyi karantinaya alir)")
		return
	}
	if a.CPUYuzde < 0 || a.RAMMb < 0 || a.IsParcacigi < 0 || a.DosyaHizSn < 0 {
		avHata(w, 400, "limitler negatif olamaz (0 = otomatik)")
		return
	}
	if err := Yaz(r.Context(), h.DB, a); err != nil {
		avHata(w, 400, err.Error())
		return
	}
	h.Getir(w, r) // kaydettikten sonra GERÇEK durumu geri döndür
}

func avJSON(w http.ResponseWriter, kod int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(kod)
	_ = json.NewEncoder(w).Encode(v)
}

func avHata(w http.ResponseWriter, kod int, msg string) {
	avJSON(w, kod, map[string]string{"hata": msg})
}
