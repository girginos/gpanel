package provisioner

// homeperms_acl_test.go — 4ea7bac regresyonu: hardenHomePerms pinli fd'yi "/proc/self/fd/<N>" yoluyla
// setfacl/chown ÇOCUK sürecine veriyordu; çocukta o fd yok → "No such file or directory" → public_html'e
// nginx ACL'i konmuyor, grup=nginx fail-safe da aynı yolla çalışmıyor → her YENİ hosting nginx'ten 404.
// Mevcut testler yalnız sahip/mod'a baktığı için (ACL'e bakmadığı için) bunu yakalamadı.

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// aclOrtamiHazirMi: gerçek ACL testi için kök + setfacl/getfacl + nginx kullanıcısı + ACL destekli dosya sistemi.
func aclOrtamiHazirMi(t *testing.T, dizin string) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("kök gerekli")
	}
	for _, a := range []string{"setfacl", "getfacl"} {
		if _, err := exec.LookPath(a); err != nil {
			t.Skipf("%s yok", a)
		}
	}
	if _, err := user.Lookup("nginx"); err != nil {
		t.Skip("nginx kullanıcısı yok")
	}
	deneme := filepath.Join(dizin, "acl-deneme")
	if err := os.Mkdir(deneme, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("setfacl", "-m", "u:nginx:rX", deneme).CombinedOutput(); err != nil {
		t.Skipf("dosya sistemi ACL desteklemiyor: %v %s", err, out)
	}
	_ = os.Remove(deneme)
}

// aclSatiriVar: getfacl çıktısında TAM bu giriş var mı (satır başı eşleşmesi; "#effective:" soneki yok sayılır).
// 🔴 strings.Contains KULLANILMAZ: "user:nginx:r-x", "default:user:nginx:r-x" satırının içinde de geçer →
// erişim ACL'i hiç konmasa da test yeşil kalırdı (negatif kontrol bunu yakaladı).
func aclSatiriVar(acl, giris string) bool {
	for _, satir := range strings.Split(acl, "\n") {
		if f := strings.Fields(satir); len(f) > 0 && f[0] == giris {
			return true
		}
	}
	return false
}

func getfaclCikti(t *testing.T, yol string) string {
	t.Helper()
	out, err := exec.Command("getfacl", "-p", "-c", yol).CombinedOutput()
	if err != nil {
		t.Fatalf("getfacl %s: %v %s", yol, err, out)
	}
	return string(out)
}

// Pinli fd çocuğa DOĞRU inode olarak görünmeli; çağıranın fd numarasıyla yazılan eski yol çocukta YOK.
func TestPinliKomut_CocukPinliInodaGorur(t *testing.T) {
	d := t.TempDir()
	f, err := dizinPinle(d)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st := new(syscall.Stat_t)
	if err := syscall.Stat(d, st); err != nil {
		t.Fatal(err)
	}

	out, err := pinliKomut(f, "stat", "-L", "-c", "%i", pinCocukYolu).CombinedOutput()
	if err != nil {
		t.Fatalf("pinliKomut çocukta pinli dizini göremedi: %v %s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != strconv.FormatUint(st.Ino, 10) {
		t.Fatalf("çocuktaki inode %s, pinli dizin %d", got, st.Ino)
	}

	// Regresyonun varsayımı: ebeveynin fd numarasıyla yazılan yol ÇOCUKTA çözülmez (4ea7bac böyle yapıyordu).
	eski := "/proc/self/fd/" + strconv.Itoa(int(f.Fd()))
	if out, err := exec.Command("stat", "-L", "-c", "%i", eski).CombinedOutput(); err == nil {
		t.Fatalf("beklenmedik: ebeveyn fd yolu çocukta çözüldü (%s) — pinliKomut'a gerek kalmamış olabilir: %s", eski, out)
	}
}

func TestHardenHomePerms_NginxACLGercektenKonur(t *testing.T) {
	kok := t.TempDir()
	aclOrtamiHazirMi(t, kok)
	home := filepath.Join(kok, "home")
	ph := filepath.Join(home, "public_html")
	if err := os.MkdirAll(ph, 0o755); err != nil {
		t.Fatal(err)
	}
	const sahteUID = 31337
	hardenHomePerms(home, "c_acltest", sahteUID, sahteUID)

	acl := getfaclCikti(t, ph)
	for _, bek := range []string{"user:nginx:r-x", "default:user:nginx:r-x"} {
		if !aclSatiriVar(acl, bek) {
			t.Fatalf("public_html ACL'inde %q YOK — yeni hosting nginx'ten 404 alır.\ngetfacl:\n%s", bek, acl)
		}
	}
	// home'a nginx yalnız traverse (--x) verilir.
	if hacl := getfaclCikti(t, home); !aclSatiriVar(hacl, "user:nginx:--x") {
		t.Fatalf("home ACL'inde user:nginx:--x YOK:\n%s", hacl)
	}
}

func TestHardenHomePermsRecursive_ACLIcerigeUygulanir(t *testing.T) {
	kok := t.TempDir()
	aclOrtamiHazirMi(t, kok)
	ph := filepath.Join(kok, "public_html")
	alt := filepath.Join(ph, "wp", "icerik")
	if err := os.MkdirAll(alt, 0o755); err != nil {
		t.Fatal(err)
	}
	dosya := filepath.Join(alt, "index.php")
	if err := os.WriteFile(dosya, []byte("<?php"), 0o640); err != nil {
		t.Fatal(err)
	}
	hardenHomePermsRecursive(ph)

	if acl := getfaclCikti(t, dosya); !aclSatiriVar(acl, "user:nginx:r--") {
		t.Fatalf("iç dosyada nginx ACL'i YOK:\n%s", acl)
	}
	if acl := getfaclCikti(t, alt); !aclSatiriVar(acl, "default:user:nginx:r-x") {
		t.Fatalf("alt dizinde default nginx ACL'i YOK:\n%s", acl)
	}
}
