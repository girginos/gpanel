package ajan

// akis_db_test.go — Telegram destek ajanı uçtan uca: geçici MariaDB (GOSP_TEST_GECICI_DSN
// yoksa atlanır), GERÇEK göç dosyası (migrations/0129, iki kez — idempotent), gerçek chi
// yönlendiricisi + RequireAuth, sahte merkez (WSS ve uzun yoklama) ve sahte araç yürütücüsü.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	b, err := os.ReadFile("../../../migrations/0129_destek.sql")
	if err != nil {
		t.Fatal(err)
	}
	var temiz []string
	for _, s := range strings.Split(string(b), "\n") {
		if ts := strings.TrimSpace(s); ts == "" || strings.HasPrefix(ts, "--") {
			continue
		}
		temiz = append(temiz, s)
	}
	for kez := 0; kez < 2; kez++ {
		for _, st := range strings.Split(strings.Join(temiz, "\n"), ";") {
			if st = strings.TrimSpace(st); st == "" {
				continue
			}
			if _, err := db.Exec(st); err != nil {
				t.Fatalf("göç 0129 (tur %d): %v", kez+1, err)
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
}

func kurOrtam(t *testing.T, yoklama bool, modulAcik bool) *ortam {
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
	a.ModulKontrolAraligi = 200 * time.Millisecond
	y := &sahteYurutucu{}
	a.Yurutucu = y
	o := &ortam{t: t, db: db, m: m, a: a, y: y}
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth(dtSecret))
			a.Rotalar(r)
		})
	})
	o.h = r
	o.kok = jwtAt(t, 1, "root", "admin", 0)
	o.bA = jwtAt(t, 7, "bayi_a", "reseller", 7)
	o.bB = jwtAt(t, 8, "bayi_b", "reseller", 8)
	o.mA = musteriJWT(t, 5, 42, "amus", "a.test", nil)
	o.mB = musteriJWT(t, 6, 43, "bmus", "b.test", nil)
	o.mA2 = musteriJWT(t, 8, 45, "a2mus", "a2.test", nil)
	o.mK = musteriJWT(t, 7, 44, "kmus", "kok.test", nil)
	if modulAcik {
		o.istekOK("PUT", "/destek/ayar", o.kok, map[string]any{"modul_duzey": "tam", "bayi_duzey": "tam", "musteri_duzey": "tam"})
		o.istekOK("PUT", "/destek/ayar", o.bA, map[string]any{"musteri_duzey": "tam"})
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); time.Sleep(50 * time.Millisecond) })
	a.Baslat(ctx)
	if modulAcik {
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

type yanit struct {
	kod   int
	govde map[string]any
	ham   string
}

func (o *ortam) istek(metod, yol, tok string, govde any) yanit {
	o.t.Helper()
	var b []byte
	if govde != nil {
		b, _ = json.Marshal(govde)
	}
	req := httptest.NewRequest(metod, "/api/v1"+yol, bytes.NewReader(b))
	req.RemoteAddr = "192.0.2.10:40000"
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	o.h.ServeHTTP(w, req)
	y := yanit{kod: w.Code, ham: w.Body.String()}
	_ = json.Unmarshal(w.Body.Bytes(), &y.govde)
	return y
}

func (o *ortam) istekOK(metod, yol, tok string, govde any) yanit {
	o.t.Helper()
	y := o.istek(metod, yol, tok, govde)
	if y.kod != 200 {
		o.t.Fatalf("%s %s: %d %s", metod, yol, y.kod, y.ham)
	}
	return y
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

// eslestir: tok ile kod üretir, merkez imzalı eslesme_onayla gönderir; bekleyen bağlantı kimliği döner.
func (o *ortam) eslestir(tok string, tgID int64, tgAd, duzey string) string {
	o.t.Helper()
	y := o.istekOK("POST", "/destek/eslestir", tok, map[string]any{"duzey": duzey, "sure_gun": 7})
	kod := y.govde["kod"].(string)
	oz, _ := p.EslesmeKoduOzet(kod)
	o.m.bekle(p.TipKodOzet, func(g p.Govde) bool { return g.(*p.KodOzet).KodOzet == oz })
	e := &p.EslesmeOnayla{KodOzet: oz, TgUserID: tgID, TgKullaniciAdi: tgAd}
	o.m.imzala(e)
	o.m.gonder(e)
	bd := o.m.bekle(p.TipBaglantiDurum, func(g p.Govde) bool {
		d := g.(*p.BaglantiDurum)
		return d.KodOzet == oz && d.Durum == p.BaglantiOnayBekliyor
	}).(*p.BaglantiDurum)
	if bd.Duzey != p.DuzeyKapali || len(bd.Hesaplar) != 0 || bd.TgUserID != tgID {
		o.t.Fatalf("onay bekleyen bağlantı kapsam taşıyor: %+v", bd)
	}
	return bd.BaglantiID
}

func (o *ortam) onayla(tok, kimlik string) *p.BaglantiDurum {
	o.t.Helper()
	o.istekOK("POST", "/destek/baglantilar/"+kimlik+"/onayla", tok, map[string]any{"aydinlatma_surum": aydinlatmaSurum, "kabul": true})
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
