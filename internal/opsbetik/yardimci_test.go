package opsbetik

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
)

const kiraciUID = 65534 // nobody

func rootGerek(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("chown/sahiplik davranışı için root gerekir")
	}
	if out, err := exec.Command("getent", "passwd", "nobody").Output(); err != nil || !strings.Contains(string(out), ":65534:") {
		t.Skip("nobody (65534) kullanıcısı yok")
	}
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
