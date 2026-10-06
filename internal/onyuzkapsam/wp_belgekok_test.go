package onyuzkapsam

// wp_belgekok_test.go — WordPress sihirbazı belge kökü uyuşmazlığı: (1) sunucunun makine okunur
// kodları ile arayüzün okuduğu kodlar AYNI; (2) 409 (kurulum başlatılmadı) ve kurulum-sonrası
// uyarısı (kuruldu ama site sunulmuyor) gösterilir; (3) mesajlar tr/en, yollar {0}/{1}
// yer tutucusuyla (sunucunun ev-göreli yolları), mutlak yol arayüzde üretilmez.

import (
	"os"
	"strings"
	"testing"
)

func TestWPBelgeKok_KodlarSunucuylaAyni(t *testing.T) {
	s := oku(t, "pages/DomainWordPressPage.tsx")
	b, err := os.ReadFile("../wordpress/wordpress.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, kod := range []string{"belgekok_uyusmuyor", "belgekok_sunulmuyor"} {
		if !strings.Contains(string(b), `"`+kod+`"`) {
			t.Errorf("sunucuda %s kodu yok", kod)
		}
		if !strings.Contains(s, "'"+kod+"'") {
			t.Errorf("arayüz %s kodunu okumuyor", kod)
		}
	}
}

func TestWPBelgeKok_SayfaKaynakKilitleri(t *testing.T) {
	s := oku(t, "pages/DomainWordPressPage.tsx")
	for ad, gerek := range map[string]string{
		"409 kod okunur":            "bk?.kod === 'belgekok_uyusmuyor'",
		"409 yollar yerleştirilir":  "bk.servis_koku ?? '', bk.hedef ?? ''",
		"kur hatası toast":          `toast.hata(cevir("İşlem başarısız"), m)`,
		"sonuç uyarı kutusu":        "s.uyari_kod === 'belgekok_sunulmuyor'",
		"uyarı başlığı":             `cevir("Site sunulmuyor")`,
		"otomatik düzeltme bilgisi": "s.belge_koku_duzeltildi",
		"sonuç tipi uyarı":          "uyari_kod?: string; servis_koku?: string; hedef?: string",
	} {
		if !strings.Contains(s, gerek) {
			t.Errorf("%s: %q yok", ad, gerek)
		}
	}
	// TR kaynak metin ↔ EN sözlüğü: üç yeni mesajın her biri hem kullanılır hem sözlükte çevrilidir.
	for _, tr := range []string{
		"Servis edilen belge kökü {0}, WordPress hedefi {1}. Site WordPress'i sunmaz.",
		"WordPress kuruldu ancak site sunulmuyor: servis edilen belge kökü {0}, WordPress hedefi {1}.",
		"Belge kökü otomatik düzeltildi:",
		"Site sunulmuyor",
	} {
		if n := strings.Count(s, tr); n < 2 {
			t.Errorf("%q hem kullanımda hem EN sözlüğünde olmalı (bulunan: %d)", tr, n)
		}
	}
	if !strings.Contains(s, "The served document root is {0} but the WordPress target is {1}") ||
		!strings.Contains(s, "the served document root is {0} and the WordPress target is {1}") {
		t.Error("EN çevirileri yer tutucularla eksik")
	}
	// Arayüz mutlak yol üretmez / göndermez.
	if strings.Contains(s, "/home/") {
		t.Error("arayüz mutlak ev yolu içermemeli")
	}
}
