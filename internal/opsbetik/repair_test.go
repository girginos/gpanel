package opsbetik

// girginospanel-repair kiracı ağacı gerileme testleri.
//
// Saldırı: kiracı kendi evinde ~/.ssh, ~/public_html ya da ~/.karantina'yı ev dışını
// gösteren bir symlink'e çevirir (ya da root'a ait bir dosyayı sabit bağlar, ya da
// root'a ait .karantina'yı başka ada taşır). Yönetici/kurulum `girginospanel-repair`
// koştuğunda root, symlink'in hedefini chmod/chown ediyordu (chmod 700 / → DoS,
// chmod 755 /root → ifşa, .karantina -> başka kiracı → root:root 700).
//
// CANLI SİSTEME DOKUNMAZ: yalnız domain_evi_onar fonksiyonu t.TempDir altındaki sahte
// evlerde koşulur (SEL=0: restorecon yok). Sahiplik denetimi için root gerekir.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

const (
	kiraciUID = 65534 // nobody
	yabanciID = 54321
)

func betikYolu(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs("../../assets/ops/girginospanel-repair")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func rootGerek(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("chown/sahiplik davranışı için root gerekir")
	}
	if out, err := exec.Command("getent", "passwd", "nobody").Output(); err != nil || !strings.Contains(string(out), ":65534:") {
		t.Skip("nobody (65534) kullanıcısı yok")
	}
}

// onar: betiği kütüphane olarak yükleyip domain_evi_onar'ı koşar.
func onar(t *testing.T, ev string, deep bool) string {
	t.Helper()
	d := "0"
	if deep {
		d = "1"
	}
	// set --: source konumsal argümanları betiğe geçirir; betik bunları kendi
	// seçenekleri sanmasın.
	betik := `f=$1 e=$2 d=$3; set --; source "$f" || exit 90; SEL=0; DEEP=$d; domain_evi_onar "$e" nobody; exit 0`
	cmd := exec.Command("bash", "-c", betik, "bash", betikYolu(t), ev, d)
	cmd.Env = append(os.Environ(), "GOSP_REPAIR_KUTUPHANE=1", "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("betik hatası (%v): %s", err, out)
	}
	return string(out)
}

type durum struct {
	uid, gid uint32
	mod      os.FileMode
}

func oku(t *testing.T, p string) durum {
	t.Helper()
	st, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	s := st.Sys().(*syscall.Stat_t)
	return durum{s.Uid, s.Gid, st.Mode()}
}

func mkdir(t *testing.T, p string, mod os.FileMode, uid int) {
	t.Helper()
	if err := os.Mkdir(p, mod); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mod); err != nil { // umask'tan bağımsız
		t.Fatal(err)
	}
	if err := os.Chown(p, uid, uid); err != nil {
		t.Fatal(err)
	}
}

func yaz(t *testing.T, p string, mod os.FileMode, uid int) {
	t.Helper()
	if err := os.WriteFile(p, []byte("veri\n"), mod); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mod); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(p, uid, uid); err != nil {
		t.Fatal(err)
	}
}

func symlink(t *testing.T, hedef, ad string) {
	t.Helper()
	if err := os.Symlink(hedef, ad); err != nil {
		t.Fatal(err)
	}
	if err := os.Lchown(ad, kiraciUID, kiraciUID); err != nil {
		t.Fatal(err)
	}
}

// evKur: kök/c_x kiracıya ait 0711 ev dizini.
func evKur(t *testing.T) (kok, ev string) {
	t.Helper()
	kok = t.TempDir()
	if err := os.Chmod(kok, 0o755); err != nil {
		t.Fatal(err)
	}
	ev = filepath.Join(kok, "c_x")
	mkdir(t, ev, 0o711, kiraciUID)
	return kok, ev
}

func TestDomainEviOnar_SymlinkHedefiDegismez(t *testing.T) {
	rootGerek(t)
	for _, deep := range []bool{false, true} {
		kok, ev := evKur(t)
		dis := t.TempDir()                          // kiracı evi DIŞI (root'un alanı)
		kurbanSSH := filepath.Join(dis, "sistem")   // ~/.ssh -> (beklenen 700 yapılırdı)
		kurbanPub := filepath.Join(dis, "rootev")   // ~/public_html -> (755 yapılırdı)
		kurbanKar := filepath.Join(kok, "c_kurban") // ~/.karantina -> başka kiracı (root:root 700 yapılırdı)
		kurbanDos := filepath.Join(dis, "shadow")   // sabit bağ (kiracıya chown edilirdi)
		mkdir(t, kurbanSSH, 0o755, 0)
		mkdir(t, kurbanPub, 0o700, 0)
		mkdir(t, kurbanKar, 0o751, yabanciID)
		yaz(t, kurbanDos, 0o600, 0)
		symlink(t, kurbanSSH, filepath.Join(ev, ".ssh"))
		symlink(t, kurbanPub, filepath.Join(ev, "public_html"))
		symlink(t, kurbanKar, filepath.Join(ev, ".karantina"))
		if err := os.Link(kurbanDos, filepath.Join(ev, "shadow-bag")); err != nil {
			t.Fatal(err)
		}
		// Taşınmış karantina: root:root 0700, içinde 0000 kiracı dosyası.
		eskiKar := filepath.Join(ev, "eski")
		mkdir(t, eskiKar, 0o700, 0)
		yaz(t, filepath.Join(eskiKar, "shell.php"), 0, kiraciUID)

		once := map[string]durum{}
		for _, p := range []string{kurbanSSH, kurbanPub, kurbanKar, kurbanDos, eskiKar} {
			once[p] = oku(t, p)
		}
		cikti := onar(t, ev, deep)
		for p, o := range once {
			if s := oku(t, p); s != o {
				t.Fatalf("deep=%v: KURBAN DEĞİŞTİ %s: önce %+v sonra %+v\n%s", deep, p, o, s, cikti)
			}
		}
		if !strings.Contains(cikti, "symlink") {
			t.Fatalf("deep=%v: symlink uyarısı yok:\n%s", deep, cikti)
		}
	}
}

func TestDomainEviOnar_MesruOnarim(t *testing.T) {
	rootGerek(t)
	for _, deep := range []bool{false, true} {
		_, ev := evKur(t)
		ssh := filepath.Join(ev, ".ssh")
		pub := filepath.Join(ev, "public_html")
		kar := filepath.Join(ev, ".karantina")
		yanlis := filepath.Join(ev, "yanlis.txt")
		rootDizin := filepath.Join(ev, "dosyalar")
		mkdir(t, ssh, 0o755, kiraciUID)
		mkdir(t, pub, 0o700, kiraciUID)
		mkdir(t, kar, 0o755, kiraciUID)
		yaz(t, yanlis, 0o644, 0)
		mkdir(t, rootDizin, 0o755, 0)
		yaz(t, filepath.Join(rootDizin, "ic.txt"), 0o644, 0)
		derin := filepath.Join(pub, "derin.php")
		yaz(t, derin, 0o644, 0)

		cikti := onar(t, ev, deep)
		bekle := func(p string, uid uint32, mod os.FileMode) {
			t.Helper()
			s := oku(t, p)
			if s.uid != uid || s.mod.Perm() != mod {
				t.Fatalf("deep=%v: %s uid=%d mod=%v, beklenen uid=%d mod=%v\n%s", deep, p, s.uid, s.mod.Perm(), uid, mod, cikti)
			}
		}
		bekle(ssh, kiraciUID, 0o700)
		bekle(pub, kiraciUID, 0o755)
		bekle(kar, 0, 0o700)
		bekle(yanlis, kiraciUID, 0o644)
		bekle(rootDizin, kiraciUID, 0o755)
		bekle(filepath.Join(rootDizin, "ic.txt"), kiraciUID, 0o644)
		if deep {
			bekle(derin, kiraciUID, 0o644)
		} else {
			bekle(derin, 0, 0o644)
		}
	}
}

// DEEP: iç dizindeki sabit bağ (root dosyası) kiracıya chown edilmez.
func TestDomainEviOnar_DeepSabitBagDegismez(t *testing.T) {
	rootGerek(t)
	_, ev := evKur(t)
	dis := t.TempDir()
	kurban := filepath.Join(dis, "gizli")
	yaz(t, kurban, 0o600, 0)
	pub := filepath.Join(ev, "public_html")
	mkdir(t, pub, 0o755, kiraciUID)
	if err := os.Link(kurban, filepath.Join(pub, "bag.php")); err != nil {
		t.Fatal(err)
	}
	once := oku(t, kurban)
	cikti := onar(t, ev, true)
	if s := oku(t, kurban); s != once {
		t.Fatalf("sabit bağlı kurbanın sahibi değişti: %+v → %+v\n%s", once, s, cikti)
	}
}
