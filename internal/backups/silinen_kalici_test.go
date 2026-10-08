package backups

// silinen_kalici_test.go — "bilinen sınırlar" KALICI çözümleri (silinen.go):
//   S1  domaine özel FTP/SFTP hedefinin silme anındaki anlık görüntüsü (backup_yetim_hedefler)
//   S2  silme izi yazılamazsa tek yeniden deneme + sahipsiz satırların sonradan işaretlenmesi
//   S3  bağlanan eski yedekler 'kurtarma' tipine geçer, pruneOld/pruneManuelYedek dokunmaz
//   S4  yetim satır domain_id=0'dır (domain id'si yeniden kullanılsa bile sızıntı yok)
//   +   elle bağlamada alan adı/sahip farkı için zorunlu onay (zorla)
// Gerçek BackupRoot'a YAZILMAZ ve AĞA ÇIKILMAZ (uzak silme yetimUzakSil ile taklit edilir).
// Geçici MariaDB gerekir (GOSP_TEST_GECICI_DSN yoksa atlanır).

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"girginospanel/internal/gizli"

	"github.com/go-chi/chi/v5"
)

const kParola = "GIZLI-PAROLA-7f3a9"

// ---------------------------------------------------------------- yardımcılar

// kDB: silinenDB + List/Restore'un okuduğu yan tablolar + denetim tablosu.
func kDB(t *testing.T) *sql.DB {
	t.Helper()
	db := silinenDB(t)
	g113_exec(t, db, `ALTER TABLE backups ADD COLUMN job_id BIGINT NULL`)
	g113_exec(t, db, `CREATE TABLE backup_uzak_kopyalar (id BIGINT AUTO_INCREMENT PRIMARY KEY, backup_id BIGINT NULL, kaynak VARCHAR(8) NOT NULL DEFAULT 'hedef', durum VARCHAR(16) NOT NULL DEFAULT 'bekliyor')`)
	g113_exec(t, db, `CREATE TABLE audit_log (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, ts TIMESTAMP NULL DEFAULT current_timestamp(), actor_user_id BIGINT UNSIGNED NULL,
		actor_username VARCHAR(64) NOT NULL DEFAULT '', adina_domain_id BIGINT UNSIGNED NULL, ip VARCHAR(45) NOT NULL DEFAULT '', action VARCHAR(64) NOT NULL,
		target VARCHAR(255) NOT NULL DEFAULT '', detail LONGTEXT NULL, ok TINYINT(1) NOT NULL DEFAULT 1, reseller_id BIGINT UNSIGNED NOT NULL DEFAULT 0)`)
	return db
}

func kRouter(db *sql.DB) http.Handler {
	db.SetMaxOpenConns(8)
	h := &Handlers{DB: db}
	r := chi.NewRouter()
	r.Get("/s", h.SilinenListe)
	r.Post("/s/{sk}/bagla", h.SilinenBagla)
	r.Delete("/s/{sk}", h.SilinenSil)
	r.Get("/d/{id}/backups", h.List)
	r.Delete("/d/{id}/backups/{bid}", h.Delete)
	r.Post("/d/{id}/backups/{bid}/geriyukle", h.Restore)
	r.Get("/d/{id}/backups/{bid}/icerik", h.Icerik)
	r.Get("/d/{id}/backups/{bid}/indir", h.Download)
	return r
}

// kHesap: canlı hosting + (host doluysa) domaine özel FTP/SFTP hedefi + yedek satırları. Hedefin
// ŞİFRELİ ham parola değerini döner.
func kHesap(t *testing.T, db *sql.DB, id int64, alan, sk string, rid int64, host string, dosyalar ...string) (ham string) {
	t.Helper()
	var ridV any
	if rid > 0 {
		ridV = rid
	}
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES (?,?,?,?)`, id, alan, sk, ridV)
	if host != "" {
		ham = gizli.SaklaBagli(kParola, "yedek")
		g113_exec(t, db, `INSERT INTO backup_destinations (domain_id, tip, host, port, kullanici, parola, uzak_dizin) VALUES (?, 'sftp', ?, 2222, 'ftpkul', ?, '/yedekler/musteri')`, id, host, ham)
	}
	for _, d := range dosyalar {
		g113_exec(t, db, `INSERT INTO backups (domain_id, dosya, boyut_b) VALUES (?,?,5)`, id, d)
	}
	return ham
}

// kSil: gerçek silme sırası — iz hazırla (domain satırı varken), domains sil (hedef CASCADE ile gider), iz yaz.
func kSil(t *testing.T, db *sql.DB, id int64, alan, sk string) {
	t.Helper()
	ctx := context.Background()
	iz := SilinenIzHazirla(ctx, db, id, alan, sk)
	g113_exec(t, db, `DELETE FROM domains WHERE id=?`, id)
	iz.Yaz(ctx, db)
}

type uzakCagri struct {
	d     *Destination // anlık görüntüden kurulan hedef (nil = hedef yok)
	dosya string
}

type sahteUzak struct {
	mu       sync.Mutex
	cagrilar []uzakCagri
	hata     func(d *Destination, dosya string) error
}

func (s *sahteUzak) say() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.cagrilar)
}

// yetimUzakSahte: yetim yedeğin uzak silmesini taklit eder (ağa ÇIKMAZ).
func yetimUzakSahte(t *testing.T) *sahteUzak {
	t.Helper()
	s := &sahteUzak{}
	eski := yetimUzakSil
	yetimUzakSil = func(ctx context.Context, db *sql.DB, d *Destination, dosya string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		var kopya *Destination
		if d != nil {
			c := *d
			kopya = &c
		}
		s.cagrilar = append(s.cagrilar, uzakCagri{d: kopya, dosya: dosya})
		if s.hata != nil {
			return s.hata(kopya, dosya)
		}
		return nil
	}
	t.Cleanup(func() { yetimUzakSil = eski })
	return s
}

type kLog struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *kLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *kLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func logYakala(t *testing.T) *kLog {
	t.Helper()
	l := &kLog{}
	log.SetOutput(l)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return l
}

func ham(t *testing.T, db *sql.DB, q string, args ...any) string {
	t.Helper()
	var s string
	if err := db.QueryRow(q, args...).Scan(&s); err != nil {
		t.Fatalf("okuma: %v\n%s", err, q)
	}
	return s
}

// tetikKur: backups üzerinde ilk n UPDATE'i yapay hatayla reddeden tetikleyici (sayaç MyISAM:
// ifade geri alınsa bile sayaç kalıcı).
func tetikKur(t *testing.T, db *sql.DB, n int) {
	t.Helper()
	g113_exec(t, db, `CREATE TABLE tetik_sayac (n INT) ENGINE=MyISAM`)
	g113_exec(t, db, fmt.Sprintf(`CREATE TRIGGER tetik_bu BEFORE UPDATE ON backups FOR EACH ROW
	BEGIN
	  IF (SELECT COUNT(*) FROM tetik_sayac) < %d THEN
	    INSERT INTO tetik_sayac VALUES (1);
	    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'test: yapay UPDATE hatasi';
	  END IF;
	END`, n))
}

// ---------------------------------------------------------------- S1

func TestS1_SilmeAninda_HedefAnlikGoruntusu_SifreliHamKopya(t *testing.T) {
	db := kDB(t)
	silinenOrtam(t)
	dosya1, dosya2 := "c_s1-20261001-000000.tar.gz", "c_s1-20261002-000000.tar.gz"
	hamParola := kHesap(t, db, 7001, "s1.test", "c_s1", 4, "ftp.musteri.invalid", dosya1, dosya2)
	// Hedefsiz domain ve yedeksiz hedefli domain: anlık görüntü OLUŞMAMALI.
	kHesap(t, db, 7002, "hedefsiz.test", "c_hs", 0, "", "c_hs-20261001-000000.tar.gz")
	kHesap(t, db, 7003, "yedeksiz.test", "c_ys", 0, "ftp.baska.invalid")

	kSil(t, db, 7001, "s1.test", "c_s1")
	kSil(t, db, 7002, "hedefsiz.test", "c_hs")
	kSil(t, db, 7003, "yedeksiz.test", "c_ys")

	if g113_say(t, db, `SELECT COUNT(*) FROM backup_destinations WHERE domain_id IN (7001,7003)`) != 0 {
		t.Fatal("kurulum: domain hedefi CASCADE ile gitmeliydi (S1'in nedeni)")
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backup_yetim_hedefler`) != 1 {
		t.Fatalf("yalnız yedeği olan hedefli domain için 1 anlık görüntü beklenirdi: %d", g113_say(t, db, `SELECT COUNT(*) FROM backup_yetim_hedefler`))
	}
	var eski int64
	var izID, tip, host, kul, parola, dizin string
	var port int
	if err := db.QueryRow(`SELECT iz_id, eski_domain_id, tip, host, port, kullanici, parola, uzak_dizin FROM backup_yetim_hedefler`).
		Scan(&izID, &eski, &tip, &host, &port, &kul, &parola, &dizin); err != nil {
		t.Fatal(err)
	}
	if len(izID) != 32 || strings.Trim(izID, "0123456789abcdef") != "" {
		t.Errorf("iz_id 32 haneli hex (16 bayt crypto/rand) olmalı: %q", izID)
	}
	if eski != 7001 || tip != "sftp" || host != "ftp.musteri.invalid" || port != 2222 || kul != "ftpkul" || dizin != "/yedekler/musteri" {
		t.Errorf("anlık görüntü alanları: %d %s %s %d %s %s", eski, tip, host, port, kul, dizin)
	}
	if parola != hamParola {
		t.Errorf("parola ŞİFRELİ HAM değer aynen kopyalanmalı: %q != %q", parola, hamParola)
	}
	if gizli.SifreliMi(hamParola) && strings.Contains(parola, kParola) {
		t.Error("anlık görüntüde DÜZ parola var")
	}
	d, err := yetimHedefOku(context.Background(), db, izID)
	if err != nil || d == nil || d.Parola != kParola || d.Host != "ftp.musteri.invalid" || d.Port != 2222 || d.Kullanici != "ftpkul" || d.UzakDizin != "/yedekler/musteri" || d.Tip != "sftp" || !d.Aktif {
		t.Fatalf("yetimHedefOku (CozBagli ile çözülmeli): %+v %v", d, err)
	}
	if d, err := yetimHedefOku(context.Background(), db, "olmayan-iz"); d != nil || err != nil {
		t.Errorf("anlık görüntüsü olmayan iz için nil,nil beklenir: %+v %v", d, err)
	}
	if d, err := yetimHedefOku(context.Background(), db, ""); d != nil || err != nil {
		t.Errorf("boş iz_id için nil,nil beklenir: %+v %v", d, err)
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=0 AND eski_domain_id=7001 AND silinme IS NOT NULL AND iz_id=?`, izID) != 2 {
		t.Error("2 yetim satır (domain_id=0, eski_domain_id=7001) anlık görüntüyle AYNI iz_id'yi taşımalı")
	}
	// hedefsiz domainin yetimi de bir iz_id taşır (anlık görüntüsüz): başka silmeyle karışmaz
	var iz2 string
	_ = db.QueryRow(`SELECT iz_id FROM backups WHERE eski_domain_id=7002`).Scan(&iz2)
	if len(iz2) != 32 || iz2 == izID {
		t.Errorf("her silmenin KENDİ iz_id'si olmalı: %q / %q", iz2, izID)
	}
}

func TestS1_YetimTemizlik_AnlikGoruntuHedefiKullanir_TumYetimlerBitinceSatirSilinir(t *testing.T) {
	db := kDB(t)
	kok := silinenOrtam(t)
	sahte := yetimUzakSahte(t)
	dosya1, dosya2 := "c_s3-20261001-000000.tar.gz", "c_s3-20261002-000000.tar.gz"
	y1 := silinenDosya(t, kok, "c_s3", dosya1)
	y2 := silinenDosya(t, kok, "c_s3", dosya2)
	kHesap(t, db, 7010, "s3.test", "c_s3", 0, "ftp.musteri.invalid", dosya1, dosya2)
	kSil(t, db, 7010, "s3.test", "c_s3")
	g113_exec(t, db, `UPDATE backups SET son_kullanma=DATE_SUB(NOW(), INTERVAL 1 HOUR) WHERE dosya=?`, dosya1)
	g113_exec(t, db, `UPDATE backups SET son_kullanma=DATE_ADD(NOW(), INTERVAL 3 DAY) WHERE dosya=?`, dosya2)

	SilinenSupur(context.Background(), db)

	if sahte.say() != 1 {
		t.Fatalf("1 uzak silme çağrısı beklenirdi: %d", sahte.say())
	}
	c := sahte.cagrilar[0]
	if c.d == nil || c.d.Host != "ftp.musteri.invalid" || c.d.Port != 2222 || c.d.Kullanici != "ftpkul" || c.d.Parola != kParola ||
		c.d.UzakDizin != "/yedekler/musteri" || c.d.Tip != "sftp" || c.dosya != dosya1 {
		t.Fatalf("uzak silme ANLIK GÖRÜNTÜ hedefiyle çağrılmalı (çözülmüş parola dahil): %+v dosya=%s", c.d, c.dosya)
	}
	if varMi(y1) || g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE dosya=?`, dosya1) != 0 {
		t.Error("süresi dolan yetim (dosya+satır) silinmeliydi")
	}
	if !varMi(y2) {
		t.Error("süresi dolmayan yetim dosyası silindi")
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backup_yetim_hedefler WHERE eski_domain_id=7010`) != 1 {
		t.Fatal("hâlâ yetim satır varken anlık görüntü SİLİNMEMELİ")
	}
	g113_exec(t, db, `UPDATE backups SET son_kullanma=DATE_SUB(NOW(), INTERVAL 1 HOUR) WHERE dosya=?`, dosya2)
	SilinenSupur(context.Background(), db)
	if sahte.say() != 2 || varMi(y2) {
		t.Errorf("ikinci yetim de silinmeliydi: çağrı=%d", sahte.say())
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backup_yetim_hedefler`) != 0 {
		t.Error("TÜM yetimler bitince anlık görüntü satırı (parola) SİLİNMELİ")
	}
}

func TestS1_SilUcu_UzakHataMaskeli_SatirKalir_UzagiYoksayarAnlikGoruntuyuTemizler(t *testing.T) {
	db := kDB(t)
	kok := silinenOrtam(t)
	sahte := yetimUzakSahte(t)
	logs := logYakala(t)
	r := kRouter(db)
	dosya := "c_s4-20261001-000000.tar.gz"
	y := silinenDosya(t, kok, "c_s4", dosya)
	kHesap(t, db, 7020, "s4.test", "c_s4", 0, "ftp.musteri.invalid", dosya)
	kSil(t, db, 7020, "s4.test", "c_s4")

	// Hata metni parolayı İÇERİYOR (kötü niyetli/özensiz lftp çıktısı): yanıt ve log maskeli olmalı.
	sahte.hata = func(d *Destination, _ string) error {
		return fmt.Errorf("lftp: Login failed: password %s rejected for %s", d.Parola, d.Kullanici)
	}
	kod, g := advIstek(r, "DELETE", "/s/c_s4", "")
	if kod != 409 || !strings.Contains(g, `"uzagi_yoksay":true`) {
		t.Fatalf("uzak hata → 409: %d %s", kod, g)
	}
	if strings.Contains(g, kParola) || strings.Contains(logs.String(), kParola) {
		t.Errorf("PAROLA yanıta/loga sızdı: yanıt=%s", g)
	}
	if !varMi(y) || g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE dosya=?`, dosya) != 1 {
		t.Fatal("uzak silinemediyse yerel dosya + satır KALMALI")
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backup_yetim_hedefler`) != 1 {
		t.Fatal("yetim satır varken anlık görüntü kalmalı")
	}
	// Süpürme de aynı hatayı loglar: parola yine yok.
	g113_exec(t, db, `UPDATE backups SET son_kullanma=DATE_SUB(NOW(), INTERVAL 1 HOUR)`)
	SilinenSupur(context.Background(), db)
	if strings.Contains(logs.String(), kParola) {
		t.Error("süpürme logunda PAROLA var")
	}
	if !strings.Contains(logs.String(), "temizlenemedi") {
		t.Error("kontrol: süpürme hatası loglanmış olmalıydı")
	}
	// ErrLftpYok sabit mesajdır, aynen yayılır.
	sahte.hata = func(*Destination, string) error { return ErrLftpYok }
	if kod, g := advIstek(r, "DELETE", "/s/c_s4", ""); kod != 409 || !strings.Contains(g, "lftp kurulu değil") {
		t.Errorf("ErrLftpYok sabit mesajla dönmeli: %d %s", kod, g)
	}
	// Uzak düzelince tamam: satır, dosya ve anlık görüntü gider.
	sahte.hata = nil
	if kod, g := advIstek(r, "DELETE", "/s/c_s4", ""); kod != 200 {
		t.Fatalf("%d %s", kod, g)
	}
	if varMi(y) || g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE dosya=?`, dosya) != 0 || g113_say(t, db, `SELECT COUNT(*) FROM backup_yetim_hedefler`) != 0 {
		t.Error("başarılı silmede dosya, satır ve anlık görüntü gitmeliydi")
	}

	// uzagi_yoksay=1: uzak ADIM ATLANIR ama parola yine tabloda kalmaz.
	d2 := "c_s5-20261001-000000.tar.gz"
	silinenDosya(t, kok, "c_s5", d2)
	kHesap(t, db, 7021, "s5.test", "c_s5", 0, "ftp.musteri.invalid", d2)
	kSil(t, db, 7021, "s5.test", "c_s5")
	once := sahte.say()
	if kod, g := advIstek(r, "DELETE", "/s/c_s5?uzagi_yoksay=1", ""); kod != 200 {
		t.Fatalf("%d %s", kod, g)
	}
	if sahte.say() != once {
		t.Error("uzagi_yoksay=1 uzak silmeyi ÇAĞIRMAMALI")
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backup_yetim_hedefler`) != 0 {
		t.Error("uzagi_yoksay=1 sonrası da anlık görüntü (parola) silinmeli")
	}
}

func TestS1_ParolaHicbirYanitaSizmaz(t *testing.T) {
	db := kDB(t)
	kok := silinenOrtam(t)
	r := kRouter(db)
	dosya := "c_s6-20261001-000000.tar.gz"
	silinenDosya(t, kok, "c_s6", dosya)
	hamParola := kHesap(t, db, 7030, "s6.test", "c_s6", 0, "ftp.musteri.invalid", dosya)
	kSil(t, db, 7030, "s6.test", "c_s6")
	logs := logYakala(t)
	kod, g := advIstek(r, "GET", "/s", "")
	if kod != 200 || !strings.Contains(g, "c_s6") {
		t.Fatalf("liste: %d %s", kod, g)
	}
	for _, yasak := range []string{kParola, hamParola, "ftpkul", "gos1:"} {
		if yasak != "" && strings.Contains(g, yasak) {
			t.Errorf("liste yanıtı %q sızdırıyor", yasak)
		}
	}
	if strings.Contains(logs.String(), kParola) || (gizli.SifreliMi(hamParola) && strings.Contains(logs.String(), hamParola)) {
		t.Error("log parola sızdırıyor")
	}
}

// Karar (koordinatör, 2026-10-08): uzak hedef anlık görüntüsü domaine HİÇBİR YOLLA geri yüklenmez — ne devralmada ne
// elle (onaylı/onaysız) bağlamada. Anlık görüntü yalnız yetimlerin uzak silmesi içindir; yetimler bağlanınca silinir.
func TestS1_Baglama_AnlikGoruntu_GeriYuklenmez_ParolaKalmaz(t *testing.T) {
	ctx := context.Background()
	hedefSay := func(t *testing.T, db *sql.DB, id int64) int {
		t.Helper()
		return g113_say(t, db, `SELECT COUNT(*) FROM backup_destinations WHERE domain_id=?`, id)
	}

	t.Run("devralma (bayrak): aynı alan+sahip olsa bile hedef GERİ YÜKLENMEZ, anlık görüntü (parola) silinir", func(t *testing.T) {
		db := kDB(t)
		silinenOrtam(t)
		kHesap(t, db, 7100, "ayni.test", "c_ayni", 5, "ftp.musteri.invalid", "c_ayni-20261001-000000.tar.gz")
		kSil(t, db, 7100, "ayni.test", "c_ayni")
		g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES (7101,'ayni.test','c_ayni',5)`)
		if n := devral(ctx, db, 7101); n != 1 {
			t.Fatalf("bağlanan: %d", n)
		}
		if hedefSay(t, db, 7101) != 0 {
			t.Error("eski müşterinin FTP hedefi (host/kullanıcı) yeni domaine geri yüklendi")
		}
		if g113_say(t, db, `SELECT COUNT(*) FROM backup_yetim_hedefler`) != 0 {
			t.Error("yetimler bağlanınca anlık görüntü (parola) silinmeli")
		}
	})

	t.Run("domainin kendi hedefi varsa dokunulmaz, anlık görüntü silinir", func(t *testing.T) {
		db := kDB(t)
		silinenOrtam(t)
		kHesap(t, db, 7110, "ayni.test", "c_ayni", 5, "ftp.eski.invalid", "c_ayni-20261001-000000.tar.gz")
		kSil(t, db, 7110, "ayni.test", "c_ayni")
		kHesap(t, db, 7111, "ayni.test", "c_ayni", 5, "ftp.yeni-kendi.invalid")
		if n := devral(ctx, db, 7111); n != 1 {
			t.Fatalf("bağlanan: %d", n)
		}
		var host string
		var aktif int
		if err := db.QueryRow(`SELECT host, aktif FROM backup_destinations WHERE domain_id=7111`).Scan(&host, &aktif); err != nil || host != "ftp.yeni-kendi.invalid" || aktif != 1 {
			t.Errorf("domainin kendi hedefi bozuldu: %q aktif=%d %v", host, aktif, err)
		}
		if g113_say(t, db, `SELECT COUNT(*) FROM backup_yetim_hedefler`) != 0 {
			t.Error("anlık görüntü silinmeli")
		}
	})

	t.Run("elle bağlama (aynı alan+sahip, onaysız): geri yüklenmez, anlık görüntü silinir", func(t *testing.T) {
		db := kDB(t)
		silinenOrtam(t)
		r := kRouter(db)
		kHesap(t, db, 7120, "ayni.test", "c_ayni", 5, "ftp.musteri.invalid", "c_ayni-20261001-000000.tar.gz")
		kSil(t, db, 7120, "ayni.test", "c_ayni")
		g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES (7121,'AYNI.test ','c_ayni',5)`) // büyük harf/boşluk: aynı alan adı sayılır
		kod, g := advIstek(r, "POST", "/s/c_ayni/bagla", `{"domain_id":7121}`)
		if kod != 200 || !strings.Contains(g, `"baglanan":1`) {
			t.Fatalf("%d %s", kod, g)
		}
		if hedefSay(t, db, 7121) != 0 {
			t.Error("elle bağlamada da hedef geri yüklenmemeli")
		}
		if g113_say(t, db, `SELECT COUNT(*) FROM backup_yetim_hedefler`) != 0 {
			t.Error("anlık görüntü silinmeli")
		}
	})

	t.Run("farklı sahibe zorla bağlama: hedef (kimlik bilgisi) VERİLMEZ, anlık görüntü silinir", func(t *testing.T) {
		db := kDB(t)
		silinenOrtam(t)
		r := kRouter(db)
		kHesap(t, db, 7130, "girgin.com", "c_girgin", 5, "ftp.musteri.invalid", "c_girgin-20261001-000000.tar.gz")
		kSil(t, db, 7130, "girgin.com", "c_girgin")
		g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES (7131,'girgin.net','c_girgin',9)`)
		if kod, g := advIstek(r, "POST", "/s/c_girgin/bagla", `{"domain_id":7131,"zorla":true}`); kod != 200 {
			t.Fatalf("%d %s", kod, g)
		}
		if hedefSay(t, db, 7131) != 0 {
			t.Error("başka müşterinin FTP kimlik bilgisi yeni sahibe VERİLDİ")
		}
		if g113_say(t, db, `SELECT COUNT(*) FROM backup_yetim_hedefler`) != 0 {
			t.Error("parola tabloda kalmamalı")
		}
	})

	t.Run("iki eski kimlik: hiç hedef geri yüklenmez, iki anlık görüntü de silinir", func(t *testing.T) {
		db := kDB(t)
		silinenOrtam(t)
		kHesap(t, db, 7140, "ayni.test", "c_ayni", 5, "ftp.a.invalid", "c_ayni-20261001-000000.tar.gz")
		kSil(t, db, 7140, "ayni.test", "c_ayni")
		kHesap(t, db, 7141, "ayni.test", "c_ayni", 5, "ftp.b.invalid", "c_ayni-20261002-000000.tar.gz")
		kSil(t, db, 7141, "ayni.test", "c_ayni")
		if g113_say(t, db, `SELECT COUNT(*) FROM backup_yetim_hedefler`) != 2 {
			t.Fatal("kurulum: iki silmenin iki ayrı anlık görüntüsü")
		}
		g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES (7142,'ayni.test','c_ayni',5)`)
		if n := devral(ctx, db, 7142); n != 2 {
			t.Fatalf("bağlanan: %d", n)
		}
		if hedefSay(t, db, 7142) != 0 {
			t.Error("hedef geri yüklenmemeli")
		}
		if g113_say(t, db, `SELECT COUNT(*) FROM backup_yetim_hedefler`) != 0 {
			t.Error("her iki anlık görüntü de silinmeli")
		}
	})

	t.Run("kısmi bağlama: yetimi kalan iz'in anlık görüntüsü KALIR, bitenin silinir", func(t *testing.T) {
		db := kDB(t)
		silinenOrtam(t)
		r := kRouter(db)
		kHesap(t, db, 7150, "a.test", "c_kismi", 5, "ftp.a.invalid", "c_kismi-20261001-000000.tar.gz")
		kSil(t, db, 7150, "a.test", "c_kismi")
		kHesap(t, db, 7151, "b.test", "c_kismi", 6, "ftp.b.invalid", "c_kismi-20261002-000000.tar.gz")
		kSil(t, db, 7151, "b.test", "c_kismi")
		g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES (7152,'a.test','c_kismi',5)`)
		// onaysız bağlama yalnız a.test/bayi 5'i bağlar (b.test için 409 beklenir ama admin ucu farklı sahipleri görünce zorla ister)
		if kod, _ := advIstek(r, "POST", "/s/c_kismi/bagla", `{"domain_id":7152}`); kod != 409 {
			t.Fatalf("karışık sahipli grupta onaysız bağlama 409 olmalı: %d", kod)
		}
		kosul, args := baglaKosulu("c_kismi", false, yetimSahibi("a.test", 5))
		if n, err := yetimleriBagla(ctx, db, 7152, kosul, args...); err != nil || n != 1 {
			t.Fatalf("yalnız a.test bağlanmalı: n=%d err=%v", n, err)
		}
		var kalan []string
		rows, _ := db.Query(`SELECT host FROM backup_yetim_hedefler ORDER BY host`)
		for rows.Next() {
			var h string
			_ = rows.Scan(&h)
			kalan = append(kalan, h)
		}
		rows.Close()
		if fmt.Sprint(kalan) != "[ftp.b.invalid]" {
			t.Errorf("yalnız yetimi kalan (b.test) iz'in anlık görüntüsü kalmalı: %v", kalan)
		}
	})

	t.Run("anlık görüntü tablosu bozukken bağlama yine başarılı", func(t *testing.T) {
		db := kDB(t)
		silinenOrtam(t)
		kHesap(t, db, 7160, "ayni.test", "c_ayni", 5, "ftp.musteri.invalid", "c_ayni-20261001-000000.tar.gz")
		kSil(t, db, 7160, "ayni.test", "c_ayni")
		g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES (7161,'ayni.test','c_ayni',5)`)
		g113_exec(t, db, `DROP TABLE backup_yetim_hedefler`)
		logs := logYakala(t)
		if n := devral(ctx, db, 7161); n != 1 {
			t.Fatalf("anlık görüntü tablosu bozukken de bağlama çalışmalı: %d", n)
		}
		if strings.Contains(logs.String(), kParola) {
			t.Error("log parola içeriyor")
		}
	})
}

func TestS1_YedekAltyapisiBozukkenIzYazilirVeSilmeEngellenmez(t *testing.T) {
	ctx := context.Background()
	logs := logYakala(t)

	// (a) anlık görüntü tablosu yok: satırlar yine yetim işaretlenir, panik/engel yok, parola loga girmez.
	db := kDB(t)
	kHesap(t, db, 7200, "a.test", "c_a", 0, "ftp.musteri.invalid", "c_a-20261001-000000.tar.gz")
	g113_exec(t, db, `DROP TABLE backup_yetim_hedefler`)
	kSil(t, db, 7200, "a.test", "c_a")
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=0 AND eski_domain_id=7200 AND silinme IS NOT NULL`) != 1 {
		t.Error("anlık görüntü yazılamasa da satırlar yetim işaretlenmeli")
	}
	if !strings.Contains(logs.String(), "anlık görüntüsü yazılamadı") {
		t.Error("anlık görüntü hatası 🔴 ile loglanmalı")
	}

	// (b) backup_destinations yok: hedefsiz devam.
	db2 := kDB(t)
	g113_exec(t, db2, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (7201,'b.test','c_b')`)
	g113_exec(t, db2, `INSERT INTO backups (domain_id, dosya) VALUES (7201,'c_b-20261001-000000.tar.gz')`)
	g113_exec(t, db2, `DROP TABLE backup_destinations`)
	kSil(t, db2, 7201, "b.test", "c_b")
	if g113_say(t, db2, `SELECT COUNT(*) FROM backups WHERE domain_id=0 AND eski_domain_id=7201`) != 1 {
		t.Error("hedef tablosu yokken de iz yazılmalı")
	}

	// (c) backups tablosu yok: Hazırla/Yaz panik etmez, hata döndürmez (HostingYik iz.Yaz sonucunu kullanmaz).
	db3 := kDB(t)
	g113_exec(t, db3, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (7202,'c.test','c_c')`)
	g113_exec(t, db3, `DROP TABLE backups`)
	iz := SilinenIzHazirla(ctx, db3, 7202, "c.test", "c_c")
	g113_exec(t, db3, `DELETE FROM domains WHERE id=7202`)
	iz.Yaz(ctx, db3) // dönmeli
	if strings.Contains(logs.String(), kParola) {
		t.Error("log parola içeriyor")
	}
}

func TestS1_ArtikAnlikGoruntuTemizligi(t *testing.T) {
	db := kDB(t)
	ctx := context.Background()
	ekle := func(iz string, eski int64, yas string) {
		g113_exec(t, db, `INSERT INTO backup_yetim_hedefler (iz_id, eski_domain_id, host, kullanici, parola, olusturma) VALUES (?, ?, 'h.invalid', 'k', 'p', `+yas+`)`, iz, eski)
	}
	ekle("iz-yetimi-yok", 7300, "NOW() - INTERVAL 2 HOUR") // yetimi yok → silinir
	ekle("iz-yeni", 7301, "NOW()")                         // yeni (UPDATE öncesi yazılmış olabilir) → kalır
	ekle("iz-yetimli", 7302, "NOW() - INTERVAL 2 HOUR")    // yetim satırı bu iz_id'yi taşıyor → kalır
	ekle("iz-sahipsiz", 7303, "NOW() - INTERVAL 2 HOUR")   // iz yazılamamış (domain_id>0, domain yok) satırı var → kalır
	ekle("iz-canli-id", 7304, "NOW() - INTERVAL 2 HOUR")   // eski kimliği şimdi CANLI bir domain kullanıyor (E7c) → silinir
	g113_exec(t, db, `INSERT INTO backups (domain_id, eski_domain_id, iz_id, dosya, silinme) VALUES (0,7302,'iz-yetimli','c_t-20261001-000000.tar.gz',NOW())`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya) VALUES (7303,'elle-yuklendi.tar.gz')`) // sk türetilemez → dönüştürülmez, ama anlık görüntüsü hâlâ gerekli
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (7304,'yeni-sahip.test','c_ys')`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya) VALUES (7304,'c_ys-20261001-000000.tar.gz')`) // yeni sahibin CANLI satırı eski anlık görüntüyü ayakta tutmamalı
	SilinenSupur(ctx, db)                                                                                  // 04:00 süpürmesi anlık görüntü temizliğini de yapar
	var kalan []string
	rows, _ := db.Query(`SELECT iz_id FROM backup_yetim_hedefler ORDER BY 1`)
	for rows.Next() {
		var e string
		_ = rows.Scan(&e)
		kalan = append(kalan, e)
	}
	rows.Close()
	if fmt.Sprint(kalan) != "[iz-sahipsiz iz-yeni iz-yetimli]" {
		t.Errorf("kalan anlık görüntüler [iz-sahipsiz iz-yeni iz-yetimli] olmalı: %v", kalan)
	}
}

// ---------------------------------------------------------------- S2

func TestS2_SahipsizIsaretle_TuretilemeyeneDokunmazIdempotent(t *testing.T) {
	db := kDB(t)
	kok := silinenOrtam(t)
	ctx := context.Background()
	logs := logYakala(t)
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (7403,'canli.test','c_canli')`)
	ekle := func(domainID int64, dosya string) int64 {
		r, err := db.Exec(`INSERT INTO backups (domain_id, dosya, boyut_b) VALUES (?,?,5)`, domainID, dosya)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := r.LastInsertId()
		return id
	}
	a1 := ekle(7401, "c_s2-20261001-000000.tar.gz")
	a2 := ekle(7401, "c_s2-20261002-000000-planli.tar.gz")
	garip := ekle(7402, "garip.tar.gz")
	canli := ekle(7403, "c_canli-20261001-000000.tar.gz")
	canliGarip := ekle(7403, "elle-yuklendi.tar.gz") // canlı domainin türetilemeyen adlı satırı: SAYILMAMALI
	// Eskiden alan adı/sahip doldurulmuş (göç yedekleme) satır: bu değerler KORUNUR.
	p, err := db.Exec(`INSERT INTO backups (domain_id, dosya, alan_adi, sistem_kullanici, reseller_id) VALUES (7404,'c_p-20261001-000000.tar.gz','onceden.test','c_p',3)`)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := p.LastInsertId()
	yetimZaten := silinenYetim(t, db, 7405, "z.test", "c_z", "c_z-20261001-000000.tar.gz", 0, "NULL")
	silinenDosya(t, kok, "c_s2", "c_s2-20261001-000000.tar.gz")

	m, tur := SilinenSahipsizIsaretle(ctx, db)
	if m != 3 || tur != 1 {
		t.Fatalf("işaretlenen=%d türetilemeyen=%d (3,1 beklenir)", m, tur)
	}
	for _, id := range []int64{a1, a2} {
		if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE id=? AND domain_id=0 AND eski_domain_id=7401 AND alan_adi='c_s2' AND sistem_kullanici='c_s2' AND reseller_id=0
			AND silinme IS NOT NULL AND DATEDIFF(son_kullanma, silinme)=7 AND LENGTH(iz_id)=32`, id) != 1 {
			t.Errorf("satır %d: yetim işaretlenmedi (alan_adi=sk, 7 gün, iz_id)", id)
		}
	}
	if g113_say(t, db, `SELECT COUNT(DISTINCT iz_id) FROM backups WHERE eski_domain_id=7401`) != 1 {
		t.Error("aynı eski kimliğin sahipsiz satırları TEK iz_id paylaşmalı")
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE id=? AND domain_id=0 AND eski_domain_id=7404 AND alan_adi='onceden.test' AND sistem_kullanici='c_p' AND reseller_id=3 AND silinme IS NOT NULL`, pid) != 1 {
		t.Error("önceden doldurulmuş alan adı/sahip KORUNMALI")
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE id=? AND domain_id=7402 AND silinme IS NULL AND eski_domain_id=0 AND sistem_kullanici=''`, garip) != 1 {
		t.Error("sk'sı türetilemeyen satıra DOKUNULMAMALI")
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE id IN (?,?) AND domain_id=7403 AND silinme IS NULL`, canli, canliGarip) != 2 {
		t.Error("domain'i CANLI olan satırlara dokunulmamalı")
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE id=? AND domain_id=0 AND eski_domain_id=7405`, yetimZaten) != 1 {
		t.Error("zaten yetim olan satır bozuldu")
	}
	if !strings.Contains(logs.String(), "1 satırın sk'sı dosya adından türetilemedi") {
		t.Errorf("türetilemeyen sayısı loglanmalı: %s", logs.String())
	}
	// İdempotent: ikinci tur yalnız türetilemeyeni sayar.
	if m, tur := SilinenSahipsizIsaretle(ctx, db); m != 0 || tur != 1 {
		t.Errorf("ikinci tur: işaretlenen=%d türetilemeyen=%d (0,1 beklenir)", m, tur)
	}
	// Normal yaşam döngüsüne girdi: listelenir, süresi dolunca süpürme siler.
	r := kRouter(db)
	if _, g := advIstek(r, "GET", "/s", ""); !strings.Contains(g, `"sk":"c_s2"`) {
		t.Errorf("işaretlenen satırlar yetim listesinde görünmeli: %s", g)
	}
	g113_exec(t, db, `UPDATE backups SET son_kullanma=DATE_SUB(NOW(), INTERVAL 1 HOUR) WHERE id=?`, a1)
	yetimUzakSahte(t)
	SilinenSupur(ctx, db)
	if satirVarMi(t, db, a1) || varMi(filepath.Join(kok, "c_s2", "c_s2-20261001-000000.tar.gz")) || !satirVarMi(t, db, a2) {
		t.Error("süresi dolan işaretlenmiş satır silinmeli, diğeri kalmalı")
	}
}

func TestS2_SahipsizIsaretle_AyarSureyiBelirler(t *testing.T) {
	for _, c := range []struct {
		ayar string
		gun  int // -1 = NULL
	}{{"", 7}, {"30", 30}, {"0", -1}, {"abc", 7}} {
		db := kDB(t)
		if c.ayar != "" {
			g113_exec(t, db, `INSERT INTO cp_ayarlar (anahtar, deger) VALUES ('silinen_yedek_gun', ?)`, c.ayar)
		}
		g113_exec(t, db, `INSERT INTO backups (domain_id, dosya) VALUES (7410,'c_g-20261001-000000.tar.gz')`)
		SilinenSahipsizIsaretle(context.Background(), db)
		var g *int
		if err := db.QueryRow(`SELECT DATEDIFF(son_kullanma, silinme) FROM backups WHERE eski_domain_id=7410`).Scan(&g); err != nil {
			t.Fatal(err)
		}
		if c.gun == -1 && g != nil || c.gun != -1 && (g == nil || *g != c.gun) {
			t.Errorf("ayar %q: %d gün beklenirdi, %v", c.ayar, c.gun, g)
		}
	}
}

func TestS2_IzUPDATEHatasi_TekYenidenDeneme(t *testing.T) {
	db := kDB(t)
	logs := logYakala(t)
	kHesap(t, db, 7420, "r.test", "c_r", 0, "", "c_r-20261001-000000.tar.gz")
	tetikKur(t, db, 1) // ilk UPDATE reddedilir, ikincisi geçer
	kSil(t, db, 7420, "r.test", "c_r")
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=0 AND eski_domain_id=7420 AND silinme IS NOT NULL`) != 1 {
		t.Fatalf("yeniden denemeyle iz yazılmalıydı: %s", logs.String())
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM tetik_sayac`) != 1 {
		t.Error("tam 1 başarısız deneme beklenirdi")
	}
	if !strings.Contains(logs.String(), "🔴 silinen yedek iz UPDATE hatası") || strings.Contains(logs.String(), "YAZILAMADI") {
		t.Errorf("ilk hata 🔴 ile loglanmalı, iz sonunda yazıldığı için YAZILAMADI olmamalı: %s", logs.String())
	}
}

func TestS2_TumDenemelerBasarisiz_LoglanirVeSahipsizTuruKurtarir(t *testing.T) {
	db := kDB(t)
	silinenOrtam(t)
	logs := logYakala(t)
	dosya := "c_q-20261001-000000.tar.gz"
	kHesap(t, db, 7430, "q.test", "c_q", 0, "ftp.musteri.invalid", dosya)
	tetikKur(t, db, 1000) // UPDATE hep reddedilir
	kSil(t, db, 7430, "q.test", "c_q")
	if n := g113_say(t, db, `SELECT COUNT(*) FROM tetik_sayac`); n != izDeneme {
		t.Errorf("tam %d deneme (izDeneme: 1 + yeniden denemeler) beklenirdi: %d", izDeneme, n)
	}
	if !strings.Contains(logs.String(), "🔴 silinen yedek iz YAZILAMADI") {
		t.Errorf("kalıcı başarısızlık 🔴 ile loglanmalı: %s", logs.String())
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=7430 AND silinme IS NULL`) != 1 {
		t.Fatal("kurulum: satır işaretsiz kalmalıydı")
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backup_yetim_hedefler WHERE eski_domain_id=7430`) != 1 {
		t.Fatal("anlık görüntü UPDATE'ten ÖNCE yazılmış olmalı")
	}
	// Süreç yeniden başlar / 04:00 turu: sahipsiz işaretleme satırı kurtarır; anlık görüntü hâlâ kullanılabilir.
	g113_exec(t, db, `DROP TRIGGER tetik_bu`)
	if m, _ := SilinenSahipsizIsaretle(context.Background(), db); m != 1 {
		t.Fatalf("sahipsiz satır işaretlenmeliydi: %d", m)
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=0 AND eski_domain_id=7430 AND silinme IS NOT NULL`) != 1 {
		t.Error("satır yetim olmalı")
	}
	// anlık görüntü UPDATE'ten önce yazılmıştı ve hiçbir satırın iz_id'si değildi: S2 onu eski_domain_id eşleşmesiyle SAHİPLENİR
	var izID string
	if err := db.QueryRow(`SELECT iz_id FROM backup_yetim_hedefler WHERE eski_domain_id=7430`).Scan(&izID); err != nil {
		t.Fatal(err)
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=0 AND eski_domain_id=7430 AND iz_id=?`, izID) != 1 {
		t.Error("sahipsiz satır, yazılamayan izin anlık görüntüsünü (iz_id) devralmalı")
	}
	if d, err := yetimHedefOku(context.Background(), db, izID); err != nil || d == nil || d.Parola != kParola {
		t.Errorf("kurtarılan yetim anlık görüntüsüne ulaşabilmeli: %+v %v", d, err)
	}
	yetimHedefArtiklariniTemizle(context.Background(), db)
	if g113_say(t, db, `SELECT COUNT(*) FROM backup_yetim_hedefler`) != 1 {
		t.Error("kullanımdaki anlık görüntü temizlenmemeli")
	}
}

// ---------------------------------------------------------------- S3

func TestS3_BaglananYedekler_KurtarmaTipi_PruneDokunmaz_ElleSilinebilir(t *testing.T) {
	db := kDB(t)
	kok := silinenOrtam(t)
	ctx := context.Background()
	r := kRouter(db)
	eski := []string{"c_kur-20260901-000000.tar.gz", "c_kur-20260902-000000.tar.gz", "c_kur-20260903-000000.tar.gz"}
	for _, d := range eski {
		silinenDosya(t, kok, "c_kur", d)
		silinenYetim(t, db, 7500, "kur.test", "c_kur", d, 5, "NULL")
	}
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES (7510,'kur.test','c_kur',5)`)
	if n := devral(ctx, db, 7510); n != 3 {
		t.Fatalf("bağlanan: %d", n)
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=7510 AND tip='kurtarma' AND eski_domain_id=0 AND silinme IS NULL AND son_kullanma IS NULL`) != 3 {
		t.Fatal("bağlanan satırlar tip='kurtarma' olmalı")
	}
	// Yeni hesabın kendi yedekleri: 3 oto (en yeni 1 kalır) ve 12 manuel (en yeni 10 kalır).
	for i := 1; i <= 3; i++ {
		d := fmt.Sprintf("c_kur-2026110%d-000000.tar.gz", i)
		silinenDosya(t, kok, "c_kur", d)
		g113_exec(t, db, `INSERT INTO backups (domain_id, tip, dosya) VALUES (7510,'oto',?)`, d)
	}
	for i := 1; i <= 12; i++ {
		d := fmt.Sprintf("c_kur-202612%02d-000000.tar.gz", i)
		silinenDosya(t, kok, "c_kur", d)
		g113_exec(t, db, `INSERT INTO backups (domain_id, tip, dosya) VALUES (7510,'tam',?)`, d)
	}
	if err := pruneOld(db, 7510, "c_kur", 1); err != nil {
		t.Fatal(err)
	}
	pruneManuelYedek(db, 7510, "c_kur")
	if n := g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=7510 AND tip='oto'`); n != 1 {
		t.Errorf("pruneOld kendi oto yedeklerini 1'e indirmeliydi: %d", n)
	}
	if n := g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=7510 AND tip='tam'`); n != 10 {
		t.Errorf("pruneManuelYedek 10'a indirmeliydi: %d", n)
	}
	if n := g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=7510 AND tip='kurtarma'`); n != 3 {
		t.Fatalf("🔴 bağlanan kurtarma yedekleri BUDANDI: %d kaldı", n)
	}
	for _, d := range eski {
		if !varMi(filepath.Join(kok, "c_kur", d)) {
			t.Errorf("kurtarma yedeğinin DOSYASI silindi: %s", d)
		}
	}
	// Kontrol: aynı satır tip='oto' olsaydı budanırdı (korumayı sağlayan şey tip).
	g113_exec(t, db, `UPDATE backups SET tip='oto' WHERE dosya=?`, eski[0])
	if err := pruneOld(db, 7510, "c_kur", 1); err != nil {
		t.Fatal(err)
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE dosya=?`, eski[0]) != 0 {
		t.Error("kontrol: tip='oto' olan eski yedek budanmalıydı (test korumayı tip üzerinden ölçmüyor)")
	}
	// Kullanıcı kurtarma yedeğini ELLE silebilir (normal Delete ucu; JOIN domains bulur).
	var bid int64
	_ = db.QueryRow(`SELECT id FROM backups WHERE dosya=?`, eski[1]).Scan(&bid)
	if kod, g := advIstek(r, "DELETE", fmt.Sprintf("/d/7510/backups/%d", bid), ""); kod != 200 {
		t.Fatalf("kurtarma yedeği elle silinemedi: %d %s", kod, g)
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE id=?`, bid) != 0 || varMi(filepath.Join(kok, "c_kur", eski[1])) {
		t.Error("elle silinen kurtarma yedeği (satır + dosya) gitmeliydi")
	}
	// Liste: kaynak 'kurtarma' (arayüz "Kurtarılan yedek" rozeti gösterir).
	_, g := advIstek(r, "GET", "/d/7510/backups", "")
	var liste []struct {
		Tip    string `json:"tip"`
		Kaynak string `json:"kaynak"`
		Dosya  string `json:"dosya"`
	}
	if err := json.Unmarshal([]byte(g), &liste); err != nil {
		t.Fatalf("liste: %v %s", err, g)
	}
	say := 0
	for _, y := range liste {
		if y.Dosya == eski[2] {
			say++
			if y.Tip != "kurtarma" || y.Kaynak != "kurtarma" {
				t.Errorf("kurtarma satırı tip/kaynak: %+v", y)
			}
		}
	}
	if say != 1 {
		t.Errorf("kurtarma yedeği listede olmalı: %s", g)
	}
}

func TestS3_ElleBaglama_KurtarmaTipi(t *testing.T) {
	db := kDB(t)
	r := kRouter(db)
	silinenYetim(t, db, 7520, "e.test", "c_e", "c_e-20260901-000000.tar.gz", 0, "NULL")
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (7521,'e.test','c_e')`)
	if kod, g := advIstek(r, "POST", "/s/c_e/bagla", `{"domain_id":7521}`); kod != 200 {
		t.Fatalf("%d %s", kod, g)
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=7521 AND tip='kurtarma' AND eski_domain_id=0 AND silinme IS NULL`) != 1 {
		t.Error("elle bağlanan satır da tip='kurtarma' olmalı")
	}
}

func TestS3_YedekKaynagi(t *testing.T) {
	if got := yedekKaynagi("kurtarma", sql.NullInt64{Int64: 5, Valid: true}); got != "kurtarma" {
		t.Errorf("job_id dolu olsa da kurtarma → \"kurtarma\": %q", got)
	}
	if got := yedekKaynagi("oto", sql.NullInt64{}); got != "sistem" {
		t.Errorf("oto → sistem: %q", got)
	}
	if got := yedekKaynagi("tam", sql.NullInt64{}); got != "manuel" {
		t.Errorf("tam → manuel: %q", got)
	}
}

// ---------------------------------------------------------------- S4

func TestS4_DomainIDYenidenKullanimi_EskiYetimlerYeniDomaineSizmaz(t *testing.T) {
	db := kDB(t)
	kok := silinenOrtam(t)
	r := kRouter(db)
	yetimUzakSahte(t)
	dA, dB := "c_girgin-20261001-000000.tar.gz", "c_girgin-20261002-000000.tar.gz"
	silinenDosya(t, kok, "c_girgin", dA)
	silinenDosya(t, kok, "c_girgin", dB)
	// Eski müşteri: girgin.com (id 7600, bayi 5) → silindi.
	kHesap(t, db, 7600, "girgin.com", "c_girgin", 5, "", dA, dB)
	kSil(t, db, 7600, "girgin.com", "c_girgin")
	var yetimIDs []int64
	rows, _ := db.Query(`SELECT id FROM backups WHERE eski_domain_id=7600 ORDER BY id`)
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		yetimIDs = append(yetimIDs, id)
	}
	rows.Close()
	if len(yetimIDs) != 2 || g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=7600`) != 0 {
		t.Fatalf("kurulum: 2 yetim, hiçbiri domain_id=7600 olmamalı: %v", yetimIDs)
	}
	// domain_id YENİDEN KULLANILDI: başka müşteri girgin.net (bayi 9) AYNI id ve (alan adının ilk etiketinden) AYNI sk.
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES (7600,'girgin.net','c_girgin',9)`)
	silinenDosya(t, kok, "c_girgin", "c_girgin-20261201-000000.tar.gz")
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya, tip) VALUES (7600,'c_girgin-20261201-000000.tar.gz','oto')`)

	// 1) Yedek listesi yalnız KENDİ yedeğini gösterir.
	_, g := advIstek(r, "GET", "/d/7600/backups", "")
	var liste []struct {
		Dosya string `json:"dosya"`
	}
	if err := json.Unmarshal([]byte(g), &liste); err != nil || len(liste) != 1 || liste[0].Dosya != "c_girgin-20261201-000000.tar.gz" {
		t.Fatalf("🔴 yeni domainin listesi eski müşterinin yedeğini içeriyor: %s", g)
	}
	// 2) Sil / geri yükle / içerik / indir: eski yetim id'leri 404.
	for _, bid := range yetimIDs {
		for _, c := range []struct{ m, yol string }{
			{"DELETE", "/d/7600/backups/%d"}, {"POST", "/d/7600/backups/%d/geriyukle"}, {"GET", "/d/7600/backups/%d/icerik"}, {"GET", "/d/7600/backups/%d/indir"},
		} {
			if kod, _ := advIstek(r, c.m, fmt.Sprintf(c.yol, bid), `{}`); kod != 404 {
				t.Errorf("🔴 %s %s: eski müşterinin yetim yedeği yeni domain üzerinden erişilebildi (kod %d)", c.m, c.yol, kod)
			}
		}
	}
	// 3) Budama: yeni domainin pruneOld/pruneManuelYedek'i eski yetimleri GÖRMEZ (hepsi tip='oto').
	for i := 1; i <= 3; i++ {
		d := fmt.Sprintf("c_girgin-2027010%d-000000.tar.gz", i)
		silinenDosya(t, kok, "c_girgin", d)
		g113_exec(t, db, `INSERT INTO backups (domain_id, tip, dosya) VALUES (7600,'oto',?)`, d)
	}
	for i := 1; i <= 12; i++ {
		g113_exec(t, db, `INSERT INTO backups (domain_id, tip, dosya) VALUES (7600,'tam',?)`, fmt.Sprintf("c_girgin-202702%02d-000000.tar.gz", i))
	}
	if err := pruneOld(db, 7600, "c_girgin", 1); err != nil {
		t.Fatal(err)
	}
	pruneManuelYedek(db, 7600, "c_girgin")
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE eski_domain_id=7600 AND domain_id=0 AND silinme IS NOT NULL`) != 2 || !varMi(filepath.Join(kok, "c_girgin", dA)) || !varMi(filepath.Join(kok, "c_girgin", dB)) {
		t.Fatal("🔴 yeni domainin budaması eski müşterinin yetim yedeklerini (satır/dosya) sildi")
	}
	// 4) Domain_id bazlı tüm sayımlar (kota vb.) yetimleri saymaz.
	if n := g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=7600 AND (silinme IS NOT NULL OR eski_domain_id<>0)`); n != 0 {
		t.Errorf("yeni domainin sayımına yetim girdi: %d", n)
	}
	// 5) Yetimler hâlâ yönetici listesinde; yeni domain farklı müşteri → UYARI bayrağı.
	_, g = advIstek(r, "GET", "/s", "")
	if !strings.Contains(g, `"alan_adi":"girgin.com"`) || !strings.Contains(g, `"baglanabilir_alan_adi":"girgin.net"`) || !strings.Contains(g, `"baglanabilir_farkli":true`) {
		t.Errorf("yetim listesi/uyarı bayrağı: %s", g)
	}
	// 6) Onaysız elle bağlama yapılamaz (farklı müşteri).
	if kod, _ := advIstek(r, "POST", "/s/c_girgin/bagla", `{"domain_id":7600}`); kod != 409 {
		t.Errorf("onaysız bağlama 409 olmalı: %d", kod)
	}
	// 7) Yetimler yönetici/süpürme tarafından hâlâ silinebilir.
	if kod, g := advIstek(r, "DELETE", "/s/c_girgin", ""); kod != 200 || !strings.Contains(g, `"silinen":2`) {
		t.Errorf("yetimler yönetici ucuyla silinebilmeli: %d %s", kod, g)
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=7600 AND tip='oto'`) != 1 {
		t.Error("yönetici silmesi yeni domainin yedeklerine dokunmamalı")
	}
}

// ---------------------------------------------------------------- elle bağlamada alan adı/sahip onayı

func TestBagla_AyniAlanVeSahip_OnaysizBaglanir_DenetimdeFarkYok(t *testing.T) {
	db := kDB(t)
	r := kRouter(db)
	silinenYetim(t, db, 7700, "Eski.Test", "c_e", "c_e-20260901-000000.tar.gz", 5, "NULL")
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES (7701,' eski.test','c_e',5)`)
	_, g := advIstek(r, "GET", "/s", "")
	if !strings.Contains(g, `"baglanabilir_farkli":false`) || !strings.Contains(g, `"baglanabilir_alan_adi":" eski.test"`) {
		t.Errorf("aynı alan adı (büyük/küçük harf, boşluk) + aynı sahip: uyarı olmamalı: %s", g)
	}
	if kod, g := advIstek(r, "POST", "/s/c_e/bagla", `{"domain_id":7701}`); kod != 200 || !strings.Contains(g, `"baglanan":1`) {
		t.Fatalf("%d %s", kod, g)
	}
	var detay string
	if err := db.QueryRow(`SELECT detail FROM audit_log WHERE action='silinen-yedek.bagla'`).Scan(&detay); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(detay, "onaylandı") {
		t.Errorf("farksız bağlamada onay notu olmamalı: %s", detay)
	}
}

func TestBagla_FarkliAlanAdi_VeFarkliReseller_409_SatirlarDegismez(t *testing.T) {
	db := kDB(t)
	r := kRouter(db)
	farkAlan := silinenYetim(t, db, 7710, "girgin.com", "c_g1", "c_g1-20260901-000000.tar.gz", 5, "NULL")
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES (7711,'girgin.net','c_g1',5)`) // alan farklı, sahip aynı
	farkSahip := silinenYetim(t, db, 7712, "ayni.test", "c_g2", "c_g2-20260901-000000.tar.gz", 5, "NULL")
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES (7713,'ayni.test','c_g2',6)`) // alan aynı, sahip farklı
	// Yönetici (reseller NULL=0) yedeği bayiye / bayi yedeği yöneticiye de FARKLI sahiptir.
	farkAdmin := silinenYetim(t, db, 7714, "ayni2.test", "c_g3", "c_g3-20260901-000000.tar.gz", 0, "NULL")
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES (7715,'ayni2.test','c_g3',8)`)

	for _, c := range []struct {
		ad, sk, govde string
		satir         int64
	}{
		{"farklı alan adı", "c_g1", `{"domain_id":7711}`, farkAlan},
		{"farklı reseller", "c_g2", `{"domain_id":7713}`, farkSahip},
		{"yönetici→bayi", "c_g3", `{"domain_id":7715}`, farkAdmin},
	} {
		kod, g := advIstek(r, "POST", "/s/"+c.sk+"/bagla", c.govde)
		if kod != 409 || !strings.Contains(g, `"alan_adi_farkli":true`) {
			t.Errorf("%s: 409 beklenirdi: %d %s", c.ad, kod, g)
		}
		if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE id=? AND domain_id=0 AND silinme IS NOT NULL AND tip='oto'`, c.satir) != 1 {
			t.Errorf("%s: 409 sonrası satır DEĞİŞTİ", c.ad)
		}
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM audit_log WHERE action='silinen-yedek.bagla'`) != 0 {
		t.Error("reddedilen bağlama denetime 'bağlandı' yazmamalı")
	}
}

func TestBagla_Zorla_BaglarVeDenetimeYazar(t *testing.T) {
	db := kDB(t)
	r := kRouter(db)
	y := silinenYetim(t, db, 7720, "girgin.com", "c_g4", "c_g4-20260901-000000.tar.gz", 5, "NULL")
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES (7721,'girgin.net','c_g4',9)`)
	kod, g := advIstek(r, "POST", "/s/c_g4/bagla", `{"domain_id":7721,"zorla":true}`)
	if kod != 200 || !strings.Contains(g, `"baglanan":1`) {
		t.Fatalf("zorla=true: %d %s", kod, g)
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE id=? AND domain_id=7721 AND tip='kurtarma'`, y) != 1 {
		t.Error("zorla ile bağlanmadı")
	}
	var detay, hedef string
	var kapsam int64
	if err := db.QueryRow(`SELECT detail, target, reseller_id FROM audit_log WHERE action='silinen-yedek.bagla'`).Scan(&detay, &hedef, &kapsam); err != nil {
		t.Fatalf("denetim kaydı yok: %v", err)
	}
	if !strings.Contains(detay, "alan adı/sahip farkı onaylandı") || !strings.Contains(detay, "girgin.com (bayi=5, 1 dosya)") || !strings.Contains(detay, "hedef=girgin.net (bayi=9)") {
		t.Errorf("denetim notu eksik: %s", detay)
	}
	if hedef != "girgin.net" {
		t.Errorf("denetim hedefi: %q", hedef)
	}
	if kapsam != 9 {
		t.Errorf("denetim kapsamı hedef hostingin sahibi bayi (users.id=9) olmalı, domain kimliği (7721) DEĞİL: %d", kapsam)
	}
}

func TestBagla_KarmaSahipler_HerhangiBiriFarkliysaOnayGerekir(t *testing.T) {
	db := kDB(t)
	r := kRouter(db)
	a := silinenYetim(t, db, 7730, "hedef.test", "c_g5", "c_g5-20260901-000000.tar.gz", 5, "NULL") // hedefle aynı
	b := silinenYetim(t, db, 7731, "baska.test", "c_g5", "c_g5-20260902-000000.tar.gz", 5, "NULL") // farklı müşteri, aynı sk
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES (7732,'hedef.test','c_g5',5)`)
	if kod, g := advIstek(r, "POST", "/s/c_g5/bagla", `{"domain_id":7732}`); kod != 409 || !strings.Contains(g, `"yedek_alan_adi":"baska.test"`) {
		t.Fatalf("karışık grupta farklı satır varsa 409: %d %s", kod, g)
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE id IN (?,?) AND domain_id=0`, a, b) != 2 {
		t.Fatal("409'da HİÇBİR satıra dokunulmamalı (aynı alan adlı olana bile)")
	}
	if kod, _ := advIstek(r, "POST", "/s/c_g5/bagla", `{"domain_id":7732,"zorla":true}`); kod != 200 {
		t.Fatal("zorla ile bağlanmalı")
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE id IN (?,?) AND domain_id=7732`, a, b) != 2 {
		t.Error("zorla ile ikisi de bağlanmalı")
	}
}

// ---------------------------------------------------------------- ek koruma testleri

// Aynı eski kimlik ikinci kez silinirse (kimlik yeniden kullanıldı) HER silme kendi iz_id'siyle KENDİ anlık görüntüsünü
// alır: eskisi ezilmez, yenisi eskisinin yetimlerine karışmaz (O4). Her sahibin yetimi kendi hedefiyle silinir.
func TestS1_AnlikGoruntu_AyniKimlikIkinciSilmede_IkiAyriIzIkiAyriGoruntu(t *testing.T) {
	db := kDB(t)
	silinenOrtam(t)
	kHesap(t, db, 7800, "x.test", "c_x", 0, "ftp.ilk.invalid", "c_x-20261001-000000.tar.gz")
	kSil(t, db, 7800, "x.test", "c_x")
	kHesap(t, db, 7800, "y.test", "c_y", 0, "ftp.ikinci.invalid", "c_y-20261001-000000.tar.gz")
	kSil(t, db, 7800, "y.test", "c_y")
	if g113_say(t, db, `SELECT COUNT(*) FROM backup_yetim_hedefler WHERE eski_domain_id=7800`) != 2 {
		t.Fatal("iki ayrı silmenin iki ayrı anlık görüntü satırı beklenirdi (eski_domain_id benzersiz DEĞİL)")
	}
	if g113_say(t, db, `SELECT COUNT(DISTINCT iz_id) FROM backup_yetim_hedefler`) != 2 {
		t.Error("iki anlık görüntü iki ayrı iz_id taşımalı")
	}
	for host, sk := range map[string]string{"ftp.ilk.invalid": "c_x", "ftp.ikinci.invalid": "c_y"} {
		if g113_say(t, db, `SELECT COUNT(*) FROM backups b JOIN backup_yetim_hedefler h ON h.iz_id=b.iz_id WHERE b.domain_id=0 AND b.sistem_kullanici=? AND h.host=?`, sk, host) != 1 {
			t.Errorf("%s yetimi %s anlık görüntüsüne bağlı olmalı (karışmamalı)", sk, host)
		}
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=0 AND eski_domain_id=7800`) != 2 {
		t.Error("iki sahibin yetimleri de yetim olmalı")
	}
}

// domain_id=0 ama silinme NULL (iz yarım): yetim DEĞİLDİR → listelenmez, indirilmez, silinmez, süpürülmez.
func TestYetim_DomainIDSifirAmaSilinmeNull_YetimSayilmaz(t *testing.T) {
	db := kDB(t)
	kok := silinenOrtam(t)
	r := kRouter(db)
	yetimUzakSahte(t)
	y := silinenDosya(t, kok, "c_n", "c_n-20261001-000000.tar.gz")
	res, err := db.Exec(`INSERT INTO backups (domain_id, dosya, sistem_kullanici, alan_adi, son_kullanma) VALUES (0,'c_n-20261001-000000.tar.gz','c_n','n.test',DATE_SUB(NOW(), INTERVAL 1 DAY))`)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	// İndirme ucu advRouter'da (kRouter'da yok).
	ar := advRouter(db, &Handlers{DB: db})
	if kod, _ := advIstek(ar, "GET", fmt.Sprintf("/s/yedek/%d/indir", id), ""); kod != 404 {
		t.Errorf("silinme NULL satır indirilmemeli: %d", kod)
	}
	if kod, _ := advIstek(r, "DELETE", "/s/c_n", ""); kod != 404 {
		t.Errorf("silinme NULL satır yönetici silmesine girmemeli: %d", kod)
	}
	if _, g := advIstek(r, "GET", "/s", ""); strings.Contains(g, "c_n") {
		t.Errorf("listelenmemeli: %s", g)
	}
	SilinenSupur(context.Background(), db)
	if !satirVarMi(t, db, id) || !varMi(y) {
		t.Error("süpürme silinme=NULL satırı silmemeli")
	}
}

// yetimYedegiSil: araya giren bağlama sonrası (artık yetim değil) canlı yedeği silmez.
func TestYetimYedegiSil_ArtikYetimDegilseSilmez(t *testing.T) {
	db := kDB(t)
	kok := silinenOrtam(t)
	sahte := yetimUzakSahte(t)
	dosya := "c_b-20261001-000000.tar.gz"
	y := silinenDosya(t, kok, "c_b", dosya)
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (7810,'b.test','c_b')`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya, tip) VALUES (7810,?, 'kurtarma')`, dosya)
	id := satirID(t, db, 7810)
	for _, yoksay := range []bool{false, true} {
		if err := yetimYedegiSil(context.Background(), db, id, "", "c_b", dosya, yoksay); err == nil {
			t.Errorf("yoksay=%v: canlı (bağlı) yedek yetim silmesiyle silinmemeli", yoksay)
		}
	}
	if !satirVarMi(t, db, id) || !varMi(y) || sahte.say() != 0 {
		t.Error("bağlı yedeğin satırı/dosyası KALMALI, uzak silme çağrılmamalı")
	}
}

// Onaysız bağlama koşulu satır düzeyinde de alan adı + sahibi uygular (kontrol ile bağlama arası araya girme).
func TestBaglaKosulu_OnaysizYolAlanVeSahibiUygular(t *testing.T) {
	db := kDB(t)
	ctx := context.Background()
	ayni := silinenYetim(t, db, 7820, "hedef.test", "c_k", "c_k-20261001-000000.tar.gz", 5, "NULL")
	farkliAlan := silinenYetim(t, db, 7821, "baska.test", "c_k", "c_k-20261002-000000.tar.gz", 5, "NULL")
	farkliSahip := silinenYetim(t, db, 7822, "hedef.test", "c_k", "c_k-20261003-000000.tar.gz", 6, "NULL")
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES (7823,'hedef.test','c_k',5)`)

	kosul, args := baglaKosulu("c_k", false, yetimSahibi("Hedef.Test ", 5))
	n, err := yetimleriBagla(ctx, db, 7823, kosul, args...)
	if err != nil || n != 1 {
		t.Fatalf("onaysız koşul yalnız aynı alan+sahibi bağlamalı: n=%d err=%v", n, err)
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE id=? AND domain_id=7823`, ayni) != 1 ||
		g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE id IN (?,?) AND domain_id=0`, farkliAlan, farkliSahip) != 2 {
		t.Error("onaysız koşul farklı müşterinin yedeğini bağladı")
	}
	kosul, args = baglaKosulu("c_k", true, yetimSahibi("hedef.test", 5))
	if n, err := yetimleriBagla(ctx, db, 7823, kosul, args...); err != nil || n != 2 {
		t.Errorf("onaylı (farkli=true) koşul sk'daki tüm yetimleri bağlamalı: n=%d err=%v", n, err)
	}
	if _, err := yetimleriBagla(ctx, db, 0, "sistem_kullanici=?", "c_k"); err == nil {
		t.Error("geçersiz domain reddedilmeli")
	}
}

// Plan ekranındaki "son sistem yedeği" kurtarma yedeğini (job_id taşısa bile) saymaz.
func TestS3_SonSistemYedek_KurtarmayiSaymaz(t *testing.T) {
	db := kDB(t)
	g113_exec(t, db, `ALTER TABLE domains ADD COLUMN backup_freq VARCHAR(16) NOT NULL DEFAULT 'none', ADD COLUMN backup_hour TINYINT NOT NULL DEFAULT 3, ADD COLUMN last_backup_at TIMESTAMP NULL DEFAULT NULL`)
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (7830,'p.test','c_p')`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, tip, dosya, created_at) VALUES (7830,'oto','c_p-20260601-100000.tar.gz','2026-06-01 10:00:00')`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, tip, dosya, created_at, job_id) VALUES (7830,'kurtarma','c_p-20260901-100000.tar.gz','2026-09-01 10:00:00',9)`)
	h := &Handlers{DB: db}
	db.SetMaxOpenConns(8)
	rt := chi.NewRouter()
	rt.Get("/d/{id}/backup-schedule", h.GetSchedule)
	kod, g := advIstek(rt, "GET", "/d/7830/backup-schedule", "")
	if kod != 200 || !strings.Contains(g, `"son_sistem_yedek":"2026-06-01 10:00"`) {
		t.Errorf("son sistem yedeği kurtarma satırını saymamalı: %d %s", kod, g)
	}
}

// Seçim ile güncelleme arasında domain oluşursa canlı satır yetim yapılmaz (UPDATE koşulu yeniden doğrular).
func TestS2_SahipsizSatiriIsaretle_CanliYadaIsaretliSatiriYapmaz(t *testing.T) {
	db := kDB(t)
	ctx := context.Background()
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (7900,'canli.test','c_cn')`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya) VALUES (7900,'c_cn-20261001-000000.tar.gz'),(7901,'c_cn-20261002-000000.tar.gz')`)
	canli := satirID(t, db, 7900)
	sahipsiz := satirID(t, db, 7901)
	if ok, err := sahipsizSatiriIsaretle(ctx, db, canli, "c_cn", "iz-1", 7); ok || err != nil {
		t.Errorf("🔴 canlı domainin satırı yetim yapıldı: %v %v", ok, err)
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE id=? AND domain_id=7900 AND silinme IS NULL`, canli) != 1 {
		t.Error("canlı satır değişti")
	}
	if ok, err := sahipsizSatiriIsaretle(ctx, db, sahipsiz, "c_cn", "iz-1", 7); !ok || err != nil {
		t.Fatalf("sahipsiz satır işaretlenmeli: %v %v", ok, err)
	}
	if ok, _ := sahipsizSatiriIsaretle(ctx, db, sahipsiz, "c_cn", "iz-1", 7); ok {
		t.Error("ikinci işaretleme (artık yetim, domain_id=0) bir şey yapmamalı")
	}
}
