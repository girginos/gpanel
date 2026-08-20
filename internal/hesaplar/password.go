// Hesaplar paketi'ne eklenecek: MySQL kullanıcı parola değiştirme
package hesaplar

import (
	"database/sql"
	"fmt"
	"girginospanel/internal/gizli"
	"os/exec"
	"strings"
)

// MySQLChangePassword: ALTER USER '<user>'@'localhost' IDENTIFIED BY '<yeni>'
// + panel DB metadata (db_accounts.db_pass_plain) güncelle.
// Birden çok DB aynı user'a sahipse (ki bizde 1:1) tek query yeterli.
func MySQLChangePassword(panelDB *sql.DB, dbUser, yeniPw string) error {
	// 🔴 GÜVENLİK: dbUser panel-yönetimli bir DB kullanıcısı OLMALI (db_accounts'ta
	// kayıtlı). Eski "c_ prefix" şartı, ORİJİNAL adla taşınan DB kullanıcılarını
	// (ör. admin_wp_vvzhh) meşru olduğu halde reddediyordu — sahiplik zaten çağıran
	// handler'da (YonetimSahibi + db_accounts lookup) doğrulanır. Burada dbUser'ın
	// panel-kaydı olduğunu teyit et: root/sistem kullanıcısına ALTER'ı yine engeller.
	var kayitli int
	if e := panelDB.QueryRow(`SELECT COUNT(*) FROM db_accounts WHERE db_user=?`, dbUser).Scan(&kayitli); e != nil || kayitli == 0 {
		return fmt.Errorf("güvenlik: panel-kayıtlı olmayan DB kullanıcısı reddedildi")
	}
	// MariaDB user'ın varlığını doğrula
	if !ParolaGecerli(yeniPw) {
		return fmt.Errorf("güvenlik: parola geçersiz karakter içeriyor")
	}
	if !GecerliDBKimlik(dbUser) {
		return fmt.Errorf("güvenlik: geçersiz kullanıcı adı")
	}
	stmts := []string{
		fmt.Sprintf("ALTER USER '%s'@'localhost' IDENTIFIED BY '%s';", dbUser, sqlKac(yeniPw)),
		"FLUSH PRIVILEGES;",
	}
	out, err := exec.Command("mysql", "-e", strings.Join(stmts, " ")).CombinedOutput()
	if err != nil {
		return fmt.Errorf("mysql alter: %s: %w", strings.TrimSpace(string(out)), err)
	}
	// panel metadata güncelle
	if _, err := panelDB.Exec(
		`UPDATE db_accounts SET db_pass_plain=? WHERE db_user=?`,
		gizli.SaklaBagli(yeniPw, dbUser), dbUser); err != nil {
		return fmt.Errorf("metadata: %w", err)
	}
	return nil
}
