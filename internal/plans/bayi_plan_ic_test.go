package plans

import (
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

func TestHavuzPayi_Kural(t *testing.T) {
	for _, c := range []struct {
		ad              string
		sablon          int
		havuz           int64
		hak             int
		fazla           bool
		yedek, beklenen int
	}{
		{"havuz sinirsiz, sablon var", 10240, 0, 5, false, 1024, 10240},
		{"havuz sinirsiz, sablon sinirsiz -> tohum", 0, 0, 5, false, 1024, 1024},
		{"sablon paydan buyuk -> pay", 10240, 10000, 5, false, 1024, 1999},
		{"sablon paydan kucuk -> sablon", 1000, 10000, 5, false, 1024, 1000},
		{"sablon sinirsiz -> pay", 0, 10000, 5, false, 1024, 1999},
		{"hak sinirsiz -> havuz-1", 50000, 10000, 0, false, 1024, 9999},
		{"tek hak -> havuz-1 (sinira ulasmak dolu)", 50000, 10000, 1, false, 1024, 9999},
		{"fazla satis -> sablon", 10240, 10000, 5, true, 1024, 10240},
		{"fazla satis, sablon sinirsiz -> pay", 0, 10000, 5, true, 1024, 1999},
		{"cok kucuk havuz -> en az 1", 100, 3, 5, false, 1024, 1},
	} {
		if g := havuzPayi(c.sablon, c.havuz, c.hak, c.fazla, c.yedek); g != c.beklenen {
			t.Errorf("%s: havuzPayi=%d, beklenen %d", c.ad, g, c.beklenen)
		}
		// Hakkın tamamı bu planla açılınca taahhüt havuzun ALTINDA kalır (fazla satış hariç).
		if !c.fazla && c.havuz > 1 && c.hak > 0 {
			g := havuzPayi(c.sablon, c.havuz, c.hak, c.fazla, c.yedek)
			if int64(g)*int64(c.hak) >= c.havuz && g > 1 {
				t.Errorf("%s: %d hak × %d MB = havuz %d'e ulaşıyor", c.ad, c.hak, g, c.havuz)
			}
		}
	}
}

func TestBayiVarsayilanDegerler_SablonVeTavan(t *testing.T) {
	sablon := Plan{Ad: "Standart", DiskKotaMB: 10240, TrafikKotaMB: 51200, CPUYuzde: 150, RAMMB: 2048, PHPSurum: "8.2",
		MaxDomain: 5, MaxDB: 10, MaxFTP: 10, MaxEmail: 0, SaatlikMailLimiti: 500, MailKutuKotaMB: 0,
		IOReadMBps: 40, DBMaxQuerySeconds: 30, MaxProcess: 999, NginxEkDirektifler: "# KOK", WafEnabled: true, WafMode: "detect"}
	p := bayiVarsayilanDegerler(sablon, bayiHavuz{MaxDomain: 5, MaxDiskMB: 10000, MaxTrafikMB: 50000,
		MailMaxEmail: 10, MailSaatlik: 100, MailKutuMB: 0})
	if p.DiskKotaMB != 1999 || p.TrafikKotaMB != 9999 {
		t.Errorf("disk/trafik payı: %d/%d", p.DiskKotaMB, p.TrafikKotaMB)
	}
	if p.CPUYuzde != 150 || p.RAMMB != 2048 || p.PHPSurum != "8.2" || p.MaxDB != 10 || p.MaxFTP != 10 || p.MaxDomain != 5 {
		t.Errorf("özet alanlar şablondan alınmadı: %+v", p)
	}
	if p.MaxEmail != 10 || p.SaatlikMailLimiti != 100 || p.MailKutuKotaMB != 0 {
		t.Errorf("posta tavanı: %d/%d/%d", p.MaxEmail, p.SaatlikMailLimiti, p.MailKutuKotaMB)
	}
	// Kök iç ayarları şablondan ALINMAZ (GL-06).
	v := panelVarsayilanPlan()
	if p.IOReadMBps != 0 || p.DBMaxQuerySeconds != 0 || p.MaxProcess != v.MaxProcess || p.NginxEkDirektifler != "" || p.WafEnabled || p.WafMode != "on" {
		t.Errorf("kök iç ayarı şablondan sızdı: %+v", p)
	}
	// Şablon boş (tohum yok) → panel varsayılanları, disk/trafik tohum.
	b := bayiVarsayilanDegerler(Plan{}, bayiHavuz{})
	if b.DiskKotaMB <= 0 || b.TrafikKotaMB <= 0 || b.CPUYuzde <= 0 || b.RAMMB <= 0 || b.PHPSurum == "" {
		t.Errorf("boş şablon: %+v", b)
	}
}

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
}
