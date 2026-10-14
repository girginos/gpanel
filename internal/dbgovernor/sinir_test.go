package dbgovernor

// sinir_test.go — sınır değer / ölçüm hesabı regresyonları (mutasyon turu 2026-10-13).

import (
	"context"
	"testing"
	"time"
)

// Ölçüm dilimi 300 sn'ye hizalıdır; temizlik ilk turda ve sonra saatte bir çalışır.
func TestDilimHizasiVeTemizlikAraligi(t *testing.T) {
	o := yeniOrtam(t)
	o.taban()
	o.calis(3, hizlar("a_db", 0.3))
	if len(o.depo.dilimler) == 0 {
		t.Fatal("ölçüm satırı yazılmadı")
	}
	for _, d := range o.depo.dilimler {
		if d%300 != 0 {
			t.Fatalf("dilim 300'ün katı değil: %d", d)
		}
	}
	if o.depo.temizSay != 1 {
		t.Fatalf("temizlik ilk turda bir kez: %d", o.depo.temizSay)
	}
	o.calis(200, nil) // 50 dk
	if o.depo.temizSay != 1 {
		t.Fatalf("temizlik saatte bir: %d", o.depo.temizSay)
	}
	o.calis(40, nil) // +10 dk > 1 saat
	if o.depo.temizSay != 2 {
		t.Fatalf("1 saat sonra ikinci temizlik: %d", o.depo.temizSay)
	}
}

// Sayaç sıfırlanması (MariaDB yeniden başladı): sıfırlanan sayaçtaki gerçek CPU yine yakalanır;
// düşüşün kendisi ise kısıt/negatif delta üretmez.
func TestSayacSifirlandiktanSonraGercekCPU(t *testing.T) {
	o := yeniOrtam(t)
	o.olcer.cum["a_db"] = 5000
	o.taban()
	o.olcer.cum["a_db"] = 0 // yeniden başlama: ilk tur sıfırlanan sayaçtaki gerçek 30 CPU-sn
	o.calis(2, hizlar("a_db", 2.0))
	if o.yon.alterSayisi() != 0 {
		t.Fatalf("sıfırlanma tek başına/negatif delta kısıt üretmemeli: %v", o.yon.alterler)
	}
	o.calis(1, hizlar("a_db", 2.0)) // 3. tur: pencerede 90 CPU-sn / 60 = 1,5 çekirdek (sıfırlanma turu DAHİL)
	if !esitInts(o.yon.baglanti("a_db"), []int{2, 2}) {
		t.Fatalf("sıfırlanma sonrası gerçek CPU yakalanmalı: %v", o.yon.baglanti("a_db"))
	}
}

// Fiziksel üst sınır NCPU*dt: tek çekirdekli makinede devasa delta bile eşiği (1,0) aşmaz;
// dört çekirdekte eşik 0,5 iken aynı delta aşar (kırpma sıfıra değil sınıra yapılır).
func TestKirpmaSiniri(t *testing.T) {
	o := yeniOrtam(t)
	o.m.NCPU = 1
	o.taban()
	o.calis(8, hizlar("a_db", 100.0))
	if o.yon.alterSayisi() != 0 {
		t.Fatalf("1 çekirdekte kırpılmış delta eşiği aşmamalı: %v", o.yon.alterler)
	}
	o2 := yeniOrtam(t)
	o2.depo.esik[1] = 0.5
	o2.taban()
	o2.calis(6, hizlar("a_db", 100.0))
	if !esitInts(o2.yon.baglanti("a_db"), []int{2, 2}) {
		t.Fatalf("kırpılmış delta (4 çekirdek) eşik 0,5'i aşmalı: %v", o2.yon.baglanti("a_db"))
	}
}

// Eşit değer eşiği AŞMAZ (kesin büyük): sürekli tam 1,0 çekirdek kısıt üretmez; 1,0'ın hemen üstü üretir.
func TestEsikSinirDegeri(t *testing.T) {
	o := yeniOrtam(t)
	o.taban()
	o.calis(12, hizlar("a_db", 1.0))
	if o.yon.alterSayisi() != 0 {
		t.Fatalf("tam eşik değeri kısıt üretmemeli: %v", o.yon.alterler)
	}
	o.calis(8, hizlar("a_db", 1.2))
	if !esitInts(o.yon.baglanti("a_db"), []int{2, 2}) {
		t.Fatalf("eşiğin üstü kısıt üretmeli: %v", o.yon.baglanti("a_db"))
	}
}

// Plan kancası kısıttan DÜŞÜK bir plan değeri verirse doğrulama onu korur (kısıt değerine yükseltmez).
func TestDogrulaDusukPlaniKorur(t *testing.T) {
	o := yeniOrtam(t)
	o.taban()
	o.calis(4, hizlar("a_db", 2.0))
	v := o.m.baglantiKancasi("a_db", 1) // kısıt 2 iken plan 1
	if v != 1 {
		t.Fatalf("kanca düşük planı geçirmeli: %d", v)
	}
	for _, h := range []string{"localhost", "127.0.0.1"} {
		_ = o.yon.BaglantiAyarla(context.Background(), "a_db", h, v)
	}
	n := o.yon.alterSayisi()
	o.calis(2, hizlar("a_db", 2.0))
	if !esitInts(o.yon.baglanti("a_db"), []int{1, 1}) || o.yon.alterSayisi() != n {
		t.Fatalf("doğrulama düşük planı kısıt değerine yükseltti: %v", o.yon.baglanti("a_db"))
	}
}

func TestOlayKaydiSinirli(t *testing.T) {
	o := yeniOrtam(t)
	for i := 0; i < 520; i++ {
		o.m.olayKaydet(Olay{Tur: "bildirim", Eylem: "x", Detay: time.Duration(i).String()})
	}
	l := o.m.Olaylar()
	if len(l) != 500 || l[0].Detay != time.Duration(20).String() || l[499].Detay != time.Duration(519).String() {
		t.Fatalf("olay kaydı 500 ile sınırlı ve en eskiyi atmalı: %d %q", len(l), l[0].Detay)
	}
}
