package config

// KALICI düzen: birim env dosyasını LoadCredential ile verir; sırlar süreç
// ortamına HİÇ girmez. Bu testler config.Load'un kimlik yolunu ve birimin/gizli
// dosya yazımlarının kaynak düzeyinde bu düzeni bozmadığını kilitler.

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"girginospanel/internal/kiraci/kiracitest"
)

func kimlikKur(t *testing.T, icerik string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, KimlikAdi), []byte(icerik), 0o400); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CREDENTIALS_DIRECTORY", dir)
	for _, k := range append(append([]string{}, SirAdlari...), "PANEL_LISTEN", "PANEL_SURUM_KONTROL", "PANEL_LISANS_SUNUCU") {
		if v, ok := os.LookupEnv(k); ok {
			t.Setenv(k, v) // geri yükleme kaydı
			os.Unsetenv(k)
		}
	}
}

func sahteKimlikIcerigi() string {
	return "# panel env\nPANEL_LISTEN=127.0.0.1:9191\nPANEL_DB_DSN=" + kiracitest.SahteSirlar["PANEL_DB_DSN"] +
		"\nPANEL_JWT_SECRET=\"" + kiracitest.SahteSirlar["PANEL_JWT_SECRET"] + "\"\nPANEL_REDIS_ADMIN_PASS=" +
		kiracitest.SahteSirlar["PANEL_REDIS_ADMIN_PASS"] + "\nPANEL_SURUM_KONTROL=0\nexport PANEL_LISANS_SUNUCU=https://lisans.ornek\n"
}

func TestLoad_KimliktenOkurSirOrtamaGirmez(t *testing.T) {
	kimlikKur(t, sahteKimlikIcerigi())
	t.Cleanup(func() {
		os.Unsetenv("PANEL_LISTEN")
		os.Unsetenv("PANEL_SURUM_KONTROL")
		os.Unsetenv("PANEL_LISANS_SUNUCU")
	})
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SirKaynagi != "kimlik" {
		t.Errorf("SirKaynagi=%q", cfg.SirKaynagi)
	}
	if string(cfg.JWTSecret) != kiracitest.SahteSirlar["PANEL_JWT_SECRET"] || cfg.DBDsn != kiracitest.SahteSirlar["PANEL_DB_DSN"] ||
		cfg.RedisAdminPass != kiracitest.SahteSirlar["PANEL_REDIS_ADMIN_PASS"] {
		t.Error("sırlar kimlikten okunmadı")
	}
	if cfg.ListenAddr != "127.0.0.1:9191" {
		t.Errorf("ListenAddr=%q", cfg.ListenAddr)
	}
	// Sır OLMAYAN ayarlar eski EnvironmentFile gibi ortamda (os.Getenv ile okuyan kod için).
	if os.Getenv("PANEL_SURUM_KONTROL") != "0" || os.Getenv("PANEL_LISANS_SUNUCU") != "https://lisans.ornek" {
		t.Error("sır olmayan ayar ortama konmadı")
	}
	if s := kiracitest.Sizinti(os.Environ()); len(s) > 0 {
		t.Errorf("kimlik yolunda sır süreç ortamında: %v", s)
	}
	out, err := exec.Command("/usr/bin/env").Output()
	if err == nil {
		if s := kiracitest.Sizinti(strings.Split(string(out), "\n")); len(s) > 0 {
			t.Errorf("çocuk süreç sırrı gördü: %v", s)
		}
	}
}

// Kimlik varsa sırların TEK kaynağı odur: ortamda kalmış eski değer onu ezmez.
func TestLoad_KimlikOrtamiEzer(t *testing.T) {
	kimlikKur(t, sahteKimlikIcerigi())
	t.Setenv("PANEL_JWT_SECRET", "ortamdaki-eski-sir-0000000000000000000000000000")
	t.Cleanup(func() {
		os.Unsetenv("PANEL_LISTEN")
		os.Unsetenv("PANEL_SURUM_KONTROL")
		os.Unsetenv("PANEL_LISANS_SUNUCU")
	})
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if string(cfg.JWTSecret) != kiracitest.SahteSirlar["PANEL_JWT_SECRET"] {
		t.Error("ortamdaki eski sır kimliği ezdi")
	}
	if _, ok := os.LookupEnv("PANEL_JWT_SECRET"); ok {
		t.Error("ortamdaki sır silinmedi")
	}
}

// Kimlik dizini var ama panel-env okunamıyor → HATA (sessizce ortama düşmez).
func TestLoad_KimlikOkunamazsaHata(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, KimlikAdi), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CREDENTIALS_DIRECTORY", dir)
	sahteSirlariKur(t)
	if _, err := Load(); err == nil {
		t.Fatal("okunamayan kimlik hata vermeliydi")
	}
	if s := kiracitest.Sizinti(os.Environ()); len(s) > 0 {
		t.Errorf("hata yolunda sırlar ortamda kaldı: %v", s)
	}
}

// Başka bir kimlik için dizin var, panel-env yok → eski yol (ortam).
func TestLoad_KimlikDizinindePanelEnvYoksaOrtam(t *testing.T) {
	t.Setenv("CREDENTIALS_DIRECTORY", t.TempDir())
	sahteSirlariKur(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SirKaynagi != "ortam" || string(cfg.JWTSecret) != kiracitest.SahteSirlar["PANEL_JWT_SECRET"] {
		t.Error("geriye uyum yolu çalışmadı")
	}
}

func TestEnvAyristir(t *testing.T) {
	m := EnvAyristir([]byte("# y\n; y\n\nA=1\n B = 2 \nC=\"x y\"\nD='z'\nexport E=5\n9X=bad\nF=a=b\nA=son\nG\n"))
	bek := map[string]string{"A": "son", "B": "2", "C": "x y", "D": "z", "E": "5", "F": "a=b"}
	if len(m) != len(bek) {
		t.Fatalf("%v", m)
	}
	for k, v := range bek {
		if m[k] != v {
			t.Errorf("%s=%q (beklenen %q)", k, m[k], v)
		}
	}
}

func depoKoku(t *testing.T) string {
	t.Helper()
	kok, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return kok
}

// TestKaynakKoruma_BirimSirOrtamaKoymaz: panel birimi env'i kimlik olarak verir ve
// EnvironmentFile ile YÜKLEMEZ. EnvironmentFile geri eklenirse sırlar yeniden süreç
// ortamına (kiracı çocuklarına, /proc/<pid>/environ) girer.
func TestKaynakKoruma_BirimSirOrtamaKoymaz(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(depoKoku(t), "assets/systemd/girginospanel.service"))
	if err != nil {
		t.Fatal(err)
	}
	kimlik := false
	for _, s := range strings.Split(string(b), "\n") {
		s = strings.TrimSpace(s)
		if s == "" || s[0] == '#' || s[0] == ';' {
			continue
		}
		if strings.HasPrefix(s, "EnvironmentFile=") || strings.HasPrefix(s, "Environment=") && SirIcerirMi(s) {
			t.Errorf("birim sırları süreç ortamına koyuyor: %q", s)
		}
		if s == "LoadCredential="+KimlikAdi+":"+EnvYolu {
			kimlik = true
		}
	}
	if !kimlik {
		t.Errorf("birimde LoadCredential=%s:%s yok", KimlikAdi, EnvYolu)
	}
}

// SirIcerirMi: "Environment=A=b B=c" satırında sır değişkeni var mı?
func SirIcerirMi(satir string) bool {
	for _, p := range strings.Fields(strings.TrimPrefix(satir, "Environment=")) {
		k, _, _ := strings.Cut(strings.Trim(p, `"`), "=")
		if SirDegiskeniMi(k) {
			return true
		}
	}
	return false
}

// gizliYolDeseni: yazılan yolun ifadesi bir panel gizli dosyasını gösteriyorsa
// (anahtar/sır/jeton/env/kimlik). Kaynak düzeyinde ifade metnine bakılır.
var gizliYolDeseni = regexp.MustCompile(`(?i)(\.key\b|key"|KeyPath|keyYedek|\bkey\b|secret|token|anahtarYol|kimlikYol|/etc/girginospanel/|EnvYolu|calEnvYol|\.pfsql|users\.acl|panel\.key)`)

// TestKaynakKoruma_GizliDosyaSirdosyaIleYazilir: panel kodunda (cmd/server, internal)
// bir gizli dosya os.WriteFile / os.OpenFile / os.Create ile YAZILMAZ — sirdosya.Yaz
// kullanılır. os.WriteFile(yol, b, 0600) gizli dosya için yetersizdir: dosya önceden
// 0644 kaldıysa izin DEĞİŞMEZ (sır herkese okunur kalır), symlink takip edilir, yarım
// yazma yarım dosya bırakır. Ayrıca HİÇBİR gizli dosya grup/diğer YAZMA ya da diğer
// OKUMA izniyle oluşturulmaz.
func TestKaynakKoruma_GizliDosyaSirdosyaIleYazilir(t *testing.T) {
	kok := depoKoku(t)
	fset := token.NewFileSet()
	taranan := 0
	for _, alt := range []string{"cmd/server", "internal"} {
		err := filepath.WalkDir(filepath.Join(kok, alt), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "node_modules" || d.Name() == "testdata" || d.Name() == "sirdosya" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			rel, _ := filepath.Rel(kok, p)
			f, perr := parser.ParseFile(fset, p, nil, 0)
			if perr != nil {
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
				if !ok || (x.Name != "os" && x.Name != "ioutil") {
					return true
				}
				fn := se.Sel.Name
				if fn != "WriteFile" && fn != "OpenFile" && fn != "Create" {
					return true
				}
				var yb bytes.Buffer
				_ = printer.Fprint(&yb, fset, ce.Args[0])
				yol := yb.String()
				if !gizliYolDeseni.MatchString(yol) || gizliYolIstisna(rel, yol) {
					return true
				}
				if fn == "OpenFile" && len(ce.Args) >= 2 {
					var fb bytes.Buffer
					_ = printer.Fprint(&fb, fset, ce.Args[1])
					if !strings.Contains(fb.String(), "O_WRONLY") && !strings.Contains(fb.String(), "O_RDWR") && !strings.Contains(fb.String(), "O_CREATE") {
						return true // salt okuma
					}
				}
				t.Errorf("%s: os.%s(%s, ...) — gizli dosya sirdosya.Yaz/YazKok ile yazılmalı (atomik, izin TAM, symlink takip etmez)",
					fset.Position(ce.Pos()), fn, yol)
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if taranan < 100 {
		t.Fatalf("yalnız %d dosya tarandı", taranan)
	}
}

// gizliYolIstisna: desene uyan ama gizli OLMAYAN (genel/sertifika/kilit) dosyalar.
// Her istisna gerekçeli; yeni istisna eklemek bilinçli bir karar olmalı.
func gizliYolIstisna(rel, yol string) bool {
	istisnalar := map[string][]string{
		// sertifika (genel anahtar) yazımları — özel anahtar DEĞİL
		"internal/panelhost/sslkur.go": {"certYedek", "cert", "CertPath"},
	}
	for _, y := range istisnalar[filepath.ToSlash(rel)] {
		if yol == y {
			return true
		}
	}
	return false
}
