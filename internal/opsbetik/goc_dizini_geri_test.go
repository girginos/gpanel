package opsbetik

// goc_dizini_geri_test.go — girginospanel-update'in "goc-dizini-geri" bloğu.
//
// 🔴 Bulgu (181 güncelleme provası, 2026-10-08): 8. adım paketin göç dosyalarını src/migrations'a açar;
// sağlıksız açılışta rollback ikiliyi + DB'yi geri alıyordu ama dizine DOKUNMUYORDU. ESKİ ikili açılırken
// dizindeki TÜM .sql'leri okuyup defterde olmayanı yeniden uyguladığı için "geri alınmış" sistem eski
// ikili + YENİ şema oluyordu (0217 FK'yi yeniden düşürdü, tablo geri geldi, defter 128).
// CANLI SİSTEME DOKUNMAZ: blok işaretleri arasından çıkarılır, geçici dizinde bash ile koşulur.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	gdzBas = "# >>> goc-dizini-geri"
	gdzSon = "# <<< goc-dizini-geri"
)

func gdzBetik(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../assets/ops/girginospanel-update")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func gdzBlok(t *testing.T) string {
	t.Helper()
	s := gdzBetik(t)
	i, j := strings.Index(s, gdzBas), strings.Index(s, gdzSon)
	if i < 0 || j < i || strings.Count(s, gdzBas) != 1 || strings.Count(s, gdzSon) != 1 {
		t.Fatal("girginospanel-update: goc-dizini-geri bloğu yok/çift")
	}
	return s[i : j+len(gdzSon)]
}

// gdzKos: set -euo pipefail altında (gerçek betikteki gibi) bloğu + senaryoyu koşar.
func gdzKos(t *testing.T, kok, senaryo string) (string, error) {
	t.Helper()
	betik := "set -euo pipefail\n" +
		"ok(){ echo \"OK: $*\"; }\nwarn(){ echo \"WARN: $*\"; }\n" +
		"MIGR='" + filepath.Join(kok, "migrations") + "'\n" +
		gdzBlok(t) + "\n" + senaryo + "\n"
	out, err := exec.Command("bash", "-c", betik).CombinedOutput()
	return string(out), err
}

func gdzYaz(t *testing.T, yol, icerik string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(yol), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(yol, []byte(icerik), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gdzOku(yol string) string {
	b, err := os.ReadFile(yol)
	if err != nil {
		return "<yok>"
	}
	return string(b)
}

func gdzDizin(t *testing.T) string {
	t.Helper()
	kok := t.TempDir()
	m := filepath.Join(kok, "migrations")
	gdzYaz(t, filepath.Join(m, "0001_a.sql"), "A")
	gdzYaz(t, filepath.Join(m, "0002_b.sql"), "B")
	gdzYaz(t, filepath.Join(m, "0003_c.sql"), "C")
	return kok
}

// Yeni eklenen göç dosyası kaldırılır; değişen ve silinen dosya yedekten geri gelir; dizin yedekle
// çağrıldığı andaki hâline BİREBİR döner.
func TestGocDiziniGeri_YeniKaldirilirDegisenSilinenGeriGelir(t *testing.T) {
	kok := gdzDizin(t)
	m := filepath.Join(kok, "migrations")
	out, err := gdzKos(t, kok, `
goc_dizini_yedekle
echo YENI > "$MIGR/0004_yeni.sql"
echo DEGISTI > "$MIGR/0002_b.sql"
rm -f "$MIGR/0003_c.sql"
goc_dizini_geri_al`)
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v\n%s", err, out)
	}
	if _, e := os.Stat(filepath.Join(m, "0004_yeni.sql")); !os.IsNotExist(e) {
		t.Errorf("bu güncellemenin eklediği 0004 KALDIRILMALIYDI (eski ikili yeniden uygulardı)\n%s", out)
	}
	if g := gdzOku(filepath.Join(m, "0002_b.sql")); g != "B" {
		t.Errorf("değişen 0002 yedekten geri gelmeli: %q", g)
	}
	if g := gdzOku(filepath.Join(m, "0003_c.sql")); g != "C" {
		t.Errorf("silinen 0003 yedekten geri gelmeli: %q", g)
	}
	if g := gdzOku(filepath.Join(m, "0001_a.sql")); g != "A" {
		t.Errorf("dokunulmayan 0001 aynı kalmalı: %q", g)
	}
	if !strings.Contains(out, "1 yeni göç dosyası kaldırıldı") {
		t.Errorf("rapor eksik: %s", out)
	}
}

// Yedek alınmadıysa (ya da başarısızsa) geri alma dizine DOKUNMAZ ve hata kodu döner.
func TestGocDiziniGeri_YedekYoksaDokunmaz(t *testing.T) {
	kok := gdzDizin(t)
	m := filepath.Join(kok, "migrations")
	out, err := gdzKos(t, kok, `
echo YENI > "$MIGR/0004_yeni.sql"
if goc_dizini_geri_al; then echo SONUC=0; else echo SONUC=1; fi`)
	if err != nil {
		t.Fatalf("set -e altında düşmemeli: %v\n%s", err, out)
	}
	if !strings.Contains(out, "SONUC=1") {
		t.Errorf("yedek yokken hata kodu dönmeli: %s", out)
	}
	if g := gdzOku(filepath.Join(m, "0004_yeni.sql")); g != "YENI\n" {
		t.Errorf("yedek yokken dizine dokunulmamalı: %q", g)
	}
}

// Göç dizini yoksa yedekleme uyarır, hata koduyla döner; geri alma sonra da no-op.
func TestGocDiziniYedekle_DizinYoksaUyarirDevamEder(t *testing.T) {
	kok := t.TempDir() // migrations YOK
	out, err := gdzKos(t, kok, `
if goc_dizini_yedekle; then echo Y=0; else echo Y=1; fi
if goc_dizini_geri_al; then echo G=0; else echo G=1; fi
echo BAK="[$MIGR_BAK]"`)
	if err != nil {
		t.Fatalf("düşmemeli: %v\n%s", err, out)
	}
	for _, bek := range []string{"WARN: göç dizini yedeklenemedi", "Y=1", "G=1", "BAK=[]"} {
		if !strings.Contains(out, bek) {
			t.Errorf("%q yok: %s", bek, out)
		}
	}
}

// Başarıda yedek temizlenir; yedek hiç yoksa temizleme set -e altında DÜŞMEZ.
func TestGocDiziniTemizle(t *testing.T) {
	kok := gdzDizin(t)
	out, err := gdzKos(t, kok, `
goc_dizini_yedekle
[ -d "$MIGR.bak" ] && echo VAR_ONCE
goc_dizini_temizle
[ -d "$MIGR.bak" ] && echo VAR_SONRA || echo YOK_SONRA
goc_dizini_temizle`)
	if err != nil {
		t.Fatalf("boş temizleme set -e altında düşmemeli: %v\n%s", err, out)
	}
	if !strings.Contains(out, "VAR_ONCE") || !strings.Contains(out, "YOK_SONRA") || strings.Contains(out, "VAR_SONRA") {
		t.Errorf("yedek oluşmalı sonra kalkmalı: %s", out)
	}
}

// İkinci güncelleme: eski bayat .bak yeni yedekle EZİLİR (eski yedek yeni durumu bozmaz).
func TestGocDiziniYedekle_BayatBakEzilir(t *testing.T) {
	kok := gdzDizin(t)
	gdzYaz(t, filepath.Join(kok, "migrations.bak", "9999_bayat.sql"), "BAYAT")
	out, err := gdzKos(t, kok, `
goc_dizini_yedekle
echo YENI > "$MIGR/0004_yeni.sql"
goc_dizini_geri_al`)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, e := os.Stat(filepath.Join(kok, "migrations", "9999_bayat.sql")); !os.IsNotExist(e) {
		t.Errorf("bayat .bak içeriği migrations'a SIZMAMALI")
	}
}

// Gerçek betikte çağrı SIRASI: yedek → (tar ile göç açma); rollback'te panel durdurulduktan sonra
// ve yeniden başlatmadan ÖNCE geri al; başarıda temizle.
func TestUpdateGocDiziniCagriSirasi(t *testing.T) {
	s := strings.Replace(gdzBetik(t), gdzBlok(t), "", 1)
	idx := func(alt string, from int) int {
		i := strings.Index(s[from:], alt)
		if i < 0 {
			t.Fatalf("betikte yok: %q", alt)
		}
		return from + i
	}
	yedek := idx(`goc_dizini_yedekle || true`, 0)
	tar := idx(`tar xzf "$A/migrations.tar.gz" -C "$MIGR"`, 0)
	if !(yedek < tar) {
		t.Errorf("göç dizini yedeği, göç arşivi AÇILMADAN önce alınmalı (yedek=%d tar=%d)", yedek, tar)
	}
	saglikli := idx(`ok "panel ACTIVE + /healthz OK`, 0)
	temizle := idx(`goc_dizini_temizle`, saglikli)
	if temizle-saglikli > 400 {
		t.Errorf("temizleme sağlıklı-başlangıç dalında olmalı (uzaklık=%d)", temizle-saglikli)
	}
	rb := idx(`yeni sürüm sağlıklı başlamadı`, 0)
	dur := idx(`systemctl stop girginospanel`, rb)
	geri := idx(`goc_dizini_geri_al && goc_dizini_temizle || true`, rb)
	restart := idx(`systemctl restart girginospanel; sleep 3`, rb)
	dbGeri := idx(`zcat "$PREDUMP" | mysql`, rb)
	if !(dur < geri && geri < restart) {
		t.Errorf("rollback sırası: durdur < göç dizini geri < yeniden başlat olmalı (dur=%d geri=%d restart=%d)", dur, geri, restart)
	}
	if geri > dbGeri {
		t.Errorf("göç dizini DB geri yüklemeden ÖNCE ya da en geç ondan bağımsız ama yeniden başlatmadan önce dönmeli (geri=%d db=%d)", geri, dbGeri)
	}
}

func TestUpdateBetigiSozdizimi(t *testing.T) {
	if out, err := exec.Command("bash", "-n", "../../assets/ops/girginospanel-update").CombinedOutput(); err != nil {
		t.Fatalf("bash -n: %v\n%s", err, out)
	}
}
