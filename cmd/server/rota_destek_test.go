package main

// rota_destek_test.go — destek botu izin tablosunun (middleware.DestekRotalari)
// ROTA ENVANTERİYLE kilidi + altın dosya (rota_api_anahtar_test.go deseni).
//
// Kanıtlanan:
//   - tablodaki her girdi gerçekten var (yanlış yazılmış girdi sessizce "kapalı" kalırdı)
//   - tablo yalnız GET; kimlik uçları, API anahtarı yönetimi ve "kimlik benzeri" uçlar yok
//   - destek botuna AÇIK uç listesi altın dosyayla aynı (her değişiklik diff'te görünür)

import (
	"os"
	"sort"
	"strings"
	"testing"

	"girginospanel/internal/apianahtar"
	"girginospanel/internal/middleware"
)

func TestDestekRotalariEnvanterde(t *testing.T) {
	env := rotaEnvanteri(t)
	for d := range middleware.DestekRotalari {
		if !env[d] {
			t.Errorf("destek tablosundaki rota envanterde YOK (ölü/yanlış girdi): %s", d)
		}
		if !strings.HasPrefix(d, "GET ") {
			t.Errorf("destek tablosunda okuma dışı uç: %s", d)
		}
		if apianahtar.KimlikUclari[d] {
			t.Errorf("kimlik ucu destek tablosunda: %s", d)
		}
		if _, ok := apianahtar.KimlikBenzeriIzinli[d]; ok {
			t.Errorf("kimlik benzeri uç destek tablosunda: %s", d)
		}
		if kimlikRe.MatchString(d) {
			t.Errorf("adı kimlik/jeton çağrıştıran uç destek tablosunda: %s", d)
		}
	}
}

func TestDestekRotalariAltin(t *testing.T) {
	var satirlar []string
	for d, a := range middleware.DestekRotalari {
		satirlar = append(satirlar, d+"  "+a)
	}
	sort.Strings(satirlar)
	got := strings.Join(satirlar, "\n") + "\n"
	const altin = "testdata/destek_rotalari.golden"
	if os.Getenv("GOSP_ALTIN_GUNCELLE") == "1" {
		if err := os.WriteFile(altin, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(altin)
	if err != nil {
		t.Fatalf("altın dosya okunamadı (GOSP_ALTIN_GUNCELLE=1 ile üretin): %v", err)
	}
	if string(want) != got {
		t.Fatalf("destek botuna AÇIK uç listesi değişti. Bilinçliyse GOSP_ALTIN_GUNCELLE=1 ile altın dosyayı güncelleyin ve diff'i inceletin.\n--- altın\n%s\n--- şimdi\n%s", want, got)
	}
}
