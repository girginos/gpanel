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
	"path"
	"sort"
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

// ---- sk kapısı: bu yedek bu hostinge ait mi? ----
//
// 🔴 Arşivde domain_id YOKTUR. Kimlik = sk (üst-seviye "<sk>/" home dizini) + DB adları
// (__db__/<ad>.sql, __db__/manifest.json {home: sk}). sk alan adının İLK ETİKETİNDEN türer
// (provisioner.SlugFromDomain: girgin.com → c_girgin) ve çakışmada sayı eki alır
// (c_girgin2): girgin.com silinip girgin.net (başka müşteri) açılırsa AYNI c_girgin alınır;
// aynı alan adı yeniden açılırken c_girgin doluysa c_girgin2 alınır. Restore bu kimliği
// hostingin sk'sıyla KARŞILAŞTIRMIYORDU:
//   - mod=tam: hostingin sk'sına ait home üyesi arşivde yoksa cikarUyeleri yalnız __db__
//     döner, homeGeriYukle SESSİZCE hiçbir şey yapmaz ama yanıt "Yedekteki dosyalar üzerine
//     yazıldı" derdi (sessiz kısmi restore + yanlış başarı mesajı);
//   - mod=veritabani/db: arşivdeki başka sk'ya ait DB adları, DBAdiBaskaKiracininMi'den
//     geçerse "kurtarma yolu" ile bu hostinge kaydedilip import edilebilirdi (başka hesabın
//     verisi bu hesaba).
//
// Kapı TÜM modlarda ve HERHANGİ bir çıkarma/yazma/DB işleminden ÖNCE çalışır (arşiv çıkarılmaz).
// Tek hosting (Restore) ve toplu iş (geriYukleCekirdek) AYNI kapıyı kullanır.
const (
	// kodSKUyusmuyor: ön yüzün metin eşleştirmeden tanıdığı makine okunur hata kodu.
	kodSKUyusmuyor = "sk_uyusmuyor"
	// skGenelMesaj: yönetici OLMAYANA (bayi/müşteri) gösterilen TEK metin — başka kiracının
	// sk'sını (ve neden uyuşmadığını) sızdırmaz.
	skGenelMesaj = "Bu yedek bu hesaba ait değil — yöneticinize başvurun."
	// skDosyaYokMesaj: dosya geri yükleyen modlarda arşivde hiç home dizini yok.
	skDosyaYokMesaj = "yedekte bu hesabın dosyaları yok"
)

// skRet: arşivin kimliği yüzünden geri yüklemenin reddi (nil = kapı geçildi).
type skRet struct {
	Durum int    // 409 (uyuşmuyor/belirsiz) | 400 (yedekte bu hesabın dosyaları yok)
	Kod   string // makine okunur kod; yalnız 409'da (kodSKUyusmuyor)
	Neden string // kısa, sk İÇERMEYEN neden (denetim kaydı bayiye de görünür)
	Admin string // yönetici metni: iki sk'yı + çözüm ipucunu içerir
	Genel string // yönetici OLMAYAN metni: sk İÇERMEZ
}

// mesaj: çağıranın rolüne uygun metin. Yönetici olmayan başka kiracının sk'sını GÖRMEZ.
func (e *skRet) mesaj(admin bool) string {
	if admin {
		return e.Admin
	}
	return e.Genel
}

// arsivUstSKler: arşiv üyelerinin ÜST-SEVİYE adlarından sk biçimli (reSKGuvenli) olanlar —
// tekilleştirilmiş, sıralı. "__db__" ve sk biçimine uymayan (dosya/dizin) adlar sayılmaz.
// Üye adı path.Clean'den geçer: "./c_a/x" → c_a, "c_a/../c_b/x" etkin olarak c_b'dir.
func arsivUstSKler(uyeler []string) []string {
	goruldu := map[string]bool{}
	var out []string
	for _, m := range uyeler {
		ilk, _, _ := strings.Cut(path.Clean(m), "/")
		if !reSKGuvenli.MatchString(ilk) || goruldu[ilk] {
			continue
		}
		goruldu[ilk] = true
		out = append(out, ilk)
	}
	sort.Strings(out)
	return out
}

// skListe: mesajda gösterilecek sk listesi (crafted arşivde binlerce ad olabilir → kısaltılır).
func skListe(sklar []string) string {
	const en = 5
	if len(sklar) <= en {
		return strings.Join(sklar, ", ")
	}
	return strings.Join(sklar[:en], ", ") + fmt.Sprintf(" … (+%d)", len(sklar)-en)
}

// skUyusmuyor: arşiv başka bir hesaba ait.
func skUyusmuyor(arsivSK, hostSK string) *skRet {
	return &skRet{
		Durum: http.StatusConflict, Kod: kodSKUyusmuyor, Neden: "sk uyuşmuyor",
		Admin: fmt.Sprintf("Bu yedek farklı bir hesaba (%s) ait; bu hosting %s. Yedek yalnız aynı sistem kullanıcılı (sk) hostinge bağlanıp geri yüklenebilir — doğru hostingin yedek listesinden geri yükleyin.", arsivSK, hostSK),
		Genel: skGenelMesaj,
	}
}

// skKarar: kapının SAF karar mantığı (G/Ç yok → tablo testi).
//
//	sklar      : arşivin üst-seviye sk dizinleri (arsivUstSKler)
//	manifestSK : __db__/manifest.json "home" (yalnız sklar boşken ve DB modunda okunur; yoksa "")
func skKarar(mod, hostSK string, sklar []string, manifestSK string) *skRet {
	switch {
	case len(sklar) > 1:
		// Birden çok hesabın dizini: hangisine ait olduğu BELİRSİZ → reddet.
		return &skRet{
			Durum: http.StatusConflict, Kod: kodSKUyusmuyor, Neden: "birden çok hesabın dizini",
			Admin: fmt.Sprintf("Yedek birden çok hesabın dizinini içeriyor (%s); hangisine ait olduğu belirlenemediği için geri yüklenmedi (bu hosting: %s).", skListe(sklar), hostSK),
			Genel: skGenelMesaj,
		}
	case len(sklar) == 1 && sklar[0] != hostSK:
		return skUyusmuyor(sklar[0], hostSK)
	case len(sklar) == 1:
		return nil // eşleşti
	}
	// Arşivde hiç sk (c_*) home dizini yok.
	if manifestSK != "" && manifestSK != hostSK {
		return skUyusmuyor(manifestSK, hostSK)
	}
	switch mod {
	case "tam", "dosyalar", "dosya":
		// Dosya geri yükleyen modlar home üyesi OLMADAN anlamsız (eskiden sessiz kısmi restore).
		return &skRet{Durum: http.StatusBadRequest, Neden: "ev dizini yok", Admin: skDosyaYokMesaj, Genel: skDosyaYokMesaj}
	}
	return nil // DB-only modlar: legacy arşivler (kökte *.sql) eskisi gibi
}

// arsivSKKontrol: Restore (tek hosting) ve geriYukleCekirdek (toplu iş) için ORTAK kapı.
// uyeler: arsivUyeListesi çıktısı. nil = geçti; aksi halde çağıran HİÇBİR çıkarma/yazma/DB
// işlemi yapmadan reddi iletir.
func arsivSKKontrol(abs, mod, hostSK string, uyeler []string) *skRet {
	sklar := arsivUstSKler(uyeler)
	manifestSK := ""
	// Manifest YALNIZ kimliğin başka kaynağı yokken (sk dizini yok) ve kararı değiştirebileceği
	// DB modlarında okunur: tüm akışı bir kez daha gezer (manifest tar'ın sonundadır); ev
	// dizinli (olağan) arşivlerde ek maliyet YOK.
	if len(sklar) == 0 && (mod == "veritabani" || mod == "db") {
		manifestSK = arsivManifestHome(abs)
	}
	return skKarar(mod, hostSK, sklar, manifestSK)
}

// skReddet: kapının reddini yanıta yazar. Yönetici iki sk'yı görür; bayi/müşteri GENEL metni.
// 🔴 İlerleme kaydı (/backups/ilerleme) ve denetim kaydı başka roller tarafından da okunur:
// onlara yönetici metni (başka kiracının sk'sı) YAZILMAZ; ayrıntı yalnız sunucu günlüğünde.
func (h *Handlers) skReddet(w http.ResponseWriter, r *http.Request, ret *skRet, id int64, alanAdi, mod, dosya string) {
	log.Printf("restore: sk kapısı reddetti (domain=%d mod=%s dosya=%s): %s", id, mod, dosya, ret.Admin)
	if ret.Kod != "" { // güvenlik reddi (400 "dosya yok" sıradan bir doğrulama hatasıdır)
		uid, kul := middleware.Aktor(r)
		httpx.DenetimDomain(h.DB, r, uid, kul, "yedek.geriyukle", alanAdi,
			"mod="+mod+" dosya="+dosya+" reddedildi: "+ret.Neden, id, false)
	}
	IlerlemeBitir(id, "", errors.New(ret.Genel))
	msg := ret.mesaj(middleware.RolFrom(r) == "admin")
	if ret.Kod != "" {
		httpx.WriteJSON(w, ret.Durum, map[string]any{"ok": false, "kod": ret.Kod, "hata": msg})
		return
	}
	httpx.WriteError(w, ret.Durum, msg)
}

// homeGeriYukleYaniti: tam/dosyalar modunda dosyaları geri yükler; YAZILAMADIYSA yanıtı
// (ok:false) KENDİSİ yazar ve ok=false döner → çağıran hemen çıkar, başarı mesajı VERİLMEZ.
// ok=true iken uyari dosyalar için DÜRÜST metindir (temiz yapılamadıysa "SİLİNDİ" denmez).
// dbNotu: hata metnine eklenir (tam modda "veritabanına dokunulmadı": DB'ye geçilmez).
func homeGeriYukleYaniti(w http.ResponseWriter, id int64, tmpDir, sk string, temiz bool, dbNotu string) (string, bool) {
	err := homeGeriYukleSonuc(tmpDir, sk, temiz)
	switch {
	case err == nil:
		return ezmeUyari(temiz), true
	case errors.Is(err, errHomeTemizYapilamadi):
		return "Dosyalar geri yüklendi ancak TEMİZ geri yükleme uygulanamadı (rsync kullanılamadı): yedekte olmayan dosyalar SİLİNMEDİ, yedektekiler üzerine yazıldı.", true
	}
	log.Printf("restore: dosyalar geri yüklenemedi (domain=%d sk=%s): %v", id, sk, err)
	durum, msg := http.StatusBadRequest, skDosyaYokMesaj // sk kapısının ikinci katı: AYNI yanıt
	if !errors.Is(err, errHomeYedekteYok) {
		durum, msg = http.StatusInternalServerError, "dosyalar geri yüklenemedi: "+err.Error()
		if dbNotu != "" {
			msg += " — " + dbNotu
		}
	}
	IlerlemeBitir(id, "", errors.New(msg))
	httpx.WriteJSON(w, durum, map[string]any{"ok": false, "hata": msg})
	return "", false
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
	uyeListesi, listeErr := arsivUyeListesi(abs)
	if listeErr != nil {
		// Açılamayan/gzip olmayan arşivde "yedekte bu hesabın dosyaları yok" demek yanlış olur.
		log.Printf("restore: arşiv üye listesi okunamadı (domain=%d): %v", id, listeErr)
		httpx.WriteError(w, http.StatusInternalServerError, "yedek arşivi okunamadı")
		return
	}
	// 🔴 sk KAPISI: yedek bu hostinge mi ait? TÜM modlarda, HERHANGİ bir çıkarma/yazma/DB
	// işleminden ÖNCE (arşiv çıkarılmaz; ayrıntı: yukarıdaki "sk kapısı" bloğu).
	if ret := arsivSKKontrol(abs, req.Mod, sk, uyeListesi); ret != nil {
		h.skReddet(w, r, ret, id, alanAdi, req.Mod, dosya)
		return
	}
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
		// 🔴 Dosyalar yazılamadıysa DB'ye GEÇİLMEZ ve başarı mesajı verilmez (homeGeriYukleYaniti).
		dosyaUyari, ok := homeGeriYukleYaniti(w, id, tmpDir, sk, req.Temiz, "veritabanına dokunulmadı")
		if !ok {
			return
		}
		IlerlemeAsama(id, "veritabanı içe aktarılıyor", 0)
		dbSonuc := tumDBGeriYukle(h.DB, id, tmpDir, sk, "")
		sonuc["db"] = dbSonuc
		y, a, hn, m := DBSonucOzeti(dbSonuc)
		uyari := dosyaUyari
		// Dosyalar geri geldi; DB tarafi eksikse bunu SOYLE (sessizce "tamam" deme).
		if hn > 0 || (y == 0 && a > 0) {
			uyari = strings.TrimSpace(uyari + " ⚠ Dosyalar geri yüklendi ancak veritabanı tarafında sorun var: " + m)
		} else if y > 0 {
			uyari = strings.TrimSpace(uyari + fmt.Sprintf(" %d veritabanı geri yüklendi.", y))
		}
		sonuc["uyari"] = uyari

	case "dosyalar":
		uyari, ok := homeGeriYukleYaniti(w, id, tmpDir, sk, req.Temiz, "")
		if !ok {
			return
		}
		sonuc["uyari"] = uyari

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
		// 🔴 Hiçbir öğe kopyalanamadıysa (ör. kota dolu, kiracı komutu patladı) "klasörüne açıldı"
		// demek yalan olur: başarı mesajı verilmez.
		if n == 0 {
			msg := "seçilen dosyalar geri yüklenemedi (hiçbiri kopyalanamadı)"
			IlerlemeBitir(id, "", errors.New(msg))
			httpx.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "hata": msg})
			return
		}
		sonuc["dosya_sayisi"] = n
		uyari := "Seçilen dosyalar orijinal konumlarına yazıldı."
		if klasor != "" {
			sonuc["hedef_klasor"] = klasor
			uyari = "Seçilen dosyalar ‘" + klasor + "/’ klasörüne açıldı — mevcut dosyalar korundu."
		}
		if n < len(uyeler) { // kısmi: bazı öğeler kopyalanamadı — eksik yazıldığı söylenir
			uyari += fmt.Sprintf(" ⚠ %d seçimden yalnız %d tanesi geri yüklenebildi.", len(uyeler), n)
		}
		sonuc["uyari"] = uyari

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
