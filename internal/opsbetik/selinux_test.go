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
/run/php-fpm-[^/]+(/.*)? all files system_u:object_r:httpd_mevcutYollarrun_t:s0
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
	out, lg := o.kos(t, 1, 1, 0, "sec_selinux_temel")
	if n := strings.Count(out, "✓ boolean "); n != 4 {
		t.Fatalf("4 açık boolean 'on' olarak raporlanmalı (%d):\n%s", n, out)
	}
	if strings.Contains(out, "! boolean httpd_") {
		t.Fatalf("açık httpd booleanı 'okunamadı' sayıldı:\n%s", out)
	}
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

// ---- girginospanel-dogrula SELinux derinleştirme bloğu ----
// Blok betikten işaretçilerle çıkarılıp sahte araçlarla koşulur (tüm dogrula canlı servisleri
// ölçer). Döner: çıktı.
func (o *selOrtam) dogrulaKos(t *testing.T) string {
	t.Helper()
	yol, err := filepath.Abs("../../assets/ops/girginospanel-dogrula")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(yol)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	bas := strings.Index(s, "# ── SELinux deepening")
	son := strings.Index(s, "bas \"Antivirus\"")
	ust := strings.Index(s, "c_g=$'")
	ustSon := strings.Index(s, "bas()  {")
	if bas < 0 || son < 0 || ust < 0 || ustSon < 0 {
		t.Fatal("dogrula işaretçileri bulunamadı")
	}
	// ust: renk + sayaç + gec/dus/uyar tanımları (bas()'a kadar), sonra blok.
	betik := "set -uo pipefail; export LC_ALL=C; KISA=0; JSON=0\n" + s[ust:ustSon] + "\n" + s[bas:son] +
		"\necho \"SONUC kritik=$KRITIK uyari=$UYARI gecen=$GECEN\"\n"
	cmd := exec.Command("bash", "-c", betik)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "PATH="+o.bin+":"+os.Getenv("PATH"), "ST="+o.st, "LOG="+o.log)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("dogrula bloğu hatası (%v): %s", err, out)
	}
	return string(out)
}

func (o *selOrtam) dogrulaAraclari(t *testing.T, mod string) {
	t.Helper()
	yaz := func(ad, govde string) {
		if err := os.WriteFile(filepath.Join(o.bin, ad), []byte("#!/bin/bash\n"+govde), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	yaz("getenforce", "echo "+mod)
	yaz("ausearch", `if [ -f "$ST/ausearch" ]; then cat "$ST/ausearch"; exit 0; fi; echo '<no matches>'; exit 1`)
	// tenant dizini: /home/c_*/ gerçek sistemde olabilir → restorecon sahtesi her dizin için aynı çıktıyı verir
	for _, b := range []string{"httpd_can_network_connect", "httpd_can_network_connect_db", "httpd_enable_homedirs", "httpd_read_user_content"} {
		if _, err := os.Stat(filepath.Join(o.st, "bool_"+b)); err != nil {
			o.bool(t, b, "on")
		}
	}
	if _, err := os.Stat(filepath.Join(o.st, "fcontext")); err != nil {
		o.dosya(t, "fcontext", tumFcontext)
	}
}

// dg: dogrula çıktısında `ad` satırının durumu: "gec" | "dus" | "uyar" | "" (satır yok).
func dg(out, ad string) string {
	for _, l := range strings.Split(out, "\n") {
		if !strings.Contains(l, ad) {
			continue
		}
		switch {
		case strings.Contains(l, "✓"):
			return "gec"
		case strings.Contains(l, "✗"):
			return "dus"
		case strings.Contains(l, "!"):
			return "uyar"
		}
	}
	return ""
}

func TestDogrula_SELinuxSagliklida(t *testing.T) {
	o := selKur(t)
	o.dogrulaAraclari(t, "Enforcing")
	out := o.dogrulaKos(t)
	if !strings.Contains(out, "kritik=0") {
		t.Fatalf("sağlıklı sistemde kritik olmamalı:\n%s", out)
	}
	for _, g := range []string{"SELinux booleans", "SELinux fcontext rules", "SELinux AVC (24h)"} {
		if dg(out, g) != "gec" {
			t.Errorf("%q geçmeli:\n%s", g, out)
		}
	}
}

func TestDogrula_BooleanKapaliEnforcingKritik(t *testing.T) {
	o := selKur(t)
	o.bool(t, "httpd_enable_homedirs", "off")
	o.dogrulaAraclari(t, "Enforcing")
	out := o.dogrulaKos(t)
	if !strings.Contains(out, "kritik=1") || !strings.Contains(out, "httpd_enable_homedirs") {
		t.Fatalf("Enforcing'de homedirs kapalı KRİTİK olmalı:\n%s", out)
	}
	// Permissive → yalnız uyarı
	o2 := selKur(t)
	o2.bool(t, "httpd_enable_homedirs", "off")
	o2.dogrulaAraclari(t, "Permissive")
	out = o2.dogrulaKos(t)
	if !strings.Contains(out, "kritik=0") || !strings.Contains(out, "uyari=1") {
		t.Fatalf("Permissive'de uyarı olmalı:\n%s", out)
	}
}

func TestDogrula_Fcontext_PhpFpmEksikEnforcingKritik(t *testing.T) {
	o := selKur(t)
	o.dosya(t, "fcontext", strings.ReplaceAll(tumFcontext, "/run/php-fpm-[^/]+(/.*)?", "/run/baska"))
	o.dogrulaAraclari(t, "Enforcing")
	out := o.dogrulaKos(t)
	if !strings.Contains(out, "kritik=1") || !strings.Contains(out, "php-fpm") {
		t.Fatalf("php-fpm fcontext eksik + Enforcing KRİTİK olmalı:\n%s", out)
	}
}

func TestDogrula_KiraciDosyaEtiketi(t *testing.T) {
	// restorecon sahtesi her tenant dizini için aynı çıktıyı verir; /home/c_* olan makinede çalışır,
	// yoksa dizin sayısı 0 → bu test atlanır.
	if m, _ := filepath.Glob("/home/c_*/public_html"); len(m) == 0 {
		if m2, _ := filepath.Glob("/home/c_*/tmp"); len(m2) == 0 {
			t.Skip("/home/c_* kiracı dizini yok — blok /home'u tarar")
		}
	}
	tip := "Would relabel /home/c_x/public_html/database/database.sqlite from system_u:object_r:user_home_t:s0 to system_u:object_r:httpd_user_rw_content_t:s0\n"
	kullanici := "Would relabel /home/c_x/public_html/a.js from unconfined_u:object_r:httpd_user_rw_content_t:s0 to system_u:object_r:httpd_user_rw_content_t:s0\n"

	o := selKur(t)
	o.dosya(t, "restorecon_out", tip)
	o.dogrulaAraclari(t, "Enforcing")
	out := o.dogrulaKos(t)
	if dg(out, "tenant file labels") != "dus" || !strings.Contains(out, "database.sqlite") {
		t.Fatalf("tip farkı Enforcing'de KRİTİK olmalı:\n%s", out)
	}

	o2 := selKur(t)
	o2.dosya(t, "restorecon_out", kullanici)
	o2.dogrulaAraclari(t, "Enforcing")
	out = o2.dogrulaKos(t)
	if dg(out, "tenant file labels") != "gec" {
		t.Fatalf("yalnız SELinux kullanıcı alanı farkı bulgu SAYILMAMALI:\n%s", out)
	}
}

func TestDogrula_AVC(t *testing.T) {
	virt := `type=AVC msg=audit(1.1:1): avc:  denied  { read } for  pid=1 comm="qemu-ga" scontext=system_u:system_r:virt_qemu_ga_t:s0 tcontext=x permissive=0` + "\n"
	php := `type=AVC msg=audit(1.1:2): avc:  denied  { write } for  pid=2 comm="php-fpm" name="database.sqlite" scontext=system_u:system_r:httpd_t:s0 tcontext=unconfined_u:object_r:user_home_t:s0 tclass=file permissive=0` + "\n"

	o := selKur(t)
	o.dosya(t, "ausearch", virt)
	o.dogrulaAraclari(t, "Enforcing")
	out := o.dogrulaKos(t)
	if dg(out, "SELinux AVC") != "gec" {
		t.Fatalf("virt_qemu_ga_t reddi bulgu SAYILMAMALI:\n%s", out)
	}

	o2 := selKur(t)
	o2.dosya(t, "ausearch", virt+php)
	o2.dogrulaAraclari(t, "Enforcing")
	out = o2.dogrulaKos(t)
	if dg(out, "SELinux AVC") != "uyar" || !strings.Contains(out, "1 denial") || !strings.Contains(out, "php-fpm") {
		t.Fatalf("httpd_t reddi uyarı vermeli (virt hariç, 1 adet):\n%s", out)
	}

	// Enforcing'de permissive=1 kayıtları sayılmaz
	o3 := selKur(t)
	o3.dosya(t, "ausearch", strings.ReplaceAll(php, "permissive=0", "permissive=1"))
	o3.dogrulaAraclari(t, "Enforcing")
	if out = o3.dogrulaKos(t); dg(out, "SELinux AVC") != "gec" {
		t.Fatalf("Enforcing'de permissive=1 sayılmamalı:\n%s", out)
	}
}

func TestDogrula_SELinuxKapaliHicOlcmez(t *testing.T) {
	o := selKur(t)
	o.bool(t, "httpd_enable_homedirs", "off")
	o.dogrulaAraclari(t, "Disabled")
	out := o.dogrulaKos(t)
	if !strings.Contains(out, "kritik=0 uyari=0 gecen=0") {
		t.Fatalf("Disabled iken hiçbir ölçüm yapılmamalı:\n%s", out)
	}
}

func TestDogrula_TenantEtiketiPermissiveUyari(t *testing.T) {
	if m, _ := filepath.Glob("/home/c_*/public_html"); len(m) == 0 {
		if m2, _ := filepath.Glob("/home/c_*/tmp"); len(m2) == 0 {
			t.Skip("/home/c_* kiracı dizini yok")
		}
	}
	o := selKur(t)
	o.dosya(t, "restorecon_out", "Would relabel /home/c_x/public_html/a from system_u:object_r:user_home_t:s0 to system_u:object_r:httpd_user_rw_content_t:s0\n")
	o.dogrulaAraclari(t, "Permissive")
	out := o.dogrulaKos(t)
	if dg(out, "tenant file labels") != "uyar" || !strings.Contains(out, "kritik=0") {
		t.Fatalf("Permissive'de yanlış etiket yalnız UYARI olmalı:\n%s", out)
	}
}

// Kullanıcı alanı farkı (unconfined_u ↔ system_u) repair'de de bulgu/düzeltme SAYILMAZ.
func TestDocrootSelinux_YalnizKullaniciAlaniFarkiSayilmaz(t *testing.T) {
	o := selKur(t)
	ph := filepath.Join(o.ev, "c_x", "public_html")
	if err := os.MkdirAll(ph, 0o755); err != nil {
		t.Fatal(err)
	}
	o.dosya(t, "ls_label", "system_u:object_r:httpd_user_rw_content_t:s0")
	o.dosya(t, "restorecon_out", "Relabeled "+ph+"/a.js from unconfined_u:object_r:httpd_user_rw_content_t:s0 to system_u:object_r:httpd_user_rw_content_t:s0\n")
	out, _ := o.kos(t, 1, 1, 0, "sec_docroot_selinux")
	if strings.Contains(out, "alt ağaçta") {
		t.Fatalf("yalnız kullanıcı alanı farkı düzeltme sayıldı:\n%s", out)
	}
}

// Sabit sistem yolları kurulumun eski davranışıyla (-F'siz restorecon -R) etiketlenir;
// -F bu yollarda yüzlerce zararsız kullanıcı-alanı "düzeltmesi" üretir.
func TestSelinuxTemel_SabitYollardaFYok(t *testing.T) {
	var mevcutYollar []string
	for _, p := range []string{"/opt/phpmyadmin", "/var/lib/phpmyadmin", "/opt/girginospanel/frontend-dist", "/opt/girginospanel/pma-signon", "/opt/girginospanel/bin", "/var/named", "/etc/named"} {
		if fi, err := os.Lstat(p); err == nil && fi.IsDir() {
			mevcutYollar = append(mevcutYollar, p)
		}
	}
	if len(mevcutYollar) == 0 {
		t.Skip("sabit yollardan hiçbiri bu makinede yok")
	}
	o := selKur(t)
	_, lg := o.kos(t, 1, 1, 0, "sec_selinux_temel")
	for _, p := range mevcutYollar {
		if !strings.Contains(lg, "restorecon -Rv "+p+"\n") {
			t.Errorf("%s için -F'siz restorecon -Rv bekleniyordu:\n%s", p, lg)
		}
		if strings.Contains(lg, "-RFv "+p+"\n") {
			t.Errorf("%s sabit yolunda -F kullanılmamalı", p)
		}
	}
}
