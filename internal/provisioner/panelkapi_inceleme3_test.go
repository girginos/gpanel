package provisioner

// panelkapi_inceleme3_test.go — 3. çoklu-ajan incelemesinin düzeltmeleri (ADR-GP-001):
// ölçüm uzlaştırmanın kararını aşamaz (OLC-1), zorlama kapanınca bakım döngüsü dürtülür
// (OLC-2), zorlama bayrağı + /run dosyası tek kilit altında ve atomik (OLC-3), SELinux
// onarım komutu gerçeği ölçer ve bağlanamayan port önbellekten düşer (HS-1), "yeniden yükle"
// işareti yalnız başarılı uygulamadan sonra tüketilir (HS-2); işaretin zorladığı reload
// yapılamadı diye rol ayrımı kapanmaz, karar ölçümün (4. doğrulama, OLC-ISARET-1).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"girginospanel/internal/kapi"
)

// kapiTestOlcucu: ölçücüyü gerçek zincirin DOĞRU yanıtıyla değiştirir (her rol portu kendi
// tipini kanıtlı döner). Dönen anahtar açıkken her ölçüm kesin başarısızdır (bağlantı reddi);
// anahtar atomiktir — bakım döngüsü ayrı goroutine'de okur. sayac: ölçücü çağrı sayısı.
func kapiTestOlcucu(t *testing.T) (hata *atomic.Bool, sayac *atomic.Int32) {
	t.Helper()
	eski := kapiTipOlcucu
	t.Cleanup(func() { kapiTipOlcucu = eski })
	hata, sayac = new(atomic.Bool), new(atomic.Int32)
	yanit := map[int]string{21001: "yonetim", 22002: "yonetim", 23003: "musteri", 9443: "musteri"}
	kapiTipOlcucu = func(_ context.Context, port int, _ string, _ int) (string, error) {
		sayac.Add(1)
		if hata.Load() {
			return "", errors.New("connection refused")
		}
		return yanit[port], nil
	}
	return hata, sayac
}

// bekleZorlama: zorlama sure içinde v olur mu?
func bekleZorlama(v bool, sure time.Duration) bool {
	son := time.Now().Add(sure)
	for time.Now().Before(son) {
		if kapi.ZorlamaAktif() == v {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return kapi.ZorlamaAktif() == v
}

// OLC-1: müşteri kapısı yalnız loopback izi olan portta (dışarıdan erişilemez). Açılış
// uzlaştırması "açılamaz" der; ilk ölçüm 127.0.0.1'den kanıtı tutturabilse de zorlamayı
// AÇMAMALI — yoksa her açılışta bakım aralığı (15 dk) boyunca müşteriler kilitli kalırdı.
// Açılamaz kararından sonra ölçüme hiç gidilmez (açılışın 20 denemelik ölçümü boşa koşmaz).
func TestKapiOlcumUzlastirmaKararinaUyar(t *testing.T) {
	kapiTestKur(t, kapiTestCanli, &kapi.Ayar{Admin: 21001, Bayi: 22002, Musteri: 9443})
	_, sayac := kapiTestOlcucu(t)
	eskiZK := kapi.ZincirKopuk
	t.Cleanup(func() { kapi.ZincirKopuk = eskiZK })
	HealPanelKapiOnStartup()
	if kapi.ZorlamaAktif() {
		t.Fatal("ön koşul: dışa kapalı müşteri kapısıyla açılış uzlaştırması zorlamayı kapalı bırakmalı")
	}
	if s := kapiOlcVeAyarla(context.Background(), 20); s != olcumKopuk || kapi.ZorlamaAktif() {
		t.Fatalf("\"açılamaz\" kararından sonra ölçüm zorlamayı AÇMAMALI: sonuç=%v zorlama=%v", s, kapi.ZorlamaAktif())
	}
	if n := sayac.Load(); n != 0 {
		t.Fatalf("açılamaz kararından sonra ölçüme gidilmemeli, ölçüm=%d", n)
	}
	if d := oku(t, kapiZorlamaDosyasi); d != "kapali\n" {
		t.Fatalf("zorlama dosyası kapalı demeli: %q", d)
	}
}

// OLC-1 (yarış): uçuştaki bir ölçüm, o sürerken verilen uzlaştırma "açılamaz" kararını
// ezemez — araya yeniden "açılabilir" kararı girse bile (ölçüm eski düzeni ölçtü). Son
// karardan SONRA başlayan taze ölçüm açar.
func TestKapiUcustakiOlcumKapatmayiEzmez(t *testing.T) {
	s := kapiTestKur(t, kapiTestSablon, rolAyar())
	if !kapiUzlastir(1000) {
		t.Fatal("ön koşul: uzlaştırma açılabilir")
	}
	eski := kapiTipOlcucu
	t.Cleanup(func() { kapiTipOlcucu = eski })
	basladi, devam := make(chan struct{}), make(chan struct{})
	var ilk atomic.Bool
	kapiTipOlcucu = func(_ context.Context, port int, _ string, _ int) (string, error) {
		if ilk.CompareAndSwap(false, true) {
			close(basladi)
			<-devam // ölçüm uçuşta
		}
		return map[int]string{21001: "yonetim", 22002: "yonetim", 23003: "musteri"}[port], nil
	}
	sonuc := make(chan olcumSonucu, 1)
	go func() { sonuc <- kapiOlcVeAyarla(context.Background(), 1) }()
	<-basladi
	s.gizli = "" // proxy gizlisi yok → uzlaştırma "açılamaz"
	if kapiUzlastir(1000) {
		t.Fatal("gizlisiz uzlaştırma açılamaz demeli")
	}
	s.gizli = kapiTestGizli
	if !kapiUzlastir(1000) {
		t.Fatal("gizli dönünce uzlaştırma yeniden açılabilir demeli")
	}
	close(devam)
	if r := <-sonuc; r == olcumTamam || kapi.ZorlamaAktif() {
		t.Fatalf("kapatma kararından önce başlayan ölçüm zorlamayı AÇMAMALI: sonuç=%v zorlama=%v", r, kapi.ZorlamaAktif())
	}
	if r := kapiOlcVeAyarla(context.Background(), 1); r != olcumTamam || !kapi.ZorlamaAktif() {
		t.Fatalf("son karardan sonra başlayan ölçüm açmalı: sonuç=%v zorlama=%v", r, kapi.ZorlamaAktif())
	}
}

// kapiTestDongu: bakım döngüsünü ayrı goroutine'de başlatır; test bitince durdurur ve
// ÇIKMASINI bekler (dikişler geri yüklenmeden önce — yarış olmasın).
func kapiTestDongu(t *testing.T, aralik time.Duration) {
	t.Helper()
	ctx, iptal := context.WithCancel(context.Background())
	bitti := make(chan struct{})
	go func() {
		defer close(bitti)
		kapiBakimDongusu(ctx, aralik)
	}()
	t.Cleanup(func() { iptal(); <-bitti })
}

// OLC-2: döngü son ölçümde TAMAM gördü ve tam aralıkta (burada 1 saat; üretimde 15 dk)
// bekliyor. Döngü DIŞINDAKİ bir yol zorlamayı geçici bir nedenle KAPATIRSA döngü dürtülmeli:
// neden geçince zorlama geri çekilmenin ilk adımında (üretimde 1 dk) yeniden açılır, rol
// ayrımı tam aralık kapalı kalmaz. Dört kapatma yolunun her biri ayrı ölçülür.
func TestKapiZorlamaKapanincaDonguDurtulur(t *testing.T) {
	type yol struct {
		gizliYok bool // uzlaştırma "açılamaz" (proxy gizlisi yok)
		zincir   bool // kapiZincirKopuk (yoksa KapiSimdiUygula)
	}
	for ad, y := range map[string]yol{
		"KapiSimdiUygula uzlaştırma": {gizliYok: true},
		"KapiSimdiUygula ölçüm":      {},
		"zincir kopuk uzlaştırma":    {gizliYok: true, zincir: true},
		"zincir kopuk ölçüm":         {zincir: true},
	} {
		t.Run(ad, func(t *testing.T) {
			kapiTestKur(t, kapiTestSablon, rolAyar())
			olcHata, _ := kapiTestOlcucu(t)
			var gizliYok atomic.Bool
			kapiProxyGizli = func() string {
				if gizliYok.Load() {
					return ""
				}
				return kapiTestGizli
			}
			eskiIlk, eskiAra := kapiGeriCekilmeIlk, kapiZincirAra
			kapiGeriCekilmeIlk, kapiZincirAra = 50*time.Millisecond, time.Hour
			kapiZincirTetik.Store(false)
			t.Cleanup(func() {
				kapiGeriCekilmeIlk, kapiZincirAra = eskiIlk, eskiAra
				kapiZincirTetik.Store(false)
			})
			for len(kapiYenidenOlc) > 0 {
				<-kapiYenidenOlc
			}
			if !kapiUzlastir(1000) { // açılış uzlaştırması
				t.Fatal("ön koşul: uzlaştırma açılabilir")
			}
			kapiTestDongu(t, time.Hour)
			if !bekleZorlama(true, 5*time.Second) {
				t.Fatal("ön koşul: döngünün ilk ölçümü zorlamayı açmalı")
			}
			// Geçici neden: kapatma yolu koşar, zorlama kapanır, neden hemen geçer.
			if y.gizliYok {
				gizliYok.Store(true)
			} else {
				olcHata.Store(true)
			}
			if y.zincir {
				kapiZincirKopuk()
				if !bekleZorlama(false, 5*time.Second) {
					t.Fatal("zincir kopuk yolu zorlamayı kapatmalı")
				}
			} else if err := KapiSimdiUygula(context.Background()); err == nil || kapi.ZorlamaAktif() {
				t.Fatalf("KapiSimdiUygula: hata=%v zorlama=%v", err, kapi.ZorlamaAktif())
			}
			gizliYok.Store(false)
			olcHata.Store(false)
			if !bekleZorlama(true, 5*time.Second) {
				t.Fatal("zorlama kapanınca bakım döngüsü dürtülmeli: neden geçince geri çekilmenin ilk adımında yeniden açılmalı (tam aralık beklenmemeli)")
			}
		})
	}
}

// OLC-2: dürtü engellemez ve kaybolmaz — tampon doluysa bekleyen dürtü aynı işi görür.
func TestKapiDonguDurtusuEngellemez(t *testing.T) {
	for len(kapiYenidenOlc) > 0 {
		<-kapiYenidenOlc
	}
	bitti := make(chan struct{})
	go func() {
		defer close(bitti)
		for i := 0; i < 3; i++ {
			kapiDonguyuDurt()
		}
	}()
	select {
	case <-bitti:
	case <-time.After(3 * time.Second):
		t.Fatal("dürtü tampon doluyken engellememeli")
	}
	if n := len(kapiYenidenOlc); n != 1 {
		t.Fatalf("tamponda tek bekleyen dürtü olmalı, got %d", n)
	}
	<-kapiYenidenOlc
}

// OLC-3: bayrak ve /run dosyası tek kilit altında birlikte yazılır — eşzamanlı ters iki
// karardan sonra ikisi ayrışmaz ("dosya açık / bayrak kapalı" panelhost'a yalan söylerdi).
// Dosya atomik değişir: eşzamanlı okuyan (panelhost `head -c 32`) boş/yarım dosya görmez.
func TestKapiZorlamaDosyaBayrakTutarli(t *testing.T) {
	kapiTestKur(t, kapiTestSablon, rolAyar())
	eskiCikti := log.Writer()
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(eskiCikti) })
	kapiZorlamaYaz(false, "başlangıç")
	dur := make(chan struct{})
	var bozuk atomic.Int32
	var okuyucu sync.WaitGroup
	okuyucu.Add(1)
	go func() {
		defer okuyucu.Done()
		for {
			select {
			case <-dur:
				return
			default:
			}
			if b, err := os.ReadFile(kapiZorlamaDosyasi); err != nil || (string(b) != "acik\n" && string(b) != "kapali\n") {
				bozuk.Add(1)
			}
		}
	}()
	ayrisan := 0
	for i := 0; i < 1500; i++ {
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); kapiZorlamaYaz(true, "a") }()
		go func() { defer wg.Done(); kapiZorlamaYaz(false, "b") }()
		wg.Wait()
		if (oku(t, kapiZorlamaDosyasi) == "acik\n") != kapi.ZorlamaAktif() {
			ayrisan++
		}
	}
	close(dur)
	okuyucu.Wait()
	if ayrisan != 0 || bozuk.Load() != 0 {
		t.Fatalf("dosya ile bayrak ayrışmamalı ve dosya hiç boş/yarım okunmamalı: ayrışan=%d/1500 bozuk-okuma=%d", ayrisan, bozuk.Load())
	}
}

// onbellekYaz: SELinux önbelleğini (bu önyüklemede "doğrulanmış" portlar) doğrudan yazar.
func onbellekYaz(t *testing.T, icerik string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(kapiSELinuxOnbellek), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kapiSELinuxOnbellek, []byte(icerik), 0o644); err != nil {
		t.Fatal(err)
	}
}

// HS-1: `kapi selinux` (dogrula'nın önerdiği onarım) önbelleğe GÜVENMEZ. Önbellek üç portu
// "etiketli" derken etiketler panel dışında silinmiş: komut semanage listesinden ölçer,
// yeniden etiketler ve Heal için işaret bırakır. Yeniden etiketlenemeyen port önbellekten
// DÜŞER: panel açılışı onu önbellekten "hazır" saymaz, semanage ile yeniden doğrular.
func TestKapiSELinuxHazirlaOnbellegeGuvenmez(t *testing.T) {
	kapiTestKur(t, kapiTestSablon, rolAyar())
	kapiSELinuxModu = func() string { return "enforcing" }
	onbellekYaz(t, "21001\n22002\n23003\n")
	liste := 0
	var etiket []int
	kapiSELinuxListe = func() (string, error) { liste++; return "unreserved_port_t tcp 1024-32767\n", nil }
	kapiSELinuxEtiket = func(p int) error { etiket = append(etiket, p); return nil }
	if kod := KapiSELinuxHazirla(); kod != 0 || liste != 1 || fmt.Sprint(etiket) != "[21001 22002 23003]" {
		t.Fatalf("onarım gerçek durumu ölçmeli: kod=%d liste=%d etiket=%v", kod, liste, etiket)
	}
	if !kapiYukleBekliyor() {
		t.Fatal("yeniden eklenen etiket → Heal için yeniden yükleme işareti")
	}

	// Yeni CLI süreci: 21001'in etiketi silinmiş ve yeniden eklenemiyor; 22002 başka servise
	// ayrılmış. İkisi de önbellekten düşmeli, 1 dönmeli.
	kapiEtiketli, kapiOnbellekOkundu = map[int]bool{}, false
	kapiSELinuxListe = func() (string, error) {
		return "mountd_port_t tcp 22002\nhttp_port_t tcp 23003\nunreserved_port_t tcp 1024-32767\n", nil
	}
	kapiSELinuxEtiket = func(int) error { return errors.New("ValueError") }
	if kod := KapiSELinuxHazirla(); kod != 1 {
		t.Fatalf("etiketlenemeyen port Enforcing'de → 1, got %d", kod)
	}
	if got := strings.Fields(oku(t, kapiSELinuxOnbellek)); strings.Join(got, ",") != "23003" {
		t.Fatalf("ölçümde etiketsiz görülen portlar önbellekten düşmeli: %v", got)
	}

	// Panel açılışı (yeni süreç): önbellekten düşen portlar semanage ile yeniden doğrulanır.
	kapiEtiketli, kapiOnbellekOkundu = map[int]bool{}, false
	liste = 0
	kapiSELinuxListe = func() (string, error) { liste++; return "unreserved_port_t tcp 1024-32767\n", nil }
	kapiSELinuxEtiket = func(int) error { return nil }
	if !kapiUzlastir(1000) || liste != 1 {
		t.Fatalf("açılış önbellekten düşen portu semanage ile doğrulamalı: liste=%d", liste)
	}
}

// HS-1: önbellek "etiketli" dese de nginx portu bağlayamadıysa (etiket panel dışında silinmiş
// olabilir) o port önbellekten düşer; sonraki Heal semanage ile yeniden doğrular, eksik
// etiketi ekler ve yeniden yükler — önbelleğe güvenip her turda yaz/geri al döngüsüne girmez.
func TestKapiHealBaglanamayanPortOnbellektenDuser(t *testing.T) {
	s := kapiTestKur(t, kapiTestSablon, rolAyar())
	kapiSELinuxModu = func() string { return "enforcing" }
	onbellekYaz(t, "21001\n22002\n23003\n")
	s.dinliyor = false
	if kapiUzlastir(1000) {
		t.Fatal("portlar bağlanmadıysa açılabilir olmamalı")
	}
	if got := strings.TrimSpace(oku(t, kapiSELinuxOnbellek)); got != "" {
		t.Fatalf("bağlanamayan portlar önbellekten düşmeli: %q", got)
	}
	liste := 0
	var etiket []int
	kapiSELinuxListe = func() (string, error) { liste++; return "unreserved_port_t tcp 1024-32767\n", nil }
	kapiSELinuxEtiket = func(p int) error { etiket = append(etiket, p); return nil }
	s.dinliyor = true
	onceki := s.rSayac
	if !kapiUzlastir(1000) || liste != 1 || len(etiket) != 3 || s.rSayac <= onceki {
		t.Fatalf("sonraki Heal semanage ile yeniden doğrulamalı: liste=%d etiket=%v reload=%d→%d", liste, etiket, onceki, s.rSayac)
	}
}

// yukleIsaretiBirak: CLI'ın "yeniden yükle" işaretini taklit eder.
func yukleIsaretiBirak(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(kapiSELinuxOnbellek), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kapiSELinuxOnbellek+".yukle", nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

// HS-2: "yeniden yükle" işareti YALNIZ başarılı uygulamadan (reload + port ölçümü) sonra
// tüketilir. Dosyalar zaten uygulanmış (dosya değişikliği yok → reload'u yalnız işaret
// tetikler); taban `nginx -t`, reload ya da port bağlama düşerse işaret kalır, sorun geçince
// sonraki Heal yine yükler — yoksa CLI'ın etiketlediği portlar ilgisiz bir reload'a dek
// bağlanmazdı.
//
// OLC-ISARET-1: yalnız işaretin zorladığı reload YAPILAMADIYSA (taban `nginx -t`, reload
// komutu) bu "açılamaz" değildir — nginx dokunulmamış yapılandırmasıyla çalışır, kararı uçtan
// uca ölçüm verir. Reload yapılıp portların nginx'e bağlanmadığı ÖLÇÜLDÜYSE zincir gerçekten
// kopuktur: açılamaz.
func TestKapiYukleIsaretiBasaridanSonraTuketilir(t *testing.T) {
	for ad, k := range map[string]struct {
		boz        func(s *kapiSahte)
		acilabilir bool
	}{
		"taban nginx -t":  {func(s *kapiSahte) { s.nginxT = func(int) error { return errors.New("başka vhost bozuk") } }, true},
		"reload hatası":   {func(s *kapiSahte) { s.reloadHata = func(int) error { return errors.New("reload failed") } }, true},
		"port bağlanmadı": {func(s *kapiSahte) { s.dinliyor = false }, false},
	} {
		t.Run(ad, func(t *testing.T) {
			s := kapiTestKur(t, kapiTestSablon, rolAyar())
			if !kapiUzlastir(1000) {
				t.Fatal("ön koşul: dosyalar uygulanmalı")
			}
			yukleIsaretiBirak(t)
			k.boz(s)
			if got := kapiUzlastir(1000); got != k.acilabilir {
				t.Fatalf("yükleme düşerken açılabilir=%v, beklenen=%v (yalnız zorlanan reload yapılamadıysa karar ölçümde; bağlanmadığı ölçülen port kopukluktur)", got, k.acilabilir)
			}
			if !kapiYukleBekliyor() {
				t.Fatal("başarısız uygulamada işaret KALMALI")
			}
			s.nginxT, s.reloadHata, s.dinliyor = nil, nil, true
			onceki := s.rSayac
			if !kapiUzlastir(1000) || s.rSayac != onceki+1 {
				t.Fatalf("sorun geçince işaret reload ettirmeli: %d → %d", onceki, s.rSayac)
			}
			if kapiYukleBekliyor() {
				t.Fatal("başarılı uygulamadan sonra işaret tüketilmeli")
			}
		})
	}
}

// OLC-ISARET-1: kapı düzeni uygulanmış, zincir sağlam, zorlama ölçümle AÇIK. Sonra bir
// "yeniden yükle" işareti bekler (ör. `kapi selinux` onarımı panel dışında silinen etiketi
// yeniden ekledi) ve ilgisiz bir vhost `nginx -t`'yi global düşürür (ya da reload düşer).
// Dosyalarda değişiklik yok: yapılamayan yalnız işaretin zorladığı reload. Bakım turları
// rol ayrımını KAPATMAMALI — nginx dokunulmamış yapılandırmasıyla çalışıyor, karar uçtan uca
// ölçümün (zincir gerçekten koparsa ölçüm kapatır). İşaret korunur; sorun geçince sonraki
// Heal yeniden yükler ve tüketir.
func TestKapiYukleIsaretiIlgisizTabanHatasindaOlcumKarar(t *testing.T) {
	for ad, boz := range map[string]func(s *kapiSahte, v bool){
		"taban nginx -t": func(s *kapiSahte, v bool) {
			s.nginxT = nil
			if v {
				s.nginxT = func(int) error { return errors.New("başka vhost bozuk (silinmiş sertifika)") }
			}
		},
		"reload hatası": func(s *kapiSahte, v bool) {
			s.reloadHata = nil
			if v {
				s.reloadHata = func(int) error { return errors.New("reload failed") }
			}
		},
	} {
		t.Run(ad, func(t *testing.T) {
			s := kapiTestKur(t, kapiTestSablon, rolAyar())
			olcHata, _ := kapiTestOlcucu(t)
			if !kapiUzlastir(1000) || kapiOlcVeAyarla(context.Background(), 1) != olcumTamam || !kapi.ZorlamaAktif() {
				t.Fatal("ön koşul: uzlaştırma + ölçüm TAMAM, zorlama açık")
			}
			yukleIsaretiBirak(t)
			boz(s, true)
			conf := oku(t, kapiVhostYolu)
			for tur := 1; tur <= 5; tur++ { // bakım döngüsü turu: uzlaştır → (açılabilirse) ölç
				if !kapiUzlastir(1000) {
					t.Fatalf("tur %d: yalnız işaretin zorladığı reload yapılamadı diye \"açılamaz\" denmemeli (zorlama=%v)", tur, kapi.ZorlamaAktif())
				}
				if r := kapiOlcVeAyarla(context.Background(), 1); r != olcumTamam || !kapi.ZorlamaAktif() {
					t.Fatalf("tur %d: zincir sağlam — ölçüm zorlamayı AÇIK tutmalı: sonuç=%v zorlama=%v", tur, r, kapi.ZorlamaAktif())
				}
				if !kapiYukleBekliyor() {
					t.Fatalf("tur %d: yapılamayan yeniden yükleme için işaret KORUNMALI", tur)
				}
			}
			if oku(t, kapiVhostYolu) != conf {
				t.Fatal("dosyalarda değişiklik yoktu — _panel.conf değişmemeli")
			}
			// Karar ölçümün: zincir gerçekten koparsa (ör. etiketli port bağlanmamış) ölçüm kapatır.
			olcHata.Store(true)
			if !kapiUzlastir(1000) || kapiOlcVeAyarla(context.Background(), 1) != olcumKopuk || kapi.ZorlamaAktif() {
				t.Fatalf("kopuk zinciri ölçüm kapatmalı: zorlama=%v", kapi.ZorlamaAktif())
			}
			olcHata.Store(false)
			boz(s, false)
			onceki := s.rSayac
			if !kapiUzlastir(1000) || s.rSayac != onceki+1 || kapiYukleBekliyor() {
				t.Fatalf("sorun geçince işaret reload ettirip tüketilmeli: reload %d → %d, işaret=%v", onceki, s.rSayac, kapiYukleBekliyor())
			}
			if kapiOlcVeAyarla(context.Background(), 1) != olcumTamam || !kapi.ZorlamaAktif() {
				t.Fatal("onarımdan sonra ölçüm zorlamayı yeniden açmalı")
			}
		})
	}
}

// OLC-ISARET-1 (sınır): GERÇEK bir dosya değişikliği uygulanamıyorsa davranış DEĞİŞMEZ —
// işaret beklese de: taban `nginx -t` düşükse dosyaya hiç dokunulmaz, reload düşerse her şey
// geri alınıp eski yapılandırma yeniden yüklenir; ikisinde de "açılamaz" denir ve işaret kalır.
func TestKapiYukleIsaretiDosyaDegisikligiDuserseKapatir(t *testing.T) {
	for ad, k := range map[string]struct {
		boz    func(s *kapiSahte)
		reload int // beklenen `systemctl reload nginx` sayısı
	}{
		"taban nginx -t": {func(s *kapiSahte) { s.nginxT = func(int) error { return errors.New("başka vhost bozuk") } }, 0},
		"reload hatası": {func(s *kapiSahte) {
			s.reloadHata = func(n int) error {
				if n == 1 {
					return errors.New("reload failed")
				}
				return nil
			}
		}, 2},
	} {
		t.Run(ad, func(t *testing.T) {
			s := kapiTestKur(t, kapiTestSablon, rolAyar())
			yukleIsaretiBirak(t)
			k.boz(s)
			if kapiUzlastir(1000) {
				t.Fatal("dosya değişikliği uygulanamadıysa açılabilir denmemeli")
			}
			if oku(t, kapiVhostYolu) != kapiTestSablon {
				t.Fatal("_panel.conf özgün hâlinde kalmalı")
			}
			if _, err := os.Stat(kapiMapYolu); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("önceden olmayan map dosyası kalmamalı")
			}
			if s.rSayac != k.reload {
				t.Fatalf("reload sayısı %d, beklenen %d", s.rSayac, k.reload)
			}
			if !kapiYukleBekliyor() {
				t.Fatal("başarısız uygulamada işaret KALMALI")
			}
		})
	}
}

// HS-2 (yarış): Heal sürerken CLI YENİ bir etiket işareti bırakırsa, Heal kendi aldığı işareti
// tüketir ama yenisini SİLMEZ — sonraki Heal onu da yükler.
func TestKapiYukleIsaretiHealSirasindaYenisiKorunur(t *testing.T) {
	s := kapiTestKur(t, kapiTestSablon, rolAyar())
	if !kapiUzlastir(1000) {
		t.Fatal("ön koşul")
	}
	yukleIsaretiBirak(t)
	ilk := true
	s.reloadHata = func(int) error {
		if ilk { // reload anında CLI başka bir portu etiketledi
			ilk = false
			yukleIsaretiBirak(t)
		}
		return nil
	}
	onceki := s.rSayac
	if !kapiUzlastir(1000) || s.rSayac != onceki+1 {
		t.Fatalf("işaret reload ettirmeli: %d → %d", onceki, s.rSayac)
	}
	if _, err := os.Stat(kapiSELinuxOnbellek + ".yukle"); err != nil {
		t.Fatal("Heal sürerken bırakılan YENİ işaret silinmemeli")
	}
	if !kapiUzlastir(1000) || s.rSayac != onceki+2 {
		t.Fatalf("yeni işaret sonraki Heal'de yüklenmeli: %d → %d", onceki, s.rSayac)
	}
	if kapiYukleBekliyor() {
		t.Fatal("işaretler tüketilmeli")
	}
}

// HS-2 (Heal'in kendi etiketi): eski sürümün etiketsiz listen'ları dosyada; Heal portları
// kendisi etiketler ama yükleyemez (taban nginx -t). Portlar artık önbellekte olsa da
// sonraki Heal yine yeniden yüklemeli (dosya değişikliği yok — reload'u yalnız işaret tetikler).
// Yüklenemeyen yeniden yükleme "açılamaz" değildir (OLC-ISARET-1): etiketsiz port nginx'e
// bağlı değilse bunu uçtan uca ölçüm bulur.
func TestKapiHealKendiEtiketiReloadBekler(t *testing.T) {
	s := kapiTestKur(t, kapiTestSablon, rolAyar())
	if !kapiUzlastir(1000) { // SELinux kapalıyken uygulandı (etiketsiz listen'lar)
		t.Fatal("ön koşul")
	}
	kapiSELinuxModu = func() string { return "enforcing" }
	kapiSELinuxListe = func() (string, error) { return "unreserved_port_t tcp 1024-32767\n", nil }
	kapiSELinuxEtiket = func(int) error { return nil }
	s.nginxT = func(int) error { return errors.New("başka vhost bozuk") }
	if !kapiUzlastir(1000) || !kapiYukleBekliyor() {
		t.Fatalf("taban bozuk, dosya değişikliği yok: karar ölçümde (açılabilir) ve işaret korunmalı (işaret=%v)", kapiYukleBekliyor())
	}
	s.nginxT = nil
	kapiSELinuxListe = func() (string, error) {
		t.Error("etiketlenen port önbellekte — semanage beklenmiyordu")
		return "", nil
	}
	onceki := s.rSayac
	if !kapiUzlastir(1000) || s.rSayac != onceki+1 {
		t.Fatalf("kendi eklediği etiket de yüklenene dek reload beklemeli: %d → %d", onceki, s.rSayac)
	}
}
