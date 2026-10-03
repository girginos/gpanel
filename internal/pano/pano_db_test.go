package pano

// pano_db_test.go — gerçek (geçici) MariaDB ile: widget kapsamı (genel sızıntı
// tarayıcı: defterdeki HER widget bayi jetonuyla), yetki/hata gövdesi, örnekleyici
// uçtan uca (disk ölçümü, günlük görüntü, kenar-tetikli eşik bildirimleri, websec,
// biten işler), saklama. GOSP_TEST_GECICI_DSN yoksa ATLANIR.

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/auth"
	"girginospanel/internal/kaynaklimit"
	"girginospanel/internal/middleware"
)

var pt_sir = []byte("pano-test-anahtari-0123456789abcdef0123")

func pt_exec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("SQL: %v\n%s", err, q)
	}
}

func pt_say(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("sayım: %v (%s)", err, q)
	}
	return n
}

func pt_goc(t *testing.T, db *sql.DB, dosya string) {
	t.Helper()
	body, err := os.ReadFile("../../migrations/" + dosya)
	if err != nil {
		t.Fatal(err)
	}
	var temiz []string
	for _, s := range strings.Split(string(body), "\n") {
		if x := strings.TrimSpace(s); x != "" && !strings.HasPrefix(x, "--") {
			temiz = append(temiz, s)
		}
	}
	for _, q := range strings.Split(strings.Join(temiz, "\n"), ";") {
		if q = strings.TrimSpace(q); q != "" {
			pt_exec(t, db, q)
		}
	}
}

// pt_db — şema + tohum. Bayi 7: 10 (bayi7a), 11 (bayi7b, askıda); bayi 8: 20
// (bayi8x, askıda); kök: 30 (kokx, askıda). Alan adları ayırt edici: sızıntı
// tarayıcı JSON'da "bayi8x"/"kokx" arar.
func pt_db(t *testing.T) *sql.DB {
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
	ad := fmt.Sprintf("gosp_test_pano_%d", time.Now().UnixNano())
	pt_exec(t, kok, "CREATE DATABASE "+ad+" CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci")
	t.Cleanup(func() { _, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad); _ = kok.Close() })
	cfg.DBName = ad
	cfg.ParseTime = false
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, q := range []string{
		`CREATE TABLE users (id BIGINT UNSIGNED PRIMARY KEY, username VARCHAR(64) NOT NULL, role VARCHAR(16) NOT NULL,
			max_domain INT NOT NULL DEFAULT 0, max_disk_mb BIGINT NOT NULL DEFAULT 0, max_trafik_mb BIGINT NOT NULL DEFAULT 0,
			asim_bildirim TINYINT(1) NOT NULL DEFAULT 1)`,
		`CREATE TABLE service_plans (id BIGINT UNSIGNED PRIMARY KEY, disk_kota_mb INT NOT NULL DEFAULT 0, trafik_kota_mb BIGINT NOT NULL DEFAULT 0)`,
		`CREATE TABLE domains (id BIGINT UNSIGNED PRIMARY KEY, alan_adi VARCHAR(253) NOT NULL, sistem_kullanici VARCHAR(64) NOT NULL,
			reseller_id BIGINT NOT NULL DEFAULT 0, boyut_kb BIGINT NOT NULL DEFAULT 0, trafik_kb BIGINT NOT NULL DEFAULT 0,
			disk_kota_mb INT NOT NULL DEFAULT 0, plan_id BIGINT UNSIGNED NULL, ssl_aktif TINYINT(1) NOT NULL DEFAULT 0,
			ssl_bitis DATE NULL, askida TINYINT(1) NOT NULL DEFAULT 0, askida_kaynak VARCHAR(8) NOT NULL DEFAULT '',
			is_demo TINYINT(1) NOT NULL DEFAULT 0, durum VARCHAR(8) NOT NULL DEFAULT 'aktif',
			backup_freq VARCHAR(16) NOT NULL DEFAULT 'none', last_backup_at TIMESTAMP NULL DEFAULT NULL,
			olusturulma TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE audit_log (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, ts TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
			action VARCHAR(64) NOT NULL, target VARCHAR(255) NOT NULL DEFAULT '', ok TINYINT(1) NOT NULL DEFAULT 1,
			reseller_id BIGINT UNSIGNED NOT NULL DEFAULT 0)`,
		`CREATE TABLE tasima_isleri (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, kaynak_tip VARCHAR(32) NOT NULL,
			durum VARCHAR(16) NOT NULL, toplam INT NOT NULL DEFAULT 0, tamamlanan INT NOT NULL DEFAULT 0,
			basarisiz INT NOT NULL DEFAULT 0, sahip_rid BIGINT UNSIGNED NOT NULL DEFAULT 0,
			baslangic DATETIME NULL, bitis DATETIME NULL, olusturma DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE backup_jobs (id BIGINT AUTO_INCREMENT PRIMARY KEY, tur VARCHAR(16) NOT NULL DEFAULT 'manuel',
			islem VARCHAR(16) NOT NULL DEFAULT 'yedek', durum VARCHAR(16) NOT NULL DEFAULT 'calisiyor',
			toplam INT NOT NULL DEFAULT 0, basari INT NOT NULL DEFAULT 0, hata INT NOT NULL DEFAULT 0,
			baslangic TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, bitis TIMESTAMP NULL DEFAULT NULL)`,
		`CREATE TABLE cp_toplu_isler (id BIGINT AUTO_INCREMENT PRIMARY KEY, tip VARCHAR(24) NOT NULL,
			durum VARCHAR(16) NOT NULL DEFAULT 'calisiyor', toplam INT NOT NULL DEFAULT 0, basari INT NOT NULL DEFAULT 0,
			hata INT NOT NULL DEFAULT 0, baslangic TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, bitis TIMESTAMP NULL DEFAULT NULL)`,
		`CREATE TABLE cp_websec_findings (id BIGINT AUTO_INCREMENT PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL,
			install_path VARCHAR(500) NOT NULL DEFAULT '', package_name VARCHAR(200) NOT NULL, installed_version VARCHAR(64) NOT NULL DEFAULT '',
			cve_id VARCHAR(64) NOT NULL, severity VARCHAR(16) NOT NULL DEFAULT '', fixed_in VARCHAR(64) NULL,
			first_seen DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE cp_websec_domain_tarama (domain_id BIGINT UNSIGNED PRIMARY KEY, son_tarama DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE cp_bildirim (id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
			seviye VARCHAR(16) NOT NULL DEFAULT 'bilgi', kategori VARCHAR(32) NOT NULL DEFAULT '',
			baslik VARCHAR(200) NOT NULL, mesaj TEXT NOT NULL, domain_id BIGINT UNSIGNED NULL,
			reseller_id BIGINT UNSIGNED NULL, ref_tur VARCHAR(24) NOT NULL DEFAULT '', ref_id BIGINT NOT NULL DEFAULT 0,
			okundu TINYINT(1) NOT NULL DEFAULT 0, created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			KEY idx_bildirim_okundu (okundu), KEY idx_bildirim_domain (domain_id), KEY idx_bildirim_reseller (reseller_id)) ENGINE=InnoDB`,
	} {
		pt_exec(t, db, q)
	}
	pt_goc(t, db, "0105_bildirim_okuyan.sql")
	pt_goc(t, db, "0126_pano_gunluk_esik.sql")
	for _, q := range []string{
		`INSERT INTO users (id, username, role, max_domain, max_disk_mb, max_trafik_mb, asim_bildirim) VALUES
			(1,'root','admin',0,0,0,1), (7,'bayiyedi','reseller',10,100,0,1), (8,'bayisekiz','reseller',0,0,0,1)`,
		`INSERT INTO service_plans (id, disk_kota_mb, trafik_kota_mb) VALUES (1, 100, 1000)`,
		`INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id, boyut_kb, trafik_kb, plan_id, ssl_aktif, ssl_bitis, askida, askida_kaynak, backup_freq) VALUES
			(10,'bayi7a.test','c_b7a',7,0,921600,1,1,CURDATE()+INTERVAL 5 DAY,0,'','daily'),
			(11,'bayi7b.test','c_b7b',7,0,0,1,0,NULL,1,'bayi_el','none'),
			(20,'bayi8x.test','c_b8x',8,101376,0,1,1,CURDATE()+INTERVAL 2 DAY,1,'manuel','daily'),
			(30,'kokx.test','c_kok',0,0,0,1,1,CURDATE()-INTERVAL 3 DAY,1,'manuel','daily')`,
		`INSERT INTO audit_log (action, target, ok, reseller_id) VALUES
			('hosting.askiya_al','bayi7b.test',1,7), ('hosting.askiya_al','bayi8x.test',1,8), ('hosting.askiya_al','kokx.test',1,0)`,
		`INSERT INTO cp_websec_findings (domain_id, package_name, cve_id, severity, install_path) VALUES
			(10,'pakyedi','CVE-7','high','/home/c_b7a/public_html'), (20,'paksekiz','CVE-8','CRITICAL','/home/c_b8x/x'), (30,'pakkok','CVE-0','high','/home/c_kok/x')`,
		`INSERT INTO cp_websec_domain_tarama (domain_id) VALUES (10),(20),(30)`,
		`INSERT INTO tasima_isleri (kaynak_tip, durum, toplam, tamamlanan, sahip_rid, bitis) VALUES
			('cpanel-yedi','tamam',2,2,7,NOW()), ('cpanel-sekiz','hata',1,0,8,NOW()), ('cpanel-kok','tamam',1,1,0,NOW())`,
		`INSERT INTO backup_jobs (tur, islem, durum, toplam, basari) VALUES ('manuel','yedek','tamam',4,4)`,
		`INSERT INTO cp_toplu_isler (tip, durum, toplam, basari, bitis) VALUES ('ssl-kok','tamam',3,3,NOW())`,
	} {
		pt_exec(t, db, q)
	}
	return db
}

func pt_router(h *Handlers) http.Handler {
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(pt_sir))
		r.Get("/pano/w/{id}", h.Widget)
	})
	return r
}

func pt_tok(t *testing.T, rol string, rid int64) string {
	t.Helper()
	var tok string
	var err error
	if rol == "admin" {
		tok, err = auth.IssueAt(pt_sir, 3600, 1, "root", "admin", 0, "")
	} else {
		tok, err = auth.IssueResellerAt(pt_sir, 3600, 100+rid, fmt.Sprintf("bayi%d", rid), rid, 0, "")
	}
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func pt_get(t *testing.T, h *Handlers, yol, tok string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, yol, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	pt_router(h).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// Genel sızıntı tarayıcı: defterdeki HER widget bayi 7 jetonuyla — yanıtta bayi 8'in
// ve kökün alan adı/paket/iş adı YOK; ?reseller_id=8 gibi parametreler kapsamı
// değiştirmez. Pozitif kontrol: aynı veri yönetici yanıtında VAR (tarayıcı boşa geçmez).
func TestWidget_GenelSizintiTarayici(t *testing.T) {
	db := pt_db(t)
	ornekleyiciSahte(t, nil)
	Tur(context.Background(), db) // egilim için bugünün satırları
	h := &Handlers{DB: db}
	yabanci := map[int64][]string{
		7: {"bayi8x", "kokx", "paksekiz", "pakkok", "cpanel-sekiz", "cpanel-kok", "ssl-kok"},
		8: {"bayi7a", "bayi7b", "kokx", "pakyedi", "pakkok", "cpanel-yedi", "cpanel-kok", "ssl-kok"},
	}
	adminGovde := ""
	for id := range widgetlar {
		kod, g := pt_get(t, h, "/pano/w/"+id, pt_tok(t, "admin", 0))
		if kod != http.StatusOK {
			t.Fatalf("admin %s: %d %s", id, kod, g)
		}
		adminGovde += g
		for rid, yasak := range yabanci {
			kod, g := pt_get(t, h, "/pano/w/"+id, pt_tok(t, "bayi", rid))
			if kod != http.StatusOK {
				t.Fatalf("bayi%d %s: %d %s", rid, id, kod, g)
			}
			for _, y := range yasak {
				if strings.Contains(g, y) {
					t.Errorf("bayi%d %s yanıtında yabancı veri %q: %s", rid, id, y, g)
				}
			}
			if strings.Contains(g, "/home/") {
				t.Errorf("bayi%d %s yanıtında sunucu yolu: %s", rid, id, g)
			}
			_, g2 := pt_get(t, h, "/pano/w/"+id+"?reseller_id=8&kapsam=0&sahip=8&rid=0", pt_tok(t, "bayi", rid))
			if g2 != g {
				t.Errorf("bayi%d %s: istemci parametresi kapsamı değiştirdi:\n%s\n%s", rid, id, g, g2)
			}
		}
	}
	for _, y := range []string{"bayi8x", "kokx", "paksekiz", "pakkok", "cpanel-kok", "ssl-kok"} {
		if !strings.Contains(adminGovde, y) {
			t.Errorf("pozitif kontrol: yönetici yanıtlarında %q yok (tohum/sorgu bozuk)", y)
		}
	}
	// Eğilim: bayi 7 kendi sayısını (2 hesap) görür, sunucu toplamını (4) değil.
	_, g := pt_get(t, h, "/pano/w/egilim", pt_tok(t, "bayi", 7))
	if !strings.Contains(g, `"hesap":2,`) || strings.Contains(g, `"hesap":4,`) {
		t.Errorf("bayi 7 eğilimi kendi kapsamı olmalı: %s", g)
	}
	_, g = pt_get(t, h, "/pano/w/egilim", pt_tok(t, "admin", 0))
	if !strings.Contains(g, `"hesap":4,`) {
		t.Errorf("yönetici eğilimi tüm sunucu olmalı: %s", g)
	}
}

func TestWidget_YetkiVeHataGovdesi(t *testing.T) {
	db := pt_db(t)
	h := &Handlers{DB: db}
	if kod, _ := pt_get(t, h, "/pano/w/yok", pt_tok(t, "admin", 0)); kod != http.StatusNotFound {
		t.Errorf("bilinmeyen widget 404 olmalı: %d", kod)
	}
	// Bayi jetonu ama rid=0 → 403 (yönetici kapsamına DÜŞMEZ).
	tok, err := auth.IssueResellerAt(pt_sir, 3600, 55, "bozuk", 0, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if kod, g := pt_get(t, h, "/pano/w/kota", tok); kod != http.StatusForbidden {
		t.Errorf("rid=0 bayi jetonu 403 olmalı: %d %s", kod, g)
	}
	pt_exec(t, db, `DROP TABLE cp_websec_findings`)
	kod, g := pt_get(t, h, "/pano/w/websec", pt_tok(t, "bayi", 7))
	if kod != http.StatusInternalServerError || strings.Contains(g, "1146") || strings.Contains(g, "cp_websec") {
		t.Errorf("hata gövdesi ham SQL içermemeli: %d %s", kod, g)
	}
}

func TestKapsamCoz(t *testing.T) {
	cases := []struct {
		ad   string
		c    *auth.Claims
		want Kapsam
		ok   bool
	}{
		{"jetonsuz", nil, Kapsam{}, false},
		{"admin", &auth.Claims{Role: "admin"}, Kapsam{Tum: true}, true},
		{"bayi", &auth.Claims{Role: "reseller", ResellerID: 7}, Kapsam{RID: 7}, true},
		{"bayi rid=0", &auth.Claims{Role: "reseller"}, Kapsam{}, false},
		{"admin+bayi anahtarı", &auth.Claims{Role: "admin", ResellerID: 9}, Kapsam{RID: 9}, true},
		{"bilinmeyen rol", &auth.Claims{Role: "user", ResellerID: 7}, Kapsam{}, false},
	}
	for _, c := range cases {
		got, ok := claimsKapsam(c.c)
		if ok != c.ok || got != c.want {
			t.Errorf("%s: %+v %v, beklenen %+v %v", c.ad, got, ok, c.want, c.ok)
		}
	}
}

// ornekleyiciSahte — dikişler: disk raporu (sk → KB) ve SSL tazeleyici (no-op).
func ornekleyiciSahte(t *testing.T, disk map[string]int64) {
	t.Helper()
	eskiK, eskiS := kotaOkuyucu, sslTazeleyici
	t.Cleanup(func() { kotaOkuyucu, sslTazeleyici = eskiK, eskiS; sslSon = time.Time{} })
	kotaOkuyucu = func() (map[string]kaynaklimit.KotaKullanim, bool) {
		if disk == nil {
			return nil, false
		}
		m := map[string]kaynaklimit.KotaKullanim{}
		for sk, kb := range disk {
			m[sk] = kaynaklimit.KotaKullanim{KullanilanKB: kb}
		}
		return m, true
	}
	sslTazeleyici = func(context.Context, *sql.DB) int { return 0 }
}

type pt_bil struct {
	baslik string
	dom    sql.NullInt64
	rid    sql.NullInt64
}

func pt_bildirimler(t *testing.T, db *sql.DB, kosul string) []pt_bil {
	t.Helper()
	rows, err := db.Query(`SELECT baslik, domain_id, reseller_id FROM cp_bildirim WHERE ` + kosul + ` ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []pt_bil
	for rows.Next() {
		var b pt_bil
		if err := rows.Scan(&b.baslik, &b.dom, &b.rid); err != nil {
			t.Fatal(err)
		}
		out = append(out, b)
	}
	return out
}

func pt_basliklar(l []pt_bil) string {
	var s []string
	for _, b := range l {
		s = append(s, b.baslik)
	}
	return strings.Join(s, " | ")
}

// Örnekleyici uçtan uca: disk ölçümü → eşik bildirimi BİR kez; sonraki turlar
// (okunmuş olsa bile) yeni satır açmaz; yeni kova yeni bildirim; havuz → yalnız
// bayi (+ %100'de asim_bildirim ile yönetici); websec taban çizgisi.
func TestTur_KenarTetikliBildirimler(t *testing.T) {
	db := pt_db(t)
	disk := map[string]int64{"c_b7a": 95 * 1024, "c_b8x": 101376, "root": 999999}
	ornekleyiciSahte(t, disk)
	ctx := context.Background()
	Tur(ctx, db)

	if n := pt_say(t, db, `SELECT boyut_kb FROM domains WHERE id=10`); n != 95*1024 {
		t.Errorf("disk ölçümü boyut_kb'ye yazılmalı: %d", n)
	}
	d10 := pt_basliklar(pt_bildirimler(t, db, "domain_id=10"))
	for _, b := range []string{"Disk kotası %85'i aştı", "Aylık trafik kotası %85'i aştı", "SSL sertifikası yenilenemedi"} {
		if !strings.Contains(d10, b) {
			t.Errorf("domain 10 bildirimlerinde %q yok: %s", b, d10)
		}
	}
	if d20 := pt_basliklar(pt_bildirimler(t, db, "domain_id=20")); strings.Contains(d20, "SSL") {
		t.Errorf("askıdaki hesap için SSL bildirimi üretilmemeli: %s", d20)
	}
	havuz := pt_bildirimler(t, db, "domain_id IS NULL AND reseller_id IS NOT NULL")
	if len(havuz) != 1 || havuz[0].rid.Int64 != 7 || havuz[0].baslik != "Kaynak havuzu: Disk kotası %85'i aştı" {
		t.Errorf("havuz bildirimi yalnız bayi 7'ye: %+v", havuz)
	}
	if n := pt_say(t, db, `SELECT COUNT(*) FROM cp_bildirim WHERE kategori='websec'`); n != 0 {
		t.Errorf("ilk tur websec taban çizgisi: bildirim olmamalı (%d)", n)
	}
	if n := pt_say(t, db, `SELECT COUNT(*) FROM pano_gunluk WHERE tarih=CURDATE() AND kapsam_id IN (0,7,8)`); n != 3 {
		t.Errorf("günlük görüntü 0,7,8 için yazılmalı: %d", n)
	}

	toplam := pt_say(t, db, `SELECT COUNT(*) FROM cp_bildirim`)
	Tur(ctx, db)
	// Herkes okudu → yine yeni satır YOK (kenar-tetikli; cp_bildirim tekilleştirmesi değil).
	pt_exec(t, db, `INSERT IGNORE INTO cp_bildirim_okundu (bildirim_id, okuyan_tur, okuyan_id)
		SELECT id, 'bayi', reseller_id FROM cp_bildirim WHERE reseller_id IS NOT NULL
		UNION ALL SELECT id, 'musteri', domain_id FROM cp_bildirim WHERE domain_id IS NOT NULL
		UNION ALL SELECT id, 'admin', 1 FROM cp_bildirim`)
	Tur(ctx, db)
	if n := pt_say(t, db, `SELECT COUNT(*) FROM cp_bildirim`); n != toplam {
		t.Fatalf("durum sürerken yeni bildirim açılmamalı: %d → %d", toplam, n)
	}

	// Yeni kova: disk %100 → "doldu" (domain) + havuz doldu (bayi) + yönetici (asim_bildirim=1).
	disk["c_b7a"] = 101 * 1024
	Tur(ctx, db)
	if d := pt_basliklar(pt_bildirimler(t, db, "domain_id=10")); !strings.Contains(d, "Disk kotası doldu") {
		t.Errorf("%%100 kovası bildirilmeli: %s", d)
	}
	if h := pt_bildirimler(t, db, "domain_id IS NULL AND reseller_id=7 AND baslik='Kaynak havuzu: Disk kotası doldu'"); len(h) != 1 {
		t.Errorf("havuz doldu bayiye: %+v", h)
	}
	y := pt_bildirimler(t, db, "domain_id IS NULL AND reseller_id IS NULL")
	if len(y) != 1 || y[0].baslik != "Bayi kaynak havuzu doldu" {
		t.Errorf("asim_bildirim=1 → yöneticiye tek bildirim: %+v", y)
	}

	// Websec: taban çizgisinden sonra yeni yüksek bulgu → domain 10'a bir bildirim.
	pt_exec(t, db, `INSERT INTO cp_websec_findings (domain_id, package_name, cve_id, severity) VALUES (10,'yenipaket','CVE-9','critical')`)
	Tur(ctx, db)
	w := pt_bildirimler(t, db, "kategori='websec'")
	if len(w) != 1 || w[0].dom.Int64 != 10 {
		t.Errorf("yeni websec bulgusu domain 10'a: %+v", w)
	}

	// Devir: domain 10 bayi 8'e geçer → yeni sahip yukarı geçişi bir kez görür.
	pt_exec(t, db, `UPDATE domains SET reseller_id=8 WHERE id=10`)
	Tur(ctx, db)
	if n := pt_say(t, db, `SELECT COUNT(*) FROM cp_bildirim WHERE domain_id=10 AND reseller_id=8 AND baslik='Disk kotası doldu'`); n != 1 {
		t.Errorf("devirden sonra yeni sahibe tek 'doldu' bildirimi: %d", n)
	}
}

func TestIsTur_TabanCizgisiVeKapsam(t *testing.T) {
	db := pt_db(t)
	ctx := context.Background()
	IsTur(ctx, db)
	if n := pt_say(t, db, `SELECT COUNT(*) FROM cp_bildirim`); n != 0 {
		t.Fatalf("ilk tur taban çizgisi: mevcut işler bildirilmemeli (%d)", n)
	}
	pt_exec(t, db, `INSERT INTO tasima_isleri (kaynak_tip, durum, toplam, tamamlanan, basarisiz, sahip_rid, bitis) VALUES
		('plesk','tamam',3,3,1,7,NOW()), ('plesk','calisiyor',3,1,0,8,NULL)`)
	pt_exec(t, db, `INSERT INTO cp_toplu_isler (tip, durum, toplam, basari, hata, bitis) VALUES ('php','kismi',4,3,1,NOW())`)
	IsTur(ctx, db)
	l := pt_bildirimler(t, db, "1=1")
	if len(l) != 2 {
		t.Fatalf("taşıma (bayi 7) + toplu (yönetici) beklenir: %+v", l)
	}
	if l[0].baslik != "Taşıma kısmen tamamlandı" || l[0].rid.Int64 != 7 || l[0].dom.Valid {
		t.Errorf("taşıma bildirimi yalnız bayi 7'ye (alan adsız): %+v", l[0])
	}
	if l[1].baslik != "Toplu işlem kısmen tamamlandı" || l[1].rid.Valid || l[1].dom.Valid {
		t.Errorf("toplu işlem bildirimi yöneticiye (panel geneli): %+v", l[1])
	}
	IsTur(ctx, db)
	if n := pt_say(t, db, `SELECT COUNT(*) FROM cp_bildirim`); n != 2 {
		t.Errorf("aynı iş ikinci kez bildirilmemeli: %d", n)
	}
}

func TestTemizlik_Saklama(t *testing.T) {
	db := pt_db(t)
	ctx := context.Background()
	pt_exec(t, db, `INSERT INTO pano_gunluk (tarih, kapsam_id, hesap) VALUES (CURDATE() - INTERVAL 500 DAY, 7, 1), (CURDATE(), 7, 2)`)
	pt_exec(t, db, `INSERT INTO cp_esik_durum (konu_tur, konu_id, metrik, seviye) VALUES ('domain', 999, 'disk', 85), ('domain', 10, 'disk', 85),
		('bayi', 999, 'disk', 85), ('bayi', 7, 'disk', 85)`)
	pt_exec(t, db, `INSERT INTO cp_esik_durum (konu_tur, konu_id, metrik, seviye, olcum_at) VALUES ('is', 1, 'tasima', 1, NOW() - INTERVAL 40 DAY)`)
	Temizlik(ctx, db)
	if n := pt_say(t, db, `SELECT COUNT(*) FROM pano_gunluk`); n != 1 {
		t.Errorf("400 günden eski görüntü silinmeli: %d kaldı", n)
	}
	if n := pt_say(t, db, `SELECT COUNT(*) FROM cp_esik_durum`); n != 2 {
		t.Errorf("yetim domain/bayi durumu ve eski iş işareti silinmeli: %d kaldı", n)
	}
}
