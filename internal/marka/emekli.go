package marka

// emekli.go — `whitelabel` eklentisinin emekliye ayrılması (D2, kullanıcı kararı 5).
//
// Neden: eklenti marka.json'u KENDİ yapısına çözüp yalnız onu yazıyordu (B5):
// çekirdeğe eklenen her yeni alan (destek_eposta, destek_telefon…) eklentinin
// ilk PUT'unda sessizce silinirdi. Yazma yolu artık çekirdekte (yonet.go).
//
// 🔴 VERİ KAYBI YOK: eklenti ile çekirdek AYNI dosyaları kullanır
// (Kok/marka.json + logo/banner/favicon.{bin,tip}); çekirdek onları aynen okur.
// 🔴 GERİ ALINABİLİR: eklenti dosyaları (/opt/girginospanel/eklentiler/whitelabel,
// birim dosyası) SİLİNMEZ. Yapılan yalnız:
//   - birim `systemctl disable --now` (geri: `systemctl enable --now <birim>`),
//   - cp_eklentiler satırı aktif=0, saglik='emekli' (geri: aktif=1),
//   - /api/v1/eklenti/whitelabel/* ve eklenti-bundle/whitelabel → 404 (EmekliKapisi).
// Eski panel ikilisine dönülürse cekirdekEklentiGuvence satırı görür (kurulu),
// yönetici eklentiyi Eklentiler sayfasından yeniden etkinleştirebilir.

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	EmekliEklenti  = "whitelabel"
	emekliBirim    = "girginospanel-eklenti-whitelabel"
	emekliBirimYol = "/etc/systemd/system/girginospanel-eklenti-whitelabel.service"
)

// EmekliKapisi — emekli eklentinin proxy/lisans/kurulum/bundle yollarını 404'ler.
// Yeniden kurulup çalıştırılması B5'i (alan silme) geri getirirdi.
func EmekliKapisi(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if emekliYolMu(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func emekliYolMu(p string) bool {
	for _, on := range []string{"/eklenti/" + EmekliEklenti, "/eklenti-bundle/" + EmekliEklenti} {
		if i := strings.Index(p, on); i >= 0 {
			kalan := p[i+len(on):]
			if kalan == "" || kalan[0] == '/' {
				return true
			}
		}
	}
	return false
}

// birimKomut — testler değiştirir.
var birimKomut = func(ctx context.Context, arg ...string) error {
	return exec.CommandContext(ctx, "systemctl", arg...).Run()
}

// EklentiEmekliyeAyir — açılışta (idempotent, açılışı bloklamaz; çağıran goroutine'de).
func EklentiEmekliyeAyir(ctx context.Context, db *sql.DB) {
	if _, err := os.Stat(emekliBirimYol); err == nil {
		c, iptal := context.WithTimeout(ctx, 30*time.Second)
		if err := birimKomut(c, "disable", "--now", emekliBirim); err != nil {
			log.Printf("marka: emekli eklenti birimi durdurulamadı (%s): %v", emekliBirim, err)
		}
		iptal()
	}
	if db == nil {
		return
	}
	res, err := db.ExecContext(ctx, `UPDATE cp_eklentiler SET aktif=0, saglik='emekli' WHERE ad=? AND (aktif<>0 OR saglik<>'emekli')`, EmekliEklenti)
	if err != nil {
		log.Printf("marka: emekli eklenti kaydı güncellenemedi: %v", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("marka: whitelabel eklentisi emekliye ayrıldı (birim durduruldu, kayıt aktif=0; dosyalar korunur, marka çekirdekte)")
	}
}

// YetimGorselTemizle — DB'de karşılığı olmayan bayi görsel dizinlerini siler
// (bayi silinince FK satırı götürür; dizin burada temizlenir). DB okunamazsa
// HİÇBİR ŞEY silinmez.
func YetimGorselTemizle(ctx context.Context, db *sql.DB) {
	kok := filepath.Join(Kok, "bayi")
	girdiler, err := os.ReadDir(kok)
	if err != nil || db == nil {
		return
	}
	rows, err := db.QueryContext(ctx, `SELECT anahtar FROM cp_marka`)
	if err != nil {
		return
	}
	var_ := map[string]bool{}
	for rows.Next() {
		var a string
		if rows.Scan(&a) == nil {
			var_[a] = true
		}
	}
	if rows.Err() != nil {
		rows.Close()
		return
	}
	rows.Close()
	for _, g := range girdiler {
		if !g.IsDir() || !AnahtarDeseni.MatchString(g.Name()) || var_[g.Name()] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(kok, g.Name())); err == nil {
			log.Printf("marka: yetim bayi görsel dizini silindi (%s)", g.Name())
		}
	}
}
