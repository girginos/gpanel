package uzak

// yerelsil.go — "Drive'a gönderilince yerel kopyayı sil".
//
// 🔴 BİR NUMARALI KISIT: yedek kaybı. Şüphede SİLME. Bir yerel yedek dosyası YALNIZ şu
// koşulların HEPSİ sağlanınca silinir (biri tutmazsa dosyaya dokunulmaz):
//
//  1. kayıt kök (sistem) yedeği: tip oto/planli ya da toplu iş (KokYedegiMi); müşteri/manuel ASLA
//  2. Drive kopyası: kaynak='hedef', durum='tamam', manifest yazılmış, doğrulama zamanı kayıtlı,
//     hedef kaydı duruyor, adlar bu orkestratörün biçiminde
//  3. kopya yerel yedekle aynı: düz boyut = kayıtlı boyut = yerel dosya boyutu; kayıtlı özet
//     varsa Drive özetiyle eşit
//  4. alan adının en yeni yerel_tut kök yedeği DEĞİL
//  5. yol güvenli: sk "c_", dosya adı yaprak ve .tar.gz, kök dizin altında, sembolik bağlantı
//     yok, normal dosya (kayıttaki ad dışında hiçbir girdi yola karışmaz)
//  6. dosya en az 10 dk eski (yeni yedek / sürmekte olan başka gönderim olabilir)
//  7. şu an kullanımda değil: geri yükleme/yedek işlemi, başka gönderim, çalışan geri yükleme işi
//  8. Drive'da şu an GERÇEKTEN duruyor: şifreli dosya + manifest hedeften listelenir, boyut eşit.
//     Hedefe ulaşılamazsa silinmez (sonra yeniden denenir)
//
// Silme sırası: dosyayı aynı dizinde ".silinecek-*" adına TAŞI (yeni okuyucular dosyayı
// bulamaz, açık okuyucular etkilenmez) → kullanımı yeniden denetle → sil → backups.yerel_yok
// işaretle. Tüm koşu tek kilit altında (işçi + süpürme + toplu temizlik aynı dosyayı çift
// silemez). backups satırı SİLİNMEZ.

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/backups/hedef"
)

// YerelTutEnFazla — her alan adı için yerelde tutulabilecek en çok yedek.
const YerelTutEnFazla = 10

const (
	yerelSilEnAzYas     = 10 * time.Minute
	yerelSilSupurmeVar  = 10 * time.Minute
	yerelSilHedefSure   = 2 * time.Minute
	silinecekEki        = ".silinecek-"
	yerelSilToplamSure  = 30 * time.Minute
	onizlemeAlanAdiSayi = 10
)

// Sınıflar (önizleme ile çalıştırma AYNI sınıflamayı kullanır).
const (
	sinifSil      = "sil"      // tüm yerel koşullar tamam: silinecek
	sinifIsaretle = "isaretle" // dosya zaten yok, Drive doğrulanmış: yalnız işaretlenecek
	sinifSonN     = "son_n"    // yerel_tut kuralıyla korunur
	sinifDriveYok = "drive_yok"
	sinifManuel   = "manuel"
	sinifSorunlu  = "sorunlu" // boyut/özet/yol/yaş uyuşmazlığı: korunur
	sinifYok      = "yok"     // dosya yok ve Drive'da doğrulanmamış ya da zaten işaretli: iş yok
)

// ErrTemizlikSuruyor — başka bir silme koşusu çalışıyor.
var ErrTemizlikSuruyor = errors.New("başka bir yerel temizlik sürüyor, biraz sonra tekrar deneyin")

// yerelSilMu — tüm silme/işaretleme koşuları (işçi, süpürme, toplu temizlik) bu kilitle sıralanır.
var yerelSilMu sync.Mutex

// yerelKayit — bir yedek + (varsa) en yeni 'tamam' Drive kopyası.
type yerelKayit struct {
	yedekID, domainID int64
	alanAdi, sk       string
	dosya, sha        string
	boyutB            int64
	kok, yerelYok     bool
	sira              int // alan adının kök yedekleri arasında, 1 = en yeni

	kopyaID                    int64
	hedefID                    int64
	uzakAd, manifestAd, shaDuz string
	boyutDuz, boyutSifreli     int64
	dogrulandi                 bool

	yol        string
	yerelBoyut int64
	sinif      string
	neden      string
}

// YerelOnizleme — "Mevcut yedekleri temizle" onay penceresinin özeti.
type YerelOnizleme struct {
	YerelTut   int      `json:"yerel_tut"`
	Silinecek  int      `json:"silinecek_adet"`
	SilinecekB int64    `json:"silinecek_b"`
	Domain     int      `json:"domain"`
	AlanAdlari []string `json:"alan_adlari"`
	Tutulan    int      `json:"tutulan_adet"`
	TutulanB   int64    `json:"tutulan_b"`
	DriveYok   int      `json:"drive_yok_adet"`
	DriveYokB  int64    `json:"drive_yok_b"`
	Manuel     int      `json:"manuel_adet"`
	ManuelB    int64    `json:"manuel_b"`
	Sorunlu    int      `json:"sorunlu_adet"`
	SorunluB   int64    `json:"sorunlu_b"`
}

// NedenSayi — sonuç özetindeki atlama/hata nedeni.
type NedenSayi struct {
	Neden string `json:"neden"`
	Adet  int    `json:"adet"`
}

// YerelSilSonuc — çalıştırma özeti.
type YerelSilSonuc struct {
	Silinen     int         `json:"silinen"`
	SilinenB    int64       `json:"silinen_b"`
	Isaretlenen int         `json:"isaretlenen"`
	Atlanan     int         `json:"atlanan"`
	Hata        int         `json:"hata"`
	Nedenler    []NedenSayi `json:"nedenler"`
}

func (s *YerelSilSonuc) nedenEkle(n string, hata bool) {
	if hata {
		s.Hata++
	} else {
		s.Atlanan++
	}
	for k := range s.Nedenler {
		if s.Nedenler[k].Neden == n {
			s.Nedenler[k].Adet++
			return
		}
	}
	s.Nedenler = append(s.Nedenler, NedenSayi{Neden: n, Adet: 1})
}

// YerelSayac — bu süreçte yapılan silme sayaçları (durum kartı / günlük).
type YerelSayac struct {
	Silinen    int64  `json:"silinen"`
	Hata       int64  `json:"hata"`
	SonSupurme string `json:"son_supurme,omitempty"`
}

// YerelSayac — işçinin süreç boyunca silme sayaçları.
func (i *Isci) YerelSayac() YerelSayac {
	i.cm.Lock()
	defer i.cm.Unlock()
	s := YerelSayac{Silinen: i.ysSilinen, Hata: i.ysHata}
	if !i.ysSon.IsZero() {
		s.SonSupurme = i.ysSon.Format("2006-01-02 15:04")
	}
	return s
}

// YerelDosyaYolu — yedeğin yerel yolunu GÜVENLİ çözer. neden != "" → yol kullanılamaz;
// yok=true → dosya bulunmuyor (hata değil).
func YerelDosyaYolu(kok, sk, dosya string) (yol string, fi os.FileInfo, neden string, yok bool) {
	if !strings.HasPrefix(sk, "c_") || strings.ContainsAny(sk, `/\`) || strings.Contains(sk, "..") ||
		dosya == "" || dosya != filepath.Base(dosya) || strings.ContainsAny(dosya, `/\`) || strings.Contains(dosya, "..") ||
		!strings.HasSuffix(dosya, ".tar.gz") {
		return "", nil, "yedek yolu güvenli değil", false
	}
	kokReal, err := filepath.EvalSymlinks(kok)
	if err != nil {
		return "", nil, "yedek dizini okunamadı", false
	}
	beklenenDizin := filepath.Join(kokReal, sk)
	dirReal, err := filepath.EvalSymlinks(beklenenDizin)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil, "", true
		}
		return "", nil, "yedek dizini okunamadı", false
	}
	if dirReal != beklenenDizin {
		return "", nil, "yedek dizini sembolik bağlantı", false
	}
	yol = filepath.Join(dirReal, dosya)
	fi, err = os.Lstat(yol)
	if err != nil {
		if os.IsNotExist(err) {
			return yol, nil, "", true
		}
		return "", nil, "yedek dosyası okunamadı", false
	}
	if !fi.Mode().IsRegular() {
		return "", nil, "yedek dosyası normal dosya değil (sembolik bağlantı olabilir)", false
	}
	return yol, fi, "", false
}

// driveTamam — kayıt, doğrulanmış ve bu orkestratörün biçiminde adlandırılmış bir Drive kopyasına bağlı mı?
func (k *yerelKayit) driveTamam() bool {
	return k.kopyaID > 0 && k.dogrulandi && k.hedefID > 0 && k.boyutSifreli > 0 &&
		k.uzakAd != "" && hedef.AdDogrula(k.uzakAd) == nil && !hedef.GeciciMi(k.uzakAd) &&
		strings.HasSuffix(k.uzakAd, ".age") && k.manifestAd == ManifestAdi(k.uzakAd)
}

// siniflandir — yerel (ağsız) koşulları denetler; sonuç sinif/neden alanlarına yazılır.
func (k *yerelKayit) siniflandir(kokDizin string, tut int, simdi time.Time) {
	k.yol, k.yerelBoyut, k.neden = "", 0, ""
	if !k.kok {
		k.sinif = sinifManuel
		return
	}
	yol, fi, neden, yok := YerelDosyaYolu(kokDizin, k.sk, k.dosya)
	if yok {
		if k.yerelYok || !k.driveTamam() {
			k.sinif = sinifYok
			return
		}
		k.sinif = sinifIsaretle
		return
	}
	if neden != "" {
		k.sinif, k.neden = sinifSorunlu, neden
		return
	}
	k.yol, k.yerelBoyut = yol, fi.Size()
	if !k.driveTamam() {
		k.sinif = sinifDriveYok
		return
	}
	if k.sira <= tut {
		k.sinif = sinifSonN
		return
	}
	switch {
	case simdi.Sub(fi.ModTime()) < yerelSilEnAzYas:
		k.sinif, k.neden = sinifSorunlu, "yedek henüz yeni"
	case k.yerelBoyut <= 0 || k.yerelBoyut != k.boyutB || k.boyutDuz != k.yerelBoyut:
		k.sinif, k.neden = sinifSorunlu, "yerel dosya boyutu Drive kopyasıyla uyuşmuyor"
	case k.sha != "" && k.shaDuz != "" && !strings.EqualFold(k.sha, k.shaDuz):
		k.sinif, k.neden = sinifSorunlu, "yerel yedeğin özeti Drive kopyasıyla uyuşmuyor"
	default:
		k.sinif = sinifSil
	}
}

// yerelKayitlari — yedekler + en yeni 'tamam' Drive kopyaları, sınıflanmış. domainID>0 → yalnız o
// alan adı. Ağa ÇIKMAZ.
func yerelKayitlari(ctx context.Context, db *sql.DB, kok string, tut int, domainID int64) ([]*yerelKayit, error) {
	q := `SELECT b.id, b.domain_id, d.alan_adi, d.sistem_kullanici, b.dosya, b.boyut_b, b.sha256,
	        (b.tip IN ('oto','planli') OR b.job_id IS NOT NULL), (b.yerel_yok IS NOT NULL),
	        COALESCE(k.id,0), COALESCE(k.hedef_id,0), COALESCE(k.uzak_ad,''), COALESCE(k.manifest_ad,''), COALESCE(k.sha256_duz,''),
	        COALESCE(k.boyut_duz,0), COALESCE(k.boyut_b,0), (k.id IS NOT NULL AND k.son_dogrulama IS NOT NULL)
	      FROM backups b JOIN domains d ON d.id=b.domain_id
	      LEFT JOIN backup_uzak_kopyalar k ON k.id = (
	        SELECT MAX(k2.id) FROM backup_uzak_kopyalar k2 WHERE k2.backup_id=b.id AND k2.kaynak='hedef' AND k2.durum='tamam')`
	var args []any
	if domainID > 0 {
		q += ` WHERE b.domain_id=?`
		args = append(args, domainID)
	}
	q += ` ORDER BY b.domain_id, b.id DESC`
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*yerelKayit
	var sonDomain int64 = -1
	sira := 0
	for rows.Next() {
		k := &yerelKayit{}
		var kokMu, yy, dog int
		if err := rows.Scan(&k.yedekID, &k.domainID, &k.alanAdi, &k.sk, &k.dosya, &k.boyutB, &k.sha, &kokMu, &yy,
			&k.kopyaID, &k.hedefID, &k.uzakAd, &k.manifestAd, &k.shaDuz, &k.boyutDuz, &k.boyutSifreli, &dog); err != nil {
			return nil, err
		}
		k.kok, k.yerelYok, k.dogrulandi = kokMu == 1, yy == 1, dog == 1
		if k.domainID != sonDomain {
			sonDomain, sira = k.domainID, 0
		}
		if k.kok {
			sira++
			k.sira = sira
		}
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	simdi := time.Now()
	for _, k := range out {
		k.siniflandir(kok, tut, simdi)
	}
	return out, nil
}

// YerelOnizle — kaç yedek / kaç bayt silinecek, neler korunacak. AĞA ÇIKMAZ, hiçbir şey değiştirmez.
// Çalıştırma aynı sınıflamayı kullanır; fark yalnız çalıştırmadaki Drive varlık denetimidir.
func (i *Isci) YerelOnizle(ctx context.Context) (YerelOnizleme, error) {
	var on YerelOnizleme
	ayar, err := AyarOku(ctx, i.DB)
	if err != nil {
		return on, err
	}
	on.YerelTut = ayar.YerelTut
	liste, err := yerelKayitlari(ctx, i.DB, i.KokDizin, ayar.YerelTut, 0)
	if err != nil {
		return on, err
	}
	alan := map[string]bool{}
	for _, k := range liste {
		switch k.sinif {
		case sinifSil:
			on.Silinecek++
			on.SilinecekB += k.yerelBoyut
			alan[k.alanAdi] = true
		case sinifSonN:
			on.Tutulan++
			on.TutulanB += k.yerelBoyut
		case sinifDriveYok:
			on.DriveYok++
			on.DriveYokB += k.yerelBoyut
		case sinifSorunlu:
			on.Sorunlu++
			on.SorunluB += k.yerelBoyut
		case sinifManuel:
			// yalnız yerelde duran müşteri/manuel yedekler sayılır
			if _, fi, neden, yok := YerelDosyaYolu(i.KokDizin, k.sk, k.dosya); !yok && neden == "" {
				on.Manuel++
				on.ManuelB += fi.Size()
			}
		}
	}
	on.Domain = len(alan)
	on.AlanAdlari = make([]string, 0, len(alan))
	for a := range alan {
		on.AlanAdlari = append(on.AlanAdlari, a)
	}
	sort.Strings(on.AlanAdlari)
	if len(on.AlanAdlari) > onizlemeAlanAdiSayi {
		on.AlanAdlari = on.AlanAdlari[:onizlemeAlanAdiSayi]
	}
	return on, nil
}

// kullanimda — yedek şu an başka bir işlemce kullanılıyor mu? (neden boş = kullanımda değil)
func (i *Isci) kullanimda(ctx context.Context, k *yerelKayit) string {
	if i.Mesgul != nil && i.Mesgul(k.domainID) {
		return "alan adında yedek ya da geri yükleme işlemi sürüyor"
	}
	var n int
	if err := i.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM backup_jobs WHERE islem='geri' AND durum='calisiyor'`).Scan(&n); err != nil {
		return "iş durumu okunamadı"
	}
	if n > 0 {
		return "geri yükleme işi sürüyor"
	}
	if err := i.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM backup_uzak_kopyalar WHERE backup_id=? AND kaynak='hedef' AND durum IN ('bekliyor','sifreleniyor','yukleniyor','dogrulaniyor')`,
		k.yedekID).Scan(&n); err != nil {
		return "gönderim durumu okunamadı"
	}
	if n > 0 {
		return "yedeğin başka bir gönderimi sürüyor"
	}
	return ""
}

// uzakDogrula — Drive kopyası ŞU AN hedefte duruyor mu: şifreli dosya + manifest, boyut eşit.
func (i *Isci) uzakDogrula(ctx context.Context, k *yerelKayit) string {
	var durum, man, uzak string
	if err := i.DB.QueryRowContext(ctx, `SELECT durum, manifest_ad, uzak_ad FROM backup_uzak_kopyalar WHERE id=?`, k.kopyaID).
		Scan(&durum, &man, &uzak); err != nil || durum != DurumTamam || man != k.manifestAd || uzak != k.uzakAd {
		return "Drive kopyasının durumu değişti"
	}
	if i.HedefAc == nil {
		return "hedef açılamadı"
	}
	cctx, iptal := context.WithTimeout(ctx, yerelSilHedefSure)
	defer iptal()
	h, err := i.HedefAc(cctx, k.hedefID)
	if err != nil {
		return "Drive hedefine ulaşılamadı"
	}
	nesneler, err := h.Listele(cctx, k.uzakAd)
	if err != nil {
		return "Drive hedefine ulaşılamadı"
	}
	var veriVar, manifestVar bool
	for _, n := range nesneler {
		switch n.Ad {
		case k.uzakAd:
			veriVar = n.Boyut == k.boyutSifreli
		case k.manifestAd:
			manifestVar = n.Boyut > 0
		}
	}
	if !veriVar || !manifestVar {
		return "kopya Drive'da görünmüyor ya da boyutu uyuşmuyor"
	}
	return ""
}

func yerelRastgele() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// yerelSilBir — tek yedeğin yerel dosyasını (ya da yalnız işaretini) tüm koşullar sağlanırsa kaldırır.
// donus: "silindi" | "isaretlendi" | "atlandi" | "hata"
func (i *Isci) yerelSilBir(ctx context.Context, k *yerelKayit, tut int) (donus, neden string) {
	// koşullar kilit altında YENİDEN okunmuş durumla denetlenir (liste ile silme arası değişmiş olabilir)
	k.siniflandir(i.KokDizin, tut, time.Now())
	if k.sinif != sinifSil && k.sinif != sinifIsaretle {
		if k.neden == "" {
			k.neden = "koşullar artık sağlanmıyor"
		}
		return "atlandi", k.neden
	}
	if n := i.kullanimda(ctx, k); n != "" {
		return "atlandi", n
	}
	if n := i.uzakDogrula(ctx, k); n != "" {
		return "atlandi", n
	}
	if k.sinif == sinifIsaretle {
		if err := i.yerelYokYaz(ctx, k.yedekID); err != nil {
			return "hata", "kayıt güncellenemedi"
		}
		return "isaretlendi", ""
	}
	gecici := k.yol + silinecekEki + yerelRastgele()
	if err := os.Rename(k.yol, gecici); err != nil {
		if os.IsNotExist(err) {
			return "atlandi", "dosya başka bir işlemce kaldırıldı"
		}
		log.Printf("yerel silme: %s taşınamadı: %v", k.dosya, err)
		return "hata", "dosya taşınamadı"
	}
	// taşıma sonrası kullanımı yeniden denetle: yarışta kazanan geri yüklemeyse geri koy
	if n := i.kullanimda(ctx, k); n != "" {
		if err := os.Rename(gecici, k.yol); err != nil {
			log.Printf("🔴 yerel silme: %s geri konamadı (%s): %v", k.dosya, gecici, err)
			return "hata", "dosya geri konamadı"
		}
		return "atlandi", n
	}
	if err := os.Remove(gecici); err != nil {
		if rerr := os.Rename(gecici, k.yol); rerr != nil {
			log.Printf("🔴 yerel silme: %s silinemedi ve geri konamadı (%s): %v / %v", k.dosya, gecici, err, rerr)
		}
		return "hata", "dosya silinemedi"
	}
	if err := i.yerelYokYaz(ctx, k.yedekID); err != nil {
		// Dosya gitti, işaret yazılamadı: süpürme dosyanın yokluğunu görüp işaretler.
		log.Printf("yerel silme: %s silindi ama kayıt işaretlenemedi: %v", k.dosya, err)
	}
	return "silindi", ""
}

func (i *Isci) yerelYokYaz(ctx context.Context, yedekID int64) error {
	_, err := i.DB.ExecContext(ctx, `UPDATE backups SET yerel_yok=NOW() WHERE id=? AND yerel_yok IS NULL`, yedekID)
	return err
}

// yerelTemizleKilitli — kilit alınmış koşu. domainID>0 → yalnız o alan adı.
func (i *Isci) yerelTemizleKilitli(ctx context.Context, tut int, domainID int64) (YerelSilSonuc, error) {
	var s YerelSilSonuc
	liste, err := yerelKayitlari(ctx, i.DB, i.KokDizin, tut, domainID)
	if err != nil {
		return s, err
	}
	for _, k := range liste {
		if k.sinif != sinifSil && k.sinif != sinifIsaretle {
			continue
		}
		if err := ctx.Err(); err != nil {
			return s, err
		}
		boyut := k.yerelBoyut
		donus, neden := i.yerelSilBir(ctx, k, tut)
		switch donus {
		case "silindi":
			s.Silinen++
			s.SilinenB += boyut
			i.sayac(1, 0)
		case "isaretlendi":
			s.Isaretlenen++
		case "atlandi":
			s.nedenEkle(neden, false)
		default:
			s.nedenEkle(neden, true)
			i.sayac(0, 1)
		}
	}
	return s, nil
}

func (i *Isci) sayac(silinen, hata int64) {
	i.cm.Lock()
	i.ysSilinen += silinen
	i.ysHata += hata
	i.cm.Unlock()
}

// YerelTemizle — tek seferlik toplu temizlik (yerel_tut kuralı uygulanır; yalnız doğrulanmış Drive
// kopyası olanlar). Ayar kapalı olsa da çalışır: kullanıcı açıkça ister. Başka koşu sürüyorsa bekler.
func (i *Isci) YerelTemizle(ctx context.Context) (YerelSilSonuc, error) {
	ayar, err := AyarOku(ctx, i.DB)
	if err != nil {
		return YerelSilSonuc{}, err
	}
	yerelSilMu.Lock()
	defer yerelSilMu.Unlock()
	return i.yerelTemizleKilitli(ctx, ayar.YerelTut, 0)
}

// yerelOtomatik — işçi/süpürme tetikleri: YALNIZ ayar.Aktif && ayar.YerelSil iken; kilit doluysa atlar
// (süpürme zaten tekrar dener); hata iş durumunu bozmaz.
func (i *Isci) yerelOtomatik(ctx context.Context, domainID int64) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("yerel silme çöktü (yutuldu): %v", r)
		}
	}()
	ayar, err := AyarOku(ctx, i.DB)
	if err != nil || !ayar.Aktif || !ayar.YerelSil {
		return
	}
	if !yerelSilMu.TryLock() {
		return
	}
	defer yerelSilMu.Unlock()
	sctx, iptal := context.WithTimeout(ctx, yerelSilToplamSure)
	defer iptal()
	s, err := i.yerelTemizleKilitli(sctx, ayar.YerelTut, domainID)
	if domainID == 0 {
		i.cm.Lock()
		i.ysSon = time.Now()
		i.cm.Unlock()
	}
	if err != nil {
		log.Printf("yerel silme: koşu tamamlanamadı: %v", err)
		return
	}
	if s.Silinen > 0 || s.Isaretlenen > 0 || s.Hata > 0 {
		log.Printf("yerel silme: %d yedek silindi (%s), %d işaretlendi, %d atlandı, %d hata",
			s.Silinen, insanB(s.SilinenB), s.Isaretlenen, s.Atlanan, s.Hata)
	}
}

// YerelSupur — açılışta/periyodik süpürme: yarım kalan silmeleri tamamlar.
func (i *Isci) YerelSupur(ctx context.Context) { i.yerelOtomatik(ctx, 0) }

func insanB(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// YerelYokOzeti — yalnız Drive'da duran yedeklerin sayısı ve boyutu (durum kartı).
func YerelYokOzeti(ctx context.Context, db *sql.DB) (adet int, boyut int64, err error) {
	err = db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(boyut_b),0) FROM backups WHERE yerel_yok IS NOT NULL`).Scan(&adet, &boyut)
	return adet, boyut, err
}
