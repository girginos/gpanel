package ajan

// akis_db_test.go — Telegram destek ajanı uçtan uca: geçici MariaDB (GOSP_TEST_GECICI_DSN
// yoksa atlanır), GERÇEK göç dosyası (migrations/0129, iki kez — idempotent), gerçek chi
// yönlendiricisi + RequireAuth, sahte merkez (WSS ve uzun yoklama) ve sahte araç yürütücüsü.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/auth"
	"girginospanel/internal/destek/baglam"
	"girginospanel/internal/destek/iptal"
	p "girginospanel/internal/destek/protokol"
	"girginospanel/internal/middleware"
)

var dtSecret = []byte("destek-ajan-test-jwt-sirri-0123456789-abcdef")

const dtKanarya = "KANARYA-PANEL-SIRRI-5f1e2d3c4b5a"

func dtGeciciDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li test atlandı")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("DSN: %v", err)
	}
	cfg.DBName = ""
	kok, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	ad := fmt.Sprintf("gosp_test_destek_%d", time.Now().UnixNano())
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
	t.Cleanup(func() { _ = db.Close() })
	for _, q := range []string{
		`CREATE TABLE users (id BIGINT UNSIGNED PRIMARY KEY, username VARCHAR(64) NOT NULL DEFAULT '',
			password_hash VARCHAR(255) NOT NULL DEFAULT '', role VARCHAR(16) NOT NULL DEFAULT 'user',
			status VARCHAR(16) NOT NULL DEFAULT 'active', token_gecersiz_ts BIGINT NOT NULL DEFAULT 0,
			last_activity_ts BIGINT NOT NULL DEFAULT 0, totp_secret VARCHAR(64) NOT NULL DEFAULT '',
			totp_enabled TINYINT(1) NOT NULL DEFAULT 0)`,
		`CREATE TABLE domains (id BIGINT UNSIGNED PRIMARY KEY, alan_adi VARCHAR(255) NOT NULL,
			ftp_user VARCHAR(64) NOT NULL DEFAULT '', askida TINYINT(1) NOT NULL DEFAULT 0, askida_kaynak VARCHAR(8) NOT NULL DEFAULT '',
			reseller_id BIGINT UNSIGNED NULL)`,
		`CREATE TABLE ftp_accounts (id BIGINT UNSIGNED PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL,
			username VARCHAR(64) NOT NULL, password_md5 VARCHAR(255) NOT NULL DEFAULT '',
			status ENUM('active','suspended') DEFAULT 'active', token_gecersiz_ts BIGINT NOT NULL DEFAULT 0)`,
		`CREATE TABLE audit_log (id BIGINT AUTO_INCREMENT PRIMARY KEY, ts TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			actor_user_id BIGINT UNSIGNED NULL, actor_username VARCHAR(64) NOT NULL DEFAULT '', ip VARCHAR(64) NULL,
			action VARCHAR(64) NOT NULL, target VARCHAR(255) NULL, detail JSON NULL CHECK (json_valid(detail)),
			ok TINYINT NOT NULL DEFAULT 1, reseller_id BIGINT NOT NULL DEFAULT 0, adina_domain_id BIGINT UNSIGNED NULL)`,
		`INSERT INTO users (id, username, role, password_hash) VALUES (1,'root','admin','kokhash'),
			(7,'bayi_a','reseller','bayihashA'), (8,'bayi_b','reseller','bayihashB')`,
		`INSERT INTO domains (id, alan_adi, ftp_user, reseller_id) VALUES (42,'a.test','amus',7), (43,'b.test','bmus',8),
			(44,'kok.test','kmus',NULL), (45,'a2.test','a2mus',7)`,
		`INSERT INTO ftp_accounts (id, domain_id, username, password_md5) VALUES (5,42,'amus','$6$aaa'), (6,43,'bmus','$6$bbb'),
			(7,44,'kmus','$6$kkk'), (8,45,'a2mus','$6$a22')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("şema: %v\n%s", err, q)
		}
	}
	var temiz []string
	for _, dosya := range []string{"0129_destek.sql", "0130_destek_ajan_ayri.sql"} {
		b, err := os.ReadFile("../../../migrations/" + dosya)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range strings.Split(string(b), "\n") {
			if ts := strings.TrimSpace(s); ts == "" || strings.HasPrefix(ts, "--") {
				continue
			}
			temiz = append(temiz, s)
		}
		temiz = append(temiz, ";")
	}
	for kez := 0; kez < 2; kez++ {
		for _, st := range strings.Split(strings.Join(temiz, "\n"), ";") {
			if st = strings.TrimSpace(st); st == "" {
				continue
			}
			if _, err := db.Exec(st); err != nil {
				t.Fatalf("göç 0129/0130 (tur %d): %v", kez+1, err)
			}
		}
	}
	middleware.Init(db)
	t.Cleanup(func() { middleware.Init(nil) })
	return db
}

// ---- sahte yürütücü ----

type cagri struct {
	ob   p.OturumBaglami
	emir p.IsEmri
	kap  baglam.Kapsam
	ok   bool
}

type sahteYurutucu struct {
	mu       sync.Mutex
	cagrilar []cagri
}

func (s *sahteYurutucu) Yurut(ctx context.Context, ob p.OturumBaglami, emir p.IsEmri) p.IsSonucu {
	k, ok := baglam.Al(ctx)
	s.mu.Lock()
	s.cagrilar = append(s.cagrilar, cagri{ob: ob, emir: emir, kap: k, ok: ok})
	s.mu.Unlock()
	return p.IsSonucu{IsKodu: emir.IsKodu, Ok: true, Ozet: "tamam " + dtKanarya,
		Satirlar: []string{"password=gizli123 giriş denemesi", "panel sırrı: " + dtKanarya, "normal satır"}}
}

func (s *sahteYurutucu) sayi() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.cagrilar)
}

func (s *sahteYurutucu) son() cagri {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cagrilar[len(s.cagrilar)-1]
}

// ---- kurulum ----

type ortam struct {
	t   *testing.T
	db  *sql.DB
	m   *sahteMerkez
	a   *Ajan
	y   *sahteYurutucu
	h   http.Handler
	kok string
	bA  string // bayi_a (7)
	bB  string // bayi_b (8)
	mA  string // müşteri A (domain 42, ftp 5, bayi 7)
	mB  string // müşteri B (domain 43, ftp 6, bayi 8)
	mA2 string // müşteri A2 (domain 45, ftp 8, bayi 7)
	mK  string // kök müşterisi (domain 44, ftp 7)

	kapatma string // yerel acil kapatma dosyasının (test) yolu

	etkin map[string]*p.BaglantiDurum // kök (v2) eşleştirmesinin etkin durumu: kimlik → merkeze giden durum
}

// kurOrtam: bagli=false → yerel acil kapatma dosyası ajan başlamadan ÖNCE oluşturulur (ajan
// merkeze bağlanmaz). destek_ayar satırı HİÇ yazılmaz: ajan şalter/tavan gerektirmez.
func kurOrtam(t *testing.T, yoklama bool, bagli bool) *ortam {
	t.Helper()
	db := dtGeciciDB(t)
	m := yeniSahteMerkez(t)
	eskiPI := lisansParmakIzi
	lisansParmakIzi = func() string { return strings.Repeat("ab", 32) }
	t.Cleanup(func() { lisansParmakIzi = eskiPI; iptal.Kanca = nil })
	a := Yeni(db, Yapilandirma{MerkezURL: m.srv.URL, MerkezAnahtarlari: []ed25519.PublicKey{m.cift.Acik},
		CihazAnahtarYolu: filepath.Join(t.TempDir(), "cihaz.key"), GuvensizIzin: true, YoklamaZorla: yoklama,
		BotKullaniciAdi: "GirginTestBot", LisansIspati: func() []byte { return []byte("test-ispat") }})
	a.BilinenSirlar = []string{dtKanarya}
	a.KapatmaKontrolAraligi = 200 * time.Millisecond
	kapatma := filepath.Join(t.TempDir(), "destek-kapali") // gerçek /etc yoluna ASLA dokunulmaz
	a.KapatmaDosyasi = kapatma
	a.DBYoklamaAraligi = 100 * time.Millisecond // panel ↔ ajan bağı DB yoklaması
	a.NabizAraligi = 200 * time.Millisecond
	a.PanelKancasiKur() // main.go'daki panel tarafı kanca (denetim); bildirim ajan yoklamasıyla
	y := &sahteYurutucu{}
	a.Yurutucu = y
	o := &ortam{t: t, db: db, m: m, a: a, y: y, etkin: map[string]*p.BaglantiDurum{}, kapatma: kapatma}
	// Panelde destek HTTP ucu YOK: yönlendirici boş (araç testleri kendi sahte panel rotalarını ekler).
	o.h = chi.NewRouter()
	o.kok = jwtAt(t, 1, "root", "admin", 0)
	o.bA = jwtAt(t, 7, "bayi_a", "reseller", 7)
	o.bB = jwtAt(t, 8, "bayi_b", "reseller", 8)
	o.mA = musteriJWT(t, 5, 42, "amus", "a.test", nil)
	o.mB = musteriJWT(t, 6, 43, "bmus", "b.test", nil)
	o.mA2 = musteriJWT(t, 8, 45, "a2mus", "a2.test", nil)
	o.mK = musteriJWT(t, 7, 44, "kmus", "kok.test", nil)
	if !bagli {
		if err := os.WriteFile(kapatma, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); time.Sleep(50 * time.Millisecond) })
	a.Baslat(ctx)
	if bagli {
		o.bagliBekle()
	}
	return o
}

func jwtAt(t *testing.T, uid int64, ad, rol string, rid int64) string {
	t.Helper()
	var tok string
	var err error
	if rol == "reseller" {
		tok, err = auth.IssueResellerAt(dtSecret, 3600, uid, ad, rid, 0, "")
	} else {
		tok, err = auth.IssueAt(dtSecret, 3600, uid, ad, rol, 0, "")
	}
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func musteriJWT(t *testing.T, ftp, dom int64, kul, alan string, adina *auth.MusteriClaims) string {
	t.Helper()
	c := auth.MusteriClaims{FTPHesapID: ftp, DomainID: dom, Kullanici: kul, AlanAdi: alan}
	if adina != nil {
		c.AktorUID, c.AktorAd, c.AktorRol, c.AktorRID = adina.AktorUID, adina.AktorAd, adina.AktorRol, adina.AktorRID
	}
	omur := int64(3600)
	if adina != nil {
		omur = auth.AdinaOmurSn
	}
	tok, _, err := auth.GenerateMusteri(dtSecret, c, omur)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func (o *ortam) bagliBekle() {
	o.t.Helper()
	son := time.Now().Add(10 * time.Second)
	for !o.a.Bagli() {
		if time.Now().After(son) {
			o.t.Fatalf("ajan merkeze bağlanmadı: %+v", o.a.DurumAl())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// kokEslestir: GERÇEK portal yolu — merkez imzalı eslesme_onayla_v2 gönderir; ajan bağlantıyı
// doğrudan etkin açar (kök). Etkin baglanti_durum'u beklenir; bağlantı kimliği döner.
func (o *ortam) kokEslestir(tgID int64, tgAd, duzey string) string {
	o.t.Helper()
	e := o.v2(tgID, tgAd, duzey, 7)
	o.m.imzala(e)
	o.m.gonder(e)
	bd := o.m.bekle(p.TipBaglantiDurum, func(g p.Govde) bool {
		d := g.(*p.BaglantiDurum)
		return d.KodOzet == e.KodOzet && d.Durum == p.BaglantiEtkin
	}).(*p.BaglantiDurum)
	o.etkin[bd.BaglantiID] = bd
	return bd.BaglantiID
}

// v2: imzalanMAMIŞ eslesme_onayla_v2 (hedef = bu ajanın oturumundaki sunucu kimliği; taze kod özeti).
func (o *ortam) v2(tgID int64, tgAd, duzey string, gun int) *p.EslesmeOnayla {
	o.t.Helper()
	ham := make([]byte, 16)
	_, _ = rand.Read(ham)
	oz := sha256.Sum256(ham)
	return &p.EslesmeOnayla{KodOzet: hex.EncodeToString(oz[:]), TgUserID: tgID, TgKullaniciAdi: tgAd, SunucuKimligi: o.a.DurumAl().SunucuKimligi,
		Duzey: p.Duzey(duzey), SureGun: gun, Onayli: true, PortalKullanici: "u_42", AydinlatmaSurum: "2026-10-04.1"}
}

// eskiKimlik: eski (bayi/müşteri) kayıt kimlikleri. Eşleştirme artık YALNIZ kök içindir; bu kayıtlar
// ajan/arac kapsam, iz ve süpürge davranışının (kapsam sızıntısı, otomatik iptal) hâlâ doğru
// çalıştığını doğrulamak için DOĞRUDAN veritabanına yazılır.
type eskiKimlik struct {
	rol                     p.Rol
	dom, ftp, rid, panelUID int64
}

func (o *ortam) eskiKimligi(tok string) (eskiKimlik, bool) {
	k := map[string]eskiKimlik{
		o.mA:  {p.RolMusteri, 42, 5, 7, 0},
		o.mB:  {p.RolMusteri, 43, 6, 8, 0},
		o.mA2: {p.RolMusteri, 45, 8, 7, 0},
		o.mK:  {p.RolMusteri, 44, 7, 0, 0},
		o.bA:  {p.RolBayi, 0, 0, 7, 7},
		o.bB:  {p.RolBayi, 0, 0, 8, 8},
	}
	v, ok := k[tok]
	return v, ok
}

// eslestir: kök → gerçek v2 yolu (etkin döner); bayi/müşteri → eski 'bekliyor' kaydı (onayla ile aktifleşir).
func (o *ortam) eslestir(tok string, tgID int64, tgAd, duzey string) string {
	o.t.Helper()
	if tok == o.kok {
		return o.kokEslestir(tgID, tgAd, duzey)
	}
	k, ok := o.eskiKimligi(tok)
	if !ok {
		o.t.Fatal("bilinmeyen kimlik")
	}
	kimlik, _ := yeniKimlik()
	ham := make([]byte, 16)
	_, _ = rand.Read(ham)
	oz := sha256.Sum256(ham)
	ozet := hex.EncodeToString(oz[:])
	var dom, ftp any
	if k.rol == p.RolMusteri {
		dom, ftp = k.dom, k.ftp
	}
	if _, err := o.db.Exec(`INSERT INTO destek_baglantilari (kimlik, kod_ozet, tg_user_id, tg_kullanici_adi, rol, domain_id, ftp_hesap_id,
		reseller_id, panel_user_id, duzey, sure_gun, durum, olusturan) VALUES (?,?,?,?,?,?,?,?,?,?,7,'bekliyor','test-eski')`,
		kimlik, ozet, tgID, tgAd, string(k.rol), dom, ftp, k.rid, k.panelUID, duzey); err != nil {
		o.t.Fatal(err)
	}
	b, err := baglantiKimlikle(context.Background(), o.db, kimlik)
	if err != nil {
		o.t.Fatal(err)
	}
	o.a.durumBildir(context.Background(), b)
	bd := o.m.bekle(p.TipBaglantiDurum, func(g p.Govde) bool {
		d := g.(*p.BaglantiDurum)
		return d.KodOzet == ozet && d.Durum == p.BaglantiOnayBekliyor
	}).(*p.BaglantiDurum)
	if bd.Duzey != p.DuzeyKapali || len(bd.Hesaplar) != 0 || bd.TgUserID != tgID {
		o.t.Fatalf("onay bekleyen bağlantı kapsam taşıyor: %+v", bd)
	}
	return bd.BaglantiID
}

// onayla: kök → zaten etkin (saklanan durum); eski kayıt → ikinci onayın eski etkisi doğrudan uygulanır.
func (o *ortam) onayla(tok, kimlik string) *p.BaglantiDurum {
	o.t.Helper()
	if tok == o.kok {
		bd := o.etkin[kimlik]
		if bd == nil {
			o.t.Fatal("kök bağlantısı etkin değil")
		}
		return bd
	}
	ctx := context.Background()
	b, err := baglantiKimlikle(ctx, o.db, kimlik)
	if err != nil {
		o.t.Fatal(err)
	}
	kd, err := kimlikIzi(ctx, o.db, b.Rol, b.DomainID, b.FTPHesapID, b.PanelUserID)
	if err != nil {
		o.t.Fatal(err)
	}
	if _, err := o.db.Exec(`UPDATE destek_baglantilari SET durum='aktif', onaylayan='test-eski', onay_at=CURRENT_TIMESTAMP,
		bitis=CURRENT_TIMESTAMP + INTERVAL sure_gun DAY, aydinlatma_surum='eski', aydinlatma_zamani=CURRENT_TIMESTAMP, durum_izi=?, iki_fa=?
		WHERE id=? AND durum='bekliyor'`, kd.iz, kd.ikiFA, b.ID); err != nil {
		o.t.Fatal(err)
	}
	yeni, _ := baglantiKimlikle(ctx, o.db, kimlik)
	o.a.durumBildir(ctx, yeni)
	return o.m.bekle(p.TipBaglantiDurum, func(g p.Govde) bool {
		d := g.(*p.BaglantiDurum)
		return d.BaglantiID == kimlik && d.Durum == p.BaglantiEtkin
	}).(*p.BaglantiDurum)
}

func (o *ortam) denetimSay(eylem, aktorOnek string) int {
	var n int
	_ = o.db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action=? AND actor_username LIKE ?`, eylem, aktorOnek+"%").Scan(&n)
	return n
}

func (o *ortam) durum(kimlik string) (string, string) {
	var d, n string
	_ = o.db.QueryRow(`SELECT durum, iptal_nedeni FROM destek_baglantilari WHERE kimlik=?`, kimlik).Scan(&d, &n)
	return d, n
}
