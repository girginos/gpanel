package provisioner

// selinux_hiz_test.go — hosting oluşturmanın ~26 sn'si iki `semanage fcontext -a`
// politika yeniden derlemesiydi (2026-10-03 181). Politika yolu zaten kiracı-
// yazılabilir tipe çözüyorsa (kurulumun genel kuralı) semanage ÇAĞRILMAZ, yalnız
// restorecon -RF; çözmüyorsa eski davranış (kiracıya özel kural) aynen.

import (
	"reflect"
	"strings"
	"testing"
)

func sh_sahte(t *testing.T, durum, matchpath string, semanageVar bool) *[]string {
	t.Helper()
	var cagri []string
	eK, eA := selinuxKomut, selinuxAracVar
	t.Cleanup(func() { selinuxKomut, selinuxAracVar = eK, eA })
	selinuxAracVar = func(ad string) bool { return ad == "semanage" && semanageVar }
	selinuxKomut = func(ad string, arg ...string) ([]byte, error) {
		cagri = append(cagri, strings.TrimSpace(ad+" "+strings.Join(arg, " ")))
		switch ad {
		case "getenforce":
			return []byte(durum + "\n"), nil
		case "matchpathcon":
			return []byte(matchpath + "\n"), nil
		}
		return nil, nil
	}
	return &cagri
}

func TestSelinuxDocroot_GenelKuralVarsaSemanageYok(t *testing.T) {
	c := sh_sahte(t, "Enforcing", "system_u:object_r:httpd_user_rw_content_t:s0", true)
	selinuxDocrootEtiketle("/home/c_x/public_html")
	want := []string{"getenforce", "matchpathcon -n /home/c_x/public_html", "restorecon -RF /home/c_x/public_html"}
	if !reflect.DeepEqual(*c, want) {
		t.Fatalf("genel kural varken çağrılar:\n got %q\nwant %q", *c, want)
	}
}

func TestSelinuxDocroot_GenelKuralYoksaEskiYol(t *testing.T) {
	c := sh_sahte(t, "Permissive", "system_u:object_r:httpd_user_content_t:s0", true)
	selinuxDocrootEtiketle("/home/c_x/tmp")
	want := []string{"getenforce", "matchpathcon -n /home/c_x/tmp",
		"semanage fcontext -a -t httpd_user_rw_content_t /home/c_x/tmp(/.*)?", "restorecon -RF /home/c_x/tmp"}
	if !reflect.DeepEqual(*c, want) {
		t.Fatalf("genel kural yokken çağrılar:\n got %q\nwant %q", *c, want)
	}
}

func TestSelinuxDocroot_KapaliVeSemanageYok(t *testing.T) {
	c := sh_sahte(t, "Disabled", "", true)
	selinuxDocrootEtiketle("/home/c_x/public_html")
	if !reflect.DeepEqual(*c, []string{"getenforce"}) {
		t.Fatalf("SELinux kapalıyken yalnız getenforce: %q", *c)
	}
	c = sh_sahte(t, "Enforcing", "", false)
	selinuxDocrootEtiketle("/home/c_x/public_html")
	if !reflect.DeepEqual(*c, []string{"getenforce", "restorecon -RF /home/c_x/public_html"}) {
		t.Fatalf("semanage yokken yalnız restorecon: %q", *c)
	}
}
