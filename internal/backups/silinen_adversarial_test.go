package backups

// silinen_adversarial_test.go — yetim yedek uçları ve süpürmesi için KIRMA denemeleri: symlink,
// yol kaçışı, sk regex, sahiplik, uzak hata, limit, ctx iptali, eşzamanlı süpürme.
// Gerçek BackupRoot'a YAZILMAZ (yedekKok geçici dizin). Geçici MariaDB gerekir.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
)

// advRouter: AdminOnly'siz ham handler rotaları (main.go ile AYNI yollar).
func advRouter(db interface{ SetMaxOpenConns(int) }, h *Handlers) http.Handler {
	db.SetMaxOpenConns(8)
	r := chi.NewRouter()
	r.Get("/s", h.SilinenListe)
	r.Get("/s/ayar", h.SilinenAyarGet)
	r.Put("/s/ayar", h.SilinenAyarSet)
	r.Get("/s/yedek/{bid}/indir", h.SilinenIndir)
	r.Post("/s/{sk}/bagla", h.SilinenBagla)
	r.Delete("/s/{sk}", h.SilinenSil)
	return r
}

func advIstek(r http.Handler, method, yol, govde string) (int, string) {
	req := httptest.NewRequest(method, yol, strings.NewReader(govde))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// ---------------------------------------------------------------- indir

func TestAdvIndir_YalnizYetimDuzDosya(t *testing.T) {
	db := silinenDB(t)
	kok := silinenOrtam(t)
	h := &Handlers{DB: db}
	r := advRouter(db, h)

	gizli := filepath.Join(t.TempDir(), "gizli.txt")
	_ = os.WriteFile(gizli, []byte("ROOT-SIRRI"), 0o600)
	silinenDosya(t, kok, "c_x", "c_x-20261001-000000.tar.gz")
	if err := os.Symlink(gizli, filepath.Join(kok, "c_x", "c_x-20261002-000000.tar.gz")); err != nil {
		t.Fatal(err)
	}
	ok := silinenYetim(t, db, 9301, "x.test", "c_x", "c_x-20261001-000000.tar.gz", 0, "NULL")
	lnk := silinenYetim(t, db, 9301, "x.test", "c_x", "c_x-20261002-000000.tar.gz", 0, "NULL")
	gez := silinenYetim(t, db, 9301, "x.test", "c_x", "../gizli.txt", 0, "NULL")
	gez2 := silinenYetim(t, db, 9301, "x.test", "c_x", "a/b.tar.gz", 0, "NULL")
	yok := silinenYetim(t, db, 9301, "x.test", "c_x", "c_x-20261003-000000.tar.gz", 0, "NULL")

	if kod, g := advIstek(r, "GET", fmt.Sprintf("/s/yedek/%d/indir", ok), ""); kod != 200 || g != "12345" {
		t.Fatalf("pozitif kontrol: yetim düz dosya inmeli: %d %q", kod, g)
	}
	for ad, id := range map[string]int64{"symlink dosya": lnk, "../ kaçışı": gez, "alt dizin": gez2, "diskte yok": yok, "olmayan id": 999999} {
		if kod, g := advIstek(r, "GET", fmt.Sprintf("/s/yedek/%d/indir", id), ""); kod == 200 || strings.Contains(g, "ROOT-SIRRI") {
			t.Errorf("%s: indirilmemeli: %d %q", ad, kod, g)
		}
	}
	// YAŞAYAN domainin yedeği (yetim değil) bu uçtan inmez.
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (9302,'canli.test','c_canli')`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya, sistem_kullanici, silinme) VALUES (9302,'c_canli-20261001-000000.tar.gz','c_canli',NOW())`)
	silinenDosya(t, kok, "c_canli", "c_canli-20261001-000000.tar.gz")
	if kod, _ := advIstek(r, "GET", fmt.Sprintf("/s/yedek/%d/indir", satirID(t, db, 9302)), ""); kod == 200 {
		t.Error("domaini canlı olan satır (silinme dolu olsa bile) yetim sayılmamalı")
	}
	if kod, _ := advIstek(r, "GET", "/s/yedek/abc/indir", ""); kod == 200 {
		t.Error("geçersiz id")
	}
}

func TestAdvIndir_DizinSymlinkiDisariyaCikmaz(t *testing.T) {
	db := silinenDB(t)
	kok := silinenOrtam(t)
	r := advRouter(db, &Handlers{DB: db})
	disari := t.TempDir()
	_ = os.WriteFile(filepath.Join(disari, "c_lnk-20261001-000000.tar.gz"), []byte("BASKA"), 0o600)
	if err := os.Symlink(disari, filepath.Join(kok, "c_lnk")); err != nil {
		t.Fatal(err)
	}
	id := silinenYetim(t, db, 9310, "lnk.test", "c_lnk", "c_lnk-20261001-000000.tar.gz", 0, "DATE_SUB(NOW(), INTERVAL 1 DAY)")
	if kod, g := advIstek(r, "GET", fmt.Sprintf("/s/yedek/%d/indir", id), ""); kod == 200 {
		t.Errorf("dizin symlink'i üzerinden dışarıdaki dosya indirildi: %q", g)
	}
	if _, g := advIstek(r, "GET", "/s", ""); strings.Contains(g, `"yerel":true`) {
		t.Errorf("symlink dizindeki dosya yerel görünmemeli: %s", g)
	}
	// Süpürme ve DELETE dışarıdaki dosyaya DOKUNMAZ, satır korunur.
	SilinenSupur(context.Background(), db)
	if kod, _ := advIstek(r, "DELETE", "/s/c_lnk", ""); kod != 409 {
		t.Errorf("güvensiz dizin: 409 beklenirdi: %d", kod)
	}
	if !varMi(filepath.Join(disari, "c_lnk-20261001-000000.tar.gz")) {
		t.Fatal("dizin symlink'i takip edilip BackupRoot DIŞINDAKİ dosya silindi")
	}
	if !satirVarMi(t, db, id) {
		t.Error("güvensiz dizinde satır silinmemeli")
	}
}

// ---------------------------------------------------------------- liste

func TestAdvListe_GrupluYalnizYetimEnYeniOnce(t *testing.T) {
	db := silinenDB(t)
	kok := silinenOrtam(t)
	r := advRouter(db, &Handlers{DB: db})
	silinenDosya(t, kok, "c_a", "c_a-20261001-000000.tar.gz")
	silinenYetim(t, db, 9401, "a.test", "c_a", "c_a-20261001-000000.tar.gz", 3, "DATE_ADD(NOW(), INTERVAL 5 DAY)")
	silinenYetim(t, db, 9401, "a.test", "c_a", "c_a-20261002-000000.tar.gz", 3, "DATE_ADD(NOW(), INTERVAL 5 DAY)")
	g113_exec(t, db, `UPDATE backups SET silinme=DATE_SUB(NOW(), INTERVAL 2 DAY)`)
	silinenYetim(t, db, 9402, "b.test", "c_b", "c_b-20261001-000000.tar.gz", 0, "NULL")
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (9403,'a2.test','c_a')`) // aynı sk'li canlı domain
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (9404,'canli.test','c_canli')`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya, silinme) VALUES (9404,'c_canli-20261001-000000.tar.gz',NOW())`)

	kod, g := advIstek(r, "GET", "/s", "")
	if kod != 200 {
		t.Fatalf("%d %s", kod, g)
	}
	var out []struct {
		SK        string `json:"sk"`
		Alan      string `json:"alan_adi"`
		Rid       int64  `json:"reseller_id"`
		Sonk      string `json:"son_kullanma"`
		N         int    `json:"dosya_sayisi"`
		Boyut     int64  `json:"boyut_b"`
		YerelVar  bool   `json:"yerel_var"`
		Baglanabi *int64 `json:"baglanabilir_domain_id"`
		Yedekler  []struct {
			ID    int64 `json:"id"`
			Yerel bool  `json:"yerel"`
		} `json:"yedekler"`
	}
	if err := json.Unmarshal([]byte(g), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("2 grup (c_b en yeni, c_a), canlı domain listelenmemeli: %s", g)
	}
	if out[0].SK != "c_b" || out[0].Sonk != "" || out[0].Baglanabi != nil || out[0].YerelVar {
		t.Errorf("c_b grubu: %+v", out[0])
	}
	a := out[1]
	if a.SK != "c_a" || a.N != 2 || a.Boyut != 10 || a.Rid != 3 || !a.YerelVar || a.Baglanabi == nil || *a.Baglanabi != 9403 || a.Sonk == "" {
		t.Errorf("c_a grubu: %+v", a)
	}
	yerelSay := 0
	for _, y := range a.Yedekler {
		if y.Yerel {
			yerelSay++
		}
	}
	if yerelSay != 1 {
		t.Errorf("yalnız diskteki 1 dosya yerel olmalı: %d", yerelSay)
	}
}

// ---------------------------------------------------------------- bağla

func TestAdvBagla(t *testing.T) {
	db := silinenDB(t)
	r := advRouter(db, &Handlers{DB: db})
	a := silinenYetim(t, db, 9501, "a.test", "c_bag", "c_bag-20261001-000000.tar.gz", 8, "NULL") // farklı sahip de olsa YÖNETİCİ bağlar
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES (9502,'yeni.test','c_bag',9)`)
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (9503,'baska.test','c_baska')`)
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (9504,'diger.test','c_bag2')`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya, sistem_kullanici) VALUES (9504,'c_bag-20261201-000000.tar.gz','c_bag')`) // canlı domainin satırı
	canliSatir := satirID(t, db, 9504)

	if kod, _ := advIstek(r, "POST", "/s/c_bag/bagla", `{"domain_id":9503}`); kod != 400 {
		t.Errorf("sk uyuşmazlığı 400 olmalı: %d", kod)
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE id=? AND silinme IS NOT NULL`, a) != 1 {
		t.Fatal("reddedilen bağlamada satır değişti")
	}
	if kod, _ := advIstek(r, "POST", "/s/c_bag/bagla", `{"domain_id":424242}`); kod != 404 {
		t.Errorf("olmayan domain 404: %d", kod)
	}
	for _, govde := range []string{`{}`, `{"domain_id":0}`, `{"domain_id":-1}`, `bozuk`} {
		if kod, _ := advIstek(r, "POST", "/s/c_bag/bagla", govde); kod != 400 {
			t.Errorf("%s: 400 beklenirdi: %d", govde, kod)
		}
	}
	if kod, _ := advIstek(r, "POST", "/s/BAD..sk/bagla", `{"domain_id":9502}`); kod != 400 {
		t.Errorf("geçersiz sk: %d", kod)
	}
	kod, g := advIstek(r, "POST", "/s/c_bag/bagla", `{"domain_id":9502}`)
	if kod != 200 || !strings.Contains(g, `"baglanan":1`) {
		t.Fatalf("bağlama: %d %s", kod, g)
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE id=? AND domain_id=9502 AND silinme IS NULL AND son_kullanma IS NULL`, a) != 1 {
		t.Error("satır bağlanmadı / silinme temizlenmedi")
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE id=? AND domain_id=9504`, canliSatir) != 1 {
		t.Error("domaini canlı olan (yetim olmayan) satır başka domaine TAŞINDI")
	}
	if _, g := advIstek(r, "POST", "/s/c_bag/bagla", `{"domain_id":9502}`); !strings.Contains(g, `"baglanan":0`) {
		t.Errorf("ikinci bağlama 0 olmalı: %s", g)
	}
}

// ---------------------------------------------------------------- DELETE {sk}

func TestAdvSil_UzakYoksa_DosyaSatirVeBosDizinGider_DigerlerineDokunmaz(t *testing.T) {
	db := silinenDB(t)
	kok := silinenOrtam(t)
	r := advRouter(db, &Handlers{DB: db})
	y1 := silinenDosya(t, kok, "c_del", "c_del-20261001-000000.tar.gz")
	diger := silinenDosya(t, kok, "c_diger", "c_diger-20261001-000000.tar.gz")
	a := silinenYetim(t, db, 9601, "d.test", "c_del", "c_del-20261001-000000.tar.gz", 0, "NULL")
	b := silinenYetim(t, db, 9601, "d.test", "c_del", "c_del-20261002-000000.tar.gz", 0, "NULL") // dosyası diskte yok
	d := silinenYetim(t, db, 9602, "o.test", "c_diger", "c_diger-20261001-000000.tar.gz", 0, "NULL")
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya, sistem_kullanici) VALUES (9603,'c_del-20261009-000000.tar.gz','c_del')`) // silme izi yok
	izsiz := satirID(t, db, 9603)

	if kod, _ := advIstek(r, "DELETE", "/s/c_yok", ""); kod != 404 {
		t.Errorf("olmayan sk 404: %d", kod)
	}
	if kod, _ := advIstek(r, "DELETE", "/s/C_BAD", ""); kod != 400 {
		t.Errorf("geçersiz sk 400: %d", kod)
	}
	kod, g := advIstek(r, "DELETE", "/s/c_del", "")
	if kod != 200 {
		t.Fatalf("%d %s", kod, g)
	}
	if varMi(y1) || satirVarMi(t, db, a) || satirVarMi(t, db, b) {
		t.Error("dosya ve satırlar silinmeliydi")
	}
	if varMi(filepath.Join(kok, "c_del")) {
		t.Error("boşalan sk dizini kaldırılmalıydı")
	}
	if !varMi(diger) || !satirVarMi(t, db, d) {
		t.Error("başka sk'ya DOKUNULDU")
	}
	if !satirVarMi(t, db, izsiz) {
		t.Error("silme izi olmayan (yetim sayılmayan) satır silinmemeli")
	}
}

func TestAdvSil_CanliHostingAyniSK_DizinVeCanliSatirKorunur(t *testing.T) {
	db := silinenDB(t)
	kok := silinenOrtam(t)
	r := advRouter(db, &Handlers{DB: db})
	silinenDosya(t, kok, "c_ort", "c_ort-20261001-000000.tar.gz")
	canliDosya := silinenDosya(t, kok, "c_ort", "c_ort-20261201-000000.tar.gz")
	yetim := silinenYetim(t, db, 9610, "ort.test", "c_ort", "c_ort-20261001-000000.tar.gz", 0, "NULL")
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (9611,'ort2.test','c_ort')`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya) VALUES (9611,'c_ort-20261201-000000.tar.gz')`)
	canli := satirID(t, db, 9611)
	if kod, g := advIstek(r, "DELETE", "/s/c_ort", ""); kod != 200 {
		t.Fatalf("%d %s", kod, g)
	}
	if satirVarMi(t, db, yetim) || !satirVarMi(t, db, canli) || !varMi(canliDosya) {
		t.Error("yalnız yetim silinmeli; sk'yı devralan canlı hostingin yedeği KORUNMALI")
	}
}

func TestAdvSil_UzakHata_DosyaVeSatirKalir_UzagiYoksayarSiler(t *testing.T) {
	db := silinenDB(t)
	kok := silinenOrtam(t)
	silinenUzakHedef(t, db)
	r := advRouter(db, &Handlers{DB: db})
	y := silinenDosya(t, kok, "c_uz", "c_uz-20261001-000000.tar.gz")
	id := silinenYetim(t, db, 9620, "uz.test", "c_uz", "c_uz-20261001-000000.tar.gz", 0, "NULL")

	for _, lftpYok := range []bool{false, true} { // lftp varsa bağlantı hatası, yoksa ErrLftpYok: ikisi de HATA
		if lftpYok {
			t.Setenv("PATH", t.TempDir())
		}
		kod, g := advIstek(r, "DELETE", "/s/c_uz", "")
		if kod != 409 || !strings.Contains(g, `"uzagi_yoksay":true`) || !strings.Contains(g, `"ok":false`) {
			t.Fatalf("lftpYok=%v: uzak silinemedi → 409 beklenirdi: %d %s", lftpYok, kod, g)
		}
		if !varMi(y) || !satirVarMi(t, db, id) {
			t.Fatalf("lftpYok=%v: uzak silinemediyse yerel dosya + satır KALMALI", lftpYok)
		}
		for _, yasak := range []string{kok, "gosp-lftp", "parola", "password"} {
			if strings.Contains(g, yasak) {
				t.Errorf("yanıt %q sızdırıyor: %s", yasak, g)
			}
		}
	}
	if kod, g := advIstek(r, "DELETE", "/s/c_uz?uzagi_yoksay=1", ""); kod != 200 {
		t.Fatalf("uzagi_yoksay: %d %s", kod, g)
	}
	if varMi(y) || satirVarMi(t, db, id) {
		t.Error("uzagi_yoksay=1 yerel dosya + satırı silmeliydi")
	}
}

func TestAdvYetimYedegiSil_GuvensizGirdiler(t *testing.T) {
	db := silinenDB(t)
	kok := silinenOrtam(t)
	disari := filepath.Join(t.TempDir(), "disari.tar.gz")
	_ = os.WriteFile(disari, []byte("X"), 0o600)
	silinenDosya(t, kok, "c_g", "c_g-20261001-000000.tar.gz")
	id := silinenYetim(t, db, 9630, "g.test", "c_g", "../../x", 0, "NULL")
	ctx := context.Background()
	for _, c := range []struct{ sk, dosya string }{
		{"c_g", "../../" + filepath.Base(disari)}, {"c_g", "a/b.tar.gz"}, {"c_g", ""}, {"c_g", ".."}, {"c_g", "x.txt"},
		{"../c_g", "c_g-20261001-000000.tar.gz"}, {"C_G", "x.tar.gz"}, {"", "x.tar.gz"},
	} {
		for _, yoksay := range []bool{false, true} {
			if yetimYedegiSil(ctx, db, id, 9630, c.sk, c.dosya, yoksay) == nil {
				t.Errorf("%+v yoksay=%v: reddedilmeliydi", c, yoksay)
			}
		}
	}
	if !varMi(disari) || !varMi(filepath.Join(kok, "c_g", "c_g-20261001-000000.tar.gz")) {
		t.Error("güvensiz girdi bir dosyayı sildi")
	}
}

// ---------------------------------------------------------------- süpürme

func TestAdvSupur_YalnizSuresiDolanYetimSilinir(t *testing.T) {
	db := silinenDB(t)
	kok := silinenOrtam(t)
	dolan := silinenDosya(t, kok, "c_s", "c_s-20261001-000000.tar.gz")
	dolmayan := silinenDosya(t, kok, "c_s", "c_s-20261002-000000.tar.gz")
	suresiz := silinenDosya(t, kok, "c_s", "c_s-20261003-000000.tar.gz")
	canli := silinenDosya(t, kok, "c_s", "c_s-20261004-000000.tar.gz")
	a := silinenYetim(t, db, 9701, "s.test", "c_s", "c_s-20261001-000000.tar.gz", 0, "DATE_SUB(NOW(), INTERVAL 1 HOUR)")
	b := silinenYetim(t, db, 9701, "s.test", "c_s", "c_s-20261002-000000.tar.gz", 0, "DATE_ADD(NOW(), INTERVAL 1 DAY)")
	c := silinenYetim(t, db, 9701, "s.test", "c_s", "c_s-20261003-000000.tar.gz", 0, "NULL")
	// Süresi dolmuş görünen ama domaini CANLI olan satır (rebind sonrası artefaktı) silinmemeli.
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (9702,'c.test','c_s')`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya, silinme, son_kullanma) VALUES (9702,'c_s-20261004-000000.tar.gz',NOW(),DATE_SUB(NOW(), INTERVAL 1 DAY))`)
	d := satirID(t, db, 9702)
	// Silme izi olmayan (silinme NULL) ve domain'i olmayan satır: otomatik temizlenmez.
	izsizDosya := silinenDosya(t, kok, "c_iz", "c_iz-20261001-000000.tar.gz")
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya, sistem_kullanici, son_kullanma) VALUES (9703,'c_iz-20261001-000000.tar.gz','c_iz',DATE_SUB(NOW(), INTERVAL 1 DAY))`)
	e := satirID(t, db, 9703)

	SilinenSupur(context.Background(), db)

	if satirVarMi(t, db, a) || varMi(dolan) {
		t.Error("süresi dolan yetim silinmeliydi")
	}
	for ad, v := range map[string]struct {
		id  int64
		yol string
	}{"dolmayan": {b, dolmayan}, "süresiz": {c, suresiz}, "canlı": {d, canli}} {
		if !satirVarMi(t, db, v.id) || (v.yol != "" && !varMi(v.yol)) {
			t.Errorf("%s silinmemeliydi", ad)
		}
	}
	if !satirVarMi(t, db, e) || !varMi(izsizDosya) {
		t.Error("silme izi olmayan satır otomatik silinmemeli")
	}
}

func TestAdvSupur_Limit200_CtxIptali_UzakHata(t *testing.T) {
	db := silinenDB(t)
	silinenOrtam(t)
	for i := 0; i < 205; i++ {
		g113_exec(t, db, `INSERT INTO backups (domain_id, dosya, sistem_kullanici, silinme, son_kullanma) VALUES (9801,?, 'c_lim', NOW(), DATE_SUB(NOW(), INTERVAL 1 DAY))`,
			fmt.Sprintf("c_lim-20261001-%06d.tar.gz", i))
	}
	iptal, cancel := context.WithCancel(context.Background())
	cancel()
	SilinenSupur(iptal, db)
	if n := g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=9801`); n != 205 {
		t.Fatalf("iptal edilmiş ctx ile hiçbir şey silinmemeli: %d", n)
	}
	SilinenSupur(context.Background(), db)
	if n := g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=9801`); n != 5 {
		t.Errorf("tur başına en çok 200 silinmeli, 5 kalmalı: %d", n)
	}
	SilinenSupur(context.Background(), db)
	if n := g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=9801`); n != 0 {
		t.Errorf("ertesi tur kalanı silmeli: %d", n)
	}

	// Uzak hedef ulaşılamaz → satır + dosya kalır (ertesi gün tekrar denenir).
	kok := silinenOrtam(t)
	silinenUzakHedef(t, db)
	y := silinenDosya(t, kok, "c_uz2", "c_uz2-20261001-000000.tar.gz")
	id := silinenYetim(t, db, 9802, "uz.test", "c_uz2", "c_uz2-20261001-000000.tar.gz", 0, "DATE_SUB(NOW(), INTERVAL 1 HOUR)")
	SilinenSupur(context.Background(), db)
	if !satirVarMi(t, db, id) || !varMi(y) {
		t.Error("uzak silinemediyse süpürme satırı/dosyayı KORUMALI")
	}
}

func TestAdvSupur_EszamanliTurlar(t *testing.T) {
	db := silinenDB(t)
	silinenOrtam(t)
	for i := 0; i < 40; i++ {
		g113_exec(t, db, `INSERT INTO backups (domain_id, dosya, sistem_kullanici, silinme, son_kullanma) VALUES (9901,?, 'c_es', NOW(), DATE_SUB(NOW(), INTERVAL 1 DAY))`,
			fmt.Sprintf("c_es-20261001-%06d.tar.gz", i))
	}
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); SilinenSupur(context.Background(), db) }()
	}
	wg.Wait()
	if n := g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=9901`); n != 0 {
		t.Errorf("eşzamanlı turlardan sonra hepsi silinmeli: %d", n)
	}
}

var _ sql.DB

func TestAdvSil_CanliHostingAyniSK_BosDizinKorunur(t *testing.T) {
	db := silinenDB(t)
	kok := silinenOrtam(t)
	r := advRouter(db, &Handlers{DB: db})
	silinenDosya(t, kok, "c_bos", "c_bos-20261001-000000.tar.gz")
	silinenYetim(t, db, 9640, "bos.test", "c_bos", "c_bos-20261001-000000.tar.gz", 0, "NULL")
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (9641,'bos2.test','c_bos')`)
	if kod, g := advIstek(r, "DELETE", "/s/c_bos", ""); kod != 200 {
		t.Fatalf("%d %s", kod, g)
	}
	if !varMi(filepath.Join(kok, "c_bos")) {
		t.Error("sk'yı kullanan canlı hosting varken (boş bile olsa) dizin kaldırılmamalı")
	}
}
