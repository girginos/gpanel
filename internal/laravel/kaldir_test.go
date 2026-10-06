package laravel

// kaldir_test.go — KaydiKaldir/Kaldir: Laravel uygulaması panelden kaldırılınca cron silinir,
// web_root YALNIZ <app>/public ise public_html'e döner (elle ayarlıya dokunulmaz), kayıt
// silinir; geri alma başarısızsa kayıt KALIR. DB bellek içi kayıt tutan sahte sürücü; vhost
// yeniden kurma sahte; ev/cron dizinleri t.TempDir().

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"girginospanel/internal/belgekok"
)

const kaldirSurucuAdi = "laravel-kaldir-sahte"

func init() { sql.Register(kaldirSurucuAdi, kaldirSurucu{}) }

// kaldirDurum: sahte DB'nin tek paylaşılan durumu (testler sıralı).
var kaldirDurum struct {
	mu       sync.Mutex
	kayit    bool   // cp_laravel_apps satırı var mı
	appRoot  string // kayıttaki app_root
	durum    string // son_deploy_durum
	webRoot  string // domains.web_root
	execler  []string
	execHata string // bu alt dizgiyi içeren Exec hata verir
}

type kaldirSurucu struct{}

func (kaldirSurucu) Open(string) (driver.Conn, error) { return kaldirBag{}, nil }

type kaldirBag struct{}

func (kaldirBag) Prepare(string) (driver.Stmt, error) { return nil, errors.New("Prepare yok") }
func (kaldirBag) Close() error                        { return nil }
func (kaldirBag) Begin() (driver.Tx, error)           { return nil, errors.New("işlem yok") }

func (kaldirBag) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	kaldirDurum.mu.Lock()
	defer kaldirDurum.mu.Unlock()
	switch {
	case strings.Contains(q, "FROM cp_laravel_apps"):
		if !kaldirDurum.kayit {
			return &sahteSatirlar{kolon: []string{"x"}}, nil
		}
		return &sahteSatirlar{
			kolon: []string{"app_root", "deploy_mode", "php_surum", "node_surum", "schedule_enabled", "queue_enabled",
				"queue_timeout", "queue_max_jobs", "queue_connection", "maintenance", "last_commit", "son_deploy_durum"},
			veri: [][]driver.Value{{kaldirDurum.appRoot, "yerel", "8.3", "", int64(1), int64(0), int64(60), int64(1000), "database", int64(0), "", kaldirDurum.durum}},
		}, nil
	case strings.Contains(q, "web_root") && strings.Contains(q, "FROM domains"):
		return &sahteSatirlar{kolon: []string{"web_root"}, veri: [][]driver.Value{{kaldirDurum.webRoot}}}, nil
	case strings.Contains(q, "FROM domains"):
		return &sahteSatirlar{kolon: []string{"a", "b", "c"}, veri: [][]driver.Value{{"c_test", "8.3", int64(0)}}}, nil
	}
	return &sahteSatirlar{kolon: []string{"x"}}, nil
}

func (kaldirBag) ExecContext(_ context.Context, q string, a []driver.NamedValue) (driver.Result, error) {
	kaldirDurum.mu.Lock()
	defer kaldirDurum.mu.Unlock()
	if kaldirDurum.execHata != "" && strings.Contains(q, kaldirDurum.execHata) {
		return nil, errors.New("sahte exec hatası")
	}
	if strings.HasPrefix(q, "UPDATE domains SET web_root") && len(a) > 0 {
		kaldirDurum.webRoot, _ = a[0].Value.(string)
	}
	kaldirDurum.execler = append(kaldirDurum.execler, q)
	return driver.RowsAffected(1), nil
}

type kaldirOrtam struct {
	e    sahteEv
	db   *sql.DB
	cron string
	rr   int
	rerr error
}

func kaldirKur(t *testing.T) *kaldirOrtam {
	t.Helper()
	e := sahteKok(t, kiraciUID, kiraciGID)
	if g, err := filepath.EvalSymlinks(evKoku); err == nil {
		e.home = filepath.Join(g, filepath.Base(e.home))
		evKoku = g
	}
	e.app = filepath.Join(e.home, "public_html")
	db, err := sql.Open(kaldirSurucuAdi, "x")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	o := &kaldirOrtam{e: e, db: db}
	eskiCron, eskiRR := cronDizini, belgekok.RerenderFn
	cronDizini = t.TempDir()
	o.cron = cronDPath(7)
	belgekok.RerenderFn = func(*sql.DB, int64) error { o.rr++; return o.rerr }
	t.Cleanup(func() { cronDizini, belgekok.RerenderFn = eskiCron, eskiRR })
	kaldirDurum.mu.Lock()
	kaldirDurum.kayit, kaldirDurum.appRoot, kaldirDurum.durum = true, "public_html", "hazir"
	kaldirDurum.webRoot, kaldirDurum.execler, kaldirDurum.execHata = "", nil, ""
	kaldirDurum.mu.Unlock()
	dosyaYaz(t, o.cron, "* * * * * x\n", 0o644)
	return o
}

func kaldirExecler() []string {
	kaldirDurum.mu.Lock()
	defer kaldirDurum.mu.Unlock()
	return append([]string(nil), kaldirDurum.execler...)
}

func kaldirSay(parca string) int {
	n := 0
	for _, q := range kaldirExecler() {
		if strings.Contains(q, parca) {
			n++
		}
	}
	return n
}

func TestKaydiKaldir_WebRootAppPublic_GeriAlinir_KayitVeCronSilinir(t *testing.T) {
	o := kaldirKur(t)
	kaldirDurum.webRoot = o.e.app + "/public"
	geri, err := KaydiKaldir(context.Background(), o.db, 7, "c_test")
	if err != nil || !geri {
		t.Fatalf("geri=%v err=%v", geri, err)
	}
	if kaldirDurum.webRoot != o.e.app || o.rr != 1 {
		t.Fatalf("web_root public_html olmalı: %q rr=%d", kaldirDurum.webRoot, o.rr)
	}
	if kaldirSay("DELETE FROM cp_laravel_apps") != 1 {
		t.Fatalf("kayıt silinmeli: %v", kaldirExecler())
	}
	if _, err := os.Stat(o.cron); err == nil {
		t.Fatal("cron dosyası silinmeli")
	}
}

func TestKaydiKaldir_ElleAyarliWebRoot_DokunulmazKayitSilinir(t *testing.T) {
	o := kaldirKur(t)
	kaldirDurum.webRoot = o.e.app + "/ozel"
	geri, err := KaydiKaldir(context.Background(), o.db, 7, "c_test")
	if err != nil || geri {
		t.Fatalf("geri=%v err=%v", geri, err)
	}
	if kaldirDurum.webRoot != o.e.app+"/ozel" || o.rr != 0 || kaldirSay("UPDATE domains") != 0 {
		t.Fatalf("elle ayar korunmalı: %q rr=%d", kaldirDurum.webRoot, o.rr)
	}
	if kaldirSay("DELETE FROM cp_laravel_apps") != 1 {
		t.Fatal("kayıt yine de silinmeli")
	}
}

func TestKaydiKaldir_AltDizinliUygulama(t *testing.T) {
	o := kaldirKur(t)
	kaldirDurum.appRoot = "public_html/x"
	_ = os.MkdirAll(o.e.app+"/x/public", 0o755)
	kaldirDurum.webRoot = o.e.app + "/x/public"
	geri, err := KaydiKaldir(context.Background(), o.db, 7, "c_test")
	if err != nil || !geri || kaldirDurum.webRoot != o.e.app {
		t.Fatalf("geri=%v err=%v %q", geri, err, kaldirDurum.webRoot)
	}
}

func TestKaydiKaldir_KayitYok_HicbirSeyYapmaz(t *testing.T) {
	o := kaldirKur(t)
	kaldirDurum.kayit = false
	geri, err := KaydiKaldir(context.Background(), o.db, 7, "c_test")
	if err != nil || geri || len(kaldirExecler()) != 0 {
		t.Fatalf("geri=%v err=%v exec=%v", geri, err, kaldirExecler())
	}
	if _, err := os.Stat(o.cron); err != nil {
		t.Fatal("kayıt yokken cron'a dokunulmamalı")
	}
}

func TestKaydiKaldir_RenderHatasi_KayitKalir(t *testing.T) {
	o := kaldirKur(t)
	kaldirDurum.webRoot = o.e.app + "/public"
	o.rerr = errors.New("nginx -t")
	if _, err := KaydiKaldir(context.Background(), o.db, 7, "c_test"); err == nil {
		t.Fatal("hata beklenirdi")
	}
	if kaldirSay("DELETE FROM cp_laravel_apps") != 0 {
		t.Fatal("geri alma başarısızsa kayıt SİLİNMEMELİ (yeniden denenebilir)")
	}
	if kaldirDurum.webRoot != o.e.app+"/public" {
		t.Fatalf("web_root eski değere dönmeli: %q", kaldirDurum.webRoot)
	}
}

func TestKaydiKaldir_GecersizKullanici(t *testing.T) {
	o := kaldirKur(t)
	if _, err := KaydiKaldir(context.Background(), o.db, 7, "root"); err == nil {
		t.Fatal("c_ olmayan kullanıcı reddedilmeli")
	}
	if len(kaldirExecler()) != 0 {
		t.Fatal("yan etki olmamalı")
	}
}

func TestKaldirHandler_409IslemSurerken_404KayitYok(t *testing.T) {
	o := kaldirKur(t)
	h := &Handlers{DB: o.db}
	kaldirDurum.durum = "kuruluyor"
	if w := handlerIstegi(h.Kaldir, http.MethodDelete, ""); w.Code != http.StatusConflict {
		t.Fatalf("işlem sürerken 409: %d %s", w.Code, w.Body.String())
	}
	if kaldirSay("DELETE") != 0 {
		t.Fatal("kayıt silinmemeli")
	}
	kaldirDurum.kayit = false
	if w := handlerIstegi(h.Kaldir, http.MethodDelete, ""); w.Code != http.StatusNotFound {
		t.Fatalf("kayıt yoksa 404: %d", w.Code)
	}
}
