package httpx

// kopuk_kapsam_test.go — "istemci koparsa uzun iş yarıda kalmaz" kuralının
// KAYNAK kapsam kilidi (rol fark etmez: kök/bayi/müşteri aynı uçları kullanır).
//
// 2026-10-03 181: tarayıcının 30 sn axios sınırı bağlantıyı kapatınca r.Context()
// iptal oldu ve hosting oluşturma zone'suz YARIM kaldı. Aynı desen (yan etkiden
// sonra r.Context() ile DB/komut/zone) müşteri uçlarında da vardı. Aşağıdaki her
// işleyici yan etkiye geçerken httpx.KopukBaglam'a geçer ve o noktadan sonra
// r.Context() KULLANMAZ. Bu test iki şeyi kilitler:
//  1. listedeki işleyici KopukBaglam çağırıyor (kaldırılırsa düşer),
//  2. KopukBaglam çağrısından SONRA gövdede r.Context() yok (geri gelirse düşer).
// Davranış testleri ayrıca: domains (Create iptal), KopukBaglam_IptalTasimaz.

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

var kopukKapsam = map[string][]string{
	"domains/handlers.go":     {"Create", "SetPHP", "SetWebBackend"},
	"domains/handlers_ssl.go": {"SSLDisable"},
	"subdomain/subdomain.go":  {"Olustur", "Sil"},
	"subdomain/ssl.go":        {"SSLKur", "SSLKaldir"},
	"subdomain/yonet.go":      {"PHPDegistir"},
	"php/php.go":              {"PutAyarlar"},
	"wordpress/wordpress.go":  {"Kur", "Sil"},
	"wordpress/toolkit.go":    {"bakimKaydet"},
	"laravel/komut.go":        {"Artisan", "Composer", "Npm"},
	"laravel/env.go":          {"Bakim"},
	"laravel/appkok.go":       {"OtomatikHazirla"},
	"laravel/gorevkuyruk.go":  {"Schedule", "Queue"},
	"laravel/kur.go":          {"Kur"},
	"laravel/deploy.go":       {"Deploy"},
	"sitekopya/sitekopya.go":  {"Olustur"},
	"uygulama/handler.go":     {"Kur", "Sil"},
	"backups/backups.go":      {"Delete"},
	"backups/restore.go":      {"Restore"},
	"git/git.go":              {"Klonla", "Pull", "Webhook"},
	"github/github.go":        {"Use"},
	"cron/cron.go":            {"Calistir"},
	"dns/dns.go":              {"Create", "Update", "Delete", "TopluSil", "TopluDurum", "ApplyTemplate"},
	"dns/dnssec.go":           {"PostDNSSEC"},
	"dns/porta.go":            {"IceAktar"},
	"dns/soa.go":              {"PutSOA"},
}

// rContextMi: r.Context() çağrısı mı?
func rContextMi(n ast.Node) bool {
	c, ok := n.(*ast.CallExpr)
	if !ok || len(c.Args) != 0 {
		return false
	}
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Context" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "r"
}

func kopukMu(n ast.Node) bool {
	c, ok := n.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "KopukBaglam" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "httpx"
}

func TestKopukBaglam_UzunIsleyicilerKapsami(t *testing.T) {
	for dosya, fonklar := range kopukKapsam {
		fs := token.NewFileSet()
		f, err := parser.ParseFile(fs, filepath.Join("..", dosya), nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", dosya, err)
		}
		bul := map[string]*ast.FuncDecl{}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				bul[fd.Name.Name] = fd
			}
		}
		for _, ad := range fonklar {
			fd := bul[ad]
			if fd == nil {
				t.Errorf("%s: %s bulunamadı", dosya, ad)
				continue
			}
			kopukPos := token.NoPos
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if kopukPos == token.NoPos && n != nil && kopukMu(n) {
					kopukPos = n.Pos()
				}
				return true
			})
			if kopukPos == token.NoPos {
				t.Errorf("%s %s: httpx.KopukBaglam YOK — istemci koparsa iş yarıda kalır", dosya, ad)
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if n != nil && n.Pos() > kopukPos && rContextMi(n) {
					t.Errorf("%s %s: KopukBaglam'dan SONRA r.Context() (%s) — iptal edilen istek işi yarıda keser",
						dosya, ad, fs.Position(n.Pos()))
				}
				return true
			})
		}
	}
}

// KopukBaglam: istek bağlamı iptal olunca KOPMAZ, değerleri taşır, üst süreyi uygular.
func TestKopukBaglam_IptalTasimaz(t *testing.T) {
	type anahtar struct{}
	ictx, iptal := context.WithCancel(context.WithValue(context.Background(), anahtar{}, "deger"))
	r := httptest.NewRequest("POST", "/x", nil).WithContext(ictx)
	ctx, kiptal := KopukBaglam(r, 50*time.Millisecond)
	defer kiptal()
	iptal()
	if r.Context().Err() == nil {
		t.Fatal("kurgu: istek bağlamı iptal olmalıydı")
	}
	if ctx.Err() != nil {
		t.Fatalf("kopuk bağlam istek iptaliyle iptal oldu: %v", ctx.Err())
	}
	if ctx.Value(anahtar{}) != "deger" {
		t.Error("istek değerleri taşınmadı")
	}
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("üst süre uygulanmadı")
	}
}
