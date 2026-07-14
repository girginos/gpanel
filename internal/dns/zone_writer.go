package dns

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

const (
	ZoneDir          = "/var/named"
	NamedConfInclude = "/etc/named/girginospanel-zones.conf"
)

// fqdn: hedef alan adi (NS/MX/CNAME/SRV) trailing nokta ile bitmeliki BIND
// "relative" yorumlamasın (yoksa zone adi append eder ve "host.X.Y.X.Y" gibi olur).
func fqdn(tip, deger string) string {
	t := strings.ToUpper(strings.TrimSpace(tip))
	d := strings.TrimSpace(deger)
	if t == "NS" || t == "MX" || t == "CNAME" || t == "SRV" || t == "PTR" {
		if !strings.HasSuffix(d, ".") {
			d = d + "."
		}
	}
	return d
}

var zoneTmpl = template.Must(template.New("z").Funcs(template.FuncMap{
	"fqdn": fqdn,
}).Parse(`$TTL 3600
@   IN  SOA ns1.{{.AlanAdi}}. admin.{{.AlanAdi}}. (
    {{.Serial}}  ; serial
    3600         ; refresh
    900          ; retry
    1209600      ; expire
    3600         ; minimum
)
{{range .Kayitlar}}{{.Ad}}	{{.TTL}}	IN	{{.Tip}}	{{if .Oncelik}}{{.Oncelik}} {{end}}{{fqdn .Tip .Deger}}
{{end}}`))

type zoneCtx struct {
	AlanAdi  string
	Serial   string
	Kayitlar []Kayit
}

func WriteZone(ctx context.Context, db *sql.DB, domainID int64) error {
	var alanAdi string
	if err := db.QueryRowContext(ctx, `SELECT alan_adi FROM domains WHERE id=?`, domainID).Scan(&alanAdi); err != nil {
		return err
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id, domain_id, ad, tip, deger, ttl, oncelik, aktif,
		   DATE_FORMAT(created_at,'%Y-%m-%d %H:%i') FROM dns_records
		 WHERE domain_id=? AND aktif=1 ORDER BY tip, ad`, domainID)
	if err != nil {
		return err
	}
	defer rows.Close()
	kayitlar := make([]Kayit, 0)
	for rows.Next() {
		k, err := scan(rows)
		if err == nil {
			kayitlar = append(kayitlar, k)
		}
	}
	if len(kayitlar) == 0 {
		return nil
	}

	// serial: yyyymmddHH + sn (saniye granularity, 10 hane max DNS standardı için)
	// Format: yyyymmddNN where NN is HH (00-23). Aynı saat içinde tekrar yazımda BIND eski cache'i tutabilir;
	// bu durumda named.run.log uyarı verir ama prod'da nadir.
	serial := time.Now().UTC().Format("2006010215")

	var buf bytes.Buffer
	if err := zoneTmpl.Execute(&buf, zoneCtx{AlanAdi: alanAdi, Serial: serial, Kayitlar: kayitlar}); err != nil {
		return err
	}

	_ = os.MkdirAll(ZoneDir, 0750)
	zonePath := filepath.Join(ZoneDir, alanAdi+".zone")
	if err := os.WriteFile(zonePath, buf.Bytes(), 0640); err != nil {
		return err
	}
	_, _ = exec.Command("chown", "named:named", zonePath).CombinedOutput()
	_, _ = exec.Command("restorecon", zonePath).CombinedOutput()

	if out, err := exec.Command("named-checkzone", alanAdi, zonePath).CombinedOutput(); err != nil {
		return fmt.Errorf("named-checkzone: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if err := updateZoneIncludes(ctx, db); err != nil {
		return err
	}
	_, _ = exec.Command("rndc", "reload").CombinedOutput()
	return nil
}

func updateZoneIncludes(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT d.alan_adi FROM domains d
	  WHERE EXISTS (SELECT 1 FROM dns_records r WHERE r.domain_id=d.id AND r.aktif=1)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var sb strings.Builder
	sb.WriteString("// girginospanel — otomatik üretildi\n")
	for rows.Next() {
		var alanAdi string
		if err := rows.Scan(&alanAdi); err == nil {
			fmt.Fprintf(&sb, `zone "%s" { type master; file "%s/%s.zone"; allow-query { any; }; };
`, alanAdi, ZoneDir, alanAdi)
		}
	}
	return os.WriteFile(NamedConfInclude, []byte(sb.String()), 0644)
}

func DeleteZone(ctx context.Context, db *sql.DB, alanAdi string) error {
	_ = os.Remove(filepath.Join(ZoneDir, alanAdi+".zone"))
	_ = updateZoneIncludes(ctx, db)
	_, _ = exec.Command("rndc", "reload").CombinedOutput()
	return nil
}
