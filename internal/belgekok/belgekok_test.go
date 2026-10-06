package belgekok

// belgekok_test.go — karar tablosu (Uyumlu/UrlYolu/Degerlendir), güvenli düzeltme (Duzelt) ve
// Laravel kaldırınca geri alma (GeriAl). DB'li testler YALNIZ geçici MariaDB
// (GOSP_TEST_GECICI_DSN; go_test db:true), yoksa ATLANIR; canlı DB'ye gidilmez. Vhost
// yeniden kurma sahte; dosya sistemi t.TempDir().

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func TestUyumluVeUrlYolu(t *testing.T) {
	b := "/home/c_x/public_html"
	for _, c := range []struct {
		servis, hedef string
		uyumlu        bool
		yol           string
	}{
		{b, b, true, ""},
		{b, b + "/blog", true, "blog"},
		{b + "/blog", b + "/blog", true, ""},
		{b + "/public", b, false, ""},             // hedef servis kökünün ÜSTÜNDE
		{b + "/public", b + "/blog", false, ""},   // kardeş
		{b + "/blog", b + "/blog2", false, ""},    // önek benzerliği uyum DEĞİL
		{b + "/blog", b + "/blog/ic", true, "ic"}, // altında
	} {
		if got := Uyumlu(c.servis, c.hedef); got != c.uyumlu {
			t.Errorf("Uyumlu(%s,%s)=%v", c.servis, c.hedef, got)
		}
		if got := UrlYolu(c.servis, c.hedef); got != c.yol {
			t.Errorf("UrlYolu(%s,%s)=%q", c.servis, c.hedef, got)
		}
	}
	if g := Goreli(b, b+"/public"); g != "public_html/public" {
		t.Errorf("Goreli=%q", g)
	}
}

func bk_db(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li test atlandı")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Net != "unix" || cfg.Addr == "" {
		t.Skip("yalnız unix soketli geçici DSN desteklenir")
	}
	switch filepath.Clean(cfg.Addr) {
	case "/var/lib/mysql/mysql.sock", "/run/mysqld/mysqld.sock", "/var/run/mysqld/mysqld.sock", "/tmp/mysql.sock":
		t.Fatalf("GOSP_TEST_GECICI_DSN canlı MariaDB soketini gösteriyor — reddedildi")
	}
	cfg.DBName = ""
	kok, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = kok.Close() })
	ad := fmt.Sprintf("gosp_test_bk_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad) })
	cfg.DBName = ad
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, q := range []string{
		`CREATE TABLE domains (id BIGINT UNSIGNED PRIMARY KEY, sistem_kullanici VARCHAR(64) NOT NULL, web_root VARCHAR(255) NOT NULL DEFAULT '')`,
		`CREATE TABLE cp_laravel_apps (domain_id BIGINT UNSIGNED PRIMARY KEY, app_root VARCHAR(255) NOT NULL DEFAULT '', son_deploy_durum VARCHAR(20) NOT NULL DEFAULT '')`,
		`INSERT INTO domains (id, sistem_kullanici) VALUES (1,'c_a'),(2,'c_b')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

type bkEv struct {
	db   *sql.DB
	home string
	base string
	rr   int
	err  error
}

func bk_ev(t *testing.T) *bkEv {
	t.Helper()
	e := &bkEv{db: bk_db(t)}
	e.home = filepath.Join(t.TempDir(), "c_a")
	e.base = e.home + "/public_html"
	if err := os.MkdirAll(e.base, 0o755); err != nil {
		t.Fatal(err)
	}
	eski := RerenderFn
	RerenderFn = func(*sql.DB, int64) error { e.rr++; return e.err }
	t.Cleanup(func() { RerenderFn = eski })
	return e
}

func (e *bkEv) wr(t *testing.T, id int) string {
	t.Helper()
	var w string
	if err := e.db.QueryRow(`SELECT web_root FROM domains WHERE id=?`, id).Scan(&w); err != nil {
		t.Fatal(err)
	}
	return w
}

func (e *bkEv) set(t *testing.T, id int, w string) {
	t.Helper()
	if _, err := e.db.Exec(`UPDATE domains SET web_root=? WHERE id=?`, w, id); err != nil {
		t.Fatal(err)
	}
}

func TestDegerlendir_KararTablosu(t *testing.T) {
	e := bk_ev(t)
	ctx := context.Background()
	app := func(ar string) {
		_, _ = e.db.Exec(`DELETE FROM cp_laravel_apps`)
		if ar != "-" {
			if _, err := e.db.Exec(`INSERT INTO cp_laravel_apps (domain_id, app_root) VALUES (1,?)`, ar); err != nil {
				t.Fatal(err)
			}
		}
	}
	// 1) varsayılan kök → uyumlu
	k := Degerlendir(ctx, e.db, 1, e.base, e.base)
	if !k.Uyumlu || k.Duzeltilebilir {
		t.Fatalf("varsayılan: %+v", k)
	}
	// 2) bayat web_root (dizin yok) → düzeltilebilir, Laravel kaydı yok → LaravelBayat=false
	e.set(t, 1, e.base+"/public")
	app("-")
	k = Degerlendir(ctx, e.db, 1, e.base, e.base)
	if k.Uyumlu || !k.Duzeltilebilir || k.LaravelBayat || k.Sebep != "web_root_yok" {
		t.Fatalf("bayat: %+v", k)
	}
	// 3) bayat + Laravel kaydı artisan yok → LaravelBayat
	app("public_html")
	k = Degerlendir(ctx, e.db, 1, e.base, e.base)
	if !k.Duzeltilebilir || !k.LaravelBayat {
		t.Fatalf("bayat+laravel: %+v", k)
	}
	// 4) servis dizini var + artisan var → düzeltilemez (canlı Laravel)
	_ = os.MkdirAll(e.base+"/public", 0o755)
	_ = os.WriteFile(e.base+"/artisan", []byte("x"), 0o644)
	k = Degerlendir(ctx, e.db, 1, e.base, e.base)
	if k.Uyumlu || k.Duzeltilebilir {
		t.Fatalf("canlı Laravel düzeltilemez olmalı: %+v", k)
	}
	// 5) artisan siliniyor → düzeltilebilir (servis dizini var ama uygulama yok)
	_ = os.Remove(e.base + "/artisan")
	k = Degerlendir(ctx, e.db, 1, e.base, e.base)
	if !k.Duzeltilebilir || !k.LaravelBayat || k.Sebep != "laravel_silinmis" {
		t.Fatalf("artisan yok: %+v", k)
	}
	// 6) app_root ev dışına kaçıyor ('..') → güvenle değerlendirilemez → canlı say (düzeltilemez)
	app("../c_b/public_html")
	k = Degerlendir(ctx, e.db, 1, e.base, e.base)
	if k.Duzeltilebilir {
		t.Fatalf("ev dışı app_root düzeltilebilir sayılmamalı: %+v", k)
	}
	// 7) elle ayarlı web_root (dizin var, Laravel yok) → düzeltilemez
	app("-")
	_ = os.MkdirAll(e.base+"/ozel", 0o755)
	e.set(t, 1, e.base+"/ozel")
	if k = Degerlendir(ctx, e.db, 1, e.base, e.base); k.Duzeltilebilir || k.Uyumlu {
		t.Fatalf("elle ayarlı: %+v", k)
	}
	// 7b) İLİŞKİSİZ bilinçli web_root: servis dizini var (landing), eski Laravel kaydı artisan'sız
	// ama başka dizinde → düzeltilemez (landing ezilmez).
	_ = os.MkdirAll(e.base+"/landing", 0o755)
	e.set(t, 1, e.base+"/landing")
	app("laravel_eski")
	if k = Degerlendir(ctx, e.db, 1, e.base, e.base); k.Duzeltilebilir || k.LaravelBayat {
		t.Fatalf("ilişkisiz web_root + bayat kayıt düzeltilmemeli: %+v", k)
	}
	// 7c) CANLI Laravel + servis dizini YOK → düzeltilemez (kayıt/cron/kuyruk yetim kalmasın).
	_ = os.WriteFile(e.base+"/artisan", []byte("x"), 0o644)
	e.set(t, 1, e.base+"/olmayan")
	app("public_html")
	if k = Degerlendir(ctx, e.db, 1, e.base, e.base); k.Duzeltilebilir {
		t.Fatalf("canlı Laravel + yok dizin düzeltilmemeli: %+v", k)
	}
	_ = os.Remove(e.base + "/artisan")
	// 7d) servis dizini yok + artisan'sız Laravel + web_root ilişkisiz ama dizin YOK → düzeltilir.
	if k = Degerlendir(ctx, e.db, 1, e.base, e.base); !k.Duzeltilebilir || !k.LaravelBayat {
		t.Fatalf("yok dizin + artisansız kayıt düzeltilmeli: %+v", k)
	}
	// 7e) kurulum/dağıtım sürüyor → düzeltilemez + LaravelMesgul
	for _, d := range []string{"kuruluyor", "calisiyor"} {
		_, _ = e.db.Exec(`UPDATE cp_laravel_apps SET son_deploy_durum=? WHERE domain_id=1`, d)
		if k = Degerlendir(ctx, e.db, 1, e.base, e.base); k.Duzeltilebilir || !k.LaravelMesgul {
			t.Fatalf("%s: meşgul, düzeltme yok: %+v", d, k)
		}
	}
	_, _ = e.db.Exec(`UPDATE cp_laravel_apps SET son_deploy_durum='' WHERE domain_id=1`)
	// 7f) Laravel tablosu okunamıyor (satır yokluğu DIŞI hata) → muhafazakâr: düzeltme yok
	_, _ = e.db.Exec(`ALTER TABLE cp_laravel_apps RENAME TO cp_laravel_apps_x`)
	if k = Degerlendir(ctx, e.db, 1, e.base, e.base); k.Duzeltilebilir {
		t.Fatalf("okunamayan Laravel kaydında düzeltme yok: %+v", k)
	}
	_, _ = e.db.Exec(`ALTER TABLE cp_laravel_apps_x RENAME TO cp_laravel_apps`)
	app("-")
	// 8) kurcalanmış web_root (hesap dışı) → tabana düşer → uyumlu
	e.set(t, 1, "/etc")
	if k = Degerlendir(ctx, e.db, 1, e.base, e.base); !k.Uyumlu {
		t.Fatalf("kurcalanmış değer tabana düşmeli: %+v", k)
	}
}

func TestDuzelt_YazarVeRenderEder_SahipDogrulanir(t *testing.T) {
	e := bk_ev(t)
	ctx := context.Background()
	e.set(t, 1, e.base+"/public")
	if err := Duzelt(ctx, e.db, 1, "c_a", e.base, e.base+"/public"); err != nil {
		t.Fatal(err)
	}
	if e.wr(t, 1) != e.base || e.rr != 1 {
		t.Fatalf("web_root=%q rr=%d", e.wr(t, 1), e.rr)
	}
	// IDOR: sahibi olmayan hesap adıyla başka satır DEĞİŞMEZ
	e.set(t, 2, "/x/yabanci")
	if err := Duzelt(ctx, e.db, 2, "c_a", e.base, "/x/yabanci"); err == nil {
		t.Fatal("başka hesabın satırı için hata beklenirdi")
	}
	if e.wr(t, 2) != "/x/yabanci" || e.rr != 1 {
		t.Fatalf("yabancı satır değişti ya da render edildi: %q rr=%d", e.wr(t, 2), e.rr)
	}
}

func TestDuzelt_RenderHatasi_DBGeriAlinir(t *testing.T) {
	e := bk_ev(t)
	e.set(t, 1, e.base+"/public")
	e.err = errors.New("nginx -t")
	if err := Duzelt(context.Background(), e.db, 1, "c_a", e.base, e.base+"/public"); err == nil {
		t.Fatal("hata beklenirdi")
	}
	if e.wr(t, 1) != e.base+"/public" {
		t.Fatalf("DB geri alınmalı: %q", e.wr(t, 1))
	}
}

func TestDuzelt_SembolikBagVeOlmayanTaban_Reddedilir(t *testing.T) {
	e := bk_ev(t)
	e.set(t, 1, e.base+"/public")
	// public_html bir symlink (başka hesabın/dışarıya) → reddedilir, DB'ye yazılmaz, render yok
	hedef := filepath.Join(t.TempDir(), "disari")
	_ = os.MkdirAll(hedef, 0o755)
	_ = os.RemoveAll(e.base)
	if err := os.Symlink(hedef, e.base); err != nil {
		t.Fatal(err)
	}
	if err := Duzelt(context.Background(), e.db, 1, "c_a", e.base, e.base+"/public"); err == nil {
		t.Fatal("symlink taban reddedilmeli")
	}
	// taban yok
	_ = os.Remove(e.base)
	if err := Duzelt(context.Background(), e.db, 1, "c_a", e.base, e.base+"/public"); err == nil {
		t.Fatal("olmayan taban reddedilmeli")
	}
	if e.wr(t, 1) != e.base+"/public" || e.rr != 0 {
		t.Fatalf("değişmemeli: %q rr=%d", e.wr(t, 1), e.rr)
	}
}

// (e) Laravel kaldırılınca: web_root == <app>/public → varsayılana; aksi halde DOKUNMA.
func TestGeriAl(t *testing.T) {
	e := bk_ev(t)
	ctx := context.Background()
	app := e.base
	// web_root = app/public → geri alınır
	_ = os.MkdirAll(app+"/public", 0o755)
	e.set(t, 1, app+"/public")
	geri, err := GeriAl(ctx, e.db, 1, "c_a", e.base, app)
	if err != nil || !geri || e.wr(t, 1) != e.base || e.rr != 1 {
		t.Fatalf("geri=%v err=%v web_root=%q rr=%d", geri, err, e.wr(t, 1), e.rr)
	}
	// elle başka yere ayarlı → dokunma
	_ = os.MkdirAll(e.base+"/ozel", 0o755)
	e.set(t, 1, e.base+"/ozel")
	geri, err = GeriAl(ctx, e.db, 1, "c_a", e.base, app)
	if err != nil || geri || e.wr(t, 1) != e.base+"/ozel" || e.rr != 1 {
		t.Fatalf("elle ayar korunmalı: geri=%v err=%v %q rr=%d", geri, err, e.wr(t, 1), e.rr)
	}
	// boş / zaten varsayılan → dokunma
	e.set(t, 1, "")
	if geri, _ = GeriAl(ctx, e.db, 1, "c_a", e.base, app); geri || e.wr(t, 1) != "" || e.rr != 1 {
		t.Fatal("boş web_root'a dokunulmamalı")
	}
	// alt dizinli uygulama: app=public_html/x, web_root=public_html/x/public
	_ = os.MkdirAll(e.base+"/x/public", 0o755)
	e.set(t, 1, e.base+"/x/public")
	if geri, err = GeriAl(ctx, e.db, 1, "c_a", e.base, e.base+"/x"); err != nil || !geri || e.wr(t, 1) != e.base {
		t.Fatalf("alt dizinli: %v %v %q", geri, err, e.wr(t, 1))
	}
	// başka başka uygulamanın public'i (web_root=public_html/y/public, app=public_html/x) → dokunma
	_ = os.MkdirAll(e.base+"/y/public", 0o755)
	e.set(t, 1, e.base+"/y/public")
	if geri, _ = GeriAl(ctx, e.db, 1, "c_a", e.base, e.base+"/x"); geri || e.wr(t, 1) != e.base+"/y/public" {
		t.Fatal("başka uygulamanın kökü geri alınmamalı")
	}
	// IDOR: yanlış sk → satır bulunamaz, değişmez
	e.set(t, 2, "/b/public_html/public")
	// Hata TÜRÜ önemli: GeriAl'in kendi sahip kapısı (sql.ErrNoRows) devrede olmalı; yalnız
	// Duzelt'in ikinci kapısına ya da taban doğrulamasına güvenilmez.
	if _, err = GeriAl(ctx, e.db, 2, "c_a", e.base, "/b/public_html"); !errors.Is(err, sql.ErrNoRows) || e.wr(t, 2) != "/b/public_html/public" {
		t.Fatalf("yabancı hesap satırı değişmemeli ve sahip kapısı ErrNoRows vermeli: %v", err)
	}
}

// CAS: karar anındaki değer değişmişse ezilmez (ErrDegisti), render edilmez; zaten istenen
// değerdeyse sessizce başarılı.
func TestDuzelt_CompareAndSwap_DegisenWebRootEzilmez(t *testing.T) {
	e := bk_ev(t)
	ctx := context.Background()
	e.set(t, 1, e.base+"/baska") // karar anında e.base+"/public" idi; arada başkası değiştirdi
	err := Duzelt(ctx, e.db, 1, "c_a", e.base, e.base+"/public")
	if !errors.Is(err, ErrDegisti) || e.wr(t, 1) != e.base+"/baska" || e.rr != 0 {
		t.Fatalf("ezilmemeli: err=%v web_root=%q rr=%d", err, e.wr(t, 1), e.rr)
	}
	// başkası tam istenen değere getirdiyse: hata değil, tekrar render yok
	e.set(t, 1, e.base)
	if err := Duzelt(ctx, e.db, 1, "c_a", e.base, e.base+"/public"); err != nil || e.rr != 0 {
		t.Fatalf("zaten istenen değer: err=%v rr=%d", err, e.rr)
	}
}

// Render hatasında geri alma da CAS: render sırasında başkası belge kökünü değiştirdiyse
// bizim geri almamız onu ezmez.
func TestDuzelt_RenderHatasi_GeriAlmaBaskasininDegisikligiEzmez(t *testing.T) {
	e := bk_ev(t)
	e.set(t, 1, e.base+"/public")
	e.err = errors.New("nginx -t")
	RerenderFn = func(*sql.DB, int64) error { e.set(t, 1, e.base+"/baska"); return e.err }
	if err := Duzelt(context.Background(), e.db, 1, "c_a", e.base, e.base+"/public"); err == nil {
		t.Fatal("hata beklenirdi")
	}
	if e.wr(t, 1) != e.base+"/baska" {
		t.Fatalf("başkasının değişikliği ezilmemeli: %q", e.wr(t, 1))
	}
}

// Kilitlenme yok: Duzelt/GeriAl eşzamanlı (aynı domain) çağrılar zaman aşımsız biter; GeriAl
// içeriden Duzelt'in kilitsiz çekirdeğini çağırır (kilit yeniden alınmaz).
func TestKilit_EszamanliDuzeltGeriAl_KilitlenmeYok(t *testing.T) {
	e := bk_ev(t)
	ctx := context.Background()
	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		for i := 0; i < 12; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				if i%2 == 0 {
					_ = Duzelt(ctx, e.db, 1, "c_a", e.base, "")
				} else {
					_, _ = GeriAl(ctx, e.db, 1, "c_a", e.base, e.base)
				}
			}(i)
		}
		wg.Wait()
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("kilitlenme: eşzamanlı Duzelt/GeriAl bitmedi")
	}
}
