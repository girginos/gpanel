package backups

// Silinen hosting/bayi yedekleri ("yetim yedek").
//
// Karar (kullanıcı, 2026-10-08): hosting/bayi silme YEDEKLERDEN BAĞIMSIZDIR. Silme yolu yedek
// dizinine/uzak kopyalara hiç dokunmaz ve yedekler yüzünden ASLA başarısız olmaz/beklemez.
//
// Tasarım: backups.domain_id → domains(id) FK'si KALKTI (migrations/0217): domain silinince yedeğin
// DB satırı yerinde kalır, YEDEK SAYFASINDAN geri yüklenebilir. Silme `DELETE FROM domains`
// başarılı olunca TEK best-effort UPDATE ile satırlara alan_adi / sistem_kullanici / reseller_id /
// silinme / son_kullanma yazar ve satırı domain_id=0'a taşır (SilinenIz.Yaz). Süre
// cp_ayarlar.silinen_yedek_gun: boş = hostingin yedek slotu (en az 7 gün), 0 = otomatik silme yok,
// N = sabit gün (≤3650).
//
//	Yetim yedek = domain_id=0 AND silinme IS NOT NULL
//
// 🔴 S4 — domain_id YENİDEN KULLANIM KORUMASI: yetim satır eski domain_id'yi TAŞIMAZ, `eski_domain_id`
// kolonunda saklar; domain_id=0 olur (domains'te 0 id'li satır olamaz). Böylece domain id'si bir gün
// yeniden kullanılsa bile `domain_id=?` ya da `JOIN domains` yapan HİÇBİR canlı sorgu (yedek listesi,
// restore, budama, uzak gönderim, kota) eski müşterinin yedeğini görmez/silmez/geri yüklemez.
//
// 🔴 S1 — UZAK HEDEF ANLIK GÖRÜNTÜSÜ: domaine özel FTP/SFTP hedefi domain silinince CASCADE ile gider
// (fk_bdest_domain). Silme anında hedefin ŞİFRELİ ham kaydı backup_yetim_hedefler'e (eski_domain_id
// UNIQUE) kopyalanır; yetim yedeğin uzak kopyası bu anlık görüntüyle silinir. Bir eski_domain_id'nin
// TÜM yetimleri bitince (silindi/bağlandı) satır silinir, parola tabloda kalmaz. Bağlamada domainin
// kendi hedefi yoksa anlık görüntü o domaine (aktif=0) geri yüklenir.
//
// 🔴 S2 — SAHİPSİZ İŞARETLEME: silme izi yazılamazsa (UPDATE hatası/süreç çöktü) satır `domain_id>0,
// silinme NULL, domain yok` kalır. SilinenSahipsizIsaretle (panel açılışı + 04:00 turu) bunları dosya
// adından sk türeterek işaretler ve normal yaşam döngüsüne sokar.
//
// 🔴 S3 — BAĞLANAN YEDEK 'kurtarma' TİPİNE GEÇER: pruneOld yalnız tip='oto', pruneManuelYedek yalnız
// tip='tam' siler; 'kurtarma' ikisinin de dışındadır → eski yedekler domainin yeni saklama sayımına
// girip budanmaz. Otomatik süre dolumu YOKTUR (bilinçli); kullanıcı elle siler. 'kurtarma' ayrıca uzak
// gönderim adayı DEĞİLDİR (bkz. uzak.KokYedegiMi): eski yedeği kullanıcı istemeden uzağa itmek ve
// ardından yerel kopyayı silmek (yerel-silme) beklenmedik olurdu.
//
// 🔴 Yetim satırlar HİÇBİR canlı akışa girmemeli ve kendi başına silinmemeli. Yetimi yalnız şunlar
// siler: 04:00 süpürmesi (süresi dolanı), yönetici DELETE ucu.
//
// 🔴 Aynı sk/alan adı yeniden açılırsa: AYNI alan adı + AYNI sahip (reseller_id) + AYNI sk ise yetimler
// domainin yedek sayfası açılınca otomatik bağlanır (yetimleriOtoBagla); farklıysa YALNIZ yönetici
// elle bağlar (başka müşterinin yedeği sızmasın).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/backups/hedef"
	"girginospanel/internal/gizli"
	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"

	"github.com/go-chi/chi/v5"
)

// yedekKok: saklama işlemlerinin kökü (testte geçici dizine çevrilir).
var yedekKok = BackupRoot

const (
	silinenAyarKey  = "silinen_yedek_gun"
	silinenEnAzGun  = 7
	silinenEnCokGun = 3650
	// silinenSupurLimit: tek turda en çok kaç yetim yedek temizlenir (kalanı ertesi gün).
	silinenSupurLimit = 200
	silinenGrupLimit  = 500
	silinenSatirLimit = 20000
	// sahipsizLimit: tek turda en çok kaç sahipsiz satır işaretlenir (kalanı sonraki turda).
	sahipsizLimit = 5000
	// TipKurtarma: yetimken domaine bağlanan yedeklerin tipi (pruneOld/pruneManuelYedek dokunmaz).
	TipKurtarma = "kurtarma"
)

// yetimKosulu: yetim yedek SQL koşulu (backups tablosu "backups" adıyla, takma adsız).
// domain_id=0: domains'te 0 id'li satır olamaz → her JOIN domains yetimi doğal olarak eler.
const yetimKosulu = `domain_id=0 AND silinme IS NOT NULL`

// izYenidenDenemeBekleme: iz UPDATE'i hata verirse tek yeniden deneme öncesi bekleme (testte kısaltılır).
var izYenidenDenemeBekleme = 750 * time.Millisecond

// yetimUzakSil: yetim yedeğin uzak kopyasını silen işlev (testte sahte enjekte edilir; ağa çıkmaz).
var yetimUzakSil = yedekUzaktanSilHedefle

// silinenAyar: cp_ayarlar'daki ham değer ("" = ayarsız → slot).
func silinenAyar(ctx context.Context, db *sql.DB) string {
	var v string
	if err := db.QueryRowContext(ctx, `SELECT deger FROM cp_ayarlar WHERE anahtar=?`, silinenAyarKey).Scan(&v); err != nil {
		return ""
	}
	return strings.TrimSpace(v)
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

// yedekDosyaSKRe: yedek dosya adından sk türetme (<sk>-YYYYMMDD-HHMMSS.tar.gz). Eski bash zamanlayıcısının
// kaynak eki (-auto / -planli) de kabul edilir; sk karakter kümesinde '-' olmadığından belirsizlik yok.
var yedekDosyaSKRe = regexp.MustCompile(`^(c_[a-z0-9_]{1,40})-\d{8}-\d{6}(?:-(?:auto|planli))?\.tar\.gz$`)

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

// yetimHedefHam — silinecek domainin uzak hedefinin ŞİFRELİ ham kaydı (parola gizli.SaklaBagli çıktısı,
// aynen kopyalanır; çözme yalnız silme anında yetimHedefOku'da).
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
	hedef       *yetimHedefHam // domaine özel uzak hedef (yoksa nil)
}

// SilinenIzHazirla — domains satırı gitmeden ÖNCE çağrılır; YALNIZ DB okur (dizine dokunmaz).
// Geçersiz sk/id ya da hatada nil döner; nil iz Yaz'da yok sayılır. Panik yutulur.
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
	ctx, iptal := context.WithTimeout(ctx, 10*time.Second)
	defer iptal()
	iz = &SilinenIz{alanAdi: alanAdi, sk: sk, domainID: domainID, slot: 7}
	_ = db.QueryRowContext(ctx, `SELECT COALESCE(reseller_id,0), COALESCE(backup_retention,7) FROM domains WHERE id=?`, domainID).
		Scan(&iz.resellerID, &iz.slot)
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
// satırlarını yetim olarak işaretler. Hata vermez/engellemez, 10 sn zaman aşımı, panik yutulur.
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
	ctx, iptal := context.WithTimeout(ctx, 10*time.Second)
	defer iptal()
	// S1: anlık görüntü UPDATE'ten ÖNCE — UPDATE çökerse satırlar S2 süpürmesiyle domain_id>0'dan
	// eski_domain_id'ye taşınır ve bu anlık görüntü yine bulunur.
	if iz.hedef != nil {
		iz.hedefAnlikGoruntusu(ctx, db)
	}
	gun := silinenSaklamaGunu(silinenAyar(ctx, db), iz.slot)
	// S4: satır domain_id=0'a taşınır, eski id eski_domain_id'de kalır.
	q := `UPDATE backups SET alan_adi=?, sistem_kullanici=?, reseller_id=?, eski_domain_id=?, domain_id=0, silinme=NOW(), son_kullanma=NULL WHERE domain_id=?`
	args := []any{iz.alanAdi, iz.sk, iz.resellerID, iz.domainID, iz.domainID}
	if gun > 0 { // 0 = otomatik silme yok (son_kullanma NULL)
		q = `UPDATE backups SET alan_adi=?, sistem_kullanici=?, reseller_id=?, eski_domain_id=?, domain_id=0, silinme=NOW(), son_kullanma=DATE_ADD(NOW(), INTERVAL ? DAY) WHERE domain_id=?`
		args = []any{iz.alanAdi, iz.sk, iz.resellerID, iz.domainID, gun, iz.domainID}
	}
	// S2: geçici hata (kilit/kısa kopma) için TEK yeniden deneme.
	var err error
	for deneme := 1; deneme <= 2; deneme++ {
		if _, err = db.ExecContext(ctx, q, args...); err == nil {
			return nil
		}
		if deneme == 1 {
			log.Printf("🔴 silinen yedek iz UPDATE hatası (%s), bir kez daha denenecek: %v", iz.alanAdi, err)
			select {
			case <-ctx.Done():
				return err
			case <-time.After(izYenidenDenemeBekleme):
			}
		}
	}
	return err
}

// hedefAnlikGoruntusu — domainin uzak hedefini backup_yetim_hedefler'e KALICI kopyalar (yalnız domainin
// yedek satırı varsa). Hata silmeyi/izi ENGELLEMEZ; yalnız loglanır (parola maskeli). Aynı eski_domain_id
// için satır zaten varsa DOKUNMAZ (önceki bir domain kimliğinin yetimleri hâlâ onu kullanıyor olabilir).
func (iz *SilinenIz) hedefAnlikGoruntusu(ctx context.Context, db *sql.DB) {
	var var1 int
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM backups WHERE domain_id=?)`, iz.domainID).Scan(&var1); err != nil || var1 != 1 {
		return
	}
	h := iz.hedef
	if _, err := db.ExecContext(ctx,
		`INSERT INTO backup_yetim_hedefler (eski_domain_id, tip, host, port, kullanici, parola, uzak_dizin) VALUES (?,?,?,?,?,?,?)
		 ON DUPLICATE KEY UPDATE eski_domain_id=eski_domain_id`,
		iz.domainID, h.tip, h.host, h.port, h.kullanici, h.parola, h.uzakDizin); err != nil {
		log.Printf("🔴 silinen yedek: uzak hedef anlık görüntüsü yazılamadı (%s; yetim yedeklerin uzak kopyaları otomatik silinemeyecek): %s",
			iz.alanAdi, hedef.MaskeleHata(err, h.sirlar()...))
	}
}

// SilinenIsaretle — Hazirla + Yaz (domain satırı ZATEN silindiyse; testler/ara çağıranlar için).
func SilinenIsaretle(ctx context.Context, db *sql.DB, domainID int64, alanAdi, sk string) {
	SilinenIzHazirla(ctx, db, domainID, alanAdi, sk).Yaz(ctx, db)
}

// sahipsizSatiriIsaretle — TEK sahipsiz satırı yetim yapar: alan_adi/sk/reseller_id eskiden doldurulmuşsa
// (göç yedeklemesi) KORUNUR, boşsa sk yazılır; domain_id=0'a taşınır, eski id eski_domain_id'ye yazılır.
// UPDATE koşulu satırın HÂLÂ sahipsiz olduğunu (domain yok, silinme NULL) yeniden doğrular: seçim ile
// güncelleme arasında domain oluşturulursa (ya da satır işaretlenirse) canlı yedek yetim yapılmaz.
func sahipsizSatiriIsaretle(ctx context.Context, db *sql.DB, id int64, sk string, gun int) (bool, error) {
	set := `alan_adi=IF(alan_adi='',?,alan_adi), sistem_kullanici=IF(sistem_kullanici='',?,sistem_kullanici),
		eski_domain_id=domain_id, domain_id=0, silinme=NOW(), son_kullanma=NULL`
	args := []any{sk, sk}
	if gun > 0 {
		set = strings.Replace(set, "son_kullanma=NULL", "son_kullanma=DATE_ADD(NOW(), INTERVAL ? DAY)", 1)
		args = append(args, gun)
	}
	args = append(args, id)
	res, err := db.ExecContext(ctx, `UPDATE backups SET `+set+`
		WHERE id=? AND domain_id>0 AND silinme IS NULL AND NOT EXISTS (SELECT 1 FROM domains d WHERE d.id=backups.domain_id)`, args...)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// SilinenSahipsizIsaretle — S2: domain'i olmayan ama yetim işaretsiz (domain_id>0, silinme NULL) satırları
// yetim yapar: sk'yı dosya adından türetir (türetilemeyene DOKUNMAZ, sayıp loglar), alan_adi=sk,
// silinme=NOW(), son_kullanma ayara göre (slot bilinmiyor → ayar ya da 7 gün). Satır domain_id=0'a
// taşınır, eski id eski_domain_id'ye yazılır. İdempotent; panel açılışında ve 04:00 turunda çağrılır.
// Dönen: işaretlenen, türetilemeyen.
func SilinenSahipsizIsaretle(ctx context.Context, db *sql.DB) (isaretlenen, turetilemeyen int) {
	type satir struct {
		id, domainID int64
		dosya        string
	}
	rows, err := db.QueryContext(ctx,
		`SELECT b.id, b.domain_id, b.dosya FROM backups b
		 WHERE b.domain_id>0 AND b.silinme IS NULL AND NOT EXISTS (SELECT 1 FROM domains d WHERE d.id=b.domain_id)
		 ORDER BY b.id LIMIT ?`, sahipsizLimit)
	if err != nil {
		log.Printf("sahipsiz yedek işaretleme: %v", err)
		return 0, 0
	}
	var liste []satir
	for rows.Next() {
		var s satir
		if rows.Scan(&s.id, &s.domainID, &s.dosya) == nil {
			liste = append(liste, s)
		}
	}
	rows.Close()
	if len(liste) == 0 {
		return 0, 0
	}
	gun := silinenSaklamaGunu(silinenAyar(ctx, db), silinenEnAzGun)
	for _, s := range liste {
		if ctx.Err() != nil {
			break
		}
		sk, ok := skDosyaAdindan(s.dosya)
		if !ok {
			turetilemeyen++
			continue
		}
		ok2, e := sahipsizSatiriIsaretle(ctx, db, s.id, sk, gun)
		if e != nil {
			log.Printf("sahipsiz yedek %d işaretlenemedi: %v", s.id, e)
			continue
		}
		if ok2 {
			isaretlenen++
		}
	}
	if isaretlenen > 0 || turetilemeyen > 0 {
		log.Printf("sahipsiz yedek işaretleme: %d satır yetim yapıldı, %d satırın sk'sı dosya adından türetilemedi (dokunulmadı)", isaretlenen, turetilemeyen)
	}
	return isaretlenen, turetilemeyen
}

// yetimHedefOku — yetim yedeğin eski domain kimliğine ait uzak hedef anlık görüntüsü (yoksa nil, nil).
// Parola burada ÇÖZÜLÜR; Destination yalnız bellekte yaşar, asla döndürülmez/loglanmaz.
func yetimHedefOku(ctx context.Context, db *sql.DB, eskiID int64) (*Destination, error) {
	if eskiID <= 0 {
		return nil, nil
	}
	d := &Destination{Aktif: true}
	var ham string
	err := db.QueryRowContext(ctx,
		`SELECT tip, host, port, kullanici, parola, uzak_dizin FROM backup_yetim_hedefler WHERE eski_domain_id=?`, eskiID).
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

// yetimHedefBittiyse — eski_domain_id'nin yetim satırı kalmadıysa anlık görüntüyü (parola dahil) siler.
func yetimHedefBittiyse(ctx context.Context, db *sql.DB, eskiID int64) {
	if eskiID <= 0 {
		return
	}
	if _, err := db.ExecContext(ctx,
		`DELETE FROM backup_yetim_hedefler WHERE eski_domain_id=?
		 AND NOT EXISTS (SELECT 1 FROM backups b WHERE b.eski_domain_id=? AND b.domain_id=0)`, eskiID, eskiID); err != nil {
		log.Printf("yetim hedef anlık görüntüsü silinemedi (eski domain %d): %v", eskiID, err)
	}
}

// yetimHedefArtiklariniTemizle — hiçbir yetim satırın (ya da domain'i olmayan işaretsiz satırın) artık
// kullanmadığı, 1 saatten eski anlık görüntüleri siler (parola kalmasın). Yeni yazılmış satırlara
// dokunmamak için yaş koşulu var: anlık görüntü UPDATE'ten önce yazılır.
func yetimHedefArtiklariniTemizle(ctx context.Context, db *sql.DB) {
	res, err := db.ExecContext(ctx,
		`DELETE FROM backup_yetim_hedefler WHERE olusturma < NOW() - INTERVAL 1 HOUR
		 AND NOT EXISTS (SELECT 1 FROM backups b WHERE b.eski_domain_id=backup_yetim_hedefler.eski_domain_id AND b.domain_id=0)
		 AND NOT EXISTS (SELECT 1 FROM backups b2 WHERE b2.domain_id=backup_yetim_hedefler.eski_domain_id)`)
	if err != nil {
		log.Printf("yetim hedef artık temizliği: %v", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("yetim hedef artık temizliği: %d kullanılmayan anlık görüntü silindi", n)
	}
}

// yetimleriBagla — kosul (sk/alan/sahip) ile seçilen yetim yedekleri domaine bağlar: domain_id=<domain>,
// eski_domain_id=0, silinme/son_kullanma NULL, tip='kurtarma' (S3). Aynı işlemde (S1) eski domainin
// uzak hedef anlık görüntüsü, domainin KENDİ hedefi yoksa ve sahip (reseller) aynıysa domaine aktif=0
// olarak geri yüklenir; sonra anlık görüntü silinir (yetimi kalmadıysa). Bağlanan sayıyı döner.
func yetimleriBagla(ctx context.Context, db *sql.DB, domainID int64, kosul string, args ...any) (int64, error) {
	if domainID <= 0 {
		return 0, errors.New("geçersiz domain")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT eski_domain_id FROM backups WHERE `+kosul+` AND `+yetimKosulu+` FOR UPDATE`, args...)
	if err != nil {
		return 0, err
	}
	var eskiler []int64
	for rows.Next() {
		var e int64
		if rows.Scan(&e) == nil && e > 0 {
			eskiler = append(eskiler, e)
		}
	}
	rows.Close()
	var rid int64
	_ = tx.QueryRowContext(ctx, `SELECT COALESCE(reseller_id,0) FROM domains WHERE id=?`, domainID).Scan(&rid)
	// Hedef geri yükleme UPDATE'ten ÖNCE (yetim satırlar hâlâ eski_domain_id taşırken); hata bağlamayı bozmaz.
	geriYuklendi := map[int64]bool{}
	for _, e := range eskiler {
		var domainHedefi int
		if tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM backup_destinations WHERE domain_id=?)`, domainID).Scan(&domainHedefi) != nil {
			continue // tablo okunamıyor: anlık görüntü korunur
		}
		if domainHedefi == 1 {
			geriYuklendi[e] = true // domainin kendi hedefi var: anlık görüntü gereksiz
			continue
		}
		if _, ie := tx.ExecContext(ctx,
			`INSERT INTO backup_destinations (domain_id, tip, host, port, kullanici, parola, uzak_dizin, aktif)
			 SELECT ?, h.tip, h.host, h.port, h.kullanici, h.parola, h.uzak_dizin, 0 FROM backup_yetim_hedefler h
			 WHERE h.eski_domain_id=? AND EXISTS (SELECT 1 FROM backups b WHERE b.eski_domain_id=h.eski_domain_id AND b.domain_id=0 AND b.reseller_id=?)`,
			domainID, e, rid); ie != nil {
			log.Printf("yetim uzak hedef geri yüklenemedi (domain=%d eski=%d): %s", domainID, e, hedef.Maskele(ie.Error()))
			continue
		}
		geriYuklendi[e] = true
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE backups SET domain_id=?, eski_domain_id=0, silinme=NULL, son_kullanma=NULL, tip=? WHERE `+kosul+` AND `+yetimKosulu,
		append([]any{domainID, TipKurtarma}, args...)...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	for _, e := range eskiler {
		if !geriYuklendi[e] {
			continue
		}
		if _, de := tx.ExecContext(ctx,
			`DELETE FROM backup_yetim_hedefler WHERE eski_domain_id=?
			 AND NOT EXISTS (SELECT 1 FROM backups b WHERE b.eski_domain_id=? AND b.domain_id=0)`, e, e); de != nil {
			log.Printf("yetim hedef anlık görüntüsü silinemedi (eski domain %d): %v", e, de)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}

// yetimleriOtoBagla — domainin alan adı + sahibi (reseller_id) + sk'sı ile BİREBİR aynı olan yetim
// yedekleri bu domaine bağlar (lazy; yedek sayfası açılınca). Sahip/alan adı farklıysa dokunmaz.
// Bağlanan sayısını döner; hata sessizce loglanır (liste yine açılır).
func yetimleriOtoBagla(ctx context.Context, db *sql.DB, domainID int64) int64 {
	var alan, sk string
	var rid int64
	if err := db.QueryRowContext(ctx, `SELECT alan_adi, sistem_kullanici, COALESCE(reseller_id,0) FROM domains WHERE id=?`, domainID).
		Scan(&alan, &sk, &rid); err != nil || alan == "" || !silinenSKGecerli(sk) {
		return 0
	}
	n, err := yetimleriBagla(ctx, db, domainID, `sistem_kullanici=? AND alan_adi=? AND reseller_id=?`, sk, alan, rid)
	if err != nil {
		log.Printf("yetim yedek otomatik bağlama (domain=%d): %v", domainID, err)
		return 0
	}
	if n > 0 {
		log.Printf("yetim yedek otomatik bağlandı: domain=%d sk=%s adet=%d", domainID, sk, n)
	}
	return n
}

// maskeliUzakHata — uzak silme hatasından anlık görüntü parolasını temizler (lftp çıktısı/ağ hatası
// ayrıntıları API yanıtına ve loga gider). ErrLftpYok sabit mesajdır, aynen döner.
func maskeliUzakHata(err error, d *Destination) error {
	if err == nil || d == nil || errors.Is(err, ErrLftpYok) {
		return err
	}
	return errors.New(hedef.Maskele(err.Error(), d.Parola))
}

// yetimYedegiSil — BİR yetim yedeği kalıcı siler: uzak (anlık görüntü hedefi + genel hedef) → yerel dosya
// → DB satırı. eskiID: satırın eski_domain_id'si (anlık görüntü anahtarı). uzagiYoksay: uzak adımı atlar.
// Uzak silinemezse satır + dosya KALIR (hata döner, parola maskeli). Bitince eskiID'nin yetimi kalmadıysa
// anlık görüntü silinir. Güvenlik: sk regex, dosya adı tek bileşen + .tar.gz, sk dizini symlink değil,
// satır HÂLÂ yetim (araya giren bağlama canlı yedeği silmesin).
func yetimYedegiSil(ctx context.Context, db *sql.DB, bid, eskiID int64, sk, dosya string, uzagiYoksay bool) error {
	if !silinenSKGecerli(sk) || !dosyaAdiGuvenli(dosya) {
		return fmt.Errorf("yedek %d: güvensiz sk/dosya adı, atlandı", bid)
	}
	if !yedekDizinSilinebilir(sk) {
		return fmt.Errorf("yedek %d: yedek dizini güvenli değil (symlink?), atlandı", bid)
	}
	var hala int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM backups WHERE id=? AND `+yetimKosulu, bid).Scan(&hala); err != nil {
		return err
	}
	if hala != 1 {
		return fmt.Errorf("yedek %d: artık yetim değil (araya bağlama girdi), atlandı", bid)
	}
	if !uzagiYoksay {
		d, err := yetimHedefOku(ctx, db, eskiID)
		if err != nil {
			return fmt.Errorf("yedek %d: uzak hedef anlık görüntüsü okunamadı: %w", bid, err)
		}
		uzak := func() error { return maskeliUzakHata(yetimUzakSil(ctx, db, d, dosya), d) }
		if _, err := yedekKaydiniYokEtFn(ctx, db, sk, dosya, bid, uzak); err != nil {
			return err
		}
	} else {
		if e := os.Remove(filepath.Join(yedekKok, sk, dosya)); e != nil && !os.IsNotExist(e) {
			return e
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM backups WHERE id=? AND `+yetimKosulu, bid); err != nil {
			return err
		}
	}
	yetimHedefBittiyse(ctx, db, eskiID)
	return nil
}

// silinenSupurKilit: aynı anda tek süpürme.
var silinenSupurKilit sync.Mutex

// SilinenSupur — süresi dolan yetim yedekleri temizler (günde bir, 04:00 turu). Önce sahipsiz satırları
// işaretler (S2) ve artık anlık görüntüleri temizler. Uzak silinemezse satır kalır, ertesi gün tekrar
// denenir. Tur başına en çok silinenSupurLimit yedek.
func SilinenSupur(ctx context.Context, db *sql.DB) {
	if !silinenSupurKilit.TryLock() {
		return // zaten çalışıyor
	}
	defer silinenSupurKilit.Unlock()
	SilinenSahipsizIsaretle(ctx, db)
	yetimHedefArtiklariniTemizle(ctx, db)
	rows, err := db.QueryContext(ctx,
		`SELECT id, eski_domain_id, sistem_kullanici, dosya FROM backups
		 WHERE son_kullanma IS NOT NULL AND son_kullanma<=NOW() AND `+yetimKosulu+` ORDER BY id LIMIT ?`, silinenSupurLimit)
	if err != nil {
		log.Printf("yetim yedek süpürme: %v", err)
		return
	}
	type kayit struct {
		id, eskiID int64
		sk, dosya  string
	}
	var liste []kayit
	for rows.Next() {
		var k kayit
		if rows.Scan(&k.id, &k.eskiID, &k.sk, &k.dosya) == nil {
			liste = append(liste, k)
		}
	}
	rows.Close()
	for _, k := range liste {
		if ctx.Err() != nil {
			log.Printf("yetim yedek süpürme: süre doldu, kalanı yarın")
			return
		}
		if e := yetimYedegiSil(ctx, db, k.id, k.eskiID, k.sk, k.dosya, false); e != nil {
			log.Printf("yetim yedek %d (%s) temizlenemedi, kayıt korundu (yarın tekrar denenir): %v", k.id, k.dosya, e)
		} else {
			log.Printf("yetim yedek %d (%s) süresi doldu, temizlendi", k.id, k.dosya)
		}
	}
}

// ---------------------------------------------------------------- yönetici uçları

type silinenYedekDosya struct {
	ID        int64  `json:"id"`
	Dosya     string `json:"dosya"`
	BoyutB    int64  `json:"boyut_b"`
	Olusturma string `json:"olusturma"`
	Yerel     bool   `json:"yerel"`
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
	BaglanabilirFarkli bool                `json:"baglanabilir_farkli"`
	Yedekler           []silinenYedekDosya `json:"yedekler"`
	// gruptaki ayrık (küçük harf alan adı, reseller) çiftleri; JSON'a girmez.
	sahipler map[yetimSahip]bool
}

// yetimSahip — yetim satırın (alan adı, reseller) kimliği; alan adı küçük harf + trim.
type yetimSahip struct {
	alan string
	rid  int64
}

func yetimSahibi(alan string, rid int64) yetimSahip {
	return yetimSahip{alan: strings.ToLower(strings.TrimSpace(alan)), rid: rid}
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
		if rows.Scan(&y.ID, &sk, &alan, &rid, &y.Dosya, &y.BoyutB, &y.Olusturma, &silinme, &sonK) != nil {
			continue
		}
		g := idx[sk]
		if g == nil {
			if len(out) >= silinenGrupLimit {
				continue
			}
			g = &silinenGrup{SK: sk, AlanAdi: alan, ResellerID: rid, Silinme: silinme, SonKullanma: sonK, Yedekler: []silinenYedekDosya{},
				sahipler: map[yetimSahip]bool{}}
			idx[sk] = g
			out = append(out, g)
		}
		g.sahipler[yetimSahibi(alan, rid)] = true
		y.Yerel = yerelDuzDosya(sk, y.Dosya)
		g.YerelVar = g.YerelVar || y.Yerel
		g.BoyutB += y.BoyutB
		g.Yedekler = append(g.Yedekler, y)
	}
	rows.Close()
	for _, g := range out {
		g.DosyaSayisi = len(g.Yedekler)
		var did, hrid int64
		var halan string
		if silinenSKGecerli(g.SK) &&
			h.DB.QueryRowContext(r.Context(), `SELECT id, alan_adi, COALESCE(reseller_id,0) FROM domains WHERE sistem_kullanici=? ORDER BY id LIMIT 1`, g.SK).
				Scan(&did, &halan, &hrid) == nil {
			g.BaglanabilirDomainID = &did
			g.BaglanabilirAlanAdi = halan
			hedefSahip := yetimSahibi(halan, hrid)
			for sh := range g.sahipler {
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
// süreyi beklemeden kalıcı siler. Uzak silinemeyen satır + dosya KALIR (409).
func (h *Handlers) SilinenSil(w http.ResponseWriter, r *http.Request) {
	sk := chi.URLParam(r, "sk")
	if !silinenSKGecerli(sk) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz sistem kullanıcısı")
		return
	}
	yoksay := r.URL.Query().Get("uzagi_yoksay") == "1"
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, eski_domain_id, dosya FROM backups WHERE sistem_kullanici=? AND `+yetimKosulu+` ORDER BY id LIMIT ?`, sk, silinenSatirLimit)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "kayıtlar okunamadı")
		return
	}
	type kayit struct {
		id, eskiID int64
		dosya      string
	}
	var liste []kayit
	for rows.Next() {
		var k kayit
		if rows.Scan(&k.id, &k.eskiID, &k.dosya) == nil {
			liste = append(liste, k)
		}
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
	kalan, silinen := 0, 0
	for _, k := range liste {
		if e := yetimYedegiSil(kctx, h.DB, k.id, k.eskiID, sk, k.dosya, yoksay); e != nil {
			kalan++
			if ilk == nil {
				ilk = e
			}
			continue
		}
		silinen++
	}
	if kalan > 0 {
		log.Printf("silinen yedek %s silme: %d/%d silinemedi: %v", sk, kalan, len(liste), ilk)
		httpx.WriteJSON(w, http.StatusConflict, map[string]any{
			"ok": false, "hata": fmt.Sprintf("%d yedek silinemedi (kayıt ve dosya korundu): %v", kalan, ilk),
			"silinen": silinen, "uzagi_yoksay": true,
		})
		return
	}
	// Boşalan sk dizini: yalnız canlı bir hosting sk'yı kullanmıyorsa ve dizin boşsa (os.Remove dolu dizine dokunmaz).
	var canli int
	_ = h.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM domains WHERE sistem_kullanici=?`, sk).Scan(&canli)
	if canli == 0 && yedekDizinGuvenli(sk) {
		_ = os.Remove(filepath.Join(yedekKok, sk))
	}
	httpx.Denetim(h.DB, r, uid, kul, "silinen-yedek.temizle", sk, fmt.Sprintf("yedek=%d uzak_yoksay=%v", silinen, yoksay), 0, true)
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

// SilinenBagla: POST /admin/silinen-yedekler/{sk}/bagla {"domain_id":N, "zorla":bool} — yönetici kararıyla
// yetim yedekleri canlı bir domaine bağlar (hedefin sk'sı eşit olmalı: aksi 400).
//
// 🔴 sk eşitliği AYNI MÜŞTERİ demek değildir: sk alan adının İLK ETİKETİNDEN türer (girgin.com ve
// girgin.net ikisi de c_girgin alabilir). Restore dosyaları + kullanicilar.sql içindeki DB parola
// özetlerini hedef hesaba yazar → farklı müşterinin yedeğini sessizce bağlamak veri sızdırır. Hedefin
// alan adı (küçük harf) VEYA sahibi (reseller_id) yetim satırlarınkinden farklıysa ve zorla!=true
// → 409 {alan_adi_farkli:true,...} ve HİÇBİR satıra dokunulmaz. zorla=true açık onaydır ve denetime yazılır.
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
	// Yetim satırların (alan adı, sahip) çiftleri: HERHANGİ biri hedeften farklıysa onay gerekir.
	srows, err := h.DB.QueryContext(r.Context(), `SELECT DISTINCT alan_adi, reseller_id FROM backups WHERE sistem_kullanici=? AND `+yetimKosulu, sk)
	if err != nil {
		log.Printf("silinen yedek bağla (%s): sahip okunamadı: %v", sk, err)
		httpx.WriteError(w, http.StatusInternalServerError, "bağlanamadı")
		return
	}
	var farkli bool
	var yedekAlan string
	var yedekRID int64
	for srows.Next() {
		var a string
		var rid int64
		if srows.Scan(&a, &rid) != nil {
			continue
		}
		if yetimSahibi(a, rid) != hedefSahip && !farkli {
			farkli, yedekAlan, yedekRID = true, a, rid
		}
	}
	srows.Close()
	if farkli && !req.Zorla {
		httpx.WriteJSON(w, http.StatusConflict, map[string]any{
			"ok": false, "alan_adi_farkli": true,
			"hata":              "yedeğin alan adı/sahibi hedef hostingle aynı değil: açık onay (zorla) gerekir",
			"yedek_alan_adi":    yedekAlan,
			"hedef_alan_adi":    alan,
			"yedek_reseller_id": yedekRID,
			"hedef_reseller_id": hedefRID,
		})
		return
	}
	// Bağlanan yedekler tip='kurtarma' olur (pruneOld/pruneManuelYedek dokunmaz) ve eski uzak hedef
	// anlık görüntüsü (sahip aynıysa, domainin kendi hedefi yoksa) domaine aktif=0 geri yüklenir.
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
		detay += fmt.Sprintf(" alan adı/sahip farkı onaylandı (yedek=%s/bayi=%d, hedef=%s/bayi=%d)", yedekAlan, yedekRID, alan, hedefRID)
	}
	httpx.Denetim(h.DB, r, uid, kul, "silinen-yedek.bagla", alan, detay, req.DomainID, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "baglanan": n})
}

// SilinenAyarGet: GET /admin/silinen-yedekler/ayar → {gun: null|N}  (null = hostingin yedek slotu)
func (h *Handlers) SilinenAyarGet(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"gun": silinenAyarJSON(silinenAyar(r.Context(), h.DB)), "en_az_slot_gun": silinenEnAzGun})
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
