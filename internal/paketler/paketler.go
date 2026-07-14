// Package paketler: sunucu paket yoneticisi (DNF wrapper)
package paketler

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"girginospanel/internal/httpx"
)

// Kritik paketler — kaldirilirsa sistem coker
var KORUNAN = map[string]bool{
	"bash": true, "glibc": true, "kernel": true, "systemd": true,
	"openssh": true, "openssh-server": true, "openssh-clients": true,
	"sudo": true, "dnf": true, "rpm": true, "filesystem": true,
	"setup": true, "selinux-policy": true, "selinux-policy-targeted": true,
	"libselinux": true, "policycoreutils": true,
	// Panel'in calismasi icin gerekli
	"nginx": true, "mariadb": true, "mariadb-server": true, "mariadb-common": true,
	"bind": true, "bind-utils": true,
	"pure-ftpd": true, "pure-ftpd-mysql": true,
	"php": true, "php-fpm": true, "php-cli": true, "php-common": true,
}

type Handlers struct {
	DB *sql.DB
}

// Paket icin guvenli ad
func safe(s string) bool {
	if s == "" || len(s) > 80 {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == '+') {
			return false
		}
	}
	return true
}

type Paket struct {
	Adi      string `json:"adi"`
	Surum    string `json:"surum,omitempty"`
	Repo     string `json:"repo,omitempty"`
	Aciklama string `json:"aciklama,omitempty"`
	Kurulu   bool   `json:"kurulu"`
	Korunan  bool   `json:"korunan"`
}

// Ara: dnf search ile arama (max 200 sonuc)
func (h *Handlers) Ara(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		httpx.WriteError(w, http.StatusBadRequest, "q parametresi gerekli")
		return
	}
	if !safe(q) {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz arama")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	// dnf search "<q>" — Name & Summary Matched bolumlerini parse
	out, _ := exec.CommandContext(ctx, "dnf", "search", "--quiet", q).CombinedOutput()
	lines := strings.Split(string(out), "\n")
	paketler := []Paket{}
	kuruluSet := installedSet()
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "===") || strings.HasPrefix(ln, "Last metadata") {
			continue
		}
		// format: "paket-adi.x86_64 : aciklama"
		if !strings.Contains(ln, " : ") {
			continue
		}
		parts := strings.SplitN(ln, " : ", 2)
		nameArch := strings.TrimSpace(parts[0])
		desc := strings.TrimSpace(parts[1])
		// arch suffix'i temizle
		name := nameArch
		if i := strings.LastIndex(name, "."); i > 0 {
			suf := name[i+1:]
			if suf == "x86_64" || suf == "noarch" || suf == "i686" || suf == "src" {
				name = name[:i]
			}
		}
		paketler = append(paketler, Paket{
			Adi:      name,
			Aciklama: desc,
			Kurulu:   kuruluSet[name],
			Korunan:  KORUNAN[name],
		})
		if len(paketler) >= 200 {
			break
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"q":      q,
		"toplam": len(paketler),
		"icerik": paketler,
	})
}

// installedSet: tüm kurulu paket adlarini set olarak don
func installedSet() map[string]bool {
	out, _ := exec.Command("rpm", "-qa", "--qf", "%{NAME}\n").CombinedOutput()
	set := make(map[string]bool, 600)
	for _, ln := range strings.Split(string(out), "\n") {
		ln = strings.TrimSpace(ln)
		if ln != "" {
			set[ln] = true
		}
	}
	return set
}

// Kurulu: tüm kurulu paketleri opsiyonel filtre ile listele
func (h *Handlers) Kurulu(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	out, _ := exec.Command("rpm", "-qa", "--qf", "%{NAME}|%{VERSION}|%{SUMMARY}\n").CombinedOutput()
	paketler := []Paket{}
	for _, ln := range strings.Split(string(out), "\n") {
		parts := strings.SplitN(ln, "|", 3)
		if len(parts) < 3 {
			continue
		}
		name := parts[0]
		if q != "" && !strings.Contains(strings.ToLower(name), q) && !strings.Contains(strings.ToLower(parts[2]), q) {
			continue
		}
		paketler = append(paketler, Paket{
			Adi:      name,
			Surum:    parts[1],
			Aciklama: parts[2],
			Kurulu:   true,
			Korunan:  KORUNAN[name],
		})
		if len(paketler) >= 500 {
			break
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"toplam": len(paketler),
		"icerik": paketler,
	})
}

type opReq struct {
	Paket string `json:"paket"`
}

// Kur: dnf install
func (h *Handlers) Kur(w http.ResponseWriter, r *http.Request) {
	var req opReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz govde")
		return
	}
	if !safe(req.Paket) {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz paket adi")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "dnf", "install", "-y", req.Paket)
	out, err := cmd.CombinedOutput()
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError,
			"dnf install fail: "+strings.TrimSpace(string(out)))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"paket":  req.Paket,
		"output": string(out),
	})
}

// Kaldir: dnf remove (korumalı paketler reddedilir)
func (h *Handlers) Kaldir(w http.ResponseWriter, r *http.Request) {
	var req opReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz govde")
		return
	}
	if !safe(req.Paket) {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz paket adi")
		return
	}
	if KORUNAN[req.Paket] {
		httpx.WriteError(w, http.StatusForbidden,
			"bu paket sistem icin kritik, kaldirilamaz: "+req.Paket)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "dnf", "remove", "-y", req.Paket)
	out, err := cmd.CombinedOutput()
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError,
			"dnf remove fail: "+strings.TrimSpace(string(out)))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"paket":  req.Paket,
		"output": string(out),
	})
}

// Guncelle: dnf upgrade
func (h *Handlers) Guncelle(w http.ResponseWriter, r *http.Request) {
	var req opReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz govde")
		return
	}
	if req.Paket != "" && !safe(req.Paket) {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz paket")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
	defer cancel()
	args := []string{"upgrade", "-y"}
	if req.Paket != "" {
		args = append(args, req.Paket)
	}
	cmd := exec.CommandContext(ctx, "dnf", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError,
			"dnf upgrade fail: "+strings.TrimSpace(string(out)))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"paket":  req.Paket,
		"output": string(out),
	})
}

// Bilgi: dnf info <paket>
func (h *Handlers) Bilgi(w http.ResponseWriter, r *http.Request) {
	ad := r.URL.Query().Get("ad")
	if !safe(ad) {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz ad")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, "dnf", "info", ad).CombinedOutput()
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ad":     ad,
		"output": string(out),
	})
}

// Durum: virgüllü paket adlari listesi için kurulu durumunu döner
func (h *Handlers) Durum(w http.ResponseWriter, r *http.Request) {
	adlarStr := r.URL.Query().Get("adlar")
	if adlarStr == "" {
		httpx.WriteJSON(w, http.StatusOK, map[string]bool{})
		return
	}
	set := installedSet()
	res := make(map[string]bool)
	for _, ad := range strings.Split(adlarStr, ",") {
		ad = strings.TrimSpace(ad)
		if ad == "" || !safe(ad) {
			continue
		}
		res[ad] = set[ad]
	}
	httpx.WriteJSON(w, http.StatusOK, res)
}

