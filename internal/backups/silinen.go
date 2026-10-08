package backups

// Silinen hosting/bayi yedekleri ("yetim yedek").
//
// Karar (kullanıcı, 2026-10-08): hosting/bayi silme YEDEKLERDEN BAĞIMSIZDIR. Silme yolu yedek
// dizinine/uzak kopyalara hiç dokunmaz ve yedekler yüzünden ASLA başarısız olmaz/beklemez.
//
// Tasarım: backups.domain_id → domains(id) FK'si KALKTI (migrations/0217): domain silinince yedeğin
// DB satırı yerinde kalır, YEDEK SAYFASINDAN geri yüklenebilir. Silme `DELETE FROM domains`
// başarılı olunca TEK best-effort UPDATE ile satırlara alan_adi / sistem_kullanici / reseller_id /
// iz_id / silinme / son_kullanma yazar ve satırı domain_id=0'a taşır (SilinenIz.Yaz). Süre
// cp_ayarlar.silinen_yedek_gun: boş = hostingin yedek slotu (en az 7 gün), 0 = otomatik silme yok,
// N = sabit gün (≤3650).
//
//	Yetim yedek = domain_id=0 AND silinme IS NOT NULL
//
// 🔴 Y1 — YETİM YEDEK HİÇBİR İSTEKLE KENDİLİĞİNDEN BAĞLANMAZ. GET /domains/{id}/backups yan etkisizdir.
// Yetimi domaine bağlayan YALNIZ iki yol vardır: (a) yönetici ucu SilinenBagla, (b) POST /domains'e
// açıkça verilen `yedekleri_devral:true` bayrağı (YetimleriDevral; yalnız yönetici/bayi/API anahtarı
// çağırabilir, denetimli). Eşleşme alan adı (küçük harf) + sahip (reseller_id) + sk'dır. Nedeni:
// alan adını kimi akışlarda MÜŞTERİ belirler; silinmiş bir adı başka biri açınca eski müşterinin
// yedekleri (ve FTP/SFTP hedefi) otomatik ona geçmemeli. Uzak hedef anlık görüntüsü domaine HİÇBİR YOLLA geri
// yüklenmez (ne devralmada ne zorla bağlamada): yalnız yetimlerin UZAK SİLMESİ içindir, yetimler bağlanınca/bitince
// anlık görüntü (parola dahil) silinir. Aksi halde eski müşterinin host/kullanıcı bilgisi yenisine geçer ve geri
// yüklenen (pasif, çoğu zaman ulaşılamayan) hedef domainin KENDİ yedeklerinin budamasını kilitlerdi.
//
// 🔴 O4 — SİLME İZİ KİMLİĞİ (iz_id): her silme, benzersiz rastgele bir iz_id (16 bayt crypto/rand, 32 hex)
// üretir; yetim satırlar ve uzak hedef anlık görüntüsü bu kimlikle bağlanır. domain_id / bayi kimlikleri
// yeniden kullanılsa (AUTO_INCREMENT geri sarılması, yedekten dönüş) bile iki silme asla karışmaz.
// eski_domain_id yalnız BİLGİDİR (anahtar değil). Bayi silinince yetimlerin reseller_id'si 0'a çekilir
// (internal/reseller): yeni bir bayi aynı kimliği alırsa eskinin yetimlerini yakalayamaz.
//
// 🔴 S4 — domain_id YENİDEN KULLANIM KORUMASI: yetim satır eski domain_id'yi TAŞIMAZ, `eski_domain_id`
// kolonunda saklar; domain_id=0 olur (domains'te 0 id'li satır olamaz). Böylece domain id'si bir gün
// yeniden kullanılsa bile `domain_id=?` ya da `JOIN domains` yapan HİÇBİR canlı sorgu (yedek listesi,
// restore, budama, uzak gönderim, kota) eski müşterinin yedeğini görmez/silmez/geri yüklemez.
//
// 🔴 S1 — UZAK HEDEF ANLIK GÖRÜNTÜSÜ: domaine özel FTP/SFTP hedefi domain silinince CASCADE ile gider
// (fk_bdest_domain). Silme anında hedefin ŞİFRELİ kaydı backup_yetim_hedefler'e (iz_id UNIQUE) kopyalanır
// (anahtar varken SaklaGecis(CozBagli(..)) ile at-rest şifreli); yetim yedeğin uzak kopyası bu anlık
// görüntüyle silinir. Bir iz_id'nin TÜM yetimleri bitince (silindi/bağlandı) satır silinir, parola tabloda kalmaz.
//
// 🔴 S2 — SAHİPSİZ İŞARETLEME: silme izi yazılamazsa (UPDATE hatası/süreç çöktü) satır `domain_id>0,
// silinme NULL, domain yok` kalır. SilinenSahipsizIsaretle (panel açılışı + 04:00 turu) bunları dosya
// adından sk türeterek işaretler ve normal yaşam döngüsüne sokar. Toplu işaretleme eşiklidir (D6):
// `domains` tablosu eksik/hatalı bir DB'de TÜM yedekler sahipsiz görünür ve ≥7 gün sonra silinirdi.
//
// 🔴 S3 — BAĞLANAN YEDEK 'kurtarma' TİPİNE GEÇER: pruneOld yalnız tip='oto', pruneManuelYedek yalnız
// tip='tam' siler; 'kurtarma' ikisinin de dışındadır → eski yedekler domainin yeni saklama sayımına
// girip budanmaz. Otomatik süre dolumu YOKTUR (bilinçli); kullanıcı elle siler. 'kurtarma' ayrıca uzak
// gönderim adayı DEĞİLDİR (bkz. uzak.KokYedegiMi): eski yedeği kullanıcı istemeden uzağa itmek ve
// ardından yerel kopyayı silmek (yerel-silme) beklenmedik olurdu.
//
// 🔴 O2 — YARIŞ: yetim satırı silen HER yol (süpürme, DELETE /admin/silinen-yedekler/{sk}) uzak silmeden SONRA tek
// işlemde `DELETE ... WHERE id=? AND domain_id=0 AND silinme IS NOT NULL` yapar; yalnız RowsAffected()==1 ise
// dosyayı siler, dosya silinemezse (yok hariç) işlemi geri alır ve satırı KORUR. Uzak silme dakikalar sürebilir ve
// DELETE'ten ÖNCE koşar: araya giren bir bağlama canlı yedeğin satırını/dosyasını korusa bile UZAK kopyası silinmiş
// olurdu (yerel kopya "uzağa taşındı" diye silinmişse tek kopya). Bu yüzden uzak silmesi süren satırlar süreç içi
// bir kayıt defterinde tutulur (yetimSilinmekte): silme önce satırı KİLİTLİ okumayla doğrular (süren bir bağlamanın
// commit'ini bekler; bağlandıysa hiçbir şey silinmez), sonra uzak adıma geçer; bağlama (yetimleriBaglaBir) satırlarını
// kilitledikten sonra defteri okur ve silinmekte olanları BAĞLAMAZ. İki sıralama da kapalıdır: ya bağlama önce gelir
// ve uzak kopyaya hiç dokunulmaz, ya silme önce gelir ve o satır bağlanmaz. (Uzun InnoDB satır kilidi tutmak yerine
// uygulama düzeyinde dışlama: bağlama bekletilmez, birincil/ikincil indeks kilit sırası ters kilitlenmesi oluşmaz.
// Panel tek süreçtir: süpürme goroutine'i ve HTTP uçları aynı süreçte koşar.) Uzak adım satır başına
// yetimUzakSureSiniri ile sınırlıdır.
//
// 🔴 Yetim satırlar HİÇBİR canlı akışa girmemeli ve kendi başına silinmemeli. Yetimi yalnız şunlar
// siler: süpürme (süresi dolanı; 04:00 turu, süre bütçesi bitene dek parti parti), yönetici DELETE ucu.

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/backups/hedef"
	"girginospanel/internal/gizli"
	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"
)

// yedekKok: saklama işlemlerinin kökü (testte geçici dizine çevrilir).
var yedekKok = BackupRoot

const (
	silinenAyarKey  = "silinen_yedek_gun"
	silinenEnAzGun  = 7
	silinenEnCokGun = 3650
	// silinenSupurLimit: süpürmenin TEK partide çektiği yetim sayısı. Süpürme süre bütçesi (bağlam) bitene ya da
	// süresi dolan kayıt kalmayana dek parti parti döner (O7): "günde 200" sınırı toplu bayi silmesinden sonra
	// birikimi aylara yayıp depo tavanını (max_depo_gb) besliyordu.
	silinenSupurLimit = 200
	silinenGrupLimit  = 500
	silinenSatirLimit = 20000
	// sahipsizLimit: S2'nin tek partide çektiği sahipsiz satır sayısı (imleçle sayfalanır, süre bütçesi bitene dek).
	sahipsizLimit = 5000
	// sahipsizEsikAdet: bundan çok sahipsiz adayın TOPLU işaretlenmesi şüphelidir (D6).
	sahipsizEsikAdet = 100
	// TipKurtarma: yetimken domaine bağlanan yedeklerin tipi (pruneOld/pruneManuelYedek dokunmaz).
	TipKurtarma = "kurtarma"
)

// izZamanAsimi: iz hazırlama ve iz yazma için AYRI AYRI üst süre. Silme isteği backups tablosundaki bir kilitte bu kadar
// bekleyebilir; iz best-effort'tur (yazılamayan iz sahipsiz işaretleme turuyla tamamlanır) → kısa tutulur.
const izZamanAsimi = 3 * time.Second

// kalintiZamanAsimi: YetimKalintilariniAyir'ın üst süresi. Normalde tek indeksli SELECT'tir (kalıntı yok); kalıntı varsa
// (nadir çift arıza) satır satır işaretlenir ve eksik kalan satırlar yeni müşteriye GÖRÜNÜR kalır (S2 artık
// "domain yok" koşuluna uymayan bu satırlara bakmaz) → iz bütçesinden (izZamanAsimi) çok daha cömert.
const kalintiZamanAsimi = 30 * time.Second

// yetimUzakSureSiniri: TEK yetim satırın uzak silme adımının (anlık görüntü hedefi + genel hedef; her biri sil + doğrula
// lftp oturumları) üst süresi. Ölü/kara delik hedef satır başına dakikalarca sürebilir ve bu sürede satır kilidi
// tutulur (O2); aşılırsa adım hata olur (satır korunur, sonraki turda yeniden denenir).
var yetimUzakSureSiniri = 2 * time.Minute // testte kısaltılır

// yetimHedefParolaSutun: backup_yetim_hedefler.parola VARCHAR genişliği (backup_destinations.parola ile aynı).
const yetimHedefParolaSutun = 255

// izDeneme: iz UPDATE'inin en çok deneme sayısı (geçici kilit/kilitlenme hatası için; bağlam süresiyle sınırlı).
const izDeneme = 3

// baglaDeneme / baglaBekleme: bağlama işleminin kilitlenme (1213) / kilit bekleme zaman aşımı (1205) hatalarında
// SINIRLI yeniden deneme sayısı ve deneme başına bekleme tabanı.
const baglaDeneme = 4

var baglaBekleme = 60 * time.Millisecond

// yetimKosulu: yetim yedek SQL koşulu (backups tablosu "backups" adıyla, takma adsız).
// domain_id=0: domains'te 0 id'li satır olamaz → her JOIN domains yetimi doğal olarak eler.
const yetimKosulu = `domain_id=0 AND silinme IS NOT NULL`

// izYenidenDenemeBekleme: iz UPDATE'i hata verirse tek yeniden deneme öncesi bekleme (testte kısaltılır).
var izYenidenDenemeBekleme = 750 * time.Millisecond

// yetimUzakSil: yetim yedeğin uzak kopyasını silen işlev (testte sahte enjekte edilir; ağa çıkmaz).
var yetimUzakSil = yedekUzaktanSilHedefle

// izIDUret: silme izinin benzersiz kimliği — 16 bayt crypto/rand, 32 hex (backups.iz_id VARCHAR(32)).
// Testte değiştirilebilir.
var izIDUret = func() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// silinenAyar: cp_ayarlar'daki ham değer ("" = ayarsız → slot). Satır YOKSA ("", nil); okuma HATASIysa ("", err):
// "ayarsız" ile "okunamadı" ayrı şeylerdir (ayar 0 = otomatik silme yok iken geçici okuma hatası yetimleri 7 gün sonra
// sildirmesin; ayar GET ucu hatayı 200+null ile gizlemesin).
func silinenAyar(ctx context.Context, db *sql.DB) (string, error) {
	var v string
	err := db.QueryRowContext(ctx, `SELECT deger FROM cp_ayarlar WHERE anahtar=?`, silinenAyarKey).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(v), nil
}

// silinenGun — bu silme için saklama günü (0 = otomatik silme yok). ok=false: ayar OKUNAMADI (günlüğe 🔴 yazılır).
// Çağıranlar okunamayan kuralı "sil" yönünde yorumlamamalı: iz yazımı süresiz saklamayı (gun=0), S2 turu işaretlemeyi
// sonraya bırakmayı seçer — erken silme geri alınamaz, geç silme yönetici tarafından düzeltilebilir.
func silinenGun(ctx context.Context, db *sql.DB, slot int) (gun int, ok bool) {
	ayar, err := silinenAyar(ctx, db)
	if err != nil {
		log.Printf("🔴 silinmiş yedek saklama ayarı okunamadı: %v", err)
		return 0, false
	}
	return silinenSaklamaGunu(ayar, slot), true
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

// yedekDosyaSKRe: yedek dosya adından sk türetme. Tanınan adlar (hepsi .tar.gz):
//
//	<sk>-YYYYMMDD-HHMMSS                      güncel ad (yedekDosyaAdi)
//	<sk>-YYYYMMDD-HHMMSS-N                    aynı saniye çakışma eki (N = 2…99)
//	<sk>-YYYYMMDD-HHMMSS-auto | -planli       eski bash zamanlayıcısının kaynak eki
//	<sk>-auto-YYYYMMDD-HHMMSS                 eski ön ekli ad
//
// sk karakter kümesinde '-' olmadığından belirsizlik yok (bir sk, başka bir sk'nın öneki gibi okunamaz: sk'dan sonra
// DAİMA '-' + isteğe bağlı "auto-" + 8 rakam gelmek zorundadır).
var yedekDosyaSKRe = regexp.MustCompile(`^(c_[a-z0-9_]{1,40})-(?:auto-)?\d{8}-\d{6}(?:-(?:planli|auto))?(?:-\d{1,2})?\.tar\.gz$`)

// skDosyaAdindan — silme izi yazılamamış (silinme NULL) satırlar için sk'yı dosya adından türetir.
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

// yetimHedefHam — silinecek domainin uzak hedefinin ŞİFRELİ ham kaydı (parola gizli.SaklaBagli çıktısı;
// çözme yalnız silme anında yetimHedefOku'da).
type yetimHedefHam struct {
	tip, host, kullanici, parola, uzakDizin string
	port                                    int
}

// sirlar: hata/log metinlerinden maskelenecek değerler (ham şifreli ve çözülmüş parola).
func (h *yetimHedefHam) sirlar() []string {
	return []string{h.parola, gizli.CozBagli(h.parola, "yedek")}
}

// SilinenIz — silinecek hostingin yetim-işaret bilgisi (domains satırı gitmeden ÖNCE okunur).
type SilinenIz struct {
	alanAdi, sk string
	domainID    int64
	resellerID  int64
	slot        int
	izID        string         // bu silmenin benzersiz kimliği (O4)
	hedef       *yetimHedefHam // domaine özel uzak hedef (yoksa nil)
}

// SilinenIzHazirla — domains satırı gitmeden ÖNCE çağrılır; YALNIZ DB okur (dizine dokunmaz). 3 sn zaman aşımı.
// Geçersiz sk/id ya da hatada nil döner; nil iz Yaz'da yok sayılır. Panik yutulur.
//
// 🔴 D3: domain satırı (sahip + slot) OKUNAMAZSA iz HİÇ kurulmaz (nil): eskiden Scan hatası yutulup
// reseller_id=0 (= kök sahip) yazılıyordu. Satırlar yerinde kalır; işaretleme turu (panel açılışı/04:00)
// onları sahipsiz olarak işaretler (sahip BİLİNMEZ → kök sayılmaz, alan adı yerine sk yazılır).
func SilinenIzHazirla(ctx context.Context, db *sql.DB, domainID int64, alanAdi, sk string) (iz *SilinenIz) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("silinen yedek iz hazırlama: panik (%s): %v", alanAdi, r)
			iz = nil
		}
	}()
	// domainID<=0: Yaz'ın `WHERE domain_id=?` koşulu 0'da MEVCUT yetimleri vururdu.
	if domainID <= 0 || !silinenSKGecerli(sk) {
		return nil
	}
	ctx, iptal := context.WithTimeout(ctx, izZamanAsimi)
	defer iptal()
	izID, err := izIDUret()
	if err != nil {
		log.Printf("🔴 silinen yedek iz hazırlama: iz kimliği üretilemedi (%s): %v — iz yazılmayacak, işaretleme turu işaretler", alanAdi, err)
		return nil
	}
	iz = &SilinenIz{alanAdi: alanAdi, sk: sk, domainID: domainID, slot: 7, izID: izID}
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(reseller_id,0), COALESCE(backup_retention,7) FROM domains WHERE id=?`, domainID).
		Scan(&iz.resellerID, &iz.slot); err != nil {
		log.Printf("🔴 silinen yedek iz hazırlama: hosting satırı okunamadı (%s): %v — iz yazılmayacak (sahip bilinmeden yazılmaz), işaretleme turu işaretler", alanAdi, err)
		return nil
	}
	// S1: uzak hedef (CASCADE ile gidecek) — şifreli ham değer okunur. Tablo yok/hata → hedefsiz devam.
	h := &yetimHedefHam{}
	if err := db.QueryRowContext(ctx,
		`SELECT tip, host, port, kullanici, parola, uzak_dizin FROM backup_destinations WHERE domain_id=?`, domainID).
		Scan(&h.tip, &h.host, &h.port, &h.kullanici, &h.parola, &h.uzakDizin); err == nil && strings.TrimSpace(h.host) != "" {
		iz.hedef = h
	}
	return iz
}

// Yaz — hosting satırı SİLİNDİKTEN sonra çağrılır (silme başarısızsa iz bırakılmaz): o domainin yedek
// satırlarını yetim olarak işaretler. Hata vermez/engellemez, 3 sn zaman aşımı (izZamanAsimi), panik yutulur.
func (iz *SilinenIz) Yaz(ctx context.Context, db *sql.DB) {
	if iz == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("🔴 silinen yedek iz: panik (%s): %v", iz.alanAdi, r)
		}
	}()
	if err := iz.yaz(ctx, db); err != nil {
		log.Printf("🔴 silinen yedek iz YAZILAMADI (%s; yedekler yerinde, sahipsiz işaretleme turu panel açılışında/04:00'te sk'yı dosya adından türetip işaretler): %v", iz.alanAdi, err)
	}
}

func (iz *SilinenIz) yaz(ctx context.Context, db *sql.DB) error {
	ctx, iptal := context.WithTimeout(ctx, izZamanAsimi)
	defer iptal()
	if iz.izID == "" { // doğrudan kurulmuş iz (testler/ara çağıranlar)
		id, err := izIDUret()
		if err != nil {
			return err
		}
		iz.izID = id
	}
	// S1: anlık görüntü UPDATE'ten ÖNCE — UPDATE çökerse satırlar S2 süpürmesiyle domain_id>0'dan
	// eski_domain_id'ye taşınır ve bu anlık görüntü (henüz hiçbir satırın iz_id'si değil) S2 tarafından
	// eski_domain_id eşleşmesiyle sahiplenilir.
	if iz.hedef != nil {
		iz.hedefAnlikGoruntusu(ctx, db)
	}
	gun, _ := silinenGun(ctx, db, iz.slot) // kural okunamazsa 0: süresiz sakla (son_kullanma NULL), yönetici ucundan elle silinir
	// S4: satır domain_id=0'a taşınır, eski id eski_domain_id'de (yalnız bilgi) kalır; bağ iz_id iledir.
	q := `UPDATE backups SET alan_adi=?, sistem_kullanici=?, reseller_id=?, eski_domain_id=?, iz_id=?, domain_id=0, silinme=NOW(), son_kullanma=NULL WHERE domain_id=?`
	args := []any{iz.alanAdi, iz.sk, iz.resellerID, iz.domainID, iz.izID, iz.domainID}
	if gun > 0 { // 0 = otomatik silme yok (son_kullanma NULL)
		q = `UPDATE backups SET alan_adi=?, sistem_kullanici=?, reseller_id=?, eski_domain_id=?, iz_id=?, domain_id=0, silinme=NOW(), son_kullanma=DATE_ADD(NOW(), INTERVAL ? DAY) WHERE domain_id=?`
		args = []any{iz.alanAdi, iz.sk, iz.resellerID, iz.domainID, iz.izID, gun, iz.domainID}
	}
	// S2: geçici hata (kilit/kilitlenme/kısa kopma) için sınırlı yeniden deneme (izDeneme, izZamanAsimi içinde).
	var err error
	for deneme := 1; deneme <= izDeneme; deneme++ {
		err = yalitimliIslem(ctx, db, func(tx *sql.Tx) error {
			_, e := tx.ExecContext(ctx, q, args...)
			return e
		})
		if err == nil {
			return nil
		}
		if deneme < izDeneme {
			log.Printf("🔴 silinen yedek iz UPDATE hatası (%s), tekrar denenecek (%d/%d): %v", iz.alanAdi, deneme, izDeneme, err)
			select {
			case <-ctx.Done():
				return err
			case <-time.After(izYenidenDenemeBekleme):
			}
		}
	}
	return err
}

// mysqlHatasi — err bir MySQL/MariaDB hatasıysa ve numarası verilenlerden biriyse true.
func mysqlHatasi(err error, nolar ...uint16) bool {
	var me *mysql.MySQLError
	if !errors.As(err, &me) {
		return false
	}
	for _, n := range nolar {
		if me.Number == n {
			return true
		}
	}
	return false
}

// kilitHatasi — InnoDB kilitlenmesi (1213) ya da kilit bekleme zaman aşımı (1205): işlem yeniden denenebilir.
func kilitHatasi(err error) bool { return mysqlHatasi(err, 1213, 1205) }

// yalitimliIslem — fn'i READ COMMITTED bir işlemde koşturur ve işlem başarılıysa commit eder. READ COMMITTED'da
// aralık (gap) kilidi tutulmaz: komşu sk'lara / domain_id aralıklarına yazan eşzamanlı işlemlerle (ix_backups_silinen,
// ix_backup_domain) InnoDB kilitlenmesi oluşmaz. İkili günlük STATEMENT kipinde READ COMMITTED InnoDB yazmalarını
// reddeder (1665): o durumda varsayılan yalıtımla bir kez daha denenir. fn hata dönerse işlem geri alınır.
func yalitimliIslem(ctx context.Context, db *sql.DB, fn func(tx *sql.Tx) error) error {
	var son error
	for _, iso := range []sql.IsolationLevel{sql.LevelReadCommitted, sql.LevelDefault} {
		tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: iso})
		if err != nil {
			return err
		}
		if err = fn(tx); err != nil {
			_ = tx.Rollback()
		} else if err = tx.Commit(); err == nil {
			return nil
		}
		son = err
		if iso == sql.LevelReadCommitted && mysqlHatasi(err, 1665) {
			continue
		}
		return err
	}
	return son
}

// hedefAnlikGoruntusu — domainin uzak hedefini backup_yetim_hedefler'e KALICI kopyalar (yalnız domainin
// yedek satırı varsa), bu silmenin iz_id'si altında. Hata silmeyi/izi ENGELLEMEZ; yalnız loglanır
// (parola maskeli).
//
// 🔴 D4 — AT-REST: kaynak değer anahtar yokken/eski kayıtta düz metin olabilir ve ham kopya düz parolayı
// ikinci bir tabloya taşırdı. Kopya gizli.SaklaGecis ile yazılır: zaten şifreliyse AYNEN (okuma CozBagli'nin
// baglamsız eski kayıt geri dönüşüyle sürer), düz metinse (anahtar varsa) şifrelenir. Şifrelenmiş hâli sütuna
// SIĞMAZSA (düz metin ≳160 karakter) satır yazılmaz ve 🔴 loglanır: düz parola asla ikinci tabloya girmez.
func (iz *SilinenIz) hedefAnlikGoruntusu(ctx context.Context, db *sql.DB) {
	var var1 int
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM backups WHERE domain_id=?)`, iz.domainID).Scan(&var1); err != nil || var1 != 1 {
		return
	}
	h := iz.hedef
	parola := gizli.SaklaGecis(h.parola, "yedek")
	if len([]rune(parola)) > yetimHedefParolaSutun {
		// katı olmayan sql_mode sütunu SESSİZCE keserdi (çözülemez parola); yazmamak daha dürüst.
		log.Printf("🔴 silinen yedek: uzak hedef anlık görüntüsü yazılamadı (%s; şifreli parola %d karakter, sütun %d: yetim yedeklerin uzak kopyaları otomatik silinemeyecek)",
			iz.alanAdi, len([]rune(parola)), yetimHedefParolaSutun)
		return
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO backup_yetim_hedefler (iz_id, eski_domain_id, tip, host, port, kullanici, parola, uzak_dizin) VALUES (?,?,?,?,?,?,?,?)`,
		iz.izID, iz.domainID, h.tip, h.host, h.port, h.kullanici, parola, h.uzakDizin); err != nil {
		log.Printf("🔴 silinen yedek: uzak hedef anlık görüntüsü yazılamadı (%s; yetim yedeklerin uzak kopyaları otomatik silinemeyecek): %s",
			iz.alanAdi, hedef.MaskeleHata(err, h.sirlar()...))
	}
}

// satiriYetimIsaretle — TEK satırı yetim yapar: alan_adi/sk eskiden doldurulmuşsa (göç yedeklemesi) KORUNUR, boşsa sk
// yazılır; sahip (reseller_id) BİLİNMEZ ve değiştirilmez; domain_id=0'a taşınır, eski id eski_domain_id'ye yazılır,
// iz_id atanır. UPDATE koşulu satırın HÂLÂ işaretsiz olduğunu (silinme NULL) ve verilen koşulu (kosul) yeniden
// doğrular: seçim ile güncelleme arasında satır işaretlenirse ya da domain oluşturulursa canlı yedek yetim yapılmaz.
func satiriYetimIsaretle(ctx context.Context, db *sql.DB, id int64, sk, izID string, gun int, kosul string, kargs ...any) (bool, error) {
	set := `alan_adi=IF(alan_adi='',?,alan_adi), sistem_kullanici=IF(sistem_kullanici='',?,sistem_kullanici),
		eski_domain_id=domain_id, iz_id=?, domain_id=0, silinme=NOW(), son_kullanma=NULL`
	args := []any{sk, sk, izID}
	if gun > 0 {
		set = strings.Replace(set, "son_kullanma=NULL", "son_kullanma=DATE_ADD(NOW(), INTERVAL ? DAY)", 1)
		args = append(args, gun)
	}
	args = append(args, id)
	args = append(args, kargs...)
	res, err := db.ExecContext(ctx, `UPDATE backups SET `+set+` WHERE id=? AND silinme IS NULL AND `+kosul, args...)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// sahipsizSatiriIsaretle — S2: TEK sahipsiz (domain'i olmayan) satırı yetim yapar (bkz. satiriYetimIsaretle).
func sahipsizSatiriIsaretle(ctx context.Context, db *sql.DB, id int64, sk, izID string, gun int) (bool, error) {
	return satiriYetimIsaretle(ctx, db, id, sk, izID, gun,
		`domain_id>0 AND NOT EXISTS (SELECT 1 FROM domains d WHERE d.id=backups.domain_id)`)
}

// YetimKalintilariniAyir — A2 azaltması. YENİ açılmış bir domainin kimliğine (domain_id) bağlı HİÇBİR yedek satırı olamaz;
// varsa bunlar o kimliği daha önce taşıyan silinmiş bir hostingin kalıntılarıdır: silme izi yazılamamıştı, sahipsiz
// işaretleme turu (panel açılışı/04:00) henüz koşmadı ve kimlik yeniden kullanıldı (AUTO_INCREMENT geri sarılması,
// yedekten dönüş). Bırakılırlarsa yeni müşteri eski müşterinin yedeklerini listeler/indirir/geri yükler (eskiden FK
// CASCADE bunu imkânsız kılıyordu). domains.Create ve taşıma, satırı yazdıktan hemen sonra çağırır: bu satırları sk'yı
// dosya adından türeterek yetim işaretler (S2 ile aynı kural; anlık görüntü varsa sahiplenilir). sk türetilemeyen
// satırlara dokunulmaz (loglanır). Dönen: işaretlenen satır sayısı. Hata/panik çağıranı etkilemez.
func YetimKalintilariniAyir(ctx context.Context, db *sql.DB, domainID int64) (isaretlenen int) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("yetim kalıntı ayırma: panik (domain=%d): %v", domainID, r)
		}
	}()
	if db == nil || domainID <= 0 {
		return 0
	}
	ctx, iptal := context.WithTimeout(ctx, kalintiZamanAsimi)
	defer iptal()
	rows, err := db.QueryContext(ctx, `SELECT id, dosya FROM backups WHERE domain_id=? AND silinme IS NULL ORDER BY id LIMIT ?`, domainID, silinenSatirLimit)
	if err != nil {
		log.Printf("yetim kalıntı ayırma (domain=%d): %v", domainID, err)
		return 0
	}
	type kalinti struct {
		id    int64
		dosya string
	}
	var liste []kalinti
	for rows.Next() {
		var k kalinti
		if rows.Scan(&k.id, &k.dosya) == nil {
			liste = append(liste, k)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(liste) == 0 {
		if err != nil {
			log.Printf("yetim kalıntı ayırma (domain=%d): %v", domainID, err)
		}
		return 0
	}
	gun, _ := silinenGun(ctx, db, silinenEnAzGun) // kural okunamazsa 0: yeni müşteriden AYIRMAK şart, silme süresi değil → süresiz sakla
	izler := map[string]string{}
	turetilemeyen := 0
	for _, k := range liste {
		sk, ok := skDosyaAdindan(k.dosya)
		if !ok {
			turetilemeyen++
			continue
		}
		izID, var_ := izler[sk]
		if !var_ {
			izID = sahipsizIzID(ctx, db, domainID)
			izler[sk] = izID
		}
		ok2, e := satiriYetimIsaretle(ctx, db, k.id, sk, izID, gun, `domain_id=?`, domainID)
		if e != nil {
			log.Printf("yetim kalıntı %d işaretlenemedi (domain=%d): %v", k.id, domainID, e)
			continue
		}
		if ok2 {
			isaretlenen++
		}
	}
	log.Printf("🔴 yeni domain %d kimliğine bağlı %d eski yedek satırı bulundu (kimlik yeniden kullanılmış, silme izi yazılamamıştı): %d yetim işaretlendi, %d satırın sk'sı dosya adından türetilemedi (dokunulmadı)",
		domainID, len(liste), isaretlenen, turetilemeyen)
	return isaretlenen
}

// sahipsizIsaretKilit: aynı anda tek sahipsiz işaretleme turu (S2).
var sahipsizIsaretKilit sync.Mutex

// sahipsizAday — S2'nin işaretlemeye aday satırı.
type sahipsizAday struct {
	id, domainID int64
	dosya        string
}

// sahipsizAdaylar — imleçten (id > imlec) sonraki sahipsiz satırlar. Scan hatalı satır ATLANIR; rows.Err() hata döner.
func sahipsizAdaylar(ctx context.Context, db *sql.DB, imlec int64) ([]sahipsizAday, int64, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT b.id, b.domain_id, b.dosya FROM backups b
		 WHERE b.id>? AND b.domain_id>0 AND b.silinme IS NULL AND NOT EXISTS (SELECT 1 FROM domains d WHERE d.id=b.domain_id)
		 ORDER BY b.id LIMIT ?`, imlec, sahipsizLimit)
	if err != nil {
		return nil, imlec, err
	}
	defer rows.Close()
	var liste []sahipsizAday
	son := imlec
	for rows.Next() {
		var s sahipsizAday
		if rows.Scan(&s.id, &s.domainID, &s.dosya) != nil {
			continue
		}
		liste = append(liste, s)
		if s.id > son {
			son = s.id
		}
	}
	if err := rows.Err(); err != nil {
		return nil, imlec, err
	}
	return liste, son, nil
}

// sahipsizIzID — S2: bir eski domain kimliğinin sahipsiz satırlarına verilecek iz_id. İz yazılırken anlık
// görüntü UPDATE'ten ÖNCE yazıldığı için, bu kimliğe ait HİÇBİR yetim satırın kullanmadığı bir anlık görüntü
// varsa (UPDATE hiç yazılamadı) onun iz_id'si devralınır: yetimlerin uzak kopyaları yine silinebilir. Yoksa yeni kimlik.
func sahipsizIzID(ctx context.Context, db *sql.DB, domainID int64) string {
	var id string
	err := db.QueryRowContext(ctx,
		`SELECT h.iz_id FROM backup_yetim_hedefler h
		 WHERE h.eski_domain_id=? AND h.iz_id<>'' AND NOT EXISTS (SELECT 1 FROM backups b WHERE b.domain_id=0 AND b.iz_id=h.iz_id)
		 ORDER BY h.id DESC LIMIT 1`, domainID).Scan(&id)
	if err == nil && id != "" {
		return id
	}
	yeni, e := izIDUret()
	if e != nil {
		return ""
	}
	return yeni
}

// SilinenSahipsizIsaretle — S2: domain'i olmayan ama yetim işaretsiz (domain_id>0, silinme NULL) satırları
// yetim yapar: sk'yı dosya adından türetir (türetilemeyene DOKUNMAZ, sayıp loglar), alan_adi=sk,
// silinme=NOW(), son_kullanma ayara göre (slot bilinmiyor → ayar ya da 7 gün). Satır domain_id=0'a
// taşınır, eski id eski_domain_id'ye yazılır. İdempotent; panel açılışında ve 04:00 turunda çağrılır.
// Satırlar id imleciyle PARTİ PARTİ işlenir (türetilemeyen satırlar arkadakileri aç bırakmaz) ve bağlam
// (süre bütçesi) bitene ya da aday kalmayana dek sürer.
//
// 🔴 D6 — EŞİK: aday sayısı sahipsizEsikAdet'ten çoksa ve (domains tablosu boşsa ya da adaylar canlı+aday backups
// satırlarının (domain_id>0; yetimler hariç) yarısından fazlaysa) toplu işaretleme ATLANIR: eksik/hatalı bir `domains` tablosu TÜM yedekleri
// sahipsiz gösterir ve ≥7 gün sonra dosya + uzak kopyalar silinirdi. Atlamak veri kaybettirmez (satırlar
// olduğu gibi kalır); operatör 🔴 günlüğü görür.
// Dönen: işaretlenen, türetilemeyen.
func SilinenSahipsizIsaretle(ctx context.Context, db *sql.DB) (isaretlenen, turetilemeyen int) {
	// Panel açılışındaki tur ile 04:00 turu çakışırsa iki koşu aynı (domain, sk) grubuna FARKLI iz_id verebilir ve
	// biri anlık görüntüsüz kalırdı: aynı anda tek tur (diğeri bekletilmez, sonraki tur zaten tamamlar).
	if !sahipsizIsaretKilit.TryLock() {
		return 0, 0
	}
	defer sahipsizIsaretKilit.Unlock()
	var aday, toplam, alanSayisi int64
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM backups b WHERE b.domain_id>0 AND b.silinme IS NULL AND NOT EXISTS (SELECT 1 FROM domains d WHERE d.id=b.domain_id)`).Scan(&aday); err != nil {
		log.Printf("sahipsiz yedek işaretleme: %v", err)
		return 0, 0
	}
	if aday == 0 {
		return 0, 0
	}
	// Payda: canlı + aday satırlar (domain_id>0). Zaten işaretli YETİMLER (domain_id=0) paydayı şişirmez: toplu bayi silmesinden
	// sonra binlerce yetim varken bozuk bir `domains` tablosu yedeklerin çoğunu sahipsiz gösterse bile oran %50'nin altına inmesin.
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM backups WHERE domain_id>0`).Scan(&toplam); err != nil {
		log.Printf("sahipsiz yedek işaretleme: toplam sayılamadı, atlandı: %v", err)
		return 0, 0
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM domains`).Scan(&alanSayisi); err != nil {
		log.Printf("sahipsiz yedek işaretleme: domains sayılamadı, atlandı: %v", err)
		return 0, 0
	}
	if aday > sahipsizEsikAdet && (alanSayisi == 0 || aday*2 > toplam) {
		log.Printf("🔴 sahipsiz yedek işaretleme ATLANDI: %d aday satır (backups toplam %d, domains %d) — `domains` tablosu eksik/bozuk olabilir; "+
			"toplu işaretleme, dosyaları ve uzak kopyaları ≥7 gün sonra sildirirdi. Hiçbir satıra dokunulmadı; tabloyu doğrulayıp elle işaretleyin",
			aday, toplam, alanSayisi)
		return 0, 0
	}
	gun, okundu := silinenGun(ctx, db, silinenEnAzGun)
	if !okundu {
		return 0, 0 // idempotent tur: sonraki tur (04:00/açılış) işaretler; yanlış süreyle işaretlenmesin
	}
	type izAnahtar struct {
		dom int64
		sk  string
	}
	izler := map[izAnahtar]string{}
	var imlec int64
	for ctx.Err() == nil {
		liste, son, err := sahipsizAdaylar(ctx, db, imlec)
		if err != nil {
			log.Printf("sahipsiz yedek işaretleme: %v", err)
			break
		}
		if len(liste) == 0 {
			break
		}
		for _, s := range liste {
			if ctx.Err() != nil {
				break
			}
			sk, ok := skDosyaAdindan(s.dosya)
			if !ok {
				turetilemeyen++
				continue
			}
			a := izAnahtar{s.domainID, sk}
			izID, var_ := izler[a]
			if !var_ {
				izID = sahipsizIzID(ctx, db, s.domainID)
				izler[a] = izID
			}
			ok2, e := sahipsizSatiriIsaretle(ctx, db, s.id, sk, izID, gun)
			if e != nil {
				log.Printf("sahipsiz yedek %d işaretlenemedi: %v", s.id, e)
				continue
			}
			if ok2 {
				isaretlenen++
			}
		}
		if son <= imlec || len(liste) < sahipsizLimit {
			break
		}
		imlec = son
	}
	if isaretlenen > 0 || turetilemeyen > 0 {
		log.Printf("sahipsiz yedek işaretleme: %d satır yetim yapıldı, %d satırın sk'sı dosya adından türetilemedi (dokunulmadı)", isaretlenen, turetilemeyen)
	}
	return isaretlenen, turetilemeyen
}

// yetimHedefOku — yetim yedeğin silme izine (iz_id) ait uzak hedef anlık görüntüsü (yoksa nil, nil).
// Parola burada ÇÖZÜLÜR; Destination yalnız bellekte yaşar, asla döndürülmez/loglanmaz.
func yetimHedefOku(ctx context.Context, db *sql.DB, izID string) (*Destination, error) {
	if izID == "" {
		return nil, nil
	}
	d := &Destination{Aktif: true}
	var ham string
	err := db.QueryRowContext(ctx,
		`SELECT tip, host, port, kullanici, parola, uzak_dizin FROM backup_yetim_hedefler WHERE iz_id=?`, izID).
		Scan(&d.Tip, &d.Host, &d.Port, &d.Kullanici, &ham, &d.UzakDizin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.Parola = gizli.CozBagli(ham, "yedek")
	return d, nil
}

// yetimHedefBittiyse — iz_id'nin yetim satırı kalmadıysa anlık görüntüyü (parola dahil) siler.
func yetimHedefBittiyse(ctx context.Context, db *sql.DB, izID string) {
	if izID == "" {
		return
	}
	if _, err := db.ExecContext(ctx,
		`DELETE FROM backup_yetim_hedefler WHERE iz_id=?
		 AND NOT EXISTS (SELECT 1 FROM backups b WHERE b.domain_id=0 AND b.iz_id=?)`, izID, izID); err != nil {
		log.Printf("yetim hedef anlık görüntüsü silinemedi (iz %s): %v", izID, err)
	}
}

// yetimHedefArtiklariniTemizle — hiçbir yetim satırın (ya da domain'i olmayan, işaretsiz satırın) artık
// kullanmadığı, 1 saatten eski anlık görüntüleri siler (parola kalmasın). Yeni yazılmış satırlara
// dokunmamak için yaş koşulu var: anlık görüntü UPDATE'ten önce yazılır.
// Korunan: iz_id'sini bir yetim satırın taşıdığı anlık görüntüler ve eski_domain_id'sinde HÂLÂ sahipsiz
// (domain'i olmayan, işaretsiz) satır duran anlık görüntüler (S2 onları sahiplenecek). Canlı bir domainin
// satırları, kimliği yeniden kullanılmış olsa bile anlık görüntüyü ayakta TUTMAZ (E7c).
func yetimHedefArtiklariniTemizle(ctx context.Context, db *sql.DB) {
	res, err := db.ExecContext(ctx,
		`DELETE FROM backup_yetim_hedefler WHERE olusturma < NOW() - INTERVAL 1 HOUR
		 AND NOT EXISTS (SELECT 1 FROM backups b WHERE b.domain_id=0 AND b.iz_id=backup_yetim_hedefler.iz_id)
		 AND NOT EXISTS (SELECT 1 FROM backups b2 WHERE b2.domain_id=backup_yetim_hedefler.eski_domain_id AND b2.domain_id>0 AND b2.silinme IS NULL
		                 AND NOT EXISTS (SELECT 1 FROM domains d WHERE d.id=b2.domain_id))`)
	if err != nil {
		log.Printf("yetim hedef artık temizliği: %v", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("yetim hedef artık temizliği: %d kullanılmayan anlık görüntü silindi", n)
	}
}

// errHedefDomainYok — bağlama hedefi domain yok (kontrol ile bağlama arasında silindi ya da hiç yoktu).
var errHedefDomainYok = errors.New("hedef domain yok")

// yetimleriBagla — kosul (sk/alan/sahip) ile seçilen yetim yedekleri domaine bağlar: domain_id=<domain>,
// eski_domain_id=0, iz_id boş, silinme/son_kullanma NULL, tip='kurtarma' (S3). Hepsi TEK READ COMMITTED işlemde:
//   - hedef domain işlem içinde KİLİTLENİR (SELECT … FOR UPDATE) ve varlığı doğrulanır: bağlama sürerken silinemez,
//     olmayan bir domaine satır bağlanmaz (N1);
//   - her satır okuma/yazma hatası işlemi iptal eder: eşzamanlı işlemlerle InnoDB kilitlenmesi (1213) ya da kilit bekleme
//     zaman aşımı (1205) olursa işlem sunucuda geri alınmıştır — hata YUTULUP devam edilirse sonraki deyim otomatik
//     commit'te koşardı (B5/B6). rows.Err() dahil hepsi kontrol edilir; 1213/1205'te işlem SINIRLI sayıda yeniden denenir;
//   - READ COMMITTED: ix_backups_silinen / ix_backup_domain üzerinde boşluk kilidi yok → komşu sk'lardaki eşzamanlı
//     silme izi yazımlarıyla kilitlenme olmaz.
//
// Uzak hedef anlık görüntüsü domaine HİÇ geri yüklenmez (bkz. dosya başlığı); bağlamadan sonra, yetimi kalmayan
// izlerin anlık görüntüleri (parola dahil) işlem DIŞINDA, best-effort silinir. Bağlanan sayıyı döner.
func yetimleriBagla(ctx context.Context, db *sql.DB, domainID int64, kosul string, args ...any) (int64, error) {
	if domainID <= 0 {
		return 0, errors.New("geçersiz domain")
	}
	var n int64
	var izler []string
	var err error
	for deneme := 1; deneme <= baglaDeneme; deneme++ {
		n, izler, err = yetimleriBaglaBir(ctx, db, domainID, kosul, args...)
		if err == nil || !kilitHatasi(err) || ctx.Err() != nil {
			break
		}
		log.Printf("yetim yedek bağlama: kilit hatası, işlem yeniden denenecek (%d/%d, domain=%d): %v", deneme, baglaDeneme, domainID, err)
		if deneme < baglaDeneme {
			select {
			case <-ctx.Done():
			case <-time.After(baglaBekleme * time.Duration(deneme)):
			}
		}
	}
	if err != nil {
		return 0, err
	}
	for _, iz := range izler {
		yetimHedefBittiyse(ctx, db, iz)
	}
	return n, nil
}

// yetimleriBaglaBir — yetimleriBagla'nın TEK deneme gövdesi. Bağlanan sayı ve etkilenen iz kimliklerini döner.
func yetimleriBaglaBir(ctx context.Context, db *sql.DB, domainID int64, kosul string, args ...any) (n int64, izler []string, err error) {
	err = yalitimliIslem(ctx, db, func(tx *sql.Tx) error {
		var var1 int64
		if e := tx.QueryRowContext(ctx, `SELECT id FROM domains WHERE id=? FOR UPDATE`, domainID).Scan(&var1); e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				return errHedefDomainYok
			}
			return e
		}
		rows, e := tx.QueryContext(ctx, `SELECT DISTINCT iz_id FROM backups WHERE `+kosul+` AND `+yetimKosulu+` FOR UPDATE`, args...)
		if e != nil {
			return e
		}
		izler = izler[:0]
		for rows.Next() {
			var iz string
			if rows.Scan(&iz) != nil {
				continue
			}
			if iz != "" {
				izler = append(izler, iz)
			}
		}
		if e := rows.Err(); e != nil {
			rows.Close()
			return e
		}
		rows.Close()
		// O2: uzak kopyası şu an SİLİNMEKTE olan satırlar bağlanmaz (satırlar yukarıda kilitlendi; defter kilitten SONRA
		// okunur: silme kaydı bu noktadan sonra düşse bile yetimHalaYetim bu işlemin commit'ini bekler ve bağlı satırı görür).
		set := `UPDATE backups SET domain_id=?, eski_domain_id=0, iz_id='', silinme=NULL, son_kullanma=NULL, tip=? WHERE ` + kosul + ` AND ` + yetimKosulu
		uargs := append([]any{domainID, TipKurtarma}, args...)
		if sil := yetimSilinmektekiler(); len(sil) > 0 {
			set += ` AND id NOT IN (` + strings.TrimSuffix(strings.Repeat("?,", len(sil)), ",") + `)`
			for _, id := range sil {
				uargs = append(uargs, id)
			}
		}
		res, e := tx.ExecContext(ctx, set, uargs...)
		if e != nil {
			return e
		}
		n, _ = res.RowsAffected()
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	return n, izler, nil
}

// YetimleriDevral — POST /domains `yedekleri_devral:true` bayrağı: yeni açılan domainin alan adı (küçük harf,
// kırpılmış) + sahibi (reseller_id) + sk'sı ile BİREBİR aynı olan yetim yedekleri bu domaine bağlar.
// Y1: bağlama YALNIZ bu açık bayrakla (ve yönetici ucuyla) olur; sayfa açmak hiçbir şeyi bağlamaz.
// Eşleşme yoksa 0; hata varsa (0, err) — çağıran hosting oluşturmayı BAŞARISIZ yapmamalıdır.
func YetimleriDevral(ctx context.Context, db *sql.DB, domainID int64) (int64, error) {
	var alan, sk string
	var rid int64
	if err := db.QueryRowContext(ctx, `SELECT alan_adi, sistem_kullanici, COALESCE(reseller_id,0) FROM domains WHERE id=?`, domainID).
		Scan(&alan, &sk, &rid); err != nil {
		return 0, err
	}
	alan = strings.ToLower(strings.TrimSpace(alan))
	if alan == "" || !silinenSKGecerli(sk) {
		return 0, nil
	}
	n, err := yetimleriBagla(ctx, db, domainID, `sistem_kullanici=? AND LOWER(TRIM(alan_adi))=? AND reseller_id=?`, sk, alan, rid)
	if err != nil {
		return 0, err
	}
	if n > 0 {
		log.Printf("yetim yedekler devralındı: domain=%d sk=%s adet=%d", domainID, sk, n)
	}
	return n, nil
}

// maskeliUzakHata — uzak silme hatasından anlık görüntü parolasını temizler (lftp çıktısı/ağ hatası
// ayrıntıları API yanıtına ve loga gider). ErrLftpYok sabit mesajdır, aynen döner. d==nil iken (anlık
// görüntü yok, yalnız genel hedef) bilinen sır yoktur ama sır BİÇİMLİ desenler (URL kullanıcı bilgisi vb.) yine maskelenir (D3).
func maskeliUzakHata(err error, d *Destination) error {
	if err == nil || errors.Is(err, ErrLftpYok) {
		return err
	}
	if d == nil {
		return errors.New(hedef.Maskele(err.Error()))
	}
	return errors.New(hedef.Maskele(err.Error(), d.Parola))
}

// errYetimDegil — silinecek satır araya giren bir bağlamayla artık yetim değil (canlı yedek, dokunulmadı).
var errYetimDegil = errors.New("artık yetim değil (araya bağlama girdi), atlandı")

// yetimSilHatasi — yetim yedek silme hatasının KAYNAĞINI taşır: uzak depo (uzak=true: ulaşılamıyor, lftp yok…)
// ya da yerel dosya. DELETE ucu `uzagi_yoksay` önerisini yalnız uzak kaynaklı hatalarda sunar.
type yetimSilHatasi struct {
	uzak bool
	err  error
}

func (e *yetimSilHatasi) Error() string { return e.err.Error() }
func (e *yetimSilHatasi) Unwrap() error { return e.err }

// uzakHedefUlasilamiyor — uzak silme hatası HEDEFİN kendisinin erişilemezliğini mi gösteriyor (çözümleme/bağlantı/
// kimlik doğrulama/zaman aşımı/lftp yok) yoksa tek dosyaya özgü bir hata mı (izin, silindi ama hâlâ listeleniyor)?
// Yalnız ilki aynı hedefe giden diğer satırlar için de geçerlidir: devre kesici (SilinenSupur/SilinenSil) yalnız buna
// bakar — dosyaya özgü bir hata, aynı hesabın sağlam satırlarının temizliğini ertelememeli.
func uzakHedefUlasilamiyor(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrLftpYok) {
		return true
	}
	// context.DeadlineExceeded (satır başına süre sınırımız) metniyle ("context deadline exceeded") ya da lftp sürecinin
	// öldürülmesiyle ("signal: killed") yakalanır.
	m := strings.ToLower(err.Error())
	for _, p := range []string{"login failed", "connection refused", "could not resolve", "host key verification failed",
		"no route to host", "connection timed out", "cannot connect", "name or service not known", "network is unreachable",
		"context deadline exceeded", "signal: killed", "timeout"} {
		if strings.Contains(m, p) {
			return true
		}
	}
	return false
}

// yetimSatiriniVeDosyayiSil — O2/D3: satırı KOŞULLU siler (`AND domain_id=0 AND silinme IS NOT NULL`), YALNIZ tam bir
// satır silindiyse dosyayı siler; dosya silinemezse (yok hariç) işlemi geri alıp satırı KORUR. Satır kilidi
// işlem boyunca tutulur: aynı satırı bağlamak isteyen yetimleriBagla kilit açılana dek bekler.
func yetimSatiriniVeDosyayiSil(ctx context.Context, db *sql.DB, bid int64, sk, dosya string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `DELETE FROM backups WHERE id=? AND `+yetimKosulu, bid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("yedek %d: %w", bid, errYetimDegil)
	}
	if e := os.Remove(filepath.Join(yedekKok, sk, dosya)); e != nil && !os.IsNotExist(e) {
		return &yetimSilHatasi{err: fmt.Errorf("yedek %d: yerel dosya silinemedi, kayıt korundu: %w", bid, e)}
	}
	return tx.Commit()
}

// yetimSilinmekte — uzak silmesi SÜREN yetim satır kimlikleri (süreç içi; bkz. dosya başlığı O2).
var yetimSilinmekte = struct {
	sync.Mutex
	m map[int64]struct{}
}{m: map[int64]struct{}{}}

// yetimSilmeBasla — id'yi "silinmekte" olarak kaydeder. Zaten kayıtlıysa (başka bir silme aynı satırı işliyor) ok=false.
// birak: kaydı kaldırır (silme bitince/başarısız olunca MUTLAKA çağrılır).
func yetimSilmeBasla(id int64) (birak func(), ok bool) {
	yetimSilinmekte.Lock()
	defer yetimSilinmekte.Unlock()
	if _, var1 := yetimSilinmekte.m[id]; var1 {
		return nil, false
	}
	yetimSilinmekte.m[id] = struct{}{}
	return func() {
		yetimSilinmekte.Lock()
		delete(yetimSilinmekte.m, id)
		yetimSilinmekte.Unlock()
	}, true
}

// yetimSilinmektekiler — şu an silinmekte olan satır kimlikleri (anlık görüntü, artan).
func yetimSilinmektekiler() []int64 {
	yetimSilinmekte.Lock()
	defer yetimSilinmekte.Unlock()
	out := make([]int64, 0, len(yetimSilinmekte.m))
	for id := range yetimSilinmekte.m {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// yetimHalaYetim — satır HÂLÂ yetim mi? Kilitli okuma (FOR UPDATE) + hemen commit: süren bir bağlamanın (satırı
// kilitlemiş olabilir) commit'ini bekler, böylece "yetim" cevabı bağlamayla yarışmaz. Bağlandıysa errYetimDegil.
func yetimHalaYetim(ctx context.Context, db *sql.DB, bid int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var id int64
	switch e := tx.QueryRowContext(ctx, `SELECT id FROM backups WHERE id=? AND `+yetimKosulu+` FOR UPDATE`, bid).Scan(&id); {
	case errors.Is(e, sql.ErrNoRows):
		return fmt.Errorf("yedek %d: %w", bid, errYetimDegil)
	case e != nil:
		return e
	}
	return tx.Commit()
}

// yetimYedegiSil — BİR yetim yedeği kalıcı siler: kayıt defterine yaz (aynı satırı başka bir silme işliyorsa atla) →
// satır HÂLÂ yetim mi (kilitli okuma) → uzak (anlık görüntü hedefi + genel hedef; yetimUzakSureSiniri ile sınırlı) →
// satır + yerel dosya (tek işlemde, yetimSatiriniVeDosyayiSil). Uzak adımın sürdüğü boyunca bağlama bu satırı BAĞLAMAZ
// (dosya başlığı O2): canlı yedeğin uzak kopyası silinmez. izID: satırın silme izi (anlık görüntü anahtarı).
// uzagiYoksay: uzak adımı atlar. Uzak silinemezse satır + dosya KALIR (yetimSilHatasi{uzak}, parola maskeli). Bitince
// izID'nin yetimi kalmadıysa anlık görüntü silinir. Güvenlik: sk regex, dosya adı tek bileşen + .tar.gz, sk dizini
// symlink değil, satır HÂLÂ yetim (araya giren bağlama canlı yedeği silmesin: errYetimDegil).
func yetimYedegiSil(ctx context.Context, db *sql.DB, bid int64, izID, sk, dosya string, uzagiYoksay bool) error {
	if !silinenSKGecerli(sk) || !dosyaAdiGuvenli(dosya) {
		return fmt.Errorf("yedek %d: güvensiz sk/dosya adı, atlandı", bid)
	}
	if !yedekDizinSilinebilir(sk) {
		return fmt.Errorf("yedek %d: yedek dizini güvenli değil (symlink?), atlandı", bid)
	}
	birak, ok := yetimSilmeBasla(bid)
	if !ok {
		return fmt.Errorf("yedek %d (başka bir silme işliyor): %w", bid, errYetimDegil)
	}
	defer birak()
	if err := yetimHalaYetim(ctx, db, bid); err != nil {
		return err
	}
	if !uzagiYoksay {
		d, err := yetimHedefOku(ctx, db, izID)
		if err != nil {
			return fmt.Errorf("yedek %d: uzak hedef anlık görüntüsü okunamadı: %w", bid, err)
		}
		uctx, iptal := context.WithTimeout(ctx, yetimUzakSureSiniri)
		e := maskeliUzakHata(yetimUzakSil(uctx, db, d, dosya), d)
		iptal()
		if e != nil {
			return &yetimSilHatasi{uzak: true, err: e}
		}
	}
	if err := yetimSatiriniVeDosyayiSil(ctx, db, bid, sk, dosya); err != nil {
		return err
	}
	yetimHedefBittiyse(ctx, db, izID)
	return nil
}

// silinenSupurKilit: aynı anda tek süpürme.
var silinenSupurKilit sync.Mutex

// supurKayit — süpürmenin işleyeceği süresi dolmuş yetim satır.
type supurKayit struct {
	id, boyut int64
	izID, sk  string
	dosya     string
}

// supurPartisi — imleçten (id > imlec) sonraki en çok silinenSupurLimit süresi dolmuş yetim satır.
func supurPartisi(ctx context.Context, db *sql.DB, imlec int64) ([]supurKayit, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, iz_id, sistem_kullanici, dosya, boyut_b FROM backups
		 WHERE id>? AND son_kullanma IS NOT NULL AND son_kullanma<=NOW() AND `+yetimKosulu+` ORDER BY id LIMIT ?`, imlec, silinenSupurLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var liste []supurKayit
	for rows.Next() {
		var k supurKayit
		if rows.Scan(&k.id, &k.izID, &k.sk, &k.dosya, &k.boyut) != nil {
			continue
		}
		liste = append(liste, k)
	}
	return liste, rows.Err()
}

// boyutMetni — özet günlüğü/denetim için insan okunur boyut.
func boyutMetni(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// SilinenSupur — süresi dolan yetim yedekleri temizler (04:00 turu). Önce sahipsiz satırları işaretler (S2) ve
// artık anlık görüntüleri temizler. Süre bütçesi (ctx) bitene ya da süresi dolan kayıt kalmayana dek
// silinenSupurLimit'lik partiler halinde döner (O7). Uzak silinemeyen satır kalır, ertesi gün tekrar denenir
// (aynı turda imleç onu geçer: başarısız satır döngüyü kilitlemez). Tek özet günlüğü + tek denetim satırı yazar (D2).
func SilinenSupur(ctx context.Context, db *sql.DB) {
	if !silinenSupurKilit.TryLock() {
		return // zaten çalışıyor
	}
	defer silinenSupurKilit.Unlock()
	SilinenSahipsizIsaretle(ctx, db)
	yetimHedefArtiklariniTemizle(ctx, db)
	var silinen, hata, atlanan, denenmedi int
	var silinenB int64
	var imlec int64
	sureDoldu := false
	// Devre kesici: bir silme izinin (aynı hesabın; iz_id boşsa yalnız genel hedefi olan eski satırlar) uzak hedefi turda
	// bir kez ERİŞİLEMEZ çıktıysa (uzakHedefUlasilamiyor: çözümleme/bağlantı/kimlik/zaman aşımı) o izin kalan satırları
	// AYNI hatayı verecektir; her biri için 2 dk'ya varan lftp oturumları açmak 20 dk'lık bütçeyi ölü bir hedefin başında
	// eritir, ID'si daha büyük sağlıklı hesapların yetimleri hiç işlenmez. Kalanlar bu turda denenmez (korunur; ertesi tur
	// yeniden dener). Dosyaya özgü hatalar (izin vb.) kesiciyi AÇMAZ.
	uzakBozuk := map[string]bool{}
	for {
		if ctx.Err() != nil {
			sureDoldu = true
			break
		}
		liste, err := supurPartisi(ctx, db, imlec)
		if err != nil {
			log.Printf("yetim yedek süpürme: %v", err)
			break
		}
		if len(liste) == 0 {
			break
		}
		for _, k := range liste {
			if ctx.Err() != nil {
				sureDoldu = true
				break
			}
			imlec = k.id
			if uzakBozuk[k.izID] {
				denenmedi++
				continue
			}
			switch e := yetimYedegiSil(ctx, db, k.id, k.izID, k.sk, k.dosya, false); {
			case e == nil:
				silinen++
				silinenB += k.boyut
			case errors.Is(e, errYetimDegil):
				atlanan++ // sahibi bağladı: canlı yedek, dokunulmadı
			default:
				hata++
				var se *yetimSilHatasi
				if errors.As(e, &se) && se.uzak && uzakHedefUlasilamiyor(se.err) {
					uzakBozuk[k.izID] = true
				}
				log.Printf("yetim yedek %d (%s) temizlenemedi, kayıt korundu (yarın tekrar denenir): %v", k.id, k.dosya, e)
			}
		}
		if sureDoldu || len(liste) < silinenSupurLimit {
			break
		}
	}
	if sureDoldu {
		log.Printf("yetim yedek süpürme: süre doldu, kalanı sonraki turda")
	}
	if silinen == 0 && hata == 0 && atlanan == 0 && denenmedi == 0 {
		return
	}
	log.Printf("yetim yedek süpürme: %d yedek silindi (%s), %d silinemedi (korundu), %d atlandı (bağlanmış), %d denenmedi (aynı hesabın uzak hedefi hata verdi)", silinen, boyutMetni(silinenB), hata, atlanan, denenmedi)
	// D2: silmeler tek bir özet denetim satırı (kök kapsamı; arka plan işi → httpx.DenetimSistem).
	detay := fmt.Sprintf("%d yedek silindi (%s), %d silinemedi, %d bağlanmış olduğundan atlandı", silinen, boyutMetni(silinenB), hata, atlanan)
	if denenmedi > 0 {
		detay += fmt.Sprintf(", %d aynı hesabın uzak hedefi hata verdiği için denenmedi", denenmedi)
	}
	if sureDoldu {
		detay += "; süre doldu, kalanı sonraki turda"
	}
	httpx.DenetimSistem(db, 0, "sistem", "silinen-yedek.supur", "silinmiş hesap yedekleri", detay, 0, hata == 0)
}

// silinenOzet — yetim yedeklerin sayısı ve toplam boyutu (Ozet KPI'sı ve disk kapısı bildirimi için).
func silinenOzet(ctx context.Context, db *sql.DB) (adet int, boyut int64, err error) {
	err = db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(boyut_b),0) FROM backups WHERE `+yetimKosulu).Scan(&adet, &boyut)
	return adet, boyut, err
}

// ---------------------------------------------------------------- yönetici uçları

type silinenYedekDosya struct {
	ID        int64  `json:"id"`
	Dosya     string `json:"dosya"`
	BoyutB    int64  `json:"boyut_b"`
	Olusturma string `json:"olusturma"`
	Yerel     bool   `json:"yerel"`
}

// silinenSahip — bir sk'daki yetim yedeklerin ayrık (alan adı, bayi) çifti ve o çiftin dosya sayısı (O3).
type silinenSahip struct {
	AlanAdi     string `json:"alan_adi"`
	ResellerID  int64  `json:"reseller_id"`
	DosyaSayisi int    `json:"dosya_sayisi"`
}

type silinenGrup struct {
	SK                   string `json:"sk"`
	AlanAdi              string `json:"alan_adi"`
	ResellerID           int64  `json:"reseller_id"`
	Silinme              string `json:"silinme"`
	SonKullanma          string `json:"son_kullanma"` // boş = otomatik silinmez
	DosyaSayisi          int    `json:"dosya_sayisi"`
	BoyutB               int64  `json:"boyut_b"`
	YerelVar             bool   `json:"yerel_var"`
	BaglanabilirDomainID *int64 `json:"baglanabilir_domain_id"`
	// BaglanabilirAlanAdi: bağlanabilir domainin alan adı (arayüz iki adı yan yana gösterir).
	BaglanabilirAlanAdi string `json:"baglanabilir_alan_adi,omitempty"`
	// BaglanabilirFarkli: hedef domainin alan adı VEYA sahibi (reseller) yedeklerinkinden farklı mı?
	// Doğruysa elle bağlama açık onay (zorla) ister: sk eşitliği aynı müşteri demek DEĞİLDİR
	// (girgin.com ve girgin.net ikisi de c_girgin alabilir).
	BaglanabilirFarkli bool `json:"baglanabilir_farkli"`
	// Sahipler: gruptaki TÜM ayrık (alan adı, bayi) çiftleri. Silme/bağlama gruptaki hepsini etkiler;
	// arayüz yalnız ilk satırı değil bunları gösterir (O3).
	Sahipler []silinenSahip      `json:"sahipler"`
	Yedekler []silinenYedekDosya `json:"yedekler"`
	// gruptaki ayrık (küçük harf alan adı, reseller) çiftleri ve sayıları; JSON'a girmez.
	sahipSayac map[yetimSahip]*silinenSahip
}

// yetimSahip — yetim satırın (alan adı, reseller) kimliği; alan adı küçük harf + trim.
type yetimSahip struct {
	alan string
	rid  int64
}

func yetimSahibi(alan string, rid int64) yetimSahip {
	return yetimSahip{alan: strings.ToLower(strings.TrimSpace(alan)), rid: rid}
}

// sahipSirala — deterministik sıra: dosya sayısı çoktan aza, sonra alan adı, sonra bayi.
func sahipSirala(l []silinenSahip) {
	sort.SliceStable(l, func(i, j int) bool {
		if l[i].DosyaSayisi != l[j].DosyaSayisi {
			return l[i].DosyaSayisi > l[j].DosyaSayisi
		}
		if a, b := strings.ToLower(l[i].AlanAdi), strings.ToLower(l[j].AlanAdi); a != b {
			return a < b
		}
		return l[i].ResellerID < l[j].ResellerID
	})
}

// yetimSahipleri — sk'nın yetim yedeklerindeki TÜM ayrık (alan adı, bayi) çiftleri ve dosya sayıları (sıralı).
func yetimSahipleri(ctx context.Context, db *sql.DB, sk string) ([]silinenSahip, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT MIN(alan_adi), reseller_id, COUNT(*) FROM backups WHERE sistem_kullanici=? AND `+yetimKosulu+`
		 GROUP BY LOWER(TRIM(alan_adi)), reseller_id`, sk)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []silinenSahip{}
	for rows.Next() {
		var s silinenSahip
		if rows.Scan(&s.AlanAdi, &s.ResellerID, &s.DosyaSayisi) != nil {
			continue
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sahipSirala(out)
	return out, nil
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
		// created_at NULL olabilir (DATE_FORMAT(NULL) → NULL): Scan hatası satırı ATLATIR, sahibi 0 (= kök) yazılmaz.
		var olusturma sql.NullString
		if rows.Scan(&y.ID, &sk, &alan, &rid, &y.Dosya, &y.BoyutB, &olusturma, &silinme, &sonK) != nil {
			continue
		}
		y.Olusturma = olusturma.String
		g := idx[sk]
		if g == nil {
			if len(out) >= silinenGrupLimit {
				continue
			}
			g = &silinenGrup{SK: sk, AlanAdi: alan, ResellerID: rid, Silinme: silinme, SonKullanma: sonK, Yedekler: []silinenYedekDosya{},
				Sahipler: []silinenSahip{}, sahipSayac: map[yetimSahip]*silinenSahip{}}
			idx[sk] = g
			out = append(out, g)
		}
		ss := yetimSahibi(alan, rid)
		if s := g.sahipSayac[ss]; s != nil {
			s.DosyaSayisi++
		} else {
			g.sahipSayac[ss] = &silinenSahip{AlanAdi: alan, ResellerID: rid, DosyaSayisi: 1}
		}
		y.Yerel = yerelDuzDosya(sk, y.Dosya)
		g.YerelVar = g.YerelVar || y.Yerel
		g.BoyutB += y.BoyutB
		g.Yedekler = append(g.Yedekler, y)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		log.Printf("silinen yedek: liste (tarama): %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, "liste okunamadı")
		return
	}
	rows.Close()
	for _, g := range out {
		g.DosyaSayisi = len(g.Yedekler)
		for _, s := range g.sahipSayac {
			g.Sahipler = append(g.Sahipler, *s)
		}
		sahipSirala(g.Sahipler)
		var did, hrid int64
		var halan string
		if silinenSKGecerli(g.SK) &&
			h.DB.QueryRowContext(r.Context(), `SELECT id, alan_adi, COALESCE(reseller_id,0) FROM domains WHERE sistem_kullanici=? ORDER BY id LIMIT 1`, g.SK).
				Scan(&did, &halan, &hrid) == nil {
			g.BaglanabilirDomainID = &did
			g.BaglanabilirAlanAdi = halan
			hedefSahip := yetimSahibi(halan, hrid)
			for sh := range g.sahipSayac {
				if sh != hedefSahip {
					g.BaglanabilirFarkli = true
				}
			}
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
// süreyi beklemeden kalıcı siler. Silinemeyen satır + dosya KALIR (409).
//
// 409 gövdesinde `uzagi_yoksay` YALNIZ başarısızlıkların hepsi UZAK silmeden geliyorsa true'dur (uzak depo
// ulaşılamıyor / lftp yok): arayüz "uzak kopyayı yoksay" önerir. Yerel dosya silinemediyse (ya da başka bir
// hata) false'tur ve hata metni onu söyler — uzağı yoksaymak onu çözmez. Kısmi başarısızlıkta da denetim
// kaydı yazılır (silinen + kalan, D2). Araya bağlanan (artık yetim olmayan) satırlar atlanır, hata sayılmaz.
func (h *Handlers) SilinenSil(w http.ResponseWriter, r *http.Request) {
	sk := chi.URLParam(r, "sk")
	if !silinenSKGecerli(sk) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz sistem kullanıcısı")
		return
	}
	yoksay := r.URL.Query().Get("uzagi_yoksay") == "1"
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, iz_id, dosya FROM backups WHERE sistem_kullanici=? AND `+yetimKosulu+` ORDER BY id LIMIT ?`, sk, silinenSatirLimit)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "kayıtlar okunamadı")
		return
	}
	type kayit struct {
		id    int64
		izID  string
		dosya string
	}
	var liste []kayit
	for rows.Next() {
		var k kayit
		if rows.Scan(&k.id, &k.izID, &k.dosya) == nil {
			liste = append(liste, k)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		log.Printf("silinen yedek %s silme: kayıtlar taranamadı: %v", sk, err)
		httpx.WriteError(w, http.StatusInternalServerError, "kayıtlar okunamadı")
		return
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
	silinen, atlanan, uzakKalan, yerelKalan, digerKalan := 0, 0, 0, 0, 0
	uzakBozuk := map[string]bool{} // devre kesici (SilinenSupur ile aynı): aynı izin uzak hedefi erişilemezse kalanı denenmez
	for _, k := range liste {
		if uzakBozuk[k.izID] {
			uzakKalan++
			continue
		}
		e := yetimYedegiSil(kctx, h.DB, k.id, k.izID, sk, k.dosya, yoksay)
		if e == nil {
			silinen++
			continue
		}
		if errors.Is(e, errYetimDegil) {
			atlanan++ // sahibi araya bağladı: canlı yedek, dokunulmadı
			continue
		}
		var sh *yetimSilHatasi
		switch {
		case errors.As(e, &sh) && sh.uzak:
			uzakKalan++
			if uzakHedefUlasilamiyor(sh.err) {
				uzakBozuk[k.izID] = true
			}
		case errors.As(e, &sh):
			yerelKalan++
		default:
			digerKalan++
		}
		if ilk == nil {
			ilk = e
		}
	}
	if kalan := uzakKalan + yerelKalan + digerKalan; kalan > 0 {
		log.Printf("silinen yedek %s silme: %d/%d silinemedi (uzak=%d yerel=%d diğer=%d): %v", sk, kalan, len(liste), uzakKalan, yerelKalan, digerKalan, ilk)
		neden := "beklenmeyen hata"
		switch {
		case yerelKalan == 0 && digerKalan == 0:
			neden = "uzak depodaki kopya silinemedi"
		case uzakKalan == 0 && digerKalan == 0:
			neden = "sunucudaki yerel dosya silinemedi"
		case digerKalan == 0:
			neden = "uzak depodaki kopya ve sunucudaki yerel dosya silinemedi"
		}
		httpx.Denetim(h.DB, r, uid, kul, "silinen-yedek.temizle", sk,
			fmt.Sprintf("kısmi: silinen=%d kalan=%d (%s) uzak_yoksay=%v", silinen, kalan, neden, yoksay), 0, false)
		httpx.WriteJSON(w, http.StatusConflict, map[string]any{
			"ok": false, "hata": fmt.Sprintf("%d yedek silinemedi — %s (kayıt ve dosya korundu): %v", kalan, neden, ilk),
			"silinen": silinen, "uzagi_yoksay": yerelKalan == 0 && digerKalan == 0 && uzakKalan > 0,
		})
		return
	}
	// Boşalan sk dizini: yalnız canlı bir hosting sk'yı kullanmıyorsa ve dizin boşsa (os.Remove dolu dizine dokunmaz).
	var canli int
	_ = h.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM domains WHERE sistem_kullanici=?`, sk).Scan(&canli)
	if canli == 0 && yedekDizinGuvenli(sk) {
		_ = os.Remove(filepath.Join(yedekKok, sk))
	}
	detay := fmt.Sprintf("yedek=%d uzak_yoksay=%v", silinen, yoksay)
	if atlanan > 0 {
		detay += fmt.Sprintf(" atlanan=%d (bağlanmış)", atlanan)
	}
	httpx.Denetim(h.DB, r, uid, kul, "silinen-yedek.temizle", sk, detay, 0, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "silinen": silinen})
}

// baglaKosulu — bağlanacak yetim satırların SQL koşulu. Onaysız yolda (farkli=false) alan adı + sahip de
// koşula girer: kontrol ile bağlama arasında bir satırın sahibi değişse bile farklı müşterinin yedeği
// onaysız bağlanmaz (savunma derinliği). farkli=true: yönetici açık onay verdi, yalnız sk.
func baglaKosulu(sk string, farkli bool, hedef yetimSahip) (string, []any) {
	kosul, args := `sistem_kullanici=?`, []any{sk}
	if !farkli {
		kosul += ` AND LOWER(TRIM(alan_adi))=? AND reseller_id=?`
		args = append(args, hedef.alan, hedef.rid)
	}
	return kosul, args
}

// sahipMetni — denetim/hata metni için "alan (bayi=N, K dosya)" biçimi.
func sahipMetni(l []silinenSahip) string {
	p := make([]string, 0, len(l))
	for _, s := range l {
		p = append(p, fmt.Sprintf("%s (bayi=%d, %d dosya)", s.AlanAdi, s.ResellerID, s.DosyaSayisi))
	}
	return strings.Join(p, ", ")
}

// SilinenBagla: POST /admin/silinen-yedekler/{sk}/bagla {"domain_id":N, "zorla":bool} — yönetici kararıyla
// yetim yedekleri canlı bir domaine bağlar (hedefin sk'sı eşit olmalı: aksi 400).
//
// 🔴 sk eşitliği AYNI MÜŞTERİ demek değildir: sk alan adının İLK ETİKETİNDEN türer (girgin.com ve
// girgin.net ikisi de c_girgin alabilir). Restore dosyaları + kullanicilar.sql içindeki DB parola
// özetlerini hedef hesaba yazar → farklı müşterinin yedeğini sessizce bağlamak veri sızdırır. Hedefin
// alan adı (küçük harf) VEYA sahibi (reseller_id) yetim satırlarınkinden farklıysa ve zorla!=true
// → 409 {alan_adi_farkli:true, farklilar:[…tüm farklı çiftler…], …} ve HİÇBİR satıra dokunulmaz.
// zorla=true açık onaydır: gruptaki TÜM sahipleri bağlar ve denetime TÜM çiftleri yazar (O3).
func (h *Handlers) SilinenBagla(w http.ResponseWriter, r *http.Request) {
	sk := chi.URLParam(r, "sk")
	if !silinenSKGecerli(sk) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz sistem kullanıcısı")
		return
	}
	var req struct {
		DomainID int64 `json:"domain_id"`
		Zorla    bool  `json:"zorla"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || req.DomainID <= 0 {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	var hedefSK, alan string
	var hedefRID int64
	if err := h.DB.QueryRowContext(r.Context(), `SELECT sistem_kullanici, alan_adi, COALESCE(reseller_id,0) FROM domains WHERE id=?`, req.DomainID).
		Scan(&hedefSK, &alan, &hedefRID); err != nil {
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
	hedefSahip := yetimSahibi(alan, hedefRID)
	// Yetim satırların TÜM (alan adı, sahip) çiftleri: HERHANGİ biri hedeften farklıysa onay gerekir.
	sahipler, err := yetimSahipleri(r.Context(), h.DB, sk)
	if err != nil {
		log.Printf("silinen yedek bağla (%s): sahip okunamadı: %v", sk, err)
		httpx.WriteError(w, http.StatusInternalServerError, "bağlanamadı")
		return
	}
	farklilar := []silinenSahip{}
	for _, s := range sahipler {
		if yetimSahibi(s.AlanAdi, s.ResellerID) != hedefSahip {
			farklilar = append(farklilar, s)
		}
	}
	farkli := len(farklilar) > 0
	if farkli && !req.Zorla {
		httpx.WriteJSON(w, http.StatusConflict, map[string]any{
			"ok": false, "alan_adi_farkli": true,
			"hata":              "yedeğin alan adı/sahibi hedef hostingle aynı değil: açık onay (zorla) gerekir",
			"farklilar":         farklilar,
			"yedek_alan_adi":    farklilar[0].AlanAdi,
			"hedef_alan_adi":    alan,
			"yedek_reseller_id": farklilar[0].ResellerID,
			"hedef_reseller_id": hedefRID,
		})
		return
	}
	// Bağlanan yedekler tip='kurtarma' olur (pruneOld/pruneManuelYedek dokunmaz) ve eski uzak hedef
	// anlık görüntüsü (sahip aynıysa, domainin kendi hedefi yoksa) domaine aktif=0, parolasız geri yüklenir.
	// Onaysız yolda koşul, kontrol ile bağlama arasındaki araya girmeye karşı satır düzeyinde de uygulanır.
	kosul, kargs := baglaKosulu(sk, farkli, hedefSahip)
	n, err := yetimleriBagla(r.Context(), h.DB, req.DomainID, kosul, kargs...)
	if err != nil {
		log.Printf("silinen yedek bağla (%s): %v", sk, err)
		httpx.WriteError(w, http.StatusInternalServerError, "bağlanamadı")
		return
	}
	uid, kul := middleware.Aktor(r)
	detay := fmt.Sprintf("sk=%s baglanan=%d", sk, n)
	if farkli {
		detay += fmt.Sprintf(" alan adı/sahip farkı onaylandı: yedek sahipleri [%s] → hedef=%s (bayi=%d)", sahipMetni(farklilar), alan, hedefRID)
	}
	// Kapsam: hedef hostingin sahibi bayi (users.id; 0 = kök) — domain kimliği DEĞİL (D2).
	httpx.Denetim(h.DB, r, uid, kul, "silinen-yedek.bagla", alan, detay, hedefRID, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "baglanan": n})
}

// SilinenAyarGet: GET /admin/silinen-yedekler/ayar → {gun: null|N}  (null = hostingin yedek slotu)
func (h *Handlers) SilinenAyarGet(w http.ResponseWriter, r *http.Request) {
	ayar, err := silinenAyar(r.Context(), h.DB)
	if err != nil {
		// 200+null "ayarsız" demektir; okunamayan ayarı öyle göstermek arayüzün kayıtlı değeri (ör. 0) sessizce
		// sıfırlamasına yol açardı. Arayüz okuma hatasında Kaydet'i kapatır.
		log.Printf("silinen yedek ayar okunamadı: %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, "ayar okunamadı")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"gun": silinenAyarJSON(ayar), "en_az_slot_gun": silinenEnAzGun})
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
