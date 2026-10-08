package files

// arsiv_kiraci_test.go — denetim-b: Archive zip/tar'ı KÖK değil KİRACI kimliğinde
// koşmalı (kuyruk bekleme/uzun arşivleme sırasında ara bileşen symlink takası → kök
// jail dışı okuma/yazma). Seam: kiraciKomut sıradan komut döner ve argv'yi kaydeder.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"girginospanel/internal/sahtedb"
)

func TestArsivYurut_KiraciKimligindeCalisir(t *testing.T) {
	kok := t.TempDir()
	if err := os.WriteFile(filepath.Join(kok, "a.txt"), []byte("merhaba"), 0644); err != nil {
		t.Fatal(err)
	}
	cikti := filepath.Join(kok, "o.tar.gz")

	var gorulenSK []string
	var gorulenAd []string
	eski := kiraciKomut
	kiraciKomut = func(ctx context.Context, sk string, argv ...string) (*exec.Cmd, error) {
		gorulenSK = append(gorulenSK, sk)
		gorulenAd = append(gorulenAd, argv[0])
		return exec.CommandContext(ctx, argv[0], argv[1:]...), nil
	}
	defer func() { kiraciKomut = eski }()

	is := &arsivIs{Durum: "calisiyor", sahip: "c_test"}
	hata := ""
	arsivYurut(is, func(m string) { hata = m }, "tar.gz", cikti, kok, []string{"a.txt"}, []string{filepath.Join(kok, "a.txt")}, "c_test")
	if hata != "" {
		t.Fatalf("arşivleme hata verdi: %s", hata)
	}
	if len(gorulenSK) == 0 || gorulenSK[0] != "c_test" || gorulenAd[0] != "tar" {
		t.Fatalf("tar kiracı kimliğinde kurulmadı: sk=%v ad=%v", gorulenSK, gorulenAd)
	}
	if _, err := os.Stat(cikti); err != nil {
		t.Fatalf("çıktı yok: %v", err)
	}
	if is.Durum != "tamam" {
		t.Fatalf("durum=%q", is.Durum)
	}
}

func TestArsivYurut_KiraciKomutuKurulamazsaCalistirmaz(t *testing.T) {
	kok := t.TempDir()
	cikti := filepath.Join(kok, "o.zip")
	eski := kiraciKomut
	kiraciKomut = func(ctx context.Context, sk string, argv ...string) (*exec.Cmd, error) {
		return nil, os.ErrPermission // ör. sk uid 0 / kullanıcı yok → KÖK'e düşme YOK
	}
	defer func() { kiraciKomut = eski }()
	is := &arsivIs{Durum: "calisiyor", sahip: "c_test"}
	hata := ""
	arsivYurut(is, func(m string) { hata = m }, "zip", cikti, kok, []string{"x"}, []string{filepath.Join(kok, "x")}, "c_test")
	if hata == "" {
		t.Fatal("kiracı komutu kurulamayınca iş hata ile bitmeli (kök yedek yol yok)")
	}
	if _, err := os.Stat(cikti); err == nil {
		t.Fatal("kök kimliğiyle arşiv üretilmemeli")
	}
}

// du ve find de KİRACI kimliğinde (jail dışı ağaç boyutu/adı sızıntısı + bağlamsız süreç).
func TestBoyutHesapla_Ara_KiraciKimligindeCalisir(t *testing.T) {
	var cagrilar [][]string
	eski := kiraciKomut
	kiraciKomut = func(ctx context.Context, sk string, argv ...string) (*exec.Cmd, error) {
		cagrilar = append(cagrilar, append([]string{sk}, argv...))
		return exec.CommandContext(ctx, "true"), nil
	}
	defer func() { kiraciKomut = eski }()

	gonder := func(yol string, f http.HandlerFunc) {
		req := httptest.NewRequest(http.MethodGet, yol, nil)
		f(httptest.NewRecorder(), req)
	}
	sd1, db := sahtedb.Yeni()
	sd1.Satirlar("SELECT sistem_kullanici, is_demo FROM domains", []any{"c_dbtest", int64(0)})
	h := &Handlers{DB: db}
	gonder("/x?yol=/", h.BoyutHesapla)
	sd2, dbB := sahtedb.Yeni()
	sd2.Satirlar("SELECT sistem_kullanici, is_demo FROM domains", []any{"c_dbtest", int64(0)})
	h2 := &Handlers{DB: dbB}
	gonder("/x?q=abc", h2.Ara)

	var du, find bool
	for _, c := range cagrilar {
		if c[0] != "c_dbtest" {
			t.Fatalf("yanlış kimlik: %v", c)
		}
		du = du || c[1] == "du"
		find = find || c[1] == "find"
	}
	if !du || !find {
		t.Fatalf("du/find kiracı kimliğinde çalışmadı: du=%v find=%v çağrılar=%v", du, find, cagrilar)
	}
}
