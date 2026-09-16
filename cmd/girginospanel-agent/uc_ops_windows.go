//go:build windows

// uc_ops_windows.go — yerel panelin OPERASYON uclari: site/uygulama yonetimi
// (appcmd) + servis/kaynak operasyonu.
//
// 🔴 MIMARI: bu dosya yerelpanel_windows.go'ya DOKUNMADAN, uclarini tek bir
// disa acik fonksiyonla (OpsUclariniKaydet) o dosyadaki mux'a takar. `oturumlu`
// middleware'i ve `yaz` (=yerelJSON) yazicisi PARAMETRE olarak alinir; ikisi de
// yerelpanel_windows.go'da tanimli, ana kurucu buraya gecirir. Boylece iki dosya
// birbirinin govdesini duzenlemeden ayni oturum + JSON sozlesmesini paylasir.
//
// HATA KODU SOZLESMESI (her ucta ayni):
//   - platform.ErrGecersizIstek sarili hata  -> 400 (dogrulama)
//   - okuma uclarinda diger hata             -> 500 (islem)
//   - mutasyon/aksiyon uclarinda diger hata  -> 422 (islem)
//   - yanlis HTTP metodu                     -> 405
package main

import (
	"encoding/json"
	"net/http"

	"girginospanel/internal/platform"
)

// opsYazFn — yerelpanel_windows.go'daki yerelJSON'un tip esi (alias: birebir
// ayni tip, gecirilen yerelJSON sorunsuz uyar).
type opsYazFn = func(http.ResponseWriter, int, any)

// OpsUclariniKaydet — operasyon uclarini mux'a takar. Ana kurucu (yerelPanel
// sunucusu) bunu `OpsUclariniKaydet(mux, oturumlu, yerelJSON)` imzasiyla cagirir.
func OpsUclariniKaydet(mux *http.ServeMux, oturumlu func(http.HandlerFunc) http.HandlerFunc, yaz func(http.ResponseWriter, int, any)) {
	// Okuma uclari (site-detay/servisler/kaynak) denetim logu ISTEMEZ (mutasyon
	// degil, gurultu olur); mutasyon uclari `denetimli` ile sarilir (B-09).
	mux.HandleFunc("/api/yerel/site-detay", oturumlu(opsSiteDetay(yaz)))
	mux.HandleFunc("/api/yerel/havuz-islem", oturumlu(denetimli("havuz-islem", opsHavuzIslem(yaz))))
	mux.HandleFunc("/api/yerel/havuz-dotnet", oturumlu(denetimli("havuz-dotnet", opsHavuzDotNet(yaz))))
	mux.HandleFunc("/api/yerel/baglama-ekle", oturumlu(denetimli("baglama-ekle", opsBaglamaEkle(yaz))))
	mux.HandleFunc("/api/yerel/baglama", oturumlu(denetimli("baglama-sil", opsBaglamaSil(yaz))))
	mux.HandleFunc("/api/yerel/site-ssl", oturumlu(denetimli("site-ssl", opsSiteSSL(yaz))))
	mux.HandleFunc("/api/yerel/servisler", oturumlu(opsServisler(yaz)))
	mux.HandleFunc("/api/yerel/servis-islem", oturumlu(denetimli("servis-islem", opsServisIslem(yaz))))
	mux.HandleFunc("/api/yerel/kaynak", oturumlu(opsKaynak(yaz)))
	mux.HandleFunc("/api/yerel/kurulum-kilit-temizle", oturumlu(denetimli("kurulum-kilit-temizle", opsKurulumKilitTemizle(yaz))))
}

// opsKurulumKilitTemizle — POST /api/yerel/kurulum-kilit-temizle: ajan restart'inda
// yarim kalan (asili) kurulum kilidini temizler; operator calisan bir yukleyici
// olmadigindan emin olduktan sonra cagirir, boylece yeni kurulumlar tekrar acilir.
func opsKurulumKilitTemizle(yaz opsYazFn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !opsMetot(yaz, w, r, http.MethodPost) {
			return
		}
		platform.KurulumAsiliTemizle()
		yaz(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// ── ortak yardimcilar ────────────────────────────────────────────────────────

// opsHataYaz — STANDART HATA ZARFI (bkz. hata_windows.go): sentinel'den kod+durum
// +retryable turetir; sentinel disi hata ucun verdigi islemKodu'nu (422/500) alir.
// 🔴 `yaz` artik kullanilmiyor ama imza KORUNDU — 9 cagiranin degismesi (ve
// dolayisiyla regresyon riski) icin gereksiz. r gecirilmedigi icin log method/yol
// yerine "-" yazar; istek_id yine uretilir ve yanit+log korelasyonu saglanir
// (tam istek-kapsamli korelasyon B-09'da).
func opsHataYaz(yaz opsYazFn, w http.ResponseWriter, islemKodu int, err error) {
	kod, durum, yeniden := kodCoz(err, islemKodu)
	zarfYaz(w, nil, durum, kod, err.Error(), yeniden)
}

// opsGovdeCoz — JSON govdesini cozer (MaxBytesReader 4<<10). Basarisizsa standart
// GOVDE_OKUNAMADI zarfini (400) yazar ve false doner.
func opsGovdeCoz(yaz opsYazFn, w http.ResponseWriter, r *http.Request, hedef any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(hedef); err != nil {
		govdeHatasi(w, r)
		return false
	}
	return true
}

// opsMetot — beklenen metodu dogrular; degilse standart YONTEM_YOK zarfini (405)
// yazar ve false doner.
func opsMetot(yaz opsYazFn, w http.ResponseWriter, r *http.Request, beklenen string) bool {
	if r.Method != beklenen {
		yontemHatasi(w, r)
		return false
	}
	return true
}

// ── site / uygulama uclari ───────────────────────────────────────────────────

// opsSiteDetay — GET /api/yerel/site-detay?ad=<alan> -> SiteDetayGoruntu.
func opsSiteDetay(yaz opsYazFn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !opsMetot(yaz, w, r, http.MethodGet) {
			return
		}
		d, err := platform.SiteDetay(r.URL.Query().Get("ad"))
		if err != nil {
			opsHataYaz(yaz, w, http.StatusInternalServerError, err)
			return
		}
		yaz(w, http.StatusOK, d)
	}
}

// opsHavuzIslem — POST /api/yerel/havuz-islem {ad,islem}.
func opsHavuzIslem(yaz opsYazFn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !opsMetot(yaz, w, r, http.MethodPost) {
			return
		}
		var ist struct {
			Ad    string `json:"ad"`
			Islem string `json:"islem"`
		}
		if !opsGovdeCoz(yaz, w, r, &ist) {
			return
		}
		if err := platform.HavuzIslem(ist.Ad, ist.Islem); err != nil {
			opsHataYaz(yaz, w, http.StatusUnprocessableEntity, err)
			return
		}
		yaz(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// opsHavuzDotNet — POST /api/yerel/havuz-dotnet {ad,surum}.
func opsHavuzDotNet(yaz opsYazFn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !opsMetot(yaz, w, r, http.MethodPost) {
			return
		}
		var ist struct {
			Ad    string `json:"ad"`
			Surum string `json:"surum"`
		}
		if !opsGovdeCoz(yaz, w, r, &ist) {
			return
		}
		if err := platform.HavuzDotNet(ist.Ad, ist.Surum); err != nil {
			opsHataYaz(yaz, w, http.StatusUnprocessableEntity, err)
			return
		}
		yaz(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// opsBaglamaEkle — POST /api/yerel/baglama-ekle {site,protokol,port,host}.
// port bilerek dizge: DELETE tarafiyla (sorgu parametresi) ayni tip, dogrulama
// (1-65535) platform katinda yapilir.
func opsBaglamaEkle(yaz opsYazFn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !opsMetot(yaz, w, r, http.MethodPost) {
			return
		}
		var ist struct {
			Site     string `json:"site"`
			Protokol string `json:"protokol"`
			Port     string `json:"port"`
			Host     string `json:"host"`
		}
		if !opsGovdeCoz(yaz, w, r, &ist) {
			return
		}
		if err := platform.BaglamaEkle(ist.Site, ist.Protokol, ist.Port, ist.Host); err != nil {
			opsHataYaz(yaz, w, http.StatusUnprocessableEntity, err)
			return
		}
		yaz(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// opsBaglamaSil — DELETE /api/yerel/baglama?site=&protokol=&port=&host=.
func opsBaglamaSil(yaz opsYazFn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !opsMetot(yaz, w, r, http.MethodDelete) {
			return
		}
		q := r.URL.Query()
		if err := platform.BaglamaSil(q.Get("site"), q.Get("protokol"), q.Get("port"), q.Get("host")); err != nil {
			opsHataYaz(yaz, w, http.StatusUnprocessableEntity, err)
			return
		}
		yaz(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// opsSiteSSL — POST /api/yerel/site-ssl {site} -> 200 {mesaj} / 422 {hata}.
// 🔴 wacs BAŞARILI olursa 200 + dürüst mesaj; wacs zaman aşımı/hata ile
// çıkarsa (sertifika alınamadı) 422 — 200-içi-gizli-hata YOK. Ön koşul
// hataları (geçersiz ad/wacs yok/site yok) 422/400.
func opsSiteSSL(yaz opsYazFn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !opsMetot(yaz, w, r, http.MethodPost) {
			return
		}
		var ist struct {
			Site string `json:"site"`
		}
		if !opsGovdeCoz(yaz, w, r, &ist) {
			return
		}
		mesaj, err := platform.SiteSSLLetsEncrypt(ist.Site)
		if err != nil {
			opsHataYaz(yaz, w, http.StatusUnprocessableEntity, err)
			return
		}
		yaz(w, http.StatusOK, map[string]string{"mesaj": mesaj})
	}
}

// ── servis / kaynak uclari ───────────────────────────────────────────────────

// opsServisler — GET /api/yerel/servisler -> {servisler:[...]}.
func opsServisler(yaz opsYazFn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !opsMetot(yaz, w, r, http.MethodGet) {
			return
		}
		liste, err := platform.ServisListe()
		if err != nil {
			opsHataYaz(yaz, w, http.StatusInternalServerError, err)
			return
		}
		yaz(w, http.StatusOK, map[string]any{"servisler": liste})
	}
}

// opsServisIslem — POST /api/yerel/servis-islem {ad,islem}.
func opsServisIslem(yaz opsYazFn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !opsMetot(yaz, w, r, http.MethodPost) {
			return
		}
		var ist struct {
			Ad    string `json:"ad"`
			Islem string `json:"islem"`
		}
		if !opsGovdeCoz(yaz, w, r, &ist) {
			return
		}
		if err := platform.ServisIslem(ist.Ad, ist.Islem); err != nil {
			opsHataYaz(yaz, w, http.StatusUnprocessableEntity, err)
			return
		}
		yaz(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// opsKaynak — GET /api/yerel/kaynak -> KaynakGoruntu.
func opsKaynak(yaz opsYazFn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !opsMetot(yaz, w, r, http.MethodGet) {
			return
		}
		k, err := platform.KaynakDurum()
		if err != nil {
			opsHataYaz(yaz, w, http.StatusInternalServerError, err)
			return
		}
		yaz(w, http.StatusOK, k)
	}
}
