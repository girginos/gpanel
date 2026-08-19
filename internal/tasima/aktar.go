package tasima

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"girginospanel/internal/dns"
	"girginospanel/internal/hesaplar"
	"girginospanel/internal/kaynaklimit"
	"girginospanel/internal/phpsurum"
	"girginospanel/internal/provisioner"
)

// AktarSonuc — bir hesabin tasima ciktisi.
type AktarSonuc struct {
	DomainID  int64
	DosyaBayt int64
	DBSayisi  int
	DNSSayisi int
	Uyarilar  []string
}

// HesapAktar — TEK bir hesabi/domaini uctan uca tasir.
//
// Sira: hedef kontrolu → saglama → domains kaydi → FTP → dosyalar → veritabani
// (+ yapilandirma yeniden yazma) → DNS → SSL.
//
// 🔴 Veri guvenligi kurallari:
//   - Hedefte var olan bir domaine yazarken (ustune) rsync SILME YAPMAZ; mevcut
//     dosyalar korunur, kaynaktakiler uzerine yazilir.
//   - Veritabani parolasi domain basina BIR KEZ uretilir; mevcut DB kullanicisi
//     varsa parolasi DEGISTIRILMEZ (canli siteyi olduruyordu).
//   - Veritabani adimi basarisiz olursa kalem BASARILI sayilmaz.
func (h *Handlers) HesapAktar(ctx context.Context, k *Kaynak, hs Hesap, ay Ayarlar, log func(string, ...any)) (*AktarSonuc, error) {
	ctx, iptal := context.WithTimeout(ctx, hesapTimeout)
	defer iptal()

	sonuc := &AktarSonuc{}
	alanAdi := strings.ToLower(strings.TrimSpace(hs.AlanAdi))
	if !reAlanAdi.MatchString(alanAdi) || !strings.Contains(alanAdi, ".") {
		return nil, fmt.Errorf("gecersiz alan adi")
	}

	// --- 1. Hedef kontrolu -------------------------------------------------
	var mevcutID int64
	var mevcutSK, mevcutKok string
	err := h.DB.QueryRowContext(ctx,
		`SELECT id, sistem_kullanici, COALESCE(web_root,'') FROM domains WHERE alan_adi=?`,
		alanAdi).Scan(&mevcutID, &mevcutSK, &mevcutKok)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("hedef kontrolu: %w", err)
	}
	yeniOlusturuldu := false
	var sk, webRoot string
	php := ay.HedefPHP
	if php == "" {
		php = hs.PHPSurum
	}
	if php == "" {
		php = "8.3"
	}

	if mevcutID > 0 {
		if !ay.Ustune {
			return nil, fmt.Errorf("hedefte '%s' zaten var (uzerine yazma kapali)", alanAdi)
		}
		sonuc.DomainID, sk = mevcutID, mevcutSK
		// 🔴 Belge koku alt klasor olabilir (or. Laravel .../public_html/public).
		// public_html'e yazarsak icerik HIC YAYINLANMAZ.
		webRoot = mevcutKok
		if webRoot == "" {
			webRoot = filepath.Join("/home", sk, "public_html")
		}
		log("hedefte mevcut domain bulundu, uzerine yaziliyor (id=%d, kok=%s)", mevcutID, webRoot)
	} else {
		istenen := php
		php = kuruluPHPSec(php)
		if php != istenen {
			log("uyari: kaynak PHP %s hedefte kurulu degil → %s kullanilacak", istenen, php)
			sonuc.Uyarilar = append(sonuc.Uyarilar,
				fmt.Sprintf("PHP %s kurulu degil, %s ile saglandi", istenen, php))
		}
		log("sistem hesabi olusturuluyor (php %s)…", php)
		pr, err := provisioner.Provision(alanAdi, php)
		if err != nil {
			return nil, fmt.Errorf("saglama: %w", err)
		}
		sk = pr.SistemKullanici
		webRoot = pr.WebRoot
		yeniOlusturuldu = true

		dbUser, dbAdi := sk+"_db", sk+"_main"
		// 🔴 durum='pasif': surec yarida olurse (panel restart) yarim domain
		// "aktif" gorunmesin. Basarida 'aktif'e cevrilir.
		// Sahiplik hedefi geçerli mi (bayi=reseller users satırı, müşteri=customers).
		if err := h.sahiplikDogrula(ctx, ay.ResellerID, ay.CustomerID); err != nil {
			_ = provisioner.Deprovision(alanAdi, sk)
			return nil, err
		}
		res, err := h.DB.ExecContext(ctx,
			`INSERT INTO domains (alan_adi, sistem_kullanici, php_surum, ipv4, ftp_host,
			   db_host, db_user, db_adi, web_root, durum, plan_id, web_backend, reseller_id, customer_id)
			 VALUES (?,?,?,?,?,'localhost',?,?,?, 'pasif', NULLIF(?,0), 'php-fpm', ?, NULLIF(?,0))`,
			alanAdi, sk, php, h.sunucuIP(), h.sunucuIP(), dbUser, dbAdi, pr.WebRoot, ay.PlanID, ay.ResellerID, ay.CustomerID)
		if err != nil {
			_ = provisioner.Deprovision(alanAdi, sk)
			return nil, fmt.Errorf("domain kaydi: %w", err)
		}
		sonuc.DomainID, _ = res.LastInsertId()

		limCtx, limIptal := context.WithTimeout(context.Background(), 5*time.Minute)
		if err := kaynaklimit.UygulaHepsi(limCtx, h.DB, sonuc.DomainID); err != nil {
			log("uyari: kaynak limitleri uygulanamadi: %v", err)
			sonuc.Uyarilar = append(sonuc.Uyarilar, "kaynak limitleri uygulanamadi")
		}
		limIptal()

		if uid, gid, err := uidGid(sk); err == nil {
			if err := hesaplar.FTPCreate(h.DB, sonuc.DomainID, sk, hesaplar.RandomParola(16), uid, gid); err != nil {
				log("uyari: FTP hesabi olusturulamadi: %v", err)
			}
		}
	}

	basarili := false
	defer func() {
		if basarili {
			return
		}
		if yeniOlusturuldu {
			log("hata olustu — olusturulan hesap geri aliniyor…")
			_, _ = h.DB.Exec(`DELETE FROM domains WHERE id=?`, sonuc.DomainID)
			_ = provisioner.Deprovision(alanAdi, sk)
		}
	}()

	// --- 2. Dosyalar -------------------------------------------------------
	if ay.Dosyalar {
		uzak := strings.TrimSpace(hs.WebRoot)
		if !gecerliUzakYol(uzak) {
			return nil, fmt.Errorf("kaynak web kok dizini gecersiz")
		}
		if err := os.MkdirAll(webRoot, 0o750); err != nil {
			return nil, fmt.Errorf("hedef dizin: %w", err)
		}
		log("dosyalar aktariliyor: %s → %s", uzak, webRoot)
		if _, err := k.RsyncCek(ctx, uzak+"/", webRoot+"/",
			"--exclude=.git/", "--exclude=*.sock", "--exclude=.cpanel/"); err != nil {
			return nil, fmt.Errorf("dosya aktarimi: %w", err)
		}
		if b, err := dizinBoyut(webRoot); err == nil {
			sonuc.DosyaBayt = b
		}
		_ = exec.CommandContext(ctx, "chown", "-R", sk+":"+sk, webRoot).Run()
		_ = exec.CommandContext(ctx, "restorecon", "-RF", webRoot).Run()
		log("dosyalar tamam (%.1f MB)", float64(sonuc.DosyaBayt)/(1024*1024))
	}

	// --- 3. Veritabanlari --------------------------------------------------
	// 🔴 YEDEK KEŞIF: Kesif DB listesini boş bıraktıysa (ek/alt alan adı — DB'ler
	// yalnız ana alana atanır — ya da Plesk DB sorgusu sessizce boş döndü), KOPYALANAN
	// yapılandırmadan (wp-config.php/.env/…) gerçek DB adını çıkar. Bu ad kaynakta da
	// aynıdır → dökümü oradan alınır. Böylece "SQL hiç taşınmadı, kalem yeşil" biter.
	if ay.Veritabani && len(hs.DBler) == 0 && ay.Dosyalar {
		if bulunan := configtenDBBul(webRoot); len(bulunan) > 0 {
			log("kesif DB bulmadi; yapilandirmadan %d DB adi cikarildi: %v", len(bulunan), bulunan)
			hs.DBler = bulunan
		}
	}
	if ay.Veritabani && len(hs.DBler) > 0 {
		esleme, dbPw, dbHata := h.veritabanlariniAktar(ctx, k, hs, sk, sonuc, log)
		if dbHata != nil {
			// 🔴 DB adimi basarisizsa kalem BASARILI sayilmaz — sessiz basari
			// mustericinin sitesini bos veritabaniyla yayina alir.
			return nil, dbHata
		}
		if n := h.configYenidenYaz(webRoot, esleme, hs.KaynakHesap, dbPw, log); n > 0 {
			log("%d yapilandirma dosyasi guncellendi (DB baglantisi)", n)
		}
	} else if ay.Veritabani {
		// 🔴 SESSİZ GEÇME: DB istendi ama hiç DB bulunamadı → kullanıcı "SQL taşındı"
		// sanmasın. Görünür uyarı bırak (kalem "Not" sütunu + log).
		log("uyari: bu site icin kaynakta veritabani bulunamadi; SQL tasinmadi")
		sonuc.Uyarilar = append(sonuc.Uyarilar,
			"veritabani tasinmadi: kaynakta bu site icin DB bulunamadi (ek/alt alan adiysa DB ana alan adiyla tasinir; ya da kesif DB'yi goremedi)")
	}

	// --- 4. DNS ------------------------------------------------------------
	if ay.DNS {
		n, err := h.dnsAktar(ctx, k, sonuc.DomainID, alanAdi, log)
		if err != nil {
			log("uyari: DNS aktarilamadi (varsayilan sablon kullanildi): %v", err)
			sonuc.Uyarilar = append(sonuc.Uyarilar, "DNS varsayilan sablonla olusturuldu")
		}
		sonuc.DNSSayisi = n
	}

	// --- 5. SSL ------------------------------------------------------------
	if ay.SSL {
		log("SSL sertifikasi isteniyor…")
		crt, key, gercek := provisioner.OtoSSLDene(alanAdi, sk, kuruluPHPSec(php), "php-fpm")
		if crt != "" {
			kaynakAd := "self-signed"
			if gercek {
				kaynakAd = "letsencrypt"
			}
			_, _ = h.DB.ExecContext(ctx,
				`UPDATE domains SET ssl_aktif=1, ssl_kaynak=?, cert_path=?, key_path=? WHERE id=?`,
				kaynakAd, crt, key, sonuc.DomainID)
			log("SSL: %s", kaynakAd)
			if !gercek {
				sonuc.Uyarilar = append(sonuc.Uyarilar,
					"SSL self-signed — DNS bu sunucuya yonlendikten sonra yenileyin")
			}
		} else {
			sonuc.Uyarilar = append(sonuc.Uyarilar, "SSL alinamadi")
		}
	}

	if yeniOlusturuldu {
		_, _ = h.DB.ExecContext(ctx, `UPDATE domains SET durum='aktif' WHERE id=?`, sonuc.DomainID)
	}
	basarili = true
	return sonuc, nil
}

type dbHedef struct{ Ad, Kul string }

// veritabanlariniAktar — tum kaynak DB'lerini tasir.
//
// 🔴 Parola domain basina BIR KEZ uretilir ve kullanici YOKSA atanir. Mevcut
// kullanicinin parolasi DEGISTIRILMEZ: hesaplar.MySQLCreateDB kosulsuz
// "ALTER USER … IDENTIFIED BY" calistiriyor; bu, ayni kullaniciyi paylasan
// CANLI sitelerin baglantisini aninda koparir.
func (h *Handlers) veritabanlariniAktar(ctx context.Context, k *Kaynak, hs Hesap, sk string,
	sonuc *AktarSonuc, log func(string, ...any)) (map[string]dbHedef, string, error) {

	hedefKul := sk + "_db"
	// <sk>_db bu domaine OZEL kullanicidir (sk domain basina benzersiz) → parolasini
	// bilinen yeni bir degere ayarlamak GUVENLI ve ZORUNLU: aksi halde wp-config'e
	// yazacak parolamiz olmaz ve site "Access denied" verir. MySQLCreateDB, kullanici
	// varsa CREATE-OR-ALTER ile parolayi dbPw'ye esitler.
	dbPw := hesaplar.RandomParola(24)

	esleme := map[string]dbHedef{}
	kuruldu := false
	var basarisiz []string

	for _, kaynakDB := range hs.DBler {
		if !reDBAd.MatchString(kaynakDB) {
			continue
		}
		hedefAd, err := h.benzersizHedefDB(ctx, sk, kaynakDB, hs.KaynakHesap)
		if err != nil {
			log("uyari: %s icin hedef ad uretilemedi: %v", kaynakDB, err)
			basarisiz = append(basarisiz, kaynakDB)
			continue
		}
		log("veritabani: %s → %s", kaynakDB, hedefAd)

		if !kuruldu {
			if err := hesaplar.MySQLCreateDB(h.DB, sonuc.DomainID, hedefAd, hedefKul, dbPw); err != nil {
				log("uyari: %s olusturulamadi: %v", hedefAd, err)
				basarisiz = append(basarisiz, kaynakDB)
				continue
			}
			kuruldu = true
		} else if err := hesaplar.MySQLCreateDBForUser(h.DB, sonuc.DomainID, hedefAd, hedefKul); err != nil {
			log("uyari: %s olusturulamadi: %v", hedefAd, err)
			basarisiz = append(basarisiz, kaynakDB)
			continue
		}

		if err := h.dbAktar(ctx, k, kaynakDB, hedefAd, log); err != nil {
			log("HATA: %s aktarilamadi: %v", kaynakDB, err)
			basarisiz = append(basarisiz, kaynakDB)
			continue
		}
		sonuc.DBSayisi++
		esleme[kaynakDB] = dbHedef{Ad: hedefAd, Kul: hedefKul}
	}

	if len(basarisiz) > 0 {
		return esleme, dbPw, fmt.Errorf("veritabani aktarilamadi: %s", strings.Join(basarisiz, ", "))
	}
	return esleme, dbPw, nil
}

func (h *Handlers) dbKullanicisiVarMi(ctx context.Context, kul string) bool {
	out, err := exec.CommandContext(ctx, "mysql", "-N", "-B", "-e",
		"SELECT COUNT(*) FROM mysql.user WHERE user='"+kul+"' AND host='localhost'").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != "0"
}

// benzersizHedefDB — "olduser_wp" → "<sk>_wp". 64 karakter sinirinda KIRPMAK
// yerine cakisma denetler ve sayac eki verir; kirpma iki farkli kaynak DB'yi
// ayni hedefe indirip birini sessizce dusuruyordu.
func (h *Handlers) benzersizHedefDB(ctx context.Context, sk, kaynakDB, kaynakHesap string) (string, error) {
	sonek := kaynakDB
	if kaynakHesap != "" && strings.HasPrefix(kaynakDB, kaynakHesap+"_") {
		sonek = strings.TrimPrefix(kaynakDB, kaynakHesap+"_")
	}
	sonek = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			return r
		}
		return '_'
	}, sonek)
	if sonek == "" {
		sonek = "db"
	}
	temel := sk + "_" + sonek
	for i := 0; i < 50; i++ {
		aday := temel
		if i > 0 {
			aday = fmt.Sprintf("%s_%d", temel, i+1)
		}
		if len(aday) > 64 {
			kes := 64 - len(aday) + len(temel)
			if kes < 1 {
				return "", fmt.Errorf("ad cok uzun")
			}
			aday = temel[:kes]
			if i > 0 {
				aday = fmt.Sprintf("%s_%d", temel[:kes-2], i+1)
			}
		}
		// Benzersizlik HEM gercek sema HEM panel kaydindan (db_accounts) kontrol
		// edilmeli: MySQLCreateDB, db_accounts.db_name UNIQUE'e carpar; yalniz
		// information_schema bakmak yeniden-tasimada 1062 Duplicate verir.
		var n int
		_ = h.DB.QueryRowContext(ctx,
			`SELECT (SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name=?)
			      + (SELECT COUNT(*) FROM db_accounts WHERE db_name=?)`, aday, aday).Scan(&n)
		if n == 0 {
			return aday, nil
		}
	}
	return "", fmt.Errorf("benzersiz veritabani adi uretilemedi")
}

// ---------------------------------------------------------------------------
// Veritabani aktarimi
// ---------------------------------------------------------------------------

const (
	maxDumpBayt   int64 = 8 << 30
	maxAcilimBayt int64 = 64 << 30
	dumpBitisIz         = "Dump completed"
)

// dbAktar — uzak dump'i indirir ve KISITLI bir MySQL kullanicisiyla import eder.
//
// 🔴 Iki tuzak:
//  1. "mysqldump | gzip" boru hattinda kabuk varsayilan olarak SON komutun
//     cikis kodunu dondurur → mysqldump cokse bile exit 0, sonuc "basarili"
//     ama veritabani BOS. Bu yuzden pipefail ZORUNLU + dump sonu damgasi
//     dogrulanir.
//  2. Import ROOT ile yapilirsa dusman dump hedef DB sinirini asar.
func (h *Handlers) dbAktar(ctx context.Context, k *Kaynak, kaynakDB, hedefDB string, log func(string, ...any)) error {
	if !reDBAd.MatchString(kaynakDB) || !reDBAd.MatchString(hedefDB) {
		return fmt.Errorf("gecersiz veritabani adi")
	}
	tmp, err := os.CreateTemp("/var/tmp", "gosp_tasima_*.sql.gz")
	if err != nil {
		return err
	}
	tmpAd := tmp.Name()
	defer os.Remove(tmpAd)

	ic := "mysqldump --single-transaction --quick --routines --triggers " +
		"--no-tablespaces --default-character-set=utf8mb4 " + shQuote(kaynakDB) + " | gzip -c"
	// pipefail icin bash zorlanir; yoksa sh'e duser (o zaman dump-sonu damgasi
	// tek koruma olarak kalir).
	uzak := "if command -v bash >/dev/null 2>&1; then bash -o pipefail -c " + shQuote(ic) +
		"; else " + ic + "; fi"

	anahtar, temizle, err := k.anahtarYaz()
	if err != nil {
		tmp.Close()
		return err
	}
	defer temizle()

	args := k.sshOrtakArgs(anahtar)
	args = append(args, "-l", k.Kullanici, "--", k.Host, uzak)

	var cmd *exec.Cmd
	if anahtar == "" {
		cmd = exec.CommandContext(ctx, "sshpass", append([]string{"-e", "ssh"}, args...)...)
		cmd.Env = append(os.Environ(), "SSHPASS="+k.Parola)
	} else {
		cmd = exec.CommandContext(ctx, "ssh", args...)
		cmd.Env = os.Environ()
	}
	var eb strings.Builder
	sayan := &sinirliYazici{alt: tmp, kalan: maxDumpBayt}
	cmd.Stdout = sayan
	cmd.Stderr = &eb
	runErr := cmd.Run()
	tmp.Close()
	if sayan.asildi {
		return fmt.Errorf("dump boyut sinirini asti (%d GB)", maxDumpBayt>>30)
	}
	if runErr != nil {
		return fmt.Errorf("dump: %s", kisalt(temizHata(eb.String(), k.Parola), 200))
	}
	st, err := os.Stat(tmpAd)
	if err != nil || st.Size() == 0 {
		return fmt.Errorf("dump bos dondu")
	}

	impKul, impPw, err := h.geciciImportKullanicisi(ctx, hedefDB)
	if err != nil {
		return fmt.Errorf("import kullanicisi: %w", err)
	}
	defer h.importKullanicisiDusur(impKul)

	cnfAd, err := cnfYaz(impKul, impPw)
	if err != nil {
		return err
	}
	defer os.Remove(cnfAd)

	f, err := os.Open(tmpAd)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("dump okunamadi (gzip): %w", err)
	}
	defer gz.Close()

	imp := exec.CommandContext(ctx, "mysql", "--defaults-extra-file="+cnfAd,
		"--default-character-set=utf8mb4", hedefDB)
	filtre := &dumpFiltre{}
	imp.Stdin = filtre.Sar(io.LimitReader(gz, maxAcilimBayt))
	var ieb strings.Builder
	imp.Stderr = &ieb
	if err := imp.Run(); err != nil {
		return fmt.Errorf("import: %s", kisalt(temizHata(ieb.String(), ""), 200))
	}
	// 🔴 mysqldump ciktisini "-- Dump completed" ile bitirir. Damga yoksa dump
	// yarim kalmistir (uzak hata, kopan baglanti, kilitli tablo…).
	if !filtre.Tamam {
		return fmt.Errorf("dump yarim kaldi (kaynak sunucuda mysqldump hata verdi)")
	}
	return nil
}

func cnfYaz(kul, pw string) (string, error) {
	c, err := os.CreateTemp("/var/tmp", "gosp_imp_*.cnf")
	if err != nil {
		return "", err
	}
	ad := c.Name()
	_ = c.Chmod(0o600)
	_, err = c.WriteString("[client]\nuser=" + kul + "\npassword=" + pw +
		"\nsocket=/var/lib/mysql/mysql.sock\n")
	c.Close()
	if err != nil {
		os.Remove(ad)
		return "", err
	}
	return ad, nil
}

type sinirliYazici struct {
	alt    *os.File
	kalan  int64
	asildi bool
}

func (w *sinirliYazici) Write(p []byte) (int, error) {
	if w.asildi {
		return 0, fmt.Errorf("boyut siniri asildi")
	}
	if int64(len(p)) > w.kalan {
		w.asildi = true
		return 0, fmt.Errorf("boyut siniri asildi")
	}
	n, err := w.alt.Write(p)
	w.kalan -= int64(n)
	return n, err
}

var reDefiner = regexp.MustCompile("(?i)/\\*![0-9]* *DEFINER *= *[^*]*\\*/|DEFINER *= *`[^`]*`@`[^`]*`")

// dumpFiltre — DEFINER yan tumcelerini soker (kisitli import kullanicisi
// baskasi adina DEFINER atayamaz) ve dump-sonu damgasini izler.
type dumpFiltre struct{ Tamam bool }

func (d *dumpFiltre) Sar(r io.Reader) io.Reader {
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 1<<20), 32<<20)
		for sc.Scan() {
			satir := sc.Text()
			if strings.Contains(satir, dumpBitisIz) {
				d.Tamam = true
			}
			if _, err := pw.Write([]byte(reDefiner.ReplaceAllString(satir, "") + "\n")); err != nil {
				return
			}
		}
		if err := sc.Err(); err != nil {
			_ = pw.CloseWithError(err)
		}
	}()
	return pr
}

// geciciImportKullanicisi — YALNIZ hedefDB uzerinde yetkili gecici kullanici.
func (h *Handlers) geciciImportKullanicisi(ctx context.Context, hedefDB string) (string, string, error) {
	if !reDBAd.MatchString(hedefDB) {
		return "", "", fmt.Errorf("gecersiz hedef veritabani")
	}
	ham := make([]byte, 9)
	if _, err := rand.Read(ham); err != nil {
		return "", "", err
	}
	kul := "gosp_imp_" + hex.EncodeToString(ham)[:12]
	pw := hesaplar.RandomParola(28)
	for _, q := range []string{
		"CREATE USER '" + kul + "'@'localhost' IDENTIFIED BY '" + pw + "'",
		"GRANT ALL PRIVILEGES ON `" + hedefDB + "`.* TO '" + kul + "'@'localhost'",
		"FLUSH PRIVILEGES",
	} {
		c := exec.CommandContext(ctx, "mysql", "-e", q)
		var eb strings.Builder
		c.Stderr = &eb
		if err := c.Run(); err != nil {
			_ = exec.Command("mysql", "-e", "DROP USER IF EXISTS '"+kul+"'@'localhost'").Run()
			return "", "", fmt.Errorf("%s", kisalt(temizHata(eb.String(), ""), 150))
		}
	}
	return kul, pw, nil
}

func (h *Handlers) importKullanicisiDusur(kul string) {
	if kul == "" {
		return
	}
	_ = exec.Command("mysql", "-e", "DROP USER IF EXISTS '"+kul+"'@'localhost'").Run()
}

// ---------------------------------------------------------------------------
// Yapilandirma dosyasi yeniden yazma
// ---------------------------------------------------------------------------

// dbAnahtarlari — degeri DB ADI olan yapilandirma anahtarlari.
var dbAdAnahtarlari = []string{"DB_NAME", "DB_DATABASE", "DATABASE", "dbname", "database", "db"}

// dbKullaniciAnahtarlari — degeri DB KULLANICISI olan anahtarlar.
var dbKulAnahtarlari = []string{"DB_USER", "DB_USERNAME", "DATABASE_USER"}

// dbParolaAnahtarlari — degeri DB PAROLASI olan anahtarlar.
var dbPwAnahtarlari = []string{"DB_PASSWORD", "DB_PASS", "DATABASE_PASSWORD"}

// yapilandirmaAdaylari — DB baglanti bilgisi barindiran bilinen yapilandirma dosyalari.
var yapilandirmaAdaylari = []string{
	"wp-config.php", ".env", "configuration.php", "config.php",
	"app/etc/env.php", "sites/default/settings.php", "config/db.php",
	"application/config/database.php", "includes/config.php",
}

// configtenDBBul — kopyalanan site yapilandirmasindan (wp-config.php/.env/…) DB
// ADLARINI cikarir. Kesif DB listesi bos kaldiginda YEDEK yol: gercek DB adi
// yapilandirmada yazilidir ve kaynakta da AYNI isimdir → dokumu oradan alinir.
// Yalniz reDBAd'e uyan, sistemDB olmayan adlar dondurulur (guvenlik).
func configtenDBBul(webRoot string) []string {
	gorulen := map[string]bool{}
	var out []string
	for _, rel := range yapilandirmaAdaylari {
		yol := filepath.Join(webRoot, rel)
		st, err := os.Lstat(yol)
		if err != nil || !st.Mode().IsRegular() || st.Size() > 4<<20 {
			continue
		}
		ham, err := os.ReadFile(yol)
		if err != nil {
			continue
		}
		for _, satir := range strings.Split(string(ham), "\n") {
			m := reAnahtarSatir.FindStringSubmatch(satir)
			if m == nil {
				continue
			}
			dbAnahtar := false
			for _, a := range dbAdAnahtarlari {
				if strings.EqualFold(m[2], a) {
					dbAnahtar = true
					break
				}
			}
			if !dbAnahtar {
				continue
			}
			deger, _ := degerAyikla(m[3])
			deger = strings.TrimSpace(deger)
			if deger == "" || gorulen[deger] || !reDBAd.MatchString(deger) || sistemDB(deger) {
				continue
			}
			gorulen[deger] = true
			out = append(out, deger)
		}
	}
	return out
}

// configYenidenYaz — site yapilandirmasindaki DB baglanti bilgilerini gunceller.
//
// 🔴 Yalnizca BILINEN ANAHTARLARIN degeri degistirilir. Onceki surum metinde
// "<hesap>_" gecen her yeri degistiriyordu: DB adini kullanici adiyla eziyor ve
// icinde apostrof olan PHP dosyalarini sozdizimsel olarak BOZUYORDU.
func (h *Handlers) configYenidenYaz(webRoot string, esleme map[string]dbHedef, kaynakHesap, yeniPw string, log func(string, ...any)) int {
	if len(esleme) == 0 {
		return 0
	}
	var hedefKul string
	adEsleme := map[string]string{}
	for eski, hedef := range esleme {
		adEsleme[eski] = hedef.Ad
		hedefKul = hedef.Kul
	}

	sayac := 0
	for _, rel := range yapilandirmaAdaylari {
		yol := filepath.Join(webRoot, rel)
		st, err := os.Lstat(yol)
		if err != nil || !st.Mode().IsRegular() || st.Size() > 4<<20 {
			continue
		}
		ham, err := os.ReadFile(yol)
		if err != nil {
			continue
		}
		yeni := string(ham)
		for _, a := range dbAdAnahtarlari {
			yeni = anahtarDegeriDegistirMap(yeni, a, adEsleme)
		}
		if hedefKul != "" {
			for _, a := range dbKulAnahtarlari {
				yeni = anahtarDegeriDegistir(yeni, a, hedefKul, "")
			}
		}
		if yeniPw != "" {
			for _, a := range dbPwAnahtarlari {
				yeni = anahtarDegeriDegistir(yeni, a, yeniPw, "")
			}
		}
		if yeni == string(ham) {
			continue
		}
		if err := yedekYaz(webRoot, rel, ham); err != nil {
			log("uyari: %s yedegi alinamadi, dosya degistirilmedi: %v", rel, err)
			continue
		}
		if err := atomikYaz(yol, []byte(yeni), st); err == nil {
			sayac++
			log("yapilandirma guncellendi: %s", rel)
		}
	}
	return sayac
}

// atomikYaz — ayni dizine gecici yaz + rename. Yarida cokme yarim
// wp-config.php birakmasin; sahiplik/izin korunur.
func atomikYaz(yol string, veri []byte, st os.FileInfo) error {
	dizin := filepath.Dir(yol)
	tmp, err := os.CreateTemp(dizin, ".gosp_cfg_*")
	if err != nil {
		return err
	}
	ad := tmp.Name()
	if _, err := tmp.Write(veri); err != nil {
		tmp.Close()
		os.Remove(ad)
		return err
	}
	tmp.Close()
	_ = os.Chmod(ad, st.Mode().Perm())
	if sys, ok := st.Sys().(*syscall.Stat_t); ok {
		_ = os.Chown(ad, int(sys.Uid), int(sys.Gid))
	}
	if err := os.Rename(ad, yol); err != nil {
		os.Remove(ad)
		return err
	}
	return nil
}

var reAnahtarSatir = regexp.MustCompile(`^(\s*)(?:define\s*\(\s*)?['"]?([A-Za-z_][A-Za-z0-9_]*)['"]?\s*(?:,|=>|=|:)\s*(.*)$`)

// anahtarDegeriDegistir — "<anahtar> = <deger>" bicimindeki satirlarda degeri
// yenisiyle degistirir. Tirnak turu korunur. onekSarti verilirse yalnizca o
// onekle baslayan degerler degistirilir (yanlislikla baska degeri ezmemek icin).
func anahtarDegeriDegistir(metin, anahtar, yeni, onekSarti string) string {
	satirlar := strings.Split(metin, "\n")
	for i, s := range satirlar {
		m := reAnahtarSatir.FindStringSubmatch(s)
		if m == nil || !strings.EqualFold(m[2], anahtar) {
			continue
		}
		eskiDeger, tirnak := degerAyikla(m[3])
		if eskiDeger == "" {
			continue
		}
		if onekSarti != "" && !strings.HasPrefix(eskiDeger, onekSarti+"_") && eskiDeger != onekSarti {
			continue
		}
		satirlar[i] = strings.Replace(s, tirnak+eskiDeger+tirnak, tirnak+yeni+tirnak, 1)
	}
	return strings.Join(satirlar, "\n")
}

// anahtarDegeriDegistirMap — degeri eslemede bulunan anahtarlari degistirir.
func anahtarDegeriDegistirMap(metin, anahtar string, esleme map[string]string) string {
	satirlar := strings.Split(metin, "\n")
	for i, s := range satirlar {
		m := reAnahtarSatir.FindStringSubmatch(s)
		if m == nil || !strings.EqualFold(m[2], anahtar) {
			continue
		}
		eskiDeger, tirnak := degerAyikla(m[3])
		yeni, ok := esleme[eskiDeger]
		if !ok || eskiDeger == "" {
			continue
		}
		satirlar[i] = strings.Replace(s, tirnak+eskiDeger+tirnak, tirnak+yeni+tirnak, 1)
	}
	return strings.Join(satirlar, "\n")
}

// degerAyikla — "'abc' );" → ("abc","'") · "abc" → ("abc","")
func degerAyikla(kalan string) (string, string) {
	kalan = strings.TrimSpace(kalan)
	if kalan == "" {
		return "", ""
	}
	if kalan[0] == '\'' || kalan[0] == '"' {
		t := string(kalan[0])
		son := strings.Index(kalan[1:], t)
		if son < 0 {
			return "", ""
		}
		return kalan[1 : 1+son], t
	}
	// tirnaksiz: satir sonuna / ayraca kadar
	son := strings.IndexAny(kalan, " \t;,)#")
	if son >= 0 {
		kalan = kalan[:son]
	}
	return strings.TrimSpace(kalan), ""
}

// ---------------------------------------------------------------------------
// DNS aktarimi
// ---------------------------------------------------------------------------

// kaynakUstunTipler — kaynagin degeri panel varsayilanini EZMELIDIR. MX/TXT
// (SPF, DKIM, DMARC) mustericinin e-posta akisidir; varsayilanla degistirilirse
// tasima aninda tum e-posta durur.
var kaynakUstunTipler = map[string]bool{"MX": true, "TXT": true, "CNAME": true, "SRV": true, "CAA": true}

func (h *Handlers) dnsAktar(ctx context.Context, k *Kaynak, domainID int64, alanAdi string, log func(string, ...any)) (int, error) {
	if _, err := dns.SeedDefaults(ctx, h.DB, domainID, alanAdi, h.sunucuIP()); err != nil {
		log("uyari: DNS varsayilanlari yazilamadi: %v", err)
	}

	q := shQuote(alanAdi)
	var kayitlar []zoneKayit

	// Plesk DNS'i zone DOSYASINDA degil kendi deposunda tutar → `plesk bin dns
	// --info` ile cekilir (FQDN TYPE [pri] deger bicimi). cPanel/DA icin ham
	// BIND zone dosyasi okunur.
	if k.Tip == "plesk" {
		if ham, err := k.Calistir(ctx, "plesk bin dns --info "+q+" 2>/dev/null"); err == nil {
			kayitlar = pleskDNSAyristir(ham, alanAdi)
		}
	}
	if len(kayitlar) == 0 {
		komut := "cat /var/named/" + q + ".db 2>/dev/null || " +
			"cat /var/named/run-root/var/named/" + q + ".db 2>/dev/null || " +
			"cat /var/lib/named/var/named/" + q + ".db 2>/dev/null || " +
			"cat /etc/bind/db." + q + " 2>/dev/null || " +
			"cat /var/named/data/" + q + ".db 2>/dev/null"
		if ham, err := k.Calistir(ctx, komut); err == nil && strings.TrimSpace(ham) != "" {
			kayitlar = zoneAyristir(ham, alanAdi)
		}
	}
	if len(kayitlar) == 0 {
		if err := dns.WriteZone(ctx, h.DB, domainID); err != nil {
			return 0, err
		}
		return 0, fmt.Errorf("kaynak DNS kayitlari okunamadi")
	}
	yeniIP := h.sunucuIP()
	eskiIP := ""
	for _, kt := range kayitlar {
		if kt.Tip == "A" && kt.Ad == "@" {
			eskiIP = kt.Deger
			break
		}
	}
	if eskiIP == "" && net.ParseIP(k.Host) != nil {
		eskiIP = k.Host
	}

	eklenen := 0
	temizlenen := map[string]bool{}
	for _, kt := range kayitlar {
		if kt.Tip == "AAAA" || kt.Tip == "NS" {
			continue // eski IPv6 gecersiz; NS panelin kendi sunuculari olmali
		}
		deger := kt.Deger
		if kt.Tip == "A" && (deger == eskiIP || kt.Ad == "@" || kt.Ad == "www") {
			deger = yeniIP
		}
		anahtar := kt.Ad + "|" + kt.Tip
		if kt.Tip == "CNAME" {
			// 🔴 CNAME bir isim icin TEK kayit olmali (RFC 1034): ayni isimde A/TXT
			// vb. OLAMAZ. SeedDefaults'un olusturdugu 'www A' gibi kayitlari da sil,
			// yoksa named-checkzone "CNAME and other data" ile zone'u REDDEDER.
			if !temizlenen[kt.Ad+"|*"] {
				_, _ = h.DB.ExecContext(ctx,
					`DELETE FROM dns_records WHERE domain_id=? AND ad=?`, domainID, kt.Ad)
				temizlenen[kt.Ad+"|*"] = true
			}
		} else if kaynakUstunTipler[kt.Tip] {
			// Kaynak bu (ad,tip) icin kayit veriyorsa panel varsayilanini bir
			// kez temizle, sonra kaynagin TUM kayitlarini ekle.
			if !temizlenen[anahtar] {
				_, _ = h.DB.ExecContext(ctx,
					`DELETE FROM dns_records WHERE domain_id=? AND ad=? AND tip=?`,
					domainID, kt.Ad, kt.Tip)
				temizlenen[anahtar] = true
			}
		} else {
			// CNAME olan bir isme baska kayit EKLEME (cakisma).
			var cn int
			_ = h.DB.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM dns_records WHERE domain_id=? AND ad=? AND tip='CNAME'`,
				domainID, kt.Ad).Scan(&cn)
			if cn > 0 {
				continue
			}
			var v int
			_ = h.DB.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM dns_records WHERE domain_id=? AND ad=? AND tip=?`,
				domainID, kt.Ad, kt.Tip).Scan(&v)
			if v > 0 {
				continue
			}
		}
		var ayni int
		_ = h.DB.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM dns_records WHERE domain_id=? AND ad=? AND tip=? AND deger=?`,
			domainID, kt.Ad, kt.Tip, deger).Scan(&ayni)
		if ayni > 0 {
			continue
		}
		if _, err := h.DB.ExecContext(ctx,
			`INSERT INTO dns_records (domain_id, ad, tip, deger, ttl, oncelik, aktif)
			 VALUES (?,?,?,?,?,?,1)`,
			domainID, kt.Ad, kt.Tip, deger, kt.TTL, kt.Oncelik); err == nil {
			eklenen++
		}
	}
	if err := dns.WriteZone(ctx, h.DB, domainID); err != nil {
		return eklenen, fmt.Errorf("zone yazilamadi: %w", err)
	}
	log("DNS: %d kayit aktarildi", eklenen)
	return eklenen, nil
}

type zoneKayit struct {
	Ad, Tip, Deger string
	TTL, Oncelik   int
}

var tasinabilirTipler = map[string]bool{
	"A": true, "CNAME": true, "MX": true, "TXT": true, "SRV": true, "CAA": true,
}

// pleskDNSAyristir — `plesk bin dns --info <domain>` ciktisini kayitlara cevirir.
// Bicim satir basina: "<fqdn>. <TYPE> [<pri> [<weight> <port>]] <deger>"
func pleskDNSAyristir(ham, alanAdi string) []zoneKayit {
	var out []zoneKayit
	for _, satir := range strings.Split(ham, "\n") {
		f := strings.Fields(strings.TrimSpace(satir))
		if len(f) < 3 {
			continue
		}
		tip := strings.ToUpper(f[1])
		if !tasinabilirTipler[tip] {
			continue
		}
		ad := strings.TrimSuffix(f[0], ".")
		ad = strings.TrimSuffix(ad, "."+alanAdi)
		if ad == alanAdi || ad == "" {
			ad = "@"
		}
		oncelik := 0
		var deger string
		switch tip {
		case "MX":
			if len(f) < 4 {
				continue
			}
			oncelik, _ = strconv.Atoi(f[2])
			deger = strings.TrimSuffix(f[3], ".")
		case "SRV":
			if len(f) < 6 {
				continue
			}
			oncelik, _ = strconv.Atoi(f[2])
			deger = f[3] + " " + f[4] + " " + strings.TrimSuffix(f[5], ".")
		case "CNAME":
			deger = strings.TrimSuffix(f[2], ".")
		default: // A, AAAA, TXT, CAA
			deger = strings.Join(f[2:], " ")
		}
		if deger == "" || len(deger) > 2048 || len(ad) > 100 {
			continue
		}
		out = append(out, zoneKayit{Ad: ad, Tip: tip, Deger: deger, TTL: 3600, Oncelik: oncelik})
	}
	return out
}

// zoneAyristir — BIND zone metnini kayitlara cevirir.
//
// 🔴 Yorum ayiklama TIRNAK FARKINDA olmali: DMARC/DKIM/SPF degerlerinde ';'
// VERIDIR. Onceki surum ilk ';' karakterinde kesip "v=DMARC1" birakiyordu.
// Ayrica bolunmus tirnakli dizeler ("v=DKIM1;" "p=MIG…") birlestirilir.
func zoneAyristir(ham, alanAdi string) []zoneKayit {
	var out []zoneKayit
	sonAd := "@"
	parenDerinlik := 0

	for _, hamSatir := range strings.Split(ham, "\n") {
		satir := yorumAyikla(hamSatir)
		if parenDerinlik > 0 {
			parenDerinlik += strings.Count(satir, "(") - strings.Count(satir, ")")
			continue // SOA gibi cok satirli blogun govdesi
		}
		if strings.TrimSpace(satir) == "" || strings.HasPrefix(strings.TrimSpace(satir), "$") {
			continue
		}
		if strings.Contains(strings.ToUpper(satir), "SOA") {
			parenDerinlik += strings.Count(satir, "(") - strings.Count(satir, ")")
			continue
		}
		if a := strings.Count(satir, "(") - strings.Count(satir, ")"); a > 0 {
			parenDerinlik += a
			continue
		}

		alanlar := strings.Fields(satir)
		if len(alanlar) < 3 {
			continue
		}
		ad := sonAd
		i := 0
		if !strings.HasPrefix(hamSatir, " ") && !strings.HasPrefix(hamSatir, "\t") {
			ad = alanlar[0]
			i = 1
		}
		ttl := 3600
		for ; i < len(alanlar); i++ {
			u := strings.ToUpper(alanlar[i])
			if u == "IN" {
				continue
			}
			if n, err := strconv.Atoi(alanlar[i]); err == nil {
				ttl = n
				continue
			}
			break
		}
		if i >= len(alanlar) {
			continue
		}
		tip := strings.ToUpper(alanlar[i])

		// Ad normalizasyonu — desteklenmeyen tipte de guncellenmeli, yoksa
		// sonraki girintili satir YANLIS sahibe yazilir.
		adNorm := strings.TrimSuffix(ad, ".")
		adNorm = strings.TrimSuffix(adNorm, "."+alanAdi)
		if adNorm == alanAdi || adNorm == "" {
			adNorm = "@"
		}
		sonAd = adNorm

		if !tasinabilirTipler[tip] {
			continue
		}
		kalan := alanlar[i+1:]
		if len(kalan) == 0 {
			continue
		}
		oncelik := 0
		if tip == "MX" || tip == "SRV" {
			if n, err := strconv.Atoi(kalan[0]); err == nil {
				oncelik = n
				kalan = kalan[1:]
			}
		}
		if len(kalan) == 0 {
			continue
		}
		// Tek satirda tamamlanan parantezli rdata:  ( "v=DKIM1;" "p=MIG…" )
		deger := tirnakBirlestir(parenSoy(strings.Join(kalan, " ")))
		if deger == "" || len(deger) > 500 || len(adNorm) > 100 {
			continue
		}
		out = append(out, zoneKayit{Ad: adNorm, Tip: tip, Deger: deger, TTL: ttl, Oncelik: oncelik})
	}
	return out
}

// yorumAyikla — tirnak disindaki ilk ';' karakterinden itibarasini atar.
func yorumAyikla(s string) string {
	tirnakta := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			tirnakta = !tirnakta
		case ';':
			if !tirnakta {
				return s[:i]
			}
		}
	}
	return s
}

// tirnakBirlestir — `"v=DKIM1; k=rsa" "p=MIG…"` → `v=DKIM1; k=rsa p=MIG…`
// (BIND 255 bayt sinirini asan TXT'leri boler; birlestirilmezse DKIM bozulur.)
func tirnakBirlestir(s string) string {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "\"") {
		return s
	}
	var b strings.Builder
	tirnakta := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' {
			tirnakta = !tirnakta
			continue
		}
		if !tirnakta && c == ' ' {
			continue // tirnaklar arasi bosluk
		}
		b.WriteByte(c)
	}
	return strings.TrimSpace(b.String())
}

// ---------------------------------------------------------------------------
// PHP surumu
// ---------------------------------------------------------------------------

// kuruluPHPSec — kaynak surumu hedefte kurulu degilse en uygun kurulu surume
// duser. 🔴 Ayni ANA surum icinde once YUKARI bakilir: "8" veya "8.1" istegi
// 7.4'e dusurulurse PHP 8 kodu patlar.
func kuruluPHPSec(istenen string) string {
	var kurulu []string
	for _, v := range phpsurum.TumSurumler() {
		if v.Yuklu {
			kurulu = append(kurulu, v.Surum)
		}
	}
	if len(kurulu) == 0 || istenen == "" {
		return istenen
	}
	for _, v := range kurulu {
		if v == istenen {
			return istenen
		}
	}
	ia, ib := phpParcala(istenen)
	// 1) Ayni ana surum — once >= istenen, sonra en yuksek ayni ana surum
	var ayniUst, ayniAlt string
	for _, v := range kurulu {
		va, vb := phpParcala(v)
		if va != ia {
			continue
		}
		if vb >= ib {
			if ayniUst == "" {
				ayniUst = v
			} else if _, ub := phpParcala(ayniUst); vb < ub {
				ayniUst = v
			}
		} else if ayniAlt == "" {
			ayniAlt = v
		} else if _, ab := phpParcala(ayniAlt); vb > ab {
			ayniAlt = v
		}
	}
	if ayniUst != "" {
		return ayniUst
	}
	if ayniAlt != "" {
		return ayniAlt
	}
	// 2) Farkli ana surum — en yakin buyuk, yoksa en yakin kucuk
	var ust, alt string
	for _, v := range kurulu {
		va, vb := phpParcala(v)
		if va > ia || (va == ia && vb > ib) {
			if ust == "" {
				ust = v
			} else if ua, ub := phpParcala(ust); va < ua || (va == ua && vb < ub) {
				ust = v
			}
		} else {
			if alt == "" {
				alt = v
			} else if aa, ab := phpParcala(alt); va > aa || (va == aa && vb > ab) {
				alt = v
			}
		}
	}
	if ust != "" {
		return ust
	}
	if alt != "" {
		return alt
	}
	return istenen
}

func phpParcala(s string) (int, int) {
	var a, b int
	_, _ = fmt.Sscanf(s, "%d.%d", &a, &b)
	return a, b
}

// ---------------------------------------------------------------------------
// Yardimcilar
// ---------------------------------------------------------------------------

const tasimaYedekKok = "/var/lib/girginospanel/tasima-yedek"

func yedekYaz(webRoot, rel string, ham []byte) error {
	dizin := filepath.Join(tasimaYedekKok, filepath.Base(filepath.Dir(webRoot)))
	if err := os.MkdirAll(dizin, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dizin, strings.ReplaceAll(rel, "/", "_")), ham, 0o600)
}

func uidGid(sk string) (int, int, error) {
	u, err := user.Lookup(sk)
	if err != nil {
		return 0, 0, err
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	return uid, gid, nil
}

func dizinBoyut(yol string) (int64, error) {
	var t int64
	err := filepath.Walk(yol, func(_ string, fi os.FileInfo, err error) error {
		if err == nil && fi != nil && fi.Mode().IsRegular() {
			t += fi.Size()
		}
		return nil
	})
	return t, err
}

func (h *Handlers) sunucuIP() string {
	var ip string
	_ = h.DB.QueryRow(`SELECT ipv4 FROM domains WHERE ipv4 IS NOT NULL AND ipv4<>'' LIMIT 1`).Scan(&ip)
	if ip != "" {
		return ip
	}
	out, err := exec.Command("hostname", "-I").Output()
	if err == nil {
		if f := strings.Fields(string(out)); len(f) > 0 {
			return f[0]
		}
	}
	return "127.0.0.1"
}

// parenSoy — tek satirda tamamlanan parantezli rdata blogunun dis parantezlerini
// atar. BIND cok-satirli/uzun TXT kayitlarini parantezle sarar; birakilirsa deger
// "(v=DKIM1; k=rsa; p=MIG…)" olarak kaydedilir ve DKIM dogrulamasi COKER.
func parenSoy(s string) string {
	s = strings.TrimSpace(s)
	for strings.HasPrefix(s, "(") {
		s = strings.TrimSpace(s[1:])
	}
	for strings.HasSuffix(s, ")") {
		s = strings.TrimSpace(s[:len(s)-1])
	}
	return strings.TrimSpace(s)
}

// sahiplikDogrula — taşınan sitenin atanacağı bayi/müşteri gerçekten var mı?
// Geçersiz hedef sessizce ana hesaba düşmesin — hata döndür.
func (h *Handlers) sahiplikDogrula(ctx context.Context, resellerID, customerID int64) error {
	if resellerID > 0 {
		var n int
		if err := h.DB.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM users WHERE id=? AND role='reseller'`, resellerID).Scan(&n); err != nil || n == 0 {
			return fmt.Errorf("geçersiz bayi (reseller_id=%d)", resellerID)
		}
	}
	if customerID > 0 {
		var n int
		if err := h.DB.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM customers WHERE id=?`, customerID).Scan(&n); err != nil || n == 0 {
			return fmt.Errorf("geçersiz müşteri (customer_id=%d)", customerID)
		}
	}
	return nil
}
