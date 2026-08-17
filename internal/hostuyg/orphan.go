package hostuyg

// I1 fix: orphan cleanup — panel restart / crash sırasında yarım kalan
// kurulumların disk artıklarını temizle.
//
// Startup'ta (main.go içinden IsRestartTemizle sonrası) çağrılır:
//   OrphanTemizle(db)  →
//     - /opt/gpanel-apps/* dizinleri için DB'de kayıt YOKSA → dizin sil
//     - /etc/systemd/system/gpanel-app-*.service DB'de yoksa → unit + env sil
//     - gpanel-app-* user'lar DB'de yoksa → user sil
//     - _panel.conf marker blokları DB'de olmayan kod'lar için → nginx sil
//
// Best-effort: her adım hata verirse log + devam.

import (
	"context"
	"database/sql"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// OrphanTemizle — main.go'da startup'ta çağrılır. DB'ye göre disk artıkları temizler.
func OrphanTemizle(db *sql.DB) {
	if db == nil {
		return
	}
	// DB'deki kayıtları yükle
	ctx, iptal := context.WithTimeout(context.Background(), 5*time.Second)
	defer iptal()
	rows, err := db.QueryContext(ctx,
		`SELECT kod, ornek_ad, kurulum_yolu, sistem_kullanici, systemd_unit
		 FROM cp_host_uygulamalar`)
	if err != nil {
		log.Printf("hostuyg.OrphanTemizle: DB fetch: %v", err)
		return
	}
	defer rows.Close()
	dbKod := map[string]bool{}
	dbYol := map[string]bool{}
	dbKul := map[string]bool{}
	dbUnit := map[string]bool{}
	for rows.Next() {
		var kod, ornek, yol, kul, unit string
		// N1 fix: Scan hatası SESSİZ atlanırsa kısmen dolu map ile devam →
		// canlı app'ler orphan sanılır. Scan fail → tümüyle iptal.
		if err := rows.Scan(&kod, &ornek, &yol, &kul, &unit); err != nil {
			log.Printf("hostuyg.OrphanTemizle: DB scan hatası — cleanup ATLANDI (güvenlik): %v", err)
			return
		}
		dbKod[kod] = true
		dbYol[yol] = true
		dbKul[kul] = true
		dbUnit[unit] = true
	}

	// N1 fix: DB boş + disk dolu → çok muhtemel corruption/restore mid-flight.
	// Tüm app'leri sessizce silmek yerine cleanup'ı ATLA + log.
	if len(dbKod) == 0 {
		diskDolu := false
		if entries, _ := os.ReadDir(KurulumKok); len(entries) > 0 {
			diskDolu = true
		}
		if !diskDolu {
			systemdEntries, _ := os.ReadDir(UnitDizin)
			for _, e := range systemdEntries {
				if strings.HasPrefix(e.Name(), "gpanel-app-") {
					diskDolu = true
					break
				}
			}
		}
		if diskDolu {
			log.Printf("hostuyg.OrphanTemizle: DB boş ama disk'te app artıkları var → cleanup ATLANDI (kill-switch guard). Manuel temizlik gerekiyorsa 'gpanel hostuyg cleanup --force' kullan.")
			return
		}
	}

	silinen := 0

	// 0) `.rollback.tmp` + `.tmp` startup cleanup — nginx.go / persist.go
	// crash mid-write olsa dosyaları geçici olarak bırakır.
	// Defense-in-depth: 30s'den yeni dosyalara dokunma (canlı yazım olabilir).
	for _, suffix := range []string{".rollback.tmp", ".tmp"} {
		yol := PanelVhostYolu + suffix
		st, err := os.Stat(yol)
		if err != nil {
			continue
		}
		if time.Since(st.ModTime()) < 30*time.Second {
			log.Printf("hostuyg.OrphanTemizle: %s ÇOK YENİ (%v), silme atlandı",
				yol, time.Since(st.ModTime()))
			continue
		}
		_ = os.Remove(yol)
		log.Printf("hostuyg.OrphanTemizle: kalıntı %s silindi", yol)
	}

	// 1) /opt/gpanel-apps/* dizinleri
	entries, _ := os.ReadDir(KurulumKok)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		yol := filepath.Join(KurulumKok, e.Name())
		if dbYol[yol] {
			continue
		}
		if err := os.RemoveAll(yol); err == nil {
			silinen++
			log.Printf("hostuyg.OrphanTemizle: orphan dizin silindi: %s", yol)
		}
	}

	// 2) /etc/systemd/system/gpanel-app-*.service unit'leri + .env dosyaları
	systemdEntries, _ := os.ReadDir(UnitDizin)
	for _, e := range systemdEntries {
		ad := e.Name()
		if !strings.HasPrefix(ad, "gpanel-app-") {
			continue
		}
		if strings.HasSuffix(ad, ".service") {
			unitAd := strings.TrimSuffix(ad, ".service")
			if dbUnit[unitAd] {
				continue
			}
			// stop + disable + sil
			_, _ = exec.Command("systemctl", "stop", ad).CombinedOutput()
			_, _ = exec.Command("systemctl", "disable", ad).CombinedOutput()
			_ = os.Remove(filepath.Join(UnitDizin, ad))
			_ = os.Remove(filepath.Join(UnitDizin, unitAd+".env"))
			silinen++
			log.Printf("hostuyg.OrphanTemizle: orphan unit silindi: %s", unitAd)
		}
	}
	if silinen > 0 {
		_, _ = exec.Command("systemctl", "daemon-reload").CombinedOutput()
	}

	// 3) gpanel-app-* user'lar (getent passwd) — I3 fix: sssd/LDAP hang
	// için timeout. NSS backend yavaşsa 5s'te iptal.
	getentCtx, getentIptal := context.WithTimeout(context.Background(), 5*time.Second)
	defer getentIptal()
	out, err := exec.CommandContext(getentCtx, "getent", "passwd").Output()
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if !strings.HasPrefix(line, KullaniciPrefix) {
				continue
			}
			f := strings.SplitN(line, ":", 2)
			if len(f) < 1 {
				continue
			}
			ad := f[0]
			if dbKul[ad] {
				continue
			}
			_ = KullaniciSil(ad)
			silinen++
			log.Printf("hostuyg.OrphanTemizle: orphan user silindi: %s", ad)
		}
	}

	// 4a) `/etc/nginx/conf.d/gpanel-app-*.conf` orphan subdomain vhost'ları
	// (kodları DB'de olmayanlar) + eşlik eden cert dizinleri
	// SubdomainAd'ı DB'de olan kodların map'ini kur → kodun subdomain'i
	dbKodSub := map[string]string{} // kod → subdomain_ad
	if rows2, err2 := db.QueryContext(ctx, `SELECT kod, subdomain_ad FROM cp_host_uygulamalar`); err2 == nil {
		defer rows2.Close()
		for rows2.Next() {
			var kod, sub string
			if rows2.Scan(&kod, &sub) == nil {
				dbKodSub[kod] = sub
			}
		}
	}
	// Vhost dosyalarını tara — DB'de olmayan gpanel-app-*.conf'ları temizle
	if entries, err := os.ReadDir(SubdomainVhostDir); err == nil {
		for _, e := range entries {
			ad := e.Name()
			if !strings.HasPrefix(ad, "gpanel-app-") || !strings.HasSuffix(ad, ".conf") {
				continue
			}
			if strings.HasSuffix(ad, "-acme.conf") {
				// Geçici ACME challenge vhost — 30dk'dan eski ise sil (canlı issue biterdi)
				st, e2 := os.Stat(filepath.Join(SubdomainVhostDir, ad))
				if e2 == nil && time.Since(st.ModTime()) > 30*time.Minute {
					_ = os.Remove(filepath.Join(SubdomainVhostDir, ad))
					silinen++
					log.Printf("hostuyg.OrphanTemizle: eski ACME challenge vhost silindi: %s", ad)
				}
				continue
			}
			// gpanel-app-<kod>.conf → kod ayıkla
			kod := strings.TrimSuffix(strings.TrimPrefix(ad, "gpanel-app-"), ".conf")
			if _, ok := dbKodSub[kod]; ok {
				continue // DB'de kayıtlı, orphan değil
			}
			SubdomainVhostSil(kod)
			silinen++
			log.Printf("hostuyg.OrphanTemizle: orphan subdomain vhost silindi: %s", ad)
		}
	}
	// `/etc/ssl/gpanel-apps/*` cert dizinleri — DB'de subdomain_ad olmayanları sil
	if entries, err := os.ReadDir(HostuygSSLKok); err == nil {
		dbSubSet := map[string]bool{}
		for _, sub := range dbKodSub {
			if sub != "" {
				dbSubSet[sub] = true
			}
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if dbSubSet[e.Name()] {
				continue
			}
			SubdomainCertSil(e.Name())
			silinen++
			log.Printf("hostuyg.OrphanTemizle: orphan subdomain cert silindi: %s", e.Name())
		}
	}

	// 4b) _panel.conf marker blokları — I4 fix: batch cleanup, tek reload.
	// Her `NginxProxyKaldir(kod)` içinde nginx reload var → 10 orphan = 10
	// reload = 10× flap. subpathLocationSil'i direkt çağır + son bir reload.
	if b, err := os.ReadFile(PanelVhostYolu); err == nil {
		content := string(b)
		markerRe := regexp.MustCompile(`# >>> gpanel-app:([a-z0-9-]+) >>>`)
		matches := markerRe.FindAllStringSubmatch(content, -1)
		reloadGerek := false
		for _, m := range matches {
			kod := m[1]
			if dbKod[kod] {
				continue
			}
			if err := subpathLocationSil(kod); err == nil {
				silinen++
				reloadGerek = true
				log.Printf("hostuyg.OrphanTemizle: orphan nginx bloğu silindi: %s", kod)
			}
		}
		if reloadGerek {
			_, _ = exec.Command("nginx", "-s", "reload").CombinedOutput()
		}
	}

	if silinen > 0 {
		log.Printf("hostuyg.OrphanTemizle: toplam %d orphan artık temizlendi", silinen)
	}
}
