// Package phpsurum: dinamik PHP surum kesfi + kur/kaldir
package phpsurum

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"girginospanel/internal/httpx"
)

// Tum desteklenen Remi PHP surumleri + AppStream PHP 8.3
var DesteklenenSurumler = []SurumMeta{
	{"5.6", "56", "remi"},
	{"7.0", "70", "remi"},
	{"7.1", "71", "remi"},
	{"7.2", "72", "remi"},
	{"7.3", "73", "remi"},
	{"7.4", "74", "remi"},
	{"8.0", "80", "remi"},
	{"8.1", "81", "remi"},
	{"8.2", "82", "remi"},
	{"8.3", "", "appstream"}, // AppStream native
	{"8.3", "83", "remi"},
	{"8.4", "84", "remi"},
	{"8.5", "85", "remi"},
}

type SurumMeta struct {
	Surum  string `json:"surum"`
	Kod    string `json:"kod"`    // "74", "82" — Remi paket prefix
	Kaynak string `json:"kaynak"` // "remi" | "appstream"
}

type Surum struct {
	SurumMeta
	Yuklu       bool   `json:"yuklu"`
	PoolDir     string `json:"pool_dir,omitempty"`
	SockDir     string `json:"sock_dir,omitempty"`
	Service     string `json:"service,omitempty"`
	PHPBin      string `json:"php_bin,omitempty"`
	GercekSurum string `json:"gercek_surum,omitempty"` // örn "8.3.31"
	ModulSayi   int    `json:"modul_sayi,omitempty"`
	Aciklama    string `json:"aciklama,omitempty"`
}

// Yollar(meta): yuklenmis olsa olsa nerede olur
func yollar(m SurumMeta) (poolDir, sockDir, service, phpBin string) {
	if m.Kaynak == "appstream" {
		return "/etc/php-fpm.d", "/run/php-fpm", "php-fpm", "/usr/bin/php"
	}
	pre := "/opt/remi/php" + m.Kod + "/root"
	return "/etc/opt/remi/php" + m.Kod + "/php-fpm.d",
		"/var/opt/remi/php" + m.Kod + "/run/php-fpm",
		"php" + m.Kod + "-php-fpm",
		pre + "/usr/bin/php"
}

// Discover: tek bir sürümün dolu metadata'sini doldur
func Discover(m SurumMeta) Surum {
	s := Surum{SurumMeta: m}
	s.PoolDir, s.SockDir, s.Service, s.PHPBin = yollar(m)
	// PHP binary varsa yüklü kabul
	if _, err := os.Stat(s.PHPBin); err == nil {
		s.Yuklu = true
		// Modül sayısı + gerçek sürüm
		if out, err := exec.Command(s.PHPBin, "-v").Output(); err == nil {
			line := strings.SplitN(string(out), "\n", 2)[0]
			// "PHP 8.3.31 (cli) ..."
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				s.GercekSurum = parts[1]
			}
		}
		if out, err := exec.Command(s.PHPBin, "-m").Output(); err == nil {
			lines := strings.Split(string(out), "\n")
			n := 0
			for _, ln := range lines {
				ln = strings.TrimSpace(ln)
				if ln != "" && !strings.HasPrefix(ln, "[") {
					n++
				}
			}
			s.ModulSayi = n
		}
	}
	if m.Kaynak == "appstream" {
		s.Aciklama = "Sistem default (AlmaLinux AppStream)"
	} else {
		s.Aciklama = "Remi modular — geliştirme/test/legacy"
	}
	return s
}

// TumSurumler: desteklenen tüm sürümleri tara
func TumSurumler() []Surum {
	out := make([]Surum, 0, len(DesteklenenSurumler))
	for _, m := range DesteklenenSurumler {
		out = append(out, Discover(m))
	}
	// Yüklüleri öne, sonra sürüm sıralı (büyükten küçüğe)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Yuklu != out[j].Yuklu {
			return out[i].Yuklu
		}
		return surumKarsi(out[i].Surum, out[j].Surum) > 0
	})
	return out
}

func surumKarsi(a, b string) int {
	pa := strings.Split(a, ".")
	pb := strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		ia, ib := 0, 0
		fmt.Sscanf(pa[i], "%d", &ia)
		fmt.Sscanf(pb[i], "%d", &ib)
		if ia != ib {
			return ia - ib
		}
	}
	return 0
}

// Default extension bundle (modern PHP icin)
var DefaultBundle = []string{
	"php-fpm",
	"php-cli",
	"php-mysqlnd",
	"php-mbstring",
	"php-bcmath",
	"php-intl",
	"php-gd",
	"php-soap",
	"php-opcache",
	"php-pdo",
	"php-xml",
	"php-zip",
	"php-pgsql",
	"php-ldap",
}

// PaketAdlari: bir sürüm için tüm paket isimlerini hazırla
func PaketAdlari(m SurumMeta) []string {
	pre := "php"
	if m.Kaynak == "remi" {
		pre = "php" + m.Kod + "-php"
	}
	out := make([]string, 0, len(DefaultBundle))
	for _, p := range DefaultBundle {
		out = append(out, strings.Replace(p, "php", pre, 1))
	}
	return out
}

// ----- HTTP -----

type Handlers struct {
	DB *sql.DB
}

func (h *Handlers) Liste(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"surumler": TumSurumler(),
	})
}

type opReq struct {
	Surum  string `json:"surum"`
	Kaynak string `json:"kaynak"`
}

func (h *Handlers) Kur(w http.ResponseWriter, r *http.Request) {
	var req opReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz govde")
		return
	}
	var m SurumMeta
	for _, d := range DesteklenenSurumler {
		if d.Surum == req.Surum && d.Kaynak == req.Kaynak {
			m = d
			break
		}
	}
	if m.Surum == "" {
		httpx.WriteError(w, http.StatusBadRequest, "desteklenmeyen surum")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
	defer cancel()
	args := append([]string{"install", "-y"}, PaketAdlari(m)...)
	cmd := exec.CommandContext(ctx, "dnf", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError,
			"dnf install: "+strings.TrimSpace(string(out)))
		return
	}

	// Pool dizini yoksa olustur + default www.conf
	pd, _, svc, _ := yollar(m)
	_ = os.MkdirAll(pd, 0755)
	// Remi'de www.conf.disabled varsa aktive et
	if m.Kaynak == "remi" {
		dis := filepath.Join(pd, "www.conf.disabled")
		main := filepath.Join(pd, "www.conf")
		if _, err := os.Stat(dis); err == nil {
			_, _ = os.Stat(main) // varsa atla
			if _, err := os.Stat(main); err != nil {
				_ = os.Rename(dis, main)
			}
		}
	}
	// GirginOSPanel default: buyuk form/import (phpMyAdmin, WordPress) icin max_input_vars
	phpdDir := "/etc/php.d"
	if m.Kaynak == "remi" {
		phpdDir = "/etc/opt/remi/php" + m.Kod + "/php.d"
	}
	if err := os.MkdirAll(phpdDir, 0755); err == nil {
		_ = os.WriteFile(filepath.Join(phpdDir, "99-girginospanel-input.ini"),
			[]byte("; GirginOSPanel: buyuk form/import (phpMyAdmin, WordPress) - takilma onler\nmax_input_vars = 10000\n"), 0644)
	}

	// FPM servis enable + start
	_, _ = exec.Command("systemctl", "enable", "--now", svc).CombinedOutput()

	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"ok":     true,
		"surum":  req.Surum,
		"kaynak": req.Kaynak,
		"output": string(out),
	})
}

func (h *Handlers) Kaldir(w http.ResponseWriter, r *http.Request) {
	var req opReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz govde")
		return
	}
	if req.Kaynak == "appstream" {
		httpx.WriteError(w, http.StatusForbidden,
			"AppStream PHP sistemin default'u, kaldirilamaz")
		return
	}
	var m SurumMeta
	for _, d := range DesteklenenSurumler {
		if d.Surum == req.Surum && d.Kaynak == req.Kaynak {
			m = d
			break
		}
	}
	if m.Surum == "" || m.Kaynak != "remi" {
		httpx.WriteError(w, http.StatusBadRequest, "desteklenmeyen surum")
		return
	}

	// Bu sürümü kullanan domain var mı?
	var count int
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM domains WHERE php_surum=?`, req.Surum).Scan(&count)
	if count > 0 {
		httpx.WriteError(w, http.StatusConflict,
			fmt.Sprintf("Bu surumu kullanan %d domain var, once baska bir surume gec.", count))
		return
	}

	// FPM durdur
	_, svc, _, _ := yollar(m)
	_, _ = exec.Command("systemctl", "disable", "--now", svc).CombinedOutput()
	_ = svc

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	args := append([]string{"remove", "-y"}, "php"+m.Kod+"-*")
	cmd := exec.CommandContext(ctx, "dnf", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError,
			"dnf remove: "+strings.TrimSpace(string(out)))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"output": string(out),
	})
}
