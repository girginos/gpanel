package bayikaynak

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tek bayi kapsamlı tur (hosting oluşturma / bayi güncelleme) yalnız O bayinin hostinglerine dokunur.
func TestProjeUygula_TekBayiKapsami(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	projeSeed(t, db, o) // bayi 3 (1000 MB): c_d1, c_d2; bayi 4 havuzsuz: c_e1
	bayiEkle(t, db, 5, "bayie", 0, 0, 2000, 0)
	hostingEkle(t, db, 10, "f1.test", "c_f1", 5, 100)
	evKur(t, o, "c_f1")
	projeSahte(o, false, "", "")
	ctx := context.Background()

	if _, err := ProjeUygula(ctx, db, 5); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(projeSatirlari(o), "\n")
	if !strings.Contains(got, filepath.Join(o.Ev, "c_f1")+" 3000005") {
		t.Errorf("bayi 5 hostingi atanmadı:\n%s", got)
	}
	if strings.Contains(got, "c_d1") || strings.Contains(got, "c_d2") || projeIsaretiOku("c_d1") != 0 {
		t.Errorf("tek bayi turu başka bayinin hostingine dokundu:\n%s", got)
	}
	// Tüm bayiler turu kalanı tamamlar; c_f1'e tekrar dokunmaz (idempotent).
	o.Komut.Sifirla()
	if _, err := ProjeUygula(ctx, db, 0); err != nil {
		t.Fatal(err)
	}
	got = strings.Join(projeSatirlari(o), "\n")
	if !strings.Contains(got, "c_d1") || !strings.Contains(got, "c_d2") || strings.Contains(got, "c_f1") {
		t.Errorf("tam tur:\n%s", got)
	}
}

// İşaret dosyası panel aralığının dışını gösteriyorsa YOK sayılır: panel başka bir projeyi
// (docker, elle atanmış proje) temizlemeye kalkmaz.
func TestProjeIsaretiOku_PanelAraligiDisiYoksayilir(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	projeSeed(t, db, o)
	projeSahte(o, false, "", "")
	for ad, c := range map[string]struct {
		icerik string
		bek    int64
	}{
		"c_d1": {"3000003\n", 3000003}, "c_d2": {"5\n", 0}, "c_e1": {"-7\n", 0}, "c_x": {"abc\n", 0}, "c_y": {"3000000\n", 0}, "c_z": {"", 0},
	} {
		if err := os.MkdirAll(projeIsaretDizini, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(projeIsaretDizini, ad), []byte(c.icerik), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := projeIsaretiOku(ad); got != c.bek {
			t.Errorf("işaret %q (%q) → %d (beklenen %d)", ad, strings.TrimSpace(c.icerik), got, c.bek)
		}
	}
	// c_e1 havuzsuz bayide, işareti "-7" (geçersiz): temizleme komutu ÇALIŞMAZ (yabancı proje yok sayılır).
	if _, err := ProjeUygula(context.Background(), db, 0); err != nil {
		t.Fatal(err)
	}
	for _, l := range projeSatirlari(o) {
		if strings.Contains(l, "project -C") {
			t.Errorf("panel aralığı dışındaki işaret için temizleme yapıldı: %s", l)
		}
	}
}
