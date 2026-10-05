package websec

// kuyruk_test.go — tarama kuyruğu, bayi sınırları ve tarama uçları (sahte DB).
// Gerçek tarama HİÇ koşmaz: işçinin tarama çağrısı (taraFn) sahtelenir; exec/dış
// feed yok.

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"girginospanel/internal/auth"
	"girginospanel/internal/sahtedb"
)

// kuyrukKur — test süresince taze bir kuyruk (süreç geneli kuy yerine).
func kuyrukKur(t *testing.T) *kuyruk {
	t.Helper()
	eski := kuy
	kuy = yeniKuyruk()
	t.Cleanup(func() { kuy = eski })
	return kuy
}

// saatKur — sınır testleri için elle ilerletilen saat.
func saatKur(t *testing.T) *time.Time {
	t.Helper()
	simdi := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	eski := saat
	saat = func() time.Time { return simdi }
	t.Cleanup(func() { saat = eski })
	return &simdi
}

// taraKaydet — taraFn'i sahteler; her çağrının domain listesini kaydeder.
type taraKaydi struct {
	mu       sync.Mutex
	cagrilar [][]int64
	nilMi    []bool
}

func taraSahte(t *testing.T) *taraKaydi {
	t.Helper()
	kay := &taraKaydi{}
	eski := taraFn
	taraFn = func(_ context.Context, _ *sql.DB, ids []int64) error {
		kay.mu.Lock()
		defer kay.mu.Unlock()
		kay.cagrilar = append(kay.cagrilar, append([]int64(nil), ids...))
		kay.nilMi = append(kay.nilMi, ids == nil)
		return nil
	}
	t.Cleanup(func() { taraFn = eski })
	return kay
}

func (k *kuyruk) bekleyenSayisi() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.bekleyen)
}

func (k *kuyruk) tamMi() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.tam
}

// Kök regresyonu: domain_id'siz rescan = TAM tarama; domain_id'li = yalnız o domain;
// geçersiz domain_id eskisi gibi tam tarama. Yanıt biçimi aynı; denetim/kapsam yok.
func TestRescanKokDavranisiDegismedi(t *testing.T) {
	k := kuyrukKur(t)
	kimlikKur(t, kokJeton)
	sd, db := sahtedb.Yeni()
	h := &Handler{DB: db}

	for _, c := range []struct {
		yol string
		tam bool
		ids []int64
	}{
		{"/websec/rescan", true, nil},
		{"/websec/rescan?domain_id=12", false, []int64{12}},
		{"/websec/rescan?domain_id=abc", true, nil},
	} {
		rr := istek(t, h.Rescan, "POST", c.yol, "")
		m := jsonAnahtarlar(t, rr.Body.Bytes())
		if rr.Code != 202 || string(m["started"]) != "true" || m["at"] == nil || m["domain_id"] == nil || m["sirada"] != nil {
			t.Fatalf("%s: kök yanıtı değişmemeli: %d %s", c.yol, rr.Code, rr.Body)
		}
		is, ok := k.sonrakiIs()
		if !ok || is.tam != c.tam || !reflect.DeepEqual(is.ids, c.ids) {
			t.Fatalf("%s: iş %+v ok=%v, beklenen tam=%v ids=%v", c.yol, is, ok, c.tam, c.ids)
		}
	}
	rr := istek(t, h.RescanMany, "POST", "/websec/rescan-many", `{"domain_ids":[3,4,3,0]}`)
	if rr.Code != 202 || !strings.Contains(rr.Body.String(), `"count":2`) || !strings.Contains(rr.Body.String(), `"started":true`) {
		t.Fatalf("kök rescan-many: %d %s", rr.Code, rr.Body)
	}
	if is, ok := k.sonrakiIs(); !ok || is.tam || !reflect.DeepEqual(is.ids, []int64{3, 4}) {
		t.Fatalf("kök rescan-many işi: %+v %v", is, ok)
	}
	if sd.Sorgulandi("reseller_id") || sd.Yurutuldu("audit_log") {
		t.Fatal("kökte kapsam sorgusu/denetim olmamalı (davranış değişmedi)")
	}
}

// 🔴 Domaini olmayan bayinin domain_id'siz isteği ASLA tam tarama (nil = tüm sunucu)
// üretmez: 202 {"sirada":0}, kuyruk boş, tam bayrağı yok, denetim yok.
func TestRescanDomainsizBayiNilListeUretmez(t *testing.T) {
	for _, c := range []struct {
		ad    string
		jeton *auth.Claims
		kosul string
	}{
		{"domainsiz bayi", bayiJeton, "WHERE 1=1 AND d.reseller_id=?"},
		{"kimliksiz bayi", &auth.Claims{UserID: 7, Role: "reseller"}, "WHERE 1=1 AND 1=0"},
	} {
		k := kuyrukKur(t)
		kimlikKur(t, c.jeton)
		sd, db := sahtedb.Yeni()
		rr := istek(t, (&Handler{DB: db}).Rescan, "POST", "/websec/rescan", "")
		if rr.Code != 202 || strings.TrimSpace(rr.Body.String()) != `{"sirada":0}` {
			t.Fatalf("%s: 202 {\"sirada\":0} beklenirdi: %d %s", c.ad, rr.Code, rr.Body)
		}
		if k.tamMi() || k.bekleyenSayisi() != 0 {
			t.Fatalf("%s: kuyruk boş kalmalı (tam=%v bekleyen=%d)", c.ad, k.tamMi(), k.bekleyenSayisi())
		}
		if _, ok := k.sonrakiIs(); ok {
			t.Fatalf("%s: işçiye iş gitmemeli", c.ad)
		}
		if !sd.Sorgulandi("SELECT d.id, DATE_FORMAT(t.son_tarama") || !sd.Sorgulandi(c.kosul) {
			t.Fatalf("%s: domain listesi sunucuda kapsamla çıkarılmalı (%s)", c.ad, c.kosul)
		}
		if sd.Yurutuldu("audit_log") {
			t.Fatalf("%s: hiçbir şey kuyruğa girmediyse denetim yazılmamalı", c.ad)
		}
	}
}

// Bayinin "Domainlerimi Tara"sı: yalnız kendi domainleri kısmi iş olarak; denetim.
func TestRescanBayiKendiDomainleri(t *testing.T) {
	k := kuyrukKur(t)
	saatKur(t)
	kimlikKur(t, bayiJeton)
	sd, db := sahtedb.Yeni()
	sd.Satirlar("SELECT d.id, DATE_FORMAT(t.son_tarama", []any{int64(10), nil}, []any{int64(11), nil})
	rr := istek(t, (&Handler{DB: db}).Rescan, "POST", "/websec/rescan", "")
	if rr.Code != 202 || strings.TrimSpace(rr.Body.String()) != `{"sirada":2}` {
		t.Fatalf("202 {\"sirada\":2} beklenirdi: %d %s", rr.Code, rr.Body)
	}
	if is, ok := k.sonrakiIs(); !ok || is.tam || !reflect.DeepEqual(is.ids, []int64{10, 11}) {
		t.Fatalf("yalnız kendi domainleri kısmi iş olmalı: %+v", is)
	}
	if !sd.Yurutuldu("websec.tara") {
		t.Fatal("bayinin tarama tetiklemesi denetime yazılmalı")
	}
}

// Yabancı/olmayan/geçersiz domain_id: aynı 404 gövdesi, kuyruk boş, denetim yok.
func TestRescanBayiYabanciDomain404(t *testing.T) {
	k := kuyrukKur(t)
	kimlikKur(t, bayiJeton)
	sd, db := sahtedb.Yeni()
	h := &Handler{DB: db}
	for _, q := range []string{"11", "999", "abc", "-3"} {
		rr := istek(t, h.Rescan, "POST", "/websec/rescan?domain_id="+q, "")
		if rr.Code != 404 || strings.TrimSpace(rr.Body.String()) != `{"error":"domain bulunamadı"}` {
			t.Fatalf("domain_id=%s: 404 domain bulunamadı beklenirdi: %d %s", q, rr.Code, rr.Body)
		}
	}
	if !sd.Sorgulandi("SELECT d.alan_adi FROM domains d WHERE d.id=? AND d.reseller_id=?") {
		t.Fatal("sahiplik sorgusu kapsam süzgeci taşımalı")
	}
	if k.tamMi() || k.bekleyenSayisi() != 0 || sd.Yurutuldu("audit_log") {
		t.Fatal("yabancı domain kuyruğa/denetime girmemeli")
	}
}

// RescanMany (bayi): listede tek yabancı/olmayan id → TAMAMI 404, hiçbiri kuyruğa girmez.
func TestRescanManyBayiKarisikListe404(t *testing.T) {
	k := kuyrukKur(t)
	saatKur(t)
	kimlikKur(t, bayiJeton)
	sd, db := sahtedb.Yeni()
	sd.Satirlar("SELECT COUNT(*) FROM domains d WHERE d.id IN", []any{int64(1)})
	rr := istek(t, (&Handler{DB: db}).RescanMany, "POST", "/websec/rescan-many", `{"domain_ids":[10,11]}`)
	if rr.Code != 404 || strings.TrimSpace(rr.Body.String()) != `{"error":"domain bulunamadı"}` {
		t.Fatalf("karışık liste 404 olmalı: %d %s", rr.Code, rr.Body)
	}
	if !sd.Sorgulandi("WHERE d.id IN (?,?) AND d.reseller_id=?") {
		t.Fatal("sahiplik sayımı kapsam süzgeci taşımalı")
	}
	if k.tamMi() || k.bekleyenSayisi() != 0 {
		t.Fatal("hiçbiri kuyruğa girmemeli")
	}

	// Tek elemanlı liste: sahiplik denetimi liste boyundan bağımsız — tek yabancı id
	// (11) olmayan id (999) ile bayt bayt aynı 404'ü alır, kuyruğa/denetime girmez.
	sd1, db1 := sahtedb.Yeni()
	sd1.Satirlar("SELECT COUNT(*) FROM domains d WHERE d.id IN", []any{int64(0)})
	h1 := &Handler{DB: db1}
	yabanci := istek(t, h1.RescanMany, "POST", "/websec/rescan-many", `{"domain_ids":[11]}`)
	olmayan := istek(t, h1.RescanMany, "POST", "/websec/rescan-many", `{"domain_ids":[999]}`)
	if yabanci.Code != 404 || strings.TrimSpace(yabanci.Body.String()) != `{"error":"domain bulunamadı"}` ||
		yabanci.Code != olmayan.Code || yabanci.Body.String() != olmayan.Body.String() {
		t.Fatalf("tek yabancı id (%d %s) olmayanla (%d %s) aynı 404 olmalı",
			yabanci.Code, yabanci.Body, olmayan.Code, olmayan.Body)
	}
	if !sd1.Sorgulandi("WHERE d.id IN (?) AND d.reseller_id=?") {
		t.Fatal("tek id'de de sahiplik sayımı kapsam süzgeci taşımalı")
	}
	if k.tamMi() || k.bekleyenSayisi() != 0 || sd1.Yurutuldu("audit_log") {
		t.Fatal("tek yabancı id kuyruğa/denetime girmemeli")
	}

	sd2, db2 := sahtedb.Yeni()
	sd2.Satirlar("SELECT COUNT(*) FROM domains d WHERE d.id IN", []any{int64(2)})
	rr = istek(t, (&Handler{DB: db2}).RescanMany, "POST", "/websec/rescan-many", `{"domain_ids":[10,11]}`)
	if rr.Code != 202 || strings.TrimSpace(rr.Body.String()) != `{"sirada":2}` || k.bekleyenSayisi() != 2 {
		t.Fatalf("hepsi kendi domainiyse 202 {\"sirada\":2}: %d %s", rr.Code, rr.Body)
	}
}

// Soğuma: aynı domain 15 dk içinde yeniden istenirse 429 + "N dk sonra tekrar deneyin".
// Sıradayken yeniden istemek 202 (zaten sırada).
func TestBayiDomainSogumasi429(t *testing.T) {
	k := kuyrukKur(t)
	simdi := saatKur(t)
	kimlikKur(t, bayiJeton)
	sd, db := sahtedb.Yeni()
	sd.Satirlar("SELECT d.alan_adi FROM domains d WHERE d.id=?", []any{"on.test"})
	h := &Handler{DB: db}
	tara := func() (int, string) {
		rr := istek(t, h.Rescan, "POST", "/websec/rescan?domain_id=10", "")
		return rr.Code, strings.TrimSpace(rr.Body.String())
	}
	if kod, g := tara(); kod != 202 || g != `{"sirada":1}` {
		t.Fatalf("ilk istek: %d %s", kod, g)
	}
	if kod, g := tara(); kod != 202 || g != `{"sirada":1,"zaten":true}` {
		t.Fatalf("sıradayken tekrar: %d %s", kod, g)
	}
	k.sonrakiIs() // işçi aldı
	if kod, g := tara(); kod != 429 || g != `{"error":"15 dk sonra tekrar deneyin"}` {
		t.Fatalf("soğumada 429 beklenirdi: %d %s", kod, g)
	}
	*simdi = simdi.Add(10*time.Minute + 30*time.Second)
	if kod, g := tara(); kod != 429 || g != `{"error":"5 dk sonra tekrar deneyin"}` {
		t.Fatalf("kalan süre yukarı yuvarlanmalı: %d %s", kod, g)
	}
	*simdi = simdi.Add(5 * time.Minute)
	if kod, g := tara(); kod != 202 || g != `{"sirada":1}` {
		t.Fatalf("soğuma bitince 202: %d %s", kod, g)
	}
}

// Soğuma isteyen bayiye aittir: devirden sonra yeni sahip, eski sahibin 15 dk
// içindeki isteği yüzünden 429 almaz (ve kalan süreden onu öğrenemez).
func TestBayiSogumasiDevirdeYeniSahibeGecmez(t *testing.T) {
	k := kuyrukKur(t)
	simdi := saatKur(t)
	if n, bekle := k.bayiEkle(7, []int64{10}, false); n != 1 || bekle != 0 {
		t.Fatalf("eski sahip: %d %v", n, bekle)
	}
	k.sonrakiIs()
	*simdi = simdi.Add(3 * time.Minute)
	if n, bekle := k.bayiEkle(8, []int64{10}, false); n != 1 || bekle != 0 {
		t.Fatalf("yeni sahip eski sahibin soğumasını görmemeli: sirada=%d bekle=%v", n, bekle)
	}
	k.sonrakiIs()
	if n, bekle := k.bayiEkle(8, []int64{10}, false); n != 0 || bekle != domainSoguma {
		t.Fatalf("yeni sahibin kendi soğuması: sirada=%d bekle=%v", n, bekle)
	}
}

// "Domainlerimi Tara" bayi başına 30 dk'da bir. Sınır bayiler arasında AYRIDIR: bayi
// 7'nin basışı bayi 8'i engellemez, bayi 8'in basışı bayi 7'nin kalan süresini
// değiştirmez (429 süresi hep bayinin kendi basışından — başkasının tarama zamanı sızmaz).
func TestBayiTumunuTara30Dk(t *testing.T) {
	k := kuyrukKur(t)
	simdi := saatKur(t)
	kimlikKur(t, bayiJeton)
	sd, db := sahtedb.Yeni()
	sd.Satirlar("SELECT d.id, DATE_FORMAT(t.son_tarama", []any{int64(10), nil}, []any{int64(11), nil})
	h := &Handler{DB: db}
	tara := func() (int, string) {
		rr := istek(t, h.Rescan, "POST", "/websec/rescan", "")
		return rr.Code, strings.TrimSpace(rr.Body.String())
	}
	if kod, g := tara(); kod != 202 || g != `{"sirada":2}` {
		t.Fatalf("ilk: %d %s", kod, g)
	}
	k.sonrakiIs()
	*simdi = simdi.Add(5 * time.Minute)
	if n, bekle := k.bayiEkle(8, []int64{20, 21}, true); n != 2 || bekle != 0 {
		t.Fatalf("bayi 7'nin basışı bayi 8'i engellememeli: sirada=%d bekle=%v", n, bekle)
	}
	k.sonrakiIs()
	*simdi = simdi.Add(11 * time.Minute) // domain soğuması bitti, bayi aralığı bitmedi
	if kod, g := tara(); kod != 429 || g != `{"error":"14 dk sonra tekrar deneyin"}` {
		t.Fatalf("30 dk içinde 429 (bayi 8'in basışı süreyi değiştirmez): %d %s", kod, g)
	}
	if n, bekle := k.bayiEkle(8, []int64{20, 21}, true); n != 0 || bekle != 19*time.Minute {
		t.Fatalf("bayi 8'in süresi kendi basışından: sirada=%d bekle=%v", n, bekle)
	}
	*simdi = simdi.Add(14 * time.Minute)
	if kod, g := tara(); kod != 202 || g != `{"sirada":2}` {
		t.Fatalf("30 dk sonra 202: %d %s", kod, g)
	}
}

// Bayi başına kuyrukta en çok 50 domain; başka bayiyi etkilemez; kimliksiz bayi hiçbir
// şey ekleyemez.
func TestBayiKuyrukPayi(t *testing.T) {
	k := kuyrukKur(t)
	saatKur(t)
	var ids []int64
	for i := int64(1); i <= 60; i++ {
		ids = append(ids, i)
	}
	if n, bekle := k.bayiEkle(7, ids, false); n != bayiKuyrukSiniri || bekle != 0 {
		t.Fatalf("pay 50: sirada=%d bekle=%v", n, bekle)
	}
	if n, bekle := k.bayiEkle(7, []int64{61}, false); n != 0 || bekle != kuyrukDoluBekleme {
		t.Fatalf("pay doluyken 429: sirada=%d bekle=%v", n, bekle)
	}
	if n, bekle := k.bayiEkle(8, []int64{100}, false); n != 1 || bekle != 0 {
		t.Fatalf("başka bayi etkilenmemeli: sirada=%d bekle=%v", n, bekle)
	}
	if n, _ := k.bayiEkle(0, []int64{200}, false); n != 0 || k.bekleyenSayisi() != 51 {
		t.Fatalf("kimliksiz bayi ekleyememeli: sirada=%d bekleyen=%d", n, k.bekleyenSayisi())
	}
}

// Zamanlayıcının tam taraması bayi kuyruğu doluyken ve bir tarama sürerken ATLANMAZ:
// sıradaki iş odur; tam tarama bekleyen kısmi işleri de kapsar ve bekleyenleri
// BOŞALTIR — boşaltmasa bayi domainleri kalıcı "sirada" görünür, 50'lik pay dolu
// sayılır ve domainler ("zaten sırada" sayıldığından) bir daha kuyruğa giremezdi.
func TestTamTaramaBayiKuyruguYuzundenAtlanmaz(t *testing.T) {
	k := kuyrukKur(t)
	simdi := saatKur(t)
	var ids []int64
	for i := int64(1); i <= 50; i++ {
		ids = append(ids, i)
	}
	k.kokEkle([]int64{900, 901})
	if is, ok := k.sonrakiIs(); !ok || is.tam { // işçi bir parti aldı (tarama sürüyor)
		t.Fatalf("önce kısmi parti: %+v", is)
	}
	k.bayiEkle(7, ids, false)
	k.tamIste() // zamanlayıcı turu
	if is, ok := k.sonrakiIs(); !ok || !is.tam {
		t.Fatalf("tam tarama atlanmamalı, sıradaki iş olmalı: %+v %v", is, ok)
	}
	if _, ok := k.sonrakiIs(); ok {
		t.Fatal("tam tarama bekleyen kısmi işleri kapsar; ayrıca koşmamalı")
	}
	if n := k.bekleyenSayisi(); n != 0 {
		t.Fatalf("tam tarama sonrası bekleyen kalmamalı: %d", n)
	}
	*simdi = simdi.Add(domainSoguma + time.Minute)
	if n, bekle := k.bayiEkle(7, ids, false); n != bayiKuyrukSiniri || bekle != 0 {
		t.Fatalf("soğuma bitince pay yeniden 50: sirada=%d bekle=%v", n, bekle)
	}
	if is, ok := k.sonrakiIs(); !ok || is.tam || !reflect.DeepEqual(is.ids, ids) {
		t.Fatalf("domainler yeniden kuyruğa girip taranmalı: %+v %v", is, ok)
	}
}

// Partiler en çok partiBoyut domain.
func TestKuyrukPartiBoyutu(t *testing.T) {
	k := kuyrukKur(t)
	var ids []int64
	for i := int64(1); i <= partiBoyut+5; i++ {
		ids = append(ids, i)
	}
	k.kokEkle(ids)
	is, _ := k.sonrakiIs()
	is2, _ := k.sonrakiIs()
	if len(is.ids) != partiBoyut || !reflect.DeepEqual(is2.ids, []int64{201, 202, 203, 204, 205}) {
		t.Fatalf("parti: %d + %v", len(is.ids), is2.ids)
	}
	if _, ok := k.sonrakiIs(); ok {
		t.Fatal("kuyruk boşalmalı")
	}
}

// 🔴 Boş kısmi iş taramaya ASLA gitmez (TaraDomains'te boş liste = TÜM SUNUCU).
func TestIsCalistirBosKismiIsKosmaz(t *testing.T) {
	kay := taraSahte(t)
	ctx := t.Context()
	isCalistir(ctx, nil, taramaIsi{})
	isCalistir(ctx, nil, taramaIsi{ids: []int64{}})
	if len(kay.cagrilar) != 0 {
		t.Fatalf("boş kısmi iş taranmamalı: %v", kay.cagrilar)
	}
	isCalistir(ctx, nil, taramaIsi{ids: []int64{3}})
	isCalistir(ctx, nil, taramaIsi{tam: true})
	if !reflect.DeepEqual(kay.cagrilar, [][]int64{{3}, nil}) || !reflect.DeepEqual(kay.nilMi, []bool{false, true}) {
		t.Fatalf("kısmi → [3], tam → nil beklenirdi: %v %v", kay.cagrilar, kay.nilMi)
	}
}

// İşçi uçtan uca: tetiklenince kuyruğu boşaltır, sırayla tek tek koşar.
func TestIsciKuyruguIsler(t *testing.T) {
	k := kuyrukKur(t)
	cagri := make(chan string, 4)
	eski := taraFn
	taraFn = func(_ context.Context, _ *sql.DB, ids []int64) error {
		cagri <- fmt.Sprint(ids == nil, ids)
		return nil
	}
	ctx, iptal := context.WithCancel(context.Background())
	bitti := make(chan struct{})
	go func() { k.isci(ctx, nil); close(bitti) }()
	t.Cleanup(func() { iptal(); <-bitti; taraFn = eski })

	k.kokEkle([]int64{5})
	if got := bekle(t, cagri); got != "false [5]" {
		t.Fatalf("kısmi iş: %s", got)
	}
	k.tamIste()
	if got := bekle(t, cagri); got != "true []" {
		t.Fatalf("tam iş nil listeyle: %s", got)
	}
}

func bekle(t *testing.T, c <-chan string) string {
	t.Helper()
	select {
	case s := <-c:
		return s
	case <-time.After(5 * time.Second):
		t.Fatal("işçi tarama çağırmadı")
		return ""
	}
}

// Sıradaki kendi domaini: Apps durumu "sirada", bayi durum kartında running.
func TestSiradaDurumu(t *testing.T) {
	k := kuyrukKur(t)
	saatKur(t)
	kimlikKur(t, bayiJeton)
	k.bayiEkle(7, []int64{10}, false)
	sd, db := sahtedb.Yeni()
	sd.Satirlar("(t.domain_id IS NOT NULL)", []any{int64(10), "on.test", "", "", "", int64(0), int64(0), "", "", int64(0)})
	sd.Satirlar("LEFT JOIN cp_websec_domain_tarama t ON t.domain_id = d.id", []any{int64(10), ""})
	h := &Handler{DB: db}
	rr := istek(t, h.Apps, "GET", "/websec/apps", "")
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"durum":"sirada"`) {
		t.Fatalf("sıradaki domain 'sirada' görünmeli: %d %s", rr.Code, rr.Body)
	}
	rr = istek(t, h.Status, "GET", "/websec/status", "")
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"running":true`) {
		t.Fatalf("kendi domaini sıradayken running: %d %s", rr.Code, rr.Body)
	}
}
