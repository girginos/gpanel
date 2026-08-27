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

	"golang.org/x/sys/unix"
)

// kullaniciDosyaAdi: arsiv icindeki kullanici/GRANT dokumu.
const kullaniciDosyaAdi = "kullanicilar.sql"

// kimlikRe: MySQL kullanici/veritabani adlari icin dar whitelist. Bu kontrol
// SQL enjeksiyonunu ifade uretiminden ONCE keser (adlar mysql -e ile gecer).
var kimlikRe = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)

func gecerliKimlikMi(s string) bool { return kimlikRe.MatchString(s) }

// sistemKullanicisiMi: asla yedeklenmeyecek/geri yuklenmeyecek hesaplar.
func sistemKullanicisiMi(u string) bool {
	l := strings.ToLower(u)
	if strings.HasPrefix(l, "mysql.") || strings.HasPrefix(l, "mariadb.") {
		return true
	}
	switch l {
	case "root", "debian-sys-maint", "", "panel":
		return true
	}
	return false
}

// mysqlSorgu: root socket auth ile sorgu (sekmeli satirlar).
func mysqlSorgu(q string) ([]string, error) {
	out, err := exec.Command("mysql", "-N", "-B", "-e", q).Output()
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
	if out, err := exec.Command("mysql", "-e", stmt).CombinedOutput(); err != nil {
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

// dbKullanicilariUygula: arsivdeki kullanicilar.sql dosyasini UYGULAR.
// Her ifade YENIDEN dogrulanir: dosya arsivden gelir, arsiv disaridan gelmis
// olabilir (site tasima). Yazarken suzmek YETMEZ; okurken de suzulur.
func dbKullanicilariUygula(dbDir string, izin map[string]bool) (int, error) {
	ham, err := os.ReadFile(filepath.Join(dbDir, kullaniciDosyaAdi))
	if err != nil {
		return 0, err
	}
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
		case grantSuzgeci(s, izin):
		default:
			log.Printf("backup: kullanicilar.sql reddedilen ifade: %.80s", s)
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
	if h := dbHesaplari(dbName); len(h) > 0 {
		panelKaydiGuncelle(db, domainID, dbName, h[0].K, "")
		return "kullanıcı " + h[0].K + " geri yüklendi"
	}

	kul, parola, kaynak := uygulamaKimligi(sk, dbName)
	if kul == "" {
		return ""
	}
	stmt := fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'localhost' IDENTIFIED BY '%s'; "+
		"GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost'; FLUSH PRIVILEGES;",
		kul, sqlKacis(parola), dbName, kul)
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
		_, _ = db.Exec(`UPDATE db_accounts SET db_user=?, db_pass_plain=?
			WHERE domain_id=? AND db_name=?`,
			kul, gizli.SaklaGecis(parola, kul), domainID, dbName)
		return
	}
	_, _ = db.Exec(`UPDATE db_accounts SET db_user=?
		WHERE domain_id=? AND db_name=? AND (db_user='' OR db_user IS NULL)`,
		kul, domainID, dbName)
}

// uygulamaKimligi: /home/<sk> altindaki uygulama yapilandirmasinda dbName'i
// arar; eslesirse kullanici/parola dondurur.
func uygulamaKimligi(sk, dbName string) (kul, parola, kaynak string) {
	kok := filepath.Join("/home", sk)
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
