package git

// CWE-59 (link-following) gerileme testleri — Git dağıtımı.
//
// Tehdit: kiracı kendi evinde (FTP/SSH/PHP) symlink kurup kendi alan adının git
// uçlarını (Bagla/Klonla/Pull/Webhook) tetikler. Panel ROOT çalışır; eskiden hedef
// temizliği (ReadDir+RemoveAll), MkdirAll, chown, deploy key okuma ve ~/.ssh/config
// yazımı root olarak ve symlink izlenerek yapılıyordu → komşu kiracının/sistemin
// içeriği root olarak silinir, sahipliği kiracıya geçer, root-okunur dosya içeriği
// deploy_key_pub olarak kiracıya döner.
//
// Testler kiracı komutlarını ÇALIŞTIRMAYAN kaydedici bir sahte kullanır: böylece
// root tarafında (test süreci) hiçbir dosya işlemi yapılmadığı — silme, oluşturma,
// okuma, yazmanın yalnız kiracı kimliğine devredildiği — doğrudan ölçülür.

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const testSK = "c_deneme"

type sahteKiraci struct {
	cagrilar [][]string
	kimler   []string
	cikti    func(argv []string) (string, string, error)
}

type ortam struct {
	home, kurban string
	s            *sahteKiraci
	restorecon   *[]string
}

func ortamKur(t *testing.T) ortam {
	t.Helper()
	kok := t.TempDir()
	eskiKok, eskiKomut, eskiRc := evKoku, kiraciCalistir, restoreconCalistir
	t.Cleanup(func() { evKoku, kiraciCalistir, restoreconCalistir = eskiKok, eskiKomut, eskiRc })

	evKoku = filepath.Join(kok, "home")
	s := &sahteKiraci{}
	kiraciCalistir = func(sk, cwd string, stdin []byte, argv ...string) (string, string, error) {
		s.cagrilar = append(s.cagrilar, append([]string(nil), argv...))
		s.kimler = append(s.kimler, sk)
		if s.cikti != nil {
			return s.cikti(argv)
		}
		return "", "", nil
	}
	rc := []string{}
	restoreconCalistir = func(yol string) { rc = append(rc, yol) }

	o := ortam{
		home:       filepath.Join(evKoku, testSK),
		kurban:     filepath.Join(kok, "kurban"),
		s:          s,
		restorecon: &rc,
	}
	mustMkdir(t, o.home)
	mustMkdir(t, o.kurban)
	return o
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustYaz(t *testing.T, p, icerik string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(icerik), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustLink(t *testing.T, hedef, link string) {
	t.Helper()
	if err := os.Symlink(hedef, link); err != nil {
		t.Fatal(err)
	}
}

func icerik(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%s okunamadı (silinmiş olabilir): %v", p, err)
	}
	return string(b)
}

func varMi(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// kiraciCagrisiYok: reddedilen bir istekte kiracı adına hiçbir komut çalışmamalı.
func (o ortam) kiraciCagrisiYok(t *testing.T) {
	t.Helper()
	if len(o.s.cagrilar) != 0 {
		t.Fatalf("symlink'li yolda reddetmeden komut çalıştırıldı: %q", o.s.cagrilar)
	}
	if len(*o.restorecon) != 0 {
		t.Fatalf("symlink'li yolda root restorecon çalıştı: %q", *o.restorecon)
	}
}

// kiraciCagrisiVar: argv tam eşleşen bir kiracı çağrısı olmalı ve TÜM çağrılar sk ile.
func (o ortam) kiraciCagrisiVar(t *testing.T, argv ...string) {
	t.Helper()
	for _, k := range o.s.kimler {
		if k != testSK {
			t.Fatalf("komut kiracı dışı kimlikle çalıştı: %q", k)
		}
	}
	for _, c := range o.s.cagrilar {
		if reflect.DeepEqual(c, argv) {
			return
		}
	}
	t.Fatalf("beklenen kiracı komutu yok: %q\nçağrılar: %q", argv, o.s.cagrilar)
}

// ---------- target_dir biçim doğrulaması ----------

func TestGecerliTargetDir_EvKokuVeGizliDizinReddedilir(t *testing.T) {
	kabul := []string{"public_html", "public_html/app", "app", "public_html/", "a-b_c.d/e", "public_html/v1.2"}
	for _, td := range kabul {
		if !gecerliTargetDir(td) {
			t.Errorf("meşru hedef reddedildi: %q", td)
		}
	}
	ret := []string{
		".", "./", "a/./b", "a//b", ".ssh", ".ssh/", "public_html/.git", ".config/x",
		"..", "a/../b", "a/..", "/etc", "/", "", "   ", "a b", "a;b", "a$b",
		strings.Repeat("a", 129),
	}
	for _, td := range ret {
		if gecerliTargetDir(td) {
			t.Errorf("tehlikeli hedef kabul edildi: %q", td)
		}
	}
}

func TestGecerliSK(t *testing.T) {
	for _, sk := range []string{"c_girgin", "c_site2", "c_a-b"} {
		if !gecerliSK(sk) {
			t.Errorf("meşru sk reddedildi: %q", sk)
		}
	}
	for _, sk := range []string{"", "../etc", "c_a/b", "..", "C_X", "c x", strings.Repeat("a", 40)} {
		if gecerliSK(sk) {
			t.Errorf("tehlikeli sk kabul edildi: %q", sk)
		}
	}
}

// ---------- Klonla (gitClone) ----------

// Saldırı: public_html → komşu kiracının/sistemin dizini. Eskiden root içeriği siliyordu.
func TestGitClone_SymlinkHedefKurbaniSilmez(t *testing.T) {
	o := ortamKur(t)
	mustYaz(t, filepath.Join(o.kurban, "gizli.txt"), "KURBAN")
	mustLink(t, o.kurban, filepath.Join(o.home, "public_html"))

	_, _, err := gitClone(testSK, "https://example.com/r.git", "main", "public_html")
	if icerik(t, filepath.Join(o.kurban, "gizli.txt")) != "KURBAN" {
		t.Fatal("kurban dosyası değişti")
	}
	if err == nil {
		t.Fatal("symlink hedefli klonlama reddedilmeli")
	}
	o.kiraciCagrisiYok(t)
}

// Saldırı: ara bileşen symlink'i (site → kurban, target_dir = site/app).
func TestGitClone_AraBilesenSymlinkKurbaniSilmez(t *testing.T) {
	o := ortamKur(t)
	mustMkdir(t, filepath.Join(o.kurban, "app"))
	mustYaz(t, filepath.Join(o.kurban, "app", "gizli.txt"), "KURBAN")
	mustLink(t, o.kurban, filepath.Join(o.home, "site"))

	_, _, err := gitClone(testSK, "https://example.com/r.git", "main", "site/app")
	if icerik(t, filepath.Join(o.kurban, "app", "gizli.txt")) != "KURBAN" {
		t.Fatal("kurban dosyası değişti")
	}
	if err == nil {
		t.Fatal("ara bileşeni symlink olan hedef reddedilmeli")
	}
	o.kiraciCagrisiYok(t)
}

// Saldırı: ev dizininin kendisi symlink (yönetici hatası/eski kurulum) — izlenmemeli.
func TestGitClone_EvDiziniSymlinkReddedilir(t *testing.T) {
	o := ortamKur(t)
	if err := os.Remove(o.home); err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, filepath.Join(o.kurban, "public_html"))
	mustYaz(t, filepath.Join(o.kurban, "public_html", "gizli.txt"), "KURBAN")
	mustLink(t, o.kurban, o.home)

	if _, _, err := gitClone(testSK, "https://example.com/r.git", "main", "public_html"); err == nil {
		t.Fatal("symlink ev dizini reddedilmeli")
	}
	if icerik(t, filepath.Join(o.kurban, "public_html", "gizli.txt")) != "KURBAN" {
		t.Fatal("kurban dosyası değişti")
	}
	o.kiraciCagrisiYok(t)
}

// target_dir "." → ev kökünün tamamı silinirdi. Artık hiç başlamamalı.
func TestGitClone_EvKokuHedefOlamaz(t *testing.T) {
	o := ortamKur(t)
	mustYaz(t, filepath.Join(o.home, ".bashrc"), "EV")
	for _, td := range []string{".", "./", ".ssh"} {
		if _, _, err := gitClone(testSK, "https://example.com/r.git", "main", td); err == nil {
			t.Fatalf("hedef %q reddedilmeli", td)
		}
	}
	if icerik(t, filepath.Join(o.home, ".bashrc")) != "EV" {
		t.Fatal("ev dosyası değişti")
	}
	o.kiraciCagrisiYok(t)
}

// sk DB'den gelir; yine de "/" ya da "." içeren bir ad ev yolunu /home dışına taşımamalı.
// kurban/public_html VAR: denetim olmasa işlem "../kurban" üzerinden sürerdi.
func TestGecersizSK_HicbirGirisNoktasindaIslemYapmaz(t *testing.T) {
	o := ortamKur(t)
	mustMkdir(t, filepath.Join(o.kurban, "public_html", ".git"))
	if _, _, err := gitClone("../kurban", "https://example.com/r.git", "main", "public_html"); err == nil {
		t.Fatal("gitClone: geçersiz sk reddedilmeli")
	}
	if _, _, err := gitPull("../kurban", "public_html", "main"); err == nil {
		t.Fatal("gitPull: geçersiz sk reddedilmeli")
	}
	if _, err := generateDeployKey("../kurban"); err == nil {
		t.Fatal("generateDeployKey: geçersiz sk reddedilmeli")
	}
	o.kiraciCagrisiYok(t)
}

// Ara bileşen normal dosya (ENOTDIR): Lstat hatası yutulmamalı.
func TestGitClone_AraBilesenDosyaReddedilir(t *testing.T) {
	o := ortamKur(t)
	mustYaz(t, filepath.Join(o.home, "dosya"), "x")
	if _, _, err := gitClone(testSK, "https://example.com/r.git", "main", "dosya/alt"); err == nil {
		t.Fatal("ara bileşeni dosya olan hedef reddedilmeli")
	}
	o.kiraciCagrisiYok(t)
}

// Meşru akış: temizlik ve oluşturma KİRACI kimliğine devredilir; root tarafında hiçbir
// dosya silinmez/oluşturulmaz (sahte kiracı komutu çalıştırmaz → dosyalar yerinde kalır).
func TestGitClone_MesruHedefIslemleriKiraciyaDevredilir(t *testing.T) {
	o := ortamKur(t)
	ph := filepath.Join(o.home, "public_html")
	mustMkdir(t, ph)
	mustYaz(t, filepath.Join(ph, "eski.html"), "ESKI")

	if _, _, err := gitClone(testSK, "https://example.com/r.git", "main", "public_html/"); err != nil {
		t.Fatalf("meşru klonlama hata verdi: %v", err)
	}
	if icerik(t, filepath.Join(ph, "eski.html")) != "ESKI" {
		t.Fatal("hedef içeriği ROOT olarak silindi; silme kiracıya devredilmeliydi")
	}
	o.kiraciCagrisiVar(t, "mkdir", "-p", "--", ph)
	o.kiraciCagrisiVar(t, "find", ph, "-mindepth", "1", "-maxdepth", "1", "-exec", "rm", "-rf", "--", "{}", "+")
	o.kiraciCagrisiVar(t, "git", "clone", "--depth", "1", "--branch", "main", "--", "https://example.com/r.git", ph)
	if !reflect.DeepEqual(*o.restorecon, []string{ph}) {
		t.Fatalf("restorecon yalnız hedefte bir kez çalışmalı: %q", *o.restorecon)
	}
}

// Meşru akış: henüz olmayan iç içe hedef root tarafından OLUŞTURULMAZ (root MkdirAll
// ara dizinleri root sahipli bırakıyordu); kiracı mkdir -p yapar.
func TestGitClone_YeniHedefKokTarafindanOlusturulmaz(t *testing.T) {
	o := ortamKur(t)
	mustMkdir(t, filepath.Join(o.home, "public_html"))
	yeni := filepath.Join(o.home, "public_html", "yeni", "alt")

	if _, _, err := gitClone(testSK, "https://example.com/r.git", "main", "public_html/yeni/alt"); err != nil {
		t.Fatalf("meşru klonlama hata verdi: %v", err)
	}
	if varMi(filepath.Join(o.home, "public_html", "yeni")) {
		t.Fatal("hedef dizin ROOT olarak oluşturuldu; kiracıya devredilmeliydi")
	}
	o.kiraciCagrisiVar(t, "mkdir", "-p", "--", yeni)
}

// Kiracı mkdir/temizliği başarısızsa (ör. root sahipli içerik) klonlama sessizce sürmemeli.
func TestGitClone_KiraciKomutuBasarisizsaDurur(t *testing.T) {
	for _, bozuk := range []string{"mkdir", "find"} {
		t.Run(bozuk, func(t *testing.T) {
			o := ortamKur(t)
			mustMkdir(t, filepath.Join(o.home, "public_html"))
			o.s.cikti = func(argv []string) (string, string, error) {
				if argv[0] == bozuk {
					return "", "izin reddedildi", errors.New("exit status 1")
				}
				return "", "", nil
			}
			if _, _, err := gitClone(testSK, "https://example.com/r.git", "main", "public_html"); err == nil {
				t.Fatalf("%s hatası yutulmamalı", bozuk)
			}
			for _, c := range o.s.cagrilar {
				if c[0] == "git" {
					t.Fatalf("%s başarısızken git çalıştı: %q", bozuk, c)
				}
			}
		})
	}
}

// ---------- Pull / Webhook (gitPull) ----------

func TestGitPull_SymlinkHedefReddedilir(t *testing.T) {
	o := ortamKur(t)
	mustMkdir(t, filepath.Join(o.kurban, ".git"))
	mustLink(t, o.kurban, filepath.Join(o.home, "public_html"))

	if _, _, err := gitPull(testSK, "public_html", "main"); err == nil {
		t.Fatal("symlink hedefli pull reddedilmeli")
	}
	o.kiraciCagrisiYok(t)
}

// .git varlığı root os.Stat ile (symlink izleyerek) değil, kiracı kimliğinde denetlenir.
func TestGitPull_GitVarligiKiraciOlarakDenetlenir(t *testing.T) {
	o := ortamKur(t)
	ph := filepath.Join(o.home, "public_html")
	mustMkdir(t, ph) // root görünümünde .git YOK — karar kiracı komutuna ait

	if _, _, err := gitPull(testSK, "public_html", "main"); err != nil {
		t.Fatalf("kiracı .git'i doğruladığında pull sürmeli: %v", err)
	}
	o.kiraciCagrisiVar(t, "test", "-d", filepath.Join(ph, ".git"))
	o.kiraciCagrisiVar(t, "git", "-C", ph, "fetch", "origin", "main")

	// Kiracı .git yok derse pull durur.
	o2 := ortamKur(t)
	mustMkdir(t, filepath.Join(o2.home, "public_html"))
	o2.s.cikti = func(argv []string) (string, string, error) {
		if argv[0] == "test" {
			return "", "", errors.New("exit status 1")
		}
		return "", "", nil
	}
	if _, _, err := gitPull(testSK, "public_html", "main"); err == nil {
		t.Fatal(".git yokken pull reddedilmeli")
	}
}

func TestGitPull_EvKokuHedefOlamaz(t *testing.T) {
	o := ortamKur(t)
	mustMkdir(t, filepath.Join(o.home, ".git"))
	if _, _, err := gitPull(testSK, ".", "main"); err == nil {
		t.Fatal("ev kökünde pull reddedilmeli")
	}
	o.kiraciCagrisiYok(t)
}

// ---------- Deploy key (generateDeployKey / Bagla) ----------

const gecerliPub = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGb7cX5h2kQ0bZ4yJwq9n8Vt3mWc1xE6YpLr0sTuD4aB deploy@girginospanel/c_deneme"

// Saldırı: ~/.ssh → kurban dizini; root .pub'ı okuyup içeriği kiracıya döndürüyordu.
func TestDeployKey_SshSymlinkIcerikSizdirmaz(t *testing.T) {
	o := ortamKur(t)
	mustYaz(t, filepath.Join(o.kurban, "gospanel_deploy.pub"), "GIZLI-KOK-ICERIGI")
	mustLink(t, o.kurban, filepath.Join(o.home, ".ssh"))

	pub, err := generateDeployKey(testSK)
	if err == nil || strings.Contains(pub, "GIZLI") {
		t.Fatalf("symlink .ssh reddedilmeli; pub=%q err=%v", pub, err)
	}
	o.kiraciCagrisiYok(t)
}

// Saldırı: ~/.ssh/gospanel_deploy.pub → kurban dosyası (yaprak symlink).
func TestDeployKey_PubSymlinkIcerikSizdirmaz(t *testing.T) {
	o := ortamKur(t)
	mustMkdir(t, filepath.Join(o.home, ".ssh"))
	mustYaz(t, filepath.Join(o.kurban, "sir.txt"), "GIZLI-KOK-ICERIGI")
	mustLink(t, filepath.Join(o.kurban, "sir.txt"), filepath.Join(o.home, ".ssh", "gospanel_deploy.pub"))

	pub, err := generateDeployKey(testSK)
	if err == nil || strings.Contains(pub, "GIZLI") {
		t.Fatalf("symlink .pub reddedilmeli; pub=%q err=%v", pub, err)
	}
	o.kiraciCagrisiYok(t)
}

// Saldırı: ~/.ssh/config → kurban dosyası; root üzerine yazıp sahipliği kiracıya veriyordu.
func TestDeployKey_ConfigSymlinkUzerineYazmaz(t *testing.T) {
	o := ortamKur(t)
	mustMkdir(t, filepath.Join(o.home, ".ssh"))
	hedef := filepath.Join(o.kurban, "hedef.conf")
	mustYaz(t, hedef, "ORIJINAL")
	mustLink(t, hedef, filepath.Join(o.home, ".ssh", "config"))

	_, err := generateDeployKey(testSK)
	if icerik(t, hedef) != "ORIJINAL" {
		t.Fatal("kurban dosyasının üzerine yazıldı")
	}
	if err == nil {
		t.Fatal("symlink config reddedilmeli")
	}
	o.kiraciCagrisiYok(t)
}

// Saldırı: ~/.ssh/gospanel_deploy (özel anahtar yolu) → kurban dosyası.
func TestDeployKey_OzelAnahtarSymlinkReddedilir(t *testing.T) {
	o := ortamKur(t)
	mustMkdir(t, filepath.Join(o.home, ".ssh"))
	hedef := filepath.Join(o.kurban, "shadow")
	mustYaz(t, hedef, "ORIJINAL")
	mustLink(t, hedef, filepath.Join(o.home, ".ssh", "gospanel_deploy"))

	if _, err := generateDeployKey(testSK); err == nil {
		t.Fatal("symlink özel anahtar yolu reddedilmeli")
	}
	if icerik(t, hedef) != "ORIJINAL" {
		t.Fatal("kurban dosyası değişti")
	}
	o.kiraciCagrisiYok(t)
}

// Meşru akış: tüm dosya işlemleri kiracı kimliğinde; root .ssh'ı oluşturmaz/yazmaz.
func TestDeployKey_KiraciOlarakUretilir(t *testing.T) {
	o := ortamKur(t)
	uretildi := false
	o.s.cikti = func(argv []string) (string, string, error) {
		switch argv[0] {
		case "ssh-keygen":
			uretildi = true
		case "head":
			if !uretildi {
				return "", "head: yok", errors.New("exit status 1")
			}
			return gecerliPub + "\n", "", nil
		}
		return "", "", nil
	}
	pub, err := generateDeployKey(testSK)
	if err != nil || pub != gecerliPub {
		t.Fatalf("meşru anahtar üretimi: pub=%q err=%v", pub, err)
	}
	dir := filepath.Join(o.home, ".ssh")
	if varMi(dir) {
		t.Fatal("~/.ssh ROOT olarak oluşturuldu; kiracıya devredilmeliydi")
	}
	o.kiraciCagrisiVar(t, "mkdir", "-p", "-m", "0700", "--", dir)
	o.kiraciCagrisiVar(t, "rm", "-f", "--", filepath.Join(dir, "gospanel_deploy"), filepath.Join(dir, "gospanel_deploy.pub"))
	o.kiraciCagrisiVar(t, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "deploy@girginospanel/"+testSK,
		"-f", filepath.Join(dir, "gospanel_deploy"))
	o.kiraciCagrisiVar(t, "tee", "--", filepath.Join(dir, "config"))
	// Okuma boyut sınırlı (yol yarışla /dev/zero'ya çevrilse bile bellek şişmez).
	o.kiraciCagrisiVar(t, "head", "-c", "4096", "--", filepath.Join(dir, "gospanel_deploy.pub"))
	if !reflect.DeepEqual(*o.restorecon, []string{dir}) {
		t.Fatalf("restorecon yalnız ~/.ssh'ta bir kez çalışmalı: %q", *o.restorecon)
	}
}

// Kiracı adımlarından biri başarısızsa anahtar üretimi başarılı sayılmamalı. Sahte cat,
// ssh-keygen ÇAĞRILDIKTAN sonra geçerli anahtar döndürür: hata yutulursa sonuç "başarılı"
// görünürdü — test bunu yakalar.
func TestDeployKey_KiraciKomutuBasarisizsaDurur(t *testing.T) {
	for _, bozuk := range []string{"mkdir", "rm", "ssh-keygen", "tee"} {
		t.Run(bozuk, func(t *testing.T) {
			o := ortamKur(t)
			uretildi := false
			o.s.cikti = func(argv []string) (string, string, error) {
				if argv[0] == "ssh-keygen" {
					uretildi = true
				}
				if argv[0] == bozuk {
					return "", "izin reddedildi", errors.New("exit status 1")
				}
				if argv[0] == "head" {
					if !uretildi {
						return "", "head: yok", errors.New("exit status 1")
					}
					return gecerliPub + "\n", "", nil
				}
				return "", "", nil
			}
			if pub, err := generateDeployKey(testSK); err == nil {
				t.Fatalf("%s hatası yutuldu; pub=%q", bozuk, pub)
			}
		})
	}
}

// Mevcut geçerli anahtar yeniden kullanılır (yeniden üretilmez).
func TestDeployKey_MevcutGecerliAnahtarKullanilir(t *testing.T) {
	o := ortamKur(t)
	o.s.cikti = func(argv []string) (string, string, error) {
		if argv[0] == "head" {
			return gecerliPub + "\n", "", nil
		}
		return "", "", nil
	}
	pub, err := generateDeployKey(testSK)
	if err != nil || pub != gecerliPub {
		t.Fatalf("pub=%q err=%v", pub, err)
	}
	for _, c := range o.s.cagrilar {
		if c[0] == "ssh-keygen" || c[0] == "rm" || c[0] == "tee" {
			t.Fatalf("geçerli anahtar varken yeniden üretildi: %q", c)
		}
	}
}

// .pub içeriği ssh-ed25519 biçiminde değilse ASLA yanıta dönmez.
func TestDeployKey_GecersizPubIcerigiDondurulmez(t *testing.T) {
	o := ortamKur(t)
	o.s.cikti = func(argv []string) (string, string, error) {
		if argv[0] == "head" {
			return "root:x:0:0:root:/root:/bin/bash\n", "", nil
		}
		return "", "", nil
	}
	pub, err := generateDeployKey(testSK)
	if err == nil || strings.Contains(pub, "root:") {
		t.Fatalf("geçersiz .pub içeriği döndü: pub=%q err=%v", pub, err)
	}
}

func TestDeployKey_GecersizSKReddedilir(t *testing.T) {
	o := ortamKur(t)
	if _, err := generateDeployKey("../kurban"); err == nil {
		t.Fatal("geçersiz sk reddedilmeli")
	}
	o.kiraciCagrisiYok(t)
}

// ---------- restorecon (root'ta kalan tek adım) ----------

func TestRestoreconKiraci_SymlinkYoldaCalismaz(t *testing.T) {
	o := ortamKur(t)
	mustLink(t, o.kurban, filepath.Join(o.home, "public_html"))
	restoreconKiraci(o.home, "public_html")
	if len(*o.restorecon) != 0 {
		t.Fatalf("symlink yolda root restorecon çalıştı: %q", *o.restorecon)
	}
	mustMkdir(t, filepath.Join(o.home, "gercek"))
	restoreconKiraci(o.home, "gercek")
	if !reflect.DeepEqual(*o.restorecon, []string{filepath.Join(o.home, "gercek")}) {
		t.Fatalf("meşru yolda restorecon çalışmalı: %q", *o.restorecon)
	}
}
