package laravel

// aski_test.go — hosting HESABI askısı, Laravel adımı: kuyruk işleyicisi
// (Restart=always) askıda `disable --now`, askıdan alınca yalnız ayar açıksa
// reset-failed + `enable --now`; zamanlanmış görevin cron.d dosyası askıda cron.d
// dışında saklanır, askıdan alınca bayt bayt geri gelir; askıdaki hesaba işleyici
// ve görev AÇILAMAZ (409). Komut sahte, dizinler t.TempDir — systemd ve /etc'ye
// dokunulmaz. DB: sahtedb (bellek içi).

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"girginospanel/internal/sahtedb"
)

type laAskiDikis struct {
	o        askiOrtam
	komutlar *[]string
}

func laAskiKur(t *testing.T, kuyruk, gorev int64, unit bool) (laAskiDikis, *sql.DB) {
	t.Helper()
	kok := t.TempDir()
	d := laAskiDikis{komutlar: &[]string{}}
	d.o = askiOrtam{
		komut: func(ad string, arg ...string) ([]byte, error) {
			*d.komutlar = append(*d.komutlar, ad+" "+strings.Join(arg, " "))
			return nil, nil
		},
		unitDizini: filepath.Join(kok, "systemd"),
		cronDizini: filepath.Join(kok, "cron.d"),
		saklama:    filepath.Join(kok, "askida"),
	}
	for _, dz := range []string{d.o.unitDizini, d.o.cronDizini} {
		if err := os.MkdirAll(dz, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if unit {
		if err := os.WriteFile(filepath.Join(d.o.unitDizini, queueUnitName(7)+".service"), []byte("[Unit]\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sd, db := sahtedb.Yeni()
	t.Cleanup(func() { _ = db.Close() })
	sd.Satirlar("FROM cp_laravel_apps WHERE domain_id", []any{kuyruk, gorev})
	return d, db
}

func (d laAskiDikis) komutVar(k string) bool {
	for _, c := range *d.komutlar {
		if c == k {
			return true
		}
	}
	return false
}

const laUnit = "girginos-laravel-queue-7.service"

func TestHesapAski_KuyrukVeGorev_GidisDonus(t *testing.T) {
	d, db := laAskiKur(t, 1, 1, true)
	cron := filepath.Join(d.o.cronDizini, "girginos-laravel-7")
	govde := "* * * * * c_test /usr/bin/php83 /home/c_test/public_html/artisan schedule:run\n"
	if err := os.WriteFile(cron, []byte(govde), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := d.o.hesapAski(ctx, db, 7, true); err != nil {
		t.Fatalf("askıya alma: %v", err)
	}
	if !d.komutVar("systemctl disable --now " + laUnit) {
		t.Fatalf("kuyruk işleyicisi disable --now edilmeli (Restart=always'i yenen tek yol): %v", *d.komutlar)
	}
	if dosyaVarMi(cron) {
		t.Fatal("askıda zamanlanmış görev cron.d'de kalmamalı")
	}
	sakli := filepath.Join(d.o.saklama, "girginos-laravel-7")
	if b, err := os.ReadFile(sakli); err != nil || string(b) != govde {
		t.Fatalf("görev içeriği saklanmalı: %q %v", b, err)
	}
	// İdempotent: ikinci askıya alma saklananı ezmez.
	if err := d.o.hesapAski(ctx, db, 7, true); err != nil {
		t.Fatalf("ikinci askıya alma: %v", err)
	}
	if b, _ := os.ReadFile(sakli); string(b) != govde {
		t.Fatal("ikinci askıya alma saklanan görevi bozmamalı")
	}

	*d.komutlar = nil
	if err := d.o.hesapAski(ctx, db, 7, false); err != nil {
		t.Fatalf("askıdan alma: %v", err)
	}
	if !d.komutVar("systemctl reset-failed "+laUnit) || !d.komutVar("systemctl enable --now "+laUnit) {
		t.Fatalf("askıdan alınca reset-failed + enable --now: %v", *d.komutlar)
	}
	if b, err := os.ReadFile(cron); err != nil || string(b) != govde {
		t.Fatalf("zamanlanmış görev bayt bayt geri gelmeli: %q %v", b, err)
	}
	if dosyaVarMi(sakli) {
		t.Fatal("saklanan kopya kalmamalı")
	}
}

// Ayar kapalıysa askıdan alma işleyiciyi AÇMAZ, görevi geri koymaz (saklananı siler);
// askıda ise ayardan bağımsız olarak disk üzerindeki birim/görev kapatılır.
func TestHesapAski_AyarKapaliysaAcilmaz(t *testing.T) {
	d, db := laAskiKur(t, 0, 0, true)
	sakli := filepath.Join(d.o.saklama, "girginos-laravel-7")
	cron := filepath.Join(d.o.cronDizini, "girginos-laravel-7")
	if err := os.WriteFile(cron, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := d.o.hesapAski(context.Background(), db, 7, true); err != nil {
		t.Fatal(err)
	}
	if !d.komutVar("systemctl disable --now "+laUnit) || dosyaVarMi(cron) {
		t.Fatalf("askıda diskteki birim/görev kapatılmalı: %v", *d.komutlar)
	}
	*d.komutlar = nil
	if err := d.o.hesapAski(context.Background(), db, 7, false); err != nil {
		t.Fatal(err)
	}
	if len(*d.komutlar) != 0 || dosyaVarMi(cron) || dosyaVarMi(sakli) {
		t.Fatalf("ayar kapalıyken hiçbir şey açılmamalı: %v cron=%v sakli=%v", *d.komutlar, dosyaVarMi(cron), dosyaVarMi(sakli))
	}
}

// Laravel kurulu değilse (satır yok, birim yok) hiçbir komut çalışmaz.
func TestHesapAski_LaravelYok(t *testing.T) {
	d, _ := laAskiKur(t, 0, 0, false)
	_, db := sahtedb.Yeni() // cp_laravel_apps satırı yok
	for _, askida := range []bool{true, false} {
		if err := d.o.hesapAski(context.Background(), db, 7, askida); err != nil {
			t.Fatal(err)
		}
	}
	if len(*d.komutlar) != 0 {
		t.Fatalf("komut çalışmamalı: %v", *d.komutlar)
	}
}

// Askıdaki hesaba kuyruk işleyicisi ve zamanlanmış görev AÇILAMAZ (409), hiçbir
// şey yazılmaz; askıda olmayan hesapta kapı engellemez.
func TestQueueSchedule_AskidakiHesapta409(t *testing.T) {
	cron := sahteCron(t)
	db, err := sql.Open(sahteSurucuAdi, "c_askili")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	h := &Handlers{DB: db}
	for ad, fn := range map[string]http.HandlerFunc{"Queue": h.Queue, "Schedule": h.Schedule} {
		w := handlerIstegi(fn, http.MethodPost, `{"aktif":true}`)
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "hesap askıda") {
			t.Errorf("%s: askıdaki hesapta 409 bekleniyordu, %d: %s", ad, w.Code, w.Body.String())
		}
	}
	if girdiler, _ := os.ReadDir(cron); len(girdiler) != 0 {
		t.Fatalf("askıdaki hesapta cron.d'ye yazıldı: %v", girdiler)
	}
	if !askiKapisiGecer(t, "c_acik") {
		t.Fatal("askıda olmayan hesapta kapı engellememeli")
	}
}

func askiKapisiGecer(t *testing.T, sk string) bool {
	t.Helper()
	db, err := sql.Open(sahteSurucuAdi, sk)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	gecti := false
	handlerIstegi(func(w http.ResponseWriter, r *http.Request) { gecti = askiKapisi(w, r, db, 7, sk) }, http.MethodPost, "")
	return gecti
}
