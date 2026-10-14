package dbgovernor

// yayilma_test.go — kısıtın YAYILMASI: boşta kardeş hesaplar kısılmaz/öldürülmez (VM-C ölçümü: boşta
// kardeş 30→2'ye düşürülmüş, açık SELECT SLEEP(900) ERROR 1317 ile KILL edilmişti); yük hesaplara
// bölünse de atlatma yok; KILL yalnız CPU yakan okumaya.

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func (o *ortam) sorgular(qs ...Sorgu) {
	o.olcer.mu.Lock()
	o.olcer.calisan = qs
	o.olcer.mu.Unlock()
}

func (o *ortam) altersVar(onek string) bool {
	o.yon.mu.Lock()
	defer o.yon.mu.Unlock()
	for _, a := range o.yon.alterler {
		if strings.HasPrefix(a, onek) {
			return true
		}
	}
	return false
}

func idler(l []int64, x int64) bool {
	for _, v := range l {
		if v == x {
			return true
		}
	}
	return false
}

func TestBostaKardesKisilmazVeOldurulmez(t *testing.T) {
	for _, pay := range []float64{0, 0.04} { // tamamen boşta / ~%2 pay
		t.Run(fmt.Sprintf("pay%v", pay), func(t *testing.T) {
			o := yeniOrtam(t)
			for i := range o.yon.hesap["b_wp"] {
				o.yon.hesap["b_wp"][i].Baglanti = 30
			}
			_ = o.yon.baglan("b_wp") // boşta kardeşin açık bağlantısı
			bosID := o.yon.conns["b_wp"][0]
			o.taban()
			h := hizlar("b_db", 2.0)
			if pay > 0 {
				h["b_wp"] = pay
			}
			o.sorgular(
				Sorgu{QueryID: 901, Kullanici: "b_wp", Sure: 14 * time.Minute, Durum: "User sleep", Metin: "SELECT SLEEP(900)"},
				Sorgu{QueryID: 902, Kullanici: "b_db", Sure: 40 * time.Second, Durum: "Sending data", Metin: "SELECT * FROM big"},
			)
			o.calis(20, h) // 300 sn: 2. ve 3. seviye
			if got := o.m.AktifKullanicilar(); len(got) != 1 || got[0] != "b_db" {
				t.Fatalf("yalnız yük bindiren b_db kısılmalı: %v", got)
			}
			if o.kisitSeviyesi("b_db") != 3 {
				t.Fatalf("b_db seviye 3 olmalıydı (tırmanma sürmeli): %d", o.kisitSeviyesi("b_db"))
			}
			if !esitInts(o.yon.baglanti("b_wp"), []int{30, 30}) || o.altersVar("b_wp@") {
				t.Fatalf("boşta kardeşin sınırına dokunuldu: %v %v", o.yon.baglanti("b_wp"), o.yon.alterler)
			}
			if !idler(o.yon.killler, 902) || idler(o.yon.killler, 901) {
				t.Fatalf("CPU yakan SELECT(902) öldürülmeli, SLEEP(901) öldürülmemeli: %v", o.yon.killler)
			}
			if idler(o.yon.kesilenler, bosID) || o.yon.baglantiSayisi("b_wp") != 1 {
				t.Fatalf("boşta kardeşin bağlantısı kesildi: %v", o.yon.kesilenler)
			}
			if o.depo.acikSayisi() != 1 {
				t.Fatalf("db_cpu_kisit yalnız kısılan kullanıcı için açılmalı: %d", o.depo.acikSayisi())
			}
			for _, e := range o.m.Olaylar() {
				if strings.Contains(e.Detay, "b_wp") {
					t.Fatalf("bildirim/denetim boşta kardeşi anıyor: %+v", e)
				}
			}
		})
	}
}

// Yük iki hesaba bölününce ikisinin de payı yüksek: İKİSİ de kısılır (hesap bölerek atlatma yok).
func TestBolunmusYukHerIkisiniDeKisar(t *testing.T) {
	o := yeniOrtam(t)
	o.taban()
	o.calis(5, hizlar("b_db", 1.2, "b_wp", 1.2))
	if got := o.m.AktifKullanicilar(); len(got) != 2 {
		t.Fatalf("iki büyük pay da kısılmalı: %v", got)
	}
	for _, u := range []string{"b_db", "b_wp"} {
		if !esitInts(o.yon.baglanti(u), []int{2, 2}) {
			t.Fatalf("%s kısılmadı: %v", u, o.yon.baglanti(u))
		}
	}
}

// 20 hesaba bölünmüş yük (her biri %5): kısılmayan kuyruk en çok %10.
func TestCokHesabaBolunmusYuk(t *testing.T) {
	o := yeniOrtam(t)
	var us []string
	h := map[string]float64{}
	for i := 1; i <= 20; i++ {
		u := fmt.Sprintf("c_u%02d", i)
		us = append(us, u)
		h[u] = 0.1
	}
	o.hosting(3, 0, "c.test", us...)
	o.taban()
	o.calis(5, h)
	n := len(o.m.AktifKullanicilar())
	if n != 18 {
		t.Fatalf("20 eşit hesabın %%90'ı = 18 hesap kısılmalı: %d (%v)", n, o.m.AktifKullanicilar())
	}
}

// Yük kayınca (b_db → b_wp) yeni baskın hesap da kısılır.
func TestYukKayinca(t *testing.T) {
	o := yeniOrtam(t)
	o.taban()
	o.calis(5, hizlar("b_db", 2.0))
	if got := o.m.AktifKullanicilar(); len(got) != 1 || got[0] != "b_db" {
		t.Fatalf("önce yalnız b_db: %v", got)
	}
	o.calis(5, hizlar("b_wp", 2.0))
	if got := o.m.AktifKullanicilar(); len(got) != 2 {
		t.Fatalf("yük kayınca b_wp de kısılmalı: %v", got)
	}
}

// Bayi yolunda da aynı seçim: yalnız baskın tüketici.
func TestBayiYolundaBostaKardes(t *testing.T) {
	o := yeniOrtam(t)
	o.hosting(11, 7, "h1.test", "h1_db", "h1_bos")
	o.depo.bayi[7] = 0.5
	o.taban()
	o.calis(5, hizlar("h1_db", 0.8, "h1_bos", 0.01))
	if got := o.m.AktifKullanicilar(); len(got) != 1 || got[0] != "h1_db" {
		t.Fatalf("bayi yolunda da yalnız baskın tüketici kısılmalı: %v", got)
	}
}

// KILL QUERY yalnız CPU yakan OKUMA deyimine: uyku/kilit bekleyen/durumsuz deyim ve yazma öldürülmez.
func TestKillSadeceCPUYakanOkuma(t *testing.T) {
	o := yeniOrtam(t)
	o.taban()
	o.calis(7, hizlar("a_db", 2.0))
	if o.kisitSeviyesi("a_db") != 2 {
		t.Fatalf("seviye 2 bekleniyordu: %d", o.kisitSeviyesi("a_db"))
	}
	o.sorgular(
		Sorgu{QueryID: 1, Kullanici: "a_db", Sure: time.Minute, Durum: "Sending data", Metin: "SELECT * FROM t"},
		Sorgu{QueryID: 2, Kullanici: "a_db", Sure: time.Minute, Durum: "User sleep", Metin: "SELECT SLEEP(900)"},
		Sorgu{QueryID: 3, Kullanici: "a_db", Sure: time.Minute, Durum: "Waiting for table metadata lock", Metin: "SELECT * FROM t"},
		Sorgu{QueryID: 4, Kullanici: "a_db", Sure: time.Minute, Durum: "", Metin: "SELECT * FROM t"},
		Sorgu{QueryID: 5, Kullanici: "a_db", Sure: time.Minute, Durum: "Sending data", Metin: "UPDATE t SET a=1"},
		Sorgu{QueryID: 6, Kullanici: "a_db", Sure: time.Minute, Durum: "Copying to tmp table", Metin: "SELECT a FROM t GROUP BY a"},
	)
	o.adim(15, hizlar("a_db", 2.0))
	if len(o.yon.killler) != 2 || !idler(o.yon.killler, 1) || !idler(o.yon.killler, 6) {
		t.Fatalf("yalnız CPU yakan SELECT'ler (1,6) öldürülmeli: %v", o.yon.killler)
	}
}

// kisitHedefleri saf seçim kuralı: %90 kapsam, ad sırası bağ bozucu, güvensiz/sistem adı asla.
func TestKisitHedefleriSecimi(t *testing.T) {
	tablo := []struct {
		ad    string
		pay   map[string]float64
		users []string
		want  string
	}{
		{"iki baskin", map[string]float64{"a": 0.7, "b": 0.3}, []string{"a", "b"}, "a,b"},
		{"tek baskin", map[string]float64{"a": 0.95, "b": 0.05}, []string{"a", "b"}, "a"},
		{"tam yuzde 90", map[string]float64{"a": 0.9, "b": 0.1}, []string{"a", "b"}, "a"},
		{"esit paylar ad sirali", map[string]float64{"b": 1, "a": 1}, []string{"b", "a"}, "a,b"},
		{"tuketim yok", map[string]float64{"a": 0, "b": 0}, []string{"a", "b"}, ""},
		{"bos", nil, []string{"a"}, ""},
		{"listede olmayan", map[string]float64{"a": 1, "z": 5}, []string{"a"}, "a"},
		{"sistem ve guvensiz adlar asla", map[string]float64{"a": 1, "root": 50, "panel": 9, "x y;": 7}, []string{"a", "root", "panel", "x y;"}, "a"},
	}
	for _, c := range tablo {
		if got := strings.Join(kisitHedefleri(c.pay, c.users), ","); got != c.want {
			t.Errorf("%s: %q != %q", c.ad, got, c.want)
		}
	}
}
