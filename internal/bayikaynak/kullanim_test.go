package bayikaynak

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// cgroupYaz: sahte cgroup dizini (bellek, cpu.stat, memory.events, io.stat).
func cgroupYaz(t *testing.T, kok, goreli string, ramBayt, cpuUsec, kisitUsec, oom, okuma, yazma int64) {
	t.Helper()
	d := filepath.Join(kok, goreli)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	w := func(ad, ic string) {
		if err := os.WriteFile(filepath.Join(d, ad), []byte(ic), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w("memory.current", fmt.Sprintf("%d\n", ramBayt))
	w("cpu.stat", fmt.Sprintf("usage_usec %d\nuser_usec 1\nsystem_usec 1\nnr_periods 5\nnr_throttled 1\nthrottled_usec %d\n", cpuUsec, kisitUsec))
	w("memory.events", fmt.Sprintf("low 0\nhigh 0\nmax 0\noom 0\noom_kill %d\n", oom))
	w("io.stat", fmt.Sprintf("7777:1 rbytes=%d wbytes=%d rios=1 wios=1 dbytes=0 dios=0\n", okuma, yazma))
}

func TestToplayici_HizlarKaynakVeKapsam(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	seedBayiler(t, db, o)
	bayiEkle(t, db, 9, "bayiz", 1, 1024, 0, 0)
	hostingEkle(t, db, 9, "z.test", "c_z", 9, 5)
	ctx := context.Background()
	t0 := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

	// bayi 1: havuz dilimi var. bayi 9: havuz dilimi YOK ama üyenin eski düz dilimi var → "hosting_toplami".
	cgroupYaz(t, o.Cgroup, "girginos.slice/girginos-bayi1.slice", 100<<20, 1_000_000, 0, 0, 0, 0)
	cgroupYaz(t, o.Cgroup, "girginos.slice/girginos-c_z.slice", 50<<20, 500_000, 0, 0, 0, 0)
	top := &Toplayici{DB: db}
	if err := top.Tek(ctx, t0); err != nil {
		t.Fatal(err)
	}
	// İlk örnek: hız hesaplanamaz (nil), bellek ve sınırlar var.
	n1 := sonNokta(ctx, db, 1)
	if n1 == nil || n1.CPUMilli != nil || n1.RAMMB == nil || *n1.RAMMB != 100 || n1.Kaynak != "havuz" {
		t.Fatalf("bayi 1 ilk örnek: %+v", n1)
	}
	if n1.CPUSinirMilli != 1500 || n1.RAMSinirMB != 2048 {
		t.Errorf("sınırlar cpu=%d ram=%d", n1.CPUSinirMilli, n1.RAMSinirMB)
	}
	if n9 := sonNokta(ctx, db, 9); n9 == nil || n9.Kaynak != "hosting_toplami" || *n9.RAMMB != 50 {
		t.Fatalf("bayi 9 (havuz dilimi yok): %+v", n9)
	}
	// Havuzu hiç olmayan bayi 2 ÖRNEKLENMEZ; yalnız disk havuzu olan bayi 3 diskle örneklenir.
	if say(t, db, `SELECT COUNT(*) FROM bayi_kaynak_kullanim WHERE bayi_id=2`) != 0 {
		t.Error("havuzsuz bayi için satır yazıldı (tablo bayi sayısıyla şişer)")
	}
	n3 := sonNokta(ctx, db, 3)
	if n3 == nil || n3.DiskMB == nil || *n3.DiskMB != 0 /* 100 KB / 1024 */ || n3.Kaynak != "yok" || n3.DiskSinirMB != 1000 {
		t.Fatalf("bayi 3 (yalnız disk): %+v", n3)
	}

	// 5 dk sonra: +30 sn CPU (=100 milli), +2 sn kısma, +1 OOM, +10 MiB okuma, +20 MiB yazma.
	cgroupYaz(t, o.Cgroup, "girginos.slice/girginos-bayi1.slice", 120<<20, 31_000_000, 2_000_000, 1, 10<<20, 20<<20)
	if err := top.Tek(ctx, t0.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	n := sonNokta(ctx, db, 1)
	if n.CPUMilli == nil || *n.CPUMilli != 100 || *n.KisitlamaMS != 2000 || *n.OOM != 1 || *n.RAMMB != 120 {
		t.Fatalf("hızlar yanlış: %+v", n)
	}
	if n.IOOkuKBs == nil || *n.IOOkuKBs != 34 || *n.IOYazKBs != 68 {
		t.Errorf("g/ç hızları: %v %v (34/68 KB/s beklenir)", n.IOOkuKBs, n.IOYazKBs)
	}
	if say(t, db, `SELECT COUNT(*) FROM bayi_kaynak_kullanim WHERE bayi_id=1`) != 2 {
		t.Error("iki örnek iki satır olmalı")
	}
	// Aynı 5 dk dilimi içinde tekrar yazılırsa SATIR ÇOĞALMAZ (ON DUPLICATE KEY UPDATE).
	if err := top.Tek(ctx, t0.Add(5*time.Minute+30*time.Second)); err != nil {
		t.Fatal(err)
	}
	if say(t, db, `SELECT COUNT(*) FROM bayi_kaynak_kullanim WHERE bayi_id=1`) != 2 {
		t.Error("aynı dilimde satır çoğaldı")
	}
	// Sayaç geri gitti (dilim yeniden başladı): HIZ yazılmaz (negatif değer asla).
	cgroupYaz(t, o.Cgroup, "girginos.slice/girginos-bayi1.slice", 10<<20, 100, 0, 0, 0, 0)
	if err := top.Tek(ctx, t0.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if n := sonNokta(ctx, db, 1); n.CPUMilli != nil {
		t.Errorf("sayaç sıfırlanmasında hız yazıldı: %+v", n)
	}
	// Bayi havuzu kaldırılırsa örnekleme DURUR.
	if _, err := db.Exec(`UPDATE users SET kaynak_cpu_cekirdek=0, kaynak_ram_mb=0 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	once := say(t, db, `SELECT COUNT(*) FROM bayi_kaynak_kullanim WHERE bayi_id=1`)
	if err := top.Tek(ctx, t0.Add(15*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if say(t, db, `SELECT COUNT(*) FROM bayi_kaynak_kullanim WHERE bayi_id=1`) != once {
		t.Error("havuzu kalkan bayi örneklenmeye devam etti")
	}
}

func TestSeri_KovalamaKapsamVeBudama(t *testing.T) {
	o := ortamKur(t)
	_ = o
	db := geciciDB(t)
	bayiEkle(t, db, 1, "bayia", 1, 1024, 0, 0)
	bayiEkle(t, db, 2, "bayib", 1, 1024, 0, 0)
	ctx := context.Background()
	simdi := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	ip := func(v int) *int { return &v }
	yaz := func(bayi int64, dk int, cpu, ram, kisit, oom int) {
		ts := simdi.Add(-time.Duration(dk) * time.Minute)
		d := int64(ram * 2)
		if err := noktaYaz(ctx, db, bayi, ts, Nokta{CPUMilli: ip(cpu), CPUSinirMilli: 1000, KisitlamaMS: ip(kisit), RAMMB: ip(ram), RAMSinirMB: 1024, OOM: ip(oom), DiskMB: &d, DiskSinirMB: 5000, Kaynak: "havuz"}); err != nil {
			t.Fatal(err)
		}
	}
	// Bayi 1: aynı 30 dk kovasında 3 örnek (11:30-11:59 → 11:30 kovası); bayi 2: tamamen başka değerler.
	yaz(1, 25, 100, 10, 5, 0)
	yaz(1, 20, 200, 30, 10, 1)
	yaz(1, 15, 300, 20, 0, 0)
	yaz(2, 20, 999, 999, 999, 9)
	// Eski satırlar: 8 gün önce (7g aralığı dışında), 40 gün önce (saklama dışında).
	yaz(1, 8*24*60, 1, 1, 1, 0)
	yaz(1, 40*24*60, 1, 1, 1, 0)

	s24, err := Seri(ctx, db, 1, "24s", simdi)
	if err != nil || len(s24) != 3 {
		t.Fatalf("24s serisi: %d nokta, %v (3 beklenir; 5 dk çözünürlük)", len(s24), err)
	}
	s7, err := Seri(ctx, db, 1, "7g", simdi)
	if err != nil {
		t.Fatal(err)
	}
	// 7g → 30 dk kova: 3 örnek 2 kovaya (11:30 ve 11:30'a düşen) — 11:35, 11:40, 11:45 hepsi 11:30 kovasında.
	if len(s7) != 1 {
		t.Fatalf("7g serisi %d nokta (1 kova beklenir): %+v", len(s7), s7)
	}
	k := s7[0]
	if *k.CPUMilli != 200 || *k.RAMMB != 30 || *k.KisitlamaMS != 15 || *k.OOM != 1 || *k.DiskMB != 60 {
		t.Errorf("kova toplulaştırması: cpu=%d ram=%d kisit=%d oom=%d disk=%d (200/30 en yüksek/15 toplam/1 toplam/60 en yüksek)", *k.CPUMilli, *k.RAMMB, *k.KisitlamaMS, *k.OOM, *k.DiskMB)
	}
	if k.T != "2026-10-12T11:30:00Z" {
		t.Errorf("kova zamanı %s", k.T)
	}
	// KAPSAM: başka bayinin değeri ASLA sızmaz.
	s2, _ := Seri(ctx, db, 2, "7g", simdi)
	if len(s2) != 1 || *s2[0].CPUMilli != 999 {
		t.Errorf("bayi 2 serisi: %+v", s2)
	}
	if bos, _ := Seri(ctx, db, 77, "30g", simdi); len(bos) != 0 {
		t.Errorf("olmayan bayi serisi boş değil: %+v", bos)
	}
	// 30g: 8 günlük satır içeride, 40 günlük dışarıda.
	s30, _ := Seri(ctx, db, 1, "30g", simdi)
	if len(s30) != 2 {
		t.Errorf("30g serisi %d nokta (2 beklenir: 8 gün önceki + yakın kova)", len(s30))
	}
	if _, err := Seri(ctx, db, 1, "1y", simdi); err == nil {
		t.Error("geçersiz aralık kabul edildi")
	}
	if !AralikGecerli("24s") || AralikGecerli("") || AralikGecerli("24h") {
		t.Error("AralikGecerli")
	}
	// Budama: 30 günden eski satırlar silinir, kalanlar durur.
	sil, err := Buda(ctx, db, simdi)
	if err != nil || sil != 1 {
		t.Fatalf("Buda: %d %v (1 beklenir)", sil, err)
	}
	if say(t, db, `SELECT COUNT(*) FROM bayi_kaynak_kullanim`) != 5 {
		t.Error("budama yanlış satırları sildi")
	}
}

// Migration: iki kez uygulanabilir, mevcut bayi satırları 0 (= sınırsız) ile gelir, bayi silinince seri CASCADE gider.
func TestGoc0219_IdempotentSifirVarsayilanVeCascade(t *testing.T) {
	ortamKur(t)
	db := geciciDB(t) // göç bir kez uygulandı
	if _, err := db.Exec(`INSERT INTO users (id, username, role, max_disk_mb) VALUES (5, 'eskibayi', 'reseller', 4000)`); err != nil {
		t.Fatal(err)
	}
	migrasyonUygula(t, db) // ikinci uygulama: hata vermemeli
	var cpu float64
	var ram int64
	var io int
	if err := db.QueryRow(`SELECT kaynak_cpu_cekirdek, kaynak_ram_mb, kaynak_io_agirlik FROM users WHERE id=5`).Scan(&cpu, &ram, &io); err != nil {
		t.Fatal(err)
	}
	if cpu != 0 || ram != 0 || io != 0 {
		t.Errorf("mevcut bayi sınırsız (0) gelmeliydi: %v %v %v", cpu, ram, io)
	}
	// Mevcut bayi etkilenmez: disk havuzu var ama CPU/RAM yok → dilim yok.
	hv, err := HavuzlariOku(context.Background(), db, 5)
	if err != nil || hv[5].DilimAktif() || !hv[5].DiskAktif() || hv[5].DiskMB != 4000 {
		t.Errorf("mevcut bayi havuzu: %+v %v", hv[5], err)
	}
	if err := noktaYaz(context.Background(), db, 5, simdiSabit(), Nokta{Kaynak: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM users WHERE id=5`); err != nil {
		t.Fatal(err)
	}
	if say(t, db, `SELECT COUNT(*) FROM bayi_kaynak_kullanim WHERE bayi_id=5`) != 0 {
		t.Error("bayi silinince kullanım serisi CASCADE ile gitmedi")
	}
	// Olmayan bayiye satır yazılamaz (FK).
	if err := noktaYaz(context.Background(), db, 6, simdiSabit(), Nokta{Kaynak: "x"}); err == nil {
		t.Error("var olmayan bayi için kullanım satırı yazıldı")
	}
}
