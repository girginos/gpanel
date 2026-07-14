// IonCube Loader kurma/kaldırma — commercial, ioncube.com'dan indirilir
package phpext

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"girginospanel/internal/httpx"
)

const IonCubeURL = "https://downloads.ioncube.com/loader_downloads/ioncube_loaders_lin_x86-64.tar.gz"

type ioncubeReq struct {
	Surum string `json:"surum"`
}

// IonCubeKur: belirtilen sürüm için IonCube loader kurar (zend_extension)
func (h *Handlers) IonCubeKur(w http.ResponseWriter, r *http.Request) {
	var req ioncubeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	s, ok := surumByID(req.Surum)
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "desteklenmeyen sürüm")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()

	// 1) PHP extension_dir öğren
	extOut, err := exec.CommandContext(ctx, s.PHPBin, "-r", "echo ini_get('extension_dir');").Output()
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "extension_dir alınamadı: "+err.Error())
		return
	}
	extDir := strings.TrimSpace(string(extOut))
	if extDir == "" {
		httpx.WriteError(w, http.StatusInternalServerError, "extension_dir boş")
		return
	}

	// 2) Tmp dir + indir
	tmpDir, err := os.MkdirTemp("", "ioncube-*")
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "tmp dir: "+err.Error())
		return
	}
	defer os.RemoveAll(tmpDir)
	tarPath := filepath.Join(tmpDir, "ioncube.tar.gz")
	if err := indir(ctx, IonCubeURL, tarPath); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "indirme: "+err.Error())
		return
	}

	// 3) Extract
	if out, err := exec.CommandContext(ctx, "tar", "xzf", tarPath, "-C", tmpDir).CombinedOutput(); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "tar: "+strings.TrimSpace(string(out)))
		return
	}

	// 4) PHP sürüm-uygun .so seç
	soSrc := filepath.Join(tmpDir, "ioncube", "ioncube_loader_lin_"+req.Surum+".so")
	if _, err := os.Stat(soSrc); err != nil {
		// Bazı sürümler için ts (thread-safe) gerekebilir — düz versiyon yoksa diğeri yok demek
		httpx.WriteError(w, http.StatusBadRequest,
			"IonCube PHP "+req.Surum+" için yayınlanmamış (tipik olarak 5.6→8.3 arası destekler)")
		return
	}

	// 5) extension_dir'e kopyala
	soDst := filepath.Join(extDir, "ioncube_loader_lin_"+req.Surum+".so")
	if err := dosyaKopyala(soSrc, soDst); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "kopyala: "+err.Error())
		return
	}
	_ = os.Chmod(soDst, 0644)

	// 6) .ini — OPcache'den ÖNCE yüklenmeli (00- prefix)
	iniPath := filepath.Join(s.IniDir, "00-ioncube.ini")
	iniContent := "; IonCube Loader — OPcache'den ÖNCE yüklenmeli\nzend_extension = " + soDst + "\n"
	if err := os.WriteFile(iniPath, []byte(iniContent), 0644); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "ini yaz: "+err.Error())
		return
	}

	// 7) FPM reload
	if out, err := exec.CommandContext(ctx, "systemctl", "reload-or-restart", s.Service).CombinedOutput(); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError,
			"fpm reload: "+strings.TrimSpace(string(out)))
		return
	}

	// 8) Doğrulama — php -m'de ionCube görünüyor mu
	verifyCtx, vc := context.WithTimeout(r.Context(), 5*time.Second)
	defer vc()
	mOut, _ := exec.CommandContext(verifyCtx, s.PHPBin, "-m").Output()
	loaded := strings.Contains(strings.ToLower(string(mOut)), "ioncube")

	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"ok":         true,
		"surum":      req.Surum,
		"so_dosyasi": soDst,
		"ini":        iniPath,
		"yuklendi":   loaded,
	})
}

// IonCubeKaldir: .ini + .so siler, FPM reload
func (h *Handlers) IonCubeKaldir(w http.ResponseWriter, r *http.Request) {
	var req ioncubeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	s, ok := surumByID(req.Surum)
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "desteklenmeyen sürüm")
		return
	}
	iniPath := filepath.Join(s.IniDir, "00-ioncube.ini")
	_ = os.Remove(iniPath)
	extOut, _ := exec.Command(s.PHPBin, "-r", "echo ini_get('extension_dir');").Output()
	extDir := strings.TrimSpace(string(extOut))
	if extDir != "" {
		_ = os.Remove(filepath.Join(extDir, "ioncube_loader_lin_"+req.Surum+".so"))
	}
	_, _ = exec.Command("systemctl", "reload-or-restart", s.Service).CombinedOutput()
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "surum": req.Surum})
}

func indir(ctx context.Context, url, hedef string) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(hedef)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

func dosyaKopyala(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
