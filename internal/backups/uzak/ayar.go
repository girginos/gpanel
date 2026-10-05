package uzak

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"girginospanel/internal/backups/hedef"
	"girginospanel/internal/gizli"
)

// Test dikişleri: üretimde gerçek uygulamalar; testler değiştirir.
var (
	SirSakla = gizli.SaklaBagli
	SirCoz   = gizli.CozBagli
	SirHazir = gizli.AnahtarHazir
)

// Hatalar (arayüze aynen gösterilebilir; sır içermez).
var (
	ErrKapali          = errors.New("uzak kopya kapalı")
	ErrAnahtarYok      = errors.New("etkin şifreleme anahtarı yok: önce anahtar kurup kurtarma dosyasını onaylayın")
	ErrHedefYok        = errors.New("uzak hedef seçilmedi ya da bulunamadı")
	ErrSifreHazirDegil = errors.New("panel şifreleme anahtarı hazır değil; özel anahtar güvenle saklanamaz")
	ErrAyarGecersiz    = errors.New("geçersiz ayar")
	ErrKokDegil        = errors.New("yalnız sistem (otomatik/toplu) yedekleri uzağa gönderilir")
)

// Ayar — backup_uzak_kopya_ayar (tek satır).
type Ayar struct {
	Aktif      bool   `json:"aktif"`
	HedefID    int64  `json:"hedef_id"` // 0 = seçilmedi
	Klasor     string `json:"klasor"`
	SaklamaN   int    `json:"saklama_n"`
	SaklamaGun int    `json:"saklama_gun"`
	BantKBps   int    `json:"bant_kbps"`
}

// VarsayilanAyar — satır yoksa güvenli varsayılan (KAPALI).
func VarsayilanAyar() Ayar {
	return Ayar{Klasor: "girginospanel-yedek", SaklamaN: 7}
}

// AyarOku — ayar satırı; tablo/satır yoksa varsayılan (hata değil: göç henüz koşmamış
// kurulumda yedek akışı etkilenmemeli).
func AyarOku(ctx context.Context, db *sql.DB) (Ayar, error) {
	a := VarsayilanAyar()
	var aktif int
	var hid sql.NullInt64
	err := db.QueryRowContext(ctx,
		`SELECT aktif, hedef_id, klasor, saklama_n, saklama_gun, bant_kbps FROM backup_uzak_kopya_ayar WHERE id=1`).
		Scan(&aktif, &hid, &a.Klasor, &a.SaklamaN, &a.SaklamaGun, &a.BantKBps)
	if errors.Is(err, sql.ErrNoRows) {
		return VarsayilanAyar(), nil
	}
	if err != nil {
		return VarsayilanAyar(), err
	}
	a.Aktif = aktif == 1 && hid.Valid
	a.HedefID = hid.Int64
	return a, nil
}

// AyarDenetle — alan sınırları; geçersizse ErrAyarGecersiz'i saran açıklayıcı hata.
func AyarDenetle(a *Ayar) error {
	a.Klasor = strings.Trim(strings.TrimSpace(a.Klasor), "/")
	gecersiz := hedef.AdDogrula(a.Klasor) != nil
	for _, p := range strings.Split(a.Klasor, "/") {
		if strings.HasPrefix(p, hedef.UzakGeciciOnek) {
			gecersiz = true
		}
	}
	if gecersiz {
		return fmt.Errorf("%w: klasör adı geçersiz", ErrAyarGecersiz)
	}
	if a.SaklamaN < 1 || a.SaklamaN > 3650 {
		return fmt.Errorf("%w: saklanacak kopya sayısı 1-3650 olmalı", ErrAyarGecersiz)
	}
	if a.SaklamaGun < 0 || a.SaklamaGun > 3650 {
		return fmt.Errorf("%w: gün sınırı 0-3650 olmalı", ErrAyarGecersiz)
	}
	if a.BantKBps < 0 || a.BantKBps > 10_000_000 {
		return fmt.Errorf("%w: bant sınırı 0-10000000 KiB/sn olmalı", ErrAyarGecersiz)
	}
	return nil
}

// ayarKontrol — alan sınırları + hedef denetimi (yazmaz, anahtar üretmez).
func ayarKontrol(ctx context.Context, db *sql.DB, a *Ayar) error {
	if err := AyarDenetle(a); err != nil {
		return err
	}
	if a.Aktif && a.HedefID <= 0 {
		return ErrHedefYok
	}
	if a.HedefID > 0 {
		var n int
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM backup_hedefler WHERE id=? AND sahip_tur='kok' AND sahip_id=0 AND aktif=1`, a.HedefID).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return ErrHedefYok
		}
	}
	return nil
}

func ayarKaydet(ctx context.Context, db *sql.DB, a Ayar) error {
	var hid any
	if a.HedefID > 0 {
		hid = a.HedefID
	}
	ak := 0
	if a.Aktif {
		ak = 1
	}
	if _, err := db.ExecContext(ctx, `INSERT IGNORE INTO backup_uzak_kopya_ayar (id) VALUES (1)`); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx,
		`UPDATE backup_uzak_kopya_ayar SET aktif=?, hedef_id=?, klasor=?, saklama_n=?, saklama_gun=?, bant_kbps=? WHERE id=1`,
		ak, hid, a.Klasor, a.SaklamaN, a.SaklamaGun, a.BantKBps)
	return err
}

// AyarYaz — ayarı doğrular ve yazar. Açarken hedef (kök, aktif) şart; etkin şifreleme
// anahtarı yoksa SUNUCU otomatik üretir (kullanıcı anahtar üretmez/onaylamaz).
func AyarYaz(ctx context.Context, db *sql.DB, a Ayar) error {
	_, err := AyarYazAnahtarli(ctx, db, a)
	return err
}

// ─── anahtarlar ─────────────────────────────────────────────────────────────

// AnahtarBilgi — backup_sifre_anahtarlari satırının SIRSIZ özeti.
type AnahtarBilgi struct {
	ID                int64  `json:"id"`
	Parmak            string `json:"parmak"`
	KisaParmak        string `json:"kisa_parmak"`
	Alici             string `json:"alici"`
	Durum             string `json:"durum"` // bekliyor | etkin | emekli
	Sifreleme         string `json:"sifreleme"`
	SunucudaOzel      bool   `json:"sunucuda_ozel"` // false: yalnız açık anahtar içe aktarıldı
	KurtarmaIndirildi string `json:"kurtarma_indirildi,omitempty"`
	KurtarmaOnay      string `json:"kurtarma_onay,omitempty"`
	Olusturma         string `json:"olusturma"`
}

const anahtarSecim = `SELECT id, parmak, alici, durum, gizli_anahtar <> '',
	COALESCE(DATE_FORMAT(kurtarma_indirildi,'%Y-%m-%d %H:%i'),''), COALESCE(DATE_FORMAT(kurtarma_onay,'%Y-%m-%d %H:%i'),''),
	DATE_FORMAT(created_at,'%Y-%m-%d %H:%i') FROM backup_sifre_anahtarlari`

func anahtarTara(sc interface{ Scan(...any) error }) (AnahtarBilgi, error) {
	var k AnahtarBilgi
	var oz int
	if err := sc.Scan(&k.ID, &k.Parmak, &k.Alici, &k.Durum, &oz, &k.KurtarmaIndirildi, &k.KurtarmaOnay, &k.Olusturma); err != nil {
		return k, err
	}
	k.KisaParmak = KisaParmak(k.Parmak)
	k.Sifreleme = SifrelemeTuru(k.Alici)
	k.SunucudaOzel = oz == 1
	return k, nil
}

// Anahtarlar — tüm anahtarlar (yeniden eskiye). Özel anahtar DÖNMEZ.
func Anahtarlar(ctx context.Context, db *sql.DB) ([]AnahtarBilgi, error) {
	rows, err := db.QueryContext(ctx, anahtarSecim+` ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AnahtarBilgi{}
	for rows.Next() {
		k, err := anahtarTara(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// EtkinAnahtar — şifrelemede kullanılan anahtar; yoksa nil.
func EtkinAnahtar(ctx context.Context, db *sql.DB) (*AnahtarBilgi, error) {
	k, err := anahtarTara(db.QueryRowContext(ctx, anahtarSecim+` WHERE durum='etkin'`))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// AnahtarUret — yeni anahtar çifti üretir; özel anahtar satıra bağlı ŞİFRELİ saklanır
// (durum 'bekliyor': onaylanana dek şifrelemede kullanılmaz) ve kurtarma dosyası
// TEK KEZ döner. Önceki onaylanmamış ('bekliyor') anahtar silinir. Panel şifreleme
// anahtarı hazır değilse hiçbir şey yazılmaz.
func AnahtarUret(ctx context.Context, db *sql.DB, hibrit bool, simdi time.Time) (dosyaAdi, metin string, b AnahtarBilgi, err error) {
	if SirHazir() != nil {
		return "", "", b, ErrSifreHazirDegil
	}
	u, err := AnahtarCifti(hibrit)
	if err != nil {
		return "", "", b, err
	}
	parmak := Parmak(u.Alici)
	kapali := SirSakla(u.Ozel, parmak)
	if !gizli.SifreliMi(kapali) {
		return "", "", b, ErrSifreHazirDegil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", b, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `DELETE FROM backup_sifre_anahtarlari WHERE durum='bekliyor'`); err != nil {
		return "", "", b, err
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO backup_sifre_anahtarlari (parmak, alici, gizli_anahtar, durum) VALUES (?,?,?,'bekliyor')`,
		parmak, u.Alici, kapali)
	if err != nil {
		return "", "", b, err
	}
	id, _ := res.LastInsertId()
	if err = tx.Commit(); err != nil {
		return "", "", b, err
	}
	dosyaAdi, metin = KurtarmaDosyasi(u, simdi)
	b = AnahtarBilgi{ID: id, Parmak: parmak, KisaParmak: KisaParmak(parmak), Alici: u.Alici, Durum: "bekliyor",
		Sifreleme: SifrelemeTuru(u.Alici), SunucudaOzel: true}
	return dosyaAdi, metin, b, nil
}

// ErrAnahtarBulunamadi — onaylanacak bekleyen anahtar yok.
var ErrAnahtarBulunamadi = errors.New("onaylanacak bekleyen anahtar bulunamadı")

// AnahtarOnayla — kullanıcı kurtarma dosyasını sakladığını onayladı: anahtar etkin
// olur, önceki etkin anahtar 'emekli' (eski kopyaları çözmek için kalır).
func AnahtarOnayla(ctx context.Context, db *sql.DB, parmak string, indirildi bool) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var id int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM backup_sifre_anahtarlari WHERE parmak=? AND durum='bekliyor' FOR UPDATE`, parmak).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrAnahtarBulunamadi
		}
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE backup_sifre_anahtarlari SET durum='emekli' WHERE durum='etkin'`); err != nil {
		return err
	}
	ind := "NULL"
	if indirildi {
		ind = "NOW()"
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE backup_sifre_anahtarlari SET durum='etkin', kurtarma_onay=NOW(), kurtarma_indirildi=`+ind+` WHERE id=?`, id); err != nil { //nolint:gosec // G202: ind SABİT iki değerden biri
		return err
	}
	return tx.Commit()
}

// ErrAnahtarVar — bu açık anahtar zaten kayıtlı.
var ErrAnahtarVar = errors.New("bu açık anahtar zaten kayıtlı")

// AnahtarIceAktar — kullanıcı kendi açık anahtarını verir (özel anahtar kullanıcıda
// kalır, sunucuya GİRMEZ). Anında etkin olur; kurtarma sorumluluğu kullanıcıdadır.
func AnahtarIceAktar(ctx context.Context, db *sql.DB, alici string) (AnahtarBilgi, error) {
	alici = strings.TrimSpace(alici)
	if _, err := AliciCoz(alici); err != nil {
		return AnahtarBilgi{}, err
	}
	parmak := Parmak(alici)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return AnahtarBilgi{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM backup_sifre_anahtarlari WHERE parmak=?`, parmak).Scan(&n); err != nil {
		return AnahtarBilgi{}, err
	}
	if n > 0 {
		return AnahtarBilgi{}, ErrAnahtarVar
	}
	if _, err := tx.ExecContext(ctx, `UPDATE backup_sifre_anahtarlari SET durum='emekli' WHERE durum='etkin'`); err != nil {
		return AnahtarBilgi{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM backup_sifre_anahtarlari WHERE durum='bekliyor'`); err != nil {
		return AnahtarBilgi{}, err
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO backup_sifre_anahtarlari (parmak, alici, gizli_anahtar, durum, kurtarma_onay) VALUES (?,?,'','etkin',NOW())`,
		parmak, alici)
	if err != nil {
		return AnahtarBilgi{}, err
	}
	id, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		return AnahtarBilgi{}, err
	}
	return AnahtarBilgi{ID: id, Parmak: parmak, KisaParmak: KisaParmak(parmak), Alici: alici, Durum: "etkin",
		Sifreleme: SifrelemeTuru(alici)}, nil
}
