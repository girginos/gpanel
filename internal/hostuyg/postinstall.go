package hostuyg

// Recipe-özel kurulum-sonrası kancalar.
//
// TS3 için: servis ilk kalktığında journal'a düşen
//   Server Query Admin Account created — loginname="serveradmin", password="..."
//   token=...
// bilgilerini yakalar, cp_host_uygulamalar.meta_json'a kaydeder ki panel query
// login yapabilsin + UI privilege key'i kullanıcıya gösterebilsin.

import (
	"context"
	"database/sql"
	"encoding/json"
	"os/exec"
	"regexp"
	"time"
)

var (
	ts3PwdRe   = regexp.MustCompile(`loginname\s*=\s*"([^"]+)"\s*,\s*password\s*=\s*"([^"]+)"`)
	ts3TokenRe = regexp.MustCompile(`token=([A-Za-z0-9+/=]+)`)
)

// PostInstallHook — kurulum sonrası recipe-özel iş. En sondaki başarı
// adımından ÖNCE çağrılır. Hata dönerse kurulum başarılı sayılmaya devam
// eder (best-effort meta enrichment).
func PostInstallHook(db *sql.DB, uygID int64, kod, unitAd string) {
	switch kod {
	case "teamspeak3":
		ts3IlkKimliklerYakala(db, uygID, unitAd)
	}
}

func ts3IlkKimliklerYakala(db *sql.DB, uygID int64, unitAd string) {
	// Journal en fazla 15sn içinde ilk kimlikleri düşürür — 20 * 750ms = 15sn
	var raw string
	for i := 0; i < 20; i++ {
		ctx, iptal := context.WithTimeout(context.Background(), 3*time.Second)
		out, err := exec.CommandContext(ctx, "journalctl",
			"-u", unitAd, "--no-pager", "-n", "500", "-o", "cat").Output()
		iptal()
		if err == nil {
			raw = string(out)
			if ts3PwdRe.MatchString(raw) && ts3TokenRe.MatchString(raw) {
				break
			}
		}
		time.Sleep(750 * time.Millisecond)
	}
	if raw == "" {
		return
	}

	meta := map[string]string{}
	if m := ts3PwdRe.FindStringSubmatch(raw); len(m) == 3 {
		meta["ts3_admin_user"] = m[1]
		meta["ts3_admin_password"] = m[2]
	}
	if m := ts3TokenRe.FindStringSubmatch(raw); len(m) == 2 {
		meta["ts3_admin_token"] = m[1]
	}
	if len(meta) == 0 {
		return
	}

	ctx, iptal := context.WithTimeout(context.Background(), 5*time.Second)
	defer iptal()
	var mevcut string
	_ = db.QueryRowContext(ctx,
		`SELECT COALESCE(meta_json, '{}') FROM cp_host_uygulamalar WHERE id=?`,
		uygID).Scan(&mevcut)
	var mevcutMap map[string]any
	if json.Unmarshal([]byte(mevcut), &mevcutMap) != nil || mevcutMap == nil {
		mevcutMap = map[string]any{}
	}
	for k, v := range meta {
		mevcutMap[k] = v
	}
	yeni, _ := json.Marshal(mevcutMap)
	_, _ = db.ExecContext(ctx,
		`UPDATE cp_host_uygulamalar SET meta_json=? WHERE id=?`,
		string(yeni), uygID)
}
