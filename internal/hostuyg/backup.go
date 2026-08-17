package hostuyg

// Hostuyg app yedekleme + geri yükleme (admin-only).
//
// Kapsam: `<KurulumYolu>` (kurulum + data) + systemd unit + secret env dosyası.
// Servis yedekleme sırasında **KISA SÜRE DURDURULUR** (data tutarlılığı):
//   1. Systemd unit stop
//   2. tar czf <KurulumYolu> + unit + env + subdomain cert (varsa) → tmp
//   3. SHA256 hesapla
//   4. Atomic rename → BackupKok/<kod-ornek>/<ts>.tar.gz
//   5. Systemd unit start
//   6. Sağlık bekle
//   7. DB kayıt
//   8. Rotasyon (son N tut, YedekTutSayisi default 5)
//
// Geri yükleme:
//   1. Unit stop
//   2. Mevcut kurulum dizinini yedeğe al (.geriyukleme-oncesi.tar.gz)
//   3. Tar aç → KurulumYolu
//   4. Unit start
//   5. Healthcheck fail → mevcut yedekten geri (rollback)

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"
)

const (
	BackupKok      = "/var/backups/gpanel-apps"
	YedekTutSayisi = 5                       // rotasyon: son N tut
	YedekBoyutMax  = 20 * 1024 * 1024 * 1024 // 20 GB — DoS koruması
)

type Yedek struct {
	ID         int64     `json:"id"`
	UygulamaID int64     `json:"uygulama_id"`
	Dosya      string    `json:"dosya"`
	BoyutByte  int64     `json:"boyut_byte"`
	SHA256     string    `json:"sha256"`
	Aciklama   string    `json:"aciklama"`
	Olusturan  *int64    `json:"olusturan,omitempty"`
	Olusturma  time.Time `json:"olusturma"`
}

// YedekAlAsync — Is döner (Prometheus TSDB gibi büyük app'ler için 15dk sync
// HTTP timeout riski var; async pattern KurAsync ile aynı).
// Frontend `/hostuyg/is/{is_id}` ile polling yapar.
func YedekAlAsync(db *sql.DB, k *UygulamaKayit, aciklama string, aktor *int64) *Is {
	is := IsBaslat("yedek", k.Kod, k.ID)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("hostuyg.YedekAlAsync PANIC: %v\n%s", r, debug.Stack())
				is.Bitir(fmt.Sprintf("panik: %v", r))
			}
		}()
		ctx, iptal := context.WithTimeout(context.Background(), 30*time.Minute)
		defer iptal()
		is.AdimEkle("yedekleme başladı: "+k.SystemdUnit, true)
		y, err := YedekAl(ctx, db, k, aciklama, aktor)
		if err != nil {
			is.AdimEkle("hata: "+err.Error(), false)
			is.Bitir(err.Error())
			return
		}
		is.AdimEkle(fmt.Sprintf("yedek alındı id=%d boyut=%d bytes sha=%s",
			y.ID, y.BoyutByte, y.SHA256[:16]), true)
		is.Bitir("")
	}()
	return is
}

// YedekGeriYukleAsync — Is döner.
func YedekGeriYukleAsync(db *sql.DB, k *UygulamaKayit, yedekID int64) *Is {
	is := IsBaslat("restore", k.Kod, k.ID)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("hostuyg.YedekGeriYukleAsync PANIC: %v\n%s", r, debug.Stack())
				is.Bitir(fmt.Sprintf("panik: %v", r))
			}
		}()
		ctx, iptal := context.WithTimeout(context.Background(), 30*time.Minute)
		defer iptal()
		is.AdimEkle(fmt.Sprintf("geri yükleme başladı (yedek id=%d)", yedekID), true)
		err := YedekGeriYukle(ctx, db, k, yedekID)
		if err != nil {
			is.AdimEkle("hata: "+err.Error(), false)
			is.Bitir(err.Error())
			return
		}
		is.AdimEkle("geri yükleme başarılı", true)
		is.Bitir("")
	}()
	return is
}

// YedekAl — servis stop → tar czf → sha256 → start → DB kayıt → rotasyon.
func YedekAl(ctx context.Context, db *sql.DB, k *UygulamaKayit, aciklama string, aktor *int64) (*Yedek, error) {
	if k == nil || k.ID == 0 {
		return nil, fmt.Errorf("uygulama boş")
	}
	// Backup dizini
	appBackupDir := filepath.Join(BackupKok, k.Kod+"-"+k.OrnekAd)
	if err := os.MkdirAll(appBackupDir, 0700); err != nil {
		return nil, fmt.Errorf("backup dizini: %w", err)
	}

	// Stop unit (data tutarlılığı için)
	stopCtx, stopIptal := context.WithTimeout(ctx, 15*time.Second)
	_ = UnitStopDisable(stopCtx, k.SystemdUnit)
	stopIptal()
	log.Printf("hostuyg.YedekAl: %s durduruldu", k.SystemdUnit)

	// Start defer — hata olsa da servisi geri kaldır (start fail'i log)
	defer func() {
		startCtx, startIptal := context.WithTimeout(context.Background(), 15*time.Second)
		if err := UnitEnableStart(startCtx, k.SystemdUnit); err != nil {
			log.Printf("hostuyg.YedekAl: KRİTİK — unit start FAIL post-backup: %s: %v",
				k.SystemdUnit, err)
		}
		startIptal()
	}()

	// tar hedef listesi
	ts := time.Now().UTC().Format("20060102T150405Z")
	tmp := filepath.Join(appBackupDir, ts+".tar.gz.tmp")
	hedef := filepath.Join(appBackupDir, ts+".tar.gz")
	// MV#10: tmp leak koruma — herhangi bir hata sonrası tmp'yi temizle
	tmpBasarili := false
	defer func() {
		if !tmpBasarili {
			_ = os.Remove(tmp)
		}
	}()

	// tar czf tmp -C / opt/gpanel-apps/... etc/systemd/system/...
	// Path relative olsun ki restore aynı yerlere açsın
	kurulumRel := strings.TrimPrefix(k.KurulumYolu, "/")
	unitRel := strings.TrimPrefix(filepath.Join(UnitDizin, k.SystemdUnit+".service"), "/")
	envRel := strings.TrimPrefix(EnvDosyaYolu(k.SystemdUnit), "/")

	// Recipe.BackupExclude — recipe-özel exclude pattern'ları
	// MV#10: --anchored + <kurulumRel>/ prefix ile exclude'u net anchor'la
	// (GNU tar default non-anchored; ambiguous. Explicit path daha güvenli.)
	args := []string{"czf", tmp, "--anchored", "--wildcards", "--exclude=*.log"}
	if r := KatalogAra(k.Kod); r != nil {
		for _, ex := range r.BackupExclude {
			// Recipe pattern göreli (data/wal/*) → tar arşiv path ile eşleşsin diye
			// kurulumRel prefix ekle
			args = append(args, "--exclude="+kurulumRel+"/"+ex)
		}
	}
	args = append(args, "-C", "/", kurulumRel, unitRel)
	// env dosyası varsa dahil
	if _, err := os.Stat("/" + envRel); err == nil {
		args = append(args, envRel)
	}
	// Subdomain cert dizini varsa
	if k.SubdomainAd != "" {
		certRel := strings.TrimPrefix(filepath.Join(HostuygSSLKok, k.SubdomainAd), "/")
		if _, err := os.Stat("/" + certRel); err == nil {
			args = append(args, certRel)
		}
	}

	tarCtx, tarIptal := context.WithTimeout(ctx, 10*time.Minute)
	defer tarIptal()
	if out, err := exec.CommandContext(tarCtx, "tar", args...).CombinedOutput(); err != nil {
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("tar czf: %s (%w)", strings.TrimSpace(string(out)), err)
	}

	// Boyut kontrolü
	st, err := os.Stat(tmp)
	if err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	if st.Size() > YedekBoyutMax {
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("yedek boyutu %d 20GB'ı aşıyor", st.Size())
	}

	// SHA256
	sha, err := dosyaSHA(tmp)
	if err != nil {
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("sha256: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmp, hedef); err != nil {
		return nil, fmt.Errorf("rename: %w", err)
	}
	tmpBasarili = true        // defer artık silmesin
	_ = os.Chmod(hedef, 0600) // sadece root

	// DB kayıt
	dbCtx, dbIptal := context.WithTimeout(ctx, 5*time.Second)
	defer dbIptal()
	res, err := db.ExecContext(dbCtx,
		`INSERT INTO cp_host_uyg_yedekler (uygulama_id, dosya, boyut_byte, sha256, aciklama, olusturan)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		k.ID, hedef, st.Size(), sha, aciklama, aktor)
	if err != nil {
		_ = os.Remove(hedef)
		return nil, fmt.Errorf("DB kayıt: %w", err)
	}
	id, _ := res.LastInsertId()

	// Rotasyon — recipe.BackupTutSayisi override
	tutSayisi := YedekTutSayisi
	if r := KatalogAra(k.Kod); r != nil && r.BackupTutSayisi > 0 {
		tutSayisi = r.BackupTutSayisi
	}
	if err := rotasyonUygulaN(dbCtx, db, k.ID, tutSayisi); err != nil {
		log.Printf("hostuyg.YedekAl: rotasyon uyarısı: %v", err)
	}

	log.Printf("hostuyg.YedekAl: %s yedeklendi %s (%d bytes)", k.SystemdUnit, hedef, st.Size())
	return &Yedek{
		ID: id, UygulamaID: k.ID, Dosya: hedef, BoyutByte: st.Size(),
		SHA256: sha, Aciklama: aciklama, Olusturan: aktor, Olusturma: time.Now(),
	}, nil
}

// YedekListe — bir app için tüm yedekler (yeni önce).
func YedekListe(ctx context.Context, db *sql.DB, uygID int64) ([]Yedek, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, uygulama_id, dosya, boyut_byte, sha256, aciklama, olusturan, olusturma
		 FROM cp_host_uyg_yedekler WHERE uygulama_id=? ORDER BY id DESC`, uygID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Yedek{}
	for rows.Next() {
		var y Yedek
		if err := rows.Scan(&y.ID, &y.UygulamaID, &y.Dosya, &y.BoyutByte,
			&y.SHA256, &y.Aciklama, &y.Olusturan, &y.Olusturma); err == nil {
			out = append(out, y)
		}
	}
	return out, nil
}

// YedekSil — DB kayıt + fiziksel dosya sil.
func YedekSil(ctx context.Context, db *sql.DB, yedekID int64) error {
	var dosya string
	err := db.QueryRowContext(ctx, `SELECT dosya FROM cp_host_uyg_yedekler WHERE id=?`, yedekID).Scan(&dosya)
	if err != nil {
		return err
	}
	// Güvenlik: dosya BackupKok altında olmalı + Clean (path traversal defense)
	if filepath.Clean(dosya) != dosya || !strings.HasPrefix(dosya, BackupKok+string(filepath.Separator)) {
		return fmt.Errorf("güvenlik: dosya %s BackupKok dışında veya normalize gerektiriyor", dosya)
	}
	_ = os.Remove(dosya)
	_, err = db.ExecContext(ctx, `DELETE FROM cp_host_uyg_yedekler WHERE id=?`, yedekID)
	return err
}

// YedekGeriYukle — tar aç → healthcheck → fail ise mevcut yedekten geri.
func YedekGeriYukle(ctx context.Context, db *sql.DB, k *UygulamaKayit, yedekID int64) error {
	var yedekDosya, yedekSHA string
	err := db.QueryRowContext(ctx,
		`SELECT dosya, sha256 FROM cp_host_uyg_yedekler WHERE id=? AND uygulama_id=?`,
		yedekID, k.ID).Scan(&yedekDosya, &yedekSHA)
	if err != nil {
		return fmt.Errorf("yedek bulunamadı: %w", err)
	}
	// SHA doğrulama
	if got, err := dosyaSHA(yedekDosya); err != nil {
		return fmt.Errorf("sha check: %w", err)
	} else if got != yedekSHA {
		return fmt.Errorf("yedek dosyası bozuk: sha mismatch (bekleniyor=%s, gerçek=%s)", yedekSHA, got)
	}

	// Stop
	stopCtx, stopIptal := context.WithTimeout(ctx, 15*time.Second)
	_ = UnitStopDisable(stopCtx, k.SystemdUnit)
	stopIptal()

	// Mevcut kurulum yedeğe al (rollback için)
	rollbackTar := k.KurulumYolu + ".geriyukleme-oncesi.tar.gz"
	kurulumRel := strings.TrimPrefix(k.KurulumYolu, "/")
	if out, err := exec.CommandContext(ctx, "tar", "czf", rollbackTar, "-C", "/", kurulumRel).CombinedOutput(); err != nil {
		return fmt.Errorf("mevcut yedek: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	// MV#9 fix: defer koşullu — sadece BAŞARILI son yolda sil. Rollback yolunda
	// rollbackTar diski KORU (manuel kurtarma için).
	basariliyleTemizle := false
	defer func() {
		if basariliyleTemizle {
			_ = os.Remove(rollbackTar)
		} else {
			log.Printf("hostuyg.YedekGeriYukle: rollback tar KORUNDU (manuel kurtarma): %s", rollbackTar)
		}
	}()

	// Rollback fn — sessiz yerine log + timeout (20GB tar sonsuz sürmesin)
	rollback := func(neden string) {
		rbCtx, rbIptal := context.WithTimeout(context.Background(), 15*time.Minute)
		defer rbIptal()
		if out, err := exec.CommandContext(rbCtx, "tar", "xzf", rollbackTar, "-C", "/").CombinedOutput(); err != nil {
			log.Printf("hostuyg.YedekGeriYukle: KRİTİK — ROLLBACK FAIL (%s): %s: %v. App KIRIK, manuel: tar xzf %s -C /",
				neden, strings.TrimSpace(string(out)), err, rollbackTar)
		}
		if err := UnitEnableStart(context.Background(), k.SystemdUnit); err != nil {
			log.Printf("hostuyg.YedekGeriYukle: rollback sonrası unit start fail: %v", err)
		}
	}

	// Mevcut kurulum sil + tar aç
	_ = os.RemoveAll(k.KurulumYolu)
	if out, err := exec.CommandContext(ctx, "tar", "xzf", yedekDosya, "-C", "/").CombinedOutput(); err != nil {
		rollback("tar xzf")
		return fmt.Errorf("tar xzf: %s — geri alındı", strings.TrimSpace(string(out)))
	}

	// Start
	startCtx, startIptal := context.WithTimeout(ctx, 15*time.Second)
	if err := UnitEnableStart(startCtx, k.SystemdUnit); err != nil {
		startIptal()
		_ = os.RemoveAll(k.KurulumYolu)
		rollback("unit start")
		return fmt.Errorf("unit start: %w — geri alındı", err)
	}
	startIptal()

	// Healthcheck — durum bazlı
	time.Sleep(5 * time.Second)
	if UnitDurum(k.SystemdUnit) != "active" {
		_ = UnitStopDisable(context.Background(), k.SystemdUnit)
		_ = os.RemoveAll(k.KurulumYolu)
		rollback("systemd active değil")
		return fmt.Errorf("restore sonrası unit active değil — geri alındı")
	}

	basariliyleTemizle = true // defer temizler
	log.Printf("hostuyg.YedekGeriYukle: %s başarıyla geri yüklendi (yedek id=%d)", k.SystemdUnit, yedekID)
	return nil
}

/* ---- yardımcılar ---- */

// rotasyonUygulaN — belirtilen N sayısını aşan eski yedekleri sil.
func rotasyonUygulaN(ctx context.Context, db *sql.DB, uygID int64, tutSayisi int) error {
	if tutSayisi <= 0 {
		tutSayisi = YedekTutSayisi
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id, dosya FROM cp_host_uyg_yedekler WHERE uygulama_id=? ORDER BY id DESC`, uygID)
	if err != nil {
		return err
	}
	defer rows.Close()
	type kayit struct {
		id    int64
		dosya string
	}
	var tumu []kayit
	for rows.Next() {
		var k kayit
		if err := rows.Scan(&k.id, &k.dosya); err == nil {
			tumu = append(tumu, k)
		}
	}
	rows.Close()
	if len(tumu) <= tutSayisi {
		return nil
	}
	for i := tutSayisi; i < len(tumu); i++ {
		if err := os.Remove(tumu[i].dosya); err != nil && !os.IsNotExist(err) {
			log.Printf("hostuyg.rotasyon: dosya sil UYARI %s: %v", tumu[i].dosya, err)
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM cp_host_uyg_yedekler WHERE id=?`, tumu[i].id)
		log.Printf("hostuyg.rotasyon: eski yedek silindi %s", tumu[i].dosya)
	}
	return nil
}

func dosyaSHA(yol string) (string, error) {
	f, err := os.Open(yol)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
