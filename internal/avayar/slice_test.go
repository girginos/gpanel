package avayar

import (
	"os"
	"strings"
	"testing"
)

// 🔴 URETIM YOLU TESTI: "slice dosyasini yazdim" ile "cekirdek limiti
// uyguluyor" AYRI SEYLER. Bu test dosyayi yazar, systemd'ye yukletir ve
// systemd'nin GERI OKUDUGU degeri dogrular. Dosya iceriğine bakan bir test
// mekanizmayi hic kanitlamaz.
func TestSliceGercektenUygulanir(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root gerekli — slice yazilamaz")
	}
	a := Ayarlar{Kapsam: "host", IOAgirlik: 50, CPUYuzde: 150, RAMMb: 400, EsikKritik: 100}
	if err := LimitleriUygula(a); err != nil {
		t.Fatalf("limit uygulanamadi: %v", err)
	}
	d := LimitDurumu()
	t.Logf("cekirdek: %v", d)

	// systemd CPUQuota'yi mikrosaniye/saniye olarak doner: 150% -> 1500000
	if q := d["CPUQuotaPerSecUSec"]; q == "OLCULEMEDI" || q == "" {
		t.Errorf("CPUQuota OKUNAMADI: %q", q)
	} else if !strings.Contains(q, "1.5") && !strings.Contains(q, "1500") {
		t.Errorf("CPUQuota beklenen 150%% degil: %q", q)
	}
	if m := d["MemoryMax"]; m != "419430400" { // 400M
		t.Errorf("MemoryMax 400M (419430400) olmali, bulunan: %q", m)
	}
	if w := d["IOWeight"]; w != "50" {
		t.Errorf("IOWeight 50 olmali, bulunan: %q", w)
	}
}
