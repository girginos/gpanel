package httpx

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// NginxtenMi yetki kararı verir (panel kapısı): kanıtsız isteğe GÜVENMEMELİ.
//
// 🔴 Bu testler makinedeki gerçek /etc/girginospanel/proxy.secret'ı ASLA okumaz: saf
// çekirdek (nginxtenMiGizli / proxyGizliOku) gizliyi/yolu parametre alır; ProxySecret'in
// önbelleği TestMain'de "kurulu değil" ile tüketilir. Eskiden pozitif test gizli yoksa
// atlanıyor, negatifler de neyi sınadığından bağımsız geçiyordu.

// testGizli: üretimdeki biçimde (64 hex) ama kaynakta sabit dize OLMAYAN test gizlisi.
var testGizli = strings.Repeat("ab", 32)

func TestMain(m *testing.M) {
	// ProxySecret'in sync.Once'ı dosyayı OKUMADAN tüketilir (proxyVal = "").
	proxyOnce.Do(func() {})
	os.Exit(m.Run())
}

// proxyGizliSabitle: ProxySecret önbelleğini g ile doldurur (dosya okunmaz); test
// bitince yine OKUMADAN "kurulu değil"e döner.
func proxyGizliSabitle(t *testing.T, g string) {
	t.Helper()
	proxyOnce = sync.Once{}
	proxyOnce.Do(func() { proxyVal = g })
	t.Cleanup(func() {
		proxyOnce = sync.Once{}
		proxyOnce.Do(func() { proxyVal = "" })
	})
}

func TestNginxtenMiGizli(t *testing.T) {
	// Yalnız SON karakteri farklı, aynı uzunlukta (testGizli "b" ile biter).
	yanlisAyniBoy := testGizli[:len(testGizli)-1] + "c"
	for _, tc := range []struct {
		ad      string
		uzak    string
		baslik  []string // nil = başlık YOK
		gizli   string
		beklene bool
	}{
		{"loopback4 + doğru gizli", "127.0.0.1:5555", []string{testGizli}, testGizli, true},
		{"loopback6 + doğru gizli", "[::1]:5555", []string{testGizli}, testGizli, true},
		{"127/8 + doğru gizli", "127.0.0.2:5555", []string{testGizli}, testGizli, true},

		{"dış adres + doğru gizli", "203.0.113.5:4444", []string{testGizli}, testGizli, false},
		{"özel ağ (loopback değil) + doğru gizli", "10.0.0.1:4444", []string{testGizli}, testGizli, false},
		{"dış IPv6 + doğru gizli", "[2001:db8::1]:4444", []string{testGizli}, testGizli, false},
		{"adressiz (unix soket) + doğru gizli", "", []string{testGizli}, testGizli, false},

		{"loopback + yanlış gizli (aynı boy)", "127.0.0.1:5555", []string{yanlisAyniBoy}, testGizli, false},
		{"loopback + gizlinin öneki", "127.0.0.1:5555", []string{testGizli[:32]}, testGizli, false},
		{"loopback + gizli + ek", "127.0.0.1:5555", []string{testGizli + "0"}, testGizli, false},
		{"loopback + başlık yok", "127.0.0.1:5555", nil, testGizli, false},
		{"loopback + boş başlık", "127.0.0.1:5555", []string{""}, testGizli, false},
		// nginx proxy_set_header istemcinin aynı adlı başlığını SİLİP tek değer yazar;
		// sona eklenmiş doğru değer yanlış olanı kurtarmamalı ("herhangi biri eşleşsin" YOK).
		{"loopback + çoklu başlık (yanlış, doğru)", "127.0.0.1:5555", []string{"kiraci-uydurmasi", testGizli}, testGizli, false},

		{"boş gizli + başlık yok", "127.0.0.1:5555", nil, "", false},
		{"boş gizli + boş başlık", "127.0.0.1:5555", []string{""}, "", false},
		{"boş gizli + herhangi başlık", "127.0.0.1:5555", []string{"x"}, "", false},
	} {
		t.Run(tc.ad, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tc.uzak
			for _, v := range tc.baslik {
				r.Header.Add("X-Gosp-Proxy", v)
			}
			if got := nginxtenMiGizli(r, tc.gizli); got != tc.beklene {
				t.Fatalf("nginxtenMiGizli = %v, beklenen %v", got, tc.beklene)
			}
		})
	}
}

// gizliVekilBasligi boş gizliyle ASLA eşleşmemeli (ConstantTimeCompare("", "") == 1).
func TestGizliVekilBasligiBosGizliEslesmez(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	if gizliVekilBasligi(r, "") {
		t.Fatal("başlıksız istek boş gizliyle eşleşti")
	}
	r.Header.Set("X-Gosp-Proxy", "")
	if gizliVekilBasligi(r, "") {
		t.Fatal("boş başlık boş gizliyle eşleşti")
	}
}

// NginxtenMi (üretim girişi) çekirdeğe ProxySecret'in değerini geçirmeli.
func TestNginxtenMiProxySecretKullanir(t *testing.T) {
	yeni := func(uzak, baslik string) bool {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = uzak
		if baslik != "" {
			r.Header.Set("X-Gosp-Proxy", baslik)
		}
		return NginxtenMi(r)
	}

	proxyGizliSabitle(t, testGizli)
	if !yeni("127.0.0.1:5555", testGizli) {
		t.Fatal("kurulu gizli + loopback nginx sayılmalı")
	}
	if yeni("127.0.0.1:5555", strings.Repeat("cd", 32)) {
		t.Fatal("başka gizli nginx sayılmamalı")
	}
	if yeni("203.0.113.5:4444", testGizli) {
		t.Fatal("dış adres doğru gizliyle bile nginx sayılmamalı")
	}

	proxyGizliSabitle(t, "") // gizli kurulu değil (ilk açılış / rollback)
	if yeni("127.0.0.1:5555", "") || yeni("127.0.0.1:5555", testGizli) {
		t.Fatal("gizli kurulu değilken hiçbir istek nginx sayılmamalı (ClientIP'in gevşek yedeği burada YOK)")
	}
}

func TestProxyGizliOku(t *testing.T) {
	dir := t.TempDir()
	yaz := func(ad, icerik string) string {
		t.Helper()
		yol := filepath.Join(dir, ad)
		if err := os.WriteFile(yol, []byte(icerik), 0o600); err != nil {
			t.Fatal(err)
		}
		return yol
	}
	if got := proxyGizliOku(filepath.Join(dir, "yok")); got != "" {
		t.Fatalf("olmayan dosya → \"\" beklenir, got %d karakter", len(got))
	}
	if got := proxyGizliOku(yaz("kisa", strings.Repeat("a", 31)+"\n")); got != "" {
		t.Fatalf("31 karakterlik gizli reddedilmeli, got %d karakter", len(got))
	}
	// Ham hâli 32'yi geçer ama kırpılınca 20: uzunluk KIRPILMIŞ değere bakmalı.
	if got := proxyGizliOku(yaz("bosluklu", strings.Repeat(" ", 20)+strings.Repeat("a", 20)+"\n")); got != "" {
		t.Fatalf("boşluklarla 32'yi geçen kısa gizli reddedilmeli, got %d karakter", len(got))
	}
	if got := proxyGizliOku(yaz("sinir", strings.Repeat("a", 32))); got != strings.Repeat("a", 32) {
		t.Fatal("tam 32 karakter kabul edilmeli")
	}
	if got := proxyGizliOku(yaz("tam", " "+testGizli+"\n")); got != testGizli {
		t.Fatal("gizli kırpılarak okunmalı")
	}
}
