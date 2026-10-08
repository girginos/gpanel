package backups

// silinen_adversarial_test.go — silinen yedekler (silinen.go) için KIRMA denemeleri.
// Gerçek BackupRoot'a YAZILMAZ (yedekKok geçici dizin). Geçici MariaDB gerekir.
// Kuralı çiğneyen testler üretim kodunda gerçek hata gösterir; test adında HHATA etiketi var.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// advRouter: AdminOnly'siz ham handler rotaları. Aktor boş bağlamda ("anonim") çalışır.
func advRouter(t *testing.T, db interface{ SetMaxOpenConns(int) }, h *Handlers) http.Handler {
	t.Helper()
	db.SetMaxOpenConns(8) // Liste: satırlar açıkken ikinci sorgu (g113_db tek bağlantı verir)
	r := chi.NewRouter()
	r.Get("/s", h.SilinenListe)
	r.Get("/s/ayar", h.SilinenAyarGet)
	r.Put("/s/ayar", h.SilinenAyarSet)
	r.Get("/s/{id}/indir", h.SilinenIndir)
	r.Delete("/s/{id}", h.SilinenSil)
	return r
}

func advIstek(r http.Handler, method, yol, govde string) (int, string) {
	req := httptest.NewRequest(method, yol, strings.NewReader(govde))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func advKayitID(t *testing.T, h *Handlers, eskiDomain int64) int64 {
	t.Helper()
	var id int64
	if err := h.DB.QueryRow(`SELECT id FROM cp_silinen_yedek WHERE eski_domain_id=? ORDER BY id DESC LIMIT 1`, eskiDomain).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func advVar(yol string) bool { _, err := os.Lstat(yol); return err == nil }

func lookPathLftp() (string, error) { return exec.LookPath("lftp") }

// ---------------------------------------------------------------- 1) dosya symlink'i

func TestAdv1_DosyaSymlinki_ListelenmezIndirilmezHedefSilinmez(t *testing.T) {
	db := silinenDB(t)
	kok := silinenOrtam(t)
	h := &Handlers{DB: db}
	r := advRouter(t, db, h)

	gizli := filepath.Join(t.TempDir(), "gizli.txt")
	if err := os.WriteFile(gizli, []byte("ROOT-SIRRI"), 0o600); err != nil {
		t.Fatal(err)
	}
	silinenDosya(t, kok, "c_x", "c_x-0.tar.gz", 20*24*time.Hour) // meşru dosya
	link := filepath.Join(kok, "c_x", "c_x-1.tar.gz")
	if err := os.Symlink(gizli, link); err != nil {
		t.Fatal(err)
	}
	silinenKayit(t, db, 9201, "x.test", "c_x", true)
	id := advKayitID(t, h, 9201)

	dos, _ := silinenDosyalari(context.Background(), db, "c_x", time.Now())
	if _, var_ := dos["c_x-1.tar.gz"]; var_ {
		t.Errorf("symlink listelenmemeli: %v", dos)
	}
	kod, g := advIstek(r, "GET", fmt.Sprintf("/s/%d/indir?dosya=c_x-1.tar.gz", id), "")
	if kod == 200 || strings.Contains(g, "ROOT-SIRRI") {
		t.Fatalf("symlink üzerinden hedef dosya SIZDI: %d %s", kod, g)
	}
	kod, g = advIstek(r, "GET", fmt.Sprintf("/s/%d/indir?dosya=c_x-0.tar.gz", id), "")
	if kod != 200 || g != "12345" {
		t.Fatalf("pozitif kontrol: meşru dosya inmeli: %d %q", kod, g)
	}

	if err := SilinenTemizleBir(context.Background(), db, id, false); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(gizli); err != nil || string(b) != "ROOT-SIRRI" {
		t.Fatalf("symlink HEDEFİ silindi/bozuldu: %v", err)
	}
	if advVar(filepath.Join(kok, "c_x", "c_x-0.tar.gz")) {
		t.Error("meşru dosya silinmeliydi")
	}
	t.Logf("not: temizlik sonrası symlink dizinde kalıyor=%v (kayıt temizlendi, dizin boş değil)", advVar(link))
}

// ---------------------------------------------------------------- 2) dizin symlink'i

func TestAdv2_HHATA_DizinSymlinki_DisariyaCikmamali(t *testing.T) {
	db := silinenDB(t)
	kok := silinenOrtam(t)
	h := &Handlers{DB: db}
	r := advRouter(t, db, h)

	disari := t.TempDir()
	if err := os.WriteFile(filepath.Join(disari, "baska-musteri.tar.gz"), []byte("BASKA"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-20 * 24 * time.Hour)
	_ = os.Chtimes(filepath.Join(disari, "baska-musteri.tar.gz"), old, old)
	if err := os.Symlink(disari, filepath.Join(kok, "c_lnk")); err != nil {
		t.Fatal(err)
	}
	silinenKayit(t, db, 9202, "lnk.test", "c_lnk", true)
	id := advKayitID(t, h, 9202)

	dos, _ := silinenDosyalari(context.Background(), db, "c_lnk", time.Now())
	if len(dos) != 0 {
		t.Errorf("sk symlink ise dışarıdaki dosyalar listelenmemeli: %v", dos)
	}
	kod, g := advIstek(r, "GET", fmt.Sprintf("/s/%d/indir?dosya=baska-musteri.tar.gz", id), "")
	if kod == 200 {
		t.Errorf("dizin symlink'i üzerinden dışarıdaki dosya indirildi: %q", g)
	}
	_ = SilinenTemizleBir(context.Background(), db, id, false)
	if !advVar(filepath.Join(disari, "baska-musteri.tar.gz")) {
		t.Error("dizin symlink'i takip edilip BackupRoot DIŞINDAKİ dosya silindi")
	}
}

// ---------------------------------------------------------------- 3) aynı sk, iki silinmiş kayıt

func TestAdv3_HHATA_AyniSKIkiKayit_IlkinTemizligiIkinciyiBozmamali(t *testing.T) {
	db := silinenDB(t)
	kok := silinenOrtam(t)
	silinenDosya(t, kok, "c_cift", "c_cift-hayat1.tar.gz", 25*24*time.Hour) // 1. hayat
	silinenDosya(t, kok, "c_cift", "c_cift-hayat2.tar.gz", 5*24*time.Hour)  // 2. hayat (A silindikten SONRA yazıldı)
	g113_exec(t, db, `INSERT INTO cp_silinen_yedek (id, alan_adi, sistem_kullanici, eski_domain_id, silinme, son_kullanma, dizin, dosyalar)
		VALUES (9301,'cift.test','c_cift',9301, DATE_SUB(NOW(), INTERVAL 20 DAY), DATE_SUB(NOW(), INTERVAL 1 HOUR),'c_cift','["c_cift-hayat1.tar.gz"]')`)
	g113_exec(t, db, `INSERT INTO cp_silinen_yedek (id, alan_adi, sistem_kullanici, eski_domain_id, silinme, son_kullanma, dizin, dosyalar)
		VALUES (9302,'cift.test','c_cift',9302, DATE_SUB(NOW(), INTERVAL 2 DAY), DATE_ADD(NOW(), INTERVAL 5 DAY),'c_cift','["c_cift-hayat2.tar.gz"]')`)

	var silA time.Time
	_ = db.QueryRow(`SELECT silinme FROM cp_silinen_yedek WHERE id=9301`).Scan(&silA)
	dos, _ := silinenDosyalari(context.Background(), db, "c_cift", silA)
	if _, ok := dos["c_cift-hayat2.tar.gz"]; ok {
		t.Errorf("A silindikten SONRA yazılan dosya A'ya ait sayılmamalı: %v", dos)
	}

	SilinenSupur(context.Background(), db)

	if !advVar(filepath.Join(kok, "c_cift", "c_cift-hayat2.tar.gz")) {
		t.Error("B (saklama süresi dolmadı) dosyası A'nın temizliğiyle SİLİNDİ")
	}
	if advVar(filepath.Join(kok, "c_cift", "c_cift-hayat1.tar.gz")) {
		t.Error("A'nın dosyası silinmeliydi")
	}
	var dA, dB string
	_ = db.QueryRow(`SELECT durum FROM cp_silinen_yedek WHERE id=9301`).Scan(&dA)
	_ = db.QueryRow(`SELECT durum FROM cp_silinen_yedek WHERE id=9302`).Scan(&dB)
	if dA != "temizlendi" || dB != "saklaniyor" {
		t.Errorf("durumlar A=%s B=%s", dA, dB)
	}
}

// ---------------------------------------------------------------- 4) devralınan sk, kayıtlı eski dosya

func TestAdv4_DevralanHostingKayitliEskiDosyaKorunur(t *testing.T) {
	db := silinenDB(t)
	kok := silinenOrtam(t)
	silinenDosya(t, kok, "c_dev", "c_dev-eski.tar.gz", 30*24*time.Hour)
	silinenDosya(t, kok, "c_dev", "c_dev-yeni-ama-eski-mtime.tar.gz", 40*24*time.Hour) // mtime silinmeden ÖNCE ama yeni hostingin kaydı
	silinenKayit(t, db, 9401, "dev.test", "c_dev", true)
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (9402,'dev.test','c_dev')`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, tip, dosya, boyut_b) VALUES (9402,'oto','c_dev-yeni-ama-eski-mtime.tar.gz',5)`)

	SilinenSupur(context.Background(), db)

	if !advVar(filepath.Join(kok, "c_dev", "c_dev-yeni-ama-eski-mtime.tar.gz")) {
		t.Error("devralan hostingin kayıtlı yedeği silindi")
	}
	if advVar(filepath.Join(kok, "c_dev", "c_dev-eski.tar.gz")) {
		t.Error("eski hesabın dosyası silinmeliydi")
	}
	if !advVar(filepath.Join(kok, "c_dev")) {
		t.Error("devralınan dizin silinmemeli")
	}
}

// ---------------------------------------------------------------- 5) uzak silme

func advUzakHazirla(t *testing.T, host string) (id int64, dosya string, h *Handlers) {
	t.Helper()
	db := silinenDB(t)
	kok := silinenOrtam(t)
	silinenDosya(t, kok, "c_uz", "c_uz-1.tar.gz", 20*24*time.Hour)
	silinenKayit(t, db, 9501, "uz.test", "c_uz", true)
	g113_exec(t, db, `UPDATE cp_silinen_yedek SET hedef_tip='ftp', hedef_host=?, hedef_port=21, hedef_kullanici='u', hedef_dizin='/x' WHERE eski_domain_id=9501`, host)
	h = &Handlers{DB: db}
	return advKayitID(t, h, 9501), filepath.Join(kok, "c_uz", "c_uz-1.tar.gz"), h
}

func TestAdv5a_UzakHostGecersiz_HataDonerYerelSilinmez(t *testing.T) {
	if _, err := lookPathLftp(); err != nil {
		t.Skip("lftp yok: bu yol 5c'de")
	}
	id, dosya, h := advUzakHazirla(t, "kotu host;rm -rf /")
	ctx, iptal := context.WithTimeout(context.Background(), 60*time.Second)
	defer iptal()
	if err := SilinenTemizleBir(ctx, h.DB, id, false); err == nil {
		t.Error("geçersiz host ile hata dönmeli")
	}
	if !advVar(dosya) {
		t.Error("uzak silinemediyse yerel dosya SİLİNMEMELİ")
	}
	var durum string
	_ = h.DB.QueryRow(`SELECT durum FROM cp_silinen_yedek WHERE id=?`, id).Scan(&durum)
	if durum != "saklaniyor" {
		t.Errorf("durum=%s", durum)
	}
}

func TestAdv5b_UzakHostErisilmez_HataDonerYerelSilinmez(t *testing.T) {
	if _, err := lookPathLftp(); err != nil {
		t.Skip("lftp yok: bu yol 5c'de")
	}
	id, dosya, h := advUzakHazirla(t, "ornek.invalid")
	ctx, iptal := context.WithTimeout(context.Background(), 90*time.Second)
	defer iptal()
	if err := SilinenTemizleBir(ctx, h.DB, id, false); err == nil {
		t.Error("erişilemeyen host ile hata dönmeli")
	}
	if !advVar(dosya) {
		t.Error("uzak silinemediyse yerel dosya SİLİNMEMELİ")
	}
	var hata string
	_ = h.DB.QueryRow(`SELECT son_hata FROM cp_silinen_yedek WHERE id=?`, id).Scan(&hata)
	if hata == "" {
		t.Error("son_hata yazılmalı")
	}
	// uzagiYoksay=true: yerel silinir, kayıt temizlendi
	if err := SilinenTemizleBir(ctx, h.DB, id, true); err != nil {
		t.Fatalf("uzagiYoksay=true hata dönmemeli: %v", err)
	}
	if advVar(dosya) {
		t.Error("uzagiYoksay=true iken yerel dosya silinmeli")
	}
	var durum, parola string
	_ = h.DB.QueryRow(`SELECT durum, hedef_parola FROM cp_silinen_yedek WHERE id=?`, id).Scan(&durum, &parola)
	if durum != "temizlendi" || parola != "" {
		t.Errorf("durum=%s parola=%q", durum, parola)
	}
}

func TestAdv5c_HHATA_LftpYokVeHedefTanimli_UzakKopyaOlduguHaldeYerelSilinmemeli(t *testing.T) {
	id, dosya, h := advUzakHazirla(t, "ornek.invalid")
	t.Setenv("PATH", t.TempDir()) // lftp bulunamaz → ErrLftpYok
	err := SilinenTemizleBir(context.Background(), h.DB, id, false)
	var durum string
	_ = h.DB.QueryRow(`SELECT durum FROM cp_silinen_yedek WHERE id=?`, id).Scan(&durum)
	if err == nil || !advVar(dosya) || durum != "saklaniyor" {
		t.Errorf("lftp yokken hedef_host dolu: uzak kopya silinemedi ama err=%v yerelVar=%v durum=%s (kayıt 'temizlendi' olup uzak kopya yetim kalıyor)",
			err, advVar(dosya), durum)
	}
}

// ---------------------------------------------------------------- 6) kimlik / parametre sınırları

func TestAdv6_SilIndirKimlikVeDosyaSiniri(t *testing.T) {
	db := silinenDB(t)
	kok := silinenOrtam(t)
	h := &Handlers{DB: db}
	r := advRouter(t, db, h)
	silinenDosya(t, kok, "c_idt", "c_idt-1.tar.gz", 20*24*time.Hour)
	_ = os.WriteFile(filepath.Join(kok, "disarida.tar.gz"), []byte("DISARI"), 0o600)
	silinenKayit(t, db, 9601, "idt.test", "c_idt", false)
	id := advKayitID(t, h, 9601)

	for _, kimlik := range []string{"0", "-1", "99999999999999999999", "abc", "1.5", "%20", "9223372036854775807"} {
		if kod, g := advIstek(r, "GET", "/s/"+kimlik+"/indir?dosya=c_idt-1.tar.gz", ""); kod != 404 {
			t.Errorf("indir id=%q: %d %s", kimlik, kod, g)
		}
		if kod, g := advIstek(r, "DELETE", "/s/"+kimlik, ""); kod != 404 {
			t.Errorf("sil id=%q: %d %s", kimlik, kod, g)
		}
	}
	if !advVar(filepath.Join(kok, "c_idt", "c_idt-1.tar.gz")) {
		t.Fatal("geçersiz kimliklerle dosya silindi")
	}

	kotuAd := []string{"", "../disarida.tar.gz", "..", "../c_idt/c_idt-1.tar.gz", "a/b.tar.gz", "/etc/passwd",
		"%2e%2e", "%2e%2e%2fdisarida.tar.gz", "c_idt-1.tar.gz/", "c_idt-1.tar.gz\x00", strings.Repeat("a", 5000) + ".tar.gz", "c_idt-1.TAR.GZ"}
	for _, ad := range kotuAd {
		yol := fmt.Sprintf("/s/%d/indir?dosya=%s", id, url.QueryEscape(ad))
		if kod, g := advIstek(r, "GET", yol, ""); kod != 404 {
			t.Errorf("indir dosya=%q: %d (%.60s)", ad, kod, g)
		}
	}
	for _, ham := range []string{"dosya=%2e%2e", "dosya=%252e%252e", "dosya=..%2fdisarida.tar.gz"} {
		if kod, g := advIstek(r, "GET", fmt.Sprintf("/s/%d/indir?%s", id, ham), ""); kod != 404 {
			t.Errorf("indir ham %q: %d %s", ham, kod, g)
		}
	}
	if kod, _ := advIstek(r, "GET", fmt.Sprintf("/s/%d/indir?dosya=x&dosya=c_idt-1.tar.gz", id), ""); kod == 500 {
		t.Error("500")
	}

	// temizlendi kaydı indirilemez
	g113_exec(t, db, `UPDATE cp_silinen_yedek SET durum='temizlendi' WHERE id=?`, id)
	if kod, g := advIstek(r, "GET", fmt.Sprintf("/s/%d/indir?dosya=c_idt-1.tar.gz", id), ""); kod != 404 {
		t.Errorf("temizlendi kaydı indirilebildi: %d %s", kod, g)
	}
	// temizlendi kaydına Sil: dosyaya dokunmaz; 5xx olmamalı
	kod, _ := advIstek(r, "DELETE", fmt.Sprintf("/s/%d", id), "")
	if kod >= 500 || !advVar(filepath.Join(kok, "c_idt", "c_idt-1.tar.gz")) {
		t.Errorf("temizlendi kaydına DELETE: %d, dosya var=%v", kod, advVar(filepath.Join(kok, "c_idt", "c_idt-1.tar.gz")))
	}
}

func TestAdv6b_SilYanitiYolSizdirmaz(t *testing.T) {
	if _, err := lookPathLftp(); err != nil {
		t.Skip("lftp yok")
	}
	id, _, h := advUzakHazirla(t, "kotu host;x")
	r := advRouter(t, h.DB, h)
	kod, g := advIstek(r, "DELETE", fmt.Sprintf("/s/%d", id), "")
	if kod != http.StatusConflict {
		t.Fatalf("uzak hata: 409 beklenirdi: %d %s", kod, g)
	}
	for _, yasak := range []string{BackupRoot, "gosp-lftp", "parola", "password"} {
		if strings.Contains(strings.ToLower(g), strings.ToLower(yasak)) {
			t.Errorf("hata gövdesi %q sızdırıyor: %s", yasak, g)
		}
	}
}

// ---------------------------------------------------------------- 7) ayar

func TestAdv7_AyarSetSinirlari(t *testing.T) {
	db := silinenDB(t)
	silinenOrtam(t)
	h := &Handlers{DB: db}
	r := advRouter(t, db, h)
	oku := func() (string, bool) {
		var v string
		err := db.QueryRow(`SELECT deger FROM cp_ayarlar WHERE anahtar='silinen_yedek_gun'`).Scan(&v)
		return v, err == nil
	}
	reddedilmeli := map[string]string{
		"eksi":        `{"gun":-1}`,
		"3651":        `{"gun":3651}`,
		"string":      `{"gun":"abc"}`,
		"ondalik":     `{"gun":1.5}`,
		"dev sayi":    `{"gun":99999999999999999999}`,
		"bos":         ``,
		"dizi":        `[]`,
		"bozuk":       `{"gun":`,
		"cok buyuk":   `{"gun":5,"x":"` + strings.Repeat("a", 10000) + `"}`,
		"bool":        `{"gun":true}`,
		"string sayi": `{"gun":"7"}`,
	}
	for ad, govde := range reddedilmeli {
		if kod, g := advIstek(r, "PUT", "/s/ayar", govde); kod != 400 {
			t.Errorf("%s: 400 beklenirdi: %d %s", ad, kod, g)
		}
	}
	if _, var_ := oku(); var_ {
		t.Error("reddedilen istekler ayar yazmamalı")
	}
	if kod, _ := advIstek(r, "PUT", "/s/ayar", `{"gun":3650}`); kod != 200 {
		t.Errorf("3650 sınır geçerli: %d", kod)
	}
	if v, _ := oku(); v != "3650" {
		t.Errorf("v=%q", v)
	}
	if kod, _ := advIstek(r, "PUT", "/s/ayar", `{"gun":0}`); kod != 200 {
		t.Errorf("0 geçerli: %d", kod)
	}
	if v, _ := oku(); v != "0" {
		t.Errorf("v=%q", v)
	}
	for _, g := range []string{`{"gun":null}`, `{}`} {
		_, _ = advIstek(r, "PUT", "/s/ayar", `{"gun":9}`)
		if kod, _ := advIstek(r, "PUT", "/s/ayar", g); kod != 200 {
			t.Errorf("%s: 200 beklenirdi: %d", g, kod)
		}
		if _, var_ := oku(); var_ {
			t.Errorf("%s ayarı silmeliydi", g)
		}
	}
	kod, _ := advIstek(r, "PUT", "/s/ayar", `{"gun":5}xyz`)
	t.Logf("not: sonda çöplü gövde `{\"gun\":5}xyz` -> %d", kod)
}

// DB'de elle bozulmuş değerler: Get, etkin davranışla (slot) tutarlı olmalı.
func TestAdv7b_HHATA_AyarGetAralikDisiDegeriGostermemeli(t *testing.T) {
	db := silinenDB(t)
	silinenOrtam(t)
	h := &Handlers{DB: db}
	r := advRouter(t, db, h)
	for _, bozuk := range []string{"-5", "99999", "abc"} {
		g113_exec(t, db, `INSERT INTO cp_ayarlar (anahtar, deger) VALUES ('silinen_yedek_gun',?) ON DUPLICATE KEY UPDATE deger=VALUES(deger)`, bozuk)
		kod, g := advIstek(r, "GET", "/s/ayar", "")
		etkin := silinenSaklamaGunu(silinenAyar(context.Background(), db), 14)
		if kod != 200 {
			t.Fatalf("%d", kod)
		}
		if etkin == 14 && !strings.Contains(g, `"gun":null`) {
			t.Errorf("ayar=%q: etkin davranış slot ama Get %s", bozuk, g)
		}
	}
}

// ---------------------------------------------------------------- 8) süpürme sınırları

func advToplu(t *testing.T, h *Handlers, n int, ofset int64) {
	t.Helper()
	for i := 0; i < n; i++ {
		g113_exec(t, h.DB, `INSERT INTO cp_silinen_yedek (alan_adi, sistem_kullanici, eski_domain_id, silinme, son_kullanma, dizin, dosyalar)
			VALUES (?,?,?, DATE_SUB(NOW(), INTERVAL 10 DAY), DATE_SUB(NOW(), INTERVAL 1 HOUR), ?, '[]')`,
			fmt.Sprintf("t%d.test", i), fmt.Sprintf("c_toplu%d", i), ofset+int64(i), fmt.Sprintf("c_toplu%d", i))
	}
}

func TestAdv8_SupurLimit200TurBasinaVeKalanErtesiGun(t *testing.T) {
	db := silinenDB(t)
	silinenOrtam(t)
	h := &Handlers{DB: db}
	advToplu(t, h, 205, 100000)
	SilinenSupur(context.Background(), db)
	var kalan int
	_ = db.QueryRow(`SELECT COUNT(*) FROM cp_silinen_yedek WHERE durum='saklaniyor'`).Scan(&kalan)
	if kalan != 5 {
		t.Fatalf("tur başına 200: kalan 5 olmalı, %d", kalan)
	}
	SilinenSupur(context.Background(), db)
	_ = db.QueryRow(`SELECT COUNT(*) FROM cp_silinen_yedek WHERE durum='saklaniyor'`).Scan(&kalan)
	if kalan != 0 {
		t.Fatalf("ikinci tur kalanı bitirmeli: %d", kalan)
	}
}

func TestAdv8b_SupurIptalEdilmisBaglamDokunmaz(t *testing.T) {
	db := silinenDB(t)
	kok := silinenOrtam(t)
	silinenDosya(t, kok, "c_ipt", "c_ipt-1.tar.gz", 20*24*time.Hour)
	silinenKayit(t, db, 9801, "ipt.test", "c_ipt", true)
	ctx, iptal := context.WithCancel(context.Background())
	iptal()
	SilinenSupur(ctx, db)
	if !advVar(filepath.Join(kok, "c_ipt", "c_ipt-1.tar.gz")) {
		t.Error("iptal edilmiş bağlamla dosya silindi")
	}
	var durum string
	_ = db.QueryRow(`SELECT durum FROM cp_silinen_yedek WHERE eski_domain_id=9801`).Scan(&durum)
	if durum != "saklaniyor" {
		t.Errorf("durum=%s", durum)
	}
}

func TestAdv8c_TemizlenmisKayit180GunSonraSilinir(t *testing.T) {
	db := silinenDB(t)
	silinenOrtam(t)
	ek := func(id int, gun int, durum string) {
		g113_exec(t, db, `INSERT INTO cp_silinen_yedek (id, alan_adi, sistem_kullanici, eski_domain_id, dizin, dosyalar, durum, temizlenme)
			VALUES (?, 'k.test','c_k',?, 'c_k','[]',?, DATE_SUB(NOW(), INTERVAL ? DAY))`, id, id, durum, gun)
	}
	ek(1, 181, "temizlendi")
	ek(2, 179, "temizlendi")
	ek(3, 400, "saklaniyor")
	g113_exec(t, db, `INSERT INTO cp_silinen_yedek (id, alan_adi, sistem_kullanici, eski_domain_id, dizin, dosyalar, durum) VALUES (4,'k.test','c_k',4,'c_k','[]','temizlendi')`)
	SilinenSupur(context.Background(), db)
	var ids []int
	rows, _ := db.Query(`SELECT id FROM cp_silinen_yedek ORDER BY id`)
	for rows.Next() {
		var i int
		_ = rows.Scan(&i)
		ids = append(ids, i)
	}
	rows.Close()
	if fmt.Sprint(ids) != "[2 3 4]" {
		t.Errorf("kalanlar [2 3 4] olmalı: %v", ids)
	}
}

// ---------------------------------------------------------------- 9) eşzamanlılık

func TestAdv9_SupurVeSilParalel(t *testing.T) {
	db := silinenDB(t)
	kok := silinenOrtam(t)
	h := &Handlers{DB: db}
	r := advRouter(t, db, h)
	var idler []int64
	for i := 0; i < 8; i++ {
		sk := fmt.Sprintf("c_par%d", i)
		for j := 0; j < 5; j++ {
			silinenDosya(t, kok, sk, fmt.Sprintf("%s-%d.tar.gz", sk, j), 20*24*time.Hour)
		}
		silinenKayit(t, db, int64(9900+i), fmt.Sprintf("par%d.test", i), sk, true)
		idler = append(idler, advKayitID(t, h, int64(9900+i)))
	}
	var wg sync.WaitGroup
	for k := 0; k < 3; k++ {
		wg.Add(1)
		go func() { defer wg.Done(); SilinenSupur(context.Background(), db) }()
	}
	for _, id := range idler {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			if kod, g := advIstek(r, "DELETE", fmt.Sprintf("/s/%d", id), ""); kod >= 500 {
				t.Errorf("paralel sil %d: %d %s", id, kod, g)
			}
		}(id)
	}
	wg.Wait()
	for i := 0; i < 8; i++ {
		if advVar(filepath.Join(kok, fmt.Sprintf("c_par%d", i))) {
			t.Errorf("c_par%d kaldı", i)
		}
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM cp_silinen_yedek WHERE durum<>'temizlendi'`).Scan(&n)
	if n != 0 {
		t.Errorf("hepsi temizlendi olmalı: %d", n)
	}
}

func TestAdv9b_ListeVeSilParalel(t *testing.T) {
	db := silinenDB(t)
	kok := silinenOrtam(t)
	h := &Handlers{DB: db}
	r := advRouter(t, db, h)
	var idler []int64
	for i := 0; i < 6; i++ {
		sk := fmt.Sprintf("c_lp%d", i)
		silinenDosya(t, kok, sk, sk+"-1.tar.gz", 20*24*time.Hour)
		silinenKayit(t, db, int64(9950+i), fmt.Sprintf("lp%d.test", i), sk, false)
		idler = append(idler, advKayitID(t, h, int64(9950+i)))
	}
	var wg sync.WaitGroup
	for _, id := range idler {
		wg.Add(2)
		go func(id int64) { defer wg.Done(); advIstek(r, "DELETE", fmt.Sprintf("/s/%d", id), "") }(id)
		go func() {
			defer wg.Done()
			if kod, g := advIstek(r, "GET", "/s", ""); kod != 200 {
				t.Errorf("liste: %d %s", kod, g)
			}
		}()
	}
	wg.Wait()
}

// 3b) Ters yön: B (sonraki hayat) önce dolarsa A'nın (hâlâ saklanan) eski dosyalarını silmemeli.
func TestAdv3b_HHATA_AyniSKIkiKayit_IkincininTemizligiBirincininDosyasiniSilmemeli(t *testing.T) {
	db := silinenDB(t)
	kok := silinenOrtam(t)
	silinenDosya(t, kok, "c_cift2", "c_cift2-hayat1.tar.gz", 25*24*time.Hour)
	silinenDosya(t, kok, "c_cift2", "c_cift2-hayat2.tar.gz", 5*24*time.Hour)
	// A: uzun saklama (dolmadı); B: kısa saklama (doldu)
	g113_exec(t, db, `INSERT INTO cp_silinen_yedek (id, alan_adi, sistem_kullanici, eski_domain_id, silinme, son_kullanma, dizin, dosyalar)
		VALUES (9311,'cift2.test','c_cift2',9311, DATE_SUB(NOW(), INTERVAL 20 DAY), DATE_ADD(NOW(), INTERVAL 100 DAY),'c_cift2','["c_cift2-hayat1.tar.gz"]')`)
	g113_exec(t, db, `INSERT INTO cp_silinen_yedek (id, alan_adi, sistem_kullanici, eski_domain_id, silinme, son_kullanma, dizin, dosyalar)
		VALUES (9312,'cift2.test','c_cift2',9312, DATE_SUB(NOW(), INTERVAL 2 DAY), DATE_SUB(NOW(), INTERVAL 1 HOUR),'c_cift2','["c_cift2-hayat2.tar.gz"]')`)
	SilinenSupur(context.Background(), db)
	if !advVar(filepath.Join(kok, "c_cift2", "c_cift2-hayat1.tar.gz")) {
		t.Error("A (saklama süresi dolmadı) dosyası B'nin temizliğiyle SİLİNDİ")
	}
	if advVar(filepath.Join(kok, "c_cift2", "c_cift2-hayat2.tar.gz")) {
		t.Error("B'nin dosyası silinmeliydi")
	}
}
