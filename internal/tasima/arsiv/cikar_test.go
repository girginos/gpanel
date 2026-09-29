package arsiv

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
)

var beklenenAtlanan = map[string]string{
	"dis-bag":      "mutlak ya da seçili kök dışına bağ",
	"mutlak":       "mutlak ya da seçili kök dışına bağ",
	"secim-disi":   "mutlak ya da seçili kök dışına bağ",
	"bos-bag":      "mutlak ya da seçili kök dışına bağ",
	"ters-bag":     "mutlak ya da seçili kök dışına bağ",
	"l/x.php":      "bağ üzerinden geçen yol",
	"aygit":        "aygıt ya da fifo",
	"blok":         "aygıt ya da fifo",
	"fifo":         "aygıt ya da fifo",
	"sabit-disari": "seçim dışına ya da yazılmamış dosyaya sabit bağ",
	"sabit-ileri":  "seçim dışına ya da yazılmamış dosyaya sabit bağ",
	"sabit-kacis":  "seçim dışına ya da yazılmamış dosyaya sabit bağ",
}

func atlananHaritasi(r *Rapor) map[string]string {
	m := map[string]string{}
	for _, a := range r.Atlanan {
		m[a.Ad] = a.Neden
	}
	return m
}

// TestAkit_Suzgec: çıkarıcıya giden akış üye üye sıfırdan kurulmuş temiz başlıklardan
// oluşur; seçim dışı, kaçan bağ, bağ altından yol, aygıt/fifo ve seçim dışı sabit bağ
// akışa girmez ve raporlanır.
func TestAkit_Suzgec(t *testing.T) {
	a, err := acT(t, gzUret(t, tarUret(t, append(iyiUyeler(), kotuUyeler()...)...)), Sinirlar{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	r := &Rapor{}
	if err := a.akit(context.Background(), secimOnek, &buf, r); err != nil {
		t.Fatal(err)
	}
	basliklar := map[string]*tar.Header{}
	icerik := map[string]string{}
	tr := tar.NewReader(&buf)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		basliklar[h.Name], icerik[h.Name] = h, string(b)
	}
	var adlar []string
	for ad := range basliklar {
		adlar = append(adlar, ad)
	}
	sort.Strings(adlar)
	want := []string{"index.php", "kisayol", "l", "sabit", "sonra.txt", "sticky/", "suid", "wp/", "wp/a.txt", "wp/ust", "wp/\xfe\xfdeski.txt"}
	if !reflect.DeepEqual(adlar, want) {
		t.Fatalf("akıştaki üyeler\n  %q\nbeklenen\n  %q", adlar, want)
	}
	saniye := time.Unix(testZaman.Unix(), 0)
	for ad, h := range basliklar {
		if h.Uid != 0 || h.Gid != 0 || h.Uname != "" || h.Gname != "" || len(h.PAXRecords) != 0 {
			t.Errorf("%q: sahiplik/xattr taşındı: %+v", ad, h)
		}
		if h.Mode&^0o777 != 0 || h.Format != tar.FormatGNU || !h.ModTime.Equal(saniye) {
			t.Errorf("%q: kip=%o biçim=%v zaman=%v", ad, h.Mode, h.Format, h.ModTime)
		}
	}
	if m := basliklar["suid"].Mode; m != 0o755 {
		t.Errorf("setuid/setgid silinmedi: %o", m)
	}
	if m := basliklar["sticky/"].Mode; m != 0o777 {
		t.Errorf("sticky silinmedi: %o", m)
	}
	if h := basliklar["sabit"]; h.Typeflag != tar.TypeLink || h.Linkname != "wp/a.txt" {
		t.Errorf("sabit bağ seçime göre göreli olmalı: %+v", h)
	}
	if h := basliklar["kisayol"]; h.Typeflag != tar.TypeSymlink || h.Linkname != "wp/a.txt" {
		t.Errorf("kısayol: %+v", h)
	}
	if icerik["index.php"] != "<?php echo 1;" || icerik["wp/a.txt"] != "AAAA" {
		t.Errorf("içerik bozuldu: %q", icerik)
	}
	if got := atlananHaritasi(r); !reflect.DeepEqual(got, beklenenAtlanan) {
		t.Errorf("atlananlar\n  %q\nbeklenen\n  %q", got, beklenenAtlanan)
	}
	if r.Dosya != 5 || r.Dizin != 2 || r.Bag != 3 || r.Sabit != 1 || r.AtlananSayisi != len(beklenenAtlanan) {
		t.Errorf("rapor = %+v", r)
	}
}

// TestAkit_TumArsiv: onek "" → tüm arşiv; arşiv kökü dizini yazılmaz.
func TestAkit_TumArsiv(t *testing.T) {
	a, err := acT(t, tarUret(t, uyeT{ad: "./", tur: tar.TypeDir}, uyeT{ad: "a/b.txt", veri: "b"},
		uyeT{ad: "c", tur: tar.TypeLink, hedef: "a/b.txt"}), Sinirlar{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	r := &Rapor{}
	if err := a.akit(context.Background(), "", &buf, r); err != nil {
		t.Fatal(err)
	}
	if r.Dosya != 1 || r.Sabit != 1 || r.Dizin != 0 || r.AtlananSayisi != 0 {
		t.Fatalf("rapor = %+v", r)
	}
}

func TestAkit_ArsivDegistiVeBomba(t *testing.T) {
	uyeler := append(iyiUyeler(), uyeT{ad: secimOnek + "/sifir.bin", boyut: 8 << 20})
	yol := dosyala(t, gzUret(t, tarUret(t, uyeler...)))
	a, err := Ac(context.Background(), yol, Sinirlar{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Bomba: oran sınırı akış sırasında (yazım yarıda) kesilir.
	eski := a.s
	a.s.BombaEsik, a.s.BombaOrani = 1<<20, 100
	if err := a.akit(context.Background(), secimOnek, io.Discard, &Rapor{}); !errors.Is(err, ErrBomba) {
		t.Fatalf("ErrBomba beklendi, gelen %v", err)
	}
	a.s = eski
	// Envanterden sonra bir üye eklendi.
	if err := os.WriteFile(yol, gzUret(t, tarUret(t, append(uyeler, uyeT{ad: secimOnek + "/sonradan.php", veri: "x"})...)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.akit(context.Background(), secimOnek, io.Discard, &Rapor{}); !errors.Is(err, ErrDegisti) {
		t.Fatalf("ErrDegisti beklendi, gelen %v", err)
	}
}

func TestDiskYeterli(t *testing.T) {
	home := t.TempDir()
	if err := diskYeterli(home, 1<<20, 10); err != nil {
		t.Fatalf("küçük seçim sığmalı: %v", err)
	}
	if err := diskYeterli(home, 1<<62, 10); !errors.Is(err, ErrDisk) {
		t.Fatalf("bayt: ErrDisk beklendi, gelen %v", err)
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(home, &st); err != nil || st.Files == 0 {
		t.Skip("dosya sistemi inode bildirmiyor")
	}
	if err := diskYeterli(home, 0, int(st.Ffree)); !errors.Is(err, ErrDisk) {
		t.Fatalf("inode: ErrDisk beklendi, gelen %v", err)
	}
}

func TestCikar_GecersizHedef(t *testing.T) {
	a, err := acT(t, gzUret(t, tarUret(t, iyiUyeler()...)), Sinirlar{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, h := range []Hedef{
		{SK: "root", Kok: "public_html"},
		{SK: "c_x/../c_y", Kok: "public_html"},
		{SK: testSK, Kok: "../c_baska/public_html"},
		{SK: testSK, Kok: "/etc"},
	} {
		if _, err := a.Cikar(ctx, secimOnek, h); !errors.Is(err, ErrGecersiz) {
			t.Errorf("%+v: ErrGecersiz beklendi, gelen %v", h, err)
		}
	}
	if _, err := a.Cikar(ctx, "../x", Hedef{SK: testSK, Kok: "public_html"}); !errors.Is(err, ErrUyeAdi) {
		t.Errorf("geçersiz önek: %v", err)
	}
}

// ---- Gerçek çıkarma (root + runuser; kiracı yerine nobody) ----

func TestCikar_Gercek(t *testing.T) {
	o := yeniOrtam(t)
	for ad, uret := range map[string]func(*testing.T, []byte) []byte{
		"gzip": gzUret,
		"xz":   func(t *testing.T, b []byte) []byte { return aracla(t, "xz", b) },
	} {
		t.Run(ad, func(t *testing.T) {
			kokAd := "public_html_" + ad
			a, err := acT(t, uret(t, tarUret(t, append(iyiUyeler(), kotuUyeler()...)...)), Sinirlar{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			r, err := a.Cikar(context.Background(), "./"+secimOnek+"/", Hedef{SK: testSK, Kok: kokAd})
			if err != nil {
				t.Fatalf("%v (tar: %s)", err, r.TarCikti)
			}
			kok := filepath.Join(o.home, kokAd)
			oku := func(rel string) string {
				b, err := os.ReadFile(filepath.Join(kok, rel))
				if err != nil {
					t.Fatalf("%s: %v", rel, err)
				}
				return string(b)
			}
			if oku("index.php") != "<?php echo 1;" || oku("wp/a.txt") != "AAAA" || oku("wp/\xfe\xfdeski.txt") != "latin5" {
				t.Fatal("içerik bozuk")
			}
			if hedef, _ := os.Readlink(filepath.Join(kok, "kisayol")); hedef != "wp/a.txt" {
				t.Errorf("kısayol hedefi %q", hedef)
			}
			s1, _ := os.Stat(filepath.Join(kok, "sabit"))
			s2, _ := os.Stat(filepath.Join(kok, "wp/a.txt"))
			if s1 == nil || s2 == nil || !os.SameFile(s1, s2) {
				t.Error("sabit bağ aynı dosyayı göstermeli")
			}
			if st, _ := os.Lstat(filepath.Join(kok, "suid")); st == nil || st.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 {
				t.Errorf("suid kipi: %v", st.Mode())
			}
			if st, _ := os.Lstat(filepath.Join(kok, "sticky")); st == nil || st.Mode()&os.ModeSticky != 0 {
				t.Errorf("sticky kipi: %v", st.Mode())
			}
			for ad := range beklenenAtlanan {
				if _, err := os.Lstat(filepath.Join(kok, ad)); err == nil {
					t.Errorf("atlanması gereken %q yazıldı", ad)
				}
			}
			if _, err := os.Lstat(filepath.Join(kok, "wp", "x.php")); err == nil {
				t.Error("bağ altından yol, bağın hedefine yazıldı")
			}
			// Her şey kiracıya ait; ev dizininde yalnız hedef kök var.
			_ = filepath.Walk(kok, func(p string, fi os.FileInfo, err error) error {
				if err == nil {
					if sys := fi.Sys().(*syscall.Stat_t); int(sys.Uid) != o.uid {
						t.Errorf("%s kiracıya ait değil (uid %d)", p, sys.Uid)
					}
				}
				return nil
			})
			if st, _ := os.Lstat(kok); st == nil || st.Mode().Perm() != 0o755 {
				t.Errorf("hedef kök kipi: %v", st.Mode())
			}
			if atlananHaritasi(r)["aygit"] == "" || r.Dosya != 5 {
				t.Errorf("rapor = %+v", r)
			}
		})
	}
	girdiler, _ := os.ReadDir(o.home)
	for _, g := range girdiler {
		if !strings.HasPrefix(g.Name(), "public_html_") {
			t.Errorf("ev dizininde beklenmeyen girdi: %q", g.Name())
		}
	}
}

// TestCikar_KiraciDACKatmani: çıkarıcı KİRACI kimliğinde koşar. Kiracı hedef kökte
// komşuyu gösteren bir bağ kurmuş olsa bile tar o dizine yazamaz (EACCES); kurban
// ağacı bayt bayt aynı kalır.
func TestCikar_KiraciDACKatmani(t *testing.T) {
	o := yeniOrtam(t)
	kurbanDir := kurban(t, o.base)
	once := agacOzeti(t, kurbanDir)
	kok := filepath.Join(o.home, "public_html")
	if err := os.Mkdir(kok, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(kok, o.uid, o.gid); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(kurbanDir, filepath.Join(kok, "wp")); err != nil {
		t.Fatal(err)
	}
	if err := os.Lchown(filepath.Join(kok, "wp"), o.uid, o.gid); err != nil {
		t.Fatal(err)
	}
	a, err := acT(t, gzUret(t, tarUret(t,
		uyeT{ad: secimOnek + "/wp/gizli.txt", veri: "EZILDI"},
		uyeT{ad: secimOnek + "/wp/kabuk.php", veri: "<?php system($_GET[1]);"},
	)), Sinirlar{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Cikar(context.Background(), secimOnek, Hedef{SK: testSK, Kok: "public_html"}); !errors.Is(err, ErrCikarma) {
		t.Errorf("çıkarıcı hata vermeliydi, gelen %v", err)
	}
	if sonra := agacOzeti(t, kurbanDir); !reflect.DeepEqual(once, sonra) {
		t.Fatalf("kurban ağacı değişti:\nönce  %q\nsonra %q", once, sonra)
	}
}

// TestCikar_HedefKokBag: hedef kökün kendisi kiracının kurduğu bir bağ ise izlenmez;
// çıkarıcı hiç başlamaz.
func TestCikar_HedefKokBag(t *testing.T) {
	o := yeniOrtam(t)
	kurbanDir := kurban(t, o.base)
	once := agacOzeti(t, kurbanDir)
	if err := os.Symlink(kurbanDir, filepath.Join(o.home, "public_html")); err != nil {
		t.Fatal(err)
	}
	a, err := acT(t, gzUret(t, tarUret(t, iyiUyeler()...)), Sinirlar{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Cikar(context.Background(), secimOnek, Hedef{SK: testSK, Kok: "public_html"}); !errors.Is(err, ErrGecersiz) {
		t.Fatalf("ErrGecersiz beklendi, gelen %v", err)
	}
	if sonra := agacOzeti(t, kurbanDir); !reflect.DeepEqual(once, sonra) {
		t.Fatalf("kurban ağacı değişti: %q", sonra)
	}
}

func TestCikar_DiskYetersizVeBomba(t *testing.T) {
	o := yeniOrtam(t)
	uyeler := append(iyiUyeler(), uyeT{ad: secimOnek + "/sifir.bin", boyut: 8 << 20})
	a, err := acT(t, gzUret(t, tarUret(t, uyeler...)), Sinirlar{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	eski := a.s
	a.s.DiskPayi = 1 << 62
	if _, err := a.Cikar(context.Background(), secimOnek, Hedef{SK: testSK, Kok: "public_html"}); !errors.Is(err, ErrDisk) {
		t.Fatalf("ErrDisk beklendi, gelen %v", err)
	}
	if _, err := os.Lstat(filepath.Join(o.home, "public_html")); err == nil {
		t.Fatal("disk yetersizken hedef dizin yaratıldı")
	}
	// Bomba yazım sırasında yakalanır; çıkarıcının hatası değil ErrBomba döner.
	a.s = eski
	a.s.BombaEsik, a.s.BombaOrani = 1<<20, 100
	r, err := a.Cikar(context.Background(), secimOnek, Hedef{SK: testSK, Kok: "public_html"})
	if !errors.Is(err, ErrBomba) || r == nil {
		t.Fatalf("ErrBomba beklendi, gelen %v", err)
	}
}

// TestCikar_ArsivDegisti: envanterden sonra arşive eklenen üye, çıkarıcıya hiç
// ulaşmadan kesilir (akış tamponu taşacak kadar büyük olsa bile).
func TestCikar_ArsivDegisti(t *testing.T) {
	o := yeniOrtam(t)
	yol := dosyala(t, gzUret(t, tarUret(t, iyiUyeler()...)))
	a, err := Ac(context.Background(), yol, Sinirlar{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ek := uyeT{ad: secimOnek + "/sonradan.bin", boyut: 1 << 20}
	if err := os.WriteFile(yol, gzUret(t, tarUret(t, append(iyiUyeler(), ek)...)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Cikar(context.Background(), secimOnek, Hedef{SK: testSK, Kok: "public_html"}); !errors.Is(err, ErrDegisti) {
		t.Fatalf("ErrDegisti beklendi, gelen %v", err)
	}
	if _, err := os.Lstat(filepath.Join(o.home, "public_html", "sonradan.bin")); err == nil {
		t.Fatal("envanterde olmayan üye çıkarıldı")
	}
}
