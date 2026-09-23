package guvenlikduvari

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Panel kapıları rastgele porttadır: statik liste onları korumaz. Dinamik liste
// bağlıysa rastgele kapı portu da "kapat" ile kapatılamamalı (panel kendini kilitler).
func TestKorumaliMiDinamik(t *testing.T) {
	eski := EkKorumaliPortlar
	t.Cleanup(func() { EkKorumaliPortlar = eski })

	EkKorumaliPortlar = nil
	if korumaliMi(21001) {
		t.Fatal("dinamik liste yokken rastgele port korumalı olmamalı")
	}
	if !korumaliMi(22) || !korumaliMi(8443) {
		t.Fatal("statik liste korunmalı")
	}
	EkKorumaliPortlar = func() []int { return []int{21001, 8443} }
	if !korumaliMi(21001) {
		t.Fatal("kapı portu korumalı olmalı")
	}
	want := []int{22, 53, 80, 443, 8080, 8443, 21001}
	if got := korumaliListe(); !reflect.DeepEqual(got, want) {
		t.Fatalf("liste artan + tekrarsız olmalı: got %v want %v", got, want)
	}
}

func TestKapatilanPortlar(t *testing.T) {
	yol := filepath.Join(t.TempDir(), "girginos_fw.nft")
	// rebuild çıktısının biçimi: whitelist accept, allowlist drop, kapat drop, ban drop.
	ruleset := "table inet girginos_fw {\n\tchain input {\n" +
		"\t\tip saddr 1.2.3.4 tcp dport 25000 accept\n" +
		"\t\ttcp dport 25000 drop\n" + // port-kısıtlı whitelist → allowlist
		"\t\ttcp dport 3306 drop\n" + // kapat
		"\t\tudp dport 5353 drop\n" + // UDP: panel kapısı TCP, ilgisiz
		"\t\tip saddr 5.6.7.8 tcp dport 26000 drop\n" + // tek IP ban: port herkese kapalı DEĞİL
		"\t}\n}\n"
	if err := os.WriteFile(yol, []byte(ruleset), 0o600); err != nil {
		t.Fatal(err)
	}
	got := kapatilanPortlar(yol)
	if !got[25000] || !got[3306] {
		t.Errorf("herkese kapalı/kısıtlı TCP portları dönmeli: %v", got)
	}
	if got[5353] || got[26000] {
		t.Errorf("UDP ve tek-IP ban kapalı sayılmamalı: %v", got)
	}
	if len(kapatilanPortlar(filepath.Join(t.TempDir(), "yok"))) != 0 {
		t.Error("dosya yoksa boş")
	}
}
