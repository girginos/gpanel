package provisioner

import (
	"os"
	"path/filepath"
	"testing"
)

// yedekYonlendir — eski/yeni yedek yollarını geçici dizine çevirir.
func yedekYonlendir(t *testing.T) (eski, yeni string) {
	t.Helper()
	d := t.TempDir()
	oEski, oDizin := nginxLogrotateEskiYedek, nginxLogrotateYedekDizin
	nginxLogrotateEskiYedek = filepath.Join(d, "logrotate.d", "nginx.dagitim.yedek")
	nginxLogrotateYedekDizin = filepath.Join(d, "var", "logrotate-yedek")
	t.Cleanup(func() { nginxLogrotateEskiYedek, nginxLogrotateYedekDizin = oEski, oDizin })
	if err := os.MkdirAll(filepath.Dir(nginxLogrotateEskiYedek), 0o755); err != nil {
		t.Fatal(err)
	}
	return nginxLogrotateEskiYedek, nginxLogrotateYedekYolu()
}

// Eski kurulumun /etc/logrotate.d içindeki yedeği dizin dışına taşınır, içerik korunur.
func TestNginxLogrotateEskiYedekTasi_Tasir(t *testing.T) {
	eski, yeni := yedekYonlendir(t)
	if err := os.WriteFile(eski, []byte("/var/log/nginx/*.log {\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	nginxLogrotateEskiYedekTasi()
	if _, err := os.Stat(eski); !os.IsNotExist(err) {
		t.Fatalf("eski yedek logrotate.d icinde kaldi (logrotate duplicate hatasi surer): %v", err)
	}
	b, err := os.ReadFile(yeni)
	if err != nil || string(b) != "/var/log/nginx/*.log {\n}\n" {
		t.Fatalf("yedek icerigi korunmadi: %q %v", b, err)
	}
	if fi, _ := os.Stat(yeni); fi.Mode().Perm() != 0o600 {
		t.Fatalf("yedek izni 0600 olmali: %v", fi.Mode().Perm())
	}
}

// Yeni konumda gerçek dağıtım yedeği varsa ezilmez; eski kopya yalnız silinir.
func TestNginxLogrotateEskiYedekTasi_MevcutYedegiEzmez(t *testing.T) {
	eski, yeni := yedekYonlendir(t)
	if err := os.MkdirAll(filepath.Dir(yeni), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(yeni, []byte("GERCEK-DAGITIM"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(eski, []byte("BASKA"), 0o644); err != nil {
		t.Fatal(err)
	}
	nginxLogrotateEskiYedekTasi()
	if b, _ := os.ReadFile(yeni); string(b) != "GERCEK-DAGITIM" {
		t.Fatalf("mevcut dagitim yedegi ezildi: %q", b)
	}
	if _, err := os.Stat(eski); !os.IsNotExist(err) {
		t.Fatalf("eski kopya silinmedi: %v", err)
	}
}

// Eski yedek yoksa hiçbir şey yapmaz (yeni dizin bile oluşmaz); ikinci çağrı da zararsız.
func TestNginxLogrotateEskiYedekTasi_YoksaNoop(t *testing.T) {
	_, yeni := yedekYonlendir(t)
	nginxLogrotateEskiYedekTasi()
	nginxLogrotateEskiYedekTasi()
	if _, err := os.Stat(yeni); !os.IsNotExist(err) {
		t.Fatalf("yoktan yedek uretildi: %v", err)
	}
}

// Yedek yazılamıyorsa (dizin yerine dosya var) ESKİ yedek silinmez — veri kaybı olmaz.
func TestNginxLogrotateEskiYedekTasi_YazilamazsaEskiyiSilmez(t *testing.T) {
	eski, _ := yedekYonlendir(t)
	if err := os.MkdirAll(filepath.Dir(nginxLogrotateYedekDizin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nginxLogrotateYedekDizin, []byte("dosya"), 0o600); err != nil { // dizin yerine dosya
		t.Fatal(err)
	}
	if err := os.WriteFile(eski, []byte("DEGERLI"), 0o644); err != nil {
		t.Fatal(err)
	}
	nginxLogrotateEskiYedekTasi()
	if b, err := os.ReadFile(eski); err != nil || string(b) != "DEGERLI" {
		t.Fatalf("yedek yazilamadi ama eski silindi: %q %v", b, err)
	}
}
