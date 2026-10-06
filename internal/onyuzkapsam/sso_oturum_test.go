package onyuzkapsam

// sso_oturum_test.go — WHMCS tek tık girişi (API anahtarı, JWT/oturumda SSO bayrağı):
// giren GERÇEK müşteridir. Adına-oturum (bayi/impersonation) şeridi ve "bayi paneline dön"
// görünmez; çıkış olağan müşteri çıkışıdır; müşteri giriş parolası formu yerine kısa
// açıklama çıkar (sunucu kuralı: AdinaYasak 403 aynen sürer). Kaynak-kilit testi.

import (
	"strings"
	"testing"
)

func TestSSO_SeritRenderEdilmez(t *testing.T) {
	seridi := oku(t, "components/AdinaSeridi.tsx")
	if !strings.Contains(seridi, "if (!b || b.sso) return null") {
		t.Error("AdinaSeridi: SSO oturumunda null dönmeli (şerit render edilmez)")
	}
	if !strings.Contains(seridi, "if (!did || b?.sso) return") {
		t.Error("AdinaSeridi: SSO'da şerit için domain isteği de atılmamalı")
	}
	lay := oku(t, "components/DashboardLayout.tsx")
	if !strings.Contains(lay, "const adinaOturum = isMusteri && adinaBilgi() !== null && !ssoOturum()") {
		t.Error("DashboardLayout: şerit SSO oturumunda bağlanmamalı (adinaOturum !ssoOturum() ile korunmalı)")
	}
	if strings.Count(lay, "<AdinaSeridi />") != 1 || !strings.Contains(lay, "{adinaOturum && <AdinaSeridi />}") {
		t.Error("AdinaSeridi yalnız adinaOturum koşuluyla render edilmeli")
	}
}

func TestSSO_CikisOlaganMusteriCikisi_BayiyeDonmez(t *testing.T) {
	a := oku(t, "lib/adina.ts")
	i := strings.Index(a, "if (b?.sso) {")
	j := strings.Index(a, "if (b?.sekme) {")
	if i < 0 || j < 0 || i > j || !strings.Contains(a[i:j], "window.location.assign('/cp/giris')") {
		t.Error("adinaCik: SSO oturumu bayi dönüşünden ÖNCE müşteri giriş sayfasına (/cp/giris) gitmeli")
	}
	au := oku(t, "store/auth.ts")
	if !strings.Contains(au, "if (donusuSakla && b?.don && !b.sso)") {
		t.Error("store/auth: SSO oturumunda bayi dönüş adresi saklanmamalı (süre dolunca müşteri formu)")
	}
	if !strings.Contains(au, "export function ssoOturum(): boolean") || !strings.Contains(au, "sso?: boolean") {
		t.Error("store/auth: AdinaBilgi.sso ve ssoOturum() olmalı")
	}
	g := oku(t, "pages/AdinaGirisPage.tsx")
	for _, b := range []string{"const sso = !p.has('don')", "sso: sso || y.sso === true", "ssoLink ? '/cp/giris' : don", "'Giriş sayfasına git'"} {
		if !strings.Contains(g, b) {
			t.Errorf("AdinaGirisPage: beklenen parça yok: %s", b)
		}
	}
}

func TestSSO_ParolaFormuYerineAciklama(t *testing.T) {
	not := oku(t, "components/SsoParolaNotu.tsx")
	if !strings.Contains(not, "t('Parolanızı müşteri alanınızdan değiştirin')") {
		t.Error("SsoParolaNotu: açıklama metni yok")
	}
	if strings.Contains(not, "<input") || strings.Contains(not, "api.put") {
		t.Error("SsoParolaNotu form/istek içermemeli")
	}
	ftp := oku(t, "pages/DomainFTPPage.tsx")
	if !strings.Contains(ftp, "{ssoOturum() ? <SsoParolaNotu /> : <>") || !strings.Contains(ftp, "anaParolaSifirla") {
		t.Error("DomainFTPPage: ana FTP parola formu SSO'da SsoParolaNotu ile değiştirilmeli")
	}
	con := oku(t, "pages/DomainConnectionPage.tsx")
	if !strings.Contains(con, "if (tip === 'ftp' && ssoOturum())") || !strings.Contains(con, "<SsoParolaNotu />") {
		t.Error("DomainConnectionPage: FTP parola satırı SSO'da açıklamaya dönmeli")
	}
	// tr/en
	c := oku(t, "lib/bayiCeviri.ts")
	for _, k := range []string{"'Parolanızı müşteri alanınızdan değiştirin':", "'Giriş sayfasına git':"} {
		if !strings.Contains(c, k) {
			t.Errorf("bayiCeviri.ts: EN karşılık yok: %s", k)
		}
	}
}
