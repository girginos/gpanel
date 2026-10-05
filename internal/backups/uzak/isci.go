package uzak

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"filippo.io/age"

	"girginospanel/internal/backups/hedef"
)

// Manifest — her uzak kopyanın yanına yazılan, sha256 içeren kayıt (uzakta da durur).
// Düz yedeğin ya da anahtarın KENDİSİ burada yoktur.
type Manifest struct {
	ManifestSurum int    `json:"manifest_surum"`
	YedekID       int64  `json:"yedek_id"`
	DomainID      int64  `json:"domain_id"`
	YedekDosya    string `json:"yedek_dosya"`
	UzakAd        string `json:"uzak_ad"`
	BoyutDuz      int64  `json:"boyut_duz"`
	BoyutSifreli  int64  `json:"boyut_sifreli"`
	Sha256Duz     string `json:"sha256_duz"`
	Sha256Sifreli string `json:"sha256_sifreli"`
	Sifreleme     string `json:"sifreleme"`
	AliciParmak   string `json:"alici_parmak"`
	AgeSurum      string `json:"age_surum"`
	KaynakSurum   string `json:"kaynak_surum"`
	Zaman         string `json:"zaman"`
}

// Isci — TEK işçi: aynı anda en çok bir uzak kopya işi koşar (mu). Yerel yedeği
// bekletmez: yedek akışından bağımsız goroutine, düşük öncelikli iş parçacığı.
type Isci struct {
	DB          *sql.DB
	KokDizin    string // yerel yedek kökü (BackupRoot)
	GeciciDizin string // şifreli geçici dosyalar (0700); yedek diskinde olmalı
	HedefAc     func(ctx context.Context, hedefID int64) (hedef.Hedef, error)
	// Uyari — kalıcı hatada kullanıcıya bildirim (nil olabilir). Mesaj sırsızdır.
	Uyari        func(baslik, mesaj string, domainID, yedekID int64)
	PanelSurum   string
	MaksDeneme   int                            // varsayılan 6
	Bekleme      func(deneme int) time.Duration // üstel geri çekilme; varsayılan 1dk,5dk,15dk,1sa,3sa,6sa
	IsZamanAsimi time.Duration                  // tek işin üst sınırı; varsayılan 24 sa
	Aralik       time.Duration                  // kuyruk yoklaması; varsayılan 30 sn

	mu    sync.Mutex // eşzamanlılık 1
	tetik chan struct{}

	cm        sync.Mutex
	calisanID int64
	iptalFn   context.CancelFunc
	iptalIst  bool
}

func (i *Isci) maks() int {
	if i.MaksDeneme > 0 {
		return i.MaksDeneme
	}
	return 6
}

func (i *Isci) bekleme(d int) time.Duration {
	if i.Bekleme != nil {
		return i.Bekleme(d)
	}
	adimlar := []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, 3 * time.Hour, 6 * time.Hour}
	if d < 1 {
		d = 1
	}
	if d > len(adimlar) {
		d = len(adimlar)
	}
	return adimlar[d-1]
}

// Tetikle — yeni iş geldi: işçiyi hemen uyandırır (bloklamaz).
func (i *Isci) Tetikle() {
	if i.tetik == nil {
		return
	}
	select {
	case i.tetik <- struct{}{}:
	default:
	}
}

// Hazirla — tetik kanalını kurar (Calistir'den önce; Tetikle için güvenli).
func (i *Isci) Hazirla() {
	if i.tetik == nil {
		i.tetik = make(chan struct{}, 1)
	}
}

// YarimlariSifirla — panel yeniden başlayınca yarım kalan işler yeniden denenir
// (aynı nihai uzak ada yazılır; hedefin atomik yüklemesi eski yarımı atar).
func (i *Isci) YarimlariSifirla(ctx context.Context) (int64, error) {
	res, err := i.DB.ExecContext(ctx,
		`UPDATE backup_uzak_kopyalar SET durum='bekliyor', sonraki_deneme=NULL
		 WHERE kaynak='hedef' AND durum IN ('sifreleniyor','yukleniyor','dogrulaniyor')`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// Calistir — işçi döngüsü; ctx bitince döner.
func (i *Isci) Calistir(ctx context.Context) {
	DusukOncelikThread()
	i.Hazirla()
	if n, err := i.YarimlariSifirla(ctx); err != nil {
		log.Printf("uzak kopya: yarım işler sıfırlanamadı: %v", err)
	} else if n > 0 {
		log.Printf("uzak kopya: %d yarım iş yeniden kuyruğa alındı", n)
	}
	i.GeciciTemizle(time.Hour)
	aralik := i.Aralik
	if aralik <= 0 {
		aralik = 30 * time.Second
	}
	for ctx.Err() == nil {
		isDone, err := i.Bir(ctx)
		if err != nil {
			log.Printf("uzak kopya işçi: %v", err)
		}
		if isDone {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-i.tetik:
		case <-time.After(aralik):
		}
	}
}

// GeciciTemizle — önceki çalışmadan kalan şifreli geçici dosyaları siler.
func (i *Isci) GeciciTemizle(yasEsik time.Duration) {
	ents, err := os.ReadDir(i.GeciciDizin)
	if err != nil {
		return
	}
	for _, e := range ents {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "uzak-") {
			continue
		}
		if fi, err := e.Info(); err == nil && time.Since(fi.ModTime()) > yasEsik {
			_ = os.Remove(filepath.Join(i.GeciciDizin, e.Name()))
		}
	}
}

// Iptal — işi iptal eder: bekleyen/hatalı iş hemen 'iptal'; KOŞAN iş bağlamı
// iptal edilerek durdurulur (hedef yarım yüklemeyi temizler).
func (i *Isci) Iptal(ctx context.Context, isID int64) error {
	i.cm.Lock()
	if i.calisanID == isID && i.iptalFn != nil {
		i.iptalIst = true
		i.iptalFn()
		i.cm.Unlock()
		return nil
	}
	i.cm.Unlock()
	res, err := i.DB.ExecContext(ctx,
		`UPDATE backup_uzak_kopyalar SET durum='iptal', sonraki_deneme=NULL, son_hata='Kullanıcı iptal etti'
		 WHERE id=? AND kaynak='hedef' AND durum IN ('bekliyor','hata')`, isID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil
	}
	var d string
	if err := i.DB.QueryRowContext(ctx, `SELECT durum FROM backup_uzak_kopyalar WHERE id=? AND kaynak='hedef'`, isID).Scan(&d); errors.Is(err, sql.ErrNoRows) {
		return ErrIsYok
	}
	return ErrDurumUygunDegil
}

// isSatir — işlenen iş.
type isSatir struct {
	id, domainID, hedefID int64
	yedekID               sql.NullInt64
	yedekDosya            string
	deneme                int
}

// Bir — kuyruktan EN ESKİ hazır işi alıp işler. İş yoksa (false, nil). Ayar kapalıysa
// hiçbir iş alınmaz (bekleyenler açılınca devam eder).
func (i *Isci) Bir(ctx context.Context) (bool, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	ayar, err := AyarOku(ctx, i.DB)
	if err != nil {
		return false, err
	}
	if !ayar.Aktif {
		return false, nil
	}
	var is isSatir
	err = i.DB.QueryRowContext(ctx,
		`SELECT id, backup_id, domain_id, hedef_id, yedek_dosya, deneme FROM backup_uzak_kopyalar
		 WHERE kaynak='hedef' AND durum='bekliyor' AND (sonraki_deneme IS NULL OR sonraki_deneme <= NOW())
		 ORDER BY id LIMIT 1`).Scan(&is.id, &is.yedekID, &is.domainID, &is.hedefID, &is.yedekDosya, &is.deneme)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	res, err := i.DB.ExecContext(ctx,
		`UPDATE backup_uzak_kopyalar SET durum='sifreleniyor', baslangic_at=NOW(), yuklenen_b=0 WHERE id=? AND durum='bekliyor'`, is.id)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return true, nil
	}
	sure := i.IsZamanAsimi
	if sure <= 0 {
		sure = 24 * time.Hour
	}
	jctx, iptal := context.WithTimeout(ctx, sure)
	i.cm.Lock()
	i.calisanID, i.iptalFn, i.iptalIst = is.id, iptal, false
	i.cm.Unlock()
	hata := i.isle(jctx, &is, ayar)
	iptal()
	i.cm.Lock()
	kullaniciIptali := i.iptalIst
	i.calisanID, i.iptalFn, i.iptalIst = 0, nil, false
	i.cm.Unlock()
	if hata == nil {
		return true, nil
	}
	if kullaniciIptali {
		i.durumYaz(is.id, DurumIptal, "Kullanıcı iptal etti", "", 0)
		return true, nil
	}
	if ctx.Err() != nil {
		// Panel kapanıyor: iş yarım kaldı, açılışta YarimlariSifirla alır.
		return true, ctx.Err()
	}
	i.basarisiz(&is, hata)
	return true, nil
}

// isHatasi — işin neden başarısız olduğu. sade metin sırsızdır ve kullanıcıya gösterilir.
type isHatasi struct {
	kalici bool
	sade   string
	kat    string
	ic     error // yalnız sunucu günlüğü
}

func (e *isHatasi) Error() string { return e.sade }

func hedefHatasi(islem string, err error) *isHatasi {
	kat := hedef.KategoriAl(err)
	if kat == "" {
		kat = hedef.KategoriBilinmeyen
	}
	h := &isHatasi{sade: islem + ": " + kat.Aciklama(), kat: string(kat), ic: err}
	h.kalici = kat == hedef.KategoriKimlik
	return h
}

func (i *Isci) durumYaz(id int64, durum, sonHata, kat string, deneme int) {
	if _, err := i.DB.Exec(
		`UPDATE backup_uzak_kopyalar SET durum=?, son_hata=?, hata_kategori=?, bitis_at=NOW(), sonraki_deneme=NULL WHERE id=?`,
		durum, kisalt(sonHata, 250), kisalt(kat, 16), id); err != nil {
		log.Printf("uzak kopya: durum yazılamadı (id=%d): %v", id, err)
	}
}

func kisalt(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// basarisiz — deneme sayacını artırır; kalıcı hata ya da deneme tavanı → 'hata' + bildirim,
// değilse üstel geri çekilmeyle 'bekliyor'.
func (i *Isci) basarisiz(is *isSatir, h *isHatasi) {
	if h.ic != nil {
		var he *hedef.Hata
		if errors.As(h.ic, &he) {
			log.Printf("uzak kopya: iş %d başarısız (%s): %s", is.id, h.kat, he.Ayrinti())
		} else {
			log.Printf("uzak kopya: iş %d başarısız: %v", is.id, h.sade)
		}
	}
	deneme := is.deneme + 1
	if h.kalici || deneme >= i.maks() {
		if _, err := i.DB.Exec(
			`UPDATE backup_uzak_kopyalar SET durum='hata', deneme=?, son_hata=?, hata_kategori=?, bitis_at=NOW(), sonraki_deneme=NULL WHERE id=?`,
			deneme, kisalt(h.sade, 250), kisalt(h.kat, 16), is.id); err != nil {
			log.Printf("uzak kopya: hata durumu yazılamadı: %v", err)
		}
		if i.Uyari != nil {
			i.Uyari("Uzak yedek kopyası gönderilemedi",
				fmt.Sprintf("%s: %s. Yerel yedek korundu; Yedek Yönetimi sayfasından yeniden deneyebilirsiniz.", is.yedekDosya, h.sade),
				is.domainID, is.yedekID.Int64)
		}
		return
	}
	bk := i.bekleme(deneme)
	if _, err := i.DB.Exec(
		`UPDATE backup_uzak_kopyalar SET durum='bekliyor', deneme=?, son_hata=?, hata_kategori=?, sonraki_deneme=NOW() + INTERVAL ? SECOND WHERE id=?`,
		deneme, kisalt(h.sade, 250), kisalt(h.kat, 16), int64(bk/time.Second), is.id); err != nil {
		log.Printf("uzak kopya: yeniden deneme yazılamadı: %v", err)
	}
}

func (i *Isci) ilerlemeYaz(id int64, b int64) {
	_, _ = i.DB.Exec(`UPDATE backup_uzak_kopyalar SET yuklenen_b=? WHERE id=?`, b, id)
}

func rastgeleHex() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// isle — tek işin gövdesi: şifrele → yükle → doğrula → manifest → tamam → saklama.
func (i *Isci) isle(ctx context.Context, is *isSatir, ayar Ayar) *isHatasi {
	// 1) etkin anahtar
	anahtar, err := EtkinAnahtar(ctx, i.DB)
	if err != nil {
		return &isHatasi{sade: "anahtar okunamadı", ic: err}
	}
	if anahtar == nil {
		return &isHatasi{sade: ErrAnahtarYok.Error()}
	}
	alici, err := AliciCoz(anahtar.Alici)
	if err != nil {
		return &isHatasi{kalici: true, sade: "etkin açık anahtar geçersiz"}
	}
	// 2) yerel dosya
	yol, kHata := i.yerelYol(ctx, is)
	if kHata != nil {
		return kHata
	}
	fi, err := os.Lstat(yol)
	if err != nil || !fi.Mode().IsRegular() {
		return &isHatasi{kalici: true, sade: "yerel yedek dosyası bulunamadı (yerel saklama süresi dolmuş olabilir)"}
	}
	if err := os.MkdirAll(i.GeciciDizin, 0o700); err != nil {
		return &isHatasi{sade: "geçici dizin oluşturulamadı", ic: err}
	}
	if bos, err := BosAlan(i.GeciciDizin); err == nil && bos < uint64(fi.Size())+uint64(fi.Size())/20+(64<<20) { //nolint:gosec // G115: Size >= 0
		return &isHatasi{sade: "şifreli geçici dosya için disk alanı yetersiz"}
	}
	uzakAd := UzakAdUret(ayar.Klasor, is.yedekDosya)
	if err := hedef.AdDogrula(uzakAd); err != nil {
		return &isHatasi{kalici: true, sade: "uzak ad geçersiz (klasör ya da dosya adı)"}
	}
	// 3) şifrele (akış → geçici dosya 0600; düz ikinci kopya yok)
	tmp := filepath.Join(i.GeciciDizin, fmt.Sprintf("uzak-%d-%s.age.tmp", is.id, rastgeleHex()))
	defer func() { _ = os.Remove(tmp) }()
	sonuc, hh := i.sifreleDosyaya(ctx, yol, tmp, alici)
	if hh != nil {
		return hh
	}
	// yerel yedeğin kayıtlı özeti varsa kaynak bozuk mu diye karşılaştır
	if is.yedekID.Valid {
		var kayitli string
		if err := i.DB.QueryRowContext(ctx, `SELECT sha256 FROM backups WHERE id=?`, is.yedekID.Int64).Scan(&kayitli); err == nil && kayitli != "" && kayitli != sonuc.DuzSha {
			if i.Uyari != nil {
				i.Uyari("Yerel yedek bozuk", is.yedekDosya+": kayıtlı özetle uyuşmuyor; uzağa GÖNDERİLMEDİ.", is.domainID, is.yedekID.Int64)
			}
			return &isHatasi{kalici: true, sade: "yerel yedek kayıtlı özetle uyuşmuyor (bozuk olabilir); uzağa gönderilmedi"}
		}
	}
	_, _ = i.DB.ExecContext(ctx,
		`UPDATE backup_uzak_kopyalar SET durum='yukleniyor', uzak_ad=?, boyut_b=?, boyut_duz=?, sha256_sifreli=?, sha256_duz=?,
		 sifreleme=?, anahtar_parmak=? WHERE id=?`,
		uzakAd, sonuc.SifreliBoyut, sonuc.DuzBoyut, sonuc.SifreliSha, sonuc.DuzSha, anahtar.Sifreleme, anahtar.Parmak, is.id)
	// 4) yükle
	h, err := i.HedefAc(ctx, is.hedefID)
	if err != nil {
		return hedefHatasi("hedef açılamadı", err)
	}
	var son time.Time
	sec := hedef.YukleSecenek{Boyut: sonuc.SifreliBoyut, Sha256: sonuc.SifreliSha, BantKBps: ayar.BantKBps,
		Ilerleme: func(a int64) {
			if time.Since(son) > 3*time.Second {
				son = time.Now()
				i.ilerlemeYaz(is.id, a)
			}
		}}
	if err := h.Yukle(ctx, tmp, uzakAd, sec); err != nil {
		return hedefHatasi("yükleme", err)
	}
	i.ilerlemeYaz(is.id, sonuc.SifreliBoyut)
	// 5) uzaktan doğrula
	_, _ = i.DB.ExecContext(ctx, `UPDATE backup_uzak_kopyalar SET durum='dogrulaniyor' WHERE id=?`, is.id)
	if hh := i.dogrula(ctx, h, uzakAd, sonuc); hh != nil {
		// bozuk/eksik uzak dosyayı bırakma (en iyi çaba; yalnız KENDİ adımızı sileriz)
		sctx, c := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		_ = h.Sil(sctx, uzakAd)
		c()
		return hh
	}
	// 6) manifest (son adım: manifestli kopya = geçerli kopya)
	man := Manifest{ManifestSurum: 1, YedekID: is.yedekID.Int64, DomainID: is.domainID, YedekDosya: is.yedekDosya, UzakAd: uzakAd,
		BoyutDuz: sonuc.DuzBoyut, BoyutSifreli: sonuc.SifreliBoyut, Sha256Duz: sonuc.DuzSha, Sha256Sifreli: sonuc.SifreliSha,
		Sifreleme: anahtar.Sifreleme, AliciParmak: anahtar.Parmak, AgeSurum: AgeSurum(), KaynakSurum: i.PanelSurum,
		Zaman: time.Now().UTC().Format(time.RFC3339)}
	mAd := ManifestAdi(uzakAd)
	if hh := i.manifestYaz(ctx, h, mAd, man); hh != nil {
		return hh
	}
	if _, err := i.DB.ExecContext(ctx,
		`UPDATE backup_uzak_kopyalar SET durum='tamam', manifest_ad=?, son_hata='', hata_kategori='', bitis_at=NOW(),
		 sonraki_deneme=NULL, son_dogrulama=NOW() WHERE id=?`, mAd, is.id); err != nil {
		return &isHatasi{sade: "durum yazılamadı", ic: err}
	}
	// 7) saklama (yalnız kendi manifestli kopyalarımız)
	if _, err := SaklamaUygula(ctx, i.DB, h, ayar, false); err != nil {
		log.Printf("uzak kopya: saklama uygulanamadı: %v", err)
	}
	return nil
}

func (i *Isci) yerelYol(ctx context.Context, is *isSatir) (string, *isHatasi) {
	var sk string
	if err := i.DB.QueryRowContext(ctx, `SELECT sistem_kullanici FROM domains WHERE id=?`, is.domainID).Scan(&sk); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", &isHatasi{kalici: true, sade: "yedeğin alan adı kaydı bulunamadı"}
		}
		return "", &isHatasi{sade: "alan adı okunamadı", ic: err}
	}
	if !strings.HasPrefix(sk, "c_") || is.yedekDosya == "" || is.yedekDosya != filepath.Base(is.yedekDosya) || strings.ContainsAny(sk, `/\`) {
		return "", &isHatasi{kalici: true, sade: "güvensiz yedek yolu"}
	}
	return filepath.Join(i.KokDizin, sk, is.yedekDosya), nil
}

func (i *Isci) sifreleDosyaya(ctx context.Context, yol, tmp string, alici age.Recipient) (SifreleSonuc, *isHatasi) {
	src, err := os.Open(yol)
	if err != nil {
		return SifreleSonuc{}, &isHatasi{kalici: true, sade: "yerel yedek dosyası açılamadı", ic: err}
	}
	defer src.Close()
	dst, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return SifreleSonuc{}, &isHatasi{sade: "şifreli geçici dosya oluşturulamadı", ic: err}
	}
	bw := bufio.NewWriterSize(dst, 256<<10)
	sonuc, err := Sifrele(bw, hedef.IlerlemeOkuyucu(ctx, src, nil), alici)
	if err == nil {
		err = bw.Flush()
	}
	if err == nil {
		err = dst.Sync()
	}
	if cerr := dst.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		if ctx.Err() != nil {
			return SifreleSonuc{}, &isHatasi{sade: "iptal edildi ya da zaman aşımı", ic: ctx.Err()}
		}
		return SifreleSonuc{}, &isHatasi{sade: "şifreleme başarısız (disk ya da okuma hatası)", ic: err}
	}
	return sonuc, nil
}

// dogrula — yüklenen dosyayı uzaktan geri okuyarak doğrular: ad + boyut her zaman;
// sağlayıcı sunucu özeti veriyorsa o, vermiyorsa dosya GERİ İNDİRİLİP sha256'sı
// kendimizce hesaplanır (sağlayıcının kaydettiği özet sessiz bozulmayı yakalamaz).
func (i *Isci) dogrula(ctx context.Context, h hedef.Hedef, uzakAd string, s SifreleSonuc) *isHatasi {
	nesneler, err := h.Listele(ctx, uzakAd)
	if err != nil {
		return hedefHatasi("doğrulama (listeleme)", err)
	}
	var bulunan *hedef.Nesne
	for k := range nesneler {
		if nesneler[k].Ad == uzakAd {
			bulunan = &nesneler[k]
		}
	}
	if bulunan == nil {
		return &isHatasi{sade: "doğrulama: yüklenen dosya uzakta görünmüyor", kat: string(hedef.KategoriBulunamadi)}
	}
	if bulunan.Boyut != s.SifreliBoyut {
		return &isHatasi{sade: fmt.Sprintf("doğrulama: uzak boyut uyuşmuyor (yerel %d, uzak %d)", s.SifreliBoyut, bulunan.Boyut), kat: "dogrulama"}
	}
	if h.Yetenekler().Var(hedef.YetenekSunucuSha256) && bulunan.Sha256 != "" {
		if !strings.EqualFold(bulunan.Sha256, s.SifreliSha) {
			return &isHatasi{sade: "doğrulama: uzak özet uyuşmuyor", kat: "dogrulama"}
		}
		return nil
	}
	gecici := filepath.Join(i.GeciciDizin, fmt.Sprintf("uzak-dogrula-%s.tmp", rastgeleHex()))
	defer func() { _ = os.Remove(gecici) }()
	if err := h.Indir(ctx, uzakAd, gecici); err != nil {
		return hedefHatasi("doğrulama (geri indirme)", err)
	}
	f, err := os.Open(gecici)
	if err != nil {
		return &isHatasi{sade: "doğrulama dosyası açılamadı", ic: err}
	}
	defer f.Close()
	hs := sha256.New()
	n, err := io.Copy(hs, hedef.IlerlemeOkuyucu(ctx, f, nil))
	if err != nil {
		return &isHatasi{sade: "doğrulama dosyası okunamadı", ic: err}
	}
	if n != s.SifreliBoyut || hex.EncodeToString(hs.Sum(nil)) != s.SifreliSha {
		return &isHatasi{sade: "doğrulama: uzaktan indirilen içerik özetle uyuşmuyor (bozuk kopya)", kat: "dogrulama"}
	}
	return nil
}

// manifestYaz — manifesti 0600 geçici dosyadan yükler ve varlığını/boyutunu doğrular.
func (i *Isci) manifestYaz(ctx context.Context, h hedef.Hedef, mAd string, m Manifest) *isHatasi {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return &isHatasi{sade: "manifest üretilemedi", ic: err}
	}
	gecici := filepath.Join(i.GeciciDizin, fmt.Sprintf("uzak-manifest-%s.tmp", rastgeleHex()))
	if err := os.WriteFile(gecici, b, 0o600); err != nil {
		return &isHatasi{sade: "manifest geçici dosyası yazılamadı", ic: err}
	}
	defer func() { _ = os.Remove(gecici) }()
	if err := h.Yukle(ctx, gecici, mAd, hedef.YukleSecenek{Boyut: int64(len(b))}); err != nil {
		return hedefHatasi("manifest yükleme", err)
	}
	nesneler, err := h.Listele(ctx, mAd)
	if err != nil {
		return hedefHatasi("manifest doğrulama", err)
	}
	for _, n := range nesneler {
		if n.Ad == mAd && n.Boyut == int64(len(b)) {
			return nil
		}
	}
	return &isHatasi{sade: "manifest uzakta doğrulanamadı", kat: "dogrulama"}
}
