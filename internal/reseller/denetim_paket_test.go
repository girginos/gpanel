package reseller

// denetim_paket_test.go — bayi paketi uçlarının denetim kaydı (audit_log):
//
//	POST   /reseller-plans        PaketOlustur   → bayi_paketi.olustur
//	PUT    /reseller-plans/{id}   PaketGuncelle  → bayi_paketi.guncelle
//	DELETE /reseller-plans/{id}   PaketSil       → bayi_paketi.sil
//
// GERÇEK kimlik zinciri (RequireAuth + AdminOnly, main.go ile aynı). Başarı ve DB
// hatası kayıtları; reddedilen/etkisiz istekler (yetkisiz, doğrulama, olmayan paket,
// bağlı bayi var) kayıt YAZMAZ. Detayda yalnız sayısal limitler durur (açıklama/fiyat yok).
// Geçici MariaDB (GOSP_TEST_GECICI_DSN yoksa atlanır).

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/middleware"
)

func bpRouter(h *Handlers) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequireAuth(kaSir))
	a := middleware.AdminOnly
	r.With(a).Post("/reseller-plans", h.PaketOlustur)
	r.With(a).Put("/reseller-plans/{id}", h.PaketGuncelle)
	r.With(a).Delete("/reseller-plans/{id}", h.PaketSil)
	return r
}

type bpKayit struct {
	Hedef, Not, Aktor, Ham string
	Ok                     int
	Kapsam                 int64
}

func bpKayitlar(t *testing.T, db *sql.DB, eylem string) []bpKayit {
	t.Helper()
	rows, err := db.Query(`SELECT target, COALESCE(detail,''), actor_username, ok, reseller_id
		FROM audit_log WHERE action=? ORDER BY id`, eylem)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []bpKayit
	for rows.Next() {
		var k bpKayit
		if err := rows.Scan(&k.Hedef, &k.Ham, &k.Aktor, &k.Ok, &k.Kapsam); err != nil {
			t.Fatal(err)
		}
		var m map[string]string
		_ = json.Unmarshal([]byte(k.Ham), &m)
		k.Not = m["not"]
		out = append(out, k)
	}
	return out
}

func bpTek(t *testing.T, db *sql.DB, eylem, hedef, not string, ok int) {
	t.Helper()
	k := bpKayitlar(t, db, eylem)
	if len(k) != 1 {
		t.Fatalf("%s: tam 1 denetim kaydı beklenir, alınan %d: %+v", eylem, len(k), k)
	}
	g := k[0]
	if g.Hedef != hedef || g.Not != not || g.Ok != ok || g.Aktor != "root" || g.Kapsam != 0 {
		t.Errorf("%s: kayıt %+v; beklenen hedef=%q detay=%q ok=%d aktör=root kapsam=0", eylem, g, hedef, not, ok)
	}
	for _, s := range []string{"GIZLI-ACIKLAMA", "fiyat", "12345"} {
		if strings.Contains(g.Ham, s) {
			t.Errorf("%s: denetim detayında paket içeriği (%q) var: %s", eylem, s, g.Ham)
		}
	}
}

func bpToplam(t *testing.T, db *sql.DB) int {
	t.Helper()
	return opSay(t, db, `SELECT COUNT(*) FROM audit_log`)
}

func bpGovde(ad string, disk int64, varsayilan bool) map[string]any {
	return map[string]any{"ad": ad, "aciklama": "GIZLI-ACIKLAMA", "max_domain": 10, "max_disk_mb": disk,
		"max_trafik_mb": 1000000, "fiyat_kurus": 12345, "asim_ilkesi": "yok", "fazla_satis": true, "varsayilan": varsayilan}
}

func TestBayiPaketi_Olustur_Denetim(t *testing.T) {
	t.Run("başarı", func(t *testing.T) {
		db := kaDB(t)
		h := bpRouter(&Handlers{DB: db})
		w := kaAdmin(t).istek(h, "POST", "/reseller-plans", bpGovde("Denetim Paketi", 100000, true))
		if w.Code != http.StatusCreated {
			t.Fatalf("201 beklenir: %d %s", w.Code, w.Body.String())
		}
		bpTek(t, db, "bayi_paketi.olustur", "Denetim Paketi", "domain=10 disk=100000 trafik=1000000 varsayilan=true", 1)
	})
	t.Run("DB hatası", func(t *testing.T) {
		db := kaDB(t)
		if _, err := db.Exec(`ALTER TABLE reseller_plans DROP COLUMN fiyat_kurus`); err != nil {
			t.Fatal(err)
		}
		h := bpRouter(&Handlers{DB: db})
		w := kaAdmin(t).istek(h, "POST", "/reseller-plans", bpGovde("Hatalı Paket", 100000, false))
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("500 beklenir: %d %s", w.Code, w.Body.String())
		}
		bpTek(t, db, "bayi_paketi.olustur", "Hatalı Paket", "domain=10 disk=100000 trafik=1000000", 0)
	})
	t.Run("reddedilenler kayıtsız", func(t *testing.T) {
		db := kaDB(t)
		h := bpRouter(&Handlers{DB: db})
		once := opSay(t, db, `SELECT COUNT(*) FROM reseller_plans`)
		for _, c := range []struct {
			ad  string
			tok kaJeton
			g   any
			kod int
		}{
			{"geçersiz gövde", kaAdmin(t), `{`, http.StatusBadRequest},
			{"ad yok", kaAdmin(t), map[string]any{"max_domain": 1}, http.StatusBadRequest},
			{"negatif limit", kaAdmin(t), map[string]any{"ad": "X", "max_domain": -1}, http.StatusBadRequest},
			{"bayi yetkisiz", kaBayi(t, 10, "bayia"), bpGovde("Yetkisiz", 100000, false), http.StatusForbidden},
		} {
			if w := c.tok.istek(h, "POST", "/reseller-plans", c.g); w.Code != c.kod {
				t.Errorf("%s: %d beklenir, alınan %d %s", c.ad, c.kod, w.Code, w.Body.String())
			}
		}
		if sonra := opSay(t, db, `SELECT COUNT(*) FROM reseller_plans`); sonra != once {
			t.Errorf("reddedilen istek paket oluşturdu: %d → %d", once, sonra)
		}
		if n := bpToplam(t, db); n != 0 {
			t.Errorf("reddedilen isteklerde denetim kaydı yazılmamalı: %d", n)
		}
	})
}

func TestBayiPaketi_Guncelle_Denetim(t *testing.T) {
	t.Run("başarı", func(t *testing.T) {
		db := kaDB(t)
		h := bpRouter(&Handlers{DB: db})
		w := kaAdmin(t).istek(h, "PUT", "/reseller-plans/1", bpGovde("Bayi S", 200000, false))
		if w.Code != http.StatusOK {
			t.Fatalf("200 beklenir: %d %s", w.Code, w.Body.String())
		}
		bpTek(t, db, "bayi_paketi.guncelle", "Bayi S", "domain=10 disk=200000 trafik=1000000", 1)
	})
	t.Run("DB hatası", func(t *testing.T) {
		db := kaDB(t)
		if _, err := db.Exec(`ALTER TABLE reseller_plans DROP COLUMN fiyat_kurus`); err != nil {
			t.Fatal(err)
		}
		h := bpRouter(&Handlers{DB: db})
		w := kaAdmin(t).istek(h, "PUT", "/reseller-plans/1", bpGovde("Bayi S", 200000, false))
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("500 beklenir: %d %s", w.Code, w.Body.String())
		}
		bpTek(t, db, "bayi_paketi.guncelle", "Bayi S", "domain=10 disk=200000 trafik=1000000", 0)
	})
	t.Run("olmayan paket / yetkisiz / doğrulama kayıtsız", func(t *testing.T) {
		db := kaDB(t)
		h := bpRouter(&Handlers{DB: db})
		for _, c := range []struct {
			ad, yol string
			tok     kaJeton
			g       any
			kod     int
		}{
			// 2026-10-13: olmayan paket 404 (eskiden 200 + varsayılan sıfırlanıyordu); id sayı değilse de 404.
			{"olmayan paket", "/reseller-plans/999", kaAdmin(t), bpGovde("Hayalet", 100000, true), http.StatusNotFound},
			{"sayı olmayan id", "/reseller-plans/abc", kaAdmin(t), bpGovde("Hayalet", 100000, true), http.StatusNotFound},
			{"bayi yetkisiz", "/reseller-plans/1", kaBayi(t, 10, "bayia"), bpGovde("Bayi S", 100000, false), http.StatusForbidden},
			{"geçersiz gövde", "/reseller-plans/1", kaAdmin(t), `{`, http.StatusBadRequest},
			{"ad yok", "/reseller-plans/1", kaAdmin(t), map[string]any{"max_domain": 1}, http.StatusBadRequest},
		} {
			if w := c.tok.istek(h, "PUT", c.yol, c.g); w.Code != c.kod {
				t.Errorf("%s: %d beklenir, alınan %d %s", c.ad, c.kod, w.Code, w.Body.String())
			}
		}
		if n := bpToplam(t, db); n != 0 {
			t.Errorf("etkisiz/reddedilen isteklerde denetim kaydı yazılmamalı: %d", n)
		}
		if ad := func() string {
			var s string
			_ = db.QueryRow(`SELECT ad FROM reseller_plans WHERE id=1`).Scan(&s)
			return s
		}(); ad != "Bayi S" {
			t.Errorf("yetkisiz/etkisiz istek paketi değiştirdi: %q", ad)
		}
	})
}

// Gerçek VPS 2026-10-13: olmayan pakete varsayilan:true mevcut varsayılanı SIFIRLAMAZ;
// ad çakışması 409 (ham SQL metni / indeks adı dönmez).
func TestBayiPaketi_Guncelle_Varsayilan_AdCakismasi(t *testing.T) {
	db := kaDB(t)
	h := bpRouter(&Handlers{DB: db})
	if _, err := db.Exec(`UPDATE reseller_plans SET varsayilan=1 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO reseller_plans (id, ad) VALUES (2,'Bayi M')`); err != nil {
		t.Fatal(err)
	}
	// Üretim şeması gibi: paket adı benzersiz (uq_rp_ad).
	if _, err := db.Exec(`ALTER TABLE reseller_plans ADD UNIQUE KEY uq_rp_ad (ad)`); err != nil {
		t.Fatal(err)
	}
	if w := kaAdmin(t).istek(h, "PUT", "/reseller-plans/999", bpGovde("Hayalet", 100000, true)); w.Code != http.StatusNotFound {
		t.Fatalf("olmayan paket 404 beklenir: %d %s", w.Code, w.Body.String())
	}
	if n := opSay(t, db, `SELECT COUNT(*) FROM reseller_plans WHERE id=1 AND varsayilan=1`); n != 1 {
		t.Fatal("olmayan pakete PUT mevcut varsayılanı sıfırladı")
	}
	w := kaAdmin(t).istek(h, "PUT", "/reseller-plans/2", bpGovde("Bayi S", 100000, false))
	if w.Code != http.StatusConflict || strings.Contains(w.Body.String(), "1062") || strings.Contains(w.Body.String(), "Duplicate") {
		t.Fatalf("ad çakışması 409 ve ham SQL'siz beklenir: %d %s", w.Code, w.Body.String())
	}
	if n := opSay(t, db, `SELECT COUNT(*) FROM reseller_plans WHERE id=2 AND ad='Bayi M'`); n != 1 {
		t.Fatal("çakışan güncelleme paketi değiştirdi")
	}
}

func TestBayiPaketi_Sil_Denetim(t *testing.T) {
	sayi := func(t *testing.T, db *sql.DB, id int) int {
		return opSay(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM reseller_plans WHERE id=%d`, id))
	}
	t.Run("başarı", func(t *testing.T) {
		db := kaDB(t)
		h := bpRouter(&Handlers{DB: db})
		if w := kaAdmin(t).istek(h, "DELETE", "/reseller-plans/1", nil); w.Code != http.StatusOK {
			t.Fatalf("200 beklenir: %d %s", w.Code, w.Body.String())
		}
		if sayi(t, db, 1) != 0 {
			t.Error("paket silinmeliydi")
		}
		bpTek(t, db, "bayi_paketi.sil", "Bayi S", "", 1)
	})
	t.Run("DB hatası", func(t *testing.T) {
		db := kaDB(t)
		if _, err := db.Exec(`CREATE TRIGGER bp_sil_engel BEFORE DELETE ON reseller_plans FOR EACH ROW
			SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='silme engellendi'`); err != nil {
			t.Fatal(err)
		}
		h := bpRouter(&Handlers{DB: db})
		if w := kaAdmin(t).istek(h, "DELETE", "/reseller-plans/1", nil); w.Code != http.StatusInternalServerError {
			t.Fatalf("500 beklenir: %d %s", w.Code, w.Body.String())
		}
		if sayi(t, db, 1) != 1 {
			t.Error("silme düşünce paket kalmalı")
		}
		bpTek(t, db, "bayi_paketi.sil", "Bayi S", "", 0)
	})
	t.Run("bağlı bayi / olmayan paket / yetkisiz kayıtsız", func(t *testing.T) {
		db := kaDB(t)
		h := bpRouter(&Handlers{DB: db})
		if _, err := db.Exec(`INSERT INTO users (id, username, role, reseller_plan_id) VALUES (50,'bagli','reseller',1)`); err != nil {
			t.Fatal(err)
		}
		for _, c := range []struct {
			ad, yol string
			tok     kaJeton
			kod     int
		}{
			{"bağlı bayi var", "/reseller-plans/1", kaAdmin(t), http.StatusConflict},
			{"olmayan paket", "/reseller-plans/999", kaAdmin(t), http.StatusNotFound},
			{"bayi yetkisiz", "/reseller-plans/1", kaBayi(t, 10, "bayia"), http.StatusForbidden},
		} {
			if w := c.tok.istek(h, "DELETE", c.yol, nil); w.Code != c.kod {
				t.Errorf("%s: %d beklenir, alınan %d %s", c.ad, c.kod, w.Code, w.Body.String())
			}
		}
		if sayi(t, db, 1) != 1 {
			t.Error("bağlı bayisi olan paket silinmemeli")
		}
		if n := bpToplam(t, db); n != 0 {
			t.Errorf("etkisiz/reddedilen isteklerde denetim kaydı yazılmamalı: %d", n)
		}
	})
}
