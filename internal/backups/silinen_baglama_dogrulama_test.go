package backups

// silinen_baglama_dogrulama_test.go — DÜŞMANCA DOĞRULAMA (yetim yedek tasarımı, commit e449f18).
//
//	A  domain_id yeniden kullanımı (AUTO_INCREMENT geri sarılmış / yedekten dönülmüş DB)
//	B  bağlama koşulları + eşzamanlılık (Y1 sonrası: GET listesi bağlamaz; bağlama = YetimleriDevral / yönetici ucu)
//	C  elle bağlama / silme uçları (zorla, sk ve gövde saldırıları, "reddedilen istek hiçbir şeyi değiştirmez")
//
// Mevcut silinen_*_test.go yardımcılarını (kDB, kRouter, kHesap, kSil, yetimUzakSahte, logYakala, advIstek...)
// kullanır. Gerçek BackupRoot'a YAZILMAZ (yedekKok geçici dizine çevrilir), AĞA ÇIKILMAZ (uzak silme sahte).
// Geçici MariaDB gerekir (GOSP_TEST_GECICI_DSN yoksa atlanır).
//
// Adında "Bulgu" geçen testler ÜRETİM KODUNDAKİ GERÇEK BİR HATAYI kanıtlar: bugün KIRMIZIDIR.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/go-sql-driver/mysql"
)

// ---------------------------------------------------------------- ortak yardımcılar (dg önekli)

type dgOrtam struct {
	t   *testing.T
	db  *sql.DB
	kok string
	r   http.Handler
}

func dgYeni(t *testing.T) *dgOrtam {
	t.Helper()
	db := kDB(t)
	kok := silinenOrtam(t)
	return &dgOrtam{t: t, db: db, kok: kok, r: kRouter(db)}
}

// dgAd: sk için geçerli, benzersiz yedek dosya adı (<sk>-2026MMDD-HHMMSS.tar.gz).
func dgAd(sk string, i int) string {
	return fmt.Sprintf("%s-2026%04d-%06d.tar.gz", sk, 101+i/60, i)
}

// hesap: canlı hosting + diskte gerçek dosyalar + backups satırları (+ host doluysa domaine özel uzak hedef).
func (o *dgOrtam) hesap(id int64, alan, sk string, rid int64, host string, n int) (dosyalar []string, ham string) {
	o.t.Helper()
	for i := 0; i < n; i++ {
		d := dgAd(sk, i)
		silinenDosya(o.t, o.kok, sk, d)
		dosyalar = append(dosyalar, d)
	}
	ham = kHesap(o.t, o.db, id, alan, sk, rid, host, dosyalar...)
	return dosyalar, ham
}

// dgDomain: yalnız domains satırı (rid=0 → NULL = yönetici).
func dgDomain(t *testing.T, db *sql.DB, id int64, alan, sk string, rid int64) {
	t.Helper()
	var ridV any
	if rid > 0 {
		ridV = rid
	}
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES (?,?,?,?)`, id, alan, sk, ridV)
}

// dgYetimIDleri: eski_domain_id'si eskiID olan yetim satırların kimlikleri (artan).
func dgYetimIDleri(t *testing.T, db *sql.DB, eskiID int64) []int64 {
	t.Helper()
	rows, err := db.Query(`SELECT id FROM backups WHERE eski_domain_id=? AND domain_id=0 AND silinme IS NOT NULL ORDER BY id`, eskiID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out = append(out, id)
	}
	return out
}

// dgDokumSorgu: sorgunun tüm satırlarını deterministik metne döker (önce/sonra karşılaştırması için).
func dgDokumSorgu(t *testing.T, db *sql.DB, etiket, q string, args ...any) string {
	t.Helper()
	rows, err := db.Query(q, args...)
	if err != nil {
		t.Fatalf("döküm %s: %v", etiket, err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	var sb strings.Builder
	for rows.Next() {
		vals := make([]sql.RawBytes, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		sb.WriteString(etiket)
		for i, v := range vals {
			if v == nil {
				fmt.Fprintf(&sb, "|%s=NULL", cols[i])
			} else {
				fmt.Fprintf(&sb, "|%s=%s", cols[i], v)
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// dgDokum: verilen tabloların tüm içeriği (ORDER BY 1).
func dgDokum(t *testing.T, db *sql.DB, tablolar ...string) string {
	t.Helper()
	var sb strings.Builder
	for _, tb := range tablolar {
		sb.WriteString(dgDokumSorgu(t, db, tb, "SELECT * FROM "+tb+" ORDER BY 1"))
	}
	return sb.String()
}

// dgBagla: yönetici ucuyla (POST /s/{sk}/bagla) elle bağlar; List'in otomatik bağlama yan etkisine bağımlı olmayan kurulum yolu.
func dgBagla(t *testing.T, r http.Handler, sk string, domainID int64, zorla bool) int {
	t.Helper()
	govde := fmt.Sprintf(`{"domain_id":%d,"zorla":%v}`, domainID, zorla)
	kod, g := advIstek(r, "POST", "/s/"+sk+"/bagla", govde)
	if kod != 200 {
		t.Fatalf("elle bağlama %s→%d: %d %s", sk, domainID, kod, g)
	}
	var y struct {
		Baglanan int `json:"baglanan"`
	}
	_ = json.Unmarshal([]byte(g), &y)
	return y.Baglanan
}

type dgYedekListe struct {
	ID       int64  `json:"id"`
	DomainID int64  `json:"domain_id"`
	Tip      string `json:"tip"`
	Kaynak   string `json:"kaynak"`
	Dosya    string `json:"dosya"`
}

// dgListe: GET /d/{id}/backups (Y1: salt okunur, hiçbir şeyi bağlamaz).
func dgListe(t *testing.T, r http.Handler, id int64) []dgYedekListe {
	t.Helper()
	kod, g := advIstek(r, "GET", fmt.Sprintf("/d/%d/backups", id), "")
	if kod != 200 {
		t.Fatalf("liste %d: %d %s", id, kod, g)
	}
	var out []dgYedekListe
	if err := json.Unmarshal([]byte(g), &out); err != nil {
		t.Fatalf("liste JSON: %v %s", err, g)
	}
	return out
}

// ================================================================ A — domain_id yeniden kullanımı

// Eski müşterinin domain_id'si BAŞKA bir müşteriye (aynı bayi, başka alan adı + sk) verilirse: yeni domainin
// liste/sil/geri yükle/içerik/indir uçları, otomatik bağlama ve hedef okuması eski yetimlere/kimlik
// bilgilerine ULAŞMAMALI; DB'de hiçbir satır değişmemeli.
func TestDG_A1_DomainIDYenidenKullanimi_BaskaAlanAdiVeSK_EskiYetimiHicbirYolGormez(t *testing.T) {
	o := dgYeni(t)
	yetimUzakSahte(t)
	ctx := context.Background()
	dosyalar, _ := o.hesap(100, "eski.com", "c_eski", 5, "ftp.eski.invalid", 3)
	kSil(t, o.db, 100, "eski.com", "c_eski")
	yetimler := dgYetimIDleri(t, o.db, 100)
	if len(yetimler) != 3 || g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE domain_id=100`) != 0 {
		t.Fatalf("kurulum: 3 yetim (domain_id=0) beklenirdi: %v", yetimler)
	}

	// AUTO_INCREMENT geri sarılmış gibi: AYNI kimlik (100), AYNI bayi (5), BAŞKA alan adı + sk.
	dgDomain(t, o.db, 100, "yeni.org", "c_yeni", 5)
	yeniDosya := dgAd("c_yeni", 0)
	silinenDosya(t, o.kok, "c_yeni", yeniDosya)
	g113_exec(t, o.db, `INSERT INTO backups (domain_id, dosya, tip) VALUES (100, ?, 'oto')`, yeniDosya)
	oncesi := dgDokum(t, o.db, "backups", "backup_destinations", "backup_yetim_hedefler", "audit_log")

	liste := dgListe(t, o.r, 100)
	if len(liste) != 1 || liste[0].Dosya != yeniDosya {
		t.Fatalf("🔴 yeni domainin yedek listesi eski müşterinin yedeğini içeriyor: %+v", liste)
	}
	if n := devral(ctx, o.db, 100); n != 0 {
		t.Errorf("🔴 otomatik bağlama başka alan adı/sk'lı yeni domaine %d yetim bağladı", n)
	}
	for _, bid := range yetimler {
		for _, c := range []struct{ m, y, b string }{
			{"DELETE", "/d/100/backups/%d", ""},
			{"POST", "/d/100/backups/%d/geriyukle", `{}`},
			{"GET", "/d/100/backups/%d/icerik", ""},
			{"GET", "/d/100/backups/%d/indir", ""},
		} {
			if kod, _ := advIstek(o.r, c.m, fmt.Sprintf(c.y, bid), c.b); kod != 404 {
				t.Errorf("🔴 %s %s: eski müşterinin yetim yedeğine yeni domain üzerinden erişildi (kod %d)", c.m, fmt.Sprintf(c.y, bid), kod)
			}
		}
	}
	if d, err := readDestination(ctx, o.db, 100); d != nil || err != nil {
		t.Errorf("🔴 yeni domain (id yeniden kullanıldı) eski müşterinin FTP hedefini okuyabiliyor: %+v %v", d, err)
	}
	if sonrasi := dgDokum(t, o.db, "backups", "backup_destinations", "backup_yetim_hedefler", "audit_log"); sonrasi != oncesi {
		t.Errorf("DB değişti (okuma/404 uçları yazmamalı)\n--- önce ---\n%s--- sonra ---\n%s", oncesi, sonrasi)
	}
	_, g := advIstek(o.r, "GET", "/s", "")
	if !strings.Contains(g, `"sk":"c_eski"`) || !strings.Contains(g, `"baglanabilir_domain_id":null`) {
		t.Errorf("yönetici listesinde eski grup yetim ve bağlanabilir hedefsiz görünmeli: %s", g)
	}
	for _, d := range dosyalar {
		if !varMi(filepath.Join(o.kok, "c_eski", d)) {
			t.Errorf("eski müşterinin yedek dosyası kayboldu: %s", d)
		}
	}
}

// BULGU A2 (bileşik hata) — FK kalktığı için silme izi yazılamazsa (tüm denemeler düşerse) yedek satırları eski domain_id'yi taşımaya
// devam eder; S2 turu (panel açılışı / 04:00) gelmeden domain kimliği başka bir müşteriye verilirse, yeni müşteri eski müşterinin
// yedeklerini listeler (indirme/geri yükleme uçları da aynı sorguyla açılır). Eskiden FK CASCADE bunu imkânsız kılıyordu.
// AZALTMA (koordinatör: "ucuz azaltma bulursan uygula"): domain satırını yazan HER yol (domains.Create, taşıma) yazar yazmaz
// YetimKalintilariniAyir çağırır — yeni bir domainin kimliğine bağlı yedek satırı olamaz; varsa bunlar kimliği önceden taşıyan
// silinmiş hostingin kalıntılarıdır ve sk dosya adından türetilerek yetim işaretlenir. Bu test o çağrıyı Create'in yaptığı gibi yapar.
// Kalan risk: domains satırı bu iki yol DIŞINDA (elle SQL) yazılırsa pencere açıktır — bkz. TestDG_A2b.
func TestDGBulgu_A2_IzYazilamadi_IdYenidenKullanildi_EskiMusterininYedegiYeniMusteriyeGorunur(t *testing.T) {
	o := dgYeni(t)
	yetimUzakSahte(t)
	logYakala(t)
	o.hesap(100, "eski.com", "c_eski", 5, "", 2)
	tetikKur(t, o.db, 1000) // her UPDATE reddedilir: iz tüm denemelerde de yazılamaz
	kSil(t, o.db, 100, "eski.com", "c_eski")
	g113_exec(t, o.db, `DROP TRIGGER tetik_bu`)
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE domain_id=100 AND silinme IS NULL`) != 2 {
		t.Fatal("kurulum: satırlar işaretsiz, domain_id=100'de kalmalıydı")
	}
	dgDomain(t, o.db, 100, "yeni.org", "c_yeni", 5)                           // S2 turu henüz koşmadan kimlik başka müşteriye verildi
	if n := YetimKalintilariniAyir(context.Background(), o.db, 100); n != 2 { // domains.Create / taşıma bunu hemen çağırır
		t.Fatalf("kimliği önceden taşıyan 2 kalıntı satırı yetim işaretlenmeliydi: %d", n)
	}
	if liste := dgListe(t, o.r, 100); len(liste) != 0 {
		t.Errorf("🔴 yeni müşterinin (yeni.org / c_yeni) yedek listesinde eski müşterinin (eski.com / c_eski) %d yedeği görünüyor: %+v", len(liste), liste)
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE domain_id=0 AND eski_domain_id=100 AND sistem_kullanici='c_eski' AND silinme IS NOT NULL AND LENGTH(iz_id)=32`) != 2 {
		t.Error("kalıntılar sk=c_eski ile yetim olmalı (yönetici listesinde, eski sahibin süpürmesine tabi)")
	}
}

// A2b — KALAN RİSK (belgelendi, kod azaltması yok): domains satırı Create/taşıma DIŞINDA yazılırsa ve iz yazılamamışsa pencere
// S2 turuna dek açıktır. Olasılık çok düşük: iz iki+ denemede de düşmeli VE AUTO_INCREMENT geri sarılmalı (MariaDB ≥10.2.4'te kalıcı).
func TestDG_A2b_KalanRisk_DomainSatiriElleYazilirsaPencereAcik_S2Kapatir(t *testing.T) {
	o := dgYeni(t)
	yetimUzakSahte(t)
	logYakala(t)
	o.hesap(100, "eski.com", "c_eski", 5, "", 2)
	tetikKur(t, o.db, 1000)
	kSil(t, o.db, 100, "eski.com", "c_eski")
	g113_exec(t, o.db, `DROP TRIGGER tetik_bu`)
	dgDomain(t, o.db, 100, "yeni.org", "c_yeni", 5)
	if liste := dgListe(t, o.r, 100); len(liste) != 2 {
		t.Skipf("kalan risk kapanmış görünüyor (liste %d satır): bu testi güncelleyin", len(liste))
	}
	// Domain silinince (ya da hiç bağlanmadan) S2 turu satırları sahipsiz bulur ve kapatır; domain VARKEN S2 onları görmez.
	g113_exec(t, o.db, `DELETE FROM domains WHERE id=100`)
	if m, _ := SilinenSahipsizIsaretle(context.Background(), o.db); m != 2 {
		t.Errorf("domain yokken S2 2 satırı yetim işaretlemeli: %d", m)
	}
}

// ================================================================ B — otomatik bağlama

// Koşul tablosu: yalnız (alan adı, bayi, sk) üçlüsü BİREBİR aynıysa bağlanır. Her satırda eski kimlik (100)
// yeniden kullanılır ya da yeni kimlik verilir.
func TestDG_B1_OtoBagla_KosulTablosu(t *testing.T) {
	type vaka struct {
		ad       string
		yeniID   int64
		alan, sk string
		rid      int64
		bagli    bool
	}
	vakalar := []vaka{
		{"birebir aynı kimlik, id yeniden kullanıldı (kontrol)", 100, "eski.com", "c_eski", 5, true},
		{"birebir aynı kimlik, yeni id", 101, "eski.com", "c_eski", 5, true},
		{"alan adı büyük harf (harmanlama büyük/küçük duyarsız; elle bağlamayla tutarlı)", 100, "ESKI.COM", "c_eski", 5, true},
		{"farklı bayi", 100, "eski.com", "c_eski", 9, false},
		{"bayi yok (yönetici sahipli)", 100, "eski.com", "c_eski", 0, false},
		{"farklı sk (c_eski2)", 100, "eski.com", "c_eski2", 5, false},
		{"aynı sk, farklı alan adı (girgin.com / girgin.net)", 100, "eski.net", "c_eski", 5, false},
		{"www. ön eki", 100, "www.eski.com", "c_eski", 5, false},
		{"sondaki nokta", 100, "eski.com.", "c_eski", 5, false},
		{"alt alan adı", 100, "a.eski.com", "c_eski", 5, false},
	}
	for _, v := range vakalar {
		t.Run(v.ad, func(t *testing.T) {
			o := dgYeni(t)
			yetimUzakSahte(t)
			ctx := context.Background()
			o.hesap(100, "eski.com", "c_eski", 5, "ftp.eski.invalid", 2)
			kSil(t, o.db, 100, "eski.com", "c_eski")
			yetimler := dgYetimIDleri(t, o.db, 100)
			dgDomain(t, o.db, v.yeniID, v.alan, v.sk, v.rid)
			oncesi := dgDokum(t, o.db, "backups", "backup_destinations", "backup_yetim_hedefler")

			n := devral(ctx, o.db, v.yeniID)
			if !v.bagli {
				if n != 0 {
					t.Fatalf("🔴 başka müşteriye %d yetim BAĞLANDI", n)
				}
				if sonrasi := dgDokum(t, o.db, "backups", "backup_destinations", "backup_yetim_hedefler"); sonrasi != oncesi {
					t.Errorf("bağlanmayan durumda DB değişti\n%s\n---\n%s", oncesi, sonrasi)
				}
				return
			}
			if n != int64(len(yetimler)) {
				t.Fatalf("aynı kimlik: %d yetim bağlanmalıydı, %d", len(yetimler), n)
			}
			if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE domain_id=? AND eski_domain_id=0 AND silinme IS NULL AND son_kullanma IS NULL AND tip='kurtarma'`, v.yeniID) != len(yetimler) {
				t.Error("bağlanan satırlar: domain_id=yeni, eski_domain_id=0, silinme/son_kullanma NULL, tip=kurtarma olmalı")
			}
			if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE domain_id=0`) != 0 {
				t.Error("bağlandıktan sonra yetim kalmamalı")
			}
			for _, y := range dgListe(t, o.r, v.yeniID) {
				if y.Tip != "kurtarma" || y.Kaynak != "kurtarma" || y.DomainID != v.yeniID {
					t.Errorf("liste satırı: %+v", y)
				}
			}
		})
	}
}

// Var olmayan / geçersiz domain için otomatik bağlama HİÇBİR satıra dokunmaz.
func TestDG_B1b_OtoBagla_GecersizDomain_Dokunmaz(t *testing.T) {
	o := dgYeni(t)
	yetimUzakSahte(t)
	o.hesap(100, "eski.com", "c_eski", 5, "", 2)
	kSil(t, o.db, 100, "eski.com", "c_eski")
	oncesi := dgDokum(t, o.db, "backups")
	for _, id := range []int64{0, -1, 424242} {
		if n := devral(context.Background(), o.db, id); n != 0 {
			t.Errorf("domain %d: %d bağlandı", id, n)
		}
	}
	// boş alan adı / geçersiz sk'lı domain (gerçekte olmamalı ama DB bozuksa) da bağlamaz.
	g113_exec(t, o.db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES (7,'','c_eski',5),(8,'eski.com','../c_eski',5),(9,'eski.com','',5)`)
	for _, id := range []int64{7, 8, 9} {
		if n := devral(context.Background(), o.db, id); n != 0 {
			t.Errorf("bozuk domain satırı %d: %d bağlandı", id, n)
		}
	}
	if dgDokum(t, o.db, "backups") != oncesi {
		t.Error("DB değişti")
	}
}

// İki ayrı yetim grubu (farklı sk) karışmaz; bağlanan satırların kimlik/boyut/tarih/not alanları korunur.
func TestDG_B2_OtoBagla_IkiGrup_YalnizEslesenBaglanir_VeriAlanlariKorunur(t *testing.T) {
	o := dgYeni(t)
	yetimUzakSahte(t)
	ctx := context.Background()
	o.hesap(200, "a.com", "c_a", 5, "", 3)
	o.hesap(201, "b.com", "c_b", 5, "", 2)
	g113_exec(t, o.db, `UPDATE backups SET boyut_b=1234567, created_at='2026-01-02 03:04:05', notlar='eski not' WHERE domain_id IN (200,201)`)
	kSil(t, o.db, 200, "a.com", "c_a")
	kSil(t, o.db, 201, "b.com", "c_b")
	alanlar := func(sk string) string {
		return dgDokumSorgu(t, o.db, sk, `SELECT id, dosya, boyut_b, created_at, notlar, job_id FROM backups WHERE sistem_kullanici=? ORDER BY id`, sk)
	}
	aOnce, bOnce := alanlar("c_a"), alanlar("c_b")

	dgDomain(t, o.db, 210, "a.com", "c_a", 5)
	if n := devral(ctx, o.db, 210); n != 3 {
		t.Fatalf("c_a grubundan 3 satır bağlanmalıydı: %d", n)
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE sistem_kullanici='c_a' AND domain_id=210`) != 3 {
		t.Error("c_a satırları 210'a bağlanmalı")
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE sistem_kullanici='c_b' AND domain_id=0 AND eski_domain_id=201 AND silinme IS NOT NULL AND tip='oto'`) != 2 {
		t.Error("🔴 c_b grubu a.com domaini açılınca etkilendi")
	}
	if alanlar("c_a") != aOnce {
		t.Errorf("bağlanan satırların id/dosya/boyut/created_at/notlar alanları DEĞİŞTİ\n%s\n---\n%s", aOnce, alanlar("c_a"))
	}
	if alanlar("c_b") != bOnce {
		t.Error("c_b grubunun veri alanları değişti")
	}
	dgDomain(t, o.db, 211, "b.com", "c_b", 5)
	if n := devral(ctx, o.db, 211); n != 2 {
		t.Fatalf("c_b grubundan 2 satır bağlanmalıydı: %d", n)
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE sistem_kullanici='c_a' AND domain_id=210`) != 3 {
		t.Error("b.com bağlanırken a.com satırları kaydı")
	}
}

// KARAR TESTİ O1/Y1(1) (BULGULAR-DOGRULAMA.md) — GET /domains/{id}/backups SALT OKUNUR olmalı: yetimleri bağlamamalı, hedef geri yüklememeli,
// DB'ye hiçbir şey yazmamalı (API anahtarı yedek:oku, destek botu GET-only bu ucu tetikleyebiliyor; her çağrı FOR UPDATE'li işlem açıyor).
func TestDGBulgu_O1_ListeUcu_SaltOkunur_YetimleriBaglamaz(t *testing.T) {
	o := dgYeni(t)
	yetimUzakSahte(t)
	o.hesap(3200, "ro.com", "c_ro", 5, "ftp.ro.invalid", 2)
	kSil(t, o.db, 3200, "ro.com", "c_ro")
	dgDomain(t, o.db, 3201, "ro.com", "c_ro", 5) // aynı kimlikle yeniden açıldı
	oncesi := dgDokum(t, o.db, "backups", "backup_destinations", "backup_yetim_hedefler", "audit_log")
	if liste := dgListe(t, o.r, 3201); len(liste) != 0 {
		t.Errorf("🔴 GET listesi eski hesabın yetimlerini domainin yedekleri gibi döndürdü: %+v", liste)
	}
	if sonrasi := dgDokum(t, o.db, "backups", "backup_destinations", "backup_yetim_hedefler", "audit_log"); sonrasi != oncesi {
		t.Errorf("🔴 GET yedek listesi DB'yi DEĞİŞTİRDİ (otomatik bağlama + hedef geri yükleme)\n--- önce ---\n%s--- sonra ---\n%s", oncesi, sonrasi)
	}
}

// Yarış: aynı yeni domain için çok sayıda EŞZAMANLI devralma (YetimleriDevral) ve yedek listesi isteği → yetimler TEK kez bağlanır,
// anlık görüntü (parola) silinir, hiçbir istek 5xx vermez, listede yarım (0<n<tümü) görünüm olmaz, kilitlenme günlüğü yok.
// (Y1: GET listesi bağlamaz — bağlamayı yalnız devral çağrıları yapar; liste ya 0 ya N satır görür.) (-race ile de koşar.)
func TestDG_B3_Devral_EszamanliCagrilarVeListeler_CiftBaglamaYok(t *testing.T) {
	o := dgYeni(t)
	o.db.SetMaxOpenConns(64) // List açık satır tutarken ikinci sorgu çalıştırır: havuz yetmezse test kendini kilitler
	yetimUzakSahte(t)
	const N, G = 15, 12
	o.hesap(300, "yarisan.com", "c_yaris", 5, "ftp.yaris.invalid", N)
	kSil(t, o.db, 300, "yarisan.com", "c_yaris")
	dgDomain(t, o.db, 301, "yarisan.com", "c_yaris", 5)
	logs := logYakala(t)

	type sonuc struct {
		kod, adet int
		govde     string
	}
	sonuclar := make([]sonuc, G)
	var bagli int64
	var wg sync.WaitGroup
	basla := make(chan struct{})
	for i := 0; i < G; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			<-basla
			kod, g := advIstek(o.r, "GET", "/d/301/backups", "")
			var l []dgYedekListe
			_ = json.Unmarshal([]byte(g), &l)
			sonuclar[i] = sonuc{kod, len(l), g}
		}(i)
		go func() {
			defer wg.Done()
			<-basla
			atomic.AddInt64(&bagli, devral(context.Background(), o.db, 301))
		}()
	}
	close(basla)
	wg.Wait()

	for i, s := range sonuclar {
		if s.kod != 200 {
			t.Errorf("istek %d: %d %s", i, s.kod, s.govde)
		} else if s.adet != 0 && s.adet != N {
			t.Errorf("istek %d: %d satır görüldü (yarım bağlama); 0 ya da %d beklenirdi", i, s.adet, N)
		}
	}
	if bagli != N {
		t.Errorf("devral çağrılarının toplamı %d (tam %d beklenirdi: çift ya da eksik bağlama)", bagli, N)
	}
	if len(dgListe(t, o.r, 301)) != N {
		t.Error("bağlama bitince liste tüm satırları göstermeli")
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE domain_id=301 AND tip='kurtarma' AND eski_domain_id=0 AND silinme IS NULL AND iz_id=''`) != N {
		t.Error("tüm satırlar tam bir kez bağlanmış olmalı")
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE domain_id=0`) != 0 {
		t.Error("yetim kalmamalı")
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backup_destinations WHERE domain_id=301`) != 0 {
		t.Error("eski müşterinin hedefi domaine geri YÜKLENMEMELİ")
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backup_yetim_hedefler`) != 0 {
		t.Error("anlık görüntü (parola) tabloda kalmamalı")
	}
	if say := strings.Count(logs.String(), "yetim yedekler devralındı"); say != 1 {
		t.Errorf("devralma TAM BİR KEZ gerçekleşmeli (çift bağlama), log sayısı=%d\n%s", say, logs.String())
	}
	for _, kotu := range []string{"Deadlock", "deadlock", "Lock wait timeout", "kilit hatası"} {
		if strings.Contains(logs.String(), kotu) {
			t.Logf("not: eşzamanlı bağlamada %q günlüğü (yeniden denemeyle çözüldü): %s", kotu, logs.String())
		}
	}
}

// Devral (bayrak) + elle (admin POST bagla) çağrıları aynı anda: toplam bağlanan = yetim sayısı (ne eksik ne fazla).
func TestDG_B4_DevralVeElleBagla_Eszamanli_ToplamTamAdet(t *testing.T) {
	o := dgYeni(t)
	o.db.SetMaxOpenConns(64)
	yetimUzakSahte(t)
	ctx := context.Background()
	const N = 15
	o.hesap(310, "kosu.com", "c_kosu", 5, "ftp.kosu.invalid", N)
	kSil(t, o.db, 310, "kosu.com", "c_kosu")
	dgDomain(t, o.db, 311, "kosu.com", "c_kosu", 5)

	var toplam int64
	var wg sync.WaitGroup
	basla := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-basla
			atomic.AddInt64(&toplam, devral(ctx, o.db, 311))
		}()
		go func() {
			defer wg.Done()
			<-basla
			kod, g := advIstek(o.r, "POST", "/s/c_kosu/bagla", `{"domain_id":311}`)
			if kod != 200 {
				t.Errorf("elle bağla: %d %s", kod, g)
				return
			}
			var y struct {
				Baglanan int64 `json:"baglanan"`
			}
			_ = json.Unmarshal([]byte(g), &y)
			atomic.AddInt64(&toplam, y.Baglanan)
		}()
	}
	close(basla)
	wg.Wait()
	if toplam != N {
		t.Errorf("🔴 toplam bağlanan %d (beklenen %d): çift ya da eksik bağlama", toplam, N)
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE domain_id=311 AND tip='kurtarma'`) != N || g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE domain_id=0`) != 0 {
		t.Error("son durum: 15 bağlı, 0 yetim")
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backup_destinations WHERE domain_id=311`) != 0 || g113_say(t, o.db, `SELECT COUNT(*) FROM backup_yetim_hedefler`) != 0 {
		t.Error("hedef geri yüklenmemeli, anlık görüntü silinmeli")
	}
}

// ---- hata enjeksiyonlu sürücü: belirli bir deyimin SONUÇ SATIRI okunurken InnoDB kilitlenmesi (1213) gönderir.
// Gerçekte kilitlenme kurbanı olan `SELECT … FOR UPDATE` önce satır akıtmaya başlar; hata `Query`de değil `rows.Next/Err`de görünür.

type dgHataBaglayici struct {
	driver.Connector
	hedef string
}

func (c *dgHataBaglayici) Connect(ctx context.Context) (driver.Conn, error) {
	b, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &dgHataConn{Conn: b, hedef: c.hedef}, nil
}

type dgHataConn struct {
	driver.Conn
	hedef string
}

func (c *dgHataConn) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	var st driver.Stmt
	var err error
	if p, ok := c.Conn.(driver.ConnPrepareContext); ok {
		st, err = p.PrepareContext(ctx, q)
	} else {
		st, err = c.Conn.Prepare(q)
	}
	if err != nil {
		return nil, err
	}
	return &dgHataStmt{Stmt: st, hata: strings.Contains(q, c.hedef)}, nil
}

func (c *dgHataConn) BeginTx(ctx context.Context, o driver.TxOptions) (driver.Tx, error) {
	if b, ok := c.Conn.(driver.ConnBeginTx); ok {
		return b.BeginTx(ctx, o)
	}
	return c.Conn.Begin()
}

type dgHataStmt struct {
	driver.Stmt
	hata bool
}

func (s *dgHataStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	var r driver.Rows
	var err error
	if q, ok := s.Stmt.(driver.StmtQueryContext); ok {
		r, err = q.QueryContext(ctx, args)
	} else {
		dv := make([]driver.Value, len(args))
		for i, a := range args {
			dv[i] = a.Value
		}
		r, err = s.Stmt.Query(dv)
	}
	if err != nil || !s.hata {
		return r, err
	}
	return &dgHataRows{Rows: r}, nil
}

func (s *dgHataStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if e, ok := s.Stmt.(driver.StmtExecContext); ok {
		return e.ExecContext(ctx, args)
	}
	dv := make([]driver.Value, len(args))
	for i, a := range args {
		dv[i] = a.Value
	}
	return s.Stmt.Exec(dv)
}

type dgHataRows struct{ driver.Rows }

func (r *dgHataRows) Next(dest []driver.Value) error {
	return &mysql.MySQLError{Number: 1213, SQLState: [5]byte{'4', '0', '0', '0', '1'}, Message: "Deadlock found when trying to get lock; try restarting transaction"}
}

// BULGU B5 — yetimleriBagla `SELECT … FOR UPDATE` sonuç satırlarını okurken rows.Err()'ü KONTROL ETMEZ. InnoDB kilitlenme kurbanı
// olan deyimin hatası satır akışı sırasında (rows.Next false → rows.Err()) gelir ve TÜM işlemi geri alır; kod hatayı görmeyip
// (eskiler boş) UPDATE'e devam eder: hedef anlık görüntüsü geri YÜKLENMEZ ve SİLİNMEZ, bağlama "başarılı" (log + 200) döner.
// Aynı anda birden çok yetim grubu bağlanırken (bitişik sk'larda aralık kilitleri) gerçekte oluşur (bkz. B6).
func TestDGBulgu_B5_YetimleriBagla_SatirOkumaHatasi_RowsErrKontrolEdilmeli(t *testing.T) {
	o := dgYeni(t)
	yetimUzakSahte(t)
	logYakala(t)
	o.hesap(3100, "rowserr.com", "c_rowserr", 5, "ftp.r.invalid", 3)
	kSil(t, o.db, 3100, "rowserr.com", "c_rowserr")
	dgDomain(t, o.db, 3101, "rowserr.com", "c_rowserr", 5)

	var adi string
	if err := o.db.QueryRow(`SELECT DATABASE()`).Scan(&adi); err != nil {
		t.Fatal(err)
	}
	cfg, err := mysql.ParseDSN(os.Getenv("GOSP_TEST_GECICI_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = adi
	baz, err := mysql.NewConnector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	db2 := sql.OpenDB(&dgHataBaglayici{Connector: baz, hedef: "FOR UPDATE"})
	defer db2.Close()

	n, err := yetimleriBagla(context.Background(), db2, 3101, `sistem_kullanici=? AND alan_adi=? AND reseller_id=?`, "c_rowserr", "rowserr.com", int64(5))
	if err == nil {
		t.Errorf("🔴 SELECT … FOR UPDATE satır okurken 1213 verdi ama yetimleriBagla hata DÖNMEDİ (n=%d)", n)
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE domain_id=3101`) != 0 {
		t.Errorf("🔴 kilitlenme sonrası satırlar yine de bağlandı (atomiklik kayboldu): %d satır", g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE domain_id=3101`))
	}
}

// BULGU B6 (stres; B5'in gerçek hayattaki karşılığı) — farklı domainlerin silme izleri (iz.Yaz → UPDATE … SET domain_id=0) ile
// devralmalar (SELECT … FOR UPDATE + UPDATE; ix_backups_silinen üzerinde BİTİŞİK sk aralık kilitleri) AYNI ANDA koşar.
// InnoDB kilitlenmeleri oluşur (günlükte hiç görünmez, çünkü hata rows.Err()'de kalır): bağlama "başarılı" biter ama hedef anlık
// görüntüsü geri yüklenmez ve silinmez. İz kayıpları olmamalı (iz.Yaz yeniden denemesi bunu korur), bağlamalar eksiksiz olmalı.
func TestDGBulgu_B6_Stres_IzYazmaVeOtoBaglama_AyniAnda_SonundaTutarli(t *testing.T) {
	o := dgYeni(t)
	o.db.SetMaxOpenConns(64)
	yetimUzakSahte(t)
	logYakala(t)
	ctx := context.Background()
	const D, N = 16, 40
	type hesap struct {
		silId, yeniId int64
		sk, alan      string
	}
	var silinecek, baglanacak []hesap
	for i := 0; i < D; i++ {
		sk := fmt.Sprintf("c_sil%d", i) // canlı hosting (N satır + hedef): eşzamanlı silinecek
		o.hesap(2000+int64(i), fmt.Sprintf("sil%d.com", i), sk, 5, "ftp.s.invalid", N)
		silinecek = append(silinecek, hesap{silId: 2000 + int64(i), sk: sk, alan: fmt.Sprintf("sil%d.com", i)})
		bsk := fmt.Sprintf("c_bag%d", i) // önce silinmiş (yetim) + yeniden açılmış hosting: eşzamanlı bağlanacak
		o.hesap(3000+int64(i), fmt.Sprintf("bag%d.com", i), bsk, 5, "ftp.b.invalid", N)
		kSil(t, o.db, 3000+int64(i), fmt.Sprintf("bag%d.com", i), bsk)
		dgDomain(t, o.db, 3500+int64(i), fmt.Sprintf("bag%d.com", i), bsk, 5)
		baglanacak = append(baglanacak, hesap{silId: 3000 + int64(i), yeniId: 3500 + int64(i), sk: bsk, alan: fmt.Sprintf("bag%d.com", i)})
	}
	var wg sync.WaitGroup
	basla := make(chan struct{})
	for _, h := range silinecek {
		wg.Add(1)
		go func(h hesap) {
			defer wg.Done()
			<-basla
			iz := SilinenIzHazirla(ctx, o.db, h.silId, h.alan, h.sk)
			if _, err := o.db.Exec(`DELETE FROM domains WHERE id=?`, h.silId); err != nil {
				t.Errorf("domain silinemedi: %v", err)
			}
			iz.Yaz(ctx, o.db)
		}(h)
	}
	for _, h := range baglanacak {
		wg.Add(1)
		go func(h hesap) {
			defer wg.Done()
			<-basla
			devral(ctx, o.db, h.yeniId)
		}(h)
	}
	close(basla)
	wg.Wait()

	// bayrak yeniden gönderilir (ikinci tur: ilk turda kilitlenme kurbanı olan varsa tamamlanır) + S2 turu
	for _, h := range baglanacak {
		devral(ctx, o.db, h.yeniId)
	}
	SilinenSahipsizIsaretle(ctx, o.db)
	for _, h := range silinecek {
		if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE eski_domain_id=? AND domain_id=0 AND silinme IS NOT NULL`, h.silId) != N {
			t.Errorf("🔴 %s: silme izi eksik/kayıp", h.alan)
		}
		if g113_say(t, o.db, `SELECT COUNT(*) FROM backup_yetim_hedefler WHERE eski_domain_id=?`, h.silId) != 1 {
			t.Errorf("%s: anlık görüntü yazılamadı", h.alan)
		}
	}
	eksikHedef, kalanAnlik := 0, g113_say(t, o.db, `SELECT COUNT(*) FROM backup_yetim_hedefler WHERE eski_domain_id BETWEEN 3000 AND 3099`)
	for _, h := range baglanacak {
		if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE domain_id=? AND tip='kurtarma'`, h.yeniId) != N {
			t.Errorf("🔴 %s: %d satır bağlanmalıydı", h.alan, N)
		}
		if g113_say(t, o.db, `SELECT COUNT(*) FROM backup_destinations WHERE domain_id=?`, h.yeniId) != 0 {
			eksikHedef++
		}
	}
	if eksikHedef > 0 || kalanAnlik > 0 {
		t.Errorf("🔴 eşzamanlı bağlamada %d/%d domaine eski müşterinin hedefi geri yüklendi, %d anlık görüntü (parola) silinmeden kaldı (kilitlenme kurbanı bağlama hatasız 'başarılı' bitti)", eksikHedef, D, kalanAnlik)
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE domain_id=0 AND eski_domain_id BETWEEN 3000 AND 3099`) != 0 {
		t.Error("bağlanan gruplardan yetim kalmamalı")
	}
}

// ================================================================ C — elle bağlama / silme uçları

// Saldırgan sk değerleri (yol kaçışı, büyük harf, NUL, satır sonu, SQL tırnağı, uzun, unicode): 4xx, 2xx/5xx ASLA,
// DB ve diskte hiçbir şey değişmez.
func TestDG_C1_BaglaVeSil_SaldirganSKYollari_4xxVeDBDegismez(t *testing.T) {
	o := dgYeni(t)
	yetimUzakSahte(t)
	dosyalar, _ := o.hesap(400, "c.test", "c_c", 0, "ftp.c.invalid", 2)
	kSil(t, o.db, 400, "c.test", "c_c")
	dgDomain(t, o.db, 401, "c.test", "c_c", 0)
	oncesi := dgDokum(t, o.db, "backups", "backup_destinations", "backup_yetim_hedefler", "audit_log")

	kotu := []string{
		"", "..%2Fetc", "%2e%2e", "%2E%2E%2Fc_c", "c_C", "C_c", "c_c%00", "c_c%0A", "c_c%0D%0A", "c_c%20", "%20c_c",
		"c_c;rm", "c_c%27%20OR%201%3D1--", "c_c%22", "c_%C3%BC", "c_c%2Fx", "c_c%5Cx", "c_c.", "c_c%2e%2e",
		"c_" + strings.Repeat("a", 41), "c_" + strings.Repeat("a", 5000), "c_", "x_c", "%5C", "c_c%E2%80%AE", "c_c*", "c_c%25",
	}
	for _, sk := range kotu {
		for _, c := range []struct{ m, y, b string }{
			{"POST", "/s/" + sk + "/bagla", `{"domain_id":401,"zorla":true}`},
			{"DELETE", "/s/" + sk, ""},
			{"DELETE", "/s/" + sk + "?uzagi_yoksay=1", ""},
		} {
			kod, g := advIstek(o.r, c.m, c.y, c.b)
			if kod < 400 || kod >= 500 {
				t.Errorf("%s %.60s: 4xx beklenirdi, %d %s", c.m, c.y, kod, g)
			}
		}
	}
	if sonrasi := dgDokum(t, o.db, "backups", "backup_destinations", "backup_yetim_hedefler", "audit_log"); sonrasi != oncesi {
		t.Errorf("saldırgan sk istekleri DB'yi değiştirdi\n%s\n---\n%s", oncesi, sonrasi)
	}
	for _, d := range dosyalar {
		if !varMi(filepath.Join(o.kok, "c_c", d)) {
			t.Errorf("dosya silindi: %s", d)
		}
	}
}

// Gövde saldırıları: hiçbiri bağlamaz / denetim yazmaz.
func TestDG_C2_Bagla_GovdeSaldirilari_400VeDBDegismez(t *testing.T) {
	o := dgYeni(t)
	yetimUzakSahte(t)
	o.hesap(400, "c.test", "c_c", 0, "ftp.c.invalid", 2)
	kSil(t, o.db, 400, "c.test", "c_c")
	dgDomain(t, o.db, 401, "c.test", "c_c", 0)
	oncesi := dgDokum(t, o.db, "backups", "backup_destinations", "backup_yetim_hedefler", "audit_log")
	govdeler := []string{
		``, `bozuk`, `null`, `[]`, `"x"`, `{`, `{"domain_id":`,
		`{"domain_id":"401"}`, `{"domain_id":401.5}`, `{"domain_id":1e3}`, `{"domain_id":99999999999999999999}`,
		`{"domain_id":-401}`, `{"domain_id":0}`, `{"zorla":true}`, `{"domain_id":401,"zorla":"true"}`, `{"domain_id":401,"zorla":1}`,
		`{"domain_id":[401]}`, `{"domain_id":{"id":401}}`, `{"domain_id":true}`,
		`{"x":"` + strings.Repeat("a", 6000) + `","domain_id":401}`,
	}
	for _, g := range govdeler {
		kod, yanit := advIstek(o.r, "POST", "/s/c_c/bagla", g)
		if kod != 400 {
			t.Errorf("gövde %.50q: 400 beklenirdi, %d %s", g, kod, yanit)
		}
	}
	if sonrasi := dgDokum(t, o.db, "backups", "backup_destinations", "backup_yetim_hedefler", "audit_log"); sonrasi != oncesi {
		t.Errorf("geçersiz gövdeler DB'yi değiştirdi\n%s\n---\n%s", oncesi, sonrasi)
	}
}

// 409 / 400 / 404 ile reddedilen bağlama TÜM ilgili tabloları (satırlar, hedefler, anlık görüntü, denetim) olduğu gibi bırakır
// ve 409 gövdesi ön yüzün beklediği tüm alanları taşır.
func TestDG_C3_Bagla_Reddedilen_TumTablolarDegismez(t *testing.T) {
	o := dgYeni(t)
	yetimUzakSahte(t)
	o.hesap(410, "a.com", "c_g", 5, "ftp.a.invalid", 3)
	kSil(t, o.db, 410, "a.com", "c_g")
	dgDomain(t, o.db, 411, "b.com", "c_g", 9)   // aynı sk, farklı alan adı + bayi
	dgDomain(t, o.db, 412, "z.com", "c_zzz", 5) // sk uyuşmuyor
	oncesi := dgDokum(t, o.db, "backups", "backup_destinations", "backup_yetim_hedefler", "audit_log")

	for _, govde := range []string{`{"domain_id":411}`, `{"domain_id":411,"zorla":false}`} {
		kod, g := advIstek(o.r, "POST", "/s/c_g/bagla", govde)
		if kod != 409 {
			t.Fatalf("%s: 409 beklenirdi: %d %s", govde, kod, g)
		}
		var y map[string]any
		if err := json.Unmarshal([]byte(g), &y); err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{"ok", "alan_adi_farkli", "hata", "yedek_alan_adi", "hedef_alan_adi", "yedek_reseller_id", "hedef_reseller_id"} {
			if _, var_ := y[k]; !var_ {
				t.Errorf("409 gövdesinde %q yok: %s", k, g)
			}
		}
		if y["ok"] != false || y["alan_adi_farkli"] != true || y["yedek_alan_adi"] != "a.com" || y["hedef_alan_adi"] != "b.com" {
			t.Errorf("409 gövde değerleri: %s", g)
		}
	}
	if kod, _ := advIstek(o.r, "POST", "/s/c_g/bagla", `{"domain_id":412,"zorla":true}`); kod != 400 {
		t.Errorf("sk uyuşmazlığı zorla ile de 400: %d", kod)
	}
	if kod, _ := advIstek(o.r, "POST", "/s/c_g/bagla", `{"domain_id":999999,"zorla":true}`); kod != 404 {
		t.Errorf("olmayan domain 404: %d", kod)
	}
	if sonrasi := dgDokum(t, o.db, "backups", "backup_destinations", "backup_yetim_hedefler", "audit_log"); sonrasi != oncesi {
		t.Errorf("reddedilen bağlamalar DB'yi değiştirdi\n%s\n---\n%s", oncesi, sonrasi)
	}
}

// Yetimi olmayan sk için bağlama: 200 baglanan=0, hiçbir canlı satıra dokunmaz.
func TestDG_C3b_Bagla_YetimYokken_Sifir_CanliSatirlaraDokunmaz(t *testing.T) {
	o := dgYeni(t)
	o.hesap(420, "canli.com", "c_canli", 0, "", 2)
	oncesi := dgDokum(t, o.db, "backups")
	kod, g := advIstek(o.r, "POST", "/s/c_canli/bagla", `{"domain_id":420}`)
	if kod != 200 || !strings.Contains(g, `"baglanan":0`) {
		t.Fatalf("%d %s", kod, g)
	}
	if dgDokum(t, o.db, "backups") != oncesi {
		t.Error("canlı satırlar değişti (tip'i kurtarma yapılmış olabilir)")
	}
}

// Yönetici uçlarının HEPSİ AdminOnly; yedek sayfası uçları MusteriScope; eski yetim-ice-al rotası kalmamış.
func TestDG_C5_Rotalar_AdminOnlyVeMusteriScope(t *testing.T) {
	b, err := os.ReadFile("../../cmd/server/main.go")
	if err != nil {
		t.Fatal(err)
	}
	bulunan := 0
	for i, satir := range strings.Split(string(b), "\n") {
		if strings.Contains(satir, `"/admin/silinen-yedekler`) {
			bulunan++
			if !strings.Contains(satir, "middleware.AdminOnly") {
				t.Errorf("main.go:%d AdminOnly YOK: %s", i+1, strings.TrimSpace(satir))
			}
		}
		if strings.Contains(satir, "backupsH.List") || strings.Contains(satir, "backupsH.Download") || strings.Contains(satir, "backupsH.Restore") ||
			strings.Contains(satir, "backupsH.Icerik") || strings.Contains(satir, "backupsH.Delete") {
			if !strings.Contains(satir, "middleware.MusteriScope") {
				t.Errorf("main.go:%d MusteriScope YOK (domain sahipliği zorlanmıyor): %s", i+1, strings.TrimSpace(satir))
			}
		}
	}
	if bulunan != 6 {
		t.Errorf("silinen-yedekler rota sayısı %d (6 beklenir: liste, ayar GET/PUT, indir, bagla, sil)", bulunan)
	}
	if strings.Contains(string(b), "yetim-ice-al") || strings.Contains(string(b), "YetimIceAl") {
		t.Error("kaldırılan yetim-ice-al rotası hâlâ bağlı")
	}
}
