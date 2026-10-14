package domains

// php_kurulu_test.go — canlı API doğrulamasında (VM, 2026-10-13): kurulu olmayan (ama
// desteklenen) sürüm 500 + ham iç ileti dönüyordu; per-tenant FPM'de hosting sürümü
// değişince alt alan kayıtları eski sürümde kalıyordu.

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"girginospanel/internal/provisioner"
)

func TestSetPHP_KuruluDegil409_YanEtkisiz(t *testing.T) {
	db := dnp_db(t)
	f := dnp_phpDikis(t)
	f.tenantAktif = true
	f.tenantHata = fmt.Errorf("%w (8.6): php-fpm binary yok: /opt/remi/php86/root/usr/sbin/php-fpm", provisioner.ErrPHPKuruluDegil)
	h := &Handlers{DB: db}
	kod, govde := dnp_istek(t, h, http.MethodPut, "/domains/20/php", aa_yonetici(t), `{"php_surum":"8.6"}`)
	if kod != http.StatusConflict || !strings.Contains(govde, "kurulu değil") || strings.Contains(govde, "/opt/remi") {
		t.Errorf("409 + açık ileti (iç yol olmadan) beklenirdi: %d %s", kod, govde)
	}
	if s := dnp_phpSurumu(t, db, 20); s != "8.2" {
		t.Errorf("php_surum değişti: %q", s)
	}
	if n := dnp_kayitSayisi(t, db); n != 0 {
		t.Errorf("hiçbir şeye dokunulmadı, denetim kaydı yazılmamalı: %d", n)
	}
}

func TestSetPHP_TenantFPM_AltAlanKayitlariEsitlenir(t *testing.T) {
	db := dnp_db(t)
	// aa_db: sub.bayi2.test (domain 20) + blog.kokdomain.test (domain 10); domain 20'ye ikinci alt alan.
	aa_exec(t, db, `INSERT INTO subdomanlar (domain_id, alt_ad, tam_ad) VALUES (20, 'iki', 'iki.bayi2.test')`)
	aa_exec(t, db, `UPDATE subdomanlar SET php_surum='8.2'`)
	sayi := func(sorgu string) int {
		var n int
		if err := db.QueryRow(sorgu).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	f := dnp_phpDikis(t)
	f.tenantAktif = true
	h := &Handlers{DB: db}
	if kod, govde := dnp_istek(t, h, http.MethodPut, "/domains/20/php", aa_yonetici(t), `{"php_surum":"8.4"}`); kod != http.StatusOK {
		t.Fatalf("200 beklenirdi: %d %s", kod, govde)
	}
	if n := sayi(`SELECT COUNT(*) FROM subdomanlar WHERE domain_id=20 AND php_surum='8.4'`); n != 2 {
		t.Errorf("per-tenant FPM'de alt alan kayıtları hosting sürümüne eşitlenmeli: %d/2", n)
	}
	if n := sayi(`SELECT COUNT(*) FROM subdomanlar WHERE domain_id=10 AND php_surum='8.2'`); n != 1 {
		t.Error("başka hostingin alt alanı değişti")
	}
	// Paylaşılan havuzda alt alanın kendi havuzu olabilir: kayıtlarına dokunulmaz.
	f.tenantAktif = false
	if kod, govde := dnp_istek(t, h, http.MethodPut, "/domains/20/php", aa_yonetici(t), `{"php_surum":"8.3"}`); kod != http.StatusOK {
		t.Fatalf("200 beklenirdi: %d %s", kod, govde)
	}
	if n := sayi(`SELECT COUNT(*) FROM subdomanlar WHERE domain_id=20 AND php_surum='8.4'`); n != 2 {
		t.Error("paylaşılan havuzda alt alan kayıtları değişmemeli")
	}
}
