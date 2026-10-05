package onyuzkapsam

// wp_ek_alan_test.go — WordPress Toolkit: ek (addon) / alt alan adı hedefleri. (1) lib/wpHedef.ts
// saf yardımcıları esbuild + node ile GERÇEKTEN çalıştırılır (anahtar, API tabanı, derin
// bağlantı, URL önizleme); (2) DomainWordPressPage kaynak kilitleri: dinamik alan adı seçici,
// "WordPress kurulu" etiketi, SSL uyarısı, URL önizleme, hedef parametresi/base gönderimi,
// tek listede tüm alan adları + süzgeç, boş durum; (3) ek alan yönetim sayfası kısayolu.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWPHedef_SafYardimcilar(t *testing.T) {
	esb, err := filepath.Abs("../../frontend/node_modules/.bin/esbuild")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(esb); err != nil {
		t.Skip("esbuild yok (frontend/node_modules kurulu değil)")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node yok")
	}
	lib, _ := filepath.Abs(filepath.Join(kok, "lib/wpHedef"))
	dir := t.TempDir()
	giris := filepath.Join(dir, "t.ts")
	os.WriteFile(giris, []byte(`import { hedefAnahtar, hedefBase, hedefSslYol, derinBaglanti, urlOnizleme } from '`+filepath.ToSlash(lib)+`'
const ana = { tur: 'ana' as const }, ek = { tur: 'ek' as const, ek_alan_id: 7 }, alt = { tur: 'alt' as const, alt_alan_id: 9 }
console.log(JSON.stringify({
  anahtar: [hedefAnahtar(ana), hedefAnahtar(ek), hedefAnahtar(alt), hedefAnahtar({})],
  base: [hedefBase('5', ana), hedefBase('5', ek), hedefBase('5', alt), hedefBase('5', {})],
  ssl: [hedefSslYol('5', ana), hedefSslYol('5', ek), hedefSslYol('5', alt)],
  derin: [derinBaglanti(undefined, 'ek:12'), derinBaglanti(undefined, 'alt:3'), derinBaglanti(undefined, 'ana'), derinBaglanti('4', 'ek:12'),
          derinBaglanti(undefined, 'ek:'), derinBaglanti(undefined, 'ek:1/../x'), derinBaglanti(undefined, 'javascript:1'), derinBaglanti(undefined, null), derinBaglanti('x', null), derinBaglanti(undefined, 'ek:12 ')],
  url: [urlOnizleme('https://test5.com', 'blog'), urlOnizleme('https://test5.com', ''), urlOnizleme('http://a.b', ' /yeni '), urlOnizleme('https://a.b', '//x')],
}))
`), 0o600)
	cikis := filepath.Join(dir, "o.js")
	if b, err := exec.Command(esb, giris, "--bundle", "--platform=node", "--format=cjs", "--outfile="+cikis, "--log-level=error").CombinedOutput(); err != nil {
		t.Fatalf("esbuild: %v\n%s", err, b)
	}
	out, err := exec.Command(node, cikis).Output()
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	var r map[string][]string
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("çıktı: %v\n%s", err, out)
	}
	bek := map[string][]string{
		"anahtar": {"ana", "ek:7", "alt:9", "ana"},
		"base":    {"/domains/5/wordpress", "/domains/5/ek-alan-adlari/7/wordpress", "/domains/5/subdomain/9/wordpress", "/domains/5/wordpress"},
		"ssl":     {"/abonelikler/5/ssl", "/abonelikler/5/ek-alanlar/7", "/abonelikler/5/subdomainler/9"},
		"derin":   {"ek:12", "alt:3", "ana", "alt:4", "", "", "", "", "", ""},
		"url":     {"https://test5.com/blog", "https://test5.com", "http://a.b/yeni", "https://a.b/x"},
	}
	for k, b := range bek {
		g := r[k]
		if strings.Join(g, "|") != strings.Join(b, "|") {
			t.Errorf("%s: %q, beklenen %q", k, g, b)
		}
	}
}

func TestWPEkAlan_SayfaKaynakKilitleri(t *testing.T) {
	s := oku(t, "pages/DomainWordPressPage.tsx")
	for ad, gerek := range map[string]string{
		"hedefler ucu":                    "`/domains/${id}/wordpress/hedefler`",
		"hesap geneli liste":              "`/domains/${id}/wordpress/hesap`",
		"alan adı seçici (Select)":        `label={cevir("Alan adı")}`,
		"seçenekler hedeflerden":          "p.hedefler.map(x => ({ value: hedefAnahtar(x), label: hedefEtiket(x, true)",
		"tür etiketleri":                  "cevir('ek alan adı')",
		"WordPress kurulu etiketi":        "cevir('WordPress kurulu')",
		"kök kurulum kapalıysa alt dizin": "altGerekli",
		"SSL uyarısı":                     `cevir("Bu alan adında SSL yok; site http olarak kurulur, sonra SSL ekleyebilirsiniz.")`,
		"SSL sayfası bağlantısı":          "hedefSslYol(p.id, h)",
		"URL önizleme":                    "urlOnizleme(h.url_onizleme, p.altDizin)",
		"önizleme etiketi":                `cevir("Site adresi")`,
		"derin bağlantı":                  "derinBaglanti(sid, sp.get('hedef'))",
		"kurulum hedef tabanına gider":    "api.post<Sonuc>(`${hedefBase(id, seciliHedef)}`",
		"uzun istek":                      "}, UZUN_ISTEK)",
		"toolkit hedef tabanı":            "base={hedefBase(id!, k)}",
		"alan adı süzgeci":                `aria-label={cevir("Alan adı süzgeci")}`,
		"süzgeç: tüm alan adları":         `cevir("Tüm alan adları")`,
		"kart başlığında alan adı":        "kurulum.alan_adi || 'WordPress'",
		"tür rozeti":                      `cevir("Ek") : kurulum.tur === 'alt' ? cevir("Alt") : cevir("Ana")`,
		"boş durum (hesap)":               `cevir("Hesabınızda henüz WordPress yok")`,
		"boş durum (alan adı)":            `cevir("Bu alan adında henüz WordPress yok")`,
		"yeni WordPress düğmesi":          `cevir("Yeni WordPress")`,
		"kimlik: anahtar listede":         "key={`${hedefAnahtar(k)}|${k.dizin}`}",
	} {
		if !strings.Contains(s, gerek) {
			t.Errorf("%s: %q yok", ad, gerek)
		}
	}
	// Kurulum yolu tek bir alan adına SABİTLENMEMİŞ olmalı (eski: base = sid ? ... : /domains/${id}/wordpress).
	if strings.Contains(s, "const base = sid ?") {
		t.Error("sayfa hâlâ tek alan adına sabit base kullanıyor")
	}
	// Seçici ortak Select bileşeni (kompakt h-9); elle <select> yok.
	if strings.Contains(s, "<select") {
		t.Error("elle <select> kullanılmamalı; ortak Select bileşeni")
	}
	// Mutlak yol / parola istemci tarafında yok sayılmaz: hedef gövdede GÖNDERİLMEZ (rota taşır).
	if strings.Contains(s, "hedef: ") && strings.Contains(s, "alt_dizin: altDizin.trim()") && strings.Contains(s, "ek_alan_id:") && strings.Contains(s, "docroot") {
		t.Error("istemci docroot/mutlak yol göndermemeli")
	}
	// EN çevirileri
	for _, k := range []string{`"Alan adı":`, `"Tüm alan adları":`, `"WordPress kurulu":`, `"Hesabınızda henüz WordPress yok":`, `"ek alan adı":`, `"alt alan adı":`, `"Site adresi":`} {
		if !strings.Contains(s, k) {
			t.Errorf("EN sözlüğünde %s yok", k)
		}
	}
}

func TestWPEkAlan_EkAlanSayfasindanKisayol(t *testing.T) {
	s := oku(t, "pages/EkAlanYonetPage.tsx")
	if !strings.Contains(s, "`${kok}/wordpress?hedef=ek:${eid}`") || !strings.Contains(s, "cevir('WordPress kur')") {
		t.Error("ek alan yönetim sayfasında hedef önseçili 'WordPress kur' kısayolu yok")
	}
	if !strings.Contains(oku(t, "components/musteri-ana/dil.ts"), "'WordPress kur': 'Install WordPress'") {
		t.Error("kısayol EN çevirisi yok")
	}
}
