package denetim

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"girginospanel/internal/auth"
	"girginospanel/internal/sahtedb"
)

// Bayi YALNIZ kendi yaptıklarını görür: koşul işlemi yapana (actor_user_id) göre,
// kapsama (reseller_id) göre DEĞİL — kapsamındaki hostingde kökün yaptığı işlem
// bayiye gösterilmez. İstemcinin kapsam parametresi bayide yok sayılır.
func TestKapsamKurBayiYalnizKendiYaptiklari(t *testing.T) {
	bayi := &auth.Claims{UserID: 7, Username: "bayi1", Role: "reseller", ResellerID: 7}
	for _, kap := range []string{"", "kok", "3", "x"} {
		kosul, args, err := kapsamKur(bayi, kap)
		if err != nil {
			t.Fatalf("kapsam=%q: bayide istemci parametresi yok sayılmalı, hata: %v", kap, err)
		}
		if !reflect.DeepEqual(kosul, []string{"1=1", "a.actor_user_id=?"}) || !reflect.DeepEqual(args, []any{int64(7)}) {
			t.Fatalf("kapsam=%q: bayi yalnız kendi yaptıklarını görmeli: %v %v", kap, kosul, args)
		}
		if strings.Contains(strings.Join(kosul, " "), "reseller_id") {
			t.Fatalf("kapsam=%q: bayi kapsamına göre süzülmemeli (kökün işlemleri de görünür): %v", kap, kosul)
		}
	}
	// Rol ya da sahiplik anahtarından biri eksik olsa da bayi yönetici görünürlüğüne düşmez.
	for _, c := range []*auth.Claims{
		{UserID: 7, Role: "reseller"},
		{UserID: 7, Role: "", ResellerID: 7},
	} {
		if kosul, _, _ := kapsamKur(c, ""); len(kosul) != 2 || kosul[1] != "a.actor_user_id=?" {
			t.Fatalf("%+v: bayi olarak süzülmeli: %v", c, kosul)
		}
	}
	// Kimliksiz bayi jetonu: hiçbir şey (başkasınınki asla).
	if kosul, args, err := kapsamKur(&auth.Claims{Role: "reseller", ResellerID: 7}, ""); err != nil ||
		!reflect.DeepEqual(kosul, []string{"1=1", "1=0"}) || len(args) != 0 {
		t.Fatalf("kimliksiz bayi hiçbir kayıt görmemeli: %v %v %v", kosul, args, err)
	}
}

func TestKapsamKurYonetici(t *testing.T) {
	kok := &auth.Claims{UserID: 1, Username: "root", Role: "admin"}
	cases := []struct {
		kap   string
		kosul []string
		args  []any
		hata  bool
	}{
		{"", []string{"1=1"}, []any{}, false},
		{"kok", []string{"1=1", "a.reseller_id=0"}, []any{}, false},
		{" 5 ", []string{"1=1", "a.reseller_id=?"}, []any{int64(5)}, false},
		{"x", nil, nil, true},
		{"0", nil, nil, true},
		{"-2", nil, nil, true},
	}
	for _, c := range cases {
		kosul, args, err := kapsamKur(kok, c.kap)
		if (err != nil) != c.hata {
			t.Fatalf("kapsam=%q: hata=%v beklenen hata=%v", c.kap, err, c.hata)
		}
		if !c.hata && (!reflect.DeepEqual(kosul, c.kosul) || !reflect.DeepEqual(args, c.args)) {
			t.Fatalf("kapsam=%q: %v %v, beklenen %v %v", c.kap, kosul, args, c.kosul, c.args)
		}
	}
}

// Uç: bayi isteğinde ÜÇ sorgu da (sayaç, liste, eylem açılır listesi) aktör süzgecini
// taşır — biri eksik kalırsa sayaç ya da eylem listesi kökün işlemlerini sızdırır.
func TestListeBayiTumSorgularAktorSuzgecli(t *testing.T) {
	sd, db := sahtedb.Yeni()
	eski := claimsAl
	t.Cleanup(func() { claimsAl = eski })
	claimsAl = func(*http.Request) *auth.Claims {
		return &auth.Claims{UserID: 7, Username: "bayi1", Role: "reseller", ResellerID: 7}
	}
	h := &Handlers{DB: db}
	rr := httptest.NewRecorder()
	h.Liste(rr, httptest.NewRequest("GET", "/denetim?kapsam=kok&eylem=hosting.sil&ara=x", nil))
	if rr.Code != 200 {
		t.Fatalf("200 beklenirdi: %d %s", rr.Code, rr.Body)
	}
	for _, parca := range []string{
		"SELECT COUNT(*) FROM audit_log a WHERE 1=1 AND a.actor_user_id=?",
		"LEFT JOIN users ru ON ru.id = a.reseller_id WHERE 1=1 AND a.actor_user_id=?",
		"SELECT DISTINCT a.action FROM audit_log a WHERE 1=1 AND a.actor_user_id=?",
	} {
		if !sd.Sorgulandi(parca) {
			t.Fatalf("sorgu aktör süzgeci taşımalı: %q", parca)
		}
	}
	if sd.Sorgulandi("a.reseller_id=0") || sd.Sorgulandi("a.reseller_id=?") {
		t.Fatal("bayide istemcinin kapsam parametresi ya da kapsam süzgeci kullanılmamalı")
	}
	if !strings.Contains(rr.Body.String(), `"bayi_mi":true`) {
		t.Fatalf("yanıt bayi görünümünü bildirmeli: %s", rr.Body)
	}
}

// Yönetici görünümü değişmez: kapsam süzgeci yalnız istenince, aktör süzgeci yok.
func TestListeYoneticiAktorSuzgecsiz(t *testing.T) {
	sd, db := sahtedb.Yeni()
	eski := claimsAl
	t.Cleanup(func() { claimsAl = eski })
	claimsAl = func(*http.Request) *auth.Claims { return &auth.Claims{UserID: 1, Username: "root", Role: "admin"} }
	h := &Handlers{DB: db}
	rr := httptest.NewRecorder()
	h.Liste(rr, httptest.NewRequest("GET", "/denetim?kapsam=7", nil))
	if rr.Code != 200 || sd.Sorgulandi("actor_user_id=?") || !sd.Sorgulandi("a.reseller_id=?") {
		t.Fatalf("yönetici kapsam=7 → reseller_id süzgeci, aktör süzgeci yok: %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	h.Liste(rr, httptest.NewRequest("GET", "/denetim?kapsam=abc", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("çözülemeyen kapsam 400 olmalı: %d", rr.Code)
	}
}
