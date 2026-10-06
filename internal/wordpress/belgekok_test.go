package wordpress

// belgekok_test.go — WordPress kurulum ÖN KONTROLÜ: servis edilen belge kökü (domains.web_root)
// ile kurulum hedefi uyuşuyor mu. Canlı olay: Laravel → web_root=public_html/public, dosyalar
// panel dışından silindi, köke WordPress kuruldu → site 404, panel sessiz.
// DB'li (geçici MariaDB; go_test db:true), dış etki yok: vhost yeniden kurma ve Laravel kaydı
// temizliği sahte; dosya sistemi t.TempDir().

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"girginospanel/internal/belgekok"
)

type bkOrtam struct {
	*whOrtam
	base  string // c_wpa public_html
	mu2   sync.Mutex
	rr    int // RerenderFn çağrı sayısı
	lk    int // laravelKaldirFn çağrı sayısı
	rrErr error
}

func bk_ortam(t *testing.T) *bkOrtam {
	t.Helper()
	o := &bkOrtam{whOrtam: wh_ortam(t)}
	o.base = o.ev + "/" + wh_sk1 + "/public_html"
	eskiRR, eskiLK := belgekok.RerenderFn, laravelKaldirFn
	belgekok.RerenderFn = func(db *sql.DB, id int64) error {
		o.mu2.Lock()
		defer o.mu2.Unlock()
		o.rr++
		return o.rrErr
	}
	laravelKaldirFn = func(ctx context.Context, db *sql.DB, id int64, sk string) (bool, error) {
		o.mu2.Lock()
		o.lk++
		o.mu2.Unlock()
		_, err := db.ExecContext(ctx, `DELETE FROM cp_laravel_apps WHERE domain_id=?`, id)
		return false, err
	}
	t.Cleanup(func() { belgekok.RerenderFn, laravelKaldirFn = eskiRR, eskiLK })
	return o
}

func (o *bkOrtam) webRoot(t *testing.T, id int) string {
	t.Helper()
	var w string
	if err := o.db.QueryRow(`SELECT web_root FROM domains WHERE id=?`, id).Scan(&w); err != nil {
		t.Fatal(err)
	}
	return w
}

func (o *bkOrtam) ayarla(t *testing.T, id int, webRoot string) {
	t.Helper()
	if _, err := o.db.Exec(`UPDATE domains SET web_root=? WHERE id=?`, webRoot, id); err != nil {
		t.Fatal(err)
	}
}

func (o *bkOrtam) laravelSatiri(t *testing.T, id int, appRoot string) {
	t.Helper()
	if _, err := o.db.Exec(`INSERT INTO cp_laravel_apps (domain_id, app_root) VALUES (?,?)`, id, appRoot); err != nil {
		t.Fatal(err)
	}
}

func (o *bkOrtam) cagri() (rr, lk int) {
	o.mu2.Lock()
	defer o.mu2.Unlock()
	return o.rr, o.lk
}

type bkYanit struct {
	SiteURL    string `json:"site_url"`
	Uyari      string `json:"uyari"`
	UyariKod   string `json:"uyari_kod"`
	Duzeltildi bool   `json:"belge_koku_duzeltildi"`
	Hata       string `json:"hata"`
	Kod        string `json:"kod"`
	Servis     string `json:"servis_koku"`
	Hedef      string `json:"hedef"`
}

func bk_coz(t *testing.T, govde string) bkYanit {
	t.Helper()
	var y bkYanit
	if err := json.Unmarshal([]byte(govde), &y); err != nil {
		t.Fatalf("json: %v (%s)", err, govde)
	}
	return y
}

// (a) Uyuşan hedef: web_root boş (varsayılan) → hiçbir şeye dokunulmaz, uyarı yok.
func TestBelgeKok_Uyusan_DokunulmazUyariYok(t *testing.T) {
	o := bk_ortam(t)
	kod, govde := wh_kur(o.whOrtam, "/domains/1/wordpress", "")
	if kod != 200 {
		t.Fatalf("%d %s", kod, govde)
	}
	y := bk_coz(t, govde)
	if y.Uyari != "" || y.Duzeltildi || y.SiteURL != "https://"+wh_ana1 {
		t.Fatalf("uyuşan hedefte uyarı/düzeltme olmamalı: %+v", y)
	}
	if rr, lk := o.cagri(); rr != 0 || lk != 0 {
		t.Fatalf("vhost/Laravel dokunulmamalı: rr=%d lk=%d", rr, lk)
	}
	if o.webRoot(t, 1) != "" {
		t.Fatal("web_root değişmemeli")
	}
}

// Alt dizin + varsayılan servis kökü: adres /blog (eski davranışla birebir).
func TestBelgeKok_AltDizin_VarsayilanKok_URLBlog(t *testing.T) {
	o := bk_ortam(t)
	kod, govde := wh_kur(o.whOrtam, "/domains/1/wordpress", "blog")
	if kod != 200 || bk_coz(t, govde).SiteURL != "https://"+wh_ana1+"/blog" {
		t.Fatalf("%d %s", kod, govde)
	}
}

// Servis kökü == hedef (web_root bilinçli olarak public_html/blog): uyumlu; adres alan adı kökü.
func TestBelgeKok_ServisKoku_HedefeEsit_Uyumlu_URLKok(t *testing.T) {
	o := bk_ortam(t)
	if err := os.MkdirAll(o.base+"/blog", 0o755); err != nil {
		t.Fatal(err)
	}
	o.ayarla(t, 1, o.base+"/blog")
	kod, govde := wh_kur(o.whOrtam, "/domains/1/wordpress", "blog")
	if kod != 200 {
		t.Fatalf("%d %s", kod, govde)
	}
	y := bk_coz(t, govde)
	if y.SiteURL != "https://"+wh_ana1 || y.Uyari != "" {
		t.Fatalf("servis kökü=hedef: adres kök olmalı, uyarı yok: %+v", y)
	}
	if o.webRoot(t, 1) != o.base+"/blog" {
		t.Fatal("web_root değişmemeli")
	}
}

// (b) Bayat web_root (dizin YOK) + Laravel kaydı + artisan YOK → web_root public_html'e döner,
// vhost yeniden kurulur, bayat Laravel satırı temizlenir.
func TestBelgeKok_BayatWebRoot_Duzeltilir_LaravelSatiriTemizlenir(t *testing.T) {
	o := bk_ortam(t)
	o.ayarla(t, 1, o.base+"/public") // dizin yok
	o.laravelSatiri(t, 1, "public_html")
	kod, govde := wh_kur(o.whOrtam, "/domains/1/wordpress", "")
	if kod != 200 {
		t.Fatalf("%d %s", kod, govde)
	}
	y := bk_coz(t, govde)
	if !y.Duzeltildi || y.Uyari != "" {
		t.Fatalf("düzeltilmeli, uyarı olmamalı: %+v", y)
	}
	if got := o.webRoot(t, 1); got != o.base {
		t.Fatalf("web_root public_html olmalı: %q", got)
	}
	if rr, lk := o.cagri(); rr != 1 || lk != 1 {
		t.Fatalf("vhost 1 kez, Laravel temizliği 1 kez: rr=%d lk=%d", rr, lk)
	}
	if n := o.sayi(t, `SELECT COUNT(*) FROM cp_laravel_apps WHERE domain_id=1`); n != 0 {
		t.Fatalf("bayat Laravel satırı kalmamalı: %d", n)
	}
}

// Yalnız web_root bayat, Laravel kaydı HİÇ yok → düzeltilir, Laravel temizliği çağrılmaz.
func TestBelgeKok_BayatWebRoot_LaravelKaydiYok_SadeceWebRoot(t *testing.T) {
	o := bk_ortam(t)
	o.ayarla(t, 1, o.base+"/public")
	kod, govde := wh_kur(o.whOrtam, "/domains/1/wordpress", "")
	if kod != 200 || !bk_coz(t, govde).Duzeltildi {
		t.Fatalf("%d %s", kod, govde)
	}
	if rr, lk := o.cagri(); rr != 1 || lk != 0 {
		t.Fatalf("rr=%d lk=%d", rr, lk)
	}
}

// Servis dizini VAR ama kayıtlı Laravel'in artisan'ı yok (uygulama silinmiş) → düzeltilir.
func TestBelgeKok_LaravelSilinmis_ArtisanYok_ServisDizinVar_Duzeltilir(t *testing.T) {
	o := bk_ortam(t)
	if err := os.MkdirAll(o.base+"/public", 0o755); err != nil {
		t.Fatal(err)
	}
	o.ayarla(t, 1, o.base+"/public")
	o.laravelSatiri(t, 1, "public_html")
	kod, govde := wh_kur(o.whOrtam, "/domains/1/wordpress", "blog")
	if kod != 200 || !bk_coz(t, govde).Duzeltildi || o.webRoot(t, 1) != o.base {
		t.Fatalf("%d %s web_root=%q", kod, govde, o.webRoot(t, 1))
	}
}

// (c) Canlı Laravel (artisan var, web_root=public mevcut) + alt dizine kurulum → 409,
// kurulum BAŞLATILMAZ: hiçbir komut, dizin, DB, web_root ya da Laravel satırı değişimi yok.
func TestBelgeKok_CanliLaravel_409_YanEtkisiz(t *testing.T) {
	o := bk_ortam(t)
	wh_yaz(t, o.base+"/artisan", "<?php")
	wh_yaz(t, o.base+"/public/index.php", "<?php")
	o.ayarla(t, 1, o.base+"/public")
	o.laravelSatiri(t, 1, "public_html")
	kod, govde := wh_kur(o.whOrtam, "/domains/1/wordpress", "blog")
	if kod != 409 {
		t.Fatalf("409 bekleniyordu: %d %s", kod, govde)
	}
	y := bk_coz(t, govde)
	if y.Kod != KodBelgeKokUyusmuyor || y.Servis != "public_html/public" || y.Hedef != "public_html/blog" ||
		!strings.Contains(y.Hata, "servis edilen belge kökü") {
		t.Fatalf("409 gövdesi: %+v", y)
	}
	if strings.Contains(govde, o.ev) {
		t.Fatalf("mutlak yol sızdı: %s", govde)
	}
	if len(o.komutlar()) != 0 {
		t.Fatalf("kurulum başlatılmamalıydı: %v", o.komutlar())
	}
	if _, err := os.Stat(o.base + "/blog"); err == nil {
		t.Fatal("hedef dizin oluşturulmamalı")
	}
	if n := o.sayi(t, `SELECT COUNT(*) FROM db_accounts`); n != 0 {
		t.Fatalf("DB açılmamalı: %d", n)
	}
	if o.webRoot(t, 1) != o.base+"/public" || o.sayi(t, `SELECT COUNT(*) FROM cp_laravel_apps WHERE domain_id=1`) != 1 {
		t.Fatal("web_root/Laravel kaydı değişmemeli")
	}
	if rr, lk := o.cagri(); rr != 0 || lk != 0 {
		t.Fatalf("rr=%d lk=%d", rr, lk)
	}
}

// Elle (bilinçli) ayarlı web_root: dizin var, Laravel kaydı yok → 409, DOKUNULMAZ.
func TestBelgeKok_ElleAyarliWebRoot_409_DokunulmazDuzeltilmez(t *testing.T) {
	o := bk_ortam(t)
	if err := os.MkdirAll(o.base+"/ozel", 0o755); err != nil {
		t.Fatal(err)
	}
	o.ayarla(t, 1, o.base+"/ozel")
	kod, govde := wh_kur(o.whOrtam, "/domains/1/wordpress", "blog")
	if kod != 409 || bk_coz(t, govde).Kod != KodBelgeKokUyusmuyor {
		t.Fatalf("%d %s", kod, govde)
	}
	if o.webRoot(t, 1) != o.base+"/ozel" {
		t.Fatal("elle ayarlı web_root değişti")
	}
	if rr, _ := o.cagri(); rr != 0 || len(o.komutlar()) != 0 {
		t.Fatal("yan etki olmamalı")
	}
}

// (d) Kurulum sonrası doğrulama: uyumlu kök ama wp-login.php yok → kurulum başarılı + UYARI.
func TestBelgeKok_KurulumSonrasi_DosyaCozulmuyor_Uyari(t *testing.T) {
	o := bk_ortam(t)
	onceki := wpKomutFn
	wpKomutFn = func(sk string, args ...string) ([]byte, error) {
		out, err := onceki(sk, args...)
		_ = os.Remove(o.base + "/wp-login.php") // çıkarma eksik kaldı
		return out, err
	}
	kod, govde := wh_kur(o.whOrtam, "/domains/1/wordpress", "")
	if kod != 200 {
		t.Fatalf("%d %s", kod, govde)
	}
	y := bk_coz(t, govde)
	if y.UyariKod != KodBelgeKokSunulmuyor || y.Uyari == "" || y.Servis != "public_html" || y.Hedef != "public_html" {
		t.Fatalf("uyarı beklenir: %+v", y)
	}
}

// (d) Düzeltme başarısız (nginx -t geri aldı): kurulum başarılı + UYARI; web_root eski değere DÖNER.
func TestBelgeKok_DuzeltmeBasarisiz_UyariVeGeriAlma(t *testing.T) {
	o := bk_ortam(t)
	o.ayarla(t, 1, o.base+"/public")
	o.rrErr = errors.New("nginx -t başarısız")
	kod, govde := wh_kur(o.whOrtam, "/domains/1/wordpress", "")
	if kod != 200 {
		t.Fatalf("%d %s", kod, govde)
	}
	y := bk_coz(t, govde)
	if y.UyariKod != KodBelgeKokSunulmuyor || y.Duzeltildi {
		t.Fatalf("uyarı + düzeltilmedi bekleniyordu: %+v", y)
	}
	if o.webRoot(t, 1) != o.base+"/public" {
		t.Fatalf("render başarısızsa DB eski değere dönmeli: %q", o.webRoot(t, 1))
	}
	if _, lk := o.cagri(); lk != 0 {
		t.Fatal("düzeltme olmadan Laravel kaydı temizlenmemeli")
	}
}

// IDOR: domain 1'e kurulum, domain 2'nin web_root'una / Laravel kaydına ASLA dokunmaz.
func TestBelgeKok_IDOR_BaskaHesapDokunulmaz(t *testing.T) {
	o := bk_ortam(t)
	o.ayarla(t, 1, o.base+"/public")
	b2 := o.ev + "/" + wh_sk2 + "/public_html"
	o.ayarla(t, 2, b2+"/public") // domain 2 de bayat
	if _, err := o.db.Exec(`INSERT INTO cp_laravel_apps (domain_id, app_root) VALUES (2,'public_html')`); err != nil {
		t.Fatal(err)
	}
	if kod, g := wh_kur(o.whOrtam, "/domains/1/wordpress", ""); kod != 200 {
		t.Fatalf("%d %s", kod, g)
	}
	if o.webRoot(t, 2) != b2+"/public" || o.sayi(t, `SELECT COUNT(*) FROM cp_laravel_apps WHERE domain_id=2`) != 1 {
		t.Fatal("başka hesabın web_root/Laravel kaydı değişti")
	}
	// Başka hesabın kimliği altında ek/alt alan: ana-kök kontrolü uygulanmaz, 404 (mevcut IDOR kapısı).
	if kod, _ := wh_kur(o.whOrtam, "/domains/1/ek-alan-adlari/21/wordpress", ""); kod != 404 {
		t.Fatalf("yabancı ek alan 404 olmalı: %d", kod)
	}
}

// Kurcalanmış DB değeri (hesap dışı yol) servis kökü sayılmaz: public_html'e düşer, uyumlu.
func TestBelgeKok_KurcalanmisWebRoot_TabanaDuser(t *testing.T) {
	o := bk_ortam(t)
	o.ayarla(t, 1, "/etc")
	kod, govde := wh_kur(o.whOrtam, "/domains/1/wordpress", "")
	if kod != 200 || bk_coz(t, govde).Uyari != "" {
		t.Fatalf("%d %s", kod, govde)
	}
	if o.webRoot(t, 1) != "/etc" {
		t.Fatal("uyumlu yolda web_root'a dokunulmaz")
	}
}
