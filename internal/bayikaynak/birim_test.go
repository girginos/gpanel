package bayikaynak

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDilimIcerik_DegerlerVeSinirsiz(t *testing.T) {
	tam := DilimIcerik(Havuz{BayiID: 12, CPUCekirdek: 1.5, RAMMB: 2048, IOAgirlik: 200})
	for _, ister := range []string{
		"# GirginOSPanel bayi kaynak havuzu — bayi 12\n",
		"CPUQuota=150%\n", "MemoryMax=2048M\n", "MemoryHigh=1843M\n", "MemorySwapMax=0\n", "IOWeight=200\n",
		"CPUAccounting=yes\n", "MemoryAccounting=yes\n", "IOAccounting=yes\n", "Before=slices.target\n",
	} {
		if !strings.Contains(tam, ister) {
			t.Errorf("havuz diliminde yok: %q\n%s", ister, tam)
		}
	}
	if !strings.HasPrefix(tam, DilimImza+"12\n") {
		t.Errorf("imza ilk satır değil: %q", tam)
	}
	// 0 = SINIRSIZ: hiçbir tavan direktifi yazılmaz, yalnız muhasebe.
	bos := DilimIcerik(Havuz{BayiID: 12})
	for _, yasak := range []string{"CPUQuota", "MemoryMax", "MemoryHigh", "MemorySwapMax", "IOWeight"} {
		if strings.Contains(bos, yasak) {
			t.Errorf("sınırsız (0) havuzda %s yazıldı:\n%s", yasak, bos)
		}
	}
	if !strings.Contains(bos, "CPUAccounting=yes") {
		t.Error("muhasebe her zaman açık olmalı (kullanım grafiği)")
	}
	// Yalnız CPU: RAM ve G/Ç satırı yok (alanlar birbirinden bağımsız).
	yalnizCPU := DilimIcerik(Havuz{BayiID: 3, CPUCekirdek: 0.5})
	if !strings.Contains(yalnizCPU, "CPUQuota=50%\n") || strings.Contains(yalnizCPU, "MemoryMax") || strings.Contains(yalnizCPU, "IOWeight") {
		t.Errorf("yalnız CPU havuzu:\n%s", yalnizCPU)
	}
	// Ondalık çekirdek yuvarlanır; çok küçük değer 0'a (=sınırsız) DÜŞMEZ.
	if k := (Havuz{CPUCekirdek: 0.004}).CPUKota(); k != 1 {
		t.Errorf("çok küçük CPU kotası = %d (en az 1 beklenir; 0 = sınırsız olurdu)", k)
	}
	if k := (Havuz{CPUCekirdek: 2.345}).CPUKota(); k != 235 && k != 234 {
		t.Errorf("CPUKota(2.345) = %d", k)
	}
	if k := (Havuz{CPUCekirdek: 0}).CPUKota(); k != 0 {
		t.Errorf("CPUKota(0) = %d", k)
	}
}

func TestMemHigh_YuzdeDoksan(t *testing.T) {
	for _, c := range []struct{ ram, bek int64 }{{1000, 900}, {2048, 1843}, {64, 57}, {1, 1}} {
		if got := memHigh(c.ram); got != c.bek {
			t.Errorf("memHigh(%d) = %d, beklenen %d", c.ram, got, c.bek)
		}
	}
}

func TestCanliOzellikArgs_SifirlamaDahil(t *testing.T) {
	got := CanliOzellikArgs(Havuz{CPUCekirdek: 2, RAMMB: 1000, IOAgirlik: 50})
	want := []string{"CPUQuota=200%", "MemoryMax=1000M", "MemoryHigh=900M", "MemorySwapMax=0", "IOWeight=50"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("args = %v", got)
	}
	// >0 → 0 geçişinde canlı limit KALKMALI (boş atama / infinity); yoksa sınırsıza dönüş sessizce uygulanmaz.
	got = CanliOzellikArgs(Havuz{})
	want = []string{"CPUQuota=", "MemoryMax=infinity", "MemoryHigh=infinity", "MemorySwapMax=infinity", "IOWeight="}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sıfırlama args = %v", got)
	}
}

func TestHavuz_AktiflikVeDogrulama(t *testing.T) {
	if (Havuz{}).DilimAktif() || (Havuz{}).DiskAktif() {
		t.Error("boş havuz aktif sayıldı")
	}
	if !(Havuz{IOAgirlik: 1}).DilimAktif() || !(Havuz{RAMMB: 1}).DilimAktif() || !(Havuz{CPUCekirdek: 0.1}).DilimAktif() {
		t.Error("tek alanlı havuz aktif sayılmadı")
	}
	if (Havuz{DiskMB: 5}).DilimAktif() || !(Havuz{DiskMB: 5}).DiskAktif() {
		t.Error("disk havuzu dilimi etkilememeli")
	}
	tamam := []struct {
		ad  string
		cpu float64
		ram int64
		dsk int64
		io  int
	}{
		{"hepsi sınırsız", 0, 0, 0, 0}, {"tipik", 1.5, 2048, 50000, 100}, {"alt sınırlar", 0.1, 64, 1, 1},
		{"üst sınırlar", 1024, EnCokRAMMB, EnCokDiskMB, EnCokIOAgirlik},
	}
	for _, c := range tamam {
		if err := DegerDogrula(c.cpu, c.ram, c.dsk, c.io); err != nil {
			t.Errorf("%s reddedildi: %v", c.ad, err)
		}
	}
	hatali := []struct {
		ad  string
		cpu float64
		ram int64
		dsk int64
		io  int
	}{
		{"negatif cpu", -1, 0, 0, 0}, {"NaN cpu", math.NaN(), 0, 0, 0}, {"+Inf cpu", math.Inf(1), 0, 0, 0},
		{"cpu çok küçük", 0.05, 0, 0, 0}, {"cpu çok büyük", 1025, 0, 0, 0},
		{"negatif ram", 0, -1, 0, 0}, {"ram taban altı", 0, 63, 0, 0}, {"ram tavan üstü", 0, EnCokRAMMB + 1, 0, 0},
		{"negatif disk", 0, 0, -1, 0}, {"disk tavan üstü", 0, 0, EnCokDiskMB + 1, 0},
		{"negatif io", 0, 0, 0, -1}, {"io tavan üstü", 0, 0, 0, EnCokIOAgirlik + 1},
	}
	for _, c := range hatali {
		if err := DegerDogrula(c.cpu, c.ram, c.dsk, c.io); err == nil {
			t.Errorf("%s kabul edildi", c.ad)
		}
	}
}

// Birim dosyası içeriği/yazımı: değişmediyse DOSYAYA DOKUNULMAZ (gereksiz daemon-reload ve mtime
// gürültüsü yok), değiştiyse eski runtime override silinir ve SELinux etiketi yenilenir.
func TestDilimKur_IdempotentVeOverrideTemizler(t *testing.T) {
	o := ortamKur(t)
	h := Havuz{BayiID: 5, CPUCekirdek: 1, RAMMB: 512}
	if d, err := dilimKur(h); err != nil || !d {
		t.Fatalf("ilk yazım: %v %v", d, err)
	}
	yol := filepath.Join(o.Birim, "girginos-bayi5.slice")
	if o.Oku(t, yol) != DilimIcerik(h) {
		t.Error("dosya içeriği beklenenle aynı değil")
	}
	if len(o.Etiket) != 1 || o.Etiket[0] != yol {
		t.Errorf("restorecon çağrıları: %v", o.Etiket)
	}
	if d, err := dilimKur(h); err != nil || d {
		t.Fatalf("aynı içerik yeniden yazıldı: %v %v", d, err)
	}
	if len(o.Etiket) != 1 {
		t.Error("değişmeyen dosyada restorecon çağrıldı")
	}
	// Eski runtime override varken içerik değişirse override silinir (yoksa yeni dosya değerini ezerdi).
	ov := filepath.Join(o.Run, "girginos-bayi5.slice.d")
	if err := os.MkdirAll(ov, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(ov, "50-CPUQuota.conf"), []byte("[Slice]\nCPUQuota=10%\n"), 0o644)
	h.CPUCekirdek = 2
	if d, _ := dilimKur(h); !d {
		t.Fatal("değişen içerik yazılmadı")
	}
	if o.Var(ov) {
		t.Error("eski runtime override silinmedi")
	}
	// Geçersiz kimlik asla dosya yaratmaz.
	if _, err := dilimKur(Havuz{BayiID: 0, RAMMB: 100}); err == nil {
		t.Error("geçersiz bayi kimliği için dilim yazıldı")
	}
}

func TestBekleyenFPM_SliceIleCgroupKarsilastirir(t *testing.T) {
	o := ortamKur(t)
	o.Komut.Yanit = func(satir string) (string, error, bool) {
		if !strings.HasPrefix(satir, "systemctl show") {
			return "", nil, false
		}
		// a: yapılandırma yeni dilimde ama çalışan servis eski cgroup'ta → BEKLEYEN
		// b: servis yeni dilimde → bekleyen değil
		// c: pasif → atlanır, d: Slice/ControlGroup boş → atlanır
		return "Id=php-fpm-c_a.service\nActiveState=active\nSlice=girginos-bayi1-c_a.slice\nControlGroup=/girginos.slice/girginos-c_a.slice/php-fpm-c_a.service\n\n" +
			"Id=php-fpm-c_b.service\nActiveState=active\nSlice=girginos-bayi1-c_b.slice\nControlGroup=/girginos.slice/girginos-bayi1.slice/girginos-bayi1-c_b.slice/php-fpm-c_b.service\n\n" +
			"Id=php-fpm-c_c.service\nActiveState=inactive\nSlice=girginos-bayi1-c_c.slice\nControlGroup=\n\n" +
			"Id=php-fpm-c_d.service\nActiveState=active\nSlice=\nControlGroup=\n", nil, true
	}
	got := BekleyenFPM(context.Background(), []string{"c_a", "c_b", "c_c", "c_d", "c_gecersiz-ad", "../x"})
	if !reflect.DeepEqual(got, []string{"c_a"}) {
		t.Errorf("bekleyenler = %v, beklenen [c_a]", got)
	}
	// Geçersiz sk'ler komut satırına HİÇ girmez.
	for _, l := range o.Komut.Satirlar() {
		if strings.Contains(l, "gecersiz") || strings.Contains(l, "..") {
			t.Errorf("geçersiz sk komuta sızdı: %s", l)
		}
	}
}

func TestBekleyenFPM_Gruplar80lik(t *testing.T) {
	o := ortamKur(t)
	var skler []string
	for i := 0; i < 170; i++ {
		skler = append(skler, "c_h"+strings.Repeat("x", 1)+string(rune('a'+i%26))+string(rune('a'+i/26)))
	}
	o.Komut.Yanit = func(satir string) (string, error, bool) { return "", nil, strings.HasPrefix(satir, "systemctl show") }
	_ = BekleyenFPM(context.Background(), skler)
	if n := o.Komut.Say("systemctl show"); n != 3 {
		t.Errorf("170 hosting için %d systemctl çağrısı (80'lik gruplarla 3 beklenir)", n)
	}
}
