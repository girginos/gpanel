package main

// sema_silinen_test.go — semaDogrula 0217'yi (silinmiş hesap yedekleri) denetler: kolonlar + YAPI (tür, indeks, FK).
// Göç düşer ya da defter dosyayı "uygulandı" sayıp eski revizyon kalırsa açılışta 🔴 satırı adıyla söyler; yeni dosya
// koşunca susar. Gerçek MariaDB gerekir (GOSP_TEST_GECICI_DSN yoksa ATLANIR).

import (
	"bytes"
	"database/sql"
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

// Dar kapsamlı birim kontrol: yalnız yapı sorunu (kolonlar tam) olduğunda da uyarı verir ve başarı mesajı basmaz.
func TestSemaYapiDogrula_TemizVeBozukDurum(t *testing.T) {
	db := semaTestDB(t)
	if _, err := db.Exec(`CREATE TABLE domains (id BIGINT UNSIGNED NOT NULL PRIMARY KEY) ENGINE=InnoDB`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE backups (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL, dosya VARCHAR(255) NOT NULL,
		KEY ix_backup_domain (domain_id)) ENGINE=InnoDB`); err != nil {
		t.Fatal(err)
	}
	semaTestGoc(t, db, "0217_silinen_yedek.sql")
	if s := semaYapiDogrula(db); len(s) != 0 {
		t.Fatalf("0217 sonrası temiz olmalı: %v", s)
	}
	// indeks düşerse / tür bozulursa yakalanır
	if _, err := db.Exec(`ALTER TABLE backups DROP KEY ix_backups_iz`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE backup_yetim_hedefler DROP KEY uk_yetim_hedef_iz`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE backups MODIFY COLUMN son_kullanma TIMESTAMP NULL DEFAULT NULL`); err != nil {
		t.Fatal(err)
	}
	s := strings.Join(semaYapiDogrula(db), " | ")
	for _, bek := range []string{"ix_backups_iz", "uk_yetim_hedef_iz", "son_kullanma turu timestamp"} {
		if !strings.Contains(s, bek) {
			t.Errorf("%q yakalanmalıydı: %s", bek, s)
		}
	}
}
