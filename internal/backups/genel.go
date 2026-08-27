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
	"sync"
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
// 🔴 `get -O <dizin>` DEGIL `get <uzak> -o <yerel>`: -O dosyayi dizine ORIJINAL
// adiyla yazar, cagiranin verdigi gecici ada (.indiriliyor) DEGIL. Atomik
// indirme bu yuzden kirilmisti: dosya dogru inip nihai ada yaziliyor ama kod
// gecici adi arayip bulamayinca "dosya olusmadi" hatasi donuyordu.
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
			`get "%s" -o "%s"; `+
			`bye`,
		lftpEscape(g.UzakKullanici), lftpEscape(g.UzakParola), url,
		lftpEscape(uzakDizin), lftpEscape(dosyaAdi), yerelYol)
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
	// 🔴 VAR OLMAK YETMEZ, DOGRU OLMALI. Eskiden yalnizca os.Stat basariliysa
	// dosya kabul ediliyordu. Panel indirme sirasinda yeniden baslarsa (deploy,
	// crash, OOM) goroutine olur ve YARIM dosya nihai adiyla diskte kalir; bir
	// sonraki geri-yukleme onu SAGLAM sanip BOZUK arsivle calisir.
	// Uretimde gerceklesti: 1.30 GB / 5.49 GB, gzip -t "unexpected end of file".
	// Artik beklenen boyutla karsilastirilir; tutmuyorsa kalinti SILINIR ve
	// yeniden indirilir.
	if fi, err := os.Stat(yol); err == nil {
		bekBoyut, _ := yedekBeklenenBoyut(db, dosya)
		if bekBoyut <= 0 || fi.Size() == bekBoyut {
			return yol, nil
		}
		log.Printf("backup: %s yerel kopya EKSIK (%d/%d bayt) — kalinti silinip yeniden indirilecek",
			dosya, fi.Size(), bekBoyut)
		_ = os.Remove(yol)
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
	// Indirme yuzdesi KESINDIR: beklenen boyut backups.boyut_b'den bilinir.
	if bek, _ := yedekBeklenenBoyut(db, dosya); bek > 0 {
		if did := dosyaDomainID(db, dosya); did > 0 {
			IlerlemeAsama(did, "yedek uzak hedeften indiriliyor", bek)
			IlerlemeDosyaIzle(did, yol+".indiriliyor")
		}
	}
	indirmeCtx, iptal := context.WithTimeout(ctx, 60*time.Minute)
	defer iptal()
	// 🔴 ATOMIK: once ".indiriliyor" adina indir, boyutu dogrula, ANCAK SONRA
	// nihai ada tasi. Boylece kesilen bir indirme nihai adi ASLA almaz ve
	// yukaridaki dogrulama devreye girmek zorunda kalmaz (ikinci savunma hatti).
	gecici := yol + ".indiriliyor"
	_ = os.Remove(gecici)
	if err := fetchGenelUzaktan(indirmeCtx, g, dosya, gecici); err != nil {
		_ = os.Remove(gecici)
		return "", err
	}
	if bek, _ := yedekBeklenenBoyut(db, dosya); bek > 0 {
		if fi, err := os.Stat(gecici); err != nil || fi.Size() != bek {
			var gercek int64 = -1
			if fi != nil {
				gercek = fi.Size()
			}
			_ = os.Remove(gecici)
			return "", fmt.Errorf("uzaktan indirme eksik: %d/%d bayt", gercek, bek)
		}
	}
	if err := os.Rename(gecici, yol); err != nil {
		_ = os.Remove(gecici)
		return "", fmt.Errorf("indirilen dosya yerine konamadi: %w", err)
	}
	log.Printf("backup genel: %s uzak hedeften indirildi (dogrulandi)", dosya)
	return yol, nil
}

// yedekBeklenenBoyut: backups tablosundaki kayitli boyut (0 = bilinmiyor).
func yedekBeklenenBoyut(db *sql.DB, dosya string) (int64, error) {
	var b int64
	err := db.QueryRow(`SELECT boyut_b FROM backups WHERE dosya=? ORDER BY id DESC LIMIT 1`, dosya).Scan(&b)
	return b, err
}

// indirmeKuyrugu: ayni yedek icin es zamanli/tekrarlanan indirmeleri engeller.
// (Kullanici "geri yukle"ye ust uste basarsa 5 GB'lik dosya birden fazla kez
// indirilmemeli.)
var indirmeKuyrugu sync.Map // dosya adi -> zaman.Time (baslangic)

// UzakIndirmeBaslat: yedegi ARKA PLANDA uzak hedeften indirir ve durum doner.
//
//	hazir=true  → dosya zaten yerelde ve dogru boyutta, hemen kullanilabilir
//	hazir=false → indirme baslatildi/suruyor; cagiran islemi kuyruga almali
//
// 🔴 Neden: senkron geri-yukleme ucu indirmeyi ISTEGIN ICINDE yapiyordu. 5 GB'lik
// bir yedekte bu dakikalar surer; tarayici/istemci cok daha once kopar ve
// kullaniciya "sunucu yanit vermiyor" gibi ALAKASIZ bir hata doner — oysa is
// arka planda surmektedir. Artik istek aninda doner, indirme arkada akar.
func UzakIndirmeBaslat(db *sql.DB, sk, dosya string) (hazir bool, suruyor bool, err error) {
	yol := filepath.Join(BackupRoot, sk, dosya)
	bek, _ := yedekBeklenenBoyut(db, dosya)
	if fi, e := os.Stat(yol); e == nil && (bek <= 0 || fi.Size() == bek) {
		return true, false, nil
	}
	g := genelAyarOku(context.Background(), db)
	if !g.UzakAktif || strings.TrimSpace(g.UzakHost) == "" {
		return false, false, fmt.Errorf("yedek dosyası diskte bulunamadı")
	}
	if _, yukleniyor := indirmeKuyrugu.LoadOrStore(dosya, time.Now()); yukleniyor {
		return false, true, nil // zaten iniyor
	}
	go func() {
		defer indirmeKuyrugu.Delete(dosya)
		if _, e := YerelDosyaHazirla(context.Background(), db, sk, dosya); e != nil {
			log.Printf("backup: %s arka plan indirme basarisiz: %v", dosya, e)
			bildirim.Yaz(db, "uyari", "yedek", "Yedek indirilemedi",
				dosya+": uzak hedeften indirilemedi — "+e.Error(), 0, "backup", 0)
			return
		}
		log.Printf("backup: %s arka plan indirme TAMAM, geri yukleme yapilabilir", dosya)
	}()
	return false, true, nil
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
// 🔴 `ls "<dosya>"` KULLANMA: Hetzner Storage Box gibi SFTP hedeflerinde tek dosya
// argumaniyla "Access failed: Not a directory" doner ve boyut hic okunamaz (-1) →
// dogrulama kapisi hep basarisiz olur, yerel kopya asla silinmez. `cls -l <dosya>`
// tek dosyanin uzun listesini verir ve bayt cinsinden boyutu icerir (olculdu).
// lftp `cls -s` cikti formatina bagli kalmamak icin `ls` ciktisindan sayisal alan
// ayiklanir; bulunamazsa -1.
func uzakBoyut(ctx context.Context, g *GenelAyar, uzakDizin, dosyaAdi string) int64 {
	betik := fmt.Sprintf(
		`set cmd:fail-exit yes; set sftp:auto-confirm yes; set net:max-retries 1; set net:timeout 20; `+
			`open -u "%s","%s" %s; cd "%s"; cls -l "%s"; bye`,
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

// dosyaDomainID: yedek dosya adindan domain_id (ilerleme kaydini bulmak icin).
func dosyaDomainID(db *sql.DB, dosya string) int64 {
	var id int64
	_ = db.QueryRow(`SELECT domain_id FROM backups WHERE dosya=? ORDER BY id DESC LIMIT 1`, dosya).Scan(&id)
	return id
}
