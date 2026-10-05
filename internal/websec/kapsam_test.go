package websec

// kapsam_test.go — Website Security Monitor bayi kapsamı (sahte DB, ağ/exec yok).
//
// Kural: bayi YALNIZ domains.reseller_id = kendi ResellerID'si olan domainleri görür;
// sayaç, liste, hata metni ve durum alanı dahil sunucu geneli bilgi bayiye gitmez.
// Kök (yönetici) görünümü DEĞİŞMEZ — regresyon testleri aşağıda.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"girginospanel/internal/auth"
	"girginospanel/internal/sahtedb"
)

var (
	kokJeton  = &auth.Claims{UserID: 1, Username: "root", Role: "admin"}
	bayiJeton = &auth.Claims{UserID: 7, Username: "bayi7", Role: "reseller", ResellerID: 7}
)

// kimlikKur — claimsAl dikişini test süresince c'ye bağlar.
func kimlikKur(t *testing.T, c *auth.Claims) {
	t.Helper()
	eski := claimsAl
	t.Cleanup(func() { claimsAl = eski })
	claimsAl = func(*http.Request) *auth.Claims { return c }
}

func istek(t *testing.T, h http.HandlerFunc, yontem, yol, govde string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(yontem, yol, strings.NewReader(govde)))
	return rr
}

func jsonAnahtarlar(t *testing.T, govde []byte) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(govde, &m); err != nil {
		t.Fatalf("yanıt JSON değil: %v: %s", err, govde)
	}
	return m
}

func TestKapsamAl(t *testing.T) {
	cases := []struct {
		ad    string
		c     *auth.Claims
		kosul string
		args  []any
	}{
		{"kök", kokJeton, "", nil},
		{"bayi", bayiJeton, " AND d.reseller_id=?", []any{int64(7)}},
		{"kimliksiz bayi", &auth.Claims{UserID: 7, Role: "reseller"}, " AND 1=0", nil},
		{"rolü boş, rid'li jeton", &auth.Claims{UserID: 7, ResellerID: 7}, " AND d.reseller_id=?", []any{int64(7)}},
		{"admin rolü + rid (bayi sayılır)", &auth.Claims{UserID: 9, Role: "admin", ResellerID: 9}, " AND d.reseller_id=?", []any{int64(9)}},
		{"kimlik yok", nil, " AND 1=0", nil},
		{"tanınmayan rol", &auth.Claims{UserID: 5, Role: "musteri"}, " AND 1=0", nil},
	}
	for _, c := range cases {
		kimlikKur(t, c.c)
		k := kapsamAl(httptest.NewRequest("GET", "/", nil))
		kosul, args := k.kosul()
		if kosul != c.kosul || !reflect.DeepEqual(args, c.args) {
			t.Errorf("%s: kosul=%q args=%v, beklenen %q %v", c.ad, kosul, args, c.kosul, c.args)
		}
		if (c.kosul == "") == k.bayi {
			t.Errorf("%s: bayi=%v yanlış", c.ad, k.bayi)
		}
	}
}

// Bayiye ham SQL hatası dönmez; kökün yanıtı değişmedi.
func TestKapsamHataBayiyeHamDegil(t *testing.T) {
	hata := errors.New("Error 1146: Table 'gpanel.cp_websec_findings' doesn't exist")
	rr := httptest.NewRecorder()
	kapsam{bayi: true, rid: 7}.hata(rr, hata)
	if rr.Code != 500 || strings.Contains(rr.Body.String(), "cp_websec") || !strings.Contains(rr.Body.String(), "işlem tamamlanamadı") {
		t.Fatalf("bayi genel hata almalı: %d %s", rr.Code, rr.Body)
	}
	rr = httptest.NewRecorder()
	kapsam{}.hata(rr, hata)
	if rr.Code != 500 || !strings.Contains(rr.Body.String(), "cp_websec_findings") {
		t.Fatalf("kök ham hatayı görmeye devam etmeli: %d %s", rr.Code, rr.Body)
	}
}

// Findings: sayı ve liste sorgularının İKİSİ de kapsam süzgecini taşır — biri eksik
// kalırsa toplam başka kiracıların bulgu sayısını sızdırır.
func TestFindingsBayiIkiSorguKapsamli(t *testing.T) {
	sd, db := sahtedb.Yeni()
	sd.Satirlar("SELECT COUNT(*) FROM cp_websec_findings", []any{int64(0)})
	kimlikKur(t, bayiJeton)
	rr := istek(t, (&Handler{DB: db}).Findings, "GET", "/websec/findings?severity=high&domain_id=11&q=x", "")
	if rr.Code != 200 {
		t.Fatalf("200 beklenirdi: %d %s", rr.Code, rr.Body)
	}
	for _, parca := range []string{
		"SELECT COUNT(*) FROM cp_websec_findings f LEFT JOIN domains d ON d.id=f.domain_id WHERE 1=1 AND d.id IS NOT NULL AND d.reseller_id=? AND f.severity=?",
		"AND d.id IS NOT NULL AND d.reseller_id=? AND f.severity=? AND f.domain_id=? AND (f.package_name LIKE ? OR f.cve_id LIKE ? OR f.title LIKE ? OR d.alan_adi LIKE ?) ORDER BY FIELD",
	} {
		if !sd.Sorgulandi(parca) {
			t.Fatalf("sorgu kapsam süzgeci taşımalı: %q", parca)
		}
	}
	if got := strings.TrimSpace(rr.Body.String()); got != `{"toplam":0,"sayfa":1,"sayfa_boyut":50,"items":[]}` {
		t.Fatalf("yanıt biçimi: %s", got)
	}
}

// Kök regresyonu: Findings süzgeçsiz.
func TestFindingsKokSuzgecsiz(t *testing.T) {
	sd, db := sahtedb.Yeni()
	sd.Satirlar("SELECT COUNT(*) FROM cp_websec_findings", []any{int64(0)})
	kimlikKur(t, kokJeton)
	rr := istek(t, (&Handler{DB: db}).Findings, "GET", "/websec/findings?severity=high", "")
	if rr.Code != 200 || sd.Sorgulandi("reseller_id") || sd.Sorgulandi("1=0") ||
		!sd.Sorgulandi("WHERE 1=1 AND d.id IS NOT NULL AND f.severity=?") {
		t.Fatalf("kök tüm bulguları görmeli (süzgeçsiz): %d %s", rr.Code, rr.Body)
	}
}

// Kimliksiz bayi: hiçbir şey (1=0), başkasınınki asla.
func TestFindingsKimliksizBayiHicbirSey(t *testing.T) {
	sd, db := sahtedb.Yeni()
	sd.Satirlar("SELECT COUNT(*) FROM cp_websec_findings", []any{int64(0)})
	kimlikKur(t, &auth.Claims{UserID: 7, Role: "reseller"})
	rr := istek(t, (&Handler{DB: db}).Findings, "GET", "/websec/findings", "")
	if rr.Code != 200 || !sd.Sorgulandi("WHERE 1=1 AND d.id IS NOT NULL AND 1=0") || sd.Sorgulandi("reseller_id") {
		t.Fatalf("kimliksiz bayi 1=0 almalı: %d %s", rr.Code, rr.Body)
	}
}

// Status (bayi): sayaçlar ve son tarama kendi domainlerinden; sunucu geneli durum
// satırı (cp_websec_status) HİÇ okunmaz; sunucu geneli alanlar yanıtta YOK.
func TestStatusBayiKapsamliVeAlanlarYok(t *testing.T) {
	sd, db := sahtedb.Yeni()
	sd.Satirlar("LEFT JOIN cp_websec_domain_tarama t ON t.domain_id = d.id",
		[]any{int64(10), "2026-09-29 10:00:00"},
		[]any{int64(11), "2026-09-29 12:30:00"},
		[]any{int64(12), ""})
	sd.Satirlar("SELECT f.severity, COUNT(*)", []any{"high", int64(2)}, []any{"low", int64(1)})
	kimlikKur(t, bayiJeton)
	rr := istek(t, (&Handler{DB: db}).Status, "GET", "/websec/status", "")
	if rr.Code != 200 {
		t.Fatalf("200 beklenirdi: %d %s", rr.Code, rr.Body)
	}
	for _, parca := range []string{
		"LEFT JOIN cp_websec_domain_tarama t ON t.domain_id = d.id\n\t\t WHERE 1=1 AND d.reseller_id=?",
		"JOIN domains d ON d.id = f.domain_id\n\t\t WHERE 1=1 AND d.reseller_id=?",
	} {
		if !sd.Sorgulandi(parca) {
			t.Fatalf("durum sorgusu kapsam süzgeci taşımalı: %q", parca)
		}
	}
	if sd.Sorgulandi("cp_websec_status") {
		t.Fatal("bayi durumu sunucu geneli cp_websec_status satırını okumamalı")
	}
	m := jsonAnahtarlar(t, rr.Body.Bytes())
	for _, yok := range []string{"scanned_apps", "duration_ms", "last_error", "next_estimate", "app_sayaclari"} {
		if _, ok := m[yok]; ok {
			t.Errorf("bayi yanıtında %q anahtarı OLMAMALI: %s", yok, rr.Body)
		}
	}
	var y bayiDurumYanit
	_ = json.Unmarshal(rr.Body.Bytes(), &y)
	if y.TotalFindings != 3 || y.High != 2 || y.Low != 1 || y.LastRun == nil || *y.LastRun != "2026-09-29 12:30:00" ||
		y.LastSuccess == nil || *y.LastSuccess != *y.LastRun || y.Running {
		t.Fatalf("bayi durumu kendi domainlerinden: %s", rr.Body)
	}
}

// Kök regresyonu: durum kartı sunucu geneli satırdan, tüm alanlarıyla.
func TestStatusKokDegismedi(t *testing.T) {
	sd, db := sahtedb.Yeni()
	sd.Satirlar("FROM cp_websec_status WHERE id=1", []any{int64(0), "2026-09-29 10:00:00", "2026-09-29 10:05:00",
		int64(4), int64(1), int64(1), int64(1), int64(1), int64(12), int64(3400), "feed zaman aşımı"})
	kimlikKur(t, kokJeton)
	rr := istek(t, (&Handler{DB: db}).Status, "GET", "/websec/status", "")
	m := jsonAnahtarlar(t, rr.Body.Bytes())
	for _, anahtar := range []string{"running", "last_run", "last_success", "total_findings", "scanned_apps", "duration_ms",
		"last_error", "next_estimate", "ekosistemler", "app_sayaclari"} {
		if _, ok := m[anahtar]; !ok {
			t.Errorf("kök yanıtında %q olmalı: %s", anahtar, rr.Body)
		}
	}
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"scanned_apps":12`) || sd.Sorgulandi("reseller_id") {
		t.Fatalf("kök durumu değişmemeli: %d %s", rr.Code, rr.Body)
	}
}

// taramaKur — TaraDomains'in bellek durumunu (kosuyor + taranEden) test süresince
// kurar; t.Cleanup önceki durumu geri yükler. Boş küme + kosu = tam tarama.
func taramaKur(t *testing.T, kosu bool, ids ...int64) {
	t.Helper()
	kosmaMu.Lock()
	eskiKosu, eskiKume := kosuyor, taranEden
	kosuyor = kosu
	taranEden = map[int64]bool{}
	for _, id := range ids {
		taranEden[id] = true
	}
	kosmaMu.Unlock()
	t.Cleanup(func() {
		kosmaMu.Lock()
		kosuyor, taranEden = eskiKosu, eskiKume
		kosmaMu.Unlock()
	})
}

// 🔴 Durum alanı sızmaz: başka kiracının domaini (11, bayi 8) kısmi taranırken bayi 7
// kendi domaininde (10) "taraniyor", durum kartında running=true GÖRMEZ — görseydi
// başkasının tarama zamanını öğrenirdi. Kendi domaini taranırken ve tam taramada (boş
// küme = tüm domainler, onunkiler dahil) görür; tarama yokken görmez.
func TestTaraniyorDurumuYalnizKendiDomaini(t *testing.T) {
	for _, c := range []struct {
		ad      string
		kosu    bool
		taranan []int64
		durum   string
		running bool
	}{
		{"başka kiracının kısmi taraması", true, []int64{11}, "beklemede", false},
		{"kendi domaininin kısmi taraması", true, []int64{10}, "taraniyor", true},
		{"tam tarama", true, nil, "taraniyor", true},
		{"tarama yok", false, nil, "beklemede", false},
	} {
		kuyrukKur(t)
		taramaKur(t, c.kosu, c.taranan...)
		kimlikKur(t, bayiJeton)
		sd, db := sahtedb.Yeni()
		sd.Satirlar("COALESCE(a.app_type,'')", []any{int64(10), "on.test", "", "", "", int64(0), int64(0), "", "", int64(0)})
		sd.Satirlar("LEFT JOIN cp_websec_domain_tarama t ON t.domain_id = d.id", []any{int64(10), ""})
		h := &Handler{DB: db}

		rr := istek(t, h.Apps, "GET", "/websec/apps", "")
		var a envanterYanit
		if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &a) != nil || len(a.Items) != 1 || a.Items[0].Durum != c.durum {
			t.Fatalf("%s: apps durumu %q beklenirdi: %d %s", c.ad, c.durum, rr.Code, rr.Body)
		}
		rr = istek(t, h.Status, "GET", "/websec/status", "")
		var y bayiDurumYanit
		if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &y) != nil || y.Running != c.running {
			t.Fatalf("%s: running=%v beklenirdi: %d %s", c.ad, c.running, rr.Code, rr.Body)
		}
	}
}

// Apps: bayide d.reseller_id=? süzgeci; domain_id süzgeci; geçersiz domain_id hiçbir
// satır; kökte süzgeç yok.
func TestAppsKapsamVeDomainSuzgeci(t *testing.T) {
	sd, db := sahtedb.Yeni()
	kimlikKur(t, bayiJeton)
	h := &Handler{DB: db}
	if rr := istek(t, h.Apps, "GET", "/websec/apps?domain_id=11", ""); rr.Code != 200 ||
		!sd.Sorgulandi("WHERE 1=1 AND d.reseller_id=? AND d.id=?") {
		t.Fatalf("bayi apps kapsam + domain süzgeci: %d %s", rr.Code, rr.Body)
	}
	if rr := istek(t, h.Apps, "GET", "/websec/apps?domain_id=abc", ""); rr.Code != 200 ||
		!sd.Sorgulandi("WHERE 1=1 AND d.reseller_id=? AND 1=0") {
		t.Fatalf("geçersiz domain_id hiçbir satır döndürmemeli: %d %s", rr.Code, rr.Body)
	}

	sd2, db2 := sahtedb.Yeni()
	kimlikKur(t, kokJeton)
	rr := istek(t, (&Handler{DB: db2}).Apps, "GET", "/websec/apps", "")
	if rr.Code != 200 || sd2.Sorgulandi("reseller_id") || !sd2.Sorgulandi("WHERE 1=1\n") {
		t.Fatalf("kök apps süzgeçsiz: %d %s", rr.Code, rr.Body)
	}
	if got := strings.TrimSpace(rr.Body.String()); got != `{"toplam":0,"items":[],"desteklenmeyen":0}` {
		t.Fatalf("yanıt biçimi: %s", got)
	}
}

// Keşif: dolu liste → WHERE id IN (...); boş → tüm domainler (tam tarama).
func TestKesifSorgusu(t *testing.T) {
	q, args := kesifSorgusu([]int64{10, 11})
	if !strings.Contains(q, "FROM domains WHERE id IN (?,?) ORDER BY alan_adi") || !reflect.DeepEqual(args, []any{int64(10), int64(11)}) {
		t.Fatalf("kısmi keşif: %q %v", q, args)
	}
	if q, args := kesifSorgusu(nil); strings.Contains(q, "WHERE") || args != nil {
		t.Fatalf("tam keşif süzgeçsiz olmalı: %q %v", q, args)
	}
	// Üç tarayıcının her biri kısmi keşifte süzgeçli sorgu atar (satırsız sahte DB:
	// exec/dosya okuma hiç koşmaz).
	for ad, bul := range map[string]func(*sql.DB) error{
		"wp":   func(db *sql.DB) error { _, e := WPKurulumlariBul(t.Context(), db, []int64{5}); return e },
		"node": func(db *sql.DB) error { _, e := NodejsKurulumlariBul(t.Context(), db, []int64{5}); return e },
		"php":  func(db *sql.DB) error { _, e := PhpKurulumlariBul(t.Context(), db, []int64{5}); return e },
	} {
		sd, db := sahtedb.Yeni()
		if err := bul(db); err != nil || !sd.Sorgulandi("SELECT id, sistem_kullanici, alan_adi FROM domains WHERE id IN (?) ORDER BY alan_adi") {
			t.Errorf("%s keşfi WHERE id IN taşımalı (hata=%v)", ad, err)
		}
	}
}
