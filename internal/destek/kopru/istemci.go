package kopru

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
	"time"
)

// Istemci — ajan sürecinin köprü istemcisi.
type Istemci struct {
	// Yol — soket yolu (boş → SoketYolu).
	Yol string
	// CagriSiniri — tek çağrının toplam sınırı (varsayılan 25 sn). Bu süreyi aşan
	// panel ErrYok sayılır.
	CagriSiniri time.Duration
	// SahipUID — soketin sahibi olması gereken uid (varsayılan 0). Başkasının koyduğu
	// soket asla kullanılmaz.
	SahipUID uint32
}

func (c *Istemci) yol() string {
	if c.Yol != "" {
		return c.Yol
	}
	return SoketYolu
}

// Ping — köprü (ve panel süreci) canlı mı.
func (c *Istemci) Ping(ctx context.Context) error {
	y, err := c.Cagir(ctx, Istek{Ping: true})
	if err != nil {
		return err
	}
	if y.Kod != 200 {
		return ErrYok
	}
	return nil
}

// Cagir — Gecit uygulaması. Taşıma hatalarının hepsi ErrYok'a sarılır; köprünün kendi
// reddi (Yanit.Hata dolu, kod 0) ayrıca "köprü reddetti" olarak ErrYok'a sarılmaz
// ama hata döner (bir yapılandırma/izin hatasıdır, panel kapalı değildir).
func (c *Istemci) Cagir(ctx context.Context, i Istek) (Yanit, error) {
	sin := c.CagriSiniri
	if sin <= 0 {
		sin = 25 * time.Second
	}
	ctx, iptal := context.WithTimeout(ctx, sin)
	defer iptal()

	yol := c.yol()
	if fi, err := os.Lstat(yol); err != nil {
		return Yanit{}, fmt.Errorf("%w: soket yok", ErrYok)
	} else if fi.Mode()&os.ModeSocket == 0 {
		return Yanit{}, fmt.Errorf("%w: yol soket değil", ErrYok)
	} else if sys, ok := fi.Sys().(*syscall.Stat_t); ok && sys.Uid != c.SahipUID {
		return Yanit{}, fmt.Errorf("%w: soket sahibi güvensiz", ErrYok)
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", yol)
	if err != nil {
		return Yanit{}, fmt.Errorf("%w: bağlanılamadı", ErrYok)
	}
	defer conn.Close()
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return Yanit{}, ErrYok
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	// ctx iptalinde bağlantıyı kes (okuma bloke kalmasın).
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	b, err := json.Marshal(i)
	if err != nil || len(b) > IstekSiniri {
		return Yanit{}, errors.New("köprü isteği kurulamadı")
	}
	if _, err := uc.Write(b); err != nil {
		return Yanit{}, fmt.Errorf("%w: yazılamadı", ErrYok)
	}
	if err := uc.CloseWrite(); err != nil {
		return Yanit{}, fmt.Errorf("%w: yazma kapatılamadı", ErrYok)
	}
	// Yanıt gövdesi base64 (≈4/3) + zarf payı.
	ham, err := io.ReadAll(io.LimitReader(uc, YanitSiniri*4/3+64<<10))
	if err != nil || len(ham) == 0 {
		return Yanit{}, fmt.Errorf("%w: yanıt alınamadı", ErrYok)
	}
	var y Yanit
	if err := json.Unmarshal(ham, &y); err != nil {
		return Yanit{}, fmt.Errorf("%w: yanıt çözülemedi", ErrYok)
	}
	if y.Kod == 0 {
		if y.Hata == "" {
			y.Hata = "bilinmeyen ret"
		}
		return y, fmt.Errorf("köprü reddetti: %s", y.Hata)
	}
	return y, nil
}
