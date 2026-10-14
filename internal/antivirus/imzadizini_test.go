package antivirus

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"testing"
)

// imzaSahne: geçici imza dizini + sahte clamupdate kimliği; chown ve yeniden başlatma kaydedilir (ana
// makinenin /var/lib/clamav'ına ve servislerine dokunulmaz).
type imzaSahne struct {
	dizin      string
	chownlar   []string
	yeniden    int
	chownHata  error
	kullaniciY bool
}

func imzaKur(t *testing.T, uid, gid int) *imzaSahne {
	t.Helper()
	s := &imzaSahne{dizin: filepath.Join(t.TempDir(), "clamav")}
	if err := os.Mkdir(s.dizin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"main.cvd", "daily.cvd", "bytecode.cvd"} {
		if err := os.WriteFile(filepath.Join(s.dizin, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	eD, eK, eB, eC, eY := imzaDizini, imzaKullanici, imzaKullaniciBul, imzaChown, imzaYenidenBaslat
	t.Cleanup(func() { imzaDizini, imzaKullanici, imzaKullaniciBul, imzaChown, imzaYenidenBaslat = eD, eK, eB, eC, eY })
	imzaDizini = s.dizin
	imzaKullaniciBul = func(string) (*user.User, error) {
		if s.kullaniciY {
			return nil, user.UnknownUserError("clamupdate")
		}
		return &user.User{Uid: strconv.Itoa(uid), Gid: strconv.Itoa(gid)}, nil
	}
	imzaChown = func(yol string, _, _ int) error {
		if s.chownHata != nil {
			return s.chownHata
		}
		s.chownlar = append(s.chownlar, filepath.Base(yol))
		return nil
	}
	imzaYenidenBaslat = func() error { s.yeniden++; return nil }
	return s
}

func TestImzaDiziniOnar_YanlisSahipOnarilirVeYenidenBaslatilir(t *testing.T) {
	s := imzaKur(t, os.Getuid()+4242, os.Getgid()+4242) // dosyalar test kullanıcısında → "yanlış sahip"
	if err := os.Symlink("/etc/passwd", filepath.Join(s.dizin, "bag")); err != nil {
		t.Fatal(err)
	}
	n, err := ImzaDiziniOnar()
	if err != nil || n != 4 {
		t.Fatalf("dizin + 3 dosya onarılmalı: n=%d err=%v chown=%v", n, err, s.chownlar)
	}
	for _, c := range s.chownlar {
		if c == "bag" {
			t.Fatal("sembolik bağ izlenmemeli / chown edilmemeli")
		}
	}
	if s.yeniden != 1 {
		t.Fatalf("freshclam bir kez yeniden başlatılmalı: %d", s.yeniden)
	}
}

func TestImzaDiziniOnar_DogruSahipteDokunulmaz(t *testing.T) {
	s := imzaKur(t, os.Getuid(), os.Getgid())
	if n, err := ImzaDiziniOnar(); n != 0 || err != nil || len(s.chownlar) != 0 || s.yeniden != 0 {
		t.Fatalf("doğru sahiplikte hiçbir şey yapılmamalı: n=%d err=%v chown=%v yeniden=%d", n, err, s.chownlar, s.yeniden)
	}
}

func TestImzaDiziniOnar_YalnizGrupYanlis(t *testing.T) {
	s := imzaKur(t, os.Getuid(), os.Getgid()+4242)
	if n, _ := ImzaDiziniOnar(); n != 4 || s.yeniden != 1 {
		t.Fatalf("grup farklıysa da onarılmalı: n=%d yeniden=%d", n, s.yeniden)
	}
}

func TestImzaDiziniOnar_KullaniciYokVeyaDizinBagsaDokunulmaz(t *testing.T) {
	s := imzaKur(t, os.Getuid()+4242, os.Getgid()+4242)
	s.kullaniciY = true
	if n, err := ImzaDiziniOnar(); n != 0 || err != nil || len(s.chownlar) != 0 {
		t.Fatalf("clamupdate yoksa dokunulmamalı: n=%d err=%v", n, err)
	}
	s.kullaniciY = false
	bag := filepath.Join(t.TempDir(), "clamav-bag")
	if err := os.Symlink(s.dizin, bag); err != nil {
		t.Fatal(err)
	}
	imzaDizini = bag
	if n, err := ImzaDiziniOnar(); n != 0 || err != nil || len(s.chownlar) != 0 || s.yeniden != 0 {
		t.Fatalf("dizin sembolik bağsa dokunulmamalı: n=%d err=%v chown=%v", n, err, s.chownlar)
	}
	imzaDizini = filepath.Join(t.TempDir(), "yok")
	if n, err := ImzaDiziniOnar(); n != 0 || err != nil {
		t.Fatalf("dizin yoksa sessiz: n=%d err=%v", n, err)
	}
}

func TestImzaDiziniOnar_ChownHatasiBildirilirYenidenBaslatilmaz(t *testing.T) {
	s := imzaKur(t, os.Getuid()+4242, os.Getgid()+4242)
	s.chownHata = errors.New("izin yok")
	n, err := ImzaDiziniOnar()
	if err == nil || n != 0 || s.yeniden != 0 {
		t.Fatalf("chown düşerse hata dönmeli, yeniden başlatma olmamalı: n=%d err=%v yeniden=%d", n, err, s.yeniden)
	}
}
