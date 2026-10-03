package domains

// ssl_tarama.go — domains.ssl_bitis'i DİSKTEKİ sertifikadan tazeler (D3b).
//
// NEDEN: ssl_bitis yaklaşık bir değer — oto-SSL yolu `NOW()+90 gün` yazıyor
// (handlers.go), acme.sh/certbot yenilemesi DB'ye hiç yazmıyor. Pano SSL widget'ı
// ve SSL eşik bildirimi bu sütunu okur; bayat değer "süresi doluyor" yalanı
// (yenilenmiş sertifika) ya da sessizlik (yenilenemeyen sertifika) üretirdi.
// Tek gerçek kaynak sertifikanın kendisidir (sertifikaGercek, SSLDurum ile aynı).
//
// Güvenlik: yalnız NotAfter okunur; içerik/yol hiçbir yere yazılmaz. Yol
// sembolik bağlantısı çözülür; kiracının yazabildiği /home altına çıkan ya da
// mutlak olmayan yol okunmaz (cert_path panel yazar; bu ikinci savunma).

import (
	"context"
	"database/sql"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// sertifikaMaxBayt — okunacak en büyük sertifika dosyası (zincir dahil bol bol yeter).
const sertifikaMaxBayt = 256 << 10

// sertifikaYasakKok — kiracının yazabildiği kök: çözülmüş yol buranın altındaysa
// okunmaz (test geçici dizinle değiştirir).
var sertifikaYasakKok = "/home"

// sertifikaYoluGuvenli — okunabilir sertifika yolu mu (mutlak, çözülmüş hali
// /home altında değil, normal dosya, boyut sınırı içinde).
func sertifikaYoluGuvenli(yol string) bool {
	if yol == "" || !filepath.IsAbs(yol) {
		return false
	}
	gercek, err := filepath.EvalSymlinks(yol)
	if err != nil || !filepath.IsAbs(gercek) {
		return false
	}
	if gercek == sertifikaYasakKok || strings.HasPrefix(gercek, sertifikaYasakKok+"/") {
		return false
	}
	fi, err := os.Stat(gercek)
	return err == nil && fi.Mode().IsRegular() && fi.Size() <= sertifikaMaxBayt
}

// SSLBitisTazele — ssl_aktif=1 domainlerin ssl_bitis'ini sertifikanın NotAfter'ından
// yazar. UPDATE cert_path koşulludur: tarama sırasında yeniden çıkarılan sertifikayı
// ezmez. Dönen: güncellenen satır sayısı.
func SSLBitisTazele(ctx context.Context, db *sql.DB) int {
	rows, err := db.QueryContext(ctx,
		`SELECT id, cert_path, COALESCE(ssl_kaynak,''), COALESCE(DATE_FORMAT(ssl_bitis,'%Y-%m-%d'),'')
		   FROM domains WHERE ssl_aktif=1 AND cert_path<>''`)
	if err != nil {
		log.Printf("ssl tarama: domainler okunamadı: %v", err)
		return 0
	}
	type satir struct {
		id                 int64
		yol, kaynak, bitis string
	}
	var liste []satir
	for rows.Next() {
		var s satir
		if rows.Scan(&s.id, &s.yol, &s.kaynak, &s.bitis) == nil {
			liste = append(liste, s)
		}
	}
	rows.Close()
	n := 0
	for _, s := range liste {
		if ctx.Err() != nil {
			break
		}
		if !sertifikaYoluGuvenli(s.yol) {
			continue
		}
		_, bitis, _ := sertifikaGercek(s.yol, s.kaynak, time.Time{})
		if bitis.IsZero() {
			continue
		}
		gun := bitis.Local().Format("2006-01-02")
		if gun == s.bitis {
			continue
		}
		if res, e := db.ExecContext(ctx, `UPDATE domains SET ssl_bitis=? WHERE id=? AND cert_path=?`, gun, s.id, s.yol); e == nil {
			if k, _ := res.RowsAffected(); k > 0 {
				n++
			}
		}
	}
	return n
}
