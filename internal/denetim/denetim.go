// Package denetim: audit_log okuma ucu.
//
// GORUNURLUK KURALI (tek yerde, atlanamaz — bkz. kapsamKur):
//   - kok (admin) → TUM kayitlar; istege bagli kapsam suzgeci
//   - bayi        → YALNIZ KENDI YAPTIKLARI (audit_log.actor_user_id = kendi users.id).
//     Kapsamindaki bir hostingde kokun, musterinin ya da sistemin yaptigi islemler
//     bayiye GOSTERILMEZ (kullanici karari 2026-09-25). Bu kosul istemciden gelen
//     parametreyle DEGISTIRILEMEZ.
package denetim

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"girginospanel/internal/auth"
	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
)

// claimsAl: istegin kimligi (testler degistirir; uretimde middleware.ClaimsFrom).
var claimsAl = middleware.ClaimsFrom

// errKapsam: yonetici kapsam parametresi cozulemedi.
var errKapsam = errors.New("geçersiz kapsam")

// bayiMi: bayi jetonu mu (rol ya da sahiplik anahtari — ikisinden biri yeter; biri
// eksik kalsa bile bayi yonetici gorunurlugune DUSMEZ).
func bayiMi(c *auth.Claims) bool {
	return c != nil && (c.Role == "reseller" || c.ResellerID > 0)
}

// kapsamKur: "kim neyi gorebilir" kosullari. Bayi → yalniz kendi yaptiklari; istemcinin
// kapsam parametresi YOK SAYILIR. Yonetici → kap "" hepsi, "kok" panel sahibi kapsami,
// sayi → o bayinin kapsami (kok dahil o kapsamdaki tum islemler).
func kapsamKur(c *auth.Claims, kap string) ([]string, []any, error) {
	kosul := []string{"1=1"}
	args := []any{}
	if bayiMi(c) {
		if c.UserID <= 0 {
			// Kimliksiz bayi jetonu: hicbir sey gostermemek, baskasininkini
			// gostermekten iyidir.
			return append(kosul, "1=0"), args, nil
		}
		return append(kosul, "a.actor_user_id=?"), append(args, c.UserID), nil
	}
	switch kap = strings.TrimSpace(kap); {
	case kap == "":
	case kap == "kok":
		kosul = append(kosul, "a.reseller_id=0")
	default:
		n, err := strconv.ParseInt(kap, 10, 64)
		if err != nil || n <= 0 {
			// Cozulemeyen kapsam sessizce "hepsi" olmasin — kullanici yanlis
			// veriye baktigini anlamaz.
			return nil, nil, errKapsam
		}
		kosul = append(kosul, "a.reseller_id=?")
		args = append(args, n)
	}
	return kosul, args, nil
}

type Handlers struct {
	DB *sql.DB
}

type Kayit struct {
	ID    int64  `json:"id"`
	Zaman string `json:"zaman"`
	// Aktor: işlemi YAPANIN adı — tek başına ("girgin"); müşteri adına yapılan
	// işlemde "kimin adına" Adina/AdinaAlan'dadır. Müşteri kaydında hostingin alan
	// adı (yoksa ""); ham "musteri:<id>" kimliği gösterilmez.
	Aktor    string `json:"aktor"`
	AktorRol string `json:"aktor_rol"`
	// Adina: işlem müşteri ADINA oturumda yapıldı (bayi/yönetici müşteri panelinde).
	Adina bool `json:"adina,omitempty"`
	// AdinaAlan: adına olunan hostingin alan adı (silinmişse "").
	AdinaAlan string `json:"adina_alan,omitempty"`
	IP        string `json:"ip"`
	Eylem     string `json:"eylem"`
	Hedef     string `json:"hedef"`
	Detay     string `json:"detay,omitempty"`
	Basarili  bool   `json:"basarili"`
	KapsamID  int64  `json:"kapsam_id"`
	KapsamAd  string `json:"kapsam_ad"` // "" = kök (panel sahibi)
}

// Liste: GET /denetim?ara=&eylem=&kapsam=&limit=&offset=
func (h *Handlers) Liste(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50 // sinirsiz LIMIT = DoS yuzeyi; ust sinir sabit
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	if offset < 0 {
		offset = 0
	}

	// Kosullar IKI kumede: kapsam (kim neyi gorebilir) + filtre (kullanici secimi).
	// Eylem listesi YALNIZ kapsamla uretilir; aksi halde bir eylem secildiginde
	// acilir liste tek secenege iner ve kullanici baska eyleme gecemezdi.
	// 🔴 Kapsam zorlamasi: bayi icin istemci parametresi YOK SAYILIR (kapsamKur).
	c := claimsAl(r)
	kapsamKosul, kapsamArgs, err := kapsamKur(c, q.Get("kapsam"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	var rid int64
	if bayiMi(c) {
		rid = c.ResellerID
	}
	kosul := append([]string{}, kapsamKosul...)
	args := append([]any{}, kapsamArgs...)
	if e := strings.TrimSpace(q.Get("eylem")); e != "" {
		kosul = append(kosul, "a.action=?")
		args = append(args, e)
	} else if tk := teknikEylemler(); len(tk) > 0 {
		// Teknik ara adımlar (ör. müşteri paneline giriş KODU üretimi) varsayılan
		// listede gizlenir: aynı girişi "Müşteri paneline girildi" kaydı anlatır.
		// Başarısız olanları görünür kalır; süzgeçte açıkça seçilince hepsi görünür.
		kosul = append(kosul, "NOT (a.action IN (?"+strings.Repeat(",?", len(tk)-1)+") AND a.ok=1)")
		for _, k := range tk {
			args = append(args, k)
		}
	}
	if ara := strings.TrimSpace(q.Get("ara")); ara != "" {
		kosul = append(kosul, "(a.actor_username LIKE ? OR a.target LIKE ? OR a.ip LIKE ? OR a.action LIKE ?)")
		k := "%" + ara + "%"
		args = append(args, k, k, k, k)
	}
	where := " WHERE " + strings.Join(kosul, " AND ")
	kapsamWhere := " WHERE " + strings.Join(kapsamKosul, " AND ")

	var toplam int64
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM audit_log a`+where, args...).Scan(&toplam); err != nil {
		// Sessiz 0 = sayfalama tamamen gizlenir, kullanici 50 kaydi "hepsi" sanir.
		log.Printf("denetim: toplam sayilamadi: %v", err)
	}

	//nolint:gosec // G202: where = SABİT koşul parçalarının (kapsam + "a.action=?" + LIKE ? bloğu) " AND " birleşimidir; kapsam parametresi ParseInt ile int'e çevrilir, tüm kullanıcı değerleri args ile ? bağlanır.
	sorgu := `SELECT a.id, DATE_FORMAT(a.ts,'%Y-%m-%d %H:%i:%s'), a.actor_username,
	                 COALESCE(a.adina_domain_id,0), COALESCE(au.role,''), a.ip, a.action, COALESCE(a.target,''),
	                 COALESCE(a.detail,''), a.ok, a.reseller_id, COALESCE(ru.username,'')
	            FROM audit_log a
	            LEFT JOIN users au ON au.id = a.actor_user_id
	            LEFT JOIN users ru ON ru.id = a.reseller_id` + where + `
	           ORDER BY a.id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := h.DB.QueryContext(r.Context(), sorgu, args...)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "denetim kaydı okunamadı: "+err.Error())
		return
	}
	defer rows.Close()

	out := make([]Kayit, 0, limit)
	// alanIcin: kaydın gösterimi için alan adı gereken hosting id'leri (adına olunan
	// hosting ya da müşteri aktörü) → kayıt sıraları. Tek sorguda çözülür.
	type alanIhtiyac struct {
		i     int
		adina bool
	}
	alanIcin := map[int64][]alanIhtiyac{}
	for rows.Next() {
		var k Kayit
		var ok int
		var detay string
		var adinaID int64
		if err := rows.Scan(&k.ID, &k.Zaman, &k.Aktor, &adinaID, &k.AktorRol, &k.IP, &k.Eylem,
			&k.Hedef, &detay, &ok, &k.KapsamID, &k.KapsamAd); err != nil {
			log.Printf("denetim: satir okunamadi: %v", err)
			continue
		}
		k.Basarili = ok == 1
		k.Detay = notSade(k.Eylem, notCikar(detay))
		if adinaID == 0 {
			// 0114 öncesi kayıt: "aktör (adına musteri:<id>)" tek dizgede.
			k.Aktor, adinaID = httpx.AktorAyir(k.Aktor)
		}
		if adinaID > 0 {
			k.Adina = true
			alanIcin[adinaID] = append(alanIcin[adinaID], alanIhtiyac{len(out), true})
		}
		// users satiri yok: ya musteri token'i (aktor "musteri:<id>"), ya panelin
		// kendi arka-plan isi ("sistem"), ya da silinmis bir hesap.
		if k.AktorRol == "" {
			switch {
			case strings.HasPrefix(k.Aktor, "musteri:"):
				k.AktorRol = "müşteri"
				id, _ := strconv.ParseInt(strings.TrimPrefix(k.Aktor, "musteri:"), 10, 64)
				k.Aktor = "" // ham müşteri kimliği gösterilmez; alan adıyla doldurulur
				if id > 0 {
					alanIcin[id] = append(alanIcin[id], alanIhtiyac{len(out), false})
				}
			case k.Aktor == "sistem":
				k.AktorRol = "sistem"
			case k.Aktor != "":
				k.AktorRol = "silinmiş"
			}
		}
		out = append(out, k)
	}
	if len(alanIcin) > 0 {
		ids := make([]any, 0, len(alanIcin))
		for id := range alanIcin {
			ids = append(ids, id)
		}
		//nolint:gosec // G202: yalnız "?" yer tutucuları birleştirilir; değerler args ile bağlanır.
		if ar, err := h.DB.QueryContext(r.Context(),
			`SELECT id, alan_adi FROM domains WHERE id IN (?`+strings.Repeat(",?", len(ids)-1)+`)`, ids...); err == nil {
			for ar.Next() {
				var id int64
				var alan string
				if ar.Scan(&id, &alan) != nil {
					continue
				}
				for _, ih := range alanIcin[id] {
					if ih.adina {
						out[ih.i].AdinaAlan = alan
					} else {
						out[ih.i].Aktor = alan
					}
				}
			}
			_ = ar.Close()
		}
	}

	// Suzgec kutusu icin bu KAPSAMDAKI eylem turleri (secili eylem listeyi daraltmaz)
	eylemler := make([]string, 0, 24)
	if er, err := h.DB.QueryContext(r.Context(),
		`SELECT DISTINCT a.action FROM audit_log a`+kapsamWhere+` ORDER BY a.action`, kapsamArgs...); err == nil { //nolint:gosec // G202: kapsamWhere = SABİT kapsam parçalarının " AND " birleşimi; kapsamArgs değerleri ? bağlanır, kullanıcı girdisi enterpole edilmez.
		defer er.Close()
		for er.Next() {
			var e string
			if er.Scan(&e) == nil {
				eylemler = append(eylemler, e)
			}
		}
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"toplam": toplam, "limit": limit, "offset": offset,
		"kayitlar": out, "eylemler": eylemler,
		// Ön yüzün TEK etiket kaynağı: listedeki ve süzgeçteki kodların TR/EN adları.
		"etiketler": etiketHaritasi(out, eylemler),
		"kapsam":    map[string]any{"bayi_mi": bayiMi(c), "bayi_id": rid},
	})
}

// notCikar: detail JSON'unu okunur metne cevirir.
//
//	{"not":"..."}      → duz metin
//	{"a":1,"b":"x"}    → "a=1 · b=x" (sistem kayitlari zengin JSON yaziyor;
//	                     ham JSON tabloda okunmuyordu)
func notCikar(detay string) string {
	d := strings.TrimSpace(detay)
	if d == "" {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(d), &m); err != nil {
		return d
	}
	if not, ok := m["not"].(string); ok && len(m) == 1 {
		return not
	}
	anahtarlar := make([]string, 0, len(m))
	for k := range m {
		anahtarlar = append(anahtarlar, k)
	}
	sort.Strings(anahtarlar)
	parcalar := make([]string, 0, len(anahtarlar))
	for _, k := range anahtarlar {
		v := fmt.Sprintf("%v", m[k])
		if len(v) > 160 { // cok uzun aciklamalar tabloyu bozmasin
			v = v[:157] + "…"
		}
		parcalar = append(parcalar, k+"="+v)
	}
	return strings.Join(parcalar, " · ")
}

// etiketHaritasi: yanıttaki kayıtların ve süzgeç seçeneklerinin eylem etiketleri
// (bilinmeyen kodlar okunur geri dönüşle).
func etiketHaritasi(kayitlar []Kayit, eylemler []string) map[string]EylemEtiket {
	m := make(map[string]EylemEtiket, len(eylemler)+len(kayitlar))
	for _, e := range eylemler {
		m[e] = eylemEtiketi(e)
	}
	for _, k := range kayitlar {
		if _, ok := m[k.Eylem]; !ok {
			m[k.Eylem] = eylemEtiketi(k.Eylem)
		}
	}
	return m
}
