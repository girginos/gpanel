// Package websec — Website Security Monitor.
//
// wpvulnerability.net API istemcisi. Auth'suz, ücretsiz, sınırsız kullanım.
// Uç nokta: https://www.wpvulnerability.net/plugin/<slug>
//                       .../theme/<slug>
//                       .../wordpress/<version>
//
// Yanıt şeması (canlı doğrulandı 2026-08-14 elementor için):
//   {
//     "error": 0,
//     "message": null,
//     "data": {
//       "name":..., "plugin":..., "link":..., "latest": <unix>,
//       "vulnerability": [
//         {
//           "uuid":..., "name":..., "description":...,
//           "operator": {"min_version":..., "min_operator":...,
//                        "max_version":"3.6.3","max_operator":"lt","unfixed":"0"},
//           "source": [{"id":"CVE-2021-24891","name":..., "link":..., "description":...}],
//           "impact": {"cvss": "9.8", "score": "critical"}
//         }
//       ]
//     }
//   }
//
// error=1 (paket yok/silinmiş) yanıtta data null olabilir → boş liste dön.

package websec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	wpvulnBaseURL = "https://www.wpvulnerability.net"
	wpvulnUA      = "GirginOSPanel-Websec/1.0 (+https://girginos.io)"
	wpvulnZaman   = 20 * time.Second
)

// WPVulnZafiyet — dış API'den okuduğumuz tek bir zafiyet kaydı. Alanlar bizim
// modeline uyacak şekilde normalleştirilir.
type WPVulnZafiyet struct {
	CVE         string  `json:"cve"`
	Title       string  `json:"title"`
	Severity    string  `json:"severity"` // critical|high|medium|low
	CVSS        float64 `json:"cvss"`
	FixedIn     string  `json:"fixed_in"`     // güvenli minimum sürüm (biliniyorsa)
	MinVersion  string  `json:"min_version"`  // aralık: v ≥ min
	MinOperator string  `json:"min_operator"` // gte|gt
	MaxVersion  string  `json:"max_version"`  // aralık: v ≤ max
	MaxOperator string  `json:"max_operator"` // lte|lt|eq
	Source      string  `json:"source"`
}

// wpvulnHam — API'nin ham iç yapısı; okuyup Zafiyet'e çeviriyoruz.
type wpvulnHam struct {
	Error   int    `json:"error"`
	Message string `json:"message"`
	Data    *struct {
		Name          string `json:"name"`
		Plugin        string `json:"plugin"`
		Vulnerability []struct {
			Name        string `json:"name"`
			Description any    `json:"description"` // string veya null
			Operator    struct {
				MinVersion  any    `json:"min_version"`  // string veya null
				MinOperator any    `json:"min_operator"`
				MaxVersion  any    `json:"max_version"`
				MaxOperator any    `json:"max_operator"`
				Unfixed     string `json:"unfixed"`
			} `json:"operator"`
			Source []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
				Link string `json:"link"`
			} `json:"source"`
			// 🔴 impact şeması TUTARSIZ (canlı gözlendi 2026-08-14):
			//   - Ekseriya {"cvss": {...}, "score": "..."} (object)
			//   - Bazen [] (boş array — impact bilgisi yok)
			//   - Bazen {"cvss": "9.8"} (düz string — eski kayıtlar)
			// Bu yüzden any olarak alıp normalize ediyoruz.
			Impact any `json:"impact"`
		} `json:"vulnerability"`
	} `json:"data"`
}

// WPVulnGetir — verilen plugin slug için zafiyet listesi.
// Endpoint 404/500 → hata; 200 ama data null → boş liste (bilinen plugin değil).
func WPVulnGetir(ctx context.Context, tur, slug string) ([]WPVulnZafiyet, error) {
	if slug == "" {
		return nil, errors.New("slug boş")
	}
	if tur != "plugin" && tur != "theme" && tur != "wordpress" {
		return nil, fmt.Errorf("bilinmeyen tur: %s", tur)
	}
	// URL kaçış — slug'ta beklenmedik karakter olursa. WP plugin slug'ları
	// genelde [a-z0-9-] ama bir kez trust boundary'ye rastlayalım.
	u := wpvulnBaseURL + "/" + tur + "/" + url.PathEscape(slug)

	ctx2, iptal := context.WithTimeout(ctx, wpvulnZaman)
	defer iptal()
	req, err := http.NewRequestWithContext(ctx2, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", wpvulnUA)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	// Boyut sınırı — 2 MB. Bilinen büyük eklentiler ~200 KB (elementor 165KB).
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}

	var ham wpvulnHam
	if err := json.Unmarshal(body, &ham); err != nil {
		return nil, fmt.Errorf("json parse: %w", err)
	}
	if ham.Data == nil {
		// error=1 (bilinmeyen paket) — normal; zafiyet yok demek.
		return nil, nil
	}

	out := make([]WPVulnZafiyet, 0, len(ham.Data.Vulnerability))
	for _, v := range ham.Data.Vulnerability {
		z := WPVulnZafiyet{Source: "wpvulnerability.net"}
		// CVE — genelde source[0].id "CVE-XXXX-YYYY"; yoksa uuid'in ilk 12
		// karakterini geçici kimlik say.
		if len(v.Source) > 0 && v.Source[0].ID != "" {
			z.CVE = v.Source[0].ID
		}
		if z.CVE == "" {
			z.CVE = "WPV-" + strings.ToUpper(kisaHash(v.Name))
		}
		z.Title = v.Name
		// Impact'ı normalize et — 3 farklı şemayı da kabul et.
		z.CVSS, z.Severity = impactCoz(v.Impact)
		// Severity yoksa CVSS'ten türet — feed bazen bunu doldurmuyor.
		if z.Severity == "" && z.CVSS > 0 {
			z.Severity = cvssToSeverity(z.CVSS)
		}
		// Sürüm aralığı — string'e cast (any nedeniyle)
		z.MinVersion, _ = v.Operator.MinVersion.(string)
		z.MinOperator, _ = v.Operator.MinOperator.(string)
		z.MaxVersion, _ = v.Operator.MaxVersion.(string)
		z.MaxOperator, _ = v.Operator.MaxOperator.(string)
		// FixedIn — Unfixed="0" ise + max_operator "lt"/"lte" ise, max_version
		// yaklaşık olarak düzeltilen sürümdür. Yaklaşımın hatası küçük: gerçek
		// "fixed_in" ayrı bir alan feed'de yok, bu bir tahmindir.
		if v.Operator.Unfixed == "0" && z.MaxVersion != "" {
			z.FixedIn = z.MaxVersion
		}
		out = append(out, z)
	}
	return out, nil
}

// impactCoz — impact alanını (any) parse edip (cvss puanı, severity string)
// döner. Boş/tanınmaz şema → (0, "").
//
// Kabul edilen şekiller:
//   1) {"cvss": {"score": "6.1", "severity": "medium"}, ...}  ← modern
//   2) {"cvss": "9.8", "score": "critical"}                    ← eski
//   3) {"cvss": 9.8}                                           ← nadiren number
//   4) []                                                       ← impact yok
//   5) nil
func impactCoz(v any) (float64, string) {
	m, ok := v.(map[string]any)
	if !ok || m == nil {
		return 0, "" // array veya nil
	}
	// Önce nested cvss objesi
	if c, ok := m["cvss"].(map[string]any); ok {
		var puan float64
		if s, ok := c["score"].(string); ok {
			if f, e := strconv.ParseFloat(strings.TrimSpace(s), 64); e == nil {
				puan = f
			}
		} else if fn, ok := c["score"].(float64); ok {
			puan = fn
		}
		sev := ""
		if s, ok := c["severity"].(string); ok {
			sev = sevNormalize(s)
		}
		if sev == "" && puan > 0 {
			sev = cvssToSeverity(puan)
		}
		return puan, sev
	}
	// Eski şema: cvss string, score seviye
	var puan float64
	if s, ok := m["cvss"].(string); ok {
		if f, e := strconv.ParseFloat(strings.TrimSpace(s), 64); e == nil {
			puan = f
		}
	} else if fn, ok := m["cvss"].(float64); ok {
		puan = fn
	}
	sev := ""
	if s, ok := m["score"].(string); ok {
		sev = sevNormalize(s)
	}
	if sev == "" && puan > 0 {
		sev = cvssToSeverity(puan)
	}
	return puan, sev
}

// sevNormalize — feed'in severity kısaltmaları ("m", "medium", "MODERATE") →
// standart string. Bilinmeyen değerler olduğu gibi lowercase döndürülür.
func sevNormalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "c", "critical":
		return "critical"
	case "h", "high":
		return "high"
	case "m", "medium", "moderate":
		return "medium"
	case "l", "low":
		return "low"
	case "n", "none", "":
		return ""
	}
	return s
}

// cvssToSeverity — CVSS 3.x standart eşikleri.
func cvssToSeverity(c float64) string {
	switch {
	case c >= 9.0:
		return "critical"
	case c >= 7.0:
		return "high"
	case c >= 4.0:
		return "medium"
	case c > 0:
		return "low"
	default:
		return ""
	}
}

// kisaHash — CVE yoksa zafiyeti tekilleştirmek için ada dayalı kısa etiket.
// Tam kriptografik olması gerekmez; sadece UNIQUE KEY için tekil olsun.
func kisaHash(s string) string {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return strconv.FormatUint(uint64(h), 16)
}

// VersiyonKapsanir — kurulu versiyon zafiyet aralığında mı?
// Kural: min ≤ v (varsa) VE v ≤/≥ max (operator'e göre). Kabaca semver
// karşılaştırması — WP eklenti sürüm şemaları genelde N.N.N.
func VersiyonKapsanir(kurulu string, z WPVulnZafiyet) bool {
	if kurulu == "" {
		return false
	}
	if z.MinVersion != "" {
		if cmp := semverKarsilastir(kurulu, z.MinVersion); cmp < 0 {
			return false
		}
	}
	if z.MaxVersion != "" {
		cmp := semverKarsilastir(kurulu, z.MaxVersion)
		switch z.MaxOperator {
		case "lt":
			if cmp >= 0 {
				return false
			}
		case "lte", "le", "":
			if cmp > 0 {
				return false
			}
		case "eq":
			if cmp != 0 {
				return false
			}
		}
	}
	return true
}

// semverKarsilastir — a<b:-1, a==b:0, a>b:1. WordPress plugin sürümleri her
// zaman semver değildir (bazıları "1.0", "1.0-beta"); noktayla parça parça
// karşılaştırıyoruz, alfabetik son parça pre-release sayılır (< herhangi
// sayısal). Yeterli bir yaklaşım — %100 semver-strict olması bu iş için gerekmez.
func semverKarsilastir(a, b string) int {
	pa, pb := versiyonParcala(a), versiyonParcala(b)
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		var x, y string
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		xn, xErr := strconv.Atoi(x)
		yn, yErr := strconv.Atoi(y)
		if xErr == nil && yErr == nil {
			if xn != yn {
				if xn < yn {
					return -1
				}
				return 1
			}
			continue
		}
		if xErr == nil {
			return 1 // sayı > alfabetik (pre-release)
		}
		if yErr == nil {
			return -1
		}
		// ikisi de alfabetik
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func versiyonParcala(v string) []string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.SplitN(v, "+", 2)[0] // build metadata at
	v = strings.ReplaceAll(v, "-", ".")
	return strings.Split(v, ".")
}
