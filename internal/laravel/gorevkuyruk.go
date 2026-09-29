package laravel

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"girginospanel/internal/httpx"
)

// queueProps: systemctl show KEY=VALUE (çoklu -p → --value KULLANMA, sıra bozulur).
func queueProps(unit string) map[string]string {
	m := map[string]string{}
	out, err := exec.Command("systemctl", "show", "-p", "ActiveState", "-p", "SubState", "-p", "NRestarts", unit).Output()
	if err != nil {
		return m
	}
	for _, ln := range strings.Split(string(out), "\n") {
		if i := strings.IndexByte(ln, '='); i > 0 {
			m[ln[:i]] = strings.TrimSpace(ln[i+1:])
		}
	}
	return m
}

// monitorQueue: worker başlatıldıktan kısa süre sonra crash-loop var mı? (log, sağlıklı)
func monitorQueue(unit string) (string, bool) {
	time.Sleep(3 * time.Second)
	m := queueProps(unit)
	if m["ActiveState"] == "failed" || m["SubState"] == "failed" {
		out, _ := exec.Command("journalctl", "-u", unit, "-n", "20", "--no-pager").Output()
		tail := string(out)
		if len(tail) > 4096 {
			tail = tail[len(tail)-4096:]
		}
		return "worker başlar başlamaz çöktü (muhtemelen .env/DB hatası):\n" + tail, false
	}
	return "", true
}

// ensureLogDir: <evKoku>/<sk>/logs yoksa yaratır ve kiracıya verir (0750). Eskiden root
// yol ile os.Stat + os.MkdirAll + `chown sk:sk <yol>` (-h yok → symlink izler) yapıyordu;
// kiracının denetlediği bu yol yoklama ile chown arasında değiştirilebildiği için root'un
// sahiplik devri ev dışına yönlendirilebilirdi. Artık ev kökünden O_NOFOLLOW ile
// Mkdirat+Openat yapılır, sahiplik yalnız YENİ yaratılan dizinin fd'sine verilir
// (evAltiMkdirHepsi); var olan girdiye (dizin ya da symlink) dokunulmaz.
func ensureLogDir(sk string) error {
	home, err := evDizini(sk)
	if err != nil {
		return err
	}
	uid, gid, err := kimlikBul(sk)
	if err != nil || uid <= 0 || gid <= 0 {
		return errKimlikYok
	}
	return evAltiMkdirHepsi(home, logAltDizin, uid, gid, 0o750)
}

// ---- Zamanlanmış görev (schedule:run — dakikada bir, /etc/cron.d) ----

// cronDizini: cron.d dizini. Yalnız testler değiştirir (t.TempDir); üretimde /etc/cron.d.
var cronDizini = cronDDizin

func cronDPath(id int64) string { return cronDizini + "/girginos-laravel-" + fmt.Sprint(id) }

// cronYaz / cronSil: Schedule'ın root olarak dokunduğu TEK dosya cron.d/girginos-laravel-<id>
// (root'a ait sistem dizini; yol yalnız domain id'sinden türetilir, kiracı denetleyemez).
// Kaynak koruma testi: buradaki her os çağrısının yolu cronDPath(id) olmalı.
func cronYaz(id int64, govde string) error {
	return os.WriteFile(cronDPath(id), []byte(govde), 0o644)
}

func cronSil(id int64) error {
	if err := os.Remove(cronDPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// POST /domains/{id}/laravel/schedule  {"aktif":true}
func (h *Handlers) Schedule(w http.ResponseWriter, r *http.Request) {
	id, sk, phpSurum, demo, ok := h.lookup(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde zamanlanmış görev yönetilemez")
		return
	}
	var req struct {
		Aktif bool `json:"aktif"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	appDir, err := h.appDizin(r, id, sk)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	// 🔴 Kiracının denetlediği yollarda root dosya işlemi YALNIZ ensureLogDir üzerinden
	// (ev köküne çapalı, symlink izlemeden). Bu gövdede ve buradan erişilen fonksiyonlarda
	// yol tabanlı os/exec (MkdirAll, chown, os.Chown ...) kaynak koruma testiyle yasak;
	// davranış handler testinde (geçici DB + sahte ev) kilitli.
	if req.Aktif {
		home, err := evDizini(sk)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := ensureLogDir(sk); err != nil {
			// cron satırı yine yazılır: >> yönlendirmesini cron KİRACI olarak yapar.
			log.Printf("laravel schedule: log dizini hazırlanamadı (domain=%d): %v", id, err)
		}
		logF := home + "/" + logAltDizin + "/laravel-schedule.log"
		// cron.d formatı: dk sa gün ay haftagünü KULLANICI komut
		line := fmt.Sprintf("* * * * * %s %s %s/artisan schedule:run >> %s 2>&1\n",
			sk, phpBin(phpSurum), appDir, logF)
		body := "# girginos Laravel Toolkit — zamanlanmış görev (domain " + fmt.Sprint(id) + ")\n" +
			"PATH=" + sistemPATH + "\n" + line
		if err := cronYaz(id, body); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "cron yazılamadı: "+err.Error())
			return
		}
		if _, err := h.DB.ExecContext(r.Context(), `UPDATE cp_laravel_apps SET schedule_enabled=1 WHERE domain_id=?`, id); err != nil {
			log.Printf("laravel schedule: schedule_enabled=1 yazılamadı (domain=%d): %v — cron kuruldu ama panel durumu güncellenmedi", id, err)
		}
	} else {
		_ = cronSil(id)
		if _, err := h.DB.ExecContext(r.Context(), `UPDATE cp_laravel_apps SET schedule_enabled=0 WHERE domain_id=?`, id); err != nil {
			log.Printf("laravel schedule: schedule_enabled=0 yazılamadı (domain=%d): %v — cron kaldırıldı ama panel durumu güncellenmedi", id, err)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "schedule_enabled": req.Aktif})
}

// ---- Kuyruk worker (queue:work — per-tenant systemd unit) ----

var reConn = regexp.MustCompile(`^[a-z0-9_]+$`)

func queueUnitName(id int64) string { return "girginos-laravel-queue-" + fmt.Sprint(id) }
func queueUnitPath(id int64) string { return "/etc/systemd/system/" + queueUnitName(id) + ".service" }

// queueUnit: sertleştirilmiş worker unit'i (tenantfpm reçetesi; YASAK direktifler yok).
func queueUnit(id int64, sk, appDir, php, conn string, timeout, maxJobs int) string {
	return "[Unit]\n" +
		"Description=Laravel kuyruk işleyici (domain " + fmt.Sprint(id) + ")\n" +
		"After=network.target mariadb.service\n\n" +
		"[Service]\n" +
		"Type=simple\n" +
		"User=" + sk + "\nGroup=" + sk + "\n" +
		"WorkingDirectory=" + appDir + "\n" +
		"Environment=HOME=/home/" + sk + "\n" +
		fmt.Sprintf("ExecStart=%s %s/artisan queue:work %s --sleep=3 --tries=3 --timeout=%d --max-jobs=%d --max-time=3600\n",
			php, appDir, conn, timeout, maxJobs) +
		"Restart=always\nRestartSec=5\n" +
		"StartLimitIntervalSec=300\nStartLimitBurst=10\n" +
		"Slice=girginos-" + sk + ".slice\n" +
		"NoNewPrivileges=yes\n" +
		"ProtectSystem=strict\n" +
		"ReadWritePaths=/home/" + sk + "\n" +
		"PrivateTmp=yes\n" +
		"ProtectControlGroups=yes\n" +
		"ProtectKernelTunables=yes\n" +
		"RestrictSUIDSGID=yes\n\n" +
		"[Install]\nWantedBy=multi-user.target\n"
}

// POST /domains/{id}/laravel/queue  {"aktif":true,"timeout":60,"max_jobs":1000,"connection":"database"}
func (h *Handlers) Queue(w http.ResponseWriter, r *http.Request) {
	id, sk, phpSurum, demo, ok := h.lookup(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde kuyruk işleyici yönetilemez")
		return
	}
	var req struct {
		Aktif      bool   `json:"aktif"`
		Timeout    int    `json:"timeout"`
		MaxJobs    int    `json:"max_jobs"`
		Connection string `json:"connection"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	defer kilitle(id)() // per-domain serileştir (global daemon-reload amplifikasyonu — denetim #7)
	unit := queueUnitName(id) + ".service"
	if !req.Aktif {
		_ = exec.Command("systemctl", "disable", "--now", unit).Run()
		_ = os.Remove(queueUnitPath(id))
		_ = exec.Command("systemctl", "daemon-reload").Run()
		_ = exec.Command("systemctl", "reset-failed", unit).Run()
		_, _ = h.DB.ExecContext(r.Context(), `UPDATE cp_laravel_apps SET queue_enabled=0 WHERE domain_id=?`, id)
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "queue_enabled": false})
		return
	}
	appDir, err := h.appDizin(r, id, sk)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Timeout < 5 || req.Timeout > 600 {
		req.Timeout = 60
	}
	if req.MaxJobs < 10 || req.MaxJobs > 100000 {
		req.MaxJobs = 1000
	}
	conn := strings.TrimSpace(req.Connection)
	if conn == "" {
		conn = "database"
	}
	if !reConn.MatchString(conn) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz bağlantı adı")
		return
	}
	if err := os.WriteFile(queueUnitPath(id), []byte(queueUnit(id, sk, appDir, phpBin(phpSurum), conn, req.Timeout, req.MaxJobs)), 0644); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "unit yazılamadı: "+err.Error())
		return
	}
	_ = exec.Command("systemctl", "daemon-reload").Run()
	if out, err := exec.Command("systemctl", "enable", "--now", unit).CombinedOutput(); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "worker başlatılamadı: "+string(out))
		return
	}
	// crash-loop tespiti (5sn pencere)
	crashLog, saglikli := monitorQueue(unit)
	_, _ = h.DB.ExecContext(r.Context(),
		`UPDATE cp_laravel_apps SET queue_enabled=1, queue_timeout=?, queue_max_jobs=?, queue_connection=? WHERE domain_id=?`,
		req.Timeout, req.MaxJobs, conn, id)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": saglikli, "queue_enabled": true, "saglikli": saglikli, "uyari": crashLog,
	})
}

// GET /domains/{id}/laravel/queue/durum
func (h *Handlers) QueueDurum(w http.ResponseWriter, r *http.Request) {
	id, _, _, _, ok := h.lookup(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	unit := queueUnitName(id) + ".service"
	m := queueProps(unit)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"active_state": m["ActiveState"], "sub_state": m["SubState"], "restarts": m["NRestarts"],
	})
}
