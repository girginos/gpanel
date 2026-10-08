package backups

// Silinen hosting/bayi yedekleri ("yetim yedek").
//
// Karar (kullanıcı, 2026-10-08): hosting/bayi silme YEDEKLERDEN BAĞIMSIZDIR. Silme yolu yedek
// dizinine/uzak kopyalara hiç dokunmaz ve yedekler yüzünden ASLA başarısız olmaz/beklemez.
//
// Tasarım: backups.domain_id → domains(id) FK'si KALKTI (migrations/0217): domain silinince yedeğin
// DB satırı yerinde kalır, YEDEK SAYFASINDAN geri yüklenebilir. Silme `DELETE FROM domains`
// başarılı olunca TEK best-effort UPDATE ile satırlara alan_adi / sistem_kullanici / reseller_id /
// silinme / son_kullanma yazar (SilinenIz.Yaz). Süre cp_ayarlar.silinen_yedek_gun: boş = hostingin
// yedek slotu (en az 7 gün), 0 = otomatik silme yok, N = sabit gün (≤3650).
//
//	Yetim yedek = silinme IS NOT NULL AND NOT EXISTS(SELECT 1 FROM domains WHERE id=backups.domain_id)
//
// 🔴 Yetim satırlar HİÇBİR canlı akışa girmemeli ve kendi başına silinmemeli: canlı akışların hepsi
// ya domain_id bazlıdır ya da JOIN domains yapar (bkz. uzak/otomatik.go TopluAdaylar ve
// uzak/kuyruk.go SimdiGonder'deki EXISTS(domains) koruması). Yetimi yalnız şunlar siler: 04:00
// süpürmesi (süresi dolanı), yönetici DELETE ucu.
//
// 🔴 Aynı sk/alan adı yeniden açılırsa: AYNI alan adı + AYNI sahip (reseller_id) + AYNI sk ise yetimler
// domainin yedek sayfası açılınca otomatik bağlanır (yetimleriOtoBagla); farklıysa YALNIZ yönetici
// elle bağlar (başka müşterinin yedeği sızmasın).

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
	"strconv"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"

	"github.com/go-chi/chi/v5"
)

// yedekKok: saklama işlemlerinin kökü (testte geçici dizine çevrilir).
var yedekKok = BackupRoot

const (
	silinenAyarKey  = "silinen_yedek_gun"
	silinenEnAzGun  = 7
	silinenEnCokGun = 3650
	// silinenSupurLimit: tek turda en çok kaç yetim yedek temizlenir (kalanı ertesi gün).
	silinenSupurLimit = 200
	silinenGrupLimit  = 500
	silinenSatirLimit = 20000
)

// yetimKosulu: yetim yedek SQL koşulu (backups tablosu "backups" adıyla, takma adsız).
const yetimKosulu = `silinme IS NOT NULL AND NOT EXISTS (SELECT 1 FROM domains d WHERE d.id=backups.domain_id)`

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

var silinenSKRe = regexp.MustCompile(`^c_[a-z0-9_]{1,40}$`)

func silinenSKGecerli(sk string) bool { return silinenSKRe.MatchString(sk) }

// yedekDosyaSKRe: yedek dosya adından sk türetme (<sk>-YYYYMMDD-HHMMSS.tar.gz).
var yedekDosyaSKRe = regexp.MustCompile(`^(c_[a-z0-9_]+)-\d{8}-\d{6}\.tar\.gz$`)

// skDosyaAdindan — silme izi yazılamamış (silinme NULL) eski/yarım satırlar için sk'yı dosya adından
// türetir. Yalnız tanılama/elle kurtarma içindir: bu satırlar otomatik listelenmez/temizlenmez.
func skDosyaAdindan(dosya string) (string, bool) {
	m := yedekDosyaSKRe.FindStringSubmatch(dosya)
	if m == nil || !silinenSKGecerli(m[1]) {
		return "", false
	}
	return m[1], true
}

// dosyaAdiGuvenli: yedek dosya adı tek bir yol bileşeni ve .tar.gz mi?
func dosyaAdiGuvenli(ad string) bool {
	return ad != "" && ad == filepath.Base(ad) && ad != "." && ad != ".." && strings.HasSuffix(ad, ".tar.gz")
}

// yedekDizinGuvenli — BackupRoot/<sk> gerçek bir dizin mi (symlink DEĞİL)? Symlink ise dışarıya
// okuma/silme/indirme açılırdı.
func yedekDizinGuvenli(sk string) bool {
	if !silinenSKGecerli(sk) {
		return false
	}
	fi, err := os.Lstat(filepath.Join(yedekKok, sk))
	return err == nil && fi.IsDir() && fi.Mode()&os.ModeSymlink == 0
}

// yedekDizinSilinebilir: dizin yok (silinecek dosya zaten yok) ya da güvenli gerçek dizin.
// Var ama symlink/dosya ise false — üstünden silme dışarıya taşardı.
func yedekDizinSilinebilir(sk string) bool {
	if !silinenSKGecerli(sk) {
		return false
	}
	fi, err := os.Lstat(filepath.Join(yedekKok, sk))
	if err != nil {
		return os.IsNotExist(err)
	}
	return fi.IsDir() && fi.Mode()&os.ModeSymlink == 0
}

// SilinenIz — silinecek hostingin yetim-işaret bilgisi (domains satırı gitmeden ÖNCE okunur).
type SilinenIz struct {
	alanAdi, sk string
	domainID    int64
	resellerID  int64
	slot        int
}

// SilinenIzHazirla — domains satırı gitmeden ÖNCE çağrılır; YALNIZ DB okur (dizine dokunmaz).
// Geçersiz sk ya da hatada nil döner; nil iz Yaz'da yok sayılır. Panik yutulur.
func SilinenIzHazirla(ctx context.Context, db *sql.DB, domainID int64, alanAdi, sk string) (iz *SilinenIz) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("silinen yedek iz hazırlama: panik (%s): %v", alanAdi, r)
			iz = nil
		}
	}()
	if !silinenSKGecerli(sk) {
		return nil
	}
	ctx, iptal := context.WithTimeout(ctx, 10*time.Second)
	defer iptal()
	iz = &SilinenIz{alanAdi: alanAdi, sk: sk, domainID: domainID, slot: 7}
	_ = db.QueryRowContext(ctx, `SELECT COALESCE(reseller_id,0), COALESCE(backup_retention,7) FROM domains WHERE id=?`, domainID).
		Scan(&iz.resellerID, &iz.slot)
	return iz
}

// Yaz — hosting satırı SİLİNDİKTEN sonra çağrılır (silme başarısızsa iz bırakılmaz): o domainin yedek
// satırlarını yetim olarak işaretler. Hata vermez/engellemez, 10 sn zaman aşımı, panik yutulur.
func (iz *SilinenIz) Yaz(ctx context.Context, db *sql.DB) {
	if iz == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("silinen yedek iz: panik (%s): %v", iz.alanAdi, r)
		}
	}()
	if err := iz.yaz(ctx, db); err != nil {
		log.Printf("silinen yedek iz yazılamadı (%s; yedekler yerinde duruyor, yetim sayılmayacak): %v", iz.alanAdi, err)
	}
}

func (iz *SilinenIz) yaz(ctx context.Context, db *sql.DB) error {
	ctx, iptal := context.WithTimeout(ctx, 10*time.Second)
	defer iptal()
	gun := silinenSaklamaGunu(silinenAyar(ctx, db), iz.slot)
	q := `UPDATE backups SET alan_adi=?, sistem_kullanici=?, reseller_id=?, silinme=NOW(), son_kullanma=NULL WHERE domain_id=?`
	args := []any{iz.alanAdi, iz.sk, iz.resellerID, iz.domainID}
	if gun > 0 { // 0 = otomatik silme yok (son_kullanma NULL)
		q = `UPDATE backups SET alan_adi=?, sistem_kullanici=?, reseller_id=?, silinme=NOW(), son_kullanma=DATE_ADD(NOW(), INTERVAL ? DAY) WHERE domain_id=?`
		args = []any{iz.alanAdi, iz.sk, iz.resellerID, gun, iz.domainID}
	}
	_, err := db.ExecContext(ctx, q, args...)
	return err
}

// SilinenIsaretle — Hazirla + Yaz (domain satırı ZATEN silindiyse; testler/ara çağıranlar için).
func SilinenIsaretle(ctx context.Context, db *sql.DB, domainID int64, alanAdi, sk string) {
	SilinenIzHazirla(ctx, db, domainID, alanAdi, sk).Yaz(ctx, db)
}

// SilinenIsaretsizTani — domain'i olmayan ama yetim işaretsiz (silinme NULL) satırları sayar:
// toplam ve dosya adından sk türetilebilen kısmı. Bu satırlar listelenmez/temizlenmez (güvenli
// taraf); yönetici isterse SQL ile işaretler. Yalnız log için.
func SilinenIsaretsizTani(ctx context.Context, db *sql.DB) (toplam, turetilebilir int) {
	rows, err := db.QueryContext(ctx,
		`SELECT b.dosya FROM backups b WHERE b.silinme IS NULL AND NOT EXISTS (SELECT 1 FROM domains d WHERE d.id=b.domain_id) LIMIT 5000`)
	if err != nil {
		return 0, 0
	}
	defer rows.Close()
	for rows.Next() {
		var d string
		if rows.Scan(&d) != nil {
			continue
		}
		toplam++
		if _, ok := skDosyaAdindan(d); ok {
			turetilebilir++
		}
	}
	return toplam, turetilebilir
}

// yetimleriOtoBagla — domainin alan adı + sahibi (reseller_id) + sk'sı ile BİREBİR aynı olan yetim
// yedekleri bu domaine bağlar (lazy; yedek sayfası açılınca). Sahip/alan adı farklıysa dokunmaz.
// Bağlanan sayısını döner; hata sessizce loglanır (liste yine açılır).
func yetimleriOtoBagla(ctx context.Context, db *sql.DB, domainID int64) int64 {
	var alan, sk string
	var rid int64
	if err := db.QueryRowContext(ctx, `SELECT alan_adi, sistem_kullanici, COALESCE(reseller_id,0) FROM domains WHERE id=?`, domainID).
		Scan(&alan, &sk, &rid); err != nil || alan == "" || !silinenSKGecerli(sk) {
		return 0
	}
	res, err := db.ExecContext(ctx,
		`UPDATE backups SET domain_id=?, silinme=NULL, son_kullanma=NULL
		 WHERE sistem_kullanici=? AND alan_adi=? AND reseller_id=? AND `+yetimKosulu, domainID, sk, alan, rid)
	if err != nil {
		log.Printf("yetim yedek otomatik bağlama (domain=%d): %v", domainID, err)
		return 0
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		log.Printf("yetim yedek otomatik bağlandı: domain=%d sk=%s adet=%d", domainID, sk, n)
	}
	return n
}

// yetimYedegiSil — BİR yetim yedeği kalıcı siler: uzak → yerel dosya → DB satırı (yedekKaydiniYokEt).
// uzagiYoksay: uzak adımı atlar. Uzak silinemezse satır + dosya KALIR (hata döner).
// Güvenlik: sk regex, dosya adı tek bileşen + .tar.gz, sk dizini symlink değil.
func yetimYedegiSil(ctx context.Context, db *sql.DB, bid, domainID int64, sk, dosya string, uzagiYoksay bool) error {
	if !silinenSKGecerli(sk) || !dosyaAdiGuvenli(dosya) {
		return fmt.Errorf("yedek %d: güvensiz sk/dosya adı, atlandı", bid)
	}
	if !yedekDizinSilinebilir(sk) {
		return fmt.Errorf("yedek %d: yedek dizini güvenli değil (symlink?), atlandı", bid)
	}
	if !uzagiYoksay {
		_, err := yedekKaydiniYokEt(ctx, db, domainID, sk, dosya, bid)
		return err
	}
	if e := os.Remove(filepath.Join(yedekKok, sk, dosya)); e != nil && !os.IsNotExist(e) {
		return e
	}
	_, err := db.ExecContext(ctx, `DELETE FROM backups WHERE id=? AND `+yetimKosulu, bid)
	return err
}

// silinenSupurKilit: aynı anda tek süpürme.
var silinenSupurKilit sync.Mutex

// SilinenSupur — süresi dolan yetim yedekleri temizler (günde bir, 04:00 turu). Uzak silinemezse
// satır kalır, ertesi gün tekrar denenir. Tur başına en çok silinenSupurLimit yedek.
func SilinenSupur(ctx context.Context, db *sql.DB) {
	if !silinenSupurKilit.TryLock() {
		return // zaten çalışıyor
	}
	defer silinenSupurKilit.Unlock()
	if top, tur := SilinenIsaretsizTani(ctx, db); top > 0 {
		log.Printf("yetim yedek tanılama: domain'i olmayıp silme izi olmayan %d yedek satırı var (%d tanesinin sk'sı dosya adından türetilebilir) — otomatik temizlenmez", top, tur)
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id, domain_id, sistem_kullanici, dosya FROM backups
		 WHERE son_kullanma IS NOT NULL AND son_kullanma<=NOW() AND `+yetimKosulu+` ORDER BY id LIMIT ?`, silinenSupurLimit)
	if err != nil {
		log.Printf("yetim yedek süpürme: %v", err)
		return
	}
	type kayit struct {
		id, domainID int64
		sk, dosya    string
	}
	var liste []kayit
	for rows.Next() {
		var k kayit
		if rows.Scan(&k.id, &k.domainID, &k.sk, &k.dosya) == nil {
			liste = append(liste, k)
		}
	}
	rows.Close()
	for _, k := range liste {
		if ctx.Err() != nil {
			log.Printf("yetim yedek süpürme: süre doldu, kalanı yarın")
			return
		}
		if e := yetimYedegiSil(ctx, db, k.id, k.domainID, k.sk, k.dosya, false); e != nil {
			log.Printf("yetim yedek %d (%s) temizlenemedi, kayıt korundu (yarın tekrar denenir): %v", k.id, k.dosya, e)
		} else {
			log.Printf("yetim yedek %d (%s) süresi doldu, temizlendi", k.id, k.dosya)
		}
	}
}

// ---------------------------------------------------------------- yönetici uçları

type silinenYedekDosya struct {
	ID        int64  `json:"id"`
	Dosya     string `json:"dosya"`
	BoyutB    int64  `json:"boyut_b"`
	Olusturma string `json:"olusturma"`
	Yerel     bool   `json:"yerel"`
}

type silinenGrup struct {
	SK                   string              `json:"sk"`
	AlanAdi              string              `json:"alan_adi"`
	ResellerID           int64               `json:"reseller_id"`
	Silinme              string              `json:"silinme"`
	SonKullanma          string              `json:"son_kullanma"` // boş = otomatik silinmez
	DosyaSayisi          int                 `json:"dosya_sayisi"`
	BoyutB               int64               `json:"boyut_b"`
	YerelVar             bool                `json:"yerel_var"`
	BaglanabilirDomainID *int64              `json:"baglanabilir_domain_id"`
	Yedekler             []silinenYedekDosya `json:"yedekler"`
}

// yerelDuzDosya: BackupRoot/<sk>/<dosya> diskte düz dosya mı (dizin ve dosya symlink değil)?
func yerelDuzDosya(sk, dosya string) bool {
	if !yedekDizinGuvenli(sk) || !dosyaAdiGuvenli(dosya) {
		return false
	}
	fi, err := os.Lstat(filepath.Join(yedekKok, sk, dosya))
	return err == nil && fi.Mode().IsRegular()
}

// SilinenListe: GET /admin/silinen-yedekler — sk'ya göre gruplu yetim yedekler.
func (h *Handlers) SilinenListe(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, sistem_kullanici, alan_adi, reseller_id, dosya, boyut_b,
		        DATE_FORMAT(created_at,'%Y-%m-%d %H:%i'), DATE_FORMAT(silinme,'%Y-%m-%d %H:%i'),
		        COALESCE(DATE_FORMAT(son_kullanma,'%Y-%m-%d %H:%i'),'')
		 FROM backups WHERE `+yetimKosulu+` ORDER BY silinme DESC, id DESC LIMIT ?`, silinenSatirLimit)
	if err != nil {
		log.Printf("silinen yedek: liste: %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, "liste okunamadı")
		return
	}
	defer rows.Close()
	out := []*silinenGrup{}
	idx := map[string]*silinenGrup{}
	for rows.Next() {
		var y silinenYedekDosya
		var sk, alan, silinme, sonK string
		var rid int64
		if rows.Scan(&y.ID, &sk, &alan, &rid, &y.Dosya, &y.BoyutB, &y.Olusturma, &silinme, &sonK) != nil {
			continue
		}
		g := idx[sk]
		if g == nil {
			if len(out) >= silinenGrupLimit {
				continue
			}
			g = &silinenGrup{SK: sk, AlanAdi: alan, ResellerID: rid, Silinme: silinme, SonKullanma: sonK, Yedekler: []silinenYedekDosya{}}
			idx[sk] = g
			out = append(out, g)
		}
		y.Yerel = yerelDuzDosya(sk, y.Dosya)
		g.YerelVar = g.YerelVar || y.Yerel
		g.BoyutB += y.BoyutB
		g.Yedekler = append(g.Yedekler, y)
	}
	rows.Close()
	for _, g := range out {
		g.DosyaSayisi = len(g.Yedekler)
		var did int64
		if silinenSKGecerli(g.SK) &&
			h.DB.QueryRowContext(r.Context(), `SELECT id FROM domains WHERE sistem_kullanici=? ORDER BY id LIMIT 1`, g.SK).Scan(&did) == nil {
			g.BaglanabilirDomainID = &did
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// SilinenIndir: GET /admin/silinen-yedekler/yedek/{bid}/indir — YALNIZ yetim satırın düz dosyası.
func (h *Handlers) SilinenIndir(w http.ResponseWriter, r *http.Request) {
	bid, _ := strconv.ParseInt(chi.URLParam(r, "bid"), 10, 64)
	var sk, dosya, alanAdi string
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici, dosya, alan_adi FROM backups WHERE id=? AND `+yetimKosulu, bid).
		Scan(&sk, &dosya, &alanAdi); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "yedek bulunamadı")
		return
	}
	if !yerelDuzDosya(sk, dosya) {
		httpx.WriteError(w, http.StatusNotFound, "yedek dosyası diskte yok")
		return
	}
	f, err := os.Open(filepath.Join(yedekKok, sk, dosya))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "yedek dosyası diskte yok")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		httpx.WriteError(w, http.StatusNotFound, "yedek dosyası okunamadı")
		return
	}
	uid, kul := middleware.Aktor(r)
	httpx.Denetim(h.DB, r, uid, kul, "silinen-yedek.indir", alanAdi, dosya, 0, true)
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(dosya, `"`, "")+`"`)
	http.ServeContent(w, r, dosya, fi.ModTime(), f)
}

// SilinenSil: DELETE /admin/silinen-yedekler/{sk}[?uzagi_yoksay=1] — o sk'nın yetim yedeklerini
// süreyi beklemeden kalıcı siler. Uzak silinemeyen satır + dosya KALIR (409).
func (h *Handlers) SilinenSil(w http.ResponseWriter, r *http.Request) {
	sk := chi.URLParam(r, "sk")
	if !silinenSKGecerli(sk) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz sistem kullanıcısı")
		return
	}
	yoksay := r.URL.Query().Get("uzagi_yoksay") == "1"
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, domain_id, dosya FROM backups WHERE sistem_kullanici=? AND `+yetimKosulu+` ORDER BY id LIMIT ?`, sk, silinenSatirLimit)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "kayıtlar okunamadı")
		return
	}
	type kayit struct {
		id, domainID int64
		dosya        string
	}
	var liste []kayit
	for rows.Next() {
		var k kayit
		if rows.Scan(&k.id, &k.domainID, &k.dosya) == nil {
			liste = append(liste, k)
		}
	}
	rows.Close()
	if len(liste) == 0 {
		httpx.WriteError(w, http.StatusNotFound, "silinmiş hesaba ait yedek bulunamadı")
		return
	}
	kctx, iptal := httpx.KopukBaglam(r, 30*time.Minute)
	defer iptal()
	uid, kul := middleware.Aktor(r)
	var ilk error
	kalan, silinen := 0, 0
	for _, k := range liste {
		if e := yetimYedegiSil(kctx, h.DB, k.id, k.domainID, sk, k.dosya, yoksay); e != nil {
			kalan++
			if ilk == nil {
				ilk = e
			}
			continue
		}
		silinen++
	}
	if kalan > 0 {
		log.Printf("silinen yedek %s silme: %d/%d silinemedi: %v", sk, kalan, len(liste), ilk)
		httpx.WriteJSON(w, http.StatusConflict, map[string]any{
			"ok": false, "hata": fmt.Sprintf("%d yedek silinemedi (kayıt ve dosya korundu): %v", kalan, ilk),
			"silinen": silinen, "uzagi_yoksay": true,
		})
		return
	}
	// Boşalan sk dizini: yalnız canlı bir hosting sk'yı kullanmıyorsa ve dizin boşsa (os.Remove dolu dizine dokunmaz).
	var canli int
	_ = h.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM domains WHERE sistem_kullanici=?`, sk).Scan(&canli)
	if canli == 0 && yedekDizinGuvenli(sk) {
		_ = os.Remove(filepath.Join(yedekKok, sk))
	}
	httpx.Denetim(h.DB, r, uid, kul, "silinen-yedek.temizle", sk, fmt.Sprintf("yedek=%d uzak_yoksay=%v", silinen, yoksay), 0, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "silinen": silinen})
}

// SilinenBagla: POST /admin/silinen-yedekler/{sk}/bagla {"domain_id":N} — yönetici kararıyla yetim
// yedekleri canlı bir domaine bağlar (sahip koşulu YOK; hedefin sk'sı eşit olmalı).
func (h *Handlers) SilinenBagla(w http.ResponseWriter, r *http.Request) {
	sk := chi.URLParam(r, "sk")
	if !silinenSKGecerli(sk) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz sistem kullanıcısı")
		return
	}
	var req struct {
		DomainID int64 `json:"domain_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || req.DomainID <= 0 {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	var hedefSK, alan string
	if err := h.DB.QueryRowContext(r.Context(), `SELECT sistem_kullanici, alan_adi FROM domains WHERE id=?`, req.DomainID).Scan(&hedefSK, &alan); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		} else {
			httpx.WriteError(w, http.StatusInternalServerError, "domain okunamadı")
		}
		return
	}
	if hedefSK != sk {
		httpx.WriteError(w, http.StatusBadRequest, "hedef domainin sistem kullanıcısı yedeklerinkiyle aynı değil")
		return
	}
	res, err := h.DB.ExecContext(r.Context(),
		`UPDATE backups SET domain_id=?, silinme=NULL, son_kullanma=NULL WHERE sistem_kullanici=? AND `+yetimKosulu, req.DomainID, sk)
	if err != nil {
		log.Printf("silinen yedek bağla (%s): %v", sk, err)
		httpx.WriteError(w, http.StatusInternalServerError, "bağlanamadı")
		return
	}
	n, _ := res.RowsAffected()
	uid, kul := middleware.Aktor(r)
	httpx.Denetim(h.DB, r, uid, kul, "silinen-yedek.bagla", alan, fmt.Sprintf("sk=%s baglanan=%d", sk, n), req.DomainID, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "baglanan": n})
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
	if err != nil || n < 0 || n > silinenEnCokGun {
		return nil // silinenSaklamaGunu da aralık dışını yok sayıp slota düşer
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
