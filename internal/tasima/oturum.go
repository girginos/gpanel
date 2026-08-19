package tasima

// Taşıma OTURUM kalıcılığı — kullanıcı sayfa yenileyince kaynak sunucu bilgilerini
// ve keşif sonuçlarını yeniden girmesin.
//
// Akış: Kesif → oturumKaydet ('oturum' satırı, kimlik AES-GCM host-bağlı şifreli,
// kesif_json + TTL). Sayfa açılışında OturumListe/OturumGetir ile geri yüklenir.
// Baslat(oturum_id) şifreli kimliği SUNUCU TARAFINDA çözer — parola tarayıcıya
// asla geri dönmez. TTL dolunca AcilistaTemizle temizler.

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/gizli"
	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
)

const oturumTTL = 2 * time.Hour

// oturumKaydet — keşif sonrası oturumu kalıcılaştır. Aynı host+kullanıcı için
// önceki 'oturum' satırı DEĞİŞTİRİLİR (tek taze oturum). Döner: oturum id (0=hata).
func (h *Handlers) oturumKaydet(g kaynakGirdi, hesaplar []Hesap) int64 {
	k := g.kaynak()
	// Aynı kaynak için eski oturumları temizle (biriktirme yok).
	_, _ = h.DB.Exec(`DELETE FROM tasima_isleri
		WHERE durum='oturum' AND kaynak_host=? AND kaynak_kullanici=?`, k.Host, k.Kullanici)

	kesifJSON, _ := json.Marshal(hesaplar)
	parolaSif := ""
	if k.Parola != "" {
		parolaSif = gizli.SaklaBagli(k.Parola, k.Host)
	}
	anahtarSif := ""
	if k.Anahtar != "" {
		anahtarSif = gizli.SaklaBagli(k.Anahtar, k.Host)
	}
	res, err := h.DB.Exec(
		`INSERT INTO tasima_isleri
		   (kaynak_tip, kaynak_host, kaynak_port, kaynak_kullanici, kaynak_parola,
		    kaynak_anahtar, durum, toplam, kesif_json, son_kullanim, gecerlilik, baslangic)
		 VALUES (?,?,?,?,?,?, 'oturum', ?, ?, NOW(), (NOW() + INTERVAL ? SECOND), NULL)`,
		k.Tip, k.Host, k.Port, k.Kullanici, parolaSif, anahtarSif,
		len(hesaplar), string(kesifJSON), int(oturumTTL.Seconds()))
	if err != nil {
		return 0
	}
	id, _ := res.LastInsertId()
	return id
}

type oturumOzet struct {
	ID          int64  `json:"id"`
	Tip         string `json:"tip"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Kullanici   string `json:"kullanici"`
	SiteSayisi  int    `json:"site_sayisi"`
	KimlikSakli bool   `json:"kimlik_sakli"`
	SonKullanim string `json:"son_kullanim"`
}

// OturumListe — GET /system/tasima/oturumlar — kaydedilmiş (süresi dolmamış)
// oturumlar. SIR DÖNDÜRMEZ (parola/anahtar yok).
func (h *Handlers) OturumListe(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(
		`SELECT id, kaynak_tip, kaynak_host, kaynak_port, kaynak_kullanici, toplam,
		        (kaynak_parola IS NOT NULL OR kaynak_anahtar IS NOT NULL) AS sakli,
		        COALESCE(DATE_FORMAT(son_kullanim,'%Y-%m-%d %H:%i:%s'),'')
		   FROM tasima_isleri
		  WHERE durum='oturum' AND (gecerlilik IS NULL OR gecerlilik > NOW())
		  ORDER BY son_kullanim DESC LIMIT 25`)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "oturumlar okunamadi")
		return
	}
	defer rows.Close()
	out := []oturumOzet{}
	for rows.Next() {
		var o oturumOzet
		var sakli int
		if rows.Scan(&o.ID, &o.Tip, &o.Host, &o.Port, &o.Kullanici, &o.SiteSayisi, &sakli, &o.SonKullanim) == nil {
			o.KimlikSakli = sakli != 0
			out = append(out, o)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"oturumlar": out})
}

// OturumGetir — GET /system/tasima/oturum/{id} — form + keşif listesini geri yükler.
// 🔴 Parola/anahtar ASLA dönmez; yalnız kimlik_sakli bayrağı.
func (h *Handlers) OturumGetir(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var tip, host, kullanici, kesifJSON, secimJSON string
	var port int
	var pSif, aSif sql.NullString
	err := h.DB.QueryRow(
		`SELECT kaynak_tip, kaynak_host, kaynak_port, kaynak_kullanici,
		        COALESCE(kesif_json,''), COALESCE(secim_json,''), kaynak_parola, kaynak_anahtar
		   FROM tasima_isleri
		  WHERE id=? AND durum='oturum' AND (gecerlilik IS NULL OR gecerlilik > NOW())`, id).
		Scan(&tip, &host, &port, &kullanici, &kesifJSON, &secimJSON, &pSif, &aSif)
	if err == sql.ErrNoRows {
		httpx.WriteError(w, http.StatusNotFound, "oturum bulunamadi ya da suresi doldu")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "oturum okunamadi")
		return
	}
	_, _ = h.DB.Exec(`UPDATE tasima_isleri SET son_kullanim=NOW() WHERE id=?`, id)

	var hesaplar []Hesap
	if kesifJSON != "" {
		_ = json.Unmarshal([]byte(kesifJSON), &hesaplar)
	}
	var secim []string
	if secimJSON != "" {
		_ = json.Unmarshal([]byte(secimJSON), &secim)
	}
	kimlikSakli := (pSif.Valid && pSif.String != "") || (aSif.Valid && aSif.String != "")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"id": id, "tip": tip, "host": host, "port": port, "kullanici": kullanici,
		"hesaplar": hesaplar, "secim": secim, "kimlik_sakli": kimlikSakli,
	})
}

// OturumSil — DELETE /system/tasima/oturum/{id} — kaydedilmiş oturumu unut (kimlik dahil).
func (h *Handlers) OturumSil(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	aktorUID, aktor := middleware.Aktor(r)
	_, _ = h.DB.Exec(`DELETE FROM tasima_isleri WHERE id=? AND durum='oturum'`, id)
	httpx.Denetim(h.DB, r, aktorUID, aktor, "tasima_oturum_sil", strconv.FormatInt(id, 10), "", 0, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
