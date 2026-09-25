package kaynak

// canli_test.go — GET /domains/{id}/canli.
//
// cgroup okuması geçici dizindeki SAHTE ağaca karşı sınanır (cgroup.Kok çevrilir);
// gerçek /sys/fs/cgroup'a dokunulmaz. Yetki testleri gerçek RequireAuth +
// MusteriScope zinciriyle koşar; bayi sahipliği ve oturum geçerliliği gerçek
// sorgularla okunsun diye DB'li kısım GOSP_TEST_GECICI_DSN ister (yoksa atlanır)
// ve yalnız kendi açtığı gosp_test_kaynak_<zaman> veritabanını kullanıp DROP eder.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/auth"
	"girginospanel/internal/cgroup"
	"girginospanel/internal/middleware"
)

var kt_sir = []byte("kaynak-canli-test-anahtari-0123456789ab")

// kt_slice: sahte kiracı slice dizini (181'deki girginos-c_test_com.slice'tan alınmış değerler).
func kt_slice(t *testing.T, kok, sk string, dosyalar map[string]string) string {
	t.Helper()
	d := filepath.Join(kok, "girginos.slice", "girginos-"+sk+".slice")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	for ad, icerik := range dosyalar {
		if err := os.WriteFile(filepath.Join(d, ad), []byte(icerik), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func kt_tamSlice(cpuUsec, rbytes, wbytes int64) map[string]string {
	return map[string]string{
		"cpu.stat":       fmt.Sprintf("usage_usec %d\nuser_usec 1\nsystem_usec 1\n", cpuUsec),
		"cpu.max":        "50000 100000\n",
		"memory.current": "59281408\n",
		"memory.max":     "268435456\n",
		"io.stat":        fmt.Sprintf("252:0 rbytes=%d wbytes=%d rios=5284 wios=18505 dbytes=0 dios=0\n", rbytes, wbytes),
		"pids.current":   "28\n",
	}
}

func kt_sahteKok(t *testing.T) string {
	t.Helper()
	eski := cgroup.Kok
	t.Cleanup(func() { cgroup.Kok = eski })
	cgroup.Kok = t.TempDir()
	return cgroup.Kok
}

func kt_json(t *testing.T, c Canli) map[string]any {
	t.Helper()
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// Sözleşme: alan adları ve null/0 biçimi AYNEN (ön yüz buna göre yazılıyor).
func TestCanli_JSONSozlesmesi(t *testing.T) {
	m := kt_json(t, Canli{Zaman: 1})
	var anahtarlar []string
	for k := range m {
		anahtarlar = append(anahtarlar, k)
	}
	sort.Strings(anahtarlar)
	if g := strings.Join(anahtarlar, ","); g != "bellek_b,bellek_limit_b,cpu_limit_yuzde,cpu_yuzde,destek,io_okuma_bps,io_yazma_bps,surec,zaman" {
		t.Fatalf("anahtar kümesi sözleşmeden farklı: %s", g)
	}
	for _, k := range []string{"cpu_yuzde", "io_okuma_bps", "io_yazma_bps"} {
		if v, ok := m[k]; !ok || v != nil {
			t.Errorf("%s bilinmiyorken null olmalı (anahtar var): %v", k, v)
		}
	}
}

func TestCanliOku_SahteAgac(t *testing.T) {
	kok := t.TempDir()
	t0 := time.Unix(1_790_000_000, 0)

	t.Run("ilk örnek null, sonraki delta", func(t *testing.T) {
		o := &cgroup.Ornekleyici{EnAz: 500 * time.Millisecond, EnCok: 2 * time.Minute}
		d := kt_slice(t, kok, "c_delta", kt_tamSlice(550_861_904, 163_315_712, 75_796_480))
		c := canliOku(o, d, t0)
		if !c.Destek || c.CPUYuzde != nil || c.IOOkumaBps != nil || c.IOYazmaBps != nil {
			t.Fatalf("ilk örnek: destek=true ve hızlar null beklenir: %+v", c)
		}
		if c.BellekB != 59281408 || c.BellekLimitB != 268435456 || c.CPULimitYuzde != 50 || c.Surec != 28 || c.Zaman != t0.Unix() {
			t.Fatalf("anlık değerler yanlış: %+v", c)
		}
		// 10 sn sonra: +2.5 sn CPU (%25), +1 MB okuma, +512 KB yazma.
		kt_slice(t, kok, "c_delta", kt_tamSlice(553_361_904, 163_315_712+1<<20, 75_796_480+1<<19))
		c = canliOku(o, d, t0.Add(10*time.Second))
		if c.CPUYuzde == nil || *c.CPUYuzde != 25 {
			t.Fatalf("cpu_yuzde 25 beklenir: %v", c.CPUYuzde)
		}
		if c.IOOkumaBps == nil || *c.IOOkumaBps != 104858 || c.IOYazmaBps == nil || *c.IOYazmaBps != 52429 {
			t.Fatalf("io bps (104858, 52429) beklenir: %v %v", c.IOOkumaBps, c.IOYazmaBps)
		}
	})

	t.Run("max → 0", func(t *testing.T) {
		o := &cgroup.Ornekleyici{EnAz: 500 * time.Millisecond}
		f := kt_tamSlice(1, 0, 0)
		f["memory.max"] = "max\n"
		f["cpu.max"] = "max 100000\n"
		c := canliOku(o, kt_slice(t, kok, "c_sinirsiz", f), t0)
		b, _ := json.Marshal(c)
		// "-0" da yakalansın: -1 kota yuvarlamayla -0'a iner, == 0 onu ayırt etmez.
		if !c.Destek || !strings.Contains(string(b), `"cpu_limit_yuzde":0,`) || !strings.Contains(string(b), `"bellek_limit_b":0,`) {
			t.Fatalf("sınırsız limitler 0 beklenir (-1/-0 değil): %s", b)
		}
	})

	t.Run("io.stat yok → yalnız G/Ç null", func(t *testing.T) {
		o := &cgroup.Ornekleyici{EnAz: 500 * time.Millisecond}
		f := kt_tamSlice(1_000_000, 0, 0)
		delete(f, "io.stat")
		d := kt_slice(t, kok, "c_iosuz", f)
		canliOku(o, d, t0)
		kt_slice(t, kok, "c_iosuz", map[string]string{"cpu.stat": "usage_usec 2000000\n"})
		c := canliOku(o, d, t0.Add(10*time.Second))
		if !c.Destek || c.CPUYuzde == nil || *c.CPUYuzde != 10 || c.IOOkumaBps != nil || c.IOYazmaBps != nil {
			t.Fatalf("CPU %%10 + G/Ç null beklenir: %+v", c)
		}
	})

	for _, tc := range []struct {
		ad  string
		sil string
	}{
		{"slice dizini yok", "*"},
		{"cpu.stat yok", "cpu.stat"},
		{"memory.current yok", "memory.current"},
	} {
		t.Run("dosya yok → destek:false: "+tc.ad, func(t *testing.T) {
			o := &cgroup.Ornekleyici{EnAz: 500 * time.Millisecond}
			sk := "c_yok_" + strings.ReplaceAll(strings.ReplaceAll(tc.sil, ".", "_"), "*", "dizin")
			d := filepath.Join(kok, "girginos.slice", "girginos-"+sk+".slice")
			if tc.sil != "*" {
				f := kt_tamSlice(1, 1, 1)
				delete(f, tc.sil)
				d = kt_slice(t, kok, sk, f)
			}
			m := kt_json(t, canliOku(o, d, t0))
			if m["destek"] != false || m["bellek_b"] != float64(0) || m["bellek_limit_b"] != float64(0) ||
				m["cpu_limit_yuzde"] != float64(0) || m["surec"] != float64(0) ||
				m["cpu_yuzde"] != nil || m["io_okuma_bps"] != nil || m["io_yazma_bps"] != nil {
				t.Fatalf("destek=false ve diğer alanlar 0/null beklenir: %v", m)
			}
		})
	}

	t.Run("biçim dışı sk (dizin boş) → destek:false", func(t *testing.T) {
		if c := canliOku(&cgroup.Ornekleyici{}, cgroup.KiraciDizin("../girginos.slice"), t0); c.Destek {
			t.Fatalf("destek=false beklenir: %+v", c)
		}
	})
}

/* ---------------- yetki: gerçek RequireAuth + MusteriScope ---------------- */

func kt_router(h *Handlers) http.Handler {
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(kt_sir))
		r.With(middleware.MusteriScope).Get("/domains/{id}/kaynak", h.Goster)
		r.With(middleware.MusteriScope).Get("/domains/{id}/canli", h.Canli)
	})
	return r
}

func kt_tokenlar(t *testing.T) (admin, bayi, musteri string) {
	t.Helper()
	var err error
	if admin, err = auth.IssueAt(kt_sir, 3600, 1, "root", "admin", 0, ""); err != nil {
		t.Fatal(err)
	}
	if bayi, err = auth.IssueResellerAt(kt_sir, 3600, 7, "bayi", 7, 0, ""); err != nil {
		t.Fatal(err)
	}
	if musteri, _, err = auth.GenerateMusteri(kt_sir, auth.MusteriClaims{
		FTPHesapID: 5, DomainID: 42, Kullanici: "m", AlanAdi: "musteri.test"}, 3600); err != nil {
		t.Fatal(err)
	}
	return
}

func kt_istek(router http.Handler, tok, yol string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, yol, nil)
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// kt_geciciDB: yetki zincirinin (oturum, askı, bayi sahipliği) ve handler'ların
// okuduğu tablolar. middleware.Init ile kapsam DB'si olarak da bağlanır.
//
//	42 musteri.test  sk=c_musteri_test  bayi 7'nin   (müşteri fhid=5)
//	43 kok.test      sk=c_kok_test      ana hesabın (bayi 0)
//	44 goster.test   sk=x-goster        ana hesabın — /kaynak testi için; biçim dışı
//	                 sk: kota/du/crontab hiçbir kiracıya dokunmadan anında düşer
func kt_geciciDB(t *testing.T) *sql.DB {
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
		t.Fatal(err)
	}
	ad := fmt.Sprintf("gosp_test_kaynak_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad); err != nil {
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
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, q := range []string{
		`CREATE TABLE users (id BIGINT UNSIGNED PRIMARY KEY, status VARCHAR(16) NOT NULL DEFAULT 'active',
			token_gecersiz_ts BIGINT NULL, last_activity_ts BIGINT NOT NULL DEFAULT 0)`,
		`CREATE TABLE ftp_accounts (id BIGINT UNSIGNED PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL,
			status VARCHAR(16) NOT NULL DEFAULT 'active', token_gecersiz_ts BIGINT NULL)`,
		`CREATE TABLE domains (id BIGINT UNSIGNED PRIMARY KEY, alan_adi VARCHAR(255) NOT NULL,
			sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '', php_surum VARCHAR(8) NOT NULL DEFAULT '8.3',
			ipv4 VARCHAR(45) NOT NULL DEFAULT '', ssl_aktif TINYINT NOT NULL DEFAULT 0, ssl_bitis DATETIME NULL,
			plan_id BIGINT UNSIGNED NULL, boyut_kb BIGINT NOT NULL DEFAULT 0, trafik_kb BIGINT NOT NULL DEFAULT 0,
			askida TINYINT NULL, reseller_id BIGINT UNSIGNED NOT NULL DEFAULT 0)`,
		`CREATE TABLE service_plans (id BIGINT UNSIGNED PRIMARY KEY, ad VARCHAR(100), disk_kota_mb BIGINT,
			trafik_kota_mb BIGINT, max_domain BIGINT, max_db BIGINT, max_email BIGINT, max_ftp BIGINT)`,
		`CREATE TABLE db_accounts (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL,
			db_name VARCHAR(64) NOT NULL UNIQUE, db_user VARCHAR(64) NOT NULL)`,
		`CREATE TABLE dns_records (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL)`,
		`CREATE TABLE backups (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL,
			boyut_b BIGINT NOT NULL DEFAULT 0)`,
		`INSERT INTO users (id, status) VALUES (1, 'active'), (7, 'active')`,
		`INSERT INTO ftp_accounts (id, domain_id) VALUES (5, 42)`,
		`INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES
			(42, 'musteri.test', 'c_musteri_test', 7), (43, 'kok.test', 'c_kok_test', 0),
			(44, 'goster.test', 'x-goster', 0)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("şema: %v\n%s", err, q)
		}
	}
	middleware.Init(db)
	t.Cleanup(func() { middleware.Init(nil) })
	return db
}

// 🔴 Yetki: müşteri başka domain 403, bayi başkasının domaini 403, yönetici 200.
// Olumlu kontroller (müşteri/bayi kendi domaini 200) 403'lerin "her şeye 403"
// olmadığını kanıtlar.
func TestCanli_Yetki(t *testing.T) {
	db := kt_geciciDB(t)
	kok := kt_sahteKok(t)
	kt_slice(t, kok, "c_musteri_test", kt_tamSlice(1_000_000, 0, 0))
	router := kt_router(&Handlers{DB: db})
	admin, bayi, musteri := kt_tokenlar(t)

	for _, tc := range []struct {
		ad, tok string
		id      int64
		kod     int
	}{
		{"müşteri kendi domaini", musteri, 42, 200},
		{"müşteri başka domain", musteri, 43, 403},
		{"bayi kendi domaini", bayi, 42, 200},
		{"bayi başkasının domaini", bayi, 43, 403},
		{"yönetici bayinin domaini", admin, 42, 200},
		{"yönetici ana hesap domaini", admin, 43, 200},
		{"yönetici olmayan domain", admin, 999, 404},
		{"kimliksiz", "", 42, 401},
	} {
		rec := kt_istek(router, tc.tok, fmt.Sprintf("/domains/%d/canli", tc.id))
		if rec.Code != tc.kod {
			t.Errorf("%s: %d beklenir, gelen %d %s", tc.ad, tc.kod, rec.Code, rec.Body.String())
			continue
		}
		if tc.kod != 200 {
			if strings.Contains(rec.Body.String(), "destek") {
				t.Errorf("%s: reddedilen yanıtta ölçüm var: %s", tc.ad, rec.Body.String())
			}
			continue
		}
		var c map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil {
			t.Fatalf("%s: yanıt çözülemedi: %v", tc.ad, err)
		}
		// 42'nin sahte slice'ı var; 43'ünkü yok → destek:false.
		if want := tc.id == 42; c["destek"] != want {
			t.Errorf("%s: destek=%v beklenir: %v", tc.ad, want, c)
		}
		if tc.id == 42 && c["bellek_b"] != float64(59281408) {
			t.Errorf("%s: bellek_b sahte ağaçtan okunmadı: %v", tc.ad, c)
		}
	}
}

// DB'siz: müşteri başka domainin /canli'sine ulaşamaz; handler hiç çalışmaz
// (DB nil — dokunulsaydı panik).
func TestCanli_MusteriCaprazKiraci403_DBsiz(t *testing.T) {
	_, _, musteri := kt_tokenlar(t)
	rec := kt_istek(kt_router(&Handlers{DB: nil}), musteri, "/domains/99/canli")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("403 beklenir, gelen %d %s", rec.Code, rec.Body.String())
	}
}
