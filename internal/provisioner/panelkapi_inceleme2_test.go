package provisioner

// panelkapi_inceleme2_test.go — 2. çoklu-ajan incelemesinin düzeltmeleri (ADR-GP-001):
// dışarıda dinlenmeyen rol kapısı, silinmiş harita onarımı, SELinux etiket önbelleği ve
// CLI hazırlığı, uygulandı/doğrulanamadı ayrımı.

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"girginospanel/internal/kapi"
)

// Müşteri kapısı yalnız loopback izi olan porta atanmış (elle düzenlenmiş kapi.json):
// Heal o porta genel listen eklemez. Eski/beklenmeyen genel portlar KALDIRILMAZ (rolün
// son yolu), firewall'da kapatılmaz, zorlama açılmaz.
func TestKapiHealRolDisaKapaliKaldirmaYok(t *testing.T) {
	a := &kapi.Ayar{Admin: 21001, Bayi: 22002, Musteri: 9443} // 9443: kapiTestCanli'de 127.0.0.1
	s := kapiTestKur(t, kapiTestCanli, a)
	acilabilir, uygulandi := kapiUzlastirDetay(1000)
	if acilabilir || !uygulandi {
		t.Fatalf("dışarıda dinlenmeyen müşteri kapısı: acilabilir=%v uygulandi=%v", acilabilir, uygulandi)
	}
	conf := oku(t, kapiVhostYolu)
	if !strings.Contains(conf, "listen 8443 ssl;") {
		t.Fatalf("eski genel port KALDIRILMAMALI (müşterilerin son yolu):\n%s", conf)
	}
	if len(s.fwKapanan) != 0 {
		t.Fatalf("firewall'da port kapatılmamalı: %v", s.fwKapanan)
	}
}

// Kapı haritası silinmiş, vhost hâlâ $gp_kapi'ye atıf yapıyor → `nginx -t` GLOBAL düşük.
// Taban kontrolü onarımı engellememeli: harita yeniden yazılır, nginx yeniden yüklenir.
func TestKapiHealHaritaSilinmisseOnarir(t *testing.T) {
	izli, err := kapi.Uzlastir(kapiTestSablon, rolAyar(), 0, kapi.Secenek{Gizli: kapiTestGizli})
	if err != nil {
		t.Fatal(err)
	}
	s := kapiTestKur(t, izli, rolAyar())
	// Gerçek nginx gibi: harita yokken $gp_kapi atfı `nginx -t`'yi düşürür.
	kapiNginxKomut = func(ad string, arg ...string) ([]byte, error) {
		c := ad + " " + strings.Join(arg, " ")
		s.cagrilar = append(s.cagrilar, c)
		if c == "nginx -t" {
			if _, err := os.Stat(kapiMapYolu); err != nil && strings.Contains(oku(t, kapiVhostYolu), "$gp_kapi") {
				return []byte(`nginx: [emerg] unknown "gp_kapi" variable`), errors.New("exit 1")
			}
		}
		if c == "systemctl reload nginx" {
			s.rSayac++
		}
		return nil, nil
	}
	if !kapiUzlastir(1000) {
		t.Fatal("silinmiş harita onarılmalı (zorlama açılabilir)")
	}
	if m := oku(t, kapiMapYolu); !strings.Contains(m, "21001 admin;") {
		t.Fatalf("harita yeniden yazılmalı:\n%s", m)
	}
	if s.rSayac == 0 {
		t.Fatal("onarım nginx'e yüklenmeli (reload)")
	}
	// İlgisiz bozukluk (harita olsa da düşen nginx -t) onarım sayılmaz: geri alınır.
	s2 := kapiTestKur(t, izli, rolAyar())
	s2.nginxT = func(int) error { return errors.New("başka vhost bozuk") }
	if kapiUzlastir(1000) {
		t.Fatal("onarım sonrası da düşen nginx -t → uygulanmamalı")
	}
	if _, err := os.Stat(kapiMapYolu); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("başarısız onarım geri alınmalı (harita yok kalmalı)")
	}
}

// Kapı modu kapalı + harita silinmiş + başlık satırları kalmış: başlığı temizlemek de
// onarımdır (taban kontrolü engellemez).
func TestKapiModKapaliHaritasizBaslikTemizlenir(t *testing.T) {
	izli, _ := kapi.Uzlastir(kapiTestSablon, rolAyar(), 0, kapi.Secenek{Gizli: kapiTestGizli})
	s := kapiTestKur(t, izli, nil)
	kapiNginxKomut = func(ad string, arg ...string) ([]byte, error) {
		c := ad + " " + strings.Join(arg, " ")
		s.cagrilar = append(s.cagrilar, c)
		if c == "nginx -t" && strings.Contains(oku(t, kapiVhostYolu), "$gp_kapi") {
			return nil, errors.New("unknown gp_kapi")
		}
		return nil, nil
	}
	kapiUzlastir(1000)
	if strings.Contains(oku(t, kapiVhostYolu), kapi.Baslik) {
		t.Fatal("haritasız başlık satırları temizlenmeli")
	}
}

// Etiketler /run önbelleğine yazılır; "yeniden başlatma" sonrası (bellek boş, önbellek
// dolu) açılış semanage KOŞTURMAZ — güncelleyicinin sağlık penceresi aşılmasın.
func TestKapiSELinuxOnbellek(t *testing.T) {
	kapiTestKur(t, kapiTestSablon, rolAyar())
	kapiSELinuxModu = func() string { return "enforcing" }
	liste := 0
	kapiSELinuxListe = func() (string, error) { liste++; return "unreserved_port_t tcp 1024-32767\n", nil }
	kapiSELinuxEtiket = func(int) error { return nil }
	if !kapiUzlastir(1000) {
		t.Fatal("ilk uzlaştırma")
	}
	if got := strings.Fields(oku(t, kapiSELinuxOnbellek)); strings.Join(got, ",") != "21001,22002,23003" {
		t.Fatalf("önbellek: %v", got)
	}
	// Yeniden başlatma: bellek boş, önbellek dosyası yerinde.
	kapiEtiketli, kapiOnbellekOkundu = map[int]bool{}, false
	kapiSELinuxListe = func() (string, error) { t.Error("önbellekteki portlar için semanage koşmamalı"); return "", nil }
	kapiSELinuxEtiket = func(int) error { t.Error("önbellekteki port yeniden etiketlenmemeli"); return nil }
	if !kapiUzlastir(1000) || liste != 1 {
		t.Fatalf("önbellekle uzlaştırma: liste=%d", liste)
	}
}

// CLI hazırlığı: kapı modu yok → 3, SELinux kapalı → 0, Enforcing + etiket → 0 ve Heal
// için "yeniden yükle" işareti; etiketlenemez → 1. Heal işareti tüketip reload eder.
func TestKapiSELinuxHazirla(t *testing.T) {
	s := kapiTestKur(t, kapiTestSablon, nil)
	if kod := KapiSELinuxHazirla(); kod != 3 {
		t.Fatalf("kapı modu yokken 3, got %d", kod)
	}
	_ = kapi.Yaz(kapiAyarYolu, rolAyar())
	if kod := KapiSELinuxHazirla(); kod != 0 {
		t.Fatalf("SELinux kapalıyken 0, got %d", kod)
	}
	kapiSELinuxModu = func() string { return "enforcing" }
	kapiSELinuxListe = func() (string, error) { return "mountd_port_t tcp 22002\nunreserved_port_t tcp 1024-32767\n", nil }
	kapiSELinuxEtiket = func(int) error { return nil }
	if kod := KapiSELinuxHazirla(); kod != 1 {
		t.Fatalf("başka servise ayrılmış port Enforcing'de → 1, got %d", kod)
	}
	kapiEtiketli, kapiOnbellekOkundu = map[int]bool{}, false
	kapiSELinuxListe = func() (string, error) { return "unreserved_port_t tcp 1024-32767\n", nil }
	if kod := KapiSELinuxHazirla(); kod != 0 {
		t.Fatalf("etiketlenebilir portlar → 0, got %d", kod)
	}
	if _, err := os.Stat(kapiSELinuxOnbellek + ".yukle"); err != nil {
		t.Fatal("yeni etiket → Heal için yeniden yükleme işareti bırakılmalı")
	}
	// Panel açılışı: dosyalar önceden uygulanmış olsa bile işaret reload ettirir.
	kapiUzlastir(1000)
	onceki := s.rSayac
	_ = os.WriteFile(kapiSELinuxOnbellek+".yukle", nil, 0o644)
	kapiUzlastir(1000)
	if s.rSayac != onceki+1 {
		t.Fatalf("işaret reload ettirmeli: %d → %d", onceki, s.rSayac)
	}
	if _, err := os.Stat(kapiSELinuxOnbellek + ".yukle"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("işaret tüketilmeli")
	}
}

// Arayüz "uygulandı ama doğrulanamadı" ile "hiç uygulanamadı"yı ayırır.
func TestKapiSimdiUygulaUygulandiBilgisi(t *testing.T) {
	s := kapiTestKur(t, kapiTestSablon, rolAyar())
	eski := kapiTipOlcucu
	t.Cleanup(func() { kapiTipOlcucu = eski })
	s.nginxT = func(n int) error {
		if n == 2 {
			return errors.New("emerg")
		}
		return nil
	}
	var uh *kapi.UygulamaHatasi
	if err := KapiSimdiUygula(context.Background()); !errors.As(err, &uh) || uh.Uygulandi {
		t.Fatalf("nginx -t düşünce uygulanmadı: %v", err)
	}
	s.nginxT = nil
	kapiTipOlcucu = func(context.Context, int, string, int) (string, error) { return "", errors.New("connection refused") }
	if err := KapiSimdiUygula(context.Background()); !errors.As(err, &uh) || !uh.Uygulandi {
		t.Fatalf("ölçüm düştüğünde değişiklik uygulanmış sayılmalı: %v", err)
	}
}
