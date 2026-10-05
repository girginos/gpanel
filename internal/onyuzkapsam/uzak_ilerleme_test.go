package onyuzkapsam

// uzak_ilerleme_test.go — Drive gönderimi canlı ilerleme arayüzü: çubuk erişilebilirliği,
// yoklama kalıbı (görünürlük, temizlik, çakışma yok), "hesaplanıyor" dili, TR/EN karşılıkları,
// satırlarda N+1 isteğin olmaması.

import (
	"regexp"
	"strings"
	"testing"
)

func TestUzakIlerleme_YoklamaKalibi(t *testing.T) {
	lib := oku(t, "lib/uzakIlerleme.ts")
	for _, gerek := range []string{
		"/admin/backups/uzak-kopya/ilerleme",
		"document.hidden",                   // sekme gizliyken istek yok
		"visibilitychange",                  // görününce hemen yenile
		"removeEventListener",               // temizlik
		"clearTimeout",                      // temizlik
		"if (iptal || gidiyor) return",      // üst üste binme yok
		"hizliMs = 2000", "yavasMs = 15000", // aktifken 2 sn, boştayken 15 sn
		"if (!etkin)", // etkin değilse hiç istek yok
	} {
		if !strings.Contains(lib, gerek) {
			t.Errorf("uzakIlerleme.ts beklenen öğeyi içermiyor: %q", gerek)
		}
	}
	if strings.Contains(lib, "setInterval") {
		t.Error("yoklama setInterval ile değil zincirleme setTimeout ile olmalı (istekler üst üste binmesin)")
	}
}

func TestUzakIlerleme_CubukErisilebilirVeDilMetinleri(t *testing.T) {
	bil := oku(t, "components/UzakIlerleme.tsx")
	lib := oku(t, "lib/uzakIlerleme.ts")
	for _, gerek := range []string{
		`role="progressbar"`, "aria-valuenow", "aria-valuemin={0}", "aria-valuemax={100}", "aria-valuetext",
		"dark:bg-dark-600", "dark:border-sky-500/30", // koyu tema
		"motion-reduce:transition-none",       // hareket azaltma
		"break-words", "min-w-0", "flex-wrap", // 375px taşma yok
		"süre hesaplanıyor…", "Hata alan işler için", "sonIslerAc",
	} {
		if !strings.Contains(bil+lib, gerek) {
			t.Errorf("ilerleme arayüzü beklenen öğeyi içermiyor: %q", gerek)
		}
	}
	// "yaklaşık" dili: kesin süre vaadi yok
	if !strings.Contains(lib, "yaklaşık {0} dk kalan") || !strings.Contains(lib, "1 dakikadan az kalan") {
		t.Error("kalan süre 'yaklaşık' diliyle verilmeli")
	}
	// Her kullanıcı metninin EN karşılığı var.
	re := regexp.MustCompile(`cevir(?:T)?\(\s*(?:cevir\()?'([^']+)'`)
	for _, m := range re.FindAllStringSubmatch(bil+lib, -1) {
		if !strings.Contains(lib, "'"+m[1]+"':") {
			t.Errorf("EN karşılığı yok: %q", m[1])
		}
	}
	// Teknik jargon yok.
	for _, jargon := range []*regexp.Regexp{regexp.MustCompile(`(?i)\bage\b`), regexp.MustCompile(`(?i)manifest`), regexp.MustCompile(`(?i)sha-?256`)} {
		for _, satir := range strings.Split(bil+lib, "\n") {
			s := strings.TrimSpace(satir)
			if !strings.HasPrefix(s, "//") && jargon.MatchString(s) {
				t.Errorf("kullanıcı metninde jargon (%s): %s", jargon, s)
			}
		}
	}
}

func TestUzakIlerleme_KartVeSatirEntegrasyonu(t *testing.T) {
	kart := oku(t, "components/YedekUzakKopya.tsx")
	for _, gerek := range []string{
		"<UzakIlerlemePaneli", "useUzakIlerleme(", "setHizlandir(n => n + 1)", // gönderince yoklama hemen hızlanır
		"useIlerlemeBitisi(",       // bitince özet/tablo yenilenir
		"!!ilerleme?.aktif_mi",     // en eski aktif iş ilk sayfada olmayabilir
		"{0} yedek sıraya alındı.", // mevcut bildirim KALDI
	} {
		if !strings.Contains(kart, gerek) {
			t.Errorf("YedekUzakKopya.tsx beklenen öğeyi içermiyor: %q", gerek)
		}
	}
	if strings.Index(kart, "<UzakIlerlemePaneli") > strings.Index(kart, "<UyariKutusu ton=\"tehlike\">{hata}</UyariKutusu>}\n        {basari") {
		t.Error("ilerleme paneli kartın en üstünde (uyarılardan önce) olmalı")
	}
	sayfa := oku(t, "pages/DomainBackupsPage.tsx")
	if n := strings.Count(sayfa, "useUzakIlerleme("); n != 1 {
		t.Errorf("DomainBackupsPage ilerleme için TEK yoklama kullanmalı (N+1 yok): %d", n)
	}
	if strings.Contains(sayfa, "uzak-kopya/ilerleme") {
		t.Error("satır sayfası ilerleme ucunu doğrudan çağırmamalı (tek kancadan)")
	}
	if !strings.Contains(sayfa, "<UzakSatirCubugu") || !strings.Contains(sayfa, "driveIlerleme.aktif.yedek_id === y.id") {
		t.Error("yalnız aktif işin satırında küçük çubuk gösterilmeli")
	}
}
