package bayikaynak

// proje_artik_test.go — silinen hostinglerin proje işaret dosyaları tam turda silinir
// (hosting silme işareti kaldırmıyordu; VM doğrulaması 2026-10-13).

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestProjeUygula_SilinenHostingIsaretleriTemizlenir(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	projeSeed(t, db, o)
	projeSahte(o, false, "", "")
	if err := os.MkdirAll(o.Proje, 0o755); err != nil {
		t.Fatal(err)
	}
	for ad, icerik := range map[string]string{"c_silindi": "3000003\n", "BENIOKU": "panel dışı\n"} {
		if err := os.WriteFile(filepath.Join(o.Proje, ad), []byte(icerik), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ProjeUygula(context.Background(), db, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(o.Proje, "c_silindi")); !os.IsNotExist(err) {
		t.Error("silinen hostingin işaret dosyası kaldı")
	}
	if _, err := os.Stat(filepath.Join(o.Proje, "BENIOKU")); err != nil {
		t.Error("hesap adı olmayan dosyaya dokunulmamalı")
	}
	if projeIsaretiOku("c_d1") != 3000003 {
		t.Error("mevcut hostingin işareti silinmemeli")
	}
}
