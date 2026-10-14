package dbgovernor

// depo.go — panel veritabanı katmanı: DB kullanıcısı → hosting/bayi eşlemesi, eşikler, kısıt
// kayıtları (db_cpu_kisit) ve ölçüm dilimleri (db_cpu_olcum). Motor yalnız Depo arayüzünü bilir;
// testler bellekte bir sahte Depo ile politikayı sınar, SQL'li gerçekleştirim ayrıca DB'li
// testlerle sınanır.

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/efektiflimit"
)

// Kimlik: bir DB kullanıcısının panel sahibi. Yok=true: kullanıcı panelde kayıtsız ya da birden
// çok hostinge kayıtlı (sahibi belirsiz) — ölçülmez, kısılmaz.
type Kimlik struct {
	DomainID   int64
	ResellerID int64
	AlanAdi    string
	Askida     bool
	Yok        bool
}

// OlcumSatiri: bir dilimde bir kullanıcının harcadığı CPU.
type OlcumSatiri struct {
	Kullanici string
	DomainID  int64
	CPUms     int64
}

// Depo: kısıcının panel DB'sine ihtiyaç duyduğu her şey.
type Depo interface {
	// Esle: kullanıcı → kimlik. Dönen haritada HER istenen kullanıcı bulunur (kayıtsız → Yok).
	Esle(ctx context.Context, kullanicilar []string) (map[string]Kimlik, error)
	// DomainKullanicilari: hostingin YALNIZ ona ait (başka hostingde kayıtlı olmayan) DB kullanıcıları.
	DomainKullanicilari(ctx context.Context, domainID int64) ([]string, error)
	// HostingEsik: hostingin DB CPU eşiği (çekirdek). Plan cpu_yuzde vermiyorsa varsayilan.
	HostingEsik(ctx context.Context, domainID int64, varsayilan float64) (float64, error)
	// BayiCPU: bayinin DB CPU sınırı (çekirdek); ok=false → sınırsız/tanımsız.
	BayiCPU(ctx context.Context, bayiID int64) (limit float64, ok bool, err error)
	// PlanBaglanti: planın MAX_USER_CONNECTIONS değeri; planli=false → hosting planlı değil (kaynaklimit
	// plansız hostinge MySQL limiti UYGULAMAZ).
	PlanBaglanti(ctx context.Context, domainID int64) (n int, planli bool, err error)

	// Kısıt kayıtları.
	Ac(ctx context.Context, k *Kisit) (int64, error)
	Guncelle(ctx context.Context, k *Kisit) error
	Kapat(ctx context.Context, id int64, ts int64, neden string) error
	AcikKayitlar(ctx context.Context) ([]*Kisit, error)

	// Ölçüm dilimleri.
	OlcumYaz(ctx context.Context, dilim int64, satirlar []OlcumSatiri) error
	Temizle(ctx context.Context, simdi time.Time) error
}

// ErrYok: kayıt bulunamadı.
var ErrYok = errors.New("kayıt yok")

// SQLDepo: Depo'nun panel DB gerçekleştirimi.
type SQLDepo struct {
	DB *sql.DB

	mu          sync.Mutex
	bayiSutunTS time.Time
	bayiSutun   bool
}

const eslemeParca = 200

func (d *SQLDepo) Esle(ctx context.Context, kullanicilar []string) (map[string]Kimlik, error) {
	out := make(map[string]Kimlik, len(kullanicilar))
	for _, u := range kullanicilar {
		out[u] = Kimlik{Yok: true}
	}
	for i := 0; i < len(kullanicilar); i += eslemeParca {
		son := i + eslemeParca
		if son > len(kullanicilar) {
			son = len(kullanicilar)
		}
		parca := kullanicilar[i:son]
		args := make([]any, len(parca))
		for j, u := range parca {
			args[j] = u
		}
		// 🔴 BINARY + tek domain: db_user UNIQUE değil ve db_accounts harf DUYARSIZ; MariaDB hesapları
		// harf DUYARLI. Aynı ad iki hostingde geçiyorsa sahibi belirsizdir (governorLimiti ile aynı kural).
		rows, err := d.DB.QueryContext(ctx,
			`SELECT a.db_user, MIN(a.domain_id), COALESCE(MIN(dm.reseller_id),0), COALESCE(MIN(dm.alan_adi),''), COALESCE(MAX(dm.askida),0)
			   FROM db_accounts a JOIN domains dm ON dm.id=a.domain_id
			  WHERE BINARY a.db_user IN (`+strings.TrimSuffix(strings.Repeat("?,", len(parca)), ",")+`)
			  GROUP BY BINARY a.db_user, a.db_user
			 HAVING COUNT(DISTINCT a.domain_id)=1`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var u, alan string
			var did, rid int64
			var askida int
			if err := rows.Scan(&u, &did, &rid, &alan, &askida); err != nil {
				rows.Close()
				return nil, err
			}
			out[u] = Kimlik{DomainID: did, ResellerID: rid, AlanAdi: alan, Askida: askida != 0}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (d *SQLDepo) DomainKullanicilari(ctx context.Context, domainID int64) ([]string, error) {
	rows, err := d.DB.QueryContext(ctx,
		`SELECT DISTINCT a.db_user FROM db_accounts a
		  WHERE a.domain_id=? AND a.db_user<>''
		    AND NOT EXISTS (SELECT 1 FROM db_accounts b WHERE BINARY b.db_user=BINARY a.db_user AND b.domain_id<>a.domain_id)`, domainID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var l []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		l = append(l, u)
	}
	return l, rows.Err()
}

func (d *SQLDepo) HostingEsik(ctx context.Context, domainID int64, varsayilan float64) (float64, error) {
	s, err := efektiflimit.Oku(ctx, d.DB, domainID)
	if err != nil {
		return varsayilan, err
	}
	if y := s.Efektif("cpu_yuzde", 0); y > 0 {
		e := float64(y) / 100
		if e < MinEsik {
			e = MinEsik
		}
		return e, nil
	}
	return varsayilan, nil
}

func (d *SQLDepo) PlanBaglanti(ctx context.Context, domainID int64) (int, bool, error) {
	s, err := efektiflimit.Oku(ctx, d.DB, domainID)
	if err != nil {
		return 0, false, err
	}
	// kaynaklimit.UygulaHepsi: plansiz = cpu_yuzde, ram_mb ve max_process hepsi 0 → MySQL limiti uygulanmaz.
	if s.Efektif("cpu_yuzde", 0) == 0 && s.Efektif("ram_mb", 0) == 0 && s.Efektif("max_process", 0) == 0 {
		return 0, false, nil
	}
	n := s.Efektif("mysql_max_baglanti", 0)
	if n < 0 {
		n = 0
	}
	return n, true, nil
}

// bayiSutunVarMi: users.cpu_cekirdek (bayi CPU sınırı; göç 0219) var mı? Sütun yoksa bayi
// sınırsız sayılır. Sonuç 5 dk önbelleklenir.
func (d *SQLDepo) bayiSutunVarMi(ctx context.Context) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.bayiSutunTS.IsZero() && time.Since(d.bayiSutunTS) < 5*time.Minute {
		return d.bayiSutun
	}
	var n int
	err := d.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='users' AND COLUMN_NAME='cpu_cekirdek'`).Scan(&n)
	if err != nil {
		return false // okunamadı: sınırsız say (kısma yok) ve önbellekleme
	}
	d.bayiSutun, d.bayiSutunTS = n > 0, time.Now()
	return d.bayiSutun
}

func (d *SQLDepo) BayiCPU(ctx context.Context, bayiID int64) (float64, bool, error) {
	if bayiID <= 0 || !d.bayiSutunVarMi(ctx) {
		return 0, false, nil
	}
	var v sql.NullFloat64
	if err := d.DB.QueryRowContext(ctx, `SELECT cpu_cekirdek FROM users WHERE id=? AND role='reseller'`, bayiID).Scan(&v); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, err
	}
	if !v.Valid || v.Float64 <= 0 {
		return 0, false, nil
	}
	return v.Float64, true, nil
}

func (d *SQLDepo) Ac(ctx context.Context, k *Kisit) (int64, error) {
	res, err := d.DB.ExecContext(ctx,
		`INSERT INTO db_cpu_kisit (db_user, domain_id, reseller_id, alan_adi, neden, seviye, onceki_baglanti, kisit_baglanti,
		    esik_milli, tepe_milli, oldurulen, kesilen, uygulandi, baslangic_ts, son_asim_ts, acik)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,1)`,
		k.Kullanici, k.Kimlik.DomainID, k.Kimlik.ResellerID, k.Kimlik.AlanAdi, k.nedenMetni(), k.Seviye, k.Onceki, k.KisitDeger,
		milli(k.enDusukEsik()), milli(k.Tepe), k.Oldurulen, k.Kesilen, boolInt(k.Uygulandi), k.Baslangic.Unix(), k.SonAsim.Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *SQLDepo) Guncelle(ctx context.Context, k *Kisit) error {
	_, err := d.DB.ExecContext(ctx,
		`UPDATE db_cpu_kisit SET neden=?, seviye=?, onceki_baglanti=?, kisit_baglanti=?, esik_milli=?, tepe_milli=?, oldurulen=?, kesilen=?, son_asim_ts=?
		  WHERE id=? AND acik=1`,
		k.nedenMetni(), k.Seviye, k.Onceki, k.KisitDeger, milli(k.enDusukEsik()), milli(k.Tepe), k.Oldurulen, k.Kesilen, k.SonAsim.Unix(), k.KayitID)
	return err
}

func (d *SQLDepo) Kapat(ctx context.Context, id int64, ts int64, neden string) error {
	_, err := d.DB.ExecContext(ctx,
		`UPDATE db_cpu_kisit SET acik=NULL, bitis_ts=?, bitis_neden=? WHERE id=? AND acik=1`, ts, neden, id)
	return err
}

func (d *SQLDepo) AcikKayitlar(ctx context.Context) ([]*Kisit, error) {
	rows, err := d.DB.QueryContext(ctx,
		`SELECT id, db_user, domain_id, reseller_id, alan_adi, neden, seviye, onceki_baglanti, kisit_baglanti,
		        esik_milli, tepe_milli, oldurulen, kesilen, uygulandi, baslangic_ts, son_asim_ts
		   FROM db_cpu_kisit WHERE acik=1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var l []*Kisit
	for rows.Next() {
		k := &Kisit{}
		var neden string
		var esik, tepe, uyg, bas, son int64
		if err := rows.Scan(&k.KayitID, &k.Kullanici, &k.Kimlik.DomainID, &k.Kimlik.ResellerID, &k.Kimlik.AlanAdi, &neden, &k.Seviye,
			&k.Onceki, &k.KisitDeger, &esik, &tepe, &k.Oldurulen, &k.Kesilen, &uyg, &bas, &son); err != nil {
			return nil, err
		}
		k.Uygulandi = uyg != 0
		k.Tepe = float64(tepe) / 1000
		k.Baslangic, k.SonAsim = time.Unix(bas, 0), time.Unix(son, 0)
		k.Birimler = nedenBirimleri(neden, k.Kimlik, float64(esik)/1000)
		l = append(l, k)
	}
	return l, rows.Err()
}

func (d *SQLDepo) OlcumYaz(ctx context.Context, dilim int64, satirlar []OlcumSatiri) error {
	if len(satirlar) == 0 {
		return nil
	}
	var sb strings.Builder
	sb.WriteString(`INSERT INTO db_cpu_olcum (dilim, db_user, domain_id, cpu_ms) VALUES `)
	args := make([]any, 0, len(satirlar)*4)
	for i, s := range satirlar {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString("(?,?,?,?)")
		args = append(args, dilim, s.Kullanici, s.DomainID, s.CPUms)
	}
	sb.WriteString(` ON DUPLICATE KEY UPDATE cpu_ms = cpu_ms + VALUES(cpu_ms), domain_id = VALUES(domain_id)`)
	_, err := d.DB.ExecContext(ctx, sb.String(), args...)
	return err
}

// Saklama süreleri.
const (
	olcumSaklama = 2 * 24 * time.Hour
	kisitSaklama = 90 * 24 * time.Hour
)

func (d *SQLDepo) Temizle(ctx context.Context, simdi time.Time) error {
	if _, err := d.DB.ExecContext(ctx, `DELETE FROM db_cpu_olcum WHERE dilim < ?`, simdi.Add(-olcumSaklama).Unix()); err != nil {
		return err
	}
	_, err := d.DB.ExecContext(ctx, `DELETE FROM db_cpu_kisit WHERE acik IS NULL AND bitis_ts < ?`, simdi.Add(-kisitSaklama).Unix())
	return err
}

func milli(c float64) int64 {
	if c < 0 {
		return 0
	}
	return int64(c*1000 + 0.5)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
