// Package ipyonetim — panel yönetici arayüzü üzerinden sunucuya ek IP
// adresleri ekleme/silme.
//
// TASARIM İLKELERİ (multi-verify sonrası revize):
//   1. KULLANICI IP'LERİNE DOKUNMA. Panel silmede yalnız kendi ekledikleri
//      (label prefix "panel-") silinebilir. ISP kurulumundan gelen primary IP
//      her zaman korunur.
//   2. PANEL'İN GERÇEKTEN BAĞLI OLDUĞU IP silinemez. `ss -tlnp` ile canlı
//      listen socket set okunur — "0.0.0.0" bind ise TÜM IP'ler bind sayılır
//      → sadece panel-eklenmemiş primary silinemez. Gerçek bind kontrolü
//      lockout riskini kapatır.
//   3. Kalıcılık: reboot sonrası oneshot systemd unit DB'yi okur ve IP'leri
//      geri ekler. `ip addr add` runtime'da, `systemd unit` boot'ta.
//   4. Duplicate check: eklemeden ÖNCE gerçek `ip -o addr show` çıktısı
//      taranır. Var olan IP tekrar eklenmez.
//   5. Label formatı: DOĞRUDAN `panel-XXXX` (4 hex, 10 char). Iface prefix
//      YOK — 15-char kernel sınırı için long-iface truncation bug'ı
//      (multi-verify #1) engellendi.
//
// v4-only. Discovery `-4` ile filtrelidir; IPv6 desteği yoktur → endpoint
// tarafında v6 girdisi reddedilir.

package ipyonetim

import (
	"bufio"
	"database/sql"
	"net"
	"os/exec"
	"strconv"
	"strings"
)

// LabelPrefix — panel eklediği tüm IP'ler bu prefix'li etiketle işaretlidir.
// Silme yolunun "kullanıcı IP'sine dokunma" garantisi buna dayanır.
// Not: DOĞRUDAN prefix — iface öneki yok.
const LabelPrefix = "panel-"

// KurulumUnitYolu — reboot sonrası restore için systemd unit.
const KurulumUnitYolu = "/etc/systemd/system/girginospanel-ip.service"
const KurulumScriptYolu = "/usr/local/sbin/girginospanel-ip-apply.sh"

type SunucuIP struct {
	IP          string `json:"ip"`
	Iface       string `json:"iface"`
	CIDR        int    `json:"cidr"`
	Label       string `json:"label"`
	PanelIP     bool   `json:"panel_ip"`
	Silinebilir bool   `json:"silinebilir"`
	PrimaryMi   bool   `json:"primary_mi"`
}

// SunucuIPler — sunucudaki TÜM IP'leri listeler.
func SunucuIPler() []SunucuIP {
	out, err := exec.Command("ip", "-o", "-4", "addr", "show", "scope", "global").Output()
	if err != nil {
		return nil
	}
	var liste []SunucuIP
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	iceilkGorulen := map[string]bool{}
	for sc.Scan() {
		parcalar := strings.Fields(sc.Text())
		if len(parcalar) < 4 {
			continue
		}
		iface := parcalar[1]
		var ipCIDR, label string
		for i := 2; i < len(parcalar); i++ {
			if parcalar[i] == "inet" && i+1 < len(parcalar) {
				ipCIDR = parcalar[i+1]
			}
			if parcalar[i] == "label" && i+1 < len(parcalar) {
				label = parcalar[i+1]
			}
		}
		if ipCIDR == "" {
			continue
		}
		ipStr, cidrStr, _ := strings.Cut(ipCIDR, "/")
		if net.ParseIP(ipStr) == nil {
			continue
		}
		cidr := 32
		if v, err := strconv.Atoi(cidrStr); err == nil && v > 0 && v <= 32 {
			cidr = v
		}

		// Panel-etiketi tespit: hem YENİ format "panel-XXXX" hem ESKİ
		// "iface:panel-XXXX" desteklenir. Eski format iface prefix'i olan
		// v1 kurulumlar için geriye dönük uyumluluk.
		labelSonrasi := label
		if i := strings.Index(label, ":"); i >= 0 {
			labelSonrasi = label[i+1:]
		}
		panelIP := strings.HasPrefix(labelSonrasi, LabelPrefix) ||
			strings.HasPrefix(label, LabelPrefix)

		primary := !iceilkGorulen[iface]
		iceilkGorulen[iface] = true

		liste = append(liste, SunucuIP{
			IP:          ipStr,
			Iface:       iface,
			CIDR:        cidr,
			Label:       label,
			PanelIP:     panelIP,
			PrimaryMi:   primary,
			Silinebilir: panelIP && !primary,
		})
	}
	return liste
}

// PanelIPleri — sunucudaki panel-etiketli IP'ler.
// dbSet parity: DB'de olmayanları "yetim" bayrağı ile kesmez — Silinebilir
// bayrağı zaten kesif.go içinde belirlendi. Boş liste normal (henüz
// eklenmemişse).
func PanelIPleri(db *sql.DB) ([]SunucuIP, error) {
	tumu := SunucuIPler()
	var out []SunucuIP
	for _, ip := range tumu {
		if ip.PanelIP {
			out = append(out, ip)
		}
	}
	return out, nil
}

// IPHostuncaVar — belirli bir IP zaten sunucuda mevcut mu?
func IPHostuncaVar(ip string) bool {
	for _, s := range SunucuIPler() {
		if s.IP == ip {
			return true
		}
	}
	return false
}

// PanelinBagliOlduguIPler — panel process'inin gerçekten dinlediği IP set'i.
// `ss -tlnp` çıktısını parse ederek girginospanel-server binary'sine ait
// listen socket'leri bulur. "0.0.0.0" veya "::" bind ise tüm IP'ler bağlı
// sayılır — bu durumda özel bir bayrak dönülür.
//
// Dönüş:
//   - kumeSet["0.0.0.0"] == true → panel wildcard bind → hiçbir secondary
//     IP silinemez (silmek → o IP üzerinden panel istekleri düşer)
//   - kumeSet["148.251.169.181"] == true → panel yalnız bu IP'yi dinliyor
func PanelinBagliOlduguIPler() map[string]bool {
	set := map[string]bool{}
	// `ss -tlnp` root gerektirir; panel root altında çalışıyor (unit).
	out, err := exec.Command("ss", "-tlnpH").Output()
	if err != nil {
		// ss YOKSA veya hata verirse FAIL-CLOSE: wildcard varsay -> silmeyi
		// tumden koru. Bilinmeyen durumda guvenli yon KORUMA.
		set["0.0.0.0"] = true
		return set
	}
	// Örnek satır (panel):
	//   LISTEN 0 4096 127.0.0.1:8080 0.0.0.0:* users:(("girginospanel-server",pid=1234,fd=6))
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		line := sc.Text()
		if !strings.Contains(line, "girginospanel") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		// f[3] = "127.0.0.1:8080" veya "0.0.0.0:8080" veya "*:8080"
		local := f[3]
		// port'u kes, IP kısmını al
		if i := strings.LastIndex(local, ":"); i > 0 {
			ipPart := local[:i]
			// IPv6 "[::1]" → "::1"
			ipPart = strings.TrimPrefix(strings.TrimSuffix(ipPart, "]"), "[")
			if ipPart == "*" {
				ipPart = "0.0.0.0"
			}
			set[ipPart] = true
		}
	}
	return set
}

// PanelBuIPUzerindeMi — bu IP'yi silmek panel'i lockout eder mi?
//
// Karar mantığı:
//   - Panel wildcard bind ediyorsa (0.0.0.0 veya ::) → HER sunucu IP'si
//     üzerinden panel istekleri gelebilir. Panel-eklenmiş secondary'yi
//     silmek DNS'in bu IP'ye çözdüğü kullanıcılar için lockout olur.
//     Ancak silinen IP zaten panel-etiketli olduğu için — DNS bu IP'ye
//     çözecek şekilde ayarlandıysa admin bilerek yapmıştır. Bu yüzden
//     wildcard case'de yalnız "bu IP primary mi" kontrolü + explicit
//     onaylı silme (UI zaten confirm dialog gösterir).
//   - Panel spesifik IP dinliyorsa → dinlenen IP'nin silinmesi HEMEN
//     lockout üretir. Ne olursa olsun engellenir.
func PanelBuIPUzerindeMi(ip string) bool {
	bagli := PanelinBagliOlduguIPler()
	// Panel wildcard bind mı?
	wildcard := bagli["0.0.0.0"] || bagli["::"]
	if wildcard {
		// Wildcard: sadece primary'yi koru. Panel-etiketli secondary silinmesi
		// admin sorumluluğunda (UI zaten confirm dialog gösterir).
		for _, s := range SunucuIPler() {
			if s.IP == ip {
				return s.PrimaryMi
			}
		}
		return false
	}
	// Spesifik bind: bu IP dinleniyorsa engelle
	return bagli[ip]
}
