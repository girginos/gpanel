package dbgovernor

// db_test.go — DB'li testler (GOSP_TEST_GECICI_DSN yoksa ATLANIR): gerçek göç dosyası
// (migrations/0220) + SQLDepo + API + bildirim/denetim yazıcıları + tam yığın (Motor + SQLDepo).
// USER_STATISTICS / mysql.user tarafı sahte kaynaktan (sahteOlcer/sahteYon) gelir; canlı MariaDB
// hesaplarına DOKUNULMAZ.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/auth"
	"girginospanel/internal/efektiflimit"
)

const gocDosyasi = "../../migrations/0220_db_cpu_kisici.sql"

func gocUygula(t *testing.T, db *sql.DB) {
	t.Helper()
	b, err := os.ReadFile(gocDosyasi)
	if err != nil {
		t.Fatalf("göç okunamadı: %v", err)
	}
	var temiz []string
	for _, ln := range strings.Split(string(b), "\n") {
		if s := strings.TrimSpace(ln); s == "" || strings.HasPrefix(s, "--") {
			continue
		}
		temiz = append(temiz, ln)
	}
	for _, q := range strings.Split(strings.Join(temiz, "\n"), ";") {
		if q = strings.TrimSpace(q); q == "" {
			continue
		}
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("GÖÇ HATASI: %v\n%s", err, q)
		}
	}
}

func geciciDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li test atlandı")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = ""
	kok, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	ad := fmt.Sprintf("gosp_test_dbgov_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad); _ = kok.Close() })
	cfg.DBName = ad
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var sp []string
	for _, a := range efektiflimit.Alanlar {
		sp = append(sp, "`"+a.Sutun+"` INT NOT NULL DEFAULT 0")
	}
	for _, q := range []string{
		"CREATE TABLE service_plans (id BIGINT UNSIGNED PRIMARY KEY, ad VARCHAR(64) NOT NULL DEFAULT '', reseller_id BIGINT NOT NULL DEFAULT 0, domain_id BIGINT UNSIGNED NULL, " + strings.Join(sp, ", ") + ")",
		"CREATE TABLE domains (id BIGINT UNSIGNED PRIMARY KEY, alan_adi VARCHAR(253) NOT NULL, sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '', reseller_id BIGINT NOT NULL DEFAULT 0, plan_id BIGINT UNSIGNED NULL, askida TINYINT(1) NOT NULL DEFAULT 0, disk_kota_mb INT NOT NULL DEFAULT 0, inode_kota INT NOT NULL DEFAULT 0)",
		"CREATE TABLE db_accounts (id BIGINT AUTO_INCREMENT PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL, db_name VARCHAR(64) NOT NULL UNIQUE, db_user VARCHAR(64) NOT NULL)",
		efektiflimit.TabloDDL,
		"CREATE TABLE users (id BIGINT UNSIGNED PRIMARY KEY, username VARCHAR(64) NOT NULL DEFAULT '', role VARCHAR(16) NOT NULL DEFAULT 'reseller')",
		"CREATE TABLE cp_ayarlar (anahtar VARCHAR(64) NOT NULL PRIMARY KEY, deger TEXT NOT NULL, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP)",
		`CREATE TABLE audit_log (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, ts TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			actor_user_id BIGINT UNSIGNED NULL, actor_username VARCHAR(64) NOT NULL DEFAULT '', ip VARCHAR(64) NOT NULL DEFAULT '',
			action VARCHAR(64) NOT NULL, target VARCHAR(255) NOT NULL DEFAULT '', detail LONGTEXT NULL, ok TINYINT(1) NOT NULL DEFAULT 1,
			reseller_id BIGINT UNSIGNED NOT NULL DEFAULT 0)`,
		`CREATE TABLE cp_bildirim (id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
			seviye VARCHAR(16) NOT NULL DEFAULT 'bilgi', kategori VARCHAR(32) NOT NULL DEFAULT '',
			baslik VARCHAR(200) NOT NULL, mesaj TEXT NOT NULL, domain_id BIGINT UNSIGNED NULL,
			reseller_id BIGINT UNSIGNED NULL, ref_tur VARCHAR(24) NOT NULL DEFAULT '', ref_id BIGINT NOT NULL DEFAULT 0,
			okundu TINYINT(1) NOT NULL DEFAULT 0, created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE cp_bildirim_okundu (bildirim_id BIGINT UNSIGNED NOT NULL, okuyan_tur VARCHAR(8) NOT NULL,
			okuyan_id BIGINT UNSIGNED NOT NULL, PRIMARY KEY (bildirim_id, okuyan_tur, okuyan_id))`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("şema: %v\n%s", err, q)
		}
	}
	gocUygula(t, db)
	return db
}

func dbExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%v\n%s", err, q)
	}
}

func sayim(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%v\n%s", err, q)
	}
	return n
}

// Göç idempotent; "bir kullanıcının en çok BİR açık kısıtı" UNIQUE ile garanti.
func TestGoc0220(t *testing.T) {
	db := geciciDB(t)
	gocUygula(t, db) // ikinci uygulama hata vermemeli
	dbExec(t, db, `INSERT INTO db_cpu_kisit (db_user, baslangic_ts, son_asim_ts, kisit_baglanti) VALUES ('u1', 1, 1, 2)`)
	if _, err := db.Exec(`INSERT INTO db_cpu_kisit (db_user, baslangic_ts, son_asim_ts, kisit_baglanti) VALUES ('u1', 2, 2, 2)`); err == nil {
		t.Fatal("aynı kullanıcıda ikinci AÇIK kısıt reddedilmeli")
	}
	dbExec(t, db, `UPDATE db_cpu_kisit SET acik=NULL, bitis_ts=5, bitis_neden='soguma' WHERE db_user='u1'`)
	dbExec(t, db, `INSERT INTO db_cpu_kisit (db_user, baslangic_ts, son_asim_ts, kisit_baglanti) VALUES ('u1', 9, 9, 2)`)
	// harf DUYARLI: MariaDB hesap adları harf duyarlıdır
	dbExec(t, db, `INSERT INTO db_cpu_kisit (db_user, baslangic_ts, son_asim_ts, kisit_baglanti) VALUES ('U1', 9, 9, 2)`)
	dbExec(t, db, `INSERT INTO db_cpu_olcum (dilim, db_user, domain_id, cpu_ms) VALUES (300,'u1',1,5),(300,'U1',1,7)`)
	if n := sayim(t, db, `SELECT COUNT(*) FROM db_cpu_olcum`); n != 2 {
		t.Fatalf("olcum PK harf duyarlı olmalı: %d satır", n)
	}
}

func TestSQLDepo_Esle(t *testing.T) {
	db := geciciDB(t)
	d := &SQLDepo{DB: db}
	dbExec(t, db, `INSERT INTO domains (id, alan_adi, reseller_id, askida) VALUES (1,'a.test',7,0),(2,'b.test',0,1),(3,'c.test',9,0)`)
	dbExec(t, db, `INSERT INTO db_accounts (domain_id, db_name, db_user) VALUES
		(1,'a_main','a_db'),(1,'a_wp','a_db'),(2,'b_main','b_db'),
		(1,'ortak1','ortak_db'),(2,'ortak2','ortak_db'),
		(3,'c_x','K_X_DB'),(1,'c_y','k_x_db')`)
	m, err := d.Esle(context.Background(), []string{"a_db", "b_db", "ortak_db", "yok_db", "K_X_DB", "k_x_db"})
	if err != nil {
		t.Fatal(err)
	}
	if g := m["a_db"]; g.Yok || g.DomainID != 1 || g.ResellerID != 7 || g.AlanAdi != "a.test" || g.Askida {
		t.Errorf("a_db (iki DB, tek hosting): %+v", g)
	}
	if g := m["b_db"]; g.Yok || !g.Askida || g.DomainID != 2 {
		t.Errorf("b_db askıda: %+v", g)
	}
	if !m["ortak_db"].Yok {
		t.Errorf("iki hostingte kayıtlı kullanıcının sahibi belirsiz → Yok: %+v", m["ortak_db"])
	}
	if !m["yok_db"].Yok {
		t.Errorf("kayıtsız → Yok")
	}
	// harf duyarlı: K_X_DB → domain 3 (bayi 9), k_x_db → domain 1 (bayi 7); birbirinin planını/sahibini almaz
	if m["K_X_DB"].DomainID != 3 || m["K_X_DB"].ResellerID != 9 || m["k_x_db"].DomainID != 1 || m["k_x_db"].ResellerID != 7 {
		t.Errorf("harf varyantları karıştı: %+v / %+v", m["K_X_DB"], m["k_x_db"])
	}
	ku, err := d.DomainKullanicilari(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(ku, ",")
	if !strings.Contains(got, "a_db") || !strings.Contains(got, "k_x_db") || strings.Contains(got, "ortak_db") || strings.Contains(got, "K_X_DB") {
		t.Errorf("hostingin YALNIZ kendi kullanıcıları dönmeli (ortak/başka hosting hariç): %v", ku)
	}
}

func TestSQLDepo_EsikVePlan(t *testing.T) {
	db := geciciDB(t)
	d := &SQLDepo{DB: db}
	ctx := context.Background()
	dbExec(t, db, `INSERT INTO service_plans (id, cpu_yuzde, ram_mb, max_process, mysql_max_baglanti) VALUES (1, 250, 2048, 60, 15), (2, 30, 512, 20, 0)`)
	dbExec(t, db, `INSERT INTO domains (id, alan_adi, plan_id) VALUES (1,'a.test',1),(2,'b.test',2),(3,'c.test',NULL),(4,'d.test',NULL)`)
	dbExec(t, db, `INSERT INTO hosting_limit_ust (domain_id, cpu_yuzde, ram_mb) VALUES (4, 400, 1024)`)
	for _, c := range []struct {
		id   int64
		esik float64
	}{{1, 2.5}, {2, MinEsik}, {3, 1.0}, {4, 4.0}} {
		got, err := d.HostingEsik(ctx, c.id, 1.0)
		if err != nil || got != c.esik {
			t.Errorf("HostingEsik(%d) = %v, %v; beklenen %v", c.id, got, err, c.esik)
		}
	}
	if _, err := d.HostingEsik(ctx, 999, 1.0); err == nil {
		t.Error("var olmayan hosting hata dönmeli (çağıran o hostinge kısıt koymaz)")
	}
	for _, c := range []struct {
		id     int64
		n      int
		planli bool
	}{{1, 15, true}, {2, 0, true}, {3, 0, false}, {4, 0, true}} {
		n, planli, err := d.PlanBaglanti(ctx, c.id)
		if err != nil || n != c.n || planli != c.planli {
			t.Errorf("PlanBaglanti(%d) = %d,%v,%v; beklenen %d,%v", c.id, n, planli, err, c.n, c.planli)
		}
	}
}

func TestSQLDepo_BayiCPU(t *testing.T) {
	db := geciciDB(t)
	d := &SQLDepo{DB: db}
	ctx := context.Background()
	dbExec(t, db, `INSERT INTO users (id, username, role) VALUES (7,'bayi7','reseller'),(8,'bayi8','reseller'),(9,'kok','admin')`)
	if _, ok, err := d.BayiCPU(ctx, 7); ok || err != nil {
		t.Fatalf("cpu_cekirdek sütunu yokken bayi sınırsız: %v %v", ok, err)
	}
	dbExec(t, db, `ALTER TABLE users ADD COLUMN cpu_cekirdek DECIMAL(6,2) NULL`)
	d.bayiSutunTS = time.Time{} // önbelleği atla
	dbExec(t, db, `UPDATE users SET cpu_cekirdek=2.5 WHERE id=7`)
	dbExec(t, db, `UPDATE users SET cpu_cekirdek=0 WHERE id=8`)
	dbExec(t, db, `UPDATE users SET cpu_cekirdek=4 WHERE id=9`)
	if v, ok, err := d.BayiCPU(ctx, 7); !ok || err != nil || v != 2.5 {
		t.Errorf("bayi 7: %v %v %v", v, ok, err)
	}
	if _, ok, _ := d.BayiCPU(ctx, 8); ok {
		t.Error("0 = sınırsız")
	}
	if _, ok, _ := d.BayiCPU(ctx, 9); ok {
		t.Error("yönetici bayi değildir")
	}
	if _, ok, _ := d.BayiCPU(ctx, 99); ok {
		t.Error("olmayan bayi sınırsız")
	}
}

func TestSQLDepo_KisitKaydiVeOlcum(t *testing.T) {
	db := geciciDB(t)
	d := &SQLDepo{DB: db}
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0)
	k := &Kisit{Kullanici: "a_db", Kimlik: Kimlik{DomainID: 1, ResellerID: 7, AlanAdi: "a.test"},
		Birimler: []BirimRef{{Tur: "hosting", ID: 1, Esik: 1.5}}, Seviye: 1, Onceki: 5, KisitDeger: 2, Tepe: 2.25, Uygulandi: true, Baslangic: now, SonAsim: now}
	id, err := d.Ac(ctx, k)
	if err != nil || id == 0 {
		t.Fatalf("Ac: %v", err)
	}
	k.KayitID = id
	if _, err := d.Ac(ctx, k); err == nil {
		t.Fatal("aynı kullanıcıda ikinci açık kayıt reddedilmeli")
	}
	k.Seviye, k.Oldurulen, k.Kesilen, k.SonAsim = 3, 3, 7, now.Add(time.Minute)
	k.birimEkle(BirimRef{Tur: "bayi", ID: 7, Esik: 1.0})
	if err := d.Guncelle(ctx, k); err != nil {
		t.Fatal(err)
	}
	acik, err := d.AcikKayitlar(ctx)
	if err != nil || len(acik) != 1 {
		t.Fatalf("AcikKayitlar: %v %v", acik, err)
	}
	g := acik[0]
	if g.Kullanici != "a_db" || g.KayitID != id || g.Seviye != 3 || g.Kesilen != 7 || g.Onceki != 5 || g.KisitDeger != 2 || g.Oldurulen != 3 || !g.Uygulandi ||
		g.Kimlik.DomainID != 1 || g.Kimlik.ResellerID != 7 || g.Tepe != 2.25 || g.SonAsim.Unix() != now.Add(time.Minute).Unix() ||
		g.nedenMetni() != "hosting+bayi" || len(g.Birimler) != 2 {
		t.Fatalf("kayıt gidiş-dönüş bozuk: %+v", g)
	}
	if err := d.Kapat(ctx, id, now.Add(time.Hour).Unix(), "soguma"); err != nil {
		t.Fatal(err)
	}
	if acik, _ = d.AcikKayitlar(ctx); len(acik) != 0 {
		t.Fatal("kapanan kayıt açık listede")
	}
	if err := d.Kapat(ctx, id, 1, "acilis"); err != nil || sayim(t, db, `SELECT COUNT(*) FROM db_cpu_kisit WHERE bitis_neden='acilis'`) != 0 {
		t.Fatal("kapanmış kayıt yeniden kapatılmamalı")
	}
	k2 := *k
	k2.KayitID = 0
	if _, err := d.Ac(ctx, &k2); err != nil {
		t.Fatalf("kapanan kullanıcıya yeniden kısıt açılabilmeli: %v", err)
	}

	// ölçüm: aynı dilimde birikir (upsert); Temizle eskileri siler
	dbExec(t, db, `INSERT INTO db_cpu_olcum (dilim, db_user, domain_id, cpu_ms) VALUES (?, 'eski', 1, 5)`, now.Add(-3*24*time.Hour).Unix())
	for i := 0; i < 2; i++ {
		if err := d.OlcumYaz(ctx, 1_800_000_000/300*300, []OlcumSatiri{{"a_db", 1, 1500}, {"b_db", 2, 250}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.OlcumYaz(ctx, 1, nil); err != nil {
		t.Fatal(err)
	}
	if n := sayim(t, db, `SELECT cpu_ms FROM db_cpu_olcum WHERE db_user='a_db'`); n != 3000 {
		t.Fatalf("aynı dilim birikmeli: %d", n)
	}
	dbExec(t, db, `INSERT INTO db_cpu_kisit (db_user, baslangic_ts, son_asim_ts, kisit_baglanti, acik, bitis_ts) VALUES ('cok_eski', 1, 1, 2, NULL, ?)`, now.Add(-100*24*time.Hour).Unix())
	if err := d.Temizle(ctx, now); err != nil {
		t.Fatal(err)
	}
	if sayim(t, db, `SELECT COUNT(*) FROM db_cpu_olcum WHERE db_user='eski'`) != 0 || sayim(t, db, `SELECT COUNT(*) FROM db_cpu_olcum`) != 2 {
		t.Fatal("2 günden eski ölçüm silinmeli, yenisi kalmalı")
	}
	if sayim(t, db, `SELECT COUNT(*) FROM db_cpu_kisit WHERE db_user='cok_eski'`) != 0 || sayim(t, db, `SELECT COUNT(*) FROM db_cpu_kisit WHERE acik=1`) != 1 {
		t.Fatal("90 günden eski KAPALI kayıt silinmeli; açık kayıt ASLA")
	}
}

func TestAyarOku(t *testing.T) {
	db := geciciDB(t)
	a, err := ayarOku(context.Background(), db)
	if err != nil || a != VarsayilanAyar() {
		t.Fatalf("satır yokken varsayılan: %+v %v", a, err)
	}
	dbExec(t, db, `INSERT INTO cp_ayarlar (anahtar, deger) VALUES ('db_kisici_mod','gozlem'),('db_kisici_esik','3'),('baska_ayar','x'),('dbXkisici_esik','9')`)
	a, err = ayarOku(context.Background(), db)
	if err != nil || a.Mod != ModGozlem || a.EsikHosting != 3 {
		t.Fatalf("ayar okunmadı (LIKE joker kaçışı dahil): %+v %v", a, err)
	}
}

// Tam yığın: Motor + SQLDepo + gerçek bildirim/denetim yazıcıları; sahte MariaDB.
func TestTamYigin(t *testing.T) {
	db := geciciDB(t)
	dbExec(t, db, `INSERT INTO users (id, username, role) VALUES (7,'bayi7','reseller')`)
	dbExec(t, db, `INSERT INTO domains (id, alan_adi, reseller_id) VALUES (1,'a.test',7)`)
	dbExec(t, db, `INSERT INTO db_accounts (domain_id, db_name, db_user) VALUES (1,'a_main','a_db'),(1,'a_wp','a_wpu')`)

	o := yeniOrtam(t)
	o.hosting(1, 7, "a.test", "a_db", "a_wpu")
	o.m.DB = db
	o.m.Depo = &SQLDepo{DB: db}
	o.m.AyarOku = func(ctx context.Context) (Ayar, error) { return ayarOku(ctx, db) }
	dbExec(t, db, `INSERT INTO cp_ayarlar (anahtar, deger) VALUES ('db_kisici_mod','uygula'),('db_kisici_poll_sn','15'),('db_kisici_pencere_sn','60')`)

	o.taban()
	o.calis(4, hizlar("a_db", 2.0, "a_wpu", 1.5)) // iki büyük pay: ikisi de kısılır (boşta kardeş kısılmaz: yayilma_test.go)
	if len(o.m.AktifKullanicilar()) != 2 {
		t.Fatalf("hostingin iki kullanıcısı kısılmalı: %v (hata: %s)", o.m.AktifKullanicilar(), o.m.DurumAl().SonHata)
	}
	if n := sayim(t, db, `SELECT COUNT(*) FROM db_cpu_kisit WHERE acik=1 AND reseller_id=7 AND domain_id=1 AND uygulandi=1 AND onceki_baglanti=0 AND kisit_baglanti=2`); n != 2 {
		t.Fatalf("db_cpu_kisit: %d açık satır", n)
	}
	if n := sayim(t, db, `SELECT COUNT(*) FROM db_cpu_olcum WHERE domain_id=1 AND cpu_ms>0`); n == 0 {
		t.Fatal("ölçüm dilimi yazılmadı")
	}
	// bildirim: kategori 'kota', hosting sahibi bayi 7'ye (müşteriye değil)
	if n := sayim(t, db, `SELECT COUNT(*) FROM cp_bildirim WHERE kategori='kota' AND baslik='Veritabanı CPU kısıtı uygulandı' AND domain_id=1 AND reseller_id=7`); n != 1 {
		t.Fatalf("bildirim: %d", n)
	}
	// denetim: sistem aktörü, kapsam bayi 7 (kök görür)
	var aktor, hedef, detay string
	var kapsam int
	if err := db.QueryRow(`SELECT actor_username, target, detail, reseller_id FROM audit_log WHERE action='db.cpu_kisit'`).Scan(&aktor, &hedef, &detay, &kapsam); err != nil {
		t.Fatalf("denetim kaydı yok: %v", err)
	}
	if aktor != "sistem" || hedef != "a.test" || kapsam != 7 || !strings.Contains(detay, "a_db") {
		t.Fatalf("denetim: %s %s %d %s", aktor, hedef, kapsam, detay)
	}

	// YENİDEN BAŞLAMA: yeni Motor (bellek yok) açık kayıtları bulur ve geri alır
	o2 := yeniOrtam(t)
	o2.m.DB, o2.m.Depo = db, &SQLDepo{DB: db}
	o2.m.Yon = o.yon // MariaDB'de kısıt durumu aynı
	if got := o.yon.baglanti("a_db"); !esitInts(got, []int{2, 2}) {
		t.Fatalf("ön koşul: kısıt MariaDB'de: %v", got)
	}
	o2.m.Kurtar(context.Background())
	if got := o.yon.baglanti("a_db"); !esitInts(got, []int{0, 0}) {
		t.Fatalf("açılış kurtarma önceki değeri yazmalı: %v", got)
	}
	if sayim(t, db, `SELECT COUNT(*) FROM db_cpu_kisit WHERE acik=1`) != 0 || sayim(t, db, `SELECT COUNT(*) FROM db_cpu_kisit WHERE bitis_neden='acilis'`) != 2 {
		t.Fatal("kayıtlar 'acilis' ile kapanmalı")
	}
}

func istek(t *testing.T, h http.HandlerFunc, yol string, c *auth.Claims) (int, map[string]any) {
	t.Helper()
	claimsAl = func(*http.Request) *auth.Claims { return c }
	t.Cleanup(func() { claimsAl = nilClaimsAl })
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest("GET", yol, nil))
	var m map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	return rec.Code, m
}

var nilClaimsAl = claimsAl

// API görünürlüğü: kök hepsi, bayi YALNIZ kendi hostingleri; müşteri/kimliksiz yasak; durum yalnız kök.
func TestAPIGorunurluk(t *testing.T) {
	db := geciciDB(t)
	now := time.Unix(1_800_000_000, 0)
	dbExec(t, db, `INSERT INTO domains (id, alan_adi, reseller_id) VALUES (1,'a.test',7),(2,'b.test',8),(3,'kok.test',0)`)
	dbExec(t, db, `INSERT INTO db_accounts (domain_id, db_name, db_user) VALUES (1,'a_main','a_db'),(1,'a_wp','a_db'),(2,'b_main','b_db'),(3,'k_main','k_db')`)
	d := &SQLDepo{DB: db}
	dilim := now.Unix() / 300 * 300
	if err := d.OlcumYaz(context.Background(), dilim-600, []OlcumSatiri{{"a_db", 1, 30000}, {"b_db", 2, 90000}, {"k_db", 3, 60000}}); err != nil {
		t.Fatal(err)
	}
	if err := d.OlcumYaz(context.Background(), dilim-3*3600, []OlcumSatiri{{"a_db", 1, 10000}}); err != nil { // 1 saatten eski, 24 saat içinde
		t.Fatal(err)
	}
	if _, err := d.Ac(context.Background(), &Kisit{Kullanici: "b_db", Kimlik: Kimlik{DomainID: 2, ResellerID: 8, AlanAdi: "b.test"}, Birimler: []BirimRef{{Tur: "hosting", ID: 2, Esik: 1}},
		Seviye: 1, KisitDeger: 2, Uygulandi: true, Baslangic: now, SonAsim: now}); err != nil {
		t.Fatal(err)
	}
	h := &Handlers{DB: db, Simdi: func() time.Time { return now }}
	kok := &auth.Claims{UserID: 1, Role: "admin"}
	bayi7 := &auth.Claims{UserID: 7, Role: "reseller", ResellerID: 7}
	bayi8 := &auth.Claims{UserID: 8, Role: "reseller", ResellerID: 8}

	liste := func(m map[string]any) []map[string]any {
		var out []map[string]any
		for _, x := range m["liste"].([]any) {
			out = append(out, x.(map[string]any))
		}
		return out
	}
	// kök: 3 hosting, CPU'ya göre sıralı
	code, m := istek(t, h.EnCok, "/db-cpu/en-cok?sure=1s", kok)
	l := liste(m)
	if code != 200 || len(l) != 3 || l[0]["db_kullanici"] != "b_db" || l[1]["db_kullanici"] != "k_db" || l[2]["db_kullanici"] != "a_db" {
		t.Fatalf("kök 1 saat: %d %v", code, m)
	}
	if l[0]["cpu_sn"].(float64) != 90 || l[0]["kisitli"] != true || l[2]["kisitli"] != false || l[0]["bayi_id"].(float64) != 8 {
		t.Fatalf("kök alanları: %v", l[0])
	}
	if vts := l[2]["veritabanlari"].([]any); len(vts) != 2 || vts[0] != "a_main" || vts[1] != "a_wp" {
		t.Fatalf("veritabanı adları: %v", l[2])
	}
	// 24 saat: a_db iki dilim birikir (30+10)
	_, m = istek(t, h.EnCok, "/db-cpu/en-cok?sure=24s", kok)
	for _, x := range liste(m) {
		if x["db_kullanici"] == "a_db" && x["cpu_sn"].(float64) != 40 {
			t.Fatalf("24 saatlik toplam: %v", x)
		}
	}
	// kök bayi süzgeci
	_, m = istek(t, h.EnCok, "/db-cpu/en-cok?bayi=7", kok)
	if l = liste(m); len(l) != 1 || l[0]["db_kullanici"] != "a_db" {
		t.Fatalf("bayi=7 süzgeci: %v", m)
	}
	// bayi 7: YALNIZ kendi hostingi; başkasının bayi_id/alan adı/CPU'su görünmez; pay kendi toplamına göre
	code, m = istek(t, h.EnCok, "/db-cpu/en-cok?bayi=8", bayi7) // istemci parametresi YOK SAYILIR
	l = liste(m)
	if code != 200 || len(l) != 1 || l[0]["db_kullanici"] != "a_db" || l[0]["pay_yuzde"].(float64) != 100 {
		t.Fatalf("bayi kapsamı sızdı: %v", m)
	}
	if _, var_ := l[0]["bayi_id"]; var_ {
		t.Fatal("bayiye bayi_id döndürülmemeli")
	}
	if raw, _ := json.Marshal(m); strings.Contains(string(raw), "b.test") || strings.Contains(string(raw), "b_db") || strings.Contains(string(raw), "kok.test") {
		t.Fatalf("başka hostingin verisi bayiye sızdı: %s", raw)
	}
	// kısıtlar
	code, m = istek(t, h.Kisitlar, "/db-cpu/kisitlar", kok)
	if l = liste(m); code != 200 || len(l) != 1 || l[0]["db_kullanici"] != "b_db" || l[0]["aktif"] != true || l[0]["bayi_id"].(float64) != 8 {
		t.Fatalf("kök kısıtlar: %v", m)
	}
	if _, m = istek(t, h.Kisitlar, "/db-cpu/kisitlar", bayi7); len(liste(m)) != 0 {
		t.Fatalf("bayi 7 başkasının kısıtını görmemeli: %v", m)
	}
	if _, m = istek(t, h.Kisitlar, "/db-cpu/kisitlar?durum=tumu&sure=7g", bayi8); len(liste(m)) != 1 || liste(m)[0]["alan_adi"] != "b.test" {
		t.Fatalf("bayi 8 kendi kısıtını görmeli: %v", m)
	}
	// yetkisizler
	for _, c := range []*auth.Claims{nil, {Role: "user"}, {Role: "reseller"}} {
		if code, _ := istek(t, h.EnCok, "/db-cpu/en-cok", c); code != 403 {
			t.Errorf("yetkisiz %+v → 403 bekleniyordu: %d", c, code)
		}
		if code, _ := istek(t, h.Kisitlar, "/db-cpu/kisitlar", c); code != 403 {
			t.Errorf("yetkisiz kısıtlar %+v: %d", c, code)
		}
	}
	// durum yalnız kök
	h.Durum = func() (Durum, bool) { return Durum{Mod: ModUygula, Userstat: "acik"}, true }
	if code, m = istek(t, h.DurumUcu, "/db-cpu/durum", kok); code != 200 || m["calisiyor"] != true {
		t.Fatalf("kök durum: %d %v", code, m)
	}
	if code, _ = istek(t, h.DurumUcu, "/db-cpu/durum", bayi7); code != 403 {
		t.Fatalf("bayi durum görmemeli: %d", code)
	}
	// geçersiz bayi süzgeci reddedilir (sessizce 'hepsi' olmasın)
	if code, _ = istek(t, h.EnCok, "/db-cpu/en-cok?bayi=abc", kok); code != 403 {
		t.Fatalf("geçersiz bayi parametresi: %d", code)
	}
}

// Varsayılan (ayar satırı YOK) = gözlem: tam yığında hiçbir MariaDB eylemi, kayıt uygulandi=0, bildirim +
// denetim "uygula modunda kısılırdı".
func TestTamYiginVarsayilanGozlem(t *testing.T) {
	db := geciciDB(t)
	dbExec(t, db, `INSERT INTO users (id, username, role) VALUES (7,'bayi7','reseller')`)
	dbExec(t, db, `INSERT INTO domains (id, alan_adi, reseller_id) VALUES (1,'a.test',7)`)
	dbExec(t, db, `INSERT INTO db_accounts (domain_id, db_name, db_user) VALUES (1,'a_main','a_db')`)
	dbExec(t, db, `INSERT INTO cp_ayarlar (anahtar, deger) VALUES ('db_kisici_poll_sn','15'),('db_kisici_pencere_sn','60')`)

	o := yeniOrtam(t)
	o.m.DB, o.m.Depo = db, &SQLDepo{DB: db}
	o.m.AyarOku = func(ctx context.Context) (Ayar, error) { return ayarOku(ctx, db) }
	o.yon.hesap["a_db"] = []Hesap{{Host: "localhost"}}
	o.olcer.cum["a_db"] = 0
	o.taban()
	o.calis(60, hizlar("a_db", 2.0)) // 15 dk
	if o.yon.alterSayisi() != 0 || len(o.yon.killler) != 0 || len(o.yon.kesilenler) != 0 {
		t.Fatal("varsayılan gözlem modunda MariaDB eylemi")
	}
	if n := sayim(t, db, `SELECT COUNT(*) FROM db_cpu_kisit WHERE acik=1 AND uygulandi=0 AND seviye=1 AND reseller_id=7 AND domain_id=1`); n != 1 {
		t.Fatalf("gözlem kaydı: %d", n)
	}
	var msg string
	if err := db.QueryRow(`SELECT mesaj FROM cp_bildirim WHERE kategori='kota' AND baslik='Veritabanı CPU eşiği aşıldı (gözlem modu)' AND domain_id=1 AND reseller_id=7`).Scan(&msg); err != nil {
		t.Fatalf("gözlem bildirimi yok: %v", err)
	}
	if !strings.Contains(msg, "a_db") || !strings.Contains(msg, "kısılırdı") {
		t.Fatalf("bildirim metni: %q", msg)
	}
	if sayim(t, db, `SELECT COUNT(*) FROM audit_log WHERE action='db.cpu_asim_gozlem' AND reseller_id=7`) != 1 ||
		sayim(t, db, `SELECT COUNT(*) FROM audit_log WHERE action IN ('db.cpu_kisit','db.cpu_kisit_yukselt','db.cpu_baglanti_kes')`) != 0 {
		t.Fatal("denetim: yalnız db.cpu_asim_gozlem beklenir")
	}
	if sayim(t, db, `SELECT COUNT(*) FROM cp_bildirim WHERE baslik='Veritabanı CPU kısıtı uygulandı'`) != 0 {
		t.Fatal("gözlem modunda 'kısıt uygulandı' bildirimi")
	}
}

func istekPut(t *testing.T, h http.HandlerFunc, yol, govde string, c *auth.Claims) (int, map[string]any) {
	t.Helper()
	claimsAl = func(*http.Request) *auth.Claims { return c }
	aktorAl = func(*http.Request) (int64, string) {
		if c == nil {
			return 0, ""
		}
		return c.UserID, c.Username
	}
	t.Cleanup(func() { claimsAl, aktorAl = nilClaimsAl, nilAktorAl })
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", yol, bytes.NewBufferString(govde))
	req.RemoteAddr = "203.0.113.9:4000"
	h(rec, req)
	var m map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	return rec.Code, m
}

var nilAktorAl = aktorAl

// PUT /db-cpu/durum: yalnız kök; geçerli mod; gerçek değişiklik denetime yazılır, aynı değer yazmaz.
func TestAPIModAyarla(t *testing.T) {
	db := geciciDB(t)
	h := &Handlers{DB: db}
	kok := &auth.Claims{UserID: 1, Username: "kok", Role: "admin"}
	bayi := &auth.Claims{UserID: 7, Username: "bayi7", Role: "reseller", ResellerID: 7}

	// GET: ayar yokken varsayılan gözlem
	_, m := istek(t, h.DurumUcu, "/db-cpu/durum", kok)
	if m["mod_ayar"] != "gozlem" || m["varsayilan_mod"] != "gozlem" || m["calisiyor"] != false {
		t.Fatalf("GET durum: %v", m)
	}
	// yetkisizler
	for _, c := range []*auth.Claims{nil, bayi, {Role: "user"}} {
		if code, _ := istekPut(t, h.ModAyarla, "/db-cpu/durum", `{"mod":"uygula"}`, c); code != 403 {
			t.Fatalf("yetkisiz PUT %+v: %d", c, code)
		}
	}
	if sayim(t, db, `SELECT COUNT(*) FROM cp_ayarlar WHERE anahtar='db_kisici_mod'`) != 0 {
		t.Fatal("yetkisiz istek ayarı değiştirdi")
	}
	// geçersiz
	for _, g := range []string{`{"mod":"agresif"}`, `{"mod":""}`, `{}`, `bozuk`, `{"mod":["uygula"]}`} {
		if code, _ := istekPut(t, h.ModAyarla, "/db-cpu/durum", g, kok); code != 400 {
			t.Fatalf("geçersiz gövde %s: %d", g, code)
		}
	}
	// gözlem → uygula (değişiklik)
	code, m := istekPut(t, h.ModAyarla, "/db-cpu/durum", `{"mod":" Uygula "}`, kok)
	if code != 200 || m["mod"] != "uygula" || m["onceki"] != "gozlem" || m["degisti"] != true {
		t.Fatalf("PUT: %d %v", code, m)
	}
	var deger string
	if err := db.QueryRow(`SELECT deger FROM cp_ayarlar WHERE anahtar='db_kisici_mod'`).Scan(&deger); err != nil || deger != "uygula" {
		t.Fatalf("ayar yazılmadı: %q %v", deger, err)
	}
	var aktor, hedef, detay, ip string
	var uid, kapsam int64
	if err := db.QueryRow(`SELECT actor_user_id, actor_username, target, detail, ip, reseller_id FROM audit_log WHERE action='db.cpu_kisici_mod'`).Scan(&uid, &aktor, &hedef, &detay, &ip, &kapsam); err != nil {
		t.Fatalf("denetim kaydı yok: %v", err)
	}
	if uid != 1 || aktor != "kok" || hedef != "db-cpu" || !strings.Contains(detay, "gozlem → uygula") || kapsam != 0 {
		t.Fatalf("denetim: %d %s %s %s %d", uid, aktor, hedef, detay, kapsam)
	}
	// aynı değer: değişmedi, yeni denetim satırı YOK
	code, m = istekPut(t, h.ModAyarla, "/db-cpu/durum", `{"mod":"uygula"}`, kok)
	if code != 200 || m["degisti"] != false || sayim(t, db, `SELECT COUNT(*) FROM audit_log WHERE action='db.cpu_kisici_mod'`) != 1 {
		t.Fatalf("aynı değer: %d %v", code, m)
	}
	// ayarOku yeni modu görür; GET yansıtır; kapalı da geçerli
	if a, err := ayarOku(context.Background(), db); err != nil || a.Mod != ModUygula {
		t.Fatalf("ayarOku: %+v %v", a, err)
	}
	_, m = istek(t, h.DurumUcu, "/db-cpu/durum", kok)
	if m["mod_ayar"] != "uygula" {
		t.Fatalf("GET durum mod_ayar: %v", m)
	}
	if code, _ = istekPut(t, h.ModAyarla, "/db-cpu/durum", `{"mod":"kapali"}`, kok); code != 200 ||
		sayim(t, db, `SELECT COUNT(*) FROM audit_log WHERE action='db.cpu_kisici_mod' AND detail LIKE '%uygula → kapali%'`) != 1 {
		t.Fatal("kapali geçişi / denetimi")
	}
}

// Kısıt API'si 3. seviyeyi ve kesilen bağlantı sayısını gösterir.
func TestAPIKesilenBaglanti(t *testing.T) {
	db := geciciDB(t)
	now := time.Unix(1_800_000_000, 0)
	dbExec(t, db, `INSERT INTO domains (id, alan_adi, reseller_id) VALUES (1,'a.test',7)`)
	d := &SQLDepo{DB: db}
	if _, err := d.Ac(context.Background(), &Kisit{Kullanici: "a_db", Kimlik: Kimlik{DomainID: 1, ResellerID: 7, AlanAdi: "a.test"},
		Birimler: []BirimRef{{Tur: "hosting", ID: 1, Esik: 1}}, Seviye: 3, KisitDeger: 2, Kesilen: 7, Uygulandi: true, Baslangic: now, SonAsim: now}); err != nil {
		t.Fatal(err)
	}
	h := &Handlers{DB: db, Simdi: func() time.Time { return now }}
	_, m := istek(t, h.Kisitlar, "/db-cpu/kisitlar", &auth.Claims{UserID: 7, Role: "reseller", ResellerID: 7})
	l := m["liste"].([]any)
	if len(l) != 1 || l[0].(map[string]any)["seviye"].(float64) != 3 || l[0].(map[string]any)["kesilen_baglanti"].(float64) != 7 {
		t.Fatalf("kısıt satırı: %v", m)
	}
}
