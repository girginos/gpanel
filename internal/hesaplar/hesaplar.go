// Package hesaplar: FTP hesabi + MySQL DB hesabi olusturma yardimcilari
package hesaplar

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"girginospanel/internal/gizli"
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
	b := make([]byte, n)
	// 🔴 Hata YUTULMAZ: crypto/rand okunamazsa b tamamen sifir kalir ve
	// uretilen "parola" AAAA... olur. Bos donup cagirani durdurmak,
	// tahmin edilebilir parola vermekten iyidir.
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	for i := range b {
		b[i] = harf[int(b[i])%len(harf)]
	}
	return string(b)
}

var reDBKimlik = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)

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

// FTPCreate: ftp_accounts tablosuna kayit ekler, parolayi cleartext olarak tutar (Pure-FTPd MYSQLCrypt cleartext)
func FTPCreate(db *sql.DB, domainID int64, sistemKullanici, parola string, uidN, gidN int) error {
	home := "/home/" + sistemKullanici
	_, err := db.Exec(
		`INSERT INTO ftp_accounts(domain_id, username, password_md5, home_dir, uid_n, gid_n, status)
		 VALUES(?,?,?,?,?,?, 'active')
		 ON DUPLICATE KEY UPDATE password_md5=VALUES(password_md5), home_dir=VALUES(home_dir), uid_n=VALUES(uid_n), gid_n=VALUES(gid_n), status='active'`,
		domainID, sistemKullanici, parola, home, uidN, gidN)
	return err
}

// FTPUpdatePassword: mevcut FTP hesabinin parolasini guncelle
func FTPUpdatePassword(db *sql.DB, sistemKullanici, parola string) error {
	_, err := db.Exec(
		`UPDATE ftp_accounts SET password_md5=? WHERE username=?`,
		parola, sistemKullanici)
	return err
}

// FTPDelete: hesabi ve domain ile birlikte CASCADE silinir, ama yine de explicit
func FTPDelete(db *sql.DB, sistemKullanici string) error {
	_, err := db.Exec(`DELETE FROM ftp_accounts WHERE username=?`, sistemKullanici)
	return err
}

// MySQLCreateDB: MariaDB'de yeni veritabani + kullanici olustur + GRANT, sonra db_accounts'a kaydet
func MySQLCreateDB(db *sql.DB, domainID int64, dbName, dbUser, dbPass string) error {
	if !GecerliDBKimlik(dbName) || !GecerliDBKimlik(dbUser) {
		return fmt.Errorf("güvenlik: geçersiz veritabanı adı veya kullanıcısı")
	}
	// 1) MariaDB'de DB + user create (root socket auth ile)
	stmts := []string{
		fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;", dbName),
		fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'localhost' IDENTIFIED BY '%s';", dbUser, sqlKac(dbPass)),
		fmt.Sprintf("ALTER USER '%s'@'localhost' IDENTIFIED BY '%s';", dbUser, sqlKac(dbPass)),
		fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost';", dbName, dbUser),
		"FLUSH PRIVILEGES;",
	}
	sql := strings.Join(stmts, " ")
	if out, err := exec.Command("mysql", "-e", sql).CombinedOutput(); err != nil {
		return fmt.Errorf("mysql exec: %s: %w", strings.TrimSpace(string(out)), err)
	}

	// 2) panel DB metadata
	_, err := db.Exec(
		`INSERT INTO db_accounts(domain_id, db_name, db_user, db_pass_plain, db_host)
		 VALUES(?,?,?,?, 'localhost')`,
		domainID, dbName, dbUser, gizli.SaklaBagli(dbPass, dbUser))
	return err
}

// MySQLCreateDBForUser: yeni DB olustur + MEVCUT bir DB-kullanicisina GRANT ver
// (kullanicinin parolasina DOKUNMAZ — baska DB'leri bozmaz). db_accounts'a bu domain+db
// icin mevcut kullanicinin parolasiyla (db_pass_plain) yeni satir ekler.
// Cagiran, dbUser'in bu domaine ait oldugunu ONCEDEN dogrulamalidir (sahiplik + onek).
func MySQLCreateDBForUser(db *sql.DB, domainID int64, dbName, dbUser string) error {
	if !GecerliDBKimlik(dbName) || !GecerliDBKimlik(dbUser) {
		return fmt.Errorf("güvenlik: geçersiz veritabanı adı veya kullanıcısı")
	}
	// Mevcut kullanicinin parolasi (yeni db_accounts satiri icin — phpMyAdmin SSO).
	// NOT: deger at-rest sifreli olabilir; asagida gizli.Sakla ile yeniden
	// saklanir ve Sakla zaten-sifreli degere DOKUNMAZ → cift sifreleme olmaz.
	var pass string
	if err := db.QueryRow(
		`SELECT db_pass_plain FROM db_accounts WHERE db_user=? LIMIT 1`, dbUser).Scan(&pass); err != nil {
		return fmt.Errorf("mevcut kullanıcı parolası bulunamadı: %w", err)
	}
	// DB olustur + mevcut kullaniciya GRANT (CREATE/ALTER USER YOK → parola korunur).
	stmts := []string{
		fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;", dbName),
		fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost';", dbName, dbUser),
		"FLUSH PRIVILEGES;",
	}
	if out, err := exec.Command("mysql", "-e", strings.Join(stmts, " ")).CombinedOutput(); err != nil {
		return fmt.Errorf("mysql exec: %s: %w", strings.TrimSpace(string(out)), err)
	}
	_, err := db.Exec(
		`INSERT INTO db_accounts(domain_id, db_name, db_user, db_pass_plain, db_host)
		 VALUES(?,?,?,?, 'localhost')`,
		domainID, dbName, dbUser, gizli.SaklaGecis(pass, dbUser))
	return err
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
	defer rows.Close()
	for rows.Next() {
		var dbName, dbUser string
		if err := rows.Scan(&dbName, &dbUser); err != nil {
			continue
		}
		_ = MySQLDropDB(db, dbName, dbUser)
	}
	return nil
}

// SyncSSHPassword: sistem (SSH) hesabının parolasını FTP parolasıyla eşitler.
// FTP parolası ftp_accounts.password_md5 sütununda cleartext tutulur
// (Pure-FTPd MYSQLCrypt cleartext) — böylece SSH parolası = FTP parolası olur.
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
	if !ParolaGecerli(pw) {
		return fmt.Errorf("güvenlik: parola geçersiz karakter içeriyor")
	}
	cmd := exec.Command("chpasswd")
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
	out, err := exec.Command("mysql", "-N", "-e",
		"SELECT SCHEMA_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME='"+sqlKac(dbName)+"'").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
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
	stmts := []string{
		fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'localhost' IDENTIFIED BY '%s';", dbUser, sqlKac(dbPass)),
		fmt.Sprintf("ALTER USER '%s'@'localhost' IDENTIFIED BY '%s';", dbUser, sqlKac(dbPass)),
		fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost';", dbName, dbUser),
		"FLUSH PRIVILEGES;",
	}
	if out, err := exec.Command("mysql", "-e", strings.Join(stmts, " ")).CombinedOutput(); err != nil {
		return fmt.Errorf("mysql exec: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}
