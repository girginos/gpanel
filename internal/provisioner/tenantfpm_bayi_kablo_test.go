package provisioner

// tenantfpm_bayi_kablo_test.go — EnableTenantFPM (systemctl/php-fpm gerektirir, birim testinde
// koşturulamaz) drop-in'leri TEK yerden (tenantDropinleriYaz) ve daemon-reload'dan ÖNCE yazar.
// Kablolama sökülürse yeni kurulan hosting bayi havuzuna katılmadan ilk uzlaştırma turuna kadar
// eski düz dilimde çalışırdı; bu sessiz bozulmayı kaynak ağacı denetimiyle yakalar.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

func TestEnableTenantFPM_DropinleriTekYerdenVeReloadOncesiYazar(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "tenantfpm.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var govde *ast.BlockStmt
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == "EnableTenantFPM" {
			govde = fn.Body
		}
	}
	if govde == nil {
		t.Fatal("EnableTenantFPM bulunamadı")
	}
	var dropin, dogrudan int
	var dropinPos, reloadPos token.Pos
	ast.Inspect(govde, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := c.Fun.(type) {
		case *ast.Ident:
			switch fn.Name {
			case "tenantDropinleriYaz":
				dropin++
				dropinPos = c.Pos()
			case "tenantStartLimitYaz":
				dogrudan++
			}
		case *ast.SelectorExpr:
			if x, ok := fn.X.(*ast.Ident); ok && x.Name == "exec" && fn.Sel.Name == "Command" && len(c.Args) >= 2 {
				a0, ok0 := c.Args[0].(*ast.BasicLit)
				a1, ok1 := c.Args[1].(*ast.BasicLit)
				if ok0 && ok1 {
					s0, _ := strconv.Unquote(a0.Value)
					s1, _ := strconv.Unquote(a1.Value)
					if s0 == "systemctl" && s1 == "daemon-reload" && reloadPos == token.NoPos {
						reloadPos = c.Pos()
					}
				}
			}
		}
		return true
	})
	if dropin != 1 {
		t.Errorf("EnableTenantFPM tenantDropinleriYaz'ı %d kez çağırıyor (1 olmalı)", dropin)
	}
	if dogrudan != 0 {
		t.Errorf("EnableTenantFPM tenantStartLimitYaz'ı doğrudan çağırıyor (havuz drop-in'i atlanır)")
	}
	if reloadPos == token.NoPos {
		t.Fatal("EnableTenantFPM içinde daemon-reload bulunamadı")
	}
	if dropinPos == token.NoPos || dropinPos > reloadPos {
		t.Error("drop-in'ler daemon-reload'dan ÖNCE yazılmalı (yoksa ilk başlatma eski dilimde olur)")
	}
}
