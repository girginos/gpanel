package portyonetim

// Panel dış SSL portu değişirken firewall'da yeni portu aç, eski portu kapat.
// Backend hostuyg paketiyle SEMANTİK aynı (girginos_fw tercihli, filter/input
// yedek). Circular import olmasın diye burada mikro-kopyalanmıştır; ortak
// bir internal/fw paketine taşımak bir sonraki refactor'ün konusu.

import (
	"fmt"
	"os/exec"
	"strings"
)

type fwHedef struct{ Aile, Tablo, Zincir string }

func fwAktifHedef() *fwHedef {
	if out, err := exec.Command("nft", "list", "chain", "inet", "girginos_fw", "input").Output(); err == nil && len(out) > 0 {
		return &fwHedef{"inet", "girginos_fw", "input"}
	}
	if out, err := exec.Command("nft", "list", "chain", "inet", "filter", "input").Output(); err == nil && len(out) > 0 {
		return &fwHedef{"inet", "filter", "input"}
	}
	return nil
}

// FwPortAc — dış TCP portunu aç (idempotent).
func FwPortAc(port int) error {
	h := fwAktifHedef()
	if h == nil {
		return nil // nft yok — sessiz geç
	}
	comment := fmt.Sprintf("gpanel-panel port %d", port)
	if out, err := exec.Command("nft", "list", "chain", h.Aile, h.Tablo, h.Zincir).Output(); err == nil {
		if strings.Contains(string(out), fmt.Sprintf(`comment "%s"`, comment)) {
			return nil
		}
	}
	kural := fmt.Sprintf(`add rule %s %s %s tcp dport %d accept comment "%s"`,
		h.Aile, h.Tablo, h.Zincir, port, comment)
	cmd := exec.Command("nft", "-f", "-")
	cmd.Stdin = strings.NewReader(kural + "\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("nft add: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// FwPortKapat — handle bulup sil (idempotent).
func FwPortKapat(port int) error {
	h := fwAktifHedef()
	if h == nil {
		return nil
	}
	out, err := exec.Command("nft", "-a", "list", "chain", h.Aile, h.Tablo, h.Zincir).Output()
	if err != nil {
		return nil
	}
	aramaStr := fmt.Sprintf(`comment "gpanel-panel port %d"`, port)
	for _, l := range strings.Split(string(out), "\n") {
		if !strings.Contains(l, aramaStr) {
			continue
		}
		if i := strings.Index(l, "handle "); i >= 0 {
			handle := strings.Fields(l[i+7:])[0]
			_, _ = exec.Command("nft", "delete", "rule", h.Aile, h.Tablo, h.Zincir,
				"handle", handle).CombinedOutput()
		}
	}
	return nil
}
