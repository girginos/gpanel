package bildirim

// bildirim_test.go — okundu durumunun GÖRÜNTÜLEYENE göre tutulması (B3),
// "Ana hesap"a devirde bildirimlerin yöneticiden kaybolmaması (B4) ve
// 0105 göçünün güvenli/idempotent taşıması.
//
// Gerçek SQL davranışı test edilir (MariaDB). DB yoksa testler ATLANIR.
//
// Çalıştırma: DSN'deki kullanıcı veritabanı oluşturup silebilmeli. Test YALNIZ
// kendi açtığı gosp_test_bildirim_<zaman> veritabanını kullanır ve sonunda
// DROP eder; DSN'deki veritabanı adı yok sayılır, başka veritabanına dokunmaz.
// Yine de canlı panel sunucusunu GÖSTERME — geçici bir örnek kullan:
//
//	GOSP_TEST_GECICI_DSN='root@unix(/tmp/gecici-db/sock)/' go test ./internal/bildirim/

import (
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

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/auth"
	"girginospanel/internal/middleware"
)

var bt_sir = []byte("bildirim-test-anahtari-0123456789abcdef")

const bt_goc = "../../migrations/0105_bildirim_okuyan.sql"

func bt_geciciDB(t *testing.T) *sql.DB {
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
	ad := fmt.Sprintf("gosp_test_bildirim_%d", time.Now().UnixNano())
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
	bt_exec(t, db, `CREATE TABLE domains (
		id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
		reseller_id BIGINT UNSIGNED NOT NULL DEFAULT 0
	) ENGINE=InnoDB`)
	// cp_bildirim: 0080 + 0082 göçlerinin ürettiği şemanın aynısı.
	bt_exec(t, db, `CREATE TABLE cp_bildirim (
		id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		seviye     VARCHAR(16)  NOT NULL DEFAULT 'bilgi',
		kategori   VARCHAR(32)  NOT NULL DEFAULT '',
		baslik     VARCHAR(200) NOT NULL,
		mesaj      TEXT         NOT NULL,
		domain_id  BIGINT UNSIGNED NULL,
		reseller_id BIGINT UNSIGNED NULL,
		ref_tur    VARCHAR(24)  NOT NULL DEFAULT '',
		ref_id     BIGINT       NOT NULL DEFAULT 0,
		okundu     TINYINT(1)   NOT NULL DEFAULT 0,
		created_at TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
		KEY idx_bildirim_okundu (okundu),
		KEY idx_bildirim_domain (domain_id),
		KEY idx_bildirim_reseller (reseller_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`)
	return db
}

func bt_exec(t *testing.T, db *sql.DB, q string, args ...any) sql.Result {
	t.Helper()
	res, err := db.Exec(q, args...)
	if err != nil {
		t.Fatalf("SQL hatası: %v\n%s", err, q)
	}
	return res
}

// bt_gocUygula — cmd/server runMigrations ile AYNI ayrıştırma: "--" ile başlayan
// satırlar atılır, ";" ile bölünür; "zaten var" türü hatalar zararsız sayılır.
func bt_gocUygula(t *testing.T, db *sql.DB) {
	t.Helper()
	body, err := os.ReadFile(bt_goc)
	if err != nil {
		t.Fatalf("göç dosyası okunamadı (%s): %v", bt_goc, err)
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
			t.Fatalf("GÖÇ HATASI: %v\n%s", err, s)
		}
	}
}

// bt_semaHazir — test şeması + 0105 göçü (varsa). Tabanda (göç yokken) yalnız
// eski şema kurulur; böylece aynı testler eski davranışı da ölçebilir.
func bt_semaHazir(t *testing.T) *sql.DB {
	t.Helper()
	db := bt_geciciDB(t)
	if _, err := os.Stat(bt_goc); err == nil {
		bt_gocUygula(t, db)
	} else {
		t.Logf("NOT: %s yok — eski şemayla koşuluyor", bt_goc)
	}
	return db
}

func bt_router(h *Handlers) http.Handler {
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(bt_sir))
		r.Get("/bildirimler", h.Liste)
		r.Post("/bildirimler/{id}/okundu", h.Okundu)
	})
	return r
}

func bt_admin(t *testing.T) string {
	t.Helper()
	tok, err := auth.IssueAt(bt_sir, 3600, 1, "root", "admin", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func bt_bayi(t *testing.T, rid int64) string {
	t.Helper()
	tok, err := auth.IssueResellerAt(bt_sir, 3600, rid, fmt.Sprintf("bayi%d", rid), rid, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func bt_musteri(t *testing.T, did int64) string {
	t.Helper()
	tok, _, err := auth.GenerateMusteri(bt_sir, auth.MusteriClaims{
		FTPHesapID: did * 10, DomainID: did, Kullanici: "m", AlanAdi: "ornek.test",
	}, 3600)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

type bt_yanit struct {
	Bildirimler []Bildirim `json:"bildirimler"`
	Okunmamis   int        `json:"okunmamis"`
}

func bt_liste(t *testing.T, h *Handlers, tok, sorgu string) bt_yanit {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/bildirimler"+sorgu, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	bt_router(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /bildirimler%s: %d %s", sorgu, rec.Code, rec.Body.String())
	}
	var y bt_yanit
	if err := json.Unmarshal(rec.Body.Bytes(), &y); err != nil {
		t.Fatalf("yanıt çözülemedi: %v (%s)", err, rec.Body.String())
	}
	return y
}

func bt_okundu(t *testing.T, h *Handlers, tok string, id int64) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/bildirimler/%d/okundu", id), nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	bt_router(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /bildirimler/%d/okundu: %d %s", id, rec.Code, rec.Body.String())
	}
}

func bt_sayi(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("sayım: %v (%s)", err, q)
	}
	return n
}

func bt_idler(y bt_yanit) map[int64]bool {
	m := map[int64]bool{}
	for _, b := range y.Bildirimler {
		m[b.ID] = b.Okundu
	}
	return m
}

// 🔴 B3: müşteri "tümünü okundu" yapınca BAYİNİN kritik rozetleri düşmemeli;
// bayinin okuması da müşterininkini değiştirmemeli.
func TestOkundu_MusteriTumunuOkuyuncaBayiRozetiDusmez(t *testing.T) {
	db := bt_semaHazir(t)
	h := &Handlers{DB: db}
	bt_exec(t, db, `INSERT INTO domains (id, reseller_id) VALUES (10, 7)`)
	Yaz(db, "kritik", "yedek", "Yedek başarısız", "ornek.test yedeği alınamadı", 10, "backup", 0)
	Yaz(db, "kritik", "zincir", "Saldırı zinciri tespit edildi", "ornek.test", 10, "av_zincir", 3)

	bayi, musteri := bt_bayi(t, 7), bt_musteri(t, 10)
	if y := bt_liste(t, h, bayi, ""); y.Okunmamis != 2 || len(y.Bildirimler) != 2 {
		t.Fatalf("ön koşul: bayi 2 okunmamış görmeli, görülen=%d/%d", y.Okunmamis, len(y.Bildirimler))
	}
	if y := bt_liste(t, h, musteri, ""); y.Okunmamis != 2 {
		t.Fatalf("ön koşul: müşteri 2 okunmamış görmeli, görülen=%d", y.Okunmamis)
	}

	bt_okundu(t, h, musteri, 0) // müşteri: tümünü okundu

	if y := bt_liste(t, h, musteri, ""); y.Okunmamis != 0 {
		t.Fatalf("müşteri kendi 'tümünü okundu'sundan sonra 0 okunmamış görmeli, görülen=%d", y.Okunmamis)
	} else {
		for _, b := range y.Bildirimler {
			if !b.Okundu {
				t.Fatalf("müşteri listesinde okunmamış kaldı: %+v", b)
			}
		}
	}
	y := bt_liste(t, h, bayi, "")
	if y.Okunmamis != 2 {
		t.Fatalf("B3: müşterinin okuması BAYİNİN rozetini düşürdü — bayi okunmamış=%d (beklenen 2)", y.Okunmamis)
	}
	for _, b := range y.Bildirimler {
		if b.Okundu {
			t.Fatalf("B3: bayi listesinde müşterinin okuduğu bildirim 'okundu' görünüyor: %q", b.Baslik)
		}
	}
	if y := bt_liste(t, h, bayi, "?sadece_okunmamis=1"); len(y.Bildirimler) != 2 {
		t.Fatalf("B3: bayinin sadece_okunmamis listesi 2 olmalı, görülen=%d", len(y.Bildirimler))
	}

	// Ters yön: bayi TEK bildirimi okur → yalnız kendisi için.
	bt_exec(t, db, `INSERT INTO cp_bildirim (seviye,kategori,baslik,mesaj,domain_id,reseller_id) VALUES ('uyari','yedek','Yeni','m',10,7)`)
	var yeni int64
	if err := db.QueryRow(`SELECT MAX(id) FROM cp_bildirim`).Scan(&yeni); err != nil {
		t.Fatal(err)
	}
	bt_okundu(t, h, bayi, yeni)
	if !bt_idler(bt_liste(t, h, bayi, ""))[yeni] {
		t.Fatalf("bayi okuduğu bildirimi okundu görmeli")
	}
	if my := bt_liste(t, h, musteri, ""); my.Okunmamis != 1 || bt_idler(my)[yeni] {
		t.Fatalf("B3: bayinin tekil okuması müşteriyi etkiledi — müşteri okunmamış=%d", my.Okunmamis)
	}
}

// 🔴 B3: yönetici sahipli domain — müşteri okuması yöneticinin rozetini düşürmez;
// yöneticinin "tümünü okundu"su da müşteriyi etkilemez.
func TestOkundu_YoneticiSahipliDomain(t *testing.T) {
	db := bt_semaHazir(t)
	h := &Handlers{DB: db}
	bt_exec(t, db, `INSERT INTO domains (id, reseller_id) VALUES (20, 0), (21, 0)`)
	Yaz(db, "kritik", "yedek", "Yedek başarısız", "a", 20, "backup", 0)
	Yaz(db, "kritik", "yedek", "Yedek başarısız", "b", 21, "backup", 0)
	Yaz(db, "kritik", "sistem", "Swap alani yok", "panel geneli", 0, "", 0)

	admin, m20, m21 := bt_admin(t), bt_musteri(t, 20), bt_musteri(t, 21)
	if y := bt_liste(t, h, admin, ""); y.Okunmamis != 3 {
		t.Fatalf("ön koşul: yönetici 3 okunmamış görmeli, görülen=%d", y.Okunmamis)
	}
	bt_okundu(t, h, m20, 0)
	if y := bt_liste(t, h, admin, ""); y.Okunmamis != 3 {
		t.Fatalf("B3: müşterinin okuması YÖNETİCİNİN rozetini düşürdü — okunmamış=%d (beklenen 3)", y.Okunmamis)
	}
	bt_okundu(t, h, admin, 0)
	if y := bt_liste(t, h, admin, ""); y.Okunmamis != 0 {
		t.Fatalf("yönetici tümünü okundu sonrası 0 beklenir, görülen=%d", y.Okunmamis)
	}
	if y := bt_liste(t, h, m21, ""); y.Okunmamis != 1 {
		t.Fatalf("B3: yöneticinin okuması müşteri 21'in rozetini düşürdü — okunmamış=%d", y.Okunmamis)
	}
	// Panel geneli bildirim müşteriye hiç görünmez (izolasyon korunur).
	for _, b := range bt_liste(t, h, m21, "").Bildirimler {
		if b.DomainID == nil || *b.DomainID != 21 {
			t.Fatalf("müşteri 21 başka kapsamdan bildirim gördü: %+v", b)
		}
	}
}

// Kapsam izolasyonu: başka bayi/müşteri görmez; başka kapsamın bildirimini
// okundu yapamaz (tekil id ile de).
func TestOkundu_KapsamDisiEtkisiz(t *testing.T) {
	db := bt_semaHazir(t)
	h := &Handlers{DB: db}
	bt_exec(t, db, `INSERT INTO domains (id, reseller_id) VALUES (10, 7), (11, 8)`)
	Yaz(db, "kritik", "yedek", "Yedek başarısız", "x", 10, "backup", 0)
	var id int64
	if err := db.QueryRow(`SELECT id FROM cp_bildirim WHERE domain_id=10`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	baskaBayi, baskaMusteri := bt_bayi(t, 8), bt_musteri(t, 11)
	if y := bt_liste(t, h, baskaBayi, ""); len(y.Bildirimler) != 0 || y.Okunmamis != 0 {
		t.Fatalf("başka bayi başka kiracının bildirimini gördü: %+v", y)
	}
	if y := bt_liste(t, h, baskaMusteri, ""); len(y.Bildirimler) != 0 {
		t.Fatalf("başka müşteri başka domainin bildirimini gördü: %+v", y)
	}
	bt_okundu(t, h, baskaBayi, id)
	bt_okundu(t, h, baskaMusteri, id)
	bt_okundu(t, h, baskaBayi, 0)
	bt_okundu(t, h, baskaMusteri, 0)
	if y := bt_liste(t, h, bt_bayi(t, 7), ""); y.Okunmamis != 1 {
		t.Fatalf("kapsam dışı okundu çağrısı sahibin rozetini düşürdü: %d", y.Okunmamis)
	}
	if y := bt_liste(t, h, bt_musteri(t, 10), ""); y.Okunmamis != 1 {
		t.Fatalf("kapsam dışı okundu çağrısı müşterinin rozetini düşürdü: %d", y.Okunmamis)
	}
}

// Tekilleştirme görüntüleyene göre: herkes aynı bildirimi hâlâ okunmamış
// tutuyorsa yeni satır yok; biri okuduysa ve durum sürüyorsa YALNIZ o yeniden
// uyarılır, okumayanın rozetine ikinci kopya binmez.
func TestYaz_TekillestirmeGoruntuleyeneGore(t *testing.T) {
	db := bt_semaHazir(t)
	h := &Handlers{DB: db}
	bt_exec(t, db, `INSERT INTO domains (id, reseller_id) VALUES (10, 7)`)
	bayi, musteri := bt_bayi(t, 7), bt_musteri(t, 10)
	yaz := func() { Yaz(db, "kritik", "yedek", "Yedek başarısız", "d", 10, "backup", 0) }
	satir := func() int { return bt_sayi(t, db, `SELECT COUNT(*) FROM cp_bildirim`) }

	yaz()
	yaz()
	if n := satir(); n != 1 {
		t.Fatalf("birebir tekrar yeni satır üretmemeli, satır=%d", n)
	}
	bt_okundu(t, h, bayi, 0) // yalnız bayi okudu, durum sürüyor
	yaz()
	if n := satir(); n != 2 {
		t.Fatalf("okuyan (bayi) yeniden uyarılmalı → yeni satır beklenir, satır=%d", n)
	}
	if y := bt_liste(t, h, bayi, ""); y.Okunmamis != 1 {
		t.Fatalf("bayi yeniden uyarıldıktan sonra 1 okunmamış görmeli, görülen=%d", y.Okunmamis)
	}
	if y := bt_liste(t, h, musteri, ""); y.Okunmamis != 1 {
		t.Fatalf("okumayan müşterinin rozetine ikinci kopya bindi: okunmamış=%d (beklenen 1)", y.Okunmamis)
	}
	yaz()
	if n := satir(); n != 2 {
		t.Fatalf("herkes okunmamış tutarken yeni satır yazılmamalı, satır=%d", n)
	}
	bt_okundu(t, h, musteri, 0) // şimdi müşteri okudu
	yaz()
	if n := satir(); n != 3 {
		t.Fatalf("okuyan (müşteri) yeniden uyarılmalı, satır=%d", n)
	}
	if y := bt_liste(t, h, musteri, ""); y.Okunmamis != 1 {
		t.Fatalf("müşteri 1 okunmamış görmeli, görülen=%d", y.Okunmamis)
	}
	if y := bt_liste(t, h, bayi, ""); y.Okunmamis != 1 {
		t.Fatalf("bayinin rozetine ikinci kopya bindi: %d", y.Okunmamis)
	}

	// Panel geneli (yalnız yönetici görür) — eski davranış korunur.
	admin := bt_admin(t)
	Yaz(db, "kritik", "sistem", "Swap alani yok", "s", 0, "", 0)
	Yaz(db, "kritik", "sistem", "Swap alani yok", "s", 0, "", 0)
	if n := bt_sayi(t, db, `SELECT COUNT(*) FROM cp_bildirim WHERE domain_id IS NULL`); n != 1 {
		t.Fatalf("panel geneli tekrar tekilleşmeli, satır=%d", n)
	}
	bt_okundu(t, h, admin, 0)
	Yaz(db, "kritik", "sistem", "Swap alani yok", "s", 0, "", 0)
	if y := bt_liste(t, h, admin, ""); y.Okunmamis != 1 {
		t.Fatalf("yönetici okuduktan sonra durum sürerse yeniden uyarılmalı, okunmamış=%d", y.Okunmamis)
	}
}

// 🔴 B4: "Ana hesap (admin)"e devirde açık bildirimler yöneticiye geçmeli
// (reseller_id=0 değil NULL), eski bayiden çıkmalı; tekilleştirme de artık
// görünmeyen bir satır yüzünden yöneticiyi susturmamalı.
func TestSahipDevri_AnaHesabaDevirYoneticiGorur(t *testing.T) {
	db := bt_semaHazir(t)
	h := &Handlers{DB: db}
	ctx := context.Background()
	bt_exec(t, db, `INSERT INTO domains (id, reseller_id) VALUES (30, 7)`)
	Yaz(db, "kritik", "yedek", "Yedek başarısız", "d", 30, "backup", 0)
	Yaz(db, "kritik", "zincir", "Saldırı zinciri tespit edildi", "z", 30, "av_zincir", 5)
	admin, bayi := bt_admin(t), bt_bayi(t, 7)

	// Bayinin OKUDUĞU bildirim onun döneminin geçmişidir → bayide kalır.
	var zincirID int64
	if err := db.QueryRow(`SELECT id FROM cp_bildirim WHERE kategori='zincir'`).Scan(&zincirID); err != nil {
		t.Fatal(err)
	}
	bt_okundu(t, h, bayi, zincirID)

	// devret() ile aynı sıra: önce domain sahipliği, sonra bildirim taşıma (tx içinde).
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE domains SET reseller_id=0 WHERE id=30`); err != nil {
		t.Fatal(err)
	}
	if err := SahipDevri(ctx, tx, 30, 7, 0); err != nil {
		t.Fatalf("SahipDevri: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if n := bt_sayi(t, db, `SELECT COUNT(*) FROM cp_bildirim WHERE reseller_id=0`); n != 0 {
		t.Fatalf("B4: devir reseller_id=0 yazdı (%d satır) — 'Ana hesap' NULL olmalı", n)
	}
	ay := bt_liste(t, h, admin, "")
	if ay.Okunmamis != 1 || len(ay.Bildirimler) != 1 || ay.Bildirimler[0].Kategori != "yedek" {
		t.Fatalf("B4: yönetici devrettiği domainin açık bildirimini görmeli (1 yedek), görülen=%+v", ay)
	}
	by := bt_liste(t, h, bayi, "")
	if len(by.Bildirimler) != 1 || by.Bildirimler[0].ID != zincirID || !by.Bildirimler[0].Okundu {
		t.Fatalf("eski bayide yalnız OKUDUĞU geçmiş kalmalı, görülen=%+v", by)
	}

	// Tekilleştirme: yönetici görüyor ve okumadı → yeni kopya yok.
	Yaz(db, "kritik", "yedek", "Yedek başarısız", "d", 30, "backup", 0)
	if n := bt_sayi(t, db, `SELECT COUNT(*) FROM cp_bildirim WHERE kategori='yedek'`); n != 1 {
		t.Fatalf("yönetici okunmamış tutarken kopya yazılmamalı, satır=%d", n)
	}
	// Yönetici okudu, durum sürüyor → yöneticiye yeniden düşmeli (müşteri okumasa da).
	bt_okundu(t, h, admin, 0)
	Yaz(db, "kritik", "yedek", "Yedek başarısız", "d", 30, "backup", 0)
	if y := bt_liste(t, h, admin, ""); y.Okunmamis != 1 {
		t.Fatalf("B4: yönetici okuduktan sonra tekrarlayan 'Yedek başarısız' yeniden düşmeli, okunmamış=%d", y.Okunmamis)
	}
}

// Devirde ölçüt ESKİ SAHİBİN okuması: müşteri okumuş ama bayi okumamışsa bildirim
// açıktır ve yeni sahibe geçer. Bayiden bayiye devir de çalışır.
func TestSahipDevri_BayidenBayiye(t *testing.T) {
	db := bt_semaHazir(t)
	h := &Handlers{DB: db}
	ctx := context.Background()
	bt_exec(t, db, `INSERT INTO domains (id, reseller_id) VALUES (40, 7)`)
	Yaz(db, "kritik", "yedek", "Yedek başarısız", "d", 40, "backup", 0)
	bt_okundu(t, h, bt_musteri(t, 40), 0) // yalnız müşteri okudu

	bt_exec(t, db, `UPDATE domains SET reseller_id=9 WHERE id=40`)
	if err := SahipDevri(ctx, db, 40, 7, 9); err != nil {
		t.Fatalf("SahipDevri: %v", err)
	}
	if y := bt_liste(t, h, bt_bayi(t, 9), ""); y.Okunmamis != 1 {
		t.Fatalf("yeni bayi, eski bayinin okumadığı bildirimi almalı; okunmamış=%d", y.Okunmamis)
	}
	if y := bt_liste(t, h, bt_bayi(t, 7), ""); len(y.Bildirimler) != 0 {
		t.Fatalf("eski bayi devredilen açık bildirimi görmeye devam ediyor: %+v", y)
	}
}

// Eski sürümün yazdığı reseller_id=0 satırı (göç öncesi/başka yazıcı) yöneticiye
// görünür — kapsam 0'ı da "ana hesap" sayar.
func TestKapsam_EskiSifirSatiriYoneticiyeGorunur(t *testing.T) {
	db := bt_semaHazir(t)
	h := &Handlers{DB: db}
	bt_exec(t, db, `INSERT INTO cp_bildirim (seviye,kategori,baslik,mesaj,domain_id,reseller_id) VALUES ('kritik','yedek','Eski','m',50,0)`)
	if y := bt_liste(t, h, bt_admin(t), ""); y.Okunmamis != 1 || len(y.Bildirimler) != 1 {
		t.Fatalf("reseller_id=0 (ana hesap) satırı yöneticiye görünmeli, görülen=%+v", y)
	}
	if y := bt_liste(t, h, bt_bayi(t, 7), ""); len(y.Bildirimler) != 0 {
		t.Fatalf("reseller_id=0 satırı bayiye görünmemeli: %+v", y)
	}
}

// Göç uygulanmamışsa (cp_bildirim_okundu yok) Yaz eski ortak sütunla
// tekilleştirir — bildirim merkezi her turda spam'lenmez.
func TestYaz_GocYokkenEskiTekillestirme(t *testing.T) {
	db := bt_geciciDB(t) // 0105 UYGULANMADI
	bt_exec(t, db, `INSERT INTO domains (id, reseller_id) VALUES (10, 7)`)
	for i := 0; i < 3; i++ {
		Yaz(db, "kritik", "yedek", "Yedek başarısız", "d", 10, "backup", 0)
		Yaz(db, "kritik", "sistem", "Swap alani yok", "s", 0, "", 0)
	}
	if n := bt_sayi(t, db, `SELECT COUNT(*) FROM cp_bildirim`); n != 2 {
		t.Fatalf("göç yokken birebir tekrar tekilleşmeli: satır=%d (beklenen 2)", n)
	}
}

// 0105 göçü: mevcut okundu=1 → o satırı BUGÜN gören herkes için okundu (görünen
// durum aynen korunur); reseller_id=0 → NULL; yeniden koşmak (defter devre dışı)
// yeni okumaların üstüne eski ortak bayrağı YAYMAZ.
func TestGoc0105_TasimaVeIdempotent(t *testing.T) {
	if _, err := os.Stat(bt_goc); err != nil {
		t.Skipf("%s yok", bt_goc)
	}
	db := bt_geciciDB(t) // göç ÖNCESİ şema
	h := &Handlers{DB: db}
	bt_exec(t, db, `INSERT INTO domains (id, reseller_id) VALUES (10, 7), (20, 0)`)
	ekle := func(dom any, rid any, okundu int, baslik string) int64 {
		res := bt_exec(t, db, `INSERT INTO cp_bildirim (seviye,kategori,baslik,mesaj,domain_id,reseller_id,okundu)
			VALUES ('kritik','yedek',?,'m',?,?,?)`, baslik, dom, rid, okundu)
		id, _ := res.LastInsertId()
		return id
	}
	bayiOkundu := ekle(10, 7, 1, "bayi-okundu")
	bayiAcik := ekle(10, 7, 0, "bayi-acik")
	yonOkundu := ekle(20, nil, 1, "yon-okundu")
	panelOkundu := ekle(nil, nil, 1, "panel-okundu")
	sifirAcik := ekle(20, 0, 0, "sifir-acik")

	bt_gocUygula(t, db)

	isaret := func(id int64, tur string, oid int64) bool {
		return bt_sayi(t, db, `SELECT COUNT(*) FROM cp_bildirim_okundu WHERE bildirim_id=? AND okuyan_tur=? AND okuyan_id=?`, id, tur, oid) == 1
	}
	if !isaret(bayiOkundu, "bayi", 7) || !isaret(bayiOkundu, "musteri", 10) {
		t.Fatalf("okundu=1 bayi satırı bayi+müşteri için okundu taşınmalı")
	}
	if !isaret(yonOkundu, "admin", 1) || !isaret(yonOkundu, "musteri", 20) {
		t.Fatalf("okundu=1 yönetici sahipli satır yönetici+müşteri için okundu taşınmalı")
	}
	if !isaret(panelOkundu, "admin", 1) {
		t.Fatalf("okundu=1 panel geneli satır yönetici için okundu taşınmalı")
	}
	if n := bt_sayi(t, db, `SELECT COUNT(*) FROM cp_bildirim_okundu WHERE bildirim_id IN (?,?)`, bayiAcik, sifirAcik); n != 0 {
		t.Fatalf("okunmamış satırlara okundu işareti yazılmamalı (%d)", n)
	}
	if n := bt_sayi(t, db, `SELECT COUNT(*) FROM cp_bildirim WHERE reseller_id=0`); n != 0 {
		t.Fatalf("reseller_id=0 satırı NULL'a çekilmeli (%d kaldı)", n)
	}
	if n := bt_sayi(t, db, `SELECT COUNT(*) FROM cp_bildirim_okundu`); n != 5 {
		t.Fatalf("beklenen 5 taşıma işareti, bulunan=%d", n)
	}
	// Görünen durum göçten önceki ile AYNI (kimse eski bildirimleri yeniden okunmamış görmez).
	if y := bt_liste(t, h, bt_bayi(t, 7), ""); y.Okunmamis != 1 {
		t.Fatalf("göç sonrası bayi okunmamış=1 beklenir, görülen=%d", y.Okunmamis)
	}
	if y := bt_liste(t, h, bt_admin(t), ""); y.Okunmamis != 1 { // sifir-acik artık yöneticide
		t.Fatalf("göç sonrası yönetici okunmamış=1 beklenir, görülen=%d", y.Okunmamis)
	}

	// Yeni düzende müşteri bayi-acik'i okur (eski ortak bayrak da 1 olur).
	bt_okundu(t, h, bt_musteri(t, 10), bayiAcik)
	// Defter devre dışıyken göç her açılışta yeniden koşar: bayiye yayılmamalı.
	bt_gocUygula(t, db)
	if isaret(bayiAcik, "bayi", 7) {
		t.Fatalf("göç yeniden koşunca müşterinin okuması bayiye yayıldı (B3 geri geldi)")
	}
	if n := bt_sayi(t, db, `SELECT COUNT(*) FROM cp_bildirim_okundu`); n != 6 {
		t.Fatalf("yeniden koşma işaret sayısını değiştirmemeli: %d (beklenen 6)", n)
	}
	if y := bt_liste(t, h, bt_bayi(t, 7), ""); y.Okunmamis != 1 {
		t.Fatalf("bayi hâlâ 1 okunmamış görmeli, görülen=%d", y.Okunmamis)
	}
}
