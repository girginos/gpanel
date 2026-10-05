package php

// php_musteri_kisit_test.go — hosting MÜŞTERİSİ için disable_functions gizliliği ve
// ek_direktifler sertleştirmesi (kok_sinir.go + php.go EkDirektifKapisi):
//   - müşteri GET'inde disable_functions yok; PUT'ta anahtar varsa (değer ne olursa)
//     403; bayi/yönetici eskisi gibi;
//   - ek_direktifler müşteri girdisinde yasak anahtar/biçim tablosu 400 (açık hata);
//     yönetici temel kuralların ötesinde serbest;
//   - havuza yazarken geçersiz kayıtlı satırlar süzülür (geriye dönük);
//   - ön yüz: müşteride kart ve kutu çizilmez (kaynak-okuma kalıbı).

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
)

func mk_ekGovde(ek string) string {
	b, _ := json.Marshal(map[string]any{"ayarlar": map[string]any{"ek_direktifler": ek}})
	return string(b)
}

func TestMusteriKisit_RolMatrisi_DisableFunctions(t *testing.T) {
	tok := ks_tokenlar(t)
	// GET
	for rol, tk := range tok {
		kod, govde, _, _ := ks_cagir(t, http.MethodGet, tk, "")
		if kod != http.StatusOK {
			t.Fatalf("%s GET: %d", rol, kod)
		}
		var y struct {
			Ayarlar map[string]any `json:"ayarlar"`
		}
		_ = json.Unmarshal([]byte(govde), &y)
		_, var_ := y.Ayarlar["disable_functions"]
		if var_ == (rol == "müşteri") {
			t.Errorf("%s: disable_functions görünürlüğü yanlış: %v", rol, var_)
		}
		if strings.Contains(govde, "exec,system") && rol == "müşteri" {
			t.Errorf("müşteri yanıtında df değeri sızdı: %s", govde)
		}
	}
	// PUT: müşteri her biçimde 403 + yazımsız
	for _, g := range []string{
		`{"ayarlar":{"disable_functions":"exec,system"}}`,
		`{"ayarlar":{"disable_functions":""}}`,
		`{"ayarlar":{"disable_functions":null}}`,
		`{"ayarlar":{"DISABLE_FUNCTIONS":"x"}}`,
		`{"ayarlar":{"disable_functions":"exec,system","memory_limit":"512M"}}`,
	} {
		kod, govde, sd, havuz := ks_cagir(t, http.MethodPut, tok["müşteri"], g)
		if kod != http.StatusForbidden || sd.Yurutuldu("INSERT INTO php_settings") || havuz != nil {
			t.Errorf("müşteri %s: 403 + yazımsız beklenirdi: %d %s", g, kod, govde)
		}
		if !strings.Contains(govde, "disable_functions") {
			t.Errorf("müşteri %s: açık hata mesajı beklenirdi: %s", g, govde)
		}
	}
	// bayi: aynı değer eskisi gibi 200; yönetici değiştirir.
	if kod, govde, _, _ := ks_cagir(t, http.MethodPut, tok["bayi"], `{"ayarlar":{"disable_functions":"exec,system"}}`); kod != http.StatusOK {
		t.Errorf("bayi aynı değer eskisi gibi 200: %d %s", kod, govde)
	}
	if kod, govde, sd, havuz := ks_cagir(t, http.MethodPut, tok["admin"], `{"ayarlar":{"disable_functions":"exec"}}`); kod != http.StatusOK || havuz == nil || !sd.Yurutuldu("INSERT INTO php_settings") || !strings.HasPrefix(havuz.DisableFunctions, "exec") {
		t.Errorf("yönetici değiştirebilmeli: %d %s", kod, govde)
	}
}

func TestMusteriKisit_EkDirektif_YasakTablosu(t *testing.T) {
	tok := ks_tokenlar(t)
	// Her rol için reddedilen (temel allowlist) satırlar
	hepsiyleRed := []string{
		"php_value[disable_functions]=",
		"php_value[Disable_Functions] = ",
		"  php_value[open_basedir]=/",
		"php_value[auto_prepend_file]=/tmp/x.php",
		"php_value[extension]=evil.so",
		"php_value[zend_extension]=evil.so",
		"php_admin_value[memory_limit]=9G",
		"php_admin_flag[x]=on",
		"php_value[ open_basedir]=/",
		"php_value[open_basedir ]=/",
		"extension=imagick.so",
		"zend_extension=x.so",
		"[www]",
		"user = root",
		"include=/etc/passwd",
		"php_value[error_log]=/x",
		"php_value[sys_temp_dir]=/",
		"php_value[upload_tmp_dir]=/",
		"php_value[session.save_path]=/",
		";yorum\nphp_admin_value[disable_functions]=",
		"php_value[a]=1\r\nphp_admin_value[disable_functions]=",
		"php_value[a]=1\rphp_value[open_basedir]=/",
		"php_value[a]=1\x00",
	}
	for _, rol := range []string{"müşteri", "bayi", "admin"} {
		for _, ek := range hepsiyleRed {
			kod, govde, sd, havuz := ks_cagir(t, http.MethodPut, tok[rol], mk_ekGovde(ek))
			if kod != http.StatusBadRequest || sd.Yurutuldu("INSERT INTO php_settings") || havuz != nil {
				t.Errorf("%s ek %q: 400 + yazımsız beklenirdi: %d %s", rol, ek, kod, govde)
			}
		}
	}
	// Yalnız müşteriye yasak EK anahtarlar: müşteri 400, bayi/yönetici (mevcut serbestlik) 200.
	musteriyeRed := []string{
		"php_value[user_ini.filename]=.x",
		"php_value[USER_INI.cache_ttl]=1",
		"php_value[opcache.preload]=/x.php",
		"php_value[OpCache.file_cache]=/tmp",
		"php_value[xdebug.output_dir]=/tmp",
		"php_value[phar.readonly]=0",
		"php_value[sendmail_path]=/bin/sh",
		"php_value[mail.log]=/tmp/x",
		"php_value[doc_root]=/",
		"php_value[user_dir]=/",
		"php_value[extension_dir]=/tmp",
		"php_value[enable_dl]=1",
		"php_flag[allow_url_include]=on",
		"php_value[unserialize_callback_func]=system",
		"php_value[session.save_handler]=files",
		"php_value[syslog.ident]=x",
		"php_value[ffi.preload]=/x",
		"php_value[cgi.fix_pathinfo]=0",
		"; yorum\nphp_value[opcache.preload]=/x",
		"php_value[a]=1\r\nphp_value[opcache.preload]=/x",
	}
	for _, ek := range musteriyeRed {
		kod, govde, sd, _ := ks_cagir(t, http.MethodPut, tok["müşteri"], mk_ekGovde(ek))
		if kod != http.StatusBadRequest || sd.Yurutuldu("INSERT INTO php_settings") || !strings.Contains(govde, "ek direktif") {
			t.Errorf("müşteri ek %q: 400 + açık mesaj + yazımsız beklenirdi: %d %s", ek, kod, govde)
		}
		for _, rol := range []string{"bayi", "admin"} {
			if kod, govde, _, _ := ks_cagir(t, http.MethodPut, tok[rol], mk_ekGovde(ek)); kod != http.StatusOK {
				t.Errorf("%s ek %q: mevcut serbestlik korunmalı (200): %d %s", rol, ek, kod, govde)
			}
		}
	}
	// Müşteri için meşru satırlar geçer.
	for _, ek := range []string{
		"", ";yorum", "php_value[date.timezone] = Europe/Istanbul",
		"php_flag[display_errors]=on\n; not\nphp_value[max_input_vars]=20000",
	} {
		if kod, govde, _, _ := ks_cagir(t, http.MethodPut, tok["müşteri"], mk_ekGovde(ek)); kod != http.StatusOK {
			t.Errorf("müşteri meşru ek %q: 200 beklenirdi: %d %s", ek, kod, govde)
		}
	}
}

func TestMusteriKisit_HavuzRender_GeriyeDonukSuzgec(t *testing.T) {
	kayitli := "php_value[date.timezone]=UTC\n" +
		"php_value[open_basedir]=/\r\n" +
		"php_admin_value[disable_functions]=\n" +
		"; yorum\n" +
		"extension=evil.so\n" +
		"php_value[auto_prepend_file]=/home/x/a.php\n" +
		"php_flag[display_errors]=on"
	s := Defaults()
	s.EkDirektifler = kayitli
	for ad, fn := range map[string]func() (string, error){
		"RenderPool":    func() (string, error) { return RenderPool("c_kok", "/run", s) },
		"renderSubPool": func() (string, error) { return renderSubPool("c_kok", "c_kok_s5", "/run", s) },
	} {
		out, err := fn()
		if err != nil {
			t.Fatal(err)
		}
		blok := out[strings.Index(out, "BEGIN_CUSTOM"):]
		for _, yasak := range []string{"open_basedir]=/", "php_admin_value[disable_functions]=", "extension=evil", "auto_prepend_file"} {
			if strings.Contains(blok, yasak) {
				t.Errorf("%s: kayıtlı yasak satır havuza yazıldı: %q\n%s", ad, yasak, blok)
			}
		}
		for _, iyi := range []string{"php_value[date.timezone]=UTC", "php_flag[display_errors]=on", "; yorum"} {
			if !strings.Contains(blok, iyi) {
				t.Errorf("%s: meşru satır korunmalı: %q\n%s", ad, iyi, blok)
			}
		}
		if !strings.Contains(blok, "yok sayildi") {
			t.Errorf("%s: yok sayılan satır için açıklayıcı yorum beklenirdi", ad)
		}
	}
	if s.EkDirektifler != kayitli {
		t.Error("render çağıranın ayarını değiştirmemeli (DB kaydı korunur)")
	}
}

// Ön yüz: müşteri rolünde kart + ham kutu çizilmez. Go'da React render'ı yok; kaynak
// kalıbıyla sabitlenir (kapı ya silinirse/gevşerse kırılır).
func TestMusteriKisit_OnYuz_KartVeKutuMusterideCizilmez(t *testing.T) {
	oku := func(yol string) string {
		b, err := os.ReadFile(yol)
		if err != nil {
			t.Skipf("ön yüz kaynağı yok: %v", err)
		}
		return string(b)
	}
	d := oku("../../frontend/src/pages/DomainPHPPage.tsx")
	for _, kal := range []string{
		"const musteri = musteriMi(rol)",
		`{!musteri && <Kart baslik={cevir("Tehlikeli Fonksiyonları Devre Dışı Bırak")}>`,
		"{!musteri && <div className=\"mt-4\">\n              <Etiket>disable_functions",
		"          </Kart>}\n\n          {/* Additional */}",
	} {
		if !strings.Contains(d, kal) {
			t.Errorf("DomainPHPPage kapısı eksik: %q", kal)
		}
	}
	sb := oku("../../frontend/src/components/SubPHPAyarlari.tsx")
	for _, kal := range []string{
		"musteriMi(rol)",
		"{!musteri && <div>\n            <label className={etk}>disable_functions</label>",
	} {
		if !strings.Contains(sb, kal) {
			t.Errorf("SubPHPAyarlari kapısı eksik: %q", kal)
		}
	}
}
