package sirrota

import (
	"bufio"
	"context"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// sahteValkey: AUTH / ACL SETUSER default resetpass >p / ACL SAVE / PING konuşan
// asgari RESP sunucusu. saveHata=true ise ACL SAVE -ERR döner.
type sahteValkey struct {
	mu       sync.Mutex
	parola   string
	kayitli  string // ACL SAVE ile "diske" yazılan parola
	saveHata bool
	ln       net.Listener
}

func yeniSahteValkey(t *testing.T, parola string) *sahteValkey {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	v := &sahteValkey{parola: parola, kayitli: parola, ln: ln}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go v.hizmet(c)
		}
	}()
	return v
}

func (v *sahteValkey) hizmet(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	yetkili := false
	for {
		args, err := respDiziOku(r)
		if err != nil {
			return
		}
		v.mu.Lock()
		yanit := "+OK\r\n"
		switch strings.ToUpper(args[0]) {
		case "AUTH":
			if len(args) == 3 && args[1] == "default" && args[2] == v.parola {
				yetkili = true
			} else {
				yanit = "-WRONGPASS invalid username-password pair\r\n"
			}
		case "PING":
			yanit = "+PONG\r\n"
			if !yetkili {
				yanit = "-NOAUTH\r\n"
			}
		case "ACL":
			switch {
			case !yetkili:
				yanit = "-NOAUTH\r\n"
			case len(args) == 5 && strings.ToUpper(args[1]) == "SETUSER" && args[2] == "default" && args[3] == "resetpass" && strings.HasPrefix(args[4], ">"):
				v.parola = args[4][1:]
			case len(args) == 2 && strings.ToUpper(args[1]) == "SAVE":
				if v.saveHata {
					yanit = "-ERR This Redis instance is not configured to use an ACL file\r\n"
				} else {
					v.kayitli = v.parola
				}
			default:
				yanit = "-ERR bilinmeyen\r\n"
			}
		default:
			yanit = "-ERR bilinmeyen komut\r\n"
		}
		v.mu.Unlock()
		if _, err := io.WriteString(c, yanit); err != nil {
			return
		}
	}
}

func respDiziOku(r *bufio.Reader) ([]string, error) {
	s, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	n, _ := strconv.Atoi(strings.TrimSpace(s[1:]))
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		h, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		l, _ := strconv.Atoi(strings.TrimSpace(h[1:]))
		buf := make([]byte, l+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		out = append(out, string(buf[:l]))
	}
	return out, nil
}

func TestValkeyTCP_ParolaDegistirVeDogrula(t *testing.T) {
	v := yeniSahteValkey(t, "eski")
	k := ValkeyTCP{Adres: v.ln.Addr().String()}
	ctx := context.Background()
	if err := k.Dogrula(ctx, "eski"); err != nil {
		t.Fatal(err)
	}
	if err := k.ParolaDegistir(ctx, "yanlis", "yeni"); err == nil {
		t.Fatal("yanlış eski parola kabul edildi")
	}
	if err := k.ParolaDegistir(ctx, "eski", "yeni"); err != nil {
		t.Fatal(err)
	}
	if v.kayitli != "yeni" {
		t.Error("ACL SAVE ile kalıcı yazılmadı")
	}
	if k.Dogrula(ctx, "eski") == nil || k.Dogrula(ctx, "yeni") != nil {
		t.Error("parola değişimi doğrulanamadı")
	}
}

// ACL SAVE düşerse bellek içi değişiklik geri alınmalı: yoksa Valkey restart'ında
// eski parolaya dönerdi ve env'deki yeni parola tutmazdı (panel Redis'siz kalır).
func TestValkeyTCP_SaveDuserseBellekGeriAlinir(t *testing.T) {
	v := yeniSahteValkey(t, "eski")
	v.saveHata = true
	k := ValkeyTCP{Adres: v.ln.Addr().String()}
	if err := k.ParolaDegistir(context.Background(), "eski", "yeni"); err == nil {
		t.Fatal("SAVE hatası yutuldu")
	}
	if v.parola != "eski" {
		t.Errorf("bellek içi parola geri alınmadı: %q", v.parola)
	}
}
