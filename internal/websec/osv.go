package websec

// OSV.dev istemcisi — Node.js (npm) ve PHP (Packagist) tarayıcıları paylaşır.
//
// API: https://api.osv.dev
// Docs: https://google.github.io/osv.dev/api/
//
// İki uç var:
//   POST /v1/query       → tek paket sorgusu, 5 istek/sn'lik yumuşak sınır
//   POST /v1/querybatch  → toplu sorgu (max 1000 paket), tek istek. Yanıt sadece
//                          vuln ID'lerini döner; detay için tek tek /v1/vulns/{id}
//                          çağırmak gerekir → yavaş. Basit tutalım: query kullan.
//
// Rate limit: OSV kamuya açık, key yok. Kaba yaklaşık 5-10 rps. Biz ekleyicileri
// istek arasında 200ms jitter ile geciktiriyoruz (feedIsteğiBekleme).
//
// 🔴 OSV zafiyet aralık hesabını KENDİSİ yapıyor — kurulu versiyonu geçirince
// yalnız etkileyenleri döner. Bizim VersiyonKapsanir'a (WP tarafı) benzer bir
// eşleştirmeye gerek YOK.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	osvBaseURL = "https://api.osv.dev"
	osvUA      = "GirginOSPanel-Websec/1.0 (+https://girginos.io)"
	osvZaman   = 20 * time.Second

	OSVEcosysNPM       = "npm"
	OSVEcosysPackagist = "Packagist"
)

type osvSorguGovde struct {
	Package struct {
		Name      string `json:"name"`
		Ecosystem string `json:"ecosystem"`
	} `json:"package"`
	Version string `json:"version"`
}

type osvYanit struct {
	Vulns []osvVuln `json:"vulns"`
}

type osvVuln struct {
	ID       string   `json:"id"`       // GHSA-xxxx-xxxx-xxxx
	Aliases  []string `json:"aliases"`  // ["CVE-2024-...", "..."]
	Summary  string   `json:"summary"`
	Details  string   `json:"details"`
	Severity []struct {
		Type  string `json:"type"`  // "CVSS_V3" | "CVSS_V4"
		Score string `json:"score"` // "CVSS:3.1/AV:N/..."  → parse edip base score çıkar
	} `json:"severity"`
	Affected []struct {
		Ranges []struct {
			Events []map[string]string `json:"events"` // [{"introduced":"0"},{"fixed":"4.17.3"}]
		} `json:"ranges"`
	} `json:"affected"`
	DatabaseSpecific struct {
		Severity string `json:"severity"` // GHSA'da "HIGH", "CRITICAL"
	} `json:"database_specific"`
}

// OSVSorgula — paket + versiyon için zafiyet listesi. Yok/hata = boş dilim.
func OSVSorgula(ctx context.Context, ecosystem, name, version string) ([]WPVulnZafiyet, error) {
	if name == "" || version == "" {
		return nil, nil
	}

	// v gövdesini kur
	var g osvSorguGovde
	g.Package.Name = name
	g.Package.Ecosystem = ecosystem
	g.Version = version
	body, _ := json.Marshal(g)

	ctx2, iptal := context.WithTimeout(ctx, osvZaman)
	defer iptal()
	req, err := http.NewRequestWithContext(ctx2, "POST", osvBaseURL+"/v1/query", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", osvUA)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("OSV HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}

	var y osvYanit
	if err := json.Unmarshal(raw, &y); err != nil {
		return nil, fmt.Errorf("json parse: %w", err)
	}

	// Ortak Zafiyet modeline dönüştür (WPVulnZafiyet zaten generic; sadece
	// isim yanıltıcı, refactor bir sonraki tur).
	out := make([]WPVulnZafiyet, 0, len(y.Vulns))
	for _, v := range y.Vulns {
		z := WPVulnZafiyet{Source: "osv.dev"}
		// CVE tercih; yoksa GHSA
		z.CVE = birinciCVE(v.Aliases)
		if z.CVE == "" {
			z.CVE = v.ID
		}
		z.Title = v.Summary
		if z.Title == "" && v.Details != "" {
			// Details bazen çok uzun; ilk satırı al
			z.Title = ilkSatir(v.Details, 140)
		}
		// Severity: database_specific.severity (GHSA) > CVSS vector'den parse
		if v.DatabaseSpecific.Severity != "" {
			z.Severity = sevNormalize(v.DatabaseSpecific.Severity)
		}
		if len(v.Severity) > 0 {
			puan := cvssVectorPuan(v.Severity[0].Score)
			if puan > 0 {
				z.CVSS = puan
				if z.Severity == "" {
					z.Severity = cvssToSeverity(puan)
				}
			}
		}
		// FixedIn — affected[0].ranges[0].events[?].fixed en yakın düzeltilmiş sürüm
		z.FixedIn = firstFixed(v.Affected)
		out = append(out, z)
	}
	return out, nil
}

func birinciCVE(aliases []string) string {
	for _, a := range aliases {
		if strings.HasPrefix(a, "CVE-") {
			return a
		}
	}
	return ""
}

func ilkSatir(s string, max int) string {
	if i := strings.IndexByte(s, '\n'); i > 0 {
		s = s[:i]
	}
	if len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

// cvssVectorPuan — CVSS 3.x vector'ünden Base Score. Gerçek hesaplama
// cvss.go'da; burası ince sarmalayıcı.
func cvssVectorPuan(vector string) float64 {
	return CVSSBaseScore(vector)
}

// firstFixed — affected/ranges/events içinden en yakın "fixed" versiyonunu
// bul. Semver aralıklı olabilir; ilkini dönmek pratikte kullanıcıya "en az
// bu sürüme çık" ipucu verir.
func firstFixed(affected []struct {
	Ranges []struct {
		Events []map[string]string `json:"events"`
	} `json:"ranges"`
}) string {
	for _, a := range affected {
		for _, r := range a.Ranges {
			for _, e := range r.Events {
				if f, ok := e["fixed"]; ok && f != "" {
					return f
				}
			}
		}
	}
	return ""
}

// oSVPaketiKarsilastirveKaydet — Node/PHP tarayıcıları için ortak yardımcı.
// WP tarafındaki paketKarsilastirveKaydet ile eşdeğer, ama versiyon
// karşılaştırması yok (OSV döndüğü şey zaten etkilenen zafiyetlerin listesi).
func oSVPaketiKarsilastirveKaydet(ctx context.Context, db interface{ Exec(string, ...any) (any, error) }, _ struct{}) int {
	// Bu fonksiyon Node/PHP tarayıcılarında inline yazıldı — burada bir stub yok.
	// Import döngüsü nedeniyle burada tutmuyoruz.
	return 0
}

var _ = errors.New // gelecekte kullanmak için import'u tut
