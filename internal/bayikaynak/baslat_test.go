package bayikaynak

// baslat_test.go — panel ömrü boyunca çalışan döngü: açılışta tam uzlaştırma, her turda kullanım
// örneği, her 6. turda (üretimde 30 dk) sürüklenmeyi onaran tam uzlaştırma; ctx iptalinde çıkış.

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"girginospanel/internal/bayidilim"
)

func baslatSureleri(t *testing.T, bekleme, aralik time.Duration) {
	t.Helper()
	eB, eA := acilisBekleme, calistirAraligi
	t.Cleanup(func() { acilisBekleme, calistirAraligi = eB, eA })
	acilisBekleme, calistirAraligi = bekleme, aralik
}

func kosKapat(ctx context.Context, f func(context.Context)) <-chan struct{} {
	bitti := make(chan struct{})
	go func() { defer close(bitti); f(ctx) }()
	return bitti
}

func bekle(t *testing.T, ne string, kosul func() bool) {
	t.Helper()
	son := time.Now().Add(10 * time.Second)
	for time.Now().Before(son) {
		if kosul() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("zaman aşımı: %s", ne)
}

func TestCalistir_DBYoksaHemenDoner(t *testing.T) {
	baslatSureleri(t, time.Hour, time.Hour)
	bitti := kosKapat(context.Background(), func(ctx context.Context) { Calistir(ctx, nil) })
	select {
	case <-bitti:
	case <-time.After(2 * time.Second):
		t.Fatal("db=nil iken Calistir dönmedi")
	}
}

// Açılış beklemesinde ctx iptal edilirse hiçbir iş yapılmadan çıkılır.
func TestCalistir_AcilistaIptalIsYapmaz(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	seedBayiler(t, db, o)
	baslatSureleri(t, time.Hour, time.Hour)
	ctx, iptal := context.WithCancel(context.Background())
	bitti := kosKapat(ctx, func(ctx context.Context) { Calistir(ctx, db) })
	iptal()
	select {
	case <-bitti:
	case <-time.After(2 * time.Second):
		t.Fatal("iptalde Calistir dönmedi")
	}
	if bayidilim.Uyelik("c_a") != 0 || o.Var(filepath.Join(o.Birim, "girginos-bayi1.slice")) {
		t.Error("iptal edilmiş açılışta uzlaştırma çalıştı")
	}
}

func TestCalistir_AcilistaUzlastirirKullanimOrnekler_AltinciTurdaOnarir(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	seedBayiler(t, db, o)
	baslatSureleri(t, 5*time.Millisecond, 20*time.Millisecond)
	ctx, iptal := context.WithCancel(context.Background())
	defer iptal()
	bitti := kosKapat(ctx, func(ctx context.Context) { Calistir(ctx, db) })

	// 1) Açılışta tam uzlaştırma: üyelik + havuz dilimi.
	bekle(t, "açılış uzlaştırması", func() bool {
		return bayidilim.Uyelik("c_a") == 1 && o.Var(filepath.Join(o.Birim, "girginos-bayi1.slice"))
	})

	// 2) Her turda kullanım örneği (bayi 3: disk havuzu → disk ölçülür).
	bekle(t, "kullanım örneği", func() bool {
		return say(t, db, `SELECT COUNT(*) FROM bayi_kaynak_kullanim WHERE bayi_id=3`) > 0
	})

	// 3) Sürüklenme (elle silinen dosyalar) periyodik tam uzlaştırmayla onarılır.
	if err := os.Remove(filepath.Join(o.Uyelik, "c_a")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(o.Birim, "girginos-bayi1.slice")); err != nil {
		t.Fatal(err)
	}
	bekle(t, "periyodik onarım", func() bool {
		return bayidilim.Uyelik("c_a") == 1 && o.Var(filepath.Join(o.Birim, "girginos-bayi1.slice"))
	})

	// 4) Artık dilim (eski düz ad) tur içinde temizlenir.
	o.BirimYaz(t, "girginos-c_a.slice", "# GirginOSPanel per-domain resource slice — c_a\n[Slice]\n")
	bekle(t, "artık dilim temizliği", func() bool { return !o.Var(filepath.Join(o.Birim, "girginos-c_a.slice")) })

	// 5) İptalde çıkar.
	iptal()
	select {
	case <-bitti:
	case <-time.After(5 * time.Second):
		t.Fatal("iptalde Calistir dönmedi")
	}
}

// Açılışta (ilk tur gelmeden) tam uzlaştırma + disk proje ataması + ilk kullanım örneği yapılır:
// panel yeniden başlayınca/sunucu açılınca havuzlar ayağa kalkar, 30 dk beklenmez.
func TestCalistir_AcilistaHemenTamUzlastirmaDiskVeIlkOrnek(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	seedBayiler(t, db, o)
	evKur(t, o, "c_d")
	projeSahte(o, false, "", "")
	baslatSureleri(t, 5*time.Millisecond, time.Hour) // ilk tur 1 saat sonra: yalnız açılış işleri görünür
	ctx, iptal := context.WithCancel(context.Background())
	defer iptal()
	bitti := kosKapat(ctx, func(ctx context.Context) { Calistir(ctx, db) })

	bekle(t, "açılış uzlaştırması + disk ataması + ilk örnek", func() bool {
		return bayidilim.Uyelik("c_a") == 1 && o.Var(filepath.Join(o.Birim, "girginos-bayi1.slice")) &&
			projeIsaretiOku("c_d") == 3000003 &&
			say(t, db, `SELECT COUNT(*) FROM bayi_kaynak_kullanim WHERE bayi_id=3`) > 0
	})
	iptal()
	select {
	case <-bitti:
	case <-time.After(5 * time.Second):
		t.Fatal("iptalde Calistir dönmedi")
	}
}

// Yalnız havuzdan ÇIKAN üyelik de kancayı tetikler (plan dilimi eski düz ada yeniden yazılmalı);
// değişiklik yoksa kanca çağrılmaz.
func TestUzlastirTam_YalnizKalkanUyelikKancayiTetikler(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	seedBayiler(t, db, o)
	var cagri []Rapor
	HookKur(Hooklar{UyelikDegisti: func(_ context.Context, _ *sql.DB, r Rapor) { cagri = append(cagri, r) }})
	ctx := context.Background()

	UzlastirTam(ctx, db, false)
	if len(cagri) != 1 || len(cagri[0].UyelikEklenen) != 2 {
		t.Fatalf("ilk tur: kanca %d kez, rapor %+v", len(cagri), cagri)
	}
	// Değişiklik yok: kanca çağrılmaz.
	UzlastirTam(ctx, db, false)
	if len(cagri) != 1 {
		t.Fatalf("değişiklik yokken kanca çağrıldı: %d", len(cagri))
	}
	// c_a bayiden çıkar (yönetici hostingine döner): eklenen YOK, kalkan var.
	if _, err := db.Exec(`UPDATE domains SET reseller_id=0 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	UzlastirTam(ctx, db, false)
	if len(cagri) != 2 {
		t.Fatalf("kalkan üyelikte kanca çağrılmadı: %d", len(cagri))
	}
	if r := cagri[1]; len(r.UyelikEklenen) != 0 || !reflect.DeepEqual(r.UyelikKalkan, []string{"c_a"}) {
		t.Errorf("rapor: %+v", r)
	}
}

// Tur sonu bakımı: yalnız her 6. turda tam uzlaştırma (30 dk); diğer turlarda hafif artık temizliği
// (havuz dilimi dosyası ONARILMAZ ama artık dilim silinir).
func TestTurSonu_Her6TurdaTamUzlastirma_DigerlerindeYalnizArtikTemizligi(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	seedBayiler(t, db, o)
	evKur(t, o, "c_d")
	projeSahte(o, false, "", "")
	ctx := context.Background()
	UzlastirTam(ctx, db, false)
	artik := filepath.Join(o.Birim, "girginos-c_a.slice")
	havuz := filepath.Join(o.Birim, "girginos-bayi1.slice") // yalnız tam uzlaştırma yeniden yazar
	for n := 1; n <= 13; n++ {
		if err := os.Remove(havuz); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(o.Proje, "c_d")); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		o.BirimYaz(t, "girginos-c_a.slice", "# GirginOSPanel per-domain resource slice — c_a\n[Slice]\n")
		turSonu(ctx, db, n)
		if tam := o.Var(havuz); tam != (n%6 == 0) {
			t.Errorf("tur %d: tam uzlaştırma=%v, beklenen=%v", n, tam, n%6 == 0)
		}
		if disk := projeIsaretiOku("c_d") == 3000003; disk != (n%6 == 0) {
			t.Errorf("tur %d: disk proje ataması=%v, beklenen=%v", n, disk, n%6 == 0)
		}
		if o.Var(artik) {
			t.Errorf("tur %d: artık dilim silinmedi", n)
		}
	}
}
