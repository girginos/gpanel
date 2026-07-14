// Package phpext: sunucu bazinda PHP extension yoneticisi (3 surum)
package phpext

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"girginospanel/internal/httpx"
	"girginospanel/internal/phpsurum"

	"github.com/go-chi/chi/v5"
)

type Surum struct {
	Surum   string `json:"surum"`
	IniDir  string `json:"ini_dir"`
	Service string `json:"service"`
	PHPBin  string `json:"php_bin"`
	PECLBin string `json:"pecl_bin"`
}

// Surumler: dinamik discover — yalnız kurulu sürümleri döner
func Surumler() []Surum {
	out := []Surum{}
	gorulen := map[string]bool{}
	for _, ds := range phpsurum.TumSurumler() {
		if !ds.Yuklu || gorulen[ds.Surum] {
			continue
		}
		gorulen[ds.Surum] = true
		iniDir := "/etc/php.d"
		peclBin := "/usr/bin/pecl"
		if ds.Kaynak == "remi" {
			iniDir = "/etc/opt/remi/php" + ds.Kod + "/php.d"
			peclBin = "/opt/remi/php" + ds.Kod + "/root/usr/bin/pecl"
		}
		out = append(out, Surum{
			Surum:   ds.Surum,
			IniDir:  iniDir,
			Service: ds.Service,
			PHPBin:  ds.PHPBin,
			PECLBin: peclBin,
		})
	}
	return out
}

func surumByID(id string) (Surum, bool) {
	for _, s := range Surumler() {
		if s.Surum == id {
			return s, true
		}
	}
	return Surum{}, false
}

type Extension struct {
	Adi      string `json:"adi"`
	Aktif    bool   `json:"aktif"`
	IniDosya string `json:"ini_dosya"`
}

type Handlers struct {
	DB *sql.DB // su an kullanilmiyor ama gelecekte audit icin
}

// safe ad: sadece harf+rakam+underscore
func safeName(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// List: surum icin tum extension'lari listele (aktif + pasif)
func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	surum := r.URL.Query().Get("surum")
	if surum == "" {
		surum = "8.3"
	}
	s, ok := surumByID(surum)
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "desteklenmeyen surum")
		return
	}
	entries, err := os.ReadDir(s.IniDir)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "dizin okuma: "+err.Error())
		return
	}
	exts := []Extension{}
	for _, e := range entries {
		name := e.Name()
		if !strings.Contains(name, ".ini") {
			continue
		}
		aktif := strings.HasSuffix(name, ".ini")
		if !aktif && !strings.HasSuffix(name, ".ini.disabled") {
			continue
		}
		// XX-{name}.ini[.disabled] formatindan ad cikar
		clean := strings.TrimSuffix(name, ".disabled")
		clean = strings.TrimSuffix(clean, ".ini")
		// 20- prefix'i cikar
		if idx := strings.Index(clean, "-"); idx > 0 && idx < 4 {
			pre := clean[:idx]
			isNum := true
			for _, c := range pre {
				if c < '0' || c > '9' {
					isNum = false
					break
				}
			}
			if isNum {
				clean = clean[idx+1:]
			}
		}
		exts = append(exts, Extension{
			Adi:      clean,
			Aktif:    aktif,
			IniDosya: name,
		})
	}
	sort.Slice(exts, func(i, j int) bool { return exts[i].Adi < exts[j].Adi })

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"surum":   surum,
		"toplam":  len(exts),
		"icerik":  exts,
		"surumler": Surumler(),
	})
}

// Toggle: ini dosyasini rename + FPM reload
type toggleReq struct {
	Surum    string `json:"surum"`
	IniDosya string `json:"ini_dosya"` // tam dosya adi: "20-soap.ini" veya "20-soap.ini.disabled"
	Aktif    bool   `json:"aktif"`
}

func (h *Handlers) Toggle(w http.ResponseWriter, r *http.Request) {
	var req toggleReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz govde")
		return
	}
	s, ok := surumByID(req.Surum)
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "desteklenmeyen surum")
		return
	}
	// guvenlik: ini_dosya sadece ad olmali, path olamaz
	if strings.ContainsAny(req.IniDosya, "/\\") || !strings.Contains(req.IniDosya, ".ini") {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz dosya adi")
		return
	}

	mevcut := filepath.Join(s.IniDir, req.IniDosya)
	if _, err := os.Stat(mevcut); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "dosya bulunamadi")
		return
	}

	// Yeni ad
	var yeni string
	if req.Aktif {
		// disabled -> enabled
		yeni = strings.TrimSuffix(mevcut, ".disabled")
		if mevcut == yeni {
			httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "msg": "zaten aktif"})
			return
		}
	} else {
		// enabled -> disabled
		if strings.HasSuffix(mevcut, ".disabled") {
			httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "msg": "zaten pasif"})
			return
		}
		yeni = mevcut + ".disabled"
	}

	if err := os.Rename(mevcut, yeni); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "rename: "+err.Error())
		return
	}

	// FPM reload
	if out, err := exec.Command("systemctl", "reload-or-restart", s.Service).CombinedOutput(); err != nil {
		// Hata olursa eski adi geri yukle
		_ = os.Rename(yeni, mevcut)
		httpx.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("FPM reload: %s: %v", strings.TrimSpace(string(out)), err))
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"surum":  req.Surum,
		"dosya":  filepath.Base(yeni),
		"aktif":  req.Aktif,
	})
}

// PECL install — bonus
type peclReq struct {
	Surum string `json:"surum"`
	Paket string `json:"paket"`
}

func (h *Handlers) PECLKur(w http.ResponseWriter, r *http.Request) {
	var req peclReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz govde")
		return
	}
	if !safeName(req.Paket) {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz paket adi")
		return
	}
	s, ok := surumByID(req.Surum)
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "desteklenmeyen surum")
		return
	}

	// 1) DNF prebuild paket dene — Remi'de paket adı varyantlı (im7, 6, 5 suffixli)
	prefix := "php"
	if strings.HasPrefix(s.Service, "php") && strings.Contains(s.Service, "-php-fpm") && s.Service != "php-fpm" {
		prefix = strings.Split(s.Service, "-")[0] // "php82"
	}

	// Olası paket adı varyantları
	adaylar := []string{
		prefix + "-php-pecl-" + req.Paket,         // 1) base ad
		prefix + "-php-pecl-" + req.Paket + "-im7", // 2) imagick için im7 suffix
		prefix + "-php-pecl-" + req.Paket + "6",    // 3) redis6 / mongodb1 vb. (versiyon suffix)
		prefix + "-php-pecl-" + req.Paket + "5",    // 4) redis5 (eski sürüm)
		prefix + "-php-pecl-" + req.Paket + "3",    // 5) xdebug3
	}
	if prefix == "php" {
		// AppStream — ek varyant denemeleri
		adaylar = []string{
			"php-pecl-" + req.Paket,
			"php-pecl-" + req.Paket + "6",
			"php-pecl-" + req.Paket + "5",
			"php-pecl-" + req.Paket + "3",
		}
	}

	dnfPkg := ""
	for _, ad := range adaylar {
		if exec.Command("dnf", "info", "--quiet", ad).Run() == nil {
			dnfPkg = ad
			break
		}
	}

	if dnfPkg != "" {
		// Prebuild paket var, dnf ile kur
		cmd := exec.Command("dnf", "install", "-y", dnfPkg)
		out, err := cmd.CombinedOutput()
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError,
				"dnf install fail: "+strings.TrimSpace(string(out)))
			return
		}
		// FPM reload
		_, _ = exec.Command("systemctl", "reload-or-restart", s.Service).CombinedOutput()
		httpx.WriteJSON(w, http.StatusCreated, map[string]any{
			"ok":     true,
			"paket":  req.Paket,
			"surum":  req.Surum,
			"yontem": "dnf",
			"dnf_pkg": dnfPkg,
			"output": string(out),
		})
		return
	}

	// 2) Fallback: PECL build (libc-client gerek olabilir, dev paketleri lazim)
	if _, err := os.Stat(s.PECLBin); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError,
			"prebuild dnf paketi yok ("+dnfPkg+") ve PECL kurulu degil. Manuel kurulum gerekli: dnf install "+strings.Split(s.Service, "-")[0]+"-php-pear")
		return
	}
	cmd := exec.Command(s.PECLBin, "install", "-f", req.Paket)
	cmd.Env = append(os.Environ(), "PHP_PEAR_PHP_BIN="+s.PHPBin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError,
			"pecl install fail: "+strings.TrimSpace(string(out)))
		return
	}

	// ini dosyasi olustur
	iniPath := filepath.Join(s.IniDir, "50-"+req.Paket+".ini")
	if _, err := os.Stat(iniPath); err != nil {
		_ = os.WriteFile(iniPath, []byte("extension="+req.Paket+".so\n"), 0644)
	}
	_, _ = exec.Command("systemctl", "reload-or-restart", s.Service).CombinedOutput()

	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"ok":     true,
		"paket":  req.Paket,
		"surum":  req.Surum,
		"yontem": "pecl",
		"output": string(out),
	})
}

func (h *Handlers) PECLSil(w http.ResponseWriter, r *http.Request) {
	var req peclReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz govde")
		return
	}
	if !safeName(req.Paket) {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz paket adi")
		return
	}
	s, ok := surumByID(req.Surum)
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "desteklenmeyen surum")
		return
	}
	out, err := exec.Command(s.PECLBin, "uninstall", req.Paket).CombinedOutput()
	if err != nil {
		// pecl uninstall bazen ini dosyayi birakir; biz silelim
		_ = chi.URLParam // keep import
	}

	// ini'yi sil
	for _, suffix := range []string{".ini", ".ini.disabled"} {
		for _, prefix := range []string{"50-", "40-", "30-", "20-"} {
			path := filepath.Join(s.IniDir, prefix+req.Paket+suffix)
			_ = os.Remove(path)
		}
	}

	_, _ = exec.Command("systemctl", "reload-or-restart", s.Service).CombinedOutput()

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"paket":  req.Paket,
		"surum":  req.Surum,
		"output": string(out),
	})
}
