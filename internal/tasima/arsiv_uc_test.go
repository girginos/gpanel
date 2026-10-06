package tasima

// arsiv_uc_test.go — arşiv uçları (yükleme, keşif, başlat): rol/anahtar kapıları, IDOR,
// kota, devam eden yükleme, asenkron keşif, sunucu tarafı oturum, başlatma kuralları.
// DB'li (GOSP_TEST_GECICI_DSN yoksa atlanır); iş yürütücüsü sahtedir (gerçek taşıma yok).

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/tasima/arsiv"
)

type tUye struct {
	ad, veri string
	dizin    bool
	tur      byte   // 0 = düzenli dosya (ya da dizin); tar.TypeSymlink, TypeChar ...
	hedef    string // bağ hedefi
	mod      int64
}

// tarGz — küçük bir cPanel yedeği (tar.gz) üretir.
func tarGz(t testing.TB, uyeler []tUye) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, u := range uyeler {
		h := &tar.Header{Name: u.ad, Mode: 0o644, ModTime: time.Unix(1_700_000_000, 0), Typeflag: tar.TypeReg, Size: int64(len(u.veri))}
		if u.mod != 0 {
			h.Mode = u.mod
		}
		switch {
		case u.dizin:
			h.Typeflag, h.Mode, h.Size = tar.TypeDir, 0o755, 0
		case u.tur != 0:
			h.Typeflag, h.Linkname, h.Size = u.tur, u.hedef, 0
			if u.tur == tar.TypeChar || u.tur == tar.TypeBlock {
				h.Devmajor, h.Devminor = 1, 3
			}
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if !u.dizin && u.tur == 0 {
			_, _ = tw.Write([]byte(u.veri))
		}
	}
	_ = tw.Close()
	_ = zw.Close()
	return buf.Bytes()
}

const (
	tDump = "CREATE TABLE `t` (`a` int);\nINSERT INTO `t` VALUES (1);\n-- Dump completed on 2026-10-06\n"
	tHash = "$6$GIZLIHASH$abcdefghijklmnop"
)

// cpmoveT — meşru küçük cPanel yedeği: ornek.com (ana) + ek.net (ek), 1 DB, 1 kutu, DNS, cron.
func cpmoveT(t testing.TB) []byte {
	k := "cpmove-u/"
	return tarGz(t, []tUye{
		{ad: k + "cp/u", veri: "USER=u\nDNS=ornek.com\nOWNER=baska-bayi\n"},
		{ad: k + "userdata/main", veri: "main_domain: ornek.com\naddon_domains:\n  ek.net: ek.net.ornek.com\n"},
		{ad: k + "userdata/ornek.com", veri: "documentroot: /home/u/public_html\n"},
		{ad: k + "userdata/ek.net.ornek.com", veri: "documentroot: /home/u/ek\n"},
		{ad: k + "mysql/u_wp.sql", veri: tDump},
		{ad: k + "dnszones/ornek.com.db", veri: "$TTL 14400\nornek.com. IN SOA ns1. h. (1 2 3 4 5)\n@ IN A 1.2.3.4\nwww IN CNAME @\n"},
		{ad: k + "cron/u", veri: "MAILTO=\"\"\n*/5 * * * * php /home/u/public_html/cron.php\n"},
		{ad: k + "homedir/public_html/index.php", veri: "<?php echo 1;"},
		{ad: k + "homedir/ek/a.txt", veri: "aaaa"},
		{ad: k + "homedir/mail/ornek.com/info/cur/1", veri: "mesaj"},
		{ad: k + "homedir/etc/ornek.com/passwd", veri: "info:" + tHash + ":1:2::/home/u/mail:\n"},
	})
}

type arsivOrt struct {
	*d3Ortam
	kok   string // izinli kök dizin (kök yolu için)
	dosya string
}

// arsivKur — d3 şeması + 0216, geçici yükleme kökü, geniş disk, kök yolu için izinli dizin;
// kök için arsiv-cpanel açık (bayi anahtarı KAPALI — varsayılan).
func arsivKur(t *testing.T) *arsivOrt {
	t.Helper()
	o := d3Kur(t)
	ao := &arsivOrt{d3Ortam: o}
	eskiK, eskiD, eskiU := arsivYukleKok, diskBosAlan, arsivSahipUID
	arsivYukleKok = filepath.Join(t.TempDir(), "arsiv")
	diskBosAlan = func(string) (int64, error) { return 1 << 50, nil }
	arsivSahipUID = uint32(os.Geteuid())
	t.Cleanup(func() { arsivYukleKok, diskBosAlan, arsivSahipUID = eskiK, eskiD, eskiU })
	ao.kok = t.TempDir()
	_ = os.Chmod(ao.kok, 0o755)
	ao.exec(t, `INSERT INTO cp_ayarlar (anahtar, deger) VALUES ('tasima_arsiv_dizinleri', ?) ON DUPLICATE KEY UPDATE deger=VALUES(deger)`, ao.kok)
	ao.dosya = filepath.Join(ao.kok, "cpmove-u.tar.gz")
	if err := os.WriteFile(ao.dosya, cpmoveT(t), 0o644); err != nil {
		t.Fatal(err)
	}
	ao.exec(t, `UPDATE users SET max_disk_mb=0`)
	if kod, g := ao.ist(t, "kok", http.MethodPut, "/tasima/kaynaklar/arsiv-cpanel", map[string]any{"etkin": true}); kod != 200 {
		t.Fatalf("kaynak açılamadı: %d %s", kod, g)
	}
	return ao
}

func (o *arsivOrt) router() http.Handler {
	h := o.h
	r := chi.NewRouter()
	r.Get("/tasima/kaynaklar", h.Kaynaklar)
	r.Put("/tasima/ayar", h.AyarGuncelle)
	r.Put("/tasima/kaynaklar/{kod}", h.KaynakGuncelle)
	r.Get("/tasima/oturumlar", h.OturumListe)
	r.Post("/tasima/arsiv/yukle", h.YuklemeBaslat)
	r.Get("/tasima/arsiv/yuklemeler", h.YuklemeListe)
	r.Get("/tasima/arsiv/yukle/{id}", h.YuklemeDurum)
	r.Put("/tasima/arsiv/yukle/{id}", h.YuklemeParca)
	r.Post("/tasima/arsiv/yukle/{id}/bitir", h.YuklemeBitir)
	r.Delete("/tasima/arsiv/yukle/{id}", h.YuklemeSil)
	r.Post("/tasima/arsiv/kesif", h.ArsivKesif)
	r.Get("/tasima/arsiv/kesif/{id}", h.ArsivKesifDurum)
	r.Post("/tasima/arsiv/baslat", h.ArsivBaslat)
	return r
}

func (o *arsivOrt) ham(kim, yontem, yol string, govde []byte) (int, string) {
	req := httptest.NewRequest(yontem, yol, bytes.NewReader(govde))
	req.Header.Set("X-T-Kim", kim)
	rec := httptest.NewRecorder()
	o.router().ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func (o *arsivOrt) ist(t *testing.T, kim, yontem, yol string, govde any) (int, string) {
	t.Helper()
	var b []byte
	if govde != nil {
		b, _ = json.Marshal(govde)
	}
	return o.ham(kim, yontem, yol, b)
}

func hizSifirla() {
	hizMu.Lock()
	hizKovas = map[int64]*hizKova{}
	hizMu.Unlock()
}

func jsonSayi(t *testing.T, g, alan string) int64 {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(g), &m); err != nil {
		t.Fatalf("JSON: %v (%s)", err, g)
	}
	f, ok := m[alan].(float64)
	if !ok {
		t.Fatalf("%q alanı yok: %s", alan, g)
	}
	return int64(f)
}

// yukle — API üzerinden parçalı yükleme (parça: 700 bayt) → hazır kayıt kimliği.
func (o *arsivOrt) yukle(t *testing.T, kim string, veri []byte) int64 {
	t.Helper()
	kod, g := o.ist(t, kim, http.MethodPost, "/tasima/arsiv/yukle", map[string]any{"ad": "cpmove-u.tar.gz", "boyut": len(veri)})
	if kod != http.StatusCreated {
		t.Fatalf("%s yükleme başlatılamadı: %d %s", kim, kod, g)
	}
	id := jsonSayi(t, g, "id")
	for konum := 0; konum < len(veri); konum += 700 {
		son := min(konum+700, len(veri))
		if kod, g := o.ham(kim, http.MethodPut, fmt.Sprintf("/tasima/arsiv/yukle/%d?konum=%d", id, konum), veri[konum:son]); kod != 200 {
			t.Fatalf("parça %d: %d %s", konum, kod, g)
		}
	}
	if kod, g := o.ist(t, kim, http.MethodPost, fmt.Sprintf("/tasima/arsiv/yukle/%d/bitir", id), nil); kod != 200 {
		t.Fatalf("bitir: %d %s", kod, g)
	}
	return id
}

// kesfet — keşfi başlatır ve 'hazir'/'hata' olana dek yoklar.
func (o *arsivOrt) kesfet(t *testing.T, kim string, govde map[string]any) (oturum int64, yanit map[string]any) {
	t.Helper()
	kod, g := o.ist(t, kim, http.MethodPost, "/tasima/arsiv/kesif", govde)
	if kod != http.StatusAccepted {
		t.Fatalf("keşif başlatılamadı: %d %s", kod, g)
	}
	oturum = jsonSayi(t, g, "oturum_id")
	for i := 0; i < 200; i++ {
		kod, g := o.ist(t, kim, http.MethodGet, fmt.Sprintf("/tasima/arsiv/kesif/%d", oturum), nil)
		if kod != 200 {
			t.Fatalf("keşif durumu: %d %s", kod, g)
		}
		var y map[string]any
		_ = json.Unmarshal([]byte(g), &y)
		if y["durum"] != "kesif" {
			// Goroutine'in kapanış (defer) işleri bitsin: test DB'leri kimlikleri yeniden kullanır,
			// geç gelen Delete sonraki testin aynı kimlikli girdisini silerdi (üretimde kimlik tekildir).
			for j := 0; j < 100; j++ {
				if _, aktif := arsivKesifAktif.Load(oturum); !aktif {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			return oturum, y
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("keşif bitmedi")
	return 0, nil
}

// ---------------------------------------------------------------------------

// Kural: bayiye açılma VARSAYILAN KAPALI (kullanıcı kararı); kapı = genel taşıma anahtarı ∧
// kaynak etkin ∧ kaynağın bayi_etkin'i. Kök yalnız kaynağın etkin olmasını ister.
func TestArsivKapi_BayiVarsayilanKapaliVeKokAnahtari(t *testing.T) {
	o := arsivKur(t)
	o.bayiAc(t, true)
	liste := func(kim string) []kaynakYanit {
		t.Helper()
		kod, g := o.ist(t, kim, http.MethodGet, "/tasima/kaynaklar", nil)
		var y struct {
			Kaynaklar []kaynakYanit    `json:"kaynaklar"`
			Limitler  map[string]int64 `json:"arsiv_limitleri"`
		}
		if kod != 200 || json.Unmarshal([]byte(g), &y) != nil {
			t.Fatalf("kaynaklar: %d %s", kod, g)
		}
		if y.Limitler["yol_tavan"] != 500<<30 || y.Limitler["yukleme_tavan"] != 50<<30 || y.Limitler["parca"] != 8<<20 {
			t.Errorf("arşiv limitleri: %v", y.Limitler)
		}
		return y.Kaynaklar
	}
	var arsivVar = func(l []kaynakYanit, kod string) *kaynakYanit {
		for i := range l {
			if l[i].Kod == kod {
				return &l[i]
			}
		}
		return nil
	}
	// Kök etkinleştirdi ama bayi anahtarı yok: bayi HİÇ görmez, hiçbir uç açılmaz.
	if arsivVar(liste("bayi7"), "arsiv-cpanel") != nil {
		t.Fatal("bayi anahtarı kapalıyken arşiv kaynağı bayiye görünüyor")
	}
	for _, c := range [][3]string{{http.MethodPost, "/tasima/arsiv/yukle", `{"ad":"a.tar.gz","boyut":1000}`},
		{http.MethodGet, "/tasima/arsiv/yuklemeler", ""}, {http.MethodPost, "/tasima/arsiv/kesif", `{"tip":"arsiv-cpanel","yukleme_id":1}`},
		{http.MethodPost, "/tasima/arsiv/baslat", `{"tip":"arsiv-cpanel","oturum_id":1}`}} {
		if kod, g := o.ham("bayi7", c[0], c[1], []byte(c[2])); kod != http.StatusForbidden {
			t.Errorf("bayi %s %s: 403 beklendi, gelen %d %s", c[0], c[1], kod, g)
		}
	}
	kk := arsivVar(liste("kok"), "arsiv-cpanel")
	if kk == nil || !kk.Etkin || kk.BayiEtkin || !kk.Arsiv || !kk.Yetenekler.Cron {
		t.Fatalf("kök görünümü: %+v", kk)
	}
	if kod, g := o.ist(t, "kok", http.MethodPost, "/tasima/arsiv/yukle", map[string]any{"ad": "a.tar.gz", "boyut": 1000}); kod != 201 {
		t.Fatalf("kök yükleme: %d %s", kod, g)
	}
	// Başka kaynak türünün bayi anahtarı olmaz; boş gövde ve bayi çağrısı reddedilir.
	if kod, _ := o.ist(t, "kok", http.MethodPut, "/tasima/kaynaklar/plesk", map[string]any{"bayi_etkin": true}); kod != 400 {
		t.Errorf("plesk bayi_etkin: %d", kod)
	}
	if kod, _ := o.ist(t, "kok", http.MethodPut, "/tasima/kaynaklar/arsiv-cpanel", map[string]any{}); kod != 400 {
		t.Errorf("boş gövde: %d", kod)
	}
	if kod, _ := o.ist(t, "bayi7", http.MethodPut, "/tasima/kaynaklar/arsiv-cpanel", map[string]any{"bayi_etkin": true}); kod != 403 {
		t.Errorf("bayi kaynak anahtarını değiştirebildi: %d", kod)
	}
	// Kök bayi anahtarını açar: bayi YALNIZ o kaynağı görür/kullanır (kapalı kardeş görünmez).
	if kod, g := o.ist(t, "kok", http.MethodPut, "/tasima/kaynaklar/arsiv-cpanel", map[string]any{"bayi_etkin": true}); kod != 200 {
		t.Fatalf("bayi_etkin: %d %s", kod, g)
	}
	if a := arsivVar(liste("bayi7"), "arsiv-cpanel"); a == nil || !a.Arsiv || a.Yetenekler.Sahipler {
		t.Fatalf("bayi görünümü: %+v", a)
	}
	if arsivVar(liste("bayi7"), "arsiv-directadmin") != nil {
		t.Error("kapalı kardeş kaynak bayiye göründü")
	}
	if kod, g := o.ist(t, "bayi7", http.MethodPost, "/tasima/arsiv/yukle", map[string]any{"ad": "a.tar.gz", "boyut": 1000}); kod != 201 {
		t.Fatalf("bayi yükleme: %d %s", kod, g)
	}
	// Etkin değerini değiştirmeden bayi_etkin güncellemesi etkini korur.
	if a := arsivVar(liste("kok"), "arsiv-cpanel"); a == nil || !a.Etkin || !a.BayiEtkin {
		t.Fatalf("kök görünümü: %+v", a)
	}
	// Genel taşıma anahtarı kapanınca bayi_etkin tek başına yetmez.
	o.bayiAc(t, false)
	if kod, g := o.ist(t, "bayi7", http.MethodPost, "/tasima/arsiv/yukle", map[string]any{"ad": "b.tar.gz", "boyut": 1000}); kod != 403 {
		t.Errorf("genel anahtar kapalıyken bayi: %d %s", kod, g)
	}
	o.bayiAc(t, true)
	// bayi_etkin kapanınca bayinin SIRADAKİ arşiv işi iptal edilir (kök işine dokunulmaz).
	o.exec(t, `INSERT INTO tasima_isleri (kaynak_tip, kaynak_host, durum, toplam, sahip_rid, baslatan) VALUES ('arsiv-cpanel','arsiv','kuyrukta',1,7,'x')`)
	o.exec(t, `INSERT INTO tasima_isleri (kaynak_tip, kaynak_host, durum, toplam, sahip_rid, baslatan) VALUES ('arsiv-cpanel','arsiv','kuyrukta',1,0,'x')`)
	if kod, g := o.ist(t, "kok", http.MethodPut, "/tasima/kaynaklar/arsiv-cpanel", map[string]any{"bayi_etkin": false}); kod != 200 || !strings.Contains(g, `"iptal_edilen":1`) {
		t.Fatalf("bayi_etkin kapatma: %d %s", kod, g)
	}
	var bayiDurum, kokDurum string
	_ = o.db.QueryRow(`SELECT durum FROM tasima_isleri WHERE sahip_rid=7 AND kaynak_tip='arsiv-cpanel'`).Scan(&bayiDurum)
	_ = o.db.QueryRow(`SELECT durum FROM tasima_isleri WHERE sahip_rid=0 AND kaynak_tip='arsiv-cpanel'`).Scan(&kokDurum)
	if bayiDurum != "iptal" || kokDurum != "kuyrukta" {
		t.Errorf("iptal kapsamı: bayi=%s kök=%s", bayiDurum, kokDurum)
	}
	if kod, _ := o.ist(t, "bayi7", http.MethodPost, "/tasima/arsiv/yukle", map[string]any{"ad": "c.tar.gz", "boyut": 1000}); kod != 403 {
		t.Errorf("bayi_etkin kapandı ama bayi hâlâ yükleyebiliyor: %d", kod)
	}
	// Kaynak (etkin) kapanınca bayi_etkin açık olsa da kimse kullanamaz.
	o.ist(t, "kok", http.MethodPut, "/tasima/kaynaklar/arsiv-cpanel", map[string]any{"bayi_etkin": true})
	if kod, _ := o.ist(t, "kok", http.MethodPut, "/tasima/kaynaklar/arsiv-cpanel", map[string]any{"etkin": false}); kod != 200 {
		t.Fatal("kapatılamadı")
	}
	for _, kim := range []string{"kok", "bayi7"} {
		if kod, _ := o.ist(t, kim, http.MethodPost, "/tasima/arsiv/yukle", map[string]any{"ad": "d.tar.gz", "boyut": 1000}); kod != 403 {
			t.Errorf("%s: kaynak kapalıyken yükleme: %d", kim, kod)
		}
	}
	// Müşteri / kimliksiz bayi jetonu hiçbir arşiv ucuna giremez.
	for _, kim := range []string{"musteri", "bayi0", ""} {
		if kod, _ := o.ist(t, kim, http.MethodPost, "/tasima/arsiv/yukle", map[string]any{"ad": "e.tar.gz", "boyut": 1000}); kod != 403 {
			t.Errorf("%q: 403 beklendi, gelen %d", kim, kod)
		}
	}
}

// Kural: parçalı + devam edebilir yükleme; konum = alınan; parça bildirilen boyutu aşamaz;
// biçimi tanınmayan dosya tutulmaz; dosya yolu sunucuda türetilir (ad yola girmez).
func TestYukleme_ParcaliDevamEdebilir(t *testing.T) {
	o := arsivKur(t)
	veri := cpmoveT(t)
	kod, g := o.ist(t, "kok", http.MethodPost, "/tasima/arsiv/yukle", map[string]any{"ad": "../../etc/x.tar.gz", "boyut": len(veri)})
	if kod != 400 {
		t.Fatalf("yol ayracı içeren ad reddedilmeli: %d %s", kod, g)
	}
	kod, g = o.ist(t, "kok", http.MethodPost, "/tasima/arsiv/yukle", map[string]any{"ad": "yedek (1).tar.gz", "boyut": len(veri)})
	if kod != 201 || jsonSayi(t, g, "parca") != 8<<20 {
		t.Fatalf("başlat: %d %s", kod, g)
	}
	id := jsonSayi(t, g, "id")
	yol := fmt.Sprintf("/tasima/arsiv/yukle/%d", id)
	if fi, err := os.Lstat(filepath.Join(arsivYukleKok, "0", fmt.Sprintf("%d.arsiv", id))); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("yükleme dosyası sunucu yolunda 0600 olmalı: %v %v", fi, err)
	}
	if d, _ := os.Stat(filepath.Join(arsivYukleKok, "0")); d == nil || d.Mode().Perm() != 0o700 {
		t.Errorf("yükleme dizini 0700 olmalı: %v", d)
	}
	// 1. parça
	if kod, g := o.ham("kok", http.MethodPut, yol+"?konum=0", veri[:300]); kod != 200 || jsonSayi(t, g, "alinan") != 300 {
		t.Fatalf("parça 1: %d %s", kod, g)
	}
	// Yanlış konum (ileri, geri, tekrar): 409 + gerçek alınan; ilerleme bozulmaz.
	for _, konum := range []string{"0", "299", "301", "-1", "x", ""} {
		kod, g := o.ham("kok", http.MethodPut, yol+"?konum="+konum, veri[300:400])
		want := http.StatusConflict
		if konum == "-1" || konum == "x" || konum == "" {
			want = http.StatusBadRequest
		}
		if kod != want {
			t.Errorf("konum=%q: %d beklendi, gelen %d %s", konum, want, kod, g)
		}
		if kod == 409 && jsonSayi(t, g, "alinan") != 300 {
			t.Errorf("409 gövdesi alınanı vermeli: %s", g)
		}
	}
	// Kalan boyutu aşan parça: 413 ve dosya/ilerleme DEĞİŞMEZ.
	if kod, _ := o.ham("kok", http.MethodPut, yol+"?konum=300", make([]byte, len(veri)-300+1)); kod != http.StatusRequestEntityTooLarge {
		t.Errorf("aşan parça: %d", kod)
	}
	// Uzunluğu BİLİNMEYEN (chunked) aşan gövde: gövde okunurken yakalanır, dosya geri alınır.
	{
		req := httptest.NewRequest(http.MethodPut, yol+"?konum=300", io.NopCloser(bytes.NewReader(make([]byte, len(veri)))))
		req.ContentLength = -1
		req.Header.Set("X-T-Kim", "kok")
		rec := httptest.NewRecorder()
		o.router().ServeHTTP(rec, req)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("uzunluksuz aşan gövde: %d %s", rec.Code, rec.Body.String())
		}
		if fi, err := os.Stat(yuklemeYolu(0, id)); err != nil || fi.Size() != 300 {
			t.Errorf("aşan gövde dosyada iz bıraktı: %v %v", fi, err)
		}
	}
	// "Devam": durum sorgusu → kaldığı yer; geri kalan tek parça.
	if kod, g := o.ist(t, "kok", http.MethodGet, yol, nil); kod != 200 || jsonSayi(t, g, "alinan") != 300 || !strings.Contains(g, `"durum":"yukleniyor"`) {
		t.Fatalf("durum: %d %s", kod, g)
	}
	// Eksikken bitir: 409.
	if kod, _ := o.ist(t, "kok", http.MethodPost, yol+"/bitir", nil); kod != http.StatusConflict {
		t.Errorf("eksik yükleme bitirilebildi: %d", kod)
	}
	if kod, g := o.ham("kok", http.MethodPut, yol+"?konum=300", veri[300:]); kod != 200 || jsonSayi(t, g, "alinan") != int64(len(veri)) {
		t.Fatalf("parça 2: %d %s", kod, g)
	}
	// Tamam olan yüklemeye parça eklenemez.
	if kod, _ := o.ham("kok", http.MethodPut, yol+"?konum="+strconv.Itoa(len(veri)), []byte("x")); kod != 413 {
		t.Errorf("tamamlanmış (bitirilmemiş) yükleme fazla bayt kabul etti: %d", kod)
	}
	if kod, g := o.ist(t, "kok", http.MethodPost, yol+"/bitir", nil); kod != 200 || !strings.Contains(g, `"hazir"`) {
		t.Fatalf("bitir: %d %s", kod, g)
	}
	got, _ := os.ReadFile(filepath.Join(arsivYukleKok, "0", fmt.Sprintf("%d.arsiv", id)))
	if !bytes.Equal(got, veri) {
		t.Fatal("yüklenen dosya bayt bayt aynı değil")
	}
	for _, konum := range []int{0, len(veri)} {
		if kod, g := o.ham("kok", http.MethodPut, yol+"?konum="+strconv.Itoa(konum), []byte("x")); kod != http.StatusConflict || !strings.Contains(g, "tamamlanmış") {
			t.Errorf("hazır yükleme konum=%d: durum kapısından 409 beklendi: %d %s", konum, kod, g)
		}
	}
	if kod, g := o.ist(t, "kok", http.MethodGet, "/tasima/arsiv/yuklemeler", nil); kod != 200 || !strings.Contains(g, `"durum":"hazir"`) || strings.Contains(g, "/tmp") {
		t.Errorf("liste: %d %s", kod, g)
	}

	// Biçimi tanınmayan dosya: bitirmede 422, kayıt + dosya silinir (kota geri döner).
	duz := bytes.Repeat([]byte("A"), 600)
	kod, g = o.ist(t, "kok", http.MethodPost, "/tasima/arsiv/yukle", map[string]any{"ad": "sahte.tar.gz", "boyut": len(duz)})
	id2 := jsonSayi(t, g, "id")
	o.ham("kok", http.MethodPut, fmt.Sprintf("/tasima/arsiv/yukle/%d?konum=0", id2), duz)
	if kod, g := o.ist(t, "kok", http.MethodPost, fmt.Sprintf("/tasima/arsiv/yukle/%d/bitir", id2), nil); kod != 422 {
		t.Fatalf("biçimsiz dosya: %d %s", kod, g)
	}
	if _, err := os.Lstat(filepath.Join(arsivYukleKok, "0", fmt.Sprintf("%d.arsiv", id2))); err == nil {
		t.Error("biçimsiz yükleme dosyası silinmedi")
	}
	if kod, _ := o.ist(t, "kok", http.MethodGet, fmt.Sprintf("/tasima/arsiv/yukle/%d", id2), nil); kod != 404 {
		t.Errorf("biçimsiz yükleme kaydı kaldı: %d", kod)
	}
	// Silme: dosya + kayıt gider.
	if kod, _ := o.ist(t, "kok", http.MethodDelete, yol, nil); kod != 200 {
		t.Fatalf("silme: %d", kod)
	}
	if _, err := os.Lstat(filepath.Join(arsivYukleKok, "0", fmt.Sprintf("%d.arsiv", id))); err == nil {
		t.Error("silinen yüklemenin dosyası kaldı")
	}
}

// Kural: yükleme tavanı 50 GiB; kapsam başına toplam kota; en çok 3 kayıt; boş alan; bayi disk
// havuzu; ad/boyut doğrulaması.
func TestYukleme_TavanVeKota(t *testing.T) {
	o := arsivKur(t)
	o.bayiAc(t, true)
	o.ist(t, "kok", http.MethodPut, "/tasima/kaynaklar/arsiv-cpanel", map[string]any{"bayi_etkin": true})
	yeni := func(kim string, boyut int64, ad string) (int, string) {
		hizSifirla()
		return o.ist(t, kim, http.MethodPost, "/tasima/arsiv/yukle", map[string]any{"ad": ad, "boyut": boyut})
	}
	for ad, c := range map[string]struct {
		boyut int64
		ad    string
		kod   int
	}{
		"tavan+1":    {50<<30 + 1, "a.tar.gz", 413},
		"sıfır":      {0, "a.tar.gz", 400},
		"çok küçük":  {511, "a.tar.gz", 400},
		"negatif":    {-5, "a.tar.gz", 400},
		"ad boş":     {1000, "", 400},
		"ad kontrol": {1000, "a\nb", 400},
		"tam tavan":  {50 << 30, "tam.tar.gz", 201},
	} {
		kod, g := yeni("bayi7", c.boyut, c.ad)
		if kod != c.kod {
			t.Errorf("%s: %d beklendi, gelen %d %s", ad, c.kod, kod, g)
		}
		if ad == "tavan+1" && !strings.Contains(g, "50 GiB") {
			t.Errorf("tavan reddi tavan kapısından olmalı (kota değil): %s", g)
		}
	}
	// Kapsam toplamı tavanı aşamaz: tam tavan yüklemesi varken yenisi 413 (bayi7); başka bayi etkilenmez.
	if kod, g := yeni("bayi7", 1000, "b.tar.gz"); kod != 413 {
		t.Errorf("toplam kota: %d %s", kod, g)
	}
	if kod, g := yeni("bayi9", 1000, "b.tar.gz"); kod != 201 {
		t.Errorf("başka bayinin kotası etkilenmemeli: %d %s", kod, g)
	}
	// En çok 3 kayıt (kapsam başına).
	for i := 0; i < 2; i++ {
		if kod, g := yeni("bayi9", 1000, "c.tar.gz"); kod != 201 {
			t.Fatalf("bayi9 %d. kayıt: %d %s", i+2, kod, g)
		}
	}
	if kod, g := yeni("bayi9", 1000, "d.tar.gz"); kod != 409 {
		t.Errorf("4. kayıt: %d %s", kod, g)
	}
	// Bayi disk havuzu: arşiv havuzun kalanına sığmıyorsa baştan 413 (kök için havuz yok).
	o.exec(t, `UPDATE users SET max_disk_mb=1 WHERE id=7`)
	o.exec(t, `DELETE FROM tasima_yuklemeleri WHERE sahip_rid=7`)
	if kod, g := yeni("bayi7", 2<<20, "e.tar.gz"); kod != 413 || !strings.Contains(g, errArsivKota.Error()) {
		t.Errorf("bayi disk havuzu: %d %s", kod, g)
	}
	if kod, g := yeni("bayi7", 1<<20, "f.tar.gz"); kod != 201 {
		t.Errorf("havuza sığan yükleme: %d %s", kod, g)
	}
	if kod, g := yeni("kok", 2<<20, "g.tar.gz"); kod != 201 {
		t.Errorf("kökte bayi havuzu uygulanmamalı: %d %s", kod, g)
	}
	// Geçici alanda boş yer yoksa 507 (boyut + 1 GiB pay).
	eski := diskBosAlan
	diskBosAlan = func(string) (int64, error) { return 1000 + yuklemeBosPayi - 1, nil }
	if kod, g := yeni("bayi9", 1000, "h.tar.gz"); kod != http.StatusInsufficientStorage && kod != 409 {
		t.Errorf("boş alan yetersiz: %d %s", kod, g)
	}
	o.exec(t, `DELETE FROM tasima_yuklemeleri WHERE sahip_rid=9`)
	if kod, g := yeni("bayi9", 1000, "i.tar.gz"); kod != http.StatusInsufficientStorage {
		t.Errorf("boş alan yetersiz: %d %s", kod, g)
	}
	diskBosAlan = func(string) (int64, error) { return 1000 + yuklemeBosPayi, nil }
	if kod, g := yeni("bayi9", 1000, "j.tar.gz"); kod != 201 {
		t.Errorf("boş alan tam yeterli: %d %s", kod, g)
	}
	diskBosAlan = eski
}

// Kural (IDOR): yükleme kayıtları sahip_rid ile süzülür; kapsam dışı ve olmayan kayıt AYNI 404;
// işe bağlı yükleme silinemez; dosya sahibine ait yol dışında okunamaz.
func TestYukleme_KapsamDisiAyni404(t *testing.T) {
	o := arsivKur(t)
	o.bayiAc(t, true)
	o.ist(t, "kok", http.MethodPut, "/tasima/kaynaklar/arsiv-cpanel", map[string]any{"bayi_etkin": true})
	id := o.yukle(t, "bayi7", cpmoveT(t))
	_, bos := o.ist(t, "bayi7", http.MethodGet, "/tasima/arsiv/yukle/999999", nil)
	for _, kim := range []string{"bayi9", "kok"} {
		for _, c := range [][2]string{{http.MethodGet, ""}, {http.MethodPut, "?konum=0"}, {http.MethodPost, "/bitir"}, {http.MethodDelete, ""}} {
			kod, g := o.ham(kim, c[0], fmt.Sprintf("/tasima/arsiv/yukle/%d%s", id, c[1]), []byte("x"))
			if kod != 404 || g != bos {
				t.Errorf("%s %s%s: olmayan kayıtla AYNI 404 beklendi: %d %q (olmayan: %q)", kim, c[0], c[1], kod, g, bos)
			}
		}
		if kod, g := o.ist(t, kim, http.MethodGet, "/tasima/arsiv/yuklemeler", nil); kod != 200 || strings.Contains(g, fmt.Sprintf(`"id":%d,`, id)) {
			t.Errorf("%s başkasının yüklemesini listeledi: %d %s", kim, kod, g)
		}
	}
	if _, err := os.Lstat(yuklemeYolu(7, id)); err != nil {
		t.Fatalf("başkasının silme denemesi dosyayı etkiledi: %v", err)
	}
	// Kapsam dışı yükleme keşfe de konu olamaz (bayi9 ve kök bayi7'nin yüklemesini çözemez).
	o.ist(t, "kok", http.MethodPut, "/tasima/kaynaklar/arsiv-cpanel", map[string]any{"bayi_etkin": true})
	for _, kim := range []string{"bayi9", "kok"} {
		hizSifirla()
		if kod, g := o.ist(t, kim, http.MethodPost, "/tasima/arsiv/kesif", map[string]any{"tip": "arsiv-cpanel", "yukleme_id": id}); kod != 404 {
			t.Errorf("%s: başkasının yüklemesiyle keşif: %d %s", kim, kod, g)
		}
	}
	// İşe bağlı (kuyrukta/çalışıyor) yükleme silinemez; iş bitince silinir.
	res := o.exec(t, `INSERT INTO tasima_isleri (kaynak_tip, kaynak_host, durum, toplam, sahip_rid, baslatan, arsiv_yukleme_id)
		VALUES ('arsiv-cpanel','arsiv','kuyrukta',1,7,'x',?)`, id)
	isID, _ := res.LastInsertId()
	if kod, _ := o.ist(t, "bayi7", http.MethodDelete, fmt.Sprintf("/tasima/arsiv/yukle/%d", id), nil); kod != http.StatusConflict {
		t.Errorf("işe bağlı yükleme silindi: %d", kod)
	}
	o.exec(t, `UPDATE tasima_isleri SET durum='tamam' WHERE id=?`, isID)
	if kod, _ := o.ist(t, "bayi7", http.MethodDelete, fmt.Sprintf("/tasima/arsiv/yukle/%d", id), nil); kod != 200 {
		t.Errorf("biten işin yüklemesi silinemedi: %d", kod)
	}
	// Dosya kayıtla uyuşmuyorsa (kesilmiş/bozulmuş) hazır yükleme çözülmez.
	id2 := o.yukle(t, "bayi7", cpmoveT(t))
	if err := os.Truncate(yuklemeYolu(7, id2), 10); err != nil {
		t.Fatal(err)
	}
	hizSifirla()
	if kod, g := o.ist(t, "bayi7", http.MethodPost, "/tasima/arsiv/kesif", map[string]any{"tip": "arsiv-cpanel", "yukleme_id": id2}); kod != 404 {
		t.Errorf("boyutu uyuşmayan yükleme çözüldü: %d %s", kod, g)
	}
	// Tüm baytları gelmiş ama BİTİR çağrılmamış (hazır değil) kayıt keşfe konu olamaz (dosya boyutu
	// kayıtla aynı olduğundan yalnız durum denetimi yakalar).
	tam := cpmoveT(t)
	kod, g := o.ist(t, "bayi7", http.MethodPost, "/tasima/arsiv/yukle", map[string]any{"ad": "x.tar.gz", "boyut": len(tam)})
	if kod != 201 {
		t.Fatalf("yükleme: %d %s", kod, g)
	}
	yarim := jsonSayi(t, g, "id")
	if kod, g := o.ham("bayi7", http.MethodPut, fmt.Sprintf("/tasima/arsiv/yukle/%d?konum=0", yarim), tam[:700]); kod != 200 {
		t.Fatalf("parça: %d %s", kod, g)
	}
	if len(tam) > 700 {
		o.ham("bayi7", http.MethodPut, fmt.Sprintf("/tasima/arsiv/yukle/%d?konum=700", yarim), tam[700:])
	}
	hizSifirla()
	if kod, g := o.ist(t, "bayi7", http.MethodPost, "/tasima/arsiv/kesif", map[string]any{"tip": "arsiv-cpanel", "yukleme_id": yarim}); kod != 404 {
		t.Errorf("hazır olmayan yükleme çözüldü: %d %s", kod, g)
	}
}

// Kural: keşif asenkron (202 + yoklama); kök için hazır dosya yolu doğrulanır; yol/yükleme
// kimliği istemciye geri DÖNMEZ; sonuç yalnız aynı kapsamda okunur; yanlış tür/bozuk dosya
// 'hata' durumuyla bildirilir; eşzamanlı inceleme sınırlıdır.
func TestArsivKesif_AkisVeKapsam(t *testing.T) {
	o := arsivKur(t)
	oturum, y := o.kesfet(t, "kok", map[string]any{"tip": "arsiv-cpanel", "yol": o.dosya})
	if y["durum"] != "hazir" {
		t.Fatalf("keşif: %+v", y)
	}
	hs := y["hesaplar"].([]any)
	if len(hs) != 2 {
		t.Fatalf("hesaplar: %+v", hs)
	}
	var adlar []string
	for _, h := range hs {
		adlar = append(adlar, h.(map[string]any)["alan_adi"].(string))
	}
	if strings.Join(adlar, ",") != "ek.net,ornek.com" { // manifest alfabetik
		t.Errorf("alan adları: %v", adlar)
	}
	oz := y["ozet"].(map[string]any)
	if oz["db_sayisi"].(float64) != 1 || oz["posta_sayisi"].(float64) != 1 || oz["cron_sayisi"].(float64) != 1 || oz["dns_sayisi"].(float64) != 1 ||
		oz["arsiv_bayt"].(float64) <= 0 || oz["dosya_sayisi"].(float64) < 3 {
		t.Errorf("özet: %+v", oz)
	}
	ok := y["onkontrol"].(map[string]any)
	if ok["yeterli"] != true || ok["gerekli_bos_bayt"].(float64) != float64(gerekliBosBayt(int64(oz["arsiv_bayt"].(float64)))) {
		t.Errorf("önkontrol: %+v", ok)
	}
	if len(y["tasinmayacak"].([]any)) == 0 || len(y["uyarilar"].([]any)) == 0 {
		t.Errorf("uyarı/taşınmayacak listeleri boş: %+v", y)
	}
	_, ham := o.ist(t, "kok", http.MethodGet, fmt.Sprintf("/tasima/arsiv/kesif/%d", oturum), nil)
	for _, sizinti := range []string{o.kok, "cpmove-u.tar.gz", "baska-bayi", tHash, "yukleme_id", `"yol"`} {
		if strings.Contains(ham, sizinti) {
			t.Errorf("yanıt %q sızdırıyor: %s", sizinti, ham)
		}
	}
	// Yol yalnız sunucu tarafı oturum satırında; kimlik yok.
	var ayarlar string
	var rid int64
	var parola sql.NullString
	_ = o.db.QueryRow(`SELECT ayarlar, sahip_rid, kaynak_parola FROM tasima_isleri WHERE id=?`, oturum).Scan(&ayarlar, &rid, &parola)
	if !strings.Contains(ayarlar, o.dosya) || rid != 0 || parola.Valid {
		t.Errorf("oturum satırı: rid=%d parola=%v ayarlar=%q", rid, parola, ayarlar)
	}
	// Kapsam: başka kapsamın oturumu 'yok' ile aynı yanıt.
	o.bayiAc(t, true)
	o.ist(t, "kok", http.MethodPut, "/tasima/kaynaklar/arsiv-cpanel", map[string]any{"bayi_etkin": true})
	_, yokYanit := o.ist(t, "bayi7", http.MethodGet, "/tasima/arsiv/kesif/999999", nil)
	for _, kim := range []string{"bayi7", "bayi9"} {
		if kod, g := o.ist(t, kim, http.MethodGet, fmt.Sprintf("/tasima/arsiv/kesif/%d", oturum), nil); kod != 404 || g != yokYanit {
			t.Errorf("%s kök oturumunu okudu: %d %s", kim, kod, g)
		}
	}
	// Yeni keşif aynı kapsamın eski arşiv oturumunu değiştirir (biriktirme yok).
	oturum2, _ := o.kesfet(t, "kok", map[string]any{"tip": "arsiv-cpanel", "yol": o.dosya})
	var n int
	_ = o.db.QueryRow(`SELECT COUNT(*) FROM tasima_isleri WHERE durum='oturum' AND kaynak_host='arsiv' AND sahip_rid=0`).Scan(&n)
	if n != 1 || oturum2 == oturum {
		t.Errorf("eski oturum silinmedi: n=%d", n)
	}
	// Yanlış tür: DirectAdmin kaynağı seçilmiş ama dosya cPanel → hata durumu + açık neden.
	o.ist(t, "kok", http.MethodPut, "/tasima/kaynaklar/arsiv-directadmin", map[string]any{"etkin": true})
	if _, y := o.kesfet(t, "kok", map[string]any{"tip": "arsiv-directadmin", "yol": o.dosya}); y["durum"] != "hata" || !strings.Contains(fmt.Sprint(y["hata"]), "cPanel") {
		t.Errorf("yanlış tür: %+v", y)
	}
	// Bozuk dosya (tar.gz değil).
	bozuk := filepath.Join(o.kok, "bozuk.tar.gz")
	_ = os.WriteFile(bozuk, bytes.Repeat([]byte("Z"), 4096), 0o644)
	if _, y := o.kesfet(t, "kok", map[string]any{"tip": "arsiv-cpanel", "yol": bozuk}); y["durum"] != "hata" {
		t.Errorf("bozuk dosya: %+v", y)
	}
	// Panel yeniden başlayınca 'kesif'te kalan oturum (aktif işaret yok) yetim olarak 'hata' görünür.
	res := o.exec(t, `INSERT INTO tasima_isleri (kaynak_tip, kaynak_host, kaynak_port, kaynak_kullanici, durum, toplam, ayarlar, gecerlilik, sahip_rid)
		VALUES ('arsiv-cpanel','arsiv',0,'arsiv','oturum',0,'{"durum":"kesif","tip":"arsiv-cpanel","baslangic":1}', (NOW() + INTERVAL 1 HOUR), 0)`)
	yetim, _ := res.LastInsertId()
	if kod, g := o.ist(t, "kok", http.MethodGet, fmt.Sprintf("/tasima/arsiv/kesif/%d", yetim), nil); kod != 200 ||
		!strings.Contains(g, `"durum":"hata"`) || !strings.Contains(g, "yeniden") {
		t.Errorf("yetim keşif: %d %s", kod, g)
	}
	// Süresi dolan oturum 'yok' ile aynı.
	o.exec(t, `UPDATE tasima_isleri SET gecerlilik=(NOW() - INTERVAL 1 MINUTE) WHERE id=?`, yetim)
	if kod, g := o.ist(t, "kok", http.MethodGet, fmt.Sprintf("/tasima/arsiv/kesif/%d", yetim), nil); kod != 404 || g != yokYanit {
		t.Errorf("süresi dolan oturum: %d %s", kod, g)
	}
	// Eşzamanlı inceleme sınırı: yuva doluysa 409 (kuyruk değil).
	arsivKesifSem <- struct{}{}
	arsivKesifSem <- struct{}{}
	kod, g := o.ist(t, "kok", http.MethodPost, "/tasima/arsiv/kesif", map[string]any{"tip": "arsiv-cpanel", "yol": o.dosya})
	<-arsivKesifSem
	<-arsivKesifSem
	if kod != http.StatusConflict {
		t.Errorf("yuva doluyken keşif: %d %s", kod, g)
	}
}

// Kural: yol yalnız kökte; geçersiz/izinsiz yol 400; bayi yol veremez (403); kaynak türü doğrulanır.
func TestArsivKesif_YolKapilari(t *testing.T) {
	o := arsivKur(t)
	o.bayiAc(t, true)
	o.ist(t, "kok", http.MethodPut, "/tasima/kaynaklar/arsiv-cpanel", map[string]any{"bayi_etkin": true})
	bag := filepath.Join(o.kok, "bag.tar.gz")
	_ = os.Symlink(o.dosya, bag)
	yazilir := filepath.Join(o.kok, "yazilir.tar.gz")
	_ = os.WriteFile(yazilir, cpmoveT(t), 0o666)
	_ = os.Chmod(yazilir, 0o666)
	post := func(kim string, g map[string]any) (int, string) {
		hizSifirla()
		return o.ist(t, kim, http.MethodPost, "/tasima/arsiv/kesif", g)
	}
	for ad, c := range map[string]struct {
		kim string
		g   map[string]any
		kod int
	}{
		"bayi yol":          {"bayi7", map[string]any{"tip": "arsiv-cpanel", "yol": o.dosya}, 403},
		"yol yok":           {"kok", map[string]any{"tip": "arsiv-cpanel"}, 404},
		"izinsiz dizin":     {"kok", map[string]any{"tip": "arsiv-cpanel", "yol": "/etc/passwd"}, 400},
		"home":              {"kok", map[string]any{"tip": "arsiv-cpanel", "yol": "/home/c_x/y.tar.gz"}, 400},
		"göreli":            {"kok", map[string]any{"tip": "arsiv-cpanel", "yol": "cpmove-u.tar.gz"}, 400},
		"nokta nokta":       {"kok", map[string]any{"tip": "arsiv-cpanel", "yol": o.kok + "/../" + filepath.Base(o.kok) + "/cpmove-u.tar.gz"}, 400},
		"sembolik bağ":      {"kok", map[string]any{"tip": "arsiv-cpanel", "yol": bag}, 400},
		"herkes yazar":      {"kok", map[string]any{"tip": "arsiv-cpanel", "yol": yazilir}, 400},
		"olmayan dosya":     {"kok", map[string]any{"tip": "arsiv-cpanel", "yol": filepath.Join(o.kok, "yok.tar.gz")}, 400},
		"tip yok":           {"kok", map[string]any{"yol": o.dosya}, 400},
		"canlı kaynak tipi": {"kok", map[string]any{"tip": "plesk", "yol": o.dosya}, 400},
		"bilinmeyen tip":    {"kok", map[string]any{"tip": "arsiv-x", "yol": o.dosya}, 400},
		"kapalı kardeş":     {"kok", map[string]any{"tip": "arsiv-directadmin", "yol": o.dosya}, 400},
	} {
		if kod, g := post(c.kim, c.g); kod != c.kod {
			t.Errorf("%s: %d beklendi, gelen %d %s", ad, c.kod, kod, g)
		}
	}
	// Bayinin yol denemesinde hata metni yol/dizin ayrıntısı taşımaz.
	if _, g := post("bayi7", map[string]any{"tip": "arsiv-cpanel", "yol": o.dosya}); strings.Contains(g, o.kok) {
		t.Errorf("bayi yanıtı yol sızdırıyor: %s", g)
	}
	var n int
	_ = o.db.QueryRow(`SELECT COUNT(*) FROM tasima_isleri WHERE durum='oturum' AND kaynak_host='arsiv'`).Scan(&n)
	if n != 0 {
		t.Errorf("reddedilen isteklerden oturum satırı kaldı: %d", n)
	}
}

// Kural: başlatma — kaynak SUNUCU oturumundan; istemci gövdesi (web_root, dbler, ssl, sahip,
// yol) yetki değildir; kök için de sunucu kopyasıyla kesişir; aynı kiracıda tek açık iş;
// dosya değiştiyse / disk yetersizse başlamadan ret.
func TestArsivBaslat_KokKurallari(t *testing.T) {
	o := arsivKur(t)
	oturum, y := o.kesfet(t, "kok", map[string]any{"tip": "arsiv-cpanel", "yol": o.dosya})
	if y["durum"] != "hazir" {
		t.Fatalf("keşif: %+v", y)
	}
	baslat := func(g map[string]any) (int, string) {
		hizSifirla()
		return o.ist(t, "kok", http.MethodPost, "/tasima/arsiv/baslat", g)
	}
	temel := func() map[string]any {
		return map[string]any{
			"tip": "arsiv-cpanel", "mod": "tekil", "oturum_id": oturum,
			"secilen": []map[string]any{{"alan_adi": "ornek.com", "web_root": "/etc", "dbler": []string{"mysql", "x"}, "kaynak_hesap": "root"}, {"alan_adi": " ORNEK.com "}, {"alan_adi": "yok.example"}},
			"ayarlar": map[string]any{"dosyalar": true, "veritabani": true, "ssl": true, "cron": true, "sahipleri_tasi": true, "reseller_id": 7, "customer_id": 3},
		}
	}
	// Ret yolları (hiçbiri iş açmaz).
	g := temel()
	delete(g, "oturum_id")
	if kod, _ := baslat(g); kod != 400 {
		t.Errorf("oturum_id'siz: %d", kod)
	}
	g = temel()
	g["oturum_id"] = 999999
	if kod, _ := baslat(g); kod != 400 {
		t.Errorf("olmayan oturum: %d", kod)
	}
	g = temel()
	g["tip"] = "arsiv-directadmin"
	o.ist(t, "kok", http.MethodPut, "/tasima/kaynaklar/arsiv-directadmin", map[string]any{"etkin": true})
	if kod, _ := baslat(g); kod != http.StatusConflict {
		t.Errorf("tip uyuşmazlığı: %d", kod)
	}
	// Keşif tamamlanmadan başlatılamaz (oturum 'kesif' durumunda).
	o.exec(t, `UPDATE tasima_isleri SET ayarlar=JSON_SET(ayarlar, '$.durum', 'kesif') WHERE id=?`, oturum)
	if kod, _ := baslat(temel()); kod != http.StatusConflict {
		t.Errorf("keşif sürerken başlatma: %d", kod)
	}
	o.exec(t, `UPDATE tasima_isleri SET ayarlar=JSON_SET(ayarlar, '$.durum', 'hazir') WHERE id=?`, oturum)
	g = temel()
	g["secilen"] = []map[string]any{{"alan_adi": "yok.example"}}
	if kod, _ := baslat(g); kod != 400 {
		t.Errorf("oturumda olmayan site: %d", kod)
	}
	eski := diskBosAlan
	diskBosAlan = func(string) (int64, error) { return 10, nil }
	if kod, _ := baslat(temel()); kod != http.StatusInsufficientStorage {
		t.Errorf("disk yetersiz: %d", kod)
	}
	diskBosAlan = eski
	// Keşiften sonra dosya değişti → 409.
	gelecek := time.Now().Add(time.Hour)
	_ = os.Chtimes(o.dosya, gelecek, gelecek)
	if kod, g2 := baslat(temel()); kod != http.StatusConflict || !strings.Contains(g2, "değişti") {
		t.Errorf("dosya değişti: %d %s", kod, g2)
	}
	var n int
	_ = o.db.QueryRow(`SELECT COUNT(*) FROM tasima_isleri WHERE durum='kuyrukta'`).Scan(&n)
	if n != 0 {
		t.Fatalf("ret yollarından iş açıldı: %d", n)
	}
	// Dosya eski haline: yeniden keşif sonrası başarılı başlatma.
	oturum, _ = o.kesfet(t, "kok", map[string]any{"tip": "arsiv-cpanel", "yol": o.dosya})
	kod, gv := baslat(temel())
	if kod != http.StatusAccepted {
		t.Fatalf("başlatma: %d %s", kod, gv)
	}
	isID := jsonSayi(t, gv, "is_id")
	var tip, host, ayarlar string
	var parola, anahtar sql.NullString
	var toplam int
	_ = o.db.QueryRow(`SELECT kaynak_tip, kaynak_host, COALESCE(ayarlar,''), kaynak_parola, kaynak_anahtar, toplam FROM tasima_isleri WHERE id=?`, isID).
		Scan(&tip, &host, &ayarlar, &parola, &anahtar, &toplam)
	if tip != "arsiv-cpanel" || host != "arsiv" || parola.Valid || anahtar.Valid || toplam != 1 {
		t.Errorf("iş satırı: %s %s %v %v %d", tip, host, parola, anahtar, toplam)
	}
	var kayit isAyarKayit
	if err := json.Unmarshal([]byte(ayarlar), &kayit); err != nil {
		t.Fatal(err)
	}
	if kayit.SSL || kayit.SahipleriTasi || len(kayit.SahipEsleK) != 0 || !kayit.Cron || !kayit.Dosyalar || !kayit.Veritabani {
		t.Errorf("ayarlar zorlaması: %+v", kayit.Ayarlar)
	}
	if kayit.Arsiv == nil || kayit.Arsiv.Yol != o.dosya || kayit.Arsiv.ParmakIzi.Boyut <= 0 || kayit.Arsiv.ParmakIzi.MtimeNs == 0 {
		t.Errorf("arşiv kaynağı kaydı: %+v", kayit.Arsiv)
	}
	// Kalem: sunucu keşfinden (istemcinin web_root/dbler/kaynak_hesap değeri yok sayıldı).
	var kalemSayisi int
	var hj string
	_ = o.db.QueryRow(`SELECT COUNT(*), MAX(hesap_json) FROM tasima_kalemleri WHERE is_id=?`, isID).Scan(&kalemSayisi, &hj)
	var hs Hesap
	_ = json.Unmarshal([]byte(hj), &hs)
	if kalemSayisi != 1 || hs.AlanAdi != "ornek.com" || hs.WebRoot != "public_html" || strings.Join(hs.DBler, ",") != "u_wp" || hs.KaynakHesap != "u" {
		t.Errorf("kalem: %d %+v", kalemSayisi, hs)
	}
	// Aynı kiracıda ikinci açık iş: 409.
	if kod, _ := baslat(temel()); kod != http.StatusConflict {
		t.Errorf("ikinci açık iş: %d", kod)
	}
	// Denetim kodu yazıldı.
	var denetim int
	_ = o.db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action IN ('tasima_baslat','tasima_arsiv_kesif')`).Scan(&denetim)
	if denetim < 2 {
		t.Errorf("denetim kaydı eksik: %d", denetim)
	}
}

// Kural (bayi): yükleme → keşif → başlat; bayi kendi hesabına açar; sahip/üzerine yazma/ssl zorlanır;
// plan zorunlu+kapsamlı; disk havuzu açılmış boyutla önkontrol edilir; yükleme işe bağlanır.
func TestArsivBaslat_BayiZorlamalari(t *testing.T) {
	o := arsivKur(t)
	o.bayiAc(t, true)
	o.ist(t, "kok", http.MethodPut, "/tasima/kaynaklar/arsiv-cpanel", map[string]any{"bayi_etkin": true})
	yid := o.yukle(t, "bayi7", cpmoveT(t))
	hizSifirla()
	oturum, y := o.kesfet(t, "bayi7", map[string]any{"tip": "arsiv-cpanel", "yukleme_id": yid, "yol": o.dosya})
	if y["durum"] != "hazir" {
		t.Fatalf("bayi keşfi: %+v", y)
	}
	// Bayi `yol` verse de yükleme kullanılır (kök yolu bayide reddedilir: yol-only → 403).
	if kod, _ := o.ist(t, "bayi7", http.MethodPost, "/tasima/arsiv/kesif", map[string]any{"tip": "arsiv-cpanel", "yol": o.dosya}); kod != 403 {
		t.Errorf("bayi yolu: %d", kod)
	}
	hizSifirla()
	baslat := func(g map[string]any) (int, string) {
		hizSifirla()
		return o.ist(t, "bayi7", http.MethodPost, "/tasima/arsiv/baslat", g)
	}
	temel := func() map[string]any {
		return map[string]any{
			"tip": "arsiv-cpanel", "mod": "toplu", "oturum_id": oturum,
			"secilen": []map[string]any{{"alan_adi": "ornek.com"}, {"alan_adi": "ek.net"}},
			"ayarlar": map[string]any{"dosyalar": true, "veritabani": true, "ssl": true, "ustune": true, "sahipleri_tasi": true,
				"reseller_id": 9, "customer_id": 5, "plan_id": 3},
		}
	}
	// Plan kapsamı: başka bayinin planı reddedilir (plan 2 = bayi 9'un).
	g := temel()
	g["ayarlar"].(map[string]any)["plan_id"] = 2
	if kod, gv := baslat(g); kod != 403 {
		t.Errorf("başka bayinin planı: %d %s", kod, gv)
	}
	// Disk havuzu: açılmış boyut havuzun kalanını aşıyorsa başlamadan 413.
	o.exec(t, `UPDATE users SET max_disk_mb=1 WHERE id=7`)
	var ozetToplam int64
	_, gg := o.ist(t, "bayi7", http.MethodGet, fmt.Sprintf("/tasima/arsiv/kesif/%d", oturum), nil)
	var yy struct {
		Ozet arsivOzet `json:"ozet"`
	}
	_ = json.Unmarshal([]byte(gg), &yy)
	ozetToplam = yy.Ozet.ToplamBayt
	if ozetToplam <= 0 {
		t.Fatalf("özet toplam bayt: %s", gg)
	}
	o.exec(t, `UPDATE users SET max_disk_mb=0`)
	o.exec(t, `UPDATE users SET max_disk_mb=1 WHERE id=7`)
	// 1 MiB havuz, küçük arşiv sığar → havuzu arşivden küçük yapmak için mevcut kullanım ekle.
	o.exec(t, `UPDATE domains SET boyut_kb=? WHERE id=20`, 1024-ozetToplam/1024/2)
	if kod, gv := baslat(temel()); kod != http.StatusRequestEntityTooLarge || !strings.Contains(gv, errArsivKota.Error()) {
		t.Errorf("disk havuzu: %d %s", kod, gv)
	}
	o.exec(t, `UPDATE users SET max_disk_mb=0 WHERE id=7`)
	o.exec(t, `UPDATE domains SET boyut_kb=0 WHERE id=20`)
	kod, gv := baslat(temel())
	if kod != http.StatusAccepted {
		t.Fatalf("bayi başlatma: %d %s", kod, gv)
	}
	isID := jsonSayi(t, gv, "is_id")
	var rid, yuk sql.NullInt64
	var ayarlar string
	_ = o.db.QueryRow(`SELECT sahip_rid, arsiv_yukleme_id, ayarlar FROM tasima_isleri WHERE id=?`, isID).Scan(&rid, &yuk, &ayarlar)
	if rid.Int64 != 7 || yuk.Int64 != yid {
		t.Errorf("kapsam/yükleme bağı: rid=%v yukleme=%v", rid, yuk)
	}
	var kayit isAyarKayit
	_ = json.Unmarshal([]byte(ayarlar), &kayit)
	if kayit.ResellerID != 7 || kayit.CustomerID != 0 || kayit.Ustune || kayit.SSL || kayit.SahipleriTasi || kayit.PlanID != 3 {
		t.Errorf("bayi ayar zorlaması: %+v", kayit.Ayarlar)
	}
	if kayit.Arsiv == nil || kayit.Arsiv.YuklemeID != yid || kayit.Arsiv.Yol != "" {
		t.Errorf("bayi işinde yol olmamalı, yükleme kimliği olmalı: %+v", kayit.Arsiv)
	}
	var kalem int
	_ = o.db.QueryRow(`SELECT COUNT(*) FROM tasima_kalemleri WHERE is_id=?`, isID).Scan(&kalem)
	if kalem != 2 {
		t.Errorf("kalem sayısı: %d", kalem)
	}
	// İşe bağlı yükleme silinemez.
	if kod, _ := o.ist(t, "bayi7", http.MethodDelete, fmt.Sprintf("/tasima/arsiv/yukle/%d", yid), nil); kod != 409 {
		t.Errorf("işe bağlı yükleme silindi: %d", kod)
	}
	// Başka bayi (ve kök) bu oturumla başlatamaz.
	for _, kim := range []string{"bayi9", "kok"} {
		hizSifirla()
		if kod, _ := o.ist(t, kim, http.MethodPost, "/tasima/arsiv/baslat", temel()); kod != 400 {
			t.Errorf("%s başkasının oturumuyla başlattı: %d", kim, kod)
		}
	}
	_ = io.Discard
	_ = arsiv.YolTavan
}
