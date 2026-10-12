package onyuzkapsam

// pma_parola_test.go — phpMyAdmin "panel kaydı bayat" akışı (2026-10-12, bir müşteri sunucusu):
// (1) sunucunun db_parola_uyusmuyor kodu ile arayüzün okuduğu kod AYNI; (2) her sahip
// (yönetici, bayi, hosting sahibi müşteri) doğrudan "yalnız panel kaydını güncelle"
// penceresine gider — uç DBSahipligiMusteri, müşteri kendi DB'sinde yetkili; (3) pencere
// yalniz_kayit bayrağını gönderir ve bu modda 6-karakter kuralı uygulanmaz; (4) yeni
// metinler tr/en.

import (
	"os"
	"strings"
	"testing"
)

func TestPMAParola_KodSunucuylaAyni(t *testing.T) {
	d := oku(t, "pages/DomainDatabaseDetailPage.tsx")
	for _, dosya := range []string{"../pma/pma.go", "../domains/handlers_dbpw.go"} {
		b, err := os.ReadFile(dosya)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), `"db_parola_uyusmuyor"`) {
			t.Errorf("%s db_parola_uyusmuyor kodunu üretmiyor", dosya)
		}
	}
	if !strings.Contains(d, "hataKodu(e) === 'db_parola_uyusmuyor'") {
		t.Error("detay sayfası db_parola_uyusmuyor kodunu okumuyor")
	}
}

func TestPMAParola_SayfaKaynakKilitleri(t *testing.T) {
	d := oku(t, "pages/DomainDatabaseDetailPage.tsx")
	m := oku(t, "pages/DomainDatabasesPage.tsx")
	for ad, c := range map[string]struct{ s, gerek string }{
		"uyuşmazlıkta pencereye yönlenir": {d, "hataKodu(e) === 'db_parola_uyusmuyor' && db.db_kullanici"},
		"pencere yalnız-kayıt açılır":     {d, "setPwYalnizKayit(true); setPwReset(true)"},
		"normal buton bayrağı sıfırlar":   {d, "setPwYalnizKayit(false); setPwReset(true)"},
		"pencereye bayrak geçer":          {d, "yalnizKayitIle={pwYalnizKayit}"},
		"bayrak gövdeye yazılır":          {m, "if (yalnizKayit) body.yalniz_kayit = true"},
		"asgari uzunluk moda bağlı":       {m, "const asgari = yalnizKayit ? 1 : 6"},
		"buton asgariye bağlı":            {m, "disabled={ozelPw.length < asgari}"},
	} {
		if !strings.Contains(c.s, c.gerek) {
			t.Errorf("%s: %q yok", ad, c.gerek)
		}
	}
	for _, c := range []struct{ s, tr string }{
		{m, "Yalnız panel kaydını güncelle (MySQL parolası değişmez)"},
		{m, "Panel Kaydını Güncelle — {0}"},
		{m, "Doğrula ve Kaydet"},
		{m, "Panel kaydı güncellendi"},
		{m, "Mevcut parolayı girin"},
		{d, "Veritabanı parolası uyuşmuyor"},
		{d, "Panel Kaydını Güncelle"},
	} {
		if n := strings.Count(c.s, `"`+c.tr+`"`); n < 2 {
			t.Errorf("%q hem kullanımda hem EN sözlüğünde olmalı (bulunan: %d)", c.tr, n)
		}
	}
	// Müşteri artık dışlanmaz (uç müşteriye açık): eski rol kapısı geri gelmemeli.
	if strings.Contains(d, "!musteriMi(rol)") {
		t.Error("müşteri yalnız-kayıt penceresinden dışlanıyor (uç DBSahipligiMusteri ile müşteriye açık)")
	}
}
