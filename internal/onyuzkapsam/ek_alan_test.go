package onyuzkapsam

// ek_alan_test.go — "Alan adları" kartı: ekleme düğmelerinin görünürlük kuralı (ekleme.ts saf
// fonksiyonu, esbuild + node ile GERÇEKTEN çalıştırılır) ve BilgiPaneli kaynak kilitleri.
// Kural: paket alan adı limiti 1 (yalnız ana) ise "Alan adı ekle" YOK; veri gelmeden düğme YOK;
// ikisi de kapalıysa düğme yerine bilgi satırı.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type eklemeGirdi struct {
	AlanLimit, AlanKullanim, AltLimit, AltKullanim int
	Yuklendi                                       bool
}

func eklemeKosu(t *testing.T, girdiler []eklemeGirdi, adlar []string) (sonuclar []struct {
	AlanEkle, AltEkle bool
	Bilgi             *string
}, hatalar []string) {
	t.Helper()
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
	ekleme, _ := filepath.Abs(filepath.Join(kok, "components/musteri-ana/ekleme"))
	dir := t.TempDir()
	stub := filepath.Join(dir, "i18n.ts")
	os.WriteFile(stub, []byte("export default { language: 'tr' }\n"), 0o600)
	if girdiler == nil {
		girdiler = []eklemeGirdi{}
	}
	if adlar == nil {
		adlar = []string{}
	}
	g, _ := json.Marshal(girdiler)
	a, _ := json.Marshal(adlar)
	// Go alan adları büyük harfle başlar → JS tarafında küçük harfe çevrilir.
	giris := filepath.Join(dir, "t.ts")
	os.WriteFile(giris, []byte(`import { eklemeEylemleri, alanAdiHatasi } from '`+filepath.ToSlash(ekleme)+`'
const g = `+string(g)+` as any[]
const a = `+string(a)+` as string[]
const k = (o: any) => ({ alanLimit: o.AlanLimit, alanKullanim: o.AlanKullanim, altLimit: o.AltLimit, altKullanim: o.AltKullanim, yuklendi: o.Yuklendi })
console.log(JSON.stringify({ s: g.map((o) => eklemeEylemleri(k(o))), h: a.map((x) => alanAdiHatasi(x)) }))
`), 0o600)
	cikis := filepath.Join(dir, "o.js")
	if b, err := exec.Command(esb, giris, "--bundle", "--platform=node", "--format=cjs", "--alias:@/lib/i18n="+stub, "--outfile="+cikis, "--log-level=error").CombinedOutput(); err != nil {
		t.Fatalf("esbuild: %v\n%s", err, b)
	}
	out, err := exec.Command(node, cikis).Output()
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	var r struct {
		S []struct {
			AlanEkle bool    `json:"alanEkle"`
			AltEkle  bool    `json:"altEkle"`
			Bilgi    *string `json:"bilgi"`
		} `json:"s"`
		H []string `json:"h"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("çıktı: %v\n%s", err, out)
	}
	for _, s := range r.S {
		sonuclar = append(sonuclar, struct {
			AlanEkle, AltEkle bool
			Bilgi             *string
		}{s.AlanEkle, s.AltEkle, s.Bilgi})
	}
	return sonuclar, r.H
}

func TestEkleme_GorunurlukMatrisi(t *testing.T) {
	const yok = "Paketinizde yeni alan adı hakkı yok"
	durumlar := []struct {
		ad        string
		g         eklemeGirdi
		alan, alt bool
		bilgiVar  bool
	}{
		{"limit 1: yalnız ana → alan adı eklenemez", eklemeGirdi{1, 1, 0, 0, true}, false, true, false},
		{"limit 0 (sınırsız)", eklemeGirdi{0, 7, 0, 0, true}, true, true, false},
		{"limit 3 kullanım 2", eklemeGirdi{3, 2, 0, 0, true}, true, true, false},
		{"limit 3 kullanım 3 (dolu)", eklemeGirdi{3, 3, 0, 0, true}, false, true, false},
		{"alt limit dolu, alan adı uygun", eklemeGirdi{0, 1, 2, 2, true}, true, false, false},
		{"alt limit sınırsız (0) kullanım yüksek", eklemeGirdi{0, 1, 0, 99, true}, true, true, false},
		{"ikisi de kapalı → bilgi", eklemeGirdi{1, 1, 2, 2, true}, false, false, true},
		{"yüklenmedi → hiçbiri, bilgi de yok", eklemeGirdi{0, 0, 0, 0, false}, false, false, false},
		{"yüklenmedi, limit dolu görünse de", eklemeGirdi{1, 1, 1, 1, false}, false, false, false},
	}
	var gir []eklemeGirdi
	for _, d := range durumlar {
		gir = append(gir, d.g)
	}
	sonuc, _ := eklemeKosu(t, gir, nil)
	if len(sonuc) != len(durumlar) {
		t.Fatalf("sonuç sayısı %d != %d", len(sonuc), len(durumlar))
	}
	for i, d := range durumlar {
		s := sonuc[i]
		if s.AlanEkle != d.alan || s.AltEkle != d.alt {
			t.Errorf("%s: alanEkle=%v altEkle=%v, beklenen %v/%v", d.ad, s.AlanEkle, s.AltEkle, d.alan, d.alt)
		}
		if d.bilgiVar != (s.Bilgi != nil) || (s.Bilgi != nil && *s.Bilgi != yok) {
			t.Errorf("%s: bilgi=%v, beklenen var=%v (%q)", d.ad, s.Bilgi, d.bilgiVar, yok)
		}
	}
}

func TestEkleme_AlanAdiDogrulama(t *testing.T) {
	gecerli := []string{"ornek.com", "alt.ornek.com.tr", "xn--bcher-kva.example", "a-b.co", "1abc.net"}
	gecersiz := []string{"ornek", "ornek..com", ".ornek.com", "ornek.com.", "http://ornek.com", "ornek.com/yol", "orn ek.com", "-ornek.com", "ornek-.com", "ornek.com:80", "192.168.1.1", "Ornek.com", strings.Repeat("a", 64) + ".com", strings.Repeat("a.", 130) + "com", "ör_nek.com"}
	adlar := append(append([]string{}, gecerli...), gecersiz...)
	_, h := eklemeKosu(t, nil, adlar)
	if len(h) != len(adlar) {
		t.Fatalf("doğrulama sonucu sayısı %d != %d", len(h), len(adlar))
	}
	for i, a := range adlar {
		if i < len(gecerli) && h[i] != "" {
			t.Errorf("%q geçerli olmalıydı: %q", a, h[i])
		}
		if i >= len(gecerli) && h[i] == "" {
			t.Errorf("%q reddedilmeliydi", a)
		}
	}
}

func TestBilgiPaneli_EkleDugmeleriYalnizEklemeEylemleriyle(t *testing.T) {
	src := oku(t, "components/musteri-ana/BilgiPaneli.tsx")
	if !strings.Contains(src, "eklemeEylemleri({") || !strings.Contains(src, "yuklendi: el != null") {
		t.Fatal("görünürlük eklemeEylemleri(...) ve yuklendi bayrağıyla belirlenmeli")
	}
	// Tek ternary zinciri: ikisi → menü; yalnız alan → "Alan adı ekle"; yalnız alt → "Alt alan adı ekle"; yoksa null.
	zincir := regexp.MustCompile(`alanEkle && altEkle \? \(\s*<EkleMenusu[\s\S]*?\) : alanEkle \? \([\s\S]*?cevir\('Alan adı ekle'\)[\s\S]*?\) : altEkle \? \([\s\S]*?cevir\('Alt alan adı ekle'\)[\s\S]*?\) : null`)
	if !zincir.MatchString(src) {
		t.Error("düğme ternary zinciri (menü | alan | alt | null) bulunamadı")
	}
	// "Alan adı ekle" yalnız iki yerde: menü içinde ve alanEkle dalında; <EkleMenusu yalnız zincirde.
	if n := strings.Count(src, "cevir('Alan adı ekle')"); n != 2 {
		t.Errorf("'Alan adı ekle' %d kez geçiyor, 2 olmalı (menü + tek düğme)", n)
	}
	if n := strings.Count(src, "<EkleMenusu"); n != 1 {
		t.Errorf("<EkleMenusu %d kez kullanılmış, 1 olmalı", n)
	}
	if strings.Contains(src, "cevir('Ekle')}\n        </BaglantiDugme>") {
		t.Error("eski koşulsuz '+ Ekle' bağlantısı geri gelmiş")
	}
	// Menü erişilebilirliği.
	for _, gerek := range []string{`aria-haspopup="menu"`, "aria-expanded={acik}", `role="menu"`, `role="menuitem"`, "useDisariKapat(acik, kokRef", "/ek-alanlar/yeni", "/subdomainler`"} {
		if !strings.Contains(src, gerek) {
			t.Errorf("EkleMenusu beklenen öğeyi içermiyor: %q", gerek)
		}
	}
	// Bilgi satırı yalnız ikisi de kapalıyken gelen metinle çizilir.
	if !strings.Contains(src, "{bilgi && (") {
		t.Error("bilgi satırı bilgi değerine bağlı değil")
	}
}

func TestEkAlan_RotalarVeKirinti(t *testing.T) {
	app := oku(t, "App.tsx")
	if !strings.Contains(app, `path="abonelikler/:id/ek-alanlar/yeni" element={<EkAlanEklePage />}`) {
		t.Error("App.tsx rota eksik: ek-alanlar/yeni")
	}
	// Ek alan detay sayfası KALDIRILDI: alan seçici ortak sayfalarda (SSL/WordPress/Günlükler); silme hesap
	// sayfasındaki alan listesinden. Rota, sayfa ve ona giden bağlantılar geri gelmemeli.
	if strings.Contains(app, "EkAlanYonetPage") || strings.Contains(app, "ek-alanlar/:eid") {
		t.Error("ek alan detay rotası/sayfası geri gelmiş")
	}
	if _, err := os.Stat(filepath.Join(kok, "pages/EkAlanYonetPage.tsx")); err == nil {
		t.Error("pages/EkAlanYonetPage.tsx geri gelmiş")
	}
	sil := oku(t, "components/musteri-ana/EkAlanSilModal.tsx")
	if !strings.Contains(sil, "?onay=${encodeURIComponent(alanAdi)}") {
		t.Error("silme çağrısı ?onay=<alan adı> göndermeli (sunucu zorunlu tutuyor)")
	}
	if !strings.Contains(oku(t, "components/musteri-ana/BilgiPaneli.tsx"), "<EkAlanSilModal") {
		t.Error("alan listesinde ek alan silme yok")
	}
}
