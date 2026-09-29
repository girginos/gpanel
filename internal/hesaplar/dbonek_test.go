package hesaplar

// dbonek_test.go — DB KULLANICI ÖNEKİ ÇAKIŞMASI → KOMŞUNUN DB PAROLASI.
//
// Kiracı DB ad-alanı "<önek>_<sonek>" ve önek sistem kullanıcısından türer
// (c_girgin → "girgin", c_girgin_x → "girgin_x"). Tire alt çizgiye dönüştüğü ve
// sonekte '_' serbest olduğu için girgin.com'un "x_db" soneki girgin-x.com'un
// varsayılan kullanıcısı "girgin_x_db" ile AYNI ada çıkar. MySQLCreateDB eskiden
// "CREATE USER IF NOT EXISTS" + koşulsuz "ALTER USER … IDENTIFIED BY" çalıştırdığı
// için komşunun kullanıcısının parolası çağıranın seçtiği değere dönüyor, GRANT
// ekleniyor ve satır çağıranın domain_id'siyle db_accounts'a yazılıyordu.
//
// Gerçek MariaDB gerekir; GOSP_TEST_GECICI_DSN (unix soketli geçici sunucu) yoksa
// ATLANIR. `mysql` CLI çağrıları PATH'e konan bir sarmalayıcıyla YALNIZ o geçici
// sunucuya gider (--no-defaults + açık --socket; canlı sunucuya asla ulaşmaz —
// dbo_ortam bunu @@socket ile ayrıca doğrular).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

type dboOrtam struct {
	panel *sql.DB       // geçici panel şeması (domains + db_accounts)
	kok   *sql.DB       // geçici sunucuda root (şema dışı; kullanıcı/DB kurulumu)
	cfg   *mysql.Config // bağlantı denemeleri için taban yapılandırma
	on    string        // bu teste özgü ad öneki (paralel paketler çakışmasın)
}

// dbo_ortam: geçici panel DB'si + mysql sarmalayıcısı + iki kiracı:
//
//	domain 1: c_<on>    (kısa önek "<on>")   — saldırgan
//	domain 2: c_<on>_x  (uzun önek "<on>_x") — kurban
func dbo_ortam(t *testing.T) *dboOrtam {
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
		t.Skip("geçici DSN unix soketi değil — mysql CLI sarmalayıcısı kurulamaz")
	}
	gercek, err := exec.LookPath("mysql")
	if err != nil {
		t.Skip("mysql istemcisi yok")
	}
	dir := t.TempDir()
	betik := "#!/bin/sh\nexec '" + gercek + "' --no-defaults --protocol=socket --socket='" + cfg.Addr + "' --user=root \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "mysql"), []byte(betik), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	// Güvenlik: sarmalayıcı gerçekten geçici sunucuya mı gidiyor?
	out, err := exec.Command("mysql", "-N", "-B", "-e", "SELECT @@socket").CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != cfg.Addr {
		t.Fatalf("mysql sarmalayıcısı geçici sunucuya gitmiyor (%q, %v) — test durduruldu", strings.TrimSpace(string(out)), err)
	}

	kcfg := cfg.Clone()
	kcfg.DBName = ""
	kok, err := sql.Open("mysql", kcfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = kok.Close() })
	on := "dbo" + strconv.FormatInt(time.Now().UnixNano()%1e12, 36)
	ad := "gosp_test_" + on
	if _, err := kok.Exec("CREATE DATABASE " + ad); err != nil {
		t.Fatalf("geçici veritabanı oluşturulamadı: %v", err)
	}
	t.Cleanup(func() {
		// Testin açtığı MariaDB kullanıcı/veritabanlarını topla (ad öneki bu teste özgü).
		if rows, err := kok.Query(`SELECT User FROM mysql.user WHERE User LIKE ?`, on+"%"); err == nil {
			var kul []string
			for rows.Next() {
				var u string
				if rows.Scan(&u) == nil {
					kul = append(kul, u)
				}
			}
			rows.Close()
			for _, u := range kul {
				_, _ = kok.Exec("DROP USER IF EXISTS '" + u + "'@'localhost'")
			}
		}
		if rows, err := kok.Query(`SELECT SCHEMA_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME LIKE ?`, on+"%"); err == nil {
			var dbler []string
			for rows.Next() {
				var d string
				if rows.Scan(&d) == nil {
					dbler = append(dbler, d)
				}
			}
			rows.Close()
			for _, d := range dbler {
				_, _ = kok.Exec("DROP DATABASE IF EXISTS `" + d + "`")
			}
		}
		_, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad)
	})
	pcfg := cfg.Clone()
	pcfg.DBName = ad
	panel, err := sql.Open("mysql", pcfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = panel.Close() })
	for _, q := range []string{
		`CREATE TABLE domains (id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
			sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '') ENGINE=InnoDB`,
		// 0004 + 0036 (db_user artık UNIQUE değil)
		`CREATE TABLE db_accounts (
			id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
			domain_id BIGINT UNSIGNED NOT NULL,
			db_name VARCHAR(64) NOT NULL UNIQUE,
			db_user VARCHAR(64) NOT NULL,
			db_pass_plain VARCHAR(255) NOT NULL,
			db_host VARCHAR(64) NOT NULL DEFAULT 'localhost',
			notlar VARCHAR(255) NOT NULL DEFAULT '',
			KEY ix_db_domain (domain_id),
			CONSTRAINT fk_db_domain FOREIGN KEY (domain_id) REFERENCES domains(id) ON DELETE CASCADE
		) ENGINE=InnoDB`,
		fmt.Sprintf(`INSERT INTO domains (id, sistem_kullanici) VALUES (1,'c_%s'), (2,'c_%s_x')`, on, on),
	} {
		if _, err := panel.Exec(q); err != nil {
			t.Fatalf("şema: %v\n%s", err, q)
		}
	}
	return &dboOrtam{panel: panel, kok: kok, cfg: cfg, on: on}
}

func (o *dboOrtam) kokExec(t *testing.T, q string) {
	t.Helper()
	if _, err := o.kok.Exec(q); err != nil {
		t.Fatalf("root SQL: %v\n%s", err, q)
	}
}

// kurbanKur: kurban kiracının (domain 2) sağlamadaki gibi kurulmuş varsayılan
// kullanıcısı "<on>_x_db" + veritabanı "<on>_x_main" + db_accounts satırı.
func (o *dboOrtam) kurbanKur(t *testing.T, parola string) (kul, dbAdi string) {
	t.Helper()
	kul, dbAdi = o.on+"_x_db", o.on+"_x_main"
	o.kokExec(t, "CREATE USER '"+kul+"'@'localhost' IDENTIFIED BY '"+parola+"'")
	o.kokExec(t, "CREATE DATABASE `"+dbAdi+"`")
	o.kokExec(t, "GRANT ALL PRIVILEGES ON `"+GrantDBKac(dbAdi)+"`.* TO '"+kul+"'@'localhost'")
	if _, err := o.panel.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (2,?,?,?)`,
		dbAdi, kul, parola); err != nil {
		t.Fatal(err)
	}
	return kul, dbAdi
}

// baglanir: kul/parola ile geçici sunucuya (isteğe bağlı olarak dbAdi'ya) bağlanılabiliyor mu?
func (o *dboOrtam) baglanir(kul, parola, dbAdi string) bool {
	c := o.cfg.Clone()
	c.User, c.Passwd, c.DBName = kul, parola, dbAdi
	db, err := sql.Open("mysql", c.FormatDSN())
	if err != nil {
		return false
	}
	defer db.Close()
	return db.Ping() == nil
}

func (o *dboOrtam) kullaniciVar(t *testing.T, kul string) bool {
	t.Helper()
	var n int
	if err := o.kok.QueryRow(`SELECT COUNT(*) FROM mysql.user WHERE User=? AND Host='localhost'`, kul).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

func (o *dboOrtam) dbVar(t *testing.T, ad string) bool {
	t.Helper()
	var n int
	if err := o.kok.QueryRow(`SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME=?`, ad).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

// yetkiVar: kul'un dbAdi üzerinde (kaçışlı ya da kaçışsız) db-düzeyi yetkisi var mı?
func (o *dboOrtam) yetkiVar(t *testing.T, kul, dbAdi string) bool {
	t.Helper()
	var n int
	if err := o.kok.QueryRow(`SELECT COUNT(*) FROM mysql.db WHERE User=? AND Host='localhost' AND (Db=? OR Db=?)`,
		kul, dbAdi, GrantDBKac(dbAdi)).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

func (o *dboOrtam) satirSayisi(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := o.panel.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("sayım: %v (%s)", err, q)
	}
	return n
}

// 🔴 ASIL İSTİSMAR: kısa önekli kiracı (domain 1, önek "<on>") "yeni kullanıcı" soneki
// olarak "x_db" verir → ad "<on>_x_db" = uzun önekli komşunun (domain 2) varsayılan
// kullanıcısı. Kullanıcı ZATEN VAR olduğu için MySQLCreateDB hiçbir şeye dokunmadan
// reddetmeli: komşunun parolası değişmez, çağıranın DB'si ona GRANT edilmez, satır
// çağıranın domain_id'siyle yazılmaz.
func TestMySQLCreateDB_KomsununKullanicisiniDevralmaz(t *testing.T) {
	o := dbo_ortam(t)
	kurbanPw := "KurbanParola123"
	kul, kurbanDB := o.kurbanKur(t, kurbanPw)
	saldirganDB := o.on + "_saldiri"
	saldirganPw := "SaldirganParola123"

	err := MySQLCreateDB(o.panel, 1, saldirganDB, kul, saldirganPw)
	if err == nil {
		t.Errorf("var olan komşu kullanıcısı %q ile MySQLCreateDB BAŞARILI oldu — devralma", kul)
	}
	if !o.baglanir(kul, kurbanPw, kurbanDB) {
		t.Errorf("komşunun parolası değişti: kurban artık kendi parolasıyla bağlanamıyor")
	}
	if o.baglanir(kul, saldirganPw, "") {
		t.Errorf("saldırganın seçtiği parola komşunun kullanıcısına yazıldı")
	}
	if o.yetkiVar(t, kul, saldirganDB) {
		t.Errorf("komşunun kullanıcısına saldırganın DB'si için GRANT eklendi")
	}
	if n := o.satirSayisi(t, `SELECT COUNT(*) FROM db_accounts WHERE domain_id=1`); n != 0 {
		t.Errorf("komşunun kullanıcısı saldırganın domain_id'siyle db_accounts'a yazıldı (%d satır)", n)
	}
	if o.dbVar(t, saldirganDB) {
		t.Errorf("reddedilen istekte veritabanı %q yine de oluşturuldu (yarım iş)", saldirganDB)
	}
	var pw string
	if err := o.panel.QueryRow(`SELECT db_pass_plain FROM db_accounts WHERE domain_id=2 AND db_user=?`, kul).Scan(&pw); err != nil || pw != kurbanPw {
		t.Errorf("kurbanın db_accounts parolası değişti: %v", err)
	}
	if !errors.Is(err, ErrDBKimlikMevcut) {
		t.Errorf("hata ErrDBKimlikMevcut olmalı (çağıran var olanı düşürmesin): %v", err)
	}
}

// Var olan (panelde kayıtsız) bir veritabanı adıyla MySQLCreateDB: yeni kullanıcıya o
// DB'nin GRANT'i verilmemeli; bu çağrının açtığı kullanıcı geri alınmalı.
func TestMySQLCreateDB_VarOlanDByeYetkiVermez(t *testing.T) {
	o := dbo_ortam(t)
	gizliDB := o.on + "_x_gizli"
	o.kokExec(t, "CREATE DATABASE `"+gizliDB+"`")
	yeniKul := o.on + "_yeni"

	err := MySQLCreateDB(o.panel, 1, gizliDB, yeniKul, "YeniParola12345")
	if !errors.Is(err, ErrDBKimlikMevcut) {
		t.Fatalf("var olan DB için ErrDBKimlikMevcut beklenir, alınan: %v", err)
	}
	if o.yetkiVar(t, yeniKul, gizliDB) {
		t.Errorf("var olan DB %q yeni kullanıcıya GRANT edildi", gizliDB)
	}
	if o.kullaniciVar(t, yeniKul) {
		t.Errorf("başarısız çağrının açtığı kullanıcı %q geri alınmadı", yeniKul)
	}
	if !o.dbVar(t, gizliDB) {
		t.Errorf("var olan DB %q geri almada DÜŞÜRÜLDÜ (başkasının olabilir)", gizliDB)
	}
	if n := o.satirSayisi(t, `SELECT COUNT(*) FROM db_accounts`); n != 0 {
		t.Errorf("reddedilen istek db_accounts'a %d satır yazdı", n)
	}
}

// Meşru yol + INSERT düşerse geri alma: yeni ad → oluşur, bağlanılır, satır doğru
// domain_id'yle yazılır. db_accounts INSERT'i düşerse (FK) MariaDB tarafı geri alınır.
func TestMySQLCreateDB_MesruVeGeriAlma(t *testing.T) {
	o := dbo_ortam(t)
	ad, kul, pw := o.on+"_site", o.on+"_siteu", "MesruParola12345"
	if err := MySQLCreateDB(o.panel, 1, ad, kul, pw); err != nil {
		t.Fatalf("meşru oluşturma düştü: %v", err)
	}
	if !o.baglanir(kul, pw, ad) {
		t.Errorf("meşru kullanıcı kendi DB'sine bağlanamıyor")
	}
	if n := o.satirSayisi(t, `SELECT COUNT(*) FROM db_accounts WHERE domain_id=1 AND db_name=? AND db_user=?`, ad, kul); n != 1 {
		t.Errorf("db_accounts satırı yazılmadı (%d)", n)
	}

	ad2, kul2 := o.on+"_fk", o.on+"_fku"
	if err := MySQLCreateDB(o.panel, 999, ad2, kul2, pw); err == nil {
		t.Fatalf("var olmayan domain_id ile INSERT düşmeliydi")
	}
	if o.kullaniciVar(t, kul2) {
		t.Errorf("INSERT düşünce kullanıcı %q geri alınmadı (öksüz hesap)", kul2)
	}
	if o.dbVar(t, ad2) {
		t.Errorf("INSERT düşünce veritabanı %q geri alınmadı (öksüz DB)", ad2)
	}
}

// Kullanıcısı olmayan (yedekten dönen) DB'ye kullanıcı ekleme: verilen ad VAR OLAN bir
// hesapsa (komşunun kayıtsız kullanıcısı, root vb.) parolası DEĞİŞMEMELİ.
func TestMySQLKullaniciEkle_VarOlanHesabiDegistirmez(t *testing.T) {
	o := dbo_ortam(t)
	kurban, kurbanPw := o.on+"_kayitsiz", "KurbanParola123"
	o.kokExec(t, "CREATE USER '"+kurban+"'@'localhost' IDENTIFIED BY '"+kurbanPw+"'")
	dbAdi := o.on + "_geri"
	o.kokExec(t, "CREATE DATABASE `"+dbAdi+"`")

	err := MySQLKullaniciEkle(dbAdi, kurban, "SaldirganParola123")
	if !errors.Is(err, ErrDBKimlikMevcut) {
		t.Errorf("var olan hesap için ErrDBKimlikMevcut beklenir, alınan: %v", err)
	}
	if !o.baglanir(kurban, kurbanPw, "") {
		t.Errorf("var olan hesabın parolası değişti")
	}
	if o.yetkiVar(t, kurban, dbAdi) {
		t.Errorf("var olan hesaba %q için GRANT eklendi", dbAdi)
	}
	// Meşru: yeni ad → oluşur ve bağlanır.
	yeni := o.on + "_geriu"
	if err := MySQLKullaniciEkle(dbAdi, yeni, "YeniParola12345"); err != nil {
		t.Fatalf("meşru kullanıcı ekleme düştü: %v", err)
	}
	if !o.baglanir(yeni, "YeniParola12345", dbAdi) {
		t.Errorf("eklenen kullanıcı DB'ye bağlanamıyor")
	}
}

// Parola değiştirme sahipliği: kullanıcı yalnız çağıranın domainine kayıtlı olmalı.
// Düzeltme öncesi devralmayla oluşmuş (aynı kullanıcı iki domainin satırında) kayıt
// ya da başka domainin kullanıcısı üzerinden komşunun parolası değiştirilemez.
func TestMySQLChangePassword_YalnizKendiDomainininKullanicisi(t *testing.T) {
	o := dbo_ortam(t)
	kurbanPw := "KurbanParola123"
	kul, _ := o.kurbanKur(t, kurbanPw)

	// (a) başka domainin kullanıcısı
	if err := MySQLChangePassword(o.panel, 1, kul, "SaldirganParola123"); err == nil {
		t.Errorf("domain 1, domain 2'nin kullanıcısının parolasını değiştirebildi")
	}
	// (b) eski devralma kaydı: aynı kullanıcı domain 1'in satırında da
	if _, err := o.panel.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (1,?,?,'x')`,
		o.on+"_eski", kul); err != nil {
		t.Fatal(err)
	}
	if err := MySQLChangePassword(o.panel, 1, kul, "SaldirganParola123"); err == nil {
		t.Errorf("iki domaine kayıtlı kullanıcı (eski devralma kaydı) üzerinden parola değişti")
	}
	if !o.baglanir(kul, kurbanPw, "") {
		t.Errorf("komşunun parolası değişti")
	}
	var pw string
	_ = o.panel.QueryRow(`SELECT db_pass_plain FROM db_accounts WHERE domain_id=2 AND db_user=?`, kul).Scan(&pw)
	if pw != kurbanPw {
		t.Errorf("kurbanın db_accounts parolası değişti")
	}
	// (c) panelde HİÇ kayıtlı olmayan var olan hesap (yetkisi yalnız USAGE).
	kayitsiz := o.on + "_kayitsiz"
	o.kokExec(t, "CREATE USER '"+kayitsiz+"'@'localhost' IDENTIFIED BY 'KayitsizParola123'")
	if err := MySQLChangePassword(o.panel, 1, kayitsiz, "SaldirganParola123"); err == nil {
		t.Errorf("panelde kayıtsız hesabın parolası değişti")
	}
	if !o.baglanir(kayitsiz, "KayitsizParola123", "") {
		t.Errorf("kayıtsız hesabın parolası değişti")
	}
	// (d) domain 1'de gerçek DB'si olan ama domain 2'nin (şeması olmayan) satırında da
	// geçen kullanıcı: iki domaine kayıtlı → belirsiz sahiplik → red.
	iki := o.on + "_iki"
	ikiDB := o.hesapKur(t, 1, iki, "IkiParola12345")
	if _, err := o.panel.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (2,?,?,'x')`,
		o.on+"_x_hayalet", iki); err != nil {
		t.Fatal(err)
	}
	if err := MySQLChangePassword(o.panel, 1, iki, "SaldirganParola123"); err == nil {
		t.Errorf("iki domaine kayıtlı kullanıcının parolası değişti")
	}
	if !o.baglanir(iki, "IkiParola12345", ikiDB) {
		t.Errorf("iki domaine kayıtlı kullanıcının parolası değişti")
	}

	// Meşru: kendi kullanıcısı.
	ad, kendi := o.on+"_kendi", o.on+"_kendiu"
	if err := MySQLCreateDB(o.panel, 1, ad, kendi, "IlkParola12345"); err != nil {
		t.Fatal(err)
	}
	if err := MySQLChangePassword(o.panel, 1, kendi, "YeniParola12345"); err != nil {
		t.Fatalf("kendi kullanıcısının parolası değiştirilemedi: %v", err)
	}
	if !o.baglanir(kendi, "YeniParola12345", ad) {
		t.Errorf("yeni parola uygulanmadı")
	}
}

// Var olan kullanıcıya DB bağlama: kullanıcı başka domaine kayıtlıysa ya da DB zaten
// varsa GRANT verilmez.
func TestMySQLCreateDBForUser_SahiplikVeVarOlanDB(t *testing.T) {
	o := dbo_ortam(t)
	kul, _ := o.kurbanKur(t, "KurbanParola123")
	yeniDB := o.on + "_bagla"
	if err := MySQLCreateDBForUser(o.panel, 1, yeniDB, kul); err == nil {
		t.Errorf("domain 1, domain 2'nin kullanıcısına DB bağlayabildi")
	}
	if o.yetkiVar(t, kul, yeniDB) || o.dbVar(t, yeniDB) {
		t.Errorf("reddedilen bağlamada DB/GRANT oluştu")
	}
	// Eski devralma kaydı: komşunun kullanıcısı domain 1'in satırında da → red.
	if _, err := o.panel.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (1,?,?,'x')`,
		o.on+"_eski2", kul); err != nil {
		t.Fatal(err)
	}
	yeniDB2 := o.on + "_bagla2"
	if err := MySQLCreateDBForUser(o.panel, 1, yeniDB2, kul); err == nil {
		t.Errorf("iki domaine kayıtlı (eski devralma) kullanıcıya DB bağlandı")
	}
	if o.yetkiVar(t, kul, yeniDB2) || o.dbVar(t, yeniDB2) {
		t.Errorf("eski devralma kaydıyla reddedilen bağlamada DB/GRANT oluştu")
	}

	// Kendi kullanıcısı + panelde kayıtsız ama VAR OLAN DB → red, DB'ye dokunulmaz.
	ad, kendi := o.on+"_k", o.on+"_ku"
	if err := MySQLCreateDB(o.panel, 1, ad, kendi, "KendiParola12345"); err != nil {
		t.Fatal(err)
	}
	varOlan := o.on + "_varolan"
	o.kokExec(t, "CREATE DATABASE `"+varOlan+"`")
	if err := MySQLCreateDBForUser(o.panel, 1, varOlan, kendi); !errors.Is(err, ErrDBKimlikMevcut) {
		t.Errorf("var olan DB için ErrDBKimlikMevcut beklenir: %v", err)
	}
	if o.yetkiVar(t, kendi, varOlan) {
		t.Errorf("var olan DB %q kullanıcıya GRANT edildi", varOlan)
	}
	// Komşuya KAYITLI ama şeması olmayan ad: şema oluşur, GRANT verilir, INSERT
	// (db_name UNIQUE) düşer → geri alma DB'yi VE yetkiyi kaldırmalı; yoksa komşu DB'yi
	// geri yüklediğinde artık yetki onun verisine uzanır.
	kayitliAd := o.on + "_x_yedek2"
	if _, err := o.panel.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (2,?,?,'x')`,
		kayitliAd, o.on+"_x_db"); err != nil {
		t.Fatal(err)
	}
	if err := MySQLCreateDBForUser(o.panel, 1, kayitliAd, kendi); err == nil {
		t.Errorf("komşuya kayıtlı DB adı kullanıcıya bağlandı")
	}
	if o.yetkiVar(t, kendi, kayitliAd) {
		t.Errorf("başarısız bağlamada %q için artık yetki kaldı", kayitliAd)
	}
	if o.dbVar(t, kayitliAd) {
		t.Errorf("başarısız bağlamada %q şeması kaldı", kayitliAd)
	}
	// Meşru: yeni DB kendi kullanıcısına bağlanır.
	ek := o.on + "_ek2"
	if err := MySQLCreateDBForUser(o.panel, 1, ek, kendi); err != nil {
		t.Fatalf("meşru bağlama düştü: %v", err)
	}
	if !o.baglanir(kendi, "KendiParola12345", ek) {
		t.Errorf("bağlanan DB'ye erişilemiyor")
	}
}

// Domain silme: eski devralma kaydında komşuyla paylaşılan kullanıcı DÜŞÜRÜLMEZ.
func TestMySQLDropAllForDomain_KomsuylaPaylasilanKullaniciKalir(t *testing.T) {
	o := dbo_ortam(t)
	kurbanPw := "KurbanParola123"
	kul, kurbanDB := o.kurbanKur(t, kurbanPw)
	eski := o.on + "_eski"
	o.kokExec(t, "CREATE DATABASE `"+eski+"`")
	if _, err := o.panel.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (1,?,?,'x')`,
		eski, kul); err != nil {
		t.Fatal(err)
	}
	ad, kendi := o.on+"_d1", o.on+"_d1u"
	if err := MySQLCreateDB(o.panel, 1, ad, kendi, "KendiParola12345"); err != nil {
		t.Fatal(err)
	}
	if err := MySQLDropAllForDomain(o.panel, 1); err != nil {
		t.Fatal(err)
	}
	if !o.baglanir(kul, kurbanPw, kurbanDB) {
		t.Errorf("domain 1 silinirken komşunun kullanıcısı düşürüldü")
	}
	if o.dbVar(t, eski) || o.dbVar(t, ad) {
		t.Errorf("domain 1'in veritabanları kaldırılmadı")
	}
	if o.kullaniciVar(t, kendi) {
		t.Errorf("domain 1'in kendi kullanıcısı kaldırılmadı")
	}
}

// Ad-alanı sahipliği (en uzun kök kazanır).
func TestDBAdiBaskaKiracininMi(t *testing.T) {
	o := dbo_ortam(t)
	on := o.on
	if _, err := o.panel.Exec(`INSERT INTO domains (id, sistem_kullanici) VALUES (3, ?)`, "c_c_"+on); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		domain int64
		ad     string
		baska  bool
	}{
		{1, on + "_x_db", true},        // kısa önek → komşunun varsayılan kullanıcısı
		{1, on + "_x", true},           // komşunun kökünün kendisi
		{1, on + "_x_main", true},      // komşunun varsayılan DB'si
		{1, "c_" + on + "_x_db", true}, // komşunun taşıma kullanıcısı (tam sk kökü)
		{1, on + "_xy_db", false},      // "<on>_x_" ile başlamıyor → kendi
		{1, on + "_db", false},         // kendi
		{1, "c_" + on + "_db", false},
		{2, on + "_x_db", false},       // komşu kendi adında
		{2, on + "_db", true},          // uzun önekli kiracı da kısa önekli komşunun adını alamaz
		{1, "wp_" + on, false},         // hiçbir köke düşmeyen ad
		{3, "c_" + on + "_db", false},  // eşit kök ("c_<on>" = domain 1'in sk'sı) engellemez
		{3, "c_" + on + "_x_db", true}, // domain 2'nin tam-sk kökü daha uzun
	} {
		baska, err := DBAdiBaskaKiracininMi(context.Background(), o.panel, c.domain, c.ad)
		if err != nil {
			t.Fatalf("%q: %v", c.ad, err)
		}
		if baska != c.baska {
			t.Errorf("domain %d, ad %q: başkasının=%v beklenir, alınan %v", c.domain, c.ad, c.baska, baska)
		}
	}
	// Bilinmeyen domain → fail-closed.
	if baska, err := DBAdiBaskaKiracininMi(context.Background(), o.panel, 12345, on+"_db"); err == nil || !baska {
		t.Errorf("bilinmeyen domain fail-closed olmalı (true+err), alınan %v %v", baska, err)
	}
}

// hesapKur: domainID'ye kayıtlı kendi DB'si olan bir hesap (DB + kullanıcı + GRANT + satır).
func (o *dboOrtam) hesapKur(t *testing.T, domainID int64, kul, parola string) string {
	t.Helper()
	dbAdi := kul + "db"
	o.kokExec(t, "CREATE USER '"+kul+"'@'localhost' IDENTIFIED BY '"+parola+"'")
	o.kokExec(t, "CREATE DATABASE `"+dbAdi+"`")
	o.kokExec(t, "GRANT ALL PRIVILEGES ON `"+GrantDBKac(dbAdi)+"`.* TO '"+kul+"'@'localhost'")
	if _, err := o.panel.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (?,?,?,?)`,
		domainID, dbAdi, kul, parola); err != nil {
		t.Fatal(err)
	}
	return dbAdi
}

// Kayıt sahipliği yetmez: satırı YALNIZ bu domainde olan ama MariaDB'de başka
// şeylerde yetkili bir hesap (yedekten dönen DB'de kiracı-yazılabilir wp-config'ten
// kurtarılan komşu/sistem hesabı, eski devralma) üzerinden parola değişmez.
func TestMySQLChangePassword_YabanciYetkiliHesap(t *testing.T) {
	o := dbo_ortam(t)
	on := o.on
	_, kurbanDB := o.kurbanKur(t, "KurbanParola123")
	o.kokExec(t, "CREATE DATABASE `"+on+"_sistem`") // panelde kayıtsız (panel/roundcube/pma benzeri)
	// Komşuya kayıtlı ama şeması şu an olmayan DB (silinmiş, geri yüklenecek).
	if _, err := o.panel.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (2,?,?,'x')`,
		on+"_x_yedek", on+"_x_db"); err != nil {
		t.Fatal(err)
	}
	rol := on + "_rol"
	o.kokExec(t, "CREATE ROLE `"+rol+"`")
	t.Cleanup(func() { _, _ = o.kok.Exec("DROP ROLE IF EXISTS `" + rol + "`") })

	for _, c := range []struct{ ad, kul, ek string }{
		{"komşunun kayıtlı DB'sinde yetkili", on + "_a1", "GRANT SELECT ON `" + GrantDBKac(kurbanDB) + "`.* TO '" + on + "_a1'@'localhost'"},
		{"kayıtsız var olan şemada yetkili", on + "_a2", "GRANT SELECT ON `" + GrantDBKac(on+"_sistem") + "`.* TO '" + on + "_a2'@'localhost'"},
		{"sunucu geneli yetki", on + "_a3", "GRANT SELECT ON *.* TO '" + on + "_a3'@'localhost'"},
		{"jokerli DB deseni", on + "_a4", "GRANT SELECT ON `" + GrantDBKac(on+"_a") + "%`.* TO '" + on + "_a4'@'localhost'"},
		{"rol ataması", on + "_a6", "GRANT `" + rol + "` TO '" + on + "_a6'@'localhost'"},
		{"komşunun kayıtlı DB'sinde tablo yetkisi", on + "_a7", "GRANT SELECT ON `" + kurbanDB + "`.`t` TO '" + on + "_a7'@'localhost'"},
		{"komşuya kayıtlı ama şu an şeması olmayan DB", on + "_a8", "GRANT ALL PRIVILEGES ON `" + GrantDBKac(on+"_x_yedek") + "`.* TO '" + on + "_a8'@'localhost'"},
		{"PROXY yetkisi", on + "_a9", "GRANT PROXY ON ''@'%' TO '" + on + "_a9'@'localhost'"},
	} {
		dbAdi := o.hesapKur(t, 1, c.kul, "EskiParola12345")
		if strings.Contains(c.ad, "tablo") {
			o.kokExec(t, "CREATE TABLE IF NOT EXISTS `"+kurbanDB+"`.`t` (id INT)")
		}
		o.kokExec(t, c.ek)
		if err := MySQLChangePassword(o.panel, 1, c.kul, "SaldirganParola123"); err == nil {
			t.Errorf("%s: hesabın parolası değiştirildi", c.ad)
		}
		if !o.baglanir(c.kul, "EskiParola12345", dbAdi) {
			t.Errorf("%s: hesabın parolası değişti", c.ad)
		}
	}

	// Meşru: kendi DB'si + silinmiş (var olmayan) DB'den artık yetki → izin verilir.
	kendi := on + "_a5"
	dbAdi := o.hesapKur(t, 1, kendi, "EskiParola12345")
	o.kokExec(t, "GRANT ALL PRIVILEGES ON `"+GrantDBKac(on+"_silinmis")+"`.* TO '"+kendi+"'@'localhost'")
	if err := MySQLChangePassword(o.panel, 1, kendi, "YeniParola12345"); err != nil {
		t.Fatalf("meşru hesabın parolası değiştirilemedi: %v", err)
	}
	if !o.baglanir(kendi, "YeniParola12345", dbAdi) {
		t.Errorf("yeni parola uygulanmadı")
	}
}

// DBKullanicisiVarMi: var olan/olmayan/geçersiz ad.
func TestDBKullanicisiVarMi(t *testing.T) {
	o := dbo_ortam(t)
	kul := o.on + "_var"
	o.kokExec(t, "CREATE USER '"+kul+"'@'localhost' IDENTIFIED BY 'VarParola12345'")
	if !DBKullanicisiVarMi(kul) {
		t.Errorf("var olan kullanıcı görülmedi")
	}
	if DBKullanicisiVarMi(o.on + "_yok") {
		t.Errorf("olmayan kullanıcı var sayıldı")
	}
	if !DBKullanicisiVarMi("a'b") {
		t.Errorf("geçersiz ad fail-closed (var) sayılmalı")
	}
}
