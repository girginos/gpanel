package dbgovernor

// mutasyon_test.go — mutasyon turunda (2026-10-13) kaçan mutantları öldüren sınır/durum testleri.

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Ayar sınırları: her anahtar için sınırın altı/üstü sınıra çekilir (bozuk değer güvenli aralıkta kalır).
func TestAyarSiniriTablosu(t *testing.T) {
	sn := func(s string, v string) Ayar { return ayarUygula(map[string]string{s: v}) }
	if a := sn(anEsik, "0.01"); a.EsikHosting != 0.1 {
		t.Fatalf("esik alt: %v", a.EsikHosting)
	}
	if a := sn(anEsik, "64.5"); a.EsikHosting != 64 {
		t.Fatalf("esik üst (sınırın hemen üstü): %v", a.EsikHosting)
	}
	if a := sn(anEsik, "1000"); a.EsikHosting != 64 {
		t.Fatalf("esik üst: %v", a.EsikHosting)
	}
	if a := sn(anBaglanti, "0"); a.KisitBaglanti != 1 {
		t.Fatalf("baglanti alt: %v", a.KisitBaglanti)
	}
	if a := sn(anBaglanti, "5000"); a.KisitBaglanti != 100 {
		t.Fatalf("baglanti üst: %v", a.KisitBaglanti)
	}
	if a := sn(anPoll, "1"); a.Poll != 5*time.Second {
		t.Fatalf("poll alt: %v", a.Poll)
	}
	if a := sn(anPoll, "9999"); a.Poll != 120*time.Second {
		t.Fatalf("poll üst: %v", a.Poll)
	}
	if a := sn(anPencere, "5000"); a.Pencere != 900*time.Second {
		t.Fatalf("pencere üst: %v", a.Pencere)
	}
	if a := sn(anPencere, "1"); a.Pencere != 30*time.Second { // 2*poll(15)
		t.Fatalf("pencere alt (2*poll): %v", a.Pencere)
	}
	if a := sn(anYukselme, "1"); a.Yukselme != 10*time.Second {
		t.Fatalf("yükselme alt: %v", a.Yukselme)
	}
	if a := sn(anYukselme, "99999"); a.Yukselme != 3600*time.Second {
		t.Fatalf("yükselme üst: %v", a.Yukselme)
	}
	if a := sn(anKill, "0"); a.KillEsigi != time.Second {
		t.Fatalf("kill alt: %v", a.KillEsigi)
	}
	if a := sn(anKill, "99999"); a.KillEsigi != 3600*time.Second {
		t.Fatalf("kill üst: %v", a.KillEsigi)
	}
	if a := sn(anSoguma, "1"); a.Soguma != 30*time.Second {
		t.Fatalf("soğuma alt: %v", a.Soguma)
	}
	if a := sn(anSoguma, "999999"); a.Soguma != 86400*time.Second {
		t.Fatalf("soğuma üst: %v", a.Soguma)
	}
	if a := sn(anUzun, "0"); a.UzunSorgu != time.Second {
		t.Fatalf("uzun alt: %v", a.UzunSorgu)
	}
	if a := sn(anUzun, "9999"); a.UzunSorgu != 600*time.Second {
		t.Fatalf("uzun üst: %v", a.UzunSorgu)
	}
	if a := sn(anKes, "0"); a.BaglantiKes != time.Minute {
		t.Fatalf("kes alt: %v", a.BaglantiKes)
	}
	if a := sn(anKes, "9999"); a.BaglantiKes != 120*time.Minute {
		t.Fatalf("kes üst: %v", a.BaglantiKes)
	}
	if a := sn(anMod, " UYGULA "); a.Mod != ModUygula {
		t.Fatalf("mod boşluk/büyük harf: %v", a.Mod)
	}
	if a := sn(anEsik, "NaN"); a.EsikHosting != 1.0 {
		t.Fatalf("NaN reddedilmeli: %v", a.EsikHosting)
	}
	d := VarsayilanAyar()
	if d.Mod != ModGozlem || d.Poll != 15*time.Second || d.Pencere != 60*time.Second || d.EsikHosting != 1 || d.KisitBaglanti != 2 ||
		d.Yukselme != 45*time.Second || d.KillEsigi != 10*time.Second || d.Soguma != 5*time.Minute || d.SogumaOrani != 0.8 ||
		d.UzunSorgu != 5*time.Second || d.BaglantiKes != 3*time.Minute {
		t.Fatalf("varsayılanlar değişti: %+v", d)
	}
}

func TestKucukSinirlar(t *testing.T) {
	if !okumaSorgusuMu("select") || !okumaSorgusuMu("  (SeLeCt 1)") || okumaSorgusuMu("selec") || okumaSorgusuMu("selectx") {
		t.Fatal("okumaSorgusuMu sınır durumları")
	}
	if !(&halka{ornekler: []ornek{{cpu: 0, canli: 0}}}).bos() {
		t.Fatal("sıfır etkinlikli halka boş sayılmalı")
	}
	if milli(2.0006) != 2001 {
		t.Fatalf("milli yuvarlama: %d", milli(2.0006))
	}
}

// Boşluk: 37,5 sn (2,5 yoklama) üstündeki aralık tek tur olarak ölçülmez (biriken sayaç sahte sıçrama olmaz).
func TestBoslukEsigi(t *testing.T) {
	o := yeniOrtam(t)
	o.taban()
	o.adim(50, hizlar("a_db", 3.0)) // 50 sn boşluk, 150 CPU-sn birikmiş
	o.adim(15, nil)
	if o.yon.alterSayisi() != 0 {
		t.Fatalf("uzun aralık sonrası biriken sayaç kısıt üretti: %v", o.yon.alterler)
	}
}

// Sayaçta ilk kez görünen kullanıcının ilk aralıktaki CPU'su sayılır; eşiğe eşit canlı sorgu eşiği sayar.
func TestYeniKullaniciVeCanliSinir(t *testing.T) {
	o := yeniOrtam(t)
	o.hosting(5, 0, "c.test", "c_db")
	delete(o.olcer.cum, "c_db")
	o.taban()
	o.calis(3, hizlar("c_db", 2.0)) // ilk tur yeni kullanıcı: 30 + 30 + 30 = 90 CPU-sn / 60 sn
	if !esitInts(o.yon.baglanti("c_db"), []int{2, 2}) {
		t.Fatalf("yeni kullanıcının ilk aralığı sayılmadı: %v", o.yon.baglanti("c_db"))
	}
	o2 := yeniOrtam(t)
	o2.taban()
	q := Sorgu{Kullanici: "a_db", Sure: 5 * time.Second, Durum: "Sending data", QueryID: 9, Metin: "SELECT 1"}
	o2.olcer.calisan = []Sorgu{q, q}
	o2.calis(6, nil) // iki uzun (tam eşik süresi) CPU sorgusu = 2 çekirdek
	if !esitInts(o2.yon.baglanti("a_db"), []int{2, 2}) {
		t.Fatalf("eşik süresine eşit canlı sorgu sayılmalı: %v", o2.yon.baglanti("a_db"))
	}
}

// Bayi sınırı: toplam == sınır kısmaz; sınırı aşınca sınırın altına inmeye yetecek kadar hosting kısılır
// (eşitlikte durur); askıdaki hosting bayi yolundan da kısılmaz.
func TestBayiSinirDegerleri(t *testing.T) {
	kur := func(t *testing.T, limit float64, ek bool) *ortam {
		o := yeniOrtam(t)
		o.hosting(3, 7, "x.test", "x_db")
		o.hosting(4, 7, "y.test", "y_db")
		if ek {
			o.hosting(5, 7, "z.test", "z_db")
		}
		o.depo.bayi[7] = limit
		return o
	}
	o := kur(t, 2.0, false)
	o.taban()
	o.calis(10, hizlar("x_db", 1.0, "y_db", 1.0))
	if o.yon.alterSayisi() != 0 {
		t.Fatalf("toplam == sınır kısıt üretmemeli: %v", o.yon.alterler)
	}
	o = kur(t, 2.0, true)
	o.taban()
	o.calis(8, hizlar("x_db", 1.0, "y_db", 1.0, "z_db", 1.0))
	if !esitInts(o.yon.baglanti("x_db"), []int{2, 2}) || !esitInts(o.yon.baglanti("y_db"), []int{0, 0}) {
		t.Fatalf("x kısılınca kalan == sınır: y kısılmamalı: x=%v y=%v z=%v", o.yon.baglanti("x_db"), o.yon.baglanti("y_db"), o.yon.baglanti("z_db"))
	}
	o = kur(t, 1.5, false)
	o.depo.kimlik["x_db"] = Kimlik{DomainID: 3, ResellerID: 7, AlanAdi: "x.test", Askida: true}
	o.taban()
	o.calis(8, hizlar("x_db", 0.95, "y_db", 0.95))
	for _, al := range o.yon.alterler {
		if strings.HasPrefix(al, "x_db@") {
			t.Fatalf("askıdaki hosting bayi yolundan kısılmamalı (kısılıp hemen bırakılmamalı da): %v", o.yon.alterler)
		}
	}
	if !esitInts(o.yon.baglanti("y_db"), []int{2, 2}) {
		t.Fatalf("askıda olmayan hosting kısılmalı: %v", o.yon.baglanti("y_db"))
	}
}

// Kısıt ömrü boyunca Tepe en yüksek ölçümdür; kapalı kipte ölçüm yazılmaz; sıfır CPU satırı yazılmaz;
// hata durumu sonraki başarılı turda temizlenir.
func TestTepeKapaliKipOlcumSatirlari(t *testing.T) {
	o := yeniOrtam(t)
	o.taban()
	o.calis(6, hizlar("a_db", 2.0))
	o.calis(4, hizlar("a_db", 0.9))
	if k, ok := o.m.AktifKisit("a_db"); !ok || k.Tepe < 1.9 {
		t.Fatalf("Tepe en yüksek ölçümü korumalı: %+v", k)
	}
	o.calis(8, nil)
	for _, s := range o.depo.olcum {
		if s.CPUms <= 0 {
			t.Fatalf("sıfır CPU satırı yazıldı: %+v", s)
		}
	}
	n := len(o.depo.olcum)
	o.ayar.Mod = ModKapali
	o.calis(3, hizlar("a_db", 2.0))
	if len(o.depo.olcum) != n || len(o.m.AktifKullanicilar()) != 0 {
		t.Fatalf("kapalı kipte ölçüm yazıldı ya da kısıt kaldı: %d→%d %v", n, len(o.depo.olcum), o.m.AktifKullanicilar())
	}
	o.ayarH = errSahte
	o.ayar.Mod = ModUygula
	o.calis(2, hizlar("a_db", 1.0))
	if o.m.DurumAl().SonHata == "" {
		t.Fatal("hata kaydı yok")
	}
	o.ayarH = nil
	o.calis(2, hizlar("a_db", 1.0))
	if h := o.m.DurumAl().SonHata; h != "" {
		t.Fatalf("hata durumu başarılı turda temizlenmeli: %q", h)
	}
}

// Seviye 2 -> 3 tam 3 dk (12 tur) kesintisiz aşımda; seviye 2'de KILL QUERY sınırları (>= süre, QueryID>0).
func TestSeviyeSureleriVeKill(t *testing.T) {
	o := yeniOrtam(t)
	o.taban()
	n2, n3 := 0, 0
	for i := 1; i <= 40 && n3 == 0; i++ {
		o.adim(15, hizlar("a_db", 2.0))
		if s := o.kisitSeviyesi("a_db"); s == 2 && n2 == 0 {
			n2 = i
		} else if s == 3 {
			n3 = i
		}
	}
	if n2 == 0 || n3-n2 != 12 {
		t.Fatalf("3. seviye 2. seviyeden tam 180 sn sonra olmalı: n2=%d n3=%d", n2, n3)
	}
	o = yeniOrtam(t)
	o.taban()
	o.calis(7, hizlar("a_db", 2.0))
	o.olcer.calisan = []Sorgu{
		{Kullanici: "a_db", Sure: 10 * time.Second, QueryID: 77, Durum: "Sending data", Metin: "SELECT * FROM t"},
		{Kullanici: "a_db", Sure: 9*time.Second + 900*time.Millisecond, QueryID: 78, Durum: "Sending data", Metin: "SELECT * FROM t"},
		{Kullanici: "a_db", Sure: 20 * time.Second, QueryID: 0, Durum: "Sending data", Metin: "SELECT * FROM t"},
		{Kullanici: "a_db", Sure: 20 * time.Second, QueryID: 79, Durum: "Sending data", Metin: "UPDATE t SET a=1"},
	}
	o.adim(15, hizlar("a_db", 2.0))
	if len(o.yon.killler) != 1 || o.yon.killler[0] != 77 {
		t.Fatalf("yalnız eşiğe eşit süreli okuma sorgusu öldürülmeli: %v", o.yon.killler)
	}
}

// Kısıt kayıt/ALTER durumları: hesap zaten kısıt değerindeyse ALTER yok; bir host başarısızsa kısıt yine
// kayıtlı; kaldırmada yalnız farklı hesaplara yazılır; kalan kısıt yoksa tek "kaldırıldı" bildirimi; askı nedeniyle
// bırakılırken bildirim yok.
func TestKisitKayitVeBirakmaDurumlari(t *testing.T) {
	o := yeniOrtam(t)
	o.yon.hesap["a_db"] = []Hesap{{Host: "localhost", Baglanti: 2}, {Host: "127.0.0.1", Baglanti: 2}}
	o.taban()
	o.calis(4, hizlar("a_db", 2.0))
	if o.yon.alterSayisi() != 0 || len(o.m.AktifKullanicilar()) != 1 {
		t.Fatalf("zaten kısıt değerindeki hesaba ALTER yazılmamalı: %v", o.yon.alterler)
	}

	o = yeniOrtam(t)
	o.yon.alterHata["a_db@127.0.0.1"] = errSahte
	o.taban()
	o.calis(4, hizlar("a_db", 2.0))
	if len(o.m.AktifKullanicilar()) != 1 || o.depo.acikSayisi() != 1 {
		t.Fatalf("bir host başarılıysa kısıt kayıtlı kalmalı: %v", o.m.AktifKullanicilar())
	}

	o = yeniOrtam(t)
	o.taban()
	o.calis(5, hizlar("b_db", 2.0, "b_wp", 2.0))
	if len(o.m.AktifKullanicilar()) != 2 {
		t.Fatalf("iki kullanıcı kısılmalı: %v", o.m.AktifKullanicilar())
	}
	n := o.yon.alterSayisi()
	o.m.mu.Lock()
	kb := o.m.aktif["b_db"]
	o.m.mu.Unlock()
	o.yon.mu.Lock()
	o.yon.hesap["b_db"][1].Baglanti = 0 // biri zaten eski değerinde
	o.yon.mu.Unlock()
	if !o.m.birak(context.Background(), o.simdi, kb, "soguma") {
		t.Fatal("bırakma başarısız")
	}
	if got := o.yon.alterSayisi() - n; got != 1 {
		t.Fatalf("geri yazmada yalnız farklı hesaba ALTER (1): %d (%v)", got, o.yon.alterler[n:])
	}
	o.calis(30, nil)
	if o.olayAdet("bildirim", "Veritabanı CPU kısıtı kaldırıldı") != 1 || o.olayAdet("denetim", "db.cpu_kisit_kaldir") != 1 {
		t.Fatalf("hosting başına tek kaldırıldı bildirimi+denetimi: %v", o.m.Olaylar())
	}

	o = yeniOrtam(t)
	o.taban()
	o.calis(4, hizlar("a_db", 2.0))
	o.depo.kimlik["a_db"] = Kimlik{DomainID: 1, AlanAdi: "a.test", Askida: true}
	o.calis(6, nil) // kimlik önbelleği dolunca askı görülür
	if len(o.m.AktifKullanicilar()) != 0 || o.olayAdet("bildirim", "Veritabanı CPU kısıtı kaldırıldı") != 0 {
		t.Fatalf("askı nedeniyle bırakmada kısıt kalkar, 'kaldırıldı' bildirimi gitmez: %v", o.m.Olaylar())
	}
}

// Açılışta bulunan kısıt geri alınamazsa (Hesaplar hatası) Kurtar işaretiyle kalır ve son aşım yeni olsa
// bile ilk fırsatta bırakılır.
func TestKurtarIsaretiTekrarDener(t *testing.T) {
	o := yeniOrtam(t)
	ctx := context.Background()
	k := &Kisit{Kullanici: "a_db", Kimlik: Kimlik{DomainID: 1, AlanAdi: "a.test"}, Birimler: []BirimRef{{Tur: "hosting", ID: 1, Esik: 1}},
		Seviye: 1, KisitDeger: 2, Uygulandi: true, Baslangic: o.simdi, SonAsim: o.simdi}
	if _, err := o.depo.Ac(ctx, k); err != nil {
		t.Fatal(err)
	}
	o.yon.hesap["a_db"] = []Hesap{{Host: "localhost", Baglanti: 2}, {Host: "127.0.0.1", Baglanti: 2}}
	o.yon.hesapHata = errSahte
	o.m.Kurtar(ctx)
	if len(o.m.AktifKullanicilar()) != 1 {
		t.Fatalf("geri alınamayan kısıt bellekte kalmalı: %v", o.m.AktifKullanicilar())
	}
	o.yon.hesapHata = nil
	o.taban()
	if len(o.m.AktifKullanicilar()) != 0 || !esitInts(o.yon.baglanti("a_db"), []int{0, 0}) {
		t.Fatalf("Kurtar işaretli kısıt ilk turda bırakılmalı: %v %v", o.m.AktifKullanicilar(), o.yon.baglanti("a_db"))
	}
	if nd := o.depo.bitisNedenleri(); len(nd) != 1 || nd[0] != "acilis" {
		t.Fatalf("bitiş nedeni acilis: %v", nd)
	}
}

// userstat denetimi: açıkken 40 turda bir sorgulanır (her turda değil).
type sayanYon struct {
	*sahteYon
	n int
}

func (s *sayanYon) UserstatAcikMi(ctx context.Context) (bool, error) {
	s.n++
	return s.sahteYon.UserstatAcikMi(ctx)
}

func TestUserstatDenetimAraligi(t *testing.T) {
	o := yeniOrtam(t)
	sy := &sayanYon{sahteYon: o.yon}
	o.m.Yon = sy
	o.taban()
	o.calis(100, nil)
	// Açıkken HER tur: dışarıdan kapatılma bir sonraki turda görülsün (sayaçlar donmasın).
	if sy.n < 100 {
		t.Fatalf("userstat açıkken her turda denetlenmeli: %d çağrı / 101 tur", sy.n)
	}
	// Açılamıyorken yeniden deneme ~10 turda bir (her tur SET GLOBAL denenmez).
	o2 := yeniOrtam(t)
	o2.yon.userstat = false
	o2.yon.userstatAcHt = errSahte
	sy2 := &sayanYon{sahteYon: o2.yon}
	o2.m.Yon = sy2
	o2.taban()
	o2.calis(100, nil)
	if sy2.n < 10 || sy2.n > 12 {
		t.Fatalf("açılamıyorken ~10 turda bir denetlenmeli: %d çağrı / 101 tur", sy2.n)
	}
}
