package backups

// Silinen hosting/bayi yedekleri ("çöp kutusu").
//
// Karar (kullanıcı, 2026-10-08): hosting/bayi silme YEDEKLERDEN BAĞIMSIZDIR. Silme yolu yedek
// dizinine/uzak kopyalara hiç dokunmaz ve yedekler yüzünden ASLA başarısız olmaz: yedekler
// BackupRoot/<sk> altında olduğu yerde kalır. Silme yalnız DB'ye best-effort bir iz bırakır
// (SilinenIsaretle: alan adı, sk, yedek slotu, dosya adları, domaine özel FTP hedefi); süre
// dolunca günlük tur (SilinenSupur) yedekleri temizler. Süre cp_ayarlar.silinen_yedek_gun:
// boş = hostingin yedek slotu (en az 7 gün), 0 = otomatik silme yok, N = sabit gün.
//
// 🔴 sk YENİDEN KULLANILABİLİR: aynı alan adı yeniden açılırsa yeni hosting aynı BackupRoot/<sk>
// dizinini devralır. Bu yüzden listeleme/indirme/temizlik YALNIZ "silinmeden önceki ve canlı
// hostingin kayıtlarında olmayan" dosyalara dokunur (silinenDosyalari).

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
	"regexp"
	"sort"
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

// SilinenIsaretle — hosting silindiğinde (HostingYik'in SON adımlarından, DB satırı gitmeden ÖNCE)
// saklama izini yazar. YALNIZ DB okur/yazar: dizine dokunmaz, hata VERMEZ (panik dahil yutulur);
// başarısızlığı silmeyi etkilemez, yalnız yedeğin otomatik süresi izlenemez (dizin yine yerinde durur).
func SilinenIsaretle(ctx context.Context, db *sql.DB, domainID int64, alanAdi, sk string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("silinen yedek iz: panik (%s): %v", alanAdi, r)
		}
	}()
	if !silinenSKGecerli(sk) {
		return
	}
	var resellerID int64
	var slot int
	_ = db.QueryRowContext(ctx, `SELECT COALESCE(reseller_id,0), COALESCE(backup_retention,7) FROM domains WHERE id=?`, domainID).
		Scan(&resellerID, &slot)

	dosyalar := []string{}
	if rows, err := db.QueryContext(ctx, `SELECT dosya FROM backups WHERE domain_id=? ORDER BY id`, domainID); err == nil {
		for rows.Next() {
			var d string
			if rows.Scan(&d) == nil && strings.TrimSpace(d) != "" {
				dosyalar = append(dosyalar, d)
			}
		}
		rows.Close()
	}
	var hTip, hHost, hKul, hParola, hDizin string
	var hPort int
	_ = db.QueryRowContext(ctx, `SELECT tip, host, port, kullanici, parola, uzak_dizin FROM backup_destinations WHERE domain_id=?`, domainID).
		Scan(&hTip, &hHost, &hPort, &hKul, &hParola, &hDizin)
	js, _ := json.Marshal(dosyalar)

	gun := silinenSaklamaGunu(silinenAyar(ctx, db), slot)
	var sonKullanma any // NULL = otomatik silme yok
	if gun > 0 {
		sonKullanma = gun
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO cp_silinen_yedek (alan_adi, sistem_kullanici, eski_domain_id, reseller_id, son_kullanma, dizin,
		   dosyalar, dosya_sayisi, hedef_tip, hedef_host, hedef_port, hedef_kullanici, hedef_parola, hedef_dizin)
		 VALUES (?,?,?,?, IF(? IS NULL, NULL, DATE_ADD(NOW(), INTERVAL ? DAY)), ?, ?,?,?,?,?,?,?,?)`,
		alanAdi, sk, domainID, resellerID, sonKullanma, sonKullanma, sk,
		string(js), len(dosyalar), hTip, hHost, hPort, hKul, hParola, hDizin); err != nil {
		log.Printf("silinen yedek iz yazılamadı (%s, yedekler yerinde duruyor): %v", alanAdi, err)
	}
}

var silinenSKRe = regexp.MustCompile(`^c_[a-z0-9_]{1,40}$`)

func silinenSKGecerli(sk string) bool { return silinenSKRe.MatchString(sk) }

// silinenDosyalari — kaydın yedek klasöründeki, ŞU AN bu silinmiş hesaba ait sayılabilecek
// *.tar.gz dosyaları (ad → boyut). sk başka bir canlı hostingce devralındıysa onun kayıtlı
// yedekleri ve silinme anından sonra yazılanlar HARİÇ tutulur (başka müşterinin yedeği sızmasın/silinmesin).
func silinenDosyalari(ctx context.Context, db *sql.DB, sk string, silinme time.Time) (map[string]int64, bool) {
	out := map[string]int64{}
	if !silinenSKGecerli(sk) {
		return out, false
	}
	var sahipID int64
	_ = db.QueryRowContext(ctx, `SELECT id FROM domains WHERE sistem_kullanici=? LIMIT 1`, sk).Scan(&sahipID)
	canli := map[string]bool{}
	if sahipID > 0 {
		if rows, err := db.QueryContext(ctx, `SELECT dosya FROM backups WHERE domain_id=?`, sahipID); err == nil {
			for rows.Next() {
				var d string
				if rows.Scan(&d) == nil {
					canli[d] = true
				}
			}
			rows.Close()
		}
	}
	ents, err := os.ReadDir(filepath.Join(yedekKok, sk))
	if err != nil {
		return out, sahipID > 0
	}
	for _, e := range ents {
		ad := e.Name()
		if e.IsDir() || !strings.HasSuffix(ad, ".tar.gz") || canli[ad] {
			continue
		}
		fi, err := e.Info()
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		if sahipID > 0 && !silinme.IsZero() && fi.ModTime().After(silinme) {
			continue
		}
		out[ad] = fi.Size()
	}
	return out, sahipID > 0
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
		`SELECT id, alan_adi, sistem_kullanici, reseller_id, DATE_FORMAT(silinme,'%Y-%m-%d %H:%i'), silinme,
		        COALESCE(DATE_FORMAT(son_kullanma,'%Y-%m-%d %H:%i'),''), dosyalar,
		        hedef_host<>'', durum, son_hata
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
		var js string
		var uzak int
		var silinme time.Time
		if rows.Scan(&s.ID, &s.AlanAdi, &s.Sistem, &s.ResellerID, &s.Silinme, &silinme, &s.SonKullan,
			&js, &uzak, &s.Durum, &s.SonHata) != nil {
			continue
		}
		s.UzakHedef = uzak == 1
		_ = json.Unmarshal([]byte(js), &s.Dosyalar)
		if s.Durum == "saklaniyor" {
			dos, _ := silinenDosyalari(r.Context(), h.DB, s.Sistem, silinme)
			s.YerelVar = len(dos) > 0
			if s.YerelVar {
				s.Dosyalar = s.Dosyalar[:0]
				for ad, b := range dos {
					s.Dosyalar = append(s.Dosyalar, ad)
					s.BoyutB += b
				}
				sort.Strings(s.Dosyalar)
			}
			// Yerelde de uzakta da saklanacak bir şey kalmadıysa satırı gösterme (yedeksiz hesap izi).
			if !s.YerelVar && !s.UzakHedef && len(s.Dosyalar) == 0 {
				continue
			}
		}
		s.DosyaSayi = len(s.Dosyalar)
		out = append(out, s)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// SilinenTemizleBir — bir silinmiş hesabın saklanan yedeklerini kalıcı siler: önce uzak
// kopyalar (silinemezse uzagiYoksay olmadıkça HİÇBİR ŞEY silinmez), sonra yerel dosyalar.
// Yerel silme yalnız silinenDosyalari'nın döndürdüğü dosyalardır; sk'yi başka bir canlı hosting
// devraldıysa onun dosyalarına dokunulmaz.
func SilinenTemizleBir(ctx context.Context, db *sql.DB, id int64, uzagiYoksay bool) error {
	var alanAdi, sk, js, hTip, hHost, hKul, hParola, hDizin, durum string
	var hPort int
	var silinme time.Time
	err := db.QueryRowContext(ctx,
		`SELECT alan_adi, sistem_kullanici, dosyalar, hedef_tip, hedef_host, hedef_port, hedef_kullanici, hedef_parola, hedef_dizin, durum, silinme
		 FROM cp_silinen_yedek WHERE id=?`, id).
		Scan(&alanAdi, &sk, &js, &hTip, &hHost, &hPort, &hKul, &hParola, &hDizin, &durum, &silinme)
	if err != nil {
		return err
	}
	if durum != "saklaniyor" {
		return nil
	}
	yerel, devralindi := silinenDosyalari(ctx, db, sk, silinme)
	var dosyalar []string
	_ = json.Unmarshal([]byte(js), &dosyalar)
	uzakAdlar := map[string]bool{}
	for _, f := range dosyalar {
		if f == filepath.Base(f) {
			uzakAdlar[f] = true
		}
	}
	for f := range yerel {
		uzakAdlar[f] = true
	}

	if !uzagiYoksay && len(uzakAdlar) > 0 {
		var d *Destination
		if strings.TrimSpace(hHost) != "" {
			d = &Destination{Tip: hTip, Host: hHost, Port: hPort, Kullanici: hKul,
				Parola: gizli.CozBagli(hParola, "yedek"), UzakDizin: hDizin, Aktif: true}
		}
		var ilk error
		kalan := 0
		for f := range uzakAdlar {
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
	var hata []string
	for f := range yerel {
		if e := os.Remove(filepath.Join(yedekKok, sk, f)); e != nil && !os.IsNotExist(e) {
			hata = append(hata, e.Error())
		}
	}
	if len(hata) > 0 {
		msg := "yerel dosya silinemedi: " + hata[0]
		_, _ = db.ExecContext(ctx, `UPDATE cp_silinen_yedek SET son_hata=? WHERE id=?`, msg, id)
		return errors.New(msg)
	}
	// Dizin yalnız sk'yi kimse devralmadıysa ve boşsa kaldırılır (Remove: dolu dizine dokunmaz).
	if !devralindi {
		_ = os.Remove(filepath.Join(yedekKok, sk))
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
	var sk, durum, alanAdi string
	var silinme time.Time
	if err := h.DB.QueryRowContext(r.Context(), `SELECT sistem_kullanici, durum, alan_adi, silinme FROM cp_silinen_yedek WHERE id=?`, id).
		Scan(&sk, &durum, &alanAdi, &silinme); err != nil || durum != "saklaniyor" {
		httpx.WriteError(w, http.StatusNotFound, "kayıt bulunamadı")
		return
	}
	dos, _ := silinenDosyalari(r.Context(), h.DB, sk, silinme)
	if _, ok := dos[ad]; !ok || ad != filepath.Base(ad) {
		httpx.WriteError(w, http.StatusNotFound, "yedek dosyası bulunamadı")
		return
	}
	f, err := os.Open(filepath.Join(yedekKok, sk, ad))
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
