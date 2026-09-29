package hesaplar

// dbkullanici_devralma_test.go — "kullanıcısı olmayan DB'ye kullanıcı oluştur"
// (MySQLKullaniciEkle) ve DB parola değiştirme (MySQLChangePassword) yolları
// VAR OLAN ya da SUNUCU GENELİ bir MariaDB hesabını DEVRALAMAZ.
//
// Eski davranış: CREATE USER IF NOT EXISTS (var olan hesapta no-op) + koşulsuz
// ALTER USER → bayinin verdiği ad 'root', 'mysql', panelin kendi hesabı ya da
// başka kiracının kayıtsız hesabı olunca o hesabın parolası bayinin bildiği
// değere çekiliyordu (sunucu geneli DB hesabı ele geçirme).
//
// 🔴 CANLI MariaDB'YE DOKUNULMAZ: DB'li testlerde mysql CLI (mysqlKomut)
// --no-defaults + GOSP_TEST_GECICI_DSN soketiyle GEÇİCİ MariaDB'ye yönlendirilir;
// DSN yoksa bu testler ATLANIR. DB'siz testlerde mysqlKomut "çağrılmamalı"
// sahtesiyle değiştirilir — çağrılırsa test DÜŞER, gerçek mysql çalışmaz.

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

// dk_mysqlYasak: mysqlKomut'u "hiç çağrılmamalı" sahtesiyle değiştirir.
// Dönen sayaç, (yanlışlıkla) yapılan çağrı sayısıdır.
func dk_mysqlYasak(t *testing.T) *int {
	t.Helper()
	eski := mysqlKomut
	n := 0
	mysqlKomut = func(args ...string) *exec.Cmd {
		n++
		return exec.Command("false")
	}
	t.Cleanup(func() { mysqlKomut = eski })
	return &n
}

// dk_geciciMariaDB: mysqlKomut'u GEÇİCİ MariaDB'ye yönlendirir; kök bağlantısı
// ve içinde db_accounts bulunan geçici bir panel veritabanı döner.
func dk_geciciMariaDB(t *testing.T) (kok, panel *sql.DB, soket string) {
	t.Helper()
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li test atlandı")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("DSN çözülemedi: %v", err)
	}
	if cfg.Net != "unix" || cfg.Addr == "" {
		t.Skip("yalnız unix soketli geçici DSN desteklenir")
	}
	switch filepath.Clean(cfg.Addr) {
	case "/var/lib/mysql/mysql.sock", "/run/mysqld/mysqld.sock", "/var/run/mysqld/mysqld.sock", "/tmp/mysql.sock":
		t.Fatalf("GOSP_TEST_GECICI_DSN canlı MariaDB soketini gösteriyor — reddedildi")
	}
	onEk := []string{"--no-defaults", "--socket=" + cfg.Addr, "--user=" + cfg.User}
	parola := cfg.Passwd
	eski := mysqlKomut
	mysqlKomut = func(args ...string) *exec.Cmd {
		c := exec.Command("mysql", append(append([]string{}, onEk...), args...)...)
		if parola != "" {
			c.Env = append(os.Environ(), "MYSQL_PWD="+parola)
		}
		return c
	}
	t.Cleanup(func() { mysqlKomut = eski })

	cfg.DBName = ""
	kok, err = sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = kok.Close() })
	ad := fmt.Sprintf("gosp_test_dbkul_%d", time.Now().UnixNano())
	dk_exec(t, kok, "CREATE DATABASE "+ad)
	t.Cleanup(func() { _, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad) })
	cfg.DBName = ad
	panel, err = sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = panel.Close() })
	dk_exec(t, panel, `CREATE TABLE db_accounts (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
		domain_id BIGINT UNSIGNED NOT NULL, db_name VARCHAR(64) NOT NULL UNIQUE,
		db_user VARCHAR(64) NOT NULL, db_pass_plain TEXT NOT NULL,
		db_host VARCHAR(64) NOT NULL DEFAULT 'localhost')`)
	return kok, panel, cfg.Addr
}

func dk_exec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("SQL hatası: %v (%s)", err, q)
	}
}

func dk_sayi(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("sayım: %v (%s)", err, q)
	}
	return n
}

// dk_kimlik: hesabın saklanan kimlik doğrulama değeri (yalnız eşitlik için
// karşılaştırılır, çıktıya basılmaz).
func dk_kimlik(t *testing.T, kok *sql.DB, kul, host string) string {
	t.Helper()
	var s string
	if err := kok.QueryRow(`SELECT authentication_string FROM mysql.user WHERE User=? AND Host=?`,
		kul, host).Scan(&s); err != nil {
		t.Fatalf("mysql.user okunamadı (%s@%s): %v", kul, host, err)
	}
	return s
}

// dk_grantVar: kullanıcının dbAd üzerinde veritabanı düzeyi yetkisi var mı?
func dk_grantVar(t *testing.T, kok *sql.DB, kul, host, dbAd string) bool {
	t.Helper()
	return dk_sayi(t, kok, `SELECT COUNT(*) FROM mysql.db WHERE User=? AND Host=? AND (Db=? OR Db=?)`,
		kul, host, dbAd, GrantDBKac(dbAd)) > 0
}

// Sunucu geneli hesap adları sistem hesabı sayılır; kiracı adları sayılmaz.
func TestSistemDBHesabiMi(t *testing.T) {
	for _, ad := range []string{"root", "Root", "ROOT", "mysql", "mariadb.sys", "mysql.session",
		"panel", "pma", "phpmyadmin", "pureftpd", "roundcube", "roundcubemail",
		"mailadmin", "mailuser", "postfix", "dovecot", "debian-sys-maint", "", "  "} {
		if !SistemDBHesabiMi(ad) {
			t.Errorf("%q sistem hesabı sayılmalı", ad)
		}
	}
	for _, ad := range []string{"girgin_db", "c_girgin_db", "wpu_ab12cd", "admin_wp_vvzhh",
		"rooter", "mysql_app", "panel_site", "pma_x"} {
		if SistemDBHesabiMi(ad) {
			t.Errorf("%q kiracı adıdır, sistem hesabı sayılmamalı", ad)
		}
	}
}

// 🔴 Sistem hesabı adı MySQLKullaniciEkle'de mysql'e HİÇ ulaşmadan reddedilir.
func TestMySQLKullaniciEkle_SistemHesabi_MysqlCagrilmaz(t *testing.T) {
	n := dk_mysqlYasak(t)
	for _, ad := range []string{"root", "Root", "mysql", "panel", "pma", "pureftpd",
		"roundcube", "mailadmin", "mailuser"} {
		err := MySQLKullaniciEkle("bayi2_geri", ad, "Saldirgan-Parola-1")
		if !errors.Is(err, ErrSistemDBHesabi) {
			t.Errorf("%q: ErrSistemDBHesabi beklenir, alınan: %v", ad, err)
		}
	}
	if *n != 0 {
		t.Fatalf("sistem hesabı için mysql CLI %d kez çağrıldı (ALTER/CREATE root'a ulaşabilirdi)", *n)
	}
}

// Varlık sorgusu düşerse (mysql erişilemez, beklenmeyen çıktı) hesap OLUŞTURULMAZ:
// belirsizlik "yok" sayılmaz (fail-closed).
func TestMySQLKullaniciEkle_VarlikSorgusuHatasi_FailClosed(t *testing.T) {
	eski := mysqlKomut
	olusturma := 0
	mysqlKomut = func(args ...string) *exec.Cmd {
		for _, a := range args {
			if strings.Contains(a, "FROM mysql.user WHERE User=") {
				return exec.Command("false") // varlık sorgusu düşer
			}
		}
		olusturma++
		return exec.Command("false")
	}
	t.Cleanup(func() { mysqlKomut = eski })
	if err := MySQLKullaniciEkle("bayi2_geri", "bayi2_yeni", "Parola-123"); err == nil {
		t.Errorf("varlık sorgusu düştüğü halde hata dönmedi")
	}
	if olusturma != 0 {
		t.Fatalf("varlık bilinmezken CREATE/GRANT gönderildi (%d çağrı)", olusturma)
	}
}

// 🔴 Zincir kapısı: db_accounts'a (ör. yedekten kimlik kurtarma ya da eski
// sürüm yüzünden) 'root' / 'mysql' / 'panel' yazılmış olsa bile parola
// değişimi o hesaba ALTER USER GÖNDERMEZ ("panel-kayıtlı" şartı yetmez).
func TestMySQLChangePassword_SistemHesabi_MysqlCagrilmaz(t *testing.T) {
	_, panel, _ := dk_geciciMariaDB(t)
	n := dk_mysqlYasak(t)
	for i, ad := range []string{"root", "mysql", "panel", "pma"} {
		dk_exec(t, panel, `INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (20, ?, ?, '')`,
			fmt.Sprintf("bayi2_geri%d", i), ad)
		err := MySQLChangePassword(panel, 20, ad, "Saldirgan-Parola-1")
		if !errors.Is(err, ErrSistemDBHesabi) {
			t.Errorf("%q: ErrSistemDBHesabi beklenir, alınan: %v", ad, err)
		}
	}
	if *n != 0 {
		t.Fatalf("sistem hesabı için ALTER USER gönderildi (%d mysql çağrısı)", *n)
	}
}

// 🔴 ÇEKİRDEK: gerçek (geçici) MariaDB'de var olan hesap devralınamaz; var olan
// hesabın parolası/yetkisi DEĞİŞMEZ, başka host'taki hesap gölgelenmez; yeni
// ad ise eskisi gibi çalışır (hesap + parola + yalnız o DB'ye GRANT).
func TestMySQLKullaniciEkle_VarOlanHesabiDevralmaz(t *testing.T) {
	kok, _, soket := dk_geciciMariaDB(t)
	son := fmt.Sprintf("%d", time.Now().UnixNano()%1000000000)
	dbAd := "gosp_t_db_" + son
	mevcut := "gosp_t_mevcut_" + son
	uzak := "gosp_t_uzak_" + son
	yeni := "gosp_t_yeni_" + son
	dk_exec(t, kok, "CREATE DATABASE `"+dbAd+"`")
	dk_exec(t, kok, "CREATE USER '"+mevcut+"'@'localhost' IDENTIFIED BY 'Eski-Parola-1'")
	dk_exec(t, kok, "CREATE USER '"+uzak+"'@'%' IDENTIFIED BY 'Eski-Parola-2'")
	t.Cleanup(func() {
		for _, s := range []string{
			"DROP USER IF EXISTS '" + mevcut + "'@'localhost'",
			"DROP USER IF EXISTS '" + uzak + "'@'%'",
			"DROP USER IF EXISTS '" + uzak + "'@'localhost'",
			"DROP USER IF EXISTS '" + yeni + "'@'localhost'",
			"DROP USER IF EXISTS 'phpmyadmin'@'localhost'",
			"DROP USER IF EXISTS 'roundcubemail'@'localhost'",
			"DROP DATABASE IF EXISTS `" + dbAd + "`",
		} {
			_, _ = kok.Exec(s)
		}
	})
	onceMevcut := dk_kimlik(t, kok, mevcut, "localhost")
	onceUzak := dk_kimlik(t, kok, uzak, "%")

	// 1) 'x'@'localhost' zaten var → ret; parola ve yetki AYNI kalır.
	err := MySQLKullaniciEkle(dbAd, mevcut, "Saldirgan-Parola-9")
	if !errors.Is(err, ErrDBKullaniciMevcut) {
		t.Errorf("var olan hesap: ErrDBKullaniciMevcut beklenir, alınan: %v", err)
	}
	if dk_kimlik(t, kok, mevcut, "localhost") != onceMevcut {
		t.Errorf("DEVRALMA: var olan hesabın parolası DEĞİŞTİ")
	}
	if dk_grantVar(t, kok, mevcut, "localhost", dbAd) {
		t.Errorf("DEVRALMA: var olan hesaba kiracı DB'si üzerinde GRANT verildi")
	}

	// 2) Ad yalnız başka host'ta var ('x'@'%') → ret; 'x'@'localhost' OLUŞMAZ.
	err = MySQLKullaniciEkle(dbAd, uzak, "Saldirgan-Parola-9")
	if !errors.Is(err, ErrDBKullaniciMevcut) {
		t.Errorf("başka host'taki hesap: ErrDBKullaniciMevcut beklenir, alınan: %v", err)
	}
	if n := dk_sayi(t, kok, `SELECT COUNT(*) FROM mysql.user WHERE User=? AND Host='localhost'`, uzak); n != 0 {
		t.Errorf("GÖLGELEME: %s@localhost oluşturuldu (servis hesabı yerel bağlantıda gölgelenir)", uzak)
	}
	if dk_kimlik(t, kok, uzak, "%") != onceUzak {
		t.Errorf("başka host'taki hesabın parolası DEĞİŞTİ")
	}

	// 3) Henüz kurulmamış servisin adı (sistem listesi) → ret; hesap OLUŞMAZ
	//    (kiracı adı önceden kapıp sonraki kurulumun GRANT'ini devralamaz).
	for _, ad := range []string{"phpmyadmin", "roundcubemail"} {
		if dk_sayi(t, kok, `SELECT COUNT(*) FROM mysql.user WHERE User=?`, ad) > 0 {
			t.Logf("%s geçici MariaDB'de zaten var — alt durum atlandı", ad)
			continue
		}
		err = MySQLKullaniciEkle(dbAd, ad, "Saldirgan-Parola-9")
		if !errors.Is(err, ErrSistemDBHesabi) {
			t.Errorf("%q: ErrSistemDBHesabi beklenir, alınan: %v", ad, err)
		}
		if dk_sayi(t, kok, `SELECT COUNT(*) FROM mysql.user WHERE User=?`, ad) != 0 {
			t.Errorf("%q servis adıyla kiracı hesabı OLUŞTURULDU", ad)
		}
	}

	// 4) Meşru kullanım bozulmaz: yeni ad → hesap + verilen parola + DB GRANT'i.
	if err := MySQLKullaniciEkle(dbAd, yeni, "Yeni-Parola-123"); err != nil {
		t.Fatalf("yeni kullanıcı oluşturulamadı: %v", err)
	}
	if n := dk_sayi(t, kok, `SELECT COUNT(*) FROM mysql.user WHERE User=? AND Host='localhost'`, yeni); n != 1 {
		t.Fatalf("yeni kullanıcı mysql.user'da yok (n=%d)", n)
	}
	if !dk_grantVar(t, kok, yeni, "localhost", dbAd) {
		t.Errorf("yeni kullanıcıya %s üzerinde GRANT verilmedi", dbAd)
	}
	gcfg := mysql.NewConfig()
	gcfg.User, gcfg.Passwd, gcfg.Net, gcfg.Addr, gcfg.DBName = yeni, "Yeni-Parola-123", "unix", soket, dbAd
	gdb, err := sql.Open("mysql", gcfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer gdb.Close()
	if err := gdb.Ping(); err != nil {
		t.Errorf("yeni kullanıcı verilen parolayla bağlanamadı: %v", err)
	}
}

// 🔴 Yarış (TOCTOU): varlık sorgusu "yok" dese bile (arada hesap belirdi) CREATE
// USER IF NOT EXISTS'siz olduğu için DÜŞER; var olan hesabın parolası değişmez,
// GRANT çalışmaz.
func TestMySQLKullaniciEkle_YarisVarOlanHesap(t *testing.T) {
	kok, _, _ := dk_geciciMariaDB(t)
	son := fmt.Sprintf("%d", time.Now().UnixNano()%1000000000)
	dbAd := "gosp_t_ydb_" + son
	mevcut := "gosp_t_yaris_" + son
	dk_exec(t, kok, "CREATE DATABASE `"+dbAd+"`")
	dk_exec(t, kok, "CREATE USER '"+mevcut+"'@'localhost' IDENTIFIED BY 'Eski-Parola-1'")
	t.Cleanup(func() {
		_, _ = kok.Exec("DROP USER IF EXISTS '" + mevcut + "'@'localhost'")
		_, _ = kok.Exec("DROP DATABASE IF EXISTS `" + dbAd + "`")
	})
	once := dk_kimlik(t, kok, mevcut, "localhost")

	gercek := mysqlKomut
	mysqlKomut = func(args ...string) *exec.Cmd {
		for _, a := range args {
			if strings.Contains(a, "FROM mysql.user WHERE User=") {
				return exec.Command("echo", "0") // varlık sorgusu "yok" der (yarış penceresi)
			}
		}
		return gercek(args...)
	}
	t.Cleanup(func() { mysqlKomut = gercek })

	err := MySQLKullaniciEkle(dbAd, mevcut, "Saldirgan-Parola-9")
	if !errors.Is(err, ErrDBKullaniciMevcut) {
		t.Errorf("yarışta var olan hesap: ErrDBKullaniciMevcut beklenir, alınan: %v", err)
	}
	if dk_kimlik(t, kok, mevcut, "localhost") != once {
		t.Errorf("DEVRALMA (yarış): var olan hesabın parolası DEĞİŞTİ")
	}
	if dk_grantVar(t, kok, mevcut, "localhost", dbAd) {
		t.Errorf("DEVRALMA (yarış): var olan hesaba GRANT verildi")
	}
}
