package git

// kiraciCalistirGercek gerileme testleri — ayrıcalık düşürmenin KENDİSİ.
//
// symlink_guvenlik_test.go kiraciCalistir'ı kaydedici bir sahteyle değiştirir; oradaki
// Lstat ön denetimi (yolSymlinksiz) yalnız DURAĞAN symlink'leri yakalar. Lstat ile işlem
// arasında bir ara bileşen (site) ya da ~/.ssh symlink'e çevrilirse (TOCTOU: kiracı
// rename(2) ile atomik değiştirebilir) kurbanı koruyan TEK şey komutun kiracı kimliğinde
// çalışmasıdır. Bu dosya o bariyeri iki katmanda kilitler:
//
//  1. Sahte sudo/runuser/komut (PATH'in başına konur): tam argv ("-u <sk>", "--"), stdin
//     (tee ile ~/.ssh/config), cwd, ortam (panel ortamı sızmaz, HOME kiracının evi),
//     stdout/stderr ayrımı ve "ikisi de başarısızsa komut root olarak ASLA doğrudan
//     çalıştırılmaz".
//  2. Tümleşik (euid 0 + nobody varsa; 181'de testler root koşar): GERÇEK sudo/runuser ile
//     çekirdeğin erişimi kiracı kimliğinde değerlendirdiği — symlink İŞLEM ANINDA kurulu
//     olsa bile (yarış kazanılmış gibi) kök dosyası okunamaz, üzerine yazılamaz, silinemez;
//     meşru akış (kendi evinde yazma/silme) ise çalışır.

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// ---------- 1. katman: sahte sudo / runuser ----------

type sahteKomutOrtami struct {
	kayit, cwd string
}

// sahteKomutKur: bin/<ad> — argv'yi, ortamı, cwd'yi ve stdin'i kayit/<ad>.* dosyalarına
// yazar; kayit/<ad>.basarisiz varsa 1 ile çıkar. Yalnız mutlak yollar gömülür: runuser
// yedeğinde ortam (dolayısıyla test ortam değişkenleri) bilinçli olarak boşaltılır.
func sahteKomutKur(t *testing.T, bin, kayit, ad string) {
	t.Helper()
	k := filepath.Join(kayit, ad)
	betik := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > '" + k + ".argv'\n" +
		"env > '" + k + ".env'\n" +
		"pwd -P > '" + k + ".pwd'\n" +
		"cat > '" + k + ".stdin'\n" +
		"if [ -f '" + k + ".basarisiz' ]; then echo 'sahte " + ad + " hata' >&2; exit 1; fi\n" +
		"echo 'sahte-" + ad + "-cikti'\n" +
		"echo 'sahte-" + ad + "-hata' >&2\n"
	if err := os.WriteFile(filepath.Join(bin, ad), []byte(betik), 0o755); err != nil {
		t.Fatal(err)
	}
}

func sahteKomutOrtamiKur(t *testing.T) sahteKomutOrtami {
	t.Helper()
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("/bin/sh yok")
	}
	tmp := t.TempDir()
	bin, kayit, cwd := filepath.Join(tmp, "bin"), filepath.Join(tmp, "kayit"), filepath.Join(tmp, "cwd")
	for _, d := range []string{bin, kayit, cwd} {
		mustMkdir(t, d)
	}
	// "kokcalisti": kiracı adına çalıştırılacak komut. sudo/runuser sahteleri onu
	// ÇALIŞTIRMAZ → kayıt dosyası oluşursa komut kimlik düşürülmeden (root) çalışmıştır.
	for _, ad := range []string{"sudo", "runuser", "kokcalisti"} {
		sahteKomutKur(t, bin, kayit, ad)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GOSP_TEST_PANEL_SIRRI", "panel-ortami-sizmamali")
	eskiKok := evKoku
	evKoku = filepath.Join(tmp, "home")
	t.Cleanup(func() { evKoku = eskiKok })
	gercekCwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	return sahteKomutOrtami{kayit: kayit, cwd: gercekCwd}
}

func (s sahteKomutOrtami) cagrildi(ad string) bool {
	return varMi(filepath.Join(s.kayit, ad+".argv"))
}

func (s sahteKomutOrtami) dosya(t *testing.T, ad, uzanti string) string {
	t.Helper()
	return icerik(t, filepath.Join(s.kayit, ad+"."+uzanti))
}

func (s sahteKomutOrtami) argv(t *testing.T, ad string) []string {
	t.Helper()
	if !s.cagrildi(ad) {
		t.Fatalf("%s hiç çağrılmadı", ad)
	}
	return strings.Split(strings.TrimSuffix(s.dosya(t, ad, "argv"), "\n"), "\n")
}

func (s sahteKomutOrtami) basarisizYap(t *testing.T, ad string) {
	t.Helper()
	mustYaz(t, filepath.Join(s.kayit, ad+".basarisiz"), "")
}

var kiraciKomutu = []string{"kokcalisti", "-a", "b c"}

func TestKiraciCalistirGercek_SudoIleKiraciKimligindeCalisir(t *testing.T) {
	s := sahteKomutOrtamiKur(t)
	o, e, err := kiraciCalistirGercek(testSK, s.cwd, []byte("GIRDI-VERISI\n"), kiraciKomutu...)
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v (%s)", err, e)
	}
	if s.cagrildi("kokcalisti") {
		t.Fatal("komut kiracı kimliğine düşürülmeden (root olarak) DOĞRUDAN çalıştırıldı")
	}
	want := append([]string{"-u", testSK, "-H", "--"}, kiraciKomutu...)
	if got := s.argv(t, "sudo"); !reflect.DeepEqual(got, want) {
		t.Fatalf("sudo argv:\n got %q\nwant %q", got, want)
	}
	if got := s.dosya(t, "sudo", "stdin"); got != "GIRDI-VERISI\n" {
		t.Fatalf("stdin komuta verilmedi (tee ile ~/.ssh/config boş yazılır): %q", got)
	}
	if got := strings.TrimSpace(s.dosya(t, "sudo", "pwd")); got != s.cwd {
		t.Fatalf("cwd: got %q want %q", got, s.cwd)
	}
	if o != "sahte-sudo-cikti\n" || e != "sahte-sudo-hata\n" {
		t.Fatalf("stdout/stderr ayrı dönmeli: o=%q e=%q", o, e)
	}
	if s.cagrildi("runuser") {
		t.Fatal("sudo başarılıyken runuser yedeği de çalıştı")
	}
}

func TestKiraciCalistirGercek_SudoBasarisizsaRunuserKiraciOlarak(t *testing.T) {
	s := sahteKomutOrtamiKur(t)
	s.basarisizYap(t, "sudo")
	o, e, err := kiraciCalistirGercek(testSK, s.cwd, []byte("GIRDI-VERISI\n"), kiraciKomutu...)
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v (%s)", err, e)
	}
	if s.cagrildi("kokcalisti") {
		t.Fatal("yedek yolda komut kiracı kimliğine düşürülmeden (root olarak) DOĞRUDAN çalıştırıldı")
	}
	if got, want := s.argv(t, "sudo"), append([]string{"-u", testSK, "-H", "--"}, kiraciKomutu...); !reflect.DeepEqual(got, want) {
		t.Fatalf("önce sudo denenmeli:\n got %q\nwant %q", got, want)
	}
	want := append([]string{"-u", testSK, "--"}, kiraciKomutu...)
	if got := s.argv(t, "runuser"); !reflect.DeepEqual(got, want) {
		t.Fatalf("runuser argv:\n got %q\nwant %q", got, want)
	}
	if got := s.dosya(t, "runuser", "stdin"); got != "GIRDI-VERISI\n" {
		t.Fatalf("yedek yolda stdin komuta verilmedi: %q", got)
	}
	if got := strings.TrimSpace(s.dosya(t, "runuser", "pwd")); got != s.cwd {
		t.Fatalf("yedek yolda cwd: got %q want %q", got, s.cwd)
	}
	ortamSatirlari := strings.Split(s.dosya(t, "runuser", "env"), "\n")
	for _, gerekli := range []string{
		"HOME=" + filepath.Join(evKoku, testSK),
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	} {
		bulundu := false
		for _, satir := range ortamSatirlari {
			if satir == gerekli {
				bulundu = true
			}
		}
		if !bulundu {
			t.Fatalf("runuser ortamında %q yok: %q", gerekli, ortamSatirlari)
		}
	}
	for _, satir := range ortamSatirlari {
		if strings.HasPrefix(satir, "GOSP_TEST_PANEL_SIRRI=") {
			t.Fatal("panel (root) ortamı kiracı komutuna sızdı")
		}
	}
	if o != "sahte-runuser-cikti\n" || e != "sahte-runuser-hata\n" {
		t.Fatalf("yedek yolda stdout/stderr: o=%q e=%q", o, e)
	}
}

func TestKiraciCalistirGercek_IkisiDeBasarisizsaRootOlarakCalistirmaz(t *testing.T) {
	s := sahteKomutOrtamiKur(t)
	s.basarisizYap(t, "sudo")
	s.basarisizYap(t, "runuser")
	_, e, err := kiraciCalistirGercek(testSK, s.cwd, []byte("GIRDI"), kiraciKomutu...)
	if err == nil {
		t.Fatal("sudo ve runuser başarısızken hata dönmeli")
	}
	if s.cagrildi("kokcalisti") {
		t.Fatal("kimlik düşürülemeyince komut root olarak DOĞRUDAN çalıştırıldı")
	}
	if got := s.argv(t, "sudo"); len(got) < 2 || got[0] != "-u" || got[1] != testSK {
		t.Fatalf("sudo kiracı kimliğiyle denenmedi: %q", got)
	}
	if got := s.argv(t, "runuser"); len(got) < 2 || got[0] != "-u" || got[1] != testSK {
		t.Fatalf("runuser kiracı kimliğiyle denenmedi: %q", got)
	}
	if !strings.Contains(e, "sahte runuser hata") {
		t.Fatalf("son denemenin hatası dönmeli: %q", e)
	}
}

// ---------- 2. katman: gerçek kimlik düşürme (root + nobody) ----------

// Birincil yol: gerçek sudo.
func TestKiraciCalistirGercek_GercekKimlikDusurme_Sudo(t *testing.T) {
	gercekKimlikDusurmeDenetle(t, false)
}

// Yedek yol: PATH başındaki sahte sudo başarısız olur → gerçek runuser çalışır.
func TestKiraciCalistirGercek_GercekKimlikDusurme_RunuserYedegi(t *testing.T) {
	gercekKimlikDusurmeDenetle(t, true)
}

func gercekKimlikDusurmeDenetle(t *testing.T, runuserYedegi bool) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("gerçek kimlik düşürme root gerektirir (181'de testler root koşar)")
	}
	u, err := user.Lookup("nobody")
	if err != nil {
		t.Skip("nobody kullanıcısı yok")
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	_, sErr := exec.LookPath("sudo")
	_, rErr := exec.LookPath("runuser")
	if sErr != nil && rErr != nil {
		t.Skip("sudo da runuser da yok")
	}
	if runuserYedegi {
		if rErr != nil {
			t.Skip("runuser yok")
		}
		bin := filepath.Join(t.TempDir(), "bin")
		mustMkdir(t, bin)
		isaret := filepath.Join(bin, "sudo-denendi")
		if err := os.WriteFile(filepath.Join(bin, "sudo"), []byte("#!/bin/sh\n: > '"+isaret+"'\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
		defer func() {
			if !t.Failed() && !varMi(isaret) {
				t.Fatal("sudo denenmedi → runuser yedeği sınanmadı")
			}
		}()
	}
	const kiraci = "nobody"

	base := t.TempDir()
	// t.TempDir'in üst dizini 0700 (MkdirTemp): nobody'nin yolu geçebilmesi için aç.
	for _, d := range []string{filepath.Dir(base), base} {
		if err := os.Chmod(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ev := filepath.Join(base, "ev") // kiracının kendi (yazabildiği) evi
	mustMkdir(t, ev)
	if err := os.Chown(ev, uid, gid); err != nil {
		t.Fatal(err)
	}
	kurban := filepath.Join(base, "kurban") // kök sahipli "komşu kiracı / sistem" ağacı
	mustMkdir(t, filepath.Join(kurban, "app"))
	gizli := filepath.Join(kurban, "app", "gizli.txt")
	if err := os.WriteFile(gizli, []byte("KOK-GIZLI"), 0o600); err != nil {
		t.Fatal(err)
	}
	yazilmaz := filepath.Join(kurban, "yazilmaz.txt")
	mustYaz(t, yazilmaz, "DOKUNULMAZ")

	// (1) Kimlik: komut nobody olarak çalışır — root DEĞİL.
	o, e, err := kiraciCalistirGercek(kiraci, ev, nil, "id", "-un")
	if err != nil {
		t.Fatalf("id -un kiracı olarak çalışmadı: %v (%s)", err, e)
	}
	if got := strings.TrimSpace(o); got != kiraci {
		t.Fatalf("komut %q kimliğiyle çalıştı, %q bekleniyordu", got, kiraci)
	}

	// (2) Meşru akış: stdin → tee kiracının kendi dosyasına ulaşır, dosya kiracıya aittir.
	cfg := filepath.Join(ev, "config")
	if _, e, err := kiraciCalistirGercek(kiraci, ev, []byte(sshConfigGovde), "tee", "--", cfg); err != nil {
		t.Fatalf("kiracı kendi evine yazamadı: %v (%s)", err, e)
	}
	if got := icerik(t, cfg); got != sshConfigGovde {
		t.Fatalf("stdin tee'ye ulaşmadı: %q", got)
	}
	fi, err := os.Stat(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != uid {
		t.Fatalf("dosya kiracı kimliğiyle oluşturulmadı (sahip uid=%v)", fi.Sys())
	}

	// (3) Meşru akış: gitClone'un temizlik komutu kiracının kendi hedefinde siler.
	kendi := filepath.Join(ev, "public_html")
	if _, e, err := kiraciCalistirGercek(kiraci, ev, nil, "mkdir", "-p", "--", filepath.Join(kendi, "eski")); err != nil {
		t.Fatalf("kiracı kendi evinde dizin oluşturamadı: %v (%s)", err, e)
	}
	if _, e, err := kiraciCalistirGercek(kiraci, ev, nil, "find", kendi, "-mindepth", "1", "-maxdepth", "1",
		"-exec", "rm", "-rf", "--", "{}", "+"); err != nil {
		t.Fatalf("kiracı kendi hedefini temizleyemedi: %v (%s)", err, e)
	}
	if varMi(filepath.Join(kendi, "eski")) {
		t.Fatal("meşru temizlik çalışmadı")
	}

	// Aşağıdaki symlink'ler komut ANINDA kurulu: Lstat denetiminden sonra yarışla kurulmuş
	// hali. Bariyer yalnız çekirdeğin erişimi kiracı kimliğinde değerlendirmesidir.

	// (4) Okuma: .pub → kök 0600 dosyası; içerik kiracıya (deploy_key_pub) dönmez.
	pubLink := filepath.Join(ev, "gospanel_deploy.pub")
	mustLink(t, gizli, pubLink)
	o, e, err = kiraciCalistirGercek(kiraci, ev, nil, pubOkuArgv(pubLink)...)
	if err == nil || strings.Contains(o+e, "KOK-GIZLI") {
		t.Fatalf("symlink üzerinden kök dosyası okundu: err=%v o=%q", err, o)
	}

	// (5) Yazma: ~/.ssh/config → kök dosyası; tee açamaz, içerik değişmez.
	cfgLink := filepath.Join(ev, "config-link")
	mustLink(t, yazilmaz, cfgLink)
	if _, _, err := kiraciCalistirGercek(kiraci, ev, []byte("SALDIRI\n"), "tee", "--", cfgLink); err == nil {
		t.Fatal("symlink üzerinden kök dosyasına yazma hata vermedi")
	}
	if got := icerik(t, yazilmaz); got != "DOKUNULMAZ" {
		t.Fatalf("kök dosyasının üzerine yazıldı: %q", got)
	}

	// (6) Silme: ara bileşen symlink (site → kök ağacı); temizlik kök dosyasını silemez.
	site := filepath.Join(ev, "site")
	mustLink(t, kurban, site)
	_, _, _ = kiraciCalistirGercek(kiraci, ev, nil, "find", filepath.Join(site, "app"), "-mindepth", "1", "-maxdepth", "1",
		"-exec", "rm", "-rf", "--", "{}", "+")
	_, _, _ = kiraciCalistirGercek(kiraci, ev, nil, "rm", "-f", "--", filepath.Join(site, "yazilmaz.txt"))
	if !varMi(gizli) || !varMi(yazilmaz) {
		t.Fatal("ara bileşen symlink'i üzerinden kök ağacındaki dosya silindi")
	}
}
