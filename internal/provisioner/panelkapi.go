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

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
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
	// reload sinyali asenkron işlenir: nginx yeni soketi biraz sonra bağlayabilir.
	kapiPortDinliyor = func(p int) bool {
		for i := 0; i < 6; i++ {
			if portyonetim.PortAcikTest("127.0.0.1", p) {
				return true
			}
			time.Sleep(500 * time.Millisecond)
		}
		return false
	}
	kapiProxyGizli = httpx.ProxySecret
	kapiVhostIzin  = gizliVhostIzniSertlestir // gerçek yola chmod/chgrp — testte no-op
	kapiNginxAktif = func() bool {
		return exec.Command("systemctl", "is-active", "--quiet", "nginx").Run() == nil
	}
)

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
	kapi.ZorlamaAyarla(kapiUzlastir(time.Now().Unix()))
}

// KapiSimdiUygula: kapı ayarı panelden değiştikten sonra nginx'i HEMEN uyumlar ve
// zinciri uçtan uca ölçer (15 dakikalık bakımı beklemeden). Zincir kurulamazsa zorlama
// kapanır (kilitlenme yok, eski davranış) ve hata döner.
func KapiSimdiUygula(ctx context.Context) error {
	kapiMu.Lock()
	acilabilir := kapiUzlastir(time.Now().Unix())
	kapiMu.Unlock()
	if !acilabilir {
		kapiZorlamaYaz(false, "uzlaştırma")
		return errors.New("nginx kapı ayarına uyumlanamadı — kapı kuralları devre dışı (panel günlüğüne bakın)")
	}
	kapiOlcVeAyarla(ctx, 3)
	if !kapi.ZorlamaAktif() {
		return errors.New("kapı zinciri ölçülemedi — kapı kuralları devre dışı (panel günlüğüne bakın)")
	}
	return nil
}

// kapiUzlastir: nginx'i ayara getirir. Dönüş: zorlama açılabilir mi (ayar geçerli,
// başlık her çapada, nginx doğrulandı, proxy gizlisi kurulu).
func kapiUzlastir(simdi int64) bool {
	origB, err := os.ReadFile(kapiVhostYolu)
	if err != nil {
		return false // panel bu host'ta kurulu değil
	}
	orig := string(origB)
	a, err := kapi.Oku(kapiAyarYolu)
	if err != nil {
		if !errors.Is(err, kapi.ErrYok) {
			log.Printf("🔴 kapi: %v — kapı modu KAPALI kalıyor (eski davranış)", err)
		}
		kapiKapaliTemizle(orig)
		return false
	}

	// ── Aşama 1: EKLEYİCİ ──────────────────────────────────────────────
	yeni, err := kapi.Uzlastir(orig, a, simdi)
	if err != nil {
		log.Printf("🔴 kapi: _panel.conf uzlaştırılamadı: %v — zorlama KAPALI", err)
		return false
	}
	dosyalar := append([]nginxDosya{
		{kapiVhostYolu, origB, []byte(yeni)},
		kapiMapDosyasi(kapi.MapIcerigi(a, simdi)),
	}, kapiYanDosyalar(yeni, a)...)
	if !kapiNginxUygula(dosyalar) {
		return false
	}
	kapiVhostIzin() // yeniden yazılan dosya da 0640 root:nginx kalsın
	kapiFwRolPortlariniAc(yeni, a, simdi)

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
			d := append([]nginxDosya{{kapiVhostYolu, []byte(yeni), []byte(kaldirilmis)}},
				kapiYanDosyalar(kaldirilmis, a)...)
			if kapiNginxUygula(d) {
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

	if !kapi.BaslikTamMi(son) {
		log.Printf("🔴 kapi: kapı başlığı her proxy location'ında değil — zorlama KAPALI")
		return false
	}
	if kapiProxyGizli() == "" {
		log.Printf("🔴 kapi: proxy gizlisi kurulu değil — başlığa güvenilemez, zorlama KAPALI")
		return false
	}
	return true
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
	d := []nginxDosya{{kapiVhostYolu, []byte(conf), []byte(temiz)}}
	if mapErr == nil {
		// Map kalır ama boş varsayılanla: $gp_kapi tanımlı kalsın (başka bir yerde
		// atıf kalmışsa `nginx -t` global düşmesin).
		d = append(d, nginxDosya{kapiMapYolu, mapB, []byte(kapi.MapIcerigi(nil, 0))})
	}
	if kapiNginxUygula(d) {
		kapiVhostIzin()
	}
}

// nginxDosya: uygulanacak bir nginx dosya değişikliği. eski == nil → dosya yoktu.
type nginxDosya struct {
	yol        string
	eski, yeni []byte
}

func kapiMapDosyasi(icerik string) nginxDosya {
	eski, err := os.ReadFile(kapiMapYolu)
	if err != nil {
		eski = nil
	}
	return nginxDosya{kapiMapYolu, eski, []byte(icerik)}
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
		out = append(out, nginxDosya{kapiCatchallYolu, eski, []byte(yeni)})
	}
	if eski, err := os.ReadFile(kapiAcmeYolu); err == nil {
		yeni := kapi.AcmeYonlendirmeUzlastir(string(eski), kapi.GenelPanelPortu(conf, a))
		out = append(out, nginxDosya{kapiAcmeYolu, eski, []byte(yeni)})
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
// HEPSİNİ geri alır; başarılıysa reload eder. Hiçbiri değişmediyse dokunmaz.
//
// 🔴 TABAN KONTROLÜ: `nginx -t` GLOBALDİR. nginx değişiklikten ÖNCE zaten bozuksa
// (başka bir vhost) hatanın bizim değişikliğimizden olup olmadığı bilinemez; o
// durumda yazmayız — yoksa sağlam değişikliği geri alıp yanlış karar verirdik.
func kapiNginxUygula(dosyalar []nginxDosya) bool {
	var degisen []nginxDosya
	for _, d := range dosyalar {
		if !bytes.Equal(d.eski, d.yeni) {
			degisen = append(degisen, d)
		}
	}
	if len(degisen) == 0 {
		return true
	}
	if o, e := kapiNginxKomut("nginx", "-t"); e != nil {
		log.Printf("🔴 kapi: nginx -t değişiklikten ÖNCE zaten başarısız — uygulanmadı: %s", kirpKapi(o))
		return false
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
		if e := os.WriteFile(d.yol, d.yeni, 0o644); e != nil {
			geriAl()
			log.Printf("🔴 kapi: %s yazılamadı: %v — geri alındı", d.yol, e)
			return false
		}
	}
	if o, e := kapiNginxKomut("nginx", "-t"); e != nil {
		geriAl()
		log.Printf("🔴 kapi: nginx -t başarısız, geri alındı: %s", kirpKapi(o))
		return false
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
		kapiOlcVeAyarla(ctx, 20)
		t := time.NewTicker(aralik)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				kapiMu.Lock()
				acilabilir := kapiUzlastir(time.Now().Unix())
				kapiMu.Unlock()
				if !acilabilir {
					kapiZorlamaYaz(false, "uzlaştırma")
					continue
				}
				kapiOlcVeAyarla(ctx, 3)
			}
		}
	}()
}

// kapiOlcVeAyarla: üç rol portunu da ölçer; YALNIZ hepsi beklenen tipi dönerse
// zorlama açık kalır/açılır. deneme: backend henüz dinlemiyorsa kaç kez denensin.
func kapiOlcVeAyarla(ctx context.Context, deneme int) {
	a, err := kapi.Oku(kapiAyarYolu)
	if err != nil {
		kapiZorlamaYaz(false, "ayar yok")
		return
	}
	beklenen := []struct {
		port int
		tip  string
	}{{a.Admin, "yonetim"}, {a.Bayi, "yonetim"}, {a.Musteri, "musteri"}}
	for _, b := range beklenen {
		tip, err := kapiTipOlcucu(ctx, b.port, deneme)
		if err != nil || tip != b.tip {
			log.Printf("🔴 kapi: ölçüm :%d beklenen=%s alınan=%q hata=%v — zorlama KAPALI (eski davranış)", b.port, b.tip, tip, err)
			kapiZorlamaYaz(false, "ölçüm")
			return
		}
	}
	kapiZorlamaYaz(true, "ölçüm")
}

func kapiZorlamaYaz(v bool, neden string) {
	if kapi.ZorlamaAktif() != v {
		log.Printf("kapi: zorlama %s (%s)", map[bool]string{true: "AÇIK", false: "KAPALI"}[v], neden)
	}
	kapi.ZorlamaAyarla(v)
}

// kapiTipOlc: https://127.0.0.1:port/api/v1/kapi → tip. Gerçek istemcilerle AYNI yol
// (nginx listen → map → başlık → X-Gosp-Proxy → backend güveni).
func kapiTipOlc(ctx context.Context, port, deneme int) (string, error) {
	istemci := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			// Yerel ölçüm: panel sertifikası varsayılan olarak kendinden imzalıdır ve
			// yalnız 127.0.0.1'e bağlanılır; okunan tek şey hassas olmayan kapı tipidir.
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // G402: yerel self-signed panel sertifikasına 127.0.0.1 ölçümü
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
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return "", err
		}
		resp, err := istemci.Do(req)
		if err != nil {
			son = err
			continue
		}
		var v struct {
			Tip string `json:"tip"`
		}
		derr := json.NewDecoder(resp.Body).Decode(&v)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK && derr == nil {
			return v.Tip, nil
		}
		son = fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return "", son
}
