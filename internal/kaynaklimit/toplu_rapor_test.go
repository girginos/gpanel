package kaynaklimit

import "testing"

func TestKotaRaporuAyristir(t *testing.T) {
	cikti := `User quota on / (/dev/sda1)
                               Blocks
User ID          Used       Soft       Hard    Warn/Grace
---------- --------------------------------------------------
root          1234567          0          0     00 [--------]
c_ornek_com     20480    4980736    5242880     00 [--------]
c_b              1024          0          0     00 [--------]
c_kotu          abc            0          0
www-data           12          0          0     00 [--------]
`
	m := kotaRaporuAyristir(cikti)
	if len(m) != 2 {
		t.Fatalf("yalnız iki c_* satırı beklenir: %v", m)
	}
	if m["c_ornek_com"].KullanilanKB != 20480 || m["c_ornek_com"].HardKB != 5242880 {
		t.Errorf("c_ornek_com: %+v", m["c_ornek_com"])
	}
	if _, ok := m["root"]; ok {
		t.Error("root alınmamalı")
	}
}

func TestEfektifDiskMB(t *testing.T) {
	if got := EfektifDiskMB(0, false, 0); got != varsayilanDiskMB {
		t.Errorf("plan yok → varsayılan: %d", got)
	}
	if got := EfektifDiskMB(0, true, 0); got != 0 {
		t.Errorf("plan açıkça sınırsız → 0: %d", got)
	}
	if got := EfektifDiskMB(700, true, 100); got != 700 {
		t.Errorf("override planı ezer: %d", got)
	}
}
