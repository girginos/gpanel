package subdomain

// ekalan_webroot_test.go — ek alan adı belge kökü: yol kaçışları reddedilir (.. / mutlak /
// symlink / başka kiracı), rol kapsamı (başkasının ek alanı 404), vhost `root`, nginx -t
// kapısı (başarısızsa DB + vhost eski), varsayılan davranış DEĞİŞMEZ, kurcalanmış DB değeri
// tabana düşer. DB'li testler GOSP_TEST_GECICI_DSN yoksa atlanır.

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var ekWRDB *sql.DB

func ekWRKur(t *testing.T) (*ekDikis, int64, string) {
	t.Helper()
	db := ek_db(t)
	d := ek_dikisler(t)
	ekWRDB = db
	const ad = "wr.example.org"
	if kod, g := ek_ekle(t, db, ek_dom1, ad); kod != http.StatusOK {
		t.Fatalf("kurulum: %d %s", kod, g)
	}
	return d, ek_eid(t, db, ad), ad
}

func ekWRPut(t *testing.T, dom, eid int64, govde string) (int, string) {
	t.Helper()
	return ek_cagir(t, ekWRDB, "PUT", "/domains/{id}/ek-alan-adlari/{eid}/web-root",
		fmt.Sprintf("/domains/%d/ek-alan-adlari/%d/web-root", dom, eid), govde,
		func(h *Handlers) http.HandlerFunc { return h.EkWebRootPut })
}

func ekWRGet(t *testing.T, dom, eid int64) (int, string) {
	t.Helper()
	return ek_cagir(t, ekWRDB, "GET", "/domains/{id}/ek-alan-adlari/{eid}/web-root",
		fmt.Sprintf("/domains/%d/ek-alan-adlari/%d/web-root", dom, eid), "",
		func(h *Handlers) http.HandlerFunc { return h.EkWebRootGet })
}

func ekWRDBdeger(t *testing.T, eid int64) string {
	t.Helper()
	var w string
	if err := ekWRDB.QueryRow(`SELECT web_root FROM ek_alan_adlari WHERE id=?`, eid).Scan(&w); err != nil {
		t.Fatal(err)
	}
	return w
}

func ekWRVhost(t *testing.T, ad string) string {
	t.Helper()
	b, err := os.ReadFile(ekConfPath(ek_sk1, ad))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestEkWebRoot_VarsayilanDegismez(t *testing.T) {
	_, eid, ad := ekWRKur(t)
	if w := ekWRDBdeger(t, eid); w != "" {
		t.Fatalf("yeni ek alanın web_root'u boş olmalı: %q", w)
	}
	if !strings.Contains(ekWRVhost(t, ad), "root "+ekDocroot(ek_sk1, ad)+";") {
		t.Fatal("varsayılan root ek alanın public_html'i olmalı")
	}
	a, _ := ekVhostGovde(ek_sk1, ad, "8.3")
	b, _ := ekVhostGovdeKok(ek_sk1, ad, "8.3", "")
	if a != b {
		t.Fatal("boş web_root eski gövdeyle birebir aynı olmalı")
	}
	if kod, g := ekWRGet(t, ek_dom1, eid); kod != 200 || !strings.Contains(g, `"alt_dizin":""`) {
		t.Fatalf("GET: %d %s", kod, g)
	}
}

func TestEkWebRoot_Ayarla_VhostRootDogru(t *testing.T) {
	d, eid, ad := ekWRKur(t)
	taban := ekDocroot(ek_sk1, ad)
	if err := os.MkdirAll(taban+"/uyg/public", 0o755); err != nil {
		t.Fatal(err)
	}
	reload0 := d.say("systemctl reload nginx")
	for _, alt := range []string{"uyg/public", "/uyg/public/"} {
		kod, g := ekWRPut(t, ek_dom1, eid, `{"alt_dizin":"`+alt+`"}`)
		if kod != 200 || !strings.Contains(g, `"alt_dizin":"uyg/public"`) {
			t.Fatalf("%q: %d %s", alt, kod, g)
		}
		if w := ekWRDBdeger(t, eid); w != taban+"/uyg/public" {
			t.Fatalf("DB web_root: %q", w)
		}
		v := ekWRVhost(t, ad)
		if !strings.Contains(v, "root "+taban+"/uyg/public;") || strings.Contains(v, "root "+taban+";") {
			t.Fatalf("vhost root yanlış:\n%s", v)
		}
	}
	if d.say("nginx -t") == 0 || d.say("systemctl reload nginx") <= reload0 {
		t.Fatal("nginx -t + reload çalışmadı")
	}
	if kod, g := ekWRGet(t, ek_dom1, eid); kod != 200 || !strings.Contains(g, `"alt_dizin":"uyg/public"`) || !strings.Contains(g, `"adaylar"`) {
		t.Fatalf("GET: %d %s", kod, g)
	}
	// Boş = tabana dön, sütun boş.
	if kod, g := ekWRPut(t, ek_dom1, eid, `{"alt_dizin":""}`); kod != 200 {
		t.Fatalf("sıfırlama: %d %s", kod, g)
	}
	if ekWRDBdeger(t, eid) != "" || !strings.Contains(ekWRVhost(t, ad), "root "+taban+";") {
		t.Fatal("sıfırlama tabana dönmedi")
	}
}

func TestEkWebRoot_Kacislar_Reddedilir(t *testing.T) {
	d, eid, ad := ekWRKur(t)
	taban := ekDocroot(ek_sk1, ad)
	dis := filepath.Join(d.kok, "dis-hedef")
	baskaKiraci := filepath.Join(evKoku, ek_sk2, "domains", "x.org", "public_html")
	for _, p := range []string{dis, baskaKiraci, taban + "/gercek"} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// leaf symlink (dışarı), ara-bileşen symlink (a → dış; a/dis içinde), başka kiracıya symlink.
	if err := os.Symlink(dis, taban+"/lnk"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(baskaKiraci, taban+"/baska"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dis+"/alt", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dis, taban+"/ara"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(taban+"/dosya.txt", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	vhost0 := ekWRVhost(t, ad)
	reload0 := d.say("systemctl reload nginx")
	for _, kotu := range []string{
		"..", "../", "../x", "gercek/../..", "a/../../b", "/etc", "/etc/passwd", dis, baskaKiraci,
		"../../" + ek_sk2 + "/domains/x.org/public_html", "x;y", "a b", "a\\nb", "yok-dizin",
		"lnk", "baska", "ara/alt", "dosya.txt", "gercek/../lnk", "%2e%2e",
	} {
		kod, g := ekWRPut(t, ek_dom1, eid, `{"alt_dizin":"`+strings.ReplaceAll(kotu, `\n`, `\\n`)+`"}`)
		if kod != http.StatusBadRequest {
			t.Errorf("%q: 400 beklenir, %d %s", kotu, kod, g)
		}
	}
	if ekWRDBdeger(t, eid) != "" {
		t.Fatal("reddedilen istek DB'yi değiştirdi")
	}
	if ekWRVhost(t, ad) != vhost0 || d.say("systemctl reload nginx") != reload0 {
		t.Fatal("reddedilen istek vhost'a/nginx'e dokundu")
	}
	// Bozuk gövde.
	if kod, _ := ekWRPut(t, ek_dom1, eid, `{`); kod != http.StatusBadRequest {
		t.Fatalf("bozuk gövde 400 değil: %d", kod)
	}
}

func TestEkWebRoot_RolKapsami_BaskasininEkAlani404(t *testing.T) {
	d, eid, ad := ekWRKur(t)
	taban := ekDocroot(ek_sk1, ad)
	if err := os.MkdirAll(taban+"/public", 0o755); err != nil {
		t.Fatal(err)
	}
	vhost0 := ekWRVhost(t, ad)
	reload0 := d.say("systemctl reload nginx")
	// Saldırgan hesap 2'nin {id}'siyle hesap 1'in {eid}'si.
	if kod, g := ekWRPut(t, ek_dom2, eid, `{"alt_dizin":"public"}`); kod != http.StatusNotFound {
		t.Fatalf("PUT IDOR: 404 beklenir: %d %s", kod, g)
	}
	if kod, g := ekWRGet(t, ek_dom2, eid); kod != http.StatusNotFound {
		t.Fatalf("GET IDOR: 404 beklenir: %d %s", kod, g)
	}
	if ekWRDBdeger(t, eid) != "" || ekWRVhost(t, ad) != vhost0 || d.say("systemctl reload nginx") != reload0 {
		t.Fatal("IDOR denemesi yan etki üretti")
	}
}

func TestEkWebRoot_Demo403(t *testing.T) {
	_, eid, ad := ekWRKur(t)
	if err := os.MkdirAll(ekDocroot(ek_sk1, ad)+"/public", 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ekWRDB.Exec(`UPDATE domains SET is_demo=1 WHERE id=?`, ek_dom1); err != nil {
		t.Fatal(err)
	}
	if kod, g := ekWRPut(t, ek_dom1, eid, `{"alt_dizin":"public"}`); kod != http.StatusForbidden {
		t.Fatalf("demo: 403 beklenir: %d %s", kod, g)
	}
	if ekWRDBdeger(t, eid) != "" {
		t.Fatal("demo hesapta web_root değişti")
	}
}

func TestEkWebRoot_NginxTBasarisiz_DBVeVhostEskiKalir(t *testing.T) {
	d, eid, ad := ekWRKur(t)
	taban := ekDocroot(ek_sk1, ad)
	if err := os.MkdirAll(taban+"/public", 0o755); err != nil {
		t.Fatal(err)
	}
	vhost0 := ekWRVhost(t, ad)
	*d.nginxHata = true
	kod, g := ekWRPut(t, ek_dom1, eid, `{"alt_dizin":"public"}`)
	*d.nginxHata = false
	if kod != http.StatusInternalServerError {
		t.Fatalf("nginx -t hatası 500 olmalı: %d %s", kod, g)
	}
	if ekWRDBdeger(t, eid) != "" {
		t.Fatal("nginx -t başarısızken DB eski değere dönmedi")
	}
	if ekWRVhost(t, ad) != vhost0 {
		t.Fatal("nginx -t başarısızken vhost eski haline dönmedi")
	}
}

func TestEkWebRootGuvenli_KurcalanmisDBDegeriTabanaDuser(t *testing.T) {
	ek_dikisler(t)
	taban := ekDocroot(ek_sk1, "wr.example.org")
	for _, kotu := range []string{"/etc", "/etc/passwd", taban + "/../../" + ek_sk2, evKoku + "/" + ek_sk2 + "/domains/x.org/public_html", taban + "x"} {
		if g := ekWebRootGuvenli(ek_sk1, "wr.example.org", kotu); g != taban {
			t.Errorf("%q → %q; taban beklenir", kotu, g)
		}
	}
	if g := ekWebRootGuvenli(ek_sk1, "wr.example.org", taban+"/public"); g != taban+"/public" {
		t.Errorf("geçerli alt dizin bozuldu: %q", g)
	}
	gv, err := ekVhostGovdeKok(ek_sk1, "wr.example.org", "8.3", "/etc")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gv, "root "+taban+";") || strings.Contains(gv, "root /etc;") {
		t.Fatal("kurcalanmış web_root vhost'a sızdı")
	}
}
