// Hesaplar paketi'ne eklenecek: MySQL kullanıcı parola değiştirme
package hesaplar

import (
	"database/sql"
	"fmt"
	"os/exec"
	"strings"
)

// MySQLChangePassword: ALTER USER '<user>'@'localhost' IDENTIFIED BY '<yeni>'
// + panel DB metadata (db_accounts.db_pass_plain) güncelle.
// Birden çok DB aynı user'a sahipse (ki bizde 1:1) tek query yeterli.
func MySQLChangePassword(panelDB *sql.DB, dbUser, yeniPw string) error {
	if !strings.HasPrefix(dbUser, "c_") {
		return fmt.Errorf("güvenlik: c_ prefix'siz user reddedildi")
	}
	// MariaDB user'ın varlığını doğrula
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
		yeniPw, dbUser); err != nil {
		return fmt.Errorf("metadata: %w", err)
	}
	return nil
}
