package tasima

// Sır sızıntısı kilidi (taşıma): kiracı kimliğindeki yerleşim adımları
// (kiraciAdim → kiraci.Komut) ve kaynak sunucuya giden kök ssh/rsync/sshpass
// çocukları panel ortamını (sırlar) TAŞIMAZ. Sahte komutlar kiracitest ile
// PATH'in başına konur; ağa çıkılmaz, kimlik düşürülmez.

import (
	"context"
	"os/user"
	"strings"
	"testing"
	"time"

	"girginospanel/internal/kiraci"
	"girginospanel/internal/kiraci/kiracitest"
)

// ozelDegisken — sır OLMAYAN ama panel ortamında bulunan bir değişken: çocukta
// görünmesi ortamın açıkça değil os.Environ ile kurulduğunu gösterir.
const ozelDegisken = "GOSP_TASIMA_ORTAM_IZI"

func ortamdaVar(env []string, ad string) bool {
	for _, s := range env {
		if strings.HasPrefix(s, ad+"=") {
			return true
		}
	}
	return false
}

func TestSirCocuk_KiraciAdimPanelOrtamiGecmez(t *testing.T) {
	if _, err := user.Lookup("nobody"); err != nil {
		t.Skipf("nobody yok: %v", err)
	}
	kayit := kiracitest.Kur(t, "runuser")
	t.Setenv(ozelDegisken, "1")
	ki := kiraci.Kiraci{Kul: "nobody", Ev: t.TempDir()}
	if err := kiraciAdim(context.Background(), 30*time.Second, ki, "mkdir", "-p", "--", ki.Ev+"/x"); err != nil {
		t.Fatalf("kiraciAdim: %v", err)
	}
	for _, c := range kayit.SizintiYok(t, "runuser", "-u", "nobody", "--", "mkdir") {
		if ortamdaVar(c.Env, ozelDegisken) {
			t.Errorf("kiracı adımı panel ortamını miras aldı: %q", c.Env)
		}
		if !ortamdaVar(c.Env, "HOME") || !ortamdaVar(c.Env, "PATH") {
			t.Errorf("asgari ortam eksik: %q", c.Env)
		}
	}
}

func TestSirCocuk_UzakSshRsyncOrtamiAcik(t *testing.T) {
	kayit := kiracitest.Kur(t, "sshpass")
	t.Setenv(ozelDegisken, "1")
	k := &Kaynak{Tip: "cpanel", Host: "192.0.2.10", Port: 22, Kullanici: "kaynak", Parola: "kaynak-parolasi-7c1"}
	ctx := context.Background()
	if _, err := k.Calistir(ctx, "true"); err != nil {
		t.Fatalf("Calistir: %v", err)
	}
	if _, err := k.RsyncCek(ctx, "/home/kaynak/public_html", t.TempDir()); err != nil {
		t.Fatalf("RsyncCek: %v", err)
	}
	var sb strings.Builder
	if _, err := k.AkisCalistir(ctx, "true", &sb); err != nil {
		t.Fatalf("AkisCalistir: %v", err)
	}
	cs := kayit.SizintiYok(t, "sshpass", "-e")
	if len(cs) != 3 {
		t.Fatalf("3 sshpass çağrısı beklendi, %d", len(cs))
	}
	for i, c := range cs {
		if ortamdaVar(c.Env, ozelDegisken) {
			t.Errorf("çağrı #%d (%q): panel ortamının tamamı verildi", i, c.Argv)
		}
		if !ortamdaVar(c.Env, "SSHPASS") {
			t.Errorf("çağrı #%d: SSHPASS yok", i)
		}
	}
}
