package wordpress

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// bekleKosul: koşul sağlanana dek (en çok 3 sn) yoklar.
func bekleKosul(t *testing.T, ad string, f func() bool) {
	t.Helper()
	for i := 0; i < 300; i++ {
		if f() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("zaman aşımı: %s", ad)
}

// sahte saatli önbellek
func saatliOnb(ttl time.Duration) (*onbellek, *time.Time) {
	now := time.Unix(1_000_000, 0)
	o := yeniOnbellek(ttl, 16)
	o.simdi = func() time.Time { return now }
	return o, &now
}

// ---------------------------------------------------------------- SWR çekirdeği

func TestYapBayat_BayatHemenDonerArkadaYenilenir(t *testing.T) {
	o, now := saatliOnb(time.Minute)
	var n int64
	kapi := make(chan struct{})
	fn := func(context.Context) (any, error) {
		k := atomic.AddInt64(&n, 1)
		if k > 1 {
			<-kapi // yenileme yavaş
		}
		return fmt.Sprintf("v%d", k), nil
	}
	if d, _ := o.YapBayat(context.Background(), "k", time.Second, time.Hour, fn); d != "v1" {
		t.Fatalf("ilk: %v", d)
	}
	*now = now.Add(2 * time.Minute) // TTL doldu, bayat penceresinde
	bas := time.Now()
	d, err := o.YapBayat(context.Background(), "k", time.Second, time.Hour, fn)
	if err != nil || d != "v1" || time.Since(bas) > 300*time.Millisecond {
		t.Fatalf("bayat değer HEMEN dönmeliydi: %v %v %v", d, err, time.Since(bas))
	}
	// yenileme sürerken ikinci istek de bayatı alır, ikinci yenileme BAŞLAMAZ
	if d, _ := o.YapBayat(context.Background(), "k", time.Second, time.Hour, fn); d != "v1" {
		t.Fatalf("yenileme sürerken: %v", d)
	}
	close(kapi)
	bekleKosul(t, "yenileme biter", func() bool { v, ok := o.Bak("k"); return ok && v == "v2" })
	if atomic.LoadInt64(&n) != 2 {
		t.Fatalf("yenileme %d kez çalıştı (2 beklenir)", n)
	}
}

func TestYapBayat_BayatPenceresiDolunca_Bekler(t *testing.T) {
	o, now := saatliOnb(time.Minute)
	var n int64
	fn := func(context.Context) (any, error) { return atomic.AddInt64(&n, 1), nil }
	o.YapBayat(context.Background(), "k", time.Second, time.Hour, fn)
	*now = now.Add(3 * time.Hour) // pencereyi de aştı
	d, _ := o.YapBayat(context.Background(), "k", time.Second, time.Hour, fn)
	if d != int64(2) {
		t.Fatalf("pencere dışı bayat sunuldu: %v", d)
	}
}

// Mutasyon (Sil) bayat değeri de siler; uçuştaki yenilemenin ESKİ sonucu yazılmaz ve
// uçuş sırasında bayat sunulmaz.
func TestYapBayat_SilBayatiVeUcustakiniTemizler(t *testing.T) {
	o, now := saatliOnb(time.Minute)
	var n int64
	kapi := make(chan struct{})
	fn := func(context.Context) (any, error) {
		k := atomic.AddInt64(&n, 1)
		if k == 2 {
			<-kapi
		}
		return fmt.Sprintf("v%d", k), nil
	}
	o.YapBayat(context.Background(), "p\x00k", time.Second, time.Hour, fn)
	*now = now.Add(2 * time.Minute)
	o.YapBayat(context.Background(), "p\x00k", time.Second, time.Hour, fn) // yenileme başladı (bekliyor)
	bekleKosul(t, "yenileme uçuşta", func() bool { return atomic.LoadInt64(&n) == 2 })
	o.Sil("p\x00")
	if d, ok := o.BayatBak("p\x00k", time.Hour); ok {
		t.Fatalf("Sil sonrası uçuşta bayat sunuldu: %v", d)
	}
	close(kapi) // eski (mutasyon öncesi) yenileme biter
	time.Sleep(50 * time.Millisecond)
	if d, ok := o.BayatBak("p\x00k", time.Hour); ok {
		t.Fatalf("mutasyon öncesi sonuç yazıldı: %v", d)
	}
	if d, _ := o.YapBayat(context.Background(), "p\x00k", time.Second, time.Hour, fn); d != "v3" {
		t.Fatalf("Sil sonrası yeni değer beklenir: %v", d)
	}
}

func TestYapBayat_YenilemeHatasiOnbelleklenmezBayatKalmaz(t *testing.T) {
	o, now := saatliOnb(time.Minute)
	var bozuk atomic.Bool
	fn := func(context.Context) (any, error) {
		if bozuk.Load() {
			return nil, fmt.Errorf("wp-cli çöktü")
		}
		return "iyi", nil
	}
	o.YapBayat(context.Background(), "k", time.Second, time.Hour, fn)
	*now = now.Add(2 * time.Minute)
	bozuk.Store(true)
	o.YapBayat(context.Background(), "k", time.Second, time.Hour, fn) // bayat döner, arka yenileme hata
	bekleKosul(t, "hata sonrası kayıt atılır", func() bool { _, ok := o.BayatBak("k", time.Hour); return !ok })
	if _, err := o.YapBayat(context.Background(), "k", time.Second, time.Hour, fn); err == nil {
		t.Fatal("hata iletilmeliydi (sessizce bayat sunulmamalı)")
	}
}

// ---------------------------------------------------------------- listeJSON SWR

func TestListeJSON_BayatSunulurYenilemeArkada(t *testing.T) {
	o, now := saatliOnb(time.Minute)
	onbDegistir(o, nil)
	var n int64
	sahteWPCli(t, func(ctx context.Context, sk string, args ...string) ([]byte, error) {
		return []byte(fmt.Sprintf(`[{"name":"p%d"}]`, atomic.AddInt64(&n, 1))), nil
	})
	dir := "/home/c_a/public_html"
	a := listeJSON(context.Background(), "c_a", dir, "plugin", time.Second, "plugin", "list")
	*now = now.Add(5 * time.Minute)
	b := listeJSON(context.Background(), "c_a", dir, "plugin", time.Second, "plugin", "list")
	if string(a[0]) != string(b[0]) {
		t.Fatalf("bayat değer beklenir: %s %s", a[0], b[0])
	}
	bekleKosul(t, "arka yenileme", func() bool {
		v, ok := listeOnb.Bak(onbAnahtar("c_a", dir, "plugin"))
		return ok && string(v.([]json.RawMessage)[0]) == `{"name":"p2"}`
	})
}

// ---------------------------------------------------------------- ön-ısıtma

// onbDegistir: genel önbellekleri değiştirmeden önce önceki testten kalan arka plan
// işlerinin (ön-ısıtma, SWR yenileme) bitmesini bekler → test kaynaklı yarış olmaz.
func onbDegistir(l, a *onbellek) {
	for i := 0; i < 300 && isitSayac.Load() != 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if l != nil {
		listeOnb = l
	}
	if a != nil {
		agirOnb = a
	}
}

func isitSifirla() { onbDegistir(yeniOnbellek(time.Minute, 64), yeniOnbellek(time.Minute, 64)) }

func TestWpIsit_ListeleriTekTekIsitirKiracıKapsamli(t *testing.T) {
	isitSifirla()
	var cur, max int64
	var mu sync.Mutex
	var cagri []string
	sahteWPCli(t, func(ctx context.Context, sk string, args ...string) ([]byte, error) {
		c := atomic.AddInt64(&cur, 1)
		defer atomic.AddInt64(&cur, -1)
		for {
			m := atomic.LoadInt64(&max)
			if c <= m || atomic.CompareAndSwapInt64(&max, m, c) {
				break
			}
		}
		mu.Lock()
		cagri = append(cagri, sk+":"+args[0])
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		if args[0] == "db" {
			return []byte("7\n"), nil
		}
		return []byte(fmt.Sprintf(`[{"name":"%s-%s"}]`, sk, args[0])), nil
	})
	dir := "/home/c_a/public_html"
	wpIsit("c_a", dir)
	wpIsit("c_a", dir) // tekilleştirilir
	wpIsit("c_b", dir)
	bekleKosul(t, "iki site ısındı", func() bool {
		for _, sk := range []string{"c_a", "c_b"} {
			for _, tur := range []string{"plugin", "theme", "user"} {
				if _, ok := listeOnb.Bak(onbAnahtar(sk, dir, tur)); !ok {
					return false
				}
			}
			if _, ok := agirOnb.Bak(onbAnahtar(sk, dir, "dbmb")); !ok {
				return false
			}
		}
		return true
	})
	if m := atomic.LoadInt64(&max); m != 1 {
		t.Fatalf("ön-ısıtma eşzamanlılığı %d (1 beklenir)", m)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(cagri) != 8 { // 2 site x (db + 3 liste); çift wpIsit tekilleştirildi
		t.Fatalf("çağrılar: %v", cagri)
	}
	v, _ := listeOnb.Bak(onbAnahtar("c_b", dir, "plugin"))
	if !strings.Contains(string(v.([]json.RawMessage)[0]), "c_b-") {
		t.Fatalf("kiracı karışması: %s", v.([]json.RawMessage)[0])
	}
}

func TestWpIsit_ZatenOnbellektekiniAtlar(t *testing.T) {
	isitSifirla()
	var n int64
	sahteWPCli(t, func(ctx context.Context, sk string, args ...string) ([]byte, error) {
		atomic.AddInt64(&n, 1)
		if args[0] == "db" {
			return []byte("1"), nil
		}
		return []byte(`[]`), nil
	})
	dir := "/home/c_a/public_html"
	wpIsit("c_a", dir)
	bekleKosul(t, "ilk ısınma", func() bool {
		_, ok := listeOnb.Bak(onbAnahtar("c_a", dir, "user"))
		return ok && atomic.LoadInt64(&n) == 4
	})
	bekleKosul(t, "iş bitti", func() bool { _, var_ := isitDevam.Load("c_a\x00" + dir); return !var_ })
	wpIsit("c_a", dir)
	time.Sleep(100 * time.Millisecond)
	if atomic.LoadInt64(&n) != 4 {
		t.Fatalf("sıcak site yeniden çalıştırıldı: %d", n)
	}
}

func TestWpMutasyonSonu_AtarVeYenidenIsitir(t *testing.T) {
	isitSifirla()
	var surum int64 = 1
	sahteWPCli(t, func(ctx context.Context, sk string, args ...string) ([]byte, error) {
		if args[0] == "db" {
			return []byte("1"), nil
		}
		return []byte(fmt.Sprintf(`[{"name":"v%d"}]`, atomic.LoadInt64(&surum))), nil
	})
	dir := "/home/c_a/public_html"
	wpIsit("c_a", dir)
	bekleKosul(t, "ilk ısınma", func() bool { _, ok := listeOnb.Bak(onbAnahtar("c_a", dir, "user")); return ok })
	bekleKosul(t, "iş bitti", func() bool { _, var_ := isitDevam.Load("c_a\x00" + dir); return !var_ })
	atomic.StoreInt64(&surum, 2) // mutasyon eklentiyi değiştirdi
	wpMutasyonSonu("c_a", dir)
	if v, ok := listeOnb.BayatBak(onbAnahtar("c_a", dir, "plugin"), time.Hour); ok && strings.Contains(string(v.([]json.RawMessage)[0]), "v1") {
		t.Fatal("mutasyon sonrası bayat (v1) sunulabilir durumda")
	}
	bekleKosul(t, "yeniden ısındı v2", func() bool {
		v, ok := listeOnb.Bak(onbAnahtar("c_a", dir, "plugin"))
		return ok && strings.Contains(string(v.([]json.RawMessage)[0]), "v2")
	})
}

// wp-cli başarısızsa ön-ısıtma panik/kilitlenme yapmaz, sonra istek yolu yeniden dener;
// uç boş dizi döner (sayfa boş değil hata göstermez ama çökmez) ve sonraki çağrı düzelir.
func TestWpIsit_HataSonrasiIstekYoluYenidenDener(t *testing.T) {
	isitSifirla()
	var bozuk atomic.Bool
	bozuk.Store(true)
	sahteWPCli(t, func(ctx context.Context, sk string, args ...string) ([]byte, error) {
		if bozuk.Load() {
			return nil, fmt.Errorf("php yok")
		}
		return []byte(`[{"name":"x"}]`), nil
	})
	dir := "/home/c_a/public_html"
	wpIsit("c_a", dir)
	bekleKosul(t, "iş bitti", func() bool { _, var_ := isitDevam.Load("c_a\x00" + dir); return !var_ })
	if _, ok := listeOnb.BayatBak(onbAnahtar("c_a", dir, "plugin"), time.Hour); ok {
		t.Fatal("hata önbelleğe alındı")
	}
	bozuk.Store(false)
	if r := listeTur(context.Background(), "c_a", dir, "plugin"); len(r) != 1 {
		t.Fatalf("yeniden deneme: %v", r)
	}
}

func TestListeTanim_DizinTekArgumanVeBilinmeyenTurBos(t *testing.T) {
	a, _ := listeTanim("plugin", "/x --exec=evil")
	n := 0
	for _, s := range a {
		if strings.Contains(s, "evil") {
			n++
			if !strings.HasPrefix(s, "--path=") {
				t.Fatalf("dizin --path dışında geçti: %q", s)
			}
		}
	}
	if n != 1 {
		t.Fatalf("dizin tek argümanda olmalı: %v", a)
	}
	if a, _ := listeTanim("rm", "/x"); a != nil {
		t.Fatal("bilinmeyen tür tanımsız olmalı")
	}
	if r := listeTur(context.Background(), "c_a", "/x", "rm"); len(r) != 0 {
		t.Fatal("bilinmeyen tür boş dizi")
	}
}

// ---------------------------------------------------------------- DB boyutu

func TestDbBoyutuAl_HizliYolSatirIciYavasYolYukleniyor(t *testing.T) {
	onbDegistir(nil, yeniOnbellek(time.Minute, 64))
	var gecikme atomic.Int64
	sahteWPCli(t, func(ctx context.Context, sk string, args ...string) ([]byte, error) {
		time.Sleep(time.Duration(gecikme.Load()))
		return []byte("3\n"), nil
	})
	gecikme.Store(int64(10 * time.Millisecond))
	if mb, yuk := dbBoyutuAl(context.Background(), "c_a", "/d1", time.Second); mb != "3" || yuk {
		t.Fatalf("hızlı yol: %q %v", mb, yuk)
	}
	gecikme.Store(int64(300 * time.Millisecond))
	if mb, yuk := dbBoyutuAl(context.Background(), "c_a", "/d2", 20*time.Millisecond); mb != "" || !yuk {
		t.Fatalf("yavaş yol: %q %v", mb, yuk)
	}
	bekleKosul(t, "arkada tamamlanır", func() bool { _, ok := agirOnb.Bak(onbAnahtar("c_a", "/d2", "dbmb")); return ok })
	if mb, yuk := dbBoyutuAl(context.Background(), "c_a", "/d2", 20*time.Millisecond); mb != "3" || yuk {
		t.Fatalf("sonra: %q %v", mb, yuk)
	}
}

func TestDbBoyutuAl_HataYukleniyorDegil(t *testing.T) {
	onbDegistir(nil, yeniOnbellek(time.Minute, 64))
	sahteWPCli(t, func(ctx context.Context, sk string, args ...string) ([]byte, error) { return nil, fmt.Errorf("db yok") })
	if mb, yuk := dbBoyutuAl(context.Background(), "c_a", "/d", time.Second); mb != "" || yuk {
		t.Fatalf("hata '…' döngüsüne girmemeli: %q %v", mb, yuk)
	}
}

func TestDbBoyutuAl_BayatHemenDonerKiracıKapsamli(t *testing.T) {
	o, now := saatliOnb(time.Minute)
	onbDegistir(nil, o)
	var n int64
	sahteWPCli(t, func(ctx context.Context, sk string, args ...string) ([]byte, error) {
		return []byte(fmt.Sprintf("%d", 10*atomic.AddInt64(&n, 1))), nil
	})
	dbBoyutuAl(context.Background(), "c_a", "/d", time.Second)
	*now = now.Add(time.Hour)
	bas := time.Now()
	if mb, yuk := dbBoyutuAl(context.Background(), "c_a", "/d", time.Second); mb != "10" || yuk || time.Since(bas) > 200*time.Millisecond {
		t.Fatalf("bayat hemen: %q %v", mb, yuk)
	}
	if _, ok := agirOnb.BayatBak(onbAnahtar("c_b", "/d", "dbmb"), time.Hour); ok {
		t.Fatal("c_b değeri c_a'dan sızdı")
	}
	bekleKosul(t, "arka yenileme biter", func() bool { v, ok := agirOnb.Bak(onbAnahtar("c_a", "/d", "dbmb")); return ok && v == "20" })
}
