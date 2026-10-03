package hedef_test

// bagimlilik_test.go — Y0 bağımlılık duman testleri.
//
// NEDEN BU DOSYA: sonraki dalganın (Y1) kütüphaneleri go.mod/go.sum'a TEK
// sahipte (Y0) eklendi; henüz hiçbir üretim paketi onları içe aktarmıyor ve
// `go mod tidy` onları silerdi. Seçenekler:
//   - üretim kodunda blank import: kütüphane panel ikilisine bağlanır (boyut,
//     init yan etkileri) ve hiçbir şey sınanmaz → HAYIR;
//   - tools.go (derleme etiketli blank import): tidy'yi korur ama yine hiçbir
//     şey sınamaz → HAYIR;
//   - burada GERÇEK kullanım: tidy test içe aktarmalarını korur, hiçbiri üretim
//     ikilisine bağlanmaz ve Y1'in dayandığı API varsayımları (SSRF kapısından
//     geçen bağlantı, PKCE S256, SMB2 müzakeresi, age gidiş-dönüş) şimdiden
//     kanıtlanır → EVET.
// Y1 paketleri kendi içe aktarmalarını ekleyince bu dosya küçültülebilir.
// Ağa ÇIKILMAZ: tüm uçlar httptest / yerel dinleyici.

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	smb2 "github.com/oiweiwei/go-smb2.fork"
	"golang.org/x/oauth2"

	"girginospanel/internal/agkapi"
	"girginospanel/internal/backups/hedef"
)

var yerelAg = []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}

// sayacliTransport — kapıdan geçen, dial sayan Transport.
func sayacliTransport(k *agkapi.Kapi, sayac *atomic.Int64) *http.Transport {
	tr := k.HTTPTransport()
	ic := tr.DialContext
	tr.DialContext = func(ctx context.Context, n, a string) (net.Conn, error) {
		sayac.Add(1)
		return ic(ctx, n, a)
	}
	return tr
}

// S3: minio-go özel Transport'u (SSRF kapısı) kabul eder, SigV4 imzalar;
// katı kapı iç adresi reddeder ve istek sunucuya ULAŞMAZ.
func TestBagimlilik_MinioKapidanGecer(t *testing.T) {
	var (
		mu     sync.Mutex
		metot  string
		yol    string
		imza   string
		istekS atomic.Int64
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		istekS.Add(1)
		mu.Lock()
		metot, yol, imza = r.Method, r.URL.Path, r.Header.Get("Authorization")
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	ctx, iptal := context.WithTimeout(context.Background(), 30*time.Second)
	defer iptal()

	var dial atomic.Int64
	cl, err := minio.New(u.Host, &minio.Options{
		Creds:        credentials.NewStaticV4("SAHTEERISIMKIMLIGI", "sahte-gizli-anahtar", ""),
		Transport:    sayacliTransport(agkapi.Yeni(agkapi.Secenek{IzinliAglar: yerelAg}), &dial),
		Region:       "us-east-1",
		BucketLookup: minio.BucketLookupPath,
		MaxRetries:   1,
	})
	if err != nil {
		t.Fatal(err)
	}
	mevcut, err := cl.BucketExists(ctx, "yedekler")
	if err != nil || !mevcut {
		t.Fatalf("BucketExists: %v %v", mevcut, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if metot != http.MethodHead || !strings.HasPrefix(yol, "/yedekler") || !strings.HasPrefix(imza, "AWS4-HMAC-SHA256") {
		t.Errorf("istek: %s %s imza=%q", metot, yol, imza)
	}
	if dial.Load() == 0 {
		t.Error("bağlantı kapıdan geçmedi")
	}
	// Gerçek SigV4 imza değeri günlüğe maskelenmeden geçmez.
	if i := strings.Index(imza, "Signature="); i < 0 {
		t.Errorf("imza yok: %q", imza)
	} else if deger := imza[i+len("Signature="):]; strings.Contains(hedef.Maskele(imza), deger) {
		t.Errorf("SigV4 imzası maskelenmedi: %s", hedef.Maskele(imza))
	}

	// Katı kapı: loopback reddedilir, sunucuya istek gitmez, kategori erisim.
	onceki := istekS.Load()
	kati, err := minio.New(u.Host, &minio.Options{
		Creds:     credentials.NewStaticV4("SAHTEERISIMKIMLIGI", "sahte-gizli-anahtar", ""),
		Transport: agkapi.Yeni(agkapi.Secenek{}).HTTPTransport(), Region: "us-east-1",
		BucketLookup: minio.BucketLookupPath, MaxRetries: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = kati.BucketExists(ctx, "yedekler")
	if err == nil || istekS.Load() != onceki {
		t.Fatalf("katı kapı iç adresi reddetmeliydi: %v (istek %d→%d)", err, onceki, istekS.Load())
	}
	if k := hedef.KategoriAl(err); k != hedef.KategoriErisim {
		t.Errorf("ssrf reddi kategorisi %q: %v", k, err)
	}
}

// SMB: go-smb2.fork, SSRF kapısından alınmış bir net.Conn üzerinde SMB2
// müzakeresi başlatır (SMB1 DEĞİL: ilk paket \xfeSMB); sunucu koparsa hata
// döner ve parola hata metnine girmez.
func TestBagimlilik_SMB2KapidanGecenBaglanti(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	ilk := make(chan []byte, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			ilk <- nil
			return
		}
		defer c.Close()
		_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
		b := make([]byte, 8)
		if _, err := io.ReadFull(c, b); err != nil {
			ilk <- nil
			return
		}
		ilk <- b
	}()
	ctx, iptal := context.WithTimeout(context.Background(), 15*time.Second)
	defer iptal()
	k := agkapi.Yeni(agkapi.Secenek{IzinliAglar: yerelAg})
	conn, err := k.DialContext(ctx, "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("kapıdan bağlantı: %v", err)
	}
	defer conn.Close()
	const sir = "Smb-N0betci-Parola-9d1e"
	d := &smb2.Dialer{Initiator: &smb2.NTLMInitiator{User: "yedekci", Password: sir}}
	_, err = d.DialContext(ctx, conn)
	if err == nil {
		t.Fatal("sunucu koptuğu halde oturum kuruldu")
	}
	if strings.Contains(err.Error(), sir) || strings.Contains(hedef.Sarmala("baglan", err, sir).Error(), sir) {
		t.Fatalf("parola hata metninde: %v", err)
	}
	b := <-ilk
	if len(b) != 8 || binary.BigEndian.Uint32(b[:4])>>24 != 0 || string(b[4:8]) != "\xfeSMB" {
		t.Fatalf("ilk paket SMB2 müzakeresi değil: %x", b)
	}
	// Katı kapı iç adrese SMB bağlantısını açmaz.
	if _, err := agkapi.Yeni(agkapi.Secenek{}).DialContext(ctx, "tcp", ln.Addr().String()); !errors.Is(err, agkapi.ErrReddedildi) {
		t.Errorf("katı kapı: %v", err)
	}
}

// OAuth2: PKCE S256 yetki URL'si + kod değişimi kapıdan geçen istemciyle;
// istemci sırrı URL'de yok; invalid_grant kimlik kategorisine düşer.
func TestBagimlilik_OAuth2PKCE(t *testing.T) {
	var (
		mu     sync.Mutex
		alinan url.Values
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		mu.Lock()
		alinan = r.PostForm
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.PostForm.Get("code") == "eski-kod" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"invalid_grant","error_description":"Bad Request"}`)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "sahte-erisim", "token_type": "Bearer", "refresh_token": "sahte-yenileme", "expires_in": 3600,
		})
	}))
	defer srv.Close()
	conf := &oauth2.Config{
		ClientID: "istemci-kimligi", ClientSecret: "istemci-sirri",
		Endpoint:    oauth2.Endpoint{AuthURL: srv.URL + "/auth", TokenURL: srv.URL + "/token", AuthStyle: oauth2.AuthStyleInParams},
		RedirectURL: "https://panel.ornek.test:8443/yedek-hedefleri/oauth-donus",
		Scopes:      []string{"https://www.googleapis.com/auth/drive.file"},
	}
	dogrulayici := oauth2.GenerateVerifier()
	yetkiURL := conf.AuthCodeURL("durum-123", oauth2.AccessTypeOffline, oauth2.S256ChallengeOption(dogrulayici))
	yu, err := url.Parse(yetkiURL)
	if err != nil {
		t.Fatal(err)
	}
	q := yu.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") != oauth2.S256ChallengeFromVerifier(dogrulayici) ||
		q.Get("state") != "durum-123" || q.Get("access_type") != "offline" {
		t.Errorf("yetki URL'si: %s", yetkiURL)
	}
	if strings.Contains(yetkiURL, "istemci-sirri") || strings.Contains(yetkiURL, dogrulayici) {
		t.Errorf("yetki URL'sine sır/doğrulayıcı girdi: %s", yetkiURL)
	}

	var dial atomic.Int64
	hc := &http.Client{Transport: sayacliTransport(agkapi.Yeni(agkapi.Secenek{IzinliAglar: yerelAg}), &dial), Timeout: 10 * time.Second}
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, hc)
	tok, err := conf.Exchange(ctx, "yeni-kod", oauth2.VerifierOption(dogrulayici))
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	mu.Lock()
	if alinan.Get("code_verifier") != dogrulayici || alinan.Get("grant_type") != "authorization_code" {
		t.Errorf("jeton isteği: %v", alinan)
	}
	mu.Unlock()
	if tok.RefreshToken != "sahte-yenileme" || dial.Load() == 0 {
		t.Errorf("jeton: %+v dial=%d", tok, dial.Load())
	}
	_, err = conf.Exchange(ctx, "eski-kod", oauth2.VerifierOption(dogrulayici))
	if err == nil || hedef.KategoriAl(err) != hedef.KategoriKimlik {
		t.Errorf("invalid_grant kimlik kategorisi olmalı: %v", err)
	}
	if s := hedef.Sarmala("oauth", err, "istemci-sirri").Error(); strings.Contains(s, "invalid_grant") {
		t.Errorf("Error() ham metin taşımamalı: %q", s)
	}
}

// age: X25519 ve hibrit (ML-KEM-768 + X25519) gidiş-dönüş; yanlış anahtar ve
// 1 bayt bozulma reddedilir; gizli anahtar maskeleme deseniyle günlükten silinir.
//
// KARAR (Y1 sifrele): yeni anahtarlar HİBRİT üretilir. age v1.3 hibrit alıcıyı
// "standart age genel anahtarı" sayıyor; uzak yedekler uzun yaşar ("şimdi topla,
// sonra çöz" riski). Bedeli: alıcı ~2000 karakter (backup_sifre_anahtarlari.alici
// TEXT) ve kurtarma dosyasını elle açmak için age >= v1.3 gerekir. X25519 ve
// hibrit alıcılar AYNI dosyada karıştırılamaz (aşağıda sınanıyor): anahtar
// döndürmede eski anahtar yalnız çözmek için tutulur.
func TestBagimlilik_AgeGidisDonus(t *testing.T) {
	type anahtar struct {
		kimlik    age.Identity
		alici     age.Recipient
		gizli     string
		genel     string
		gizliOnek string
		genelOnek string
	}
	x25519 := func() anahtar {
		k, err := age.GenerateX25519Identity()
		if err != nil {
			t.Fatal(err)
		}
		return anahtar{k, k.Recipient(), k.String(), k.Recipient().String(), "AGE-SECRET-KEY-1", "age1"}
	}
	hibrit := func() anahtar {
		k, err := age.GenerateHybridIdentity()
		if err != nil {
			t.Fatal(err)
		}
		return anahtar{k, k.Recipient(), k.String(), k.Recipient().String(), "AGE-SECRET-KEY-PQ-1", "age1pq1"}
	}
	for _, tur := range []struct {
		ad  string
		yap func() anahtar
	}{{"x25519", x25519}, {"hibrit", hibrit}} {
		t.Run(tur.ad, func(t *testing.T) {
			a := tur.yap()
			if !strings.HasPrefix(a.genel, a.genelOnek) || !strings.HasPrefix(a.gizli, a.gizliOnek) {
				t.Fatalf("anahtar biçimi: %.20s / %.20s", a.genel, a.gizli)
			}
			t.Logf("%s: alıcı %d karakter, gizli anahtar %d karakter", tur.ad, len(a.genel), len(a.gizli))
			duz := bytes.Repeat([]byte("yedek-içeriği-ğüşıöç "), 50000) // ~1 MiB, birden çok age parçası
			var sifreli bytes.Buffer
			w, err := age.Encrypt(&sifreli, a.alici)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write(duz); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			coz := func(veri []byte, k age.Identity) ([]byte, error) {
				r, err := age.Decrypt(bytes.NewReader(veri), k)
				if err != nil {
					return nil, err
				}
				return io.ReadAll(r)
			}
			acik, err := coz(sifreli.Bytes(), a.kimlik)
			if err != nil || !bytes.Equal(acik, duz) {
				t.Fatalf("gidiş-dönüş: %v", err)
			}
			if _, err := coz(sifreli.Bytes(), tur.yap().kimlik); err == nil {
				t.Error("yanlış anahtarla çözüldü")
			}
			bozuk := bytes.Clone(sifreli.Bytes())
			bozuk[len(bozuk)/2] ^= 1
			if _, err := coz(bozuk, a.kimlik); err == nil {
				t.Error("1 bayt bozulmuş şifreli dosya çözüldü")
			}
			parca := a.gizli[len(a.gizli)-24:]
			if m := hedef.Maskele("anahtar: " + a.gizli); strings.Contains(m, parca) {
				t.Errorf("age gizli anahtarı maskelenmedi: %.60s", m)
			}
		})
	}
	if _, err := age.Encrypt(io.Discard, x25519().alici, hibrit().alici); err == nil {
		t.Error("X25519 ve hibrit alıcı aynı dosyada karıştırılabildi (kuantum korumasını düşürür)")
	}
}
