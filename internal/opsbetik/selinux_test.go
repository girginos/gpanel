package opsbetik

// girginospanel-repair `selinux_temel` + `docroot_selinux`: kurulum ve update'in ORTAK SELinux
// garantisi. CANLI SİSTEME DOKUNMAZ: getsebool/setsebool/semanage/restorecon/matchpathcon/
// chcon/ls -Zd PATH'in başındaki sahte betiklerdir; her çağrı $LOG'a yazılır, durum $ST
// dosyalarındadır. SEL/HAVE_SEMANAGE/DRY doğrudan atanır (getenforce çağrılmaz).

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type selOrtam struct {
	dir, bin, st, log, ev string
}

func selKur(t *testing.T) *selOrtam {
	t.Helper()
	d := t.TempDir()
	o := &selOrtam{dir: d, bin: filepath.Join(d, "bin"), st: filepath.Join(d, "st"), log: filepath.Join(d, "log"), ev: filepath.Join(d, "home")}
	for _, p := range []string{o.bin, o.st, o.ev} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	yaz := func(ad, govde string) {
		if err := os.WriteFile(filepath.Join(o.bin, ad), []byte("#!/bin/bash\n"+govde), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	yaz("getsebool", `f="$ST/bool_$1"; [ -f "$f" ] || exit 1; echo "$1 --> $(cat "$f")"`)
	yaz("setsebool", `echo "setsebool $*" >>"$LOG"; for a in "$@"; do case "$a" in *=1) echo on >"$ST/bool_${a%=1}";; esac; done`)
	yaz("semanage", `
if [ "$1 $2" = "fcontext -l" ]; then cat "$ST/fcontext" 2>/dev/null; exit 0; fi
if [ "$1 $2" = "port -l" ]; then cat "$ST/port" 2>/dev/null; exit 0; fi
echo "semanage $*" >>"$LOG"
if [ "$1 $2" = "fcontext -a" ]; then echo "${@: -1} all files system_u:object_r:$4:s0" >>"$ST/fcontext"; fi
if [ "$1 $2" = "port -a" ]; then echo "http_port_t tcp 80, 10080" >>"$ST/port"; fi
exit 0`)
	yaz("restorecon", `echo "restorecon $*" >>"$LOG"; [ -f "$ST/restorecon_out" ] && cat "$ST/restorecon_out"; exit 0`)
	yaz("matchpathcon", `cat "$ST/mp" 2>/dev/null || echo "$2 system_u:object_r:user_home_t:s0"`)
	yaz("chcon", `echo "chcon $*" >>"$LOG"; exit 0`)
	yaz("ls", `if [ "$1" = "-Zd" ]; then echo "$(cat "$ST/ls_label" 2>/dev/null || echo system_u:object_r:user_home_t:s0) $2"; exit 0; fi; exec /bin/ls "$@"`)
	return o
}

func (o *selOrtam) bool(t *testing.T, ad, deger string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(o.st, "bool_"+ad), []byte(deger+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (o *selOrtam) dosya(t *testing.T, ad, icerik string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(o.st, ad), []byte(icerik), 0o644); err != nil {
		t.Fatal(err)
	}
}

// kos: kaynak betik + ayar + bölüm fonksiyonları. Döner: (çıktı, log).
func (o *selOrtam) kos(t *testing.T, sel, semanage, dry int, bolumler ...string) (string, string) {
	t.Helper()
	betik := `f=$1; set --; source "$f" || exit 90; SEL=` + itoa(sel) + `; HAVE_SEMANAGE=` + itoa(semanage) + `; DRY=` + itoa(dry) + `; QUIET=0; ` + strings.Join(bolumler, "; ") + `; exit 0`
	cmd := exec.Command("bash", "-c", betik, "bash", betikYolu(t))
	cmd.Env = append(os.Environ(), "GOSP_REPAIR_KUTUPHANE=1", "LC_ALL=C",
		"PATH="+o.bin+":"+os.Getenv("PATH"), "ST="+o.st, "LOG="+o.log,
		"GOSP_REPAIR_EV_KOKU="+o.ev, "GOSP_REPAIR_HTTPD_CONF="+filepath.Join(o.dir, "httpd.conf"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("betik hatası (%v): %s", err, out)
	}
	lg, _ := os.ReadFile(o.log)
	return string(out), string(lg)
}

func itoa(n int) string { return string(rune('0' + n)) }

const tumFcontext = `/opt/phpmyadmin(/.*)? all files system_u:object_r:httpd_sys_content_t:s0
/var/lib/phpmyadmin(/.*)? all files system_u:object_r:httpd_sys_rw_content_t:s0
/opt/girginospanel/frontend-dist(/.*)? all files system_u:object_r:httpd_sys_content_t:s0
/opt/girginospanel/pma-signon(/.*)? all files system_u:object_r:httpd_sys_content_t:s0
/run/php-fpm-[^/]+(/.*)? all files system_u:object_r:httpd_var_run_t:s0
/home/c_[^/]+/(public_html|tmp)(/.*)? all files system_u:object_r:httpd_user_rw_content_t:s0
`

func TestSelinuxTemel_EksikleriTekSeferdeYazar(t *testing.T) {
	o := selKur(t)
	for _, b := range []string{"httpd_can_network_connect", "httpd_can_network_connect_db", "httpd_enable_homedirs", "httpd_read_user_content"} {
		o.bool(t, b, "off")
	}
	if err := os.WriteFile(filepath.Join(o.dir, "httpd.conf"), []byte("Listen 127.0.0.1:10080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, lg := o.kos(t, 1, 1, 0, "sec_selinux_temel")
	// 4 boolean TEK setsebool -P çağrısında (her -P politika derler).
	if n := strings.Count(lg, "setsebool "); n != 1 {
		t.Fatalf("setsebool çağrı sayısı %d (1 beklenir):\n%s", n, lg)
	}
	for _, b := range []string{"httpd_can_network_connect=1", "httpd_can_network_connect_db=1", "httpd_enable_homedirs=1", "httpd_read_user_content=1"} {
		if !strings.Contains(lg, b) {
			t.Errorf("%s yazılmadı:\n%s", b, lg)
		}
	}
	for _, spec := range []string{"/opt/phpmyadmin(/.*)?", "/var/lib/phpmyadmin(/.*)?", "/opt/girginospanel/frontend-dist(/.*)?",
		"/opt/girginospanel/pma-signon(/.*)?", "/run/php-fpm-[^/]+(/.*)?", "/home/c_[^/]+/(public_html|tmp)(/.*)?"} {
		if !strings.Contains(lg, "semanage fcontext -a -t") || !strings.Contains(lg, spec) {
			t.Errorf("fcontext kuralı yazılmadı: %s\n%s", spec, lg)
		}
	}
	if !strings.Contains(lg, "semanage port -a -t http_port_t -p tcp 10080") {
		t.Errorf("10080 portu etiketlenmedi:\n%s", lg)
	}
	if strings.Contains(out, "setenforce") || strings.Contains(lg, "setenforce") {
		t.Fatal("mod değiştirme kodu YOK olmalı")
	}
}

func TestSelinuxTemel_IdempotentHicYazmaz(t *testing.T) {
	o := selKur(t)
	for _, b := range []string{"httpd_can_network_connect", "httpd_can_network_connect_db", "httpd_enable_homedirs", "httpd_read_user_content"} {
		o.bool(t, b, "on")
	}
	o.dosya(t, "fcontext", tumFcontext)
	if err := os.WriteFile(filepath.Join(o.dir, "httpd.conf"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	o.dosya(t, "port", "http_port_t                    tcp      80, 81, 443, 10080, 8443\n")
	_, lg := o.kos(t, 1, 1, 0, "sec_selinux_temel")
	for _, yasak := range []string{"setsebool", "semanage fcontext -a", "semanage fcontext -m", "semanage port"} {
		if strings.Contains(lg, yasak) {
			t.Fatalf("her şey yerindeyken %q çağrıldı (politika boşuna yeniden derlenir):\n%s", yasak, lg)
		}
	}
}

func TestSelinuxTemel_DryRunHicbirSeyDegistirmez(t *testing.T) {
	o := selKur(t)
	o.bool(t, "httpd_enable_homedirs", "off")
	out, lg := o.kos(t, 1, 1, 1, "sec_selinux_temel")
	if strings.Contains(lg, "setsebool") || strings.Contains(lg, "semanage fcontext -a") || strings.Contains(lg, "semanage port -a") {
		t.Fatalf("dry-run değişiklik yaptı:\n%s", lg)
	}
	for _, satir := range strings.Split(lg, "\n") {
		if strings.HasPrefix(satir, "restorecon ") && !strings.HasPrefix(satir, "restorecon -n") {
			t.Fatalf("dry-run'da -n'siz restorecon: %q", satir)
		}
	}
	if !strings.Contains(out, "httpd_enable_homedirs kapalı") {
		t.Fatalf("dry-run neyin düzeltileceğini söylemeli:\n%s", out)
	}
}

func TestSelinuxTemel_SELinuxKapaliysaSessizGecer(t *testing.T) {
	o := selKur(t)
	o.bool(t, "httpd_enable_homedirs", "off")
	_, lg := o.kos(t, 0, 1, 0, "sec_selinux_temel", "sec_docroot_selinux")
	if strings.TrimSpace(lg) != "" {
		t.Fatalf("SELinux kapalıyken komut çalıştı:\n%s", lg)
	}
}

// 🔴 Gerileme (181, 2026-10-06): üst dizin ZATEN doğru etiketliyken alt ağaçtaki yanlış etiketli
// dosyalar (cp -al ile gelen user_home_t database.sqlite) onarılmıyordu (`continue`).
func TestDocrootSelinux_UstDogruAltAgacYineOnarilir(t *testing.T) {
	o := selKur(t)
	ph := filepath.Join(o.ev, "c_x", "public_html")
	if err := os.MkdirAll(ph, 0o755); err != nil {
		t.Fatal(err)
	}
	o.dosya(t, "ls_label", "system_u:object_r:httpd_user_rw_content_t:s0")
	o.dosya(t, "restorecon_out", "Relabeled "+ph+"/database/database.sqlite from system_u:object_r:user_home_t:s0 to system_u:object_r:httpd_user_rw_content_t:s0\n")
	o.dosya(t, "mp", ph+" system_u:object_r:httpd_user_rw_content_t:s0")
	out, lg := o.kos(t, 1, 1, 0, "sec_docroot_selinux")
	if !strings.Contains(lg, "restorecon -RFv "+ph) {
		t.Fatalf("üst dizin doğru olsa da alt ağaç restorecon -RF'ten geçmeli:\n%s", lg)
	}
	if !strings.Contains(out, "alt ağaçta 1 dosya yanlış etiketliydi") {
		t.Fatalf("düzeltilen dosya raporlanmalı:\n%s", out)
	}
	if strings.Contains(lg, "semanage fcontext -a") {
		t.Fatalf("genel kural varken kiracıya özel kural yazılmamalı (12 sn politika derlemesi):\n%s", lg)
	}
}

func TestDocrootSelinux_DryRunAltAgaciDegistirmez(t *testing.T) {
	o := selKur(t)
	ph := filepath.Join(o.ev, "c_x", "public_html")
	if err := os.MkdirAll(ph, 0o755); err != nil {
		t.Fatal(err)
	}
	o.dosya(t, "ls_label", "system_u:object_r:httpd_user_rw_content_t:s0")
	o.dosya(t, "restorecon_out", "Would relabel "+ph+"/a from system_u:object_r:user_home_t:s0 to system_u:object_r:httpd_user_rw_content_t:s0\n")
	_, lg := o.kos(t, 1, 1, 1, "sec_docroot_selinux")
	if !strings.Contains(lg, "restorecon -nRFv "+ph) || strings.Contains(lg, "restorecon -RFv") {
		t.Fatalf("dry-run yalnız -n kullanmalı:\n%s", lg)
	}
}

func TestDocrootSelinux_SemanageYoksaChconYedegi(t *testing.T) {
	o := selKur(t)
	ph := filepath.Join(o.ev, "c_x", "public_html")
	if err := os.MkdirAll(ph, 0o755); err != nil {
		t.Fatal(err)
	}
	// üst dizin yanlış etiketli (ls_label varsayılan user_home_t), politika da RW'ye çözmüyor
	_, lg := o.kos(t, 1, 0, 0, "sec_docroot_selinux")
	if !strings.Contains(lg, "chcon -R -t httpd_user_rw_content_t "+ph) {
		t.Fatalf("semanage yokken chcon yedeği çalışmalı:\n%s", lg)
	}
	if strings.Contains(lg, "semanage") {
		t.Fatalf("semanage yokken semanage çağrılmamalı:\n%s", lg)
	}
}

func TestDocrootSelinux_SymlinkDizineDokunmaz(t *testing.T) {
	o := selKur(t)
	disari := t.TempDir()
	if err := os.MkdirAll(filepath.Join(o.ev, "c_x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(disari, filepath.Join(o.ev, "c_x", "public_html")); err != nil {
		t.Fatal(err)
	}
	_, lg := o.kos(t, 1, 1, 0, "sec_docroot_selinux")
	if strings.Contains(lg, "restorecon") || strings.Contains(lg, "semanage") || strings.Contains(lg, "chcon") {
		t.Fatalf("symlink public_html'e dokunuldu:\n%s", lg)
	}
}
