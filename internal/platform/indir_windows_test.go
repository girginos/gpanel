//go:build windows

// indir_windows_test.go — B-06/B-12 indirme dayanikliligi birim testleri:
// checksum dogrulama, HTTP Range ile SURDURME (resume), ussel-backoff RETRY,
// ve kalici (404) hata ayrimi. httptest ile deterministik; gercek indirme YOK,
// bu yuzden capraz-derlenen test binary'si sunucuda internetiz kosar.
package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func sinaIs() *KurulumIsi { return &KurulumIsi{ID: "test", Durum: isKosuyor} }

func shaHex(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// baytSun — data'yi sunar; rangeDestek ise Range istegini 206 ile karsilar.
func baytSun(data []byte, rangeDestek bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if rangeDestek {
			if rng := r.Header.Get("Range"); rng != "" {
				var start int64
				if _, err := fmt.Sscanf(rng, "bytes=%d-", &start); err == nil && start >= 0 && start < int64(len(data)) {
					w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(data)-1, len(data)))
					w.WriteHeader(http.StatusPartialContent)
					_, _ = w.Write(data[start:])
					return
				}
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}
}

// TestIndirChecksum — dogru hash gecer; yanlis hash REDDEDILIR ve bozuk gecici silinir.
func TestIndirChecksum(t *testing.T) {
	data := []byte("gpanel checksum dogrulama testi — 123456")
	srv := httptest.NewServer(baytSun(data, false))
	defer srv.Close()

	g1 := filepath.Join(t.TempDir(), "a.indiriliyor")
	if err := indirDene(sinaIs(), srv.URL, g1, shaHex(data), "sina.exe"); err != nil {
		t.Fatalf("dogru checksum reddedildi: %v", err)
	}
	if b, _ := os.ReadFile(g1); !bytes.Equal(b, data) {
		t.Fatalf("indirilen veri yanlis")
	}

	g2 := filepath.Join(t.TempDir(), "b.indiriliyor")
	yanlis := strings.Repeat("0", 64)
	if err := indirDene(sinaIs(), srv.URL, g2, yanlis, "sina.exe"); err == nil {
		t.Fatal("YANLIS checksum kabul edildi (guvenlik)")
	}
	if _, err := os.Stat(g2); !os.IsNotExist(err) {
		t.Fatal("checksum tutmayinca bozuk gecici SILINMELI")
	}
}

// TestIndirResume — kismi gecici dosya varsa Range (206) ile surer ve TAM dosya
// hash'i dogrulanir (mevcut baytlar + eklenen baytlar birlikte hashlenir).
func TestIndirResume(t *testing.T) {
	data := bytes.Repeat([]byte("ABCD"), 4096) // 16 KB
	srv := httptest.NewServer(baytSun(data, true))
	defer srv.Close()

	gecici := filepath.Join(t.TempDir(), "r.indiriliyor")
	if err := os.WriteFile(gecici, data[:5000], 0o644); err != nil { // dogru onEk
		t.Fatal(err)
	}
	if err := indirDene(sinaIs(), srv.URL, gecici, shaHex(data), "sina.exe"); err != nil {
		t.Fatalf("resume basarisiz: %v", err)
	}
	got, _ := os.ReadFile(gecici)
	if !bytes.Equal(got, data) {
		t.Fatalf("resume sonucu tam dosyaya esit degil (%d != %d)", len(got), len(data))
	}
}

// TestIndirRetry — indir sarmalayicisi: sunucu ilk istekte 500, sonra 200 verir;
// backoff'la yeniden denenir ve basarir; sonuc hedefe ATOMIK tasinir.
func TestIndirRetry(t *testing.T) {
	data := []byte("retry yuku")
	var sayac int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&sayac, 1) == 1 {
			w.WriteHeader(http.StatusInternalServerError) // ilk deneme gecici hata
			return
		}
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	hedef := filepath.Join(t.TempDir(), "r.bin")
	if err := indir(sinaIs(), srv.URL, hedef); err != nil {
		t.Fatalf("retry sonrasi basarisiz: %v", err)
	}
	if b, _ := os.ReadFile(hedef); !bytes.Equal(b, data) {
		t.Fatal("retry sonucu yanlis")
	}
	if _, err := os.Stat(hedef + ".indiriliyor"); !os.IsNotExist(err) {
		t.Fatal("basari sonrasi .indiriliyor kalintisi kalmamali")
	}
}

// TestIndirKaliciHata — 404 KALICI'dir: errIndirmeKalici doner (disaridaki dongu
// yeniden denemez, bosuna backoff beklemez).
func TestIndirKaliciHata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	err := indirDene(sinaIs(), srv.URL, filepath.Join(t.TempDir(), "n.indiriliyor"), "", "sina.exe")
	if !errors.Is(err, errIndirmeKalici) {
		t.Fatalf("404 kalici hata olmali, geldi: %v", err)
	}
}

// TestIlerlemeYapisal — canli ETA cubugu sozlesmesi: ilerleme okuyucusu
// okundukca is.Ilerleme'yi yuzde/bayt/hiz/ETA ile dogru doldurmali. Tam inende
// yuzde=100, ETA>=0, hiz>0 olmali; boyut BILINMIYORSA yuzde belirsiz (-1) kalmali.
func TestIlerlemeYapisal(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 1<<20) // 1 MB

	// boyut biliniyor → yuzde/ETA belirlenir
	is := sinaIs()
	o := &ilerlemeOkuyucu{
		r: bytes.NewReader(data), is: is, etiket: "sina.exe",
		boyut: int64(len(data)), baslama: time.Now().Add(-time.Second), // 1 sn once → hiz olculur
	}
	buf := make([]byte, 64<<10)
	for {
		if _, err := o.Read(buf); err != nil {
			break
		}
	}
	p := is.Ilerleme
	if p.Asama != "indiriliyor" || p.Etiket != "sina.exe" {
		t.Fatalf("asama/etiket yanlis: %+v", p)
	}
	if p.ByteInen != int64(len(data)) || p.ByteToplam != int64(len(data)) {
		t.Fatalf("bayt yanlis: inen=%d toplam=%d", p.ByteInen, p.ByteToplam)
	}
	if p.Yuzde != 100 {
		t.Fatalf("yuzde = %d, beklenen 100", p.Yuzde)
	}
	if p.HizBps <= 0 {
		t.Fatalf("hiz hesaplanmadi: %d", p.HizBps)
	}
	if p.KalanSn < 0 {
		t.Fatalf("tam inende ETA negatif olmamali: %d", p.KalanSn)
	}

	// boyut bilinmiyor (0) → yuzde belirsiz (-1), ETA belirsiz (-1)
	is2 := sinaIs()
	o2 := &ilerlemeOkuyucu{
		r: bytes.NewReader(data), is: is2, etiket: "n.bin",
		boyut: 0, baslama: time.Now().Add(-time.Second),
	}
	for {
		if _, err := o2.Read(buf); err != nil {
			break
		}
	}
	if is2.Ilerleme.Yuzde != -1 || is2.Ilerleme.KalanSn != -1 {
		t.Fatalf("boyut bilinmiyorken yuzde/ETA belirsiz (-1) olmali: %+v", is2.Ilerleme)
	}
	if is2.Ilerleme.ByteInen != int64(len(data)) {
		t.Fatalf("boyutsuz inen bayt yine sayilmali: %d", is2.Ilerleme.ByteInen)
	}
}
