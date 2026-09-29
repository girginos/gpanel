package backups

// dbonek_test.go — yedekten geri yüklemede DB ÖNEK ÇAKIŞMASI (ikinci tur):
//
//  1. birDBGeriYukle "yeni ada geri yükle" yalnız "<sk>_" önekine bakıyordu: c_<on>
//     kiracısı var olan c_<on>_x komşusunun öngörülebilir adını ("c_<on>_x_joomla_2")
//     kendi ana kullanıcısına GRANT'li açabiliyordu (kota da yoktu).
//  2. tumDBGeriYukle yeniden-kayıt dalı adı komşunun ad-alanında da açıyordu.
//  3. kimlikTamamla / kullanicilar.sql: kiracının yazabildiği wp-config'ten ya da
//     arşivden gelen VAR OLAN bir hesap (komşunun kullanıcısı, servis hesabı) satıra
//     yazılıyor / ona GRANT veriliyor / USAGE satırıyla parolası arşivdeki hash'e dönüyordu.
//
// Gerçek MariaDB gerekir: YALNIZ geçici sunucu (GOSP_TEST_GECICI_DSN); yoksa ATLANIR.
// `mysql` CLI çağrıları (bash -c içindekiler dahil) PATH'e konan sarmalayıcıyla yalnız o
// sunucuya gider (--no-defaults + açık --socket; @@socket ile doğrulanır). Kiracı evi
// (wp-config) t.TempDir altında kurulur (evKoku); canlı /home'a dokunulmaz.

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/hesaplar"
)

type bdoOrtam struct {
	panel *sql.DB
	kok   *sql.DB
	cfg   *mysql.Config
	on    string
}

// bdo_ortam: domain 1 = c_<on> (kısa önek, saldırgan; customer 7 → plan 9),
// domain 2 = c_<on>_x (kurban).
func bdo_ortam(t *testing.T) *bdoOrtam {
	t.Helper()
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li test atlandı (canlı MariaDB'ye ASLA gidilmez)")
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
	// Kiracı evleri (wp-config kurtarma) geçici dizinde: canlı /home'a bakılmaz.
	eskiEv := evKoku
	evKoku = t.TempDir()
	t.Cleanup(func() { evKoku = eskiEv })
	for _, k := range []string{"mysql -N -B -e 'SELECT @@socket'", "bash -c \"mysql -N -B -e 'SELECT @@socket'\""} {
		out, err := exec.Command("sh", "-c", k).CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != cfg.Addr {
			t.Fatalf("mysql sarmalayıcısı geçici sunucuya gitmiyor (%s → %q, %v) — test durduruldu", k, strings.TrimSpace(string(out)), err)
		}
	}
	kcfg := cfg.Clone()
	kcfg.DBName = ""
	kok, err := sql.Open("mysql", kcfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = kok.Close() })
	on := "bdo" + strconv.FormatInt(time.Now().UnixNano()%1e12, 36)
	ad := "gosp_test_" + on
	if _, err := kok.Exec("CREATE DATABASE " + ad); err != nil {
		t.Fatalf("geçici veritabanı oluşturulamadı: %v", err)
	}
	t.Cleanup(func() {
		topla := func(q string, desen string) []string {
			var l []string
			if rows, err := kok.Query(q, desen); err == nil {
				for rows.Next() {
					var s string
					if rows.Scan(&s) == nil {
						l = append(l, s)
					}
				}
				rows.Close()
			}
			return l
		}
		for _, desen := range []string{on + "%", "c\\_" + on + "%", "wpu\\_" + on + "%"} {
			for _, u := range topla(`SELECT User FROM mysql.user WHERE User LIKE ?`, desen) {
				_, _ = kok.Exec("DROP USER IF EXISTS '" + u + "'@'localhost'")
			}
			for _, d := range topla(`SELECT SCHEMA_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME LIKE ?`, desen) {
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
			sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '', customer_id BIGINT UNSIGNED NULL) ENGINE=InnoDB`,
		`CREATE TABLE customers (id BIGINT UNSIGNED NOT NULL PRIMARY KEY, plan_id BIGINT UNSIGNED NULL) ENGINE=InnoDB`,
		`CREATE TABLE service_plans (id BIGINT UNSIGNED NOT NULL PRIMARY KEY, max_db INT NOT NULL DEFAULT 0) ENGINE=InnoDB`,
		// canlı şema: DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci (harf duyarsız)
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
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		fmt.Sprintf(`INSERT INTO domains (id, sistem_kullanici, customer_id) VALUES (1,'c_%s',7), (2,'c_%s_x',NULL)`, on, on),
		`INSERT INTO customers (id, plan_id) VALUES (7, 9)`,
		`INSERT INTO service_plans (id, max_db) VALUES (9, 50)`,
	} {
		if _, err := panel.Exec(q); err != nil {
			t.Fatalf("şema: %v\n%s", err, q)
		}
	}
	return &bdoOrtam{panel: panel, kok: kok, cfg: cfg, on: on}
}

func (o *bdoOrtam) kokExec(t *testing.T, q string) {
	t.Helper()
	if _, err := o.kok.Exec(q); err != nil {
		t.Fatalf("root SQL: %v\n%s", err, q)
	}
}

func (o *bdoOrtam) baglanir(kul, parola, dbAdi string) bool {
	c := o.cfg.Clone()
	c.User, c.Passwd, c.DBName = kul, parola, dbAdi
	db, err := sql.Open("mysql", c.FormatDSN())
	if err != nil {
		return false
	}
	defer db.Close()
	return db.Ping() == nil
}

func (o *bdoOrtam) sayi(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("sayım: %v (%s)", err, q)
	}
	return n
}

func (o *bdoOrtam) dbVar(t *testing.T, ad string) bool {
	return o.sayi(t, o.kok, `SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME=?`, ad) > 0
}

// yetkiVar: kul'un ad üzerinde (kaçışlı/kaçışsız) db-düzeyi yetkisi var mı?
func (o *bdoOrtam) yetkiVar(t *testing.T, kul, ad string) bool {
	return o.sayi(t, o.kok, `SELECT COUNT(*) FROM mysql.db WHERE User=? AND (Db=? OR Db=?)`,
		kul, ad, hesaplar.GrantDBKac(ad)) > 0
}

// kurbanKur: domain 2'nin kayıtlı varsayılan kullanıcısı + DB'si.
func (o *bdoOrtam) kurbanKur(t *testing.T, parola string) (kul, dbAdi string) {
	t.Helper()
	kul, dbAdi = o.on+"_x_db", o.on+"_x_main"
	if err := hesaplar.MySQLCreateDB(o.panel, 2, dbAdi, kul, parola); err != nil {
		t.Fatal(err)
	}
	return kul, dbAdi
}

// arsiv: tmp/__db__/<ad>.sql dosyaları (her biri bir tablo oluşturur).
func arsiv(t *testing.T, adlar ...string) string {
	t.Helper()
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "__db__"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, a := range adlar {
		if err := os.WriteFile(filepath.Join(tmp, "__db__", a+".sql"),
			[]byte("CREATE TABLE geri_t (id INT);\nINSERT INTO geri_t VALUES (42);\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return tmp
}

// 🔴 BULGU 1: "yeni ada geri yükle" komşunun tam-sk ad-alanına açılamaz; kota uygulanır.
func TestBirDBGeriYukle_YeniHedefAdAlaniVeKota(t *testing.T) {
	o := bdo_ortam(t)
	on := o.on
	sk := "c_" + on
	ana, anaPw := on+"_anau", "AnaParola12345"
	if err := hesaplar.MySQLCreateDB(o.panel, 1, on+"_ana", ana, anaPw); err != nil {
		t.Fatal(err)
	}
	o.kurbanKur(t, "KurbanParola123")
	tmp := arsiv(t, on+"_ana")

	for _, hedef := range []string{sk + "_x_joomla_2", sk + "_x", sk + "_X_wp"} {
		if _, err := birDBGeriYukle(o.panel, 1, tmp, sk, on+"_ana", hedef); err == nil {
			t.Errorf("komşunun ad-alanındaki %q hedefine geri yükleme BAŞARILI oldu", hedef)
		}
		if o.dbVar(t, hedef) || o.yetkiVar(t, ana, hedef) {
			t.Errorf("reddedilen hedef %q için DB/GRANT oluştu", hedef)
		}
		if o.sayi(t, o.panel, `SELECT COUNT(*) FROM db_accounts WHERE db_name=?`, hedef) != 0 {
			t.Errorf("reddedilen hedef %q kaydedildi", hedef)
		}
	}
	// Meşru: kendi ad-alanında yeni ad.
	yeni := sk + "_kopya"
	if _, err := birDBGeriYukle(o.panel, 1, tmp, sk, on+"_ana", yeni); err != nil {
		t.Fatalf("meşru yeni ada geri yükleme düştü: %v", err)
	}
	if !o.baglanir(ana, anaPw, yeni) {
		t.Errorf("yeni ada geri yüklenen DB'ye ana kullanıcı bağlanamıyor")
	}
	if o.sayi(t, o.kok, "SELECT COUNT(*) FROM `"+yeni+"`.geri_t") != 1 {
		t.Errorf("döküm içe aktarılmadı")
	}
	// Kota: plan limiti doluysa yeni hedef açılmaz.
	if _, err := o.panel.Exec(`UPDATE service_plans SET max_db=? WHERE id=9`,
		o.sayi(t, o.panel, `SELECT COUNT(*) FROM db_accounts WHERE domain_id=1`)); err != nil {
		t.Fatal(err)
	}
	if _, err := birDBGeriYukle(o.panel, 1, tmp, sk, on+"_ana", sk+"_kopya2"); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Errorf("plan limiti dolu iken yeni ada geri yükleme reddedilmedi: %v", err)
	}
	if o.dbVar(t, sk+"_kopya2") {
		t.Errorf("limit aşımında DB oluştu")
	}
}

// 🔴 BULGU 1 (toplu yol): yeniden-kayıt dalı; komşu ad-alanı, kayıtsız var olan şema, kota,
// yeni açılan şemadaki artık yetki.
func TestTumDBGeriYukle_YenidenKayitKapilari(t *testing.T) {
	o := bdo_ortam(t)
	on := o.on
	sk := "c_" + on
	o.kurbanKur(t, "KurbanParola123")
	spy, spyPw := on+"_spy", "SpyParola12345"
	if err := hesaplar.MySQLCreateDB(o.panel, 1, on+"_ana", spy, spyPw); err != nil {
		t.Fatal(err)
	}
	komsuAd := on + "_x_yedek" // komşunun ad-alanı (kayıtsız, şeması yok)
	varolan := on + "_varolan" // kendi ad-alanı, panelde kayıtsız ama sunucuda var
	o.kokExec(t, "CREATE DATABASE `"+varolan+"`")
	kendi := on + "_eski" // kendi ad-alanı; üzerinde artık yetki (eski KeepUser kalıntısı)
	artik := on + "_artik"
	o.kokExec(t, "CREATE USER '"+artik+"'@'localhost' IDENTIFIED BY 'ArtikParola123'")
	o.kokExec(t, "GRANT ALL PRIVILEGES ON `"+hesaplar.GrantDBKac(kendi)+"`.* TO '"+artik+"'@'localhost'")
	tmp := arsiv(t, komsuAd, varolan, kendi)

	durum := map[string]string{}
	for _, r := range tumDBGeriYukle(o.panel, 1, tmp, sk, "") {
		durum[r["db"]] = r["durum"]
	}
	if !strings.HasPrefix(durum[komsuAd], "reddedildi") || o.dbVar(t, komsuAd) {
		t.Errorf("komşunun ad-alanındaki ad yeniden açıldı: %q", durum[komsuAd])
	}
	if !strings.HasPrefix(durum[varolan], "reddedildi") ||
		o.sayi(t, o.kok, "SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=?", varolan) != 0 {
		t.Errorf("panelde kayıtsız var olan şemanın üstüne yazıldı: %q", durum[varolan])
	}
	if !strings.HasPrefix(durum[kendi], "geri yüklendi") {
		t.Fatalf("meşru kurtarma düştü: %q", durum[kendi])
	}
	if o.sayi(t, o.panel, `SELECT COUNT(*) FROM db_accounts WHERE domain_id=1 AND db_name=?`, kendi) != 1 {
		t.Errorf("kurtarılan DB yeniden kaydedilmedi")
	}
	if o.yetkiVar(t, artik, kendi) || o.baglanir(artik, "ArtikParola123", kendi) {
		t.Errorf("yeni açılan şemada artık yetki kaldı")
	}
	// Kota: limit doluysa yeniden kayıt yapılmaz.
	if _, err := o.panel.Exec(`UPDATE service_plans SET max_db=? WHERE id=9`,
		o.sayi(t, o.panel, `SELECT COUNT(*) FROM db_accounts WHERE domain_id=1`)); err != nil {
		t.Fatal(err)
	}
	ek := on + "_ek"
	for _, r := range tumDBGeriYukle(o.panel, 1, arsiv(t, ek), sk, "") {
		if r["db"] == ek && !strings.HasPrefix(r["durum"], "reddedildi") {
			t.Errorf("plan limiti dolu iken yeniden kayıt yapıldı: %q", r["durum"])
		}
	}
	if o.dbVar(t, ek) {
		t.Errorf("limit aşımında DB oluştu")
	}
}

// Üzerine geri yükleme (kayıtlı DB şeması kayıp): yeni açılan şemada kayıtlı sahibin
// yetkisi KORUNUR, başkasınınki silinir.
func TestTumDBGeriYukle_KayitliSahipKorunur(t *testing.T) {
	o := bdo_ortam(t)
	on := o.on
	sk := "c_" + on
	ad, sahip, pw := on+"_site", on+"_siteu", "SiteParola12345"
	if err := hesaplar.MySQLCreateDB(o.panel, 1, ad, sahip, pw); err != nil {
		t.Fatal(err)
	}
	artik := on + "_artik"
	o.kokExec(t, "CREATE USER '"+artik+"'@'localhost' IDENTIFIED BY 'ArtikParola123'")
	o.kokExec(t, "GRANT ALL PRIVILEGES ON `"+hesaplar.GrantDBKac(ad)+"`.* TO '"+artik+"'@'localhost'")
	o.kokExec(t, "DROP DATABASE `"+ad+"`") // panel dışında kaybolmuş şema
	res := tumDBGeriYukle(o.panel, 1, arsiv(t, ad), sk, "")
	if len(res) != 1 || !strings.HasPrefix(res[0]["durum"], "geri yüklendi") {
		t.Fatalf("geri yükleme düştü: %v", res)
	}
	if !o.baglanir(sahip, pw, ad) {
		t.Errorf("kayıtlı sahip geri yüklenen DB'ye bağlanamıyor")
	}
	if o.yetkiVar(t, artik, ad) {
		t.Errorf("başka hesabın artık yetkisi kaldı")
	}
}

// 🔴 BULGU 3: kimlikTamamla yabancı hesabı satıra yazmaz / ona GRANT vermez.
func TestKimlikTamamla_YabanciHesap(t *testing.T) {
	o := bdo_ortam(t)
	on := o.on
	sk := "c_" + on
	ev := evKoku // bdo_ortam'ın geçici ev kökü
	if err := os.MkdirAll(filepath.Join(ev, sk, "public_html"), 0o755); err != nil {
		t.Fatal(err)
	}
	wp := func(dbAdi, kul, pw string) {
		t.Helper()
		icerik := "<?php\ndefine('DB_NAME', '" + dbAdi + "');\ndefine('DB_USER', '" + kul + "');\ndefine('DB_PASSWORD', '" + pw + "');\n"
		if err := os.WriteFile(filepath.Join(ev, sk, "public_html", "wp-config.php"), []byte(icerik), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	kurbanPw := "KurbanParola123"
	kurban, kurbanDB := o.kurbanKur(t, kurbanPw)
	svc := on + "svc"
	o.kokExec(t, "CREATE USER '"+svc+"'@'localhost' IDENTIFIED BY 'SvcParola12345'")
	o.kokExec(t, "CREATE DATABASE `"+on+"svcsema`")
	o.kokExec(t, "GRANT ALL PRIVILEGES ON `"+on+"svcsema`.* TO '"+svc+"'@'localhost'")

	yeniDB := func(ad string) {
		t.Helper()
		o.kokExec(t, "CREATE DATABASE `"+ad+"`")
		if _, err := o.panel.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (1,?,'','')`, ad); err != nil {
			t.Fatal(err)
		}
	}
	satirKul := func(ad string) string {
		var u string
		_ = o.panel.QueryRow(`SELECT db_user FROM db_accounts WHERE BINARY db_name=?`, ad).Scan(&u)
		return u
	}
	for i, c := range []struct{ ad, kul string }{
		{"komşuya kayıtlı hesap", kurban},
		{"kendi şemasında yetkili servis hesabı", svc},
		{"komşunun ad-alanında yeni ad (büyük harf)", strings.ToUpper(on + "_x_yeni")},
		{"komşunun ad-alanında yeni ad", on + "_x_yeni2"},
		{"sistem hesabı", "pma"},
	} {
		ad := on + "_r" + strconv.Itoa(i)
		yeniDB(ad)
		wp(ad, c.kul, "SaldirganParola1")
		kimlikTamamla(o.panel, 1, sk, ad)
		if u := satirKul(ad); u != "" {
			t.Errorf("%s: satıra %q yazıldı", c.ad, u)
		}
		if o.yetkiVar(t, c.kul, ad) {
			t.Errorf("%s: hesaba kiracının DB'sinde GRANT verildi", c.ad)
		}
		if o.sayi(t, o.kok, `SELECT COUNT(*) FROM mysql.user WHERE User=?`, c.kul) > 0 &&
			(c.kul == strings.ToUpper(on+"_x_yeni") || c.kul == on+"_x_yeni2") {
			t.Errorf("%s: komşunun ad-alanında hesap oluşturuldu", c.ad)
		}
	}
	if !o.baglanir(kurban, kurbanPw, kurbanDB) {
		t.Errorf("komşunun parolası/erişimi bozuldu")
	}
	if err := hesaplar.MySQLChangePassword(o.panel, 2, kurban, "KurbanYeni12345"); err != nil {
		t.Errorf("komşunun kendi parola değişimi kilitlendi: %v", err)
	}

	// Adım 1: komşunun hesabının geri yüklenen DB'de yetkisi var (dbHesaplari onu döner)
	// → bağlanmaz; wp-config'teki kendi yeni kullanıcısı bağlanır.
	ad := on + "_mesru"
	yeniDB(ad)
	o.kokExec(t, "GRANT SELECT ON `"+hesaplar.GrantDBKac(ad)+"`.* TO '"+kurban+"'@'localhost'")
	kendi, kendiPw := on+"_wpu", "KendiParola123"
	wp(ad, kendi, kendiPw)
	kimlikTamamla(o.panel, 1, sk, ad)
	if u := satirKul(ad); u != kendi {
		t.Errorf("meşru kurtarma: satırda %q, beklenen %q", u, kendi)
	}
	if !o.baglanir(kendi, kendiPw, ad) {
		t.Errorf("meşru kurtarılan kullanıcı DB'ye bağlanamıyor")
	}
}

// 🔴 BULGU 3 (arşiv yolu): kullanicilar.sql yabancı hesaba GRANT vermez, USAGE satırıyla
// parolasını değiştirmez; kendi hesabını geri getirir.
func TestDbKullanicilariUygula_YabanciHesapFiltrelenir(t *testing.T) {
	o := bdo_ortam(t)
	on := o.on
	kurbanPw := "KurbanParola123"
	kurban, kurbanDB := o.kurbanKur(t, kurbanPw)
	ad := on + "_geri"
	o.kokExec(t, "CREATE DATABASE `"+ad+"`")
	if _, err := o.panel.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (1,?,'','')`, ad); err != nil {
		t.Fatal(err)
	}
	var hash string
	if err := o.kok.QueryRow(`SELECT PASSWORD('SaldirganParola1')`).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	kendi := on + "_geriu"
	dir := t.TempDir()
	satirlar := []string{
		"GRANT USAGE ON *.* TO `" + kurban + "`@`localhost` IDENTIFIED BY PASSWORD '" + hash + "';",
		"GRANT ALL PRIVILEGES ON `" + ad + "`.* TO `" + kurban + "`@`localhost`;",
		"CREATE USER IF NOT EXISTS `" + strings.ToUpper(on+"_x_db2") + "`@`localhost` IDENTIFIED BY PASSWORD '" + hash + "';",
		"CREATE USER IF NOT EXISTS `" + kendi + "`@`localhost` IDENTIFIED BY PASSWORD '" + hash + "';",
		"GRANT ALL PRIVILEGES ON `" + ad + "`.* TO `" + kendi + "`@`localhost`;",
	}
	if err := os.WriteFile(filepath.Join(dir, kullaniciDosyaAdi), []byte(strings.Join(satirlar, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := dbKullanicilariUygula(o.panel, 1, dir, map[string]bool{ad: true}); err != nil {
		t.Fatal(err)
	}
	if !o.baglanir(kurban, kurbanPw, kurbanDB) {
		t.Errorf("komşunun parolası arşivdeki hash'e çevrildi")
	}
	if o.yetkiVar(t, kurban, ad) {
		t.Errorf("komşunun hesabına kiracının DB'sinde GRANT verildi")
	}
	if o.sayi(t, o.kok, `SELECT COUNT(*) FROM mysql.user WHERE User=?`, strings.ToUpper(on+"_x_db2")) != 0 {
		t.Errorf("komşunun ad-alanında (büyük harf) hesap oluşturuldu")
	}
	if !o.baglanir(kendi, "SaldirganParola1", ad) {
		t.Errorf("kendi hesabı arşivden geri gelmedi")
	}
}

func TestSatirHesabi(t *testing.T) {
	for _, c := range []struct {
		s, kul, host string
		ok           bool
	}{
		{"CREATE USER IF NOT EXISTS `a_b`@`localhost` IDENTIFIED BY PASSWORD '*AB'", "a_b", "localhost", true},
		{"CREATE USER IF NOT EXISTS 'a'@'%' IDENTIFIED BY PASSWORD '*AB'", "a", "%", true},
		{"GRANT USAGE ON *.* TO `a`@`localhost` IDENTIFIED BY PASSWORD '*AB'", "a", "localhost", true},
		{"GRANT ALL PRIVILEGES ON `d\\_b`.* TO `k`@`localhost` WITH GRANT OPTION", "k", "localhost", true},
		{"GRANT SELECT ON `d`.`t TO x` TO `k`@`h`", "k", "h", true},
		// hash içindeki sahte " TO " hesabı değiştirmez
		{"GRANT USAGE ON *.* TO `kurban`@`localhost` IDENTIFIED BY PASSWORD ' TO `ben`@`localhost`'", "kurban", "localhost", true},
		{"GRANT PROXY ON ''@'%' TO 'a'@'localhost'", "", "", false},
		{"GRANT ALL ON `d`.* TO", "", "", false},
		{"DROP USER `a`@`localhost`", "", "", false},
	} {
		k, h, ok := satirHesabi(c.s)
		if ok != c.ok || k != c.kul || h != c.host {
			t.Errorf("satirHesabi(%q) = %q %q %v; beklenen %q %q %v", c.s, k, h, ok, c.kul, c.host, c.ok)
		}
	}
}
