package avmotor

import (
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// ionCube ile kodlanmış bir dosyayı taklit eder: düz metin önsöz + şifreli
// (rastgele) gövde. gomulu varsa gövdeye AYNEN eklenir (imza çakışması testi).
func sahteIoncube(t *testing.T, gomulu string, kuyruk string) string {
	t.Helper()
	onsoz := "<?php //ICB0 82:0 83:e7bc                    ?><?php //00cba\n" +
		"// WHMCS - The Complete Client Management, Billing & Support Solution\n" +
		"if(!extension_loaded('ionCube Loader')){ echo 'the ionCube Loader for PHP needs to be installed.'; }\n"
	rng := rand.New(rand.NewSource(42))
	govde := make([]byte, 60000)
	for i := range govde {
		govde[i] = byte(rng.Intn(256))
	}
	// gövdenin ikili başlangıcını garantile (ardışık ikili bayt)
	for i := 0; i < 64; i++ {
		govde[i] = byte(200 + i%40)
	}
	icerik := onsoz + string(govde)
	if gomulu != "" {
		icerik += gomulu // şifreli gövde içinde rastgele geçen kısa dizi
	}
	icerik += kuyruk
	yol := filepath.Join(t.TempDir(), "init.php")
	if err := os.WriteFile(yol, []byte(icerik), 0o644); err != nil {
		t.Fatal(err)
	}
	return yol
}

// FP: şifreli gövdede rastgele geçen imza dizisi bulgu ÜRETMEMELİ.
// (Gerçek olay: WHMCS blob'unda "c99" 2 kez geçti → 347 dosya karantina.)
func TestKodlayiciSifreliGovdedeImzaCakismasiFPUretmez(t *testing.T) {
	m, _ := Yeni(TabanSet(), 0)
	yol := sahteIoncube(t, "c99shell r57 safe_mode bypass", "")
	b, tespit := m.TaraDosya(yol, "", nil)
	if tespit {
		t.Fatalf("YANLIŞ POZİTİF: kodlanmış dosya zararlı sayıldı (puan=%d kurallar=%v)", b.Puan, b.Kurallar)
	}
}

// NEGATİF KONTROL 1: kodlayıcı başlığı TAKLİT edilip gövdeye gerçek webshell
// eklenirse YİNE yakalanmalı (kaçış koruması).
func TestKodlayiciKacisiEngellenir(t *testing.T) {
	m, _ := Yeni(TabanSet(), 0)
	webshell := "\n<?php if(isset($_REQUEST['cmd'])){ @eval(base64_decode($_POST['x'])); " +
		"@system($_GET['cmd']); @error_reporting(0); @passthru($_REQUEST['c']); } // uzun duz metin kod bloku\n"
	yol := sahteIoncube(t, "", webshell)
	b, tespit := m.TaraDosya(yol, "", nil)
	if !tespit {
		t.Fatalf("KAÇIŞ: kodlayıcı başlığı arkasına gizlenen webshell yakalanmadı (puan=%d)", b.Puan)
	}
}

// NEGATİF KONTROL 2: kodlayıcı damgası olan ama ŞİFRELİ GÖVDESİ OLMAYAN dosya
// (damgayı taklit eden düz webshell) tam taranmalı.
func TestKodlayiciDamgasiTaklitDuzWebshellYakalanir(t *testing.T) {
	m, _ := Yeni(TabanSet(), 0)
	icerik := "<?php //ICB0 82:0 83:aaaa\n" +
		"@eval(base64_decode($_POST['x'])); @system($_GET['cmd']); @error_reporting(0);\n"
	yol := filepath.Join(t.TempDir(), "sahte.php")
	if err := os.WriteFile(yol, []byte(icerik), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, tespit := m.TaraDosya(yol, "", nil); !tespit {
		t.Fatal("KAÇIŞ: sahte kodlayıcı damgalı düz webshell yakalanmadı")
	}
}

// NEGATİF KONTROL 3: kodlayıcı damgası OLMAYAN normal webshell hâlâ yakalanır
// (değişiklik genel tespiti zayıflatmamalı).
func TestKodlayiciDegisikligiNormalTespitiBozmaz(t *testing.T) {
	m, _ := Yeni(TabanSet(), 0)
	icerik := "<?php @eval(base64_decode($_POST['x'])); @system($_GET['cmd']); @error_reporting(0);\n"
	yol := filepath.Join(t.TempDir(), "shell.php")
	if err := os.WriteFile(yol, []byte(icerik), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, tespit := m.TaraDosya(yol, "", nil); !tespit {
		t.Fatal("REGRESYON: normal webshell yakalanmadı")
	}
}
