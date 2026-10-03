package main

// config.Load panel sırlarını ortamdan SİLER; Redis yönetici parolası artık
// yalnız main'deki redis.AdminParolaAyarla(cfg.RedisAdminPass) ile verilir.
// Bu satır kaybolursa (ör. main'e elle port edilen hotfix'te) adminPass() boş
// döner, Redis özelliği sessizce "yapılandırılmamış" olur ve valkey-cli
// REDISCLI_AUTH'u boş gider — hiçbir paket testi bunu görmez. Bu test main
// gövdesini AST ile kilitler.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// mainGovdesi: main.go'daki func main'in üst düzey deyimleri.
func mainGovdesi(t *testing.T, kaynak any) []ast.Stmt {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "main.go", kaynak, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == "main" && fd.Body != nil {
			return fd.Body.List
		}
	}
	t.Fatal("func main bulunamadı")
	return nil
}

// secici: e "<x>.<ad>" ise x'in adını döner.
func secici(e ast.Expr, ad string) (string, bool) {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != ad {
		return "", false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	return id.Name, true
}

// redisParolaBaglantisi: config.Load'dan hemen sonra (arada yalnız hata denetimi)
// üst düzeyde redis.AdminParolaAyarla(<cfg>.RedisAdminPass) var mı? Yoksa neden döner.
func redisParolaBaglantisi(govde []ast.Stmt) string {
	yukle, cfg := -1, ""
	for i, s := range govde {
		as, ok := s.(*ast.AssignStmt)
		if !ok || len(as.Rhs) != 1 || len(as.Lhs) == 0 {
			continue
		}
		c, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok {
			continue
		}
		if p, ok := secici(c.Fun, "Load"); ok && p == "config" {
			if id, ok := as.Lhs[0].(*ast.Ident); ok {
				yukle, cfg = i, id.Name
			}
			break
		}
	}
	if yukle < 0 {
		return "main'de üst düzey cfg, err := config.Load() yok"
	}
	for i := yukle + 1; i < len(govde); i++ {
		if _, ok := govde[i].(*ast.IfStmt); ok && i == yukle+1 {
			continue // if err != nil { log.Fatalf(...) }
		}
		es, ok := govde[i].(*ast.ExprStmt)
		if !ok {
			break
		}
		c, ok := es.X.(*ast.CallExpr)
		if !ok {
			break
		}
		if p, ok := secici(c.Fun, "AdminParolaAyarla"); !ok || p != "redis" {
			break
		}
		if len(c.Args) != 1 {
			return "redis.AdminParolaAyarla tek argüman almalı"
		}
		if x, ok := secici(c.Args[0], "RedisAdminPass"); !ok || x != cfg {
			return "redis.AdminParolaAyarla argümanı " + cfg + ".RedisAdminPass olmalı"
		}
		return ""
	}
	return "config.Load'dan hemen sonra (yalnız hata denetiminden sonra) redis.AdminParolaAyarla(" + cfg + ".RedisAdminPass) yok"
}

func TestMain_RedisAdminParolasiConfigtenVerilir(t *testing.T) {
	if neden := redisParolaBaglantisi(mainGovdesi(t, nil)); neden != "" {
		t.Fatal(neden)
	}
}

func TestMain_RedisAdminParolasiKurali_Ornekler(t *testing.T) {
	for _, d := range []struct {
		ad, govde string
		gecer     bool
	}{
		{"dogru", "cfg, err := config.Load()\nif err != nil { panic(err) }\nredis.AdminParolaAyarla(cfg.RedisAdminPass)", true},
		{"eksik", "cfg, err := config.Load()\nif err != nil { panic(err) }\n_ = cfg", false},
		{"yanlis-arguman", "cfg, err := config.Load()\nif err != nil { panic(err) }\nredis.AdminParolaAyarla(\"\")", false},
		{"gec", "cfg, err := config.Load()\nif err != nil { panic(err) }\nbasla()\nredis.AdminParolaAyarla(cfg.RedisAdminPass)", false},
		{"kosullu", "cfg, err := config.Load()\nif err != nil { panic(err) }\nif x { redis.AdminParolaAyarla(cfg.RedisAdminPass) }", false},
		{"load-yok", "redis.AdminParolaAyarla(\"\")", false},
	} {
		govde := mainGovdesi(t, "package main\nfunc main() {\n"+d.govde+"\n}\n")
		if neden := redisParolaBaglantisi(govde); (neden == "") != d.gecer {
			t.Errorf("%s: geçer=%v bekleniyordu, neden=%q", d.ad, d.gecer, neden)
		}
	}
}
