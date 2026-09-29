// Hesaplar paketi'ne eklenecek: MySQL kullanıcı parola değiştirme
package hesaplar

import (
	"database/sql"
	"fmt"
	"girginospanel/internal/gizli"
	"os/exec"
	"regexp"
	"strings"
)

// MySQLChangePassword: ALTER USER '<user>'@'localhost' IDENTIFIED BY '<yeni>'
// + panel DB metadata (db_accounts.db_pass_plain) güncelle.
// Birden çok DB aynı user'a sahipse (mevcut-kullanıcı modu) tek ALTER yeterli.
//
// domainID: işlemi yapan domain. Kullanıcı panelde YALNIZ bu domaine kayıtlı
// olmalıdır (kullaniciYalnizBuDomainde).
func MySQLChangePassword(panelDB *sql.DB, domainID int64, dbUser, yeniPw string) error {
	// 🔴 GÜVENLİK: dbUser panel-yönetimli bir DB kullanıcısı OLMALI (db_accounts'ta
	// kayıtlı). Eski "c_ prefix" şartı, ORİJİNAL adla taşınan DB kullanıcılarını
	// (ör. admin_wp_vvzhh) meşru olduğu halde reddediyordu. Eskiden yalnız "db_accounts'ta
	// kayıtlı mı" bakılıyordu; hangi domaine kayıtlı olduğu DENETLENMİYORDU. Önek
	// çakışmasıyla (bkz. MySQLCreateDB) komşunun kullanıcısını kendi domain_id'siyle
	// yazdırmış bir satır, bu uç üzerinden komşunun parolasını yeniden değiştirmeye
	// yetiyordu. Artık kullanıcı YALNIZ çağıranın domainine kayıtlı olmalı; başka bir
	// domainin satırında da geçiyorsa (eski devralma kaydı) reddedilir.
	if err := kullaniciYalnizBuDomainde(panelDB, domainID, dbUser); err != nil {
		return err
	}
	// MariaDB user'ın varlığını doğrula
	if !ParolaGecerli(yeniPw) {
		return fmt.Errorf("güvenlik: parola geçersiz karakter içeriyor")
	}
	if !GecerliDBKimlik(dbUser) {
		return fmt.Errorf("güvenlik: geçersiz kullanıcı adı")
	}
	// Kayıt sahipliği yetmez: satıra BAŞKASININ hesabı yazılmış olabilir (düzeltme
	// öncesi önek çakışması; yedekten dönen DB'de kiracı-yazılabilir wp-config'ten
	// kurtarılan kullanıcı adı…). Parolayı değiştirmek hesabın TÜM yetkilerini
	// çağırana verir → hesap yalnız bu domainin veritabanlarında yetkili olmalı.
	if err := hesapYetkileriBuDomainde(panelDB, domainID, dbUser); err != nil {
		return err
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

// reGrantNesne: SHOW GRANTS satirindaki nesne — `db`.* ya da `db`.`tablo` (sutun
// yetkisi de tablo nesnesiyle gelir). Baska her bicim (PROXY ”@'%', PROCEDURE /
// FUNCTION ...) cozumlenemez sayilir ve reddedilir.
var reGrantNesne = regexp.MustCompile("^`([^`]+)`[.](?:[*]|`[^`]+`)$")

// hesapYetkileriBuDomainde: '<dbUser>'@'localhost' hesabinin MariaDB yetkileri YALNIZ
// bu domaine kayitli veritabanlarinda mi? (SHOW GRANTS, root socket.) Reddedilenler:
//   - sunucu geneli (*.*) USAGE disi yetki, rol / PROXY atamasi, cozumlenemeyen satir;
//   - jokerli (kacissiz %) ya da gecersiz veritabani adi;
//   - BASKA domaine kayitli bir veritabaninda yetki (DB su an olmasa bile: o domain
//     DB'yi geri yukleyince yetki canlanir);
//   - panelde bu domaine kayitli OLMAYAN ama VAR OLAN bir veritabaninda yetki
//     (panel/roundcube/phpMyAdmin semalari, komsunun kayitsiz DB'si).
//
// Hic kimseye kayitli olmayan VE var olmayan semadaki artik yetki (DB silinip kullanici
// tutuldugunda kalan satir) yok sayilir: erisim vermez, mesru parola degisimini bozmamali.
func hesapYetkileriBuDomainde(panelDB *sql.DB, domainID int64, dbUser string) error {
	out, err := exec.Command("mysql", "-N", "-B", "-r", "-e",
		"SHOW GRANTS FOR '"+sqlKac(dbUser)+"'@'localhost'").Output()
	if err != nil {
		return fmt.Errorf("güvenlik: DB kullanıcısının yetkileri okunamadı: %w", err)
	}
	for _, satir := range strings.Split(string(out), "\n") {
		satir = strings.TrimSpace(satir)
		if satir == "" {
			continue
		}
		on := strings.Index(satir, " ON ")
		to := strings.LastIndex(satir, " TO ")
		if !strings.HasPrefix(satir, "GRANT ") || on < 0 || to < on {
			return fmt.Errorf("güvenlik: DB kullanıcısının rol ya da çözümlenemeyen yetkisi var — yönetici incelemesi gerekli")
		}
		yetki := strings.TrimSpace(satir[len("GRANT "):on])
		nesne := strings.TrimSpace(satir[on+len(" ON ") : to])
		if nesne == "*.*" {
			if yetki != "USAGE" {
				return fmt.Errorf("güvenlik: DB kullanıcısının sunucu geneli yetkisi var — yönetici incelemesi gerekli")
			}
			continue
		}
		m := reGrantNesne.FindStringSubmatch(nesne)
		if m == nil {
			return fmt.Errorf("güvenlik: DB kullanıcısının çözümlenemeyen (proxy/rutin) yetkisi var — yönetici incelemesi gerekli")
		}
		ad := strings.NewReplacer(`\_`, "_", `\%`, "%").Replace(m[1])
		if !GecerliDBKimlik(ad) { // kacissiz % (joker) ya da gecersiz ad
			return fmt.Errorf("güvenlik: DB kullanıcısının jokerli/geçersiz veritabanı yetkisi var — yönetici incelemesi gerekli")
		}
		var bu, baska int
		if err := panelDB.QueryRow(
			`SELECT COALESCE(SUM(domain_id=?),0), COALESCE(SUM(domain_id<>?),0) FROM db_accounts WHERE db_name=?`,
			domainID, domainID, ad).Scan(&bu, &baska); err != nil {
			return fmt.Errorf("güvenlik: DB kullanıcısının yetkileri doğrulanamadı: %w", err)
		}
		if baska > 0 {
			return fmt.Errorf("güvenlik: DB kullanıcısı başka bir domainin veritabanında yetkili")
		}
		if bu > 0 {
			continue
		}
		if varMi, err := semaVarMi(ad); err != nil || varMi {
			return fmt.Errorf("güvenlik: DB kullanıcısı bu domaine ait olmayan bir veritabanında yetkili")
		}
	}
	return nil
}
