// Package bildirim — panel/müşteri bildirim akışı (cp_bildirim).
//
// 🔴 NEDEN GENEL: antivirüs bulgusu müşteriye/operatöre AKMALI — yoksa tarama
// sessizce loglanır, kimse görmez ("başarısızlık güven olarak render"). Ama
// bu paket yalnız AV'ye özel değil; kategori alanıyla her modül kullanabilir
// (SSL bitişi, yedek hatası, kota aşımı...).
//
// 🔴🔴 İZOLASYON: bir hosting hesabındaki zararlı dosya bildirimi YALNIZ o
// domainin SAHİBİNE görünür. Root/admin BAŞKA kiracının tespitini görmez;
// başka resellerlar da görmez. Görünürlük `okuyan.gorunurluk()` ile role göre
// süzülür:
//
//	admin    → yalnız panel-geneli / ana hesap bildirimleri (reseller_id NULL ya da 0)
//	reseller → yalnız KENDİ domainleri (reseller_id = kendi uid)
//	müşteri  → yalnız KENDİ domaini (domain_id = token domaini)
//
// Bu süzgeç OLMADAN cp_bildirim küresel okunuyordu → tam sunucu yolu dahil her
// kiracının tespiti admin bell'ine düşüyordu (gizlilik sızıntısı).
//
// 🔴🔴 OKUNDU GÖRÜNTÜLEYENE AİTTİR (cp_bildirim_okundu, göç 0105): bir domain
// bildirimini hem müşteri hem sahip (bayi ya da yönetici) görür. Okundu eskiden
// satırdaki TEK bir sütundu → müşteri "tümünü okundu" deyince bayinin/yöneticinin
// "Yedek başarısız", "Saldırı zinciri" rozetleri de düşüyordu (ele geçirilmiş bir
// müşteri hesabı sahibin kritik uyarısını susturabiliyordu). Artık her
// görüntüleyenin (rol + kimlik) kendi okundu kaydı var; bir rolün eylemi başka
// rolün uyarı durumunu değiştirmez.
//
// cp_bildirim.okundu sütunu KALIR ama anlamı "en az bir görüntüleyen okudu"dur:
// ekranda gösterilmez, yalnız "satır kapandı, yeni olay yeni satır açsın" diye
// bakan yazıcılar içindir (cmd/gosp-avajan AV toplama). Kimin okuduğu
// cp_bildirim_okundu'dadır.
package bildirim

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
)

type Handlers struct{ DB *sql.DB }

type Bildirim struct {
	ID       int64  `json:"id"`
	Seviye   string `json:"seviye"`
	Kategori string `json:"kategori"`
	Baslik   string `json:"baslik"`
	Mesaj    string `json:"mesaj"`
	DomainID *int64 `json:"domain_id"`
	RefTur   string `json:"ref_tur"`
	RefID    int64  `json:"ref_id"`
	Okundu   bool   `json:"okundu"` // İSTEYEN görüntüleyen için
	Tarih    string `json:"tarih"`
}

// Görüntüleyen türleri (cp_bildirim_okundu.okuyan_tur).
const (
	okuyanAdmin   = "admin"
	okuyanBayi    = "bayi"
	okuyanMusteri = "musteri"

	// yoneticiID — sistem tarafının (Yaz ön-işareti, 0105 göçü) yönetici kimliği.
	// auth.Login yönetici jetonunu HER ZAMAN uid=1 ("root") ile üretir; HTTP
	// tarafı jetondaki UserID'yi kullanır, bugün ikisi aynıdır.
	yoneticiID int64 = 1
)

// okuyan — bildirimi GÖRÜNTÜLEYEN kimlik; okundu durumu bu anahtarla tutulur.
//
//	admin   → id = users.id (jetondaki UserID)
//	bayi    → id = kendi users.id (jetondaki ResellerID)
//	müşteri → id = domains.id (jetondaki DomainID; o domainin tüm müşteri girişleri ortak)
type okuyan struct {
	tur string
	id  int64
}

// sahipOkuyan — domainin sahibi olan görüntüleyen (reseller_id 0 = ana hesap/yönetici).
func sahipOkuyan(resellerID int64) okuyan {
	if resellerID > 0 {
		return okuyan{okuyanBayi, resellerID}
	}
	return okuyan{okuyanAdmin, yoneticiID}
}

// gorunurluk — bu kimliğin görebildiği cp_bildirim satırları (takma ad `b`).
// Döndürülen koşul TEK bir SQL ifadesidir; değerler arg ile bağlanır.
func (o okuyan) gorunurluk() (string, []any) {
	switch o.tur {
	case okuyanAdmin:
		// Admin YALNIZ sahipsiz (panel-geneli) ve ana hesap bildirimlerini görür;
		// kiracı/bayi domain bildirimleri sahibine aittir (admin AV panelinden izler).
		// reseller_id=0: eski sürümün "Ana hesap"a devirde yazdığı değer (B4) —
		// 0105 göçü NULL'a çeker; bu kol yine de kaçağı yöneticiden saklamaz.
		return "(b.reseller_id IS NULL OR b.reseller_id = 0)", nil
	case okuyanBayi:
		return "b.reseller_id = ?", []any{o.id}
	default:
		return "b.domain_id = ?", []any{o.id}
	}
}

// okumadi — "`b` satırını bu kimlik henüz okumadı" koşulu.
func (o okuyan) okumadi() (string, []any) {
	return `NOT EXISTS (SELECT 1 FROM cp_bildirim_okundu k
		WHERE k.bildirim_id = b.id AND k.okuyan_tur = ? AND k.okuyan_id = ?)`, []any{o.tur, o.id}
}

// istekOkuyan — isteğin sahibi. ok=false → kimlik yok/yetkisiz.
func istekOkuyan(r *http.Request) (okuyan, bool) {
	if c := middleware.ClaimsFrom(r); c != nil {
		switch c.Role {
		case "admin":
			return okuyan{okuyanAdmin, c.UserID}, true
		case "reseller":
			return okuyan{okuyanBayi, c.ResellerID}, true
		}
	}
	if mc := middleware.MusteriClaimsFrom(r); mc != nil {
		return okuyan{okuyanMusteri, mc.DomainID}, true
	}
	return okuyan{}, false
}

// Liste — GET /api/v1/bildirimler?sadece_okunmamis=1&kategori=antivirus&domain_id=42
//
// domain_id: çağıranın görünürlük kapsamı İÇİNDE ek süzgeçtir (müşteri/alan adı
// panosu tek alan adının bildirimlerini ister). Kapsamla AND'lenir, asla
// genişletmez: kapsam dışı ya da var olmayan alan adı → boş liste. Geçersiz değer
// (sayı değil / ≤0) → boş liste (kapsamın tamamına düşülmez). `okunmamis`
// sayacı parametreden bağımsızdır: rozet her zaman kapsamın tamamını sayar.
func (h *Handlers) Liste(w http.ResponseWriter, r *http.Request) {
	o, ok := istekOkuyan(r)
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "yetkiniz yok")
		return
	}
	kos, scopeArg := o.gorunurluk()
	kosuller := []string{kos}
	// İlk iki argüman LEFT JOIN'in (bu görüntüleyenin okundu kaydı), sonra WHERE.
	arg := append([]any{o.tur, o.id}, scopeArg...)
	if r.URL.Query().Get("sadece_okunmamis") == "1" {
		kosuller = append(kosuller, "k.bildirim_id IS NULL")
	}
	if k := r.URL.Query().Get("kategori"); k != "" {
		kosuller = append(kosuller, "b.kategori=?")
		arg = append(arg, k)
	}
	if v := r.URL.Query().Get("domain_id"); v != "" {
		did, err := strconv.ParseInt(v, 10, 64)
		if err != nil || did <= 0 {
			// 🔴 Geçersiz süzgeç "süzgeçsiz" sayılmaz: çağıran tek alan adını
			// istedi; kapsamın tamamını dönmek yanlış alan adının bildirimlerini
			// o alan adınınmış gibi gösterirdi.
			kosuller = append(kosuller, "1=0")
		} else {
			kosuller = append(kosuller, "b.domain_id=?")
			arg = append(arg, did)
		}
	}
	// Tercih: görüntüleyenin kapattığı kategoriler gizlenir (tercih.go). Sayaç da
	// aynı süzgeci kullanır; rozet ile liste tutarlı kalır.
	tk, ta := tercihSuzgec(r.Context(), h.DB, o)
	if tk != "" {
		kosuller = append(kosuller, tk)
		arg = append(arg, ta...)
	}
	// limit: 1..100 (zil yalnız son birkaç kaydı ister); verilmez/geçersizse eski 200.
	limit := 200
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v >= 1 && v <= 100 {
		limit = v
	}
	//nolint:gosec // G202: joinAnd yalnız SABİT koşul parçalarını (gorunurluk() rol ifadesi + "k.bildirim_id IS NULL" + "b.kategori=?" + "b.domain_id=?" / "1=0") birleştirir; kullanıcı değerleri (kategori, domain_id, kimlik) ? ile arg üzerinden bağlanır. Sorguya kullanıcı girdisi enterpole edilmez.
	q := `SELECT b.id, b.seviye, b.kategori, b.baslik, b.mesaj, b.domain_id, b.ref_tur, b.ref_id,
	             k.bildirim_id IS NOT NULL,
	             DATE_FORMAT(b.created_at,'%Y-%m-%d %H:%i:%s')
	        FROM cp_bildirim b
	        LEFT JOIN cp_bildirim_okundu k
	          ON k.bildirim_id = b.id AND k.okuyan_tur = ? AND k.okuyan_id = ?
	       WHERE ` + joinAnd(kosuller) + ` ORDER BY b.created_at DESC, b.id DESC LIMIT ` + strconv.Itoa(limit)

	rows, err := h.DB.QueryContext(r.Context(), q, arg...) //nolint:gosec // G701: q yalnız SABİT parçalardan kurulur (bkz. yukarı); istek değerleri ? ile bağlanır.
	if err != nil {
		// Ham SQL hatası (tablo/şema/kilit ayrıntısı) istemciye gitmez; ayrıntı günlükte
		// (Okundu ile aynı ilke).
		log.Printf("bildirim: liste okunamadı (%s #%d): %v", o.tur, o.id, err)
		httpx.WriteError(w, http.StatusInternalServerError, "bildirimler alınamadı, lütfen tekrar deneyin")
		return
	}
	defer rows.Close()
	out := []Bildirim{}
	for rows.Next() {
		var b Bildirim
		var dom sql.NullInt64
		if rows.Scan(&b.ID, &b.Seviye, &b.Kategori, &b.Baslik, &b.Mesaj, &dom,
			&b.RefTur, &b.RefID, &b.Okundu, &b.Tarih) != nil {
			continue
		}
		if dom.Valid {
			b.DomainID = &dom.Int64
		}
		out = append(out, b)
	}
	// Okunmamış sayısı — AYNI kapsam + BU görüntüleyenin okundu kaydı (badge yalnız
	// kendi bildirimlerini ve kendi okumasını saymalı).
	okK, okArg := o.okumadi()
	sayArg := append(append([]any{}, scopeArg...), okArg...)
	sayQ := `SELECT COUNT(*) FROM cp_bildirim b WHERE ` + kos + ` AND ` + okK //nolint:gosec // G202: kos/okK SABİT ifadeler (gorunurluk/okumadi); değerler ? ile bağlanır.
	if tk != "" {
		sayQ += ` AND ` + tk
		sayArg = append(sayArg, ta...)
	}
	var okunmamis int
	_ = h.DB.QueryRowContext(r.Context(), sayQ, sayArg...).Scan(&okunmamis) //nolint:gosec // G701: sayQ SABİT parçalardan; değerler ? ile bağlanır.
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"bildirimler": out, "okunmamis": okunmamis})
}

// Okundu — POST /api/v1/bildirimler/{id}/okundu  (id=0 → kapsamdaki hepsi)
//
// Yalnız İSTEYEN görüntüleyen için okundu kaydı yazar; aynı satırı gören öteki
// rolün (müşteri ↔ bayi/yönetici) durumu değişmez.
func (h *Handlers) Okundu(w http.ResponseWriter, r *http.Request) {
	o, ok := istekOkuyan(r)
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "yetkiniz yok")
		return
	}
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err := okunduYaz(r.Context(), h.DB, o, id); err != nil {
		// Ham SQL hatası (tablo/kilit ayrıntısı) istemciye gitmez; ayrıntı günlükte.
		log.Printf("bildirim: okundu yazılamadı (%s #%d, id=%d): %v", o.tur, o.id, id, err)
		httpx.WriteError(w, http.StatusInternalServerError, "bildirimler güncellenemedi, lütfen tekrar deneyin")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// KilitDenemeSayisi — KilitDenemeli'nin bir işlemi en fazla kaç kez BAŞTAN koştuğu.
const KilitDenemeSayisi = 4

// KilitCakismasi — err, işlemin baştan yeniden denenmesi gereken bir InnoDB kilit
// hatası mı? 1213 (kilitlenme): InnoDB kurban seçtiği işlemin TAMAMINI geri alır;
// aynı bağlantıda sonraki ifadeler autocommit'te koşar ve Commit boş döner — hata
// yutulursa geri alınan değişiklik "başarılı" görünür (BLD-1). 1205 (kilit
// beklemesi aşımı): yalnız ifade geri alınır ama işlem yarımdır.
func KilitCakismasi(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && (me.Number == 1213 || me.Number == 1205)
}

// KilitDenemeli — fn'i kilit çakışmasında (KilitCakismasi) en fazla
// KilitDenemeSayisi kez, artan ve rastgele beklemeyle yeniden koşar. fn HER
// çağrıda kendi transaction'ını baştan açıp kapatmalıdır (geri alınmış işlemin
// devamı yoktur). Diğer hatalar ve ctx iptali hemen döner.
func KilitDenemeli(ctx context.Context, fn func() error) error {
	for deneme := 1; ; deneme++ {
		err := fn()
		if err == nil || !KilitCakismasi(err) || deneme >= KilitDenemeSayisi {
			return err
		}
		// Görünür olsun: sık görülürse kilit sırası yeniden bozulmuştur.
		log.Printf("db: kilit çakışması (deneme %d/%d), işlem yeniden deneniyor: %v", deneme, KilitDenemeSayisi, err)
		bekle := time.Duration(deneme*15+rand.IntN(25)) * time.Millisecond //nolint:gosec // G404: yeniden deneme titreşimi; güvenlikle ilgisi yok.
		t := time.NewTimer(bekle)
		select {
		case <-ctx.Done():
			t.Stop()
			return err
		case <-t.C:
		}
	}
}

// okunduParca — tek ifadedeki en fazla kimlik (yer tutucu sınırının çok altında).
const okunduParca = 500

// pkGuncelle — kilitlenmiş satırları YALNIZ birincil anahtarla güncelleme başı.
// 🔴 FORCE INDEX (PRIMARY) şart: "id IN (…)" listesi tablonun büyük kısmını
// tutunca iyileştirici tam tarama seçiyor (EXPLAIN: type=index) ve REPEATABLE-READ'de
// UPDATE taradığı HER satırı X kilitliyor — başka kapsamların satırları dahil;
// ayrık kapsamlı eşzamanlı "tümünü okundu"lar kilitlenmeye düşüyordu. Aynı
// nedenle "okundu = 0" gibi ek koşul yazılmaz (idx_bildirim_okundu taranırdı).
// PRIMARY her şemada vardır; ipucu hiçbir kurulumda hata veremez.
const pkGuncelle = `UPDATE cp_bildirim FORCE INDEX (PRIMARY) SET `

// okunduYaz — görüntüleyenin okundu kayıtları + "en az biri okudu" sütunu, tek
// işlemde. id=0 → görünür hepsi. Kilit çakışmasında işlem baştan denenir.
//
// 🔴 Kapsam ŞART: aksi hâlde bir kullanıcı görmediği bildirimi (başka kiracının)
// okundu işaretleyebilirdi. Kayıt yalnız görünür satırlar için yazılır.
func okunduYaz(ctx context.Context, db *sql.DB, o okuyan, id int64) error {
	return KilitDenemeli(ctx, func() error { return okunduYazTek(ctx, db, o, id) })
}

// okunduYazTek — okunduYaz'ın tek denemesi.
//
// 🔴 KİLİT SIRASI (BLD-1/BLD-2): eski sürüm önce INSERT IGNORE…SELECT ile
// cp_bildirim satırlarına PAYLAŞIMLI (S) kilit alıp ardından UPDATE ile aynı
// satırlarda DIŞLAYICI (X) kilide yükseltiyordu. Aynı satırları isteyen ikinci
// bir işlem (eşzamanlı "tümünü okundu", bayi+müşteri, sahiplik devrinin
// SahipDevri'si) ile bu S→X yükseltmesi kilitlenme (1213) üretiyordu: okundu
// isteği 500 alıyor, devir ise sessizce geri alınıyordu. "INSERT IGNORE" bunu
// önlemez — düşülen yer yinelenen anahtar değil kilittir.
//
// Artık: (1) kapsamdaki satırlar TEK taramada, en baştan X kilitlenir (SELECT …
// FOR UPDATE) — aynı satırları isteyen işlemler burada sıraya girer, yükseltme
// yok; (2) okundu kayıtları kilitli kimliklerle VALUES'tan yazılır — tabloyu
// NOT EXISTS ile tarayıp kilitlemek (paylaşımlı aralık kilidi) yok, yinelenen
// satırı INSERT IGNORE atlar; (3) bayrak birincil anahtarla güncellenir.
func okunduYazTek(ctx context.Context, db *sql.DB, o okuyan, id int64) error {
	kos, arg := o.gorunurluk()
	if id != 0 {
		kos += " AND b.id = ?"
		arg = append(append([]any{}, arg...), id)
	}
	kilitQ := `SELECT b.id FROM cp_bildirim b WHERE ` + kos + ` FOR UPDATE` //nolint:gosec // G202: kos SABİT ifade (gorunurluk + " AND b.id = ?"); değerler ? ile bağlanır.

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	idler, err := kimlikler(ctx, tx, kilitQ, arg...)
	if err != nil {
		return err
	}
	for bas := 0; bas < len(idler); bas += okunduParca {
		parca := idler[bas:min(bas+okunduParca, len(idler))]
		yer := strings.TrimSuffix(strings.Repeat("?,", len(parca)), ",")
		degerler := make([]any, 0, 3*len(parca))
		for _, b := range parca {
			degerler = append(degerler, b, o.tur, o.id)
		}
		isaretQ := `INSERT IGNORE INTO cp_bildirim_okundu (bildirim_id, okuyan_tur, okuyan_id) VALUES ` + //nolint:gosec // G202: yalnız "(?,?,?)" yer tutucu tekrarı; değerler ? ile bağlanır.
			strings.TrimSuffix(strings.Repeat("(?,?,?),", len(parca)), ",")
		if _, err := tx.ExecContext(ctx, isaretQ, degerler...); err != nil { //nolint:gosec // G701: isaretQ yalnız yer tutuculardan; değerler ? ile bağlanır.
			return err
		}
		if _, err := tx.ExecContext(ctx, pkGuncelle+`okundu = 1 WHERE id IN (`+yer+`)`, idAny(parca)...); err != nil { //nolint:gosec // G202/G701: yalnız yer tutucular; kimlikler ? ile bağlanır.
			return err
		}
	}
	return tx.Commit()
}

// kimlikler — tek sütunlu (BIGINT) sorgunun sonuçları.
func kimlikler(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, sorgu string, arg ...any) ([]int64, error) {
	rows, err := q.QueryContext(ctx, sorgu, arg...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func idAny(l []int64) []any {
	out := make([]any, len(l))
	for i, v := range l {
		out[i] = v
	}
	return out
}

// Yaz — modül-içi kullanım (AV, SSL, ...): bildirim oluştur. domainID>0 ise
// sahip reseller_id domains'ten çözülür ve kayda yazılır → görünürlük sahibine
// kilitlenir. domainID=0 → panel-geneli (admin).
//
// Gövde yazGenel'dedir (hedef.go): aynı tekilleştirme (görüntüleyene göre) +
// kiracıya görünen mesajda sunucu yolu maskesi (Guvenli). Zaman pencereli
// bastırma ve aktörün kendi eylemini okunmuş yazma yalnız YazOlay/YazBayi ile.
func Yaz(db *sql.DB, seviye, kategori, baslik, mesaj string, domainID int64, refTur string, refID int64) {
	YazOlay(db, Olay{Seviye: seviye, Kategori: kategori, Baslik: baslik, Mesaj: mesaj, RefTur: refTur, RefID: refID}, domainID, "", 0)
}

// ayniOkunmamis — o görüntüleyenin gördüğü ve henüz okumadığı birebir aynı
// bildirimlerin sayısı (COUNT satırı).
func ayniOkunmamis(db *sql.DB, o okuyan, kategori, baslik, refTur string, refID int64, dom any) *sql.Row {
	kos, arg := o.gorunurluk()
	okK, okArg := o.okumadi()
	args := append([]any{kategori, baslik, refTur, refID, dom}, arg...)
	args = append(args, okArg...)
	q := `SELECT COUNT(*) FROM cp_bildirim b WHERE b.kategori=? AND b.baslik=? AND b.ref_tur=? AND b.ref_id=? AND b.domain_id <=> ? AND ` + kos + ` AND ` + okK //nolint:gosec // G202: kos/okK SABİT ifadeler; değerler ? ile bağlanır.
	return db.QueryRow(q, args...)                                                                                                                              //nolint:gosec // G701: q SABİT parçalardan; değerler ? ile bağlanır.
}

// SahipDevri — domainin bayi sahipliği eskiResellerID → yeniResellerID
// değişirken (toplu "sahip" işlemi, internal/toplu/devir.go) AÇIK bildirimleri
// yeni sahibe taşır. 0 = ana hesap (yönetici).
//
// "Açık" = ESKİ SAHİBİN henüz okumadığı ve hâlâ onun gördüğü satır. Eski sahibin
// okudukları onun döneminin geçmişi olarak onda kalır; müşterinin okuması sahibin
// açık uyarısını kapatmaz (B3 ile aynı ilke).
//
// 🔴 B4: yeni sahip "Ana hesap" (0) iken reseller_id=0 YAZILMAZ, NULL yazılır —
// yöneticinin kapsamı NULL'dur; 0 yazılınca bildirimler yöneticiden de eski
// bayiden de kayboluyordu.
//
// ex genelde devrin transaction'ıdır (*sql.Tx) — sahiplik ile birlikte taşınır.
// Hata döndüğünde çağıran devri BAŞARISIZ saymalıdır: kilitlenme (1213) tüm
// transaction'ı geri almıştır (bkz. KilitCakismasi).
//
// 🔴 KİLİT SIRASI (BLD-1): okunduYazTek ile aynı ilke — önce ilgili cp_bildirim
// satırları X kilitlenir, cp_bildirim_okundu KİLİTSİZ okunur, sonra birincil
// anahtarla güncellenir. Eski tek UPDATE, NOT EXISTS alt sorgusuyla
// cp_bildirim_okundu'ya paylaşımlı aralık kilitleri koyuyordu; aynı aralığa okundu
// kaydı ekleyen eşzamanlı bir "tümünü okundu" ile kilitlenme döngüsü kurulabiliyordu.
//
// Kilitsiz okuma neden doğru: cp_bildirim_okundu → cp_bildirim yabancı anahtarı,
// bir satıra okundu kaydı ekleyen her işlemin o cp_bildirim satırında paylaşımlı
// kilidi commit'e dek tutmasını gerektirir; X kilidini aldığımız anda o satırın
// bütün okundu kayıtları commit edilmiştir ya da yenisi bizim commit'imizi bekler.
// REPEATABLE-READ'de okuma görüntüsü işlemdeki ilk kilitsiz okumada açılır —
// devret'te bu okuma, yani kilitlerden sonra. (Çağıran daha önce kilitsiz okuma
// yapmışsa arada okunan bir bildirim yeni sahibe taşınabilir; zararsız: domain artık onun.)
func SahipDevri(ctx context.Context, ex interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, domainID, eskiResellerID, yeniResellerID int64) error {
	if domainID <= 0 || eskiResellerID == yeniResellerID {
		return nil
	}
	eski := sahipOkuyan(eskiResellerID)
	kos, arg := eski.gorunurluk()
	// 🔴 DİZİN SIRASI: satırlar idx_bildirim_reseller üzerinden (eski sahibin
	// aralığı) kilitlenir — eski sahibin "tümünü okundu"su da AYNI dizini aynı
	// sırada tarar (önce dizin kaydı, sonra satır). idx_bildirim_domain ya da
	// index_merge ile kilitlenince devir önce SATIRI tutup ardından reseller_id
	// değişikliği için DİZİN kaydını istiyordu; okuyan tersini yapıyordu → 1213.
	// Ayrıca domainin başka sahiplerden kalma geçmiş satırlarına (yeni sahibin
	// okuduğu aralık) hiç dokunulmaz. Dizin (0082) yoksa ipucusuz sorguya düşülür.
	kilitQ := `SELECT b.id FROM cp_bildirim b FORCE INDEX (idx_bildirim_reseller) WHERE b.domain_id = ? AND ` + kos + ` FOR UPDATE` //nolint:gosec // G202: kos SABİT ifade (gorunurluk); değerler ? ile bağlanır.
	kilitArg := append([]any{domainID}, arg...)
	idler, err := kimlikler(ctx, ex, kilitQ, kilitArg...)
	var me *mysql.MySQLError
	if errors.As(err, &me) && me.Number == 1176 { // ER_KEY_DOES_NOT_EXITS: dizin yok (şema kayması)
		idler, err = kimlikler(ctx, ex, strings.Replace(kilitQ, ` FORCE INDEX (idx_bildirim_reseller)`, ``, 1), kilitArg...)
	}
	if err != nil || len(idler) == 0 {
		return err
	}
	okunan := map[int64]bool{}
	for bas := 0; bas < len(idler); bas += okunduParca {
		parca := idler[bas:min(bas+okunduParca, len(idler))]
		yer := strings.TrimSuffix(strings.Repeat("?,", len(parca)), ",")
		l, err := kimlikler(ctx, ex,
			`SELECT bildirim_id FROM cp_bildirim_okundu WHERE okuyan_tur = ? AND okuyan_id = ? AND bildirim_id IN (`+yer+`)`, //nolint:gosec // G202: yalnız yer tutucular; değerler ? ile bağlanır.
			append([]any{eski.tur, eski.id}, idAny(parca)...)...)
		if err != nil {
			return err
		}
		for _, v := range l {
			okunan[v] = true
		}
	}
	// "Açık" = eski sahibin henüz okumadığı; okudukları onun döneminin geçmişi olarak onda kalır.
	var acik []int64
	for _, v := range idler {
		if !okunan[v] {
			acik = append(acik, v)
		}
	}
	for bas := 0; bas < len(acik); bas += okunduParca {
		parca := acik[bas:min(bas+okunduParca, len(acik))]
		yer := strings.TrimSuffix(strings.Repeat("?,", len(parca)), ",")
		if _, err := ex.ExecContext(ctx, pkGuncelle+`reseller_id = NULLIF(?, 0) WHERE id IN (`+yer+`)`, //nolint:gosec // G202/G701: yalnız yer tutucular; değerler ? ile bağlanır.
			append([]any{yeniResellerID}, idAny(parca)...)...); err != nil {
			return err
		}
	}
	return nil
}

func joinAnd(l []string) string {
	out := ""
	for i, s := range l {
		if i > 0 {
			out += " AND "
		}
		out += s
	}
	return out
}

var _ = json.Marshal // (ileride)
