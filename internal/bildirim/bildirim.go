// Package bildirim — panel/müşteri bildirim akışı (cp_bildirim).
//
// 🔴 NEDEN GENEL: antivirüs bulgusu müşteriye/operatöre AKMALI — yoksa tarama
// sessizce loglanır, kimse görmez ("başarısızlık güven olarak render"). Ama
// bu paket yalnız AV'ye özel değil; kategori alanıyla her modül kullanabilir
// (SSL bitişi, yedek hatası, kota aşımı...).
//
// 🔴🔴 İZOLASYON: bir hosting hesabındaki zararlı dosya bildirimi YALNIZ o
// domainin SAHİBİNE görünür. Root/admin BAŞKA kiracının tespitini görmez;
// başka resellerlar da görmez. Görünürlük `kapsam()` ile role göre süzülür:
//   admin    → yalnız panel-geneli bildirimler (reseller_id IS NULL)
//   reseller → yalnız KENDİ domainleri (reseller_id = kendi uid)
//   müşteri  → yalnız KENDİ domaini (domain_id = token domaini)
// Bu süzgeç OLMADAN cp_bildirim küresel okunuyordu → tam sunucu yolu dahil her
// kiracının tespiti admin bell'ine düşüyordu (gizlilik sızıntısı).
package bildirim

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

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
	Okundu   bool   `json:"okundu"`
	Tarih    string `json:"tarih"`
}

// kapsam — istek sahibinin rolüne göre cp_bildirim görünürlük WHERE parçası.
// ok=false → kimlik yok/yetkisiz. Döndürülen kosul TEK bir SQL ifadesidir;
// argümanları arg ile gelir (admin'de argümansız).
func kapsam(r *http.Request) (kosul string, arg []any, ok bool) {
	if c := middleware.ClaimsFrom(r); c != nil {
		switch c.Role {
		case "admin":
			// Admin YALNIZ sahipsiz (panel-geneli) bildirimleri görür; kiracı/domain
			// bildirimleri sahibine aittir. Kiracı zararlısını admin buradan GÖRMEZ
			// (admin AV panelinden izler).
			return "reseller_id IS NULL", nil, true
		case "reseller":
			return "reseller_id = ?", []any{c.ResellerID}, true
		}
	}
	if mc := middleware.MusteriClaimsFrom(r); mc != nil {
		return "domain_id = ?", []any{mc.DomainID}, true
	}
	return "", nil, false
}

// Liste — GET /api/v1/bildirimler?sadece_okunmamis=1&kategori=antivirus
func (h *Handlers) Liste(w http.ResponseWriter, r *http.Request) {
	kos, scopeArg, ok := kapsam(r)
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "yetkiniz yok")
		return
	}
	kosuller := []string{kos}
	arg := append([]any{}, scopeArg...)
	if r.URL.Query().Get("sadece_okunmamis") == "1" {
		kosuller = append(kosuller, "okundu=0")
	}
	if k := r.URL.Query().Get("kategori"); k != "" {
		kosuller = append(kosuller, "kategori=?")
		arg = append(arg, k)
	}
	q := `SELECT id, seviye, kategori, baslik, mesaj, domain_id, ref_tur, ref_id, okundu,
	             DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
	        FROM cp_bildirim WHERE ` + joinAnd(kosuller) + ` ORDER BY created_at DESC LIMIT 200`

	rows, err := h.DB.QueryContext(r.Context(), q, arg...)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
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
	// Okunmamış sayısı — AYNI kapsam (badge yalnız kendi bildirimlerini saymalı).
	var okunmamis int
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM cp_bildirim WHERE `+kos+` AND okundu=0`, scopeArg...).Scan(&okunmamis)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"bildirimler": out, "okunmamis": okunmamis})
}

// Okundu — POST /api/v1/bildirimler/{id}/okundu  (id=0 → kapsamdaki hepsi)
func (h *Handlers) Okundu(w http.ResponseWriter, r *http.Request) {
	kos, scopeArg, ok := kapsam(r)
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "yetkiniz yok")
		return
	}
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var err error
	if id == 0 {
		// 🔴 Kapsam ŞART: aksi hâlde bir kullanıcı HERKESİN bildirimini okundu yapardı.
		_, err = h.DB.Exec(`UPDATE cp_bildirim SET okundu=1 WHERE `+kos+` AND okundu=0`, scopeArg...)
	} else {
		args := append(append([]any{}, scopeArg...), id)
		_, err = h.DB.Exec(`UPDATE cp_bildirim SET okundu=1 WHERE `+kos+` AND id=?`, args...)
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Yaz — modül-içi kullanım (AV, SSL, ...): bildirim oluştur. domainID>0 ise
// sahip reseller_id domains'ten çözülür ve kayda yazılır → görünürlük sahibine
// kilitlenir. domainID=0 → panel-geneli (admin).
func Yaz(db *sql.DB, seviye, kategori, baslik, mesaj string, domainID int64, refTur string, refID int64) {
	if db == nil {
		return
	}
	var dom, rid any
	if domainID > 0 {
		dom = domainID
		var r int64
		if db.QueryRow(`SELECT reseller_id FROM domains WHERE id=?`, domainID).Scan(&r) == nil && r > 0 {
			rid = r // reseller-sahipli; 0 ise admin-sahipli → NULL (admin görür)
		}
	}
	_, _ = db.Exec(
		`INSERT INTO cp_bildirim (seviye, kategori, baslik, mesaj, domain_id, reseller_id, ref_tur, ref_id)
		 VALUES (?,?,?,?,?,?,?,?)`, seviye, kategori, baslik, mesaj, dom, rid, refTur, refID)
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
