// Package sinirlidosya: KİRACININ yazabildiği bir dosyayı (wp-config.php, .user.ini,
// taranan .php) panel (root) kimliğinde GÜVENLE okur.
//
// 🔴 Neden: düz os.ReadFile(yol) kiracı dosyasında root olarak (1) symlink izler
// (wp-config.php -> /etc/girginospanel/env) ve (2) boyutsuz okur: yol /dev/zero ya da
// çok büyük bir dosyaya çevrilirse panel süreci belleği tüketip düşer (DoS); FIFO ise
// okumayı sonsuza dek bloklar. Burada: son bileşende O_NOFOLLOW, O_NONBLOCK (FIFO'da
// takılmaz), yalnız düzenli dosya, en çok max bayt (fazlası ErrBuyuk).
package sinirlidosya

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
)

// ErrBuyuk: dosya max baytı aşıyor (kısmi veri DÖNMEZ).
var ErrBuyuk = errors.New("dosya boyut tavanını aşıyor")

// Oku: yolu symlink izlemeden açar, düzenli dosya değilse reddeder, en çok max bayt okur.
func Oku(yol string, max int64) ([]byte, error) {
	f, err := os.OpenFile(yol, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("düzenli dosya değil: %s", yol)
	}
	if st.Size() > max {
		return nil, ErrBuyuk
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, ErrBuyuk
	}
	return b, nil
}
