package bayikaynak

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"girginospanel/internal/bayidilim"
)

// seed: bayi 1 (CPU+RAM havuzu), bayi 2 (hiç havuz yok = eski davranış), bayi 3 (yalnız disk havuzu).
func seedBayiler(t *testing.T, db *sql.DB, o *ortam) {
	t.Helper()
	bayiEkle(t, db, 1, "bayia", 1.5, 2048, 0, 0)
	bayiEkle(t, db, 2, "bayib", 0, 0, 0, 0)
	bayiEkle(t, db, 3, "bayic", 0, 0, 1000, 0)
	hostingEkle(t, db, 1, "a.test", "c_a", 1, 100)
	hostingEkle(t, db, 2, "b.test", "c_b", 1, 100)
	hostingEkle(t, db, 3, "c.test", "c_c", 2, 100)
	hostingEkle(t, db, 4, "d.test", "c_d", 3, 100)
	hostingEkle(t, db, 5, "root.test", "c_root", 0, 100)
	// Yalnız c_a ve c_c için FPM ana birimi kurulu (c_b henüz kurulmamış).
	o.BirimYaz(t, "php-fpm-c_a.service", "[Service]\nSlice=girginos-c_a.slice\n")
	o.BirimYaz(t, "php-fpm-c_c.service", "[Service]\nSlice=girginos-c_c.slice\n")
}

func ids(u []Uye) []string {
	var s []string
	for _, x := range u {
		s = append(s, x.SK)
	}
	sort.Strings(s)
	return s
}

func TestUzlastir_HavuzDilimiUyelikVeDropin(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	seedBayiler(t, db, o)
	ctx := context.Background()

	r, err := Uzlastir(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	// Havuz dilimi yalnız CPU/RAM havuzu olan bayiye: bayi 2 (hiç değer yok) ve bayi 3 (yalnız disk) dilim ALMAZ.
	h1 := Havuz{BayiID: 1, CPUCekirdek: 1.5, RAMMB: 2048}
	if o.Oku(t, filepath.Join(o.Birim, "girginos-bayi1.slice")) != DilimIcerik(h1) {
		t.Error("bayi 1 havuz dilimi içeriği beklenenle aynı değil")
	}
	for _, ad := range []string{"girginos-bayi2.slice", "girginos-bayi3.slice"} {
		if o.Var(filepath.Join(o.Birim, ad)) {
			t.Errorf("%s yazıldı: değer verilmemiş bayi ETKİLENMEMELİ", ad)
		}
	}
	if len(o.Etiket) == 0 || o.Etiket[0] != filepath.Join(o.Birim, "girginos-bayi1.slice") {
		t.Errorf("SELinux etiketi (restorecon) çağrılmadı: %v", o.Etiket)
	}
	// Üyelik yalnız bayi 1'in hostinglerinde.
	if bayidilim.Uyelik("c_a") != 1 || bayidilim.Uyelik("c_b") != 1 {
		t.Error("bayi 1 hostingleri üye değil")
	}
	for _, sk := range []string{"c_c", "c_d", "c_root"} {
		if bayidilim.Uyelik(sk) != 0 {
			t.Errorf("%s havuza alındı", sk)
		}
	}
	// Drop-in yalnız ana birimi KURULU üyeye: c_b (birim yok) dosya yaratılmaz, c_c üye değil.
	dropin := filepath.Join(o.Birim, "php-fpm-c_a.service.d", bayidilim.DropinAd)
	if o.Oku(t, dropin) != bayidilim.DropinIcerik(1, "c_a") {
		t.Errorf("c_a drop-in: %q", o.Oku(t, dropin))
	}
	if o.Var(filepath.Join(o.Birim, "php-fpm-c_b.service.d")) || o.Var(filepath.Join(o.Birim, "php-fpm-c_c.service.d")) {
		t.Error("birimi olmayan / üye olmayan hosting için drop-in yazıldı")
	}
	// Ana birim DOKUNULMADI (drift-restart tetiklememeli).
	if o.Oku(t, filepath.Join(o.Birim, "php-fpm-c_a.service")) != "[Service]\nSlice=girginos-c_a.slice\n" {
		t.Error("ana birim değiştirildi")
	}
	if r.Havuzlar != 1 || r.Uyeler != 2 || !reflect.DeepEqual(r.DilimYazilan, []int64{1}) || ids(r.UyelikEklenen)[0] != "c_a" || r.Dropin != 1 || len(r.Hatalar) != 0 {
		t.Errorf("rapor: %+v", r)
	}
	// Tek daemon-reload; hiçbir servis durdurulmadı/yeniden başlatılmadı.
	if n := o.Komut.Say("systemctl daemon-reload"); n != 1 {
		t.Errorf("daemon-reload sayısı %d (1 beklenir)", n)
	}
	if y := o.Komut.YikiciVar(); y != "" {
		t.Errorf("uzlaştırma çalışan hizmete dokundu: %s", y)
	}

	// İdempotent: ikinci tur hiçbir şey değiştirmez, systemd'ye hiç dokunmaz.
	o.Komut.Sifirla()
	r2, err := Uzlastir(ctx, db)
	if err != nil || r2.Degisti() {
		t.Fatalf("ikinci tur değişiklik yaptı: %+v %v", r2, err)
	}
	if n := o.Komut.Say("systemctl daemon-reload"); n != 0 {
		t.Errorf("değişiklik yokken daemon-reload çağrıldı (%d)", n)
	}
}

// 0 = sınırsız: değer sıfırlanınca dilim, üyelik ve drop-in kalkar (eski ad geri gelir).
func TestUzlastir_HavuzSifirlaninca_EskiDuzene(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	seedBayiler(t, db, o)
	ctx := context.Background()
	if _, err := Uzlastir(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE users SET kaynak_cpu_cekirdek=0, kaynak_ram_mb=0 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	o.Komut.Sifirla()
	r, err := Uzlastir(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if o.Var(filepath.Join(o.Birim, "girginos-bayi1.slice")) {
		t.Error("havuzu sıfırlanan bayinin (pasif) dilimi kaldı")
	}
	if bayidilim.Uyelik("c_a") != 0 || bayidilim.Uyelik("c_b") != 0 {
		t.Error("üyelik kaldı")
	}
	if o.Var(filepath.Join(o.Birim, "php-fpm-c_a.service.d")) {
		t.Error("drop-in kaldı (hosting havuzdan çıkmış olmalı)")
	}
	if !reflect.DeepEqual(r.UyelikKalkan, []string{"c_a", "c_b"}) || len(r.DilimSilinen) != 1 {
		t.Errorf("rapor: %+v", r)
	}
	if bayidilim.Adi("c_a") != "girginos-c_a.slice" {
		t.Errorf("sıfırlamadan sonra dilim adı %q", bayidilim.Adi("c_a"))
	}
	if y := o.Komut.YikiciVar(); y != "" {
		t.Errorf("hizmete dokunuldu: %s", y)
	}
}

// İçinde süreç olan (etkin) havuz dilimi sökülmez: limitsiz kalıba çevrilir (çalışan siteler altından çekilmez).
func TestUzlastir_EtkinDilimSokulmez_LimitsizKalip(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	seedBayiler(t, db, o)
	ctx := context.Background()
	if _, err := Uzlastir(ctx, db); err != nil {
		t.Fatal(err)
	}
	aktif := true
	o.Komut.Yanit = func(satir string) (string, error, bool) {
		if strings.HasPrefix(satir, "systemctl is-active") {
			if aktif {
				return "active\n", nil, true
			}
			return "inactive\n", hataCikis3, true
		}
		return "", nil, false
	}
	// Değer değişince etkin dilime CANLI uygulanır (set-property --runtime), yeniden başlatma yok.
	if _, err := db.Exec(`UPDATE users SET kaynak_cpu_cekirdek=2 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	o.Komut.Sifirla()
	if _, err := Uzlastir(ctx, db); err != nil {
		t.Fatal(err)
	}
	if o.Komut.Say("systemctl set-property --runtime girginos-bayi1.slice CPUQuota=200%") != 1 {
		t.Errorf("canlı uygulama yok: %v", o.Komut.Satirlar())
	}
	if y := o.Komut.YikiciVar(); y != "" {
		t.Errorf("hizmete dokunuldu: %s", y)
	}
	// Havuz kapanınca: dilim etkin → silinmez, limitsiz kalıba döner ve üyelik/drop-in yine kalkar.
	if _, err := db.Exec(`UPDATE users SET kaynak_cpu_cekirdek=0, kaynak_ram_mb=0 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := Uzlastir(ctx, db); err != nil {
		t.Fatal(err)
	}
	icerik := o.Oku(t, filepath.Join(o.Birim, "girginos-bayi1.slice"))
	if strings.Contains(icerik, "CPUQuota") || strings.Contains(icerik, "MemoryMax") {
		t.Errorf("etkin dilim limitsiz kalıba dönmedi:\n%s", icerik)
	}
	if bayidilim.Uyelik("c_a") != 0 {
		t.Error("üyelik kalktı bekleniyordu")
	}
	// Süreçler gidince sonraki tur dilimi gerçekten siler.
	aktif = false
	if _, err := Uzlastir(ctx, db); err != nil {
		t.Fatal(err)
	}
	if o.Var(filepath.Join(o.Birim, "girginos-bayi1.slice")) {
		t.Error("pasifleşen limitsiz kalıp silinmedi")
	}
}

// Bir bayinin hatası diğerlerini ve paneli durdurmaz; hatalı bayinin hostingleri havuza ALINMAZ.
func TestUzlastir_BayiHatasiIzoleEdilir(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	seedBayiler(t, db, o)
	bayiEkle(t, db, 9, "bayiz", 1, 1024, 0, 0)
	hostingEkle(t, db, 9, "z.test", "c_z", 9, 10)
	// bayi 1'in dilim yolunda DİZİN var → yazım başarısız olur.
	if err := os.MkdirAll(filepath.Join(o.Birim, "girginos-bayi1.slice"), 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := Uzlastir(context.Background(), db)
	if err != nil {
		t.Fatalf("tek bayinin hatası turu düşürdü: %v", err)
	}
	if len(r.Hatalar) == 0 || !strings.Contains(strings.Join(r.Hatalar, "\n"), "bayi 1") {
		t.Errorf("hata raporlanmadı: %+v", r.Hatalar)
	}
	if !o.Var(filepath.Join(o.Birim, "girginos-bayi9.slice")) || bayidilim.Uyelik("c_z") != 9 {
		t.Error("sağlam bayi 9 işlenmedi")
	}
	if bayidilim.Uyelik("c_a") != 0 || bayidilim.Uyelik("c_b") != 0 {
		t.Error("dilimi yazılamayan bayinin hostingleri havuza alındı (var olmayan dilime taşınırlardı)")
	}
}

// Veritabanı okunamazsa (göç uygulanmamış vb.) HİÇBİR şey değişmez: mevcut dilim/üyelik/drop-in korunur.
func TestUzlastir_DBHatasindaDokunmaz(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	seedBayiler(t, db, o)
	if _, err := Uzlastir(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE users DROP COLUMN kaynak_ram_mb`); err != nil {
		t.Fatal(err)
	}
	o.Komut.Sifirla()
	if _, err := Uzlastir(context.Background(), db); err == nil {
		t.Fatal("kolon yokken hata beklenirdi")
	}
	if !o.Var(filepath.Join(o.Birim, "girginos-bayi1.slice")) || bayidilim.Uyelik("c_a") != 1 ||
		!o.Var(filepath.Join(o.Birim, "php-fpm-c_a.service.d", bayidilim.DropinAd)) {
		t.Error("DB hatasında mevcut kurulum bozuldu")
	}
	if len(o.Komut.Satirlar()) != 0 {
		t.Errorf("DB hatasında komut çalıştı: %v", o.Komut.Satirlar())
	}
}

// Devir: hosting bayi 1 → bayi 9 havuzuna geçer; üyelik, drop-in ve (hook ile) dilim adı yeni bayiye döner,
// eski bayinin yuvalı plan dilimi YALNIZ pasifse silinir.
func TestUzlastir_DevirVeArtikDilim(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	seedBayiler(t, db, o)
	bayiEkle(t, db, 9, "bayiz", 0, 4096, 0, 0)
	ctx := context.Background()
	if _, err := Uzlastir(ctx, db); err != nil {
		t.Fatal(err)
	}
	// Hook'un yazacağı eski yuvalı plan dilimi (panel imzalı).
	eski := filepath.Join(o.Birim, "girginos-bayi1-c_a.slice")
	imzali := "# GirginOSPanel per-domain resource slice — c_a\n[Slice]\nMemoryMax=512M\n"
	if err := os.WriteFile(eski, []byte(imzali), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE domains SET reseller_id=9 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	r, err := Uzlastir(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if bayidilim.Uyelik("c_a") != 9 || bayidilim.Adi("c_a") != "girginos-bayi9-c_a.slice" {
		t.Errorf("devir sonrası üyelik %d ad %q", bayidilim.Uyelik("c_a"), bayidilim.Adi("c_a"))
	}
	if len(r.UyelikEklenen) != 1 || r.UyelikEklenen[0].SK != "c_a" || r.UyelikEklenen[0].BayiID != 9 {
		t.Errorf("üyelik eklenen: %+v", r.UyelikEklenen)
	}
	if b := o.Oku(t, filepath.Join(o.Birim, "php-fpm-c_a.service.d", bayidilim.DropinAd)); b != bayidilim.DropinIcerik(9, "c_a") {
		t.Errorf("drop-in yeni bayiye geçmedi: %q", b)
	}
	// Eski yuvalı dilim etkinken KORUNUR (süreçler hâlâ içinde), pasifleşince silinir.
	o.Komut.Yanit = func(satir string) (string, error, bool) {
		if satir == "systemctl is-active girginos-bayi1-c_a.slice" {
			return "active\n", nil, true
		}
		return "", nil, false
	}
	if sil := ArtikTemizle(ctx); len(sil) != 0 || !o.Var(eski) {
		t.Errorf("etkin eski dilim silindi: %v", sil)
	}
	o.Komut.Yanit = nil
	if sil := ArtikTemizle(ctx); !reflect.DeepEqual(sil, []string{"girginos-bayi1-c_a.slice"}) || o.Var(eski) {
		t.Errorf("pasif artık dilim silinmedi: %v", sil)
	}
	if y := o.Komut.YikiciVar(); y != "" {
		t.Errorf("hizmete dokunuldu: %s", y)
	}
}

// Hosting silinince üyelik ve drop-in kalkar; bayi silinince (DB'de yok) artık havuz dilimi süpürülür,
// yabancı (imzasız) dosyaya DOKUNULMAZ.
func TestUzlastir_SilinenHostingVeBayiTemizligi(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	seedBayiler(t, db, o)
	ctx := context.Background()
	if _, err := Uzlastir(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM domains WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	// Silinmiş bayiden kalan imzalı dilim + yabancı dosya + başka dosya türü.
	artik := filepath.Join(o.Birim, "girginos-bayi77.slice")
	yabanci := filepath.Join(o.Birim, "girginos-bayi78.slice")
	_ = os.WriteFile(artik, []byte(DilimIcerik(Havuz{BayiID: 77, RAMMB: 100})), 0o644)
	_ = os.WriteFile(yabanci, []byte("[Slice]\nMemoryMax=1G\n"), 0o644)
	r, err := Uzlastir(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if bayidilim.Uyelik("c_a") != 0 || o.Var(filepath.Join(o.Birim, "php-fpm-c_a.service.d")) {
		t.Error("silinen hostingin üyeliği/drop-in'i kaldı")
	}
	if !reflect.DeepEqual(r.UyelikKalkan, []string{"c_a"}) {
		t.Errorf("üyelik kalkan: %v", r.UyelikKalkan)
	}
	if o.Var(artik) {
		t.Error("silinmiş bayinin imzalı havuz dilimi kaldı")
	}
	if !o.Var(yabanci) {
		t.Error("yabancı (imzasız) dosya silindi")
	}
	// Bayi 1'in kalan hostingi (c_b) hâlâ üye.
	if bayidilim.Uyelik("c_b") != 1 {
		t.Error("kalan hosting havuzdan çıktı")
	}
}

func TestUzlastir_GecersizSKAtlanir(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	bayiEkle(t, db, 1, "bayia", 1, 512, 0, 0)
	hostingEkle(t, db, 1, "x.test", "c_bad-name", 1, 1)
	hostingEkle(t, db, 2, "y.test", "c_ok", 1, 1)
	hostingEkle(t, db, 3, "z.test", "../etc", 1, 1)
	hostingEkle(t, db, 4, "w.test", "root", 1, 1)
	if _, err := Uzlastir(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if l := bayidilim.UyelikListe(); !reflect.DeepEqual(l, map[string]int64{"c_ok": 1}) {
		t.Errorf("üyelikler: %v (yalnız geçerli sk)", l)
	}
	ents, _ := os.ReadDir(o.Birim)
	for _, e := range ents {
		if strings.Contains(e.Name(), "bad") || strings.Contains(e.Name(), "..") || strings.Contains(e.Name(), "root") {
			t.Errorf("geçersiz sk için dosya: %s", e.Name())
		}
	}
}

func TestArtikTemizle_Matris(t *testing.T) {
	o := ortamKur(t)
	ctx := context.Background()
	imza := func(sk string) string { return "# GirginOSPanel per-domain resource slice — " + sk + "\n[Slice]\n" }
	yaz := func(ad, ic string) { o.BirimYaz(t, ad, ic) }
	_, _ = bayidilim.UyelikYaz("c_uye", 4) // c_uye 4 numaralı havuzun üyesi
	// 1) üye hostingin ESKİ düz dilimi → artık (silinmeli)
	yaz("girginos-c_uye.slice", imza("c_uye"))
	// 2) üye hostingin GEÇERLİ yuvalı dilimi → korunmalı
	yaz("girginos-bayi4-c_uye.slice", imza("c_uye"))
	// 3) üye hostingin başka havuzdaki yuvalı dilimi → artık
	yaz("girginos-bayi5-c_uye.slice", imza("c_uye"))
	// 4) üye olmayan hostingin düz dilimi → meşru, korunmalı
	yaz("girginos-c_serbest.slice", imza("c_serbest"))
	// 5) üye olmayan hostingin yuvalı dilimi → artık
	yaz("girginos-bayi4-c_serbest.slice", imza("c_serbest"))
	// 6) imzasız yabancı dosya → asla silinmez
	yaz("girginos-bayi9-c_yabanci.slice", "[Slice]\nMemoryMax=1G\n")
	// 7) imzası başka sk'ye ait → silinmez
	yaz("girginos-bayi4-c_baska.slice", imza("c_uye"))
	sil := ArtikTemizle(ctx)
	sort.Strings(sil)
	want := []string{"girginos-bayi4-c_serbest.slice", "girginos-bayi5-c_uye.slice", "girginos-c_uye.slice"}
	if !reflect.DeepEqual(sil, want) {
		t.Errorf("silinenler %v, beklenen %v", sil, want)
	}
	for _, korunan := range []string{"girginos-bayi4-c_uye.slice", "girginos-c_serbest.slice", "girginos-bayi9-c_yabanci.slice", "girginos-bayi4-c_baska.slice"} {
		if !o.Var(filepath.Join(o.Birim, korunan)) {
			t.Errorf("silinmemesi gereken dosya silindi: %s", korunan)
		}
	}
	if o.Komut.Say("systemctl daemon-reload") != 1 || o.Komut.Say("systemctl reset-failed") != 3 {
		t.Errorf("systemd bildirimi: %v", o.Komut.Satirlar())
	}
}

func TestHostingUye_TekHostingYolu(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	seedBayiler(t, db, o)
	ctx := context.Background()
	// Yeni hosting havuzlu bayide: dilim + üyelik hazır olur (FPM havuzun içinde doğsun), drop-in'i EnableTenantFPM yazar.
	if d, err := HostingUye(ctx, db, 1); err != nil || !d {
		t.Fatalf("HostingUye: %v %v", d, err)
	}
	if !o.Var(filepath.Join(o.Birim, "girginos-bayi1.slice")) || bayidilim.Uyelik("c_a") != 1 {
		t.Error("dilim/üyelik kurulmadı")
	}
	if o.Var(filepath.Join(o.Birim, "php-fpm-c_a.service.d")) {
		t.Error("HostingUye drop-in yazmamalı (EnableTenantFPM yazar)")
	}
	if d, _ := HostingUye(ctx, db, 1); d {
		t.Error("ikinci çağrı değişiklik bildirdi")
	}
	// Havuzu olmayan bayide ve kök hostingde üyelik YOK (varsa silinir).
	_, _ = bayidilim.UyelikYaz("c_c", 5)
	for _, id := range []int64{3, 4, 5} {
		if _, err := HostingUye(ctx, db, id); err != nil {
			t.Fatal(err)
		}
	}
	if bayidilim.Uyelik("c_c") != 0 || bayidilim.Uyelik("c_d") != 0 || bayidilim.Uyelik("c_root") != 0 {
		t.Error("havuzsuz hosting üye yapıldı / eski üyelik silinmedi")
	}
	if o.Var(filepath.Join(o.Birim, "girginos-bayi3.slice")) {
		t.Error("yalnız disk havuzu olan bayi için CPU/RAM dilimi yazıldı")
	}
	if y := o.Komut.YikiciVar(); y != "" {
		t.Errorf("hizmete dokunuldu: %s", y)
	}
	if _, err := HostingUye(ctx, db, 999); err == nil {
		t.Error("olmayan hosting için hata beklenirdi")
	}
}

// UzlastirTam: Uzlastir → hook (üyelik değişenler) → artık süpürme sırası; hook panik etse bile tur ÇÖKMEZ.
func TestUzlastirTam_HookSirasiVePanikGuvenligi(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	seedBayiler(t, db, o)
	var gelen Rapor
	HookKur(Hooklar{UyelikDegisti: func(_ context.Context, _ *sql.DB, r Rapor) {
		gelen = r
		// Hook yeni adla plan dilimini yazar (kaynaklimit'in yaptığı iş).
		for _, u := range r.UyelikEklenen {
			ad := bayidilim.Adi(u.SK)
			o.BirimYaz(t, ad, "# GirginOSPanel per-domain resource slice — "+u.SK+"\n[Slice]\n")
		}
	}})
	// Eski düz plan dilimi (artık olmalı).
	o.BirimYaz(t, "girginos-c_a.slice", "# GirginOSPanel per-domain resource slice — c_a\n[Slice]\n")
	UzlastirTam(context.Background(), db, false)
	if got := ids(gelen.UyelikEklenen); !reflect.DeepEqual(got, []string{"c_a", "c_b"}) {
		t.Errorf("hook'a gelen üyelik: %v", got)
	}
	if !o.Var(filepath.Join(o.Birim, "girginos-bayi1-c_a.slice")) {
		t.Error("hook'un yazdığı geçerli üye dilimi ArtikTemizle tarafından silindi")
	}
	if o.Var(filepath.Join(o.Birim, "girginos-c_a.slice")) {
		t.Error("eski düz dilim artık sayılıp silinmedi")
	}
	// Panikleyen hook turu düşürmez.
	HookKur(Hooklar{UyelikDegisti: func(context.Context, *sql.DB, Rapor) { panic("hook patladı") }})
	if _, err := db.Exec(`UPDATE domains SET reseller_id=2 WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	UzlastirTam(context.Background(), db, false) // panik yakalanmalı
}

func TestBayiTemizle_DilimKullanimVeProjeSiniri(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	seedBayiler(t, db, o)
	ctx := context.Background()
	if _, err := Uzlastir(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{1, 2} {
		if err := noktaYaz(ctx, db, id, simdiSabit(), Nokta{Kaynak: "havuz"}); err != nil {
			t.Fatal(err)
		}
	}
	// Proje zorlaması açık: bayi silinince sert sınır kaldırılır (limit ... bhard=0m).
	diskSahte(o, "xfs", "rw,usrquota,prjquota", pquotaAcik, "ro rootflags=uquota,pquota")
	BayiTemizle(ctx, db, 1)
	if o.Var(filepath.Join(o.Birim, "girginos-bayi1.slice")) {
		t.Error("bayi havuz dilimi kaldı")
	}
	if say(t, db, `SELECT COUNT(*) FROM bayi_kaynak_kullanim WHERE bayi_id=1`) != 0 || say(t, db, `SELECT COUNT(*) FROM bayi_kaynak_kullanim WHERE bayi_id=2`) != 1 {
		t.Error("kullanım satırları yalnız silinen bayi için temizlenmeli")
	}
	if o.Komut.Say("xfs_quota -x -c limit -p bsoft=0 bhard=0m 3000001 /") != 1 {
		t.Errorf("proje sınırı kaldırılmadı: %v", o.Komut.Satirlar())
	}
	// Geçersiz kimlik hiçbir şey yapmaz.
	o.Komut.Sifirla()
	BayiTemizle(ctx, db, 0)
	BayiTemizle(ctx, db, -3)
	if len(o.Komut.Satirlar()) != 0 {
		t.Errorf("geçersiz bayi kimliği komut çalıştırdı: %v", o.Komut.Satirlar())
	}
}
