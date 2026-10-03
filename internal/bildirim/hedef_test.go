package bildirim

// hedef_test.go — D3b yazıcı çekirdeği ve tercihler: YazBayi görünürlük matrisi,
// zaman pencereli bastırma, aktörün okunmuş kopyası, Guvenli maskesi, tercih
// süzgeci + sayaç + tercih IDOR'u, saklama silmesi.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/middleware"
)

const bh_tercihGoc = "../../migrations/0127_bildirim_tercih.sql"

// bh_kur — bt_semaHazir + 0127 (tercih) göçü; domainler 10,11 (bayi 7), 20 (bayi 8), 30 (kök).
func bh_kur(t *testing.T) *Handlers {
	t.Helper()
	db := bt_semaHazir(t)
	body, err := os.ReadFile(bh_tercihGoc)
	if err != nil {
		t.Fatalf("0127 okunamadı: %v", err)
	}
	var temiz []string
	for _, s := range strings.Split(string(body), "\n") {
		if x := strings.TrimSpace(s); x != "" && !strings.HasPrefix(x, "--") {
			temiz = append(temiz, s)
		}
	}
	for _, q := range strings.Split(strings.Join(temiz, "\n"), ";") {
		if q = strings.TrimSpace(q); q != "" {
			bt_exec(t, db, q)
		}
	}
	bt_exec(t, db, `INSERT INTO domains (id, reseller_id) VALUES (10, 7), (11, 7), (20, 8), (30, 0)`)
	return &Handlers{DB: db}
}

func bh_router(h *Handlers) http.Handler {
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(bt_sir))
		r.Get("/bildirimler", h.Liste)
		r.Get("/bildirimler/sayac", h.Sayac)
		r.Get("/bildirim-tercihleri", h.TercihGetir)
		r.Put("/bildirim-tercihleri", h.TercihKaydet)
		r.Post("/bildirimler/{id}/okundu", h.Okundu)
	})
	return r
}

func bh_istek(t *testing.T, h *Handlers, yontem, yol, tok, govde string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(yontem, yol, strings.NewReader(govde))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	bh_router(h).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func bh_sayac(t *testing.T, h *Handlers, tok string) (int, int) {
	t.Helper()
	kod, g := bh_istek(t, h, http.MethodGet, "/bildirimler/sayac", tok, "")
	if kod != http.StatusOK {
		t.Fatalf("sayaç: %d %s", kod, g)
	}
	var y struct{ Okunmamis, Kritik int }
	if err := json.Unmarshal([]byte(g), &y); err != nil {
		t.Fatal(err)
	}
	return y.Okunmamis, y.Kritik
}

// YazBayi: yalnız o bayi görür — öteki bayi, yönetici ve müşteriler görmez.
func TestYazBayi_GorunurlukMatrisi(t *testing.T) {
	h := bh_kur(t)
	id := YazBayi(h.DB, 7, Olay{Seviye: "uyari", Kategori: "kota", Baslik: "Kaynak havuzu: Disk kotası %85'i aştı", Mesaj: "m"}, 0)
	if id <= 0 {
		t.Fatal("YazBayi satır yazmalı")
	}
	gorur := map[string]bool{}
	for ad, tok := range map[string]string{
		"bayi7": bt_bayi(t, 7), "bayi8": bt_bayi(t, 8), "admin": bt_admin(t),
		"musteri10": bt_musteri(t, 10), "musteri20": bt_musteri(t, 20),
	} {
		_, gordu := bt_idler(bt_liste(t, h, tok, ""))[id]
		gorur[ad] = gordu
	}
	if !gorur["bayi7"] || gorur["bayi8"] || gorur["admin"] || gorur["musteri10"] || gorur["musteri20"] {
		t.Fatalf("YazBayi görünürlüğü yalnız bayi7 olmalı: %v", gorur)
	}
	if YazBayi(h.DB, 0, Olay{Baslik: "x"}, 0) != 0 {
		t.Error("rid=0 → yazılmamalı (yöneticiye sızmasın)")
	}
}

// Bastırma: aynı olay pencere içinde okunmuş olsa da yeniden yazılmaz; pencere
// dışında (eski kayıt) yeniden yazılır. Pencere 0 iken eski davranış (okuyunca yeni satır).
func TestYazOlay_ZamanPenceresiBastirir(t *testing.T) {
	h := bh_kur(t)
	o := Olay{Seviye: "kritik", Kategori: "yedek", Baslik: "Yedek başarısız", Mesaj: "m", RefTur: "backup"}
	ilk := YazOlay(h.DB, o, 10, AktorYok, time.Hour)
	if ilk <= 0 {
		t.Fatal("ilk bildirim yazılmalı")
	}
	bt_okundu(t, h, bt_bayi(t, 7), 0)
	bt_okundu(t, h, bt_musteri(t, 10), 0)
	if YazOlay(h.DB, o, 10, AktorYok, time.Hour) != 0 {
		t.Fatal("pencere içinde (okunmuş olsa da) yeniden yazılmamalı")
	}
	if YazOlay(h.DB, o, 10, AktorYok, 0) <= 0 {
		t.Fatal("pencere yokken okunduktan sonra yeniden uyarı yazılmalı (eski davranış)")
	}
	bt_exec(t, h.DB, `UPDATE cp_bildirim SET created_at = NOW() - INTERVAL 2 HOUR`)
	bt_okundu(t, h, bt_bayi(t, 7), 0)
	bt_okundu(t, h, bt_musteri(t, 10), 0)
	if YazOlay(h.DB, o, 10, AktorYok, time.Hour) <= 0 {
		t.Fatal("pencere dışında yeniden yazılmalı")
	}
	// Başka domain aynı olay → bastırılmaz (hedef anahtarın parçası).
	if YazOlay(h.DB, o, 11, AktorYok, time.Hour) <= 0 {
		t.Fatal("farklı domain bastırılmamalı")
	}
}

// Aktör: kendi eylemi sahibin kopyasında okunmuş, müşteride okunmamış.
func TestYazOlay_AktorKendiKopyasiOkunmus(t *testing.T) {
	h := bh_kur(t)
	id := YazOlay(h.DB, Olay{Seviye: "uyari", Kategori: "aski", Baslik: "Hesap askıya alındı", Mesaj: "m"}, 10, AktorBayi, 0)
	if id <= 0 {
		t.Fatal("yazılmalı")
	}
	if n, _ := bh_sayac(t, h, bt_bayi(t, 7)); n != 0 {
		t.Errorf("bayi kendi eylemi için okunmamış görmemeli: %d", n)
	}
	if n, _ := bh_sayac(t, h, bt_musteri(t, 10)); n != 1 {
		t.Errorf("müşteri okunmamış görmeli: %d", n)
	}
	// Aktör yönetici ama domain bayi sahipli → bayinin kopyası okunmuş SAYILMAZ.
	YazOlay(h.DB, Olay{Seviye: "uyari", Kategori: "aski", Baslik: "Hesap yeniden etkinleştirildi", Mesaj: "m"}, 10, AktorYonetici, 0)
	if n, _ := bh_sayac(t, h, bt_bayi(t, 7)); n != 1 {
		t.Errorf("kökün eylemi bayiye okunmamış düşmeli: %d", n)
	}
}

func TestGuvenli(t *testing.T) {
	cases := map[string]string{
		"tar: /var/backups/gosp/c_x/a.tar.gz: Cannot open": "tar: [sunucu yolu]: Cannot open",
		"dosya /home/c_ornek/public_html/x.php bulundu":    "dosya public_html/x.php bulundu",
		"wp-content/uploads/tmp/x.php göreli kalır":        "wp-content/uploads/tmp/x.php göreli kalır",
		"satır1\nsatır2\r\nsatır3":                         "satır1 satır2 satır3",
		"hedef=/etc/nginx/conf.d/a.conf":                   "hedef=[sunucu yolu]",
	}
	for g, want := range cases {
		if got := Guvenli(g); got != want {
			t.Errorf("Guvenli(%q) = %q, beklenen %q", g, got, want)
		}
	}
	if r := []rune(Guvenli(strings.Repeat("ş", 2000))); len(r) != mesajSiniri+1 {
		t.Errorf("uzunluk sınırı: %d", len(r))
	}
}

// Domain hedefli bildirimde mesaj Guvenli'den geçer (tek nokta, eleştiri 14).
func TestYaz_KiraciMesajiTemizlenir(t *testing.T) {
	h := bh_kur(t)
	Yaz(h.DB, "kritik", "yedek", "Yedek başarısız", "tar: /var/lib/gosp/yedek/c_x.tar: exit status 2", 10, "backup", 0)
	y := bt_liste(t, h, bt_bayi(t, 7), "")
	if len(y.Bildirimler) != 1 || strings.Contains(y.Bildirimler[0].Mesaj, "/var/") {
		t.Fatalf("bayi listesinde sunucu yolu olmamalı: %+v", y.Bildirimler)
	}
	// Panel geneli (yalnız yönetici) mesajı olduğu gibi kalır.
	Yaz(h.DB, "kritik", "sistem", "Swap alani yok", "/etc/fstab kontrol edin", 0, "", 0)
	a := bt_liste(t, h, bt_admin(t), "")
	if len(a.Bildirimler) != 1 || !strings.Contains(a.Bildirimler[0].Mesaj, "/etc/fstab") {
		t.Fatalf("yönetici paneli geneli mesajı değişmemeli: %+v", a.Bildirimler)
	}
}

// Tercih: kapalı kategori listede ve sayaçta gizlenir; yalnız istek sahibini etkiler.
func TestTercih_SuzgecVeKapsam(t *testing.T) {
	h := bh_kur(t)
	Yaz(h.DB, "kritik", "yedek", "Yedek başarısız", "m", 10, "backup", 0)
	Yaz(h.DB, "uyari", "ssl", "SSL sertifikası yenilenemedi", "m", 10, "esik_ssl", 1)
	bayi7, mus10 := bt_bayi(t, 7), bt_musteri(t, 10)
	if n, k := bh_sayac(t, h, bayi7); n != 2 || k != 1 {
		t.Fatalf("ön koşul sayaç: %d/%d", n, k)
	}
	kod, g := bh_istek(t, h, http.MethodPut, "/bildirim-tercihleri", mus10, `{"tercihler":[{"kategori":"yedek","panel":false}]}`)
	if kod != http.StatusOK {
		t.Fatalf("müşteri tercih kaydı: %d %s", kod, g)
	}
	if n, k := bh_sayac(t, h, mus10); n != 1 || k != 0 {
		t.Errorf("müşteri yedeği kapattı → sayaç 1/0: %d/%d", n, k)
	}
	if y := bt_liste(t, h, mus10, ""); len(y.Bildirimler) != 1 || y.Bildirimler[0].Kategori != "ssl" || y.Okunmamis != 1 {
		t.Errorf("müşteri listesinde yalnız ssl: %+v", y)
	}
	// Bayinin görünümü etkilenmez (tercih görüntüleyene ait).
	if n, _ := bh_sayac(t, h, bayi7); n != 2 {
		t.Errorf("müşterinin tercihi bayiyi etkilememeli: %d", n)
	}
	// Rol dışı / bilinmeyen kategori → 400 (müşteri "toplu"/"sistem" ayarlayamaz).
	for _, gv := range []string{`{"tercihler":[{"kategori":"toplu","panel":false}]}`, `{"tercihler":[{"kategori":"yok","panel":false}]}`, `{}`, `x`} {
		if kod, _ := bh_istek(t, h, http.MethodPut, "/bildirim-tercihleri", mus10, gv); kod != http.StatusBadRequest {
			t.Errorf("geçersiz tercih %s → 400 beklenir, %d", gv, kod)
		}
	}
	// GET: rolün kategorileri + kapalı olan işaretli.
	kod, g = bh_istek(t, h, http.MethodGet, "/bildirim-tercihleri", mus10, "")
	if kod != http.StatusOK || !strings.Contains(g, `{"kategori":"yedek","panel":false}`) || strings.Contains(g, `"toplu"`) || !strings.Contains(g, `"eposta_kullanilabilir":false`) {
		t.Errorf("müşteri tercih listesi: %d %s", kod, g)
	}
	kod, g = bh_istek(t, h, http.MethodGet, "/bildirim-tercihleri", bt_admin(t), "")
	if kod != http.StatusOK || !strings.Contains(g, `"toplu"`) || strings.Contains(g, `"panel":false`) {
		t.Errorf("yönetici tercih listesi (müşterininkinden bağımsız): %s", g)
	}
}

func TestListe_Limit(t *testing.T) {
	h := bh_kur(t)
	for i := 0; i < 5; i++ {
		Yaz(h.DB, "bilgi", "yedek", "Yedek "+string(rune('a'+i)), "m", 10, "backup", int64(i))
	}
	b7 := bt_bayi(t, 7)
	if y := bt_liste(t, h, b7, "?limit=2"); len(y.Bildirimler) != 2 || y.Okunmamis != 5 {
		t.Errorf("limit=2: %d satır, okunmamış %d", len(y.Bildirimler), y.Okunmamis)
	}
	for _, q := range []string{"?limit=0", "?limit=500", "?limit=x"} {
		if y := bt_liste(t, h, b7, q); len(y.Bildirimler) != 5 {
			t.Errorf("%s geçersiz → varsayılan: %d", q, len(y.Bildirimler))
		}
	}
}

func TestTemizle_SaklamaSuresi(t *testing.T) {
	h := bh_kur(t)
	Yaz(h.DB, "bilgi", "yedek", "Eski", "m", 10, "backup", 1)
	Yaz(h.DB, "bilgi", "yedek", "Yeni", "m", 10, "backup", 2)
	bt_exec(t, h.DB, `UPDATE cp_bildirim SET created_at = NOW() - INTERVAL 200 DAY WHERE baslik='Eski'`)
	bt_okundu(t, h, bt_bayi(t, 7), 0)
	if n := Temizle(t.Context(), h.DB); n != 1 {
		t.Fatalf("1 eski satır silinmeli: %d", n)
	}
	if n := bt_sayi(t, h.DB, `SELECT COUNT(*) FROM cp_bildirim`); n != 1 {
		t.Errorf("yeni kalmalı: %d", n)
	}
	if n := bt_sayi(t, h.DB, `SELECT COUNT(*) FROM cp_bildirim_okundu k LEFT JOIN cp_bildirim b ON b.id=k.bildirim_id WHERE b.id IS NULL`); n != 0 {
		t.Errorf("okundu kayıtları CASCADE ile düşmeli: %d yetim", n)
	}
}
