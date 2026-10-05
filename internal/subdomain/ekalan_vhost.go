package subdomain

// ekalan_vhost.go — ek alan adı vhost üreticisi (TEK üretici), açılış onarımı (heal)
// ve hesap askısı bağlantısı.
//
// Üretici alt alanla ORTAK şablonu (renderSubVhost) kullanır: ACME challenge konumu
// (root-sahipli /var/www/acme + token biçimi), güvenlik başlıkları, PHP bloğu, 404
// sayfası orada tek yerde durur; burada ikinci bir şablon YOKTUR. Ek alan adına özgü
// tek fark: server_name "<alan> www.<alan>" ve docroot yolu.

import (
	"context"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"girginospanel/internal/dns"
	"girginospanel/internal/hesaplar"
	"girginospanel/internal/provisioner"
)

// ekVhostYenidenKur — testler sahteyle değiştirir (askı/heal sınamaları nginx'e dokunmadan).
var ekVhostYenidenKur = ekVhostYenidenKurDB

// ekSunucuAdlari — server_name değeri: apex + www.
func ekSunucuAdlari(ad string) string { return ad + " www." + ad }

// ekVhostGovde — askıda OLMAYAN hesabın ek alan adı vhost gövdesi.
func ekVhostGovde(sk, ad, php string) (string, error) {
	socket, err := provisioner.PHPSocketFor(sk, php)
	if err != nil {
		return "", err
	}
	crt, key := certYolu(sk, ad)
	o := subVhostOpts{TamAd: ad, DocRoot: ekDocroot(sk, ad), Socket: socket, Backend: "php-fpm",
		SSL: dosyaVar(crt) && dosyaVar(key), Crt: crt, Key: key, N: subNginxDefaults()}
	body := renderSubVhost(o)
	// www takma adı: renderSubVhost TamAd'ı günlük dosyası adında da kullanır, bu yüzden
	// takma ad yalnız server_name satırlarına eklenir (her iki sunucu bloğu).
	return strings.ReplaceAll(body, "server_name "+ad+";", "server_name "+ekSunucuAdlari(ad)+";"), nil
}

// ekVhostYenidenKurDB — ek alan vhost'unun TEK üreticisi/yazıcısı. Render kilidi
// altında: hesap askıda mı (tek kaynak), gövde, yaz, `nginx -t` (başarısızsa ESKİ
// dosya geri konur), reload (başarısızsa geri alınır). Askı okunamazsa vhost'a
// DOKUNULMAZ (ne açar ne kapatır).
func ekVhostYenidenKurDB(ctx context.Context, db *sql.DB, ekID int64) error {
	birak := provisioner.RenderKilidiAl()
	defer birak()

	var sk, ad, php string
	if err := db.QueryRowContext(ctx,
		`SELECT d.sistem_kullanici, e.alan_adi, COALESCE(d.php_surum,'8.3')
		   FROM ek_alan_adlari e JOIN domains d ON d.id=e.domain_id WHERE e.id=?`, ekID).Scan(&sk, &ad, &php); err != nil {
		return fmt.Errorf("ek alan adı okunamadı: %w", err)
	}
	if !reEkSK.MatchString(sk) || !ekAdGuvenli(ad) {
		return errors.New("ek alan adı kaydı güvenli değil — vhost yazılmadı")
	}
	askida, aerr := hesaplar.HesapAskida(ctx, db, sk)
	if aerr != nil {
		return fmt.Errorf("ek alan vhost'u DEGISTIRILMEDI (%s): %w", ad, aerr)
	}
	var body string
	if askida {
		crt, key := certYolu(sk, ad)
		if !dosyaVar(crt) || !dosyaVar(key) {
			crt, key = "", ""
		}
		b, err := provisioner.AskiVhostGovde(ad, ekSunucuAdlari(ad), crt, key)
		if err != nil {
			return err
		}
		body = b
	} else {
		b, err := ekVhostGovde(sk, ad, php)
		if err != nil {
			return err
		}
		body = b
	}
	conf := ekConfPath(sk, ad)
	eskiB, _ := os.ReadFile(conf)
	geriAl := func() {
		if len(eskiB) > 0 {
			_ = os.WriteFile(conf, eskiB, 0o644)
		} else {
			_ = os.Remove(conf)
		}
	}
	if err := os.WriteFile(conf, []byte(body), 0o644); err != nil {
		return err
	}
	_, _ = komut("restorecon", conf)
	if out, e := komut("nginx", "-t"); e != nil {
		geriAl()
		_, _ = komut("systemctl", "reload", "nginx")
		return &nginxHata{strings.TrimSpace(string(out))}
	}
	if out, e := komut("systemctl", "reload", "nginx"); e != nil {
		geriAl()
		return fmt.Errorf("nginx reload BASARISIZ, %s GERI ALINDI: %s", conf, strings.TrimSpace(string(out)))
	}
	return nil
}

// ekIDler — hesabın ek alan adı kimlikleri (tablo yoksa boş).
func ekIDler(ctx context.Context, db *sql.DB, domainID int64) ([]int64, error) {
	rows, err := db.QueryContext(ctx, `SELECT id FROM ek_alan_adlari WHERE domain_id=? ORDER BY id`, domainID)
	if err != nil {
		if tabloYok(err) {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out, rows.Err()
}

// EkAlanVhostlariniYenidenKur — hesabın TÜM ek alan adı vhost'larını tek üreticiden
// yeniden kurar. Hesap askısı (AltAlanVhostlariniYenidenKur içinden) ve hesap PHP
// sürümü değişimi çağırır. Liste render kilidi ALTINDA okunur (alt alan ile aynı
// yarış kapısı). Biri düşse ötekiler denenir; hatalar birlikte döner.
func EkAlanVhostlariniYenidenKur(ctx context.Context, db *sql.DB, domainID int64) error {
	birak := provisioner.RenderKilidiAl()
	idler, err := ekIDler(ctx, db, domainID)
	birak()
	if err != nil {
		return fmt.Errorf("ek alan adları okunamadı: %w", err)
	}
	var hatalar []error
	for _, id := range idler {
		if err := ekVhostYenidenKur(ctx, db, id); err != nil {
			hatalar = append(hatalar, fmt.Errorf("ek alan adı #%d: %w", id, err))
		}
	}
	return errors.Join(hatalar...)
}

// HealEkAlanlar — açılış onarımı (idempotent): her ek alan adı için (1) vhost dosyası
// yok ya da beklenenden farklıysa yeniden yaz (yedek geri yükleme / elle silinme /
// PHP soketi değişimi), (2) DNS kaydı hiç yoksa tohumla, (3) bölge dosyası yoksa yaz.
// Sapma yoksa nginx'e DOKUNMAZ (reload yok).
func HealEkAlanlar(db *sql.DB, varsayilanIPv4 string) {
	if db == nil {
		return
	}
	rows, err := db.Query(`SELECT e.id, e.alan_adi, d.sistem_kullanici, COALESCE(d.php_surum,'8.3'),
	        COALESCE(d.ipv4,''), COALESCE(d.reseller_id,0)
	   FROM ek_alan_adlari e JOIN domains d ON d.id=e.domain_id`)
	if err != nil {
		if !tabloYok(err) {
			log.Printf("ek alan heal: liste okunamadı: %v", err)
		}
		return
	}
	type satir struct {
		id              int64
		ad, sk, php, ip string
		rid             int64
	}
	var liste []satir
	for rows.Next() {
		var s satir
		if rows.Scan(&s.id, &s.ad, &s.sk, &s.php, &s.ip, &s.rid) == nil {
			liste = append(liste, s)
		}
	}
	rows.Close()
	ctx := context.Background()
	duzeltilen := 0
	for _, s := range liste {
		if !reEkSK.MatchString(s.sk) || !ekAdGuvenli(s.ad) {
			log.Printf("ek alan heal: güvensiz kayıt atlandı (id=%d)", s.id)
			continue
		}
		if ekDocrootTasimaBekliyor(s.sk, s.ad) {
			log.Printf("ek alan heal: %s klasörü eski yerde (taşıma atlanmış) — vhost'a dokunulmadı", s.ad)
			continue
		}
		// Kiracı domains klasörünü/alan klasörünü sildiyse (vhost 404): boş docroot'u yeniden
		// kur. Hesap ev dizini yoksa (hesap diskte kaldırılmış) dokunma.
		if fi, e := os.Stat(evKoku + "/" + s.sk); e == nil && fi.IsDir() && !dosyaVar(ekDocroot(s.sk, s.ad)) {
			if _, _, e := ekDocrootHazirla(s.sk, s.ad); e != nil {
				log.Printf("ek alan heal: %s docroot yeniden kurulamadı: %v", s.ad, e)
			} else {
				log.Printf("✓ ek alan heal: %s eksik docroot yeniden oluşturuldu", s.ad)
			}
		}
		if ekVhostSapmasi(ctx, db, s.sk, s.ad, s.php) {
			if e := ekVhostYenidenKur(ctx, db, s.id); e != nil {
				log.Printf("ek alan heal: %s vhost yeniden kurulamadı: %v", s.ad, e)
			} else {
				duzeltilen++
			}
		}
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ek_dns_kayitlari WHERE ek_alan_id=?`, s.id).Scan(&n); err == nil && n == 0 {
			ip := s.ip
			if ip == "" {
				ip = varsayilanIPv4
			}
			if _, e := ekZoneTohum(ctx, db, s.id, s.ad, ip, s.rid); e != nil {
				log.Printf("ek alan heal: %s DNS tohumu: %v", s.ad, e)
			}
		}
		if _, err := os.Stat(filepath.Join(dns.ZoneDir, s.ad+".zone")); err != nil {
			if e := ekZoneYaz(ctx, db, s.id); e != nil {
				log.Printf("ek alan heal: %s bölge dosyası: %v", s.ad, e)
			}
		}
	}
	if duzeltilen > 0 {
		log.Printf("✓ ek alan adı vhost sapması düzeltildi: %d", duzeltilen)
	}
}

// ekVhostSapmasi — diskteki vhost beklenenle tutmuyor mu (yok ya da farklı)?
// Beklenen gövde okunamıyorsa (askı durumu, PHP soketi) SAPMA SAYILMAZ: belirsizlikte
// çalışan vhost'a dokunulmaz.
func ekVhostSapmasi(ctx context.Context, db *sql.DB, sk, ad, php string) bool {
	askida, err := hesaplar.HesapAskida(ctx, db, sk)
	if err != nil {
		return false
	}
	var beklenen string
	if askida {
		crt, key := certYolu(sk, ad)
		if !dosyaVar(crt) || !dosyaVar(key) {
			crt, key = "", ""
		}
		b, e := provisioner.AskiVhostGovde(ad, ekSunucuAdlari(ad), crt, key)
		if e != nil {
			return false
		}
		beklenen = b
	} else {
		b, e := ekVhostGovde(sk, ad, php)
		if e != nil {
			return false
		}
		beklenen = b
	}
	mevcut, err := os.ReadFile(ekConfPath(sk, ad))
	if err != nil {
		return true
	}
	return string(mevcut) != beklenen
}

// certBitis — sertifika dosyasının bitiş tarihi (YYYY-MM-DD); okunamazsa "".
func certBitis(crtYol string) string {
	b, err := os.ReadFile(crtYol)
	if err != nil {
		return ""
	}
	for {
		var blk *pem.Block
		blk, b = pem.Decode(b)
		if blk == nil {
			return ""
		}
		if blk.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(blk.Bytes)
		if err != nil {
			return ""
		}
		return c.NotAfter.UTC().Format("2006-01-02")
	}
}

// ekGunlukler — dizin altında ad ile başlayan (logrotate arşivleri dahil) günlükler.
// ad alan adı + ".access.log" biçimindedir; "<ad>*" deseni komşu alan adını
// (shop.com → shop.com.tr) yakalardı: yalnız tam ad ya da ad+"-"/"." + rakam/tarih
// arşiv eki kabul edilir.
func ekGunlukler(dizin, ad string) []string {
	m, _ := filepath.Glob(filepath.Join(dizin, ad+"*"))
	var out []string
	for _, y := range m {
		kalan := strings.TrimPrefix(filepath.Base(y), ad)
		if kalan == "" || strings.HasPrefix(kalan, "-") || strings.HasPrefix(kalan, ".") {
			out = append(out, y)
		}
	}
	return out
}
