package dbgovernor

// api.go — salt-okunur görünürlük uçları.
//
//	GET /db-cpu/en-cok?sure=1s|24s&limit=N   en çok CPU harcayan veritabanları
//	GET /db-cpu/kisitlar?durum=aktif|tumu&sure=24s|7g   kısıtlar (etkin + geçmiş)
//	GET /db-cpu/durum                         kısıcı durumu + ayarlı mod (YALNIZ kök)
//	PUT /db-cpu/durum {"mod":"gozlem|uygula|kapali"}   modu değiştirir (YALNIZ kök; denetime yazılır)
//
// GÖRÜNÜRLÜK: kök hepsini görür (bayi=ID süzgeci isteğe bağlı); bayi YALNIZ kendi hostinglerini.
// Bayi kapsamı istemci parametresiyle DEĞİŞTİRİLEMEZ; kaynağı jetondur. Hosting sahipliği
// ölçüm anındaki değil GÜNCEL domains.reseller_id'dir (taşınan/silinen hostingin verisi eski
// sahibe sızmaz).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/auth"
	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
)

// claimsAl: isteğin kimliği (testler değiştirir; üretimde middleware.ClaimsFrom).
var claimsAl = middleware.ClaimsFrom

// aktorAl: denetim için çağıran (uid, ad); testler değiştirir.
var aktorAl = middleware.Aktor

// Handlers: HTTP uçları.
type Handlers struct {
	DB *sql.DB
	// Simdi: testler için; nil → time.Now.
	Simdi func() time.Time
	// Durum: kök /durum için; nil → üretimdeki Motor.
	Durum func() (Durum, bool)
}

func (h *Handlers) simdi() time.Time {
	if h.Simdi != nil {
		return h.Simdi()
	}
	return time.Now()
}

// kapsam: bayi jetonu → (bayiID, true); kök → (istemcinin istediği bayi süzgeci ya da 0, false).
// ok=false: kimlik yok / müşteri jetonu.
func kapsam(r *http.Request) (rid int64, bayi bool, ok bool) {
	return kapsamClaims(claimsAl(r), r.URL.Query().Get("bayi"))
}

func kapsamClaims(c *auth.Claims, bayiParam string) (rid int64, bayi bool, ok bool) {
	if c == nil {
		return 0, false, false
	}
	if c.Role == "reseller" || c.ResellerID > 0 {
		if c.ResellerID <= 0 {
			return 0, true, false // kimliksiz bayi jetonu: hiçbir şey gösterme
		}
		return c.ResellerID, true, true
	}
	if c.Role != "admin" {
		return 0, false, false
	}
	if bayiParam != "" {
		n, err := strconv.ParseInt(bayiParam, 10, 64)
		if err != nil || n < 0 {
			return 0, false, false
		}
		return n, false, true
	}
	return 0, false, true
}

func sureCoz(s string) (time.Duration, string) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "24s", "24h", "1g", "1d":
		return 24 * time.Hour, "24s"
	case "7g", "7d":
		return 7 * 24 * time.Hour, "7g"
	}
	return time.Hour, "1s"
}

// EnCokKayit: "en çok CPU harcayan veritabanı" satırı.
type EnCokKayit struct {
	DBKullanici   string   `json:"db_kullanici"`
	Veritabanlari []string `json:"veritabanlari"`
	DomainID      int64    `json:"domain_id"`
	AlanAdi       string   `json:"alan_adi"`
	BayiID        int64    `json:"bayi_id,omitempty"`
	CPUSn         float64  `json:"cpu_sn"`
	OrtCekirdek   float64  `json:"ort_cekirdek"`
	PayYuzde      float64  `json:"pay_yuzde"`
	Kisitli       bool     `json:"kisitli"`
}

// EnCok: GET /db-cpu/en-cok
func (h *Handlers) EnCok(w http.ResponseWriter, r *http.Request) {
	rid, bayi, ok := kapsam(r)
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "yetkiniz yok")
		return
	}
	sure, ad := sureCoz(r.URL.Query().Get("sure"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	now := h.simdi()
	basla := now.Add(-sure).Unix()

	kosul, args := "o.dilim >= ?", []any{basla}
	if bayi || rid > 0 {
		kosul += " AND d.reseller_id=?"
		args = append(args, rid)
	}
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT o.db_user, o.domain_id, d.alan_adi, d.reseller_id, SUM(o.cpu_ms)
		   FROM db_cpu_olcum o JOIN domains d ON d.id=o.domain_id
		  WHERE `+kosul+`
		  GROUP BY o.db_user, o.domain_id, d.alan_adi, d.reseller_id
		  ORDER BY SUM(o.cpu_ms) DESC, o.db_user LIMIT 1000`, args...)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "ölçümler okunamadı")
		return
	}
	var liste []EnCokKayit
	var toplamMs float64
	domSet := map[int64]bool{}
	for rows.Next() {
		var k EnCokKayit
		var ms int64
		var rd int64
		if err := rows.Scan(&k.DBKullanici, &k.DomainID, &k.AlanAdi, &rd, &ms); err != nil {
			rows.Close()
			httpx.WriteError(w, http.StatusInternalServerError, "ölçümler okunamadı")
			return
		}
		k.CPUSn = float64(ms) / 1000
		k.OrtCekirdek = k.CPUSn / sure.Seconds()
		toplamMs += float64(ms)
		if !bayi {
			k.BayiID = rd
		}
		domSet[k.DomainID] = true
		liste = append(liste, k)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "ölçümler okunamadı")
		return
	}
	for i := range liste {
		if toplamMs > 0 {
			liste[i].PayYuzde = liste[i].CPUSn * 1000 / toplamMs * 100
		}
	}
	if len(liste) > limit {
		liste = liste[:limit]
	}
	h.dbAdlariVeKisit(r, liste)
	if liste == nil {
		liste = []EnCokKayit{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"sure": ad, "pencere_sn": int(sure.Seconds()), "liste": liste,
		"toplam_cpu_sn": toplamMs / 1000,
	})
}

// dbAdlariVeKisit: satırlara veritabanı adlarını ve kısıt durumunu ekler (aynı kapsamdaki kayıtlardan).
func (h *Handlers) dbAdlariVeKisit(r *http.Request, liste []EnCokKayit) {
	if len(liste) == 0 {
		return
	}
	idx := map[string][]int{}
	users := make([]any, 0, len(liste))
	for i, k := range liste {
		if _, ok := idx[k.DBKullanici]; !ok {
			users = append(users, k.DBKullanici)
		}
		idx[k.DBKullanici] = append(idx[k.DBKullanici], i)
	}
	in := strings.TrimSuffix(strings.Repeat("?,", len(users)), ",")
	if rows, err := h.DB.QueryContext(r.Context(),
		`SELECT db_user, domain_id, db_name FROM db_accounts WHERE BINARY db_user IN (`+in+`) ORDER BY db_name`, users...); err == nil {
		for rows.Next() {
			var u, dn string
			var did int64
			if rows.Scan(&u, &did, &dn) != nil {
				continue
			}
			for _, i := range idx[u] {
				if liste[i].DomainID == did {
					liste[i].Veritabanlari = append(liste[i].Veritabanlari, dn)
				}
			}
		}
		rows.Close()
	}
	if rows, err := h.DB.QueryContext(r.Context(),
		`SELECT db_user FROM db_cpu_kisit WHERE acik=1 AND BINARY db_user IN (`+in+`)`, users...); err == nil {
		for rows.Next() {
			var u string
			if rows.Scan(&u) != nil {
				continue
			}
			for _, i := range idx[u] {
				liste[i].Kisitli = true
			}
		}
		rows.Close()
	}
	for i := range liste {
		if liste[i].Veritabanlari == nil {
			liste[i].Veritabanlari = []string{}
		}
		sort.Strings(liste[i].Veritabanlari)
	}
}

// KisitKayit: kısıt satırı.
type KisitKayit struct {
	ID              int64   `json:"id"`
	DBKullanici     string  `json:"db_kullanici"`
	DomainID        int64   `json:"domain_id"`
	AlanAdi         string  `json:"alan_adi"`
	BayiID          int64   `json:"bayi_id,omitempty"`
	Neden           string  `json:"neden"`
	Seviye          int     `json:"seviye"`
	OncekiBaglanti  int     `json:"onceki_baglanti"`
	KisitBaglanti   int     `json:"kisit_baglanti"`
	EsikCekirdek    float64 `json:"esik_cekirdek"`
	TepeCekirdek    float64 `json:"tepe_cekirdek"`
	OldurulenSorgu  int     `json:"oldurulen_sorgu"`
	KesilenBaglanti int     `json:"kesilen_baglanti"`
	Uygulandi       bool    `json:"uygulandi"`
	Aktif           bool    `json:"aktif"`
	Baslangic       string  `json:"baslangic"`
	SonAsim         string  `json:"son_asim"`
	Bitis           *string `json:"bitis"`
	BitisNeden      string  `json:"bitis_neden,omitempty"`
}

// Kisitlar: GET /db-cpu/kisitlar
func (h *Handlers) Kisitlar(w http.ResponseWriter, r *http.Request) {
	rid, bayi, ok := kapsam(r)
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "yetkiniz yok")
		return
	}
	sure, ad := sureCoz(r.URL.Query().Get("sure"))
	yalnizAktif := strings.ToLower(r.URL.Query().Get("durum")) != "tumu"
	now := h.simdi()

	// Kök, sahibi silinmiş hostingin kayıtlarını da görür (LEFT JOIN); bayi YALNIZ güncel sahibi olduğu hostingleri.
	join, kosul, args := " LEFT JOIN domains d ON d.id=k.domain_id", "1=1", []any{}
	if bayi || rid > 0 {
		join, kosul = " JOIN domains d ON d.id=k.domain_id", "d.reseller_id=?"
		args = append(args, rid)
	}
	if yalnizAktif {
		kosul += " AND k.acik=1"
	} else {
		kosul += " AND (k.acik=1 OR k.baslangic_ts >= ?)"
		args = append(args, now.Add(-sure).Unix())
	}
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT k.id, k.db_user, k.domain_id, COALESCE(d.alan_adi,k.alan_adi), COALESCE(d.reseller_id,k.reseller_id), k.neden, k.seviye,
		        k.onceki_baglanti, k.kisit_baglanti, k.esik_milli, k.tepe_milli, k.oldurulen, k.kesilen, k.uygulandi,
		        k.baslangic_ts, k.son_asim_ts, k.bitis_ts, COALESCE(k.bitis_neden,''), COALESCE(k.acik,0)
		   FROM db_cpu_kisit k`+join+` WHERE `+kosul+` ORDER BY k.baslangic_ts DESC, k.id DESC LIMIT 500`, args...)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "kısıtlar okunamadı")
		return
	}
	defer rows.Close()
	liste := []KisitKayit{}
	for rows.Next() {
		var k KisitKayit
		var esik, tepe, uyg, acik int64
		var bas, son int64
		var bit sql.NullInt64
		var rd int64
		if err := rows.Scan(&k.ID, &k.DBKullanici, &k.DomainID, &k.AlanAdi, &rd, &k.Neden, &k.Seviye, &k.OncekiBaglanti, &k.KisitBaglanti,
			&esik, &tepe, &k.OldurulenSorgu, &k.KesilenBaglanti, &uyg, &bas, &son, &bit, &k.BitisNeden, &acik); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "kısıtlar okunamadı")
			return
		}
		k.EsikCekirdek, k.TepeCekirdek = float64(esik)/1000, float64(tepe)/1000
		k.Uygulandi, k.Aktif = uyg != 0, acik != 0
		k.Baslangic = time.Unix(bas, 0).UTC().Format(time.RFC3339)
		k.SonAsim = time.Unix(son, 0).UTC().Format(time.RFC3339)
		if bit.Valid {
			s := time.Unix(bit.Int64, 0).UTC().Format(time.RFC3339)
			k.Bitis = &s
		}
		if !bayi {
			k.BayiID = rd
		}
		liste = append(liste, k)
	}
	if err := rows.Err(); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "kısıtlar okunamadı")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"sure": ad, "durum": map[bool]string{true: "aktif", false: "tumu"}[yalnizAktif], "liste": liste})
}

// DurumUcu: GET /db-cpu/durum (yalnız kök).
func (h *Handlers) DurumUcu(w http.ResponseWriter, r *http.Request) {
	_, bayi, ok := kapsam(r)
	if !ok || bayi {
		httpx.WriteError(w, http.StatusForbidden, "sadece yöneticiler için")
		return
	}
	var d Durum
	var var_ bool
	if h.Durum != nil {
		d, var_ = h.Durum()
	} else if m := genel.Load(); m != nil {
		d, var_ = m.DurumAl(), true
	}
	out := map[string]any{"calisiyor": var_, "varsayilan_mod": VarsayilanAyar().Mod}
	if h.DB != nil {
		if md, err := h.modOku(r.Context()); err == nil {
			out["mod_ayar"] = md
		}
	}
	if var_ {
		out["durum"] = d
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// modOku: kök ayarındaki mod (cp_ayarlar.db_kisici_mod); satır yok/bozuksa VARSAYILAN (gozlem).
func (h *Handlers) modOku(ctx context.Context) (Mod, error) {
	var v string
	err := h.DB.QueryRowContext(ctx, `SELECT deger FROM cp_ayarlar WHERE anahtar=?`, ModAnahtari).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return VarsayilanAyar().Mod, nil
	}
	if err != nil {
		return "", err
	}
	if md, ok := ModCoz(v); ok {
		return md, nil
	}
	return VarsayilanAyar().Mod, nil
}

// ModAyarla: PUT /db-cpu/durum  {"mod":"gozlem|uygula|kapali"} (yalnız kök). Kısıcı ayarı her
// turda yeniden okur: değişiklik en çok bir yoklama aralığı (varsayılan 15 sn) sonra etkinleşir.
// Gerçek bir değişiklik denetim kaydına yazılır; aynı değeri yeniden yazmak kayıt üretmez.
func (h *Handlers) ModAyarla(w http.ResponseWriter, r *http.Request) {
	_, bayi, ok := kapsam(r)
	if !ok || bayi {
		httpx.WriteError(w, http.StatusForbidden, "sadece yöneticiler için")
		return
	}
	var req struct {
		Mod string `json:"mod"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	yeni, gecerli := ModCoz(req.Mod)
	if !gecerli {
		httpx.WriteError(w, http.StatusBadRequest, "mod: kapali | gozlem | uygula")
		return
	}
	eski, err := h.modOku(r.Context())
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "mevcut mod okunamadı")
		return
	}
	if eski == yeni {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"mod": yeni, "onceki": eski, "degisti": false})
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO cp_ayarlar (anahtar, deger) VALUES (?, ?) ON DUPLICATE KEY UPDATE deger=VALUES(deger)`,
		ModAnahtari, string(yeni)); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "kayıt işlemi başarısız")
		return
	}
	uid, aktor := aktorAl(r)
	httpx.Denetim(h.DB, r, uid, aktor, "db.cpu_kisici_mod", "db-cpu", fmt.Sprintf("%s → %s", eski, yeni), 0, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"mod": yeni, "onceki": eski, "degisti": true})
}
