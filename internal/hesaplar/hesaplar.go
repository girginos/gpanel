// Package hesaplar: FTP hesabi + MySQL DB hesabi olusturma yardimcilari
package hesaplar

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"girginospanel/internal/gizli"
	"log"
	"os/exec"
	"regexp"
	"strings"
)

// RandomParola: URL-safe alphanumeric parola (default 20 karakter)
func RandomParola(n int) string {
	if n <= 0 {
		n = 20
	}
	const harf = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789"
	// 🔴 MODULO YANLILIGI: alfabe 56 karakter, 256 % 56 = 32 artik birakir →
	// 0..31 bayt degerlerine dusen ilk 32 harf digerlerinden ~%20 daha sik
	// cikiyordu. Reddetme ornekleme (rejection sampling): 56'nin 256'yi asmayan
	// en buyuk kati 224'tur; >=224 baytlar ATILIR ve yerine yenisi cekilir.
	const esik = 256 - (256 % len(harf)) // 224
	out := make([]byte, 0, n)
	buf := make([]byte, n)
	for tur := 0; len(out) < n; tur++ {
		if tur > 64 {
			return "" // pratikte imkansiz; yine de sonsuz donguye kapi birakma
		}
		// Hata YUTULMAZ: crypto/rand okunamazsa buf tamamen sifir kalir ve
		// uretilen "parola" AAAA... olur. Bos donup cagirani durdurmak,
		// tahmin edilebilir parola vermekten iyidir.
		if _, err := rand.Read(buf); err != nil {
			return ""
		}
		for _, v := range buf {
			if int(v) >= esik {
				continue // yanliligi yaratan artik bolge — at, yeniden cek
			}
			out = append(out, harf[int(v)%len(harf)])
			if len(out) == n {
				break
			}
		}
	}
	return string(out)
}

var reDBKimlik = regexp.MustCompile(`\A[A-Za-z0-9_]{1,64}\z`)

// reDBSonek: musteri-verdigi DB/kullanici SONEKI (panel `<sk>_` onekini kendisi ekler).
// Yalniz kucuk harf/rakam/alt-cizgi, 1-32 karakter. Onek eklendikten sonra toplam <=64
// olmasi ayrica GecerliDBKimlik ile dogrulanir.
var reDBSonek = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)

// GecerliDBKimlik: MySQL identifier (db/kullanici adi) guvenli mi? backtick/tirnak/bosluk yok => SQLi kapali
func GecerliDBKimlik(s string) bool {
	return reDBKimlik.MatchString(s)
}

// GecerliDBSonek: musteri sonek girdisi guvenli mi? (onek eklemeden ONCE dogrulanir)
func GecerliDBSonek(s string) bool {
	return reDBSonek.MatchString(s)
}

// ParolaGucluMu: musteri DB parolasi yeterince guclu mu? >=12 karakter + karisik
// (en az bir harf ve bir rakam) + tek satir. UI'de gosterilmek uzere Turkce neden dondurur.
func ParolaGucluMu(pw string) (bool, string) {
	if !ParolaGecerli(pw) {
		return false, "parola geçersiz karakter (satır sonu/kontrol) içeriyor"
	}
	if len([]rune(pw)) < 12 {
		return false, "parola en az 12 karakter olmalı"
	}
	var harf, rakam bool
	for _, r := range pw {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			harf = true
		case r >= '0' && r <= '9':
			rakam = true
		}
	}
	if !harf || !rakam {
		return false, "parola harf ve rakam içermeli (karışık)"
	}
	return true, ""
}

// MusteriDBKimlikGecerli: musteri-verdigi ad guvenli VE domain kullanicisiyla namespaced mi?
func MusteriDBKimlikGecerli(sk, s string) bool {
	if !GecerliDBKimlik(s) {
		return false
	}
	return s == sk || strings.HasPrefix(s, sk+"_")
}

// DBAdiBaskaKiracininMi: ad (veritabani YA DA DB kullanici adi) BASKA bir kiracinin
// DB ad-alanina mi dusuyor?
//
// 🔴 ONEK CAKISMASI: kiraci ad-alani "<kok>_..." bicimindedir; kok sistem
// kullanicisindan turer (c_girgin → "girgin"; tasima/uygulama yollari tam sk'yi da
// kullanir: "c_girgin_db"). SlugFromDomain tireyi alt cizgiye cevirdigi ve sonekte
// '_' serbest oldugu icin ad-alanlari IC ICE gecer: girgin.com'un ("girgin") "x_db"
// soneki, girgin-x.com'un ("girgin_x") varsayilan kullanicisi "girgin_x_db" ile AYNI
// addir. Zorunlu onek bu yuzden sahiplik kaniti DEGILDIR.
//
// Kural (en uzun kok kazanir): her domainin kokleri {sk, c_'siz sk}; ad bir koke
// esitse ya da "<kok>_" ile basliyorsa o koke duser. Adin dustugu bir kok BASKA bir
// domainin ve cagiranin kendi eslesen kokunden UZUNSA ad baskasinindir. Esit uzunluk
// (ayni kok dizgesi — c-foo.com'un "c_foo" oneki ile foo.com'un "c_foo" sk'si)
// cagirani ENGELLEMEZ; aksi halde o kiraci hic DB acamazdi. Oradaki olasi ad
// cakismasini yalniz-yeni semantik (MySQLCreateDB: var olan kimlige dokunmaz) kapatir.
//
// Hata (DB okunamadi) durumunda true + err doner: cagiran REDDETMELI (fail-closed).
func DBAdiBaskaKiracininMi(ctx context.Context, db *sql.DB, domainID int64, ad string) (bool, error) {
	var sk string
	if err := db.QueryRowContext(ctx, `SELECT sistem_kullanici FROM domains WHERE id=?`, domainID).Scan(&sk); err != nil {
		return true, err
	}
	kendi := adKokUzunlugu(ad, sk)
	rows, err := db.QueryContext(ctx,
		`SELECT sistem_kullanici FROM domains WHERE id<>? AND sistem_kullanici<>''`, domainID)
	if err != nil {
		return true, err
	}
	defer rows.Close()
	for rows.Next() {
		var baska string
		if err := rows.Scan(&baska); err != nil {
			return true, err
		}
		if adKokUzunlugu(ad, baska) > kendi {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return true, err
	}
	return false, nil
}

// adKokUzunlugu: ad, sk'nin koklerinden ({sk, c_'siz sk}) birine dusuyorsa eslesen
// EN UZUN kokun uzunlugu; hicbirine dusmuyorsa 0.
func adKokUzunlugu(ad, sk string) int {
	en := 0
	for _, kok := range []string{sk, strings.TrimPrefix(sk, "c_")} {
		if kok == "" {
			continue
		}
		if (ad == kok || strings.HasPrefix(ad, kok+"_")) && len(kok) > en {
			en = len(kok)
		}
	}
	return en
}

// GrantDBKac: GRANT/REVOKE'ta backtick'li DB ADI icin LIKE-joker kacisi.
// 🔴 MySQL/MariaDB, GRANT ... ON `db`.* ifadesindeki db adinda `_` ve `%`
// karakterlerini backtick icinde OLSA BILE joker sayar. DB adlari <slug>_<sonek>
// biciminde alt-cizgiyle dolu ve sonek kiraci kontrolunde; kacissiz GRANT bir
// kiracinin yetkisini komsu DB'lere tasiyor (cross-tenant; canli kanitlandi).
// Literal istenirse \_ / \%% kacisi sart. YALNIZ GRANT/REVOKE icin; CREATE/DROP
// DATABASE adi LITERAL alir, orada KULLANMA.
func GrantDBKac(s string) string {
	s = strings.ReplaceAll(s, "_", `\_`)
	s = strings.ReplaceAll(s, "%", `\%`)
	return s
}

// sqlKac: MySQL string-literal ('...') icin kacis (ters-bolu + tek-tirnak)
func sqlKac(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "'", "\\'")
	return s
}

// ParolaGecerli: parola tek-satir mi? chpasswd/mysql satir-enjeksiyonunu engeller.
func ParolaGecerli(pw string) bool {
	return !strings.ContainsAny(pw, "\r\n\x00")
}

// FTPCreate: ftp_accounts tablosuna kayit ekler; parola at-rest $6$ crypt hash olarak saklanir (password_md5 sutunu, Pure-FTPd MYSQLCrypt crypt)
func FTPCreate(db *sql.DB, domainID int64, sistemKullanici, parola string, uidN, gidN int) error {
	home := "/home/" + sistemKullanici
	hash := FTPParolaHash(parola) // at-rest $6$ crypt (cleartext DEGIL)
	if hash == "" {
		return fmt.Errorf("güvenlik: FTP parola hash üretilemedi")
	}
	_, err := db.Exec(
		`INSERT INTO ftp_accounts(domain_id, username, password_md5, home_dir, uid_n, gid_n, status)
		 VALUES(?,?,?,?,?,?, 'active')
		 ON DUPLICATE KEY UPDATE password_md5=VALUES(password_md5), home_dir=VALUES(home_dir), uid_n=VALUES(uid_n), gid_n=VALUES(gid_n), status='active'`,
		domainID, sistemKullanici, hash, home, uidN, gidN)
	return err
}

// FTPUpdatePassword: mevcut FTP hesabinin parolasini guncelle
func FTPUpdatePassword(db *sql.DB, sistemKullanici, parola string) error {
	hash := FTPParolaHash(parola) // at-rest $6$ crypt
	if hash == "" {
		return fmt.Errorf("güvenlik: FTP parola hash üretilemedi")
	}
	_, err := db.Exec(
		`UPDATE ftp_accounts SET password_md5=? WHERE username=?`,
		hash, sistemKullanici)
	return err
}

// FTPDelete: hesabi ve domain ile birlikte CASCADE silinir, ama yine de explicit
func FTPDelete(db *sql.DB, sistemKullanici string) error {
	_, err := db.Exec(`DELETE FROM ftp_accounts WHERE username=?`, sistemKullanici)
	return err
}

// ErrDBKimlikMevcut: istenen veritabani ya da DB kullanicisi MariaDB'de ZATEN VAR.
// Yalniz-yeni olusturucular (MySQLCreateDB, MySQLCreateDBForUser, MySQLKullaniciEkle)
// var olan kimlige HIC DOKUNMADAN bu hatayla doner. Cagiran bu durumda o kimligi
// "geri alma" adina DUSURMEMELIDIR: kimlik baska bir kiraciya ait olabilir.
var ErrDBKimlikMevcut = errors.New("veritabanı ya da kullanıcı zaten var")

// mysqlCalistir: root-socket mysql CLI ile ifade(ler) calistirir. Var olan kullanici
// (ERROR 1396 — CREATE USER) ya da veritabani (ERROR 1007 — CREATE DATABASE) hatasi
// ErrDBKimlikMevcut'a sarilir.
func mysqlCalistir(ifadeler ...string) error {
	out, err := exec.Command("mysql", "-e", strings.Join(ifadeler, " ")).CombinedOutput()
	if err == nil {
		return nil
	}
	cikti := strings.TrimSpace(string(out))
	if strings.Contains(cikti, "ERROR 1396") || strings.Contains(cikti, "ERROR 1007") {
		return fmt.Errorf("%w: %s", ErrDBKimlikMevcut, cikti)
	}
	return fmt.Errorf("mysql exec: %s: %w", cikti, err)
}

// DBKullanicisiVarMi: MariaDB'de '<dbUser>'@'localhost' hesabi var mi? Gecersiz ad ya
// da olculemeyen durum "var" sayilir (fail-closed): cagiran adi REDDETMELI.
func DBKullanicisiVarMi(dbUser string) bool {
	if !GecerliDBKimlik(dbUser) {
		return true
	}
	out, err := exec.Command("mysql", "-N", "-B", "-e",
		"SELECT COUNT(*) FROM mysql.user WHERE User='"+sqlKac(dbUser)+"' AND Host='localhost'").Output()
	if err != nil {
		return true
	}
	return strings.TrimSpace(string(out)) != "0"
}

// kullaniciYalnizBuDomainde: dbUser panelde (db_accounts) YALNIZ domainID'ye mi
// kayitli? En az bir satir bu domainde olmali, BASKA domainde satir olmamali. Ikinci
// kosul, duzeltme oncesi surumlerde onek cakismasiyla komsunun kullanicisini kendi
// domain_id'siyle yazdirmis (bkz. MySQLCreateDB) kayitlar uzerinden sonraki parola /
// GRANT islemlerinin komsuya uzanmasini keser.
func kullaniciYalnizBuDomainde(db *sql.DB, domainID int64, dbUser string) error {
	var bu, baska int
	if err := db.QueryRow(
		`SELECT COALESCE(SUM(domain_id=?),0), COALESCE(SUM(domain_id<>?),0) FROM db_accounts WHERE db_user=?`,
		domainID, domainID, dbUser).Scan(&bu, &baska); err != nil {
		return fmt.Errorf("güvenlik: DB kullanıcısının sahipliği doğrulanamadı: %w", err)
	}
	if bu == 0 {
		return fmt.Errorf("güvenlik: DB kullanıcısı bu domaine kayıtlı değil")
	}
	if baska > 0 {
		return fmt.Errorf("güvenlik: DB kullanıcısı birden çok domaine kayıtlı — yönetici incelemesi gerekli")
	}
	return nil
}

// MySQLCreateDB: MariaDB'de YENI veritabani + YENI kullanici olustur + GRANT, sonra
// db_accounts'a kaydet.
//
// 🔴 YALNIZ-YENI SEMANTIK (DB kullanici oneki cakismasi): eskiden "CREATE USER IF NOT
// EXISTS" + kosulsuz "ALTER USER ... IDENTIFIED BY" + "CREATE DATABASE IF NOT EXISTS"
// ile "olustur YA DA DEGISTIR" calisiyordu. Ad-alanlari ic ice gectigi icin (bkz.
// DBAdiBaskaKiracininMi) kisa onekli kiraci komsunun "<onek>_db" kullanicisini kendi
// soneki gibi verip parolasini sectigi degere ceviriyor, kendi DB'sini ona GRANT
// ediyor ve satiri kendi domain_id'siyle yazdiriyordu (kiracilar arasi hesap ele
// gecirme). Simdi: CREATE USER (IF NOT EXISTS YOK) → kullanici varsa ERROR 1396 ile
// DURUR, hicbir sey degismez; CREATE DATABASE (IF NOT EXISTS YOK) → DB varsa yeni
// kullanici geri alinir. Sonraki adimlardan biri duserse YALNIZ bu cagrinin
// olusturdugu DB + kullanici geri alinir. Var olan (sahipligi dogrulanmis) bir
// kullaniciya DB baglamak MySQLCreateDBForUser'in, parolasini degistirmek
// MySQLChangePassword'un isidir.
func MySQLCreateDB(db *sql.DB, domainID int64, dbName, dbUser, dbPass string) error {
	if !GecerliDBKimlik(dbName) || !GecerliDBKimlik(dbUser) {
		return fmt.Errorf("güvenlik: geçersiz veritabanı adı veya kullanıcısı")
	}
	// 1) Kullanici — YALNIZ yoksa olusur (root socket auth ile).
	if err := mysqlCalistir(fmt.Sprintf("CREATE USER '%s'@'localhost' IDENTIFIED BY '%s';", dbUser, sqlKac(dbPass))); err != nil {
		return err
	}
	// Buradan sonra kullanici BU cagrinin: sonraki bir adim duserse geri alinir.
	kullaniciGeriAl := func() {
		_ = mysqlCalistir(fmt.Sprintf("DROP USER IF EXISTS '%s'@'localhost';", dbUser))
	}
	// 2) Veritabani — YALNIZ yoksa olusur (var olan DB'ye GRANT verilmez).
	if err := mysqlCalistir(fmt.Sprintf("CREATE DATABASE `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;", dbName)); err != nil {
		kullaniciGeriAl()
		return err
	}
	hepsiniGeriAl := func() {
		_ = mysqlCalistir(fmt.Sprintf("DROP DATABASE IF EXISTS `%s`;", dbName))
		kullaniciGeriAl()
	}
	// 3) GRANT
	if err := mysqlCalistir(
		fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost';", GrantDBKac(dbName), dbUser),
		"FLUSH PRIVILEGES;"); err != nil {
		hepsiniGeriAl()
		return err
	}
	// 4) panel DB metadata
	if _, err := db.Exec(
		`INSERT INTO db_accounts(domain_id, db_name, db_user, db_pass_plain, db_host)
		 VALUES(?,?,?,?, 'localhost')`,
		domainID, dbName, dbUser, gizli.SaklaBagli(dbPass, dbUser)); err != nil {
		hepsiniGeriAl()
		return err
	}
	return nil
}

// MySQLCreateDBForUser: YENI DB olustur + MEVCUT bir DB-kullanicisina GRANT ver
// (kullanicinin parolasina DOKUNMAZ — baska DB'leri bozmaz). db_accounts'a bu domain+db
// icin mevcut kullanicinin parolasiyla (db_pass_plain) yeni satir ekler.
// Kullanici panelde YALNIZ bu domaine kayitli olmali (kullaniciYalnizBuDomainde);
// cagiranin ayrica onek/sahiplik denetimi yapmasi beklenir.
func MySQLCreateDBForUser(db *sql.DB, domainID int64, dbName, dbUser string) error {
	if !GecerliDBKimlik(dbName) || !GecerliDBKimlik(dbUser) {
		return fmt.Errorf("güvenlik: geçersiz veritabanı adı veya kullanıcısı")
	}
	if err := kullaniciYalnizBuDomainde(db, domainID, dbUser); err != nil {
		return err
	}
	// Mevcut kullanicinin parolasi (yeni db_accounts satiri icin — phpMyAdmin SSO).
	// NOT: deger at-rest sifreli olabilir; asagida gizli.Sakla ile yeniden
	// saklanir ve Sakla zaten-sifreli degere DOKUNMAZ → cift sifreleme olmaz.
	var pass string
	if err := db.QueryRow(
		`SELECT db_pass_plain FROM db_accounts WHERE db_user=? LIMIT 1`, dbUser).Scan(&pass); err != nil {
		return fmt.Errorf("mevcut kullanıcı parolası bulunamadı: %w", err)
	}
	// DB — YALNIZ yoksa olusur: var olan (baskasinin olabilecek) bir DB'ye GRANT verilmez.
	if err := mysqlCalistir(fmt.Sprintf("CREATE DATABASE `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;", dbName)); err != nil {
		return err
	}
	// Buradan sonra DB BU cagrinin: duserse yetki + DB geri alinir (DROP DATABASE
	// mysql.db yetki satirini SILMEZ; kalirsa ayni adla sonra acilan DB'ye uzanir).
	geriAl := func() {
		_ = mysqlCalistir(fmt.Sprintf("REVOKE ALL PRIVILEGES ON `%s`.* FROM '%s'@'localhost';", GrantDBKac(dbName), dbUser))
		_ = mysqlCalistir(fmt.Sprintf("DROP DATABASE IF EXISTS `%s`;", dbName))
	}
	// Mevcut kullaniciya GRANT (CREATE/ALTER USER YOK → parola korunur).
	if err := mysqlCalistir(
		fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost';", GrantDBKac(dbName), dbUser),
		"FLUSH PRIVILEGES;"); err != nil {
		geriAl()
		return err
	}
	if _, err := db.Exec(
		`INSERT INTO db_accounts(domain_id, db_name, db_user, db_pass_plain, db_host)
		 VALUES(?,?,?,?, 'localhost')`,
		domainID, dbName, dbUser, gizli.SaklaGecis(pass, dbUser)); err != nil {
		geriAl()
		return err
	}
	return nil
}

// MySQLDropDB: DB ve user kaldir + metadata sil
func MySQLDropDB(db *sql.DB, dbName, dbUser string) error {
	if !GecerliDBKimlik(dbName) || !GecerliDBKimlik(dbUser) {
		return fmt.Errorf("güvenlik: geçersiz veritabanı adı veya kullanıcısı")
	}
	stmts := []string{
		fmt.Sprintf("DROP DATABASE IF EXISTS `%s`;", dbName),
		fmt.Sprintf("DROP USER IF EXISTS '%s'@'localhost';", dbUser),
		"FLUSH PRIVILEGES;",
	}
	if out, err := exec.Command("mysql", "-e", strings.Join(stmts, " ")).CombinedOutput(); err != nil {
		return fmt.Errorf("mysql drop: %s: %w", strings.TrimSpace(string(out)), err)
	}
	_, err := db.Exec(`DELETE FROM db_accounts WHERE db_name=?`, dbName)
	return err
}

// MySQLDropDBKeepUser: yalniz DB'yi kaldir + metadata satirini sil; kullaniciya DOKUNMA.
// Kullanici baska DB'lerde de kullaniliyorsa (mevcut-kullanici modu) onlari bozmamak icin
// tek-DB silmede kullanilir.
func MySQLDropDBKeepUser(db *sql.DB, dbName string) error {
	if !GecerliDBKimlik(dbName) {
		return fmt.Errorf("güvenlik: geçersiz veritabanı adı")
	}
	if out, err := exec.Command("mysql", "-e",
		fmt.Sprintf("DROP DATABASE IF EXISTS `%s`;", dbName)).CombinedOutput(); err != nil {
		return fmt.Errorf("mysql drop: %s: %w", strings.TrimSpace(string(out)), err)
	}
	_, err := db.Exec(`DELETE FROM db_accounts WHERE db_name=?`, dbName)
	return err
}

// MySQLDropAllForDomain: domain silinince ona ait tum DB'leri kaldir
func MySQLDropAllForDomain(db *sql.DB, domainID int64) error {
	rows, err := db.Query(`SELECT db_name, db_user FROM db_accounts WHERE domain_id=?`, domainID)
	if err != nil {
		return err
	}
	type cift struct{ ad, kul string }
	var liste []cift
	for rows.Next() {
		var c cift
		if err := rows.Scan(&c.ad, &c.kul); err != nil {
			continue
		}
		liste = append(liste, c)
	}
	rows.Close()
	for _, c := range liste {
		// 🔴 Kullanici BASKA bir domainin satirinda da geciyorsa (duzeltme oncesi onek
		// cakismasiyla komsunun kullanicisini kendi satirina yazdirmis eski kayit)
		// DROP USER komsunun sitesini dusururdu: yalniz bu domainin DB'sini kaldir.
		var baska int
		if err := db.QueryRow(`SELECT COUNT(*) FROM db_accounts WHERE db_user=? AND domain_id<>?`,
			c.kul, domainID).Scan(&baska); err != nil || baska > 0 {
			_ = MySQLDropDBKeepUser(db, c.ad)
			continue
		}
		_ = MySQLDropDB(db, c.ad, c.kul)
	}
	return nil
}

// SyncSSHPassword: sistem (SSH) hesabının parolasını FTP parolasıyla eşitler.
// FTP parolası ftp_accounts.password_md5 sütununda $6$ crypt hash'i olarak
// tutulur (Pure-FTPd MYSQLCrypt crypt) ve /etc/shadow'a aynen yazılır — böylece
// SSH parolası = FTP parolası olur. 🔴 password_md5'e DÜZ METİN YAZMAYIN: FTP
// 530 verir (crypt(girilen, düz) ≠ düz). Yazıcılar FTPParolaHash kullanmalı
// (FTPCreate, FTPUpdatePassword, toplu devir, sifreheal).
func SyncSSHPassword(db *sql.DB, sistemKullanici string) error {
	if !strings.HasPrefix(sistemKullanici, "c_") {
		return fmt.Errorf("güvenlik: c_ prefiksli olmayan kullanıcı")
	}
	var pw string
	if err := db.QueryRow(
		`SELECT password_md5 FROM ftp_accounts WHERE username=? AND status='active'`,
		sistemKullanici).Scan(&pw); err != nil {
		return fmt.Errorf("ftp parola oku: %w", err)
	}
	if strings.TrimSpace(pw) == "" {
		return fmt.Errorf("ftp parolası boş")
	}
	// pw artik $6$ crypt hash'i (FTP ile paylasilan). /etc/shadow'a -e ile DOGRUDAN
	// yazilir → SSH parolasi = FTP parolasi (ayni hash). Eski duz-metin satir icin
	// (migrate edilmemis) geriye-uyumlu cleartext chpasswd.
	var cmd *exec.Cmd
	if IsFTPHash(pw) {
		cmd = exec.Command("chpasswd", "-e")
	} else {
		if !ParolaGecerli(pw) {
			return fmt.Errorf("güvenlik: parola geçersiz karakter içeriyor")
		}
		cmd = exec.Command("chpasswd")
	}
	cmd.Stdin = strings.NewReader(sistemKullanici + ":" + pw)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("chpasswd: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// LockSSHPassword: SSH kapatıldığında sistem parolasını kilitler (passwd -l).
func LockSSHPassword(sistemKullanici string) error {
	if !strings.HasPrefix(sistemKullanici, "c_") {
		return fmt.Errorf("güvenlik: c_ prefiksli olmayan kullanıcı")
	}
	out, err := exec.Command("passwd", "-l", sistemKullanici).CombinedOutput()
	if err != nil {
		return fmt.Errorf("passwd -l: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// DBVarMi — MariaDB'de bu adda bir veritabani var mi?
// "IF NOT EXISTS" sessizce no-op oldugu icin, MEVCUT bir DB'yi devralmayi
// onlemek adina kurulum oncesi acikca kontrol edilir.
func DBVarMi(dbName string) bool {
	if !GecerliDBKimlik(dbName) {
		return false
	}
	v, _ := semaVarMi(dbName)
	return v
}

// semaVarMi: DBVarMi'nin hata donduren hali (olcum yapilamadiysa err != nil);
// fail-closed karar vermesi gereken cagiranlar icin.
func semaVarMi(dbName string) (bool, error) {
	out, err := exec.Command("mysql", "-N", "-e",
		"SELECT SCHEMA_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME='"+sqlKac(dbName)+"'").Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// MySQLKullaniciEkle: MEVCUT bir veritabanina kullanici + yetki ekler.
//
// 🔴 Neden MySQLCreateDB kullanilamiyor: o fonksiyon sonunda db_accounts'a INSERT
// yapar; kaydi ZATEN var olan (ornegin geri yuklemede yeniden kaydedilmis) bir DB
// icin bu UNIQUE(db_name) ihlali verir. Buradaki surum yalniz MariaDB tarafini
// kurar, panel metadata'sini cagiran gunceller.
//
// Kullanim yeri: veritabani var ama kullanicisi YOK durumu — DB panelden silinip
// yedekten geri yuklendiginde olusur (yedekte MySQL kullanicisi/GRANT bulunmaz).
func MySQLKullaniciEkle(dbName, dbUser, dbPass string) error {
	if !GecerliDBKimlik(dbName) || !GecerliDBKimlik(dbUser) {
		return fmt.Errorf("güvenlik: geçersiz veritabanı adı veya kullanıcısı")
	}
	if !ParolaGecerli(dbPass) {
		return fmt.Errorf("güvenlik: parola geçersiz karakter içeriyor")
	}
	// 🔴 YALNIZ-YENI kullanici (bkz. MySQLCreateDB): eskiden CREATE USER IF NOT EXISTS +
	// kosulsuz ALTER USER, verilen ad VAR OLAN bir hesapsa (komsunun DB kullanicisi,
	// hatta root/panel hesabi) parolasini cagiranin sectigi degere ceviriyordu.
	if err := mysqlCalistir(fmt.Sprintf("CREATE USER '%s'@'localhost' IDENTIFIED BY '%s';", dbUser, sqlKac(dbPass))); err != nil {
		return err
	}
	if err := mysqlCalistir(
		fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost';", GrantDBKac(dbName), dbUser),
		"FLUSH PRIVILEGES;"); err != nil {
		_ = mysqlCalistir(fmt.Sprintf("DROP USER IF EXISTS '%s'@'localhost';", dbUser)) // bu cagrinin kullanicisi
		return err
	}
	return nil
}

// HealGrantJokerleri: MEVCUT kurulumlarda eski KACISSIZ GRANT'leri onarir.
// Eski "GRANT ALL ON `db`.*" satirlarinda db adindaki _ ve % LIKE-joker gibi
// davranip komsu tenant DB'lerine erisim aciyordu (cross-tenant, canli kanit).
// GrantDBKac artik YENI grant'leri kacisliyor; bu fonksiyon panel HER acilista
// MEVCUT grant'leri idempotent olarak kacisli hale getirir.
//
// Kaynak = panel'in KENDI db_accounts tablosu (c_*_db + wpu_* kayitlari; ikisi
// de MySQLCreateDB uzerinden yazildigi icin tam). DCL root-socket (mysql CLI),
// tenant DB olusturmayla ayni yol. Yalniz GecerliDBKimlik gecen adlar islenir
// => backtick/tirnak yok => SQLi kapali.
//
// Idempotency: mysql.db'de Db degeri TAM ad'a ("=" ile, _ literal) esitse
// kacissiz grant hala duruyor demektir; kacisli satirin Db'si "db\_x" oldugu
// icin bu esitligi saglamaz => zaten onarilmis satirlar atlanir.
func HealGrantJokerleri(ctx context.Context, db *sql.DB) {
	rows, err := db.QueryContext(ctx, "SELECT db_name, db_user FROM db_accounts")
	if err != nil {
		log.Printf("grant-joker heal: db_accounts okunamadi: %v", err)
		return
	}
	type cift struct{ ad, kul string }
	var liste []cift
	for rows.Next() {
		var c cift
		if err := rows.Scan(&c.ad, &c.kul); err == nil {
			liste = append(liste, c)
		}
	}
	rows.Close()

	var onarilan, atlanan, hata int
	for _, c := range liste {
		if !GecerliDBKimlik(c.ad) || !GecerliDBKimlik(c.kul) {
			atlanan++
			continue
		}
		esc := GrantDBKac(c.ad)
		if esc == c.ad {
			atlanan++ // _ veya % yok -> joker riski yok
			continue
		}
		// Kacissiz grant HALA duruyor mu? ("=" literal eslesme)
		q := fmt.Sprintf(
			"SELECT 1 FROM mysql.db WHERE User='%s' AND Host='localhost' AND Db='%s' LIMIT 1",
			c.kul, c.ad)
		out, err := exec.Command("mysql", "-N", "-B", "-r", "-e", q).Output()
		if err != nil {
			log.Printf("grant-joker heal: durum sorgusu hata (%s): %v", c.ad, err)
			hata++
			continue
		}
		if strings.TrimSpace(string(out)) == "" {
			atlanan++ // zaten kacisli ya da grant yok
			continue
		}
		// REVOKE eski kacissiz + GRANT kacisli (root-socket, idempotent-safe).
		stmt := fmt.Sprintf(
			"REVOKE ALL PRIVILEGES ON `%s`.* FROM '%s'@'localhost'; "+
				"GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost'; FLUSH PRIVILEGES;",
			c.ad, c.kul, esc, c.kul)
		if o, e := exec.Command("mysql", "-e", stmt).CombinedOutput(); e != nil {
			log.Printf("grant-joker heal HATA (%s): %s: %v", c.ad, strings.TrimSpace(string(o)), e)
			hata++
			continue
		}
		onarilan++
	}
	if onarilan > 0 || hata > 0 {
		log.Printf("grant-joker heal: %d onarildi, %d atlandi, %d hata", onarilan, atlanan, hata)
	}
}
