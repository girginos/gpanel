package main

// rota_ek_alan_test.go — ek (addon) alan adı uçları: (1) HER rota MusteriScope arkasında
// (hesap sahipliği {id} için doğrulanır; {eid} handler'da hesaba bağlanır), AdminOnly/
// rolsüz kayıt yok; (2) ad-alanı (takeover) denetleyicisi BAĞLI — bağlı değilse
// subdomain.EkOlustur isteği reddeder (fail-closed) ve özellik çalışmaz; (3) API anahtarı
// izin tablosunda YOK (varsayılan reddet: anahtarla yeni alan adı açılamaz).

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"girginospanel/internal/apianahtar"
	"girginospanel/internal/subdomain"
)

func TestEkAlanRotalari_MusteriScopeVeBagli(t *testing.T) {
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	var satirlar []string
	for _, s := range strings.Split(string(b), "\n") {
		if strings.Contains(s, `"/domains/{id}/ek-alan-adlari`) {
			satirlar = append(satirlar, s)
		}
	}
	if len(satirlar) != 8 {
		t.Fatalf("8 ek alan adı rotası bekleniyordu, bulunan %d", len(satirlar))
	}
	re := regexp.MustCompile(`r\.With\(middleware\.MusteriScope\)\.(Get|Post|Delete)\("/domains/\{id\}/ek-alan-adlari`)
	for _, s := range satirlar {
		if !re.MatchString(strings.TrimSpace(s)) {
			t.Errorf("ek alan adı rotası MusteriScope arkasında değil: %s", strings.TrimSpace(s))
		}
	}
	if subdomain.AdAlaniEkle == nil {
		t.Fatal("subdomain.AdAlaniEkle bağlı değil (cmd/server/ek_alan_kablo.go) — takeover denetimi çalışmaz")
	}
}

func TestEkAlanRotalari_APIAnahtarinaKapali(t *testing.T) {
	for k := range apianahtar.Rotalar {
		if strings.Contains(k, "ek-alan-adlari") {
			t.Errorf("ek alan adı ucu API anahtarına AÇILMAMALI (varsayılan kapalı): %s", k)
		}
	}
}
