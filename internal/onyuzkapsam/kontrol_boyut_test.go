package onyuzkapsam

// kontrol_boyut_test.go — 2026-10-05: müşteri panelinde anahtar (toggle) ve açılır kutu
// (select) aşırı büyüktü (Erişim Kısıtlama: h-11 w-[76px] anahtar, text-sm font-semibold
// durum yazısı; Tarayıcı önbelleği: tam genişlik, iri select). Kompakt boyutlar ortak
// bileşenlerde kilitlenir; sayfalar elle anahtar çizmek yerine <Toggle> kullanır.

import (
	"strings"
	"testing"
)

func TestToggleKompaktBoyut(t *testing.T) {
	s := oku(t, "components/ui/Toggle.tsx")
	for _, w := range []string{"'h-5 w-9'", "'h-4 w-7'", "'size-4'", "'size-3'", `role="switch"`, "aria-checked={acik}", "focus-visible:ring-2", "text-xs font-medium"} {
		if !strings.Contains(s, w) {
			t.Errorf("Toggle.tsx: %q yok", w)
		}
	}
}

func TestSelectKompaktBoyut(t *testing.T) {
	s := oku(t, "components/ui/Select.tsx")
	if !strings.Contains(s, "h-9 py-1 pr-9 text-sm") {
		t.Error("Select: kompakt h-9 text-sm sınıfları yok")
	}
}

func TestSayfalarOrtakToggleKullanir(t *testing.T) {
	for _, y := range []string{"pages/DomainErisimPage.tsx", "pages/DomainWebSunucuPage.tsx", "pages/DomainLaravelPage.tsx", "pages/DomainPHPPage.tsx", "pages/SiteTasimaPage.tsx"} {
		s := oku(t, y)
		if !strings.Contains(s, "<Toggle ") {
			t.Errorf("%s: <Toggle> kullanılmıyor", y)
		}
		if strings.Contains(s, `role="switch"`) {
			t.Errorf("%s: elle çizilmiş role=switch kaldı", y)
		}
		if strings.Contains(s, "min-h-[44px]") || strings.Contains(s, "h-11 w-[76px]") {
			t.Errorf("%s: devasa kontrol sınıfı kaldı", y)
		}
	}
}

func TestErisimSayfasiKompakt(t *testing.T) {
	s := oku(t, "pages/DomainErisimPage.tsx")
	for _, w := range []string{"max-w-4xl", `text-lg font-semibold`, `boyut="sm" ikon={I.disket}`, "durumEtiketi"} {
		if !strings.Contains(s, w) {
			t.Errorf("DomainErisimPage: %q yok", w)
		}
	}
}

func TestSaklamaSuresiYanYana(t *testing.T) {
	s := oku(t, "pages/DomainWebSunucuPage.tsx")
	if strings.Count(s, "classNames={{ root: SAKLAMA_KOK }}") != 2 || !strings.Contains(s, "sm:flex-row sm:items-center") {
		t.Error("Saklama süresi kompakt düzeni yok")
	}
}
