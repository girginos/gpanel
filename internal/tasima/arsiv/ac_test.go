package arsiv

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func ustVeriSec(ad string) bool {
	return strings.HasPrefix(ad, "cpmove-u/cp/") || strings.HasPrefix(ad, "cpmove-u/userdata/")
}

func TestAc_Envanter(t *testing.T) {
	a, err := acT(t, gzUret(t, tarUret(t, iyiUyeler()...)), Sinirlar{}, ustVeriSec)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Uyeler) != len(iyiUyeler()) {
		t.Fatalf("üye sayısı %d, beklenen %d", len(a.Uyeler), len(iyiUyeler()))
	}
	if u := a.Uyeler[0]; u.Ad != "" || u.Tur != tar.TypeDir {
		t.Fatalf("kök üye = %+v", u)
	}
	if u := a.Uyeler[2]; u.Ad != "cpmove-u/cp/u" {
		t.Fatalf("./ öneki normalize edilmedi: %q", u.Ad)
	}
	if u := a.Uyeler[8]; u.Ad != secimOnek || u.Tur != tar.TypeDir {
		t.Fatalf("sondaki / düşmeli: %+v", u)
	}
	want := map[string][]byte{
		"cpmove-u/cp/u":          []byte("USER=u\nDNS=ornek.com\n"),
		"cpmove-u/userdata/main": []byte("main_domain: ornek.com\n"),
	}
	if !reflect.DeepEqual(a.UstVeri, want) {
		t.Fatalf("üst veri = %q", a.UstVeri)
	}
	if u := a.Uyeler[a.indeks["cpmove-u/homedir/access-logs"]]; u.Tur != tar.TypeSymlink || u.Hedef != "/usr/local/apache/domlogs/u" {
		t.Fatalf("cPanel'in mutlak bağı envanterde olduğu gibi durmalı (çıkarmada atlanır): %+v", u)
	}
}

func TestAc_Bicimler(t *testing.T) {
	ham := tarUret(t, iyiUyeler()...)
	taban, err := acT(t, ham, Sinirlar{}, ustVeriSec)
	if err != nil {
		t.Fatal(err)
	}
	for ad, uret := range map[string]func(*testing.T) []byte{
		"gzip":  func(t *testing.T) []byte { return gzUret(t, ham) },
		"bzip2": func(t *testing.T) []byte { return aracla(t, "bzip2", ham) },
		"xz":    func(t *testing.T) []byte { return aracla(t, "xz", ham) },
		"zstd":  func(t *testing.T) []byte { sahteZstd(t); return zstdUret(ham) },
	} {
		t.Run(ad, func(t *testing.T) {
			a, err := acT(t, uret(t), Sinirlar{}, ustVeriSec)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(a.Uyeler, taban.Uyeler) || !reflect.DeepEqual(a.UstVeri, taban.UstVeri) {
				t.Fatal("envanter düz tar ile aynı değil")
			}
		})
	}
}

func TestAc_KotuAdlarArsiviReddeder(t *testing.T) {
	for ad, u := range map[string]uyeT{
		"mutlak":         {ad: "/etc/cron.d/x", veri: "x"},
		"ust":            {ad: "../x", veri: "x"},
		"ic-ust":         {ad: "a/../../x", veri: "x"},
		"sonda-ust":      {ad: "a/..", tur: tar.TypeDir},
		"ters-bolu":      {ad: `a\..\x`, veri: "x"},
		"uzun":           {ad: strings.Repeat("x", 4097), veri: "x"},
		"derin":          {ad: strings.Repeat("d/", 64) + "x", veri: "x"},
		"uzun-bag-hedef": {ad: "b", tur: tar.TypeSymlink, hedef: strings.Repeat("h", 4097)},
		"kok-dosya":      {ad: ".", veri: "x"},
	} {
		t.Run(ad, func(t *testing.T) {
			_, err := acT(t, gzUret(t, tarUret(t, uyeT{ad: "iyi.txt", veri: "i"}, u)), Sinirlar{}, nil)
			if !errors.Is(err, ErrUyeAdi) {
				t.Fatalf("ErrUyeAdi beklendi, gelen %v", err)
			}
		})
	}
	// Sınırın bir altı geçerli.
	if _, err := acT(t, tarUret(t, uyeT{ad: strings.Repeat("d/", 63) + "x", veri: "x"},
		uyeT{ad: strings.Repeat("y", 4096), veri: "y"}), Sinirlar{}, nil); err != nil {
		t.Fatalf("64 bileşen / 4096 bayt kabul edilmeli: %v", err)
	}
}

// TestAc_NULluPAXYoluReddedilir: adTemizle NUL aramaz; tar.Reader NUL'lu PAX yolunu
// reddettiği için bu varsayım burada kilitlenir.
func TestAc_NULluPAXYoluReddedilir(t *testing.T) {
	ad := strings.Repeat("a", 120) + "Q"
	ham := tarUret(t, uyeT{ad: ad, veri: "x"})
	i := bytes.Index(ham, []byte(ad+"\n"))
	if i < 0 {
		t.Fatal("PAX yol kaydı bulunamadı")
	}
	ham[i+len(ad)-1] = 0
	if _, err := acT(t, ham, Sinirlar{}, nil); !errors.Is(err, ErrBicim) {
		t.Fatalf("NUL'lu ad reddedilmeliydi: %v", err)
	}
}

func TestAc_TekrarlananUye(t *testing.T) {
	for ad, uyeler := range map[string][]uyeT{
		"ust-veri":    {{ad: "cpmove-u/cp/u", veri: "USER=u"}, {ad: "./cpmove-u/cp/u", veri: "USER=kok"}},
		"dosya":       {{ad: "a.sql", veri: "1"}, {ad: "a.sql", veri: "2"}},
		"dizin-dosya": {{ad: "d", tur: tar.TypeDir}, {ad: "d", veri: "x"}},
		"bag-dizin":   {{ad: "d", tur: tar.TypeSymlink, hedef: "."}, {ad: "d/", tur: tar.TypeDir}},
	} {
		t.Run(ad, func(t *testing.T) {
			if _, err := acT(t, tarUret(t, uyeler...), Sinirlar{}, ustVeriSec); !errors.Is(err, ErrTekrar) {
				t.Fatalf("ErrTekrar beklendi, gelen %v", err)
			}
		})
	}
	// İki kez geçen dizin zararsızdır (tar birleştirir).
	if _, err := acT(t, tarUret(t, uyeT{ad: "d/", tur: tar.TypeDir}, uyeT{ad: "./d", tur: tar.TypeDir},
		uyeT{ad: "./", tur: tar.TypeDir}, uyeT{ad: ".", tur: tar.TypeDir}), Sinirlar{}, nil); err != nil {
		t.Fatalf("yinelenen dizin kabul edilmeli: %v", err)
	}
}

func TestAc_Sinirlar(t *testing.T) {
	ust := uyeT{ad: "cpmove-u/cp/u", veri: strings.Repeat("u", 20)}
	cases := []struct {
		ad     string
		s      Sinirlar
		uyeler []uyeT
		want   error
	}{
		{"ust-veri", Sinirlar{UstVeriBayt: 19}, []uyeT{ust}, ErrUstVeri},
		{"ust-veri-toplam", Sinirlar{UstVeriBayt: 30}, []uyeT{ust, {ad: "cpmove-u/userdata/main", veri: strings.Repeat("m", 20)}}, ErrUstVeri},
		{"ust-veri-4MiB-tavan", Sinirlar{UstVeriBayt: 1 << 30}, []uyeT{{ad: "cpmove-u/cp/u", boyut: 4<<20 + 1}}, ErrUstVeri},
		{"uye-sayisi", Sinirlar{UyeSayisi: 3}, []uyeT{{ad: "a"}, {ad: "b"}, {ad: "c"}, {ad: "d"}}, ErrSinir},
		{"toplam", Sinirlar{ToplamBayt: 100}, []uyeT{{ad: "a", boyut: 60}, {ad: "b", boyut: 60}}, ErrSinir},
		{"ad-bayti", Sinirlar{AdBayt: 30}, []uyeT{{ad: strings.Repeat("a", 20)}, {ad: strings.Repeat("b", 20)}}, ErrSinir},
	}
	for _, c := range cases {
		t.Run(c.ad, func(t *testing.T) {
			if _, err := acT(t, gzUret(t, tarUret(t, c.uyeler...)), c.s, ustVeriSec); !errors.Is(err, c.want) {
				t.Fatalf("%v beklendi, gelen %v", c.want, err)
			}
		})
	}
	// Sınırın tam üstü değil, tam kendisi geçer.
	if _, err := acT(t, tarUret(t, ust), Sinirlar{UstVeriBayt: 20}, ustVeriSec); err != nil {
		t.Fatalf("sınıra eşit üst veri kabul edilmeli: %v", err)
	}
	if _, err := acT(t, tarUret(t, uyeT{ad: "a"}, uyeT{ad: "b"}, uyeT{ad: "c"}), Sinirlar{UyeSayisi: 3}, nil); err != nil {
		t.Fatalf("sınıra eşit üye sayısı kabul edilmeli: %v", err)
	}
}

func TestAc_Bomba(t *testing.T) {
	ham := tarUret(t, uyeT{ad: "sifir.bin", boyut: 8 << 20})
	dar := Sinirlar{BombaEsik: 1 << 20, BombaOrani: 100}
	for ad, arsiv := range map[string]func(*testing.T) []byte{
		"gzip": func(t *testing.T) []byte { return gzUret(t, ham) },
		"xz":   func(t *testing.T) []byte { return aracla(t, "xz", ham) },
	} {
		t.Run(ad, func(t *testing.T) {
			b := arsiv(t)
			if _, err := acT(t, b, dar, nil); !errors.Is(err, ErrBomba) {
				t.Fatalf("ErrBomba beklendi, gelen %v", err)
			}
			// Varsayılan eşiğin (512 MiB) altında aynı arşiv geçer.
			if _, err := acT(t, b, Sinirlar{}, nil); err != nil {
				t.Fatalf("eşik altı arşiv reddedildi: %v", err)
			}
		})
	}
}

func TestAc_Bozuk(t *testing.T) {
	gz := gzUret(t, tarUret(t, iyiUyeler()...))
	crc := append([]byte{}, gz...)
	crc[len(crc)-8] ^= 0xff // gzip CRC32: tar sonu okunduktan SONRA denetlenir
	xz := aracla(t, "xz", tarUret(t, iyiUyeler()...))
	for ad, b := range map[string][]byte{
		"cop":      bytes.Repeat([]byte("cop-veri-"), 200),
		"gz-kesik": gz[:len(gz)/2],
		"gz-crc":   crc,
		"xz-kesik": xz[:len(xz)-20],
		"bos-gzip": gz[:10],
	} {
		t.Run(ad, func(t *testing.T) {
			if _, err := acT(t, b, Sinirlar{}, nil); !errors.Is(err, ErrBicim) {
				t.Fatalf("ErrBicim beklendi, gelen %v", err)
			}
		})
	}
}

func TestAc_AciciYok(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := acT(t, zstdUret(tarUret(t, iyiUyeler()...)), Sinirlar{}, nil); !errors.Is(err, ErrAcici) {
		t.Fatalf("ErrAcici beklendi, gelen %v", err)
	}
}

func TestAc_IptalVeDosyaTuru(t *testing.T) {
	yol := dosyala(t, gzUret(t, tarUret(t, iyiUyeler()...)))
	ctx, iptal := context.WithCancel(context.Background())
	iptal()
	if _, err := Ac(ctx, yol, Sinirlar{}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("iptal beklendi, gelen %v", err)
	}
	bag := filepath.Join(t.TempDir(), "bag.tar.gz")
	if err := os.Symlink(yol, bag); err != nil {
		t.Fatal(err)
	}
	if _, err := Ac(context.Background(), bag, Sinirlar{}, nil); !errors.Is(err, ErrBicim) {
		t.Fatalf("bağ olan arşiv yolu izlenmemeli: %v", err)
	}
	if _, err := Ac(context.Background(), t.TempDir(), Sinirlar{}, nil); !errors.Is(err, ErrBicim) {
		t.Fatalf("dizin arşiv sayılmamalı: %v", err)
	}
}
