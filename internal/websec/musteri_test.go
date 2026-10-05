package websec

// musteri_test.go — hosting MÜŞTERİSİ güvenlik uçları (musteri.go). Gerçek SQL (geçici
// MariaDB; GOSP_TEST_GECICI_DSN yoksa DB'li testler atlanır) + geçici ev dizini.
// Tarama HİÇ koşmaz; kuyruk bellekte.

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/auth"
	"girginospanel/internal/middleware"
)

// musteriMux — rotaları main.go'daki gibi (MusteriKapi önde) chi'ye bağlar; {id} URL
// parametresi gerçek yönlendiriciden gelir.
func musteriMux(h *Handler) http.Handler {
	r := chi.NewRouter()
	g := r.With(MusteriKapi)
	g.Get("/domains/{id}/guvenlik", h.MusteriListe)
	g.Get("/domains/{id}/guvenlik/ozet", h.MusteriOzet)
	g.Get("/domains/{id}/guvenlik/bulgular", h.MusteriBulgular)
	g.Post("/domains/{id}/guvenlik/tara", h.MusteriTara)
	return r
}

func musteriIstek(t *testing.T, h *Handler, yontem, yol, govde string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	musteriMux(h).ServeHTTP(rr, httptest.NewRequest(yontem, yol, strings.NewReader(govde)))
	return rr
}

// musteriKur — dikişleri test süresince müşteri jetonuna (nil = müşteri jetonu yok)
// bağlar. aktör: middleware.Aktor'un müşteri dalı (adına oturumda gerçek aktör).
func musteriKur(t *testing.T, mc *auth.MusteriClaims) {
	t.Helper()
	eskiM, eskiA := musteriAl, aktorAl
	t.Cleanup(func() { musteriAl, aktorAl = eskiM, eskiA })
	musteriAl = func(*http.Request) *auth.MusteriClaims { return mc }
	aktorAl = func(*http.Request) (int64, string) {
		switch {
		case mc == nil:
			return 0, "anonim"
		case mc.Adina():
			return mc.AktorUID, middleware.AdinaAktorAdi(mc.AktorAd, mc.DomainID)
		default:
			return 0, "musteri:" + itoa(mc.DomainID)
		}
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

var (
	musteri10 = &auth.MusteriClaims{DomainID: 10, Kullanici: "on"}
	musteri11 = &auth.MusteriClaims{DomainID: 11, Kullanici: "onbir"}
	adina10   = &auth.MusteriClaims{DomainID: 10, AktorUID: 7, AktorAd: "bayi7", AktorRol: "reseller", AktorRID: 7}
)

// musteriTohum — ekBulguTohumla + çeşitli bulgu biçimleri: domain 10 ana (high), ek1
// (high), domain 11'in ek2'si (başkasının). Ek olarak d10'a düzeltmeli/düzeltmesiz tema,
// npm ve WPV kimlikli bulgu.
func musteriTohum(t *testing.T, db *sql.DB) {
	t.Helper()
	ekBulguTohumla(t, db)
	ek := func(dom int64, yol, paket, surum, cve, sev, baslik, fixed string, ekID any) {
		wsExec(t, db, `INSERT INTO cp_websec_findings
			(domain_id, app_type, install_path, package_name, installed_version, cve_id, severity, cvss, title, fixed_in, source, first_seen, last_seen, ek_alan_id)
			VALUES (?, 'wordpress', ?, ?, ?, ?, ?, 7.5, ?, ?, 'wpvulnerability.net', NOW(), NOW(), ?)`,
			dom, yol, paket, surum, cve, sev, baslik, fixed, ekID)
	}
	ek(10, "/home/on/public_html#theme:tema-x", "tema-x", "1.0", "WPV-ABC123", "critical", "Tema açığı", "", nil)
	ek(10, "/home/on/public_html#plugin:eklenti-y", "eklenti-y", "2.0", "CVE-2026-2000", "medium", "Eklenti açığı", "2.1", nil)
	wsExec(t, db, `INSERT INTO cp_websec_findings
		(domain_id, app_type, install_path, package_name, installed_version, cve_id, severity, cvss, title, fixed_in, source, first_seen, last_seen, ek_alan_id)
		VALUES (10, 'nodejs', '/home/on/public_html#npm:lodash', 'lodash', '4.0.0', 'GHSA-aaaa-bbbb-cccc', 'low', NULL, 'npm açığı', '4.17.21', 'osv.dev', NOW(), NOW(), NULL)`)
}

type musteriListeTest struct {
	Ozet  musteriOzet      `json:"ozet"`
	Tara  musteriTaraHakki `json:"tara"`
	Items []musteriSatir   `json:"items"`
}

func musteriListeAl(t *testing.T, h *Handler, yol string) (musteriListeTest, string) {
	t.Helper()
	rr := musteriIstek(t, h, "GET", yol, "")
	if rr.Code != 200 {
		t.Fatalf("%s: %d %s", yol, rr.Code, rr.Body)
	}
	var l musteriListeTest
	if err := json.Unmarshal(rr.Body.Bytes(), &l); err != nil {
		t.Fatal(err)
	}
	return l, rr.Body.String()
}

// Liste kapsamı: ana + KENDİ ek alanları (docroot'u olmayan ek3 dahil); başka hesabın
// ek alanı/ana adı yok. Önem sayaçları kendi bulgularından; durum + ek işareti.
func TestMusteri_ListeKapsami(t *testing.T) {
	db, _ := ekOrtam(t)
	musteriTohum(t, db)
	kuyrukKur(t)
	saatKur(t)
	musteriKur(t, musteri10)
	h := &Handler{DB: db}

	l, _ := musteriListeAl(t, h, "/domains/10/guvenlik")
	var adlar []string
	satir := map[string]musteriSatir{}
	for _, s := range l.Items {
		adlar = append(adlar, s.AlanAdi)
		satir[s.AlanAdi] = s
	}
	sort.Strings(adlar)
	if strings.Join(adlar, ",") != "ek1.test,ek3.test,on.test" {
		t.Fatalf("yalnız kendi hesabının alan adları: %v", adlar)
	}
	if satir["on.test"].Ek || !satir["ek1.test"].Ek || satir["ek1.test"].EkAlanID != 1 {
		t.Fatalf("ek işareti/kimliği: %+v %+v", satir["on.test"], satir["ek1.test"])
	}
	// on.test: plugin p (high) + tema critical + eklenti medium + npm low = 4 bulgu.
	on := satir["on.test"]
	if on.Onem.Critical != 1 || on.Onem.High != 1 || on.Onem.Medium != 1 || on.Onem.Low != 1 || on.BulguSayisi != 4 || on.Durum != "acik" {
		t.Fatalf("on.test sayaçları: %+v", on)
	}
	if on.AppType != "wordpress" || on.UygulamaSayisi != 1 || on.PaketSayisi != 1 {
		t.Fatalf("on.test uygulama özeti: %+v", on)
	}
	if e := satir["ek1.test"]; e.BulguSayisi != 1 || e.Onem.High != 1 || e.Durum != "acik" {
		t.Fatalf("ek1: %+v", e)
	}
	if e := satir["ek3.test"]; e.Durum != "beklemede" || e.BulguSayisi != 0 || e.AppType != "" {
		t.Fatalf("taranmamış ek alan beklemede: %+v", e)
	}
	if l.Ozet.Toplam != 5 || l.Ozet.Critical != 1 || l.Ozet.High != 2 || l.Ozet.Durum != "acik" || l.Ozet.Hedef != 3 {
		t.Fatalf("özet: %+v", l.Ozet)
	}
	if l.Tara.SogumaDk != 15 || l.Tara.GunlukSinir != musteriGunlukSinir || l.Tara.GunlukKalan != musteriGunlukSinir {
		t.Fatalf("tara hakkı: %+v", l.Tara)
	}

	// Başka hesap: yalnız kendisi (+ek2).
	musteriKur(t, musteri11)
	l, _ = musteriListeAl(t, h, "/domains/11/guvenlik")
	adlar = nil
	for _, s := range l.Items {
		adlar = append(adlar, s.AlanAdi)
	}
	sort.Strings(adlar)
	if strings.Join(adlar, ",") != "ek2.test,onbir.test" || l.Ozet.Toplam != 1 {
		t.Fatalf("hesap 11: %v %+v", adlar, l.Ozet)
	}
}

func TestMusteri_OzetTemizVeBeklemede(t *testing.T) {
	db, _ := ekOrtam(t)
	kuyrukKur(t)
	musteriKur(t, &auth.MusteriClaims{DomainID: 12})
	h := &Handler{DB: db}
	rr := musteriIstek(t, h, "GET", "/domains/12/guvenlik/ozet", "")
	var oz musteriOzet
	_ = json.Unmarshal(rr.Body.Bytes(), &oz)
	if rr.Code != 200 || oz.Durum != "beklemede" || oz.Toplam != 0 || oz.Hedef != 1 {
		t.Fatalf("hiç taranmamış: %d %+v", rr.Code, oz)
	}
	// Taranmış + uygulama yok → desteklenmiyor; uygulama + bulgu yok → temiz.
	wsExec(t, db, `INSERT INTO cp_websec_domain_tarama (domain_id, son_tarama) VALUES (12, NOW())`)
	rr = musteriIstek(t, h, "GET", "/domains/12/guvenlik/ozet", "")
	_ = json.Unmarshal(rr.Body.Bytes(), &oz)
	if oz.Durum != "desteklenmiyor" || oz.SonTarama == "" {
		t.Fatalf("taranmış, uygulama yok: %+v", oz)
	}
	envanterYaz(t.Context(), db, 12, 0, "wordpress", "/x/oniki/public_html", "6.5", 3, 0)
	rr = musteriIstek(t, h, "GET", "/domains/12/guvenlik/ozet", "")
	_ = json.Unmarshal(rr.Body.Bytes(), &oz)
	if oz.Durum != "temiz" {
		t.Fatalf("uygulama + bulgu yok = temiz: %+v", oz)
	}
}

// Yetki matrisi: kendi domain/ek alan 200; yabancı domain/ek alan = olmayanla AYNI 404;
// kök/bayi doğrudan jeton (müşteri jetonu yok) 403; adına oturum normal müşteri gibi.
func TestMusteri_YetkiMatrisi(t *testing.T) {
	db, _ := ekOrtam(t)
	musteriTohum(t, db)
	kuyrukKur(t)
	saatKur(t)
	h := &Handler{DB: db}

	musteriKur(t, musteri10)
	for _, c := range []struct{ yontem, yol, govde string }{
		{"GET", "/domains/10/guvenlik", ""},
		{"GET", "/domains/10/guvenlik/ozet", ""},
		{"GET", "/domains/10/guvenlik/bulgular", ""},
		{"GET", "/domains/10/guvenlik/bulgular?ek_alan_id=1", ""},
		{"GET", "/domains/10/guvenlik/bulgular?ek_alan_id=0", ""},
		{"POST", "/domains/10/guvenlik/tara", `{"ek_alan_id":1}`},
	} {
		if rr := musteriIstek(t, h, c.yontem, c.yol, c.govde); rr.Code != 200 && rr.Code != 202 {
			t.Errorf("kendi %s %s: %d %s", c.yontem, c.yol, rr.Code, rr.Body)
		}
	}

	ayni := func(ad string, a, b *httptest.ResponseRecorder) {
		t.Helper()
		if a.Code != 404 || a.Code != b.Code || a.Body.String() != b.Body.String() {
			t.Errorf("%s: yabancı (%d %s) olmayanla (%d %s) aynı 404 olmalı", ad, a.Code, a.Body, b.Code, b.Body)
		}
	}
	for _, alt := range []string{"", "/ozet", "/bulgular"} {
		ayni("domain "+alt, musteriIstek(t, h, "GET", "/domains/11/guvenlik"+alt, ""), musteriIstek(t, h, "GET", "/domains/9999/guvenlik"+alt, ""))
	}
	ayni("tara domain", musteriIstek(t, h, "POST", "/domains/11/guvenlik/tara", ""), musteriIstek(t, h, "POST", "/domains/9999/guvenlik/tara", ""))
	// Kendi domainim + yabancı ek alan (ek2 d11'in) / olmayan ek alan.
	ayni("bulgular ek alan", musteriIstek(t, h, "GET", "/domains/10/guvenlik/bulgular?ek_alan_id=2", ""), musteriIstek(t, h, "GET", "/domains/10/guvenlik/bulgular?ek_alan_id=999", ""))
	ayni("tara ek alan", musteriIstek(t, h, "POST", "/domains/10/guvenlik/tara", `{"ek_alan_id":2}`), musteriIstek(t, h, "POST", "/domains/10/guvenlik/tara", `{"ek_alan_id":999}`))
	ayni("tara ek alan negatif", musteriIstek(t, h, "POST", "/domains/10/guvenlik/tara", `{"ek_alan_id":-1}`), musteriIstek(t, h, "POST", "/domains/10/guvenlik/tara", `{"ek_alan_id":999}`))
	if musteriIstek(t, h, "GET", "/domains/10/guvenlik/bulgular?ek_alan_id=abc", "").Code != 404 {
		t.Error("geçersiz ek_alan_id 404 olmalı")
	}

	// Kök/bayi doğrudan jeton: müşteri jetonu yok → 403 (kendi sayfaları /websec).
	for _, kim := range []*auth.Claims{kokJeton, bayiJeton} {
		kimlikKur(t, kim)
		musteriKur(t, nil)
		for _, yol := range []string{"/domains/10/guvenlik", "/domains/10/guvenlik/ozet", "/domains/10/guvenlik/bulgular"} {
			if rr := musteriIstek(t, h, "GET", yol, ""); rr.Code != 403 {
				t.Errorf("%s doğrudan jeton %s: %d (403 beklenir)", kim.Role, yol, rr.Code)
			}
		}
		if rr := musteriIstek(t, h, "POST", "/domains/10/guvenlik/tara", ""); rr.Code != 403 {
			t.Errorf("%s doğrudan jeton tara: %d", kim.Role, rr.Code)
		}
	}

	// Adına oturum (bayi → müşteri 10): normal müşteri gibi çalışır; yabancı domain 404.
	musteriKur(t, adina10)
	if l, _ := musteriListeAl(t, h, "/domains/10/guvenlik"); len(l.Items) != 3 {
		t.Errorf("adına oturum kendi hesabını görmeli: %+v", l.Items)
	}
	if rr := musteriIstek(t, h, "GET", "/domains/11/guvenlik", ""); rr.Code != 404 {
		t.Errorf("adına oturum yabancı domain: %d", rr.Code)
	}
}

// Sızıntı: yanıtlarda başka hesap/kök verisi, mutlak yol, kaynak, ham feed hatası,
// kuyruk toplamı/next_estimate yok; alan kümeleri sabit.
func TestMusteri_SizintiYok(t *testing.T) {
	db, ev := ekOrtam(t)
	musteriTohum(t, db)
	kuyrukKur(t)
	saatKur(t)
	musteriKur(t, musteri10)
	h := &Handler{DB: db}
	wsExec(t, db, `UPDATE cp_websec_status SET last_error='feed: https://wpvulnerability.net/gizli-url 503', total_findings=999, scanned_apps=77 WHERE id=1`)
	// Başka hesapları kuyruğa koy (kuyruk toplamı sızmasın).
	kuy.kokEkle([]int64{11, 12, -2})

	yasak := []string{
		"onbir.test", "oniki.test", "ek2.test", ev, "public_html", "install_path", "next_estimate",
		"last_error", "gizli-url", "wpvulnerability.net", "osv.dev", "\"source\"", "scanned_apps",
		"total_findings", "app_sayaclari", "reseller", "sistem_kullanici", "/home/",
	}
	govdeler := map[string]string{}
	for ad, yol := range map[string]string{"liste": "/domains/10/guvenlik", "ozet": "/domains/10/guvenlik/ozet", "bulgular": "/domains/10/guvenlik/bulgular"} {
		rr := musteriIstek(t, h, "GET", yol, "")
		if rr.Code != 200 {
			t.Fatalf("%s: %d %s", ad, rr.Code, rr.Body)
		}
		govdeler[ad] = rr.Body.String()
		for _, y := range yasak {
			if strings.Contains(rr.Body.String(), y) {
				t.Errorf("%s yanıtında %q sızıyor: %s", ad, y, rr.Body)
			}
		}
	}
	// Kuyrukta başkasının işleri var ama bu hesabın satırları "sirada" göstermemeli.
	l, _ := musteriListeAl(t, h, "/domains/10/guvenlik")
	for _, s := range l.Items {
		if s.Durum == "sirada" {
			t.Errorf("başkasının kuyruk kaydı hesabın satırına yansıdı: %+v", s)
		}
	}
	// Alan kümeleri sabit (yeni sızıntı alanı eklenirse test düşer).
	var ust map[string]json.RawMessage
	_ = json.Unmarshal([]byte(govdeler["liste"]), &ust)
	if got := anahtarKumesi(ust); got != "feed_uyari,items,ozet,tara" {
		t.Errorf("liste üst alanları: %s", got)
	}
	var oz map[string]json.RawMessage
	_ = json.Unmarshal([]byte(govdeler["ozet"]), &oz)
	if got := anahtarKumesi(oz); got != "critical,durum,feed_uyari,hedef_sayisi,high,low,medium,son_tarama,taraniyor,toplam" {
		t.Errorf("özet alanları: %s", got)
	}
	var bl struct{ Items []map[string]json.RawMessage }
	_ = json.Unmarshal([]byte(govdeler["bulgular"]), &bl)
	if len(bl.Items) == 0 {
		t.Fatal("bulgu yok")
	}
	for _, it := range bl.Items {
		if got := anahtarKumesi(it); !strings.HasPrefix(got, "alan_adi,app_type,") || strings.Contains(got, "install") || strings.Contains(got, "source") {
			t.Errorf("bulgu alanları: %s", got)
		}
	}
	// Hata gövdesi genel: bozuk DB bağlantısı ham SQL hatası göstermemeli.
	kapali := &Handler{DB: func() *sql.DB { d, _ := sql.Open("mysql", "x:y@tcp(127.0.0.1:1)/z"); return d }()}
	rr := musteriIstek(t, kapali, "GET", "/domains/10/guvenlik", "")
	if rr.Code != 500 || !strings.Contains(rr.Body.String(), "işlem tamamlanamadı") || strings.Contains(strings.ToLower(rr.Body.String()), "dial") {
		t.Errorf("hata gövdesi genel olmalı: %d %s", rr.Code, rr.Body)
	}
}

func anahtarKumesi(m map[string]json.RawMessage) string {
	var a []string
	for k := range m {
		a = append(a, k)
	}
	sort.Strings(a)
	return strings.Join(a, ",")
}

// Bulgu satırı biçimi + öneri (düzeltme sürümü var/yok) + kaynak bağlantısı + süzgeçler.
func TestMusteri_BulguSatiriVeOneri(t *testing.T) {
	db, _ := ekOrtam(t)
	musteriTohum(t, db)
	kuyrukKur(t)
	musteriKur(t, musteri10)
	h := &Handler{DB: db}
	get := func(sorgu string) []musteriBulgu {
		rr := musteriIstek(t, h, "GET", "/domains/10/guvenlik/bulgular"+sorgu, "")
		var y musteriBulguYanit
		if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &y) != nil {
			t.Fatalf("bulgular%s: %d %s", sorgu, rr.Code, rr.Body)
		}
		return y.Items
	}
	tum := get("?ek_alan_id=0")
	if len(tum) != 4 || tum[0].Onem != "critical" || tum[3].Onem != "low" {
		t.Fatalf("önem sırası critical→low: %+v", tum)
	}
	by := map[string]musteriBulgu{}
	for _, b := range tum {
		by[b.Paket] = b
	}
	if b := by["tema-x"]; b.OneriKod != "yama_yok_tema" || b.Duzeltme != "" || b.Baglanti != "" || b.CVE != "WPV-ABC123" || !strings.Contains(b.Oneri, "Henüz yama yok") {
		t.Errorf("düzeltmesiz tema: %+v", b)
	}
	if b := by["eklenti-y"]; b.OneriKod != "guncelle_eklenti" || b.Duzeltme != "2.1" || !strings.Contains(b.Oneri, "2.1") || b.Baglanti != "https://nvd.nist.gov/vuln/detail/CVE-2026-2000" || b.Surum != "2.0" || b.CVSS != 7.5 {
		t.Errorf("düzeltmeli eklenti: %+v", b)
	}
	if b := by["lodash"]; b.OneriKod != "guncelle_paket" || b.Baglanti != "https://github.com/advisories/GHSA-aaaa-bbbb-cccc" || b.AppType != "nodejs" {
		t.Errorf("npm paketi: %+v", b)
	}
	if b := by["p"]; b.OneriKod != "yama_yok_eklenti" || b.Ek || b.AlanAdi != "on.test" {
		t.Errorf("ana alan eklentisi: %+v", b)
	}
	if ek := get("?ek_alan_id=1"); len(ek) != 1 || !ek[0].Ek || ek[0].EkAlanID != 1 || ek[0].AlanAdi != "ek1.test" {
		t.Errorf("ek alan bulgusu: %+v", ek)
	}
	if hepsi := get(""); len(hepsi) != 5 {
		t.Errorf("süzgeçsiz: hesabın tümü (ana+ek), başkasınınki yok: %d", len(hepsi))
	}
	if k := get("?severity=critical"); len(k) != 1 || k[0].Paket != "tema-x" {
		t.Errorf("önem süzgeci: %+v", k)
	}
	if k := get("?severity=bogus"); len(k) != 0 {
		t.Errorf("geçersiz önem boş döner: %+v", k)
	}
	if k := get("?page_size=2&page=2"); len(k) != 2 {
		t.Errorf("sayfalama: %d", len(k))
	}
}

func TestMusteri_OneriVeBaglantiSaf(t *testing.T) {
	cases := []struct{ tur, paket, fix, kod string }{
		{"plugin", "a", "1.2", "guncelle_eklenti"}, {"plugin", "a", "", "yama_yok_eklenti"},
		{"theme", "a", "1.2", "guncelle_tema"}, {"theme", "a", "", "yama_yok_tema"},
		{"npm", "a", "1.2", "guncelle_paket"}, {"composer", "a", "", "yama_yok_paket"}, {"", "a", "", "yama_yok_paket"},
	}
	for _, c := range cases {
		kod, metin := oneriUret(c.tur, c.paket, c.fix)
		if kod != c.kod || metin == "" || (c.fix != "" && !strings.Contains(metin, c.fix)) {
			t.Errorf("%+v → %s %q", c, kod, metin)
		}
	}
	for id, beklenen := range map[string]string{
		"CVE-2026-12345":      "https://nvd.nist.gov/vuln/detail/CVE-2026-12345",
		"GHSA-abcd-1234-wxyz": "https://github.com/advisories/GHSA-abcd-1234-wxyz",
		"WPV-ABC":             "", "javascript:alert(1)": "", "CVE-2026-1/../x": "", "": "",
	} {
		if got := bulguBaglanti(id); got != beklenen {
			t.Errorf("baglanti(%q)=%q, beklenen %q", id, got, beklenen)
		}
	}
	for yol, beklenen := range map[string]string{
		"/home/x/public_html#plugin:abc": "plugin", "/h#theme:t": "theme", "/h#npm:l": "npm",
		"/h#composer:v/p": "composer", "/h": "", "/h#kotu:x": "", "/h#plugin": "",
	} {
		if got := paketTuru(yol); got != beklenen {
			t.Errorf("paketTuru(%q)=%q", yol, got)
		}
	}
}

func musteriDenetim(t *testing.T, db *sql.DB) (n int, hedef, aktor string, uid sql.NullInt64) {
	t.Helper()
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_log`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n > 0 {
		if err := db.QueryRow(`SELECT COALESCE(target,''), actor_username, actor_user_id FROM audit_log ORDER BY id DESC LIMIT 1`).Scan(&hedef, &aktor, &uid); err != nil {
			t.Fatal(err)
		}
	}
	return
}

// Tara kapıları: ilk tıklama kuyruğa + tek denetim satırı; tekrar tıklama "zaten sırada"
// (denetim artmaz); işlenince 15 dk soğuma 429; süre dolunca yeniden; günlük sınır 429;
// başka hesabın hakkı etkilenmez; adına oturum gerçek aktörle denetlenir.
func TestMusteri_TaraKapilari(t *testing.T) {
	db, _ := ekOrtam(t)
	k := kuyrukKur(t)
	simdi := saatKur(t)
	musteriKur(t, musteri10)
	h := &Handler{DB: db}

	tara := func(govde string) (int, map[string]any) {
		rr := musteriIstek(t, h, "POST", "/domains/10/guvenlik/tara", govde)
		var m map[string]any
		_ = json.Unmarshal(rr.Body.Bytes(), &m)
		return rr.Code, m
	}
	if kod, m := tara(`{}`); kod != 202 || m["sirada"] != float64(1) || m["zaten"] != nil {
		t.Fatalf("ilk tara: %d %v", kod, m)
	}
	n, hedef, aktor, uid := musteriDenetim(t, db)
	if n != 1 || hedef != "on.test" || aktor != "musteri:10" || uid.Valid {
		t.Fatalf("denetim: %d %q %q %v", n, hedef, aktor, uid)
	}
	if k.bekleyenSayisi() != 1 {
		t.Fatal("kuyrukta 1 hedef beklemeli")
	}
	// Tekrar tıklama: zaten sırada, denetim artmaz.
	if kod, m := tara(``); kod != 202 || m["zaten"] != true {
		t.Fatalf("tekrar tıklama: %d %v", kod, m)
	}
	if n, _, _, _ := musteriDenetim(t, db); n != 1 {
		t.Fatalf("zaten sırada: denetim satırı artmamalı: %d", n)
	}
	// İşçi aldı (sıradan düştü) → 15 dk soğuma: 429 + dk.
	k.sonrakiIs()
	kod, m := tara(`{}`)
	if kod != 429 || m["kod"] != "soguma" || !strings.Contains(m["error"].(string), "15 dk") {
		t.Fatalf("soğuma: %d %v", kod, m)
	}
	// Ek alan ayrı hedef: soğumadan etkilenmez.
	if kod, _ := tara(`{"ek_alan_id":1}`); kod != 202 {
		t.Fatalf("ek alan ayrı hedef: %d", kod)
	}
	if _, hedef, _, _ := musteriDenetim(t, db); hedef != "ek1.test" {
		t.Fatalf("ek alan denetim hedefi: %q", hedef)
	}
	k.sonrakiIs()
	// Başka hesap (11): d10'un soğuması/hakkı onu etkilemez.
	musteriKur(t, musteri11)
	if rr := musteriIstek(t, h, "POST", "/domains/11/guvenlik/tara", ""); rr.Code != 202 {
		t.Fatalf("hesap 11 etkilenmemeli: %d %s", rr.Code, rr.Body)
	}
	k.sonrakiIs()
	musteriKur(t, musteri10)
	// 16 dk sonra yeniden.
	*simdi = simdi.Add(16 * time.Minute)
	if kod, _ := tara(`{}`); kod != 202 {
		t.Fatalf("soğuma bitti: %d", kod)
	}
	k.sonrakiIs()

	// Günlük sınır: 30 kuyruğa alış dolana dek (zaten 3 yapıldı: d10 ×2 ana + ek1).
	yapilan := 3
	for yapilan < musteriGunlukSinir {
		*simdi = simdi.Add(16 * time.Minute)
		if kod, m := tara(`{}`); kod != 202 {
			t.Fatalf("günlük sınırdan önce %d. tara: %d %v", yapilan+1, kod, m)
		}
		k.sonrakiIs()
		yapilan++
	}
	*simdi = simdi.Add(16 * time.Minute)
	kod, m = tara(`{}`)
	if kod != 429 || m["kod"] != "gunluk_sinir" || !strings.Contains(m["error"].(string), "Günlük") {
		t.Fatalf("günlük sınır: %d %v", kod, m)
	}
	// Ek alan da aynı hesap hakkından düşer.
	if kod, _ := tara(`{"ek_alan_id":3}`); kod != 429 {
		t.Fatalf("günlük sınır hesap geneli: %d", kod)
	}
	if n, _, _, _ := musteriDenetim(t, db); n != musteriGunlukSinir+1 { // +1: hesap 11'in taraması
		t.Fatalf("denetim satırı sayısı: %d", n)
	}
	// 24 saat sonra hak yenilenir.
	*simdi = simdi.Add(25 * time.Hour)
	if kod, _ := tara(`{}`); kod != 202 {
		t.Fatalf("24 saat sonra yeniden: %d", kod)
	}
}

func TestMusteri_TaraAdinaOturumGercekAktor(t *testing.T) {
	db, _ := ekOrtam(t)
	kuyrukKur(t)
	saatKur(t)
	musteriKur(t, adina10)
	h := &Handler{DB: db}
	if rr := musteriIstek(t, h, "POST", "/domains/10/guvenlik/tara", ""); rr.Code != 202 {
		t.Fatalf("adına oturum tara: %d %s", rr.Code, rr.Body)
	}
	n, hedef, aktor, uid := musteriDenetim(t, db)
	if n != 1 || hedef != "on.test" || aktor != "bayi7 (adına musteri:10)" || !uid.Valid || uid.Int64 != 7 {
		t.Fatalf("adına oturum denetimi gerçek aktörle: %d %q %q %v", n, hedef, aktor, uid)
	}
}

// Bilinmeyen alanlı / yanlış tipli gövde 400, kuyruğa girmez.
func TestMusteri_TaraGovde(t *testing.T) {
	db, _ := ekOrtam(t)
	k := kuyrukKur(t)
	saatKur(t)
	musteriKur(t, musteri10)
	h := &Handler{DB: db}
	if rr := musteriIstek(t, h, "POST", "/domains/10/guvenlik/tara", `{"domain_id":11}`); rr.Code != 400 {
		t.Fatalf("bilinmeyen alan 400: %d", rr.Code)
	}
	if k.bekleyenSayisi() != 0 {
		t.Fatal("reddedilen istek kuyruğa girmemeli")
	}
	if rr := musteriIstek(t, h, "POST", "/domains/10/guvenlik/tara", `{"ek_alan_id":"x"}`); rr.Code != 400 {
		t.Fatalf("tür hatası 400: %d", rr.Code)
	}
}
