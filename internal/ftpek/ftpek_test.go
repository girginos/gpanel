package ftpek

// ftpek_test.go — ek FTP hesapları çekirdeği.
//
// Saf testler (doğrulayıcılar, FS güvenliği) DB istemez. DB'li testler ağa kapalı geçici
// MariaDB ister (GOSP_TEST_GECICI_DSN; yoksa atlanır). Şema: üretimdeki ftp_accounts
// + GERÇEK göç dosyası 0150 uygulanır (göçün kendisi de sınanır).

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"girginospanel/internal/hesaplar"

	"github.com/go-sql-driver/mysql"
)

const testSK = "c_ornek_com"

// ---- doğrulayıcı tabloları ----

func TestDizinCoz_KacisVektorleri(t *testing.T) {
	uzun := strings.Repeat("a", 300)
	derin := strings.Repeat("d/", 14)
	kotu := []struct{ ad, girdi string }{
		{"bos", ""},
		{"yalniz bosluk", "   "},
		{"ust dizin", "../etc"},
		{"ic ust dizin", "public_html/../../etc"},
		{"sadece ..", ".."},
		{"nokta bilesen", "./a"},
		{"gizli dizin", ".ssh"},
		{"gizli ic", "public_html/.git/hooks"},
		{"mutlak /etc", "/etc/passwd"},
		{"mutlak /root", "/root/.ssh"},
		{"mutlak baska home", "/home/c_baska_com/public_html"},
		{"mutlak /proc", "/proc/self"},
		{"NUL", "a\x00b"},
		{"newline", "a\nb"},
		{"ters bolu", `a\..\b`},
		{"RLO biçim", "a‮b"},
		{"gecersiz utf8", "a\xffb"},
		{"cok uzun", uzun},
		{"cok derin", derin},
		{"ev dizini", "/home/" + testSK},
		{"ev dizini bolu", "/"},
		{"tilde", "~root"},
		{"joker", "a*b"},
		{"bosluklu bilesen", "a /b"},
	}
	for _, c := range kotu {
		t.Run(c.ad, func(t *testing.T) {
			if rel, err := DizinCoz(c.girdi, testSK); err == nil {
				t.Fatalf("REDDEDİLMELİYDİ: %q → %q", c.girdi, rel)
			}
		})
	}
	iyi := map[string]string{
		"public_html":                    "public_html",
		"/public_html":                   "public_html",
		"/public_html/foto/2026":         "public_html/foto/2026",
		"public_html//foto/":             "public_html/foto",
		"/home/" + testSK + "/web/yedek": "web/yedek",
		"Müşteri Dosyaları/İndirilenler": "Müşteri Dosyaları/İndirilenler",
		"/varlik/belgeler":               "varlik/belgeler", // sistem kökü DEĞİL ("var" ≠ "varlik")
		"  /public_html/x  ":             "public_html/x",
	}
	for girdi, bekle := range iyi {
		got, err := DizinCoz(girdi, testSK)
		if err != nil || got != bekle {
			t.Fatalf("%q → %q,%v; beklenen %q", girdi, got, err, bekle)
		}
	}
}

func TestKullaniciAdi(t *testing.T) {
	if u, ek, err := KullaniciAdi(testSK, " Foto_1 "); err != nil || u != testSK+"-foto_1" || ek != "foto_1" {
		t.Fatalf("beklenmeyen: %q %q %v", u, ek, err)
	}
	for _, kotu := range []string{"", "a b", "a-b", "ç", "root", "ROOT", "admin", "a;drop", "a'b", "_x", strings.Repeat("x", 25), "../x", "a\x00"} {
		if u, _, err := KullaniciAdi(testSK, kotu); err == nil {
			t.Fatalf("%q kabul edildi → %q", kotu, u)
		}
	}
	if _, _, err := KullaniciAdi("root", "x"); err == nil {
		t.Fatal("geçersiz sistem kullanıcısı kabul edildi")
	}
	// Ek hesap adı HİÇBİR sistem kullanıcısıyla çakışamaz: ayraç "-" sk'da yok.
	if u, _, _ := KullaniciAdi(testSK, "x"); reSK.MatchString(u) {
		t.Fatalf("ek hesap adı sistem kullanıcısı desenine uyuyor: %s", u)
	}
}

// ---- dosya sistemi güvenliği ----

func geciciHome(t *testing.T) string {
	t.Helper()
	kok := t.TempDir()
	eski := HomeKok
	HomeKok = kok
	t.Cleanup(func() { HomeKok = eski })
	home := filepath.Join(kok, testSK)
	if err := os.MkdirAll(filepath.Join(home, "public_html", "foto"), 0o755); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestDizinHazirla_SymlinkVeTur(t *testing.T) {
	home := geciciHome(t)
	dis := t.TempDir()
	if err := os.Symlink(dis, filepath.Join(home, "kacak")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dis, filepath.Join(home, "public_html", "kacak2")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "dosya.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		ad, rel string
		olustur bool
	}{
		{"symlink son bilesen", "kacak", false},
		{"symlink ara bilesen + olustur", "kacak/yeni", true},
		{"symlink ic", "public_html/kacak2", true},
		{"dosya klasor degil", "dosya.txt", false},
		{"yok ve olusturma yok", "yok/yer", false},
	} {
		t.Run(c.ad, func(t *testing.T) {
			if abs, err := DizinHazirla(testSK, c.rel, c.olustur); err == nil {
				t.Fatalf("REDDEDİLMELİYDİ: %s → %s", c.rel, abs)
			}
		})
	}
	// symlink hedefinde (dışarıda) hiçbir şey oluşmadı
	if ents, _ := os.ReadDir(dis); len(ents) != 0 {
		t.Fatalf("symlink üzerinden dış dizinde oluşturma yapıldı: %v", ents)
	}
	abs, err := DizinHazirla(testSK, "public_html/foto", false)
	if err != nil || abs != home+"/public_html/foto" {
		t.Fatalf("mevcut dizin: %q %v", abs, err)
	}
	abs, err = DizinHazirla(testSK, "web/yedek/2026", true)
	if err != nil || abs != home+"/web/yedek/2026" {
		t.Fatalf("oluşturma: %q %v", abs, err)
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		t.Fatalf("dizin oluşmadı: %v", err)
	}
}

func TestHizIzin(t *testing.T) {
	HizSifirla()
	t.Cleanup(func() { HizSifirla(); Simdi = time.Now })
	t0 := time.Now()
	Simdi = func() time.Time { return t0 }
	for i := 0; i < HizMaks; i++ {
		if ok, _ := HizIzin(7); !ok {
			t.Fatalf("%d. istek reddedildi", i)
		}
	}
	if ok, bekle := HizIzin(7); ok || bekle <= 0 {
		t.Fatal("pencere dolunca reddedilmeli")
	}
	if ok, _ := HizIzin(8); !ok {
		t.Fatal("hız sınırı alan adları arasında paylaşılmamalı")
	}
	Simdi = func() time.Time { return t0.Add(HizPencere + time.Second) }
	if ok, _ := HizIzin(7); !ok {
		t.Fatal("pencere geçince serbest kalmalı")
	}
}

// ---- DB'li testler ----

func fdb(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li test atlandı")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = ""
	kok, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	ad := fmt.Sprintf("gosp_test_ftpek_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		_ = kok.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad); _ = kok.Close() })
	cfg.DBName = ad
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(40)
	t.Cleanup(func() { _ = db.Close() })
	eski := hesaplar.FTPParolaHash("Eski-Parola-12345")
	for _, q := range []string{
		`CREATE TABLE domains (id BIGINT UNSIGNED PRIMARY KEY, alan_adi VARCHAR(253) NOT NULL,
			sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '', reseller_id BIGINT NULL,
			is_demo TINYINT NOT NULL DEFAULT 0, askida TINYINT NOT NULL DEFAULT 0)`,
		// Üretimdeki ftp_accounts (göç öncesi hali).
		`CREATE TABLE ftp_accounts (
			id bigint(20) unsigned NOT NULL AUTO_INCREMENT,
			domain_id bigint(20) unsigned NOT NULL,
			username varchar(64) NOT NULL,
			password_md5 varchar(255) NOT NULL,
			home_dir varchar(255) NOT NULL,
			uid_n int(11) NOT NULL,
			gid_n int(11) NOT NULL,
			status enum('active','suspended') DEFAULT 'active',
			created_at timestamp NULL DEFAULT current_timestamp(),
			updated_at timestamp NULL DEFAULT current_timestamp() ON UPDATE current_timestamp(),
			token_gecersiz_ts bigint(20) NOT NULL DEFAULT 0,
			PRIMARY KEY (id), UNIQUE KEY username (username), KEY ix_ftp_domain (domain_id),
			CONSTRAINT fk_ftp_domain FOREIGN KEY (domain_id) REFERENCES domains (id) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id, is_demo, askida) VALUES
			(1,'ornek.com','` + testSK + `',2,0,0), (2,'baska.com','c_baska_com',3,0,0),
			(3,'askida.com','c_askida_com',2,0,1), (4,'demo.com','c_demo_com',2,1,0)`,
		`INSERT INTO ftp_accounts (domain_id, username, password_md5, home_dir, uid_n, gid_n, status) VALUES
			(1,'` + testSK + `','` + eski + `','/home/` + testSK + `',1001,1001,'active'),
			(2,'c_baska_com','` + eski + `','/home/c_baska_com',1002,1002,'active'),
			(3,'c_askida_com','` + eski + `','/home/c_askida_com',1003,1003,'suspended'),
			(4,'c_demo_com','` + eski + `','/home/c_demo_com',1004,1004,'active')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("şema: %v\n%s", err, q)
		}
	}
	// GERÇEK göç dosyası (idempotent: iki kez uygulanır).
	b, err := os.ReadFile("../../migrations/0150_ftp_ek_hesap.sql")
	if err != nil {
		t.Fatal(err)
	}
	var temiz []string
	for _, s := range strings.Split(string(b), "\n") {
		if ts := strings.TrimSpace(s); ts != "" && !strings.HasPrefix(ts, "--") {
			temiz = append(temiz, s)
		}
	}
	for i := 0; i < 2; i++ {
		for _, st := range strings.Split(strings.Join(temiz, "\n"), ";") {
			if st = strings.TrimSpace(st); st != "" {
				if _, err := db.Exec(st); err != nil {
					t.Fatalf("göç 0150 (%d. uygulama): %v", i+1, err)
				}
			}
		}
	}
	geciciHome(t)
	for _, sk := range []string{"c_baska_com", "c_askida_com"} {
		_ = os.MkdirAll(filepath.Join(HomeKok, sk, "public_html"), 0o755)
	}
	LimitOku = func(context.Context, *sql.DB, int64) int { return 0 }
	t.Cleanup(func() { LimitOku = func(ctx context.Context, db *sql.DB, id int64) int { return 0 } })
	return db
}

const pw = "Guclu-Parola-2026x"

func olustur(t *testing.T, db *sql.DB, dom int64, ek, dizin string) (*Hesap, error) {
	t.Helper()
	return Olustur(context.Background(), db, OlusturGirdi{DomainID: dom, Ek: ek, Dizin: dizin, DizinOlustur: true, Parola: pw})
}

func TestGoc0150_VarsayilanAnaVeIdempotent(t *testing.T) {
	db := fdb(t)
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ftp_accounts WHERE tur='ana' AND ek_etkin=1`).Scan(&n); err != nil || n != 4 {
		t.Fatalf("mevcut satırlar tur=ana/ek_etkin=1 olmalı: n=%d err=%v", n, err)
	}
}

func TestOlustur_SatirPureFTPdSorgulariVeParolaBicimi(t *testing.T) {
	db := fdb(t)
	h, err := olustur(t, db, 1, "Foto", "/public_html/foto")
	if err != nil {
		t.Fatal(err)
	}
	if h.Kullanici != testSK+"-foto" || h.Dizin != "/public_html/foto" || h.Izin != "rw" || h.Durum != "aktif" {
		t.Fatalf("hesap: %+v", h)
	}
	// ftp-setup'taki 4 sorgunun AYNISI (username=\L, status='active'): ek hesap kısıtlı dizinle,
	// ana hesabın uid/gid'iyle döner.
	var hash, dir string
	var uid, gid int
	k := testSK + "-foto"
	for _, c := range []struct {
		sorgu string
		hedef any
	}{
		{`SELECT password_md5 FROM ftp_accounts WHERE username=? AND status='active'`, &hash},
		{`SELECT uid_n FROM ftp_accounts WHERE username=? AND status='active'`, &uid},
		{`SELECT gid_n FROM ftp_accounts WHERE username=? AND status='active'`, &gid},
		{`SELECT home_dir FROM ftp_accounts WHERE username=? AND status='active'`, &dir},
	} {
		if err := db.QueryRow(c.sorgu, k).Scan(c.hedef); err != nil {
			t.Fatalf("pure-ftpd sorgusu ek hesabı bulamadı: %v", err)
		}
	}
	if dir != filepath.Join(HomeKok, testSK, "public_html", "foto") || uid != 1001 || gid != 1001 {
		t.Fatalf("pure-ftpd çıktısı: dir=%s uid=%d gid=%d", dir, uid, gid)
	}
	// Parola biçimi = ana hesapla AYNI fonksiyon ($6$ crypt; MYSQLCrypt=crypt uyumu).
	if !strings.HasPrefix(hash, "$6$") || !hesaplar.FTPParolaDogrula(pw, hash) || strings.Contains(hash, pw) {
		t.Fatalf("parola $6$ crypt olarak saklanmalı: %q", hash)
	}
	var tur string
	var etkin int
	_ = db.QueryRow(`SELECT tur, ek_etkin FROM ftp_accounts WHERE username=?`, k).Scan(&tur, &etkin)
	if tur != "ek" || etkin != 1 {
		t.Fatalf("tur/ek_etkin: %s %d", tur, etkin)
	}
}

func TestOlustur_Reddedilenler(t *testing.T) {
	db := fdb(t)
	dis := t.TempDir()
	_ = os.Symlink(dis, filepath.Join(HomeKok, testSK, "kacak"))
	for _, c := range []struct {
		ad, ek, dizin string
		dom           int64
		kod           int
	}{
		{"dizin ..", "a", "../c_baska_com/public_html", 1, 400},
		{"dizin mutlak", "a", "/etc", 1, 400},
		{"dizin gizli", "a", ".ssh", 1, 400},
		{"symlink", "a", "kacak/x", 1, 400},
		{"ev dizini", "a", "/", 1, 400},
		{"rezerve ad", "root", "web", 1, 400},
		{"bozuk ad", "a b", "web", 1, 400},
		{"askida hosting", "a", "public_html", 3, 409},
		{"demo", "a", "public_html", 4, 403},
		{"olmayan domain", "a", "web", 99, 404},
	} {
		t.Run(c.ad, func(t *testing.T) {
			_, err := olustur(t, db, c.dom, c.ek, c.dizin)
			he, ok := err.(*Hata)
			if !ok || he.HTTP != c.kod {
				t.Fatalf("beklenen %d: %v", c.kod, err)
			}
		})
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM ftp_accounts WHERE tur='ek'`).Scan(&n)
	if n != 0 {
		t.Fatalf("reddedilen isteklerden satır oluştu: %d", n)
	}
	if ents, _ := os.ReadDir(dis); len(ents) != 0 {
		t.Fatalf("symlink hedefine yazıldı: %v", ents)
	}
	// Zayıf parola çekirdekte değil işleyicide denetlenir; kontrol karakteri çekirdekte.
	if _, err := Olustur(context.Background(), db, OlusturGirdi{DomainID: 1, Ek: "x", Dizin: "web", DizinOlustur: true, Parola: "a\nb"}); err == nil {
		t.Fatal("satır sonlu parola kabul edildi")
	}
}

func TestOlustur_CakismaVeAnaHesapAdi(t *testing.T) {
	db := fdb(t)
	if _, err := olustur(t, db, 1, "a", "web"); err != nil {
		t.Fatal(err)
	}
	if _, err := olustur(t, db, 1, "A", "web2"); err == nil || err.(*Hata).HTTP != 409 {
		t.Fatalf("aynı ad (büyük/küçük) 409 olmalı: %v", err)
	}
	// Başka hostingte aynı serbest ad farklı önekle serbest.
	if _, err := olustur(t, db, 2, "a", "public_html"); err != nil {
		t.Fatalf("başka hosting aynı serbest ad: %v", err)
	}
	// Ana hesap adı ek olarak üretilemez (ayraç).
	if _, err := olustur(t, db, 1, strings.TrimPrefix(testSK, "c_"), "web"); err != nil {
		// "ornek_com" geçerli ek ad → c_ornek_com-ornek_com; ana hesabı EZMEZ.
		t.Fatal(err)
	}
	var h string
	_ = db.QueryRow(`SELECT password_md5 FROM ftp_accounts WHERE username=?`, testSK).Scan(&h)
	if !hesaplar.FTPParolaDogrula("Eski-Parola-12345", h) {
		t.Fatal("ana hesap parolası değişti")
	}
}

func TestOlustur_LimitPlanVeVarsayilan(t *testing.T) {
	db := fdb(t)
	LimitOku = func(context.Context, *sql.DB, int64) int { return 3 } // ana + 2 ek
	for i, ek := range []string{"a", "b"} {
		if _, err := olustur(t, db, 1, ek, "web/"+ek); err != nil {
			t.Fatalf("%d: %v", i, err)
		}
	}
	if _, err := olustur(t, db, 1, "c", "web/c"); err == nil || err.(*Hata).HTTP != 409 || !strings.Contains(err.Error(), "en çok 2") {
		t.Fatalf("plan limiti: %v", err)
	}
	if _, err := os.Stat(filepath.Join(HomeKok, testSK, "web", "c")); err == nil {
		t.Fatal("limit aşıldığı halde klasör oluşturuldu")
	}
	LimitOku = func(context.Context, *sql.DB, int64) int { return 1 } // yalnız ana hesap
	if _, err := olustur(t, db, 2, "z", "public_html"); err == nil || !strings.Contains(err.Error(), "hakkı yok") {
		t.Fatalf("max_ftp=1: %v", err)
	}
	LimitOku = func(context.Context, *sql.DB, int64) int { return 0 } // sınırsız → varsayılan 20
	for i := 0; i < MaksEk-2; i++ {
		if _, err := olustur(t, db, 1, fmt.Sprintf("u%d", i), fmt.Sprintf("web/u%d", i)); err != nil {
			t.Fatalf("u%d: %v", i, err)
		}
	}
	if _, err := olustur(t, db, 1, "fazla", "web/fazla"); err == nil || !strings.Contains(err.Error(), "en çok 20") {
		t.Fatalf("varsayılan üst sınır: %v", err)
	}
}

func TestOlustur_EszamanliYaris(t *testing.T) {
	db := fdb(t)
	// Aynı ad: tam 1 başarı, kalanı 409.
	var ok, cakisma, diger int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := olustur(t, db, 1, "ayni", "web/ayni")
			switch {
			case err == nil:
				atomic.AddInt32(&ok, 1)
			case err.(*Hata).HTTP == 409:
				atomic.AddInt32(&cakisma, 1)
			default:
				atomic.AddInt32(&diger, 1)
			}
		}()
	}
	wg.Wait()
	if ok != 1 || cakisma != 11 || diger != 0 {
		t.Fatalf("aynı ad yarışı: ok=%d cakisma=%d diger=%d", ok, cakisma, diger)
	}
	// Farklı adlar, limit 5 (ana + 4... zaten 1 ek var): toplam ek TAM 5 olmalı.
	LimitOku = func(context.Context, *sql.DB, int64) int { return 6 }
	var basarili int32
	for i := 0; i < 15; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := olustur(t, db, 1, fmt.Sprintf("p%d", i), fmt.Sprintf("web/p%d", i)); err == nil {
				atomic.AddInt32(&basarili, 1)
			}
		}(i)
	}
	wg.Wait()
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM ftp_accounts WHERE domain_id=1 AND tur='ek'`).Scan(&n)
	if n != 5 || basarili != 4 {
		t.Fatalf("limit yarışı: toplam ek=%d (5 beklenir), yeni başarılı=%d (4 beklenir)", n, basarili)
	}
}

func TestIDOR_BaskaHostingHesabi(t *testing.T) {
	db := fdb(t)
	if _, err := olustur(t, db, 2, "kurban", "public_html"); err != nil {
		t.Fatal(err)
	}
	kurban := "c_baska_com-kurban"
	ctx := context.Background()
	d := "web"
	e := false
	if _, err := Guncelle(ctx, db, GuncelleGirdi{DomainID: 1, Kullanici: kurban, Dizin: &d, DizinOlustur: true}); err == nil || err.(*Hata).HTTP != 404 {
		t.Fatalf("güncelle IDOR: %v", err)
	}
	if _, err := Guncelle(ctx, db, GuncelleGirdi{DomainID: 1, Kullanici: kurban, Etkin: &e}); err == nil || err.(*Hata).HTTP != 404 {
		t.Fatalf("etkin IDOR: %v", err)
	}
	if err := ParolaDegistir(ctx, db, 1, kurban, "Saldirgan-Parola-12"); err == nil || err.(*Hata).HTTP != 404 {
		t.Fatalf("parola IDOR: %v", err)
	}
	if err := Sil(ctx, db, 1, kurban); err == nil || err.(*Hata).HTTP != 404 {
		t.Fatalf("sil IDOR: %v", err)
	}
	var h, dir, durum string
	_ = db.QueryRow(`SELECT password_md5, home_dir, status FROM ftp_accounts WHERE username=?`, kurban).Scan(&h, &dir, &durum)
	if !hesaplar.FTPParolaDogrula(pw, h) || durum != "active" || !strings.HasSuffix(dir, "public_html") {
		t.Fatal("kurban hesap değişmiş")
	}
	// Ana hesap bu uçlarla ASLA değiştirilemez/silinemez (tur='ek' kapısı).
	if err := ParolaDegistir(ctx, db, 1, testSK, "Saldirgan-Parola-12"); err == nil || err.(*Hata).HTTP != 404 {
		t.Fatalf("ana hesap parolası ek ucuyla değişti: %v", err)
	}
	if err := Sil(ctx, db, 1, testSK); err == nil || err.(*Hata).HTTP != 404 {
		t.Fatalf("ana hesap ek ucuyla silindi: %v", err)
	}
	if _, err := Guncelle(ctx, db, GuncelleGirdi{DomainID: 1, Kullanici: testSK, Etkin: &e}); err == nil {
		t.Fatal("ana hesap ek ucuyla askıya alındı")
	}
	// Büyük/küçük harf oyunu (BINARY eşleşme).
	if err := Sil(ctx, db, 2, strings.ToUpper(kurban)); err == nil {
		t.Fatal("büyük harfli ad eşleşti")
	}
}

func TestAski_EtkinVeHostAskisi(t *testing.T) {
	db := fdb(t)
	ctx := context.Background()
	k := testSK + "-a"
	if _, err := olustur(t, db, 1, "a", "web"); err != nil {
		t.Fatal(err)
	}
	dur := func() (string, int) {
		var s string
		var e int
		_ = db.QueryRow(`SELECT status, ek_etkin FROM ftp_accounts WHERE username=?`, k).Scan(&s, &e)
		return s, e
	}
	f := false
	if h, err := Guncelle(ctx, db, GuncelleGirdi{DomainID: 1, Kullanici: k, Etkin: &f}); err != nil || h.Durum != "askida" || h.Etkin {
		t.Fatalf("kapat: %+v %v", h, err)
	}
	if s, e := dur(); s != "suspended" || e != 0 {
		t.Fatalf("kapalı hesap pure-ftpd'ye açık: %s %d", s, e)
	}
	// Hosting askıdayken hesap AÇILAMAZ: ek_etkin=1 yazılır ama status suspended kalır.
	_, _ = db.Exec(`UPDATE domains SET askida=1 WHERE id=1`) // yalnız domains.askida (ana hesap hâlâ active)
	tr := true
	h, err := Guncelle(ctx, db, GuncelleGirdi{DomainID: 1, Kullanici: k, Etkin: &tr})
	if err != nil || h.Durum != "askida" {
		t.Fatalf("askıdaki hostingte hesap açıldı: %+v %v", h, err)
	}
	if s, e := dur(); s != "suspended" || e != 1 {
		t.Fatalf("askıdaki hosting: status=%s ek_etkin=%d", s, e)
	}
	// Yalnız ana hesap askıdaysa (domains.askida=0) da ek hesap açılamaz.
	_, _ = db.Exec(`UPDATE domains SET askida=0 WHERE id=1`)
	_, _ = db.Exec(`UPDATE ftp_accounts SET status='suspended' WHERE username=?`, testSK)
	if h, err = Guncelle(ctx, db, GuncelleGirdi{DomainID: 1, Kullanici: k, Etkin: &tr}); err != nil || h.Durum != "askida" {
		t.Fatalf("ana hesap askıdayken ek hesap açıldı: %+v %v", h, err)
	}
	_, _ = db.Exec(`UPDATE ftp_accounts SET status='active' WHERE username=?`, testSK)
	// Askı kalkınca (hesapaski ftpDurum mantığı) hesap müşterinin tercihine döner: burada
	// ek_etkin=1 → active. Listede host_askida bayrağı.
	_, _ = db.Exec(`UPDATE domains SET askida=0 WHERE id=1`)
	l, err := Listele(ctx, db, 1)
	if err != nil || l.HostAskida || len(l.Hesaplar) != 1 {
		t.Fatalf("liste: %+v %v", l, err)
	}
}

func TestParola_YalnizHashVeListeSizdirmaz(t *testing.T) {
	db := fdb(t)
	ctx := context.Background()
	k := testSK + "-a"
	if _, err := olustur(t, db, 1, "a", "web"); err != nil {
		t.Fatal(err)
	}
	yeni := "Yepyeni-Parola-2026!"
	if err := ParolaDegistir(ctx, db, 1, k, yeni); err != nil {
		t.Fatal(err)
	}
	var h string
	_ = db.QueryRow(`SELECT password_md5 FROM ftp_accounts WHERE username=?`, k).Scan(&h)
	if !hesaplar.FTPParolaDogrula(yeni, h) || hesaplar.FTPParolaDogrula(pw, h) || strings.Contains(h, yeni) {
		t.Fatalf("parola değişimi/biçimi: %q", h)
	}
	l, err := Listele(ctx, db, 1)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(l)
	if strings.Contains(string(b), "$6$") || strings.Contains(string(b), yeni) || strings.Contains(strings.ToLower(string(b)), "password") {
		t.Fatalf("liste yanıtı parola/hash sızdırıyor: %s", b)
	}
}

func TestSil_VeAlanAdiSilinincePurge(t *testing.T) {
	db := fdb(t)
	ctx := context.Background()
	if _, err := olustur(t, db, 1, "a", "web/a"); err != nil {
		t.Fatal(err)
	}
	if _, err := olustur(t, db, 1, "b", "web/b"); err != nil {
		t.Fatal(err)
	}
	if err := Sil(ctx, db, 1, testSK+"-a"); err != nil {
		t.Fatal(err)
	}
	// Klasör ve içeriğe dokunulmadı.
	if _, err := os.Stat(filepath.Join(HomeKok, testSK, "web", "a")); err != nil {
		t.Fatalf("silme klasörü kaldırdı: %v", err)
	}
	if err := Sil(ctx, db, 1, testSK+"-a"); err == nil {
		t.Fatal("olmayan hesap silindi demek")
	}
	// Alan adı silinince (FK CASCADE) ek hesaplar da gider; başka hosting etkilenmez.
	if _, err := db.Exec(`DELETE FROM domains WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM ftp_accounts WHERE domain_id=1`).Scan(&n)
	if n != 0 {
		t.Fatalf("alan adı silinince ftp satırları kaldı: %d", n)
	}
	_ = db.QueryRow(`SELECT COUNT(*) FROM ftp_accounts WHERE domain_id=2`).Scan(&n)
	if n != 1 {
		t.Fatalf("başka hostingin satırı etkilendi: %d", n)
	}
}

func TestGuncelle_DizinDegistirVeKacis(t *testing.T) {
	db := fdb(t)
	ctx := context.Background()
	k := testSK + "-a"
	if _, err := olustur(t, db, 1, "a", "web/a"); err != nil {
		t.Fatal(err)
	}
	yeni := "/public_html/foto"
	h, err := Guncelle(ctx, db, GuncelleGirdi{DomainID: 1, Kullanici: k, Dizin: &yeni})
	if err != nil || h.Dizin != "/public_html/foto" {
		t.Fatalf("dizin değiştir: %+v %v", h, err)
	}
	for _, kotu := range []string{"../c_baska_com", "/etc", ".ssh", "/", ""} {
		d := kotu
		if _, err := Guncelle(ctx, db, GuncelleGirdi{DomainID: 1, Kullanici: k, Dizin: &d, DizinOlustur: true}); err == nil {
			t.Fatalf("dizin %q kabul edildi", kotu)
		}
	}
	var dir string
	_ = db.QueryRow(`SELECT home_dir FROM ftp_accounts WHERE username=?`, k).Scan(&dir)
	if dir != filepath.Join(HomeKok, testSK, "public_html", "foto") {
		t.Fatalf("reddedilen güncellemeler dizini bozdu: %s", dir)
	}
	if _, err := Guncelle(ctx, db, GuncelleGirdi{DomainID: 1, Kullanici: k}); err == nil {
		t.Fatal("boş güncelleme kabul edildi")
	}
}
