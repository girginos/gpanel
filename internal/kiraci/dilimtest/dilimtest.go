// Package dilimtest — kaynak dilimli (kiraci.Kiraci.Dilim) çağrı yollarının
// testleri için düzenek: kiracitest.Kur (sahte runuser/sudo/systemd-run +
// sahte panel sırları) ve "c_*" adlarını sistemde kullanıcı olmadan çözen
// kullanıcı dikişi. Dilim adı yalnız panel biçimli (c_...) kiracıda üretilir;
// bu yüzden Dilim yolunu süren testler "nobody" yerine SK kullanır.
//
// Yalnız testler içe aktarır; üretim ikilisine girmez.
package dilimtest

import (
	"errors"
	"os/exec"
	"os/user"
	"strings"
	"testing"

	"girginospanel/internal/kiraci"
	"girginospanel/internal/kiraci/kiracitest"
)

// SK — sahte kiracı kullanıcısı (uid 4242): Dilim yolunu süren testlerin kiracısı.
const SK = "c_ornek_com"

// Kur: kiracitest.Kur(t, adlar...) + "c_*" adlarını uid 4242'ye çözen dikiş
// (kök "c_kok" uid 0'a çözülür: reddedilme testi için). Diğer adlar gerçek aramaya gider.
func Kur(t testing.TB, adlar ...string) kiracitest.Kayit {
	t.Helper()
	k := kiracitest.Kur(t, adlar...)
	eski := user.Lookup
	geri := kiraci.KullaniciDikisi(func(ad string) (*user.User, error) {
		switch {
		case ad == "c_kok":
			return &user.User{Username: ad, Uid: "0", Gid: "0"}, nil
		case strings.HasPrefix(ad, "c_") && ad != "c_yok":
			return &user.User{Username: ad, Uid: "4242", Gid: "4242"}, nil
		}
		return eski(ad)
	})
	t.Cleanup(geri)
	return k
}

// DilimKilidi: ad=systemd-run çağrılarının hepsi --scope + SK'nin diliminde ve
// runuser'ı SK kimliğinde sarıyor; en az bir çağrı var. Çağrıları döner.
func DilimKilidi(t testing.TB, k kiracitest.Kayit, sk string) []kiracitest.Cagri {
	t.Helper()
	cs := k.SizintiYok(t, "systemd-run", "--scope")
	for i, c := range cs {
		var slice, runuser, kul bool
		for j, a := range c.Argv {
			slice = slice || a == "--slice=girginos-"+sk+".slice"
			if a == "runuser" && j+2 < len(c.Argv) && c.Argv[j+1] == "-u" {
				runuser, kul = true, c.Argv[j+2] == sk
			}
		}
		if !slice || !runuser || !kul {
			t.Errorf("systemd-run çağrısı #%d kiracı diliminde/kimliğinde değil: %q", i, c.Argv)
		}
	}
	return cs
}

// Sabit çıktı sözleşmesi: KurSabitCikti sonrası kiracı komutu Cikti'yı
// (stdout+stderr) yazıp CikisKodu ile çıkar.
const (
	CikisKodu = 7
	Cikti     = "tamam-cikti\nuyari-satiri\n"
)

// KurSabitCikti: Kur + sahte runuser (scope içindeki asıl komut) Cikti'yı yazıp
// CikisKodu ile çıkar. Uzun işlerin (composer/wp-cli) scope içinde çıktı/çıkış
// kodunu bozmadığının kilidi için.
func KurSabitCikti(t testing.TB) kiracitest.Kayit {
	t.Helper()
	k := Kur(t)
	k.KomutYaz(t, "runuser", "echo tamam-cikti\necho uyari-satiri >&2\nexit 7")
	return k
}

// SabitCiktiDogrula: birleşik çıktı ve çıkış kodu sözleşmeye uyar.
func SabitCiktiDogrula(t testing.TB, cikti string, err error) {
	t.Helper()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != CikisKodu {
		t.Errorf("çıkış kodu %d korunmalı: %v", CikisKodu, err)
	}
	if cikti != Cikti {
		t.Errorf("çıktı bozuldu: %q (beklenen %q)", cikti, Cikti)
	}
}
