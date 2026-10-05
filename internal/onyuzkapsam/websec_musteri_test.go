package onyuzkapsam

// websec_musteri_test.go — hosting MÜŞTERİSİ "Website Security" sayfası sözleşmesi
// (kaynak kapsam testi; frontend'de JS test koşucusu yok). Rota + araç kartı + ana
// sayfa özeti, yoklama temizliği, Ek rozeti, önem süzgeci/öneri, feed şeridi, mobil
// kart listesi, rol kapısı, sızıntı yok ve i18n anahtarları.

import (
	"regexp"
	"strings"
	"testing"
)

// Rota ve kart kaydı: sayfa rotası ToolPage yakalayıcısından ÖNCE; araç kataloğunda
// Güvenlik kategorisinde; ana sayfa bilgi panelinde özet kartı; bildirim bağlantısı.
func TestOnyuz_MusteriGuvenlikRotaVeKart(t *testing.T) {
	app := oku(t, "App.tsx")
	wsIcerir(t, "App", app,
		"const MusteriGuvenlikPage = lazy(() => import('@/pages/MusteriGuvenlikPage'))",
		`<Route path="abonelikler/:id/guvenlik" element={<MusteriGuvenlikPage />} />`)
	if a, b := strings.Index(app, `path="abonelikler/:id/guvenlik"`), strings.Index(app, `path="abonelikler/:id/:slug"`); a < 0 || b < 0 || a > b {
		t.Errorf("guvenlik rotası :slug (ToolPage) rotasından önce olmalı: %d %d", a, b)
	}
	arac := oku(t, "components/musteri-ana/araclar.ts")
	wsIcerir(t, "araclar", arac,
		"kod: 'guvenlik-tarama', ad: 'Website Security'", "kategori: 'guvenlik', yol: 'guvenlik'", "ikon: 'kalkanTara'",
		"'Website Security': 'Website Security'")
	panel := oku(t, "components/musteri-ana/BilgiPaneli.tsx")
	wsIcerir(t, "BilgiPaneli", panel, "import GuvenlikKarti from './GuvenlikKarti'", "<GuvenlikKarti domainId={domainId} />")
	kart := oku(t, "components/musteri-ana/GuvenlikKarti.tsx")
	wsIcerir(t, "kart", kart,
		"`/domains/${domainId}/guvenlik/ozet`", "to={`/abonelikler/${domainId}/guvenlik`}", "ozetGorunumu(o)", "YenidenDene")
	wsIcermez(t, "kart", kart, "'/websec/", "`/websec/", "Math.random", "setInterval")
	bildirim := oku(t, "lib/bildirim.ts")
	wsIcerir(t, "bildirim", bildirim, "musteri ? `${kok}/guvenlik` : `/website-security/domain/${d}`")
	// Müşteri yol izin listesi /abonelikler/<kendi id>/* kapsar (yeni yol ayrıca açılmaz).
	rol := oku(t, "lib/rol.ts")
	wsIcerir(t, "rol", rol, "y.startsWith(kok + '/')")
}

// Yoklama: 2 sn zincirleme zamanlayıcı, gizli sekmede durur, bileşen kalkınca temizlenir.
func TestOnyuz_MusteriGuvenlikYoklamaTemizligi(t *testing.T) {
	sayfa := oku(t, "pages/MusteriGuvenlikPage.tsx")
	wsIcerir(t, "sayfa", sayfa,
		"YOKLAMA_MS", "setTimeout(async () => {", "clearTimeout(zamanlayici)", "if (!document.hidden) await yukle()",
		"addEventListener('visibilitychange'", "removeEventListener('visibilitychange'",
		"yukluyor.current", "canli.current = false", "aktifTarama", "degerlendir(bekleyen.current, l.items, Date.now())",
		"cevir('Tarama beklenenden uzun sürüyor')", "cevir('Tarama tamamlandı')")
	wsIcermez(t, "sayfa", sayfa, "setInterval")
}

// Yalnız müşteri uçları; kök/bayi /websec uçları ve iç yol alanları yok.
func TestOnyuz_MusteriGuvenlikYalnizMusteriUclari(t *testing.T) {
	sayfa := oku(t, "pages/MusteriGuvenlikPage.tsx")
	lib := oku(t, "lib/musteriGuvenlik.ts")
	wsIcerir(t, "sayfa", sayfa,
		"`/domains/${domainId}/guvenlik`", "`/domains/${domainId}/guvenlik/bulgular?", "`/domains/${domainId}/guvenlik/tara`",
		"{ ek_alan_id: s.ek_alan_id ?? 0 }", "musteriMi(useRol())", "if (!gecerli || !musteri) return")
	for ad, k := range map[string]string{"sayfa": sayfa, "lib": lib} {
		wsIcermez(t, ad, k, "'/websec/", "`/websec/", `"/websec/`, "install_path", "next_estimate", "last_error", "scanned_apps")
	}
}

// Ek rozeti, tür/sürüm/paket, durum rozetleri, açılır bulgu paneli (önem süzgeci).
func TestOnyuz_MusteriGuvenlikSatirVeBulgu(t *testing.T) {
	sayfa := oku(t, "pages/MusteriGuvenlikPage.tsx")
	wsIcerir(t, "sayfa", sayfa,
		"function EkRozeti()", "{u.ek && <EkRozeti />}", "cevir('Ek alan adı')",
		"satirAnahtari(u)", "appAd(u.app_type)", "u.paket_sayisi",
		"case 'desteklenmiyor':", "Bu alan adında taranabilir bir uygulama bulunamadı",
		"function BulguPaneli", "aria-expanded={acikMi}", "ONEMLER.map", "aria-pressed={filtre === v}",
		"oneriMetni(b)", "b.duzeltme_surumu", "b.baglanti", `rel="noreferrer noopener"`, "Ikon d={I.harici}",
		"cevir('Hepsi')", "cevir('Bu filtreyle açık yok.')", "cevir('Bu alan adında bilinen açık bulunmadı.')",
		"ek_alan_id: String(ekID)")
	// Ek rozeti hem tabloda hem mobil kartta çizilir (iki yerde).
	if n := strings.Count(sayfa, "{u.ek && <EkRozeti />}"); n != 2 {
		t.Errorf("Ek rozeti tablo + kart olmak üzere 2 yerde çizilmeli: %d", n)
	}
	// Hesabın "Tara" hakkı bilgisi.
	wsIcerir(t, "sayfa", sayfa, "liste.tara.soguma_dk", "liste.tara.gunluk_kalan")
}

// Boş durum + ilk tarama; hata/yükleniyor; feed uyarı şeridi.
func TestOnyuz_MusteriGuvenlikBosHataVeFeedSeridi(t *testing.T) {
	sayfa := oku(t, "pages/MusteriGuvenlikPage.tsx")
	wsIcerir(t, "sayfa", sayfa,
		"<EmptyState", "cevir('İlk taramayı başlat')", "hicTaranmamis", "const ilkTarama = async",
		"cevir('Yükleniyor…')", "cevir('Güvenlik bilgileri alınamadı')", "cevir('Tekrar dene')",
		"{liste.feed_uyari && (", "Güvenlik veri kaynaklarına şu an erişilemiyor, sonuçlar güncel olmayabilir.",
		"<TaramaButonu", "zamanYaz(liste.ozet.son_tarama)", "zamanYaz(u.domain_son_tarama)")
	wsIcermez(t, "sayfa", sayfa, "toLocaleString", "Hepsini Tara", "Tümünü Tara", "false && ")
}

// Mobil (375 px): kart listesi, yatay kaydırma yok; koyu tema; küçük buton kuralı.
func TestOnyuz_MusteriGuvenlikMobilKoyu(t *testing.T) {
	sayfa := oku(t, "pages/MusteriGuvenlikPage.tsx")
	wsIcerir(t, "sayfa", sayfa,
		`className="hidden overflow-hidden rounded-lg border border-slate-200 md:block dark:border-dark-600"`,
		`<ul className="space-y-2 md:hidden">`, "break-all", "flex flex-wrap", "table-fixed",
		"dark:bg-dark-800", "dark:border-dark-600", "dark:text-slate-100", `boyut="sm"`)
	wsIcermez(t, "sayfa", sayfa, "min-w-[", "overflow-x-auto", "whitespace-nowrap overflow")
}

// Yönetici/bayi bu sayfada istek atmaz; kendi sayfalarına yönlendirilir.
func TestOnyuz_MusteriGuvenlikRolKapisi(t *testing.T) {
	sayfa := oku(t, "pages/MusteriGuvenlikPage.tsx")
	wsIcerir(t, "sayfa", sayfa, "if (!musteri) {", `to="/website-security"`,
		"Bu sayfa hosting hesabı oturumuna özeldir")
}

// i18n: sayfada/kartta kullanılan her sabit TR anahtarı EN sözlükte (GUV_EN ya da ORTAK_EN) var.
func TestOnyuz_MusteriGuvenlikEnAnahtarlari(t *testing.T) {
	lib := oku(t, "lib/musteriGuvenlik.ts")
	ortak := oku(t, "lib/cevirOrtak.ts")
	re := regexp.MustCompile(`\b(?:cevir|sablon)\('((?:[^'\\]|\\.)*)'`)
	// ONEM_ROZET.ad değerleri ve oneri şablonları dolaylı anahtarlardır.
	ek := []string{"Kritik", "Yüksek", "Orta", "Düşük", "Kritik {0}", "Yüksek {0}", "Orta {0}", "Düşük {0}"}
	var anahtarlar []string
	for _, y := range []string{"pages/MusteriGuvenlikPage.tsx", "components/musteri-ana/GuvenlikKarti.tsx", "lib/musteriGuvenlik.ts"} {
		for _, m := range re.FindAllStringSubmatch(oku(t, y), -1) {
			anahtarlar = append(anahtarlar, strings.ReplaceAll(m[1], `\'`, `'`))
		}
	}
	anahtarlar = append(anahtarlar, ek...)
	if len(anahtarlar) < 30 {
		t.Fatalf("anahtar ayrıştırıcı şüpheli: %d", len(anahtarlar))
	}
	for _, a := range anahtarlar {
		q := "'" + strings.ReplaceAll(a, "'", `\'`) + "':"
		if !strings.Contains(lib, q) && !strings.Contains(ortak, `"`+a+`"`) && !strings.Contains(lib, `'`+a+`':`) {
			t.Errorf("EN çeviri yok: %q", a)
		}
	}
	// Öneri şablonları (oneri_kod) ve her şablonun EN karşılığı.
	for _, kod := range []string{"guncelle_eklenti", "guncelle_tema", "guncelle_paket", "yama_yok_eklenti", "yama_yok_tema", "yama_yok_paket"} {
		wsIcerir(t, "lib", lib, kod+":")
	}
}
