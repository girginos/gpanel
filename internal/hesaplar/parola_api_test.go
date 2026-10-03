package hesaplar

// parola_api_test.go — API parola çekirdeği GERÇEK geçici MariaDB'de
// (dk_geciciMariaDB: yalnız geçici sunucu; DSN yoksa ATLANIR).
//   - DB parolası kullanıcının TÜM host tanımlarında değişir, askı kilidi korunur.
//   - Bir host'u bile kurala uymayan hesapta HİÇBİR host değişmez (yarım değişim yok).
//   - FTP parolası yalnız o domainin satırına yazılır, askı (status) korunur.

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"girginospanel/internal/gizli"
)

func pa_auth(t *testing.T, kok *sql.DB, kul, host string) string {
	t.Helper()
	var s string
	if err := kok.QueryRow("SELECT authentication_string FROM mysql.user WHERE User=? AND Host=?", kul, host).Scan(&s); err != nil {
		t.Fatalf("%s@%s okunamadı: %v", kul, host, err)
	}
	return s
}

func pa_hash(t *testing.T, kok *sql.DB, pw string) string {
	t.Helper()
	var s string
	if err := kok.QueryRow("SELECT PASSWORD(?)", pw).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func pa_kullanici(t *testing.T, kok *sql.DB, kul string, hostlar ...string) {
	t.Helper()
	for _, h := range hostlar {
		dk_exec(t, kok, "CREATE USER '"+kul+"'@'"+h+"' IDENTIFIED BY 'Eski-Parola-12345'")
		h := h
		t.Cleanup(func() { _, _ = kok.Exec("DROP USER IF EXISTS '" + kul + "'@'" + h + "'") })
	}
}

func TestDBKullaniciParolaDegistir_TumHostlarVeAskiKilidi(t *testing.T) {
	kok, panel, soket := dk_geciciMariaDB(t)
	on := fmt.Sprintf("pah%d", time.Now().UnixNano()%1e9)
	kul, dbAd := on+"_db", on+"_a"
	hostlar := []string{"localhost", "%", "127.0.0.1"}
	pa_kullanici(t, kok, kul, hostlar...)
	dk_exec(t, kok, "CREATE DATABASE "+dbAd)
	t.Cleanup(func() { _, _ = kok.Exec("DROP DATABASE IF EXISTS " + dbAd) })
	for _, h := range hostlar {
		dk_exec(t, kok, "GRANT ALL ON `"+dbAd+"`.* TO '"+kul+"'@'"+h+"'")
	}
	dk_exec(t, panel, `INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (7,?,?,'eski')`, dbAd, kul)
	// Askı: tüm host'lar kilitli.
	if err := DBKullanicilariKilit(panel, 7, true); err != nil {
		t.Fatalf("kilit: %v", err)
	}
	const yeni = "Yeni-Parola-98765"
	if err := DBKullaniciParolaDegistir(panel, 7, kul, yeni); err != nil {
		t.Fatalf("parola değişimi: %v", err)
	}
	beklenen := pa_hash(t, kok, yeni)
	for _, h := range hostlar {
		if got := pa_auth(t, kok, kul, h); got != beklenen {
			t.Errorf("'%s'@'%s' parolası değişmeli (TÜM host'lar)", kul, h)
		}
		if !ha_kilitliHost(t, kok, kul, h) {
			t.Errorf("'%s'@'%s' askı kilidi parola değişiminden sonra SÜRMELİ", kul, h)
		}
	}
	var sakli string
	if err := panel.QueryRow(`SELECT db_pass_plain FROM db_accounts WHERE db_name=?`, dbAd).Scan(&sakli); err != nil {
		t.Fatal(err)
	}
	if gizli.CozBagli(sakli, kul) != yeni {
		t.Fatal("db_pass_plain yeni parolayı (kullanıcıya bağlı) tutmalı")
	}
	// Askıdan alınınca yeni parolayla bağlanılır.
	if err := DBKullanicilariKilit(panel, 7, false); err != nil {
		t.Fatal(err)
	}
	db, err := ha_baglan(soket, kul, yeni)
	if err != nil {
		t.Fatalf("yeni parolayla bağlantı: %v", err)
	}
	_ = db.Close()
}

// Bir host hesabının bile başka domainin veritabanında yetkisi varsa reddedilir ve
// HİÇBİR host'un parolası değişmez.
func TestDBKullaniciParolaDegistir_YarimDegisimYok(t *testing.T) {
	kok, panel, _ := dk_geciciMariaDB(t)
	on := fmt.Sprintf("pay%d", time.Now().UnixNano()%1e9)
	kul, bizim, komsu := on+"_db", on+"_a", on+"k_a"
	pa_kullanici(t, kok, kul, "localhost", "%")
	for _, d := range []string{bizim, komsu} {
		dk_exec(t, kok, "CREATE DATABASE "+d)
		d := d
		t.Cleanup(func() { _, _ = kok.Exec("DROP DATABASE IF EXISTS " + d) })
	}
	dk_exec(t, kok, "GRANT ALL ON `"+bizim+"`.* TO '"+kul+"'@'localhost'")
	dk_exec(t, kok, "GRANT ALL ON `"+komsu+"`.* TO '"+kul+"'@'%'") // komşunun DB'si
	dk_exec(t, panel, `INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (7,?,?,'x'),(8,?,'baskasi','x')`,
		bizim, kul, komsu)
	once := pa_auth(t, kok, kul, "localhost")
	if err := DBKullaniciParolaDegistir(panel, 7, kul, "Saldirgan-Parola-1"); err == nil {
		t.Fatal("başka domainin DB'sinde yetkili host hesabı reddedilmeli")
	}
	if pa_auth(t, kok, kul, "localhost") != once || pa_auth(t, kok, kul, "%") != once {
		t.Fatal("reddedilen değişimde HİÇBİR host'un parolası değişmemeli")
	}
}

func TestDBKullaniciParolaDegistir_KapsamDisi(t *testing.T) {
	kok, panel, _ := dk_geciciMariaDB(t)
	on := fmt.Sprintf("pak%d", time.Now().UnixNano()%1e9)
	komsuKul := on + "k_db"
	pa_kullanici(t, kok, komsuKul, "localhost")
	dk_exec(t, panel, `INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (8,?,?,'x'),(7,?,'root','x')`,
		on+"k_a", komsuKul, on+"_r")
	once := pa_auth(t, kok, komsuKul, "localhost")
	cases := []struct {
		ad, kul string
		hata    error
	}{
		{"başka domainin kullanıcısı", komsuKul, ErrDBKullaniciYok},
		{"kayıtsız ad", on + "_yok", ErrDBKullaniciYok},
		{"sistem hesabı (kayıtlı olsa bile)", "root", ErrSistemDBHesabi},
		{"geçersiz ad", "a'b", ErrDBKullaniciYok},
	}
	for _, c := range cases {
		if err := DBKullaniciParolaDegistir(panel, 7, c.kul, "Saldirgan-Parola-1"); !errors.Is(err, c.hata) {
			t.Errorf("%s: %v (beklenen %v)", c.ad, err, c.hata)
		}
	}
	if pa_auth(t, kok, komsuKul, "localhost") != once {
		t.Fatal("komşunun parolası değişmemeli")
	}
}

func TestFTPHesapParolaDegistir_DomainKapsamiVeAski(t *testing.T) {
	_, panel, _ := dk_geciciMariaDB(t)
	dk_exec(t, panel, `CREATE TABLE ftp_accounts (id BIGINT AUTO_INCREMENT PRIMARY KEY, domain_id BIGINT NOT NULL,
		username VARCHAR(64) NOT NULL UNIQUE, password_md5 VARCHAR(255) NOT NULL, status VARCHAR(16) NOT NULL DEFAULT 'active')`)
	eski := FTPParolaHash("Eski-Parola-12345")
	dk_exec(t, panel, `INSERT INTO ftp_accounts (domain_id, username, password_md5, status) VALUES (7,'c_bizim',?,'suspended'),(8,'c_komsu',?,'active')`, eski, eski)
	if err := FTPHesapParolaDegistir(panel, 7, "c_bizim", "Yeni-Parola-98765"); err != nil {
		t.Fatal(err)
	}
	var h, durum string
	_ = panel.QueryRow(`SELECT password_md5, status FROM ftp_accounts WHERE username='c_bizim'`).Scan(&h, &durum)
	if !FTPParolaDogrula("Yeni-Parola-98765", h) || durum != "suspended" {
		t.Fatalf("parola değişmeli ($6$), askı korunmalı: dogru=%v durum=%s", FTPParolaDogrula("Yeni-Parola-98765", h), durum)
	}
	for _, c := range []struct {
		ad  string
		dom int64
		kul string
	}{
		{"başka domainin hesabı", 7, "c_komsu"},
		{"harf varyantı", 7, "C_BIZIM"},
		{"yok", 7, "c_yok"},
	} {
		if err := FTPHesapParolaDegistir(panel, c.dom, c.kul, "Saldirgan-Parola-1"); !errors.Is(err, ErrFTPHesabiYok) {
			t.Errorf("%s: %v (beklenen ErrFTPHesabiYok)", c.ad, err)
		}
	}
	_ = panel.QueryRow(`SELECT password_md5 FROM ftp_accounts WHERE username='c_komsu'`).Scan(&h)
	if h != eski {
		t.Fatal("komşunun FTP parolası değişmemeli")
	}
}
