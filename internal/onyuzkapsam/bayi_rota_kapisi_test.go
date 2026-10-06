package onyuzkapsam

// bayi_rota_kapisi_test.go — bayi (rol 'reseller') ROTA KAPISI sözleşmesi (kaynak kapsam
// testi; frontend'de JS test koşucusu yok). Güvenlik sınırı arka uçtur (AdminOnly → 403);
// kapı, bayinin yönetici sayfalarına adres çubuğundan girmesini ve yönetici uçlarına istek
// yağdırmasını engelleyen deneyim katmanıdır.

import (
	"regexp"
	"strings"
	"testing"
)

// bayiSerbest: lib/rol.ts BAYI_SERBEST kümesinin elemanları.
func bayiSerbest(t *testing.T, rol string) map[string]bool {
	t.Helper()
	m := regexp.MustCompile(`(?s)const BAYI_SERBEST = new Set\(\[(.*?)\]\)`).FindStringSubmatch(rol)
	if m == nil {
		t.Fatal("lib/rol.ts: BAYI_SERBEST bulunamadı")
	}
	out := map[string]bool{}
	for _, e := range regexp.MustCompile(`'(/[^']*)'`).FindAllStringSubmatch(m[1], -1) {
		out[e[1]] = true
	}
	return out
}

func TestBayiKapisi_RolKapisiBayiyiYonlendirir(t *testing.T) {
	app := oku(t, "App.tsx")
	for _, s := range []string{
		"if (rol === 'reseller') return <BayiKapisi yol={pathname} />",
		"if (!izinli) return <Navigate to={bayiYonlendirme(yol)} replace />",
		"useBayiAbonelikSahibi(abonelik)",
	} {
		if !strings.Contains(app, s) {
			t.Errorf("App.tsx: %q yok", s)
		}
	}
	if !strings.Contains(app, `<Route element={<RolKapisi />}>`) {
		t.Error("App.tsx: tüm rotalar RolKapisi altında olmalı")
	}
}

func TestBayiKapisi_YoneticiSayfalariIzinListesindeDegil(t *testing.T) {
	serbest := bayiSerbest(t, oku(t, "lib/rol.ts"))
	for _, y := range []string{
		"/bayiler", "/bayi-planlari", "/eklentiler", "/firewall", "/izleme", "/wordpress",
		"/backup-yonetimi", "/araclar-ayarlar", "/araclar/paketler", "/araclar/servisler",
		"/araclar/php-surumler", "/araclar/port-degistirme", "/araclar/ip-yonetimi",
		"/araclar/panel-hostname", "/araclar/guncelleme", "/araclar/optimize",
		"/araclar/oturum-guvenligi", "/araclar/yasakli-domain", "/antivirus", "/tehdit",
		"/saldiri-zincirleri", "/mail-sunucu", "/istatistikler", "/php-sunucu-sihirbazi",
		"/sistem/php-modulleri",
	} {
		if serbest[y] {
			t.Errorf("bayi izin listesinde yönetici sayfası var: %s", y)
		}
	}
}

func TestBayiKapisi_BayiMenusuHedefleriIzinli(t *testing.T) {
	serbest := bayiSerbest(t, oku(t, "lib/rol.ts"))
	lay := oku(t, "components/DashboardLayout.tsx")
	i := strings.Index(lay, "const RESELLER_NAV")
	if i < 0 {
		t.Fatal("DashboardLayout: RESELLER_NAV bulunamadı")
	}
	j := strings.Index(lay[i:], "\n]\n")
	if j < 0 {
		t.Fatal("DashboardLayout: RESELLER_NAV sonu bulunamadı")
	}
	n := 0
	for _, m := range regexp.MustCompile(`to: '(/[^']*)'`).FindAllStringSubmatch(lay[i:i+j], -1) {
		n++
		if !serbest[m[1]] {
			t.Errorf("bayi menüsü %s açıyor ama kapı izin vermiyor (menü ölü bağlantı olur)", m[1])
		}
	}
	if n < 8 {
		t.Errorf("RESELLER_NAV hedefleri az bulundu: %d", n)
	}
	alt := oku(t, "components/AltNavBar.tsx")
	for _, y := range []string{"/", "/domainler", "/hizmet-planlari"} {
		if !serbest[y] {
			t.Errorf("alt çubuk hedefi %s izinli değil", y)
		}
	}
	if !strings.Contains(alt, "bayiSekmeler") {
		t.Error("AltNavBar: bayiSekmeler yok")
	}
}

func TestBayiKapisi_AbonelikYalnizKendiKapsaminda(t *testing.T) {
	rol := oku(t, "lib/rol.ts")
	for _, s := range []string{
		"BAYI_ABONELIK", "BAYI_PLAN", "BAYI_WEBSEC", "export function bayiYoluIzinli",
		"export function bayiYonlendirme", "export function bayiAbonelikID", ".toLowerCase()",
	} {
		if !strings.Contains(rol, s) {
			t.Errorf("lib/rol.ts: %q yok", s)
		}
	}
	ab := oku(t, "lib/bayiAbonelik.ts")
	if !strings.Contains(ab, "api.get<{ id: number }[]>('/domains')") {
		t.Error("bayiAbonelik: sahiplik bayinin kapsamlı GET /domains listesinden verilmeli")
	}
}

func TestBayiKapisi_AramaVeBildirimYoneticiSayfasiGostermez(t *testing.T) {
	top := oku(t, "components/TopBar.tsx")
	// roller verilmeyen girdi yalnız yöneticiye görünür; bayiye açılan girdiler kapıda izinli olmalı.
	serbest := bayiSerbest(t, oku(t, "lib/rol.ts"))
	n := 0
	for _, m := range regexp.MustCompile(`yol: '(/[^']*)', roller: (\[[^\]]*\]|HERKES)`).FindAllStringSubmatch(top, -1) {
		if m[2] == "HERKES" || strings.Contains(m[2], "reseller") {
			n++
			if !serbest[m[1]] {
				t.Errorf("TopBar araması bayiye %s gösteriyor ama kapı izin vermiyor", m[1])
			}
		}
	}
	if n < 5 {
		t.Errorf("TopBar bayi girdileri az bulundu: %d", n)
	}
	bil := oku(t, "lib/bildirim.ts")
	if !strings.Contains(bil, "case 'zincir': return rol === 'admin' ? '/saldiri-zincirleri' : null") {
		t.Error("bildirim hedefi: saldırı zincirleri sayfası yalnız yöneticiye")
	}
}
