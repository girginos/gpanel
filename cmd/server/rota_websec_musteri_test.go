package main

// rota_websec_musteri_test.go — hosting MÜŞTERİSİ güvenlik uçları (internal/websec/musteri.go):
// rota kaydı kilidi + API anahtarına KAPALI kanıtı. Yönlendirici main() içinde kurulduğu
// için main.go kaynak satırları okunur (rota_websec_test.go deseni).

import (
	"os"
	"strings"
	"testing"

	"girginospanel/internal/apianahtar"
)

var websecMusteriUclari = []struct{ yontem, yol, handler string }{
	{"Get", "/domains/{id}/guvenlik", "websecH.MusteriListe"},
	{"Get", "/domains/{id}/guvenlik/ozet", "websecH.MusteriOzet"},
	{"Get", "/domains/{id}/guvenlik/bulgular", "websecH.MusteriBulgular"},
	{"Post", "/domains/{id}/guvenlik/tara", "websecH.MusteriTara"},
}

// Her uç tek kez, YALNIZ websec.MusteriKapi (yabancı {id} → 404) + middleware.MusteriScope
// (askı + müşteri/bayi/kök kapısı) ile bağlanır; AdminOnly/AdminVeyaReseller ile değil.
func TestWebsecMusteriRotalari_KapiliVeKayitli(t *testing.T) {
	kaynak, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	satirlar := strings.Split(string(kaynak), "\n")
	for _, u := range websecMusteriUclari {
		var kayitlar []string
		for _, satir := range satirlar {
			if strings.Contains(satir, `"`+u.yol+`"`) {
				kayitlar = append(kayitlar, strings.TrimSpace(satir))
			}
		}
		if len(kayitlar) != 1 {
			t.Fatalf("%s: tek kayıt beklenir, %d bulundu: %q", u.yol, len(kayitlar), kayitlar)
		}
		beklenen := `r.With(websec.MusteriKapi, middleware.MusteriScope).` + u.yontem + `("` + u.yol + `", ` + u.handler + `)`
		if kayitlar[0] != beklenen {
			t.Errorf("%s kapısı yanlış:\n  var:  %s\n  olmalı: %s", u.yol, kayitlar[0], beklenen)
		}
	}
	n := 0
	for _, satir := range satirlar {
		if strings.Contains(satir, `/guvenlik`) && strings.Contains(satir, `"/domains/{id}/`) {
			n++
		}
	}
	if n != len(websecMusteriUclari) {
		t.Fatalf("müşteri güvenlik rota sayısı %d, beklenen %d — yeni uç kapsam denetiminden geçmeli", n, len(websecMusteriUclari))
	}
}

// API anahtarı bu uçlara GİREMEZ: izin tablosunda yok (varsayılan reddet) ve her izin
// verilse bile Karar "uç kapalı" der.
func TestWebsecMusteriUclari_APIAnahtarinaKapali(t *testing.T) {
	env := rotaEnvanteri(t)
	var hepsi []string
	for _, g := range apianahtar.Gruplar {
		hepsi = append(hepsi, g.Ad+":yaz")
	}
	for _, u := range websecMusteriUclari {
		d := strings.ToUpper(u.yontem) + " /api/v1" + u.yol
		if !env[d] {
			t.Errorf("rota envanterde yok: %s", d)
		}
		if _, ok := apianahtar.Rotalar[d]; ok {
			t.Errorf("müşteri güvenlik ucu API anahtarına AÇIK: %s", d)
		}
		if apianahtar.HerZaman[d] {
			t.Errorf("müşteri güvenlik ucu HerZaman listesinde: %s", d)
		}
		for _, rol := range []string{apianahtar.RolKok, apianahtar.RolBayi} {
			if apianahtar.Karar(d, rol, hepsi) != apianahtar.KararUcKapali {
				t.Errorf("%s rolü için uç kapalı değil: %s", rol, d)
			}
		}
	}
}
