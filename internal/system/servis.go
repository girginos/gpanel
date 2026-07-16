package system

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"

	"girginospanel/internal/httpx"
)

// Servis yönetimi: Genel Ayarlar'dan izin verilen servisleri restart/reload etme.
// Güvenlik: SADECE allowlist'teki birimler; keyfi systemctl çalıştırılamaz.

type servisTanim struct {
	Birim   string `json:"birim"`   // systemd unit adı
	Etiket  string `json:"etiket"`  // UI etiketi
	Reload  bool   `json:"reload"`  // reload destekliyor mu
}

var servisAllow = []servisTanim{
	{"nginx", "Nginx (Web)", true},
	{"httpd", "Apache (Backend)", true},
	{"mariadb", "MariaDB (Veritabanı)", false},
	{"named", "BIND (DNS)", true},
	{"valkey", "Valkey (Redis)", false},
	{"php-fpm", "PHP-FPM 8.3", true},
	{"php82-php-fpm", "PHP-FPM 8.2", true},
	{"php74-php-fpm", "PHP-FPM 7.4", true},
	{"pure-ftpd", "Pure-FTPd (FTP)", false},
	{"crond", "Cron", false},
}

func tanimBul(birim string) (servisTanim, bool) {
	for _, s := range servisAllow {
		if s.Birim == birim {
			return s, true
		}
	}
	return servisTanim{}, false
}

// ServisDurumlar: GET — izin verilen servislerin listesi + durumları (active/inactive/absent).
func ServisDurumlar(w http.ResponseWriter, r *http.Request) {
	type satir struct {
		servisTanim
		Durum string `json:"durum"`
	}
	out := make([]satir, 0, len(servisAllow))
	for _, s := range servisAllow {
		st := strings.TrimSpace(runOut("systemctl", "is-active", s.Birim))
		// is-active: active / inactive / failed / unknown; birim yoksa "inactive"/boş döner
		if st == "" {
			st = "absent"
		}
		out = append(out, satir{servisTanim: s, Durum: st})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// ServisIslem: POST {birim, aksiyon:"restart"|"reload"} — allowlist'li servisi yeniden başlat.
func ServisIslem(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Birim   string `json:"birim"`
		Aksiyon string `json:"aksiyon"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	tanim, ok := tanimBul(req.Birim)
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "bu servis için işlem izni yok")
		return
	}
	aksiyon := req.Aksiyon
	if aksiyon != "restart" && aksiyon != "reload" {
		aksiyon = "restart"
	}
	if aksiyon == "reload" && !tanim.Reload {
		aksiyon = "restart" // reload desteklemeyen serviste restart'a düş
	}
	out := runOut("systemctl", aksiyon, req.Birim)
	durum := strings.TrimSpace(runOut("systemctl", "is-active", req.Birim))
	if durum != "active" {
		httpx.WriteError(w, http.StatusInternalServerError,
			tanim.Etiket+" "+aksiyon+" başarısız (durum: "+durum+") "+strings.TrimSpace(out))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "birim": req.Birim, "aksiyon": aksiyon, "durum": durum,
	})
}

func runOut(name string, args ...string) string {
	b, _ := exec.Command(name, args...).CombinedOutput()
	return string(b)
}
