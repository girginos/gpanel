package bildirim

// bildirim_domain_test.go — GET /bildirimler?domain_id=<n>: çağıranın görünürlük
// kapsamı İÇİNDE ek süzgeç (müşteri/alan adı panosu). Kapsam ASLA genişlemez:
// yabancı/geçersiz domain_id → boş liste; okunmamış sayacı parametreden
// bağımsızdır. Ayrıca liste hatasında ham SQL hatası istemciye gitmez.
//
// DB'li testler gerçek MariaDB ister (GOSP_TEST_GECICI_DSN; bkz. bildirim_test.go),
// yoksa ATLANIR. Hata metni testi DB'siz çalışır (kapalı *sql.DB).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"girginospanel/internal/sahtedb"
)

// bd_kur — 10,11: bayi 7; 20: bayi 8; 30: kök (reseller 0). Her domaine bir
// bildirim + panel geneli bir bildirim.
func bd_kur(t *testing.T) *Handlers {
	t.Helper()
	db := bt_semaHazir(t)
	bt_exec(t, db, `INSERT INTO domains (id, reseller_id) VALUES (10, 7), (11, 7), (20, 8), (30, 0)`)
	Yaz(db, "kritik", "yedek", "Yedek başarısız", "on", 10, "backup", 0)
	Yaz(db, "kritik", "yedek", "Yedek başarısız", "onbir", 11, "backup", 0)
	Yaz(db, "kritik", "yedek", "Yedek başarısız", "yirmi", 20, "backup", 0)
	Yaz(db, "kritik", "yedek", "Yedek başarısız", "otuz", 30, "backup", 0)
	Yaz(db, "kritik", "sistem", "Swap alani yok", "panel geneli", 0, "", 0)
	return &Handlers{DB: db}
}

func bd_domainler(y bt_yanit) map[int64]int {
	m := map[int64]int{}
	for _, b := range y.Bildirimler {
		var d int64
		if b.DomainID != nil {
			d = *b.DomainID
		}
		m[d]++
	}
	return m
}

func TestListe_DomainIDKapsamIcindeSuzer(t *testing.T) {
	h := bd_kur(t)
	bayi7 := bt_bayi(t, 7)

	// Parametresiz davranış değişmedi: bayi 7 → 10 ve 11.
	tum := bt_liste(t, h, bayi7, "")
	if d := bd_domainler(tum); len(tum.Bildirimler) != 2 || d[10] != 1 || d[11] != 1 {
		t.Fatalf("ön koşul: bayi 7 parametresiz 10+11 görmeli: %v", d)
	}

	y := bt_liste(t, h, bayi7, "?domain_id=10")
	if d := bd_domainler(y); len(y.Bildirimler) != 1 || d[10] != 1 {
		t.Errorf("domain_id=10 yalnız 10'un bildirimini dönmeli: %v", d)
	}
	if y.Okunmamis != tum.Okunmamis {
		t.Errorf("okunmamış sayacı parametreden bağımsız olmalı: %d != %d", y.Okunmamis, tum.Okunmamis)
	}

	// Kapsam genişlemez: yabancı bayinin (20) ve kökün (30) alan adı → boş.
	for _, q := range []string{"?domain_id=20", "?domain_id=30", "?domain_id=999"} {
		y := bt_liste(t, h, bayi7, q)
		if len(y.Bildirimler) != 0 {
			t.Errorf("bayi 7 %s: kapsam dışı → boş liste olmalı: %v", q, bd_domainler(y))
		}
		if y.Okunmamis != tum.Okunmamis {
			t.Errorf("bayi 7 %s: okunmamış sayacı değişmemeli: %d != %d", q, y.Okunmamis, tum.Okunmamis)
		}
	}
	// Geçersiz değer → boş liste (kapsamın tamamına düşülmez).
	for _, q := range []string{"?domain_id=abc", "?domain_id=0", "?domain_id=-10", "?domain_id=10abc"} {
		if y := bt_liste(t, h, bayi7, q); len(y.Bildirimler) != 0 {
			t.Errorf("bayi 7 %s: geçersiz domain_id → boş liste olmalı: %v", q, bd_domainler(y))
		}
	}
	// Öteki süzgeçlerle birleşir.
	if y := bt_liste(t, h, bayi7, "?domain_id=11&sadece_okunmamis=1&kategori=yedek"); len(y.Bildirimler) != 1 {
		t.Errorf("domain_id + sadece_okunmamis + kategori birlikte: %v", bd_domainler(y))
	}
}

func TestListe_DomainIDMusteriVeYonetici(t *testing.T) {
	h := bd_kur(t)
	m10 := bt_musteri(t, 10)
	if y := bt_liste(t, h, m10, "?domain_id=10"); len(y.Bildirimler) != 1 {
		t.Errorf("müşteri 10 kendi domain_id'siyle kendi bildirimini görmeli: %v", bd_domainler(y))
	}
	if y := bt_liste(t, h, m10, "?domain_id=11"); len(y.Bildirimler) != 0 {
		t.Errorf("müşteri 10 domain_id=11 ile başka alan adını görmemeli: %v", bd_domainler(y))
	}

	admin := bt_admin(t)
	tum := bt_liste(t, h, admin, "")
	if d := bd_domainler(tum); d[30] != 1 || d[0] != 1 || d[10] != 0 {
		t.Fatalf("ön koşul: yönetici kök (30) + panel geneli görmeli, bayi alan adını değil: %v", d)
	}
	if y := bt_liste(t, h, admin, "?domain_id=30"); len(y.Bildirimler) != 1 || y.Okunmamis != tum.Okunmamis {
		t.Errorf("yönetici domain_id=30: yalnız 30 + sayaç sabit: %v okunmamis=%d/%d", bd_domainler(y), y.Okunmamis, tum.Okunmamis)
	}
	if y := bt_liste(t, h, admin, "?domain_id=10"); len(y.Bildirimler) != 0 {
		t.Errorf("yönetici domain_id=10 (bayinin alan adı) ile kapsamını genişletmemeli: %v", bd_domainler(y))
	}
}

// Liste hatası: ham SQL/sürücü hatası istemciye gitmez (DB'siz: kapalı bağlantı).
func TestListe_HataHamSQLDonmez_Kapali(t *testing.T) {
	_, db := sahtedb.Yeni()
	_ = db.Close()
	h := &Handlers{DB: db}
	for rol, tok := range map[string]string{"yönetici": bt_admin(t), "bayi": bt_bayi(t, 7), "müşteri": bt_musteri(t, 10)} {
		req := httptest.NewRequest(http.MethodGet, "/bildirimler", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		bt_router(h).ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("%s: 500 beklenirdi: %d %s", rol, rec.Code, rec.Body.String())
		}
		var m map[string]string
		_ = json.Unmarshal(rec.Body.Bytes(), &m)
		if strings.Contains(m["hata"], "sql:") || strings.Contains(m["hata"], "closed") || m["hata"] == "" {
			t.Errorf("%s: ham sürücü hatası dönmemeli, genel mesaj beklenirdi: %q", rol, m["hata"])
		}
	}
}

// Liste hatası (gerçek MariaDB): tablo yokken ham "Error 1146 ... Table '<db>.<tablo>'"
// metni istemciye gitmez.
func TestListe_HataHamSQLDonmez_MariaDB(t *testing.T) {
	db := bt_semaHazir(t)
	h := &Handlers{DB: db}
	bt_exec(t, db, `DROP TABLE IF EXISTS cp_bildirim_okundu`)
	bt_exec(t, db, `DROP TABLE cp_bildirim`)
	req := httptest.NewRequest(http.MethodGet, "/bildirimler", nil)
	req.Header.Set("Authorization", "Bearer "+bt_bayi(t, 7))
	rec := httptest.NewRecorder()
	bt_router(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("500 beklenirdi: %d %s", rec.Code, rec.Body.String())
	}
	for _, iz := range []string{"1146", "doesn't exist", "gosp_test_bildirim", "cp_bildirim"} {
		if strings.Contains(rec.Body.String(), iz) {
			t.Errorf("ham SQL hatası izi %q yanıtta: %s", iz, rec.Body.String())
		}
	}
}
