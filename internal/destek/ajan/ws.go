package ajan

// ws.go — en küçük RFC 6455 WebSocket İSTEMCİSİ (yalnız standart kütüphane).
//
// Neden kendi istemcimiz: depoda WebSocket bağımlılığı yok; ajanın ihtiyacı tek
// bir metin kanalı (istemci → maskeli çerçeve, sunucu → maskesiz). Yeni bağımlılık
// (ve go.mod çakışması) yerine ~250 satır, tam test kapsamıyla. Desteklenen:
// el sıkışma (Sec-WebSocket-Accept + alt protokol ZORUNLU), metin/ikili mesaj,
// parçalı mesaj (continuation), ping→pong, kapanış. Uzantı (permessage-deflate)
// İSTENMEZ; sunucu uzantı dönerse bağlantı reddedilir. HTTP vekil desteği yok.

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // G505: RFC 6455 Sec-WebSocket-Accept SHA-1 ister (güvenlik ilkeli değil).
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

var errWSKapandi = errors.New("websocket kapandı")

type wsBag struct {
	c      net.Conn
	br     *bufio.Reader
	yazMu  sync.Mutex
	sinir  int
	kapali bool
}

// wsAccept: Sec-WebSocket-Accept beklenen değeri.
func wsAccept(anahtar string) string {
	h := sha1.Sum([]byte(anahtar + wsGUID)) //nolint:gosec // G401: RFC 6455.
	return base64.StdEncoding.EncodeToString(h[:])
}

// wsBaglan: ws:// ya da wss:// adrese el sıkışır. sinir: bir mesajın en büyük boyutu.
func wsBaglan(ctx context.Context, u *url.URL, altProtokol string, tlsCfg *tls.Config, sinir int) (*wsBag, error) {
	host := u.Host
	if u.Port() == "" {
		if u.Scheme == "wss" {
			host = net.JoinHostPort(u.Hostname(), "443")
		} else {
			host = net.JoinHostPort(u.Hostname(), "80")
		}
	}
	d := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	var c net.Conn
	var err error
	switch u.Scheme {
	case "wss":
		cfg := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname(), NextProtos: []string{"http/1.1"}}
		if tlsCfg != nil {
			cfg = tlsCfg.Clone()
			if cfg.ServerName == "" {
				cfg.ServerName = u.Hostname()
			}
			cfg.NextProtos = []string{"http/1.1"}
		}
		c, err = (&tls.Dialer{NetDialer: d, Config: cfg}).DialContext(ctx, "tcp", host)
	case "ws":
		c, err = d.DialContext(ctx, "tcp", host)
	default:
		return nil, fmt.Errorf("websocket şeması desteklenmiyor: %q", u.Scheme)
	}
	if err != nil {
		return nil, err
	}
	basarili := false
	defer func() {
		if !basarili {
			_ = c.Close()
		}
	}()
	if dl, ok := ctx.Deadline(); ok {
		_ = c.SetDeadline(dl)
	} else {
		_ = c.SetDeadline(time.Now().Add(20 * time.Second))
	}
	kb := make([]byte, 16)
	if _, err := rand.Read(kb); err != nil {
		return nil, err
	}
	anahtar := base64.StdEncoding.EncodeToString(kb)
	yol := u.EscapedPath()
	if yol == "" {
		yol = "/"
	}
	if u.RawQuery != "" {
		yol += "?" + u.RawQuery
	}
	istek := "GET " + yol + " HTTP/1.1\r\n" +
		"Host: " + u.Host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + anahtar + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Sec-WebSocket-Protocol: " + altProtokol + "\r\n" +
		"User-Agent: girginospanel-destek/1\r\n\r\n"
	if _, err := io.WriteString(c, istek); err != nil {
		return nil, err
	}
	br := bufio.NewReaderSize(c, 16<<10)
	req, _ := http.NewRequest(http.MethodGet, u.String(), nil)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		return nil, fmt.Errorf("el sıkışma yanıtı: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		return nil, fmt.Errorf("el sıkışma reddedildi: HTTP %d", resp.StatusCode)
	}
	if !strings.EqualFold(resp.Header.Get("Upgrade"), "websocket") || !basliktaVar(resp.Header.Get("Connection"), "upgrade") {
		return nil, errors.New("el sıkışma: Upgrade/Connection başlığı yok")
	}
	if resp.Header.Get("Sec-WebSocket-Accept") != wsAccept(anahtar) {
		return nil, errors.New("el sıkışma: Sec-WebSocket-Accept uyuşmuyor")
	}
	if resp.Header.Get("Sec-WebSocket-Protocol") != altProtokol {
		return nil, fmt.Errorf("el sıkışma: alt protokol %q bekleniyordu", altProtokol)
	}
	if resp.Header.Get("Sec-WebSocket-Extensions") != "" {
		return nil, errors.New("el sıkışma: istenmeyen uzantı")
	}
	_ = c.SetDeadline(time.Time{})
	basarili = true
	return &wsBag{c: c, br: br, sinir: sinir}, nil
}

func basliktaVar(deger, aranan string) bool {
	for _, p := range strings.Split(deger, ",") {
		if strings.EqualFold(strings.TrimSpace(p), aranan) {
			return true
		}
	}
	return false
}

// Gonder: tek metin çerçevesi (FIN, maskeli).
func (w *wsBag) Gonder(veri []byte) error {
	return w.cerceveYaz(0x1, veri)
}

func (w *wsBag) cerceveYaz(opkod byte, veri []byte) error {
	w.yazMu.Lock()
	defer w.yazMu.Unlock()
	if w.kapali {
		return errWSKapandi
	}
	baslik := make([]byte, 0, 14)
	baslik = append(baslik, 0x80|opkod)
	n := len(veri)
	switch {
	case n <= 125:
		baslik = append(baslik, 0x80|byte(n))
	case n <= 0xffff:
		baslik = append(baslik, 0x80|126)
		baslik = binary.BigEndian.AppendUint16(baslik, uint16(n))
	default:
		baslik = append(baslik, 0x80|127)
		baslik = binary.BigEndian.AppendUint64(baslik, uint64(n))
	}
	var maske [4]byte
	if _, err := rand.Read(maske[:]); err != nil {
		return err
	}
	baslik = append(baslik, maske[:]...)
	govde := make([]byte, n)
	for i := 0; i < n; i++ {
		govde[i] = veri[i] ^ maske[i%4]
	}
	_ = w.c.SetWriteDeadline(time.Now().Add(30 * time.Second))
	if _, err := w.c.Write(append(baslik, govde...)); err != nil {
		return err
	}
	return nil
}

// Al: sıradaki veri mesajı. Ping'e pong döner; kapanışta errWSKapandi.
// zamanAsimi > 0 ise bir mesaj o süre içinde gelmezse zaman aşımı hatası.
func (w *wsBag) Al(zamanAsimi time.Duration) ([]byte, error) {
	var mesaj []byte
	parcali := false
	for {
		if zamanAsimi > 0 {
			_ = w.c.SetReadDeadline(time.Now().Add(zamanAsimi))
		}
		var bas [2]byte
		if _, err := io.ReadFull(w.br, bas[:]); err != nil {
			return nil, err
		}
		fin := bas[0]&0x80 != 0
		if bas[0]&0x70 != 0 {
			return nil, errors.New("websocket: RSV biti (uzantı) beklenmiyor")
		}
		opkod := bas[0] & 0x0f
		if bas[1]&0x80 != 0 {
			return nil, errors.New("websocket: sunucu çerçevesi maskeli olamaz")
		}
		n := uint64(bas[1] & 0x7f)
		switch n {
		case 126:
			var e [2]byte
			if _, err := io.ReadFull(w.br, e[:]); err != nil {
				return nil, err
			}
			n = uint64(binary.BigEndian.Uint16(e[:]))
		case 127:
			var e [8]byte
			if _, err := io.ReadFull(w.br, e[:]); err != nil {
				return nil, err
			}
			n = binary.BigEndian.Uint64(e[:])
		}
		kontrol := opkod >= 0x8
		if kontrol && (n > 125 || !fin) {
			return nil, errors.New("websocket: geçersiz kontrol çerçevesi")
		}
		if !kontrol && uint64(len(mesaj))+n > uint64(w.sinir) {
			return nil, fmt.Errorf("websocket: mesaj %d bayt sınırını aşıyor", w.sinir)
		}
		govde := make([]byte, n)
		if _, err := io.ReadFull(w.br, govde); err != nil {
			return nil, err
		}
		switch opkod {
		case 0x8: // kapanış: yankıla ve bitir
			_ = w.cerceveYaz(0x8, govde)
			w.yazMu.Lock()
			w.kapali = true
			w.yazMu.Unlock()
			return nil, errWSKapandi
		case 0x9: // ping
			if err := w.cerceveYaz(0xA, govde); err != nil {
				return nil, err
			}
			continue
		case 0xA: // pong
			continue
		case 0x1, 0x2:
			if parcali {
				return nil, errors.New("websocket: parçalı mesaj ortasında yeni mesaj")
			}
			mesaj = append(mesaj[:0], govde...)
			if fin {
				return mesaj, nil
			}
			parcali = true
		case 0x0:
			if !parcali {
				return nil, errors.New("websocket: beklenmeyen devam çerçevesi")
			}
			mesaj = append(mesaj, govde...)
			if fin {
				return mesaj, nil
			}
		default:
			return nil, fmt.Errorf("websocket: bilinmeyen opkod %d", opkod)
		}
	}
}

// Kapat: kapanış çerçevesi (en iyi çaba) + bağlantıyı kapatır.
func (w *wsBag) Kapat() error {
	_ = w.cerceveYaz(0x8, []byte{0x03, 0xe8}) // 1000 normal kapanış
	w.yazMu.Lock()
	w.kapali = true
	w.yazMu.Unlock()
	return w.c.Close()
}
