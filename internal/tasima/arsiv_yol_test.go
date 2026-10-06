package tasima

// arsiv_yol_test.go — arşiv yol doğrulaması, disk önkontrolü, biçim imzası ve keşif
// listesi (DB'siz). Kural → test eşlemesi her testin başındadır.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"girginospanel/internal/tasima/arsiv"
)

// yolOrtam: root'a ait olmasına gerek kalmadan (testi çalıştıran kullanıcı = "root" rolü)
// geçici izinli kök + dosya.
type yolOrtam struct {
	kok, dizin, dosya string
}

func yolKur(t *testing.T) yolOrtam {
	t.Helper()
	kok := t.TempDir()
	if err := os.Chmod(kok, 0o755); err != nil {
		t.Fatal(err)
	}
	eski := arsivSahipUID
	arsivSahipUID = uint32(os.Geteuid())
	t.Cleanup(func() { arsivSahipUID = eski })
	dizin := filepath.Join(kok, "yedek")
	if err := os.Mkdir(dizin, 0o755); err != nil {
		t.Fatal(err)
	}
	dosya := filepath.Join(dizin, "cpmove-u.tar.gz")
	if err := os.WriteFile(dosya, []byte("0123456789abcdef"), 0o644); err != nil {
		t.Fatal(err)
	}
	return yolOrtam{kok: kok, dizin: dizin, dosya: dosya}
}

// Kural: yol mutlak+temiz, izinli kök altında, bağsız, düzenli dosya, root sahipli,
// grup/diğer yazamaz (dosya VE üst dizinler), boyut (0, tavan].
func TestArsivYolDogrula(t *testing.T) {
	o := yolKur(t)
	kokler := []string{o.kok}
	if fi, err := arsivYolDogrula(o.dosya, kokler, 1<<20); err != nil || fi.Size() != 16 {
		t.Fatalf("meşru yol reddedildi: %v", err)
	}
	disKok := t.TempDir()
	disDosya := filepath.Join(disKok, "x.tar.gz")
	_ = os.WriteFile(disDosya, []byte("x"), 0o644)

	// symlink'ler
	bagDosya := filepath.Join(o.dizin, "bag.tar.gz")
	_ = os.Symlink(o.dosya, bagDosya)
	bagDizin := filepath.Join(o.kok, "bagdizin")
	_ = os.Symlink(o.dizin, bagDizin)
	bagKok := filepath.Join(t.TempDir(), "kokbag")
	_ = os.Symlink(o.kok, bagKok)
	// izinler
	yazilir := filepath.Join(o.dizin, "yazilir.tar.gz")
	_ = os.WriteFile(yazilir, []byte("x"), 0o664)
	_ = os.Chmod(yazilir, 0o664)
	grupDizin := filepath.Join(o.kok, "grup")
	_ = os.Mkdir(grupDizin, 0o755)
	_ = os.WriteFile(filepath.Join(grupDizin, "a.tar.gz"), []byte("x"), 0o644)
	_ = os.Chmod(grupDizin, 0o775)
	bos := filepath.Join(o.dizin, "bos.tar.gz")
	_ = os.WriteFile(bos, nil, 0o644)
	buyuk := filepath.Join(o.dizin, "buyuk.tar.gz")
	_ = os.WriteFile(buyuk, make([]byte, 100), 0o644)

	red := map[string]string{
		"boş":                "",
		"göreli":             "yedek/cpmove-u.tar.gz",
		"nokta nokta":        o.dizin + "/../yedek/cpmove-u.tar.gz",
		"çift bölü":          o.dizin + "//cpmove-u.tar.gz",
		"sondaki bölü":       o.dosya + "/",
		"kök dışı":           disDosya,
		"kökün kendisi":      o.kok,
		"NUL":                o.dosya + "\x00x",
		"satır sonu":         o.dosya + "\nx",
		"UTF-8 değil":        o.dizin + "/\xff\xfe.tar.gz",
		"dosya bağı":         bagDosya,
		"dizin bağı":         filepath.Join(bagDizin, "cpmove-u.tar.gz"),
		"kök bağı":           filepath.Join(bagKok, "yedek", "cpmove-u.tar.gz"),
		"dizin dosya değil":  o.dizin,
		"grup yazılır dosya": yazilir,
		"grup yazılır dizin": filepath.Join(grupDizin, "a.tar.gz"),
		"boş dosya":          bos,
		"olmayan":            filepath.Join(o.dizin, "yok.tar.gz"),
		"çok uzun":           o.dizin + "/" + strings.Repeat("a", 1100),
	}
	for ad, yol := range red {
		if _, err := arsivYolDogrula(yol, kokler, 1<<20); !errors.Is(err, errArsivYol) {
			t.Errorf("%s: errArsivYol beklendi, gelen %v", ad, err)
		}
	}
	// Boyut tavanı ayrı hata (413 eşlemesi için).
	if _, err := arsivYolDogrula(buyuk, kokler, 99); !errors.Is(err, errArsivBoyut) {
		t.Errorf("tavan aşımı: errArsivBoyut beklendi, gelen %v", err)
	}
	if _, err := arsivYolDogrula(buyuk, kokler, 100); err != nil {
		t.Errorf("tavana eşit dosya geçmeli: %v", err)
	}
	// Sahip denetimi: dosya/dizin zorunlu sahip değilse ret.
	arsivSahipUID++
	if _, err := arsivYolDogrula(o.dosya, kokler, 1<<20); !errors.Is(err, errArsivYol) {
		t.Errorf("yabancı sahipli yol geçti: %v", err)
	}
	arsivSahipUID--
	// Yasak önekler: izinli kök listesine konsa bile panel/kiracı/sistem dizinleri reddedilir.
	for _, y := range []string{"/etc/passwd", "/home/c_x/yedek.tar.gz", "/var/lib/girginospanel/tasima/arsiv/0/1.arsiv",
		"/opt/girginospanel/bin/x", "/var/vmail/a/b", "/proc/self/environ", "/var/backups/girginospanel/x.tar.gz"} {
		if _, err := arsivYolDogrula(y, []string{"/etc", "/home", "/var/lib/girginospanel", "/opt/girginospanel", "/var/vmail", "/proc", "/var/backups"}, 1<<30); !errors.Is(err, errArsivYol) {
			t.Errorf("%s yasak önek olarak reddedilmedi: %v", y, err)
		}
	}
}

func TestArsivAltinda(t *testing.T) {
	for _, c := range []struct {
		p, k string
		ok   bool
	}{
		{"/root/a", "/root", true}, {"/root", "/root", true}, {"/rootfs/a", "/root", false},
		{"/home/x", "/home", true}, {"/homex", "/home", false}, {"/a/b", "/a/", true},
	} {
		if got := altinda(c.p, c.k); got != c.ok {
			t.Errorf("altinda(%q,%q)=%v", c.p, c.k, got)
		}
	}
}

// Kural: hedefte boş alan < arşiv × 2,5 → taşıma başlamadan reddedilir (sınır değeri dahil).
func TestArsivDiskOnkontrol_SinirDegeri(t *testing.T) {
	eski := diskBosAlan
	t.Cleanup(func() { diskBosAlan = eski })
	var sorulan string
	for _, c := range []struct {
		arsiv, bos int64
		ok         bool
	}{
		{1000, 2499, false}, {1000, 2500, true}, {1000, 2501, true},
		{1001, 2501, false}, {1001, 2502, true}, // 2,5 × 1001 = 2502,5 → alt tam sayı 2502
		{500 << 30, 1250<<30 - 1, false}, {500 << 30, 1250 << 30, true},
	} {
		diskBosAlan = func(y string) (int64, error) { sorulan = y; return c.bos, nil }
		err := arsivDiskOnkontrol(c.arsiv)
		if (err == nil) != c.ok || (!c.ok && !errors.Is(err, errArsivDisk)) {
			t.Errorf("arşiv=%d boş=%d: ok=%v err=%v", c.arsiv, c.bos, c.ok, err)
		}
		if gerekliBosBayt(c.arsiv) > c.bos != !c.ok {
			t.Errorf("gerekliBosBayt tutarsız: %d", gerekliBosBayt(c.arsiv))
		}
	}
	if sorulan != evKoku {
		t.Errorf("disk kiracı evlerinin dosya sisteminde ölçülmeli: %q", sorulan)
	}
	diskBosAlan = func(string) (int64, error) { return 0, errors.New("statfs") }
	if err := arsivDiskOnkontrol(1); err == nil || errors.Is(err, errArsivDisk) {
		t.Errorf("ölçüm hatası 'yetersiz' gibi sunulmamalı: %v", err)
	}
}

// Kural: yükleme biçimi imzayla (gzip/bzip2/xz/zstd/ustar) tanınır; uzantıya güvenilmez.
func TestArsivSihirGecerli(t *testing.T) {
	tar := make([]byte, 512)
	copy(tar[257:], "ustar")
	ok := [][]byte{{0x1f, 0x8b, 8}, []byte("BZh9"), []byte("\xfd7zXZ\x00x"), []byte("\x28\xb5\x2f\xfdx"), tar}
	for i, b := range ok {
		if !arsivSihirGecerli(b) {
			t.Errorf("geçerli imza %d reddedildi", i)
		}
	}
	for i, b := range [][]byte{nil, {0x1f}, []byte("PK\x03\x04zip"), []byte("#!/bin/sh\n"), []byte("BZ"), make([]byte, 512), []byte("\xfd7zXZ")} {
		if arsivSihirGecerli(b) {
			t.Errorf("geçersiz içerik %d kabul edildi", i)
		}
	}
}

func TestYuklemeAdiGecerli(t *testing.T) {
	for _, ad := range []string{"cpmove-u.tar.gz", "yedek (1).tgz", "ç.tar.gz"} {
		if !yuklemeAdiGecerli(ad) {
			t.Errorf("%q reddedildi", ad)
		}
	}
	for _, ad := range []string{"", "a/b", `a\b`, "a\nb", "a\x00b", strings.Repeat("a", 191), "a\xffb"} {
		if yuklemeAdiGecerli(ad) {
			t.Errorf("%q kabul edildi", ad)
		}
	}
}

// Kural: keşif listesi — ana/ek/alt ayrı site, park alanı taşınmaz (nota düşer), veritabanı
// YALNIZ ana alana, reddedilen döküm listede kalır ama nota yazılır; belge kökü yoksa not.
func TestArsivHesaplari(t *testing.T) {
	m := &arsiv.Manifest{
		Tur: arsiv.TurCPanel, Kullanici: "u", AnaAlan: "ornek.com",
		Alanlar: []arsiv.Alan{
			{Ad: "ornek.com", Tur: "ana", BelgeKoku: "public_html"},
			{Ad: "ek.net", Tur: "ek", BelgeKoku: "ek"},
			{Ad: "blog.ornek.com", Tur: "alt"},
			{Ad: "park.org", Tur: "park"},
			{Ad: "-gecersiz-", Tur: "ek"},
		},
		Veritabanlari: []arsiv.Veritabani{{Ad: "u_wp"}, {Ad: "u_kotu", Reddedildi: arsiv.KuralDosya}, {Ad: "bad name"}},
	}
	l := arsivHesaplari(m)
	if len(l) != 3 {
		t.Fatalf("3 site beklendi (park ve geçersiz hariç): %+v", l)
	}
	if l[0].AlanAdi != "ornek.com" || strings.Join(l[0].DBler, ",") != "u_wp,u_kotu" || l[0].WebRoot != "public_html" || l[0].KaynakHesap != "u" {
		t.Errorf("ana alan: %+v", l[0])
	}
	if !strings.Contains(l[0].Not, "park.org") || !strings.Contains(l[0].Not, "u_kotu") {
		t.Errorf("ana alan notu park/reddi içermeli: %q", l[0].Not)
	}
	if len(l[1].DBler) != 0 || len(l[2].DBler) != 0 {
		t.Errorf("veritabanı yalnız ana alanda: %+v %+v", l[1], l[2])
	}
	if !strings.Contains(l[2].Not, "belge kökü yedekte yok") || !strings.Contains(l[1].Not, "ek (addon)") {
		t.Errorf("notlar: %q / %q", l[1].Not, l[2].Not)
	}
}

func TestArsivTuruUyar(t *testing.T) {
	cp, da := &arsiv.Manifest{Tur: arsiv.TurCPanel}, &arsiv.Manifest{Tur: arsiv.TurDirectAdmin}
	if arsivTuruUyar("arsiv-cpanel", cp) != nil || arsivTuruUyar("arsiv-directadmin", da) != nil {
		t.Fatal("doğru tür reddedildi")
	}
	if arsivTuruUyar("arsiv-cpanel", da) == nil || arsivTuruUyar("arsiv-directadmin", cp) == nil {
		t.Fatal("yanlış tür kabul edildi")
	}
}
