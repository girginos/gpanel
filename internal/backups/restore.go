package backups

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

// Restore: POST /api/v1/domains/:id/backups/:bid/geriyukle
// tar -xzf .. + mysql import (eger dump.sql varsa).
// Tehlikeli: mevcut public_html ezilir, DB tablolari yeniden olusur.
func (h *Handlers) Restore(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	bid, _ := strconv.ParseInt(chi.URLParam(r, "bid"), 10, 64)

	var sk, dosya, alanAdi string
	var isDemo int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT d.sistem_kullanici, d.alan_adi, d.is_demo, b.dosya FROM backups b
		 JOIN domains d ON d.id=b.domain_id
		 WHERE b.id=? AND b.domain_id=?`, bid, id).Scan(&sk, &alanAdi, &isDemo, &dosya)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "yedek bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if isDemo == 1 {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğe geri yükleme yapılamaz")
		return
	}
	if !strings.HasPrefix(sk, "c_") {
		httpx.WriteError(w, http.StatusBadRequest, "güvenlik")
		return
	}

	abs := filepath.Join(BackupRoot, sk, dosya)
	if _, err := os.Stat(abs); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "yedek dosyası diskte bulunamadı")
		return
	}

	// Geçici extract dizini
	tmpDir, _ := os.MkdirTemp("", "gosp-restore-*")
	defer os.RemoveAll(tmpDir)

	if out, err := exec.Command("tar", "xzf", abs, "-C", tmpDir).CombinedOutput(); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError,
			"tar extract: "+strings.TrimSpace(string(out)))
		return
	}

	// Home replace (mevcut /home/c_<user> üstüne yaz)
	// Güvenli: yedeğin içinde c_<sk> klasörü var, onu kopyala
	extractedHome := filepath.Join(tmpDir, sk)
	if _, err := os.Stat(extractedHome); err == nil {
		out, err := exec.Command("rsync", "-a", "--delete", extractedHome+"/", "/home/"+sk+"/").CombinedOutput()
		if err != nil {
			// rsync yoksa cp -af
			_, _ = exec.Command("cp", "-af", extractedHome+"/.", "/home/"+sk+"/").CombinedOutput()
			_ = out
		}
		_, _ = exec.Command("chown", "-R", sk+":"+sk, "/home/"+sk).CombinedOutput()
		_, _ = exec.Command("restorecon", "-R", "/home/"+sk).CombinedOutput()
	}

	// DB dump varsa import et
	dumpPath := filepath.Join(tmpDir, "dump.sql")
	dbName := sk + "_main"
	var dbImport string
	if _, err := os.Stat(dumpPath); err == nil {
		cmd := fmt.Sprintf("mysql %s < %s 2>&1", dbName, dumpPath)
		out, err := exec.Command("bash", "-c", cmd).CombinedOutput()
		if err != nil {
			dbImport = "DB import uyarı: " + strings.TrimSpace(string(out))
		} else {
			dbImport = "DB import OK (" + dbName + ")"
		}
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"alan_adi":   alanAdi,
		"dosya":      dosya,
		"db_import":  dbImport,
		"uyari":      "Mevcut dosyalar üzerine yazıldı, DB tabloları yeniden oluşturuldu.",
	})
}
