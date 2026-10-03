package merkez

// Asgari RFC 6455 WebSocket SUNUCUSU (yalnız stdlib).
//
// 🔴 NEDEN kendi uygulamamız: repo'da WebSocket bağımlılığı yok; merkezin
// ihtiyacı dar (metin çerçevesi, parçalı mesaj, ping/pong, kapatma, boyut
// sınırı) ve bu kadarı için yeni bağımlılık go.mod'u paralel dallarla
// (ajan tarafı) çakıştırır. Uzantı (permessage-deflate) MÜZAKERE EDİLMEZ;
// RSV bitli çerçeve, maskesiz istemci çerçevesi, 125 bayttan büyük ya da
// parçalı kontrol çerçevesi, sınırı aşan mesaj → bağlantı kapatılır.

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

const (
	wsOpDevam   = 0x0
	wsOpMetin   = 0x1
	wsOpIkili   = 0x2
	wsOpKapat   = 0x8
	wsOpPing    = 0x9
	wsOpPong    = 0xA
	wsYazSuresi = 15 * time.Second
)

var (
	errWSKapandi  = errors.New("websocket kapandı")
	errWSProtokol = errors.New("websocket protokol hatası")
	errWSBuyuk    = errors.New("websocket mesajı sınırı aşıyor")
)

type wsBaglanti struct {
	c     net.Conn
	br    *bufio.Reader
	wmu   sync.Mutex
	sinir int
	once  sync.Once
}

func basliktaJeton(h http.Header, ad, jeton string) bool {
	for _, v := range h.Values(ad) {
		for _, p := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(p), jeton) {
				return true
			}
		}
	}
	return false
}

// wsYukselt: el sıkışmayı doğrular (alt protokol ZORUNLU) ve bağlantıyı devralır.
func wsYukselt(w http.ResponseWriter, r *http.Request, altProtokol string, sinir int) (*wsBaglanti, error) {
	if r.Method != http.MethodGet {
		http.Error(w, "yöntem", http.StatusMethodNotAllowed)
		return nil, errors.New("ws: yöntem")
	}
	if !basliktaJeton(r.Header, "Connection", "upgrade") || !basliktaJeton(r.Header, "Upgrade", "websocket") {
		http.Error(w, "websocket bekleniyor", http.StatusBadRequest)
		return nil, errors.New("ws: upgrade başlığı yok")
	}
	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		w.Header().Set("Sec-WebSocket-Version", "13")
		http.Error(w, "sürüm", http.StatusUpgradeRequired)
		return nil, errors.New("ws: sürüm")
	}
	anahtar := r.Header.Get("Sec-WebSocket-Key")
	if b, err := base64.StdEncoding.DecodeString(anahtar); err != nil || len(b) != 16 {
		http.Error(w, "anahtar", http.StatusBadRequest)
		return nil, errors.New("ws: anahtar")
	}
	if !basliktaJeton(r.Header, "Sec-WebSocket-Protocol", altProtokol) {
		http.Error(w, "alt protokol", http.StatusBadRequest)
		return nil, errors.New("ws: alt protokol")
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "devralma yok", http.StatusInternalServerError)
		return nil, errors.New("ws: hijack desteklenmiyor")
	}
	c, brw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	_ = c.SetDeadline(time.Time{})
	h := sha1.Sum([]byte(anahtar + wsGUID))
	yanit := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + base64.StdEncoding.EncodeToString(h[:]) + "\r\n" +
		"Sec-WebSocket-Protocol: " + altProtokol + "\r\n\r\n"
	_ = c.SetWriteDeadline(time.Now().Add(wsYazSuresi))
	if _, err := c.Write([]byte(yanit)); err != nil {
		c.Close()
		return nil, err
	}
	_ = c.SetWriteDeadline(time.Time{})
	return &wsBaglanti{c: c, br: brw.Reader, sinir: sinir}, nil
}

// Oku: sonraki tam veri mesajını döndürür; ping'e pong verir, kapatmayı yansıtır.
func (b *wsBaglanti) Oku(sonTarih time.Time) ([]byte, error) {
	_ = b.c.SetReadDeadline(sonTarih)
	var mesaj []byte
	parcali := false
	metin := false
	for {
		fin, op, veri, err := b.cerceveOku(b.sinir - len(mesaj))
		if err != nil {
			if errors.Is(err, errWSBuyuk) {
				b.Kapat(1009)
			} else if errors.Is(err, errWSProtokol) {
				b.Kapat(1002)
			}
			return nil, err
		}
		switch op {
		case wsOpKapat:
			b.Kapat(1000)
			return nil, errWSKapandi
		case wsOpPing:
			if err := b.yaz(wsOpPong, veri); err != nil {
				return nil, err
			}
		case wsOpPong:
		case wsOpMetin, wsOpIkili:
			if parcali {
				b.Kapat(1002)
				return nil, errWSProtokol
			}
			mesaj = append(mesaj[:0], veri...)
			metin = op == wsOpMetin
			if fin {
				return b.bitir(mesaj, metin)
			}
			parcali = true
		case wsOpDevam:
			if !parcali {
				b.Kapat(1002)
				return nil, errWSProtokol
			}
			mesaj = append(mesaj, veri...)
			if fin {
				return b.bitir(mesaj, metin)
			}
		default:
			b.Kapat(1002)
			return nil, errWSProtokol
		}
	}
}

func (b *wsBaglanti) bitir(m []byte, metin bool) ([]byte, error) {
	if metin && !utf8.Valid(m) {
		b.Kapat(1007)
		return nil, errWSProtokol
	}
	return m, nil
}

func (b *wsBaglanti) cerceveOku(kalan int) (fin bool, op byte, veri []byte, err error) {
	var bas [2]byte
	if _, err = io.ReadFull(b.br, bas[:]); err != nil {
		return
	}
	fin = bas[0]&0x80 != 0
	if bas[0]&0x70 != 0 {
		return false, 0, nil, errWSProtokol
	}
	op = bas[0] & 0x0F
	if bas[1]&0x80 == 0 { // istemci çerçeveleri maskeli OLMALI
		return false, 0, nil, errWSProtokol
	}
	n := uint64(bas[1] & 0x7F)
	switch n {
	case 126:
		var u [2]byte
		if _, err = io.ReadFull(b.br, u[:]); err != nil {
			return
		}
		n = uint64(binary.BigEndian.Uint16(u[:]))
	case 127:
		var u [8]byte
		if _, err = io.ReadFull(b.br, u[:]); err != nil {
			return
		}
		n = binary.BigEndian.Uint64(u[:])
		if n>>63 != 0 {
			return false, 0, nil, errWSProtokol
		}
	}
	if op >= 0x8 {
		if !fin || n > 125 {
			return false, 0, nil, errWSProtokol
		}
	} else if kalan < 0 || n > uint64(kalan) {
		return false, 0, nil, errWSBuyuk
	}
	var mk [4]byte
	if _, err = io.ReadFull(b.br, mk[:]); err != nil {
		return
	}
	veri = make([]byte, n)
	if _, err = io.ReadFull(b.br, veri); err != nil {
		return
	}
	for i := range veri {
		veri[i] ^= mk[i%4]
	}
	return fin, op, veri, nil
}

func (b *wsBaglanti) yaz(op byte, veri []byte) error {
	b.wmu.Lock()
	defer b.wmu.Unlock()
	hdr := []byte{0x80 | op}
	n := len(veri)
	switch {
	case n <= 125:
		hdr = append(hdr, byte(n))
	case n <= 0xFFFF:
		hdr = append(hdr, 126, byte(n>>8), byte(n))
	default:
		hdr = append(hdr, 127)
		hdr = binary.BigEndian.AppendUint64(hdr, uint64(n))
	}
	_ = b.c.SetWriteDeadline(time.Now().Add(wsYazSuresi))
	_, err := b.c.Write(append(hdr, veri...))
	return err
}

// MetinYaz: tek çerçeveli metin mesajı.
func (b *wsBaglanti) MetinYaz(v []byte) error { return b.yaz(wsOpMetin, v) }

// Kapat: kapatma çerçevesi (en iyi çaba) + soket kapatma; tekrar çağrılabilir.
func (b *wsBaglanti) Kapat(kod uint16) {
	b.once.Do(func() {
		_ = b.yaz(wsOpKapat, []byte{byte(kod >> 8), byte(kod)})
		_ = b.c.Close()
	})
}
