package toplu

// devir_test.go — sahiplik devri (devret) DB davranışı:
//
//   - FTP-DEVIR-DUZMETIN: yeni FTP parolası ftp_accounts.password_md5'e $6$ crypt
//     hash'i olarak yazılır ve Pure-FTPd'nin yaptığı gibi libc crypt(3) ile
//     doğrulanır (düz metin → FTP 530).
//   - BLD-1: bildirim devri / işlem hatası devri DÜŞÜRÜR; InnoDB kilitlenmesinde
//     (1213) işlemin TAMAMI yeniden denenir, geri alınmış devir asla "başarılı"
//     raporlanmaz.
//
// Gerçek MariaDB gerekir (REPEATABLE-READ, InnoDB); DB yoksa testler ATLANIR:
//
//	GOSP_TEST_GECICI_DSN='root@unix(/tmp/gecici-db/sock)/' go test ./internal/toplu/
//
// Test YALNIZ kendi açtığı gosp_test_devir_<zaman> veritabanını kullanır ve siler.
// devret'in DB dışı adımları (chpasswd, passwd -l, crontab) PATH'in başına konan
// SAHTE araçlarla koşar — sistem hesaplarına/crontab'a dokunulmaz.

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/auth"
	"girginospanel/internal/bildirim"
	"girginospanel/internal/efektiflimit"
	"girginospanel/internal/hesaplar"
	"girginospanel/internal/middleware"
)

const (
	dt_sk         = "c_d4testyok" // sistemde OLMAYAN kiracı kullanıcısı
	dt_did        = int64(30)
	dt_eskiParola = "EskiParola-1234567"
	dt_goc0105    = "../../migrations/0105_bildirim_okuyan.sql"
)

var dt_sir = []byte("toplu-devir-test-anahtari-0123456789abcdef")

func dt_exec(t *testing.T, db *sql.DB, q string, args ...any) sql.Result {
	t.Helper()
	res, err := db.Exec(q, args...)
	if err != nil {
		t.Fatalf("SQL hatası: %v\n%s", err, q)
	}
	return res
}

func dt_sayi(t *testing.T, db *sql.DB, q string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("sayım: %v (%s)", err, q)
	}
	return n
}

// dt_geciciDB — geçici veritabanı + devret'in dokunduğu tabloların şeması
// (göçlerdeki sütun/anahtar/yabancı anahtar düzeniyle) + 0105 göçü.
func dt_geciciDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li test atlandı")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("DSN çözülemedi: %v", err)
	}
	cfg.DBName = ""
	kok, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatalf("db açılamadı: %v", err)
	}
	ad := fmt.Sprintf("gosp_test_devir_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		_ = kok.Close()
		t.Fatalf("geçici veritabanı oluşturulamadı: %v", err)
	}
	t.Cleanup(func() {
		_, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad)
		_ = kok.Close()
	})
	cfg.DBName = ad
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatalf("db açılamadı: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(16)

	for _, q := range []string{
		`CREATE TABLE users (
			id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
			role VARCHAR(16) NOT NULL DEFAULT 'reseller',
			status VARCHAR(16) NOT NULL DEFAULT 'active',
			max_domain INT NULL, max_disk_mb INT NULL
		) ENGINE=InnoDB`,
		// alan_adi + plan_id: devir planı yeni sahibin kapsamına alır (GL-03,
		// plans.DevirPlanTasi) — canlı şemadaki gibi.
		`CREATE TABLE domains (
			id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
			alan_adi VARCHAR(253) NOT NULL DEFAULT '',
			reseller_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
			customer_id BIGINT UNSIGNED NULL,
			boyut_kb BIGINT NULL,
			plan_id BIGINT UNSIGNED NULL,
			disk_kota_mb INT NOT NULL DEFAULT 0, inode_kota INT NOT NULL DEFAULT 0,
			waf_enabled TINYINT NULL, waf_mode VARCHAR(10) NULL, waf_paranoia TINYINT NULL,
			KEY ix_domains_plan (plan_id)
		) ENGINE=InnoDB`,
		dt_servicePlansDDL,
		efektiflimit.TabloDDL, // 0120: devir farkları hostinge özel üst yazım olur
		// 0003 + 0089 (token_gecersiz_ts) + 0104 (password_md5 VARCHAR(255))
		`CREATE TABLE ftp_accounts (
			id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
			domain_id BIGINT UNSIGNED NOT NULL,
			username VARCHAR(64) NOT NULL UNIQUE,
			password_md5 VARCHAR(255) NOT NULL,
			home_dir VARCHAR(255) NOT NULL,
			uid_n INT NOT NULL, gid_n INT NOT NULL,
			status ENUM('active','suspended') DEFAULT 'active',
			token_gecersiz_ts BIGINT NOT NULL DEFAULT 0,
			KEY ix_ftp_domain (domain_id),
			CONSTRAINT fk_ftp_domain FOREIGN KEY (domain_id) REFERENCES domains(id) ON DELETE CASCADE
		) ENGINE=InnoDB`,
		`CREATE TABLE git_repos (
			id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
			domain_id BIGINT UNSIGNED NOT NULL,
			webhook_secret VARCHAR(255) NOT NULL,
			webhook_secret_hash VARCHAR(64) NOT NULL DEFAULT '',
			KEY ix_git_domain (domain_id),
			CONSTRAINT fk_git_domain FOREIGN KEY (domain_id) REFERENCES domains(id) ON DELETE CASCADE
		) ENGINE=InnoDB`,
		`CREATE TABLE backup_destinations (
			id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
			domain_id BIGINT UNSIGNED NOT NULL,
			parola VARCHAR(255) NOT NULL,
			aktif TINYINT NOT NULL DEFAULT 1,
			UNIQUE KEY uk_domain (domain_id),
			CONSTRAINT fk_bdest_domain FOREIGN KEY (domain_id) REFERENCES domains (id) ON DELETE CASCADE
		) ENGINE=InnoDB`,
		`CREATE TABLE github_connections (
			id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
			domain_id BIGINT UNSIGNED NOT NULL,
			UNIQUE KEY uk_domain (domain_id),
			CONSTRAINT fk_gh_domain FOREIGN KEY (domain_id) REFERENCES domains (id) ON DELETE CASCADE
		) ENGINE=InnoDB`,
		// 0080 + 0082
		`CREATE TABLE cp_bildirim (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
			seviye VARCHAR(16) NOT NULL DEFAULT 'bilgi',
			kategori VARCHAR(32) NOT NULL DEFAULT '',
			baslik VARCHAR(200) NOT NULL,
			mesaj TEXT NOT NULL,
			domain_id BIGINT UNSIGNED NULL,
			reseller_id BIGINT UNSIGNED NULL,
			ref_tur VARCHAR(24) NOT NULL DEFAULT '',
			ref_id BIGINT NOT NULL DEFAULT 0,
			okundu TINYINT(1) NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			KEY idx_bildirim_okundu (okundu),
			KEY idx_bildirim_kategori (kategori),
			KEY idx_bildirim_domain (domain_id),
			KEY idx_bildirim_created (created_at),
			KEY idx_bildirim_reseller (reseller_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
	} {
		dt_exec(t, db, q)
	}
	dt_gocUygula(t, db, dt_goc0105)
	return db
}

// dt_gocUygula — cmd/server runMigrations ile aynı ayrıştırma.
func dt_gocUygula(t *testing.T, db *sql.DB, yol string) {
	t.Helper()
	body, err := os.ReadFile(yol)
	if err != nil {
		t.Fatalf("göç dosyası okunamadı (%s): %v", yol, err)
	}
	var temiz []string
	for _, satir := range strings.Split(string(body), "\n") {
		s := strings.TrimSpace(satir)
		if s == "" || strings.HasPrefix(s, "--") {
			continue
		}
		temiz = append(temiz, satir)
	}
	for _, ifade := range strings.Split(strings.Join(temiz, "\n"), ";") {
		if s := strings.TrimSpace(ifade); s != "" {
			dt_exec(t, db, s)
		}
	}
}

// dt_sahteAraclar — chpasswd / passwd / crontab'ı PATH'te gölgeler. chpasswd
// argümanlarını ve stdin'ini kayda yazar (SSH eşitlemesine giden değeri görmek için).
func dt_sahteAraclar(t *testing.T) string {
	t.Helper()
	if _, err := user.Lookup(dt_sk); err == nil {
		t.Fatalf("güvenlik: test kullanıcısı %s sistemde VAR — test koşmaz", dt_sk)
	}
	dizin := t.TempDir()
	kayit := filepath.Join(dizin, "chpasswd.kayit")
	araclar := map[string]string{
		"chpasswd": "#!/bin/sh\n{ echo \"ARGS:$*\"; cat; echo; } >> \"$DT_CHPASSWD_KAYIT\"\nexit 0\n",
		"passwd":   "#!/bin/sh\nexit 0\n",
		"crontab":  "#!/bin/sh\necho \"no crontab for $2\" >&2\nexit 1\n",
	}
	for ad, govde := range araclar {
		if err := os.WriteFile(filepath.Join(dizin, ad), []byte(govde), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("DT_CHPASSWD_KAYIT", kayit)
	t.Setenv("PATH", dizin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for ad := range araclar {
		if yol, err := exec.LookPath(ad); err != nil || filepath.Dir(yol) != dizin {
			t.Fatalf("güvenlik: %s sahte araca çözülmedi (%s, %v)", ad, yol, err)
		}
	}
	return kayit
}

// dt_libcCrypt — Pure-FTPd `MYSQLCrypt crypt` doğrulamasının aynısı:
// crypt(girilen, saklanan) == saklanan (glibc/libxcrypt crypt(3), perl üzerinden;
// parola argv'de değil ortamda).
func dt_libcCrypt(t *testing.T, girilen, saklanan string) bool {
	t.Helper()
	if _, err := exec.LookPath("perl"); err != nil {
		t.Skip("perl yok — libc crypt(3) doğrulaması yapılamıyor")
	}
	cmd := exec.Command("perl", "-e", `print crypt($ENV{DT_P}, $ENV{DT_H}) // ""`)
	cmd.Env = append(os.Environ(), "DT_P="+girilen, "DT_H="+saklanan)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("perl crypt: %v", err)
	}
	return string(out) == saklanan
}

// dt_hazirla — bayi 7 ve 8 (aktif), bayi 7'nin domaini 30 ve FTP hesabı ($6$).
func dt_hazirla(t *testing.T, db *sql.DB) string {
	t.Helper()
	dt_exec(t, db, `INSERT INTO users (id, role, status) VALUES (7,'reseller','active'), (8,'reseller','active')`)
	dt_exec(t, db, `INSERT INTO domains (id, reseller_id) VALUES (?, 7)`, dt_did)
	eski := hesaplar.FTPParolaHash(dt_eskiParola)
	if !hesaplar.IsFTPHash(eski) {
		t.Fatalf("openssl ile hash üretilemedi")
	}
	dt_exec(t, db, `INSERT INTO ftp_accounts (domain_id, username, password_md5, home_dir, uid_n, gid_n) VALUES (?,?,?,?,1000,1000)`,
		dt_did, dt_sk, eski, "/home/"+dt_sk)
	return eski
}

func dt_bildirim(t *testing.T, db *sql.DB, dom, rid int64) int64 {
	t.Helper()
	res := dt_exec(t, db, `INSERT INTO cp_bildirim (seviye,kategori,baslik,mesaj,domain_id,reseller_id) VALUES ('kritik','yedek','b','m',?,?)`, dom, rid)
	id, _ := res.LastInsertId()
	return id
}

func dt_sahip(t *testing.T, db *sql.DB) (rid int64, parola string, token int64) {
	t.Helper()
	if err := db.QueryRow(`SELECT d.reseller_id, f.password_md5, f.token_gecersiz_ts FROM domains d JOIN ftp_accounts f ON f.domain_id=d.id WHERE d.id=?`, dt_did).
		Scan(&rid, &parola, &token); err != nil {
		t.Fatal(err)
	}
	return
}

// dt_baglantiID — tx'in sunucu bağlantı kimliği.
func dt_baglantiID(t *testing.T, tx *sql.Tx) uint64 {
	t.Helper()
	var id uint64
	if err := tx.QueryRow(`SELECT CONNECTION_ID()`).Scan(&id); err != nil {
		t.Fatalf("bağlantı kimliği okunamadı: %v", err)
	}
	return id
}

// dt_logYakala — standart günlüğü (bildirim.KilitDenemeli'nin yeniden deneme
// satırları dahil) test boyunca tampona yönlendirir.
func dt_logYakala(t *testing.T) *bytes.Buffer {
	t.Helper()
	var b bytes.Buffer
	eski := log.Writer()
	log.SetOutput(&b)
	t.Cleanup(func() { log.SetOutput(eski) })
	return &b
}

// 🔴 FTP-DEVIR-DUZMETIN: devirde yazılan parola $6$ crypt olmalı ve Pure-FTPd'nin
// crypt(girilen, saklanan) karşılaştırmasından geçmeli. Eski kod düz metin
// yazıyordu → yeni sahip kendisine gösterilen parolayla FTP'de 530 alıyordu.
func TestDevir_FTPParolasiCryptHashYazilir(t *testing.T) {
	db := dt_geciciDB(t)
	kayit := dt_sahteAraclar(t)
	eski := dt_hazirla(t, db)
	if dt_libcCrypt(t, dt_eskiParola, dt_eskiParola) {
		t.Fatalf("crypt yardımcısı ayırt edici değil (düz metni kabul etti) — test geçersiz")
	}
	if !dt_libcCrypt(t, dt_eskiParola, eski) {
		t.Fatalf("crypt yardımcısı openssl $6$ hash'ini doğrulayamadı — test geçersiz")
	}

	h := &Handlers{DB: db}
	hedef := int64(8)
	ds, err := h.devret(context.Background(), dt_did, "ornek.test", dt_sk, &hedef, nil, 1, "root")
	if err != nil {
		t.Fatalf("devret: %v", err)
	}
	if ds.Kritik || ds.YeniFTPParola == "" {
		t.Fatalf("devir sonucu beklenmedik: %+v", ds)
	}
	rid, saklanan, token := dt_sahip(t, db)
	if rid != 8 || token == 0 {
		t.Fatalf("sahiplik/oturum düşürme yazılmadı: rid=%d token=%d", rid, token)
	}
	if !hesaplar.IsFTPHash(saklanan) || saklanan == ds.YeniFTPParola {
		t.Fatalf("FTP-DEVIR-DUZMETIN: password_md5 $6$ crypt değil (düz metin?) — başı=%.3q", saklanan)
	}
	if !dt_libcCrypt(t, ds.YeniFTPParola, saklanan) {
		t.Fatalf("FTP-DEVIR-DUZMETIN: Pure-FTPd crypt(yeni parola, saklanan) eşleşmedi → FTP 530")
	}
	if !hesaplar.FTPParolaDogrula(ds.YeniFTPParola, saklanan) {
		t.Fatalf("panel girişi (FTPParolaDogrula) yeni parolayı kabul etmedi")
	}
	if hesaplar.FTPParolaDogrula(dt_eskiParola, saklanan) || dt_libcCrypt(t, dt_eskiParola, saklanan) {
		t.Fatalf("ESKİ parola devirden sonra hâlâ geçiyor")
	}
	// SSH eşitlemesi aynı hash'i chpasswd -e ile almalı (düz metin chpasswd değil).
	k, err := os.ReadFile(kayit)
	if err != nil {
		t.Fatalf("chpasswd çağrılmadı: %v", err)
	}
	if !strings.Contains(string(k), "ARGS:-e\n") || !strings.Contains(string(k), dt_sk+":"+saklanan) {
		t.Fatalf("SSH parolası hash ile (chpasswd -e) eşitlenmedi: %q", string(k))
	}
}

// 🔴 BLD-1: bildirim devri başarısızsa devir BAŞARISIZ raporlanmalı ve hiçbir DB
// değişikliği kalıcı olmamalı. Eskiden hata uyarıya çevrilip Commit ediliyordu.
func TestDevir_BildirimDevriHatasiDevriDusurur(t *testing.T) {
	db := dt_geciciDB(t)
	kayit := dt_sahteAraclar(t)
	eski := dt_hazirla(t, db)
	dt_bildirim(t, db, dt_did, 7)
	dt_exec(t, db, `DROP TABLE cp_bildirim_okundu`) // bildirim devri hata versin

	h := &Handlers{DB: db}
	hedef := int64(8)
	ds, err := h.devret(context.Background(), dt_did, "ornek.test", dt_sk, &hedef, nil, 1, "root")
	rid, saklanan, token := dt_sahip(t, db)
	if err == nil {
		t.Fatalf("BLD-1: bildirim devri hatası yutuldu, devir BAŞARILI raporlandı (sonuç=%+v, rid=%d)", ds, rid)
	}
	if rid != 7 || saklanan != eski || token != 0 {
		t.Fatalf("başarısız devir DB'de iz bıraktı: rid=%d parola_degisti=%t token=%d", rid, saklanan != eski, token)
	}
	if _, e := os.Stat(kayit); e == nil {
		t.Fatalf("başarısız devirde SSH parolası yine de değiştirildi")
	}
}

// 🔴 BLD-1: gerçek bir InnoDB kilitlenmesinde devir kurban seçilirse işlemin
// TAMAMI yeniden denenmeli; geri alınmış devir "başarılı" raporlanmamalı.
// Kilitlenme belirlenimci kurulur: ağır bir işlem (A) devrin ileride ihtiyaç
// duyacağı bir satırı tutar; devir domains satırını kilitleyip o satırı bekler;
// A domains satırını ister → döngü. InnoDB küçük işlemi (devir) geri alır.
// İki yer denenir: bildirim devri (6. adım) ve "best effort" uzak yedek hedefi
// (4. adım) — kilitlenme hiçbir adımda uyarıya çevrilip yutulmamalı.
func TestDevir_KilitlenmeKurbaniTamamenYenidenDenenir(t *testing.T) {
	for _, tc := range []struct {
		ad     string
		kilitQ string
	}{
		{"bildirim devri", `SELECT id FROM cp_bildirim WHERE domain_id=? FOR UPDATE`},
		{"uzak yedek hedefi (best effort adım)", `SELECT id FROM backup_destinations WHERE domain_id=? FOR UPDATE`},
	} {
		t.Run(tc.ad, func(t *testing.T) { dt_kilitlenmeKurbani(t, tc.kilitQ) })
	}
}

func dt_kilitlenmeKurbani(t *testing.T, kilitQ string) {
	db := dt_geciciDB(t)
	dt_sahteAraclar(t)
	dt_hazirla(t, db)
	r := dt_bildirim(t, db, dt_did, 7)
	dt_exec(t, db, `INSERT INTO backup_destinations (domain_id, parola) VALUES (?, 'eski-sahibin-sifresi')`, dt_did)
	dt_exec(t, db, `CREATE TABLE dt_agir (id INT PRIMARY KEY, v INT NOT NULL) ENGINE=InnoDB`)
	var parca []string
	for i := 1; i <= 3000; i++ {
		parca = append(parca, fmt.Sprintf("(%d,0)", i))
	}
	dt_exec(t, db, `INSERT INTO dt_agir VALUES `+strings.Join(parca, ","))

	ctx := context.Background()
	a, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Rollback() }()
	if _, err := a.ExecContext(ctx, `UPDATE dt_agir SET v = v + 1`); err != nil {
		t.Fatal(err)
	}
	var x int64
	if err := a.QueryRowContext(ctx, kilitQ, dt_did).Scan(&x); err != nil {
		t.Fatal(err)
	}
	aBag := dt_baglantiID(t, a)
	gunluk := dt_logYakala(t)

	h := &Handlers{DB: db}
	hedef := int64(8)
	type sonucT struct {
		ds  *devirSonuc
		err error
	}
	bitti := make(chan sonucT, 1)
	go func() {
		ds, err := h.devret(ctx, dt_did, "ornek.test", dt_sk, &hedef, nil, 1, "root")
		bitti <- sonucT{ds, err}
	}()
	// Devir, A'nın tuttuğu satırı beklemeye girene dek bekle: BU test
	// veritabanında A dışındaki bir bağlantının ifadesi 100 ms'yi aşmış olmalı.
	// (information_schema.PROCESSLIST canlıdır; INNODB_TRX/INNODB_LOCK_WAITS 100 ms'lik
	// önbellekten okunur ve sık sorgulanınca hiç tazelenmez.)
	son := time.Now().Add(10 * time.Second)
	for {
		var n int
		_ = db.QueryRow(`SELECT COUNT(*) FROM information_schema.PROCESSLIST
			WHERE DB = DATABASE() AND ID <> CONNECTION_ID() AND ID <> ?
			  AND COMMAND IN ('Query','Execute') AND TIME_MS > 100`, aBag).Scan(&n)
		if n > 0 {
			break
		}
		select {
		case s := <-bitti:
			t.Fatalf("devir kilidi beklemeden bitti (err=%v) — senaryo kurulamadı", s.err)
		default:
		}
		if time.Now().After(son) {
			t.Fatalf("devir kilit beklemeye girmedi — senaryo kurulamadı")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Döngüyü kapat: A, devrin (ilk ifadesinde) kilitlediği domains satırını
	// istiyor. Bu ifade ancak devrin işlemi geri alınırsa (kurban) ilerleyebilir.
	if _, err := a.ExecContext(ctx, `UPDATE domains SET boyut_kb = boyut_kb WHERE id=?`, dt_did); err != nil {
		t.Fatalf("ağır işlem kurban seçildi (senaryo kurulamadı): %v", err)
	}
	if err := a.Commit(); err != nil {
		t.Fatal(err)
	}
	s := <-bitti
	rid, saklanan, _ := dt_sahip(t, db)
	if g := gunluk.String(); !strings.Contains(g, "kilit çakışması") || !strings.Contains(g, "1213") {
		// Yeniden deneme yoksa (taban) günlük boştur; asıl belirti aşağıdaki rid.
		t.Logf("yeniden deneme günlüğü yok: %q", g)
		if s.err == nil && rid == 8 {
			t.Fatalf("senaryo devri kilitlenme kurbanı yapmadı — test geçersiz")
		}
	}
	if s.err != nil {
		t.Fatalf("BLD-1: kilitlenme kurbanı devir yeniden denenmedi: %v (rid=%d)", s.err, rid)
	}
	if rid != 8 {
		t.Fatalf("BLD-1: devir BAŞARILI raporlandı ama sahiplik geri alınmış: reseller_id=%d (beklenen 8), uyarılar=%v", rid, s.ds.Uyarilar)
	}
	if !hesaplar.FTPParolaDogrula(s.ds.YeniFTPParola, saklanan) {
		t.Fatalf("BLD-1: yöneticiye gösterilen yeni FTP parolası DB'ye yazılmamış")
	}
	if n := dt_sayi(t, db, `SELECT COUNT(*) FROM cp_bildirim WHERE id=? AND reseller_id=8`, r); n != 1 {
		t.Fatalf("açık bildirim yeni sahibe taşınmadı")
	}
	if n := dt_sayi(t, db, `SELECT COUNT(*) FROM backup_destinations WHERE domain_id=? AND aktif=0 AND parola=''`, dt_did); n != 1 {
		t.Fatalf("uzak yedek hedefi pasifleştirilmedi")
	}
}

// adimHatasiYutulabilir: yalnız ifade düzeyi sunucu hatası uyarıya çevrilir;
// kilit çakışması (işlemi geri alan/yarım bırakan) ve sunucu dışı hata yutulmaz.
func TestAdimHatasiYutulabilir(t *testing.T) {
	for _, tc := range []struct {
		ad    string
		err   error
		yutul bool
	}{
		{"1146 tablo yok", &mysql.MySQLError{Number: 1146, Message: "Table doesn't exist"}, true},
		{"1213 kilitlenme", &mysql.MySQLError{Number: 1213, Message: "Deadlock found"}, false},
		{"1213 sarılı", fmt.Errorf("adım: %w", &mysql.MySQLError{Number: 1213}), false},
		{"1205 kilit beklemesi", &mysql.MySQLError{Number: 1205, Message: "Lock wait timeout"}, false},
		{"bağlantı hatası", mysql.ErrInvalidConn, false},
		{"ctx iptal", context.Canceled, false},
	} {
		if got := adimHatasiYutulabilir(tc.err); got != tc.yutul {
			t.Errorf("%s: adimHatasiYutulabilir=%t, beklenen %t", tc.ad, got, tc.yutul)
		}
	}
}

// 🔴 BLD-1 (gerçekçi yarış): devir sürerken eski/yeni bayi, müşteri ve yönetici
// "tümünü okundu" diyor. Hiçbir turda devir sessizce kaybolmamalı (başarılı
// raporlanıp sahiplik/parola yazılmamış), devir ve okundu istekleri hata
// vermemeli. Eski kodda turların önemli kısmı sessiz kayıptı.
func TestDevir_EszamanliOkunduIleSessizKayipYok(t *testing.T) {
	db := dt_geciciDB(t)
	dt_sahteAraclar(t)
	dt_hazirla(t, db)
	for i := 0; i < 40; i++ {
		dt_bildirim(t, db, dt_did, 7)
	}
	h := &Handlers{DB: db}
	bh := &bildirim.Handlers{DB: db}
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(dt_sir))
		r.Post("/bildirimler/{id}/okundu", bh.Okundu)
	})
	tok := func(tur string, id int64) string {
		var s string
		var err error
		switch tur {
		case "bayi":
			s, err = auth.IssueResellerAt(dt_sir, 3600, id, fmt.Sprintf("bayi%d", id), id, 0, "")
		case "admin":
			s, err = auth.IssueAt(dt_sir, 3600, 1, "root", "admin", 0, "")
		default:
			s, _, err = auth.GenerateMusteri(dt_sir, auth.MusteriClaims{FTPHesapID: 1, DomainID: id, Kullanici: dt_sk, AlanAdi: "ornek.test"}, 3600)
		}
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	okuyanlar := []string{tok("bayi", 7), tok("bayi", 8), tok("musteri", dt_did), tok("admin", 1)}
	okundu := func(jeton string) int {
		req := httptest.NewRequest(http.MethodPost, "/bildirimler/0/okundu", nil)
		req.Header.Set("Authorization", "Bearer "+jeton)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	const tur = 80
	gunluk := dt_logYakala(t)
	var sessiz, devirHata, okunduHata int
	var ornek string
	for i := 0; i < tur; i++ {
		eskiSahip, yeniSahip := int64(7), int64(8)
		if i%2 == 1 {
			eskiSahip, yeniSahip = 8, 7
		}
		// Tur başı: tüm bildirimler eski sahipte ve kimse okumamış.
		dt_exec(t, db, `DELETE FROM cp_bildirim_okundu`)
		dt_exec(t, db, `UPDATE cp_bildirim SET okundu=0, reseller_id=?`, eskiSahip)
		dt_exec(t, db, `UPDATE domains SET reseller_id=? WHERE id=?`, eskiSahip, dt_did)

		var wg sync.WaitGroup
		basla := make(chan struct{})
		var ds *devirSonuc
		var derr error
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-basla
			ds, derr = h.devret(context.Background(), dt_did, "ornek.test", dt_sk, &yeniSahip, nil, 1, "root")
		}()
		kodlar := make([]int, len(okuyanlar))
		for j, jeton := range okuyanlar {
			wg.Add(1)
			go func(j int, jeton string) {
				defer wg.Done()
				<-basla
				time.Sleep(time.Duration(rand.IntN(1500)) * time.Microsecond)
				kodlar[j] = okundu(jeton)
			}(j, jeton)
		}
		close(basla)
		wg.Wait()

		for _, k := range kodlar {
			if k != http.StatusOK {
				okunduHata++
			}
		}
		rid, saklanan, _ := dt_sahip(t, db)
		switch {
		case derr != nil:
			devirHata++
			ornek = derr.Error()
		case rid != yeniSahip || !hesaplar.FTPParolaDogrula(ds.YeniFTPParola, saklanan):
			sessiz++
			ornek = fmt.Sprintf("tur %d: devir başarılı raporlandı ama rid=%d (beklenen %d), uyarılar=%v", i, rid, yeniSahip, ds.Uyarilar)
		}
	}
	t.Logf("%d tur: sessiz kayıp=%d, devir hatası=%d, okundu hatası=%d, kilit çakışması yeniden denemesi=%d",
		tur, sessiz, devirHata, okunduHata, strings.Count(gunluk.String(), "kilit çakışması"))
	if sessiz > 0 {
		t.Errorf("BLD-1: %d/%d turda devir SESSİZCE kayboldu — örnek: %s", sessiz, tur, ornek)
	}
	if devirHata > 0 {
		t.Errorf("BLD-1: %d/%d turda devir hata verdi (yeniden deneme yetmedi?) — örnek: %s", devirHata, tur, ornek)
	}
	if okunduHata > 0 {
		t.Errorf("BLD-2: %d okundu isteği 200 dönmedi", okunduHata)
	}
}
