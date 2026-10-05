package provisioner

// acme.sh kayıt onarımı — eski kodla alınmış sertifika kayıtlarında Le_Webroot müşteri docroot'unu
// (/home/...) gösteriyordu. nginx ACME challenge'ı YALNIZ /var/www/acme kökünden sunar → yenileme
// "geçersiz yanıt" veriyordu. Burada bu kayıtlar panelin sabit köküne çevrilir (güvenli yön:
// müşteri kontrollü yol → panelin sabit yolu). dns/no/apache/nginx/standalone/stateless ve diğer
// panel kökleri DOKUNULMAZ; yalnız /home/ altı mutlak yollar. Aynı mantık sarmalayıcının `onar`
// alt komutunda da var (acmecron/girginospanel-acme-yenile) ve her yenileme turunun başında koşar.

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

const acmeWebrootKok = "/var/www/acme"

var (
	acmeOnarHomes   = func() []string { return []string{acmeConfigHome, acmePanelHome2} }
	acmeOnarGunluk  = func(s string) { _ = exec.Command("logger", "-t", "girginospanel-acme", "--", s).Run() }
	acmeAlanAdiRe   = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$`)
	acmeWebrootHome = regexp.MustCompile(`^Le_Webroot='(/home/[A-Za-z0-9._/@+-]+)'$`)
)

// AcmeOnarSonuc: bir onarım turunun özeti.
type AcmeOnarSonuc struct {
	Duzeltilen []string // yeniden yazılan .conf yolları
	Atlanan    []string // dokunulmayan ama raporlanan (bozuk/sembolik/çoklu satır/hata)
}

// AcmeKayitOnar: tüm config-home'lardaki kayıtları gezer; hata bir kaydı etkilemez.
func AcmeKayitOnar() AcmeOnarSonuc {
	var s AcmeOnarSonuc
	for _, h := range acmeOnarHomes() {
		acmeHomeOnar(h, &s)
	}
	if len(s.Duzeltilen) > 0 || len(s.Atlanan) > 0 {
		for _, f := range s.Duzeltilen {
			acmeOnarGunluk("Le_Webroot onarıldı → " + acmeWebrootKok + ": " + f)
		}
		for _, a := range s.Atlanan {
			acmeOnarGunluk("UYARI Le_Webroot onarılamadı: " + a)
		}
	}
	return s
}

func acmeHomeOnar(home string, s *AcmeOnarSonuc) {
	if st, err := os.Lstat(home); err != nil || !st.IsDir() {
		return
	}
	dosyalar, _ := filepath.Glob(filepath.Join(home, "*", "*.conf"))
	for _, f := range dosyalar {
		if strings.HasSuffix(f, ".csr.conf") {
			continue
		}
		dir := filepath.Dir(f)
		dad := filepath.Base(dir)
		ad := strings.TrimSuffix(filepath.Base(f), ".conf")
		if dad != ad && dad != ad+"_ecc" {
			continue // kayıt dosyası değil (ör. başka konf)
		}
		if !acmeAlanAdiRe.MatchString(ad) {
			continue
		}
		if st, err := os.Lstat(dir); err != nil || st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
			s.Atlanan = append(s.Atlanan, f+" (dizin sembolik/okunamaz)")
			continue
		}
		st, err := os.Lstat(f)
		if err != nil || st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() {
			s.Atlanan = append(s.Atlanan, f+" (sembolik bağ/düz dosya değil)")
			continue
		}
		ok, nedenAtla, err := acmeKayitDuzelt(f, st)
		switch {
		case err != nil:
			s.Atlanan = append(s.Atlanan, fmt.Sprintf("%s (%v)", f, err))
		case nedenAtla != "":
			s.Atlanan = append(s.Atlanan, f+" ("+nedenAtla+")")
		case ok:
			s.Duzeltilen = append(s.Duzeltilen, f)
		}
	}
}

// acmeKayitDuzelt: (değişti, atlamaNedeni, hata). Atlama nedeni boş + değişmedi = dokunulmadı (normal).
func acmeKayitDuzelt(f string, st os.FileInfo) (bool, string, error) {
	b, err := os.ReadFile(f)
	if err != nil {
		return false, "", err
	}
	satirlar := strings.Split(string(b), "\n")
	idx, adet := -1, 0
	for i, l := range satirlar {
		if strings.HasPrefix(l, "Le_Webroot=") {
			idx = i
			adet++
		}
	}
	if adet == 0 {
		return false, "", nil
	}
	if adet > 1 {
		return false, "birden çok Le_Webroot satırı", nil
	}
	l := strings.TrimRight(satirlar[idx], "\r")
	if l == "Le_Webroot='"+acmeWebrootKok+"'" {
		return false, "", nil
	}
	m := acmeWebrootHome.FindStringSubmatch(l)
	if m == nil {
		// /home/ geçiyor ama biçim bozuk (alıntısız, boşluk, özel karakter) → dokunma + raporla
		if strings.Contains(l, "/home/") {
			return false, "bozuk/alıntısız Le_Webroot değeri", nil
		}
		return false, "", nil // dns_*, no, apache, nginx, standalone, stateless, başka panel kökleri
	}
	// Yedek: bir kez, 0600, var ise ezme.
	yedek := f + ".webroot-yedek"
	if _, err := os.Lstat(yedek); os.IsNotExist(err) {
		if err := acmeYedekYaz(f, yedek, st); err != nil {
			return false, "", fmt.Errorf("yedek: %w", err)
		}
	}
	satirlar[idx] = "Le_Webroot='" + acmeWebrootKok + "'"
	tmp := f + ".onar.tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(satirlar, "\n")), 0o600); err != nil {
		return false, "", err
	}
	defer os.Remove(tmp)
	if err := os.Chmod(tmp, st.Mode().Perm()); err != nil {
		return false, "", err
	}
	if sys, ok := st.Sys().(*syscall.Stat_t); ok {
		_ = os.Chown(tmp, int(sys.Uid), int(sys.Gid))
	}
	if err := os.Rename(tmp, f); err != nil {
		return false, "", err
	}
	return true, "", nil
}

func acmeYedekYaz(src, dst string, st os.FileInfo) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if sys, ok := st.Sys().(*syscall.Stat_t); ok {
		_ = out.Chown(int(sys.Uid), int(sys.Gid))
	}
	return out.Close()
}
