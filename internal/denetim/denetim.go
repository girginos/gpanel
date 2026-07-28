// Package denetim: audit_log okuma ucu.
//
// GORUNURLUK KURALI (tek yerde, atlanamaz):
//   - kok (admin) → TUM kayitlar; istege bagli kapsam suzgeci
//   - bayi        → YALNIZ kendi kapsami (audit_log.reseller_id = kendi users.id)
//     Bu kosul istemciden gelen parametreyle DEGISTIRILEMEZ.
package denetim

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
)

type Handlers struct {
	DB *sql.DB
}

type Kayit struct {
	ID       int64  `json:"id"`
	Zaman    string `json:"zaman"`
	Aktor    string `json:"aktor"`
	AktorRol string `json:"aktor_rol"`
	IP       string `json:"ip"`
	Eylem    string `json:"eylem"`
	Hedef    string `json:"hedef"`
	Detay    string `json:"detay,omitempty"`
	Basarili bool   `json:"basarili"`
	KapsamID int64  `json:"kapsam_id"`
	KapsamAd string `json:"kapsam_ad"` // "" = kök (panel sahibi)
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
	kapsamKosul := []string{"1=1"}
	kapsamArgs := []any{}

	// 🔴 Kapsam zorlamasi: bayi icin istemci parametresi YOK SAYILIR.
	rid := middleware.ResellerIDFrom(r)
	if rid > 0 {
		kapsamKosul = append(kapsamKosul, "a.reseller_id=?")
		kapsamArgs = append(kapsamArgs, rid)
	} else if kap := strings.TrimSpace(q.Get("kapsam")); kap != "" {
		switch {
		case kap == "kok":
			kapsamKosul = append(kapsamKosul, "a.reseller_id=0")
		default:
			n, err := strconv.ParseInt(kap, 10, 64)
			if err != nil || n <= 0 {
				// Cozulemeyen kapsam sessizce "hepsi" olmasin — kullanici yanlis
				// veriye baktigini anlamaz.
				httpx.WriteError(w, http.StatusBadRequest, "geçersiz kapsam")
				return
			}
			kapsamKosul = append(kapsamKosul, "a.reseller_id=?")
			kapsamArgs = append(kapsamArgs, n)
		}
	}
	kosul := append([]string{}, kapsamKosul...)
	args := append([]any{}, kapsamArgs...)
	if e := strings.TrimSpace(q.Get("eylem")); e != "" {
		kosul = append(kosul, "a.action=?")
		args = append(args, e)
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

	sorgu := `SELECT a.id, DATE_FORMAT(a.ts,'%Y-%m-%d %H:%i:%s'), a.actor_username,
	                 COALESCE(au.role,''), a.ip, a.action, COALESCE(a.target,''),
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
	for rows.Next() {
		var k Kayit
		var ok int
		var detay string
		if err := rows.Scan(&k.ID, &k.Zaman, &k.Aktor, &k.AktorRol, &k.IP, &k.Eylem,
			&k.Hedef, &detay, &ok, &k.KapsamID, &k.KapsamAd); err != nil {
			log.Printf("denetim: satir okunamadi: %v", err)
			continue
		}
		k.Basarili = ok == 1
		k.Detay = notCikar(detay)
		// users satiri yok: ya musteri token'i (aktor "musteri:<id>"), ya panelin
		// kendi arka-plan isi ("sistem"), ya da silinmis bir hesap.
		if k.AktorRol == "" {
			switch {
			case strings.HasPrefix(k.Aktor, "musteri:"):
				k.AktorRol = "müşteri"
			case k.Aktor == "sistem":
				k.AktorRol = "sistem"
			case k.Aktor != "":
				k.AktorRol = "silinmiş"
			}
		}
		out = append(out, k)
	}

	// Suzgec kutusu icin bu KAPSAMDAKI eylem turleri (secili eylem listeyi daraltmaz)
	eylemler := make([]string, 0, 24)
	if er, err := h.DB.QueryContext(r.Context(),
		`SELECT DISTINCT a.action FROM audit_log a`+kapsamWhere+` ORDER BY a.action`, kapsamArgs...); err == nil {
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
		"kapsam": map[string]any{"bayi_mi": rid > 0, "bayi_id": rid},
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
