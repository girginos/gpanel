package provisioner

// init_kok_test.go — provisioner'daki init() fonksiyonları ağır fonksiyonları KÖK yapamaz.
//
// Neden: Go linker'ı init() gövdesini her zaman köke alır. Orada RollbackToSharedFPM (veya EnableTenantFPM,
// ApplyVhostForDomain …) anılırsa, provisioner'i yalnız küçük yardımcılar için içeri alan ikililer (destek-ajan,
// eklenti-başlatıcı) o fonksiyonun TÜM bağımlılıklarını (nginx vhost üretimi, text/template, database/sql, image …)
// da bağlar: aday7 paketinde bu iki ikili +2 MB büyüdü ve gereksiz saldırı yüzeyi kazandı. Fonksiyon-değeri
// DEĞİŞKEN ilklendirmesi (`var x = F`) ise statik veridir; yalnız x erişilebilirse F korunur — onda sorun yok.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProvisionerInit_AgirFonksiyonlariKokYapmaz(t *testing.T) {
	yasak := map[string]bool{
		"RollbackToSharedFPM": true, "EnableTenantFPM": true, "ApplyVhostForDomain": true,
		"guardPostStart": true, "RepairTenantUnitDrift": true, "PHPHavuzAski": true,
	}
	dosyalar, err := filepath.Glob("*.go")
	if err != nil || len(dosyalar) == 0 {
		t.Fatalf("kaynak dosyalar bulunamadı: %v", err)
	}
	fset := token.NewFileSet()
	var ihlal []string
	for _, f := range dosyalar {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		af, err := parser.ParseFile(fset, f, b, 0)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for _, d := range af.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name.Name != "init" || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && yasak[id.Name] {
					ihlal = append(ihlal, f+": init() içinde "+id.Name+" (linker kökü olur; destek-ajan/eklenti-başlatıcı şişer)")
				}
				return true
			})
		}
	}
	if len(ihlal) > 0 {
		t.Fatalf("init() ağır fonksiyonları köke alıyor:\n  %s", strings.Join(ihlal, "\n  "))
	}
}
