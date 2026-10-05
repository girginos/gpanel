package uzak

import (
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// sahteSaat — zaman temelli testler KARARSIZ olmasın diye elle ilerletilen saat.
type sahteSaat struct {
	mu sync.Mutex
	t  time.Time
}

func yeniSaat() *sahteSaat { return &sahteSaat{t: time.Date(2026, 10, 5, 3, 0, 0, 0, time.UTC)} }
func (s *sahteSaat) Now() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.t
}
func (s *sahteSaat) Ilerle(d time.Duration) {
	s.mu.Lock()
	s.t = s.t.Add(d)
	s.mu.Unlock()
}

func yeniIz(sa *sahteSaat) *Izleyici {
	z := &Izleyici{}
	z.SaatKur(sa.Now)
	z.Calisiyor(true)
	return z
}

func hesap(z *Izleyici, s Sayim) Ilerleme {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.hesapla(s, true, false)
}

const mb = 1_000_000

func TestHizEMA(t *testing.T) {
	sa := yeniSaat()
	z := yeniIz(sa)
	z.Baslat(1, 1, 1, "a.com", "c_a-1.tar.gz")
	z.Boyut(1000 * mb)
	z.EvreGec(DurumYukleniyor, 1000*mb)
	sa.Ilerle(time.Second)
	z.Bildir(10 * mb) // 10 MB/sn → ilk örnek EMA'yı doğrudan kurar
	if h := z.olc[evreYukle].ema; math.Abs(h-10*mb) > 1 {
		t.Fatalf("ilk örnek: %v", h)
	}
	sa.Ilerle(time.Second)
	z.Bildir(40 * mb) // anlık 30 MB/sn → 0.3*30+0.7*10 = 16
	if h := z.olc[evreYukle].ema; math.Abs(h-16*mb) > 1 {
		t.Fatalf("EMA 16 MB/sn beklenirdi: %v", h)
	}
	// ornekAralik'tan sık bildirim örnek ALMAZ (DB/CPU koruması için ölçüm seyrek)
	sa.Ilerle(200 * time.Millisecond)
	z.Bildir(41 * mb)
	if z.olc[evreYukle].ornek != 2 {
		t.Fatalf("1 sn'den sık bildirim örnek almamalı: %d", z.olc[evreYukle].ornek)
	}
	// ağ durdu: hız bayat → 0 gösterilir (uydurma hız yok)
	sa.Ilerle(30 * time.Second)
	if o := hesap(z, Sayim{ToplamIs: 1, Kosan: 1, ToplamB: 1000 * mb}); o.Kuyruk.HizBps != 0 || o.Aktif.HizBps != 0 {
		t.Fatalf("bayat hız 0 olmalı: %+v", o.Kuyruk)
	}
}

func TestETA_NullDanDegere(t *testing.T) {
	sa := yeniSaat()
	z := yeniIz(sa)
	z.Baslat(1, 1, 1, "a.com", "c_a-1.tar.gz")
	z.Boyut(100 * mb)
	z.EvreGec(DurumYukleniyor, 100*mb)
	s := Sayim{ToplamIs: 1, Kosan: 1, ToplamB: 100 * mb}
	if o := hesap(z, s); o.Kuyruk.EtaSn != nil {
		t.Fatal("hiç örnek yokken ETA null olmalı")
	}
	// 5 MB/sn sabit akış
	var son Ilerleme
	for sn := 1; sn <= 11; sn++ {
		sa.Ilerle(time.Second)
		z.Bildir(int64(sn) * 5 * mb)
		son = hesap(z, s)
		if sn < 10 && son.Kuyruk.EtaSn != nil {
			t.Fatalf("ilk 10 sn'de (sn=%d) ETA null olmalı", sn)
		}
	}
	if son.Kuyruk.EtaSn == nil {
		t.Fatal("11. sn'de ETA hesaplanmış olmalı")
	}
	// kalan: yükleme 45 MB @5 = 9 sn; doğrulama (ölçülmemiş, ağırlık eşit → yükleme hızı) 100 MB @5 = 20 sn
	if *son.Kuyruk.EtaSn != 29 {
		t.Fatalf("ETA 29 sn beklenirdi: %d", *son.Kuyruk.EtaSn)
	}
	// <3 örnek: ayrı izleyici
	z2 := yeniIz(sa)
	z2.Baslat(2, 2, 1, "a.com", "x")
	z2.Boyut(100 * mb)
	z2.EvreGec(DurumYukleniyor, 100*mb)
	sa.Ilerle(30 * time.Second)
	z2.Bildir(10 * mb)
	if o := hesap(z2, s); o.Kuyruk.EtaSn != nil {
		t.Fatal("tek örnekle ETA null olmalı")
	}
}

func TestETA_KuyrukBekleyenlerVeSunucuOzetiHedefi(t *testing.T) {
	sa := yeniSaat()
	z := yeniIz(sa)
	z.DogrulamaIndirir(false) // doğrulama geri indirme yapmıyor → ağırlık 0
	z.Baslat(1, 1, 1, "a.com", "x")
	z.Boyut(300 * mb)
	z.EvreGec(DurumYukleniyor, 300*mb)
	for sn := 1; sn <= 12; sn++ {
		sa.Ilerle(time.Second)
		z.Bildir(int64(sn) * 10 * mb) // 10 MB/sn
	}
	// aktif: 300 MB'ın 120 MB'ı gitti → kalan 180 MB yükleme (18 sn). Bekleyen 200 MB: şifreleme + yükleme. Doğrulama süreye girmez.
	// şifreleme ölçülmemiş → yükleme hızından 10/45 oranıyla türetilir: 200 MB * (0.1s/MB*10/45).
	o := hesap(z, Sayim{ToplamIs: 3, Kosan: 1, Hazir: 2, ToplamB: 300 * mb, HazirB: 200 * mb})
	if o.Kuyruk.EtaSn == nil {
		t.Fatal("ETA beklenirdi")
	}
	beklenen := math.Ceil(180.0/10 + 200.0/10 + 200.0/10*(10.0/45.0))
	if math.Abs(float64(*o.Kuyruk.EtaSn)-beklenen) > 1 {
		t.Fatalf("ETA ~%v beklenirdi: %d", beklenen, *o.Kuyruk.EtaSn)
	}
}

func TestEvreAgirlikliYuzde(t *testing.T) {
	sa := yeniSaat()
	z := yeniIz(sa)
	z.Baslat(1, 1, 1, "a.com", "x")
	z.Boyut(100 * mb)
	s := Sayim{ToplamIs: 1, Kosan: 1, ToplamB: 100 * mb}
	z.Bildir(50 * mb) // şifreleme %50 → 0.10*0.5 = %5
	if o := hesap(z, s); math.Abs(o.Aktif.IsYuzde-5) > 0.05 || o.Aktif.EvreYuzde != 50 || o.Aktif.Evre != DurumSifreleniyor {
		t.Fatalf("şifreleme: %+v", o.Aktif)
	}
	z.EvreGec(DurumYukleniyor, 100*mb)
	z.Bildir(50 * mb) // 10 + 22.5 = 32.5
	if o := hesap(z, s); math.Abs(o.Aktif.IsYuzde-32.5) > 0.05 {
		t.Fatalf("yükleme %%50 → is %%32.5 beklenirdi: %+v", o.Aktif)
	}
	z.EvreGec(DurumDogrulaniyor, 100*mb)
	z.Bildir(100 * mb) // %100 → 10+45+45
	if o := hesap(z, s); math.Abs(o.Aktif.IsYuzde-100) > 0.05 {
		t.Fatalf("doğrulama bitişi %%100: %+v", o.Aktif)
	}
	// kuyruk baytı: aktif işin ağırlıklı payı tamamlanan sayılır
	z.EvreGec(DurumYukleniyor, 100*mb)
	z.Bildir(50 * mb)
	o := hesap(z, Sayim{ToplamIs: 2, Tamam: 1, Kosan: 1, ToplamB: 300 * mb, TamamB: 200 * mb})
	if math.Abs(float64(o.Kuyruk.TamamlananBayt-(200*mb+32_500_000))) > 2 || o.Kuyruk.KalanBayt != 300*mb-o.Kuyruk.TamamlananBayt {
		t.Fatalf("kuyruk baytları: %+v", o.Kuyruk)
	}
	// sunucu özeti veren hedef: ağırlıklar 10/55, 45/55
	z.DogrulamaIndirir(false)
	if o := hesap(z, s); math.Abs(o.Aktif.IsYuzde-(10+22.5)/55*100) > 0.1 {
		t.Fatalf("indirmesiz ağırlık: %+v", o.Aktif)
	}
}

func TestSifirBaytVeBosKuyruk(t *testing.T) {
	sa := yeniSaat()
	z := yeniIz(sa)
	// boş kuyruk: aktif null, ETA null, aktif_mi false
	o := hesap(z, Sayim{})
	if o.Aktif != nil || o.Kuyruk.EtaSn != nil || o.AktifMi || o.Kuyruk.Yuzde != 0 || o.Kuyruk.ToplamIs != 0 {
		t.Fatalf("boş kuyruk: %+v", o)
	}
	// 0 bayt dosya: NaN/Inf/sıfıra bölme yok, yüzde 0-100
	z.Baslat(1, 1, 1, "a.com", "bos.tar.gz")
	z.Boyut(0)
	z.EvreGec(DurumYukleniyor, 0)
	z.Bildir(0)
	o = hesap(z, Sayim{ToplamIs: 1, Kosan: 1})
	for _, v := range []float64{o.Aktif.EvreYuzde, o.Aktif.IsYuzde, o.Kuyruk.Yuzde, o.Kuyruk.HizBps} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 100*mb {
			t.Fatalf("geçersiz sayı: %+v", o)
		}
	}
	if o.Kuyruk.ToplamBayt != 0 || o.Kuyruk.KalanBayt != 0 {
		t.Fatalf("0 bayt kuyruk: %+v", o.Kuyruk)
	}
}

func TestIsciYoksaAktifMiFalse(t *testing.T) {
	sa := yeniSaat()
	z := yeniIz(sa)
	z.Calisiyor(false)
	if o := hesap(z, Sayim{ToplamIs: 2, Hazir: 2}); o.AktifMi {
		t.Fatal("işçi çalışmıyorsa aktif_mi false olmalı")
	}
	z.Calisiyor(true)
	z.mu.Lock()
	o := z.hesapla(Sayim{ToplamIs: 2, Hazir: 2}, false, false) // ayar kapalı
	z.mu.Unlock()
	if o.AktifMi {
		t.Fatal("ayar kapalıysa aktif_mi false olmalı")
	}
	if o := hesap(z, Sayim{ToplamIs: 2, Hazir: 2}); !o.AktifMi {
		t.Fatal("işçi + ayar açık + bekleyen iş varsa aktif_mi true")
	}
	if o := hesap(z, Sayim{ToplamIs: 2, Tamam: 2}); o.AktifMi {
		t.Fatal("iş kalmadıysa aktif_mi false")
	}
}

func TestYazmaIzniSiniri(t *testing.T) {
	sa := yeniSaat()
	z := yeniIz(sa)
	z.Baslat(1, 1, 1, "a", "b")
	n := 0
	for k := 0; k < 100; k++ { // 100 × 0.5 sn = 50 sn
		sa.Ilerle(500 * time.Millisecond)
		if z.YazmaIzni() {
			n++
		}
	}
	if n < 9 || n > 11 {
		t.Fatalf("50 sn'de ~10 yazma izni beklenirdi: %d", n)
	}
}

func TestIndirmeIzleBuyukDosyayiOkur(t *testing.T) {
	d := t.TempDir()
	hedefYol := filepath.Join(d, "uzak-dogrula-x.tmp")
	gecici := filepath.Join(d, ".uzak-dogrula-x.tmp.indiriliyor-abc")
	if err := os.WriteFile(gecici, make([]byte, 1234), 0o600); err != nil {
		t.Fatal(err)
	}
	if n := enBuyukDosya(filepath.Join(d, ".uzak-dogrula-x.tmp.indiriliyor-*")); n != 1234 {
		t.Fatalf("geçici dosya boyutu okunmalı: %d", n)
	}
	if n := enBuyukDosya(filepath.Join(d, ".baska.indiriliyor-*")); n != 0 {
		t.Fatalf("eşleşmeyen: %d", n)
	}
	_ = hedefYol
}
