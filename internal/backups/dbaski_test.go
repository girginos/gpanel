package backups

// dbaski_test.go — hesap askısı ve yedekten dönüş: askıdaki hesabın yedeği
// (SHOW CREATE USER … ACCOUNT LOCK) açık hesaba KİLİTLİ kullanıcı getirmez; geri
// yüklemeden sonra kilit hesabın BUGÜNKÜ askısından uygulanır (askı sürerken
// yapılan geri yükleme kilitsiz kullanıcı bırakmaz). Gerçek geçici MariaDB
// (bdo_ortam); yoksa ATLANIR.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func (o *bdoOrtam) kilitli(t *testing.T, kul string) bool {
	t.Helper()
	var v string
	if err := o.kok.QueryRow("SHOW CREATE USER '" + kul + "'@'localhost'").Scan(&v); err != nil {
		t.Fatalf("%s: %v", kul, err)
	}
	return strings.Contains(v, "ACCOUNT LOCK")
}

func TestGeriYukleme_AskiKilidiArsivdenGelmez_HesaptanUygulanir(t *testing.T) {
	o := bdo_ortam(t)
	on := o.on
	ad, kendi, parola := on+"_geri", on+"_geriu", "AskiParola123"
	o.kokExec(t, "CREATE DATABASE `"+ad+"`")
	if _, err := o.panel.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (1,?,?,'')`, ad, kendi); err != nil {
		t.Fatal(err)
	}
	var hash string
	if err := o.kok.QueryRow(`SELECT PASSWORD(?)`, parola).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	// Askıdayken alınmış yedek: CREATE USER satırı ACCOUNT LOCK taşır.
	satirlar := []string{
		"CREATE USER IF NOT EXISTS `" + kendi + "`@`localhost` IDENTIFIED BY PASSWORD '" + hash + "' ACCOUNT LOCK;",
		"GRANT ALL PRIVILEGES ON `" + ad + "`.* TO `" + kendi + "`@`localhost`;",
	}
	if err := os.WriteFile(filepath.Join(dir, kullaniciDosyaAdi), []byte(strings.Join(satirlar, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := dbKullanicilariUygula(o.panel, 1, dir, map[string]bool{ad: true}); err != nil {
		t.Fatal(err)
	}
	if o.kilitli(t, kendi) || !o.baglanir(kendi, parola, ad) {
		t.Fatal("askıda alınmış yedek açık hesaba kilitli kullanıcı getirmemeli")
	}
	sk := "c_" + on
	// Askı sürerken geri yükleme: kilit hesabın askısından uygulanır.
	if _, err := o.panel.Exec(`UPDATE domains SET askida=1 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	dbKilidiniHesabaUydur(o.panel, 1, sk)
	if !o.kilitli(t, kendi) || o.baglanir(kendi, parola, ad) {
		t.Fatal("askıdaki hesaba geri yüklenen kullanıcı kilitli olmalı")
	}
	if _, err := o.panel.Exec(`UPDATE domains SET askida=0 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	dbKilidiniHesabaUydur(o.panel, 1, sk)
	if o.kilitli(t, kendi) || !o.baglanir(kendi, parola, ad) {
		t.Fatal("açık hesapta kullanıcı kilitsiz olmalı")
	}
}
