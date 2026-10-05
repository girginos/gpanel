package uzak

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"girginospanel/internal/backups/hedef"
)

func (o *ortam) iz() (*Izleyici, *sahteSaat) {
	sa := yeniSaat()
	z := o.isci.Izle()
	z.SaatKur(sa.Now)
	z.Calisiyor(true)
	return z, sa
}

func (o *ortam) oku(z *Izleyici) Ilerleme {
	o.t.Helper()
	r, err := IlerlemeOku(context.Background(), o.db, z, true)
	if err != nil {
		o.t.Fatal(err)
	}
	return r
}

func (o *ortam) durumYap(id int64, durum string, ek string) {
	o.t.Helper()
	if _, err := o.db.Exec(`UPDATE backup_uzak_kopyalar SET durum=? `+ek+` WHERE id=?`, durum, id); err != nil {
		o.t.Fatal(err)
	}
}

func TestIlerlemeOku_SayimlarHataIptalDusurVeYenidenDenenecek(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(nil)
	z, _ := o.iz()
	var ids []int64
	for k := 0; k < 6; k++ {
		yid := o.yedek(int64(k+1), fmt.Sprintf("c_d%d", k), "20260825-0101"+fmt.Sprintf("%02d", k), make([]byte, 1000*(k+1)), "oto", nil)
		ids = append(ids, o.kuyrukla(yid))
	}
	// 0,1 bekliyor(hazır) | 2 yeniden denenecek | 3 hata (büyük) | 4 iptal (büyük) | 5 tamam
	o.durumYap(ids[2], "bekliyor", ", deneme=1, sonraki_deneme=NOW() + INTERVAL 1 HOUR")
	o.durumYap(ids[3], "hata", ", boyut_b=900000")
	o.durumYap(ids[4], "iptal", ", boyut_b=900000")
	o.durumYap(ids[5], "tamam", "")
	r := o.oku(z)
	k := r.Kuyruk
	// iptal toplamdan düşer; hata sayılır ama bayt/ETA'ya girmez
	if k.ToplamIs != 5 || k.Tamam != 1 || k.Hata != 1 || k.Bekleyen != 2 || k.YenidenDenenecek != 1 {
		t.Fatalf("sayımlar: %+v", k)
	}
	if k.ToplamBayt != 1000+2000+3000+6000 || k.TamamlananBayt != 6000 || k.KalanBayt != 6000 {
		t.Fatalf("baytlar (hata/iptal düşmeli): %+v", k)
	}
	if k.EtaSn != nil || r.Aktif != nil || !r.AktifMi || r.Tamamlandi {
		t.Fatalf("ölçüm yokken ETA null; aktif iş yok; hazır iş var → aktif_mi: %+v", r)
	}
	if k.Yuzde < 49.9 || k.Yuzde > 50.1 {
		t.Fatalf("yüzde 6000/12000: %v", k.Yuzde)
	}
}

func TestIlerlemeOku_TurMandalVeTamamlandiOzeti(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(nil)
	z, sa := o.iz()
	var ids []int64
	for k := 0; k < 3; k++ {
		yid := o.yedek(int64(k+1), fmt.Sprintf("c_t%d", k), "20260825-010101", make([]byte, 1000), "oto", nil)
		ids = append(ids, o.kuyrukla(yid))
	}
	if r := o.oku(z); r.Kuyruk.ToplamIs != 3 {
		t.Fatalf("tur 3 iş: %+v", r.Kuyruk)
	}
	// iş bitince toplam KÜÇÜLMEZ (tur mandallı), yüzde artar
	o.durumYap(ids[0], "tamam", "")
	o.durumYap(ids[1], "tamam", "")
	r := o.oku(z)
	if r.Kuyruk.ToplamIs != 3 || r.Kuyruk.Tamam != 2 || r.Tamamlandi {
		t.Fatalf("tur küçüldü: %+v", r.Kuyruk)
	}
	o.durumYap(ids[2], "hata", "")
	r = o.oku(z)
	if !r.Tamamlandi || r.Kuyruk.Tamam != 2 || r.Kuyruk.Hata != 1 || r.Kuyruk.Yuzde != 100 || r.AktifMi || r.Kuyruk.EtaSn != nil {
		t.Fatalf("tamamlandı özeti: %+v", r)
	}
	// süre dolunca özet kaybolur
	sa.Ilerle(TamamlandiSuresi + time.Minute)
	if r = o.oku(z); r.Tamamlandi || r.Kuyruk.ToplamIs != 0 || r.Aktif != nil {
		t.Fatalf("özet süresi dolunca boş: %+v", r)
	}
	// yeni iş = YENİ tur (eski tamamlananlar karışmaz); eski işler gerçekte saatler önce kuyruğa alınmıştı
	if _, err := o.db.Exec(`UPDATE backup_uzak_kopyalar SET created_at = NOW() - INTERVAL 1 HOUR`); err != nil {
		t.Fatal(err)
	}
	yid := o.yedek(9, "c_yeni", "20260826-010101", make([]byte, 500), "oto", nil)
	o.kuyrukla(yid)
	if r = o.oku(z); r.Kuyruk.ToplamIs != 1 || r.Kuyruk.Tamam != 0 {
		t.Fatalf("yeni tur: %+v", r.Kuyruk)
	}
}

func TestIlerlemeOku_IsciNilVeAyarKapali(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(nil)
	yid := o.yedek(1, "c_a", "20260825-010101", make([]byte, 100), "oto", nil)
	o.kuyrukla(yid)
	r, err := IlerlemeOku(context.Background(), o.db, nil, true)
	if err != nil || r.AktifMi || r.Aktif != nil {
		t.Fatalf("işçi yoksa aktif_mi false: %+v %v", r, err)
	}
	z, _ := o.iz()
	if r, _ = IlerlemeOku(context.Background(), o.db, z, false); r.AktifMi {
		t.Fatal("ayar kapalıyken aktif_mi false")
	}
}

// adimHedef — Yukle'yi n adıma böler; her adım test tarafından kanalla serbest bırakılır
// (zaman/uyku YOK → kararsız değil). Adım sonunda ack gönderir.
type adimHedef struct {
	*sarmal
	adim chan struct{}
	ack  chan int
	n    int
}

func (a *adimHedef) Yukle(ctx context.Context, yerel, ad string, sec hedef.YukleSecenek) error {
	if !strings.HasSuffix(ad, ".manifest.json") && sec.Ilerleme != nil {
		for k := 1; k <= a.n; k++ {
			select {
			case <-a.adim:
			case <-ctx.Done():
				return ctx.Err()
			}
			sec.Ilerleme(sec.Boyut * int64(k) / int64(a.n))
			a.ack <- k
		}
	}
	return a.sarmal.Yukle(ctx, yerel, ad, sec)
}

// akanIs — yavaş sahte hedefle tek işi akıtır; her adımdan sonra kontrol çağrılır.
func akanIs(t *testing.T, n int, adim time.Duration, kontrol func(k int, r Ilerleme)) (*ortam, int64) {
	t.Helper()
	o := yeniOrtam(t)
	o.ayar(nil)
	z, sa := o.iz()
	ah := &adimHedef{sarmal: o.sar, adim: make(chan struct{}), ack: make(chan int), n: n}
	o.isci.HedefAc = func(context.Context, int64) (hedef.Hedef, error) { return ah, nil }
	yid := o.yedek(1, "c_ornek", "20260825-233538", sahteIcerik(400_000, 3), "oto", nil)
	id := o.kuyrukla(yid)
	bitti := make(chan error, 1)
	go func() { _, err := o.isci.Bir(context.Background()); bitti <- err }()
	for k := 1; k <= n; k++ {
		sa.Ilerle(adim)
		ah.adim <- struct{}{}
		if got := <-ah.ack; got != k {
			t.Fatalf("adım sırası: %d != %d", got, k)
		}
		if k < n { // son adımdan sonra işçi sürer (yarış); önceki adımlarda hedef bir sonraki adımı bekler → durum belirli
			kontrol(k, o.oku(z))
		}
	}
	if err := <-bitti; err != nil {
		t.Fatal(err)
	}
	return o, id
}

func TestAkarkenIlerlemeArtarVeETAHesaplanir(t *testing.T) {
	var onceki int64 = -1
	var onceYuzde float64 = -1
	o, id := akanIs(t, 14, time.Second, func(k int, r Ilerleme) {
		a := r.Aktif
		if a == nil || a.Evre != DurumYukleniyor || a.Domain != "c_ornek.example" || !strings.HasPrefix(a.Dosya, "c_ornek-") {
			t.Fatalf("adım %d aktif: %+v", k, a)
		}
		if a.BaytGonderilen <= onceki || a.IsYuzde <= onceYuzde {
			t.Fatalf("adım %d: ilerleme ARTMALI (%d <= %d, %v <= %v)", k, a.BaytGonderilen, onceki, a.IsYuzde, onceYuzde)
		}
		onceki, onceYuzde = a.BaytGonderilen, a.IsYuzde
		if !r.AktifMi || r.Kuyruk.Yuzde <= 0 {
			t.Fatalf("adım %d: %+v", k, r.Kuyruk)
		}
		switch {
		case k <= 9 && r.Kuyruk.EtaSn != nil:
			t.Fatalf("adım %d (<10 sn): ETA null olmalı", k)
		case k >= 11 && r.Kuyruk.EtaSn == nil:
			t.Fatalf("adım %d: ETA hesaplanmış olmalı", k)
		}
		if k >= 11 && (a.HizBps <= 0 || r.Kuyruk.HizBps != a.HizBps) {
			t.Fatalf("hız ölçülmüş olmalı: %+v", a)
		}
	})
	// iş bitti: kuyruk boş → tamamlandı özeti, çubuk dolu
	z := o.isci.Izle()
	r := o.oku(z)
	if !r.Tamamlandi || r.Kuyruk.Tamam != 1 || r.Kuyruk.Yuzde != 100 || r.Aktif != nil {
		t.Fatalf("bitiş: %+v", r)
	}
	if d := o.is(id); d.durum != DurumTamam {
		t.Fatalf("iş tamam olmalı: %s %s", d.durum, d.sonHata)
	}
}

func TestAkarkenDBYazmaSikligiSinirli(t *testing.T) {
	// 40 adım × 0.5 sn = 20 sn akış; yuklenen_b en çok ~5 sn'de bir yazılır.
	o, _ := akanIs(t, 40, 500*time.Millisecond, func(int, Ilerleme) {})
	n := o.isci.dbYazma.Load()
	// 1 ilk + ~4 (5 sn aralık) + 1 yükleme sonu (+ Bellek.Yukle'nin gerçek geri çağrıları aynı sınırdan geçer)
	if n < 2 || n > 8 {
		t.Fatalf("akarken DB yazma sayısı sınırlı olmalı (40 geri çağrı): %d", n)
	}
}

func TestIlerlemeSirSizdirmaz(t *testing.T) {
	var govde string
	o, _ := akanIs(t, 3, time.Second, func(k int, r Ilerleme) {
		if k == 2 {
			govde = fmt.Sprintf("%+v %+v", r.Aktif, r.Kuyruk)
		}
	})
	for _, yasak := range []string{o.ozel, o.kok, o.gecici, "AGE-SECRET", "gos1:", o.anahtarYol} {
		if yasak != "" && strings.Contains(govde, yasak) {
			t.Fatalf("ilerleme yanıtı yasak içerik taşıyor: %q", yasak)
		}
	}
}
