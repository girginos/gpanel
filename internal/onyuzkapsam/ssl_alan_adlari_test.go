package onyuzkapsam

// ssl_alan_adlari_test.go — SSL/TLS Sertifikaları sayfası: hesabın TÜM alan adları (ana +
// ek + alt) tek tabloda; her satır hedef türüne göre MEVCUT SSL ucuna gider (yeni yazma ucu
// yok); uzun çağrılar UZUN_ISTEK; DNS uyarısı; ?hedef derin bağlantı; ortak seçici.

import (
	"regexp"
	"strings"
	"testing"
)

func TestSslTablosu_KolonlarVeRozetler(t *testing.T) {
	s := oku(t, "components/SslAlanAdlari.tsx")
	for _, w := range []string{
		"t('Alan adı')", "t('Durum')", "t('Sertifika')", "t('Bitiş')", "t('Kapsam')", "t('İşlemler')", // kolonlar
		"t('Aktif')", "t('Yok')", "t('Süresi yaklaşıyor')", "t('Süresi doldu')", // durum rozetleri
		"t('Let’s Encrypt')", "t('Öz-imza')", // sertifika türü
		"{2} süresi yaklaşan (≤14 gün)", "data-ozet", // hesap özeti
		"h.kalan_gun", "h.bitis", "h.kapsam.map", // bitiş + kalan gün + kapsam
		"<table", "md:hidden", // tablo + 375px kart listesi
		"TurRozeti tur={h.tur}", // Ana/Ek/Alt rozeti
	} {
		if !strings.Contains(s, w) {
			t.Errorf("SslAlanAdlari.tsx: %q yok", w)
		}
	}
	// yeniden kullanılan ölçüler: işlem butonları küçük
	if n := strings.Count(s, `boyut="sm"`); n < 6 {
		t.Errorf("işlem butonları boyut=\"sm\" olmalı (bulunan %d)", n)
	}
}

func TestSslTablosu_HedefeGoreDogruUc(t *testing.T) {
	s := oku(t, "components/SslAlanAdlari.tsx")
	// uç eşleme: tek yerde (sslUc) ve tam yollar
	if !strings.Contains(s, "return `/domains/${id}/ek-alan-adlari/${h.ek_alan_id}/ssl`") {
		t.Error("ek hedef → /domains/{id}/ek-alan-adlari/{eid}/ssl değil")
	}
	if !strings.Contains(s, "return `/domains/${id}/subdomain/${h.alt_alan_id}/ssl`") {
		t.Error("alt hedef → /domains/{id}/subdomain/{sid}/ssl değil")
	}
	if !regexp.MustCompile(`if \(h\.tur === 'ek'\) return `+"`"+`/domains/\$\{id\}/ek-alan-adlari`).MatchString(s) ||
		!regexp.MustCompile(`if \(h\.tur === 'alt'\) return `+"`"+`/domains/\$\{id\}/subdomain`).MatchString(s) {
		t.Error("sslUc tür koşulları yanlış eşleşiyor")
	}
	// ana hedef kendi uçlarına gitmez: sayfanın akışını çağırır
	if strings.Count(s, "if (h.tur === 'ana') { anaKur(tip); return }") != 1 || strings.Count(s, "if (h.tur === 'ana') { anaKaldir(); return }") != 1 {
		t.Error("ana hedef sayfanın kendi akışına (anaKur/anaKaldir) yönlenmeli")
	}
	if strings.Contains(s, "/ssl/issue") {
		t.Error("tablo bileşeni ana alan adı ucunu doğrudan çağırmamalı (sayfanın ilerleme akışı kullanılır)")
	}
	// okuma ucu
	if !strings.Contains(s, "`/domains/${id}/ssl/hedefler`") {
		t.Error("hedefler ucu çağrılmıyor")
	}
	// POST gövdesi tip, uzun istek zaman aşımı
	posts := cagrilar(s, "api.post(uc")
	dels := cagrilar(s, "api.delete(uc")
	if len(posts) != 1 || len(dels) != 1 {
		t.Fatalf("1 POST + 1 DELETE bekleniyordu: %d/%d", len(posts), len(dels))
	}
	for _, c := range append(posts, dels...) {
		if !strings.Contains(c, "UZUN_ISTEK") {
			t.Errorf("uzun SSL çağrısı UZUN_ISTEK kullanmıyor: %s", c)
		}
	}
	if !strings.Contains(posts[0], "{ tip }") {
		t.Error("POST gövdesi { tip } olmalı")
	}
}

func TestSslTablosu_DNSUyarisiVeHatalar(t *testing.T) {
	s := oku(t, "components/SslAlanAdlari.tsx")
	for _, w := range []string{
		"Alan adı bu sunucuya yönlenmiyor; Let’s Encrypt başarısız olabilir",
		"const dnsUyari = h.dns_hazir === false && (!h.ssl_aktif || h.tur_sertifika === 'oz-imza')",
		"{dnsUyari && (",
		"DNS’i tekrar kontrol et",
		"dns_hazir === null", // arka plan çözümü: sınırlı yeniden okuma
		"dnsDeneme.current >= 3",
		"apiHata(e,",                     // sunucunun sınıflandırılmış mesajı; ham çıktı gösterilmez
		"toast.hata(t('SSL kurulamadı')", // hata → toast
		"toast.basari(",
		"yukleniyor={mesgul === 'kur'}", // satır içi spinner
		"t('Kuruluyor…')",
		"h.askida || h.demo",
	} {
		if !strings.Contains(s, w) {
			t.Errorf("SslAlanAdlari.tsx: %q yok", w)
		}
	}
	if strings.Contains(s, "Math.random") || strings.Contains(s, "sahte") {
		t.Error("sahte veri izi")
	}
}

func TestSslSayfasi_SuzgecVeDerinBaglanti(t *testing.T) {
	p := oku(t, "pages/DomainSSLPage.tsx")
	for _, w := range []string{
		"useSearchParams", "sp.get('hedef')", `^(ana|ek:\d+|alt:\d+)$`, // ?hedef=ek:<id>
		"<SslAlanAdlari", "anaKur=", "anaKaldir=", "yenileSay={tabloYenile}",
		"anaGorunur && (<>", // ana alan adı ayrıntı kartları (durum, kapsam, kurulum) KORUNUR
		`cevir("Mevcut Durum")`, "<KapsamKarti", `cevir("Self-Signed Sertifika")`, "ssl/issue", "api.delete(`/domains/${id}/ssl`)",
	} {
		if !strings.Contains(p, w) {
			t.Errorf("DomainSSLPage.tsx: %q yok", w)
		}
	}
	s := oku(t, "components/SslAlanAdlari.tsx")
	for _, w := range []string{"AlanAdiSecici", "tumuEtiketi={t('Tüm alan adları')}", "Yalnız SSL’i olmayanlar", "!h.ssl_aktif", "hedefAnahtari(h) === hedef"} {
		if !strings.Contains(s, w) {
			t.Errorf("SslAlanAdlari.tsx süzgeç: %q yok", w)
		}
	}
	// alt/ek yönetim sayfalarından "SSL yönet" bağlantısı (hedef önseçili); mevcut bölümler yerinde
	if a := oku(t, "pages/DomainSubdomainYonetPage.tsx"); !strings.Contains(a, "ssl?hedef=alt:${sid}") || !strings.Contains(a, "sslKur('letsencrypt')") {
		t.Error("DomainSubdomainYonetPage: SSL yönet bağlantısı yok / mevcut bölüm bozuldu")
	}
}

func TestAlanAdiSecici_OrtakBilesen(t *testing.T) {
	s := oku(t, "components/AlanAdiSecici.tsx")
	for _, w := range []string{
		"export type AlanAdiTuru = 'ana' | 'ek' | 'alt'", "pasif?: boolean", "ipucu?: string", "tumuEtiketi?: string",
		"export function TurRozeti", "disabled: !!s.pasif", "draggable={false}", "<Select",
	} {
		if !strings.Contains(s, w) {
			t.Errorf("AlanAdiSecici.tsx: %q yok", w)
		}
	}
}
