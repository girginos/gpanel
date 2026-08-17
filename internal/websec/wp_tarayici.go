package websec

// WordPress tarayıcı — panel'deki tüm WP kurulumlarını wp-cli ile keşfeder,
// her plugin/tema için wpvulnerability.net'i sorgular, kurulu sürüm zafiyet
// aralığındaysa cp_websec_findings'e yazar.
//
// 🔴 Kurulum tespiti panel'in `internal/wordpress` paketindeki mevcut TumListe
// desenini KULLANMIYOR — o paket HTTP handler'ı (h.TumListe(w,r)), scanner
// tarafında doğrudan çağrılamaz. Onun yerine domainleri DB'den okuyup
// aynı wp-cli komutlarını burada tekrar çalıştırıyoruz (izole, testable).
//
// 🔴 wp-cli process başına belirli tenant kullanıcısı altında koşar. Bu iş
// panel process'inin sudo yetkisini kullanır (systemd unit'te tanımlı).

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type WPKurulum struct {
	DomainID     int64
	SistemKullanici string
	AlanAdi      string
	Yol          string
	CoreSurum    string
	Eklentiler   []WPPaket
	Temalar      []WPPaket
}

type WPPaket struct {
	Slug   string
	Surum  string
	Aktif  bool
}

// wpKurulumBul — panel'deki bir tenant için wp-core kurulumlarını bul.
// wp-cli `wp find` kullanmıyor (root olabilir); onun yerine tenant'ın
// public_html'inde .wp-config.php arıyoruz.
//
// Alternatif: `wp cli find --path=/home/<user>` — daha kesin ama daha yavaş.
// Şimdilik "sadece public_html kökündeki WP" ele alıyoruz (subdirectory install
// kapsam dışı — sonraki iterasyonda genişletiriz).
func wpKurulumBul(ctx context.Context, sk, alanAdi string) []string {
	// Panel kanonik yol: /home/<sk>/public_html
	kok := filepath.Join("/home", sk, "public_html")
	// wp-config.php varlığı = WP kurulumu (kaba ama pratik).
	if _, err := exec.CommandContext(ctx, "test", "-f", filepath.Join(kok, "wp-config.php")).Output(); err == nil {
		return []string{kok}
	}
	return nil
}

// wpBinYol — wp-cli tam yolu. sudo PATH'i stripler, yalın `wp` çağırınca
// "command not found" alırız (canlıda gözlendi). Kaynak: `which wp` (canlı).
const wpBinYol = "/usr/local/bin/wp"

// wpCliJSON — belirli path'te wp-cli komutu çalıştır, JSON çıktısı alır.
// Tenant kullanıcısı altında (--allow-root YASAK).
func wpCliJSON(ctx context.Context, sk, path string, altKomut ...string) ([]byte, error) {
	args := []string{"-u", sk, wpBinYol, "--path=" + path, "--skip-plugins", "--skip-themes"}
	// --format=json sadece list-tarzı komutlarda çalışır; "core version"
	// düz metin döner. Komut tipine göre bayrağı ekleme kararı çağrı yerinde.
	args = append(args, altKomut...)
	c := exec.CommandContext(ctx, "sudo", args...)
	out, err := c.Output()
	if err != nil {
		return nil, fmt.Errorf("wp-cli %s: %w", strings.Join(altKomut, " "), err)
	}
	return out, nil
}

// wpKurulumIncele — versiyon + eklenti listesi + tema listesi.
func wpKurulumIncele(ctx context.Context, sk, path string) (*WPKurulum, error) {
	k := &WPKurulum{Yol: path, SistemKullanici: sk}

	// Core versiyon
	if out, err := wpCliJSON(ctx, sk, path, "core", "version"); err == nil {
		k.CoreSurum = strings.TrimSpace(string(out))
	}
	// Eklentiler — --format=json list komutu için gerekli (yoksa tablo döner)
	if out, err := wpCliJSON(ctx, sk, path, "plugin", "list", "--fields=name,status,version", "--format=json"); err == nil {
		var arr []struct{ Name, Status, Version string }
		if json.Unmarshal(out, &arr) == nil {
			for _, p := range arr {
				k.Eklentiler = append(k.Eklentiler, WPPaket{Slug: p.Name, Surum: p.Version, Aktif: p.Status == "active"})
			}
		}
	}
	// Temalar
	if out, err := wpCliJSON(ctx, sk, path, "theme", "list", "--fields=name,status,version", "--format=json"); err == nil {
		var arr []struct{ Name, Status, Version string }
		if json.Unmarshal(out, &arr) == nil {
			for _, t := range arr {
				k.Temalar = append(k.Temalar, WPPaket{Slug: t.Name, Surum: t.Version, Aktif: t.Status == "active"})
			}
		}
	}
	return k, nil
}

// WPKurulumlariBul — tüm domainleri gezip WP kurulumlarını topla.
func WPKurulumlariBul(ctx context.Context, db *sql.DB) ([]WPKurulum, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, sistem_kullanici, alan_adi FROM domains ORDER BY alan_adi`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var domainler []struct {
		id int64
		sk string
		ad string
	}
	for rows.Next() {
		var r struct {
			id int64
			sk string
			ad string
		}
		if err := rows.Scan(&r.id, &r.sk, &r.ad); err != nil {
			continue
		}
		domainler = append(domainler, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var out []WPKurulum
	for _, d := range domainler {
		select {
		case <-ctx.Done():
			return out, ctx.Err()
		default:
		}
		yollar := wpKurulumBul(ctx, d.sk, d.ad)
		for _, yol := range yollar {
			// wp-cli çağrıları için tenant başına 30 sn üst limit
			ktx, iptal := context.WithTimeout(ctx, 30*time.Second)
			k, err := wpKurulumIncele(ktx, d.sk, yol)
			iptal()
			if err != nil {
				continue
			}
			k.DomainID = d.id
			k.AlanAdi = d.ad
			out = append(out, *k)
		}
	}
	return out, nil
}

// paketKarsilastirveKaydet — bir paketin (plugin/theme) kurulu sürümünü
// zafiyet aralıklarıyla karşılaştır, eşleşenleri DB'ye UPSERT et.
func paketKarsilastirveKaydet(ctx context.Context, db *sql.DB, k *WPKurulum, tur string, paket WPPaket, zafiyetler []WPVulnZafiyet) int {
	if paket.Surum == "" {
		return 0
	}
	n := 0
	for _, z := range zafiyetler {
		if !VersiyonKapsanir(paket.Surum, z) {
			continue
		}
		_, err := db.ExecContext(ctx,
			`INSERT INTO cp_websec_findings
			   (domain_id, app_type, install_path, package_name, installed_version,
			    cve_id, severity, cvss, title, fixed_in, source, first_seen, last_seen)
			 VALUES (?, 'wordpress', ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(), NOW())
			 ON DUPLICATE KEY UPDATE
			   installed_version=VALUES(installed_version),
			   severity=VALUES(severity), cvss=VALUES(cvss),
			   title=VALUES(title), fixed_in=VALUES(fixed_in),
			   last_seen=NOW()`,
			k.DomainID, k.Yol+"#"+tur+":"+paket.Slug, paket.Slug, paket.Surum,
			z.CVE, z.Severity, sqlNullFloat(z.CVSS), z.Title, z.FixedIn, z.Source,
		)
		if err == nil {
			n++
		}
	}
	return n
}

func sqlNullFloat(f float64) any {
	if f <= 0 {
		return nil
	}
	return f
}
