package opsbetik

// WAF kuruluyken `httpd_execmem` (libmodsecurity PCRE2 JIT): repair selinux_temel + dogrula +
// waf-setup. Sahte getsebool/setsebool/getenforce; canlı sisteme dokunulmaz.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// wafKur: WAF modülü (ya da yalnız ABI damgası) varmış gibi işaretler.
func (o *selOrtam) wafKur(t *testing.T, dosya string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(o.dir, dosya), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func execmemYazildi(lg string) bool { return strings.Contains(lg, "httpd_execmem=1") }

func TestWafExecmem_ModulVarKapaliysaAcar(t *testing.T) {
	for _, isaret := range []string{"waf.so", "waf.damga"} {
		o := selKur(t)
		o.bool(t, "httpd_execmem", "off")
		o.wafKur(t, isaret)
		out, lg := o.kos(t, 1, 1, 0, "sec_selinux_temel")
		if !execmemYazildi(lg) {
			t.Fatalf("%s: WAF var + boolean kapalı → açılmalı:\n%s\n%s", isaret, out, lg)
		}
		if n := strings.Count(lg, "setsebool "); n != 1 {
			t.Fatalf("tek setsebool -P bekleniyordu (%d):\n%s", n, lg)
		}
		if strings.Contains(lg, "setenforce") {
			t.Fatal("mod değiştirilmemeli")
		}
	}
}

func TestWafExecmem_ModulYoksaDokunmaz(t *testing.T) {
	o := selKur(t)
	o.bool(t, "httpd_execmem", "off")
	out, lg := o.kos(t, 1, 1, 0, "sec_selinux_temel")
	if strings.Contains(lg, "httpd_execmem") || strings.Contains(out, "httpd_execmem") {
		t.Fatalf("WAF yokken httpd_execmem'e dokunulmamalı:\n%s\n%s", out, lg)
	}
	// açıksa da KAPATILMAZ
	o2 := selKur(t)
	o2.bool(t, "httpd_execmem", "on")
	_, lg = o2.kos(t, 1, 1, 0, "sec_selinux_temel")
	if strings.Contains(lg, "httpd_execmem") {
		t.Fatalf("WAF yokken açık boolean'a dokunulmamalı:\n%s", lg)
	}
}

func TestWafExecmem_DryRunYazmaz(t *testing.T) {
	o := selKur(t)
	o.bool(t, "httpd_execmem", "off")
	o.wafKur(t, "waf.so")
	out, lg := o.kos(t, 1, 1, 1, "sec_selinux_temel")
	if strings.Contains(lg, "setsebool") {
		t.Fatalf("dry-run yazdı:\n%s", lg)
	}
	if !strings.Contains(out, "httpd_execmem is off") {
		t.Fatalf("dry-run düzeltilecek boolean'ı söylemeli:\n%s", out)
	}
}

func TestWafExecmem_ZatenAcikkenYazmaz(t *testing.T) {
	o := selKur(t)
	for _, b := range []string{"httpd_can_network_connect", "httpd_can_network_connect_db", "httpd_enable_homedirs", "httpd_read_user_content", "httpd_execmem"} {
		o.bool(t, b, "on")
	}
	o.wafKur(t, "waf.so")
	out, lg := o.kos(t, 1, 1, 0, "sec_selinux_temel")
	if strings.Contains(lg, "setsebool") {
		t.Fatalf("zaten açıkken setsebool çağrıldı:\n%s", lg)
	}
	if !strings.Contains(out, "✓ boolean httpd_execmem=on") {
		t.Fatalf("açık boolean raporlanmalı:\n%s", out)
	}
}

func TestWafExecmem_SELinuxKapaliSessiz(t *testing.T) {
	o := selKur(t)
	o.bool(t, "httpd_execmem", "off")
	o.wafKur(t, "waf.so")
	_, lg := o.kos(t, 0, 1, 0, "sec_selinux_temel")
	if strings.TrimSpace(lg) != "" {
		t.Fatalf("SELinux kapalıyken komut çalıştı:\n%s", lg)
	}
}

// ---- dogrula ----

func TestWafExecmem_DogrulaUyarisi(t *testing.T) {
	o := selKur(t)
	o.bool(t, "httpd_execmem", "off")
	o.wafKur(t, "waf.so")
	o.dogrulaAraclari(t, "Enforcing")
	out := o.dogrulaKos(t)
	if dg(out, "SELinux booleans") != "uyar" || !strings.Contains(out, "httpd_execmem") || !strings.Contains(out, "AVC") || !strings.Contains(out, "JIT") {
		t.Fatalf("WAF var + execmem kapalı → JIT/AVC uyarısı:\n%s", out)
	}
	if !strings.Contains(out, "kritik=0") {
		t.Fatalf("yalnız uyarı olmalı:\n%s", out)
	}
}

func TestWafExecmem_DogrulaWafYoksaKontrolYok(t *testing.T) {
	o := selKur(t)
	o.bool(t, "httpd_execmem", "off")
	o.dogrulaAraclari(t, "Enforcing")
	out := o.dogrulaKos(t)
	if strings.Contains(out, "httpd_execmem") || dg(out, "SELinux booleans") != "gec" {
		t.Fatalf("WAF yokken execmem ölçülmemeli:\n%s", out)
	}
}

func TestWafExecmem_DogrulaAcikGecer(t *testing.T) {
	o := selKur(t)
	o.bool(t, "httpd_execmem", "on")
	o.wafKur(t, "waf.so")
	o.dogrulaAraclari(t, "Enforcing")
	out := o.dogrulaKos(t)
	if dg(out, "SELinux booleans") != "gec" || !strings.Contains(out, "5 on") {
		t.Fatalf("WAF var + execmem açık geçmeli (5 on):\n%s", out)
	}
}

// ---- waf-setup (bölüm fonksiyonu; derleme çalıştırılmaz) ----

func (o *selOrtam) wafSetupKos(t *testing.T, mod string, getsebool bool) string {
	t.Helper()
	yol, err := filepath.Abs("../../assets/ops/girginospanel-waf-setup")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(o.bin, "getenforce"), []byte("#!/bin/bash\necho "+mod+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if mod == "yok" {
		_ = os.Remove(filepath.Join(o.bin, "getenforce"))
	}
	if !getsebool {
		_ = os.Remove(filepath.Join(o.bin, "getsebool"))
	}
	cmd := exec.Command("bash", "-c", `f=$1; set --; source "$f" || exit 90; waf_selinux_execmem; echo "RC=$?"`, "bash", yol)
	// PATH: yalnız sahte bin + sistem; gerçek getenforce/getsebool varsa sahte önde gelir.
	cmd.Env = append(os.Environ(), "GOSP_WAF_KUTUPHANE=1", "LC_ALL=C", "PATH="+o.bin+":"+os.Getenv("PATH"), "ST="+o.st, "LOG="+o.log)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("waf-setup bölümü hatası (%v): %s", err, out)
	}
	return string(out)
}

func TestWafSetup_ExecmemKapaliAcar(t *testing.T) {
	for _, mod := range []string{"Enforcing", "Permissive"} {
		o := selKur(t)
		o.bool(t, "httpd_execmem", "off")
		out := o.wafSetupKos(t, mod, true)
		lg, _ := os.ReadFile(o.log)
		if !strings.Contains(string(lg), "setsebool -P httpd_execmem 1") || !strings.Contains(out, "RC=0") {
			t.Fatalf("%s: setsebool -P httpd_execmem 1 bekleniyordu:\n%s\n%s", mod, out, lg)
		}
	}
}

func TestWafSetup_ZatenAcikYazmaz(t *testing.T) {
	o := selKur(t)
	o.bool(t, "httpd_execmem", "on")
	out := o.wafSetupKos(t, "Enforcing", true)
	if lg, _ := os.ReadFile(o.log); strings.Contains(string(lg), "setsebool") {
		t.Fatalf("zaten açıkken yazıldı:\n%s", lg)
	}
	if !strings.Contains(out, "is already on") {
		t.Fatalf("bilgi satırı bekleniyordu:\n%s", out)
	}
}

func TestWafSetup_DisabledSessizGecer(t *testing.T) {
	o := selKur(t)
	o.bool(t, "httpd_execmem", "off")
	out := o.wafSetupKos(t, "Disabled", true)
	if lg, _ := os.ReadFile(o.log); strings.TrimSpace(string(lg)) != "" || !strings.Contains(out, "RC=0") {
		t.Fatalf("Disabled'da komut çalıştı:\n%s\n%s", out, lg)
	}
}

func TestWafSetup_SetseboolHatasiKurulumuDusurmez(t *testing.T) {
	o := selKur(t)
	o.bool(t, "httpd_execmem", "off")
	if err := os.WriteFile(filepath.Join(o.bin, "setsebool"), []byte("#!/bin/bash\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out := o.wafSetupKos(t, "Enforcing", true)
	if !strings.Contains(out, "RC=0") || !strings.Contains(out, "FAILED") {
		t.Fatalf("hata uyarıya dönmeli, rc=0:\n%s", out)
	}
}

// Kaynak sözleşmesi: çağrı yalnız son nginx -t kapısından SONRA (rollback dalı die ile çıkar).
func TestWafSetup_CagriNginxTKapisindanSonra(t *testing.T) {
	yol, _ := filepath.Abs("../../assets/ops/girginospanel-waf-setup")
	b, err := os.ReadFile(yol)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	kapi := strings.Index(s, `ok "main nginx -t OK — setup complete"`)
	cagri := strings.Index(s[kapi:], "waf_selinux_execmem")
	rollback := strings.Index(s, "nginx.conf ROLLED BACK")
	if kapi < 0 || cagri < 0 || rollback < 0 || kapi < rollback {
		t.Fatalf("waf_selinux_execmem nginx -t kapısından sonra çağrılmalı (kapi=%d cagri=%d rb=%d)", kapi, cagri, rollback)
	}
}
