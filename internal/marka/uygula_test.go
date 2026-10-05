package marka

// uygula_test.go — bayi özel adresi TEK "Uygula" eylemi (göç 0170): sahiplik/IDOR,
// mevcut kaydı ezmeme, TXT şartı, hız sınırı, idempotentlik, yetki, sızıntı.
// Gerçek MariaDB (GOSP_TEST_GECICI_DSN; yoksa ATLANIR); DNS/ACME/bölge yazımı sahte.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

type uyOrtam struct {
	*ktOrtam
	bolgeYaz  []string // "ana:<id>" | "ek:<id>"
	bolgeHata error
	kanca     []string
	arka      int
	genAOk    bool
	txt       []string
	ns        bool
	zaman     time.Time
}

func uyKur(t *testing.T) *uyOrtam {
	t.Helper()
	o := &uyOrtam{ktOrtam: ktKur(t), ns: true, zaman: time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)}
	for _, q := range []string{
		`ALTER TABLE domains ADD COLUMN askida TINYINT NOT NULL DEFAULT 0, ADD COLUMN durum ENUM('aktif','pasif') NOT NULL DEFAULT 'aktif', ADD COLUMN customer_id BIGINT UNSIGNED NULL`,
		`CREATE TABLE dns_records (id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL, ad VARCHAR(253) NOT NULL, tip VARCHAR(16) NOT NULL,
		  deger TEXT NOT NULL, ttl INT NOT NULL DEFAULT 3600, oncelik INT NOT NULL DEFAULT 0, aktif TINYINT NOT NULL DEFAULT 1) ENGINE=InnoDB`,
		`CREATE TABLE ek_alan_adlari (id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL, alan_adi VARCHAR(253) NOT NULL) ENGINE=InnoDB`,
		`CREATE TABLE ek_dns_kayitlari (id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, ek_alan_id BIGINT UNSIGNED NOT NULL, ad VARCHAR(253) NOT NULL, tip VARCHAR(16) NOT NULL,
		  deger TEXT NOT NULL, ttl INT NOT NULL DEFAULT 3600, oncelik INT NOT NULL DEFAULT 0, aktif TINYINT NOT NULL DEFAULT 1) ENGINE=InnoDB`,
		// 101 alfa (bayi 7, kendi hesabı) · 102 beta (bayi 8) · 103 askıda (bayi 7) · 104 müşteri hesabı (bayi 7) · 105 kök · 106 pasif (bayi 7)
		`INSERT INTO domains (id, alan_adi, reseller_id, askida, durum, customer_id) VALUES
		  (101,'alfa.test',7,0,'aktif',NULL),(102,'beta.test',8,0,'aktif',NULL),(103,'askida.test',7,1,'aktif',NULL),
		  (104,'musteri.test',7,0,'aktif',5),(105,'kok.test',0,0,'aktif',NULL),(106,'pasif.test',7,0,'pasif',NULL)`,
		`INSERT INTO ek_alan_adlari (id, domain_id, alan_adi) VALUES (201,101,'ek-alfa.test'),(202,102,'ek-beta.test')`,
	} {
		if _, err := o.db.Exec(q); err != nil {
			t.Fatalf("hazırlık: %v\n%s", err, q)
		}
	}
	ktGoc(t, o.db, "0170_marka_alanadi_uygula.sql")
	ktGoc(t, o.db, "0170_marka_alanadi_uygula.sql") // idempotent
	eski := struct {
		a       func(string, []string) bool
		txt     func(context.Context, string) ([]string, error)
		ns      func(context.Context, string, []string) bool
		zy      func(context.Context, bool, int64) error
		ark     func(func())
		sim     func() time.Time
		kanca   func(string)
		adKanca func(context.Context, string, int64) error
	}{aBuSunucuya, txtCoz, nsBuSunucuya, ZoneYazKanca, arkaCalistir, simdi, AlanDegistiKanca, AdAlaniKanca}
	t.Cleanup(func() {
		aBuSunucuya, txtCoz, nsBuSunucuya, ZoneYazKanca, arkaCalistir, simdi, AlanDegistiKanca, AdAlaniKanca =
			eski.a, eski.txt, eski.ns, eski.zy, eski.ark, eski.sim, eski.kanca, eski.adKanca
		uygulaSayacSifirla()
	})
	uygulaSayacSifirla()
	ilerleDurum.Lock()
	ilerleDurum.calisiyor, ilerleDurum.sonDogrula = map[int64]bool{}, map[int64]time.Time{}
	ilerleDurum.Unlock()
	AdAlaniKanca = nil
	aBuSunucuya = func(string, []string) bool { return o.genAOk }
	txtCoz = func(context.Context, string) ([]string, error) { return o.txt, nil }
	nsBuSunucuya = func(context.Context, string, []string) bool { return o.ns }
	ZoneYazKanca = func(_ context.Context, ek bool, id int64) error {
		if o.bolgeHata != nil {
			return o.bolgeHata
		}
		if ek {
			o.bolgeYaz = append(o.bolgeYaz, fmt.Sprintf("ek:%d", id))
		} else {
			o.bolgeYaz = append(o.bolgeYaz, fmt.Sprintf("ana:%d", id))
		}
		return nil
	}
	arkaCalistir = func(f func()) { o.arka++; f() }
	simdi = func() time.Time { return o.zaman }
	AlanDegistiKanca = func(ad string) { o.kanca = append(o.kanca, ad) }
	for _, rid := range []string{"7", "8"} {
		o.istek("PUT", "/api/v1/marka/bayiler/"+rid, o.admin, map[string]any{"alanadi_izni": true})
	}
	o.kanca = nil
	return o
}

func (o *uyOrtam) uygula(tok, ad string) (int, string) {
	return o.istek("POST", "/api/v1/marka/alan-adi/uygula", tok, map[string]any{"alan_adi": ad})
}

func (o *uyOrtam) satir(rid int64) (ad, durum, adim, kaynak, hata string) {
	_ = o.db.QueryRow(`SELECT alan_adi, durum, adim, dns_kaynak, son_hata_genel FROM cp_marka_alanadi WHERE reseller_id=?`, rid).Scan(&ad, &durum, &adim, &kaynak, &hata)
	return
}

func (o *uyOrtam) kayitSayisi(tablo, sutun string, id int64) int {
	var n int
	_ = o.db.QueryRow("SELECT COUNT(*) FROM "+tablo+" WHERE "+sutun+"=?", id).Scan(&n)
	return n
}

// Yerel sahiplik: bayinin kendi hesabı + NS bu sunucuda → TXT'siz, panel tek A ekler,
// kamu DNS'inde görününce kendiliğinden (döngü) ssl_bekliyor'a geçer.
func TestUygula_YerelSahiplik_AEklenirVeIlerler(t *testing.T) {
	o := uyKur(t)
	o.txt = nil // TXT YOK: yerel kısayolda gerekmez
	k, g := o.uygula(o.bayiA, "Panel.Alfa.Test")
	if k != 202 {
		t.Fatalf("uygula: %d %s", k, g)
	}
	ad, durum, adim, kaynak, _ := o.satir(ktBayiA)
	if ad != "panel.alfa.test" || durum != "dns_bekliyor" || adim != adimDNSEklendi || kaynak != "panel" {
		t.Fatalf("durum: %s %s %s %s", ad, durum, adim, kaynak)
	}
	var tip, deger, rel string
	if err := o.db.QueryRow(`SELECT ad, tip, deger FROM dns_records WHERE domain_id=101`).Scan(&rel, &tip, &deger); err != nil || rel != "panel" || tip != "A" || deger != "203.0.113.10" {
		t.Fatalf("eklenen kayıt: %q %q %q %v", rel, tip, deger, err)
	}
	if strings.Join(o.bolgeYaz, ",") != "ana:101" {
		t.Errorf("bölge bir kez yazılmalı: %v", o.bolgeYaz)
	}
	if n := o.kayitSayisi("dns_records", "domain_id", 101); n != 1 {
		t.Errorf("yalnız TEK kayıt eklenmeli: %d", n)
	}
	// Yayılım: kamu DNS'i kaydı gösterir → döngü kendiliğinden ilerletir (bayi tekrar basmaz).
	o.genAOk = true
	o.zaman = o.zaman.Add(35 * time.Second)
	(&Handlers{DB: o.db, SunucuIPleri: []string{"203.0.113.10"}}).dongusuTek(context.Background())
	if _, durum, _, _, _ := o.satir(ktBayiA); durum != "ssl_bekliyor" {
		t.Fatalf("ssl_bekliyor beklenir: %s", durum)
	}
	if strings.Join(o.kanca, ",") != "panel.alfa.test" {
		t.Errorf("acme döngüsü adla HEMEN dürtülmeli: %v", o.kanca)
	}
	// Eski doğrulama yolu TXT'siz yerel satırı BOZMAZ.
	o.zaman = o.zaman.Add(time.Minute)
	if k, _ := o.istek("POST", "/api/v1/marka/alan-adi/dogrula", o.bayiA, nil); k != 200 {
		t.Errorf("dogrula: %d", k)
	}
	if _, durum, _, _, _ := o.satir(ktBayiA); durum != "ssl_bekliyor" {
		t.Errorf("dogrula yerel satırı geri çevirdi: %s", durum)
	}
}

func TestUygula_ZatenDogruKayit_EklenmezVeIlerler(t *testing.T) {
	o := uyKur(t)
	o.genAOk = true
	o.db.Exec(`INSERT INTO dns_records (domain_id, ad, tip, deger) VALUES (101,'panel','A','203.0.113.10')`)
	if k, g := o.uygula(o.bayiA, "panel.alfa.test"); k != 202 {
		t.Fatalf("%d %s", k, g)
	}
	if n := o.kayitSayisi("dns_records", "domain_id", 101); n != 1 || len(o.bolgeYaz) != 0 {
		t.Errorf("doğru kayıt varken ekleme/bölge yazımı olmamalı: n=%d yaz=%v", n, o.bolgeYaz)
	}
	if _, durum, _, _, _ := o.satir(ktBayiA); durum != "ssl_bekliyor" {
		t.Errorf("ssl_bekliyor beklenir: %s", durum)
	}
}

// Çakışan A/AAAA/CNAME/NS: DOKUNULMAZ, net hata, kayıt aynen kalır.
func TestUygula_CakisanKayit_Ezilmez(t *testing.T) {
	for _, c := range []struct{ tip, deger string }{
		{"A", "198.51.100.7"}, {"AAAA", "2001:db8::1"}, {"CNAME", "baska.example.net."}, {"NS", "ns1.baska.net."},
	} {
		c := c
		t.Run(c.tip, func(t *testing.T) {
			o := uyKur(t)
			o.db.Exec(`INSERT INTO dns_records (domain_id, ad, tip, deger) VALUES (101,'panel',?,?)`, c.tip, c.deger)
			if k, g := o.uygula(o.bayiA, "panel.alfa.test"); k != 202 {
				t.Fatalf("%d %s", k, g)
			}
			if n := o.kayitSayisi("dns_records", "domain_id", 101); n != 1 {
				t.Fatalf("mevcut kayıtlara ekleme/silme yapıldı: %d", n)
			}
			var tip, deger string
			o.db.QueryRow(`SELECT tip, deger FROM dns_records WHERE domain_id=101`).Scan(&tip, &deger)
			if tip != c.tip || deger != c.deger {
				t.Errorf("mevcut kayıt değişti: %s %s", tip, deger)
			}
			if len(o.bolgeYaz) != 0 {
				t.Errorf("çakışmada bölge yazılmamalı: %v", o.bolgeYaz)
			}
			_, durum, adim, _, hata := o.satir(ktBayiA)
			if durum != "dns_bekliyor" || adim != adimDNSCakisma || !strings.Contains(hata, "mevcut kayıt var: "+c.tip+" → ") || !strings.Contains(hata, "kaldırın ya da başka bir ad seçin") {
				t.Errorf("net çakışma hatası beklenir: %s %s %q", durum, adim, hata)
			}
			_, g := o.istek("GET", "/api/v1/marka/alan-adi", o.bayiA, nil)
			if !strings.Contains(g, `"dns_cakisma"`) {
				t.Errorf("okuma ucu adımı göstermeli: %s", g)
			}
		})
	}
}

// IDOR: başka bayinin / kökün bölgesine A kaydı ekletilemez; askıda/pasif reddedilir.
func TestUygula_Sahiplik_IDORVeAskida(t *testing.T) {
	o := uyKur(t)
	for _, ad := range []string{"panel.beta.test", "panel.ek-beta.test", "panel.kok.test", "panel.askida.test", "panel.pasif.test", "alfa.test", "x.alfa.test.localhost"} {
		k, g := o.uygula(o.bayiA, ad)
		if ad == "x.alfa.test.localhost" {
			if k != 400 {
				t.Errorf("%s → 400 beklenir: %d", ad, k)
			}
			continue
		}
		if k != 409 {
			t.Errorf("%s → 409 beklenir: %d %s", ad, k, g)
		}
		for _, sizinti := range []string{"beta", "bayiB", "kok.test", "#8"} {
			if strings.Contains(g, sizinti) {
				t.Errorf("%s yanıtı başka kiracı bilgisi sızdırıyor (%q): %s", ad, sizinti, g)
			}
		}
	}
	var n int
	o.db.QueryRow(`SELECT COUNT(*) FROM dns_records`).Scan(&n)
	var m int
	o.db.QueryRow(`SELECT COUNT(*) FROM cp_marka_alanadi`).Scan(&m)
	if n != 0 || m != 0 || len(o.bolgeYaz) != 0 || o.arka != 0 {
		t.Errorf("reddedilen istekler iz bırakmamalı: kayıt=%d satır=%d yaz=%v arka=%d", n, m, o.bolgeYaz, o.arka)
	}
}

// Ek alan adı bölgesi (bayinin kendi hesabına bağlı): kayıt ek_dns_kayitlari'na, bölge ek olarak yazılır.
func TestUygula_EkAlanBolgesi(t *testing.T) {
	o := uyKur(t)
	if k, g := o.uygula(o.bayiA, "panel.ek-alfa.test"); k != 202 {
		t.Fatalf("%d %s", k, g)
	}
	if n := o.kayitSayisi("ek_dns_kayitlari", "ek_alan_id", 201); n != 1 {
		t.Errorf("ek bölgeye tek A: %d", n)
	}
	if n := o.kayitSayisi("dns_records", "domain_id", 101); n != 0 {
		t.Errorf("ana bölgeye yazılmamalı: %d", n)
	}
	if strings.Join(o.bolgeYaz, ",") != "ek:201" {
		t.Errorf("ek bölge yazılmalı: %v", o.bolgeYaz)
	}
}

// TXT ŞARTI: müşteri hesabına ait bölge ya da NS başka yerde → yerel kısayol YOK:
// A ve TXT görünmeden ssl'e geçilmez; panel kayıt eklemez.
func TestUygula_TXTSarti_Korunur(t *testing.T) {
	t.Run("musteri_hesabi", func(t *testing.T) {
		o := uyKur(t)
		o.genAOk = true // A görünüyor ama TXT yok
		if k, g := o.uygula(o.bayiA, "panel.musteri.test"); k != 202 {
			t.Fatalf("%d %s", k, g)
		}
		_, durum, adim, kaynak, _ := o.satir(ktBayiA)
		if durum != "dns_bekliyor" || kaynak != "harici" || adim != adimDNSBekle {
			t.Errorf("TXT'siz ssl'e geçmemeli: %s %s %s", durum, adim, kaynak)
		}
		if n := o.kayitSayisi("dns_records", "domain_id", 104); n != 0 || len(o.bolgeYaz) != 0 {
			t.Errorf("müşteri bölgesine kayıt eklenmemeli: %d %v", n, o.bolgeYaz)
		}
	})
	t.Run("ns_baska_yerde", func(t *testing.T) {
		o := uyKur(t)
		o.ns = false
		o.genAOk = true // başka biri kendi DNS'inde A'yı bu sunucuya çevirmiş olabilir
		if k, g := o.uygula(o.bayiA, "panel.alfa.test"); k != 202 {
			t.Fatalf("%d %s", k, g)
		}
		_, durum, _, kaynak, _ := o.satir(ktBayiA)
		if durum != "dns_bekliyor" || kaynak != "harici" {
			t.Fatalf("NS başka yerdeyken TXT'siz ssl'e geçmemeli: %s %s", durum, kaynak)
		}
		_, g := o.istek("GET", "/api/v1/marka/alan-adi", o.bayiA, nil)
		if !strings.Contains(g, "ad sunucuları bu sunucuyu göstermiyor") || !strings.Contains(g, `"dns_kayitlari"`) || !strings.Contains(g, `"TXT"`) {
			t.Errorf("NS uyarısı + dış DNS talimatı (A+TXT) beklenir: %s", g)
		}
		// Bayi TXT'yi dış DNS'e girer → döngü kendiliğinden ilerler.
		var jeton string
		o.db.QueryRow(`SELECT dogrulama FROM cp_marka_alanadi WHERE reseller_id=7`).Scan(&jeton)
		o.txt = []string{jeton}
		o.zaman = o.zaman.Add(35 * time.Second)
		(&Handlers{DB: o.db, SunucuIPleri: []string{"203.0.113.10"}}).dongusuTek(context.Background())
		if _, durum, _, _, _ := o.satir(ktBayiA); durum != "ssl_bekliyor" {
			t.Errorf("A+TXT görününce ssl_bekliyor: %s", durum)
		}
	})
	t.Run("kayitsiz_bolge_harici", func(t *testing.T) {
		o := uyKur(t)
		if k, g := o.uygula(o.bayiA, "panel.bayi-disi.example.org"); k != 202 {
			t.Fatalf("%d %s", k, g)
		}
		o.genAOk = true
		o.zaman = o.zaman.Add(35 * time.Second)
		h := &Handlers{DB: o.db, SunucuIPleri: []string{"203.0.113.10"}}
		h.dongusuTek(context.Background())
		_, g := o.istek("GET", "/api/v1/marka/alan-adi", o.bayiA, nil)
		if _, durum, _, _, _ := o.satir(ktBayiA); durum != "dns_bekliyor" || !strings.Contains(g, "TXT kaydı bulunamadı") {
			t.Errorf("yalnız A ile ilerlememeli: %s %s", durum, g)
		}
		// Zaman aşımı: 30 dk sonra yoklama durur, net mesaj.
		o.zaman = o.zaman.Add(31 * time.Minute)
		h.dongusuTek(context.Background())
		_, durum, adim, _, hata := o.satir(ktBayiA)
		if durum != "dns_bekliyor" || adim != adimDNSZaman || !strings.Contains(hata, "30 dakika") {
			t.Errorf("zaman aşımı: %s %s %q", durum, adim, hata)
		}
		// Durduktan sonra döngü ona dokunmaz; "Şimdi kontrol et" yoklamayı yeniden kurar.
		var jeton string
		o.db.QueryRow(`SELECT dogrulama FROM cp_marka_alanadi WHERE reseller_id=7`).Scan(&jeton)
		o.txt = []string{jeton}
		o.zaman = o.zaman.Add(time.Minute)
		if k, _ := o.istek("POST", "/api/v1/marka/alan-adi/dogrula", o.bayiA, nil); k != 200 {
			t.Fatalf("dogrula %d", k)
		}
		if _, durum, _, _, _ := o.satir(ktBayiA); durum != "ssl_bekliyor" {
			t.Errorf("şimdi kontrol et ilerletmeli: %s", durum)
		}
	})
}

// DNS aşaması hatası 'hata' DEĞİL (acme döngüsü 'hata'yı yeniden dener → TXT atlanırdı).
func TestUygula_DNSHatasiHataDurumunaDusmez(t *testing.T) {
	o := uyKur(t)
	o.db.Exec(`INSERT INTO dns_records (domain_id, ad, tip, deger) VALUES (101,'panel','A','198.51.100.7')`)
	o.uygula(o.bayiA, "panel.alfa.test")
	if _, durum, _, _, _ := o.satir(ktBayiA); durum != "dns_bekliyor" {
		t.Errorf("durum dns_bekliyor kalmalı (acme döngüsünün tut kümesine girmemeli): %s", durum)
	}
	o.bolgeHata = fmt.Errorf("named-checkzone ham çıktı /var/named/beta.test.zone")
	o.db.Exec(`DELETE FROM dns_records`)
	o.zaman = o.zaman.Add(time.Minute)
	o.istek("POST", "/api/v1/marka/alan-adi/dogrula", o.bayiA, nil)
	_, g := o.istek("GET", "/api/v1/marka/alan-adi", o.bayiA, nil)
	if strings.Contains(g, "named-checkzone") || strings.Contains(g, "/var/named") {
		t.Errorf("ham bölge hatası bayiye sızdı: %s", g)
	}
	if n := o.kayitSayisi("dns_records", "domain_id", 101); n != 0 {
		t.Errorf("bölge yazılamayınca eklenen satır geri alınmalı: %d", n)
	}
}

// Hız sınırı: bayi başına saatte 3 (4. → 429); saat sonra açılır; başka bayi etkilenmez.
func TestUygula_HizSiniri(t *testing.T) {
	o := uyKur(t)
	o.db.Exec(`INSERT INTO dns_records (domain_id, ad, tip, deger) VALUES (101,'panel','A','198.51.100.7')`) // sürekli çakışma → işleniyor sayılmaz
	for i := 1; i <= 3; i++ {
		if k, g := o.uygula(o.bayiA, "panel.alfa.test"); k != 202 {
			t.Fatalf("%d. deneme: %d %s", i, k, g)
		}
	}
	k, g := o.uygula(o.bayiA, "panel.alfa.test")
	if k != 429 || !strings.Contains(g, "3 kez") {
		t.Fatalf("4. deneme 429 beklenir: %d %s", k, g)
	}
	if k, _ := o.uygula(o.bayiB, "panel.beta.test"); k != 202 {
		t.Errorf("sınır bayi başınadır: B etkilenmemeli (%d)", k)
	}
	o.zaman = o.zaman.Add(61 * time.Minute)
	if k, _ := o.uygula(o.bayiA, "panel.alfa.test"); k != 202 {
		t.Errorf("1 saat sonra yeniden açılmalı: %d", k)
	}
}

// İdempotent: işlenirken tekrar basmak çift iş yapmaz, sayaca girmez; aktifse no-op.
func TestUygula_Idempotent(t *testing.T) {
	o := uyKur(t)
	if k, _ := o.uygula(o.bayiA, "panel.bayi-disi.example.org"); k != 202 {
		t.Fatalf("ilk: %d", k)
	}
	for i := 0; i < 6; i++ {
		k, g := o.uygula(o.bayiA, "panel.bayi-disi.example.org")
		if k != 200 || !strings.Contains(g, `"zaten_isleniyor":true`) {
			t.Fatalf("tekrar %d: zaten işleniyor beklenir: %d %s", i, k, g)
		}
	}
	if o.arka != 1 {
		t.Errorf("arka iş yalnız bir kez başlamalı: %d", o.arka)
	}
	// Boş gövde mevcut adı sürdürür (idempotent).
	if k, g := o.istek("POST", "/api/v1/marka/alan-adi/uygula", o.bayiA, nil); k != 200 || !strings.Contains(g, "zaten_isleniyor") {
		t.Errorf("gövdesiz: %d %s", k, g)
	}
	o.db.Exec(`UPDATE cp_marka_alanadi SET durum='aktif'`)
	if k, g := o.uygula(o.bayiA, "panel.bayi-disi.example.org"); k != 200 || !strings.Contains(g, `"zaten_aktif":true`) || o.arka != 1 {
		t.Errorf("aktif: %d %s arka=%d", k, g, o.arka)
	}
	// Aktifken başka ad → önce kaldırılmalı; bekleyen ad ise yenisiyle değişir.
	if k, _ := o.uygula(o.bayiA, "panel.alfa.test"); k != 409 {
		t.Errorf("aktifken başka ad 409: %d", k)
	}
	o.db.Exec(`UPDATE cp_marka_alanadi SET durum='dns_bekliyor'`)
	if k, _ := o.uygula(o.bayiA, "panel.alfa.test"); k != 202 {
		t.Errorf("bekleyen ad yenisiyle değişmeli: %d", k)
	}
	if ad, _, _, _, _ := o.satir(ktBayiA); ad != "panel.alfa.test" {
		t.Errorf("ad değişmedi: %s", ad)
	}
}

// Yetki: müşteri/kök 403; izinsiz bayi mevcut metinle 403 (backend davranışı aynen).
func TestUygula_Yetki(t *testing.T) {
	o := uyKur(t)
	if k, _ := o.uygula(o.musA, "panel.alfa.test"); k != 403 {
		t.Errorf("müşteri 403: %d", k)
	}
	if k, _ := o.uygula(o.admin, "panel.alfa.test"); k != 403 {
		t.Errorf("kök (bayi değil) 403: %d", k)
	}
	if k, _ := o.uygula("", "panel.alfa.test"); k != 401 {
		t.Errorf("kimliksiz 401: %d", k)
	}
	o.istek("PUT", "/api/v1/marka/bayiler/7", o.admin, map[string]any{"alanadi_izni": false})
	k, g := o.uygula(o.bayiA, "panel.alfa.test")
	if k != 403 || !strings.Contains(g, "Özel panel adresi bu hesap için açık değil. Hizmet sağlayıcınızla iletişime geçin.") {
		t.Errorf("izinsiz bayi: %d %s", k, g)
	}
	if k, _ := o.istek("GET", "/api/v1/marka/alan-adi", o.bayiA, nil); k != 403 {
		t.Errorf("izinsiz GET 403: %d", k)
	}
	// İzin kapalıyken döngü dokunmaz.
	var m int
	o.db.QueryRow(`SELECT COUNT(*) FROM cp_marka_alanadi`).Scan(&m)
	if m != 0 {
		t.Errorf("izinsiz kayıt oluştu: %d", m)
	}
}

// Sızıntı: bayi B'nin adı Uygula çakışmasında kimliksiz/başka bayiye genel metinle döner.
func TestUygula_CakismaGenelMetin(t *testing.T) {
	o := uyKur(t)
	if k, _ := o.uygula(o.bayiB, "panel.ortak.example.org"); k != 202 {
		t.Fatal("B uygula")
	}
	k, g := o.uygula(o.bayiA, "panel.ortak.example.org")
	if k != 409 || strings.Contains(g, "bayiB") || strings.Contains(g, `"8"`) || !strings.Contains(g, "Bu adres kullanılamıyor.") {
		t.Errorf("genel 409 beklenir: %d %s", k, g)
	}
	if _, g := o.istek("GET", "/api/v1/marka/alan-adi", o.bayiA, nil); strings.Contains(g, "ortak") {
		t.Errorf("A görünümünde B'nin adı: %s", g)
	}
	// Ad alanı kancası (başka hesabın adı/alt alanı): hata metni kancadan, ham değil.
	AdAlaniKanca = func(context.Context, string, int64) error { return fmt.Errorf("bu ad başka bir hesaba ait") }
	if k, g := o.uygula(o.bayiA, "panel.x.example.org"); k != 409 || !strings.Contains(g, "başka bir hesaba ait") {
		t.Errorf("ad alanı kancası uygulanmalı: %d %s", k, g)
	}
}

// Müşteri giriş bağlantısı için: yalnız AKTİF özel adres yönetim görünümünde ozel_adres olur.
func TestYonet_OzelAdresYalnizAktif(t *testing.T) {
	o := uyKur(t)
	o.uygula(o.bayiA, "panel.bayi-disi.example.org")
	_, g := o.istek("GET", "/api/v1/marka/yonet", o.bayiA, nil)
	var v map[string]any
	_ = json.Unmarshal([]byte(g), &v)
	if _, var_ := v["ozel_adres"]; var_ {
		t.Errorf("aktif değilken ozel_adres olmamalı: %s", g)
	}
	o.db.Exec(`UPDATE cp_marka_alanadi SET durum='aktif'`)
	_, g = o.istek("GET", "/api/v1/marka/yonet", o.bayiA, nil)
	_ = json.Unmarshal([]byte(g), &v)
	if v["ozel_adres"] != "panel.bayi-disi.example.org" {
		t.Errorf("aktifken ozel_adres beklenir: %s", g)
	}
	// Kaydetme yanıtı da taşır (arayüz yanıtı görünüme yazar).
	_, g = o.istek("PUT", "/api/v1/marka/yonet", o.bayiA, map[string]any{"panel_adi": "Alfa"})
	if !strings.Contains(g, `"ozel_adres":"panel.bayi-disi.example.org"`) {
		t.Errorf("PUT yanıtı ozel_adres taşımalı: %s", g)
	}
	// B bayisi A'nın adresini görmez.
	_, g = o.istek("GET", "/api/v1/marka/yonet", o.bayiB, nil)
	if strings.Contains(g, "bayi-disi") {
		t.Errorf("B görünümünde A'nın adresi: %s", g)
	}
}

func TestAdimlariTuret_Tablosu(t *testing.T) {
	cases := []struct{ durum, adim, beklenen string }{
		{"dns_bekliyor", adimDogrulama, "suruyor,bekliyor,bekliyor,bekliyor"},
		{"dns_bekliyor", adimDNSBekle, "tamam,bekliyor,bekliyor,bekliyor"},
		{"dns_bekliyor", adimDNSEklendi, "tamam,suruyor,bekliyor,bekliyor"},
		{"dns_bekliyor", adimDNSCakisma, "tamam,hata,bekliyor,bekliyor"},
		{"ssl_bekliyor", adimSSL, "tamam,tamam,suruyor,bekliyor"},
		{"hata", adimSSL, "tamam,tamam,hata,bekliyor"},
		{"aktif", adimSSL, "tamam,tamam,tamam,tamam"},
	}
	for _, c := range cases {
		var d []string
		for _, s := range adimlariTuret(c.durum, c.adim) {
			d = append(d, s.Durum)
		}
		if strings.Join(d, ",") != c.beklenen {
			t.Errorf("%s/%s: %v beklenen %s", c.durum, c.adim, d, c.beklenen)
		}
	}
}
