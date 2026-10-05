package subdomain

// php_kok_sinir_test.go — alt alan PHP ayarları (PUT/GET …/subdomain/{sid}/php-settings)
// domain ucuyla AYNI kök sınırına uyar (php.IstegiCoz / php.IstemciAyarlari):
// müşteri/bayi iç alan anahtarı → 403, disable_functions/open_basedir değişimi → 403,
// hiçbir şey yazılmaz; yönetici yazar; GET iç alanı müşteri/bayiye göstermez.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestAltAlanPHPAyar_KokSiniri(t *testing.T) {
	tok := hk_tokenlar(t)
	put := func(rol, govde string) (int, string, bool) {
		hk_dikisler(t, nil, nil)
		u := hk_phpAyarPut
		u.govde = govde
		k, g, sd := hk_cagir(t, u, tok[rol])
		return k, g, sd.Yurutuldu("INSERT INTO subdomain_php_settings")
	}
	for _, rol := range []string{"müşteri", "bayi"} {
		for _, g := range []string{
			`{"ayarlar":{"pm_max_children":500}}`,
			`{"ayarlar":{"pm_max_children":8}}`,
			`{"ayarlar":{"pm_max_spare_servers":null}}`,
			`{"ayarlar":{"disable_functions":""}}`,
			`{"ayarlar":{"open_basedir":"/"}}`,
		} {
			if k, govde, yazdi := put(rol, g); k != http.StatusForbidden || yazdi {
				t.Errorf("%s %s: 403 + yazımsız beklenirdi: %d %s", rol, g, k, govde)
			}
		}
		// Kayıtsız alt alan: Defaults; aynı df + izinli alanlar serbest.
		if rol == "müşteri" {
			// disable_functions müşteriye görünmez: aynı değer bile 403; gövdede yoksa serbest.
			if k, govde, yazdi := put(rol, `{"ayarlar":{"disable_functions":"exec,passthru,shell_exec,system,proc_open,popen"}}`); k != http.StatusForbidden || yazdi {
				t.Errorf("müşteri: aynı disable_functions de 403 olmalı: %d %s", k, govde)
			}
			if k, govde, yazdi := put(rol, `{"ayarlar":{"memory_limit":"512M"}}`); k != http.StatusOK || !yazdi {
				t.Errorf("müşteri: izinli kayıt 200 beklenirdi: %d %s", k, govde)
			}
			for _, ek := range []string{"php_value[open_basedir]=/", "php_value[opcache.preload]=/x.php", "php_value[auto_prepend_file]=/x", "php_admin_value[x]=1", "extension=a.so"} {
				b, _ := json.Marshal(map[string]any{"ayarlar": map[string]any{"ek_direktifler": ek}})
				if k, govde, yazdi := put(rol, string(b)); k != http.StatusBadRequest || yazdi || !strings.Contains(govde, "ek direktif") {
					t.Errorf("müşteri ek %q: 400 + yazımsız + açık mesaj beklenirdi: %d %s", ek, k, govde)
				}
			}
			continue
		}
		if k, govde, yazdi := put(rol, `{"ayarlar":{"disable_functions":"exec,passthru,shell_exec,system,proc_open,popen","memory_limit":"512M"}}`); k != http.StatusOK || !yazdi {
			t.Errorf("%s: izinli kayıt 200 beklenirdi: %d %s", rol, k, govde)
		}
	}
	if k, govde, yazdi := put("admin", `{"ayarlar":{"pm_max_children":20,"disable_functions":""}}`); k != http.StatusOK || !yazdi {
		t.Errorf("yönetici yazabilmeli: %d %s", k, govde)
	}
}

func TestAltAlanPHPAyarGET_IcAlanGizli(t *testing.T) {
	tok := hk_tokenlar(t)
	u := hk_uc{ad: "PHPAyarGet", yontem: http.MethodGet,
		desen: "/domains/{id}/subdomain/{sid}/php-settings", yol: "/domains/42/subdomain/5/php-settings",
		isle: func(h *Handlers) http.HandlerFunc { return h.PHPAyarGet }}
	for rol, t2 := range tok {
		k, govde, _ := hk_cagir(t, u, t2)
		if k != http.StatusOK {
			t.Fatalf("%s: %d %s", rol, k, govde)
		}
		var y struct {
			Ayarlar map[string]any `json:"ayarlar"`
		}
		if err := json.Unmarshal([]byte(govde), &y); err != nil {
			t.Fatal(err)
		}
		if _, df := y.Ayarlar["disable_functions"]; df == (rol == "müşteri") {
			t.Errorf("%s: disable_functions görünürlüğü yanlış (%v)", rol, df)
		}
		_, var_ := y.Ayarlar["pm_max_children"]
		if (rol == "admin") != var_ {
			t.Errorf("%s: pm_max_children görünürlüğü yanlış (%v): %s", rol, var_, strings.TrimSpace(govde))
		}
	}
}
