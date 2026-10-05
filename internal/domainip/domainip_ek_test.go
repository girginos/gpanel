package domainip

// domainip_ek_test.go — IP değişimi hesabın EK alan adı bölgelerindeki A kayıtlarını da
// günceller (yalnız değeri eski IP olanlar); ana bölge zone hatasında hepsi geri alınır;
// başka hesabın ek kayıtlarına dokunulmaz; 0160 yokken davranış değişmez.

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
)

func di_ekSema(t *testing.T, db *sql.DB) {
	t.Helper()
	body, err := os.ReadFile("../../migrations/0160_ek_alan_adlari.sql")
	if err != nil {
		t.Fatal(err)
	}
	var temiz []string
	for _, s := range strings.Split(string(body), "\n") {
		if st := strings.TrimSpace(s); st != "" && !strings.HasPrefix(st, "--") {
			temiz = append(temiz, s)
		}
	}
	for _, ifade := range strings.Split(strings.Join(temiz, "\n"), ";") {
		if s := strings.TrimSpace(ifade); s != "" {
			if _, err := db.Exec(s); err != nil {
				t.Fatalf("0160: %v", err)
			}
		}
	}
	for _, q := range []string{
		`INSERT INTO ek_alan_adlari (id, domain_id, alan_adi) VALUES (1, 11, 'ek-b7.org'), (2, 10, 'ek-kok.org')`,
		`INSERT INTO ek_dns_kayitlari (ek_alan_id, ad, tip, deger) VALUES
			(1,'@','A','` + ipBir + `'), (1,'www','A','` + ipBir + `'), (1,'disari','A','` + ipElle + `'),
			(1,'@','NS','ns1.ek-b7.org'), (2,'@','A','` + ipBir + `')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDomainIPDegistir_EkAlanBolgeleri(t *testing.T) {
	db, z := di_kur(t)
	di_ekSema(t, db)
	var yazilan []int64
	eski := ekZoneYazFn
	ekZoneYazFn = func(_ context.Context, _ *sql.DB, id int64) error { yazilan = append(yazilan, id); return nil }
	t.Cleanup(func() { ekZoneYazFn = eski })

	s, err := DomainIPDegistir(context.Background(), db, 11, ipIki)
	if err != nil || !s.Degisti || s.EkDNSKayit != 2 {
		t.Fatalf("sonuç: %+v err=%v", s, err)
	}
	if got := di_str(t, db, `SELECT GROUP_CONCAT(deger ORDER BY id) FROM ek_dns_kayitlari WHERE ek_alan_id=1 AND tip='A'`); got != ipIki+","+ipIki+","+ipElle {
		t.Fatalf("ek A kayıtları (elle yöneltilen korunmalı): %s", got)
	}
	if got := di_str(t, db, `SELECT deger FROM ek_dns_kayitlari WHERE ek_alan_id=1 AND tip='NS'`); got != "ns1.ek-b7.org" {
		t.Fatalf("NS değişmemeli: %s", got)
	}
	if got := di_str(t, db, `SELECT deger FROM ek_dns_kayitlari WHERE ek_alan_id=2`); got != ipBir {
		t.Fatalf("BAŞKA hesabın ek kaydı değişti: %s", got)
	}
	if len(yazilan) != 1 || yazilan[0] != 1 {
		t.Fatalf("yalnız değişen ek bölge yazılmalı: %v", yazilan)
	}
	if z.cagri != 1 {
		t.Fatalf("ana zone 1 kez: %d", z.cagri)
	}
	// İdempotent: ikinci çağrı no-op.
	if s, err := DomainIPDegistir(context.Background(), db, 11, ipIki); err != nil || s.Degisti || len(yazilan) != 1 {
		t.Fatalf("ikinci çağrı no-op olmalı: %+v err=%v yazilan=%v", s, err, yazilan)
	}
}

func TestDomainIPDegistir_EkAlan_AnaZoneHatasindaGeriAlinir(t *testing.T) {
	db, z := di_kur(t)
	di_ekSema(t, db)
	eski := ekZoneYazFn
	ekZoneYazFn = func(_ context.Context, _ *sql.DB, _ int64) error { return nil }
	t.Cleanup(func() { ekZoneYazFn = eski })
	z.hata = errors.New("named-checkzone: bozuk")
	if _, err := DomainIPDegistir(context.Background(), db, 11, ipIki); err == nil {
		t.Fatal("geri alma hatası beklenir")
	}
	if got := di_str(t, db, `SELECT GROUP_CONCAT(deger ORDER BY id) FROM ek_dns_kayitlari WHERE ek_alan_id=1 AND tip='A'`); got != ipBir+","+ipBir+","+ipElle {
		t.Fatalf("ek kayıtlar geri alınmadı: %s", got)
	}
}

func TestDomainIPDegistir_EkAlanTablosuYoksaDegismez(t *testing.T) {
	db, _ := di_kur(t) // 0160 YOK
	s, err := DomainIPDegistir(context.Background(), db, 11, ipIki)
	if err != nil || !s.Degisti || s.EkDNSKayit != 0 || s.DNSKayit != 2 {
		t.Fatalf("0160 yokken davranış değişmemeli: %+v err=%v", s, err)
	}
}
