package bayikaynak

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"girginospanel/internal/bayidilim"
)

// projeSahte: findmnt + proje durumu + rapor çıktıları; atama/sınır komutları kaydedilir.
func projeSahte(o *ortam, zorlama bool, rapor string, hataVeren string) {
	durum := pquotaKapali
	if zorlama {
		durum = pquotaAcik
	}
	o.Komut.Yanit = func(satir string) (string, error, bool) {
		switch {
		case strings.HasPrefix(satir, "findmnt"):
			return "/ xfs rw,usrquota\n", nil, true
		case strings.HasPrefix(satir, "xfs_quota -x -c state -p"):
			return durum, nil, true
		case strings.HasPrefix(satir, "xfs_quota -x -c report -p -N -b"):
			return rapor, nil, true
		case strings.HasPrefix(satir, "grubby --info=DEFAULT"):
			return "args=\"ro rootflags=uquota\"\n", nil, true
		case hataVeren != "" && strings.Contains(satir, hataVeren):
			return "boom", errors.New("exit status 1"), true
		}
		return "", nil, false
	}
}

func evKur(t *testing.T, o *ortam, sks ...string) {
	t.Helper()
	for _, sk := range sks {
		if err := os.MkdirAll(filepath.Join(o.Ev, sk), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func projeSatirlari(o *ortam) []string {
	var s []string
	for _, l := range o.Komut.Satirlar() {
		if strings.Contains(l, " project ") || strings.Contains(l, "limit -p") {
			s = append(s, l)
		}
	}
	return s
}

func projeSeed(t *testing.T, db *sql.DB, o *ortam) {
	t.Helper()
	bayiEkle(t, db, 3, "bayic", 0, 0, 1000, 0)
	bayiEkle(t, db, 4, "bayid", 0, 0, 0, 0)
	hostingEkle(t, db, 1, "d1.test", "c_d1", 3, 100)
	hostingEkle(t, db, 2, "d2.test", "c_d2", 3, 100)
	hostingEkle(t, db, 3, "e1.test", "c_e1", 4, 100)
	evKur(t, o, "c_d1", "c_d2", "c_e1")
}

// pquota KAPALIYKEN: proje kimlikleri atanır (yeniden başlatma sonrası sayılsın) ama SINIR yazılmaz
// (kapalıyken `limit -p` "No such process" verir ve sınır yine de uygulanmazdı). Mod olcum_uyari.
func TestProjeUygula_PquotaKapaliyken_YalnizAtama(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	projeSeed(t, db, o)
	projeSahte(o, false, "", "")
	ctx := context.Background()

	r, err := ProjeUygula(ctx, db, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"xfs_quota -x -c project -s -p " + filepath.Join(o.Ev, "c_d1") + " 3000003 /",
		"xfs_quota -x -c project -s -p " + filepath.Join(o.Ev, "c_d2") + " 3000003 /",
	}
	if got := projeSatirlari(o); !reflect.DeepEqual(got, want) {
		t.Errorf("proje komutları:\n got %q\nwant %q", got, want)
	}
	if r.Mod != ModOlcumUyari || r.Atanan != 2 || r.SinirYazilan != 0 || len(r.Hatalar) != 0 {
		t.Errorf("rapor: %+v", r)
	}
	// Disk havuzu OLMAYAN bayinin hostingine (c_e1) dokunulmaz.
	if projeIsaretiOku("c_e1") != 0 || projeIsaretiOku("c_d1") != 3000003 {
		t.Error("işaret dosyaları yanlış")
	}
	// İdempotent: ikinci tur komut çalıştırmaz.
	o.Komut.Sifirla()
	r, _ = ProjeUygula(ctx, db, 0)
	if len(projeSatirlari(o)) != 0 || r.Atanan != 0 {
		t.Errorf("ikinci tur tekrar atadı: %v", projeSatirlari(o))
	}
}

func TestProjeUygula_ZorlamaAcikken_SertSinirYazilirVeKaldirilir(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	projeSeed(t, db, o)
	ctx := context.Background()
	// Raporda: bayi 3 henüz sınırsız; bayi 8 (silinmiş) eski sınırı duruyor; 1234 panel dışı (docker vb.).
	rapor := "#0 500 0 0 00 [-]\n#1234 77 0 9999 00 [-]\n#3000003 5000 0 0 00 [-]\n#3000008 10 0 5000 00 [-]\n"
	projeSahte(o, true, rapor, "")
	r, err := ProjeUygula(ctx, db, 0)
	if err != nil {
		t.Fatal(err)
	}
	satir := strings.Join(projeSatirlari(o), "\n")
	if !strings.Contains(satir, "limit -p bsoft=0 bhard=1000m 3000003 /") {
		t.Errorf("bayi 3 sert sınırı yazılmadı:\n%s", satir)
	}
	if !strings.Contains(satir, "limit -p bsoft=0 bhard=0m 3000008 /") {
		t.Errorf("silinmiş bayinin eski sınırı kaldırılmadı:\n%s", satir)
	}
	if strings.Contains(satir, " 1234 ") || strings.Contains(satir, " 0 /") {
		t.Errorf("panel dışı proje kimliğine dokunuldu:\n%s", satir)
	}
	if r.Mod != ModSert || r.SinirYazilan != 1 || r.SinirKalkan != 1 {
		t.Errorf("rapor: %+v", r)
	}
	// Sınır zaten doğruysa (1000 MB = 1024000 KB) yeniden yazılmaz.
	rapor = "#3000003 5000 0 1024000 00 [-]\n"
	projeSahte(o, true, rapor, "")
	o.Komut.Sifirla()
	r, _ = ProjeUygula(ctx, db, 0)
	if strings.Contains(strings.Join(projeSatirlari(o), "\n"), "limit -p") || r.SinirYazilan != 0 {
		t.Errorf("doğru sınır yeniden yazıldı: %v", projeSatirlari(o))
	}
	// Havuz KAPATILIRSA (disk 0 = sınırsız) sınır kaldırılır ve hostingler projeden çıkarılır.
	if _, err := db.Exec(`UPDATE users SET max_disk_mb=0 WHERE id=3`); err != nil {
		t.Fatal(err)
	}
	o.Komut.Sifirla()
	r, _ = ProjeUygula(ctx, db, 0)
	satir = strings.Join(projeSatirlari(o), "\n")
	if !strings.Contains(satir, "limit -p bsoft=0 bhard=0m 3000003 /") || !strings.Contains(satir, "project -C -p "+filepath.Join(o.Ev, "c_d1")+" 3000003 /") {
		t.Errorf("havuz kapanınca sınır/proje temizlenmedi:\n%s", satir)
	}
	if projeIsaretiOku("c_d1") != 0 || projeIsaretiOku("c_d2") != 0 {
		t.Error("işaret dosyaları temizlenmedi")
	}
	if r.Temizlenen != 2 {
		t.Errorf("rapor: %+v", r)
	}
}

// Devir: hosting havuzlu bayiden havuzsuz bayiye geçince proje temizlenir; havuzlu bayiye geçince yenisi atanır.
func TestProjeUygula_DevirVeTekHosting(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	projeSeed(t, db, o)
	bayiEkle(t, db, 5, "bayie", 0, 0, 2000, 0)
	projeSahte(o, false, "", "")
	ctx := context.Background()
	if _, err := ProjeUygula(ctx, db, 0); err != nil {
		t.Fatal(err)
	}
	// c_d1: bayi 3 → bayi 5 (başka disk havuzu).
	if _, err := db.Exec(`UPDATE domains SET reseller_id=5 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	o.Komut.Sifirla()
	if err := HostingDisk(ctx, db, 1); err != nil {
		t.Fatal(err)
	}
	want := []string{"xfs_quota -x -c project -s -p " + filepath.Join(o.Ev, "c_d1") + " 3000005 /"}
	if got := projeSatirlari(o); !reflect.DeepEqual(got, want) {
		t.Errorf("devir (havuzlu→havuzlu):\n got %q\nwant %q", got, want)
	}
	if projeIsaretiOku("c_d1") != 3000005 || projeIsaretiOku("c_d2") != 3000003 {
		t.Error("yalnız devredilen hostingin işareti değişmeli")
	}
	// c_d1: bayi 5 → bayi 4 (havuzsuz): eski proje temizlenir.
	if _, err := db.Exec(`UPDATE domains SET reseller_id=4 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	o.Komut.Sifirla()
	if err := HostingDisk(ctx, db, 1); err != nil {
		t.Fatal(err)
	}
	want = []string{"xfs_quota -x -c project -C -p " + filepath.Join(o.Ev, "c_d1") + " 3000005 /"}
	if got := projeSatirlari(o); !reflect.DeepEqual(got, want) {
		t.Errorf("devir (havuzlu→havuzsuz):\n got %q\nwant %q", got, want)
	}
	if projeIsaretiOku("c_d1") != 0 {
		t.Error("işaret kalmış")
	}
	// Kapsam: bayiID=3 turu yalnız bayi 3'ün hostinglerine (ve onun projesindekilere) bakar.
	o.Komut.Sifirla()
	if _, err := ProjeUygula(ctx, db, 3); err != nil {
		t.Fatal(err)
	}
	if len(projeSatirlari(o)) != 0 {
		t.Errorf("kapsam dışı hostinglere dokunuldu: %v", projeSatirlari(o))
	}
}

// Bir hostingin hatası diğerlerini durdurmaz; başarısız hostingin işareti YAZILMAZ (sonraki tur yeniden dener).
func TestProjeUygula_HostingHatasiIzoleEdilir(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	projeSeed(t, db, o)
	projeSahte(o, false, "", "c_d1")
	r, err := ProjeUygula(context.Background(), db, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Hatalar) != 1 || !strings.Contains(r.Hatalar[0], "c_d1") || r.Atanan != 1 {
		t.Errorf("rapor: %+v", r)
	}
	if projeIsaretiOku("c_d1") != 0 || projeIsaretiOku("c_d2") != 3000003 {
		t.Error("başarısız hostingin işareti yazıldı / başarılınınki yazılmadı")
	}
}

// Ev dizini olmayan hosting (henüz kurulmamış / silinmiş) atlanır: komut ve işaret yok.
func TestProjeUygula_EvDiziniYoksaAtlanir_VeXFSDegilseDokunmaz(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	bayiEkle(t, db, 3, "bayic", 0, 0, 1000, 0)
	hostingEkle(t, db, 1, "d1.test", "c_d1", 3, 1)
	projeSahte(o, false, "", "")
	r, _ := ProjeUygula(context.Background(), db, 0)
	if r.Atlanan != 1 || len(projeSatirlari(o)) != 0 || projeIsaretiOku("c_d1") != 0 {
		t.Errorf("ev yokken: %+v %v", r, projeSatirlari(o))
	}
	evKur(t, o, "c_d1")
	o.Komut.Yanit = func(satir string) (string, error, bool) {
		if strings.HasPrefix(satir, "findmnt") {
			return "/ ext4 rw\n", nil, true
		}
		return "", nil, false
	}
	o.Komut.Sifirla()
	r, _ = ProjeUygula(context.Background(), db, 0)
	if r.Mod != ModDesteklenmiyor || len(projeSatirlari(o)) != 0 {
		t.Errorf("ext4'te komut çalıştı: %+v %v", r, o.Komut.Satirlar())
	}
}

func TestDiskKullanimlari_KaynakOnceligi(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	bayiEkle(t, db, 3, "bayic", 0, 0, 1000, 0)
	bayiEkle(t, db, 4, "bayid", 0, 0, 1000, 0)
	hostingEkle(t, db, 1, "d1.test", "c_d1", 3, 111)
	hostingEkle(t, db, 2, "d2.test", "c_d2", 4, 222)
	ctx := context.Background()

	// 1) Proje sayacı (hesap açık + proje atanmış) en güvenilir kaynak.
	o.Komut.Yanit = func(satir string) (string, error, bool) {
		switch {
		case strings.HasPrefix(satir, "findmnt"):
			return "/ xfs rw\n", nil, true
		case strings.HasPrefix(satir, "xfs_quota -x -c state -p"):
			return pquotaAcik, nil, true
		case strings.HasPrefix(satir, "xfs_quota -x -c report -p"):
			return "#3000003 4096 0 0 00 [-]\n", nil, true
		case strings.HasPrefix(satir, "xfs_quota -x -c report -u"):
			return "c_d2 2048 0 0 00 [-]\n", nil, true
		}
		return "", nil, false
	}
	k := DiskKullanimlari(ctx, db, []int64{3, 4})
	if k[3].Kaynak != "proje" || k[3].KullanilanKB != 4096 {
		t.Errorf("bayi 3 (proje sayacı): %+v", k[3])
	}
	// 2) Proje yoksa hosting kullanıcı kotası toplamı.
	if k[4].Kaynak != "kullanici_kotasi" || k[4].KullanilanKB != 2048 {
		t.Errorf("bayi 4 (kullanıcı kotası): %+v", k[4])
	}
	// 3) Hiçbiri yoksa veritabanı (boyut_kb).
	o.Komut.Yanit = func(satir string) (string, error, bool) {
		return "", errors.New("yok"), strings.HasPrefix(satir, "findmnt")
	}
	k = DiskKullanimlari(ctx, db, []int64{3, 4})
	if k[3].Kaynak != "veritabani" || k[3].KullanilanKB != 111 || k[4].KullanilanKB != 222 {
		t.Errorf("veritabanı yedeği: %+v", k)
	}
	if len(DiskKullanimlari(ctx, db, nil)) != 0 {
		t.Error("boş bayi listesi sonuç üretti")
	}
}

func TestAsimdakiBayiler_SertSinirOncesiUyari(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	bayiEkle(t, db, 3, "tasan", 0, 0, 1000, 0) // 2.000.000 KB ≈ 1953 MB > 1000
	bayiEkle(t, db, 4, "sigan", 0, 0, 1000, 0) // 100 KB
	bayiEkle(t, db, 5, "havuzsuz", 0, 0, 0, 0) // disk havuzu yok → listelenmez
	hostingEkle(t, db, 1, "a.test", "c_a", 3, 2_000_000)
	hostingEkle(t, db, 2, "b.test", "c_b", 4, 100)
	hostingEkle(t, db, 3, "c.test", "c_c", 5, 9_000_000)
	o.Komut.Yanit = func(satir string) (string, error, bool) {
		return "", errors.New("yok"), strings.HasPrefix(satir, "findmnt")
	}
	got, err := AsimdakiBayiler(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].BayiID != 3 || got[0].Kullanici != "tasan" || got[0].KullanilanMB != 1953 || got[0].SinirMB != 1000 {
		t.Errorf("aşan bayiler: %+v", got)
	}
}

func TestDurumu_BekleyenVeBayiGorunumu(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	seedBayiler(t, db, o)
	ctx := context.Background()
	if _, err := Uzlastir(ctx, db); err != nil {
		t.Fatal(err)
	}
	o.Komut.Yanit = func(satir string) (string, error, bool) {
		switch {
		case strings.HasPrefix(satir, "systemctl show"):
			// c_a: yapılandırma yeni dilimde, çalışan servis eski dilimde → bekleyen
			return "Id=php-fpm-c_a.service\nActiveState=active\nSlice=girginos-bayi1-c_a.slice\nControlGroup=/girginos.slice/girginos-c_a.slice/php-fpm-c_a.service\n\n" +
				"Id=php-fpm-c_b.service\nActiveState=active\nSlice=girginos-bayi1-c_b.slice\nControlGroup=/girginos.slice/girginos-bayi1.slice/girginos-bayi1-c_b.slice/x\n", nil, true
		case strings.HasPrefix(satir, "findmnt"):
			return "/ xfs rw\n", nil, true
		case strings.HasPrefix(satir, "xfs_quota -x -c state -p"):
			return pquotaKapali, nil, true
		}
		return "", nil, false
	}
	d, err := Durumu(ctx, db, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !d.DilimAktif || d.Hostingler != 2 || d.Uyeler != 2 || d.Bekleyen != 1 || len(d.BekleyenAlanlar) != 1 || d.BekleyenAlanlar[0] != "a.test" {
		t.Errorf("durum: %+v", d)
	}
	if !d.YenidenBaslatmaGerekli || d.Havuz.RAMMB != 2048 {
		t.Errorf("durum: %+v", d)
	}
	if d.Disk.Mod != "sinirsiz" {
		t.Errorf("disk havuzu olmayan bayide mod %q", d.Disk.Mod)
	}
	// Bayiye gösterilen görünüm sunucu iç ayrıntılarını (mod/mesaj, yeniden başlatma) taşımaz.
	g := d.BayiGorunumu()
	if g.Disk.Mod != "" || g.Disk.Mesaj != "" || g.YenidenBaslatmaGerekli || g.Bekleyen != 1 {
		t.Errorf("bayi görünümü: %+v", g)
	}
	// Bayi olmayan kimlik / yönetici → ErrBayiYok.
	if _, err := Durumu(ctx, db, 100); !errors.Is(err, ErrBayiYok) {
		t.Errorf("yönetici kimliği için hata: %v", err)
	}
	if _, err := Durumu(ctx, db, 12345); !errors.Is(err, ErrBayiYok) {
		t.Errorf("olmayan bayi: %v", err)
	}
	// Özet listesi: yalnız bayiler, üyelik sayıları doğru.
	ozet, err := TumOzetler(ctx, db)
	if err != nil || len(ozet) != 3 {
		t.Fatalf("özet: %d %v", len(ozet), err)
	}
	for _, x := range ozet {
		if x.Kullanici == "root" {
			t.Error("yönetici özet listesinde")
		}
		if x.BayiID == 1 && (x.Hostingler != 2 || x.Uyeler != 2 || !x.DilimAktif) {
			t.Errorf("bayi 1 özeti: %+v", x)
		}
		if x.BayiID == 2 && (x.Hostingler != 1 || x.Uyeler != 0 || x.DilimAktif) {
			t.Errorf("bayi 2 özeti: %+v", x)
		}
	}
	_ = bayidilim.Uyelik
}
