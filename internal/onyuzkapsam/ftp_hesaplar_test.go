package onyuzkapsam

// ftp_hesaplar_test.go — müşteri paneli FTP sayfası (ana hesap + ek FTP hesapları)
// kaynak kapsam kilitleri: buton dili, parola sızıntısı yok, uç sözleşmesi, i18n
// anahtarları, dar/mobil düzen. Kaynağı okur; derleme/tsc ayrıca fe_check ile yapılır.

import (
	"regexp"
	"strings"
	"testing"
)

const ftpSayfa = "pages/DomainFTPPage.tsx"
const ftpSecici = "components/FtpKlasorSecici.tsx"

// Ham <button> ve düz bağlantı-buton YOK: tüm eylemler ortak <Button> bileşeniyle.
func TestFtpSayfasi_ButonDili(t *testing.T) {
	for _, d := range []string{ftpSayfa, ftpSecici} {
		src := oku(t, d)
		if strings.Contains(src, "<button") {
			t.Errorf("%s: ham <button> var; <Button> bileşeni kullanılmalı", d)
		}
	}
	src := oku(t, ftpSayfa)
	// Satır işlemleri ve ana eylemler ikonlu Button; yıkıcı olan error tonunda.
	for _, beklenen := range []string{
		`<Button color="error" variant="outlined" boyut="sm" ikon={I.cop}`, // Sil
		`boyut="sm" ikon={I.anahtar}`,                                      // Parola
		`boyut="sm" ikon={I.kalem}`,                                        // Düzenle
		`color="primary" ikon={I.arti}`,                                    // Yeni FTP hesabı
	} {
		if !strings.Contains(src, beklenen) {
			t.Errorf("beklenen buton kalıbı yok: %s", beklenen)
		}
	}
	if strings.Contains(src, "<table") {
		t.Error("ek hesap listesi tablo değil kart/satır listesi olmalı (375px yatay kaydırma yok)")
	}
}

// Parola: göster/gizle, tarayıcıya kalıcı yazılmaz, konsola basılmaz, oto-doldurma kapalı.
func TestFtpSayfasi_ParolaSizmaz(t *testing.T) {
	for _, d := range []string{ftpSayfa, ftpSecici} {
		src := oku(t, d)
		for _, yasak := range []string{"localStorage", "sessionStorage", "console.log", "console.info", "document.cookie"} {
			if strings.Contains(src, yasak) {
				t.Errorf("%s: %q kullanılmamalı (parola/bilgi sızıntısı)", d, yasak)
			}
		}
	}
	src := oku(t, ftpSayfa)
	if !strings.Contains(src, "type={goster ? 'text' : 'password'}") {
		t.Error("parola alanı göster/gizle düğmesine bağlı olmalı (varsayılan gizli)")
	}
	if !strings.Contains(src, `autoComplete="new-password"`) {
		t.Error("parola alanı tarayıcı oto-doldurmasına kapalı olmalı")
	}
	if !strings.Contains(src, "parolaUret(16)") || !strings.Contains(src, "panoyaKopyala") {
		t.Error("güvenli üreteç (crypto) ve ortak pano yardımcısı kullanılmalı")
	}
}

// Uç sözleşmesi: sayfa yalnız /ftp-ek uçlarını ve mevcut ana parola ucunu çağırır.
func TestFtpSayfasi_UcSozlesmesi(t *testing.T) {
	src := oku(t, ftpSayfa)
	for _, uc := range []string{
		"api.get<EkListe>(`/domains/${id}/ftp-ek`)",
		"api.post<{ hesap: EkHesap }>(`/domains/${id}/ftp-ek`",
		"api.put(`/domains/${id}/ftp/password`", // ana hesap parola sıfırlama (mevcut uç, değişmedi)
		"/password`, { parola: pw }",            // ek hesap parolası
		"{ etkin: !h.etkin }",
		"api.delete(`/domains/${id}/ftp-ek/${encodeURIComponent(",
	} {
		if !strings.Contains(src, uc) {
			t.Errorf("beklenen çağrı yok: %s", uc)
		}
	}
	// Kullanıcı adı URL'ye kaçışlı girer (IDOR/yol enjeksiyonu).
	if n := strings.Count(src, "encodeURIComponent("); n < 4 {
		t.Errorf("ek hesap uçlarında kullanıcı adı encodeURIComponent ile girmeli (bulunan %d)", n)
	}
	// Yeni hesap formu sunucu kuralını yansıtır: önek sabit, 12+ harf+rakam parola.
	if !strings.Contains(src, "^[a-z0-9][a-z0-9_]{0,23}$") || !strings.Contains(src, "p.length >= 12") {
		t.Error("istemci ön doğrulaması sunucu kurallarıyla (ek ad deseni, parola 12+) uyumlu olmalı")
	}
}

// i18n: cevir("...") ile çağrılan her Türkçe metin FTP_EN ya da ORTAK_EN'de İngilizcesine sahip.
func TestFtpSayfasi_EnCeviriKapsami(t *testing.T) {
	anahtarRe := regexp.MustCompile(`(?m)^\s*"((?:[^"\\]|\\.)+)"\s*:`)
	var bilinen = map[string]bool{}
	for _, m := range anahtarRe.FindAllStringSubmatch(oku(t, "lib/cevirOrtak.ts"), -1) {
		bilinen[m[1]] = true
	}
	src := oku(t, ftpSayfa)
	// FTP_EN bloğu
	bas := strings.Index(src, "const FTP_EN")
	son := strings.Index(src, "const cevir =")
	if bas < 0 || son < bas {
		t.Fatal("FTP_EN bloğu bulunamadı")
	}
	for _, m := range anahtarRe.FindAllStringSubmatch(src[bas:son], -1) {
		bilinen[m[1]] = true
	}
	cagriRe := regexp.MustCompile(`cevir\(([^()]*)\)`)
	tirnakRe := regexp.MustCompile(`"((?:[^"\\]|\\.)+)"`)
	for _, d := range []string{ftpSayfa} {
		for _, c := range cagriRe.FindAllStringSubmatch(oku(t, d), -1) {
			for _, s := range tirnakRe.FindAllStringSubmatch(c[1], -1) {
				if !bilinen[s[1]] {
					t.Errorf("%s: EN çeviri yok: %q", d, s[1])
				}
			}
		}
	}
	// Seçici bileşen metinleri t() ile sayfadan gelir; aynı kapsama bakılır.
	sec := oku(t, ftpSecici)
	for _, m := range regexp.MustCompile(`\bt\('((?:[^'\\]|\\.)+)'\)`).FindAllStringSubmatch(sec, -1) {
		if !bilinen[m[1]] {
			t.Errorf("%s: EN çeviri yok: %q", ftpSecici, m[1])
		}
	}
}

// Düzen: içerik dar (max-w-4xl, ortalı), kartlar, satırlar sarılır (min-w-0/flex-wrap),
// koyu tema sınıfları var; sayfa kendi kökünde sabit genişlik/yatay kaydırma yok.
func TestFtpSayfasi_DarVeMobilDuzen(t *testing.T) {
	src := oku(t, ftpSayfa)
	if !strings.Contains(src, "mx-auto w-full max-w-4xl") {
		t.Error("içerik max-w-4xl + mx-auto ile daraltılmalı")
	}
	for _, g := range []string{"flex-wrap", "min-w-0", "truncate", "dark:bg-dark-700"} {
		if !strings.Contains(src, g) {
			t.Errorf("düzen sınıfı eksik: %s", g)
		}
	}
	for _, yasak := range []string{"overflow-x-scroll", "min-w-[600px]", "min-w-[700px]", "w-[800px]"} {
		if strings.Contains(src, yasak) {
			t.Errorf("mobilde yatay taşma riski: %s", yasak)
		}
	}
	if !strings.Contains(src, "EmptyState") || !strings.Contains(src, "Henüz ek FTP hesabı yok") {
		t.Error("boş durum çizimi yok")
	}
	if strings.Contains(src, "cleartext") {
		t.Error("eski 'cleartext' uyarı metni sade SFTP notuyla değişmeliydi")
	}
}
