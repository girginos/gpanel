// Veritabani KULLANICILARININ yedeklenmesi ve geri yuklenmesi.
//
// 🔴 Neden: mysqldump yalniz SEMA + VERI alir. `CREATE USER` ve `GRANT`
// ifadeleri MySQL'in kendi `mysql` semasinda durur ve tenant yedegine HIC
// girmiyordu. Sonuc: bir veritabani silinip yedekten geri yuklendiginde 88
// tablo geri geliyor ama siteyi ayaga kaldiran sey — baglanacak kullanici —
// eksik kaliyordu. Panel "kullanici/parola tanimlayin" diyordu; yani geri
// yukleme teknik olarak basarili, PRATIKTE ise site 500 donmeye devam
// ediyordu (uretimde yasandi).
//
// Iki yol birden kapatilir:
//
//	(A) Yeni yedekler `__db__/kullanicilar.sql` tasir — kullanici parola
//	    HASH'i ile birlikte doner, site hicbir dosya duzenlemeden baglanir.
//	(B) ESKI arsivlerde bu dosya yoktur. O durumda kimlik, sitenin KENDI
//	    yapilandirmasindan (wp-config.php / .env) kurtarilir — uygulamanin
//	    zaten kullandigi kullanici adi ve parola oldugu icin sonuc aynidir.
package backups

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"girginospanel/internal/gizli"
	"girginospanel/internal/hesaplar"

	"golang.org/x/sys/unix"
)

// kullaniciDosyaAdi: arsiv icindeki kullanici/GRANT dokumu.
const kullaniciDosyaAdi = "kullanicilar.sql"

// kimlikRe: MySQL kullanici/veritabani adlari icin dar whitelist. Bu kontrol
// SQL enjeksiyonunu ifade uretiminden ONCE keser (adlar mysql -e ile gecer).
var kimlikRe = regexp.MustCompile(`\A[A-Za-z0-9_]{1,64}\z`)

func gecerliKimlikMi(s string) bool { return kimlikRe.MatchString(s) }

// gecerliParolaMi: config'den (wp-config.php/.env) okunan DB parolasi — yazdirilabilir
// olmali, KONTROL karakteri (NUL/newline/CR/tab/DEL) icermemeli, makul uzunlukta olmali.
// sqlKacis backslash+tirnak kacisi yapar; bu ek dogrulama (a) mysql -e ifadesini bozan
// kontrol karakterlerini reddeder, (b) parolayi "yalnizca-escape" degil VALIDATE edilmis
// kilar (SAST taint kirilir). Anormal parola = kurtarma YOK (fail-closed).
func gecerliParolaMi(pw string) bool {
	if pw == "" || len(pw) > 256 {
		return false
	}
	for _, r := range pw {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// sistemKullanicisiMi: asla yedeklenmeyecek/geri yuklenmeyecek hesaplar. Tek liste
// hesaplar.SistemDBHesabiMi'dir (eskiden burada yalniz root/debian-sys-maint/panel vardi;
// roundcube/pma/pureftpd satira yazilip DROP USER ile dusurulebiliyordu).
func sistemKullanicisiMi(u string) bool {
	return hesaplar.SistemDBHesabiMi(u)
}

// mysqlSorgu: root socket auth ile sorgu (sekmeli satirlar).
func mysqlSorgu(q string) ([]string, error) {
	out, err := exec.Command("mysql", "--default-character-set=utf8mb4", "-N", "-B", "-e", q).Output()
	if err != nil {
		return nil, err
	}
	var satir []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimRight(l, "\r"); strings.TrimSpace(l) != "" {
			satir = append(satir, l)
		}
	}
	return satir, nil
}

// mysqlCalistir: tek ifade calistirir (shell YOK, arg olarak gecer).
func mysqlCalistir(stmt string) error {
	if out, err := exec.Command("mysql", "--default-character-set=utf8mb4", "-e", stmt).CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

type dbHesap struct{ K, H string }

// dbHesaplari: bir veritabaninda yetkisi olan kullanicilar.
// mysql.db.Db sutunu GRANT'in yazilis bicimine gore alt cizgiyi kacisli
// (ad\_x) tutabilir; iki bicim de eslestirilir.
func dbHesaplari(dbName string) []dbHesap {
	if !gecerliKimlikMi(dbName) {
		return nil
	}
	q := fmt.Sprintf(
		"SELECT User,Host FROM mysql.db WHERE Db='%s' OR REPLACE(Db,'\\\\_','_')='%s'",
		dbName, dbName)
	satirlar, err := mysqlSorgu(q)
	if err != nil {
		return nil
	}
	var out []dbHesap
	for _, l := range satirlar {
		p := strings.Split(l, "\t")
		if len(p) != 2 || sistemKullanicisiMi(p[0]) || !gecerliKimlikMi(p[0]) {
			continue
		}
		out = append(out, dbHesap{K: p[0], H: p[1]})
	}
	return out
}

// grantSuzgeci: bir SHOW GRANTS satiri bu arsive girebilir/uygulanabilir mi.
// 🔴 GUVENLIK: arsivden gelen bir GRANT asla GLOBAL yetki tasiyamaz. `ON *.*`
// yalniz ciplak USAGE olarak kabul edilir (MariaDB parola bilgisini o satirda
// tasir); geri kalan her *.* ifadesi ve izin listesi disindaki her veritabani
// REDDEDILIR. Aksi halde ozel hazirlanmis bir arsiv SUPER/GRANT OPTION
// kazandirabilirdi.
func grantSuzgeci(satir string, izin map[string]bool) bool {
	s := strings.TrimSpace(satir)
	if !strings.HasPrefix(s, "GRANT ") {
		return false
	}
	i := strings.Index(s, " ON ")
	if i < 0 {
		return false
	}
	yetki := strings.TrimSpace(s[len("GRANT "):i])
	hedef := strings.TrimSpace(s[i+len(" ON "):])
	if strings.HasPrefix(hedef, "*.*") {
		return strings.EqualFold(yetki, "USAGE")
	}
	if !strings.HasPrefix(hedef, "`") {
		return false
	}
	son := strings.Index(hedef[1:], "`")
	if son < 0 {
		return false
	}
	adi := strings.ReplaceAll(hedef[1:1+son], `\_`, "_")
	return izin[adi]
}

// dbKullanicilariYaz: verilen veritabanlarina ait kullanicilari + yetkilerini
// __db__/kullanicilar.sql dosyasina yazar. Dosya parola HASH'i icerdigi icin
// 0600'dur ve arsivin geri kalaniyla ayni gizlilik sinifindadir.
func dbKullanicilariYaz(dbDir string, dbAdlari []string) int {
	izin := map[string]bool{}
	for _, n := range dbAdlari {
		izin[n] = true
	}
	gorulen := map[string]bool{}
	var govde []string
	for _, dbName := range dbAdlari {
		for _, h := range dbHesaplari(dbName) {
			anahtar := h.K + "@" + h.H
			if gorulen[anahtar] {
				continue
			}
			gorulen[anahtar] = true
			govde = append(govde, hesapIfadeleri(h, izin)...)
		}
	}
	if len(govde) == 0 {
		return 0
	}
	bas := "-- girginospanel: veritabani kullanicilari ve yetkileri\n" +
		"-- Geri yuklemede yalniz bu arsivdeki veritabanlarina ait yetkiler uygulanir.\n"
	_ = os.WriteFile(filepath.Join(dbDir, kullaniciDosyaAdi),
		[]byte(bas+strings.Join(govde, "\n")+"\n"), 0600)
	return len(gorulen)
}

// hesapIfadeleri: bir hesabin CREATE USER + suzulmus GRANT satirlari.
func hesapIfadeleri(h dbHesap, izin map[string]bool) []string {
	hedef := fmt.Sprintf("'%s'@'%s'", h.K, sqlKacis(h.H))
	var out []string
	if satir, err := mysqlSorgu("SHOW CREATE USER " + hedef); err == nil && len(satir) > 0 {
		c := strings.TrimSpace(satir[0])
		if strings.HasPrefix(c, "CREATE USER ") {
			c = "CREATE USER IF NOT EXISTS " + strings.TrimPrefix(c, "CREATE USER ")
			out = append(out, tekSatir(c)+";")
		}
	}
	satirlar, err := mysqlSorgu("SHOW GRANTS FOR " + hedef)
	if err != nil {
		return out
	}
	for _, g := range satirlar {
		if grantSuzgeci(g, izin) {
			out = append(out, tekSatir(strings.TrimSpace(g))+";")
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// tekSatir: ifade icindeki satir sonlarini bosluga cevirir — dosya "her satir
// bir ifade" sozlesmesine dayanir, cok satirli bir deger onu bozardi.
func tekSatir(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSuffix(strings.TrimSpace(s), ";")
}

// sqlKacis: tek tirnakli SQL degeri icin kacis.
func sqlKacis(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `'`, `\'`)
}

// tirnakliAl: r'nin basindaki 'deger' ya da `deger`'i ayirir.
func tirnakliAl(r string) (deger, kalan string, ok bool) {
	if r == "" || (r[0] != '\'' && r[0] != '`') {
		return "", "", false
	}
	son := strings.IndexByte(r[1:], r[0])
	if son < 0 {
		return "", "", false
	}
	return r[1 : 1+son], r[2+son:], true
}

// satirHesabi: kullanicilar.sql ifadesinin HEDEF hesabi. Bicimler:
// "CREATE USER IF NOT EXISTS <hesap> ..." ve "GRANT <yetki> ON <nesne> TO <hesap> ...";
// <nesne> = *.* | `db`.* | `db`.`tablo`, <hesap> = 'k'@'h' ya da `k`@`h`. Hesap, nesneden
// HEMEN sonraki " TO "dan okunur (sondaki parola hash'i icindeki metne bakilmaz).
// Cozumlenemeyen satir = red (fail-closed).
func satirHesabi(s string) (kul, host string, ok bool) {
	kalan := ""
	if strings.HasPrefix(s, "CREATE USER IF NOT EXISTS ") {
		kalan = s[len("CREATE USER IF NOT EXISTS "):]
	} else {
		i := strings.Index(s, " ON ")
		if !strings.HasPrefix(s, "GRANT ") || i < 0 {
			return "", "", false
		}
		r := s[i+len(" ON "):]
		if strings.HasPrefix(r, "*.*") {
			r = r[len("*.*"):]
		} else {
			var tamam bool
			if _, r, tamam = tirnakliAl(r); !tamam || !strings.HasPrefix(r, ".") {
				return "", "", false
			}
			r = r[1:]
			if strings.HasPrefix(r, "*") {
				r = r[1:]
			} else if _, r, tamam = tirnakliAl(r); !tamam {
				return "", "", false
			}
		}
		if !strings.HasPrefix(r, " TO ") {
			return "", "", false
		}
		kalan = r[len(" TO "):]
	}
	kul, r, tamam := tirnakliAl(kalan)
	if !tamam || !strings.HasPrefix(r, "@") {
		return "", "", false
	}
	host, _, tamam = tirnakliAl(r[1:])
	if !tamam {
		return "", "", false
	}
	return kul, host, true
}

// dbKullanicilariUygula: arsivdeki kullanicilar.sql dosyasini UYGULAR.
// Her ifade YENIDEN dogrulanir: dosya arsivden gelir, arsiv disaridan gelmis
// olabilir (site tasima). Yazarken suzmek YETMEZ; okurken de suzulur.
//
// 🔴 HESAP SAHIPLIGI: ifadenin hedef hesabi hesaplar.DBHesabiBaglanabilirMi'den
// gecmelidir. Eskiden yalniz yetki/nesne suzuluyordu: arsivdeki "GRANT ... TO
// 'komsu_kullanici'" komsunun hesabina bu kiracinin DB'sinde yetki ekliyor (komsunun
// parola degisimi "baska domainin veritabaninda yetkili" diye kilitleniyordu), USAGE
// satirindaki "IDENTIFIED BY PASSWORD" ise VAR OLAN bir hesabin parolasini arsivdeki
// hash'e ceviriyordu. Karar hesap basina ILK satirda verilir ve saklanir (CREATE USER
// satiri hesabi olusturduktan sonra GRANT satiri "var ama yetkisiz" diye reddedilmesin).
func dbKullanicilariUygula(db *sql.DB, domainID int64, dbDir string, izin map[string]bool) (int, error) {
	ham, err := os.ReadFile(filepath.Join(dbDir, kullaniciDosyaAdi))
	if err != nil {
		return 0, err
	}
	kararlar := map[string]error{}
	n := 0
	for _, satir := range strings.Split(string(ham), "\n") {
		s := strings.TrimSpace(satir)
		if s == "" || strings.HasPrefix(s, "--") {
			continue
		}
		s = strings.TrimSuffix(s, ";")
		// 🔴 ZINCIRLEME IFADE: `mysql -e` noktali virgulle ayrilmis BIRDEN COK
		// ifadeyi calistirir. Satir basi suzgecten gecen bir GRANT'in ardina
		// ikinci bir ifade iliştirilebilirdi:
		//   GRANT ALL ON `izinli`.* TO 'x'@'l'; GRANT ALL ON *.* TO 'x'@'l'
		// Suzgec ilk ifadeye bakip kabul eder, mysql IKISINI DE calistirirdi.
		// Govdede noktali virgul kalan her satir reddedilir; mesru ifadelerde
		// (hash, host, yetki listesi) noktali virgul BULUNMAZ.
		if strings.Contains(s, ";") {
			log.Printf("backup: kullanicilar.sql zincirleme ifade reddedildi: %.80s", s)
			continue
		}
		switch {
		case strings.HasPrefix(s, "CREATE USER IF NOT EXISTS "):
			// Askıdaki hesabın yedeğinde SHOW CREATE USER "ACCOUNT LOCK" taşır. Kilit
			// arşivden gelmez: kullanıcı kilitsiz doğar, kilit durumu geri yükleme
			// sonunda hesabın BUGÜNKÜ askısından uygulanır (dbKilidiniHesabaUydur).
			s = reHesapKilidi.ReplaceAllString(s, "")
		case grantSuzgeci(s, izin):
		default:
			log.Printf("backup: kullanicilar.sql reddedilen ifade: %.80s", s)
			continue
		}
		kul, host, ok := satirHesabi(s)
		if !ok {
			log.Printf("backup: kullanicilar.sql hesabı çözümlenemeyen ifade reddedildi: %.80s", s)
			continue
		}
		anahtar := kul + "\x00" + host
		karar, bilinen := kararlar[anahtar]
		if !bilinen {
			karar = hesaplar.DBHesabiBaglanabilirMi(context.Background(), db, domainID, kul, host)
			kararlar[anahtar] = karar
		}
		if karar != nil {
			log.Printf("backup: kullanicilar.sql ifadesi reddedildi (hesap %.40s): %v", kul, karar)
			continue
		}
		if err := mysqlCalistir(s + ";"); err != nil {
			log.Printf("backup: kullanici ifadesi uygulanamadi (%.60s): %v", s, err)
			continue
		}
		n++
	}
	if n > 0 {
		_ = mysqlCalistir("FLUSH PRIVILEGES;")
	}
	return n, nil
}

// reHesapKilidi — CREATE USER satırındaki hesap kilidi (MariaDB SHOW CREATE USER
// çıktısı). Yalnız SİLER: arşivden gelen satıra bir şey eklemez.
var reHesapKilidi = regexp.MustCompile(`(?i)\s+ACCOUNT\s+LOCK\b`)

// dbKilidiniHesabaUydur — geri yüklemeden sonra hesabın DB kullanıcılarının kilidi
// hesabın askı durumuna (tek kaynak: hesaplar.HesapAskida) göre uygulanır. Askıda
// alınmış yedek açık hesaba KİLİTLİ kullanıcı ("account is locked", panelde iz
// yok) getirmez; askı SÜRERKEN yapılan geri yükleme (kimlikTamamla dahil) kilitsiz
// kullanıcı bırakmaz.
func dbKilidiniHesabaUydur(db *sql.DB, domainID int64, sk string) {
	askida, err := hesaplar.HesapAskida(context.Background(), db, sk)
	if err != nil {
		log.Printf("restore %s: DB kullanıcı kilidi uygulanamadı: %v", sk, err)
		return
	}
	if err := hesaplar.DBKullanicilariKilit(db, domainID, askida); err != nil {
		log.Printf("restore %s: DB kullanıcı kilidi (askida=%v): %v", sk, askida, err)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// (B) ESKI arsivler: kimligi sitenin kendi yapilandirmasindan kurtar
// ═══════════════════════════════════════════════════════════════════════════

// kimlikTamamla: geri yuklenen bir veritabani icin panel kaydini ve MySQL
// kullanicisini eksiksiz hale getirir. Sirasiyla:
//  1. MySQL'de bu DB'ye yetkili bir kullanici zaten var mi (kullanicilar.sql
//     uygulandiysa vardir) -> panel kaydina yaz, bitti.
//  2. Yoksa sitenin yapilandirma dosyasindan kullanici/parola kurtar, MySQL'de
//     olustur, yetkilendir, panel kaydina yaz.
//
// Donen metin kullaniciya gosterilecek EK aciklamadir; bos ise ek yok.
func kimlikTamamla(db *sql.DB, domainID int64, sk, dbName string) string {
	// 🔴 GUVENLIK (fail-closed): dbName arsiv icindeki DOSYA ADINDAN turetilebilir
	// (arsivDBDosyalari) ve arsiv disaridan gelmis olabilir (site tasima).
	// Backtick iceren bir ad asagidaki GRANT'in `%s` kimligini kirar ve mysql
	// ROOT olarak zincirleme ifade calistirirdi. Gecersiz ad = kurtarma YOK.
	if !gecerliKimlikMi(dbName) {
		log.Printf("backup: kimlik kurtarma reddedildi (geçersiz db adı): %.60s", dbName)
		return ""
	}
	// 🔴 HESAP SAHIPLIGI (hesaplar.DBHesabiBaglanabilirMi): satira yazilan kullanici
	// sonra parola degisimi, mevcut-kullanici modu ve DB silmede (DROP USER) "bu domainin
	// hesabi" diye kullanilir. Eskiden bu DB'ye yetkili ILK hesap / wp-config'teki ad
	// kosulsuz yaziliyordu: komsunun kullanicisi (komsu kilitlenir), roundcube/pma/pureftpd
	// (DB silinince DROP USER → sunucu geneli kesinti), komsunun ad-alaninda yeni bir ad.
	ctx := context.Background()
	for _, h := range dbHesaplari(dbName) {
		if err := hesaplar.DBHesabiBaglanabilirMi(ctx, db, domainID, h.K, h.H); err != nil {
			log.Printf("backup: %s icin %s@%s panel kaydina baglanmadi: %v", dbName, h.K, h.H, err)
			continue
		}
		panelKaydiGuncelle(db, domainID, dbName, h.K, "")
		return "kullanıcı " + h.K + " geri yüklendi"
	}

	kul, parola, kaynak := uygulamaKimligi(sk, dbName)
	// 🔴 kul KIRACI-YAZILABILIR dosyadan (wp-config.php/.env) gelir; uygulamaKimligi
	// zaten dogrular ama asagidaki Sprintf'in sahibi olarak burada da kesinlestir:
	// "x'@'localhost' IDENTIFIED BY 'p'; DROP ...;-- " tarzi bir deger CREATE
	// USER'in tek tirnagini kirip mysql root'ta zincirleme ifade calistirirdi.
	if kul == "" || !gecerliKimlikMi(kul) {
		return ""
	}
	// 🔴 GUVENLIK: parola da KIRACI-YAZILABILIR config'den gelir. sqlKacis kacislar ama
	// kontrol karakteri (NUL/newline) iceren parola mysql -e ifadesini bozabilir →
	// fail-closed reddet (defense-in-depth; SAST parola taint'ini kirar).
	if !gecerliParolaMi(parola) {
		log.Printf("backup: kimlik kurtarma reddedildi (geçersiz parola biçimi): %.60s", dbName)
		return ""
	}
	// Ad kiracinin yazabildigi dosyadan: VAR OLAN bir hesapsa (CREATE USER IF NOT EXISTS
	// no-op olur) o hesap bu domainin olmali; YOKSA baska kiracinin ad-alaninda olmamali.
	// Yanit adin kime ait oldugunu sizdirmaz.
	if err := hesaplar.DBHesabiBaglanabilirMi(ctx, db, domainID, kul, "localhost"); err != nil {
		log.Printf("backup: %s icin %s dosyasindaki kullanici baglanmadi: %v", dbName, kaynak, err)
		return "kullanıcı site yapılandırmasından kurtarılamadı — Veritabanları bölümünden tanımlayın"
	}
	// 🔴 GUVENLIK: NO_BACKSLASH_ESCAPES sql_mode ACIKSA sqlKacis'in \' kacisi kirilir
	// (backslash literal olur → tirnak string'i kapatir → SQLi). Bu oturumda o modu
	// kaldirarak sqlKacis'i mode-BAGIMSIZ guvenli kil (diger sql_mode bayraklari korunur).
	stmt := fmt.Sprintf("SET SESSION sql_mode=REPLACE(@@SESSION.sql_mode,'NO_BACKSLASH_ESCAPES',''); "+
		"CREATE USER IF NOT EXISTS '%s'@'localhost' IDENTIFIED BY '%s'; "+
		"GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost'; FLUSH PRIVILEGES;",
		kul, sqlKacis(parola), hesaplar.GrantDBKac(dbName), kul)
	if err := mysqlCalistir(stmt); err != nil {
		log.Printf("backup: %s icin kimlik kurtarma basarisiz: %v", dbName, err)
		return ""
	}
	panelKaydiGuncelle(db, domainID, dbName, kul, parola)
	log.Printf("backup: %s kullanicisi %s dosyasindan kurtarildi (%s)", dbName, kaynak, kul)
	return "kullanıcı " + kul + " site yapılandırmasından (" + kaynak + ") kurtarıldı"
}

// panelKaydiGuncelle: db_accounts satirini kullanici/parola ile tamamlar.
// Parola bos gecilirse yalniz kullanici adi yazilir (hash'ten duz parola
// TURETILEMEZ; site calisir, operator gerekirse panelden sifirlar).
func panelKaydiGuncelle(db *sql.DB, domainID int64, dbName, kul, parola string) {
	if parola != "" {
		if _, err := db.Exec(`UPDATE db_accounts SET db_user=?, db_pass_plain=?
			WHERE domain_id=? AND BINARY db_name=?`,
			kul, gizli.SaklaGecis(parola, kul), domainID, dbName); err != nil {
			log.Printf("backup: panel db_accounts guncellenemedi (domain=%d db=%s): %v", domainID, dbName, err)
		}
		return
	}
	if _, err := db.Exec(`UPDATE db_accounts SET db_user=?
		WHERE domain_id=? AND BINARY db_name=? AND (db_user='' OR db_user IS NULL)`,
		kul, domainID, dbName); err != nil {
		log.Printf("backup: panel db_accounts kullanici guncellenemedi (domain=%d db=%s): %v", domainID, dbName, err)
	}
}

// evKoku: kiraci ev dizinlerinin koku (testler t.TempDir ile degistirir; canli /home'a
// dokunmadan wp-config kurtarma yolu sinanir).
var evKoku = "/home"

// uygulamaKimligi: /home/<sk> altindaki uygulama yapilandirmasinda dbName'i
// arar; eslesirse kullanici/parola dondurur.
func uygulamaKimligi(sk, dbName string) (kul, parola, kaynak string) {
	kok := filepath.Join(evKoku, sk)
	for _, aday := range adayYapilandirmalar(kok) {
		ham, err := guvenliOku(kok, aday, 512*1024)
		if err != nil {
			continue
		}
		var d, u, p string
		if strings.HasSuffix(aday, ".env") {
			d, u, p = envAyikla(string(ham))
		} else {
			d, u, p = wpAyikla(string(ham))
		}
		if d == dbName && u != "" && gecerliKimlikMi(u) && !sistemKullanicisiMi(u) {
			return u, p, aday
		}
	}
	return "", "", ""
}

// adayYapilandirmalar: docroot kokunde ve BIR seviye altinda bakilacak
// dosyalar. Derin tarama YOK — maliyeti ongorulebilir kalmali.
func adayYapilandirmalar(kok string) []string {
	tabanlar := []string{"public_html"}
	adlar := []string{"wp-config.php", ".env"}
	var out []string
	for _, t := range tabanlar {
		for _, a := range adlar {
			out = append(out, t+"/"+a)
		}
		ents, err := os.ReadDir(filepath.Join(kok, t))
		if err != nil {
			continue
		}
		n := 0
		for _, e := range ents {
			// Symlink dizinler ATLANIR (guvenliOku zaten reddederdi).
			if !e.IsDir() || e.Type()&os.ModeSymlink != 0 || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			if n++; n > 40 {
				break
			}
			for _, a := range adlar {
				out = append(out, t+"/"+e.Name()+"/"+a)
			}
		}
	}
	return out
}

var (
	// 🔴 Deger tirnagi ACILDIGI tirnakla kapanir. Tek bir ['"] sinifi kullanmak
	// (RE2'de geri referans yok) 'p@ss"word' gibi bir parolayi ICINDEKI
	// tirnakta kesiyordu — yanlis parola ile kullanici olusturulur, site yine
	// baglanamazdi. Iki alternatif ile acilis/kapanis eslestirilir.
	wpRe = regexp.MustCompile(
		`(?i)define\(\s*['"]DB_(NAME|USER|PASSWORD)['"]\s*,\s*` +
			`(?:'((?:\\.|[^'\\])*)'|"((?:\\.|[^"\\])*)")`)
	envRe = regexp.MustCompile(`(?m)^\s*DB_(DATABASE|USERNAME|PASSWORD)\s*=\s*(.*)$`)
)

// phpKacisCoz: PHP dize kacislarini geri alir (parolalarda \' ve \\ yaygin).
func phpKacisCoz(s string) string {
	r := strings.NewReplacer(`\'`, `'`, `\"`, `"`, `\\`, `\`)
	return r.Replace(s)
}

// wpAyikla: wp-config.php icinden DB_NAME/DB_USER/DB_PASSWORD.
func wpAyikla(s string) (d, u, p string) {
	for _, m := range wpRe.FindAllStringSubmatch(s, -1) {
		v := phpKacisCoz(m[2] + m[3]) // biri bos: tek-tirnakli / cift-tirnakli dal
		switch strings.ToUpper(m[1]) {
		case "NAME":
			d = v
		case "USER":
			u = v
		case "PASSWORD":
			p = v
		}
	}
	return
}

// envAyikla: .env icinden DB_DATABASE/DB_USERNAME/DB_PASSWORD.
func envAyikla(s string) (d, u, p string) {
	kirp := func(v string) string {
		v = strings.TrimSpace(v)
		if i := strings.Index(v, " #"); i >= 0 {
			v = strings.TrimSpace(v[:i])
		}
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		return v
	}
	for _, m := range envRe.FindAllStringSubmatch(s, -1) {
		switch strings.ToUpper(m[1]) {
		case "DATABASE":
			d = kirp(m[2])
		case "USERNAME":
			u = kirp(m[2])
		case "PASSWORD":
			p = kirp(m[2])
		}
	}
	return
}

// guvenliOku: kiracinin ev dizininden baslayarak altYol'u bilesen bilesen
// O_NOFOLLOW ile acar.
//
// 🔴 Kiraci bu dosyalari YAZABILIR. wp-config.php yerine /etc/shadow'a bir
// symlink birakip panelin (root olarak) onu okumasini saglayabilirdi. Her
// bilesen O_NOFOLLOW ile acilarak zincir kiracinin evine capalanir.
func guvenliOku(homeKok, altYol string, limit int64) ([]byte, error) {
	fd, err := unix.Open(homeKok, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = unix.Close(fd) }()

	parcalar := strings.Split(altYol, "/")
	for i, p := range parcalar {
		if p == "" || p == "." || p == ".." {
			return nil, fmt.Errorf("gecersiz yol bileseni")
		}
		if i == len(parcalar)-1 {
			ffd, err := unix.Openat(fd, p, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			if err != nil {
				return nil, err
			}
			f := os.NewFile(uintptr(ffd), p)
			defer func() { _ = f.Close() }()
			return io.ReadAll(io.LimitReader(f, limit))
		}
		nfd, err := unix.Openat(fd, p, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return nil, err
		}
		_ = unix.Close(fd)
		fd = nfd
	}
	return nil, fmt.Errorf("bos yol")
}
