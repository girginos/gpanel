package avayar

import "testing"

func TestKapasiteVeEtkinDegerler(t *testing.T) {
	k := SunucuKapasitesi()
	t.Logf("olculen: %d cekirdek, %d MB RAM", k.CPUCekirdek, k.RAMToplamMB)
	t.Logf("oneri  : CPUQuota=%d%%  MemoryMax=%dM  isParcacigi=%d",
		k.OneriCPUYuzde, k.OneriRAMMb, k.OneriIsParcacigi)

	if k.CPUCekirdek < 1 {
		t.Fatal("cekirdek sayisi olculemedi")
	}
	if k.RAMToplamMB < 1 {
		t.Fatal("RAM olculemedi — /proc/meminfo okunamiyor")
	}
	// 🔴 Oneri, sunucuyu SOMURMEMELI: toplam CPU'nun yarisini gecmemeli.
	if k.OneriCPUYuzde > k.CPUCekirdek*100/2 {
		t.Errorf("oneri CPU cok yuksek: %d%% (toplam %d%%)", k.OneriCPUYuzde, k.CPUCekirdek*100)
	}
	if k.OneriRAMMb > k.RAMToplamMB/4 && k.RAMToplamMB > 1024 {
		t.Errorf("oneri RAM cok yuksek: %dM (toplam %dM)", k.OneriRAMMb, k.RAMToplamMB)
	}
}

func TestOtomatikDegerlerCozulur(t *testing.T) {
	k := SunucuKapasitesi()
	// Hepsi 0 = otomatik
	c, r, i := Ayarlar{}.Etkin(k)
	if c != k.OneriCPUYuzde || r != k.OneriRAMMb || i != k.OneriIsParcacigi {
		t.Errorf("otomatik degerler oneriye esitlenmedi: %d/%d/%d vs %d/%d/%d",
			c, r, i, k.OneriCPUYuzde, k.OneriRAMMb, k.OneriIsParcacigi)
	}
	// Elle verilen deger KORUNMALI (otomatik ezmemeli)
	c2, r2, i2 := Ayarlar{CPUYuzde: 150, RAMMb: 700, IsParcacigi: 3}.Etkin(k)
	if c2 != 150 || r2 != 700 || i2 != 3 {
		t.Errorf("elle verilen deger EZILDI: %d/%d/%d", c2, r2, i2)
	}
}

func TestKapsamVarsayilaniHost(t *testing.T) {
	// 🔴 Varsayilan HOST olmali. Tum sunucuyu taramak varsayilan olursa
	// /var/lib/mysql icinde dolasip disk G/C yakar.
	kok := Ayarlar{Kapsam: "host"}.TaramaKokleri()
	if len(kok) != 1 || kok[0] != "/home" {
		t.Errorf("host kapsami /home olmali, bulunan: %v", kok)
	}
	kok = Ayarlar{Kapsam: "sunucu"}.TaramaKokleri()
	if len(kok) != 1 || kok[0] != "/" {
		t.Errorf("sunucu kapsami / olmali, bulunan: %v", kok)
	}
}
