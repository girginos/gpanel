package ajan

// sahte_ws_test.go — testler için SUNUCU tarafı en küçük WebSocket (RFC 6455):
// istemci çerçevesi maskeli olmalı, sunucu çerçevesi maskesiz.

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
)

type sunucuWS struct {
	c  net.Conn
	br *bufio.Reader
	mu sync.Mutex
}

func wsSunucuEl(w http.ResponseWriter, r *http.Request, protokol string) (*sunucuWS, error) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || r.Header.Get("Sec-WebSocket-Version") != "13" {
		http.Error(w, "upgrade yok", http.StatusBadRequest)
		return nil, errors.New("upgrade yok")
	}
	if r.Header.Get("Sec-WebSocket-Protocol") != protokol {
		http.Error(w, "alt protokol", http.StatusBadRequest)
		return nil, errors.New("alt protokol")
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("hijack yok")
	}
	c, brw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	yanit := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + wsAccept(r.Header.Get("Sec-WebSocket-Key")) + "\r\n" +
		"Sec-WebSocket-Protocol: " + protokol + "\r\n\r\n"
	if _, err := c.Write([]byte(yanit)); err != nil {
		return nil, err
	}
	return &sunucuWS{c: c, br: brw.Reader}, nil
}

func (s *sunucuWS) oku() ([]byte, error) {
	var mesaj []byte
	for {
		var bas [2]byte
		if _, err := io.ReadFull(s.br, bas[:]); err != nil {
			return nil, err
		}
		fin, op := bas[0]&0x80 != 0, bas[0]&0x0f
		if bas[1]&0x80 == 0 {
			return nil, errors.New("istemci çerçevesi maskesiz")
		}
		n := uint64(bas[1] & 0x7f)
		switch n {
		case 126:
			var e [2]byte
			if _, err := io.ReadFull(s.br, e[:]); err != nil {
				return nil, err
			}
			n = uint64(binary.BigEndian.Uint16(e[:]))
		case 127:
			var e [8]byte
			if _, err := io.ReadFull(s.br, e[:]); err != nil {
				return nil, err
			}
			n = binary.BigEndian.Uint64(e[:])
		}
		var maske [4]byte
		if _, err := io.ReadFull(s.br, maske[:]); err != nil {
			return nil, err
		}
		govde := make([]byte, n)
		if _, err := io.ReadFull(s.br, govde); err != nil {
			return nil, err
		}
		for i := range govde {
			govde[i] ^= maske[i%4]
		}
		switch op {
		case 0x8:
			return nil, io.EOF
		case 0x9:
			_ = s.yaz(0xA, govde, true)
			continue
		case 0xA:
			continue
		case 0x1, 0x2:
			mesaj = govde
		case 0x0:
			mesaj = append(mesaj, govde...)
		}
		if fin {
			return mesaj, nil
		}
	}
}

// yaz: maskesiz sunucu çerçevesi.
func (s *sunucuWS) yaz(op byte, veri []byte, fin bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return sunucuCerceve(s.c, op, veri, fin, false)
}

func sunucuCerceve(c io.Writer, op byte, veri []byte, fin, maskeli bool) error {
	b0 := op
	if fin {
		b0 |= 0x80
	}
	baslik := []byte{b0}
	m := byte(0)
	if maskeli {
		m = 0x80
	}
	n := len(veri)
	switch {
	case n <= 125:
		baslik = append(baslik, m|byte(n))
	case n <= 0xffff:
		baslik = append(baslik, m|126)
		baslik = binary.BigEndian.AppendUint16(baslik, uint16(n))
	default:
		baslik = append(baslik, m|127)
		baslik = binary.BigEndian.AppendUint64(baslik, uint64(n))
	}
	if maskeli {
		baslik = append(baslik, 0, 0, 0, 0) // sıfır maske (yasak olduğunu sınamak için yeterli)
	}
	_, err := c.Write(append(baslik, veri...))
	return err
}
