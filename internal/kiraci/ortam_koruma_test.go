package kiraci

// Kaynak koruma (ortam): Komut kiracı çocuğunun ortamını asgari kurar; bu testler
// o ortamın ÇAĞRI YERİNDE yeniden genişletilmesini yakalar.
//
//  1. os.Environ / syscall.Environ (panelin TÜM ortamı) internal/kiraci dışında
//     yalnız gerekçeli, sayısı sabit istisnalarda (kök çocukları) geçebilir.
//     Kiracı çağrısına "cmd.Env = append(cmd.Env, os.Environ()...)" eklemek
//     sayıyı değiştirir → yakalanır.
//  2. Komut'tan (ya da Komut'u döndüren bir sarmalayıcıdan) gelen *exec.Cmd'nin
//     Env alanına YALNIZ "x.Env = append(x.Env, ...)" yazılabilir. "x.Env = nil"
//     (nil = panel ortamını MİRAS al), "x.Env = os.Environ()", "x.Env =
//     []string{...}" ya da "append(os.Environ(), ...)" reddedilir.
//
// 1. katman (config.Load sırları ortamdan siler) bir gün gerilerse kiracı kodu
// (wp-cli, artisan, composer, cron komutu) sırrı yine görmesin diye: derinlemesine
// savunma. Davranış kilidi: paketlerin sir_cocuk_test.go dosyaları.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// ortamIstisnalari — os.Environ/syscall.Environ kullanabilen dosyalar (dosya → sayı).
// Hepsi KÖK çocuğu ya da sırları silen koddur; kiracı kimliğinde çalışan yok.
// Yeni istisna EKLENMEZ: çocuk ortamı açıkça (asgari) kurulur.
var ortamIstisnalari = map[string]int{
	// SirlariOrtamdanSil: ortamı tarayıp PANEL_* sırlarını SİLER (1. katman).
	"internal/config/config.go": 1,
	// sshpass (kök): SSHPASS eklenir; 1. katmandan sonra ortamda sır yok.
	"internal/backups/destination.go": 1,
	"internal/tasima/aktar.go":        2,
	"internal/tasima/tasima.go":       4,
	// valkey-cli (kök): REDISCLI_AUTH eklenir.
	"internal/redis/redis.go": 1,
	// Windows geliştirme platformu (mysql istemcisi), Linux'ta derlenmez.
	"internal/platform/veritabani_windows.go": 1,
}

// goDosyalari: cmd/ ve internal/ altındaki üretim .go dosyaları (test ve
// internal/kiraci hariç), depo köküne göreli yol → ayrıştırılmış dosya.
func goDosyalari(t *testing.T, fset *token.FileSet) map[string]*ast.File {
	t.Helper()
	kok, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*ast.File{}
	for _, alt := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(kok, alt), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(kok, p)
			rel = filepath.ToSlash(rel)
			if d.IsDir() {
				if d.Name() == "node_modules" || d.Name() == "testdata" || rel == "internal/kiraci" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			f, perr := parser.ParseFile(fset, p, nil, 0)
			if perr != nil {
				t.Errorf("%s: %v", rel, perr)
				return nil
			}
			out[rel] = f
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(out) < 100 {
		t.Fatalf("yalnız %d dosya tarandı — depo kökü yanlış mı?", len(out))
	}
	return out
}

// importAdlari: dosyada yol için kullanılan yerel adlar (takma ad dahil).
func importAdlari(f *ast.File, yol string) map[string]bool {
	out := map[string]bool{}
	for _, im := range f.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		if p != yol {
			continue
		}
		ad := path2base(p)
		if im.Name != nil {
			ad = im.Name.Name
		}
		out[ad] = true
	}
	return out
}

func path2base(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// environBul: dosyadaki os.Environ / syscall.Environ başvuruları (çağrı ya da değer).
func environBul(fset *token.FileSet, f *ast.File) []string {
	adlar := importAdlari(f, "os")
	for a := range importAdlari(f, "syscall") {
		adlar[a] = true
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Environ" {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && adlar[id.Name] {
			out = append(out, fset.Position(sel.Pos()).String()+" "+id.Name+".Environ")
		}
		return true
	})
	return out
}

func TestKaynakKoruma_PanelOrtamiYalnizIstisnalarda(t *testing.T) {
	fset := token.NewFileSet()
	dosyalar := goDosyalari(t, fset)
	bulunan := map[string][]string{}
	for rel, f := range dosyalar {
		if b := environBul(fset, f); len(b) > 0 {
			bulunan[rel] = b
		}
	}
	adlar := make([]string, 0, len(bulunan))
	for d := range bulunan {
		adlar = append(adlar, d)
	}
	sort.Strings(adlar)
	for _, d := range adlar {
		if izinli, ok := ortamIstisnalari[d]; ok && len(bulunan[d]) == izinli {
			continue
		}
		t.Errorf("%s: panel ortamının tamamı (os/syscall.Environ) çocuğa verilebilir — çocuk ortamını açıkça kur "+
			"(kiracı: kiraci.Komut; ek değişken: append(cmd.Env, \"AD=değer\")):\n  %s",
			d, strings.Join(bulunan[d], "\n  "))
	}
	for d, izinli := range ortamIstisnalari {
		if _, ok := dosyalar[d]; !ok {
			t.Errorf("istisna %s: dosya yok — istisnayı kaldır", d)
			continue
		}
		if len(bulunan[d]) != izinli {
			t.Errorf("istisna %s: %d başvuru bekleniyordu, %d bulundu — istisnayı güncelle/kaldır", d, izinli, len(bulunan[d]))
		}
	}
}

// komutUretenler: Komut'u çağırıp *exec.Cmd döndüren sarmalayıcıların adları
// (sabit nokta: sarmalayıcıyı döndüren sarmalayıcı da). Ad düzeyinde (paketler
// arası çakışma kuralı yalnız sıkılaştırır).
func komutUretenler(dosyalar map[string]*ast.File) map[string]bool {
	uretici := map[string]bool{}
	for {
		degisti := false
		for _, f := range dosyalar {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Body == nil || uretici[fd.Name.Name] || !cmdDondurur(fd) {
					continue
				}
				bulundu := false
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					if c, ok := n.(*ast.CallExpr); ok && komutCagrisi(c, uretici) {
						bulundu = true
					}
					return !bulundu
				})
				if bulundu {
					uretici[fd.Name.Name] = true
					degisti = true
				}
			}
		}
		if !degisti {
			return uretici
		}
	}
}

// cmdDondurur: fonksiyonun sonuçlarından biri *exec.Cmd mi?
func cmdDondurur(fd *ast.FuncDecl) bool {
	if fd.Type.Results == nil {
		return false
	}
	for _, r := range fd.Type.Results.List {
		if st, ok := r.Type.(*ast.StarExpr); ok {
			if sel, ok := st.X.(*ast.SelectorExpr); ok && sel.Sel.Name == "Cmd" {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "exec" {
					return true
				}
			}
		}
	}
	return false
}

// komutCagrisi: c, Komut yöntemine ya da bilinen bir sarmalayıcıya çağrı mı?
func komutCagrisi(c *ast.CallExpr, uretici map[string]bool) bool {
	switch fn := c.Fun.(type) {
	case *ast.SelectorExpr:
		return fn.Sel.Name == "Komut" || uretici[fn.Sel.Name]
	case *ast.Ident:
		return uretici[fn.Name]
	}
	return false
}

// envSecici: e "<ad>.Env" ise ad'ı döner.
func envSecici(e ast.Expr) (string, bool) {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Env" {
		return "", false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	return id.Name, true
}

// yalnizEkleme: rhs "append(<ad>.Env, ...)" mi?
func yalnizEkleme(rhs ast.Expr, ad string) bool {
	c, ok := rhs.(*ast.CallExpr)
	if !ok || len(c.Args) == 0 {
		return false
	}
	if id, ok := c.Fun.(*ast.Ident); !ok || id.Name != "append" {
		return false
	}
	ilk, ok := envSecici(c.Args[0])
	return ok && ilk == ad
}

// kiraciEnvIhlalleri: dosyada Komut kaynaklı cmd'nin Env'ini append dışında
// yazan atamalar. Değişkenler fonksiyon (ve iç fonksiyon) gövdesi başına ad ile izlenir.
func kiraciEnvIhlalleri(fset *token.FileSet, f *ast.File, uretici map[string]bool) []string {
	var out []string
	govdeTara := func(govde *ast.BlockStmt) {
		izlenen := map[string]bool{}
		ast.Inspect(govde, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok || len(as.Rhs) != 1 {
				return true
			}
			if c, ok := as.Rhs[0].(*ast.CallExpr); ok && komutCagrisi(c, uretici) {
				if id, ok := as.Lhs[0].(*ast.Ident); ok && id.Name != "_" {
					izlenen[id.Name] = true
				}
			}
			return true
		})
		if len(izlenen) == 0 {
			return
		}
		ast.Inspect(govde, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for i, lhs := range as.Lhs {
				ad, ok := envSecici(lhs)
				if !ok || !izlenen[ad] {
					continue
				}
				if as.Tok == token.ASSIGN && len(as.Rhs) == len(as.Lhs) && yalnizEkleme(as.Rhs[i], ad) {
					continue
				}
				out = append(out, fset.Position(as.Pos()).String()+" "+ad+".Env")
			}
			return true
		})
	}
	// Üst düzey gövdeler iç fonksiyonlarıyla BİRLİKTE taranır (kapanış dıştaki
	// cmd'yi yazabilir); iç fonksiyon ayrıca taranmaz (çift sayım olmasın).
	for _, d := range f.Decls {
		switch x := d.(type) {
		case *ast.FuncDecl:
			if x.Body != nil {
				govdeTara(x.Body)
			}
		case *ast.GenDecl:
			ast.Inspect(x, func(n ast.Node) bool {
				if lit, ok := n.(*ast.FuncLit); ok {
					govdeTara(lit.Body)
					return false
				}
				return true
			})
		}
	}
	return out
}

func TestKaynakKoruma_KiraciOrtamiYalnizEklenir(t *testing.T) {
	fset := token.NewFileSet()
	dosyalar := goDosyalari(t, fset)
	uretici := komutUretenler(dosyalar)
	// Bilinen sarmalayıcılar bulunmalı: tespit bozulursa test boşa geçmesin.
	for _, ad := range []string{"kiraciKomut", "runuserKomut"} {
		if !uretici[ad] {
			t.Errorf("sarmalayıcı %s tespit edilemedi (bulunan: %v) — tespit bozuk", ad, uretici)
		}
	}
	izlenenDosya := 0
	adlar := make([]string, 0, len(dosyalar))
	for d := range dosyalar {
		adlar = append(adlar, d)
	}
	sort.Strings(adlar)
	for _, d := range adlar {
		f := dosyalar[d]
		if len(importAdlari(f, "girginospanel/internal/kiraci")) > 0 {
			izlenenDosya++
		}
		if b := kiraciEnvIhlalleri(fset, f, uretici); len(b) > 0 {
			t.Errorf("%s: kiraci.Komut ortamı yalnız append(cmd.Env, ...) ile genişletilebilir "+
				"(nil = panel ortamını miras alır; os.Environ = tüm sırlar):\n  %s", d, strings.Join(b, "\n  "))
		}
	}
	if izlenenDosya < 10 {
		t.Fatalf("yalnız %d dosya kiraci paketini içe aktarıyor — tarama bozuk mu?", izlenenDosya)
	}
}

// Kural kendini doğrular: bilinen ihlal biçimleri yakalanmalı, izinli biçim geçmeli.
func TestKaynakKoruma_KiraciOrtamiKuraliOrnekler(t *testing.T) {
	kaynak := func(govde string) string {
		return `package p
import ("context"; "os"; "os/exec"; "girginospanel/internal/kiraci")
func sar(sk string) (*exec.Cmd, error) { return kiraci.Kiraci{Kul: sk}.Komut(context.Background(), "x") }
func f(sk string) {
` + govde + `
}`
	}
	durumlar := []struct {
		ad    string
		govde string
		ihlal int
	}{
		{"append-izinli", `cmd, _ := kiraci.Kiraci{Kul: sk}.Komut(context.Background(), "x"); cmd.Env = append(cmd.Env, "A=b")`, 0},
		{"nil", `cmd, _ := kiraci.Kiraci{Kul: sk}.Komut(context.Background(), "x"); cmd.Env = nil`, 1},
		{"os.Environ", `cmd, _ := kiraci.Kiraci{Kul: sk}.Komut(context.Background(), "x"); cmd.Env = os.Environ()`, 1},
		{"append-os.Environ", `cmd, _ := kiraci.Kiraci{Kul: sk}.Komut(context.Background(), "x"); cmd.Env = append(os.Environ(), "A=b")`, 1},
		{"sarmalayici", `c, _ := sar(sk); c.Env = []string{"A=b"}`, 1},
		{"var-sonra-atama", `var c *exec.Cmd; c, _ = sar(sk); c.Env = nil`, 1},
		{"ic-fonksiyon", `func() { c, _ := sar(sk); c.Env = nil }()`, 1},
		{"kapanis-distaki-cmd", `c, _ := sar(sk); func() { c.Env = os.Environ() }()`, 1},
		{"paket-duzeyi", `}
var h = func(sk string) { c, _ := sar(sk); c.Env = nil }
func z() {`, 1},
		{"izlenmeyen", `c := exec.Command("x"); c.Env = nil`, 0},
	}
	for _, d := range durumlar {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, d.ad+".go", kaynak(d.govde), 0)
		if err != nil {
			t.Fatalf("%s: %v", d.ad, err)
		}
		uretici := komutUretenler(map[string]*ast.File{d.ad: f})
		if !uretici["sar"] {
			t.Fatalf("%s: sarmalayıcı sar tespit edilmedi", d.ad)
		}
		if n := len(kiraciEnvIhlalleri(fset, f, uretici)); n != d.ihlal {
			t.Errorf("%s: %d ihlal bekleniyordu, %d", d.ad, d.ihlal, n)
		}
	}
	// os.Environ tespiti takma adla da çalışmalı.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "t.go", `package p
import osx "os"
var _ = osx.Environ
func g() []string { return osx.Environ() }`, 0)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(environBul(fset, f)); n != 2 {
		t.Errorf("takma adlı os.Environ: 2 başvuru bekleniyordu, %d", n)
	}
}
