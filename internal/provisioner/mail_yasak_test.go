package provisioner

// mail_yasak_test.go — kafeste düz mail çıkışı yasağı (180/181 olayı, 2026-10-09).
//
// Neyi korur: (1) havuz sendmail_path'i hızlı-başarısız betiği gösterir ve kullanıcı ezemez; (2) betik
// gerçekten HIZLI başarısız olur, stdin'i tüketir, -bs'te takılmaz, gerçek PHP'de mail() false verir;
// (3) kurucu idempotent ve bozulmuşu onarır; (4) yetim postdrop yalnız KAFESTE bulunur ve yalnız
// postdrop öldürülür; (5) bağlantı: betik havuzlardan ÖNCE kurulur, yetim temizliği havuz reload'larından ÖNCE koşar.

import (
	"bytes"
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ---- havuz satırı ----

func TestRenderTenantPool_SendmailPathHizliBasarisizBetik(t *testing.T) {
	pool := renderTenantPool(nil, "c_mailyasak", 0)
	want := "php_admin_value[sendmail_path] = " + mailYasakYol + " -t -i\n"
	if strings.Count(pool, want) != 1 {
		t.Fatalf("havuz tam bir kez %q içermeli:\n%s", strings.TrimSpace(want), pool)
	}
	// sendmail_path başka hiçbir yerde geçmemeli: sonradan gelen bir satır ilkini ezerdi (php-fpm: son kazanır).
	if n := strings.Count(pool, "sendmail_path"); n != 1 {
		t.Fatalf("sendmail_path havuzda %d kez geçiyor, 1 olmalı:\n%s", n, pool)
	}
	// php_admin_value → kullanıcı ini_set/.user.ini ile EZEMEZ (php_value olsaydı ezerdi).
	if strings.Contains(pool, "php_value[sendmail_path]") || strings.Contains(pool, "php_flag[sendmail_path]") {
		t.Fatalf("sendmail_path php_admin_value olmalı:\n%s", pool)
	}
	// [sk] bölümünün içinde (başlıktan sonra) ve catch_workers_output'tan önce.
	i, j, h := strings.Index(pool, want), strings.Index(pool, "catch_workers_output"), strings.Index(pool, "[c_mailyasak]")
	if h != 0 || i < h || j < i {
		t.Fatalf("satır yeri yanlış (başlık=%d satır=%d son=%d):\n%s", h, i, j, pool)
	}
	if mailYasakSendmailPath != mailYasakYol+" -t -i" {
		t.Fatalf("sendmail_path değeri PHP varsayılan biçimini (-t -i) korumalı: %q", mailYasakSendmailPath)
	}
}

// ---- kurucu ----

func mailYasakTestOrtam(t *testing.T) (betik string, etiketle *int) {
	t.Helper()
	dizin := t.TempDir()
	oY, oE := mailYasakBetikYolu, mailYasakEtiketle
	t.Cleanup(func() { mailYasakBetikYolu, mailYasakEtiketle = oY, oE })
	mailYasakBetikYolu = filepath.Join(dizin, "bin", "girginospanel-mail-yasak")
	n := 0
	mailYasakEtiketle = func(string) { n++ }
	return mailYasakBetikYolu, &n
}

func TestMailYasakKur_YazarIdempotentOnarir(t *testing.T) {
	yol, etiketle := mailYasakTestOrtam(t)
	if !bytes.HasPrefix(mailYasakBetik, []byte("#!/bin/sh\n")) || !bytes.Contains(mailYasakBetik, []byte("exit 69")) {
		t.Fatalf("gömülü betik beklenen biçimde değil:\n%s", mailYasakBetik)
	}

	if err := MailYasakKur(); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(yol); err != nil || !bytes.Equal(b, mailYasakBetik) {
		t.Fatalf("betik gömülü kaynakla aynı yazılmalı: %v", err)
	}
	if st, _ := os.Stat(yol); st.Mode().Perm() != 0o755 {
		t.Fatalf("betik 0755 olmalı: %v", st.Mode())
	}
	if *etiketle != 1 {
		t.Fatalf("ilk kurulumda SELinux etiketi 1 kez tazelenmeli: %d", *etiketle)
	}

	// idempotent: ikinci çalıştırma yeniden yazmaz, etiketlemez
	st1, _ := os.Stat(yol)
	time.Sleep(20 * time.Millisecond)
	if err := MailYasakKur(); err != nil {
		t.Fatal(err)
	}
	st2, _ := os.Stat(yol)
	if !st2.ModTime().Equal(st1.ModTime()) || *etiketle != 1 {
		t.Fatalf("ikinci çalıştırma dosyaya dokunmamalı (etiket=%d)", *etiketle)
	}

	// elle değiştirilmiş/bozulmuş içerik onarılır
	if err := os.WriteFile(yol, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := MailYasakKur(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(yol); !bytes.Equal(b, mailYasakBetik) {
		t.Fatal("değiştirilmiş betik onarılmalı")
	}
	// içerik doğru ama izin bozuk (çalıştırılamaz) → onarılır
	if err := os.Chmod(yol, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := MailYasakKur(); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(yol); st.Mode().Perm() != 0o755 {
		t.Fatalf("izin 0755'e onarılmalı: %v", st.Mode())
	}
}

// ---- betiğin davranışı ----

func mailYasakBetikYaz(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh yok")
	}
	yol := filepath.Join(t.TempDir(), "girginospanel-mail-yasak")
	if err := os.WriteFile(yol, mailYasakBetik, 0o755); err != nil {
		t.Fatal(err)
	}
	return yol
}

func cikisKodu(t *testing.T, err error) int {
	t.Helper()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("çıkış kodu beklenirken hata: %v", err)
	}
	return ee.ExitCode()
}

// mail() biçimi: mesaj stdin'den gelir, EOF'ta biter. Betik (a) 69 ile çıkmalı (0/75 DEĞİL),
// (b) stdin'i SON BAYTINA kadar tüketmeli (aksi halde yazan taraf EPIPE alır), (c) stdout'a hiçbir
// şey yazmamalı, (d) stderr'e açıklayıcı tek mesaj bırakmalı.
func TestMailYasakBetik_MailModu_HizliBasarisiz_StdinTuketir(t *testing.T) {
	yol := mailYasakBetikYaz(t)
	ctx, iptal := context.WithTimeout(context.Background(), 15*time.Second)
	defer iptal()
	cmd := exec.CommandContext(ctx, yol, "-t", "-i")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	girdi, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	const blok, adet = 64 << 10, 80 // 5 MiB mesaj
	var yazErr error
	yazilan := 0
	bitti := make(chan struct{})
	go func() {
		defer close(bitti)
		b := bytes.Repeat([]byte("x"), blok)
		for i := 0; i < adet; i++ {
			n, err := girdi.Write(b)
			yazilan += n
			if err != nil {
				yazErr = err
				return
			}
		}
		_ = girdi.Close()
	}()
	<-bitti
	err = cmd.Wait()
	if yazErr != nil || yazilan != blok*adet {
		t.Fatalf("betik mesajı sonuna kadar tüketmeli (yazılan=%d/%d, hata=%v)", yazilan, blok*adet, yazErr)
	}
	if k := cikisKodu(t, err); k != 69 {
		t.Fatalf("çıkış kodu 69 (EX_UNAVAILABLE) olmalı, %d", k)
	}
	if stdout.Len() != 0 {
		t.Fatalf("mail modunda stdout boş olmalı: %q", stdout.String())
	}
	if m := stderr.String(); !strings.Contains(m, "girginospanel") || !strings.Contains(m, "kapali") || strings.Count(m, "\n") != 1 {
		t.Fatalf("stderr'de tek satır açıklama olmalı: %q", m)
	}
}

// -bs (SMTP-stdio): istemci selam bekler. Betik stdin KAPANMADAN kendiliğinden çıkmalı ve 5xx selamı
// vermeli; yoksa istemci ile betik karşılıklı bekleyip PHP işçisini sonsuza dek takardı.
func TestMailYasakBetik_BsModu_StdinBeklemezVe554Verir(t *testing.T) {
	yol := mailYasakBetikYaz(t)
	for _, args := range [][]string{{"-bs"}, {"-bs", "-i"}, {"-i", "-bs"}} {
		cmd := exec.Command(yol, args...)
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		girdi, err := cmd.StdinPipe() // asla yazılmaz / kapatılmaz
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		sonuc := make(chan error, 1)
		go func() { sonuc <- cmd.Wait() }()
		select {
		case err := <-sonuc:
			if k := cikisKodu(t, err); k != 69 {
				t.Fatalf("%v: çıkış kodu 69 olmalı, %d", args, k)
			}
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			t.Fatalf("%v: betik stdin kapanmadan çıkmadı (karşılıklı bekleme)", args)
		}
		_ = girdi.Close()
		if !strings.HasPrefix(stdout.String(), "554 ") {
			t.Fatalf("%v: -bs selamı 554 ile başlamalı: %q", args, stdout.String())
		}
	}
}

// Gerçek PHP: sendmail_path betiği gösterince mail() false döner, hızlıdır, mesaj PHP'nin stderr'ine düşer.
// 0 ya da 75 ile çıkılsaydı PHP "gönderildi" sayardı — çıkış kodu sözleşmesini yalnız gerçek PHP kanıtlar.
func TestMailYasakBetik_GercekPHP_MailFalseDoner(t *testing.T) {
	php, err := exec.LookPath("php")
	if err != nil {
		t.Skip("php yok")
	}
	yol := mailYasakBetikYaz(t)
	ctx, iptal := context.WithTimeout(context.Background(), 20*time.Second)
	defer iptal()
	cmd := exec.CommandContext(ctx, php, "-n", "-d", "sendmail_path="+yol+" -t -i",
		"-r", `echo json_encode(mail("a@example.com","konu","govde","From: b@example.com"));`)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	basla := time.Now()
	if err := cmd.Run(); err != nil {
		t.Fatalf("php çalışmadı: %v (%s)", err, stderr.String())
	}
	if gecen := time.Since(basla); gecen > 10*time.Second {
		t.Fatalf("mail() hızlı dönmeli, %v sürdü", gecen)
	}
	if got := strings.TrimSpace(stdout.String()); got != "false" {
		t.Fatalf("mail() false dönmeli (gönderildi sanılmamalı), döndü: %q", got)
	}
	if !strings.Contains(stderr.String(), "girginospanel") {
		t.Fatalf("açıklayıcı mesaj PHP'nin stderr'ine düşmeli: %q", stderr.String())
	}
}

// ---- yetim postdrop temizliği ----

func TestMailCgroupKafeste(t *testing.T) {
	cases := []struct {
		ad, cg string
		want   bool
	}{
		{"v2 doğrudan", "0::/system.slice/php-fpm-c_a.service\n", true},
		{"v2 slice altında (gerçek 181 yolu)", "0::/girginos.slice/girginos-c_girginos.slice/php-fpm-c_girginos.service\n", true},
		{"v2 alt cgroup", "0::/system.slice/php-fpm-c_a.service/alt\n", true},
		{"v1 çok satır", "12:cpu,cpuacct:/system.slice/php-fpm-c_a.service\n1:name=systemd:/system.slice/php-fpm-c_a.service\n", true},
		{"v1 yalnız bir satır kafes", "12:cpu:/user.slice\n1:name=systemd:/system.slice/php-fpm-c_a.service\n", true},
		{"paylaşılan master (php-fpm.service)", "0::/system.slice/php-fpm.service\n", false},
		{"paylaşılan master (remi)", "0::/system.slice/php83-php-fpm.service\n", false},
		{"tenant dışı php-fpm-* birimi", "0::/system.slice/php-fpm-yedek.service\n", false},
		{"ön-ek çakışması (xphp-fpm-)", "0::/system.slice/xphp-fpm-c_a.service\n", false},
		{"birim adı uzatılmış (.scope)", "0::/system.slice/php-fpm-c_a.service.scope\n", false},
		{"başka birim", "0::/system.slice/postfix.service\n", false},
		{"kafes dışı oturum (SSH/cron)", "0::/user.slice/user-0.slice/session-3.scope\n", false},
		{"boş", "", false},
		{"bozuk satır", "yol-yok\n", false},
	}
	for _, c := range cases {
		if got := mailCgroupKafeste(c.cg); got != c.want {
			t.Errorf("%s: got %v want %v (%q)", c.ad, got, c.want, c.cg)
		}
	}
}

// sahteProc: {pid: {comm, cgroup}} → geçici /proc ağacı. cgroup=="" → dosya HİÇ yazılmaz (okunamaz süreç).
func sahteProc(t *testing.T, surecler map[int][2]string) string {
	t.Helper()
	kok := t.TempDir()
	for pid, v := range surecler {
		d := filepath.Join(kok, strconv.Itoa(pid))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "comm"), []byte(v[0]+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if v[1] != "" {
			if err := os.WriteFile(filepath.Join(d, "cgroup"), []byte(v[1]), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	// gerçek /proc'ta sayısal olmayan girdiler de vardır
	_ = os.MkdirAll(filepath.Join(kok, "self"), 0o755)
	_ = os.WriteFile(filepath.Join(kok, "stat"), []byte("cpu 1 2 3\n"), 0o644)
	return kok
}

func mailOldurmaDikisi(t *testing.T, kok string, hataPid map[int]bool) *[]int {
	t.Helper()
	oK, oO := mailProcKok, mailSurecOldur
	t.Cleanup(func() { mailProcKok, mailSurecOldur = oK, oO })
	mailProcKok = kok
	var olen []int
	mailSurecOldur = func(pid int) error {
		if hataPid[pid] {
			return errors.New("öldürülemedi")
		}
		olen = append(olen, pid)
		return nil
	}
	return &olen
}

func TestMailTakiliPostdropOldur_YalnizKafesteVeYalnizPostdrop(t *testing.T) {
	const kafes = "0::/girginos.slice/girginos-c_a.slice/php-fpm-c_a.service\n"
	kok := sahteProc(t, map[int][2]string{
		1:   {"postdrop", kafes},                                                                                          // init koruması
		100: {"postdrop", kafes},                                                                                          // ÖLDÜR
		101: {"postdrop", "0::/girginos.slice/girginos-c_b.slice/php-fpm-c_b.service\n"},                                  // ÖLDÜR (başka tenant da kafes)
		102: {"sendmail", kafes},                                                                                          // sendmail öldürülmez: postdrop ölünce kendiliğinden çıkar
		103: {"postdrop", "0::/user.slice/user-0.slice/session-3.scope\n"},                                                // kafes DIŞI (cron/SSH)
		104: {"postdrop", "0::/system.slice/php83-php-fpm.service\n"},                                                     // paylaşılan master: orada postdrop MEŞRU çalışır
		105: {"php-fpm", kafes},                                                                                           // yanlış ad
		106: {"postdrop", "0::/system.slice/php-fpm-c_a.service/alt\n"},                                                   // ÖLDÜR (alt cgroup)
		107: {"postdrop", "12:cpu:/system.slice/php-fpm-c_a.service\n1:name=systemd:/system.slice/php-fpm-c_a.service\n"}, // ÖLDÜR (v1)
		108: {"postdrop", ""},                                                                                             // cgroup okunamaz
		109: {"postdrop-x", kafes},                                                                                        // tam ad eşleşmesi
		110: {"xpostdrop", kafes},
	})
	olen := mailOldurmaDikisi(t, kok, nil)
	n := mailTakiliPostdropOldur()
	sort.Ints(*olen)
	if want := []int{100, 101, 106, 107}; !reflect.DeepEqual(*olen, want) || n != len(want) {
		t.Fatalf("öldürülenler %v (n=%d), beklenen %v", *olen, n, want)
	}
}

func TestMailTakiliPostdropOldur_OldurulemeyenSayilmaz_ProcYoksaSifir(t *testing.T) {
	const kafes = "0::/system.slice/php-fpm-c_a.service\n"
	kok := sahteProc(t, map[int][2]string{100: {"postdrop", kafes}, 101: {"postdrop", kafes}})
	olen := mailOldurmaDikisi(t, kok, map[int]bool{100: true})
	if n := mailTakiliPostdropOldur(); n != 1 || !reflect.DeepEqual(*olen, []int{101}) {
		t.Fatalf("yalnız başarıyla öldürülen sayılmalı: n=%d olen=%v", n, *olen)
	}
	mailProcKok = filepath.Join(t.TempDir(), "yok")
	if n := mailTakiliPostdropOldur(); n != 0 {
		t.Fatalf("/proc okunamazsa 0 dönmeli: %d", n)
	}
}

// ---- bağlantılar (AST) ----
//
// repairTenantPoolDrift / Init'in gövdesi birim testle sürülemez (DB + systemctl gerekir); sıra
// değişmezleri bu yüzden kaynak ağacından doğrulanır. Biçimden (boşluk, yorum) etkilenmez.

func fonksiyonGovdesi(t *testing.T, dosya, ad string) (*token.FileSet, *ast.BlockStmt) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, dosya, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == ad {
			return fset, fd.Body
		}
	}
	t.Fatalf("%s içinde %s bulunamadı", dosya, ad)
	return nil, nil
}

// cagriKonumlari: gövdedeki çağrıların kaynak sırasındaki konumları. ad "pkg.Fn" ya da "Fn"; harf
// ise çağrının argümanlarından biri bu string sabiti olmalı (boş = argüman denetimi yok).
func cagriKonumlari(g *ast.BlockStmt, ad, harf string) []token.Pos {
	var konum []token.Pos
	ast.Inspect(g, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		var isim string
		switch fn := c.Fun.(type) {
		case *ast.Ident:
			isim = fn.Name
		case *ast.SelectorExpr:
			if x, ok := fn.X.(*ast.Ident); ok {
				isim = x.Name + "." + fn.Sel.Name
			}
		}
		if isim != ad {
			return true
		}
		if harf != "" {
			var var_ bool
			for _, a := range c.Args {
				if bl, ok := a.(*ast.BasicLit); ok && bl.Kind == token.STRING {
					if s, err := strconv.Unquote(bl.Value); err == nil && s == harf {
						var_ = true
					}
				}
			}
			if !var_ {
				return true
			}
		}
		konum = append(konum, c.Pos())
		return true
	})
	return konum
}

func TestBaglanti_Init_MailBetigiVeYetimTemizligiHavuzlardanOnce(t *testing.T) {
	_, g := fonksiyonGovdesi(t, "provisioner.go", "Init")
	kur := cagriKonumlari(g, "MailYasakKur", "")
	temizle := cagriKonumlari(g, "mailTakiliPostdropOldur", "")
	sweep := cagriKonumlari(g, "EnsureTenantFPMOnStartup", "")
	if len(kur) != 1 || len(temizle) != 1 || len(sweep) != 1 {
		t.Fatalf("Init'te MailYasakKur=%d, mailTakiliPostdropOldur=%d, EnsureTenantFPMOnStartup=%d çağrı var; her biri 1 olmalı",
			len(kur), len(temizle), len(sweep))
	}
	if kur[0] >= sweep[0] {
		t.Fatal("MailYasakKur, havuz drift onarımını yapan EnsureTenantFPMOnStartup'tan ÖNCE çağrılmalı " +
			"(aksi halde pool.conf henüz var olmayan betiği gösterir)")
	}
	if temizle[0] >= sweep[0] {
		t.Fatal("yetim postdrop temizliği EnsureTenantFPMOnStartup'tan ÖNCE koşmalı " +
			"(havuz reload'ları ve birim yeniden başlatmaları temiz bir cgroup'ta yapılsın)")
	}
}
