package backups

// silinen_hedef_dogrulama_test.go — DÜŞMANCA DOĞRULAMA (yetim yedek tasarımı, commit e449f18).
//
//	E  S1: uzak hedef anlık görüntüsü (yazma, parola sızıntısı, temizlik, kimlik yeniden kullanımı)
//	M  yetimYedegiSil ile bağlama arasındaki yarış (canlı yedeği silme)
//	N  yetimleriBagla değişmezleri
//	L  silme izi / süre hesabı sınırları (TIMESTAMP 2038)
//
// Gerçek BackupRoot'a YAZILMAZ, AĞA ÇIKILMAZ (uzak silme sahte). Geçici MariaDB gerekir.
// Adında "Bulgu" geçen testler ÜRETİM KODUNDAKİ GERÇEK BİR HATAYI kanıtlar: bugün KIRMIZIDIR.

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"girginospanel/internal/gizli"

	"github.com/go-chi/chi/v5"
)

// ================================================================ E — S1 anlık görüntü

// Anlık görüntü sütunları kaynak (backup_destinations) sütunlarından DAR olamaz: dar olursa silme izi sessizce
// "anlık görüntü yazılamadı" ile geçer ve uzak kopyalar bir daha silinemez.
func TestDG_E1_AnlikGoruntu_SutunTanimlariKaynaktanDarDegil(t *testing.T) {
	db := kDB(t)
	q := `SELECT CHARACTER_MAXIMUM_LENGTH FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND COLUMN_NAME=?`
	for _, c := range []string{"tip", "host", "kullanici", "parola", "uzak_dizin"} {
		var kaynak, anlik int
		if err := db.QueryRow(q, "backup_destinations", c).Scan(&kaynak); err != nil {
			t.Fatalf("%s kaynak: %v", c, err)
		}
		if err := db.QueryRow(q, "backup_yetim_hedefler", c).Scan(&anlik); err != nil {
			t.Fatalf("%s anlık görüntü: %v", c, err)
		}
		if anlik < kaynak {
			t.Errorf("🔴 backup_yetim_hedefler.%s (%d) < backup_destinations.%s (%d): en uzun değer anlık görüntüye SIĞMAZ", c, anlik, c, kaynak)
		}
	}
}

// En uzun değerler ve çok baytlı karakterler anlık görüntüye eksiksiz kopyalanır.
func TestDG_E1b_AnlikGoruntu_EnUzunDegerlerVeCokBaytliKarakterler(t *testing.T) {
	db := kDB(t)
	silinenOrtam(t)
	host := strings.Repeat("h", 253)
	kul := strings.Repeat("ş", 128)
	// Sütundaki gerçek değer her zaman SaklaBagli çıktısıdır (düz 255 karakter ancak eski/anahtarsız kayıtta bulunur ve D4
	// gereği anlık görüntüye düz yazılmaz): sütuna SIĞAN EN UZUN şifreli parola (düz metin 159 karakter ≈ 250 karakter).
	parola := gizli.SaklaBagli(strings.Repeat("p", 159), "yedek")
	if n := len([]rune(parola)); n > 255 || (gizli.SifreliMi(parola) && n < 240) {
		t.Fatalf("kurulum: şifreli parola uzunluğu %d (240-255 aralığı beklenirdi)", n)
	}
	dizin := strings.Repeat("😀", 255)
	dgDomain(t, db, 1200, "uzun.com", "c_uzun", 0)
	g113_exec(t, db, `INSERT INTO backup_destinations (domain_id, tip, host, port, kullanici, parola, uzak_dizin) VALUES (1200,'sftp',?,65535,?,?,?)`, host, kul, parola, dizin)
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya) VALUES (1200, ?)`, dgAd("c_uzun", 0))
	kSil(t, db, 1200, "uzun.com", "c_uzun")
	var h, k, p, d string
	var port int
	if err := db.QueryRow(`SELECT host, port, kullanici, parola, uzak_dizin FROM backup_yetim_hedefler WHERE eski_domain_id=1200`).Scan(&h, &port, &k, &p, &d); err != nil {
		t.Fatalf("🔴 en uzun değerli hedef için anlık görüntü yazılamadı: %v", err)
	}
	if h != host || port != 65535 || k != kul || p != parola || d != dizin {
		t.Error("anlık görüntü alanları kaynakla birebir aynı olmalı")
	}
}

// Çok büyük domain kimlikleri (> 2^32, int64 sınırı) iz, anlık görüntü, S2 ve bağlamada taşmaz.
func TestDG_E1c_BuyukDomainKimlikleri_Tasmaz(t *testing.T) {
	for _, id := range []int64{999999164, 4294967297, 9223372036854775807} {
		t.Run(fmt.Sprint(id), func(t *testing.T) {
			o := dgYeni(t)
			yetimUzakSahte(t)
			ctx := context.Background()
			o.hesap(id, "buyuk.com", "c_buyuk", 5, "ftp.b.invalid", 2)
			kSil(t, o.db, id, "buyuk.com", "c_buyuk")
			if len(dgYetimIDleri(t, o.db, id)) != 2 || g113_say(t, o.db, `SELECT COUNT(*) FROM backup_yetim_hedefler WHERE eski_domain_id=?`, id) != 1 {
				t.Fatal("iz/anlık görüntü büyük kimlikle yazılamadı")
			}
			dgDomain(t, o.db, 5, "buyuk.com", "c_buyuk", 5)
			if n := devral(ctx, o.db, 5); n != 2 {
				t.Fatalf("bağlanan %d", n)
			}
			if g113_say(t, o.db, `SELECT COUNT(*) FROM backup_destinations WHERE domain_id=5`) != 0 || g113_say(t, o.db, `SELECT COUNT(*) FROM backup_yetim_hedefler`) != 0 {
				t.Error("hedef geri yüklenmemeli, anlık görüntü silinmeli")
			}
			// S2: iz yazılamamış satır, büyük kimlikle de yetim yapılır.
			g113_exec(t, o.db, `INSERT INTO backups (domain_id, dosya) VALUES (?, ?)`, id, dgAd("c_buyuk", 9))
			if m, _ := SilinenSahipsizIsaretle(ctx, o.db); m != 1 || g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE eski_domain_id=? AND domain_id=0`, id) != 1 {
				t.Errorf("S2 büyük kimlikli satırı işaretleyemedi: %d", m)
			}
		})
	}
}

// aktif=0 (kapatılmış) hedefin kopyası da alınır (kapalı hedefte de dosyalar durur) ve uzak silmede kullanılır;
// host'u boş/boşluk olan hedef için anlık görüntü YAZILMAZ.
func TestDG_E2_AnlikGoruntu_KapaliHedefYazilirVeKullanilir_BosHostYazilmaz(t *testing.T) {
	o := dgYeni(t)
	ctx := context.Background()
	sahte := yetimUzakSahte(t)
	d1, _ := o.hesap(1300, "kapali.com", "c_kapali", 0, "ftp.kapali.invalid", 1)
	g113_exec(t, o.db, `UPDATE backup_destinations SET aktif=0 WHERE domain_id=1300`)
	o.hesap(1301, "bos.com", "c_bos1", 0, "", 1)
	g113_exec(t, o.db, `INSERT INTO backup_destinations (domain_id, tip, host, port, kullanici, parola, uzak_dizin) VALUES (1301,'ftp','   ',21,'k','p','/')`)
	kSil(t, o.db, 1300, "kapali.com", "c_kapali")
	kSil(t, o.db, 1301, "bos.com", "c_bos1")
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backup_yetim_hedefler WHERE eski_domain_id=1300`) != 1 {
		t.Error("kapalı (aktif=0) hedefin anlık görüntüsü alınmalı")
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backup_yetim_hedefler WHERE eski_domain_id=1301`) != 0 {
		t.Error("host'u boşluk olan hedef için anlık görüntü yazılmamalı")
	}
	g113_exec(t, o.db, `UPDATE backups SET son_kullanma=DATE_SUB(NOW(), INTERVAL 1 HOUR) WHERE domain_id=0`)
	SilinenSupur(ctx, o.db)
	var bulundu bool
	for _, c := range sahte.cagrilar {
		if c.dosya == d1[0] {
			bulundu = true
			if c.d == nil || c.d.Host != "ftp.kapali.invalid" || !c.d.Aktif {
				t.Errorf("kapalı hedef uzak silmede kullanılmalı (Aktif zorlanır): %+v", c.d)
			}
		}
	}
	if !bulundu {
		t.Error("uzak silme çağrılmadı")
	}
}

const dgZorParola = `Pa$$"w\o/rd&1 %2F;'x<y>`

func dgYasakBicimler(parolalar ...string) []string {
	var out []string
	for _, p := range parolalar {
		if p == "" {
			continue
		}
		jb, _ := json.Marshal(p)
		out = append(out, p, url.QueryEscape(p), url.PathEscape(p), lftpEscape(p), string(jb[1:len(jb)-1]))
	}
	return out
}

func dgSizintiYok(t *testing.T, yuzey, metin string, yasak []string) {
	t.Helper()
	for _, y := range yasak {
		if len(y) >= 6 && strings.Contains(metin, y) {
			t.Errorf("🔴 %s parolayı SIZDIRIYOR (bulunan biçim %q)", yuzey, y)
		}
	}
}

func dgZorHesap(t *testing.T, db *sql.DB, id int64, alan, sk string, rid int64) (ham string) {
	t.Helper()
	dgDomain(t, db, id, alan, sk, rid)
	ham = gizli.SaklaBagli(dgZorParola, "yedek")
	g113_exec(t, db, `INSERT INTO backup_destinations (domain_id, tip, host, port, kullanici, parola, uzak_dizin) VALUES (?,'sftp','ftp.zor.invalid',22,'zorkul',?,'/y')`, id, ham)
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya, boyut_b) VALUES (?,?,5)`, id, dgAd(sk, 0))
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya, boyut_b) VALUES (?,?,5)`, id, dgAd(sk, 1))
	return ham
}

// Parola sızıntı taraması: ZOR karakterli (tırnak, ters bölü, $, boşluk, %, ;, <>) parola; uzak silme hatası parolayı
// ham + URL + lftp + JSON kaçışlı biçimlerde yansıtıyor. Hiçbir API yanıtında, günlükte ve denetim kaydında geçmemeli.
func TestDG_E3_ParolaSizintiTaramasi_ZorKarakterliParola_TumYuzeyler(t *testing.T) {
	o := dgYeni(t)
	ctx := context.Background()
	logs := logYakala(t)
	sahte := yetimUzakSahte(t)
	ham := dgZorHesap(t, o.db, 1000, "zor.com", "c_zor", 5)
	kSil(t, o.db, 1000, "zor.com", "c_zor")
	yasak := dgYasakBicimler(dgZorParola, ham)

	sahte.hata = func(d *Destination, dosya string) error {
		jb, _ := json.Marshal(d.Parola)
		return fmt.Errorf("lftp: open -u \"%s\",\"%s\" sftp://%s: Login failed; raw=%s url=%s json=%s dosya=%s",
			d.Kullanici, lftpEscape(d.Parola), d.Host, d.Parola, url.QueryEscape(d.Parola), jb, dosya)
	}
	kod, g := advIstek(o.r, "DELETE", "/s/c_zor", "")
	if kod != 409 {
		t.Fatalf("uzak hata → 409: %d %s", kod, g)
	}
	if sahte.say() == 0 {
		t.Fatal("kontrol: uzak silme hiç çağrılmadı")
	}
	yasak = append(yasak, dgYasakBicimler(sahte.cagrilar[0].d.Parola)...) // hook'un gördüğü çözülmüş değer
	dgSizintiYok(t, "DELETE yanıtı", g, yasak)

	g113_exec(t, o.db, `UPDATE backups SET son_kullanma=DATE_SUB(NOW(), INTERVAL 1 HOUR) WHERE domain_id=0`)
	SilinenSupur(ctx, o.db)
	if !strings.Contains(logs.String(), "temizlenemedi") {
		t.Error("kontrol: süpürme hatası loglanmalıydı")
	}
	_, g = advIstek(o.r, "GET", "/s", "")
	dgSizintiYok(t, "yetim listesi yanıtı", g, yasak)

	// yeniden açılış + devralma + liste/hedef sayfaları
	dgDomain(t, o.db, 1001, "zor.com", "c_zor", 5)
	devral(ctx, o.db, 1001)
	_, g = advIstek(o.r, "GET", "/d/1001/backups", "")
	dgSizintiYok(t, "domain yedek listesi yanıtı", g, yasak)
	h := &Handlers{DB: o.db}
	rt := chi.NewRouter()
	rt.Get("/d/{id}/dest", h.GetDestination)
	_, g = advIstek(rt, "GET", "/d/1001/dest", "")
	dgSizintiYok(t, "hedef GET yanıtı", g, yasak)
	if strings.Contains(g, `"parola"`) {
		t.Errorf("hedef GET yanıtı parola alanı taşımamalı: %s", g)
	}
	dgSizintiYok(t, "günlük", logs.String(), yasak)
	dgSizintiYok(t, "denetim kaydı", dgDokum(t, o.db, "audit_log"), yasak)

	t.Run("anlık görüntü yazılamazsa günlük", func(t *testing.T) {
		db := kDB(t)
		l := logYakala(t)
		ham2 := dgZorHesap(t, db, 1100, "s.com", "c_s1", 0)
		g113_exec(t, db, `ALTER TABLE backup_yetim_hedefler MODIFY host VARCHAR(5) NOT NULL`)
		kSil(t, db, 1100, "s.com", "c_s1")
		if !strings.Contains(l.String(), "anlık görüntüsü yazılamadı") {
			t.Fatalf("kontrol: yazma hatası loglanmalıydı: %s", l.String())
		}
		dgSizintiYok(t, "anlık görüntü yazma hatası günlüğü", l.String(), dgYasakBicimler(dgZorParola, ham2))
	})
	t.Run("bağlama günlüğü ve anlık görüntü temizliği parola sızdırmaz", func(t *testing.T) {
		db := kDB(t)
		l := logYakala(t)
		ham2 := dgZorHesap(t, db, 1110, "r.com", "c_r1", 0)
		kSil(t, db, 1110, "r.com", "c_r1")
		dgDomain(t, db, 1111, "r.com", "c_r1", 0)
		if n := devral(ctx, db, 1111); n != 2 {
			t.Fatalf("bağlama başarılı olmalı: %d", n)
		}
		dgSizintiYok(t, "bağlama günlüğü", l.String(), dgYasakBicimler(dgZorParola, ham2))
		if g113_say(t, db, `SELECT COUNT(*) FROM backup_yetim_hedefler`) != 0 || g113_say(t, db, `SELECT COUNT(*) FROM backup_destinations WHERE domain_id=1111`) != 0 {
			t.Error("bağlamadan sonra ne anlık görüntü ne geri yüklenmiş hedef kalmalı")
		}
	})
}

// Aynı sk altında iki ayrı silinme (iki ayrı eski kimlik, iki ayrı uzak sunucu): biri başarısız olursa YALNIZ
// başarıyla biten kimliğin yetimi ve anlık görüntüsü gider; başarısızınki (parola dahil) tekrar denenebilsin diye kalır.
func TestDG_E4_SilUcu_KismiUzakHata_YalnizBitenKimliginAnlikGoruntusuSilinir(t *testing.T) {
	o := dgYeni(t)
	sahte := yetimUzakSahte(t)
	logs := logYakala(t)
	kHesap(t, o.db, 7001, "a.com", "c_s", 0, "ftp.a.invalid", dgAd("c_s", 0))
	kSil(t, o.db, 7001, "a.com", "c_s")
	kHesap(t, o.db, 7002, "b.com", "c_s", 0, "ftp.b.invalid", dgAd("c_s", 1))
	kSil(t, o.db, 7002, "b.com", "c_s")
	sahte.hata = func(d *Destination, _ string) error {
		if d != nil && d.Host == "ftp.b.invalid" {
			return fmt.Errorf("lftp: Login failed for %s with %s", d.Kullanici, d.Parola)
		}
		return nil
	}
	kod, g := advIstek(o.r, "DELETE", "/s/c_s", "")
	if kod != 409 || !strings.Contains(g, `"silinen":1`) {
		t.Fatalf("kısmi başarı: 409 silinen=1 beklenirdi: %d %s", kod, g)
	}
	if strings.Contains(g, kParola) || strings.Contains(logs.String(), kParola) {
		t.Error("parola sızdı")
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE eski_domain_id=7001`) != 0 || g113_say(t, o.db, `SELECT COUNT(*) FROM backup_yetim_hedefler WHERE eski_domain_id=7001`) != 0 {
		t.Error("başarıyla silinen kimliğin yetimi + anlık görüntüsü gitmeli")
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE eski_domain_id=7002 AND domain_id=0`) != 1 || g113_say(t, o.db, `SELECT COUNT(*) FROM backup_yetim_hedefler WHERE eski_domain_id=7002`) != 1 {
		t.Error("başarısız kimliğin yetimi + anlık görüntüsü tekrar denemek için KALMALI")
	}
	sahte.hata = nil
	if kod, g := advIstek(o.r, "DELETE", "/s/c_s", ""); kod != 200 {
		t.Fatalf("hata giderilince tamam olmalı: %d %s", kod, g)
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backups`) != 0 || g113_say(t, o.db, `SELECT COUNT(*) FROM backup_yetim_hedefler`) != 0 {
		t.Error("her şey gitmeli (parola tabloda kalmamalı)")
	}
}

// Bağlanan 'kurtarma' yedeği kullanıcı tarafından elle silinirken eski müşterinin hedefi KULLANILMAZ: geri yüklenmemiştir (karar:
// anlık görüntü domaine asla geri yüklenmez), silme yalnız genel hedefe bakar; yerel dosya + satır silinir.
func TestDG_E5_BaglananKurtarmaYedegi_ElleSilinirken_EskiMusterininHedefiYok(t *testing.T) {
	o := dgYeni(t)
	yetimUzakSahte(t)
	dosyalar, _ := o.hesap(1400, "e5.com", "c_e5", 5, "ftp.e5.invalid", 1)
	kSil(t, o.db, 1400, "e5.com", "c_e5")
	dgDomain(t, o.db, 1401, "e5.com", "c_e5", 5)
	dgBagla(t, o.r, "c_e5", 1401, false)
	liste := dgListe(t, o.r, 1401)
	if len(liste) != 1 || liste[0].Tip != "kurtarma" {
		t.Fatalf("kurulum: 1 kurtarma satırı beklenirdi: %+v", liste)
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backup_destinations WHERE domain_id=1401`) != 0 {
		t.Fatal("eski müşterinin hedefi domaine geri yüklenmemeli")
	}
	kod, g := advIstek(o.r, "DELETE", fmt.Sprintf("/d/1401/backups/%d", liste[0].ID), "")
	if kod != 200 {
		t.Errorf("hedef yok → uzak silme adımı atlanır, silme tamamlanmalı: %d %s", kod, g)
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE id=?`, liste[0].ID) != 0 || varMi(filepath.Join(o.kok, "c_e5", dosyalar[0])) {
		t.Error("satır + dosya silinmeli")
	}
}

// BULGU E9 — bağlama domaine KULLANICININ TANIMLAMADIĞI bir uzak hedefi (aktif=0) geri yüklüyordu. deleteFromRemote "Aktif yükleme kapısıdır,
// silme kapısı değil" diye pasif hedefe de gider; hedef ulaşılamıyorsa (eski müşterinin FTP'si kapanmışsa) domainin KENDİ yedeklerinin
// gece budaması (pruneOld) uzak silme hatasıyla düşerdi: "kayıt korundu", yerel yedekler saklama sayısını aşıp birikir. KARAR: hedef
// hiç geri yüklenmez → budama takılmaz.
func TestDGBulgu_E9_BaglamadaHedefGeriYuklenmez_DomaininKendiYedeklerininBudamasiTakilmaz(t *testing.T) {
	o := dgYeni(t)
	yetimUzakSahte(t)
	logYakala(t)
	o.hesap(1500, "p5.com", "c_p5", 5, "ftp.p5.invalid", 1)
	kSil(t, o.db, 1500, "p5.com", "c_p5")
	dgDomain(t, o.db, 1501, "p5.com", "c_p5", 5)
	dgBagla(t, o.r, "c_p5", 1501, false) // bağlama
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backup_destinations WHERE domain_id=1501`) != 0 {
		t.Fatal("hedef geri yüklenmemeli")
	}
	// yeni hesabın KENDİ otomatik yedekleri: saklama sayısı 1
	for i := 10; i < 13; i++ {
		d := dgAd("c_p5", i)
		silinenDosya(t, o.kok, "c_p5", d)
		g113_exec(t, o.db, `INSERT INTO backups (domain_id, tip, dosya) VALUES (1501, 'oto', ?)`, d)
	}
	if err := pruneOld(o.db, 1501, "c_p5", 1); err != nil {
		t.Fatal(err)
	}
	if n := g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE domain_id=1501 AND tip='oto'`); n != 1 {
		t.Errorf("🔴 saklama sayısı 1 iken %d oto yedek kaldı: budama takıldı", n)
	}
}

// BULGU E7a — domain kimliği yeniden kullanılıp İKİNCİ sahip de silinirse, ikinci sahibin yetimleri anlık görüntü
// (eski_domain_id UNIQUE, "zaten varsa dokunma") yüzünden İLK sahibin sunucusuna ve parolasına karşı silinmeye
// çalışılır: ikinci sahibin uzak kopyası silinmez, ilk sahibin sunucusuna yanlış kimlik bilgisiyle gidilir.
func TestDGBulgu_E7a_IdYenidenKullanimi_IkinciSahibinYetimiIlkSahibinHedefiyleSilinmemeli(t *testing.T) {
	o := dgYeni(t)
	sahte := yetimUzakSahte(t)
	fx, _ := o.hesap(7800, "x.test", "c_x", 0, "ftp.ilk.invalid", 1)
	kSil(t, o.db, 7800, "x.test", "c_x")
	fy, _ := o.hesap(7800, "y.test", "c_y", 0, "ftp.ikinci.invalid", 1) // kimlik yeniden kullanıldı
	kSil(t, o.db, 7800, "y.test", "c_y")
	g113_exec(t, o.db, `UPDATE backups SET son_kullanma=DATE_SUB(NOW(), INTERVAL 1 HOUR) WHERE domain_id=0`)
	SilinenSupur(context.Background(), o.db)
	host := map[string]string{}
	for _, c := range sahte.cagrilar {
		if c.d != nil {
			host[c.dosya] = c.d.Host
		}
	}
	if host[fx[0]] != "ftp.ilk.invalid" {
		t.Errorf("kontrol: ilk sahibin yedeği kendi hedefiyle silinmeli: %q", host[fx[0]])
	}
	if host[fy[0]] != "ftp.ikinci.invalid" {
		t.Errorf("🔴 ikinci sahibin yedeği (%s) KENDİ hedefiyle silinmedi; kullanılan sunucu: %q (ilk sahibin kimlik bilgisi)", fy[0], host[fy[0]])
	}
}

// BULGU E7b — kimlik yeniden kullanımı + aynı bayi: B'nin yetimleri bağlanırken, A'nın (başka müşteri) hâlâ duran
// anlık görüntüsü B'nin yeni domainine GERİ YÜKLENİR (EXISTS koşulu A'nın yetimleriyle sağlanır) → A'nın FTP kimlik bilgisi B'ye geçer.
func TestDGBulgu_E7b_IdYenidenKullanimi_IlkMusterininFTPKimligiYenidenAcilanIkinciMusteriyeVerilmemeli(t *testing.T) {
	o := dgYeni(t)
	yetimUzakSahte(t)
	o.hesap(100, "a.com", "c_a", 5, "ftp.a.invalid", 1) // müşteri A: FTP hedefi var
	kSil(t, o.db, 100, "a.com", "c_a")
	o.hesap(100, "b.com", "c_b", 5, "", 1) // kimlik yeniden kullanıldı: müşteri B (aynı bayi), hedefi YOK
	kSil(t, o.db, 100, "b.com", "c_b")
	dgDomain(t, o.db, 101, "b.com", "c_b", 5) // B hostingini yeniden açtı
	if n := dgBagla(t, o.r, "c_b", 101, false); n != 1 {
		t.Fatalf("kontrol: B'nin yedeği bağlanmalı: %d", n)
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backup_destinations WHERE domain_id=101`) != 0 {
		t.Errorf("🔴 müşteri A'nın FTP kimlik bilgisi (ftp.a.invalid) müşteri B'nin domainine geri yüklendi")
	}
}

// BULGU E7c — aynı kök neden: A'nın yetimleri bittikten sonra bile B'nin canlı satırları eski_domain_id=domain_id'yi
// "kullanımda" gösterdiği için A'nın parolalı anlık görüntüsü temizlenmez.
func TestDGBulgu_E7c_IdYenidenKullanimi_KullanilmayanAnlikGoruntu_ParolaTabloda_Kalmamali(t *testing.T) {
	o := dgYeni(t)
	yetimUzakSahte(t)
	ctx := context.Background()
	o.hesap(100, "a.com", "c_a", 5, "ftp.a.invalid", 1)
	kSil(t, o.db, 100, "a.com", "c_a")
	// A'nın yetimleri başka yoldan gitti (ör. kayıtlar elle temizlendi): anlık görüntü artık kimseye ait değil.
	g113_exec(t, o.db, `DELETE FROM backups WHERE eski_domain_id=100`)
	g113_exec(t, o.db, `UPDATE backup_yetim_hedefler SET olusturma=NOW() - INTERVAL 3 HOUR`)
	o.hesap(100, "b.com", "c_b", 5, "ftp.b.invalid", 1) // kimlik yeniden kullanıldı, B'nin canlı yedeği var
	yetimHedefArtiklariniTemizle(ctx, o.db)
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backup_yetim_hedefler WHERE host='ftp.a.invalid'`) != 0 {
		t.Error("🔴 A'nın artık kullanılmayan anlık görüntüsü (parola) tabloda kaldı: canlı B'nin satırları onu 'kullanımda' gösteriyor")
	}
}

// BULGU E8 — zorla bağlama + aynı bayi: yedeğin alan adı farklı (başka müşteri) olsa bile bayi eşit diye eski müşterinin
// FTP kimlik bilgisi yeni müşterinin domainine geri yüklenir; "parola boş = mevcut korunur" semantiği ile yeni müşteri host'u
// kendi sunucusuna çevirip eski müşterinin parolasını kendi sunucusuna gönderebilir.
func TestDGBulgu_E8_ZorlaBaglama_AyniBayi_EskiMusterininFTPParolasiYeniHostaYonlendirilebilir(t *testing.T) {
	o := dgYeni(t)
	yetimUzakSahte(t)
	ctx := context.Background()
	o.hesap(500, "girgin.com", "c_girgin", 5, "ftp.a.invalid", 1) // müşteri A (parola: kParola)
	kSil(t, o.db, 500, "girgin.com", "c_girgin")
	dgDomain(t, o.db, 501, "girgin.net", "c_girgin", 5) // müşteri B: aynı bayi, aynı sk
	if kod, g := advIstek(o.r, "POST", "/s/c_girgin/bagla", `{"domain_id":501,"zorla":true}`); kod != 200 {
		t.Fatalf("zorla bağlama: %d %s", kod, g)
	}
	n := g113_say(t, o.db, `SELECT COUNT(*) FROM backup_destinations WHERE domain_id=501`)
	if n != 0 {
		t.Errorf("🔴 başka müşterinin (girgin.com) FTP kimlik bilgisi girgin.net domainine geri yüklendi")
	}
	h := &Handlers{DB: o.db}
	rt := chi.NewRouter()
	rt.Put("/d/{id}/dest", h.PutDestination)
	advIstek(rt, "PUT", "/d/501/dest", `{"tip":"ftp","host":"saldirgan.invalid","port":21,"kullanici":"ftpkul","parola":"","uzak_dizin":"/","aktif":true}`)
	if d, _ := readDestination(ctx, o.db, 501); d != nil && d.Host == "saldirgan.invalid" && d.Parola == kParola {
		t.Errorf("🔴 müşteri A'nın FTP PAROLASI müşteri B'nin kontrolündeki host'a (%s) bağlandı", d.Host)
	}
}

// ================================================================ KARAR TESTLERİ (BULGULAR-DOGRULAMA.md Y1 madde 4)

// dgSahteFTP: loopback'te minimal FTP sunucusu; istemcinin PASS komutuyla gönderdiği değeri yakalar.
func dgSahteFTP(t *testing.T) (port int, parola func() string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback dinlenemiyor: %v", err)
	}
	var mu sync.Mutex
	var alinan []string
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(10 * time.Second))
				_, _ = fmt.Fprint(c, "220 dg-sahte-ftp\r\n")
				r := bufio.NewReader(c)
				for {
					satir, err := r.ReadString('\n')
					if err != nil {
						return
					}
					satir = strings.TrimRight(satir, "\r\n")
					up := strings.ToUpper(satir)
					switch {
					case strings.HasPrefix(up, "USER "):
						_, _ = fmt.Fprint(c, "331 parola gerekli\r\n")
					case strings.HasPrefix(up, "PASS "):
						mu.Lock()
						alinan = append(alinan, satir[5:])
						mu.Unlock()
						_, _ = fmt.Fprint(c, "530 yanlis\r\n")
						return
					default:
						_, _ = fmt.Fprint(c, "500 anlasilmadi\r\n")
					}
				}
			}(c)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().(*net.TCPAddr).Port, func() string {
		mu.Lock()
		defer mu.Unlock()
		if len(alinan) == 0 {
			return ""
		}
		return alinan[len(alinan)-1]
	}
}

// KARAR TESTİ Y1(4) — TestDestination: host/port/kullanıcı değişip parola BOŞ bırakılırsa kayıtlı (başka müşteriden geri yüklenmiş olabilir)
// parola yeni hosta GÖNDERİLMEMELİ (kaydetmeden test bile yeter: kiracı parolayı kendi sunucusuna akıtır). Kontrol: açık parola gönderilince
// sahte sunucu onu alır (düzenek çalışıyor).
func TestDGBulgu_Y1d_TestDestination_HostDegisinceBosParola_KayitliParolaGonderilmemeli(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl yok")
	}
	o := dgYeni(t)
	port, alinan := dgSahteFTP(t)
	dgDomain(t, o.db, 1600, "t.test", "c_t16", 5)
	ham := gizli.SaklaBagli(kParola, "yedek")
	g113_exec(t, o.db, `INSERT INTO backup_destinations (domain_id, tip, host, port, kullanici, parola, uzak_dizin, aktif) VALUES (1600,'ftp','ftp.eski.invalid',21,'ftpkul',?,'/',0)`, ham)
	h := &Handlers{DB: o.db}
	rt := chi.NewRouter()
	rt.Post("/d/{id}/dest/test", h.TestDestination)
	govde := func(parola string) string {
		return fmt.Sprintf(`{"tip":"ftp","host":"127.0.0.1","port":%d,"kullanici":"ftpkul","parola":%q,"uzak_dizin":"/"}`, port, parola)
	}
	advIstek(rt, "POST", "/d/1600/dest/test", govde("KONTROL-PAROLA-1"))
	if alinan() != "KONTROL-PAROLA-1" {
		t.Skipf("kontrol: sahte FTP sunucusu açık parolayı almadı (%q): düzenek çalışmıyor", alinan())
	}
	advIstek(rt, "POST", "/d/1600/dest/test", govde(""))
	if alinan() == kParola {
		t.Errorf("🔴 host değişip parola boş bırakılınca KAYITLI parola (%q) kullanıcının seçtiği hosta gönderildi", alinan())
	}
}

// KARAR TESTİ Y1(4) — PutDestination: host değişip parola boşsa 400 ("parola yeniden girilmeli"); eski parola yeni hosta bağlanmamalı.
func TestDGBulgu_Y1d_PutDestination_HostDegisinceBosParola_Reddedilmeli(t *testing.T) {
	o := dgYeni(t)
	dgDomain(t, o.db, 1601, "p.test", "c_p16", 5)
	h := &Handlers{DB: o.db}
	rt := chi.NewRouter()
	rt.Put("/d/{id}/dest", h.PutDestination)
	if kod, g := advIstek(rt, "PUT", "/d/1601/dest", `{"tip":"ftp","host":"ftp.a.invalid","port":21,"kullanici":"u","parola":"ILK-PAROLA-9","uzak_dizin":"/","aktif":true}`); kod != 200 {
		t.Fatalf("ilk kayıt: %d %s", kod, g)
	}
	kod, g := advIstek(rt, "PUT", "/d/1601/dest", `{"tip":"ftp","host":"baska.invalid","port":21,"kullanici":"u","parola":"","uzak_dizin":"/","aktif":true}`)
	d, _ := readDestination(context.Background(), o.db, 1601)
	if kod == 200 && d != nil && d.Host == "baska.invalid" && d.Parola == "ILK-PAROLA-9" {
		t.Errorf("🔴 host değişti, parola boş bırakıldı: eski parola yeni hosta bağlandı (yanıt %d %s)", kod, g)
	}
}

// ================================================================ M — yetimYedegiSil ↔ bağlama yarışı

// UYARLAMA (düzeltme turu, bağımsız inceleme bulgusu): M1/M2 ilk yazıldığında "uzak silme sürerken araya giren bağlama
// SONRADAN koşullu DELETE ile yakalanır" tasarımını kodluyordu: bağlama başarıyla biter (iki satırı da bağlar), silme
// errYetimDegil ile satırı + yerel dosyayı korurdu. Bu, bağlanan CANLI yedeğin UZAK kopyasının silinmesini önlemiyordu (uzak
// adım DELETE'ten ÖNCE koşar; yerel kopya "uzağa taşındı" diye silinmişse tek kopya yok olur). Artık uzak silmesi süren satır
// süreç içi kayıt defterindedir (yetimSilinmekte) ve bağlama onu BAĞLAMAZ; kalan satırlar normal bağlanır. Değişmezler:
//   - uzak silmesi çağrılan hiçbir yedek canlı kalmaz (satır ve yerel dosya gitmiştir);
//   - bağlanan hiçbir yedeğin uzak silmesi çağrılmaz, yerel dosyası yerindedir;
//   - uzak silmesi SÜREN satır, o sırada başlayan bağlamayla bağlanmaz (bağlama diğer satırları bağlar, beklemez).

// dgBaglaIstek: dgBagla'nın t kullanmayan biçimi (HTTP durum kodu + bağlanan sayısı).
func dgBaglaIstek(r http.Handler, sk string, domainID int64) (kod int, baglanan int) {
	kod, g := advIstek(r, "POST", "/s/"+sk+"/bagla", fmt.Sprintf(`{"domain_id":%d,"zorla":false}`, domainID))
	var y struct {
		Baglanan int `json:"baglanan"`
	}
	_ = json.Unmarshal([]byte(g), &y)
	return kod, y.Baglanan
}

// dgYarisDegismezleri: uzak silmesi çağrılan dosyalar / bağlanan dosyalar için değişmezleri denetler.
func dgYarisDegismezleri(t *testing.T, o *dgOrtam, sk string, hedefDomain int64, dosyalar, uzakSilinen []string) {
	t.Helper()
	uzakta := map[string]bool{}
	for _, d := range uzakSilinen {
		uzakta[d] = true
	}
	for _, d := range dosyalar {
		var canli, var1 int
		if err := o.db.QueryRow(`SELECT COUNT(*) FROM backups WHERE dosya=? AND domain_id=?`, d, hedefDomain).Scan(&canli); err != nil {
			t.Fatal(err)
		}
		if err := o.db.QueryRow(`SELECT COUNT(*) FROM backups WHERE dosya=?`, d).Scan(&var1); err != nil {
			t.Fatal(err)
		}
		dosyaVar := varMi(filepath.Join(o.kok, sk, d))
		switch {
		case uzakta[d] && (var1 != 0 || dosyaVar):
			t.Errorf("🔴 uzak kopyası silinen yedek canlı/yarım kaldı: %s (satır=%d dosya=%v) — uzak silme sürerken araya giren bağlama bunu kurtarsa bile uzak kopya gitmişti", d, var1, dosyaVar)
		case !uzakta[d] && canli == 1 && !dosyaVar:
			t.Errorf("🔴 bağlanan (canlı) yedeğin YEREL DOSYASI silindi: %s", d)
		case !uzakta[d] && canli != 1:
			t.Errorf("uzağı silinmeyen yedek ne bağlandı ne silindi (satır=%d): %s", var1, d)
		}
	}
}

// BULGU M1 — süpürme bir yetimin uzak kopyasını silerken (ağ gecikmesi) müşteri yeni hostingini açıp yetimleri bağlamaya
// çalışır. Eski kod "hâlâ yetim mi" denetimini YALNIZ başta yapıp sonda koşulsuz DELETE + os.Remove çalıştırıyordu → CANLI yedek
// silinirdi; koşullu DELETE bunu önledi ama uzak kopya yine silinmiş olurdu.
func TestDGBulgu_M1_Supurme_UzakSilmeSirasindaBaglama_SilinmekteOlanBaglanmazDigeriBaglanir(t *testing.T) {
	o := dgYeni(t)
	ctx := context.Background()
	dosyalar, _ := o.hesap(700, "tk.com", "c_tk", 5, "", 2)
	kSil(t, o.db, 700, "tk.com", "c_tk")
	g113_exec(t, o.db, `UPDATE backups SET son_kullanma=DATE_SUB(NOW(), INTERVAL 1 HOUR) WHERE domain_id=0`)
	dgDomain(t, o.db, 701, "tk.com", "c_tk", 5)

	var uzakSilinen []string
	var bagKod, bagN int
	eski := yetimUzakSil
	t.Cleanup(func() { yetimUzakSil = eski })
	var bir sync.Once
	yetimUzakSil = func(ctx context.Context, db *sql.DB, d *Destination, dosya string) error {
		uzakSilinen = append(uzakSilinen, dosya)
		bir.Do(func() {
			// uzak silme (ağ) sürerken müşteri yeni hostingini açıp yetimleri bağlar
			bagKod, bagN = dgBaglaIstek(o.r, "c_tk", 701)
		})
		return nil
	}
	SilinenSupur(ctx, o.db)
	if bagKod != 200 || bagN != 1 {
		t.Errorf("silinmekte olan satır HARİÇ diğeri bağlanmalı (200, 1 satır): %d, %d", bagKod, bagN)
	}
	if len(uzakSilinen) != 1 || uzakSilinen[0] != dosyalar[0] {
		t.Fatalf("🔴 uzak silme yalnız silinmekte olan ilk satır için çağrılmalı (bağlanan ikinci satırın uzak kopyası ASLA): %v", uzakSilinen)
	}
	dgYarisDegismezleri(t, o, "c_tk", 701, dosyalar, uzakSilinen)
	if n := g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE domain_id=701 AND tip='kurtarma'`); n != 1 {
		t.Errorf("domain 701'de bağlanan 1 satır beklenirdi: %d", n)
	}
}

// BULGU M2 — aynı yarış yönetici DELETE ucunda: 200 döner, müşterinin kurtardığı yedek (ve uzak kopyası) yerinde kalır.
func TestDGBulgu_M2_YoneticiSilUcu_UzakSilmeSirasindaBaglama_SilinmekteOlanBaglanmazDigeriBaglanir(t *testing.T) {
	o := dgYeni(t)
	dosyalar, _ := o.hesap(710, "tm.com", "c_tm", 5, "", 2)
	kSil(t, o.db, 710, "tm.com", "c_tm")
	dgDomain(t, o.db, 711, "tm.com", "c_tm", 5)

	var uzakSilinen []string
	var bagKod, bagN int
	eski := yetimUzakSil
	t.Cleanup(func() { yetimUzakSil = eski })
	var bir sync.Once
	yetimUzakSil = func(ctx context.Context, db *sql.DB, d *Destination, dosya string) error {
		uzakSilinen = append(uzakSilinen, dosya)
		bir.Do(func() { bagKod, bagN = dgBaglaIstek(o.r, "c_tm", 711) })
		return nil
	}
	kod, g := advIstek(o.r, "DELETE", "/s/c_tm", "")
	t.Logf("yönetici DELETE yanıtı: %d %s", kod, g)
	if kod != 200 {
		t.Errorf("silme ucu 200 dönmeli (araya bağlanan satırlar atlanır, hata sayılmaz): %d %s", kod, g)
	}
	if bagKod != 200 || bagN != 1 {
		t.Errorf("silinmekte olan satır HARİÇ diğeri bağlanmalı (200, 1 satır): %d, %d", bagKod, bagN)
	}
	if len(uzakSilinen) != 1 || uzakSilinen[0] != dosyalar[0] {
		t.Fatalf("🔴 uzak silme yalnız silinmekte olan ilk satır için çağrılmalı: %v", uzakSilinen)
	}
	dgYarisDegismezleri(t, o, "c_tm", 711, dosyalar, uzakSilinen)
}

// ================================================================ N — yetimleriBagla değişmezleri

// BULGU N1 — yetimleriBagla hedef domainin VARLIĞINI doğrulamaz (yalnız çağıranlar bakar): domain, kontrol ile
// bağlama arasında silinirse (HostingYik) satırlar var olmayan bir domaine bağlanır (silinme NULL, domain yok).
func TestDGBulgu_N1_YetimleriBagla_VarOlmayanDomaina_BaglamaYapmamali(t *testing.T) {
	o := dgYeni(t)
	yetimUzakSahte(t)
	o.hesap(900, "n.com", "c_n9", 5, "ftp.n.invalid", 2)
	kSil(t, o.db, 900, "n.com", "c_n9")
	n, err := yetimleriBagla(context.Background(), o.db, 424242, "sistem_kullanici=?", "c_n9")
	if err == nil && n > 0 {
		t.Errorf("🔴 %d yetim var olmayan domaine (424242) bağlandı", n)
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE domain_id=424242`) != 0 {
		t.Error("🔴 var olmayan domaine bağlı satır kaldı: S2 sahipsiz turu bunları yeniden yetim yapana dek yedekler hiçbir listede görünmez")
	}
}

// ================================================================ L — silme izi süre hesabı

// BULGU L1 — son_kullanma/silinme TIMESTAMP (en çok 2038-01-19). Ayar üst sınırı 3650 gün kabul edilir; saat
// 2028-01-22'yi geçince NOW()+3650 gün bu sınırı aşar: strict modda UPDATE hata verir (iz YAZILAMAZ, satırlar hiçbir listede
// görünmeyen sahipsiz kalır ve S2 de aynı hatayla düşer), strict kapalıysa '0000-00-00' yazılır (satır HEMEN süresi dolmuş sayılıp silinir).
func TestDGBulgu_L1_SilmeIzi_TIMESTAMP2038Siniri_3650GunAyari(t *testing.T) {
	for _, mod := range []struct{ ad, sqlMode string }{{"strict (varsayılan)", ""}, {"strict kapalı", "''"}} {
		t.Run(mod.ad, func(t *testing.T) {
			db := kDB(t)
			db.SetMaxOpenConns(1)
			db.SetMaxIdleConns(1)
			if mod.sqlMode != "" {
				g113_exec(t, db, "SET SESSION sql_mode="+mod.sqlMode)
			}
			g113_exec(t, db, `INSERT INTO cp_ayarlar (anahtar, deger) VALUES ('silinen_yedek_gun','3650')`)
			kHesap(t, db, 800, "l.com", "c_l", 0, "", dgAd("c_l", 0), dgAd("c_l", 1))
			var epoch int64
			if err := db.QueryRow(`SELECT UNIX_TIMESTAMP('2028-06-01 12:00:00')`).Scan(&epoch); err != nil {
				t.Fatal(err)
			}
			g113_exec(t, db, fmt.Sprintf("SET SESSION TIMESTAMP = %d", epoch))
			ctx := context.Background()
			iz := SilinenIzHazirla(ctx, db, 800, "l.com", "c_l")
			g113_exec(t, db, `DELETE FROM domains WHERE id=800`)
			iz.Yaz(ctx, db)
			g113_exec(t, db, "SET SESSION TIMESTAMP = DEFAULT")
			if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=0 AND silinme IS NOT NULL`) != 2 {
				t.Errorf("🔴 ayar=3650 gün + saat 2028-06: silme izi YAZILAMADI (TIMESTAMP 2038 taşması); satırlar domain_id=800'de sahipsiz kaldı")
			}
			if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=0 AND son_kullanma IS NOT NULL AND son_kullanma <= NOW()`) != 0 {
				t.Errorf("🔴 son_kullanma geçmiş/sıfır tarih yazıldı: satırlar bir sonraki süpürmede HEMEN silinir")
			}
		})
	}
}
