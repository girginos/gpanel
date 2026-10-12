package php

// mail_yasak_test.go — paylaşılan-master (poolTmpl) ve alt alan (subPoolTmpl) havuzlarında düz mail() KAPALI.
//
// Neyi korur: (1) iki şablon da yasak satırını TAM BİR KEZ, php_admin_value olarak (ini_set / .user.ini /
// php_value ile ezilemez) ve kullanıcı ek direktiflerinden ÖNCE yazar; (2) şablonlar, MEVCUT havuz dosyalarına
// satır ekleyen açılış taramasıyla (provisioner.MailYasakHavuzuYamala) UYUMLUDUR: eski biçim + tarama =
// şablonun yeni çıktısı — bir şablon düzenlemesi çapayı (disable_functions satırı) bozarsa roll-out sessizce
// çalışmaz hale gelirdi.

import (
	"strings"
	"testing"

	"girginospanel/internal/provisioner"
)

// mailHavuzlari: iki şablonun çıktısı — ayar varyantlarının her biri için (ek direktif, mail.force_extra_parameters,
// özel open_basedir/session yolu, hepsi bir arada). Koşullu şablon bölümleri yasak satırını yutmamalı.
func mailHavuzlari(t *testing.T) map[string]string {
	t.Helper()
	varyantlar := map[string]func(*Settings){
		"varsayilan": func(*Settings) {},
		"ek": func(s *Settings) {
			s.EkDirektifler = "php_value[date.timezone]=Europe/Istanbul\nphp_flag[display_startup_errors]=on"
		},
		"mailforce": func(s *Settings) { s.MailForceExtraParameters = "-f noreply@example.com" },
		"yollar": func(s *Settings) {
			s.OpenBasedir = "/home/c_mailphp/:/tmp/:/usr/share/php/"
			s.SessionSavePath = "/home/c_mailphp/sess"
		},
		"hepsi": func(s *Settings) {
			s.EkDirektifler = "php_value[date.timezone]=Europe/Istanbul"
			s.MailForceExtraParameters = "-f noreply@example.com"
			s.OpenBasedir = "/home/c_mailphp/:/tmp/"
			s.SessionSavePath = "/home/c_mailphp/sess"
			s.PMStrategy = "dynamic"
			s.DebugMode = true
		},
	}
	out := map[string]string{}
	for ad, uygula := range varyantlar {
		s := Defaults()
		uygula(&s)
		ana, err := RenderPool("c_mailphp", "/run/php-fpm", s)
		if err != nil {
			t.Fatal(err)
		}
		alt, err := renderSubPool("c_mailphp", "c_mailphp_s3", "/run/php-fpm", s)
		if err != nil {
			t.Fatal(err)
		}
		out["RenderPool/"+ad], out["renderSubPool/"+ad] = ana, alt
	}
	return out
}

func TestSablonlar_MailYasakSatiriBirKezAdminOlarakVeOzelBlokTanOnce(t *testing.T) {
	satir := provisioner.MailYasakPoolSatiri()
	if !strings.HasPrefix(satir, "php_admin_value[sendmail_path] = ") {
		t.Fatalf("satır php_admin_value[sendmail_path] olmalı (php_value ezilirdi): %q", satir)
	}
	for ad, pool := range mailHavuzlari(t) {
		if n := strings.Count(pool, satir+"\n"); n != 1 {
			t.Errorf("%s: yasak satırı %d kez geçiyor, 1 olmalı:\n%s", ad, n, pool)
		}
		if n := strings.Count(pool, "sendmail_path"); n != 1 {
			t.Errorf("%s: sendmail_path %d kez geçiyor, 1 olmalı (php-fpm'de son satır kazanır):\n%s", ad, n, pool)
		}
		d := strings.Index(pool, "php_admin_value[disable_functions]")
		i := strings.Index(pool, satir)
		c := strings.Index(pool, "; ---- BEGIN_CUSTOM ----")
		if !(d >= 0 && d < i && i < c) {
			t.Errorf("%s: sıra disable_functions(%d) < yasak satırı(%d) < BEGIN_CUSTOM(%d) olmalı", ad, d, i, c)
		}
	}
}

func TestSablonlar_MailYasakYamasiylaUyumlu(t *testing.T) {
	satir := provisioner.MailYasakPoolSatiri() + "\n"
	for ad, yeni := range mailHavuzlari(t) {
		eski := strings.Replace(yeni, satir, "", 1) // ESKİ sürümün ürettiği biçim
		if eski == yeni {
			t.Fatalf("%s: şablon çıktısında yasak satırı yok", ad)
		}
		got, uygulandi := provisioner.MailYasakHavuzuYamala(eski)
		if !uygulandi || got != yeni {
			t.Errorf("%s: açılış taraması eski biçimi şablonun yeni çıktısına getirmeli (uygulandi=%v)\n--- got\n%s\n--- want\n%s", ad, uygulandi, got, yeni)
		}
		if _, tekrar := provisioner.MailYasakHavuzuYamala(yeni); tekrar {
			t.Errorf("%s: yeni biçim ikinci kez yamalanmamalı", ad)
		}
	}
}
