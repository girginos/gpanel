package hesapaski

// hesapaski_test.go — askı HESAP düzeyindedir: tek yol (Uygula) hesaba bağlı her
// şeyi (ana + alt alan vhost'ları, PHP havuzları, FTP/panel, SSH, DB kullanıcıları,
// cron/süreçler) birlikte kapatır/açar; ilk adım düşerse DB geri alınır, sonraki
// adım düşerse hesap askıda kalır; bayi zinciri aynı yoldan geçer.
//
// Gerçek (geçici) MariaDB: GOSP_TEST_GECICI_DSN (go_test db:true); yoksa ATLANIR.
// Dış etkili adımlar sahte (Adimlar) — ana makineye dokunulmaz.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/hesaplar"
)

func ha_db(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li test atlandı")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || cfg.Net != "unix" || cfg.Addr == "" {
		t.Skip("geçici DSN unix soketi değil")
	}
	switch filepath.Clean(cfg.Addr) {
	case "/var/lib/mysql/mysql.sock", "/run/mysqld/mysqld.sock", "/var/run/mysqld/mysqld.sock", "/tmp/mysql.sock":
		t.Fatalf("GOSP_TEST_GECICI_DSN canlı MariaDB soketini gösteriyor — reddedildi")
	}
	kcfg := cfg.Clone()
	kcfg.DBName = ""
	kok, err := sql.Open("mysql", kcfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = kok.Close() })
	ad := fmt.Sprintf("gosp_test_hesapaski_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad) })
	pcfg := cfg.Clone()
	pcfg.DBName = ad
	db, err := sql.Open("mysql", pcfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, q := range []string{
		`CREATE TABLE users (id BIGINT UNSIGNED PRIMARY KEY, status VARCHAR(16) NOT NULL DEFAULT 'active')`,
		`CREATE TABLE domains (id BIGINT UNSIGNED PRIMARY KEY, alan_adi VARCHAR(253) NOT NULL,
			sistem_kullanici VARCHAR(64) NOT NULL, php_surum VARCHAR(8) NOT NULL DEFAULT '8.3',
			is_demo TINYINT NOT NULL DEFAULT 0, reseller_id BIGINT UNSIGNED NULL,
			askida TINYINT NOT NULL DEFAULT 0, askida_kaynak VARCHAR(16) NOT NULL DEFAULT '',
			durum VARCHAR(16) NOT NULL DEFAULT 'aktif')`,
		`CREATE TABLE ftp_accounts (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
			domain_id BIGINT UNSIGNED NOT NULL, status VARCHAR(16) NOT NULL DEFAULT 'active')`,
		`INSERT INTO users (id, status) VALUES (7,'active')`,
		`INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES (1,'ha1.test','c_ha1',7)`,
		`INSERT INTO ftp_accounts (domain_id) VALUES (1),(1)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("şema: %v\n%s", err, q)
		}
	}
	return db
}

// ha_kayit — sahte adımların çağrı günlüğü + her çağrı ANINDA hesabın tek
// kaynaktaki askı durumu (render'ın gördüğü durum).
type ha_kayit struct {
	db      *sql.DB
	cagri   []string
	hata    map[string]error // adım → döneceği hata
	havuzAd []string
}

func (k *ha_kayit) not(t *testing.T, adim, sk string) error {
	t.Helper()
	askida, err := hesaplar.HesapAskida(context.Background(), k.db, sk)
	if err != nil {
		t.Fatalf("%s: askı durumu okunamadı: %v", adim, err)
	}
	k.cagri = append(k.cagri, fmt.Sprintf("%s[askida=%v]", adim, askida))
	return k.hata[adim]
}

func (k *ha_kayit) adimlar(t *testing.T) Adimlar {
	return Adimlar{
		AnaVhost: func(_ *sql.DB, id int64) error { return k.not(t, "ana", k.sk(t, id)) },
		AltVhostlar: func(_ context.Context, _ *sql.DB, id int64) error {
			return k.not(t, "alt", k.sk(t, id))
		},
		AltHavuzlar: func(_ context.Context, _ *sql.DB, _ int64, sk string) ([]string, error) {
			return []string{sk + "_s3"}, k.not(t, "althavuz", sk)
		},
		PHPHavuz: func(_ *sql.DB, _ int64, sk, _ string, ek []string, askida bool) error {
			k.havuzAd = ek
			return k.not(t, fmt.Sprintf("php(%v)", askida), sk)
		},
		SSHGiris: func(sk string, kilit bool) error { return k.not(t, fmt.Sprintf("ssh(%v)", kilit), sk) },
		DBKullanici: func(_ *sql.DB, id int64, kilit bool) error {
			return k.not(t, fmt.Sprintf("db(%v)", kilit), k.sk(t, id))
		},
		CalismaZamani: func(sk string, askida bool) { _ = k.not(t, fmt.Sprintf("cron(%v)", askida), sk) },
	}
}

func (k *ha_kayit) sk(t *testing.T, id int64) string {
	t.Helper()
	var sk string
	if err := k.db.QueryRow(`SELECT sistem_kullanici FROM domains WHERE id=?`, id).Scan(&sk); err != nil {
		t.Fatal(err)
	}
	return sk
}

type ha_durum struct {
	askida        int
	durum, kaynak string
	ftp           string
}

func ha_oku(t *testing.T, db *sql.DB, id int64) ha_durum {
	t.Helper()
	var d ha_durum
	if err := db.QueryRow(`SELECT askida, durum, askida_kaynak FROM domains WHERE id=?`, id).Scan(&d.askida, &d.durum, &d.kaynak); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`SELECT DISTINCT status FROM ftp_accounts WHERE domain_id=?`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ftp []string
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		ftp = append(ftp, s)
	}
	d.ftp = strings.Join(ftp, ",")
	return d
}

// Askıya alma: hesaba bağlı HER adım, render'lar askı durumunu tek kaynaktan
// "askıda" okurken; askıdan alma: hepsi geri, PHP alt alanlardan ÖNCE.
func TestUygula_HesapDuzeyinde_GidisDonus(t *testing.T) {
	db := ha_db(t)
	k := &ha_kayit{db: db}
	a := k.adimlar(t)
	ctx := context.Background()

	alan, err := a.Uygula(ctx, db, 1, true, KaynakManuel)
	if err != nil || alan != "ha1.test" {
		t.Fatalf("askıya alma: %q %v", alan, err)
	}
	want := "ana[askida=true] alt[askida=true] db(true)[askida=true] althavuz[askida=true] php(true)[askida=true] ssh(true)[askida=true] cron(true)[askida=true]"
	if got := strings.Join(k.cagri, " "); got != want {
		t.Fatalf("askıya alma adımları:\n got: %s\nwant: %s", got, want)
	}
	if strings.Join(k.havuzAd, ",") != "c_ha1_s3" {
		t.Fatalf("alt alan havuzları da askıya alınmalı: %v", k.havuzAd)
	}
	if d := ha_oku(t, db, 1); d != (ha_durum{1, "pasif", "manuel", "suspended"}) {
		t.Fatalf("askı sonrası durum: %+v", d)
	}
	// İdempotent: ikinci askıya alma aynı adımları zararsızca yineler.
	k.cagri = nil
	if _, err := a.Uygula(ctx, db, 1, true, KaynakManuel); err != nil || len(k.cagri) != 7 {
		t.Fatalf("ikinci askıya alma: %v %v", err, k.cagri)
	}

	k.cagri = nil
	if _, err := a.Uygula(ctx, db, 1, false, KaynakManuel); err != nil {
		t.Fatalf("askıdan alma: %v", err)
	}
	want = "ana[askida=false] althavuz[askida=false] php(false)[askida=false] alt[askida=false] db(false)[askida=false] ssh(false)[askida=false] cron(false)[askida=false]"
	if got := strings.Join(k.cagri, " "); got != want {
		t.Fatalf("askıdan alma adımları:\n got: %s\nwant: %s", got, want)
	}
	if d := ha_oku(t, db, 1); d != (ha_durum{0, "aktif", "", "active"}) {
		t.Fatalf("askıdan alma sonrası durum: %+v", d)
	}
}

// İlk adım (ana vhost) düşerse DB eski hâline döner, başka adım çalışmaz.
func TestUygula_AnaVhostHatasi_DBGeriAlinir(t *testing.T) {
	db := ha_db(t)
	k := &ha_kayit{db: db, hata: map[string]error{"ana": errors.New("nginx -t başarısız")}}
	a := k.adimlar(t)
	_, err := a.Uygula(context.Background(), db, 1, true, KaynakManuel)
	var vh *VhostHatasi
	if !errors.As(err, &vh) {
		t.Fatalf("VhostHatasi beklenirdi: %v", err)
	}
	if d := ha_oku(t, db, 1); d != (ha_durum{0, "aktif", "", "active"}) || len(k.cagri) != 1 {
		t.Fatalf("hiçbir şey değişmemeli: %+v %v", d, k.cagri)
	}
	// Askıdan almada da: bayi kaynağı korunarak askıda kalır.
	if _, err := db.Exec(`UPDATE domains SET askida=1, durum='pasif', askida_kaynak='bayi' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Uygula(context.Background(), db, 1, false, KaynakBayi); !errors.As(err, &vh) {
		t.Fatalf("VhostHatasi beklenirdi: %v", err)
	}
	if d := ha_oku(t, db, 1); d.askida != 1 || d.kaynak != "bayi" || d.durum != "pasif" {
		t.Fatalf("askı kaynağıyla geri dönmeli: %+v", d)
	}
}

// Sonraki bir adım düşerse ötekiler yine uygulanır, hesap ASKIDA kalır.
func TestUygula_KismiHata_HesapAskidaKalir(t *testing.T) {
	db := ha_db(t)
	k := &ha_kayit{db: db, hata: map[string]error{"php(true)": errors.New("systemctl disable başarısız")}}
	a := k.adimlar(t)
	_, err := a.Uygula(context.Background(), db, 1, true, KaynakManuel)
	var kh *KismiHata
	if !errors.As(err, &kh) || !kh.Askida || !strings.Contains(err.Error(), "PHP") {
		t.Fatalf("askıda kalan KismiHata beklenirdi: %v", err)
	}
	if d := ha_oku(t, db, 1); d.askida != 1 || d.ftp != "suspended" {
		t.Fatalf("hesap askıda sayılmalı: %+v", d)
	}
	if !strings.Contains(strings.Join(k.cagri, " "), "ssh(true)") || !strings.Contains(strings.Join(k.cagri, " "), "cron(true)") {
		t.Fatalf("düşen adımdan sonrakiler de uygulanmalı: %v", k.cagri)
	}
}

// Askıdan alma yarım kalırsa güvenli taraf: hesap yeniden askıya alınır
// (kaynak korunur), hata raporlanır.
func TestUygula_AskidanAlmaYarim_YenidenAskiya(t *testing.T) {
	db := ha_db(t)
	if _, err := db.Exec(`UPDATE domains SET askida=1, durum='pasif', askida_kaynak='bayi' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE ftp_accounts SET status='suspended'`); err != nil {
		t.Fatal(err)
	}
	k := &ha_kayit{db: db, hata: map[string]error{"ssh(false)": errors.New("chage başarısız")}}
	a := k.adimlar(t)
	_, err := a.Uygula(context.Background(), db, 1, false, KaynakBayi)
	var kh *KismiHata
	if !errors.As(err, &kh) || !kh.Askida {
		t.Fatalf("yeniden askıya alınmış KismiHata beklenirdi: %v", err)
	}
	if d := ha_oku(t, db, 1); d != (ha_durum{1, "pasif", "bayi", "suspended"}) {
		t.Fatalf("hesap askı kaynağıyla askıda kalmalı: %+v", d)
	}
	cagri := strings.Join(k.cagri, " ")
	for _, adim := range []string{"php(false)", "php(true)[askida=true]", "ssh(true)", "db(true)", "cron(true)"} {
		if !strings.Contains(cagri, adim) {
			t.Fatalf("%s bekleniyordu (açılanlar geri kapanmalı): %s", adim, cagri)
		}
	}
}

// Bayi kuralları: bayi askıdayken hosting tek tek askıya alınamaz/açılamaz
// (tekil ve toplu aynı yol); bayi zinciri kuralı atlar.
func TestUygula_BayiKurallari(t *testing.T) {
	db := ha_db(t)
	k := &ha_kayit{db: db}
	a := k.adimlar(t)
	ctx := context.Background()
	if _, err := db.Exec(`UPDATE users SET status='suspended' WHERE id=7`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Uygula(ctx, db, 1, true, KaynakManuel); !errors.Is(err, ErrBayiAskida) {
		t.Fatalf("ErrBayiAskida beklenirdi: %v", err)
	}
	if _, err := a.Uygula(ctx, db, 1, false, KaynakManuel); !errors.Is(err, ErrBayiAskidaAcilamaz) {
		t.Fatalf("ErrBayiAskidaAcilamaz beklenirdi: %v", err)
	}
	if len(k.cagri) != 0 {
		t.Fatalf("reddedilen istekte adım çalışmamalı: %v", k.cagri)
	}
	if _, err := a.Uygula(ctx, db, 1, true, KaynakBayi); err != nil {
		t.Fatalf("bayi zinciri kuralı atlamalı: %v", err)
	}
	if d := ha_oku(t, db, 1); d.kaynak != "bayi" {
		t.Fatalf("zincirin askısı 'bayi' kaynaklı olmalı: %+v", d)
	}
}

func TestUygula_Demo(t *testing.T) {
	db := ha_db(t)
	if _, err := db.Exec(`UPDATE domains SET is_demo=1 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	k := &ha_kayit{db: db}
	if _, err := k.adimlar(t).Uygula(context.Background(), db, 1, true, KaynakManuel); !errors.Is(err, ErrDemo) {
		t.Fatalf("ErrDemo beklenirdi: %v", err)
	}
	if d := ha_oku(t, db, 1); d.askida != 0 || len(k.cagri) != 0 {
		t.Fatalf("demo dokunulmamalı: %+v %v", d, k.cagri)
	}
}

// Bayi zinciri TEK yoldan: açık hesaplar 'bayi' kaynaklı askıya alınır, elle
// askıya alınmış ve demo hesaplar zincire girmez; geri açarken YALNIZ 'bayi'.
func TestBayiUygula_TekYoldan(t *testing.T) {
	db := ha_db(t)
	for _, q := range []string{
		`INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id, askida, askida_kaynak, durum) VALUES (2,'ha2.test','c_ha2',7,1,'manuel','pasif')`,
		`INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id, is_demo) VALUES (3,'ha3.test','c_ha3',7,1)`,
		`INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES (4,'ha4.test','c_ha4',8)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	k := &ha_kayit{db: db}
	a := k.adimlar(t)
	ctx := context.Background()
	if _, err := db.Exec(`UPDATE users SET status='suspended' WHERE id=7`); err != nil {
		t.Fatal(err)
	}
	ok, hatali, err := a.BayiUygula(ctx, db, 7, true)
	if err != nil || ok != 1 || hatali != 0 {
		t.Fatalf("zincir: ok=%d hatali=%d err=%v", ok, hatali, err)
	}
	for id, w := range map[int64]ha_durum{1: {1, "pasif", "bayi", "suspended"}, 2: {1, "pasif", "manuel", ""}, 3: {0, "aktif", "", ""}, 4: {0, "aktif", "", ""}} {
		if d := ha_oku(t, db, id); d != w {
			t.Errorf("domain %d: %+v, beklenen %+v", id, d, w)
		}
	}
	if !strings.Contains(strings.Join(k.cagri, " "), "php(true)") {
		t.Fatalf("zincir tüm hesap adımlarını uygulamalı: %v", k.cagri)
	}
	if _, err := db.Exec(`UPDATE users SET status='active' WHERE id=7`); err != nil {
		t.Fatal(err)
	}
	ok, hatali, err = a.BayiUygula(ctx, db, 7, false)
	if err != nil || ok != 1 || hatali != 0 {
		t.Fatalf("geri açma: ok=%d hatali=%d err=%v", ok, hatali, err)
	}
	if d := ha_oku(t, db, 1); d.askida != 0 || d.ftp != "active" {
		t.Fatalf("bayi askısı kalkmalı: %+v", d)
	}
	if d := ha_oku(t, db, 2); d.askida != 1 || d.kaynak != "manuel" {
		t.Fatalf("elle askıya alınan açılmamalı: %+v", d)
	}
}

// Bayi zincirinde ana vhost düşerse hesap GERİ ALINMAZ: askıdaki bayinin altında
// yayında site kalmaz (tek tek askıya alma bayi askıdayken kapalıdır); kalan
// adımlar uygulanır, başarısız sayılır; bayi geri açılınca zincir onu da açar.
func TestBayiUygula_AnaVhostHatasi_AskidaKalir(t *testing.T) {
	db := ha_db(t)
	k := &ha_kayit{db: db, hata: map[string]error{"ana": errors.New("nginx -t başarısız")}}
	a := k.adimlar(t)
	ctx := context.Background()
	ok, hatali, err := a.BayiUygula(ctx, db, 7, true)
	if err != nil || ok != 0 || hatali != 1 {
		t.Fatalf("zincir: ok=%d hatali=%d err=%v", ok, hatali, err)
	}
	if d := ha_oku(t, db, 1); d != (ha_durum{1, "pasif", "bayi", "suspended"}) {
		t.Fatalf("bayi zincirinde hesap askıda kalmalı: %+v", d)
	}
	if !strings.Contains(strings.Join(k.cagri, " "), "php(true)") {
		t.Fatalf("kalan askı adımları uygulanmalı: %v", k.cagri)
	}
	delete(k.hata, "ana")
	if ok, hatali, err = a.BayiUygula(ctx, db, 7, false); err != nil || ok != 1 || hatali != 0 {
		t.Fatalf("geri açma: ok=%d hatali=%d err=%v", ok, hatali, err)
	}
}
