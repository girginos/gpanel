package avmotor

import (
	"regexp"
	"strings"
	"testing"
)

func testKurallar() []Kural {
	return []Kural{
		{ID: "GOSP-TEST-SYSTEM", Desen: `system\s*\(`, Puan: 60, re: regexp.MustCompile(`system\s*\(`)},
		{ID: "GOSP-TEST-EVAL", Desen: `eval\s*\(`, Puan: 60, re: regexp.MustCompile(`eval\s*\(`)},
	}
}

func semScore(src string) (int, []string) {
	return semantikTara([]byte(src), testKurallar(), "php", map[string]bool{})
}

func idIcerir(ids []string, sub string) bool {
	for _, id := range ids {
		if strings.Contains(id, sub) {
			return true
		}
	}
	return false
}

func TestFaz4_PozitifKacislar(t *testing.T) {
	posit := []struct{ ad, src, id string }{
		{"concat-sink-atama", `<?php $f = 'sy'.'st'.'em'; $f('id');`, "CONCAT-SINK"},
		{"paren-concat-cagri", `<?php ('sy'.'stem')('whoami');`, "CONCAT-SINK"},
		{"varfunc-sink", `<?php $x = 'system'; $x($_GET['c']);`, "VARFUNC-SINK"},
		{"superglobal-cagri", `<?php $_GET['f']($_GET['a']);`, "SG-CALL"},
		{"taint-call", `<?php $cb = $_POST['x']; $cb('arg');`, "TAINT-CALL"},
		{"concat-evaded-rule", `<?php $c = 'sys'.'tem'.'("ls")'; eval($c);`, "SEMANTIK-CONCAT:"},
		{"eval-concat-var", `<?php $c = 'ph'.'pinfo()'; eval($c);`, "EVAL-CONCAT"},
		{"eval-direct-concat", `<?php eval('sys'.'tem'.'("x")');`, "EVAL-CONCAT"},
		{"assert-direct-concat", `<?php assert('sys'.'tem'.'("x")');`, "EVAL-CONCAT"},
		// Regresyon — CR#1: atamaya SARILI yapısal çağrı artık KAÇMAZ.
		{"atama-sarili-sg", `<?php $x = $_GET['f']($_GET['a']);`, "SG-CALL"},
		{"atama-sarili-varfunc", `<?php $g='system'; $x = $g($_GET['a']);`, "VARFUNC-SINK"},
		// Regresyon — transitif concat (symtab üzerinden).
		{"transitif-concat", `<?php $a='sy'; $b='stem'; $c=$a.$b; $c('x');`, "CONCAT-SINK"},
		// Regresyon — SEC M2: .= birleşik concat.
		{"nokta-esit-concat", `<?php $c='sy'; $c.='stem'; $c($_GET['x']);`, "CONCAT-SINK"},
		// Regresyon — CR#4: çift-tırnak \xNN gizlemesi.
		{"hex-escape-sink", `<?php $f = "\x73\x79\x73\x74\x65\x6d"; $f($_GET['c']);`, "VARFUNC-SINK"},
	}
	for _, c := range posit {
		p, ids := semScore(c.src)
		if p <= 0 || !idIcerir(ids, c.id) {
			t.Errorf("%s: puan=%d ids=%v ('%s' bekleniyordu)", c.ad, p, ids, c.id)
		}
	}
}

func TestFaz4_YanlisPozitifYok(t *testing.T) {
	temiz := []struct{ ad, src string }{
		{"zararsiz-concat", `<?php $msg = 'Merhaba ' . $ad . ', hos geldin';`},
		{"benign-varfunc", `<?php $cb = 'strtolower'; echo $cb($x);`},
		{"wp-hook-deseni", `<?php $f = 'esc_html'; add_filter('t', $f);`},
		{"cift-tirnak-interpolasyon", `<?php $q = "SELECT $a FROM t"; $r = $q;`},
		{"sql-concat", `<?php $sql = "SELECT * " . "FROM users " . "WHERE id=1";`},
		{"html-disinda-nokta", `<html><body>a.b.c 'quote' system( ) not php</body></html>`},
		{"benign-sabit-fonksiyon", `<?php echo strtoupper('sys' . 'tem');`},
		{"array-map-callback", `<?php $r = array_map('trim', $arr);`},
		// 🔴 KRİTİK FP regresyonu — FP-adversary #1: dinamik METOT sevki (üye erişimi).
		{"dinamik-metot-taint", `<?php $action = $_REQUEST['action']; $this->$action();`},
		{"rest-metot-dispatch", `<?php $m = $_SERVER['REQUEST_METHOD']; $this->$m();`},
		{"pdo-exec-metot", `<?php $method = 'exec'; $db->$method($sql);`},
		{"statik-metot-dispatch", `<?php $cb = $_GET['x']; Foo::$cb();`},
		{"xml-bildirimi", `<?xml version="1.0"?><root>system('x')</root>`},
	}
	for _, c := range temiz {
		p, ids := semScore(c.src)
		if p > 0 {
			t.Errorf("FP: %s puan=%d ids=%v (0 bekleniyordu)", c.ad, p, ids)
		}
	}
}

func TestFaz4_ReassignTemizler(t *testing.T) {
	// $f sink iken güvenliye reatanınca VARFUNC ateşlenmemeli.
	p, ids := semScore(`<?php $f='system'; $f='strtolower'; $f($x);`)
	if p > 0 {
		t.Errorf("reassign: puan=%d ids=%v (0 bekleniyordu)", p, ids)
	}
}

func TestFaz4_YalnizPHP(t *testing.T) {
	p, _ := semantikTara([]byte(`$f='sy'.'stem'; $f('x');`), testKurallar(), "txt", map[string]bool{})
	if p != 0 {
		t.Errorf("non-php ext puan=%d (0 bekleniyordu)", p)
	}
}

func TestFaz4_BillionLaughs(t *testing.T) {
	// Üstel şişme (variable-doubling): boyut tavanı ile SINIRLI — OOM/asılma YOK.
	var b strings.Builder
	b.WriteString("<?php $a='xxxxxxxx';")
	vars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOP"
	for k := 1; k < len(vars); k++ {
		b.WriteByte('$')
		b.WriteByte(vars[k])
		b.WriteByte('=')
		b.WriteByte('$')
		b.WriteByte(vars[k-1])
		b.WriteByte('.')
		b.WriteByte('$')
		b.WriteByte(vars[k-1])
		b.WriteByte(';')
	}
	semScore(b.String()) // panik/OOM/asılma olmamalı — makul sürede dönmeli
}

func TestFaz4_DoSButce(t *testing.T) {
	var b strings.Builder
	b.WriteString("<?php $x = ")
	for i := 0; i < 50000; i++ {
		b.WriteString("'a'.")
	}
	b.WriteString("'b';")
	semScore(b.String())
}
