package kiraci

// Kaynak koruma: panel kodunda (cmd/, internal/) kimlik düşüren süreç YALNIZ
// Komut ile başlatılır. Doğrudan exec.Command("runuser", ...) / sudo / su /
// setpriv / doas ya da SysProcAttr.Credential panel ortamını (sırlar) çocuğa
// geçirmeye açıktır: Env'i unutmak = kiracı PHP'sinin getenv('PANEL_JWT_SECRET')
// ile yönetici JWT'si üretmesi. Bu test bu adları taşıyan HER dizge
// değişmezini (çağrı argümanı, değişken, sabit — yalnız harita anahtarı
// serbest) ve Credential kullanımını internal/kiraci dışında yakalar.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// kimlikDusurenler — kimlik düşüren araçlar (yalın ad ya da mutlak yol).
var kimlikDusurenler = map[string]bool{
	"runuser": true, "sudo": true, "su": true, "setpriv": true, "doas": true,
	"chroot": true, "nsenter": true, "unshare": true,
}

// kaynakIstisnalari — gerekçeli, sayısı sabit istisnalar (dosya → izinli değişmez sayısı).
// Yeni istisna EKLENMEZ: yeni kod Komut kullanır.
var kaynakIstisnalari = map[string]int{
	// (boş) — git.kiraciCalistirGercek kiraci.Komut'a taşındı; istisna kalmadı.
}

func TestKaynakKoruma_KimlikDusurmeYalnizKiraciKomut(t *testing.T) {
	kok, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	bulunan := map[string][]string{}
	taranan := 0
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
			taranan++
			// Harita anahtarı olan değişmezler (ör. paket adı listesi "sudo": true) serbest.
			anahtar := map[*ast.BasicLit]bool{}
			ast.Inspect(f, func(n ast.Node) bool {
				if kv, ok := n.(*ast.KeyValueExpr); ok {
					if bl, ok := kv.Key.(*ast.BasicLit); ok {
						anahtar[bl] = true
					}
				}
				return true
			})
			ast.Inspect(f, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.BasicLit:
					if x.Kind != token.STRING || anahtar[x] {
						return true
					}
					v, err := strconv.Unquote(x.Value)
					if err != nil {
						return true
					}
					if (!strings.Contains(v, "/") || path.IsAbs(v)) && kimlikDusurenler[path.Base(v)] {
						bulunan[rel] = append(bulunan[rel], fset.Position(x.Pos()).String()+" "+x.Value)
					}
				case *ast.Ident:
					// syscall.Credential / SysProcAttr{Credential: ...}: Go içinden uid düşürme.
					if x.Name == "Credential" {
						bulunan[rel] = append(bulunan[rel], fset.Position(x.Pos()).String()+" Credential")
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if taranan < 100 {
		t.Fatalf("yalnız %d dosya tarandı — depo kökü yanlış mı?", taranan)
	}
	dosyalar := make([]string, 0, len(bulunan))
	for d := range bulunan {
		dosyalar = append(dosyalar, d)
	}
	sort.Strings(dosyalar)
	for _, d := range dosyalar {
		if izinli, ok := kaynakIstisnalari[d]; ok && len(bulunan[d]) == izinli {
			continue
		}
		t.Errorf("%s: kimlik düşürme kiraci.Komut dışında (panel ortamı çocuğa geçebilir) — kiraci.Kiraci{...}.Komut kullan:\n  %s",
			d, strings.Join(bulunan[d], "\n  "))
	}
	for d, izinli := range kaynakIstisnalari {
		if len(bulunan[d]) != izinli {
			t.Errorf("istisna %s: %d değişmez bekleniyordu, %d bulundu — istisnayı güncelle/kaldır", d, izinli, len(bulunan[d]))
		}
	}
}
