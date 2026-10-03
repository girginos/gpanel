package laravel

// Hosting HESABI askısı — Laravel tarafı (hesapaski.Uygula'nın bir adımı).
//
// Kuyruk işleyicisi (girginos-laravel-queue-<id>.service: User=<sk>,
// Restart=always) ve zamanlanmış görev (/etc/cron.d/girginos-laravel-<id>,
// kullanıcı <sk>) hesabın PHP kodunu PHP-FPM DIŞINDA çalıştırır. Askıda ikisi de
// kapanır, askıdan alınca kayıtlı ayara (cp_laravel_apps.queue_enabled /
// schedule_enabled — askı bu ayarı DEĞİŞTİRMEZ) göre geri gelir.
//
// 🔴 pkill tek başına yetmez: SuspendUserRuntime'ın `pkill -KILL -u` ile öldürdüğü
// işleyiciyi systemd RestartSec=5 sonra yeniden başlatır (Restart=always) ve
// systemd User= birimi PAM hesap denetiminden geçmez (hesap süresinin dolması
// onu durdurmaz). Restart'ı yenen tek yol birimi `disable --now` etmektir; bu,
// sunucu açılışında da başlamasını engeller. Askıdayken işleyiciyi / görevi AÇAN
// uçlar (Queue, Schedule) askiKapisi ile reddeder.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"girginospanel/internal/hesaplar"
	"girginospanel/internal/httpx"
)

// laravelCronAskiDizini — askıdaki hesabın cron.d dosyasının saklandığı kök-sahipli
// dizin (cron.d dışında: crond onu okumaz). İçerik bayt bayt korunur.
const laravelCronAskiDizini = "/var/lib/girginospanel/laravel-cron-askida"

// askiOrtam — askının dış etkileri. Üretimde uretimAskiOrtami(); testler geçici
// dizin + sahte komut verir (ana makinenin systemd'sine ve /etc'sine dokunulmaz).
type askiOrtam struct {
	komut      func(ad string, arg ...string) ([]byte, error)
	unitDizini string // systemd unit dizini (queueUnitPath ile aynı)
	cronDizini string // cron.d
	saklama    string // askıdaki cron.d dosyalarının dizini
}

func uretimAskiOrtami() askiOrtam {
	return askiOrtam{komut: sistemKomutu, unitDizini: queueUnitDizini, cronDizini: cronDizini, saklama: laravelCronAskiDizini}
}

func sistemKomutu(ad string, arg ...string) ([]byte, error) {
	return exec.Command(ad, arg...).CombinedOutput()
}

// HesapAski — hosting hesabının (domainID) Laravel kuyruk işleyicisini ve
// zamanlanmış görevini askıya alır (askida=true) ya da kayıtlı ayara göre geri
// getirir. İdempotenttir. Queue/Schedule uçlarıyla aynı alan adı kilidinde
// çalışır (kilitle): askı yazıldıktan sonra açılan uç askiKapisi'na takılır.
func HesapAski(ctx context.Context, db *sql.DB, domainID int64, askida bool) error {
	return uretimAskiOrtami().hesapAski(ctx, db, domainID, askida)
}

func (o askiOrtam) hesapAski(ctx context.Context, db *sql.DB, id int64, askida bool) error {
	defer kilitle(id)()
	var kuyruk, gorev int
	err := db.QueryRowContext(ctx,
		`SELECT COALESCE(queue_enabled,0), COALESCE(schedule_enabled,0) FROM cp_laravel_apps WHERE domain_id=?`, id).
		Scan(&kuyruk, &gorev)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("Laravel ayarı okunamadı: %w", err)
	}
	// Satır yoksa (Laravel kurulu değil) ayar kapalıdır; yine de disk artığına
	// bakılır: askıda birim/görev dosyası varsa kapatılır.
	unit := queueUnitName(id) + ".service"
	unitVar := dosyaVarMi(filepath.Join(o.unitDizini, unit))
	gorevAdi := "girginos-laravel-" + fmt.Sprint(id)
	etkin, sakli := filepath.Join(o.cronDizini, gorevAdi), filepath.Join(o.saklama, gorevAdi)

	var hatalar []error
	if askida {
		if unitVar {
			if out, err := o.komut("systemctl", "disable", "--now", unit); err != nil {
				hatalar = append(hatalar, fmt.Errorf("kuyruk işleyicisi durdurulamadı: %s: %w", strings.TrimSpace(string(out)), err))
			}
		}
		if dosyaVarMi(etkin) {
			if err := dosyaTasi(etkin, sakli, 0o600); err != nil {
				hatalar = append(hatalar, fmt.Errorf("zamanlanmış görev askıya alınamadı: %w", err))
			}
		}
		return errors.Join(hatalar...)
	}

	if kuyruk == 1 {
		if unitVar {
			// Askıda çökme döngüsüne girip StartLimitBurst'e takılmış olabilir.
			_, _ = o.komut("systemctl", "reset-failed", unit)
			if out, err := o.komut("systemctl", "enable", "--now", unit); err != nil {
				hatalar = append(hatalar, fmt.Errorf("kuyruk işleyicisi başlatılamadı: %s: %w", strings.TrimSpace(string(out)), err))
			}
		} else {
			// Askı birimi silmez; yokluğu askıdan bağımsız bir sapmadır — hesabı
			// yeniden askıya aldırmaz, Laravel sayfasından yeniden açılır.
			log.Printf("askıdan alma: domain %d kuyruk işleyicisi birimi yok — Laravel sayfasından yeniden etkinleştirilmeli", id)
		}
	}
	switch {
	case !dosyaVarMi(sakli):
	case gorev == 1 && !dosyaVarMi(etkin):
		if err := dosyaTasi(sakli, etkin, 0o644); err != nil {
			hatalar = append(hatalar, fmt.Errorf("zamanlanmış görev geri getirilemedi: %w", err))
		}
	default:
		// Ayar kapalı ya da askıdayken yeniden yazılmış (güncel olan etkin dosya):
		// saklanan kopya artıktır.
		if err := os.Remove(sakli); err != nil && !errors.Is(err, os.ErrNotExist) {
			hatalar = append(hatalar, fmt.Errorf("zamanlanmış görev artığı silinemedi: %w", err))
		}
	}
	return errors.Join(hatalar...)
}

// dosyaTasi — kaynak → hedef (içerik bayt bayt), sonra kaynak silinir. Rename
// değil: cron.d (/etc) ile saklama dizini (/var) ayrı dosya sistemlerinde olabilir.
func dosyaTasi(kaynak, hedef string, mod os.FileMode) error {
	b, err := os.ReadFile(kaynak)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(hedef), 0o700); err != nil {
		return err
	}
	gecici := hedef + ".yeni"
	if err := os.WriteFile(gecici, b, mod); err != nil {
		return err
	}
	if err := os.Chmod(gecici, mod); err != nil {
		_ = os.Remove(gecici)
		return err
	}
	if err := os.Rename(gecici, hedef); err != nil {
		_ = os.Remove(gecici)
		return err
	}
	return os.Remove(kaynak)
}

func dosyaVarMi(yol string) bool {
	_, err := os.Lstat(yol)
	return err == nil
}

// askiKapisi — hesap askıdaysa kuyruk işleyicisi / zamanlanmış görev AÇILMAZ
// (kapatmak serbesttir). Durum okunamazsa da açılmaz. false dönerse yanıt yazıldı.
func askiKapisi(w http.ResponseWriter, r *http.Request, db *sql.DB, id int64, sk string) bool {
	askida, err := hesaplar.HesapAskida(r.Context(), db, sk)
	if err != nil {
		log.Printf("laravel: domain %d: %v", id, err)
		httpx.WriteError(w, http.StatusInternalServerError, "hesap askı durumu okunamadı")
		return false
	}
	if askida {
		httpx.WriteError(w, http.StatusConflict, "hesap askıda — kuyruk işleyicisi ve zamanlanmış görev askıdan alınınca açılabilir")
		return false
	}
	return true
}
