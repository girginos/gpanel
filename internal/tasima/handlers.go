package tasima

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/gizli"
	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
)

const logDizin = "/opt/girginospanel/logs"

// Ayni anda yalnizca TEK tasima isi calisir: rsync/mysqldump agir islerdir,
// paralel calisirsa sunucuyu bogar ve ayni sistem kullanicisi uzerinde yaris
// olusur.
// rezerveNobetci — is kaydi henuz acilmadan slotun tutuldugunu belirtir.
const rezerveNobetci int64 = -1

var (
	kilit      sync.Mutex
	aktifIsID  int64
	aktifIptal context.CancelFunc
)

func logYolu(isID int64) string {
	return filepath.Join(logDizin, fmt.Sprintf("tasima-%d.log", isID))
}

// ---------------------------------------------------------------------------
// Baglanti testi + kesif
// ---------------------------------------------------------------------------

type kaynakGirdi struct {
	Tip       string `json:"tip"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Kullanici string `json:"kullanici"`
	Parola    string `json:"parola"`
	Anahtar   string `json:"anahtar"`
	// PanelAnahtari: true ise panelin KENDI taşıma anahtarı kullanılır (parola/kullanıcı
	// anahtarı yerine). Kaynak sunucuya panelin public anahtarı eklenmiş olmalı. Kaynak
	// root parola-girişini kapatmış (prohibit-password) olsa bile çalışır.
	PanelAnahtari bool `json:"panel_anahtari"`
}

func (g kaynakGirdi) kaynak() *Kaynak {
	p := g.Port
	if p == 0 {
		p = 22
	}
	parola, anahtar := g.Parola, g.Anahtar
	if g.PanelAnahtari {
		if priv, _, err := panelTasimaAnahtar(); err == nil {
			anahtar = priv // panel özel anahtarı → key-auth (parola devre dışı)
			parola = ""
		}
	}
	return &Kaynak{
		Tip: strings.ToLower(strings.TrimSpace(g.Tip)), Host: g.Host, Port: p,
		Kullanici: g.Kullanici, Parola: parola, Anahtar: anahtar,
	}
}

// ── Panel taşıma SSH anahtarı (parola çalışmadığında otomatik yol) ──────────
const tasimaKeyDir = "/var/lib/girginospanel/tasima"
const tasimaKeyPath = tasimaKeyDir + "/id_ed25519"

var tasimaKeyMu sync.Mutex

// panelTasimaAnahtar — panelin taşıma ed25519 anahtar çiftini garanti eder (yoksa
// üretir) ve (özel-anahtar-içeriği, public-anahtar-satırı) döner. Kullanıcı public
// anahtarı kaynağa ekler; taşıma panelin özel anahtarıyla parolasız bağlanır.
func panelTasimaAnahtar() (priv string, pub string, err error) {
	tasimaKeyMu.Lock()
	defer tasimaKeyMu.Unlock()
	if err = os.MkdirAll(tasimaKeyDir, 0o700); err != nil {
		return "", "", err
	}
	if _, e := os.Stat(tasimaKeyPath); e != nil {
		out, ge := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-C", "girginospanel-tasima", "-f", tasimaKeyPath).CombinedOutput()
		if ge != nil {
			return "", "", fmt.Errorf("anahtar üretilemedi: %s", strings.TrimSpace(string(out)))
		}
	}
	pb, err := os.ReadFile(tasimaKeyPath)
	if err != nil {
		return "", "", err
	}
	pubB, err := os.ReadFile(tasimaKeyPath + ".pub")
	if err != nil {
		return "", "", err
	}
	return string(pb), strings.TrimSpace(string(pubB)), nil
}

// PanelAnahtar: GET /system/tasima/anahtar → {pubkey, komut}. Parola çalışmazsa
// (yanlış parola veya kaynak root parola-girişini kapatmış) kullanılır: kullanıcı
// tek-satır komutu kaynak sunucuda çalıştırır, sonra "Panel anahtarıyla" bağlanır.
func (h *Handlers) PanelAnahtar(w http.ResponseWriter, r *http.Request) {
	_, pub, err := panelTasimaAnahtar()
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "panel taşıma anahtarı hazırlanamadı: "+err.Error())
		return
	}
	// Kaynakta çalıştırılacak tek komut (idempotent): dizin + izin + anahtar ekle.
	komut := "mkdir -p ~/.ssh && chmod 700 ~/.ssh && grep -qxF '" + pub + "' ~/.ssh/authorized_keys 2>/dev/null || echo '" + pub + "' >> ~/.ssh/authorized_keys; chmod 600 ~/.ssh/authorized_keys"
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"pubkey": pub, "komut": komut})
}

// Test — POST /system/tasima/test
func (h *Handlers) Test(w http.ResponseWriter, r *http.Request) {
	var g kaynakGirdi
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&g); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz istek")
		return
	}
	k := g.kaynak()
	if err := k.Dogrula(); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	ad, err := k.BaglantiTesti(r.Context())
	if err != nil {
		httpx.WriteError(w, http.StatusBadGateway, "baglanti kurulamadi: "+err.Error())
		return
	}
	tespit, _ := k.PanelTespit(r.Context())
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "sunucu_adi": ad, "tespit_edilen": tespit,
		"uyusuyor": tespit == k.Tip,
	})
}

// Kesif — POST /system/tasima/kesif
func (h *Handlers) Kesif(w http.ResponseWriter, r *http.Request) {
	var g kaynakGirdi
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&g); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz istek")
		return
	}
	k := g.kaynak()
	if err := k.Dogrula(); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	hesaplar, err := k.Kesfet(r.Context())
	if err != nil {
		httpx.WriteError(w, http.StatusBadGateway, "kesif basarisiz: "+err.Error())
		return
	}
	// Hedefte zaten var mi bilgisini isaretle.
	type cikti struct {
		Hesap
		Mevcut bool `json:"mevcut"`
	}
	out := make([]cikti, 0, len(hesaplar))
	for _, hs := range hesaplar {
		var n int
		_ = h.DB.QueryRow(`SELECT COUNT(*) FROM domains WHERE alan_adi=?`, hs.AlanAdi).Scan(&n)
		out = append(out, cikti{Hesap: hs, Mevcut: n > 0})
	}
	// 🔴 Oturumu kalıcılaştır: kaynak bilgileri + keşif sonucu şifreli saklanır ki
	// sayfa yenilenince kullanıcı her şeyi yeniden girmesin (bkz. oturum.go).
	oturumID := h.oturumKaydet(g, hesaplar)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"hesaplar": out, "toplam": len(out), "oturum_id": oturumID})
}

// ---------------------------------------------------------------------------
// Is baslatma
// ---------------------------------------------------------------------------

type baslatGirdi struct {
	kaynakGirdi
	Mod      string  `json:"mod"` // tekil | toplu
	Ayarlar  Ayarlar `json:"ayarlar"`
	Secilen  []Hesap `json:"secilen"`   // frontend'in kesiften sectigi hesaplar
	OturumID int64   `json:"oturum_id"` // kaydedilmis oturum — parola yeniden girilmezse buradan cozulur
}

// Baslat — POST /system/tasima/baslat
func (h *Handlers) Baslat(w http.ResponseWriter, r *http.Request) {
	// Slotu ATOMIK rezerve et: kontrol ile atama arasinda kilit birakilirsa
	// (JSON cozme + sifreleme + DB insert suresi) iki istek birden gecip AYNI
	// hedefe iki rsync/DB-import calistirabilir — yikici.
	kilit.Lock()
	if aktifIsID != 0 {
		kilit.Unlock()
		httpx.WriteError(w, http.StatusConflict, "zaten calisan bir tasima isi var")
		return
	}
	aktifIsID = rezerveNobetci // rezerve edildi; is kaydi acilinca gercek id yazilir
	kilit.Unlock()

	// Hata ile donulen HER yolda rezervasyon birakilmali.
	rezerveAktif := true
	birak := func() {
		if !rezerveAktif {
			return
		}
		rezerveAktif = false
		kilit.Lock()
		if aktifIsID == rezerveNobetci {
			aktifIsID = 0
		}
		kilit.Unlock()
	}
	defer birak()

	var g baslatGirdi
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&g); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz istek")
		return
	}
	k := g.kaynak()
	// 🔴 KİMLİK YENİDEN KULLAN: kullanıcı parolayı yeniden girmediyse (sayfa
	// yenilendi → oturumdan geldi) saklı şifreli kimliği SUNUCU TARAFINDA çöz.
	// Parola tarayıcıya asla geri dönmedi; burada düz-metin yalnızca bu iş için.
	if k.Parola == "" && k.Anahtar == "" && g.OturumID > 0 {
		var host string
		var pSif, aSif sql.NullString
		if h.DB.QueryRow(
			`SELECT kaynak_host, kaynak_parola, kaynak_anahtar FROM tasima_isleri
			 WHERE id=? AND durum='oturum' AND (gecerlilik IS NULL OR gecerlilik > NOW())`,
			g.OturumID).Scan(&host, &pSif, &aSif) == nil {
			if pSif.Valid && pSif.String != "" {
				k.Parola = gizli.CozBagli(pSif.String, host)
			}
			if aSif.Valid && aSif.String != "" {
				k.Anahtar = gizli.CozBagli(aSif.String, host)
			}
		}
	}
	if err := k.Dogrula(); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(g.Secilen) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "tasinacak hesap secilmedi")
		return
	}
	if len(g.Secilen) > 500 {
		httpx.WriteError(w, http.StatusBadRequest, "tek seferde en fazla 500 site tasinabilir")
		return
	}
	mod := "toplu"
	if g.Mod == "tekil" || len(g.Secilen) == 1 {
		mod = "tekil"
	}
	// Gecerli hesaplari suz (uzaktan gelen veri — yeniden dogrula).
	var gecerli []Hesap
	for _, hs := range g.Secilen {
		hs.AlanAdi = strings.ToLower(strings.TrimSpace(hs.AlanAdi))
		if !reAlanAdi.MatchString(hs.AlanAdi) || !strings.Contains(hs.AlanAdi, ".") {
			continue
		}
		if hs.KaynakHesap != "" && !reHesap.MatchString(hs.KaynakHesap) {
			continue
		}
		gecerli = append(gecerli, hs)
	}
	if len(gecerli) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "gecerli hesap bulunamadi")
		return
	}

	// 🔴 SAHİPLİK TAŞIMA (senkron, domainlerden ÖNCE): kaynaktaki reseller/müşteri
	// hesaplarını GVM'de oluştur; login→hedef eşlemesini Ayarlar'a koy. Üretilen
	// reseller parolaları admin'e yanıtta bir kez raporlanır (Plesk parolaları
	// düz-metin alınamaz). İş kaydı açılmadan yapılır ki hata olursa temiz dönülür.
	var uretilenKimlikler []UretilenKimlik
	if g.Ayarlar.SahipleriTasi {
		sahipler, serr := k.KesfetSahipler(r.Context())
		if serr != nil {
			httpx.WriteError(w, http.StatusBadGateway, "sahip keşfi başarısız: "+serr.Error())
			return
		}
		esle, kimlikler, herr := sahipleriHazirla(r.Context(), h.DB, sahipler)
		if herr != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "sahip hesapları oluşturulamadı: "+herr.Error())
			return
		}
		g.Ayarlar.SahipEsle = esle
		uretilenKimlikler = kimlikler
	}

	ayarJSON, _ := json.Marshal(g.Ayarlar)
	aktorUID, aktor := middleware.Aktor(r)

	// Kimlik bilgileri at-rest sifreli (baglam = host, capraz kullanim engeli).
	parolaSifreli := ""
	if k.Parola != "" {
		parolaSifreli = gizli.SaklaBagli(k.Parola, k.Host)
	}
	anahtarSifreli := ""
	if k.Anahtar != "" {
		anahtarSifreli = gizli.SaklaBagli(k.Anahtar, k.Host)
	}

	res, err := h.DB.Exec(
		`INSERT INTO tasima_isleri (kaynak_tip, kaynak_host, kaynak_port, kaynak_kullanici,
		   kaynak_parola, kaynak_anahtar, tasima_modu, durum, toplam, ayarlar, baslatan, baslangic)
		 VALUES (?,?,?,?,?,?,?, 'calisiyor', ?, ?, ?, NOW())`,
		k.Tip, k.Host, k.Port, k.Kullanici, parolaSifreli, anahtarSifreli,
		mod, len(gecerli), string(ayarJSON), aktor)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "is kaydi olusturulamadi")
		return
	}
	isID, _ := res.LastInsertId()

	for _, hs := range gecerli {
		_, _ = h.DB.Exec(
			`INSERT INTO tasima_kalemleri (is_id, kaynak_hesap, alan_adi, durum)
			 VALUES (?,?,?, 'bekliyor')`, isID, hs.KaynakHesap, hs.AlanAdi)
	}

	httpx.Denetim(h.DB, r, aktorUID, aktor, "tasima_baslat",
		fmt.Sprintf("%s@%s", k.Tip, k.Host),
		fmt.Sprintf("%d site", len(gecerli)), 0, true)

	ctx, iptal := context.WithCancel(context.Background())
	kilit.Lock()
	aktifIsID, aktifIptal = isID, iptal
	kilit.Unlock()
	rezerveAktif = false // artik gercek is id tutuyor; defer birak() bosa dusmesin

	go h.calistir(ctx, isID, k, gecerli, g.Ayarlar)

	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"is_id": isID, "toplam": len(gecerli), "uretilen_kimlikler": uretilenKimlikler})
}

// calistir — is yurutucusu (goroutine).
func (h *Handlers) calistir(ctx context.Context, isID int64, k *Kaynak, hesaplar []Hesap, ay Ayarlar) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("tasima: panik (is=%d): %v", isID, rec)
			_, _ = h.DB.Exec(`UPDATE tasima_isleri SET durum='hata', hata=?, bitis=NOW() WHERE id=?`,
				fmt.Sprintf("beklenmeyen hata: %v", rec), isID)
		}
		kilit.Lock()
		aktifIsID, aktifIptal = 0, nil
		kilit.Unlock()
		// Kimlik bilgilerini DB'den temizle — is bitti, artik gerekmiyor.
		_, _ = h.DB.Exec(
			`UPDATE tasima_isleri SET kaynak_parola=NULL, kaynak_anahtar=NULL, kimlik_temiz=1 WHERE id=?`, isID)
		k.Parola, k.Anahtar = "", ""
	}()

	_ = os.MkdirAll(logDizin, 0o750)
	lf, err := os.OpenFile(logYolu(isID), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		log.Printf("tasima: log acilamadi: %v", err)
	}
	yaz := func(f string, a ...any) {
		satir := fmt.Sprintf("[%s] %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(f, a...))
		if lf != nil {
			_, _ = lf.WriteString(satir)
			_ = lf.Sync()
		}
	}
	if lf != nil {
		defer lf.Close()
	}

	yaz("Tasima basladi — kaynak %s (%s), %d site", k.Host, k.Tip, len(hesaplar))

	var tamam, hata int
	for i, hs := range hesaplar {
		select {
		case <-ctx.Done():
			yaz("IPTAL EDILDI — kalan %d site atlandi", len(hesaplar)-i)
			_, _ = h.DB.Exec(`UPDATE tasima_isleri SET durum='iptal', bitis=NOW() WHERE id=?`, isID)
			return
		default:
		}

		yaz("──────── [%d/%d] %s ────────", i+1, len(hesaplar), hs.AlanAdi)
		_, _ = h.DB.Exec(
			`UPDATE tasima_kalemleri SET durum='calisiyor', basladi=NOW() WHERE is_id=? AND alan_adi=?`,
			isID, hs.AlanAdi)

		sonuc, err := h.HesapAktar(ctx, k, hs, ay, yaz)
		if err != nil && ctx.Err() != nil {
			// Iptal son hesap sirasinda gelirse dongunun basindaki ctx kontrolune
			// hic donulmez; is 'hata' olarak ve BOS mesajla kapaniyordu.
			yaz("IPTAL EDILDI — %s yarida kesildi", hs.AlanAdi)
			_, _ = h.DB.Exec(
				`UPDATE tasima_kalemleri SET durum='atlandi', hata='kullanici iptal etti', bitti=NOW() WHERE is_id=? AND alan_adi=?`,
				isID, hs.AlanAdi)
			_, _ = h.DB.Exec(`UPDATE tasima_isleri SET durum='iptal', bitis=NOW() WHERE id=?`, isID)
			return
		}
		if err != nil {
			hata++
			yaz("HATA: %s → %v", hs.AlanAdi, err)
			_, _ = h.DB.Exec(
				`UPDATE tasima_kalemleri SET durum='hata', hata=?, bitti=NOW() WHERE is_id=? AND alan_adi=?`,
				kisalt(err.Error(), 500), isID, hs.AlanAdi)
		} else {
			tamam++
			uy := ""
			if len(sonuc.Uyarilar) > 0 {
				uy = " | uyari: " + strings.Join(sonuc.Uyarilar, "; ")
			}
			yaz("TAMAM: %s (%.1f MB, %d DB, %d DNS)%s", hs.AlanAdi,
				float64(sonuc.DosyaBayt)/(1024*1024), sonuc.DBSayisi, sonuc.DNSSayisi, uy)
			_, _ = h.DB.Exec(
				`UPDATE tasima_kalemleri SET durum='tamam', domain_id=?, dosya_bayt=?, db_sayisi=?,
				   dns_sayisi=?, hata=NULLIF(?,''), bitti=NOW() WHERE is_id=? AND alan_adi=?`,
				sonuc.DomainID, sonuc.DosyaBayt, sonuc.DBSayisi, sonuc.DNSSayisi,
				strings.Join(sonuc.Uyarilar, "; "), isID, hs.AlanAdi)
		}
		_, _ = h.DB.Exec(`UPDATE tasima_isleri SET tamamlanan=?, basarisiz=? WHERE id=?`, tamam, hata, isID)
	}

	sonDurum := "tamam"
	if hata > 0 && tamam == 0 {
		sonDurum = "hata"
	}
	yaz("Tasima bitti — %d basarili, %d basarisiz", tamam, hata)
	_, _ = h.DB.Exec(`UPDATE tasima_isleri SET durum=?, tamamlanan=?, basarisiz=?, bitis=NOW() WHERE id=?`,
		sonDurum, tamam, hata, isID)
}

// ---------------------------------------------------------------------------
// Durum / liste / log / iptal
// ---------------------------------------------------------------------------

// Durum — GET /system/tasima  (son isler + aktif is)
func (h *Handlers) Durum(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(
		`SELECT id, kaynak_tip, kaynak_host, tasima_modu, durum, toplam, tamamlanan, basarisiz,
		        COALESCE(hata,''), COALESCE(baslatan,''), baslangic, bitis, olusturma
		 FROM tasima_isleri WHERE durum <> 'oturum' ORDER BY id DESC LIMIT 25`)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "isler okunamadi")
		return
	}
	defer rows.Close()
	type is struct {
		ID         int64      `json:"id"`
		Tip        string     `json:"tip"`
		Host       string     `json:"host"`
		Mod        string     `json:"mod"`
		Durum      string     `json:"durum"`
		Toplam     int        `json:"toplam"`
		Tamamlanan int        `json:"tamamlanan"`
		Basarisiz  int        `json:"basarisiz"`
		Hata       string     `json:"hata"`
		Baslatan   string     `json:"baslatan"`
		Baslangic  *time.Time `json:"baslangic"`
		Bitis      *time.Time `json:"bitis"`
		Olusturma  time.Time  `json:"olusturma"`
	}
	out := []is{}
	for rows.Next() {
		var v is
		if err := rows.Scan(&v.ID, &v.Tip, &v.Host, &v.Mod, &v.Durum, &v.Toplam,
			&v.Tamamlanan, &v.Basarisiz, &v.Hata, &v.Baslatan, &v.Baslangic, &v.Bitis, &v.Olusturma); err != nil {
			continue
		}
		out = append(out, v)
	}
	kilit.Lock()
	aktif := aktifIsID
	kilit.Unlock()
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"isler": out, "aktif_is": aktif})
}

// Detay — GET /system/tasima/{id}
func (h *Handlers) Detay(w http.ResponseWriter, r *http.Request) {
	isID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || isID <= 0 {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz is")
		return
	}
	rows, err := h.DB.Query(
		`SELECT id, kaynak_hesap, alan_adi, durum, COALESCE(domain_id,0),
		        dosya_bayt, db_sayisi, dns_sayisi, COALESCE(hata,''), basladi, bitti
		 FROM tasima_kalemleri WHERE is_id=? ORDER BY id`, isID)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "kalemler okunamadi")
		return
	}
	defer rows.Close()
	type kalem struct {
		ID        int64      `json:"id"`
		Hesap     string     `json:"kaynak_hesap"`
		AlanAdi   string     `json:"alan_adi"`
		Durum     string     `json:"durum"`
		DomainID  int64      `json:"domain_id"`
		DosyaBayt int64      `json:"dosya_bayt"`
		DBSayisi  int        `json:"db_sayisi"`
		DNSSayisi int        `json:"dns_sayisi"`
		Hata      string     `json:"hata"`
		Basladi   *time.Time `json:"basladi"`
		Bitti     *time.Time `json:"bitti"`
	}
	out := []kalem{}
	for rows.Next() {
		var v kalem
		if err := rows.Scan(&v.ID, &v.Hesap, &v.AlanAdi, &v.Durum, &v.DomainID,
			&v.DosyaBayt, &v.DBSayisi, &v.DNSSayisi, &v.Hata, &v.Basladi, &v.Bitti); err != nil {
			continue
		}
		out = append(out, v)
	}
	var durum string
	var toplam, tamamlanan, basarisiz int
	_ = h.DB.QueryRow(`SELECT durum, toplam, tamamlanan, basarisiz FROM tasima_isleri WHERE id=?`, isID).
		Scan(&durum, &toplam, &tamamlanan, &basarisiz)
	kilit.Lock()
	calisiyor := aktifIsID == isID
	kilit.Unlock()
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"kalemler": out, "durum": durum, "toplam": toplam,
		"tamamlanan": tamamlanan, "basarisiz": basarisiz, "calisiyor": calisiyor,
	})
}

// Log — GET /system/tasima/{id}/log
func (h *Handlers) Log(w http.ResponseWriter, r *http.Request) {
	isID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || isID <= 0 {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz is")
		return
	}
	metin := dosyaSonu(logYolu(isID), 120<<10)
	kilit.Lock()
	calisiyor := aktifIsID == isID
	kilit.Unlock()
	var durum string
	_ = h.DB.QueryRow(`SELECT durum FROM tasima_isleri WHERE id=?`, isID).Scan(&durum)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"log": metin, "calisiyor": calisiyor, "durum": durum,
	})
}

// Iptal — POST /system/tasima/{id}/iptal
func (h *Handlers) Iptal(w http.ResponseWriter, r *http.Request) {
	isID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || isID <= 0 {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz is")
		return
	}
	kilit.Lock()
	eslesti := aktifIsID == isID && aktifIptal != nil
	if eslesti {
		aktifIptal()
	}
	kilit.Unlock()
	if !eslesti {
		httpx.WriteError(w, http.StatusNotFound, "bu is calismiyor")
		return
	}
	httpx.Denetim(h.DB, r, 0, aktorAd(r), "tasima_iptal",
		fmt.Sprintf("is=%d", isID), "", 0, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// AcilistaTemizle — main.go acilista cagirir.
func (h *Handlers) AcilistaTemizle() {
	res, err := h.DB.Exec(
		`UPDATE tasima_isleri SET durum='kesildi', hata='panel yeniden baslatildi', bitis=NOW()
		 WHERE durum IN ('calisiyor','kesif')`)
	if err != nil {
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("tasima: %d yarim kalan is 'kesildi' olarak isaretlendi", n)
		_, _ = h.DB.Exec(
			`UPDATE tasima_kalemleri SET durum='hata', hata='panel yeniden baslatildi', bitti=NOW()
			 WHERE durum='calisiyor'`)
	}
	// Guvenlik: yarim kalan islerin kimlik bilgilerini de temizle.
	_, _ = h.DB.Exec(
		`UPDATE tasima_isleri SET kaynak_parola=NULL, kaynak_anahtar=NULL, kimlik_temiz=1
		 WHERE durum IN ('tamam','hata','iptal','kesildi') AND kimlik_temiz=0`)
	// 🔴 Suresi dolan kaydedilmis oturumlari SIL (kimlik dahil). 'oturum' durumu
	// yeniden baslatmayi ATLATIR (yukaridaki UPDATE onu OLDURMEZ); yalniz TTL siler.
	_, _ = h.DB.Exec(`DELETE FROM tasima_isleri WHERE durum='oturum' AND gecerlilik IS NOT NULL AND gecerlilik < NOW()`)
}

// dosyaSonu — dosyanin son n baytini guvenli okur (symlink/ozel dosya reddi).
func dosyaSonu(yol string, n int64) string {
	f, err := os.OpenFile(yol, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return ""
	}
	boyut := st.Size()
	if boyut > n {
		if _, err := f.Seek(boyut-n, 0); err != nil {
			return ""
		}
	}
	buf := make([]byte, n)
	okunan, _ := f.Read(buf)
	if okunan <= 0 {
		return ""
	}
	return string(buf[:okunan])
}

// aktorAd — denetim kaydi icin kullanici adi.
func aktorAd(r *http.Request) string {
	_, ad := middleware.Aktor(r)
	return ad
}
