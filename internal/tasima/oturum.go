package tasima

// Taşıma OTURUM kalıcılığı — kullanıcı sayfa yenileyince kaynak sunucu bilgilerini
// ve keşif sonuçlarını yeniden girmesin.
//
// Akış: Kesif → oturumKaydet ('oturum' satırı, kimlik AES-GCM şifreli, bağlam
// kapsam+host; kesif_json + TTL). Sayfa açılışında OturumListe/OturumGetir ile geri
// yüklenir. Baslat(oturum_id) şifreli kimliği SUNUCU TARAFINDA çözer — parola
// tarayıcıya asla geri dönmez. TTL dolunca AcilistaTemizle temizler.
//
// 🔴 KAPSAM: oturum satırı sahip_rid taşır. Liste/Getir/Sil yalnız kendi
// kapsamının satırlarını görür (bayi: kendi rid'i; kök: kök oturumları); başka
// kiracının oturumu "yok" ile birebir aynı yanıtı alır. oturumKaydet yalnız AYNI
// kapsamın eski oturumunu değiştirir (başkasının oturumunu silemez).

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/httpx"
)

const oturumTTL = 2 * time.Hour

// oturumKaydet — keşif sonrası oturumu kalıcılaştır. Aynı kapsam+host+kullanıcı için
// önceki 'oturum' satırı DEĞİŞTİRİLİR (tek taze oturum). Döner: oturum id (0=hata).
// Oturum yalnız kolaylıktır, keşif yanıtı yine döner; ama hata YUTULMAZ, günlüğe
// yazılır (0109 öncesi gpanel kaynağı ENUM'a takılıp her keşifte sessizce 0 dönüyordu).
func (h *Handlers) oturumKaydet(kp istekKapsami, g kaynakGirdi, hesaplar []Hesap) int64 {
	k := g.kaynak(kp.Kapsam)
	// Aynı kaynak için eski oturumları temizle (biriktirme yok) — YALNIZ bu kapsamın.
	if _, err := h.DB.Exec(`DELETE FROM tasima_isleri
		WHERE durum='oturum' AND kaynak_host=? AND kaynak_kullanici=? AND sahip_rid=?`, k.Host, k.Kullanici, kp.RID); err != nil {
		log.Printf("tasima: eski oturum silinemedi (tip=%q): %v", k.Tip, err)
	}

	kesifJSON, _ := json.Marshal(hesaplar)
	baglam := kimlikBaglam(kp.Kapsam, k.Host)
	parolaSif := ""
	if k.Parola != "" {
		parolaSif = kimlikSifrele(k.Parola, baglam)
	}
	anahtarSif := ""
	if k.Anahtar != "" {
		anahtarSif = kimlikSifrele(k.Anahtar, baglam)
	}
	res, err := h.DB.Exec(
		`INSERT INTO tasima_isleri
		   (kaynak_tip, kaynak_host, kaynak_port, kaynak_kullanici, kaynak_parola,
		    kaynak_anahtar, durum, toplam, kesif_json, son_kullanim, gecerlilik, baslangic,
		    sahip_rid, baslatan_uid)
		 VALUES (?,?,?,?,?,?, 'oturum', ?, ?, NOW(), (NOW() + INTERVAL ? SECOND), NULL, ?, NULLIF(?,0))`,
		k.Tip, k.Host, k.Port, k.Kullanici, parolaSif, anahtarSif,
		len(hesaplar), string(kesifJSON), int(oturumTTL.Seconds()), kp.RID, kp.UID)
	if err != nil {
		log.Printf("tasima: oturum kaydedilemedi (tip=%q): %v", k.Tip, err)
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

// oturumBulunamadi — kapsam dışı ve var olmayan oturum için AYNI yanıt.
func oturumBulunamadi(w http.ResponseWriter) {
	httpx.WriteError(w, http.StatusNotFound, "oturum bulunamadi ya da suresi doldu")
}

// OturumListe — GET /tasima/oturumlar — kapsamın kaydedilmiş (süresi dolmamış)
// oturumları. SIR DÖNDÜRMEZ (parola/anahtar yok).
func (h *Handlers) OturumListe(w http.ResponseWriter, r *http.Request) {
	kp, kod, msj := kapsamAl(r)
	if !kapsamHatasi(w, kod, msj) {
		return
	}
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, kaynak_tip, kaynak_host, kaynak_port, kaynak_kullanici, toplam,
		        (kaynak_parola IS NOT NULL OR kaynak_anahtar IS NOT NULL) AS sakli,
		        COALESCE(DATE_FORMAT(son_kullanim,'%Y-%m-%d %H:%i:%s'),'')
		   FROM tasima_isleri
		  WHERE durum='oturum' AND sahip_rid=? AND (gecerlilik IS NULL OR gecerlilik > NOW())
		  ORDER BY son_kullanim DESC LIMIT 25`, kp.RID)
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

// OturumGetir — GET /tasima/oturum/{id} — form + keşif listesini geri yükler.
// 🔴 Parola/anahtar ASLA dönmez; yalnız kimlik_sakli bayrağı.
func (h *Handlers) OturumGetir(w http.ResponseWriter, r *http.Request) {
	kp, kod, msj := kapsamAl(r)
	if !kapsamHatasi(w, kod, msj) {
		return
	}
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var tip, host, kullanici, kesifJSON, secimJSON string
	var port int
	var pSif, aSif sql.NullString
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT kaynak_tip, kaynak_host, kaynak_port, kaynak_kullanici,
		        COALESCE(kesif_json,''), COALESCE(secim_json,''), kaynak_parola, kaynak_anahtar
		   FROM tasima_isleri
		  WHERE id=? AND durum='oturum' AND sahip_rid=? AND (gecerlilik IS NULL OR gecerlilik > NOW())`, id, kp.RID).
		Scan(&tip, &host, &port, &kullanici, &kesifJSON, &secimJSON, &pSif, &aSif)
	if err == sql.ErrNoRows {
		oturumBulunamadi(w)
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "oturum okunamadi")
		return
	}
	_, _ = h.DB.ExecContext(r.Context(), `UPDATE tasima_isleri SET son_kullanim=NOW() WHERE id=? AND sahip_rid=?`, id, kp.RID)

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
		"hesaplar": h.kesifIsaretle(r.Context(), kp.Kapsam, hesaplar), "secim": secim, "kimlik_sakli": kimlikSakli,
	})
}

// OturumSil — DELETE /tasima/oturum/{id} — kaydedilmiş oturumu unut (kimlik dahil).
func (h *Handlers) OturumSil(w http.ResponseWriter, r *http.Request) {
	kp, kod, msj := kapsamAl(r)
	if !kapsamHatasi(w, kod, msj) {
		return
	}
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	// Hata yutulmaz, olmayan (ya da başka kapsamın) kayıt "silindi" görünmez ve
	// denetim kaydı yalnız GERÇEK silmede yazılır.
	res, err := h.DB.ExecContext(r.Context(), `DELETE FROM tasima_isleri WHERE id=? AND durum='oturum' AND sahip_rid=?`, id, kp.RID)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "oturum silinemedi")
		return
	}
	if aff, _ := res.RowsAffected(); aff == 0 {
		oturumBulunamadi(w)
		return
	}
	httpx.Denetim(h.DB, r, kp.UID, kp.Kul, "tasima_oturum_sil", strconv.FormatInt(id, 10), "", kp.RID, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
