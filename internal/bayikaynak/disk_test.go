package bayikaynak

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestProjeKimligi_AralikVeSinirlar(t *testing.T) {
	if ProjeKimligi(1) != ProjeTaban+1 || ProjeKimligi(900_000_000) != ProjeTaban+900_000_000 {
		t.Error("proje kimliği = taban + bayi kimliği değil")
	}
	if ProjeKimligi(0) != 0 || ProjeKimligi(-5) != 0 || ProjeKimligi(900_000_001) != 0 {
		t.Error("geçersiz bayi kimliği proje kimliği ÜRETMEMELİ")
	}
	if b, ok := ProjedenBayi(ProjeTaban + 42); !ok || b != 42 {
		t.Errorf("ProjedenBayi = %d %v", b, ok)
	}
	// Panel aralığı dışındaki projeler (docker, elle) bayi sayılmaz → asla dokunulmaz.
	for _, p := range []int64{0, 1, 100, 2_999_999, ProjeTaban, ProjeTaban + 900_000_001} {
		if _, ok := ProjedenBayi(p); ok {
			t.Errorf("panel aralığı dışındaki proje %d bayi sayıldı", p)
		}
	}
}

// Komut üretimi (gerçek xfs_quota ÇALIŞMAZ): tam argv, kabuk yok, sert sınır yalnız bhard.
func TestProjeKomutArgumanlari(t *testing.T) {
	if got := projeAtaArgs("/", "/home/c_x", 3000007); !reflect.DeepEqual(got, []string{"-x", "-c", "project -s -p /home/c_x 3000007", "/"}) {
		t.Errorf("ata = %q", got)
	}
	if got := projeTemizleArgs("/", "/home/c_x", 3000007); !reflect.DeepEqual(got, []string{"-x", "-c", "project -C -p /home/c_x 3000007", "/"}) {
		t.Errorf("temizle = %q", got)
	}
	if got := projeLimitArgs("/", 3000007, 51200); !reflect.DeepEqual(got, []string{"-x", "-c", "limit -p bsoft=0 bhard=51200m 3000007", "/"}) {
		t.Errorf("limit = %q", got)
	}
	// Yumuşak sınır HİÇ yazılmaz (XFS yumuşak sınırı 7 gün sonra sert olur → sessiz erken kesinti).
	if g := projeLimitArgs("/", 1, 10); !strings.Contains(g[2], "bsoft=0") {
		t.Errorf("yumuşak sınır sıfırlanmıyor: %q", g)
	}
	// Sınır kaldırma = 0, negatif → 0 (asla negatif/yanlış sınır yazılmaz).
	if g := projeLimitArgs("/", 1, -5); !strings.Contains(g[2], "bhard=0m") {
		t.Errorf("negatif sınır: %q", g)
	}
}

func TestProjeRaporAyristir(t *testing.T) {
	out := "#0   100 0 0 00 [--------]\n#3000002   12345   0   102400   00 [--------]\n#3000003 999 0 0 00 [--------]\nbozuk satir\n#abc 1 0 2 00 [-]\n#3000004 x 0 5 00 [-]\n"
	got := projeRaporAyristir(out)
	if got[3000002] != [2]int64{12345, 102400} || got[3000003] != [2]int64{999, 0} || got[0] != [2]int64{100, 0} {
		t.Errorf("rapor = %v", got)
	}
	if _, ok := got[3000004]; ok {
		t.Error("sayısal olmayan kullanım satırı kabul edildi")
	}
	k := kullaniciRaporAyristir("root 5 0 0 00 [-]\nc_a 1024 0 2048 00 [-]\nc_b-x 7 0 0 00 [-]\nc_c 12 0 0 00 [-]\n")
	if len(k) != 2 || k["c_a"] != 1024 || k["c_c"] != 12 {
		t.Errorf("kullanıcı raporu = %v (yalnız geçerli c_ sk'leri)", k)
	}
}

func TestProjeDurumuAyristir(t *testing.T) {
	kapali := "User quota state on /home (/dev/vda4)\n  Accounting: ON\n  Enforcement: ON\nGroup quota state on /home (/dev/vda4)\n  Accounting: OFF\n  Enforcement: OFF\nProject quota state on /home (/dev/vda4)\n  Accounting: OFF\n  Enforcement: OFF\n  Inode: #131 (1 blocks, 1 extents)\n"
	if h, z := projeDurumuAyristir(kapali); h || z {
		t.Errorf("pquota kapalı ama (%v,%v)", h, z)
	}
	// Kullanıcı kotası AÇIK olsa da proje bölümü OKUNMALI (eski hata: ilk Accounting satırına bakmak).
	acik := "User quota state on / (/dev/vda4)\n  Accounting: OFF\n  Enforcement: OFF\nProject quota state on / (/dev/vda4)\n  Accounting: ON\n  Enforcement: ON\n"
	if h, z := projeDurumuAyristir(acik); !h || !z {
		t.Errorf("pquota açık ama (%v,%v)", h, z)
	}
	sadeceHesap := "Project quota state on / (/dev/vda4)\n  Accounting: ON\n  Enforcement: OFF\n"
	if h, z := projeDurumuAyristir(sadeceHesap); !h || z {
		t.Errorf("yalnız hesap: (%v,%v)", h, z)
	}
	if h, z := projeDurumuAyristir(""); h || z {
		t.Error("boş çıktıda açık sayıldı")
	}
}

func TestRootflagsVeOnerilen(t *testing.T) {
	if rootflagsAl("ro root=/dev/vda4 rootflags=uquota quiet") != "uquota" || rootflagsAl("ro quiet") != "" {
		t.Error("rootflagsAl")
	}
	if rootflagsAl("rootflags=a rootflags=b") != "b" {
		t.Error("son rootflags= kazanır")
	}
	cases := []struct {
		ad, mevcut, mount, bek string
		elle                   bool
	}{
		{"hiç yok", "", "rw", "uquota,pquota", false},
		{"uquota korunur", "uquota", "rw,usrquota", "uquota,pquota", false},
		{"başka bayraklar korunur", "inode64,uquota", "rw", "inode64,uquota,pquota", false},
		{"zaten var → değişmez", "uquota,pquota", "rw", "uquota,pquota", false},
		{"prjquota eşanlamlısı", "usrquota,prjquota", "rw", "usrquota,prjquota", false},
		{"pqnoenforce zorlayanla değişir", "uquota,pqnoenforce", "rw", "uquota,pquota", false},
		{"grup kotası → elle", "uquota,gquota", "rw", "", true},
		{"mount grpquota → elle", "uquota", "rw,grpquota", "", true},
		{"boşluk/virgül gürültüsü", " uquota ,, ", "rw", "uquota,pquota", false},
	}
	for _, c := range cases {
		got, elle := OnerilenRootflags(c.mevcut, c.mount)
		if got != c.bek || elle != c.elle {
			t.Errorf("%s: (%q,%v) beklenen (%q,%v)", c.ad, got, elle, c.bek, c.elle)
		}
	}
}

// diskSahte: findmnt/xfs_quota/grubby çıktılarını senaryoya göre verir.
func diskSahte(o *ortam, fs, secenek, quotaDurum, grubbyArgs string) {
	o.Komut.Yanit = func(satir string) (string, error, bool) {
		switch {
		case strings.HasPrefix(satir, "findmnt"):
			return "/ " + fs + " " + secenek + "\n", nil, true
		case strings.HasPrefix(satir, "xfs_quota -x -c state -p"):
			return quotaDurum, nil, true
		case strings.HasPrefix(satir, "grubby --info=DEFAULT"):
			if grubbyArgs == "" {
				return "", errors.New("grubby yok"), true
			}
			return "index=0\nkernel=\"/boot/vmlinuz\"\nargs=\"" + grubbyArgs + "\"\n", nil, true
		}
		return "", nil, false
	}
}

const (
	pquotaKapali = "Project quota state on / (/dev/vda4)\n  Accounting: OFF\n  Enforcement: OFF\n"
	pquotaAcik   = "Project quota state on / (/dev/vda4)\n  Accounting: ON\n  Enforcement: ON\n"
)

func TestDiskDurumu_Modlar(t *testing.T) {
	o := ortamKur(t)
	ctx := context.Background()

	diskSahte(o, "ext4", "rw", "", "")
	if d := DiskDurumu(ctx); d.Mod != ModDesteklenmiyor || d.XFS {
		t.Errorf("ext4: %+v", d)
	}
	// XFS, pquota kapalı, kernel girdisi hazır DEĞİL → ölçüm + uyarı (UYGULANMIYOR; açıkça söylenir).
	diskSahte(o, "xfs", "rw,usrquota", pquotaKapali, "ro rootflags=uquota")
	d := DiskDurumu(ctx)
	if d.Mod != ModOlcumUyari || d.ZorlamaAcik || d.YenidenBaslatma || !strings.Contains(d.Mesaj, "UYGULANMIYOR") {
		t.Errorf("pquota yok: %+v", d)
	}
	if d.MevcutRootflags != "uquota" || d.OnerilenRootflag != "uquota,pquota" {
		t.Errorf("rootflags: mevcut=%q önerilen=%q", d.MevcutRootflags, d.OnerilenRootflag)
	}
	// Kernel girdisine pquota yazılmış ama çalışan çekirdek henüz açmadı → yeniden başlatma bekleniyor.
	diskSahte(o, "xfs", "rw,usrquota", pquotaKapali, "ro rootflags=uquota,pquota")
	if d := DiskDurumu(ctx); d.Mod != ModBekliyor || !d.YenidenBaslatma || !d.BootHazir {
		t.Errorf("bekleyen: %+v", d)
	}
	// Çalışan montajda zorlama açık.
	diskSahte(o, "xfs", "rw,usrquota,prjquota", pquotaAcik, "ro rootflags=uquota,pquota")
	if d := DiskDurumu(ctx); d.Mod != ModSert || !d.ZorlamaAcik || !d.HesapAcik {
		t.Errorf("sert: %+v", d)
	}
	// Grup kotası açık: otomatik değiştirilmez.
	diskSahte(o, "xfs", "rw,grpquota", pquotaKapali, "ro rootflags=uquota,gquota")
	d = DiskDurumu(ctx)
	if d.Mod != ModOlcumUyari || !d.Elle {
		t.Errorf("grup kotası: %+v", d)
	}
	// findmnt başarısız → desteklenmiyor (varsayım yok).
	o.Komut.Yanit = func(satir string) (string, error, bool) {
		if strings.HasPrefix(satir, "findmnt") {
			return "", errors.New("yok"), true
		}
		return "", nil, false
	}
	if d := DiskDurumu(ctx); d.Mod != ModDesteklenmiyor {
		t.Errorf("findmnt hatası: %+v", d)
	}
}

func TestEtkinlestir_KuruCalistirmaGrubbyVeGeriOkuma(t *testing.T) {
	o := ortamKur(t)
	ctx := context.Background()
	diskSahte(o, "xfs", "rw,usrquota", pquotaKapali, "ro rootflags=uquota")

	// KURU ÇALIŞTIRMA: grubby --update-kernel ASLA çağrılmaz, sentinel yazılmaz.
	s, err := Etkinlestir(ctx, false)
	if err != nil || s.Uygulandi {
		t.Fatalf("kuru çalıştırma: %+v %v", s, err)
	}
	if !strings.Contains(s.Komut, "grubby --update-kernel=ALL --args=rootflags=uquota,pquota") {
		t.Errorf("önerilen komut: %q", s.Komut)
	}
	if o.Komut.Say("grubby --update-kernel") != 0 || o.Var(o.Sentinel) {
		t.Fatal("kuru çalıştırma kernel'e dokundu")
	}

	// UYGULA: grubby doğru argümanla çağrılır; geri okuma yeni rootflags'i görürse başarılı.
	uygulandi := false
	o.Komut.Yanit = func(satir string) (string, error, bool) {
		switch {
		case strings.HasPrefix(satir, "findmnt"):
			return "/ xfs rw,usrquota\n", nil, true
		case strings.HasPrefix(satir, "xfs_quota"):
			return pquotaKapali, nil, true
		case strings.HasPrefix(satir, "grubby --update-kernel"):
			uygulandi = true
			return "", nil, true
		case strings.HasPrefix(satir, "grubby --info=DEFAULT"):
			if uygulandi {
				return "args=\"ro rootflags=uquota,pquota\"\n", nil, true
			}
			return "args=\"ro rootflags=uquota\"\n", nil, true
		}
		return "", nil, false
	}
	s, err = Etkinlestir(ctx, true)
	if err != nil || !s.Uygulandi || s.Mod != ModBekliyor {
		t.Fatalf("uygulama: %+v %v", s, err)
	}
	if o.Komut.Say("grubby --update-kernel=ALL --args=rootflags=uquota,pquota") != 1 {
		t.Errorf("grubby çağrıları: %v", o.Komut.Satirlar())
	}
	if !o.Var(o.Sentinel) {
		t.Error("yeniden başlatma sentinel'i yazılmadı")
	}
	// İkinci çağrı: kernel girdisi hazır → hiçbir şey çalıştırılmaz (idempotent).
	o.Komut.Sifirla()
	s, err = Etkinlestir(ctx, true)
	if err != nil || s.Uygulandi || o.Komut.Say("grubby --update-kernel") != 0 {
		t.Errorf("idempotent değil: %+v %v %v", s, err, o.Komut.Satirlar())
	}
}

// grubby "başarılı" dönüp hiçbir şeyi değiştirmediyse bu BAŞARI sayılmaz (geri okuma doğrular).
func TestEtkinlestir_GeriOkumaBasarisizsaHata(t *testing.T) {
	o := ortamKur(t)
	diskSahte(o, "xfs", "rw,usrquota", pquotaKapali, "ro rootflags=uquota") // değişmeyen çıktı
	_, err := Etkinlestir(context.Background(), true)
	if err == nil || !strings.Contains(err.Error(), "doğrulanamadı") {
		t.Fatalf("hata beklenirdi: %v", err)
	}
	if o.Var(o.Sentinel) {
		t.Error("doğrulanamayan değişiklik için sentinel yazıldı")
	}
}

func TestEtkinlestir_ReddedilenDurumlar(t *testing.T) {
	o := ortamKur(t)
	ctx := context.Background()
	for _, c := range []struct {
		ad, fs, sec, durum, args string
	}{
		{"xfs değil", "ext4", "rw", "", ""},
		{"grup kotası → elle", "xfs", "rw,grpquota", pquotaKapali, "ro rootflags=uquota,gquota"},
		{"grubby yok → elle", "xfs", "rw,usrquota", pquotaKapali, ""},
	} {
		diskSahte(o, c.fs, c.sec, c.durum, c.args)
		o.Komut.Sifirla()
		yolBul = func(ad string) (string, error) {
			if ad == "grubby" && c.args == "" {
				return "", errors.New("grubby yok")
			}
			return "/usr/sbin/" + ad, nil
		}
		if _, err := Etkinlestir(ctx, true); err == nil {
			t.Errorf("%s: hata beklenirdi", c.ad)
		}
		if o.Komut.Say("grubby --update-kernel") != 0 {
			t.Errorf("%s: reddedilen durumda grubby çalıştı", c.ad)
		}
	}
	// Zaten etkin: iş yok.
	diskSahte(o, "xfs", "rw,usrquota,prjquota", pquotaAcik, "ro rootflags=uquota,pquota")
	o.Komut.Sifirla()
	s, err := Etkinlestir(ctx, true)
	if err != nil || s.Uygulandi || o.Komut.Say("grubby --update-kernel") != 0 {
		t.Errorf("zaten etkin: %+v %v", s, err)
	}
	_ = os.Remove(o.Sentinel)
}
