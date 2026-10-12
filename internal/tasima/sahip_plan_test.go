package tasima

// sahip_plan_test.go — kullanıcı bildirimi 2026-10-12 ("100 bayi = 100 hosting
// planı"): site taşımada kaynaktaki bayiler oluşturulurken (sahipleri taşı) bayiye
// HOSTING planı ("<bayi> Varsayılan") AÇILMAZ. Bayi paketi ("Taşınan-<login>",
// reseller_plans) kaynak limitlerini taşıdığı için oluşur — bayi kaydı paketsiz
// olamaz. Şema eski otomatik plan INSERT'ini kabul eder (test o davranışı gerçekten
// yakalar). Gerçek MariaDB; GOSP_TEST_GECICI_DSN yoksa ATLANIR.

import (
	"database/sql"
	"testing"
)

func TestSahipleriHazirla_BayiyeHostingPlaniAcmaz(t *testing.T) {
	db := g109DB(t)
	for _, q := range []string{
		`CREATE TABLE users (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, username VARCHAR(64) NOT NULL UNIQUE,
			password_hash VARCHAR(255) NOT NULL DEFAULT '', role ENUM('admin','reseller','user') NOT NULL DEFAULT 'user',
			full_name VARCHAR(255) NULL, status ENUM('active','suspended') NOT NULL DEFAULT 'active',
			max_domain INT NOT NULL DEFAULT 0, max_disk_mb BIGINT NOT NULL DEFAULT 0, max_trafik_mb BIGINT NOT NULL DEFAULT 0,
			reseller_plan_id BIGINT NOT NULL DEFAULT 0, asim_ilkesi VARCHAR(16) NOT NULL DEFAULT 'yok',
			asim_bildirim TINYINT NOT NULL DEFAULT 0, fazla_satis TINYINT NOT NULL DEFAULT 0)`,
		`CREATE TABLE reseller_plans (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, ad VARCHAR(64) NOT NULL UNIQUE,
			aciklama VARCHAR(255) NOT NULL DEFAULT '', max_domain INT NOT NULL DEFAULT 0, max_disk_mb BIGINT NOT NULL DEFAULT 0,
			max_trafik_mb BIGINT NOT NULL DEFAULT 0, mail_max_email INT NOT NULL DEFAULT 0, mail_saatlik_limit INT NOT NULL DEFAULT 0,
			mail_kutu_kota_mb INT NOT NULL DEFAULT 0, fiyat_kurus BIGINT NOT NULL DEFAULT 0, asim_ilkesi VARCHAR(16) NOT NULL DEFAULT 'yok',
			asim_bildirim TINYINT NOT NULL DEFAULT 0, fazla_satis TINYINT NOT NULL DEFAULT 0, varsayilan TINYINT NOT NULL DEFAULT 0)`,
		`CREATE TABLE customers (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, ad VARCHAR(255) NOT NULL DEFAULT '',
			eposta VARCHAR(255) NOT NULL DEFAULT '', durum VARCHAR(16) NOT NULL DEFAULT 'aktif', notlar TEXT NULL)`,
		// Eski otomatik plan INSERT'inin yazdığı tüm sütunlar.
		`CREATE TABLE service_plans (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, ad VARCHAR(64) NOT NULL UNIQUE,
			aciklama VARCHAR(255) NOT NULL DEFAULT '', disk_kota_mb INT NOT NULL DEFAULT 0, trafik_kota_mb INT NOT NULL DEFAULT 0,
			max_domain INT NOT NULL DEFAULT 0, max_db INT NOT NULL DEFAULT 0, max_email INT NOT NULL DEFAULT 0,
			saatlik_mail_limiti INT NOT NULL DEFAULT 0, mail_kutu_kota_mb INT NOT NULL DEFAULT 0, max_ftp INT NOT NULL DEFAULT 0,
			cpu_yuzde INT NOT NULL DEFAULT 100, ram_mb INT NOT NULL DEFAULT 512, max_process INT NOT NULL DEFAULT 50,
			inode_kota INT NOT NULL DEFAULT 50000, io_agirlik INT NOT NULL DEFAULT 100, mysql_max_baglanti INT NOT NULL DEFAULT 25,
			pm_max_children INT NOT NULL DEFAULT 0, php_surum VARCHAR(8) NOT NULL DEFAULT '8.3',
			fastcgi_cache TINYINT NOT NULL DEFAULT 0, client_max_body_mb INT NOT NULL DEFAULT 64, nginx_ek_direktifler TEXT NULL,
			waf_enabled TINYINT NOT NULL DEFAULT 0, waf_mode VARCHAR(10) NOT NULL DEFAULT 'on', waf_paranoia TINYINT NOT NULL DEFAULT 1,
			varsayilan TINYINT NOT NULL DEFAULT 0, reseller_id BIGINT NOT NULL DEFAULT 0, domain_id BIGINT UNSIGNED NULL)`,
		`CREATE TABLE audit_log (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, actor_user_id BIGINT UNSIGNED NULL,
			actor_username VARCHAR(64) NOT NULL DEFAULT '', ip VARCHAR(45) NOT NULL DEFAULT '', action VARCHAR(64) NOT NULL,
			target VARCHAR(255) NOT NULL DEFAULT '', detail TEXT NULL, ok TINYINT NOT NULL DEFAULT 1,
			reseller_id BIGINT UNSIGNED NOT NULL DEFAULT 0)`,
		`INSERT INTO service_plans (id, ad, disk_kota_mb, trafik_kota_mb, varsayilan) VALUES (1, 'Başlangıç', 1024, 5120, 1)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}
	say := func(q string) int {
		t.Helper()
		var n int
		if err := db.QueryRow(q).Scan(&n); err != nil {
			t.Fatalf("%v: %s", err, q)
		}
		return n
	}
	sahipler := []KaynakSahip{
		{Tip: "reseller", Login: "kaynakbayi", Ad: "Kaynak Bayi", MaxDomain: 10, DiskMB: 20000},
		{Tip: "reseller", Login: "ikincibayi", Ad: "İkinci Bayi", MaxDomain: 3, DiskMB: 5000},
		{Tip: "client", Login: "musteri1", Reseller: "kaynakbayi", Eposta: "m1@ornek.test"},
	}
	for tur := 1; tur <= 2; tur++ { // ikinci tur: idempotent (var olan bayi yeniden kullanılır)
		esle, _, err := sahipleriHazirla(t.Context(), db, sahipler, Kapsam{})
		if err != nil {
			t.Fatalf("tur %d: %v", tur, err)
		}
		if esle["kaynakbayi"].ResellerID <= 0 || esle["ikincibayi"].ResellerID <= 0 || esle["musteri1"].ResellerID != esle["kaynakbayi"].ResellerID {
			t.Fatalf("tur %d eşleme: %+v", tur, esle)
		}
		if n := say(`SELECT COUNT(*) FROM service_plans`); n != 1 {
			t.Errorf("tur %d: taşınan bayilere hosting planı açıldı (toplam %d, beklenen 1)", tur, n)
		}
		if n := say(`SELECT COUNT(*) FROM users WHERE role='reseller'`); n != 2 {
			t.Errorf("tur %d: bayi sayısı %d", tur, n)
		}
	}
	// Bayi paketi (kaynak limitleri) bayi kaydına bağlı; hosting planı değil.
	var paket sql.NullInt64
	var md int
	var dk int64
	if err := db.QueryRow(`SELECT u.reseller_plan_id, u.max_domain, u.max_disk_mb FROM users u
		JOIN reseller_plans rp ON rp.id=u.reseller_plan_id AND rp.ad='Taşınan-kaynakbayi' WHERE u.username='kaynakbayi'`).Scan(&paket, &md, &dk); err != nil {
		t.Fatalf("bayi paketi/kaydı: %v", err)
	}
	if md != 10 || dk != 20000 {
		t.Errorf("bayi kaydı limitleri: domain=%d disk=%d", md, dk)
	}
	if n := say(`SELECT COUNT(*) FROM audit_log WHERE action='plan.varsayilan_olustur'`); n != 0 {
		t.Errorf("plan.varsayilan_olustur denetimi: %d", n)
	}
}
