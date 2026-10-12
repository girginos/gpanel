package provisioner

// pma_heal_test.go — pma-signon.php (kanonik içerik + heal güncelliği + davranış).
//
// 🔴 OLAY (2026-10-12, bir müşteri sunucusu): phpMyAdmin girişi "Access denied" ile
// düşünce SignonURL'e TOKENSIZ dönüyor ve nedeni signon oturumuna
// PMA_single_signon_error_message olarak yazıyordu; betik bunu okumadan her durumda
// "Token eksik" yazıyordu → kullanıcı/destek gerçek nedeni hiç görmedi.
// Ayrıca heal yalnız İŞARET dizgelerine bakıyordu: betikteki bir düzeltme mevcut
// kurulumlara (eski kopya her iki işareti de taşıdığı için) ASLA ulaşmazdı.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Paralel yol: kurulum betiği assets kopyasını, heal ise Go sabitini yazar —
// ikisi ayrışırsa kurulum türüne göre farklı betik çalışır.
func TestPMASignon_AssetVeSabitBirebir(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "assets", "phpmyadmin", "pma-signon.php"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != pmaSignonPHP {
		t.Fatal("assets/phpmyadmin/pma-signon.php ile pmaSignonPHP sabiti FARKLI — ikisini birlikte güncelleyin")
	}
}

// Heal, eski işaretleri (host=localhost + use_strict_mode) taşıyan ama kanonikten
// FARKLI kopyayı bayat saymalı (eski koşul bunu "zaten doğru" sayıyordu).
func TestPMASignon_HealEskiKopyayiBayatSayar(t *testing.T) {
	if !signonGuncel([]byte(pmaSignonPHP)) {
		t.Fatal("kanonik içerik güncel sayılmadı")
	}
	eski := strings.Replace(pmaSignonPHP, "PMA_single_signon_error_message", "PMA_eski_surum", -1)
	if !strings.Contains(eski, "PMA_single_signon_host'] = 'localhost'") || !strings.Contains(eski, "session.use_strict_mode") {
		t.Fatal("test kurgusu: eski kopya iki eski işareti de taşımalı")
	}
	if signonGuncel([]byte(eski)) {
		t.Error("eski işaretleri taşıyan farklı kopya GÜNCEL sayıldı — düzeltme mevcut kurulumlara ulaşmaz")
	}
}

// signonCalistir: betiği php-cgi ile (gerçek CGI ortamı) çalıştırır.
func signonCalistir(t *testing.T, oturumDizini, cerez, sorgu string) (durum string, govde string) {
	t.Helper()
	cgi, err := exec.LookPath("php-cgi")
	if err != nil {
		t.Skip("php-cgi yok — davranış testi atlandı")
	}
	betik := filepath.Join(t.TempDir(), "pma-signon.php")
	if err := os.WriteFile(betik, []byte(pmaSignonPHP), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(cgi, "-d", "session.save_handler=files", "-d", "session.save_path="+oturumDizini,
		"-d", "display_errors=0", "-d", "log_errors=0")
	cmd.Env = []string{
		"REDIRECT_STATUS=200", "GATEWAY_INTERFACE=CGI/1.1", "SERVER_PROTOCOL=HTTP/1.1",
		"REQUEST_METHOD=GET", "SCRIPT_FILENAME=" + betik, "QUERY_STRING=" + sorgu,
		"HTTP_COOKIE=" + cerez, "PATH=/usr/bin:/bin",
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("php-cgi: %v\n%s", err, out)
	}
	baslik, govdeB, _ := strings.Cut(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n\n")
	durum = "200"
	for _, s := range strings.Split(baslik, "\n") {
		if v, ok := strings.CutPrefix(s, "Status: "); ok {
			durum = strings.Fields(v)[0]
		}
	}
	return durum, govdeB
}

// 🔴 ÇEKİRDEK: phpMyAdmin'in yazdığı hata kullanıcıya GÖSTERİLİR (kaçışlı), başarısız
// kimlik (parola dahil) oturumda BEKLETİLMEZ.
func TestPMASignon_PhpMyAdminHatasiGosterilir(t *testing.T) {
	dir := t.TempDir()
	id := "testoturum0123456789abcdefABCDEF"
	hata := `MySQL sunucusuna oturum açılamıyor <script>alert(1)</script>`
	veri := fmt.Sprintf(`PMA_single_signon_user|s:6:"u20_db";PMA_single_signon_password|s:8:"Gizli-P1";PMA_single_signon_error_message|s:%d:"%s";`, len(hata), hata)
	oturum := filepath.Join(dir, "sess_"+id)
	if err := os.WriteFile(oturum, []byte(veri), 0o600); err != nil {
		t.Fatal(err)
	}
	durum, govde := signonCalistir(t, dir, "pma_signon="+id, "")
	if durum != "401" {
		t.Errorf("durum=%s; 401 bekleniyordu\n%s", durum, govde)
	}
	if !strings.Contains(govde, "MySQL sunucusuna oturum açılamıyor") {
		t.Errorf("phpMyAdmin'in hata nedeni gösterilmedi:\n%s", govde)
	}
	if strings.Contains(govde, "<script>") || !strings.Contains(govde, "&lt;script&gt;") {
		t.Errorf("hata metni kaçışlanmadı (XSS):\n%s", govde)
	}
	if strings.Contains(govde, "Token eksik") {
		t.Errorf("gerçek neden varken yine 'Token eksik' gösterildi")
	}
	if strings.Contains(govde, "Gizli-P1") {
		t.Errorf("parola yanıta sızdı")
	}
	if _, err := os.Stat(oturum); !os.IsNotExist(err) {
		t.Errorf("başarısız kimliği (parola) taşıyan oturum dosyası silinmedi (err=%v)", err)
	}
}

// Hata yoksa eski davranış: tokensız doğrudan erişim 400 "Token eksik".
func TestPMASignon_TokenYokHataYok400(t *testing.T) {
	dir := t.TempDir()
	for _, cerez := range []string{"", "pma_signon=olmayanoturum0123456789abcdefAB"} {
		durum, govde := signonCalistir(t, dir, cerez, "")
		if durum != "400" || !strings.Contains(govde, "Token eksik") {
			t.Errorf("çerez=%q: durum=%s govde=%q; 400 'Token eksik' bekleniyordu", cerez, durum, govde)
		}
	}
}
