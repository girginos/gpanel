package sirrota

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// ValkeyTCP — Valkey/Redis default (yönetici) kullanıcısının parolasını RESP
// üzerinden doğrudan değiştirir. valkey-cli KULLANILMAZ: parola argv'de
// (ACL SETUSER ... >parola) /proc/<pid>/cmdline'dan herkese görünürdü.
// girginospanel-redis-setup'ın kurduğu düzen: aclfile /etc/valkey/users.acl,
// "user default on >ADMIN ~* &* +@all". Değişiklik ACL SAVE ile o dosyaya
// kalıcı yazılır; kiracı ACL kullanıcılarına dokunulmaz.
type ValkeyTCP struct {
	Adres string // 127.0.0.1:6379
}

type respBag struct {
	c net.Conn
	r *bufio.Reader
}

func (v ValkeyTCP) bag(ctx context.Context) (*respBag, error) {
	d := net.Dialer{Timeout: 5 * time.Second}
	c, err := d.DialContext(ctx, "tcp", v.Adres)
	if err != nil {
		return nil, err
	}
	_ = c.SetDeadline(time.Now().Add(15 * time.Second))
	return &respBag{c: c, r: bufio.NewReader(c)}, nil
}

func (b *respBag) kapat() { _ = b.c.Close() }

// komut: RESP dizi olarak gönderir; +OK/-ERR/:n/$bulk yanıtını döndürür.
// -ERR yanıtı error olur (metni parola içermez: sunucu argümanı yankılamaz).
func (b *respBag) komut(args ...string) (string, error) {
	var sb strings.Builder
	sb.WriteString("*" + strconv.Itoa(len(args)) + "\r\n")
	for _, a := range args {
		sb.WriteString("$" + strconv.Itoa(len(a)) + "\r\n" + a + "\r\n")
	}
	if _, err := b.c.Write([]byte(sb.String())); err != nil {
		return "", err
	}
	return respOku(b.r)
}

func respOku(r *bufio.Reader) (string, error) {
	satir, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	satir = strings.TrimRight(satir, "\r\n")
	if satir == "" {
		return "", errors.New("boş RESP yanıtı")
	}
	switch satir[0] {
	case '+', ':':
		return satir[1:], nil
	case '-':
		return "", errors.New(satir[1:])
	case '$':
		n, err := strconv.Atoi(satir[1:])
		if err != nil || n < -1 || n > 1<<20 {
			return "", errors.New("geçersiz RESP uzunluğu")
		}
		if n == -1 {
			return "", nil
		}
		buf := make([]byte, n+2)
		if _, err := ioReadFull(r, buf); err != nil {
			return "", err
		}
		return string(buf[:n]), nil
	default:
		return "", fmt.Errorf("beklenmeyen RESP yanıtı türü %q", satir[0])
	}
}

func ioReadFull(r *bufio.Reader, buf []byte) (int, error) {
	n := 0
	for n < len(buf) {
		m, err := r.Read(buf[n:])
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// Dogrula: parola ile default kullanıcı olarak AUTH + PING.
func (v ValkeyTCP) Dogrula(ctx context.Context, parola string) error {
	b, err := v.bag(ctx)
	if err != nil {
		return err
	}
	defer b.kapat()
	if _, err := b.komut("AUTH", "default", parola); err != nil {
		return fmt.Errorf("AUTH: %v", err)
	}
	if p, err := b.komut("PING"); err != nil || p != "PONG" {
		return fmt.Errorf("PING: %q %v", p, err)
	}
	return nil
}

// ParolaDegistir: eski parolayla AUTH → ACL SETUSER default resetpass >yeni →
// ACL SAVE. SAVE düşerse (aclfile yok/yazılamıyor) bellek içi değişiklik eski
// parolaya GERİ ALINIR: yoksa Valkey yeniden başlayınca eski parolaya döner ve
// env'deki yeni parola tutmaz.
func (v ValkeyTCP) ParolaDegistir(ctx context.Context, eski, yeni string) error {
	if yeni == "" || strings.ContainsAny(yeni, " \r\n") {
		return errors.New("geçersiz yeni parola")
	}
	b, err := v.bag(ctx)
	if err != nil {
		return err
	}
	defer b.kapat()
	if _, err := b.komut("AUTH", "default", eski); err != nil {
		return fmt.Errorf("AUTH (eski parola): %v", err)
	}
	if _, err := b.komut("ACL", "SETUSER", "default", "resetpass", ">"+yeni); err != nil {
		return fmt.Errorf("ACL SETUSER: %v", err)
	}
	if _, err := b.komut("ACL", "SAVE"); err != nil {
		_, gerr := b.komut("ACL", "SETUSER", "default", "resetpass", ">"+eski)
		if gerr != nil {
			return fmt.Errorf("ACL SAVE: %v; bellek içi geri alma da başarısız: %v", err, gerr)
		}
		return fmt.Errorf("ACL SAVE: %v (bellek içi değişiklik geri alındı)", err)
	}
	return nil
}
