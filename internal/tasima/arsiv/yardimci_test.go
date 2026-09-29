package arsiv

// Test fikstürleri: arşivler test içinde archive/tar ile üretilir (repoda ikili yok);
// bzip2/xz sıkıştırması sistem aracıyla, zstd ise imza baytını atan sahte bir açıcıyla
// yapılır (181'de zstd kurulu değil; alt süreç hattı yine uçtan uca sınanır).

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"girginospanel/internal/evalti"
)

var testZaman = time.Date(2026, 9, 29, 12, 0, 0, 123456789, time.UTC)

// uyeT: fikstür üyesi. tur 0 → düzenli dosya; boyut > 0 → veri yerine o kadar sıfır.
type uyeT struct {
	ad    string
	tur   byte
	mod   int64
	veri  string
	hedef string
	boyut int64
	pax   map[string]string
}

func tarUret(t *testing.T, uyeler ...uyeT) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, u := range uyeler {
		tur := u.tur
		if tur == 0 {
			tur = tar.TypeReg
		}
		mod := u.mod
		if mod == 0 {
			mod = 0o644
			if tur == tar.TypeDir {
				mod = 0o755
			}
		}
		h := &tar.Header{
			Name: u.ad, Typeflag: tur, Mode: mod, Linkname: u.hedef, ModTime: testZaman,
			Uid: 1234, Gid: 1234, Uname: "saldirgan", Gname: "saldirgan", PAXRecords: u.pax,
		}
		if tur == tar.TypeReg {
			h.Size = int64(len(u.veri))
			if u.boyut > 0 {
				h.Size = u.boyut
			}
		}
		if tur == tar.TypeChar || tur == tar.TypeBlock {
			h.Devmajor, h.Devminor = 1, 3
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatalf("%q: %v", u.ad, err)
		}
		if tur == tar.TypeReg {
			veri := []byte(u.veri)
			if u.boyut > 0 {
				veri = make([]byte, u.boyut)
			}
			if _, err := tw.Write(veri); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func gzUret(t *testing.T, b []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// aracla: bzip2/xz ile sıkıştırır (araç yoksa test atlanır).
func aracla(t *testing.T, arac string, b []byte) []byte {
	t.Helper()
	if _, err := exec.LookPath(arac); err != nil {
		t.Skipf("%s yok", arac)
	}
	cmd := exec.Command(arac, "-z", "-c")
	cmd.Stdin = bytes.NewReader(b)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s: %v", arac, err)
	}
	return out
}

// zstdUret: sahte açıcının beklediği biçim — zstd imzası + düz tar.
func zstdUret(b []byte) []byte { return append(append([]byte{}, zstdSihir...), b...) }

// sahteZstd: PATH'in başına argv'yi ve (root ise) nobody kimliğini doğrulayan, imza
// baytlarını atıp gerisini aktaran bir `zstd` koyar.
func sahteZstd(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	for _, d := range []string{filepath.Dir(dir), dir} {
		if err := os.Chmod(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	betik := "#!/bin/sh\n[ \"$*\" = \"-d -c -q --memory=512MB\" ] || exit 3\n"
	if os.Geteuid() == 0 {
		u, err := user.Lookup("nobody")
		if err != nil {
			t.Skipf("nobody yok: %v", err)
		}
		betik += "[ \"$(id -u)\" = \"" + u.Uid + "\" ] || exit 4\n"
	}
	betik += "exec tail -c +5\n"
	if err := os.WriteFile(filepath.Join(dir, "zstd"), []byte(betik), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

func dosyala(t *testing.T, b []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "arsiv.bin")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func acT(t *testing.T, b []byte, s Sinirlar, ust func(string) bool) (*Arsiv, error) {
	t.Helper()
	return Ac(context.Background(), dosyala(t, b), s, ust)
}

const (
	secimOnek = "cpmove-u/homedir/public_html"
	gizliKok  = "GIZLI-KOK-ICERIGI-5c1e"
)

// iyiUyeler: cPanel cpmove benzeri meşru hesap arşivi.
func iyiUyeler() []uyeT {
	return []uyeT{
		{ad: "./", tur: tar.TypeDir},
		{ad: "./cpmove-u/", tur: tar.TypeDir},
		{ad: "./cpmove-u/cp/u", veri: "USER=u\nDNS=ornek.com\n"},
		{ad: "cpmove-u/userdata/main", veri: "main_domain: ornek.com\n"},
		{ad: "cpmove-u/mysql/u_wp.sql", veri: "CREATE TABLE t (a int);\nINSERT INTO t VALUES (1);\n"},
		{ad: "cpmove-u/homedir/", tur: tar.TypeDir},
		{ad: "cpmove-u/homedir/access-logs", tur: tar.TypeSymlink, hedef: "/usr/local/apache/domlogs/u"},
		{ad: "cpmove-u/homedir/mail/", tur: tar.TypeDir},
		{ad: "cpmove-u/homedir/public_html/", tur: tar.TypeDir, mod: 0o750},
		{ad: "cpmove-u/homedir/public_html/index.php", veri: "<?php echo 1;", pax: map[string]string{"SCHILY.xattr.user.saldiri": "x"}},
		{ad: "cpmove-u/homedir/public_html/suid", veri: "#!/bin/sh\n", mod: 0o6755},
		{ad: "cpmove-u/homedir/public_html/sticky/", tur: tar.TypeDir, mod: 0o1777},
		{ad: "cpmove-u/homedir/public_html/wp/", tur: tar.TypeDir},
		{ad: "cpmove-u/homedir/public_html/wp/a.txt", veri: "AAAA"},
		{ad: "cpmove-u/homedir/public_html/wp/\xfe\xfdeski.txt", veri: "latin5"},
		{ad: "cpmove-u/homedir/public_html/kisayol", tur: tar.TypeSymlink, hedef: "wp/a.txt"},
		{ad: "cpmove-u/homedir/public_html/wp/ust", tur: tar.TypeSymlink, hedef: "../index.php"},
		{ad: "cpmove-u/homedir/public_html/sabit", tur: tar.TypeLink, hedef: "cpmove-u/homedir/public_html/wp/a.txt"},
	}
}

// kotuUyeler: seçim içinde atlanması gereken her şey (arşivi reddettirmeyen türden).
func kotuUyeler() []uyeT {
	return []uyeT{
		{ad: "cpmove-u/homedir/public_html/dis-bag", tur: tar.TypeSymlink, hedef: "../../../../etc"},
		{ad: "cpmove-u/homedir/public_html/mutlak", tur: tar.TypeSymlink, hedef: "/etc/shadow"},
		{ad: "cpmove-u/homedir/public_html/secim-disi", tur: tar.TypeSymlink, hedef: "../mail"},
		{ad: "cpmove-u/homedir/public_html/bos-bag", tur: tar.TypeSymlink, hedef: ""},
		{ad: "cpmove-u/homedir/public_html/ters-bag", tur: tar.TypeSymlink, hedef: `..\..\etc`},
		{ad: "cpmove-u/homedir/public_html/l", tur: tar.TypeSymlink, hedef: "wp"},
		{ad: "cpmove-u/homedir/public_html/l/x.php", veri: "BAGDAN-YAZILDI"},
		{ad: "cpmove-u/homedir/public_html/aygit", tur: tar.TypeChar},
		{ad: "cpmove-u/homedir/public_html/blok", tur: tar.TypeBlock},
		{ad: "cpmove-u/homedir/public_html/fifo", tur: tar.TypeFifo},
		{ad: "cpmove-u/homedir/public_html/sabit-disari", tur: tar.TypeLink, hedef: "cpmove-u/cp/u"},
		{ad: "cpmove-u/homedir/public_html/sabit-ileri", tur: tar.TypeLink, hedef: "cpmove-u/homedir/public_html/sonra.txt"},
		{ad: "cpmove-u/homedir/public_html/sabit-kacis", tur: tar.TypeLink, hedef: "../../etc/passwd"},
		{ad: "cpmove-u/homedir/public_html/sonra.txt", veri: "sonra"},
	}
}

// ortam: kiracı yerine ayrıcalıksız nobody; ev kökü t.TempDir (canlı /home'a dokunulmaz).
type ortam struct {
	base, home string
	uid, gid   int
}

const testSK = "c_test"

func yeniOrtam(t *testing.T) ortam {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("runuser ile kiracı kimliği için root gerekli")
	}
	for _, b := range []string{"runuser", "tar"} {
		if _, err := exec.LookPath(b); err != nil {
			t.Skipf("%s yok", b)
		}
	}
	u, err := user.Lookup("nobody")
	if err != nil {
		t.Skipf("nobody yok: %v", err)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	base := t.TempDir()
	for _, d := range []string{filepath.Dir(base), base} {
		if err := os.Chmod(d, 0o711); err != nil {
			t.Fatal(err)
		}
	}
	eskiKok, eskiKul := evalti.EvKoku, evalti.OSKullanici
	evalti.EvKoku = base
	evalti.OSKullanici = func(string) string { return "nobody" }
	t.Cleanup(func() { evalti.EvKoku, evalti.OSKullanici = eskiKok, eskiKul })
	home := filepath.Join(base, testSK)
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(home, uid, gid); err != nil {
		t.Fatal(err)
	}
	return ortam{base: base, home: home, uid: uid, gid: gid}
}

// kurban: kiracının erişemediği kök sahipli dizin + gizli dosya (komşu ev, /etc yerine).
func kurban(t *testing.T, base string) string {
	t.Helper()
	d := filepath.Join(base, "kurban")
	if err := os.Mkdir(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "gizli.txt"), []byte(gizliKok), 0o644); err != nil {
		t.Fatal(err)
	}
	return d
}

// agacOzeti: dizin ağacının (bağ izlemeden) ad → tür/kip/içerik özeti.
func agacOzeti(t *testing.T, kok string) map[string]string {
	t.Helper()
	oz := map[string]string{}
	err := filepath.Walk(kok, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(kok, p)
		s := fi.Mode().String()
		if fi.Mode().IsRegular() {
			b, _ := os.ReadFile(p)
			s += ":" + string(b)
		}
		oz[rel] = s
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return oz
}
