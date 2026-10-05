package onyuzkapsam

// marka_uygula_test.go — bayi "Markam → Özel adres" TEK Uygula akışı: kaynak kapsam kilitleri
// (sekme her zaman görünür, adım listesi, yoklama temizliği, bağlantı seçimi, EN karşılıklar).
// Kaynağı okur; derleme ayrıca fe_check ile yapılır.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	ozelAdresTSX = "components/marka/OzelAdres.tsx"
	markaSayfa   = "pages/MarkaPage.tsx"
	markaCeviri  = "components/marka/ceviri.ts"
)

func TestMarkaSekme_HerZamanGorunur(t *testing.T) {
	src := oku(t, markaSayfa)
	if !strings.Contains(src, "if (bayi) sekmeler.push({ k: 'adres', ad: 'Özel adres' })") {
		t.Error("bayide 'Özel adres' sekmesi KOŞULSUZ eklenmeli")
	}
	if regexp.MustCompile(`alanadi_izni\)\s*sekmeler\.push`).MatchString(src) {
		t.Error("sekme alanadi_izni'ne bağlanmamalı (izin kapalıyken 'göremiyorum' şikâyeti)")
	}
	oz := oku(t, ozelAdresTSX)
	if !strings.Contains(oz, "status === 403) setIzinYok(true)") || !strings.Contains(oz, "if (izinYok) return <UyariKutusu ton=\"bilgi\">{cevir('Özel panel adresi bu hesap için açık değil. Hizmet sağlayıcınızla iletişime geçin.')}") {
		t.Error("izin yoksa sekme içinde izinYok bilgi mesajı çıkmalı")
	}
}

func TestOzelAdres_AdimListesiVeEylemler(t *testing.T) {
	src := oku(t, ozelAdresTSX)
	for _, b := range []string{
		"'Alan adı doğrulanıyor'", "'DNS kontrol ediliyor'", "'DNS kaydı eklendi'", "'DNS kaydı bekleniyor'", "'SSL sertifikası alınıyor'", "'Aktif'",
		"data-adim-listesi", `data-adim-durum="tamam"`, `data-adim-durum="suruyor"`, `data-adim-durum="bekliyor"`, `data-adim-durum="hata"`,
		"api.post<Yanit>('/marka/alan-adi/uygula'", "cevir('Uygula')", "cevir('Şimdi kontrol et')", "cevir('Yeniden dene')", "cevir('Adresi kaldır')",
		"DNS yayılımı birkaç dakika sürebilir; sayfa kendiliğinden kontrol eder.", "data-dns-tablosu", "cevir('Kopyala')", "cevir('Tür')", "cevir('Ad')", "cevir('Değer')",
	} {
		if !strings.Contains(src, b) {
			t.Errorf("OzelAdres.tsx'te beklenen parça yok: %s", b)
		}
	}
	if strings.Contains(src, "<button") {
		t.Error("ham <button> yok: ortak <Button> kullanılmalı")
	}
	if strings.Contains(src, "api.post<Yanit>('/marka/alan-adi', ") {
		t.Error("eski ekle ucu arayüzden çağrılmamalı (Uygula tek eylem)")
	}
	if !strings.Contains(src, `color="primary" ikon={I.onay} yukleniyor={mesgul}`) {
		t.Error("Uygula birincil (primary) düğme olmalı")
	}
	for _, yasak := range []string{"localStorage", "console.log", "<Select", "<Toggle"} {
		if strings.Contains(src, yasak) {
			t.Errorf("%q kullanılmamalı", yasak)
		}
	}
}

func TestOzelAdres_YoklamaTemizligi(t *testing.T) {
	src := oku(t, ozelAdresTSX)
	for _, b := range []string{
		"const YOKLAMA_MS = 2000",
		"if (!isleniyor) return",   // işlem bitince yoklama durur
		"if (!document.hidden)",    // sekme gizliyken dur
		"window.clearInterval(id)", // temizlik
		"removeEventListener('visibilitychange'",
		"if (sorguda.current) return", // istek çakışması yok
		"canli.current = false",       // bileşen kalkınca durum yazılmaz
		"[isleniyor, yukle]",
	} {
		if !strings.Contains(src, b) {
			t.Errorf("yoklama kilidi eksik: %s", b)
		}
	}
	if strings.Contains(src, "setInterval(() => { void yukle() }") {
		t.Error("yoklama gizli sekmede durmalı")
	}
}

func TestMusteriBaglantisi_OzelAdresAktifseOnuKullanir(t *testing.T) {
	src := oku(t, markaSayfa)
	ozel := strings.Index(src, "if (veri.ozel_adres) return { url: `https://${veri.ozel_adres}/cp/giris`, ozel: true }")
	kisa := strings.Index(src, "/cp/giris?m=${veri.anahtar}")
	if ozel < 0 || kisa < 0 || ozel > kisa {
		t.Fatal("bağlantı: önce özel adres (https://<ad>/cp/giris), yoksa kısa kodlu bağlantı seçilmeli")
	}
	if !strings.Contains(src, "musteriBaglantisi(veri, window.location.origin)") || !strings.Contains(src, "Kendi alan adınızı Özel adres sekmesinden bağlayabilirsiniz.") {
		t.Error("kart seçici fonksiyonu + aktif değilken ipucu olmalı")
	}
	if !strings.Contains(src, "<OzelAdres onDegisti={yukle} />") {
		t.Error("özel adres aktif/kaldırılınca görünüm yenilenmeli (bağlantı kartı güncellensin)")
	}
	if !strings.Contains(oku(t, "components/marka/MarkaFormu.tsx"), "ozel_adres?: string") {
		t.Error("YonetGorunum ozel_adres alanı yok")
	}
}

// Her görünür TR metni için EN karşılık: cevir('…') sabitleri + sunucu adım mesajları.
func TestOzelAdres_ENKarsiliklari(t *testing.T) {
	ceviri := oku(t, markaCeviri)
	re := regexp.MustCompile(`cevir\('((?:[^'\\]|\\.)*)'\)`)
	for _, d := range []string{ozelAdresTSX, markaSayfa} {
		for _, m := range re.FindAllStringSubmatch(oku(t, d), -1) {
			if !strings.Contains(ceviri, "'"+m[1]+"':") && !strings.Contains(ceviri, `"`+m[1]+`":`) {
				t.Errorf("%s: %q için EN karşılık yok (ceviri.ts)", d, m[1])
			}
		}
	}
	// adimEtiketi dönüşleri.
	for _, m := range regexp.MustCompile(`return '([^']+)'`).FindAllStringSubmatch(oku(t, ozelAdresTSX), -1) {
		if !strings.Contains(ceviri, "'"+m[1]+"':") {
			t.Errorf("adım etiketi %q için EN yok", m[1])
		}
	}
	// Sunucunun sabit mesajları (alanadi_uygula.go msj* sabitleri) arayüzde çevrilmiş olmalı.
	gosrc, err := os.ReadFile(filepath.Join("..", "marka", "alanadi_uygula.go"))
	if err != nil {
		t.Fatal(err)
	}
	tsx := oku(t, ozelAdresTSX)
	n := 0
	for _, m := range regexp.MustCompile(`(?m)^\s*msj\w+\s*=\s*"([^"]+)"`).FindAllStringSubmatch(string(gosrc), -1) {
		n++
		if m[1] == "Bu ad için mevcut kayıt var" {
			continue
		}
		if !strings.Contains(tsx, "'"+m[1]+"'") {
			t.Errorf("sunucu mesajı arayüzde EN karşılıksız: %q", m[1])
		}
	}
	if n < 10 {
		t.Errorf("msj sabitleri ayrıştırılamadı (%d)", n)
	}
}
