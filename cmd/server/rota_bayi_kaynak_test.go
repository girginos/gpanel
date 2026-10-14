package main

// rota_bayi_kaynak_test.go — bayi kaynak havuzu uçlarının KAYIT satırlarını kilitler (yönlendirici
// main() içinde kurulduğu için kaynak satırlarından doğrulanır; rota_domainip_test.go deseni):
//   - /bayi-kaynak* (tüm bayilerin havuzu, disk kotası etkinleştirme) YALNIZ AdminOnly arkasında,
//   - /reseller/kaynak* AdminVeyaReseller (bayi kendi havuzunu görür; kimlik oturumdan gelir),
//   - hiçbiri API anahtarına AÇIK değil (varsayılan-reddet; grubby'ye giden bir uç anahtarla çağrılamaz);
//     TEK istisna salt okunur GET /bayi-kaynak/{id}: kök anahtarı bayi:oku ile (WHMCS bayi ürünü sayfası),
//   - bayi-disk-kotasi CLI'ı bilinmeyen argümanda hiçbir şey çalıştırmadan 2 ile döner.

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"girginospanel/internal/apianahtar"
)

func TestBayiKaynakRotalari_KapilarVeAPIAnahtari(t *testing.T) {
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?m)^\s*r(\.With\([^)]*\))?\.(Get|Post|Put|Patch|Delete)\("(/(?:bayi-kaynak|reseller/kaynak)[^"]*)",\s*([A-Za-z.]+)\)`)
	bul := re.FindAllStringSubmatch(string(b), -1)
	if len(bul) != 8 {
		t.Fatalf("main.go'da 8 kaynak ucu bekleniyor, %d bulundu (yeni uç eklenince bu test bilinçli güncellenmeli)", len(bul))
	}
	for _, m := range bul {
		kapi, metod, yol := m[1], strings.ToUpper(m[2]), m[3]
		bek := "(middleware.AdminOnly)"
		if strings.HasPrefix(yol, "/reseller/kaynak") {
			bek = "(middleware.AdminVeyaReseller)"
		}
		if kapi != ".With"+bek {
			t.Errorf("%s %s kapısı %q (beklenen .With%s)", metod, yol, kapi, bek)
		}
		// Bayi uçları SALT OKUNUR; yazan uçlar yalnız yönetici kapısında.
		if strings.HasPrefix(yol, "/reseller/kaynak") && metod != "GET" {
			t.Errorf("bayi ucu %s %s yazma yöntemi", metod, yol)
		}
		desen := yol
		if metod == "GET" && yol == "/bayi-kaynak/{id}" {
			// TEK istisna: salt okunur havuz durumu; yalnız kök anahtarı ve yalnız bayi izniyle.
			if k := apianahtar.Karar("GET /api/v1"+desen, apianahtar.RolBayi, []string{"hesap:yaz", "bayi:yaz", "domain:yaz"}); k == apianahtar.KararIzinli {
				t.Errorf("GET %s bayi rolündeki anahtara açık olmamalı", yol)
			}
			if k := apianahtar.Karar("GET /api/v1"+desen, "admin", []string{"bayi:oku"}); k != apianahtar.KararIzinli {
				t.Errorf("GET %s kök anahtarına bayi:oku ile açık olmalı (karar=%d)", yol, k)
			}
			if k := apianahtar.Karar("GET /api/v1"+desen, "admin", []string{"hesap:yaz"}); k != apianahtar.KararIzinYetmez {
				t.Errorf("GET %s bayi izni olmayan kök anahtarına kapalı olmalı (karar=%d)", yol, k)
			}
			continue
		}
		if k := apianahtar.Karar(metod+" /api/v1"+desen, apianahtar.RolBayi, []string{"hesap:yaz", "bayi:yaz", "domain:yaz"}); k != apianahtar.KararUcKapali {
			t.Errorf("%s %s API anahtarına AÇIK (karar=%d): grubby/uzlaştırma ucu anahtarla çağrılamamalı", metod, yol, k)
		}
		if k := apianahtar.Karar(metod+" /api/v1"+desen, "admin", []string{"bayi:yaz", "hesap:yaz"}); k != apianahtar.KararUcKapali {
			t.Errorf("%s %s yönetici rolündeki API anahtarına AÇIK (karar=%d)", metod, yol, k)
		}
	}
	// Statik "disk-kotasi" yolları {id}'den ÖNCE kayıtlı olmalı (chi'de {id} yakalayıp 400 üretmesin).
	s := string(b)
	if iDisk, iID := strings.Index(s, `"/bayi-kaynak/disk-kotasi"`), strings.Index(s, `"/bayi-kaynak/{id}"`); iDisk < 0 || iID < 0 || iDisk > iID {
		t.Error("main.go: /bayi-kaynak/disk-kotasi, /bayi-kaynak/{id}'den önce kaydedilmeli")
	}
	// Açılışta uzlaştırıcı + göç şema doğrulaması bağlı.
	for _, p := range []string{"go bayikaynak.Calistir(context.Background(), d)", `{"users", "kaynak_cpu_cekirdek"}`, `{"bayi_kaynak_kullanim", "bayi_id"}`} {
		if strings.Count(s, p) != 1 {
			t.Errorf("main.go: %q tam bir kez bulunmalı", p)
		}
	}
	// Kernel komut satırını değiştiren eylem (Etkinlestir / "grubby") YALNIZ yönetici API'si ve CLI'dan
	// çağrılır; panelin hiçbir arka plan görevi (açılış, uzlaştırma turu, devir...) buna dokunamaz.
	izinli := map[string]bool{"../../internal/reseller/kaynak_api.go": true, "bayi_disk.go": true}
	grubbyIzinli := "../../internal/bayikaynak/disk.go"
	for _, kok := range []string{"../../internal", "."} {
		err := filepath.WalkDir(kok, func(yol string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(yol, ".go") || strings.HasSuffix(yol, "_test.go") {
				return err
			}
			kaynak, err := os.ReadFile(yol)
			if err != nil {
				return err
			}
			yol = filepath.ToSlash(yol)
			if bytes.Contains(kaynak, []byte("bayikaynak.Etkinlestir")) && !izinli[yol] {
				t.Errorf("%s: bayikaynak.Etkinlestir yalnız yönetici API'si/CLI'dan çağrılabilir", yol)
			}
			if bytes.Contains(kaynak, []byte(`"grubby"`)) && yol != grubbyIzinli {
				t.Errorf("%s: kernel komut satırı (grubby) yalnız internal/bayikaynak/disk.go'dan değişir", yol)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestBayiDiskKotasiCLI_BilinmeyenArgumanHicbirSeyCalistirmaz(t *testing.T) {
	for _, a := range [][]string{{"--uygula=1"}, {"uygula"}, {"--apply"}, {"--uygula", "fazla"}, {"-x"}} {
		var out, er bytes.Buffer
		if kod := bayiDiskKotasiCLI(a, &out, &er); kod != 2 {
			t.Errorf("%v → çıkış %d (2 beklenir)", a, kod)
		}
		if out.Len() != 0 {
			t.Errorf("%v: hata yolunda host çıktısı üretildi (komut çalışmış olabilir): %q", a, out.String())
		}
		if !strings.Contains(er.String(), "bilinmeyen argüman") {
			t.Errorf("%v: hata mesajı: %q", a, er.String())
		}
	}
	var out, er bytes.Buffer
	if kod := bayiDiskKotasiCLI([]string{"--help"}, &out, &er); kod != 0 || !strings.Contains(out.String(), "kullanım") {
		t.Errorf("--help → %d %q", kod, out.String())
	}
}
