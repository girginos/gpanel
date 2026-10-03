package kaynaklimit

// toplu_rapor.go — pano örnekleyicisi (internal/pano) için TOPLU kota okuma.
// KotaDurum her çağrıda tüm raporu koşup tek satırı süzüyor (kiracı başına iki
// xfs_quota); örnekleyici tüm kiracıları TEK `report -u -N -b` ile okur.

import (
	"bufio"
	"os/exec"
	"strconv"
	"strings"
)

// KotaKullanim — bir kiracının XFS kota kullanımı (blok metriği KB).
type KotaKullanim struct {
	KullanilanKB int64
	HardKB       int64
}

// KotaRaporuTumu — tüm c_* kiracılarının disk kullanımı. ok=false: kota
// accounting kapalı ya da okunamadı (çağıran disk değerini GÜNCELLEMEMELİ —
// sıfır yazmak "boş disk" yalanı olur).
func KotaRaporuTumu() (map[string]KotaKullanim, bool) {
	if acc, _ := mountKotaAktif(); !acc {
		return nil, false
	}
	out, err := exec.Command("xfs_quota", "-x", "-c", "report -u -N -b", kotaMount).Output()
	if err != nil {
		return nil, false
	}
	return kotaRaporuAyristir(string(out)), true
}

// kotaRaporuAyristir — `report -u -N -b` çıktısı: "User Used Soft Hard [Warn/Grace]".
// Yalnız reKotaSK'ya uyan (c_*) satırlar alınır; root/sistem kullanıcıları atlanır.
func kotaRaporuAyristir(cikti string) map[string]KotaKullanim {
	m := map[string]KotaKullanim{}
	sc := bufio.NewScanner(strings.NewReader(cikti))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 4 || !reKotaSK.MatchString(f[0]) {
			continue
		}
		used, e1 := strconv.ParseInt(f[1], 10, 64)
		hard, e2 := strconv.ParseInt(f[3], 10, 64)
		if e1 != nil || e2 != nil || used < 0 {
			continue
		}
		m[f[0]] = KotaKullanim{KullanilanKB: used, HardKB: hard}
	}
	return m
}

// EfektifDiskMB — domainin efektif disk kotası (MB; 0 = sınırsız): domain
// override > plan değeri > (plan yoksa) varsayılan. Tek kaynak efektifKota.
func EfektifDiskMB(diskOverride int, planVar bool, planDisk int) int {
	d, _ := efektifKota(diskOverride, 0, planVar, planDisk, 0)
	return d
}
