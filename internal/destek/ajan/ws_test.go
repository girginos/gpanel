package ajan

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func wsTestSunucu(t *testing.T, protokol string, isle func(s *sunucuWS)) *url.URL {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, err := wsSunucuEl(w, r, protokol)
		if err != nil {
			return
		}
		defer s.c.Close()
		isle(s)
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	u.Scheme = "ws"
	u.Path = "/v1/ajan/ws"
	return u
}

func TestWSYankiPingParcali(t *testing.T) {
	u := wsTestSunucu(t, "test.v1", func(s *sunucuWS) {
		for {
			m, err := s.oku()
			if err != nil {
				return
			}
			switch string(m) {
			case "parcali":
				_ = s.yaz(0x9, []byte("p"), true) // ping araya girer
				_ = s.yaz(0x1, []byte("bir-"), false)
				_ = s.yaz(0x0, []byte("iki-"), false)
				_ = s.yaz(0x0, []byte("uc"), true)
			case "buyuk":
				_ = s.yaz(0x1, bytes.Repeat([]byte("x"), 70000), true)
			default:
				_ = s.yaz(0x1, append([]byte("yanki:"), m...), true)
			}
		}
	})
	w, err := wsBaglan(context.Background(), u, "test.v1", nil, 100000)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Kapat()
	if err := w.Gonder([]byte("merhaba")); err != nil {
		t.Fatal(err)
	}
	if m, err := w.Al(5 * time.Second); err != nil || string(m) != "yanki:merhaba" {
		t.Fatalf("yankı: %q %v", m, err)
	}
	// 126 bayt üstü (16 bit uzunluk) istemci çerçevesi
	uzun := strings.Repeat("y", 300)
	_ = w.Gonder([]byte(uzun))
	if m, err := w.Al(5 * time.Second); err != nil || string(m) != "yanki:"+uzun {
		t.Fatalf("uzun yankı: %d %v", len(m), err)
	}
	_ = w.Gonder([]byte("parcali"))
	if m, err := w.Al(5 * time.Second); err != nil || string(m) != "bir-iki-uc" {
		t.Fatalf("parçalı: %q %v", m, err)
	}
	_ = w.Gonder([]byte("buyuk"))
	if m, err := w.Al(5 * time.Second); err != nil || len(m) != 70000 {
		t.Fatalf("64 bit uzunluk: %d %v", len(m), err)
	}
}

func TestWSSinirVeMaskeliSunucuReddi(t *testing.T) {
	u := wsTestSunucu(t, "test.v1", func(s *sunucuWS) {
		_, _ = s.oku()
		_ = s.yaz(0x1, bytes.Repeat([]byte("z"), 2000), true)
	})
	w, err := wsBaglan(context.Background(), u, "test.v1", nil, 1000)
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Gonder([]byte("x"))
	if _, err := w.Al(5 * time.Second); err == nil || !strings.Contains(err.Error(), "sınırını") {
		t.Fatalf("sınır aşımı reddedilmedi: %v", err)
	}
	w.Kapat()

	u2 := wsTestSunucu(t, "test.v1", func(s *sunucuWS) {
		_, _ = s.oku()
		s.mu.Lock()
		_ = sunucuCerceve(s.c, 0x1, []byte("maskeli"), true, true)
		s.mu.Unlock()
	})
	w2, err := wsBaglan(context.Background(), u2, "test.v1", nil, 1000)
	if err != nil {
		t.Fatal(err)
	}
	defer w2.Kapat()
	_ = w2.Gonder([]byte("x"))
	if _, err := w2.Al(5 * time.Second); err == nil || !strings.Contains(err.Error(), "maskeli") {
		t.Fatalf("maskeli sunucu çerçevesi kabul edildi: %v", err)
	}
}

func TestWSElSikismaRed(t *testing.T) {
	// Yanlış alt protokol: sunucu 400 → istemci reddeder.
	u := wsTestSunucu(t, "baska.v1", func(s *sunucuWS) {})
	if _, err := wsBaglan(context.Background(), u, "test.v1", nil, 1000); err == nil {
		t.Fatal("yanlış alt protokol kabul edildi")
	}
	// Sahte Accept: 101 ama yanlış anahtar.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj := w.(http.Hijacker)
		c, _, _ := hj.Hijack()
		defer c.Close()
		_, _ = c.Write([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: yanlis\r\nSec-WebSocket-Protocol: test.v1\r\n\r\n"))
		time.Sleep(100 * time.Millisecond)
	}))
	defer srv.Close()
	u2, _ := url.Parse(strings.Replace(srv.URL, "http", "ws", 1))
	if _, err := wsBaglan(context.Background(), u2, "test.v1", nil, 1000); err == nil || !strings.Contains(err.Error(), "Accept") {
		t.Fatalf("sahte Accept kabul edildi: %v", err)
	}
	// Alt protokolü yankılamayan sunucu.
	srv3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj := w.(http.Hijacker)
		c, _, _ := hj.Hijack()
		defer c.Close()
		_, _ = c.Write([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: " + wsAccept(r.Header.Get("Sec-WebSocket-Key")) + "\r\n\r\n"))
		time.Sleep(100 * time.Millisecond)
	}))
	defer srv3.Close()
	u3, _ := url.Parse(strings.Replace(srv3.URL, "http", "ws", 1))
	if _, err := wsBaglan(context.Background(), u3, "test.v1", nil, 1000); err == nil || !strings.Contains(err.Error(), "alt protokol") {
		t.Fatalf("alt protokolsüz yanıt kabul edildi: %v", err)
	}
}
