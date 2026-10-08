package opsbetik

// frontend_geri_test.go — girginospanel-update'in "frontend-geri" bloğu.
//
// 🔴 Bulgu (181 güncelleme provası, 2026-10-08): 7. adım frontend-dist'i takas eder (eskisi
// frontend-dist.old olarak kalır); sağlıksız açılışta rollback ikiliyi, DB'yi ve göç dizinini geri
// alıyordu ama arayüze DOKUNMUYORDU → "geri alınmış" sistem ESKİ ikili + YENİ arayüz (index-<yeni>.js)
// olarak kaldı: yeni arayüz eski ikilide olmayan uç noktaları çağırır.
// CANLI SİSTEME DOKUNMAZ: blok işaretleri arasından çıkarılır, geçici dizinde bash ile koşulur.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	fgrBas = "# >>> frontend-geri"
	fgrSon = "# <<< frontend-geri"
)

func fgrBlok(t *testing.T) string {
	t.Helper()
	s := gdzBetik(t)
	i, j := strings.Index(s, fgrBas), strings.Index(s, fgrSon)
	if i < 0 || j < i || strings.Count(s, fgrBas) != 1 || strings.Count(s, fgrSon) != 1 {
		t.Fatal("girginospanel-update: frontend-geri bloğu yok/çift")
	}
	return s[i : j+len(fgrSon)]
}

// fgrKos: set -euo pipefail altında (gerçek betikteki gibi) bloğu + senaryoyu koşar. on: bloktan önce
// çalışan kurulum (ör. mv sahtesi).
func fgrKos(t *testing.T, kok, on, senaryo string) (string, error) {
	t.Helper()
	betik := "set -euo pipefail\n" +
		"ok(){ echo \"OK: $*\"; }\nwarn(){ echo \"WARN: $*\"; }\n" +
		"FDIST='" + filepath.Join(kok, "frontend-dist") + "'\n" +
		on + "\n" + fgrBlok(t) + "\n" + senaryo + "\n"
	out, err := exec.Command("bash", "-c", betik).CombinedOutput()
	return string(out), err
}

// fgrDizin: YENİ arayüz frontend-dist'te, güncelleme öncesi arayüz frontend-dist.old'da.
func fgrDizin(t *testing.T) string {
	t.Helper()
	kok := t.TempDir()
	gdzYaz(t, filepath.Join(kok, "frontend-dist", "index.html"), "YENI")
	gdzYaz(t, filepath.Join(kok, "frontend-dist", "assets", "index-yeni.js"), "yeni-js")
	gdzYaz(t, filepath.Join(kok, "frontend-dist.old", "index.html"), "ESKI")
	gdzYaz(t, filepath.Join(kok, "frontend-dist.old", "assets", "index-eski.js"), "eski-js")
	return kok
}

func fgrVar(yol string) bool { _, err := os.Stat(yol); return err == nil }

func fgrArtik(kok string) []string {
	k, _ := filepath.Glob(filepath.Join(kok, "frontend-dist.red.*"))
	return k
}

// Bu koşu arayüzü takas ettiyse rollback güncelleme öncesi arayüzü geri koyar; yeni arayüzün hiçbir
// dosyası kalmaz, .old tüketilir, kenara alınan kopya artık bırakılmaz.
func TestFrontendGeri_EskiArayuzGeriGelir(t *testing.T) {
	kok := fgrDizin(t)
	out, err := fgrKos(t, kok, "", "FDIST_TAKAS=1\nfrontend_geri_al")
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v\n%s", err, out)
	}
	fd := filepath.Join(kok, "frontend-dist")
	if g := gdzOku(filepath.Join(fd, "index.html")); g != "ESKI" {
		t.Errorf("güncelleme öncesi index.html geri gelmeli: %q", g)
	}
	if fgrVar(filepath.Join(fd, "assets", "index-yeni.js")) {
		t.Errorf("yeni arayüzün paketi KALMAMALI (eski ikili + yeni arayüz)")
	}
	if !fgrVar(filepath.Join(fd, "assets", "index-eski.js")) {
		t.Errorf("eski arayüzün paketi yerinde olmalı")
	}
	if fgrVar(fd + ".old") {
		t.Errorf(".old tüketilmeli (taşındı)")
	}
	if k := fgrArtik(kok); len(k) != 0 {
		t.Errorf("kenara alınan kopya artık bırakılmamalı: %v", k)
	}
	if !strings.Contains(out, "OK: frontend-dist güncelleme öncesine döndürüldü") {
		t.Errorf("rapor eksik: %s", out)
	}
}

// Bu koşu arayüzü takas ETMEDİYSE (ör. pakette frontend yok) ${FDIST}.old eski bir koşudan kalmadır:
// geri koymak çalışan arayüzü ESKİ bir sürümle ezerdi. Dokunulmaz, hata kodu döner.
func TestFrontendGeri_BuKosuTakasEtmediyseDokunmaz(t *testing.T) {
	kok := fgrDizin(t)
	out, err := fgrKos(t, kok, "", "if frontend_geri_al; then echo SONUC=0; else echo SONUC=1; fi")
	if err != nil {
		t.Fatalf("set -e altında düşmemeli: %v\n%s", err, out)
	}
	if !strings.Contains(out, "SONUC=1") {
		t.Errorf("takas yokken hata kodu dönmeli: %s", out)
	}
	if g := gdzOku(filepath.Join(kok, "frontend-dist", "index.html")); g != "YENI" {
		t.Errorf("bayat .old yüzünden çalışan arayüz ezilmemeli: %q", g)
	}
	if g := gdzOku(filepath.Join(kok, "frontend-dist.old", "index.html")); g != "ESKI" {
		t.Errorf(".old'a dokunulmamalı: %q", g)
	}
}

// Takas işaretli ama .old yoksa (ilk kurulum: önceki arayüz yoktu) hiçbir şey yapılmaz.
func TestFrontendGeri_OldYoksaDokunmaz(t *testing.T) {
	kok := fgrDizin(t)
	if err := os.RemoveAll(filepath.Join(kok, "frontend-dist.old")); err != nil {
		t.Fatal(err)
	}
	out, err := fgrKos(t, kok, "", "FDIST_TAKAS=1\nif frontend_geri_al; then echo SONUC=0; else echo SONUC=1; fi")
	if err != nil {
		t.Fatalf("düşmemeli: %v\n%s", err, out)
	}
	if !strings.Contains(out, "SONUC=1") {
		t.Errorf("geri konacak bir şey yokken hata kodu dönmeli: %s", out)
	}
	if g := gdzOku(filepath.Join(kok, "frontend-dist", "index.html")); g != "YENI" {
		t.Errorf("çalışan arayüze dokunulmamalı: %q", g)
	}
}

// frontend-dist hiç yoksa (silinmiş/bozuk) .old yine yerine konur.
func TestFrontendGeri_FdistYoksaOldYerineKonur(t *testing.T) {
	kok := fgrDizin(t)
	if err := os.RemoveAll(filepath.Join(kok, "frontend-dist")); err != nil {
		t.Fatal(err)
	}
	out, err := fgrKos(t, kok, "", "FDIST_TAKAS=1\nfrontend_geri_al")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if g := gdzOku(filepath.Join(kok, "frontend-dist", "index.html")); g != "ESKI" {
		t.Errorf("eski arayüz yerine konmalı: %q", g)
	}
}

// İkinci taşıma (.old → frontend-dist) başarısız olursa yeni arayüz GERİ konur: dizin yok kalıp
// BEYAZ EKRAN outage'ı çıkmamalı. .old yerinde kalır (elle kurtarma), uyarı verilir.
func TestFrontendGeri_IkinciTasimaBasarisizsaYeniArayuzGeriKonur(t *testing.T) {
	kok := fgrDizin(t)
	on := `mv(){ if [ "$1" = "${FDIST}.old" ]; then return 1; fi; command mv "$@"; }`
	out, err := fgrKos(t, kok, on, "FDIST_TAKAS=1\nif frontend_geri_al; then echo SONUC=0; else echo SONUC=1; fi")
	if err != nil {
		t.Fatalf("set -e altında düşmemeli: %v\n%s", err, out)
	}
	if !strings.Contains(out, "SONUC=1") || !strings.Contains(out, "WARN: frontend-dist geri alınamadı") {
		t.Errorf("hata kodu + uyarı bekleniyordu: %s", out)
	}
	if g := gdzOku(filepath.Join(kok, "frontend-dist", "index.html")); g != "YENI" {
		t.Errorf("frontend-dist yok kalmamalı; yeni arayüz geri konmalı: %q", g)
	}
	if g := gdzOku(filepath.Join(kok, "frontend-dist.old", "index.html")); g != "ESKI" {
		t.Errorf(".old elle kurtarma için yerinde kalmalı: %q", g)
	}
	if k := fgrArtik(kok); len(k) != 0 {
		t.Errorf("kenar kopya geri taşınmış olmalı, artık kalmamalı: %v", k)
	}
}

// İlk taşıma (frontend-dist → kenar) başarısız olursa hiçbir şey değişmez.
func TestFrontendGeri_IlkTasimaBasarisizsaDokunmaz(t *testing.T) {
	kok := fgrDizin(t)
	on := `mv(){ if [ "$1" = "$FDIST" ]; then return 1; fi; command mv "$@"; }`
	out, err := fgrKos(t, kok, on, "FDIST_TAKAS=1\nif frontend_geri_al; then echo SONUC=0; else echo SONUC=1; fi")
	if err != nil {
		t.Fatalf("set -e altında düşmemeli: %v\n%s", err, out)
	}
	if !strings.Contains(out, "SONUC=1") || !strings.Contains(out, "WARN: frontend-dist geri alınamadı") {
		t.Errorf("hata kodu + uyarı bekleniyordu: %s", out)
	}
	if g := gdzOku(filepath.Join(kok, "frontend-dist", "index.html")); g != "YENI" {
		t.Errorf("çalışan arayüze dokunulmamalı: %q", g)
	}
	if g := gdzOku(filepath.Join(kok, "frontend-dist.old", "index.html")); g != "ESKI" {
		t.Errorf(".old'a dokunulmamalı: %q", g)
	}
}

// Başarıdan sonra işaret sıfırlanır: aynı koşuda ikinci çağrı (ör. sonradan oluşan bir .old ile)
// arayüzü yeniden takas ETMEZ.
func TestFrontendGeri_BasaridanSonraIsaretSifirlanir(t *testing.T) {
	kok := fgrDizin(t)
	out, err := fgrKos(t, kok, "", `FDIST_TAKAS=1
frontend_geri_al
mkdir -p "${FDIST}.old"; echo BAYAT > "${FDIST}.old/index.html"
if frontend_geri_al; then echo IKINCI=0; else echo IKINCI=1; fi`)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "IKINCI=1") {
		t.Errorf("ikinci çağrı reddedilmeli: %s", out)
	}
	if g := gdzOku(filepath.Join(kok, "frontend-dist", "index.html")); g != "ESKI" {
		t.Errorf("ikinci çağrı geri alınmış arayüzü ezmemeli: %q", g)
	}
}

// Gerçek betikte çağrı SIRASI: blok varsayılanı (FDIST_TAKAS=0) 7. adımdan ÖNCE tanımlı (set -u);
// 7. adım takası işaretler; rollback'te panel durdurulduktan sonra, yeniden başlatmadan ÖNCE geri alınır.
func TestUpdateFrontendGeriCagriSirasi(t *testing.T) {
	tam := gdzBetik(t)
	if strings.Index(tam, fgrSon) > strings.Index(strings.Replace(tam, fgrBlok(t), "", 1), "FDIST_TAKAS=1") {
		t.Errorf("FDIST_TAKAS varsayılanı blokta, 7. adımdan ÖNCE tanımlanmalı (set -u)")
	}
	s := strings.Replace(tam, fgrBlok(t), "", 1)
	s = strings.Replace(s, gdzBlok(t), "", 1)
	idx := func(alt string, from int) int {
		i := strings.Index(s[from:], alt)
		if i < 0 {
			t.Fatalf("betikte yok: %q", alt)
		}
		return from + i
	}
	takas := idx(`mv "$FDNEW" "$FDIST"`, 0)
	isaret := idx(`FDIST_TAKAS=1`, takas)
	tamam := idx(`ok "frontend-dist güncellendi`, takas)
	if !(isaret < tamam) || isaret-takas > 200 {
		t.Errorf("takas işareti, takastan hemen sonra ve başarı satırından önce konmalı (takas=%d işaret=%d tamam=%d)", takas, isaret, tamam)
	}
	rb := idx(`yeni sürüm sağlıklı başlamadı`, 0)
	dur := idx(`systemctl stop girginospanel`, rb)
	geri := idx(`frontend_geri_al || true`, rb)
	restart := idx(`systemctl restart girginospanel; sleep 3`, rb)
	if !(dur < geri && geri < restart) {
		t.Errorf("rollback sırası: durdur < arayüz geri < yeniden başlat olmalı (dur=%d geri=%d restart=%d)", dur, geri, restart)
	}
	if strings.Count(s, "frontend_geri_al") != 1 {
		t.Errorf("frontend_geri_al betikte tek yerden (rollback) çağrılmalı: %d", strings.Count(s, "frontend_geri_al"))
	}
}
