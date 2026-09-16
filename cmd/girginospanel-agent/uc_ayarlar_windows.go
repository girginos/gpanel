//go:build windows

// uc_ayarlar_windows.go — yerel panel GENEL AYARLAR ucu: panel 'admin'
// parolasini degistirme. (Dil client-side i18n ile; ajan bilgisi mevcut
// /api/yerel/ozet ucundan okunur — bu dosya yalniz parola degisimini ekler.)
//
// 🔴 Parola degisimi ajan.json'a yeni bcrypt hash yazar; girisUcu hash'i HER
// denemede diskten taze okur (yerelpanel_windows.go) → RESTART BEKLEMEDEN gecerli.
// ajan.json DOGRUDAN okunur (ayarYukle DEGIL): ayarYukle ortam degiskeni
// ezmelerini uygular, ezilmis degeri geri yazmak config'i bozar (panelParolaSifirla
// ile ayni disiplin). Dosya ACL'i (icacls, SYSTEM+Admins) WriteFile'da korunur
// (O_TRUNC var olan dosyayi ve ACL'ini tutar).
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"girginospanel/internal/platform"

	"golang.org/x/crypto/bcrypt"
)

// AyarUclariniKaydet — genel ayar uclarini mux'a takar (yerelPanelSunucu cagirir).
func AyarUclariniKaydet(mux *http.ServeMux, oturumlu func(http.HandlerFunc) http.HandlerFunc, yaz func(http.ResponseWriter, int, any)) {
	mux.HandleFunc("/api/yerel/parola-degistir", oturumlu(denetimli("parola-degistir", parolaDegistirUcu(yaz))))
}

// POST /api/yerel/parola-degistir {eskiParola, yeniParola}
func parolaDegistirUcu(yaz opsYazFn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !opsMetot(yaz, w, r, http.MethodPost) {
			return
		}
		var ist struct {
			EskiParola string `json:"eskiParola"`
			YeniParola string `json:"yeniParola"`
		}
		if !opsGovdeCoz(yaz, w, r, &ist) {
			return
		}
		if len(ist.YeniParola) < 8 || len(ist.YeniParola) > 200 {
			opsHataYaz(yaz, w, http.StatusUnprocessableEntity,
				fmt.Errorf("yeni parola 8-200 karakter olmali: %w", platform.ErrGecersizIstek))
			return
		}
		b, err := os.ReadFile(ayarYolu())
		if err != nil {
			opsHataYaz(yaz, w, http.StatusInternalServerError, fmt.Errorf("ayar okunamadi: %w", err))
			return
		}
		var ayar ajanAyar
		if err := json.Unmarshal(b, &ayar); err != nil {
			opsHataYaz(yaz, w, http.StatusInternalServerError, fmt.Errorf("ayar cozulemedi: %w", err))
			return
		}
		// 🔴 Mevcut parola dogrulanir. Yanlissa SABIT 500ms gecikme (giris ucuyla
		// tutarli kaba-kuvvet yavaslatma). 🔴 422 doner (401 DEGIL): SPA'nin api()
		// yardimcisi /giris disi HER 401'i "oturum bitti" sayip login'e dusurur →
		// 401 kullaniciyi sessizce atardi (denetim bulgusu). ErrGecersizIstek de
		// SARILMAZ ki 400'e donusmesin; mutasyon-dogrulama sozlesmesi geregi 422.
		if ayar.PanelParolaHash == "" ||
			bcrypt.CompareHashAndPassword([]byte(ayar.PanelParolaHash), []byte(ist.EskiParola)) != nil {
			time.Sleep(500 * time.Millisecond)
			opsHataYaz(yaz, w, http.StatusUnprocessableEntity, fmt.Errorf("mevcut parola hatali"))
			return
		}
		h, err := bcrypt.GenerateFromPassword([]byte(ist.YeniParola), bcrypt.DefaultCost)
		if err != nil {
			opsHataYaz(yaz, w, http.StatusInternalServerError, fmt.Errorf("parola hashlenemedi: %w", err))
			return
		}
		ayar.PanelParolaHash = string(h)
		y, _ := json.MarshalIndent(ayar, "", "  ")
		// 🔴 Atomik yaz: gecici + rename (planYaz ile ayni disiplin). Duz WriteFile
		// yarida kesilirse ajan.json (kayit jetonu dahil) bozulur → ayarYukle "jeton
		// yok" der. Gecici dosya dizin ACL'ini (SYSTEM+Admins) miras alir.
		gecici := ayarYolu() + ".yeni"
		if err := os.WriteFile(gecici, y, 0o600); err != nil {
			opsHataYaz(yaz, w, http.StatusInternalServerError, fmt.Errorf("ayar yazilamadi: %w", err))
			return
		}
		if err := os.Rename(gecici, ayarYolu()); err != nil {
			_ = os.Remove(gecici)
			opsHataYaz(yaz, w, http.StatusInternalServerError, fmt.Errorf("ayar degistirilemedi: %w", err))
			return
		}
		log.Printf("yerel panel: admin parolasi degistirildi (%s)", r.RemoteAddr)
		yaz(w, http.StatusOK, map[string]bool{"ok": true})
	}
}
