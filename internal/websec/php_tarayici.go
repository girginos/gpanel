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
	"path/filepath"
	"time"
)

type phpKurulum struct {
	DomainID int64
	EkAlanID int64
	AlanAdi  string
	Yol      string
	Paketler []phpPaket
}

type phpPaket struct {
	Ad    string // "vendor/package"
	Surum string // "v1.2.3"
}

// guvenliKullanici (defense-in-depth): sistem kullanıcı adı yalnız
// harf/rakam/altçizgi/tire olabilir ('/', '.', '..' yasak) — CWE-22 savunması.
func guvenliKullanici(sk string) bool {
	if sk == "" || len(sk) > 64 {
		return false
	}
	for _, r := range sk {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// phpBul — composer.lock arar. Ana alan adı için sırayla:
//  1. /home/<sk>/public_html/composer.lock
//  2. /home/<sk>/composer.lock             (Laravel gibi framework kalıbı)
//
// Ek alan adı için: docroot/composer.lock, sonra alan dizini (docroot'un bir üstü,
// ek alan klasörü/<alan>/composer.lock). Ek alan ASLA /home/<sk>/composer.lock'a
// bakmaz: o ana alan adınındır (çift sayım).
func phpBul(_ context.Context, h hedef) *phpKurulum {
	// Güvenlik (defense-in-depth): sk = sistem_kullanici (DB) yol içine gömülüyor
	// ("/home/<sk>/..."). gPanel sistem kullanıcıları harf/rakam/altçizgi/tire —
	// '/', '.', '..' yasak → CWE-22 FP savunma katmanı.
	if !guvenliKullanici(h.SK) {
		return nil
	}
	type aday struct{ kok, yol string }
	var adaylar []aday
	if h.EkAlanID > 0 {
		adaylar = append(adaylar, aday{h.okumaKok(), h.Docroot})
		// Alan dizini: gerçek docroot'un üstü, ama yalnız hâlâ ek alan klasörü/<alan> ise
		// (docroot başka yere bağlıysa ekDocrootDogrula zaten reddetmiştir).
		adaylar = append(adaylar, aday{filepath.Dir(h.okumaKok()), filepath.Dir(h.Docroot)})
	} else {
		adaylar = append(adaylar,
			aday{h.Docroot, h.Docroot},
			aday{filepath.Join(evKoku, h.SK), filepath.Join(evKoku, h.SK)})
	}
	for _, a := range adaylar {
		if !kokDosyaVarMi(a.kok, "composer.lock") {
			continue
		}
		pk := composerLockOku(a.kok, "composer.lock")
		if len(pk) > 0 {
			return &phpKurulum{Yol: a.yol, Paketler: pk}
		}
	}
	return nil
}

func composerLockOku(kok, ad string) []phpPaket {
	raw, err := kokDosyaOku(kok, ad)
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

// PhpKurulumlariBul — domainlerdeki PHP Composer uygulamalarını topla (ids boş =
// tüm domainler; dolu = yalnız o domainler, bkz. kesifSorgusu).
func PhpKurulumlariBul(ctx context.Context, db *sql.DB, ids []int64) ([]phpKurulum, error) {
	hedefler, err := tarananHedefler(ctx, db, ids)
	if err != nil {
		return nil, err
	}
	var out []phpKurulum
	for _, h := range hedefler {
		if k := phpBul(ctx, h); k != nil {
			k.DomainID = h.DomainID
			k.EkAlanID = h.EkAlanID
			k.AlanAdi = h.AlanAdi
			out = append(out, *k)
		}
	}
	return out, nil
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
				    cve_id, severity, cvss, title, fixed_in, source, first_seen, last_seen, ek_alan_id)
				 VALUES (?, 'php-composer', ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(), NOW(), NULLIF(?, 0))
				 ON DUPLICATE KEY UPDATE
				   ek_alan_id=VALUES(ek_alan_id),
				   installed_version=VALUES(installed_version),
				   severity=VALUES(severity), cvss=VALUES(cvss),
				   title=VALUES(title), fixed_in=VALUES(fixed_in),
				   last_seen=NOW()`,
				k.DomainID, k.Yol+"#composer:"+p.Ad, p.Ad, p.Surum,
				z.CVE, z.Severity, sqlNullFloat(z.CVSS), z.Title, z.FixedIn, z.Source, k.EkAlanID,
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
