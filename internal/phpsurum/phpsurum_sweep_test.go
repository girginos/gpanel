package phpsurum

// phpsurum_sweep_test.go — arka-plan dnf süpürücüsünün yük azaltması:
//   - ikilisi diskte olan (kurulu) sürümler dnf'e SORULMAZ,
//   - tur sonucu "her paket KESİN mi" bilgisini döndürür,
//   - sweepLoop bir sonraki turu buna göre (kesin → uzun, belirsiz → 10 dk) zamanlar.

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

// swVarsayilanKurulu: phpKuruluMu'nun ÜRETİM varsayılanı (testler seam'i değiştirmeden ÖNCE yakalanır).
var swVarsayilanKurulu = phpKuruluMu

// swKur: sweep seam'lerini testin süresince sabitler (kurulu: Kod → PHP ikilisi diskte var mı;
// probe: dnf sondası) ve dnf'e sorulan paketlerin kaydını döndürür. Eski değerler t.Cleanup ile
// geri konur. Gerçek arka-plan sweeper'ı ASLA başlatılmaz.
func swKur(t *testing.T, kurulu map[string]bool, probe func(pkg string) (bool, bool)) *[]string {
	t.Helper()
	sweeperOnce.Do(func() {})
	eskiKurulu, eskiProbe := phpKuruluMu, dnfProbe
	t.Cleanup(func() { phpKuruluMu, dnfProbe = eskiKurulu, eskiProbe })
	resetAvailCache()
	phpKuruluMu = func(m SurumMeta) bool { return kurulu[m.Kod] }
	sorulan := &[]string{}
	dnfProbe = func(pkg string) (bool, bool) {
		*sorulan = append(*sorulan, pkg)
		return probe(pkg)
	}
	return sorulan
}

// swRemiPaketler: DesteklenenSurumler'deki tekil Remi paketleri (haric: dışlanan Kod'lar), sıralı.
func swRemiPaketler(haric map[string]bool) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range DesteklenenSurumler {
		if m.Kaynak != "remi" || haric[m.Kod] {
			continue
		}
		pkg := "php" + m.Kod + "-php-fpm"
		if !seen[pkg] {
			seen[pkg] = true
			out = append(out, pkg)
		}
	}
	sort.Strings(out)
	return out
}

// swKod: "php82-php-fpm" → "82".
func swKod(pkg string) string {
	return strings.TrimSuffix(strings.TrimPrefix(pkg, "php"), "-php-fpm")
}

func swCache(pkg string) (deger, var_ bool) {
	availMu.Lock()
	defer availMu.Unlock()
	deger, var_ = availCache[pkg]
	return
}

// Kurulu sürümler dnf'e sorulmaz ve KESİN true yazılır; kalanlar TAM BİR kez sorulur.
func TestSweepKuruluSurumDnfeSorulmaz(t *testing.T) {
	kurulu := map[string]bool{"82": true, "84": true}
	sorulan := swKur(t, kurulu, func(string) (bool, bool) { return false, true }) // dnf: "No match"
	if !sweepOnce() {
		t.Fatal("kalan paketler KESİN yanıt verdi → tur kesin sayılmalı")
	}
	got := append([]string(nil), *sorulan...)
	sort.Strings(got)
	want := swRemiPaketler(kurulu)
	if len(want) == 0 || len(want) == len(swRemiPaketler(nil)) {
		t.Fatalf("test verisi anlamsız: want=%v", want)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dnf'e sorulan paketler: got=%v want=%v (kurulu sürümler sorulmamalı, kalanlar TAM BİR kez)", got, want)
	}
	for _, kod := range []string{"82", "84"} {
		if v, ok := swCache("php" + kod + "-php-fpm"); !ok || !v {
			t.Fatalf("php%s kurulu → cache true olmalı (v=%v ok=%v)", kod, v, ok)
		}
	}
	for _, pkg := range want {
		if v, ok := swCache(pkg); !ok || v {
			t.Fatalf("%s: dnf net 'yok' dedi → cache AÇIKÇA false olmalı (v=%v ok=%v)", pkg, v, ok)
		}
	}
}

// Sürüm sonradan kurulunca eski (kesin) false, dnf'e sorulmadan true'ya döner.
func TestSweepKurulduktanSonraEskiFalseDnfsizTrueOlur(t *testing.T) {
	kurulu := map[string]bool{}
	sorulan := swKur(t, kurulu, func(string) (bool, bool) { return false, true })
	sweepOnce()
	if v, ok := swCache("php85-php-fpm"); !ok || v {
		t.Fatalf("kurulu değilken dnf net yok dedi → false bekleniyordu (v=%v ok=%v)", v, ok)
	}
	kurulu["85"] = true // panel PHP 8.5'i kurdu
	*sorulan = nil
	sweepOnce()
	if v, ok := swCache("php85-php-fpm"); !ok || !v {
		t.Fatalf("kurulu olunca cache true olmalı (v=%v ok=%v)", v, ok)
	}
	for _, p := range *sorulan {
		if p == "php85-php-fpm" {
			t.Fatal("kurulu sürüm için dnf çağrıldı")
		}
	}
}

// sweepOnce'ın dönüşü: "yok" yanıtı da KESİN yanıttır; yalnız SORULAMAYAN paket turu belirsiz yapar.
func TestSweepHepsiKesinDonusu(t *testing.T) {
	tum := swRemiPaketler(nil)
	if len(tum) < 3 {
		t.Fatalf("test verisi: en az 3 Remi paketi bekleniyordu: %v", tum)
	}
	hedef := tum[len(tum)-1]
	hepsiKurulu := map[string]bool{}
	for _, p := range tum {
		hepsiKurulu[swKod(p)] = true
	}
	kesinVar := func(string) (bool, bool) { return true, true }
	kesinYok := func(string) (bool, bool) { return false, true } // dnf net "No match"
	hedefSorulamadi := func(pkg string) (bool, bool) {
		if pkg == hedef {
			return false, false // timeout / kilit
		}
		return true, true
	}
	cases := []struct {
		ad     string
		kurulu map[string]bool
		probe  func(string) (bool, bool) // nil → dnf'e HİÇ sorulmamalı
		kesin  bool
	}{
		{"hepsi kesin var", nil, kesinVar, true},
		{"hepsi kesin yok (No match da KESİN yanıttır)", nil, kesinYok, true},
		{"biri sorulamadı", nil, hedefSorulamadi, false},
		{"sorulamayacak paket zaten kurulu → dnf'e gitmez", map[string]bool{swKod(hedef): true}, hedefSorulamadi, true},
		{"hepsi sorulamadı", nil, func(string) (bool, bool) { return false, false }, false},
		{"hepsi kurulu → dnf hiç çağrılmaz", hepsiKurulu, nil, true},
	}
	for _, c := range cases {
		t.Run(c.ad, func(t *testing.T) {
			probe := c.probe
			if probe == nil {
				probe = func(pkg string) (bool, bool) {
					t.Errorf("dnf'e sorulmamalıydı: %s", pkg)
					return false, false
				}
			}
			swKur(t, c.kurulu, probe)
			if got := sweepOnce(); got != c.kesin {
				t.Fatalf("sweepOnce() = %v, want %v", got, c.kesin)
			}
		})
	}
}

func TestSweepAraligi(t *testing.T) {
	if got := sweepAraligi(true); got != availKesinTTL {
		t.Fatalf("kesin tur sonrası aralık = %v, want %v", got, availKesinTTL)
	}
	if got := sweepAraligi(false); got != availTTL {
		t.Fatalf("belirsiz tur sonrası aralık = %v, want %v", got, availTTL)
	}
	if availKesinTTL <= availTTL {
		t.Fatalf("kesin tur sonrası aralık (%v) yeniden-deneme aralığından (%v) UZUN olmalı", availKesinTTL, availTTL)
	}
	if availTTL > 10*time.Minute {
		t.Fatalf("dnf'e sorulamayan turdan sonra en geç eski periyotta (10 dk) yeniden denenmeli, got %v", availTTL)
	}
}

// sweepLoop gerçekten sweepOnce sonucuna göre bekler: kesin → availKesinTTL, belirsiz → availTTL.
func TestSweepLoopAraligiTurSonucunaGoreSecilir(t *testing.T) {
	tum := swRemiPaketler(nil)
	hedef := tum[len(tum)-1]
	tur := 1 // sweepUyku her çağrıldığında artar (probe ile AYNI goroutine → yarışsız)
	swKur(t, nil, func(pkg string) (bool, bool) {
		if tur == 2 && pkg == hedef {
			return false, false // yalnız 2. turda dnf'e sorulamadı
		}
		return true, true
	})
	var uykular []time.Duration
	eskiUyku := sweepUyku
	t.Cleanup(func() { sweepUyku = eskiUyku })
	sweepUyku = func(d time.Duration) {
		uykular = append(uykular, d)
		tur++
		if len(uykular) == 3 {
			runtime.Goexit() // döngüyü deterministik bitir (defer'ler çalışır)
		}
	}
	bitti := make(chan struct{})
	go func() {
		defer close(bitti)
		sweepLoop()
	}()
	select {
	case <-bitti:
	case <-time.After(10 * time.Second):
		t.Fatal("sweepLoop 3 tur tamamlamadı")
	}
	want := []time.Duration{availKesinTTL, availTTL, availKesinTTL}
	if !reflect.DeepEqual(uykular, want) {
		t.Fatalf("turlar arası bekleme: got=%v want=%v", uykular, want)
	}
}

func TestDosyaVar(t *testing.T) {
	yol := filepath.Join(t.TempDir(), "php")
	if dosyaVar(yol) {
		t.Fatal("olmayan dosya için true")
	}
	if err := os.WriteFile(yol, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !dosyaVar(yol) {
		t.Fatal("var olan dosya için false")
	}
}

// Üretim varsayılanı: diskte olmayan bir Remi sürümü için "kurulu" denmez.
func TestPhpKuruluMuVarsayilanOlmayanSurumuReddeder(t *testing.T) {
	if swVarsayilanKurulu(SurumMeta{Surum: "0.0", Kod: "zz9", Kaynak: "remi"}) {
		t.Fatal("var olmayan Remi sürümü için 'kurulu' denmemeli")
	}
}

// Discover: ikilisi diskte olmayan sürüm "yüklü" değildir (sweepOnce'ın kısayoluyla AYNI dosyaVar tanımı).
func TestDiscoverOlmayanSurumYukluDegil(t *testing.T) {
	sweeperOnce.Do(func() {}) // paketMevcut gerçek sweeper'ı başlatmasın
	resetAvailCache()
	s := Discover(SurumMeta{Surum: "0.0", Kod: "zz9", Kaynak: "remi"})
	if s.Yuklu {
		t.Fatal("ikilisi olmayan sürüm yüklü sayıldı")
	}
	if s.Kurulabilir {
		t.Fatal("boş cache + yüklü değil → kurulabilir=false (henüz bilinmiyor) bekleniyordu")
	}
}
