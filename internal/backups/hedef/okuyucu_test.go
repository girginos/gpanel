package hedef

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIlerlemeOkuyucu(t *testing.T) {
	veri := bytes.Repeat([]byte{7}, 3<<20+5)
	var bildirimler []int64
	r := IlerlemeOkuyucu(context.Background(), bytes.NewReader(veri), func(n int64) { bildirimler = append(bildirimler, n) })
	buf := make([]byte, 8<<20) // tek seferde büyük tampon: yine de 1 MiB'lik parçalarla bildirilmeli
	var toplam int
	for {
		n, err := r.Read(buf)
		toplam += n
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if toplam != len(veri) || len(bildirimler) < 4 || bildirimler[len(bildirimler)-1] != int64(len(veri)) {
		t.Fatalf("toplam=%d bildirim=%v", toplam, bildirimler)
	}
	for i := 1; i < len(bildirimler); i++ {
		if bildirimler[i]-bildirimler[i-1] > 1<<20 {
			t.Errorf("bildirim aralığı 1 MiB'yi aştı: %v", bildirimler)
		}
	}
	ctx, iptal := context.WithCancel(context.Background())
	iptal()
	if _, err := IlerlemeOkuyucu(ctx, bytes.NewReader(veri), nil).Read(buf); !errors.Is(err, context.Canceled) {
		t.Errorf("iptal edilmiş bağlamda okuma: %v", err)
	}
}

type bozukOkuyucu struct{ kalan int }

func (b *bozukOkuyucu) Read(p []byte) (int, error) {
	if b.kalan <= 0 {
		return 0, errors.New("bağlantı koptu")
	}
	n := min(len(p), b.kalan)
	b.kalan -= n
	return n, nil
}

func TestAtomikYaz(t *testing.T) {
	dizin := t.TempDir()
	yol := filepath.Join(dizin, "yedek dosyası.tar.gz")
	if err := AtomikYaz(context.Background(), yol, strings.NewReader("tamam")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(yol); string(b) != "tamam" {
		t.Fatalf("içerik: %q", b)
	}
	// Yarıda kopan okuma: nihai yol oluşmaz, geçici dosya kalmaz.
	yol2 := filepath.Join(dizin, "yarim.tar.gz")
	err := AtomikYaz(context.Background(), yol2, &bozukOkuyucu{kalan: 4096})
	if err == nil || KategoriAl(err) == "" {
		t.Fatalf("kopan okuma kategorili hata vermeliydi: %v", err)
	}
	ctx, iptal := context.WithCancel(context.Background())
	iptal()
	if err := AtomikYaz(ctx, yol2, strings.NewReader("x")); !errors.Is(err, context.Canceled) {
		t.Fatalf("iptal: %v", err)
	}
	girdiler, _ := os.ReadDir(dizin)
	if len(girdiler) != 1 || girdiler[0].Name() != "yedek dosyası.tar.gz" {
		var adlar []string
		for _, g := range girdiler {
			adlar = append(adlar, g.Name())
		}
		t.Fatalf("yarım/geçici dosya kaldı: %v", adlar)
	}
	if fi, _ := os.Stat(yol); fi.Mode().Perm() != 0o600 {
		t.Errorf("izin 0600 olmalı: %v", fi.Mode().Perm())
	}
}

func TestSinirliOkuyucu(t *testing.T) {
	veri := bytes.Repeat([]byte{1}, 256<<10)
	if r := SinirliOkuyucu(context.Background(), bytes.NewReader(veri), 0); r == nil {
		t.Fatal("sınırsız okuyucu nil")
	}
	bas := time.Now()
	n, err := io.Copy(io.Discard, SinirliOkuyucu(context.Background(), bytes.NewReader(veri), 1024)) // 1 MiB/sn
	sure := time.Since(bas)
	if err != nil || n != int64(len(veri)) {
		t.Fatalf("kopya: %d %v", n, err)
	}
	if sure < 200*time.Millisecond {
		t.Errorf("256 KiB @ 1 MiB/sn en az ~250 ms sürmeliydi, %v", sure)
	}
	ctx, iptal := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer iptal()
	_, err = io.Copy(io.Discard, SinirliOkuyucu(ctx, bytes.NewReader(veri), 16)) // 16 KiB/sn → saniyeler sürer
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("bekleme bağlamla kesilmeliydi: %v", err)
	}
}
