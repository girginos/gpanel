package bayikaynak

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bosDilimSahte: is-active → active; TasksCurrent → gorev (hata ise err); stop → başarı.
func bosDilimSahte(o *ortam, gorev string, gorevHata error) {
	o.Komut.Yanit = func(satir string) (string, error, bool) {
		switch {
		case strings.HasPrefix(satir, "systemctl is-active"):
			return "active\n", nil, true
		case strings.HasPrefix(satir, "systemctl show") && strings.Contains(satir, "TasksCurrent"):
			return gorev + "\n", gorevHata, true
		case strings.HasPrefix(satir, "systemctl stop") && o.durdurHata != nil:
			return "Job failed\n", o.durdurHata, true
		}
		return "", nil, false
	}
}

// Silinen bayinin dilimi ETKİN ama BOŞ: systemd kendiliğinden durdurmaz → durdurulup silinir
// (eskiden limitsiz kalıba çevrilip yeniden başlatmaya dek kalıyordu).
func TestBayiTemizle_BosEtkinDilimDurdurulupSilinir(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	seedBayiler(t, db, o)
	ctx := context.Background()
	if _, err := Uzlastir(ctx, db); err != nil {
		t.Fatal(err)
	}
	bosDilimSahte(o, "0", nil)
	o.Komut.Sifirla()
	BayiTemizle(ctx, db, 1)
	if o.Var(filepath.Join(o.Birim, "girginos-bayi1.slice")) {
		t.Error("boş etkin dilim silinmedi")
	}
	if o.Komut.Say("systemctl stop girginos-bayi1.slice") != 1 {
		t.Errorf("boş dilim durdurulmadı: %v", o.Komut.Satirlar())
	}
}

// Durdurma BAŞARISIZ olursa dilim silinmez (etkin dilimin dosyası silinirse systemd onu
// "not-found" olarak bellekte tutar); limitsiz kalıba döner.
func TestBayiTemizle_DurdurmaHatasindaSilinmez(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	seedBayiler(t, db, o)
	ctx := context.Background()
	if _, err := Uzlastir(ctx, db); err != nil {
		t.Fatal(err)
	}
	o.durdurHata = errors.New("exit status 1")
	bosDilimSahte(o, "0", nil)
	BayiTemizle(ctx, db, 1)
	if !o.Var(filepath.Join(o.Birim, "girginos-bayi1.slice")) {
		t.Error("durdurulamayan etkin dilimin dosyası silindi")
	}
}

// İçinde süreç varsa (ya da sayı okunamıyorsa) DURDURULMAZ: limitsiz kalıba döner, dosya kalır.
func TestBayiTemizle_SurecliDilimDurdurulmaz(t *testing.T) {
	for _, d := range []struct {
		ad, gorev string
		hata      error
	}{
		{"süreç var", "3", nil},
		{"muhasebe kapalı", "[not set]", nil},
		{"komut hatası", "", errors.New("exit status 1")},
	} {
		t.Run(d.ad, func(t *testing.T) {
			o := ortamKur(t)
			db := geciciDB(t)
			seedBayiler(t, db, o)
			ctx := context.Background()
			if _, err := Uzlastir(ctx, db); err != nil {
				t.Fatal(err)
			}
			bosDilimSahte(o, d.gorev, d.hata)
			o.Komut.Sifirla()
			BayiTemizle(ctx, db, 1)
			if y := o.Komut.YikiciVar(); y != "" {
				t.Errorf("süreçli/bilinmeyen dilimde hizmete dokunuldu: %s", y)
			}
			yol := filepath.Join(o.Birim, "girginos-bayi1.slice")
			if !o.Var(yol) {
				t.Fatal("dilim silindi")
			}
			if c := o.Oku(t, yol); strings.Contains(c, "CPUQuota=") && !strings.Contains(c, "CPUQuotaPerSecUSec=infinity") && !strings.Contains(c, "CPUQuota=infinity") {
				t.Errorf("limitsiz kalıba dönmedi:\n%s", c)
			}
		})
	}
}

// Hosting havuzdan çıkınca eski üye dilimi ETKİN ama BOŞ ise durdurulup silinir; süreç varsa bekler.
func TestArtikTemizle_BosEtkinUyeDilimi(t *testing.T) {
	for _, d := range []struct {
		gorev string
		silin bool
	}{{"0", true}, {"5", false}} {
		o := ortamKur(t)
		yol := filepath.Join(o.Birim, "girginos-bayi7-c_eski.slice")
		if err := os.WriteFile(yol, []byte("# GirginOSPanel per-domain resource slice — c_eski\n[Slice]\nMemoryMax=512M\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		bosDilimSahte(o, d.gorev, nil)
		ArtikTemizle(context.Background())
		if o.Var(yol) == d.silin {
			t.Errorf("TasksCurrent=%s: silindi=%v, beklenen %v (%v)", d.gorev, !o.Var(yol), d.silin, o.Komut.Satirlar())
		}
		if dur := o.Komut.Say("systemctl stop girginos-bayi7-c_eski.slice"); (dur == 1) != d.silin {
			t.Errorf("TasksCurrent=%s: stop çağrısı %d", d.gorev, dur)
		}
	}
}
