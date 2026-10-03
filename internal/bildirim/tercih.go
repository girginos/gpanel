package bildirim

// tercih.go — bildirim tercihleri (göç 0127 cp_bildirim_tercih) + okunmamış sayacı.
//
// Tercih GÖRÜNTÜLEYENE aittir (okuyan: rol + kimlik, okundu kaydıyla aynı sözlük):
// müşterinin "yedek" kategorisini kapatması bayinin yedek uyarılarını etkilemez.
// Kapalı kategori listede ve sayaçta GİZLENİR; cp_bildirim satırı paylaşımlıdır,
// silinmez (öteki görüntüleyenler görmeye devam eder).
//
// E-posta kanalı YOK: panelde giden posta altyapısı yok (tasarım 1.2) — yalnız
// panel içi. Yanıttaki eposta_kullanilabilir=false bunu arayüze söyler.

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"girginospanel/internal/httpx"
)

// Kategori — tercih sözlüğündeki olay türü.
type Kategori struct {
	Kod    string   `json:"kod"`
	Roller []string `json:"-"`
}

// Kategoriler — olay türü sözlüğü (üreticiler bu kodları yazar). Roller: o türü
// görebilecek görüntüleyenler; tercih ekranı yalnız bunları listeler.
var Kategoriler = []Kategori{
	// kota: eşik bildirimleri müşteriye gitmez (musteriyeKapali) → müşteri tercih ekranında yok.
	{"kota", []string{okuyanAdmin, okuyanBayi}},
	{"ssl", []string{okuyanAdmin, okuyanBayi, okuyanMusteri}},
	{"aski", []string{okuyanAdmin, okuyanBayi, okuyanMusteri}},
	{"yedek", []string{okuyanAdmin, okuyanBayi, okuyanMusteri}},
	{"websec", []string{okuyanAdmin, okuyanBayi, okuyanMusteri}},
	{"antivirus", []string{okuyanAdmin, okuyanBayi, okuyanMusteri}},
	{"zincir", []string{okuyanAdmin, okuyanBayi, okuyanMusteri}},
	{"tasima", []string{okuyanAdmin, okuyanBayi}},
	{"toplu", []string{okuyanAdmin}},
	{"sistem", []string{okuyanAdmin}},
}

func kategoriVar(kod string, tur string) bool {
	for _, k := range Kategoriler {
		if k.Kod == kod {
			for _, r := range k.Roller {
				if r == tur {
					return true
				}
			}
		}
	}
	return false
}

// kapaliKategoriler — görüntüleyenin kapattığı kategoriler. Okunamazsa (0127
// göçü yok / DB hatası) boş: süzgeç uygulanmaz, bildirim GİZLENMEZ (fail-open
// burada doğru taraf — uyarıyı saklamak, göstermekten kötüdür).
func kapaliKategoriler(ctx context.Context, db *sql.DB, o okuyan) []string {
	rows, err := db.QueryContext(ctx,
		`SELECT kategori FROM cp_bildirim_tercih WHERE okuyan_tur=? AND okuyan_id=? AND panel=0`, o.tur, o.id)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if rows.Scan(&k) == nil {
			out = append(out, k)
		}
	}
	return out
}

// tercihSuzgec — "b.kategori NOT IN (?,…)" koşulu ve argümanları (kapalı yoksa boş).
func tercihSuzgec(ctx context.Context, db *sql.DB, o okuyan) (string, []any) {
	k := kapaliKategoriler(ctx, db, o)
	if len(k) == 0 {
		return "", nil
	}
	arg := make([]any, len(k))
	for i, v := range k {
		arg[i] = v
	}
	return "b.kategori NOT IN (" + strings.TrimSuffix(strings.Repeat("?,", len(k)), ",") + ")", arg
}

// Sayac — GET /api/v1/bildirimler/sayac → {okunmamis, kritik}. Zil bunu yoklar
// (tam liste yerine): aynı görünürlük + bu görüntüleyenin okundu kaydı + tercihler.
func (h *Handlers) Sayac(w http.ResponseWriter, r *http.Request) {
	o, ok := istekOkuyan(r)
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "yetkiniz yok")
		return
	}
	kos, arg := o.gorunurluk()
	okK, okArg := o.okumadi()
	arg = append(append([]any{}, arg...), okArg...)
	q := `SELECT COUNT(*), COALESCE(SUM(b.seviye='kritik'),0) FROM cp_bildirim b WHERE ` + kos + ` AND ` + okK
	if tk, ta := tercihSuzgec(r.Context(), h.DB, o); tk != "" {
		q += ` AND ` + tk
		arg = append(arg, ta...)
	}
	var n, kritik int
	if err := h.DB.QueryRowContext(r.Context(), q, arg...).Scan(&n, &kritik); err != nil { //nolint:gosec // G701: q SABİT parçalardan (gorunurluk/okumadi/yer tutucular); değerler ? ile bağlanır.
		log.Printf("bildirim: sayaç okunamadı (%s #%d): %v", o.tur, o.id, err)
		httpx.WriteError(w, http.StatusInternalServerError, "bildirimler alınamadı, lütfen tekrar deneyin")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]int{"okunmamis": n, "kritik": kritik})
}

// TercihSatiri — tercih ekranındaki bir kategori.
type TercihSatiri struct {
	Kategori string `json:"kategori"`
	Panel    bool   `json:"panel"`
}

// TercihGetir — GET /api/v1/bildirim-tercihleri → görüntüleyenin rolüne açık
// kategoriler ve panel içi açık/kapalı durumu.
func (h *Handlers) TercihGetir(w http.ResponseWriter, r *http.Request) {
	o, ok := istekOkuyan(r)
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "yetkiniz yok")
		return
	}
	kapali := map[string]bool{}
	for _, k := range kapaliKategoriler(r.Context(), h.DB, o) {
		kapali[k] = true
	}
	out := []TercihSatiri{}
	for _, k := range Kategoriler {
		if kategoriVar(k.Kod, o.tur) {
			out = append(out, TercihSatiri{Kategori: k.Kod, Panel: !kapali[k.Kod]})
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"tercihler": out, "eposta_kullanilabilir": false})
}

// TercihKaydet — PUT /api/v1/bildirim-tercihleri {"tercihler":[{kategori,panel}]}.
// Yalnız rolün görebildiği sözlük kategorileri kabul edilir; bilinmeyen → 400.
// Kayıt YALNIZ istek sahibinin (jetondaki kimlik) satırlarına yazılır.
func (h *Handlers) TercihKaydet(w http.ResponseWriter, r *http.Request) {
	o, ok := istekOkuyan(r)
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "yetkiniz yok")
		return
	}
	var g struct {
		Tercihler []TercihSatiri `json:"tercihler"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&g); err != nil || len(g.Tercihler) == 0 || len(g.Tercihler) > len(Kategoriler) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz tercih listesi")
		return
	}
	for _, t := range g.Tercihler {
		if !kategoriVar(t.Kategori, o.tur) {
			httpx.WriteError(w, http.StatusBadRequest, "bilinmeyen bildirim türü")
			return
		}
	}
	for _, t := range g.Tercihler {
		panel := 0
		if t.Panel {
			panel = 1
		}
		if _, err := h.DB.ExecContext(r.Context(),
			`INSERT INTO cp_bildirim_tercih (okuyan_tur, okuyan_id, kategori, panel) VALUES (?,?,?,?)
			 ON DUPLICATE KEY UPDATE panel=VALUES(panel)`, o.tur, o.id, t.Kategori, panel); err != nil {
			log.Printf("bildirim: tercih yazılamadı (%s #%d): %v", o.tur, o.id, err)
			httpx.WriteError(w, http.StatusInternalServerError, "tercihler kaydedilemedi, lütfen tekrar deneyin")
			return
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
