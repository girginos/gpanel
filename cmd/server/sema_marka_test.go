package main

// sema_marka_test.go — 0122/0123 (D2 whitelabel): semaDogrula bayi markası
// kolonlarını adıyla denetler; göçler temiz şemada hatasız ve İKİ KEZ koşar.

import (
	"bytes"
	"girginospanel/internal/apianahtar"
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

	kolonlar := []string{"cp_marka.alanadi_izni", "cp_marka_alanadi.dogrulama", "cp_marka_alanadi.adim", "cp_marka_alanadi.yoklama_bitis"}
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
		semaTestGoc(t, db, "0170_marka_alanadi_uygula.sql")
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

// Uygula ucu API anahtarına KAPALI (varsayılan reddet): izin tablosunda yok, altın dosyada yok.
func TestAlanAdiUygula_AnahtarKapali(t *testing.T) {
	const rota = "POST /api/v1/marka/alan-adi/uygula"
	if !rotaEnvanteri(t)[rota] {
		t.Fatalf("rota envanterde yok: %s", rota)
	}
	if _, acik := apianahtar.Rotalar[rota]; acik {
		t.Errorf("%s API anahtarına AÇIK olmamalı", rota)
	}
	if apianahtar.HerZaman[rota] {
		t.Errorf("%s HerZaman listesinde olmamalı", rota)
	}
}
