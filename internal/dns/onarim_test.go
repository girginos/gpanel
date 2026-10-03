package dns

// onarim_test.go — EksikZoneOnar: istemci kopunca DNS'i hiç tohumlanmamış kalan
// (kayıt + SOA yok) aktif alan adları açılışta tohumlanır; kaydını bilerek
// silen (SOA'sı duran), demo ve pasif alan adlarına dokunulmaz; ikinci koşu
// hiçbir şey yapmaz. Tohum GERÇEK SeedDefaults (DB); zone yazımı sahte
// (/var/named ve named'e dokunulmaz). GOSP_TEST_GECICI_DSN yoksa ATLANIR.

import (
	"context"
	"database/sql"
	"testing"
)

func TestEksikZoneOnar_YalnizHicTohumlanmamislar_Idempotent(t *testing.T) {
	db := dk_db(t)
	dk_goc(t, db, "0007_dns.sql")
	dk_exec(t, db, `ALTER TABLE domains ADD COLUMN durum VARCHAR(8) NOT NULL DEFAULT 'aktif',
		ADD COLUMN is_demo TINYINT NOT NULL DEFAULT 0, ADD COLUMN ipv4 VARCHAR(45) NOT NULL DEFAULT ''`)
	dk_exec(t, db, `UPDATE dns_template_meta SET dkim_aktif=0`) // DKIM anahtarı/opendkim yan etkisi yok
	dk_exec(t, db, `UPDATE domains SET ipv4='198.51.100.7'`)
	// 10 kokdomain.test : YARIM (kayıt yok, SOA yok)         → onarılır
	// 11 bayi9.test     : kaydı var                          → dokunulmaz
	// 12 bayi7.test     : kaydını silmiş, SOA duruyor         → dokunulmaz
	// 13 demo.test      : demo, yarım                         → dokunulmaz
	// 14 pasif.test     : pasif, yarım                        → dokunulmaz
	// 15 ipsiz.test     : YARIM, ipv4 boş                     → varsayılan IP ile onarılır
	dk_exec(t, db, `INSERT INTO domains (id, alan_adi, reseller_id, durum, is_demo, ipv4) VALUES
		(13,'demo.test',0,'aktif',1,'198.51.100.7'), (14,'pasif.test',0,'pasif',0,'198.51.100.7'),
		(15,'ipsiz.test',0,'aktif',0,'')`)
	dk_exec(t, db, `INSERT INTO dns_records (domain_id, ad, tip, deger, ttl, oncelik, aktif) VALUES (11,'@','A','198.51.100.9',3600,0,1)`)
	dk_exec(t, db, `INSERT INTO dns_soa (domain_id, primary_ns, hostmaster, refresh, retry, expire, minimum, ttl)
		VALUES (12,'ns1.bayi7.test.','hostmaster.bayi7.test.',3600,900,1209600,3600,3600)`)

	var zone []int64
	eski := onarimZoneYaz
	onarimZoneYaz = func(_ context.Context, _ *sql.DB, id int64) error { zone = append(zone, id); return nil }
	t.Cleanup(func() { onarimZoneYaz = eski })

	n, err := EksikZoneOnar(context.Background(), db, "203.0.113.1")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || len(zone) != 2 || zone[0] != 10 || zone[1] != 15 {
		t.Fatalf("yalnız 10 ve 15 onarılmalıydı: n=%d zone=%v", n, zone)
	}
	for id, bek := range map[int64]int64{10: 1, 15: 1, 11: 0, 12: 1, 13: 0, 14: 0} {
		if got := dk_sayi(t, db, `SELECT COUNT(*) FROM dns_soa WHERE domain_id=?`, id); got != bek {
			t.Errorf("domain %d SOA sayısı %d, beklenen %d", id, got, bek)
		}
	}
	if got := dk_sayi(t, db, `SELECT COUNT(*) FROM dns_records WHERE domain_id=10 AND ad='@' AND tip='A' AND deger='198.51.100.7'`); got != 1 {
		t.Errorf("10: alan adının kendi IP'siyle @ A kaydı bekleniyordu (%d)", got)
	}
	if got := dk_sayi(t, db, `SELECT COUNT(*) FROM dns_records WHERE domain_id=15 AND deger='203.0.113.1'`); got == 0 {
		t.Error("15: ipv4 boşken varsayılan IP kullanılmalıydı")
	}
	for _, id := range []int64{11, 12, 13, 14} {
		bek := int64(0)
		if id == 11 {
			bek = 1
		}
		if got := dk_sayi(t, db, `SELECT COUNT(*) FROM dns_records WHERE domain_id=?`, id); got != bek {
			t.Errorf("domain %d kayıtlarına dokunuldu: %d (beklenen %d)", id, got, bek)
		}
	}
	once := dk_sayi(t, db, `SELECT COUNT(*) FROM dns_records`)

	// İkinci koşu: hiçbir şey yapmaz.
	zone = nil
	n, err = EksikZoneOnar(context.Background(), db, "203.0.113.1")
	if err != nil || n != 0 || len(zone) != 0 {
		t.Fatalf("ikinci koşu boş olmalı: n=%d err=%v zone=%v", n, err, zone)
	}
	if sonra := dk_sayi(t, db, `SELECT COUNT(*) FROM dns_records`); sonra != once {
		t.Errorf("ikinci koşu kayıt ekledi: %d → %d", once, sonra)
	}
}
