package arsiv

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"

	"girginospanel/internal/kiraci"
)

// Sıkıştırma imzaları (uzantıya değil içeriğe bakılır; ad saldırganın elinde).
var (
	gzipSihir  = []byte{0x1f, 0x8b}
	bzip2Sihir = []byte("BZh")
	xzSihir    = []byte{0xfd, '7', 'z', 'X', 'Z', 0x00}
	zstdSihir  = []byte{0x28, 0xb5, 0x2f, 0xfd}
)

// Go standart kitaplığında xz/zstd açıcı yok → alt süreç: yalnız stdin → stdout, bellek
// sınırlı, sessiz. Root ise nobody kimliğinde koşar (acici).
// aciciKul — root panelde xz/zstd açıcısının ayrıcalıksız kimliği.
const aciciKul = "nobody"

var (
	xzArgv   = []string{"xz", "-d", "-c", "-q", "--memlimit-decompress=512MiB"}
	zstdArgv = []string{"zstd", "-d", "-c", "-q", "--memory=512MB"}
)

// sayac: okunan baytı sayar (sıkıştırılmış taraf). exec'in stdin kopyalayan
// goroutine'i de okuyabildiği için atomik.
type sayac struct {
	r io.Reader
	n atomic.Int64
}

func (s *sayac) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	s.n.Add(int64(n))
	return n, err
}

// oranli: r'den okunan baytı sayar; esik aşıldıktan sonra okunan / arşiv dosyasından
// okunan (ham) oranı sınırı geçerse ErrBomba döner. Bir geçişte iki kez kullanılır:
// açılmış akış (sıkıştırma bombası) ve tar.Reader'ın verdiği üye verisi (seyrek üye).
// Aşımın olduğu okumada veri VERİLMEZ (0, ErrBomba): io.ReadFull tampon dolunca
// yanındaki hatayı yutar (tar başlık okuması, üst veri), aşım böyle kaçmasın.
type oranli struct {
	r          io.Reader
	ham        *sayac
	n          int64
	oran, esik int64
}

func (o *oranli) Read(p []byte) (int, error) {
	n, err := o.r.Read(p)
	o.n += int64(n)
	if o.n > o.esik && o.n > o.oran*o.ham.n.Load() {
		return 0, ErrBomba
	}
	return n, err
}

// gecis: arşivin baştan sona TEK okunuşu. Her üye başlığı sonraki() içinde sınırlar ve
// ad kurallarından geçer; beklenen doluysa (envanter sonrası geçiş) üye envanterdeki
// aynı sıradaki üyeyle birebir aynı olmalıdır.
type gecis struct {
	ctx      context.Context
	s        Sinirlar
	f        *os.File
	acik     *oranli
	tr       *tar.Reader // yalnız Next; üye verisi veri'den okunur
	veri     *oranli     // tar.Reader'ın verdiği üye verisi
	alt      *exec.Cmd
	altCikis io.ReadCloser
	beklenen []Uye
	n        int   // döndürülen üye sayısı (PAX genel başlıkları hariç)
	toplam   int64 // bildirilen açılmış boyut toplamı
}

func (a *Arsiv) gecisAc(ctx context.Context, beklenen []Uye) (*gecis, error) {
	f, err := os.OpenFile(a.yol, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: arşiv dosyası açılamadı", ErrBicim)
	}
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("%w: arşiv düzenli bir dosya değil", ErrBicim)
	}
	g := &gecis{ctx: ctx, s: a.s, f: f, beklenen: beklenen}
	ham := &sayac{r: f}
	br := bufio.NewReaderSize(ham, 64<<10)
	sihir, _ := br.Peek(len(xzSihir))
	var acik io.Reader
	switch {
	case bytes.HasPrefix(sihir, gzipSihir):
		zr, zerr := gzip.NewReader(br)
		if zerr != nil {
			f.Close()
			return nil, fmt.Errorf("%w (%v)", ErrBicim, zerr)
		}
		acik = zr
	case bytes.HasPrefix(sihir, bzip2Sihir):
		acik = bzip2.NewReader(br)
	case bytes.HasPrefix(sihir, xzSihir):
		acik, err = g.acici(xzArgv, br)
	case bytes.HasPrefix(sihir, zstdSihir):
		acik, err = g.acici(zstdArgv, br)
	default:
		acik = br // sıkıştırmasız: açılmış akış ham akışı geçemez
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	g.acik = &oranli{r: acik, ham: ham, oran: a.s.BombaOrani, esik: a.s.BombaEsik}
	g.tr = tar.NewReader(g.acik)
	// Üye verisi de aynı orana tabi: PAX seyrek üyede (GNU.sparse.*) tür '0' ve boyut
	// mantıksaldır, delikleri tar.Reader sıfırla doldurur. Açılmış akış birkaç yüz bayt
	// kalırken üye verisi (ve süzülmüş akışta seyrek olmayan dosya) istenen boyuta çıkar.
	g.veri = &oranli{r: g.tr, ham: ham, oran: a.s.BombaOrani, esik: a.s.BombaEsik}
	return g, nil
}

// acici: xz/zstd alt süreci. Arşivi panel açar ve stdin'den verir; açıcı dosya
// sistemine erişmez. Panel root ise açıcı nobody kimliğinde koşar (güvenilmez girdiyi
// ayrıştıran C kodu root yetkisi taşımasın) — kimlik düşürme panelin tek
// sarmalayıcısı kiraci.Komut ile (runuser, ortam yalnız PATH/HOME/LANG). Komut adı
// panelde çözülür (eksikse ErrAcici; runuser'ın 127'si biçim hatası sanılmasın) ve
// mutlak yolla verilir. Panel ölürse runuser SIGKILL alır; açıcının stdin'i kapanır,
// o da biter.
func (g *gecis) acici(argv []string, girdi io.Reader) (io.Reader, error) {
	yol, err := exec.LookPath(argv[0])
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrAcici, argv[0])
	}
	var cmd *exec.Cmd
	if os.Geteuid() == 0 {
		if cmd, err = (kiraci.Kiraci{Kul: aciciKul, Ev: "/"}).Komut(g.ctx, append([]string{yol}, argv[1:]...)...); err != nil {
			return nil, fmt.Errorf("açıcı için ayrıcalıksız kullanıcı yok: %w", err)
		}
	} else {
		cmd = yetkisizAcici(g.ctx, yol, argv[1:])
	}
	cmd.Stdin = girdi
	cmd.WaitDelay = 5 * time.Second
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	cikis, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrAcici, argv[0])
		}
		return nil, fmt.Errorf("%w: %s başlatılamadı", ErrAcici, argv[0])
	}
	g.alt, g.altCikis = cmd, cikis
	return cikis, nil
}

// yetkisizAcici — panel root DEĞİLKEN açıcı: kimlik zaten ayrıcalıksız, ortam
// açıkça asgari.
func yetkisizAcici(ctx context.Context, yol string, args []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, yol, args...)
	cmd.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C"}
	cmd.Dir = "/"
	return cmd
}

// hata: okuma hatasını sınıflar. İptal ve bomba olduğu gibi; gerisi (tar/gzip/bzip2
// biçim hataları, açıcının erken kapanması) ErrBicim. Metin arşiv içeriği taşımaz.
func (g *gecis) hata(err error) error {
	if cerr := g.ctx.Err(); cerr != nil {
		return cerr
	}
	if errors.Is(err, ErrBomba) {
		return err
	}
	return fmt.Errorf("%w (%v)", ErrBicim, err)
}

// sonraki: bir sonraki üye. Arşiv sonunda io.EOF.
func (g *gecis) sonraki() (Uye, *tar.Header, error) {
	for {
		if err := g.ctx.Err(); err != nil {
			return Uye{}, nil, err
		}
		hdr, err := g.tr.Next()
		if err == io.EOF {
			if g.beklenen != nil && g.n != len(g.beklenen) {
				return Uye{}, nil, ErrDegisti
			}
			return Uye{}, nil, io.EOF
		}
		if err != nil {
			return Uye{}, nil, g.hata(err)
		}
		if hdr.Typeflag == tar.TypeXGlobalHeader {
			continue // PAX genel başlığı üye değildir
		}
		g.n++
		if g.n > g.s.UyeSayisi {
			return Uye{}, nil, fmt.Errorf("%w: üye sayısı", ErrSinir)
		}
		g.toplam += hdr.Size // negatif boyutu tar.Reader ErrHeader ile reddeder
		if g.toplam > g.s.ToplamBayt {
			return Uye{}, nil, fmt.Errorf("%w: toplam boyut", ErrSinir)
		}
		ad, err := adTemizle(hdr.Name)
		if err != nil {
			return Uye{}, nil, err
		}
		if len(hdr.Linkname) > maxAdUzunluk {
			return Uye{}, nil, adHatasi(hdr.Linkname)
		}
		u := Uye{Ad: ad, Tur: hdr.Typeflag, Boyut: hdr.Size, Hedef: hdr.Linkname}
		if g.beklenen != nil && (g.n > len(g.beklenen) || g.beklenen[g.n-1] != u) {
			return Uye{}, nil, ErrDegisti
		}
		return u, hdr, nil
	}
}

// bitir: tar sonu işaretinden sonra açılmış akışı sonuna kadar tüketir; gzip CRC'si ve
// açıcının çıkış kodu ancak akış sonunda denetlenir (bozuk kuyruk sessiz geçmesin).
func (g *gecis) bitir() error {
	if _, err := io.Copy(io.Discard, g.acik); err != nil {
		return g.hata(err)
	}
	if g.alt != nil {
		err := g.alt.Wait()
		g.alt = nil
		if err != nil {
			return fmt.Errorf("%w: açıcı hata ile bitti", ErrBicim)
		}
	}
	return nil
}

// kapat: geçişi erken ya da normal kapatır (açıcı öldürülür, dosya kapanır).
func (g *gecis) kapat() {
	if g.alt != nil {
		_ = g.altCikis.Close()
		_ = g.alt.Process.Kill()
		_ = g.alt.Wait()
		g.alt = nil
	}
	if g.f != nil {
		_ = g.f.Close()
		g.f = nil
	}
}
