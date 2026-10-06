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
//	uyumsuz + Laravel kaydı yok + servis dizini YOK       → düzeltilebilir (bayat web_root)
//	uyumsuz + Laravel kaydı var, artisan diskte YOK ve
//	  (servis dizini yok  YA DA  servis == <app>/public)  → düzeltilebilir (uygulama silinmiş);
//	  bayat Laravel kaydı/cron/kuyruk da temizlenir
//	uyumsuz + Laravel CANLI (artisan var / belirsiz) ya da kurulum/dağıtım SÜRÜYOR
//	  → düzeltilemez (çağıran 409 döner) — servis dizini yok olsa bile
//	uyumsuz + servis dizini var ve ilişkisiz (elle/bilinçli web_root; ör. eski Laravel kaydı
//	  artisan'sız ama web_root başka dizinde) → düzeltilemez (409): bilinçli ayar ezilmez
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
	"sync"

	"girginospanel/internal/provisioner"
)

// RerenderFn: vhost yeniden kurma (nginx -t + geri alma İÇERİDE). Testler sahteyle
// değiştirir; üretimde değer SABİTTİR.
var RerenderFn = provisioner.RerenderVhost

// Karar: ön kontrolün sonucu.
type Karar struct {
	Servis         string // şu an servis edilen (güvenli çözülmüş, mutlak) kök
	Stored         string // karar anındaki HAM domains.web_root (Duzelt'in compare-and-swap beklentisi)
	Uyumlu         bool   // hedef bu kökten sunulur mu
	Duzeltilebilir bool   // Uyumlu değilse: web_root güvenle varsayılana alınabilir
	LaravelBayat   bool   // düzeltmede bayat cp_laravel_apps satırı da temizlenmeli (artisan yok)
	LaravelMesgul  bool   // Laravel kurulumu/dağıtımı sürüyor: düzeltme YOK (çağıran 409 döner)
	Sebep          string // "web_root_yok" | "laravel_silinmis" | "" (günlük/test için)
}

// ErrDegisti: karar ile yazım arasında web_root başkası tarafından değiştirildi; ezilmedi.
var ErrDegisti = errors.New("belge kökü bu sırada değişti")

// kilitler: domain başına Duzelt/GeriAl serileştirmesi. Kilit SIRASI: laravel.kilitle(id)
// → belgekok kilidi (laravel.KaydiKaldir → GeriAl). Bu paket laravel kilidini ALMAZ ve
// kilit altında dış kilit beklemez → ters yön yok, kendi kendine kilitlenme yok
// (Duzelt/GeriAl dışarıya kilitsiz iç işlevi çağırır; kilit yeniden alınmaz).
var kilitler sync.Map

func kilitle(id int64) func() {
	m, _ := kilitler.LoadOrStore(id, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
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

// laravelAppDizin: kayıtlı app_root (eve göreli) → mutlak uygulama dizini. Ev dışına çıkıyorsa
// ya da gizli/garip ise güvenle çözülemez (ok=false → çağıran "canlı" sayar).
func laravelAppDizin(base, appRoot string) (string, bool) {
	home := filepath.Dir(filepath.Clean(base))
	appRoot = strings.Trim(strings.TrimSpace(appRoot), "/")
	if appRoot == "" {
		appRoot = filepath.Base(filepath.Clean(base))
	}
	dir := filepath.Clean(filepath.Join(home, appRoot))
	if dir != home && !strings.HasPrefix(dir, home+"/") {
		return "", false
	}
	return dir, true
}

// artisanYok: artisan GERÇEKTEN diskte yok mu? Yalnız ErrNotExist kesin kanıttır; başka her
// hata/belirsizlik "canlı" sayılır (muhafazakâr: yanlış yere dokunmak yerine 409).
func artisanYok(appDir string) bool {
	_, err := os.Lstat(filepath.Join(appDir, "artisan"))
	return errors.Is(err, os.ErrNotExist)
}

// Degerlendir: domain id için (base = <ev>/<sk>/public_html) hedefin servis edilip
// edilmediğine karar verir. domains.web_root okunamazsa uyumlu kabul edilir (ön kontrol
// mevcut akışı kırmaz); cp_laravel_apps okunamazsa (satır yokluğu dışında hata) MUHAFAZAKÂR:
// düzeltilemez — belirsizlikte bilinçli ayar ezilmez.
func Degerlendir(ctx context.Context, db *sql.DB, id int64, base, hedef string) Karar {
	var stored string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(web_root,'') FROM domains WHERE id=?`, id).Scan(&stored); err != nil {
		log.Printf("belgekok: web_root okunamadı (domain=%d): %v — ön kontrol atlandı", id, err)
		return Karar{Servis: filepath.Clean(base), Uyumlu: true}
	}
	servis := provisioner.GuvenliWebRootTaban(base, stored)
	k := Karar{Servis: servis, Stored: stored, Uyumlu: Uyumlu(servis, hedef)}
	if k.Uyumlu {
		return k
	}
	_, serr := os.Lstat(servis)
	servisYok := errors.Is(serr, os.ErrNotExist)

	var appRoot, durum string
	lerr := db.QueryRowContext(ctx, `SELECT COALESCE(app_root,''), COALESCE(son_deploy_durum,'') FROM cp_laravel_apps WHERE domain_id=?`, id).Scan(&appRoot, &durum)
	switch {
	case errors.Is(lerr, sql.ErrNoRows):
		// Laravel kaydı yok: yalnız servis dizini diskte YOKSA bayat; varsa elle/bilinçli ayar.
		if servisYok {
			k.Duzeltilebilir, k.Sebep = true, "web_root_yok"
		}
		return k
	case lerr != nil:
		log.Printf("belgekok: cp_laravel_apps okunamadı (domain=%d): %v — düzeltme yapılmaz", id, lerr)
		return k
	}
	// Laravel kaydı VAR.
	if durum == "kuruluyor" || durum == "calisiyor" {
		k.LaravelMesgul = true // kurulum/dağıtım sürerken kayıt ve belge kökü dokunulmaz
		return k
	}
	appDir, ok := laravelAppDizin(base, appRoot)
	if !ok || !artisanYok(appDir) {
		return k // Laravel CANLI (ya da belirsiz): servis dizini yok olsa bile düzeltme yok
	}
	// Uygulama gerçekten silinmiş. Belge kökü onunla İLİŞKİLİYSE (== <app>/public) ya da zaten
	// diskte yoksa düzeltilir; başka bir dizine bilinçli ayarlıysa (ör. landing) DOKUNULMAZ.
	if servisYok || filepath.Clean(servis) == filepath.Join(appDir, "public") {
		k.Duzeltilebilir, k.LaravelBayat, k.Sebep = true, true, "laravel_silinmis"
		if servisYok {
			k.Sebep = "web_root_yok"
		}
	}
	return k
}

// Duzelt: web_root'u domain varsayılanına (base) alır ve vhost'u yeniden kurar. beklenen =
// karar anındaki HAM web_root: yazım compare-and-swap'tır (kurulum 20-90 sn sürer; bu sürede
// başkası belge kökünü değiştirdiyse EZİLMEZ → ErrDegisti). Render hatasında (nginx -t geri
// aldı) DB yalnız hâlâ bizim yazdığımız değerse eski değere döner. sk, satırın sahibini doğrular.
func Duzelt(ctx context.Context, db *sql.DB, id int64, sk, base, beklenen string) error {
	defer kilitle(id)()
	return duzeltKilitsiz(ctx, db, id, sk, base, beklenen)
}

func duzeltKilitsiz(ctx context.Context, db *sql.DB, id int64, sk, base, beklenen string) error {
	abs, err := provisioner.WebRootMutlakTaban(base, "")
	if err != nil {
		return err
	}
	var eski string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(web_root,'') FROM domains WHERE id=? AND sistem_kullanici=?`, id, sk).Scan(&eski); err != nil {
		return err
	}
	res, err := db.ExecContext(ctx, `UPDATE domains SET web_root=? WHERE id=? AND sistem_kullanici=? AND COALESCE(web_root,'')=?`, abs, id, sk, beklenen)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var simdi string
		if err := db.QueryRowContext(ctx, `SELECT COALESCE(web_root,'') FROM domains WHERE id=? AND sistem_kullanici=?`, id, sk).Scan(&simdi); err != nil {
			return err
		}
		if simdi == abs {
			return nil // zaten istenen değer
		}
		return ErrDegisti
	}
	if err := RerenderFn(db, id); err != nil {
		_, _ = db.ExecContext(context.WithoutCancel(ctx), `UPDATE domains SET web_root=? WHERE id=? AND sistem_kullanici=? AND web_root=?`, eski, id, sk, abs)
		return err
	}
	return nil
}

// GeriAl: Laravel uygulaması panelden kaldırılınca web_root'u varsayılana (public_html)
// döndürür — YALNIZ web_root tam olarak <app kökü>/public ise. Elle başka yere ayarlanmışsa
// dokunmaz (false, nil). appDir mutlak uygulama dizinidir.
func GeriAl(ctx context.Context, db *sql.DB, id int64, sk, base, appDir string) (bool, error) {
	defer kilitle(id)()
	var stored string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(web_root,'') FROM domains WHERE id=? AND sistem_kullanici=?`, id, sk).Scan(&stored); err != nil {
		return false, err
	}
	if strings.TrimSpace(stored) == "" || filepath.Clean(strings.TrimSpace(stored)) != filepath.Join(filepath.Clean(appDir), "public") {
		return false, nil
	}
	if filepath.Clean(stored) == filepath.Clean(base) {
		return false, nil
	}
	if err := duzeltKilitsiz(ctx, db, id, sk, base, stored); err != nil {
		return false, err
	}
	return true, nil
}
