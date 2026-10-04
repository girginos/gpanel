package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"girginospanel/internal/sirrota"
)

type cliSahteServis struct {
	onceSaglikli  bool
	sonraSaglikli []bool // her YenidenBaslat sonrası Saglikli sonucu sırayla
	baslatma      int
	denenen       []string
}

func (s *cliSahteServis) Saglikli(context.Context, time.Duration) error {
	if s.baslatma == 0 {
		if s.onceSaglikli {
			return nil
		}
		return errors.New("sağlıksız")
	}
	i := s.baslatma - 1
	if i < len(s.sonraSaglikli) && s.sonraSaglikli[i] {
		return nil
	}
	return errors.New("sağlıksız")
}

func (s *cliSahteServis) YenidenBaslat(context.Context) error { s.baslatma++; return nil }
func (s *cliSahteServis) DeneYenidenBaslat(_ context.Context, b ...string) {
	s.denenen = append(s.denenen, b...)
}

type cliSahteRedis struct{ p string }

func (r *cliSahteRedis) ParolaDegistir(_ context.Context, e, y string) error {
	if e != r.p {
		return errors.New("WRONGPASS")
	}
	r.p = y
	return nil
}
func (r *cliSahteRedis) Dogrula(_ context.Context, p string) error {
	if p != r.p {
		return errors.New("WRONGPASS")
	}
	return nil
}

var cliEnv = "PANEL_LISTEN=127.0.0.1:8080\nPANEL_JWT_SECRET=" + strings.Repeat("x", 40) + "\nPANEL_REDIS_ADMIN_PASS=eskiredis\n"

func cliKur(t *testing.T) (*sirrota.Rotator, *cliSahteRedis, string) {
	t.Helper()
	dir := t.TempDir()
	env := filepath.Join(dir, "env")
	if err := os.WriteFile(env, []byte(cliEnv), 0o600); err != nil {
		t.Fatal(err)
	}
	rd := &cliSahteRedis{p: "eskiredis"}
	return &sirrota.Rotator{EnvYolu: env, BekleyenYolu: filepath.Join(dir, "bek"), EkDizin: dir, Redis: rd}, rd, env
}

// Panel yeni sırlarla sağlıklı açılmazsa HER ŞEY eski sırlara döner (kilitlenme yok).
func TestSirYenileCLI_SagliksizsaGeriDoner(t *testing.T) {
	r, rd, env := cliKur(t)
	sv := &cliSahteServis{onceSaglikli: true, sonraSaglikli: []bool{false, true}}
	var out, hata bytes.Buffer
	kod := sirYenileCLI(context.Background(), r, sv, []string{"jwt", "redis"}, &out, &hata)
	if kod != 1 {
		t.Fatalf("kod=%d hata=%s", kod, hata.String())
	}
	if b, _ := os.ReadFile(env); string(b) != cliEnv {
		t.Error("geri dönüşte env eski haline gelmedi")
	}
	if rd.p != "eskiredis" {
		t.Error("geri dönüşte Valkey eski parolaya dönmedi")
	}
	if sv.baslatma != 2 {
		t.Errorf("yeniden başlatma sayısı %d (yeni + geri dönüş = 2)", sv.baslatma)
	}
}

func TestSirYenileCLI_GeriDonusDeSagliksizsaKritik(t *testing.T) {
	r, _, _ := cliKur(t)
	sv := &cliSahteServis{onceSaglikli: true, sonraSaglikli: []bool{false, false}}
	var out, hata bytes.Buffer
	if kod := sirYenileCLI(context.Background(), r, sv, []string{"--hepsi"}, &out, &hata); kod != 3 {
		t.Fatalf("kod=%d", kod)
	}
	if !strings.Contains(hata.String(), "KRİTİK") {
		t.Error("KRİTİK mesajı yok")
	}
}

// Panel ŞU AN sağlıksızsa yenileme REDDEDİLİR, hiçbir şeye dokunulmaz.
func TestSirYenileCLI_OnceSagliksizsaRed(t *testing.T) {
	r, rd, env := cliKur(t)
	sv := &cliSahteServis{onceSaglikli: false}
	var out, hata bytes.Buffer
	if kod := sirYenileCLI(context.Background(), r, sv, []string{"--hepsi"}, &out, &hata); kod != 1 {
		t.Fatalf("kod=%d", kod)
	}
	if b, _ := os.ReadFile(env); string(b) != cliEnv || rd.p != "eskiredis" || sv.baslatma != 0 {
		t.Error("sağlıksız panelde sırlara dokunuldu")
	}
}

func TestSirYenileCLI_Basari(t *testing.T) {
	r, rd, env := cliKur(t)
	sv := &cliSahteServis{onceSaglikli: true, sonraSaglikli: []bool{true}}
	var out, hata bytes.Buffer
	if kod := sirYenileCLI(context.Background(), r, sv, []string{"--hepsi"}, &out, &hata); kod != 0 {
		t.Fatalf("kod=%d hata=%s", kod, hata.String())
	}
	b, _ := os.ReadFile(env)
	if string(b) == cliEnv || rd.p == "eskiredis" || !strings.Contains(string(b), "PANEL_REDIS_ADMIN_PASS="+rd.p) {
		t.Error("yenileme uygulanmadı")
	}
	// Sır değeri çıktıya sızmamalı
	if strings.Contains(out.String()+hata.String(), rd.p) {
		t.Error("yeni sır stdout/stderr'e yazıldı")
	}
}

// Destek ajanı ayrı hizmet: sırlar yenilenince (kendi LoadCredential kopyası bayatlar) yeniden başlatılır.
func TestSirYenileCLI_DestekAjaniYenidenBaslar(t *testing.T) {
	r, _, _ := cliKur(t)
	sv := &cliSahteServis{onceSaglikli: true, sonraSaglikli: []bool{true}}
	var out, hata bytes.Buffer
	if kod := sirYenileCLI(context.Background(), r, sv, []string{"--hepsi"}, &out, &hata); kod != 0 {
		t.Fatalf("kod=%d hata=%s", kod, hata.String())
	}
	if !slices.Contains(sv.denenen, "girginospanel-destek-ajan.service") {
		t.Fatalf("sır yenilenince destek ajanı try-restart listesinde yok: %v", sv.denenen)
	}
}

func TestSirYenileCLI_Kullanim(t *testing.T) {
	r, _, _ := cliKur(t)
	var o, h bytes.Buffer
	for _, a := range [][]string{nil, {"bilinmeyen"}, {"--saglik-sn"}, {"--saglik-sn", "1", "jwt"}} {
		if kod := sirYenileCLI(context.Background(), r, &cliSahteServis{}, a, &o, &h); kod != 2 {
			t.Errorf("%v → %d", a, kod)
		}
	}
}

func TestBirimKimlikli(t *testing.T) {
	yeni, err := os.ReadFile("../../assets/systemd/girginospanel.service")
	if err != nil {
		t.Fatal(err)
	}
	if !birimKimlikli(string(yeni)) {
		t.Error("paket birimi kimlikli sayılmadı")
	}
	eski := "[Service]\nEnvironmentFile=/etc/girginospanel/env\nExecStart=/x\n"
	if birimKimlikli(eski) {
		t.Error("eski birim kimlikli sayıldı")
	}
	ikisi := "[Service]\nLoadCredential=panel-env:/etc/girginospanel/env\nEnvironmentFile=-/etc/girginospanel/env\n"
	if birimKimlikli(ikisi) {
		t.Error("EnvironmentFile hâlâ varken kimlikli sayıldı")
	}
}
