package provisioner

// tenantfpm_sokum_test.go — per-tenant FPM yaşam sonu: söküm (silme yolları),
// söküm/kurulum yarışı, StartLimit tavanı ve sahipsiz birim uzlaştırması.
//
// Tüm yollar (birim dizini, /etc/php-fpm-tenant, /run, /var/log, paylaşılan havuz
// dizinleri, izolasyon sentinel'i) geçici dizine; systemctl / kullanıcı sorgusu /
// slice kancası SAHTE — ana makinenin systemd'sine ve /etc'sine dokunulmaz.
// DB'li uzlaştırma testi geçici MariaDB ister (GOSP_TEST_GECICI_DSN, go_test db:true).

import (
	"database/sql"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

type sokumOrtam struct {
	kok       string
	havuz     string
	komutlar  *[]string
	sliceSil  *[]string
	kullanici map[string]bool
	mu        *sync.Mutex
}

func (o sokumOrtam) komutVar(k string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, x := range *o.komutlar {
		if x == k {
			return true
		}
	}
	return false
}

// sokumDikis — tüm dış etkileri geçici dizine / sahteye çevirir.
func sokumDikis(t *testing.T) sokumOrtam {
	t.Helper()
	kok := t.TempDir()
	o := sokumOrtam{kok: kok, havuz: filepath.Join(kok, "php-fpm.d"), komutlar: &[]string{},
		sliceSil: &[]string{}, kullanici: map[string]bool{}, mu: &sync.Mutex{}}
	for _, d := range []string{"systemd", "tenant", "run", "log", "php-fpm.d", "sentinel"} {
		if err := os.MkdirAll(filepath.Join(kok, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	e := struct {
		ud, cr, rk, lk, sd string
		km                 func(string, ...string) ([]byte, error)
		kv                 func(string) bool
		ss                 func(string) error
		hd                 func() map[string]string
		my                 time.Duration
		db                 *sql.DB
	}{tenantUnitDir, tenantCfgRoot, tenantRunKok, tenantLogKok, izolasyonSentinelDir,
		sokumKomut, tenantKullaniciVar, TenantSliceSilKancasi, askiHavuzDizini, sahipsizMinYas, pkgDB}
	t.Cleanup(func() {
		tenantUnitDir, tenantCfgRoot, tenantRunKok, tenantLogKok, izolasyonSentinelDir = e.ud, e.cr, e.rk, e.lk, e.sd
		sokumKomut, tenantKullaniciVar, TenantSliceSilKancasi, askiHavuzDizini, sahipsizMinYas, pkgDB = e.km, e.kv, e.ss, e.hd, e.my, e.db
	})
	tenantUnitDir = filepath.Join(kok, "systemd")
	tenantCfgRoot = filepath.Join(kok, "tenant")
	tenantRunKok = filepath.Join(kok, "run")
	tenantLogKok = filepath.Join(kok, "log")
	izolasyonSentinelDir = filepath.Join(kok, "sentinel")
	pkgDB = nil
	sokumKomut = func(ad string, arg ...string) ([]byte, error) {
		o.mu.Lock()
		defer o.mu.Unlock()
		*o.komutlar = append(*o.komutlar, ad+" "+strings.Join(arg, " "))
		return nil, nil
	}
	tenantKullaniciVar = func(sk string) bool {
		o.mu.Lock()
		defer o.mu.Unlock()
		return o.kullanici[sk]
	}
	TenantSliceSilKancasi = func(sk string) error {
		o.mu.Lock()
		defer o.mu.Unlock()
		*o.sliceSil = append(*o.sliceSil, sk)
		_ = os.Remove(tenantSlicePath(sk))
		return nil
	}
	askiHavuzDizini = func() map[string]string { return map[string]string{o.havuz: "php-fpm"} }
	return o
}

func sokumIsaretiTemizle(t *testing.T, sks ...string) {
	t.Cleanup(func() {
		for _, sk := range sks {
			tenantSokumIsaretiKaldir(sk)
		}
	})
}

func sk_yaz(t *testing.T, yol, icerik string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(yol), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(yol, []byte(icerik), 0o644); err != nil {
		t.Fatal(err)
	}
}

// tenantKur — panelin ürettiği izlerin tamamı (birim, drop-in, cfg, run, log, slice,
// paylaşılan havuz .bak'ı). yas>0 ise tüm izlerin zamanı geriye çekilir.
func tenantKur(t *testing.T, o sokumOrtam, sk string, yas time.Duration) {
	t.Helper()
	sk_yaz(t, tenantUnitPath(sk), renderTenantUnit(sk, "/opt/remi/php82/root/usr/sbin/php-fpm"))
	sk_yaz(t, filepath.Join(tenantUnitDropinDir(sk), tenantStartLimitDropinAd), tenantStartLimitDropin())
	sk_yaz(t, filepath.Join(tenantCfgDir(sk), "php-fpm.conf"), renderTenantGlobalCfg(sk))
	sk_yaz(t, filepath.Join(tenantCfgDir(sk), "pool.conf"), "[x]\n")
	sk_yaz(t, filepath.Join(tenantRunDir(sk), "x.pid"), "1")
	sk_yaz(t, tenantPerSkLogFile(sk), "")
	sk_yaz(t, tenantSlicePath(sk), "# GirginOSPanel per-domain resource slice — "+sk+"\n[Unit]\n")
	sk_yaz(t, filepath.Join(o.havuz, sk+".conf.bak"), "[x]\n")
	if yas > 0 {
		eski := time.Now().Add(-yas)
		for _, y := range []string{tenantUnitPath(sk), tenantUnitDropinDir(sk), tenantCfgDir(sk),
			tenantRunDir(sk), tenantPerSkLogDir(sk), tenantSlicePath(sk)} {
			if err := os.Chtimes(y, eski, eski); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func izlerKalmadi(t *testing.T, o sokumOrtam, sk string) {
	t.Helper()
	for _, y := range []string{tenantUnitPath(sk), tenantUnitDropinDir(sk), tenantCfgDir(sk),
		tenantRunDir(sk), tenantPerSkLogDir(sk), tenantSlicePath(sk), filepath.Join(o.havuz, sk+".conf.bak")} {
		if izVar(y) {
			t.Errorf("%s: iz kaldı: %s", sk, y)
		}
	}
}

func izlerDuruyor(t *testing.T, sk string) {
	t.Helper()
	for _, y := range []string{tenantUnitPath(sk), tenantCfgDir(sk)} {
		if !izVar(y) {
			t.Errorf("%s: DOKUNULMAMASI gereken iz silindi: %s", sk, y)
		}
	}
}

// Silme yolu: TeardownTenantFPM birimi durdurur/disable eder, birim + drop-in +
// cfg/run/log + slice + paylaşılan havuz artıklarını kaldırır, daemon-reload +
// reset-failed yapar; ikinci çağrı no-op (idempotent).
func TestTeardownTenantFPM_TumIzleriSoker_Idempotent(t *testing.T) {
	o := sokumDikis(t)
	const sk = "c_sokum_bir"
	sokumIsaretiTemizle(t, sk)
	tenantKur(t, o, sk, 0)
	TeardownTenantFPM(sk)
	izlerKalmadi(t, o, sk)
	for _, k := range []string{"systemctl disable --now php-fpm-" + sk + ".service", "systemctl daemon-reload",
		"systemctl reset-failed php-fpm-" + sk + ".service"} {
		if !o.komutVar(k) {
			t.Errorf("beklenen komut yok: %q (koşan: %v)", k, *o.komutlar)
		}
	}
	if len(*o.sliceSil) != 1 || (*o.sliceSil)[0] != sk {
		t.Errorf("slice kancası çağrılmalı: %v", *o.sliceSil)
	}
	if !TenantSokumde(sk) {
		t.Error("söküm işareti konmalı")
	}
	// İdempotent: ikinci söküm panik/hata yok, iz yok.
	TeardownTenantFPM(sk)
	izlerKalmadi(t, o, sk)
	// Geçersiz ad hiçbir şeye dokunmaz.
	n := len(*o.komutlar)
	TeardownTenantFPM("../etc")
	TeardownTenantFPM("www")
	if len(*o.komutlar) != n {
		t.Errorf("geçersiz sk için komut koşmamalı: %v", (*o.komutlar)[n:])
	}
}

// 🔴 Kök neden (yarış): söküm SONRASI gelen arka plan EnableTenantFPM birimi
// YENİDEN YAZMAMALI (181: php-fpm-c_e2ebayi1_test). Yeniden sağlama işareti kaldırır.
func TestEnableTenantFPM_SokumdenSonraBirimYazmaz(t *testing.T) {
	o := sokumDikis(t)
	const sk = "c_sokum_iki"
	sokumIsaretiTemizle(t, sk)
	o.kullanici[sk] = true
	TeardownTenantFPM(sk)
	if _, err := EnableTenantFPM(nil, 0, sk, "8.2"); !errors.Is(err, ErrTenantSokuldu) {
		t.Fatalf("sökülen hesapta EnableTenantFPM reddetmeli: %v", err)
	}
	if izVar(tenantUnitPath(sk)) || izVar(tenantCfgDir(sk)) {
		t.Fatal("reddedilen kurulum iz bırakmamalı")
	}
	// Kullanıcı yoksa da (userdel sonrası) kurulum yok.
	tenantSokumIsaretiKaldir(sk)
	o.kullanici[sk] = false
	if _, err := EnableTenantFPM(nil, 0, sk, "8.2"); !errors.Is(err, ErrTenantSokuldu) {
		t.Fatalf("sistem kullanıcısı yokken EnableTenantFPM reddetmeli: %v", err)
	}
	if izVar(tenantUnitPath(sk)) {
		t.Fatal("kullanıcısız hesaba birim yazılmamalı")
	}
}

// Kurulum sürerken (yaşam kilidi tutuluyor) gelen söküm BEKLER, sonra kurulumun
// yazdığını kaldırır — sıra ne olursa olsun yetim birim kalmaz.
func TestTeardownTenantFPM_SurenKurulumuBekler(t *testing.T) {
	o := sokumDikis(t)
	const sk = "c_sokum_uc"
	sokumIsaretiTemizle(t, sk)
	yk := fpmYasamKilidi(sk)
	yk.Lock() // EnableTenantFPM kilitte
	bitti := make(chan struct{})
	go func() { TeardownTenantFPM(sk); close(bitti) }()
	time.Sleep(150 * time.Millisecond)
	select {
	case <-bitti:
		yk.Unlock()
		t.Fatal("söküm süren kurulumu BEKLEMELİ (yaşam kilidi)")
	default:
	}
	tenantKur(t, o, sk, 0) // kurulum birimi yazdı
	yk.Unlock()
	select {
	case <-bitti:
	case <-time.After(5 * time.Second):
		t.Fatal("söküm bitmedi")
	}
	izlerKalmadi(t, o, sk)
}

// Silinmiş hesapta RollbackToSharedFPM paylaşılana indirmez, sahte "izolasyon
// kaybı" yazmaz; kullanıcısı yoksa yetim cfg dizini BIRAKMAZ (181: c_dgrplan_test).
func TestRollbackToSharedFPM_SilinmisHesap(t *testing.T) {
	o := sokumDikis(t)
	const sk = "c_sokum_dort"
	sokumIsaretiTemizle(t, sk)
	tenantKur(t, o, sk, 0)
	if err := RollbackToSharedFPM(nil, 0, sk, "8.2", "test"); !errors.Is(err, ErrTenantSokuldu) {
		t.Fatalf("kullanıcısız hesapta rollback ErrTenantSokuldu dönmeli: %v", err)
	}
	if izVar(tenantUnitPath(sk)) || izVar(tenantCfgDir(sk)) || izVar(tenantPerSkLogDir(sk)) {
		t.Fatal("kullanıcısız hesabın per-tenant izleri sökülmeli")
	}
	if izVar(izolasyonSentinelPath(sk)) {
		t.Fatal("silinmiş hesap için izolasyon-kaybı sentinel'i yazılmamalı")
	}
	tenantKur(t, o, sk, 0)
	o.kullanici[sk] = true
	tenantSokulen.Store(sk, time.Now())
	if err := RollbackToSharedFPM(nil, 0, sk, "8.2", "test"); !errors.Is(err, ErrTenantSokuldu) {
		t.Fatalf("sökülen hesapta rollback ErrTenantSokuldu dönmeli: %v", err)
	}
	if izVar(izolasyonSentinelPath(sk)) {
		t.Fatal("sökülen hesap için izolasyon-kaybı sentinel'i yazılmamalı")
	}
}

// Yapısal kapı: TÜM silme yolları (domain silme, bayi toplu silme, taşıma geri
// alma, oluşturma hatası geri alma, platform SiteSil) Deprovision'a gelir;
// Deprovision TeardownTenantFPM'i çağırmalı.
func TestDeprovision_TeardownTenantFPMCagirir(t *testing.T) {
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, "provisioner.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	bulundu := false
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "Deprovision" || fn.Recv != nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				if id, ok := c.Fun.(*ast.Ident); ok && id.Name == "TeardownTenantFPM" {
					bulundu = true
				}
			}
			return true
		})
	}
	if !bulundu {
		t.Fatal("Deprovision TeardownTenantFPM çağırmalı (per-tenant FPM birimi silme yolunda kalır)")
	}
}

// StartLimit: drop-in bir kez yazılır (sonra no-op); ana birim şablonu DEĞİŞMEZ
// (damga aynı → RepairTenantUnitDrift sağlıklı tenant'ları yeniden başlatmaz);
// geri doldurma yalnız panel birimlerine yazar ve TEK daemon-reload yapar.
func TestTenantStartLimit_DropinVeGeriDoldurma(t *testing.T) {
	o := sokumDikis(t)
	if u := renderTenantUnit("c_x", "/bin/php-fpm"); strings.Contains(u, "StartLimitBurst=") || strings.Contains(u, "StartLimitIntervalSec=") {
		t.Fatal("tavan ana birime GİRMEMELİ (damga değişir → tüm tenant'lar restart)")
	}
	d := tenantStartLimitDropin()
	for _, p := range []string{"[Unit]\n", fmt.Sprintf("StartLimitIntervalSec=%d\n", tenantStartLimitAralikSn),
		fmt.Sprintf("StartLimitBurst=%d\n", tenantStartLimitPatlama)} {
		if !strings.Contains(d, p) {
			t.Errorf("drop-in'de %q yok:\n%s", p, d)
		}
	}
	// Tavan RestartSec=2 döngüsünü 300 sn içinde YAKALAMALI (varsayılan 5/10 sn yakalamıyordu).
	if tenantStartLimitPatlama*2 >= tenantStartLimitAralikSn {
		t.Fatal("tavan RestartSec=2 döngüsünü pencerede yakalayamaz")
	}
	sk_yaz(t, tenantUnitPath("c_panel_a"), renderTenantUnit("c_panel_a", "/bin/php-fpm"))
	sk_yaz(t, tenantUnitPath("c_panel_b"), renderTenantUnit("c_panel_b", "/bin/php-fpm"))
	sk_yaz(t, tenantUnitPath("c_yabanci"), "[Unit]\nDescription=baska bir sey\n")
	if n := tenantStartLimitGeriDoldur(); n != 2 {
		t.Fatalf("2 panel birimi güncellenmeli, %d", n)
	}
	if izVar(tenantUnitDropinDir("c_yabanci")) {
		t.Fatal("yabancı birime drop-in yazılmamalı")
	}
	reload := 0
	for _, k := range *o.komutlar {
		if k == "systemctl daemon-reload" {
			reload++
		}
		if strings.Contains(k, "restart") || strings.Contains(k, " start ") {
			t.Errorf("geri doldurma servis (yeniden) başlatmamalı: %q", k)
		}
	}
	if reload != 1 {
		t.Fatalf("tek daemon-reload beklenir, %d", reload)
	}
	once := len(*o.komutlar)
	if n := tenantStartLimitGeriDoldur(); n != 0 || len(*o.komutlar) != once {
		t.Fatalf("ikinci geri doldurma no-op olmalı (daemon-reload dahil): n=%d komut=%v", n, (*o.komutlar)[once:])
	}
	if ok, _ := tenantStartLimitYaz("c_birimsiz"); ok {
		t.Fatal("birimi olmayan hesaba drop-in yazılmamalı")
	}
}

// Sürüm kaldırma kapısı: yalnız o sürümün ikilisini gösteren panel birimleri.
func TestSurumuKullananTenantBirimleri(t *testing.T) {
	sokumDikis(t)
	b82 := phpMap[normalizePHP("8.2")].FpmBin
	b84 := phpMap[normalizePHP("8.4")].FpmBin
	if b82 == "" || b84 == "" || b82 == b84 {
		t.Skip("phpMap 8.2/8.4 ikilisi tanımsız")
	}
	sk_yaz(t, tenantUnitPath("c_s82"), renderTenantUnit("c_s82", b82))
	sk_yaz(t, tenantUnitPath("c_s84"), renderTenantUnit("c_s84", b84))
	sk_yaz(t, tenantUnitPath("c_yab82"), "[Service]\nExecStart="+b82+" --nodaemonize\n")
	got := SurumuKullananTenantBirimleri("8.2")
	if len(got) != 1 || got[0] != "c_s82" {
		t.Fatalf("yalnız c_s82 beklenir: %v", got)
	}
}

// ── Uzlaştırma (geçici MariaDB) ──────────────────────────────────────────────

func sokum_panel(t *testing.T) *sql.DB {
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
	ad := fmt.Sprintf("gosp_test_fpmsahipsiz_%d", time.Now().UnixNano())
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
		`CREATE TABLE domains (id BIGINT UNSIGNED PRIMARY KEY, sistem_kullanici VARCHAR(64) NOT NULL,
			askida TINYINT NOT NULL DEFAULT 0)`,
		`CREATE TABLE audit_log (id BIGINT AUTO_INCREMENT PRIMARY KEY, actor_user_id BIGINT NULL,
			actor_username VARCHAR(64), ip VARCHAR(64), action VARCHAR(64), target VARCHAR(255),
			detail LONGTEXT CHECK (json_valid(detail)), ok TINYINT, reseller_id BIGINT NOT NULL DEFAULT 0)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

// Uzlaştırma YALNIZ gerçekten sahipsizi (kullanıcı YOK + domains kaydı YOK + panel
// imzalı + yeterince eski) söker; aktif, askıdaki, yarım-sahipsiz, yabancı ve taze
// birimler korunur; denetime yazar; ikinci koşu no-op; DB okunamazsa dokunmaz.
func TestSahipsizTenantFPMTemizle_YalnizSahipsiz(t *testing.T) {
	o := sokumDikis(t)
	db := sokum_panel(t)
	sahipsizMinYas = 10 * time.Minute
	eski := time.Hour
	tenantKur(t, o, "c_sahipsiz", eski) // kullanıcı yok, kayıt yok → SÖKÜLÜR
	tenantKur(t, o, "c_aktif", eski)    // kayıt + kullanıcı
	tenantKur(t, o, "c_askida", eski)   // askıdaki kayıt (kullanıcı da yok sayalım) → korunur
	tenantKur(t, o, "c_kayitli", eski)  // kayıt var, kullanıcı yok (bozuk ama sahipsiz değil)
	tenantKur(t, o, "c_kulvar", eski)   // kullanıcı var, kayıt yok (yarım) → korunur
	tenantKur(t, o, "c_taze", 0)        // sahipsiz ama TAZE → korunur
	tenantKur(t, o, "c_yabanci_bir", eski)
	// yabancı: birim başka bir şeyin (imza yok) → sk'nin hiçbir izine dokunulmaz
	sk_yaz(t, tenantUnitPath("c_yabanci_bir"), "[Unit]\nDescription=Baska urun\n[Service]\nExecStart=/bin/true\n")
	old := time.Now().Add(-eski)
	_ = os.Chtimes(tenantUnitPath("c_yabanci_bir"), old, old)
	// yalnız cfg dizini kalmış sahipsiz (181: c_dgrplan_test) → SÖKÜLÜR
	sk_yaz(t, filepath.Join(tenantCfgDir("c_dgr"), "php-fpm.conf"), renderTenantGlobalCfg("c_dgr"))
	_ = os.Chtimes(tenantCfgDir("c_dgr"), old, old)
	// panelin deseni dışında birim: dokunulmaz
	sk_yaz(t, filepath.Join(tenantUnitDir, "php-fpm.service"), "[Unit]\n")
	sk_yaz(t, filepath.Join(tenantUnitDir, "php-fpm-www.service"), "[Unit]\n")

	for _, q := range []string{
		`INSERT INTO domains (id, sistem_kullanici) VALUES (1,'c_aktif'),(3,'c_kayitli')`,
		`INSERT INTO domains (id, sistem_kullanici, askida) VALUES (2,'c_askida',1)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	o.kullanici["c_aktif"] = true
	o.kullanici["c_kulvar"] = true

	temiz, err := SahipsizTenantFPMTemizle(db)
	if err != nil {
		t.Fatal(err)
	}
	var adlar []string
	for _, x := range temiz {
		adlar = append(adlar, x.SK)
	}
	if strings.Join(adlar, ",") != "c_dgr,c_sahipsiz" {
		t.Fatalf("yalnız c_dgr,c_sahipsiz sökülmeli: %v", adlar)
	}
	izlerKalmadi(t, o, "c_sahipsiz")
	if izVar(tenantCfgDir("c_dgr")) {
		t.Fatal("yalnız cfg dizini kalmış sahipsiz sökülmeli")
	}
	for _, sk := range []string{"c_aktif", "c_askida", "c_kayitli", "c_kulvar", "c_taze", "c_yabanci_bir"} {
		izlerDuruyor(t, sk)
	}
	for _, y := range []string{"php-fpm.service", "php-fpm-www.service"} {
		if !izVar(filepath.Join(tenantUnitDir, y)) {
			t.Errorf("desen dışı birim silindi: %s", y)
		}
	}
	for _, sk := range []string{"c_aktif", "c_askida", "c_kayitli", "c_kulvar", "c_taze", "c_yabanci_bir"} {
		if o.komutVar("systemctl disable --now php-fpm-" + sk + ".service") {
			t.Errorf("korunan %s durdurulmamalı", sk)
		}
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='sistem.fpm_sahipsiz_temizlendi'
		AND target IN ('c_sahipsiz','c_dgr') AND ok=1`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("2 denetim satırı beklenir: n=%d err=%v", n, err)
	}

	// İdempotent: ikinci koşu hiçbir şey sökmez, yeni denetim yazmaz.
	temiz, err = SahipsizTenantFPMTemizle(db)
	if err != nil || len(temiz) != 0 {
		t.Fatalf("ikinci koşu no-op olmalı: %v %v", temiz, err)
	}
	_ = db.QueryRow(`SELECT COUNT(*) FROM audit_log`).Scan(&n)
	if n != 2 {
		t.Fatalf("ikinci koşu denetim yazmamalı: %d", n)
	}

	// DB okunamazsa (belirsizlik) HİÇBİR ŞEY sökülmez.
	tenantKur(t, o, "c_sahipsiz2", eski)
	_ = db.Close()
	if _, err := SahipsizTenantFPMTemizle(db); err == nil {
		t.Fatal("DB okunamazken hata dönmeli")
	}
	izlerDuruyor(t, "c_sahipsiz2")
	if _, err := SahipsizTenantFPMTemizle(nil); err == nil {
		t.Fatal("DB yokken hata dönmeli")
	}
}

// EnableTenantFPM süren (fpmInflight) hesaba uzlaştırma dokunmaz.
func TestSahipsizTenantFPMTemizle_SurenKurulumaDokunmaz(t *testing.T) {
	o := sokumDikis(t)
	db := sokum_panel(t)
	tenantKur(t, o, "c_inflight", time.Hour)
	fpmInflight.Store("c_inflight", struct{}{})
	defer fpmInflight.Delete("c_inflight")
	if temiz, err := SahipsizTenantFPMTemizle(db); err != nil || len(temiz) != 0 {
		t.Fatalf("süren kuruluma dokunulmamalı: %v %v", temiz, err)
	}
	izlerDuruyor(t, "c_inflight")
}
