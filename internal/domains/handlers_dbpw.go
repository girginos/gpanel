package domains

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"girginospanel/internal/gizli"
	"girginospanel/internal/hesaplar"
	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"

	"github.com/go-chi/chi/v5"
)

type setDBPwReq struct {
	Parola string `json:"parola"`
	// Kullanici: YALNIZ db_user bos oldugunda (kullanicisi olmayan veritabani)
	// zorunludur — o durumda bu ad ile kullanici olusturulur.
	Kullanici string `json:"kullanici"`
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
		req.Parola = hesaplar.RandomParola(24)
	}
	// 🔴 Kullanici, BASKA bir satirin ciphertext'ini parola olarak yazamaz:
	// aksi halde deger sonra cozulup okunarak sifre-cozme oracle'i olusuyordu.
	if gizli.SifreliMi(req.Parola) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz parola")
		return
	}
	if len(req.Parola) < 6 {
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
	// Rota seviyesinde {id} domain param'i yok → sahiplik BURADA dogrulanir.
	if !middleware.YonetimSahibi(r, domainID) {
		httpx.WriteError(w, http.StatusForbidden, "bu veritabanına erişiminiz yok")
		return
	}
	if isDemo == 1 {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin DB parolası değiştirilemez")
		return
	}

	islem := "db.parola"
	if strings.TrimSpace(dbUser) == "" {
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
		// Baska bir DB kaydinin kullanicisiyla CAKISMASIN (yetki sizmasi).
		var cakisma int
		if e := h.DB.QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM db_accounts WHERE db_user=? AND id<>?`, yeni, dbid).Scan(&cakisma); e != nil || cakisma > 0 {
			httpx.WriteError(w, http.StatusConflict, "bu kullanıcı adı başka bir veritabanında kullanılıyor")
			return
		}
		if err := hesaplar.MySQLKullaniciEkle(dbName, yeni, req.Parola); err != nil {
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
	} else if err := hesaplar.MySQLChangePassword(h.DB, dbUser, req.Parola); err != nil {
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
		"db_parola":    req.Parola,
	})
}

var reOptDBAdi = regexp.MustCompile("^[A-Za-z0-9_]+$")

// OptimizeDatabase: POST /api/v1/databases/:dbid/optimize
// DB nin TUM tablolarini optimize eder (InnoDB: recreate+analyze -> fragmentasyon
// giderir, alan geri kazanir). Sahiplik route middleware (DBSahipligi) + burada teyit.
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
	if !middleware.YonetimSahibi(r, domainID) {
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
	once := dbBoyutBayt(r.Context(), dbName)
	out, cerr := exec.CommandContext(r.Context(), "mysqlcheck", "--optimize", "--skip-write-binlog", dbName).CombinedOutput()
	if cerr != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "optimize: "+strings.TrimSpace(string(out)))
		return
	}
	sonra := dbBoyutBayt(r.Context(), dbName)
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
