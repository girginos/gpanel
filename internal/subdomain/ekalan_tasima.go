package subdomain

// ekalan_tasima.go — ek alan klasörü taşıma onarımı: <ev>/<sk>/ek-alanlar/<alan> →
// <ev>/<sk>/domains/<alan>. Açılışta HealEkAlanlar'dan ÖNCE koşar; idempotent.
//
// Güvenlik ilkeleri (kiracı bu dizinlerde yazabilir, işlemler root):
//   - YALNIZ panelin DB'de kayıtlı ek alan adı klasörleri taşınır (alt klasör adı
//     ek_alan_adlari.alan_adi ile birebir eşleşmeli); gerisi (başka klasör/dosya/symlink)
//     yerinde kalır ve günlüğe yazılır.
//   - Sembolik bağlantı olan ya da dizin olmayan taban/alan klasörü taşınmaz.
//   - Her rename'den önce ekYolGuvenli (EvalSymlinks sonucu hesap dizini altında).
//   - Hedefte aynı adlı bir şey varsa HİÇBİR ŞEY taşınmaz (veri ezme yok): hesap atlanır.
//   - Taşıma sonrası vhost'lar yeniden üretilir; biri düşerse (nginx -t dahil) TÜM
//     değişiklik geri alınır (klasörler eski yerine, vhost dosyaları eski içeriğe).
//   - os.Rename: aynı dosya sistemi, atomik; veri kopyalanmaz, sahiplik/izin/SELinux
//     bağlamı olduğu gibi korunur (restorecon bilerek çağrılmaz: ekDocrootHazirla
//     chcon ile httpd_sys_content_t verir, restorecon politika varsayılanına çevirip
//     bağlamı bozabilirdi).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
)

type ekTasimaAlan struct {
	ID int64
	Ad string
}

// ekKlasorTasi — tek hesap için taşıma. (taşınan sayısı, hata). Hata = hesap atlandı ya da
// geri alındı (günlüğe yazılması çağıranın işi); nil+0 = yapılacak iş yok.
func ekKlasorTasi(ctx context.Context, sk string, alanlar []ekTasimaAlan,
	yenidenKur func(context.Context, int64) error) (int, error) {
	if !reEkSK.MatchString(sk) {
		return 0, errors.New("güvensiz hesap adı")
	}
	eski := evKoku + "/" + sk + "/" + ekAlanEskiKlasor
	yeni := ekDocrootTaban(sk)

	fi, err := os.Lstat(eski)
	if err != nil {
		return 0, nil // eski klasör yok: iş yok
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		return 0, fmt.Errorf("%s dizin değil/sembolik bağlantı — dokunulmadı", ekAlanEskiKlasor)
	}
	if err := ekYolGuvenli(sk, eski); err != nil {
		return 0, fmt.Errorf("%s: %w", ekAlanEskiKlasor, err)
	}
	yeniVar := false
	if yfi, ye := os.Lstat(yeni); ye == nil {
		if yfi.Mode()&os.ModeSymlink != 0 || !yfi.IsDir() {
			return 0, fmt.Errorf("%s dizin değil/sembolik bağlantı — taşıma atlandı", ekAlanKlasoru)
		}
		if err := ekYolGuvenli(sk, yeni); err != nil {
			return 0, fmt.Errorf("%s: %w", ekAlanKlasoru, err)
		}
		yeniVar = true
	} else if !os.IsNotExist(ye) {
		return 0, fmt.Errorf("%s okunamadı: %w", ekAlanKlasoru, ye)
	}

	ents, err := os.ReadDir(eski)
	if err != nil {
		return 0, fmt.Errorf("%s okunamadı: %w", ekAlanEskiKlasor, err)
	}
	kayitli := map[string]ekTasimaAlan{}
	for _, a := range alanlar {
		if ekAdGuvenli(a.Ad) {
			kayitli[a.Ad] = a
		}
	}
	var eslesen []ekTasimaAlan
	var eslesmeyen []string
	for _, en := range ents {
		a, ok := kayitli[en.Name()]
		if !ok || !en.IsDir() { // symlink'in IsDir'i false: taşınmaz
			eslesmeyen = append(eslesmeyen, en.Name())
			continue
		}
		eslesen = append(eslesen, a)
	}
	if len(eslesen) == 0 {
		if len(eslesmeyen) == 0 {
			_ = os.Remove(eski) // boş kaynak
		} else {
			log.Printf("ek alan taşıma: %s/%s içinde panele ait klasör yok, %d öğe yerinde bırakıldı", sk, ekAlanEskiKlasor, len(eslesmeyen))
		}
		return 0, nil
	}
	// Çakışma ön denetimi: hiçbir şey taşımadan önce.
	if yeniVar {
		for _, a := range eslesen {
			if _, e := os.Lstat(yeni + "/" + a.Ad); e == nil {
				return 0, fmt.Errorf("çakışma: %s/%s zaten var — hesap atlandı (veri ezilmedi)", ekAlanKlasoru, a.Ad)
			}
		}
	}

	eskiConf := map[int64][]byte{}
	for _, a := range eslesen {
		b, _ := os.ReadFile(ekConfPath(sk, a.Ad))
		eskiConf[a.ID] = b
	}
	var geri []func()
	geriAl := func() {
		for i := len(geri) - 1; i >= 0; i-- {
			geri[i]()
		}
		for _, a := range eslesen {
			conf := ekConfPath(sk, a.Ad)
			if b := eskiConf[a.ID]; len(b) > 0 {
				_ = os.WriteFile(conf, b, 0o644)
			}
		}
		_, _ = komut("systemctl", "reload", "nginx")
	}

	butun := !yeniVar && len(eslesmeyen) == 0
	if butun {
		// Taban dizinin tamamı eşleşen klasörlerden ibaret: tek atomik rename.
		if err := os.Rename(eski, yeni); err != nil {
			return 0, fmt.Errorf("rename: %w", err)
		}
		geri = append(geri, func() { _ = os.Rename(yeni, eski) })
		if err := ekYolGuvenli(sk, yeni); err != nil {
			geriAl()
			return 0, err
		}
	} else {
		if !yeniVar {
			if err := os.MkdirAll(yeni, 0o755); err != nil {
				return 0, err
			}
			_, _ = komut("chown", "-h", sk+":"+sk, yeni)
			geri = append(geri, func() { _ = os.Remove(yeni) }) // yalnız boşsa kalkar
			if err := ekYolGuvenli(sk, yeni); err != nil {
				geriAl()
				return 0, err
			}
		}
		for _, a := range eslesen {
			src, dst := eski+"/"+a.Ad, yeni+"/"+a.Ad
			if err := ekYolGuvenli(sk, src); err != nil {
				geriAl()
				return 0, err
			}
			if err := ekYolGuvenli(sk, dst); err != nil {
				geriAl()
				return 0, err
			}
			if err := os.Rename(src, dst); err != nil {
				geriAl()
				return 0, fmt.Errorf("rename %s: %w", a.Ad, err)
			}
			geri = append(geri, func() { _ = os.Rename(dst, src) })
		}
	}
	for _, a := range eslesen {
		if err := yenidenKur(ctx, a.ID); err != nil {
			geriAl()
			return 0, fmt.Errorf("vhost %s yeniden kurulamadı, taşıma GERİ ALINDI: %w", a.Ad, err)
		}
	}
	if !butun {
		_ = os.Remove(eski) // boşaldıysa kalkar
	}
	for _, a := range eslesen {
		log.Printf("✓ ek alan klasörü taşındı: %s/%s %s → %s", sk, a.Ad, ekAlanEskiKlasor, ekAlanKlasoru)
	}
	if len(eslesmeyen) > 0 {
		log.Printf("ek alan taşıma: %s/%s içinde panele ait olmayan %d öğe yerinde bırakıldı", sk, ekAlanEskiKlasor, len(eslesmeyen))
	}
	return len(eslesen), nil
}

// HealEkAlanKlasorTasima — açılış onarımı: tüm hesapların ek alan klasörlerini
// ek-alanlar'dan domains'e taşır. HealEkAlanlar'dan ÖNCE çağrılmalı.
func HealEkAlanKlasorTasima(db *sql.DB) {
	if db == nil {
		return
	}
	rows, err := db.Query(`SELECT d.sistem_kullanici, e.id, e.alan_adi
	   FROM ek_alan_adlari e JOIN domains d ON d.id=e.domain_id ORDER BY d.sistem_kullanici, e.id`)
	if err != nil {
		if !tabloYok(err) {
			log.Printf("ek alan taşıma: liste okunamadı: %v", err)
		}
		return
	}
	var sirali []string
	grup := map[string][]ekTasimaAlan{}
	for rows.Next() {
		var sk, ad string
		var id int64
		if rows.Scan(&sk, &id, &ad) != nil {
			continue
		}
		if !reEkSK.MatchString(sk) || !ekAdGuvenli(ad) {
			continue
		}
		if _, ok := grup[sk]; !ok {
			sirali = append(sirali, sk)
		}
		grup[sk] = append(grup[sk], ekTasimaAlan{ID: id, Ad: ad})
	}
	rows.Close()
	ctx := context.Background()
	for _, sk := range sirali {
		if _, err := ekKlasorTasi(ctx, sk, grup[sk], func(c context.Context, id int64) error {
			return ekVhostYenidenKur(c, db, id)
		}); err != nil {
			log.Printf("ek alan taşıma: %s atlandı/geri alındı: %s", sk, strings.TrimSpace(err.Error()))
		}
	}
}

// ekDocrootTasimaBekliyor — alan klasörü eski yerde duruyor, yenisi yok (taşıma atlandı):
// HealEkAlanlar bu alana dokunmamalı (vhost'u yeni, var olmayan yola çevirmesin).
func ekDocrootTasimaBekliyor(sk, ad string) bool {
	if _, err := os.Lstat(ekDocrootTaban(sk) + "/" + ad); err == nil {
		return false
	}
	_, err := os.Lstat(evKoku + "/" + sk + "/" + ekAlanEskiKlasor + "/" + ad)
	return err == nil
}
