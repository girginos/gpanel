package websec

// Çekirdek zamanlayıcı + toplayıcı. Panel açılışında Dongusu() bir goroutine
// olarak başlatılır; her 6 saatte bir tarama yapar. İlk tarama açılıştan
// 5 dk sonra (panel'in ısınmasını bekle).
//
// 🔴 Aynı anda birden çok tarama koşmasın — running bayrağı hem DB'de hem
// bellekte tutulur. DB bayrağı reboot sonrası korunur; bellek bayrağı aynı
// process içinde tekrarlı tetiklemeyi önler.

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
	"time"
)

const (
	varsayilanPeriyot   = 6 * time.Hour
	acilisGecikmesi     = 5 * time.Minute
	feedIsteğiBekleme   = 200 * time.Millisecond // eklenti başına küçük jitter
)

var (
	kosmaMu   sync.Mutex
	kosuyor   bool
	sonHata   error
	sonBaslama time.Time
)

// Dongusu — arka plan zamanlayıcısı. Blocking; goroutine olarak çağır.
func Dongusu(ctx context.Context, db *sql.DB) {
	// 🔴 STARTUP RECONCILIATION: panel process kill/crash olursa DB'de
	// running=1 sonsuza dek kalır → "tarama zaten çalışıyor" 409'u.
	// Açılışta bir kez sıfırla — bellek bayrağı (kosuyor) zaten reset,
	// DB damgasını da hizala.
	_, _ = db.Exec(`UPDATE cp_websec_status SET running=0 WHERE running=1`)

	// Açılış gecikmesi — panel yeni açıldıysa migration/init akışı bitsin.
	select {
	case <-ctx.Done():
		return
	case <-time.After(acilisGecikmesi):
	}

	Tara(ctx, db, false)

	tk := time.NewTicker(varsayilanPeriyot)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
			Tara(ctx, db, false)
		}
	}
}

// Tara — bir tam tarama koşusu. force=true yalnız elle tetiklemede kullanılır
// (throttle atlatır). Zaten koşuyorsa hemen döner.
//
// Geriye-uyumlu — Yeni imzayı `TaraDomain` ile genişlettim; eski çağrı yerleri
// bu sarmalayıcıya bağlı kaldı, tüm domainleri tarar.
func Tara(ctx context.Context, db *sql.DB, force bool) error {
	return TaraDomain(ctx, db, force, 0)
}

// TaraDomain — tek domain wrapper (geriye-uyumlu).
func TaraDomain(ctx context.Context, db *sql.DB, force bool, domainID int64) error {
	if domainID <= 0 {
		return TaraDomains(ctx, db, force, nil)
	}
	return TaraDomains(ctx, db, force, []int64{domainID})
}

// TaraDomains — filtre listesi verilirse yalnız o domain'ler taranır.
// nil/boş liste → tüm filo (klasik full-scan). Kilit TEK çağrı boyunca
// tutulur — bu yüzden N domain için N çağrı YERINE bir liste geçirilmeli.
func TaraDomains(ctx context.Context, db *sql.DB, force bool, domainIDs []int64) error {
	kosmaMu.Lock()
	if kosuyor {
		kosmaMu.Unlock()
		return errors.New("tarama zaten çalışıyor")
	}
	kosuyor = true
	sonBaslama = time.Now()
	kosmaMu.Unlock()
	// DB damgası kilit ALTINDA değil — DB yavaşsa Rescan handler'ları da
	// bloke olur. Kilit yalnız bellek bayrağı için; DB damgası best-effort.
	_, _ = db.ExecContext(ctx, `UPDATE cp_websec_status SET running=1, last_run=NOW() WHERE id=1`)

	defer func() {
		kosmaMu.Lock()
		kosuyor = false
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

	// 1) WP kurulumlarını bul (domainID > 0 ise sadece o domain)
	kurulumlar, err := WPKurulumlariBul(ctx, db)
	if err != nil {
		hataYaz(db, err)
		return err
	}
	if len(domainIDs) > 0 {
		set := make(map[int64]struct{}, len(domainIDs))
		for _, id := range domainIDs {
			if id > 0 {
				set[id] = struct{}{}
			}
		}
		var filt []WPKurulum
		for _, k := range kurulumlar {
			if _, ok := set[k.DomainID]; ok {
				filt = append(filt, k)
			}
		}
		kurulumlar = filt
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
				case <-time.After(feedIsteğiBekleme + time.Duration(rand.IntN(200))*time.Millisecond):
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
	nodeKur, err := NodejsKurulumlariBul(ctx, db)
	if err == nil {
		if len(domainIDs) > 0 {
			set := make(map[int64]struct{}, len(domainIDs))
			for _, id := range domainIDs { set[id] = struct{}{} }
			var f []nodejsKurulum
			for _, k := range nodeKur { if _, ok := set[k.DomainID]; ok { f = append(f, k) } }
			nodeKur = f
		}
		toplamApp += len(nodeKur)
		for _, k := range nodeKur {
			if ctx.Err() != nil { break }
			n, ok := nodejsTara(ctx, db, &k)
			toplamBulgu += n
			if ok { nodeFeedOK = true }
			envanterYaz(ctx, db, k.DomainID, "nodejs", k.Yol, "", len(k.Paketler), n)
		}
	}

	// 4) PHP Composer — composer.lock → OSV Packagist
	phpKur, err := PhpKurulumlariBul(ctx, db)
	if err == nil {
		if len(domainIDs) > 0 {
			set := make(map[int64]struct{}, len(domainIDs))
			for _, id := range domainIDs { set[id] = struct{}{} }
			var f []phpKurulum
			for _, k := range phpKur { if _, ok := set[k.DomainID]; ok { f = append(f, k) } }
			phpKur = f
		}
		toplamApp += len(phpKur)
		for _, k := range phpKur {
			if ctx.Err() != nil { break }
			n, ok := phpTara(ctx, db, &k)
			toplamBulgu += n
			if ok { phpFeedOK = true }
			envanterYaz(ctx, db, k.DomainID, "php-composer", k.Yol, "", len(k.Paketler), n)
		}
	}

	sure := time.Since(basla).Milliseconds()

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
	if wpFeedOK   { basariliApps = append(basariliApps, "wordpress") }
	if nodeFeedOK { basariliApps = append(basariliApps, "nodejs") }
	if phpFeedOK  { basariliApps = append(basariliApps, "php-composer") }

	if len(basariliApps) > 0 {
		stale := basla.Add(-1 * time.Second)
		appYer := make([]string, len(basariliApps))
		sArgs := []any{stale}
		for i, a := range basariliApps {
			appYer[i] = "?"
			sArgs = append(sArgs, a)
		}
		sorgu := `DELETE FROM cp_websec_findings WHERE last_seen < ? AND app_type IN (` + strings.Join(appYer, ",") + `)`
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
	} else {
		log.Printf("websec: solma temizliği atlandı — hiçbir feed başarılı yanıt vermedi (network outage şüphesi)")
	}

	ozet := ozetiHesapla(yazCtx, db)
	// 🔴 Tekil taramada scanned_apps/duration_ms güncellenmez — bunlar SON
	// FULL taramayı temsil eder. Aksi halde "1 uygulama tarandı" yazması
	// filoyu yanlış anlatır. Bulgu sayaçları (total/severity) her durumda
	// güncellenir çünkü ozetiHesapla tüm tablodan sayar.
	if len(domainIDs) > 0 {
		_, _ = db.ExecContext(yazCtx,
			`UPDATE cp_websec_status SET
			   running=0, last_success=NOW(),
			   total_findings=?, critical=?, high=?, medium=?, low=?,
			   last_error=NULL WHERE id=1`,
			ozet.Toplam, ozet.Critical, ozet.High, ozet.Medium, ozet.Low)
	} else {
		_, _ = db.ExecContext(yazCtx,
			`UPDATE cp_websec_status SET
			   running=0, last_success=NOW(), duration_ms=?, scanned_apps=?,
			   total_findings=?, critical=?, high=?, medium=?, low=?,
			   last_error=NULL WHERE id=1`,
			sure, toplamApp, ozet.Toplam, ozet.Critical, ozet.High, ozet.Medium, ozet.Low)
	}

	kapsam := ""
	if len(domainIDs) > 0 {
		kapsam = fmt.Sprintf(" (domain'ler=%v)", domainIDs)
	}
	log.Printf("websec: tarama tamam — %d uygulama%s, %d bulgu (critical=%d, high=%d), %d ms",
		toplamApp, kapsam, toplamBulgu, ozet.Critical, ozet.High, sure)
	_ = force
	return nil
}

func hataYaz(db *sql.DB, err error) {
	sonHata = err
	_, _ = db.Exec(`UPDATE cp_websec_status SET running=0, last_error=? WHERE id=1`, err.Error())
	log.Printf("websec: tarama başarısız: %v", err)
}

// Ozet — severity sayaçları.
type Ozet struct {
	Toplam, Critical, High, Medium, Low int
}

func ozetiHesapla(ctx context.Context, db *sql.DB) Ozet {
	var o Ozet
	rows, err := db.QueryContext(ctx, `SELECT severity, COUNT(*) FROM cp_websec_findings GROUP BY severity`)
	if err != nil {
		return o
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
	return o
}

/* ---------------- HTTP Handler'lar (AdminOnly) ---------------- */

type Handler struct {
	DB *sql.DB
}

type durumYanit struct {
	Running        bool     `json:"running"`
	LastRun        *string  `json:"last_run"`
	LastSuccess    *string  `json:"last_success"`
	TotalFindings  int      `json:"total_findings"`
	Critical       int      `json:"critical"`
	High           int      `json:"high"`
	Medium         int      `json:"medium"`
	Low            int      `json:"low"`
	ScannedApps    int      `json:"scanned_apps"`
	DurationMs     int      `json:"duration_ms"`
	LastError      *string  `json:"last_error"`
	NextEstimate   string   `json:"next_estimate"`
	// Aktif tarayıcı ekosistemleri — sayfa "Şu an: X, Y, Z" chip'i buradan üretir.
	// Her ekosistem için ayrıca kaç bulgu var: {"wordpress": 3, "nodejs": 12}
	Ekosistemler   []string       `json:"ekosistemler"`
	AppSayaclari   map[string]int `json:"app_sayaclari"`
}

func (h *Handler) Status(w http.ResponseWriter, _ *http.Request) {
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
	y.Ekosistemler = []string{"wordpress", "nodejs", "php-composer"}
	y.AppSayaclari = map[string]int{}
	if rows, e := h.DB.Query(`SELECT app_type, COUNT(*) FROM cp_websec_findings GROUP BY app_type`); e == nil {
		defer rows.Close()
		for rows.Next() {
			var at string; var n int
			if rows.Scan(&at, &n) == nil {
				y.AppSayaclari[at] = n
			}
		}
	}
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
		if n, _ := strconv.Atoi(s); n > 0 { sayfa = n }
	}
	boyut := 50
	if s := q.Get("page_size"); s != "" {
		if n, e := strconv.Atoi(s); e == nil && n > 0 && n <= 500 { boyut = n }
	}

	// WHERE koşulları — hem count hem list için ortak
	where := ""
	var args []any
	if sev != "" {
		where += " AND f.severity=?"
		args = append(args, sev)
	}
	if appType != "" {
		where += " AND f.app_type=?"
		args = append(args, appType)
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
	if err := h.DB.QueryRow(sayiSorgu, args...).Scan(&toplam); err != nil {
		hataMesaji(w, 500, err.Error())
		return
	}

	// 2) Sayfa verisi
	offset := (sayfa - 1) * boyut
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

	rows, err := h.DB.Query(listeSorgu, listArgs...)
	if err != nil {
		hataMesaji(w, 500, err.Error())
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

func (h *Handler) Rescan(w http.ResponseWriter, r *http.Request) {
	kosmaMu.Lock()
	kosuyorMu := kosuyor
	kosmaMu.Unlock()
	if kosuyorMu {
		hataMesaji(w, 409, "tarama zaten çalışıyor")
		return
	}
	// domain_id query parametresi verilirse yalnız o domain taranır.
	var domainID int64
	if q := r.URL.Query().Get("domain_id"); q != "" {
		if v, e := strconv.ParseInt(q, 10, 64); e == nil && v > 0 {
			domainID = v
		}
	}
	// Async — API cevabı hemen dön, tarama arka planda
	go TaraDomain(context.Background(), h.DB, true, domainID)
	jsonYaz(w, 202, map[string]any{"started": true, "domain_id": domainID, "at": time.Now().Format(time.RFC3339)})
}

type rescanManyGovde struct {
	DomainIDs []int64 `json:"domain_ids"`
}

// RescanMany — birden çok domain'i tek çağrıda tara. Frontend'de "seçili
// satırları tara" akışını karşılar. Boş liste → 400.
func (h *Handler) RescanMany(w http.ResponseWriter, r *http.Request) {
	kosmaMu.Lock()
	kosuyorMu := kosuyor
	kosmaMu.Unlock()
	if kosuyorMu {
		hataMesaji(w, 409, "tarama zaten çalışıyor")
		return
	}
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
	go TaraDomains(context.Background(), h.DB, true, temiz)
	jsonYaz(w, 202, map[string]any{"started": true, "count": len(temiz), "at": time.Now().Format(time.RFC3339)})
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
