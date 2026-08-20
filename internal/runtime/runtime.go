// Package runtime: sunucu geneli ek dil runtime'ları (.NET Core / Node.js / Python)
// — curated katalog + kurulu durumu + async kur/kaldır (dnf). PHP dışı diller için
// bileşen yönetimi; sihirbazın "Ek Runtime'lar" adımı bunu kullanır.
package runtime

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/httpx"
)

// RuntimeEk — kurulabilir bir runtime bileşeni (bir dnf paketi).
type RuntimeEk struct {
	Ad       string `json:"ad"`
	Anahtar  string `json:"anahtar"` // dnf paket adı (kur/kaldır + kurulu tespiti)
	Aciklama string `json:"aciklama"`
	Kurulu   bool   `json:"kurulu"`
	// Yonetilemez: panel kur/kaldır SUNMAZ (salt-bilgi). Örn Node.js sistemde
	// nodesource RPM'i ile kurulu; dnf modülü EL10'da yok, dnf remove nodesource'u
	// kırardı → yönetimi kapalı, yalnız "kurulu" gösterilir.
	Yonetilemez bool `json:"yonetilemez,omitempty"`
}

// RuntimeGrup — bir dil ailesi (dotnet/nodejs/python) ve bileşenleri.
type RuntimeGrup struct {
	Tip   string      `json:"tip"`
	Ad    string      `json:"ad"`
	Ikon  string      `json:"ikon"`
	Ekler []RuntimeEk `json:"ekler"`
}

// katalog — curated runtime bileşenleri. Sürümler node 49'da dnf'te mevcut
// (aspnetcore/dotnet 8-10, python 3.12-3.14, nodejs). Kurulu durumu runtime'da
// rpm/komut ile doldurulur.
func katalog() []RuntimeGrup {
	return []RuntimeGrup{
		{Tip: "dotnet", Ad: ".NET Core", Ikon: "dotnet", Ekler: []RuntimeEk{
			{Ad: "ASP.NET Core Runtime 8.0", Anahtar: "aspnetcore-runtime-8.0", Aciklama: "ASP.NET Core web uygulamaları çalıştırır (LTS)"},
			{Ad: "ASP.NET Core Runtime 9.0", Anahtar: "aspnetcore-runtime-9.0", Aciklama: "ASP.NET Core web uygulamaları (STS)"},
			{Ad: "ASP.NET Core Runtime 10.0", Anahtar: "aspnetcore-runtime-10.0", Aciklama: "ASP.NET Core web uygulamaları (en yeni)"},
			{Ad: ".NET SDK 8.0", Anahtar: "dotnet-sdk-8.0", Aciklama: "Derleme + geliştirme (LTS) — dotnet CLI"},
			{Ad: ".NET SDK 9.0", Anahtar: "dotnet-sdk-9.0", Aciklama: "Derleme + geliştirme (STS)"},
			{Ad: ".NET SDK 10.0", Anahtar: "dotnet-sdk-10.0", Aciklama: "Derleme + geliştirme (en yeni)"},
		}},
		{Tip: "python", Ad: "Python", Ikon: "python", Ekler: []RuntimeEk{
			// python3.12 YOK: base python3 zaten 3.12 (sanal provide) — kur "nothing to do",
			// kaldır sistem python'unu hedefler. Yalnız GERÇEK ek sürümler listelenir.
			{Ad: "Python 3.13", Anahtar: "python3.13", Aciklama: "Python 3.13 yorumlayıcısı"},
			{Ad: "Python 3.14", Anahtar: "python3.14", Aciklama: "Python 3.14 yorumlayıcısı"},
			{Ad: "pip (paket yöneticisi)", Anahtar: "python3-pip", Aciklama: "Python paket yöneticisi (python3-pip)"},
		}},
		{Tip: "nodejs", Ad: "Node.js", Ikon: "nodejs", Ekler: []RuntimeEk{
			// Yönetilemez: sistemde nodesource RPM'i ile kurulu; dnf remove onu kırardı.
			{Ad: "Node.js (sistem)", Anahtar: "nodejs", Aciklama: "Sistemde kurulu — npm/npx global. Sürüm yönetimi panel dışıdır.", Yonetilemez: true},
		}},
	}
}

type Handlers struct{}

// safeAd — dnf paket adı allowlist (kur/kaldır argümanı; komut enjeksiyonu önler).
func safeAd(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') && c != '-' && c != '.' {
			return false
		}
	}
	return true
}

// paketKurulu — rpm -q ile paket kurulu mu (nodejs modülü için özel).
func paketKurulu(anahtar string) bool {
	if anahtar == "nodejs" {
		return exec.Command("sh", "-c", "command -v node >/dev/null 2>&1").Run() == nil
	}
	return exec.Command("rpm", "-q", anahtar).Run() == nil
}

// Liste: GET /runtimeler — katalog + her bileşenin kurulu durumu.
func (h *Handlers) Liste(w http.ResponseWriter, r *http.Request) {
	kat := katalog()
	for i := range kat {
		for j := range kat[i].Ekler {
			kat[i].Ekler[j].Kurulu = paketKurulu(kat[i].Ekler[j].Anahtar)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"gruplar": kat})
}

// ── Async kurulum işi (dotnet-sdk ~200MB, dnf yavaş → goroutine + ilerleme) ──
type is struct {
	mu    sync.Mutex
	Durum string // "calisiyor" | "tamam" | "hata"
	Adim  string
	Yuzde int
	Hata  string
	bitti time.Time
}

var (
	islerMu sync.Mutex
	isler   = map[string]*is{}
)

func isID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "rt" + time.Now().Format("150405.000000000")
	}
	return hex.EncodeToString(b)
}

func temizle() {
	islerMu.Lock()
	defer islerMu.Unlock()
	for id, j := range isler {
		j.mu.Lock()
		eski := j.Durum != "calisiyor" && !j.bitti.IsZero() && time.Since(j.bitti) > 10*time.Minute
		j.mu.Unlock()
		if eski {
			delete(isler, id)
		}
	}
}

type kurReq struct {
	Anahtar string `json:"anahtar"`
}

// Kur: POST /runtimeler/kur {anahtar} → async is_id.
func (h *Handlers) Kur(w http.ResponseWriter, r *http.Request) {
	h.calistir(w, r, "kur")
}

// Kaldir: POST /runtimeler/kaldir {anahtar} → async is_id.
func (h *Handlers) Kaldir(w http.ResponseWriter, r *http.Request) {
	h.calistir(w, r, "kaldir")
}

func (h *Handlers) calistir(w http.ResponseWriter, r *http.Request, islem string) {
	var req kurReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	if !safeAd(req.Anahtar) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz paket adı")
		return
	}
	// Anahtar katalogda olmalı (keyfi paket kur/kaldır engellenir).
	gecerli := false
	nodejs := false
	yonetilemez := false
	for _, g := range katalog() {
		for _, e := range g.Ekler {
			if e.Anahtar == req.Anahtar {
				gecerli = true
				nodejs = g.Tip == "nodejs"
				yonetilemez = e.Yonetilemez
			}
		}
	}
	if !gecerli {
		httpx.WriteError(w, http.StatusBadRequest, "bilinmeyen runtime bileşeni")
		return
	}
	if yonetilemez {
		httpx.WriteError(w, http.StatusBadRequest, "bu bileşen panelden kur/kaldır yapılamaz (sistem yönetimli)")
		return
	}

	temizle()
	id := isID()
	j := &is{Durum: "calisiyor", Adim: "Başlatılıyor…", Yuzde: 2}
	islerMu.Lock()
	isler[id] = j
	islerMu.Unlock()
	anahtar := req.Anahtar

	go func() {
		defer func() {
			if p := recover(); p != nil {
				j.mu.Lock()
				j.Durum = "hata"
				j.Hata = fmt.Sprintf("iç hata: %v", p)
				j.bitti = time.Now()
				j.mu.Unlock()
			}
		}()
		set := func(adim string, yuzde int) {
			j.mu.Lock()
			j.Adim = adim
			j.Yuzde = yuzde
			j.mu.Unlock()
		}
		basarisiz := func(msg string) {
			j.mu.Lock()
			j.Durum = "hata"
			j.Hata = msg
			j.bitti = time.Now()
			j.mu.Unlock()
		}

		var cmd *exec.Cmd
		if nodejs {
			// Node.js dnf modülü — install/remove.
			modAlt := "remove"
			durumMsg := "Node.js kaldırılıyor…"
			if islem == "kur" {
				modAlt = "install"
				durumMsg = "Node.js kuruluyor…"
			}
			set(durumMsg, 40)
			cmd = exec.Command("dnf", "-y", "module", modAlt, "nodejs")
		} else if islem == "kur" {
			set(anahtar+" kuruluyor (indiriliyor + kuruluyor)…", 40)
			cmd = exec.Command("dnf", "install", "-y", anahtar)
		} else {
			set(anahtar+" kaldırılıyor…", 40)
			cmd = exec.Command("dnf", "remove", "-y", anahtar)
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			basarisiz(islem + " başarısız: " + sonSatir(out))
			return
		}
		sonEk := " kaldırıldı"
		if islem == "kur" {
			sonEk = " kuruldu"
		}
		j.mu.Lock()
		j.Durum = "tamam"
		j.Adim = anahtar + sonEk
		j.Yuzde = 100
		j.bitti = time.Now()
		j.mu.Unlock()
	}()

	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"ok": true, "is_id": id, "anahtar": req.Anahtar})
}

// Durum: GET /runtimeler/durum?id=
func (h *Handlers) Durum(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	islerMu.Lock()
	j := isler[id]
	islerMu.Unlock()
	if j == nil {
		httpx.WriteError(w, http.StatusNotFound, "iş bulunamadı")
		return
	}
	j.mu.Lock()
	resp := map[string]any{"durum": j.Durum, "adim": j.Adim, "yuzde": j.Yuzde, "hata": j.Hata}
	j.mu.Unlock()
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func sonSatir(b []byte) string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(lines[i]); s != "" {
			return s
		}
	}
	return ""
}
