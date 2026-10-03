package hesaplar

// aski_test.go — hesap askısı: tek kaynak (HesapAskida) ve hesabın MariaDB
// kullanıcılarının kilidi (ACCOUNT LOCK / UNLOCK) GERÇEK geçici MariaDB'de.
// mysqlKomut dk_geciciMariaDB ile yalnız geçici sunucuya gider; canlı MariaDB'ye
// dokunulmaz. DSN yoksa ATLANIR.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func TestHesapAskida_TekKaynak(t *testing.T) {
	_, panel, _ := dk_geciciMariaDB(t)
	dk_exec(t, panel, `CREATE TABLE domains (id BIGINT UNSIGNED PRIMARY KEY, sistem_kullanici VARCHAR(64) NOT NULL,
		askida TINYINT NULL)`)
	dk_exec(t, panel, `INSERT INTO domains VALUES (1,'c_haone',0),(2,'c_hatwo',1),(3,'c_hanull',NULL)`)
	ctx := context.Background()
	for sk, want := range map[string]bool{"c_haone": false, "c_hatwo": true, "c_hanull": false, "c_yok": false} {
		got, err := HesapAskida(ctx, panel, sk)
		if err != nil || got != want {
			t.Errorf("%s: askida=%v err=%v, beklenen %v", sk, got, err, want)
		}
	}
	_ = panel.Close()
	if _, err := HesapAskida(ctx, panel, "c_hatwo"); err == nil {
		t.Fatal("durum okunamazsa hata dönmeli (çağıran vhost'a dokunmaz)")
	}
}

// ha_baglan — kullanıcı adı/parolasıyla geçici sunucuya yeni bağlantı.
func ha_baglan(soket, kul, parola string) (*sql.DB, error) {
	c := mysql.NewConfig()
	c.User, c.Passwd, c.Net, c.Addr = kul, parola, "unix", soket
	db, err := sql.Open("mysql", c.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func ha_kilitli(t *testing.T, kok *sql.DB, kul string) bool {
	t.Helper()
	return ha_kilitliHost(t, kok, kul, "localhost")
}

func ha_kilitliHost(t *testing.T, kok *sql.DB, kul, host string) bool {
	t.Helper()
	var v string
	if err := kok.QueryRow("SHOW CREATE USER '" + kul + "'@'" + host + "'").Scan(&v); err != nil {
		t.Fatalf("%s kilit durumu okunamadı: %v", kul, err)
	}
	return strings.HasSuffix(v, " ACCOUNT LOCK")
}

// Askıda hesabın MariaDB kullanıcıları kilitli: yeni bağlantı yok, açık oturum
// düşer; komşu kiracının kullanıcısı etkilenmez; askıdan alınca açılır.
func TestDBKullanicilariKilit_AskidaKilitliAskidanAlincaAcik(t *testing.T) {
	kok, panel, soket := dk_geciciMariaDB(t)
	on := fmt.Sprintf("hak%d", time.Now().UnixNano()%1e9)
	kul, komsu, parola := on+"_db", on+"k_db", "Ha-Parola-12345"
	for _, u := range []string{kul, komsu} {
		dk_exec(t, kok, "CREATE USER '"+u+"'@'localhost' IDENTIFIED BY '"+parola+"'")
		u := u
		t.Cleanup(func() { _, _ = kok.Exec("DROP USER IF EXISTS '" + u + "'@'localhost'") })
	}
	dk_exec(t, panel, `INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (7,?,?,'x'),(7,?,?,'x'),(8,?,?,'x')`,
		on+"_a", kul, on+"_b", kul, on+"k_a", komsu)

	acikDB, err := ha_baglan(soket, kul, parola)
	if err != nil {
		t.Fatalf("askı öncesi bağlantı: %v", err)
	}
	defer acikDB.Close()
	// *sql.Conn TEK oturuma sabitlenir: düşerse yeniden bağlanıp gizlemez.
	acik, err := acikDB.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer acik.Close()

	if err := DBKullanicilariKilit(panel, 7, true); err != nil {
		t.Fatalf("kilitleme: %v", err)
	}
	if !ha_kilitli(t, kok, kul) {
		t.Fatal("askıdaki hesabın DB kullanıcısı ACCOUNT LOCK olmalı")
	}
	if db, err := ha_baglan(soket, kul, parola); err == nil {
		_ = db.Close()
		t.Fatal("askıda yeni bağlantı reddedilmeli")
	} else if !strings.Contains(strings.ToLower(err.Error()), "locked") {
		t.Fatalf("red nedeni hesap kilidi olmalı: %v", err)
	}
	if err := acik.PingContext(context.Background()); err == nil {
		t.Fatal("askı öncesi açılmış oturum düşürülmeli (KILL USER)")
	}
	if ha_kilitli(t, kok, komsu) {
		t.Fatal("başka domainin DB kullanıcısı kilitlenmemeli")
	}
	// İdempotent.
	if err := DBKullanicilariKilit(panel, 7, true); err != nil || !ha_kilitli(t, kok, kul) {
		t.Fatalf("ikinci kilitleme zararsız olmalı: %v", err)
	}

	if err := DBKullanicilariKilit(panel, 7, false); err != nil {
		t.Fatalf("kilit açma: %v", err)
	}
	if ha_kilitli(t, kok, kul) {
		t.Fatal("askıdan alınca kilit kalkmalı")
	}
	db, err := ha_baglan(soket, kul, parola)
	if err != nil {
		t.Fatalf("askıdan alınca aynı parolayla bağlanılabilmeli: %v", err)
	}
	_ = db.Close()
}

// Sistem hesabı ya da başka domaine de kayıtlı kullanıcı ASLA kilitlenmez;
// hata raporlanır (sessiz atlama yok).
func TestDBKullanicilariKilit_PaylasilanVeSistemHesabiDokunulmaz(t *testing.T) {
	kok, panel, _ := dk_geciciMariaDB(t)
	on := fmt.Sprintf("hap%d", time.Now().UnixNano()%1e9)
	ortak := on + "_db"
	dk_exec(t, kok, "CREATE USER '"+ortak+"'@'localhost' IDENTIFIED BY 'Ha-Parola-12345'")
	t.Cleanup(func() { _, _ = kok.Exec("DROP USER IF EXISTS '" + ortak + "'@'localhost'") })
	dk_exec(t, panel, `INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (7,?,?,'x'),(9,?,?,'x'),(7,?,'root','x')`,
		on+"_a", ortak, on+"_b", ortak, on+"_c")
	err := DBKullanicilariKilit(panel, 7, true)
	if err == nil || !strings.Contains(err.Error(), ortak) || !strings.Contains(err.Error(), `"root"`) {
		t.Fatalf("paylaşılan ve sistem hesabı hata olarak raporlanmalı: %v", err)
	}
	if ha_kilitli(t, kok, ortak) {
		t.Fatal("başka domaine de kayıtlı kullanıcı kilitlenmemeli")
	}
	// Askıdan alırken aynı satırlar HATA DEĞİL: onları hiç kilitlemedik; hata
	// askıdan almayı yarım bırakıp hesabı yeniden askıya alırdı (hiç açılamazdı).
	if err := DBKullanicilariKilit(panel, 7, false); err != nil {
		t.Fatalf("askıdan almada kilitlenmemiş paylaşılan/sistem hesabı hata olmamalı: %v", err)
	}
	if ha_kilitli(t, kok, ortak) {
		t.Fatal("paylaşılan kullanıcıya askıdan almada da dokunulmamalı")
	}
}

// 🔴 db_accounts.db_host'a güvenilmez: yedekten dönüş kullanıcıyı arşivdeki
// host'ta ('%', '127.0.0.1') oluşturabilir. Kullanıcının MariaDB'deki TÜM
// hesapları kilitlenir/açılır; kullanıcısı boş satır (geri yüklemenin yeniden
// kaydı) hata üretmez; MariaDB'de hiç olmayan kullanıcı bağlanamaz → hata yok.
func TestDBKullanicilariKilit_TumHostlar(t *testing.T) {
	kok, panel, _ := dk_geciciMariaDB(t)
	on := fmt.Sprintf("hah%d", time.Now().UnixNano()%1e9)
	kul, yok := on+"_db", on+"_yok"
	hostlar := []string{"localhost", "%", "127.0.0.1"}
	for _, h := range hostlar {
		dk_exec(t, kok, "CREATE USER '"+kul+"'@'"+h+"' IDENTIFIED BY 'Ha-Parola-12345'")
		h := h
		t.Cleanup(func() { _, _ = kok.Exec("DROP USER IF EXISTS '" + kul + "'@'" + h + "'") })
	}
	// db_host 'localhost' kayıtlı (varsayılan) — öteki iki host yalnız MariaDB'de.
	dk_exec(t, panel, `INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (7,?,?,'x'),(7,?,'','') ,(7,?,?,'x')`,
		on+"_a", kul, on+"_b", on+"_c", yok)
	if err := DBKullanicilariKilit(panel, 7, true); err != nil {
		t.Fatalf("kilitleme: %v", err)
	}
	for _, h := range hostlar {
		if !ha_kilitliHost(t, kok, kul, h) {
			t.Errorf("'%s'@'%s' kilitlenmeli (kullanıcının TÜM hesapları)", kul, h)
		}
	}
	if err := DBKullanicilariKilit(panel, 7, false); err != nil {
		t.Fatalf("kilit açma: %v", err)
	}
	for _, h := range hostlar {
		if ha_kilitliHost(t, kok, kul, h) {
			t.Errorf("'%s'@'%s' askıdan alınca açılmalı", kul, h)
		}
	}
}
