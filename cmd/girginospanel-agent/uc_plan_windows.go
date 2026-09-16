//go:build windows

// uc_plan_windows.go — yerel panel PLAN (paket) uclari: plan CRUD + siteye atama/
// kaldirma + disk kota motoru (FSRM) kurulumu.
//
// 🔴 MIMARI: uc_ops_windows.go ile AYNI sozlesme — `oturumlu` middleware'i ve
// `yaz` (=yerelJSON) PARAMETRE gelir; mutasyonlar `denetimli()` ile denetim
// loguna yazilir. Hata kodu: platform.ErrGecersizIstek -> 400 (hataYaz icinde),
// diger mutasyon hatasi -> 422, okuma hatasi -> 500, yanlis metot -> 405.
package main

import (
	"net/http"

	"girginospanel/internal/platform"
)

// PlanUclariniKaydet — plan uclarini mux'a takar (yerelPanelSunucu cagirir).
func PlanUclariniKaydet(mux *http.ServeMux, oturumlu func(http.HandlerFunc) http.HandlerFunc, yaz func(http.ResponseWriter, int, any)) {
	mux.HandleFunc("/api/yerel/planlar", oturumlu(planlarUcu(yaz)))
	mux.HandleFunc("/api/yerel/plan-kaydet", oturumlu(denetimli("plan-kaydet", planKaydetUcu(yaz))))
	mux.HandleFunc("/api/yerel/plan-sil", oturumlu(denetimli("plan-sil", planSilUcu(yaz))))
	mux.HandleFunc("/api/yerel/plan-ata", oturumlu(denetimli("plan-ata", planAtaUcu(yaz))))
	mux.HandleFunc("/api/yerel/plan-kaldir", oturumlu(denetimli("plan-kaldir", planKaldirUcu(yaz))))
	mux.HandleFunc("/api/yerel/kota-motoru-kur", oturumlu(denetimli("kota-motoru-kur", kotaMotoruKurUcu(yaz))))
}

// GET /api/yerel/planlar -> PlanDurumu (planlar + atamalar + FSRM durumu)
func planlarUcu(yaz opsYazFn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !opsMetot(yaz, w, r, http.MethodGet) {
			return
		}
		d, err := platform.PlanDurumuGetir()
		if err != nil {
			opsHataYaz(yaz, w, http.StatusInternalServerError, err)
			return
		}
		yaz(w, http.StatusOK, d)
	}
}

// POST /api/yerel/plan-kaydet {ad, diskKotaMB, maxBaglanti, maxBantGenisligiKBs, cpuLimitYuzde, bellekMB}
func planKaydetUcu(yaz opsYazFn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !opsMetot(yaz, w, r, http.MethodPost) {
			return
		}
		var p platform.Plan
		if !opsGovdeCoz(yaz, w, r, &p) {
			return
		}
		if err := platform.PlanKaydet(p); err != nil {
			opsHataYaz(yaz, w, http.StatusUnprocessableEntity, err)
			return
		}
		yaz(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// POST /api/yerel/plan-sil {ad}
func planSilUcu(yaz opsYazFn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !opsMetot(yaz, w, r, http.MethodPost) {
			return
		}
		var ist struct {
			Ad string `json:"ad"`
		}
		if !opsGovdeCoz(yaz, w, r, &ist) {
			return
		}
		if err := platform.PlanSil(ist.Ad); err != nil {
			opsHataYaz(yaz, w, http.StatusUnprocessableEntity, err)
			return
		}
		yaz(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// POST /api/yerel/plan-ata {site, plan} -> AtaSonuc
func planAtaUcu(yaz opsYazFn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !opsMetot(yaz, w, r, http.MethodPost) {
			return
		}
		var ist struct {
			Site string `json:"site"`
			Plan string `json:"plan"`
		}
		if !opsGovdeCoz(yaz, w, r, &ist) {
			return
		}
		s, err := platform.PlanAta(ist.Site, ist.Plan)
		if err != nil {
			opsHataYaz(yaz, w, http.StatusUnprocessableEntity, err)
			return
		}
		yaz(w, http.StatusOK, s)
	}
}

// POST /api/yerel/plan-kaldir {site}
func planKaldirUcu(yaz opsYazFn) http.HandlerFunc {
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
		if err := platform.PlanKaldir(ist.Site); err != nil {
			opsHataYaz(yaz, w, http.StatusUnprocessableEntity, err)
			return
		}
		yaz(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// POST /api/yerel/kota-motoru-kur -> {ok, restart}. FSRM ozelligini kurar; uzun
// surer (sunucuda WriteTimeout yok).
func kotaMotoruKurUcu(yaz opsYazFn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !opsMetot(yaz, w, r, http.MethodPost) {
			return
		}
		restart, err := platform.KotaMotoruKur()
		if err != nil {
			opsHataYaz(yaz, w, http.StatusUnprocessableEntity, err)
			return
		}
		yaz(w, http.StatusOK, map[string]bool{"ok": true, "restart": restart})
	}
}
