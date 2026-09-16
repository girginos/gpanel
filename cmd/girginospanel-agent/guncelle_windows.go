//go:build windows

// guncelle_windows.go — AJANIN KENDINI GUVENLI GUNCELLEMESI (Update + Rollback).
//
// 🔴 NEDEN: kullanicinin sarti "guncelleme yaparken bir sey digerini
// cokertmesin". Eski akis (`kur`) yalnizca exe'yi kopyaliyordu — yeni binary
// bozuksa servis coker, geri donus YOK, sunucu yonetilemez kalir. Bu motor
// stabilite rehberi #4 (state machine + rollback) ve #12 (health-gate) uygular:
//
//  1. ON-SINAMA  — yeni exe TAKAS ONCESI `surum` ile calistirilir; calismiyorsa
//     servise HIC dokunulmadan reddedilir (fail-fast).
//  2. YEDEK      — mevcut exe atomik rename ile `.guncelleme-yedek`e alinir.
//  3. TAKAS+BASLAT
//  4. HEALTH-GATE — servis Running VE /saglik beklenen surumu dondurene kadar
//     yoklanir; gecmezse...
//  5. OTOMATIK ROLLBACK — eski binary geri konur, baslatilir, sagligi DOGRULANIR.
//
// Boylece basarisiz bir guncelleme, calisan sistemi otomatik olarak eski
// (saglikli) surume dondurur; operator bozuk bir ajanla bas basa kalmaz.
package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"girginospanel/internal/platform"
)

// guncelle — <yeniExe>'yi guvenli sekilde kurulu ajanin yerine alir.
func guncelle(yeniExe string) error {
	if !yoneticiMi() {
		return fmt.Errorf("yonetici (elevated) gerekli")
	}
	yeniExe = filepath.Clean(yeniExe)
	fi, err := os.Stat(yeniExe)
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return fmt.Errorf("yeni exe bulunamadi/gecersiz: %s", yeniExe)
	}
	hedef := filepath.Join(kurulumDizini, exeAdi)
	if ayniDosyaMi(yeniExe, hedef) {
		return fmt.Errorf("yeni exe kurulu exe ile AYNI dosya — once ayri bir konuma koyun")
	}

	// 1) ON-SINAMA (takas ONCESI): yeni binary calisiyor mu? `surum` ile dogrula.
	//    Bozuk/uyumsuz binary servise HIC dokunmadan reddedilir.
	yeniSurum, err := exeSurum(yeniExe)
	if err != nil {
		return fmt.Errorf("yeni exe on-sinama BASARISIZ (calismiyor), takas YAPILMADI: %w", err)
	}
	fmt.Printf("on-sinama gecti — yeni: %q, mevcut: %q\n", yeniSurum, platform.Surum+" "+platform.Kanal)

	// 2) MEVCUDU YEDEKLE (rollback kaynagi). Once servisi durdur (calisan exe kilitli).
	yedek := hedef + ".guncelleme-yedek"
	_ = os.Remove(yedek)
	_, _ = kos("sc", "stop", servisAdi)
	servisDurumBekleAjan("STOPPED", 30*time.Second)
	time.Sleep(1 * time.Second)
	if err := os.Rename(hedef, yedek); err != nil {
		// rename olmadiysa kopyala + devam (yedek kopya da rollback icin yeterli).
		if e2 := kopyala(hedef, yedek); e2 != nil {
			_, _ = kos("sc", "start", servisAdi)
			return fmt.Errorf("mevcut exe yedeklenemedi (%v / %v) — guncelleme iptal, servis geri baslatildi", err, e2)
		}
	}

	// 3) YENIYI KUR.
	if err := kopyala(yeniExe, hedef); err != nil {
		// koyamadik → yedegi geri koy, baslat, iptal.
		_ = os.Remove(hedef)
		_ = os.Rename(yedek, hedef)
		_, _ = kos("sc", "start", servisAdi)
		return fmt.Errorf("yeni exe kopyalanamadi (eski geri konuldu): %w", err)
	}

	// 4) BASLAT + HEALTH-GATE.
	if out, err := kos("sc", "start", servisAdi); err != nil {
		return guncellemeRollback(hedef, yedek, fmt.Sprintf("servis baslamadi: %v — %s", err, strings.TrimSpace(out)))
	}
	if err := saglikBekle(yeniSurum, 60*time.Second); err != nil {
		return guncellemeRollback(hedef, yedek, err.Error())
	}

	// 5) COMMIT — yedegi `.onceki` olarak sakla (elle rollback icin).
	onceki := hedef + ".onceki"
	_ = os.Remove(onceki)
	if err := os.Rename(yedek, onceki); err != nil {
		_ = os.Remove(yedek) // saklanamadi; sorun degil, guncelleme basarili
	}
	fmt.Println("GUNCELLEME TAMAM ✓ — yeni surum saglikli:", yeniSurum)
	fmt.Println("  onceki binary saklandi:", onceki)
	return nil
}

// guncellemeRollback — yeni binary health-gate'i gecemedi: eskiyi geri koy,
// baslat, eskinin sagligini DOGRULA. Rollback da basarisizsa DURUST felaket
// raporu (operator elle mudahale etsin).
func guncellemeRollback(hedef, yedek, neden string) error {
	fmt.Fprintln(os.Stderr, "🔴 HEALTH-GATE BASARISIZ:", neden, "— ROLLBACK yapiliyor")
	_, _ = kos("sc", "stop", servisAdi)
	servisDurumBekleAjan("STOPPED", 30*time.Second)
	time.Sleep(1 * time.Second)
	_ = os.Remove(hedef)
	if err := os.Rename(yedek, hedef); err != nil {
		return fmt.Errorf("🔴🔴 ROLLBACK BASARISIZ (%s): eski binary geri konamadi: %w — ELLE: %q → %q", neden, err, yedek, hedef)
	}
	if out, err := kos("sc", "start", servisAdi); err != nil {
		return fmt.Errorf("🔴🔴 rollback sonrasi servis baslamadi (%s): %v — %s", neden, err, strings.TrimSpace(out))
	}
	// Eski binary'nin kendi surumuyle saglik: bu komutu calistiran (kurulu) ajan
	// da ayni surum oldugundan platform.Surum beklenen degerdir.
	if err := saglikBekle(platform.Surum, 60*time.Second); err != nil {
		return fmt.Errorf("🔴🔴 rollback yapildi ama eski binary de saglik gecemedi (%s): %w", neden, err)
	}
	return fmt.Errorf("guncelleme reddedildi, ESKI SURUME guvenle donuldu (neden: %s)", neden)
}

// exeSurum — bir ajan exe'sini `surum` ile calistirir, "<surum> <kanal>" satirini
// dondurur. Calismazsa (bozuk/uyumsuz binary) hata → on-sinama reddi.
func exeSurum(exe string) (string, error) {
	out, err := exec.Command(exe, "surum").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s surum: %v — %s", exe, err, strings.TrimSpace(string(out)))
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return "", fmt.Errorf("surum ciktisi bos (ajan binary'si degil olabilir)")
	}
	return s, nil
}

// saglikBekle — servis Running olana VE /saglik BEKLENEN surumu dondurene kadar
// yoklar (localhost, jeton). Health-gate cekirdegi. beklenenSurum "<surum> <kanal>"
// bicimindedir; yalniz ilk alan (surum) /saglik.surum ile karsilastirilir.
//
// 🔴 InsecureSkipVerify: bu YEREL bir canlilik kontrolu (kendi ajanimiza
// localhost uzerinden), uzak kimlik dogrulamasi DEGIL — panelin TOFU igneleme
// yolu ayridir ve buna dokunmaz.
func saglikBekle(beklenenSurum string, sure time.Duration) error {
	ayar, err := ayarYukle()
	if err != nil {
		return fmt.Errorf("saglik kontrolu icin jeton okunamadi: %w", err)
	}
	adres := ayar.Adres
	if strings.HasPrefix(adres, "0.0.0.0") {
		adres = "127.0.0.1" + strings.TrimPrefix(adres, "0.0.0.0")
	}
	url := "https://" + adres + "/saglik"
	beklenen := beklenenSurum
	if f := strings.Fields(beklenenSurum); len(f) > 0 {
		beklenen = f[0]
	}
	cli := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
	bitis := time.Now().Add(sure)
	sonHata := "zaman asimi"
	for time.Now().Before(bitis) {
		durum, _ := kos("sc", "query", servisAdi)
		if strings.Contains(durum, "RUNNING") {
			req, _ := http.NewRequest(http.MethodGet, url, nil)
			req.Header.Set("X-Gosp-Jeton", ayar.Jeton)
			resp, e := cli.Do(req)
			if e == nil {
				b, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					var s struct {
						Surum string `json:"surum"`
					}
					_ = json.Unmarshal(b, &s)
					if s.Surum == beklenen {
						return nil // saglikli + dogru surum servis ediliyor
					}
					sonHata = fmt.Sprintf("saglik 200 ama surum %q != beklenen %q", s.Surum, beklenen)
				} else {
					sonHata = fmt.Sprintf("saglik HTTP %d", resp.StatusCode)
				}
			} else {
				sonHata = "saglik'a baglanilamadi: " + e.Error()
			}
		} else {
			sonHata = "servis Running degil"
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("health-gate gecilemedi (%s)", sonHata)
}

// servisDurumBekleAjan — sc query ile servis durumunun hedefe (STOPPED/RUNNING)
// ulasmasini bekler; ulasmazsa sessizce doner (cagiran zaten sonrasini yonetir).
func servisDurumBekleAjan(hedefBuyuk string, sure time.Duration) {
	bitis := time.Now().Add(sure)
	for time.Now().Before(bitis) {
		out, _ := kos("sc", "query", servisAdi)
		if strings.Contains(out, hedefBuyuk) {
			return
		}
		time.Sleep(1 * time.Second)
	}
}
