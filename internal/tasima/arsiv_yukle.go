package tasima

// arsiv_yukle.go — parçalı + devam edebilir arşiv YÜKLEMESİ (bayi ve kök).
//
// Akış: POST /tasima/arsiv/yukle {ad,boyut} → kayıt + boş dosya; PUT .../{id}?konum=N ham
// parça (konum sunucudaki `alinan`a EŞİT olmalı, değilse 409 + alinan → istemci kaldığı
// yerden sürer); POST .../{id}/bitir → boyut + biçim imzası denetimi, 'hazir'.
//
// 🔴 Güvenlik/kota:
//   - dosya yolu SUNUCUDA türetilir (<arsivYukleKok>/<rid>/<id>.arsiv, dizinler 0700 root);
//     istemcinin adı yalnız görüntü etiketidir, yola GİRMEZ → kiracılar birbirinin arşivini
//     göremez/okuyamaz, yol kaçışı yok;
//   - tavan 50 GiB (arsiv.YuklemeTavan); kapsam başına toplam (yukleniyor+hazir) da bu tavanı
//     aşamaz (geçici dizin kotası); aynı anda en çok 3 kayıt;
//   - bayide disk havuzu (max_disk_mb) arşiv boyutuna yetmiyorsa baştan 413;
//   - parça, bildirilen boyutu aşamaz; aşan parça geri alınır (413);
//   - kayıtlar IDOR'a karşı sahip_rid ile süzülür; kapsam dışı ve olmayan kayıt AYNI 404;
//   - işe bağlı (kuyrukta/çalışıyor) yükleme silinemez; yarım yüklemeler 24 sa, kullanılmayan
//     hazır yüklemeler 72 sa sonra silinir (AcilistaTemizle + iş sonu).

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/httpx"
	"girginospanel/internal/tasima/arsiv"
)

// arsivYukleKok — yüklenen arşivlerin geçici dizini (testler geçici dizine çevirir).
var arsivYukleKok = "/var/lib/girginospanel/tasima/arsiv"

const (
	yuklemeParca     int64 = 8 << 20  // istemciye bildirilen parça boyutu
	yuklemeMaxIstek  int64 = 16 << 20 // tek PUT gövdesi üst sınırı
	yuklemeMaxKayit        = 3        // kapsam başına eşzamanlı kayıt
	yuklemeYarimOmru       = 24 * time.Hour
	yuklemeHazirOmru       = 72 * time.Hour
	yuklemeBosPayi   int64 = 1 << 30 // staging dosya sisteminde boyutun üstüne boş kalacak
)

var errArsivYukleme = fmt.Errorf("arşiv yüklemesi reddedildi")

// yuklemeYolu — kayıt dosyasının sunucudaki yolu (id ve rid sayıdır; ad yola girmez).
func yuklemeYolu(rid, id int64) string {
	return filepath.Join(arsivYukleKok, strconv.FormatInt(rid, 10), strconv.FormatInt(id, 10)+".arsiv")
}

// yuklemeKilit — aynı kaydın parçaları sıralı işlenir.
var (
	yuklemeKilitMu  sync.Mutex
	yuklemeKilitler = map[int64]*sync.Mutex{}
)

func yuklemeKilidi(id int64) *sync.Mutex {
	yuklemeKilitMu.Lock()
	defer yuklemeKilitMu.Unlock()
	m := yuklemeKilitler[id]
	if m == nil {
		m = &sync.Mutex{}
		yuklemeKilitler[id] = m
	}
	return m
}

// arsivKapi — arşiv uçları için kapsam + rol/anahtar kapısı: arşiv kaynaklarından en az biri
// bu kapsamda kullanılabilir olmalı (kök: etkin; bayi: etkin + bayi_etkin + genel anahtar).
func (h *Handlers) arsivKapi(w http.ResponseWriter, r *http.Request) (istekKapsami, bool) {
	kp, kod, msj := kapsamAl(r)
	if !kapsamHatasi(w, kod, msj) {
		return kp, false
	}
	var hata error = errKaynakKapali
	for _, s := range kaynakSurucuSirali() {
		if !s.Arsiv {
			continue
		}
		if err := h.kaynakKullanilabilir(r.Context(), kp.Kapsam, s.Kod); err == nil {
			return kp, true
		} else if err == errTasimaKapali {
			hata = err
		}
	}
	httpx.WriteError(w, http.StatusForbidden, hata.Error())
	return kp, false
}

func yuklemeAdiGecerli(ad string) bool {
	if ad == "" || len(ad) > 190 {
		return false
	}
	for _, r := range ad {
		if unicode.IsControl(r) || r == '/' || r == '\\' || r == 0xFFFD {
			return false
		}
	}
	return true
}

type yuklemeKayit struct {
	ID        int64     `json:"id"`
	Ad        string    `json:"ad"`
	Boyut     int64     `json:"boyut"`
	Alinan    int64     `json:"alinan"`
	Durum     string    `json:"durum"`
	Olusturma time.Time `json:"olusturma"`
}

func (h *Handlers) yuklemeOku(ctx context.Context, kp Kapsam, id int64) (*yuklemeKayit, error) {
	var y yuklemeKayit
	err := h.DB.QueryRowContext(ctx,
		`SELECT id, ad, boyut, alinan, durum, olusturma FROM tasima_yuklemeleri WHERE id=? AND sahip_rid=?`,
		id, kp.RID).Scan(&y.ID, &y.Ad, &y.Boyut, &y.Alinan, &y.Durum, &y.Olusturma)
	if err != nil {
		return nil, err
	}
	return &y, nil
}

func yuklemeBulunamadi(w http.ResponseWriter) {
	httpx.WriteError(w, http.StatusNotFound, "yükleme bulunamadı")
}

// yuklemeErisim — URL'deki kayıt bu kapsamın mı; değilse (yok/başkasının) AYNI 404.
func (h *Handlers) yuklemeErisim(w http.ResponseWriter, r *http.Request, kp Kapsam) (*yuklemeKayit, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		yuklemeBulunamadi(w)
		return nil, false
	}
	y, err := h.yuklemeOku(r.Context(), kp, id)
	if err != nil {
		if err != sql.ErrNoRows {
			log.Printf("tasima: yukleme okunamadi: %v", err)
		}
		yuklemeBulunamadi(w)
		return nil, false
	}
	return y, true
}

// YuklemeBaslat — POST /tasima/arsiv/yukle {ad, boyut}
func (h *Handlers) YuklemeBaslat(w http.ResponseWriter, r *http.Request) {
	kp, ok := h.arsivKapi(w, r)
	if !ok {
		return
	}
	if !bayiHizIzni(kp.Kapsam) {
		httpx.WriteError(w, http.StatusTooManyRequests, "çok sık deneme — bir dakika sonra yeniden deneyin")
		return
	}
	var g struct {
		Ad    string `json:"ad"`
		Boyut int64  `json:"boyut"`
	}
	if err := decodeJSON(w, r, 4096, &g); err != nil || !yuklemeAdiGecerli(g.Ad) || g.Boyut < 512 {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek")
		return
	}
	if g.Boyut > arsiv.YuklemeTavan {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("arşiv en çok %d GiB olabilir", arsiv.YuklemeTavan>>30))
		return
	}
	ctx := r.Context()
	// Kapsam başına geçici dizin kotası + kayıt sayısı (yalnız KENDİ kayıtları).
	var toplam int64
	var adet int
	if err := h.DB.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(boyut),0), COUNT(*) FROM tasima_yuklemeleri WHERE sahip_rid=?`, kp.RID).Scan(&toplam, &adet); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "yükleme kaydı oluşturulamadı")
		return
	}
	if adet >= yuklemeMaxKayit {
		httpx.WriteError(w, http.StatusConflict, "en çok 3 yükleme tutulabilir; birini silin")
		return
	}
	if toplam+g.Boyut > arsiv.YuklemeTavan {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "geçici yükleme alanı kotanız dolu; önceki yüklemeyi silin")
		return
	}
	// Bayi disk havuzu: arşiv (en azından) havuzun kalanına sığmalı.
	if kp.Bayi() {
		if kalan, sinirli, err := h.bayiDiskKalan(ctx, kp.RID); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "yükleme kaydı oluşturulamadı")
			return
		} else if sinirli && g.Boyut > kalan {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, errArsivKota.Error())
			return
		}
	}
	if err := os.MkdirAll(filepath.Join(arsivYukleKok, strconv.FormatInt(kp.RID, 10)), 0o700); err != nil {
		log.Printf("tasima: yukleme dizini: %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, "yükleme alanı hazırlanamadı")
		return
	}
	if bos, err := diskBosAlan(arsivYukleKok); err != nil || bos < g.Boyut+yuklemeBosPayi {
		httpx.WriteError(w, http.StatusInsufficientStorage, "sunucuda geçici yükleme alanı yetersiz")
		return
	}
	res, err := h.DB.ExecContext(ctx,
		`INSERT INTO tasima_yuklemeleri (sahip_rid, yukleyen_uid, ad, boyut) VALUES (?, NULLIF(?,0), ?, ?)`,
		kp.RID, kp.UID, g.Ad, g.Boyut)
	if err != nil {
		log.Printf("tasima: yukleme kaydi: %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, "yükleme kaydı oluşturulamadı")
		return
	}
	id, _ := res.LastInsertId()
	f, err := os.OpenFile(yuklemeYolu(kp.RID, id), os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		_, _ = h.DB.ExecContext(ctx, `DELETE FROM tasima_yuklemeleri WHERE id=?`, id)
		log.Printf("tasima: yukleme dosyasi: %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, "yükleme alanı hazırlanamadı")
		return
	}
	f.Close()
	httpx.Denetim(h.DB, r, kp.UID, kp.Kul, "tasima_arsiv_yukle_basla", strconv.FormatInt(id, 10),
		fmt.Sprintf("%d bayt", g.Boyut), kp.RID, true)
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"id": id, "boyut": g.Boyut, "alinan": 0, "parca": yuklemeParca})
}

// decodeJSON — küçük JSON gövdesi (sınırlı).
func decodeJSON(w http.ResponseWriter, r *http.Request, sinir int64, v any) error {
	return json.NewDecoder(http.MaxBytesReader(w, r.Body, sinir)).Decode(v)
}

// YuklemeDurum — GET /tasima/arsiv/yukle/{id}
func (h *Handlers) YuklemeDurum(w http.ResponseWriter, r *http.Request) {
	kp, ok := h.arsivKapi(w, r)
	if !ok {
		return
	}
	y, ok := h.yuklemeErisim(w, r, kp.Kapsam)
	if !ok {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, y)
}

// YuklemeListe — GET /tasima/arsiv/yuklemeler (yalnız kendi kapsamı)
func (h *Handlers) YuklemeListe(w http.ResponseWriter, r *http.Request) {
	kp, ok := h.arsivKapi(w, r)
	if !ok {
		return
	}
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, ad, boyut, alinan, durum, olusturma FROM tasima_yuklemeleri WHERE sahip_rid=? ORDER BY id DESC LIMIT 25`, kp.RID)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "yüklemeler okunamadı")
		return
	}
	defer rows.Close()
	out := []yuklemeKayit{}
	for rows.Next() {
		var y yuklemeKayit
		if rows.Scan(&y.ID, &y.Ad, &y.Boyut, &y.Alinan, &y.Durum, &y.Olusturma) == nil {
			out = append(out, y)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"yuklemeler": out})
}

// YuklemeParca — PUT /tasima/arsiv/yukle/{id}?konum=N  (gövde = ham bayt)
func (h *Handlers) YuklemeParca(w http.ResponseWriter, r *http.Request) {
	kp, ok := h.arsivKapi(w, r)
	if !ok {
		return
	}
	y, ok := h.yuklemeErisim(w, r, kp.Kapsam)
	if !ok {
		return
	}
	kil := yuklemeKilidi(y.ID)
	kil.Lock()
	defer kil.Unlock()
	// Kilit altında yeniden oku: eşzamanlı parça/bitir/silme sonrası durumla karar ver.
	y, err := h.yuklemeOku(r.Context(), kp.Kapsam, y.ID)
	if err != nil {
		yuklemeBulunamadi(w)
		return
	}
	if y.Durum != "yukleniyor" {
		httpx.WriteError(w, http.StatusConflict, "yükleme tamamlanmış")
		return
	}
	konum, err := strconv.ParseInt(r.URL.Query().Get("konum"), 10, 64)
	if err != nil || konum < 0 {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz konum")
		return
	}
	if konum != y.Alinan {
		httpx.WriteJSON(w, http.StatusConflict, map[string]any{"hata": "konum sunucudaki alınan bayttan farklı", "alinan": y.Alinan})
		return
	}
	kalan := y.Boyut - y.Alinan
	if r.ContentLength > kalan || r.ContentLength > yuklemeMaxIstek {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "parça bildirilen boyutu aşıyor")
		return
	}
	f, err := os.OpenFile(yuklemeYolu(kp.RID, y.ID), os.O_WRONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		log.Printf("tasima: yukleme dosyasi acilamadi: %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, "yükleme dosyası açılamadı")
		return
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
		httpx.WriteError(w, http.StatusInternalServerError, "yükleme dosyası geçersiz")
		return
	}
	limit := yuklemeMaxIstek
	if kalan < limit {
		limit = kalan
	}
	// limit+1: sınırı AŞAN gövde (ContentLength bilinmiyorsa) fark edilir.
	n, err := io.Copy(io.NewOffsetWriter(f, konum), io.LimitReader(r.Body, limit+1))
	if err != nil || n == 0 || n > limit {
		_ = f.Truncate(konum) // yarım/aşan parça atılır; alinan DEĞİŞMEDİ
		if n > limit {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "parça bildirilen boyutu aşıyor")
		} else {
			httpx.WriteError(w, http.StatusBadRequest, "parça okunamadı")
		}
		return
	}
	if err := f.Sync(); err != nil {
		_ = f.Truncate(konum)
		httpx.WriteError(w, http.StatusInsufficientStorage, "parça yazılamadı")
		return
	}
	res, err := h.DB.ExecContext(r.Context(),
		`UPDATE tasima_yuklemeleri SET alinan=? WHERE id=? AND sahip_rid=? AND alinan=? AND durum='yukleniyor'`,
		konum+n, y.ID, kp.RID, konum)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "ilerleme kaydedilemedi")
		return
	}
	if a, _ := res.RowsAffected(); a == 0 {
		httpx.WriteError(w, http.StatusConflict, "yükleme durumu değişti")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"alinan": konum + n})
}

// arsivSihirGecerli — ilk baytlar bilinen arşiv biçimi mi (gzip, bzip2, xz, zstd, düz tar).
func arsivSihirGecerli(b []byte) bool {
	switch {
	case len(b) >= 2 && b[0] == 0x1f && b[1] == 0x8b:
		return true
	case len(b) >= 3 && string(b[:3]) == "BZh":
		return true
	case len(b) >= 6 && string(b[:6]) == "\xfd7zXZ\x00":
		return true
	case len(b) >= 4 && string(b[:4]) == "\x28\xb5\x2f\xfd":
		return true
	case len(b) >= 262 && string(b[257:262]) == "ustar":
		return true
	}
	return false
}

// YuklemeBitir — POST /tasima/arsiv/yukle/{id}/bitir
func (h *Handlers) YuklemeBitir(w http.ResponseWriter, r *http.Request) {
	kp, ok := h.arsivKapi(w, r)
	if !ok {
		return
	}
	y, ok := h.yuklemeErisim(w, r, kp.Kapsam)
	if !ok {
		return
	}
	kil := yuklemeKilidi(y.ID)
	kil.Lock()
	defer kil.Unlock()
	y, err := h.yuklemeOku(r.Context(), kp.Kapsam, y.ID)
	if err != nil {
		yuklemeBulunamadi(w)
		return
	}
	if y.Durum != "yukleniyor" || y.Alinan != y.Boyut {
		httpx.WriteError(w, http.StatusConflict, "yükleme tamamlanmadı")
		return
	}
	yol := yuklemeYolu(kp.RID, y.ID)
	f, err := os.OpenFile(yol, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	var bas [512]byte
	n := 0
	if err == nil {
		st, serr := f.Stat()
		if serr != nil || !st.Mode().IsRegular() || st.Size() != y.Boyut {
			err = fmt.Errorf("boyut uyuşmuyor")
		} else {
			n, _ = io.ReadFull(f, bas[:])
		}
		f.Close()
	}
	if err != nil || !arsivSihirGecerli(bas[:n]) {
		// Biçimi tanınmayan yükleme tutulmaz (disk + kota geri verilir).
		h.yuklemeSil(r.Context(), kp.RID, y.ID)
		httpx.WriteError(w, http.StatusUnprocessableEntity, "dosya tanınan bir arşiv biçimi değil (tar.gz, tar.bz2, tar.xz, tar.zst, tar)")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE tasima_yuklemeleri SET durum='hazir' WHERE id=? AND sahip_rid=? AND durum='yukleniyor'`, y.ID, kp.RID); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "yükleme tamamlanamadı")
		return
	}
	httpx.Denetim(h.DB, r, kp.UID, kp.Kul, "tasima_arsiv_yukle_bitti", strconv.FormatInt(y.ID, 10),
		fmt.Sprintf("%d bayt", y.Boyut), kp.RID, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"id": y.ID, "durum": "hazir", "boyut": y.Boyut})
}

// yuklemeSil — kaydı, dosyayı ve ona bağlı keşif oturumlarını siler.
func (h *Handlers) yuklemeSil(ctx context.Context, rid, id int64) {
	_, _ = h.DB.ExecContext(ctx, `DELETE FROM tasima_isleri WHERE arsiv_yukleme_id=? AND durum='oturum' AND sahip_rid=?`, id, rid)
	_, _ = h.DB.ExecContext(ctx, `DELETE FROM tasima_yuklemeleri WHERE id=? AND sahip_rid=?`, id, rid)
	if err := os.Remove(yuklemeYolu(rid, id)); err != nil && !os.IsNotExist(err) {
		log.Printf("tasima: yukleme dosyasi silinemedi (id=%d): %v", id, err)
	}
}

// YuklemeSil — DELETE /tasima/arsiv/yukle/{id}
func (h *Handlers) YuklemeSil(w http.ResponseWriter, r *http.Request) {
	kp, ok := h.arsivKapi(w, r)
	if !ok {
		return
	}
	y, ok := h.yuklemeErisim(w, r, kp.Kapsam)
	if !ok {
		return
	}
	kil := yuklemeKilidi(y.ID)
	kil.Lock()
	defer kil.Unlock()
	var aktif int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM tasima_isleri WHERE arsiv_yukleme_id=? AND durum IN ('kuyrukta','calisiyor')`, y.ID).Scan(&aktif); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "yükleme silinemedi")
		return
	}
	if aktif > 0 {
		httpx.WriteError(w, http.StatusConflict, "bu arşiv bir taşıma işinde kullanılıyor")
		return
	}
	h.yuklemeSil(r.Context(), kp.RID, y.ID)
	httpx.Denetim(h.DB, r, kp.UID, kp.Kul, "tasima_arsiv_yukle_sil", strconv.FormatInt(y.ID, 10), "", kp.RID, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// arsivYuklemeTemizle — süresi dolan yüklemeleri siler ve kayıtsız artık dosyaları kaldırır.
// Açılışta ve her arşiv işinin sonunda çağrılır; çalışan işin yüklemesine dokunmaz.
func (h *Handlers) arsivYuklemeTemizle() {
	rows, err := h.DB.Query(
		`SELECT y.id, y.sahip_rid FROM tasima_yuklemeleri y
		  WHERE ((y.durum='yukleniyor' AND y.guncelleme < (NOW() - INTERVAL ? SECOND))
		      OR (y.durum='hazir' AND y.guncelleme < (NOW() - INTERVAL ? SECOND)))
		    AND NOT EXISTS (SELECT 1 FROM tasima_isleri t WHERE t.arsiv_yukleme_id=y.id AND t.durum IN ('kuyrukta','calisiyor'))`,
		int(yuklemeYarimOmru.Seconds()), int(yuklemeHazirOmru.Seconds()))
	if err == nil {
		type kayit struct{ id, rid int64 }
		var eski []kayit
		for rows.Next() {
			var k kayit
			if rows.Scan(&k.id, &k.rid) == nil {
				eski = append(eski, k)
			}
		}
		rows.Close()
		for _, k := range eski {
			h.yuklemeSil(context.Background(), k.rid, k.id)
		}
	}
	// Kayıtsız artık dosyalar (kayıt silinmiş/göç): <rid>/<id>.arsiv, 1 günden eski.
	ridler, err := os.ReadDir(arsivYukleKok)
	if err != nil {
		return
	}
	for _, rd := range ridler {
		rid, err := strconv.ParseInt(rd.Name(), 10, 64)
		if err != nil || !rd.IsDir() {
			continue
		}
		dosyalar, err := os.ReadDir(filepath.Join(arsivYukleKok, rd.Name()))
		if err != nil {
			continue
		}
		for _, d := range dosyalar {
			ad := d.Name()
			id, err := strconv.ParseInt(strings.TrimSuffix(ad, ".arsiv"), 10, 64)
			if err != nil || !strings.HasSuffix(ad, ".arsiv") {
				continue
			}
			fi, err := d.Info()
			if err != nil || time.Since(fi.ModTime()) < 24*time.Hour {
				continue
			}
			var n int
			if err := h.DB.QueryRow(`SELECT COUNT(*) FROM tasima_yuklemeleri WHERE id=? AND sahip_rid=?`, id, rid).Scan(&n); err == nil && n == 0 {
				_ = os.Remove(filepath.Join(arsivYukleKok, rd.Name(), ad))
			}
		}
	}
}

// bayiDiskKalan — bayinin disk havuzundan kalan bayt. sinirli=false: max_disk_mb 0 (sınırsız).
func (h *Handlers) bayiDiskKalan(ctx context.Context, rid int64) (kalan int64, sinirli bool, err error) {
	var maxMB int64
	if err := h.DB.QueryRowContext(ctx, `SELECT max_disk_mb FROM users WHERE id=? AND role='reseller'`, rid).Scan(&maxMB); err != nil {
		return 0, false, err
	}
	if maxMB <= 0 {
		return 0, false, nil
	}
	var kullanKB int64
	if err := h.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(boyut_kb),0) FROM domains WHERE reseller_id=?`, rid).Scan(&kullanKB); err != nil {
		return 0, false, err
	}
	kalan = maxMB<<20 - kullanKB<<10
	if kalan < 0 {
		kalan = 0
	}
	return kalan, true, nil
}
