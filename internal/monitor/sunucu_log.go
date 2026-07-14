package monitor

import (
	"net/http"
	"os/exec"
	"strconv"
	"strings"

	"girginospanel/internal/httpx"
)

// logKaynaklari: kaynak anahtarı → systemd unit. Allowlist — komut enjeksiyonu YOK
// (kullanıcı girdisi doğrudan komuta gitmez, sadece bu haritadan geçer).
var logKaynaklari = map[string]string{
	"panel":   "girginospanel.service",
	"mariadb": "mariadb.service",
	"named":   "named.service",
	"sshd":    "sshd.service",
	"cron":    "crond.service",
}

// nginx journald'a yazmaz (dosyaya loglar) → dosya-tabanlı kaynak.
var dosyaKaynaklari = map[string]string{
	"nginx": "/var/log/nginx/error.log",
}

var logKaynakSira = []string{"panel", "nginx", "mariadb", "named", "sshd", "cron", "sistem"}

// SunucuLog: GET /admin/system/loglar?kaynak=panel&son=200 — journald sunucu günlükleri.
func (h *Handlers) SunucuLog(w http.ResponseWriter, r *http.Request) {
	kaynak := r.URL.Query().Get("kaynak")
	if kaynak == "" {
		kaynak = "panel"
	}
	son, _ := strconv.Atoi(r.URL.Query().Get("son"))
	if son < 50 {
		son = 200
	}
	if son > 1000 {
		son = 1000
	}
	var out []byte
	if dosya, ok := dosyaKaynaklari[kaynak]; ok {
		// dosya-tabanlı (nginx error.log gibi) — tail
		out, _ = exec.Command("tail", "-n", strconv.Itoa(son), dosya).CombinedOutput()
	} else {
		args := []string{"--no-pager", "-o", "short-iso", "-n", strconv.Itoa(son)}
		if kaynak != "sistem" {
			unit, ok := logKaynaklari[kaynak]
			if !ok {
				httpx.WriteError(w, http.StatusBadRequest, "geçersiz log kaynağı")
				return
			}
			args = append(args, "-u", unit)
		}
		out, _ = exec.Command("journalctl", args...).CombinedOutput()
	}
	metin := strings.TrimRight(string(out), "\n")
	satirlar := []string{}
	if metin != "" {
		satirlar = strings.Split(metin, "\n")
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"kaynak":    kaynak,
		"satirlar":  satirlar,
		"kaynaklar": logKaynakSira,
	})
}
