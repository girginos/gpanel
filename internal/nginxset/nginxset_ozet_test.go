package nginxset

// nginxset_ozet_test.go — DirektifHatasiOzet: `nginx -t` çıktısından YALNIZ
// kiracının kendi direktiflerine ait hata satırları kalır (ret varsayılan).

import (
	"strings"
	"testing"
)

func TestDirektifHatasiOzet(t *testing.T) {
	cases := []struct {
		ad, cikti, direktifler string
		icermeli, icermemeli   []string
	}{
		{
			ad:          "uyarılar ve konumsuz komşu satırı atılır",
			cikti:       nt_komsuUyarilar + "\nnginx: [emerg] cannot load certificate \"/etc/pki/girginospanel/baska.com/baska.com.crt\"\n" + nt_testBasarisiz,
			direktifler: "add_header X 1;",
			icermemeli:  []string{"baska.com", "test2.girginos.app", "/etc/", "ssl_stapling"},
		},
		{
			ad:          "başka dosyadaki konumlu hata atılır",
			cikti:       "nginx: [emerg] unknown directive \"x\" in /etc/nginx/conf.d/dom_c_komsu.conf:3",
			direktifler: "x 1;",
			icermemeli:  []string{"dom_c_komsu", "/etc/"},
		},
		{
			ad:          "kendi değişkeni (sözcük olarak) kalır",
			cikti:       "nginx: [emerg] unknown \"benim_degiskenim\" variable",
			direktifler: "add_header X $benim_degiskenim;",
			icermeli:    []string{`unknown "benim_degiskenim" variable`},
		},
		{
			// Alt dizgi yoklaması: kiracı "gizli" yazarak komşunun "gizli_komsu"
			// değişkeninin adını doğrulayamasın.
			ad:          "alt dizgi eşleşmesi yetmez",
			cikti:       "nginx: [emerg] unknown \"gizli_komsu\" variable",
			direktifler: "add_header X $gizli_komsu2; add_header Y $xgizli_komsu;",
			icermemeli:  []string{"gizli_komsu"},
		},
		{
			ad:          "tırnaksız konumsuz sunucu geneli satır atılır",
			cikti:       "nginx: [emerg] could not build server_names_hash, you should increase server_names_hash_bucket_size: 64",
			direktifler: "add_header X 1;",
			icermemeli:  []string{"server_names_hash"},
		},
		{
			ad:          "kendi geçici dosyasındaki hata kalır, yabancı yol maskelenir",
			cikti:       "nginx: [emerg] open() \"/etc/pki/girginospanel/baska.com/dh.pem\" failed (2: No such file or directory) in (direktifler):8",
			direktifler: "ssl_dhparam dh.pem;",
			icermeli:    []string{"(direktifler):8", "(yol)"},
			icermemeli:  []string{"baska.com", "/etc/pki"},
		},
		{
			ad:          "kiracının kendi yazdığı yol maskelenmez",
			cikti:       "nginx: [emerg] invalid value \"/benim/yolum\" in (direktifler):7",
			direktifler: "expires /benim/yolum;",
			icermeli:    []string{"/benim/yolum", "(direktifler):7"},
		},
	}
	for _, c := range cases {
		oz := DirektifHatasiOzet(c.cikti, c.direktifler)
		for _, s := range c.icermeli {
			if !strings.Contains(oz, s) {
				t.Errorf("%s: %q içermeli, gelen %q", c.ad, s, oz)
			}
		}
		for _, s := range c.icermemeli {
			if strings.Contains(oz, s) {
				t.Errorf("%s: %q İÇERMEMELİ, gelen %q", c.ad, s, oz)
			}
		}
		if len(c.icermeli) == 0 && oz != "" {
			t.Errorf("%s: hiçbir satır kalmamalıydı, gelen %q", c.ad, oz)
		}
	}
}
