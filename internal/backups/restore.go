package backups

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"

	"github.com/go-chi/chi/v5"
)

// yedekHazirla — geri yükleme/içerik uçlarının yerel dosya hazırlığı (gerekirse
// GENEL uzak hedeften lftp ile indirme). Testler sahteyle değiştirir: hata yolu
// ağa/lftp'ye çıkmadan sınanır.
var yedekHazirla = YerelDosyaHazirla

// errYedekDiskteYok — yerel kopya yok ve uzak hedef kapalı: kullanıcıya olduğu
// gibi söylenebilen tek hazırlık hatası (sunucuya dair ayrıntı taşımaz).
var errYedekDiskteYok = errors.New("yedek dosyası diskte bulunamadı")

// errYedekYalnizDrive — yerel dosya yok (Drive'a gönderilince silindi) ve başka kopya yok: yedek
// yalnız Google Drive'da. Otomatik uzaktan geri yükleme KAPSAM DIŞI; açık yönlendirme verilir.
var errYedekYalnizDrive = errors.New("yedek yalnız Google Drive'da")

type driveYalnizHata struct{ is int64 }

func (e *driveYalnizHata) Error() string {
	no := "<iş-no>"
	if e.is > 0 {
		no = strconv.FormatInt(e.is, 10)
	}
	return "Bu yedek yalnız Google Drive'da. Çözmek için `girginospanel-server yedek-coz --indir " + no +
		" --anahtar <anahtar-dosyası>` komutunu kullanın."
}

func (e *driveYalnizHata) Is(t error) bool { return t == errYedekYalnizDrive }

// driveYalnizIs — yerel dosya yok ama doğrulanmış (tamam) Drive kopyası var → iş no.
func driveYalnizIs(db *sql.DB, dosya string) (int64, bool) {
	var is int64
	err := db.QueryRow(`SELECT k.id FROM backups b
		JOIN backup_uzak_kopyalar k ON k.backup_id=b.id AND k.kaynak='hedef' AND k.durum='tamam'
		WHERE b.dosya=? ORDER BY k.id DESC LIMIT 1`, dosya).Scan(&is)
	return is, err == nil
}

// driveYalnizMesaji — Download/hazırlık için istemci metni (rol ayrımı hazirlikHatasi ile aynı).
func driveYalnizMesaji(r *http.Request, err error) string {
	if middleware.RolFrom(r) == "admin" {
		return err.Error()
	}
	return "Bu yedek yalnız Google Drive'da; geri yüklemek için yöneticinize başvurun."
}

// hazirlikHatasi — YerelDosyaHazirla hatasını istemciye dönecek metne çevirir.
//
// 🔴 KAPSAM (YK-02): hata, YÖNETİCİNİN sistem geneli uzak yedek hedefinden
// indirmenin HAM lftp çıktısını taşır ("uzaktan indirme: cd: Access failed:
// 550 ... (/admin-deposu/sunucu181/2026-09-24)", çözülemeyen host adı) ya da
// sunucunun boş disk miktarını. Bu uçlar MusteriScope ile bayiye ve müşteriye
// açık; ham metin kökün uzak deposunun host adını ve dizin şemasını sızdırıyordu.
// Delete'teki ilkeyle aynı: ayrıntı günlüğe ve YALNIZ yöneticiye.
func hazirlikHatasi(r *http.Request, domainID int64, err error) string {
	log.Printf("yedek: dosya hazırlanamadı (domain=%d): %v", domainID, err)
	if errors.Is(err, errYedekYalnizDrive) {
		return driveYalnizMesaji(r, err)
	}
	if middleware.RolFrom(r) == "admin" || errors.Is(err, errYedekDiskteYok) {
		return err.Error()
	}
	return "yedek dosyası hazırlanamadı (uzak yedek deposundan alınamadı) — ayrıntı sunucu günlüğüne yazıldı; sorun sürerse yöneticinize başvurun"
}

// restoreIstek: geri yükleme gövdesi (tüm alanlar opsiyonel; boş gövde = mod "tam").
type restoreIstek struct {
	Mod     string   `json:"mod"`      // tam | dosyalar | veritabani | dosya | db
	Temiz   bool     `json:"temiz"`    // mod=tam/dosyalar: rsync --delete (ESKI ezme davranışı)
	Yollar  []string `json:"yollar"`   // mod=dosya: arşiv-içi göreli yollar
	Hedef   string   `json:"hedef"`    // mod=dosya: "klasor" (varsayılan) | "yerinde"
	DB      string   `json:"db"`       // mod=db (zorunlu) / veritabani (opsiyonel filtre)
	HedefDB string   `json:"hedef_db"` // mod=db: "" = üzerine yaz, dolu = yeni DB adı
}

// Restore: POST /api/v1/domains/:id/backups/:bid/geriyukle
// Granüler geri yükleme: tam / yalnız dosyalar / yalnız DB / seçili dosyalar / tek DB.
// Varsayılanlar NON-DESTRUCTIVE: tam/dosyalar EZMEZ (temiz=false), dosya seçimi ayrı
// klasöre açar, DB seçimi yeni ada geri yükleyebilir.
func (h *Handlers) Restore(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	bid, _ := strconv.ParseInt(chi.URLParam(r, "bid"), 10, 64)

	var req restoreIstek
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) { // boş gövde tolere, bozuk reddet
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	req.Mod = strings.TrimSpace(req.Mod)
	if req.Mod == "" {
		req.Mod = "tam"
	}

	var sk, dosya, alanAdi string
	var isDemo int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT d.sistem_kullanici, d.alan_adi, d.is_demo, b.dosya FROM backups b
		 JOIN domains d ON d.id=b.domain_id
		 WHERE b.id=? AND b.domain_id=?`, bid, id).Scan(&sk, &alanAdi, &isDemo, &dosya)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "yedek bulunamadı")
		return
	}
	if err != nil {
		// 🔴 ham err.Error() musteri yanitina degil loga — DB ic detaylari sizmasin.
		log.Printf("restore: yedek kaydı okunamadı: %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, "yedek kaydı okunamadı")
		return
	}
	if isDemo == 1 {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğe geri yükleme yapılamaz")
		return
	}
	if !strings.HasPrefix(sk, "c_") {
		httpx.WriteError(w, http.StatusBadRequest, "güvenlik")
		return
	}

	// 🔴 TEK TIK. Onceki surum "indirme basladi, tekrar deneyin" donuyordu;
	// kullanici bir kez tikliyor, indirme arkada bitiyor ama GERI YUKLEME HIC
	// BASLAMIYORDU (uretimde yasandi: "yine olmadi"). Artik istek, gerekiyorsa
	// indirmeyi de kapsayacak sekilde is bitene kadar acik kalir; kullanici
	// bekleyisi bos degil: ilerleme /domains/{id}/backups/ilerleme ucundan
	// asama + yuzde olarak akar (nginx panel vhost'u 3600s okuma izni verir).
	if IlerlemeAktifMi(id) {
		httpx.WriteError(w, http.StatusTooManyRequests,
			"bu domain için zaten bir işlem sürüyor, lütfen bekleyin")
		return
	}
	IlerlemeBaslat(id, "geri", "hazırlanıyor", 0)
	defer func() {
		// Hata yollarinda da kayit kapansin (basarili bitis asagida ayrica yazilir).
		if IlerlemeAktifMi(id) {
			IlerlemeBitir(id, "", fmt.Errorf("geri yükleme tamamlanamadı"))
		}
	}()

	// Yedek uzak hedefteyse burada indirilir; yuzde KESIN (beklenen boyut bilinir).
	// 🔴 Uzak indirme 30 sn'yi aşınca tarayıcı kopuyor, r.Context() indirmeyi
	// kesiyordu → büyük uzak yedek HİÇ geri yüklenemiyordu. Geri yüklemenin geri
	// kalanı zaten bağlamsız; indirme de kopuk bağlamla (ilerleme /backups/ilerleme).
	kctx, iptal := httpx.KopukBaglam(r, 2*time.Hour)
	defer iptal()
	abs, err := yedekHazirla(kctx, h.DB, sk, dosya)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, hazirlikHatasi(r, id, err))
		return
	}
	IlerlemeAsama(id, "arşiv açılıyor", 0)

	// Kota-dostu sahneleme: SADECE moda göre gereken üyeleri ROOT olarak /var/tmp'e
	// çıkar (tenant home'unun tam kopyası tenant kotasını aşmadan). Güvenlik: üye
	// ön-taraması arsivUyeCikarRoot içinde (archivex.Tara → jail-escape reddi).
	uyeListesi, _ := arsivUyeListesi(abs)
	uyeler := cikarUyeleri(req.Mod, sk, uyeListesi, req.Yollar)
	if len(uyeler) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "bu mod için yedekte uygun içerik bulunamadı")
		return
	}
	tmpDir, _ := os.MkdirTemp("/var/tmp", "gosp-restore-*")
	defer os.RemoveAll(tmpDir)
	if out, err := arsivUyeCikarRoot(abs, tmpDir, uyeler); err != nil {
		msg := err.Error()
		if strings.TrimSpace(out) != "" {
			msg += ": " + strings.TrimSpace(out)
		}
		httpx.WriteError(w, http.StatusInternalServerError, "arşiv çıkarma: "+msg)
		return
	}

	sonuc := map[string]any{"ok": true, "mod": req.Mod, "alan_adi": alanAdi, "dosya": dosya}

	switch req.Mod {
	case "tam":
		IlerlemeAsama(id, "dosyalar geri yükleniyor", 0)
		homeGeriYukle(tmpDir, sk, req.Temiz)
		IlerlemeAsama(id, "veritabanı içe aktarılıyor", 0)
		dbSonuc := tumDBGeriYukle(h.DB, id, tmpDir, sk, "")
		sonuc["db"] = dbSonuc
		y, a, hn, m := DBSonucOzeti(dbSonuc)
		uyari := ezmeUyari(req.Temiz)
		// Dosyalar geri geldi; DB tarafi eksikse bunu SOYLE (sessizce "tamam" deme).
		if hn > 0 || (y == 0 && a > 0) {
			uyari = strings.TrimSpace(uyari + " ⚠ Dosyalar geri yüklendi ancak veritabanı tarafında sorun var: " + m)
		} else if y > 0 {
			uyari = strings.TrimSpace(uyari + fmt.Sprintf(" %d veritabanı geri yüklendi.", y))
		}
		sonuc["uyari"] = uyari

	case "dosyalar":
		homeGeriYukle(tmpDir, sk, req.Temiz)
		sonuc["uyari"] = ezmeUyari(req.Temiz)

	case "veritabani":
		IlerlemeAsama(id, "veritabanı içe aktarılıyor", 0)
		dbSonuc := tumDBGeriYukle(h.DB, id, tmpDir, sk, strings.TrimSpace(req.DB))
		sonuc["db"] = dbSonuc
		// 🔴 SIFIR DB geri yuklendiyse bu BASARI DEGILDIR. Toplu-is yolunda
		// duzeltilmisti; tekil (domain sayfasi) yolu hala "ok" donuyordu ve
		// kullanici hicbir sey olmadigi halde "Geri yukleme tamam" goruyordu.
		y, a, hn, m := DBSonucOzeti(dbSonuc)
		if hn > 0 {
			IlerlemeBitir(id, "", fmt.Errorf("%d veritabanı geri yüklenemedi — %s", hn, m))
			httpx.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("%d veritabanı geri yüklenemedi — %s", hn, m))
			return
		}
		if y == 0 {
			msg := "bu yedekte geri yüklenecek veritabanı YOK"
			if a > 0 {
				msg = "hiçbir veritabanı geri yüklenmedi — " + m
			}
			IlerlemeBitir(id, "", fmt.Errorf("%s", msg))
			httpx.WriteError(w, http.StatusBadRequest, msg)
			return
		}
		sonuc["uyari"] = fmt.Sprintf("%d veritabanı geri yüklendi — %s", y, m)

	case "dosya":
		if len(req.Yollar) == 0 {
			httpx.WriteError(w, http.StatusBadRequest, "geri yüklenecek dosya seçilmedi")
			return
		}
		n, klasor, err := secilenDosyalariGeriYukle(tmpDir, sk, req.Yollar, req.Hedef)
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		sonuc["dosya_sayisi"] = n
		if klasor != "" {
			sonuc["hedef_klasor"] = klasor
			sonuc["uyari"] = "Seçilen dosyalar ‘" + klasor + "/’ klasörüne açıldı — mevcut dosyalar korundu."
		} else {
			sonuc["uyari"] = "Seçilen dosyalar orijinal konumlarına yazıldı."
		}

	case "db":
		if strings.TrimSpace(req.DB) == "" {
			httpx.WriteError(w, http.StatusBadRequest, "veritabanı seçilmedi")
			return
		}
		msg, err := birDBGeriYukle(h.DB, id, tmpDir, sk, strings.TrimSpace(req.DB), strings.TrimSpace(req.HedefDB))
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		sonuc["db"] = msg

	default:
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz geri yükleme modu: "+req.Mod)
		return
	}

	uid, kul := middleware.Aktor(r)
	httpx.DenetimDomain(h.DB, r, uid, kul, "yedek.geriyukle",
		alanAdi, "mod="+req.Mod+" dosya="+dosya, id, true)

	// Ilerleme kaydini BASARIYLA kapat (defer yalniz hata yollarini yakalar).
	IlerlemeBitir(id, "Geri yükleme tamamlandı ("+req.Mod+")", nil)
	httpx.WriteJSON(w, http.StatusOK, sonuc)
}

func ezmeUyari(temiz bool) string {
	if temiz {
		return "Temiz geri yükleme: yedekte olmayan dosyalar SİLİNDİ, DB tabloları yeniden oluşturuldu."
	}
	return "Yedekteki dosyalar üzerine yazıldı; yedekte olmayan aktif dosyalar korundu."
}
