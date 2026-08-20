// Package phpext: sunucu bazinda PHP extension yoneticisi (3 surum)
package phpext

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/httpx"
	"girginospanel/internal/phpsurum"

	"github.com/go-chi/chi/v5"
)

// ── PECL kurulum işi (async + canlı ilerleme) ───────────────────────────────
// PECL derlemesi (php-devel + pear + gcc derleme) dakikalar sürebilir; senkron
// HTTP askıda kalır ve kullanıcı ne olduğunu göremezdi. İş goroutine'de yürür,
// UI /php-extensions/pecl-durum ile adım+yüzdeyi canlı izler.
type peclIs struct {
	mu     sync.Mutex
	Durum  string // "calisiyor" | "tamam" | "hata"
	Adim   string // insan-okunur adım ("Derleniyor…")
	Yuzde  int
	Yontem string // "dnf" | "pecl"
	Hata   string
	Log    string // son ~8KB birleşik çıktı
	bitti  time.Time
}

func (p *peclIs) set(adim string, yuzde int) {
	p.mu.Lock()
	p.Adim = adim
	p.Yuzde = yuzde
	p.mu.Unlock()
}
func (p *peclIs) logEkle(b []byte) {
	p.mu.Lock()
	p.Log += string(b)
	if len(p.Log) > 8192 {
		p.Log = p.Log[len(p.Log)-8192:]
	}
	p.mu.Unlock()
}
func (p *peclIs) basarisiz(msg string) {
	p.mu.Lock()
	p.Durum = "hata"
	p.Hata = msg
	p.bitti = time.Now()
	p.mu.Unlock()
}

var (
	peclIslerMu sync.Mutex
	peclIsler   = map[string]*peclIs{}
)

func peclIsID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "pecl" + time.Now().Format("150405.000000000")
	}
	return hex.EncodeToString(b)
}

func peclIsTemizle() {
	peclIslerMu.Lock()
	defer peclIslerMu.Unlock()
	for id, is := range peclIsler {
		is.mu.Lock()
		eski := is.Durum != "calisiyor" && !is.bitti.IsZero() && time.Since(is.bitti) > 10*time.Minute
		is.mu.Unlock()
		if eski {
			delete(peclIsler, id)
		}
	}
}

// PECLDurum: GET /php-extensions/pecl-durum?id=<is_id>
func (h *Handlers) PECLDurum(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	peclIslerMu.Lock()
	is := peclIsler[id]
	peclIslerMu.Unlock()
	if is == nil {
		httpx.WriteError(w, http.StatusNotFound, "iş bulunamadı (bitmiş olabilir)")
		return
	}
	is.mu.Lock()
	resp := map[string]any{
		"durum": is.Durum, "adim": is.Adim, "yuzde": is.Yuzde,
		"yontem": is.Yontem, "hata": is.Hata, "log": is.Log,
	}
	is.mu.Unlock()
	httpx.WriteJSON(w, http.StatusOK, resp)
}

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
		"surum":    surum,
		"toplam":   len(exts),
		"icerik":   exts,
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
		"ok":    true,
		"surum": req.Surum,
		"dosya": filepath.Base(yeni),
		"aktif": req.Aktif,
	})
}

// PECL install — bonus
type peclReq struct {
	Surum string `json:"surum"`
	Paket string `json:"paket"`
}

// peclPrefix — Remi sürümünde paket ön eki ("php82"); AppStream'de "php".
func peclPrefix(s Surum) string {
	if strings.HasPrefix(s.Service, "php") && strings.Contains(s.Service, "-php-fpm") && s.Service != "php-fpm" {
		return strings.Split(s.Service, "-")[0]
	}
	return "php"
}

// peclKomut — alt süreci panel sırları OLMADAN, temiz env ile hazırlar.
func peclKomut(s Surum, argv ...string) *exec.Cmd {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root",
		"PHP_PEAR_PHP_BIN=" + s.PHPBin,
	}
	return cmd
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

	// ── ASENKRON: PECL derlemesi (pear+devel kurulumu + gcc derleme) dakikalar
	// sürebilir. İş goroutine'de yürür; yanıt hemen is_id döner, UI ilerlemeyi izler.
	peclIsTemizle()
	isID := peclIsID()
	is := &peclIs{Durum: "calisiyor", Adim: "Başlatılıyor…", Yuzde: 2}
	peclIslerMu.Lock()
	peclIsler[isID] = is
	peclIslerMu.Unlock()

	prefix := peclPrefix(s)
	paket := req.Paket

	go func() {
		defer func() {
			if p := recover(); p != nil {
				is.basarisiz(fmt.Sprintf("iç hata: %v", p))
			}
		}()

		// 1) DNF prebuild paket dene — hızlı yol (derleme yok).
		is.set("Hazır paket aranıyor…", 10)
		adaylar := []string{
			prefix + "-php-pecl-" + paket,
			prefix + "-php-pecl-" + paket + "-im7",
			prefix + "-php-pecl-" + paket + "6",
			prefix + "-php-pecl-" + paket + "5",
			prefix + "-php-pecl-" + paket + "3",
		}
		if prefix == "php" {
			adaylar = []string{
				"php-pecl-" + paket, "php-pecl-" + paket + "6",
				"php-pecl-" + paket + "5", "php-pecl-" + paket + "3",
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
			is.mu.Lock()
			is.Yontem = "dnf"
			is.mu.Unlock()
			is.set("Hazır paket kuruluyor ("+dnfPkg+")…", 55)
			out, err := exec.Command("dnf", "install", "-y", dnfPkg).CombinedOutput()
			is.logEkle(out)
			if err != nil {
				is.basarisiz("dnf install başarısız: " + sonSatir(out))
				return
			}
			is.set("PHP-FPM yeniden başlatılıyor…", 90)
			ro, _ := exec.Command("systemctl", "reload-or-restart", s.Service).CombinedOutput()
			is.logEkle(ro)
			is.mu.Lock()
			is.Durum = "tamam"
			is.Adim = paket + " kuruldu (hazır paket)"
			is.Yuzde = 100
			is.bitti = time.Now()
			is.mu.Unlock()
			return
		}

		// 2) PECL build yolu. KALICI ÇÖZÜM: pecl/pear + derleme araçları yoksa
		// otomatik kur (eskiden "Manuel kurulum gerekli" diye hata veriyordu).
		is.mu.Lock()
		is.Yontem = "pecl"
		is.mu.Unlock()

		if _, err := os.Stat(s.PECLBin); err != nil {
			is.set("PECL/PEAR kuruluyor ("+prefix+"-php-pear)…", 25)
			out, err := exec.Command("dnf", "install", "-y", prefix+"-php-pear").CombinedOutput()
			is.logEkle(out)
			if err != nil {
				is.basarisiz("PEAR kurulamadı (" + prefix + "-php-pear): " + sonSatir(out))
				return
			}
			if _, err := os.Stat(s.PECLBin); err != nil {
				is.basarisiz("PEAR kuruldu ama pecl bulunamadı: " + s.PECLBin)
				return
			}
		}

		// Derleme araçları: pecl install kaynaktan derler → php-devel + gcc/make/autoconf şart.
		is.set("Derleme araçları hazırlanıyor…", 40)
		devel := prefix + "-php-devel"
		if prefix == "php" {
			devel = "php-devel"
		}
		bout, berr := exec.Command("dnf", "install", "-y", devel, "gcc", "make", "autoconf").CombinedOutput()
		is.logEkle(bout)
		if berr != nil {
			is.basarisiz("derleme araçları kurulamadı: " + sonSatir(bout))
			return
		}

		// pecl install — kaynaktan derleme (uzun sürebilir).
		is.set("Derleniyor: "+paket+" (pecl install)…", 60)
		out, err := peclKomut(s, s.PECLBin, "install", "-f", paket).CombinedOutput()
		is.logEkle(out)
		if err != nil {
			is.basarisiz("pecl install başarısız: " + sonSatir(out))
			return
		}

		// ini dosyası + FPM reload.
		is.set("Etkinleştiriliyor (ini + PHP-FPM)…", 88)
		iniPath := filepath.Join(s.IniDir, "50-"+paket+".ini")
		if _, err := os.Stat(iniPath); err != nil {
			_ = os.WriteFile(iniPath, []byte("extension="+paket+".so\n"), 0644)
		}
		ro, _ := exec.Command("systemctl", "reload-or-restart", s.Service).CombinedOutput()
		is.logEkle(ro)
		is.mu.Lock()
		is.Durum = "tamam"
		is.Adim = paket + " derlendi ve etkinleştirildi"
		is.Yuzde = 100
		is.bitti = time.Now()
		is.mu.Unlock()
	}()

	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{
		"ok":    true,
		"is_id": isID,
		"paket": req.Paket,
		"surum": req.Surum,
	})
}

// sonSatir — çıktının son anlamlı satırı (hata mesajı için).
func sonSatir(b []byte) string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(lines[i]); s != "" {
			return s
		}
	}
	return ""
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
