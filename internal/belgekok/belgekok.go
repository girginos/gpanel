// Package belgekok: ana alan adının SERVİS EDİLEN belge kökü (domains.web_root) ile bir
// uygulamanın (WordPress) kurulum hedefi arasındaki uyum kontrolü ve güvenli düzeltme.
//
// Sorun (canlı yaşandı): alan adına önce Laravel kuruldu → web_root=<ev>/public_html/public
// + cp_laravel_apps satırı. Kullanıcı dosyaları panel dışından silip public_html köküne
// WordPress kurdu: nginx kökü hâlâ olmayan .../public → 404, php-fpm "Primary script
// unknown", panel hiçbir uyarı vermedi.
//
// Karar tablosu (Degerlendir):
//
//	hedef == servis ya da hedef servis kökünün ALTINDA   → uyumlu (site WordPress'i sunar)
//	uyumsuz + servis kökü diskte YOK                      → düzeltilebilir (bayat web_root)
//	uyumsuz + kayıtlı Laravel'in artisan'ı diskte YOK     → düzeltilebilir (uygulama silinmiş)
//	uyumsuz + servis kökü var ve (Laravel canlı ya da
//	  Laravel kaydı yok = web_root elle/bilinçli ayarlı)  → düzeltilemez (çağıran 409 döner)
//
// Düzeltme DAİMA web_root'u alan adı varsayılanına (public_html) alır — hedefin kendisine
// değil: hedef public_html/blog ise ana siteyi blog klasörüne daraltmak kullanıcının
// bilmediği bir yan etki olurdu; public_html hem hedefi hem öteki içeriği kapsar.
// Güvenlik: web_root yalnız kiracının KENDİ public_html'ine (provisioner.WebRootMutlakTaban:
// "..", mutlak yol, symlink, var olmayan dizin reddi) ve yalnız o hesabın domains satırına
// yazılır; vhost render hatasında (nginx -t) DB eski değere döner.
package belgekok

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"

	"girginospanel/internal/provisioner"
)

// RerenderFn: vhost yeniden kurma (nginx -t + geri alma İÇERİDE). Testler sahteyle
// değiştirir; üretimde değer SABİTTİR.
var RerenderFn = provisioner.RerenderVhost

// Karar: ön kontrolün sonucu.
type Karar struct {
	Servis         string // şu an servis edilen (güvenli çözülmüş, mutlak) kök
	Uyumlu         bool   // hedef bu kökten sunulur mu
	Duzeltilebilir bool   // Uyumlu değilse: web_root güvenle varsayılana alınabilir
	LaravelBayat   bool   // düzeltmede bayat cp_laravel_apps satırı da temizlenmeli (artisan yok)
	Sebep          string // "web_root_yok" | "laravel_silinmis" | "" (günlük/test için)
}

// Uyumlu: hedef, servis kökünün kendisi ya da altında mı? (Üstünde ya da kardeşi ise site
// WordPress'i HİÇ sunmaz.)
func Uyumlu(servis, hedef string) bool {
	servis, hedef = filepath.Clean(servis), filepath.Clean(hedef)
	return hedef == servis || strings.HasPrefix(hedef, servis+"/")
}

// UrlYolu: hedefin site adresindeki yol parçası ("" = alan adı kökü). Uyumsuzsa "".
func UrlYolu(servis, hedef string) string {
	servis, hedef = filepath.Clean(servis), filepath.Clean(hedef)
	if hedef == servis || !strings.HasPrefix(hedef, servis+"/") {
		return ""
	}
	return strings.TrimPrefix(hedef, servis+"/")
}

// Goreli: kullanıcıya gösterilecek ev-göreli yol ("public_html/public"); mutlak yol sızmaz.
func Goreli(base, yol string) string {
	home := filepath.Dir(filepath.Clean(base))
	yol = filepath.Clean(yol)
	if strings.HasPrefix(yol, home+"/") {
		return strings.TrimPrefix(yol, home+"/")
	}
	return filepath.Base(yol)
}

// laravelArtisanYok: kayıtlı Laravel uygulamasının artisan'ı GERÇEKTEN diskte yok mu?
// Yalnız "yok" (ErrNotExist) kesin kanıttır; başka her hata/belirsizlik "canlı" sayılır
// (muhafazakâr: yanlış yere silmek yerine 409). app_root eve göreli ve ev içinde
// kalmalı; dışına çıkıyorsa güvenle değerlendirilemez → canlı say.
func laravelArtisanYok(base, appRoot string) bool {
	home := filepath.Dir(filepath.Clean(base))
	appRoot = strings.Trim(strings.TrimSpace(appRoot), "/")
	if appRoot == "" {
		appRoot = filepath.Base(filepath.Clean(base))
	}
	dir := filepath.Clean(filepath.Join(home, appRoot))
	if dir != home && !strings.HasPrefix(dir, home+"/") {
		return false
	}
	_, err := os.Lstat(filepath.Join(dir, "artisan"))
	return errors.Is(err, os.ErrNotExist)
}

// Degerlendir: domain id için (base = <ev>/<sk>/public_html) hedefin servis edilip
// edilmediğine karar verir. DB okuma hatasında uyumlu kabul edilir (ön kontrol mevcut
// akışı kırmaz; yeni davranış yalnız kesin kanıtla devreye girer).
func Degerlendir(ctx context.Context, db *sql.DB, id int64, base, hedef string) Karar {
	var stored string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(web_root,'') FROM domains WHERE id=?`, id).Scan(&stored); err != nil {
		log.Printf("belgekok: web_root okunamadı (domain=%d): %v — ön kontrol atlandı", id, err)
		return Karar{Servis: filepath.Clean(base), Uyumlu: true}
	}
	servis := provisioner.GuvenliWebRootTaban(base, stored)
	k := Karar{Servis: servis, Uyumlu: Uyumlu(servis, hedef)}
	if k.Uyumlu {
		return k
	}
	// (1) servis edilen dizin diskte yok → bayat web_root (kullanıcı dosyaları sildi).
	if _, err := os.Lstat(servis); errors.Is(err, os.ErrNotExist) {
		k.Duzeltilebilir, k.Sebep = true, "web_root_yok"
	}
	// (2) kayıtlı Laravel uygulaması gerçekte silinmiş (artisan yok).
	var appRoot string
	lerr := db.QueryRowContext(ctx, `SELECT COALESCE(app_root,'') FROM cp_laravel_apps WHERE domain_id=?`, id).Scan(&appRoot)
	if lerr == nil && laravelArtisanYok(base, appRoot) {
		k.Duzeltilebilir, k.LaravelBayat = true, true
		if k.Sebep == "" {
			k.Sebep = "laravel_silinmis"
		}
	}
	return k
}

// Duzelt: web_root'u domain varsayılanına (base) alır ve vhost'u yeniden kurar; render
// hatasında (nginx -t geri aldı) DB'yi eski değere döndürür. sk, satırın sahibini doğrular
// (başka hesabın domains satırı değişmez).
func Duzelt(ctx context.Context, db *sql.DB, id int64, sk, base string) error {
	abs, err := provisioner.WebRootMutlakTaban(base, "")
	if err != nil {
		return err
	}
	var eski string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(web_root,'') FROM domains WHERE id=? AND sistem_kullanici=?`, id, sk).Scan(&eski); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `UPDATE domains SET web_root=? WHERE id=? AND sistem_kullanici=?`, abs, id, sk); err != nil {
		return err
	}
	if err := RerenderFn(db, id); err != nil {
		_, _ = db.ExecContext(context.WithoutCancel(ctx), `UPDATE domains SET web_root=? WHERE id=? AND sistem_kullanici=?`, eski, id, sk)
		return err
	}
	return nil
}

// GeriAl: Laravel uygulaması panelden kaldırılınca web_root'u varsayılana (public_html)
// döndürür — YALNIZ web_root tam olarak <app kökü>/public ise. Elle başka yere ayarlanmışsa
// dokunmaz (false, nil). appDir mutlak uygulama dizinidir.
func GeriAl(ctx context.Context, db *sql.DB, id int64, sk, base, appDir string) (bool, error) {
	var stored string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(web_root,'') FROM domains WHERE id=? AND sistem_kullanici=?`, id, sk).Scan(&stored); err != nil {
		return false, err
	}
	stored = strings.TrimSpace(stored)
	if stored == "" || filepath.Clean(stored) != filepath.Join(filepath.Clean(appDir), "public") {
		return false, nil
	}
	if filepath.Clean(stored) == filepath.Clean(base) {
		return false, nil
	}
	return true, Duzelt(ctx, db, id, sk, base)
}
