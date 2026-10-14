package provisioner

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// PHPKurulu: phpMap'teki sürümün php-fpm ikilisi var mı; bilinmeyen sürüm 8.3'e DÜŞMEZ.
func TestPHPKurulu(t *testing.T) {
	eski := phpMap["8.6"]
	t.Cleanup(func() { phpMap["8.6"] = eski })
	bin := filepath.Join(t.TempDir(), "php-fpm")
	ay := eski
	ay.FpmBin = bin
	phpMap["8.6"] = ay
	if PHPKurulu("8.6") {
		t.Fatal("ikili yokken kurulu sayıldı")
	}
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !PHPKurulu("8.6") || !PHPKurulu(" 8.6 ") {
		t.Fatal("ikili varken kurulu sayılmalı")
	}
	for _, s := range []string{"9.9", "", "php86", "8.30"} {
		if PHPKurulu(s) {
			t.Errorf("bilinmeyen sürüm %q kurulu sayıldı", s)
		}
	}
}

// Provision: kurulu olmayan sürüm kullanıcı adı ayırmadan/useradd'den ÖNCE ErrPHPKuruluDegil.
func TestProvision_KuruluOlmayanPHP_YanEtkisiz(t *testing.T) {
	eskiK, eskiSK := phpKuruluMu, skKullanimda
	t.Cleanup(func() { phpKuruluMu, skKullanimda = eskiK, eskiSK })
	phpKuruluMu = func(string) bool { return false }
	skKullanimda = func(string) bool {
		t.Error("kullanıcı adı ayrılmamalıydı (useradd'e inilmez)")
		return true
	}
	_, err := Provision("kurulu-degil.test", "8.6")
	if !errors.Is(err, ErrPHPKuruluDegil) {
		t.Fatalf("ErrPHPKuruluDegil beklenirdi: %v", err)
	}
}
