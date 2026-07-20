package phpsurum

import (
	"sync"
	"sync/atomic"
	"testing"
)

// resetAvailCache: testler arası deterministik başlangıç.
func resetAvailCache() {
	availMu.Lock()
	availCache = map[string]bool{}
	availMu.Unlock()
}

// TestPaketMevcutCacheOnly: istek path'inin (paketMevcut / TumSurumler) ASLA dnf çağırmadığını,
// yalnızca arka-plan sweep'in doldurduğu cache'i okuduğunu ve eşzamanlı erişimin race içermediğini
// doğrular. `go test -race` altında çalıştırılmalı.
func TestPaketMevcutCacheOnly(t *testing.T) {
	// Gerçek arka-plan goroutine'ini başlatMA: sweeperOnce'ı boş fonk. ile tüket → deterministik.
	sweeperOnce.Do(func() {})
	resetAvailCache()

	var probeCalls int64
	old := dnfProbe
	dnfProbe = func(pkg string) (available bool, checked bool) { // dnf'e ASLA gitmeyen sahte prob
		atomic.AddInt64(&probeCalls, 1)
		return pkg == "php82-php-fpm", true // sadece php82 kurulabilir; hepsi KESİN yanıt
	}
	defer func() { dnfProbe = old }()

	// Cache'i elle (senkron) doldur — normalde bunu arka-plan sweeper yapar.
	sweepOnce()

	// Doğruluk: değerler cache'ten okunur.
	if !paketMevcut(SurumMeta{Surum: "8.2", Kod: "82", Kaynak: "remi"}) {
		t.Fatal("php82 kurulabilir bekleniyordu")
	}
	if paketMevcut(SurumMeta{Surum: "8.1", Kod: "81", Kaynak: "remi"}) {
		t.Fatal("php81 kurulamaz bekleniyordu (cache=false)")
	}
	if !paketMevcut(SurumMeta{Surum: "8.3", Kod: "", Kaynak: "appstream"}) {
		t.Fatal("appstream daima mevcut olmalı")
	}

	base := atomic.LoadInt64(&probeCalls)

	// Eşzamanlı istek yükü: cache-only okuma, dnf çağrısı OLMAMALI + race OLMAMALI.
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = paketMevcut(SurumMeta{Surum: "8.2", Kod: "82", Kaynak: "remi"})
			_ = paketMevcut(SurumMeta{Surum: "8.4", Kod: "84", Kaynak: "remi"})
			_ = TumSurumler()
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt64(&probeCalls); got != base {
		t.Fatalf("istek path'i dnf çağırdı: base=%d got=%d (cache-only olmalı)", base, got)
	}
}

// TestBosCacheBloklamaz: cache boşken (ilk boot) istek path'i dnf'e gitmeden hemen makul
// varsayılan (false) döner — asılmaz.
func TestBosCacheBloklamaz(t *testing.T) {
	sweeperOnce.Do(func() {})

	old := dnfProbe
	dnfProbe = func(pkg string) (bool, bool) {
		t.Fatalf("istek path'inde dnf çağrıldı: %s", pkg)
		return false, false
	}
	defer func() { dnfProbe = old }()

	// Cache'i boşalt (ilk-boot simülasyonu).
	resetAvailCache()

	// Boş cache → false, dnf çağrısı YOK (dnfProbe çağrılırsa test patlar).
	if paketMevcut(SurumMeta{Surum: "8.5", Kod: "85", Kaynak: "remi"}) {
		t.Fatal("boş cache'te varsayılan false bekleniyordu")
	}
}

// TestSweepGeciciBasarisizlikSonBilinenIyiyiKorur: (a) transient-fail true->true KORUR.
// Bir tur KESİN true yazar; sonraki tur dnf'e SORULAMAZ (checked=false) → önceki true
// false'a ÇEVRİLMEMELİ. Bu, orijinal yanlış-negatif hatasının tam regresyon testidir:
// geçici dnf kilidi eskiden atomik-wipe ile tüm cache'i false yapıyordu.
func TestSweepGeciciBasarisizlikSonBilinenIyiyiKorur(t *testing.T) {
	sweeperOnce.Do(func() {})
	resetAvailCache()
	old := dnfProbe
	defer func() { dnfProbe = old }()

	// Tur 1: her paket KESİN mevcut (checked=true, available=true).
	dnfProbe = func(pkg string) (bool, bool) { return true, true }
	sweepOnce()
	if !paketMevcut(SurumMeta{Surum: "8.2", Kod: "82", Kaynak: "remi"}) {
		t.Fatal("tur1 sonrası php82 cache=true bekleniyordu")
	}
	if !paketMevcut(SurumMeta{Surum: "8.4", Kod: "84", Kaynak: "remi"}) {
		t.Fatal("tur1 sonrası php84 cache=true bekleniyordu")
	}

	// Tur 2: dnf geçici olarak SORULAMADI (checked=false) — TÜM paketler.
	// Beklenti: önceki true'lar KORUNUR.
	dnfProbe = func(pkg string) (bool, bool) { return false, false }
	sweepOnce()
	if !paketMevcut(SurumMeta{Surum: "8.2", Kod: "82", Kaynak: "remi"}) {
		t.Fatal("geçici dnf başarısızlığı (checked=false) son-bilinen-iyi true'yu false'a ÇEVİRMEMELİ")
	}
	if !paketMevcut(SurumMeta{Surum: "8.4", Kod: "84", Kaynak: "remi"}) {
		t.Fatal("geçici dnf başarısızlığı php84 true'sunu da korumalı")
	}
}

// TestSweepTimeoutYokDegildir: (b) timeout != unavailable.
// Aynı paket için: checked=false (timeout) önceki değeri korur; checked=true+available=false
// ise (dnf KESİN 'No match') değeri false'a çevirir. İki durumun AYRI davranışını kanıtlar.
func TestSweepTimeoutYokDegildir(t *testing.T) {
	sweeperOnce.Do(func() {})
	resetAvailCache()
	old := dnfProbe
	defer func() { dnfProbe = old }()

	// Seed: php81 KESİN mevcut.
	dnfProbe = func(pkg string) (bool, bool) { return true, true }
	sweepOnce()
	if !paketMevcut(SurumMeta{Surum: "8.1", Kod: "81", Kaynak: "remi"}) {
		t.Fatal("seed sonrası php81 true olmalı")
	}

	// Timeout turu (checked=false): 'yok' DEĞİLDİR → true korunmalı.
	dnfProbe = func(pkg string) (bool, bool) { return false, false }
	sweepOnce()
	if !paketMevcut(SurumMeta{Surum: "8.1", Kod: "81", Kaynak: "remi"}) {
		t.Fatal("timeout (checked=false) unavailable DEĞİLDİR; önceki true korunmalı")
	}

	// Confirmed-unavailable turu (checked=true, available=false): ARTIK false olmalı.
	dnfProbe = func(pkg string) (bool, bool) { return false, true }
	sweepOnce()
	if paketMevcut(SurumMeta{Surum: "8.1", Kod: "81", Kaynak: "remi"}) {
		t.Fatal("dnf KESİN 'No match' dediğinde (checked=true) false olmalı")
	}
}

// TestSweepConfirmedUnavailableFalse: (c) confirmed-unavailable hala false.
// Boş cache'ten başlayıp dnf KESİN 'No match' derse cache'te AÇIKÇA false yazılmalı.
func TestSweepConfirmedUnavailableFalse(t *testing.T) {
	sweeperOnce.Do(func() {})
	resetAvailCache()
	old := dnfProbe
	defer func() { dnfProbe = old }()

	dnfProbe = func(pkg string) (bool, bool) { return false, true } // dnf net: paket yok
	sweepOnce()

	if paketMevcut(SurumMeta{Surum: "8.4", Kod: "84", Kaynak: "remi"}) {
		t.Fatal("confirmed-unavailable → paketMevcut false olmalı")
	}
	// Cache'te absent değil, AÇIKÇA false olmalı.
	availMu.Lock()
	v, ok := availCache["php84-php-fpm"]
	availMu.Unlock()
	if !ok {
		t.Fatal("php84 cache'te bulunmalı (confirmed → yazılmalı)")
	}
	if v {
		t.Fatal("php84 cache değeri false olmalı")
	}
}

// TestKurulabilirlikDenetleUcDurum: install-gate'in CANLI otoriter sondasının (dnfCanliProbe)
// üç durumu doğru ayırt ettiğini doğrular — özellikle 'sorulamadı' (checked=false) ASLA
// EOL/yok iması vermez (yanlış-negatif önleme). appstream için canlı sonda hiç çağrılmaz.
func TestKurulabilirlikDenetleUcDurum(t *testing.T) {
	old := dnfCanliProbe
	defer func() { dnfCanliProbe = old }()

	remi81 := SurumMeta{Surum: "8.1", Kod: "81", Kaynak: "remi"}

	// 1) confirmed-unavailable: checked=true & available=false → güvenle EOL denebilir.
	dnfCanliProbe = func(pkg string) (bool, bool) { return false, true }
	if a, c := kurulabilirlikDenetle(remi81); !c || a {
		t.Fatalf("confirmed-unavailable bekleniyordu (checked=true, available=false): a=%v c=%v", a, c)
	}

	// 2) sorulamadı: checked=false → ASLA EOL deme (yanlış-negatif önleme).
	dnfCanliProbe = func(pkg string) (bool, bool) { return false, false }
	if _, c := kurulabilirlikDenetle(remi81); c {
		t.Fatal("dnf'e sorulamadıysa checked=false olmalı (EOL denemez)")
	}

	// 3) available: checked=true & available=true.
	dnfCanliProbe = func(pkg string) (bool, bool) { return true, true }
	if a, c := kurulabilirlikDenetle(remi81); !a || !c {
		t.Fatalf("available bekleniyordu: a=%v c=%v", a, c)
	}

	// 4) appstream: canlı sonda ÇAĞRILMADAN daima (true,true).
	dnfCanliProbe = func(pkg string) (bool, bool) {
		t.Fatal("appstream için canlı dnf sondası çağrılmamalı")
		return false, false
	}
	if a, c := kurulabilirlikDenetle(SurumMeta{Surum: "8.3", Kod: "", Kaynak: "appstream"}); !a || !c {
		t.Fatalf("appstream (true,true) olmalı: a=%v c=%v", a, c)
	}
}
