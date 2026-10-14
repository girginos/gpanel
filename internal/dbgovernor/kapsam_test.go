package dbgovernor

// kapsam_test.go — kod düzeyi inceleme turunda (2026-10-13) eklenen kapsam testleri: CLI sorgu
// metinleri, bayi yolları, Depo/Yonetici hata yolları, kimlik önbelleği, Calistir/kanca.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// CLI: gönderilen SQL metinleri ve cevap çözümleme (mysql CLI dikişi).
func TestCLISorguMetinleriVeCozum(t *testing.T) {
	var sorgular []string
	cevap, hata := "", error(nil)
	c := CLI{Calistir: func(ctx context.Context, q string) (string, error) {
		sorgular = append(sorgular, q)
		return cevap, hata
	}}
	ctx := context.Background()
	son := func() string { return sorgular[len(sorgular)-1] }

	cevap = "a_db\t1.5\nroot\t3\nbozuk\n-4\t\nneg\t-1\n"
	cpu, err := c.KullaniciCPU(ctx)
	if err != nil || len(cpu) != 2 || cpu["a_db"] != 1.5 || cpu["root"] != 3 {
		t.Fatalf("KullaniciCPU: %v %v", cpu, err)
	}
	if !strings.Contains(son(), "information_schema.USER_STATISTICS") || !strings.Contains(son(), "CPU_TIME") {
		t.Fatalf("KullaniciCPU SQL: %q", son())
	}

	cevap = "7\ta_db\t12000\t901\tSending data\tSELECT 1\nbozuk satir\n8\tb_db\tx\t1\ts\ti\n"
	qs, err := c.Calisanlar(ctx)
	if err != nil || len(qs) != 1 || qs[0].ID != 7 || qs[0].QueryID != 901 || qs[0].Kullanici != "a_db" ||
		qs[0].Sure != 12*time.Second || qs[0].Durum != "Sending data" || qs[0].Metin != "SELECT 1" {
		t.Fatalf("Calisanlar: %+v %v", qs, err)
	}
	if !strings.Contains(son(), "information_schema.PROCESSLIST") || !strings.Contains(son(), "COMMAND IN ('Query','Execute')") ||
		!strings.Contains(son(), "TIME_MS") || !strings.Contains(son(), "QUERY_ID") {
		t.Fatalf("Calisanlar SQL: %q", son())
	}

	cevap = "1\n"
	if ok, err := c.UserstatAcikMi(ctx); err != nil || !ok || son() != "SELECT @@GLOBAL.userstat" {
		t.Fatalf("UserstatAcikMi: %v %v %q", ok, err, son())
	}
	cevap = "0\n"
	if ok, _ := c.UserstatAcikMi(ctx); ok {
		t.Fatal("userstat=0 kapalı sayılmalı")
	}
	if err := c.UserstatAc(ctx); err != nil || son() != "SET GLOBAL userstat=1" {
		t.Fatalf("UserstatAc: %v %q", err, son())
	}

	// Hesaplar: yalnız geçerli kiracı adları sorguya girer; bozuk/negatif/sistem satırları atlanır
	cevap = "a_db\tlocalhost\t0\na_db\t127.0.0.1\t5\na_db\tbad host\t1\na_db\t%\t-1\na_db\t%\tx\nroot\tlocalhost\t0\nb\tlocalhost\n"
	hs, err := c.Hesaplar(ctx, []string{"a_db", "root", "a'b", ""})
	if err != nil || len(hs) != 1 || len(hs["a_db"]) != 2 || hs["a_db"][0] != (Hesap{Host: "localhost"}) || hs["a_db"][1] != (Hesap{Host: "127.0.0.1", Baglanti: 5}) {
		t.Fatalf("Hesaplar: %v %v", hs, err)
	}
	if !strings.Contains(son(), "WHERE User IN ('a_db')") || strings.Contains(son(), "root") || strings.Contains(son(), "a'b") {
		t.Fatalf("Hesaplar SQL güvensiz adları içeriyor: %q", son())
	}
	n := len(sorgular)
	if hs, err := c.Hesaplar(ctx, []string{"root", "x y"}); err != nil || len(hs) != 0 || len(sorgular) != n {
		t.Fatalf("yalnız güvensiz adlarda DB'ye gidilmemeli: %v %v", hs, err)
	}

	if err := c.BaglantiAyarla(ctx, "a_db", "localhost", 2); err != nil || son() != "ALTER USER 'a_db'@'localhost' WITH MAX_USER_CONNECTIONS 2" {
		t.Fatalf("BaglantiAyarla: %v %q", err, son())
	}
	n = len(sorgular)
	if c.BaglantiAyarla(ctx, "a_db", "lo'cal", 2) == nil || c.BaglantiAyarla(ctx, "a_db", "localhost", -1) == nil ||
		c.BaglantiAyarla(ctx, "root", "localhost", 2) == nil || len(sorgular) != n {
		t.Fatal("güvensiz ALTER reddedilmeli ve DB'ye gitmemeli")
	}
	if err := c.SorguOldur(ctx, 901); err != nil || son() != "KILL QUERY ID 901" {
		t.Fatalf("SorguOldur: %v %q", err, son())
	}
	if err := c.BaglantiKes(ctx, 55); err != nil || son() != "KILL CONNECTION 55" {
		t.Fatalf("BaglantiKes: %v %q", err, son())
	}
	if c.SorguOldur(ctx, 0) == nil || c.BaglantiKes(ctx, -3) == nil {
		t.Fatal("geçersiz id reddedilmeli")
	}

	// hata yayılımı
	hata = errSahte
	if _, err := c.KullaniciCPU(ctx); !errors.Is(err, errSahte) {
		t.Fatalf("KullaniciCPU hata yutuldu: %v", err)
	}
	if _, err := c.Calisanlar(ctx); !errors.Is(err, errSahte) {
		t.Fatalf("Calisanlar hata yutuldu: %v", err)
	}
	if ok, err := c.UserstatAcikMi(ctx); ok || !errors.Is(err, errSahte) {
		t.Fatalf("UserstatAcikMi: %v %v", ok, err)
	}
	if !errors.Is(c.UserstatAc(ctx), errSahte) || !errors.Is(c.SorguOldur(ctx, 1), errSahte) || !errors.Is(c.BaglantiKes(ctx, 1), errSahte) {
		t.Fatal("yazan komut hataları yutuldu")
	}
	if _, err := c.Hesaplar(ctx, []string{"a_db"}); !errors.Is(err, errSahte) {
		t.Fatalf("Hesaplar hata yutuldu: %v", err)
	}
	if _, err := c.Baglantilar(ctx, "a_db"); !errors.Is(err, errSahte) {
		t.Fatalf("Baglantilar hata yutuldu: %v", err)
	}
}

// Bayi toplam sınırı: GÖZLEM modunda yalnız bildirim/denetim (ALTER yok); UYGULA'da 3. seviyeye
// kadar bayiye giden bildirimler (hostingin sahibi bayi; kapsam = bayi).
func TestBayiGozlemVeUcuncuKademeBildirimleri(t *testing.T) {
	kur := func(t *testing.T) *ortam {
		o := yeniOrtam(t)
		o.hosting(3, 7, "x.test", "x_db")
		o.hosting(4, 7, "y.test", "y_db")
		o.depo.bayi[7] = 1.5 // iki hosting 0,95 çekirdek: tek tek eşik altı, toplam 1,9 > 1,5
		return o
	}
	h := hizlar("x_db", 0.95, "y_db", 0.95)

	t.Run("gozlem", func(t *testing.T) {
		o := kur(t)
		o.ayar.Mod = ModGozlem
		o.taban()
		o.calis(8, h)
		if o.yon.alterSayisi() != 0 {
			t.Fatalf("gözlem modunda ALTER: %v", o.yon.alterler)
		}
		if o.olayAdet("denetim", "db.cpu_asim_gozlem") == 0 || o.olayAdet("bildirim", "Veritabanı CPU sınırı aşıldı (gözlem modu)") == 0 {
			t.Fatalf("bayi gözlem bildirimi/denetimi yok: %v", o.m.Olaylar())
		}
		for _, e := range o.m.Olaylar() {
			if e.Tur == "bildirim" && (e.Bayi != 7 || e.Domain != 0) {
				t.Fatalf("bayi bildirimi bayiye gitmeli: %+v", e)
			}
		}
	})
	t.Run("uygula", func(t *testing.T) {
		o := kur(t)
		_ = o.yon.baglan("x_db")
		_ = o.yon.baglan("x_db")
		o.taban()
		o.calis(5, h)
		if !esitInts(o.yon.baglanti("x_db"), []int{2, 2}) || !esitInts(o.yon.baglanti("y_db"), []int{0, 0}) {
			t.Fatalf("yalnız en büyük tüketici (x, eşitlikte küçük id) kısılmalı: x=%v y=%v", o.yon.baglanti("x_db"), o.yon.baglanti("y_db"))
		}
		o.calis(20, h) // seviye 2, sonra 3 dk kesintisiz aşım → seviye 3
		if k, ok := o.m.AktifKisit("x_db"); !ok || k.Seviye != 3 {
			t.Fatalf("seviye 3 bekleniyordu: %+v", k)
		}
		var kes, yuk bool
		for _, e := range o.m.Olaylar() {
			if e.Tur == "bildirim" && e.Eylem == "Veritabanı bağlantıları kesildi" {
				kes = true
				if e.Bayi != 7 || e.Domain != 0 {
					t.Fatalf("bayi kesim bildirimi bayiye gitmeli: %+v", e)
				}
			}
			if e.Tur == "bildirim" && e.Eylem == "Veritabanı CPU kısıtı sertleşti" {
				yuk = true // bayi yolunda hosting'e "sertleşti" bildirimi gitmez
			}
		}
		if !kes || yuk {
			t.Fatalf("kes=%v yukselt(bayi yolunda olmamalı)=%v: %v", kes, yuk, o.m.Olaylar())
		}
	})
}

// hataliDepo: seçili Depo çağrılarını başarısız yapar.
type hataliDepo struct {
	*sahteDepo
	esik, bayi, domKul, guncelle, acikKayit error
}

func (d *hataliDepo) HostingEsik(ctx context.Context, id int64, v float64) (float64, error) {
	if d.esik != nil {
		return 0, d.esik
	}
	return d.sahteDepo.HostingEsik(ctx, id, v)
}
func (d *hataliDepo) BayiCPU(ctx context.Context, id int64) (float64, bool, error) {
	if d.bayi != nil {
		return 0, false, d.bayi
	}
	return d.sahteDepo.BayiCPU(ctx, id)
}
func (d *hataliDepo) DomainKullanicilari(ctx context.Context, id int64) ([]string, error) {
	if d.domKul != nil {
		return nil, d.domKul
	}
	return d.sahteDepo.DomainKullanicilari(ctx, id)
}
func (d *hataliDepo) Guncelle(ctx context.Context, k *Kisit) error {
	if d.guncelle != nil {
		return d.guncelle
	}
	return d.sahteDepo.Guncelle(ctx, k)
}
func (d *hataliDepo) AcikKayitlar(ctx context.Context) ([]*Kisit, error) {
	if d.acikKayit != nil {
		return nil, d.acikKayit
	}
	return d.sahteDepo.AcikKayitlar(ctx)
}

// Depo okuma hataları FAIL-SAFE: okunamayan eşik/bayi sınırı/kullanıcı listesiyle YENİ kısıt konmaz.
func TestDepoHatalariKisitKoymaz(t *testing.T) {
	cases := []struct {
		ad    string
		kur   func(d *hataliDepo, o *ortam)
		hata  string
		hiz   map[string]float64
		bayiO bool
	}{
		{"esik", func(d *hataliDepo, o *ortam) { d.esik = errSahte }, "eşiği okunamadı", hizlar("a_db", 2.0), false},
		{"domKul", func(d *hataliDepo, o *ortam) { d.domKul = errSahte }, "DB kullanıcıları okunamadı", hizlar("a_db", 2.0), false},
		{"bayi", func(d *hataliDepo, o *ortam) {
			d.bayi = errSahte
			o.hosting(3, 7, "x.test", "x_db")
			o.hosting(4, 7, "y.test", "y_db")
		}, "CPU sınırı okunamadı", hizlar("x_db", 0.95, "y_db", 0.95), true},
	}
	for _, c := range cases {
		t.Run(c.ad, func(t *testing.T) {
			o := yeniOrtam(t)
			d := &hataliDepo{sahteDepo: o.depo}
			o.m.Depo = d
			c.kur(d, o)
			o.taban()
			o.calis(8, c.hiz)
			if o.yon.alterSayisi() != 0 || o.depo.acikSayisi() != 0 || len(o.m.AktifKullanicilar()) != 0 {
				t.Fatalf("okunamayan veriyle kısıt kondu: %v", o.yon.alterler)
			}
			if h := o.m.DurumAl().SonHata; !strings.Contains(h, c.hata) {
				t.Fatalf("SonHata=%q, beklenen parça %q", h, c.hata)
			}
		})
	}
}

// Kayıt güncelleme ve doğrulama okuma hataları kısıtı bozmaz; açılış kurtarma okunamazsa sonraki
// açılışta denenir (hata, kısıt belleğe alınmaz).
func TestGuncelleDogrulaKurtarHatalari(t *testing.T) {
	o := yeniOrtam(t)
	d := &hataliDepo{sahteDepo: o.depo}
	o.m.Depo = d
	o.taban()
	o.calis(4, hizlar("a_db", 2.0))
	d.guncelle = errSahte
	o.yon.mu.Lock()
	o.yon.hesapHata = errSahte
	o.yon.mu.Unlock()
	o.calis(6, hizlar("a_db", 2.0)) // Guncelle + Hesaplar(dogrula) hatalı, aşım sürüyor
	if h := o.m.DurumAl().SonHata; !strings.Contains(h, "doğrulaması için hesaplar okunamadı") {
		t.Fatalf("SonHata=%q", h)
	}
	if len(o.m.AktifKullanicilar()) != 1 {
		t.Fatalf("hata kısıtı düşürmemeli: %v", o.m.AktifKullanicilar())
	}
	d.guncelle, o.yon.hesapHata = nil, nil
	o.calis(2, hizlar("a_db", 2.0))

	// Kurtar: AcikKayitlar hatası → hata kaydı, kısıt belleğe alınmaz
	o2 := yeniOrtam(t)
	d2 := &hataliDepo{sahteDepo: o2.depo, acikKayit: errSahte}
	o2.m.Depo = d2
	o2.m.Kurtar(context.Background())
	if !strings.Contains(o2.m.DurumAl().SonHata, "açık kısıtlar okunamadı") || len(o2.m.AktifKullanicilar()) != 0 {
		t.Fatalf("Kurtar hata yolu: %q", o2.m.DurumAl().SonHata)
	}
}

// userstat hazırlığı: okuma hatası → "hata"; açılamayan/açılsa da doğrulanamayan → "bekliyor";
// kalıcı yapılandırma yalnız dizin varsa yazılır ve aynı içerik yeniden yazılmaz.
type userstatYon struct {
	*sahteYon
	acikHata error
	acNoop   bool
}

func (u *userstatYon) UserstatAcikMi(ctx context.Context) (bool, error) {
	if u.acikHata != nil {
		return false, u.acikHata
	}
	return u.sahteYon.UserstatAcikMi(ctx)
}
func (u *userstatYon) UserstatAc(ctx context.Context) error {
	if u.acNoop {
		return nil
	}
	return u.sahteYon.UserstatAc(ctx)
}

func TestUserstatHataVeKaliciYollar(t *testing.T) {
	o := yeniOrtam(t)
	uy := &userstatYon{sahteYon: o.yon, acikHata: errSahte}
	o.m.Yon = uy
	o.yon.userstat = false
	o.taban()
	if o.m.DurumAl().Userstat != "hata" {
		t.Fatalf("okuma hatası → hata: %q", o.m.DurumAl().Userstat)
	}
	uy.acikHata, uy.acNoop = nil, true
	o.m.mu.Lock()
	o.m.durum.Userstat = ""
	o.m.mu.Unlock()
	o.adim(15, nil)
	if o.m.DurumAl().Userstat != "bekliyor" {
		t.Fatalf("açma sessizce etkisiz → bekliyor: %q", o.m.DurumAl().Userstat)
	}

	// kalıcı yapılandırma: dizin yoksa yazılmaz; varsa bir kez yazılır, aynı içerik yeniden yazılmaz
	o.m.UserstatCnf = filepath.Join(t.TempDir(), "yok-dizin", "x.cnf")
	o.m.cnfYaz()
	if o.m.DurumAl().UserstatKalici {
		t.Fatal("dizin yokken kalıcı sayıldı")
	}
	yol := filepath.Join(t.TempDir(), "zz.cnf")
	o.m.UserstatCnf = yol
	o.m.cnfYaz()
	b, err := os.ReadFile(yol)
	if err != nil || string(b) != userstatCnfIcerik || !o.m.DurumAl().UserstatKalici {
		t.Fatalf("yapılandırma yazılmadı: %v %q", err, b)
	}
	st1, _ := os.Stat(yol)
	time.Sleep(20 * time.Millisecond)
	o.m.cnfYaz()
	if st2, _ := os.Stat(yol); !st2.ModTime().Equal(st1.ModTime()) || st1.Mode().Perm() != 0o644 {
		t.Fatalf("aynı içerik yeniden yazıldı ya da izin 0644 değil: %v", st1.Mode())
	}
}

// Kimlik önbelleği: 60 sn TTL, bilinmeyen kullanıcı → Yok, hata yayılımı, bayat girdi temizliği.
func TestKimlikOnbellegi(t *testing.T) {
	o := yeniOrtam(t)
	o.m.hazirla()
	ctx := context.Background()
	t0 := o.simdi
	k, err := o.m.kimlikler(ctx, []string{"a_db", "yok_kul"}, t0)
	if err != nil || k["a_db"].DomainID != 1 || !k["yok_kul"].Yok {
		t.Fatalf("kimlikler: %+v %v", k, err)
	}
	n := o.depo.esleSay
	_, _ = o.m.kimlikler(ctx, []string{"a_db"}, t0.Add(59*time.Second))
	if o.depo.esleSay != n {
		t.Fatal("TTL içinde Esle yeniden çağrıldı")
	}
	_, _ = o.m.kimlikler(ctx, []string{"a_db"}, t0.Add(60*time.Second))
	if o.depo.esleSay != n+1 {
		t.Fatalf("TTL dolunca Esle çağrılmalı: %d → %d", n, o.depo.esleSay)
	}
	o.depo.esleHata = errSahte
	if _, err := o.m.kimlikler(ctx, []string{"b_db"}, t0.Add(5*time.Minute)); !errors.Is(err, errSahte) {
		t.Fatalf("Esle hatası yutuldu: %v", err)
	}
	o.depo.esleHata = nil
	for i := 0; i < 20001; i++ {
		o.m.kimlikOn[string(rune('A'+i%26))+strings.Repeat("x", i%7)+time.Duration(i).String()] = kimlikKaydi{ts: t0.Add(-time.Hour)}
	}
	_, _ = o.m.kimlikler(ctx, []string{"a_db"}, t0.Add(10*time.Minute))
	if len(o.m.kimlikOn) > 100 {
		t.Fatalf("bayat önbellek girdileri temizlenmedi: %d", len(o.m.kimlikOn))
	}
}

func TestHalkaSifirPencere(t *testing.T) {
	h := &halka{}
	h.ekle(ornek{t: time.Unix(100, 0), dt: 15, cpu: 10, canli: 1}, time.Minute)
	if h.bos() {
		t.Fatal("etkinlik var: bos olmamalı")
	}
	if c, g := h.oranlar(time.Unix(101, 0), time.Minute); c != 10.0/60 || g != 15.0/60 {
		t.Fatalf("oranlar: %v %v", c, g)
	}
	if c, g := h.oranlar(time.Unix(101, 0), 0); c != 0 || g != 0 {
		t.Fatalf("sıfır pencere → 0,0: %v %v", c, g)
	}
	if !h.bos() {
		t.Fatal("pencere dışına çıkan örnekler atıldı: bos olmalı")
	}
	if got := milli(-3); got != 0 {
		t.Fatalf("milli(-3)=%d", got)
	}
	if got := milli(2.25); got != 2250 {
		t.Fatalf("milli(2.25)=%d", got)
	}
}
