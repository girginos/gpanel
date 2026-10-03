package hesaplar

// parola_api.go — API anahtarıyla (faturalama otomasyonu) FTP / DB parolası
// değiştirme çekirdeği.
//
// 🔴 FTP ve DB parolaları BİRBİRİNDEN BAĞIMSIZDIR (kullanıcı kararı 2026-10-03):
// FTPHesapParolaDegistir yalnız ftp_accounts'a, DBKullaniciParolaDegistir yalnız
// MariaDB hesabına + db_accounts.db_pass_plain'e yazar. Panelde FTP↔DB eşitlemesi
// YOKTUR; FTP parolası yalnız aynı sistem kullanıcısının SSH parolasıyla paylaşılır
// (SyncSSHPassword — o da aynı Linux hesabıdır, DB değildir).

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"girginospanel/internal/gizli"
)

// ErrFTPHesabiYok: bu domainde bu adla FTP hesabı yok (başkasının hesabı da olabilir
// → çağıran 404 döner, varlık sızmaz).
var ErrFTPHesabiYok = errors.New("FTP hesabı bulunamadı")

// ErrDBKullaniciYok: bu domainde bu adla kayıtlı DB kullanıcısı yok.
var ErrDBKullaniciYok = errors.New("veritabanı kullanıcısı bulunamadı")

// FTPHesapParolaDegistir: domainID'ye ait ftp_accounts satırının (BINARY ad eşleşmesi)
// parolasını $6$ crypt olarak yazar. Satırın askı durumuna (status) DOKUNMAZ:
// askıdaki hesapta parola değişir ama FTP girişi kapalı kalır.
func FTPHesapParolaDegistir(db *sql.DB, domainID int64, kullanici, parola string) error {
	if !ParolaGecerli(parola) {
		return fmt.Errorf("güvenlik: parola geçersiz karakter içeriyor")
	}
	hash := FTPParolaHash(parola)
	if hash == "" {
		return fmt.Errorf("güvenlik: FTP parola hash üretilemedi")
	}
	res, err := db.Exec(`UPDATE ftp_accounts SET password_md5=? WHERE domain_id=? AND BINARY username=?`,
		hash, domainID, kullanici)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// RowsAffected, aynı hash yazılsa da 0 olmaz ($6$ tuzu her seferinde yeni);
		// yine de varlığı ayrıca doğrula (sürücü CLIENT_FOUND_ROWS ayarına bağlı kalma).
		var bir int
		if e := db.QueryRow(`SELECT 1 FROM ftp_accounts WHERE domain_id=? AND BINARY username=?`,
			domainID, kullanici).Scan(&bir); errors.Is(e, sql.ErrNoRows) {
			return ErrFTPHesabiYok
		} else if e != nil {
			return e
		}
	}
	return nil
}

// DBKullaniciParolaDegistir: domainID'ye kayıtlı dbUser'ın MariaDB'deki TÜM host
// tanımlarının ('localhost', '%', '127.0.0.1' …) parolasını değiştirir ve
// db_accounts.db_pass_plain'i günceller.
//
// Güvenlik kapıları MySQLChangePassword ile aynı, ek olarak HER host için:
//   - sistem/servis hesabı ASLA (SistemDBHesabiMi);
//   - kullanıcı YALNIZ bu domainde kayıtlı (kullaniciYalnizBuDomainde);
//   - her host hesabının yetkileri yalnız bu domainin veritabanlarında
//     (hesapYetkiSayimi) — parola değişimi o hesabın TÜM yetkilerini çağırana verir.
//
// Önce TÜM host'lar doğrulanır, sonra ALTER edilir (yarım değişim yok). ALTER USER …
// IDENTIFIED BY hesap kilidine (ACCOUNT LOCK, askı) DOKUNMAZ: askıdaki hesabın
// parolası değişir, kilit sürer.
func DBKullaniciParolaDegistir(panelDB *sql.DB, domainID int64, dbUser, yeniPw string) error {
	if SistemDBHesabiMi(dbUser) {
		return ErrSistemDBHesabi
	}
	if !GecerliDBKimlik(dbUser) {
		return ErrDBKullaniciYok
	}
	if !ParolaGecerli(yeniPw) {
		return fmt.Errorf("güvenlik: parola geçersiz karakter içeriyor")
	}
	var kayitli int
	if err := panelDB.QueryRow(`SELECT COUNT(*) FROM db_accounts WHERE domain_id=? AND BINARY db_user=?`,
		domainID, dbUser).Scan(&kayitli); err != nil {
		return fmt.Errorf("kayıt okunamadı: %w", err)
	}
	if kayitli == 0 {
		return ErrDBKullaniciYok
	}
	if err := kullaniciYalnizBuDomainde(panelDB, domainID, dbUser); err != nil {
		return err
	}
	hostlar, err := dbHesapHostlari(dbUser)
	if err != nil {
		return err
	}
	if len(hostlar) == 0 {
		return fmt.Errorf("veritabanı kullanıcısı MariaDB'de yok — panelden kullanıcı tanımlayın")
	}
	for _, host := range hostlar {
		if !reDBHost.MatchString(host) {
			return fmt.Errorf("güvenlik: işlenemez host")
		}
		if _, err := hesapYetkiSayimi(panelDB, domainID, dbUser, host); err != nil {
			return err
		}
	}
	ifadeler := []string{kacisModu}
	for _, host := range hostlar {
		ifadeler = append(ifadeler, fmt.Sprintf("ALTER USER '%s'@'%s' IDENTIFIED BY '%s';",
			dbUser, sqlKac(host), sqlKac(yeniPw)))
	}
	ifadeler = append(ifadeler, "FLUSH PRIVILEGES;")
	out, err := mysqlKomut("-e", strings.Join(ifadeler, " ")).CombinedOutput()
	if err != nil {
		// Sözdizimi hatası "near '…'" ile ifadeyi yankılayabilir → parola maskelenir.
		metin := strings.ReplaceAll(strings.TrimSpace(string(out)), sqlKac(yeniPw), "***")
		metin = strings.ReplaceAll(metin, yeniPw, "***")
		return fmt.Errorf("mysql alter: %s: %w", metin, err)
	}
	if _, err := panelDB.Exec(
		`UPDATE db_accounts SET db_pass_plain=? WHERE domain_id=? AND BINARY db_user=?`,
		gizli.SaklaBagli(yeniPw, dbUser), domainID, dbUser); err != nil {
		return fmt.Errorf("metadata: %w", err)
	}
	return nil
}
