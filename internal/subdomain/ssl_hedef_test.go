package subdomain

// ssl_hedef_test.go — hesap SSL hedef listesi: sertifika türü/bitiş ayrıştırma, durum
// sınırları, güvenli dosya okuma (sembolik bağ/yol), DNS önbelleği + zaman aşımı,
// hedef listesi (ana/ek/alt, yabancı hesap yok, askı, sızıntı yok).

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"encoding/json"

	"github.com/go-chi/chi/v5"
)

var hSimdi = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

// hSert — sertifika PEM'i: kendinden imzalı (ihraççı=özne) ya da verilen ihraççı O ile imzalı.
func hSert(t *testing.T, ihracciO string, kendi bool, bitis time.Time, sanlar ...string) []byte {
	t.Helper()
	anahtar, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ozne := pkix.Name{CommonName: sanlar[0]}
	sablon := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: ozne,
		NotBefore: bitis.Add(-90 * 24 * time.Hour), NotAfter: bitis, DNSNames: sanlar}
	ust, imza := sablon, anahtar
	if !kendi {
		ca, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		ust = &x509.Certificate{SerialNumber: big.NewInt(7), Subject: pkix.Name{Organization: []string{ihracciO}, CommonName: "R11"},
			NotBefore: hSimdi.Add(-1000 * time.Hour), NotAfter: hSimdi.Add(1000 * time.Hour), IsCA: true, BasicConstraintsValid: true}
		imza = ca
	}
	der, err := x509.CreateCertificate(rand.Reader, sablon, ust, &anahtar.PublicKey, imza)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestSertAyristir_TurVeBitis(t *testing.T) {
	bitis := hSimdi.Add(40 * 24 * time.Hour)
	for _, c := range []struct {
		ad, tur string
		pem     []byte
	}{
		{"le", "letsencrypt", hSert(t, "Let's Encrypt", false, bitis, "a.test", "www.a.test")},
		{"ozimza", "oz-imza", hSert(t, "", true, bitis, "a.test")},
		{"baska-ca", "bilinmiyor", hSert(t, "Acme CA", false, bitis, "a.test")},
	} {
		o, err := sertAyristir(c.pem)
		if err != nil {
			t.Fatalf("%s: %v", c.ad, err)
		}
		if o.Tur != c.tur {
			t.Errorf("%s: tür=%s, beklenen %s", c.ad, o.Tur, c.tur)
		}
		if !o.Bitis.Equal(bitis.Truncate(time.Second)) {
			t.Errorf("%s: bitiş=%v", c.ad, o.Bitis)
		}
	}
	for _, kotu := range [][]byte{nil, []byte(""), []byte("garbage"), []byte("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n")} {
		if _, err := sertAyristir(kotu); err == nil {
			t.Errorf("bozuk girdi hata vermedi: %q", kotu)
		}
	}
}

func TestSSLDurumHesapla_Sinirlar(t *testing.T) {
	for _, c := range []struct {
		ad    string
		bitis time.Time
		durum string
		kalan int
	}{
		{"uzak", hSimdi.Add(60 * 24 * time.Hour), "aktif", 60},
		{"15 gün", hSimdi.Add(15 * 24 * time.Hour), "aktif", 15},
		{"14 gün tam", hSimdi.Add(14 * 24 * time.Hour), "yaklasiyor", 14},
		{"14 gün eksik saat", hSimdi.Add(14*24*time.Hour - time.Hour), "yaklasiyor", 13},
		{"bugün bitiyor", hSimdi.Add(2 * time.Hour), "yaklasiyor", 0},
		{"şimdi", hSimdi, "doldu", 0},
		{"dün", hSimdi.Add(-30 * time.Hour), "doldu", -2},
	} {
		d, k := sslDurumHesapla(c.bitis, hSimdi)
		if d != c.durum || k != c.kalan {
			t.Errorf("%s: (%s,%d) beklenen (%s,%d)", c.ad, d, k, c.durum, c.kalan)
		}
	}
}

func TestSertDosyaOku_SembolikBagVeYol(t *testing.T) {
	kok := t.TempDir()
	dis := t.TempDir()
	os.WriteFile(filepath.Join(dis, "gizli.crt"), []byte("DISARIDAKI"), 0o644)
	os.MkdirAll(filepath.Join(kok, "ssl"), 0o755)
	os.WriteFile(filepath.Join(kok, "ssl", "iyi.crt"), []byte("ICERIDEKI"), 0o644)
	// dosya sembolik bağı (dışarı) ve (kök içi başka dosyaya)
	os.Symlink(filepath.Join(dis, "gizli.crt"), filepath.Join(kok, "ssl", "bag.crt"))
	os.Symlink(filepath.Join(kok, "ssl", "iyi.crt"), filepath.Join(kok, "ssl", "ici-bag.crt"))
	// dizin sembolik bağı
	os.Symlink(dis, filepath.Join(kok, "bagdizin"))
	os.WriteFile(filepath.Join(kok, "ssl", "buyuk.crt"), make([]byte, 1000), 0o644)

	if b, err := sertDosyaOku(kok, filepath.Join(kok, "ssl", "iyi.crt"), 1<<10); err != nil || string(b) != "ICERIDEKI" {
		t.Fatalf("normal dosya okunamadı: %v", err)
	}
	for ad, yol := range map[string]string{
		"dosya-bag-dis":  filepath.Join(kok, "ssl", "bag.crt"),
		"dosya-bag-ici":  filepath.Join(kok, "ssl", "ici-bag.crt"),
		"dizin-bag":      filepath.Join(kok, "bagdizin", "gizli.crt"),
		"kok-disi":       filepath.Join(dis, "gizli.crt"),
		"nokta-nokta":    filepath.Join(kok, "ssl", "..", "..", "etc", "passwd"),
		"komsu-onek":     kok + "x/ssl/iyi.crt",
		"dizin-dosya-mi": filepath.Join(kok, "ssl"),
	} {
		if b, err := sertDosyaOku(kok, yol, 1<<10); err == nil {
			t.Errorf("%s: okunmamalıydı, içerik=%q", ad, b)
		}
	}
	if _, err := sertDosyaOku(kok, filepath.Join(kok, "ssl", "buyuk.crt"), 500); err == nil {
		t.Error("boyut sınırı aşan dosya okundu")
	}
}

func dnsSifirla(t *testing.T, cozucu func(string) []string, bekle time.Duration) {
	t.Helper()
	e1, e2, e3 := dnsCozucu, dnsBekle, sslSimdi
	dnsMu.Lock()
	dnsOnb, dnsUcusta = map[string]dnsGiris{}, map[string]bool{}
	dnsMu.Unlock()
	dnsCozucu, dnsBekle = cozucu, bekle
	t.Cleanup(func() {
		dnsCozucu, dnsBekle, sslSimdi = e1, e2, e3
		time.Sleep(20 * time.Millisecond)
		dnsMu.Lock()
		dnsOnb, dnsUcusta = map[string]dnsGiris{}, map[string]bool{}
		dnsMu.Unlock()
	})
}

func TestDNSKontrol_OnbellekTTL(t *testing.T) {
	var cagri atomic.Int32
	dnsSifirla(t, func(h string) []string { cagri.Add(1); return []string{"203.0.113.7"} }, 2*time.Second)
	simdi := hSimdi
	sslSimdi = func() time.Time { return simdi }
	ips := []string{"203.0.113.7"}
	if v := dnsKontrol([]string{"a.test", "www.a.test"}, ips); v == nil || !*v {
		t.Fatalf("hazır bekleniyordu: %v", v)
	}
	if cagri.Load() != 2 {
		t.Fatalf("ilk istekte 2 çözüm bekleniyordu: %d", cagri.Load())
	}
	dnsKontrol([]string{"a.test", "www.a.test"}, ips)
	if cagri.Load() != 2 {
		t.Fatalf("önbellek içinde yeni dış istek: %d", cagri.Load())
	}
	simdi = simdi.Add(dnsTTL + time.Second)
	dnsKontrol([]string{"a.test"}, ips)
	if cagri.Load() != 3 {
		t.Fatalf("TTL sonrası yeniden çözüm bekleniyordu: %d", cagri.Load())
	}
	// farklı IP → hazır değil
	if v := dnsKontrol([]string{"b.test"}, []string{"198.51.100.1"}); v == nil || *v {
		t.Fatalf("yanlış IP hazır sayıldı: %v", v)
	}
}

func TestDNSKontrol_ZamanAsimiNil(t *testing.T) {
	dur := make(chan struct{})
	dnsSifirla(t, func(h string) []string { <-dur; return []string{"203.0.113.7"} }, 50*time.Millisecond)
	t0 := time.Now()
	v := dnsKontrol([]string{"yavas.test"}, []string{"203.0.113.7"})
	if v != nil {
		t.Fatalf("zaman aşımında nil bekleniyordu: %v", *v)
	}
	if time.Since(t0) > 2*time.Second {
		t.Fatal("zaman aşımı beklenenden uzun")
	}
	close(dur)
	time.Sleep(100 * time.Millisecond)
	if v := dnsKontrol([]string{"yavas.test"}, []string{"203.0.113.7"}); v == nil || !*v {
		t.Fatalf("arka plan sonucu önbelleğe yazılmalıydı: %v", v)
	}
}

func TestDNSKontrol_EszamanlilikSiniri(t *testing.T) {
	var ayni, azami atomic.Int32
	dur := make(chan struct{})
	dnsSifirla(t, func(h string) []string {
		n := ayni.Add(1)
		for {
			m := azami.Load()
			if n <= m || azami.CompareAndSwap(m, n) {
				break
			}
		}
		<-dur
		ayni.Add(-1)
		return nil
	}, 10*time.Millisecond)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); dnsKontrol([]string{fmt.Sprintf("d%d.test", i)}, []string{"203.0.113.7"}) }(i)
	}
	wg.Wait()
	if azami.Load() > 4 {
		t.Fatalf("eşzamanlı dış çözüm sınırı aşıldı: %d > 4", azami.Load())
	}
	close(dur)
}

// ───────────────────────── hedef listesi (DB) ─────────────────────────

func hGet(t *testing.T, h *Handlers, did int64) (int, string) {
	t.Helper()
	r := chi.NewRouter()
	r.Get("/domains/{id}/ssl/hedefler", h.SSLHedefler)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", fmt.Sprintf("/domains/%d/ssl/hedefler", did), nil))
	return rec.Code, rec.Body.String()
}

func hYaz(t *testing.T, yol string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(yol), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(yol, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

type hYanit struct {
	Hedefler []SSLHedef `json:"hedefler"`
}

func hBul(t *testing.T, y hYanit, ad string) SSLHedef {
	t.Helper()
	for _, h := range y.Hedefler {
		if h.AlanAdi == ad {
			return h
		}
	}
	t.Fatalf("%s listede yok: %+v", ad, y.Hedefler)
	return SSLHedef{}
}

const hGizliAnahtar = "PRIVATE-KEY-BU-SIZMAMALI"

func hKur(t *testing.T) (*Handlers, *ekDikis, string) {
	t.Helper()
	db := ek_db(t)
	d := ek_dikisler(t)
	for _, q := range []string{
		`ALTER TABLE domains ADD COLUMN ssl_aktif TINYINT NOT NULL DEFAULT 0, ADD COLUMN cert_path VARCHAR(255) NOT NULL DEFAULT ''`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	anaKok, acmeKok := d.kok+"/pki", d.kok+"/acmehome"
	e1, e2, e3 := anaSertifikaKoku, acmeKayitKoku, sslSimdi
	anaSertifikaKoku, acmeKayitKoku, sslSimdi = anaKok, func() string { return acmeKok }, func() time.Time { return hSimdi }
	t.Cleanup(func() { anaSertifikaKoku, acmeKayitKoku, sslSimdi = e1, e2, e3 })
	dnsSifirla(t, func(h string) []string {
		if h == "yok.test" || h == "www.yok.test" {
			return []string{"203.0.113.7"}
		}
		return nil
	}, 3*time.Second)
	sslSimdi = func() time.Time { return hSimdi }

	// ana (dom1): LE, 40 gün
	db.Exec(`UPDATE domains SET ssl_aktif=1, cert_path=? WHERE id=?`, anaKok+"/"+ek_alan1+"/"+ek_alan1+".crt", ek_dom1)
	hYaz(t, anaKok+"/"+ek_alan1+"/"+ek_alan1+".crt", hSert(t, "Let's Encrypt", false, hSimdi.Add(40*24*time.Hour), ek_alan1, "www."+ek_alan1))
	ssl := evKoku + "/" + ek_sk1 + "/ssl/"
	ekle := func(dom int64, ad string) int64 {
		r, err := db.Exec(`INSERT INTO ek_alan_adlari (domain_id, alan_adi) VALUES (?,?)`, dom, ad)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := r.LastInsertId()
		return id
	}
	// ek: LE 10 gün + acme kaydı gecikmiş
	ekle(ek_dom1, "shop.test")
	hYaz(t, ssl+"shop.test.crt", hSert(t, "Let's Encrypt", false, hSimdi.Add(10*24*time.Hour), "shop.test", "www.shop.test"))
	hYaz(t, ssl+"shop.test.key", []byte(hGizliAnahtar))
	hYaz(t, acmeKok+"/shop.test_ecc/shop.test.conf",
		[]byte(fmt.Sprintf("Le_Domain='shop.test'\nLe_NextRenewTime='%d'\n", hSimdi.Add(-3*24*time.Hour).Unix())))
	// ek: öz-imza
	ekle(ek_dom1, "eski.test")
	hYaz(t, ssl+"eski.test.crt", hSert(t, "", true, hSimdi.Add(300*24*time.Hour), "eski.test", "www.eski.test"))
	hYaz(t, ssl+"eski.test.key", []byte(hGizliAnahtar))
	// ek: bozuk dosya
	ekle(ek_dom1, "bozuk.test")
	hYaz(t, ssl+"bozuk.test.crt", []byte("-----BEGIN CERTIFICATE-----\nZZZ-ham-icerik-sizmasin\n"))
	hYaz(t, ssl+"bozuk.test.key", []byte(hGizliAnahtar))
	// ek: SSL yok, DNS hazır
	ekle(ek_dom1, "yok.test")
	// alt: süresi dolmuş LE
	db.Exec(`INSERT INTO subdomanlar (domain_id, alt_ad, tam_ad) VALUES (?,?,?)`, ek_dom1, "blog", "blog."+ek_alan1)
	hYaz(t, ssl+"blog."+ek_alan1+".crt", hSert(t, "Let's Encrypt", false, hSimdi.Add(-5*24*time.Hour), "blog."+ek_alan1))
	hYaz(t, ssl+"blog."+ek_alan1+".key", []byte(hGizliAnahtar))
	// yabancı hesap (dom2)
	ekle(ek_dom2, "gizli.test")
	hYaz(t, evKoku+"/"+ek_sk2+"/ssl/gizli.test.crt", hSert(t, "Let's Encrypt", false, hSimdi.Add(80*24*time.Hour), "gizli.test"))
	hYaz(t, evKoku+"/"+ek_sk2+"/ssl/gizli.test.key", []byte(hGizliAnahtar))
	db.Exec(`INSERT INTO subdomanlar (domain_id, alt_ad, tam_ad) VALUES (?,?,?)`, ek_dom2, "yabanci", "yabanci."+ek_alan2)
	return &Handlers{DB: db, IPv4: "203.0.113.7"}, d, anaKok
}

func TestSSLHedefler_Liste(t *testing.T) {
	h, _, _ := hKur(t)
	kod, govde := hGet(t, h, ek_dom1)
	if kod != http.StatusOK {
		t.Fatalf("%d %s", kod, govde)
	}
	var y hYanit
	if err := json.Unmarshal([]byte(govde), &y); err != nil {
		t.Fatal(err)
	}
	if len(y.Hedefler) != 6 {
		t.Fatalf("6 hedef bekleniyordu (ana+4 ek+1 alt): %d\n%s", len(y.Hedefler), govde)
	}
	if y.Hedefler[0].Tur != "ana" {
		t.Error("ilk satır ana alan adı olmalı")
	}
	// ana
	a := hBul(t, y, ek_alan1)
	if a.Tur != "ana" || !a.SSLAktif || *a.SertifikaTuru != "letsencrypt" || a.Durum != "aktif" || *a.KalanGun != 40 {
		t.Errorf("ana: %+v", a)
	}
	if strings.Join(a.Kapsam, ",") != "apex,www" || a.DNSHazir != nil {
		t.Errorf("ana kapsam/dns: %+v", a)
	}
	// ek LE yaklaşan + yenileme gecikti
	s := hBul(t, y, "shop.test")
	if s.Tur != "ek" || s.EkAlanID == nil || s.AltAlanID != nil || s.Durum != "yaklasiyor" || *s.KalanGun != 10 ||
		!s.OtomatikYenileme || !s.YenilemeGecikti || s.SonrakiYenileme != "2026-10-02" {
		t.Errorf("shop: %+v", s)
	}
	// ek öz-imza (LE değil, otomatik yenileme yok)
	e := hBul(t, y, "eski.test")
	if *e.SertifikaTuru != "oz-imza" || e.OtomatikYenileme || e.Durum != "aktif" || e.DNSHazir == nil || *e.DNSHazir {
		t.Errorf("eski: %+v", e)
	}
	// bozuk
	b := hBul(t, y, "bozuk.test")
	if !b.SSLAktif || b.Durum != "okunamadi" || b.Bitis != "" || b.KalanGun != nil {
		t.Errorf("bozuk: %+v", b)
	}
	// yok + DNS hazır
	n := hBul(t, y, "yok.test")
	if n.SSLAktif || n.Durum != "yok" || n.SertifikaTuru != nil || n.DNSHazir == nil || !*n.DNSHazir {
		t.Errorf("yok: %+v", n)
	}
	// alt, süresi dolmuş
	l := hBul(t, y, "blog."+ek_alan1)
	if l.Tur != "alt" || l.AltAlanID == nil || l.Durum != "doldu" || *l.KalanGun != -5 || strings.Join(l.Kapsam, ",") != "apex" {
		t.Errorf("alt: %+v", l)
	}
	// yabancı hesap yok
	for _, ad := range []string{"gizli.test", "yabanci." + ek_alan2, ek_alan2} {
		if strings.Contains(govde, ad) {
			t.Errorf("başka hesabın alan adı sızdı: %s", ad)
		}
	}
	// sızıntı: anahtar, ham PEM, dosya yolu
	for _, yasak := range []string{hGizliAnahtar, "BEGIN", "ham-icerik", "/ssl/", ".crt", ".key", "cert_yol", "key_yol", evKoku} {
		if strings.Contains(govde, yasak) {
			t.Errorf("yanıt %q içeriyor", yasak)
		}
	}
}

func TestSSLHedefler_IDORVeAski(t *testing.T) {
	h, _, _ := hKur(t)
	// dom2 yalnız kendini görür
	kod, govde := hGet(t, h, ek_dom2)
	if kod != 200 || strings.Contains(govde, "shop.test") || strings.Contains(govde, ek_alan1) || !strings.Contains(govde, "gizli.test") {
		t.Fatalf("hesap 2 listesi: %d %s", kod, govde)
	}
	// olmayan hesap
	if kod, _ := hGet(t, h, 99999); kod != http.StatusNotFound {
		t.Errorf("olmayan hesap 404 olmalı: %d", kod)
	}
	// askı
	h.DB.Exec(`UPDATE domains SET askida=1 WHERE id=?`, ek_dom1)
	_, govde = hGet(t, h, ek_dom1)
	var y hYanit
	json.Unmarshal([]byte(govde), &y)
	for _, x := range y.Hedefler {
		if !x.Askida {
			t.Errorf("askıdaki hesapta %s askida=false", x.AlanAdi)
		}
	}
}

func TestSSLHedefler_SembolikBagSertifikaOkunmaz(t *testing.T) {
	h, _, _ := hKur(t)
	dis := t.TempDir()
	hYaz(t, dis+"/gizli.crt", hSert(t, "Let's Encrypt", false, hSimdi.Add(200*24*time.Hour), "yok.test"))
	hYaz(t, dis+"/gizli.key", []byte("x"))
	ssl := evKoku + "/" + ek_sk1 + "/ssl/"
	os.Symlink(dis+"/gizli.crt", ssl+"yok.test.crt")
	os.Symlink(dis+"/gizli.key", ssl+"yok.test.key")
	_, govde := hGet(t, h, ek_dom1)
	var y hYanit
	json.Unmarshal([]byte(govde), &y)
	n := hBul(t, y, "yok.test")
	if n.Durum == "aktif" || n.Bitis != "" {
		t.Fatalf("sembolik bağlı sertifika okundu: %+v", n)
	}
	// ana alan: DB'deki cert_path kök dışına işaret ediyorsa kanonik yola düşer (dışarıdaki okunmaz)
	h.DB.Exec(`UPDATE domains SET cert_path=? WHERE id=?`, dis+"/gizli.crt", ek_dom1)
	_, govde = hGet(t, h, ek_dom1)
	y = hYanit{}
	json.Unmarshal([]byte(govde), &y)
	if a := hBul(t, y, ek_alan1); *a.KalanGun != 40 {
		t.Fatalf("ana: kök dışı yol okundu: %+v", a)
	}
}

func TestSSLHedefler_UzunListeHizli(t *testing.T) {
	h, _, _ := hKur(t)
	ssl := evKoku + "/" + ek_sk1 + "/ssl/"
	for i := 0; i < 80; i++ {
		ad := fmt.Sprintf("toplu%d.test", i)
		h.DB.Exec(`INSERT INTO ek_alan_adlari (domain_id, alan_adi) VALUES (?,?)`, ek_dom1, ad)
		hYaz(t, ssl+ad+".crt", hSert(t, "Let's Encrypt", false, hSimdi.Add(50*24*time.Hour), ad))
		hYaz(t, ssl+ad+".key", []byte("k"))
	}
	t0 := time.Now()
	kod, govde := hGet(t, h, ek_dom1)
	if kod != 200 || !strings.Contains(govde, "toplu79.test") {
		t.Fatalf("%d", kod)
	}
	if d := time.Since(t0); d > 5*time.Second {
		t.Fatalf("86 hedefli liste yavaş: %v", d)
	}
}
