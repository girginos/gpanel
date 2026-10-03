// Package sirdosya — panelin gizli dosyalarını (env, db.key, proxy.secret, TLS
// özel anahtarı, pma token ...) GÜVENLİ okur/yazar.
//
// os.WriteFile(yol, b, 0o600) gizli dosya için YETERSİZDİR:
//   - izin yalnız dosya YOKSA uygulanır: daha önce 0644 kalmış bir dosya üzerine
//     yazılırsa 0644 kalır (sır herkese okunur kalır);
//   - yol bir symlink ise hedefini (başka bir dosyayı) ezer;
//   - yazma yarıda kalırsa yarım/boş dosya bırakır (env yarımsa panel açılmaz).
//
// Yaz: aynı dizinde O_EXCL geçici dosya (0600) → Fchmod(mod) (umask'tan bağımsız)
// → isteğe bağlı Fchown → yaz + fsync → rename (atomik) → dizin fsync. Hedef
// symlink ise REDDEDER (takip etmez).
package sirdosya

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// ErrSymlink — hedef yol bir symlink; gizli dosya işlemleri symlink TAKİP ETMEZ.
var ErrSymlink = errors.New("gizli dosya yolu symlink — takip edilmez")

// Yaz: veriyi yol'a atomik yazar; sonuç dosyanın izni TAM OLARAK mod olur.
// uid/gid < 0 ise sahiplik değiştirilmez (yazan sürecin kimliği kalır).
func Yaz(yol string, veri []byte, mod os.FileMode, uid, gid int) (err error) {
	if st, e := os.Lstat(yol); e == nil {
		if st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s: %w", yol, ErrSymlink)
		}
		if !st.Mode().IsRegular() {
			return fmt.Errorf("%s: düzenli dosya değil", yol)
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	dizin := filepath.Dir(yol)
	f, err := os.CreateTemp(dizin, "."+filepath.Base(yol)+".yaz-*")
	if err != nil {
		return err
	}
	gecici := f.Name()
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(gecici)
		}
	}()
	if err = f.Chmod(mod.Perm()); err != nil {
		return err
	}
	if uid >= 0 || gid >= 0 {
		if err = f.Chown(uid, gid); err != nil {
			return err
		}
	}
	if _, err = f.Write(veri); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(gecici, yol); err != nil {
		return err
	}
	if d, e := os.Open(dizin); e == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// YazKok: root:root sahipli, mod izinli gizli dosya (panel kökte çalışır).
func YazKok(yol string, veri []byte, mod os.FileMode) error {
	return Yaz(yol, veri, mod, 0, 0)
}

// Oku: gizli dosyayı symlink TAKİP ETMEDEN okur (en çok enCok bayt). Düzenli
// dosya değilse ya da symlink ise hata. Dönen FileInfo açılan dosyanındır.
func Oku(yol string, enCok int64) ([]byte, os.FileInfo, error) {
	f, err := os.OpenFile(yol, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, nil, fmt.Errorf("%s: %w", yol, ErrSymlink)
		}
		return nil, nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("%s: düzenli dosya değil", yol)
	}
	b, err := io.ReadAll(io.LimitReader(f, enCok+1))
	if err != nil {
		return nil, nil, err
	}
	if int64(len(b)) > enCok {
		return nil, nil, fmt.Errorf("%s: %d bayttan büyük", yol, enCok)
	}
	return b, st, nil
}

// Sahip: FileInfo'dan uid/gid (Linux).
func Sahip(st os.FileInfo) (uid, gid uint32, ok bool) {
	s, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return s.Uid, s.Gid, true
}
