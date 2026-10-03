package config

// Kök neden kilidi: panel sırları config.Load okuduktan sonra süreç ortamından
// SİLİNİR → hiçbir çocuk süreç (kök ya da kiracı) onları miras alamaz.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"girginospanel/internal/kiraci/kiracitest"
)

func sahteSirlariKur(t *testing.T) {
	t.Helper()
	for k, v := range kiracitest.SahteSirlar {
		t.Setenv(k, v)
	}
}

func TestLoad_SirlarOrtamdanSilinir(t *testing.T) {
	sahteSirlariKur(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if string(cfg.JWTSecret) != kiracitest.SahteSirlar["PANEL_JWT_SECRET"] {
		t.Error("JWTSecret config'e okunmadı")
	}
	if cfg.DBDsn != kiracitest.SahteSirlar["PANEL_DB_DSN"] {
		t.Error("DBDsn config'e okunmadı")
	}
	if cfg.RedisAdminPass != kiracitest.SahteSirlar["PANEL_REDIS_ADMIN_PASS"] {
		t.Error("RedisAdminPass config'e okunmadı")
	}
	for k := range kiracitest.SahteSirlar {
		if v, ok := os.LookupEnv(k); ok {
			t.Errorf("%s config yüklendikten sonra süreç ortamında KALDI (uzunluk %d)", k, len(v))
		}
	}
	if s := kiracitest.Sizinti(os.Environ()); len(s) > 0 {
		t.Errorf("os.Environ panel sırrı taşıyor: %v", s)
	}
}

// Hatalı yapılandırmada (kısa JWT sırrı) da sırlar ortamda kalmaz.
func TestLoad_HatadaDaSirlarSilinir(t *testing.T) {
	sahteSirlariKur(t)
	t.Setenv("PANEL_JWT_SECRET", "kisa")
	if _, err := Load(); err == nil {
		t.Fatal("kısa JWT sırrı reddedilmeli")
	}
	if s := kiracitest.Sizinti(os.Environ()); len(s) > 0 {
		t.Errorf("hata yolunda sırlar ortamda kaldı: %v", s)
	}
	if _, ok := os.LookupEnv("PANEL_JWT_SECRET"); ok {
		t.Error("PANEL_JWT_SECRET hata yolunda ortamda kaldı")
	}
}

// Gelecekte eklenecek sırlar desenle yakalanır; sır olmayan ayarlar kalır.
func TestSirlariOrtamdanSil_DesenVeListe(t *testing.T) {
	sirlar := []string{"PANEL_YENI_API_KEY", "PANEL_SMTP_PASSWORD", "PANEL_LISANS_TOKEN",
		"GOSP_YEDEK_PAROLA", "PANEL_S3_SECRET_ACCESS", "PANEL_WEBHOOK_SIRRI", "PANEL_DB_DSN"}
	kalanlar := []string{"PANEL_LISTEN", "PANEL_ENV", "PANEL_JWT_LIFETIME_SEC", "PANEL_PUBLIC_IPV4",
		"PANEL_SURUM_KONTROL", "PANEL_SURUM_UC", "GOSP_SUREC_DEBUG", "HOME", "PATH_TEST_X", "SSH_AUTH_SOCK_X"}
	for _, k := range append(append([]string{}, sirlar...), kalanlar...) {
		t.Setenv(k, "deger-"+k)
	}
	SirlariOrtamdanSil()
	for _, k := range sirlar {
		if _, ok := os.LookupEnv(k); ok {
			t.Errorf("%s sır sayılmalı ve silinmeliydi", k)
		}
	}
	for _, k := range kalanlar {
		if os.Getenv(k) != "deger-"+k {
			t.Errorf("%s sır değil, silinmemeliydi", k)
		}
	}
}

// Env'i hiç ayarlanmamış (miras alan) bir KÖK çocuk süreç de sırrı görmez.
func TestLoad_SonrasiCocukSurecSirGormez(t *testing.T) {
	if _, err := os.Stat("/usr/bin/env"); err != nil {
		t.Skip("/usr/bin/env yok")
	}
	sahteSirlariKur(t)
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("/usr/bin/env").Output()
	if err != nil {
		t.Fatal(err)
	}
	if s := kiracitest.Sizinti(strings.Split(string(out), "\n")); len(s) > 0 {
		t.Fatalf("config yüklendikten sonra başlatılan çocuk süreç panel sırlarını gördü: %v", s)
	}
}

// TestKaynakKoruma_SirGetenvYalnizConfigte: panel sunucusunun kodunda (cmd/server,
// internal/) bir sır değişkeni os.Getenv/os.LookupEnv/syscall.Getenv ile YALNIZ
// internal/config'te okunur. Başka yerde okumak iki kez yanlıştır: config onu
// zaten sildiği için boş döner (sessiz işlev kaybı) ve silmeyi geri almak için
// baskı yaratır. Sırra ihtiyaç duyan kod Config alanını alır.
func TestKaynakKoruma_SirGetenvYalnizConfigte(t *testing.T) {
	kok, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	taranan := 0
	for _, alt := range []string{"cmd/server", "internal"} {
		err := filepath.WalkDir(filepath.Join(kok, alt), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "node_modules" || d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			rel, _ := filepath.Rel(kok, p)
			if filepath.Dir(rel) == filepath.Join("internal", "config") {
				return nil
			}
			f, perr := parser.ParseFile(fset, p, nil, 0)
			if perr != nil {
				t.Errorf("%s: %v", rel, perr)
				return nil
			}
			taranan++
			ast.Inspect(f, func(n ast.Node) bool {
				ce, ok := n.(*ast.CallExpr)
				if !ok || len(ce.Args) == 0 {
					return true
				}
				se, ok := ce.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				x, ok := se.X.(*ast.Ident)
				if !ok {
					return true
				}
				fn := x.Name + "." + se.Sel.Name
				if fn != "os.Getenv" && fn != "os.LookupEnv" && fn != "syscall.Getenv" {
					return true
				}
				bl, ok := ce.Args[0].(*ast.BasicLit)
				if !ok || bl.Kind != token.STRING {
					return true
				}
				ad, _ := strconv.Unquote(bl.Value)
				if SirDegiskeniMi(ad) {
					t.Errorf("%s: %s(%q) — panel sırrı config dışında ortamdan okunuyor (config.Load onu siler; Config alanını kullan)",
						fset.Position(ce.Pos()), fn, ad)
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
}
