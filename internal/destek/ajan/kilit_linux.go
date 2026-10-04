//go:build linux

package ajan

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// KilitYolu — tek örnek kilidi (tmpfs; yeniden başlatmada kendiliğinden temiz).
const KilitYolu = "/run/girginospanel/destek-ajan.lock"

// ErrKilitTutuluyor — başka bir ajan örneği çalışıyor.
var ErrKilitTutuluyor = errors.New("başka bir destek ajanı örneği çalışıyor")

// TekOrnekKilidi — flock(LOCK_EX|LOCK_NB) ile TEK ajan örneğini garanti eder. Aynı cihaz
// anahtarıyla iki ajanın merkeze aynı anda bağlanması (güncelleme sırasında eski/yeni
// hizmet, elle ikinci başlatma) merkezde çift oturum ve çakışan iş emri demektir.
// Kilit dosya tanıtıcısı SÜREÇ ÖMRÜ boyunca açık kalır; süreç ölünce çekirdek bırakır
// (bayat kilit dosyası sorun olmaz). Dönen işlev kilidi bırakır (testler için).
func TekOrnekKilidi(yol string) (func(), error) {
	if yol == "" {
		yol = KilitYolu
	}
	dizin := filepath.Dir(yol)
	if err := os.MkdirAll(dizin, 0o700); err != nil {
		return nil, fmt.Errorf("kilit dizini: %w", err)
	}
	if st, err := os.Lstat(dizin); err != nil || !st.IsDir() {
		return nil, errors.New("kilit dizini geçersiz")
	} else if sys, ok := st.Sys().(*syscall.Stat_t); ok && int(sys.Uid) != os.Geteuid() {
		return nil, errors.New("kilit dizininin sahibi güvensiz")
	}
	f, err := os.OpenFile(yol, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("kilit dosyası: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrKilitTutuluyor
		}
		return nil, fmt.Errorf("kilit: %w", err)
	}
	_ = f.Truncate(0)
	_, _ = f.WriteString(strconv.Itoa(os.Getpid()) + "\n")
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}
