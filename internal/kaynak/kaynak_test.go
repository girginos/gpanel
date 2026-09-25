package kaynak

// kaynak_test.go — /kaynak'ın db_mb ve eposta_olculmuyor alanları.
//
// db_mb üretimde root soketinden (mysql CLI) information_schema'yı okur. Testte
// mysqlKok dikişi GEÇİCİ veritabanı sunucusuna çevrilir. Sorgu gerçek MariaDB
// information_schema'sına karşı koşar, canlı DB'ye dokunulmaz. Açılan
// gosp_test_kaynak_* veritabanları sonunda DROP edilir.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

// kt_boyutDBleri: iki ölçülecek (a ~3 MB, b küçük) ve bir listede OLMAYAN (c) veritabanı.
func kt_boyutDBleri(t *testing.T) (kok *sql.DB, a, b, c string) {
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
	cfg.MultiStatements = true
	kok, err = sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = kok.Close() })
	on := fmt.Sprintf("gosp_test_kaynak_%d", time.Now().UnixNano())
	a, b, c = on+"_a", on+"_b", on+"_c"
	for _, ad := range []string{a, b, c} {
		if _, err := kok.Exec("CREATE DATABASE " + ad); err != nil {
			t.Fatalf("geçici veritabanı: %v", err)
		}
		t.Cleanup(func() { _, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad) })
	}
	for _, q := range []string{
		`CREATE TABLE ` + a + `.t1 (id INT AUTO_INCREMENT PRIMARY KEY, v VARCHAR(1000), KEY ix (v(20)))`,
		`INSERT INTO ` + a + `.t1 (v) SELECT REPEAT('x', 1000) FROM information_schema.columns c1, information_schema.columns c2 LIMIT 3000`,
		`ANALYZE TABLE ` + a + `.t1`,
		`CREATE TABLE ` + b + `.t1 (id INT PRIMARY KEY)`,
		`CREATE TABLE ` + b + `.t2 (id INT PRIMARY KEY, k INT, KEY (k))`,
		`CREATE TABLE ` + c + `.t1 (id INT AUTO_INCREMENT PRIMARY KEY, v VARCHAR(1000))`,
		`INSERT INTO ` + c + `.t1 (v) SELECT REPEAT('y', 1000) FROM information_schema.columns c1, information_schema.columns c2 LIMIT 3000`,
		`ANALYZE TABLE ` + c + `.t1`,
	} {
		rows, err := kok.Query(q)
		if err != nil {
			t.Fatalf("hazırlık: %v\n%s", err, q)
		}
		_ = rows.Close()
	}
	return kok, a, b, c
}

// kt_mysqlKok: üretimdeki `mysql -N -B -e` yerine geçici sunucuda aynı sorgu;
// çıktı CLI'ninki gibi tek satır metin.
func kt_mysqlKok(t *testing.T, kok *sql.DB, cagri *int) {
	t.Helper()
	eski := mysqlKok
	t.Cleanup(func() { mysqlKok = eski })
	mysqlKok = func(ctx context.Context, q string) ([]byte, error) {
		*cagri++
		var s sql.NullString
		if err := kok.QueryRowContext(ctx, q).Scan(&s); err != nil {
			return nil, err
		}
		return []byte(s.String + "\n"), nil
	}
}

func kt_boyutB(t *testing.T, kok *sql.DB, ad string) int64 {
	t.Helper()
	var n int64
	if err := kok.QueryRow(`SELECT COALESCE(SUM(data_length+index_length),0) FROM information_schema.tables WHERE table_schema=?`, ad).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestDBBoyutMB_GeciciDB(t *testing.T) {
	kok, a, b, c := kt_boyutDBleri(t)
	cagri := 0
	kt_mysqlKok(t, kok, &cagri)
	ctx := context.Background()

	aB, bB, cB := kt_boyutB(t, kok, a), kt_boyutB(t, kok, b), kt_boyutB(t, kok, c)
	if aB < 1<<20 || bB <= 0 || cB < 1<<20 {
		t.Fatalf("hazırlık beklenenden küçük: a=%d b=%d c=%d", aB, bB, cB)
	}
	mb := func(n int64) float64 { return math.Round(float64(n)/(1<<20)*100) / 100 }

	if g, ok := dbBoyutMB(ctx, []string{a, b}); !ok || g != mb(aB+bB) {
		t.Fatalf("a+b: %.2f MB beklenir (listede olmayan c hariç), gelen %.2f ok=%v", mb(aB+bB), g, ok)
	}
	if g, ok := dbBoyutMB(ctx, []string{a, "gosp_test_olmayan_db"}); !ok || g != mb(aB) {
		t.Fatalf("olmayan veritabanı 0 katkı: %.2f beklenir, gelen %.2f ok=%v", mb(aB), g, ok)
	}
	if g, ok := dbBoyutMB(ctx, nil); !ok || g != 0 {
		t.Fatalf("veritabanı yok → ölçülmüş 0: gelen %v %v", g, ok)
	}

	// Biçim dışı ad sorguya hiç gömülmez: ölçülemedi, sorgu çalışmaz.
	once := cagri
	for _, kotu := range []string{a + "') OR 1=1 -- ", "a-b", "a b", "", strings.Repeat("a", 65)} {
		if _, ok := dbBoyutMB(ctx, []string{a, kotu}); ok {
			t.Errorf("biçim dışı ad %q ölçülemedi dönmeli", kotu)
		}
	}
	if cagri != once {
		t.Errorf("biçim dışı adla sorgu çalıştırıldı (%d çağrı)", cagri-once)
	}
}

func TestDBBoyutMB_OlculemezseYok(t *testing.T) {
	eski := mysqlKok
	t.Cleanup(func() { mysqlKok = eski })
	for ad, f := range map[string]func(context.Context, string) ([]byte, error){
		"soket yok": func(context.Context, string) ([]byte, error) { return nil, errors.New("exit status 1") },
		"çöp çıktı": func(context.Context, string) ([]byte, error) { return []byte("ERROR 1045\n"), nil },
		"negatif":   func(context.Context, string) ([]byte, error) { return []byte("-5\n"), nil },
		"boş çıktı": func(context.Context, string) ([]byte, error) { return nil, nil },
	} {
		mysqlKok = f
		if _, ok := dbBoyutMB(context.Background(), []string{"c_x_main"}); ok {
			t.Errorf("%s: ok=false beklenir", ad)
		}
	}
}

// Uçtan uca GET /kaynak (yönetici): db_mb gerçek boyut, veritabanı yokken 0,
// ölçülemezse anahtar YOK; eposta_olculmuyor her zaman true.
func TestKaynak_DBMBVeEpostaBayragi(t *testing.T) {
	db := kt_geciciDB(t)
	kok, a, b, _ := kt_boyutDBleri(t)
	cagri := 0
	kt_mysqlKok(t, kok, &cagri)
	router := kt_router(&Handlers{DB: db})
	admin, _, _ := kt_tokenlar(t)

	getir := func() map[string]any {
		t.Helper()
		rec := kt_istek(router, admin, "/domains/44/kaynak")
		if rec.Code != 200 {
			t.Fatalf("GET /kaynak: %d %s", rec.Code, rec.Body.String())
		}
		var m map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}

	if _, err := db.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user) VALUES (44, ?, 'u1'), (44, ?, 'u1')`, a, b); err != nil {
		t.Fatal(err)
	}
	m := getir()
	beklenen := math.Round(float64(kt_boyutB(t, kok, a)+kt_boyutB(t, kok, b))/(1<<20)*100) / 100
	if m["db_mb"] != beklenen {
		t.Fatalf("db_mb=%v beklenir, gelen %v", beklenen, m["db_mb"])
	}
	if ds, _ := m["db_sayisi"].(map[string]any); ds["kullanim"] != float64(2) {
		t.Errorf("db_sayisi.kullanim 2 beklenir: %v", m["db_sayisi"])
	}
	if m["eposta_olculmuyor"] != true {
		t.Errorf("eposta_olculmuyor true beklenir: %v", m["eposta_olculmuyor"])
	}
	if es, _ := m["eposta_sayi"].(map[string]any); es["kullanim"] != float64(0) {
		t.Errorf("eposta_sayi biçimi değişmemeli: %v", m["eposta_sayi"])
	}

	// Ölçülemedi → anahtar yok (ön yüz kartı gizler; 0 "boş veritabanı" sanılırdı).
	mysqlKok = func(context.Context, string) ([]byte, error) { return nil, errors.New("soket yok") }
	if m := getir(); m["db_mb"] != nil {
		t.Fatalf("ölçülemezken db_mb anahtarı olmamalı: %v", m["db_mb"])
	} else if _, var_ := m["db_mb"]; var_ {
		t.Fatal("db_mb anahtarı null olarak bile dönmemeli")
	}

	// Veritabanı yok → ölçülmüş 0 (anahtar var).
	if _, err := db.Exec(`DELETE FROM db_accounts WHERE domain_id=44`); err != nil {
		t.Fatal(err)
	}
	if v, var_ := getir()["db_mb"]; !var_ || v != float64(0) {
		t.Fatalf("veritabanı yokken db_mb=0 beklenir: %v (var=%v)", v, var_)
	}
}
