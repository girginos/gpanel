package gdrive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"girginospanel/internal/agkapi"
	"girginospanel/internal/backups/hedef"
	"girginospanel/internal/backups/hedef/hedeftest"
)

// Nöbetçi sırlar: HİÇBİR hata metnine/ayrıntıya sızmamalı (hedeftest.Uyum denetler).
const (
	nobetSecret   = "SAHTE-CLIENT-SECRET-0001"
	nobetYenileme = "1//SAHTE-YENILEME-JETONU-0001"
)

type ortam struct {
	f       *sahteDrive
	h       *driveHedef
	mu      sync.Mutex
	uykular []time.Duration
}

func (o *ortam) uykuSayisi() []time.Duration {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]time.Duration(nil), o.uykular...)
}

func yeniOrtam(t *testing.T) *ortam {
	t.Helper()
	f := yeniSahteDrive()
	t.Cleanup(f.kapat)
	return ortamKur(t, f)
}

func ortamKur(t *testing.T, f *sahteDrive) *ortam {
	t.Helper()
	kapi := agkapi.Yeni(agkapi.Secenek{IzinliAglar: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}})
	hc := yeniHTTP(hedef.KurSecenek{Kapi: kapi}, func(string) bool { return true }, true)
	h := yeni(Ayar{ClientID: f.clientID, Klasor: "GirginOS Test"}, Kimlik{ClientID: f.clientID, ClientSecret: nobetSecret},
		nobetYenileme, f.uclar(), hc, nil)
	o := &ortam{f: f, h: h}
	h.uyku = func(ctx context.Context, d time.Duration) error { // gerçek bekleme YOK
		o.mu.Lock()
		o.uykular = append(o.uykular, d)
		o.mu.Unlock()
		return ctx.Err()
	}
	return o
}

func yerelDosya(t *testing.T, boyut int, tohum byte) (string, []byte) {
	t.Helper()
	b := make([]byte, boyut)
	for i := range b {
		b[i] = byte(i*7) ^ tohum ^ byte(i>>8)
	}
	y := filepath.Join(t.TempDir(), "k.bin")
	if err := os.WriteFile(y, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return y, b
}

func shaHex(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// ─── Sözleşme ───────────────────────────────────────────────────────────────

func TestUyum(t *testing.T) {
	o := yeniOrtam(t)
	hedeftest.Uyum(t, o.h, nobetSecret, nobetYenileme, "ya29.SAHTE-ERISIM-1")
}

// ─── Yükle → listele → indir → sha → sil ────────────────────────────────────

func TestYukleListeleIndirSil(t *testing.T) {
	o := yeniOrtam(t)
	ctx := context.Background()
	yerel, veri := yerelDosya(t, 20<<20+321, 1) // 8 MiB parçalarla 3 parça
	ad := "2026-10-05/c_ornek-20261005-120000.tar.gz.age"
	var son int64
	if err := o.h.Yukle(ctx, yerel, ad, hedef.YukleSecenek{Sha256: shaHex(veri), Ilerleme: func(n int64) { son = n }}); err != nil {
		t.Fatalf("Yukle: %v", err)
	}
	if son != int64(len(veri)) {
		t.Errorf("son ilerleme %d, beklenen %d", son, len(veri))
	}
	if got := o.f.say("PUT", "/upload/session/"); got != 3 {
		t.Errorf("parça PUT sayısı %d, beklenen 3 (8 MiB parçalar)", got)
	}
	liste, err := o.h.Listele(ctx, "2026-10-05/")
	if err != nil || len(liste) != 1 || liste[0].Ad != ad || liste[0].Boyut != int64(len(veri)) {
		t.Fatalf("Listele = %+v, %v", liste, err)
	}
	inen := filepath.Join(t.TempDir(), "inen")
	if err := o.h.Indir(ctx, ad, inen); err != nil {
		t.Fatalf("Indir: %v", err)
	}
	got, _ := os.ReadFile(inen)
	if shaHex(got) != shaHex(veri) {
		t.Errorf("indirilen sha256 uyuşmuyor")
	}
	// appProperties.sha256 yazıldı (sağlayıcı tarafında yerel özet kaydı).
	if d := o.f.dosyaVar(filepath.Base(ad)); len(d) != 1 || d[0].Ozellik["sha256"] != shaHex(veri) {
		t.Errorf("dosya özelliği sha256 yazılmadı: %+v", d)
	}
	if err := o.h.Sil(ctx, ad); err != nil {
		t.Fatalf("Sil: %v", err)
	}
	if o.f.dosyaSayisi() != 0 {
		t.Errorf("silindikten sonra dosya kaldı")
	}
	if err := o.h.Sil(ctx, ad); err != nil {
		t.Errorf("ikinci Sil: %v", err)
	}
}

func TestYanlisSha256Yuklemez(t *testing.T) {
	o := yeniOrtam(t)
	yerel, _ := yerelDosya(t, 5000, 2)
	err := o.h.Yukle(context.Background(), yerel, "a/x.age", hedef.YukleSecenek{Sha256: strings.Repeat("0", 64)})
	if err == nil {
		t.Fatal("yanlış sha256 ile Yukle başarı döndü")
	}
	if o.f.say("POST", "/upload/") != 0 || o.f.dosyaSayisi() != 0 {
		t.Errorf("özet uyuşmazlığında yükleme oturumu açıldı / dosya oluştu")
	}
}

func TestBozukAktarimSilinir(t *testing.T) {
	o := yeniOrtam(t)
	o.f.ayarla(func(f *sahteDrive) { f.md5Boz = true })
	yerel, _ := yerelDosya(t, 3000, 3)
	err := o.h.Yukle(context.Background(), yerel, "a/x.age", hedef.YukleSecenek{})
	if err == nil || hedef.KategoriAl(err) != hedef.KategoriAg {
		t.Fatalf("md5 uyuşmazlığı ag hatası olmalı: %v", err)
	}
	if o.f.dosyaSayisi() != 0 {
		t.Errorf("bozuk yüklenen yeni dosya silinmedi")
	}
}

func TestUstuneYazmaAyniDosya(t *testing.T) {
	o := yeniOrtam(t)
	ctx := context.Background()
	y1, _ := yerelDosya(t, 1000, 4)
	y2, v2 := yerelDosya(t, 2000, 5)
	if err := o.h.Yukle(ctx, y1, "d/x.age", hedef.YukleSecenek{}); err != nil {
		t.Fatal(err)
	}
	if err := o.h.Yukle(ctx, y2, "d/x.age", hedef.YukleSecenek{}); err != nil {
		t.Fatal(err)
	}
	d := o.f.dosyaVar("x.age")
	if len(d) != 1 || shaHex(d[0].Veri) != shaHex(v2) {
		t.Fatalf("üzerine yazma tek dosya + yeni içerik vermeli: %d dosya", len(d))
	}
	if o.f.say("PATCH", "/upload/drive/v3/files/") != 1 {
		t.Errorf("mevcut dosya PATCH ile güncellenmeliydi")
	}
}

func TestSayfaliListe(t *testing.T) {
	o := yeniOrtam(t)
	o.f.ayarla(func(f *sahteDrive) { f.sayfaBoyutu = 2 })
	ctx := context.Background()
	y, _ := yerelDosya(t, 10, 6)
	for _, a := range []string{"a1", "a2", "a3", "a4", "a5"} {
		if err := o.h.Yukle(ctx, y, "d/"+a, hedef.YukleSecenek{}); err != nil {
			t.Fatal(err)
		}
	}
	l, err := o.h.Listele(ctx, "d/")
	if err != nil || len(l) != 5 {
		t.Fatalf("sayfalı liste %d öğe, hata %v (beklenen 5)", len(l), err)
	}
	if o.f.say("GET", "/drive/v3/files") < 3 {
		t.Errorf("sayfalama yapılmadı")
	}
}

func TestSifirBaytDosya(t *testing.T) {
	o := yeniOrtam(t)
	y := filepath.Join(t.TempDir(), "bos")
	_ = os.WriteFile(y, nil, 0o600)
	ctx := context.Background()
	if err := o.h.Yukle(ctx, y, "d/bos.age", hedef.YukleSecenek{}); err != nil {
		t.Fatalf("0 baytlık Yukle: %v", err)
	}
	inen := filepath.Join(t.TempDir(), "i")
	if err := o.h.Indir(ctx, "d/bos.age", inen); err != nil {
		t.Fatalf("Indir: %v", err)
	}
}

// ─── Kopan yüklemeyi sürdürme ───────────────────────────────────────────────

func TestKopanYuklemeSurdurulur(t *testing.T) {
	o := yeniOrtam(t)
	yerel, veri := yerelDosya(t, 20<<20+7, 7)
	// İlk parça sağlam; İKİNCİ parça gövdesi 3 MiB + 100 bayt okununca bağlantı kopar.
	o.f.ayarla(func(f *sahteDrive) {
		f.hook = func(f *sahteDrive, r *http.Request, n int) *hookYanit {
			if strings.HasPrefix(r.URL.Path, "/upload/session/") && strings.HasPrefix(r.Header.Get("Content-Range"), "bytes 8388608-") && f.kopOfset == 0 && f.sayimlar["PUT "+r.URL.Path] == 2 {
				f.kopOfset = 3<<20 + 100
			}
			return nil
		}
	})
	var ilerlemeler []int64
	var mu sync.Mutex
	err := o.h.Yukle(context.Background(), yerel, "d/buyuk.age", hedef.YukleSecenek{Sha256: shaHex(veri),
		Ilerleme: func(n int64) { mu.Lock(); ilerlemeler = append(ilerlemeler, n); mu.Unlock() }})
	if err != nil {
		t.Fatalf("kopmadan sonra sürdürülemedi: %v", err)
	}
	if n := o.f.oturumAcmaSayisi; n != 1 {
		t.Errorf("oturum %d kez açıldı: yükleme baştan değil SÜRDÜRÜLMELİ (1 oturum)", n)
	}
	d := o.f.dosyaVar("buyuk.age")
	if len(d) != 1 || shaHex(d[0].Veri) != shaHex(veri) {
		t.Fatalf("sürdürülen yükleme içeriği bozuk")
	}
	if len(o.uykuSayisi()) == 0 {
		t.Errorf("kopmadan sonra geri çekilme beklemesi yapılmadı")
	}
	// Durum sorgusu (bytes */N) yapıldı.
	var durum bool
	o.f.ayarla(func(f *sahteDrive) { durum = len(f.istekler) > 0 })
	if !durum {
		t.Fatal("istek yok")
	}
}

func TestOturumSuresiDolarYenidenBaslar(t *testing.T) {
	o := yeniOrtam(t)
	yerel, veri := yerelDosya(t, 9<<20, 8)
	o.f.ayarla(func(f *sahteDrive) {
		f.hook = func(f *sahteDrive, r *http.Request, n int) *hookYanit {
			if strings.HasPrefix(r.URL.Path, "/upload/session/") && f.sayimlar["PUT "+r.URL.Path] == 2 && f.oturumAcmaSayisi == 1 {
				return &hookYanit{Kod: 404, Govde: `{"error":{"code":404,"errors":[{"reason":"notFound"}]}}`}
			}
			return nil
		}
	})
	if err := o.h.Yukle(context.Background(), yerel, "d/x.age", hedef.YukleSecenek{}); err != nil {
		t.Fatalf("oturum düşünce yeniden başlamalı: %v", err)
	}
	if o.f.oturumAcmaSayisi != 2 {
		t.Errorf("oturum sayısı %d, beklenen 2", o.f.oturumAcmaSayisi)
	}
	if d := o.f.dosyaVar("x.age"); len(d) != 1 || shaHex(d[0].Veri) != shaHex(veri) {
		t.Errorf("içerik bozuk")
	}
}

func TestIndirmeKopmasiYenidenDener(t *testing.T) {
	o := yeniOrtam(t)
	ctx := context.Background()
	yerel, veri := yerelDosya(t, 200000, 9)
	if err := o.h.Yukle(ctx, yerel, "d/x.age", hedef.YukleSecenek{}); err != nil {
		t.Fatal(err)
	}
	o.f.ayarla(func(f *sahteDrive) { f.indirKop = 1 })
	inen := filepath.Join(t.TempDir(), "i")
	if err := o.h.Indir(ctx, "d/x.age", inen); err != nil {
		t.Fatalf("Indir: %v", err)
	}
	got, _ := os.ReadFile(inen)
	if shaHex(got) != shaHex(veri) {
		t.Errorf("indirme içeriği bozuk")
	}
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(inen), ".*indiriliyor*"))
	if len(matches) != 0 {
		t.Errorf("geçici indirme dosyası kaldı: %v", matches)
	}
}

// ─── Jeton: 401 → yenile, invalid_grant ────────────────────────────────────

func Test401TokenYenilenirVeSurer(t *testing.T) {
	o := yeniOrtam(t)
	ctx := context.Background()
	y, _ := yerelDosya(t, 100, 10)
	if err := o.h.Yukle(ctx, y, "d/a.age", hedef.YukleSecenek{}); err != nil {
		t.Fatal(err)
	}
	ilk := o.f.yenilemeSayisi
	// Tüm erişim jetonlarını sunucuda düşür: sonraki istek 401 alır, istemci yeniler.
	o.f.ayarla(func(f *sahteDrive) { f.gecerli = map[string]bool{} })
	if _, err := o.h.Listele(ctx, "d/"); err != nil {
		t.Fatalf("401 sonrası yenileyip sürmeliydi: %v", err)
	}
	if o.f.yenilemeSayisi != ilk+1 {
		t.Errorf("yenileme sayısı %d, beklenen %d", o.f.yenilemeSayisi, ilk+1)
	}
}

func Test401YuklemeParcasindaYenilenir(t *testing.T) {
	o := yeniOrtam(t)
	yerel, veri := yerelDosya(t, 17<<20, 11)
	o.f.ayarla(func(f *sahteDrive) {
		f.hook = func(f *sahteDrive, r *http.Request, n int) *hookYanit {
			if strings.HasPrefix(r.URL.Path, "/upload/session/") && f.sayimlar["PUT "+r.URL.Path] == 2 && len(f.gecerli) > 0 {
				f.gecerli = map[string]bool{}
				return &hookYanit{Kod: 401, Govde: `{"error":{"code":401,"errors":[{"reason":"authError"}]}}`}
			}
			return nil
		}
	})
	if err := o.h.Yukle(context.Background(), yerel, "d/x.age", hedef.YukleSecenek{}); err != nil {
		t.Fatalf("parça 401 sonrası yenileyip sürmeliydi: %v", err)
	}
	if d := o.f.dosyaVar("x.age"); len(d) != 1 || shaHex(d[0].Veri) != shaHex(veri) {
		t.Errorf("içerik bozuk")
	}
	if o.f.yenilemeSayisi < 2 {
		t.Errorf("jeton yenilenmedi")
	}
}

func TestInvalidGrantKimlikKategorisi(t *testing.T) {
	o := yeniOrtam(t)
	o.f.ayarla(func(f *sahteDrive) { f.gecersizYenile = true }) // testing modunda 7 gün sonra
	_, err := o.h.Listele(context.Background(), "d/")
	if err == nil || hedef.KategoriAl(err) != hedef.KategoriKimlik {
		t.Fatalf("invalid_grant kimlik kategorisi olmalı: %v", err)
	}
	sirYok(t, err, nobetSecret, nobetYenileme)
}

func sirYok(t *testing.T, err error, sirlar ...string) {
	t.Helper()
	var h *hedef.Hata
	if !errors.As(err, &h) {
		t.Fatalf("hata *hedef.Hata değil: %T", err)
	}
	for _, s := range sirlar {
		if strings.Contains(h.Error(), s) || strings.Contains(h.Ayrinti(), s) {
			t.Errorf("hata metnine nöbetçi sır SIZDI")
		}
	}
	if strings.Contains(h.Ayrinti(), "upload_id") || strings.Contains(h.Ayrinti(), "GIZLI-OTURUM") {
		t.Errorf("hata ayrıntısına yükleme oturumu adresi sızdı: %s", h.Ayrinti())
	}
}

// ─── Kota / 403 / 429 ───────────────────────────────────────────────────────

func TestKotaOnKontrolYuklemeyeBaslamaz(t *testing.T) {
	o := yeniOrtam(t)
	o.f.ayarla(func(f *sahteDrive) { f.kotaLimit, f.kotaKullanim = "10000", "9000" })
	y, _ := yerelDosya(t, 5000, 12)
	err := o.h.Yukle(context.Background(), y, "d/x.age", hedef.YukleSecenek{})
	if err == nil || hedef.KategoriAl(err) != hedef.KategoriKota {
		t.Fatalf("kota yetersiz: kategori kota olmalı: %v", err)
	}
	if o.f.say("POST", "/upload/") != 0 {
		t.Errorf("kota yetersizken yükleme oturumu açıldı")
	}
	// Yeterli yer: geçer.
	o.f.ayarla(func(f *sahteDrive) { f.kotaKullanim = "1000" })
	if err := o.h.Yukle(context.Background(), y, "d/x.age", hedef.YukleSecenek{}); err != nil {
		t.Fatalf("yeterli kotada Yukle: %v", err)
	}
}

func Test403StorageQuotaYenidenDenenmez(t *testing.T) {
	o := yeniOrtam(t)
	o.f.ayarla(func(f *sahteDrive) {
		f.hook = func(f *sahteDrive, r *http.Request, n int) *hookYanit {
			if r.URL.Path == "/upload/drive/v3/files" {
				return &hookYanit{Kod: 403, Govde: `{"error":{"code":403,"message":"ham mesaj ` + nobetSecret + `","errors":[{"reason":"storageQuotaExceeded"}]}}`}
			}
			return nil
		}
	})
	y, _ := yerelDosya(t, 100, 13)
	err := o.h.Yukle(context.Background(), y, "d/x.age", hedef.YukleSecenek{})
	if err == nil || hedef.KategoriAl(err) != hedef.KategoriKota {
		t.Fatalf("403 storageQuotaExceeded kota olmalı: %v", err)
	}
	if n := o.f.say("POST", "/upload/drive/v3/files"); n != 1 {
		t.Errorf("kota hatası %d kez denendi (yeniden denenmemeli)", n)
	}
	sirYok(t, err, nobetSecret) // ham "message" hiç alınmaz
}

func Test403YetkiYoksaErisim(t *testing.T) {
	o := yeniOrtam(t)
	o.f.ayarla(func(f *sahteDrive) {
		f.hook = func(f *sahteDrive, r *http.Request, n int) *hookYanit {
			if r.URL.Path == "/drive/v3/files" {
				return &hookYanit{Kod: 403, Govde: `{"error":{"code":403,"errors":[{"reason":"insufficientPermissions"}]}}`}
			}
			return nil
		}
	})
	_, err := o.h.Listele(context.Background(), "d/")
	if hedef.KategoriAl(err) != hedef.KategoriErisim {
		t.Fatalf("insufficientPermissions erişim olmalı: %v", err)
	}
}

func Test429RetryAfterVeUstelGeriCekilme(t *testing.T) {
	// Retry-After saniyesi aynen beklenir.
	o := yeniOrtam(t)
	o.f.ayarla(func(f *sahteDrive) {
		f.hook = func(f *sahteDrive, r *http.Request, n int) *hookYanit {
			if r.URL.Path == "/drive/v3/files" && f.sayimlar["GET /drive/v3/files"] <= 3 {
				return &hookYanit{Kod: 429, Baslik: map[string]string{"Retry-After": "7"}, Govde: `{"error":{"code":429,"errors":[{"reason":"rateLimitExceeded"}]}}`}
			}
			return nil
		}
	})
	if _, err := o.h.Listele(context.Background(), "d/"); err != nil {
		t.Fatalf("429 sonrası sürmeliydi: %v", err)
	}
	u := o.uykuSayisi()
	if len(u) != 3 {
		t.Fatalf("beklemeler %v, beklenen 3 adet", u)
	}
	for _, d := range u {
		if d != 7*time.Second {
			t.Errorf("Retry-After 7 sn beklenirdi, bekleme %v", d)
		}
	}
	// Retry-After yoksa üstel: her bekleme bir öncekinden büyük (taban 1 sn, 2, 4 + jitter).
	o2 := yeniOrtam(t)
	o2.f.ayarla(func(f *sahteDrive) {
		f.hook = func(f *sahteDrive, r *http.Request, n int) *hookYanit {
			if r.URL.Path == "/drive/v3/files" && f.sayimlar["GET /drive/v3/files"] <= 3 {
				return &hookYanit{Kod: 503, Govde: `{"error":{"code":503,"errors":[{"reason":"backendError"}]}}`}
			}
			return nil
		}
	})
	if _, err := o2.h.Listele(context.Background(), "d/"); err != nil {
		t.Fatalf("503 sonrası sürmeliydi: %v", err)
	}
	u2 := o2.uykuSayisi()
	if len(u2) != 3 || !(u2[0] >= time.Second && u2[1] >= 2*time.Second && u2[2] >= 4*time.Second && u2[1] > u2[0] && u2[2] > u2[1]) {
		t.Errorf("üstel geri çekilme beklenirdi (≥1s,≥2s,≥4s artan): %v", u2)
	}
}

func Test429SonsuzDonmez(t *testing.T) {
	o := yeniOrtam(t)
	o.f.ayarla(func(f *sahteDrive) {
		f.hook = func(f *sahteDrive, r *http.Request, n int) *hookYanit {
			if r.URL.Path == "/drive/v3/files" {
				return &hookYanit{Kod: 429, Govde: `{"error":{"code":429,"errors":[{"reason":"rateLimitExceeded"}]}}`}
			}
			return nil
		}
	})
	_, err := o.h.Listele(context.Background(), "d/")
	if hedef.KategoriAl(err) != hedef.KategoriOran {
		t.Fatalf("kalıcı 429 oran kategorisi olmalı: %v", err)
	}
	if n := o.f.say("GET", "/drive/v3/files"); n != azamiDeneme+1 {
		t.Errorf("istek sayısı %d, beklenen %d", n, azamiDeneme+1)
	}
}

// ─── SSRF kapısı / alan adı izin listesi ────────────────────────────────────

func TestHostIzinliMi(t *testing.T) {
	izinli := []string{"www.googleapis.com", "oauth2.googleapis.com", "accounts.google.com", "WWW.GOOGLEAPIS.COM", "googleapis.com", "www.googleapis.com."}
	yasak := []string{"", "evil.example.com", "googleapis.com.evil.com", "evilgoogleapis.com", "notgoogleapis.com",
		"accounts.google.com.evil.com", "www.google.com", "127.0.0.1", "169.254.169.254", "::1", "metadata.google.internal", "google.com"}
	for _, h := range izinli {
		if !HostIzinliMi(h) {
			t.Errorf("izinli olmalı: %q", h)
		}
	}
	for _, h := range yasak {
		if HostIzinliMi(h) {
			t.Errorf("YASAK olmalı: %q", h)
		}
	}
}

// Üretim izin listesi + sahte sunucu: yabancı adres sunucuya HİÇ ulaşmaz.
func TestYabanciAdresSunucuyaUlasmaz(t *testing.T) {
	f := yeniSahteDrive()
	defer f.kapat()
	kapi := agkapi.Yeni(agkapi.Secenek{IzinliAglar: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}})
	// Üretim izin listesi (HostIzinliMi) + http'ye izin YOK; taban kapı yerel adrese açık:
	// koruma yalnız alan adı izin listesinden gelir.
	hc := &http.Client{Transport: &kapiTransport{taban: kapi.HTTPTransport(), hostIzin: HostIzinliMi, httpIzin: true}}
	h := yeni(Ayar{ClientID: f.clientID, Klasor: "x"}, Kimlik{ClientID: f.clientID, ClientSecret: nobetSecret}, nobetYenileme, f.uclar(), hc, nil)
	h.uyku = func(ctx context.Context, d time.Duration) error { return ctx.Err() }
	_, err := h.Listele(context.Background(), "d/")
	if err == nil || !errors.Is(err, agkapi.ErrReddedildi) {
		t.Fatalf("izin dışı alan adı reddedilmeli (ErrReddedildi): %v", err)
	}
	if hedef.KategoriAl(err) != hedef.KategoriErisim {
		t.Errorf("kategori erisim olmalı: %v", hedef.KategoriAl(err))
	}
	f.mu.Lock()
	n := len(f.istekler)
	f.mu.Unlock()
	if n != 0 {
		t.Errorf("izin dışı adres sahte sunucuya %d istek ulaştırdı", n)
	}
}

func TestHTTPSZorunlu(t *testing.T) {
	f := yeniSahteDrive()
	defer f.kapat()
	hc := yeniHTTP(hedef.KurSecenek{}, func(string) bool { return true }, false) // http yasak
	req, _ := http.NewRequest("GET", f.srv.URL+"/drive/v3/about", nil)
	if _, err := hc.Do(req); err == nil || !errors.Is(err, agkapi.ErrReddedildi) {
		t.Fatalf("düz http reddedilmeli: %v", err)
	}
}

func TestKapiIcAdresiReddeder(t *testing.T) {
	// Katı kapı: izin listesi açık olsa bile loopback/özel adres reddedilir.
	f := yeniSahteDrive()
	defer f.kapat()
	hc := yeniHTTP(hedef.KurSecenek{}, func(string) bool { return true }, true)
	req, _ := http.NewRequest("GET", f.srv.URL+"/drive/v3/about", nil)
	if _, err := hc.Do(req); err == nil || !errors.Is(err, agkapi.ErrReddedildi) {
		t.Fatalf("katı kapı loopback'i reddetmeli: %v", err)
	}
	for _, ip := range []string{"169.254.169.254", "10.0.0.1", "127.0.0.1"} {
		if agkapi.Yeni(agkapi.Secenek{}).Denetle(net.ParseIP(ip)) == nil {
			t.Errorf("%s kapıdan geçti", ip)
		}
	}
}

// ─── Kapsam / ayar ──────────────────────────────────────────────────────────

func TestKapsamYalnizDriveFile(t *testing.T) {
	if Kapsam != "https://www.googleapis.com/auth/drive.file" {
		t.Fatalf("Kapsam değişti: %q", Kapsam)
	}
	f := yeniSahteDrive()
	defer f.kapat()
	o := ortamKur(t, f)
	_ = o
	oa := &oauthIstemci{kimlik: Kimlik{ClientID: f.clientID, ClientSecret: nobetSecret}, u: f.uclar(), http: o.h.http}
	if _, err := oa.cihazIste(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	got := f.istenenKapsam
	f.mu.Unlock()
	if got != "https://www.googleapis.com/auth/drive.file" {
		t.Errorf("cihaz akışında istenen kapsam %q: YALNIZ drive.file istenmeli", got)
	}
	if strings.Contains(got, " ") {
		t.Errorf("tek kapsam beklenirdi")
	}
}

func TestAyarBilinmeyenAlanReddedilir(t *testing.T) {
	gecerli := `{"client_id":"abc123456.apps.googleusercontent.com","klasor":"Yedek"}`
	if _, err := AyarCoz(json.RawMessage(gecerli)); err != nil {
		t.Fatalf("geçerli ayar: %v", err)
	}
	for _, kotu := range []string{
		`{"client_id":"abc123456.apps.googleusercontent.com","token_url":"https://evil.example/token"}`,
		`{"client_id":"abc123456.apps.googleusercontent.com","api":"http://169.254.169.254"}`,
		`{"client_id":"evil","klasor":"x"}`,
		`{"client_id":"abc123456.apps.googleusercontent.com","klasor":"a/b"}`,
		`{"client_id":"abc123456.apps.googleusercontent.com","klasor":" x"}`,
		`not json`,
	} {
		if _, err := AyarCoz(json.RawMessage(kotu)); err == nil {
			t.Errorf("geçersiz ayar KABUL edildi: %s", kotu)
		}
	}
	if a, _ := AyarCoz(json.RawMessage(`{"client_id":"abc123456.apps.googleusercontent.com"}`)); a.Klasor != VarsayilanKlasor {
		t.Errorf("varsayılan klasör uygulanmadı")
	}
}

func TestKayitDefteriKurucu(t *testing.T) {
	ayar := json.RawMessage(`{"client_id":"abc123456.apps.googleusercontent.com"}`)
	h, err := hedef.Kur(TurAdi, ayar, map[string]string{SirClientSecret: nobetSecret, SirYenileme: nobetYenileme}, hedef.KurSecenek{})
	if err != nil || h.Tur() != "drive" {
		t.Fatalf("kayıt defterinden kurulamadı: %v", err)
	}
	_, err = hedef.Kur(TurAdi, ayar, map[string]string{SirClientSecret: nobetSecret}, hedef.KurSecenek{})
	if err == nil || hedef.KategoriAl(err) != hedef.KategoriKimlik {
		t.Errorf("yenileme jetonsuz kurulum kimlik hatası vermeli: %v", err)
	}
	sirYok(t, err, nobetSecret)
}

// ─── Cihaz akışı ────────────────────────────────────────────────────────────

func oauthOrtam(t *testing.T) (*sahteDrive, *oauthIstemci) {
	t.Helper()
	f := yeniSahteDrive()
	t.Cleanup(f.kapat)
	o := ortamKur(t, f)
	return f, &oauthIstemci{kimlik: Kimlik{ClientID: f.clientID, ClientSecret: nobetSecret}, u: f.uclar(), http: o.h.http}
}

func TestCihazAkisi(t *testing.T) {
	f, oa := oauthOrtam(t)
	ctx := context.Background()
	c, err := oa.cihazIste(ctx)
	if err != nil || c.KullaniciKodu != "ABCD-EFGH" || c.CihazKodu == "" || c.Aralik < 5*time.Second {
		t.Fatalf("cihaz kodu: %+v %v", c, err)
	}
	if c.DogrulamaURL != "https://www.google.com/device" {
		t.Errorf("doğrulama adresi %q", c.DogrulamaURL)
	}
	for _, durum := range []struct {
		ayar string
		want SorguDurumu
	}{{"pending", SorguBekliyor}, {"slow", SorguYavasla}, {"denied", SorguReddedildi}, {"expired", SorguSuresiDoldu}} {
		f.ayarla(func(f *sahteDrive) { f.cihazDurum = durum.ayar })
		s, err := oa.cihazSorgula(ctx, c.CihazKodu)
		if err != nil || s.Durum != durum.want || s.YenilemeJetonu != "" {
			t.Errorf("%s: %+v %v", durum.ayar, s, err)
		}
	}
	f.ayarla(func(f *sahteDrive) { f.cihazDurum = "ok" })
	s, err := oa.cihazSorgula(ctx, c.CihazKodu)
	if err != nil || s.Durum != SorguTamam || s.YenilemeJetonu != "1//SAHTE-YENI-YENILEME-0002" {
		t.Fatalf("başarılı: %+v %v", s, err)
	}
}

func TestCihazDogrulamaAdresiYabanciOlamaz(t *testing.T) {
	f, oa := oauthOrtam(t)
	for _, kotu := range []string{"https://evil.example/phish", "http://www.google.com/device", "https://user:pw@www.google.com/device", "javascript:alert(1)", "https://google.com.evil.com/device"} {
		f.ayarla(func(f *sahteDrive) { f.dogrulamaURL = kotu })
		c, err := oa.cihazIste(context.Background())
		if err != nil || c.DogrulamaURL != "https://www.google.com/device" {
			t.Errorf("%q → %q (varsayılana düşmeli) %v", kotu, c.DogrulamaURL, err)
		}
	}
}

func TestCihazKapsamVerilmediyseReddeder(t *testing.T) {
	f, oa := oauthOrtam(t)
	f.ayarla(func(f *sahteDrive) {
		f.cihazDurum = "ok"
		f.cihazKapsam = "https://www.googleapis.com/auth/userinfo.email"
	})
	_, err := oa.cihazSorgula(context.Background(), "SAHTE-CIHAZ-KODU-GIZLI")
	if err == nil || hedef.KategoriAl(err) != hedef.KategoriErisim {
		t.Fatalf("drive.file verilmediyse reddedilmeli: %v", err)
	}
	sirYok(t, err, nobetSecret, "SAHTE-CIHAZ-KODU-GIZLI", "1//SAHTE-YENI-YENILEME-0002")
}

func TestYanlisIstemciKimlik(t *testing.T) {
	yanlisSir := strings.Repeat("y", 14)
	f, _ := oauthOrtam(t)
	o := ortamKur(t, f)
	oa := &oauthIstemci{kimlik: Kimlik{ClientID: f.clientID, ClientSecret: yanlisSir}, u: f.uclar(), http: o.h.http}
	_, err := oa.cihazSorgula(context.Background(), "x")
	if hedef.KategoriAl(err) != hedef.KategoriKimlik {
		t.Fatalf("invalid_client kimlik olmalı: %v", err)
	}
	sirYok(t, err, yanlisSir)
}

func TestJetonIptal(t *testing.T) {
	f, oa := oauthOrtam(t)
	if err := oa.iptal(context.Background(), nobetYenileme); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	n := len(f.iptalEdilen)
	f.mu.Unlock()
	if n != 1 {
		t.Errorf("iptal çağrısı %d", n)
	}
	// Zaten geçersiz jeton: başarı sayılır.
	f.ayarla(func(f *sahteDrive) { f.iptalYanit = 400 })
	if err := oa.iptal(context.Background(), nobetYenileme); err != nil {
		t.Errorf("zaten geçersiz jetonun iptali başarı sayılmalı: %v", err)
	}
}

// ─── Hata metni hijyeni ─────────────────────────────────────────────────────

func TestAgHatasiURLAyiklar(t *testing.T) {
	e := &url.Error{Op: "Put", URL: "https://www.googleapis.com/upload/drive/v3/files?uploadType=resumable&upload_id=GIZLI-OTURUM-KIMLIGI", Err: io.ErrUnexpectedEOF}
	got := agHatasi(e).Error()
	if strings.Contains(got, "GIZLI-OTURUM") || strings.Contains(got, "upload_id") {
		t.Errorf("taşıma hatası URL'yi sızdırıyor: %s", got)
	}
	if !errors.Is(agHatasi(e), io.ErrUnexpectedEOF) {
		t.Errorf("iç hata errors.Is ile tanınmalı")
	}
}

func TestIptalOrtadaOturumSizintisiYok(t *testing.T) {
	o := yeniOrtam(t)
	yerel, _ := yerelDosya(t, 9<<20, 14)
	ctx, iptal := context.WithCancel(context.Background())
	err := o.h.Yukle(ctx, yerel, "d/x.age", hedef.YukleSecenek{Ilerleme: func(n int64) { iptal() }})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("iptal: %v", err)
	}
	sirYok(t, err, nobetSecret, nobetYenileme)
	if o.f.dosyaSayisi() != 0 {
		t.Errorf("iptal edilen yükleme dosya bıraktı")
	}
}

func TestTestUcuncaKanit(t *testing.T) {
	o := yeniOrtam(t)
	if err := o.h.Test(context.Background()); err != nil {
		t.Fatalf("Test: %v", err)
	}
	if o.f.dosyaSayisi() != 0 {
		t.Errorf("Test dosya bıraktı")
	}
	// Yenileme jetonu geçersizken Test kimlik hatası verir.
	o.f.ayarla(func(f *sahteDrive) { f.gecersizYenile = true; f.gecerli = map[string]bool{} })
	o.h.jeton.Sifirla()
	if err := o.h.Test(context.Background()); hedef.KategoriAl(err) != hedef.KategoriKimlik {
		t.Errorf("Test kimlik hatası vermeli: %v", err)
	}
}
