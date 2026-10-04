package kopru

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"girginospanel/internal/middleware"
)

// kokRouter: gelen isteğin destek işaretini ve yolunu JSON olarak döner.
type kokRouter struct {
	cagri atomic.Int64
	bekle time.Duration
	buyuk int
}

func (k *kokRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	k.cagri.Add(1)
	if k.bekle > 0 {
		time.Sleep(k.bekle)
	}
	if k.buyuk > 0 {
		_, _ = w.Write(bytes.Repeat([]byte("a"), k.buyuk))
		return
	}
	dk := middleware.DestekFrom(r)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"yol": r.URL.Path, "sorgu": r.URL.RawQuery, "yontem": r.Method, "kimlik": dk})
}

// kisaYol: unix soket yolu ~104 bayt sınırı (t.TempDir uzun olabilir).
func kisaDizin(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "kp")
	if err != nil {
		t.Skip("kısa geçici dizin yok")
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return d
}

func sunucuBaslat(t *testing.T, r http.Handler, ayar func(*Sunucu)) (*Sunucu, string) {
	t.Helper()
	yol := filepath.Join(kisaDizin(t), "s", "k.sock")
	s := &Sunucu{Router: r, Yol: yol, IzinliUID: uint32(os.Geteuid())}
	if ayar != nil {
		ayar(s)
	}
	ctx, iptal := context.WithCancel(context.Background())
	bitti := make(chan error, 1)
	go func() { bitti <- s.Dinle(ctx) }()
	t.Cleanup(func() { iptal(); <-bitti })
	for i := 0; i < 200; i++ {
		if _, err := os.Lstat(yol); err == nil {
			return s, yol
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("soket kurulmadı")
	return nil, ""
}

func istemci(yol string) *Istemci {
	return &Istemci{Yol: yol, SahipUID: uint32(os.Geteuid()), CagriSiniri: 3 * time.Second}
}

var kokKimlik = middleware.DestekKimlik{Rol: middleware.DestekKok, KullaniciID: 1, Kullanici: "root", TgKisa: "t1"}

func TestKopruIzinliCagriSuurIciYolIleAyniYaniti(t *testing.T) {
	r := &kokRouter{}
	_, yol := sunucuBaslat(t, r, nil)
	i := Istek{Kimlik: kokKimlik, Yol: "/api/v1/domains/42/ssl", Sorgu: "dosya=a&son=5"}
	y, err := istemci(yol).Cagir(context.Background(), i)
	if err != nil {
		t.Fatal(err)
	}
	if y.Kod != 200 {
		t.Fatalf("kod %d", y.Kod)
	}
	// Aynı çağrının süreç içi (Yerel) sonucu BİREBİR aynı olmalı (kimlik işareti dahil).
	i.Sorgu = "dosya=a&son=5"
	yerel, err := Yerel{Router: &kokRouter{}}.Cagir(context.Background(), i)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(y.Govde, yerel.Govde) || y.Kod != yerel.Kod {
		t.Fatalf("köprü yanıtı süreç içinden farklı:\nköprü: %s\nyerel: %s", y.Govde, yerel.Govde)
	}
	var g struct {
		Yontem string
		Kimlik *middleware.DestekKimlik
	}
	_ = json.Unmarshal(y.Govde, &g)
	if g.Yontem != "GET" || g.Kimlik == nil || g.Kimlik.Rol != middleware.DestekKok || g.Kimlik.TgKisa != "t1" {
		t.Fatalf("destek işareti panel tarafında konmadı: %s", y.Govde)
	}
}

func TestKopruYetkisizEsReddedilir(t *testing.T) {
	r := &kokRouter{}
	_, yol := sunucuBaslat(t, r, func(s *Sunucu) {
		s.EsUID = func(*net.UnixConn) (uint32, error) { return 1000, nil }
	})
	_, err := istemci(yol).Cagir(context.Background(), Istek{Kimlik: kokKimlik, Yol: "/api/v1/domains/42"})
	if !errors.Is(err, ErrYok) {
		t.Fatalf("yetkisiz eş ErrYok almalı: %v", err)
	}
	if r.cagri.Load() != 0 {
		t.Fatal("yetkisiz eş için yönlendirici ÇAĞRILDI")
	}
	// uid okunamazsa da kapalı başarısız.
	r2 := &kokRouter{}
	_, yol2 := sunucuBaslat(t, r2, func(s *Sunucu) {
		s.EsUID = func(*net.UnixConn) (uint32, error) { return 0, errors.New("yok") }
	})
	if _, err := istemci(yol2).Cagir(context.Background(), Istek{Kimlik: kokKimlik, Yol: "/api/v1/domains/42"}); !errors.Is(err, ErrYok) || r2.cagri.Load() != 0 {
		t.Fatalf("uid okunamayınca geçti: %v", err)
	}
}

// Gerçek SO_PEERCRED: kendi uid'imiz doğru okunmalı.
func TestKopruGercekSoPeerCred(t *testing.T) {
	var gorulen atomic.Int64
	gorulen.Store(-1)
	_, yol := sunucuBaslat(t, &kokRouter{}, func(s *Sunucu) {
		s.EsUID = func(c *net.UnixConn) (uint32, error) {
			u, err := esUIDOku(c)
			gorulen.Store(int64(u))
			return u, err
		}
	})
	if err := istemci(yol).Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	if int(gorulen.Load()) != os.Geteuid() {
		t.Fatalf("SO_PEERCRED uid=%d, beklenen %d", gorulen.Load(), os.Geteuid())
	}
}

func hamIstek(t *testing.T, yol string, govde []byte) Yanit {
	t.Helper()
	c, err := net.Dial("unix", yol)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = c.Write(govde)
	_ = c.(*net.UnixConn).CloseWrite()
	b, _ := io.ReadAll(c)
	var y Yanit
	if json.Unmarshal(b, &y) != nil {
		return Yanit{Hata: "yanıt yok"}
	}
	return y
}

func TestKopruYolVeBicimRedleri(t *testing.T) {
	r := &kokRouter{}
	_, yol := sunucuBaslat(t, r, nil)
	kim := `"kimlik":{"Rol":"kok","KullaniciID":1,"Kullanici":"root","TgKisa":"t"}`
	// Not: DestekKimlik alanlarında json etiketi yok → alan adları Go adlarıdır.
	redler := map[string]string{
		"tabloda olmayan yol":     `{` + kim + `,"yol":"/api/v1/auth/login"}`,
		"yazma ucu":               `{` + kim + `,"yol":"/api/v1/domains/42/ssl/yenile"}`,
		"yol kaçışı":              `{` + kim + `,"yol":"/api/v1/domains/42/../admin/users"}`,
		"POST (bilinmeyen alan)":  `{` + kim + `,"yol":"/api/v1/domains/42","metod":"POST"}`,
		"yönetici listesi":        `{` + kim + `,"yol":"/api/v1/admin/users"}`,
		"sorguda yol enjeksiyonu": `{` + kim + `,"yol":"/api/v1/domains/42","sorgu":"a=\u0000b"}`,
		"bilinmeyen rol":          `{"kimlik":{"Rol":"admin","KullaniciID":1},"yol":"/api/v1/domains/42"}`,
		"bozuk json":              `{`,
		"fazladan veri":           `{` + kim + `,"yol":"/api/v1/domains/42"} {}`,
		"yol yok":                 `{` + kim + `}`,
		"yolda sorgu":             `{` + kim + `,"yol":"/api/v1/domains/42?x=1"}`,
	}
	for ad, g := range redler {
		y := hamIstek(t, yol, []byte(g))
		if y.Kod != 0 || y.Hata == "" {
			t.Errorf("%s: reddedilmeliydi: %+v", ad, y)
		}
	}
	// Aşırı büyük istek.
	buyuk := `{` + kim + `,"yol":"/api/v1/domains/42","sorgu":"` + strings.Repeat("a", IstekSiniri) + `"}`
	if y := hamIstek(t, yol, []byte(buyuk)); y.Kod != 0 {
		t.Errorf("aşırı büyük istek geçti: %+v", y)
	}
	if n := r.cagri.Load(); n != 0 {
		t.Fatalf("reddedilen isteklerde yönlendirici %d kez çağrıldı", n)
	}
	// Geçerli istek hâlâ çalışır (sunucu reddedilenlerden etkilenmedi).
	if y := hamIstek(t, yol, []byte(`{`+kim+`,"yol":"/api/v1/domains/42"}`)); y.Kod != 200 {
		t.Fatalf("geçerli istek: %+v", y)
	}
}

func TestKopruYanitSiniri(t *testing.T) {
	_, yol := sunucuBaslat(t, &kokRouter{buyuk: YanitSiniri + 1000}, nil)
	y, err := istemci(yol).Cagir(context.Background(), Istek{Kimlik: kokKimlik, Yol: "/api/v1/domains/42"})
	if err != nil {
		t.Fatal(err)
	}
	if !y.Asildi || len(y.Govde) > YanitSiniri {
		t.Fatalf("yanıt sınırı uygulanmadı: asildi=%v len=%d", y.Asildi, len(y.Govde))
	}
}

func TestKopruSoketIzinleri(t *testing.T) {
	_, yol := sunucuBaslat(t, &kokRouter{}, nil)
	st, err := os.Stat(yol)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 || st.Mode()&os.ModeSocket == 0 {
		t.Errorf("soket modu %v", st.Mode())
	}
	ds, _ := os.Stat(filepath.Dir(yol))
	if ds.Mode().Perm() != 0o700 {
		t.Errorf("dizin modu %v", ds.Mode().Perm())
	}
}

func TestKopruSoketYolundaDosyaVarsaDokunmaz(t *testing.T) {
	d := filepath.Join(kisaDizin(t), "s")
	_ = os.MkdirAll(d, 0o700)
	yol := filepath.Join(d, "k.sock")
	if err := os.WriteFile(yol, []byte("önemli"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Sunucu{Router: &kokRouter{}, Yol: yol}
	if err := s.Dinle(context.Background()); err == nil {
		t.Fatal("soket olmayan dosyanın üstüne soket kuruldu")
	}
	if b, _ := os.ReadFile(yol); string(b) != "önemli" {
		t.Fatal("dosya bozuldu")
	}
}

func TestKopruIstemciYokluk(t *testing.T) {
	// Soket yok.
	if _, err := istemci(filepath.Join(kisaDizin(t), "yok.sock")).Cagir(context.Background(), Istek{Ping: true}); !errors.Is(err, ErrYok) {
		t.Fatalf("soket yokken ErrYok beklenir: %v", err)
	}
	// Yanlış sahip uid'li soket güvenilmez.
	_, yol := sunucuBaslat(t, &kokRouter{}, nil)
	c := istemci(yol)
	c.SahipUID = uint32(os.Geteuid()) + 4242
	if err := c.Ping(context.Background()); !errors.Is(err, ErrYok) {
		t.Fatalf("yabancı sahipli soket kullanıldı: %v", err)
	}
	// Panel takılırsa (yanıt gelmez) süre dolunca ErrYok.
	_, yol2 := sunucuBaslat(t, &kokRouter{bekle: 3 * time.Second}, func(s *Sunucu) { s.IslemSiniri = 5 * time.Second })
	c2 := istemci(yol2)
	c2.CagriSiniri = 300 * time.Millisecond
	basla := time.Now()
	_, err := c2.Cagir(context.Background(), Istek{Kimlik: kokKimlik, Yol: "/api/v1/domains/42"})
	if !errors.Is(err, ErrYok) || time.Since(basla) > 2*time.Second {
		t.Fatalf("takılan panel: err=%v süre=%v", err, time.Since(basla))
	}
}

// Köprü sonradan gelirse AYNI istemci yeniden başlatılmadan çalışır.
func TestKopruSonradanGelinceToparlanir(t *testing.T) {
	yol := filepath.Join(kisaDizin(t), "s", "k.sock")
	c := istemci(yol)
	if err := c.Ping(context.Background()); !errors.Is(err, ErrYok) {
		t.Fatalf("başta ErrYok beklenir: %v", err)
	}
	s := &Sunucu{Router: &kokRouter{}, Yol: yol, IzinliUID: uint32(os.Geteuid())}
	ctx, iptal := context.WithCancel(context.Background())
	bitti := make(chan error, 1)
	go func() { bitti <- s.Dinle(ctx) }()
	defer func() { iptal(); <-bitti }()
	var son error
	for i := 0; i < 100; i++ {
		if son = c.Ping(context.Background()); son == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("köprü gelince toparlanmadı: %v", son)
}

// Boşlukla şişirilmiş AMA geçerli JSON istek de boyut sınırında reddedilir (sınır JSON değil bayt).
func TestKopruIstekBoyutSiniriBayt(t *testing.T) {
	r := &kokRouter{}
	_, yol := sunucuBaslat(t, r, nil)
	govde := `{"kimlik":{"Rol":"kok","KullaniciID":1,"Kullanici":"root","TgKisa":"t"},"yol":"/api/v1/domains/42"}` + strings.Repeat(" ", IstekSiniri)
	if y := hamIstek(t, yol, []byte(govde)); y.Kod != 0 || y.Hata == "" {
		t.Fatalf("boyut sınırı aşan istek geçti: %+v", y)
	}
	if r.cagri.Load() != 0 {
		t.Fatal("yönlendirici çağrıldı")
	}
}

// Sorgu katı çözülür: bozuk kaçış, çok değer, kontrol baytı reddedilir; geçerli sorgu yeniden kodlanır.
func TestKopruSorguTemizleme(t *testing.T) {
	r := &kokRouter{}
	_, yol := sunucuBaslat(t, r, nil)
	kim := `"kimlik":{"Rol":"kok","KullaniciID":1,"Kullanici":"root","TgKisa":"t"}`
	for ad, sorgu := range map[string]string{
		"bozuk kaçış":  `x=%zz`,
		"çok değer":    `a=1&a=2&a=3&a=4&a=5&a=6&a=7&a=8&a=9`,
		"boş anahtar":  `=1`,
		"nokta virgül": `a=1;b=2`,
	} {
		if y := hamIstek(t, yol, []byte(`{`+kim+`,"yol":"/api/v1/domains/42","sorgu":"`+sorgu+`"}`)); y.Kod != 0 {
			t.Errorf("%s: sorgu geçti: %+v", ad, y)
		}
	}
	if r.cagri.Load() != 0 {
		t.Fatal("reddedilen sorgularda yönlendirici çağrıldı")
	}
	y := hamIstek(t, yol, []byte(`{`+kim+`,"yol":"/api/v1/domains/42","sorgu":"son=5&dosya=a%20b"}`))
	var g struct{ Sorgu string }
	_ = json.Unmarshal(y.Govde, &g)
	if y.Kod != 200 || g.Sorgu != "dosya=a+b&son=5" {
		t.Fatalf("geçerli sorgu yeniden kodlanmadı: %d %q", y.Kod, g.Sorgu)
	}
}

// Var olan gevşek izinli dizin 0700'e sıkılaştırılır.
func TestKopruDizinSikilastirilir(t *testing.T) {
	d := filepath.Join(kisaDizin(t), "s")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(d, 0o755)
	sunucuBaslatYol(t, filepath.Join(d, "k.sock"))
	if st, _ := os.Stat(d); st.Mode().Perm() != 0o700 {
		t.Fatalf("dizin sıkılaştırılmadı: %v", st.Mode().Perm())
	}
}

func sunucuBaslatYol(t *testing.T, yol string) {
	t.Helper()
	s := &Sunucu{Router: &kokRouter{}, Yol: yol, IzinliUID: uint32(os.Geteuid())}
	ctx, iptal := context.WithCancel(context.Background())
	bitti := make(chan error, 1)
	go func() { bitti <- s.Dinle(ctx) }()
	t.Cleanup(func() { iptal(); <-bitti })
	for i := 0; i < 200; i++ {
		if _, err := os.Lstat(yol); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("soket kurulmadı")
}

// Soket dizini başka bir kullanıcıya aitse köprü KURULMAZ (dizin sahibi soketi değiştirebilirdi).
func TestKopruYabanciSahipliDizinReddedilir(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("chown için kök gerekir")
	}
	d := filepath.Join(kisaDizin(t), "s")
	if err := os.MkdirAll(d, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(d, 4242, 4242); err != nil {
		t.Skip("chown yapılamadı")
	}
	s := &Sunucu{Router: &kokRouter{}, Yol: filepath.Join(d, "k.sock")}
	if err := s.Dinle(context.Background()); err == nil {
		t.Fatal("yabancı sahipli dizinde köprü kuruldu")
	}
}
