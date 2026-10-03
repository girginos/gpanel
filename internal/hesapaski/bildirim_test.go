package hesapaski

// bildirim_test.go — askı bildirimi (D3b): YALNIZ durum gerçekten değişince,
// domain sahibi + müşteri hedefli; tetikleyen rolün kopyası okunmuş.

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func hb_db(t *testing.T) *sql.DB {
	t.Helper()
	db := ha_db(t)
	for _, q := range []string{
		`CREATE TABLE cp_bildirim (id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
			seviye VARCHAR(16) NOT NULL DEFAULT 'bilgi', kategori VARCHAR(32) NOT NULL DEFAULT '',
			baslik VARCHAR(200) NOT NULL, mesaj TEXT NOT NULL, domain_id BIGINT UNSIGNED NULL,
			reseller_id BIGINT UNSIGNED NULL, ref_tur VARCHAR(24) NOT NULL DEFAULT '', ref_id BIGINT NOT NULL DEFAULT 0,
			okundu TINYINT(1) NOT NULL DEFAULT 0, created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE cp_bildirim_okundu (bildirim_id BIGINT UNSIGNED NOT NULL, okuyan_tur VARCHAR(8) NOT NULL,
			okuyan_id BIGINT UNSIGNED NOT NULL, PRIMARY KEY (bildirim_id, okuyan_tur, okuyan_id))`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("şema: %v", err)
		}
	}
	return db
}

type hb_satir struct {
	baslik string
	rid    sql.NullInt64
	okuyan string
}

func hb_bildirimler(t *testing.T, db *sql.DB) []hb_satir {
	t.Helper()
	rows, err := db.Query(`SELECT b.baslik, b.reseller_id, COALESCE(GROUP_CONCAT(CONCAT(k.okuyan_tur,':',k.okuyan_id) ORDER BY k.okuyan_tur),'')
		FROM cp_bildirim b LEFT JOIN cp_bildirim_okundu k ON k.bildirim_id=b.id
		WHERE b.kategori='aski' AND b.domain_id=1 GROUP BY b.id ORDER BY b.id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []hb_satir
	for rows.Next() {
		var s hb_satir
		if err := rows.Scan(&s.baslik, &s.rid, &s.okuyan); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func TestAskiBildirimi_YalnizDurumDegisince(t *testing.T) {
	db := hb_db(t)
	k := &ha_kayit{db: db}
	a := k.adimlar(t)
	ctx := context.Background()

	// Kök askıya alır → bayi 7 (sahip) okunmamış + müşteri okunmamış; yönetici
	// bu domainin görüntüleyeni değil (bayi sahipli).
	if _, err := a.Uygula(ctx, db, 1, true, KaynakKok); err != nil {
		t.Fatal(err)
	}
	l := hb_bildirimler(t, db)
	if len(l) != 1 || l[0].baslik != "Hesap askıya alındı" || !l[0].rid.Valid || l[0].rid.Int64 != 7 || l[0].okuyan != "" {
		t.Fatalf("kök askısı: tek okunmamış bildirim (sahip bayi 7) beklenir: %+v", l)
	}
	// İdempotent tekrar: durum değişmedi → bildirim yok.
	if _, err := a.Uygula(ctx, db, 1, true, KaynakKok); err != nil {
		t.Fatal(err)
	}
	if l := hb_bildirimler(t, db); len(l) != 1 {
		t.Fatalf("tekrar askı yeni bildirim üretmemeli: %+v", l)
	}
	// Kökün askısını bayi açamaz → reddedildi, bildirim yok.
	if _, err := a.Uygula(ctx, db, 1, false, KaynakBayiElle); !errors.Is(err, ErrKokAskisi) {
		t.Fatalf("bayi kökün askısını açamamalı: %v", err)
	}
	if l := hb_bildirimler(t, db); len(l) != 1 {
		t.Fatalf("reddedilen işlem bildirim üretmemeli: %+v", l)
	}
	// Kök açar → "yeniden etkinleştirildi".
	if _, err := a.Uygula(ctx, db, 1, false, KaynakKok); err != nil {
		t.Fatal(err)
	}
	// Bayi elle askıya alır → bayinin kendi kopyası okunmuş, müşteriye okunmamış.
	if _, err := a.Uygula(ctx, db, 1, true, KaynakBayiElle); err != nil {
		t.Fatal(err)
	}
	l = hb_bildirimler(t, db)
	if len(l) != 3 || l[1].baslik != "Hesap yeniden etkinleştirildi" || l[2].okuyan != "bayi:7" {
		t.Fatalf("açılış + bayi askısı (bayi kopyası okunmuş) beklenir: %+v", l)
	}
}

func TestAskiBildirimi_GeriAlinanIslemBildirimUretmez(t *testing.T) {
	db := hb_db(t)
	k := &ha_kayit{db: db, hata: map[string]error{"ana": errors.New("nginx -t başarısız")}}
	if _, err := k.adimlar(t).Uygula(context.Background(), db, 1, true, KaynakKok); err == nil {
		t.Fatal("ana vhost hatası dönmeli")
	}
	if l := hb_bildirimler(t, db); len(l) != 0 {
		t.Fatalf("DB geri alındı → bildirim olmamalı: %+v", l)
	}
}
