package tasima

// dbonek_test.go — taşıma FALLBACK yolunun <sk>_db panel kullanıcısı
// (panelKullanicisiylaOlustur). hesaplar.MySQLCreateDB artık yalnız-yeni: var olan
// kullanıcıya dokunmaz. Meşru "üstüne taşıma"da kullanıcı bu domainindir → parolası
// sahiplik doğrulanarak sıfırlanır ve yeni DB ona bağlanır; kullanıcı BAŞKA domaine
// aitse (önek çakışması: c_girgin'in ad-alanındaki "c_girgin_x_db" = c_girgin_x'in
// kullanıcısı) reddedilir. Eskiden koşulsuz ALTER USER onu devralıyordu.
//
// Gerçek MariaDB gerekir; GOSP_TEST_GECICI_DSN (unix soketli geçici sunucu) yoksa ATLANIR.
// `mysql` CLI çağrıları PATH'e konan sarmalayıcıyla YALNIZ geçici sunucuya gider.

import (
	"context"
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
)

type dboOrtam struct {
	h   *Handlers
	kok *sql.DB
	cfg *mysql.Config
	on  string
}

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
	on := "dbt" + strconv.FormatInt(time.Now().UnixNano()%1e12, 36)
	ad := "gosp_test_" + on
	if _, err := kok.Exec("CREATE DATABASE " + ad); err != nil {
		t.Fatalf("geçici veritabanı oluşturulamadı: %v", err)
	}
	t.Cleanup(func() {
		topla := func(q string) []string {
			var l []string
			if rows, err := kok.Query(q, "%"+on+"%"); err == nil {
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
		for _, u := range topla(`SELECT User FROM mysql.user WHERE User LIKE ?`) {
			_, _ = kok.Exec("DROP USER IF EXISTS '" + u + "'@'localhost'")
		}
		for _, d := range topla(`SELECT SCHEMA_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME LIKE ?`) {
			if d != ad {
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
	return &dboOrtam{h: &Handlers{DB: panel}, kok: kok, cfg: cfg, on: on}
}

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

func TestPanelKullanicisiylaOlustur(t *testing.T) {
	o := dbo_ortam(t)
	sk1, sk2 := "c_"+o.on, "c_"+o.on+"_x"

	// (1) Kullanıcı yok → yeni oluşur.
	kul1 := sk1 + "_db"
	if err := o.h.panelKullanicisiylaOlustur(1, sk1+"_wp", kul1, "IlkParola12345"); err != nil {
		t.Fatalf("yeni taşıma kullanıcısı oluşturulamadı: %v", err)
	}
	if !o.baglanir(kul1, "IlkParola12345", sk1+"_wp") {
		t.Errorf("yeni kullanıcı DB'ye bağlanamıyor")
	}

	// (2) Üstüne taşıma: kullanıcı ZATEN bu domainin → parola dbPw'ye çekilir, yeni DB bağlanır.
	if err := o.h.panelKullanicisiylaOlustur(1, sk1+"_wp_2", kul1, "IkinciParola12345"); err != nil {
		t.Fatalf("üstüne taşımada kendi kullanıcısı yeniden kullanılamadı: %v", err)
	}
	if !o.baglanir(kul1, "IkinciParola12345", sk1+"_wp_2") {
		t.Errorf("üstüne taşımada yeni parola/DB uygulanmadı")
	}
	if !o.baglanir(kul1, "IkinciParola12345", sk1+"_wp") {
		t.Errorf("üstüne taşımada eski DB'ye erişim kayboldu")
	}

	// (3) Kullanıcı BAŞKA domainin (kurban c_<on>_x'in taşıma kullanıcısı) → red.
	kurban := sk2 + "_db"
	if _, err := o.kok.Exec("CREATE USER '" + kurban + "'@'localhost' IDENTIFIED BY 'KurbanParola123'"); err != nil {
		t.Fatal(err)
	}
	if _, err := o.kok.Exec("CREATE DATABASE `" + sk2 + "_site`"); err != nil {
		t.Fatal(err)
	}
	if _, err := o.kok.Exec("GRANT ALL PRIVILEGES ON `" + strings.ReplaceAll(sk2+"_site", "_", `\_`) + "`.* TO '" + kurban + "'@'localhost'"); err != nil {
		t.Fatal(err)
	}
	if _, err := o.h.DB.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (2,?,?,'KurbanParola123')`,
		sk2+"_site", kurban); err != nil {
		t.Fatal(err)
	}
	hedef := sk1 + "_x_db_kapma"
	if err := o.h.panelKullanicisiylaOlustur(1, hedef, kurban, "SaldirganParola123"); err == nil {
		t.Errorf("başka domainin kullanıcısı taşıma yoluyla devralındı")
	}
	if !o.baglanir(kurban, "KurbanParola123", sk2+"_site") {
		t.Errorf("komşunun parolası değişti")
	}
	if o.baglanir(kurban, "SaldirganParola123", "") {
		t.Errorf("saldırganın parolası komşunun kullanıcısına yazıldı")
	}
	var n int
	_ = o.h.DB.QueryRow(`SELECT COUNT(*) FROM db_accounts WHERE domain_id=1 AND db_user=?`, kurban).Scan(&n)
	if n != 0 {
		t.Errorf("komşunun kullanıcısı domain 1'e kaydedildi")
	}

	// (4) Kayıtsız artık kullanıcı (hiçbir domainde yok) → red (sahipliği belirsiz).
	artik := sk1 + "_artik"
	if _, err := o.kok.Exec("CREATE USER '" + artik + "'@'localhost' IDENTIFIED BY 'ArtikParola12345'"); err != nil {
		t.Fatal(err)
	}
	if err := o.h.panelKullanicisiylaOlustur(1, sk1+"_artikdb", artik, "SaldirganParola123"); err == nil {
		t.Errorf("kayıtsız var olan kullanıcı devralındı")
	}
	if !o.baglanir(artik, "ArtikParola12345", "") {
		t.Errorf("kayıtsız kullanıcının parolası değişti")
	}

	// (5) Kullanıcı panelde YALNIZ bu domaine kayıtlı ama MariaDB'de komşunun DB'sinde de
	// yetkili (eski devralma / kiracı-yazılabilir yapılandırmadan kurtarılmış ad) →
	// parola sıfırlanmaz VE yeni DB ona bağlanmaz.
	yabanci := sk1 + "_yabanci"
	if _, err := o.kok.Exec("CREATE USER '" + yabanci + "'@'localhost' IDENTIFIED BY 'YabanciParola123'"); err != nil {
		t.Fatal(err)
	}
	if _, err := o.kok.Exec("CREATE DATABASE `" + yabanci + "_db1`"); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{yabanci + "_db1", sk2 + "_site"} {
		if _, err := o.kok.Exec("GRANT ALL PRIVILEGES ON `" + strings.ReplaceAll(d, "_", `\_`) + "`.* TO '" + yabanci + "'@'localhost'"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := o.h.DB.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (1,?,?,'YabanciParola123')`,
		yabanci+"_db1", yabanci); err != nil {
		t.Fatal(err)
	}
	yeniDB := sk1 + "_ydb"
	if err := o.h.panelKullanicisiylaOlustur(1, yeniDB, yabanci, "SaldirganParola123"); err == nil {
		t.Errorf("komşunun DB'sinde yetkili hesap taşıma yoluyla kullanıldı")
	}
	if !o.baglanir(yabanci, "YabanciParola123", yabanci+"_db1") {
		t.Errorf("komşunun DB'sinde yetkili hesabın parolası değişti")
	}
	var dbSayi int
	_ = o.kok.QueryRow(`SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME=?`, yeniDB).Scan(&dbSayi)
	if dbSayi != 0 {
		t.Errorf("reddedilen taşımada yeni DB %q oluşturuldu", yeniDB)
	}
}

// 🔴 DAHA UZUN KÖKLÜ KOMŞU (inceleme (b)): <on>-db.test (tam sk'sı c_<on>_db) kurbanın
// taşıma kullanıcısıyla (<sk>_db = c_<on>_db) AYNI adı taşır. Eskiden hedef kullanıcı
// sabitti; komşunun sahibi adı açınca kurbanın taşıması hiç DB aktaramıyordu. Artık:
//   - taşıma kullanıcısı kurbanın KENDİ ad-alanında seçilir (c_<on>_db2) ve gerçekten
//     oluşturulup DB'ye bağlanabilir;
//   - üstüne taşımada panelde yalnız bu domaine kayıtlı kullanıcı yeniden seçilir;
//   - komşunun (daha uzun kökün) ad-alanına düşen hedef DB adı ve orijinal kimlik
//     kullanılmaz.
func TestTasimaAdlari_UzunKokluKomsu(t *testing.T) {
	o := dbo_ortam(t)
	on := o.on
	sk := "c_" + on
	ctx := context.Background()

	// Komşu yokken: temel ad.
	if kul, err := o.h.tasimaKullanicisiSec(ctx, 1, sk); err != nil || kul != sk+"_db" {
		t.Fatalf("komşusuz taşıma kullanıcısı %s_db beklenir: %q %v", sk, kul, err)
	}
	if _, err := o.h.DB.Exec(`INSERT INTO domains (id, sistem_kullanici) VALUES (3, ?), (4, ?)`,
		sk+"_db", sk+"_joomla"); err != nil {
		t.Fatal(err)
	}
	kul, err := o.h.tasimaKullanicisiSec(ctx, 1, sk)
	if err != nil || kul != sk+"_db2" {
		t.Fatalf("komşunun köküne düşen taşıma kullanıcısı için %s_db2 beklenir: %q %v", sk, kul, err)
	}
	// Gerçek oluşturma: kullanıcı + DB oluşur, bağlanılır, satır kurbanın domain_id'siyle.
	hedef, err := o.h.benzersizHedefDB(ctx, 1, sk, "src_wp", "src")
	if err != nil || hedef != sk+"_wp" {
		t.Fatalf("hedef DB %s_wp beklenir: %q %v", sk, hedef, err)
	}
	if err := o.h.panelKullanicisiylaOlustur(1, hedef, kul, "TasimaParola12345"); err != nil {
		t.Fatalf("kendi ad-alanındaki taşıma kullanıcısı oluşturulamadı: %v", err)
	}
	if !o.baglanir(kul, "TasimaParola12345", hedef) {
		t.Errorf("seçilen taşıma kullanıcısı DB'ye bağlanamıyor")
	}
	// Üstüne taşıma: panelde yalnız bu domaine kayıtlı → aynı ad yeniden seçilir.
	if kul2, err := o.h.tasimaKullanicisiSec(ctx, 1, sk); err != nil || kul2 != kul {
		t.Errorf("üstüne taşımada kendi kullanıcısı yeniden seçilmedi: %q %v", kul2, err)
	}
	// Başka domaine kayıtlı / kayıtsız var olan ad atlanır.
	if _, err := o.h.DB.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (2,?,?,'x')`,
		sk+"_x_kayit", kul); err != nil {
		t.Fatal(err)
	}
	if kul3, err := o.h.tasimaKullanicisiSec(ctx, 1, sk); err != nil || kul3 != sk+"_db3" {
		t.Errorf("iki domaine kayıtlı ad atlanmadı (%s_db3 beklenir): %q %v", sk, kul3, err)
	}

	// Hedef DB: "<sk>_joomla_x" komşu c_<on>_joomla'nın ad-alanında → kendi kökte kompakt ad.
	if hedef, err := o.h.benzersizHedefDB(ctx, 1, sk, "src_joomla_x", "src"); err != nil || hedef != sk+"_joomlax" {
		t.Errorf("komşunun ad-alanına düşen hedef DB için %s_joomlax beklenir: %q %v", sk, hedef, err)
	}
	// Orijinal kimlik: komşunun ad-alanındaki ad kullanılmaz, ad-alanı dışı ad kullanılır.
	for ad, uygun := range map[string]bool{
		sk + "_db":        false, // komşunun kökünün kendisi
		sk + "_joomla_7":  false,
		on + "_db":        false, // komşunun c'siz kökü
		"src_wp":          true,
		sk + "_kendi":     true,
		"C_" + on + "_DB": false,
	} {
		if got := o.h.kendiAdAlaniUygun(ctx, 1, ad); got != uygun {
			t.Errorf("kendiAdAlaniUygun(%q)=%v beklenir %v", ad, got, uygun)
		}
	}
	if o.h.kendiAdAlaniUygun(ctx, 999, "src_wp") {
		t.Errorf("bilinmeyen domain uygun sayıldı (fail-closed olmalı)")
	}
}
