package websec

// PHP Composer tarayıcı (Laravel/Symfony/Drupal 8+ vb.) — composer.lock
// dosyasını parse eder, kurulu paketleri OSV.dev (Packagist ecosystem) ile
// eşler.
//
// 🔴 composer.lock genelde public_html ÜSTÜNDE (root dizinde). Ama gPanel
// akışında tenant'ın document root'u zaten public_html. Bazı framework'lerde
// (Laravel) public_html sadece /public alt-dizini; composer.lock bir üstte.
// Bu yüzden İKİ konumu deniyoruz.
//
// 🔴 dev bağımlılıkları (packages-dev) tarama DIŞI — üretim etkisi yok.

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type phpKurulum struct {
	DomainID int64
	AlanAdi  string
	Yol      string
	Paketler []phpPaket
}

type phpPaket struct {
	Ad    string // "vendor/package"
	Surum string // "v1.2.3"
}

// phpBul — composer.lock arar. Sırayla:
//   1. /home/<sk>/public_html/composer.lock
//   2. /home/<sk>/composer.lock             (Laravel gibi framework kalıbı)
func phpBul(_ context.Context, sk, _ string) *phpKurulum {
	adaylar := []string{
		filepath.Join("/home", sk, "public_html", "composer.lock"),
		filepath.Join("/home", sk, "composer.lock"),
	}
	for _, p := range adaylar {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		pk := composerLockOku(p)
		if len(pk) > 0 {
			return &phpKurulum{Yol: filepath.Dir(p), Paketler: pk}
		}
	}
	return nil
}

func composerLockOku(p string) []phpPaket {
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var lock struct {
		Packages []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(raw, &lock); err != nil {
		return nil
	}
	out := make([]phpPaket, 0, len(lock.Packages))
	for _, p := range lock.Packages {
		if p.Name == "" || p.Version == "" {
			continue
		}
		// composer versionları "v1.2.3", "1.2.3", "dev-master" olabilir. OSV
		// "dev-*" branch'lerini bilmez → onları atla.
		if len(p.Version) >= 4 && p.Version[:4] == "dev-" {
			continue
		}
		v := p.Version
		if v[0] == 'v' {
			v = v[1:]
		}
		out = append(out, phpPaket{Ad: p.Name, Surum: v})
	}
	return out
}

// PhpKurulumlariBul — tüm domainlerdeki PHP Composer uygulamalarını topla.
func PhpKurulumlariBul(ctx context.Context, db *sql.DB) ([]phpKurulum, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, sistem_kullanici, alan_adi FROM domains ORDER BY alan_adi`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []phpKurulum
	for rows.Next() {
		var id int64
		var sk, ad string
		if err := rows.Scan(&id, &sk, &ad); err != nil {
			continue
		}
		if k := phpBul(ctx, sk, ad); k != nil {
			k.DomainID = id
			k.AlanAdi = ad
			out = append(out, *k)
		}
	}
	return out, rows.Err()
}

// phpTara — Composer kurulumları için OSV sorgusu + kayıt.
// Dönüş: (bulguSayisi, feedOK) — bkz. nodejsTara yorumu.
func phpTara(ctx context.Context, db *sql.DB, k *phpKurulum) (int, bool) {
	n := 0
	feedOK := false
	for _, p := range k.Paketler {
		if ctx.Err() != nil {
			return n, feedOK
		}
		zaflar, err := OSVSorgula(ctx, OSVEcosysPackagist, p.Ad, p.Surum)
		if err != nil {
			continue
		}
		feedOK = true
		if len(zaflar) == 0 {
			continue
		}
		for _, z := range zaflar {
			if _, e := db.ExecContext(ctx,
				`INSERT INTO cp_websec_findings
				   (domain_id, app_type, install_path, package_name, installed_version,
				    cve_id, severity, cvss, title, fixed_in, source, first_seen, last_seen)
				 VALUES (?, 'php-composer', ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(), NOW())
				 ON DUPLICATE KEY UPDATE
				   installed_version=VALUES(installed_version),
				   severity=VALUES(severity), cvss=VALUES(cvss),
				   title=VALUES(title), fixed_in=VALUES(fixed_in),
				   last_seen=NOW()`,
				k.DomainID, k.Yol+"#composer:"+p.Ad, p.Ad, p.Surum,
				z.CVE, z.Severity, sqlNullFloat(z.CVSS), z.Title, z.FixedIn, z.Source,
			); e == nil {
				n++
			}
		}
		select {
		case <-time.After(feedIsteğiBekleme):
		case <-ctx.Done():
			return n, feedOK
		}
	}
	return n, feedOK
}
