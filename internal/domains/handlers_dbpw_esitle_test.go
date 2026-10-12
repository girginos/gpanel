package domains

// handlers_dbpw_esitle_test.go — PUT /databases/{dbid}/password {"yalniz_kayit":true}.
//
// 🔴 OLAY (2026-10-12, bir müşteri sunucusu): panel kaydındaki DB parolası bayattı (MariaDB
// parolası panel dışından değişmişti) → phpMyAdmin SSO "Access denied". Mevcut tek yol
// "Parola Sıfırla" (ALTER USER) çalışan sitenin config'teki parolasını geçersiz kılardı.
// Yalnız-kayıt modu: MariaDB'ye DOKUNMAZ (ALTER USER sahtesi ÇAĞRILMAZ), parola
// MariaDB'de girerse yalnız kaydı eşitler; girmezse 409 + kod.
//
// Harness: handlers_dbpw_test.go (dp_kur — geçici DB + sahte MariaDB dikişleri).

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"girginospanel/internal/hesaplar"
)

type de_sahteEsitle struct {
	mu    sync.Mutex
	sonuc error
	cagri []string // "domain:kullanici:parola"
}

func (s *de_sahteEsitle) esitle(_ context.Context, _ *sql.DB, domainID int64, kul, parola string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cagri = append(s.cagri, fmt.Sprintf("%d:%s:%s", domainID, kul, parola))
	return s.sonuc
}

func de_kur(t *testing.T, sonuc error) (*Handlers, *dp_sahteMariaDB, *de_sahteEsitle) {
	t.Helper()
	h, sahte := dp_kur(t)
	es := &de_sahteEsitle{sonuc: sonuc}
	eski := dbParolaEsitle
	dbParolaEsitle = es.esitle
	t.Cleanup(func() { dbParolaEsitle = eski })
	return h, sahte, es
}

func de_istek(t *testing.T, h *Handlers, tok string, dbid int64, govde map[string]any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(govde)
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/databases/%d/password", dbid), bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	dp_router(h).ServeHTTP(rec, req)
	var g map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &g)
	return rec.Code, g
}

// 🔴 ÇEKİRDEK: yalnız-kayıt modu ALTER USER'a İNMEZ; doğru parolada 200.
func TestSetDatabasePassword_YalnizKayit_AlterUserYok(t *testing.T) {
	h, sahte, es := de_kur(t, nil)
	bayi3 := dp_bayi(t, 3)
	// Mevcut uygulama parolası 6 karakterden kısa olabilir: asgari uzunluk kuralı uygulanmaz.
	kod, g := de_istek(t, h, bayi3, 300, map[string]any{"parola": "kisa", "yalniz_kayit": true})
	if kod != http.StatusOK {
		t.Fatalf("kod=%d govde=%v; 200 bekleniyordu", kod, g)
	}
	if len(es.cagri) != 1 || es.cagri[0] != "30:kurban_db:kisa" {
		t.Errorf("eşitleme beklenen kimlikle çağrılmadı: %v", es.cagri)
	}
	if n := sahte.parolaSayi(); n != 0 {
		t.Errorf("yalnız-kayıt modunda ALTER USER yoluna %d kez inildi (çalışan site kırılırdı)", n)
	}
}

// Parola MariaDB'de girmiyorsa 409 + kod; ALTER USER yine yok.
func TestSetDatabasePassword_YalnizKayit_Uyusmuyor409(t *testing.T) {
	h, sahte, _ := de_kur(t, hesaplar.ErrDBParolaUyusmuyor)
	kod, g := de_istek(t, h, dp_bayi(t, 3), 300, map[string]any{"parola": "Yanlis-Parola-1", "yalniz_kayit": true})
	if kod != http.StatusConflict || g["kod"] != "db_parola_uyusmuyor" {
		t.Fatalf("kod=%d govde=%v; 409/db_parola_uyusmuyor bekleniyordu", kod, g)
	}
	if m, _ := g["hata"].(string); !strings.Contains(m, "yapılandırma dosyasındaki") {
		t.Errorf("mesaj kullanıcıyı mevcut parolaya yönlendirmiyor: %q", m)
	}
	if n := sahte.parolaSayi(); n != 0 {
		t.Errorf("ALTER USER yoluna %d kez inildi", n)
	}
}

// Kullanıcısız DB'de yalnız-kayıt 400; kullanıcı OLUŞTURMA dalına da düşmez.
func TestSetDatabasePassword_YalnizKayit_KullaniciYok400(t *testing.T) {
	h, sahte, es := de_kur(t, nil)
	kod, g := de_istek(t, h, dp_bayi(t, 3), 301, map[string]any{"parola": "Bir-Parola-1", "kullanici": "c_kurban_yeni", "yalniz_kayit": true})
	if kod != http.StatusBadRequest {
		t.Fatalf("kod=%d govde=%v; 400 bekleniyordu", kod, g)
	}
	if len(es.cagri) != 0 || sahte.sayi() != 0 || sahte.parolaSayi() != 0 {
		t.Errorf("kullanıcısız DB'de MariaDB katmanına inildi: esitle=%v ekle=%d alter=%d", es.cagri, sahte.sayi(), sahte.parolaSayi())
	}
	if u := dp_dbUser(t, h.DB, 301); u != "" {
		t.Errorf("kayda kullanıcı yazıldı: %q", u)
	}
}

// Başka bayinin DB'si: rota kapısında 404, eşitleme denenmez (parola yoklama kapısı değil).
func TestSetDatabasePassword_YalnizKayit_BaskaBayi404(t *testing.T) {
	h, _, es := de_kur(t, nil)
	kod, _ := de_istek(t, h, dp_bayi(t, 2), 300, map[string]any{"parola": "Bir-Parola-1", "yalniz_kayit": true})
	if kod != http.StatusNotFound {
		t.Errorf("kod=%d; 404 bekleniyordu", kod)
	}
	if len(es.cagri) != 0 {
		t.Errorf("başka bayinin DB'si için eşitleme denendi: %v", es.cagri)
	}
}

// Regresyon: bayrak YOKKEN eski yol (ALTER USER) aynen çalışır, eşitleme çağrılmaz.
func TestSetDatabasePassword_BayrakYok_EskiYol(t *testing.T) {
	h, sahte, es := de_kur(t, nil)
	_, _ = de_istek(t, h, dp_bayi(t, 3), 300, map[string]any{"parola": "Yeni-Parola-1"})
	if sahte.parolaSayi() != 1 || len(es.cagri) != 0 {
		t.Errorf("bayraksız istek: alter=%d esitle=%v; alter=1 esitle=0 bekleniyordu", sahte.parolaSayi(), es.cagri)
	}
	// Bayraksız yolda asgari uzunluk kuralı sürer.
	if kod, _ := de_istek(t, h, dp_bayi(t, 3), 300, map[string]any{"parola": "kisa"}); kod != http.StatusBadRequest {
		t.Errorf("bayraksız kısa parola: kod=%d; 400 bekleniyordu", kod)
	}
}

// Hosting sahibi (müşteri) de KENDİ DB'sinde yalnız-kayıt eşitlemesi yapar (uç
// DBSahipligiMusteri); başka domainin DB'sine 404, eşitleme çağrılmaz.
func TestSetDatabasePassword_YalnizKayit_Musteri(t *testing.T) {
	h, sahte, es := de_kur(t, nil)
	m := dm_musteri(t, 900, 30)
	if kod, g := de_istek(t, h, m, 300, map[string]any{"parola": "kisa", "yalniz_kayit": true}); kod != http.StatusOK {
		t.Fatalf("müşteri kendi DB'si: kod=%d govde=%v; 200 bekleniyordu", kod, g)
	}
	if kod, _ := de_istek(t, h, m, 200, map[string]any{"parola": "kisa", "yalniz_kayit": true}); kod != http.StatusNotFound {
		t.Errorf("müşteri başka domainin DB'si: kod=%d; 404 bekleniyordu", kod)
	}
	if len(es.cagri) != 1 || es.cagri[0] != "30:kurban_db:kisa" {
		t.Errorf("eşitleme çağrıları: %v (yalnız 30:kurban_db beklenir)", es.cagri)
	}
	if n := sahte.parolaSayi(); n != 0 {
		t.Errorf("yalnız-kayıt modunda ALTER USER yoluna %d kez inildi", n)
	}
}
