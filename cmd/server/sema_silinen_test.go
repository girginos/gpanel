package main

// sema_silinen_test.go — semaDogrula 0217'yi (silinmiş hesap yedekleri) denetler: kolonlar + YAPI (tür, indeks, FK).
// Göç düşer ya da defter dosyayı "uygulandı" sayıp eski revizyon kalırsa açılışta 🔴 satırı adıyla söyler; yeni dosya
// koşunca susar. Gerçek MariaDB gerekir (GOSP_TEST_GECICI_DSN yoksa ATLANIR).

import (
	"bytes"
	"database/sql"
	"go/ast"
	"log"
	"strings"
	"testing"
)

// 0217'nin ikinci (e449f18) revizyonu: eski_domain_id var, iz_id yok, TIMESTAMP, anlık görüntüde eski_domain_id UNIQUE.
var sema0217V2 = []string{
	`ALTER TABLE backups DROP FOREIGN KEY IF EXISTS fk_backup_domain`,
	`ALTER TABLE backups
	  ADD COLUMN IF NOT EXISTS alan_adi VARCHAR(253) NOT NULL DEFAULT '',
	  ADD COLUMN IF NOT EXISTS sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '',
	  ADD COLUMN IF NOT EXISTS reseller_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
	  ADD COLUMN IF NOT EXISTS silinme TIMESTAMP NULL DEFAULT NULL,
	  ADD COLUMN IF NOT EXISTS son_kullanma TIMESTAMP NULL DEFAULT NULL,
	  ADD COLUMN IF NOT EXISTS eski_domain_id BIGINT UNSIGNED NOT NULL DEFAULT 0`,
	`ALTER TABLE backups ADD KEY IF NOT EXISTS ix_backups_silinen (sistem_kullanici, silinme)`,
	`CREATE TABLE IF NOT EXISTS backup_yetim_hedefler (
	  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT, eski_domain_id BIGINT UNSIGNED NOT NULL,
	  tip VARCHAR(8) NOT NULL DEFAULT 'sftp', host VARCHAR(253) NOT NULL, port INT NOT NULL DEFAULT 22,
	  kullanici VARCHAR(128) NOT NULL, parola VARCHAR(255) NOT NULL, uzak_dizin VARCHAR(255) NOT NULL DEFAULT '/',
	  olusturma TIMESTAMP NOT NULL DEFAULT current_timestamp(),
	  PRIMARY KEY (id), UNIQUE KEY uk_yetim_hedef_domain (eski_domain_id)) ENGINE=InnoDB`,
}

func semaYapiGunlugu(t *testing.T, db *sql.DB) string {
	t.Helper()
	var gunluk bytes.Buffer
	eski := log.Writer()
	log.SetOutput(&gunluk)
	defer log.SetOutput(eski)
	semaDogrula(db)
	return gunluk.String()
}

func TestSemaDogrula_Silinen0217_KolonYapiVeFK(t *testing.T) {
	db := semaTestDB(t)
	exec := func(q string) {
		t.Helper()
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}
	kolonlar := []string{"backups.alan_adi", "backups.sistem_kullanici", "backups.reseller_id", "backups.silinme", "backups.son_kullanma", "backups.eski_domain_id", "backups.iz_id", "backup_yetim_hedefler.iz_id"}

	// 1) boş şema: tüm 0217 kolonları adıyla raporlanır
	g := semaYapiGunlugu(t, db)
	for _, k := range kolonlar {
		if !strings.Contains(g, k) {
			t.Errorf("boş şemada %s eksik diye raporlanmadı — semaDogrula onu denetlemiyor: %q", k, g)
		}
	}
	if strings.Contains(g, "SEMA YAPISI BOZUK") {
		t.Errorf("tablolar hiç yokken yapı BOZUK denmemeli (kolon eksikliği zaten bildirilir): %q", g)
	}

	// 2) 0217 ÖNCESİ şema: FK'li backups → kolonlar eksik VE FK hâlâ var
	exec(`CREATE TABLE domains (id BIGINT UNSIGNED NOT NULL PRIMARY KEY) ENGINE=InnoDB`)
	exec(`CREATE TABLE backups (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL, dosya VARCHAR(255) NOT NULL,
		KEY ix_backup_domain (domain_id), CONSTRAINT fk_backup_domain FOREIGN KEY (domain_id) REFERENCES domains(id) ON DELETE CASCADE) ENGINE=InnoDB`)
	g = semaYapiGunlugu(t, db)
	if !strings.Contains(g, "fk_backup_domain") || !strings.Contains(g, "SEMA YAPISI BOZUK") {
		t.Errorf("FK hâlâ varken uyarı beklenirdi: %q", g)
	}

	// 3) ESKİ REVİZYON (V2) uygulanmış: FK kalktı ama TIMESTAMP, iz_id yok, eski UNIQUE var → hepsi adıyla raporlanır
	for _, q := range sema0217V2 {
		exec(q)
	}
	g = semaYapiGunlugu(t, db)
	for _, bek := range []string{"backups.iz_id", "backup_yetim_hedefler.iz_id", "backups.silinme turu timestamp", "backups.son_kullanma turu timestamp",
		"backup_yetim_hedefler.olusturma turu timestamp", "uk_yetim_hedef_domain", "SEMA YAPISI BOZUK", "schema_migrations"} {
		if !strings.Contains(g, bek) {
			t.Errorf("eski revizyonda %q raporlanmalı: %q", bek, g)
		}
	}
	if strings.Contains(g, "fk_backup_domain FK'si") {
		t.Errorf("FK kalktı, artık raporlanmamalı: %q", g)
	}

	// 4) yeni dosya (iki kez, idempotent): tüm 0217 uyarıları susar
	semaTestGoc(t, db, "0217_silinen_yedek.sql")
	semaTestGoc(t, db, "0217_silinen_yedek.sql")
	g = semaYapiGunlugu(t, db)
	for _, k := range kolonlar {
		if strings.Contains(g, k) {
			t.Errorf("0217 sonrası %s hâlâ eksik görünüyor: %q", k, g)
		}
	}
	if strings.Contains(g, "SEMA YAPISI BOZUK") || strings.Contains(g, "0217") {
		t.Errorf("0217 sonrası yapı uyarısı kalmamalı: %q", g)
	}
}

// Dar kapsamlı birim kontrol: her yapısal bozulma TEK BAŞINA (diğerleri sağlamken) kendi adıyla yakalanır; onarılınca
// susar. Toplu bozup birkaç alt dizeye bakmak, bir kontrolü sessizce kaldıran değişikliği başka bir uyarının arkasına
// saklayabilirdi.
func TestSemaYapiDogrula_TemizVeBozukDurum(t *testing.T) {
	db := semaTestDB(t)
	exec := func(q string) {
		t.Helper()
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}
	exec(`CREATE TABLE domains (id BIGINT UNSIGNED NOT NULL PRIMARY KEY) ENGINE=InnoDB`)
	exec(`CREATE TABLE backups (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL, dosya VARCHAR(255) NOT NULL,
		KEY ix_backup_domain (domain_id)) ENGINE=InnoDB`)
	semaTestGoc(t, db, "0217_silinen_yedek.sql")
	if s := semaYapiDogrula(db); len(s) != 0 {
		t.Fatalf("0217 sonrası temiz olmalı: %v", s)
	}

	for _, c := range []struct{ ad, boz, onar, bek string }{
		{"iz-indeksi", `ALTER TABLE backups DROP KEY ix_backups_iz`,
			`ALTER TABLE backups ADD KEY ix_backups_iz (iz_id)`, "backups.ix_backups_iz indeksi yok"},
		{"silinen-indeksi", `ALTER TABLE backups DROP KEY ix_backups_silinen`,
			`ALTER TABLE backups ADD KEY ix_backups_silinen (sistem_kullanici, silinme)`, "backups.ix_backups_silinen indeksi yok"},
		{"hedef-unique-yok", `ALTER TABLE backup_yetim_hedefler DROP KEY uk_yetim_hedef_iz`,
			`ALTER TABLE backup_yetim_hedefler ADD UNIQUE KEY uk_yetim_hedef_iz (iz_id)`, "uk_yetim_hedef_iz UNIQUE indeksi yok"},
		// Aynı adlı AMA benzersiz olmayan indeks: "indeks var" demek yetmez, UNIQUE olmalı.
		{"hedef-unique-degil", `ALTER TABLE backup_yetim_hedefler DROP KEY uk_yetim_hedef_iz, ADD KEY uk_yetim_hedef_iz (iz_id)`,
			`ALTER TABLE backup_yetim_hedefler DROP KEY uk_yetim_hedef_iz, ADD UNIQUE KEY uk_yetim_hedef_iz (iz_id)`, "uk_yetim_hedef_iz UNIQUE indeksi yok"},
		{"silinme-timestamp", `ALTER TABLE backups MODIFY COLUMN silinme TIMESTAMP NULL DEFAULT NULL`,
			`ALTER TABLE backups MODIFY COLUMN silinme DATETIME NULL DEFAULT NULL`, "backups.silinme turu timestamp"},
		{"son-kullanma-timestamp", `ALTER TABLE backups MODIFY COLUMN son_kullanma TIMESTAMP NULL DEFAULT NULL`,
			`ALTER TABLE backups MODIFY COLUMN son_kullanma DATETIME NULL DEFAULT NULL`, "backups.son_kullanma turu timestamp"},
		{"olusturma-timestamp", `ALTER TABLE backup_yetim_hedefler MODIFY COLUMN olusturma TIMESTAMP NOT NULL DEFAULT current_timestamp()`,
			`ALTER TABLE backup_yetim_hedefler MODIFY COLUMN olusturma DATETIME NOT NULL DEFAULT current_timestamp()`, "backup_yetim_hedefler.olusturma turu timestamp"},
		{"eski-domain-unique", `ALTER TABLE backup_yetim_hedefler ADD UNIQUE KEY uk_yetim_hedef_domain (eski_domain_id)`,
			`ALTER TABLE backup_yetim_hedefler DROP KEY uk_yetim_hedef_domain`, "uk_yetim_hedef_domain"},
		{"fk-geri-geldi", `ALTER TABLE backups ADD CONSTRAINT fk_backup_domain FOREIGN KEY (domain_id) REFERENCES domains(id) ON DELETE CASCADE`,
			`ALTER TABLE backups DROP FOREIGN KEY fk_backup_domain`, "backups.fk_backup_domain FK'si hala var"},
	} {
		exec(c.boz)
		s := semaYapiDogrula(db)
		if len(s) != 1 || !strings.Contains(s[0], c.bek) {
			t.Errorf("%s: tam olarak 1 sorun ve %q beklenirdi, gelen: %v", c.ad, c.bek, s)
		}
		exec(c.onar)
		if s := semaYapiDogrula(db); len(s) != 0 {
			t.Errorf("%s: onarım sonrası temiz olmalı: %v", c.ad, s)
		}
	}
}

// semaRaporla: kolon eksiği ve yapı bozukluğu 🔴 satırı yazar; "tamamı mevcut" BAŞARI mesajı yalnız ikisi de
// boşken basılır — yapı bozukken (kolonlar tam olsa da) "sorun yok" demek operatörü yanıltırdı.
func semaRaporlaGunlugu(eksik, bozuk []string) string {
	var gunluk bytes.Buffer
	eski := log.Writer()
	log.SetOutput(&gunluk)
	defer log.SetOutput(eski)
	semaRaporla(eksik, bozuk, 7)
	return gunluk.String()
}

func TestSemaRaporla_BasariMesajiYalnizTemizkenBasilir(t *testing.T) {
	const basari = "kritik kolonlarin tamami mevcut"
	g := semaRaporlaGunlugu(nil, nil)
	if !strings.Contains(g, basari) || !strings.Contains(g, "(7 kontrol)") {
		t.Errorf("temiz durumda başarı mesajı ve kontrol sayısı beklenirdi: %q", g)
	}
	if strings.Contains(g, "🔴") {
		t.Errorf("temiz durumda 🔴 satırı olmamalı: %q", g)
	}

	// yalnız yapı bozuk (kolonlar tam): 🔴 + düzeltme komutu, BAŞARI YOK
	g = semaRaporlaGunlugu(nil, []string{"backups.ix_backups_silinen indeksi yok"})
	for _, bek := range []string{"🔴 SEMA YAPISI BOZUK", "backups.ix_backups_silinen indeksi yok", "DELETE FROM schema_migrations WHERE dosya='0217_silinen_yedek.sql'"} {
		if !strings.Contains(g, bek) {
			t.Errorf("yalnız yapı bozukken %q beklenirdi: %q", bek, g)
		}
	}
	if strings.Contains(g, basari) || strings.Contains(g, "SEMA EKSIK") {
		t.Errorf("yapı bozukken başarı mesajı basılmamalı, kolon eksik de denmemeli: %q", g)
	}

	// yalnız kolon eksik: 🔴, BAŞARI YOK, yapı uyarısı YOK
	g = semaRaporlaGunlugu([]string{"backups.iz_id"}, nil)
	if !strings.Contains(g, "🔴 SEMA EKSIK") || !strings.Contains(g, "backups.iz_id") {
		t.Errorf("kolon eksikken SEMA EKSIK beklenirdi: %q", g)
	}
	if strings.Contains(g, basari) || strings.Contains(g, "SEMA YAPISI BOZUK") {
		t.Errorf("kolon eksikken başarı mesajı ve yapı uyarısı olmamalı: %q", g)
	}

	// ikisi birden: ikisi de söylenir, başarı yok
	g = semaRaporlaGunlugu([]string{"backups.iz_id"}, []string{"backups.fk_backup_domain FK'si hala var"})
	if !strings.Contains(g, "SEMA EKSIK") || !strings.Contains(g, "SEMA YAPISI BOZUK") || strings.Contains(g, basari) {
		t.Errorf("ikisi birden raporlanmalı, başarı basılmamalı: %q", g)
	}
}

// 🔴 backups.LftpArtiklariniTemizle: çöken süreçten kalan lftp betikleri (düz parola içerir, /tmp/gosp-lftp-*.cmd)
// açılışta silinir. Bu çağrı main'den kaybolursa (ör. elle port edilen bir hotfix'te) hiçbir paket testi görmez ve
// parolalar /tmp'de sonsuza dek kalır. Çağrı main'in ÜST DÜZEYİNDE ve koşulsuz olmalı.
func lftpTemizligiBagli(govde []ast.Stmt) bool {
	for _, s := range govde {
		es, ok := s.(*ast.ExprStmt)
		if !ok {
			continue
		}
		c, ok := es.X.(*ast.CallExpr)
		if !ok || len(c.Args) != 0 {
			continue
		}
		if p, ok := secici(c.Fun, "LftpArtiklariniTemizle"); ok && p == "backups" {
			return true
		}
	}
	return false
}

func TestMain_LftpArtiklariAcilistaTemizlenir(t *testing.T) {
	if !lftpTemizligiBagli(mainGovdesi(t, nil)) {
		t.Fatal("main'de üst düzey, koşulsuz backups.LftpArtiklariniTemizle() çağrısı yok: çöken süreçten kalan düz parolalı lftp betikleri /tmp'de kalır")
	}
}

func TestMain_LftpArtiklariKurali_Ornekler(t *testing.T) {
	for _, d := range []struct {
		ad, govde string
		gecer     bool
	}{
		{"dogru", "backups.LftpArtiklariniTemizle()", true},
		{"yorum", "// backups.LftpArtiklariniTemizle()\nbasla()", false},
		{"kosullu", "if x { backups.LftpArtiklariniTemizle() }", false},
		{"baska-paket", "diger.LftpArtiklariniTemizle()", false},
		{"yok", "backups.StartScheduler(d)", false},
	} {
		govde := mainGovdesi(t, "package main\nfunc main() {\n"+d.govde+"\n}\n")
		if got := lftpTemizligiBagli(govde); got != d.gecer {
			t.Errorf("%s: geçer=%v bekleniyordu, geldi %v", d.ad, d.gecer, got)
		}
	}
}
