package cgroup

// cgroup_test.go — okuyucular geçici dizindeki SAHTE cgroup ağacına karşı sınanır;
// gerçek /sys/fs/cgroup'a dokunulmaz (yalnız GOSP_TEST_CANLI_SK verilirse, salt-okur).

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func sahteDizin(t *testing.T, dosyalar map[string]string) string {
	t.Helper()
	d := t.TempDir()
	for ad, icerik := range dosyalar {
		if err := os.WriteFile(filepath.Join(d, ad), []byte(icerik), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func TestDeger(t *testing.T) {
	d := sahteDizin(t, map[string]string{
		"memory.current": "59281408\n", "memory.max": "max\n", "bozuk": "abc\n", "bos": "",
	})
	for _, tc := range []struct {
		dosya string
		v     int64
		ok    bool
	}{
		{"memory.current", 59281408, true},
		{"memory.max", Sinirsiz, true},
		{"bozuk", 0, false},
		{"bos", 0, false},
		{"yok", 0, false},
	} {
		v, ok := Deger(filepath.Join(d, tc.dosya))
		if v != tc.v || ok != tc.ok {
			t.Errorf("%s: (%d,%v) beklenir, gelen (%d,%v)", tc.dosya, tc.v, tc.ok, v, ok)
		}
	}
}

func TestCPUUsecVeCPUMax(t *testing.T) {
	for _, tc := range []struct {
		ad, stat, max string
		usec          int64
		usecOK        bool
		kota, periyot int64
		maxOK         bool
	}{
		{"181 örneği", "usage_usec 550861904\nuser_usec 314485879\nsystem_usec 236376025\n", "50000 100000\n", 550861904, true, 50000, 100000, true},
		{"sınırsız", "usage_usec 7\n", "max 100000\n", 7, true, Sinirsiz, 100000, true},
		{"usage_usec yok", "user_usec 5\n", "50000\n", 0, false, 0, 0, false},
		{"periyot sıfır", "usage_usec x\n", "50000 0\n", 0, false, 0, 0, false},
	} {
		d := sahteDizin(t, map[string]string{"cpu.stat": tc.stat, "cpu.max": tc.max})
		u, ok := CPUUsec(d)
		if u != tc.usec || ok != tc.usecOK {
			t.Errorf("%s: CPUUsec (%d,%v) beklenir, gelen (%d,%v)", tc.ad, tc.usec, tc.usecOK, u, ok)
		}
		k, p, ok := CPUMax(d)
		if k != tc.kota || p != tc.periyot || ok != tc.maxOK {
			t.Errorf("%s: CPUMax (%d,%d,%v) beklenir, gelen (%d,%d,%v)", tc.ad, tc.kota, tc.periyot, tc.maxOK, k, p, ok)
		}
	}
	if _, ok := CPUUsec(t.TempDir()); ok {
		t.Error("cpu.stat yokken ok=false beklenir")
	}
	if _, _, ok := CPUMax(t.TempDir()); ok {
		t.Error("cpu.max yokken ok=false beklenir")
	}
}

// Sahte /sys/dev/block: 253:0 (dm-0) 252:4'ün üstüne kurulu.
func sahteBlok(t *testing.T) {
	t.Helper()
	eski := blokKok
	t.Cleanup(func() { blokKok = eski })
	blokKok = t.TempDir()
	if err := os.MkdirAll(filepath.Join(blokKok, "253:0", "slaves", "vda4"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(blokKok, "252:0", "slaves"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestIOToplam(t *testing.T) {
	sahteBlok(t)
	for _, tc := range []struct {
		ad, stat string
		r, w     int64
	}{
		{"tek disk (181)", "252:0 rbytes=163315712 wbytes=75796480 rios=5284 wios=18505 dbytes=0 dios=0\n", 163315712, 75796480},
		// LVM: aynı G/Ç hem dm-0'da hem alttaki diskte görünür → yalnız yaprak sayılır.
		{"dm üstü + alt disk", "253:0 rbytes=1000 wbytes=2000 rios=1 wios=1\n252:0 rbytes=1100 wbytes=2100 rios=1 wios=1\n", 1100, 2100},
		{"yalnız dm görünüyor", "253:0 rbytes=1000 wbytes=2000\n", 1000, 2000},
		{"iki yaprak disk", "252:0 rbytes=10 wbytes=20\n8:0 rbytes=1 wbytes=2\n", 11, 22},
		{"henüz G/Ç yok", "", 0, 0},
	} {
		d := sahteDizin(t, map[string]string{"io.stat": tc.stat})
		r, w, ok := IOToplam(d)
		if !ok || r != tc.r || w != tc.w {
			t.Errorf("%s: (%d,%d,true) beklenir, gelen (%d,%d,%v)", tc.ad, tc.r, tc.w, r, w, ok)
		}
	}
	if _, _, ok := IOToplam(t.TempDir()); ok {
		t.Error("io.stat yokken ok=false beklenir")
	}
}

func TestKiraciDizin(t *testing.T) {
	eski := Kok
	t.Cleanup(func() { Kok = eski })
	Kok = "/kok"
	if g := KiraciDizin("c_test_com"); g != "/kok/girginos.slice/girginos-c_test_com.slice" {
		t.Errorf("yol yanlış: %q", g)
	}
	// Yol birleştirmeye ayırıcı / üst dizin / systemd hiyerarşi ayracı giremez.
	for _, sk := range []string{"", "c_", "../etc", "c_a/../../x", "c_a-b", "C_TEST", "root", "c_a.b", "c_a b",
		"c_" + strings.Repeat("a", 61)} {
		if g := KiraciDizin(sk); g != "" {
			t.Errorf("%q biçim dışı, boş beklenir: %q", sk, g)
		}
	}
	if g := ServisDizin("gosp-app"); g != "/kok/system.slice/gosp-app.service" {
		t.Errorf("servis yolu yanlış: %q", g)
	}
}

func sayac(zaman time.Time, cpu, r, w int64) Sayac {
	return Sayac{Zaman: zaman, CPUUsec: cpu, CPUVar: true, IOOkuma: r, IOYazma: w, IOVar: true}
}

func TestOrnekleyici(t *testing.T) {
	t0 := time.Unix(1_790_000_000, 0)
	o := &Ornekleyici{EnAz: 500 * time.Millisecond, EnCok: 2 * time.Minute}

	// İlk örnek: hız bilinmiyor.
	if h := o.Hesapla("a", sayac(t0, 1_000_000, 0, 0)); h.CPUYuzde != nil || h.IOOkumaBps != nil || h.IOYazmaBps != nil {
		t.Fatalf("ilk örnekte hız nil beklenir: %+v", h)
	}
	// 10 sn'de 5 sn CPU = %50; 10 MB okuma = 1 MB/sn; 5 MB yazma = 0.5 MB/sn.
	h := o.Hesapla("a", sayac(t0.Add(10*time.Second), 6_000_000, 10<<20, 5<<20))
	if h.CPUYuzde == nil || *h.CPUYuzde != 50 {
		t.Fatalf("CPU %%50 beklenir: %v", h.CPUYuzde)
	}
	if h.IOOkumaBps == nil || *h.IOOkumaBps != 1<<20 || h.IOYazmaBps == nil || *h.IOYazmaBps != 1<<19 {
		t.Fatalf("G/Ç 1MB/0.5MB beklenir: %v %v", h.IOOkumaBps, h.IOYazmaBps)
	}
	// Çok çekirdek: 10 sn'de 25 sn CPU = %250.
	h = o.Hesapla("a", sayac(t0.Add(20*time.Second), 31_000_000, 10<<20, 5<<20))
	if h.CPUYuzde == nil || *h.CPUYuzde != 250 {
		t.Fatalf("çok çekirdekte %%250 beklenir: %v", h.CPUYuzde)
	}
	// EnAz'dan kısa aralık: taban yerinde kalır, son hız döner (sıçrayan sayaç yok sayılır).
	h = o.Hesapla("a", sayac(t0.Add(20*time.Second+100*time.Millisecond), 99_000_000, 1<<30, 1<<30))
	if h.CPUYuzde == nil || *h.CPUYuzde != 250 {
		t.Fatalf("kısa aralıkta son hız (%%250) beklenir: %v", h.CPUYuzde)
	}
	h = o.Hesapla("a", sayac(t0.Add(30*time.Second), 36_000_000, 10<<20, 5<<20))
	if h.CPUYuzde == nil || *h.CPUYuzde != 50 {
		t.Fatalf("taban korunmalıydı (20. sn): %%50 beklenir: %v", h.CPUYuzde)
	}
	// Sayaç geri gitti (slice yeniden yaratıldı): hız nil, yeni taban.
	h = o.Hesapla("a", sayac(t0.Add(40*time.Second), 100, 0, 0))
	if h.CPUYuzde != nil || h.IOOkumaBps != nil {
		t.Fatalf("sayaç geri gidince nil beklenir: %+v", h)
	}
	h = o.Hesapla("a", sayac(t0.Add(50*time.Second), 1_000_100, 0, 0))
	if h.CPUYuzde == nil || *h.CPUYuzde != 10 {
		t.Fatalf("yeni tabandan %%10 beklenir: %v", h.CPUYuzde)
	}
	// Bayat örnek (EnCok'tan eski): uzun aralığın ortalaması verilmez.
	h = o.Hesapla("a", sayac(t0.Add(50*time.Second+3*time.Minute), 2_000_100, 0, 0))
	if h.CPUYuzde != nil {
		t.Fatalf("bayat örnekte nil beklenir: %v", *h.CPUYuzde)
	}
	// Okunamayan sayaç: yalnız o hız nil.
	b := sayac(t0.Add(4*time.Minute), 0, 0, 0)
	b.CPUVar = false
	o.Hesapla("b", b)
	b2 := sayac(t0.Add(4*time.Minute+10*time.Second), 0, 100, 0)
	h = o.Hesapla("b", b2)
	if h.CPUYuzde != nil || h.IOOkumaBps == nil || *h.IOOkumaBps != 10 {
		t.Fatalf("CPU nil + G/Ç 10 B/sn beklenir: %+v", h)
	}
}

// Budama EnCok'ta bir koşar; arada EnCok'u aşan bir örnek hâlâ haritada olabilir.
// Hız yine de verilmemeli (budamaya güvenilmez).
func TestOrnekleyici_BayatAmaBudanmamis(t *testing.T) {
	t0 := time.Unix(1_790_000_000, 0)
	o := &Ornekleyici{EnAz: 500 * time.Millisecond, EnCok: 2 * time.Minute}
	o.Hesapla("a", sayac(t0, 1_000_000, 0, 0))                                        // budama t0'da
	o.Hesapla("b", sayac(t0.Add(2*time.Minute), 1, 0, 0))                             // budama: a tam 2 dk → kalır
	h := o.Hesapla("a", sayac(t0.Add(2*time.Minute+30*time.Second), 2_500_000, 0, 0)) // budama yok (30 sn)
	if h.CPUYuzde != nil {
		t.Fatalf("2.5 dk'lık aralığın ortalaması verilmemeli: %v", *h.CPUYuzde)
	}
}

func TestOrnekleyici_BudamaSilSuz(t *testing.T) {
	t0 := time.Unix(1_790_000_000, 0)
	o := &Ornekleyici{EnAz: 500 * time.Millisecond, EnCok: 2 * time.Minute}
	o.Hesapla("eski", sayac(t0, 1, 0, 0))
	o.Hesapla("x", sayac(t0.Add(2*time.Minute+time.Second), 1, 0, 0))
	o.Hesapla("y", sayac(t0.Add(2*time.Minute+30*time.Second), 1, 0, 0))
	if _, var_ := o.son["eski"]; var_ {
		t.Error("EnCok'tan eski girdi budanmalıydı (bellek sınırsız büyür)")
	}
	if _, var_ := o.son["x"]; !var_ {
		t.Fatal("taze girdi budanmamalıydı")
	}
	o.Sil("x")
	if _, var_ := o.son["x"]; var_ {
		t.Error("Sil girdiyi silmedi")
	}
	o.Hesapla("z", sayac(t0.Add(2*time.Minute+30*time.Second), 1, 0, 0))
	o.Suz(func(k string) bool { return k == "z" })
	if _, var_ := o.son["y"]; var_ || len(o.son) != 1 {
		t.Errorf("Suz yalnız z'yi tutmalıydı: %v", o.son)
	}
}

// Gerçek ağaca karşı SALT-OKUR deneme (181 gibi canlı bir sunucuda):
//
//	GOSP_TEST_CANLI_SK=c_test_com go test -run TestGercekKiraciSlice -v ./internal/cgroup/
//
// Hiçbir dosyaya yazmaz; iki örnek arası 2 sn bekler ve değerleri yazdırır.
func TestGercekKiraciSlice_SaltOkur(t *testing.T) {
	sk := os.Getenv("GOSP_TEST_CANLI_SK")
	if sk == "" {
		t.Skip("GOSP_TEST_CANLI_SK yok — gerçek cgroup okuması atlandı")
	}
	d := KiraciDizin(sk)
	if d == "" {
		t.Fatalf("sk biçim dışı: %q", sk)
	}
	o := &Ornekleyici{EnAz: 500 * time.Millisecond, EnCok: 2 * time.Minute}
	oku := func() (Sayac, Hiz) {
		s := Sayac{Zaman: time.Now()}
		s.CPUUsec, s.CPUVar = CPUUsec(d)
		s.IOOkuma, s.IOYazma, s.IOVar = IOToplam(d)
		return s, o.Hesapla(d, s)
	}
	s1, h1 := oku()
	time.Sleep(2 * time.Second)
	s2, h2 := oku()
	bellek, _ := Deger(filepath.Join(d, "memory.current"))
	bellekMax, _ := Deger(filepath.Join(d, "memory.max"))
	surec, _ := Deger(filepath.Join(d, "pids.current"))
	kota, periyot, _ := CPUMax(d)
	f := func(p *float64) string {
		if p == nil {
			return "null"
		}
		return strconv.FormatFloat(*p, 'f', 2, 64)
	}
	t.Logf("dizin=%s", d)
	t.Logf("örnek1: cpu_usec=%d(%v) io r/w=%d/%d(%v) hız cpu=%s", s1.CPUUsec, s1.CPUVar, s1.IOOkuma, s1.IOYazma, s1.IOVar, f(h1.CPUYuzde))
	t.Logf("örnek2: cpu_usec=%d io r/w=%d/%d → cpu_yuzde=%s io_okuma_bps=%s io_yazma_bps=%s",
		s2.CPUUsec, s2.IOOkuma, s2.IOYazma, f(h2.CPUYuzde), f(h2.IOOkumaBps), f(h2.IOYazmaBps))
	t.Logf("memory.current=%d memory.max=%d pids.current=%d cpu.max=%d/%d", bellek, bellekMax, surec, kota, periyot)
	if !s1.CPUVar || h1.CPUYuzde != nil || h2.CPUYuzde == nil {
		t.Fatalf("ilk örnek nil, ikinci dolu beklenir")
	}
}
