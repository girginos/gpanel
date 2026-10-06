package arsiv

// faz2_test.go — faz 2 eklemeleri: kullanım profilleri (500 GiB yol / 50 GiB yükleme),
// manifestten üye adı türeten yardımcılar, AcHesap tutamağı ve CikarDizin (posta).

import (
	"archive/tar"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestProfiller_Tavanlar(t *testing.T) {
	yol, yuk := SecenekYol(), SecenekYukleme()
	if YolTavan != 500<<30 || YuklemeTavan != 50<<30 {
		t.Fatalf("tavanlar kullanıcı kararından saptı: yol=%d yükleme=%d", YolTavan, YuklemeTavan)
	}
	// Açılmış boyut tavanı arşiv tavanının 4 katı; yüklemeninki yolunkinden küçük.
	if yol.Sinirlar.ToplamBayt != 4*YolTavan || yuk.Sinirlar.ToplamBayt != 4*YuklemeTavan {
		t.Fatalf("açılmış boyut tavanı: yol=%d yükleme=%d", yol.Sinirlar.ToplamBayt, yuk.Sinirlar.ToplamBayt)
	}
	if yuk.Sinirlar.UyeSayisi >= yol.Sinirlar.UyeSayisi || yuk.Sinirlar.AdBayt > yol.Sinirlar.AdBayt || yuk.DumpSinir > yol.DumpSinir {
		t.Fatalf("yükleme profili yolunkinden gevşek olamaz: %+v / %+v", yuk, yol)
	}
	// 🔴 Bomba koruması profillerde AÇIK kalır: sıfır = varsayılan (500×, 512 MiB eşik), üst veri 4 MiB.
	for ad, s := range map[string]Sinirlar{"yol": yol.Sinirlar.tamamla(), "yukleme": yuk.Sinirlar.tamamla()} {
		if s.BombaOrani != 500 || s.BombaEsik != 512<<20 || s.UstVeriBayt != maxUstVeri || s.BagSayisi != varsayilanBagSayi {
			t.Errorf("%s: bomba/üst veri/bağ sınırları gevşedi: %+v", ad, s)
		}
		if s.ToplamBayt <= 0 || s.UyeSayisi <= 0 || s.AdBayt <= 0 {
			t.Errorf("%s: sınır sıfırlandı: %+v", ad, s)
		}
	}
}

func acHesapT(t *testing.T, uyeler []uyeT) (*Arsiv, *Manifest) {
	t.Helper()
	a, m, err := AcHesap(context.Background(), dosyala(t, gzUret(t, tarUret(t, uyeler...))), Secenek{})
	if err != nil {
		t.Fatal(err)
	}
	return a, m
}

func TestAcHesap_CPanelYardimcilari(t *testing.T) {
	a, m := acHesapT(t, cpmoveUyeler())
	if m.Kok != "cpmove-u" || m.Tur != TurCPanel {
		t.Fatalf("kök/tür: %q %q", m.Kok, m.Tur)
	}
	if got := m.DosyaOnek("public_html"); got != "cpmove-u/homedir/public_html" || !a.Var(got) {
		t.Errorf("DosyaOnek = %q (var=%v)", got, a.Var(got))
	}
	if m.DosyaOnek("") != "" {
		t.Error("belge kökü yoksa önek de yok")
	}
	if got := m.EvOnek("ornek.com"); got != "cpmove-u/homedir" {
		t.Errorf("EvOnek = %q", got)
	}
	if got := m.PostaAlanOnek("ornek.com"); got != "cpmove-u/homedir/mail/ornek.com" {
		t.Errorf("PostaAlanOnek = %q", got)
	}
	if got := m.PostaOnek(a, PostaHesabi{Alan: "ornek.com", Yerel: "info"}); got != "cpmove-u/homedir/mail/ornek.com/info" {
		t.Errorf("PostaOnek = %q", got)
	}
	if got := m.PostaOnek(a, PostaHesabi{Alan: "ornek.com", Yerel: "yok"}); got != "" {
		t.Errorf("yedekte olmayan kutu için önek boş olmalı: %q", got)
	}
	if got := m.DNSUye("ornek.com"); got != "cpmove-u/dnszones/ornek.com.db" {
		t.Errorf("DNSUye = %q", got)
	}
	if b, ok := a.UstVeriAl(m.DNSUye("ornek.com")); !ok || !strings.Contains(string(b), "@ IN A") {
		t.Errorf("bölge dosyası üst veriden okunamadı: %v %q", ok, b)
	}
	if _, ok := a.UstVeriAl("./cpmove-u/dnszones/ornek.com.db"); !ok {
		t.Error("UstVeriAl './' önekini atmalı")
	}
	if a.Var("cpmove-u/homedir/yok") || a.Var("../etc") {
		t.Error("olmayan üye 'var' sayıldı")
	}
}

func TestAcHesap_DirectAdminYardimcilari(t *testing.T) {
	a, m := acHesapT(t, daUyeler())
	if m.Kok != "" || m.Tur != TurDirectAdmin {
		t.Fatalf("kök/tür: %q %q", m.Kok, m.Tur)
	}
	if got := m.DosyaOnek("domains/ornek.com/public_html"); got != "domains/ornek.com/public_html" {
		t.Errorf("DosyaOnek = %q", got)
	}
	if got := m.EvOnek("ornek.com"); got != "domains/ornek.com" {
		t.Errorf("EvOnek = %q", got)
	}
	if got := m.PostaOnek(a, PostaHesabi{Alan: "ornek.com", Yerel: "info"}); got != "imap/ornek.com/info" {
		t.Errorf("PostaOnek = %q", got)
	}
	if m.DNSUye("ornek.com") != "" {
		t.Error("DirectAdmin'de bölge dosyası okunmaz")
	}
	// İç içe Maildir yerleşimi seçilir.
	ic := ekle(daUyeler(), uyeT{ad: "imap/ornek.com/info/Maildir/cur/1", veri: "m"})
	a2, m2 := acHesapT(t, ic)
	if got := m2.PostaOnek(a2, PostaHesabi{Alan: "ornek.com", Yerel: "info"}); got != "imap/ornek.com/info/Maildir" {
		t.Errorf("iç içe Maildir: %q", got)
	}
}

func TestCronOku_HamSatirlarVeSayilanKayiplar(t *testing.T) {
	cron := "MAILTO=\"\"\n# yorum\n\n0 3 * * * a\nPATH=/x\n@reboot b\n* * * * * \x01kontrol\n"
	m, err := okuT(t, degistir(cpmoveUyeler(), "cpmove-u/cron/u", func(u *uyeT) { u.veri = cron }), Sinirlar{}, Secenek{})
	if err != nil {
		t.Fatal(err)
	}
	// Ortam satırları sırayla dahil; yorum/boş atılır; kontrol karakterli satır SESSİZCE düşmez.
	if got := strings.Join(m.Cron, "|"); got != "MAILTO=\"\"|0 3 * * * a|PATH=/x|@reboot b" {
		t.Fatalf("cron = %q", got)
	}
	if !uyariVar(m, "1 cron satırı geçersiz karakter") {
		t.Fatalf("kayıp satır raporlanmadı: %v", m.Uyarilar)
	}
	if uyariVar(m, "otomatik kurulmaz") {
		t.Error("eski 'otomatik kurulmaz' uyarısı kalmamalı (cron artık taşınır)")
	}
	// Sınır aşımı da sayıyla raporlanır.
	var b strings.Builder
	for i := 0; i < maxCron+7; i++ {
		b.WriteString("0 0 * * * c\n")
	}
	m, err = okuT(t, degistir(cpmoveUyeler(), "cpmove-u/cron/u", func(u *uyeT) { u.veri = b.String() }), Sinirlar{}, Secenek{})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Cron) != maxCron || !uyariVar(m, "7 satır taşınamadı") {
		t.Fatalf("sınır: %d satır, uyarılar %v", len(m.Cron), m.Uyarilar)
	}
}

// ---- CikarDizin (posta) ----

func postaUyeler() []uyeT {
	p := "cpmove-u/homedir/mail/ornek.com/"
	return []uyeT{
		dizin("cpmove-u/"), dizin("cpmove-u/homedir/"), dizin("cpmove-u/homedir/mail/"), dizin(p),
		dizin(p + "info/"), dizin(p + "info/cur/"), dizin(p + "info/new/"), dizin(p + "info/.Trash/"),
		{ad: p + "info/cur/1:2,S", veri: "mesaj-1"},
		{ad: p + "info/new/2", veri: "mesaj-2"},
		{ad: p + "info/.Trash/3", veri: "mesaj-3"},
		{ad: p + "info/dovecot-uidlist", veri: "ic"},
		{ad: p + "info/sieve/aktif.sieve", veri: "redirect"},
		dizin(p + "evil/"), {ad: p + "evil/cur/x", veri: "baska-kutu"},
		{ad: p + "info/baglanti", tur: tar.TypeSymlink, hedef: "../evil"},
		{ad: p + "info/suid", veri: "x", mod: 0o6755},
	}
}

// Seçim bütçeye tam sığıyorsa (sınır değeri) çıkarılır.
func TestCikarDizin_ButceSinirDegeri(t *testing.T) {
	o := yeniOrtam(t)
	hedef := filepath.Join(o.base, "vmail2")
	_ = os.Mkdir(hedef, 0o755)
	_ = os.Chown(hedef, o.uid, o.gid)
	a, err := acT(t, gzUret(t, tarUret(t, postaUyeler()...)), Sinirlar{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	const onek = "cpmove-u/homedir/mail/ornek.com"
	bayt, _ := a.secim(onek)
	if _, err := a.CikarDizin(context.Background(), onek, hedef, "nobody", DizinSecenek{Butce: bayt - 1}); !errors.Is(err, ErrKota) {
		t.Fatalf("bütçe-1: %v", err)
	}
	if _, err := a.CikarDizin(context.Background(), onek, hedef, "nobody", DizinSecenek{Butce: bayt, BaglarYok: true}); err != nil {
		t.Fatalf("tam bütçe geçmeli: %v", err)
	}
}

func TestCikarDizin_PostaSuzgeciVeKimlik(t *testing.T) {
	o := yeniOrtam(t)
	hedef := filepath.Join(o.base, "vmail", "ornek.com")
	if err := os.MkdirAll(hedef, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.Chown(filepath.Dir(hedef), o.uid, o.gid)
	_ = os.Chown(hedef, o.uid, o.gid)
	a, err := acT(t, gzUret(t, tarUret(t, postaUyeler()...)), Sinirlar{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	izin := func(rel string) bool {
		kutu, kalan, _ := strings.Cut(rel, "/")
		return kutu == "info" && !strings.HasPrefix(kalan, "dovecot") && !strings.HasPrefix(kalan, "sieve")
	}
	r, err := a.CikarDizin(context.Background(), "cpmove-u/homedir/mail/ornek.com", hedef, "nobody", DizinSecenek{Butce: ButceSinirsiz, Izin: izin, BaglarYok: true})
	if err != nil {
		t.Fatalf("%v (tar: %v)", err, r)
	}
	for rel, icerik := range map[string]string{"info/cur/1:2,S": "mesaj-1", "info/new/2": "mesaj-2", "info/.Trash/3": "mesaj-3"} {
		b, err := os.ReadFile(filepath.Join(hedef, rel))
		if err != nil || string(b) != icerik {
			t.Errorf("%s: %q %v", rel, b, err)
		}
		fi, _ := os.Lstat(filepath.Join(hedef, rel))
		if st := fi.Sys().(*syscall.Stat_t); int(st.Uid) != o.uid {
			t.Errorf("%s verilen kullanıcıya ait değil (uid %d)", rel, st.Uid)
		}
	}
	for _, yok := range []string{"info/dovecot-uidlist", "info/sieve", "evil", "info/baglanti"} {
		if _, err := os.Lstat(filepath.Join(hedef, yok)); err == nil {
			t.Errorf("süzülmesi gereken %q yazıldı", yok)
		}
	}
	if fi, err := os.Lstat(filepath.Join(hedef, "info", "suid")); err != nil || fi.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 {
		t.Errorf("suid biti silinmeliydi: %v %v", fi, err)
	}
	if r.AtlananSayisi == 0 {
		t.Error("izin süzgeci atlananları raporlamalı")
	}
}

func TestCikarDizin_GecersizHedefVeKullanici(t *testing.T) {
	o := yeniOrtam(t)
	a, err := acT(t, gzUret(t, tarUret(t, postaUyeler()...)), Sinirlar{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	kurban := kurban(t, o.base)
	once := agacOzeti(t, kurban)
	bag := filepath.Join(o.base, "bag")
	if err := os.Symlink(kurban, bag); err != nil {
		t.Fatal(err)
	}
	dosya := filepath.Join(o.base, "dosya")
	_ = os.WriteFile(dosya, []byte("x"), 0o644)
	gercek := filepath.Join(o.base, "gercek")
	_ = os.Mkdir(gercek, 0o755)
	ctx := context.Background()
	const onek = "cpmove-u/homedir/mail/ornek.com"
	for ad, c := range map[string]struct {
		hedef, kul string
	}{
		"göreli":         {"vmail/x", "nobody"},
		"kök":            {"/", "nobody"},
		"temiz değil":    {gercek + "/../gercek", "nobody"},
		"yok":            {filepath.Join(o.base, "yok"), "nobody"},
		"dosya":          {dosya, "nobody"},
		"sembolik bağ":   {bag, "nobody"},
		"kullanıcı boş":  {gercek, ""},
		"kullanıcı root": {gercek, "root"},
		"seçenek gibi":   {gercek, "-u"},
	} {
		if _, err := a.CikarDizin(ctx, onek, c.hedef, c.kul, DizinSecenek{Butce: ButceSinirsiz}); !errors.Is(err, ErrGecersiz) {
			t.Errorf("%s: ErrGecersiz beklendi, gelen %v", ad, err)
		}
	}
	if sonra := agacOzeti(t, kurban); len(sonra) != len(once) {
		t.Fatalf("kurban ağacı değişti: %v", sonra)
	}
	if _, err := a.CikarDizin(ctx, "../x", gercek, "nobody", DizinSecenek{Butce: ButceSinirsiz}); !errors.Is(err, ErrUyeAdi) {
		t.Errorf("geçersiz önek: %v", err)
	}
	// Hedefte yeterli boş alan yoksa (seçim + disk payı > boş) hiçbir şey yazılmadan ErrDisk.
	ad, err := acT(t, gzUret(t, tarUret(t, postaUyeler()...)), Sinirlar{DiskPayi: 1 << 62}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ad.CikarDizin(ctx, onek, gercek, "nobody", DizinSecenek{Butce: ButceSinirsiz}); !errors.Is(err, ErrDisk) {
		t.Errorf("disk payı: ErrDisk beklendi, gelen %v", err)
	}
	// Bütçe seçimin boyutundan küçükse hiçbir şey yazılmadan ErrKota.
	if _, err := a.CikarDizin(ctx, onek, gercek, "nobody", DizinSecenek{Butce: 1}); !errors.Is(err, ErrKota) {
		t.Errorf("bütçe: ErrKota beklendi, gelen %v", err)
	}
	if girdiler, _ := os.ReadDir(gercek); len(girdiler) != 0 {
		t.Errorf("reddedilen çıkarma hedefe yazdı: %v", girdiler)
	}
}
