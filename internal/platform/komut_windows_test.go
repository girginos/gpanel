//go:build windows

// komut_windows_test.go — B-14: komutVeyaDosya, PATH bulamayinca sabit-yol
// os.Stat yedegini kullanmali (bayat surec PATH'i tuzagi).
package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKomutVeyaDosya(t *testing.T) {
	yok := "gosp-olmayan-komut-xyz-123"
	// (a) komut yok + yollar yok -> false
	if komutVeyaDosya(yok, []string{`C:\yok\yok.exe`}) {
		t.Error("olmayan komut + olmayan yol true dondu")
	}
	// (b) komut yok ama SABIT YOL VAR -> true (B-14 yedegi: os.Stat)
	tmp := filepath.Join(t.TempDir(), "sahte.exe")
	if err := os.WriteFile(tmp, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !komutVeyaDosya(yok, []string{`C:\yok\yok.exe`, tmp}) {
		t.Error("var olan sabit yol bulunamadi — os.Stat yedegi calismiyor")
	}
	// (c) PATH'te KESIN var olan komut (cmd) -> true
	if !komutVeyaDosya("cmd", nil) {
		t.Error("PATH'teki cmd bulunamadi")
	}
}
