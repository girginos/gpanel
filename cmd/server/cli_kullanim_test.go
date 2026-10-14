package main

import (
	"bytes"
	"strings"
	"testing"
)

// Yönetici CLI'larının kullanım metinleri İngilizce ve çıkış-kodu satırı belgeli kalmalı
// (cli-envanter.json bu metinleri kaynak alır). Alt komut/bayrak adları makine belirteci →
// Türkçe kalır; bu test onların da yerinde olduğunu doğrular.
func TestCLIKullanimMetinleri(t *testing.T) {
	cases := []struct {
		ad, metin string
		icerir    []string
	}{
		{"kapi", kapiKullanim, []string{"Usage: girginospanel-server kapi <command>", "gecis [days]", "durum [--json]", "eski-kapat --zorla", "Exit: 0 ok · 1 error · 2 usage · 3 gate mode not configured"}},
		{"sir", sirKullanim, []string{"Usage: girginospanel-server sir <command>", "--hepsi", "--yeniden-baslatma-yok", "--saglik-sn N", "Exit: 0 ok · 1 failed (old secrets still in place) · 2 usage · 3 CRITICAL (rollback incomplete)"}},
		{"yedek-coz", yedekCozKullanim, []string{"Usage:", "--anahtar", "--cikti", "--indir", "Exit: 0 ok · 1 failed · 2 usage"}},
	}
	for _, c := range cases {
		for _, s := range c.icerir {
			if !strings.Contains(c.metin, s) {
				t.Errorf("%s kullanım metninde %q yok", c.ad, s)
			}
		}
	}
	// Argümansız çağrı: kullanım stderr'e, çıkış 2.
	var out, errw bytes.Buffer
	if kod := yedekCozCLI(nil, &out, &errw); kod != 2 || !strings.HasPrefix(errw.String(), "Usage:") {
		t.Errorf("yedek-coz argümansız: kod=%d çıktı=%q", kod, errw.String())
	}
}
