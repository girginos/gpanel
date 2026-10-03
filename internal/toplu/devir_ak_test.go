package toplu

// authorizedKeysArsivle gerileme testleri (devir: eski sahibin SSH anahtarları).
// CANLI SİSTEME DOKUNMAZ: ev kökü ve arşiv t.TempDir; LockSSHPassword çağrılmaz.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const akSK = "c_devir"

var akAn = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func akOrtam(t *testing.T) (evKok, ssh, arsiv, dis string) {
	t.Helper()
	evKok = t.TempDir()
	ssh = filepath.Join(evKok, akSK, ".ssh")
	if err := os.MkdirAll(ssh, 0o700); err != nil {
		t.Fatal(err)
	}
	return evKok, ssh, filepath.Join(t.TempDir(), "arsiv"), t.TempDir()
}

func akCalistir(t *testing.T, evKok, arsiv string) error {
	t.Helper()
	sonuc := make(chan error, 1)
	go func() { sonuc <- authorizedKeysArsivle(evKok, akSK, arsiv, akAn) }()
	select {
	case err := <-sonuc:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("authorizedKeysArsivle asılı kaldı")
		return nil
	}
}

func TestAuthorizedKeysArsivle_Mesru(t *testing.T) {
	evKok, ssh, arsiv, _ := akOrtam(t)
	ak := filepath.Join(ssh, "authorized_keys")
	if err := os.WriteFile(ak, []byte("ssh-ed25519 AAAAESKI eski@sahip\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := akCalistir(t, evKok, arsiv); err != nil {
		t.Fatalf("meşru: %v", err)
	}
	if st, err := os.Lstat(ak); err != nil || st.Size() != 0 {
		t.Fatalf("authorized_keys boşaltılmadı: %v %v", st, err)
	}
	b, err := os.ReadFile(filepath.Join(arsiv, akSK+".authorized_keys.20260102-030405"))
	if err != nil || string(b) != "ssh-ed25519 AAAAESKI eski@sahip\n" {
		t.Fatalf("arşiv %q %v", b, err)
	}
	// .ssh / authorized_keys yoksa hata yok.
	_ = os.Remove(ak)
	if err := akCalistir(t, evKok, arsiv); err != nil {
		t.Fatalf("authorized_keys yok: %v", err)
	}
	_ = os.Remove(ssh)
	if err := akCalistir(t, evKok, arsiv); err != nil {
		t.Fatalf(".ssh yok: %v", err)
	}
}

// FIFO: eskiden O_RDWR + io.ReadAll sonsuza dek bekliyordu (devir DoS).
func TestAuthorizedKeysArsivle_FIFOAsiliKalmazVeKaldirilir(t *testing.T) {
	evKok, ssh, arsiv, _ := akOrtam(t)
	ak := filepath.Join(ssh, "authorized_keys")
	if err := unix.Mkfifo(ak, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := akCalistir(t, evKok, arsiv); err == nil {
		t.Fatal("FIFO sessizce kabul edildi")
	}
	if _, err := os.Lstat(ak); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("FIFO girdisi kaldırılmadı: %v", err)
	}
}

// Sabit bağ: bağın hedefi (ev dışı, root'un dosyası) okunmamalı/boşaltılmamalı;
// eski sahip anahtarını bir bağla da koruyamamalı (girdi kaldırılır).
func TestAuthorizedKeysArsivle_SabitBagHedefiDegismez(t *testing.T) {
	evKok, ssh, arsiv, dis := akOrtam(t)
	kurban := filepath.Join(dis, "authorized_keys")
	const icerik = "ssh-ed25519 AAAAYONETICI kok@sunucu\n"
	if err := os.WriteFile(kurban, []byte(icerik), 0o600); err != nil {
		t.Fatal(err)
	}
	ak := filepath.Join(ssh, "authorized_keys")
	if err := os.Link(kurban, ak); err != nil {
		t.Skipf("sabit bağ kurulamadı: %v", err)
	}
	if err := akCalistir(t, evKok, arsiv); err == nil {
		t.Fatal("sabit bağ sessizce kabul edildi")
	}
	if b, _ := os.ReadFile(kurban); string(b) != icerik {
		t.Fatalf("KURBAN EZİLDİ: %q", b)
	}
	if _, err := os.Lstat(ak); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sabit bağ girdisi kaldırılmadı: %v", err)
	}
	if girdiler, _ := os.ReadDir(arsiv); len(girdiler) != 0 {
		t.Fatalf("sabit bağın hedefi arşive okundu: %v", girdiler)
	}
}

// Symlink'ler (.ssh ya da authorized_keys) izlenmez; hedef değişmez.
func TestAuthorizedKeysArsivle_SymlinkHedefiDegismez(t *testing.T) {
	const icerik = "ssh-ed25519 AAAAYONETICI kok@sunucu\n"
	for _, tur := range []string{".ssh", "authorized_keys"} {
		t.Run(tur, func(t *testing.T) {
			evKok, ssh, arsiv, dis := akOrtam(t)
			kurban := filepath.Join(dis, "authorized_keys")
			if err := os.WriteFile(kurban, []byte(icerik), 0o600); err != nil {
				t.Fatal(err)
			}
			if tur == ".ssh" {
				_ = os.Remove(ssh)
				if err := os.Symlink(dis, ssh); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Symlink(kurban, filepath.Join(ssh, "authorized_keys")); err != nil {
				t.Fatal(err)
			}
			_ = akCalistir(t, evKok, arsiv)
			if b, _ := os.ReadFile(kurban); string(b) != icerik {
				t.Fatalf("KURBAN EZİLDİ: %q", b)
			}
			if girdiler, _ := os.ReadDir(arsiv); len(girdiler) != 0 {
				t.Fatalf("symlink hedefi arşive okundu: %v", girdiler)
			}
		})
	}
}

// Okuma üst sınırı: devasa dosya belleğe tümüyle okunmaz (arşiv ≤ sınır).
func TestAuthorizedKeysArsivle_BoyutSiniri(t *testing.T) {
	evKok, ssh, arsiv, _ := akOrtam(t)
	ak := filepath.Join(ssh, "authorized_keys")
	f, err := os.Create(ak)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(authorizedKeysMaks + 4096); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := akCalistir(t, evKok, arsiv); err != nil {
		t.Fatalf("büyük dosya: %v", err)
	}
	st, err := os.Stat(filepath.Join(arsiv, akSK+".authorized_keys.20260102-030405"))
	if err != nil || st.Size() != authorizedKeysMaks {
		t.Fatalf("arşiv boyutu %v %v, beklenen %d", st, err, authorizedKeysMaks)
	}
	if st, _ := os.Lstat(ak); st.Size() != 0 {
		t.Fatal("büyük dosya boşaltılmadı")
	}
}
