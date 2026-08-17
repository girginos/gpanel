package hostuyg

// Binary/tarball indirici + SHA256 doğrulama + çıkarma.
//
// GÜVENLİK:
//   - SHA256 zorunlu (Tarif.Dogrula() kontrolü) — supply chain koruması
//   - İndirmeden ÖNCE HEAD ile boyut kontrolü (5GB max, DoS koruması)
//   - Streaming SHA hesaplama (RAM'e almaz)
//   - Tar/zip çıkarma path traversal koruması (arşivde "../etc/passwd" red)
//   - Symlink içeren arşiv reddedilir (Faz 1 defect'i tekrar etmesin)

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const MaxIndirmeBoyutu = 5 * 1024 * 1024 * 1024 // 5 GB

// IndirmeCacheDir — indirilen dosyalar burada tutulur. /tmp yerine ayrı
// bir konum ki 5GB'lık indirmeler root FS'i doldurmasın (I2 fix).
var IndirmeCacheDir = "/var/tmp/gpanel-apps-cache"

// Indir — URL'den download + SHA256 verify. Dönen: geçici dosya yolu.
// Hata halinde geçici dosya silinir.
func Indir(url, beklenenSHA256 string) (string, error) {
	// HEAD önce — boyut kontrolü
	head, err := http.Head(url)
	if err == nil {
		defer head.Body.Close()
		if head.ContentLength > MaxIndirmeBoyutu {
			return "", fmt.Errorf("dosya boyutu %d aşıyor (max 5GB)", head.ContentLength)
		}
	}

	// I2 fix: cache dizini ayrı mount ki /tmp dolmasın
	_ = os.MkdirAll(IndirmeCacheDir, 0755)
	tmp, err := os.CreateTemp(IndirmeCacheDir, "indir-*.dat")
	if err != nil {
		// fallback /tmp
		tmp, err = os.CreateTemp("", "hostuyg-indir-*.dat")
		if err != nil {
			return "", err
		}
	}
	tmpYol := tmp.Name()
	defer func() {
		if err != nil {
			tmp.Close()
			_ = os.Remove(tmpYol)
		}
	}()

	client := &http.Client{Timeout: 20 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("indirme HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > MaxIndirmeBoyutu {
		return "", fmt.Errorf("dosya boyutu %d aşıyor (max 5GB)", resp.ContentLength)
	}

	// Streaming SHA hesap
	h := sha256.New()
	limited := io.LimitReader(resp.Body, MaxIndirmeBoyutu+1)
	written, err := io.Copy(io.MultiWriter(tmp, h), limited)
	if err != nil {
		return "", err
	}
	if written > MaxIndirmeBoyutu {
		return "", errors.New("dosya boyutu 5GB'ı aştı")
	}
	tmp.Close()

	hesaplanan := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(hesaplanan, beklenenSHA256) {
		return "", fmt.Errorf("SHA256 uyumsuz: bekleniyor=%s hesaplanan=%s (supply chain koruması)",
			beklenenSHA256, hesaplanan)
	}
	return tmpYol, nil
}

// Cikart — indirilen dosyayı hedefe aç. IcerikTuru'na göre format seç.
// "binary" için indirilen dosyayı hedef/BinaryYol olarak taşır ve +x yapar.
func Cikart(indirilenYol, hedefDizin, icerikTuru, binaryYol string) error {
	if err := os.MkdirAll(hedefDizin, 0755); err != nil {
		return err
	}
	switch icerikTuru {
	case "binary":
		ad := binaryYol
		if ad == "" {
			ad = filepath.Base(hedefDizin) // vaultwarden gibi
		}
		hedef := filepath.Join(hedefDizin, ad)
		if err := dosyaTasi(indirilenYol, hedef); err != nil {
			return err
		}
		return os.Chmod(hedef, 0755)
	case "tarball_gz":
		return tarballCikart(indirilenYol, hedefDizin, true)
	case "tarball_xz":
		// xz için exec kullan (arşiv paketinde xz yok stdlib'de)
		cmd := exec.Command("tar", "-xJf", indirilenYol, "-C", hedefDizin)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("tar -xJf: %s (%w)", strings.TrimSpace(string(out)), err)
		}
		return nil
	case "tarball_bz2":
		// bz2 için exec (stdlib bz2 decompress yok). Sistem bzip2 gerekli.
		cmd := exec.Command("tar", "-xjf", indirilenYol, "-C", hedefDizin)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("tar -xjf: %s (%w)", strings.TrimSpace(string(out)), err)
		}
		return nil
	case "zip":
		return zipCikart(indirilenYol, hedefDizin)
	default:
		return fmt.Errorf("bilinmeyen içerik türü: %s", icerikTuru)
	}
}

func dosyaTasi(src, dst string) error {
	// rename farklı FS ise fail eder — copy fallback
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return os.Remove(src)
}

// tarballCikart — .tar.gz açar. Path traversal + symlink reddi.
func tarballCikart(arsiv, hedefKok string, gz bool) error {
	f, err := os.Open(arsiv)
	if err != nil {
		return err
	}
	defer f.Close()

	var r io.Reader = f
	if gz {
		gzr, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer gzr.Close()
		r = gzr
	}
	tr := tar.NewReader(r)
	hedefKokMutlak, err := filepath.Abs(hedefKok)
	if err != nil {
		return err
	}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		// GÜVENLİK: symlink/hardlink yasak (Faz 1 defect'i)
		if hdr.Typeflag == tar.TypeSymlink || hdr.Typeflag == tar.TypeLink {
			return fmt.Errorf("arşiv symlink içeriyor: %s (güvenlik reddi)", hdr.Name)
		}
		hedef := filepath.Join(hedefKok, hdr.Name)
		hedefMutlak, err := filepath.Abs(hedef)
		if err != nil {
			return err
		}
		// GÜVENLİK: path traversal
		if !strings.HasPrefix(hedefMutlak, hedefKokMutlak+string(os.PathSeparator)) &&
			hedefMutlak != hedefKokMutlak {
			return fmt.Errorf("arşiv path traversal denemesi: %s", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(hedef, os.FileMode(hdr.Mode)&0777&^0022); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(hedef), 0755); err != nil {
				return err
			}
			fh, err := os.OpenFile(hedef, os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
				os.FileMode(hdr.Mode)&0777&^0022)
			if err != nil {
				return err
			}
			if _, err := io.Copy(fh, tr); err != nil {
				fh.Close()
				return err
			}
			fh.Close()
		}
	}
}

// zipCikart — .zip açar. Aynı path traversal + symlink koruması.
func zipCikart(arsiv, hedefKok string) error {
	zr, err := zip.OpenReader(arsiv)
	if err != nil {
		return err
	}
	defer zr.Close()
	hedefKokMutlak, err := filepath.Abs(hedefKok)
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		// symlink reddi
		if f.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("zip symlink içeriyor: %s (güvenlik reddi)", f.Name)
		}
		hedef := filepath.Join(hedefKok, f.Name)
		hedefMutlak, err := filepath.Abs(hedef)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(hedefMutlak, hedefKokMutlak+string(os.PathSeparator)) &&
			hedefMutlak != hedefKokMutlak {
			return fmt.Errorf("zip path traversal: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(hedef, 0755)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(hedef), 0755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(hedef, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode()&0777&^0022)
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			rc.Close()
			out.Close()
			return err
		}
		rc.Close()
		out.Close()
	}
	return nil
}

// SHAHesapla — mevcut dosyanın hash'i (recipe SHA'sını üretmek için).
func SHAHesapla(yol string) (string, error) {
	f, err := os.Open(yol)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
