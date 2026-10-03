package denetim

// kaynak_tarama_test.go — kaynak kodda audit_log'a yazılan TÜM eylem kodlarını
// go/ast ile toplar. Elle tutulan bir liste yeni eylem eklendiğinde unutulur; bu
// tarayıcı denetim yazan fonksiyonları (httpx.Denetim* ve onları saran yerel
// yardımcıları) kendisi bulur ve eylem argümanını literal değerlere çözer.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// tohumHavuzlar: audit_log'a doğrudan yazan fonksiyonlar → eylem argümanının sırası.
// Bunları saran yardımcılar (eylem parametresini aynen geçirenler) tarayıcı
// tarafından kendiliğinden havuza eklenir.
var tohumHavuzlar = map[string]int{
	"*:Denetim":                4,
	"*:DenetimDomain":          4,
	"*:DenetimSistem":          3,
	"internal/auth:writeAudit": 4,
}

var eylemKodDeseni = regexp.MustCompile(`^[a-z][a-z0-9_]*([._-][a-z0-9_]+)*$`)

// sqlEylemDeseni: doğrudan INSERT INTO audit_log ... VALUES(..., 'kod', ...) yazan SQL.
var sqlEylemDeseni = regexp.MustCompile(`'([a-z][a-z0-9_]*\.[a-z0-9_.-]+)'`)

type taranmisDosya struct {
	yol string // depo köküne göre
	pkg string // dizin (havuz anahtarı öneki)
	f   *ast.File
}

type taramaSonucu struct {
	kodlar     map[string][]string // kod → konumlar
	cozulemedi []string            // eylemi literal'e çözülemeyen çağrılar (konum + ifade)
}

func depoKoku(t *testing.T) string {
	t.Helper()
	d, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(d, "go.mod")); err != nil {
		t.Fatalf("depo kökü bulunamadı (%s): %v", d, err)
	}
	return d
}

func kaynaklariOku(t *testing.T, kok string) (*token.FileSet, []taranmisDosya) {
	t.Helper()
	fs := token.NewFileSet()
	var out []taranmisDosya
	for _, alt := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(kok, alt), func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if n := d.Name(); n == "node_modules" || n == "testdata" || strings.HasPrefix(n, ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			f, perr := parser.ParseFile(fs, p, nil, 0)
			if perr != nil {
				return perr
			}
			rel, _ := filepath.Rel(kok, p)
			out = append(out, taranmisDosya{yol: filepath.ToSlash(rel), pkg: filepath.ToSlash(filepath.Dir(rel)), f: f})
			return nil
		})
		if err != nil {
			t.Fatalf("kaynak taranamadı: %v", err)
		}
	}
	return fs, out
}

func cagriAdi(c *ast.CallExpr) string {
	switch fn := c.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		return fn.Sel.Name
	}
	return ""
}

func parametreSirasi(ft *ast.FuncType, ad string) int {
	if ft == nil || ft.Params == nil {
		return -1
	}
	i := 0
	for _, alan := range ft.Params.List {
		if len(alan.Names) == 0 {
			i++
			continue
		}
		for _, n := range alan.Names {
			if n.Name == ad {
				return i
			}
			i++
		}
	}
	return -1
}

// atamalar: govde içinde ad'a yapılan atamaların sağ tarafları (literal ise değer).
func atamalar(govde ast.Node, ad string) (lit []string, literalDisi bool, bulundu bool) {
	if govde == nil {
		return nil, false, false
	}
	ekle := func(e ast.Expr) {
		bulundu = true
		if bl, ok := e.(*ast.BasicLit); ok && bl.Kind == token.STRING {
			if s, err := strconv.Unquote(bl.Value); err == nil {
				lit = append(lit, s)
				return
			}
		}
		literalDisi = true
	}
	ast.Inspect(govde, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			for i, l := range x.Lhs {
				if id, ok := l.(*ast.Ident); ok && id.Name == ad {
					if len(x.Rhs) == len(x.Lhs) {
						ekle(x.Rhs[i])
					} else {
						bulundu, literalDisi = true, true
					}
				}
			}
		case *ast.ValueSpec:
			for i, id := range x.Names {
				if id.Name == ad && i < len(x.Values) {
					ekle(x.Values[i])
				}
			}
		}
		return true
	})
	return
}

func kaynakEylemleriniTara(t *testing.T) taramaSonucu {
	t.Helper()
	kok := depoKoku(t)
	fs, dosyalar := kaynaklariOku(t, kok)

	// Paket düzeyi sabit/değişkenler: pkg → ad → literal değerler.
	pkgSabit := map[string]map[string][]string{}
	for _, d := range dosyalar {
		for _, decl := range d.f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, sp := range gd.Specs {
				vs, ok := sp.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, id := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					if bl, ok := vs.Values[i].(*ast.BasicLit); ok && bl.Kind == token.STRING {
						if s, err := strconv.Unquote(bl.Value); err == nil {
							if pkgSabit[d.pkg] == nil {
								pkgSabit[d.pkg] = map[string][]string{}
							}
							pkgSabit[d.pkg][id.Name] = append(pkgSabit[d.pkg][id.Name], s)
						}
					}
				}
			}
		}
	}

	havuz := map[string]int{}
	for k, v := range tohumHavuzlar {
		havuz[k] = v
	}
	havuzBul := func(pkg, ad string) (int, bool) {
		if i, ok := havuz[pkg+":"+ad]; ok {
			return i, true
		}
		i, ok := havuz["*:"+ad]
		return i, ok
	}

	var son taramaSonucu
	for tur := 0; tur < 10; tur++ {
		son = taramaSonucu{kodlar: map[string][]string{}}
		degisti := false
		for _, d := range dosyalar {
			var yigin []ast.Node
			ast.Inspect(d.f, func(n ast.Node) bool {
				if n == nil {
					yigin = yigin[:len(yigin)-1]
					return true
				}
				yigin = append(yigin, n)
				c, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				idx, ok := havuzBul(d.pkg, cagriAdi(c))
				if !ok || idx >= len(c.Args) {
					return true
				}
				konum := fs.Position(c.Pos())
				yer := d.yol + ":" + strconv.Itoa(konum.Line)
				arg := c.Args[idx]
				// en yakın kapsayan fonksiyon + en dış FuncDecl
				var yakin ast.Node
				var dis *ast.FuncDecl
				var yakinEbeveyn ast.Node
				for i := len(yigin) - 2; i >= 0; i-- {
					switch f := yigin[i].(type) {
					case *ast.FuncLit:
						if yakin == nil {
							yakin = f
							if i > 0 {
								yakinEbeveyn = yigin[i-1]
							}
						}
					case *ast.FuncDecl:
						if yakin == nil {
							yakin = f
						}
						dis = f
					}
				}
				switch a := arg.(type) {
				case *ast.BasicLit:
					if s, err := strconv.Unquote(a.Value); err == nil && a.Kind == token.STRING {
						son.kodlar[s] = append(son.kodlar[s], yer)
						return true
					}
				case *ast.Ident:
					var ft *ast.FuncType
					switch f := yakin.(type) {
					case *ast.FuncLit:
						ft = f.Type
					case *ast.FuncDecl:
						ft = f.Type
					}
					if p := parametreSirasi(ft, a.Name); p >= 0 {
						// Saran yardımcı: onu çağıranlar da taranır.
						var anahtar string
						switch f := yakin.(type) {
						case *ast.FuncDecl:
							if ast.IsExported(f.Name.Name) {
								anahtar = "*:" + f.Name.Name
							} else {
								anahtar = d.pkg + ":" + f.Name.Name
							}
						case *ast.FuncLit:
							if kv, ok := yakinEbeveyn.(*ast.KeyValueExpr); ok {
								if id, ok := kv.Key.(*ast.Ident); ok {
									anahtar = "*:" + id.Name
								}
							}
						}
						if anahtar == "" {
							son.cozulemedi = append(son.cozulemedi, yer+" (adsız fonksiyon parametresi "+a.Name+")")
							return true
						}
						if eski, ok := havuz[anahtar]; !ok || eski != p {
							havuz[anahtar] = p
							degisti = true
						}
						return true
					}
					var govde ast.Node
					if dis != nil {
						govde = dis.Body
					}
					lit, disi, bulundu := atamalar(govde, a.Name)
					if !bulundu {
						if v, ok := pkgSabit[d.pkg][a.Name]; ok {
							lit, bulundu = v, true
						}
					}
					for _, s := range lit {
						son.kodlar[s] = append(son.kodlar[s], yer)
					}
					if disi || !bulundu {
						son.cozulemedi = append(son.cozulemedi, yer+" ("+a.Name+")")
					}
					return true
				}
				son.cozulemedi = append(son.cozulemedi, yer+" (ifade)")
				return true
			})
		}
		// Doğrudan SQL ile yazanlar.
		for _, d := range dosyalar {
			ast.Inspect(d.f, func(n ast.Node) bool {
				bl, ok := n.(*ast.BasicLit)
				if !ok || bl.Kind != token.STRING || !strings.Contains(bl.Value, "INSERT INTO audit_log") {
					return true
				}
				for _, m := range sqlEylemDeseni.FindAllStringSubmatch(bl.Value, -1) {
					son.kodlar[m[1]] = append(son.kodlar[m[1]], d.yol+":"+strconv.Itoa(fs.Position(bl.Pos()).Line))
				}
				return true
			})
		}
		if !degisti {
			break
		}
	}
	sort.Strings(son.cozulemedi)
	return son
}

// Kaynakta audit_log'a yazılan HER eylem kodunun katalogda etiketi var: yeni bir
// eylem eklenip etiketlenmezse ön yüz onu ham koddan türetilmiş geri dönüşle
// gösterir — bu test onu daha birleştirmeden yakalar.
func TestEylemKataloguKaynaktakiTumKodlariKapsar(t *testing.T) {
	s := kaynakEylemleriniTara(t)
	if len(s.cozulemedi) > 0 {
		t.Fatalf("eylemi literal değere çözülemeyen denetim çağrıları (tarayıcıyı ya da çağrıyı düzeltin):\n%s",
			strings.Join(s.cozulemedi, "\n"))
	}
	// Tarayıcının kendisi bozulup HİÇBİR şey bulmazsa test boşuna geçmesin.
	for _, zorunlu := range []string{"auth.login", "hosting.sil", "musteri.adina_cikis", "erisim.ayar", "guvenlik.izolasyon_kaybi", "kapi.rol_port"} {
		if _, ok := s.kodlar[zorunlu]; !ok {
			t.Fatalf("tarayıcı bilinen %q kodunu bulamadı — tarama bozuk", zorunlu)
		}
	}
	if len(s.kodlar) < 40 {
		t.Fatalf("tarayıcı yalnız %d kod buldu — tarama bozuk olabilir", len(s.kodlar))
	}
	var eksik []string
	for kod, yer := range s.kodlar {
		if !eylemKodDeseni.MatchString(kod) {
			t.Errorf("eylem kodu biçimsiz: %q (%s)", kod, strings.Join(yer, ","))
		}
		if _, ok := eylemKatalogu[kod]; !ok {
			eksik = append(eksik, kod+"  ("+strings.Join(yer, ", ")+")")
		}
	}
	sort.Strings(eksik)
	if len(eksik) > 0 {
		t.Fatalf("eylemKatalogu'nda (internal/denetim/eylemler.go) etiketi OLMAYAN kodlar:\n%s", strings.Join(eksik, "\n"))
	}
}
