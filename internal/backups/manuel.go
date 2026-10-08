// Müşteri yedeği YALNIZ MANUEL; otomatik/zamanlanmış yedek yalnız kök (yönetici) tarafında.
//
// Kullanıcı kararı (2026-10-03): "Yedek sistemi manuel olsun otomatik olmasın.
// Root zaten yedekliyorsa kullanıcı kendi yedeklerini görüyor; o sürede kendisi
// yedek alacaksa manuel tetiklemeli. Günlük/aylık olmamalı — çok inode'lu hesapta
// disk IO'yu sömürür."
//
// Bu dosya üç şeyi taşır:
//  1. Plan yetkisi: zamanlama (backup-schedule PUT) YALNIZ gerçek yönetici
//     token'ıyla değişir. Müşteri, bayi ve müşteri-ADINA oturum (yönetici dahil)
//     403 alır — adına oturum müşterinin gözüyle çalışır, plan müşterinin işi değil.
//  2. Manuel yedek IO koruması: sunucu geneli eşzamanlılık sınırı (FIFO kuyruk),
//     hesap başına bekleme süresi, düşük öncelikli süreçler (ionice idle + nice 19).
//  3. Kaynak: liste satırının Sistem / Manuel rozeti için yedeğin kaynağı.
package backups

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"girginospanel/internal/middleware"
)

// ---- IO koruma parametreleri (sabit; değiştirmek için derleme gerekir) ----

const (
	// ManuelEszamanliSinir: sunucu genelinde AYNI ANDA çalışan manuel yedek sayısı.
	// Fazlası FIFO sırada bekler; müşteri sıra numarasını görür.
	ManuelEszamanliSinir = 1
	// ManuelKuyrukAzami: sırada bekleyebilecek en çok manuel yedek. Doluysa 503.
	ManuelKuyrukAzami = 50
	// ManuelBeklemeSuresi: aynı hesapta son BAŞARILI manuel yedekten sonra yeni
	// manuel yedek için beklenecek süre (gerçek yönetici muaf).
	ManuelBeklemeSuresi = 6 * time.Hour
)

// oncelikOnEki: manuel yedeğin harici süreçleri (mysqldump, tar, gzip, lftp) bu
// önekle çalışır: G/Ç sınıfı "idle" (yalnız disk boştayken hizmet alır) + en düşük
// CPU önceliği. Çocuk süreçler (bash -c altındaki mysqldump) ikisini de miras alır.
var oncelikOnEki = []string{"ionice", "-c3", "nice", "-n", "19"}

type dusukOncelikAnahtari struct{}

// dusukOncelikli: bu context ile başlatılan yedek süreçleri düşük öncelikle koşar.
func dusukOncelikli(ctx context.Context) context.Context {
	return context.WithValue(ctx, dusukOncelikAnahtari{}, true)
}

func dusukOncelikMi(ctx context.Context) bool {
	v, _ := ctx.Value(dusukOncelikAnahtari{}).(bool)
	return v
}

// komutBak: ionice yoksa (çok eski/minimal sistem) önek düşer; test değiştirir.
var komutBak = exec.LookPath

// komut: yedek yolundaki TÜM harici süreçler buradan oluşturulur. Context düşük
// öncelikliyse komut `ionice -c3 nice -n 19 <ad> <arg...>` olarak sarılır
// (ionice ve nice exec ile yerini verir: PID aynı kalır, context iptali yine
// asıl süreci öldürür). Kök yolları (zamanlayıcı, toplu iş) düz context geçer →
// davranışları DEĞİŞMEZ.
func komut(ctx context.Context, ad string, args ...string) *exec.Cmd {
	if dusukOncelikMi(ctx) {
		if _, err := komutBak(oncelikOnEki[0]); err == nil {
			tam := make([]string, 0, len(oncelikOnEki)+len(args))
			tam = append(tam, oncelikOnEki[1:]...)
			tam = append(tam, ad)
			tam = append(tam, args...)
			return exec.CommandContext(ctx, oncelikOnEki[0], tam...)
		}
	}
	return exec.CommandContext(ctx, ad, args...)
}

// ---- Sunucu geneli manuel yedek kuyruğu ----

// isKuyrugu: sınırlı eşzamanlılık + FIFO bekleme listesi (sıra numarası için).
type isKuyrugu struct {
	slot     chan struct{}
	azami    int
	mu       sync.Mutex
	bekleyen []int64
}

func yeniIsKuyrugu(sinir, azami int) *isKuyrugu {
	if sinir < 1 {
		sinir = 1
	}
	return &isKuyrugu{slot: make(chan struct{}, sinir), azami: azami}
}

var manuelKuyrugu = yeniIsKuyrugu(ManuelEszamanliSinir, ManuelKuyrukAzami)

// Ekle: domaini bekleme listesine yazar. Liste doluysa false.
func (k *isKuyrugu) Ekle(id int64) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	if len(k.bekleyen) >= k.azami {
		return false
	}
	k.bekleyen = append(k.bekleyen, id)
	return true
}

func (k *isKuyrugu) cikar(id int64) {
	k.mu.Lock()
	defer k.mu.Unlock()
	for i, v := range k.bekleyen {
		if v == id {
			k.bekleyen = append(k.bekleyen[:i], k.bekleyen[i+1:]...)
			return
		}
	}
}

// Sira: bekliyorsa 1'den başlayan sıra numarası, beklemiyorsa 0.
func (k *isKuyrugu) Sira(id int64) int {
	k.mu.Lock()
	defer k.mu.Unlock()
	for i, v := range k.bekleyen {
		if v == id {
			return i + 1
		}
	}
	return 0
}

// Bekle: slot açılana kadar bekler (Ekle'den sonra çağrılır). Slot alınınca
// domain bekleme listesinden çıkar; çağıran işi bitince Birak'ı çağırmalı.
func (k *isKuyrugu) Bekle(ctx context.Context, id int64) error {
	select {
	case k.slot <- struct{}{}:
		k.cikar(id)
		return nil
	case <-ctx.Done():
		k.cikar(id)
		return ctx.Err()
	}
}

func (k *isKuyrugu) Birak() { <-k.slot }

// Calisan: şu an slot tutan iş sayısı.
func (k *isKuyrugu) Calisan() int { return len(k.slot) }

// ---- Hesap başına bekleme süresi ----

// manuelSonBasariSn: bu domainin son BAŞARILI manuel yedeğinden bu yana geçen
// saniye. Manuel yedek = tip 'tam' ve job_id NULL (toplu kök işi job_id taşır,
// zamanlayıcı tip 'oto'). Kayıt yoksa -1. Başarısız yedek satır yazmaz, sayılmaz.
func manuelSonBasariSn(ctx context.Context, db *sql.DB, domainID int64) int64 {
	var sn sql.NullInt64
	err := db.QueryRowContext(ctx,
		`SELECT TIMESTAMPDIFF(SECOND, created_at, NOW()) FROM backups
		 WHERE domain_id=? AND tip='tam' AND job_id IS NULL
		 ORDER BY id DESC LIMIT 1`, domainID).Scan(&sn)
	if err != nil || !sn.Valid {
		return -1
	}
	return sn.Int64
}

// manuelBeklemeKalan: yeni manuel yedek için kalan bekleme (0 = serbest).
func manuelBeklemeKalan(ctx context.Context, db *sql.DB, domainID int64) time.Duration {
	gecen := manuelSonBasariSn(ctx, db, domainID)
	if gecen < 0 {
		return 0
	}
	kalan := ManuelBeklemeSuresi - time.Duration(gecen)*time.Second
	if kalan < 0 {
		return 0
	}
	return kalan
}

// sureMetni: "5 sa 12 dk" / "42 dk" / "1 dk".
func sureMetni(d time.Duration) string {
	dk := int((d + time.Minute - 1) / time.Minute)
	if dk < 1 {
		dk = 1
	}
	if dk >= 60 {
		return fmt.Sprintf("%d sa %d dk", dk/60, dk%60)
	}
	return fmt.Sprintf("%d dk", dk)
}

// ---- Rol: kim plan değiştirebilir, kim beklemeden muaf ----

// istekRolu: "admin" | "reseller" | "musteri" | "adina" (müşteri adına oturum,
// aktör yönetici olsa bile) | "" (kimliksiz).
func istekRolu(r *http.Request) string {
	if c := middleware.ClaimsFrom(r); c != nil {
		return c.Role
	}
	if mc := middleware.MusteriClaimsFrom(r); mc != nil {
		if mc.Adina() {
			return "adina"
		}
		return "musteri"
	}
	return ""
}

// rolCoz: testler kimlik zincirini atlamak için değiştirebilir.
var rolCoz = istekRolu

// planDuzenleyebilir: otomatik yedek planını YALNIZ gerçek yönetici değiştirir.
func planDuzenleyebilir(rol string) bool { return rol == "admin" }

// beklemedenMuaf: hesap başına bekleme süresinden yalnız gerçek yönetici muaf
// (kuyruk ve düşük öncelik yöneticinin manuel yedeğine de uygulanır).
func beklemedenMuaf(rol string) bool { return rol == "admin" }

// ---- Kaynak ----

// yedekKaynagi: liste rozeti. Zamanlayıcı ('oto'), eski bash zamanlayıcısı
// ('planli') ve kök toplu işi (job_id dolu) → "sistem"; silinmiş hesaptan bağlanan ('kurtarma') →
// "kurtarma"; geri kalan → "manuel".
func yedekKaynagi(tip string, jobID sql.NullInt64) string {
	// Silinmiş hesaptan domaine bağlanan yedek: ne sistem ne manuel (bkz. silinen.go S3).
	if tip == TipKurtarma {
		return "kurtarma"
	}
	if tip == "oto" || tip == "planli" || jobID.Valid {
		return "sistem"
	}
	return "manuel"
}

// ---- Dosya adı ----

// yedekDosyaAdi: YENİ yedeklerin adı `c_<hesap>-<YYYYMMDD>-<HHMMSS>.tar.gz`.
// Kaynak eki (-auto, eski bash'in -planli'si) ARTIK YOK: kaynak DB'de durur (tip +
// job_id → yedekKaynagi). Adı okuyan tek yer uzakTarihDizini'dir ve damgayı ad
// içinde ARAR (önek/sonekten bağımsız) — eski adlar da aynen çözülür.
// Aynı saniyede aynı ad doluysa (hesap kilidi bunu önler ama savunma) -2, -3 … eklenir.
func yedekDosyaAdi(dir, sk string, an time.Time) string {
	taban := fmt.Sprintf("%s-%s", sk, an.UTC().Format("20060102-150405"))
	ad := taban + ".tar.gz"
	for i := 2; i < 100 && dosyaVar(filepath.Join(dir, ad)); i++ {
		ad = fmt.Sprintf("%s-%d.tar.gz", taban, i)
	}
	return ad
}
