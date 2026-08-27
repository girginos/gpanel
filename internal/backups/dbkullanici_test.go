package backups

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Gercek MariaDB gerektirir (root socket auth). Yoksa test atlanir.
const (
	tDB  = "gp_t_dbkul"
	tKul = "gp_t_kul"
	tPw  = "S3cr3t-Parola!x"
)

func mysqlVarMi(t *testing.T) {
	t.Helper()
	if err := exec.Command("mysql", "-e", "SELECT 1").Run(); err != nil {
		t.Skip("mysql erisimi yok, atlaniyor")
	}
}

func temizle() {
	_ = mysqlCalistir("DROP DATABASE IF EXISTS `" + tDB + "`;")
	_ = mysqlCalistir("DROP USER IF EXISTS '" + tKul + "'@'localhost';")
}

func kur(t *testing.T) {
	t.Helper()
	temizle()
	if err := mysqlCalistir("CREATE DATABASE `" + tDB + "`;" +
		"CREATE USER '" + tKul + "'@'localhost' IDENTIFIED BY '" + tPw + "';" +
		"GRANT ALL PRIVILEGES ON `" + tDB + "`.* TO '" + tKul + "'@'localhost';"); err != nil {
		t.Fatalf("kurulum: %v", err)
	}
}

// TestKullaniciYazUygula: yedege giren kullanici, silindikten sonra arsivden
// GERI GELMELI ve parolasi calismaya devam ETMELI.
func TestKullaniciYazUygula(t *testing.T) {
	mysqlVarMi(t)
	kur(t)
	defer temizle()

	// POZITIF KONTROL: olcum yontemi CALISIYOR mu? Bu gecmezse asagidaki
	// "parola geri geldi" iddiasi hicbir sey kanitlamaz.
	if err := exec.Command("mysql", "-u", tKul, "-p"+tPw, "-e", "SELECT 1", tDB).Run(); err != nil {
		t.Fatalf("pozitif kontrol: yeni kullanici zaten baglanamiyor: %v", err)
	}

	dir := t.TempDir()
	if n := dbKullanicilariYaz(dir, []string{tDB}); n != 1 {
		t.Fatalf("yazilan hesap sayisi = %d, beklenen 1", n)
	}
	ham, err := os.ReadFile(filepath.Join(dir, kullaniciDosyaAdi))
	if err != nil {
		t.Fatalf("dosya okunamadi: %v", err)
	}
	icerik := string(ham)
	// MariaDB kimlikleri backtick ile tirnaklar; MySQL tek tirnak kullanir.
	if !strings.Contains(icerik, "CREATE USER IF NOT EXISTS") ||
		!strings.Contains(icerik, tKul) {
		t.Fatalf("CREATE USER yok:\n%s", icerik)
	}
	if !strings.Contains(icerik, "ON `"+tDB+"`.*") {
		t.Fatalf("DB GRANT yok:\n%s", icerik)
	}
	// Dosya parola hash'i tasir: izinler 0600 olmali.
	fi, _ := os.Stat(filepath.Join(dir, kullaniciDosyaAdi))
	if fi.Mode().Perm() != 0600 {
		t.Fatalf("izin = %v, beklenen 0600", fi.Mode().Perm())
	}

	// Kullaniciyi YOK ET, sonra arsivden geri yukle.
	if err := mysqlCalistir("DROP USER '" + tKul + "'@'localhost';"); err != nil {
		t.Fatalf("drop user: %v", err)
	}
	if kullaniciVarMi(t) {
		t.Fatal("negatif kontrol: kullanici silinmedi, testin geri kalani anlamsiz")
	}
	n, err := dbKullanicilariUygula(dir, map[string]bool{tDB: true})
	if err != nil || n == 0 {
		t.Fatalf("uygula: n=%d err=%v", n, err)
	}
	if !kullaniciVarMi(t) {
		t.Fatal("kullanici geri gelmedi")
	}
	// PAROLA da geri gelmeli: hash ile birlikte dondu mu, gercekten baglanarak olc.
	// Soket uzerinden: @localhost hesabi TCP 127.0.0.1 ile ESLESMEZ.
	if err := exec.Command("mysql", "-u", tKul, "-p"+tPw,
		"-e", "SELECT 1", tDB).Run(); err != nil {
		t.Fatalf("geri yuklenen kullanici parolasiyla baglanamadi: %v", err)
	}
}

// TestGlobalYetkiReddedilir: NEGATIF KONTROL. Arsivden gelen bir GRANT asla
// global yetki kazandiramaz — ne yazarken dosyaya girer, ne okurken uygulanir.
func TestGlobalYetkiReddedilir(t *testing.T) {
	mysqlVarMi(t)
	kur(t)
	defer temizle()

	// (a) Yazma tarafi: kullanicinin GERCEK global yetkisi var; arsive GIRMEMELI.
	if err := mysqlCalistir("GRANT SELECT ON *.* TO '" + tKul + "'@'localhost';"); err != nil {
		t.Fatalf("global grant: %v", err)
	}
	dir := t.TempDir()
	dbKullanicilariYaz(dir, []string{tDB})
	ham, _ := os.ReadFile(filepath.Join(dir, kullaniciDosyaAdi))
	for _, l := range strings.Split(string(ham), "\n") {
		if strings.HasPrefix(l, "GRANT") && strings.Contains(l, "ON *.*") &&
			!strings.Contains(l, "USAGE ON *.*") {
			t.Fatalf("global yetki arsive sizdi: %s", l)
		}
	}

	// (b) Okuma tarafi: dosyaya ELLE global yetki enjekte et; uygulanMAMALI.
	yol := filepath.Join(dir, kullaniciDosyaAdi)
	kotu := "GRANT ALL PRIVILEGES ON *.* TO '" + tKul + "'@'localhost' WITH GRANT OPTION;\n" +
		"GRANT ALL PRIVILEGES ON `mysql`.* TO '" + tKul + "'@'localhost';\n" +
		// ZINCIRLEME: ilk ifade izinli, ikincisi global. `mysql -e` ikisini de
		// calistirirdi; satirda noktali virgul kalmasi reddi tetiklemeli.
		"GRANT ALL PRIVILEGES ON `" + tDB + "`.* TO '" + tKul + "'@'localhost'; " +
		"GRANT ALL PRIVILEGES ON *.* TO '" + tKul + "'@'localhost';\n"
	_ = os.WriteFile(yol, append(ham, []byte(kotu)...), 0600)

	_ = mysqlCalistir("REVOKE SELECT ON *.* FROM '" + tKul + "'@'localhost';")
	if _, err := dbKullanicilariUygula(dir, map[string]bool{tDB: true}); err != nil {
		t.Fatalf("uygula: %v", err)
	}
	out, _ := exec.Command("mysql", "-N", "-B", "-e",
		"SHOW GRANTS FOR '"+tKul+"'@'localhost'").Output()
	for _, l := range strings.Split(string(out), "\n") {
		if strings.Contains(l, "ON *.*") && !strings.Contains(l, "USAGE ON *.*") {
			t.Fatalf("enjekte edilen global yetki UYGULANDI: %s", l)
		}
		if strings.Contains(l, "`mysql`.*") {
			t.Fatalf("izin listesi disindaki veritabani yetkisi UYGULANDI: %s", l)
		}
	}
}

func kullaniciVarMi(t *testing.T) bool {
	t.Helper()
	out, err := exec.Command("mysql", "-N", "-B", "-e",
		"SELECT COUNT(*) FROM mysql.user WHERE User='"+tKul+"'").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "1"
}

// TestYapilandirmaAyikla: eski arsivler icin kimlik kurtarma ayristiricisi.
func TestYapilandirmaAyikla(t *testing.T) {
	wp := `<?php
define( 'DB_NAME', 'admin_wp_fo2wi' );
define( 'DB_USER', 'admin_kullanici' );
define( 'DB_PASSWORD', 'p@ss"word-1' );
define( 'DB_HOST', 'localhost' );`
	d, u, p := wpAyikla(wp)
	if d != "admin_wp_fo2wi" || u != "admin_kullanici" || p != `p@ss"word-1` {
		t.Fatalf("wpAyikla = %q %q %q", d, u, p)
	}

	env := "APP_ENV=production\nDB_DATABASE=laravel_db  # yorum\nDB_USERNAME=\"lar_kul\"\nDB_PASSWORD='gizli p'\n"
	d, u, p = envAyikla(env)
	if d != "laravel_db" || u != "lar_kul" || p != "gizli p" {
		t.Fatalf("envAyikla = %q %q %q", d, u, p)
	}
}

// TestGuvenliOkuSymlink: kiraci wp-config.php yerine symlink birakirsa panel
// (root) hedefi OKUMAMALI.
func TestGuvenliOkuSymlink(t *testing.T) {
	kok := t.TempDir()
	if err := os.MkdirAll(filepath.Join(kok, "public_html"), 0755); err != nil {
		t.Fatal(err)
	}
	gercek := filepath.Join(kok, "public_html", "wp-config.php")
	if err := os.WriteFile(gercek, []byte("define('DB_NAME','x');"), 0644); err != nil {
		t.Fatal(err)
	}
	// Pozitif kontrol: duz dosya OKUNABILMELI (aksi halde negatif kontrol anlamsiz).
	if _, err := guvenliOku(kok, "public_html/wp-config.php", 1<<20); err != nil {
		t.Fatalf("duz dosya okunamadi: %v", err)
	}
	// Negatif kontrol: symlink REDDEDILMELI.
	kurban := filepath.Join(kok, "hedef.txt")
	_ = os.WriteFile(kurban, []byte("gizli"), 0600)
	_ = os.Remove(gercek)
	if err := os.Symlink(kurban, gercek); err != nil {
		t.Skipf("symlink olusturulamadi: %v", err)
	}
	if b, err := guvenliOku(kok, "public_html/wp-config.php", 1<<20); err == nil {
		t.Fatalf("symlink IZLENDI, icerik okundu: %q", string(b))
	}
}
