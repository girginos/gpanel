package kaynaklimit

// bayi_havuz_test.go — hosting plan diliminin bayi kaynak havuzuyla ilişkisi: dilim adı üyeliğe
// göre yuvalanır, plan limitleri havuzun İÇİNDE aynen yazılır, üyelik değişince dosyalar yeni
// ada taşınır, plan kalkınca / hosting silinince TÜM ad varyantları temizlenir.

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/bayidilim"
	"girginospanel/internal/bayikaynak"
	"girginospanel/internal/provisioner"
)

type bhOrtam struct {
	Dizin, Run string
	mu         sync.Mutex
	Cagri      [][]string
}

func (o *bhOrtam) say(onek string) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	n := 0
	for _, c := range o.Cagri {
		if strings.HasPrefix(strings.Join(c, " "), onek) {
			n++
		}
	}
	return n
}

// bhKur: geçici birim/run dizinleri, geçici üyelik dizini ve kayıtlı sahte systemctl.
func bhKur(t *testing.T, aktif bool) *bhOrtam {
	t.Helper()
	o := &bhOrtam{Dizin: t.TempDir(), Run: t.TempDir()}
	eD, eR, eU, eS, eL, eT := sliceDir, runKontrolDizini, bayidilim.UyelikDizini, systemctlCalistir, planLimitleri, tenantSokumde
	t.Cleanup(func() {
		sliceDir, runKontrolDizini, bayidilim.UyelikDizini, systemctlCalistir, planLimitleri, tenantSokumde = eD, eR, eU, eS, eL, eT
	})
	tenantSokumde = func(string) bool { return false }
	sliceDir, runKontrolDizini = o.Dizin, o.Run
	bayidilim.UyelikDizini = filepath.Join(t.TempDir(), "uyelik")
	systemctlCalistir = func(args ...string) ([]byte, error) {
		o.mu.Lock()
		o.Cagri = append(o.Cagri, args)
		o.mu.Unlock()
		if len(args) > 0 && args[0] == "is-active" {
			if aktif {
				return []byte("active\n"), nil
			}
			return []byte("inactive\n"), fmt.Errorf("3")
		}
		return nil, nil
	}
	return o
}

// Mevcut (havuzsuz) hostinglerin dilim dosyası AYNEN korunur: değişiklik yalnız üyelerde.
func TestSliceIcerik_AltinMetin(t *testing.T) {
	got := sliceIcerik("c_x", Limitler{CPUYuzde: 150, RAMMB: 1000, MaxProcess: 20, IOAgirlik: 300, IOWriteMBps: 15})
	want := "# GirginOSPanel per-domain resource slice — c_x\n[Unit]\nDescription=GirginOS panel slice for c_x\nBefore=slices.target\n\n[Slice]\n" +
		"CPUAccounting=yes\nMemoryAccounting=yes\nTasksAccounting=yes\nIOAccounting=yes\n\n" +
		"CPUQuota=150%\nMemoryMax=1000M\nMemoryHigh=900M\nTasksMax=20\nIOWeight=300\nIOWriteBandwidthMax=/home 15M\n"
	if got != want {
		t.Errorf("dilim içeriği değişti:\n%s\n---\n%s", got, want)
	}
	// Limit verilmemiş alanlar eski varsayılanlara düşer (mevcut davranış).
	d := sliceIcerik("c_y", Limitler{})
	for _, s := range []string{"CPUQuota=100%\n", "MemoryMax=512M\n", "TasksMax=50\n", "IOWeight=100\n"} {
		if !strings.Contains(d, s) {
			t.Errorf("varsayılan yok: %q", s)
		}
	}
}

func TestSliceName_UyelikYuvalar(t *testing.T) {
	bhKur(t, false)
	if sliceName("c_x") != "girginos-c_x.slice" || slicePath("c_x") != filepath.Join(sliceDir, "girginos-c_x.slice") {
		t.Fatalf("üyelik yokken ad %q", sliceName("c_x"))
	}
	_, _ = bayidilim.UyelikYaz("c_x", 12)
	if sliceName("c_x") != "girginos-bayi12-c_x.slice" {
		t.Errorf("üye ad %q", sliceName("c_x"))
	}
	// Laravel gibi gevşek çağıranlar geçersiz sk verse de ad ASLA boş olmaz (boş Slice= = sınırsız).
	if sliceName("c_Weird-Name") == "" {
		t.Error("boş dilim adı")
	}
}

func TestSliceAdaylari_YalnizKendiAdlari(t *testing.T) {
	o := bhKur(t, false)
	for _, ad := range []string{"girginos-bayi5-c_a.slice", "girginos-bayi7-c_a.slice", "girginos-bayi5-c_b.slice", "girginos-c_a.slice", "girginos-bayi5-c_a.slice.bak"} {
		_ = os.WriteFile(filepath.Join(o.Dizin, ad), []byte("x"), 0o644)
	}
	_, _ = bayidilim.UyelikYaz("c_a", 5)
	got := sliceAdaylari("c_a")
	want := map[string]bool{"girginos-bayi5-c_a.slice": true, "girginos-c_a.slice": true, "girginos-bayi7-c_a.slice": true}
	if len(got) != 3 || got[0] != "girginos-bayi5-c_a.slice" {
		t.Errorf("adaylar %v (güncel ad önce gelmeli)", got)
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("beklenmeyen aday %q (başka hostingin / yedek dosyası)", g)
		}
	}
	// Bir hostingin adayları başka hostingin dilimlerini içermez (c_b'nin dosyası c_a'nın adayı değil).
	for _, g := range got {
		if strings.Contains(g, "c_b") {
			t.Errorf("başka hostingin dilimi aday: %q", g)
		}
	}
	// Geçersiz sk: yalnız güncel ad (glob deseni oluşmaz).
	if g := sliceAdaylari("c_a/../x"); len(g) != 1 {
		t.Errorf("geçersiz sk adayları: %v", g)
	}
}

func TestPlanDilimiYazHafif_IdempotentVeOverrideTemizler(t *testing.T) {
	o := bhKur(t, false)
	_, _ = bayidilim.UyelikYaz("c_x", 3)
	l := Limitler{CPUYuzde: 100, RAMMB: 256, MaxProcess: 10}
	ov := filepath.Join(o.Run, "girginos-bayi3-c_x.slice.d")
	_ = os.MkdirAll(ov, 0o755)
	_ = os.WriteFile(filepath.Join(ov, "50-x.conf"), []byte("[Slice]\nMemoryMax=1M\n"), 0o644)
	d, err := planDilimiYazHafif("c_x", l)
	if err != nil || !d {
		t.Fatalf("ilk yazım: %v %v", d, err)
	}
	yol := filepath.Join(o.Dizin, "girginos-bayi3-c_x.slice")
	if b, _ := os.ReadFile(yol); string(b) != sliceIcerik("c_x", l) {
		t.Errorf("içerik: %q", b)
	}
	if _, err := os.Stat(ov); err == nil {
		t.Error("eski runtime override silinmedi (yeni dosya değerini ezerdi)")
	}
	if d, err := planDilimiYazHafif("c_x", l); err != nil || d {
		t.Errorf("idempotent değil: %v %v", d, err)
	}
}

func bhDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li test atlandı")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = ""
	kok, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	ad := fmt.Sprintf("gosp_test_bhavuz_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad); _ = kok.Close() })
	cfg.DBName = ad
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE domains (id BIGINT PRIMARY KEY, sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '')`); err != nil {
		t.Fatal(err)
	}
	return db
}

// Üyelik değişince plan dilimi yeni adla yazılır: havuza giren → yuvalı ad, çıkan → eski düz ad,
// planı olmayan → dosya YOK, hata bir hostingi atlatır. TEK daemon-reload.
func TestUyelikDegisti_PlanDilimleriYeniAdlaYazilir(t *testing.T) {
	o := bhKur(t, false)
	db := bhDB(t)
	for _, q := range []string{`INSERT INTO domains VALUES (1,'c_giren'),(2,'c_cikan'),(3,'c_plansiz'),(4,'c_hata'),(5,'c_sokumde')`} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	planLimitleri = func(_ context.Context, _ *sql.DB, id int64) (Limitler, error) {
		switch id {
		case 1:
			return Limitler{CPUYuzde: 50, RAMMB: 300, MaxProcess: 9}, nil
		case 2:
			return Limitler{CPUYuzde: 200, RAMMB: 800, MaxProcess: 30}, nil
		case 3:
			return Limitler{}, nil // plansız
		case 5, 6:
			return Limitler{CPUYuzde: 80, RAMMB: 200, MaxProcess: 5}, nil
		}
		// Hata dönen çağrının kısmi değeri KULLANILMAZ.
		return Limitler{CPUYuzde: 10, RAMMB: 100, MaxProcess: 3}, fmt.Errorf("okunamadı")
	}
	_, _ = bayidilim.UyelikYaz("c_giren", 5) // Uzlastir üyeliği önceden yazmıştır
	// Sökümü süren hostingin dilimi YENİDEN YAZILMAZ (kalkan hostingi diriltmek olurdu).
	tenantSokumde = func(sk string) bool { return sk == "c_sokumde" }
	r := bayikaynak.Rapor{
		UyelikEklenen: []bayikaynak.Uye{{DomainID: 1, BayiID: 5, SK: "c_giren"}, {DomainID: 3, BayiID: 5, SK: "c_plansiz"}, {DomainID: 4, BayiID: 5, SK: "c_hata"}, {DomainID: 5, BayiID: 5, SK: "c_sokumde"}, {DomainID: 6, BayiID: 5, SK: "c_Gecersiz-Ad"}},
		UyelikKalkan:  []string{"c_cikan"},
	}
	uyelikDegisti(context.Background(), db, r)
	b, err := os.ReadFile(filepath.Join(o.Dizin, "girginos-bayi5-c_giren.slice"))
	if err != nil || string(b) != sliceIcerik("c_giren", Limitler{CPUYuzde: 50, RAMMB: 300, MaxProcess: 9}) {
		t.Errorf("havuza giren hostingin yuvalı dilimi: %v %q", err, b)
	}
	b, err = os.ReadFile(filepath.Join(o.Dizin, "girginos-c_cikan.slice"))
	if err != nil || !strings.Contains(string(b), "CPUQuota=200%") {
		t.Errorf("havuzdan çıkan hostingin eski düz dilimi: %v %q", err, b)
	}
	ents, _ := os.ReadDir(o.Dizin)
	if len(ents) != 2 {
		t.Errorf("planı olmayan / hatalı / sökümdeki / geçersiz adlı hosting için dosya yazıldı: %v", ents)
	}
	if n := o.say("daemon-reload"); n != 1 {
		t.Errorf("daemon-reload %d kez (1 beklenir)", n)
	}
	// İdempotent: ikinci çağrı dosyaya da systemd'ye de dokunmaz.
	o.Cagri = nil
	uyelikDegisti(context.Background(), db, r)
	if n := o.say("daemon-reload"); n != 0 {
		t.Errorf("değişiklik yokken daemon-reload %d kez", n)
	}
}

// Plan kalkarken / hosting silinirken: güncel ad + eski düz ad + başka havuzlardan kalan yuvalı adlar
// DURDURULUR ve dosyaları/override'ları silinir; başka hostingin dilimine dokunulmaz. Üyelik kaydı
// plan kalkınca KORUNUR (yalnız tam sökümde silinir).
func TestSystemdSliceSil_TumAdVaryantlari(t *testing.T) {
	o := bhKur(t, false)
	oturumSahte(t, false)
	systemctlCalistirSon := systemctlCalistir
	_ = systemctlCalistirSon
	// oturumSahte kendi systemctl'ini kurdu: bu testte kayıt için geri al.
	systemctlCalistir = func(args ...string) ([]byte, error) {
		o.mu.Lock()
		o.Cagri = append(o.Cagri, args)
		o.mu.Unlock()
		return nil, nil
	}
	_, _ = bayidilim.UyelikYaz("c_ornek_com", 5)
	for _, ad := range []string{"girginos-bayi5-c_ornek_com.slice", "girginos-c_ornek_com.slice", "girginos-bayi7-c_ornek_com.slice", "girginos-bayi5-c_diger.slice", "girginos-c_diger.slice"} {
		_ = os.WriteFile(filepath.Join(o.Dizin, ad), []byte("x"), 0o644)
		_ = os.MkdirAll(filepath.Join(o.Run, ad+".d"), 0o755)
	}
	if err := SystemdSliceSil("c_ornek_com"); err != nil {
		t.Fatal(err)
	}
	for _, ad := range []string{"girginos-bayi5-c_ornek_com.slice", "girginos-c_ornek_com.slice", "girginos-bayi7-c_ornek_com.slice"} {
		if _, err := os.Stat(filepath.Join(o.Dizin, ad)); err == nil {
			t.Errorf("%s silinmedi", ad)
		}
		if _, err := os.Stat(filepath.Join(o.Run, ad+".d")); err == nil {
			t.Errorf("%s runtime override'ı silinmedi", ad)
		}
		if o.say("stop "+ad) != 1 || o.say("reset-failed "+ad) != 1 {
			t.Errorf("%s durdurulmadı/reset-failed edilmedi: %v", ad, o.Cagri)
		}
	}
	for _, ad := range []string{"girginos-bayi5-c_diger.slice", "girginos-c_diger.slice"} {
		if _, err := os.Stat(filepath.Join(o.Dizin, ad)); err != nil {
			t.Errorf("başka hostingin dilimi silindi: %s", ad)
		}
		if o.say("stop "+ad) != 0 {
			t.Errorf("başka hostingin dilimi durduruldu: %s", ad)
		}
	}
	if bayidilim.Uyelik("c_ornek_com") != 5 {
		t.Error("plan kalkınca üyelik silindi (üyelik veritabanından türetilir, tam sökümde silinir)")
	}
	// Söküm kancası (hosting TAMAMEN kalkıyor) dilimi de, üyeliği de siler. Dosyalar yeniden yaratılır:
	// kancanın SystemdSliceSil'i GERÇEKTEN çağırdığı görülsün (yukarıdaki çağrı zaten sildi).
	kancaAdlari := []string{"girginos-bayi5-c_ornek_com.slice", "girginos-c_ornek_com.slice"}
	for _, ad := range kancaAdlari {
		_ = os.WriteFile(filepath.Join(o.Dizin, ad), []byte("x"), 0o644)
	}
	o.Cagri = nil
	if err := provisioner.TenantSliceSilKancasi("c_ornek_com"); err != nil {
		t.Fatal(err)
	}
	for _, ad := range kancaAdlari {
		if _, err := os.Stat(filepath.Join(o.Dizin, ad)); err == nil {
			t.Errorf("söküm kancası %s dilimini silmedi", ad)
		}
		if o.say("stop "+ad) != 1 {
			t.Errorf("söküm kancası %s dilimini durdurmadı: %v", ad, o.Cagri)
		}
	}
	if bayidilim.Uyelik("c_ornek_com") != 0 {
		t.Error("söküm kancası üyeliği silmedi")
	}
	// Panel dışı / geçersiz adlara dokunulmaz.
	o.Cagri = nil
	_ = SystemdSliceSil("root")
	_ = SystemdSliceSil("")
	if len(o.Cagri) != 0 {
		t.Errorf("geçersiz sk için komut çalıştı: %v", o.Cagri)
	}
}

// Havuza yeni alınmış ama FPM'i henüz yeniden başlamamış hosting ESKİ düz dilimde çalışır: plan değişikliği
// iki dilime de CANLI uygulanır (yeniden başlatma beklenmez), süreç öldürülmez.
func TestSystemdSliceYaz_UyeyseHemYuvaliHemEskiDilimeCanli(t *testing.T) {
	o := bhKur(t, true)
	cagrilar := oturumSahte(t, true)
	// oturumSahte systemctl'i değiştirdi; kayıt oturumSahte'nin listesinde.
	_, _ = bayidilim.UyelikYaz("c_ornek_com", 5)
	l := Limitler{CPUYuzde: 120, RAMMB: 600, MaxProcess: 40}
	if err := SystemdSliceYaz("c_ornek_com", l); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(o.Dizin, "girginos-bayi5-c_ornek_com.slice"))
	if err != nil || string(b) != sliceIcerik("c_ornek_com", l) {
		t.Errorf("yuvalı dilim dosyası: %v %q", err, b)
	}
	if _, err := os.Stat(filepath.Join(o.Dizin, "girginos-c_ornek_com.slice")); err == nil {
		t.Error("eski düz dilim dosyası yeniden yazıldı (yalnız canlı uygulanmalı, dosya artık)")
	}
	say := func(hedef string) int {
		n := 0
		for _, c := range *cagrilar {
			if len(c) > 3 && c[0] == "set-property" && c[1] == "--runtime" && c[2] == hedef && c[3] == "CPUQuota=120%" {
				n++
			}
		}
		return n
	}
	if say("girginos-bayi5-c_ornek_com.slice") != 1 || say("girginos-c_ornek_com.slice") != 1 {
		t.Errorf("canlı uygulama: %v", *cagrilar)
	}
	for _, c := range *cagrilar {
		if len(c) > 0 && (c[0] == "restart" || c[0] == "stop" || c[0] == "kill") {
			t.Errorf("plan yazımı hizmete dokundu: %v", c)
		}
	}
}

// Kancanın kurulu olduğunu kanıtlar: kaynaklimit içe aktarıldığında havuz paketi bu işlevi çağırır.
func TestHookKurulu(t *testing.T) {
	if !bayikaynak.HookKurulu() {
		t.Fatal("bayikaynak.UyelikDegisti kancası kurulu değil: üyelik değişince plan dilimleri yeni adla yazılmaz")
	}
}

// Plan kalkınca (SystemdSliceRuntimeSifirla) etkin olan HER aday dilimin (güncel yuvalı ad, eski düz
// ad, başka havuzdan kalan yuvalı ad) runtime override'ı sıfırlanır; pasif olanlara dokunulmaz.
func TestSystemdSliceRuntimeSifirla_TumAdaylarEtkinse(t *testing.T) {
	o := bhKur(t, true)
	oturumSahte(t, true)
	systemctlCalistir = func(args ...string) ([]byte, error) {
		o.mu.Lock()
		o.Cagri = append(o.Cagri, args)
		o.mu.Unlock()
		if len(args) > 1 && args[0] == "is-active" {
			if args[1] == "girginos-bayi7-c_ornek_com.slice" {
				return []byte("inactive\n"), fmt.Errorf("3")
			}
			return []byte("active\n"), nil
		}
		return nil, nil
	}
	_, _ = bayidilim.UyelikYaz("c_ornek_com", 5)
	_ = os.WriteFile(filepath.Join(o.Dizin, "girginos-bayi7-c_ornek_com.slice"), []byte("x"), 0o644)
	SystemdSliceRuntimeSifirla("c_ornek_com")
	for _, ad := range []string{"girginos-bayi5-c_ornek_com.slice", "girginos-c_ornek_com.slice"} {
		if o.say("set-property --runtime "+ad+" CPUQuota=") != 1 {
			t.Errorf("%s runtime override'ı sıfırlanmadı: %v", ad, o.Cagri)
		}
	}
	if o.say("set-property --runtime girginos-bayi7-c_ornek_com.slice") != 0 {
		t.Error("pasif dilime set-property uygulandı")
	}
	for _, c := range o.Cagri {
		if len(c) > 0 && (c[0] == "restart" || c[0] == "stop" || c[0] == "kill") {
			t.Errorf("sıfırlama hizmete dokundu: %v", c)
		}
	}
}

// Canlı uygulama (set-property --runtime) yalnız ETKİN dilime ve dilim başına TEK kez yapılır:
// havuzsuz hostingde eski düz ad ile güncel ad aynıdır (ikinci çağrı gereksiz yük ve çifte günlük).
func TestSystemdSliceYaz_CanliUygulamaTekSeferVeYalnizEtkin(t *testing.T) {
	l := Limitler{CPUYuzde: 130, RAMMB: 600, MaxProcess: 40}
	say := func(cagrilar [][]string, hedef string) int {
		n := 0
		for _, c := range cagrilar {
			if len(c) > 3 && c[0] == "set-property" && c[1] == "--runtime" && c[2] == hedef && c[3] == "CPUQuota=130%" {
				n++
			}
		}
		return n
	}

	// 1) Havuzsuz + etkin: düz dilime tam bir kez.
	bhKur(t, true)
	cagrilar := oturumSahte(t, true)
	if err := SystemdSliceYaz("c_ornek_com", l); err != nil {
		t.Fatal(err)
	}
	if n := say(*cagrilar, "girginos-c_ornek_com.slice"); n != 1 {
		t.Errorf("havuzsuz hostingde düz dilime canlı uygulama %d kez: %v", n, *cagrilar)
	}

	// 2) Havuzsuz + pasif: hiç canlı uygulama yok (FPM başlarken dosyadan alır).
	bhKur(t, false)
	cagrilar = oturumSahte(t, false)
	if err := SystemdSliceYaz("c_ornek_com", l); err != nil {
		t.Fatal(err)
	}
	if n := say(*cagrilar, "girginos-c_ornek_com.slice"); n != 0 {
		t.Errorf("pasif dilime canlı uygulama yapıldı (%d): %v", n, *cagrilar)
	}

	// 3) Üye: yuvalı dilim PASİF, eski düz dilim ETKİN (FPM henüz yeniden başlamadı) → yalnız düz dilime.
	o := bhKur(t, true)
	cagrilar = oturumSahte(t, true)
	sahte := systemctlCalistir
	systemctlCalistir = func(args ...string) ([]byte, error) {
		if len(args) > 1 && args[0] == "is-active" && args[1] == "girginos-bayi5-c_ornek_com.slice" {
			*cagrilar = append(*cagrilar, args)
			return []byte("inactive\n"), fmt.Errorf("3")
		}
		return sahte(args...)
	}
	_, _ = bayidilim.UyelikYaz("c_ornek_com", 5)
	if err := SystemdSliceYaz("c_ornek_com", l); err != nil {
		t.Fatal(err)
	}
	if n := say(*cagrilar, "girginos-bayi5-c_ornek_com.slice"); n != 0 {
		t.Errorf("pasif yuvalı dilime canlı uygulama yapıldı (%d): %v", n, *cagrilar)
	}
	if n := say(*cagrilar, "girginos-c_ornek_com.slice"); n != 1 {
		t.Errorf("etkin eski düz dilime canlı uygulama %d kez: %v", n, *cagrilar)
	}
	_ = o
}
