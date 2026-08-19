package avmotor

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"math/rand"
	"regexp"
	"strings"
	"testing"
)

const testPayload = `system($_POST['c']); shell_exec($_GET['x']);`

func testKuralSeti(t *testing.T) []Kural {
	t.Helper()
	k := Kural{ID: "TEST-SYS-POST", Ad: "system+POST", Desen: `system\s*\(\s*\$_POST`, Puan: 100, Uzanti: []string{"php"}}
	re, err := regexp.Compile(k.Desen)
	if err != nil {
		t.Fatal(err)
	}
	k.re = re
	return []Kural{k}
}

func idVar(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// Düz metinde eşleşmeyen ama base64 çözülünce eşleşen payload yakalanmalı.
func TestDeobfuskeBase64(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString([]byte(testPayload))
	content := []byte(`<?php eval(base64_decode('` + b64 + `')); ?>`)
	// Kontrol: düz metin kuralı eşleşMEMELİ (obfuscate).
	if testKuralSeti(t)[0].re.Match(content) {
		t.Fatal("düz metinde eşleşmemeliydi — test geçersiz")
	}
	puan, idler := deobfuskeTara(content, testKuralSeti(t), "php")
	if puan == 0 || !idVar(idler, "GOSP-DECODE:TEST-SYS-POST") {
		t.Fatalf("base64 payload yakalanmadı: puan=%d idler=%v", puan, idler)
	}
}

// gzinflate(base64_decode(...)) — PHP gzinflate = ham DEFLATE.
func TestDeobfuskeGzinflate(t *testing.T) {
	var buf bytes.Buffer
	w, _ := flate.NewWriter(&buf, flate.BestCompression)
	_, _ = w.Write([]byte(testPayload))
	_ = w.Close()
	b64 := base64.StdEncoding.EncodeToString(buf.Bytes())
	content := []byte(`<?php eval(gzinflate(base64_decode('` + b64 + `'))); ?>`)
	puan, idler := deobfuskeTara(content, testKuralSeti(t), "php")
	if puan == 0 || !idVar(idler, "GOSP-DECODE:TEST-SYS-POST") {
		t.Fatalf("gzinflate payload yakalanmadı: puan=%d idler=%v", puan, idler)
	}
}

// str_rot13 ile gizlenmiş payload.
func TestDeobfuskeRot13(t *testing.T) {
	// rot13("system($_POST['c']);") — çalışma zamanında üretelim.
	rot := string(rot13([]byte(testPayload)))
	content := []byte(`<?php eval(str_rot13('` + rot + `')); ?>`)
	puan, _ := deobfuskeTara(content, testKuralSeti(t), "php")
	if puan == 0 {
		t.Fatalf("rot13 payload yakalanmadı")
	}
}

// Temiz düz metin → decode katmanı puan ÜRETMEMELİ (negatif kontrol).
func TestDeobfuskeTemiz(t *testing.T) {
	content := []byte(`<?php $x = base64_encode("merhaba dunya"); echo $x; ?>`)
	puan, idler := deobfuskeTara(content, testKuralSeti(t), "php")
	if puan != 0 {
		t.Fatalf("temiz dosyada yanlış pozitif: puan=%d idler=%v", puan, idler)
	}
}

// Normal kod + gömülü yüksek-entropi blob → BLOB tespiti.
func TestBolgeselEntropiBlob(t *testing.T) {
	normal := strings.Repeat("function f($a){ return $a + 1; } // normal kod satiri duz\n", 80)
	var blob strings.Builder
	r := rand.New(rand.NewSource(42)) // sabit seed → deterministik, yüksek entropi
	const alf = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	for i := 0; i < 18000; i++ {
		blob.WriteByte(alf[r.Intn(64)])
	}
	content := []byte(normal + blob.String() + "\neval($x);\n")
	s, id, ok := bolgeselEntropi(content)
	if !ok || s == 0 || id != "GOSP-HEUR-ENTROPI-BLOB" {
		t.Fatalf("gömülü blob tespit edilmedi: skor=%d id=%s ok=%v", s, id, ok)
	}
}

// Tamamen normal kod → entropi tespiti YOK (negatif kontrol).
func TestBolgeselEntropiTemiz(t *testing.T) {
	content := []byte(strings.Repeat("$total = $price * $qty; echo $total;\n", 300))
	if _, _, ok := bolgeselEntropi(content); ok {
		t.Fatalf("normal kod yanlış pozitif entropi verdi")
	}
}
