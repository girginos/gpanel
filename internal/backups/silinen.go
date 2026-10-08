package backups

// Silinen hosting/bayi yedekleri ("çöp kutusu").
//
// Karar (kullanıcı, 2026-10-08): hosting ya da bayi silinince yedekleri SİLİNMEZ —
// müşteri sonradan yedek isteyebilir ya da yanlışlıkla silmiş olabilir. Yerel yedek
// dizini BackupRoot/_silinen/<id>-<sk> altına taşınır, uzak kopyalara dokunulmaz; saklama
// süresi dolunca (varsayılan: hostingin yedek slotu kadar gün, en az 7) ya da yönetici
// isteyince temizlenir. Süre cp_ayarlar.silinen_yedek_gun ile ayarlanır
// (boş = slot, 0 = otomatik silme yok, N = sabit gün).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/gizli"
	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"

	"github.com/go-chi/chi/v5"
)

// yedekKok: saklama işlemlerinin kökü (testte geçici dizine çevrilir).
var yedekKok = BackupRoot

const (
	silinenKlasor     = "_silinen"
	silinenAyarKey    = "silinen_yedek_gun"
	silinenEnAzGun    = 7
	silinenEnCokGun   = 3650
	silinenKayitTutma = 180 // temizlenmiş kaydın günü
)

// silinenAyar: cp_ayarlar'daki ham değer ("" = ayarsız → slot).
func silinenAyar(ctx context.Context, db *sql.DB) string {
	var v string
	if err := db.QueryRowContext(ctx, `SELECT deger FROM cp_ayarlar WHERE anahtar=?`, silinenAyarKey).Scan(&v); err != nil {
		return ""
	}
	return strings.TrimSpace(v)
}

// silinenSaklamaGunu: bu hosting için saklama günü; 0 = otomatik silme yok.
func silinenSaklamaGunu(ayar string, slot int) int {
	if ayar != "" {
		if n, err := strconv.Atoi(ayar); err == nil && n >= 0 && n <= silinenEnCokGun {
			return n
		}
	}
	if slot < silinenEnAzGun {
		slot = silinenEnAzGun
	}
	return slot
}

func dizinBoyutu(kok string) (toplam int64) {
	_ = filepath.Walk(kok, func(_ string, fi os.FileInfo, err error) error {
		if err == nil && fi != nil && fi.Mode().IsRegular() {
			toplam += fi.Size()
		}
		return nil
	})
	return toplam
}

// SilinenKaydet — hosting yıkımının İLK adımı: yedekleri silmek yerine saklamaya alır.
// Hata dönerse çağıran yıkıma BAŞLAMAMALIDIR (yedek taşınamadıysa hesap silinmez).
// Saklanacak bir şey yoksa (yedek kaydı, dizin ve uzak hedef yok) kayıt açmaz.
func SilinenKaydet(ctx context.Context, db *sql.DB, domainID int64, alanAdi, sk string) error {
	if !strings.HasPrefix(sk, "c_") || strings.ContainsAny(sk, "/\\") || strings.Contains(sk, "..") {
		return nil // geçersiz sk'ya dokunulmaz (yol kaçışı koruması)
	}
	var resellerID int64
	var slot int
	_ = db.QueryRowContext(ctx, `SELECT COALESCE(reseller_id,0), COALESCE(backup_retention,7) FROM domains WHERE id=?`, domainID).
		Scan(&resellerID, &slot)

	var dosyalar []string
	rows, err := db.QueryContext(ctx, `SELECT dosya FROM backups WHERE domain_id=? ORDER BY id`, domainID)
	if err != nil {
		return fmt.Errorf("yedek listesi okunamadı: %w", err)
	}
	for rows.Next() {
		var d string
		if rows.Scan(&d) == nil && strings.TrimSpace(d) != "" {
			dosyalar = append(dosyalar, d)
		}
	}
	rows.Close()

	var hTip, hHost, hKul, hParola, hDizin string
	var hPort int
	_ = db.QueryRowContext(ctx, `SELECT tip, host, port, kullanici, parola, uzak_dizin FROM backup_destinations WHERE domain_id=?`, domainID).
		Scan(&hTip, &hHost, &hPort, &hKul, &hParola, &hDizin)

	kaynak := filepath.Join(yedekKok, sk)
	_, statErr := os.Stat(kaynak)
	dizinVar := statErr == nil
	if len(dosyalar) == 0 && !dizinVar && hHost == "" {
		return nil
	}
	if dosyalar == nil {
		dosyalar = []string{}
	}
	js, _ := json.Marshal(dosyalar)
	var boyut int64
	if dizinVar {
		boyut = dizinBoyutu(kaynak)
	}

	gun := silinenSaklamaGunu(silinenAyar(ctx, db), slot)
	var sonKullanma any // NULL = otomatik silme yok
	if gun > 0 {
		sonKullanma = gun
	}
	res, err := db.ExecContext(ctx,
		`INSERT INTO cp_silinen_yedek (alan_adi, sistem_kullanici, eski_domain_id, reseller_id, son_kullanma,
		   dosyalar, dosya_sayisi, boyut_b, hedef_tip, hedef_host, hedef_port, hedef_kullanici, hedef_parola, hedef_dizin)
		 VALUES (?,?,?,?, IF(? IS NULL, NULL, DATE_ADD(NOW(), INTERVAL ? DAY)), ?,?,?,?,?,?,?,?,?)`,
		alanAdi, sk, domainID, resellerID, sonKullanma, sonKullanma,
		string(js), len(dosyalar), boyut, hTip, hHost, hPort, hKul, hParola, hDizin)
	if err != nil {
		return fmt.Errorf("silinen yedek kaydı açılamadı: %w", err)
	}
	id, _ := res.LastInsertId()

	if dizinVar {
		goreli := filepath.Join(silinenKlasor, fmt.Sprintf("%d-%s", id, sk))
		hedef := filepath.Join(yedekKok, goreli)
		if e := os.MkdirAll(filepath.Join(yedekKok, silinenKlasor), 0o700); e == nil {
			e = os.Rename(kaynak, hedef)
			if e != nil {
				_, _ = db.ExecContext(ctx, `DELETE FROM cp_silinen_yedek WHERE id=?`, id)
				return fmt.Errorf("yedek dizini saklamaya taşınamadı: %w", e)
			}
		} else {
			_, _ = db.ExecContext(ctx, `DELETE FROM cp_silinen_yedek WHERE id=?`, id)
			return fmt.Errorf("saklama dizini açılamadı: %w", e)
		}
		if _, e := db.ExecContext(ctx, `UPDATE cp_silinen_yedek SET dizin=? WHERE id=?`, goreli, id); e != nil {
			log.Printf("silinen yedek %d: dizin yolu yazılamadı (dizin %s): %v", id, goreli, e)
		}
	}
	log.Printf("silinen yedek: %s (%s) %d dosya, %d bayt saklamaya alındı (kayıt %d, %d gün)", alanAdi, sk, len(dosyalar), boyut, id, gun)
	return nil
}

type silinenSatir struct {
	ID         int64    `json:"id"`
	AlanAdi    string   `json:"alan_adi"`
	Sistem     string   `json:"sistem_kullanici"`
	ResellerID int64    `json:"reseller_id"`
	Silinme    string   `json:"silinme"`
	SonKullan  string   `json:"son_kullanma"` // boş = otomatik silinmez
	DosyaSayi  int      `json:"dosya_sayisi"`
	BoyutB     int64    `json:"boyut_b"`
	Dosyalar   []string `json:"dosyalar"`
	UzakHedef  bool     `json:"uzak_hedef"`
	YerelVar   bool     `json:"yerel_var"`
	Durum      string   `json:"durum"`
	SonHata    string   `json:"son_hata,omitempty"`
}

// SilinenListe: GET /admin/silinen-yedekler
func (h *Handlers) SilinenListe(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, alan_adi, sistem_kullanici, reseller_id, DATE_FORMAT(silinme,'%Y-%m-%d %H:%i'),
		        COALESCE(DATE_FORMAT(son_kullanma,'%Y-%m-%d %H:%i'),''), dosya_sayisi, boyut_b, dosyalar,
		        hedef_host<>'', dizin, durum, son_hata
		 FROM cp_silinen_yedek ORDER BY id DESC LIMIT 500`)
	if err != nil {
		log.Printf("silinen yedek: liste: %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, "liste okunamadı")
		return
	}
	defer rows.Close()
	out := []silinenSatir{}
	for rows.Next() {
		var s silinenSatir
		var js, dizin string
		var uzak int
		if rows.Scan(&s.ID, &s.AlanAdi, &s.Sistem, &s.ResellerID, &s.Silinme, &s.SonKullan, &s.DosyaSayi, &s.BoyutB,
			&js, &uzak, &dizin, &s.Durum, &s.SonHata) != nil {
			continue
		}
		_ = json.Unmarshal([]byte(js), &s.Dosyalar)
		s.UzakHedef = uzak == 1
		if abs := silinenAbsDizin(dizin); abs != "" && s.Durum == "saklaniyor" {
			if fi, e := os.Stat(abs); e == nil && fi.IsDir() {
				s.YerelVar = true
			}
		}
		out = append(out, s)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// silinenAbsDizin: kayıttaki göreli dizini doğrulayıp mutlak yola çevirir (_silinen altında değilse "").
func silinenAbsDizin(dizin string) string {
	if dizin == "" {
		return ""
	}
	temiz := filepath.Clean(dizin)
	if !strings.HasPrefix(temiz, silinenKlasor+string(filepath.Separator)) || strings.Contains(temiz, "..") {
		return ""
	}
	abs := filepath.Join(yedekKok, temiz)
	if !strings.HasPrefix(abs, filepath.Join(yedekKok, silinenKlasor)+string(filepath.Separator)) {
		return ""
	}
	return abs
}

// SilinenTemizleBir — bir silinmiş hesabın saklanan yedeklerini kalıcı siler: önce uzak
// kopyalar (silinemezse uzagiYoksay olmadıkça HİÇBİR ŞEY silinmez), sonra yerel dizin.
func SilinenTemizleBir(ctx context.Context, db *sql.DB, id int64, uzagiYoksay bool) error {
	var alanAdi, sk, js, dizin, hTip, hHost, hKul, hParola, hDizin, durum string
	var hPort int
	err := db.QueryRowContext(ctx,
		`SELECT alan_adi, sistem_kullanici, dosyalar, dizin, hedef_tip, hedef_host, hedef_port, hedef_kullanici, hedef_parola, hedef_dizin, durum
		 FROM cp_silinen_yedek WHERE id=?`, id).
		Scan(&alanAdi, &sk, &js, &dizin, &hTip, &hHost, &hPort, &hKul, &hParola, &hDizin, &durum)
	if err != nil {
		return err
	}
	if durum != "saklaniyor" {
		return nil
	}
	var dosyalar []string
	_ = json.Unmarshal([]byte(js), &dosyalar)

	if !uzagiYoksay && len(dosyalar) > 0 {
		var d *Destination
		if strings.TrimSpace(hHost) != "" {
			d = &Destination{Tip: hTip, Host: hHost, Port: hPort, Kullanici: hKul,
				Parola: gizli.CozBagli(hParola, "yedek"), UzakDizin: hDizin, Aktif: true}
		}
		var ilk error
		kalan := 0
		for _, f := range dosyalar {
			if e := yedekUzaktanSilHedefle(ctx, db, d, f); e != nil && !errors.Is(e, ErrLftpYok) {
				kalan++
				if ilk == nil {
					ilk = e
				}
			}
		}
		if kalan > 0 {
			msg := fmt.Sprintf("%d yedeğin uzak kopyası silinemedi: %v", kalan, ilk)
			if len(msg) > 900 {
				msg = msg[:900]
			}
			_, _ = db.ExecContext(ctx, `UPDATE cp_silinen_yedek SET son_hata=? WHERE id=?`, msg, id)
			return errors.New(msg)
		}
	}
	if abs := silinenAbsDizin(dizin); abs != "" {
		if e := os.RemoveAll(abs); e != nil {
			msg := "yerel dizin silinemedi: " + e.Error()
			_, _ = db.ExecContext(ctx, `UPDATE cp_silinen_yedek SET son_hata=? WHERE id=?`, msg, id)
			return errors.New(msg)
		}
	}
	_, err = db.ExecContext(ctx,
		`UPDATE cp_silinen_yedek SET durum='temizlendi', temizlenme=NOW(), son_hata='', hedef_parola='' WHERE id=?`, id)
	return err
}

// SilinenSupur — süresi dolanları temizler (günde bir, 04:00 turu).
func SilinenSupur(ctx context.Context, db *sql.DB) {
	rows, err := db.QueryContext(ctx,
		`SELECT id FROM cp_silinen_yedek WHERE durum='saklaniyor' AND son_kullanma IS NOT NULL AND son_kullanma<=NOW() ORDER BY id LIMIT 200`)
	if err != nil {
		log.Printf("silinen yedek süpürme: %v", err)
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if e := SilinenTemizleBir(ctx, db, id, false); e != nil {
			log.Printf("silinen yedek %d temizlenemedi (yarın tekrar denenir): %v", id, e)
		} else {
			log.Printf("silinen yedek %d süresi doldu, temizlendi", id)
		}
	}
	_, _ = db.ExecContext(ctx, `DELETE FROM cp_silinen_yedek WHERE durum='temizlendi' AND temizlenme < DATE_SUB(NOW(), INTERVAL ? DAY)`, silinenKayitTutma)
}

// SilinenSil: DELETE /admin/silinen-yedekler/{id}[?uzagi_yoksay=1] — süreyi beklemeden kalıcı siler.
func (h *Handlers) SilinenSil(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	yoksay := r.URL.Query().Get("uzagi_yoksay") == "1"
	var alanAdi string
	if err := h.DB.QueryRowContext(r.Context(), `SELECT alan_adi FROM cp_silinen_yedek WHERE id=?`, id).Scan(&alanAdi); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "kayıt bulunamadı")
		return
	}
	kctx, iptal := httpx.KopukBaglam(r, 30*time.Minute)
	defer iptal()
	uid, kul := middleware.Aktor(r)
	if err := SilinenTemizleBir(kctx, h.DB, id, yoksay); err != nil {
		log.Printf("silinen yedek %d silme: %v", id, err)
		httpx.WriteJSON(w, http.StatusConflict, map[string]any{"ok": false, "hata": err.Error(), "uzagi_yoksay": true})
		return
	}
	httpx.Denetim(h.DB, r, uid, kul, "silinen-yedek.temizle", alanAdi, fmt.Sprintf("kayıt=%d uzak_yoksay=%v", id, yoksay), 0, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// SilinenIndir: GET /admin/silinen-yedekler/{id}/indir?dosya=<ad> — saklanan yedeği indirir.
func (h *Handlers) SilinenIndir(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	ad := r.URL.Query().Get("dosya")
	var js, dizin, durum, alanAdi string
	if err := h.DB.QueryRowContext(r.Context(), `SELECT dosyalar, dizin, durum, alan_adi FROM cp_silinen_yedek WHERE id=?`, id).
		Scan(&js, &dizin, &durum, &alanAdi); err != nil || durum != "saklaniyor" {
		httpx.WriteError(w, http.StatusNotFound, "kayıt bulunamadı")
		return
	}
	var dosyalar []string
	_ = json.Unmarshal([]byte(js), &dosyalar)
	bulundu := false
	for _, d := range dosyalar {
		if d == ad && ad == filepath.Base(ad) {
			bulundu = true
		}
	}
	abs := silinenAbsDizin(dizin)
	if !bulundu || abs == "" {
		httpx.WriteError(w, http.StatusNotFound, "yedek dosyası bulunamadı")
		return
	}
	f, err := os.Open(filepath.Join(abs, ad))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "yedek dosyası diskte yok")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.IsDir() {
		httpx.WriteError(w, http.StatusNotFound, "yedek dosyası okunamadı")
		return
	}
	uid, kul := middleware.Aktor(r)
	httpx.Denetim(h.DB, r, uid, kul, "silinen-yedek.indir", alanAdi, ad, 0, true)
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(ad, `"`, "")+`"`)
	http.ServeContent(w, r, ad, fi.ModTime(), f)
}

// SilinenAyarGet: GET /admin/silinen-yedekler/ayar → {gun: null|N}  (null = hostingin yedek slotu)
func (h *Handlers) SilinenAyarGet(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"gun": silinenAyarJSON(silinenAyar(r.Context(), h.DB)), "en_az_slot_gun": silinenEnAzGun})
}

func silinenAyarJSON(v string) any {
	if v == "" {
		return nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return nil
	}
	return n
}

// SilinenAyarSet: PUT /admin/silinen-yedekler/ayar {gun: null|0|N}
func (h *Handlers) SilinenAyarSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Gun *int `json:"gun"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	if req.Gun != nil && (*req.Gun < 0 || *req.Gun > silinenEnCokGun) {
		httpx.WriteError(w, http.StatusBadRequest, "gün 0 ile "+strconv.Itoa(silinenEnCokGun)+" arasında olmalı (boş = hosting yedek slotu)")
		return
	}
	var err error
	if req.Gun == nil {
		_, err = h.DB.ExecContext(r.Context(), `DELETE FROM cp_ayarlar WHERE anahtar=?`, silinenAyarKey)
	} else {
		_, err = h.DB.ExecContext(r.Context(),
			`INSERT INTO cp_ayarlar (anahtar, deger) VALUES (?, ?) ON DUPLICATE KEY UPDATE deger=VALUES(deger)`,
			silinenAyarKey, strconv.Itoa(*req.Gun))
	}
	if err != nil {
		log.Printf("silinen yedek ayar: %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, "ayar yazılamadı")
		return
	}
	uid, kul := middleware.Aktor(r)
	httpx.Denetim(h.DB, r, uid, kul, "silinen-yedek.ayar", silinenAyarKey, fmt.Sprintf("%v", silinenAyarJSON(func() string {
		if req.Gun == nil {
			return ""
		}
		return strconv.Itoa(*req.Gun)
	}())), 0, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
