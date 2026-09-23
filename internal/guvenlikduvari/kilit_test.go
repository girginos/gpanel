package guvenlikduvari

// kilit_test.go — RK-KILIT-01 / RK-06: korumalı panel kapısı portu "kapat" kuralıyla
// kapatılamaz; ne pasif kuralı yeniden etkinleştirerek (Durum) ne de DB'de zaten aktif
// duran bir kuralla (rebuild süzmesi).

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/sahtedb"
)

// TestMain: hiçbir test gerçek nft'ye / /etc'ye inmesin — dikişi değiştirmeyi unutan test
// canlı güvenlik duvarını değiştirmek yerine hata alır.
func TestMain(m *testing.M) {
	rulesetUygula = func([]byte) error { return errors.New("test: gerçek nft uygulaması yasak") }
	os.Exit(m.Run())
}

// sahteUygula: uygulanan ruleset'leri kaydeder.
func sahteUygula(t *testing.T) *[]string {
	t.Helper()
	eski := rulesetUygula
	t.Cleanup(func() { rulesetUygula = eski })
	uygulanan := &[]string{}
	rulesetUygula = func(b []byte) error {
		*uygulanan = append(*uygulanan, string(b))
		return nil
	}
	return uygulanan
}

// kapiPortlari: dinamik korunan listeyi (rol kapıları) test süresince bağlar.
func kapiPortlari(t *testing.T, portlar ...int) {
	t.Helper()
	eski := EkKorumaliPortlar
	t.Cleanup(func() { EkKorumaliPortlar = eski })
	EkKorumaliPortlar = func() []int { return portlar }
}

func fwOrtami(t *testing.T) (*sahtedb.DB, *Handlers) {
	t.Helper()
	s, db := sahtedb.Yeni()
	t.Cleanup(func() { db.Close() })
	return s, &Handlers{DB: db}
}

func durumIstek(h *Handlers, id, govde string) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	r.Post("/firewall/{id}/durum", h.Durum)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/firewall/"+id+"/durum", strings.NewReader(govde)))
	return rec
}

const durumSorgu = "FROM firewall_kurallari WHERE id=?"
const aktifSorgu = "WHERE aktif=1"

// Pasif bir "kapat" kuralı, portu sonradan panel kapısı olduysa yeniden etkinleştirilemez;
// ret Ekle'dekiyle aynı kod + metin, DB'ye yazılmaz, nft'ye inilmez.
func TestDurumKorumaliKapatAktiflestirmeReddedilir(t *testing.T) {
	for _, tc := range []struct {
		ad    string
		kapi  []int
		port  int64
		proto string
	}{
		{"rastgele rol kapısı", []int{21001}, 21001, "tcp"},
		{"statik kritik port", nil, 22, "tcp"},
		{"udp de kapatılamaz (Ekle ile aynı)", []int{21001}, 21001, "udp"},
	} {
		t.Run(tc.ad, func(t *testing.T) {
			kapiPortlari(t, tc.kapi...)
			uygulanan := sahteUygula(t)
			s, h := fwOrtami(t)
			s.Satirlar(durumSorgu, []any{"kapat", tc.port})

			rec := durumIstek(h, "7", `{"aktif":true}`)

			// Ekle ile aynı yanıt beklenir.
			ekleRec := httptest.NewRecorder()
			h.Ekle(ekleRec, httptest.NewRequest(http.MethodPost, "/firewall",
				strings.NewReader(`{"tip":"kapat","protokol":"`+tc.proto+`","port":`+strconv.FormatInt(tc.port, 10)+`}`)))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("korumalı kapat etkinleştirme 400 dönmeli, got %d %s", rec.Code, rec.Body)
			}
			if ekleRec.Code != rec.Code || ekleRec.Body.String() != rec.Body.String() {
				t.Fatalf("Durum reddi Ekle ile aynı olmalı:\n durum=%d %s\n ekle=%d %s",
					rec.Code, rec.Body, ekleRec.Code, ekleRec.Body)
			}
			if !strings.Contains(rec.Body.String(), "port "+strconv.FormatInt(tc.port, 10)+" kritik") {
				t.Fatalf("mesaj portu adlandırmalı: %s", rec.Body)
			}
			if s.Yurutuldu("SET aktif") {
				t.Fatal("reddedilen istek aktif bayrağını yazmamalı")
			}
			if len(*uygulanan) != 0 {
				t.Fatal("reddedilen istek ruleset uygulamamalı")
			}
		})
	}
}

// Korumasız port / korumalı porta ban-whitelist / pasife alma: eski davranış (200, yazılır,
// yeniden kurulur). Pasife almak kural okumaz bile.
func TestDurumKorumasizDavranisKorunur(t *testing.T) {
	for _, tc := range []struct {
		ad       string
		govde    string
		satir    []any // nil → kural okunmamalı
		yazilan  string
		okunmali bool
	}{
		{"korumasız kapat etkinleştirilir", `{"aktif":true}`, []any{"kapat", int64(3306)}, "| 1 7", true},
		{"korumalı porta ban serbest", `{"aktif":true}`, []any{"ban", int64(21001)}, "| 1 7", true},
		{"korumalı porta whitelist serbest", `{"aktif":true}`, []any{"whitelist", int64(21001)}, "| 1 7", true},
		{"korumalı kapat pasife alınır", `{"aktif":false}`, nil, "| 0 7", false},
		{"gövdesiz = pasif (eski davranış)", ``, nil, "| 0 7", false},
	} {
		t.Run(tc.ad, func(t *testing.T) {
			kapiPortlari(t, 21001)
			uygulanan := sahteUygula(t)
			s, h := fwOrtami(t)
			if tc.satir != nil {
				s.Satirlar(durumSorgu, tc.satir)
			} else {
				s.Satirlar(durumSorgu, []any{"kapat", int64(21001)})
			}

			rec := durumIstek(h, "7", tc.govde)
			if rec.Code != http.StatusOK {
				t.Fatalf("200 beklenirdi, got %d %s", rec.Code, rec.Body)
			}
			if !s.Yurutuldu("UPDATE firewall_kurallari SET aktif=? WHERE id=? " + tc.yazilan) {
				t.Fatalf("aktif bayrağı yazılmalı (%s)", tc.yazilan)
			}
			if got := s.Sorgulandi(durumSorgu); got != tc.okunmali {
				t.Fatalf("kural okundu mu = %v, beklenen %v", got, tc.okunmali)
			}
			if len(*uygulanan) != 1 {
				t.Fatalf("ruleset bir kez uygulanmalı, got %d", len(*uygulanan))
			}
		})
	}
}

// Etkinleştirilecek kural yoksa 404; DB'ye yazılmaz.
func TestDurumKuralYok(t *testing.T) {
	uygulanan := sahteUygula(t)
	s, h := fwOrtami(t)
	rec := durumIstek(h, "99", `{"aktif":true}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("404 beklenirdi, got %d %s", rec.Code, rec.Body)
	}
	if s.Yurutuldu("SET aktif") || len(*uygulanan) != 0 {
		t.Fatal("olmayan kural için yazma/uygulama olmamalı")
	}
}

// DB'de zaten AKTİF duran (koruma öncesi girmiş ya da portu sonradan kapı olmuş) korumalı
// "kapat" kuralı nft'ye yazılmaz; korumasız kurallar aynen kalır.
func TestRebuildKorumaliKapatiSuzer(t *testing.T) {
	kapiPortlari(t, 21001)
	uygulanan := sahteUygula(t)
	s, h := fwOrtami(t)
	s.Satirlar(aktifSorgu,
		[]any{int64(1), "kapat", "", int64(3306), "tcp"},
		[]any{int64(2), "kapat", "", int64(21001), "tcp"},
		[]any{int64(3), "kapat", "", int64(22), "tcp"},
	)
	if err := h.rebuild(); err != nil {
		t.Fatal(err)
	}
	if len(*uygulanan) != 1 {
		t.Fatalf("bir kez uygulanmalı, got %d", len(*uygulanan))
	}
	rs := (*uygulanan)[0]
	if !strings.Contains(rs, "\t\ttcp dport 3306 drop\n") {
		t.Errorf("korumasız kapat yazılmalı:\n%s", rs)
	}
	if strings.Contains(rs, "dport 21001") || strings.Contains(rs, "dport 22 ") {
		t.Errorf("korumalı portun kapat kuralı yazılmamalı:\n%s", rs)
	}
}

// Saf üretim: korumalı kapat atlanır ve raporlanır; ban/whitelist (allowlist) korumalı
// portta da yazılır (yönetici kasıtlı kısıtlaması); korumasız çıktı eski biçimle birebir.
func TestRulesetUretSuzme(t *testing.T) {
	korumali := func(p int) bool { return p == 21001 || p == 53 }
	kurallar := []fwKural{
		{ID: 1, Tip: "whitelist", IP: "1.2.3.4", Port: 0, Protokol: "tcp"},
		{ID: 2, Tip: "whitelist", IP: "10.0.0.0/8", Port: 25000, Protokol: "tcp"},
		{ID: 3, Tip: "kapat", Port: 3306, Protokol: "tcp"},
		{ID: 4, Tip: "kapat", Port: 21001, Protokol: "tcp"},
		{ID: 5, Tip: "kapat", Port: 53, Protokol: "udp"},
		{ID: 6, Tip: "ban", IP: "5.6.7.8", Port: 21001, Protokol: "tcp"},
		{ID: 7, Tip: "ban", IP: "2001:db8::1", Port: 0, Protokol: "tcp"},
	}
	rs, atlanan := rulesetUret(kurallar, korumali)
	want := "table inet girginos_fw {}\n" +
		"delete table inet girginos_fw\n" +
		"table inet girginos_fw {\n" +
		"\tchain input {\n" +
		"\t\ttype filter hook input priority filter; policy accept;\n" +
		"\t\tct state established,related accept\n" +
		"\t\tiif \"lo\" accept\n" +
		"\t\tip saddr 1.2.3.4 accept\n" +
		"\t\tip saddr 10.0.0.0/8 tcp dport 25000 accept\n" +
		"\t\ttcp dport 25000 drop\n" +
		"\t\ttcp dport 3306 drop\n" +
		"\t\tip saddr 5.6.7.8 tcp dport 21001 drop\n" +
		"\t\tip6 saddr 2001:db8::1 drop\n" +
		"\t}\n}\n"
	if string(rs) != want {
		t.Fatalf("ruleset:\n%s\nbeklenen:\n%s", rs, want)
	}
	if len(atlanan) != 2 || atlanan[0].ID != 4 || atlanan[1].ID != 5 {
		t.Fatalf("atlanan kurallar #4 ve #5 olmalı: %+v", atlanan)
	}

	// Korumasız dünyada (hiçbir port korumalı değil) tüm kapat'lar eski sırayla yazılır.
	rs2, atlanan2 := rulesetUret(kurallar, func(int) bool { return false })
	if len(atlanan2) != 0 {
		t.Fatalf("korumasızken atlanan olmamalı: %+v", atlanan2)
	}
	for _, satir := range []string{"\t\ttcp dport 3306 drop\n", "\t\ttcp dport 21001 drop\n", "\t\tudp dport 53 drop\n"} {
		if !strings.Contains(string(rs2), satir) {
			t.Errorf("korumasızken %q yazılmalı:\n%s", satir, rs2)
		}
	}
}
