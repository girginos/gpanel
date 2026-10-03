package main

// rota_api_anahtar_test.go — API anahtarı izin tablosunun ROTA ENVANTERİYLE kilidi.
//
// Yönlendirici main() içinde kurulduğu için ayağa kaldırılamaz; envanter kaynak
// dosyalardaki kayıt satırlarından çıkarılır (rota_imza_guncelle_test.go deseni):
// cmd/server ve internal altındaki test dışı .go dosyalarında `.Get/.Post/.Put/
// .Patch/.Delete/.HandleFunc("/...")`. "/api/" ile başlamayan yollar /api/v1
// altındadır (r.Route("/api/v1") ve Routes(r) çağrıları).
//
// Kanıtlanan:
//   - izin tablosundaki (Rotalar/HerZaman/KimlikUclari) her girdi gerçekten var
//     (ölü/yanlış yazılmış girdi yok — yanlış yazılmış izin sessizce "kapalı" kalırdı)
//   - adı kimlik/jeton/parola çağrıştıran HER rota bilinçli sınıflandırılmış:
//     KimlikUclari (asla) ya da KimlikBenzeriIzinli (gerekçeli). Yeni bir kimlik
//     ucu eklenince bu test DÜŞER.
//   - anahtara açık uç listesi altın dosyayla aynı (her değişiklik diff'te görünür)

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"girginospanel/internal/apianahtar"
)

var rotaKayitRe = regexp.MustCompile(`\.(Get|Post|Put|Patch|Delete|HandleFunc)\(\s*"(/[^"]*)"`)

// rotaEnvanteri: "METOD /api/v1/desen" kümesi (HandleFunc → "*").
func rotaEnvanteri(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, kok := range []string{".", "../../internal"} {
		err := filepath.WalkDir(kok, func(yol string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(yol, ".go") || strings.HasSuffix(yol, "_test.go") {
				return nil
			}
			b, err := os.ReadFile(yol)
			if err != nil {
				return err
			}
			for _, m := range rotaKayitRe.FindAllStringSubmatch(string(b), -1) {
				metod := strings.ToUpper(m[1])
				if metod == "HANDLEFUNC" {
					metod = "*"
				}
				p := m[2]
				if p == "/healthz" {
					continue
				}
				if !strings.HasPrefix(p, "/api/") {
					p = "/api/v1" + p
				}
				out[metod+" "+p] = true
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(out) < 300 {
		t.Fatalf("rota envanteri şüpheli küçük (%d) — ayrıştırıcı bozulmuş olabilir", len(out))
	}
	return out
}

func TestAnahtarIzinTablosuEnvanterde(t *testing.T) {
	env := rotaEnvanteri(t)
	for d := range apianahtar.Rotalar {
		if !env[d] {
			t.Errorf("izin tablosundaki rota envanterde YOK (ölü/yanlış girdi): %s", d)
		}
	}
	for d := range apianahtar.HerZaman {
		if !env[d] {
			t.Errorf("HerZaman girdisi envanterde YOK: %s", d)
		}
	}
	for d := range apianahtar.KimlikUclari {
		if !env[d] {
			t.Errorf("KimlikUclari girdisi envanterde YOK (rota taşındı mı?): %s", d)
		}
	}
	for d := range apianahtar.KimlikBenzeriIzinli {
		if !env[d] {
			t.Errorf("KimlikBenzeriIzinli girdisi envanterde YOK: %s", d)
		}
	}
}

// kimlikRe: adı jeton/oturum/kimlik/kimlik bilgisi çağrıştıran rotalar.
var kimlikRe = regexp.MustCompile(`(?i)(login|giris|adina|oturum|token|parola|password|2fa|totp|api-anahtar|/me$|/me/|anahtar|secret|sifre|kimlik|pma|redeem|session|eklenti/\{ad\}/\*)`)

func TestKimlikUclariSiniflandirildi(t *testing.T) {
	env := rotaEnvanteri(t)
	var sinifsiz []string
	for d := range env {
		if !kimlikRe.MatchString(d) {
			continue
		}
		if apianahtar.KimlikUclari[d] {
			continue
		}
		if _, ok := apianahtar.KimlikBenzeriIzinli[d]; ok {
			continue
		}
		if _, ok := KimlikDisiGerekce[d]; ok {
			continue
		}
		sinifsiz = append(sinifsiz, d)
	}
	sort.Strings(sinifsiz)
	if len(sinifsiz) > 0 {
		t.Fatalf("kimlik/jeton çağrıştıran %d rota SINIFLANDIRILMAMIŞ — internal/apianahtar/izin.go "+
			"KimlikUclari (anahtara asla) ya da KimlikBenzeriIzinli (gerekçeli açık) listesine, "+
			"kimlikle ilgisizse bu testteki KimlikDisiGerekce'ye ekleyin:\n  %s", len(sinifsiz), strings.Join(sinifsiz, "\n  "))
	}
	// Bu kümelerdeki hiçbir rota anahtara açık olamaz (KimlikBenzeriIzinli hariç).
	for d := range KimlikDisiGerekce {
		if _, ok := apianahtar.Rotalar[d]; ok {
			t.Errorf("KimlikDisiGerekce rotası anahtara açık — KimlikBenzeriIzinli'ye taşıyın: %s", d)
		}
	}
}

// KimlikDisiGerekce: adı desene takılan ama kimlik/jeton işlemi OLMAYAN rotalar.
// Hepsi anahtara KAPALI kalır (varsayılan reddet); açılmak istenirse
// KimlikBenzeriIzinli'ye gerekçeyle taşınmalı.
var KimlikDisiGerekce = map[string]string{
	"GET /api/v1/marka/gorsel/{anahtar}/{yuva}": "kimliksiz marka görseli; 'anahtar' görsel kimliği, kimlik bilgisi değil",
	"GET /api/v1/marka/oturum":                  "oturumdaki kullanıcının marka görünümü (salt okuma); jeton basmaz",
}

func TestAnahtarUcListesiAltin(t *testing.T) {
	var satirlar []string
	for d, k := range apianahtar.Rotalar {
		satirlar = append(satirlar, d+"  "+k.Grup+":"+k.Erisim)
	}
	for d := range apianahtar.HerZaman {
		satirlar = append(satirlar, d+"  (her zaman)")
	}
	sort.Strings(satirlar)
	got := strings.Join(satirlar, "\n") + "\n"
	const altin = "testdata/api_anahtar_uclari.golden"
	if os.Getenv("GOSP_ALTIN_GUNCELLE") == "1" {
		if err := os.WriteFile(altin, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(altin)
	if err != nil {
		t.Fatalf("altın dosya okunamadı (GOSP_ALTIN_GUNCELLE=1 ile üretin): %v", err)
	}
	if string(want) != got {
		t.Fatalf("API anahtarına AÇIK uç listesi değişti. Bilinçliyse GOSP_ALTIN_GUNCELLE=1 ile altın dosyayı güncelleyin ve diff'i inceletin.\n--- altın\n%s\n--- şimdi\n%s", want, got)
	}
}
