package arsiv

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const (
	testHash = "$6$GIZLIHASH$abcdefghijklmnop"
	temizDB  = "CREATE TABLE `t` (`a` int);\nINSERT INTO `t` VALUES (1);\n-- Dump completed on 2026-10-06\n"
)

func dizin(ad string) uyeT { return uyeT{ad: ad, tur: tar.TypeDir} }

// cpmoveUyeler: meşru, küçük bir cpmove arşivi (alan: ornek.com, ek.net, blog.ornek.com, park.org).
func cpmoveUyeler() []uyeT {
	k := "cpmove-u/"
	return []uyeT{
		dizin("./"), dizin(k), dizin(k + "cp/"), dizin(k + "userdata/"), dizin(k + "mysql/"),
		dizin(k + "homedir/"), dizin(k + "homedir/public_html/"), dizin(k + "homedir/ek/"),
		dizin(k + "homedir/blog/"), dizin(k + "homedir/mail/"), dizin(k + "homedir/mail/ornek.com/"),
		dizin(k + "homedir/mail/ornek.com/info/"), dizin(k + "homedir/etc/"), dizin(k + "homedir/etc/ornek.com/"),
		{ad: k + "cp/u", veri: "USER=u\nDNS=ornek.com\nOWNER=baska-bayi\nPLAN=limitsiz\nIP=1.2.3.4\n"},
		{ad: k + "userdata/main", veri: "---\naddon_domains:\n  ek.net: ek.net.ornek.com\ncp_php_magic_include_path.conf: &x\nmain_domain: ornek.com\nparked_domains:\n- park.org\nsub_domains:\n- blog.ornek.com\n"},
		{ad: k + "userdata/ornek.com", veri: "documentroot: /home/u/public_html\nphpversion: ea-php82\n"},
		{ad: k + "userdata/ek.net.ornek.com", veri: "documentroot: /home/u/ek\n"},
		{ad: k + "userdata/blog.ornek.com", veri: "documentroot: /home/u/blog\n"},
		{ad: k + "mysql/u_wp.sql", veri: temizDB},
		{ad: k + "mysql/u_x.sql", veri: temizDB},
		{ad: k + "mysql/mysql.sql", veri: "CREATE USER x;\n"},
		{ad: k + "mysql.sql", veri: "GRANT USAGE ON *.* TO 'u_wp'@'localhost' IDENTIFIED BY PASSWORD '" + testHash + "';\n" +
			"GRANT ALL PRIVILEGES ON `u\\_wp`.* TO 'u_wp'@'localhost';\n" +
			"GRANT ALL PRIVILEGES ON `u\\_x`.* TO 'u_x'@'localhost';\n"},
		{ad: k + "dnszones/ornek.com.db", veri: "; zone\n$TTL 14400\nornek.com. IN SOA ns1. h. (1 2 3 4 5)\n@ IN A 1.2.3.4\nwww IN CNAME @\n"},
		{ad: k + "cron/u", veri: "MAILTO=x\n# yorum\n*/5 * * * * php /home/u/public_html/cron.php\n"},
		{ad: k + "homedir/public_html/index.php", veri: "<?php echo 1;"},
		{ad: k + "homedir/ek/a.txt", veri: "aaaa"},
		{ad: k + "homedir/mail/ornek.com/info/cur-1", veri: "mesaj"},
		{ad: k + "homedir/etc/ornek.com/passwd", veri: "info:" + testHash + ":1:2::/home/u/mail:\n"},
	}
}

func okuT(t *testing.T, uyeler []uyeT, s Sinirlar, o Secenek) (*Manifest, error) {
	t.Helper()
	o.Sinirlar = s
	return Oku(context.Background(), dosyala(t, gzUret(t, tarUret(t, uyeler...))), o)
}

func degistir(uyeler []uyeT, ad string, f func(u *uyeT)) []uyeT {
	out := append([]uyeT{}, uyeler...)
	for i := range out {
		if out[i].ad == ad {
			f(&out[i])
		}
	}
	return out
}

func ekle(uyeler []uyeT, ek ...uyeT) []uyeT { return append(append([]uyeT{}, uyeler...), ek...) }

func uyariVar(m *Manifest, parca string) bool {
	for _, u := range m.Uyarilar {
		if strings.Contains(u, parca) {
			return true
		}
	}
	return false
}

func TestOku_CPanel(t *testing.T) {
	m, err := okuT(t, cpmoveUyeler(), Sinirlar{}, Secenek{})
	if err != nil {
		t.Fatal(err)
	}
	if m.Tur != TurCPanel || m.Kullanici != "u" || m.AnaAlan != "ornek.com" {
		t.Fatalf("başlık: %+v", m)
	}
	var alanlar []string
	roots := map[string]string{}
	for _, a := range m.Alanlar {
		alanlar = append(alanlar, a.Ad+":"+a.Tur)
		roots[a.Ad] = a.BelgeKoku
	}
	if got := strings.Join(alanlar, ","); got != "blog.ornek.com:alt,ek.net:ek,ornek.com:ana,park.org:park" {
		t.Fatalf("alanlar: %s", got)
	}
	if roots["ornek.com"] != "public_html" || roots["ek.net"] != "ek" || roots["blog.ornek.com"] != "blog" {
		t.Fatalf("belge kökleri: %v", roots)
	}
	if len(m.Veritabanlari) != 2 || m.Veritabanlari[0].Ad != "u_wp" || m.Veritabanlari[0].Rapor == nil ||
		!m.Veritabanlari[0].Rapor.Tamam || m.Veritabanlari[0].Reddedildi != "" {
		t.Fatalf("veritabanları: %+v", m.Veritabanlari)
	}
	if got := strings.Join(m.DBKullanicilari, ","); got != "u_wp,u_x" || len(m.Veritabanlari[0].Kullanicilar) != 1 {
		t.Fatalf("db kullanıcıları: %v / %+v", m.DBKullanicilari, m.Veritabanlari[0])
	}
	if len(m.Posta) != 1 || m.Posta[0] != (PostaHesabi{Alan: "ornek.com", Yerel: "info", MaildirVar: true, Bayt: 5}) {
		t.Fatalf("posta: %+v", m.Posta)
	}
	if len(m.DNS) != 1 || m.DNS[0].Alan != "ornek.com" || m.DNS[0].Satir != 4 {
		t.Fatalf("dns: %+v", m.DNS)
	}
	// Ham satırlar, ortam satırı (MAILTO) dahil, sırayla; "elle gözden geçirin" uyarısı artık yok.
	if len(m.Cron) != 2 || m.Cron[0] != "MAILTO=x" || !strings.HasPrefix(m.Cron[1], "*/5 ") || uyariVar(m, "otomatik kurulmaz") {
		t.Fatalf("cron: %+v", m.Cron)
	}
	if m.DosyaSayisi != 4 || m.ToplamBayt != int64(len("<?php echo 1;")+4+5+len("info:"+testHash+":1:2::/home/u/mail:\n")) || len(m.Tasinmayacak) == 0 {
		t.Fatalf("sayımlar: dosya=%d bayt=%d", m.DosyaSayisi, m.ToplamBayt)
	}
}

// Parola özeti ve sahiplik iddiaları manifeste HİÇ girmez (JSON'da aranır).
func TestOku_SirVeSahiplikIddiasiSizmaz(t *testing.T) {
	m, err := okuT(t, cpmoveUyeler(), Sinirlar{}, Secenek{})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(m)
	for _, yasak := range []string{"GIZLIHASH", "baska-bayi", "limitsiz", "1.2.3.4", "IDENTIFIED"} {
		if strings.Contains(string(b), yasak) {
			t.Fatalf("manifest %q sızdırıyor: %s", yasak, b)
		}
	}
	if !uyariVar(m, "OWNER iddiası yok sayıldı") || !uyariVar(m, "PLAN iddiası yok sayıldı") {
		t.Fatalf("iddia uyarıları yok: %v", m.Uyarilar)
	}
}

func TestOku_PostaKutuAdiKacisi(t *testing.T) {
	passwd := "info:h\n../../etc:h\n..:h\na/b:h\n.hidden:h\nx..y:h\nok.user+tag:h\nUPPER:h\nupper:h\n:h\n"
	u := degistir(cpmoveUyeler(), "cpmove-u/homedir/etc/ornek.com/passwd", func(u *uyeT) { u.veri = passwd })
	m, err := okuT(t, u, Sinirlar{}, Secenek{})
	if err != nil {
		t.Fatal(err)
	}
	var adlar []string
	for _, p := range m.Posta {
		adlar = append(adlar, p.Yerel)
	}
	if got := strings.Join(adlar, ","); got != "UPPER,info,ok.user+tag" {
		t.Fatalf("kabul edilen kutular: %s", got)
	}
	if !uyariVar(m, "geçersiz kutu adı") || !uyariVar(m, "yinelenen kutu adı") {
		t.Fatalf("uyarılar: %v", m.Uyarilar)
	}
	for _, p := range m.Posta {
		if !PostaYerelGecerli(p.Yerel) {
			t.Fatalf("geçersiz ad sızdı: %q", p.Yerel)
		}
	}
}

func TestPostaYerelGecerli(t *testing.T) {
	for _, s := range []string{"..", "../x", "a/b", `a\b`, "", ".x", "a..b", "x.", "a b", "a\x00b", strings.Repeat("a", 65)} {
		if PostaYerelGecerli(s) {
			t.Errorf("%q geçerli sayılmamalı", s)
		}
	}
	for _, s := range []string{"info", "a.b", "a-b_c+d", "A1"} {
		if !PostaYerelGecerli(s) {
			t.Errorf("%q geçerli olmalı", s)
		}
	}
}

func TestAlanAdiGecerli(t *testing.T) {
	for _, s := range []string{"", "a", "ornek", "1.2.3.4", "*.ornek.com", "Ornek.com", "ornek..com", "-a.com", "a-.com",
		"a.com/x", "a.com\n", "a b.com", "ornek.com.", strings.Repeat("a", 64) + ".com", "a_b.com", "localhost"} {
		if AlanAdiGecerli(s) {
			t.Errorf("%q geçerli sayılmamalı", s)
		}
	}
	for _, s := range []string{"ornek.com", "a-b.co.uk", "xn--bcher-kva.de", "a.b.c.d.com"} {
		if !AlanAdiGecerli(s) {
			t.Errorf("%q geçerli olmalı", s)
		}
	}
}

// Başka bir alan adı için bölge/kutu iddiası (hesabın listesinde olmayan) atlanır.
func TestOku_BaskasininAlanAdiAtlanir(t *testing.T) {
	k := "cpmove-u/"
	u := ekle(cpmoveUyeler(),
		dizin(k+"homedir/etc/kurban.com/"),
		uyeT{ad: k + "homedir/etc/kurban.com/passwd", veri: "admin:h\n"},
		uyeT{ad: k + "dnszones/kurban.com.db", veri: "@ IN A 6.6.6.6\n"},
		uyeT{ad: k + "dnszones/GECERSIZ_.db", veri: "x\n"},
	)
	m, err := okuT(t, u, Sinirlar{}, Secenek{})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range m.Posta {
		if p.Alan == "kurban.com" {
			t.Fatalf("başkasının alanı için kutu: %+v", p)
		}
	}
	for _, d := range m.DNS {
		if d.Alan != "ornek.com" {
			t.Fatalf("başkasının alanı için bölge: %+v", d)
		}
	}
	if !uyariVar(m, "kurban.com") {
		t.Fatalf("uyarı yok: %v", m.Uyarilar)
	}
}

func TestOku_GecersizAlanVeBelgeKoku(t *testing.T) {
	main := "main_domain: ornek.com\naddon_domains:\n  ../../etc: x.ornek.com\n  ek.net: ek.net.ornek.com\n  yabanci.com: yabanci.com\nsub_domains:\n- a b.ornek.com\n- 1.2.3.4\nparked_domains: []\n"
	u := degistir(cpmoveUyeler(), "cpmove-u/userdata/main", func(u *uyeT) { u.veri = main })
	u = ekle(u,
		uyeT{ad: "cpmove-u/userdata/yabanci.com", veri: "documentroot: /home/baskasi/public_html\n"},
		uyeT{ad: "cpmove-u/userdata/ek.net.ornek.com2", veri: "documentroot: /home/u/ek/../../etc\n"},
	)
	m, err := okuT(t, u, Sinirlar{}, Secenek{})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range m.Alanlar {
		if !AlanAdiGecerli(a.Ad) {
			t.Fatalf("geçersiz alan sızdı: %q", a.Ad)
		}
		if a.Ad == "yabanci.com" && a.BelgeKoku != "" {
			t.Fatalf("ev dışı belge kökü kabul edildi: %+v", a)
		}
	}
	if !uyariVar(m, "geçersiz alan adı atlandı") || !uyariVar(m, "ev dizini dışında") {
		t.Fatalf("uyarılar: %v", m.Uyarilar)
	}
}

func TestOku_BelgeKokuKacisi(t *testing.T) {
	u := degistir(cpmoveUyeler(), "cpmove-u/userdata/ek.net.ornek.com", func(u *uyeT) {
		u.veri = "documentroot: /home/u/ek/../../etc\n"
	})
	m, err := okuT(t, u, Sinirlar{}, Secenek{})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range m.Alanlar {
		if strings.Contains(a.BelgeKoku, "..") {
			t.Fatalf("'..' içeren belge kökü: %+v", a)
		}
	}
	if !uyariVar(m, "geçersiz belge kökü") {
		t.Fatalf("'..' belge kökü açıkça reddedilmeli: %v", m.Uyarilar)
	}
}

func TestOku_YAMLCapaVeAkisStiliReddedilir(t *testing.T) {
	for ad, main := range map[string]string{
		"capa":         "main_domain: ornek.com\nsub_domains:\n- &a blog.ornek.com\n",
		"takma-ad":     "main_domain: ornek.com\nsub_domains:\n- *a\n",
		"akis":         "main_domain: ornek.com\nsub_domains: [a.ornek.com]\n",
		"ana-capa":     "main_domain: &x ornek.com\n",
		"yok":          "",
		"ana-alan-yok": "sub_domains: []\n",
		"ana-gecersiz": "main_domain: ../../x\n",
	} {
		t.Run(ad, func(t *testing.T) {
			u := degistir(cpmoveUyeler(), "cpmove-u/userdata/main", func(u *uyeT) { u.veri = main })
			if _, err := okuT(t, u, Sinirlar{}, Secenek{}); !errors.Is(err, ErrHesap) {
				t.Fatalf("ErrHesap beklendi: %v", err)
			}
		})
	}
}

func TestOku_HesapAdiDogrulanir(t *testing.T) {
	for _, ad := range []string{"../u", "U", "a b", "-x", ""} {
		u := degistir(cpmoveUyeler(), "cpmove-u/cp/u", func(u *uyeT) { u.ad = "cpmove-u/cp/" + ad })
		if ad == "" || strings.Contains(ad, "/") {
			continue
		}
		if _, err := okuT(t, u, Sinirlar{}, Secenek{}); !errors.Is(err, ErrHesap) {
			t.Errorf("%q: ErrHesap beklendi: %v", ad, err)
		}
	}
	// ikinci cp/ kaydı
	u := ekle(cpmoveUyeler(), uyeT{ad: "cpmove-u/cp/v", veri: "USER=v\n"})
	if _, err := okuT(t, u, Sinirlar{}, Secenek{}); !errors.Is(err, ErrHesap) {
		t.Fatalf("çift cp kaydı: %v", err)
	}
}

func TestOku_TurTespiti(t *testing.T) {
	if _, err := okuT(t, []uyeT{dizin("x/"), {ad: "x/a.txt", veri: "a"}}, Sinirlar{}, Secenek{}); !errors.Is(err, ErrTur) {
		t.Fatalf("tanınmayan arşiv: %v", err)
	}
	// iki farklı kök → belirsiz
	u := ekle(cpmoveUyeler(), uyeT{ad: "cpmove-v/userdata/main", veri: "main_domain: v.com\n"})
	if _, err := okuT(t, u, Sinirlar{}, Secenek{}); !errors.Is(err, ErrHesap) {
		t.Fatalf("çok kök: %v", err)
	}
	// cPanel + DA işaretleri bir arada → belirsiz
	u = ekle(cpmoveUyeler(), uyeT{ad: "backup/user.conf", veri: "username=u\ndomain=ornek.com\n"})
	if _, err := okuT(t, u, Sinirlar{}, Secenek{}); !errors.Is(err, ErrTur) {
		t.Fatalf("karışık: %v", err)
	}
}

func TestOku_DBKurallari(t *testing.T) {
	k := "cpmove-u/mysql/"
	u := ekle(cpmoveUyeler(),
		uyeT{ad: k + "mysql.sql", veri: temizDB}, // sistem DB adı: atlanır
		uyeT{ad: k + "information_schema.sql", veri: temizDB},
		uyeT{ad: k + "u_kotu.sql", veri: "GRANT ALL ON *.* TO 'x'@'%';\n"},
		uyeT{ad: k + "u_bag.sql", tur: tar.TypeSymlink, hedef: "../mysql.sql"},
		uyeT{ad: k + "ge cersiz.sql", veri: temizDB},
		uyeT{ad: k + "u_kesik.sql", veri: "CREATE TABLE t (a int);\n"},
	)
	u = degistir(u, k+"mysql.sql", func(u *uyeT) {})
	// ilk "mysql/mysql.sql" iyiUyeler'de yok: cpmoveUyeler mysql/mysql.sql içeriyor → çift giriş olmasın
	var temiz []uyeT
	say := 0
	for _, x := range u {
		if x.ad == k+"mysql.sql" {
			say++
			if say == 2 {
				continue
			}
		}
		temiz = append(temiz, x)
	}
	m, err := okuT(t, temiz, Sinirlar{}, Secenek{})
	if err != nil {
		t.Fatal(err)
	}
	durum := map[string]string{}
	for _, v := range m.Veritabanlari {
		durum[v.Ad] = v.Reddedildi
	}
	if _, var_ := durum["mysql"]; var_ {
		t.Fatalf("sistem veritabanı manifestte: %v", durum)
	}
	if _, var_ := durum["information_schema"]; var_ {
		t.Fatalf("sistem veritabanı manifestte: %v", durum)
	}
	if durum["u_kotu"] != KuralYetki || durum["u_wp"] != "" {
		t.Fatalf("durum: %v", durum)
	}
	for _, ad := range []string{"u_bag", "ge cersiz"} {
		if _, var_ := durum[ad]; var_ {
			t.Fatalf("%q kabul edildi", ad)
		}
	}
	if !uyariVar(m, "reddedildi: "+KuralYetki) || !uyariVar(m, "sistem veritabanı") || !uyariVar(m, "bitiş damgası") {
		t.Fatalf("uyarılar: %v", m.Uyarilar)
	}
}

func TestOku_SQLAtla(t *testing.T) {
	k := "cpmove-u/mysql/"
	u := ekle(cpmoveUyeler(), uyeT{ad: k + "u_kotu.sql", veri: "GRANT ALL ON *.* TO x;\n"})
	m, err := okuT(t, u, Sinirlar{}, Secenek{SQLAtla: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range m.Veritabanlari {
		if v.Rapor != nil || v.Reddedildi != "" {
			t.Fatalf("SQLAtla'da tarama yapıldı: %+v", v)
		}
	}
}

func TestOku_DumpSiniri(t *testing.T) {
	m, err := okuT(t, cpmoveUyeler(), Sinirlar{}, Secenek{DumpSinir: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range m.Veritabanlari {
		if v.Reddedildi != KuralBoyut {
			t.Fatalf("döküm sınırı uygulanmadı: %+v", v)
		}
	}
}

// Çıkarmada atlanacak üye türleri manifestte sayılır ve uyarılır.
func TestOku_BaglarVeAygitlarSayilir(t *testing.T) {
	h := "cpmove-u/homedir/public_html/"
	u := ekle(cpmoveUyeler(),
		uyeT{ad: h + "dis", tur: tar.TypeSymlink, hedef: "/etc"},
		uyeT{ad: h + "fifo", tur: tar.TypeFifo},
		uyeT{ad: h + "cihaz", tur: tar.TypeChar},
		uyeT{ad: h + "sabit", tur: tar.TypeLink, hedef: "cpmove-u/homedir/public_html/index.php"},
	)
	m, err := okuT(t, u, Sinirlar{}, Secenek{})
	if err != nil {
		t.Fatal(err)
	}
	if m.Baglar != 1 || m.Aygitlar != 2 || m.SabitBaglar != 1 || !uyariVar(m, "aygıt/FIFO") {
		t.Fatalf("sayımlar: bag=%d aygit=%d sabit=%d %v", m.Baglar, m.Aygitlar, m.SabitBaglar, m.Uyarilar)
	}
}

// Arşiv düzeyindeki kurallar Oku'dan da geçer (ayrıntılı testler ac_test.go'da).
func TestOku_DusmanArsivlerReddedilir(t *testing.T) {
	iyi := cpmoveUyeler()
	gz := gzUret(t, tarUret(t, iyi...))
	cases := map[string]struct {
		b   []byte
		s   Sinirlar
		bek error
	}{
		"nokta-nokta":  {gzUret(t, tarUret(t, ekle(iyi, uyeT{ad: "cpmove-u/../../etc/x", veri: "x"})...)), Sinirlar{}, ErrUyeAdi},
		"mutlak-yol":   {gzUret(t, tarUret(t, ekle(iyi, uyeT{ad: "/etc/x", veri: "x"})...)), Sinirlar{}, ErrUyeAdi},
		"ters-bolu":    {gzUret(t, tarUret(t, ekle(iyi, uyeT{ad: `cpmove-u\..\x`, veri: "x"})...)), Sinirlar{}, ErrUyeAdi},
		"cift-giris":   {gzUret(t, tarUret(t, ekle(iyi, uyeT{ad: "cpmove-u/cp/u", veri: "USER=u\n"})...)), Sinirlar{}, ErrTekrar},
		"uye-sayisi":   {gz, Sinirlar{UyeSayisi: 5}, ErrSinir},
		"toplam-boyut": {gz, Sinirlar{ToplamBayt: 10}, ErrSinir},
		"bomba":        {gzUret(t, tarUret(t, ekle(iyi, uyeT{ad: "cpmove-u/homedir/b", boyut: 8 << 20})...)), Sinirlar{BombaOrani: 20, BombaEsik: 1 << 20}, ErrBomba},
		"bozuk-gzip":   {gz[:len(gz)/2], Sinirlar{}, ErrBicim},
		"cop":          {[]byte("bu bir arsiv degil"), Sinirlar{}, ErrBicim},
		"ust-veri":     {gz, Sinirlar{UstVeriBayt: 10}, ErrUstVeri},
	}
	for ad, c := range cases {
		t.Run(ad, func(t *testing.T) {
			_, err := Oku(context.Background(), dosyala(t, c.b), Secenek{Sinirlar: c.s})
			if !errors.Is(err, c.bek) {
				t.Fatalf("%v beklendi, gelen %v", c.bek, err)
			}
		})
	}
}

func daUyeler() []uyeT {
	return []uyeT{
		dizin("./"), dizin("backup/"), dizin("domains/"), dizin("domains/ornek.com/"), dizin("domains/ornek.com/public_html/"),
		dizin("domains/ek.net/"), dizin("imap/"), dizin("imap/ornek.com/"), dizin("imap/ornek.com/info/"),
		dizin("backup/ornek.com/"), dizin("backup/ornek.com/email/"),
		{ad: "backup/user.conf", veri: "username=da1\ndomain=ornek.com\ncreator=baska-bayi\npackage=gold\nip=1.2.3.4\n"},
		{ad: "backup/da1_wp.sql", veri: temizDB},
		{ad: "backup/da1_wp.conf", veri: "accesshosts=0=localhost\nda1_wpu=alter_priv=Y&create_priv=Y&passwd=" + testHash + "\n"},
		{ad: "backup/ornek.com/email/passwd", veri: "info:" + testHash + "\n../x:h\n"},
		{ad: "backup/crontab.conf", veri: "0 3 * * * /usr/bin/php /home/da1/x.php\n"},
		{ad: "domains/ornek.com/public_html/index.html", veri: "<html>"},
		{ad: "imap/ornek.com/info/Maildir-cur-1", veri: "msg!"},
	}
}

func TestOku_DirectAdmin(t *testing.T) {
	m, err := okuT(t, daUyeler(), Sinirlar{}, Secenek{})
	if err != nil {
		t.Fatal(err)
	}
	if m.Tur != TurDirectAdmin || m.Kullanici != "da1" || m.AnaAlan != "ornek.com" {
		t.Fatalf("başlık: %+v", m)
	}
	if len(m.Alanlar) != 2 || m.Alanlar[1].Ad != "ornek.com" || m.Alanlar[1].Tur != "ana" ||
		m.Alanlar[1].BelgeKoku != "domains/ornek.com/public_html" {
		t.Fatalf("alanlar: %+v", m.Alanlar)
	}
	if len(m.Veritabanlari) != 1 || m.Veritabanlari[0].Rapor == nil || strings.Join(m.DBKullanicilari, ",") != "da1_wpu" {
		t.Fatalf("db: %+v %v", m.Veritabanlari, m.DBKullanicilari)
	}
	if len(m.Posta) != 1 || m.Posta[0] != (PostaHesabi{Alan: "ornek.com", Yerel: "info", MaildirVar: true, Bayt: 4}) {
		t.Fatalf("posta: %+v", m.Posta)
	}
	if len(m.Cron) != 1 || m.DosyaSayisi != 1 {
		t.Fatalf("cron/dosya: %+v %d", m.Cron, m.DosyaSayisi)
	}
	b, _ := json.Marshal(m)
	for _, yasak := range []string{"GIZLIHASH", "baska-bayi", "gold", "1.2.3.4", "alter_priv"} {
		if strings.Contains(string(b), yasak) {
			t.Fatalf("manifest %q sızdırıyor: %s", yasak, b)
		}
	}
	if !uyariVar(m, "creator iddiası yok sayıldı") || !uyariVar(m, "geçersiz kutu adı") {
		t.Fatalf("uyarılar: %v", m.Uyarilar)
	}
}

func TestOku_DirectAdminKurallari(t *testing.T) {
	u := degistir(daUyeler(), "backup/user.conf", func(u *uyeT) { u.veri = "username=../x\ndomain=ornek.com\n" })
	if _, err := okuT(t, u, Sinirlar{}, Secenek{}); !errors.Is(err, ErrHesap) {
		t.Fatalf("hesap adı: %v", err)
	}
	u = degistir(daUyeler(), "backup/user.conf", func(u *uyeT) { u.veri = "username=da1\ndomain=../../x\n" })
	if _, err := okuT(t, u, Sinirlar{}, Secenek{}); !errors.Is(err, ErrHesap) {
		t.Fatalf("ana alan: %v", err)
	}
	// başkasının alan adı için posta; geçersiz domains/ girdisi; kötü döküm
	u = ekle(daUyeler(),
		dizin("backup/kurban.com/"), dizin("backup/kurban.com/email/"),
		uyeT{ad: "backup/kurban.com/email/passwd", veri: "admin:h\n"},
		dizin("domains/Buyuk.COM/"),
		uyeT{ad: "backup/da1_kotu.sql", veri: "CREATE USER x;\n"},
		uyeT{ad: "backup/mysql.sql", veri: temizDB},
	)
	m, err := okuT(t, u, Sinirlar{}, Secenek{})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range m.Posta {
		if p.Alan != "ornek.com" {
			t.Fatalf("başkasının alanı: %+v", p)
		}
	}
	var kotu string
	for _, v := range m.Veritabanlari {
		if v.Ad == "da1_kotu" {
			kotu = v.Reddedildi
		}
		if v.Ad == "mysql" {
			t.Fatal("sistem veritabanı kabul edildi")
		}
	}
	if kotu != KuralYonetim || !uyariVar(m, "geçersiz alan girdisi") {
		t.Fatalf("kötü=%q uyarılar=%v", kotu, m.Uyarilar)
	}
}
