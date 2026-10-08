package uzak

// yetim_duzeltme_test.go — doğrulama turu D8: YerelYokOzeti yetim satırları saymaz; IsListe silinmiş hostingin işinde
// alan adını yetim yedek satırından (backups.alan_adi, 0217) tamamlar ve o kolon olmayan şemada da çalışır.

import (
	"context"
	"testing"
)

func TestYerelYokOzeti_YetimSatirlariSaymaz(t *testing.T) {
	o := yeniOrtam(t)
	canli := o.yedek(1, "c_a", "20260901-020000", sahteIcerik(100, 1), "oto", nil)
	yetim := o.yedek(2, "c_b", "20260901-020000", sahteIcerik(700, 2), "oto", nil)
	if _, err := o.db.Exec(`UPDATE backups SET yerel_yok=NOW() WHERE id IN (?,?)`, canli, yetim); err != nil {
		t.Fatal(err)
	}
	o.yetimYap(yetim)
	adet, boyut, err := YerelYokOzeti(context.Background(), o.db)
	if err != nil || adet != 1 || boyut != 100 {
		t.Errorf("yalnız canlı domainin yalnız-Drive yedeği sayılmalı (1 adet, 100 B): %d adet %d B %v", adet, boyut, err)
	}
}

func TestIsListe_SilinmisHostingAlanAdiYetimSatirdanTamamlanir(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(nil)
	ctx := context.Background()
	canli := o.yedek(1, "c_canli", "20260901-020000", sahteIcerik(10, 1), "oto", nil)
	silinen := o.yedek(2, "c_silinen", "20260901-020000", sahteIcerik(10, 2), "oto", nil)
	o.kuyrukla(canli)
	o.kuyrukla(silinen)
	// 0217 sonrası şema: kolon yok → eklenir; hosting silinir, satır yetim olur ve alan adını taşır
	if _, err := o.db.Exec(`ALTER TABLE backups ADD COLUMN alan_adi VARCHAR(253) NOT NULL DEFAULT ''`); err != nil {
		t.Fatal(err)
	}
	o.yetimYap(silinen)
	if _, err := o.db.Exec(`UPDATE backups SET alan_adi='silinmis.example' WHERE id=?`, silinen); err != nil {
		t.Fatal(err)
	}
	if _, err := o.db.Exec(`DELETE FROM domains WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	adlar := func() map[string]string {
		liste, _, err := IsListe(ctx, o.db, 1, 50, "")
		if err != nil {
			t.Fatalf("IsListe: %v", err)
		}
		out := map[string]string{}
		for _, s := range liste {
			out[s.YedekDosya] = s.Domain
		}
		return out
	}
	m := adlar()
	if m["c_silinen-20260901-020000.tar.gz"] != "silinmis.example" {
		t.Errorf("silinmiş hostingin işi alan adını yetim yedek satırından almalı: %v", m)
	}
	if m["c_canli-20260901-020000.tar.gz"] != "c_canli.example" {
		t.Errorf("canlı hostingin işi alan adını domains'ten almalı: %v", m)
	}
	// alan_adi kolonu olmayan şemada (0217 uygulanmamış) iş listesi YİNE açılır (alan adı boş)
	if _, err := o.db.Exec(`ALTER TABLE backups DROP COLUMN alan_adi`); err != nil {
		t.Fatal(err)
	}
	m = adlar()
	if len(m) != 2 || m["c_silinen-20260901-020000.tar.gz"] != "" || m["c_canli-20260901-020000.tar.gz"] != "c_canli.example" {
		t.Errorf("kolon yokken liste bozulmamalı: %v", m)
	}
}
