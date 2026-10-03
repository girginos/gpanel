package arac

// arac_test.go — DB'siz birim testleri: katalog tamlığı, sınırlar, yardımcılar.

import (
	"strings"
	"testing"
	"unicode/utf8"

	"girginospanel/internal/destek/protokol"
)

func TestKatalogTamUygulandi(t *testing.T) {
	kat := map[string]bool{}
	for _, a := range protokol.Katalog() {
		kat[a.Ad] = true
		ta, ok := araclar[a.Ad]
		if !ok {
			t.Errorf("katalog aracı uygulanmamış: %s", a.Ad)
			continue
		}
		if ta.sure <= 0 || ta.fn == nil {
			t.Errorf("%s: zaman aşımı/uygulama eksik", a.Ad)
		}
		if a.Risk != protokol.R0 {
			t.Errorf("%s: Faz 1 yürütücüsü yalnız R0 çalıştırır, katalog %s diyor", a.Ad, a.Risk)
		}
	}
	for ad := range araclar {
		if !kat[ad] {
			t.Errorf("katalogda olmayan araç uygulanmış (ölü/kaçak): %s", ad)
		}
	}
}

func TestBaytKes(t *testing.T) {
	s, k := baytKes(strings.Repeat("ğ", 3000), 4000)
	if !k || len(s) > 4000 || !utf8.ValidString(s) {
		t.Fatalf("kesme: %d %v", len(s), k)
	}
	s, _ = baytKes("a\x1bb‮c\nd", 100)
	if s != "a?b?c?d" {
		t.Fatalf("yasak karakter: %q", s)
	}
}

func TestDuzlestirHassasAnahtarAtilir(t *testing.T) {
	v := map[string]any{"alan_adi": "a.com", "ftp_parola": "x1", "db_password": "x2", "api_key": "x3",
		"ic": map[string]any{"SECRET": "x4", "ad": "y"}, "liste": []any{map[string]any{"token": "x5", "n": 1}}}
	got := strings.Join(duzlestir(v, ""), "\n")
	for _, s := range []string{"x1", "x2", "x3", "x4", "x5"} {
		if strings.Contains(got, s) {
			t.Fatalf("hassas değer düzleştirmede kaldı (%s):\n%s", s, got)
		}
	}
	if !strings.Contains(got, "alan_adi: a.com") || !strings.Contains(got, "ic.ad: y") {
		t.Fatalf("olağan alanlar eksik:\n%s", got)
	}
}

func TestDigNormal(t *testing.T) {
	got := digSatirlari("10 Mail.A.com.\n;; connection timed out\n\"v=spf1 \" \"-all\"\n10 mail.a.com\n")
	want := "10 mail.a.com,v=spf1 -all"
	if strings.Join(got, ",") != want {
		t.Fatalf("%v", got)
	}
	for ad, w := range map[string]string{"@": "a.com", "": "a.com", "www": "www.a.com", "x.b.org.": "x.b.org", "mail.a.com": "mail.a.com"} {
		if g := kayitAdi(ad, "a.com"); g != w {
			t.Fatalf("kayitAdi(%q)=%q", ad, g)
		}
	}
}

func TestAnsiSoy(t *testing.T) {
	if g := ansiSoy("\x1b[33m→ (dry)\x1b[0m chown /home/c_a"); g != "→ (dry) chown /home/c_a" {
		t.Fatalf("%q", g)
	}
}

// TestSonucKurSinirlar — protokol sınırları ve maske araçtan BAĞIMSIZ olarak
// sonucKur'da zorlanır (bir araç süzgeçsiz binlerce satır dönse de).
func TestSonucKurSinirlar(t *testing.T) {
	const sir = "PanelSirri-Birim-Testi-987654"
	y := yeni(nil, nil, []string{sir}, ortam{})
	var sat []string
	for i := 0; i < 1000; i++ {
		sat = append(sat, strings.Repeat("x", 5000)+" "+sir)
	}
	r := y.sonucKur("ABCDEFGH", &cikti{ozet: strings.Repeat("ö", 3000) + sir + "\n\x1b[31m", satirlar: sat})
	if !r.Ok || r.Dogrula() != nil {
		t.Fatalf("sonuç: %+v %v", r.HataKodu, r.Dogrula())
	}
	if len(r.Satirlar) != protokol.SatirSayisiSiniri || !r.Kesildi {
		t.Fatalf("satır sınırı: %d %v", len(r.Satirlar), r.Kesildi)
	}
	for _, s := range r.Satirlar {
		if len(s) > protokol.SatirSiniri {
			t.Fatalf("satır bayt sınırı: %d", len(s))
		}
	}
	if len(r.Ozet) > protokol.OzetSiniri {
		t.Fatalf("özet sınırı: %d", len(r.Ozet))
	}
	r = y.sonucKur("ABCDEFGH", &cikti{ozet: "özet " + sir + " DB_PASSWORD=abcdefgh123", satirlar: []string{"satır " + sir}})
	if strings.Contains(r.Ozet+strings.Join(r.Satirlar, ""), sir) || strings.Contains(r.Ozet, "abcdefgh123") {
		t.Fatalf("özet/satır maskelenmedi: %q %q", r.Ozet, r.Satirlar)
	}
	h := y.hataSonucu("ABCDEFGH", hataf(protokol.HataYetki, "ret "+sir))
	if h.Ok || h.HataKodu != protokol.HataYetki || strings.Contains(h.Ozet, sir) || h.Dogrula() != nil {
		t.Fatalf("hata sonucu: %+v", h)
	}
}
