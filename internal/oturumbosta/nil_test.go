package oturumbosta

import "testing"

// DB yokken (erken açılış / test) özellik kapalı sayılır ve hiçbir şey panik
// vermez — middleware.oturumGecerli'nin nil-DB sözleşmesiyle aynı.
func TestNilDBFailOpenPanikYok(t *testing.T) {
	if got := Ayar(nil); got != 0 {
		t.Fatalf("nil DB → özellik kapalı (0) beklenir, got %d", got)
	}
	if IdleMi(nil, 1, 0) {
		t.Fatal("nil DB → oturum boşta SAYILMAMALI (fail-open)")
	}
	SonAktiviteGuncelle(nil, 1) // panik vermemeli (goroutine'de çağrılır)
}
