package dbgovernor

// seviye3_test.go — 3. seviye: MAX_USER_CONNECTIONS=1 (ASLA 0: MariaDB'de 0 = sınırsız), önce sınır
// sonra bağlantı kesimi, sürekli aşım bildirimi. Otomatik kilit/askı/MAX_QUERIES_PER_HOUR YOK.

import (
	"strings"
	"testing"
)

// seviyeyeCik: kullanıcı verilen seviyeye ulaşana dek tur koşar.
func (o *ortam) seviyeyeCik(u string, seviye int, h map[string]float64) {
	o.t.Helper()
	for i := 0; i < 80; i++ {
		if o.kisitSeviyesi(u) >= seviye {
			return
		}
		o.adim(15, h)
	}
	o.t.Fatalf("%s %d. seviyeye ulaşmadı (seviye %d)", u, seviye, o.kisitSeviyesi(u))
}

func (o *ortam) surekliAdet() int { return o.olayAdet("bildirim", surekliBaslik) }

func TestSeviye3SinirBirOnceKesmeSonra(t *testing.T) {
	o := yeniOrtam(t)
	for i := 0; i < 6; i++ {
		_ = o.yon.baglan("a_db") // açık bağlantıları olan istemci
	}
	o.taban()
	yuk := hizlar("a_db", 2.0)
	o.seviyeyeCik("a_db", 3, yuk)

	// sıra: iki hesaba da =1 ALTER'i, ilk KILL CONNECTION'dan ÖNCE
	o.yon.mu.Lock()
	sira := append([]string(nil), o.yon.sira...)
	o.yon.mu.Unlock()
	ilkKes, son1 := -1, -1
	var bir []string
	for i, s := range sira {
		switch {
		case s == "kes" && ilkKes < 0:
			ilkKes = i
		case strings.HasSuffix(s, "=1"):
			bir = append(bir, s)
			son1 = i
		}
	}
	if ilkKes < 0 || len(bir) != 2 || son1 > ilkKes {
		t.Fatalf("MAX_USER_CONNECTIONS=1 her hesapta KILL CONNECTION'dan önce yazılmalı: kes=%d bir=%v sira=%v", ilkKes, bir, sira)
	}
	if !esitInts(o.yon.baglanti("a_db"), []int{1, 1}) {
		t.Fatalf("3. seviyede sınır 1 olmalı: %v", o.yon.baglanti("a_db"))
	}
	for _, a := range o.yon.alterler {
		if strings.HasSuffix(a, "=0") {
			t.Fatalf("kısıt sürerken ASLA =0 yazılmaz (0 = sınırsız): %v", o.yon.alterler)
		}
	}
	if k, _ := o.m.AktifKisit("a_db"); k.KisitDeger != 1 || k.Kesilen != 6 {
		t.Fatalf("kısıt kaydı: %+v", k)
	}
	// yeniden bağlanan istemci: sayaç 0 → sınır 1 geçerli
	kabul := 0
	for i := 0; i < 6; i++ {
		if o.yon.baglan("a_db") == nil {
			kabul++
		}
	}
	if kabul != 1 {
		t.Fatalf("yeniden bağlanan istemci 1 bağlantıya takılmalı: kabul=%d", kabul)
	}
	// plan kancası: plan ne olursa olsun kısıt değeri (>=1); 0 (sınırsız) ASLA
	for _, plan := range []int{100, 15, 1, 0} { // son çağrı 0: plan sınırsız → soğuyunca sınırsız geri gelir
		if got := o.m.baglantiKancasi("a_db", plan); got != 1 {
			t.Fatalf("plan=%d iken kanca %d döndü (1 beklenir)", plan, got)
		}
	}
	// dış yazım (plan yeniden uygulaması) doğrulama turunda geri alınır
	o.yon.mu.Lock()
	o.yon.hesap["a_db"][0].Baglanti = 15
	o.yon.mu.Unlock()
	o.adim(15, yuk)
	if !esitInts(o.yon.baglanti("a_db"), []int{1, 1}) {
		t.Fatalf("ezilen 3. seviye sınırı düzeltilmedi: %v", o.yon.baglanti("a_db"))
	}
	// soğuyunca kullanıcının asıl sınırı (0 = sınırsız) geri gelir
	o.calis(40, hizlar("a_db", 0.0))
	if len(o.m.AktifKullanicilar()) != 0 || !esitInts(o.yon.baglanti("a_db"), []int{0, 0}) {
		t.Fatalf("soğuma sonrası geri dönüş: %v %v", o.m.AktifKullanicilar(), o.yon.baglanti("a_db"))
	}
}

// Sınır yazılamıyorsa bağlantılar KESİLMEZ (eski sınırla yeniden bağlanmak anlamsız); düzelince 3. seviye.
func TestSeviye3SinirYazilamazsaKesmez(t *testing.T) {
	o := yeniOrtam(t)
	_ = o.yon.baglan("a_db")
	_ = o.yon.baglan("a_db")
	o.taban()
	yuk := hizlar("a_db", 2.0)
	o.seviyeyeCik("a_db", 2, yuk)
	o.yon.alterHata["a_db@localhost"] = errSahte
	o.yon.alterHata["a_db@127.0.0.1"] = errSahte
	o.calis(20, yuk) // 3 dk dolar; her turda 3. seviye denenir ve başarısız olur
	k, _ := o.m.AktifKisit("a_db")
	if k.Seviye != 2 || k.KisitDeger != 2 || len(o.yon.kesilenler) != 0 || o.yon.baglantiSayisi("a_db") != 2 {
		t.Fatalf("sınır yazılamadıysa seviye 2'de kalıp bağlantı kesilmemeli: %+v kesilen=%v", k, o.yon.kesilenler)
	}
	if o.m.DurumAl().SonHata == "" {
		t.Fatal("hata görünür olmalı")
	}
	delete(o.yon.alterHata, "a_db@localhost")
	delete(o.yon.alterHata, "a_db@127.0.0.1")
	o.calis(2, yuk)
	k, _ = o.m.AktifKisit("a_db")
	if k.Seviye != 3 || k.KisitDeger != 1 || o.yon.baglantiSayisi("a_db") != 0 {
		t.Fatalf("hata giderilince 3. seviye: %+v", k)
	}
}

// Sürekli aşım: 3. seviyede kesintisiz aşım Soguma'dan uzun sürerse yöneticiye TEK bildirim
// (10 dk'da bir); otomatik kilit/askı yok, seviye 3'te kalır.
func TestSurekliAsimBildirimi(t *testing.T) {
	o := yeniOrtam(t)
	o.taban()
	yuk := hizlar("a_db", 2.0)
	o.seviyeyeCik("a_db", 3, yuk)
	if o.surekliAdet() != 0 {
		t.Fatal("3. seviyeye çıkar çıkmaz sürekli aşım bildirimi olmamalı")
	}
	o.calis(19, yuk) // 285 sn
	if o.surekliAdet() != 0 {
		t.Fatalf("Soguma (300 sn) dolmadan bildirim: %v", o.m.Olaylar())
	}
	o.calis(1, yuk) // 300 sn
	if o.surekliAdet() != 1 {
		t.Fatalf("300 sn sonra TEK bildirim bekleniyordu: %d", o.surekliAdet())
	}
	var mesaj string
	for _, e := range o.m.Olaylar() {
		if e.Tur == "bildirim" && e.Eylem == surekliBaslik {
			mesaj = e.Detay
			if e.Domain != 1 || e.Bayi != 0 {
				t.Fatalf("hosting sahibine gitmeli: %+v", e)
			}
		}
	}
	if !strings.Contains(mesaj, "izole") || !strings.Contains(mesaj, "askı") || !strings.Contains(mesaj, "a_db") {
		t.Fatalf("mesajda öneri ve kullanıcı olmalı: %q", mesaj)
	}
	o.calis(39, yuk) // +585 sn: 10 dk dolmadı
	if o.surekliAdet() != 1 {
		t.Fatalf("10 dk içinde tekrar bildirim: %d", o.surekliAdet())
	}
	o.calis(1, yuk) // +600 sn
	if o.surekliAdet() != 2 {
		t.Fatalf("10 dk sonra yeniden bildirim bekleniyordu: %d", o.surekliAdet())
	}
	if o.kisitSeviyesi("a_db") != 3 || o.depo.acikSayisi() != 1 {
		t.Fatal("seviye 3'te kalmalı, otomatik başka işlem yok")
	}
}

// Aşım kesintiye uğrarsa Soguma süresi baştan sayılır.
func TestSurekliAsimKesintiyleSifirlanir(t *testing.T) {
	o := yeniOrtam(t)
	o.taban()
	yuk := hizlar("a_db", 2.0)
	o.seviyeyeCik("a_db", 3, yuk)
	o.calis(12, yuk)
	o.calis(4, nil) // 60 sn eşik altı: aşım kesildi (kısıt sürer: soğuma 5 dk)
	if o.kisitSeviyesi("a_db") != 3 {
		t.Fatalf("kısıt sürmeliydi: %d", o.kisitSeviyesi("a_db"))
	}
	o.calis(19, yuk)
	if o.surekliAdet() != 0 {
		t.Fatalf("kesintiden sonra kesintisiz 300 sn dolmadan bildirim: %v", o.m.Olaylar())
	}
	o.calis(7, yuk)
	if o.surekliAdet() != 1 {
		t.Fatalf("kesintisiz 300 sn sonra bildirim bekleniyordu: %d", o.surekliAdet())
	}
}

// Bayi yolunda bildirim bayiye gider.
func TestSurekliAsimBayiBildirimi(t *testing.T) {
	o := yeniOrtam(t)
	o.hosting(11, 7, "h1.test", "h1_db")
	o.depo.bayi[7] = 0.5
	o.taban()
	yuk := hizlar("h1_db", 0.9)
	o.seviyeyeCik("h1_db", 3, yuk)
	o.calis(21, yuk)
	var n int
	for _, e := range o.m.Olaylar() {
		if e.Tur == "bildirim" && e.Eylem == surekliBaslik && e.Bayi == 7 && e.Domain == 0 {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("bayiye TEK sürekli aşım bildirimi bekleniyordu: %d (%v)", n, o.m.Olaylar())
	}
	if y := yoneticiSurekliAdet(o); y != 1 {
		t.Fatalf("izole kopya kararı yöneticide: yöneticiye TEK kopya bekleniyordu: %d (%v)", y, o.m.Olaylar())
	}
}

// yoneticiSurekliAdet: panel geneli (domain ve bayi kimliksiz) sürekli aşım bildirimleri.
func yoneticiSurekliAdet(o *ortam) int {
	n := 0
	for _, e := range o.m.Olaylar() {
		if e.Tur == "bildirim" && e.Eylem == surekliBaslik && e.Domain == 0 && e.Bayi == 0 {
			n++
		}
	}
	return n
}

// Bayiye ait hostingin kendi eşiği: bildirim hosting sahibine (bayi) ve yöneticiye; admin sahipli
// hostingde yönetici kopyası yazılmaz (hosting bildirimi zaten yöneticiye gider).
func TestSurekliAsimBayiHostingiYoneticiyeDeGider(t *testing.T) {
	o := yeniOrtam(t)
	o.hosting(12, 8, "h2.test", "h2_db")
	o.taban()
	yuk := hizlar("h2_db", 2.0)
	o.seviyeyeCik("h2_db", 3, yuk)
	o.calis(21, yuk)
	sahip := 0
	for _, e := range o.m.Olaylar() {
		if e.Tur == "bildirim" && e.Eylem == surekliBaslik && e.Domain == 12 {
			sahip++
		}
	}
	if sahip != 1 || yoneticiSurekliAdet(o) != 1 {
		t.Fatalf("sahip=%d yönetici=%d (1/1 bekleniyordu): %v", sahip, yoneticiSurekliAdet(o), o.m.Olaylar())
	}
}

func TestSurekliAsimAdminHostingiTekBildirim(t *testing.T) {
	o := yeniOrtam(t)
	o.taban()
	yuk := hizlar("a_db", 2.0)
	o.seviyeyeCik("a_db", 3, yuk)
	o.calis(21, yuk)
	if o.surekliAdet() != 1 || yoneticiSurekliAdet(o) != 0 {
		t.Fatalf("admin sahipli hostingde tek bildirim (hosting): toplam=%d yönetici kopyası=%d", o.surekliAdet(), yoneticiSurekliAdet(o))
	}
}

// Gözlem modunda 3. seviye hiç oluşmaz, bildirim de yok.
func TestSurekliAsimGozlemdeYok(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar.Mod = ModGozlem
	o.taban()
	o.calis(80, hizlar("a_db", 2.0))
	if o.surekliAdet() != 0 || o.yon.alterSayisi() != 0 || o.kisitSeviyesi("a_db") != 1 {
		t.Fatalf("gözlem modu tırmanmaz: seviye=%d alter=%v", o.kisitSeviyesi("a_db"), o.yon.alterler)
	}
}
