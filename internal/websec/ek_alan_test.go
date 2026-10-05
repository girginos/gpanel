package websec

// ek_alan_test.go — Website Security Monitor: EK ALAN ADLARI taraması. Gerçek SQL
// (geçici MariaDB; GOSP_TEST_GECICI_DSN yoksa atlanır) + geçici ev dizini. Tarama
// (wp-cli / dış feed) HİÇ koşmaz: keşif ve kayıt katmanı sınanır.

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"girginospanel/internal/auth"
)

// ekOrtam — geçici ev dizini + tohum: domain 10 (on, bayi 7), 11 (onbir, bayi 8),
// 12 (oniki, kök); ek alanlar 1 ek1.test (d10, docroot var), 2 ek2.test (d11, docroot
// var), 3 ek3.test (d10, docroot YOK).
func ekOrtam(t *testing.T) (*sql.DB, string) {
	t.Helper()
	db := wsGeciciDB(t)
	ev := t.TempDir()
	eski := evKoku
	evKoku = ev
	t.Cleanup(func() { evKoku = eski })
	wsExec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id, olusturulma) VALUES
		(10, 'on.test', 'on', 7, NOW() - INTERVAL 1 DAY),
		(11, 'onbir.test', 'onbir', 8, NOW() - INTERVAL 1 DAY),
		(12, 'oniki.test', 'oniki', 0, NOW() - INTERVAL 1 DAY)`)
	wsExec(t, db, `INSERT INTO ek_alan_adlari (id, domain_id, alan_adi, created_at) VALUES
		(1, 10, 'ek1.test', NOW() - INTERVAL 1 DAY),
		(2, 11, 'ek2.test', NOW() - INTERVAL 1 DAY),
		(3, 10, 'ek3.test', NOW() - INTERVAL 1 DAY)`)
	for _, sk := range []string{"on", "onbir", "oniki"} {
		ekMkdir(t, filepath.Join(ev, sk, "public_html"))
	}
	ekMkdir(t, filepath.Join(ev, "on", "ek-alanlar", "ek1.test", "public_html"))
	ekMkdir(t, filepath.Join(ev, "onbir", "ek-alanlar", "ek2.test", "public_html"))
	return db, ev
}

func ekMkdir(t *testing.T, d string) {
	t.Helper()
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
}

func ekYaz(t *testing.T, yol, icerik string) {
	t.Helper()
	ekMkdir(t, filepath.Dir(yol))
	if err := os.WriteFile(yol, []byte(icerik), 0o644); err != nil {
		t.Fatal(err)
	}
}

func ekAnahtarlar(t *testing.T, db *sql.DB, girdi []int64) []int64 {
	t.Helper()
	h, err := tarananHedefler(t.Context(), db, girdi)
	if err != nil {
		t.Fatal(err)
	}
	var a []int64
	for _, x := range h {
		a = append(a, x.anahtar())
	}
	sort.Slice(a, func(i, j int) bool { return a[i] < a[j] })
	return a
}

// Keşif: ana domainler + docroot'u olan ek alanlar; docroot'u olmayan ek alan sessizce
// geçilir; kısmi anahtar yalnız o hedefi getirir (ana anahtar ek alanı getirmez).
func TestEk_KesifEkAlanDahilSessizGecme(t *testing.T) {
	db, _ := ekOrtam(t)
	if got := ekAnahtarlar(t, db, nil); !reflect.DeepEqual(got, []int64{-2, -1, 10, 11, 12}) {
		t.Fatalf("tam keşif ek alanları da kapsamalı (ek3 docroot yok → sessiz): %v", got)
	}
	if got := ekAnahtarlar(t, db, []int64{-1}); !reflect.DeepEqual(got, []int64{-1}) {
		t.Fatalf("ek alan anahtarı yalnız o ek alanı taramalı: %v", got)
	}
	if got := ekAnahtarlar(t, db, []int64{10}); !reflect.DeepEqual(got, []int64{10}) {
		t.Fatalf("ana domain anahtarı ek alanlarını TARAMAMALI (çift sayım): %v", got)
	}
	h, _ := tarananHedefler(t.Context(), db, []int64{-1})
	if len(h) != 1 || h[0].AlanAdi != "ek1.test" || h[0].DomainID != 10 || h[0].SK != "on" ||
		!strings.HasSuffix(h[0].Docroot, "/on/ek-alanlar/ek1.test/public_html") {
		t.Fatalf("hedef alanları: %+v", h)
	}
}

// Kiracı kontrollü dizinde sembolik bağ: docroot hesap dışına, ana public_html'e ya da
// başka ek alana işaret ederse; ek-alanlar dizini bağ ise; dizin silinmişse ATLANIR
// (hata değil).
func TestEk_SembolikBagKacisiAtlanir(t *testing.T) {
	db, ev := ekOrtam(t)
	dis := t.TempDir()
	ekMkdir(t, filepath.Join(dis, "public_html"))

	docroot := filepath.Join(ev, "on", "ek-alanlar", "ek1.test", "public_html")
	for ad, hedefYol := range map[string]string{
		"hesap dışı":      filepath.Join(dis, "public_html"),
		"ana public_html": filepath.Join(ev, "on", "public_html"),
		"başka ek alan":   filepath.Join(ev, "onbir", "ek-alanlar", "ek2.test", "public_html"),
	} {
		if err := os.RemoveAll(docroot); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(hedefYol, docroot); err != nil {
			t.Fatal(err)
		}
		if got := ekAnahtarlar(t, db, nil); !reflect.DeepEqual(got, []int64{-2, 10, 11, 12}) {
			t.Fatalf("%s: docroot bağı atlanmalı: %v", ad, got)
		}
	}
	// ek-alanlar dizininin KENDİSİ bağ (ek2'nin hesabı): dışarıdaki dizine yönlendirilmiş.
	ekMkdir(t, filepath.Join(dis, "ek2.test", "public_html"))
	taban := filepath.Join(ev, "onbir", "ek-alanlar")
	if err := os.RemoveAll(taban); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dis, taban); err != nil {
		t.Fatal(err)
	}
	if got := ekAnahtarlar(t, db, nil); !reflect.DeepEqual(got, []int64{10, 11, 12}) {
		t.Fatalf("ek-alanlar bağı atlanmalı: %v", got)
	}
	// ek-alanlar dizini tamamen silinmiş (kiracı): hata değil, boş.
	if err := os.RemoveAll(filepath.Join(ev, "on", "ek-alanlar")); err != nil {
		t.Fatal(err)
	}
	if got := ekAnahtarlar(t, db, nil); !reflect.DeepEqual(got, []int64{10, 11, 12}) {
		t.Fatalf("silinmiş ek-alanlar: %v", got)
	}
}

// Askıdaki hesap: ana domain keşfi askıya BAKMAZ; ek alan da aynı kuralı alır (ana domain
// taranıyorsa ek alanı da taranır, ayrışma yok).
func TestEk_AskidaHesapAnaDomainleAyniKural(t *testing.T) {
	db, _ := ekOrtam(t)
	wsExec(t, db, `ALTER TABLE domains ADD COLUMN askida TINYINT(1) NOT NULL DEFAULT 0`)
	wsExec(t, db, `UPDATE domains SET askida=1 WHERE id=10`)
	got := ekAnahtarlar(t, db, nil)
	var ana10, ek1 bool
	for _, a := range got {
		ana10 = ana10 || a == 10
		ek1 = ek1 || a == -1
	}
	if ana10 != ek1 || !ek1 {
		t.Fatalf("askıdaki hesabın ana domaini (%v) ve ek alanı (%v) aynı kuralı almalı: %v", ana10, ek1, got)
	}
}

// Üç tarayıcı ek alan docroot'unu tarar; ana domain ve ek alan ÇİFT SAYILMAZ; ek alan
// ASLA ana hesabın /home/<sk>/composer.lock'ına bakmaz.
func TestEk_TarayicilarEkDocroottaBulurCiftSaymaz(t *testing.T) {
	db, ev := ekOrtam(t)
	ekYaz(t, filepath.Join(ev, "on", "public_html", "package.json"), `{"dependencies":{"anapkg":"1.2.3"}}`)
	ekYaz(t, filepath.Join(ev, "on", "ek-alanlar", "ek1.test", "public_html", "package.json"), `{"dependencies":{"ekpkg":"2.0.0"}}`)
	ekYaz(t, filepath.Join(ev, "on", "composer.lock"), `{"packages":[{"name":"ana/p","version":"1.0.0"}]}`)
	ekYaz(t, filepath.Join(ev, "on", "ek-alanlar", "ek1.test", "composer.lock"), `{"packages":[{"name":"ek/p","version":"3.0.0"}]}`)

	node, err := NodejsKurulumlariBul(t.Context(), db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(node) != 2 {
		t.Fatalf("node: ana + ek = 2 kurulum beklenirdi: %+v", node)
	}
	for _, k := range node {
		switch k.EkAlanID {
		case 0:
			if k.DomainID != 10 || len(k.Paketler) != 1 || k.Paketler[0].Ad != "anapkg" || !strings.HasSuffix(k.Yol, "/on/public_html") {
				t.Fatalf("ana node kurulumu: %+v", k)
			}
		case 1:
			if k.DomainID != 10 || k.AlanAdi != "ek1.test" || len(k.Paketler) != 1 || k.Paketler[0].Ad != "ekpkg" ||
				!strings.HasSuffix(k.Yol, "/on/ek-alanlar/ek1.test/public_html") {
				t.Fatalf("ek node kurulumu: %+v", k)
			}
		default:
			t.Fatalf("beklenmeyen ek alan: %+v", k)
		}
	}

	php, err := PhpKurulumlariBul(t.Context(), db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(php) != 2 {
		t.Fatalf("php: ana (home/composer.lock) + ek (alan dizini) = 2: %+v", php)
	}
	for _, k := range php {
		if k.EkAlanID == 0 && (k.Paketler[0].Ad != "ana/p" || !strings.HasSuffix(k.Yol, "/on")) {
			t.Fatalf("ana php: %+v", k)
		}
		if k.EkAlanID == 1 && (k.Paketler[0].Ad != "ek/p" || !strings.HasSuffix(k.Yol, "/on/ek-alanlar/ek1.test")) {
			t.Fatalf("ek php: %+v", k)
		}
	}
	// Ek alanın kendi composer.lock'ı yoksa ana hesabınkini DEVRALMAZ.
	if err := os.Remove(filepath.Join(ev, "on", "ek-alanlar", "ek1.test", "composer.lock")); err != nil {
		t.Fatal(err)
	}
	php, _ = PhpKurulumlariBul(t.Context(), db, nil)
	if len(php) != 1 || php[0].EkAlanID != 0 {
		t.Fatalf("ek alan ana hesabın composer.lock'ını sayamaz: %+v", php)
	}
}

// Docroot içindeki dosya sembolik bağ ise (hesap dışındaki dosyayı okutma) okunmaz.
func TestEk_DosyaSembolikBagiOkunmaz(t *testing.T) {
	db, ev := ekOrtam(t)
	dis := t.TempDir()
	ekYaz(t, filepath.Join(dis, "sir.json"), `{"dependencies":{"sizinti":"9.9.9"}}`)
	dr := filepath.Join(ev, "on", "ek-alanlar", "ek1.test", "public_html")
	if err := os.Symlink(filepath.Join(dis, "sir.json"), filepath.Join(dr, "package.json")); err != nil {
		t.Fatal(err)
	}
	node, err := NodejsKurulumlariBul(t.Context(), db, []int64{-1})
	if err != nil || len(node) != 0 {
		t.Fatalf("bağ olan package.json okunmamalı: %v %+v", err, node)
	}
	// Aşırı büyük dosya reddedilir.
	if _, err := kokDosyaOku(dr, "yok.json"); err == nil {
		t.Fatal("olmayan dosya hata vermeli")
	}
	if _, err := kokDosyaOku(dr, "../x"); err == nil {
		t.Fatal("yol ayırıcılı ad reddedilmeli")
	}
}

// Bulgular ek alana bağlanır; ana satır ve ek satır AYRI görünür.
func ekBulguTohumla(t *testing.T, db *sql.DB) {
	t.Helper()
	z := []WPVulnZafiyet{{CVE: "CVE-2026-100", Title: "t", Severity: "high", MaxVersion: "9.9", Source: "t"}}
	ana := &WPKurulum{DomainID: 10, Yol: "/x/on/public_html"}
	ek := &WPKurulum{DomainID: 10, EkAlanID: 1, Yol: "/x/on/ek-alanlar/ek1.test/public_html"}
	ek2 := &WPKurulum{DomainID: 11, EkAlanID: 2, Yol: "/x/onbir/ek-alanlar/ek2.test/public_html"}
	for _, k := range []*WPKurulum{ana, ek, ek2} {
		if n := paketKarsilastirveKaydet(t.Context(), db, k, "plugin", WPPaket{Slug: "p", Surum: "1.0"}, z); n != 1 {
			t.Fatalf("bulgu yazılamadı: %d", n)
		}
		envanterYaz(t.Context(), db, k.DomainID, k.EkAlanID, "wordpress", k.Yol, "6.5", 1, 1)
	}
}

func TestDB_EkAlanBulguVeEnvanterAyriSatir(t *testing.T) {
	db, _ := ekOrtam(t)
	ekBulguTohumla(t, db)
	kuyrukKur(t)
	kimlikKur(t, kokJeton)
	h := &Handler{DB: db}

	rr := istek(t, h.Apps, "GET", "/websec/apps", "")
	var a envanterYanit
	if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &a) != nil {
		t.Fatalf("apps: %d %s", rr.Code, rr.Body)
	}
	satir := map[string]envanterSatir{}
	for _, it := range a.Items {
		satir[it.AlanAdi] = it
	}
	for _, ad := range []string{"on.test", "ek1.test", "ek2.test", "ek3.test", "onbir.test", "oniki.test"} {
		if _, ok := satir[ad]; !ok {
			t.Fatalf("%s satırı yok (ana + ek alanlar ayrı satır): %v", ad, a.Items)
		}
	}
	if s := satir["ek1.test"]; s.EkAlanID != 1 || s.DomainID != 10 || s.AppType != "wordpress" || s.Durum != "acik" || s.BulguSayisi != 1 {
		t.Fatalf("ek1 satırı: %+v", s)
	}
	// Ana domain satırı YALNIZ ana uygulamayı taşır (ek alanın uygulaması ana satıra sızmaz).
	var anaSatirSayisi int
	for _, it := range a.Items {
		if it.EkAlanID == 0 && it.DomainID == 10 {
			anaSatirSayisi++
			if !strings.HasSuffix(it.InstallPath, "/on/public_html") {
				t.Fatalf("ana satır ek alanın uygulamasını taşıyor: %+v", it)
			}
		}
	}
	if anaSatirSayisi != 1 {
		t.Fatalf("ana domain tek satır olmalı (çift sayım): %d", anaSatirSayisi)
	}
	if s := satir["ek3.test"]; s.EkAlanID != 3 || s.Durum != "beklemede" || s.AppType != "" {
		t.Fatalf("taranmamış ek alan beklemede: %+v", s)
	}

	// Bulgular: ek_alan_id süzgeci + alan adı ek alanın kendi adı.
	bul := func(sorgu string) bulguYanit {
		rr := istek(t, h.Findings, "GET", "/websec/findings"+sorgu, "")
		var b bulguYanit
		if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &b) != nil {
			t.Fatalf("findings%s: %d %s", sorgu, rr.Code, rr.Body)
		}
		return b
	}
	if b := bul("?domain_id=10&ek_alan_id=1"); b.Toplam != 1 || b.Items[0].AlanAdi != "ek1.test" || b.Items[0].EkAlanID != 1 {
		t.Fatalf("ek1 bulgusu ek alan adıyla görünmeli: %+v", b)
	}
	if b := bul("?domain_id=10&ek_alan_id=0"); b.Toplam != 1 || b.Items[0].AlanAdi != "on.test" || b.Items[0].EkAlanID != 0 {
		t.Fatalf("ana domain bulgusu yalnız ana: %+v", b)
	}
	if b := bul("?domain_id=10"); b.Toplam != 2 {
		t.Fatalf("süzgeçsiz (eski istemci) hesabın tümü: %+v", b)
	}
	if b := bul("?q=ek2.test"); b.Toplam != 1 || b.Items[0].EkAlanID != 2 {
		t.Fatalf("arama ek alan adını bulmalı: %+v", b)
	}
	if b := bul("?ek_alan_id=abc"); b.Toplam != 0 {
		t.Fatalf("geçersiz ek_alan_id boş döner: %+v", b)
	}
}

// IDOR: bayi yalnız kendi hesabının ek alanlarını görür/tarar; yabancı ek alan = olmayan.
func TestDB_EkAlanBayiIDOR(t *testing.T) {
	db, _ := ekOrtam(t)
	ekBulguTohumla(t, db)
	k := kuyrukKur(t)
	saatKur(t)
	h := &Handler{DB: db}

	kimlikKur(t, bayiJeton) // bayi 7: domain 10 + ek 1,3
	rr := istek(t, h.Apps, "GET", "/websec/apps", "")
	var a envanterYanit
	_ = json.Unmarshal(rr.Body.Bytes(), &a)
	var gorulen []string
	for _, it := range a.Items {
		gorulen = append(gorulen, it.AlanAdi)
	}
	sort.Strings(gorulen)
	if !reflect.DeepEqual(gorulen, []string{"ek1.test", "ek3.test", "on.test"}) {
		t.Fatalf("bayi 7 yalnız kendi hesabının satırlarını görmeli: %v", gorulen)
	}
	rr = istek(t, h.Findings, "GET", "/websec/findings?ek_alan_id=2", "")
	var b bulguYanit
	_ = json.Unmarshal(rr.Body.Bytes(), &b)
	if b.Toplam != 0 || len(b.Items) != 0 {
		t.Fatalf("yabancı ek alanın bulgusu görünmemeli: %s", rr.Body)
	}
	rr = istek(t, h.Findings, "GET", "/websec/findings?q=ek2.test", "")
	_ = json.Unmarshal(rr.Body.Bytes(), &b)
	if b.Toplam != 0 {
		t.Fatalf("arama yabancı ek alanı bulmamalı: %s", rr.Body)
	}
	rr = istek(t, h.Status, "GET", "/websec/status", "")
	var d map[string]json.RawMessage
	_ = json.Unmarshal(rr.Body.Bytes(), &d)
	if string(d["total_findings"]) != "2" || string(d["high"]) != "2" {
		t.Fatalf("bayi 7 durum sayacı yalnız kendi 2 bulgusu: %s", rr.Body)
	}

	yabanci := istek(t, h.Rescan, "POST", "/websec/rescan?ek_alan_id=2", "")
	olmayan := istek(t, h.Rescan, "POST", "/websec/rescan?ek_alan_id=999", "")
	if yabanci.Code != 404 || yabanci.Code != olmayan.Code || yabanci.Body.String() != olmayan.Body.String() {
		t.Fatalf("yabancı ek alan (%d %s) olmayanla (%d %s) aynı 404 olmalı", yabanci.Code, yabanci.Body, olmayan.Code, olmayan.Body)
	}
	karisik := istek(t, h.RescanMany, "POST", "/websec/rescan-many", `{"domain_ids":[10],"ek_alan_ids":[1,2]}`)
	if karisik.Code != 404 {
		t.Fatalf("karışık liste (yabancı ek alan) tamamı 404: %d %s", karisik.Code, karisik.Body)
	}
	if k.tamMi() || k.bekleyenSayisi() != 0 || wsDenetimSayisi(t, db) != 0 {
		t.Fatal("reddedilen istek kuyruğa/denetime girmemeli")
	}
	// Kök her şeyi görür.
	kimlikKur(t, kokJeton)
	rr = istek(t, h.Apps, "GET", "/websec/apps", "")
	_ = json.Unmarshal(rr.Body.Bytes(), &a)
	if len(a.Items) != 6 {
		t.Fatalf("kök 3 domain + 3 ek alan görmeli: %d", len(a.Items))
	}
}

// Tara kuyruğu: ek alan satırındaki Tara YALNIZ o ek alanı kuyruğa alır (anahtar < 0);
// ana domain Tara'sı ek alanı kapsamaz; tekrar tıklama kapısı ve 15 dk soğuma ek alan
// anahtarıyla ayrı çalışır; "Domainlerimi Tara" ek alanları da kapsar.
func TestDB_EkAlanTaraKuyrukSemantigi(t *testing.T) {
	db, _ := ekOrtam(t)
	k := kuyrukKur(t)
	simdi := saatKur(t)
	kimlikKur(t, bayiJeton)
	h := &Handler{DB: db}

	ilk := istek(t, h.Rescan, "POST", "/websec/rescan?ek_alan_id=1", "")
	if ilk.Code != 202 || strings.TrimSpace(ilk.Body.String()) != `{"sirada":1}` {
		t.Fatalf("ek alan Tara: %d %s", ilk.Code, ilk.Body)
	}
	ikinci := istek(t, h.Rescan, "POST", "/websec/rescan?ek_alan_id=1", "")
	if ikinci.Code != 202 || !strings.Contains(ikinci.Body.String(), `"zaten":true`) {
		t.Fatalf("tekrar tıklama zaten dönmeli: %d %s", ikinci.Code, ikinci.Body)
	}
	if wsDenetimSayisi(t, db) != 1 || k.bekleyenSayisi() != 1 {
		t.Fatalf("denetim=%d kuyruk=%d, ikisi de 1 olmalı", wsDenetimSayisi(t, db), k.bekleyenSayisi())
	}
	var hedef string
	var kapsamID int64
	if err := db.QueryRow(`SELECT target, reseller_id FROM audit_log`).Scan(&hedef, &kapsamID); err != nil || hedef != "ek1.test" || kapsamID != 7 {
		t.Fatalf("denetim hedefi ek alan adı olmalı: %v %q %d", err, hedef, kapsamID)
	}
	if is, ok := k.sonrakiIs(); !ok || is.tam || !reflect.DeepEqual(is.ids, []int64{-1}) {
		t.Fatalf("kuyruk girdisi ek alan anahtarını taşımalı: %+v", is)
	}
	// Soğuma: işlenmiş olsa da 15 dk içinde aynı ek alan 429; ana domain AYRI anahtar.
	r429 := istek(t, h.Rescan, "POST", "/websec/rescan?ek_alan_id=1", "")
	if r429.Code != 429 || !strings.Contains(r429.Body.String(), "dk sonra tekrar deneyin") {
		t.Fatalf("ek alan soğuması 429: %d %s", r429.Code, r429.Body)
	}
	ana := istek(t, h.Rescan, "POST", "/websec/rescan?domain_id=10", "")
	if ana.Code != 202 || strings.TrimSpace(ana.Body.String()) != `{"sirada":1}` {
		t.Fatalf("ana domain Tara'sı ek alan soğumasından etkilenmemeli: %d %s", ana.Code, ana.Body)
	}
	if is, ok := k.sonrakiIs(); !ok || !reflect.DeepEqual(is.ids, []int64{10}) {
		t.Fatalf("ana domain Tara'sı yalnız ana docroot: %+v", is)
	}
	*simdi = simdi.Add(16 * time.Minute)
	if r := istek(t, h.Rescan, "POST", "/websec/rescan?ek_alan_id=1", ""); r.Code != 202 {
		t.Fatalf("15 dk sonra yeniden kabul edilmeli: %d %s", r.Code, r.Body)
	}
	k.sonrakiIs()

	// "Domainlerimi Tara": bayinin domaini + ek alanları (yabancı ek 2 YOK).
	*simdi = simdi.Add(time.Hour)
	tum := istek(t, h.Rescan, "POST", "/websec/rescan", "")
	if tum.Code != 202 || strings.TrimSpace(tum.Body.String()) != `{"sirada":3}` {
		t.Fatalf("Domainlerimi Tara: %d %s", tum.Code, tum.Body)
	}
	is, _ := k.sonrakiIs()
	sort.Slice(is.ids, func(i, j int) bool { return is.ids[i] < is.ids[j] })
	if !reflect.DeepEqual(is.ids, []int64{-3, -1, 10}) {
		t.Fatalf("bayi 7: domain 10 + ek 1,3: %v", is.ids)
	}

	// Kök: ek alan Tara'sı ve toplu istek.
	kimlikKur(t, kokJeton)
	kk := istek(t, h.Rescan, "POST", "/websec/rescan?ek_alan_id=2", "")
	if kk.Code != 202 || !strings.Contains(kk.Body.String(), `"ek_alan_id":2`) {
		t.Fatalf("kök ek alan Tara: %d %s", kk.Code, kk.Body)
	}
	if kk2 := istek(t, h.Rescan, "POST", "/websec/rescan?ek_alan_id=2", ""); !strings.Contains(kk2.Body.String(), `"zaten":true`) {
		t.Fatalf("kök tekrar tıklama zaten: %s", kk2.Body)
	}
	istek(t, h.RescanMany, "POST", "/websec/rescan-many", `{"domain_ids":[12],"ek_alan_ids":[1]}`)
	is, _ = k.sonrakiIs()
	sort.Slice(is.ids, func(i, j int) bool { return is.ids[i] < is.ids[j] })
	if !reflect.DeepEqual(is.ids, []int64{-2, -1, 12}) {
		t.Fatalf("kök kuyruk: %v", is.ids)
	}
}

// Yoklama anahtarı: durum alanı ek alan satırına AYRI uygulanır (taranıyor/sırada).
func TestDB_EkAlanDurumuKuyrukVeTaranan(t *testing.T) {
	db, _ := ekOrtam(t)
	ekBulguTohumla(t, db)
	k := kuyrukKur(t)
	kimlikKur(t, kokJeton)
	k.kokEkle([]int64{-1})
	h := &Handler{DB: db}
	durum := func() map[string]string {
		rr := istek(t, h.Apps, "GET", "/websec/apps", "")
		var a envanterYanit
		_ = json.Unmarshal(rr.Body.Bytes(), &a)
		m := map[string]string{}
		for _, it := range a.Items {
			m[it.AlanAdi] = it.Durum
		}
		return m
	}
	if m := durum(); m["ek1.test"] != "sirada" || m["on.test"] == "sirada" {
		t.Fatalf("yalnız ek1 sırada olmalı: %v", m)
	}
	k.sonrakiIs()
	kosmaMu.Lock()
	kosuyor, taranEden = true, map[int64]bool{-1: true}
	kosmaMu.Unlock()
	t.Cleanup(func() {
		kosmaMu.Lock()
		kosuyor, taranEden = false, map[int64]bool{}
		kosmaMu.Unlock()
	})
	if m := durum(); m["ek1.test"] != "taraniyor" || m["on.test"] == "taraniyor" || m["ek2.test"] == "taraniyor" {
		t.Fatalf("yalnız ek1 taranıyor olmalı: %v", m)
	}
}

// Kısmi tarama damgası + solma kapsamı: ana anahtar ek alana, ek anahtar ana domaine
// dokunmaz; tam taramada ikisi de damgalanır.
func TestDB_EkAlanDamgaVeSolmaKapsami(t *testing.T) {
	db, _ := ekOrtam(t)
	if err := TaraDomains(t.Context(), db, []int64{-1}); err != nil {
		t.Fatal(err)
	}
	var ek, dom int
	_ = db.QueryRow(`SELECT COUNT(*) FROM cp_websec_ek_tarama WHERE ek_alan_id=1`).Scan(&ek)
	_ = db.QueryRow(`SELECT COUNT(*) FROM cp_websec_domain_tarama`).Scan(&dom)
	var toplamEk int
	_ = db.QueryRow(`SELECT COUNT(*) FROM cp_websec_ek_tarama`).Scan(&toplamEk)
	if ek != 1 || toplamEk != 1 || dom != 0 {
		t.Fatalf("ek alan taraması yalnız o ek alanı damgalamalı: ek=%d toplamEk=%d dom=%d", ek, toplamEk, dom)
	}
	if err := TaraDomains(t.Context(), db, []int64{11}); err != nil {
		t.Fatal(err)
	}
	_ = db.QueryRow(`SELECT COUNT(*) FROM cp_websec_ek_tarama`).Scan(&toplamEk)
	_ = db.QueryRow(`SELECT COUNT(*) FROM cp_websec_domain_tarama WHERE domain_id=11`).Scan(&dom)
	if toplamEk != 1 || dom != 1 {
		t.Fatalf("ana domain taraması ek alanı damgalamamalı: toplamEk=%d dom=%d", toplamEk, dom)
	}
	if err := TaraDomains(t.Context(), db, nil); err != nil {
		t.Fatal(err)
	}
	_ = db.QueryRow(`SELECT COUNT(*) FROM cp_websec_ek_tarama`).Scan(&toplamEk)
	if toplamEk != 3 {
		t.Fatalf("tam tarama tüm ek alanları damgalar: %d", toplamEk)
	}
	// Envanter: damgalı ek alan "desteklenmiyor", sayaç ek alanı da sayar.
	kuyrukKur(t)
	kimlikKur(t, kokJeton)
	rr := istek(t, (&Handler{DB: db}).Apps, "GET", "/websec/apps", "")
	var a envanterYanit
	_ = json.Unmarshal(rr.Body.Bytes(), &a)
	if a.Toplam != 6 || a.Desteklenmeyen != 6 {
		t.Fatalf("6 satır (3 domain + 3 ek) ve hepsi uygulamasız: toplam=%d desteklenmeyen=%d", a.Toplam, a.Desteklenmeyen)
	}

	// Solma: ana anahtar yalnız ana satırı, ek anahtar yalnız o ek satırı siler.
	wsExec(t, db, `INSERT INTO cp_websec_apps (domain_id, ek_alan_id, app_type, install_path, son_tarama) VALUES
		(10, NULL, 'wordpress', '/a', '2020-01-01 00:00:00'),
		(10, 1, 'wordpress', '/b', '2020-01-01 00:00:00')`)
	say := func() int {
		var n int
		_ = db.QueryRow(`SELECT COUNT(*) FROM cp_websec_apps`).Scan(&n)
		return n
	}
	envanterSolmaTemizle(t.Context(), db, "2021-01-01 00:00:00", []string{"wordpress"}, []int64{10})
	if say() != 1 {
		t.Fatalf("ana anahtar yalnız ana satırı silmeli: %d", say())
	}
	envanterSolmaTemizle(t.Context(), db, "2021-01-01 00:00:00", []string{"wordpress"}, []int64{-1})
	if say() != 0 {
		t.Fatalf("ek anahtar o ek satırı silmeli: %d", say())
	}
}

func TestKapsamSQL(t *testing.T) {
	if q, a := kapsamSQL(nil); q != "" || a != nil {
		t.Fatalf("tam tarama süzgeçsiz: %q %v", q, a)
	}
	q, a := kapsamSQL([]int64{10, -3, 10, 0})
	if q != " AND ((domain_id IN (?) AND ek_alan_id IS NULL) OR ek_alan_id IN (?))" || !reflect.DeepEqual(a, []any{int64(10), int64(3)}) {
		t.Fatalf("kapsamSQL: %q %v", q, a)
	}
	if q, _ := kapsamSQL([]int64{0}); q != " AND 1=0" {
		t.Fatalf("yalnız geçersiz anahtar hiçbir şey kapsamamalı: %q", q)
	}
}

// Göç 0180: idempotent (iki kez uygulanır), ek alan silinince satırlar CASCADE gider,
// yorum kuralı (satır içi yorum/noktalı virgül yok).
func TestDB_Goc0180IdempotentVeCascade(t *testing.T) {
	db, _ := ekOrtam(t)
	wsGocUygula(t, db, "../../migrations/0180_websec_ek_alan.sql")
	wsGocUygula(t, db, "../../migrations/0180_websec_ek_alan.sql")
	ekBulguTohumla(t, db)
	wsExec(t, db, `INSERT INTO cp_websec_ek_tarama (ek_alan_id) VALUES (1)`)
	wsExec(t, db, `DELETE FROM ek_alan_adlari WHERE id=1`)
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM cp_websec_apps WHERE ek_alan_id=1`).Scan(&n)
	var f, tar int
	_ = db.QueryRow(`SELECT COUNT(*) FROM cp_websec_findings WHERE ek_alan_id=1`).Scan(&f)
	_ = db.QueryRow(`SELECT COUNT(*) FROM cp_websec_ek_tarama`).Scan(&tar)
	if n != 0 || f != 0 || tar != 0 {
		t.Fatalf("ek alan silinince satırlar CASCADE gitmeli: apps=%d findings=%d tarama=%d", n, f, tar)
	}
	var ana int
	_ = db.QueryRow(`SELECT COUNT(*) FROM cp_websec_apps WHERE ek_alan_id IS NULL`).Scan(&ana)
	if ana != 1 {
		t.Fatalf("ana satır etkilenmemeli: %d", ana)
	}
}

func TestGoc0180YorumKurali(t *testing.T) {
	body, err := os.ReadFile("../../migrations/0180_websec_ek_alan.sql")
	if err != nil {
		t.Fatal(err)
	}
	for i, satir := range strings.Split(string(body), "\n") {
		s := strings.TrimSpace(satir)
		if strings.HasPrefix(s, "--") {
			continue
		}
		if strings.Contains(s, "--") {
			t.Errorf("satır %d: satır içi yorum: %q", i+1, s)
		}
	}
	var kod []string
	for _, satir := range strings.Split(string(body), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(satir), "--") {
			kod = append(kod, satir)
		}
	}
	if strings.Contains(strings.ReplaceAll(strings.Join(kod, "\n"), ";\n", ""), ";") {
		t.Error("ifadeler dışında noktalı virgül var")
	}
}

// Bayi durum kartı: son tarama ve "taranıyor" YALNIZ kendi ek alanlarından gelir.
func TestDB_EkAlanBayiDurumKartiKapsami(t *testing.T) {
	db, _ := ekOrtam(t)
	wsExec(t, db, `INSERT INTO cp_websec_ek_tarama (ek_alan_id, son_tarama) VALUES
		(1, '2026-09-29 10:00:00'), (2, '2026-09-30 23:00:00')`)
	k := kuyrukKur(t)
	h := &Handler{DB: db}
	durum := func(c *auth.Claims) map[string]json.RawMessage {
		kimlikKur(t, c)
		rr := istek(t, h.Status, "GET", "/websec/status", "")
		var d map[string]json.RawMessage
		if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &d) != nil {
			t.Fatalf("status: %d %s", rr.Code, rr.Body)
		}
		return d
	}
	d := durum(bayiJeton)
	if string(d["last_run"]) != `"2026-09-29 10:00:00"` || string(d["running"]) != "false" {
		t.Fatalf("bayi 7 yalnız kendi ek alanının zamanını görmeli: %v", d)
	}
	k.kokEkle([]int64{-2}) // başka bayinin ek alanı sırada
	if d = durum(bayiJeton); string(d["running"]) != "false" {
		t.Fatalf("başkasının ek alanı sıradayken bayi 7 running görmemeli: %v", d)
	}
	k.kokEkle([]int64{-1})
	if d = durum(bayiJeton); string(d["running"]) != "true" {
		t.Fatalf("kendi ek alanı sıradayken running: %v", d)
	}
}
