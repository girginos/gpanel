package main

// JWT sırrı TEK kaynaktan (cfg.JWTSecret ← config.Load ← LoadCredential/env) gelir ve
// panelin TÜM jeton türleri (yönetici/bayi, müşteri) onunla imzalanır →
// `sir yenile jwt` + restart sonrası HEPSİ geçersizleşir

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"
	"testing"

	"girginospanel/internal/auth"
)

func TestKaynakKoruma_TumJetonlarCfgJWTSecretIle(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ifade := func(e ast.Expr) string {
		var b bytes.Buffer
		_ = printer.Fprint(&b, fset, e)
		return b.String()
	}
	secret, requireAuth := 0, 0
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.KeyValueExpr:
			if k, ok := x.Key.(*ast.Ident); ok && k.Name == "Secret" {
				secret++
				if v := ifade(x.Value); v != "cfg.JWTSecret" {
					t.Errorf("%s: Secret: %s — jeton sırrı cfg.JWTSecret dışından (rotasyon kapsamaz)", fset.Position(x.Pos()), v)
				}
			}
		case *ast.CallExpr:
			if s := ifade(x.Fun); strings.HasSuffix(s, "RequireAuth") && len(x.Args) == 1 {
				requireAuth++
				if v := ifade(x.Args[0]); v != "cfg.JWTSecret" {
					t.Errorf("%s: RequireAuth(%s)", fset.Position(x.Pos()), v)
				}
			}
		}
		return true
	})
	// musteri + auth handler'ları (main: adına paketi yok)
	if secret < 2 || requireAuth < 1 {
		t.Fatalf("beklenen jeton bağlantıları bulunamadı (Secret=%d RequireAuth=%d) — main.go yapısı değişti mi?", secret, requireAuth)
	}
}

// Rotasyon sonrası eski sırla imzalı müşteri ve yönetici jetonları reddedilir.
func TestJWTYenileme_MusteriVeYoneticiJetonlariDuser(t *testing.T) {
	eski := []byte(strings.Repeat("e", 48))
	yeni := []byte(strings.Repeat("y", 48))
	mtok, _, err := auth.GenerateMusteri(eski, auth.MusteriClaims{FTPHesapID: 1, DomainID: 2, Kullanici: "k", AlanAdi: "a.test"}, 3600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.ParseMusteri(eski, mtok); err != nil {
		t.Fatalf("eski sırla doğrulanamadı: %v", err)
	}
	if _, err := auth.ParseMusteri(yeni, mtok); err == nil {
		t.Error("müşteri jetonu yeni sırla KABUL edildi — rotasyon müşteri oturumlarını düşürmüyor")
	}
	atok, err := auth.IssueAt(eski, 3600, 1, "root", "admin", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Parse(yeni, atok); err == nil {
		t.Error("yönetici jetonu yeni sırla KABUL edildi")
	}
}
