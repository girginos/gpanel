package main

// rota_wp_ek_alan_test.go — WordPress Toolkit ek alan adı + hesap geneli uçları: (1) HER rota
// MusteriScope arkasında ({eid} handler'da hesaba bağlanır); (2) ek alan WordPress uçları alt
// alan uçlarının birebir simetriği (aynı işleyici kümesi); (3) API anahtarına KAPALI.

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"girginospanel/internal/apianahtar"
)

func wpRotaSatirlari(t *testing.T, kalip string) []string {
	t.Helper()
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, s := range strings.Split(string(b), "\n") {
		if strings.Contains(s, kalip) {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

func TestWPEkAlanRotalari_MusteriScope_ve_AltAlanSimetrisi(t *testing.T) {
	ek := wpRotaSatirlari(t, `"/domains/{id}/ek-alan-adlari/{eid}/wordpress`)
	alt := wpRotaSatirlari(t, `"/domains/{id}/subdomain/{sid}/wordpress`)
	if len(ek) != len(alt) || len(ek) != 13 {
		t.Fatalf("ek alan WordPress rotası (%d) alt alan ile (%d) simetrik ve 13 olmalı", len(ek), len(alt))
	}
	re := regexp.MustCompile(`^r\.With\(middleware\.MusteriScope\)\.(Get|Post|Delete)\("/domains/\{id\}/ek-alan-adlari/\{eid\}/wordpress[^"]*", wpH\.(\w+)\)$`)
	imza := func(satirlar []string, r *regexp.Regexp) map[string]bool {
		m := map[string]bool{}
		for _, s := range satirlar {
			x := r.FindStringSubmatch(s)
			if x == nil {
				t.Errorf("MusteriScope arkasında değil / biçim dışı: %s", s)
				continue
			}
			m[x[1]+" "+x[2]] = true
		}
		return m
	}
	a := imza(ek, re)
	b := imza(alt, regexp.MustCompile(`^r\.With\(middleware\.MusteriScope\)\.(Get|Post|Delete)\("/domains/\{id\}/subdomain/\{sid\}/wordpress[^"]*", wpH\.(\w+)\)$`))
	for k := range b {
		if !a[k] {
			t.Errorf("alt alanda olup ek alanda olmayan işleyici: %s", k)
		}
	}
	for _, s := range wpRotaSatirlari(t, `/wordpress/hedefler`) {
		if !strings.Contains(s, "middleware.MusteriScope") {
			t.Errorf("hedefler MusteriScope değil: %s", s)
		}
	}
	for _, s := range wpRotaSatirlari(t, `/wordpress/hesap`) {
		if !strings.Contains(s, "middleware.MusteriScope") {
			t.Errorf("hesap MusteriScope değil: %s", s)
		}
	}
}

func TestWPEkAlanRotalari_APIAnahtarinaKapali(t *testing.T) {
	for k := range apianahtar.Rotalar {
		if strings.Contains(k, "wordpress") {
			t.Errorf("WordPress ucu API anahtarına AÇILMAMALI (varsayılan kapalı): %s", k)
		}
	}
	var var_ bool
	for k := range apianahtar.KimlikUclari {
		if strings.Contains(k, "ek-alan-adlari/{eid}/wordpress/kullanici-parola") {
			var_ = true
		}
	}
	if !var_ {
		t.Error("ek alan kullanici-parola ucu KimlikUclari'nda sınıflandırılmalı")
	}
}
