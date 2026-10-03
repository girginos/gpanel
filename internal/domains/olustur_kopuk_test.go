package domains

// olustur_kopuk_test.go — 2026-10-03 181 olayı: tarayıcı 30 sn'de bağlantıyı
// bıraktı (nginx 499), r.Context() iptal oldu, sağlama (kullanıcı/vhost/FPM)
// bitmişken DNS tohumu + SOA + zone "context canceled" ile düştü → YARIM alan adı.
//
// Sağlama SIRASINDA istek bağlamı iptal edilir (istemcinin gidişi); ardından
// gelen her adım (domains INSERT, DNS tohumu, zone yazımı) İPTAL EDİLMEMİŞ
// bağlamla koşmalı ve DB'ye gerçekten yazmalı. Kök ve bayi aynı yol.
//
// Yan etki kalkanı: saglama + sağlama sonrası dış adımlar (kaynaklimit/FTP/
// sertifika/zone) sahte; DNS tohumu sahte ama verilen bağlamla GERÇEK DB'ye
// yazar. Gerçek MariaDB gerekir; GOSP_TEST_GECICI_DSN yoksa ATLANIR.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"girginospanel/internal/provisioner"
)

type ok_kayit struct {
	mu                    sync.Mutex
	tohumIptal, zoneIptal []error
	tohumCagri, zoneCagri int
}

func ok_dikisler(t *testing.T, iptal func()) *ok_kayit {
	t.Helper()
	k := &ok_kayit{}
	eS, eG := saglama, saglamaGeriAl
	eK, eF, eSS, eT, eZ := olusturKaynakUygula, olusturFTP, olusturSertifikaSart, olusturDNSTohumla, olusturZoneYaz
	t.Cleanup(func() {
		saglama, saglamaGeriAl = eS, eG
		olusturKaynakUygula, olusturFTP, olusturSertifikaSart, olusturDNSTohumla, olusturZoneYaz = eK, eF, eSS, eT, eZ
	})
	saglama = func(alan, php string) (*provisioner.Result, error) {
		iptal() // istemci, sağlama sürerken bağlantıyı bırakır
		return &provisioner.Result{SistemKullanici: "c_kopuktest", WebRoot: "/nonexistent/kopuk-test"}, nil
	}
	saglamaGeriAl = func(alan, sk string) error {
		t.Errorf("geri alma beklenmezdi (%s)", alan)
		return nil
	}
	olusturKaynakUygula = func(context.Context, *sql.DB, int64) error { return nil }
	olusturFTP = func(*sql.DB, int64, string, string, int, int) error { return nil }
	olusturSertifikaSart = func(string) (bool, string) { return false, "" }
	olusturDNSTohumla = func(ctx context.Context, db *sql.DB, id int64, alan, ip string) (int, error) {
		k.mu.Lock()
		k.tohumCagri++
		k.tohumIptal = append(k.tohumIptal, ctx.Err())
		k.mu.Unlock()
		if _, err := db.ExecContext(ctx, `INSERT INTO dns_records(domain_id, ad, tip, deger) VALUES(?,?,?,?)`, id, "@", "A", ip); err != nil {
			return 0, err
		}
		return 1, nil
	}
	olusturZoneYaz = func(ctx context.Context, db *sql.DB, id int64) error {
		k.mu.Lock()
		k.zoneCagri++
		k.zoneIptal = append(k.zoneIptal, ctx.Err())
		k.mu.Unlock()
		return ctx.Err()
	}
	return k
}

func ok_db(t *testing.T) *sql.DB {
	t.Helper()
	db := aa_db(t)
	aa_exec(t, db, `ALTER TABLE domains ADD COLUMN php_surum VARCHAR(8) NOT NULL DEFAULT '8.3',
		ADD COLUMN ssl_aktif TINYINT NOT NULL DEFAULT 0, ADD COLUMN durum VARCHAR(8) NOT NULL DEFAULT 'aktif',
		ADD COLUMN ftp_host VARCHAR(253) NOT NULL DEFAULT '', ADD COLUMN ftp_user VARCHAR(64) NOT NULL DEFAULT '',
		ADD COLUMN db_host VARCHAR(64) NOT NULL DEFAULT 'localhost', ADD COLUMN db_user VARCHAR(64) NOT NULL DEFAULT '',
		ADD COLUMN db_adi VARCHAR(64) NOT NULL DEFAULT '', ADD COLUMN web_root VARCHAR(255) NOT NULL DEFAULT '',
		ADD COLUMN is_demo TINYINT NOT NULL DEFAULT 0`)
	aa_exec(t, db, `CREATE TABLE dns_records (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
		domain_id BIGINT UNSIGNED NOT NULL, ad VARCHAR(253) NOT NULL, tip VARCHAR(10) NOT NULL, deger TEXT NOT NULL)`)
	return db
}

func TestCreate_IstemciKoparsaSaglamaTamamlanir_KokVeBayi(t *testing.T) {
	db := ok_db(t)
	aa_panel(t)
	h := &Handlers{DB: db, IPv4: "203.0.113.10"}
	for _, c := range []struct{ rol, alan, tok string }{
		{"kök", "kopuk-kok.test", aa_yonetici(t)},
		{"bayi", "kopuk-bayi.test", aa_bayi(t, 2)},
	} {
		ctx, iptal := context.WithCancel(context.Background())
		k := ok_dikisler(t, iptal)
		b, _ := json.Marshal(map[string]any{"alan_adi": c.alan})
		req := httptest.NewRequest(http.MethodPost, "/domains", bytes.NewReader(b)).WithContext(ctx)
		req.Header.Set("Authorization", "Bearer "+c.tok)
		rec := httptest.NewRecorder()
		aa_router(h).ServeHTTP(rec, req)
		iptal()

		if req.Context().Err() == nil {
			t.Fatalf("%s: test kurgusu: istek bağlamı iptal edilmeliydi", c.rol)
		}
		if n := aa_sayi(t, db, `SELECT COUNT(*) FROM domains WHERE alan_adi=?`, c.alan); n != 1 {
			t.Fatalf("%s: istemci koptu diye domains satırı yazılmadı (n=%d) — kod %d %s", c.rol, n, rec.Code, rec.Body.String())
		}
		if k.tohumCagri != 1 || k.tohumIptal[0] != nil {
			t.Errorf("%s: DNS tohumu iptal edilmiş bağlamla çağrıldı: çağrı=%d hata=%v", c.rol, k.tohumCagri, k.tohumIptal)
		}
		if k.zoneCagri != 1 || k.zoneIptal[0] != nil {
			t.Errorf("%s: zone yazımı iptal edilmiş bağlamla çağrıldı: çağrı=%d hata=%v", c.rol, k.zoneCagri, k.zoneIptal)
		}
		if n := aa_sayi(t, db, `SELECT COUNT(*) FROM dns_records r JOIN domains d ON d.id=r.domain_id WHERE d.alan_adi=?`, c.alan); n != 1 {
			t.Errorf("%s: DNS kaydı yazılmadı (n=%d) — alan adı YARIM kaldı", c.rol, n)
		}
		if rec.Code != http.StatusCreated {
			t.Errorf("%s: 201 beklenirdi: %d %s", c.rol, rec.Code, rec.Body.String())
		}
	}
}
