package arac

// arac_db_test.go — yürütücü uçtan uca: geçici MariaDB (GOSP_TEST_GECICI_DSN yoksa
// atlanır) + main.go ile aynı biçimde kurulmuş chi yönlendiricisi (Route("/api/v1")
// + Group(RequireAuth) + gerçek MusteriScope/AdminOnly) + sahte işleyiciler (yanıtlarına
// kanarya sırlar gömülü) + sahte komut ortamı.
//
// Kanıtlanan: kapsam (müşteri A → B reddi, bayi → başka bayinin hesabı reddi,
// kök-only araçlar M/B'ye yetki_yok), ikinci katman (yürütücü atlatılsa bile
// middleware reddeder), şema dışı girdi reddi, maske (kanarya çıkmaz), satır/bayt
// sınırı, süzgecin maskeden sonra uygulanması, özel araçların kiracı süzgeçleri,
// denetim kapsamı, zaman aşımı.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/auth"
	"girginospanel/internal/destek/protokol"
	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
	"girginospanel/internal/monitor"
)

const (
	panelSirri    = "PanelJWT-Kanarya-Sirri-0123456789"
	kanaryaEnv    = "KanaryaSir123456"
	kanaryaFTP    = "DuzKanarya-ftp-777"
	kanaryaEposta = "kanarya.kisi@ornek.com"
	kanaryaIP     = "203.0.113.77"
)

// kanaryaJWT — JWT biçimli sahte değer; gizli tarayıcı (gitleaks) gerçek sır
// sanmasın diye parçalardan kurulur.
var kanaryaJWT = "ey" + "JhbGciOiJIUzI1NiJ9" + "." + "ey" + "JzdWIiOiJrYW5hcnlhIn0" + "." + "c2lnbmF0dXJla2FuYXJ5YQ"

var testSecret = []byte("destek-arac-test-jwt-sirri-0123456789")

type sahne struct {
	db  *sql.DB
	y   *yurutucu
	mu  sync.Mutex
	son struct {
		c  *auth.Claims
		mc *auth.MusteriClaims
	}
	logSatirlari []string
	komutlar     [][]string
	kiraci       [][]string
	yoklanan     []string
	nginxCikti   string
	komutBekle   bool
}

func (s *sahne) kaydet(r *http.Request) {
	s.mu.Lock()
	s.son.c, s.son.mc = middleware.ClaimsFrom(r), middleware.MusteriClaimsFrom(r)
	s.mu.Unlock()
}

func geciciDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li test atlandı")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = ""
	cfg.ParseTime = false
	kok, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	ad := fmt.Sprintf("gosp_test_destekarac_%d", time.Now().UnixNano())
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
		`CREATE TABLE users (id BIGINT UNSIGNED PRIMARY KEY, username VARCHAR(64) NOT NULL,
			role ENUM('admin','reseller','user') NOT NULL DEFAULT 'user',
			status ENUM('active','suspended') NOT NULL DEFAULT 'active',
			token_gecersiz_ts BIGINT NOT NULL DEFAULT 0)`,
		`CREATE TABLE domains (id BIGINT UNSIGNED PRIMARY KEY, alan_adi VARCHAR(253) NOT NULL,
			sistem_kullanici VARCHAR(64) NOT NULL, reseller_id BIGINT NOT NULL DEFAULT 0,
			askida TINYINT(1) NOT NULL DEFAULT 0, is_demo TINYINT(1) NOT NULL DEFAULT 0)`,
		`CREATE TABLE audit_log (id BIGINT AUTO_INCREMENT PRIMARY KEY, ts TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			actor_user_id BIGINT UNSIGNED NULL, actor_username VARCHAR(64) NOT NULL DEFAULT '', ip VARCHAR(64) NULL,
			action VARCHAR(64) NOT NULL, target VARCHAR(255) NULL, detail LONGTEXT NULL,
			ok TINYINT NOT NULL DEFAULT 1, reseller_id BIGINT NOT NULL DEFAULT 0, adina_domain_id BIGINT UNSIGNED NULL)`,
		`CREATE TABLE nginx_settings (domain_id BIGINT UNSIGNED PRIMARY KEY, ek_direktifler TEXT NULL)`,
		`CREATE TABLE cp_laravel_apps (domain_id BIGINT UNSIGNED PRIMARY KEY, app_root VARCHAR(255) NOT NULL DEFAULT '')`,
		`INSERT INTO users (id, username, role, status) VALUES
			(1,'admin','admin','active'),(10,'bayi10','reseller','active'),
			(11,'bayi11','reseller','active'),(12,'bayi12','reseller','suspended')`,
		`INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id, askida) VALUES
			(101,'a.com','c_a_com',10,0),(102,'b.com','c_b_com',11,0),
			(103,'c.com','c_c_com',0,0),(104,'d.com','c_d_com',0,1)`,
		`INSERT INTO cp_laravel_apps (domain_id, app_root) VALUES (101,'laravel_app'),(103,'../../etc')`,
		`INSERT INTO nginx_settings (domain_id, ek_direktifler) VALUES (101,'add_header X-Kendi "deger1";')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("şema: %v\n%s", err, q)
		}
	}
	return db
}

func sahneKur(t *testing.T) *sahne {
	t.Helper()
	db := geciciDB(t)
	middleware.Init(db)
	t.Cleanup(func() { middleware.Init(nil) })
	s := &sahne{db: db}
	alanAdi := func(r *http.Request) string {
		var a string
		_ = db.QueryRow(`SELECT alan_adi FROM domains WHERE id=?`, chi.URLParam(r, "id")).Scan(&a)
		return a
	}
	jsonYaz := func(w http.ResponseWriter, v any) { httpx.WriteJSON(w, http.StatusOK, v) }
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth(testSecret))
			r.With(middleware.MusteriScope).Get("/domains/{id}", func(w http.ResponseWriter, r *http.Request) {
				s.kaydet(r)
				jsonYaz(w, map[string]any{"alan_adi": alanAdi(r), "php_surum": "8.3", "durum": "aktif", "askida": false,
					"plan_ad": "Paket DB_PASSWORD=" + kanaryaEnv + " " + panelSirri, "ftp_user": "u", "notlar": kanaryaFTP})
			})
			r.With(middleware.MusteriScope).Get("/domains/{id}/kaynak", func(w http.ResponseWriter, r *http.Request) {
				s.kaydet(r)
				jsonYaz(w, map[string]any{"plan_adi": "Gold", "disk_mb": map[string]any{"kullanim": 10, "limit": 100},
					"inode_kullanim": 5, "inode_limit": 0})
			})
			r.With(middleware.MusteriScope).Get("/domains/{id}/ssl", func(w http.ResponseWriter, r *http.Request) {
				s.kaydet(r)
				jsonYaz(w, map[string]any{"aktif": true, "ftp_parola": kanaryaFTP, "api_token": kanaryaEnv,
					"not": "iletisim " + kanaryaEposta + " sunucu " + kanaryaIP + " jwt " + kanaryaJWT})
			})
			r.With(middleware.MusteriScope).Get("/domains/{id}/logs/oku", func(w http.ResponseWriter, r *http.Request) {
				s.kaydet(r)
				s.mu.Lock()
				ls := append([]string(nil), s.logSatirlari...)
				s.mu.Unlock()
				jsonYaz(w, map[string]any{"dosya": r.URL.Query().Get("dosya"), "satirlar": ls})
			})
			r.With(middleware.MusteriScope).Get("/domains/{id}/cron", func(w http.ResponseWriter, r *http.Request) {
				jsonYaz(w, []any{map[string]any{"idx": 0, "dakika": "*/5", "saat": "*", "gun": "*", "ay": "*", "hafta": "*",
					"komut": "php /home/c_a_com/public_html/cron.php", "etkin": true}})
			})
			r.With(middleware.MusteriScope).Post("/domains/{id}/cron", func(w http.ResponseWriter, r *http.Request) {
				s.kaydet(r)
				w.WriteHeader(http.StatusOK)
			})
			r.With(middleware.MusteriScope).Get("/domains/{id}/dns", func(w http.ResponseWriter, r *http.Request) {
				jsonYaz(w, []any{
					map[string]any{"ad": "@", "tip": "A", "deger": "198.51.100.1", "oncelik": 0, "aktif": true},
					map[string]any{"ad": "@", "tip": "MX", "deger": "mail.a.com", "oncelik": 10, "aktif": true},
				})
			})
			r.With(middleware.AdminOnly).Get("/admin/system/loglar", func(w http.ResponseWriter, r *http.Request) {
				s.kaydet(r)
				jsonYaz(w, map[string]any{"kaynak": r.URL.Query().Get("kaynak"), "satirlar": []string{"sunucu satırı 1", "Authorization: Bearer " + kanaryaEnv}})
			})
		})
	})
	o := ortam{
		komut: func(ctx context.Context, argv ...string) ([]byte, error) {
			s.mu.Lock()
			s.komutlar = append(s.komutlar, append([]string(nil), argv...))
			bekle, ng := s.komutBekle, s.nginxCikti
			s.mu.Unlock()
			if bekle {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			switch argv[0] {
			case "nginx":
				return []byte(ng), fmt.Errorf("exit status 1")
			case "girginospanel-dogrula":
				return []byte(`{"gecen":40,"kritik":2,"uyari":1,"dusenler":[` +
					`{"seviye":"KRITIK","kontrol":"vhost a.com","detay":"eksik"},` +
					`{"seviye":"KRITIK","kontrol":"vhost b.com","detay":"komsu-sirri"},` +
					`{"seviye":"UYARI","kontrol":"saat","detay":"NTP"}]}`), fmt.Errorf("exit status 1")
			case "systemctl":
				return []byte("active\n"), nil
			case "tail":
				return []byte("Oct 1 CROND[1]: (c_a_com) CMD (php a.php)\nOct 1 CROND[2]: (c_b_com) CMD (komsu)\n"), nil
			case "journalctl":
				return []byte("fpm satırı " + kanaryaEposta + "\n"), nil
			case "girginospanel-repair":
				return []byte("\x1b[33m→ (dry) \x1b[0mchown /home/c_a_com/x\n→ (dry) chown /home/c_b_com/y\n"), nil
			}
			return nil, fmt.Errorf("bilinmeyen komut")
		},
		kiraciKomut: func(_ context.Context, sk string, argv ...string) ([]byte, error) {
			s.mu.Lock()
			s.kiraci = append(s.kiraci, append([]string{sk}, argv...))
			s.mu.Unlock()
			if argv[0] == "du" {
				return []byte("5\t/home/" + sk + "/tmp\n70\t/home/" + sk + "/public_html\n80\t/home/" + sk + "\n"), nil
			}
			return []byte("[2026-10-01] local.ERROR: DB_PASSWORD=" + kanaryaEnv + "\n"), nil
		},
		yokla: func(u string) monitor.DomainHealth {
			s.mu.Lock()
			s.yoklanan = append(s.yoklanan, u)
			s.mu.Unlock()
			return monitor.DomainHealth{URL: u, DurumKodu: 200, Erisilebilir: true, YanitSuresiMs: 12}
		},
		dig: func(_ context.Context, sunucu, ad, tur string) ([]string, error) {
			switch tur {
			case "A":
				return []string{"198.51.100.1"}, nil
			case "MX":
				if sunucu == yetkiliDNS {
					return []string{"10 mail.a.com"}, nil
				}
				return []string{"10 mx.baska.net"}, nil
			}
			return nil, nil
		},
	}
	s.y = yeni(db, r, []string{panelSirri}, o)
	return s
}

func emir(arac, param string) protokol.IsEmri {
	return protokol.IsEmri{IsKodu: "ABCDEFGH", Oturum: "oturum0001", BaglantiID: "baglanti01", Arac: arac,
		Param: json.RawMessage(param), Risk: protokol.R0}
}

func oturum(rol protokol.Rol, etiketler ...string) protokol.OturumBaglami {
	b := protokol.OturumBaglami{Rol: rol, Duzey: protokol.DuzeyOkuma, Dil: "tr"}
	for i, e := range etiketler {
		b.Hesaplar = append(b.Hesaplar, protokol.HesapEtiketi{Sira: i + 1, Etiket: e})
	}
	return b
}

var (
	musteriA = Kimlik{Rol: protokol.RolMusteri, HesapIDleri: []int64{101}, TgKisa: "tgA"}
	bayi10   = Kimlik{Rol: protokol.RolBayi, KullaniciID: 10, Kullanici: "bayi10", HesapIDleri: []int64{101, 102}, TgKisa: "tgB"}
	kok      = Kimlik{Rol: protokol.RolKok, KullaniciID: 1, HesapIDleri: []int64{101, 102, 103}, TgKisa: "tgK"}
)

func (s *sahne) calistir(k *Kimlik, b protokol.OturumBaglami, arac, param string) protokol.IsSonucu {
	ctx := context.Background()
	if k != nil {
		ctx = KimlikBagla(ctx, *k)
	}
	return s.y.Yurut(ctx, b, emir(arac, param))
}

func beklenenHata(t *testing.T, ad string, r protokol.IsSonucu, kod protokol.HataKodu) {
	t.Helper()
	if r.Ok || r.HataKodu != kod {
		t.Fatalf("%s: beklenen %s, gelen ok=%v kod=%q ozet=%q", ad, kod, r.Ok, r.HataKodu, r.Ozet)
	}
	if err := r.Dogrula(); err != nil {
		t.Fatalf("%s: hata sonucu protokol dışı: %v", ad, err)
	}
}

func basarili(t *testing.T, ad string, r protokol.IsSonucu) {
	t.Helper()
	if !r.Ok {
		t.Fatalf("%s: başarısız %q %q", ad, r.HataKodu, r.Ozet)
	}
	if err := r.Dogrula(); err != nil {
		t.Fatalf("%s: sonuç protokol dışı: %v", ad, err)
	}
	if !strings.HasPrefix(r.Kanit, "sha256:") {
		t.Fatalf("%s: kanıt yok", ad)
	}
}

func metinHepsi(r protokol.IsSonucu) string { return r.Ozet + "\n" + strings.Join(r.Satirlar, "\n") }

// hesapliParam — her araç için en küçük geçerli parametre (hesap=1).
func hesapliParam(a protokol.AracTanimi) string {
	p := map[string]any{}
	for _, pt := range a.Parametreler {
		switch {
		case pt.Hesap:
			p["hesap"] = 1
		case pt.Zorunlu && len(pt.Enum) > 0:
			p[pt.Ad] = pt.Enum[0]
		}
	}
	b, _ := json.Marshal(p)
	return string(b)
}

func TestKapsamMusteri(t *testing.T) {
	s := sahneKur(t)
	r := s.calistir(&musteriA, oturum(protokol.RolMusteri, "a.com"), "hesap_ozet", `{"hesap":1}`)
	basarili(t, "müşteri kendi hesabı", r)
	if s.son.mc == nil || s.son.mc.DomainID != 101 || s.son.c != nil {
		t.Fatalf("süreç içi kimlik yanlış: %+v %+v", s.son.mc, s.son.c)
	}
	if !strings.Contains(r.Ozet, "a.com") {
		t.Fatalf("özet: %q", r.Ozet)
	}
	// Her hesaplı araç: müşteri A'nın kimliği B'nin hesabını gösterirse (etiket a.com) → kapsam_disi;
	// oturum B'nin etiketini taşırsa da (merkez uydurdu) → kapsam_disi.
	for _, a := range protokol.Katalog() {
		param := hesapliParam(a)
		if !strings.Contains(param, `"hesap"`) {
			continue
		}
		bek := protokol.HataKapsam
		if !a.RolIzinli(protokol.RolMusteri) {
			bek = protokol.HataYetki
		}
		sahte := Kimlik{Rol: protokol.RolMusteri, HesapIDleri: []int64{102}, TgKisa: "tgA"}
		beklenenHata(t, a.Ad+" A→B (kimlik)", s.calistir(&sahte, oturum(protokol.RolMusteri, "a.com"), a.Ad, param), bek)
		beklenenHata(t, a.Ad+" A→B (etiket)", s.calistir(&musteriA, oturum(protokol.RolMusteri, "b.com"), a.Ad, param), bek)
	}
	beklenenHata(t, "hesap 2 yok", s.calistir(&musteriA, oturum(protokol.RolMusteri, "a.com"), "hesap_ozet", `{"hesap":2}`), protokol.HataKapsam)
	iki := Kimlik{Rol: protokol.RolMusteri, HesapIDleri: []int64{101, 102}}
	beklenenHata(t, "müşteri iki hesap", s.calistir(&iki, oturum(protokol.RolMusteri, "a.com", "b.com"), "hesap_ozet", `{"hesap":2}`), protokol.HataKapsam)
	askida := Kimlik{Rol: protokol.RolMusteri, HesapIDleri: []int64{104}}
	beklenenHata(t, "askıdaki hesap", s.calistir(&askida, oturum(protokol.RolMusteri, "d.com"), "hesap_ozet", `{"hesap":1}`), protokol.HataKapsam)
	kullanicili := Kimlik{Rol: protokol.RolMusteri, KullaniciID: 1, HesapIDleri: []int64{101}}
	beklenenHata(t, "müşteri kimliğinde uid", s.calistir(&kullanicili, oturum(protokol.RolMusteri, "a.com"), "hesap_ozet", `{"hesap":1}`), protokol.HataYetki)
}

func TestKapsamBayi(t *testing.T) {
	s := sahneKur(t)
	b := oturum(protokol.RolBayi, "a.com", "b.com")
	r := s.calistir(&bayi10, b, "hesap_ozet", `{"hesap":1}`)
	basarili(t, "bayi kendi hesabı", r)
	if s.son.c == nil || s.son.c.Role != "reseller" || s.son.c.ResellerID != 10 {
		t.Fatalf("bayi süreç içi kimliği: %+v", s.son.c)
	}
	for _, a := range protokol.Katalog() {
		param := strings.Replace(hesapliParam(a), `"hesap":1`, `"hesap":2`, 1)
		if !strings.Contains(param, `"hesap"`) {
			continue
		}
		bek := protokol.HataKapsam
		if !a.RolIzinli(protokol.RolBayi) {
			bek = protokol.HataYetki
		}
		beklenenHata(t, a.Ad+" bayi10→bayi11", s.calistir(&bayi10, b, a.Ad, param), bek)
	}
	askida := Kimlik{Rol: protokol.RolBayi, KullaniciID: 12, HesapIDleri: []int64{101}}
	beklenenHata(t, "askıdaki bayi", s.calistir(&askida, oturum(protokol.RolBayi, "a.com"), "hesap_ozet", `{"hesap":1}`), protokol.HataYetki)
	sahteKok := Kimlik{Rol: protokol.RolKok, KullaniciID: 10, HesapIDleri: []int64{102}}
	beklenenHata(t, "bayi kök taklidi", s.calistir(&sahteKok, oturum(protokol.RolKok, "b.com"), "hesap_ozet", `{"hesap":1}`), protokol.HataYetki)
	// Rotaya gitmeyen (yalnız DB/komut) araçta da ret: middleware'in ikinci katmanına güvenilmez.
	beklenenHata(t, "bayi kök taklidi (DB aracı)", s.calistir(&sahteKok, oturum(protokol.RolKok, "b.com"), "son_degisiklikler", `{"hesap":1}`), protokol.HataYetki)
	beklenenHata(t, "bayi kök taklidi (komut aracı)", s.calistir(&sahteKok, oturum(protokol.RolKok, "b.com"), "nginx_test", `{"hesap":1}`), protokol.HataYetki)
}

func TestKokYalnizAraclar(t *testing.T) {
	s := sahneKur(t)
	for _, c := range []struct{ arac, param string }{
		{"sunucu_log_oku", `{"kaynak":"panel"}`},
		{"onarim_onizle", `{"hesap":1}`},
	} {
		beklenenHata(t, c.arac+" müşteri", s.calistir(&musteriA, oturum(protokol.RolMusteri, "a.com"), c.arac, c.param), protokol.HataYetki)
		beklenenHata(t, c.arac+" bayi", s.calistir(&bayi10, oturum(protokol.RolBayi, "a.com", "b.com"), c.arac, c.param), protokol.HataYetki)
		basarili(t, c.arac+" kök", s.calistir(&kok, oturum(protokol.RolKok, "a.com", "b.com", "c.com"), c.arac, c.param))
	}
	r := s.calistir(&kok, oturum(protokol.RolKok, "a.com", "b.com", "c.com"), "onarim_onizle", `{"hesap":1}`)
	if len(r.Satirlar) != 1 || !strings.Contains(r.Satirlar[0], "(dry)") || strings.Contains(metinHepsi(r), "c_b_com") || strings.Contains(metinHepsi(r), "\x1b") {
		t.Fatalf("onarım önizleme yalnız a.com satırı olmalı: %q", r.Satirlar)
	}
	// İkinci denetim: katalog atlatılsa bile gövde kök dışını reddeder.
	c := &cagri{y: s.y, k: &bayi10, b: oturum(protokol.RolBayi, "a.com", "b.com")}
	if _, err := sunucuLogOku(context.Background(), c); err == nil {
		t.Fatal("sunucuLogOku gövdesi bayiyi reddetmedi")
	}
	if _, err := onarimOnizle(context.Background(), c); err == nil {
		t.Fatal("onarimOnizle gövdesi bayiyi reddetmedi")
	}
}

// Yürütücünün kapsamı atlatılsa (hata) bile süreç içi istek middleware'de reddedilir.
func TestIkinciKatmanMiddleware(t *testing.T) {
	s := sahneKur(t)
	iste := func(dk middleware.DestekKimlik, metod, yol string) int {
		req := httptest.NewRequest(metod, yol, nil).WithContext(middleware.DestekBagla(context.Background(), dk))
		w := httptest.NewRecorder()
		s.y.router.ServeHTTP(w, req)
		return w.Code
	}
	b10 := middleware.DestekKimlik{Rol: middleware.DestekBayi, KullaniciID: 10, Kullanici: "bayi10", TgKisa: "t"}
	if k := iste(b10, "GET", "/api/v1/domains/102/kaynak"); k != 403 {
		t.Fatalf("bayi10 → bayi11 hesabı: %d", k)
	}
	if k := iste(b10, "GET", "/api/v1/domains/101/kaynak"); k != 200 {
		t.Fatalf("bayi10 kendi hesabı: %d", k)
	}
	if k := iste(b10, "GET", "/api/v1/admin/system/loglar"); k != 403 {
		t.Fatalf("bayi kök ucu: %d", k)
	}
	m := middleware.DestekKimlik{Rol: middleware.DestekMusteri, DomainID: 101, TgKisa: "t"}
	if k := iste(m, "GET", "/api/v1/domains/102"); k != 403 {
		t.Fatalf("müşteri A → B: %d", k)
	}
	if k := iste(m, "POST", "/api/v1/domains/101/cron"); k != 403 {
		t.Fatalf("yazma rotası destek kimliğine açık: %d", k)
	}
	ask := middleware.DestekKimlik{Rol: middleware.DestekBayi, KullaniciID: 12, TgKisa: "t"}
	if k := iste(ask, "GET", "/api/v1/domains/101/kaynak"); k != 403 {
		t.Fatalf("askıdaki bayi: %d", k)
	}
	// Başlık taklidi: context işareti yok → 401.
	req := httptest.NewRequest("GET", "/api/v1/domains/101", nil)
	req.Header.Set("X-Destek-Kimlik", "kok")
	w := httptest.NewRecorder()
	s.y.router.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("başlık taklidi: %d", w.Code)
	}
}

func TestSemaDisiGirdi(t *testing.T) {
	s := sahneKur(t)
	b := oturum(protokol.RolMusteri, "a.com")
	for _, c := range []struct {
		ad, arac, param string
		kod             protokol.HataKodu
	}{
		{"bilinmeyen araç", "kabuk", `{"komut":"id"}`, protokol.HataAracBilinmiyor},
		{"bilinmeyen parametre", "hesap_ozet", `{"hesap":1,"alan_adi":"b.com"}`, protokol.HataParam},
		{"tür", "hesap_ozet", `{"hesap":"1"}`, protokol.HataParam},
		{"eksik zorunlu", "log_oku", `{"hesap":1}`, protokol.HataParam},
		{"enum dışı", "log_oku", `{"hesap":1,"tur":"/etc/shadow"}`, protokol.HataParam},
		{"satır enum dışı", "log_oku", `{"hesap":1,"tur":"nginx_error","satir":5000}`, protokol.HataParam},
		{"yinelenen anahtar", "hesap_ozet", `{"hesap":1,"hesap":1}`, protokol.HataParam},
		{"ek veri", "hesap_ozet", `{"hesap":1}{"hesap":2}`, protokol.HataParam},
		{"yol geçişi", "site_yokla", `{"hesap":1,"yol":"/../../etc/passwd"}`, protokol.HataParam},
		{"yol çift eğik", "site_yokla", `{"hesap":1,"yol":"//evil.example/x"}`, protokol.HataParam},
		{"yol sorgu", "site_yokla", `{"hesap":1,"yol":"/?u=http://169.254.169.254/"}`, protokol.HataParam},
		{"alt ad enjeksiyon", "dns_kontrol", `{"hesap":1,"alt_ad":"www @8.8.8.8"}`, protokol.HataParam},
		{"hesap sıfır", "hesap_ozet", `{"hesap":0}`, protokol.HataKapsam},
	} {
		beklenenHata(t, c.ad, s.calistir(&musteriA, b, c.arac, c.param), c.kod)
	}
	// İş emri riski katalogla uyuşmazsa ret.
	e := emir("hesap_ozet", `{"hesap":1}`)
	e.Risk = protokol.R2
	beklenenHata(t, "risk", s.y.Yurut(KimlikBagla(context.Background(), musteriA), b, e), protokol.HataParam)
	// Kimlik yok / rol uyuşmuyor / hesap listesi uyuşmuyor.
	beklenenHata(t, "kimlik yok", s.calistir(nil, b, "hesap_ozet", `{"hesap":1}`), protokol.HataYetki)
	beklenenHata(t, "rol uyuşmuyor", s.calistir(&musteriA, oturum(protokol.RolKok, "a.com"), "hesap_ozet", `{"hesap":1}`), protokol.HataYetki)
	beklenenHata(t, "liste uyuşmuyor", s.calistir(&bayi10, oturum(protokol.RolBayi, "a.com"), "hesap_ozet", `{"hesap":1}`), protokol.HataKapsam)
	// Düzey kapalı → duzey_yetersiz.
	kapali := b
	kapali.Duzey = protokol.DuzeyKapali
	beklenenHata(t, "düzey", s.calistir(&musteriA, kapali, "hesap_ozet", `{"hesap":1}`), protokol.HataDuzey)
}

func TestMaskeKanarya(t *testing.T) {
	s := sahneKur(t)
	s.logSatirlari = []string{
		"GET /?token=x DB_PASSWORD=" + kanaryaEnv + " ref " + panelSirri,
		"Authorization: Bearer " + kanaryaJWT,
		"giris " + kanaryaEposta + " from " + kanaryaIP,
		"mysql://kok:" + kanaryaFTP + "@127.0.0.1/db",
	}
	b := oturum(protokol.RolMusteri, "a.com")
	sonuclar := map[string]protokol.IsSonucu{
		"hesap_ozet": s.calistir(&musteriA, b, "hesap_ozet", `{"hesap":1}`),
		"ssl_durum":  s.calistir(&musteriA, b, "ssl_durum", `{"hesap":1}`),
		"log_oku":    s.calistir(&musteriA, b, "log_oku", `{"hesap":1,"tur":"nginx_access","satir":300}`),
		"laravel":    s.calistir(&musteriA, b, "log_oku", `{"hesap":1,"tur":"laravel"}`),
		"php_fpm":    s.calistir(&musteriA, b, "log_oku", `{"hesap":1,"tur":"php_fpm"}`),
		"sunucu":     s.calistir(&kok, oturum(protokol.RolKok, "a.com", "b.com", "c.com"), "sunucu_log_oku", `{"kaynak":"panel"}`),
	}
	for ad, r := range sonuclar {
		basarili(t, ad, r)
		ham, _ := json.Marshal(r)
		for _, k := range []string{kanaryaEnv, panelSirri, kanaryaFTP, kanaryaEposta, kanaryaIP, kanaryaJWT} {
			if strings.Contains(string(ham), k) {
				t.Fatalf("%s: kanarya %q çıktıda:\n%s", ad, k, ham)
			}
		}
	}
	if len(sonuclar["log_oku"].Satirlar) != 4 {
		t.Fatalf("log satırları: %q", sonuclar["log_oku"].Satirlar)
	}
	// Laravel günlüğü KİRACI kimliğinde ve DB'deki uygulama kökünden okundu.
	if len(s.kiraci) == 0 || s.kiraci[len(s.kiraci)-1][0] != "c_a_com" ||
		s.kiraci[len(s.kiraci)-1][len(s.kiraci[len(s.kiraci)-1])-1] != "/home/c_a_com/laravel_app/storage/logs/laravel.log" {
		t.Fatalf("laravel okuma: %q", s.kiraci)
	}
}

func TestSuzgecMaskedenSonra(t *testing.T) {
	s := sahneKur(t)
	s.logSatirlari = []string{"a DB_PASSWORD=" + kanaryaEnv, "GET /index.php 200", "GET /wp-login.php 404"}
	b := oturum(protokol.RolMusteri, "a.com")
	r := s.calistir(&musteriA, b, "log_oku", `{"hesap":1,"tur":"nginx_access","filtre":"kanaryasir"}`)
	basarili(t, "süzgeç", r)
	if len(r.Satirlar) != 0 {
		t.Fatalf("süzgeç maskelenmiş sırrı yokladı (kahin): %q", r.Satirlar)
	}
	r = s.calistir(&musteriA, b, "log_oku", `{"hesap":1,"tur":"nginx_access","filtre":"WP-LOGIN"}`)
	if len(r.Satirlar) != 1 || !strings.Contains(r.Satirlar[0], "wp-login") {
		t.Fatalf("düz alt dizge süzgeci: %q", r.Satirlar)
	}
}

func TestSatirBaytSiniri(t *testing.T) {
	s := sahneKur(t)
	uzun := strings.Repeat("ğ", 1500) // 3000 bayt
	for i := 0; i < 2000; i++ {
		s.logSatirlari = append(s.logSatirlari, strconv.Itoa(i)+" "+uzun)
	}
	r := s.calistir(&musteriA, oturum(protokol.RolMusteri, "a.com"), "log_oku", `{"hesap":1,"tur":"nginx_error","satir":300}`)
	basarili(t, "sınır", r)
	if len(r.Satirlar) != protokol.SatirSayisiSiniri || !r.Kesildi {
		t.Fatalf("satır sınırı: %d kesildi=%v", len(r.Satirlar), r.Kesildi)
	}
	for _, ln := range r.Satirlar {
		if len(ln) > protokol.SatirSiniri {
			t.Fatalf("satır %d bayt > %d", len(ln), protokol.SatirSiniri)
		}
	}
	if !strings.HasPrefix(r.Satirlar[len(r.Satirlar)-1], "1999 ") {
		t.Fatalf("son satırlar tutulmalı: %q", r.Satirlar[len(r.Satirlar)-1][:10])
	}
	r = s.calistir(&musteriA, oturum(protokol.RolMusteri, "a.com"), "log_oku", `{"hesap":1,"tur":"nginx_error"}`)
	if len(r.Satirlar) != 100 {
		t.Fatalf("varsayılan 100 satır: %d", len(r.Satirlar))
	}
}

func TestOzelAraclarKiraciSuzgeci(t *testing.T) {
	s := sahneKur(t)
	s.nginxCikti = "nginx: [emerg] unknown directive in /etc/nginx/conf.d/dom_c_a_com.conf:12\n" +
		"nginx: [emerg] host not found in /etc/nginx/conf.d/dom_c_b_com.conf:3 komsu.example\n" +
		"nginx: configuration file /etc/nginx/nginx.conf test failed\n"
	bM := oturum(protokol.RolMusteri, "a.com")
	r := s.calistir(&musteriA, bM, "nginx_test", `{"hesap":1}`)
	basarili(t, "nginx_test M", r)
	h := metinHepsi(r)
	if !strings.Contains(h, "dom_c_a_com.conf") || strings.Contains(h, "c_b_com") || strings.Contains(h, "komsu") {
		t.Fatalf("nginx_test müşteriye komşu satırı sızdırdı ya da kendi satırını vermedi:\n%s", h)
	}
	r = s.calistir(&kok, oturum(protokol.RolKok, "a.com", "b.com", "c.com"), "nginx_test", `{"hesap":1}`)
	if !strings.Contains(metinHepsi(r), "dom_c_b_com") {
		t.Fatal("kök ham nginx -t çıktısını görmeli")
	}

	r = s.calistir(&musteriA, bM, "dogrula_ozet", `{"hesap":1}`)
	basarili(t, "dogrula M", r)
	h = metinHepsi(r)
	if len(r.Satirlar) != 1 || !strings.Contains(h, "vhost a.com") || strings.Contains(h, "b.com") || strings.Contains(h, "NTP") || strings.Contains(h, "40") {
		t.Fatalf("dogrula_ozet müşteriye yalnız kendi bulgusunu vermeli:\n%s", h)
	}

	r = s.calistir(&musteriA, bM, "site_yokla", `{"hesap":1,"yol":"/wp-login.php","protokol":"http"}`)
	basarili(t, "site_yokla", r)
	if got := s.yoklanan[len(s.yoklanan)-1]; got != "http://a.com/wp-login.php" {
		t.Fatalf("yoklanan URL: %q", got)
	}

	s.komutlar = nil
	r = s.calistir(&musteriA, bM, "servis_durum", `{"servis":"php_fpm"}`)
	basarili(t, "servis_durum M", r)
	if len(s.komutlar) != 1 || strings.Join(s.komutlar[0], " ") != "systemctl is-active -- php-fpm-c_a_com.service" {
		t.Fatalf("müşteri yalnız kendi FPM birimine bakmalı: %q", s.komutlar)
	}
	r = s.calistir(&musteriA, bM, "servis_durum", `{}`)
	if strings.Contains(metinHepsi(r), "(") && strings.Contains(metinHepsi(r), "mariadb (") {
		t.Fatalf("müşteri birim ayrıntısı görmemeli: %q", r.Satirlar)
	}

	r = s.calistir(&musteriA, bM, "cron_liste", `{"hesap":1}`)
	basarili(t, "cron_liste", r)
	if h := metinHepsi(r); !strings.Contains(h, "görev 1 [etkin] */5") || !strings.Contains(h, "(c_a_com) CMD") || strings.Contains(h, "c_b_com") {
		t.Fatalf("cron_liste: %q", r.Satirlar)
	}
	r = s.calistir(&musteriA, bM, "log_oku", `{"hesap":1,"tur":"cron"}`)
	basarili(t, "cron günlüğü", r)
	if h := metinHepsi(r); !strings.Contains(h, "c_a_com") || strings.Contains(h, "c_b_com") {
		t.Fatalf("cron günlüğü komşu satırı sızdırdı: %q", r.Satirlar)
	}

	r = s.calistir(&musteriA, bM, "disk_kota", `{"hesap":1}`)
	basarili(t, "disk_kota", r)
	if h := metinHepsi(r); !strings.Contains(h, "dizin public_html: 70 MB") || strings.Index(h, "public_html") > strings.Index(h, "dizin tmp") {
		t.Fatalf("disk_kota dizinleri: %q", r.Satirlar)
	}

	r = s.calistir(&musteriA, bM, "dns_kontrol", `{"hesap":1}`)
	basarili(t, "dns_kontrol", r)
	h = metinHepsi(r)
	if !strings.Contains(h, "MX a.com") || !strings.Contains(h, "UYUŞMAZ") || !strings.Contains(r.Ozet, "1 uyuşmazlık") {
		t.Fatalf("dns_kontrol karşılaştırması:\n%s", h)
	}

	// Laravel uygulama kökü DB'de biçim dışıysa okunmaz.
	k3 := Kimlik{Rol: protokol.RolKok, KullaniciID: 1, HesapIDleri: []int64{103}}
	beklenenHata(t, "laravel kök geçişi", s.calistir(&k3, oturum(protokol.RolKok, "c.com"), "log_oku", `{"hesap":1,"tur":"laravel"}`), protokol.HataKapsam)
}

func TestSonDegisikliklerKapsam(t *testing.T) {
	s := sahneKur(t)
	for _, q := range []string{
		`INSERT INTO audit_log (actor_user_id, actor_username, action, target, reseller_id) VALUES (10,'bayi10','php.degistir','a.com',10)`,
		`INSERT INTO audit_log (actor_user_id, actor_username, action, target, reseller_id) VALUES (0,'musteri:101','cron.ekle','a.com',10)`,
		`INSERT INTO audit_log (actor_user_id, actor_username, action, target, reseller_id) VALUES (11,'bayi11','php.degistir','b.com',11)`,
		`INSERT INTO audit_log (actor_user_id, actor_username, action, target, reseller_id) VALUES (11,'bayi11','sahte','a.com',11)`,
		`INSERT INTO audit_log (actor_user_id, actor_username, action, target, reseller_id, ts) VALUES (10,'bayi10','eski','a.com',10, NOW() - INTERVAL 40 DAY)`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	r := s.calistir(&bayi10, oturum(protokol.RolBayi, "a.com", "b.com"), "son_degisiklikler", `{"hesap":1,"gun":30}`)
	basarili(t, "bayi denetim", r)
	h := metinHepsi(r)
	if len(r.Satirlar) != 2 || strings.Contains(h, "b.com") || strings.Contains(h, "sahte") || strings.Contains(h, "eski") {
		t.Fatalf("bayi denetim kapsamı:\n%s", h)
	}
	r = s.calistir(&musteriA, oturum(protokol.RolMusteri, "a.com"), "son_degisiklikler", `{"hesap":1}`)
	h = metinHepsi(r)
	if len(r.Satirlar) != 2 || strings.Contains(h, "bayi10") || !strings.Contains(h, "siz (müşteri paneli)") || !strings.Contains(h, "panel yöneticisi") {
		t.Fatalf("müşteri denetim görünümü:\n%s", h)
	}
	r = s.calistir(&kok, oturum(protokol.RolKok, "a.com", "b.com", "c.com"), "son_degisiklikler", `{"hesap":1}`)
	if len(r.Satirlar) != 3 {
		t.Fatalf("kök a.com kayıtları: %q", r.Satirlar)
	}
}

func TestZamanAsimi(t *testing.T) {
	s := sahneKur(t)
	eski := araclar["nginx_test"]
	araclar["nginx_test"] = aracTanim{sure: 50 * time.Millisecond, fn: eski.fn}
	t.Cleanup(func() { araclar["nginx_test"] = eski })
	s.komutBekle = true
	t0 := time.Now()
	beklenenHata(t, "zaman aşımı", s.calistir(&musteriA, oturum(protokol.RolMusteri, "a.com"), "nginx_test", `{"hesap":1}`), protokol.HataZamanAsimi)
	if d := time.Since(t0); d > 5*time.Second {
		t.Fatalf("araç başına zaman aşımı uygulanmadı: %v", d)
	}
}

func TestHesapListesi(t *testing.T) {
	s := sahneKur(t)
	ctx := context.Background()
	et, id, err := HesapListesi(ctx, s.db, protokol.RolBayi, 10, 0)
	if err != nil || len(id) != 1 || id[0] != 101 || et[0].Etiket != "a.com" || et[0].Sira != 1 {
		t.Fatalf("bayi10: %v %v %v", et, id, err)
	}
	et, id, _ = HesapListesi(ctx, s.db, protokol.RolMusteri, 0, 104)
	if len(id) != 0 {
		t.Fatalf("askıdaki müşteri hesabı listelendi: %v", et)
	}
	_, id, _ = HesapListesi(ctx, s.db, protokol.RolKok, 1, 0)
	if len(id) != 4 {
		t.Fatalf("kök: %v", id)
	}
	// Liste → yürütücü uçtan uca.
	et, id, _ = HesapListesi(ctx, s.db, protokol.RolBayi, 10, 0)
	b := protokol.OturumBaglami{Rol: protokol.RolBayi, Duzey: protokol.DuzeyOkuma, Hesaplar: et}
	basarili(t, "liste ile", s.calistir(&Kimlik{Rol: protokol.RolBayi, KullaniciID: 10, HesapIDleri: id}, b, "hesap_ozet", `{"hesap":1}`))
}
