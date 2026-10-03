package provisioner

// hesap_aski_test.go — askı HESAP düzeyindedir (kullanıcı kararı 2026-09-30).
//
//   - Ana vhost askı durumunu TEK kaynaktan okur; okunamazsa vhost'a dokunmaz.
//   - Ana ve alt alan adı askı vhost'u AYNI şablondan (503 + ACME açık).
//   - PHP-FPM havuzları askıda durur, askıdan alınca bayt bayt geri gelir;
//     havuzu başlatabilecek yollar askıda kapalıdır.
//
// Gerçek (geçici) MariaDB: GOSP_TEST_GECICI_DSN (go_test db:true); yoksa DB'li
// testler ATLANIR. Havuz dizinleri, unit dizini ve komutlar dikişlerle geçici /
// sahte — ana makinenin php-fpm'ine, systemd'sine, /etc'sine dokunulmaz.

import (
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

const ha_sk = "c_hapool"

// ha_panel — geçici MariaDB'de domains tablosu; pkgDB ona çevrilir.
func ha_panel(t *testing.T) *sql.DB {
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
	ad := fmt.Sprintf("gosp_test_havuzaski_%d", time.Now().UnixNano())
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
	if _, err := db.Exec(`CREATE TABLE domains (id BIGINT UNSIGNED PRIMARY KEY,
		sistem_kullanici VARCHAR(64) NOT NULL, askida TINYINT NOT NULL DEFAULT 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO domains (id, sistem_kullanici) VALUES (1, ?)`, ha_sk); err != nil {
		t.Fatal(err)
	}
	eski := pkgDB
	pkgDB = db
	t.Cleanup(func() { pkgDB = eski })
	return db
}

func ha_askiya(t *testing.T, db *sql.DB, askida int) {
	t.Helper()
	if _, err := db.Exec(`UPDATE domains SET askida=? WHERE id=1`, askida); err != nil {
		t.Fatal(err)
	}
}

// Ana vhost render'ı askıyı tek kaynaktan okur; okunamazsa DOKUNMAZ (hata).
func TestVhostAskiDurumu_TekKaynak_OkunamazsaDokunmaz(t *testing.T) {
	db := ha_panel(t)
	o := VhostOpts{AlanAdi: "hapool.test"}
	if err := vhostAskiDurumu(&o, ha_sk); err != nil || o.Askida {
		t.Fatalf("askıda olmayan hesap yayında render edilmeli: askida=%v err=%v", o.Askida, err)
	}
	ha_askiya(t, db, 1)
	o = VhostOpts{AlanAdi: "hapool.test"}
	if err := vhostAskiDurumu(&o, ha_sk); err != nil || !o.Askida {
		t.Fatalf("askıdaki hesap 503 render edilmeli: askida=%v err=%v", o.Askida, err)
	}
	_ = db.Close() // durum artık okunamaz
	o = VhostOpts{AlanAdi: "hapool.test"}
	if err := vhostAskiDurumu(&o, ha_sk); err == nil {
		t.Fatalf("askı durumu okunamazken vhost yazılmamalı (hata beklenirdi), askida=%v", o.Askida)
	}
}

// Ana ve alt alan adı askı vhost'u AYNI şablon: 503, marka varlıkları, ACME açık.
func TestAskiVhostGovde_AnaVeAltAyniSablon(t *testing.T) {
	ana, err := AskiVhostGovde("hapool.test", "hapool.test www.hapool.test", "", "")
	if err != nil {
		t.Fatal(err)
	}
	alt, err := AskiVhostGovde("blog.hapool.test", "blog.hapool.test", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for ad, g := range map[string]string{"ana": ana, "alt": alt} {
		for _, parca := range []string{"return 503;", "error_page 503 /_gosp_askida.html;",
			"location /.well-known/acme-challenge/ {\n        root /var/www/acme;", "location ^~ /_gosp/ {"} {
			if !strings.Contains(g, parca) {
				t.Errorf("%s askı vhost'unda %q yok:\n%s", ad, parca, g)
			}
		}
		if strings.Contains(g, "listen 443") || strings.Contains(g, "fastcgi_pass") {
			t.Errorf("%s: sertifikasız askı vhost'unda 443/PHP olmamalı:\n%s", ad, g)
		}
	}
	if !strings.Contains(ana, "server_name hapool.test www.hapool.test;") || !strings.Contains(alt, "server_name blog.hapool.test;") {
		t.Fatalf("server_name yanlış:\n%s\n%s", ana, alt)
	}
	if strings.ReplaceAll(alt, "blog.hapool.test", "X") != strings.ReplaceAll(strings.ReplaceAll(ana, "hapool.test www.hapool.test", "X"), "hapool.test", "X") {
		t.Fatalf("ana ve alt alan askı vhost'u aynı şablondan gelmeli:\n%s\n---\n%s", ana, alt)
	}
	ssl, err := AskiVhostGovde("blog.hapool.test", "blog.hapool.test", "/k/blog.crt", "/k/blog.key")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ssl, "listen 443 ssl;") || !strings.Contains(ssl, "ssl_certificate     /k/blog.crt;") ||
		strings.Count(ssl, "return 503;") != 2 {
		t.Fatalf("sertifikalı askı vhost'unda 443 de 503 olmalı:\n%s", ssl)
	}
}

// Havuzu yazıp başlatan yollar askıda HİÇBİR ŞEYE dokunmadan reddeder.
func TestHesapAskiKapisi_HavuzBaslatanYollarReddeder(t *testing.T) {
	db := ha_panel(t)
	ha_askiya(t, db, 1)
	if _, _, err := writePoolValidated(ha_sk, "8.3"); !errors.Is(err, hesaplar.ErrHesapAskida) {
		t.Errorf("writePoolValidated askıda reddetmeli: %v", err)
	}
	// SetPHPVersion havuzu writePoolValidated ile yazar → aynı kapı (askıdaki
	// hesabın havuz dosyası .askida olduğundan silme döngüsü de ona dokunmaz).
	if _, err := SetPHPVersion("hapool.test", ha_sk, "8.3", "", "", "", "php-fpm"); !errors.Is(err, hesaplar.ErrHesapAskida) {
		t.Errorf("SetPHPVersion askıda reddetmeli: %v", err)
	}
	if _, err := EnableTenantFPM(db, 1, ha_sk, "8.3"); !errors.Is(err, hesaplar.ErrHesapAskida) {
		t.Errorf("EnableTenantFPM askıda reddetmeli: %v", err)
	}
	// Kapı askıya özgüdür: askıdan alınınca aynı çağrı kapıdan geçer (sonraki
	// denetime — sistem kullanıcısı yok — takılır, askı hatası DEĞİL).
	ha_askiya(t, db, 0)
	if _, _, err := writePoolValidated(ha_sk, "8.3"); err == nil || errors.Is(err, hesaplar.ErrHesapAskida) {
		t.Errorf("askıda olmayan hesapta kapı reddetmemeli: %v", err)
	}
	_ = db.Close()
	if err := HesapAskiKapisi(ha_sk); err == nil {
		t.Errorf("askı durumu okunamazsa havuz başlatılmamalı (hata beklenirdi)")
	}
}

// Açılış taraması / watchdog askıdaki hesabın per-tenant servisine dokunmaz.
func TestTenantFPMBakimi_AskidakiHesabaDokunmaz(t *testing.T) {
	db := ha_panel(t)
	eski := tenantUnitDir
	tenantUnitDir = t.TempDir()
	t.Cleanup(func() { tenantUnitDir = eski })
	if tenantFPMBakimi(ha_sk) {
		t.Fatal("per-tenant kurulu değilken bakım yapılmamalı")
	}
	if err := os.WriteFile(tenantUnitPath(ha_sk), []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !tenantFPMBakimi(ha_sk) {
		t.Fatal("askıda olmayan per-tenant hesap bakımdan geçmeli")
	}
	ha_askiya(t, db, 1)
	if tenantFPMBakimi(ha_sk) {
		t.Fatal("askıdaki hesabın durdurulmuş servisine açılış/watchdog DOKUNMAMALI")
	}
}

type ha_havuz struct {
	dizin1, dizin2 string
	komutlar       *[]string
	paylasilana    *int
}

// ha_havuzDikis — iki paylaşılan master dizini (geçici), sahte komut/servis.
func ha_havuzDikis(t *testing.T, tenant bool, enableHata error) ha_havuz {
	t.Helper()
	e1, e2, e3, e4, e5 := askiKomut, askiTenantAktif, askiSoketBekle, askiPaylasilana, askiHavuzDizini
	t.Cleanup(func() {
		askiKomut, askiTenantAktif, askiSoketBekle, askiPaylasilana, askiHavuzDizini = e1, e2, e3, e4, e5
	})
	kok := t.TempDir()
	h := ha_havuz{dizin1: kok + "/php83", dizin2: kok + "/php82", komutlar: &[]string{}, paylasilana: new(int)}
	for _, d := range []string{h.dizin1, h.dizin2} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	askiHavuzDizini = func() map[string]string { return map[string]string{h.dizin1: "php-fpm", h.dizin2: "php82-php-fpm"} }
	askiTenantAktif = func(string) bool { return tenant }
	askiSoketBekle = func(string) bool { return true }
	askiPaylasilana = func(*sql.DB, int64, string, string, ...string) error { *h.paylasilana++; return nil }
	askiKomut = func(ad string, arg ...string) ([]byte, error) {
		k := ad + " " + strings.Join(arg, " ")
		*h.komutlar = append(*h.komutlar, k)
		if enableHata != nil && strings.HasPrefix(k, "systemctl enable") {
			return []byte("Job failed"), enableHata
		}
		return nil, nil
	}
	return h
}

func ha_yaz(t *testing.T, yol, icerik string) {
	t.Helper()
	if err := os.WriteFile(yol, []byte(icerik), 0o644); err != nil {
		t.Fatal(err)
	}
}

func ha_oku(t *testing.T, yol string) string {
	t.Helper()
	b, err := os.ReadFile(yol)
	if err != nil {
		t.Fatalf("%s okunamadı: %v", yol, err)
	}
	return string(b)
}

func ha_yok(t *testing.T, yol string) {
	t.Helper()
	if _, err := os.Lstat(yol); err == nil {
		t.Fatalf("%s olmamalıydı", yol)
	}
}

func ha_komutVar(t *testing.T, h ha_havuz, k string) {
	t.Helper()
	for _, c := range *h.komutlar {
		if c == k {
			return
		}
	}
	t.Fatalf("%q çalıştırılmadı: %v", k, *h.komutlar)
}

// Paylaşılan master havuzları: askıda yüklenmez (.askida), askıdan alınca AYNEN döner;
// başka hesabın havuzuna ve per-tenant .bak yedeğine dokunulmaz.
func TestPHPHavuzAski_PaylasilanHavuz_GidisDonus(t *testing.T) {
	h := ha_havuzDikis(t, false, nil)
	ana1, alt1 := filepath.Join(h.dizin1, ha_sk+".conf"), filepath.Join(h.dizin1, ha_sk+"_s7.conf")
	komsu := filepath.Join(h.dizin1, ha_sk+"x.conf") // önek benzeri BAŞKA hesap
	bak := filepath.Join(h.dizin2, ha_sk+".conf.bak")
	ha_yaz(t, ana1, "[c_hapool]\npm = static\n")
	ha_yaz(t, alt1, "[c_hapool_s7]\n")
	ha_yaz(t, komsu, "[c_hapoolx]\n")
	ha_yaz(t, bak, "[yedek]\n")

	if err := PHPHavuzAski(nil, 1, ha_sk, "8.3", []string{ha_sk + "_s7"}, true); err != nil {
		t.Fatalf("askıya alma: %v", err)
	}
	ha_yok(t, ana1)
	ha_yok(t, alt1)
	if ha_oku(t, ana1+AskiHavuzEki) != "[c_hapool]\npm = static\n" || ha_oku(t, alt1+AskiHavuzEki) != "[c_hapool_s7]\n" {
		t.Fatal("askıdaki havuz içeriği korunmalı")
	}
	if ha_oku(t, komsu) != "[c_hapoolx]\n" || ha_oku(t, bak) != "[yedek]\n" {
		t.Fatal("başka hesabın havuzuna / per-tenant yedeğine dokunulmamalı")
	}
	ha_komutVar(t, h, "systemctl reload-or-restart php-fpm")
	for _, k := range *h.komutlar {
		if strings.Contains(k, "php82") {
			t.Fatalf("değişmeyen master yeniden yüklenmemeli: %v", *h.komutlar)
		}
	}
	// İdempotent: ikinci askıya alma bir şey değiştirmez, reload yok.
	*h.komutlar = nil
	if err := PHPHavuzAski(nil, 1, ha_sk, "8.3", []string{ha_sk + "_s7"}, true); err != nil || len(*h.komutlar) != 0 {
		t.Fatalf("ikinci askıya alma zararsız olmalı: err=%v komutlar=%v", err, *h.komutlar)
	}
	if err := PHPHavuzAski(nil, 1, ha_sk, "8.3", []string{ha_sk + "_s7"}, false); err != nil {
		t.Fatalf("askıdan alma: %v", err)
	}
	if ha_oku(t, ana1) != "[c_hapool]\npm = static\n" || ha_oku(t, alt1) != "[c_hapool_s7]\n" {
		t.Fatal("askıdan alınca havuzlar bayt bayt geri gelmeli")
	}
	ha_yok(t, ana1+AskiHavuzEki)
	ha_komutVar(t, h, "systemctl reload-or-restart php-fpm")
}

// Askıdayken başka bir yol havuzu yeniden yazdıysa askıdan alma GÜNCEL olanı tutar.
func TestPHPHavuzAski_IkiKopya_GuncelKalir(t *testing.T) {
	h := ha_havuzDikis(t, false, nil)
	ana := filepath.Join(h.dizin1, ha_sk+".conf")
	ha_yaz(t, ana, "yeni\n")
	ha_yaz(t, ana+AskiHavuzEki, "eski\n")
	if err := PHPHavuzAski(nil, 1, ha_sk, "8.3", nil, false); err != nil {
		t.Fatal(err)
	}
	if ha_oku(t, ana) != "yeni\n" {
		t.Fatal("güncel havuz korunmalı")
	}
	ha_yok(t, ana+AskiHavuzEki)
}

// Per-tenant FPM: askıda disable --now (açılışta da başlamaz), askıdan alınca
// enable --now; başlamazsa paylaşılan düzene güvenli dönüş.
func TestPHPHavuzAski_PerTenant(t *testing.T) {
	h := ha_havuzDikis(t, true, nil)
	if err := PHPHavuzAski(nil, 1, ha_sk, "8.3", nil, true); err != nil {
		t.Fatal(err)
	}
	ha_komutVar(t, h, "systemctl disable --now php-fpm-c_hapool.service")
	if err := PHPHavuzAski(nil, 1, ha_sk, "8.3", nil, false); err != nil {
		t.Fatal(err)
	}
	ha_komutVar(t, h, "systemctl enable --now php-fpm-c_hapool.service")
	if *h.paylasilana != 0 {
		t.Fatal("servis başladıysa paylaşılana dönülmemeli")
	}

	h = ha_havuzDikis(t, true, errors.New("exit status 1"))
	if err := PHPHavuzAski(nil, 1, ha_sk, "8.3", nil, false); err != nil {
		t.Fatal(err)
	}
	if *h.paylasilana != 1 {
		t.Fatal("servis başlamazsa paylaşılan düzene güvenli dönüş yapılmalı")
	}
}

// Başka hesabın havuz adı verilemez.
func TestPHPHavuzAski_YabanciHavuzReddedilir(t *testing.T) {
	h := ha_havuzDikis(t, false, nil)
	komsu := filepath.Join(h.dizin1, "c_diger.conf")
	ha_yaz(t, komsu, "x")
	if err := PHPHavuzAski(nil, 1, ha_sk, "8.3", []string{"c_diger"}, true); err == nil {
		t.Fatal("başka hesabın havuzu reddedilmeli")
	}
	if ha_oku(t, komsu) != "x" || len(*h.komutlar) != 0 {
		t.Fatal("reddedilen istekte hiçbir şeye dokunulmamalı")
	}
}

// Askıdan almada per-tenant servis başlar ama SOKET oluşmazsa paylaşılan düzene
// güvenli dönüş yapılır (yoksa site 502'de kalırdı).
func TestPHPHavuzAski_PerTenant_SoketYoksaPaylasilana(t *testing.T) {
	h := ha_havuzDikis(t, true, nil)
	askiSoketBekle = func(string) bool { return false }
	if err := PHPHavuzAski(nil, 1, ha_sk, "8.3", nil, false); err != nil {
		t.Fatal(err)
	}
	ha_komutVar(t, h, "systemctl enable --now php-fpm-c_hapool.service")
	if *h.paylasilana != 1 {
		t.Fatal("soket oluşmazsa paylaşılan düzene dönülmeli")
	}
	for _, k := range *h.komutlar {
		if strings.HasPrefix(k, "restorecon") {
			t.Fatalf("başarısız başlatmada soket dizini etiketlenmemeli: %v", *h.komutlar)
		}
	}
}

// Askıya alırken per-tenant servis durdurulurken watchdog "ölü servis" sanıp
// yeniden başlatmasın: disable anında sk uçuşta (fpmInflight) işaretli, sonra değil.
func TestPHPHavuzAski_PerTenant_DurdururkenUcusta(t *testing.T) {
	h := ha_havuzDikis(t, true, nil)
	ucusta := false
	askiKomut = func(ad string, arg ...string) ([]byte, error) {
		k := ad + " " + strings.Join(arg, " ")
		*h.komutlar = append(*h.komutlar, k)
		if strings.HasPrefix(k, "systemctl disable --now php-fpm-") {
			_, ucusta = fpmInflight.Load(ha_sk)
		}
		return nil, nil
	}
	if err := PHPHavuzAski(nil, 1, ha_sk, "8.3", nil, true); err != nil {
		t.Fatal(err)
	}
	if !ucusta {
		t.Fatal("durdurma sırasında hesap uçuşta işaretli olmalı (watchdog dokunmasın)")
	}
	if _, kaldi := fpmInflight.Load(ha_sk); kaldi {
		t.Fatal("işaret işlem bitince kalkmalı")
	}
}

// Hesap silinirken askıda saklanan havuzlar (ana + alt alan) her master dizininden
// gider; başka hesabın (önek benzeri dahil) dosyalarına ve yüklü havuzlara dokunulmaz.
func TestAskiHavuzArtiklariniSil(t *testing.T) {
	h := ha_havuzDikis(t, false, nil)
	silinecek := []string{
		filepath.Join(h.dizin1, ha_sk+".conf"+AskiHavuzEki),
		filepath.Join(h.dizin1, ha_sk+"_s7.conf"+AskiHavuzEki),
		filepath.Join(h.dizin2, ha_sk+"_s12.conf"+AskiHavuzEki),
	}
	kalacak := []string{
		filepath.Join(h.dizin1, ha_sk+"x.conf"+AskiHavuzEki), // başka hesap (önek benzeri)
		filepath.Join(h.dizin1, "c_diger.conf"+AskiHavuzEki),
		filepath.Join(h.dizin2, "c_diger_s3.conf"),
	}
	for _, y := range append(append([]string{}, silinecek...), kalacak...) {
		ha_yaz(t, y, "x")
	}
	askiHavuzArtiklariniSil(ha_sk)
	for _, y := range silinecek {
		ha_yok(t, y)
	}
	for _, y := range kalacak {
		if ha_oku(t, y) != "x" {
			t.Fatalf("%s değişmemeli", y)
		}
	}
	if len(*h.komutlar) != 0 {
		t.Fatalf("yüklü olmayan dosyalar için reload gerekmez: %v", *h.komutlar)
	}
}

// 🔴 Kiracılar arası silme yok: foo.com (c_foo) silinirken foo-s5.com (c_foo_s5)
// ve foo-store.com (c_foo_store) hesaplarının askıda saklanan havuzlarına
// dokunulmaz. c_foo_s5.conf.askida hem c_foo'nun 5 numaralı alt havuzu hem
// c_foo_s5 hesabının ana havuzu olabilir: o hesap yaşıyorsa ONUNDUR.
func TestAskiHavuzArtiklariniSil_KomsuKiraci(t *testing.T) {
	h := ha_havuzDikis(t, false, nil)
	eski := askiHesapVar
	t.Cleanup(func() { askiHesapVar = eski })
	askiHesapVar = func(ad string) bool { return ad == "c_foo_s5" || ad == "c_foo_store" }
	silinecek := []string{
		filepath.Join(h.dizin1, "c_foo.conf"+AskiHavuzEki),
		filepath.Join(h.dizin2, "c_foo_s7.conf"+AskiHavuzEki),
	}
	kalacak := []string{
		filepath.Join(h.dizin1, "c_foo_s5.conf"+AskiHavuzEki),    // yaşayan komşu hesap c_foo_s5
		filepath.Join(h.dizin1, "c_foo_store.conf"+AskiHavuzEki), // komşu hesap c_foo_store
		filepath.Join(h.dizin2, "c_foo_s5_s2.conf"+AskiHavuzEki), // komşunun alt havuzu
		filepath.Join(h.dizin2, "c_foo_s07.conf"+AskiHavuzEki),   // php.SubPoolName üretmez
		filepath.Join(h.dizin2, "c_foo_s9x.conf"+AskiHavuzEki),
	}
	for _, y := range append(append([]string{}, silinecek...), kalacak...) {
		ha_yaz(t, y, "x")
	}
	askiHavuzArtiklariniSil("c_foo")
	for _, y := range silinecek {
		ha_yok(t, y)
	}
	for _, y := range kalacak {
		if ha_oku(t, y) != "x" {
			t.Fatalf("%s silinmemeli", y)
		}
	}
}

// Havuz adı TAM eşleşir: hesabın kendisi ya da sk_s<id>.
func TestGecerliHavuzAdi_TamEslesme(t *testing.T) {
	for ad, w := range map[string]bool{
		"c_shop": true, "c_shop_s1": true, "c_shop_s120": true,
		"c_shop_store": false, "c_shop_s": false, "c_shop_s07": false, "c_shop_s5x": false,
		"c_shop_s5_s2": false, "c_shopx": false, "c_diger_s3": false,
	} {
		if g := gecerliHavuzAdi("c_shop", ad); g != w {
			t.Errorf("gecerliHavuzAdi(c_shop, %q) = %v, beklenen %v", ad, g, w)
		}
	}
}

// Hesap silinirken alt alan vhost'ları: "sub_c_shop_*" deseni komşu
// c_shop_store'un dosyalarını da yakalardı — yalnız c_shop'unkiler.
func TestHesabinAltVhostlari_KomsuyaDokunmaz(t *testing.T) {
	d := t.TempDir()
	for _, ad := range []string{"sub_c_shop_blog.conf", "sub_c_shop_www-2.conf", "sub_c_shop_store_blog.conf",
		"sub_c_shopx_a.conf", "dom_c_shop.conf", "sub_c_shop_.conf"} {
		ha_yaz(t, filepath.Join(d, ad), "x")
	}
	var got []string
	for _, y := range hesabinAltVhostlari(d, "c_shop") {
		got = append(got, filepath.Base(y))
	}
	if strings.Join(got, ",") != "sub_c_shop_blog.conf,sub_c_shop_www-2.conf" {
		t.Fatalf("yalnız hesabın alt alan vhost'ları: %v", got)
	}
	if got := hesabinAltVhostlari(d, "c_*"); got != nil {
		t.Fatalf("joker içeren sk başka hesabın dosyasını seçmemeli: %v", got)
	}
}

// Silinen alan adının günlükleri: shop.com silinirken shop.com.tr'ninkiler kalır.
func TestAlanAdiGunlukleri_KomsuyaDokunmaz(t *testing.T) {
	d := t.TempDir()
	for _, ad := range []string{"shop.com.access.log", "shop.com.error.log", "shop.com.access.log-20260101.gz",
		"shop.com.tr.access.log", "blog.shop.com.access.log"} {
		ha_yaz(t, filepath.Join(d, ad), "x")
	}
	var got []string
	for _, y := range alanAdiGunlukleri(d, "shop.com") {
		got = append(got, filepath.Base(y))
	}
	if strings.Join(got, ",") != "shop.com.access.log,shop.com.access.log-20260101.gz,shop.com.error.log" {
		t.Fatalf("yalnız alan adının kendi günlükleri: %v", got)
	}
}

// Paylaşılana dönüş (RollbackToSharedFPM) ve unit drift onarımı askıdaki
// hesapta HİÇBİR şeye dokunmaz (servisi yeniden başlatır / paylaşılan havuzu
// geri getirirdi → askı delinirdi). Kapı delinse bile ana makineye dokunulmasın:
// komutlar sahte PATH'ten, unit ve sentinel dizinleri geçici.
func TestHesapAskiKapisi_PaylasilanaDonusVeDriftOnarimiReddeder(t *testing.T) {
	db := ha_panel(t)
	sahte := t.TempDir()
	for _, k := range []string{"systemctl", "restorecon", "nginx"} {
		if err := os.WriteFile(filepath.Join(sahte, k), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", sahte)
	e1, e2 := tenantUnitDir, izolasyonSentinelDir
	t.Cleanup(func() { tenantUnitDir, izolasyonSentinelDir = e1, e2 })
	tenantUnitDir, izolasyonSentinelDir = t.TempDir(), t.TempDir()
	unit := tenantUnitPath(ha_sk)
	ha_yaz(t, unit, "eski-birim\n")
	ha_askiya(t, db, 1)

	if err := RollbackToSharedFPM(db, 1, ha_sk, "8.3", "test"); !errors.Is(err, hesaplar.ErrHesapAskida) {
		t.Fatalf("RollbackToSharedFPM askıda reddetmeli: %v", err)
	}
	if RepairTenantUnitDrift(1, ha_sk, "8.3") {
		t.Fatal("RepairTenantUnitDrift askıdaki hesaba dokunmamalı")
	}
	if ha_oku(t, unit) != "eski-birim\n" {
		t.Fatal("askıdaki hesabın unit dosyası değişmemeli / silinmemeli")
	}
	if ents, _ := os.ReadDir(izolasyonSentinelDir); len(ents) != 0 {
		t.Fatal("askıda izolasyon kaybı işaretlenmemeli")
	}
}
