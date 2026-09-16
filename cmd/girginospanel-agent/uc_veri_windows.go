//go:build windows

// uc_veri_windows.go — YEREL PANEL veritabani yonetim uclari (8443).
//
// 🔴 ALTYAPI DISARIDAN ALINIR: bu dosya oturum deposunu ve JSON yazicisini
// KOPYALAMAZ; VeriUclariniKaydet, yerelpanel_windows.go'daki oturumlu + yerelJSON
// kapanislarini parametre olarak alir. Boylece tek bir cagri satiriyla baglanir
// ve yerelpanel_windows.go'ya DOKUNULMADAN uclar ayri dosyada durur.
//
// Uc sozlesmeleri (hepsi oturumlu):
//
//	GET    /api/yerel/vt-motorlar          -> {"motorlar":[{Tur,Ad,Kurulu}]}
//	GET    /api/yerel/vt-liste?motor=mssql -> {"veritabanlari":[{Ad,BoyutMB}]} | 422
//	POST   /api/yerel/vt-olustur {motor,ad,kullanici,parola} -> {"ok":true} | 400/422
//	DELETE /api/yerel/vt-sil ?motor=&ad=   -> {"ok":true} | 400/422
//
// Kod eslemesi + zarf: standart hata modeli (hata_windows.go) — sentinel'den
// kod+durum (dogrulama 400, parola/koruma 422, beklenmeyen 500, yontem 405) +
// istek_id + retryable; geriye donuk `hata` alani korunur.
package main

import (
	"encoding/json"
	"net/http"

	"girginospanel/internal/platform"
)

// VeriUclariniKaydet — veritabani yonetim uclarini mux'a baglar. Cagiran
// (yerelPanelSunucu) oturumlu ve yerelJSON'u gecirir; imzalar bilerek yerel
// panelin mevcut kapanislariyla birebir uyumludur.
func VeriUclariniKaydet(mux *http.ServeMux, oturumlu func(http.HandlerFunc) http.HandlerFunc, yaz func(http.ResponseWriter, int, any)) {
	// GET /api/yerel/vt-motorlar — kurulu motor matrisi.
	mux.HandleFunc("/api/yerel/vt-motorlar", oturumlu(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			yontemHatasi(w, r)
			return
		}
		yaz(w, http.StatusOK, map[string]any{"motorlar": platform.VeritabaniMotorlari()})
	}))

	// GET /api/yerel/vt-liste?motor= — motorun veritabanlarini listeler.
	// Parola gerektiren motor (mysql/pgsql) 422 doner; motor eksik/gecersiz 400.
	mux.HandleFunc("/api/yerel/vt-liste", oturumlu(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			yontemHatasi(w, r)
			return
		}
		motor := r.URL.Query().Get("motor")
		if motor == "" {
			zarfYaz(w, r, http.StatusBadRequest, KodGecersizIstek, "motor parametresi gerekli", false)
			return
		}
		liste, err := platform.VeritabaniListe(motor)
		if err != nil {
			hataYaz(w, r, http.StatusInternalServerError, err)
			return
		}
		yaz(w, http.StatusOK, map[string]any{"veritabanlari": liste})
	}))

	// POST /api/yerel/vt-olustur {motor,ad,kullanici,parola} — yalniz mssql TAM.
	mux.HandleFunc("/api/yerel/vt-olustur", oturumlu(denetimli("vt-olustur", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			yontemHatasi(w, r)
			return
		}
		var ist struct {
			Motor     string `json:"motor"`
			Ad        string `json:"ad"`
			Kullanici string `json:"kullanici"`
			Parola    string `json:"parola"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&ist); err != nil {
			govdeHatasi(w, r)
			return
		}
		if err := platform.VeritabaniOlustur(ist.Motor, ist.Ad, ist.Kullanici, ist.Parola); err != nil {
			hataYaz(w, r, http.StatusUnprocessableEntity, err)
			return
		}
		yaz(w, http.StatusOK, map[string]bool{"ok": true})
	})))

	// DELETE /api/yerel/vt-sil?motor=&ad= — yalniz mssql; sistem db korunur.
	mux.HandleFunc("/api/yerel/vt-sil", oturumlu(denetimli("vt-sil", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			yontemHatasi(w, r)
			return
		}
		motor := r.URL.Query().Get("motor")
		ad := r.URL.Query().Get("ad")
		if motor == "" || ad == "" {
			zarfYaz(w, r, http.StatusBadRequest, KodGecersizIstek, "motor ve ad parametreleri gerekli", false)
			return
		}
		if err := platform.VeritabaniSil(motor, ad); err != nil {
			hataYaz(w, r, http.StatusUnprocessableEntity, err)
			return
		}
		yaz(w, http.StatusOK, map[string]bool{"ok": true})
	})))
}
