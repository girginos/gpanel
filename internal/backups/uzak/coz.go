package uzak

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"girginospanel/internal/backups/hedef"
)

// ErrCiktiVar — çıktı dosyası zaten var (üzerine yazılmaz).
var ErrCiktiVar = errors.New("the output file already exists; choose another name")

// VarsayilanCikti — "x.tar.gz.age" → "x.tar.gz"; .age değilse ".cozuldu" eklenir.
func VarsayilanCikti(girdi string) string {
	if strings.HasSuffix(girdi, ".age") {
		return strings.TrimSuffix(girdi, ".age")
	}
	return girdi + ".cozuldu"
}

func anahtarAc(yol string) (*os.File, error) {
	fi, err := os.Stat(yol)
	if err != nil {
		return nil, fmt.Errorf("could not read the key file: %w", err)
	}
	if fi.Size() > 1<<20 {
		return nil, ErrAnahtarOkunamadi
	}
	return os.Open(yol)
}

// ciktiAtomik — çıktıyı aynı dizinde 0600 geçici dosyaya yazar, başarıda yerine taşır.
// Var olan dosya EZİLMEZ.
func ciktiAtomik(cikti string, yaz func(w io.Writer) error) error {
	if _, err := os.Lstat(cikti); err == nil {
		return ErrCiktiVar
	}
	gecici := cikti + ".kismi-" + rastgeleHex()
	f, err := os.OpenFile(gecici, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	bw := bufio.NewWriterSize(f, 256<<10)
	err = yaz(bw)
	if err == nil {
		err = bw.Flush()
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(gecici, cikti)
	}
	if err != nil {
		_ = os.Remove(gecici)
	}
	return err
}

// DosyaCoz — yerel .age dosyasını anahtar dosyasıyla çözer. Panel/DB gerekmez
// (felaket sonrası başka makinede de çalışır). Yanlış anahtar → ErrAnahtarUymuyor,
// çıktı dosyası bırakılmaz.
func DosyaCoz(girdi, anahtarYol, cikti string) error {
	if cikti == "" {
		cikti = VarsayilanCikti(girdi)
	}
	k, err := anahtarAc(anahtarYol)
	if err != nil {
		return err
	}
	defer k.Close()
	in, err := os.Open(girdi)
	if err != nil {
		return fmt.Errorf("could not open the encrypted file: %w", err)
	}
	defer in.Close()
	return ciktiAtomik(cikti, func(w io.Writer) error { return Coz(w, bufio.NewReaderSize(in, 256<<10), k) })
}

// IndirVeCoz — uzak kopyayı (iş kimliği) hedeften indirir, şifreli özetini manifest
// kaydıyla doğrular, çözer ve düz özetini doğrular. Çıktı yoksa yerel yedek dosya adı.
func (i *Isci) IndirVeCoz(ctx context.Context, isID int64, anahtarYol, cikti string) (string, error) {
	var uzakAd, dosya, shaS, shaD, durum string
	var hedefID int64
	err := i.DB.QueryRowContext(ctx,
		`SELECT uzak_ad, yedek_dosya, sha256_sifreli, sha256_duz, durum, hedef_id FROM backup_uzak_kopyalar WHERE id=? AND kaynak='hedef'`, isID).
		Scan(&uzakAd, &dosya, &shaS, &shaD, &durum, &hedefID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrIsYok
	}
	if err != nil {
		return "", err
	}
	if durum != DurumTamam {
		return "", fmt.Errorf("%w (durum: %s)", ErrDurumUygunDegil, durum)
	}
	if cikti == "" {
		cikti = filepath.Base(dosya)
	}
	if _, err := os.Lstat(cikti); err == nil {
		return "", ErrCiktiVar
	}
	h, err := i.HedefAc(ctx, hedefID)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(i.GeciciDizin, 0o700); err != nil {
		return "", err
	}
	tmp := filepath.Join(i.GeciciDizin, "uzak-indir-"+rastgeleHex()+".tmp")
	defer func() { _ = os.Remove(tmp) }()
	if err := h.Indir(ctx, uzakAd, tmp); err != nil {
		return "", err
	}
	f, err := os.Open(tmp)
	if err != nil {
		return "", err
	}
	hs := sha256.New()
	_, err = io.Copy(hs, f)
	f.Close()
	if err != nil {
		return "", err
	}
	if got := hex.EncodeToString(hs.Sum(nil)); got != shaS {
		return "", errors.New("the downloaded file's digest does not match the manifest (the remote copy is corrupt)")
	}
	k, err := anahtarAc(anahtarYol)
	if err != nil {
		return "", err
	}
	defer k.Close()
	in, err := os.Open(tmp)
	if err != nil {
		return "", err
	}
	defer in.Close()
	duz := sha256.New()
	err = ciktiAtomik(cikti, func(w io.Writer) error {
		return Coz(io.MultiWriter(w, duz), bufio.NewReaderSize(in, 256<<10), k)
	})
	if err != nil {
		return "", err
	}
	if got := hex.EncodeToString(duz.Sum(nil)); got != shaD {
		_ = os.Remove(cikti)
		return "", errors.New("the decrypted backup's digest does not match the manifest")
	}
	return cikti, nil
}

// HedefAcici — testler/CLI için basit tür.
type HedefAcici = func(ctx context.Context, hedefID int64) (hedef.Hedef, error)
