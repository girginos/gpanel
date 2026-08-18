package avmotor

import "testing"

// 🔴 ADVERSARYEL DENETIM PoC'leri — bunlar bir zamanlar KACIYORDU (puan 0).
// Her biri artik esigi asmali. Kaynak: security-auditor raporu 2026-08-18.
func TestDenetimKacislariKapandi(t *testing.T) {
	m, bozuk := Yeni(TabanSet(), 0)
	if bozuk != 0 {
		t.Fatalf("%d bozuk regex", bozuk)
	}
	dir := t.TempDir()
	ornekler := []struct {
		ad, dosya, icerik string
		enAz              int
	}{
		// NOT: C-3 decoupling (`$d=$_POST; $c($d)`) BILINEN SINIRLAMA — regex
		// taint yapamaz, kurallari 21 FP urettigi icin kaldirildi. Konum
		// sezgisi (uploads/*.php) bu shell'lerin cogunu yine yakalar.
		// C-6 call_user_func
		{"call_user_func", "c.php", `<?php call_user_func($_GET['f'], $_GET['a']);`, EsikKritik},
		{"call_user_func_array", "d.php", `<?php call_user_func_array($_POST['f'], $_POST['a']);`, EsikKritik},
		// C-7 $_SERVER baslik tabanli
		{"server-header-shell", "e.php", `<?php system($_SERVER['HTTP_X_RUN']);`, EsikKritik},
		// C-8 hex2bin / pack
		{"eval-hex2bin", "f.php", `<?php eval(hex2bin('6576616c'));`, EsikKritik},
		{"eval-gzdecode", "g.php", `<?php eval(gzdecode($x));`, EsikKritik},
		// C-9 preg_replace_callback
		{"preg-callback-assert", "h.php", `<?php preg_replace_callback('/(.+)/', 'assert', [$_POST['x']]);`, EsikKritik},
		// C-13 sarmalayicilar
		{"phar-include", "i.php", `<?php include('phar://evil.phar/x');`, EsikKritik},
		{"data-include", "j.php", `<?php require('data://text/plain;base64,PD9waHA=');`, EsikKritik},
	}
	for _, o := range ornekler {
		t.Run(o.ad, func(t *testing.T) {
			yol := yaz(t, dir, o.dosya, o.icerik)
			b, tespit := m.TaraDosya(yol, "", nil)
			if !tespit {
				t.Fatalf("HALA KACIYOR (puan %d): %s", b.Puan, o.icerik)
			}
			if b.Puan < o.enAz {
				t.Errorf("puan %d < %d (kurallar %v)", b.Puan, o.enAz, b.Kurallar)
			}
		})
	}
}

// 🔴 Yeni kurallar YANLIS ALARM eklemedi mi? Mesru kod hala temiz kalmali.
func TestYeniKurallarFPUretmez(t *testing.T) {
	m, _ := Yeni(TabanSet(), 0)
	dir := t.TempDir()
	mesru := []struct{ ad, dosya, icerik string }{
		// call_user_func mesru: sabit fonksiyon adiyla
		{"cuf-sabit", "a.php", `<?php call_user_func('sanitize_text_field', $input);`},
		// array_map mesru
		{"array_map-closure", "b.php", `<?php $r = array_map(function($x){ return trim($x); }, $list);`},
		// girdi degiskene AMA dinamik cagri YOK (tek zayif sinyal = 40 < 50)
		{"girdi-degisken-guvenli", "c.php", `<?php $ad=$_POST['ad']; echo htmlspecialchars($ad);`},
		// preg_replace_callback mesru closure ile
		{"preg-callback-closure", "d.php", `<?php preg_replace_callback('/\d+/', function($m){ return $m[0]*2; }, $s);`},
		// include sabit yol
		{"include-sabit", "e.php", `<?php require_once __DIR__.'/config.php';`},
	}
	for _, o := range mesru {
		t.Run(o.ad, func(t *testing.T) {
			yol := yaz(t, dir, o.dosya, o.icerik)
			b, tespit := m.TaraDosya(yol, "", nil)
			if tespit {
				t.Errorf("YANLIS ALARM (puan %d, %v): %s", b.Puan, b.Kurallar, o.icerik)
			}
		})
	}
}
