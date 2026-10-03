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
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/hesaplar"
	"girginospanel/internal/kilit"
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
		Birimler: func(_ context.Context, _ *sql.DB, id int64, askida bool) error {
			return k.not(t, fmt.Sprintf("birim(%v)", askida), k.sk(t, id))
		},
		Uygulamalar:   func(sk string, askida bool) error { return k.not(t, fmt.Sprintf("uyg(%v)", askida), sk) },
		CalismaZamani: func(sk string, askida bool) { _ = k.not(t, fmt.Sprintf("cron(%v)", askida), sk) },
		Posta: func(_ context.Context, _ *sql.DB, id int64, askida bool) error {
			return k.not(t, fmt.Sprintf("posta(%v)", askida), k.sk(t, id))
		},
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

	alan, err := a.Uygula(ctx, db, 1, true, KaynakKok)
	if err != nil || alan != "ha1.test" {
		t.Fatalf("askıya alma: %q %v", alan, err)
	}
	want := "ana[askida=true] alt[askida=true] posta(true)[askida=true] birim(true)[askida=true] uyg(true)[askida=true] db(true)[askida=true] althavuz[askida=true] php(true)[askida=true] ssh(true)[askida=true] cron(true)[askida=true]"
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
	if _, err := a.Uygula(ctx, db, 1, true, KaynakKok); err != nil || len(k.cagri) != 10 {
		t.Fatalf("ikinci askıya alma: %v %v", err, k.cagri)
	}

	k.cagri = nil
	if _, err := a.Uygula(ctx, db, 1, false, KaynakKok); err != nil {
		t.Fatalf("askıdan alma: %v", err)
	}
	want = "ana[askida=false] althavuz[askida=false] php(false)[askida=false] alt[askida=false] posta(false)[askida=false] db(false)[askida=false] uyg(false)[askida=false] birim(false)[askida=false] ssh(false)[askida=false] cron(false)[askida=false]"
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
	_, err := a.Uygula(context.Background(), db, 1, true, KaynakKok)
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
	_, err := a.Uygula(context.Background(), db, 1, true, KaynakKok)
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
	for _, adim := range []string{"php(false)", "php(true)[askida=true]", "ssh(true)", "db(true)", "birim(true)", "uyg(true)", "cron(true)"} {
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
	if _, err := a.Uygula(ctx, db, 1, true, KaynakKok); !errors.Is(err, ErrBayiAskida) {
		t.Fatalf("ErrBayiAskida beklenirdi: %v", err)
	}
	if _, err := a.Uygula(ctx, db, 1, false, KaynakKok); !errors.Is(err, ErrBayiAskidaAcilamaz) {
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
	if _, err := k.adimlar(t).Uygula(context.Background(), db, 1, true, KaynakKok); !errors.Is(err, ErrDemo) {
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

// 🔴 Zaten AÇIK bir hesapta askıdan alma (yeniden deneme, toplu "aktif") bir
// adımda düşerse hesap askıya ALINMAZ: DB açık kalır, KismiHata{Askida:false}.
// (Bozulursa toplu "aktif" işleminde adımı düşen açık hesaplar sessizce kapanırdı.)
func TestUygula_AcikHesaptaKismiHata_AskiyaAlmaz(t *testing.T) {
	db := ha_db(t)
	k := &ha_kayit{db: db, hata: map[string]error{"ssh(false)": errors.New("chage başarısız")}}
	_, err := k.adimlar(t).Uygula(context.Background(), db, 1, false, KaynakKok)
	var kh *KismiHata
	if !errors.As(err, &kh) || kh.Askida {
		t.Fatalf("açık kalan KismiHata beklenirdi: %v", err)
	}
	if d := ha_oku(t, db, 1); d != (ha_durum{0, "aktif", "", "active"}) {
		t.Fatalf("açık hesap askıya alınmamalı: %+v", d)
	}
	for _, c := range k.cagri {
		if strings.Contains(c, "(true)") {
			t.Fatalf("açık hesapta kapatma adımı çalışmamalı: %v", k.cagri)
		}
	}
}

// Sistem kullanıcısı adımları (PHP havuzu, SSH, cron/süreçler) yalnız c_
// hesaplarına uygulanır; web/FTP/DB/işleyici adımları yine uygulanır.
func TestUygula_CKullanicisiDegilse_SistemAdimlariYok(t *testing.T) {
	db := ha_db(t)
	if _, err := db.Exec(`UPDATE domains SET sistem_kullanici='root' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	k := &ha_kayit{db: db}
	if _, err := k.adimlar(t).Uygula(context.Background(), db, 1, true, KaynakKok); err != nil {
		t.Fatal(err)
	}
	cagri := strings.Join(k.cagri, " ")
	for _, yasak := range []string{"php(", "althavuz", "ssh(", "cron(", "uyg("} {
		if strings.Contains(cagri, yasak) {
			t.Fatalf("c_ olmayan kullanıcıya %s uygulanmamalı: %s", yasak, cagri)
		}
	}
	for _, gerek := range []string{"ana", "alt", "birim(true)", "db(true)"} {
		if !strings.Contains(cagri, gerek) {
			t.Fatalf("%s uygulanmalı: %s", gerek, cagri)
		}
	}
}

// Bayi zinciri listesi anlık görüntüdür: işlem sırasında silinen hesap
// başarısız SAYILMAZ (bayi askısı "başarısız" diye raporlanmaz).
func TestBayiUygula_SilinenHesapSayilmaz(t *testing.T) {
	db := ha_db(t)
	if _, err := db.Exec(`INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES (2,'ha2.test','c_ha2',7)`); err != nil {
		t.Fatal(err)
	}
	k := &ha_kayit{db: db}
	a := k.adimlar(t)
	ana := a.AnaVhost
	a.AnaVhost = func(d *sql.DB, id int64) error {
		if id == 1 {
			if _, err := db.Exec(`DELETE FROM domains WHERE id=2`); err != nil {
				t.Fatal(err)
			}
		}
		return ana(d, id)
	}
	ok, hatali, err := a.BayiUygula(context.Background(), db, 7, true)
	if err != nil || ok != 1 || hatali != 0 {
		t.Fatalf("silinen hesap sayılmamalı: ok=%d hatali=%d err=%v", ok, hatali, err)
	}
}

// Hesap kilidi: aynı hesaba eşzamanlı askıya alma / askıdan alma iç içe
// GEÇMEZ. İkinci işlem ilki bitene kadar hiçbir adım (DB yazımı dahil) yapmaz;
// son durum son işlemindir ve adımlar tutarlıdır.
func TestUygula_HesapKilidi_IcIceGecmez(t *testing.T) {
	db := ha_db(t)
	k := &ha_kayit{db: db}
	a := k.adimlar(t)
	var mu sync.Mutex
	bekle, girdi := make(chan struct{}), make(chan struct{})
	ilk := true
	ana := a.AnaVhost
	a.AnaVhost = func(d *sql.DB, id int64) error {
		mu.Lock()
		birinci := ilk
		ilk = false
		mu.Unlock()
		if birinci {
			close(girdi)
			<-bekle // askıya alma ana vhost adımında bekletilir
		}
		return ana(d, id)
	}
	ctx := context.Background()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		// Bayi kaynağı: bayi kilidi alınmaz — yalnız HESAP kilidi sınanır.
		if _, err := a.Uygula(ctx, db, 1, true, KaynakBayi); err != nil {
			t.Errorf("askıya alma: %v", err)
		}
	}()
	<-girdi
	go func() {
		defer wg.Done()
		if _, err := a.Uygula(ctx, db, 1, false, KaynakBayi); err != nil {
			t.Errorf("askıdan alma: %v", err)
		}
	}()
	time.Sleep(300 * time.Millisecond)
	// Askıya alma ana vhost'ta beklerken askıdan alma DB'ye yazmamış olmalı.
	if d := ha_oku(t, db, 1); d.askida != 1 {
		t.Fatalf("ikinci işlem kilidi beklemeden DB'ye yazdı: %+v", d)
	}
	close(bekle)
	wg.Wait()
	cagri := strings.Join(k.cagri, " ")
	i := strings.Index(cagri, "cron(true)")
	j := strings.Index(cagri, "ana[askida=false]")
	if i < 0 || j < 0 || i > j {
		t.Fatalf("askıdan alma, askıya alma bitmeden başladı: %s", cagri)
	}
	if d := ha_oku(t, db, 1); d != (ha_durum{0, "aktif", "", "active"}) {
		t.Fatalf("son durum askıdan alma olmalı: %+v", d)
	}
}

// Tekil/toplu yol bayi durumunu ve uygulamayı BAYİ kilidi altında yapar: bayi
// zinciri kilidi tutarken tekil askı bekler, zincir bitince güncel bayi
// durumunu okur (bayi askıya alındıysa reddedilir).
func TestUygula_Manuel_BayiKilidiniBekler(t *testing.T) {
	db := ha_db(t)
	k := &ha_kayit{db: db}
	a := k.adimlar(t)
	bk := kilit.Bayi(7)
	bk.Lock()
	sonuc := make(chan error, 1)
	go func() {
		_, err := a.Uygula(context.Background(), db, 1, true, KaynakKok)
		sonuc <- err
	}()
	time.Sleep(300 * time.Millisecond)
	if len(k.cagri) != 0 || ha_oku(t, db, 1).askida != 0 {
		bk.Unlock()
		t.Fatalf("bayi kilidi tutulurken tekil askı uygulanmamalı: %v", k.cagri)
	}
	// Zincir bu sırada bayiyi askıya aldı.
	if _, err := db.Exec(`UPDATE users SET status='suspended' WHERE id=7`); err != nil {
		bk.Unlock()
		t.Fatal(err)
	}
	bk.Unlock()
	if err := <-sonuc; !errors.Is(err, ErrBayiAskida) {
		t.Fatalf("kilit bırakılınca güncel bayi durumu okunmalı (ErrBayiAskida): %v", err)
	}
}

// Açılış süpürmesi: askıdaki HER hesaba (bayi kaynaklı ve bayisi askıdaki dahil)
// askı adımları yeniden uygulanır; DB askı kolonlarına dokunulmaz, açık hesaba
// hiçbir adım uygulanmaz.
func TestAskidakileriUygula_AcilisSupurmesi(t *testing.T) {
	db := ha_db(t)
	for _, q := range []string{
		`UPDATE users SET status='suspended' WHERE id=7`,
		`UPDATE domains SET askida=1, durum='pasif', askida_kaynak='bayi' WHERE id=1`,
		`INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id, askida, askida_kaynak, durum) VALUES (2,'ha2.test','c_ha2',NULL,1,'manuel','pasif')`,
		`INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (3,'ha3.test','c_ha3')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	k := &ha_kayit{db: db}
	ok, hatali, err := k.adimlar(t).AskidakileriUygula(context.Background(), db)
	if err != nil || ok != 2 || hatali != 0 {
		t.Fatalf("süpürme: ok=%d hatali=%d err=%v", ok, hatali, err)
	}
	cagri := strings.Join(k.cagri, " ")
	if strings.Count(cagri, "php(true)[askida=true]") != 2 || strings.Count(cagri, "ana[askida=true]") != 2 ||
		strings.Contains(cagri, "(false)") || strings.Contains(cagri, "askida=false") {
		t.Fatalf("yalnız askıdaki iki hesaba askı adımları uygulanmalı: %s", cagri)
	}
	if d := ha_oku(t, db, 1); d.askida != 1 || d.kaynak != "bayi" || d.ftp != "suspended" {
		t.Fatalf("bayi kaynaklı askı korunmalı, adımlar uygulanmalı: %+v", d)
	}
	if d := ha_oku(t, db, 2); d.askida != 1 || d.kaynak != "manuel" {
		t.Fatalf("manuel askı korunmalı: %+v", d)
	}
	if d := ha_oku(t, db, 3); d.askida != 0 || d.durum != "aktif" {
		t.Fatalf("açık hesaba dokunulmamalı: %+v", d)
	}
	// Bir adım düşerse hesap hatalı sayılır, ötekiler yine uygulanır.
	k2 := &ha_kayit{db: db, hata: map[string]error{"php(true)": errors.New("reload başarısız")}}
	if ok, hatali, err := k2.adimlar(t).AskidakileriUygula(context.Background(), db); err != nil || ok != 0 || hatali != 2 {
		t.Fatalf("düşen adım hatalı sayılmalı: ok=%d hatali=%d err=%v", ok, hatali, err)
	}
	if !strings.Contains(strings.Join(k2.cagri, " "), "cron(true)") {
		t.Fatalf("düşen adımdan sonrakiler de uygulanmalı: %v", k2.cagri)
	}
}

// 🔴 Uygula istek bağlamından BAĞIMSIZDIR: istemci ana vhost adımından hemen
// sonra koptu (bağlam iptal) — askıya alma da askıdan alma da sonuna kadar
// yürür, DB ile gerçek durum ayrışmaz. (Eskiden kalan DB/FTP adımları iptal
// yüzünden düşüyor; askıdan almada DB "açık" derken PHP/SSH/DB kilitleniyordu.)
func TestUygula_IstekBaglamiIptal_IslemTamamlanir(t *testing.T) {
	db := ha_db(t)
	k := &ha_kayit{db: db}
	for _, askida := range []bool{true, false} {
		ctx, iptal := context.WithCancel(context.Background())
		a := k.adimlar(t)
		ana := a.AnaVhost
		a.AnaVhost = func(d *sql.DB, id int64) error {
			iptal()
			return ana(d, id)
		}
		k.cagri = nil
		if _, err := a.Uygula(ctx, db, 1, askida, KaynakKok); err != nil {
			t.Fatalf("askida=%v: istek bağlamı iptal olsa da işlem tamamlanmalı: %v", askida, err)
		}
		w := ha_durum{1, "pasif", "manuel", "suspended"}
		if !askida {
			w = ha_durum{0, "aktif", "", "active"}
		}
		if d := ha_oku(t, db, 1); d != w {
			t.Fatalf("askida=%v: durum %+v, beklenen %+v", askida, d, w)
		}
		cagri := strings.Join(k.cagri, " ")
		if len(k.cagri) != 10 || strings.Contains(cagri, fmt.Sprintf("(%v)", !askida)) {
			t.Fatalf("askida=%v: tüm adımlar tek yönde uygulanmalı: %s", askida, cagri)
		}
	}
}

// Üst süre (islemSuresi) adımların ortasında dolarsa güvenli taraf yine
// kurulur: askıdan almada hesap kurtarma bağlamıyla YENİDEN askıya alınır (DB
// askıda + FTP kapalı); askıya almada kapatma adımları kurtarma bağlamıyla
// tamamlanır.
func TestUygula_UstSureDolarsa_GuvenliTaraf(t *testing.T) {
	db := ha_db(t)
	eski := islemSuresi
	t.Cleanup(func() { islemSuresi = eski })
	islemSuresi = 200 * time.Millisecond
	yavas := func(a Adimlar) Adimlar {
		ana := a.AnaVhost
		a.AnaVhost = func(d *sql.DB, id int64) error {
			time.Sleep(400 * time.Millisecond) // süre ana vhost sırasında dolar
			return ana(d, id)
		}
		return a
	}

	k := &ha_kayit{db: db}
	if _, err := yavas(k.adimlar(t)).Uygula(context.Background(), db, 1, true, KaynakKok); err != nil {
		t.Fatalf("askıya alma: kapatma adımları kurtarma bağlamıyla tamamlanmalı: %v", err)
	}
	if d := ha_oku(t, db, 1); d != (ha_durum{1, "pasif", "manuel", "suspended"}) {
		t.Fatalf("askıya alma sonrası durum: %+v", d)
	}

	k = &ha_kayit{db: db}
	_, err := yavas(k.adimlar(t)).Uygula(context.Background(), db, 1, false, KaynakKok)
	var kh *KismiHata
	if !errors.As(err, &kh) || !kh.Askida {
		t.Fatalf("askıdan alma yarım kaldı: hesap yeniden askıya alınmış KismiHata beklenirdi: %v", err)
	}
	if d := ha_oku(t, db, 1); d != (ha_durum{1, "pasif", "manuel", "suspended"}) {
		t.Fatalf("hesap askıda kalmalı (DB + FTP): %+v", d)
	}
	if !strings.Contains(strings.Join(k.cagri, " "), "php(true)[askida=true]") {
		t.Fatalf("açılan adımlar geri kapanmalı: %v", k.cagri)
	}

	// Süre FTP açıldıktan SONRA dolar ve sonraki bir adım düşer: yeniden askıya
	// alma (kurtarma bağlamı) açılmış FTP'yi de kapatır.
	k = &ha_kayit{db: db, hata: map[string]error{"ssh(false)": errors.New("chage başarısız")}}
	a := k.adimlar(t)
	dbAdim := a.DBKullanici
	a.DBKullanici = func(d *sql.DB, id int64, kilit bool) error {
		if !kilit {
			time.Sleep(400 * time.Millisecond)
		}
		return dbAdim(d, id, kilit)
	}
	if _, err := a.Uygula(context.Background(), db, 1, false, KaynakKok); !errors.As(err, &kh) || !kh.Askida {
		t.Fatalf("yeniden askıya alınmış KismiHata beklenirdi: %v", err)
	}
	if d := ha_oku(t, db, 1); d != (ha_durum{1, "pasif", "manuel", "suspended"}) {
		t.Fatalf("açılmış FTP de geri kapanmalı: %+v", d)
	}
}

// 🔴 Bayi kökün askısını AÇAMAZ (kullanıcı kararı 2026-09-29): 'manuel' ve
// ayırt edilemeyen (boş) kaynak kökündür; bayinin yeniden askıya alması kaynağı
// bayininkine İNDİRMEZ. Bayi kendi askısını ve (bayi aktifken) zincir kalıntısını
// açabilir; kök bayinin askısını devralırsa bayi artık açamaz.
func TestUygula_BayiKokAskisiniAcamaz(t *testing.T) {
	db := ha_db(t)
	k := &ha_kayit{db: db}
	a := k.adimlar(t)
	ctx := context.Background()
	kaynakYaz := func(kaynak string) {
		t.Helper()
		if _, err := db.Exec(`UPDATE domains SET askida=1, durum='pasif', askida_kaynak=? WHERE id=1`, kaynak); err != nil {
			t.Fatal(err)
		}
	}
	bayiAcamaz := func(neden string) {
		t.Helper()
		k.cagri = nil
		onceki := ha_oku(t, db, 1)
		if _, err := a.Uygula(ctx, db, 1, false, KaynakBayiElle); !errors.Is(err, ErrKokAskisi) {
			t.Fatalf("%s: ErrKokAskisi beklenirdi: %v", neden, err)
		}
		if len(k.cagri) != 0 || ha_oku(t, db, 1) != onceki {
			t.Fatalf("%s: reddedilen istekte hiçbir şey değişmemeli: %v", neden, k.cagri)
		}
	}

	if _, err := a.Uygula(ctx, db, 1, true, KaynakKok); err != nil {
		t.Fatal(err)
	}
	bayiAcamaz("kökün askısı")
	if _, err := a.Uygula(ctx, db, 1, true, KaynakBayiElle); err != nil {
		t.Fatalf("bayinin yeniden askıya alması zararsız olmalı: %v", err)
	}
	if d := ha_oku(t, db, 1); d.kaynak != KaynakKok {
		t.Fatalf("yeniden askıya alma kökün kaynağını korumalı: %+v", d)
	}
	bayiAcamaz("bayinin yeniden askıya aldığı kök askısı")
	kaynakYaz("")
	bayiAcamaz("ayırt edilemeyen eski askı")
	if _, err := a.Uygula(ctx, db, 1, false, KaynakKok); err != nil {
		t.Fatalf("kök açabilmeli: %v", err)
	}

	if _, err := a.Uygula(ctx, db, 1, true, KaynakBayiElle); err != nil {
		t.Fatal(err)
	}
	if d := ha_oku(t, db, 1); d.kaynak != KaynakBayiElle {
		t.Fatalf("bayinin askısı bayi kaynaklı olmalı: %+v", d)
	}
	if _, err := a.Uygula(ctx, db, 1, false, KaynakBayiElle); err != nil {
		t.Fatalf("bayi kendi askısını açabilmeli: %v", err)
	}
	if _, err := a.Uygula(ctx, db, 1, true, KaynakBayiElle); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Uygula(ctx, db, 1, true, KaynakKok); err != nil {
		t.Fatal(err)
	}
	bayiAcamaz("kökün devraldığı bayi askısı")

	kaynakYaz(KaynakBayi)
	if _, err := a.Uygula(ctx, db, 1, false, KaynakBayiElle); err != nil {
		t.Fatalf("bayi aktifken zincir kalıntısını bayi açabilmeli: %v", err)
	}
	if _, err := a.Uygula(ctx, db, 1, true, "manuel2"); !errors.Is(err, ErrKaynak) {
		t.Fatalf("tanınmayan kaynak reddedilmeli: %v", err)
	}
}

// Ana vhost süre dolduktan SONRA düşerse de güvenli taraf kurtarma bağlamıyla
// kurulur: elle askıda DB eski hâline döner (VhostHatasi, hiçbir şey değişmedi);
// bayi zincirinde hesap askıda kalır ve kalan kapatma adımları uygulanır.
func TestUygula_AnaVhostHatasi_SureDolmus_GuvenliTaraf(t *testing.T) {
	db := ha_db(t)
	eski := islemSuresi
	t.Cleanup(func() { islemSuresi = eski })
	islemSuresi = 200 * time.Millisecond
	k := &ha_kayit{db: db, hata: map[string]error{"ana": errors.New("nginx -t başarısız")}}
	a := k.adimlar(t)
	ana := a.AnaVhost
	a.AnaVhost = func(d *sql.DB, id int64) error {
		time.Sleep(400 * time.Millisecond)
		return ana(d, id)
	}
	var vh *VhostHatasi
	if _, err := a.Uygula(context.Background(), db, 1, true, KaynakKok); !errors.As(err, &vh) {
		t.Fatalf("VhostHatasi beklenirdi (DB geri alındı): %v", err)
	}
	if d := ha_oku(t, db, 1); d != (ha_durum{0, "aktif", "", "active"}) {
		t.Fatalf("DB eski hâline dönmeli: %+v", d)
	}
	ok, hatali, err := a.BayiUygula(context.Background(), db, 7, true)
	if err != nil || ok != 0 || hatali != 1 {
		t.Fatalf("zincir: ok=%d hatali=%d err=%v", ok, hatali, err)
	}
	if d := ha_oku(t, db, 1); d != (ha_durum{1, "pasif", "bayi", "suspended"}) {
		t.Fatalf("zincirde hesap askıda kalmalı, kapatma adımları uygulanmalı: %+v", d)
	}
}
