package backups

// uzak_baglanti.go — uzak kopya orkestratörünün (backups/uzak) panele bağlantısı:
// hedef kurma, yedek "tamam" kancası, işçinin başlatılması.
//
// Kanca tek noktadır (birDomainYedekle, yedek satırı yazıldıktan sonra) ve yedeği
// ASLA geciktirmez ya da bozmaz: kuyruğa tek INSERT, hata yalnız günlüğe, panic yutulur;
// ağır iş (şifreleme/yükleme) ayrı işçi goroutine'inde koşar.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"sync/atomic"
	"time"

	"girginospanel/internal/backups/hedef"
	"girginospanel/internal/backups/hedef/gdrive"
	"girginospanel/internal/backups/uzak"
	"girginospanel/internal/bildirim"
	"girginospanel/internal/surum"
)

// UzakGeciciDizin — şifreli geçici dosyalar (0700, panel sürecine ait).
const UzakGeciciDizin = "/var/lib/girginospanel/yedek-gecici"

// uzakHedefKur — test dikişi: tür → Hedef.
var uzakHedefKur = func(tur string, ayar json.RawMessage, sirlar map[string]string, kaydet func(string)) (hedef.Hedef, error) {
	if tur == gdrive.TurAdi {
		return driveHedefKur(ayar, sirlar, kaydet)
	}
	return nil, hedef.ErrBilinmeyenTur
}

// UzakHedefAcici — kimlik bilgilerini çözüp seçili kök hedefi kurar (ağ bağlantısı AÇMAZ).
func UzakHedefAcici(db *sql.DB) uzak.HedefAcici {
	return func(ctx context.Context, id int64) (hedef.Hedef, error) {
		s, err := hedefTara(db.QueryRowContext(ctx, hedefSecim+` AND id=?`, id))
		if err != nil {
			return nil, hedef.YeniHata(hedef.KategoriBulunamadi, "kur", errors.New("hedef kaydı yok"))
		}
		if !s.aktif {
			return nil, hedef.YeniHata(hedef.KategoriErisim, "kur", errors.New("hedef pasif"))
		}
		m, err := s.sirlariAc()
		if err != nil {
			return nil, hedef.YeniHata(hedef.KategoriKimlik, "kur", errors.New("hedef sırları çözülemedi"))
		}
		if s.tur == gdrive.TurAdi && m[gdrive.SirYenileme] == "" {
			return nil, hedef.YeniHata(hedef.KategoriKimlik, "kur", errors.New("hedef yetkilendirilmemiş"))
		}
		kaydet := func(yeni string) {
			m[gdrive.SirYenileme] = yeni
			if err := hedefSirlariYaz(context.Background(), db, s.id, m); err != nil {
				log.Printf("uzak kopya: yenilenen jeton saklanamadı (id=%d)", s.id)
			}
		}
		return uzakHedefKur(s.tur, s.ayar, m, kaydet)
	}
}

// uzakIsci — panelin tek işçisi (StartUzakKopya kurar; kanca ve uçlar kullanır).
var uzakIsci atomic.Pointer[uzak.Isci]

// UzakIsciKur — işçiyi yapılandırır (başlatmaz). CLI da bunu kullanır.
func UzakIsciKur(db *sql.DB) *uzak.Isci {
	i := &uzak.Isci{
		DB:          db,
		KokDizin:    BackupRoot,
		GeciciDizin: UzakGeciciDizin,
		HedefAc:     UzakHedefAcici(db),
		PanelSurum:  surum.Panel,
		Mesgul:      domainIslemde,
		Uyari: func(baslik, mesaj string, domainID, yedekID int64) {
			bildirim.Yaz(db, "kritik", "yedek", baslik, mesaj, domainID, "backup", yedekID)
		},
	}
	i.Hazirla()
	return i
}

// domainIslemde — alan adında yedek ya da geri yükleme sürüyor mu (yerel silme bu sırada ertelenir).
func domainIslemde(domainID int64) bool {
	if IlerlemeAktifMi(domainID) {
		return true
	}
	yedekMu.Lock()
	defer yedekMu.Unlock()
	_, var_ := yedekAktif[domainID]
	return var_
}

// StartUzakKopya — panel açılışında çağrılır; işçi goroutine'ini başlatır. Ayar
// KAPALI geldiği için kurulum yedek davranışını değiştirmez.
func StartUzakKopya(db *sql.DB) {
	i := UzakIsciKur(db)
	uzakIsci.Store(i)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("uzak kopya işçisi çöktü: %v", r)
			}
		}()
		i.Calistir(context.Background())
	}()
}

// uzakKopyaKuyrugaAl — yerel yedek "tamam" kancası. Kapalı/anahtarsız ise sessiz;
// diğer hatalar yalnız günlüğe. Yedek akışına ASLA hata ya da gecikme taşımaz.
func uzakKopyaKuyrugaAl(db *sql.DB, yedekID int64) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("uzak kopya kancası: %v", r)
		}
	}()
	ctx, iptal := context.WithTimeout(context.Background(), 10*time.Second)
	defer iptal()
	_, yeni, err := uzak.KuyrugaAl(ctx, db, yedekID)
	switch {
	case err == nil:
		if i := uzakIsci.Load(); i != nil && yeni {
			i.Tetikle()
		}
	case errors.Is(err, uzak.ErrKapali), errors.Is(err, uzak.ErrKokDegil):
	case errors.Is(err, uzak.ErrAnahtarYok):
		log.Printf("uzak kopya: etkin şifreleme anahtarı yok; yedek %d uzağa gönderilmeyecek", yedekID)
	default:
		log.Printf("uzak kopya: yedek %d kuyruğa alınamadı: %v", yedekID, err)
	}
}
