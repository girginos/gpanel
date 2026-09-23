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

// Dikişler: testler geçici dizin + sahte nginx/firewall ile koşar.
var (
	kapiVhostYolu    = panelVhostPath
	kapiCatchallYolu = "/etc/nginx/conf.d/_panel_catchall.conf"
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
	dosyalar := []nginxDosya{
		{kapiVhostYolu, origB, []byte(yeni)},
		kapiMapDosyasi(kapi.MapIcerigi(a, simdi)),
	}
	if c, ok := kapiCatchallDosyasi(kapi.GenelPortlar(yeni)); ok {
		dosyalar = append(dosyalar, c)
	}
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
			d := []nginxDosya{{kapiVhostYolu, []byte(yeni), []byte(kaldirilmis)}}
			if c, ok := kapiCatchallDosyasi(kapi.GenelPortlar(kaldirilmis)); ok {
				d = append(d, c)
			}
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
			if e := kapi.Guncelle(kapiAyarYolu, func(x *kapi.Ayar) error {
				x.EskiPortlar, x.EskiBitis = nil, 0
				return nil
			}); e != nil {
				log.Printf("kapi: eski portlar ayardan düşülemedi: %v", e)
			} else {
				log.Printf("kapi: geçiş tamamlandı — eski portlar kapandı")
			}
		}
	}

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

// kapiCatchallDosyasi: panelhost'un catchall'ı varsa listen'larını portlara eşitler.
func kapiCatchallDosyasi(portlar []int) (nginxDosya, bool) {
	eski, err := os.ReadFile(kapiCatchallYolu)
	if err != nil {
		return nginxDosya{}, false // panelhost yapılandırılmamış
	}
	return nginxDosya{kapiCatchallYolu, eski, []byte(kapi.CatchallUzlastir(string(eski), portlar))}, true
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
