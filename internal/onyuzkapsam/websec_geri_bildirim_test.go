package onyuzkapsam

// onyuz_geri_bildirim_test.go — Website Security Monitor sayfasının "Tara" sonrası
// geri bildirim sözleşmesi (kaynak kapsam testi; frontend'de JS test koşucusu yok,
// onyuzkapsam desenindeki gibi kaynak metni denetlenir).

import (
	"regexp"
	"strings"
	"testing"
)

func wsIcerir(t *testing.T, ad, kaynak string, parcalar ...string) {
	t.Helper()
	for _, p := range parcalar {
		if !strings.Contains(kaynak, p) {
			t.Errorf("%s: %q yok", ad, p)
		}
	}
}

func wsIcermez(t *testing.T, ad, kaynak string, parcalar ...string) {
	t.Helper()
	for _, p := range parcalar {
		if strings.Contains(kaynak, p) {
			t.Errorf("%s: %q OLMAMALI", ad, p)
		}
	}
}

// Yoklama: 2 sn, zincirleme zamanlayıcı (üst üste binmez), gizli sekmede durur,
// bileşen kalkınca temizlenir.
func TestOnyuz_YoklamaTemizligi(t *testing.T) {
	sayfa := oku(t, "pages/WebsiteSecurityPage.tsx")
	lib := oku(t, "lib/websecTarama.ts")
	wsIcerir(t, "lib", lib, "export const YOKLAMA_MS = 2000")
	wsIcerir(t, "sayfa", sayfa,
		"setTimeout(async () => {", "clearTimeout(zamanlayici)", "if (!document.hidden) await yukle()",
		"addEventListener('visibilitychange'", "removeEventListener('visibilitychange'",
		"yukluyor.current", "canli.current = false", "aktifTarama")
	wsIcermez(t, "sayfa", sayfa, "setInterval")
}

// Satır "Taranıyor…": istek sonrası sonucu beklenen domain taranıyor gösterilir.
func TestOnyuz_TaraniyorDurumu(t *testing.T) {
	sayfa := oku(t, "pages/WebsiteSecurityPage.tsx")
	wsIcerir(t, "sayfa", sayfa,
		"bekleyenIds.has(u.domain_id) && u.durum !== 'sirada') ? 'taraniyor'",
		"<DurumRozet u={u} bekliyor={bekleyenIds.has(u.domain_id)} />",
		"taramaIste('/websec/rescan?domain_id=' + id, cevir(\"Tarama hatası\"), id)")
}

// Sonuç özeti: tamamlandı toast'ı + "taranacak uygulama bulunamadı" + uygulamasız satır ipucu.
func TestOnyuz_SonucMetinleri(t *testing.T) {
	sayfa := oku(t, "pages/WebsiteSecurityPage.tsx")
	wsIcerir(t, "sayfa", sayfa,
		`cevir("Tarama tamamlandı")`,
		"Taranacak uygulama bulunamadı (WordPress, Node.js veya PHP Composer yok).",
		"Bu domainde taranabilir bir uygulama bulunamadı (WordPress, Node.js, PHP Composer).",
		"u.durum === 'desteklenmiyor'", "d.uygulama === 0", "Zaten sırada ya da taranıyor")
}

// Zaman: tek yardımcı (zamanYaz) — üst kart ve tablo aynı biçim; ham dizge yok.
func TestOnyuz_TekZamanYardimcisi(t *testing.T) {
	sayfa := oku(t, "pages/WebsiteSecurityPage.tsx")
	wsIcerir(t, "sayfa", sayfa,
		"zamanYaz(status.last_run)", "zamanYaz(status.last_success)", "zamanYaz(u.son_tarama)")
	wsIcermez(t, "sayfa", sayfa, "zamanFmt", "u.son_tarama ||", "toLocaleString")
	if n := len(regexp.MustCompile(`zamanYaz\(`).FindAllString(sayfa, -1)); n < 3 {
		t.Errorf("zamanYaz kullanımı %d, en az 3", n)
	}
}

// Feed uyarısı: şerit + durum alanı; "Temiz" güvencesi yanıltıcı olmasın.
func TestOnyuz_FeedUyariSeridi(t *testing.T) {
	sayfa := oku(t, "pages/WebsiteSecurityPage.tsx")
	wsIcerir(t, "sayfa", sayfa,
		"feed_uyari?: boolean", "status.feed_uyari",
		"Güvenlik veri kaynaklarına şu an erişilemiyor, sonuçlar güncel olmayabilir.")
}

// Bitiş tespiti: damga değişimi (tarama çok kısa sürebilir) + zaman aşımı.
func TestOnyuz_BitisTespiti(t *testing.T) {
	lib := oku(t, "lib/websecTarama.ts")
	wsIcerir(t, "lib", lib,
		"(s.domain_son_tarama ?? '') !== b.onceki", "ZAMAN_ASIMI_MS", "aktifGoruldu")
}
