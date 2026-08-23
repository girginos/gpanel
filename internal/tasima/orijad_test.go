package tasima

// orijad_test.go — "taşıma DB'yi ORİJİNAL adıyla mı taşıyor?" sorusunun
// birim kanıtı. configtenDBKimlik gerçek wp-config.php / XenForo config.php
// içerikleriyle çağrılır; orijinal ad+kullanıcı+parola okunabiliyorsa taşıma
// kolu orijinali kullanır (aktar.go: "ORIJINAL kimlik tercihi" bloğu).

import (
	"os"
	"path/filepath"
	"testing"
)

func yazDosya(t *testing.T, yol, icerik string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(yol), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(yol, []byte(icerik), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestOrijinalDBKimligiOkunuyor(t *testing.T) {
	kok := t.TempDir()

	// 1) WordPress
	wp := filepath.Join(kok, "wp")
	yazDosya(t, filepath.Join(wp, "wp-config.php"), `<?php
define( 'DB_NAME', 'musteri_wp_asil' );
define( 'DB_USER', 'musteri_wpuser' );
define( 'DB_PASSWORD', 'GizliParola123' );
define( 'DB_HOST', 'localhost' );
`)

	// 2) XenForo — iki db bloklu (ilki ÖLÜ, PHP'de son atama kazanır)
	xf := filepath.Join(kok, "xf")
	yazDosya(t, filepath.Join(xf, "src", "config.php"), `<?php
$config['db']['host'] = 'localhost';
$config['db']['dbname'] = 'ESKI_OLU_DB';
$config['db']['username'] = 'eski_kullanici';
$config['db']['password'] = 'eskiparola';

// gerçek yapılandırma aşağıda (kurulum sonrası yazılmış)
$config['db']['dbname'] = 'forum_xf2';
$config['db']['username'] = 'forum_xfuser';
$config['db']['password'] = 'YeniParola456';
`)

	testler := []struct {
		ad         string
		kok        string
		bekAd      string
		bekKul     string
		bekPw      string
	}{
		{"wordpress", wp, "musteri_wp_asil", "musteri_wpuser", "GizliParola123"},
		{"xenforo (son atama kazanır)", xf, "forum_xf2", "forum_xfuser", "YeniParola456"},
	}

	for _, tt := range testler {
		t.Run(tt.ad, func(t *testing.T) {
			kimlikler := configtenDBKimlik(tt.kok)
			k, ok := kimlikler[tt.bekAd]
			if !ok {
				t.Fatalf("orijinal DB adı %q okunamadı; okunanlar: %v", tt.bekAd, kimlikler)
			}
			if k.Kul != tt.bekKul {
				t.Errorf("kullanıcı: %q istendi, %q bulundu", tt.bekKul, k.Kul)
			}
			if k.Pw != tt.bekPw {
				t.Errorf("parola okunamadı/yanlış (uzunluk %d)", len(k.Pw))
			}
			// ÖLÜ blok kazanmamalı
			if _, varMi := kimlikler["ESKI_OLU_DB"]; varMi && tt.ad != "wordpress" {
				t.Errorf("ölü config bloğu seçilmiş — son atama kazanmalıydı")
			}
			t.Logf("ORİJİNAL kimlik okundu: db=%s kullanıcı=%s parola=%d karakter", tt.bekAd, k.Kul, len(k.Pw))
		})
	}
}

// TestOrijinalOkunamazsaBos: config yoksa kimlik haritası boş dönmeli
// (bu durumda taşıma benzersiz ada düşer — sessiz yanlış ad üretmez).
func TestConfigYoksaBos(t *testing.T) {
	bos := t.TempDir()
	if k := configtenDBKimlik(bos); len(k) != 0 {
		t.Fatalf("boş dizinden kimlik üretildi: %v", k)
	}
	t.Log("config yok → kimlik yok → benzersiz ada düşer (beklenen)")
}
