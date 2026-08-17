package websec

// Node.js / React / Next.js tarayıcı — kiracının public_html'inde
// package.json + package-lock.json arar, kurulu paket + versiyonlarını
// OSV.dev (npm ecosystem) ile eşler.
//
// 🔴 node_modules TARANMAZ — dev bağımlılıkları büyük gürültü. Sadece
// lock dosyası authoritative. package-lock.json > yarn.lock > pnpm-lock.yaml
// sırası; tenant birden fazla varsa ilk bulduğuna güven.
//
// 🔴 Alt-dizin desteği yok — sadece public_html kökü. Monorepo/subapp için
// sonraki iterasyon.

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type nodejsKurulum struct {
	DomainID int64
	AlanAdi  string
	Yol      string
	Paketler []nodejsPaket
}

type nodejsPaket struct {
	Ad     string
	Surum  string
}

// nodejsBul — public_html'de package.json arar. Bulunursa lock dosyasını
// parse eder ve tüm dependency+devDependency paketlerini çıkarır.
//
// Lock önceliği:
//   1. package-lock.json (npm)      — en kesin, semver-lock
//   2. yarn.lock                     — Yarn 1.x + Berry (klasik format)
//   3. pnpm-lock.yaml                — pnpm workspace/monorepo yaygın
//   4. package.json dependencies     — hiçbir lock yoksa fallback (range içerir)
func nodejsBul(ctx context.Context, sk, alanAdi string) *nodejsKurulum {
	kok := filepath.Join("/home", sk, "public_html")
	pj := filepath.Join(kok, "package.json")
	if _, err := os.Stat(pj); err != nil {
		return nil
	}

	var paketler []nodejsPaket

	// 1) npm
	if _, e := os.Stat(filepath.Join(kok, "package-lock.json")); e == nil {
		paketler = npmLockOku(filepath.Join(kok, "package-lock.json"))
	}
	// 2) yarn
	if len(paketler) == 0 {
		if _, e := os.Stat(filepath.Join(kok, "yarn.lock")); e == nil {
			paketler = yarnLockOku(filepath.Join(kok, "yarn.lock"))
		}
	}
	// 3) pnpm
	if len(paketler) == 0 {
		if _, e := os.Stat(filepath.Join(kok, "pnpm-lock.yaml")); e == nil {
			paketler = pnpmLockOku(filepath.Join(kok, "pnpm-lock.yaml"))
		}
	}
	// 4) Fallback — package.json (range içerir; ilk semver'a düşer)
	if len(paketler) == 0 {
		paketler = packageJSONDependenciesOku(pj)
	}
	if len(paketler) == 0 {
		return nil
	}
	return &nodejsKurulum{Yol: kok, Paketler: paketler}
}

// npmLockOku — npm package-lock.json parse eder.
func npmLockOku(pl string) []nodejsPaket {
	raw, err := os.ReadFile(pl)
	if err != nil {
		return nil
	}
	// lockfileVersion 2/3: "packages" haritası; anahtar "node_modules/xxx"
	var lock struct {
		Packages map[string]struct {
			Version string `json:"version"`
			Dev     bool   `json:"dev"`
		} `json:"packages"`
		Dependencies map[string]struct {
			Version string `json:"version"`
			Dev     bool   `json:"dev"`
		} `json:"dependencies"` // lockfileVersion 1
	}
	if err := json.Unmarshal(raw, &lock); err != nil {
		return nil
	}
	set := map[string]string{} // ad → versiyon (dedup)

	if len(lock.Packages) > 0 {
		for k, v := range lock.Packages {
			if v.Dev {
				continue // dev-only — üretim etkisi yok
			}
			if k == "" {
				continue // root package
			}
			// "node_modules/xxx" → "xxx" (nested: "node_modules/a/node_modules/b" → "b")
			ad := k
			if p := strings.LastIndex(ad, "node_modules/"); p >= 0 {
				ad = ad[p+len("node_modules/"):]
			}
			if ad == "" || v.Version == "" {
				continue
			}
			set[ad] = v.Version
		}
	} else {
		// lockfileVersion 1 — daha eski
		for ad, v := range lock.Dependencies {
			if v.Dev || v.Version == "" {
				continue
			}
			set[ad] = v.Version
		}
	}

	out := make([]nodejsPaket, 0, len(set))
	for ad, s := range set {
		out = append(out, nodejsPaket{Ad: ad, Surum: s})
	}
	return out
}

// lastIndexOf kaldırıldı — strings.LastIndex zaten stdlib'de.

// yarnLockOku — yarn.lock (klasik + Berry) parse eder.
//
// Format örneği:
//   "lodash@^4.17.15":
//     version "4.17.21"
//     resolved "https://..."
//
//   lodash@^4.17.15, lodash@^4.17.19:
//     version "4.17.21"
//
// 🔴 Berry (Yarn 2+) __metadata bloğu var, atlanmalı. dev vs prod ayrımı bu
// dosyadan çıkarılamaz — hepsi dahil (feed'in yanlış-pozitif oranı OSV'de
// düşük olduğu için pratik sorun değil).
func yarnLockOku(p string) []nodejsPaket {
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	out := []nodejsPaket{}
	set := map[string]string{}

	// Basit satır-satır durum makinesi: paket başlığı (spec listesi) satırı
	// → sonraki `  version "..."` satırından versiyonu al.
	var bekleyenAd string
	for _, satir := range strings.Split(string(raw), "\n") {
		// Yorum ve boş
		t := strings.TrimRight(satir, "\r")
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		// Paket başlığı — girintisiz, ':' ile biter
		if !strings.HasPrefix(t, " ") && !strings.HasPrefix(t, "\t") && strings.HasSuffix(t, ":") {
			// "__metadata:" veya "lodash@^4.17.15:" veya "\"lodash@^4.17.15\":"
			bas := strings.TrimSuffix(t, ":")
			if bas == "__metadata" {
				bekleyenAd = ""
				continue
			}
			// İlk spec'i al (virgülle ayrılmış birden fazla olabilir)
			ilkSpec := bas
			if i := strings.Index(bas, ","); i >= 0 {
				ilkSpec = strings.TrimSpace(bas[:i])
			}
			ilkSpec = strings.Trim(ilkSpec, "\"")
			// 🔴 Berry non-npm protokollerini atla — OSV bunları bilmez, sadece
			// gürültü yaratır. "foo@workspace:...", "foo@link:...", "foo@file:..."
			if berryProtokolBar(ilkSpec) {
				bekleyenAd = ""
				continue
			}
			ad := yarnSpecAd(ilkSpec)
			bekleyenAd = ad
			continue
		}
		// Girintili "version" satırı → değeri al
		trim := strings.TrimSpace(t)
		if bekleyenAd != "" && strings.HasPrefix(trim, "version ") {
			v := strings.TrimPrefix(trim, "version ")
			v = strings.Trim(v, "\"")
			if v != "" {
				set[bekleyenAd] = v
			}
			bekleyenAd = ""
		}
	}
	for ad, s := range set {
		out = append(out, nodejsPaket{Ad: ad, Surum: s})
	}
	return out
}

// berryProtokolBar — spec Berry'nin npm-dışı protokolünü içeriyor mu?
func berryProtokolBar(spec string) bool {
	// scoped: @scope/name@workspace:...  → 2. @ sonrası bak
	// scoped değil: name@workspace:...   → 1. @ sonrası bak
	at := strings.Index(spec, "@")
	if strings.HasPrefix(spec, "@") { // scoped
		at = strings.Index(spec[1:], "@")
		if at >= 0 { at++ }
	}
	if at < 0 || at+1 >= len(spec) { return false }
	rest := spec[at+1:]
	for _, p := range []string{"workspace:", "link:", "portal:", "file:", "git", "http", "npm:"} {
		if strings.HasPrefix(rest, p) {
			return true
		}
	}
	return false
}

// yarnSpecAd — "lodash@^4.17.15" → "lodash", "@types/node@^18.0" → "@types/node"
func yarnSpecAd(spec string) string {
	if strings.HasPrefix(spec, "@") {
		// scoped: ikinci "@" ayırıcı
		i := strings.Index(spec[1:], "@")
		if i < 0 {
			return spec
		}
		return spec[:1+i]
	}
	if i := strings.Index(spec, "@"); i >= 0 {
		return spec[:i]
	}
	return spec
}

// pnpmLockOku — pnpm-lock.yaml parse eder (minimal, YAML paketi kullanmadan).
//
// Formatlar:
//   v5:  `/lodash/4.17.21:`              (slash ayırıcı, leading '/')
//   v6:  `/lodash@4.17.21:`              (at ayırıcı, leading '/')
//   v9:  `lodash@4.17.21:`               (leading '/' KALDIRILDI, at ayırıcı)
//   v9:  `lodash@4.17.21(peer@x):`       (peer bilgisi parantezle)
//   v9:  `@scope/pkg@1.0.0:`             (scoped paket, leading '/' YOK)
//
// 🔴 pnpm v9 (May 2024'ten beri varsayılan) leading '/' KULLANMIYOR. Önceki
// parser sadece '/' ile başlayanları kabul ediyordu → v9 tenantlar tamamen
// sessizce atlanıyor + package.json fallback'ine düşüyordu (daha az kesin).
// Şimdi: hem '/' hem '/'-siz kabul edilir.
//
// 🔴 devDependencies ile prodDeps arasında ayırım yapmaz — birden çok
// versiyondaki aynı paket set halinde alınır (dedup).
func pnpmLockOku(p string) []nodejsPaket {
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	set := map[string]string{}
	packagesBaslasin := false
	for _, satir := range strings.Split(string(raw), "\n") {
		t := strings.TrimRight(satir, "\r")
		if strings.HasPrefix(t, "packages:") {
			packagesBaslasin = true
			continue
		}
		if !packagesBaslasin {
			continue
		}
		// packages: bloğu bitince başka bir top-level anahtar gelir (girinti yok)
		if len(t) > 0 && t[0] != ' ' && t[0] != '\t' && !strings.HasPrefix(t, "#") {
			if !strings.HasPrefix(t, "packages:") {
				break
			}
		}
		trim := strings.TrimSpace(t)
		if !strings.HasSuffix(trim, ":") {
			continue
		}
		anahtar := strings.TrimSuffix(trim, ":")
		// v5/v6: leading '/'; v9: '/' yok. İkisini de kabul et.
		anahtar = strings.TrimPrefix(anahtar, "/")
		// Paket anahtarı '@' veya '/' içermeli (versiyon ayırıcı). Örneğin
		// "resolution:" gibi metadata satırlarını atla.
		if !strings.Contains(anahtar, "@") && !strings.Contains(anahtar, "/") {
			continue
		}
		// v9 metadata satırları: 'dependencies:', 'snapshots:' vb.
		if !strings.ContainsAny(anahtar, "@/") || strings.HasSuffix(anahtar, ":") {
			continue
		}
		// Parantezli peer bilgisi at
		if i := strings.Index(anahtar, "("); i >= 0 {
			anahtar = anahtar[:i]
		}
		ad, ver := pnpmAnahtarAyir(anahtar)
		if ad == "" || ver == "" {
			continue
		}
		if mev, ok := set[ad]; !ok || semverKarsilastir(ver, mev) > 0 {
			set[ad] = ver
		}
	}
	out := make([]nodejsPaket, 0, len(set))
	for ad, s := range set {
		out = append(out, nodejsPaket{Ad: ad, Surum: s})
	}
	return out
}

// pnpmAnahtarAyir — "lodash@4.17.21" veya "lodash/4.17.21" veya
// "@scope/pkg@1.0.0" veya "@scope/pkg/1.0.0" → (ad, versiyon)
func pnpmAnahtarAyir(k string) (string, string) {
	// pnpm v6+ '@' ayırıcı, v5 '/' ayırıcı
	// Scoped paketler için son '@' veya son '/' ayırıcı
	if strings.HasPrefix(k, "@") {
		// scoped: son '@' veya son '/' ver-boundary
		if i := strings.LastIndex(k, "@"); i > 0 { // ilk @'ı atla
			return k[:i], k[i+1:]
		}
		if i := strings.LastIndex(k, "/"); i > 0 {
			return k[:i], k[i+1:]
		}
		return "", ""
	}
	if i := strings.LastIndex(k, "@"); i > 0 {
		return k[:i], k[i+1:]
	}
	if i := strings.LastIndex(k, "/"); i > 0 {
		return k[:i], k[i+1:]
	}
	return "", ""
}

// packageJSONDependenciesOku — lock yoksa. Yalnız dependencies (dev DEĞİL).
// Version range'leri ("^4.17.0") çıkar; sıkı bir semver kalıbı yakala:
// önce sayı, sonra iki opsiyonel `.NUMBER` bloğu, opsiyonel `-pre` eki.
// Bu, "^1.2.3", "~1.2.3", ">=1.2 <2.0" gibi girdilerin YALNIZ ilk sürümünü
// alır ve "npm:@scope/foo@1.0.0" gibi bozuk speclerde boş döner.
func packageJSONDependenciesOku(pj string) []nodejsPaket {
	raw, err := os.ReadFile(pj)
	if err != nil {
		return nil
	}
	var pj_ struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(raw, &pj_); err != nil {
		return nil
	}
	out := make([]nodejsPaket, 0, len(pj_.Dependencies))
	for ad, s := range pj_.Dependencies {
		// npm: veya git: gibi non-semver spec'leri atla — OSV bunları bilmez
		if strings.HasPrefix(s, "npm:") || strings.HasPrefix(s, "git") ||
			strings.HasPrefix(s, "workspace:") || strings.HasPrefix(s, "link:") ||
			strings.HasPrefix(s, "file:") || strings.HasPrefix(s, "http") {
			continue
		}
		v := semverIlk(s)
		if v == "" {
			continue
		}
		out = append(out, nodejsPaket{Ad: ad, Surum: v})
	}
	return out
}

// semverReg — X.Y[.Z][-pre] kalıbı. Range operatörlerini içermez.
var semverReg = regexp.MustCompile(`\d+\.\d+(?:\.\d+)?(?:-[0-9A-Za-z.-]+)?`)

func semverIlk(s string) string {
	return semverReg.FindString(s)
}

// NodejsKurulumlariBul — tüm domainlerdeki Node.js uygulamalarını topla.
func NodejsKurulumlariBul(ctx context.Context, db *sql.DB) ([]nodejsKurulum, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, sistem_kullanici, alan_adi FROM domains ORDER BY alan_adi`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []nodejsKurulum
	for rows.Next() {
		var id int64
		var sk, ad string
		if err := rows.Scan(&id, &sk, &ad); err != nil {
			continue
		}
		if k := nodejsBul(ctx, sk, ad); k != nil {
			k.DomainID = id
			k.AlanAdi = ad
			out = append(out, *k)
		}
	}
	return out, rows.Err()
}

// nodejsTara — Node kurulumları için OSV sorgusu + kayıt.
//
// Dönüş: (bulguSayisi, feedOK). feedOK = "OSV'den EN AZ BİR başarılı yanıt aldık".
// Solma temizliği bu bayrağa bakar — feed hiç çalışmadıysa (network outage)
// eski bulgular SİLİNMEZ, yoksa dashboard sessizce '0 kritik' gösterir.
func nodejsTara(ctx context.Context, db *sql.DB, k *nodejsKurulum) (int, bool) {
	n := 0
	feedOK := false
	for _, p := range k.Paketler {
		if ctx.Err() != nil {
			return n, feedOK
		}
		zaflar, err := OSVSorgula(ctx, OSVEcosysNPM, p.Ad, p.Surum)
		if err != nil {
			// Feed hatası tek paketi atlar, bayrağı DA true'ya çekme
			continue
		}
		// Hatasız yanıt (0 zafiyet olsa bile) = feed sağlıklı
		feedOK = true
		if len(zaflar) == 0 {
			continue
		}
		for _, z := range zaflar {
			if _, e := db.ExecContext(ctx,
				`INSERT INTO cp_websec_findings
				   (domain_id, app_type, install_path, package_name, installed_version,
				    cve_id, severity, cvss, title, fixed_in, source, first_seen, last_seen)
				 VALUES (?, 'nodejs', ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(), NOW())
				 ON DUPLICATE KEY UPDATE
				   installed_version=VALUES(installed_version),
				   severity=VALUES(severity), cvss=VALUES(cvss),
				   title=VALUES(title), fixed_in=VALUES(fixed_in),
				   last_seen=NOW()`,
				k.DomainID, k.Yol+"#npm:"+p.Ad, p.Ad, p.Surum,
				z.CVE, z.Severity, sqlNullFloat(z.CVSS), z.Title, z.FixedIn, z.Source,
			); e == nil {
				n++
			}
		}
		// Rate limit — OSV kamuya açık, kibarca dur.
		select {
		case <-time.After(feedIsteğiBekleme):
		case <-ctx.Done():
			return n, feedOK
		}
	}
	return n, feedOK
}
