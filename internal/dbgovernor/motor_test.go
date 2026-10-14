package dbgovernor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// taban: ilk tur yalnız taban alır (kümülatif sayaçlar ölçüm dışı kalır).
func (o *ortam) taban() { o.adim(15, nil) }

func hizlar(kv ...any) map[string]float64 {
	m := map[string]float64{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1].(float64)
	}
	return m
}

// Eşik altı → hiçbir işlem: ne ALTER, ne kayıt, ne bildirim/denetim.
func TestEsikAltiIslemYok(t *testing.T) {
	o := yeniOrtam(t)
	o.taban()
	o.calis(12, hizlar("a_db", 0.6, "b_db", 0.3)) // 3 dk boyunca 0,6 çekirdek (< 1,0)
	if o.yon.alterSayisi() != 0 || len(o.yon.killler) != 0 {
		t.Fatalf("eşik altında MariaDB'ye dokunuldu: alter=%v kill=%v", o.yon.alterler, o.yon.killler)
	}
	if o.depo.acikSayisi() != 0 || len(o.m.Olaylar()) != 0 || len(o.m.AktifKullanicilar()) != 0 {
		t.Fatalf("eşik altında kısıt/olay oluştu: %v", o.m.Olaylar())
	}
	if len(o.depo.olcum) == 0 {
		t.Fatal("ölçüm dilimi yazılmadı (en çok CPU raporu için)")
	}
}

// Eşik üstü → 1. seviye (bağlantı sınırı) sonra 2. seviye (uzun OKUMA sorgusu KILL); yazma ve
// genç sorgu ve başka kullanıcı DOKUNULMAZ.
func TestEsikUstuKademeliKisma(t *testing.T) {
	o := yeniOrtam(t)
	o.taban()
	// Pencere dolana kadar eşik aşılmaz: 2 örnek = 1,0 (> değil), 3. örnekte 1,5.
	o.calis(2, hizlar("a_db", 2.0))
	if len(o.m.AktifKullanicilar()) != 0 {
		t.Fatalf("pencere dolmadan kısıldı: %v", o.m.AktifKullanicilar())
	}
	o.calis(1, hizlar("a_db", 2.0))
	if got := o.m.AktifKullanicilar(); len(got) != 1 || got[0] != "a_db" {
		t.Fatalf("1. örnek eşik üstü: kısıt bekleniyordu, aktif=%v", got)
	}
	k, _ := o.m.AktifKisit("a_db")
	if k.Seviye != 1 || k.KisitDeger != 2 || k.Onceki != 0 || !k.Uygulandi {
		t.Fatalf("1. seviye kısıt yanlış: %+v", k)
	}
	if got := o.yon.baglanti("a_db"); !esitInts(got, []int{2, 2}) {
		t.Fatalf("MAX_USER_CONNECTIONS her hesapta (localhost + 127.0.0.1) 2 olmalı: %v", got)
	}
	if got := o.yon.baglanti("b_db"); !esitInts(got, []int{0, 0}) {
		t.Fatalf("başka hostingin kullanıcısına dokunuldu: %v", got)
	}
	if o.olayAdet("denetim", "db.cpu_kisit") != 1 || o.olayAdet("bildirim", "Veritabanı CPU kısıtı uygulandı") != 1 {
		t.Fatalf("denetim+bildirim bekleniyordu: %v", o.m.Olaylar())
	}

	// çalışan sorgular: a_db'den uzun SELECT, uzun UPDATE, genç SELECT; b_db'den uzun SELECT
	o.olcer.calisan = []Sorgu{
		{QueryID: 101, Kullanici: "a_db", Sure: 30 * time.Second, Durum: "Sending data", Metin: "SELECT * FROM wp_posts"},
		{QueryID: 102, Kullanici: "a_db", Sure: 30 * time.Second, Durum: "Updating", Metin: "UPDATE wp_posts SET x=1"},
		{QueryID: 103, Kullanici: "a_db", Sure: 3 * time.Second, Durum: "Sending data", Metin: "SELECT 1"},
		{QueryID: 104, Kullanici: "b_db", Sure: 90 * time.Second, Durum: "Sending data", Metin: "SELECT * FROM big"},
		{QueryID: 105, Kullanici: "a_db", Sure: 40 * time.Second, Durum: "Sending data", Metin: "WITH x AS (SELECT 1) DELETE FROM t"},
	}
	// 1. seviye sürerken (Yukselme=45 sn dolmadan) KILL yok
	o.calis(2, hizlar("a_db", 2.0))
	if len(o.yon.killler) != 0 {
		t.Fatalf("2. seviyeden önce KILL: %v", o.yon.killler)
	}
	o.calis(1, hizlar("a_db", 2.0))
	k, _ = o.m.AktifKisit("a_db")
	if k.Seviye != 2 {
		t.Fatalf("45 sn sonra 2. seviye bekleniyordu: %+v", k)
	}
	if len(o.yon.killler) == 0 || o.yon.killler[0] != 101 {
		t.Fatalf("uzun SELECT öldürülmeli: %v", o.yon.killler)
	}
	for _, q := range o.yon.killler {
		if q != 101 {
			t.Fatalf("yalnız a_db'nin uzun SELECT'i öldürülmeli; öldürülen: %v", o.yon.killler)
		}
	}
	if o.olayAdet("denetim", "db.cpu_kisit_yukselt") != 1 {
		t.Fatalf("yükselme denetimi yok: %v", o.m.Olaylar())
	}
}

// Soğuma sonrası eski değere dönüş + kayıt kapanır + kaldırma bildirimi.
func TestSogumaSonrasiGeriDonus(t *testing.T) {
	o := yeniOrtam(t)
	// önceki değer 7 (sınırsız değil): kısıt 2'ye indirir, sonunda 7'ye döner
	for i := range o.yon.hesap["a_db"] {
		o.yon.hesap["a_db"][i].Baglanti = 7
	}
	o.taban()
	o.calis(4, hizlar("a_db", 2.0))
	if got := o.yon.baglanti("a_db"); !esitInts(got, []int{2, 2}) {
		t.Fatalf("kısıt uygulanmadı: %v", got)
	}
	k, _ := o.m.AktifKisit("a_db")
	if k.Onceki != 7 {
		t.Fatalf("önceki değer kaydedilmedi: %+v", k)
	}
	// yük kesilir. 4 örnekten sonra pencere boşalır; son kanıt ≈ +30 sn; soğuma 300 sn
	o.calis(8, hizlar("a_db", 0.0)) // 120 sn: hâlâ kısıtlı
	if len(o.m.AktifKullanicilar()) != 1 {
		t.Fatal("soğuma dolmadan kısıt kalktı")
	}
	o.calis(14, hizlar("a_db", 0.0)) // +210 sn → toplam >300 sn
	if len(o.m.AktifKullanicilar()) != 0 {
		t.Fatalf("soğuma sonrası kısıt kalkmalıydı: %v", o.m.AktifKullanicilar())
	}
	if got := o.yon.baglanti("a_db"); !esitInts(got, []int{7, 7}) {
		t.Fatalf("eski değer (7) geri yazılmadı: %v", got)
	}
	if n := o.depo.bitisNedenleri(); len(n) != 1 || n[0] != "soguma" {
		t.Fatalf("kayıt 'soguma' ile kapanmalı: %v", n)
	}
	if o.olayAdet("denetim", "db.cpu_kisit_kaldir") != 1 || o.olayAdet("bildirim", "Veritabanı CPU kısıtı kaldırıldı") != 1 {
		t.Fatalf("kaldırma denetimi/bildirimi eksik: %v", o.m.Olaylar())
	}
}

// Kısıt kullanıcının mevcut sınırını GEVŞETMEZ: önceki 1 (< 2) ise 1 kalır.
func TestKisitMevcutDahaDarSiniriGevsetmez(t *testing.T) {
	if kisitDegeri(2, 1) != 1 || kisitDegeri(2, 0) != 2 || kisitDegeri(2, 2) != 2 || kisitDegeri(2, 9) != 2 {
		t.Fatal("kisitDegeri yanlış")
	}
}

// Sistem/servis kullanıcıları ve güvensiz adlar: ne ölçülür ne kısılır, SQL'e ASLA girmez.
func TestSistemKullanicisiAsla(t *testing.T) {
	o := yeniOrtam(t)
	sistem := []string{"root", "panel", "pma", "roundcube", "mailadmin", "pureftpd", "mysql", "debian-sys-maint", "mariadb.sys", "healthcheck"}
	kotu := []string{"a'; DROP USER x;--", "x y", "", "a`b", strings.Repeat("a", 65)} // 65 > panelin 64 sınırı
	for _, u := range append(append([]string{}, sistem...), kotu...) {
		// panelde bir hostinge BAĞLIYMIŞ gibi göster: ikinci savunma hattı yine kısmamalı
		o.depo.kimlik[u] = Kimlik{DomainID: 1, AlanAdi: "a.test"}
		o.yon.hesap[u] = []Hesap{{Host: "localhost"}}
		o.olcer.cum[u] = 0
	}
	o.depo.domKul[1] = append([]string{"a_db"}, append(append([]string{}, sistem...), kotu...)...)
	o.taban()
	h := map[string]float64{}
	for _, u := range append(append([]string{}, sistem...), kotu...) {
		h[u] = 3.0
	}
	o.calis(8, h)
	if o.yon.alterSayisi() != 0 || o.depo.acikSayisi() != 0 || len(o.m.AktifKullanicilar()) != 0 {
		t.Fatalf("sistem/güvensiz kullanıcıya dokunuldu: alter=%v aktif=%v", o.yon.alterler, o.m.AktifKullanicilar())
	}
	// bir hosting gerçekten aşınca da yalnız KİRACI kullanıcıları kısılır (domKul listesindeki sistem adları atlanır)
	o.calis(4, hizlar("a_db", 2.0))
	for _, a := range o.yon.alterler {
		if !strings.HasPrefix(a, "a_db@") {
			t.Fatalf("a_db dışında ALTER: %v", o.yon.alterler)
		}
	}
	if len(o.yon.alterler) != 2 {
		t.Fatalf("a_db için 2 hesap ALTER bekleniyordu: %v", o.yon.alterler)
	}
}

// Hostingin birden çok DB kullanıcısı birlikte ölçülür ve birlikte kısılır.
func TestHostingToplamiVeTumKullanicilar(t *testing.T) {
	o := yeniOrtam(t)
	o.taban()
	// b_db 0,7 + b_wp 0,6 = 1,3 çekirdek: tek tek eşik altı, toplamı eşik üstü
	o.calis(5, hizlar("b_db", 0.7, "b_wp", 0.6))
	got := o.m.AktifKullanicilar()
	if len(got) != 2 || got[0] != "b_db" || got[1] != "b_wp" {
		t.Fatalf("hostingin tüm kullanıcıları kısılmalı: %v", got)
	}
	if o.olayAdet("denetim", "db.cpu_kisit") != 1 {
		t.Fatalf("hosting başına TEK denetim/bildirim bekleniyordu: %v", o.m.Olaylar())
	}
	if len(o.m.AktifKullanicilar()) == 0 || o.yon.baglanti("a_db")[0] != 0 {
		t.Fatal("başka hosting etkilendi")
	}
}

// Bayi toplamı: hosting eşiklerinin altında kalan hostinglerin toplamı bayi sınırını aşarsa en
// büyük tüketici kısılır (çıkarılınca bayi sınırın altına iner).
func TestBayiToplamiKisma(t *testing.T) {
	o := yeniOrtam(t)
	o.hosting(11, 7, "h1.test", "h1_db")
	o.hosting(12, 7, "h2.test", "h2_db")
	o.hosting(13, 7, "h3.test", "h3_db")
	o.depo.bayi[7] = 1.0
	o.taban()
	// 0,8 + 0,5 + 0,3 = 1,6 > 1,0; hiçbiri kendi eşiğini (1,0) aşmıyor
	o.calis(5, hizlar("h1_db", 0.8, "h2_db", 0.5, "h3_db", 0.3))
	got := o.m.AktifKullanicilar()
	if len(got) != 1 || got[0] != "h1_db" {
		t.Fatalf("yalnız en büyük tüketici (h1) kısılmalı: %v", got)
	}
	k, _ := o.m.AktifKisit("h1_db")
	if k.nedenMetni() != "bayi" {
		t.Fatalf("neden bayi olmalı: %s", k.nedenMetni())
	}
	// bayi bildirimi bayiye gider (hosting hedefli değil)
	var bayiBildirimi bool
	for _, e := range o.m.Olaylar() {
		if e.Tur == "bildirim" && e.Bayi == 7 && e.Domain == 0 {
			bayiBildirimi = true
		}
	}
	if !bayiBildirimi {
		t.Fatalf("bayi bildirimi yok: %v", o.m.Olaylar())
	}
	// bayi sınırı yoksa (cpu_cekirdek tanımsız) hiç kısılmaz
	o2 := yeniOrtam(t)
	o2.hosting(11, 7, "h1.test", "h1_db")
	o2.hosting(12, 7, "h2.test", "h2_db")
	o2.taban()
	o2.calis(6, hizlar("h1_db", 0.9, "h2_db", 0.9))
	if len(o2.m.AktifKullanicilar()) != 0 {
		t.Fatalf("bayi sınırı yokken kısıldı: %v", o2.m.AktifKullanicilar())
	}
}

// Açılışta geri yükleme: önceki çalışmadan kalan AÇIK kısıtlar geri alınır.
func TestAcilistaGeriYukleme(t *testing.T) {
	o := yeniOrtam(t)
	o.yon.hesap["a_db"] = []Hesap{{Host: "localhost", Baglanti: 2}, {Host: "127.0.0.1", Baglanti: 2}}
	o.depo.kayit[1] = &sahteKayit{acik: true, k: Kisit{KayitID: 1, Kullanici: "a_db", Kimlik: Kimlik{DomainID: 1, AlanAdi: "a.test"},
		Birimler: []BirimRef{{Tur: "hosting", ID: 1, Esik: 1}}, Seviye: 2, Onceki: 5, KisitDeger: 2, Uygulandi: true}}
	o.depo.sonID = 1
	o.m.Kurtar(context.Background())
	if got := o.yon.baglanti("a_db"); !esitInts(got, []int{5, 5}) {
		t.Fatalf("önceki değer (5) geri yazılmalı: %v", got)
	}
	if o.depo.acikSayisi() != 0 || len(o.m.AktifKullanicilar()) != 0 {
		t.Fatal("kayıt kapanmalı")
	}
	if n := o.depo.bitisNedenleri(); len(n) != 1 || n[0] != "acilis" {
		t.Fatalf("bitiş nedeni 'acilis' olmalı: %v", n)
	}
	// planlı hostingde GÜNCEL plan değeri yazılır (plan kısıt sırasında değişmiş olabilir)
	o2 := yeniOrtam(t)
	o2.yon.hesap["a_db"] = []Hesap{{Host: "localhost", Baglanti: 2}}
	o2.depo.plan[1] = 12
	o2.depo.kayit[1] = &sahteKayit{acik: true, k: Kisit{KayitID: 1, Kullanici: "a_db", Kimlik: Kimlik{DomainID: 1}, Onceki: 5, KisitDeger: 2, Uygulandi: true}}
	o2.m.Kurtar(context.Background())
	if got := o2.yon.baglanti("a_db"); !esitInts(got, []int{12}) {
		t.Fatalf("güncel plan değeri (12) yazılmalı: %v", got)
	}
	// ALTER başarısızsa kayıt AÇIK kalır; sonraki turda yeniden denenir ve başarınca kapanır
	o3 := yeniOrtam(t)
	o3.yon.hesap["a_db"] = []Hesap{{Host: "localhost", Baglanti: 2}}
	o3.yon.alterHata["a_db@localhost"] = errSahte
	o3.depo.kayit[1] = &sahteKayit{acik: true, k: Kisit{KayitID: 1, Kullanici: "a_db", Kimlik: Kimlik{DomainID: 1}, Onceki: 5, KisitDeger: 2, Uygulandi: true}}
	o3.m.Kurtar(context.Background())
	if o3.depo.acikSayisi() != 1 {
		t.Fatal("geri yazılamayan kısıt açık kalmalı")
	}
	delete(o3.yon.alterHata, "a_db@localhost")
	o3.taban()
	if o3.depo.acikSayisi() != 0 || !esitInts(o3.yon.baglanti("a_db"), []int{5}) {
		t.Fatalf("yeniden deneme başarısız: açık=%d bağlantı=%v", o3.depo.acikSayisi(), o3.yon.baglanti("a_db"))
	}
}

// Fail-safe: ölçüm/ayar/eşleme okunamazsa YENİ kısıt konmaz; var olan kısıt süre dolunca kalkar.
func TestFailSafe(t *testing.T) {
	// a) MariaDB ölçümü okunamıyor
	o := yeniOrtam(t)
	o.taban()
	o.calis(2, hizlar("a_db", 2.0))
	o.olcer.hata = errSahte
	o.calis(10, hizlar("a_db", 5.0))
	if o.yon.alterSayisi() != 0 || len(o.m.AktifKullanicilar()) != 0 {
		t.Fatalf("ölçüm okunamazken kısıt kondu: %v", o.yon.alterler)
	}
	if o.m.DurumAl().SonHata == "" {
		t.Fatal("hata durumda görünmeli")
	}
	// b) eşleme (panel DB) okunamıyor
	o = yeniOrtam(t)
	o.taban()
	o.depo.esleHata = errSahte
	o.calis(8, hizlar("a_db", 3.0))
	if o.yon.alterSayisi() != 0 {
		t.Fatalf("eşleme okunamazken kısıt kondu: %v", o.yon.alterler)
	}
	// c) ayar okunamıyor
	o = yeniOrtam(t)
	o.taban()
	o.ayarH = errSahte
	o.calis(8, hizlar("a_db", 3.0))
	if o.yon.alterSayisi() != 0 {
		t.Fatalf("ayar okunamazken kısıt kondu: %v", o.yon.alterler)
	}
	// d) kısıt varken ölçüm okunamıyor → süre dolunca YİNE de kalkar
	o = yeniOrtam(t)
	o.taban()
	o.calis(4, hizlar("a_db", 2.0))
	if len(o.m.AktifKullanicilar()) != 1 {
		t.Fatal("kısıt bekleniyordu")
	}
	o.olcer.hata = errSahte
	o.calis(25, nil) // 375 sn
	if len(o.m.AktifKullanicilar()) != 0 || !esitInts(o.yon.baglanti("a_db"), []int{0, 0}) {
		t.Fatalf("ölçüm hatasında bile süre dolunca kısıt kalkmalı: aktif=%v bağlantı=%v", o.m.AktifKullanicilar(), o.yon.baglanti("a_db"))
	}
	// e) kayıt (db_cpu_kisit) yazılamıyorsa kısıt konmaz (panel çökerse geri alınamazdı)
	o = yeniOrtam(t)
	o.depo.acHata = errSahte
	o.taban()
	o.calis(5, hizlar("a_db", 3.0))
	if o.yon.alterSayisi() != 0 || len(o.m.AktifKullanicilar()) != 0 {
		t.Fatalf("kayıt yazılamazken kısıt kondu: %v", o.yon.alterler)
	}
}

// Bir kullanıcının hatası ötekileri etkilemez: ALTER hatası / mysql.user'da hesap yok.
func TestKullaniciHatasiOtekileriEtkilemez(t *testing.T) {
	o := yeniOrtam(t)
	o.depo.domKul[2] = []string{"b_db", "b_wp"}
	o.yon.alterHata["b_db@localhost"] = errSahte
	o.yon.alterHata["b_db@127.0.0.1"] = errSahte
	o.taban()
	o.calis(5, hizlar("b_db", 0.7, "b_wp", 0.6))
	if got := o.m.AktifKullanicilar(); len(got) != 1 || got[0] != "b_wp" {
		t.Fatalf("b_db'nin ALTER hatası b_wp'yi engellememeli: %v", got)
	}
	if got := o.yon.baglanti("b_wp"); !esitInts(got, []int{2, 2}) {
		t.Fatalf("b_wp kısılmalı: %v", got)
	}
	// mysql.user'da hesabı olmayan kullanıcı atlanır
	o = yeniOrtam(t)
	delete(o.yon.hesap, "b_db")
	o.taban()
	o.calis(5, hizlar("b_db", 0.7, "b_wp", 0.6))
	if got := o.m.AktifKullanicilar(); len(got) != 1 || got[0] != "b_wp" {
		t.Fatalf("hesabı olmayan kullanıcı atlanmalı: %v", got)
	}
}

// Gözlem modu: ölçer, kaydeder, API'de görünür; MariaDB'ye HİÇBİR değişiklik yapmaz.
func TestGozlemModuDokunmaz(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar.Mod = ModGozlem
	o.olcer.calisan = []Sorgu{{QueryID: 9, Kullanici: "a_db", Sure: time.Minute, Durum: "Sending data", Metin: "SELECT 1"}}
	o.taban()
	o.calis(8, hizlar("a_db", 2.0))
	if o.yon.alterSayisi() != 0 || len(o.yon.killler) != 0 {
		t.Fatalf("gözlem modunda MariaDB değişti: %v %v", o.yon.alterler, o.yon.killler)
	}
	k, ok := o.m.AktifKisit("a_db")
	if !ok || k.Uygulandi {
		t.Fatalf("gözlem kaydı (uygulandi=false) bekleniyordu: %+v %v", k, ok)
	}
	// kapalı mod: açık kısıt geri alınır, hiçbir şey ölçülmez
	o2 := yeniOrtam(t)
	o2.taban()
	o2.calis(4, hizlar("a_db", 2.0))
	if len(o2.m.AktifKullanicilar()) != 1 {
		t.Fatal("kısıt bekleniyordu")
	}
	o2.ayar.Mod = ModKapali
	o2.calis(1, hizlar("a_db", 2.0))
	if len(o2.m.AktifKullanicilar()) != 0 || !esitInts(o2.yon.baglanti("a_db"), []int{0, 0}) {
		t.Fatal("kapalı modda kısıt geri alınmalı")
	}
	if n := o2.depo.bitisNedenleri(); len(n) != 1 || n[0] != "kapali" {
		t.Fatalf("bitiş nedeni 'kapali': %v", n)
	}
}

// Tek bir tamamlanan uzun deyimin CPU'su (sayaçta tek sıçrama) sahte kısıt tetiklemez; uzun
// kesinti sonrası biriken sayaç da aralığa yüklenmez.
func TestSicramaVeBoslukSahteKisitUretmez(t *testing.T) {
	o := yeniOrtam(t)
	o.taban()
	// 15 sn'lik aralıkta 100 CPU-sn (fiziksel olarak imkansız: 4 çekirdek x 15 = 60): kırpılır
	o.adim(15, hizlar("a_db", 100.0/15))
	o.calis(5, nil)
	if o.yon.alterSayisi() != 0 {
		t.Fatalf("tek sıçrama kısıt tetikledi: %v", o.yon.alterler)
	}
	// 10 dk kesinti + 5000 CPU-sn birikmiş sayaç
	o.adim(600, hizlar("a_db", 5000.0/600))
	o.calis(2, hizlar("a_db", 0.1))
	if o.yon.alterSayisi() != 0 {
		t.Fatalf("kesinti sonrası biriken sayaç kısıt tetikledi: %v", o.yon.alterler)
	}
	// MariaDB yeniden başlamış: sayaç düşer — negatif delta olmaz
	o.olcer.cum["a_db"] = 3
	o.calis(3, nil)
	if o.yon.alterSayisi() != 0 {
		t.Fatalf("sayaç sıfırlanması kısıt tetikledi: %v", o.yon.alterler)
	}
}

// Anlık gösterge: CPU_TIME deyim BİTİNCE yazılır; bitmeyen uzun deyimler sayaçta görünmez.
// Eşzamanlı uzun CPU deyimleri (>= eşik çekirdek) sayaç artmasa da kısmayı tetikler; tek uzun
// deyim (1 çekirdek) eşiği aşmaz; kilit bekleyen/uyuyan deyim sayılmaz.
func TestAnlikGostergeUzunSorgular(t *testing.T) {
	uzun := func(n int, durum string) []Sorgu {
		var l []Sorgu
		for i := 0; i < n; i++ {
			l = append(l, Sorgu{QueryID: int64(200 + i), Kullanici: "a_db", Sure: 60 * time.Second, Durum: durum, Metin: "SELECT x"})
		}
		return l
	}
	o := yeniOrtam(t)
	o.taban()
	o.olcer.calisan = uzun(1, "Sending data")
	o.calis(8, nil)
	if o.yon.alterSayisi() != 0 {
		t.Fatal("tek uzun sorgu (1 çekirdek) eşiği aşmamalı")
	}
	o.olcer.calisan = uzun(3, "Waiting for table metadata lock")
	o.calis(8, nil)
	if o.yon.alterSayisi() != 0 {
		t.Fatal("kilit bekleyen sorgular CPU sayılmamalı")
	}
	o.olcer.calisan = uzun(3, "User sleep")
	o.calis(8, nil)
	if o.yon.alterSayisi() != 0 {
		t.Fatal("SLEEP CPU sayılmamalı")
	}
	o.olcer.calisan = uzun(3, "Sending data")
	o.calis(5, nil)
	if len(o.m.AktifKullanicilar()) == 0 {
		t.Fatal("3 eşzamanlı uzun CPU deyimi (3 çekirdek > 1) kısmayı tetiklemeli")
	}
}

// Askıdaki hosting kısılmaz; kısıtlıyken askıya alınırsa kısıt bırakılır (kilitli hesaba dokunma).
func TestAskidaKisilmaz(t *testing.T) {
	o := yeniOrtam(t)
	k := o.depo.kimlik["a_db"]
	k.Askida = true
	o.depo.kimlik["a_db"] = k
	o.taban()
	o.calis(6, hizlar("a_db", 3.0))
	if o.yon.alterSayisi() != 0 {
		t.Fatalf("askıdaki hosting kısıldı: %v", o.yon.alterler)
	}
	o = yeniOrtam(t)
	o.taban()
	o.calis(4, hizlar("a_db", 2.0))
	if len(o.m.AktifKullanicilar()) != 1 {
		t.Fatal("kısıt bekleniyordu")
	}
	k = o.depo.kimlik["a_db"]
	k.Askida = true
	o.depo.kimlik["a_db"] = k
	o.m.kimlikOn = map[string]kimlikKaydi{} // önbellek TTL'ini bekleme
	o.calis(1, hizlar("a_db", 2.0))
	if len(o.m.AktifKullanicilar()) != 0 {
		t.Fatal("askıya alınan hostingin kısıtı bırakılmalı")
	}
	if n := o.depo.bitisNedenleri(); len(n) != 1 || n[0] != "askida" {
		t.Fatalf("bitiş nedeni 'askida': %v", n)
	}
}

// Plan yeniden uygulaması kısıtı EZMEZ (kanca) ve kısıt kalkınca GÜNCEL plan değeri yazılır.
func TestPlanYenidenUygulamasiKisitiEzmez(t *testing.T) {
	o := yeniOrtam(t)
	o.taban()
	o.calis(4, hizlar("a_db", 2.0))
	if got := o.m.baglantiKancasi("a_db", 10); got != 2 {
		t.Fatalf("kısıtlıyken plan 10 → kanca 2 döndürmeli: %d", got)
	}
	if got := o.m.baglantiKancasi("a_db", 1); got != 1 {
		t.Fatalf("plan kısıttan düşükse plan korunur: %d", got)
	}
	if got := o.m.baglantiKancasi("a_db", 10); got != 2 {
		t.Fatal()
	}
	if got := o.m.baglantiKancasi("baska_db", 10); got != 10 {
		t.Fatalf("kısıtsız kullanıcıda plan aynen yazılmalı: %d", got)
	}
	// kısıt kalkınca son plan değeri (10) yazılır
	o.calis(30, hizlar("a_db", 0.0))
	if len(o.m.AktifKullanicilar()) != 0 {
		t.Fatal("kısıt kalkmalıydı")
	}
	if got := o.yon.baglanti("a_db"); !esitInts(got, []int{10, 10}) {
		t.Fatalf("güncel plan değeri (10) yazılmalı: %v", got)
	}
}

// Doğrulama: biri (ör. kancasız plan yeniden uygulaması) kısıtı ezerse bir sonraki turda düzeltilir.
func TestEzilenKisitDuzeltilir(t *testing.T) {
	o := yeniOrtam(t)
	o.taban()
	o.calis(4, hizlar("a_db", 2.0))
	o.yon.hesap["a_db"][0].Baglanti = 50 // dışarıdan ezildi
	o.calis(1, hizlar("a_db", 2.0))
	if got := o.yon.baglanti("a_db"); !esitInts(got, []int{2, 2}) {
		t.Fatalf("ezilen kısıt düzeltilmeli: %v", got)
	}
}

// userstat kapalıysa dinamik açılır (yeniden başlatma yok) ve yapılandırma parçası idempotent yazılır;
// açılamazsa "bekliyor" ama parça yine yazılır (sonraki yeniden başlatmada etkin).
func TestUserstatHazirlik(t *testing.T) {
	o := yeniOrtam(t)
	dir := t.TempDir()
	o.m.UserstatCnf = filepath.Join(dir, "zz-userstat.cnf")
	o.yon.userstat = false
	o.taban()
	d := o.m.DurumAl()
	if d.Userstat != "acik" || !o.yon.userstat || !d.UserstatKalici {
		t.Fatalf("userstat dinamik açılmalı + kalıcı yazılmalı: %+v", d)
	}
	b, err := os.ReadFile(o.m.UserstatCnf)
	if err != nil || !strings.Contains(string(b), "[mysqld]") || !strings.Contains(string(b), "userstat = 1") {
		t.Fatalf("yapılandırma parçası yanlış: %q %v", b, err)
	}
	st1, _ := os.Stat(o.m.UserstatCnf)
	o.m.userstatTur = 39
	o.calis(1, nil)
	st2, _ := os.Stat(o.m.UserstatCnf)
	if !st2.ModTime().Equal(st1.ModTime()) {
		t.Fatal("içerik aynıyken dosya yeniden yazılmamalı (idempotent)")
	}

	o2 := yeniOrtam(t)
	o2.m.UserstatCnf = filepath.Join(t.TempDir(), "zz-userstat.cnf")
	o2.yon.userstat = false
	o2.yon.userstatAcHt = errSahte
	o2.taban()
	d = o2.m.DurumAl()
	if d.Userstat != "bekliyor" || !d.UserstatKalici {
		t.Fatalf("açılamıyorsa 'bekliyor' + kalıcı parça: %+v", d)
	}
	// yapılandırma dizini yoksa kalıcı değil (hata değil)
	o3 := yeniOrtam(t)
	o3.m.UserstatCnf = filepath.Join(t.TempDir(), "yok", "zz.cnf")
	o3.yon.userstat = false
	o3.taban()
	if d := o3.m.DurumAl(); d.Userstat != "acik" || d.UserstatKalici {
		t.Fatalf("dizin yokken: %+v", d)
	}
}

func TestOkumaSorgusuMu(t *testing.T) {
	ok := []string{"SELECT 1", "select * from t", "  \n SELECT x", "(SELECT 1) UNION (SELECT 2)", "/* c */ SELECT 1", "/*a*//*b*/ select 1", "SELECT\t1", "SELECT/**/1"}
	kotu := []string{"UPDATE t SET a=1", "INSERT INTO t VALUES (1)", "DELETE FROM t", "ALTER TABLE t ADD c INT", "WITH x AS (SELECT 1) DELETE FROM t", "REPLACE INTO t VALUES(1)",
		"CALL p()", "SELECTED", "SELECT_x", "", "SEL", "-- x SELECT 1", "# SELECT 1", "/* acik yorum SELECT 1", "CREATE TABLE a AS SELECT 1", "LOCK TABLES t WRITE"}
	for _, s := range ok {
		if !okumaSorgusuMu(s) {
			t.Errorf("okuma sayılmalı: %q", s)
		}
	}
	for _, s := range kotu {
		if okumaSorgusuMu(s) {
			t.Errorf("okuma SAYILMAMALI (öldürülmez): %q", s)
		}
	}
}

func TestAyarUygula(t *testing.T) {
	a := ayarUygula(map[string]string{})
	if a != VarsayilanAyar() {
		t.Fatalf("boş harita → varsayılan: %+v", a)
	}
	a = ayarUygula(map[string]string{
		anMod: "GOZLEM", anEsik: "2.5", anBaglanti: "4", anPoll: "30", anPencere: "10", anSoguma: "600", anKill: "20", anYukselme: "90", anUzun: "8", anKes: "5",
	})
	if a.BaglantiKes != 5*time.Minute {
		t.Fatalf("baglanti_kes_dk: %v", a.BaglantiKes)
	}
	if v := VarsayilanAyar(); v.BaglantiKes != 3*time.Minute || v.Mod != ModGozlem {
		t.Fatalf("varsayılanlar: %+v", v)
	}
	for girdi, beklenen := range map[string]time.Duration{"0": time.Minute, "-4": time.Minute, "999": 120 * time.Minute, "abc": 3 * time.Minute, "NaN": 3 * time.Minute} {
		if got := ayarUygula(map[string]string{anKes: girdi}).BaglantiKes; got != beklenen {
			t.Fatalf("baglanti_kes_dk=%q → %v (beklenen %v)", girdi, got, beklenen)
		}
	}
	if md, ok := ModCoz(" Uygula "); !ok || md != ModUygula {
		t.Fatalf("ModCoz: %v %v", md, ok)
	}
	if _, ok := ModCoz("agresif"); ok {
		t.Fatal("ModCoz geçersizi kabul etti")
	}
	if a.Mod != ModGozlem || a.EsikHosting != 2.5 || a.KisitBaglanti != 4 || a.Poll != 30*time.Second || a.Pencere != 60*time.Second ||
		a.Soguma != 10*time.Minute || a.KillEsigi != 20*time.Second || a.Yukselme != 90*time.Second || a.UzunSorgu != 8*time.Second {
		t.Fatalf("ayar uygulanmadı / pencere en az 2 poll olmalı: %+v", a)
	}
	// bozuk/aralık dışı değerler sessizce sınıra çekilir; mod bilinmiyorsa varsayılan
	a = ayarUygula(map[string]string{anMod: "agresif", anEsik: "-3", anBaglanti: "0", anPoll: "1", anSoguma: "x", anKill: "999999"})
	d := VarsayilanAyar()
	if a.Mod != d.Mod || a.EsikHosting != 0.1 || a.KisitBaglanti != 1 || a.Poll != 5*time.Second || a.Soguma != d.Soguma || a.KillEsigi != time.Hour {
		t.Fatalf("sınırlama yanlış: %+v", a)
	}
	a = ayarUygula(map[string]string{anEsik: "NaN"})
	if a.EsikHosting != d.EsikHosting {
		t.Fatalf("NaN → varsayılan: %v", a.EsikHosting)
	}
}

func TestHalkaPencere(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	h := &halka{}
	pen := 60 * time.Second
	for i := 1; i <= 6; i++ {
		h.ekle(ornek{t: t0.Add(time.Duration(i) * 15 * time.Second), dt: 15, cpu: 15, canli: 2}, pen)
	}
	c, g := h.oranlar(t0.Add(90*time.Second), pen)
	if c < 0.999 || c > 1.001 || g < 1.999 || g > 2.001 {
		t.Fatalf("hız=%v canlı=%v (beklenen 1,0 / 2,0)", c, g)
	}
	// pencere dışına çıkınca boşalır
	c, g = h.oranlar(t0.Add(10*time.Minute), pen)
	if c != 0 || g != 0 || !h.bos() {
		t.Fatalf("pencere dışı: %v %v bos=%v", c, g, h.bos())
	}
	// az örnek: tam pencereye bölünür
	h2 := &halka{}
	h2.ekle(ornek{t: t0.Add(15 * time.Second), dt: 15, cpu: 30}, pen)
	if c, _ := h2.oranlar(t0.Add(15*time.Second), pen); c != 0.5 {
		t.Fatalf("tek örnek 30 CPU-sn / 60 sn = 0,5 olmalı: %v", c)
	}
}

func TestCoz(t *testing.T) {
	m := kullaniciCPUCoz("a\t1.5\nb\t0.000002\nbozuk\nc\tx\nd\t-1\ne\t3\r\n")
	if len(m) != 3 || m["a"] != 1.5 || m["e"] != 3 {
		t.Fatalf("kullaniciCPUCoz: %v", m)
	}
	l := calisanlarCoz("12\tu1\t3002.411\t887073\tSending data\tSELECT 1\n13\tu2\tx\t1\ts\ti\nkisa\n14\tu3\t10\t5\t\tSELECT 2\n")
	if len(l) != 2 || l[0].ID != 12 || l[0].QueryID != 887073 || l[0].Sure != 3002411*time.Microsecond || l[1].Durum != "" || l[1].Metin != "SELECT 2" {
		t.Fatalf("calisanlarCoz: %+v", l)
	}
	for _, d := range []string{"Sending data", "Executing", "executing", "Statistics", "Copying to tmp table on disk", "Sorting result"} {
		if !cpuYakiyorMu(d) {
			t.Errorf("CPU durumu sayılmalı: %q", d)
		}
	}
	for _, d := range []string{"User sleep", "Waiting for table metadata lock", "Locked", "Sending to client", "", "Updating"} {
		if cpuYakiyorMu(d) {
			t.Errorf("CPU durumu SAYILMAMALI: %q", d)
		}
	}
}

// CLI: komutlar yalnız doğrulanmış kimlik/host/tamsayı içerir; güvensiz girdi çalıştırılmaz.
func TestCLIKomutGuvenligi(t *testing.T) {
	var komutlar []string
	c := CLI{Calistir: func(ctx context.Context, s string) (string, error) {
		komutlar = append(komutlar, s)
		return "a_db\tlocalhost\t0\nb_db\t%\t3\nkotu'x\tlocalhost\t0\na_db\tev il\t0\n", nil
	}}
	ctx := context.Background()
	if err := c.BaglantiAyarla(ctx, "a_db", "localhost", 2); err != nil {
		t.Fatal(err)
	}
	if got := komutlar[0]; got != "ALTER USER 'a_db'@'localhost' WITH MAX_USER_CONNECTIONS 2" {
		t.Fatalf("ALTER biçimi: %q", got)
	}
	for _, kotu := range [][2]string{{"a'; DROP USER x;--", "localhost"}, {"root", "localhost"}, {"panel", "%"}, {"pma", "localhost"}, {"a_db", "x'y"}, {"a_db", ""}} {
		if err := c.BaglantiAyarla(ctx, kotu[0], kotu[1], 2); err == nil {
			t.Errorf("güvensiz hesap reddedilmeli: %v", kotu)
		}
	}
	if err := c.BaglantiAyarla(ctx, "a_db", "localhost", -1); err == nil {
		t.Error("negatif değer reddedilmeli")
	}
	if len(komutlar) != 1 {
		t.Fatalf("reddedilenler SQL'e ulaştı: %v", komutlar)
	}
	komutlar = nil
	hs, err := c.Hesaplar(ctx, []string{"a_db", "b_db", "root", "x'y"})
	if err != nil {
		t.Fatal(err)
	}
	if len(komutlar) != 1 || strings.Contains(komutlar[0], "root") || strings.Contains(komutlar[0], "x'y") || !strings.Contains(komutlar[0], "'a_db','b_db'") {
		t.Fatalf("Hesaplar sorgusu yalnız geçerli kiracı adlarını içermeli: %v", komutlar)
	}
	if len(hs["a_db"]) != 1 || hs["a_db"][0].Host != "localhost" || len(hs["b_db"]) != 1 || hs["b_db"][0].Baglanti != 3 || len(hs) != 2 {
		t.Fatalf("Hesaplar ayrıştırma (güvensiz ad/host satırları atlanmalı): %+v", hs)
	}
	komutlar = nil
	if err := c.SorguOldur(ctx, 42); err != nil || komutlar[0] != "KILL QUERY ID 42" {
		t.Fatalf("KILL biçimi: %v %v", komutlar, err)
	}
	if err := c.SorguOldur(ctx, 0); err == nil {
		t.Error("geçersiz query id reddedilmeli")
	}
}

// Histerezis: kısıtlıyken yük eşiğin altına ama SogumaOrani*eşiğin ÜSTÜNE (0,8..1,0) inerse kısıt
// KALKMAZ (kısıtın kendi etkisiyle ölçüm düşer; hemen kaldırmak aç-kapa salınımı yaratır);
// yük 0,8'in altına inip soğuma dolunca kalkar.
func TestHistereziKisitiTutar(t *testing.T) {
	o := yeniOrtam(t)
	o.taban()
	o.calis(4, hizlar("a_db", 2.0))
	if len(o.m.AktifKullanicilar()) != 1 {
		t.Fatalf("kısıt bekleniyordu: %v", o.m.AktifKullanicilar())
	}
	o.calis(25, hizlar("a_db", 0.9)) // 375 sn > Soguma (300 sn), ama yük 0,9 > 0,8
	if len(o.m.AktifKullanicilar()) != 1 {
		t.Fatal("yük soğuma bandının (0,8..1,0) üstündeyken kısıt kalktı")
	}
	o.calis(25, hizlar("a_db", 0.5)) // 0,8'in altı: soğuma başlar ve dolar
	if len(o.m.AktifKullanicilar()) != 0 {
		t.Fatalf("yük 0,8'in altına inip soğuma dolunca kısıt kalkmalıydı: %v", o.m.AktifKullanicilar())
	}
}

// ---------------------------------------------------------------------------------------------
// Varsayılan GÖZLEM modu, mod geçişleri, 3. kademe (KILL CONNECTION)

func (o *ortam) kisitSeviyesi(u string) int {
	k, ok := o.m.AktifKisit(u)
	if !ok {
		return 0
	}
	return k.Seviye
}

// VARSAYILAN mod gözlemdir: ölçer, kaydeder, "uygula modunda kısılırdı" bildirir; ALTER/KILL QUERY/
// KILL CONNECTION HİÇ çağrılmaz — saatlerce süren aşımda bile tırmanmaz.
func TestVarsayilanModGozlemHicDokunmaz(t *testing.T) {
	if VarsayilanAyar().Mod != ModGozlem {
		t.Fatalf("varsayılan mod gozlem olmalı: %q", VarsayilanAyar().Mod)
	}
	o := yeniOrtam(t)
	o.ayar = VarsayilanAyar() // testAyar'ın uygula'sı DEĞİL: gerçek varsayılanlar
	for i := 0; i < 6; i++ {
		if err := o.yon.baglan("a_db"); err != nil {
			t.Fatal(err)
		}
	}
	o.olcer.calisan = []Sorgu{{QueryID: 9, Kullanici: "a_db", Sure: time.Minute, Durum: "Sending data", Metin: "SELECT * FROM big"}}
	o.taban()
	o.calis(80, hizlar("a_db", 2.0)) // 20 dakika sürekli aşım (>> Yukselme + BaglantiKes)

	if o.yon.alterSayisi() != 0 || len(o.yon.killler) != 0 || len(o.yon.kesilenler) != 0 || len(o.yon.baglantiSorgu) != 0 {
		t.Fatalf("gözlem modunda MariaDB'ye dokunuldu: alter=%v kill=%v keskonn=%v listeleme=%v",
			o.yon.alterler, o.yon.killler, o.yon.kesilenler, o.yon.baglantiSorgu)
	}
	if got := o.yon.baglanti("a_db"); !esitInts(got, []int{0, 0}) {
		t.Fatalf("mysql.user değişti: %v", got)
	}
	if n := o.yon.baglantiSayisi("a_db"); n != 6 {
		t.Fatalf("açık bağlantılar kesildi: %d", n)
	}
	k, ok := o.m.AktifKisit("a_db")
	if !ok || k.Uygulandi || k.Seviye != 1 || k.Oldurulen != 0 || k.Kesilen != 0 {
		t.Fatalf("gözlem kaydı (uygulandi=false, seviye 1) bekleniyordu: %+v", k)
	}
	// bildirim: kullanıcı adı + "kısılırdı"; denetim ayrı kodla; "kısıt uygulandı" HİÇ yok
	var bildirim, denetim string
	for _, e := range o.m.Olaylar() {
		if e.Tur == "bildirim" && e.Eylem == "Veritabanı CPU eşiği aşıldı (gözlem modu)" {
			bildirim = e.Detay
		}
		if e.Tur == "denetim" && e.Eylem == "db.cpu_asim_gozlem" {
			denetim = e.Detay
		}
	}
	if !strings.Contains(bildirim, "a_db") || !strings.Contains(bildirim, "kısılırdı") || !strings.Contains(bildirim, "hiçbir kısıt uygulanmadı") {
		t.Fatalf("gözlem bildirimi eksik: %q", bildirim)
	}
	if !strings.Contains(denetim, "GÖZLEM") || !strings.Contains(denetim, "kısılırdı") {
		t.Fatalf("gözlem denetimi eksik: %q", denetim)
	}
	if o.olayAdet("denetim", "db.cpu_kisit") != 0 || o.olayAdet("denetim", "db.cpu_kisit_yukselt") != 0 || o.olayAdet("denetim", "db.cpu_baglanti_kes") != 0 ||
		o.olayAdet("bildirim", "Veritabanı CPU kısıtı uygulandı") != 0 {
		t.Fatalf("gözlem modunda 'uygulandı' olayı üretildi: %v", o.m.Olaylar())
	}
}

// Gözlem modunda önceki uygula döneminden kalan kısıt: yeni eylem (KILL / tırmanma / düzeltme ALTER'i)
// ALMAZ ve normal soğumayla kalkar (geri yazma tek izinli ALTER).
func TestGozlemModundaOncekiKisitSogumaylaKalkar(t *testing.T) {
	o := yeniOrtam(t)
	for i := range o.yon.hesap["a_db"] {
		o.yon.hesap["a_db"][i].Baglanti = 7
	}
	o.taban()
	o.calis(4, hizlar("a_db", 2.0))
	if o.kisitSeviyesi("a_db") != 1 || !esitInts(o.yon.baglanti("a_db"), []int{2, 2}) {
		t.Fatal("ön koşul: uygula modunda seviye 1 kısıt")
	}
	alter0 := o.yon.alterSayisi()

	o.ayar.Mod = ModGozlem
	o.olcer.calisan = []Sorgu{{QueryID: 9, Kullanici: "a_db", Sure: time.Minute, Durum: "Sending data", Metin: "SELECT 1"}}
	o.yon.hesap["a_db"][0].Baglanti = 99 // bir "plan yeniden uygulaması": uygula modunda düzeltilirdi
	o.calis(40, hizlar("a_db", 2.0))     // 10 dk aşım sürer
	if o.kisitSeviyesi("a_db") != 1 {
		t.Fatalf("gözlem modunda tırmanma: seviye %d", o.kisitSeviyesi("a_db"))
	}
	if len(o.yon.killler) != 0 || len(o.yon.kesilenler) != 0 || o.yon.alterSayisi() != alter0 {
		t.Fatalf("gözlem modunda yeni eylem: alter=%v kill=%v kes=%v", o.yon.alterler[alter0:], o.yon.killler, o.yon.kesilenler)
	}
	if len(o.m.AktifKullanicilar()) != 1 {
		t.Fatal("aşım sürerken uygulanmış kısıt kalkmamalı")
	}
	// yük biter (uzun sorgu da) → normal soğuma → önceki değer (7) geri yazılır
	o.olcer.calisan = nil
	o.calis(40, hizlar("a_db", 0.0))
	if len(o.m.AktifKullanicilar()) != 0 {
		t.Fatalf("soğuma sonrası kısıt kalkmalıydı: %v", o.m.AktifKullanicilar())
	}
	if got := o.yon.baglanti("a_db"); got[1] != 7 {
		t.Fatalf("eski değer geri yazılmadı: %v", got)
	}
	if n := o.depo.bitisNedenleri(); len(n) != 1 || n[0] != "soguma" {
		t.Fatalf("bitiş nedeni: %v", n)
	}
}

// Gözlem kaydı + uygula'ya geçiş: kayıt "mod" ile kapanır, sürüyorsa HEMEN gerçek kısıt konur.
func TestGozlemKaydiUygulayaGecinceKisilir(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar.Mod = ModGozlem
	o.taban()
	o.calis(6, hizlar("a_db", 2.0))
	if k, ok := o.m.AktifKisit("a_db"); !ok || k.Uygulandi {
		t.Fatalf("gözlem kaydı bekleniyordu: %+v", k)
	}
	if o.yon.alterSayisi() != 0 {
		t.Fatal("gözlemde ALTER")
	}
	o.ayar.Mod = ModUygula
	o.calis(2, hizlar("a_db", 2.0))
	if o.olayAdet("denetim", "db.cpu_kisit_yukselt") != 0 || len(o.yon.killler) != 0 || len(o.yon.kesilenler) != 0 {
		t.Fatal("gözlem kaydı uygulanmadan tırmandı / KILL yapıldı")
	}
	k, ok := o.m.AktifKisit("a_db")
	if !ok || !k.Uygulandi || k.Seviye != 1 || !esitInts(o.yon.baglanti("a_db"), []int{2, 2}) {
		t.Fatalf("uygula'ya geçince gerçek kısıt bekleniyordu: %+v alter=%v", k, o.yon.alterler)
	}
	var mod bool
	for _, n := range o.depo.bitisNedenleri() {
		mod = mod || n == "mod"
	}
	if !mod {
		t.Fatalf("gözlem kaydı 'mod' ile kapanmalı: %v", o.depo.bitisNedenleri())
	}
}

// 3. kademe: 2. seviyeden sonra aşım BaglantiKes (3 dk) daha sürerse YALNIZ o kullanıcının
// bağlantıları KILL CONNECTION ile kesilir. Seviye 1 sınırı (2) düşük olduğu için yeniden
// bağlanan istemci düşük sınıra takılır; kesim tek seferdir.
func TestUcuncuKademeBaglantiKesme(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar.BaglantiKes = 3 * time.Minute
	// kalıcı bağlantılı istemci: kısıttan ÖNCE 6 bağlantı (sınırsız hesap); komşular
	for i := 0; i < 6; i++ {
		_ = o.yon.baglan("a_db")
	}
	for i := 0; i < 3; i++ {
		_ = o.yon.baglan("b_db")
		_ = o.yon.baglan("panel")
	}
	_ = o.yon.baglan("root")
	// hatalı kaynak benzetimi: PROCESSLIST sonucuna başka kullanıcıların satırları karışmış olsun
	o.yon.yabanci = []Baglanti{{ID: 5555, Kullanici: "b_db"}, {ID: 5556, Kullanici: "panel"}, {ID: 5557, Kullanici: "root"}}
	hedefIDs := append([]int64(nil), o.yon.conns["a_db"]...)

	o.taban()
	o.calis(4, hizlar("a_db", 2.0))
	if o.kisitSeviyesi("a_db") != 1 || !esitInts(o.yon.baglanti("a_db"), []int{2, 2}) {
		t.Fatalf("seviye 1 kısıt bekleniyordu: %d %v", o.kisitSeviyesi("a_db"), o.yon.baglanti("a_db"))
	}
	// önbellek kanıtı: limit 2 yazıldı ama 6 açık bağlantı varken yeni bağlantı HÂLÂ kabul edilir
	if err := o.yon.baglan("a_db"); err != nil {
		t.Fatalf("eski sınır önbelleği: açık bağlantı varken yeni sınır uygulanmamalıydı (benzetim): %v", err)
	}
	hedefIDs = append(hedefIDs, o.yon.conns["a_db"][len(o.yon.conns["a_db"])-1])
	o.calis(3, hizlar("a_db", 2.0)) // Yukselme (45 sn)
	if o.kisitSeviyesi("a_db") != 2 {
		t.Fatalf("seviye 2 bekleniyordu: %d", o.kisitSeviyesi("a_db"))
	}
	// 2. seviyede 3 dk dolmadan (150 sn) bağlantı KESİLMEZ
	o.calis(10, hizlar("a_db", 2.0))
	if o.kisitSeviyesi("a_db") != 2 || len(o.yon.kesilenler) != 0 || len(o.yon.baglantiSorgu) != 0 {
		t.Fatalf("3 dk dolmadan 3. kademe: seviye=%d kes=%v listeleme=%v", o.kisitSeviyesi("a_db"), o.yon.kesilenler, o.yon.baglantiSorgu)
	}
	o.calis(2, hizlar("a_db", 2.0)) // 165 sn: hâlâ yok; 180 sn: kesim
	k, ok := o.m.AktifKisit("a_db")
	if !ok || k.Seviye != 3 || k.Kesilen != 7 {
		t.Fatalf("seviye 3 + 7 bağlantı kesimi bekleniyordu: %+v", k)
	}
	// YALNIZ hedef kullanıcının 7 bağlantısı; komşular, sistem hesapları ve sızan yabancı satırlar değil
	got := append([]int64(nil), o.yon.kesilenler...)
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	sort.Slice(hedefIDs, func(i, j int) bool { return hedefIDs[i] < hedefIDs[j] })
	if len(got) != 7 || fmt.Sprint(got) != fmt.Sprint(hedefIDs) {
		t.Fatalf("kesilen bağlantılar yalnız a_db'nin olmalı: kesilen=%v beklenen=%v", got, hedefIDs)
	}
	if o.yon.baglantiSayisi("a_db") != 0 || o.yon.baglantiSayisi("b_db") != 3 || o.yon.baglantiSayisi("panel") != 3 || o.yon.baglantiSayisi("root") != 1 {
		t.Fatalf("bağlantı sayıları: a=%d b=%d panel=%d root=%d", o.yon.baglantiSayisi("a_db"), o.yon.baglantiSayisi("b_db"), o.yon.baglantiSayisi("panel"), o.yon.baglantiSayisi("root"))
	}
	if o.olayAdet("denetim", "db.cpu_baglanti_kes") != 1 || o.olayAdet("bildirim", "Veritabanı bağlantıları kesildi") != 1 {
		t.Fatalf("denetim+bildirim bekleniyordu: %v", o.m.Olaylar())
	}
	for _, e := range o.m.Olaylar() {
		if e.Eylem == "db.cpu_baglanti_kes" && !strings.Contains(e.Detay, "7 bağlantı") {
			t.Fatalf("kesilen sayısı denetimde yok: %q", e.Detay)
		}
	}
	// yeniden bağlanan istemci: sayaç 0 olduğu için artık 3. seviye sınırı (1) geçerli → 1 bağlantı, kalanı reddedilir
	ok2, red := 0, 0
	for i := 0; i < 6; i++ {
		if err := o.yon.baglan("a_db"); err == nil {
			ok2++
		} else if errors.Is(err, errLimit) {
			red++
		}
	}
	if ok2 != 1 || red != 5 {
		t.Fatalf("yeniden bağlanma 3. seviye sınırına takılmalı (1 kabul, 5 ret): kabul=%d ret=%d", ok2, red)
	}
	// kesim TEK seferdir: aşım sürse de bağlantılar yeniden kesilmez
	n0 := len(o.yon.kesilenler)
	o.calis(20, hizlar("a_db", 2.0))
	if len(o.yon.kesilenler) != n0 || o.yon.baglantiSayisi("a_db") != 1 {
		t.Fatalf("ikinci kesim: %d → %d", n0, len(o.yon.kesilenler))
	}
	// soğuma + geri dönüş mantığı değişmedi
	o.calis(40, hizlar("a_db", 0.0))
	if len(o.m.AktifKullanicilar()) != 0 || !esitInts(o.yon.baglanti("a_db"), []int{0, 0}) {
		t.Fatalf("3. seviyeden sonra da soğuma geri dönüşü: %v %v", o.m.AktifKullanicilar(), o.yon.baglanti("a_db"))
	}
}

// Sistem hesabına bağlantı kesimi ASLA: hostingin kullanıcı listesine sızsa bile, doğrudan çağrılsa bile.
func TestSistemHesabinaBaglantiKesilmez(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar.BaglantiKes = time.Minute
	o.depo.domKul[1] = []string{"a_db", "root", "panel", "pma", "mysql", "a db;DROP"}
	for _, u := range []string{"root", "panel", "pma", "mysql"} {
		o.yon.hesap[u] = []Hesap{{Host: "localhost"}}
		_ = o.yon.baglan(u)
		_ = o.yon.baglan(u)
	}
	_ = o.yon.baglan("a_db")
	o.taban()
	o.calis(40, hizlar("a_db", 2.0))
	if o.kisitSeviyesi("a_db") != 3 {
		t.Fatalf("a_db seviye 3 olmalı: %d", o.kisitSeviyesi("a_db"))
	}
	for _, u := range []string{"root", "panel", "pma", "mysql"} {
		if o.yon.baglantiSayisi(u) != 2 {
			t.Fatalf("sistem hesabı %s bağlantısı kesildi: %d", u, o.yon.baglantiSayisi(u))
		}
	}
	for _, q := range o.yon.baglantiSorgu {
		if q != "a_db" {
			t.Fatalf("bağlantı listesi yalnız a_db için istenmeli: %v", o.yon.baglantiSorgu)
		}
	}
	// doğrudan çağrı da reddedilir ve MariaDB'ye hiç gitmez
	n0 := len(o.yon.baglantiSorgu)
	for _, u := range []string{"root", "panel", "pma", "mysql", "", "x;y"} {
		if n, err := o.m.baglantiKes(context.Background(), u); err == nil || n != 0 {
			t.Fatalf("baglantiKes(%q) reddedilmeliydi: %d %v", u, n, err)
		}
	}
	if len(o.yon.baglantiSorgu) != n0 {
		t.Fatal("reddedilen kullanıcı için MariaDB sorgulandı")
	}
}

// Bağlantı listesi okunamazsa seviye 2'de kalır ve sonraki turda yeniden denenir; hiçbiri kesilemezse
// de seviye 3 sayılmaz.
func TestBaglantiKesmeHatasiYenidenDener(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar.BaglantiKes = time.Minute
	_ = o.yon.baglan("a_db")
	_ = o.yon.baglan("a_db")
	o.yon.baglantiHata = errSahte
	o.taban()
	o.calis(14, hizlar("a_db", 2.0))
	if o.kisitSeviyesi("a_db") != 2 || o.m.DurumAl().SonHata == "" {
		t.Fatalf("liste hatası: seviye 2 + hata bekleniyordu: %d %q", o.kisitSeviyesi("a_db"), o.m.DurumAl().SonHata)
	}
	o.yon.baglantiHata = nil
	o.yon.kesHata = errSahte // hiçbiri kesilemiyor
	o.calis(2, hizlar("a_db", 2.0))
	if o.kisitSeviyesi("a_db") != 2 {
		t.Fatalf("hiçbir bağlantı kesilemediyse seviye 3 olmamalı: %d", o.kisitSeviyesi("a_db"))
	}
	o.yon.kesHata = nil
	o.calis(2, hizlar("a_db", 2.0))
	if k, _ := o.m.AktifKisit("a_db"); k.Seviye != 3 || k.Kesilen != 2 || o.yon.baglantiSayisi("a_db") != 0 {
		t.Fatalf("hata giderilince kesilmeli: %+v", k)
	}
}

func TestCLIBaglantiKomutlari(t *testing.T) {
	var sorgular []string
	c := CLI{Calistir: func(ctx context.Context, q string) (string, error) {
		sorgular = append(sorgular, q)
		return "5\ta_db\n6\ta_db\nbozuk\n0\ta_db\nx\ta_db\n", nil
	}}
	l, err := c.Baglantilar(context.Background(), "a_db")
	if err != nil || len(l) != 2 || l[0].ID != 5 || l[1].ID != 6 || l[0].Kullanici != "a_db" {
		t.Fatalf("ayrıştırma: %v %v", l, err)
	}
	if q := sorgular[0]; !strings.Contains(q, "BINARY USER='a_db'") || !strings.Contains(q, "ID<>CONNECTION_ID()") {
		t.Fatalf("liste sorgusu kendi bağlantıyı hariç tutmalı ve kullanıcıyı harf duyarlı eşlemeli: %s", q)
	}
	if err := c.BaglantiKes(context.Background(), 77); err != nil || sorgular[1] != "KILL CONNECTION 77" {
		t.Fatalf("kesme komutu: %v %v", sorgular, err)
	}
	n := len(sorgular)
	for _, u := range []string{"root", "panel", "a'b", "", "a_db' OR '1'='1"} {
		if _, err := c.Baglantilar(context.Background(), u); err == nil {
			t.Fatalf("Baglantilar(%q) reddedilmeliydi", u)
		}
	}
	for _, id := range []int64{0, -3} {
		if err := c.BaglantiKes(context.Background(), id); err == nil {
			t.Fatalf("BaglantiKes(%d) reddedilmeliydi", id)
		}
	}
	if len(sorgular) != n {
		t.Fatalf("reddedilen girdiler MariaDB'ye gitti: %v", sorgular[n:])
	}
}
