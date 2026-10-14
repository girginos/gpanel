package reseller

// parola_db_test.go — PUT /api/v1/resellers/{id}/password (WHMCS bayi ChangePassword):
// gerçek chi (/api/v1, RoutePattern gerçek) + RequireAuth + gerçek API anahtarı doğrulaması
// (0128 göçü) + varsayılan-reddet izin tablosu + geçici MariaDB.
//
// Kanıtlanan: yalnız kök anahtarı ve AYRI bayi.parola izniyle (genel bayi:yaz yetmez); parola
// gerçekten değişir; bayinin açık oturumları düşer ve API anahtarları iptal olur; denetim
// anahtar adıyla, parolasız; PUT /resellers/{id} gövdesindeki parola anahtara yine kapalı.

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"

	"girginospanel/internal/apianahtar"
	"girginospanel/internal/middleware"
)

const bpEski, bpYeni = "Eski-Bayi-Parola-1", "Yeni-Bayi-Parola-2"

func bpDB(t *testing.T) (*sql.DB, *chi.Mux, func(sahip int64, rol, ad string, izinler ...string) string, func() string) {
	t.Helper()
	db := opGeciciDB(t)
	eskiHash, _ := bcrypt.GenerateFromPassword([]byte(bpEski), 4)
	for _, q := range []string{
		`ALTER TABLE users ADD COLUMN updated_at DATETIME NULL, ADD COLUMN token_gecersiz_ts BIGINT NULL,
			ADD COLUMN last_activity_ts BIGINT NOT NULL DEFAULT 0`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}
	if _, err := db.Exec(`INSERT INTO users (id, username, role, password_hash) VALUES (10,'bayia','reseller',?)`, string(eskiHash)); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile("../../migrations/0128_api_anahtarlari.sql")
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
				t.Fatalf("göç 0128: %v", err)
			}
		}
	}
	middleware.Init(db)
	t.Cleanup(func() { middleware.Init(nil) })
	h := &Handlers{DB: db}
	r := chi.NewRouter()
	// Grup içi ara katman: RoutePattern eşleşmeden SONRA dolu (izin tablosu gerçek deseni görür).
	r.Route("/api/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth(kaSir))
			r.With(middleware.AdminOnly).Put("/resellers/{id}/password", h.ParolaSifirla)
			r.With(middleware.AdminOnly).Put("/resellers/{id}", h.Guncelle)
		})
	})
	anahtar := func(sahip int64, rol, ad string, izinler ...string) string {
		t.Helper()
		u, err := apianahtar.Uret()
		if err != nil {
			t.Fatal(err)
		}
		ipler, kul := []string{}, "bayia"
		if rol == apianahtar.RolKok {
			ipler, kul = []string{"192.0.2.0/24"}, apianahtar.KokKullaniciAdi
		}
		if _, err := apianahtar.Ekle(context.Background(), db, &apianahtar.Kayit{
			SahipID: sahip, SahipRol: rol, Ad: ad, Onek: u.Onek, Ozet: u.Ozet, Son4: u.Son4,
			Izinler: izinler, IPler: ipler, HizDk: 600, SonKullanma: time.Now().Add(24 * time.Hour).Unix(),
		}); err != nil {
			t.Fatal(err)
		}
		return "gosp " + kul + ":" + u.Tam
	}
	hash := func() string {
		var s string
		_ = db.QueryRow(`SELECT password_hash FROM users WHERE id=10`).Scan(&s)
		return s
	}
	return db, r, anahtar, hash
}

func bpIstek(h http.Handler, yol, yetki, govde string) (int, string) {
	req := httptest.NewRequest(http.MethodPut, yol, strings.NewReader(govde))
	req.RemoteAddr = "192.0.2.10:40000"
	req.Header.Set("Authorization", yetki)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

func TestBayiParola_IzinMatrisi_GercekDegisim_OturumAnahtar(t *testing.T) {
	db, r, anahtar, hash := bpDB(t)
	bayiKey := anahtar(10, apianahtar.RolBayi, "bayi-otomasyon", "hesap:yaz")
	for _, c := range []struct {
		ad, yetki string
		kod       int
	}{
		{"kök: yalnız bayi:yaz (genel bayi izni parola değiştirmez)", anahtar(1, apianahtar.RolKok, "K1", "bayi:yaz"), http.StatusForbidden},
		{"kök: sso", anahtar(1, apianahtar.RolKok, "K2", "sso:yaz", "bayi:yaz"), http.StatusForbidden},
		{"bayi anahtarı", bayiKey, http.StatusForbidden},
	} {
		if kod, g := bpIstek(r, "/api/v1/resellers/10/password", c.yetki, `{"parola":"`+bpYeni+`"}`); kod != c.kod {
			t.Errorf("%s: %d beklenir, %d %s", c.ad, c.kod, kod, g)
		}
	}
	once := hash()
	if bcrypt.CompareHashAndPassword([]byte(once), []byte(bpEski)) != nil {
		t.Fatal("yetkisiz istek parolayı değiştirdi")
	}

	k := anahtar(1, apianahtar.RolKok, "WHMCS", "bayi.parola:yaz")
	// Geçersizler: kısa, uzun, çelişen takma ad, bilinmeyen alan, olmayan/bayi olmayan.
	for _, c := range []struct {
		yol, g string
		kod    int
	}{
		{"/api/v1/resellers/10/password", `{"parola":"kisa7ch"}`, http.StatusBadRequest},
		{"/api/v1/resellers/10/password", `{"parola":"` + strings.Repeat("x", 129) + `"}`, http.StatusBadRequest},
		{"/api/v1/resellers/10/password", `{"parola":"` + bpYeni + `","password":"baska-parola-99"}`, http.StatusBadRequest},
		{"/api/v1/resellers/10/password", `{"parola":"` + bpYeni + `","foo":1}`, http.StatusBadRequest},
		{"/api/v1/resellers/10/password", `{}`, http.StatusBadRequest},
		{"/api/v1/resellers/999/password", `{"parola":"` + bpYeni + `"}`, http.StatusNotFound},
		{"/api/v1/resellers/1/password", `{"parola":"` + bpYeni + `"}`, http.StatusNotFound},
	} {
		if kod, g := bpIstek(r, c.yol, k, c.g); kod != c.kod {
			t.Errorf("%s %s: %d beklenir, %d %s", c.yol, c.g[:min(len(c.g), 30)], c.kod, kod, g)
		}
	}
	if hash() != once {
		t.Fatal("reddedilen istek parolayı değiştirdi")
	}
	// Genel güncelleme ucundaki parola alanı anahtara KAPALI kalır (yönlendiren ileti).
	if kod, g := bpIstek(r, "/api/v1/resellers/10", anahtar(1, apianahtar.RolKok, "K3", "bayi:yaz", "bayi.parola:yaz"), `{"parola":"`+bpYeni+`"}`); kod != http.StatusForbidden || !strings.Contains(g, "/password") {
		t.Errorf("PUT /resellers/{id} parola anahtarla 403 + yönlendirme beklenir: %d %s", kod, g)
	}

	// Başarı ("password" takma adıyla).
	baslangic := time.Now().Unix()
	kod, g := bpIstek(r, "/api/v1/resellers/10/password", k, `{"password":"`+bpYeni+`"}`)
	if kod != http.StatusOK || strings.Contains(g, bpYeni) {
		t.Fatalf("200 beklenir (parola yanıtta yok): %d %s", kod, g)
	}
	if bcrypt.CompareHashAndPassword([]byte(hash()), []byte(bpYeni)) != nil {
		t.Fatal("parola değişmedi")
	}
	var damga int64
	_ = db.QueryRow(`SELECT COALESCE(token_gecersiz_ts,0) FROM users WHERE id=10`).Scan(&damga)
	if damga < baslangic {
		t.Fatalf("bayinin açık oturumları düşürülmedi: %d", damga)
	}
	var iptal int
	_ = db.QueryRow(`SELECT COUNT(*) FROM api_anahtarlari WHERE sahip_id=10 AND iptal_at IS NOT NULL`).Scan(&iptal)
	if iptal != 1 {
		t.Fatalf("bayinin API anahtarı iptal edilmeli: %d", iptal)
	}
	if kod, _ := bpIstek(r, "/api/v1/resellers/10/password", bayiKey, `{"parola":"x"}`); kod != http.StatusUnauthorized {
		t.Errorf("iptal edilen bayi anahtarı 401 beklenir: %d", kod)
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='bayi.parola' AND ok=1 AND target='bayia'
		AND actor_username='root (API anahtarı WHMCS)' AND reseller_id=10`).Scan(&n)
	if n != 1 {
		t.Fatalf("bayi.parola denetim kaydı yok: %d", n)
	}
	_ = db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE CONCAT_WS('|',target,COALESCE(detail,''),actor_username) LIKE ?`, "%"+bpYeni+"%").Scan(&n)
	if n != 0 {
		t.Fatal("parola denetim kaydına sızdı")
	}
}
