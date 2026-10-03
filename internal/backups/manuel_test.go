package backups

// manuel_test.go — müşteri yedeği yalnız manuel (2026-10-03):
//   - plan uçları: müşteri / bayi / müşteri-adına (yönetici aktör dahil) 403, yönetici 200
//   - zamanlayıcı yalnız kök kaynaklı planları koşar (pasif koşmaz)
//   - sunucu geneli eşzamanlılık sınırı + FIFO sıra
//   - hesap başına bekleme süresi (yalnız manuel yedek sayılır)
//   - kökün yedeği o hesapta sürerken manuel 409; manuel sürerken kök bekler
//   - manuel yedek süreçleri ionice idle + nice 19
//   - yeni dosya adı sade, eski adlar okunmaya devam eder
//
// DB'li testler YALNIZ geçici MariaDB (GOSP_TEST_GECICI_DSN) ile koşar; yoksa atlanır.

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"girginospanel/internal/auth"
	"girginospanel/internal/middleware"

	"github.com/go-chi/chi/v5"
)

var ymSir = []byte("yedek-manuel-test-sirri-0123456789abcdef")

func ymAdminTok(t *testing.T) string {
	t.Helper()
	tok, err := auth.Issue(ymSir, 600, 1, "kok", "admin")
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func ymBayiTok(t *testing.T) string {
	t.Helper()
	tok, err := auth.IssueReseller(ymSir, 600, 5, "bayi_a", 9)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func ymMusteriTok(t *testing.T, domainID int64) string {
	t.Helper()
	tok, _, err := auth.GenerateMusteri(ymSir, auth.MusteriClaims{FTPHesapID: 3, DomainID: domainID, Kullanici: "c_ym"}, 600)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func ymAdinaTok(t *testing.T, domainID int64, aktorRol string) string {
	t.Helper()
	tok, _, err := auth.GenerateMusteri(ymSir, auth.MusteriClaims{
		DomainID: domainID, Kullanici: "c_ym", AktorUID: 1, AktorAd: "aktor", AktorRol: aktorRol, AktorRID: 9,
	}, auth.AdinaOmurSn)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// ymRouter: GERÇEK kimlik zinciri (RequireAuth) + chi rotası. MusteriScope bilerek
// YOK: kapsam kapısı bayiyi kendi başına düşürür; burada handler'ın KENDİ rol
// kapısı ölçülür.
func ymRouter(h *Handlers) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequireAuth(ymSir))
	r.Put("/domains/{id}/backup-schedule", h.SetSchedule)
	r.Get("/domains/{id}/backup-schedule", h.GetSchedule)
	r.Post("/domains/{id}/backups", h.Create)
	return r
}

func ymIstek(t *testing.T, hn http.Handler, method, yol, tok, govde string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, yol, strings.NewReader(govde))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	hn.ServeHTTP(rec, req)
	return rec
}

// ---- Plan yetkisi ----

func TestPlanUcu_MusteriBayiAdina403(t *testing.T) {
	h := &Handlers{} // DB yok: kapı lookupDomain'den ÖNCE olmalı
	hn := ymRouter(h)
	govde := `{"freq":"daily","hour":3,"retention":7}`
	durumlar := []struct{ ad, tok string }{
		{"müşteri", ymMusteriTok(t, 42)},
		{"bayi", ymBayiTok(t)},
		{"müşteri adına (yönetici aktör)", ymAdinaTok(t, 42, "admin")},
		{"müşteri adına (bayi aktör)", ymAdinaTok(t, 42, "reseller")},
	}
	for _, d := range durumlar {
		rec := ymIstek(t, hn, http.MethodPut, "/domains/42/backup-schedule", d.tok, govde)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: kod %d, 403 beklendi (%s)", d.ad, rec.Code, rec.Body.String())
			continue
		}
		if !strings.Contains(rec.Body.String(), "yalnız sunucu yöneticisi") {
			t.Errorf("%s: anlaşılır mesaj yok: %s", d.ad, rec.Body.String())
		}
	}
}

func TestPlanDuzenleyebilir_YalnizAdmin(t *testing.T) {
	for rol, bek := range map[string]bool{"admin": true, "reseller": false, "musteri": false, "adina": false, "": false} {
		if planDuzenleyebilir(rol) != bek {
			t.Errorf("planDuzenleyebilir(%q) = %v", rol, !bek)
		}
		if beklemedenMuaf(rol) != bek {
			t.Errorf("beklemedenMuaf(%q) = %v", rol, !bek)
		}
	}
}

// ymDB: geçici veritabanı + yedek yolunun okuduğu domains/backups kolonları.
// domains 0114'ten ÖNCEKİ hâliyle kurulur; göç dosyası gerçekten koşulur.
func ymDB(t *testing.T, gocKos bool) *sql.DB {
	t.Helper()
	db := g113_db(t)
	g113_exec(t, db, `DROP TABLE backups`)
	g113_exec(t, db, `DROP TABLE domains`)
	g113_exec(t, db, `CREATE TABLE domains (
		id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
		alan_adi VARCHAR(255) NOT NULL,
		sistem_kullanici VARCHAR(64) NOT NULL,
		is_demo TINYINT(1) NOT NULL DEFAULT 0,
		backup_freq VARCHAR(16) NOT NULL DEFAULT 'none',
		backup_hour TINYINT NOT NULL DEFAULT 3,
		backup_retention INT NOT NULL DEFAULT 7,
		last_backup_at DATETIME NULL
	) ENGINE=InnoDB`)
	g113_exec(t, db, `CREATE TABLE backups (
		id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
		domain_id BIGINT UNSIGNED NOT NULL,
		tip VARCHAR(16) NOT NULL DEFAULT 'tam',
		dosya VARCHAR(255) NOT NULL,
		boyut_b BIGINT NOT NULL DEFAULT 0,
		notlar VARCHAR(255) NOT NULL DEFAULT '',
		created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
		job_id BIGINT NULL,
		sha256 CHAR(64) NOT NULL DEFAULT '',
		dogrulama VARCHAR(16) NOT NULL DEFAULT ''
	) ENGINE=InnoDB`)
	if gocKos {
		ymGocKos(t, db)
	}
	return db
}

func ymGocKos(t *testing.T, db *sql.DB) {
	t.Helper()
	body, err := os.ReadFile("../../migrations/0115_yedek_plan_kaynak.sql")
	if err != nil {
		t.Fatal(err)
	}
	var temiz []string
	for _, s := range strings.Split(string(body), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(s), "--") {
			temiz = append(temiz, s)
		}
	}
	for _, ifade := range strings.Split(strings.Join(temiz, "\n"), ";") {
		if strings.TrimSpace(ifade) != "" {
			g113_exec(t, db, ifade)
		}
	}
}

func TestGoc0114_IdempotentVeVarsayilanEski(t *testing.T) {
	db := ymDB(t, false)
	g113_exec(t, db, `INSERT INTO domains(id, alan_adi, sistem_kullanici, backup_freq) VALUES(1,'a.com','c_a','daily')`)
	ymGocKos(t, db)
	ymGocKos(t, db) // ikinci koşu hata vermemeli
	var k string
	if err := db.QueryRow(`SELECT backup_plan_kaynak FROM domains WHERE id=1`).Scan(&k); err != nil {
		t.Fatal(err)
	}
	if k != "eski" {
		t.Fatalf("mevcut plan kaynağı %q, 'eski' beklendi (veri silinmeden işaretlenmeli)", k)
	}
	var f string
	_ = db.QueryRow(`SELECT backup_freq FROM domains WHERE id=1`).Scan(&f)
	if f != "daily" {
		t.Fatalf("göç plan verisini değiştirdi: %q", f)
	}
}

func TestPlanUcu_Yonetici200VeKokIsaretler(t *testing.T) {
	db := ymDB(t, true)
	g113_exec(t, db, `INSERT INTO domains(id, alan_adi, sistem_kullanici) VALUES(42,'ym.com','c_ym_com')`)
	hn := ymRouter(&Handlers{DB: db})
	rec := ymIstek(t, hn, http.MethodPut, "/domains/42/backup-schedule", ymAdminTok(t), `{"freq":"weekly","hour":4,"retention":5}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("yönetici: kod %d (%s)", rec.Code, rec.Body.String())
	}
	var f, k string
	if err := db.QueryRow(`SELECT backup_freq, backup_plan_kaynak FROM domains WHERE id=42`).Scan(&f, &k); err != nil {
		t.Fatal(err)
	}
	if f != "weekly" || k != "kok" {
		t.Fatalf("freq=%q kaynak=%q; weekly/kok beklendi", f, k)
	}
	// Müşteri aynı uçta 403 ve veri DEĞİŞMEZ.
	rec = ymIstek(t, hn, http.MethodPut, "/domains/42/backup-schedule", ymMusteriTok(t, 42), `{"freq":"daily","hour":1,"retention":3}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("müşteri: kod %d", rec.Code)
	}
	_ = db.QueryRow(`SELECT backup_freq FROM domains WHERE id=42`).Scan(&f)
	if f != "weekly" {
		t.Fatalf("403 sonrası plan değişti: %q", f)
	}
	// GET: müşteride duzenlenebilir=false, yöneticide true.
	rec = ymIstek(t, hn, http.MethodGet, "/domains/42/backup-schedule", ymMusteriTok(t, 42), "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"duzenlenebilir":false`) {
		t.Fatalf("müşteri GET: %d %s", rec.Code, rec.Body.String())
	}
	rec = ymIstek(t, hn, http.MethodGet, "/domains/42/backup-schedule", ymAdminTok(t), "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"duzenlenebilir":true`) {
		t.Fatalf("yönetici GET: %d %s", rec.Code, rec.Body.String())
	}
}

// ---- Zamanlayıcı kaynak süzgeci ----

func TestZamanlayici_YalnizKokKaynakliPlanlar(t *testing.T) {
	db := ymDB(t, true)
	g113_exec(t, db, `INSERT INTO domains(id, alan_adi, sistem_kullanici, backup_freq, backup_hour, backup_plan_kaynak) VALUES
		(1,'kok.com','c_kok','daily',3,'kok'),
		(2,'eski.com','c_eski','daily',3,'eski'),
		(3,'pasif.com','c_pasif','daily',3,'pasif'),
		(4,'kapali.com','c_kapali','none',3,'kok')`)
	due, err := zamanlanmisDomainler(context.Background(), db, 3, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got := map[int64]bool{}
	for _, d := range due {
		got[d.ID] = true
	}
	if !got[1] || !got[2] || got[3] || got[4] || len(got) != 2 {
		t.Fatalf("koşacak domainler %v; {1 kok, 2 eski} beklendi (pasif/kapalı koşmamalı)", got)
	}
}

func TestZamanlayici_GocYokkenEskiDavranis(t *testing.T) {
	db := ymDB(t, false)
	g113_exec(t, db, `INSERT INTO domains(id, alan_adi, sistem_kullanici, backup_freq, backup_hour) VALUES (1,'a.com','c_a','daily',3)`)
	due, err := zamanlanmisDomainler(context.Background(), db, 3, time.Now())
	if err != nil || len(due) != 1 {
		t.Fatalf("göç koşmamışken plan koşmalı: due=%v err=%v", due, err)
	}
}

// ---- Eşzamanlılık sınırı ----

func TestKuyruk_EszamanlilikSiniriVeSira(t *testing.T) {
	k := yeniIsKuyrugu(ManuelEszamanliSinir, ManuelKuyrukAzami)
	var aktif, enCok int32
	var wg sync.WaitGroup
	birak := make(chan struct{})
	for i := int64(1); i <= 4; i++ {
		if !k.Ekle(i) {
			t.Fatalf("%d eklenemedi", i)
		}
	}
	if k.Sira(1) != 1 || k.Sira(4) != 4 || k.Sira(99) != 0 {
		t.Fatalf("FIFO sıra yanlış: %d %d %d", k.Sira(1), k.Sira(4), k.Sira(99))
	}
	for i := int64(1); i <= 4; i++ {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			if err := k.Bekle(context.Background(), id); err != nil {
				t.Error(err)
				return
			}
			n := atomic.AddInt32(&aktif, 1)
			for {
				m := atomic.LoadInt32(&enCok)
				if n <= m || atomic.CompareAndSwapInt32(&enCok, m, n) {
					break
				}
			}
			<-birak
			time.Sleep(20 * time.Millisecond)
			atomic.AddInt32(&aktif, -1)
			k.Birak()
		}(i)
	}
	time.Sleep(100 * time.Millisecond)
	if c := k.Calisan(); c != ManuelEszamanliSinir {
		t.Errorf("çalışan %d, sınır %d", c, ManuelEszamanliSinir)
	}
	close(birak)
	wg.Wait()
	if enCok != int32(ManuelEszamanliSinir) {
		t.Fatalf("aynı anda %d manuel yedek koştu; sınır %d", enCok, ManuelEszamanliSinir)
	}
	if ManuelEszamanliSinir != 1 {
		t.Fatalf("varsayılan eşzamanlılık 1 olmalı (kullanıcı kararı)")
	}
}

func TestKuyruk_DoluysaRet(t *testing.T) {
	k := yeniIsKuyrugu(1, 2)
	if !k.Ekle(1) || !k.Ekle(2) {
		t.Fatal("ilk ikisi eklenmeli")
	}
	if k.Ekle(3) {
		t.Fatal("kuyruk doluyken eklenmemeli")
	}
}

// ---- Bekleme süresi ----

func TestBekleme_YalnizManuelYedekSayilir(t *testing.T) {
	db := ymDB(t, true)
	g113_exec(t, db, `INSERT INTO domains(id, alan_adi, sistem_kullanici) VALUES(42,'ym.com','c_ym_com')`)
	ctx := context.Background()
	if k := manuelBeklemeKalan(ctx, db, 42); k != 0 {
		t.Fatalf("hiç yedek yokken bekleme %v", k)
	}
	// Sistem yedekleri (oto, toplu iş) beklemeyi TETİKLEMEZ.
	g113_exec(t, db, `INSERT INTO backups(domain_id, tip, dosya, created_at) VALUES(42,'oto','a.tar.gz',NOW())`)
	g113_exec(t, db, `INSERT INTO backups(domain_id, tip, dosya, created_at, job_id) VALUES(42,'tam','b.tar.gz',NOW(),7)`)
	if k := manuelBeklemeKalan(ctx, db, 42); k != 0 {
		t.Fatalf("sistem yedeği beklemeyi tetikledi: %v", k)
	}
	g113_exec(t, db, `INSERT INTO backups(domain_id, tip, dosya, created_at) VALUES(42,'tam','c.tar.gz',NOW() - INTERVAL 1 HOUR)`)
	k := manuelBeklemeKalan(ctx, db, 42)
	if k < ManuelBeklemeSuresi-time.Hour-time.Minute || k > ManuelBeklemeSuresi-time.Hour+time.Minute {
		t.Fatalf("1 saat önceki manuel yedekten sonra kalan %v", k)
	}
	// Müşteri Create → 429 + anlaşılır mesaj (arşive hiç dokunulmaz).
	hn := ymRouter(&Handlers{DB: db})
	rec := ymIstek(t, hn, http.MethodPost, "/domains/42/backups", ymMusteriTok(t, 42), "")
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "sonra alabilirsiniz") {
		t.Fatalf("bekleme süresinde manuel yedek: %d %s", rec.Code, rec.Body.String())
	}
	if IlerlemeAktifMi(42) {
		t.Fatal("reddedilen istek iş başlattı")
	}
	// Süre dolunca serbest.
	g113_exec(t, db, `UPDATE backups SET created_at = NOW() - INTERVAL 7 HOUR WHERE dosya='c.tar.gz'`)
	if k := manuelBeklemeKalan(ctx, db, 42); k != 0 {
		t.Fatalf("7 saat sonra hâlâ bekleme: %v", k)
	}
}

// ---- Kök yedeği ile çakışma ----

func TestKokYedegiSurerkenManuelRet(t *testing.T) {
	db := ymDB(t, true)
	g113_exec(t, db, `INSERT INTO domains(id, alan_adi, sistem_kullanici) VALUES(43,'ym2.com','c_ym2_com')`)
	if _, ok := yedekBaslaTur(43, "sistem"); !ok {
		t.Fatal("sistem kilidi alınamadı")
	}
	defer yedekBitir(43)
	hn := ymRouter(&Handlers{DB: db})
	rec := ymIstek(t, hn, http.MethodPost, "/domains/43/backups", ymMusteriTok(t, 43), "")
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "sistem yedeği") {
		t.Fatalf("kök yedeği sürerken manuel: %d %s", rec.Code, rec.Body.String())
	}
	if manuelKuyrugu.Sira(43) != 0 {
		t.Fatal("reddedilen istek kuyruğa girdi")
	}
}

func TestSistemKilidi_ManuelBitenceAlinir(t *testing.T) {
	eskiB, eskiA := sistemKilitBekleme, sistemKilitAralik
	defer func() { sistemKilitBekleme, sistemKilitAralik = eskiB, eskiA }()
	sistemKilitAralik = 5 * time.Millisecond

	if _, ok := yedekBaslaTur(77, "manuel"); !ok {
		t.Fatal("manuel kilit alınamadı")
	}
	sistemKilitBekleme = 60 * time.Millisecond
	if sistemKilidiAl(context.Background(), 77) {
		t.Fatal("manuel sürerken kök kilidi alındı")
	}
	sistemKilitBekleme = 2 * time.Second
	go func() { time.Sleep(50 * time.Millisecond); yedekBitir(77) }()
	if !sistemKilidiAl(context.Background(), 77) {
		t.Fatal("manuel bitince kök kilidi alınmalıydı")
	}
	if sahip, ok := yedekBaslaTur(77, "manuel"); ok || sahip != "sistem" {
		t.Fatalf("kilit sahibi %q ok=%v", sahip, ok)
	}
	yedekBitir(77)
}

// ---- Düşük öncelik ----

func TestKomut_ManuelDusukOncelik(t *testing.T) {
	eski := komutBak
	defer func() { komutBak = eski }()
	komutBak = func(string) (string, error) { return "/usr/bin/ionice", nil }

	ctx := context.Background()
	if got := komut(ctx, "tar", "czf", "x.tar.gz").Args; !reflect.DeepEqual(got, []string{"tar", "czf", "x.tar.gz"}) {
		t.Fatalf("kök yolu değişti: %v", got)
	}
	bek := []string{"ionice", "-c3", "nice", "-n", "19", "tar", "czf", "x.tar.gz"}
	if got := komut(dusukOncelikli(ctx), "tar", "czf", "x.tar.gz").Args; !reflect.DeepEqual(got, bek) {
		t.Fatalf("manuel yedek düşük öncelikli değil: %v", got)
	}
	// Uzak gönderim (lftp) de aynı önekle.
	cmd, temizle, err := lftpKomutu(dusukOncelikli(ctx), "bye")
	if err != nil {
		t.Fatal(err)
	}
	defer temizle()
	if len(cmd.Args) < 7 || !reflect.DeepEqual(cmd.Args[:6], []string{"ionice", "-c3", "nice", "-n", "19", "lftp"}) {
		t.Fatalf("lftp düşük öncelikli değil: %v", cmd.Args)
	}
}

// arsivOlustur'un harici süreçleri komut() üzerinden kurulmalı (doğrudan
// exec.CommandContext öncelik önekini atlar).
func TestArsivOlustur_KomutSarmaliKullanir(t *testing.T) {
	b, err := os.ReadFile("granular.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	a := strings.Index(s, "func arsivOlustur(")
	e := strings.Index(s, "// IcerikDosya / IcerikDB")
	if a < 0 || e < a {
		t.Fatal("arsivOlustur bulunamadı")
	}
	govde := s[a:e]
	if strings.Contains(govde, "exec.Command") {
		t.Fatal("arsivOlustur doğrudan exec.Command kullanıyor — manuel yedek önceliği atlanır")
	}
	if strings.Count(govde, "komut(ctx, ") < 4 {
		t.Fatal("arsivOlustur'un mysql/mysqldump/tar/gzip süreçleri komut() ile kurulmalı")
	}
}

// ---- Dosya adı + kaynak ----

func TestYedekDosyaAdi_SadeVeEskiAdlarOkunur(t *testing.T) {
	dir := t.TempDir()
	an := time.Date(2026, 8, 31, 3, 1, 25, 0, time.UTC)
	ad := yedekDosyaAdi(dir, "c_kaanreseller_com", an)
	if ad != "c_kaanreseller_com-20260831-030125.tar.gz" {
		t.Fatalf("yeni ad %q", ad)
	}
	if err := os.WriteFile(filepath.Join(dir, ad), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if ad2 := yedekDosyaAdi(dir, "c_kaanreseller_com", an); ad2 != "c_kaanreseller_com-20260831-030125-2.tar.gz" {
		t.Fatalf("çakışmada ad %q", ad2)
	}
	for _, eski := range []string{
		"c_kaanreseller_com-20260831-030125-planli.tar.gz",
		"c_kaanreseller_com-auto-20260831-030125.tar.gz",
		"c_kaanreseller_com-20260831-030125.tar.gz",
		"c_kaanreseller_com-20260831-030125-2.tar.gz",
	} {
		if got := uzakTarihDizini(eski); got != "2026-08-31" {
			t.Errorf("%s → uzak tarih dizini %q", eski, got)
		}
	}
}

func TestYedekKaynagi(t *testing.T) {
	yok := sql.NullInt64{}
	var_ := sql.NullInt64{Int64: 5, Valid: true}
	durum := []struct {
		tip string
		job sql.NullInt64
		bek string
	}{
		{"oto", yok, "sistem"}, {"planli", yok, "sistem"}, {"tam", var_, "sistem"}, {"tam", yok, "manuel"},
	}
	for _, d := range durum {
		if got := yedekKaynagi(d.tip, d.job); got != d.bek {
			t.Errorf("yedekKaynagi(%q, %v) = %q, %q beklendi", d.tip, d.job.Valid, got, d.bek)
		}
	}
}

func TestGoc0114_KosucuUyumu(t *testing.T) {
	body, err := os.ReadFile("../../migrations/0115_yedek_plan_kaynak.sql")
	if err != nil {
		t.Fatal(err)
	}
	for i, satir := range strings.Split(string(body), "\n") {
		s := strings.TrimSpace(satir)
		if strings.HasPrefix(s, "--") {
			if strings.Contains(s, ";") {
				t.Errorf("satır %d: yorumda ';' var", i+1)
			}
			continue
		}
		if strings.Contains(s, "--") || strings.Contains(s, "/*") || strings.Contains(s, "#") {
			t.Errorf("satır %d: satır içi yorum: %s", i+1, s)
		}
	}
	if !strings.Contains(string(body), "ADD COLUMN IF NOT EXISTS backup_plan_kaynak") {
		t.Error("göç idempotent değil")
	}
}
