// Sistem geneli yedek ayarlari: ana salter + disk korumasi + GLOBAL uzak hedef.
//
// Neden: eskiden (a) otomatik yedegi topluca kapatmanin yolu yoktu (bash cron
// girginospanel-backup-all domains.backup_freq sutununu HIC okumuyordu -> panelde
// "kapali" olan domainler bile her gece yedekleniyordu), (b) uzak hedef YALNIZ domain
// bazliydi (backup_destinations.domain_id NOT NULL + UNIQUE uk_domain), (c) yazmadan
// once bos alan kontrolu yoktu -> yedekler kok diski doldurup paneli+siteleri dusurebiliyordu.
package backups

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"girginospanel/internal/bildirim"
)

// tarihDamgasi: yedek dosya adindaki "-YYYYMMDD-HHMMSS" damgasi.
var tarihDamgasi = regexp.MustCompile(`-(\d{8})-\d{6}`)

// GenelAyar: backup_genel_ayar tekil satiri (id=1).
type GenelAyar struct {
	Aktif         bool   `json:"aktif"`
	MinBosGB      int    `json:"min_bos_gb"`
	MaxDepoGB     int    `json:"max_depo_gb"` // 0 = sinirsiz
	UzakAktif     bool   `json:"uzak_aktif"`
	UzakTip       string `json:"uzak_tip"`
	UzakHost      string `json:"uzak_host"`
	UzakPort      int    `json:"uzak_port"`
	UzakKullanici string `json:"uzak_kullanici"`
	UzakParola    string `json:"uzak_parola,omitempty"` // write-only: GET bos doner
	UzakDizin     string `json:"uzak_dizin"`
	UzakYerelSil  bool   `json:"uzak_yerel_sil"`
	SonYukleme    string `json:"son_yukleme,omitempty"`
	SonDurum      string `json:"son_durum,omitempty"`
	SonHata       string `json:"son_hata,omitempty"`
	// Salt-okunur olcumler (UI icin):
	BosGB  float64 `json:"bos_gb"`
	DepoGB float64 `json:"depo_gb"`
}

// varsayilanGenel: tablo/satir yoksa guvenli varsayilan (aktif, 10GB esik, uzak kapali).
func varsayilanGenel() *GenelAyar {
	return &GenelAyar{Aktif: true, MinBosGB: 10, UzakTip: "sftp", UzakPort: 22, UzakDizin: "/"}
}

// genelAyarOku: tekil ayar satirini doner. Tablo/satir yoksa varsayilan (hata DEGIL) —
// migration henuz kosmamis kurulumda scheduler calismaya devam etmeli.
func genelAyarOku(ctx context.Context, db *sql.DB) *GenelAyar {
	g := varsayilanGenel()
	var aktif, uzakAktif, yerelSil int
	var sonYuk sql.NullString
	err := db.QueryRowContext(ctx,
		`SELECT aktif, min_bos_gb, max_depo_gb, uzak_aktif, uzak_tip, uzak_host, uzak_port,
		        uzak_kullanici, uzak_parola, uzak_dizin, uzak_yerel_sil,
		        DATE_FORMAT(son_yukleme,'%Y-%m-%d %H:%i'), son_durum, son_hata
		 FROM backup_genel_ayar WHERE id=1`).
		Scan(&aktif, &g.MinBosGB, &g.MaxDepoGB, &uzakAktif, &g.UzakTip, &g.UzakHost, &g.UzakPort,
			&g.UzakKullanici, &g.UzakParola, &g.UzakDizin, &yerelSil,
			&sonYuk, &g.SonDurum, &g.SonHata)
	if err != nil {
		return varsayilanGenel()
	}
	g.Aktif = aktif == 1
	g.UzakAktif = uzakAktif == 1
	g.UzakYerelSil = yerelSil == 1
	g.SonYukleme = sonYuk.String
	return g
}

// genelAyarYaz: ayarlari gunceller. parola bos gelirse MEVCUT parola korunur
// (write-only alan: UI parolayi hic geri okumaz, her kayitta yeniden yazdirmayalim).
func genelAyarYaz(ctx context.Context, db *sql.DB, g *GenelAyar) error {
	b := func(v bool) int {
		if v {
			return 1
		}
		return 0
	}
	if _, err := db.ExecContext(ctx, `INSERT IGNORE INTO backup_genel_ayar (id) VALUES (1)`); err != nil {
		return err
	}
	if g.UzakParola == "" {
		_, err := db.ExecContext(ctx,
			`UPDATE backup_genel_ayar SET aktif=?, min_bos_gb=?, max_depo_gb=?, uzak_aktif=?,
			 uzak_tip=?, uzak_host=?, uzak_port=?, uzak_kullanici=?, uzak_dizin=?, uzak_yerel_sil=?
			 WHERE id=1`,
			b(g.Aktif), g.MinBosGB, g.MaxDepoGB, b(g.UzakAktif),
			g.UzakTip, g.UzakHost, g.UzakPort, g.UzakKullanici, g.UzakDizin, b(g.UzakYerelSil))
		return err
	}
	_, err := db.ExecContext(ctx,
		`UPDATE backup_genel_ayar SET aktif=?, min_bos_gb=?, max_depo_gb=?, uzak_aktif=?,
		 uzak_tip=?, uzak_host=?, uzak_port=?, uzak_kullanici=?, uzak_parola=?, uzak_dizin=?,
		 uzak_yerel_sil=? WHERE id=1`,
		b(g.Aktif), g.MinBosGB, g.MaxDepoGB, b(g.UzakAktif),
		g.UzakTip, g.UzakHost, g.UzakPort, g.UzakKullanici, g.UzakParola, g.UzakDizin, b(g.UzakYerelSil))
	return err
}

// hedef: global ayarlari domain-bazli Destination yapisina cevirir; boylece mevcut
// uploadToRemote/testConnection kodu (lftp/sshpass/curl) aynen tekrar kullanilir.
func (g *GenelAyar) hedef() *Destination {
	return &Destination{
		Tip: g.UzakTip, Host: g.UzakHost, Port: g.UzakPort,
		Kullanici: g.UzakKullanici, Parola: g.UzakParola,
		UzakDizin: g.UzakDizin, Aktif: g.UzakAktif,
	}
}

// diskBosGB: BackupRoot'un bulundugu dosya sisteminde bos alan (GB).
// Not: root icin ayrilmis bloklar haric (Bavail) — gercekten yazilabilir alan.
func diskBosGB(yol string) (float64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(yol, &st); err != nil {
		return 0, err
	}
	return float64(st.Bavail) * float64(st.Bsize) / (1024 * 1024 * 1024), nil
}

// depoKullanimGB: BackupRoot altindaki toplam yedek boyutu (GB).
func depoKullanimGB() float64 {
	var toplam int64
	_ = filepath.Walk(BackupRoot, func(_ string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil // okunamayan dizin taramayi durdurmasin
		}
		if fi != nil && fi.Mode().IsRegular() {
			toplam += fi.Size()
		}
		return nil
	})
	return float64(toplam) / (1024 * 1024 * 1024)
}

// diskKapisi: yedek yazmadan ONCE cagrilir. Yazmaya izin varsa "" doner; yoksa
// engel sebebini doner.
func diskKapisi(g *GenelAyar) string {
	if g.MinBosGB > 0 {
		bos, err := diskBosGB(BackupRoot)
		if err == nil && bos < float64(g.MinBosGB) {
			return fmt.Sprintf("bos alan %.1f GB < esik %d GB", bos, g.MinBosGB)
		}
	}
	if g.MaxDepoGB > 0 {
		if kul := depoKullanimGB(); kul > float64(g.MaxDepoGB) {
			return fmt.Sprintf("yedek deposu %.1f GB > tavan %d GB", kul, g.MaxDepoGB)
		}
	}
	return ""
}

// diskBildirimiVer: ayni sebeple gunde bir kez kritik bildirim (spam yok).
func diskBildirimiVer(db *sql.DB, sebep string) {
	const baslik = "Yedek durduruldu: disk"
	var n int
	_ = db.QueryRow(
		`SELECT COUNT(*) FROM cp_bildirim
		 WHERE kategori='yedek' AND baslik=? AND created_at > NOW()-INTERVAL 1 DAY`,
		baslik).Scan(&n)
	if n > 0 {
		return
	}
	bildirim.Yaz(db, "kritik", "yedek", baslik,
		"Otomatik yedekleme disk korumasi nedeniyle atlandi ("+sebep+"). Eski yedekleri temizleyin, "+
			"saklama suresini dusurun veya uzak hedef tanimlayip yerel kopyayi kapatin.", 0, "", 0)
	log.Printf("backup disk kapisi: %s", sebep)
}

// pushGenelAsync: yedegi SISTEM GENELI uzak hedefe yukler (domain bazli hedeften bagimsiz).
// Basarili yuklemeden sonra uzak_yerel_sil=1 ise yerel kopya silinir -> disk buyumesi durur.
func pushGenelAsync(db *sql.DB, g *GenelAyar, yerelYol, dosyaAdi string, yedekID int64) {
	if !g.UzakAktif || strings.TrimSpace(g.UzakHost) == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		// Tarih alt dizini: /gpanel/2026-08-25/<dosya>. uploadToRemote hedef
		// dizini `mkdir -p -f` ile kendi yaratir, ek adim gerekmez.
		hedef := g.hedef()
		hedef.UzakDizin = birlestirYol(g.UzakDizin, uzakTarihDizini(dosyaAdi))
		if err := uploadToRemote(ctx, hedef, yerelYol, dosyaAdi); err != nil {
			kisa := err.Error()
			if len(kisa) > 500 {
				kisa = kisa[:500]
			}
			db.Exec(`UPDATE backup_genel_ayar SET son_durum='hata', son_hata=?, son_yukleme=NOW() WHERE id=1`, kisa)
			// 🔴 Tek satirlik son_durum'u paralel yuklemeler EZIYOR (son-yazan-kazanir):
			// bir hata, hemen ardindan gelen basari ile silinir ve operatör yedegin
			// off-site'a HIC gitmedigini asla ogrenemez. Bildirim kalici iz birakir.
			bildirim.Yaz(db, "kritik", "yedek", "Off-site yükleme başarısız",
				dosyaAdi+": "+kisa+" — yerel kopya KORUNDU.", 0, "backup", yedekID)
			log.Printf("backup genel uzak yukleme %s: %v", dosyaAdi, err)
			return
		}
		// 🔴 YUKLEME SONRASI DOGRULAMA: lftp cikis kodu TEK BASINA yeterli degil.
		// Yerel kopyayi silmeden once uzak boyutun yerel boyutla AYNI oldugunu
		// dogrula; aksi halde yarim yuklenen dosya "basarili" sayilip tek kopya
		// silinebilir = TOPLAM VERI KAYBI.
		yerelBoyut := int64(-1)
		if fi, e := os.Stat(yerelYol); e == nil {
			yerelBoyut = fi.Size()
		}
		ub := uzakBoyut(ctx, g, hedef.UzakDizin, dosyaAdi)
		if yerelBoyut > 0 && ub > 0 && ub != yerelBoyut {
			msg := fmt.Sprintf("uzak boyut uyuşmuyor (yerel=%d uzak=%d)", yerelBoyut, ub)
			db.Exec(`UPDATE backup_genel_ayar SET son_durum='hata', son_hata=?, son_yukleme=NOW() WHERE id=1`, msg)
			bildirim.Yaz(db, "kritik", "yedek", "Off-site yükleme doğrulanamadı",
				dosyaAdi+": "+msg+" — yerel kopya KORUNDU.", 0, "backup", yedekID)
			log.Printf("backup genel: %s DOGRULAMA BASARISIZ: %s (yerel kopya korundu)", dosyaAdi, msg)
			return
		}
		db.Exec(`UPDATE backup_genel_ayar SET son_durum='ok', son_hata='', son_yukleme=NOW() WHERE id=1`)
		if g.UzakYerelSil && yedekID > 0 && ub > 0 {
			// Yerel kopyayi sil: uzakta guvende. DB kaydi kalir ki operatorun elinde
			// ne yedegi oldugu gorunsun.
			if err := os.Remove(yerelYol); err == nil {
				db.Exec(`UPDATE backups SET notlar=CONCAT(notlar,' [uzaga tasindi]') WHERE id=?`, yedekID)
				log.Printf("backup genel: %s uzaga tasindi, yerel kopya silindi", dosyaAdi)
			}
		}
	}()
}

// uzakTarihDizini: yedek dosya adindan UTC tarih klasoru turetir.
//
//	"c_corpmoney-20260825-233538.tar.gz" -> "2026-08-25"
//
// 🔴 NEDEN: tum tenantlarin tum yedekleri tek dizine yigiliyordu (28 domain x
// gunluk = kisa surede binlerce dosya); operator "hangi tarihte hangi yedek"
// sorusunu cozemez hale geliyordu. Tarih, dosya adindaki damgadan turetilir —
// ayri bir alan/semaya gerek yok ve geri-indirme de ayni kuralla yeri bulur.
// Damga cozulemezse "" doner ve dosya taban dizine yazilir (eski davranis).
func uzakTarihDizini(dosyaAdi string) string {
	m := tarihDamgasi.FindStringSubmatch(dosyaAdi)
	if len(m) != 2 {
		return ""
	}
	d := m[1] // YYYYMMDD
	return d[0:4] + "-" + d[4:6] + "-" + d[6:8]
}

// birlestirYol: "/gpanel" + "2026-08-25" -> "/gpanel/2026-08-25" (bos alt dizinde taban doner).
func birlestirYol(taban, alt string) string {
	taban = strings.TrimRight(strings.TrimSpace(taban), "/")
	if taban == "" {
		taban = "/"
	}
	if alt == "" {
		return taban
	}
	if taban == "/" {
		return "/" + alt
	}
	return taban + "/" + alt
}

// fetchGenelUzaktan: yedegi SISTEM GENELI uzak hedeften yerel yoluna indirir.
// uploadToRemote ile ayni lftp yolunu kullanir (ayni kimlik + ayni dizin semantigi).
func fetchGenelUzaktan(ctx context.Context, g *GenelAyar, dosyaAdi, yerelYol string) error {
	// Once tarih alt dizinine bak, bulunamazsa taban dizine (tarih dizini
	// gelmeden once yuklenmis yedekler orada duruyor).
	adaylar := []string{birlestirYol(g.UzakDizin, uzakTarihDizini(dosyaAdi))}
	if taban := birlestirYol(g.UzakDizin, ""); taban != adaylar[0] {
		adaylar = append(adaylar, taban)
	}
	var sonHata error
	for _, dizin := range adaylar {
		if err := fetchGenelDizinden(ctx, g, dizin, dosyaAdi, yerelYol); err == nil {
			return nil
		} else {
			sonHata = err
		}
	}
	return sonHata
}

// fetchGenelDizinden: tek bir uzak dizinden indirme denemesi.
func fetchGenelDizinden(ctx context.Context, g *GenelAyar, uzakDizin, dosyaAdi, yerelYol string) error {
	url := lftpURL(g.hedef())
	betik := fmt.Sprintf(
		`set cmd:fail-exit yes; `+
			`set sftp:auto-confirm yes; `+
			`set ssl:verify-certificate no; `+
			`set ftp:ssl-allow no; `+
			`set net:max-retries 1; `+
			`set net:timeout 20; `+
			`open -u "%s","%s" %s; `+
			`cd "%s"; `+
			`get -O "%s" "%s"; `+
			`bye`,
		lftpEscape(g.UzakKullanici), lftpEscape(g.UzakParola), url,
		lftpEscape(uzakDizin), filepath.Dir(yerelYol), lftpEscape(dosyaAdi))
	cmd, temizle, err := lftpKomutu(ctx, betik)
	if err != nil {
		return err
	}
	defer temizle()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("uzaktan indirme: %s", strings.TrimSpace(string(out)))
	}
	if fi, serr := os.Stat(yerelYol); serr != nil || fi.Size() == 0 {
		return fmt.Errorf("uzaktan indirme: dosya oluşmadı (%s)", dosyaAdi)
	}
	return nil
}

// YerelDosyaHazirla: geri-yükleme/inceleme yollarinin ORTAK on adimi.
// Dosya yereldeyse yolunu doner. Yerelde YOKSA ve sistem geneli uzak hedef
// aciksa oradan indirir.
//
// 🔴 NEDEN: "yükledikten sonra yerel kopyayı sil" acikken yedek yalnizca uzak
// sunucuda kalir; geri-yukleme yollari ise yalnizca YEREL dosyaya bakiyordu →
// yedek alinmis ama panelden GERI YUKLENEMEZ durumdaydi (404 "yedek dosyası
// diskte bulunamadı"). Kanit: uzaga tasinan yedek 404, yereli duran 200.
func YerelDosyaHazirla(ctx context.Context, db *sql.DB, sk, dosya string) (string, error) {
	yol := filepath.Join(BackupRoot, sk, dosya)
	if _, err := os.Stat(yol); err == nil {
		return yol, nil
	}
	g := genelAyarOku(ctx, db)
	if !g.UzakAktif || strings.TrimSpace(g.UzakHost) == "" {
		return "", fmt.Errorf("yedek dosyası diskte bulunamadı")
	}
	// Indirmeden once yer var mi: dolu diske indirip paneli dusurmeyelim.
	if g.MinBosGB > 0 {
		if bos, err := diskBosGB(BackupRoot); err == nil && bos < float64(g.MinBosGB) {
			return "", fmt.Errorf("yedek uzak hedefte ama indirmek için yeterli disk yok (boş %.1f GB < eşik %d GB)", bos, g.MinBosGB)
		}
	}
	if err := os.MkdirAll(filepath.Dir(yol), 0700); err != nil {
		return "", err
	}
	log.Printf("backup genel: %s yerelde yok, uzak hedeften indiriliyor", dosya)
	indirmeCtx, iptal := context.WithTimeout(ctx, 30*time.Minute)
	defer iptal()
	if err := fetchGenelUzaktan(indirmeCtx, g, dosya, yol); err != nil {
		_ = os.Remove(yol)
		return "", err
	}
	log.Printf("backup genel: %s uzak hedeften indirildi", dosya)
	return yol, nil
}

// uzakKopyaVar: yedegin sistem geneli uzak hedefte TASARIM GEREGI durup durmadigini
// soyler (uzak hedef acik + kayit "uzaga tasindi" notlu). Bit-rot taramasinin
// "yerelde yok" durumunu "bozuk" ile karistirmamasi icin kullanilir.
func uzakKopyaVar(db *sql.DB, dosya string) bool {
	g := genelAyarOku(context.Background(), db)
	if !g.UzakAktif || strings.TrimSpace(g.UzakHost) == "" {
		return false
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM backups WHERE dosya=? AND notlar LIKE '%[uzaga tasindi]%'`, dosya).Scan(&n)
	return n > 0
}

// uzakBoyut: uzak hedefteki dosyanin bayt cinsinden boyutunu doner (-1 = okunamadi).
// lftp `cls -s` cikti formatina bagli kalmamak icin `ls` ciktisindan sayisal alan
// ayiklanir; bulunamazsa -1.
func uzakBoyut(ctx context.Context, g *GenelAyar, uzakDizin, dosyaAdi string) int64 {
	betik := fmt.Sprintf(
		`set cmd:fail-exit yes; set sftp:auto-confirm yes; set net:max-retries 1; set net:timeout 20; `+
			`open -u "%s","%s" %s; cd "%s"; ls "%s"; bye`,
		lftpEscape(g.UzakKullanici), lftpEscape(g.UzakParola), lftpURL(g.hedef()),
		lftpEscape(uzakDizin), lftpEscape(dosyaAdi))
	cmd, temizle, err := lftpKomutu(ctx, betik)
	if err != nil {
		return -1
	}
	defer temizle()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return -1
	}
	for _, satir := range strings.Split(string(out), "\n") {
		if !strings.Contains(satir, dosyaAdi) {
			continue
		}
		for _, alan := range strings.Fields(satir) {
			if n, e := strconv.ParseInt(alan, 10, 64); e == nil && n > 0 {
				return n
			}
		}
	}
	return -1
}
