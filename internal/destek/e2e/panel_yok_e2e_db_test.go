package e2e

// panel_yok_e2e_db_test.go — PANEL ÇÖKTÜ simülasyonu (Faz 3): ajan ayrı süreç, panel-bağımlı araçlar
// özel köprüden gider. Köprü yokken panelden bağımsız araçlar çalışır, panel-bağımlı araçlar
// panel_yanit_vermiyor döner, bot cevap yolu çalışır; köprü geri gelince AJAN yeniden
// başlatılmadan toparlanır.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"girginospanel/internal/destek/arac"
	"girginospanel/internal/destek/kopru"
	"girginospanel/internal/destek/merkez"
	"girginospanel/internal/monitor"
)

func TestPanelYokAjanTeshisiSurer(t *testing.T) {
	o := kur(t, false, nil)
	o.bagliBekle()
	o.kosulBekle("merkez ajanı çevrimiçi gördü", 10*time.Second, func() bool { return o.metrikSayi("cevrimici_ajan") == 1 })
	o.portalEslestir(tgA, "musteri_a", "okuma")

	// Ajanın yürütücüsü: köprü istemcisi (ayrı süreç gibi); soket başta YOK.
	dizin, err := os.MkdirTemp("/tmp", "kp")
	if err != nil {
		t.Skip("kısa geçici dizin yok")
	}
	t.Cleanup(func() { _ = os.RemoveAll(dizin) })
	soket := filepath.Join(dizin, "s", "k.sock")
	ist := &kopru.Istemci{Yol: soket, SahipUID: uint32(os.Geteuid()), CagriSiniri: 3 * time.Second}
	o.ajan().YurutucuAyarla(arac.YeniDikisliKopruli(o.db, ist, []string{panelSirri}, arac.Dikisler{
		Yokla: func(u string) monitor.DomainHealth {
			return monitor.DomainHealth{URL: u, DurumKodu: 502, Erisilebilir: true, YanitSuresiMs: 31, Server: "nginx", Sema: "https"}
		},
		Komut: func(_ context.Context, argv ...string) ([]byte, error) {
			if len(argv) > 0 && argv[0] == "systemctl" {
				return []byte("active\n"), nil
			}
			return nil, fmt.Errorf("e2e: kök komutu yok")
		},
		KiraciKomut: func(context.Context, string, ...string) ([]byte, error) {
			return nil, fmt.Errorf("e2e: kiracı komutu yok")
		},
		Dig: func(context.Context, string, string, string) ([]string, error) {
			return nil, fmt.Errorf("e2e: dns yok")
		},
	}))

	sor := func(tur string) string {
		t.Helper()
		o.c.senaryo(func(n int, govde []byte) *merkez.ClaudeYanit {
			if n == 0 {
				return yanit("tool_use", metinBlok("Bakıyorum."),
					aracBlok("toolu_py_1", "servis_durum", `{"servis":"nginx"}`),
					aracBlok("toolu_py_2", "site_yokla", `{"hesap":1}`),
					aracBlok("toolu_py_3", "log_oku", `{"hesap":1,"tur":"nginx_error"}`))
			}
			sonuclar := sonAracSonuclari(govde)
			return yanit("end_turn", metinBlok(tur+"\n"+strings.Join(sonuclar, "\n")))
		})
		o.mesaj(tgA, "musteri_a", "sitem açılmıyor "+tur)
		return o.tg.bekle(t, tgA, tur)
	}

	// 1) Köprü YOK (panel kapalı): bağımsız araçlar çalışır, panel-bağımlı olan panel_yanit_vermiyor.
	r1 := sor("TurBir")
	if !strings.Contains(r1, "panel_yanit_vermiyor") {
		t.Fatalf("panel kapalıyken log_oku panel_yanit_vermiyor dönmeliydi:\n%s", r1)
	}
	if !strings.Contains(r1, "nginx") || !strings.Contains(r1, "active") {
		t.Fatalf("servis_durum panel kapalıyken de çalışmalıydı:\n%s", r1)
	}
	if !strings.Contains(r1, "502") {
		t.Fatalf("site_yokla panel kapalıyken de çalışmalıydı:\n%s", r1)
	}
	if strings.Contains(r1, "upstream timed out") {
		t.Fatal("panel kapalıyken panel günlüğü okunmuş görünüyor")
	}
	if !o.ajan().Bagli() {
		t.Fatal("panel kapalıyken ajan merkezle bağlantısını yitirdi")
	}

	// 2) Köprü GERİ GELDİ: ajan YENİDEN BAŞLATILMADAN toparlanır.
	ctx, kapat := context.WithCancel(context.Background())
	s := &kopru.Sunucu{Router: o.yurutucuRouter(), Yol: soket, IzinliUID: uint32(os.Geteuid())}
	bitti := make(chan error, 1)
	go func() { bitti <- s.Dinle(ctx) }()
	for i := 0; i < 200; i++ {
		if _, err := os.Lstat(soket); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	r2 := sor("TurIki")
	if strings.Contains(r2, "panel_yanit_vermiyor") || !strings.Contains(r2, "upstream timed out") {
		t.Fatalf("köprü gelince panel-bağımlı araç toparlanmalıydı:\n%s", r2)
	}
	// Kanarya: panel sırrı köprüden geçen günlükte de maskeli.
	if strings.Contains(r2, panelSirri) || strings.Contains(r2, kanaryaLog) {
		t.Fatal("köprü yolunda sır sızdı")
	}

	// 3) Panel tekrar düştü.
	kapat()
	<-bitti
	r3 := sor("TurUc")
	if !strings.Contains(r3, "panel_yanit_vermiyor") {
		t.Fatalf("panel yeniden düşünce panel_yanit_vermiyor beklenir:\n%s", r3)
	}
}
