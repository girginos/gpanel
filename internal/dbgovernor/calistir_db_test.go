package dbgovernor

// calistir_db_test.go — Calistir/YeniMotor/BaglantiKancasi ve ayarOku hata yolu (geçici MariaDB).

import (
	"context"
	"testing"
	"time"
)

// Calistir: panel ömrü döngüsü. Açılışta Kurtar çalışır (önceki açık kısıt geri alınır), genel Motor
// kayıtlıdır (kanca kısıtı görür), bağlam iptalinde döngü temiz biter; db nil → hemen döner.
func TestCalistirKurtarKancaVeIptal(t *testing.T) {
	db := geciciDB(t)
	genel.Store(nil)
	t.Cleanup(func() { genel.Store(nil) })
	Calistir(context.Background(), nil) // db yok: hemen döner

	// önceki panel çalışmasından kalan AÇIK, gözlem (MariaDB'ye dokunmayan) kısıt kaydı
	dbExec(t, db, `INSERT INTO db_cpu_kisit (db_user, domain_id, uygulandi, baslangic_ts, son_asim_ts, acik) VALUES ('kalan_db', 1, 0, 100, 100, 1)`)

	ctx, iptal := context.WithCancel(context.Background())
	bitti := make(chan struct{})
	go func() { Calistir(ctx, db); close(bitti) }()
	var m *Motor
	for i := 0; i < 250 && m == nil; i++ {
		if m = genel.Load(); m == nil {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if m == nil {
		t.Fatal("genel Motor kaydedilmedi")
	}
	// Kurtar kayıt güncellemesini çalıştırsın (açılış kurtarma tamamlanana kadar bekle)
	for i := 0; i < 250 && sayim(t, db, `SELECT COUNT(*) FROM db_cpu_kisit WHERE acik=1`) != 0; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if n := sayim(t, db, `SELECT COUNT(*) FROM db_cpu_kisit WHERE acik IS NULL AND bitis_neden='acilis'`); n != 1 {
		t.Fatalf("açılış kurtarma kalan kısıtı kapatmadı: kapanan=%d", n)
	}

	// kanca: kısıtsız kullanıcıda plan aynen; uygulanmış kısıtta min(plan, kısıt); gözlem kaydında plan aynen
	if BaglantiKancasi("x", 5) != 5 {
		t.Fatal("kısıtsız kullanıcıda plan değişmemeli")
	}
	m.mu.Lock()
	m.aktif["u"] = &Kisit{Kullanici: "u", Uygulandi: true, KisitDeger: 2}
	m.aktif["g"] = &Kisit{Kullanici: "g", Uygulandi: false, KisitDeger: 2}
	m.mu.Unlock()
	for _, c := range []struct {
		u    string
		plan int
		want int
	}{{"u", 10, 2}, {"u", 0, 2}, {"u", 1, 1}, {"u", 2, 2}, {"g", 10, 10}} {
		if got := BaglantiKancasi(c.u, c.plan); got != c.want {
			t.Fatalf("BaglantiKancasi(%s,%d)=%d, beklenen %d", c.u, c.plan, got, c.want)
		}
	}
	m.mu.Lock()
	m.durum.PollSn = 30
	m.mu.Unlock()
	if m.sonAyarPoll() != 30*time.Second {
		t.Fatalf("sonAyarPoll=%v", m.sonAyarPoll())
	}

	iptal()
	select {
	case <-bitti:
	case <-time.After(5 * time.Second):
		t.Fatal("bağlam iptalinde Calistir bitmedi")
	}
}

func TestYeniMotorBagimliliklari(t *testing.T) {
	db := geciciDB(t)
	m := YeniMotor(db)
	if m.DB != db || m.Depo == nil || m.Olcer == nil || m.Yon == nil || m.AyarOku == nil || m.NCPU < 1 || m.UserstatCnf != UserstatCnfYolu {
		t.Fatalf("YeniMotor eksik bağımlılık: %+v", m)
	}
	a, err := m.AyarOku(context.Background())
	if err != nil || a.Mod != ModGozlem {
		t.Fatalf("varsayılan ayar gözlem olmalı: %+v %v", a, err)
	}
}

// ayarOku: panel DB'si okunamıyorsa HATA döner (çağıran yeni kısıt koymaz).
func TestAyarOkuHatasi(t *testing.T) {
	db := geciciDB(t)
	_ = db.Close()
	if _, err := ayarOku(context.Background(), db); err == nil {
		t.Fatal("kapalı DB'de ayarOku hata vermeli")
	}
}
