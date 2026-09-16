package hesaplar

import "testing"

func TestFTPParolaHashDogrula(t *testing.T) {
	pw := "FtpGizli!2026"
	h := FTPParolaHash(pw)
	if !IsFTPHash(h) {
		t.Fatalf("hash $6$ ile baslamiyor: %q", h)
	}
	if !FTPParolaDogrula(pw, h) {
		t.Error("dogru parola dogrulanamadi")
	}
	if FTPParolaDogrula("yanlis", h) {
		t.Error("yanlis parola KABUL edildi")
	}
	// eski duz-metin satir (migrate edilmemis) — geriye-uyumlu
	if !FTPParolaDogrula("duztext", "duztext") {
		t.Error("eski duz-metin dogrulanamadi")
	}
	if FTPParolaDogrula("x", "duztext") {
		t.Error("yanlis duz-metin KABUL edildi")
	}
	if FTPParolaDogrula("x", "") {
		t.Error("bos saklanan KABUL edildi")
	}
	// tuz rastgele: ayni parola iki farkli hash
	if FTPParolaHash(pw) == h {
		t.Error("iki hash ayni — tuz rastgele degil")
	}
}
