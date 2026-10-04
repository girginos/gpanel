package kopru

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"girginospanel/internal/middleware"
)

// Sunucu — panel sürecinde çalışan köprü sunucusu.
type Sunucu struct {
	// Router — panelin kök yönlendiricisi (TÜM rotalarıyla kurulduktan sonra verilir).
	Router http.Handler
	// Yol — soket yolu (boş → SoketYolu).
	Yol string
	// IzinliUID — kabul edilen eş uid (varsayılan 0 = kök). Testte değiştirilebilir.
	IzinliUID uint32
	// EsUID — eş süreç uid'sini okur (nil → SO_PEERCRED). Test dikişi.
	EsUID func(*net.UnixConn) (uint32, error)
	// IslemSiniri — tek isteğin panelde çalışma sınırı (varsayılan 30 sn).
	IslemSiniri time.Duration
	// EsZamanli — aynı anda işlenen bağlantı sınırı (varsayılan 16).
	EsZamanli int

	mu sync.Mutex
	ln *net.UnixListener
	wg sync.WaitGroup
}

func (s *Sunucu) yol() string {
	if s.Yol != "" {
		return s.Yol
	}
	return SoketYolu
}

// Dinle — soketi kurar ve ctx bitene dek kabul döngüsünü çalıştırır. Kurulum hatası
// döner (panel çalışmaya devam eder; ajan panel_yanit_vermiyor görür).
func (s *Sunucu) Dinle(ctx context.Context) error {
	if s.Router == nil {
		return errors.New("köprü: yönlendirici yok")
	}
	ln, err := soketKur(s.yol())
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()
	go func() { <-ctx.Done(); _ = ln.Close() }()
	sem := make(chan struct{}, max(s.EsZamanli, 16))
	for {
		c, err := ln.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				s.wg.Wait()
				_ = os.Remove(s.yol())
				return nil
			}
			log.Printf("destek köprü: kabul hatası: %v", err)
			time.Sleep(200 * time.Millisecond)
			continue
		}
		select {
		case sem <- struct{}{}:
		default:
			_ = c.Close() // aşırı yük: bekletmeden reddet
			continue
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("🔴 destek köprü: panik: %v", r)
				}
			}()
			s.isle(ctx, c)
		}()
	}
}

// Kapat — dinleyiciyi kapatır (Dinle döner).
func (s *Sunucu) Kapat() {
	s.mu.Lock()
	ln := s.ln
	s.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
}

func (s *Sunucu) esUID(c *net.UnixConn) (uint32, error) {
	if s.EsUID != nil {
		return s.EsUID(c)
	}
	return esUIDOku(c)
}

// isle — tek bağlantı. SIRA BİLİNÇLİ: önce eş kimliği (hiçbir bayt okunmadan),
// sonra sınırlı okuma, sonra biçim, sonra yol izni, sonra çağrı.
func (s *Sunucu) isle(ctx context.Context, c *net.UnixConn) {
	defer c.Close()
	uid, err := s.esUID(c)
	if err != nil || uid != s.IzinliUID {
		return // sessiz kapat: yetkisiz eşe bilgi verilmez
	}
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	b, err := io.ReadAll(io.LimitReader(c, IstekSiniri+1))
	if err != nil || len(b) > IstekSiniri {
		s.yaz(c, Yanit{Hata: "istek geçersiz"})
		return
	}
	i, err := istekCoz(b)
	if err != nil {
		s.yaz(c, Yanit{Hata: "istek geçersiz"})
		return
	}
	if i.Ping {
		s.yaz(c, Yanit{Kod: http.StatusOK})
		return
	}
	if !kimlikGecerli(i.Kimlik) {
		s.yaz(c, Yanit{Hata: "kimlik geçersiz"})
		return
	}
	// 🔴 Köprünün KENDİ varsayılan-red kapısı (yönlendirici RoutePattern denetimi ikinci kapı).
	if _, ok := middleware.DestekYolIzinli(http.MethodGet, i.Yol); !ok {
		s.yaz(c, Yanit{Hata: "yol destek botuna kapalı"})
		return
	}
	q, ok := sorguTemizle(i.Sorgu)
	if !ok {
		s.yaz(c, Yanit{Hata: "sorgu geçersiz"})
		return
	}
	i.Sorgu = q
	sin := s.IslemSiniri
	if sin <= 0 {
		sin = 30 * time.Second
	}
	ictx, iptal := context.WithTimeout(ctx, sin)
	defer iptal()
	y, err := Yerel{Router: s.Router}.Cagir(ictx, i)
	if err != nil {
		s.yaz(c, Yanit{Hata: "panel yanıtı gecikti"})
		return
	}
	s.yaz(c, y)
}

func (s *Sunucu) yaz(c *net.UnixConn, y Yanit) {
	_ = c.SetWriteDeadline(time.Now().Add(20 * time.Second))
	_ = json.NewEncoder(c).Encode(y)
}

// soketKur — dizin 0700 root, soket 0600 root. Eski (ölü) soket kaldırılır; yerinde
// soket OLMAYAN bir dosya/bağlantı varsa dokunulmaz ve HATA verilir.
func soketKur(yol string) (*net.UnixListener, error) {
	dizin := filepath.Dir(yol)
	if err := os.MkdirAll(dizin, 0o700); err != nil {
		return nil, fmt.Errorf("köprü dizini: %w", err)
	}
	st, err := os.Lstat(dizin)
	if err != nil || !st.IsDir() {
		return nil, errors.New("köprü dizini geçersiz")
	}
	if sys, ok := st.Sys().(*syscall.Stat_t); ok {
		if int(sys.Uid) != os.Geteuid() {
			return nil, errors.New("köprü dizininin sahibi güvensiz")
		}
	}
	if st.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(dizin, 0o700); err != nil {
			return nil, fmt.Errorf("köprü dizin izni: %w", err)
		}
	}
	if fi, err := os.Lstat(yol); err == nil {
		if fi.Mode()&os.ModeSocket == 0 {
			return nil, errors.New("köprü yolunda soket olmayan bir dosya var")
		}
		_ = os.Remove(yol)
	}
	eski := syscall.Umask(0o177)
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: yol, Net: "unix"})
	syscall.Umask(eski)
	if err != nil {
		return nil, fmt.Errorf("köprü soketi: %w", err)
	}
	if err := os.Chmod(yol, 0o600); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("köprü soket izni: %w", err)
	}
	ln.SetUnlinkOnClose(true)
	return ln, nil
}
