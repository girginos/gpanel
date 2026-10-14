package dbgovernor

// kredi_test.go — canlı kredi (uzun deyim çift sayımı) ve uyarlanabilir boşluk eşiği testleri.

import (
	"fmt"
	"testing"
	"time"
)

// uzunDeyim: kullanıcının QueryID'li uzun CPU deyimi (Sure tur başına ilerletilir).
func (o *ortam) uzunDeyim(u string, qid int64, sure time.Duration) {
	o.olcer.mu.Lock()
	defer o.olcer.mu.Unlock()
	o.olcer.calisan = []Sorgu{{Kullanici: u, QueryID: qid, Sure: sure, Durum: "Sending data", Metin: "SELECT BENCHMARK(1e9, MD5('x'))"}}
}

// Tek uzun deyim (130 CPU-sn) çalışırken canlı gösterge ~1 çekirdek sayılır; bitince aynı CPU ikinci kez
// sayılmaz: hiçbir çekirdek sayısında kısıt TETİKLENMEZ.
func TestUzunDeyimCiftSayilmaz(t *testing.T) {
	for _, ncpu := range []float64{4, 8, 16, 64} {
		t.Run(fmt.Sprintf("ncpu%v", ncpu), func(t *testing.T) {
			o := yeniOrtam(t)
			o.m.NCPU = ncpu
			o.taban()
			for i := 0; i < 9; i++ { // 9 tur x 15 sn = 135 sn çalışır; sayaç hareketsiz
				o.uzunDeyim("a_db", 501, time.Duration(5+15*i)*time.Second)
				o.adim(15, nil)
			}
			o.olcer.mu.Lock()
			o.olcer.calisan = nil
			o.olcer.mu.Unlock()
			o.adim(15, hizlar("a_db", 130.0/15)) // deyim bitti: sayaç 130 CPU-sn atladı
			o.calis(6, nil)
			if o.yon.alterSayisi() != 0 || len(o.m.AktifKullanicilar()) != 0 {
				t.Fatalf("NCPU=%v: tek uzun deyim bitişi kısıt tetikledi: %v", ncpu, o.yon.alterler)
			}
			// muhasebe: ölçüm satırına HAM 130 CPU-sn yazıldı
			var topla int64
			for _, s := range o.depo.olcum {
				topla += s.CPUms
			}
			if topla != 130000 {
				t.Fatalf("db_cpu_olcum ham değeri taşımalı (130000 ms): %d", topla)
			}
		})
	}
}

// Atlatma kapalı: canlı görünmeyen sürekli çok bağlantılı kısa sorgu yükü (2 çekirdek) her çekirdek
// sayısında hâlâ kısar; sonsuza dek süren sahte "canlı" deyim kredi biriktirmez (kısa yük yine tam sayılır).
func TestKisaSorguYukuHalaKisar(t *testing.T) {
	for _, ncpu := range []float64{4, 8, 16, 64} {
		o := yeniOrtam(t)
		o.m.NCPU = ncpu
		o.taban()
		o.calis(5, hizlar("a_db", 2.0))
		if !esitInts(o.yon.baglanti("a_db"), []int{2, 2}) {
			t.Fatalf("NCPU=%v: kısa sorgu yükü kısılmadı: %v", ncpu, o.yon.baglanti("a_db"))
		}
	}
	o := yeniOrtam(t)
	o.m.NCPU = 8
	o.taban()
	for i := 0; i < 8; i++ { // bitmeyen sahte canlı deyim (1 çekirdek) + 1,9 çekirdek gerçek kısa sorgu
		o.uzunDeyim("a_db", 777, time.Duration(5+15*i)*time.Second)
		o.adim(15, hizlar("a_db", 1.9))
	}
	if !esitInts(o.yon.baglanti("a_db"), []int{2, 2}) {
		t.Fatalf("sahte canlı deyim gerçek CPU'yu örtmemeli: %v", o.yon.baglanti("a_db"))
	}
}

// Kredi yaşlanır: deyim bittikten sonra sayaç krediOmur turdan geç gelirse kredi düşer, CPU tam sayılır.
func TestKrediYaslanir(t *testing.T) {
	o := yeniOrtam(t)
	o.m.NCPU = 8
	o.taban()
	for i := 0; i < 9; i++ {
		o.uzunDeyim("a_db", 501, time.Duration(5+15*i)*time.Second)
		o.adim(15, nil)
	}
	o.olcer.mu.Lock()
	o.olcer.calisan = nil
	o.olcer.mu.Unlock()
	o.calis(5, nil) // deyim bitti, sayaç gelmedi; kredi süresi doldu
	o.adim(15, hizlar("a_db", 130.0/15))
	o.calis(2, nil)
	if o.yon.alterSayisi() == 0 {
		t.Fatal("süresi dolmuş kredi geç gelen CPU'yu örtmemeli (kredi yalnız bitişe yakın turlar için)")
	}
}

// Yavaş tur (25 sn) + Poll 10 sn: aralık 35 sn olur; pencere her turda sıfırlanmaz, kısıt tetiklenir.
// Gerçek kesinti (5 dk) hâlâ boşluktur: biriken sayaç kısıt üretmez.
func TestYavasTurBoslukDegil(t *testing.T) {
	kur := func(t *testing.T) *ortam {
		o := yeniOrtam(t)
		o.ayar.Poll = 10 * time.Second
		o.olcer.onKos = func() { o.simdi = o.simdi.Add(25 * time.Second) } // her ölçüm 25 sn sürer
		return o
	}
	o := kur(t)
	o.taban()
	for i := 0; i < 5; i++ {
		o.adim(10, hizlar("a_db", 7.0)) // tur başına 70 CPU-sn / 35 sn = 2 çekirdek
	}
	if !esitInts(o.yon.baglanti("a_db"), []int{2, 2}) {
		t.Fatalf("yavaş turlarda pencere sürekli sıfırlandı (kısıt yok): %v", o.yon.baglanti("a_db"))
	}
	for i := 0; i < 3; i++ {
		o.adim(10, hizlar("a_db", 7.0))
	}
	if o.kisitSeviyesi("a_db") < 2 {
		t.Fatalf("yavaş turlarda aşım sürekliliği bozuldu, tırmanma yok (seviye %d)", o.kisitSeviyesi("a_db"))
	}
	o = kur(t)
	o.taban()
	o.adim(300, hizlar("a_db", 20.0)) // 5 dk kesinti, 6000 CPU-sn birikmiş
	o.adim(10, hizlar("a_db", 0.1))
	if o.yon.alterSayisi() != 0 {
		t.Fatalf("5 dk kesinti boşluk sayılmalı: %v", o.yon.alterler)
	}
}
