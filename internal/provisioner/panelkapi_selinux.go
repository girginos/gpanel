package provisioner

// SELinux port etiketleri — kapı portları (ADR-GP-001).
//
// 🔴 nginx (httpd_t) yalnız http_port_t etiketli portlara bind edebilir; rastgele kapı
// portları varsayılan politikada unreserved_port_t'dir. Enforcing sunucuda (AlmaLinux 10
// varsayılanı) etiketsiz bir porta yazılan listen'da reload sessizce düşer, sonraki nginx
// restart'ı ise HİÇ açılmaz → tüm siteler kapanır. `nginx -t` bunu yakalamaz. Bu yüzden
// portlar listen'a yazılmadan ÖNCE etiketlenir (ayrıntı: portyonetim/selinux.go).
//
// 🔴 AÇILIŞ SÜRESİ: semanage her çağrıda saniyeler sürer ve Heal ListenAndServe'den ÖNCE
// koşar — güncelleyicinin sağlık penceresi aşılırsa sağlıklı güncelleme geri alınırdı.
// Bu yüzden CLI (kapi kur/gecis/rol-port/selinux) portları panel başlamadan etiketler ve
// doğrulanan portlar /run önbelleğine yazılır; açılış yalnız önbellekte olmayan portlar
// için semanage koşturur.
//
// 🔴 ÖNBELLEK İDDİADIR, ÖLÇÜM DEĞİL: etiket aynı önyüklemede panel dışında silinebilir
// (semanage port -d / import). Bu yüzden (1) CLI önbelleğe HİÇ güvenmez — her çağrıda
// semanage listesinden ölçer, önbelleği ölçülen gerçeğe göre yeniden yazar (dogrula'nın
// önerdiği `kapi selinux` onarımı "hazır" deyip hiçbir şey yapmadan geçmesin; güncelleyici
// de her güncellemede restart'tan önce onu koşturur); (2) nginx'in bağlayamadığı port
// önbellekten düşer (kapiEtiketUnut), sonraki Heal onu semanage ile yeniden doğrular.
// Yalnız açılış Heal'i hız için önbelleğe güvenir.

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/kapi"
	"girginospanel/internal/portyonetim"
)

// Dikişler (testler SELinux'u kapalı varsayar).
var (
	kapiSELinuxModu   = portyonetim.SELinuxModu
	kapiSELinuxListe  = portyonetim.SELinuxPortListesi
	kapiSELinuxEtiket = portyonetim.SELinuxHttpEtiketle
	// kapiEtiketli: http_port_t olduğu doğrulanan portlar. kapiMu altında kullanılır
	// (kapiUzlastir, kapiNginxUygula, KapiSELinuxHazirla); her uzlaştırmada semanage koşmasın.
	kapiEtiketli = map[int]bool{}
	// kapiSELinuxOnbellek: bu önyüklemede doğrulanan portlar (tmpfs — yeniden başlatmada
	// silinir, etiket değişmiş olabilir). CLI ile panel paylaşır. "<yol>.yukle" işareti:
	// etiket eklendi ama nginx henüz yeniden yüklenmedi — sonraki Heal yüklemeli
	// (kapiYukleIsaretiAl / kapiYukleIsaretiTuket).
	kapiSELinuxOnbellek = "/run/girginospanel/kapi-selinux"
	kapiOnbellekOkundu  bool
)

// kapiPortlariEtiketle: nginx'in bind edeceği kapı portlarını SELinux'ta http_port_t yapar.
//   - tamam=false → Enforcing'de etiketlenemeyen port var: listen YAZILMAMALI.
//     Permissive'de etiketleme hatası engellemez (bind serbest), yalnız günlüğe düşer.
//   - yeni=true → bu çağrıda etiket eklendi: dosyalar değişmese de reload gerekir
//     ("yeniden yükle" işareti de bırakılır: reload bu turda düşerse sonraki Heal yükler).
//
// Başka bir servise ayrılmış portun türü DEĞİŞTİRİLMEZ (o servisi bozabilirdi); seçim
// zaten böyle portları eler (portyonetim.KapiPortDenetleyici), elle yazılan ayar için
// günlük operatöre portu değiştirmesini söyler.
func kapiPortlariEtiketle(portlar []int) (tamam, yeni bool) {
	return kapiPortlariEtiketleOlc(portlar, false)
}

// kapiPortlariEtiketleOlc: olc=true → bellek/önbellek "etiketli" dese de HER port semanage
// listesinden ölçülür (CLI onarımı); ölçümde http_port_t olmadığı görülen port bellekten ve
// önbellekten DÜŞER (yeniden etiketlenirse geri girer). Liste okunamazsa önbelleğe dokunulmaz
// (ölçülemeyen durum için ne ekleme ne silme).
func kapiPortlariEtiketleOlc(portlar []int, olc bool) (tamam, yeni bool) {
	mod := kapiSELinuxModu()
	if mod == "" {
		return true, false
	}
	if !kapiOnbellekOkundu {
		kapiOnbellekOkundu = true
		for _, p := range kapiOnbellekOku() {
			kapiEtiketli[p] = true
		}
	}
	var eksik []int
	for _, p := range portlar {
		if olc || !kapiEtiketli[p] {
			eksik = append(eksik, p)
		}
	}
	if len(eksik) == 0 {
		return true, false
	}
	zorunlu := mod == "enforcing"
	liste, err := kapiSELinuxListe()
	if err != nil {
		log.Printf("🔴 kapi: SELinux %s ama port etiketleri okunamadı: %v", mod, err)
		return !zorunlu, false
	}
	tamam = true
	degisti := false
	for _, p := range eksik {
		http, baska := portyonetim.SELinuxPortDurumu(liste, p)
		if http {
			if !kapiEtiketli[p] {
				kapiEtiketli[p], degisti = true, true
			}
			continue
		}
		// Ölçüldü: http_port_t DEĞİL. Bellek/önbellek "etiketli" diyorsa yanlıştır (etiket
		// panel dışında silinmiş) — düşer; aşağıda yeniden etiketlenirse geri girer.
		if kapiEtiketli[p] {
			delete(kapiEtiketli, p)
			degisti = true
		}
		if baska != "" {
			log.Printf("🔴 kapi: :%d SELinux'ta %s servisine ayrılmış — http_port_t yapılmadı; bu kapı portunu değiştirin", p, baska)
			tamam = tamam && !zorunlu
			continue
		}
		if e := kapiSELinuxEtiket(p); e != nil {
			log.Printf("🔴 kapi: :%d SELinux http_port_t yapılamadı: %v", p, e)
			tamam = tamam && !zorunlu
			continue
		}
		log.Printf("kapi: :%d SELinux'ta http_port_t olarak etiketlendi (nginx bind edebilsin)", p)
		kapiEtiketli[p], degisti, yeni = true, true, true
	}
	if degisti {
		kapiOnbellekYaz()
	}
	if yeni {
		kapiYukleIsaretiBirak()
	}
	return tamam, yeni
}

// kapiEtiketUnut: nginx'in bağlayamadığı portları "doğrulanmış" kümesinden (ve önbellekten)
// çıkarır — sonraki Heal onları semanage ile yeniden ölçer. kapiMu altında.
func kapiEtiketUnut(portlar []int) {
	degisti := false
	for _, p := range portlar {
		if kapiEtiketli[p] {
			delete(kapiEtiketli, p)
			degisti = true
		}
	}
	if degisti {
		kapiOnbellekYaz()
	}
}

func kapiOnbellekOku() []int {
	b, err := os.ReadFile(kapiSELinuxOnbellek)
	if err != nil {
		return nil
	}
	var out []int
	for _, f := range strings.Fields(string(b)) {
		if p, err := strconv.Atoi(f); err == nil && p > 0 && p < 65536 {
			out = append(out, p)
		}
	}
	return out
}

func kapiOnbellekYaz() {
	portlar := make([]int, 0, len(kapiEtiketli))
	for p := range kapiEtiketli {
		portlar = append(portlar, p)
	}
	slices.Sort(portlar)
	var b strings.Builder
	for _, p := range portlar {
		b.WriteString(strconv.Itoa(p) + "\n")
	}
	if err := os.MkdirAll(filepath.Dir(kapiSELinuxOnbellek), 0o755); err == nil {
		_ = os.WriteFile(kapiSELinuxOnbellek, []byte(b.String()), 0o644)
	}
}

// kapiYukleIsaretiBirak: "etiket eklendi, nginx yeniden yüklenmeli" işareti.
func kapiYukleIsaretiBirak() {
	if err := os.MkdirAll(filepath.Dir(kapiSELinuxOnbellek), 0o755); err == nil {
		_ = os.WriteFile(kapiSELinuxOnbellek+".yukle", nil, 0o644)
	}
}

// kapiYukleIsaretiAl: bekleyen "yeniden yükle" işareti var mı? Varsa ".isleniyor" adına
// taşınır: bu Heal sürerken CLI'ın bırakacağı YENİ işaret ayrı dosyada kalır, silinmez.
// Alınan işaret YALNIZ başarılı uygulamadan sonra silinir (kapiYukleIsaretiTuket) — taban
// `nginx -t`, reload ya da port ölçümü düşerse (ya da panel bu arada yeniden başlarsa)
// sonraki Heal yine yükler; yoksa etiketlenen portlar ilgisiz bir reload'a dek bağlanmazdı.
func kapiYukleIsaretiAl() bool {
	isaret := kapiSELinuxOnbellek + ".yukle"
	if os.Rename(isaret, isaret+".isleniyor") == nil {
		return true
	}
	_, err := os.Stat(isaret + ".isleniyor")
	return err == nil
}

// kapiYukleIsaretiTuket: nginx yeni etiketli portlarla BAŞARIYLA yüklendi (ya da çalışmıyor
// ve başlarken bağlayacak) — alınan işaret silinir.
func kapiYukleIsaretiTuket() {
	_ = os.Remove(kapiSELinuxOnbellek + ".yukle.isleniyor")
}

// KapiSELinuxHazirla: CLI (`girginospanel-server kapi selinux`; kur, gecis ve rol-port da
// çağırır) — kapi.json'daki kapı portlarını panel başlamadan ÖNCE http_port_t yapar ve
// önbelleğe yazar; panel açılışı semanage koşturmaz. Yeni etiket eklendiyse sonraki Heal
// nginx'i yeniden yüklesin diye işaret bırakır.
//
// 🔴 ONARIM KOMUTU ÖLÇER: önbelleğe güvenmez (etiket panel dışında silinmiş olabilir —
// dogrula tam bunu yakalayıp bu komutu önerir); her port semanage listesinden doğrulanır,
// önbellek ölçülen duruma getirilir. CLI açılış sağlık penceresinde koşmaz: süre sorunu yok.
// Dönüş: 0 etiketli ya da gerekmiyor, 1 Enforcing'de etiketlenemedi/doğrulanamadı (panel
// kapı düzenini uygulamaz — güvenli), 3 kapı modu yapılandırılmamış.
func KapiSELinuxHazirla() int {
	a, err := kapi.Oku(kapiAyarYolu)
	if errors.Is(err, kapi.ErrYok) {
		return 3
	}
	if err != nil {
		log.Printf("🔴 kapi: kapi.json okunamadı: %v", err)
		return 1
	}
	if kapiSELinuxModu() == "" {
		return 0
	}
	kapiMu.Lock()
	defer kapiMu.Unlock()
	if tamam, _ := kapiPortlariEtiketleOlc(kapi.SiraliPortlar(a.PortHaritasi(time.Now().Unix())), true); !tamam {
		return 1
	}
	return 0
}
