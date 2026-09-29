package main

// clamav modu (kural_motoru=0) regresyon testleri: Go yürüyüşü + --file-list +
// doğrulanmış çıktı ayrıştırma.
//
// Testler CANLI SİSTEME DOKUNMAZ: clamscan ÇALIŞTIRILMAZ (clamCalistir yerine Go
// içinde sahte tarayıcı), dosya listesi ve "home" t.TempDir altında, karantina
// sahte uid ile t.TempDir içinde yapılır; DB nil (db.go yazıcıları nil'de döner).

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"girginospanel/internal/avayar"
)

// sahteClam — clamscan'in liste/argüman ve çıktı biçimini exec'siz taklit eder:
// --file-list'i satır satır okur (satır = yol), konumsal argümanları yol sayar;
// Lstat ile düzenli olup içinde işaret bulunan dosya için yolu KAÇIŞLAMADAN
// "<yol>: Test.Imza FOUND" basar; sonda clamscan özetini yazar.
type sahteClam struct {
	isaret   string
	liste    []string // --file-list'ten okunan satırlar
	konumsal []string
	gecersiz []string // tarama anında düzenli dosya olmayan girdiler (bölünmüş satır vb.)
	kosu     int
	ekCikti  string // koşu çıktısının başına eklenen ham metin (kökeni bilinmeyen satır)
	ozetYok  bool   // çıktı yarıda kesilmiş gibi: özet yok
	sonra    func() // çıktı üretildikten sonra (tarama ile kayıt arası)
}

func (s *sahteClam) calistir(args []string) []byte {
	s.kosu++
	var yollar []string
	for _, a := range args {
		if v, ok := strings.CutPrefix(a, "--file-list="); ok {
			f, err := os.Open(v)
			if err != nil {
				panic(err)
			}
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 64<<10), 64<<10)
			for sc.Scan() {
				s.liste = append(s.liste, sc.Text())
				yollar = append(yollar, sc.Text())
			}
			f.Close()
			continue
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		s.konumsal = append(s.konumsal, a)
		yollar = append(yollar, a)
	}
	var b strings.Builder
	b.WriteString(s.ekCikti)
	taranan, enfekte := 0, 0
	for _, y := range yollar {
		fi, err := os.Lstat(y)
		if err != nil || !fi.Mode().IsRegular() {
			s.gecersiz = append(s.gecersiz, y)
			fmt.Fprintf(&b, "%s: No such file or directory\n", y)
			continue
		}
		taranan++
		icerik, _ := os.ReadFile(y)
		if strings.Contains(string(icerik), s.isaret) {
			enfekte++
			fmt.Fprintf(&b, "%s: Test.Imza FOUND\n", y)
		}
	}
	if !s.ozetYok {
		fmt.Fprintf(&b, "\n----------- SCAN SUMMARY -----------\nScanned files: %d\nInfected files: %d\n", taranan, enfekte)
	}
	if s.sonra != nil {
		s.sonra()
	}
	return []byte(b.String())
}

type clamOrtam struct {
	kok         string   // sahte /home
	saldirgan   string   // <kok>/c_saldirgan/public_html
	kurbanDosya string   // <kok>/c_kurban/public_html/index.php (meşru, temiz)
	kar         []string // oto-karantinaya gönderilen yollar
	sc          *sahteClam
}

func yeniClamOrtam(t *testing.T) (*ajan, *clamOrtam) {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	o := &clamOrtam{kok: filepath.Join(d, "home"), sc: &sahteClam{isaret: zararliIcerik}}
	o.saldirgan = filepath.Join(o.kok, "c_saldirgan", "public_html")
	kurbanPub := filepath.Join(o.kok, "c_kurban", "public_html")
	for _, p := range []string{o.saldirgan, kurbanPub} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	o.kurbanDosya = filepath.Join(kurbanPub, "index.php")
	dosyaOlustur(t, o.kurbanDosya, "kurbanin-sitesi", 0o644)

	eskiDizin, eskiCalistir := clamListeDizini, clamCalistir
	clamListeDizini = filepath.Join(d, "av") // kökün DIŞINDA: liste dosyası yürünmez
	clamCalistir = o.sc.calistir
	t.Cleanup(func() { clamListeDizini, clamCalistir = eskiDizin, eskiCalistir })

	a := &ajan{ayar: avayar.Ayarlar{OtoKarantina: true}}
	a.karantina = func(y string) (string, error) {
		o.kar = append(o.kar, y)
		return karantinaAlEv(o.kok, y, buUID, testAn)
	}
	return a, o
}

func bulguYollari(a *ajan) []string {
	var y []string
	for _, b := range a.bulgular {
		y = append(y, b.Dosya)
	}
	return y
}

// Gözden geçirmedeki senaryo: saldırgan kendi evinde, adı satır sonu ve
// "<kurbanın dosyası>: <imza> FOUND" içeren bir dizine tespit edilecek dosya koyar.
// Beklenen: bu ad clamscan'e HİÇ verilmez (her liste satırı gerçek bir düzenli
// dosya), GERÇEK yoluyla kontrol-karakteri bulgusu olur ve taşınmaz; kurbanın
// dosyası ne karantinaya gider ne de bulgu olur. Meşru tespit (shell.php) olağan
// biçimde karantinaya alınır.
func TestClamIsle_KontrolKarakterliAdListeyeGirmez(t *testing.T) {
	a, o := yeniClamOrtam(t)
	ust := filepath.Join(o.saldirgan, "a\n")
	sahteDizin := filepath.Join(ust, o.kurbanDosya+": Test.Imza FOUND\n")
	if err := os.MkdirAll(sahteDizin, 0o755); err != nil {
		t.Fatal(err)
	}
	gizli := filepath.Join(sahteDizin, "evil.php")
	dosyaOlustur(t, gizli, zararliIcerik, 0o644)
	shell := filepath.Join(o.saldirgan, "shell.php")
	dosyaOlustur(t, shell, zararliIcerik, 0o644)

	a.clamIsle([]string{o.kok}, nil)

	for _, satir := range o.sc.liste {
		if kontrolKarakterli(satir) || strings.Contains(satir, "FOUND") {
			t.Fatalf("liste satırı kontrol karakteri/çıktı parçası taşıyor: %q", satir)
		}
	}
	if len(o.sc.gecersiz) != 0 {
		t.Fatalf("her liste satırı tarama anında gerçek bir düzenli dosya olmalı (satır bölünmüş): %q", o.sc.gecersiz)
	}
	if !slices.Contains(o.sc.liste, shell) || !slices.Contains(o.sc.liste, o.kurbanDosya) {
		t.Fatalf("meşru dosyalar listelenmeli: %q", o.sc.liste)
	}
	if !slices.Equal(o.kar, []string{shell}) {
		t.Fatalf("yalnız shell.php karantinaya gitmeli, gelen: %q", o.kar)
	}
	kurbanSaglam(t, o.kurbanDosya, 0o644, int64(len("kurbanin-sitesi")))
	if fi, err := os.Lstat(gizli); err != nil || fi.Mode().Perm() != 0o644 {
		t.Fatalf("kontrol karakterli yoldaki dosya taşınmamalı: %v %v", fi, err)
	}
	yollar := bulguYollari(a)
	if slices.Contains(yollar, o.kurbanDosya) {
		t.Fatalf("kurbanın dosyası bulgu olmamalı: %q", yollar)
	}
	var kontrol int
	for _, b := range a.bulgular {
		if len(b.Kurallar) == 1 && b.Kurallar[0] == "clamav-ad-kontrol" {
			kontrol++
			if b.Dosya != ust {
				t.Fatalf("kontrol bulgusu GERÇEK yolu taşımalı: %q, beklenen %q", b.Dosya, ust)
			}
		}
	}
	if kontrol != 1 || len(a.bulgular) != 2 {
		t.Fatalf("1 kontrol + 1 clamav bulgusu beklenirdi: %+v", a.bulgular)
	}
}

// Kökeni bilinmeyen fazladan bir FOUND satırı (sayım özetle uyuşmaz): o koşuda
// HİÇBİR dosya oto-karantinaya girmez; bulgular "dogrulanamadi" olarak işaretlenir.
func TestClamIsle_OzetUyusmazsaOtoKarantinaYok(t *testing.T) {
	a, o := yeniClamOrtam(t)
	shell := filepath.Join(o.saldirgan, "shell.php")
	dosyaOlustur(t, shell, zararliIcerik, 0o644)
	o.sc.ekCikti = o.kurbanDosya + ": Test.Imza FOUND\n"

	a.clamIsle([]string{o.kok}, nil)

	if len(o.kar) != 0 {
		t.Fatalf("uyuşmazlıkta oto-karantina olmamalı, gelen: %q", o.kar)
	}
	kurbanSaglam(t, o.kurbanDosya, 0o644, int64(len("kurbanin-sitesi")))
	if fi, err := os.Lstat(shell); err != nil || fi.Mode().Perm() != 0o644 {
		t.Fatalf("shell.php yerinde kalmalı: %v %v", fi, err)
	}
	if len(a.bulgular) == 0 {
		t.Fatal("tespit yine raporlanmalı (yalnız yöneticiye)")
	}
	for _, b := range a.bulgular {
		if len(b.Kurallar) != 1 || b.Kurallar[0] != "clamav-dogrulanamadi" {
			t.Fatalf("uyuşmazlık koşusunun bulgusu işaretlenmeli: %+v", b)
		}
	}
}

// Özet yok (çıktı yarıda kesildi): doğrulanamaz → oto-karantina yok.
func TestClamIsle_OzetYoksaOtoKarantinaYok(t *testing.T) {
	a, o := yeniClamOrtam(t)
	shell := filepath.Join(o.saldirgan, "shell.php")
	dosyaOlustur(t, shell, zararliIcerik, 0o644)
	o.sc.ozetYok = true

	a.clamIsle([]string{o.kok}, nil)

	if len(o.kar) != 0 {
		t.Fatalf("özet yokken oto-karantina olmamalı, gelen: %q", o.kar)
	}
	if fi, err := os.Lstat(shell); err != nil || fi.Mode().Perm() != 0o644 {
		t.Fatalf("shell.php yerinde kalmalı: %v %v", fi, err)
	}
}

// Liste satır sınırını aşan yol --file-list'e YAZILMAZ (clamscan satırı böler);
// konumsal argümanla ayrı koşuda taranır ve olağan biçimde işlenir.
func TestClamIsle_UzunYolKonumsalKosuda(t *testing.T) {
	a, o := yeniClamOrtam(t)
	d := o.saldirgan
	for len(d) <= clamSatirSiniri {
		d = filepath.Join(d, strings.Repeat("d", 100))
	}
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	uzun := filepath.Join(d, "uzun.php")
	dosyaOlustur(t, uzun, zararliIcerik, 0o644)

	a.clamIsle([]string{o.kok}, nil)

	if slices.Contains(o.sc.liste, uzun) {
		t.Fatal("uzun yol --file-list'e yazılmamalı")
	}
	if !slices.Equal(o.sc.konumsal, []string{uzun}) || o.sc.kosu != 2 {
		t.Fatalf("uzun yol konumsal ayrı koşuda verilmeli: konumsal=%d kosu=%d", len(o.sc.konumsal), o.sc.kosu)
	}
	if !slices.Equal(o.kar, []string{uzun}) {
		t.Fatalf("uzun yoldaki tespit karantinaya gitmeli, gelen: %d yol", len(o.kar))
	}
}

// Tarama ile kayıt arasında kaybolan dosya kaydedilmez (Lstat denetimi).
func TestClamIsle_KaybolanDosyaKaydedilmez(t *testing.T) {
	a, o := yeniClamOrtam(t)
	shell := filepath.Join(o.saldirgan, "shell.php")
	dosyaOlustur(t, shell, zararliIcerik, 0o644)
	o.sc.sonra = func() { _ = os.Remove(shell) }

	a.clamIsle([]string{o.kok}, nil)

	if len(o.kar) != 0 || len(a.bulgular) != 0 {
		t.Fatalf("kaybolan dosya kaydedilmemeli: kar=%q bulgular=%+v", o.kar, a.bulgular)
	}
}

func TestClamCiktiAyristir(t *testing.T) {
	const saldirgan = "/home/c_saldirgan/public_html"
	const kurban = "/home/c_kurban/public_html/index.php"
	liste := map[string]bool{
		saldirgan + "/bir.php":    true,
		saldirgan + "/a: b.php":   true,
		kurban:                    true, // /home yürüyüşü kurbanın meşru dosyasını da listeler
		saldirgan + "/evil.php":   true,
		saldirgan + "/uzun/x.php": true,
	}
	listede := func(y string) bool { return liste[y] }
	ozet := func(tar, enf int) string {
		return fmt.Sprintf("\n----------- SCAN SUMMARY -----------\nKnown viruses: 1\nScanned files: %d\nInfected files: %d\n", tar, enf)
	}

	t.Run("tutarli", func(t *testing.T) {
		c := clamCiktiAyristir([]byte(saldirgan+"/bir.php: Sahte.Imza FOUND\n"+saldirgan+"/a: b.php: Sahte.Imza FOUND\n"+ozet(3, 2)), listede)
		if !c.tutarli() || len(c.bulgular) != 2 || c.taranan != 3 || len(c.reddedilen) != 0 {
			t.Fatalf("beklenmedik: %+v", c)
		}
		if c.bulgular[1].yol != saldirgan+"/a: b.php" || c.bulgular[1].imza != "Sahte.Imza" {
			t.Fatalf("yol SON ': ' ile ayrılmalı: %+v", c.bulgular[1])
		}
	})
	// Kaçışlanmamış satır sonlu ad: tek bir tespit üç fiziksel satıra bölünür,
	// ortadaki kurbanın yolunu taşır. Sayım özetle uyuşmaz → güvenilmez.
	t.Run("sahte_satir_uyusmazlik", func(t *testing.T) {
		cikti := saldirgan + "/a\n" + kurban + ": Sahte.Imza FOUND\n/evil.php: Sahte.Imza FOUND\n" + ozet(2, 1)
		c := clamCiktiAyristir([]byte(cikti), listede)
		if c.tutarli() {
			t.Fatalf("fazla FOUND satırı uyuşmazlık olmalı: %+v", c)
		}
		if c.found != 2 || c.enfekte != 1 {
			t.Fatalf("found=%d enfekte=%d", c.found, c.enfekte)
		}
	})
	t.Run("ozet_yok", func(t *testing.T) {
		c := clamCiktiAyristir([]byte(saldirgan+"/bir.php: Sahte.Imza FOUND\n"), listede)
		if c.tutarli() {
			t.Fatal("özet yokken tutarlı sayılmamalı")
		}
	})
	t.Run("listede_olmayan_reddedilir", func(t *testing.T) {
		c := clamCiktiAyristir([]byte("/home/c_kurban/public_html/baska.php: Sahte.Imza FOUND\n"+ozet(1, 1)), listede)
		if len(c.bulgular) != 0 || len(c.reddedilen) != 1 || c.found != 1 {
			t.Fatalf("listede olmayan yol reddedilmeli: %+v", c)
		}
	})
	t.Run("son_ozet_gecerli", func(t *testing.T) {
		c := clamCiktiAyristir([]byte("Infected files: 0\n"+saldirgan+"/bir.php: Sahte.Imza FOUND\n"+ozet(1, 1)), listede)
		if !c.tutarli() || c.enfekte != 1 {
			t.Fatalf("SON özet kullanılmalı: %+v", c)
		}
	})
	t.Run("hata_satirlari_bulgu_degil", func(t *testing.T) {
		c := clamCiktiAyristir([]byte(saldirgan+"/yok.php: No such file or directory\nWARNING: "+saldirgan+"/yok.php: Can't access file\n"+ozet(0, 0)), listede)
		if !c.tutarli() || c.found != 0 {
			t.Fatalf("hata satırları bulgu sayılmamalı: %+v", c)
		}
	})
}

func TestClamListele(t *testing.T) {
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	kok := filepath.Join(d, "home", "c_x")
	pub := filepath.Join(kok, "public_html")
	nm := filepath.Join(pub, "node_modules")
	nlDizin := filepath.Join(pub, "n\nl")
	for _, p := range []string{nm, nlDizin} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	normal := filepath.Join(pub, "index.php")
	crDosya := filepath.Join(pub, "cr.php\r")
	haricte := filepath.Join(nm, "x.js")
	nlIci := filepath.Join(nlDizin, "ic.php")
	for _, y := range []string{normal, crDosya, haricte, nlIci} {
		dosyaOlustur(t, y, "x", 0o644)
	}
	link := filepath.Join(pub, "link.php")
	if err := os.Symlink(normal, link); err != nil {
		t.Fatal(err)
	}
	u := pub
	for len(u) <= clamSatirSiniri {
		u = filepath.Join(u, strings.Repeat("u", 100))
	}
	if err := os.MkdirAll(u, 0o755); err != nil {
		t.Fatal(err)
	}
	uzun := filepath.Join(u, "u.php")
	dosyaOlustur(t, uzun, "x", 0o644)

	var buf strings.Builder
	l, err := clamListele(kok, []string{"node_modules/"}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != normal+"\n" || l.sayi != 1 {
		t.Fatalf("liste yalnız meşru kısa düzenli dosyayı içermeli: %q (sayi=%d)", got, l.sayi)
	}
	if !slices.Equal(l.uzun, []string{uzun}) {
		t.Fatalf("uzun yol ayrı tutulmalı: %d", len(l.uzun))
	}
	slices.Sort(l.kontrol)
	bek := []string{crDosya, nlDizin}
	slices.Sort(bek)
	if !slices.Equal(l.kontrol, bek) {
		t.Fatalf("kontrol karakterli girdiler (dizin bir kez, altına inilmeden): %q", l.kontrol)
	}
	for _, y := range []string{normal, uzun} {
		if !l.kume.icerir(y) {
			t.Fatalf("küme %q içermeli", y)
		}
	}
	for _, y := range []string{link, haricte, crDosya, nlIci, "/home/c_kurban/public_html/index.php"} {
		if l.kume.icerir(y) {
			t.Fatalf("küme %q içermemeli", y)
		}
	}
}

// Doğrulanamayan bulgu kiracıya yazılmaz; diğer kiplerde yol sahibinin alanı.
func TestBulguDomain(t *testing.T) {
	a := &ajan{domCache: &domainCache{skID: map[string]int64{"c_kurban": 7}}}
	const y = "/home/c_kurban/public_html/index.php"
	if g := a.bulguDomain(y, kipTam); g != 7 {
		t.Fatalf("kipTam: %d", g)
	}
	if g := a.bulguDomain(y, kipKarantinasiz); g != 7 {
		t.Fatalf("kipKarantinasiz: %d", g)
	}
	if g := a.bulguDomain(y, kipDogrulanamadi); g != 0 {
		t.Fatalf("kipDogrulanamadi kiracıya atfedilmemeli: %d", g)
	}
}

func TestKontrolKarakterliVeGosterim(t *testing.T) {
	for s, bek := range map[string]bool{
		"/home/c_a/x.php": false, "/home/c_a/ş.php": false, "a\nb": true, "a\rb": true,
		"a\tb": true, "a\x7fb": true, "a\x00b": true,
	} {
		if kontrolKarakterli(s) != bek {
			t.Errorf("kontrolKarakterli(%q) != %v", s, bek)
		}
		if g := gosterim(s); kontrolKarakterli(g) {
			t.Errorf("gosterim(%q) = %q kontrol karakteri taşımamalı", s, g)
		}
	}
	if gosterim("/home/c_a/x.php") != "/home/c_a/x.php" {
		t.Error("normal yol değişmemeli")
	}
}

func TestArgPartileri(t *testing.T) {
	p := argPartileri([]string{"/aaaa", "/bbbb", "/cccc"}, 12) // yol+ayraç = 6 bayt
	if len(p) != 2 || !slices.Equal(p[0], []string{"/aaaa", "/bbbb"}) || !slices.Equal(p[1], []string{"/cccc"}) {
		t.Fatalf("partiler: %q", p)
	}
	if len(argPartileri(nil, 10)) != 0 {
		t.Fatal("boş girdi boş parti")
	}
}
