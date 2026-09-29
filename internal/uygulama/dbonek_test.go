package uygulama

// dbonek_test.go — uygulama kurulumunun DB adımı (uygulamaDBKur):
//   - "<sk>_" öneki sahiplik kanıtı DEĞİL: alt çizgili sk'larda ad-alanları iç içe
//     geçer (c_girgin'in "c_girgin_x_db"si = c_girgin_x'in taşıma kullanıcısı) → red.
//   - Oluşturma reddedilirse (ad ZATEN VAR — başkasının olabilir) geri alma ÇALIŞMAZ:
//     eskiden defer, başarısız oluşturmada da `DROP USER <ad>` koşuyordu.
//
// Panel şeması için gerçek MariaDB gerekir; GOSP_TEST_GECICI_DSN (unix soketli geçici
// sunucu) yoksa ATLANIR. hesaplar.DBVarMi'nin `mysql` CLI çağrısı PATH'e konan
// sarmalayıcıyla YALNIZ geçici sunucuya gider. MariaDB DDL'i sahte geri çağrılarla
// kaydedilir (gerçek oluşturma hesaplar testlerinde).

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

	"girginospanel/internal/hesaplar"
)

func dbo_ortam(t *testing.T) (*sql.DB, *sql.DB, string) {
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
	on := "dbu" + strconv.FormatInt(time.Now().UnixNano()%1e12, 36)
	ad := "gosp_test_" + on
	if _, err := kok.Exec("CREATE DATABASE " + ad); err != nil {
		t.Fatalf("geçici veritabanı oluşturulamadı: %v", err)
	}
	t.Cleanup(func() {
		_, _ = kok.Exec("DROP DATABASE IF EXISTS `c_" + on + "_var`")
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
		fmt.Sprintf(`INSERT INTO domains (id, sistem_kullanici) VALUES (1,'c_%s'), (2,'c_%s_x')`, on, on),
	} {
		if _, err := panel.Exec(q); err != nil {
			t.Fatalf("şema: %v\n%s", err, q)
		}
	}
	return panel, kok, on
}

type dboSahte struct {
	olustur []string
	dusur   []string
	hata    error
}

func (s *dboSahte) mysqlOlustur(_ int64, dbAd, dbUser, _ string) error {
	s.olustur = append(s.olustur, dbAd+"/"+dbUser)
	return s.hata
}

func (s *dboSahte) mysqlDrop(_ int64, dbAd, dbUser string) error {
	s.dusur = append(s.dusur, dbAd+"/"+dbUser)
	return nil
}

func TestUygulamaDBKur_OnekCakismasiVeGeriAlma(t *testing.T) {
	panel, kok, on := dbo_ortam(t)
	ctx := context.Background()
	tarif := &Tarif{Kod: "joomla", DBGerek: true}
	dom := Domain{ID: 1, SK: "c_" + on, AlanAdi: on + ".test"}

	// (1) Komşunun ad-alanı (c_<on>_x'in taşıma kullanıcısı) → red, oluşturma çağrılmaz.
	s := &dboSahte{}
	if _, _, err := uygulamaDBKur(ctx, panel, KurArgs{Domain: dom, Tarif: tarif, DBAdi: "c_" + on + "_x_db"}, s.mysqlOlustur, s.mysqlDrop); err == nil {
		t.Errorf("komşunun ad-alanındaki DB adı kabul edildi")
	}
	if len(s.olustur) != 0 {
		t.Errorf("reddedilen ad için oluşturma çağrıldı: %v", s.olustur)
	}

	// (2) Kendi ad-alanı dışı → red (eski kapı).
	s = &dboSahte{}
	if _, _, err := uygulamaDBKur(ctx, panel, KurArgs{Domain: dom, Tarif: tarif, DBAdi: "baska_db"}, s.mysqlOlustur, s.mysqlDrop); err == nil || len(s.olustur) != 0 {
		t.Errorf("ad-alanı dışı ad kabul edildi (err=%v, olustur=%v)", err, s.olustur)
	}

	// (3) Var olan DB → red.
	if _, err := kok.Exec("CREATE DATABASE `c_" + on + "_var`"); err != nil {
		t.Fatal(err)
	}
	s = &dboSahte{}
	if _, _, err := uygulamaDBKur(ctx, panel, KurArgs{Domain: dom, Tarif: tarif, DBAdi: "c_" + on + "_var"}, s.mysqlOlustur, s.mysqlDrop); err == nil || len(s.olustur) != 0 {
		t.Errorf("var olan DB kabul edildi (err=%v, olustur=%v)", err, s.olustur)
	}

	// (4) Oluşturma "zaten var" ile reddedilirse var olan kimlik DÜŞÜRÜLMEZ.
	s = &dboSahte{hata: fmt.Errorf("sarma: %w", hesaplar.ErrDBKimlikMevcut)}
	ad, geriAl, err := uygulamaDBKur(ctx, panel, KurArgs{Domain: dom, Tarif: tarif, DBAdi: "c_" + on + "_joom"}, s.mysqlOlustur, s.mysqlDrop)
	if err == nil {
		t.Fatalf("oluşturma hatası yutuldu (ad=%q)", ad)
	}
	if geriAl != nil {
		geriAl() // sözleşme ihlali: çağıran bunu defer'de koşardı
	}
	if len(s.dusur) != 0 {
		t.Errorf("başarısız oluşturmada var olan kimlik düşürüldü: %v", s.dusur)
	}

	// (5) Meşru: otomatik ad → oluşur; geri alma YALNIZ bu adı düşürür.
	s = &dboSahte{}
	ad, geriAl, err = uygulamaDBKur(ctx, panel, KurArgs{Domain: dom, Tarif: tarif}, s.mysqlOlustur, s.mysqlDrop)
	if err != nil {
		t.Fatalf("meşru kurulum düştü: %v", err)
	}
	beklenen := fmt.Sprintf("c_%s_joomla_1", on)
	if ad != beklenen || len(s.olustur) != 1 || s.olustur[0] != beklenen+"/"+beklenen {
		t.Fatalf("otomatik ad/oluşturma yanlış: ad=%q olustur=%v", ad, s.olustur)
	}
	if geriAl == nil {
		t.Fatalf("başarılı oluşturmada geri alma yok")
	}
	geriAl()
	if len(s.dusur) != 1 || s.dusur[0] != beklenen+"/"+beklenen {
		t.Errorf("geri alma yanlış adı düşürdü: %v", s.dusur)
	}

	// (6) Uzun önekli kiracı kendi ad-alanında engellenmez.
	s = &dboSahte{}
	uzun := Domain{ID: 2, SK: "c_" + on + "_x", AlanAdi: on + "-x.test"}
	if _, _, err := uygulamaDBKur(ctx, panel, KurArgs{Domain: uzun, Tarif: tarif, DBAdi: "c_" + on + "_x_joom"}, s.mysqlOlustur, s.mysqlDrop); err != nil {
		t.Errorf("uzun önekli kiracı kendi DB adını kullanamadı: %v", err)
	}
}
