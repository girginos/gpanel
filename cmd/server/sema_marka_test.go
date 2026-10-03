package main

// sema_marka_test.go — 0122/0123 (D2 whitelabel): semaDogrula bayi markası
// kolonlarını adıyla denetler; göçler temiz şemada hatasız ve İKİ KEZ koşar.

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestSemaDogrula_Marka0122_0123(t *testing.T) {
	db := semaTestDB(t)
	var gunluk bytes.Buffer
	eski := log.Writer()
	log.SetOutput(&gunluk)
	t.Cleanup(func() { log.SetOutput(eski) })

	kolonlar := []string{"cp_marka.alanadi_izni", "cp_marka_alanadi.dogrulama"}
	semaDogrula(db)
	for _, k := range kolonlar {
		if !strings.Contains(gunluk.String(), k) {
			t.Errorf("boş şemada %s eksik diye raporlanmadı: %q", k, gunluk.String())
		}
	}
	for _, q := range []string{
		`CREATE TABLE users (id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, username VARCHAR(64) NOT NULL) ENGINE=InnoDB`,
		`CREATE TABLE cp_ayarlar (anahtar VARCHAR(64) NOT NULL PRIMARY KEY, deger TEXT NOT NULL) ENGINE=InnoDB`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		semaTestGoc(t, db, "0122_bayi_marka.sql")
		semaTestGoc(t, db, "0123_bayi_marka_alanadi.sql")
	}
	gunluk.Reset()
	semaDogrula(db)
	for _, k := range kolonlar {
		if strings.Contains(gunluk.String(), k) {
			t.Errorf("göç sonrası %s hâlâ eksik görünüyor: %q", k, gunluk.String())
		}
	}
	var d string
	if err := db.QueryRow(`SELECT deger FROM cp_ayarlar WHERE anahtar='marka_bayi_yedek'`).Scan(&d); err != nil || d != "kok" {
		t.Errorf("marka_bayi_yedek varsayılanı 'kok' olmalı: %q %v", d, err)
	}
}
