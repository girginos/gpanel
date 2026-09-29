package websec

// kapsam_db_test.go — gerçek SQL (MariaDB) ile websec kapsamı ve domain başına
// tarama kaydı (0112). DB yoksa testler ATLANIR.
//
// Test YALNIZ kendi açtığı gosp_test_websec_<zaman> veritabanını kullanır ve sonunda
// DROP eder; DSN'deki veritabanı adı yok sayılır. Canlı panel sunucusunu GÖSTERME:
//
//	GOSP_TEST_GECICI_DSN='root@unix(/tmp/gecici-db/sock)/' go test ./internal/websec/
//
// Tarama (wp-cli/dosya okuma/dış feed) HİÇ çağrılmaz: bulgu/envanter satırları elle
// yazılır, handler'lar yalnız okur; kuyruk bellekte kalır.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/auth"
)

// Şema göçlerin KENDİSİNDEN kurulur (elle kopya değil): 0063 bulgu+durum, 0074 FK,
// 0076 envanter, 0112 domain başına tarama.
var wsGoclar = []string{
	"../../migrations/0063_websec.sql",
	"../../migrations/0074_websec_domain_cascade.sql",
	"../../migrations/0076_websec_apps.sql",
	"../../migrations/0112_websec_domain_tarama.sql",
}

func wsGeciciDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li test atlandı")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("DSN çözülemedi: %v", err)
	}
	cfg.DBName = ""
	kok, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatalf("db açılamadı: %v", err)
	}
	ad := fmt.Sprintf("gosp_test_websec_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		_ = kok.Close()
		t.Fatalf("geçici veritabanı oluşturulamadı: %v", err)
	}
	t.Cleanup(func() {
		_, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad)
		_ = kok.Close()
	})
	cfg.DBName = ad
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatalf("db açılamadı: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// domains: yalnız websec'in okuduğu sütunlar, canlı şemadaki tiplerle.
	wsExec(t, db, `CREATE TABLE domains (
		id               BIGINT UNSIGNED NOT NULL PRIMARY KEY,
		alan_adi         VARCHAR(253)    NOT NULL,
		sistem_kullanici VARCHAR(64)     NOT NULL,
		reseller_id      BIGINT          NOT NULL DEFAULT 0,
		olusturulma      TIMESTAMP       NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE KEY alan_adi (alan_adi),
		KEY ix_dom_reseller (reseller_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`)
	// audit_log: yalnız httpx.Denetim'in yazdığı sütunlar (tarama tetiklemesi denetimi).
	wsExec(t, db, `CREATE TABLE audit_log (
		id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		actor_user_id  BIGINT UNSIGNED NULL,
		actor_username VARCHAR(64)     NOT NULL DEFAULT '',
		ip             VARCHAR(64)     NOT NULL DEFAULT '',
		action         VARCHAR(64)     NOT NULL,
		target         VARCHAR(255)    NULL,
		detail         LONGTEXT        NULL,
		ok             TINYINT(1)      NOT NULL DEFAULT 1,
		reseller_id    BIGINT          NOT NULL DEFAULT 0
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`)
	for _, g := range wsGoclar {
		wsGocUygula(t, db, g)
	}
	return db
}

func wsExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("SQL hatası: %v\n%s", err, q)
	}
}

// wsGocUygula — cmd/server runMigrations ile AYNI ayrıştırma: "--" ile başlayan
// satırlar atılır, ";" ile bölünür; "zaten var" türü hatalar zararsız sayılır.
func wsGocUygula(t *testing.T, db *sql.DB, yol string) {
	t.Helper()
	body, err := os.ReadFile(yol)
	if err != nil {
		t.Fatalf("göç dosyası okunamadı (%s): %v", yol, err)
	}
	var temiz []string
	for _, satir := range strings.Split(string(body), "\n") {
		s := strings.TrimSpace(satir)
		if s == "" || strings.HasPrefix(s, "--") {
			continue
		}
		temiz = append(temiz, satir)
	}
	for _, ifade := range strings.Split(strings.Join(temiz, "\n"), ";") {
		s := strings.TrimSpace(ifade)
		if s == "" {
			continue
		}
		if _, err := db.Exec(s); err != nil {
			m := strings.ToLower(err.Error())
			if strings.Contains(m, "duplicate column") || strings.Contains(m, "duplicate key") ||
				strings.Contains(m, "already exists") || strings.Contains(m, "duplicate entry") {
				continue
			}
			t.Fatalf("GÖÇ HATASI (%s): %v\n%s", yol, err, s)
		}
	}
}

// 0112 satır içi yorum / ";" içeren yorum taşımamalı: göç koşucusu yalnız "--" ile
// BAŞLAYAN satırları atar ve ";"den böler.
func TestGoc0112YorumKurali(t *testing.T) {
	body, err := os.ReadFile("../../migrations/0112_websec_domain_tarama.sql")
	if err != nil {
		t.Fatal(err)
	}
	for i, satir := range strings.Split(string(body), "\n") {
		s := strings.TrimSpace(satir)
		if strings.HasPrefix(s, "--") {
			if strings.Contains(s, ";") {
				t.Errorf("satır %d: yorumda ';' var: %q", i+1, s)
			}
			continue
		}
		if strings.Contains(s, "--") {
			t.Errorf("satır %d: satır içi yorum: %q", i+1, s)
		}
	}
}

// domainTaramaYaz: tam taramada tarama BAŞLADIKTAN sonra açılan domain damgalanmaz;
// kısmi taramada yalnız listedeki domainler; domain silinince kayıt CASCADE düşer.
func TestDB_DomainTaramaYaz(t *testing.T) {
	db := wsGeciciDB(t)
	wsExec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id, olusturulma) VALUES
		(10, 'on.test', 'on', 7, NOW() - INTERVAL 2 HOUR),
		(11, 'onbir.test', 'onbir', 8, NOW() - INTERVAL 2 HOUR),
		(13, 'yeni.test', 'yeni', 0, NOW())`)
	// Tam tarama 1 saat önce başladı: 13 sonra açıldı → damgalanmaz.
	domainTaramaYaz(t.Context(), db, nil, 3600)
	if got := wsDamgali(t, db); got != "10,11" {
		t.Fatalf("tam tarama damgası: %q, beklenen 10,11", got)
	}
	// Envanter: uygulaması olmayan domain, kendi tarama kaydı varsa "desteklenmiyor",
	// yoksa "beklemede" (sunucu geneli last_success DEĞİL).
	wsExec(t, db, `UPDATE cp_websec_status SET last_success=NOW() WHERE id=1`)
	kuyrukKur(t)
	kimlikKur(t, kokJeton)
	rr := istek(t, (&Handler{DB: db}).Apps, "GET", "/websec/apps", "")
	var a envanterYanit
	if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &a) != nil {
		t.Fatalf("apps: %d %s", rr.Code, rr.Body)
	}
	durum := map[int64]string{}
	for _, it := range a.Items {
		durum[it.DomainID] = it.Durum
	}
	if !reflect.DeepEqual(durum, map[int64]string{10: "desteklenmiyor", 11: "desteklenmiyor", 13: "beklemede"}) ||
		a.Desteklenmeyen != 2 {
		t.Fatalf("envanter durumu domain başına olmalı: %v desteklenmeyen=%d", durum, a.Desteklenmeyen)
	}
	// Kısmi tarama: yalnız listedeki (13 artık keşfedildi).
	wsExec(t, db, `UPDATE cp_websec_domain_tarama SET son_tarama = NOW() - INTERVAL 1 DAY`)
	domainTaramaYaz(t.Context(), db, []int64{13}, 1)
	var taze int
	if err := db.QueryRow(`SELECT COUNT(*) FROM cp_websec_domain_tarama WHERE son_tarama > NOW() - INTERVAL 1 HOUR`).Scan(&taze); err != nil {
		t.Fatal(err)
	}
	if got := wsDamgali(t, db); got != "10,11,13" || taze != 1 {
		t.Fatalf("kısmi tarama yalnız 13'ü damgalamalı: %q taze=%d", got, taze)
	}
	wsExec(t, db, `DELETE FROM domains WHERE id=10`)
	if got := wsDamgali(t, db); got != "11,13" {
		t.Fatalf("domain silinince kayıt CASCADE düşmeli: %q", got)
	}
}

func wsDamgali(t *testing.T, db *sql.DB) string {
	t.Helper()
	var s sql.NullString
	if err := db.QueryRow(`SELECT GROUP_CONCAT(domain_id ORDER BY domain_id) FROM cp_websec_domain_tarama`).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s.String
}

// wsTohum: domain 10 (bayi 7), 11 (bayi 8), 12 (kök) + bulgu/envanter/tarama kayıtları.
func wsTohum(t *testing.T, db *sql.DB) {
	t.Helper()
	wsExec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id, olusturulma) VALUES
		(10, 'on.test', 'on', 7, NOW() - INTERVAL 1 DAY),
		(11, 'onbir.test', 'onbir', 8, NOW() - INTERVAL 1 DAY),
		(12, 'oniki.test', 'oniki', 0, NOW() - INTERVAL 1 DAY)`)
	wsExec(t, db, `INSERT INTO cp_websec_findings
		(domain_id, app_type, install_path, package_name, installed_version, cve_id, severity, source) VALUES
		(10, 'wordpress', '/home/on/public_html#plugin:a', 'a', '1.0', 'CVE-2026-1', 'high', 't'),
		(11, 'wordpress', '/home/onbir/public_html#plugin:b', 'b', '1.0', 'CVE-2026-2', 'critical', 't'),
		(11, 'nodejs', '/home/onbir/app#npm:c', 'c', '1.0', 'CVE-2026-3', 'critical', 't'),
		(12, 'php-composer', '/home/oniki#composer:d', 'd', '1.0', 'CVE-2026-4', 'low', 't')`)
	wsExec(t, db, `INSERT INTO cp_websec_apps (domain_id, app_type, install_path, bulgu_sayisi) VALUES
		(10, 'wordpress', '/home/on/public_html', 1),
		(11, 'wordpress', '/home/onbir/public_html', 1),
		(12, 'php-composer', '/home/oniki', 1)`)
	wsExec(t, db, `INSERT INTO cp_websec_domain_tarama (domain_id, son_tarama) VALUES
		(10, '2026-09-29 10:00:00'), (11, '2026-09-29 11:00:00'), (12, '2026-09-29 12:00:00')`)
	wsExec(t, db, `UPDATE cp_websec_status SET total_findings=4, critical=2, high=1, low=1,
		scanned_apps=3, last_error='kök hatası' WHERE id=1`)
}

// wsGorunum — bir kimliğin gördükleri: bulgu domainleri + toplam, envanter domainleri,
// durum kartı toplamı ve son taraması.
type wsGorunum struct {
	bulgu    []int64
	toplam   int
	apps     []int64
	durumTop int
	sonTara  string
	durum    map[string]json.RawMessage
}

func wsGor(t *testing.T, db *sql.DB, c *auth.Claims) wsGorunum {
	t.Helper()
	kimlikKur(t, c)
	h := &Handler{DB: db}
	var g wsGorunum

	rr := istek(t, h.Findings, "GET", "/websec/findings", "")
	var b bulguYanit
	if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &b) != nil {
		t.Fatalf("findings: %d %s", rr.Code, rr.Body)
	}
	g.toplam = b.Toplam
	for _, it := range b.Items {
		g.bulgu = append(g.bulgu, it.DomainID)
	}

	rr = istek(t, h.Apps, "GET", "/websec/apps", "")
	var a envanterYanit
	if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &a) != nil {
		t.Fatalf("apps: %d %s", rr.Code, rr.Body)
	}
	for _, it := range a.Items {
		g.apps = append(g.apps, it.DomainID)
	}

	rr = istek(t, h.Status, "GET", "/websec/status", "")
	if rr.Code != 200 {
		t.Fatalf("status: %d %s", rr.Code, rr.Body)
	}
	g.durum = jsonAnahtarlar(t, rr.Body.Bytes())
	var d struct {
		Toplam  int     `json:"total_findings"`
		LastRun *string `json:"last_run"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &d)
	g.durumTop = d.Toplam
	if d.LastRun != nil {
		g.sonTara = *d.LastRun
	}
	sort.Slice(g.bulgu, func(i, j int) bool { return g.bulgu[i] < g.bulgu[j] })
	sort.Slice(g.apps, func(i, j int) bool { return g.apps[i] < g.apps[j] })
	return g
}

// Bayi 7 yalnız 10'u, bayi 8 yalnız 11'i, kök üçünü görür; bayinin durum sayaçları
// kendi bulgu listesiyle uyumlu; kimliksiz bayi hiçbir şey görmez.
func TestDB_BayiYalnizKendiDomainleri(t *testing.T) {
	db := wsGeciciDB(t)
	wsTohum(t, db)

	g7 := wsGor(t, db, bayiJeton)
	if !reflect.DeepEqual(g7.bulgu, []int64{10}) || g7.toplam != 1 || !reflect.DeepEqual(g7.apps, []int64{10}) ||
		g7.durumTop != 1 || g7.sonTara != "2026-09-29 10:00:00" {
		t.Fatalf("bayi 7 yalnız 10: %+v", g7)
	}
	for _, yok := range []string{"scanned_apps", "last_error", "duration_ms", "next_estimate", "app_sayaclari"} {
		if _, ok := g7.durum[yok]; ok {
			t.Fatalf("bayi durumunda %q olmamalı", yok)
		}
	}

	g8 := wsGor(t, db, &auth.Claims{UserID: 8, Username: "bayi8", Role: "reseller", ResellerID: 8})
	if !reflect.DeepEqual(g8.bulgu, []int64{11, 11}) || g8.toplam != 2 || !reflect.DeepEqual(g8.apps, []int64{11}) ||
		g8.durumTop != 2 || g8.sonTara != "2026-09-29 11:00:00" {
		t.Fatalf("bayi 8 yalnız 11: %+v", g8)
	}

	gk := wsGor(t, db, kokJeton)
	if !reflect.DeepEqual(gk.bulgu, []int64{10, 11, 11, 12}) || gk.toplam != 4 ||
		!reflect.DeepEqual(gk.apps, []int64{10, 11, 12}) || gk.durumTop != 4 {
		t.Fatalf("kök üçünü görmeli: %+v", gk)
	}
	if string(gk.durum["scanned_apps"]) != "3" || string(gk.durum["last_error"]) != `"kök hatası"` {
		t.Fatalf("kök durum kartı değişmemeli: %v", gk.durum)
	}

	g0 := wsGor(t, db, &auth.Claims{UserID: 7, Role: "reseller"})
	if len(g0.bulgu) != 0 || g0.toplam != 0 || len(g0.apps) != 0 || g0.durumTop != 0 || g0.sonTara != "" {
		t.Fatalf("kimliksiz bayi hiçbir şey görmemeli: %+v", g0)
	}
}

// Yabancı domain ile olmayan domain bayiye BAYT BAYT aynı yanıtı alır.
func TestDB_YabanciIleOlmayanAyni(t *testing.T) {
	db := wsGeciciDB(t)
	wsTohum(t, db)
	kimlikKur(t, bayiJeton)
	h := &Handler{DB: db}
	for _, uc := range []struct {
		ad string
		f  http.HandlerFunc
		y  string
	}{
		{"findings", h.Findings, "/websec/findings?domain_id="},
		{"apps", h.Apps, "/websec/apps?domain_id="},
	} {
		yabanci := istek(t, uc.f, "GET", uc.y+"11", "")
		olmayan := istek(t, uc.f, "GET", uc.y+"999", "")
		if yabanci.Code != olmayan.Code || yabanci.Body.String() != olmayan.Body.String() {
			t.Fatalf("%s: yabancı (%d %s) ile olmayan (%d %s) aynı olmalı", uc.ad,
				yabanci.Code, yabanci.Body, olmayan.Code, olmayan.Body)
		}
		if kendi := istek(t, uc.f, "GET", uc.y+"10", ""); kendi.Body.String() == olmayan.Body.String() {
			t.Fatalf("%s: kendi domaini görünmeli: %s", uc.ad, kendi.Body)
		}
	}
}

// Devir: kapsam canlı JOIN — 10 bayi 8'e geçince 7 artık görmez, 8 görür.
func TestDB_DevirSonrasiEskiSahipGormez(t *testing.T) {
	db := wsGeciciDB(t)
	wsTohum(t, db)
	wsExec(t, db, `UPDATE domains SET reseller_id=8 WHERE id=10`)
	g7 := wsGor(t, db, bayiJeton)
	if len(g7.bulgu) != 0 || g7.toplam != 0 || len(g7.apps) != 0 || g7.durumTop != 0 || g7.sonTara != "" {
		t.Fatalf("devirden sonra eski sahip görmemeli: %+v", g7)
	}
	g8 := wsGor(t, db, &auth.Claims{UserID: 8, Username: "bayi8", Role: "reseller", ResellerID: 8})
	if !reflect.DeepEqual(g8.apps, []int64{10, 11}) || g8.toplam != 3 || g8.durumTop != 3 {
		t.Fatalf("yeni sahip 10+11 görmeli: %+v", g8)
	}
}

// Tarama uçları (bayi, gerçek SQL): yabancı ile olmayan domain kod+gövde BAYT BAYT
// aynı ve kuyruk boş; rescan-many karışık liste 404 + kuyruk boş; "Domainlerimi
// Tara" yalnız kendi domainlerini kuyruğa alır; tetikleme denetime bayinin kapsamıyla.
func TestDB_BayiTaramaKapsami(t *testing.T) {
	db := wsGeciciDB(t)
	wsTohum(t, db)
	k := kuyrukKur(t)
	saatKur(t)
	kimlikKur(t, bayiJeton)
	h := &Handler{DB: db}

	yabanci := istek(t, h.Rescan, "POST", "/websec/rescan?domain_id=11", "")
	olmayan := istek(t, h.Rescan, "POST", "/websec/rescan?domain_id=999", "")
	if yabanci.Code != 404 || yabanci.Code != olmayan.Code || yabanci.Body.String() != olmayan.Body.String() {
		t.Fatalf("yabancı (%d %s) ile olmayan (%d %s) aynı 404 olmalı",
			yabanci.Code, yabanci.Body, olmayan.Code, olmayan.Body)
	}
	karisik := istek(t, h.RescanMany, "POST", "/websec/rescan-many", `{"domain_ids":[10,11]}`)
	if karisik.Code != 404 || karisik.Body.String() != olmayan.Body.String() {
		t.Fatalf("karışık liste tamamı 404: %d %s", karisik.Code, karisik.Body)
	}
	if k.tamMi() || k.bekleyenSayisi() != 0 || wsDenetimSayisi(t, db) != 0 {
		t.Fatal("reddedilen istek kuyruğa/denetime girmemeli")
	}

	rr := istek(t, h.Rescan, "POST", "/websec/rescan", "")
	if rr.Code != 202 || strings.TrimSpace(rr.Body.String()) != `{"sirada":1}` {
		t.Fatalf("Domainlerimi Tara: %d %s", rr.Code, rr.Body)
	}
	if is, ok := k.sonrakiIs(); !ok || is.tam || !reflect.DeepEqual(is.ids, []int64{10}) {
		t.Fatalf("yalnız bayinin domaini (10) kuyruğa girmeli: %+v", is)
	}
	var eylem, hedef string
	var kapsamID, aktor int64
	if err := db.QueryRow(`SELECT action, target, reseller_id, actor_user_id FROM audit_log`).
		Scan(&eylem, &hedef, &kapsamID, &aktor); err != nil || eylem != "websec.tara" || kapsamID != 7 || aktor != 7 {
		t.Fatalf("denetim kaydı: %v %s %s kapsam=%d aktor=%d", err, eylem, hedef, kapsamID, aktor)
	}

	// Tek domain: kendi domaini (bayi 8 → 11) kuyruğa, denetim domainin sahibiyle.
	kimlikKur(t, &auth.Claims{UserID: 8, Username: "bayi8", Role: "reseller", ResellerID: 8})
	rr = istek(t, h.Rescan, "POST", "/websec/rescan?domain_id=11", "")
	if rr.Code != 202 || strings.TrimSpace(rr.Body.String()) != `{"sirada":1}` {
		t.Fatalf("kendi domaini: %d %s", rr.Code, rr.Body)
	}
	if err := db.QueryRow(`SELECT target, reseller_id FROM audit_log ORDER BY id DESC LIMIT 1`).
		Scan(&hedef, &kapsamID); err != nil || hedef != "onbir.test" || kapsamID != 8 {
		t.Fatalf("tek domain denetimi: %v %s %d", err, hedef, kapsamID)
	}
}

func wsDenetimSayisi(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_log`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
