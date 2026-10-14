package bayikaynak

// havuz_oku_test.go — DB'den okunan havuz/üye verisinin güvenli yorumu.

import (
	"context"
	"testing"
)

// Bozuk (negatif / sınır dışı) DB değeri ASLA sıfırdan küçük tavan üretmez: sınırsız sayılır.
func TestHavuzlariOku_BozukDegerSinirsizSayilir(t *testing.T) {
	db := geciciDB(t)
	bayiEkle(t, db, 5, "bozuk", -2, -5, -7, -3)
	bayiEkle(t, db, 6, "asiri", 1.5, 100, 200, 99999)
	bayiEkle(t, db, 7, "saglam", 2, 1024, 5000, 300)
	hv, err := HavuzlariOku(context.Background(), db, 0)
	if err != nil {
		t.Fatal(err)
	}
	if h := hv[5]; h.CPUCekirdek != 0 || h.RAMMB != 0 || h.DiskMB != 0 || h.IOAgirlik != 0 || h.DilimAktif() || h.DiskAktif() {
		t.Errorf("negatif değerler sınırsız sayılmadı: %+v", h)
	}
	if h := hv[6]; h.IOAgirlik != 0 || h.CPUCekirdek != 1.5 || h.RAMMB != 100 {
		t.Errorf("sınır dışı G/Ç ağırlığı sıfırlanmalı, diğerleri korunmalı: %+v", h)
	}
	if h := hv[7]; h.CPUCekirdek != 2 || h.RAMMB != 1024 || h.DiskMB != 5000 || h.IOAgirlik != 300 {
		t.Errorf("sağlam değerler bozuldu: %+v", h)
	}
	// Tek bayi sorgusu yalnız onu döndürür; yönetici/müşteri kayıtları bayi sayılmaz.
	if one, _ := HavuzlariOku(context.Background(), db, 7); len(one) != 1 || one[7].CPUCekirdek != 2 {
		t.Errorf("tek bayi sorgusu: %+v", one)
	}
	if _, ok := hv[100]; ok {
		t.Error("yönetici hesabı havuz listesinde")
	}
}

// Yönetici/doğrudan müşteri hostingleri (reseller_id 0) bayi havuzunun ÜYESİ DEĞİLDİR; geçersiz
// kullanıcı adlı kayıtlar da atlanır.
func TestUyeleriOku_YalnizBayiHostingleri(t *testing.T) {
	db := geciciDB(t)
	bayiEkle(t, db, 5, "bayi5", 1, 512, 1000, 0)
	hostingEkle(t, db, 1, "bayi.test", "c_bayi_test", 5, 10)
	hostingEkle(t, db, 2, "kok.test", "c_kok_test", 0, 10)
	hostingEkle(t, db, 3, "yabanci.test", "www-data", 5, 10)
	hostingEkle(t, db, 4, "bozuk.test", "c_BOZUK-ad", 5, 10)
	u, err := UyeleriOku(context.Background(), db, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(u) != 1 || u[0].SK != "c_bayi_test" || u[0].BayiID != 5 || u[0].Alan != "bayi.test" {
		t.Fatalf("yalnız bayinin geçerli hostingi üye olmalı: %+v", u)
	}
	if one, _ := UyeleriOku(context.Background(), db, 5, 1); len(one) != 1 {
		t.Errorf("bayi+domain süzgeci: %+v", one)
	}
	if none, _ := UyeleriOku(context.Background(), db, 5, 2); len(none) != 0 {
		t.Errorf("başka bayinin/kökün hostingi bu bayinin üyesi döndü: %+v", none)
	}
}
