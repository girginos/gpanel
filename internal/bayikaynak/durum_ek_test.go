package bayikaynak

// durum_ek_test.go — Durumu / AsimdakiBayiler sınırları: bekleyen sorgusu ne zaman yapılır, bekleyen
// alan listesinin kesilmesi, zorlama/yeniden başlatma yalnız disk havuzu olan bayi için, aşım sınırı.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"girginospanel/internal/bayidilim"
)

// fpmYanit: tüm hostingler için "yapılandırma yeni dilimde, çalışan servis eski dilimde" çıktısı.
func fpmBekleyenCikti(skler ...string) string {
	var b strings.Builder
	for _, sk := range skler {
		fmt.Fprintf(&b, "Id=php-fpm-%s.service\nActiveState=active\nSlice=girginos-bayi1-%s.slice\nControlGroup=/girginos.slice/girginos-%s.slice/php-fpm-%s.service\n\n", sk, sk, sk, sk)
	}
	return b.String()
}

// Bekleyen sayısı tam, bekleyen ALAN listesi en çok 20 öğeyle sınırlı (yanıt şişmesin).
func TestDurumu_BekleyenAlanListesi20ileSinirli(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	bayiEkle(t, db, 1, "bayia", 1, 0, 0, 0)
	var skler []string
	for i := 1; i <= 25; i++ {
		sk := fmt.Sprintf("c_h%02d", i)
		skler = append(skler, sk)
		hostingEkle(t, db, int64(i), fmt.Sprintf("h%02d.test", 26-i), sk, 1, 10) // alan sırası sk sırasının TERSİ
		o.BirimYaz(t, "php-fpm-"+sk+".service", "[Service]\n")
	}
	ctx := context.Background()
	if _, err := Uzlastir(ctx, db); err != nil {
		t.Fatal(err)
	}
	diskSahte(o, "xfs", "rw", pquotaKapali, "")
	disk := o.Komut.Yanit
	o.Komut.Yanit = func(satir string) (string, error, bool) {
		if strings.HasPrefix(satir, "systemctl show") {
			return fpmBekleyenCikti(skler...), nil, true
		}
		return disk(satir)
	}
	d, err := Durumu(ctx, db, 1)
	if err != nil {
		t.Fatal(err)
	}
	if d.Bekleyen != 25 || len(d.BekleyenAlanlar) != 20 {
		t.Fatalf("bekleyen=%d alan listesi=%d (25 ve 20 beklenir)", d.Bekleyen, len(d.BekleyenAlanlar))
	}
	// İlk 20 sk (c_h01..c_h20 → h25..h06.test) alınır, sonra alan adına göre artan sıralanır.
	if d.BekleyenAlanlar[0] != "h06.test" || d.BekleyenAlanlar[19] != "h25.test" {
		t.Errorf("alan listesi sıralı olmalı: %v", d.BekleyenAlanlar)
	}
}

// systemd'ye yalnız havuzu olan (ya da dilim dosyası hâlâ duran) bayide sorulur; havuzsuz bayide exec yok.
func TestDurumu_BekleyenSorgusuYalnizHavuzVarkenYapilir(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	bayiEkle(t, db, 1, "bayia", 1, 0, 0, 0)
	bayiEkle(t, db, 2, "bayib", 0, 0, 0, 0)
	hostingEkle(t, db, 1, "a.test", "c_a", 1, 10)
	hostingEkle(t, db, 2, "b.test", "c_b", 2, 10)
	o.BirimYaz(t, "php-fpm-c_a.service", "[Service]\n")
	o.BirimYaz(t, "php-fpm-c_b.service", "[Service]\n")
	ctx := context.Background()
	if _, err := Uzlastir(ctx, db); err != nil {
		t.Fatal(err)
	}
	diskSahte(o, "xfs", "rw", pquotaKapali, "")
	disk := o.Komut.Yanit
	o.Komut.Yanit = func(satir string) (string, error, bool) {
		if strings.HasPrefix(satir, "systemctl show") {
			return fpmBekleyenCikti("c_a", "c_b"), nil, true
		}
		return disk(satir)
	}

	// Havuzsuz bayi: dilim dosyası da yok → hiç systemctl show çalışmaz.
	o.Komut.Sifirla()
	d, err := Durumu(ctx, db, 2)
	if err != nil {
		t.Fatal(err)
	}
	if n := o.Komut.Say("systemctl show"); n != 0 || d.Bekleyen != 0 {
		t.Errorf("havuzsuz bayide systemd'ye soruldu (%d çağrı, bekleyen %d)", n, d.Bekleyen)
	}

	// Havuzlu bayi: sorulur.
	o.Komut.Sifirla()
	d, err = Durumu(ctx, db, 1)
	if err != nil {
		t.Fatal(err)
	}
	if n := o.Komut.Say("systemctl show"); n != 1 || d.Bekleyen != 1 {
		t.Errorf("havuzlu bayide çağrı=%d bekleyen=%d", n, d.Bekleyen)
	}

	// Yönetici havuz değerlerini sıfırladı ama panelin yazdığı havuz dilimi dosyası hâlâ duruyor
	// (üyeler henüz eski ada dönmedi): bekleyenler yine hesaplanır; imzasız (yabancı) dosya sayılmaz.
	if _, err := db.Exec(`UPDATE users SET kaynak_cpu_cekirdek=0, kaynak_ram_mb=0 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	o.Komut.Sifirla()
	d, err = Durumu(ctx, db, 1)
	if err != nil {
		t.Fatal(err)
	}
	if n := o.Komut.Say("systemctl show"); n != 1 || d.DilimAktif {
		t.Errorf("imzalı havuz dilimi duruyorken: çağrı=%d dilimAktif=%v", n, d.DilimAktif)
	}
	o.BirimYaz(t, bayidilim.BayiDilim(1), "# yabanci dosya\n[Slice]\n")
	o.Komut.Sifirla()
	if _, err = Durumu(ctx, db, 1); err != nil {
		t.Fatal(err)
	}
	if n := o.Komut.Say("systemctl show"); n != 0 {
		t.Errorf("imzasız dosya havuz dilimi sayıldı (%d çağrı)", n)
	}
}

// Disk bilgisi yalnız disk havuzu olan bayi için anlamlıdır: zorlama ve "yeniden başlatma gerekli".
func TestDurumu_ZorlamaVeYenidenBaslatmaYalnizDiskHavuzluBayide(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	bayiEkle(t, db, 1, "diskli", 0, 0, 1000, 0)
	bayiEkle(t, db, 2, "disksiz", 1, 0, 0, 0)
	hostingEkle(t, db, 1, "a.test", "c_a", 1, 10)
	hostingEkle(t, db, 2, "b.test", "c_b", 2, 10)
	ctx := context.Background()
	if _, err := Uzlastir(ctx, db); err != nil {
		t.Fatal(err)
	}

	// 1) Sert sınır etkin: zorlama yalnız diskli bayide; yeniden başlatma gerekmez.
	diskSahte(o, "xfs", "rw,usrquota,prjquota", pquotaAcik, "ro rootflags=uquota,pquota")
	dk, _ := Durumu(ctx, db, 1)
	ds, _ := Durumu(ctx, db, 2)
	if !dk.Disk.Zorlama || dk.Disk.Mod != ModSert || dk.YenidenBaslatmaGerekli {
		t.Errorf("diskli bayi (sert): %+v", dk)
	}
	if ds.Disk.Zorlama || ds.Disk.Mod != "sinirsiz" || ds.YenidenBaslatmaGerekli {
		t.Errorf("disksiz bayi (sert): %+v", ds)
	}

	// 2) Kernel girdisi hazır, sunucu henüz yeniden başlamadı: yalnız diskli bayi "yeniden başlat" görür.
	diskSahte(o, "xfs", "rw,usrquota", pquotaKapali, "ro rootflags=uquota,pquota")
	dk, _ = Durumu(ctx, db, 1)
	ds, _ = Durumu(ctx, db, 2)
	if !dk.YenidenBaslatmaGerekli || dk.Disk.Mod != ModBekliyor || dk.Disk.Zorlama || dk.Bekleyen != 0 {
		t.Errorf("diskli bayi (bekliyor): %+v", dk)
	}
	if ds.YenidenBaslatmaGerekli {
		t.Errorf("disk havuzu olmayan bayi için yeniden başlatma istendi: %+v", ds)
	}
}

// Aşım listesi: kullanım TAM sınıra eşitse aşım değil; sıra bayi kimliğine göre; havuzsuz bayi listelenmez.
func TestAsimdakiBayiler_EsitlikSiraVeHavuzsuz(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	bayiEkle(t, db, 6, "esit", 0, 0, 1000, 0)    // tam 1000 MB kullanıyor → aşmıyor
	bayiEkle(t, db, 4, "ikinci", 0, 0, 1000, 0)  // 1025 MB → aşıyor
	bayiEkle(t, db, 3, "birinci", 0, 0, 1000, 0) // 2000000 KB → aşıyor
	bayiEkle(t, db, 7, "havuzsuz", 0, 0, 0, 0)   // disk havuzu yok → listelenmez
	hostingEkle(t, db, 1, "a.test", "c_a", 6, 1000*1024)
	hostingEkle(t, db, 2, "b.test", "c_b", 4, 1025*1024)
	hostingEkle(t, db, 3, "c.test", "c_c", 3, 2_000_000)
	hostingEkle(t, db, 4, "d.test", "c_d", 7, 9_000_000)
	o.Komut.Yanit = func(satir string) (string, error, bool) {
		return "", errors.New("yok"), strings.HasPrefix(satir, "findmnt")
	}
	got, err := AsimdakiBayiler(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].BayiID != 3 || got[1].BayiID != 4 {
		t.Fatalf("aşan bayiler (3 ve 4, bu sırayla beklenir): %+v", got)
	}
	if got[1].KullanilanMB != 1025 || got[1].SinirMB != 1000 || got[1].Kullanici != "ikinci" {
		t.Errorf("bayi 4 ayrıntısı: %+v", got[1])
	}
}
