package backups

// silinen_duzeltme_test.go — doğrulama turu (BULGULAR-DOGRULAMA.md + koordinatör ekleri) kararlarının KİLİTLERİ:
//
//	O4  iz_id (benzersiz silme kimliği): id yeniden kullanımında yetimler/anlık görüntüler karışmaz
//	Y1  bağlama kilitlenmesi (1213/1205) yeniden denenir, hata yutulmaz; READ COMMITTED yedeği (1665)
//	O3  uzagi_yoksay yalnız UZAK kaynaklı hatada true; kısmi başarısızlık denetimi
//	D2  süpürme özet denetim satırı
//	O8  disk kapısı bildirimi silinmiş hesap yedeklerine yönlendirir
//	Y1  Put/TestDestination: hedef değişip parola boşsa 400
//	D5  lftp artık betikleri
//	D6  S2 toplu işaretleme eşiği
//	A2  YetimKalintilariniAyir
//	    Restore'un bağlanmış 'kurtarma' satırıyla uçtan uca çalışması
//
// Gerçek BackupRoot'a YAZILMAZ, AĞA ÇIKILMAZ. Geçici MariaDB gerekir (GOSP_TEST_GECICI_DSN yoksa atlanır).

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"girginospanel/internal/gizli"
	"girginospanel/internal/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"
)

// ---------------------------------------------------------------- hata enjeksiyonlu sürücü (sayaçlı)

// kilitBaglayici — ipucu geçen deyimi İLK kalan kez verilen MySQL hata numarasıyla reddeder (Query/Exec düzeyinde),
// sonra gerçeği çalıştırır. cagri: ipucu geçen toplam deneme sayısı.
type kilitBaglayici struct {
	driver.Connector
	ipucu  string
	kalan  *int32
	hataNo uint16
	cagri  *int32
}

func (c *kilitBaglayici) Connect(ctx context.Context) (driver.Conn, error) {
	b, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &kilitConn{Conn: b, c: c}, nil
}

type kilitConn struct {
	driver.Conn
	c *kilitBaglayici
}

func (k *kilitConn) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	var st driver.Stmt
	var err error
	if p, ok := k.Conn.(driver.ConnPrepareContext); ok {
		st, err = p.PrepareContext(ctx, q)
	} else {
		st, err = k.Conn.Prepare(q)
	}
	if err != nil {
		return nil, err
	}
	return &kilitStmt{Stmt: st, c: k.c, q: q}, nil
}

func (k *kilitConn) BeginTx(ctx context.Context, o driver.TxOptions) (driver.Tx, error) {
	if b, ok := k.Conn.(driver.ConnBeginTx); ok {
		return b.BeginTx(ctx, o)
	}
	return k.Conn.Begin()
}

type kilitStmt struct {
	driver.Stmt
	c *kilitBaglayici
	q string
}

func (s *kilitStmt) reddet() error {
	if !strings.Contains(s.q, s.c.ipucu) {
		return nil
	}
	atomic.AddInt32(s.c.cagri, 1)
	if atomic.AddInt32(s.c.kalan, -1) >= 0 {
		return &mysql.MySQLError{Number: s.c.hataNo, SQLState: [5]byte{'4', '0', '0', '0', '1'}, Message: "test: enjekte edilen hata"}
	}
	return nil
}

func (s *kilitStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	if err := s.reddet(); err != nil {
		return nil, err
	}
	if q, ok := s.Stmt.(driver.StmtQueryContext); ok {
		return q.QueryContext(ctx, args)
	}
	dv := make([]driver.Value, len(args))
	for i, a := range args {
		dv[i] = a.Value
	}
	return s.Stmt.Query(dv)
}

func (s *kilitStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if err := s.reddet(); err != nil {
		return nil, err
	}
	if e, ok := s.Stmt.(driver.StmtExecContext); ok {
		return e.ExecContext(ctx, args)
	}
	dv := make([]driver.Value, len(args))
	for i, a := range args {
		dv[i] = a.Value
	}
	return s.Stmt.Exec(dv)
}

// kilitDB: aynı geçici veritabanına, belirli deyimleri enjeksiyonla reddeden ikinci bir bağlantı havuzu açar.
func kilitDB(t *testing.T, ipucu string, kez int32, hataNo uint16) (db *sql.DB, cagri *int32) {
	t.Helper()
	var adi string
	o := kDB(t)
	if err := o.QueryRow(`SELECT DATABASE()`).Scan(&adi); err != nil {
		t.Fatal(err)
	}
	cfg, err := mysql.ParseDSN(os.Getenv("GOSP_TEST_GECICI_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = adi
	baz, err := mysql.NewConnector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	kalan, c := kez, int32(0)
	db = sql.OpenDB(&kilitBaglayici{Connector: baz, ipucu: ipucu, kalan: &kalan, hataNo: hataNo, cagri: &c})
	db.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = db.Close() })
	return db, &c
}

// ---------------------------------------------------------------- bağlama: kilitlenme yeniden denenir

// Gerçek InnoDB kilitlenmesi (1213) ya da kilit bekleme zaman aşımı (1205) bağlama işlemini SUNUCUDA geri alır; kod hatayı yutup
// sürerse sonraki deyim otomatik commit'te koşardı (B5/B6). Burada: ilk K deneme kilitlenmeyle düşer → işlem baştan, tam ve TEK
// seferde uygulanır; kalıcı kilitlenmede hata döner ve HİÇBİR satır bağlanmaz; kilit dışı hata yeniden denenmez.
func TestYetimleriBagla_Kilitlenme_SinirliYenidenDener(t *testing.T) {
	ctx := context.Background()
	kur := func(t *testing.T, kez int32, no uint16) (*sql.DB, *sql.DB, *int32) {
		t.Helper()
		db, cagri := kilitDB(t, "SELECT DISTINCT iz_id", kez, no)
		yetimUzakSahte(t)
		logYakala(t)
		kHesap(t, db, 3300, "kilit.com", "c_kilit", 5, "ftp.k.invalid", dgAd("c_kilit", 0), dgAd("c_kilit", 1), dgAd("c_kilit", 2))
		kSil(t, db, 3300, "kilit.com", "c_kilit")
		g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES (3301,'kilit.com','c_kilit',5)`)
		return db, db, cagri
	}
	for _, no := range []uint16{1213, 1205} {
		t.Run(fmt.Sprintf("hata %d: ilk iki deneme düşer, üçüncüde tam bağlanır", no), func(t *testing.T) {
			db, _, cagri := kur(t, 2, no)
			n, err := YetimleriDevral(ctx, db, 3301)
			if err != nil || n != 3 {
				t.Fatalf("kilitlenme yeniden denenip bağlanmalıydı: n=%d err=%v", n, err)
			}
			if c := atomic.LoadInt32(cagri); c != 3 {
				t.Errorf("3 deneme beklenirdi (2 hata + 1 başarı): %d", c)
			}
			if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=3301 AND tip='kurtarma' AND iz_id=''`) != 3 || g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=0`) != 0 {
				t.Error("üç satır TAM bağlanmış olmalı (yarım/çift uygulama yok)")
			}
			if g113_say(t, db, `SELECT COUNT(*) FROM backup_yetim_hedefler`) != 0 {
				t.Error("bağlama başarılı olunca anlık görüntü silinmeli")
			}
		})
	}
	t.Run("kalıcı kilitlenme: sınırlı deneme sonra HATA, hiçbir satır bağlanmaz", func(t *testing.T) {
		db, _, cagri := kur(t, 1000, 1213)
		n, err := YetimleriDevral(ctx, db, 3301)
		if err == nil || n != 0 || !kilitHatasi(err) {
			t.Fatalf("kalıcı kilitlenmede hata beklenirdi: n=%d err=%v", n, err)
		}
		if c := atomic.LoadInt32(cagri); c != baglaDeneme {
			t.Errorf("tam %d deneme beklenirdi (sınırlı): %d", baglaDeneme, c)
		}
		if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=3301`) != 0 || g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=0`) != 3 {
			t.Error("hata durumunda HİÇBİR satır bağlanmamış olmalı (atomiklik)")
		}
		if g113_say(t, db, `SELECT COUNT(*) FROM backup_yetim_hedefler`) != 1 {
			t.Error("bağlanamadıysa anlık görüntü yerinde kalmalı (yetimler hâlâ var)")
		}
	})
	t.Run("kilit dışı hata yeniden denenmez", func(t *testing.T) {
		db, _, cagri := kur(t, 1000, 1146) // tablo yok benzeri
		if _, err := YetimleriDevral(ctx, db, 3301); err == nil || kilitHatasi(err) {
			t.Fatalf("kilit dışı hata aynen dönmeli: %v", err)
		}
		if c := atomic.LoadInt32(cagri); c != 1 {
			t.Errorf("kilit dışı hata tek denemede bitmeli: %d", c)
		}
	})
}

// İkili günlük STATEMENT kipinde READ COMMITTED InnoDB yazmalarını reddeder (1665): işlem varsayılan yalıtımla bir kez daha denenir.
func TestYalitimliIslem_BinlogStatementKipi_VarsayilanYalitimaDuser(t *testing.T) {
	db, cagri := kilitDB(t, "UPDATE backups SET domain_id", 1, 1665)
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (3400,'bl.test','c_bl')`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya, tip) VALUES (0,'c_bl-20261001-000000.tar.gz','oto')`)
	g113_exec(t, db, `UPDATE backups SET silinme=NOW(), sistem_kullanici='c_bl', alan_adi='bl.test'`)
	logYakala(t)
	n, err := yetimleriBagla(context.Background(), db, 3400, `sistem_kullanici=?`, "c_bl")
	if err != nil || n != 1 {
		t.Fatalf("1665'te varsayılan yalıtımla yeniden denenmeliydi: n=%d err=%v", n, err)
	}
	if c := atomic.LoadInt32(cagri); c != 2 {
		t.Errorf("UPDATE iki kez denenmeli (RC → varsayılan): %d", c)
	}
}

// rows.Err(): `SELECT DISTINCT iz_id … FOR UPDATE` sonuç satırları okunurken InnoDB kilitlenmesi (1213) Query'de değil rows.Next/Err'de
// görünür ve işlemi sunucuda geri alır. Hata görülmezse eskiden UPDATE otomatik commit'te sürerdi. (B5 hedef domain kilidini vurur;
// bu test YALNIZ ikinci sorgunun satır akışını bozar.)
func TestYetimleriBagla_SatirAkisiHatasi_RowsErrKontrolEdilir(t *testing.T) {
	o := dgYeni(t)
	yetimUzakSahte(t)
	logYakala(t)
	o.hesap(3600, "ra.com", "c_ra", 5, "ftp.r.invalid", 3)
	kSil(t, o.db, 3600, "ra.com", "c_ra")
	dgDomain(t, o.db, 3601, "ra.com", "c_ra", 5)
	var adi string
	if err := o.db.QueryRow(`SELECT DATABASE()`).Scan(&adi); err != nil {
		t.Fatal(err)
	}
	cfg, err := mysql.ParseDSN(os.Getenv("GOSP_TEST_GECICI_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = adi
	baz, err := mysql.NewConnector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	db2 := sql.OpenDB(&dgHataBaglayici{Connector: baz, hedef: "DISTINCT iz_id"})
	defer db2.Close()
	n, err := yetimleriBagla(context.Background(), db2, 3601, `sistem_kullanici=?`, "c_ra")
	if err == nil || n != 0 || !kilitHatasi(err) {
		t.Errorf("🔴 satır akışındaki 1213 hata olarak dönmeli (n=%d err=%v)", n, err)
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE domain_id=3601`) != 0 || g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE domain_id=0`) != 3 {
		t.Error("hata durumunda HİÇBİR satır bağlanmamalı (UPDATE otomatik commit'te sürmemeli)")
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backup_yetim_hedefler`) != 1 {
		t.Error("anlık görüntü yerinde kalmalı")
	}
}

// ---------------------------------------------------------------- O4: iz_id

func TestIzIDUret_BenzersizRastgele32Hex(t *testing.T) {
	gorulen := map[string]bool{}
	for i := 0; i < 2000; i++ {
		id, err := izIDUret()
		if err != nil {
			t.Fatal(err)
		}
		if len(id) != 32 || strings.Trim(id, "0123456789abcdef") != "" {
			t.Fatalf("iz_id 32 haneli küçük harf hex olmalı: %q", id)
		}
		if gorulen[id] {
			t.Fatalf("iz_id tekrarlandı: %s", id)
		}
		gorulen[id] = true
	}
}

// Aynı domain kimliği iki farklı müşteriye verilip ikisi de silinirse: yetimler ve anlık görüntüler iz_id ile ayrı kalır;
// ikinci müşterinin hostingi yeniden açılıp devralınca YALNIZ kendi anlık görüntüsü gider, birinci müşterinin anlık görüntüsü (parola) ve
// yetimleri bozulmaz; süpürme her yetimi KENDİ sahibinin hedefiyle siler.
func TestIzID_KimlikYenidenKullanimi_IkiMusteriKarismaz(t *testing.T) {
	o := dgYeni(t)
	sahte := yetimUzakSahte(t)
	dosyaA, _ := o.hesap(100, "a.com", "c_a", 5, "ftp.a.invalid", 2)
	kSil(t, o.db, 100, "a.com", "c_a")
	dosyaB, _ := o.hesap(100, "b.com", "c_b", 5, "ftp.b.invalid", 3) // kimlik yeniden kullanıldı: aynı bayi
	kSil(t, o.db, 100, "b.com", "c_b")

	if g113_say(t, o.db, `SELECT COUNT(*) FROM backup_yetim_hedefler WHERE eski_domain_id=100`) != 2 {
		t.Fatal("kurulum: aynı eski kimlik için iki ayrı anlık görüntü")
	}
	var izA, izB string
	_ = o.db.QueryRow(`SELECT iz_id FROM backups WHERE sistem_kullanici='c_a' LIMIT 1`).Scan(&izA)
	_ = o.db.QueryRow(`SELECT iz_id FROM backups WHERE sistem_kullanici='c_b' LIMIT 1`).Scan(&izB)
	if izA == "" || izB == "" || izA == izB {
		t.Fatalf("iki silme iki ayrı iz_id taşımalı: %q %q", izA, izB)
	}
	// B yeniden açılır ve yedeklerini devralır
	dgDomain(t, o.db, 101, "b.com", "c_b", 5)
	if n := devral(context.Background(), o.db, 101); n != 3 {
		t.Fatalf("B'nin 3 yetimi devralınmalı: %d", n)
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backup_yetim_hedefler WHERE iz_id=?`, izB) != 0 {
		t.Error("B'nin anlık görüntüsü silinmeli")
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backup_yetim_hedefler WHERE iz_id=? AND host='ftp.a.invalid'`, izA) != 1 {
		t.Error("🔴 A'nın anlık görüntüsü (parola) bozuldu/silindi")
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE domain_id=0 AND iz_id=? AND sistem_kullanici='c_a'`, izA) != 2 {
		t.Error("A'nın yetimleri yerinde kalmalı")
	}
	// A'nın süresi dolar: uzak silme A'nın KENDİ hedefiyle
	g113_exec(t, o.db, `UPDATE backups SET son_kullanma=DATE_SUB(NOW(), INTERVAL 1 HOUR) WHERE domain_id=0`)
	SilinenSupur(context.Background(), o.db)
	if sahte.say() != 2 {
		t.Fatalf("A'nın 2 yetimi için uzak silme: %d", sahte.say())
	}
	for _, c := range sahte.cagrilar {
		if c.d == nil || c.d.Host != "ftp.a.invalid" {
			t.Errorf("🔴 uzak silme A'nın hedefiyle yapılmalı: %+v", c.d)
		}
	}
	for _, d := range dosyaB {
		if !varMi(filepath.Join(o.kok, "c_b", d)) {
			t.Errorf("B'nin bağlı (canlı) yedeği silindi: %s", d)
		}
	}
	for _, d := range dosyaA {
		if varMi(filepath.Join(o.kok, "c_a", d)) {
			t.Errorf("A'nın süresi dolan yedeği silinmeliydi: %s", d)
		}
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backup_yetim_hedefler`) != 0 {
		t.Error("tüm yetimler bitince anlık görüntü kalmamalı")
	}
}

// Anlık görüntü yazılamayan / hedefi olmayan silmenin yetimi de bir iz_id taşır (başka silmenin anlık görüntüsüne asla bağlanmaz).
func TestIzID_HedefsizSilme_AnlikGoruntusuzIzTasir_BaskasininGoruntusunuKullanmaz(t *testing.T) {
	o := dgYeni(t)
	sahte := yetimUzakSahte(t)
	o.hesap(100, "a.com", "c_a", 5, "ftp.a.invalid", 1)
	kSil(t, o.db, 100, "a.com", "c_a")
	o.hesap(100, "b.com", "c_b", 5, "", 1) // B'nin HEDEFİ YOK
	kSil(t, o.db, 100, "b.com", "c_b")
	g113_exec(t, o.db, `UPDATE backups SET son_kullanma=DATE_SUB(NOW(), INTERVAL 1 HOUR) WHERE domain_id=0`)
	SilinenSupur(context.Background(), o.db)
	for _, c := range sahte.cagrilar {
		if strings.Contains(c.dosya, "c_b-") && c.d != nil {
			t.Errorf("🔴 B'nin yetimi A'nın hedefiyle (%s) silinmeye çalışıldı", c.d.Host)
		}
	}
}

// ---------------------------------------------------------------- O3: silme 409 → uzagi_yoksay kaynağa göre

func dgJSON(t *testing.T, g string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(g), &m); err != nil {
		t.Fatalf("JSON: %v %s", err, g)
	}
	return m
}

// dgSilinemezDosya: yerel silmenin BAŞARISIZ olacağı "dosya" — içi dolu dizin (os.Remove → ENOTEMPTY); gerçekte salt-okunur/değişmez dosya aynı sonucu verir.
func dgSilinemezDosya(t *testing.T, kok, sk, dosya string) {
	t.Helper()
	yol := filepath.Join(kok, sk, dosya)
	if err := os.MkdirAll(yol, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(yol, "x"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// `uzagi_yoksay` önerisi YALNIZ başarısızlık UZAK silmeden geliyorsa true: yerel dosya silinemediyse "uzağı yoksay" onu çözmez,
// hata metni de gerçek kaynağı söyler. Kısmi başarısızlıkta denetim kaydı (silinen + kalan) yazılır.
func TestSilinenSil_UzagiYoksayBayragi_HataKaynagina_Gore(t *testing.T) {
	t.Run("yalnız uzak hata → true, metin uzak depoyu söyler", func(t *testing.T) {
		o := dgYeni(t)
		sahte := yetimUzakSahte(t)
		logYakala(t)
		sahte.hata = func(*Destination, string) error { return errors.New("uzak kapalı") }
		o.hesap(1800, "u.com", "c_u1", 0, "ftp.u.invalid", 2)
		kSil(t, o.db, 1800, "u.com", "c_u1")
		kod, g := advIstek(o.r, "DELETE", "/s/c_u1", "")
		m := dgJSON(t, g)
		if kod != 409 || m["uzagi_yoksay"] != true || m["silinen"] != float64(0) || m["ok"] != false || !strings.Contains(fmt.Sprint(m["hata"]), "uzak depodaki kopya silinemedi") {
			t.Errorf("uzak hata: 409 + uzagi_yoksay=true + uzak metni beklenirdi: %d %s", kod, g)
		}
		if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE domain_id=0`) != 2 {
			t.Error("hata durumunda satırlar KALMALI")
		}
	})
	t.Run("yalnız yerel hata (uzak yoksayılmış) → false, metin yerel dosyayı söyler, satır KORUNUR", func(t *testing.T) {
		o := dgYeni(t)
		yetimUzakSahte(t)
		logYakala(t)
		dosya := dgAd("c_u2", 0)
		dgSilinemezDosya(t, o.kok, "c_u2", dosya)
		silinenYetim(t, o.db, 1801, "u2.com", "c_u2", dosya, 0, "NULL")
		kod, g := advIstek(o.r, "DELETE", "/s/c_u2?uzagi_yoksay=1", "")
		m := dgJSON(t, g)
		if kod != 409 || m["uzagi_yoksay"] != false || !strings.Contains(fmt.Sprint(m["hata"]), "sunucudaki yerel dosya silinemedi") {
			t.Errorf("yerel hata: 409 + uzagi_yoksay=false + yerel metni beklenirdi: %d %s", kod, g)
		}
		if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE sistem_kullanici='c_u2' AND domain_id=0`) != 1 {
			t.Error("dosya silinemediyse SATIR KORUNMALI (dosya diskte sahipsiz kalmasın)")
		}
	})
	t.Run("yerel hata, uzak sağlam → false", func(t *testing.T) {
		o := dgYeni(t)
		yetimUzakSahte(t)
		logYakala(t)
		dosya := dgAd("c_u3", 0)
		dgSilinemezDosya(t, o.kok, "c_u3", dosya)
		silinenYetim(t, o.db, 1802, "u3.com", "c_u3", dosya, 0, "NULL")
		kod, g := advIstek(o.r, "DELETE", "/s/c_u3", "")
		if m := dgJSON(t, g); kod != 409 || m["uzagi_yoksay"] != false {
			t.Errorf("uzak sağlam, yerel düşünce uzagi_yoksay=false olmalı: %d %s", kod, g)
		}
	})
	t.Run("karışık: biri uzak biri yerel hata → false, metin ikisini söyler", func(t *testing.T) {
		o := dgYeni(t)
		sahte := yetimUzakSahte(t)
		logYakala(t)
		sahte.hata = func(d *Destination, _ string) error {
			if d != nil && d.Host == "ftp.k1.invalid" {
				return errors.New("uzak kapalı")
			}
			return nil
		}
		kHesap(t, o.db, 1803, "k1.com", "c_u4", 0, "ftp.k1.invalid", dgAd("c_u4", 0))
		kSil(t, o.db, 1803, "k1.com", "c_u4")
		dosya2 := dgAd("c_u4", 1)
		dgSilinemezDosya(t, o.kok, "c_u4", dosya2)
		kHesap(t, o.db, 1804, "k2.com", "c_u4", 0, "ftp.k2.invalid", dosya2)
		kSil(t, o.db, 1804, "k2.com", "c_u4")
		kod, g := advIstek(o.r, "DELETE", "/s/c_u4", "")
		m := dgJSON(t, g)
		if kod != 409 || m["uzagi_yoksay"] != false || !strings.Contains(fmt.Sprint(m["hata"]), "uzak depodaki kopya ve sunucudaki yerel dosya silinemedi") {
			t.Errorf("karışık hata: uzagi_yoksay=false + iki kaynağı söyleyen metin beklenirdi: %d %s", kod, g)
		}
	})
	t.Run("kısmi başarısızlık denetime yazılır (silinen + kalan), ok=0", func(t *testing.T) {
		o := dgYeni(t)
		sahte := yetimUzakSahte(t)
		logYakala(t)
		sahte.hata = func(d *Destination, _ string) error {
			if d != nil && d.Host == "ftp.d2.invalid" {
				return errors.New("uzak kapalı")
			}
			return nil
		}
		kHesap(t, o.db, 1805, "d1.com", "c_u5", 0, "ftp.d1.invalid", dgAd("c_u5", 0))
		kSil(t, o.db, 1805, "d1.com", "c_u5")
		kHesap(t, o.db, 1806, "d2.com", "c_u5", 0, "ftp.d2.invalid", dgAd("c_u5", 1))
		kSil(t, o.db, 1806, "d2.com", "c_u5")
		if kod, _ := advIstek(o.r, "DELETE", "/s/c_u5", ""); kod != 409 {
			t.Fatalf("kısmi başarısızlık 409: %d", kod)
		}
		var detay string
		var ok int
		var kapsam int64
		if err := o.db.QueryRow(`SELECT detail, ok, reseller_id FROM audit_log WHERE action='silinen-yedek.temizle' AND target='c_u5'`).Scan(&detay, &ok, &kapsam); err != nil {
			t.Fatalf("kısmi başarısızlıkta denetim kaydı yok: %v", err)
		}
		if ok != 0 || kapsam != 0 || !strings.Contains(detay, "silinen=1") || !strings.Contains(detay, "kalan=1") || !strings.Contains(detay, "uzak depodaki kopya silinemedi") {
			t.Errorf("denetim: ok=%d kapsam=%d detay=%s", ok, kapsam, detay)
		}
	})
	t.Run("tam başarıda denetim ok=1", func(t *testing.T) {
		o := dgYeni(t)
		yetimUzakSahte(t)
		logYakala(t)
		o.hesap(1807, "ok.com", "c_u6", 0, "ftp.ok.invalid", 2)
		kSil(t, o.db, 1807, "ok.com", "c_u6")
		if kod, g := advIstek(o.r, "DELETE", "/s/c_u6", ""); kod != 200 || dgJSON(t, g)["silinen"] != float64(2) {
			t.Fatalf("%d %s", kod, g)
		}
		if g113_say(t, o.db, `SELECT COUNT(*) FROM audit_log WHERE action='silinen-yedek.temizle' AND target='c_u6' AND ok=1 AND detail LIKE '%yedek=2%'`) != 1 {
			t.Error("tam başarıda ok=1 denetim satırı beklenirdi")
		}
	})
}

// ---------------------------------------------------------------- D2: süpürme tek özet denetim satırı

func TestSupur_TekOzetDenetimSatiri(t *testing.T) {
	o := dgYeni(t)
	sahte := yetimUzakSahte(t)
	logYakala(t)
	ctx := context.Background()
	denetim := func() (n int, ok int, detay string, kapsam int64, aktor string) {
		n = g113_say(t, o.db, `SELECT COUNT(*) FROM audit_log WHERE action='silinen-yedek.supur'`)
		if n > 0 {
			_ = o.db.QueryRow(`SELECT ok, detail, reseller_id, actor_username FROM audit_log WHERE action='silinen-yedek.supur' ORDER BY id DESC LIMIT 1`).Scan(&ok, &detay, &kapsam, &aktor)
		}
		return
	}
	// yapacak iş yok → denetim satırı YOK (günlük gürültü üretmez)
	SilinenSupur(ctx, o.db)
	if n, _, _, _, _ := denetim(); n != 0 {
		t.Fatalf("iş yokken denetim satırı yazılmamalı: %d", n)
	}
	// 3 yetim süresi doldu (boyut 5'er bayt) → TEK özet satırı, ok=1
	for i := 0; i < 3; i++ {
		d := dgAd("c_sp", i)
		silinenDosya(t, o.kok, "c_sp", d)
		silinenYetim(t, o.db, 1900, "sp.test", "c_sp", d, 0, "DATE_SUB(NOW(), INTERVAL 1 HOUR)")
	}
	SilinenSupur(ctx, o.db)
	n, ok, detay, kapsam, aktor := denetim()
	if n != 1 || ok != 1 || kapsam != 0 || aktor != "sistem" || !strings.Contains(detay, "3 yedek silindi") || !strings.Contains(detay, "0 silinemedi") {
		t.Errorf("tek özet satırı (ok=1, kök kapsam, sistem): n=%d ok=%d kapsam=%d aktör=%q detay=%q", n, ok, kapsam, aktor, detay)
	}
	// uzak hata → satır KORUNUR, özet ok=0 ve "1 silinemedi"
	d := dgAd("c_sp2", 0)
	silinenDosya(t, o.kok, "c_sp2", d)
	silinenYetim(t, o.db, 1901, "sp2.test", "c_sp2", d, 0, "DATE_SUB(NOW(), INTERVAL 1 HOUR)")
	sahte.hata = func(*Destination, string) error { return errors.New("uzak kapalı") }
	SilinenSupur(ctx, o.db)
	n, ok, detay, _, _ = denetim()
	if n != 2 || ok != 0 || !strings.Contains(detay, "0 yedek silindi") || !strings.Contains(detay, "1 silinemedi") {
		t.Errorf("hatalı turda özet ok=0: n=%d ok=%d detay=%q", n, ok, detay)
	}
	if strings.Contains(detay, "uzak kapalı") {
		t.Errorf("denetim özeti ham hata metnini taşımamalı: %q", detay)
	}
}

// ---------------------------------------------------------------- O8: disk kapısı bildirimi

func TestDiskBildirimi_SilinmisHesapYedeklerineYonlendirir(t *testing.T) {
	db := kDB(t)
	g113_exec(t, db, `CREATE TABLE cp_bildirim (id BIGINT AUTO_INCREMENT PRIMARY KEY, seviye VARCHAR(16) NOT NULL DEFAULT '', kategori VARCHAR(32) NOT NULL DEFAULT '',
		baslik VARCHAR(200) NOT NULL DEFAULT '', mesaj TEXT NULL, domain_id BIGINT NULL, reseller_id BIGINT NULL, ref_tur VARCHAR(32) NOT NULL DEFAULT '',
		ref_id BIGINT NOT NULL DEFAULT 0, okundu TINYINT NOT NULL DEFAULT 0, created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`)
	logYakala(t)
	mesaj := func() string {
		var m string
		_ = db.QueryRow(`SELECT mesaj FROM cp_bildirim WHERE kategori='yedek' AND baslik='Yedek durduruldu: disk' ORDER BY id DESC LIMIT 1`).Scan(&m)
		return m
	}
	// silinmiş hesap yedeği yok: yönlendirme yok
	diskBildirimiVer(db, "yedek deposu 5.0 GB > tavan 4 GB")
	if m := mesaj(); !strings.Contains(m, "yedek deposu 5.0 GB > tavan 4 GB") || strings.Contains(m, "Silinmiş hesap") {
		t.Errorf("yetim yokken yönlendirme olmamalı: %q", m)
	}
	// aynı gün ikinci kez yazılmaz (spam yok)
	diskBildirimiVer(db, "yedek deposu 6.0 GB > tavan 4 GB")
	if g113_say(t, db, `SELECT COUNT(*) FROM cp_bildirim`) != 1 {
		t.Error("aynı sebeple günde bir bildirim")
	}
	g113_exec(t, db, `DELETE FROM cp_bildirim`)
	silinenYetim(t, db, 1950, "s.test", "c_ds", dgAd("c_ds", 0), 0, "NULL")
	silinenYetim(t, db, 1950, "s.test", "c_ds", dgAd("c_ds", 1), 0, "NULL")
	g113_exec(t, db, `UPDATE backups SET boyut_b=1073741824 WHERE domain_id=0`) // 2 × 1 GB
	diskBildirimiVer(db, "yedek deposu 5.0 GB > tavan 4 GB")
	m := mesaj()
	if !strings.Contains(m, "2 yedek (2.00 GB)") || !strings.Contains(m, "Silinmiş hesap yedekleri") || !strings.Contains(m, "Yedek Yöneticisi") {
		t.Errorf("silinmiş hesap yedeklerine yönlendirme (adet + boyut + yer) beklenirdi: %q", m)
	}
}

// ---------------------------------------------------------------- Y1: Put/TestDestination parola koruması

func TestHedefDegistiMi(t *testing.T) {
	m := mevcutHedef{var_: true, tip: "ftp", host: "ftp.a.invalid", port: 21, kullanici: "u", parola: "p"}
	for _, c := range []struct {
		ad           string
		tip, host    string
		port         int
		kul          string
		farkliOlmali bool
	}{
		{"aynı", "ftp", "ftp.a.invalid", 21, "u", false},
		{"aynı, port 0 = tipin varsayılanı", "ftp", "ftp.a.invalid", 0, "u", false},
		{"host büyük harf (host adı duyarsız)", "ftp", "FTP.A.invalid", 21, "u", false},
		{"farklı host", "ftp", "ftp.b.invalid", 21, "u", true},
		{"farklı port", "ftp", "ftp.a.invalid", 2121, "u", true},
		{"farklı kullanıcı", "ftp", "ftp.a.invalid", 21, "v", true},
		{"kullanıcı büyük harf (FTP kullanıcı adı duyarlı)", "ftp", "ftp.a.invalid", 21, "U", true},
		{"farklı tip (varsayılan port da değişir)", "sftp", "ftp.a.invalid", 0, "u", true},
		{"sftp + port 21 (farklı tip)", "sftp", "ftp.a.invalid", 21, "u", true},
	} {
		if g := m.farkliHedef(c.tip, c.host, c.port, c.kul); g != c.farkliOlmali {
			t.Errorf("%s: farkliHedef=%v, %v beklenirdi", c.ad, g, c.farkliOlmali)
		}
	}
}

func dgDestRouter(db *sql.DB) http.Handler {
	h := &Handlers{DB: db}
	r := chi.NewRouter()
	r.Put("/d/{id}/dest", h.PutDestination)
	r.Post("/d/{id}/dest/test", h.TestDestination)
	return r
}

func TestPutDestination_ParolaYalnizAyniHedefIcinKorunur(t *testing.T) {
	db := kDB(t)
	r := dgDestRouter(db)
	dgDomain(t, db, 2000, "p.test", "c_pd", 0)
	put := func(govde string) (int, string) { return advIstek(r, "PUT", "/d/2000/dest", govde) }
	okuParola := func() string {
		d, _ := readDestination(context.Background(), db, 2000)
		if d == nil {
			return "<kayıt yok>"
		}
		return d.Host + "|" + d.Parola
	}
	// ilk kayıt parola ister
	if kod, _ := put(`{"tip":"ftp","host":"ftp.a.invalid","port":21,"kullanici":"u","parola":"","uzak_dizin":"/","aktif":true}`); kod != 400 {
		t.Errorf("yeni kayıt, parola yok → 400: %d", kod)
	}
	if kod, g := put(`{"tip":"ftp","host":"ftp.a.invalid","port":21,"kullanici":"u","parola":"ILK-PAROLA","uzak_dizin":"/","aktif":true}`); kod != 200 {
		t.Fatalf("ilk kayıt: %d %s", kod, g)
	}
	// AYNI hedef: boş parola mevcut parolayı korur (uzak_dizin / aktif değişimi hedef değişimi DEĞİLDİR)
	if kod, g := put(`{"tip":"ftp","host":"FTP.A.INVALID","port":0,"kullanici":"u","parola":"","uzak_dizin":"/yeni","aktif":false}`); kod != 200 {
		t.Errorf("aynı hedef + boş parola → 200 (korunur): %d %s", kod, g)
	}
	if g := okuParola(); g != "ftp.a.invalid|ILK-PAROLA" && g != "FTP.A.INVALID|ILK-PAROLA" {
		t.Errorf("aynı hedefte parola korunmalı: %s", g)
	}
	// FARKLI hedef + boş parola → 400 ve HİÇBİR şey değişmez
	oncesi := tabloDokumu(t, db, "backup_destinations")
	for ad, govde := range map[string]string{
		"host":      `{"tip":"ftp","host":"saldirgan.invalid","port":21,"kullanici":"u","parola":"","uzak_dizin":"/","aktif":true}`,
		"port":      `{"tip":"ftp","host":"ftp.a.invalid","port":2121,"kullanici":"u","parola":"","uzak_dizin":"/","aktif":true}`,
		"kullanıcı": `{"tip":"ftp","host":"ftp.a.invalid","port":21,"kullanici":"baska","parola":"","uzak_dizin":"/","aktif":true}`,
		"tip":       `{"tip":"sftp","host":"ftp.a.invalid","port":22,"kullanici":"u","parola":"","uzak_dizin":"/","aktif":true}`,
	} {
		kod, g := put(govde)
		if kod != 400 || !strings.Contains(g, "hedef değişti: parola yeniden girilmeli") {
			t.Errorf("%s değişti + boş parola: 400 'hedef değişti: parola yeniden girilmeli' beklenirdi: %d %s", ad, kod, g)
		}
	}
	if sonrasi := tabloDokumu(t, db, "backup_destinations"); sonrasi != oncesi {
		t.Errorf("reddedilen istekler kaydı değiştirdi\n%s\n---\n%s", oncesi, sonrasi)
	}
	// farklı hedef + YENİ parola → kabul
	if kod, g := put(`{"tip":"ftp","host":"yeni.invalid","port":21,"kullanici":"u","parola":"YENI-PAROLA","uzak_dizin":"/","aktif":true}`); kod != 200 {
		t.Fatalf("farklı hedef + yeni parola → 200: %d %s", kod, g)
	}
	if g := okuParola(); g != "yeni.invalid|YENI-PAROLA" {
		t.Errorf("yeni hedef yeni parolayla kaydedilmeli: %s", g)
	}
	// parolası boşaltılmış kayıt (sahip devri): boş parola KABUL EDİLMEZ (eski kayıtlı parola yok)
	g113_exec(t, db, `UPDATE backup_destinations SET aktif=0, parola='' WHERE domain_id=2000`)
	if kod, g := put(`{"tip":"ftp","host":"yeni.invalid","port":21,"kullanici":"u","parola":"","uzak_dizin":"/","aktif":true}`); kod != 400 || !strings.Contains(g, "parola zorunlu") {
		t.Errorf("kayıtlı parola boşken boş parola → 400 'parola zorunlu': %d %s", kod, g)
	}
	// kayıtlı parola yoksa taşınacak bir parola da yok: farklı hosta geçmek "hedef değişti" DEĞİL, yeni kayıt gibi "parola zorunlu" der
	if kod, g := put(`{"tip":"ftp","host":"baska.invalid","port":21,"kullanici":"u","parola":"","uzak_dizin":"/","aktif":true}`); kod != 400 || !strings.Contains(g, "parola zorunlu") || strings.Contains(g, "hedef değişti") {
		t.Errorf("kayıtlı parola boşken farklı hosta boş parola → 400 'parola zorunlu' (hedef değişti DEĞİL): %d %s", kod, g)
	}
}

// TestDestination (kaydetmeden deneme; müşteriye de açık): kayıtlı parola farklı hosta GÖNDERİLMEZ — ağa çıkmadan 400.
func TestTestDestination_FarkliHedefBosParola_AgaCikmadan400(t *testing.T) {
	db := kDB(t)
	r := dgDestRouter(db)
	dgDomain(t, db, 2001, "t.test", "c_td", 0)
	g113_exec(t, db, `INSERT INTO backup_destinations (domain_id, tip, host, port, kullanici, parola, uzak_dizin, aktif) VALUES (2001,'ftp','kayitli.invalid',21,'u',?,'/',1)`, gizli.SaklaBagli("KAYITLI", "yedek"))
	for ad, govde := range map[string]string{
		"host":      `{"tip":"ftp","host":"127.0.0.1","port":21,"kullanici":"u","parola":"","uzak_dizin":"/"}`,
		"port":      `{"tip":"ftp","host":"kayitli.invalid","port":2121,"kullanici":"u","parola":"","uzak_dizin":"/"}`,
		"kullanıcı": `{"tip":"ftp","host":"kayitli.invalid","port":21,"kullanici":"x","parola":"","uzak_dizin":"/"}`,
		"tip":       `{"tip":"sftp","host":"kayitli.invalid","port":22,"kullanici":"u","parola":"","uzak_dizin":"/"}`,
	} {
		t0 := time.Now()
		kod, g := advIstek(r, "POST", "/d/2001/dest/test", govde)
		if kod != 400 || !strings.Contains(g, "hedef değişti: parola yeniden girilmeli") {
			t.Errorf("%s değişti + boş parola: 400 beklenirdi: %d %s", ad, kod, g)
		}
		if time.Since(t0) > 2*time.Second {
			t.Errorf("%s: reddedilen istek ağa çıkmaya çalışmış olabilir (%v)", ad, time.Since(t0))
		}
	}
}

// ---------------------------------------------------------------- D5: lftp artık betikleri

func TestLftpArtiklariniTemizle(t *testing.T) {
	dizin := t.TempDir()
	yaz := func(ad string, yas time.Duration) string {
		yol := filepath.Join(dizin, ad)
		if err := os.WriteFile(yol, []byte(`open -u "k","PAROLA" ftp://x`), 0o600); err != nil {
			t.Fatal(err)
		}
		gecmis := time.Now().Add(-yas)
		if err := os.Chtimes(yol, gecmis, gecmis); err != nil {
			t.Fatal(err)
		}
		return yol
	}
	eski1, eski2 := yaz("gosp-lftp-111.cmd", 30*time.Minute), yaz("gosp-lftp-222.cmd", 11*time.Minute)
	genc := yaz("gosp-lftp-333.cmd", 2*time.Minute)    // çalışan bir silme/yükleme işinin betiği olabilir
	baska := yaz("baska-444.cmd", 5*time.Hour)         // ad deseni tutmuyor
	ekliDeger := yaz("gosp-lftp-555.txt", 5*time.Hour) // uzantı tutmuyor
	dizinAdi := filepath.Join(dizin, "gosp-lftp-666.cmd")
	if err := os.Mkdir(dizinAdi, 0o700); err != nil {
		t.Fatal(err)
	}
	gecmis := time.Now().Add(-time.Hour)
	_ = os.Chtimes(dizinAdi, gecmis, gecmis)
	hedef := yaz("hedef-777", 5*time.Hour)
	link := filepath.Join(dizin, "gosp-lftp-888.cmd")
	if err := os.Symlink(hedef, link); err != nil {
		t.Fatal(err)
	}
	if n := lftpArtiklariniTemizle(dizin, 10*time.Minute); n != 2 {
		t.Errorf("yalnız 10 dk'dan eski 2 düz betik silinmeli: %d", n)
	}
	for _, yol := range []string{eski1, eski2} {
		if varMi(yol) {
			t.Errorf("eski betik silinmeliydi: %s", yol)
		}
	}
	for ad, yol := range map[string]string{"genç": genc, "başka ad": baska, "başka uzantı": ekliDeger, "dizin": dizinAdi, "symlink": link, "symlink hedefi": hedef} {
		if !varMi(yol) {
			t.Errorf("%s dokunulmamalıydı: %s", ad, yol)
		}
	}
	if n := lftpArtiklariniTemizle(filepath.Join(dizin, "yok"), time.Minute); n != 0 {
		t.Errorf("olmayan dizin: %d", n)
	}
}

// Açılışta çağrılan sarmalayıcı: os.TempDir() ve 10 DAKİKA eşiği (9 dk'lık betik çalışan bir işin olabilir, 11 dk'lık artıktır).
func TestLftpArtiklariniTemizle_SarmalayiciTempDirVe10Dakika(t *testing.T) {
	dizin := t.TempDir()
	t.Setenv("TMPDIR", dizin)
	yaz := func(ad string, yas time.Duration) string {
		yol := filepath.Join(dizin, ad)
		if err := os.WriteFile(yol, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		gecmis := time.Now().Add(-yas)
		_ = os.Chtimes(yol, gecmis, gecmis)
		return yol
	}
	dokuz, onbir := yaz("gosp-lftp-1.cmd", 9*time.Minute), yaz("gosp-lftp-2.cmd", 11*time.Minute)
	if n := LftpArtiklariniTemizle(); n != 1 {
		t.Errorf("yalnız 10 dk'dan eski betik silinmeli: %d", n)
	}
	if !varMi(dokuz) || varMi(onbir) {
		t.Errorf("9 dk'lık kalmalı, 11 dk'lık silinmeli: dokuz=%v onbir=%v", varMi(dokuz), varMi(onbir))
	}
}

// ---------------------------------------------------------------- D6: S2 toplu işaretleme eşiği

func TestS2_Esik_TopluIsaretlemeKorunur(t *testing.T) {
	ekle := func(t *testing.T, db *sql.DB, domainID int64, sk string, adet int) {
		t.Helper()
		g113_exec(t, db, fmt.Sprintf(`INSERT INTO backups (domain_id, dosya) SELECT ?, CONCAT(?, '-20261001-', LPAD(seq,6,'0'), '.tar.gz') FROM seq_1_to_%d`, adet), domainID, sk)
	}
	canliAyarla := func(t *testing.T, db *sql.DB, adet int) {
		t.Helper()
		dgDomain(t, db, 5, "canli.test", "c_canli", 0)
		ekle(t, db, 5, "c_canli", adet)
	}
	t.Run("aday ≤ eşik: domains boş olsa bile işaretlenir (küçük kurulum)", func(t *testing.T) {
		db := kDB(t)
		logYakala(t)
		ekle(t, db, 9001, "c_orf", sahipsizEsikAdet)
		if m, _ := SilinenSahipsizIsaretle(context.Background(), db); m != sahipsizEsikAdet {
			t.Errorf("%d aday (eşik dahil) işaretlenmeliydi: %d", sahipsizEsikAdet, m)
		}
	})
	t.Run("aday > eşik ve domains boş → ATLANIR, 🔴 günlük", func(t *testing.T) {
		db := kDB(t)
		logs := logYakala(t)
		ekle(t, db, 9001, "c_orf", sahipsizEsikAdet+1)
		if m, tur := SilinenSahipsizIsaretle(context.Background(), db); m != 0 || tur != 0 {
			t.Errorf("hiçbir şey işaretlenmemeli: %d %d", m, tur)
		}
		if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=0`) != 0 || g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=9001 AND silinme IS NULL`) != sahipsizEsikAdet+1 {
			t.Error("satırlara dokunulmamalı")
		}
		if !strings.Contains(logs.String(), "🔴 sahipsiz yedek işaretleme ATLANDI") {
			t.Errorf("🔴 günlük beklenirdi: %s", logs.String())
		}
	})
	t.Run("aday > eşik ve adaylar toplamın yarısından fazla → ATLANIR", func(t *testing.T) {
		db := kDB(t)
		logYakala(t)
		canliAyarla(t, db, 50)
		ekle(t, db, 9001, "c_orf", 101) // 101 aday / 151 toplam
		if m, _ := SilinenSahipsizIsaretle(context.Background(), db); m != 0 {
			t.Errorf("yarıdan fazlası sahipsiz görünüyorsa işaretlenmemeli: %d", m)
		}
	})
	t.Run("aday > eşik ama toplamın yarısından az → işaretlenir", func(t *testing.T) {
		db := kDB(t)
		logYakala(t)
		canliAyarla(t, db, 300)
		ekle(t, db, 9001, "c_orf", 101) // 101 aday / 401 toplam
		if m, _ := SilinenSahipsizIsaretle(context.Background(), db); m != 101 {
			t.Errorf("gerçek sahipsiz satırlar işaretlenmeli: %d", m)
		}
		if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=5 AND silinme IS NULL`) != 300 {
			t.Error("canlı domainin satırlarına dokunulmamalı")
		}
	})
	t.Run("domains BOŞ ve aday > eşik: adaylar toplamın yarısından az olsa bile ATLANIR (tek başına yetmez)", func(t *testing.T) {
		db := kDB(t)
		logs := logYakala(t)
		for i := 0; i < 300; i++ { // zaten işaretli yetimler toplamı şişirir
			silinenYetim(t, db, 9500, "y.test", "c_yet", fmt.Sprintf("c_yet-20261001-%06d.tar.gz", i), 0, "NULL")
		}
		ekle(t, db, 9001, "c_orf", 101) // 101 aday / 401 toplam (< %50) ama domains tablosu BOŞ
		if m, _ := SilinenSahipsizIsaretle(context.Background(), db); m != 0 {
			t.Errorf("domains boşken işaretlenmemeli: %d", m)
		}
		if !strings.Contains(logs.String(), "ATLANDI") {
			t.Errorf("🔴 günlük beklenirdi: %s", logs.String())
		}
	})
	t.Run("yetimler paydayı ŞİŞİRMEZ: bozuk domains + çok yetim → adaylar canlı+aday satırların yarısından fazlaysa ATLANIR", func(t *testing.T) {
		db := kDB(t)
		logs := logYakala(t)
		for i := 0; i < 500; i++ { // toplu bayi silmesinden kalan işaretli yetimler
			silinenYetim(t, db, 9500, "y.test", "c_yet", fmt.Sprintf("c_yet-20261001-%06d.tar.gz", i), 0, "NULL")
		}
		canliAyarla(t, db, 20) // domains boş DEĞİL (1 canlı domain) ama kalanı kayıp: 150 aday / (20 + 150) canlı+aday
		ekle(t, db, 9001, "c_orf", 150)
		if m, _ := SilinenSahipsizIsaretle(context.Background(), db); m != 0 {
			t.Errorf("🔴 yetimler paydayı şişirdi: bozuk domains tablosu 150 satırı yetim yaptı: %d", m)
		}
		if !strings.Contains(logs.String(), "ATLANDI") {
			t.Errorf("🔴 günlük beklenirdi: %s", logs.String())
		}
	})
	t.Run("tam yarı (aday*2 == toplam) eşik aşılmaz → işaretlenir", func(t *testing.T) {
		db := kDB(t)
		logYakala(t)
		canliAyarla(t, db, 101)
		ekle(t, db, 9001, "c_orf", 101) // 101 aday / 202 toplam: aday*2 == toplam → "fazlası" değil
		if m, _ := SilinenSahipsizIsaretle(context.Background(), db); m != 101 {
			t.Errorf("tam yarıda işaretlenmeli: %d", m)
		}
	})
}

// ---------------------------------------------------------------- A2: kimlik yeniden kullanımı kalıntıları

func TestYetimKalintilariniAyir(t *testing.T) {
	ctx := context.Background()
	t.Run("sk dosya adından türetilir, anlık görüntü sahiplenilir, canlı komşuya dokunulmaz", func(t *testing.T) {
		o := dgYeni(t)
		yetimUzakSahte(t)
		logYakala(t)
		o.hesap(100, "eski.com", "c_eski", 5, "ftp.e.invalid", 2)
		tetikKur(t, o.db, 1000)
		kSil(t, o.db, 100, "eski.com", "c_eski") // iz yazılamadı: satırlar domain_id=100'de, anlık görüntü UPDATE'ten önce yazıldı
		g113_exec(t, o.db, `DROP TRIGGER tetik_bu`)
		g113_exec(t, o.db, `INSERT INTO backups (domain_id, dosya) VALUES (100, 'elle-yuklendi.tar.gz')`) // sk türetilemez
		dgDomain(t, o.db, 100, "yeni.org", "c_yeni", 5)
		dgDomain(t, o.db, 101, "komsu.org", "c_komsu", 5)
		g113_exec(t, o.db, `INSERT INTO backups (domain_id, dosya) VALUES (101, ?)`, dgAd("c_komsu", 0))
		var snap string
		_ = o.db.QueryRow(`SELECT iz_id FROM backup_yetim_hedefler WHERE eski_domain_id=100`).Scan(&snap)
		if snap == "" {
			t.Fatal("kurulum: anlık görüntü yazılmış olmalı")
		}
		if n := YetimKalintilariniAyir(ctx, o.db, 100); n != 2 {
			t.Fatalf("türetilebilir 2 satır işaretlenmeli: %d", n)
		}
		if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE domain_id=0 AND eski_domain_id=100 AND sistem_kullanici='c_eski' AND iz_id=? AND silinme IS NOT NULL`, snap) != 2 {
			t.Error("kalıntılar eski sahibin sk'sıyla, anlık görüntünün iz_id'siyle yetim olmalı")
		}
		if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE domain_id=100 AND dosya='elle-yuklendi.tar.gz' AND silinme IS NULL`) != 1 {
			t.Error("sk türetilemeyen satıra dokunulmamalı")
		}
		if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE domain_id=101 AND silinme IS NULL`) != 1 {
			t.Error("komşu canlı domainin satırına dokunulmamalı")
		}
		if n := YetimKalintilariniAyir(ctx, o.db, 100); n != 0 {
			t.Errorf("ikinci çağrı (kalıntı kalmadı) 0: %d", n)
		}
	})
	t.Run("kalıntı yoksa sessiz sıfır; geçersiz girdiler zararsız", func(t *testing.T) {
		o := dgYeni(t)
		dgDomain(t, o.db, 200, "temiz.org", "c_temiz", 0)
		if YetimKalintilariniAyir(ctx, o.db, 200) != 0 || YetimKalintilariniAyir(ctx, o.db, 0) != 0 || YetimKalintilariniAyir(ctx, o.db, -4) != 0 || YetimKalintilariniAyir(ctx, nil, 5) != 0 {
			t.Error("kalıntı yoksa / geçersiz girdide 0 dönmeli, panik yok")
		}
	})
	t.Run("yetim satırlara (domain_id=0) ve zaten işaretli satırlara dokunmaz", func(t *testing.T) {
		o := dgYeni(t)
		dgDomain(t, o.db, 300, "x.org", "c_x", 0)
		silinenYetim(t, o.db, 300, "y.org", "c_y", dgAd("c_y", 0), 0, "NULL")
		g113_exec(t, o.db, `INSERT INTO backups (domain_id, dosya, silinme) VALUES (300, ?, NOW())`, dgAd("c_z", 0)) // domain_id>0 ama işaretli: dokunma
		if n := YetimKalintilariniAyir(ctx, o.db, 300); n != 0 {
			t.Errorf("işaretli/yetim satırlar atlanmalı: %d", n)
		}
	})
}

// ---------------------------------------------------------------- D4: anlık görüntü at-rest

func TestAnlikGoruntu_AtRest_DuzMetinSifrelenir_SigmazsaYazilmaz(t *testing.T) {
	o := dgYeni(t)
	yetimUzakSahte(t)
	logs := logYakala(t)
	// kaynak sütunda DÜZ METİN (anahtarsız/eski kayıt): anlık görüntüye düz yazılmaz
	dgDomain(t, o.db, 2100, "duz.com", "c_duz", 0)
	g113_exec(t, o.db, `INSERT INTO backup_destinations (domain_id, tip, host, port, kullanici, parola) VALUES (2100,'sftp','ftp.d.invalid',22,'k','DUZ-PAROLA-9')`)
	g113_exec(t, o.db, `INSERT INTO backups (domain_id, dosya) VALUES (2100, ?)`, dgAd("c_duz", 0))
	kSil(t, o.db, 2100, "duz.com", "c_duz")
	var ham string
	if err := o.db.QueryRow(`SELECT parola FROM backup_yetim_hedefler WHERE eski_domain_id=2100`).Scan(&ham); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ham, "DUZ-PAROLA-9") || !gizli.SifreliMi(ham) {
		if gizli.SaklaBagli("x", "yedek") != "x" { // anahtar varsa şifreli olmalı
			t.Errorf("anlık görüntüde DÜZ parola kaldı: %q", ham)
		}
	}
	var izID string
	_ = o.db.QueryRow(`SELECT iz_id FROM backup_yetim_hedefler WHERE eski_domain_id=2100`).Scan(&izID)
	if d, err := yetimHedefOku(context.Background(), o.db, izID); err != nil || d == nil || d.Parola != "DUZ-PAROLA-9" {
		t.Errorf("şifreli anlık görüntü çözülüp okunabilmeli: %+v %v", d, err)
	}
	// şifrelenmiş hâli sütuna SIĞMAYAN düz değer: satır YAZILMAZ (kesilmiş/çözülemez parola olmaz), 🔴 günlük
	if gizli.SaklaBagli("x", "yedek") == "x" {
		t.Skip("şifreleme anahtarı yok: sığmama senaryosu sınanamaz")
	}
	dgDomain(t, o.db, 2101, "uzun.com", "c_uzun2", 0)
	g113_exec(t, o.db, `INSERT INTO backup_destinations (domain_id, tip, host, port, kullanici, parola) VALUES (2101,'sftp','ftp.u.invalid',22,'k',?)`, strings.Repeat("p", 200))
	g113_exec(t, o.db, `INSERT INTO backups (domain_id, dosya) VALUES (2101, ?)`, dgAd("c_uzun2", 0))
	kSil(t, o.db, 2101, "uzun.com", "c_uzun2")
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backup_yetim_hedefler WHERE eski_domain_id=2101`) != 0 {
		t.Error("sığmayan şifreli parola için satır yazılmamalı")
	}
	if !strings.Contains(logs.String(), "anlık görüntüsü yazılamadı") || strings.Contains(logs.String(), strings.Repeat("p", 200)) {
		t.Errorf("🔴 yazılamadı günlüğü (parolasız) beklenirdi: %s", logs.String())
	}
}

func TestMaskeliUzakHata_DestinationNilIkenDeDeseniMaskeler(t *testing.T) {
	e := maskeliUzakHata(errors.New("lftp: open ftp://musteri:GizliParola123@depo.example/x: Login failed"), nil)
	if e == nil || strings.Contains(e.Error(), "GizliParola123") {
		t.Errorf("d==nil iken URL kullanıcı bilgisi maskelenmeli: %v", e)
	}
	if !errors.Is(maskeliUzakHata(ErrLftpYok, nil), ErrLftpYok) || maskeliUzakHata(nil, nil) != nil {
		t.Error("ErrLftpYok aynen, nil → nil")
	}
}

// ---------------------------------------------------------------- liste / bağla: ek davranışlar

func TestSilinenListe_CreatedAtNullSatirKaybolmaz(t *testing.T) {
	o := dgYeni(t)
	silinenYetim(t, o.db, 2200, "n.test", "c_nl", dgAd("c_nl", 0), 0, "NULL")
	g113_exec(t, o.db, `UPDATE backups SET created_at=NULL WHERE sistem_kullanici='c_nl'`)
	_, g := advIstek(o.r, "GET", "/s", "")
	if !strings.Contains(g, `"sk":"c_nl"`) || !strings.Contains(g, `"dosya_sayisi":1`) {
		t.Errorf("created_at NULL olan yetim satır Scan hatasıyla kaybolmamalı: %s", g)
	}
}

// zorla=true gruptaki TÜM sahipleri bağlar; denetim TÜM (alan adı, bayi) çiftlerini yazar; kapsam = hedef bayi.
func TestBagla_Zorla_TumSahipler_DenetimTumCiftler(t *testing.T) {
	o := dgYeni(t)
	yetimUzakSahte(t)
	silinenYetim(t, o.db, 2300, "girgin.com", "c_g9", dgAd("c_g9", 0), 5, "NULL")
	silinenYetim(t, o.db, 2301, "girgin.com", "c_g9", dgAd("c_g9", 1), 5, "NULL")
	silinenYetim(t, o.db, 2302, "girgin.org", "c_g9", dgAd("c_g9", 2), 0, "NULL")
	dgDomain(t, o.db, 2303, "girgin.net", "c_g9", 7)
	kod, g := advIstek(o.r, "POST", "/s/c_g9/bagla", `{"domain_id":2303}`)
	m := dgJSON(t, g)
	if kod != 409 {
		t.Fatalf("409 beklenirdi: %d %s", kod, g)
	}
	f, _ := m["farklilar"].([]any)
	if len(f) != 2 {
		t.Fatalf("farklilar[] iki çift taşımalı: %s", g)
	}
	if f0, _ := f[0].(map[string]any); f0["alan_adi"] != "girgin.com" || f0["reseller_id"] != float64(5) || f0["dosya_sayisi"] != float64(2) {
		t.Errorf("ilk farklı çift (en çok dosyalı): %v", f[0])
	}
	if m["yedek_alan_adi"] != "girgin.com" || m["yedek_reseller_id"] != float64(5) || m["hedef_alan_adi"] != "girgin.net" || m["hedef_reseller_id"] != float64(7) {
		t.Errorf("yedek_* alanları ilk çifti, hedef_* hedefi göstermeli: %s", g)
	}
	if kod, g := advIstek(o.r, "POST", "/s/c_g9/bagla", `{"domain_id":2303,"zorla":true}`); kod != 200 || dgJSON(t, g)["baglanan"] != float64(3) {
		t.Fatalf("zorla: gruptaki 3 satırın TÜMÜ bağlanmalı: %d %s", kod, g)
	}
	var detay string
	var kapsam int64
	if err := o.db.QueryRow(`SELECT detail, reseller_id FROM audit_log WHERE action='silinen-yedek.bagla'`).Scan(&detay, &kapsam); err != nil {
		t.Fatal(err)
	}
	for _, bek := range []string{"girgin.com (bayi=5, 2 dosya)", "girgin.org (bayi=0, 1 dosya)", "hedef=girgin.net (bayi=7)", "baglanan=3"} {
		if !strings.Contains(detay, bek) {
			t.Errorf("denetim notu %q içermeli: %s", bek, detay)
		}
	}
	if kapsam != 7 {
		t.Errorf("denetim kapsamı hedef bayi (7) olmalı, domain kimliği (2303) DEĞİL: %d", kapsam)
	}
}

// ---------------------------------------------------------------- Restore: bağlanmış 'kurtarma' satırı uçtan uca

// Eski hesabın yetim yedeği: domain sayfasının Restore ucu 404 verir (yetim domaine ait DEĞİL). Domaine bağlanınca (kurtarma tipi)
// AYNI uç JOIN domains ile satırı bulur ve dosyaları gerçekten geri yükler; sk kapısı bağlanan yedeğin arşivini hostingin sk'sıyla eşler.
func TestRestore_BaglanmisKurtarmaYedegi_UcUcaCalisir(t *testing.T) {
	admin, _, _ := hh_tokenlar(t)
	o := dgYeni(t)
	yetimUzakSahte(t)
	logYakala(t)
	const sk = "c_rstkur"
	dosya := sk + "-20261001-000000.tar.gz"
	silinenDosya(t, o.kok, sk, dosya)
	kHesap(t, o.db, 2400, "kurtar.com", sk, 5, "", dosya)
	kSil(t, o.db, 2400, "kurtar.com", sk)
	dgDomain(t, o.db, 2401, "kurtar.com", sk, 5)

	arsiv := skt_standart(t, sk)
	eskiHazir, eskiHome := yedekHazirla, homeKok
	t.Cleanup(func() { yedekHazirla, homeKok = eskiHazir, eskiHome })
	var hazirlanan []string
	yedekHazirla = func(_ context.Context, _ *sql.DB, hsk, hdosya string) (string, error) {
		hazirlanan = append(hazirlanan, hsk+"/"+hdosya)
		return arsiv, nil
	}
	home := t.TempDir()
	homeKok = home
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(hh_sir))
		r.Post("/domains/{id}/backups/{bid}/geriyukle", (&Handlers{DB: o.db}).Restore)
	})
	cagir := func(id, bid int64) (int, string) {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/domains/%d/backups/%d/geriyukle", id, bid), strings.NewReader(`{"mod":"dosyalar"}`))
		req.Header.Set("Authorization", "Bearer "+admin)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	var bid int64
	if err := o.db.QueryRow(`SELECT id FROM backups WHERE sistem_kullanici=?`, sk).Scan(&bid); err != nil {
		t.Fatal(err)
	}
	// yetimken: yeni domain üzerinden de, eski kimlik üzerinden de bulunamaz
	for _, id := range []int64{2401, 2400} {
		if kod, g := cagir(id, bid); kod != 404 {
			t.Errorf("yetim yedek domain %d üzerinden geri yüklenemez (404): %d %s", id, kod, g)
		}
	}
	if len(hazirlanan) != 0 {
		t.Fatalf("yetimken arşiv hazırlanmamalı: %v", hazirlanan)
	}
	// devral → kurtarma satırı
	if n := devral(context.Background(), o.db, 2401); n != 1 {
		t.Fatalf("devralınan: %d", n)
	}
	kod, g := cagir(2401, bid)
	m := dgJSON(t, g)
	if kod != 200 || m["ok"] != true || m["mod"] != "dosyalar" || m["alan_adi"] != "kurtar.com" || m["dosya"] != dosya {
		t.Fatalf("bağlanmış kurtarma yedeği geri yüklenmeliydi: %d %s", kod, g)
	}
	if len(hazirlanan) != 1 || hazirlanan[0] != sk+"/"+dosya {
		t.Errorf("arşiv hostingin sk'sı ve yedek dosyasıyla hazırlanmalı: %v", hazirlanan)
	}
	if b, _ := os.ReadFile(filepath.Join(home, sk, "index.html")); string(b) != "yeni" {
		t.Errorf("dosyalar GERÇEKTEN geri yüklenmeli (index.html=%q)", b)
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM audit_log WHERE action='yedek.geriyukle' AND target='kurtar.com'`) != 1 {
		t.Error("geri yükleme denetime yazılmalı")
	}
	// ve yanlış hosting (başka domain) üzerinden aynı yedek yine 404
	dgDomain(t, o.db, 2402, "baska.com", "c_baskah", 5)
	if kod, _ := cagir(2402, bid); kod != 404 {
		t.Errorf("yedek başka domain üzerinden geri yüklenemez: %d", kod)
	}
}

// ---------------------------------------------------------------- yalıtım / döngü güvenceleri

// yalitimliIslem READ COMMITTED açar (boşluk kilidi yok → komşu sk/domain_id aralıklarında kilitlenme olmaz). Not: `SET TRANSACTION`
// yalnız SONRAKİ işlemi etkiler ve @@tx_isolation oturum değerini gösterir; işlemin gerçek yalıtımı INNODB_TRX'ten okunur.
func TestYalitimliIslem_ReadCommittedAcar(t *testing.T) {
	db := kDB(t)
	g113_exec(t, db, `CREATE TABLE yi_deneme (a INT)`)
	var iso string
	if err := yalitimliIslem(context.Background(), db, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO yi_deneme VALUES (1)`); err != nil { // InnoDB işlemini başlat
			return err
		}
		return tx.QueryRow(`SELECT trx_isolation_level FROM information_schema.INNODB_TRX WHERE trx_mysql_thread_id = CONNECTION_ID()`).Scan(&iso)
	}); err != nil {
		t.Fatal(err)
	}
	if iso != "READ COMMITTED" {
		t.Errorf("işlem READ COMMITTED olmalı: %q", iso)
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM yi_deneme`) != 1 {
		t.Error("başarılı işlem commit edilmeli")
	}
	// fn hata dönerse geri alınır (yazma kalmaz)
	hata := errors.New("fn hatası")
	if err := yalitimliIslem(context.Background(), db, func(tx *sql.Tx) error {
		_, _ = tx.Exec(`INSERT INTO yi_deneme VALUES (2)`)
		return hata
	}); !errors.Is(err, hata) {
		t.Fatalf("fn hatası aynen dönmeli: %v", err)
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM yi_deneme`) != 1 {
		t.Error("hata durumunda işlem geri alınmalı")
	}
}

// Araya giren bağlama (yetim artık canlı) hata DEĞİLDİR: silme ucu 200 döner, kurtarılan yedeğe dokunmaz ve (satır kilidi
// sayesinde) uzak kopyasını da silmez, kurtarılan satır "silinen" sayılmaz. Bağlama burada TEK satırı (id=?) hedefler: işlenmekte olan
// satırın kilidini beklemeden, silme ucu o satıra gelmeden önce tamamlanır (zamanlamadan bağımsız, deterministik).
func TestSilinenSil_ArayaBaglanan_HataSayilmaz_200(t *testing.T) {
	o := dgYeni(t)
	logYakala(t)
	dosyalar, _ := o.hesap(2500, "ara.com", "c_ara", 5, "", 2)
	kSil(t, o.db, 2500, "ara.com", "c_ara")
	dgDomain(t, o.db, 2501, "ara.com", "c_ara", 5)
	idler := dgYetimIDleri(t, o.db, 2500)
	if len(idler) != 2 {
		t.Fatalf("2 yetim bekleniyordu: %v", idler)
	}
	var uzakSilinen []string
	eski := yetimUzakSil
	t.Cleanup(func() { yetimUzakSil = eski })
	yetimUzakSil = func(ctx context.Context, db *sql.DB, d *Destination, dosya string) error {
		uzakSilinen = append(uzakSilinen, dosya)
		if len(uzakSilinen) == 1 { // ilk satırın uzak silmesi sürerken sahibi İKİNCİ satırı kurtarır
			if n, err := yetimleriBagla(ctx, db, 2501, "id=?", idler[1]); err != nil || n != 1 {
				t.Errorf("kurulum: ikinci satır bağlanmalıydı: n=%d err=%v", n, err)
			}
		}
		return nil
	}
	kod, g := advIstek(o.r, "DELETE", "/s/c_ara", "")
	m := dgJSON(t, g)
	if kod != 200 || m["ok"] != true || m["silinen"] != float64(1) {
		t.Errorf("araya bağlanan satır atlanır, hata sayılmaz → 200 silinen=1: %d %s", kod, g)
	}
	if len(uzakSilinen) != 1 || uzakSilinen[0] != dosyalar[0] {
		t.Errorf("🔴 yalnız ilk satırın uzak kopyası silinmeli, kurtarılan satırınki ASLA: %v", uzakSilinen)
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE domain_id=2501 AND tip='kurtarma' AND dosya=?`, dosyalar[1]) != 1 {
		t.Error("kurtarılan canlı yedek yerinde kalmalı")
	}
	if !varMi(filepath.Join(o.kok, "c_ara", dosyalar[1])) {
		t.Errorf("kurtarılan yedek dosyası silindi: %s", dosyalar[1])
	}
	if varMi(filepath.Join(o.kok, "c_ara", dosyalar[0])) || g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE dosya=?`, dosyalar[0]) != 0 {
		t.Errorf("silinen yedek (satır + dosya) gitmiş olmalı: %s", dosyalar[0])
	}
}

// Başarısız satırlar aynı turda yeniden denenip döngüyü kilitlemez (imleç onları geçer): kalıcı uzak hatada süpürme hızla biter.
func TestSupur_BasarisizSatirlarDonguyuKilitlemez(t *testing.T) {
	o := dgYeni(t)
	sahte := yetimUzakSahte(t)
	logYakala(t)
	sahte.hata = func(*Destination, string) error { return errors.New("uzak kapalı") }
	for i := 0; i < 450; i++ { // 3 parti (200+200+50): hepsi başarısız
		silinenYetim(t, o.db, 2600, "bk.com", "c_bk", fmt.Sprintf("c_bk-20261001-%06d.tar.gz", i), 0, "DATE_SUB(NOW(), INTERVAL 1 HOUR)")
	}
	ctx, iptal := context.WithTimeout(context.Background(), 20*time.Second)
	defer iptal()
	t0 := time.Now()
	SilinenSupur(ctx, o.db)
	if ctx.Err() != nil || time.Since(t0) > 15*time.Second {
		t.Fatalf("başarısız satırlar süpürmeyi kilitledi (aynı satırlar tekrar tekrar denendi): %v", time.Since(t0))
	}
	if n := sahte.say(); n != 450 {
		t.Errorf("her satır TAM BİR kez denenmeli: %d", n)
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE domain_id=0`) != 450 {
		t.Error("başarısız satırların hepsi KORUNMALI")
	}
}

// Devralma/bağlama önce hedef domainin VARLIĞINI doğrular ve domain işlem boyunca kilitlenir (silinemez).
func TestYetimleriBagla_HedefDomainKilitlenir(t *testing.T) {
	o := dgYeni(t)
	yetimUzakSahte(t)
	o.hesap(2700, "kil.com", "c_kil", 5, "", 1)
	kSil(t, o.db, 2700, "kil.com", "c_kil")
	dgDomain(t, o.db, 2701, "kil.com", "c_kil", 5)
	if _, err := yetimleriBagla(context.Background(), o.db, 2799, "sistem_kullanici=?", "c_kil"); !errors.Is(err, errHedefDomainYok) {
		t.Fatalf("olmayan hedef domain → errHedefDomainYok: %v", err)
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE domain_id=2799`) != 0 || g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE domain_id=0`) != 1 {
		t.Error("olmayan domaine HİÇBİR satır bağlanmamalı")
	}
}

// A2: `domains` satırını yazan HER üretim yolu (INSERT INTO domains) satırı yazar yazmaz YetimKalintilariniAyir'i çağırmalı: kimlik
// yeniden kullanılmış bir domainin eski yedek kalıntıları yeni müşteriye görünmesin. Yeni bir domain oluşturma yolu eklenirse bu test
// onu zorunlu kılar (bilinçli izin listesi yok).
func TestA2_DomainSatiriYazanYollar_KalintiAyirmayiCagirir(t *testing.T) {
	bulunan := 0
	for _, kok := range []string{"..", "../../cmd"} {
		_ = filepath.WalkDir(kok, func(yol string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(yol, ".go") || strings.HasSuffix(yol, "_test.go") {
				return nil
			}
			f, e := parser.ParseFile(token.NewFileSet(), yol, nil, 0)
			if e != nil {
				return nil
			}
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				yazar, cagirir := false, false
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					switch x := n.(type) {
					case *ast.BasicLit:
						if x.Kind == token.STRING && strings.Contains(strings.ToUpper(x.Value), "INSERT INTO DOMAINS") {
							yazar = true
						}
					case *ast.CallExpr:
						switch fun := x.Fun.(type) {
						case *ast.SelectorExpr:
							if fun.Sel.Name == "YetimKalintilariniAyir" {
								cagirir = true
							}
						case *ast.Ident:
							if fun.Name == "olusturKalintiAyir" {
								cagirir = true
							}
						}
					}
					return true
				})
				if yazar {
					bulunan++
					if !cagirir {
						t.Errorf("%s: %s `INSERT INTO domains` yazıyor ama YetimKalintilariniAyir çağırmıyor (kimlik yeniden kullanımında eski yedekler yeni müşteriye görünür)", filepath.ToSlash(yol), fn.Name.Name)
					}
				}
			}
			return nil
		})
	}
	if bulunan < 2 {
		t.Errorf("INSERT INTO domains yazan en az 2 üretim yolu bekleniyordu (domains.Create, tasima): %d — arama bozuk olabilir", bulunan)
	}
}

// ---------------------------------------------------------------- rows.Err(): kısmi veri ASLA başarı gibi sunulmaz

func rowsErrDB(t *testing.T, o *dgOrtam, ipucu string) *sql.DB {
	t.Helper()
	var adi string
	if err := o.db.QueryRow(`SELECT DATABASE()`).Scan(&adi); err != nil {
		t.Fatal(err)
	}
	cfg, err := mysql.ParseDSN(os.Getenv("GOSP_TEST_GECICI_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = adi
	baz, err := mysql.NewConnector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	db2 := sql.OpenDB(&dgHataBaglayici{Connector: baz, hedef: ipucu})
	db2.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = db2.Close() })
	return db2
}

// Satır akışı ortada koparsa (rows.Next false → rows.Err) liste/özet/silme uçları yarım veriyi başarı gibi dönmez: 500.
func TestRowsErr_ListeOzetSilinen_YarimVeriBasariGibiDonmez(t *testing.T) {
	o := dgYeni(t)
	yetimUzakSahte(t)
	logYakala(t)
	o.hesap(3700, "re.com", "c_re", 0, "", 2)
	dgDomain(t, o.db, 3701, "canli-re.com", "c_canlire", 0)
	g113_exec(t, o.db, `INSERT INTO backups (domain_id, dosya) VALUES (3701, ?)`, dgAd("c_canlire", 0))
	kSil(t, o.db, 3700, "re.com", "c_re")
	silinenYetim(t, o.db, 3702, "re2.com", "c_re2", dgAd("c_re2", 0), 0, "DATE_SUB(NOW(), INTERVAL 1 HOUR)")
	for _, c := range []struct {
		ad, ipucu, metot, yol string
		yonlendir             func(*Handlers) http.HandlerFunc
	}{
		{"domain yedek listesi", "FROM backups b WHERE b.domain_id=?", "GET", "/d/3701/backups", func(h *Handlers) http.HandlerFunc { return h.List }},
		{"yönetici özeti", "FROM domains ORDER BY alan_adi", "GET", "/ozet", func(h *Handlers) http.HandlerFunc { return h.Ozet }},
		{"silinmiş hesap listesi", "ORDER BY silinme DESC", "GET", "/s", func(h *Handlers) http.HandlerFunc { return h.SilinenListe }},
	} {
		db2 := rowsErrDB(t, o, c.ipucu)
		h := &Handlers{DB: db2}
		r := chi.NewRouter()
		r.Get("/d/{id}/backups", h.List)
		r.Get("/ozet", h.Ozet)
		r.Get("/s", h.SilinenListe)
		kod, g := advIstek(r, c.metot, c.yol, "")
		if kod != http.StatusInternalServerError || strings.Contains(g, "c_canlire-") || strings.Contains(g, `"sk":"c_re`) {
			t.Errorf("%s: satır akışı koparsa 500 beklenirdi (yarım veri başarı gibi dönmemeli): %d %s", c.ad, kod, g)
		}
	}
	// DELETE /s/{sk}: kayıt listesi okunurken akış koparsa hiçbir şey silinmez, 500
	db2 := rowsErrDB(t, o, "SELECT id, iz_id, dosya FROM backups")
	r := chi.NewRouter()
	r.Delete("/s/{sk}", (&Handlers{DB: db2}).SilinenSil)
	if kod, g := advIstek(r, "DELETE", "/s/c_re", ""); kod != http.StatusInternalServerError {
		t.Errorf("silme: kayıt listesi okunamazsa 500: %d %s", kod, g)
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE sistem_kullanici='c_re' AND domain_id=0`) != 2 {
		t.Error("liste okunamadıysa hiçbir yetim silinmemeli")
	}
	// süpürme partisi okunamazsa panik/silme yok, tur sessizce biter
	db3 := rowsErrDB(t, o, "SELECT id, iz_id, sistem_kullanici, dosya, boyut_b")
	SilinenSupur(context.Background(), db3)
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE sistem_kullanici='c_re2'`) != 1 {
		t.Error("parti okunamadıysa hiçbir şey silinmemeli")
	}
}

// ---------------------------------------------------------------- O2: uzak silmesi süren satır kayıt defterinde

// Uzak silme sürerken satır "silinmekte" kayıtlıdır: aynı satırı işlemek isteyen İKİNCİ silme (süpürme + yönetici ucu aynı anda)
// uzak kopyaya dokunmadan atlanır; bağlama o satırı bağlamaz. Bitince kayıt kalkar.
func TestYetimYedegiSil_UzakSilmeSirasindaSatirKayitliVeIkinciSilmeAtlanir(t *testing.T) {
	o := dgYeni(t)
	dosyalar, _ := o.hesap(3000, "kk.com", "c_kk", 5, "", 2)
	kSil(t, o.db, 3000, "kk.com", "c_kk")
	dgDomain(t, o.db, 3001, "kk.com", "c_kk", 5)
	idler := dgYetimIDleri(t, o.db, 3000)
	var uzak []string
	var ikinciHata error
	var kayitli []int64
	var bagN int64
	var bagErr error
	eski := yetimUzakSil
	t.Cleanup(func() { yetimUzakSil = eski })
	yetimUzakSil = func(ctx context.Context, db *sql.DB, d *Destination, dosya string) error {
		uzak = append(uzak, dosya)
		kayitli = yetimSilinmektekiler()
		// aynı satırı başka bir silme (ör. yönetici ucu) işlemek ister: uzak kopyaya DOKUNMADAN atlanmalı
		ikinciHata = yetimYedegiSil(ctx, db, idler[0], "", "c_kk", dosyalar[0], false)
		// aynı anda tüm yetimleri bağlama: silinmekte olan satır hariç diğeri bağlanır
		bagN, bagErr = yetimleriBagla(ctx, db, 3001, "sistem_kullanici=?", "c_kk")
		return nil
	}
	if err := yetimYedegiSil(context.Background(), o.db, idler[0], "", "c_kk", dosyalar[0], false); err != nil {
		t.Fatal(err)
	}
	if len(kayitli) != 1 || kayitli[0] != idler[0] {
		t.Errorf("uzak silme sürerken satır kayıt defterinde olmalı: %v", kayitli)
	}
	if !errors.Is(ikinciHata, errYetimDegil) {
		t.Errorf("🔴 aynı satırı işleyen ikinci silme atlanmalı (errYetimDegil): %v", ikinciHata)
	}
	if len(uzak) != 1 {
		t.Errorf("🔴 uzak kopya TEK kez silinmeli (ikinci silme uzağa gitmemeli): %v", uzak)
	}
	if bagErr != nil || bagN != 1 {
		t.Errorf("silinmekte olan satır hariç 1 yetim bağlanmalı: n=%d err=%v", bagN, bagErr)
	}
	if got := yetimSilinmektekiler(); len(got) != 0 {
		t.Errorf("silme bitince kayıt kalkmalı: %v", got)
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE id=?`, idler[0]) != 0 || varMi(filepath.Join(o.kok, "c_kk", dosyalar[0])) {
		t.Error("silinen satır ve dosya gitmiş olmalı")
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE id=? AND domain_id=3001`, idler[1]) != 1 || !varMi(filepath.Join(o.kok, "c_kk", dosyalar[1])) {
		t.Error("bağlanan ikinci satır canlı ve dosyası yerinde olmalı")
	}
}

// Uzak silme başarısızsa kayıt KALKAR (defter sızmaz): satır korunur, hemen yeniden denenebilir ve sonradan bağlanabilir.
func TestYetimYedegiSil_UzakHatadaKayitKalkar(t *testing.T) {
	o := dgYeni(t)
	sahte := yetimUzakSahte(t)
	sahte.hata = func(*Destination, string) error { return errors.New("uzak kapalı") }
	dosyalar, _ := o.hesap(3010, "kb.com", "c_kb", 5, "", 1)
	kSil(t, o.db, 3010, "kb.com", "c_kb")
	dgDomain(t, o.db, 3011, "kb.com", "c_kb", 5)
	idler := dgYetimIDleri(t, o.db, 3010)
	for i := 0; i < 2; i++ { // ikinci deneme de uzağa gider: kayıt birinci hatadan sonra kalkmıştı
		if err := yetimYedegiSil(context.Background(), o.db, idler[0], "", "c_kb", dosyalar[0], false); err == nil || errors.Is(err, errYetimDegil) {
			t.Fatalf("deneme %d: uzak hata dönmeliydi: %v", i, err)
		}
	}
	if sahte.say() != 2 {
		t.Errorf("her deneme uzağa gitmeli: %d", sahte.say())
	}
	if got := yetimSilinmektekiler(); len(got) != 0 {
		t.Errorf("🔴 başarısız silme kayıt defterini temizlemedi (satır sonsuza dek bağlanamaz): %v", got)
	}
	if n, err := yetimleriBagla(context.Background(), o.db, 3011, "sistem_kullanici=?", "c_kb"); err != nil || n != 1 {
		t.Errorf("başarısız silmeden sonra satır bağlanabilmeli: n=%d err=%v", n, err)
	}
}

// yetimHalaYetim KİLİTLİ okur: satırı güncelleyen (henüz commit etmemiş) bir bağlamanın commit'ini BEKLER; commit'ten sonra satır
// yetim değildir (errYetimDegil). Kilitsiz (tutarlı anlık) okuma eski "yetim" cevabını verip silmeyi uzak adıma geçirirdi.
func TestYetimHalaYetim_SurenBaglamayiBekler(t *testing.T) {
	o := dgYeni(t)
	o.hesap(3030, "bk.com", "c_bkl", 5, "", 1)
	kSil(t, o.db, 3030, "bk.com", "c_bkl")
	dgDomain(t, o.db, 3031, "bk.com", "c_bkl", 5)
	idler := dgYetimIDleri(t, o.db, 3030)
	tx, err := o.db.Begin() // bağlama işlemi: satırı bağladı ama henüz commit etmedi
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE backups SET domain_id=3031, eski_domain_id=0, iz_id='', silinme=NULL, son_kullanma=NULL, tip='kurtarma' WHERE id=?`, idler[0]); err != nil {
		t.Fatal(err)
	}
	sonuc := make(chan error, 1)
	go func() { sonuc <- yetimHalaYetim(context.Background(), o.db, idler[0]) }()
	select {
	case e := <-sonuc:
		t.Fatalf("🔴 yetimHalaYetim süren bağlamanın commit'ini BEKLEMEDİ (kilitsiz okuma): %v", e)
	case <-time.After(500 * time.Millisecond):
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-sonuc:
		if !errors.Is(e, errYetimDegil) {
			t.Errorf("bağlama commit edildikten sonra errYetimDegil beklenirdi: %v", e)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("yetimHalaYetim commit'ten sonra dönmedi")
	}
}

// Uzak adım yetimUzakSureSiniri ile sınırlıdır: kara delik bir hedef satır kilidini süresiz tutmaz; hata olur, satır + dosya KALIR.
func TestYetimYedegiSil_UzakAdimSureSinirli(t *testing.T) {
	o := dgYeni(t)
	eskiSn := yetimUzakSureSiniri
	yetimUzakSureSiniri = 300 * time.Millisecond
	t.Cleanup(func() { yetimUzakSureSiniri = eskiSn })
	eski := yetimUzakSil
	t.Cleanup(func() { yetimUzakSil = eski })
	yetimUzakSil = func(ctx context.Context, db *sql.DB, d *Destination, dosya string) error {
		select {
		case <-ctx.Done(): // kara delik: yalnız süre sınırı bitirir
			return ctx.Err()
		case <-time.After(5 * time.Second): // emniyet: süre sınırı UYGULANMADIysa test askıda kalmasın
			return errors.New("süre sınırı uygulanmadı")
		}
	}
	dosyalar, _ := o.hesap(3020, "ks.com", "c_ks", 5, "", 1)
	kSil(t, o.db, 3020, "ks.com", "c_ks")
	idler := dgYetimIDleri(t, o.db, 3020)
	t0 := time.Now()
	err := yetimYedegiSil(context.Background(), o.db, idler[0], "", "c_ks", dosyalar[0], false)
	if sure := time.Since(t0); sure > 10*time.Second {
		t.Fatalf("uzak adım süre sınırında kesilmedi: %v", sure)
	}
	var sh *yetimSilHatasi
	if !errors.As(err, &sh) || !sh.uzak || !uzakHedefUlasilamiyor(sh.err) {
		t.Fatalf("uzak kaynaklı, hedef-erişilemez sınıfında hata beklenirdi: %v", err)
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE id=?`, idler[0]) != 1 || !varMi(filepath.Join(o.kok, "c_ks", dosyalar[0])) {
		t.Error("süre sınırı aşılınca satır + dosya KORUNMALI")
	}
}

// ---------------------------------------------------------------- devre kesici (aynı izin ölü hedefi)

func TestUzakHedefUlasilamiyor_Siniflandirma(t *testing.T) {
	for _, c := range []struct {
		hata string
		bek  bool
	}{
		{"lftp: Login failed: 530 Login incorrect", true},
		{"lftp: cd: Fatal error: Could not resolve host name x.invalid", true},
		{"lftp: Connection refused", true},
		{"lftp: Fatal error: Host key verification failed", true},
		{"genel hedef (/a): lftp: signal: killed", true},
		{"context deadline exceeded", true},
		{"lftp: connect: No route to host", true},
		{"lftp: Connection timed out", true},
		{"lftp: cannot connect to ftp.x.invalid", true},
		{"lftp: getaddrinfo: Name or service not known", true},
		{"lftp: connect: Network is unreachable", true},
		{"lftp: Timeout - reconnecting", true},
		{"lftp: Access failed: 550 Permission denied", false},
		{"uzak kopya silme komutundan SONRA hâlâ listeleniyor (/a/b) — hedefteki izinleri kontrol edin", false},
		{"başka bir hata", false},
	} {
		if got := uzakHedefUlasilamiyor(errors.New(c.hata)); got != c.bek {
			t.Errorf("%q → %v, beklenen %v", c.hata, got, c.bek)
		}
	}
	if !uzakHedefUlasilamiyor(ErrLftpYok) || !uzakHedefUlasilamiyor(fmt.Errorf("x: %w", context.DeadlineExceeded)) || uzakHedefUlasilamiyor(nil) {
		t.Error("ErrLftpYok/DeadlineExceeded erişilemez sayılmalı; nil sayılmamalı")
	}
}

// Ölü hedefli hesap (5 yetim, ID'leri küçük) turda TEK deneme alır; ID'si büyük sağlıklı hesap (3 yetim) temizlenir.
// Dosyaya özgü hata (izin) kesiciyi AÇMAZ: o hesabın tüm satırları denenir.
func TestSupur_DevreKesici_OluHedefTekDeneme_SaglikliHesapTemizlenir(t *testing.T) {
	o := dgYeni(t)
	sahte := yetimUzakSahte(t)
	lg := logYakala(t)
	sahte.hata = func(d *Destination, dosya string) error {
		if strings.HasPrefix(dosya, "c_olu") {
			return errors.New("lftp: Fatal error: Could not resolve host name olu.invalid")
		}
		return nil
	}
	o.hesap(3100, "olu.com", "c_olu", 5, "", 5)
	o.hesap(3101, "iyi.com", "c_iyi", 5, "", 3)
	kSil(t, o.db, 3100, "olu.com", "c_olu")
	kSil(t, o.db, 3101, "iyi.com", "c_iyi")
	g113_exec(t, o.db, `UPDATE backups SET son_kullanma=DATE_SUB(NOW(), INTERVAL 1 HOUR) WHERE domain_id=0`)
	SilinenSupur(context.Background(), o.db)
	if n := g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE sistem_kullanici='c_olu' AND domain_id=0`); n != 5 {
		t.Errorf("ölü hedefli hesabın 5 yetimi de KORUNMALI: %d", n)
	}
	if n := g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE sistem_kullanici='c_iyi'`); n != 0 {
		t.Errorf("🔴 sağlıklı hesabın yetimleri ölü hedefin arkasında kaldı: %d/3 satır duruyor", n)
	}
	say := map[string]int{}
	sahte.mu.Lock()
	for _, c := range sahte.cagrilar {
		say[c.dosya[:strings.Index(c.dosya, "-")]]++
	}
	sahte.mu.Unlock()
	if say["c_olu"] != 1 || say["c_iyi"] != 3 {
		t.Errorf("c_olu TEK deneme, c_iyi 3 deneme beklenirdi: %v", say)
	}
	if g := lg.String(); !strings.Contains(g, "3 yedek silindi") || !strings.Contains(g, "1 silinemedi") || !strings.Contains(g, "4 denenmedi") {
		t.Errorf("özet günlüğü silinen/silinemeyen/denenmeyen sayılarını söylemeli: %q", g)
	}
}

func TestSupur_DevreKesici_DosyayaOzguHataKesiciyiAcmaz(t *testing.T) {
	o := dgYeni(t)
	sahte := yetimUzakSahte(t)
	logYakala(t)
	sahte.hata = func(d *Destination, dosya string) error {
		if strings.HasSuffix(dosya, "-000001.tar.gz") { // yalnız bir dosya silinemiyor (izin)
			return errors.New("lftp: Access failed: 550 Permission denied")
		}
		return nil
	}
	o.hesap(3110, "izin.com", "c_izin", 5, "", 4)
	kSil(t, o.db, 3110, "izin.com", "c_izin")
	g113_exec(t, o.db, `UPDATE backups SET son_kullanma=DATE_SUB(NOW(), INTERVAL 1 HOUR) WHERE domain_id=0`)
	SilinenSupur(context.Background(), o.db)
	if n := sahte.say(); n != 4 {
		t.Errorf("dosyaya özgü hata kesiciyi açmamalı: 4 satırın 4'ü de denenmeli, %d", n)
	}
	if n := g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE sistem_kullanici='c_izin'`); n != 1 {
		t.Errorf("yalnız silinemeyen tek satır kalmalı: %d", n)
	}
}

// Yönetici silme ucu da aynı kesiciyi kullanır: ölü hedefte TEK uzak deneme, 409 + uzagi_yoksay=true, hepsi korunur.
func TestSilinenSil_DevreKesici_OluHedefTekDeneme(t *testing.T) {
	o := dgYeni(t)
	sahte := yetimUzakSahte(t)
	logYakala(t)
	sahte.hata = func(*Destination, string) error { return errors.New("lftp: Connection refused") }
	o.hesap(3120, "kesici.com", "c_kes", 5, "", 5)
	kSil(t, o.db, 3120, "kesici.com", "c_kes")
	kod, g := advIstek(o.r, "DELETE", "/s/c_kes", "")
	m := dgJSON(t, g)
	if kod != 409 || m["uzagi_yoksay"] != true {
		t.Errorf("ölü hedef → 409 uzagi_yoksay=true: %d %s", kod, g)
	}
	if n := sahte.say(); n != 1 {
		t.Errorf("TEK uzak deneme beklenirdi: %d", n)
	}
	if !strings.Contains(fmt.Sprint(m["hata"]), "5 yedek silinemedi") {
		t.Errorf("5 yedeğin hepsi silinemedi sayılmalı: %s", g)
	}
	if n := g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE domain_id=0`); n != 5 {
		t.Errorf("hepsi korunmalı: %d", n)
	}
	// uzağı yoksayınca hepsi silinir (kesici yalnız uzak adım içindir)
	kod, g = advIstek(o.r, "DELETE", "/s/c_kes?uzagi_yoksay=1", "")
	if kod != 200 || g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE domain_id=0`) != 0 {
		t.Errorf("uzağı yoksayınca 5 yedek silinmeli: %d %s", kod, g)
	}
}

// Dosyaya özgü hata (izin) yönetici ucunda da kesiciyi AÇMAZ: tüm satırlar denenir, yalnız silinemeyen kalır (409, uzagi_yoksay=true).
func TestSilinenSil_DevreKesici_DosyayaOzguHataKesiciyiAcmaz(t *testing.T) {
	o := dgYeni(t)
	sahte := yetimUzakSahte(t)
	logYakala(t)
	sahte.hata = func(d *Destination, dosya string) error {
		if strings.HasSuffix(dosya, "-000001.tar.gz") {
			return errors.New("lftp: Access failed: 550 Permission denied")
		}
		return nil
	}
	o.hesap(3130, "dosya.com", "c_dos", 5, "", 4)
	kSil(t, o.db, 3130, "dosya.com", "c_dos")
	kod, g := advIstek(o.r, "DELETE", "/s/c_dos", "")
	m := dgJSON(t, g)
	if kod != 409 || m["uzagi_yoksay"] != true || m["silinen"] != float64(3) {
		t.Errorf("3 satır silinmeli, 1'i uzak dosya hatasıyla kalmalı (409, uzagi_yoksay): %d %s", kod, g)
	}
	if n := sahte.say(); n != 4 {
		t.Errorf("dosyaya özgü hata kesiciyi açmamalı: 4 satırın 4'ü de denenmeli: %d", n)
	}
	if n := g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE sistem_kullanici='c_dos'`); n != 1 {
		t.Errorf("yalnız silinemeyen tek satır kalmalı: %d", n)
	}
}

// ---------------------------------------------------------------- S2 tek tur

func TestSahipsizIsaretle_AyniAndaTekTur(t *testing.T) {
	db := silinenDB(t)
	silinenOrtam(t)
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (9990,'canli.test','c_canli')`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya, boyut_b) VALUES (9991,'c_hayalet-20261001-000000.tar.gz',5)`)
	sahipsizIsaretKilit.Lock() // başka bir tur sürüyor (ör. açılış turu ile 04:00 turu çakıştı)
	i, tn := SilinenSahipsizIsaretle(context.Background(), db)
	sahipsizIsaretKilit.Unlock()
	if i != 0 || tn != 0 || g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=9991 AND silinme IS NULL`) != 1 {
		t.Fatalf("🔴 devam eden tur varken ikinci tur işaretledi: (%d,%d)", i, tn)
	}
	if i, _ := SilinenSahipsizIsaretle(context.Background(), db); i != 1 {
		t.Errorf("kilit boşalınca tur işaretlemeli: %d", i)
	}
}

// ---------------------------------------------------------------- silinen_yedek_gun okunamazsa SİLME yönünde yorumlanmaz

func TestSilinenAyar_OkunamazsaSaklamaYonundeDavranir(t *testing.T) {
	// Okuma hatası ("tablo yok" ile benzetilir) ≠ ayarsız: GET 500; iz yazımı süresiz saklar; S2 işaretlemeyi sonraya bırakır.
	t.Run("GET 500", func(t *testing.T) {
		o := dgYeni(t)
		logYakala(t)
		h := &Handlers{DB: o.db}
		w := httptest.NewRecorder()
		h.SilinenAyarGet(w, httptest.NewRequest("GET", "/x", nil))
		if w.Code != 200 {
			t.Fatalf("ayar tablosu varken 200 beklenirdi: %d %s", w.Code, w.Body.String())
		}
		g113_exec(t, o.db, `DROP TABLE cp_ayarlar`)
		w = httptest.NewRecorder()
		h.SilinenAyarGet(w, httptest.NewRequest("GET", "/x", nil))
		if w.Code != 500 || strings.Contains(w.Body.String(), `"gun"`) {
			t.Errorf("🔴 okunamayan ayar 200+null (= 'ayarsız') gibi gösterilmemeli: %d %s", w.Code, w.Body.String())
		}
	})
	t.Run("iz yazımı süresiz saklar", func(t *testing.T) {
		o := dgYeni(t)
		logYakala(t)
		o.hesap(3200, "oku.com", "c_oku", 5, "", 2)
		ctx := context.Background()
		iz := SilinenIzHazirla(ctx, o.db, 3200, "oku.com", "c_oku")
		g113_exec(t, o.db, `DELETE FROM domains WHERE id=3200`)
		g113_exec(t, o.db, `DROP TABLE cp_ayarlar`)
		iz.Yaz(ctx, o.db)
		if n := g113_say(t, o.db, `SELECT COUNT(*) FROM backups WHERE domain_id=0 AND silinme IS NOT NULL AND son_kullanma IS NULL`); n != 2 {
			t.Errorf("🔴 ayar okunamadı: yetimler süresiz saklanmalı (son_kullanma NULL), %d/2", n)
		}
	})
	t.Run("S2 sonraya bırakır", func(t *testing.T) {
		db := silinenDB(t)
		silinenOrtam(t)
		logYakala(t)
		g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (9992,'canli.test','c_canli')`)
		g113_exec(t, db, `INSERT INTO backups (domain_id, dosya, boyut_b) VALUES (9993,'c_hayalet2-20261001-000000.tar.gz',5)`)
		g113_exec(t, db, `DROP TABLE cp_ayarlar`)
		if i, _ := SilinenSahipsizIsaretle(context.Background(), db); i != 0 || g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=9993 AND silinme IS NULL`) != 1 {
			t.Errorf("🔴 ayar okunamazken S2 yanlış süreyle işaretledi: %d", i)
		}
	})
	t.Run("kalıntı ayırma süresiz saklar", func(t *testing.T) {
		db := silinenDB(t)
		silinenOrtam(t)
		logYakala(t)
		g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (9994,'yeni.test','c_yeni')`)
		g113_exec(t, db, `INSERT INTO backups (domain_id, dosya, boyut_b) VALUES (9994,'c_eskisahip-20261001-000000.tar.gz',5)`)
		g113_exec(t, db, `DROP TABLE cp_ayarlar`)
		if n := YetimKalintilariniAyir(context.Background(), db, 9994); n != 1 {
			t.Fatalf("kalıntı yine de yeni müşteriden ayrılmalı: %d", n)
		}
		if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=0 AND son_kullanma IS NULL AND silinme IS NOT NULL`) != 1 {
			t.Error("🔴 ayar okunamadı: kalıntı süresiz saklanmalı")
		}
	})
}

// ---------------------------------------------------------------- A2: kalıntı ayırma bütçesi (30 sn) ağır kilitte kısa bütçeye düşmez

// Kalıntı satırı 4 sn başka bir işlem kilitli tutuyor: eski 3 sn'lik iz bütçesi UPDATE'i yarıda keser ve eski müşterinin yedeği yeni
// müşteriye görünür kalırdı; kalintiZamanAsimi (30 sn) beklemeyi kaldırır.
func TestYetimKalintilariniAyir_KilitliSatirBeklenir(t *testing.T) {
	db := silinenDB(t)
	silinenOrtam(t)
	logYakala(t)
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (9995,'yeni.test','c_yeni2')`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya, boyut_b) VALUES (9995,'c_eskisahip2-20261001-000000.tar.gz',5)`)
	tutan, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tutan.Rollback()
	if _, err := tutan.Exec(`SELECT id FROM backups WHERE domain_id=9995 FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(4 * time.Second)
		_ = tutan.Rollback()
	}()
	if n := YetimKalintilariniAyir(context.Background(), db, 9995); n != 1 {
		t.Errorf("🔴 kilitli kalıntı satırı beklenip işaretlenmeliydi (bütçe ≥ 30 sn): %d", n)
	}
}
