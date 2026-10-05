package websec

// Ek (addon) alan adı desteği — Website Security Monitor.
//
// 🔴 HEDEF ANAHTARI: kuyruk, taranan-küme ve arayüz yoklaması TEK int64 anahtarla
// çalışır. Anahtar > 0 → ana alan adı (domains.id); anahtar < 0 → ek alan adı
// (-ek_alan_adlari.id). Böylece domain kuyruğunun tüm kuralları (tekrar tıklama
// kapısı, bayi soğuması 15 dk, kuyruk payı ≤50, "Domainlerimi Tara" 30 dk, parti
// boyutu) ek alana DEĞİŞMEDEN uygulanır; ana domain "Tara"sı yalnız ana docroot'u,
// ek alan "Tara"sı yalnız o ek alanı tarar. 0 geçersizdir.
//
// 🔴 DOCROOT: ek alan docroot'u subdomain.EkDocrootYolu ile üretilir (klasör adı TEK
// yerde: subdomain paketi; websec'te sabit yol YOK). Kiracı yazabilen bir dizindir: panel (root) bu dizinde
// dosya okurken sembolik bağ takip ETMEZ (os.Root: kök içinde kalma garantisi),
// docroot'un gerçek yolu hesabın ek alan klasörü/<alan>/ altında kalmak zorundadır (ana
// public_html'e ya da başka ek alana işaret eden bağ atlanır → çift sayım yok).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"girginospanel/internal/subdomain"
)

// evKoku — hesap ev dizinleri kökü (değişken: testler geçici dizine yönlendirir).
var evKoku = "/home"

// maxDosyaBoyut — okunacak kilit/manifest dosyası üst sınırı (kiracı dev dosya koyup
// paneli şişiremesin). package-lock.json gerçek hayatta onlarca MB'ı seyrek aşar.
const maxDosyaBoyut = 32 << 20

// hedef — taranacak tek bir kök dizin: ana alan adı ya da ek alan adı.
type hedef struct {
	DomainID int64 // ana domain (ek alanın sahibi hesap dahil)
	EkAlanID int64 // 0 = ana alan adı
	SK       string
	AlanAdi  string
	Docroot  string // kanonik (çözülmemiş) docroot; envanterde install_path öneki
	// gercek — ek alanda EvalSymlinks sonrası doğrulanmış docroot (okuma bunun üzerinden).
	gercek string
}

func (h hedef) anahtar() int64 {
	if h.EkAlanID > 0 {
		return -h.EkAlanID
	}
	return h.DomainID
}

// okumaKok — dosya okumanın yapılacağı kök (ek alanda doğrulanmış gerçek yol).
func (h hedef) okumaKok() string {
	if h.gercek != "" {
		return h.gercek
	}
	return h.Docroot
}

// anahtarAyir — hedef anahtarlarını ana domain id'leri ve ek alan id'lerine böler
// (0 atlanır). Tekilleştirir, sırayı korur.
func anahtarAyir(anahtarlar []int64) (dom, ek []int64) {
	gor := map[int64]bool{}
	for _, a := range anahtarlar {
		if a == 0 || gor[a] {
			continue
		}
		gor[a] = true
		if a > 0 {
			dom = append(dom, a)
		} else {
			ek = append(ek, -a)
		}
	}
	return dom, ek
}

// kapsamSQL — kısmi taramada findings/apps solma temizliğini YALNIZ taranan hedeflere
// sınırlar. Ana domain anahtarı yalnız ana satırları (ek_alan_id IS NULL) kapsar:
// ana domain taranınca ek alanların sağlam bulguları silinmez. Anahtar yoksa ("") tam
// tarama: süzgeç yok.
func kapsamSQL(anahtarlar []int64) (string, []any) {
	if len(anahtarlar) == 0 {
		return "", nil
	}
	dom, ek := anahtarAyir(anahtarlar)
	var parca []string
	var args []any
	if len(dom) > 0 {
		parca = append(parca, "(domain_id IN ("+yerTutucu(len(dom))+") AND ek_alan_id IS NULL)")
		for _, id := range dom {
			args = append(args, id)
		}
	}
	if len(ek) > 0 {
		parca = append(parca, "ek_alan_id IN ("+yerTutucu(len(ek))+")")
		for _, id := range ek {
			args = append(args, id)
		}
	}
	if len(parca) == 0 {
		return " AND 1=0", nil
	}
	return " AND (" + strings.Join(parca, " OR ") + ")", args
}

var reEkAlanAd = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)

// ekAdGuvenliMi — alan adı yol bileşeni olarak güvenli mi (DB'den gelse de savunma).
func ekAdGuvenliMi(ad string) bool {
	return len(ad) > 0 && len(ad) <= 253 && reEkAlanAd.MatchString(ad) && !strings.Contains(ad, "..")
}

func ekDocrootYolu(sk, ad string) string {
	return filepath.Clean(subdomain.EkDocrootYolu(evKoku, sk, ad))
}

// ekDocrootDogrula — ek alan docroot'unu doğrular. Dönüş: gerçek yol. Docroot yoksa
// (os.ErrNotExist) çağıran SESSİZCE geçer; hesap dışına/başka dizine çıkış hata döner
// (çağıran günlüğe sade yazar ve atlar).
func ekDocrootDogrula(sk, ad string) (string, error) {
	if !guvenliKullanici(sk) || !ekAdGuvenliMi(ad) {
		return "", errors.New("geçersiz hesap ya da alan adı")
	}
	ev := filepath.Join(evKoku, sk)
	gercekEv, err := filepath.EvalSymlinks(ev)
	if err != nil {
		return "", err
	}
	taban := filepath.Clean(subdomain.EkAlanTaban(evKoku, sk))
	if fi, e := os.Lstat(taban); e != nil {
		return "", e
	} else if fi.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("ek alan dizini sembolik bağlantı")
	}
	gercek, err := filepath.EvalSymlinks(ekDocrootYolu(sk, ad))
	if err != nil {
		return "", err
	}
	beklenen := filepath.Join(gercekEv, filepath.Base(taban), ad) + string(filepath.Separator)
	if !strings.HasPrefix(gercek+string(filepath.Separator), beklenen) {
		return "", errors.New("docroot hesabın ek alan dizini dışına işaret ediyor")
	}
	if fi, e := os.Stat(gercek); e != nil || !fi.IsDir() {
		return "", os.ErrNotExist
	}
	return gercek, nil
}

// kokDosyaOku — kok altındaki DÜZ bir dosyayı (ad: yol ayırıcısız) okur. os.Root ile
// kök dışına çıkış imkânsız; dosyanın kendisi sembolik bağ/aygıt/FIFO ise ya da
// maxDosyaBoyut'u aşıyorsa reddedilir. Derin dizin yürüyüşü YOK: yalnız sabit adlar.
func kokDosyaOku(kok, ad string) ([]byte, error) {
	if strings.ContainsAny(ad, `/\`) || ad == "" || ad == "." || ad == ".." {
		return nil, errors.New("geçersiz dosya adı")
	}
	r, err := os.OpenRoot(kok)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	fi, err := r.Lstat(ad)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s düz dosya değil", ad)
	}
	if fi.Size() > maxDosyaBoyut {
		return nil, fmt.Errorf("%s çok büyük", ad)
	}
	f, err := r.Open(ad)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxDosyaBoyut+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > maxDosyaBoyut {
		return nil, fmt.Errorf("%s çok büyük", ad)
	}
	return b, nil
}

// kokDosyaVarMi — kok altında düz dosya var mı (sembolik bağ YOK sayılır).
func kokDosyaVarMi(kok, ad string) bool {
	r, err := os.OpenRoot(kok)
	if err != nil {
		return false
	}
	defer r.Close()
	fi, err := r.Lstat(ad)
	return err == nil && fi.Mode().IsRegular()
}

// tarananHedefler — keşif: anahtarlar boşsa TÜM ana domainler + TÜM ek alanlar;
// doluysa yalnız o hedefler. Askıdaki hesaplar ayrıca süzülmez (ana domain keşfi de
// süzmüyor): ek alan ana domainle aynı kuralı alır. Docroot'u olmayan ek alan sessizce
// geçilir; hesap dışına işaret eden docroot günlüğe sade yazılıp atlanır.
func tarananHedefler(ctx context.Context, db *sql.DB, anahtarlar []int64) ([]hedef, error) {
	dom, ek := anahtarAyir(anahtarlar)
	tam := len(anahtarlar) == 0
	var out []hedef
	if tam || len(dom) > 0 {
		sorgu, args := kesifSorgusu(dom)
		rows, err := db.QueryContext(ctx, sorgu, args...) //nolint:gosec // G701: sorgu SABİT parçalar + yalnız "?" (kesifSorgusu); id'ler args ile bağlanır.
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var h hedef
			if err := rows.Scan(&h.DomainID, &h.SK, &h.AlanAdi); err != nil {
				continue
			}
			h.Docroot = filepath.Join(evKoku, h.SK, "public_html")
			out = append(out, h)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return nil, err
		}
	}
	if tam || len(ek) > 0 {
		sorgu := `SELECT e.id, e.domain_id, d.sistem_kullanici, e.alan_adi
		            FROM ek_alan_adlari e JOIN domains d ON d.id = e.domain_id`
		var args []any
		if len(ek) > 0 {
			sorgu += ` WHERE e.id IN (` + yerTutucu(len(ek)) + `)` //nolint:gosec // G202: yerTutucu yalnız "?" üretir.
			for _, id := range ek {
				args = append(args, id)
			}
		}
		sorgu += ` ORDER BY e.alan_adi`
		rows, err := db.QueryContext(ctx, sorgu, args...) //nolint:gosec // G701: sorgu SABİT parçalar + yalnız "?"; id'ler args ile bağlanır.
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var h hedef
			if err := rows.Scan(&h.EkAlanID, &h.DomainID, &h.SK, &h.AlanAdi); err != nil {
				continue
			}
			gercek, e := ekDocrootDogrula(h.SK, h.AlanAdi)
			switch {
			case e == nil:
			case errors.Is(e, os.ErrNotExist):
				continue // docroot (ya da ek alan dizini) yok: hata değil, boş
			default:
				log.Printf("websec: ek alan %q atlandı: %v", h.AlanAdi, e)
				continue
			}
			h.Docroot = ekDocrootYolu(h.SK, h.AlanAdi)
			h.gercek = gercek
			out = append(out, h)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
