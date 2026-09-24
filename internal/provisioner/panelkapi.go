package provisioner

// Panel kapıları (ADR-GP-001) — nginx uzlaştırması + çalışma-zamanı ölçümü.
// Saf dönüşümler internal/kapi'de (birim testli); burada yalnız G/Ç ve sıralama.
//
// 🔴 İKİ AŞAMA, İKİNCİSİ KORUMALI:
//  1. EKLEYİCİ — rol listen'ları + kapı başlığı + map (+ panelhost catchall).
//     Başarısızsa her şey geri alınır, kimse etkilenmez.
//  2. KALDIRMA — eski/beklenmeyen genel listen'lar. YALNIZ admin kapısı gerçekten
//     dinliyorsa; eski portlar ayrıca süre dolup admin yeni kapıdan girmeden
//     kapanmaz (kapi.Ayar.EskiAcik). Son çalışan portu kapatmak = herkesi kilitlemek.
//
// 🔴 ÖLÇ, İDDİA ETME: zorlama "dosyaları yazdık" diye açık KALMAZ. Backend ayağa
// kalktıktan sonra her rol portuna GERÇEKTEN nginx üzerinden istek atılır
// (KapiDogrulaBaslat); zincirin bir halkası (listen, map, başlık, reload,
// X-Gosp-Proxy güveni) kopuksa zorlama KAPANIR → eski davranış, kilitlenme yok.
// Yanıt kanıtlıdır (kapi.OlcumKaniti): portu nginx dışında bir süreç tutuyorsa ölçüm geçmez.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"girginospanel/internal/httpx"
	"girginospanel/internal/kapi"
	"girginospanel/internal/portyonetim"
)

// KapiGenelPortUzlastir: her başarılı uzlaştırmadan sonra panelin genel portu (genel)
// ve canlı genel portları (canli) ile çağrılır; main bağlar. Panelin genel portunda
// barınan başka şeyleri (uygulama subdomain vhost'ları, GitHub webhook adresleri)
// canlı olmayan porttan genel porta taşır. DURUMSUZDUR: bir taşıma başarısız olursa
// sonraki uzlaştırma yeniden dener. Açılıştaki ilk uzlaştırma da yakalasın diye
// provisioner.Init'ten ÖNCE bağlanmalıdır.
var KapiGenelPortUzlastir func(genel int, canli map[int]bool)

// Dikişler: testler geçici dizin + sahte nginx/firewall ile koşar.
var (
	kapiVhostYolu    = panelVhostPath
	kapiCatchallYolu = "/etc/nginx/conf.d/_panel_catchall.conf"
	kapiAcmeYolu     = "/etc/nginx/conf.d/_panel_acme.conf" // panelhost.AcmeVhostPath
	kapiMapYolu      = kapi.MapYolu
	kapiAyarYolu     = kapi.AyarYolu
	kapiNginxKomut   = func(ad string, arg ...string) ([]byte, error) {
		return exec.Command(ad, arg...).CombinedOutput()
	}
	kapiFwAc    = portyonetim.FwPortAc
	kapiFwKapat = portyonetim.FwPortKapat
	// Portu NGINX'in dinlediği soket sahibinden doğrulanır: aynı portu başka bir süreç
	// tutuyorsa (bind yarışı, uygulanamamış reload) "admin kapısı dinliyor" deyip eski
	// portları kaldırmak son çalışan kapıyı kapatmaktı. reload sinyali asenkron işlenir:
	// nginx yeni soketi biraz sonra bağlayabilir.
	kapiPortDinliyor = func(p int) bool {
		for i := 0; i < 6; i++ {
			out, err := exec.Command("ss", "-Hltnp", fmt.Sprintf("sport = :%d", p)).Output()
			if err == nil && yalnizNginxDinliyor(string(out)) {
				return true
			}
			time.Sleep(500 * time.Millisecond)
		}
		return false
	}
	kapiProxyGizli = httpx.ProxySecret
	kapiMasaustu   = kapi.MasaustuModu
	kapiVhostIzin  = gizliVhostIzniSertlestir // gerçek yola chmod/chgrp — testte no-op
	// kapiOzelIzin: kapı haritası rol etiketlerini ("PORT admin;") taşır → _panel.conf gibi
	// 0640 root:nginx (yerel kiracılar yönetici portunu okuyamasın; nginx master root'tur).
	kapiOzelIzin = func(yol string) {
		_ = os.Chmod(yol, 0o640)
		_ = exec.Command("chgrp", "nginx", yol).Run()
	}
	kapiNginxAktif = func() bool {
		return exec.Command("systemctl", "is-active", "--quiet", "nginx").Run() == nil
	}
)

// ssSurecRe: `ss -p` çıktısındaki süreç adları: users:(("nginx",pid=1,fd=6),...).
var ssSurecRe = regexp.MustCompile(`\("([^"]*)",pid=`)

// yalnizNginxDinliyor: `ss -Hltnp "sport = :P"` çıktısında en az bir dinleyici var ve
// HEPSİ nginx'e mi ait? Süreç bilgisi yoksa (yetkisiz ss) kanıt yok → false.
func yalnizNginxDinliyor(ss string) bool {
	n := 0
	for _, ln := range strings.Split(strings.TrimSpace(ss), "\n") {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		adlar := ssSurecRe.FindAllStringSubmatch(ln, -1)
		if len(adlar) == 0 {
			return false
		}
		for _, m := range adlar {
			if m[1] != "nginx" {
				return false
			}
		}
		n++
	}
	return n > 0
}

// kapiMu: açılış uzlaştırması ile periyodik bakım aynı anda koşmasın.
var kapiMu sync.Mutex

// HealPanelKapiOnStartup: açılışta nginx'i kapı ayarına uyumlar ve — dosyalar tam ve
// doğrulanmışsa — zorlamayı İYİMSER açar (ilk istekten itibaren kural geçerli olsun,
// yeniden başlatma sonrası saldırı penceresi kalmasın). Asıl kanıt KapiDogrulaBaslat'ın
// ölçümüdür; zincir kopuksa saniyeler içinde kapatır.
// HealPanelProxyTrustOnStartup'tan SONRA çağrılmalı (proxy gizlisine dayanır).
func HealPanelKapiOnStartup() {
	kapiMu.Lock()
	defer kapiMu.Unlock()
	kapiZorlamaYaz(kapiUzlastir(time.Now().Unix()), "açılış uzlaştırması — uçtan uca ölçüm doğrulayacak")
	kapi.ZincirKopuk = kapiZincirKopuk
}

// kapiZincirTetik / kapiZincirAra: kopuk zincirde HER istek sinyal verir; onarım
// sinyal başına değil, en fazla kapiZincirAra'da bir koşar (kanıtsız istek yağdıran
// biri de yalnız bu kadarını tetikleyebilir).
var (
	kapiZincirTetik atomic.Bool
	kapiZincirAra   = 30 * time.Second
)

// kapiZincirKopuk: zorlama açıkken kapısı kanıtlanamayan istek geldi (kapi.ZincirKopuk).
// 15 dakikalık bakımı beklemeden zinciri yeniden uzlaştırır ve ölçer: onarılabiliyorsa
// onarır, onarılamıyorsa zorlamayı kapatır (eski davranış — kimse kilitli kalmaz).
func kapiZincirKopuk() {
	if !kapiZincirTetik.CompareAndSwap(false, true) {
		return
	}
	ara := kapiZincirAra
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("🔴 kapi zincir onarımı panikledi: %v", r)
			}
			time.AfterFunc(ara, func() { kapiZincirTetik.Store(false) })
		}()
		kapiMu.Lock()
		acilabilir := kapiUzlastir(time.Now().Unix())
		kapiMu.Unlock()
		if !acilabilir {
			kapiZorlamaYaz(false, "zincir kopuk")
			return
		}
		kapiOlcVeAyarla(context.Background(), 3)
	}()
}

// KapiSimdiUygula: kapı ayarı panelden değiştikten sonra nginx'i HEMEN uyumlar ve
// zinciri uçtan uca ölçer (15 dakikalık bakımı beklemeden). Zincir kurulamazsa zorlama
// kapanır (kilitlenme yok, eski davranış) ve hata döner.
//
// İstek bağlamından KOPUK koşar: istemci beklemeyi bırakırsa (sekme kapandı, 30 sn
// istemci zaman aşımı) yarıda kesilen ölçüm "zincir kopuk" sanılıp rol ayrımını
// kapatmamalı; ayar zaten yazılmıştır, uygulama da tamamlanmalıdır.
func KapiSimdiUygula(ctx context.Context) error {
	ctx, iptal := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer iptal()
	kapiMu.Lock()
	acilabilir, uygulandi := kapiUzlastirDetay(time.Now().Unix())
	kapiMu.Unlock()
	if !acilabilir {
		kapiZorlamaYaz(false, "uzlaştırma")
		if uygulandi {
			return &kapi.UygulamaHatasi{Uygulandi: true, Mesaj: "değişiklik nginx'e uygulandı ama kapı zinciri kurulamadı — kapı kuralları devre dışı (panel günlüğüne bakın)"}
		}
		return &kapi.UygulamaHatasi{Mesaj: "nginx kapı ayarına uyumlanamadı — kapı kuralları devre dışı (panel günlüğüne bakın)"}
	}
	switch kapiOlcVeAyarla(ctx, 3) {
	case olcumTamam:
		return nil
	case olcumBelirsiz:
		select {
		case kapiYenidenOlc <- struct{}{}:
		default:
		}
		return &kapi.UygulamaHatasi{Uygulandi: true, Mesaj: "kapı zinciri şu an ölçülemedi (geçici hata) — kapı kuralları önceki durumunda; ölçüm kısa aralıkla yinelenecek"}
	default:
		return &kapi.UygulamaHatasi{Uygulandi: true, Mesaj: "kapı zinciri ölçülemedi — kapı kuralları devre dışı (panel günlüğüne bakın)"}
	}
}

// kapiUzlastir: nginx'i ayara getirir. Dönüş: zorlama açılabilir mi (ayar geçerli,
// proxy gizlisi kurulu, gizli-bağlı başlık her çapada, nginx doğrulandı).
func kapiUzlastir(simdi int64) bool {
	acilabilir, _ := kapiUzlastirDetay(simdi)
	return acilabilir
}

// kapiUzlastirDetay: kapiUzlastir + değişikliğin nginx'e UYGULANIP uygulanmadığı (aşama 1
// yazıldı ve yüklendi mi). Arayüz "uygulandı ama doğrulanamadı" ile "hiç uygulanamadı
// (nginx eski hâlinde)" durumlarını bununla ayırır.
func kapiUzlastirDetay(simdi int64) (acilabilir, uygulandi bool) {
	origB, err := os.ReadFile(kapiVhostYolu)
	if err != nil {
		return false, false // panel bu host'ta kurulu değil
	}
	orig := string(origB)
	a, err := kapi.Oku(kapiAyarYolu)
	if err != nil {
		if !errors.Is(err, kapi.ErrYok) {
			log.Printf("🔴 kapi: %v — kapı modu KAPALI kalıyor (eski davranış)", err)
		}
		kapiKapaliTemizle(orig)
		return false, false
	}
	// Başlık değeri gizliyi taşır (kapi.KapiSatiri); gizli yoksa kapı kanıtlanamaz.
	// Hiçbir dosyaya dokunulmaz: yarım uygulanmış kapı düzeni, uygulanmamıştan kötüdür.
	gizli := kapiProxyGizli()
	if gizli == "" {
		log.Printf("🔴 kapi: proxy gizlisi kurulu değil — kapı başlığı yazılamaz, zorlama KAPALI")
		return false, false
	}

	// Kapı portları listen'a yazılmadan ÖNCE SELinux'ta http_port_t olmalı (bkz.
	// kapiPortlariEtiketle): Enforcing'de etiketsiz port = restart'ta açılmayan nginx.
	portlar := kapi.SiraliPortlar(a.PortHaritasi(simdi))
	etiketTamam, yeniEtiket := kapiPortlariEtiketle(portlar)
	if !etiketTamam {
		log.Printf("🔴 kapi: SELinux Enforcing ve kapı portları http_port_t yapılamadı — nginx'e YAZILMADI (restart'ta açılmazdı), zorlama KAPALI")
		return false, false
	}
	if kapiDisaridanEtiketlendi() {
		yeniEtiket = true
	}

	// ── Aşama 1: EKLEYİCİ ──────────────────────────────────────────────
	yeni, err := kapi.Uzlastir(orig, a, simdi, kapi.Secenek{Gizli: gizli, AdminLoopback: kapiMasaustu()})
	if err != nil {
		log.Printf("🔴 kapi: _panel.conf uzlaştırılamadı: %v — zorlama KAPALI", err)
		return false, false
	}
	// Harita silinmiş ama vhost hâlâ $gp_kapi'ye atıf yapıyorsa `nginx -t` GLOBAL düşer;
	// taban kontrolü onarımı engellemesin diye harita yazımı onarım sayılır.
	_, haritaErr := os.Stat(kapiMapYolu)
	haritaOnarim := errors.Is(haritaErr, os.ErrNotExist) && strings.Contains(orig, "$gp_kapi")
	dosyalar := append([]nginxDosya{
		{yol: kapiVhostYolu, eski: origB, yeni: []byte(yeni)},
		kapiMapDosyasi(kapi.MapIcerigi(a, simdi, kapi.YerelPortlar(yeni, a, simdi)), haritaOnarim),
	}, kapiYanDosyalar(yeni, a)...)
	// Yeni etiketlenen port: dosyalar değişmese de reload gerekir (önceden bind edilemeyen
	// port şimdi bağlansın).
	if !kapiNginxUygula(dosyalar, portlar, yeniEtiket) {
		return false, false
	}
	kapiVhostIzin() // yeniden yazılan dosya da 0640 root:nginx kalsın
	kapiFwRolPortlariniAc(yeni, a, simdi)

	// Bayi/müşteri kapısı dışarıda dinlenmiyorsa (yalnız loopback izi olan porta atanmış —
	// ör. elle düzenlenmiş kapi.json) eski/beklenmeyen genel portları KALDIRMA: o rolün
	// kullanıcılarının son yolu olabilir. Kapı kanıtlı ama dışarıdan erişilemez → zorlama da
	// açılmaz (eski davranış, kilitlenme yok).
	if kapali := kapiDisaKapaliRoller(yeni, a); len(kapali) > 0 {
		log.Printf("🔴 kapi: %s kapısı dışarıda dinlenmiyor (yalnız loopback izi) — kaldırma yapılmadı, zorlama KAPALI", strings.Join(kapali, ", "))
		return false, true
	}

	// ── Aşama 2: KALDIRMA (korumalı) ───────────────────────────────────
	son := yeni
	if !kapiPortDinliyor(a.Admin) {
		log.Printf("🔴 kapi: admin kapısı :%d dinlemiyor — eski/beklenmeyen portlar KALDIRILMADI (kilitlenme kilidi)", a.Admin)
	} else {
		tut := map[int]bool{}
		for p := range a.PortHaritasi(simdi) {
			tut[p] = true
		}
		if kaldirilmis, silinen := kapi.GenelListenKaldir(yeni, tut); len(silinen) > 0 {
			d := append([]nginxDosya{
				{yol: kapiVhostYolu, eski: []byte(yeni), yeni: []byte(kaldirilmis)},
				kapiMapDosyasi(kapi.MapIcerigi(a, simdi, kapi.YerelPortlar(kaldirilmis, a, simdi)), false),
			}, kapiYanDosyalar(kaldirilmis, a)...)
			if kapiNginxUygula(d, portlar, false) {
				kapiVhostIzin()
				son = kaldirilmis
				for _, p := range silinen {
					if e := kapiFwKapat(p); e != nil {
						log.Printf("kapi: firewall :%d kapatılamadı: %v", p, e)
					}
				}
				log.Printf("kapi: kapı dışı genel portlar kaldırıldı: %v", silinen)
			}
		}
		if len(a.EskiPortlar) > 0 && !a.EskiAcik(simdi) {
			dusuldu := false
			// Koşul TAZE ayarda yeniden denetlenir: bu uzlaştırma sürerken panelden admin
			// portu değişmiş olabilir (yeni eski port eklenir, kanıt sıfırlanır); bayat
			// kopyaya göre silmek, doğrulanmamış yeni admin portuyla kilitlenme demekti.
			if e := kapi.Guncelle(kapiAyarYolu, func(x *kapi.Ayar) error {
				if len(x.EskiPortlar) > 0 && !x.EskiAcik(simdi) {
					x.EskiPortlar, x.EskiBitis = nil, 0
					dusuldu = true
				}
				return nil
			}); e != nil {
				log.Printf("kapi: eski portlar ayardan düşülemedi: %v", e)
			} else if dusuldu {
				log.Printf("kapi: geçiş tamamlandı — eski portlar kapandı")
			}
		}
	}

	kapiGenelPortuYay(son, a)

	if !kapi.BaslikTamMi(son, gizli) {
		log.Printf("🔴 kapi: kapı başlığı her proxy location'ında değil — zorlama KAPALI")
		return false, true
	}
	return true, true
}

// kapiDisaKapaliRoller: conf'ta GENEL dinlenmeyen bayi/müşteri kapıları ("bayi :P").
// Yönetici kapısı sayılmaz: masaüstü modunda (ve operatörün erisim ile kapattığı web
// modunda) yalnız loopback'te dinlemesi bilinçlidir.
func kapiDisaKapaliRoller(conf string, a *kapi.Ayar) []string {
	genel := kapi.GenelPortlar(conf)
	var out []string
	for _, rol := range []string{kapi.Bayi, kapi.Musteri} {
		if p := a.RolPortu(rol); !slices.Contains(genel, p) {
			out = append(out, fmt.Sprintf("%s :%d", rol, p))
		}
	}
	return out
}

// kapiKapaliTemizle: kapı modu kapalıyken kapı izlerini temizler. Hiç iz yoksa
// DOKUNMAZ (geçişi yapılmamış binlerce sunucuda sıfır değişiklik, sıfır reload).
// Rol listen'larına DOKUNMAZ: mod kapatıldığında kullanıcılar zaten o portlardadır;
// onları kaldırmak kilitlenmedir. Zorlama kapalı → her portta eski davranış.
func kapiKapaliTemizle(conf string) {
	temiz := kapi.KapiBasliginiSil(conf)
	mapB, mapErr := os.ReadFile(kapiMapYolu)
	if temiz == conf && mapErr != nil {
		return
	}
	// Harita silinmiş ama başlık satırları $gp_kapi'ye atıf yapıyorsa `nginx -t` zaten
	// düşüktür: satırları kaldırmak onarımdır (taban kontrolü engellemesin).
	d := []nginxDosya{{yol: kapiVhostYolu, eski: []byte(conf), yeni: []byte(temiz), onarim: mapErr != nil}}
	if mapErr == nil {
		// Map kalır ama boş varsayılanla: $gp_kapi tanımlı kalsın (başka bir yerde
		// atıf kalmışsa `nginx -t` global düşmesin).
		d = append(d, nginxDosya{yol: kapiMapYolu, eski: mapB, yeni: []byte(kapi.MapIcerigi(nil, 0, nil)), ozel: true})
	}
	if kapiNginxUygula(d, nil, false) {
		kapiVhostIzin()
	}
}

// nginxDosya: uygulanacak bir nginx dosya değişikliği. eski == nil → dosya yoktu.
// ozel: dosya yazıldıktan sonra kapiOzelIzin ile 0640 root:nginx yapılır.
// onarim: bu değişiklik ÖNCEDEN bozuk bir `nginx -t`'yi düzeltmesi beklenen onarımdır
// (silinmiş kapı haritası) — taban kontrolü onu engellemez; yazımdan sonraki `nginx -t`
// yine geçmezse her şey geri alınır.
type nginxDosya struct {
	yol          string
	eski, yeni   []byte
	ozel, onarim bool
}

func kapiMapDosyasi(icerik string, onarim bool) nginxDosya {
	eski, err := os.ReadFile(kapiMapYolu)
	if err != nil {
		eski = nil
	}
	return nginxDosya{yol: kapiMapYolu, eski: eski, yeni: []byte(icerik), ozel: true, onarim: onarim}
}

// kapiYanDosyalar: panel vhost'u conf olacakken onunla BİRLİKTE uyumlanması gereken
// panelhost dosyaları (yalnız varsa): catchall'ın listen'ları genel portlara, :80
// mini vhost'unun HTTPS yönlendirmesi genel panel portuna. Hepsi tek kapiNginxUygula
// işleminde yazılır → biri düşerse hepsi geri alınır.
func kapiYanDosyalar(conf string, a *kapi.Ayar) []nginxDosya {
	var out []nginxDosya
	if eski, err := os.ReadFile(kapiCatchallYolu); err == nil {
		ipv6 := kapi.IPv6Etkin(conf) || kapi.IPv6Etkin(string(eski))
		yeni := kapi.CatchallUzlastir(string(eski), kapi.GenelPortlar(conf), ipv6)
		out = append(out, nginxDosya{yol: kapiCatchallYolu, eski: eski, yeni: []byte(yeni)})
	}
	if eski, err := os.ReadFile(kapiAcmeYolu); err == nil {
		yeni := kapi.AcmeYonlendirmeUzlastir(string(eski), kapi.GenelPanelPortu(conf, a))
		out = append(out, nginxDosya{yol: kapiAcmeYolu, eski: eski, yeni: []byte(yeni)})
	}
	return out
}

// kapiGenelPortuYay: genel panel portunu ona bağlı dış bileşenlere (KapiGenelPortUzlastir)
// bildirir. Genel port yoksa (hepsi loopback) dokunulmaz.
func kapiGenelPortuYay(conf string, a *kapi.Ayar) {
	if KapiGenelPortUzlastir == nil {
		return
	}
	genel := kapi.GenelPanelPortu(conf, a)
	if genel == 0 {
		return
	}
	canli := map[int]bool{}
	for _, p := range kapi.GenelPortlar(conf) {
		canli[p] = true
	}
	KapiGenelPortUzlastir(genel, canli)
}

// kapiNginxUygula: değişen dosyaları yazar, `nginx -t` ile doğrular, başarısızsa
// HEPSİNİ geri alır; başarılıysa reload eder ve dinlenecek portların GERÇEKTEN nginx'e
// bağlandığını ölçer — bağlanmadıysa yine hepsini geri alır. Hiçbiri değişmediyse (ve
// zorlaReload yoksa) dokunmaz.
//
// 🔴 TABAN KONTROLÜ: `nginx -t` GLOBALDİR. nginx değişiklikten ÖNCE zaten bozuksa
// (başka bir vhost) hatanın bizim değişikliğimizden olup olmadığı bilinemez; o
// durumda yazmayız — yoksa sağlam değişikliği geri alıp yanlış karar verirdik.
//
// 🔴 reload'un 0 dönmesi uygulandığı anlamına GELMEZ: `nginx -s reload` yalnız sinyal
// gönderir; master yeni portu bind edemezse (SELinux, port çakışması) SESSİZCE eski
// yapılandırmada kalır — dosyadaki yapılandırmayla bir sonraki nginx restart'ı ise hiç
// açılmaz (tüm siteler). Bu yüzden portlar ölçülür, uygulanamayan değişiklik geri alınır.
func kapiNginxUygula(dosyalar []nginxDosya, dinlenecek []int, zorlaReload bool) bool {
	var degisen []nginxDosya
	for _, d := range dosyalar {
		if !bytes.Equal(d.eski, d.yeni) {
			degisen = append(degisen, d)
			continue
		}
		// İçerik aynı ama izin geniş (önceki sürüm haritayı 0644 yazdı): yalnız yazımda
		// daraltılsaydı rol etiketleri (yönetici portu) yerel kiracılara açık KALIRDI.
		if d.ozel && d.eski != nil {
			if fi, err := os.Stat(d.yol); err == nil && fi.Mode().Perm() != 0o640 {
				kapiOzelIzin(d.yol)
			}
		}
	}
	if len(degisen) == 0 && !zorlaReload {
		return true
	}
	if o, e := kapiNginxKomut("nginx", "-t"); e != nil {
		if !slices.ContainsFunc(degisen, func(d nginxDosya) bool { return d.onarim }) {
			log.Printf("🔴 kapi: nginx -t değişiklikten ÖNCE zaten başarısız — uygulanmadı: %s", kirpKapi(o))
			return false
		}
		log.Printf("kapi: nginx -t değişiklikten önce başarısız (silinmiş kapı haritası?) — onarım deneniyor: %s", kirpKapi(o))
	}
	geriAl := func() {
		for _, d := range degisen {
			if d.eski == nil {
				_ = os.Remove(d.yol)
			} else {
				_ = os.WriteFile(d.yol, d.eski, 0o644)
			}
		}
	}
	for _, d := range degisen {
		izin := os.FileMode(0o644)
		if d.ozel {
			izin = 0o640 // yeni dosya baştan kısıtlı doğsun
		}
		if e := os.WriteFile(d.yol, d.yeni, izin); e != nil {
			geriAl()
			log.Printf("🔴 kapi: %s yazılamadı: %v — geri alındı", d.yol, e)
			return false
		}
		if d.ozel {
			kapiOzelIzin(d.yol) // var olan dosyanın izni WriteFile'la değişmez
		}
	}
	if len(degisen) > 0 {
		if o, e := kapiNginxKomut("nginx", "-t"); e != nil {
			geriAl()
			log.Printf("🔴 kapi: nginx -t başarısız, geri alındı: %s", kirpKapi(o))
			return false
		}
	}
	// nginx durmuşsa (taze kurulumda panel nginx'ten önce kalkabilir) reload düşer; ama
	// yapılandırma doğrulandı — geri almak doğru değişikliği atmak olurdu. nginx
	// kalktığında bu dosyalarla başlar.
	if !kapiNginxAktif() {
		log.Printf("kapi: nginx çalışmıyor — doğrulanan yapılandırma nginx başlayınca geçerli olur")
		return true
	}
	if o, e := kapiNginxKomut("systemctl", "reload", "nginx"); e != nil {
		geriAl()
		_, _ = kapiNginxKomut("systemctl", "reload", "nginx")
		log.Printf("🔴 kapi: nginx reload başarısız, geri alındı: %s", kirpKapi(o))
		return false
	}
	var dinlenmeyen []int
	for _, p := range dinlenecek {
		if !kapiPortDinliyor(p) {
			dinlenmeyen = append(dinlenmeyen, p)
		}
	}
	if len(dinlenmeyen) > 0 {
		if len(degisen) > 0 {
			geriAl()
			_, _ = kapiNginxKomut("systemctl", "reload", "nginx")
		}
		log.Printf("🔴 kapi: nginx yapılandırmayı YÜKLEYEMEDİ — :%v nginx'e bağlı değil (SELinux etiketi? port çakışması?) — değişiklik geri alındı", dinlenmeyen)
		return false
	}
	return true
}

// kapiFwRolPortlariniAc: GENEL dinlenen rol portlarını (+ açık eski portları) açar.
// erisim'in 127.0.0.1'e çektiği bir porta DELİK AÇMAZ (operatör onu dışarıya
// bilinçli kapattı; firewall'da açmak o seçimi sessizce geri alırdı).
func kapiFwRolPortlariniAc(conf string, a *kapi.Ayar, simdi int64) {
	genel := map[int]bool{}
	for _, p := range kapi.GenelPortlar(conf) {
		genel[p] = true
	}
	for p := range a.PortHaritasi(simdi) {
		if !genel[p] {
			continue
		}
		if e := kapiFwAc(p); e != nil {
			log.Printf("🔴 kapi: firewall :%d açılamadı: %v", p, e)
		}
	}
}

func kirpKapi(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

// ── Çalışma zamanı ölçümü ─────────────────────────────────────────────────

// kapiTipOlcucu: nginx ÜZERİNDEN bir porttaki kapı tipini ölçer. Testlerde değişir.
var kapiTipOlcucu = kapiTipOlc

// kapiOlcAra: ölçüm denemeleri arası bekleme (backend henüz dinlemiyor olabilir).
var kapiOlcAra = 3 * time.Second

// KapiDogrulaBaslat: backend ayağa kalktıktan sonra kapı zincirini uçtan uca ölçer
// ve sonucu zorlamaya yansıtır; sonra her `aralik`ta uzlaştırma + ölçümü tekrarlar
// (eski kapı süresi dolunca kapanır, kopan zincir zorlamayı kapatır).
func KapiDogrulaBaslat(ctx context.Context, aralik time.Duration) {
	if aralik <= 0 {
		aralik = 15 * time.Minute
	}
	go func() {
		// 🔴 Arka plan işinde panik TÜM paneli düşürür.
		defer func() {
			if r := recover(); r != nil {
				log.Printf("🔴 kapi bakım döngüsü panikledi, durduruldu: %v", r)
			}
		}()
		sonuc := kapiOlcVeAyarla(ctx, 20)
		ardisik := 0
		for {
			if sonuc == olcumTamam {
				ardisik = 0
			} else {
				ardisik++
			}
			bekle := aralik
			if sonuc != olcumTamam && kapiModuVar() {
				bekle = kapiGeriCekilme(ardisik, aralik)
			}
			t := time.NewTimer(bekle)
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-kapiYenidenOlc:
				// Panelden uygulanan değişiklik ölçülemedi: tam aralığı bekleme, kısa
				// geri çekilmeyle yeniden dene.
				t.Stop()
				sonuc, ardisik = olcumBelirsiz, 0
				continue
			case <-t.C:
			}
			kapiMu.Lock()
			acilabilir := kapiUzlastir(time.Now().Unix())
			kapiMu.Unlock()
			if !acilabilir {
				kapiZorlamaYaz(false, "uzlaştırma")
				sonuc = olcumKopuk
				continue
			}
			sonuc = kapiOlcVeAyarla(ctx, 3)
		}
	}()
}

// kapiYenidenOlc: bakım döngüsüne "kısa aralıkla yeniden ölç" dürtüsü (KapiSimdiUygula'nın
// ölçümü belirsiz kaldığında). Tamponlu: dürtü kaybolmaz, birikmez.
var kapiYenidenOlc = make(chan struct{}, 1)

// kapiModuVar: kapi.json var mı? Kapı modu yapılandırılmamış sunucuda (geçiş yapılmamış,
// masaüstü modunda atlanmış) bakım döngüsü kısa aralığa GEÇMEZ.
func kapiModuVar() bool {
	_, err := os.Stat(kapiAyarYolu)
	return err == nil
}

// kapiGeriCekilme: zorlama kapalıyken ya da ölçüm belirsizken bir sonraki deneme — rol
// ayrımı devre dışı ya da doğrulanmamış; 15 dakika beklemek fail-open penceresini uzatırdı.
// 1 dk'dan başlar, her ardışık başarısızlıkta ikiye katlanır, en çok aralik.
func kapiGeriCekilme(ardisik int, aralik time.Duration) time.Duration {
	d := time.Minute
	for i := 1; i < ardisik && d < aralik; i++ {
		d *= 2
	}
	if d > aralik {
		d = aralik
	}
	return d
}

// kapiOlcMu: ölçümler SIRAYLA koşar ve ayarı kilit altında okur — eşzamanlı iki ölçümden
// bayat ayarla başlayanın sonucu, tazesininkini ezemez (son koşan en güncel ayarı ölçer).
var kapiOlcMu sync.Mutex

// olcumSonucu: bir ölçüm turunun sonucu.
type olcumSonucu int

const (
	olcumTamam    olcumSonucu = iota // üç kapı da kanıtlı ve beklenen tipte → zorlama AÇIK
	olcumKopuk                       // kesin başarısızlık → zorlama KAPALI
	olcumBelirsiz                    // geçici hata ya da kesinti → zorlama DEĞİŞMEDİ
)

// errOlcumBelirsiz: ölçüm SONUÇ VERMEDİ (geçici: 429/502/503/504, zaman aşımı, bağlantı
// kopması). Zincirin kopuk olduğunu KANITLAMAZ: aynı kutudaki yetkisiz bir süreç panelin
// 127.0.0.1 için IP-başı bağlantı sınırını doldurarak ölçümü düşürebilir — bunu "kopuk"
// saymak, rol ayrımını dışarıdan kapattırma (fail-open) yolu olurdu. Kesin olanlar:
// bağlantı reddi (portta dinleyen yok), kanıtı tutmayan ya da beklenmeyen yanıt.
var errOlcumBelirsiz = errors.New("ölçüm belirsiz (geçici hata)")

// kapiOlcVeAyarla: üç rol portunu da ölçer; YALNIZ hepsi beklenen tipi kanıtlı olarak
// dönerse zorlama açık kalır/açılır, kesin bir başarısızlıkta kapanır. deneme: backend
// henüz dinlemiyorsa kaç kez denensin. Belirsiz sonuç ya da bağlam iptali (kapanış,
// istemci kopması) zorlamayı DEĞİŞTİRMEZ.
func kapiOlcVeAyarla(ctx context.Context, deneme int) olcumSonucu {
	kapiOlcMu.Lock()
	defer kapiOlcMu.Unlock()
	a, err := kapi.Oku(kapiAyarYolu)
	if err != nil {
		kapiZorlamaYaz(false, "ayar yok")
		return olcumKopuk
	}
	beklenen := []struct {
		port      int
		kapi, tip string
	}{{a.Admin, kapi.Admin, "yonetim"}, {a.Bayi, kapi.Bayi, "yonetim"}, {a.Musteri, kapi.Musteri, "musteri"}}
	for _, b := range beklenen {
		tip, err := kapiTipOlcucu(ctx, b.port, b.kapi, deneme)
		if ctx.Err() != nil {
			log.Printf("kapi: ölçüm yarıda kesildi (%v) — zorlama değiştirilmedi", ctx.Err())
			return olcumBelirsiz
		}
		if errors.Is(err, errOlcumBelirsiz) {
			log.Printf("kapi: ölçüm :%d belirsiz (%v) — zorlama değiştirilmedi, kısa aralıkla yinelenecek", b.port, err)
			return olcumBelirsiz
		}
		if err != nil || tip != b.tip {
			log.Printf("🔴 kapi: ölçüm :%d beklenen=%s alınan=%q hata=%v — zorlama KAPALI (eski davranış)", b.port, b.tip, tip, err)
			kapiZorlamaYaz(false, "ölçüm")
			return olcumKopuk
		}
	}
	kapiZorlamaYaz(true, "ölçüm")
	return olcumTamam
}

// kapiZorlamaDosyasi: zorlama durumunun sistem araçlarına yansıması ("acik" | "kapali").
// Bayrak panel sürecinin belleğindedir; girginospanel-erisim ve `kapi durum` onu başka
// yoldan göremez (ör. masaüstü modu "yönetici yüzeyi kapandı" demeden önce bakar).
var kapiZorlamaDosyasi = "/run/girginospanel/kapi-zorlama"

// Dosya bayraktan ÖNCE yazılır: bayrağı gören (ör. bekleyen test) dosyayı da güncel görür.
func kapiZorlamaYaz(v bool, neden string) {
	durum := "kapali\n"
	if v {
		durum = "acik\n"
	}
	if err := os.MkdirAll(filepath.Dir(kapiZorlamaDosyasi), 0o755); err == nil {
		_ = os.WriteFile(kapiZorlamaDosyasi, []byte(durum), 0o644)
	}
	if kapi.ZorlamaAktif() != v {
		log.Printf("kapi: zorlama %s (%s)", map[bool]string{true: "AÇIK", false: "KAPALI"}[v], neden)
	}
	kapi.ZorlamaAyarla(v)
}

// kapiTipOlc: https://127.0.0.1:port/api/v1/kapi → tip. Gerçek istemcilerle AYNI yol
// (nginx listen → map → başlık → X-Gosp-Proxy → backend güveni). Her denemede taze bir
// nonce gönderilir; yanıtın kanıtı beklenen kapı ve portla tutmazsa yanıt o portun
// nginx→panel zincirinden gelmiyordur (başka süreç, yanlış etiket, bayat worker).
func kapiTipOlc(ctx context.Context, port int, kapiAdi string, deneme int) (string, error) {
	gizli := kapiProxyGizli()
	if gizli == "" {
		return "", errors.New("proxy gizlisi yok — ölçüm kanıtlanamaz")
	}
	istemci := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			// Yerel ölçüm: panel sertifikası varsayılan olarak kendinden imzalıdır ve
			// yalnız 127.0.0.1'e bağlanılır; yanıtın kaynağını TLS değil ölçüm kanıtı doğrular.
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // G402: yerel self-signed panel sertifikasına 127.0.0.1 ölçümü
			// Her deneme yeni bağlantı: reload sonrası bayat worker'a tutunulmasın.
			DisableKeepAlives: true,
		},
	}
	defer istemci.CloseIdleConnections()
	url := fmt.Sprintf("https://127.0.0.1:%d/api/v1/kapi", port)
	var son error
	for i := 0; i < deneme; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(kapiOlcAra):
			}
		}
		nb := make([]byte, 32)
		if _, err := rand.Read(nb); err != nil {
			return "", err
		}
		nonce := hex.EncodeToString(nb)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set(kapi.OlcumBaslik, nonce)
		resp, err := istemci.Do(req)
		if err != nil {
			if errors.Is(err, syscall.ECONNREFUSED) {
				son = err // kesin: portta dinleyen yok
			} else {
				son = fmt.Errorf("%w: %v", errOlcumBelirsiz, err) // zaman aşımı, kopma
			}
			continue
		}
		var v struct {
			Tip   string `json:"tip"`
			Kanit string `json:"kanit"`
		}
		derr := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&v)
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusBadGateway ||
			resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusGatewayTimeout:
			// Aşırı yük / sınır (limit_conn 503) / backend henüz ayakta değil: sonuç yok.
			son = fmt.Errorf("%w: HTTP %d", errOlcumBelirsiz, resp.StatusCode)
		case resp.StatusCode != http.StatusOK || derr != nil:
			son = fmt.Errorf("HTTP %d", resp.StatusCode)
		case !hmac.Equal([]byte(v.Kanit), []byte(kapi.OlcumKaniti(gizli, nonce, kapiAdi, port))):
			son = fmt.Errorf("yanıt kanıtı tutmadı — :%d %s kapısının nginx→panel zinciri değil", port, kapiAdi)
		default:
			return v.Tip, nil
		}
	}
	return "", son
}
