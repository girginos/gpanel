package opsbetik

// ikili_atla_test.go — assets/ops/ altında İKİLİ (ELF) dosya KALMAMALI ve kurucu/güncelleyici/paketleyici bunu zorlar.
//
// 🔴 Bulgu (GirginOSVM dev'de gerçek VPS provası, 2026-10-09): pakette assets/ops/girginospanel-destek-ajan ve
// assets/ops/girginospanel-avajan vardı (git'te YOK; 10-04'te paketleme betiği build makinesinin /usr/local/bin'indeki
// elle kurulu kopyaları assets/ops'a kopyalamıştı, o günden beri her pakete girdi). Kurucu da güncelleyici de
// assets/ kökündeki TAZE ikiliyi kurduktan SONRA `for t in ops/*` döngüsüyle ops/ kopyasını /usr/local/bin'e
// yazıyordu → hem güncellenen hem taze kurulan sunucu BAYAT ajanla kaldı: destek ajanı 10-04 tarihli KVKK
// anonimleştirme/maske kodu (TCKN/IBAN maskesi, anonimlestirDB) OLMADAN, antivirüs ajanı eski kopya.
// CANLI SİSTEME DOKUNMAZ: blok işaretleri arasından çıkarılır, geçici dizinde bash ile koşulur.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const ikiBas, ikiSon = "# >>> ", "# <<< "

func ikiOku(t *testing.T, yol string) string {
	t.Helper()
	b, err := os.ReadFile(yol)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ikiBlok: dosyadan "# >>> ad" ... "# <<< ad" arasını (işaretler dahil) çıkarır; yok/çiftse test düşer.
func ikiBlok(t *testing.T, yol, ad string) string {
	t.Helper()
	s := ikiOku(t, yol)
	bas, son := ikiBas+ad, ikiSon+ad
	i, j := strings.Index(s, bas), strings.Index(s, son)
	if i < 0 || j < i || strings.Count(s, bas) != 1 || strings.Count(s, son) != 1 {
		t.Fatalf("%s: %q bloğu yok/çift", yol, ad)
	}
	return s[i : j+len(son)]
}

// ikiKos: set -euo pipefail altında (gerçek betikler gibi) sahte yardımcılarla + bloğu + senaryoyu koşar.
func ikiKos(t *testing.T, on, blok, senaryo string) (string, error) {
	t.Helper()
	betik := "set -euo pipefail\n" +
		"ok(){ echo \"OK: $*\"; }\nwarn(){ echo \"WARN: $*\"; }\nuyar(){ echo \"UYAR: $*\"; }\n" +
		"die(){ echo \"DIE: $*\"; exit 1; }\ndur(){ echo \"DUR: $*\"; exit 1; }\n" +
		"install(){ echo \"INSTALL $*\"; }\n" +
		on + "\n" + blok + "\n" + senaryo + "\n"
	out, err := exec.Command("bash", "-c", betik).CombinedOutput()
	return string(out), err
}

func ikiELF(t *testing.T, yol string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(yol), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(yol, []byte("\x7fELF\x02\x01\x01\x00bayat-ikili"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func ikiBetik(t *testing.T, yol string) {
	t.Helper()
	gdzYaz(t, yol, "#!/bin/bash\necho merhaba\n")
	_ = os.Chmod(yol, 0o755)
}

// ops dizini: bir betik + bir ELF + yapılandırma + artık yedek.
func ikiOps(t *testing.T) string {
	t.Helper()
	a := t.TempDir()
	ikiBetik(t, filepath.Join(a, "ops", "girginospanel-ornek.sh"))
	ikiELF(t, filepath.Join(a, "ops", "girginospanel-destek-ajan"))
	ikiELF(t, filepath.Join(a, "ops", "girginospanel-avajan"))
	gdzYaz(t, filepath.Join(a, "ops", "50-gosp-jail.conf"), "x\n")
	gdzYaz(t, filepath.Join(a, "ops", "girginospanel-eski.bak"), "x\n")
	return a
}

// Depoda assets/ops altında ELF dosyası bulunmamalı (izlenen ya da izlenmeyen — çalışma ağacı gerçeği).
func TestOpsDizini_IkiliYok(t *testing.T) {
	ds, err := filepath.Glob("../../assets/ops/*")
	if err != nil || len(ds) == 0 {
		t.Fatalf("assets/ops okunamadı: %v (%d dosya)", err, len(ds))
	}
	for _, d := range ds {
		f, err := os.Open(d)
		if err != nil {
			continue
		}
		b := make([]byte, 4)
		n, _ := f.Read(b)
		_ = f.Close()
		if n == 4 && string(b) == "\x7fELF" {
			t.Errorf("assets/ops/%s bir İKİLİ (ELF): ikililer assets/ kökünden kurulur; ops/ kopyası bayat kalır ve taze olanı ezer", filepath.Base(d))
		}
	}
}

func ikiOpsKontrol(t *testing.T, out string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v\n%s", err, out)
	}
	for _, yok := range []string{"girginospanel-destek-ajan", "girginospanel-avajan"} {
		if regexp.MustCompile(`INSTALL .*(/usr/local/bin|/opt/girginospanel/src/scripts)/` + yok + `\b`).MatchString(out) {
			t.Errorf("İKİLİ %s ops döngüsünden KURULDU (taze ikiliyi ezer):\n%s", yok, out)
		}
		if !strings.Contains(out, "WARN: ops/"+yok) {
			t.Errorf("atlanan ikili %s için uyarı yok:\n%s", yok, out)
		}
	}
	if !regexp.MustCompile(`INSTALL .*/usr/local/bin/girginospanel-ornek\b`).MatchString(out) {
		t.Errorf("sıradan ops betiği (girginospanel-ornek) KURULMADI — kapı fazla geniş:\n%s", out)
	}
	if strings.Contains(out, "girginospanel-eski") {
		t.Errorf(".bak artığı kuruldu:\n%s", out)
	}
}

func TestUpdater_OpsDongusu_IkiliyiAtlar(t *testing.T) {
	a := ikiOps(t)
	blok := ikiBlok(t, "../../assets/ops/girginospanel-update", "ops-kur-dongusu")
	out, err := ikiKos(t, "A='"+a+"'", blok, "")
	ikiOpsKontrol(t, out, err)
}

func TestKurucu_OpsDongusu_IkiliyiAtlar(t *testing.T) {
	a := ikiOps(t)
	blok := ikiBlok(t, "../../girginospanel-install.sh", "ops-kur-dongusu")
	out, err := ikiKos(t, "A='"+a+"'", blok, "")
	ikiOpsKontrol(t, out, err)
}

// Güncelleyici ikilileri assets/ kökünden alır: her `find "$TMP" … -name girginospanel-*` ops/ dizinini dışlar.
func TestUpdater_IkiliAramasiOpsuDislar(t *testing.T) {
	s := ikiOku(t, "../../assets/ops/girginospanel-update")
	re := regexp.MustCompile(`(?m)^.*find "\$TMP" -maxdepth 3 -type f -name girginospanel-.*$`)
	satirlar := re.FindAllString(s, -1)
	if len(satirlar) < 4 {
		t.Fatalf("beklenen ≥4 ikili araması, bulunan %d", len(satirlar))
	}
	for _, l := range satirlar {
		if !strings.Contains(l, "! -path '*/assets/ops/*'") {
			t.Errorf("ikili araması ops/ kopyasını dışlamıyor (find sırası şansa kalır): %s", strings.TrimSpace(l))
		}
	}
}

// Paketleme: build makinesindeki ikili assets/ops'a KOPYALANMAZ, betik kopyalanır.
func TestPaketle_OpsKopya_IkiliyiAtlar(t *testing.T) {
	kok := t.TempDir()
	ikiELF(t, filepath.Join(kok, "bin", "girginospanel-destek-ajan"))
	ikiELF(t, filepath.Join(kok, "bin", "girginospanel-avajan"))
	ikiBetik(t, filepath.Join(kok, "bin", "girginospanel-ornek"))
	if err := os.MkdirAll(filepath.Join(kok, "sbin"), 0o755); err != nil {
		t.Fatal(err)
	}
	blok := ikiBlok(t, "../../scripts/girginospanel-paketle", "paket-ops-kopya")
	eski := "/usr/local/bin/girginospanel-* /usr/local/sbin/girginospanel-*"
	if !strings.Contains(blok, eski) {
		t.Fatal("paketle: /usr/local/bin tarama satırı değişmiş — test güncellenmeli")
	}
	blok = strings.Replace(blok, eski, kok+"/bin/girginospanel-* "+kok+"/sbin/girginospanel-*", 1)
	a := filepath.Join(kok, "assets")
	out, err := ikiKos(t, "A='"+a+"'\nmkdir -p \"$A/ops\"", blok, "")
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v\n%s", err, out)
	}
	for _, yok := range []string{"girginospanel-destek-ajan", "girginospanel-avajan"} {
		if _, err := os.Stat(filepath.Join(a, "ops", yok)); err == nil {
			t.Errorf("İKİLİ %s assets/ops'a kopyalandı:\n%s", yok, out)
		}
	}
	if _, err := os.Stat(filepath.Join(a, "ops", "girginospanel-ornek")); err != nil {
		t.Errorf("sıradan betik assets/ops'a kopyalanmadı — kapı fazla geniş:\n%s", out)
	}
}

// Paketleme kapısı: assets/ops'ta ELF varsa paket ÜRETİLMEZ (dur); yalnız betik varsa geçer.
func TestPaketle_IkiliKapisi(t *testing.T) {
	blok := ikiBlok(t, "../../scripts/girginospanel-paketle", "paket-ops-ikili-kapisi")

	kirli := t.TempDir()
	ikiBetik(t, filepath.Join(kirli, "ops", "girginospanel-ornek"))
	ikiELF(t, filepath.Join(kirli, "ops", "girginospanel-destek-ajan"))
	out, err := ikiKos(t, "A='"+kirli+"'", blok, "echo GECTI")
	if err == nil || strings.Contains(out, "GECTI") || !strings.Contains(out, "DUR:") || !strings.Contains(out, "girginospanel-destek-ajan") {
		t.Errorf("ELF'li ops dizininde paketleme DURMALI (err=%v):\n%s", err, out)
	}

	temiz := t.TempDir()
	ikiBetik(t, filepath.Join(temiz, "ops", "girginospanel-ornek"))
	gdzYaz(t, filepath.Join(temiz, "ops", "50-gosp-jail.conf"), "x\n")
	out, err = ikiKos(t, "A='"+temiz+"'", blok, "echo GECTI")
	if err != nil || !strings.Contains(out, "GECTI") {
		t.Errorf("yalnız betik içeren ops dizininde kapı GEÇMELİ (err=%v):\n%s", err, out)
	}
}
