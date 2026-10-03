package e2e

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/auth"
	"girginospanel/internal/destek/ajan"
	"girginospanel/internal/destek/arac"
	"girginospanel/internal/destek/iptal"
	"girginospanel/internal/destek/merkez"
	"girginospanel/internal/destek/protokol"
	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
	"girginospanel/internal/monitor"
)

// Sahte sırlar ve kanaryalar çalışma zamanında parçalardan kurulur (gizli tarayıcı
// gerçek sır sanmasın).
var (
	jwtSirri   = []byte("destek-e2e-jwt-sirri-0123456789-abcdef")
	tgToken    = "123456789" + ":" + strings.Repeat("B", 35)
	anthropic  = "sk-" + "ant-api03-E2ETESTANAHTARI-0123456789abcdef"
	webhookGiz = "e2e-webhook-gizli-0123456789"

	// kanaryaLog: TEST HESABININ günlüğündeki gizli değer (DB_PASSWORD=<kanarya>).
	kanaryaLog = "Kanarya" + "E2E" + "log" + "7f3a9c1d5b"
	// panelSirri: panel sırrı (JWT/DSN gibi) — çıktıda birebir maskelenmeli.
	panelSirri = "Kanarya" + "E2E" + "panel" + "c0ffee1234"
)

const (
	modelHaiku  = "claude-haiku-4-5-20251001"
	modelSonnet = "claude-sonnet-5-5"
	modelOpus   = "claude-opus-5-5"
)

// ---- geçici veritabanı ----

func geciciDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li uçtan uca test atlandı")
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
	ad := fmt.Sprintf("gosp_test_destek_e2e_%d", time.Now().UnixNano())
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
	// Ajan (destek_baglantilari, kimlik izi) + araç yürütücüsü (kapsam) + middleware
	// (destek kimliği) şemalarının birleşimi.
	for _, q := range []string{
		`CREATE TABLE users (id BIGINT UNSIGNED PRIMARY KEY, username VARCHAR(64) NOT NULL DEFAULT '',
			password_hash VARCHAR(255) NOT NULL DEFAULT '', role VARCHAR(16) NOT NULL DEFAULT 'user',
			status VARCHAR(16) NOT NULL DEFAULT 'active', token_gecersiz_ts BIGINT NOT NULL DEFAULT 0,
			last_activity_ts BIGINT NOT NULL DEFAULT 0, totp_secret VARCHAR(64) NOT NULL DEFAULT '',
			totp_enabled TINYINT(1) NOT NULL DEFAULT 0)`,
		`CREATE TABLE domains (id BIGINT UNSIGNED PRIMARY KEY, alan_adi VARCHAR(253) NOT NULL,
			ftp_user VARCHAR(64) NOT NULL DEFAULT '', sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '',
			askida TINYINT(1) NOT NULL DEFAULT 0, askida_kaynak VARCHAR(8) NOT NULL DEFAULT '',
			reseller_id BIGINT NOT NULL DEFAULT 0, is_demo TINYINT(1) NOT NULL DEFAULT 0)`,
		`CREATE TABLE ftp_accounts (id BIGINT UNSIGNED PRIMARY KEY, domain_id BIGINT UNSIGNED NOT NULL,
			username VARCHAR(64) NOT NULL, password_md5 VARCHAR(255) NOT NULL DEFAULT '',
			status ENUM('active','suspended') DEFAULT 'active', token_gecersiz_ts BIGINT NOT NULL DEFAULT 0)`,
		`CREATE TABLE audit_log (id BIGINT AUTO_INCREMENT PRIMARY KEY, ts TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			actor_user_id BIGINT UNSIGNED NULL, actor_username VARCHAR(64) NOT NULL DEFAULT '', ip VARCHAR(64) NULL,
			action VARCHAR(64) NOT NULL, target VARCHAR(255) NULL, detail JSON NULL CHECK (json_valid(detail)),
			ok TINYINT NOT NULL DEFAULT 1, reseller_id BIGINT NOT NULL DEFAULT 0, adina_domain_id BIGINT UNSIGNED NULL)`,
		`CREATE TABLE nginx_settings (domain_id BIGINT UNSIGNED PRIMARY KEY, ek_direktifler TEXT NULL)`,
		`CREATE TABLE cp_laravel_apps (domain_id BIGINT UNSIGNED PRIMARY KEY, app_root VARCHAR(255) NOT NULL DEFAULT '')`,
		`INSERT INTO users (id, username, role, password_hash) VALUES (1,'root','admin','kokhash'),
			(7,'bayi_a','reseller','bayihashA'), (8,'bayi_b','reseller','bayihashB')`,
		`INSERT INTO domains (id, alan_adi, ftp_user, sistem_kullanici, reseller_id) VALUES
			(42,'a.test','amus','c_a_test',7), (43,'b.test','bmus','c_b_test',8), (45,'a2.test','a2mus','c_a2_test',7)`,
		`INSERT INTO ftp_accounts (id, domain_id, username, password_md5) VALUES (5,42,'amus','$6$aaa'),
			(6,43,'bmus','$6$bbb'), (8,45,'a2mus','$6$a22')`,
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
	for _, st := range strings.Split(strings.Join(temiz, "\n"), ";") {
		if st = strings.TrimSpace(st); st != "" {
			if _, err := db.Exec(st); err != nil {
				t.Fatalf("göç 0129: %v", err)
			}
		}
	}
	middleware.Init(db)
	t.Cleanup(func() { middleware.Init(nil) })
	return db
}

// ---- sahte Telegram Bot API sunucusu ----

type tgCagri struct {
	Metot string
	Chat  int64
	Mesaj int64
	Metin string
	Veri  []string // satır içi klavye callback_data
}

type sahteTGAPI struct {
	mu       sync.Mutex
	cagrilar []tgCagri
	id       int64
	srv      *httptest.Server
}

func yeniSahteTGAPI(t *testing.T) *sahteTGAPI {
	s := &sahteTGAPI{}
	s.srv = httptest.NewServer(http.HandlerFunc(s.isle))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *sahteTGAPI) isle(w http.ResponseWriter, r *http.Request) {
	on := "/bot" + tgToken + "/"
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, on) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"ok":false,"error_code":404,"description":"Not Found"}`)
		return
	}
	metot := strings.TrimPrefix(r.URL.Path, on)
	var g struct {
		ChatID      int64  `json:"chat_id"`
		MessageID   int64  `json:"message_id"`
		Text        string `json:"text"`
		ReplyMarkup *struct {
			InlineKeyboard [][]struct {
				CallbackData string `json:"callback_data"`
			} `json:"inline_keyboard"`
		} `json:"reply_markup"`
	}
	_ = json.NewDecoder(r.Body).Decode(&g)
	c := tgCagri{Metot: metot, Chat: g.ChatID, Mesaj: g.MessageID, Metin: g.Text}
	if g.ReplyMarkup != nil {
		for _, satir := range g.ReplyMarkup.InlineKeyboard {
			for _, d := range satir {
				c.Veri = append(c.Veri, d.CallbackData)
			}
		}
	}
	var sonuc any = true
	s.mu.Lock()
	if metot == "sendMessage" {
		s.id++
		c.Mesaj = s.id
		sonuc = map[string]any{"message_id": s.id}
	}
	s.cagrilar = append(s.cagrilar, c)
	s.mu.Unlock()
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": sonuc})
}

func (s *sahteTGAPI) hepsi() []tgCagri {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]tgCagri(nil), s.cagrilar...)
}

// metinler: sohbete giden (sendMessage + editMessageText) metinler, sırayla.
func (s *sahteTGAPI) metinler(chat int64) []string {
	var out []string
	for _, c := range s.hepsi() {
		if c.Chat == chat && (c.Metot == "sendMessage" || c.Metot == "editMessageText") {
			out = append(out, c.Metin)
		}
	}
	return out
}

func (s *sahteTGAPI) tumMetin() string {
	var b strings.Builder
	for _, c := range s.hepsi() {
		b.WriteString(c.Metin)
		b.WriteByte('\n')
	}
	return b.String()
}

func (s *sahteTGAPI) say(chat int64, parca string) int {
	n := 0
	for _, m := range s.metinler(chat) {
		if strings.Contains(m, parca) {
			n++
		}
	}
	return n
}

// bekle: sohbete parca'yı içeren bir metin gidene dek (10 sn).
func (s *sahteTGAPI) bekle(t *testing.T, chat int64, parca string) string {
	t.Helper()
	son := time.Now().Add(10 * time.Second)
	for time.Now().Before(son) {
		for _, m := range s.metinler(chat) {
			if strings.Contains(m, parca) {
				return m
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("Telegram'a %q içeren metin gitmedi; giden: %q", parca, s.metinler(chat))
	return ""
}

func (s *sahteTGAPI) dugmeVerisi(chat int64, onek string) string {
	for _, c := range s.hepsi() {
		if c.Chat != chat {
			continue
		}
		for _, v := range c.Veri {
			if strings.HasPrefix(v, onek) {
				return v
			}
		}
	}
	return ""
}

// ---- sahte Claude ----

type sahteClaude struct {
	mu       sync.Mutex
	govdeler [][]byte
	anaSay   int
	ana      func(n int, govde []byte) *merkez.ClaudeYanit
}

func (s *sahteClaude) Gonder(_ context.Context, ist merkez.ClaudeIstek) (*merkez.ClaudeYanit, error) {
	s.mu.Lock()
	s.govdeler = append(s.govdeler, bytes.Clone(ist.Govde))
	switch {
	case bytes.Contains(ist.Govde, []byte(`"tool_choice":{"type":"tool","name":"siniflandir"}`)):
		s.mu.Unlock()
		return yanit("tool_use", aracBlok("tu_triyaj", "siniflandir", `{"niyet":"destek","dil":"tr"}`)), nil
	case bytes.Contains(ist.Govde, []byte(`"tool_choice":{"type":"tool","name":"denetim"}`)):
		s.mu.Unlock()
		return yanit("tool_use", aracBlok("tu_denetim", "denetim", `{"guvenli":true,"neden":"yok"}`)), nil
	}
	if ist.Model != modelSonnet && ist.Model != modelOpus {
		s.mu.Unlock()
		return nil, fmt.Errorf("beklenmeyen model %q", ist.Model)
	}
	n := s.anaSay
	s.anaSay++
	f := s.ana
	s.mu.Unlock()
	if f == nil {
		return yanit("end_turn", metinBlok("tamam")), nil
	}
	return f(n, ist.Govde), nil
}

func (s *sahteClaude) anaSayisi() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.anaSay
}

func (s *sahteClaude) senaryo(f func(n int, govde []byte) *merkez.ClaudeYanit) {
	s.mu.Lock()
	s.ana, s.anaSay = f, 0
	s.mu.Unlock()
}

// tumGovde: merkezin Claude'a gönderdiği HER şey (JSON kaçışları çözülmüş metin + ham).
func (s *sahteClaude) tumGovde() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var b strings.Builder
	for _, g := range s.govdeler {
		b.Write(g)
		b.WriteByte('\n')
		b.WriteString(govdeMetni(g))
		b.WriteByte('\n')
	}
	return b.String()
}

func jsonDizge(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func metinBlok(s string) string { return `{"type":"text","text":` + jsonDizge(s) + `}` }
func aracBlok(id, ad, girdi string) string {
	return `{"type":"tool_use","id":"` + id + `","name":"` + ad + `","input":` + girdi + `}`
}

func yanit(stop string, bloklar ...string) *merkez.ClaudeYanit {
	y := &merkez.ClaudeYanit{ID: "msg_e2e", StopReason: stop, Usage: merkez.Kullanim{InputTokens: 100, OutputTokens: 50, CacheReadInputTokens: 4000}}
	for _, b := range bloklar {
		y.Content = append(y.Content, json.RawMessage(b))
	}
	return y
}

type apiGovde struct {
	Messages []struct {
		Role    string            `json:"role"`
		Content []json.RawMessage `json:"content"`
	} `json:"messages"`
}

type apiBlok struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Content json.RawMessage `json:"content"`
}

func blokMetni(b apiBlok) string {
	if b.Type == "text" {
		return b.Text
	}
	var s string
	if json.Unmarshal(b.Content, &s) == nil {
		return s
	}
	var dizi []apiBlok
	_ = json.Unmarshal(b.Content, &dizi)
	var out []string
	for _, x := range dizi {
		out = append(out, x.Text)
	}
	return strings.Join(out, "\n")
}

// govdeMetni: istekteki tüm mesaj metinleri (oturum bloğu, kullanıcı metni, araç sonuçları).
func govdeMetni(govde []byte) string {
	var g apiGovde
	_ = json.Unmarshal(govde, &g)
	var out []string
	for _, m := range g.Messages {
		for _, ham := range m.Content {
			var b apiBlok
			if json.Unmarshal(ham, &b) == nil {
				out = append(out, blokMetni(b))
			}
		}
	}
	return strings.Join(out, "\n")
}

// sonAracSonuclari: son user mesajındaki tool_result metinleri.
func sonAracSonuclari(govde []byte) []string {
	var g apiGovde
	_ = json.Unmarshal(govde, &g)
	if len(g.Messages) == 0 {
		return nil
	}
	var out []string
	for _, ham := range g.Messages[len(g.Messages)-1].Content {
		var b apiBlok
		if json.Unmarshal(ham, &b) == nil && b.Type == "tool_result" {
			out = append(out, blokMetni(b))
		}
	}
	return out
}

// ---- ortam ----

type ortam struct {
	t       *testing.T
	db      *sql.DB
	m       *merkez.Merkez
	depo    *merkez.DosyaDepo
	depoDiz string
	hub     *httptest.Server
	ic      *httptest.Server
	tg      *sahteTGAPI
	c       *sahteClaude
	cift    *protokol.AnahtarCifti
	yur     arac.Yurutucu
	yoklama bool

	amu       sync.Mutex
	a         *ajan.Ajan
	panel     http.Handler
	ajanIptal context.CancelFunc
	cihazYolu string

	upd atomic.Int64

	kmu      sync.Mutex
	yoklanan []string // site_yokla hedefleri (gerçek yürütücünün yokla dikişi)
	logAlan  []string // /domains/{id}/logs/oku isteklerinin domain id'leri

	kok, bA, mA, mB string
}

func testYap() *merkez.Yapilandirma {
	f := merkez.Fiyat{GirisMTok: 3, CikisMTok: 15, CacheYazma5dkMTok: 3.75, CacheYazma1saMTok: 6, CacheOkumaMTok: 0.3}
	y := &merkez.Yapilandirma{
		Modeller:           merkez.Modeller{Triyaj: modelHaiku, Ana: modelSonnet, Yukseltme: modelOpus},
		Fiyatlar:           map[string]merkez.Fiyat{modelHaiku: f, modelSonnet: f, modelOpus: f},
		Butce:              merkez.Butce{OturumTokenTavani: 1_000_000, SunucuGunlukUSD: 100, SunucuAylikUSD: 1000, GenelGunlukUSD: 100, GenelAylikUSD: 1000},
		SunucuOturumSiniri: 10,
	}
	y.Varsayilanlar()
	return y
}

// kur: merkez + sahte dış dünya + DB + gerçek yürütücü; ajan anahtarlar ile başlatılır
// (nil → merkezin doğru açık anahtarı).
func kur(t *testing.T, yoklama bool, anahtarlar []ed25519.PublicKey) *ortam {
	t.Helper()
	o := &ortam{t: t, db: geciciDB(t), tg: yeniSahteTGAPI(t), c: &sahteClaude{}, yoklama: yoklama}
	var err error
	if o.cift, err = protokol.AnahtarCiftiUret(nil); err != nil {
		t.Fatal(err)
	}
	o.depoDiz = t.TempDir()
	if o.depo, err = merkez.DosyaDepoAc(o.depoDiz); err != nil {
		t.Fatal(err)
	}
	logSeviye := slog.LevelError
	if os.Getenv("DESTEK_E2E_LOG") != "" {
		logSeviye = slog.LevelDebug
	}
	o.m, err = merkez.Yeni(merkez.Secenekler{Yap: testYap(),
		Sirlar: &merkez.Sirlar{TelegramToken: tgToken, AnthropicAnahtar: anthropic, WebhookGizli: webhookGiz, MerkezAnahtar: o.cift},
		Depo:   o.depo, Claude: o.c, Telegram: merkez.YeniTelegramHTTP(tgToken, o.tg.srv.URL),
		Log: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logSeviye}))})
	if err != nil {
		t.Fatal(err)
	}
	o.hub = httptest.NewServer(o.m.Handler())
	o.ic = httptest.NewServer(o.m.IcHandler())
	t.Cleanup(func() {
		o.ajanDurdur()
		o.hub.CloseClientConnections()
		o.hub.Close()
		o.ic.Close()
		o.m.Bekle()
		iptal.Kanca = nil
	})
	o.yur = arac.YeniDikisli(o.db, o.yurutucuRouter(), []string{panelSirri}, arac.Dikisler{
		Yokla: func(u string) monitor.DomainHealth {
			o.kmu.Lock()
			o.yoklanan = append(o.yoklanan, u)
			o.kmu.Unlock()
			return monitor.DomainHealth{URL: u, DurumKodu: 502, Erisilebilir: true, YanitSuresiMs: 31, Server: "nginx", Sema: "https"}
		},
		Komut: func(context.Context, ...string) ([]byte, error) { return nil, fmt.Errorf("e2e: kök komutu yok") },
		KiraciKomut: func(context.Context, string, ...string) ([]byte, error) {
			return nil, fmt.Errorf("e2e: kiracı komutu yok")
		},
		Dig: func(context.Context, string, string, string) ([]string, error) {
			return nil, fmt.Errorf("e2e: dns yok")
		},
	})
	o.kok = jwtAt(t, 1, "root", "admin")
	o.bA = jwtAt(t, 7, "bayi_a", "reseller")
	o.mA = musteriJWT(t, 5, 42, "amus", "a.test")
	o.mB = musteriJWT(t, 6, 43, "bmus", "b.test")
	o.cihazYolu = filepath.Join(t.TempDir(), "cihaz.key")

	o.ajanBaslat(anahtarlar, func() {
		o.istekOK("PUT", "/destek/ayar", o.kok, map[string]any{"modul_duzey": "tam", "bayi_duzey": "tam", "musteri_duzey": "tam"})
		o.istekOK("PUT", "/destek/ayar", o.bA, map[string]any{"musteri_duzey": "tam"})
		o.istekOK("PUT", "/destek/ayar", jwtAt(t, 8, "bayi_b", "reseller"), map[string]any{"musteri_duzey": "tam"})
	})
	return o
}

// yurutucuRouter: panelin süreç içi yönlendiricisinin destek rotaları (gerçek
// RequireAuth + MusteriScope; destek kimliği context işaretiyle gelir).
func (o *ortam) yurutucuRouter() http.Handler {
	loglar := map[string][]string{
		"42": {
			"2026/10/04 10:00:01 [error] 812#0: *1 upstream timed out (110: Connection timed out) while connecting to upstream php-fpm, client 203.0.113.9",
			"PHP Fatal error: SQLSTATE[HY000] [1045] erişim reddedildi; DB_PASSWORD=" + kanaryaLog + " (a.test/.env)",
			"panel ayar dökümü: " + panelSirri,
		},
		"43": {"b.test KOMSU-GUNLUK-SATIRI"},
		"45": {"a2.test günlük satırı"},
	}
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth(jwtSirri))
			r.With(middleware.MusteriScope).Get("/domains/{id}/logs/oku", func(w http.ResponseWriter, r *http.Request) {
				id := chi.URLParam(r, "id")
				o.kmu.Lock()
				o.logAlan = append(o.logAlan, id)
				o.kmu.Unlock()
				httpx.WriteJSON(w, http.StatusOK, map[string]any{"dosya": r.URL.Query().Get("dosya"), "satirlar": loglar[id]})
			})
		})
	})
	return r
}

// ajanBaslat: yeni ajan örneği (aynı DB + aynı cihaz anahtarı → aynı sunucu kimliği).
// once: Baslat'tan önce panel uçlarıyla yapılacak hazırlık (ayar).
func (o *ortam) ajanBaslat(anahtarlar []ed25519.PublicKey, once func()) {
	o.t.Helper()
	if anahtarlar == nil {
		anahtarlar = []ed25519.PublicKey{o.cift.Acik}
	}
	a := ajan.Yeni(o.db, ajan.Yapilandirma{MerkezURL: o.hub.URL, MerkezAnahtarlari: anahtarlar,
		BotKullaniciAdi: "GirginTestBot", CihazAnahtarYolu: o.cihazYolu, GuvensizIzin: true, YoklamaZorla: o.yoklama,
		// Faz 1: lisans bileti yoksa ajan açık işaret gönderir; merkez Faz 1'de kabul eder.
		LisansIspati:    func() []byte { return []byte("bilet-yok") },
		LisansParmakIzi: func() string { return strings.Repeat("cd", 32) }})
	a.BilinenSirlar = []string{panelSirri}
	a.ModulKontrolAraligi = 200 * time.Millisecond
	a.YurutucuAyarla(o.yur)
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth(jwtSirri))
			a.Rotalar(r)
		})
	})
	ctx, iptalF := context.WithCancel(context.Background())
	o.amu.Lock()
	o.a, o.panel, o.ajanIptal = a, r, iptalF
	o.amu.Unlock()
	if once != nil {
		once()
	}
	a.Baslat(ctx)
}

func (o *ortam) ajanDurdur() {
	o.amu.Lock()
	f, a := o.ajanIptal, o.a
	o.ajanIptal = nil
	o.amu.Unlock()
	if f == nil {
		return
	}
	f()
	son := time.Now().Add(5 * time.Second)
	for a.Bagli() && time.Now().Before(son) {
		time.Sleep(10 * time.Millisecond)
	}
}

func (o *ortam) ajan() *ajan.Ajan {
	o.amu.Lock()
	defer o.amu.Unlock()
	return o.a
}

func (o *ortam) bagliBekle() {
	o.t.Helper()
	o.kosulBekle("ajan merkeze bağlandı", 15*time.Second, func() bool { return o.ajan().Bagli() })
}

func (o *ortam) kosulBekle(ad string, sure time.Duration, f func() bool) {
	o.t.Helper()
	son := time.Now().Add(sure)
	for time.Now().Before(son) {
		if f() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	o.t.Fatalf("koşul zaman aşımı: %s", ad)
}

func jwtAt(t *testing.T, uid int64, ad, rol string) string {
	t.Helper()
	var tok string
	var err error
	if rol == "reseller" {
		tok, err = auth.IssueResellerAt(jwtSirri, 3600, uid, ad, uid, 0, "")
	} else {
		tok, err = auth.IssueAt(jwtSirri, 3600, uid, ad, rol, 0, "")
	}
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func musteriJWT(t *testing.T, ftp, dom int64, kul, alan string) string {
	t.Helper()
	tok, _, err := auth.GenerateMusteri(jwtSirri, auth.MusteriClaims{FTPHesapID: ftp, DomainID: dom, Kullanici: kul, AlanAdi: alan}, 3600)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

type yanitP struct {
	kod   int
	govde map[string]any
	ham   string
}

// istek: panel uçları (gerçek RequireAuth + ajan.Rotalar).
func (o *ortam) istek(metod, yol, tok string, govde any) yanitP {
	o.t.Helper()
	var b []byte
	if govde != nil {
		b, _ = json.Marshal(govde)
	}
	req := httptest.NewRequest(metod, "/api/v1"+yol, bytes.NewReader(b))
	req.RemoteAddr = "192.0.2.10:40000"
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	o.amu.Lock()
	h := o.panel
	o.amu.Unlock()
	h.ServeHTTP(w, req)
	y := yanitP{kod: w.Code, ham: w.Body.String()}
	_ = json.Unmarshal(w.Body.Bytes(), &y.govde)
	return y
}

func (o *ortam) istekOK(metod, yol, tok string, govde any) yanitP {
	o.t.Helper()
	y := o.istek(metod, yol, tok, govde)
	if y.kod != http.StatusOK {
		o.t.Fatalf("%s %s: %d %s", metod, yol, y.kod, y.ham)
	}
	return y
}

// metrik: merkezin iç /metrik ucu (yalnız sayılar).
func (o *ortam) metrik() map[string]any {
	o.t.Helper()
	resp, err := http.Get(o.ic.URL + "/metrik")
	if err != nil {
		o.t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		o.t.Fatal(err)
	}
	return m
}

func (o *ortam) metrikSayi(ad string) int {
	v, _ := o.metrik()[ad].(float64)
	return int(v)
}

func (o *ortam) sayac(ad string) int {
	s, _ := o.metrik()["sayac"].(map[string]any)
	v, _ := s[ad].(float64)
	return int(v)
}

// ---- Telegram tarafı (webhook) ----

func (o *ortam) webhook(u map[string]any) {
	o.t.Helper()
	u["update_id"] = o.upd.Add(1)
	b, _ := json.Marshal(u)
	req, _ := http.NewRequest(http.MethodPost, o.hub.URL+protokol.TelegramWebhookYolu, bytes.NewReader(b))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", webhookGiz)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		o.t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		o.t.Fatalf("webhook: HTTP %d", resp.StatusCode)
	}
	o.m.Bekle()
}

func (o *ortam) mesaj(tg int64, ad, txt string) {
	o.t.Helper()
	o.webhook(map[string]any{"message": map[string]any{
		"message_id": 1, "text": txt,
		"from": map[string]any{"id": tg, "is_bot": false, "username": ad},
		"chat": map[string]any{"id": tg, "type": "private"},
	}})
}

func (o *ortam) callback(tg int64, veri string) {
	o.t.Helper()
	o.webhook(map[string]any{"callback_query": map[string]any{
		"id": "cb" + fmt.Sprint(o.upd.Load()), "data": veri, "from": map[string]any{"id": tg, "is_bot": false},
		"message": map[string]any{"message_id": 5, "chat": map[string]any{"id": tg, "type": "private"}},
	}})
}

// ---- panel tarafı ----

type panelBaglanti struct {
	Kimlik         string `json:"kimlik"`
	Durum          string `json:"durum"`
	TgKullaniciAdi string `json:"tg_kullanici_adi"`
	IptalNedeni    string `json:"iptal_nedeni"`
}

func (o *ortam) panelBaglantilari(tok string) []panelBaglanti {
	o.t.Helper()
	y := o.istekOK("GET", "/destek/baglantilar", tok, nil)
	var g struct {
		Baglantilar []panelBaglanti `json:"baglantilar"`
	}
	_ = json.Unmarshal([]byte(y.ham), &g)
	return g.Baglantilar
}

// dbDurum: ajan DB'sindeki bağlantı durumu + iptal nedeni.
func (o *ortam) dbDurum(kimlik string) (string, string) {
	var d, n string
	_ = o.db.QueryRow(`SELECT durum, COALESCE(iptal_nedeni,'') FROM destek_baglantilari WHERE kimlik=?`, kimlik).Scan(&d, &n)
	return d, n
}

func (o *ortam) merkezBaglantisi(tg int64) (merkez.Baglanti, bool) {
	bs := o.depo.KullaniciBaglantilari(tg)
	if len(bs) == 0 {
		return merkez.Baglanti{}, false
	}
	return bs[0], true
}

// eslestir — senaryo (a): panelde kod → Telegram /start → panelde ikinci onay → KVKK → etkin.
// ayrintili: onay ve KVKK öncesi mesajların Claude'a GİTMEDİĞİ de sınanır.
func (o *ortam) eslestir(tok string, tg int64, ad, duzey string, ayrintili bool) string {
	o.t.Helper()
	onceki := o.metrikSayi("bekleyen_kod")
	y := o.istekOK("POST", "/destek/eslestir", tok, map[string]any{"duzey": duzey, "sure_gun": 7})
	kod, _ := y.govde["kod"].(string)
	gorunen, _ := y.govde["gorunen"].(string)
	if y.govde["merkez_bagli"] != true || kod == "" || !strings.Contains(fmt.Sprint(y.govde["baglanti_url"]), "start="+kod) {
		o.t.Fatalf("eşleştirme yanıtı: %s", y.ham)
	}
	o.kosulBekle("merkez kod özetini aldı", 10*time.Second, func() bool { return o.metrikSayi("bekleyen_kod") > onceki })
	// WSS'de kullanıcı paneldeki "ABCD-EFGH-…" biçimini küçük harfle yazar; yoklamada
	// derin bağlantının ham kodu gelir. İkisi de protokol.EslesmeKoduOzet ile aynı özet.
	start := kod
	if !o.yoklama {
		start = strings.ToLower(gorunen)
	}
	o.mesaj(tg, ad, "/start "+start)
	o.tg.bekle(o.t, tg, "panelde onay")

	var kimlik string
	o.kosulBekle("panelde onay bekleyen bağlantı", 10*time.Second, func() bool {
		for _, b := range o.panelBaglantilari(tok) {
			if b.Durum == "bekliyor" && b.TgKullaniciAdi == ad {
				kimlik = b.Kimlik
				return true
			}
		}
		return false
	})
	o.kosulBekle("merkez onay bekleyen bağlantıyı bildi", 10*time.Second, func() bool {
		b, ok := o.merkezBaglantisi(tg)
		return ok && b.BaglantiID == kimlik && b.Durum == protokol.BaglantiOnayBekliyor
	})
	if b, _ := o.merkezBaglantisi(tg); b.Duzey != protokol.DuzeyKapali || len(b.Hesaplar) != 0 {
		o.t.Fatalf("onay bekleyen bağlantı kapsam taşıyor: %+v", b)
	}
	cagri := o.c.anaSayisi()
	if ayrintili {
		o.mesaj(tg, ad, "sitem açılmıyor")
		o.tg.bekle(o.t, tg, "panel onayı bekliyor")
	}
	o.istekOK("POST", "/destek/baglantilar/"+kimlik+"/onayla", tok,
		map[string]any{"aydinlatma_surum": ajan.AydinlatmaMetni().Surum, "kabul": true})
	o.tg.bekle(o.t, tg, "Bağlantı etkin")
	veri := o.tg.dugmeVerisi(tg, "kvkk:")
	if veri == "" {
		o.t.Fatal("KVKK düğmesi yok")
	}
	if ayrintili {
		o.mesaj(tg, ad, "sitem açılmıyor")
		o.tg.bekle(o.t, tg, "aydınlatma metnini kabul")
	}
	if o.c.anaSayisi() != cagri {
		o.t.Fatal("onay/KVKK öncesi mesaj Claude'a gitti")
	}
	o.callback(tg, veri)
	o.tg.bekle(o.t, tg, "Sorununuzu yazabilirsiniz")
	b, ok := o.merkezBaglantisi(tg)
	if !ok || b.Durum != protokol.BaglantiEtkin || b.KVKKKabul.IsZero() || b.BaglantiID != kimlik {
		o.t.Fatalf("merkez bağlantısı etkin değil: %+v", b)
	}
	return kimlik
}

func (o *ortam) yoklananlar() []string {
	o.kmu.Lock()
	defer o.kmu.Unlock()
	return append([]string(nil), o.yoklanan...)
}

func (o *ortam) logAlanlari() []string {
	o.kmu.Lock()
	defer o.kmu.Unlock()
	return append([]string(nil), o.logAlan...)
}

// depoIcerigi: merkezin diskteki TÜM kaydı (durum.json + konuşma kayıtları).
func (o *ortam) depoIcerigi() string {
	var b strings.Builder
	_ = filepath.Walk(o.depoDiz, func(yol string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			v, _ := os.ReadFile(yol)
			b.Write(v)
			b.WriteByte('\n')
		}
		return nil
	})
	return b.String()
}

func (o *ortam) islemSonuclari(kimlik string) []string {
	rows, err := o.db.Query(`SELECT i.arac, i.sonuc FROM destek_islemleri i JOIN destek_baglantilari b ON b.id=i.baglanti_id
		WHERE b.kimlik=? ORDER BY i.id`, kimlik)
	if err != nil {
		o.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a, s string
		_ = rows.Scan(&a, &s)
		out = append(out, a+"="+s)
	}
	return out
}

// baglantiKisa: merkezin callback verisindeki bağlantı kısaltması (bot.go ile aynı kural).
func baglantiKisa(id string) string {
	h := sha256.Sum256([]byte(id))
	return hex.EncodeToString(h[:8])
}
