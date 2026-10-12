package domains

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/gizli"
	"girginospanel/internal/hesaplar"
	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"

	"github.com/go-chi/chi/v5"
)

// MariaDB hesap islemleri (root soket) icin dikisler. Uretimde hesaplar
// fonksiyonlari; testler sahtesini koyar (canli MariaDB'ye dokunmaz).
var (
	dbKullaniciEkle  = hesaplar.MySQLKullaniciEkle
	dbParolaDegistir = hesaplar.MySQLChangePassword
	dbParolaEsitle   = hesaplar.MySQLKayitliParolaEsitle
	// dbOptimizeCalistir / dbBoyutFn: optimize ucunun root-soket adımları (testte sahte).
	dbOptimizeCalistir = func(ctx context.Context, dbName string) ([]byte, error) {
		return exec.CommandContext(ctx, "mysqlcheck", "--optimize", "--skip-write-binlog", dbName).CombinedOutput()
	}
	dbBoyutFn = dbBoyutBayt
	// dbSilSahipli: DB silme (DROP DATABASE + güvenliyse DROP USER) — testte sahte.
	dbSilSahipli = hesaplar.MySQLDropDBSahipli
)

// adKullanilamaz: "kullanicisi olmayan DB" dalinda ad reddi — TEK genel mesaj.
// Nedeni (sistem hesabi / baska kiracinin ad alani / baska kayitta / MariaDB'de
// var) ayirt EDILMEZ: aksi halde bayi baska kiracilar ve sunucu hesaplari
// hakkinda ad yoklamasi yapabilirdi.
const adKullanilamaz = "bu kullanıcı adı kullanılamaz (sistem hesabı, başka bir hostinge ait ya da zaten mevcut) — başka bir ad seçin"

type setDBPwReq struct {
	Parola string `json:"parola"`
	// Kullanici: YALNIZ db_user bos oldugunda (kullanicisi olmayan veritabani)
	// zorunludur — o durumda bu ad ile kullanici olusturulur.
	Kullanici string `json:"kullanici"`
	// YalnizKayit: MariaDB'ye DOKUNMA — yalniz panel kaydini (db_accounts) bu parolaya
	// esitle; parola MariaDB'de GERCEKTEN giris yapabiliyorsa. Parola panel disindan
	// degistiginde (kayit bayat → phpMyAdmin "Access denied") calisan siteyi kirmadan
	// duzeltmenin yolu; ALTER USER yolu sitenin config'teki parolasini gecersiz kilardi.
	YalnizKayit bool `json:"yalniz_kayit"`
}

// SetDatabasePassword: PUT /api/v1/databases/:dbid/password
// Body bos ise rastgele uretir. Demo abonelige reddeder.
func (h *Handlers) SetDatabasePassword(w http.ResponseWriter, r *http.Request) {
	dbid, _ := strconv.ParseInt(chi.URLParam(r, "dbid"), 10, 64)
	var req setDBPwReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	if req.Parola == "" {
		httpx.WriteError(w, http.StatusBadRequest, "yeni parola gerekli")
		return
	}
	// 🔴 Kullanici, BASKA bir satirin ciphertext'ini parola olarak yazamaz:
	// aksi halde deger sonra cozulup okunarak sifre-cozme oracle'i olusuyordu.
	if gizli.SifreliMi(req.Parola) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz parola")
		return
	}
	// Yalniz-kayit modunda parola YENI degil, MEVCUT (uygulamanin config'indeki) deger:
	// asgari uzunluk kurali ona uygulanmaz (MariaDB'de giris ile ayrica dogrulanir).
	if !req.YalnizKayit && len(req.Parola) < 6 {
		httpx.WriteError(w, http.StatusBadRequest, "parola en az 6 karakter olmalı")
		return
	}

	var dbName, dbUser string
	var isDemo int
	var domainID int64
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT db.db_name, db.db_user, d.is_demo, d.id
		 FROM db_accounts db JOIN domains d ON d.id=db.domain_id
		 WHERE db.id=?`, dbid).Scan(&dbName, &dbUser, &isDemo, &domainID)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "DB kaydı bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "okuma: "+err.Error())
		return
	}
	// Rota seviyesinde {id} domain param'i yok → sahiplik BURADA da dogrulanir
	// (rota: DBSahipligiMusteri). Kok/bayi kendi kapsaminda; MUSTERI yalniz KENDI
	// domaininin DB'sinde (musteri ekranindaki "Parola Sifirla" — eskiden 404).
	if !middleware.DomainSahibiMi(r, domainID) {
		httpx.WriteError(w, http.StatusForbidden, "bu veritabanına erişiminiz yok")
		return
	}
	if isDemo == 1 {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin DB parolası değiştirilemez")
		return
	}

	islem := "db.parola"
	if req.YalnizKayit {
		if strings.TrimSpace(dbUser) == "" {
			httpx.WriteError(w, http.StatusBadRequest, "bu veritabanının kullanıcısı yok — önce kullanıcı oluşturun")
			return
		}
		if err := dbParolaEsitle(r.Context(), h.DB, domainID, dbUser, req.Parola); err != nil {
			switch {
			case errors.Is(err, hesaplar.ErrDBParolaUyusmuyor):
				// Basarisiz deneme de denetime yazilir (parola deneme izi).
				uid, kul := middleware.Aktor(r)
				httpx.DenetimDomain(h.DB, r, uid, kul, "db.parola-kayit-esitle", dbName, "kullanici="+dbUser+" parola MariaDB'de reddedildi", domainID, false)
				httpx.WriteErrorKod(w, http.StatusConflict,
					"girilen parola MySQL'deki parolayla uyuşmuyor — uygulamanızın yapılandırma dosyasındaki (ör. XenForo src/config.php, WordPress wp-config.php) mevcut parolayı girin",
					"db_parola_uyusmuyor")
			case errors.Is(err, hesaplar.ErrSistemDBHesabi):
				httpx.WriteError(w, http.StatusForbidden, "sistem veritabanı hesabının kaydı panelden değiştirilemez")
			default:
				httpx.WriteError(w, http.StatusInternalServerError, "panel kaydı güncellenemedi: "+err.Error())
			}
			return
		}
		islem = "db.parola-kayit-esitle"
	} else if strings.TrimSpace(dbUser) == "" {
		// 🔴 KULLANICISI OLMAYAN VERITABANI. Bu durum, DB panelden silinip
		// yedekten geri yuklendiginde olusur: yedek yalniz sema+veri icerir,
		// MySQL kullanicisi ve GRANT'ler arsivde BULUNMAZ. Panel ise her DB'nin
		// bir kullanicisi oldugunu varsayiyordu; sonuc olarak veritabani geri
		// gelse bile site baglanamiyor ve panelde kullanici olusturma yolu YOKTU.
		yeni := strings.TrimSpace(req.Kullanici)
		if yeni == "" {
			httpx.WriteError(w, http.StatusBadRequest,
				"bu veritabanının kullanıcısı yok — oluşturmak için kullanıcı adı gönderin")
			return
		}
		if !hesaplar.GecerliDBKimlik(yeni) {
			httpx.WriteError(w, http.StatusBadRequest, "geçersiz kullanıcı adı")
			return
		}
		// 🔴 DEVRALMA KAPISI. Ad serbest (uygulamanin wp-config/.env'deki
		// DB_USER'i girilebilsin diye); bu yuzden adin bu hostinge "yeni" ve
		// "sahipsiz ya da kendi" oldugu AYRICA kanitlanir. Eskiden yalniz desen +
		// db_accounts cakismasi bakiliyordu; 'root', 'mysql', 'panel', 'pma',
		// 'pureftpd' gibi panelin kaydetmedigi SUNUCU hesaplari geciyor ve
		// asagidaki CREATE-OR-ALTER onlarin parolasini bayinin degerine cekiyordu.
		// (1) Sunucu geneli hesap adi ASLA.
		if hesaplar.SistemDBHesabiMi(yeni) {
			httpx.WriteError(w, http.StatusConflict, adKullanilamaz)
			return
		}
		// (2) Baska bir DB kaydinin kullanicisiyla CAKISMASIN (yetki sizmasi).
		var cakisma int
		if e := h.DB.QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM db_accounts WHERE db_user=? AND id<>?`, yeni, dbid).Scan(&cakisma); e != nil || cakisma > 0 {
			httpx.WriteError(w, http.StatusConflict, adKullanilamaz)
			return
		}
		// (3) Baska hostingin AD ALANI kapatilamaz: o kiracinin panelden sonra
		// acacagi <onek>_ek<id> / <onek>_db / c_<sk>_db kullanicisi bu kayda
		// baglanir, bayi de kendi kaydindan parolayi sifirlayip kurbanin DB'sine
		// erisirdi. Kural (en uzun kok; esit kokte tam-sk sahibi; bir domainin
		// kokunun KENDISI — kendi tam sk'si ya da c'siz koku dahil — hic kimsenin)
		// TEK yerde: hesaplar.DBAdiBaskaKiracininMi (CreateDatabase / uygulama /
		// geri yukleme ile ayni). shop-db.com (c_shop_db) bu dalda "c_shop_db" /
		// "shop_db" acarak shop.com'un tasima/saglama adini kilitleyemez.
		// Olcum hatasi = red (fail-closed).
		if baska, e := hesaplar.DBAdiBaskaKiracininMi(r.Context(), h.DB, domainID, yeni); e != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "ad alanı denetlenemedi")
			return
		} else if baska {
			httpx.WriteError(w, http.StatusConflict, adKullanilamaz)
			return
		}
		// (4) MariaDB'de zaten var olan hesap (herhangi host) devralinmaz —
		// MySQLKullaniciEkle YALNIZ yeni hesap olusturur (ErrDBKullaniciMevcut; yaris
		// penceresinde CREATE 1396 da ayni hata).
		if err := dbKullaniciEkle(dbName, yeni, req.Parola); err != nil {
			if errors.Is(err, hesaplar.ErrDBKimlikMevcut) || errors.Is(err, hesaplar.ErrSistemDBHesabi) {
				httpx.WriteError(w, http.StatusConflict, adKullanilamaz)
				return
			}
			httpx.WriteError(w, http.StatusInternalServerError, "kullanıcı oluşturulamadı: "+err.Error())
			return
		}
		if _, err := h.DB.ExecContext(r.Context(),
			`UPDATE db_accounts SET db_user=?, db_pass_plain=?, db_host='localhost' WHERE id=?`,
			yeni, gizli.SaklaBagli(req.Parola, yeni), dbid); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "kayıt güncellenemedi: "+err.Error())
			return
		}
		dbUser = yeni
		islem = "db.kullanici-olustur"
	} else if err := dbParolaDegistir(h.DB, domainID, dbUser, req.Parola); err != nil {
		if errors.Is(err, hesaplar.ErrSistemDBHesabi) {
			httpx.WriteError(w, http.StatusForbidden, "sistem veritabanı hesabının parolası panelden değiştirilemez")
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "parola değişimi: "+err.Error())
		return
	}
	uid, kul := middleware.Aktor(r)
	httpx.DenetimDomain(h.DB, r, uid, kul, islem, dbName, "kullanici="+dbUser, domainID, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"dbid":         dbid,
		"db_adi":       dbName,
		"db_kullanici": dbUser,
	})
}

var reOptDBAdi = regexp.MustCompile("^[A-Za-z0-9_]+$")

// Optimize agir bir IO isidir ve artik musteri panelinden de tetiklenir:
//   - ayni DB icin es zamanli ikinci istek 409 (ust uste yigilmaz);
//   - musteri oturumu (adina/SSO dahil) ayni DB'yi son basarili optimizeden
//     optMusteriAralik gecmeden yeniden calistiramaz (429). Kok/bayi beklemez.
var (
	optCalisan       sync.Map // dbid -> struct{}
	optSonMu         sync.Mutex
	optSon           = map[int64]time.Time{} // dbid -> son basarili optimize
	optMusteriAralik = 10 * time.Minute
	optSimdi         = time.Now
)

// OptimizeDatabase: POST /api/v1/databases/:dbid/optimize
// DB nin TUM tablolarini optimize eder (InnoDB: recreate+analyze -> fragmentasyon
// giderir, alan geri kazanir). Sahiplik route middleware (DBSahipligiMusteri) + burada
// teyit: kok/bayi kendi kapsaminda, musteri KENDI domaininin DB'sinde.
// ROOT mysqlcheck (panel user tenant DB gormez). Non-destructive.
func (h *Handlers) OptimizeDatabase(w http.ResponseWriter, r *http.Request) {
	dbid, _ := strconv.ParseInt(chi.URLParam(r, "dbid"), 10, 64)
	var dbName string
	var isDemo int
	var domainID int64
	err := h.DB.QueryRowContext(r.Context(),
		"SELECT db.db_name, d.is_demo, d.id FROM db_accounts db JOIN domains d ON d.id=db.domain_id WHERE db.id=?", dbid).
		Scan(&dbName, &isDemo, &domainID)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "DB kaydı bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "okuma: "+err.Error())
		return
	}
	if !middleware.DomainSahibiMi(r, domainID) {
		httpx.WriteError(w, http.StatusForbidden, "bu veritabanına erişiminiz yok")
		return
	}
	if isDemo == 1 {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin DB'si optimize edilemez")
		return
	}
	if !reOptDBAdi.MatchString(dbName) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz DB adı")
		return
	}
	if middleware.MusteriClaimsFrom(r) != nil {
		optSonMu.Lock()
		son, vardi := optSon[dbid]
		optSonMu.Unlock()
		if gecen := optSimdi().Sub(son); vardi && gecen < optMusteriAralik {
			dk := int((optMusteriAralik - gecen + time.Minute - 1) / time.Minute)
			httpx.WriteError(w, http.StatusTooManyRequests,
				fmt.Sprintf("bu veritabanı kısa süre önce optimize edildi — %d dakika sonra tekrar deneyin", dk))
			return
		}
	}
	if _, calisiyor := optCalisan.LoadOrStore(dbid, struct{}{}); calisiyor {
		httpx.WriteError(w, http.StatusConflict, "bu veritabanı şu anda optimize ediliyor — bitmesini bekleyin")
		return
	}
	defer optCalisan.Delete(dbid)
	once := dbBoyutFn(r.Context(), dbName)
	out, cerr := dbOptimizeCalistir(r.Context(), dbName)
	if cerr != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "optimize: "+strings.TrimSpace(string(out)))
		return
	}
	optSonMu.Lock()
	optSon[dbid] = optSimdi()
	optSonMu.Unlock()
	sonra := dbBoyutFn(r.Context(), dbName)
	kazanilan := once - sonra
	if kazanilan < 0 {
		kazanilan = 0
	}
	uid, kul := middleware.Aktor(r)
	httpx.DenetimDomain(h.DB, r, uid, kul, "db.optimize", dbName, "", domainID, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "db_adi": dbName, "once_bayt": once, "sonra_bayt": sonra, "kazanilan_bayt": kazanilan,
	})
}

// dbBoyutBayt — ROOT socket ile bir DB nin data+index boyutu (panel user goremez).
func dbBoyutBayt(ctx context.Context, dbName string) int64 {
	out, err := exec.CommandContext(ctx, "mysql", "-N", "-B", "-e",
		"SELECT COALESCE(SUM(data_length+index_length),0) FROM information_schema.tables WHERE table_schema='"+dbName+"'").Output()
	if err != nil {
		return 0
	}
	n, _ := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	return n
}
