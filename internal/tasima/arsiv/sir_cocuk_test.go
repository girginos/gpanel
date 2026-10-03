package arsiv

// Sır sızıntısı kilidi (arşiv): root panelde kimlik düşüren iki çocuk —
// xz/zstd açıcısı (nobody) ve kiracı kimliğindeki tar çıkarıcısı — kiraci.Komut
// ile başlar, panel ortamını TAŞIMAZ. Sahte runuser kiracitest'ten.

import (
	"context"
	"os"
	"strings"
	"testing"

	"girginospanel/internal/kiraci/kiracitest"
)

const ozelDegisken = "GOSP_ARSIV_ORTAM_IZI"

func panelOrtamiYok(t *testing.T, cs []kiracitest.Cagri) {
	t.Helper()
	for i, c := range cs {
		for _, s := range c.Env {
			if strings.HasPrefix(s, ozelDegisken+"=") {
				t.Errorf("çağrı #%d (%q): panel ortamı miras alındı", i, c.Argv)
			}
		}
	}
}

func TestSirCocuk_AciciPanelOrtamiGecmez(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("açıcı yalnız root panelde kimlik düşürür")
	}
	sahteZstd(t)
	kayit := kiracitest.Kur(t, "runuser")
	t.Setenv(ozelDegisken, "1")
	// Sahte runuser açıcıyı çalıştırmaz → açılan akış boş; sonuç önemsiz.
	_, _ = acT(t, zstdUret(tarUret(t, iyiUyeler()...)), Sinirlar{}, nil)
	cs := kayit.SizintiYok(t, "runuser", "-u", aciciKul, "--")
	panelOrtamiYok(t, cs)
	for i, c := range cs {
		if len(c.Argv) < 4 || !strings.HasPrefix(c.Argv[3], "/") {
			t.Errorf("çağrı #%d: açıcı mutlak yolla verilmeli: %q", i, c.Argv)
		}
	}
}

func TestSirCocuk_CikarPanelOrtamiGecmez(t *testing.T) {
	kayit := kiracitest.Kur(t, "runuser")
	yeniOrtam(t)
	t.Setenv(ozelDegisken, "1")
	a, err := acT(t, gzUret(t, tarUret(t, iyiUyeler()...)), Sinirlar{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Sahte runuser tar'ı çalıştırmaz; çıkarma sonucu önemsiz.
	_, _ = a.Cikar(context.Background(), "./"+secimOnek+"/", sinirsizHedef("public_html"))
	panelOrtamiYok(t, kayit.SizintiYok(t, "runuser", "-u", "nobody", "--", "tar"))
}
