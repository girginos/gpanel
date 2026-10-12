package plans

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"girginospanel/internal/efektiflimit"
)

// planAlanlari efektiflimit.Alanlar'ın TAMAMINI kapsar (yeni limit alanı eklenince
// kök alanı koruması / değişiklik tespiti sessizce eksik kalmasın).
func TestPlanAlanlari_Eksiksiz(t *testing.T) {
	var p Plan
	m := planAlanlari(&p)
	if len(m) != len(efektiflimit.Alanlar) {
		t.Fatalf("planAlanlari %d alan, efektiflimit.Alanlar %d", len(m), len(efektiflimit.Alanlar))
	}
	goruldu := map[*int]bool{}
	for _, a := range efektiflimit.Alanlar {
		ptr, ok := m[a.Sutun]
		if !ok {
			t.Errorf("planAlanlari %q alanını kapsamıyor", a.Sutun)
			continue
		}
		if goruldu[ptr] {
			t.Errorf("%q başka bir alanla aynı Plan alanına işaret ediyor", a.Sutun)
		}
		goruldu[ptr] = true
	}
}

// Kök alanları = Ozette=false (GL-06 sınıfı); disk/trafik/CPU/RAM/sayılar/posta bayide.
func TestKokAlanlari_TekKaynak(t *testing.T) {
	k := map[string]bool{}
	for _, s := range KokAlanlari() {
		k[s] = true
	}
	for _, s := range []string{"io_read_mbps", "io_write_iops", "db_max_query_seconds", "db_max_queries_per_hour",
		"mysql_max_baglanti", "max_process", "inode_kota", "pm_max_children", "io_agirlik"} {
		if !k[s] {
			t.Errorf("%s kök alanı olmalı", s)
		}
	}
	for _, s := range []string{"disk_kota_mb", "trafik_kota_mb", "cpu_yuzde", "ram_mb", "max_domain", "max_db",
		"max_ftp", "max_email", "saatlik_mail_limiti", "mail_kutu_kota_mb"} {
		if k[s] {
			t.Errorf("%s bayinin düzenleyebileceği alan olmalı", s)
		}
	}
	// kokAlanlariniAl yalnız kök alanlarını taşır.
	hedef := Plan{DiskKotaMB: 5, IOReadMBps: 999, DBMaxQuerySeconds: 7, CPUYuzde: 300}
	kokAlanlariniAl(&hedef, Plan{DiskKotaMB: 1, IOReadMBps: 0, DBMaxQuerySeconds: 30, CPUYuzde: 100, InodeKota: 50000})
	if hedef.IOReadMBps != 0 || hedef.DBMaxQuerySeconds != 30 || hedef.InodeKota != 50000 || hedef.DiskKotaMB != 5 || hedef.CPUYuzde != 300 {
		t.Errorf("kokAlanlariniAl: %+v", hedef)
	}
}

// Eski otomatik planın ad imzası (temizlik yalnız bunu tanır): "<bayi> Varsayılan",
// çakışmada " #<rid>" / " #<rid>-<n>". Bayinin yeniden adlandırdığı plan, başka
// bayinin adı ya da benzer ad imza DEĞİLDİR.
func TestVarsayilanPlanAdi(t *testing.T) {
	if g := varsayilanPlanAdi("bayia", 7, 0); g != "bayia Varsayılan" {
		t.Errorf("ad: %q", g)
	}
	if g := varsayilanPlanAdi("bayia", 7, 1); g != "bayia Varsayılan #7" {
		t.Errorf("çakışma adı: %q", g)
	}
	if g := varsayilanPlanAdi("", 7, 0); g != "bayi-7 Varsayılan" {
		t.Errorf("boş kullanıcı: %q", g)
	}
	uzun := strings.Repeat("ş", 80)
	for d := 0; d < 3; d++ {
		g := varsayilanPlanAdi(uzun, 123456, d)
		if utf8.RuneCountInString(g) > 64 || !strings.Contains(g, "Varsayılan") {
			t.Errorf("uzun ad (%d): %q", d, g)
		}
	}
	for _, c := range []struct {
		ad, kul string
		rid     int64
		imza    bool
	}{
		{"bayia Varsayılan", "bayia", 7, true},
		{"bayia Varsayılan #7", "bayia", 7, true},
		{"bayia Varsayılan #7-2", "bayia", 7, true},
		{"bayia Varsayılan #7-3", "bayia", 7, true},
		{"bayia Varsayılan #7-4", "bayia", 7, false}, // eski kod en çok 4 deneme yapardı
		{"bayia Varsayılan #8", "bayia", 7, false},   // başka bayinin kimliği
		{"bayib Varsayılan", "bayia", 7, false},      // başka bayinin adı
		{"bayia varsayılan", "bayia", 7, false},      // bayi yeniden adlandırmış
		{"bayia Varsayılan Plan", "bayia", 7, false},
		{"Başlangıç", "bayia", 7, false},
	} {
		if g := otomatikAdMi(c.ad, c.kul, c.rid); g != c.imza {
			t.Errorf("otomatikAdMi(%q, %q, %d) = %v, beklenen %v", c.ad, c.kul, c.rid, g, c.imza)
		}
	}
}

// Okuma maskesi (PlanOzet) ile yazma reddi (efektiflimit.IcAlan) AYNI listeyi kullanır:
// özette olan her limit alanı bayiye açık, olmayan her alan iç alandır.
func TestPlanOzet_IcAlanlaAyni(t *testing.T) {
	b, err := json.Marshal(PlanOzet{})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for _, a := range efektiflimit.Alanlar {
		_, ozette := m[a.Sutun]
		if ozette == efektiflimit.IcAlan(a.Sutun) {
			t.Errorf("%s: PlanOzet'te=%v ama IcAlan=%v (okuma maskesi ile yazma reddi kaydı)", a.Sutun, ozette, efektiflimit.IcAlan(a.Sutun))
		}
	}
	if strings.Join(KokAlanlari(), ",") != strings.Join(efektiflimit.IcAlanlar(), ",") {
		t.Errorf("KokAlanlari ≠ efektiflimit.IcAlanlar")
	}
}
