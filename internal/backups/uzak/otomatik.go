package uzak

// otomatik.go — "Plesk gibi sade" akış: şifreleme anahtarı kullanıcıdan istenmez.
//
// Uzak kopya ilk kez açılırken etkin anahtar yoksa SUNUCU klasik anahtar üretir ve
// etkinleştirir. Özel anahtar yalnız mevcut şifreli DB kolonunda (gizli.SaklaBagli)
// durur; kurtarma dosyası sonradan, yalnız yönetici isteğiyle indirilir.
//
// 🔴 Özel anahtar günlüğe, hata metnine, denetime ve durum yanıtına GİRMEZ; yalnız
// KurtarmaAnahtarlari'nın döndürdüğü dosya metninde bulunur.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"girginospanel/internal/gizli"
)

// EtkinKur — etkin bir anahtar sağlar. zorla=false: etkin anahtar varsa DOKUNMAZ
// (idempotent), yoksa üretip etkinleştirir; eşzamanlı çağrılarda etkin_tekil UNIQUE
// sayesinde yalnız BİR anahtar kalır (kaybeden kazananın anahtarını döner).
// zorla=true: yeni anahtar üretir, eskisini 'emekli' yapar (eski kopyalar için saklı kalır).
func EtkinKur(ctx context.Context, db *sql.DB, hibrit, zorla bool) (b AnahtarBilgi, uretildi bool, err error) {
	if SirHazir() != nil {
		return b, false, ErrSifreHazirDegil
	}
	if !zorla {
		k, err := EtkinAnahtar(ctx, db)
		if err != nil {
			return b, false, err
		}
		if k != nil {
			return *k, false, nil
		}
	}
	u, err := AnahtarCifti(hibrit)
	if err != nil {
		return b, false, err
	}
	parmak := Parmak(u.Alici)
	kapali := SirSakla(u.Ozel, parmak)
	if !gizli.SifreliMi(kapali) {
		return b, false, ErrSifreHazirDegil
	}
	b, err = etkinEkle(ctx, db, u, parmak, kapali, zorla)
	if err != nil {
		if !zorla {
			// Yarışı kaybettik olabilir: kazananın etkin anahtarı varsa onu kullan.
			if k, e2 := EtkinAnahtar(ctx, db); e2 == nil && k != nil {
				return *k, false, nil
			}
		}
		return AnahtarBilgi{}, false, err
	}
	return b, true, nil
}

func etkinEkle(ctx context.Context, db *sql.DB, u Uretilen, parmak, kapali string, zorla bool) (AnahtarBilgi, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return AnahtarBilgi{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if zorla {
		if _, err = tx.ExecContext(ctx, `UPDATE backup_sifre_anahtarlari SET durum='emekli' WHERE durum='etkin'`); err != nil {
			return AnahtarBilgi{}, err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM backup_sifre_anahtarlari WHERE durum='bekliyor'`); err != nil {
		return AnahtarBilgi{}, err
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO backup_sifre_anahtarlari (parmak, alici, gizli_anahtar, durum) VALUES (?,?,?,'etkin')`,
		parmak, u.Alici, kapali)
	if err != nil {
		return AnahtarBilgi{}, err
	}
	id, _ := res.LastInsertId()
	if err = tx.Commit(); err != nil {
		return AnahtarBilgi{}, err
	}
	return AnahtarBilgi{ID: id, Parmak: parmak, KisaParmak: KisaParmak(parmak), Alici: u.Alici, Durum: "etkin",
		Sifreleme: SifrelemeTuru(u.Alici), SunucudaOzel: true}, nil
}

// AyarYazAnahtarli — AyarYaz + açılışta etkin anahtar yoksa otomatik üretim.
// uretildi: bu çağrıda yeni anahtar üretildi mi (denetim için).
func AyarYazAnahtarli(ctx context.Context, db *sql.DB, a Ayar) (uretildi bool, err error) {
	if err := ayarKontrol(ctx, db, &a); err != nil {
		return false, err
	}
	if a.Aktif {
		_, uretildi, err = EtkinKur(ctx, db, false, false)
		if err != nil {
			return false, err
		}
	}
	return uretildi, ayarKaydet(ctx, db, a)
}

// ─── kurtarma anahtarı (isteğe bağlı indirme) ───────────────────────────────

// KurtarmaAnahtarlari — sunucuda özel anahtarı saklı TÜM anahtarları (etkin + emekli,
// eski kopyalar da çözülebilsin) tek dosya metni olarak döner ve indirme zamanını işler.
// adet=0 → ErrAnahtarYok. Çözülemeyen kayıt sessizce atlanmaz: hata döner.
func KurtarmaAnahtarlari(ctx context.Context, db *sql.DB, simdi time.Time) (ad, metin string, adet int, err error) {
	rows, err := db.QueryContext(ctx,
		`SELECT parmak, alici, gizli_anahtar, durum FROM backup_sifre_anahtarlari WHERE gizli_anahtar <> '' ORDER BY id DESC`)
	if err != nil {
		return "", "", 0, err
	}
	defer rows.Close()
	var govde strings.Builder
	var etkinParmak string
	for rows.Next() {
		var parmak, alici, kapali, durum string
		if err := rows.Scan(&parmak, &alici, &kapali, &durum); err != nil {
			return "", "", 0, err
		}
		ozel := SirCoz(kapali, parmak)
		if !strings.HasPrefix(ozel, "AGE-SECRET-KEY") {
			return "", "", 0, ErrSifreHazirDegil
		}
		fmt.Fprintf(&govde, "# anahtar: %s (%s) — %s\n", KisaParmak(parmak), SifrelemeTuru(alici), durum)
		govde.WriteString("# açık anahtar (alıcı): " + alici + "\n")
		govde.WriteString(ozel + "\n")
		if durum == "etkin" {
			etkinParmak = parmak
		}
		adet++
	}
	if err := rows.Err(); err != nil {
		return "", "", 0, err
	}
	if adet == 0 {
		return "", "", 0, ErrAnahtarYok
	}
	var b strings.Builder
	b.WriteString("# GirginOSPanel yedek kurtarma anahtarı\n")
	b.WriteString("# Bu anahtar OLMADAN uzak yedek kopyaları GERİ YÜKLENEMEZ. Güvenli bir yerde saklayın.\n")
	fmt.Fprintf(&b, "# oluşturma: %s\n", simdi.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "# anahtar sayısı: %d (eski kopyalar için eski anahtarlar da dahildir)\n", adet)
	fmt.Fprintf(&b, "# age sürümü: %s\n", AgeSurum())
	b.WriteString("# Çözme: girginospanel-server yedek-coz <dosya.age> --anahtar <bu dosya>   (ya da: age -d -i <bu dosya> <dosya.age>)\n")
	b.WriteString(govde.String())
	if _, err := db.ExecContext(ctx, `UPDATE backup_sifre_anahtarlari SET kurtarma_indirildi=NOW() WHERE gizli_anahtar <> ''`); err != nil {
		return "", "", 0, err
	}
	kisa := KisaParmak(etkinParmak)
	if kisa == "" {
		kisa = "anahtar"
	}
	return kurtarmaDosyaOn + kisa + kurtarmaDosyaSon, b.String(), adet, nil
}

// ─── sade durum özeti ───────────────────────────────────────────────────────

// Gonderim — durum kartının özeti.
type Gonderim struct {
	Gonderilen  int    `json:"gonderilen"`
	Bekleyen    int    `json:"bekleyen"`
	Hata        int    `json:"hata"`
	SonGonderim string `json:"son_gonderim"`
}

// GonderimOzeti — uzak kopya işlerinden sade özet (hata/bekleyen: sürmekte olanlar).
func GonderimOzeti(ctx context.Context, db *sql.DB) (Gonderim, error) {
	var g Gonderim
	err := db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(durum='tamam'),0), COALESCE(SUM(durum IN ('bekliyor','sifreleniyor','yukleniyor','dogrulaniyor')),0),
		        COALESCE(SUM(durum='hata'),0),
		        COALESCE(DATE_FORMAT(MAX(CASE WHEN durum='tamam' THEN bitis_at END),'%Y-%m-%d %H:%i'),'')
		 FROM backup_uzak_kopyalar WHERE kaynak='hedef'`).Scan(&g.Gonderilen, &g.Bekleyen, &g.Hata, &g.SonGonderim)
	return g, err
}

// YedekDurumlari — verilen yedek kimliklerinin uzak kopya durumu (yedek başına en yeni
// iş). Kuyruğa hiç alınmamış yedek haritada YOKTUR.
func YedekDurumlari(ctx context.Context, db *sql.DB, ids []int64) (map[int64]string, error) {
	out := map[int64]string{}
	if len(ids) == 0 {
		return out, nil
	}
	if len(ids) > 500 {
		ids = ids[:500]
	}
	args := make([]any, len(ids))
	ph := make([]string, len(ids))
	for i, id := range ids {
		args[i], ph[i] = id, "?"
	}
	rows, err := db.QueryContext(ctx,
		`SELECT k.backup_id, k.durum FROM backup_uzak_kopyalar k
		 JOIN (SELECT backup_id, MAX(id) mid FROM backup_uzak_kopyalar WHERE kaynak='hedef' AND backup_id IN (`+strings.Join(ph, ",")+`) GROUP BY backup_id) m
		 ON m.mid=k.id`, args...) //nolint:gosec // G202: ph yalnız "?" birleşimi
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var d string
		if err := rows.Scan(&id, &d); err != nil {
			return nil, err
		}
		out[id] = d
	}
	return out, rows.Err()
}

// ─── mevcut yedekleri toplu gönder ──────────────────────────────────────────

// Toplu gönderim sınırları.
const (
	TopluVarsayilanN = 7
	TopluEnFazlaN    = 100
)

// TopluOnizleme — onay penceresinin göstereceği özet.
type TopluOnizleme struct {
	N        int   `json:"n"`
	Adet     int   `json:"adet"`
	Domain   int   `json:"domain"`
	ToplamB  int64 `json:"toplam_b"`
	SaklamaN int   `json:"saklama_n"`
}

// TopluAdaylar — domain başına en yeni n KÖK yedeği (müşteri manuel yedekleri ASLA);
// bu hedefte zaten gönderilmiş/sürmekte olanlar (hata/iptal hariç) çıkarılır.
// Kimlikler ESKİDEN YENİYE döner: kuyruk sırası kronolojik olsun, saklama politikası
// (kuyruk sırasına göre) en yeni yedekleri korusun.
func TopluAdaylar(ctx context.Context, db *sql.DB, n int) ([]int64, TopluOnizleme, error) {
	on := TopluOnizleme{N: n}
	if n < 1 || n > TopluEnFazlaN {
		return nil, on, fmt.Errorf("%w: yedek sayısı 1-%d olmalı", ErrAyarGecersiz, TopluEnFazlaN)
	}
	a, err := AyarOku(ctx, db)
	if err != nil {
		return nil, on, err
	}
	on.SaklamaN = a.SaklamaN
	if a.HedefID <= 0 {
		return nil, on, ErrHedefYok
	}
	rows, err := db.QueryContext(ctx,
		`SELECT t.id, t.domain_id, t.boyut_b FROM (
		   SELECT b.id, b.domain_id, b.boyut_b, ROW_NUMBER() OVER (PARTITION BY b.domain_id ORDER BY b.id DESC) rn
		   FROM backups b WHERE b.tip<>'kurtarma' AND (b.tip IN ('oto','planli') OR b.job_id IS NOT NULL)
		     AND EXISTS (SELECT 1 FROM domains dd WHERE dd.id=b.domain_id)
		 ) t
		 WHERE t.rn <= ? AND NOT EXISTS (
		   SELECT 1 FROM backup_uzak_kopyalar k WHERE k.backup_id=t.id AND k.kaynak='hedef' AND k.hedef_id=? AND k.durum NOT IN ('hata','iptal'))
		 ORDER BY t.id ASC`, n, a.HedefID)
	if err != nil {
		return nil, on, err
	}
	defer rows.Close()
	var ids []int64
	alan := map[int64]bool{}
	for rows.Next() {
		var id, did, boyut int64
		if err := rows.Scan(&id, &did, &boyut); err != nil {
			return nil, on, err
		}
		ids = append(ids, id)
		on.ToplamB += boyut
		alan[did] = true
	}
	on.Adet, on.Domain = len(ids), len(alan)
	return ids, on, rows.Err()
}

// TopluKuyrugaAl — adayları sıraya alır (gönderimi tek işçi yapar). Zaten gönderilmiş
// ya da kuyruğa alınamayan yedek atlanır; kapalıysa ErrKapali.
func TopluKuyrugaAl(ctx context.Context, db *sql.DB, n int) (kuyruga, atlanan int, err error) {
	ids, _, err := TopluAdaylar(ctx, db, n)
	if err != nil {
		return 0, 0, err
	}
	for _, id := range ids {
		if _, err := SimdiGonder(ctx, db, id); err != nil {
			if errors.Is(err, ErrKapali) || errors.Is(err, ErrAnahtarYok) || errors.Is(err, ErrHedefYok) {
				return kuyruga, atlanan, err
			}
			atlanan++
			continue
		}
		kuyruga++
	}
	return kuyruga, atlanan, nil
}
