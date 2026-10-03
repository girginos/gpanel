package efektiflimit

// kaynak_tarama_test.go — "efektif limiti okuyan HER yer tek fonksiyondan" kuralının
// kalıcı bekçisi. Yeni bir okuyucu planın limit sütununu doğrudan okursa (üst yazımı
// atlayıp bayiye havuz deliği / yanlış cgroup değeri bırakarak) bu test düşer.
//
// İki kural (test dışı .go dosyaları, internal/ + cmd/):
//  1. "p.<limit_sütunu>" (domains d ⟕ service_plans p kalıbı) HİÇBİR yerde geçmez —
//     okuyucu efektiflimit.Oku / Join+Ifade kullanır.
//  2. service_plans tablosunu limit sütunuyla birlikte anan dosya yalnız beyaz
//     listededir (plan CRUD, yeni hosting/yeni plan değeri, bayi paketi, dönüşüm).

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var beyazListe = map[string]string{
	"internal/plans/plans.go":           "plan CRUD + tohum (planın kendisi)",
	"internal/plans/kapsam.go":          "plan kapsamı / devir (planın kendisi)",
	"internal/plans/donustur.go":        "eski kopya → üst yazım dönüşümü (plan satırlarını karşılaştırır)",
	"internal/domains/handlers.go":      "Create: YENİ hostingin (üst yazımı yok) plan değeri + nginx/php tohumu",
	"internal/domains/handlers_plan.go": "yeniPlanEfektifKota: hosting yoksa yalnız plan değeri",
	"internal/reseller/plan.go":         "bayi paketi: kökün en küçük global planı (hosting değil)",
	"internal/kota/kota.go":             "customers.plan_id (hosting değil, müşteri planı)",
}

func TestKaynakTarama_EfektifLimitTekKaynak(t *testing.T) {
	var sut []string
	for _, a := range Alanlar {
		sut = append(sut, regexp.QuoteMeta(a.Sutun))
	}
	alt := strings.Join(sut, "|")
	pAlias := regexp.MustCompile("\\bp\\.`?(" + alt + ")\\b")
	sutunVar := regexp.MustCompile("\\b(" + alt + ")\\b")
	dizgeRe := regexp.MustCompile("`[^`]*`|\"(?:[^\"\\\\\n]|\\\\.)*\"")
	spRe := regexp.MustCompile(`(?i)\b(FROM|JOIN)\s+service_plans\b`)
	kok := "../.."
	taranan := 0
	for _, dizin := range []string{"internal", "cmd"} {
		_ = filepath.Walk(filepath.Join(kok, dizin), func(yol string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() || !strings.HasSuffix(yol, ".go") || strings.HasSuffix(yol, "_test.go") {
				return nil
			}
			goreli, _ := filepath.Rel(kok, yol)
			goreli = filepath.ToSlash(goreli)
			if strings.HasPrefix(goreli, "internal/efektiflimit/") {
				return nil
			}
			b, err := os.ReadFile(yol)
			if err != nil {
				return nil
			}
			taranan++
			icerik := string(b)
			// Kural 1: dosya service_plans'ı "p" takma adıyla birleştiriyorsa p.<limit> yasak.
			if strings.Contains(icerik, "service_plans p") {
				for i, satir := range strings.Split(icerik, "\n") {
					if s := strings.TrimSpace(satir); strings.HasPrefix(s, "//") {
						continue
					}
					if pAlias.MatchString(satir) {
						t.Errorf("%s:%d planın limit sütununu doğrudan okuyor (efektiflimit.Oku / Join+Ifade kullanın): %s",
							goreli, i+1, strings.TrimSpace(satir))
					}
				}
			}
			// Kural 2: service_plans'tan limit sütunu okuyan SQL dizgesi yalnız beyaz listede.
			if _, ok := beyazListe[goreli]; !ok {
				for _, lit := range dizgeRe.FindAllString(icerik, -1) {
					if spRe.MatchString(lit) && sutunVar.MatchString(lit) {
						t.Errorf("%s: service_plans'tan limit sütunu okuyan beyaz liste dışı SQL (efektiflimit kullanın ya da gerekçeyle beyaz listeye ekleyin): %.160s",
							goreli, strings.Join(strings.Fields(lit), " "))
					}
				}
			}
			return nil
		})
	}
	if taranan < 100 {
		t.Fatalf("yalnız %d dosya tarandı — kök yolu yanlış?", taranan)
	}
}
