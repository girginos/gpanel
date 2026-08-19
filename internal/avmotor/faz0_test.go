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

func TestDeobfuskeBase64(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString([]byte(testPayload))
	content := []byte(`<?php eval(base64_decode('` + b64 + `')); ?>`)
	if testKuralSeti(t)[0].re.Match(content) {
		t.Fatal("duz metinde eslesmemeliydi")
	}
	puan, idler := deobfuskeTara(content, testKuralSeti(t), "php", nil)
	if puan == 0 || !idVar(idler, "GOSP-DECODE:TEST-SYS-POST") {
		t.Fatalf("base64 payload yakalanmadi: puan=%d idler=%v", puan, idler)
	}
}

func TestDeobfuskeGzinflate(t *testing.T) {
	var buf bytes.Buffer
	w, _ := flate.NewWriter(&buf, flate.BestCompression)
	_, _ = w.Write([]byte(testPayload))
	_ = w.Close()
	b64 := base64.StdEncoding.EncodeToString(buf.Bytes())
	content := []byte(`<?php eval(gzinflate(base64_decode('` + b64 + `'))); ?>`)
	puan, idler := deobfuskeTara(content, testKuralSeti(t), "php", nil)
	if puan == 0 || !idVar(idler, "GOSP-DECODE:TEST-SYS-POST") {
		t.Fatalf("gzinflate payload yakalanmadi: puan=%d idler=%v", puan, idler)
	}
}

func TestDeobfuskeHexKompres(t *testing.T) {
	var buf bytes.Buffer
	w, _ := flate.NewWriter(&buf, flate.BestCompression)
	_, _ = w.Write([]byte(testPayload))
	_ = w.Close()
	bsx := string([]byte{92, 'x'}) // "\x" oneki, kaynak-kacisi olmadan
	var hx strings.Builder
	const hexd = "0123456789abcdef"
	for _, b := range buf.Bytes() {
		hx.WriteString(bsx)
		hx.WriteByte(hexd[b>>4])
		hx.WriteByte(hexd[b&0xf])
	}
	content := []byte(`<?php eval(gzinflate(hex2bin('` + hx.String() + `'))); ?>`)
	puan, _ := deobfuskeTara(content, testKuralSeti(t), "php", nil)
	if puan == 0 {
		t.Fatalf("hex+gzinflate payload yakalanmadi (BYPASS-3 acik)")
	}
}

func TestDeobfuskeRot13(t *testing.T) {
	rot := string(rot13([]byte(testPayload)))
	content := []byte(`<?php eval(str_rot13('` + rot + `')); ?>`)
	_, idler := deobfuskeTara(content, testKuralSeti(t), "php", nil)
	if !idVar(idler, "GOSP-DECODE:TEST-SYS-POST") {
		t.Fatalf("rot13 payload dogru ID ile yakalanmadi: %v", idler)
	}
}

func TestDeobfuskeTemiz(t *testing.T) {
	content := []byte(`<?php $x = strtoupper("merhaba"); echo $x; ?>`)
	if puan, _ := deobfuskeTara(content, testKuralSeti(t), "php", nil); puan != 0 {
		t.Fatalf("temiz dosyada yanlis pozitif: puan=%d", puan)
	}
}

func TestDeobfuskeTemizGercekBlok(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString([]byte("merhaba dunya, bu tamamen zararsiz bir metindir."))
	content := []byte(`<?php $mesaj = base64_decode('` + b64 + `'); echo $mesaj; ?>`)
	puan, idler := deobfuskeTara(content, testKuralSeti(t), "php", nil)
	if puan != 0 {
		t.Fatalf("zararsiz decode edilen blokta yanlis pozitif: puan=%d idler=%v", puan, idler)
	}
}

func TestDeobfuskeCiftSayimYok(t *testing.T) {
	inner := base64.StdEncoding.EncodeToString([]byte(`system($_POST['b']);`))
	content := []byte(`<?php system($_POST['a']); $y = base64_decode('` + inner + `'); ?>`)
	disGoruldu := map[string]bool{"TEST-SYS-POST": true}
	puan, idler := deobfuskeTara(content, testKuralSeti(t), "php", disGoruldu)
	if puan != 0 || idVar(idler, "GOSP-DECODE:TEST-SYS-POST") {
		t.Fatalf("cift-sayim FP: puan=%d idler=%v", puan, idler)
	}
}

func TestAcKompresBombaOrani(t *testing.T) {
	var buf bytes.Buffer
	w, _ := flate.NewWriter(&buf, flate.BestCompression)
	_, _ = w.Write(bytes.Repeat([]byte("A"), 1000000))
	_ = w.Close()
	but := &cozButce{kalanBayt: cozMaxBayt, decompresK: cozMaxDecompres}
	if o := acKompres(buf.Bytes(), but); o != nil {
		t.Fatalf("oran bombasi nil donmeliydi, %d bayt dondu", len(o))
	}
	if but.decompresK != 0 {
		t.Fatalf("bomba tespitinde decompresK 0'lanmaliydi, %d", but.decompresK)
	}
}

func TestAcKompresSayacTukenir(t *testing.T) {
	var buf bytes.Buffer
	w, _ := flate.NewWriter(&buf, flate.BestCompression)
	_, _ = w.Write([]byte("kucuk normal payload"))
	_ = w.Close()
	but := &cozButce{kalanBayt: cozMaxBayt, decompresK: 0}
	if o := acKompres(buf.Bytes(), but); o != nil {
		t.Fatalf("decompresK=0 iken nil donmeliydi")
	}
}

func TestBolgeselEntropiBlob(t *testing.T) {
	normal := strings.Repeat("function f($a){ return $a + 1; } // normal kod satiri duz\n", 80)
	blob := rasgeleB64(18000, 42)
	content := []byte(normal + blob + "\neval($x);\n")
	s, id, ok := bolgeselEntropi(content)
	if !ok || id != "GOSP-HEUR-ENTROPI-BLOB" {
		t.Fatalf("gomulu blob tespit edilmedi: skor=%d id=%s ok=%v", s, id, ok)
	}
}

func TestBolgeselEntropiUniformBlobDegil(t *testing.T) {
	content := []byte(rasgeleB64(20000, 7))
	s, id, ok := bolgeselEntropi(content)
	if !ok {
		t.Fatalf("uniform yuksek entropi sinyal vermeliydi")
	}
	if id == "GOSP-HEUR-ENTROPI-BLOB" {
		t.Fatalf("uniform yuksek entropi YANLISLIKLA BLOB (skor %d)", s)
	}
}

func TestBolgeselEntropiTemiz(t *testing.T) {
	content := []byte(strings.Repeat("$total = $price * $qty; echo $total;\n", 300))
	if _, _, ok := bolgeselEntropi(content); ok {
		t.Fatalf("normal kod yanlis pozitif entropi verdi")
	}
}

func rasgeleB64(n int, seed int64) string {
	const alf = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	r := rand.New(rand.NewSource(seed))
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteByte(alf[r.Intn(64)])
	}
	return b.String()
}
