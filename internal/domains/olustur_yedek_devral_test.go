package domains

// olustur_yedek_devral_test.go — POST /domains `yedekleri_devral` bayrağı (Y1): silinmiş aynı hesabın yetim yedekleri YALNIZ bu AÇIK bayrakla
// ve hosting BAŞARIYLA kurulduktan sonra devralınır; devralma hata verirse hosting oluşturma başarısız sayılmaz; yanıtta `devralinan_yedek`
// yalnız bayrak varken bulunur; denetim `hosting.yedek-devral`. Dış etkili adımlar sahte (olustur_kopuk_test.go düzeneği).
// Ayrıca A2: domain satırı yazılır yazılmaz kimliğe bağlı eski yedek kalıntıları ayrılır (olusturKalintiAyir).

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func yd_istek(t *testing.T, h *Handlers, tok string, govde map[string]any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(govde)
	req := httptest.NewRequest(http.MethodPost, "/domains", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	aa_router(h).ServeHTTP(rec, req)
	var m map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	return rec.Code, m
}

func TestCreate_YedekleriDevral_AcikBayrakliDenetimli(t *testing.T) {
	db := ok_db(t)
	aa_exec(t, db, `CREATE TABLE audit_log (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, ts TIMESTAMP NULL DEFAULT current_timestamp(), actor_user_id BIGINT UNSIGNED NULL,
		actor_username VARCHAR(64) NOT NULL DEFAULT '', adina_domain_id BIGINT UNSIGNED NULL, ip VARCHAR(45) NOT NULL DEFAULT '', action VARCHAR(64) NOT NULL,
		target VARCHAR(255) NOT NULL DEFAULT '', detail LONGTEXT NULL, ok TINYINT(1) NOT NULL DEFAULT 1, reseller_id BIGINT UNSIGNED NOT NULL DEFAULT 0)`)
	aa_panel(t)
	h := &Handlers{DB: db, IPv4: "203.0.113.10"}
	ok_dikisler(t, func() {})

	var sira []string
	var devralID, kalintiID int64
	var devralSatirVar bool
	devralN, devralErr := int64(0), error(nil)
	eskiD, eskiK := olusturYedekDevral, olusturKalintiAyir
	t.Cleanup(func() { olusturYedekDevral, olusturKalintiAyir = eskiD, eskiK })
	olusturKalintiAyir = func(_ context.Context, _ *sql.DB, id int64) int {
		sira = append(sira, "kalinti")
		kalintiID = id
		return 0
	}
	olusturYedekDevral = func(_ context.Context, d *sql.DB, id int64) (int64, error) {
		sira = append(sira, "devral")
		devralID = id
		devralSatirVar = aa_sayi(t, d, `SELECT COUNT(*) FROM domains WHERE id=?`, id) == 1
		return devralN, devralErr
	}
	denetim := func(alan string) (n int, ok int, detay string, kapsam int64) {
		n = int(aa_sayi(t, db, `SELECT COUNT(*) FROM audit_log WHERE action='hosting.yedek-devral' AND target=?`, alan))
		if n > 0 {
			_ = db.QueryRow(`SELECT ok, detail, reseller_id FROM audit_log WHERE action='hosting.yedek-devral' AND target=? ORDER BY id DESC LIMIT 1`, alan).Scan(&ok, &detay, &kapsam)
		}
		return
	}
	yonetici, bayi := aa_yonetici(t), aa_bayi(t, 2)
	sifirla := func() { sira, devralID, kalintiID, devralSatirVar = nil, 0, 0, false }

	t.Run("bayrak yok: devralma ÇAĞRILMAZ, yanıtta alan yok, denetim yok (kalıntı ayırma her zaman)", func(t *testing.T) {
		sifirla()
		devralN = 5
		kod, m := yd_istek(t, h, yonetici, map[string]any{"alan_adi": "bayraksiz.test"})
		if kod != http.StatusCreated {
			t.Fatalf("201 beklenirdi: %d %v", kod, m)
		}
		if _, var_ := m["devralinan_yedek"]; var_ {
			t.Errorf("bayrak yokken yanıtta devralinan_yedek OLMAMALI: %v", m)
		}
		if strings.Join(sira, ",") != "kalinti" {
			t.Errorf("yalnız kalıntı ayırma çağrılmalı: %v", sira)
		}
		if n, _, _, _ := denetim("bayraksiz.test"); n != 0 {
			t.Errorf("bayrak yokken devral denetimi olmamalı: %d", n)
		}
	})

	t.Run("bayrak false: aynı", func(t *testing.T) {
		sifirla()
		kod, m := yd_istek(t, h, yonetici, map[string]any{"alan_adi": "bayrakfalse.test", "yedekleri_devral": false})
		if _, var_ := m["devralinan_yedek"]; kod != http.StatusCreated || var_ || len(sira) != 1 {
			t.Errorf("false bayrak devralma yapmamalı: %d %v %v", kod, m, sira)
		}
	})

	t.Run("bayrak true: hosting kurulduktan SONRA devralınır, sayı yanıtta, denetim yazılır", func(t *testing.T) {
		sifirla()
		devralN, devralErr = 3, nil
		kod, m := yd_istek(t, h, yonetici, map[string]any{"alan_adi": "devral1.test", "yedekleri_devral": true})
		if kod != http.StatusCreated {
			t.Fatalf("201 beklenirdi: %d %v", kod, m)
		}
		if m["devralinan_yedek"] != float64(3) {
			t.Errorf("yanıtta devralinan_yedek=3 beklenirdi: %v", m)
		}
		if strings.Join(sira, ",") != "kalinti,devral" {
			t.Errorf("sıra: kalıntı ayırma (satır yazılır yazılmaz) → devralma: %v", sira)
		}
		var id int64
		_ = db.QueryRow(`SELECT id FROM domains WHERE alan_adi='devral1.test'`).Scan(&id)
		if id == 0 || devralID != id || kalintiID != id || !devralSatirVar {
			t.Errorf("devralma YENİ domainin kimliğiyle ve domains satırı yazıldıktan sonra çağrılmalı: id=%d devral=%d kalinti=%d satırVar=%v", id, devralID, kalintiID, devralSatirVar)
		}
		if _, ok := m["olusturulan_parolalar"]; !ok {
			t.Errorf("mevcut yanıt yapısı bozulmamalı (olusturulan_parolalar): %v", m)
		}
		if n, ok, detay, kapsam := denetim("devral1.test"); n != 1 || ok != 1 || kapsam != 0 || !strings.Contains(detay, "3 yedek devralındı") {
			t.Errorf("denetim: n=%d ok=%d kapsam=%d detay=%q", n, ok, kapsam, detay)
		}
	})

	t.Run("bayrak true ama eşleşen yetim yok: devralinan_yedek=0 yanıtta, gürültü denetimi yok", func(t *testing.T) {
		sifirla()
		devralN, devralErr = 0, nil
		kod, m := yd_istek(t, h, yonetici, map[string]any{"alan_adi": "devral0.test", "yedekleri_devral": true})
		if v, var_ := m["devralinan_yedek"]; kod != http.StatusCreated || !var_ || v != float64(0) {
			t.Errorf("devralinan_yedek=0 yanıtta olmalı (arayüz 'devir doğrulanamadı' uyarısı verebilsin): %d %v", kod, m)
		}
		if n, _, _, _ := denetim("devral0.test"); n != 0 {
			t.Errorf("0 devralmada denetim satırı yazılmamalı (WHMCS her hosting için bayrak gönderir): %d", n)
		}
	})

	t.Run("devralma HATA verirse hosting başarısız sayılmaz: 201, devralinan_yedek=0, denetim ok=0", func(t *testing.T) {
		sifirla()
		devralN, devralErr = 9, errors.New("kilitlenme")
		kod, m := yd_istek(t, h, yonetici, map[string]any{"alan_adi": "devralhata.test", "yedekleri_devral": true})
		if v, var_ := m["devralinan_yedek"]; kod != http.StatusCreated || !var_ || v != float64(0) {
			t.Fatalf("hata durumunda da 201 + devralinan_yedek=0: %d %v", kod, m)
		}
		if aa_sayi(t, db, `SELECT COUNT(*) FROM domains WHERE alan_adi='devralhata.test'`) != 1 {
			t.Error("hosting oluşturulmuş olmalı")
		}
		if n, ok, detay, _ := denetim("devralhata.test"); n != 1 || ok != 0 || strings.Contains(detay, "kilitlenme") {
			t.Errorf("hata denetimi ok=0 ve ham hata metni OLMADAN yazılmalı: n=%d ok=%d detay=%q", n, ok, detay)
		}
	})

	t.Run("bayi API/oturumu: denetim kapsamı bayi kimliği", func(t *testing.T) {
		sifirla()
		devralN, devralErr = 2, nil
		kod, m := yd_istek(t, h, bayi, map[string]any{"alan_adi": "devralbayi.test", "yedekleri_devral": true})
		if kod != http.StatusCreated || m["devralinan_yedek"] != float64(2) {
			t.Fatalf("bayi: %d %v", kod, m)
		}
		if n, ok, _, kapsam := denetim("devralbayi.test"); n != 1 || ok != 1 || kapsam != 2 {
			t.Errorf("bayi kapsamı (2): n=%d ok=%d kapsam=%d", n, ok, kapsam)
		}
	})

	t.Run("bayrak boolean değilse istek reddedilir, hiçbir şey kurulmaz", func(t *testing.T) {
		sifirla()
		kod, _ := yd_istek(t, h, yonetici, map[string]any{"alan_adi": "bozukbayrak.test", "yedekleri_devral": "true"})
		if kod != http.StatusBadRequest || aa_sayi(t, db, `SELECT COUNT(*) FROM domains WHERE alan_adi='bozukbayrak.test'`) != 0 || len(sira) != 0 {
			t.Errorf("bozuk bayrak 400 olmalı: %d %v", kod, sira)
		}
	})
}

// Ön yüz sözleşmesi (frontend/src/pages/DomainsPage.tsx): istek `yedekleri_devral`, yanıt `devralinan_yedek` (sayı).
func TestCreate_YedekleriDevral_OnYuzSozlesmesi(t *testing.T) {
	b, err := os.ReadFile("../../frontend/src/pages/DomainsPage.tsx")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, gerek := range []string{"devralinan_yedek?: number", "yedekleri_devral = true"} {
		if !strings.Contains(src, gerek) {
			t.Errorf("DomainsPage.tsx sözleşmesinde %q yok", gerek)
		}
	}
	for _, tag := range []string{`json:"yedekleri_devral,omitempty"`, `json:"devralinan_yedek,omitempty"`} {
		kaynak, _ := os.ReadFile("handlers.go")
		if !strings.Contains(string(kaynak), tag) {
			t.Errorf("handlers.go'da %s etiketi yok", tag)
		}
	}
}
