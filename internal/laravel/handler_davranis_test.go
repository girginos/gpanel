package laravel

// handler_davranis_test.go — env (GET/PUT) ve schedule (POST) handler'larının davranışı.
// Dikişler sahtelenir: envYazici (runuser/tee ÇALIŞMAZ), cronDizini (t.TempDir; /etc/cron.d
// DEĞİL), evKoku/kimlikBul (sahte ev). DB bellek içi sahte database/sql sürücüsüdür (canlı
// ya da geçici MariaDB YOK; yazmalar yok sayılır). Kilitlenen:
//   - uygulama dizini ev içi symlink üzerinden gidiyorsa (.env güvenle okunamaz) EnvOku 409
//     verir (boş "yok" DEĞİL, içerik sızmaz) ve EnvYaz 409 verip yazıcıyı HİÇ çağırmaz —
//     aksi halde düzenleyicide boş görünen .env kaydedilip gerçeği ezilirdi;
//   - Schedule ~/logs'u yalnız ensureLogDir ile (kiracıya, 0750) hazırlar; ~/logs ev dışına
//     symlink ise hedefin sahipliği devredilmez; cron dosyası yalnız cronDizini altına yazılır
//     ve kapatınca silinir.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// ---- bellek içi sahte sürücü: yalnız lookup/getKayit okumaları ----

const sahteSurucuAdi = "laravel-env-sahte"

func init() { sql.Register(sahteSurucuAdi, sahteSurucu{}) }

// sahteSurucu: DSN = sistem kullanıcısı. domains → (sk, "8.3", is_demo=0);
// cp_laravel_apps → satır yok (getKayit varsayılanı AppRoot=public_html). Exec (UPDATE)
// hiçbir şey yazmadan başarı döner.
type sahteSurucu struct{}

func (sahteSurucu) Open(dsn string) (driver.Conn, error) { return sahteBag(dsn), nil }

type sahteBag string

func (sahteBag) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("sahte sürücü: Prepare yok")
}
func (sahteBag) Close() error              { return nil }
func (sahteBag) Begin() (driver.Tx, error) { return nil, errors.New("sahte sürücü: işlem yok") }

func (b sahteBag) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(q, "FROM domains") {
		return &sahteSatirlar{kolon: []string{"sistem_kullanici", "php_surum", "is_demo"},
			veri: [][]driver.Value{{string(b), "8.3", int64(0)}}}, nil
	}
	return &sahteSatirlar{kolon: []string{"yok"}}, nil
}

func (sahteBag) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return driver.RowsAffected(0), nil
}

type sahteSatirlar struct {
	kolon []string
	veri  [][]driver.Value
}

func (s *sahteSatirlar) Columns() []string { return s.kolon }
func (s *sahteSatirlar) Close() error      { return nil }
func (s *sahteSatirlar) Next(dest []driver.Value) error {
	if len(s.veri) == 0 {
		return io.EOF
	}
	copy(dest, s.veri[0])
	s.veri = s.veri[1:]
	return nil
}

// ---- yardımcılar ----

// envHandlerKur: sahte ev (evKoku gerçek yola çözülmüş: guvenliAppDir EvalSymlinks ile
// karşılaştırır) + sahte DB'li Handlers + kayıt tutan sahte envYazici.
func envHandlerKur(t *testing.T) (sahteEv, *Handlers, *[]yaziciCagri) {
	t.Helper()
	e := sahteKok(t, kiraciUID, kiraciGID) // Cleanup evKoku'yu geri alır
	gercek, err := filepath.EvalSymlinks(evKoku)
	if err != nil {
		t.Fatal(err)
	}
	evKoku = gercek
	e.home = filepath.Join(gercek, filepath.Base(e.home))
	e.app = filepath.Join(e.home, "public_html")

	db, err := sql.Open(sahteSurucuAdi, filepath.Base(e.home))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	var cagrilar []yaziciCagri
	eski := envYazici
	envYazici = func(sk, appDir, icerik string) error {
		cagrilar = append(cagrilar, yaziciCagri{sk, appDir, icerik})
		return nil
	}
	t.Cleanup(func() { envYazici = eski })
	return e, &Handlers{DB: db}, &cagrilar
}

type yaziciCagri struct{ sk, appDir, icerik string }

func handlerIstegi(h http.HandlerFunc, yontem, govde string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(yontem, "/domains/7/laravel/x", strings.NewReader(govde))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "7")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

func dosyaOkuT(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ---- testler ----

// Uygulama dizini (public_html) ev içi symlink: home/public_html -> surumler/v1.
// guvenliAppDir bunu kabul eder (hedef ev içinde), ama .env root tarafından symlink
// izlenmeden okunamaz (ELOOP) → iki handler da 409; yazıcı çağrılmaz; gerçek .env aynı.
func TestEnvHandler_EvIciSymlink_409VeYazmaz(t *testing.T) {
	e, h, cagrilar := envHandlerKur(t)
	gercekEnv := filepath.Join(e.home, "surumler", "v1", ".env")
	asil := "APP_KEY=base64:gercek\n" + gizliIcerik + "\n"
	dosyaYaz(t, gercekEnv, asil, 0o600)
	if err := os.Remove(e.app); err != nil { // sahteEvKur'un boş public_html dizini
		t.Fatal(err)
	}
	symlinkKur(t, filepath.Join("surumler", "v1"), e.app)

	w := handlerIstegi(h.EnvOku, http.MethodGet, "")
	if w.Code != http.StatusConflict {
		t.Fatalf("EnvOku: güvenle okunamayan .env için 409 bekleniyordu, %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), gizliIcerik) {
		t.Fatalf("EnvOku içerik sızdırdı: %s", w.Body.String())
	}

	w = handlerIstegi(h.EnvYaz, http.MethodPut, `{"icerik":"APP_KEY=\n"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("EnvYaz: güvensiz .env üzerine yazım için 409 bekleniyordu, %d: %s", w.Code, w.Body.String())
	}
	if len(*cagrilar) != 0 {
		t.Fatalf("EnvYaz güvensiz .env'de yazıcıyı çağırdı: %+v", *cagrilar)
	}
	if s := dosyaOkuT(t, gercekEnv); s != asil {
		t.Fatalf("gerçek .env değişti: %q", s)
	}
}

// Karşılaştırma: meşru düzenli .env okunur ve yazım yazıcıya (dikiş) doğru argümanlarla
// gider; .env yoksa "var:false" ve yazım serbest.
func TestEnvHandler_MesruOkuVeYaz(t *testing.T) {
	e, h, cagrilar := envHandlerKur(t)

	w := handlerIstegi(h.EnvOku, http.MethodGet, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"var":false`) {
		t.Fatalf(".env yokken 200 var:false bekleniyordu, %d: %s", w.Code, w.Body.String())
	}
	w = handlerIstegi(h.EnvYaz, http.MethodPut, `{"icerik":"APP_KEY=base64:ilk\n"}`)
	if w.Code != http.StatusOK || len(*cagrilar) != 1 {
		t.Fatalf(".env yokken yazım serbest olmalı: %d %s çağrı=%d", w.Code, w.Body.String(), len(*cagrilar))
	}

	dosyaYaz(t, filepath.Join(e.app, ".env"), "APP_KEY=base64:x\n", 0o600)
	w = handlerIstegi(h.EnvOku, http.MethodGet, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"var":true`) ||
		!strings.Contains(w.Body.String(), "APP_KEY=base64:x") {
		t.Fatalf("meşru .env okunamadı: %d %s", w.Code, w.Body.String())
	}
	w = handlerIstegi(h.EnvYaz, http.MethodPut, `{"icerik":"APP_KEY=base64:y\n"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("meşru .env yazılamadı: %d %s", w.Code, w.Body.String())
	}
	want := yaziciCagri{"c_test", e.app, "APP_KEY=base64:y\n"}
	if len(*cagrilar) != 2 || (*cagrilar)[1] != want {
		t.Fatalf("yazıcı çağrısı %+v, %+v bekleniyordu", *cagrilar, want)
	}
}

// sahteCron: cronDizini dikişini t.TempDir'e yönlendirir (Cleanup geri alır).
func sahteCron(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	eski := cronDizini
	cronDizini = d
	t.Cleanup(func() { cronDizini = eski })
	return d
}

// Schedule: ~/logs yalnız ensureLogDir ile (kiracıya, 0750) hazırlanır; ev dışına symlink'li
// ~/logs'un hedefi devredilmez; cron dosyası cronDizini altına yazılır, kapatınca silinir.
// Eski root os.MkdirAll/os.Chown/chown -R dönüşü ya da ensureLogDir'in silinmesi düşer.
func TestScheduleHandler_LogDiziniVeCron(t *testing.T) {
	kokGerekli(t) // kiracıya chown (Fchown 4242) root ister
	t.Run("logs yok: kiraciya yaratilir, cron yazilir, kapatinca silinir", func(t *testing.T) {
		e, h, _ := envHandlerKur(t)
		cron := sahteCron(t)
		w := handlerIstegi(h.Schedule, http.MethodPost, `{"aktif":true}`)
		if w.Code != http.StatusOK {
			t.Fatalf("Schedule açma: %d %s", w.Code, w.Body.String())
		}
		logs := filepath.Join(e.home, logAltDizin)
		if u := uidOf(t, logs, true); u != kiraciUID {
			t.Fatalf("~/logs uid=%d, kiracı (%d) bekleniyordu", u, kiraciUID)
		}
		if m := modOf(t, logs); !m.IsDir() || m.Perm() != 0o750 {
			t.Fatalf("~/logs %v, 0750 dizin bekleniyordu", m)
		}
		cf := filepath.Join(cron, "girginos-laravel-7")
		s := dosyaOkuT(t, cf)
		satir := "* * * * * c_test "
		hedef := filepath.Join(e.app, "artisan") + " schedule:run >> " + filepath.Join(logs, "laravel-schedule.log") + " 2>&1\n"
		if !strings.Contains(s, satir) || !strings.Contains(s, hedef) {
			t.Fatalf("cron içeriği beklenmedik: %q", s)
		}
		w = handlerIstegi(h.Schedule, http.MethodPost, `{"aktif":false}`)
		if w.Code != http.StatusOK {
			t.Fatalf("Schedule kapatma: %d %s", w.Code, w.Body.String())
		}
		if _, err := os.Lstat(cf); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("kapatınca cron dosyası silinmedi: %v", err)
		}
	})
	t.Run("logs ev disina symlink: hedef devredilmez", func(t *testing.T) {
		e, h, _ := envHandlerKur(t)
		sahteCron(t)
		hedef := filepath.Join(e.dis, "sistem-dizini")
		if err := os.MkdirAll(hedef, 0o755); err != nil {
			t.Fatal(err)
		}
		symlinkKur(t, hedef, filepath.Join(e.home, logAltDizin))
		w := handlerIstegi(h.Schedule, http.MethodPost, `{"aktif":true}`)
		if w.Code != http.StatusOK { // log dizini hazırlanamazsa yalnız günlüğe yazılır
			t.Fatalf("Schedule açma: %d %s", w.Code, w.Body.String())
		}
		if u := uidOf(t, hedef, false); u != 0 {
			t.Fatalf("ev dışındaki symlink hedefi kiracıya devredildi: uid=%d", u)
		}
		if m := modOf(t, filepath.Join(e.home, logAltDizin)); m&os.ModeSymlink == 0 {
			t.Fatalf("~/logs symlink'i değiştirildi: %v", m)
		}
	})
}
