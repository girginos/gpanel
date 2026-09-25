package antivirus

// durum_kapsam_test.go — bayi kapsamı (AV-DURUM-HOST-META / YK-03):
// GET /domains/{id}/antivirus, alan adının KENDİ taraması yokken kökün sunucu
// geneli (domain_id=0) taramasının meta verisini (taranan = tüm kiracıların
// dosya toplamı, id, zamanlar) bayiye/müşteriye DÖNDÜRMEZ; yönetici görmeye
// devam eder. Ek: imza güncellemesi (sunucu geneli freshclam) yalnız yöneticiye.
//
// Gerçek RequireAuth + imzalı jetonlar; DB sahtedb (sorgu parçası → hazır satır).

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/auth"
	"girginospanel/internal/middleware"
	"girginospanel/internal/sahtedb"
)

var avt_sir = []byte("antivirus-kapsam-test-anahtari-0123456789")

func avt_tokenlar(t *testing.T) (admin, bayi, musteri string) {
	t.Helper()
	var err error
	if admin, err = auth.IssueAt(avt_sir, 3600, 1, "root", "admin", 0, ""); err != nil {
		t.Fatal(err)
	}
	if bayi, err = auth.IssueResellerAt(avt_sir, 3600, 7, "bayi7", 7, 0, ""); err != nil {
		t.Fatal(err)
	}
	if musteri, _, err = auth.GenerateMusteri(avt_sir, auth.MusteriClaims{
		FTPHesapID: 420, DomainID: 42, Kullanici: "m", AlanAdi: "bayialan.test"}, 3600); err != nil {
		t.Fatal(err)
	}
	return
}

// Kökün sunucu geneli taramasının işaretleri: bunlar bayi/müşteri yanıtında GÖRÜNMEMELİ.
const (
	avt_hostTaranan = 987654 // /home altındaki TÜM kiracıların toplam dosyası
	avt_hostID      = 900
	avt_hostBas     = "2026-09-20 03:00:00"
)

// avt_db — alan adı 42 (bayi 7'nin). kendiTarama: 42'nin kendi taraması var mı;
// bulgu: 42'de aktif bulgu var mı. Kökün host-geneli taraması HER ZAMAN var.
func avt_db(kendiTarama, bulgu bool) *sql.DB {
	sd, db := sahtedb.Yeni()
	sd.Satirlar("COALESCE(is_demo,0) FROM domains", []any{"c_bayialan", int64(0)})
	if kendiTarama {
		sd.Satirlar("FROM av_taramalar WHERE domain_id=? ORDER BY",
			[]any{int64(55), "bitti", "gosp", int64(120), int64(0), "2026-09-24 10:00:00", "2026-09-24 10:01:00"})
	}
	sd.Satirlar("domain_id=0 AND durum='tamam'",
		[]any{int64(avt_hostID), "tamam", "gosp", int64(avt_hostTaranan), int64(3), avt_hostBas, "2026-09-20 04:12:00"})
	if bulgu {
		sd.Satirlar("FROM av_bulgular",
			[]any{"/home/c_bayialan/public_html/x.php", "PHP.Webshell.EvalBase64", "gosp/kritik", int64(0)})
	}
	return db
}

func avt_durum(t *testing.T, db *sql.DB, tok string) (map[string]any, string) {
	t.Helper()
	r := chi.NewRouter()
	r.With(middleware.RequireAuth(avt_sir)).Get("/domains/{id}/antivirus", (&Handlers{DB: db}).Durum)
	req := httptest.NewRequest(http.MethodGet, "/domains/42/antivirus", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /domains/42/antivirus: %d %s", rec.Code, rec.Body.String())
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("yanıt çözülemedi: %v", err)
	}
	return m, rec.Body.String()
}

func avt_hostIzYok(t *testing.T, rol, govde string) {
	t.Helper()
	for _, iz := range []string{"987654", avt_hostBas, `"id":900`} {
		if strings.Contains(govde, iz) {
			t.Errorf("%s yanıtında kökün sunucu geneli taramasının izi (%q) var: %s", rol, iz, govde)
		}
	}
}

// Taban davranışı (yeniden üretim): bayi/müşteri kendi taraması olmayan alan adında
// son_tarama.taranan=987654 (sunucu toplamı) alıyordu.
func TestDurum_HostTaramasiBayiyeMusteriyeSizmaz(t *testing.T) {
	_, bayi, musteri := avt_tokenlar(t)
	for rol, tok := range map[string]string{"bayi": bayi, "müşteri": musteri} {
		m, govde := avt_durum(t, avt_db(false, false), tok)
		if m["son_tarama"] != nil {
			t.Errorf("%s: kendi taraması olmayan alan adında son_tarama boş olmalı (\"henüz taranmadı\"), gelen: %v", rol, m["son_tarama"])
		}
		avt_hostIzYok(t, rol, govde)
	}
}

// Bulgu var ama kendi taraması yok: bulgular görünür (kendi verisi), host meta görünmez.
func TestDurum_BulguVarKendiTaramaYok_BayiHostMetaGormez(t *testing.T) {
	_, bayi, _ := avt_tokenlar(t)
	m, govde := avt_durum(t, avt_db(false, true), bayi)
	st, _ := m["son_tarama"].(map[string]any)
	if st == nil {
		t.Fatalf("bulgu varken son_tarama dolu olmalı (bulgular gösterilsin): %s", govde)
	}
	if st["enfekte"] != float64(1) {
		t.Errorf("enfekte = kendi bulgu sayısı (1) olmalı: %v", st["enfekte"])
	}
	if _, ok := st["taranan"]; ok {
		t.Errorf("taranan (sunucu toplamı) bayiye dönmemeli: %v", st)
	}
	avt_hostIzYok(t, "bayi", govde)
	if b, _ := m["bulgular"].([]any); len(b) != 1 {
		t.Errorf("kendi bulgusu görünmeli: %v", m["bulgular"])
	}
}

// Yönetici davranışı DEĞİŞMEDİ: kendi taraması yoksa son host-geneli tarama.
func TestDurum_YoneticiHostTaramasiniGormeyeDevamEder(t *testing.T) {
	admin, _, _ := avt_tokenlar(t)
	m, govde := avt_durum(t, avt_db(false, false), admin)
	st, _ := m["son_tarama"].(map[string]any)
	if st == nil || st["taranan"] != float64(avt_hostTaranan) || st["id"] != float64(avt_hostID) {
		t.Fatalf("yönetici host-geneli taramayı görmeli (taranan=%d id=%d): %s", avt_hostTaranan, avt_hostID, govde)
	}
}

// Alan adının KENDİ taraması herkese (bayi, müşteri, yönetici) aynen görünür.
func TestDurum_KendiTaramasiHerRoldeGorunur(t *testing.T) {
	admin, bayi, musteri := avt_tokenlar(t)
	for rol, tok := range map[string]string{"yönetici": admin, "bayi": bayi, "müşteri": musteri} {
		m, govde := avt_durum(t, avt_db(true, false), tok)
		st, _ := m["son_tarama"].(map[string]any)
		if st == nil || st["taranan"] != float64(120) || st["id"] != float64(55) {
			t.Errorf("%s: kendi taraması (id=55 taranan=120) görünmeli: %s", rol, govde)
		}
		avt_hostIzYok(t, rol, govde)
	}
}

// İmza güncellemesi SUNUCU GENELİ bir eylemdir (freshclam tüm sunucunun imza
// veritabanını günceller): bayi ve müşteri kendi alan adı kimliğiyle bile
// TETİKLEYEMEZ (403, freshclam hiç çalışmaz); yönetici çalıştırır ve ham çıktıyı
// (ayna adresi, imza sürümleri) görür. Rota da AdminOnly (cmd/server rota testi);
// bu test handler içindeki ikinci kilidi sınar.
func TestImzaGuncelle_YalnizYonetici(t *testing.T) {
	admin, bayi, musteri := avt_tokenlar(t)
	dizin := t.TempDir()
	betik := filepath.Join(dizin, "freshclam")
	isaret := filepath.Join(dizin, "calisti")
	const iz = "ayna.sunucu-ic.ornek"
	if err := os.WriteFile(betik, []byte("#!/bin/sh\ntouch '"+isaret+"'\necho 'daily.cvd database is up-to-date (mirror: "+iz+")'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	eski := freshclamYolu
	t.Cleanup(func() { freshclamYolu = eski })
	freshclamYolu = betik

	cagir := func(tok string) (int, string) {
		t.Helper()
		r := chi.NewRouter()
		r.With(middleware.RequireAuth(avt_sir)).Post("/domains/{id}/antivirus/imza-guncelle", (&Handlers{}).ImzaGuncelle)
		req := httptest.NewRequest(http.MethodPost, "/domains/42/antivirus/imza-guncelle", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	for rol, tok := range map[string]string{"bayi": bayi, "müşteri": musteri} {
		kod, govde := cagir(tok)
		if kod != http.StatusForbidden {
			t.Errorf("%s: 403 beklenirdi (sunucu geneli eylem): %d %s", rol, kod, govde)
		}
		if strings.Contains(govde, iz) {
			t.Errorf("%s: ham freshclam çıktısı dönmemeli: %s", rol, govde)
		}
		if _, err := os.Stat(isaret); err == nil {
			t.Fatalf("%s: freshclam ÇALIŞMAMALIYDI", rol)
		}
	}
	kod, govde := cagir(admin)
	if kod != http.StatusOK {
		t.Fatalf("yönetici: 200 beklenirdi: %d %s", kod, govde)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(govde), &m); err != nil {
		t.Fatal(err)
	}
	if s, _ := m["cikti"].(string); !strings.Contains(s, iz) || m["ok"] != true {
		t.Errorf("yönetici freshclam'ı çalıştırıp ham çıktıyı görmeli: %v", m)
	}
	if _, err := os.Stat(isaret); err != nil {
		t.Error("yönetici isteğinde freshclam çalışmalıydı")
	}
}
