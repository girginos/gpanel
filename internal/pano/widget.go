package pano

// widget.go — GET /api/v1/pano/w/{id}: widget başına ayrı uç (bir sorgunun
// hatası bütün panoyu düşürmez; kart başına yükleniyor/boş/hata). Uçlar YALNIZ
// indeksli/ucuz SQL okur; ağır iş (disk ölçümü, SSL dosya okuma) örnekleyicide.
//
// SAHTE VERİ YOK: geçmişi olmayan eğilim boş seri döner (arayüz "veri
// toplanıyor" der); kaynağı olmayan alan (ör. bayi için toplu işler) hiç dönmez.

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/httpx"
	"girginospanel/internal/kaynaklimit"
)

// Handlers — pano uçları.
type Handlers struct{ DB *sql.DB }

type widgetFn func(ctx context.Context, db *sql.DB, k Kapsam) (any, error)

// widgetlar — kayıt defteri. Yeni widget buraya eklenir; kapsam testi
// (widget_db_test.go) defterdeki HER widget'ı bayi jetonuyla tarar.
var widgetlar = map[string]widgetFn{
	"egilim": wEgilim,
	"kota":   wKota,
	"ssl":    wSSL,
	"askida": wAskida,
	"isler":  wIsler,
	"websec": wWebsec,
}

// Widget — GET /pano/w/{id}.
func (h *Handlers) Widget(w http.ResponseWriter, r *http.Request) {
	k, ok := kapsamCoz(r)
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "yetkiniz yok")
		return
	}
	id := chi.URLParam(r, "id")
	fn, bulundu := widgetlar[id]
	if !bulundu {
		httpx.WriteError(w, http.StatusNotFound, "widget bulunamadı")
		return
	}
	v, err := fn(r.Context(), h.DB, k)
	if err != nil {
		// Ham SQL hatası (tablo/sütun adı) istemciye gitmez.
		log.Printf("pano: widget %s okunamadı (tum=%v rid=%d): %v", id, k.Tum, k.RID, err)
		httpx.WriteError(w, http.StatusInternalServerError, "pano verisi alınamadı, lütfen tekrar deneyin")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"id": id, "veri": v})
}

const tarihBicim = "'%Y-%m-%d %H:%i:%s'"

// imlecZamani — örnekleyicinin bir işi en son ne zaman yaptığı (” = hiç).
func imlecZamani(ctx context.Context, db *sql.DB, metrik string) string {
	var s sql.NullString
	_ = db.QueryRowContext(ctx, `SELECT DATE_FORMAT(olcum_at,`+tarihBicim+`) FROM cp_esik_durum
		WHERE konu_tur='imlec' AND konu_id=0 AND metrik=?`, metrik).Scan(&s)
	return s.String
}

// ── egilim: son 30 günün günlük anlık görüntüsü (KPI değişim % + sparkline) ──

type gunNokta struct {
	Tarih    string `json:"tarih"`
	Hesap    int    `json:"hesap"`
	Askida   int    `json:"askida"`
	Yeni     int    `json:"yeni"`
	DiskKB   int64  `json:"disk_kb"`
	TrafikKB int64  `json:"trafik_kb"`
}

func wEgilim(ctx context.Context, db *sql.DB, k Kapsam) (any, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT DATE_FORMAT(tarih,'%Y-%m-%d'), hesap, askida, yeni, disk_kb, trafik_kb
		   FROM pano_gunluk WHERE kapsam_id=? AND tarih >= CURDATE() - INTERVAL 30 DAY ORDER BY tarih`, k.kimlik())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []gunNokta{}
	for rows.Next() {
		var n gunNokta
		if err := rows.Scan(&n.Tarih, &n.Hesap, &n.Askida, &n.Yeni, &n.DiskKB, &n.TrafikKB); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return map[string]any{"noktalar": out, "disk_olcum": imlecZamani(ctx, db, "disk_olcum")}, nil
}

// ── kota: efektif kotanın %85+'ını kullanan hesaplar (disk / aylık trafik) ──

type kotaSatir struct {
	ID         int64   `json:"id"`
	AlanAdi    string  `json:"alan_adi"`
	Metrik     string  `json:"metrik"` // disk | trafik
	Yuzde      float64 `json:"yuzde"`
	KullanimMB float64 `json:"kullanim_mb"`
	LimitMB    int64   `json:"limit_mb"`
}

// kotaSatirlari — kapsamdaki domainlerin disk/trafik kullanım yüzdeleri (eşik
// için örnekleyici de kullanır). esik>0 → yalnız o yüzdenin üstü.
func kotaSatirlari(ctx context.Context, db *sql.DB, k Kapsam, esik float64) ([]kotaSatir, error) {
	kos, arg := k.kosul("d")
	rows, err := db.QueryContext(ctx,
		`SELECT d.id, d.alan_adi, COALESCE(d.boyut_kb,0), COALESCE(d.disk_kota_mb,0), d.plan_id IS NOT NULL,
		        COALESCE(p.disk_kota_mb,0), COALESCE(d.trafik_kb,0), COALESCE(p.trafik_kota_mb,0)
		   FROM domains d LEFT JOIN service_plans p ON p.id=d.plan_id
		  WHERE `+kos+` AND COALESCE(d.is_demo,0)=0`, arg...) //nolint:gosec // G202: kos SABİT kapsam ifadesi; değer ? ile bağlanır.
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []kotaSatir{}
	for rows.Next() {
		var id, boyutKB, trafikKB int64
		var alan string
		var dDisk, pDisk int
		var planVar bool
		var pTrafik int64
		if err := rows.Scan(&id, &alan, &boyutKB, &dDisk, &planVar, &pDisk, &trafikKB, &pTrafik); err != nil {
			return nil, err
		}
		if lim := kaynaklimit.EfektifDiskMB(dDisk, planVar, pDisk); lim > 0 {
			if y := float64(boyutKB) * 100 / (float64(lim) * 1024); y >= esik {
				out = append(out, kotaSatir{id, alan, "disk", yuvarla(y), yuvarla(float64(boyutKB) / 1024), int64(lim)})
			}
		}
		if pTrafik > 0 {
			if y := float64(trafikKB) * 100 / (float64(pTrafik) * 1024); y >= esik {
				out = append(out, kotaSatir{id, alan, "trafik", yuvarla(y), yuvarla(float64(trafikKB) / 1024), pTrafik})
			}
		}
	}
	return out, rows.Err()
}

func yuvarla(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }

func wKota(ctx context.Context, db *sql.DB, k Kapsam) (any, error) {
	l, err := kotaSatirlari(ctx, db, k, float64(kotaEsikleri[0]))
	if err != nil {
		return nil, err
	}
	sort.SliceStable(l, func(i, j int) bool { return l[i].Yuzde > l[j].Yuzde })
	toplam := len(l)
	if len(l) > 10 {
		l = l[:10]
	}
	return map[string]any{"esik": kotaEsikleri[0], "toplam": toplam, "hesaplar": l, "disk_olcum": imlecZamani(ctx, db, "disk_olcum")}, nil
}

// ── ssl: 30 gün içinde dolacak / dolmuş sertifikalar ──

type sslSatir struct {
	ID       int64  `json:"id"`
	AlanAdi  string `json:"alan_adi"`
	KalanGun int    `json:"kalan_gun"`
}

func wSSL(ctx context.Context, db *sql.DB, k Kapsam) (any, error) {
	kos, arg := k.kosul("d")
	rows, err := db.QueryContext(ctx,
		`SELECT d.id, d.alan_adi, DATEDIFF(d.ssl_bitis, CURDATE())
		   FROM domains d
		  WHERE `+kos+` AND d.ssl_aktif=1 AND d.ssl_bitis IS NOT NULL AND d.ssl_bitis <= CURDATE() + INTERVAL 30 DAY
		    AND COALESCE(d.askida,0)=0 AND COALESCE(d.is_demo,0)=0
		  ORDER BY d.ssl_bitis, d.id LIMIT 10`, arg...) //nolint:gosec // G202: kos SABİT kapsam ifadesi.
	if err != nil {
		return nil, err
	}
	liste := []sslSatir{}
	for rows.Next() {
		var s sslSatir
		if err := rows.Scan(&s.ID, &s.AlanAdi, &s.KalanGun); err != nil {
			rows.Close()
			return nil, err
		}
		liste = append(liste, s)
	}
	rows.Close()
	var doldu, yakin, sslsiz int
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(d.ssl_aktif=1 AND d.ssl_bitis < CURDATE()),0),
		        COALESCE(SUM(d.ssl_aktif=1 AND d.ssl_bitis >= CURDATE() AND d.ssl_bitis <= CURDATE() + INTERVAL 14 DAY),0),
		        COALESCE(SUM(d.ssl_aktif=0 AND d.durum='aktif'),0)
		   FROM domains d WHERE `+kos+` AND COALESCE(d.askida,0)=0 AND COALESCE(d.is_demo,0)=0`, arg...).Scan(&doldu, &yakin, &sslsiz); err != nil { //nolint:gosec // G202: kos SABİT kapsam ifadesi.
		return nil, err
	}
	return map[string]any{"hesaplar": liste, "doldu": doldu, "yakin": yakin, "sslsiz": sslsiz,
		"son_tarama": imlecZamani(ctx, db, "ssl_tarama")}, nil
}

// ── askida: askıdaki hesaplar (son askı zamanı denetim kaydından) ──

type askiSatir struct {
	ID      int64  `json:"id"`
	AlanAdi string `json:"alan_adi"`
	Kaynak  string `json:"kaynak"`
	Zaman   string `json:"zaman"`
}

func wAskida(ctx context.Context, db *sql.DB, k Kapsam) (any, error) {
	kos, arg := k.kosul("d")
	// Denetim kaydı yalnız domainin GÜNCEL sahibinin kapsamından okunur
	// (a.reseller_id = d.reseller_id): devirden önceki sahibin kaydı karışmaz.
	rows, err := db.QueryContext(ctx,
		`SELECT d.id, d.alan_adi, COALESCE(d.askida_kaynak,''),
		        COALESCE((SELECT DATE_FORMAT(MAX(a.ts),`+tarihBicim+`) FROM audit_log a
		                   WHERE a.action='hosting.askiya_al' AND a.ok=1 AND a.target=d.alan_adi
		                     AND a.reseller_id=d.reseller_id),'') AS zaman
		   FROM domains d WHERE `+kos+` AND COALESCE(d.askida,0)=1
		  ORDER BY zaman='' , zaman DESC, d.id DESC LIMIT 10`, arg...) //nolint:gosec // G202: kos SABİT kapsam ifadesi.
	if err != nil {
		return nil, err
	}
	liste := []askiSatir{}
	for rows.Next() {
		var s askiSatir
		if err := rows.Scan(&s.ID, &s.AlanAdi, &s.Kaynak, &s.Zaman); err != nil {
			rows.Close()
			return nil, err
		}
		liste = append(liste, s)
	}
	rows.Close()
	var toplam int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM domains d WHERE `+kos+` AND COALESCE(d.askida,0)=1`, arg...).Scan(&toplam); err != nil { //nolint:gosec // G202: kos SABİT kapsam ifadesi.
		return nil, err
	}
	return map[string]any{"hesaplar": liste, "toplam": toplam}, nil
}

// ── isler: son taşıma işleri + yedek durumu (+ yöneticide toplu işler) ──

type isSatir struct {
	ID        int64  `json:"id"`
	Tur       string `json:"tur"`
	Durum     string `json:"durum"`
	Toplam    int    `json:"toplam"`
	Basarili  int    `json:"basarili"`
	Basarisiz int    `json:"basarisiz"`
	Zaman     string `json:"zaman"`
}

func isSatirlari(ctx context.Context, db *sql.DB, q string, arg ...any) ([]isSatir, error) {
	rows, err := db.QueryContext(ctx, q, arg...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []isSatir{}
	for rows.Next() {
		var s isSatir
		if err := rows.Scan(&s.ID, &s.Tur, &s.Durum, &s.Toplam, &s.Basarili, &s.Basarisiz, &s.Zaman); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// gecikmeKosulu — planlı yedeği zamanında alınmamış hesap (3 saat tolerans).
const gecikmeKosulu = `(d.last_backup_at IS NULL OR d.last_backup_at < NOW() - INTERVAL IF(d.backup_freq='weekly',7,1) DAY - INTERVAL 3 HOUR)`

func wIsler(ctx context.Context, db *sql.DB, k Kapsam) (any, error) {
	out := map[string]any{}
	// Taşıma: işin sahibi sahip_rid (0109). Yönetici hepsini, bayi yalnız kendi işlerini.
	tKos, tArg := "1=1", []any(nil)
	if !k.Tum {
		tKos, tArg = "sahip_rid = ?", []any{k.RID}
	}
	tas, err := isSatirlari(ctx, db,
		`SELECT id, kaynak_tip, durum, toplam, tamamlanan, basarisiz,
		        COALESCE(DATE_FORMAT(COALESCE(bitis, baslangic, olusturma),`+tarihBicim+`),'')
		   FROM tasima_isleri WHERE `+tKos+` ORDER BY id DESC LIMIT 5`, tArg...) //nolint:gosec // G202: tKos SABİT.
	if err != nil {
		return nil, err
	}
	out["tasima"] = tas

	// Yedek: kapsamdaki planlı yedekli hesaplar ve gecikenler (domains — birincil tablo).
	kos, arg := k.kosul("d")
	var planli, geciken int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(`+gecikmeKosulu+`),0) FROM domains d
		  WHERE `+kos+` AND d.backup_freq IN ('daily','weekly') AND COALESCE(d.askida,0)=0`, arg...).Scan(&planli, &geciken); err != nil { //nolint:gosec // G202: kos SABİT.
		return nil, err
	}
	yedek := map[string]any{"planli": planli, "geciken": geciken}
	if k.Tum {
		// Yönetici: sunucu geneli yedek işleri (backup_jobs sahipsiz → yalnız yönetici).
		isler, err := isSatirlari(ctx, db,
			`SELECT id, CONCAT(tur,':',islem), durum, toplam, basari, hata,
			        COALESCE(DATE_FORMAT(COALESCE(bitis, baslangic),`+tarihBicim+`),'')
			   FROM backup_jobs ORDER BY id DESC LIMIT 5`)
		if err != nil {
			return nil, err
		}
		yedek["isler"] = isler
		toplu, err := isSatirlari(ctx, db,
			`SELECT id, tip, durum, toplam, basari, hata,
			        COALESCE(DATE_FORMAT(COALESCE(bitis, baslangic),`+tarihBicim+`),'')
			   FROM cp_toplu_isler ORDER BY id DESC LIMIT 5`)
		if err != nil {
			return nil, err
		}
		out["toplu"] = toplu
	} else {
		// Bayi: son 7 gündeki yedek hatası bildirimleri — bildirim görünürlüğüyle
		// AYNI kural (reseller_id = kendi rid).
		var hata int
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM cp_bildirim b WHERE b.reseller_id = ? AND b.kategori='yedek'
			   AND b.seviye='kritik' AND b.created_at > NOW() - INTERVAL 7 DAY`, k.RID).Scan(&hata); err != nil {
			return nil, err
		}
		yedek["hata_7gun"] = hata
	}
	out["yedek"] = yedek
	return out, nil
}

// ── websec: Website Security bulgu özeti ──

type bulguSatir struct {
	DomainID int64  `json:"domain_id"`
	AlanAdi  string `json:"alan_adi"`
	Paket    string `json:"paket"`
	Surum    string `json:"surum"`
	CVE      string `json:"cve"`
	Onem     string `json:"onem"`
	Duzeltme string `json:"duzeltme"`
	Ilk      string `json:"ilk"`
}

func wWebsec(ctx context.Context, db *sql.DB, k Kapsam) (any, error) {
	kos, arg := k.kosul("d")
	sayim := map[string]int{"critical": 0, "high": 0, "medium": 0, "low": 0}
	rows, err := db.QueryContext(ctx,
		`SELECT LOWER(f.severity), COUNT(*) FROM cp_websec_findings f JOIN domains d ON d.id=f.domain_id
		  WHERE `+kos+` GROUP BY LOWER(f.severity)`, arg...) //nolint:gosec // G202: kos SABİT.
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			rows.Close()
			return nil, err
		}
		if _, ok := sayim[s]; ok {
			sayim[s] += n
		}
	}
	rows.Close()
	// Sunucu yolu (install_path) DÖNMEZ.
	rows, err = db.QueryContext(ctx,
		`SELECT d.id, d.alan_adi, f.package_name, f.installed_version, f.cve_id, LOWER(f.severity),
		        COALESCE(f.fixed_in,''), DATE_FORMAT(f.first_seen,`+tarihBicim+`)
		   FROM cp_websec_findings f JOIN domains d ON d.id=f.domain_id
		  WHERE `+kos+` AND LOWER(f.severity) IN ('critical','high')
		  ORDER BY f.first_seen DESC, f.id DESC LIMIT 5`, arg...) //nolint:gosec // G202: kos SABİT.
	if err != nil {
		return nil, err
	}
	son := []bulguSatir{}
	for rows.Next() {
		var b bulguSatir
		if err := rows.Scan(&b.DomainID, &b.AlanAdi, &b.Paket, &b.Surum, &b.CVE, &b.Onem, &b.Duzeltme, &b.Ilk); err != nil {
			rows.Close()
			return nil, err
		}
		son = append(son, b)
	}
	rows.Close()
	var sonTarama sql.NullString
	if err := db.QueryRowContext(ctx,
		`SELECT DATE_FORMAT(MAX(t.son_tarama),`+tarihBicim+`) FROM cp_websec_domain_tarama t JOIN domains d ON d.id=t.domain_id
		  WHERE `+kos, arg...).Scan(&sonTarama); err != nil { //nolint:gosec // G202: kos SABİT.
		return nil, err
	}
	return map[string]any{"sayim": sayim, "son": son, "son_tarama": sonTarama.String}, nil
}
