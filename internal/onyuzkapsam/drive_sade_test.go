package onyuzkapsam

// drive_sade_test.go — Google Drive uzak yedek akışı "Plesk gibi sade": anahtar üret/indir/
// onayla ve "Yedek no" alanı kalktı; hedef Genel Yedek Ayarları'nda seçilir; satır düğmesi ve
// toplu gönderim var; kullanıcı metinlerinde teknik jargon yok.

import (
	"regexp"
	"strings"
	"testing"
)

func TestDriveSade_EskiKarmasikAkisKalkti(t *testing.T) {
	src := oku(t, "components/YedekUzakKopya.tsx")
	for _, yasak := range []string{
		"Yedek no", "yedekNo", "anahtar-uret", "anahtar-onayla", "anahtar-ice-aktar",
		"sakladim", "sakladım", "kurtarma_dosyasi", "simdi-gonder",
	} {
		if strings.Contains(src, yasak) {
			t.Errorf("YedekUzakKopya.tsx eski akıştan kalıntı içeriyor: %q", yasak)
		}
	}
	for _, gerek := range []string{
		"uzak-kopya/kurtarma-anahtari", "uzak-kopya/mevcutlari-gonder", "uzak-kopya/anahtar-yenile", "Kurtarma anahtarını indir",
	} {
		if !strings.Contains(src, gerek) {
			t.Errorf("YedekUzakKopya.tsx beklenen öğeyi içermiyor: %q", gerek)
		}
	}
	// Kullanıcı metinlerinde teknik jargon görünmez.
	for _, jargon := range []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bage\b`), regexp.MustCompile(`X25519`), regexp.MustCompile(`(?i)manifest`),
		regexp.MustCompile(`(?i)sha-?256`), regexp.MustCompile(`ML-KEM`), regexp.MustCompile(`(?i)hibrit`),
	} {
		for _, satir := range strings.Split(src, "\n") {
			s := strings.TrimSpace(satir)
			if strings.HasPrefix(s, "//") || strings.Contains(s, "setHibrit") || strings.Contains(s, "{ hibrit }") || strings.Contains(s, "checked={hibrit}") {
				continue
			}
			if jargon.MatchString(s) {
				t.Errorf("kullanıcı metninde jargon (%s): %s", jargon, s)
			}
		}
	}
}

func TestDriveSade_HedefGenelAyardaSecilir(t *testing.T) {
	src := oku(t, "components/YedekGenelAyar.tsx")
	for _, gerek := range []string{`value="drive"`, "uzak-kopya/ayar", "#google-drive", "Önce Google ile bağlanın"} {
		if !strings.Contains(src, gerek) {
			t.Errorf("YedekGenelAyar.tsx beklenen öğeyi içermiyor: %q", gerek)
		}
	}
	// Drive yalnız bağlıysa seçilebilir.
	if !strings.Contains(src, `disabled={drives.length === 0}`) {
		t.Error("Drive seçeneği bağlı hesap yokken devre dışı olmalı")
	}
	// Eski ayrı "Uzak kopya" kartı sayfa sırasında Drive bağlantı kartından ÖNCE, tek kez.
	sayfa := oku(t, "pages/BackupYonetimiPage.tsx")
	if strings.Count(sayfa, "<YedekUzakKopya") != 1 || !strings.Contains(sayfa, "<YedekGenelAyar") {
		t.Error("sayfa YedekGenelAyar + tek YedekUzakKopya içermeli")
	}
}

func TestDriveSade_SatirDugmesiYalnizYoneticiyeVeKokYedege(t *testing.T) {
	src := oku(t, "pages/DomainBackupsPage.tsx")
	for _, gerek := range []string{
		"uzak-kopya/yedek-durumlari", "uzak-kopya/simdi-gonder", "Drive'a gönder",
		"useAuth((st) => st.kullanici?.rol) === 'admin'",
	} {
		if !strings.Contains(src, gerek) {
			t.Errorf("DomainBackupsPage.tsx beklenen öğeyi içermiyor: %q", gerek)
		}
	}
	// düğme yalnız sistem (kök) yedeğinde ve yönetici + Drive açıkken
	if !strings.Contains(src, "admin && driveDurum?.aktif && kaynakOf(y) === 'sistem'") {
		t.Error("Drive düğmesi/rozeti yönetici + Drive açık + sistem yedeği koşuluna bağlı olmalı")
	}
}
