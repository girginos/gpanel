package onyuzkapsam

// websec_ek_alan_test.go — Website Security Monitor EK ALAN satırları sözleşmesi
// (kaynak kapsam testi): ayrı satır + "Ek" rozeti, yoklama/bekleyen anahtarı
// (domain_id, ek_alan_id), satır başına ayrı Tara, arama, ek alan detay sayfası,
// TR+EN, 375px ve koyu tema.

import (
	"regexp"
	"testing"
)

func TestOnyuz_EkAlanSatiriVeRozet(t *testing.T) {
	sayfa := oku(t, "pages/WebsiteSecurityPage.tsx")
	wsIcerir(t, "sayfa", sayfa,
		"ek_alan_id?: number",
		"u.ek_alan_id ? (", `cevir("Ek")`, `cevir("Ek alan adı (addon domain)")`,
		"{u.alan_adi || `#${u.domain_id}`}", // ek alanın KENDİ adı (hesap adı değil)
		"key={`${anahtar}-${u.app_type}-${u.install_path}`}",
		// 375px: alan adı kırılır, rozet sarar, tablo yatay kayar.
		"flex flex-wrap items-center gap-1.5", "break-all", "overflow-x-auto",
		// koyu tema
		"dark:bg-dark-700 dark:text-slate-300")
	// TR+EN
	wsIcerir(t, "sayfa EN", sayfa, `"Ek": "Add-on"`, `"Ek alan adı (addon domain)": "Add-on domain"`)
}

func TestOnyuz_EkAlanYoklamaAnahtari(t *testing.T) {
	sayfa := oku(t, "pages/WebsiteSecurityPage.tsx")
	lib := oku(t, "lib/websecTarama.ts")
	wsIcerir(t, "lib", lib,
		"export const satirAnahtari", "s.ek_alan_id ? -s.ek_alan_id : s.domain_id",
		"ek_alan_id?: number", "const a = satirAnahtari(s)")
	wsIcermez(t, "lib", lib, "sonuc.set(s.domain_id", "aktif.set(s.domain_id")
	wsIcerir(t, "sayfa", sayfa,
		"onceki.set(satirAnahtari(u)", "const a = satirAnahtari(u)", "tarananDomain === anahtar",
		"const domainTara = async (u: Uygulama)")
	wsIcermez(t, "sayfa", sayfa, "onceki.set(u.domain_id", "void domainTara(u.domain_id)")
}

// Ek alan satırındaki Tara yalnız o ek alanı tarar (ek_alan_id), ana satır domain_id.
func TestOnyuz_EkAlanTaraUcu(t *testing.T) {
	sayfa := oku(t, "pages/WebsiteSecurityPage.tsx")
	wsIcerir(t, "sayfa", sayfa,
		"u.ek_alan_id ? '/websec/rescan?ek_alan_id=' + u.ek_alan_id : '/websec/rescan?domain_id=' + u.domain_id")
	detay := oku(t, "pages/DomainSecurityPage.tsx")
	wsIcerir(t, "detay", detay,
		"ekID > 0 ? '/websec/rescan?ek_alan_id=' + ekID : '/websec/rescan?domain_id=' + domainID")
}

// Arama kutusu alan adını süzer; ek alan satırının alan_adi kendi adıdır → bulunur.
func TestOnyuz_EkAlanArama(t *testing.T) {
	sayfa := oku(t, "pages/WebsiteSecurityPage.tsx")
	wsIcerir(t, "sayfa", sayfa, "(u.alan_adi || '').toLowerCase().includes(q)")
}

// Detay sayfası: ?ek=<id> ile ek alan; bulgu + envanter ek alana süzülür, başlıkta ek
// alan adı + rozet; ana sayfa ek alan bulgularını karıştırmaz (ek_alan_id=0).
func TestOnyuz_EkAlanDetaySayfasi(t *testing.T) {
	sayfa := oku(t, "pages/WebsiteSecurityPage.tsx")
	detay := oku(t, "pages/DomainSecurityPage.tsx")
	wsIcerir(t, "liste→detay", sayfa, "'?ek=' + u.ek_alan_id")
	wsIcerir(t, "detay", detay,
		"useSearchParams", "sp.get('ek')", "ek_alan_id: String(ekID)",
		"(u.ek_alan_id ?? 0) === ekID", "{ekID > 0 && (", `cevir("Ek")`,
		`"Bu Ek Alanı Tara": "Scan This Add-on Domain"`, `"Ek": "Add-on"`,
		"[domainID, ekID, gecerliID, filtre, aktifSayfa]")
	if !regexp.MustCompile(`ekHam === null \|\| ekID > 0`).MatchString(detay) {
		t.Error("geçersiz ?ek değeri 'Domain bulunamadı' olmalı")
	}
}
