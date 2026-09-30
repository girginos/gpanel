package tasima

// aktar_dosya_rsync_linux_test.go — dosya adımının UÇTAN UCA sınaması, GERÇEK uzak
// rsync yoluyla (Kaynak.kabuk == nil → RsyncCek'in sshpass + rsync -e ssh komutu ve
// S paketinin --no-D --no-o --no-g bayrakları): RsyncCek → hazırlık sarmalayıcısı/d →
// devir → kiracı kimliğinde yerleşim → Laravel düzeltmesi.
//
// aktar_dosya_test.go'daki sahte uzak (yrUzak) rsync'i S öncesi en kötü bayraklarla
// taklit eder; bu test aynı zinciri ÜRETİMDEKİ komutla koşar. Ağa çıkılmaz: ssh/sshpass
// taklitleri rsync_ozel_linux_test.go'daki sahteSSH düzeneğidir. Kiracı "nobody",
// her şey t.TempDir içinde; root değilse atlanır.

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// yrDevir — bir hazırlık sarmalayıcısının, kök rsync bittikten sonra ve kiracıya
// devredilmeden hemen önceki durumu.
type yrDevir struct {
	sarm     yrSahip
	yabanci  []string // içerikte kökten başka sahipli girdiler
	ozelSayi int      // içerikteki aygıt/fifo/soket
}

func TestDosyalariAktar_GercekRsyncUctanUca(t *testing.T) {
	o := yrYeniOrtam(t)
	sshArgv, rsyncArgv := sahteSSH(t)

	// Sahte kaynak sunucu: belge kökü (docroot dışı bağımlılık, ağaç içi bağlar,
	// FIFO, aygıt, setuid) + belge kökü dışındaki Laravel uygulaması; HEPSİ yabancı uid.
	srv := t.TempDir()
	docroot := filepath.Join(srv, "httpdocs")
	yrKaynakDocroot(t, docroot)
	yrKaynakUygulama(t, filepath.Join(srv, "uygulama"))
	yrYaz(t, filepath.Join(docroot, "suid"), "#!/bin/sh\n", 0o755)
	if err := unix.Chmod(filepath.Join(docroot, "suid"), 0o4755); err != nil {
		t.Fatal(err)
	}
	if err := filepath.WalkDir(srv, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Lchown(p, yrYabanciUID, yrYabanciUID)
	}); err != nil {
		t.Fatal(err)
	}

	ph := filepath.Join(o.ev, "public_html")
	o.dizin(t, ph)

	// Devir anı: hazirlikDevret içeriğin kökünü (<sarmalayıcı>/d) ilk sırada devreder.
	var devirler []yrDevir
	onceki := lchown
	lchown = func(p string, u, g int) error {
		if filepath.Base(p) == hazirlikIcerik && filepath.Dir(filepath.Dir(p)) == hazirlikKoku {
			d := yrDevir{sarm: yrSahipAl(filepath.Dir(p))}
			_ = filepath.WalkDir(p, func(q string, _ fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				fi, err := os.Lstat(q)
				if err != nil {
					return err
				}
				if fi.Sys().(*syscall.Stat_t).Uid != 0 {
					d.yabanci = append(d.yabanci, q)
				}
				if fi.Mode()&(fs.ModeDevice|fs.ModeNamedPipe|fs.ModeSocket) != 0 {
					d.ozelSayi++
				}
				return nil
			})
			devirler = append(devirler, d)
		}
		return onceki(p, u, g)
	}
	t.Cleanup(func() { lchown = onceki })

	k := &Kaynak{Tip: "plesk", Host: sahteSSHHost, Port: 22, Kullanici: "root", Parola: "taklit-parola"}
	ctx, iptal := context.WithTimeout(context.Background(), 2*time.Minute)
	defer iptal()
	sonuc := &AktarSonuc{}
	if err := (&Handlers{}).dosyalariAktar(ctx, k, Hesap{WebRoot: docroot}, Ayarlar{Dosyalar: true},
		yrSK, ph, yrAlan, sonuc, yrLog(t)); err != nil {
		t.Fatalf("dosyalariAktar: %v", err)
	}

	// 1. Uzak rsync GERÇEKTEN ssh üzerinden, üretim bayraklarıyla ve yalnız bu işin
	//    sarmalayıcısının içine (…/<sk>-<zaman>-<rastgele>/d/) koştu: belge kökü + uygulama.
	sahteSSHUzakKomut(t, sshArgv)
	b, err := os.ReadFile(rsyncArgv)
	if err != nil {
		t.Fatalf("sshpass taklidi çağrılmadı: %v", err)
	}
	cagrilar := strings.Split(strings.TrimSuffix(string(b), "----\n"), "----\n")
	if len(cagrilar) != 2 {
		t.Fatalf("uzak rsync çağrı sayısı %d, beklenen 2:\n%s", len(cagrilar), b)
	}
	for _, c := range cagrilar {
		argv := strings.Split(strings.TrimSuffix(c, "\n"), "\n")
		for _, bayrak := range []string{"--no-D", "--no-o", "--no-g", "--no-perms", "--safe-links"} {
			if !strings.Contains("\n"+c, "\n"+bayrak+"\n") {
				t.Errorf("uzak rsync'te %s yok: %q", bayrak, argv)
			}
		}
		hedef := argv[len(argv)-1]
		sarm := filepath.Dir(strings.TrimSuffix(hedef, "/"))
		if filepath.Dir(sarm) != hazirlikKoku || !strings.HasPrefix(filepath.Base(sarm), yrSK+"-") ||
			hedef != filepath.Join(sarm, hazirlikIcerik)+"/" {
			t.Errorf("uzak rsync hedefi hazırlık sarmalayıcısının içi değil: %q", hedef)
		}
	}

	// 2. Devir anında sarmalayıcı hâlâ yalnız kökün (0700) ve içerikte kaynağın
	//    uid'i ile özel dosya YOK (S: --no-o/--no-g/--no-D, kaynakta kesildi).
	if len(devirler) != 2 {
		t.Fatalf("devir sayısı %d, beklenen 2", len(devirler))
	}
	for i, d := range devirler {
		if d.sarm.mod != fs.ModeDir|0o700 || d.sarm.uid != 0 {
			t.Errorf("devir %d: sarmalayıcı kök/0700 değil: %+v", i, d.sarm)
		}
		if len(d.yabanci) != 0 || d.ozelSayi != 0 {
			t.Errorf("devir %d: hazırlıkta yabancı sahip %q / özel dosya %d", i, d.yabanci, d.ozelSayi)
		}
	}
	yrHazirlikBos(t)

	// 3. Kiracı ağacı: meşru içerik yerleşti; her girdi kiracının, yalnız dizin/dosya/
	//    bağ; özel bit yok; ağaç içi bağlar korundu.
	agac := yrOzet(t, o.ev)
	for _, p := range []string{"public_html/index.php", "public_html/alt/dosya.txt", "public_html/suid",
		"uygulama/artisan", "uygulama/storage/logs/a.log"} {
		if g, ok := agac[p]; !ok || g.Tur != 0 {
			t.Errorf("%s düzenli dosya olarak yerleşmedi: %+v", p, g)
		}
	}
	for p, g := range agac {
		if g.UID != uint32(o.uid) || g.GID != uint32(o.gid) {
			t.Errorf("%s kiracıya ait değil: %d:%d", p, g.UID, g.GID)
		}
		if g.Tur != 0 && g.Tur != fs.ModeDir && g.Tur != fs.ModeSymlink {
			t.Errorf("özel dosya kiracı ağacına taşındı: %s (%v)", p, g.Tur)
		}
		if g.Mod&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) != 0 || g.Mod&0o022 != 0 && g.Tur != fs.ModeSymlink {
			t.Errorf("%s izni süzülmedi: %v", p, g.Mod)
		}
	}
	for bag, hedef := range map[string]string{"public_html/bag": "alt/dosya.txt", "public_html/dizinbag": "alt"} {
		if g := agac[bag]; g.Tur != fs.ModeSymlink || g.Icerik != hedef {
			t.Errorf("ağaç içi bağ korunmadı: %s %+v", bag, g)
		}
	}

	// 4. Laravel düzeltmesi zincirin sonunda çalıştı.
	env, err := os.ReadFile(filepath.Join(o.ev, "uygulama", ".env"))
	if err != nil || !strings.Contains(string(env), "APP_URL=https://"+yrAlan+"\n") {
		t.Errorf("APP_URL güncellenmedi: %q (%v)", env, err)
	}
	if g := agac["uygulama/.env"]; g.Mod != 0o600 {
		t.Errorf(".env izni %v, beklenen 0600", g.Mod)
	}
	if _, err := os.Lstat(filepath.Join(o.ev, "uygulama", "bootstrap", "cache", "config.php")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Laravel önbelleği silinmedi: %v", err)
	}
	if sonuc.DosyaBayt <= 0 {
		t.Errorf("DosyaBayt = %d", sonuc.DosyaBayt)
	}
}
