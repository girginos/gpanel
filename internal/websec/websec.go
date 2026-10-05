package websec

// Çekirdek zamanlayıcı + toplayıcı. Panel açılışında Dongusu() bir goroutine
// olarak başlatılır: tarama kuyruğunun TEK işçisini başlatır (kuyruk.go) ve her
// 6 saatte bir tam tarama ister. İlk tam tarama açılıştan 5 dk sonra (panel'in
// ısınmasını bekle). HTTP uçları taramayı doğrudan başlatmaz, kuyruğa yazar.
//
// 🔴 Aynı anda birden çok tarama koşmasın — tek işçi + running bayrağı (DB ve
// bellek). DB bayrağı reboot sonrası korunur; bellek bayrağı aynı process içinde
// tekrarlı tetiklemeyi önler.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"girginospanel/internal/httpx"
)

const (
	varsayilanPeriyot = 6 * time.Hour
	acilisGecikmesi   = 5 * time.Minute
	feedIsteğiBekleme = 200 * time.Millisecond // eklenti başına küçük jitter

	// feedHataOnek — cp_websec_status.last_error bu önekle başlıyorsa son YEM SORGULU
	// taramada güvenlik veri kaynaklarına erişilemedi (feed_uyari). Göç gerektirmez:
	// kalıcı işaret mevcut last_error sütunudur; kısmi/0-uygulamalı tarama bunu silmez.
	feedHataOnek  = "Güvenlik veri kaynaklarına erişilemedi"
	feedHataMetni = feedHataOnek + " — sonuçlar güncel olmayabilir"
)

// feedDegerlendir — tarama sonunda feed sağlığı. denendi: en az bir ekosistemde yem
// sorgusu YAPILDI (uygulama/paket vardı); hatali: denenen ekosistemlerden biri hiç
// başarılı yanıt alamadı. 0 uygulamalı taramada denendi=false → ne uyarı ne "feed
// sağlıklı" iddiası (yem sorgulanmadı: "solma atlandı" alarmı yanlış alarmdır).
func feedDegerlendir(wpDenendi, wpOK, nodeDenendi, nodeOK, phpDenendi, phpOK bool) (denendi, hatali bool) {
	denendi = wpDenendi || nodeDenendi || phpDenendi
	hatali = (wpDenendi && !wpOK) || (nodeDenendi && !nodeOK) || (phpDenendi && !phpOK)
	return
}

// feedHataIfadesi — status UPDATE'inin last_error parçası + bağımsız değişkeni.
// Hatalı → uyarı metni; yem sağlıklı → NULL; yem hiç sorgulanmadı → önceki feed
// uyarısı KORUNUR (başka hata ise temizlenir, eski davranış).
func feedHataIfadesi(denendi, hatali bool) (string, any) {
	switch {
	case hatali:
		return "last_error=?", feedHataMetni
	case denendi:
		return "last_error=NULL", nil
	default:
		return "last_error=IF(last_error LIKE ?, last_error, NULL)", feedHataOnek + "%"
	}
}

var (
	kosmaMu sync.Mutex
	kosuyor bool
	// taranEden — o an taranan domain id kümesi (kosmaMu korur). kosuyor==true &&
	// küme BOŞ => TÜM domainler taranıyor (full-scan). Apps handler domain başına
	// "taranıyor" durumunu buradan üretir.
	taranEden = map[int64]bool{}
)

// TaramaDurumu — (kosuyor mu, hedef domain id kümesi kopyası). Boş küme + kosuyor
// => tüm domainler taranıyor.
func TaramaDurumu() (bool, map[int64]bool) {
	kosmaMu.Lock()
	defer kosmaMu.Unlock()
	kopya := make(map[int64]bool, len(taranEden))
	for k := range taranEden {
		kopya[k] = true
	}
	return kosuyor, kopya
}

// Dongusu — arka plan zamanlayıcısı. Blocking; goroutine olarak çağır.
func Dongusu(ctx context.Context, db *sql.DB) {
	// 🔴 STARTUP RECONCILIATION: panel process kill/crash olursa DB'de
	// running=1 sonsuza dek kalır → durum kartı hep "taranıyor" gösterir.
	// Açılışta bir kez sıfırla — bellek bayrağı (kosuyor) zaten reset,
	// DB damgasını da hizala.
	_, _ = db.Exec(`UPDATE cp_websec_status SET running=0 WHERE running=1`)
	// Feed uyarısı yeniden başlatmada kaybolmasın (kalıcı işaret: last_error öneki).
	var feedVar sql.NullBool
	if db.QueryRow(`SELECT COALESCE(last_error LIKE ?, 0) FROM cp_websec_status WHERE id=1`, feedHataOnek+"%").Scan(&feedVar) == nil {
		feedUyariBayrak.Store(feedVar.Valid && feedVar.Bool)
	}

	// Tek işçi açılıştan itibaren çalışır: elle istekler açılış gecikmesini beklemez.
	go kuy.isci(ctx, db)

	// Açılış gecikmesi — panel yeni açıldıysa migration/init akışı bitsin.
	select {
	case <-ctx.Done():
		return
	case <-time.After(acilisGecikmesi):
	}
	kuy.tamIste()

	// Her tur tam tarama İSTER (bayrak): o an bir tarama sürüyor ya da kuyrukta
	// bayi istekleri bekliyor olsa da tur atlanmaz, işçi sıradaki iş olarak koşar.
	tk := time.NewTicker(varsayilanPeriyot)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
			kuy.tamIste()
		}
	}
}

// TaraDomains — filtre listesi verilirse yalnız o domain'ler taranır.
// nil/boş liste → tüm filo (klasik full-scan). Kilit TEK çağrı boyunca
// tutulur — bu yüzden N domain için N çağrı YERINE bir liste geçirilmeli.
// Yalnız kuyruk işçisi çağırır (isCalistir): kısmi iş boş listeyle koşmaz.
func TaraDomains(ctx context.Context, db *sql.DB, domainIDs []int64) error {
	kosmaMu.Lock()
	if kosuyor {
		kosmaMu.Unlock()
		return errors.New("tarama zaten çalışıyor")
	}
	kosuyor = true
	taranEden = map[int64]bool{}
	for _, id := range domainIDs {
		if id > 0 {
			taranEden[id] = true
		}
	}
	kosmaMu.Unlock()
	// DB damgası kilit ALTINDA değil — DB yavaşsa durum okuyan handler'lar da
	// bloke olur. Kilit yalnız bellek bayrağı için; DB damgası best-effort.
	_, _ = db.ExecContext(ctx, `UPDATE cp_websec_status SET running=1, last_run=NOW() WHERE id=1`)

	defer func() {
		kosmaMu.Lock()
		kosuyor = false
		taranEden = map[int64]bool{}
		kosmaMu.Unlock()
	}()

	basla := time.Now()
	var toplamBulgu int
	var toplamApp int

	// 🔴 Ekosistem başına feed başarı bayrağı. Solma temizliği YALNIZ
	// başarılı feed'lerin ekosistemi için çalışır. Aksi halde feed outage'da
	// (OSV.dev veya wpvulnerability.net kısa süreliğine yanıtsız) DELETE
	// önceki iyi taramanın tüm bulgularını siler → dashboard sessizce '0
	// kritik' gösterir → operatör rahat eder → 6 saat sonra bulgular geri
	// gelir. Bu bir güvenlik monitörü için en kötü hata modu.
	wpFeedOK := false
	nodeFeedOK := false
	phpFeedOK := false
	nodeDenendi := false
	phpDenendi := false

	// 1) WP kurulumlarını bul (domainIDs doluysa keşif YALNIZ o domainlerde koşar)
	kurulumlar, err := WPKurulumlariBul(ctx, db, domainIDs)
	if err != nil {
		hataYaz(db, err)
		return err
	}
	toplamApp = len(kurulumlar)

	// 2) Her paket için wpvulnerability.net sorgula — sonuçları önbellekle
	// (aynı slug birden çok tenant'ta olabilir).
	feedCache := map[string][]WPVulnZafiyet{} // key: "plugin:slug" veya "theme:slug"

WPloop:
	for _, k := range kurulumlar {
		// Envanter icin BU kurulumun bulgu sayisi ayri tutulur; toplamBulgu
		// tum ekosistemlerin toplamidir ve tek kuruluma atfedilemez.
		wpKurulumBulgu := 0
		if ctx.Err() != nil {
			break WPloop
		}
		// Eklentiler
		for _, p := range k.Eklentiler {
			anahtar := "plugin:" + p.Slug
			zaflar, ok := feedCache[anahtar]
			if !ok {
				var e error
				zaflar, e = WPVulnGetir(ctx, "plugin", p.Slug)
				if e != nil {
					log.Printf("websec: wpvuln %s: %v", p.Slug, e)
				} else {
					wpFeedOK = true // ≥1 başarılı yanıt = feed sağlıklı
				}
				feedCache[anahtar] = zaflar
				select {
				case <-time.After(feedIsteğiBekleme + time.Duration(rand.IntN(200))*time.Millisecond): //nolint:gosec // G404: math/rand/v2 IntN yalnız feed istekleri arası 0-199ms JITTER üretir (kibar hız-sınırlama); güvenlik jetonu/karar değeri değil, crypto/rand gerekmez.
				case <-ctx.Done():
					break WPloop
				}
			}
			n := paketKarsilastirveKaydet(ctx, db, &k, "plugin", p, zaflar)
			toplamBulgu += n
			wpKurulumBulgu += n
		}
		// Temalar
		for _, t := range k.Temalar {
			anahtar := "theme:" + t.Slug
			zaflar, ok := feedCache[anahtar]
			if !ok {
				var e error
				zaflar, e = WPVulnGetir(ctx, "theme", t.Slug)
				if e != nil {
					log.Printf("websec: wpvuln theme %s: %v", t.Slug, e)
				} else {
					wpFeedOK = true
				}
				feedCache[anahtar] = zaflar
				select {
				case <-time.After(feedIsteğiBekleme):
				case <-ctx.Done():
					break WPloop
				}
			}
			n := paketKarsilastirveKaydet(ctx, db, &k, "theme", t, zaflar)
			toplamBulgu += n
			wpKurulumBulgu += n
		}
		// Bulgu 0 olsa BILE yaz — envanterin asil degeri zaten budur:
		// "tarandi, temiz cikti" ile "hic bakilmadi" ayrimi.
		envanterYaz(ctx, db, k.DomainID, "wordpress", k.Yol, k.CoreSurum,
			len(k.Eklentiler)+len(k.Temalar), wpKurulumBulgu)
	}

	// 3) Node.js — package-lock.json / package.json → OSV npm
	nodeKur, err := NodejsKurulumlariBul(ctx, db, domainIDs)
	if err == nil {
		toplamApp += len(nodeKur)
		for _, k := range nodeKur {
			if ctx.Err() != nil {
				break
			}
			if len(k.Paketler) > 0 {
				nodeDenendi = true
			}
			n, ok := nodejsTara(ctx, db, &k)
			toplamBulgu += n
			if ok {
				nodeFeedOK = true
			}
			envanterYaz(ctx, db, k.DomainID, "nodejs", k.Yol, "", len(k.Paketler), n)
		}
	}

	// 4) PHP Composer — composer.lock → OSV Packagist
	phpKur, err := PhpKurulumlariBul(ctx, db, domainIDs)
	if err == nil {
		toplamApp += len(phpKur)
		for _, k := range phpKur {
			if ctx.Err() != nil {
				break
			}
			if len(k.Paketler) > 0 {
				phpDenendi = true
			}
			n, ok := phpTara(ctx, db, &k)
			toplamBulgu += n
			if ok {
				phpFeedOK = true
			}
			envanterYaz(ctx, db, k.DomainID, "php-composer", k.Yol, "", len(k.Paketler), n)
		}
	}

	sure := time.Since(basla).Milliseconds()
	// Feed sağlığı: yalnız yem sorgusu yapılan ekosistemler değerlendirilir.
	feedDenendi, feedHatali := feedDegerlendir(len(feedCache) > 0, wpFeedOK, nodeDenendi, nodeFeedOK, phpDenendi, phpFeedOK)
	if feedHatali && ctx.Err() != nil {
		// İptal edilmiş tarama: yarım kalan sorgular outage sayılmaz.
		feedHatali = false
	}

	// 🔴 SOLMA + STATUS için AYRI CONTEXT — scan ctx feed HTTP çağrılarında
	// tükenmiş olabilir; DB yazmayı ona bağlarsak temizlik ve durum kaydı
	// SESSİZCE atlanır. background + kısa timeout ile ayır: bunlar birkaç
	// milisaniyelik iş, gerçek risk cancel'lı ctx.
	yazCtx, yazIptal := context.WithTimeout(context.Background(), 10*time.Second)
	defer yazIptal()

	// SOLMA: bu tarama SIRASINDA `last_seen` güncellenmeyen kayıtlar artık
	// geçerli değil (paket kaldırıldı, upgrade edildi, zafiyet aralığından
	// çıktı). Ancak SADECE feed'i BAŞARIYLA ULAŞILAN ekosistemler için sil —
	// aksi halde outage sırasında dashboard sessizce boşalır (bkz. üstte
	// wpFeedOK/nodeFeedOK/phpFeedOK yorumu).
	//
	// Zaman payı: `basla`'dan 1 saniye ÖNCE eşiği koy — mikro-drift, race
	// koruması. Tam saniye başında yazılan kaydı yanlışlıkla silmemek için.
	// Kısmi tarama (domainIDs) ise silme SADECE o domain'lerle sınırlı olmalı;
	// aksi halde taranmayan domain'lerin sağlam bulguları silinir.
	basariliApps := []string{}
	if wpFeedOK {
		basariliApps = append(basariliApps, "wordpress")
	}
	if nodeFeedOK {
		basariliApps = append(basariliApps, "nodejs")
	}
	if phpFeedOK {
		basariliApps = append(basariliApps, "php-composer")
	}

	if len(basariliApps) > 0 {
		stale := basla.Add(-1 * time.Second)
		appYer := make([]string, len(basariliApps))
		sArgs := []any{stale}
		for i, a := range basariliApps {
			appYer[i] = "?"
			sArgs = append(sArgs, a)
		}
		sorgu := `DELETE FROM cp_websec_findings WHERE last_seen < ? AND app_type IN (` + strings.Join(appYer, ",") + `)` //nolint:gosec // G202: appYer = []string{"?",...} (yalnız placeholder); basariliApps SABİT ("wordpress"/"nodejs"/"php-composer") ve tüm değerler sArgs ile ? bağlanır.
		if len(domainIDs) > 0 {
			domYer := make([]string, len(domainIDs))
			for i, id := range domainIDs {
				domYer[i] = "?"
				sArgs = append(sArgs, id)
			}
			sorgu += ` AND domain_id IN (` + strings.Join(domYer, ",") + `)`
		}
		_, _ = db.ExecContext(yazCtx, sorgu, sArgs...)
		// Envanter de ayni solma mantigina tabi: silinen site / kaldirilan
		// WordPress envanterde kalirsa panel var olmayan bir kurulumu
		// "tarandi" diye gosterir.
		envanterSolmaTemizle(yazCtx, db, stale, basariliApps, domainIDs)
	} else if feedDenendi {
		log.Printf("websec: solma temizliği atlandı — hiçbir feed başarılı yanıt vermedi (network outage şüphesi)")
	} else {
		log.Printf("websec: solma temizliği atlandı — yem sorgulanmadı (taranacak uygulama/paket yok)")
	}
	// Domain başına tarama damgası — yalnız tarama YARIDA kesilmediyse: ctx iptali
	// keşfi/feed'i yarım bırakır, o domainlere "tarandı" demek yanlış olur.
	if ctx.Err() == nil {
		domainTaramaYaz(yazCtx, db, domainIDs, sure/1000+1)
	}

	ozet := ozetiHesapla(yazCtx, db)
	// 🔴 Tekil taramada scanned_apps/duration_ms güncellenmez — bunlar SON
	// FULL taramayı temsil eder. Aksi halde "1 uygulama tarandı" yazması
	// filoyu yanlış anlatır. Bulgu sayaçları (total/severity) her durumda
	// güncellenir çünkü ozetiHesapla tüm tablodan sayar.
	feedBayrakGuncelle(feedDenendi, feedHatali)
	errIfade, errArg := feedHataIfadesi(feedDenendi, feedHatali)
	var errArgs []any
	if errArg != nil {
		errArgs = []any{errArg}
	}
	if len(domainIDs) > 0 {
		_, _ = db.ExecContext(yazCtx,
			`UPDATE cp_websec_status SET
			   running=0, last_success=NOW(),
			   total_findings=?, critical=?, high=?, medium=?, low=?,
			   `+errIfade+` WHERE id=1`, //nolint:gosec // G202: errIfade SABİT parçalardan (feedHataIfadesi); değer ? ile bağlanır.
			append([]any{ozet.Toplam, ozet.Critical, ozet.High, ozet.Medium, ozet.Low}, errArgs...)...)
	} else {
		_, _ = db.ExecContext(yazCtx,
			`UPDATE cp_websec_status SET
			   running=0, last_success=NOW(), duration_ms=?, scanned_apps=?,
			   total_findings=?, critical=?, high=?, medium=?, low=?,
			   `+errIfade+` WHERE id=1`, //nolint:gosec // G202: errIfade SABİT parçalardan (feedHataIfadesi); değer ? ile bağlanır.
			append([]any{sure, toplamApp, ozet.Toplam, ozet.Critical, ozet.High, ozet.Medium, ozet.Low}, errArgs...)...)
	}

	kapsam := ""
	if len(domainIDs) > 0 {
		kapsam = fmt.Sprintf(" (domain'ler=%v)", domainIDs)
	}
	log.Printf("websec: tarama tamam — %d uygulama%s, %d bulgu (critical=%d, high=%d), %d ms",
		toplamApp, kapsam, toplamBulgu, ozet.Critical, ozet.High, sure)
	return nil
}

// kesifSorgusu — üç tarayıcının (WP/Node/PHP) domain keşif sorgusu. ids boş →
// TÜM domainler (tam tarama); dolu → YALNIZ o domainler: tek domain taransa da
// keşif (wp-cli, dosya okuma) filonun tamamında koşmasın.
func kesifSorgusu(ids []int64) (string, []any) {
	sorgu := `SELECT id, sistem_kullanici, alan_adi FROM domains`
	var args []any
	if len(ids) > 0 {
		sorgu += ` WHERE id IN (` + yerTutucu(len(ids)) + `)` //nolint:gosec // G202: yerTutucu yalnız "?" üretir; id'ler args ile bağlanır.
		for _, id := range ids {
			args = append(args, id)
		}
	}
	return sorgu + ` ORDER BY alan_adi`, args
}

// yerTutucu — n adet "?" (IN listesi için). Değerler her zaman args ile bağlanır.
func yerTutucu(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func hataYaz(db *sql.DB, err error) {
	_, _ = db.Exec(`UPDATE cp_websec_status SET running=0, last_error=? WHERE id=1`, err.Error())
	log.Printf("websec: tarama başarısız: %v", err)
}

// Ozet — severity sayaçları.
type Ozet struct {
	Toplam, Critical, High, Medium, Low int
}

func ozetiHesapla(ctx context.Context, db *sql.DB) Ozet {
	o, _ := ozetSay(ctx, db, `SELECT severity, COUNT(*) FROM cp_websec_findings GROUP BY severity`)
	return o
}

// ozetSay — (severity, sayı) satırları döndüren sorgudan Ozet. Sunucu geneli
// (ozetiHesapla) ve bayi kapsamlı durum kartı aynı toplamayı kullanır.
func ozetSay(ctx context.Context, db *sql.DB, sorgu string, args ...any) (Ozet, error) {
	var o Ozet
	rows, err := db.QueryContext(ctx, sorgu, args...) //nolint:gosec // G701: sorgu SABİT parçalar + kapsam.kosul (yalnız "?"); değerler args ile bağlanır.
	if err != nil {
		return o, err
	}
	defer rows.Close()
	for rows.Next() {
		var sev string
		var n int
		if rows.Scan(&sev, &n) != nil {
			continue
		}
		o.Toplam += n
		switch sev {
		case "critical":
			o.Critical = n
		case "high":
			o.High = n
		case "medium":
			o.Medium = n
		case "low":
			o.Low = n
		}
	}
	return o, rows.Err()
}

/* ---------------- HTTP Handler'lar (kök + bayi; kapsam: kapsam.go) ---------------- */

type Handler struct {
	DB *sql.DB
}

type durumYanit struct {
	Running       bool    `json:"running"`
	LastRun       *string `json:"last_run"`
	LastSuccess   *string `json:"last_success"`
	TotalFindings int     `json:"total_findings"`
	Critical      int     `json:"critical"`
	High          int     `json:"high"`
	Medium        int     `json:"medium"`
	Low           int     `json:"low"`
	ScannedApps   int     `json:"scanned_apps"`
	DurationMs    int     `json:"duration_ms"`
	LastError     *string `json:"last_error"`
	NextEstimate  string  `json:"next_estimate"`
	// Aktif tarayıcı ekosistemleri — sayfa "Şu an: X, Y, Z" chip'i buradan üretir.
	// Her ekosistem için ayrıca kaç bulgu var: {"wordpress": 3, "nodejs": 12}
	Ekosistemler []string       `json:"ekosistemler"`
	AppSayaclari map[string]int `json:"app_sayaclari"`
	// FeedUyari — son yem sorgulu taramada güvenlik veri kaynaklarına (wpvulnerability.net,
	// osv.dev) erişilemedi: "Temiz" güvencesi güncel olmayabilir.
	FeedUyari bool `json:"feed_uyari"`
}

// bayiDurumYanit — bayinin durum kartı. Sunucu geneli alanlar (scanned_apps,
// duration_ms, last_error, next_estimate, app_sayaclari) AYRI TİP sayesinde yanıtta
// hiç yer almaz. last_run = last_success = kendi domainlerinin en yeni taraması;
// running = kendi domaini sırada ya da taranıyor.
type bayiDurumYanit struct {
	Running       bool     `json:"running"`
	LastRun       *string  `json:"last_run"`
	LastSuccess   *string  `json:"last_success"`
	TotalFindings int      `json:"total_findings"`
	Critical      int      `json:"critical"`
	High          int      `json:"high"`
	Medium        int      `json:"medium"`
	Low           int      `json:"low"`
	Ekosistemler  []string `json:"ekosistemler"`
	FeedUyari     bool     `json:"feed_uyari"`
}

// feedUyariBayrak — bayinin görebildiği feed uyarısı (yalnız bool; sunucu geneli
// last_error metni bayiye gitmez, bayi durumu cp_websec_status'u HİÇ okumaz). Tarama
// sonu günceller; açılışta last_error önekinden yüklenir (Dongusu).
var feedUyariBayrak atomic.Bool

// feedBayrakGuncelle — yem sorgulanmadıysa (0 uygulama) bayrak DEĞİŞMEZ.
func feedBayrakGuncelle(denendi, hatali bool) {
	if denendi {
		feedUyariBayrak.Store(hatali)
	}
}

// ekosistemler — aktif tarayıcı ekosistemleri (sabit; sayfa chip'leri).
func ekosistemler() []string {
	return []string{"wordpress", "nodejs", "php-composer"}
}

func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	if k := kapsamAl(r); k.bayi {
		h.bayiDurum(w, r, k)
		return
	}
	var y durumYanit
	var lastRun, lastSuccess, lastErr sql.NullString
	var running int
	err := h.DB.QueryRow(
		`SELECT running, last_run, last_success, total_findings,
		        critical, high, medium, low, scanned_apps, duration_ms, last_error
		 FROM cp_websec_status WHERE id=1`).
		Scan(&running, &lastRun, &lastSuccess, &y.TotalFindings,
			&y.Critical, &y.High, &y.Medium, &y.Low, &y.ScannedApps, &y.DurationMs, &lastErr)
	if err != nil && err != sql.ErrNoRows {
		hataMesaji(w, 500, err.Error())
		return
	}
	y.Running = running != 0
	if lastRun.Valid {
		y.LastRun = &lastRun.String
	}
	if lastSuccess.Valid {
		y.LastSuccess = &lastSuccess.String
	}
	if lastErr.Valid {
		y.LastError = &lastErr.String
	}
	// Sonraki tarama tahmini
	if lastRun.Valid {
		if t, e := time.Parse("2006-01-02 15:04:05", lastRun.String); e == nil {
			next := t.Add(varsayilanPeriyot)
			y.NextEstimate = next.Format(time.RFC3339)
		}
	}
	// Ekosistem listesi + sayaçlar
	y.Ekosistemler = ekosistemler()
	y.FeedUyari = lastErr.Valid && strings.HasPrefix(lastErr.String, feedHataOnek)
	y.AppSayaclari = map[string]int{}
	if rows, e := h.DB.Query(`SELECT app_type, COUNT(*) FROM cp_websec_findings GROUP BY app_type`); e == nil {
		defer rows.Close()
		for rows.Next() {
			var at string
			var n int
			if rows.Scan(&at, &n) == nil {
				y.AppSayaclari[at] = n
			}
		}
	}
	jsonYaz(w, 200, y)
}

// bayiDurum — bayinin durum kartı: YALNIZ kendi domainlerinden hesaplanır.
// cp_websec_status (tek satır, sunucu geneli) HİÇ okunmaz.
func (h *Handler) bayiDurum(w http.ResponseWriter, r *http.Request, k kapsam) {
	kosul, args := k.kosul()
	y := bayiDurumYanit{Ekosistemler: ekosistemler(), FeedUyari: feedUyariBayrak.Load()}

	kosuyor, taranSet := TaramaDurumu()
	bekleyen := kuy.bekleyenKume()
	//nolint:gosec // G202/G701: kosul kapsam.kosul'dan (sabit parça + "?"); değerler args ile bağlanır.
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT d.id, COALESCE(DATE_FORMAT(t.son_tarama,'%Y-%m-%d %H:%i:%s'),'')
		  FROM domains d
		  LEFT JOIN cp_websec_domain_tarama t ON t.domain_id = d.id
		 WHERE 1=1`+kosul, args...)
	if err != nil {
		k.hata(w, err)
		return
	}
	defer rows.Close()
	enYeni := ""
	for rows.Next() {
		var id int64
		var son string
		if err := rows.Scan(&id, &son); err != nil {
			k.hata(w, err)
			return
		}
		// Sabit biçim (YYYY-AA-GG SS:DD:ss) — metin karşılaştırması zaman sırasıdır.
		if son > enYeni {
			enYeni = son
		}
		if (kosuyor && (len(taranSet) == 0 || taranSet[id])) || bekleyen[id] {
			y.Running = true
		}
	}
	if err := rows.Err(); err != nil {
		k.hata(w, err)
		return
	}
	if enYeni != "" {
		y.LastRun, y.LastSuccess = &enYeni, &enYeni
	}

	o, err := ozetSay(r.Context(), h.DB, `
		SELECT f.severity, COUNT(*)
		  FROM cp_websec_findings f
		  JOIN domains d ON d.id = f.domain_id
		 WHERE 1=1`+kosul+`
		 GROUP BY f.severity`, args...)
	if err != nil {
		k.hata(w, err)
		return
	}
	y.TotalFindings, y.Critical, y.High, y.Medium, y.Low = o.Toplam, o.Critical, o.High, o.Medium, o.Low
	jsonYaz(w, 200, y)
}

type bulguSatir struct {
	ID          int64   `json:"id"`
	DomainID    int64   `json:"domain_id"`
	AlanAdi     string  `json:"alan_adi"`
	AppType     string  `json:"app_type"`
	PackageName string  `json:"package_name"`
	Version     string  `json:"installed_version"`
	CVE         string  `json:"cve_id"`
	Severity    string  `json:"severity"`
	CVSS        float64 `json:"cvss"`
	Title       string  `json:"title"`
	FixedIn     string  `json:"fixed_in"`
	Source      string  `json:"source"`
	LastSeen    string  `json:"last_seen"`
}

type bulguYanit struct {
	Toplam     int          `json:"toplam"`
	Sayfa      int          `json:"sayfa"`
	SayfaBoyut int          `json:"sayfa_boyut"`
	Items      []bulguSatir `json:"items"`
}

func (h *Handler) Findings(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sev := q.Get("severity")
	appType := q.Get("app_type") // wordpress | nodejs | php-composer
	arama := strings.TrimSpace(q.Get("q"))

	sayfa := 1
	if s := q.Get("page"); s != "" {
		if n, _ := strconv.Atoi(s); n > 0 {
			sayfa = n
		}
	}
	boyut := 50
	if s := q.Get("page_size"); s != "" {
		if n, e := strconv.Atoi(s); e == nil && n > 0 && n <= 500 {
			boyut = n
		}
	}

	// WHERE koşulları — hem count hem list için ortak. İlk parça KAPSAM: bayide
	// d.reseller_id=? (yabancı domain_id → olmayanla aynı boş liste); kökte boş.
	k := kapsamAl(r)
	where, args := k.kosul()
	if sev != "" {
		where += " AND f.severity=?"
		args = append(args, sev)
	}
	if appType != "" {
		where += " AND f.app_type=?"
		args = append(args, appType)
	}
	// domain_id — domaine ÖZEL açık sayfası bu filtreyi kullanır.
	if dom := q.Get("domain_id"); dom != "" {
		if n, e := strconv.Atoi(dom); e == nil && n > 0 {
			where += " AND f.domain_id=?"
			args = append(args, n)
		}
	}
	if arama != "" {
		// Domain adı / paket / CVE / başlık üzerinde arama. LIKE '%…%' full
		// scan; küçük veri seti için sorun değil. Filo büyürse FTS düşünülür.
		where += " AND (f.package_name LIKE ? OR f.cve_id LIKE ? OR f.title LIKE ? OR d.alan_adi LIKE ?)"
		p := "%" + arama + "%"
		args = append(args, p, p, p, p)
	}

	// 1) Toplam sayı
	var toplam int
	sayiSorgu := `SELECT COUNT(*) FROM cp_websec_findings f LEFT JOIN domains d ON d.id=f.domain_id WHERE 1=1 AND d.id IS NOT NULL` + where
	if err := h.DB.QueryRow(sayiSorgu, args...).Scan(&toplam); err != nil { //nolint:gosec // G701: where = kapsam.kosul + SABİT süzgeç parçaları; tüm değerler args ile ? bağlanır.
		k.hata(w, err)
		return
	}

	// 2) Sayfa verisi
	offset := (sayfa - 1) * boyut
	//nolint:gosec // G202: where = kapsam.kosul + SABİT süzgeç parçaları; tüm değerler args ile ? bağlanır.
	listeSorgu := `SELECT f.id, f.domain_id, COALESCE(d.alan_adi,''), f.app_type,
	                 f.package_name, f.installed_version, f.cve_id,
	                 f.severity, IFNULL(f.cvss,0), COALESCE(f.title,''),
	                 COALESCE(f.fixed_in,''), f.source, f.last_seen
	          FROM cp_websec_findings f
	          LEFT JOIN domains d ON d.id=f.domain_id
	          WHERE 1=1 AND d.id IS NOT NULL` + where +
		` ORDER BY FIELD(f.severity,'critical','high','medium','low','') , f.last_seen DESC
	     LIMIT ? OFFSET ?`
	listArgs := append([]any{}, args...)
	listArgs = append(listArgs, boyut, offset)

	rows, err := h.DB.Query(listeSorgu, listArgs...) //nolint:gosec // G701: listeSorgu yukarıda SABİT parçalardan; değerler ? ile bağlanır.
	if err != nil {
		k.hata(w, err)
		return
	}
	defer rows.Close()
	items := []bulguSatir{}
	for rows.Next() {
		var b bulguSatir
		var lastSeen sql.NullString
		if err := rows.Scan(&b.ID, &b.DomainID, &b.AlanAdi, &b.AppType,
			&b.PackageName, &b.Version, &b.CVE, &b.Severity, &b.CVSS,
			&b.Title, &b.FixedIn, &b.Source, &lastSeen); err != nil {
			continue
		}
		if lastSeen.Valid {
			b.LastSeen = lastSeen.String
		}
		items = append(items, b)
	}
	jsonYaz(w, 200, bulguYanit{Toplam: toplam, Sayfa: sayfa, SayfaBoyut: boyut, Items: items})
}

// Rescan — POST /websec/rescan[?domain_id=]. Tarama başlatılmaz, kuyruğa yazılır
// (kuyruk.go) ve 202 döner; meşgulken 409 yok, istek sıraya girer.
// Kök: domain_id verilirse yalnız o domain, yoksa tam tarama (yanıt biçimi aynı).
// Bayi: bkz. bayiRescan.
func (h *Handler) Rescan(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("domain_id")
	if k := kapsamAl(r); k.bayi {
		h.bayiRescan(w, r, k, q)
		return
	}
	var domainID int64
	if q != "" {
		if v, e := strconv.ParseInt(q, 10, 64); e == nil && v > 0 {
			domainID = v
		}
	}
	if domainID > 0 {
		// Zaten sırada/taranıyor: tekrar tıklama kuyruğu şişirmez.
		if zatenSiradaMi(domainID) {
			jsonYaz(w, 202, map[string]any{"started": false, "zaten": true, "domain_id": domainID, "at": time.Now().Format(time.RFC3339)})
			return
		}
		kuy.kokEkle([]int64{domainID})
	} else {
		kuy.tamIste()
	}
	jsonYaz(w, 202, map[string]any{"started": true, "domain_id": domainID, "at": time.Now().Format(time.RFC3339)})
}

type rescanManyGovde struct {
	DomainIDs []int64 `json:"domain_ids"`
}

// RescanMany — birden çok domain'i tek çağrıda kuyruğa yaz. Frontend'de "seçili
// satırları tara" akışını karşılar. Boş liste → 400.
// Bayi: listede TEK yabancı/olmayan id varsa TAMAMI 404, hiçbiri kuyruğa girmez.
func (h *Handler) RescanMany(w http.ResponseWriter, r *http.Request) {
	var g rescanManyGovde
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&g); err != nil {
		hataMesaji(w, 400, "geçersiz gövde: "+err.Error())
		return
	}
	// Tekilleştirme + geçersiz ID temizleme
	set := map[int64]struct{}{}
	temiz := []int64{}
	for _, id := range g.DomainIDs {
		if id <= 0 {
			continue
		}
		if _, dup := set[id]; dup {
			continue
		}
		set[id] = struct{}{}
		temiz = append(temiz, id)
	}
	if len(temiz) == 0 {
		hataMesaji(w, 400, "domain_ids boş")
		return
	}
	// Üst sınır — istismar önlemi
	if len(temiz) > 200 {
		hataMesaji(w, 413, "tek seferde en fazla 200 domain taranabilir")
		return
	}
	if k := kapsamAl(r); k.bayi {
		kosul, args := k.kosul()
		sArgs := make([]any, 0, len(temiz)+len(args))
		for _, id := range temiz {
			sArgs = append(sArgs, id)
		}
		sArgs = append(sArgs, args...)
		var n int
		//nolint:gosec // G202/G701: yalnız "?" yer tutucular + kapsam.kosul; değerler args ile bağlanır.
		if err := h.DB.QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM domains d WHERE d.id IN (`+yerTutucu(len(temiz))+`)`+kosul, sArgs...).Scan(&n); err != nil {
			k.hata(w, err)
			return
		}
		if n != len(temiz) {
			hataMesaji(w, 404, "domain bulunamadı")
			return
		}
		h.bayiKuyruga(w, r, k, temiz, false, 0, fmt.Sprintf("%d domain", len(temiz)))
		return
	}
	kuy.kokEkle(temiz)
	jsonYaz(w, 202, map[string]any{"started": true, "count": len(temiz), "at": time.Now().Format(time.RFC3339)})
}

// bayiRescan — bayinin tarama isteği. domain_id verilirse YALNIZ kendi domaini:
// yabancı, olmayan ve geçersiz id aynı 404'ü alır. Verilmezse ("Domainlerimi Tara")
// domain listesi SUNUCUDA kapsamdan çıkarılır; domaini yoksa 202 {"sirada":0} ve
// hiçbir şey kuyruğa girmez — liste boş gidip tam taramaya (nil = tüm sunucu) dönmez.
func (h *Handler) bayiRescan(w http.ResponseWriter, r *http.Request, k kapsam, q string) {
	if q != "" {
		id, err := strconv.ParseInt(q, 10, 64)
		if err != nil || id <= 0 {
			hataMesaji(w, 404, "domain bulunamadı")
			return
		}
		kosul, args := k.kosul()
		var ad string
		//nolint:gosec // G202/G701: kosul kapsam.kosul'dan (sabit parça + "?"); değerler args ile bağlanır.
		err = h.DB.QueryRowContext(r.Context(),
			`SELECT d.alan_adi FROM domains d WHERE d.id=?`+kosul, append([]any{id}, args...)...).Scan(&ad)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			hataMesaji(w, 404, "domain bulunamadı")
		case err != nil:
			k.hata(w, err)
		default:
			h.bayiKuyruga(w, r, k, []int64{id}, false, id, ad)
		}
		return
	}
	ids, err := h.bayiDomainleri(r.Context(), k)
	if err != nil {
		k.hata(w, err)
		return
	}
	if len(ids) == 0 {
		jsonYaz(w, 202, map[string]int{"sirada": 0})
		return
	}
	h.bayiKuyruga(w, r, k, ids, true, 0, fmt.Sprintf("%d domain", len(ids)))
}

// bayiDomainleri — "Domainlerimi Tara" adayları: bayinin domainleri, en uzun süredir
// taranmayan önce (kuyruk payı listeyi keserse sonraki basışta kalanlar gelir).
func (h *Handler) bayiDomainleri(ctx context.Context, k kapsam) ([]int64, error) {
	kosul, args := k.kosul()
	//nolint:gosec // G202/G701: kosul kapsam.kosul'dan (sabit parça + "?"); değerler args ile bağlanır.
	rows, err := h.DB.QueryContext(ctx, `
		SELECT d.id FROM domains d
		  LEFT JOIN cp_websec_domain_tarama t ON t.domain_id = d.id
		 WHERE 1=1`+kosul+`
		 ORDER BY t.son_tarama IS NOT NULL, t.son_tarama, d.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// bayiKuyruga — sahipliği DOĞRULANMIŞ domainleri bayi sınırlarıyla kuyruğa yazar ve
// denetime işler (websec.tara). Yanıt: 202 {"sirada": n} ya da
// 429 {"error": "N dk sonra tekrar deneyin"} (süre yalnız bayinin kendi isteklerinden).
func (h *Handler) bayiKuyruga(w http.ResponseWriter, r *http.Request, k kapsam, ids []int64, tumu bool, domainID int64, hedef string) {
	// Şu an taranan domainler yeniden kuyruğa girmez ("zaten taranıyor").
	var aday []int64
	calisan := 0
	for _, id := range ids {
		if calisiyorMu(id) {
			calisan++
		} else {
			aday = append(aday, id)
		}
	}
	if len(aday) == 0 {
		jsonYaz(w, http.StatusAccepted, map[string]any{"sirada": calisan, "zaten": true})
		return
	}
	sirada, yeni, bekle := kuy.bayiEkleSay(k.rid, aday, tumu)
	if bekle > 0 {
		hataMesaji(w, http.StatusTooManyRequests, fmt.Sprintf("%d dk sonra tekrar deneyin", dakikaYukari(bekle)))
		return
	}
	sirada += calisan
	if yeni == 0 {
		// Hepsi zaten sırada: denetime ikinci satır YAZILMAZ.
		jsonYaz(w, http.StatusAccepted, map[string]any{"sirada": sirada, "zaten": true})
		return
	}
	var uid int64
	kul := ""
	if c := claimsAl(r); c != nil {
		uid, kul = c.UserID, c.Username
	}
	detay := fmt.Sprintf("sirada=%d", sirada)
	if domainID > 0 {
		httpx.DenetimDomain(h.DB, r, uid, kul, "websec.tara", hedef, detay, domainID, true)
	} else {
		httpx.Denetim(h.DB, r, uid, kul, "websec.tara", hedef, detay, k.rid, true)
	}
	jsonYaz(w, http.StatusAccepted, map[string]int{"sirada": sirada})
}

// calisiyorMu — domain şu an taranıyor mu (tam taramada hepsi).
func calisiyorMu(id int64) bool {
	kos, set := TaramaDurumu()
	return kos && (len(set) == 0 || set[id])
}

// zatenSiradaMi — domain taranıyor ya da kuyrukta bekliyor mu.
func zatenSiradaMi(id int64) bool {
	return calisiyorMu(id) || kuy.bekleyenKume()[id]
}

// dakikaYukari — süreyi yukarı yuvarlanmış tam dakikaya çevirir (en az 1).
func dakikaYukari(d time.Duration) int {
	if n := int((d + time.Minute - 1) / time.Minute); n > 1 {
		return n
	}
	return 1
}

func jsonYaz(w http.ResponseWriter, kod int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(kod)
	_ = json.NewEncoder(w).Encode(v)
}

func hataMesaji(w http.ResponseWriter, kod int, m string) {
	jsonYaz(w, kod, map[string]string{"error": m})
}

var _ = fmt.Errorf
