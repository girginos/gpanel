package domains

// dbonek_test.go — "Yeni Veritabanı" (POST /domains/{id}/databases) ve kullanıcısız DB'ye
// kullanıcı ekleme (PUT /databases/{dbid}/password) uçlarında DB KULLANICI ÖNEKİ
// ÇAKIŞMASI → komşunun DB kullanıcısını devralma.
//
// girgin.com (sk c_girgin, önek "girgin") "yeni kullanıcı" soneki "x_db" verince ad
// "girgin_x_db" olur = girgin-x.com'un (c_girgin_x) varsayılan kullanıcısı. Uç yalnız DB
// ADI çakışmasına bakıyor, MySQLCreateDB de var olan kullanıcının parolasını ALTER USER
// ile değiştiriyordu.
//
// Gerçek MariaDB gerekir; GOSP_TEST_GECICI_DSN (unix soketli geçici sunucu) yoksa ATLANIR.
// `mysql` CLI çağrıları PATH'e konan sarmalayıcıyla YALNIZ geçici sunucuya gider ve her
// çağrı bir günlüğe yazılır: reddedilen isteklerin MariaDB'de HİÇBİR DDL çalıştırmadığı
// buradan doğrulanır.
//
// 🔴 YAN ETKİ KALKANI: başarılı oluşturmadan sonra arka planda kaynaklimit.UygulaHepsi
// koşar; test şemasındaki domains tablosunda php_surum YOK → ilk sorguda döner, slice /
// FPM / governor adımlarına ulaşamaz.

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/hesaplar"
	"girginospanel/internal/middleware"
)

type dboOrtam struct {
	h     *Handlers
	kok   *sql.DB
	cfg   *mysql.Config
	on    string
	gunce string // mysql sarmalayıcı çağrı günlüğü
}

// dbo_ortam: domain 1 = c_<on> (kısa önek, saldırgan), domain 2 = c_<on>_x (kurban).
func dbo_ortam(t *testing.T) *dboOrtam {
	t.Helper()
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li test atlandı")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("DSN çözülemedi: %v", err)
	}
	if cfg.Net != "unix" || cfg.Addr == "" {
		t.Skip("geçici DSN unix soketi değil — mysql CLI sarmalayıcısı kurulamaz")
	}
	gercek, err := exec.LookPath("mysql")
	if err != nil {
		t.Skip("mysql istemcisi yok")
	}
	dir := t.TempDir()
	gunce := filepath.Join(dir, "cagrilar.log")
	betik := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + gunce + "'\n" +
		"exec '" + gercek + "' --no-defaults --protocol=socket --socket='" + cfg.Addr + "' --user=root \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "mysql"), []byte(betik), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := exec.Command("mysql", "-N", "-B", "-e", "SELECT @@socket").CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != cfg.Addr {
		t.Fatalf("mysql sarmalayıcısı geçici sunucuya gitmiyor (%q, %v) — test durduruldu", strings.TrimSpace(string(out)), err)
	}

	kcfg := cfg.Clone()
	kcfg.DBName = ""
	kok, err := sql.Open("mysql", kcfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = kok.Close() })
	on := "dbd" + strconv.FormatInt(time.Now().UnixNano()%1e12, 36)
	ad := "gosp_test_" + on
	if _, err := kok.Exec("CREATE DATABASE " + ad); err != nil {
		t.Fatalf("geçici veritabanı oluşturulamadı: %v", err)
	}
	t.Cleanup(func() {
		topla := func(q string) []string {
			var l []string
			if rows, err := kok.Query(q, on+"%"); err == nil {
				for rows.Next() {
					var s string
					if rows.Scan(&s) == nil {
						l = append(l, s)
					}
				}
				rows.Close()
			}
			return l
		}
		for _, u := range topla(`SELECT User FROM mysql.user WHERE User LIKE ?`) {
			_, _ = kok.Exec("DROP USER IF EXISTS '" + u + "'@'localhost'")
		}
		for _, d := range topla(`SELECT SCHEMA_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME LIKE ?`) {
			_, _ = kok.Exec("DROP DATABASE IF EXISTS `" + d + "`")
		}
		_, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad)
	})
	pcfg := cfg.Clone()
	pcfg.DBName = ad
	panel, err := sql.Open("mysql", pcfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = panel.Close() })
	for _, q := range []string{
		// 🔴 php_surum YOK (yan etki kalkanı, bkz. dosya başı).
		`CREATE TABLE domains (id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
			alan_adi VARCHAR(253) NOT NULL DEFAULT '', sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '',
			is_demo TINYINT NOT NULL DEFAULT 0, customer_id BIGINT UNSIGNED NULL,
			reseller_id BIGINT NOT NULL DEFAULT 0) ENGINE=InnoDB`,
		`CREATE TABLE db_accounts (
			id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
			domain_id BIGINT UNSIGNED NOT NULL,
			db_name VARCHAR(64) NOT NULL UNIQUE,
			db_user VARCHAR(64) NOT NULL,
			db_pass_plain VARCHAR(255) NOT NULL,
			db_host VARCHAR(64) NOT NULL DEFAULT 'localhost',
			notlar VARCHAR(255) NOT NULL DEFAULT '',
			KEY ix_db_domain (domain_id),
			CONSTRAINT fk_db_domain FOREIGN KEY (domain_id) REFERENCES domains(id) ON DELETE CASCADE
		) ENGINE=InnoDB`,
		fmt.Sprintf(`INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES
			(1,'%s.test','c_%s'), (2,'%s-x.test','c_%s_x')`, on, on, on, on),
	} {
		if _, err := panel.Exec(q); err != nil {
			t.Fatalf("şema: %v\n%s", err, q)
		}
	}
	return &dboOrtam{h: &Handlers{DB: panel}, kok: kok, cfg: cfg, on: on, gunce: gunce}
}

func (o *dboOrtam) kokExec(t *testing.T, q string) {
	t.Helper()
	if _, err := o.kok.Exec(q); err != nil {
		t.Fatalf("root SQL: %v\n%s", err, q)
	}
}

// kurbanKur: domain 2'nin sağlamadaki gibi varsayılan kullanıcısı + DB'si.
func (o *dboOrtam) kurbanKur(t *testing.T, parola string) (kul, dbAdi string) {
	t.Helper()
	kul, dbAdi = o.on+"_x_db", o.on+"_x_main"
	o.kokExec(t, "CREATE USER '"+kul+"'@'localhost' IDENTIFIED BY '"+parola+"'")
	o.kokExec(t, "CREATE DATABASE `"+dbAdi+"`")
	o.kokExec(t, "GRANT ALL PRIVILEGES ON `"+hesaplar.GrantDBKac(dbAdi)+"`.* TO '"+kul+"'@'localhost'")
	if _, err := o.h.DB.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (2,?,?,?)`,
		dbAdi, kul, parola); err != nil {
		t.Fatal(err)
	}
	return kul, dbAdi
}

func (o *dboOrtam) baglanir(kul, parola, dbAdi string) bool {
	c := o.cfg.Clone()
	c.User, c.Passwd, c.DBName = kul, parola, dbAdi
	db, err := sql.Open("mysql", c.FormatDSN())
	if err != nil {
		return false
	}
	defer db.Close()
	return db.Ping() == nil
}

func (o *dboOrtam) kullaniciVar(t *testing.T, kul string) bool {
	t.Helper()
	var n int
	if err := o.kok.QueryRow(`SELECT COUNT(*) FROM mysql.user WHERE User=? AND Host='localhost'`, kul).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

func (o *dboOrtam) dbVar(t *testing.T, ad string) bool {
	t.Helper()
	var n int
	if err := o.kok.QueryRow(`SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME=?`, ad).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

// ddlSifirla / ddl: sarmalayıcı günlüğünü boşalt / günlükteki DDL ifadelerini döndür.
func (o *dboOrtam) ddlSifirla(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(o.gunce, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func (o *dboOrtam) ddl(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(o.gunce)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var out []string
	for _, s := range strings.Split(string(b), "\n") {
		u := strings.ToUpper(s)
		for _, k := range []string{"CREATE USER", "ALTER USER", "CREATE DATABASE", "GRANT ", "DROP "} {
			if strings.Contains(u, k) {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

func (o *dboOrtam) istek(t *testing.T, yontem, yol string, govde any) (int, string) {
	t.Helper()
	b, _ := json.Marshal(govde)
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(aa_sir))
		r.Post("/domains/{id}/databases", o.h.CreateDatabase)
		r.Put("/databases/{dbid}/password", o.h.SetDatabasePassword)
	})
	req := httptest.NewRequest(yontem, yol, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+aa_yonetici(t))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func dboYeniDB(dbSonek, kulSonek, parola string) map[string]any {
	return map[string]any{"kullanici_tipi": "yeni", "db_sonek": dbSonek, "kullanici_sonek": kulSonek, "parola": parola}
}

// 🔴 ASIL İSTİSMAR + ad-alanı / var-olan-kimlik kapıları (CreateDatabase).
func TestCreateDatabase_OnekCakismasiKomsuyuDevralamaz(t *testing.T) {
	o := dbo_ortam(t)
	on := o.on
	kurbanPw := "KurbanParola123"
	kurban, kurbanDB := o.kurbanKur(t, kurbanPw)
	// Panelde kayıtlı ama MariaDB'de olmayan (kendi ad-alanında) kullanıcı adı.
	if _, err := o.h.DB.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (1,?,?,'x')`,
		on+"_kayitdb", on+"_kayitli"); err != nil {
		t.Fatal(err)
	}
	// MariaDB'de olan ama panelde kayıtsız kullanıcı + DB (kendi ad-alanında).
	o.kokExec(t, "CREATE USER '"+on+"_kayitsiz'@'localhost' IDENTIFIED BY 'KayitsizParola123'")
	o.kokExec(t, "CREATE DATABASE `"+on+"_varolan`")

	for _, c := range []struct {
		ad    string
		govde map[string]any
	}{
		{"komşunun var olan varsayılan kullanıcısı (asıl istismar)", dboYeniDB("saldiri", "x_db", "SaldirganParola123")},
		{"komşunun ad-alanında henüz olmayan kullanıcı (kapma)", dboYeniDB("saldiri2", "x_yeni", "SaldirganParola123")},
		{"komşunun ad-alanında DB adı", dboYeniDB("x_main2", "kendi1", "SaldirganParola123")},
		{"panelde kayıtlı kullanıcı adı", dboYeniDB("saldiri3", "kayitli", "SaldirganParola123")},
		{"MariaDB'de var olan kayıtsız kullanıcı", dboYeniDB("saldiri4", "kayitsiz", "SaldirganParola123")},
		{"panelde kayıtsız var olan DB", dboYeniDB("varolan", "kendi2", "SaldirganParola123")},
	} {
		o.ddlSifirla(t)
		kod, govde := o.istek(t, http.MethodPost, "/domains/1/databases", c.govde)
		if kod != http.StatusConflict {
			t.Errorf("%s: 409 beklenir, alınan %d %s", c.ad, kod, govde)
		}
		if d := o.ddl(t); len(d) > 0 {
			t.Errorf("%s: reddedilen istek MariaDB'de DDL çalıştırdı: %q", c.ad, d)
		}
		if strings.Contains(govde, on+"-x.test") || strings.Contains(govde, "c_"+on+"_x") {
			t.Errorf("%s: yanıt komşunun sistem kullanıcısını/alan adını sızdırıyor: %s", c.ad, govde)
		}
	}
	if !o.baglanir(kurban, kurbanPw, kurbanDB) {
		t.Errorf("komşunun parolası değişti")
	}
	if !o.baglanir(on+"_kayitsiz", "KayitsizParola123", "") {
		t.Errorf("kayıtsız var olan hesabın parolası değişti")
	}
	for _, k := range []string{on + "_x_yeni", on + "_kendi1", on + "_kendi2", on + "_kayitli"} {
		if o.kullaniciVar(t, k) {
			t.Errorf("reddedilen istek kullanıcı %q oluşturdu", k)
		}
	}
	for _, d := range []string{on + "_saldiri", on + "_saldiri2", on + "_x_main2", on + "_saldiri3", on + "_saldiri4"} {
		if o.dbVar(t, d) {
			t.Errorf("reddedilen istek veritabanı %q oluşturdu", d)
		}
	}
	var n int
	_ = o.h.DB.QueryRow(`SELECT COUNT(*) FROM db_accounts WHERE domain_id=1 AND db_name<>?`, on+"_kayitdb").Scan(&n)
	if n != 0 {
		t.Errorf("reddedilen istekler db_accounts'a %d satır yazdı", n)
	}
}

// Meşru kullanım bozulmaz: kısa ve uzun önekli kiracı kendi ad-alanında yeni DB +
// kullanıcı açar; "mevcut kullanıcı" modu kendi kullanıcısına DB bağlar.
func TestCreateDatabase_MesruKullanim(t *testing.T) {
	o := dbo_ortam(t)
	on := o.on
	o.kurbanKur(t, "KurbanParola123")

	kod, govde := o.istek(t, http.MethodPost, "/domains/1/databases", dboYeniDB("site", "site", "SiteParola12345"))
	if kod != http.StatusCreated {
		t.Fatalf("kısa önekli kiracı kendi DB'sini açamadı: %d %s", kod, govde)
	}
	if !o.baglanir(on+"_site", "SiteParola12345", on+"_site") {
		t.Errorf("oluşturulan kullanıcı kendi DB'sine bağlanamıyor")
	}
	// "<on>_xy" komşunun ("<on>_x") ad-alanında DEĞİL.
	if kod, govde := o.istek(t, http.MethodPost, "/domains/1/databases", dboYeniDB("xy", "xy", "SiteParola12345")); kod != http.StatusCreated {
		t.Errorf("komşuya düşmeyen ad (xy) reddedildi: %d %s", kod, govde)
	}
	// Uzun önekli kiracı kendi ad-alanında (kısa önekli komşunun kökü onu engellemez).
	if kod, govde := o.istek(t, http.MethodPost, "/domains/2/databases", dboYeniDB("blog", "blog", "BlogParola12345")); kod != http.StatusCreated {
		t.Errorf("uzun önekli kiracı kendi DB'sini açamadı: %d %s", kod, govde)
	}
	// Mevcut kullanıcı modu.
	kod, govde = o.istek(t, http.MethodPost, "/domains/1/databases", map[string]any{
		"kullanici_tipi": "mevcut", "db_sonek": "site2", "mevcut_kullanici": on + "_site"})
	if kod != http.StatusCreated {
		t.Fatalf("mevcut kullanıcıya DB bağlanamadı: %d %s", kod, govde)
	}
	if !o.baglanir(on+"_site", "SiteParola12345", on+"_site2") {
		t.Errorf("mevcut kullanıcı bağlanan DB'ye erişemiyor")
	}
	// Otomatik mod (gövde boş) — eski davranış.
	if kod, govde := o.istek(t, http.MethodPost, "/domains/1/databases", map[string]any{}); kod != http.StatusCreated {
		t.Errorf("otomatik mod bozuldu: %d %s", kod, govde)
	}
}

// Kullanıcısı olmayan (yedekten dönen) DB'ye kullanıcı ekleme ve parola değiştirme.
func TestSetDatabasePassword_VarOlanHesabiDevralamaz(t *testing.T) {
	o := dbo_ortam(t)
	on := o.on
	kurbanPw := "KurbanParola123"
	kurban, _ := o.kurbanKur(t, kurbanPw)
	geriDB := on + "_geri"
	o.kokExec(t, "CREATE DATABASE `"+geriDB+"`")
	res, err := o.h.DB.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (1,?,'','')`, geriDB)
	if err != nil {
		t.Fatal(err)
	}
	dbid, _ := res.LastInsertId()
	yol := fmt.Sprintf("/databases/%d/password", dbid)
	o.kokExec(t, "CREATE USER '"+on+"_kayitsiz'@'localhost' IDENTIFIED BY 'KayitsizParola123'")

	for _, c := range []struct{ ad, kul string }{
		{"MariaDB'de var olan kayıtsız hesap", on + "_kayitsiz"},
		{"komşunun ad-alanı (kapma)", on + "_x_kapma"},
	} {
		o.ddlSifirla(t)
		kod, govde := o.istek(t, http.MethodPut, yol, map[string]any{"parola": "SaldirganParola123", "kullanici": c.kul})
		if kod != http.StatusConflict {
			t.Errorf("%s: 409 beklenir, alınan %d %s", c.ad, kod, govde)
		}
		if d := o.ddl(t); len(d) > 0 {
			t.Errorf("%s: reddedilen istek MariaDB'de DDL çalıştırdı: %q", c.ad, d)
		}
	}
	if !o.baglanir(on+"_kayitsiz", "KayitsizParola123", "") {
		t.Errorf("var olan hesabın parolası değişti")
	}
	if o.kullaniciVar(t, on+"_x_kapma") {
		t.Errorf("komşunun ad-alanında kullanıcı oluşturuldu")
	}
	// Meşru: kendi ad-alanında yeni kullanıcı.
	if kod, govde := o.istek(t, http.MethodPut, yol, map[string]any{"parola": "GeriParola12345", "kullanici": on + "_geriu"}); kod != http.StatusOK {
		t.Fatalf("meşru kullanıcı ekleme düştü: %d %s", kod, govde)
	}
	if !o.baglanir(on+"_geriu", "GeriParola12345", geriDB) {
		t.Errorf("eklenen kullanıcı DB'ye bağlanamıyor")
	}

	// Eski devralma kaydı: domain 1'in satırında komşunun kullanıcısı → parola değişmez.
	res, err = o.h.DB.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (1,?,?,'x')`,
		on+"_eskidevralma", kurban)
	if err != nil {
		t.Fatal(err)
	}
	eskiID, _ := res.LastInsertId()
	if kod, govde := o.istek(t, http.MethodPut, fmt.Sprintf("/databases/%d/password", eskiID),
		map[string]any{"parola": "SaldirganParola123"}); kod == http.StatusOK {
		t.Errorf("eski devralma satırı üzerinden komşunun parolası değişti: %d %s", kod, govde)
	}
	if !o.baglanir(kurban, kurbanPw, "") {
		t.Errorf("komşunun parolası değişti")
	}
	// Meşru parola değişimi (kendi kullanıcısı).
	if kod, govde := o.istek(t, http.MethodPut, yol, map[string]any{"parola": "YeniGeriParola123"}); kod != http.StatusOK {
		t.Fatalf("kendi kullanıcısının parolası değiştirilemedi: %d %s", kod, govde)
	}
	if !o.baglanir(on+"_geriu", "YeniGeriParola123", geriDB) {
		t.Errorf("yeni parola uygulanmadı")
	}
}
