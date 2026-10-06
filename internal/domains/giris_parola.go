package domains

// giris_parola.go — BAYİ arayüzünden hosting hesabının MÜŞTERİ GİRİŞ (ana FTP)
// parolasını değiştirme: bayi "Domainler" listesindeki "Şifre değiştir" penceresi.
//
//	PUT /domains/{id}/giris-parola        gövde {"parola":"..."} (boş → sunucu üretir)
//
// Kapsam (rota: RequireRole("reseller"); sahiplik burada, DB'den):
//   - YALNIZ bayi, YALNIZ kendi sahipliğindeki domaine — başkasının domaini, kök sahipli
//     domain ve olmayan domain AYNI 404 ("domain bulunamadı": varlık sızmaz);
//   - kök (admin): 403 — kullanıcı kararı 2026-10-04; kök FTP/bağlantı parolasını domain
//     ayrıntısındaki uçlardan değiştirir (bu uç kök için kullanılmaz);
//   - müşteri / adına-oturum (müşteri jetonu) / API anahtarı / destek botu: ERİŞEMEZ
//     (rota katmanı + burada ikinci kilit; anahtar tablosunda da KimlikUclari).
//
// Etkileri: ana FTP parolası ($6$) değişir; SSH açıksa eşitlenir; müşterinin AÇIK
// OTURUMLARI düşer (ftp_accounts.token_gecersiz_ts); demo reddedilir; askıdaki hesapta
// çalışır (askı bozulmaz). Müşteriye bildirim GİTMEZ. DB/FTP-hesap parolalarına
// DOKUNULMAZ (bağımsız — kullanıcı kararı). Kullanıcı adı değiştirilemez.
//
// 🔴 Parola ASLA günlüğe/denetime/URL'e yazılmaz; üretildiyse yanıtta YALNIZ BİR KEZ döner.

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"

	"github.com/go-chi/chi/v5"
)

// Hesap başına hız sınırı: pencere içinde en çok girisParolaMaks yazma denemesi.
const (
	girisParolaMaks    = 6
	girisParolaPencere = 15 * time.Minute
)

var (
	girisParolaMu  sync.Mutex
	girisParolaLog = map[int64][]time.Time{}
	girisParolaNow = time.Now // testte sahte saat
)

// girisParolaIzin: hesap başına kayan pencere. (izinli, beklenecek süre)
func girisParolaIzin(domainID int64) (bool, time.Duration) {
	simdi := girisParolaNow()
	girisParolaMu.Lock()
	defer girisParolaMu.Unlock()
	var tut []time.Time
	for _, t := range girisParolaLog[domainID] {
		if simdi.Sub(t) < girisParolaPencere {
			tut = append(tut, t)
		}
	}
	if len(tut) >= girisParolaMaks {
		girisParolaLog[domainID] = tut
		return false, girisParolaPencere - simdi.Sub(tut[0])
	}
	girisParolaLog[domainID] = append(tut, simdi)
	return true, 0
}

// girisParolaSifirla: yalnız testler.
func girisParolaSifirla() {
	girisParolaMu.Lock()
	girisParolaLog = map[int64][]time.Time{}
	girisParolaMu.Unlock()
}

// GirisParola: PUT /domains/{id}/giris-parola
func (h *Handlers) GirisParola(w http.ResponseWriter, r *http.Request) {
	// İkinci kilit: yalnız BAYİ OTURUMU (kök de giremez). API anahtarı, destek botu, müşteri ve
	// adına-oturum (müşteri jetonu → ClaimsFrom nil) buraya giremez.
	c := middleware.ClaimsFrom(r)
	if c == nil || c.Role != "reseller" ||
		middleware.AnahtarFrom(r) != nil || middleware.DestekFrom(r) != nil {
		httpx.WriteError(w, http.StatusForbidden, "bu işlem için yetkiniz yok")
		return
	}
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var alan, sk string
	var isDemo, sshOn int
	var rid sql.NullInt64
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT alan_adi, sistem_kullanici, is_demo, COALESCE(ssh_erisim,0), reseller_id FROM domains WHERE id=?`, id).
		Scan(&alan, &sk, &isDemo, &sshOn, &rid)
	// 🔴 Sahiplik DB'den: istemci alanına güvenilmez. Başkasının domaini = yok.
	if errors.Is(err, sql.ErrNoRows) ||
		(err == nil && (!rid.Valid || c.ResellerID == 0 || rid.Int64 != c.ResellerID)) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "domain okunamadı")
		return
	}
	if isDemo == 1 {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin giriş parolası değiştirilemez")
		return
	}
	pw, uretildi, ok := apiParolaOku(w, r)
	if !ok {
		return
	}
	if izin, bekle := girisParolaIzin(id); !izin {
		w.Header().Set("Retry-After", strconv.Itoa(int(bekle.Seconds())+1))
		httpx.WriteError(w, http.StatusTooManyRequests, "bu hesap için çok fazla parola değişimi denendi, biraz sonra tekrar deneyin")
		return
	}
	uid, aktor := middleware.Aktor(r)
	const eylem = "ftp.giris_parola"
	// Önce açık oturumları düşür (yazım başarısız olsa bile güvenli yönde hata).
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE ftp_accounts SET token_gecersiz_ts=UNIX_TIMESTAMP()+1 WHERE domain_id=?`, id); err != nil {
		log.Printf("GÜVENLİK: giriş parolası domain=%d: müşteri oturumları düşürülemedi: %v", id, err)
		httpx.DenetimDomain(h.DB, r, uid, aktor, eylem, alan, "kullanici="+sk, id, false)
		httpx.WriteError(w, http.StatusInternalServerError, "müşteri oturumları sonlandırılamadı, parola değiştirilmedi")
		return
	}
	if err := apiFTPParolaYaz(h.DB, id, sk, pw); err != nil {
		log.Printf("giriş parolası: domain=%d: %v", id, err)
		httpx.DenetimDomain(h.DB, r, uid, aktor, eylem, alan, "kullanici="+sk, id, false)
		httpx.WriteError(w, http.StatusInternalServerError, "parola değiştirilemedi")
		return
	}
	// Müşterinin AÇIK FTP oturumları da düşer (hata parolayı bozmaz).
	h.ftpOturumlariDusur(r, uid, aktor, alan, sk, id)
	if sshOn == 1 {
		if err := apiSSHEsitle(h.DB, sk); err != nil {
			log.Printf("giriş parolası: SSH eşitleme domain=%d: %v", id, err)
		}
	}
	// Denetim: kim/hangi hosting; parola ve özeti ASLA yazılmaz.
	httpx.DenetimDomain(h.DB, r, uid, aktor, eylem, alan, "kullanici="+sk+" oturum_sonlandirildi=true", id, true)
	apiParolaYanit(w, map[string]any{"id": id, "alan_adi": alan, "kullanici": sk, "username": sk}, pw, uretildi)
}
