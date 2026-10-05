package uzak

// ortam_test.go — ortak test iskeleti. Gerçek MariaDB gerekir: YALNIZ geçici
// sunucu (GOSP_TEST_GECICI_DSN); yoksa DB'li testler ATLANIR. Gerçek Google'a
// gidilmez: hedef hedeftest.Bellek'tir.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"girginospanel/internal/backups/hedef"
	"girginospanel/internal/backups/hedef/hedeftest"

	"github.com/go-sql-driver/mysql"
)

const (
	gocYolu113 = "../../../migrations/0113_yedek_uzak_hedefler.sql"
	gocYolu140 = "../../../migrations/0140_yedek_uzak_kopya.sql"
	gocYolu141 = "../../../migrations/0141_yedek_yerel_sil.sql"
)

func gocUygula(t *testing.T, db *sql.DB, yol string) {
	t.Helper()
	body, err := os.ReadFile(yol)
	if err != nil {
		t.Fatal(err)
	}
	var temiz []string
	for _, satir := range strings.Split(string(body), "\n") {
		s := strings.TrimSpace(satir)
		if s == "" || strings.HasPrefix(s, "--") {
			continue
		}
		temiz = append(temiz, satir)
	}
	for _, ifade := range strings.Split(strings.Join(temiz, "\n"), ";") {
		s := strings.TrimSpace(ifade)
		if s == "" {
			continue
		}
		if _, err := db.Exec(s); err != nil {
			m := strings.ToLower(err.Error())
			if strings.Contains(m, "duplicate column") || strings.Contains(m, "duplicate key") ||
				strings.Contains(m, "already exists") || strings.Contains(m, "duplicate entry") {
				continue
			}
			t.Fatalf("GÖÇ HATASI (%s): %v\n%s", yol, err, s)
		}
	}
}

func gecicidb(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li test atlandı (canlı MariaDB'ye ASLA gidilmez)")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("DSN çözülemedi: %v", err)
	}
	cfg.DBName = ""
	kok, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	ad := fmt.Sprintf("gosp_test_uzak_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		_ = kok.Close()
		t.Fatalf("geçici veritabanı: %v", err)
	}
	t.Cleanup(func() {
		_, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad)
		_ = kok.Close()
	})
	cfg.DBName = ad
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = db.Close() })
	for _, q := range []string{
		`CREATE TABLE domains (id BIGINT UNSIGNED NOT NULL PRIMARY KEY, alan_adi VARCHAR(255) NOT NULL DEFAULT '', sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '') ENGINE=InnoDB`,
		`CREATE TABLE backups (
			id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
			domain_id BIGINT UNSIGNED NOT NULL,
			tip VARCHAR(16) NOT NULL DEFAULT 'tam',
			dosya VARCHAR(255) NOT NULL,
			boyut_b BIGINT NOT NULL DEFAULT 0,
			job_id BIGINT NULL,
			sha256 CHAR(64) NOT NULL DEFAULT '',
			KEY ix_backup_domain (domain_id),
			CONSTRAINT fk_backup_domain FOREIGN KEY (domain_id) REFERENCES domains(id) ON DELETE CASCADE
		) ENGINE=InnoDB`,
		`CREATE TABLE backup_jobs (id BIGINT AUTO_INCREMENT PRIMARY KEY, islem VARCHAR(16) NOT NULL DEFAULT 'yedek', durum VARCHAR(16) NOT NULL DEFAULT 'calisiyor') ENGINE=InnoDB`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	gocUygula(t, db, gocYolu113)
	gocUygula(t, db, gocYolu140)
	gocUygula(t, db, gocYolu141)
	return db
}

// sarmal — Bellek'i saran, davranışı ölçen/bozabilen test hedefi.
type sarmal struct {
	hedef.Hedef
	bel      *hedeftest.Bellek
	sunucuSh bool // false → SunucuSha256 yeteneği gizlenir (Drive gibi)

	mu       sync.Mutex
	secimler []hedef.YukleSecenek
	aktif    int32
	enCok    int32
	bekle    time.Duration
	// yuklemeSonrasi — nihai ada yükleme BAŞARIYLA bitince çağrılır (bozma benzetimi).
	yuklemeSonrasi func(ad string)
	// manifestHata — yalnız manifest yüklemesi bu hatayla başarısız olur.
	manifestHata error
}

func (s *sarmal) Yetenekler() hedef.Yetenek {
	y := s.Hedef.Yetenekler()
	if !s.sunucuSh {
		y &^= hedef.YetenekSunucuSha256
	}
	return y
}

func (s *sarmal) Yukle(ctx context.Context, yerel, ad string, sec hedef.YukleSecenek) error {
	n := atomic.AddInt32(&s.aktif, 1)
	for {
		c := atomic.LoadInt32(&s.enCok)
		if n <= c || atomic.CompareAndSwapInt32(&s.enCok, c, n) {
			break
		}
	}
	defer atomic.AddInt32(&s.aktif, -1)
	s.mu.Lock()
	s.secimler = append(s.secimler, sec)
	bk := s.bekle
	s.mu.Unlock()
	if bk > 0 {
		time.Sleep(bk)
	}
	if s.manifestHata != nil && strings.HasSuffix(ad, ".manifest.json") {
		return s.manifestHata
	}
	err := s.Hedef.Yukle(ctx, yerel, ad, sec)
	if err == nil && s.yuklemeSonrasi != nil {
		s.yuklemeSonrasi(ad)
	}
	return err
}

type ortam struct {
	t          *testing.T
	db         *sql.DB
	isci       *Isci
	bel        *hedeftest.Bellek
	sar        *sarmal
	kok        string
	gecici     string
	ozel       string // AGE özel anahtar dosyası içeriği (test içinde; sızıntı taramasında aranır)
	anahtarYol string
	hedefID    int64
	uyarilar   []string
	umu        sync.Mutex
}

func sirSaklaSahte(duz, baglam string) string {
	return "gos1:" + base64.RawStdEncoding.EncodeToString([]byte(baglam+"\x00"+duz))
}

func sirCozSahte(d, baglam string) string {
	if !strings.HasPrefix(d, "gos1:") {
		return d
	}
	b, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(d, "gos1:"))
	if err != nil || !strings.HasPrefix(string(b), baglam+"\x00") {
		return d
	}
	return strings.TrimPrefix(string(b), baglam+"\x00")
}

func yeniOrtam(t *testing.T) *ortam {
	t.Helper()
	db := gecicidb(t)
	s0, c0, h0 := SirSakla, SirCoz, SirHazir
	t.Cleanup(func() { SirSakla, SirCoz, SirHazir = s0, c0, h0 })
	SirSakla, SirCoz, SirHazir = sirSaklaSahte, sirCozSahte, func() error { return nil }

	o := &ortam{t: t, db: db, kok: t.TempDir(), gecici: filepath.Join(t.TempDir(), "gecici"), bel: hedeftest.YeniBellek()}
	o.sar = &sarmal{Hedef: o.bel, bel: o.bel}
	if _, err := db.Exec(`INSERT INTO backup_hedefler (ad, tur, ayar, sirlar) VALUES ('test hedefi', 'bellek', '{}', '')`); err != nil {
		t.Fatal(err)
	}
	_ = db.QueryRow(`SELECT id FROM backup_hedefler LIMIT 1`).Scan(&o.hedefID)
	o.isci = &Isci{
		DB: db, KokDizin: o.kok, GeciciDizin: o.gecici, PanelSurum: "test-1",
		HedefAc: func(ctx context.Context, id int64) (hedef.Hedef, error) { return o.sar, nil },
		Uyari: func(b, m string, d, y int64) {
			o.umu.Lock()
			o.uyarilar = append(o.uyarilar, b+"|"+m)
			o.umu.Unlock()
		},
		Bekleme: func(int) time.Duration { return time.Hour },
	}
	o.isci.Hazirla()
	// etkin anahtar: üret + onayla; özel anahtar kurtarma dosyasından alınır.
	ad, metin, b, err := AnahtarUret(context.Background(), db, false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	_ = ad
	o.ozel = metin
	o.anahtarYol = filepath.Join(t.TempDir(), "anahtar.txt")
	if err := os.WriteFile(o.anahtarYol, []byte(metin), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AnahtarOnayla(context.Background(), db, b.Parmak, true); err != nil {
		t.Fatal(err)
	}
	return o
}

func (o *ortam) ayar(f func(a *Ayar)) {
	o.t.Helper()
	a := Ayar{Aktif: true, HedefID: o.hedefID, Klasor: "gpanel", SaklamaN: 7}
	if f != nil {
		f(&a)
	}
	if err := AyarYaz(context.Background(), o.db, a); err != nil {
		o.t.Fatalf("AyarYaz: %v", err)
	}
}

func (o *ortam) domain(id int64, sk string) {
	o.t.Helper()
	if _, err := o.db.Exec(`INSERT IGNORE INTO domains (id, alan_adi, sistem_kullanici) VALUES (?,?,?)`, id, sk+".example", sk); err != nil {
		o.t.Fatal(err)
	}
}

// yedek — yerel yedek dosyası + backups satırı. tip 'oto' = kök sistem yedeği.
func (o *ortam) yedek(domainID int64, sk, damga string, icerik []byte, tip string, job any) int64 {
	o.t.Helper()
	o.domain(domainID, sk)
	dosya := fmt.Sprintf("%s-%s.tar.gz", sk, damga)
	dir := filepath.Join(o.kok, sk)
	_ = os.MkdirAll(dir, 0o700)
	if err := os.WriteFile(filepath.Join(dir, dosya), icerik, 0o600); err != nil {
		o.t.Fatal(err)
	}
	// yerel silme "en az 10 dk eski" ister: yedek 1 saat önce alınmış gibi
	eski := time.Now().Add(-time.Hour)
	_ = os.Chtimes(filepath.Join(dir, dosya), eski, eski)
	h := sha256.Sum256(icerik)
	res, err := o.db.Exec(`INSERT INTO backups (domain_id, tip, dosya, boyut_b, job_id, sha256) VALUES (?,?,?,?,?,?)`,
		domainID, tip, dosya, len(icerik), job, hex.EncodeToString(h[:]))
	if err != nil {
		o.t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func (o *ortam) kuyrukla(yedekID int64) int64 {
	o.t.Helper()
	id, _, err := KuyrugaAl(context.Background(), o.db, yedekID)
	if err != nil {
		o.t.Fatalf("KuyrugaAl: %v", err)
	}
	return id
}

// calis — kuyruk boşalana dek işçiyi koşturur (en çok 50 tur).
func (o *ortam) calis() int {
	o.t.Helper()
	n := 0
	for ; n < 50; n++ {
		ok, err := o.isci.Bir(context.Background())
		if err != nil {
			o.t.Fatalf("Bir: %v", err)
		}
		if !ok {
			break
		}
	}
	return n
}

type isDurum struct {
	durum, sonHata, kat, uzakAd, manifestAd, shaS, shaD string
	deneme                                              int
	boyutS, boyutD                                      int64
}

func (o *ortam) is(id int64) isDurum {
	o.t.Helper()
	var d isDurum
	err := o.db.QueryRow(`SELECT durum, son_hata, hata_kategori, uzak_ad, manifest_ad, sha256_sifreli, sha256_duz, deneme, boyut_b, boyut_duz
		FROM backup_uzak_kopyalar WHERE id=?`, id).Scan(&d.durum, &d.sonHata, &d.kat, &d.uzakAd, &d.manifestAd, &d.shaS, &d.shaD, &d.deneme, &d.boyutS, &d.boyutD)
	if err != nil {
		o.t.Fatal(err)
	}
	return d
}
