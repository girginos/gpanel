package dbgovernor

// inceleme_test.go — kod düzeyi inceleme turunda (2026-10-13) bulunan hataların regresyon testleri
// ve eşzamanlılık/yarış senaryoları. Hepsi sahte Olcer/Yonetici/Depo dikişleriyle; gerçek MariaDB yok.

import (
	"context"
	"sync"
	"testing"
)

// Tırmanma KESİNTİSİZ aşım ister: kısıt soğuma beklerken (5 dk) gelen yeni bir sıçrama, kısıtın
// başlangıcından beri geçen süreye bakılarak doğrudan 2. seviyeye (KILL QUERY) ya da 3. seviyeye
// (KILL CONNECTION) tırmandırılmamalı.
func TestTirmanmaArdisikAsimGerektirir(t *testing.T) {
	t.Run("1-2", func(t *testing.T) {
		o := yeniOrtam(t)
		o.taban()
		o.calis(3, hizlar("a_db", 2.0)) // kısıt (seviye 1), aşım 15 sn sürdü
		o.calis(17, nil)                // 255 sn sakin: kısıt soğuma bekliyor (5 dk)
		if o.kisitSeviyesi("a_db") != 1 {
			t.Fatalf("hazırlık: kısıt seviye 1 ve canlı olmalı: %d", o.kisitSeviyesi("a_db"))
		}
		o.calis(3, hizlar("a_db", 2.0)) // yeni sıçrama: ilk aşım turu
		if s := o.kisitSeviyesi("a_db"); s != 1 {
			t.Fatalf("yeni sıçrama ilk turda 2. seviyeye tırmandı (kısıt başlangıcına göre): seviye=%d", s)
		}
		o.calis(4, hizlar("a_db", 2.0)) // kesintisiz 45 sn aşım → 2. seviye
		if s := o.kisitSeviyesi("a_db"); s != 2 {
			t.Fatalf("kesintisiz aşım 2. seviyeye çıkarmalı: seviye=%d", s)
		}
	})
	t.Run("2-3", func(t *testing.T) {
		o := yeniOrtam(t)
		for i := 0; i < 3; i++ {
			_ = o.yon.baglan("a_db")
		}
		o.taban()
		o.calis(7, hizlar("a_db", 2.0)) // seviye 2
		if o.kisitSeviyesi("a_db") != 2 {
			t.Fatalf("hazırlık: seviye 2 bekleniyordu: %d", o.kisitSeviyesi("a_db"))
		}
		o.calis(14, nil) // 210 sn sakin (< 5 dk soğuma): kısıt sürüyor
		if o.kisitSeviyesi("a_db") != 2 {
			t.Fatalf("hazırlık: kısıt seviye 2 ve canlı olmalı: %d", o.kisitSeviyesi("a_db"))
		}
		o.calis(3, hizlar("a_db", 2.0)) // yeni sıçrama
		if len(o.yon.kesilenler) != 0 || o.kisitSeviyesi("a_db") != 2 {
			t.Fatalf("yeni sıçrama bağlantıları KESTİ (kesintisiz 3 dk aşım yok): seviye=%d kesilen=%v", o.kisitSeviyesi("a_db"), o.yon.kesilenler)
		}
		o.calis(13, hizlar("a_db", 2.0)) // kesintisiz ~3 dk aşım → 3. seviye
		if k, ok := o.m.AktifKisit("a_db"); !ok || k.Seviye != 3 || len(o.yon.kesilenler) != 3 {
			t.Fatalf("kesintisiz aşım 3. seviyeye çıkarmalı: %+v kesilen=%v", k, o.yon.kesilenler)
		}
	})
}

// Kısıt kaydı kapatılamazsa (panel DB hıçkırığı) kısıt bellekte AÇIK kalır ve yeniden denenir:
// aksi halde kayıt açık kalır, UNIQUE(db_user, acik) o kullanıcıya yeni kısıt yazılmasını engeller
// ve panel yeniden başlayınca bayat önceki_baglanti geri yazılır. Bu sırada kısıt yeniden
// doğrulanmaz (ALTER sallanması yok) ve yeniden kısılmaz.
func TestKayitKapanamazsaKisitAcikKalir(t *testing.T) {
	o := yeniOrtam(t)
	o.taban()
	o.calis(4, hizlar("a_db", 2.0))
	if !esitInts(o.yon.baglanti("a_db"), []int{2, 2}) {
		t.Fatalf("hazırlık: kısıt yok: %v", o.yon.baglanti("a_db"))
	}
	o.depo.mu.Lock()
	o.depo.kapatHata = errSahte
	o.depo.mu.Unlock()
	o.calis(30, nil) // soğuma doldu; geri yazma yapıldı ama kayıt kapanamıyor
	if !esitInts(o.yon.baglanti("a_db"), []int{0, 0}) {
		t.Fatalf("MAX_USER_CONNECTIONS geri yazılmalıydı: %v", o.yon.baglanti("a_db"))
	}
	if len(o.m.AktifKullanicilar()) != 1 || o.depo.acikSayisi() != 1 {
		t.Fatalf("kayıt kapanamayan kısıt bellekte açık kalmalı: aktif=%v acik=%d", o.m.AktifKullanicilar(), o.depo.acikSayisi())
	}
	n := o.yon.alterSayisi()
	o.calis(4, nil)
	if o.yon.alterSayisi() != n {
		t.Fatalf("kayıt kapanmayı beklerken kısıt yeniden uygulandı (ALTER sallanması): %v", o.yon.alterler[n:])
	}
	o.calis(4, hizlar("a_db", 3.0)) // bu sırada aşım yeniden başlasa da kapanmayı bekleyen kısıt yeniden kurulmaz
	if !esitInts(o.yon.baglanti("a_db"), []int{0, 0}) || o.depo.acikSayisi() != 1 {
		t.Fatalf("kapanış beklerken yeniden kısıldı: %v acik=%d", o.yon.baglanti("a_db"), o.depo.acikSayisi())
	}
	o.depo.mu.Lock()
	o.depo.kapatHata = nil
	o.depo.mu.Unlock()
	o.calis(1, hizlar("a_db", 3.0))
	if o.depo.acikSayisi() != 0 {
		t.Fatalf("hata kalkınca kayıt kapanmalı: acik=%d", o.depo.acikSayisi())
	}
	if nd := o.depo.bitisNedenleri(); len(nd) != 1 || nd[0] != "soguma" {
		t.Fatalf("bitiş nedeni soguma olmalı: %v", nd)
	}
}

// kancaliYon: kısıt bırakılırken tam geri yazma sırasında kaynaklimit'in plan yeniden uygulamasını
// (BaglantiKancasi → ALTER) benzetir.
type kancaliYon struct {
	*sahteYon
	tetik func()
}

func (k *kancaliYon) BaglantiAyarla(ctx context.Context, u, host string, n int) error {
	if f := k.tetik; f != nil {
		k.tetik = nil
		f()
	}
	return k.sahteYon.BaglantiAyarla(ctx, u, host, n)
}

// Kısıt bırakılırken gelen plan yeniden uygulaması kalıcı düşük sınır (kısıt değeri) bırakmamalı:
// son durum yeni plan değeri olmalı.
func TestBirakirkenPlanYenidenUygulamasi(t *testing.T) {
	o := yeniOrtam(t)
	o.depo.plan[1] = 10 // planlı hosting
	ky := &kancaliYon{sahteYon: o.yon}
	o.m.Yon = ky
	o.taban()
	o.calis(4, hizlar("a_db", 2.0))
	if !esitInts(o.yon.baglanti("a_db"), []int{2, 2}) {
		t.Fatalf("hazırlık: kısıt yok: %v", o.yon.baglanti("a_db"))
	}
	ky.tetik = func() {
		o.depo.mu.Lock()
		o.depo.plan[1] = 20
		o.depo.mu.Unlock()
		v := o.m.baglantiKancasi("a_db", 20) // kaynaklimit.MySQLLimitUygula: tüm hostlara kancadan gelen değer
		for _, h := range []string{"localhost", "127.0.0.1"} {
			_ = o.yon.BaglantiAyarla(context.Background(), "a_db", h, v)
		}
	}
	o.calis(30, nil)
	if len(o.m.AktifKullanicilar()) != 0 {
		t.Fatalf("kısıt kalkmalıydı: %v", o.m.AktifKullanicilar())
	}
	if !esitInts(o.yon.baglanti("a_db"), []int{20, 20}) {
		t.Fatalf("bırakma sırasındaki plan değişikliği ezildi (beklenen 20,20): %v", o.yon.baglanti("a_db"))
	}
}

// Kısıt sürerken hesap silinirse (DROP USER / hosting silme) kısıt temizlenir; var olmayan hesaba
// ALTER yazılmaz.
func TestKisitSurerkenHesapSilinir(t *testing.T) {
	o := yeniOrtam(t)
	o.taban()
	o.calis(4, hizlar("a_db", 2.0))
	n := o.yon.alterSayisi()
	o.yon.mu.Lock()
	delete(o.yon.hesap, "a_db")
	o.yon.mu.Unlock()
	o.depo.mu.Lock()
	o.depo.kimlik["a_db"] = Kimlik{Yok: true}
	o.depo.mu.Unlock()
	o.calis(6, hizlar("a_db", 2.0)) // kimlik önbelleği (60 sn) dolunca "silindi"
	if len(o.m.AktifKullanicilar()) != 0 || o.depo.acikSayisi() != 0 {
		t.Fatalf("silinen hesabın kısıtı temizlenmeli: aktif=%v acik=%d", o.m.AktifKullanicilar(), o.depo.acikSayisi())
	}
	if o.yon.alterSayisi() != n {
		t.Fatalf("silinen hesaba ALTER yazıldı: %v", o.yon.alterler[n:])
	}
	if nd := o.depo.bitisNedenleri(); len(nd) != 1 || nd[0] != "silindi" {
		t.Fatalf("bitiş nedeni silindi olmalı: %v", nd)
	}
}

// Eşzamanlılık: Tick tek goroutine; BaglantiKancasi/Durum/AktifKisit/Olaylar başka goroutine'lerden.
// `go test -race` altında veri yarışı olmamalı; mod döngüsü (kapalı/gözlem/uygula) de dönmeli.
func TestEszamanliTurKancaDurumYaris(t *testing.T) {
	o := yeniOrtam(t)
	o.depo.plan[1] = 10
	o.taban()
	dur := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for {
				select {
				case <-dur:
					return
				default:
				}
				_ = o.m.baglantiKancasi("a_db", 7+i)
				_ = BaglantiKancasi("a_db", 5)
				_ = o.m.DurumAl()
				_, _ = o.m.AktifKisit("a_db")
				_ = o.m.AktifKullanicilar()
				_ = o.m.Olaylar()
			}
		}(i)
	}
	modlar := []Mod{ModUygula, ModUygula, ModGozlem, ModUygula, ModKapali}
	for i := 0; i < 100; i++ {
		o.ayar.Mod = modlar[(i/10)%len(modlar)]
		hiz := 2.0
		if i%25 >= 12 {
			hiz = 0
		}
		o.adim(15, hizlar("a_db", hiz, "b_db", 1.4, "b_wp", 0.4))
	}
	close(dur)
	wg.Wait()
}
