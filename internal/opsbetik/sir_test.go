package opsbetik

// girginospanel-sir davranış testleri — CANLI SİSTEME DOKUNMAZ: betik
// GOSP_SIR_KUTUPHANE=1 ile kaynak olarak yüklenir, tüm yollar SIR_KOK geçici
// köküne yönlenir, daemon-reload SIR_DAEMON_RELOAD ile sahtelenir.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func sirBetik(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs("../../assets/ops/girginospanel-sir")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// sirKos: betiği kütüphane olarak yükleyip govde'yi koşar; (çıktı, çıkış kodu).
func sirKos(t *testing.T, kok string, ek []string, govde string) (string, int) {
	t.Helper()
	betik := `f=$1; shift; set --; source "$f" || exit 90; ` + govde
	cmd := exec.Command("bash", "-c", betik, "bash", sirBetik(t))
	cmd.Env = append(os.Environ(), "GOSP_SIR_KUTUPHANE=1", "LC_ALL=C", "SIR_KOK="+kok, "SIR_DAEMON_RELOAD=true")
	cmd.Env = append(cmd.Env, ek...)
	out, err := cmd.CombinedOutput()
	kod := 0
	if ee, ok := err.(*exec.ExitError); ok {
		kod = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return string(out), kod
}

const eskiBirim = `[Unit]
Description=GirginOSPanel backend

[Service]
Type=simple
EnvironmentFile=/etc/girginospanel/env
ExecStart=/opt/girginospanel/bin/girginospanel-server
Restart=always
`

func birimKok(t *testing.T, envIcerik string) string {
	t.Helper()
	kok := t.TempDir()
	for _, d := range []string{"etc/systemd/system", "etc/girginospanel"} {
		if err := os.MkdirAll(filepath.Join(kok, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(kok, "etc/systemd/system/girginospanel.service"), []byte(eskiBirim), 0o644); err != nil {
		t.Fatal(err)
	}
	if envIcerik != "" {
		if err := os.WriteFile(filepath.Join(kok, "etc/girginospanel/env"), []byte(envIcerik), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return kok
}

var iyiEnv = "PANEL_LISTEN=127.0.0.1:8080\nPANEL_JWT_SECRET=" + strings.Repeat("x", 40) + "\n"

func TestSirBirim_GecisVeGeri(t *testing.T) {
	kok := birimKok(t, iyiEnv)
	birim := filepath.Join(kok, "etc/systemd/system/girginospanel.service")
	out, kod := sirKos(t, kok, []string{"SIR_SYSTEMD_SURUM=257"}, `sir_birim_gecis; exit $?`)
	if kod != 0 {
		t.Fatalf("kod=%d %s", kod, out)
	}
	b, _ := os.ReadFile(birim)
	s := string(b)
	if !strings.Contains(s, "\nLoadCredential=panel-env:/etc/girginospanel/env\n") || strings.Contains(s, "EnvironmentFile=") {
		t.Fatalf("birim dönüşmedi:\n%s", s)
	}
	if !strings.Contains(s, "ExecStart=/opt/girginospanel/bin/girginospanel-server") {
		t.Error("diğer satırlar kayboldu")
	}
	st, _ := os.Stat(birim)
	if st.Mode().Perm() != 0o644 {
		t.Errorf("birim modu %o", st.Mode().Perm())
	}
	// idempotent
	if _, kod := sirKos(t, kok, []string{"SIR_SYSTEMD_SURUM=257"}, `sir_birim_gecis; exit $?`); kod != 3 {
		t.Errorf("ikinci geçiş kodu %d (3=zaten)", kod)
	}
	if out, _ := sirKos(t, kok, nil, `sir_birim_durum "$SIR_BIRIM"`); strings.TrimSpace(out) != "kimlik" {
		t.Errorf("durum %q", out)
	}
	// geri alma (güncelleme rollback'i)
	if out, kod := sirKos(t, kok, nil, `sir_birim_geri; exit $?`); kod != 0 {
		t.Fatalf("geri: %d %s", kod, out)
	}
	if b, _ := os.ReadFile(birim); string(b) != eskiBirim {
		t.Errorf("eski birim geri gelmedi:\n%s", b)
	}
}

// Önkoşul düşerse birime DOKUNULMAZ (LoadCredential kaynağı yoksa servis açılmaz).
func TestSirBirim_OnkosulDuserseDokunulmaz(t *testing.T) {
	durumlar := map[string]struct {
		env string
		ek  []string
		hz  func(kok string)
	}{
		"env yok":      {"", []string{"SIR_SYSTEMD_SURUM=257"}, nil},
		"jwt yok":      {"PANEL_LISTEN=x\n", []string{"SIR_SYSTEMD_SURUM=257"}, nil},
		"systemd eski": {iyiEnv, []string{"SIR_SYSTEMD_SURUM=239"}, nil},
		"env symlink": {"", []string{"SIR_SYSTEMD_SURUM=257"}, func(kok string) {
			gercek := filepath.Join(kok, "baska")
			_ = os.WriteFile(gercek, []byte(iyiEnv), 0o600)
			_ = os.Symlink(gercek, filepath.Join(kok, "etc/girginospanel/env"))
		}},
		"reload düşer": {iyiEnv, []string{"SIR_SYSTEMD_SURUM=257", "SIR_DAEMON_RELOAD=false"}, nil},
	}
	for ad, d := range durumlar {
		t.Run(ad, func(t *testing.T) {
			kok := birimKok(t, d.env)
			if d.hz != nil {
				d.hz(kok)
			}
			out, kod := sirKos(t, kok, d.ek, `sir_birim_gecis; exit $?`)
			if kod != 1 {
				t.Errorf("kod=%d %s", kod, out)
			}
			if b, _ := os.ReadFile(filepath.Join(kok, "etc/systemd/system/girginospanel.service")); string(b) != eskiBirim {
				t.Errorf("önkoşul düştüğü halde birim değişti:\n%s", b)
			}
		})
	}
}

func izinKok(t *testing.T) string {
	t.Helper()
	kok := t.TempDir()
	if err := os.MkdirAll(filepath.Join(kok, "etc/girginospanel"), 0o755); err != nil {
		t.Fatal(err)
	}
	return kok
}

func yazMod(t *testing.T, p string, m os.FileMode) {
	t.Helper()
	if err := os.WriteFile(p, []byte("gizli\n"), m); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, m); err != nil {
		t.Fatal(err)
	}
}

func modu(t *testing.T, p string) os.FileMode {
	t.Helper()
	st, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	return st.Mode().Perm()
}

func TestSirIzin_DenetleVeOnar(t *testing.T) {
	rootGerek(t)
	kok := izinKok(t)
	env := filepath.Join(kok, "etc/girginospanel/env")
	yedek := filepath.Join(kok, "etc/girginospanel/env.yedek.port.1")
	dbkey := filepath.Join(kok, "etc/girginospanel/db.key")
	yazMod(t, env, 0o644)
	yazMod(t, yedek, 0o640)
	yazMod(t, dbkey, 0o600)
	// denetle: KRİTİK, dosyalara DOKUNMAZ
	out, kod := sirKos(t, kok, nil, `sir_izin; exit $?`)
	if kod != 1 || strings.Count(out, "KRİTİK") < 2 {
		t.Fatalf("kod=%d\n%s", kod, out)
	}
	if modu(t, env) != 0o644 {
		t.Error("denetle modu dosyayı değiştirdi")
	}
	// kuru onar: dokunmaz
	if _, kod := sirKos(t, kok, nil, `sir_izin --onar --kuru; exit $?`); kod != 1 || modu(t, env) != 0o644 {
		t.Error("kuru onar dosyayı değiştirdi ya da temiz döndü")
	}
	out, kod = sirKos(t, kok, nil, `sir_izin --onar; exit $?`)
	if kod != 0 {
		t.Fatalf("onar kodu %d\n%s", kod, out)
	}
	if modu(t, env) != 0o600 || modu(t, yedek) != 0o600 || modu(t, dbkey) != 0o600 {
		t.Errorf("onarım sonrası modlar: env=%o yedek=%o dbkey=%o", modu(t, env), modu(t, yedek), modu(t, dbkey))
	}
	if _, kod := sirKos(t, kok, nil, `sir_izin; exit $?`); kod != 0 {
		t.Error("onarım sonrası denetim temiz değil")
	}
}

// Fazla izin SOYULUR, eksik izin EKLENMEZ (0400 kalır); yanlış sahip düzeltilir.
func TestSirIzin_SahipVeSoyma(t *testing.T) {
	rootGerek(t)
	kok := izinKok(t)
	p := filepath.Join(kok, "etc/girginospanel/proxy.secret")
	yazMod(t, p, 0o404)
	if err := os.Chown(p, kiraciUID, kiraciUID); err != nil {
		t.Fatal(err)
	}
	ro := filepath.Join(kok, "etc/girginospanel/db.key")
	yazMod(t, ro, 0o400)
	out, kod := sirKos(t, kok, nil, `sir_izin --onar; exit $?`)
	if kod != 0 {
		t.Fatalf("%d %s", kod, out)
	}
	if d := oku(t, p); d.uid != 0 || d.gid != 0 || d.mod.Perm() != 0o400 {
		t.Errorf("proxy.secret: uid=%d gid=%d mod=%o", d.uid, d.gid, d.mod.Perm())
	}
	if modu(t, ro) != 0o400 {
		t.Error("0400 dosyaya izin EKLENDİ")
	}
}

// Symlink gizli dosya: takip EDİLMEZ (hedefin izni değişmez), KRİTİK raporlanır.
func TestSirIzin_SymlinkTakipEtmez(t *testing.T) {
	rootGerek(t)
	kok := izinKok(t)
	hedef := filepath.Join(kok, "hedef")
	yazMod(t, hedef, 0o644)
	if err := os.Symlink(hedef, filepath.Join(kok, "etc/girginospanel/env")); err != nil {
		t.Fatal(err)
	}
	out, kod := sirKos(t, kok, nil, `sir_izin --onar; exit $?`)
	if kod != 1 || !strings.Contains(out, "symlink") {
		t.Fatalf("kod=%d %s", kod, out)
	}
	if modu(t, hedef) != 0o644 {
		t.Error("symlink hedefi chmod edildi")
	}
}

func TestSirBetik_Sozdizimi(t *testing.T) {
	for _, b := range []string{"girginospanel-sir", "girginospanel-update"} {
		p, _ := filepath.Abs("../../assets/ops/" + b)
		if out, err := exec.Command("bash", "-n", p).CombinedOutput(); err != nil {
			t.Errorf("%s: bash -n: %v %s", b, err, out)
		}
	}
	p, _ := filepath.Abs("../../girginospanel-install.sh")
	if out, err := exec.Command("bash", "-n", p).CombinedOutput(); err != nil {
		t.Errorf("install: %v %s", err, out)
	}
}

// Güncelleme betiği: birimi restart'tan ÖNCE geçirir, geri alma dalı birimi geri koyar,
// tek seferlik yenileme yalnız SAĞLIKLI dalda ve işaretle.
func TestGuncelleme_SirAdimSirasi(t *testing.T) {
	p, _ := filepath.Abs("../../assets/ops/girginospanel-update")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	gecis := strings.Index(s, `bash "$SIR_ARAC" birim ||`)
	restart := strings.Index(s, "\nsystemctl restart girginospanel\nHEALTHY=0")
	saglikli := strings.Index(s, `if [ "$HEALTHY" = 1 ]; then`)
	yenile := strings.Index(s, `"$BIN" sir yenile --hepsi`)
	geri := strings.Index(s, `bash "$SIR_ARAC" birim-geri`)
	rollback := strings.Index(s, `warn "yeni sürüm sağlıklı başlamadı`)
	if gecis < 0 || restart < 0 || gecis > restart {
		t.Errorf("birim geçişi restart'tan önce değil (%d, %d)", gecis, restart)
	}
	if yenile < saglikli || yenile > rollback {
		t.Errorf("tek seferlik yenileme sağlıklı dalda değil (%d %d %d)", saglikli, yenile, rollback)
	}
	if geri < rollback {
		t.Errorf("birim geri alma, geri alma dalında değil (%d %d)", geri, rollback)
	}
	if !strings.Contains(s, "SIR_ISARET=/var/lib/girginospanel/sir-yenile-2026-10") {
		t.Error("tek seferlik işaret yok")
	}
}

// İzni ve grubu doğru ama SAHİBİ yanlış gizli dosya (kiracı uid'si 0600 ile okur/yazar)
// KRİTİK'tir ve onarımda root'a alınır.
func TestSirIzin_YalnizSahipYanlis(t *testing.T) {
	rootGerek(t)
	kok := izinKok(t)
	p := filepath.Join(kok, "etc/girginospanel/db.key")
	yazMod(t, p, 0o600)
	if err := os.Chown(p, kiraciUID, 0); err != nil {
		t.Fatal(err)
	}
	out, kod := sirKos(t, kok, nil, `sir_izin; exit $?`)
	if kod != 1 || !strings.Contains(out, "sahip") {
		t.Fatalf("yanlış sahip yakalanmadı: %d %s", kod, out)
	}
	if _, kod := sirKos(t, kok, nil, `sir_izin --onar; exit $?`); kod != 0 {
		t.Fatal("onarım temiz dönmedi")
	}
	if d := oku(t, p); d.uid != 0 || d.mod.Perm() != 0o600 {
		t.Errorf("uid=%d mod=%o", d.uid, d.mod.Perm())
	}
}
