package hostuyg

// Firewall entegrasyonu — panel'in kendi nft tablosu (girginos_fw) TERCİHLİ,
// aksi halde firewalld > nftables (filter) > iptables > no-op fallback.
//
// FirewallPortAc(port, protokol)   — INPUT için ACCEPT kural ekle
// FirewallPortKapat(port, protokol) — kuralı comment'e göre sil

import (
	"fmt"
	"os/exec"
	"strings"
)

type FwBackend string

const (
	FwGirginos  FwBackend = "girginos_fw" // panel'in kendi nft tablosu
	FwFirewalld FwBackend = "firewalld"
	FwNftables  FwBackend = "nftables"
	FwIptables  FwBackend = "iptables"
	FwYok       FwBackend = "yok"
)

// nftTablo — {"inet","girginos_fw"} veya {"inet","filter"}. Aktif chain'e göre.
type nftHedef struct {
	Aile   string // "inet" | "ip" | "ip6"
	Tablo  string // "girginos_fw" | "filter"
	Zincir string // "input"
}

// nftAktifHedef — nft ile hangi table/chain kullanılacağını bulur.
// 1) inet/girginos_fw/input (panel'in kendi tablosu — TERCİH)
// 2) inet/filter/input       (nftables varsayılan)
func nftAktifHedef() *nftHedef {
	if out, err := exec.Command("nft", "list", "chain", "inet", "girginos_fw", "input").Output(); err == nil && len(out) > 0 {
		return &nftHedef{"inet", "girginos_fw", "input"}
	}
	if out, err := exec.Command("nft", "list", "chain", "inet", "filter", "input").Output(); err == nil && len(out) > 0 {
		return &nftHedef{"inet", "filter", "input"}
	}
	return nil
}

// TespitFw — hangi firewall backend aktif.
func TespitFw() FwBackend {
	// nft varsa önce panel tablosunu ara (girginos_fw)
	if komutVar("nft") {
		if h := nftAktifHedef(); h != nil {
			if h.Tablo == "girginos_fw" {
				return FwGirginos
			}
			return FwNftables
		}
	}
	if komutVar("firewall-cmd") && sistemActive("firewalld") {
		return FwFirewalld
	}
	if komutVar("iptables") {
		return FwIptables
	}
	return FwYok
}

func komutVar(ad string) bool {
	_, err := exec.LookPath(ad)
	return err == nil
}
func sistemActive(unit string) bool {
	out, _ := exec.Command("systemctl", "is-active", unit).Output()
	return strings.TrimSpace(string(out)) == "active"
}

// nftKuralEkle — girginos_fw veya filter tablosuna kural ekle (comment ile).
// nft argv comment içinde boşluk kabul etmiyor (parser identifier bekliyor)
// → `nft -f -` ile stdin'e tam komut (tırnaklı) yazıyoruz.
// Idempotent: aynı comment'li kural zaten varsa atla (çift ekleme yok).
func nftKuralEkle(h *nftHedef, port int, protokol string) error {
	comment := fmt.Sprintf("gpanel-app port %d", port)
	if out, err := exec.Command("nft", "list", "chain", h.Aile, h.Tablo, h.Zincir).Output(); err == nil {
		if strings.Contains(string(out), fmt.Sprintf(`comment "%s"`, comment)) {
			return nil
		}
	}
	kural := fmt.Sprintf(`add rule %s %s %s %s dport %d accept comment "%s"`,
		h.Aile, h.Tablo, h.Zincir, protokol, port, comment)
	cmd := exec.Command("nft", "-f", "-")
	cmd.Stdin = strings.NewReader(kural + "\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("nft add (%s/%s): %s (%w)", h.Tablo, h.Zincir,
			strings.TrimSpace(string(out)), err)
	}
	return nil
}

// nftKuralSil — comment'e göre handle bul + sil (idempotent).
func nftKuralSil(h *nftHedef, port int) error {
	out, err := exec.Command("nft", "-a", "list", "chain", h.Aile, h.Tablo, h.Zincir).Output()
	if err != nil {
		return nil // idempotent
	}
	aramaStr := fmt.Sprintf(`comment "gpanel-app port %d"`, port)
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

// FirewallPortAc — port'u dış erişime aç (INPUT ACCEPT).
// Backend "yok" ise sadece uyarı, hata dönmez (test/dev ortamı).
func FirewallPortAc(port int, protokol string) error {
	switch TespitFw() {
	case FwGirginos, FwNftables:
		h := nftAktifHedef()
		if h == nil {
			return nil
		}
		return nftKuralEkle(h, port, protokol)
	case FwFirewalld:
		cmd := exec.Command("firewall-cmd", "--permanent",
			fmt.Sprintf("--add-port=%d/%s", port, protokol))
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("firewalld --add-port: %s (%w)", strings.TrimSpace(string(out)), err)
		}
		_, _ = exec.Command("firewall-cmd", "--reload").CombinedOutput()
		return nil
	case FwIptables:
		cmd := exec.Command("iptables", "-I", "INPUT",
			"-p", protokol, "--dport", fmt.Sprintf("%d", port),
			"-j", "ACCEPT",
			"-m", "comment", "--comment", fmt.Sprintf("gpanel-app %d", port))
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("iptables -I: %s (%w)", strings.TrimSpace(string(out)), err)
		}
		return nil
	default:
		return nil
	}
}

// FirewallPortKapat — kural sil (best-effort, hata idempotent yumuşak).
func FirewallPortKapat(port int, protokol string) error {
	switch TespitFw() {
	case FwGirginos, FwNftables:
		h := nftAktifHedef()
		if h == nil {
			return nil
		}
		return nftKuralSil(h, port)
	case FwFirewalld:
		_, _ = exec.Command("firewall-cmd", "--permanent",
			fmt.Sprintf("--remove-port=%d/%s", port, protokol)).CombinedOutput()
		_, _ = exec.Command("firewall-cmd", "--reload").CombinedOutput()
		return nil
	case FwIptables:
		_, _ = exec.Command("iptables", "-D", "INPUT",
			"-p", protokol, "--dport", fmt.Sprintf("%d", port),
			"-j", "ACCEPT").CombinedOutput()
		return nil
	default:
		return nil
	}
}
