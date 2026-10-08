package uzak

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
)

// Hatalar.
var (
	ErrIsYok           = errors.New("iş bulunamadı")
	ErrDurumUygunDegil = errors.New("işin durumu bu işlem için uygun değil")
	ErrZatenGonderildi = errors.New("bu yedek zaten uzağa gönderildi")
	ErrYedekYok        = errors.New("yedek kaydı bulunamadı")
)

// KokYedegiMi — uzağa gidebilecek yedek mi? Yalnız SİSTEM yedekleri: zamanlayıcı
// ('oto', eski bash 'planli') ve kök toplu işi (job_id dolu). Müşterinin (ya da
// panelden elle alınan) manuel yedekleri tip='tam', job_id NULL olarak yazılır ve
// UZAĞA GİTMEZ (backups.yedekKaynagi ile aynı kural).
//
// 🔴 'kurtarma' (silinmiş hesaptan domaine bağlanan eski yedek) job_id taşısa bile ASLA aday değildir:
// eski yedeği kullanıcı istemeden uzağa itmek ve ardından yerel kopyayı silmek (yerel-silme) beklenmedik olur.
func KokYedegiMi(tip string, jobIDVar bool) bool {
	if tip == "kurtarma" {
		return false
	}
	return tip == "oto" || tip == "planli" || jobIDVar
}

// Gönderim durumları.
const (
	DurumBekliyor     = "bekliyor"
	DurumSifreleniyor = "sifreleniyor"
	DurumYukleniyor   = "yukleniyor"
	DurumDogrulaniyor = "dogrulaniyor"
	DurumTamam        = "tamam"
	DurumHata         = "hata"
	DurumIptal        = "iptal"
	DurumSiliniyor    = "siliniyor"
	DurumSilindi      = "silindi"
)

// KuyrugaAl — yerel yedeği uzak kopya kuyruğuna yazar. Kapalıysa ErrKapali (çağıran
// sessizce geçer), etkin anahtar yoksa ErrAnahtarYok, müşteri yedeğiyse ErrKokDegil.
// Aynı yedek+hedef için ikinci çağrı yeni satır AÇMAZ: var olan iş döner (yeni=false).
func KuyrugaAl(ctx context.Context, db *sql.DB, yedekID int64) (isID int64, yeni bool, err error) {
	a, err := AyarOku(ctx, db)
	if err != nil {
		return 0, false, err
	}
	if !a.Aktif {
		return 0, false, ErrKapali
	}
	k, err := EtkinAnahtar(ctx, db)
	if err != nil {
		return 0, false, err
	}
	if k == nil {
		return 0, false, ErrAnahtarYok
	}
	var domainID int64
	var dosya, tip string
	var jobVar int
	err = db.QueryRowContext(ctx, `SELECT domain_id, dosya, tip, job_id IS NOT NULL FROM backups b WHERE b.id=? AND EXISTS (SELECT 1 FROM domains d WHERE d.id=b.domain_id)`, yedekID).
		Scan(&domainID, &dosya, &tip, &jobVar)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, ErrYedekYok
	}
	if err != nil {
		return 0, false, err
	}
	if !KokYedegiMi(tip, jobVar == 1) {
		return 0, false, ErrKokDegil
	}
	err = db.QueryRowContext(ctx,
		`SELECT id FROM backup_uzak_kopyalar WHERE backup_id=? AND kaynak='hedef' AND hedef_id=?`, yedekID, a.HedefID).Scan(&isID)
	if err == nil {
		return isID, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}
	res, err := db.ExecContext(ctx,
		`INSERT INTO backup_uzak_kopyalar (backup_id, domain_id, kaynak, hedef_id, durum, yedek_dosya, sifreleme, anahtar_parmak)
		 VALUES (?,?,'hedef',?,'bekliyor',?,?,?)`,
		yedekID, domainID, a.HedefID, dosya, k.Sifreleme, k.Parmak)
	if err != nil {
		return 0, false, err
	}
	isID, _ = res.LastInsertId()
	return isID, true, nil
}

// YenidenDene — 'hata' ya da 'iptal' işi yeniden kuyruğa alır (deneme sayacı sıfırlanır).
func YenidenDene(ctx context.Context, db *sql.DB, isID int64) error {
	res, err := db.ExecContext(ctx,
		`UPDATE backup_uzak_kopyalar SET durum='bekliyor', deneme=0, son_hata='', hata_kategori='', sonraki_deneme=NULL
		 WHERE id=? AND kaynak='hedef' AND durum IN ('hata','iptal')`, isID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil
	}
	var d string
	if err := db.QueryRowContext(ctx, `SELECT durum FROM backup_uzak_kopyalar WHERE id=? AND kaynak='hedef'`, isID).Scan(&d); errors.Is(err, sql.ErrNoRows) {
		return ErrIsYok
	}
	return ErrDurumUygunDegil
}

// SimdiGonder — tek yedeği uzağa gönderilmek üzere sıraya alır (uç ve toplu gönderim
// ortak çekirdeği). Var olan iş: hata/iptalse yeniden denenir, tamamsa ErrZatenGonderildi,
// sürüyorsa aynen bırakılır.
func SimdiGonder(ctx context.Context, db *sql.DB, yedekID int64) (int64, error) {
	isID, yeni, err := KuyrugaAl(ctx, db, yedekID)
	if err != nil {
		return 0, err
	}
	if !yeni {
		var durum string
		_ = db.QueryRowContext(ctx, `SELECT durum FROM backup_uzak_kopyalar WHERE id=?`, isID).Scan(&durum)
		switch durum {
		case DurumTamam:
			return isID, ErrZatenGonderildi
		case DurumHata, DurumIptal:
			if err := YenidenDene(ctx, db, isID); err != nil {
				return isID, err
			}
		}
	}
	return isID, nil
}

// Is — işler listesi satırı (SIRSIZ).
type Is struct {
	ID            int64  `json:"id"`
	YedekID       int64  `json:"yedek_id,omitempty"`
	DomainID      int64  `json:"domain_id"`
	Domain        string `json:"domain,omitempty"`
	YedekDosya    string `json:"yedek_dosya"`
	Durum         string `json:"durum"`
	Deneme        int    `json:"deneme"`
	SonHata       string `json:"son_hata,omitempty"`
	BoyutSifreli  int64  `json:"boyut_sifreli"`
	BoyutDuz      int64  `json:"boyut_duz"`
	Yuklenen      int64  `json:"yuklenen_b"`
	Sha256Sifreli string `json:"sha256_sifreli,omitempty"`
	Sha256Duz     string `json:"sha256_duz,omitempty"`
	Sifreleme     string `json:"sifreleme,omitempty"`
	AnahtarParmak string `json:"anahtar_parmak,omitempty"`
	UzakAd        string `json:"uzak_ad,omitempty"`
	SonrakiDeneme string `json:"sonraki_deneme,omitempty"`
	Olusturma     string `json:"olusturma"`
	Bitis         string `json:"bitis,omitempty"`
	SonDogrulama  string `json:"son_dogrulama,omitempty"`
}

// IsListe — işleri yeniden eskiye sayfalı döner (adet 1-100). durum boş = hepsi.
func IsListe(ctx context.Context, db *sql.DB, sayfa, adet int, durum string) (liste []Is, toplam int, err error) {
	if adet < 1 || adet > 100 {
		adet = 20
	}
	if sayfa < 1 {
		sayfa = 1
	}
	where := ` WHERE k.kaynak='hedef'`
	var args []any
	if durum != "" {
		where += ` AND k.durum=?`
		args = append(args, durum)
	}
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM backup_uzak_kopyalar k`+where, args...).Scan(&toplam); err != nil {
		return nil, 0, err
	}
	q := `SELECT k.id, COALESCE(k.backup_id,0), k.domain_id, COALESCE(d.alan_adi,''), k.yedek_dosya, k.durum, k.deneme, k.son_hata,
		k.boyut_b, k.boyut_duz, k.yuklenen_b, k.sha256_sifreli, k.sha256_duz, k.sifreleme, k.anahtar_parmak, k.uzak_ad,
		COALESCE(DATE_FORMAT(k.sonraki_deneme,'%Y-%m-%d %H:%i'),''), DATE_FORMAT(k.created_at,'%Y-%m-%d %H:%i'),
		COALESCE(DATE_FORMAT(k.bitis_at,'%Y-%m-%d %H:%i'),''), COALESCE(DATE_FORMAT(k.son_dogrulama,'%Y-%m-%d %H:%i'),'')
		FROM backup_uzak_kopyalar k LEFT JOIN domains d ON d.id=k.domain_id` + where + ` ORDER BY k.id DESC LIMIT ? OFFSET ?`
	rows, err := db.QueryContext(ctx, q, append(args, adet, (sayfa-1)*adet)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	liste = []Is{}
	for rows.Next() {
		var s Is
		if err := rows.Scan(&s.ID, &s.YedekID, &s.DomainID, &s.Domain, &s.YedekDosya, &s.Durum, &s.Deneme, &s.SonHata,
			&s.BoyutSifreli, &s.BoyutDuz, &s.Yuklenen, &s.Sha256Sifreli, &s.Sha256Duz, &s.Sifreleme, &s.AnahtarParmak, &s.UzakAd,
			&s.SonrakiDeneme, &s.Olusturma, &s.Bitis, &s.SonDogrulama); err != nil {
			return nil, 0, err
		}
		s.AnahtarParmak = KisaParmak(s.AnahtarParmak)
		liste = append(liste, s)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	rows.Close()
	silinmisAdlariTamamla(ctx, db, liste)
	return liste, toplam, nil
}

// silinmisAdlariTamamla — D8: silinmiş hostingin işinde domains satırı yok → alan adı boş kalırdı. Yetim yedek satırı alan adını
// silme anında taşır (backups.alan_adi, 0217): onunla doldurulur. Ayrı, HATA-TOLERANSLI sorgu: bu kolon olmayan bir şemada
// (0217 uygulanmamış) iş listesi yine açılır, yalnız alan adı boş kalır.
func silinmisAdlariTamamla(ctx context.Context, db *sql.DB, liste []Is) {
	var idler []any
	sira := map[int64][]int{}
	for i, s := range liste {
		if s.Domain == "" && s.YedekID > 0 {
			idler = append(idler, s.YedekID)
			sira[s.YedekID] = append(sira[s.YedekID], i)
		}
	}
	if len(idler) == 0 {
		return
	}
	rows, err := db.QueryContext(ctx, `SELECT id, alan_adi FROM backups WHERE alan_adi<>'' AND id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(idler)), ",")+`)`, idler...)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var ad string
		if rows.Scan(&id, &ad) != nil {
			continue
		}
		for _, i := range sira[id] {
			liste[i].Domain = ad
		}
	}
}

// Ozet — durum başına iş sayısı (arayüz rozetleri).
func Ozet(ctx context.Context, db *sql.DB) (map[string]int, error) {
	rows, err := db.QueryContext(ctx, `SELECT durum, COUNT(*) FROM backup_uzak_kopyalar WHERE kaynak='hedef' GROUP BY durum`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var d string
		var n int
		if err := rows.Scan(&d, &n); err != nil {
			return nil, err
		}
		out[d] = n
	}
	return out, rows.Err()
}

var tarihDamgasi = regexp.MustCompile(`-(\d{8})-\d{6}`)

// UzakAdUret — <klasor>/<YYYY-MM-DD>/<dosya>.age. Tarih dosya adındaki damgadan;
// çözülemezse "tarihsiz".
func UzakAdUret(klasor, dosya string) string {
	gun := "tarihsiz"
	if m := tarihDamgasi.FindStringSubmatch(dosya); len(m) == 2 {
		d := m[1]
		gun = d[0:4] + "-" + d[4:6] + "-" + d[6:8]
	}
	return strings.Trim(klasor, "/") + "/" + gun + "/" + dosya + ".age"
}

// ManifestAdi — kopyanın manifest dosyası.
func ManifestAdi(uzakAd string) string { return uzakAd + ".manifest.json" }
