package bayikaynak

import (
	"context"
	"testing"
	"time"
)

// Aralık pencereleri TAM sınırlarıyla: 24s / 7g / 30g dışındaki noktalar seriye girmez.
func TestSeri_PencereSinirlari(t *testing.T) {
	ortamKur(t)
	db := geciciDB(t)
	bayiEkle(t, db, 1, "bayia", 1, 1024, 0, 0)
	ctx := context.Background()
	simdi := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	ip := func(v int) *int { return &v }
	dk := func(gun, saat int) int { return gun*24*60 + saat*60 }
	for _, m := range []int{
		dk(0, 23), // 24s içinde
		dk(1, 1),  // 24s dışı, 7g içinde
		dk(6, 22), // 7g içinde
		dk(7, 1),  // 7g dışı, 30g içinde
		dk(29, 6), // 30g içinde (29 günlük pencerede olmazdı)
		dk(30, 2), // 30g dışı
	} {
		if err := noktaYaz(ctx, db, 1, simdi.Add(-time.Duration(m)*time.Minute), Nokta{CPUMilli: ip(10), RAMMB: ip(10), Kaynak: "havuz"}); err != nil {
			t.Fatal(err)
		}
	}
	for aralik, bek := range map[string]int{"24s": 1, "7g": 3, "30g": 5} {
		s, err := Seri(ctx, db, 1, aralik, simdi)
		if err != nil || len(s) != bek {
			t.Errorf("%s serisi %d nokta, %v (%d beklenir)", aralik, len(s), err, bek)
		}
	}
}

// Ölçülecek bir şey olmayan bayi için satır yazılmaz: CPU/RAM havuzu var ama cgroup verisi de
// disk havuzu da yok (henüz çalışan hosting yok) → tablo şişmez.
func TestToplayici_OlculecekSeyYoksaSatirYazilmaz(t *testing.T) {
	ortamKur(t)
	db := geciciDB(t)
	bayiEkle(t, db, 1, "bayia", 1, 1024, 0, 0)
	top := &Toplayici{DB: db}
	if err := top.Tek(context.Background(), time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if n := say(t, db, `SELECT COUNT(*) FROM bayi_kaynak_kullanim`); n != 0 {
		t.Errorf("ölçülecek veri yokken %d satır yazıldı", n)
	}
}

// Örnekleyici eski satırları kendiliğinden budar (6 saatte bir): tablo sınırsız büyümez.
func TestToplayici_EskiSatirlariBudar(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	bayiEkle(t, db, 1, "bayia", 1, 1024, 0, 0)
	ctx := context.Background()
	simdi := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	cgroupYaz(t, o.Cgroup, "girginos.slice/girginos-bayi1.slice", 10<<20, 1000, 0, 0, 0, 0)
	if err := noktaYaz(ctx, db, 1, simdi.Add(-Saklama-time.Hour), Nokta{Kaynak: "havuz"}); err != nil {
		t.Fatal(err)
	}
	top := &Toplayici{DB: db}
	if err := top.Tek(ctx, simdi); err != nil {
		t.Fatal(err)
	}
	if n := say(t, db, `SELECT COUNT(*) FROM bayi_kaynak_kullanim WHERE ts < ?`, simdi.Add(-Saklama).Format("2006-01-02 15:04:05")); n != 0 {
		t.Errorf("saklama süresini aşan %d satır silinmedi", n)
	}
	// 6 saat dolmadan ikinci tur budamaz (kilit süresi/yük): yeni eski satır kalır.
	if err := noktaYaz(ctx, db, 1, simdi.Add(-Saklama-2*time.Hour), Nokta{Kaynak: "havuz"}); err != nil {
		t.Fatal(err)
	}
	if err := top.Tek(ctx, simdi.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if n := say(t, db, `SELECT COUNT(*) FROM bayi_kaynak_kullanim WHERE ts < ?`, simdi.Add(-Saklama).Format("2006-01-02 15:04:05")); n != 1 {
		t.Errorf("budama 6 saat dolmadan yeniden çalıştı (kalan eski satır %d)", n)
	}
}

// Birikimli sayaç geri giderse (dilim yeniden başladı) o hız YAZILMAZ: negatif değer asla üretilmez.
func TestHizlar_SayacGeriGidinceNilVeIOKosullari(t *testing.T) {
	t0 := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	a := ham{zaman: t0, ioVar: true, ioR: 100 << 20, ioW: 10 << 20, cpuVar: true, cpuUsec: 1000}
	// Okuma sayacı geri gitti, yazma ilerledi: G/Ç hızları TAMAMEN yazılmaz.
	b := ham{zaman: t0.Add(5 * time.Minute), ioVar: true, ioR: 50 << 20, ioW: 20 << 20, cpuVar: true, cpuUsec: 2000}
	if _, _, _, r, w := hizlar(a, b); r != nil || w != nil {
		t.Errorf("okuma sayacı geri giderken G/Ç hızı yazıldı: %v %v", r, w)
	}
	// Yazma sayacı geri gitti, okuma ilerledi.
	b = ham{zaman: t0.Add(5 * time.Minute), ioVar: true, ioR: 200 << 20, ioW: 5 << 20, cpuVar: true, cpuUsec: 2000}
	if _, _, _, r, w := hizlar(a, b); r != nil || w != nil {
		t.Errorf("yazma sayacı geri giderken G/Ç hızı yazıldı: %v %v", r, w)
	}
	// Normal artış: hız yazılır.
	b = ham{zaman: t0.Add(5 * time.Minute), ioVar: true, ioR: 200 << 20, ioW: 40 << 20, cpuVar: true, cpuUsec: 2000}
	if _, _, _, r, w := hizlar(a, b); r == nil || w == nil || *r <= 0 || *w <= 0 {
		t.Errorf("normal G/Ç artışında hız yok: %v %v", r, w)
	}
	// Zaman geri/aynı: hiçbir hız yok.
	if c, k, o, r, w := hizlar(a, ham{zaman: t0, ioVar: true, cpuVar: true}); c != nil || k != nil || o != nil || r != nil || w != nil {
		t.Error("sıfır süreli aralıkta hız üretildi")
	}
}

// Üyenin eski düz dilimi YALNIZ henüz havuza girmemişse sayılır; havuz diliminin içine taşınmış
// üye iki kez sayılmaz (havuz dilimi zaten onu kapsar).
func TestBayiOrnekle_HavuzIcindekiUyeIkiKezSayilmaz(t *testing.T) {
	o := ortamKur(t)
	cgroupYaz(t, o.Cgroup, "girginos.slice/girginos-bayi1.slice", 100<<20, 1000, 0, 0, 0, 0)
	cgroupYaz(t, o.Cgroup, "girginos.slice/girginos-bayi1.slice/girginos-bayi1-c_a.slice", 40<<20, 400, 0, 0, 0, 0)
	cgroupYaz(t, o.Cgroup, "girginos.slice/girginos-c_a.slice", 5<<20, 50, 0, 0, 0, 0)   // artık düz dilim (aynı üye)
	cgroupYaz(t, o.Cgroup, "girginos.slice/girginos-c_b.slice", 10<<20, 100, 0, 0, 0, 0) // henüz taşınmamış üye
	h, kaynak, ok := bayiOrnekle(1, []Uye{{SK: "c_a", BayiID: 1}, {SK: "c_b", BayiID: 1}})
	if !ok || kaynak != "havuz" {
		t.Fatalf("kaynak=%q ok=%v", kaynak, ok)
	}
	if got := h.ramBayt >> 20; got != 110 {
		t.Errorf("toplam RAM %d MB (110 beklenir: havuz 100 + taşınmamış c_b 10; c_a iki kez sayılmamalı)", got)
	}
	// Havuz dilimi hiç yoksa üyelerin düz dilimleri toplanır.
	_, k2, ok2 := bayiOrnekle(7, []Uye{{SK: "c_b", BayiID: 7}})
	if !ok2 || k2 != "hosting_toplami" {
		t.Errorf("havuzsuz bayi: %q %v", k2, ok2)
	}
}

// Seri hiç satır yokken NIL değil boş dilim döner (JSON'da null yerine []).
func TestSeri_BosSonucNilDegil(t *testing.T) {
	ortamKur(t)
	db := geciciDB(t)
	bayiEkle(t, db, 1, "bayia", 1, 1024, 0, 0)
	s, err := Seri(context.Background(), db, 1, "24s", time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC))
	if err != nil || s == nil || len(s) != 0 {
		t.Errorf("boş seri: %#v %v", s, err)
	}
}
