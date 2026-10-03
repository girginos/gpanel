package hedef

// okuyucu.go — hedef uygulamalarının ORTAK akış yardımcıları: iptal + ilerleme
// ve bant sınırı. Her tür kendi sarmalayıcısını yazmasın diye burada.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"time"
)

// GeciciAd — yerelYol ile AYNI dizinde, gizli ve benzersiz bir geçici dosya
// yolu (aynı dosya sisteminde → os.Rename atomik).
func GeciciAd(yerelYol string) string {
	var rnd [6]byte
	_, _ = rand.Read(rnd[:])
	return filepath.Join(filepath.Dir(yerelYol), "."+filepath.Base(yerelYol)+".indiriliyor-"+hex.EncodeToString(rnd[:]))
}

// AtomikYaz — r'yi yerelYol'un dizininde geçici bir dosyaya (0600) yazar ve
// ANCAK başarıyla bitince yerelYol'a taşır. Hata/iptal ⇒ geçici dosya silinir,
// yerelYol'a yarım içerik düşmez. Dönen hata *Hata'dır (işlem "indir").
func AtomikYaz(ctx context.Context, yerelYol string, r io.Reader) error {
	gecici := GeciciAd(yerelYol)
	f, err := os.OpenFile(gecici, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return Sarmala("indir", err)
	}
	_, err = io.Copy(f, IlerlemeOkuyucu(ctx, r, nil))
	if kerr := f.Close(); err == nil {
		err = kerr
	}
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		err = os.Rename(gecici, yerelYol)
	}
	if err != nil {
		_ = os.Remove(gecici)
		return Sarmala("indir", err)
	}
	return nil
}

// ilerlemeParcasi — tek Read çağrısında en çok bu kadar bayt okunur; böylece
// Ilerleme "en az her 1 MiB'de bir" çağrılır ve iptal en geç bir parça sonra
// fark edilir.
const ilerlemeParcasi = 1 << 20

type ilerlemeOkuyucu struct {
	ctx    context.Context
	r      io.Reader
	f      func(int64)
	toplam int64
}

// IlerlemeOkuyucu — r'yi sarar: her Read'den ÖNCE ctx denetlenir (iptal →
// ctx.Err()), okunan toplam f'ye bildirilir (f nil olabilir).
func IlerlemeOkuyucu(ctx context.Context, r io.Reader, f func(aktarilan int64)) io.Reader {
	return &ilerlemeOkuyucu{ctx: ctx, r: r, f: f}
}

func (o *ilerlemeOkuyucu) Read(p []byte) (int, error) {
	if err := o.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) > ilerlemeParcasi {
		p = p[:ilerlemeParcasi]
	}
	n, err := o.r.Read(p)
	if n > 0 {
		o.toplam += int64(n)
		if o.f != nil {
			o.f(o.toplam)
		}
	}
	return n, err
}

type sinirliOkuyucu struct {
	ctx       context.Context
	r         io.Reader
	hiz       float64 // bayt/sn
	parca     int
	baslangic time.Time
	toplam    int64
}

// SinirliOkuyucu — okumayı kibps KiB/sn ile sınırlar (<=0 → r aynen döner).
// Bekleme ctx ile kesilebilir.
func SinirliOkuyucu(ctx context.Context, r io.Reader, kibps int) io.Reader {
	if kibps <= 0 {
		return r
	}
	hiz := float64(kibps) * 1024
	parca := int(hiz / 10) // ~100 ms'lik dilim
	if parca < 1024 {
		parca = 1024
	}
	if parca > 256<<10 {
		parca = 256 << 10
	}
	return &sinirliOkuyucu{ctx: ctx, r: r, hiz: hiz, parca: parca}
}

func (s *sinirliOkuyucu) Read(p []byte) (int, error) {
	if s.baslangic.IsZero() {
		s.baslangic = time.Now()
	}
	if len(p) > s.parca {
		p = p[:s.parca]
	}
	n, err := s.r.Read(p)
	s.toplam += int64(n)
	gereken := time.Duration(float64(s.toplam) / s.hiz * float64(time.Second))
	if bekle := gereken - time.Since(s.baslangic); bekle > 0 {
		t := time.NewTimer(bekle)
		select {
		case <-s.ctx.Done():
			t.Stop()
			return n, s.ctx.Err()
		case <-t.C:
		}
	}
	return n, err
}
