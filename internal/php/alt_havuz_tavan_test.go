package php

import (
	"context"
	"reflect"
	"testing"
)

func TestHesapHavuzDagit(t *testing.T) {
	for _, c := range []struct {
		ad     string
		butce  int
		istek  []int
		beklen []int
	}{
		{"tek havuz bütçeye sığar", 6, []int{6}, []int{6}},
		{"ilk havuz bütçeyi yer, sonrakiler tabana 1", 6, []int{6, 6, 6}, []int{6, 1, 1}},
		{"bütçe paylaşılır", 8, []int{3, 3, 3}, []int{3, 3, 2}},
		{"bütçe bitince taban 1 (reddedilmez)", 2, []int{2, 2, 2, 2}, []int{2, 1, 1, 1}},
		{"sıfır/negatif istek de en az 1", 4, []int{0, -3}, []int{1, 1}},
		{"küçük istek bütçeyi tüketmez", 10, []int{2, 2, 20}, []int{2, 2, 6}},
		{"boş", 5, nil, []int{}},
	} {
		if got := hesapHavuzDagit(c.butce, c.istek); !reflect.DeepEqual(got, c.beklen) {
			t.Errorf("%s: dağıtım %v, beklenen %v", c.ad, got, c.beklen)
		}
	}
}

// GERÇEK DB: hesabın alt alan havuzlarının TOPLAMI plan bütçesini aşmaz; yeni (yüksek
// id'li) havuz kısılır, eskiler değişmez, sıra bağımsız, başka hesap etkilenmez.
func TestAltHavuz_HesapBazliToplamTavan_DB(t *testing.T) {
	db := ksdb(t)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO domains (id, alan_adi, sistem_kullanici, plan_id) VALUES (43, 'baska.test', 'c_baska', 1)`,
		`INSERT INTO subdomanlar VALUES (6, 42), (7, 42), (9, 43)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []int64{6, 7, 9} {
		if err := SaveSub(ctx, db, id, Defaults()); err != nil {
			t.Fatal(err)
		}
	}
	al := func(sk string, id int64) Settings {
		t.Helper()
		s, err := altHavuzAyarlari(ctx, db, sk, id)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	// Plan: 6 işçi. c_kok alt alanları 5,6,7 → 6 + 1 + 1.
	// Sıra bağımsız: önce 7, sonra 6, sonra 5 hesaplansa da aynı.
	s7, s6, s5 := al("c_kok", 7), al("c_kok", 6), al("c_kok", 5)
	if s5.PMMaxChildren != 6 || s6.PMMaxChildren != 1 || s7.PMMaxChildren != 1 {
		t.Fatalf("5/6/7 = %d/%d/%d, beklenen 6/1/1", s5.PMMaxChildren, s6.PMMaxChildren, s7.PMMaxChildren)
	}
	// Kısılan havuzun yedekleri yeniden türetilir (start/min/max_spare ≤ max_children).
	if s6.PMStartServers != 1 || s6.PMMinSpareServers != 1 || s6.PMMaxSpareServers != 1 {
		t.Errorf("kısılan havuz yedekleri: %+v", s6)
	}
	// Toplam ≤ bütçe + (bütçe bitince taban 1'ler): 6 + 1 + 1.
	// Başka hesap kendi bütçesini alır.
	if s9 := al("c_baska", 9); s9.PMMaxChildren != 6 {
		t.Errorf("başka hesabın havuzu etkilenmemeli: %d", s9.PMMaxChildren)
	}
	// Alt alan silinince (kayıt gider) sıradaki bütçeyi geri alır.
	if _, err := db.Exec(`DELETE FROM subdomain_php_settings WHERE subdomain_id=5`); err != nil {
		t.Fatal(err)
	}
	if s6 := al("c_kok", 6); s6.PMMaxChildren != 6 {
		t.Errorf("ilk havuz kalkınca 6. alt alan bütçeyi almalı: %d", s6.PMMaxChildren)
	}
	// DB'deki kayıt DEĞİŞMEZ (yalnız render kısılır).
	if kayit, _ := GetSub(ctx, db, 7); kayit.PMMaxChildren != Defaults().PMMaxChildren {
		t.Errorf("kayıt değişmemeli: %d", kayit.PMMaxChildren)
	}
	// Satırı olmayan (ilk kayıt) havuz da bütçeye sayılır: yeni id 8, 6/7'nin ardında.
	if _, err := db.Exec(`INSERT INTO subdomanlar VALUES (8, 42)`); err != nil {
		t.Fatal(err)
	}
	if s8 := al("c_kok", 8); s8.PMMaxChildren != 1 {
		t.Errorf("yeni havuz (id 8) kısılmalı: %d", s8.PMMaxChildren)
	}
}
