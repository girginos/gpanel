package tasima

// arsiv_aktar_test.go — arşiv kaynaklı işin ADIMLARI: dosya (kiracı kimliğinde, bütçeli),
// veritabanı (iki geçişli tarama + süzgeçli içe aktarma), DNS, posta (vmail kimliği,
// kutu süzgeci, bağ yok), cron (rapor), iş başlatma (parmak izi/kapsam).
// Gerçek tar + runuser (kiracı yerine "nobody"); mysql/ağ yok (içe aktarma dikişi sahte).

import (
	"archive/tar"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"time"

	"girginospanel/internal/cron"
	"girginospanel/internal/efektiflimit"
	"girginospanel/internal/evalti"
	"girginospanel/internal/tasima/arsiv"
)

// arsivKaynakT — uyeleri bir tar.gz dosyasına yazar, AcHesap ile açar, kaynak döndürür.
func arsivKaynakT(t *testing.T, tip string, uyeler []tUye) (*Kaynak, string) {
	t.Helper()
	yol := filepath.Join(t.TempDir(), "yedek.tar.gz")
	if err := os.WriteFile(yol, tarGz(t, uyeler), 0o644); err != nil {
		t.Fatal(err)
	}
	a, m, err := arsiv.AcHesap(context.Background(), yol, arsiv.Secenek{})
	if err != nil {
		t.Fatalf("AcHesap: %v", err)
	}
	return &Kaynak{Tip: tip, arsiv: &arsivBaglam{A: a, M: m, Yol: yol, DumpSinir: 1 << 30}}, yol
}

// kiraciOrtamT — yrYeniOrtam + evalti (arşiv çıkarma) aynı geçici ev köküne/nobody'ye bağlanır.
func kiraciOrtamT(t *testing.T) yrOrtam {
	t.Helper()
	o := yrYeniOrtam(t)
	eskiK, eskiU := evalti.EvKoku, evalti.OSKullanici
	evalti.EvKoku = o.base
	evalti.OSKullanici = func(string) string { return "nobody" }
	t.Cleanup(func() { evalti.EvKoku, evalti.OSKullanici = eskiK, eskiU })
	return o
}

func uidDe(t *testing.T, yol string) int {
	t.Helper()
	fi, err := os.Lstat(yol)
	if err != nil {
		t.Fatalf("%s: %v", yol, err)
	}
	return int(fi.Sys().(*syscall.Stat_t).Uid)
}

func butceSabit(b int64) func(*Handlers, context.Context, Kapsam, int64) int64 {
	return func(*Handlers, context.Context, Kapsam, int64) int64 { return b }
}

// Kural: dosyalar arşivden KİRACI kimliğinde çıkar (kök olarak yol üzerinden yazılmaz); aygıt/
// bağ süzgeci raporlanır (adlar değil sayılar); Laravel docroot-dışı bağımlılığı çıkar ama
// posta/parola dizinleri çıkmaz; bütçe aşımı kalemi 'atlandi' yapar; kök (./etc) yazılmaz.
func TestArsivDosyalariAktar_KiraciKimliginde(t *testing.T) {
	o := kiraciOrtamT(t)
	eski := arsivButceFn
	t.Cleanup(func() { arsivButceFn = eski })
	arsivButceFn = butceSabit(arsiv.ButceSinirsiz)
	k := "cpmove-u/"
	kaynak, _ := arsivKaynakT(t, "arsiv-cpanel", []tUye{
		{ad: k + "cp/u", veri: "USER=u\n"},
		{ad: k + "userdata/main", veri: "main_domain: ornek.com\naddon_domains:\n  ek.net: ek.net.ornek.com\n"},
		{ad: k + "userdata/ornek.com", veri: "documentroot: /home/u/public_html\n"},
		{ad: k + "userdata/ek.net.ornek.com", veri: "documentroot: /home/u/ek\n"},
		{ad: k + "homedir/public_html/index.php", veri: "<?php require __DIR__.'/../laravel_11/vendor/autoload.php'; include '../mail/x/'; ?>"},
		{ad: k + "homedir/public_html/wp/a.txt", veri: "AAAA"},
		{ad: k + "homedir/public_html/kisayol", tur: tar.TypeSymlink, hedef: "wp/a.txt"},
		{ad: k + "homedir/public_html/disbag", tur: tar.TypeSymlink, hedef: "../../../../etc"},
		{ad: k + "homedir/public_html/aygit", tur: tar.TypeChar},
		{ad: k + "homedir/public_html/suid", veri: "x", mod: 0o6755},
		{ad: k + "homedir/laravel_11/vendor/autoload.php", veri: "<?php"},
		{ad: k + "homedir/mail/x/secret", veri: "POSTA-ICERIGI"},
		{ad: k + "homedir/ek/b.txt", veri: "BBB"},
		{ad: k + "homedir/gizli-ev-dosyasi", veri: "EV-DOSYASI"},
	})
	h := &Handlers{}
	var gunluk []string
	log := func(f string, a ...any) { gunluk = append(gunluk, fmt.Sprintf(f, a...)) }

	webRoot := filepath.Join(o.ev, "public_html")
	o.dizin(t, webRoot)
	sonuc := &AktarSonuc{}
	err := h.arsivDosyalariAktar(context.Background(), kaynak, Hesap{AlanAdi: "ornek.com"}, Ayarlar{Dosyalar: true}, Kapsam{}, yrSK, webRoot, "ornek.com", sonuc, log)
	if err != nil {
		t.Fatalf("%v\n%s", err, strings.Join(gunluk, "\n"))
	}
	oku := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(o.ev, rel))
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		return string(b)
	}
	if oku("public_html/wp/a.txt") != "AAAA" || !strings.Contains(oku("public_html/index.php"), "laravel_11") {
		t.Fatal("docroot içeriği yanlış")
	}
	if oku("laravel_11/vendor/autoload.php") != "<?php" {
		t.Error("docroot-dışı bağımlılık çıkarılmadı")
	}
	for _, yok := range []string{"mail", "gizli-ev-dosyasi", "public_html/aygit", "public_html/disbag", "ek"} {
		if _, err := os.Lstat(filepath.Join(o.ev, yok)); err == nil {
			t.Errorf("çıkmaması gereken %q çıktı", yok)
		}
	}
	if hd, _ := os.Readlink(filepath.Join(o.ev, "public_html/kisayol")); hd != "wp/a.txt" {
		t.Errorf("güvenli bağ: %q", hd)
	}
	if fi, _ := os.Lstat(filepath.Join(o.ev, "public_html/suid")); fi == nil || fi.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 {
		t.Errorf("suid biti kalmamalı: %v", fi)
	}
	for _, p := range []string{"public_html/index.php", "public_html/wp", "laravel_11/vendor/autoload.php"} {
		if uidDe(t, filepath.Join(o.ev, p)) != o.uid {
			t.Errorf("%s kiracıya (nobody) ait değil", p)
		}
	}
	// Rapor: sayılar var, atlanan adlar günlükte/uyarıda YOK.
	if sonuc.DosyaBayt <= 0 {
		t.Errorf("DosyaBayt: %d", sonuc.DosyaBayt)
	}
	tum := strings.Join(append(gunluk, sonuc.Uyarilar...), "\n")
	if !strings.Contains(tum, "atlandı") && !strings.Contains(tum, "atlandi") {
		t.Errorf("atlanan üyeler raporlanmadı: %s", tum)
	}
	for _, sizinti := range []string{"disbag", "aygit", "POSTA-ICERIGI"} {
		if strings.Contains(tum, sizinti) {
			t.Errorf("rapor arşiv içeriğinden %q sızdırıyor", sizinti)
		}
	}

	// Ek alan: kendi belge kökü (ek/) ve ayrı hedef.
	ekRoot := filepath.Join(o.ev, "ek_root")
	o.dizin(t, ekRoot)
	if err := h.arsivDosyalariAktar(context.Background(), kaynak, Hesap{AlanAdi: "ek.net"}, Ayarlar{Dosyalar: true}, Kapsam{}, yrSK, ekRoot, "ek.net", &AktarSonuc{}, log); err != nil {
		t.Fatal(err)
	}
	if oku("ek_root/b.txt") != "BBB" {
		t.Error("ek alan belge kökü çıkmadı")
	}
	// Dosyalar kapalıysa hiçbir şey yazılmaz; arşivde olmayan alan hata.
	bosKok := filepath.Join(o.ev, "bos_kok")
	o.dizin(t, bosKok)
	if err := h.arsivDosyalariAktar(context.Background(), kaynak, Hesap{AlanAdi: "ornek.com"}, Ayarlar{}, Kapsam{}, yrSK, bosKok, "ornek.com", &AktarSonuc{}, log); err != nil {
		t.Errorf("dosyalar kapalı: %v", err)
	}
	if g, _ := os.ReadDir(bosKok); len(g) != 0 {
		t.Errorf("dosyalar kapalıyken hedefe yazıldı: %v", g)
	}
	if err := h.arsivDosyalariAktar(context.Background(), kaynak, Hesap{AlanAdi: "yok.com"}, Ayarlar{Dosyalar: true}, Kapsam{}, yrSK, webRoot, "yok.com", &AktarSonuc{}, log); err == nil {
		t.Error("arşivde olmayan alan adı kabul edildi")
	}
	// Belge kökü yedekte yok: hata değil, 'Dosya yok' uyarısı.
	k2, _ := arsivKaynakT(t, "arsiv-cpanel", []tUye{
		{ad: k + "cp/u", veri: "USER=u\n"},
		{ad: k + "userdata/main", veri: "main_domain: ornek.com\n"},
		{ad: k + "userdata/ornek.com", veri: "documentroot: /home/u/public_html\n"},
		{ad: k + "homedir/x", veri: "x"},
	})
	s2 := &AktarSonuc{}
	if err := h.arsivDosyalariAktar(context.Background(), k2, Hesap{AlanAdi: "ornek.com"}, Ayarlar{Dosyalar: true}, Kapsam{}, yrSK, webRoot, "ornek.com", s2, log); err != nil ||
		len(s2.Uyarilar) != 1 || !strings.Contains(s2.Uyarilar[0], "Dosya yok") {
		t.Errorf("belge kökü yok: %v %v", err, s2.Uyarilar)
	}
}

func TestArsivDosyalariAktar_ButceAsimiAtlandi(t *testing.T) {
	o := kiraciOrtamT(t)
	eski := arsivButceFn
	t.Cleanup(func() { arsivButceFn = eski })
	k := "cpmove-u/"
	kaynak, _ := arsivKaynakT(t, "arsiv-cpanel", []tUye{
		{ad: k + "cp/u", veri: "USER=u\n"},
		{ad: k + "userdata/main", veri: "main_domain: ornek.com\n"},
		{ad: k + "userdata/ornek.com", veri: "documentroot: /home/u/public_html\n"},
		{ad: k + "homedir/public_html/a", veri: strings.Repeat("A", 6000)},
	})
	webRoot := filepath.Join(o.ev, "public_html")
	o.dizin(t, webRoot)
	h := &Handlers{}
	// Bütçe: 1 blok (4 KiB) — seçim 2 blok+ ister → ErrKota → kotaHatasi (kalem 'atlandi'), hiçbir şey yazılmaz.
	arsivButceFn = butceSabit(4096)
	err := h.arsivDosyalariAktar(context.Background(), kaynak, Hesap{AlanAdi: "ornek.com"}, Ayarlar{Dosyalar: true}, Kapsam{RID: 7}, yrSK, webRoot, "ornek.com", &AktarSonuc{}, func(string, ...any) {})
	var kh *kotaHatasi
	if !errors.As(err, &kh) || kalemDurumu(err) != "atlandi" {
		t.Fatalf("kotaHatasi beklendi: %v", err)
	}
	if hataMetni(false, err).Error() != kh.mesaj {
		t.Errorf("bayiye kota metni aynen gösterilmeli: %v", hataMetni(false, err))
	}
	if g, _ := os.ReadDir(webRoot); len(g) != 0 {
		t.Errorf("bütçe aşımında dosya yazıldı: %v", g)
	}
	// Bütçe tam yeterli: geçer (sınır değeri dahil: 2 blok = 8192 + başlık bloğu yok).
	arsivButceFn = butceSabit(1 << 20)
	if err := h.arsivDosyalariAktar(context.Background(), kaynak, Hesap{AlanAdi: "ornek.com"}, Ayarlar{Dosyalar: true}, Kapsam{RID: 7}, yrSK, webRoot, "ornek.com", &AktarSonuc{}, func(string, ...any) {}); err != nil {
		t.Fatalf("yeterli bütçe: %v", err)
	}
	// ErrDisk (hedefte yer yok) hatası kota sanılmaz.
	if arsivDosyaHatasi(arsiv.ErrDisk) == nil || kalemDurumu(arsivDosyaHatasi(arsiv.ErrDisk)) != "hata" {
		t.Error("ErrDisk kalemi 'hata' olmalı")
	}
}

// Kural: veritabanı İKİ geçişle içe aktarılır — ilk geçiş SQL tarayıcı (manifest taramasından
// sonra dosya içeriği değişse bile yakalanır), reddedilen/yarım/olmayan döküm içe aktarılmaz.
func TestArsivDBAktar_TaramaVeSuzgec(t *testing.T) {
	k := "cpmove-u/"
	uyeler := func(dump string) []tUye {
		return []tUye{
			{ad: k + "cp/u", veri: "USER=u\n"}, {ad: k + "userdata/main", veri: "main_domain: ornek.com\n"},
			{ad: k + "mysql/u_wp.sql", veri: dump},
		}
	}
	eski := dbIceAktarici
	t.Cleanup(func() { dbIceAktarici = eski })
	var alinan []string
	var hedefler []string
	dbIceAktarici = func(_ *Handlers, _ context.Context, r io.Reader, hedef string) error {
		b, _ := io.ReadAll(r)
		alinan = append(alinan, string(b))
		hedefler = append(hedefler, hedef)
		return nil
	}
	h := &Handlers{}
	log := func(string, ...any) {}
	ctx := context.Background()

	kaynak, _ := arsivKaynakT(t, "arsiv-cpanel", uyeler(tDump))
	if err := h.dbAktar(ctx, kaynak, "u_wp", "c_x_wp", log); err != nil {
		t.Fatal(err)
	}
	if len(alinan) != 1 || alinan[0] != tDump || hedefler[0] != "c_x_wp" {
		t.Fatalf("içe aktarılan: %q → %v", alinan, hedefler)
	}
	// DEFINER içeren temiz döküm de geçer (süzme dumpFiltre'de; tarayıcı yalnız sayar).
	definerli := "CREATE DEFINER=`root`@`localhost` VIEW v AS SELECT 1;\n" + tDump
	kd, _ := arsivKaynakT(t, "arsiv-cpanel", uyeler(definerli))
	if err := h.dbAktar(ctx, kd, "u_wp", "c_x_wp", log); err != nil {
		t.Fatalf("DEFINER'lı döküm: %v", err)
	}
	// Olmayan veritabanı adı.
	if err := h.dbAktar(ctx, kaynak, "u_baska", "c_x_b", log); err == nil || !strings.Contains(err.Error(), "arsivde yok") {
		t.Errorf("olmayan DB: %v", err)
	}
	// Yarım döküm (bitiş damgası yok): içe aktarılmaz.
	n := len(alinan)
	ky, _ := arsivKaynakT(t, "arsiv-cpanel", uyeler("CREATE TABLE t (a int);\nINSERT INTO t VALUES (1);\n"))
	if err := h.dbAktar(ctx, ky, "u_wp", "c_x_wp", log); err == nil || !strings.Contains(err.Error(), "yarim") {
		t.Errorf("yarım döküm: %v", err)
	}
	// Manifest taramasında reddedilmiş döküm: SQLHata, içe aktarılmaz; bayiye genel metin.
	kr, _ := arsivKaynakT(t, "arsiv-cpanel", uyeler("GRANT ALL PRIVILEGES ON *.* TO 'x'@'%';\n"+tDump))
	err := h.dbAktar(ctx, kr, "u_wp", "c_x_wp", log)
	var sh *arsiv.SQLHata
	if !errors.As(err, &sh) || !errors.Is(err, arsiv.ErrSQL) {
		t.Fatalf("reddedilmiş döküm: %v", err)
	}
	if m := hataMetni(false, err).Error(); strings.Contains(m, sh.Kural) || m != arsiv.ErrSQL.Error() {
		t.Errorf("bayiye kural kodu/ayrıntı gitmemeli: %q", m)
	}
	// 🔴 TOCTOU: manifest taramasından SONRA dosya içeriği (aynı boyutta) kötü SQL ile değiştirilir;
	// envanter aynı kalır (ErrDegisti tetiklenmez) ama içe aktarma öncesi yeniden tarama yakalar.
	kt, yol := arsivKaynakT(t, "arsiv-cpanel", uyeler(tDump))
	ham, _ := os.ReadFile(yol)
	kotu := "GRANT ALL ON *.* TO 'a'@'%';" // tDump ile aynı uzunluğa tamamlanır
	for len(kotu) < len(tDump)-1 {
		kotu += " "
	}
	kotu += "\n"
	if len(kotu) != len(tDump) {
		t.Fatalf("uzunluk: %d %d", len(kotu), len(tDump))
	}
	// gzip'li dosyada yerinde değişim yerine arşivi aynı üye boyutuyla yeniden yaz.
	yeni := tarGz(t, uyeler(kotu))
	if err := os.WriteFile(yol, yeni, 0o644); err != nil {
		t.Fatal(err)
	}
	_ = ham
	n = len(alinan)
	if err := h.dbAktar(ctx, kt, "u_wp", "c_x_wp", log); !errors.Is(err, arsiv.ErrSQL) {
		t.Fatalf("değiştirilmiş döküm kabul edildi: %v", err)
	}
	if len(alinan) != n {
		t.Fatal("reddedilen dökümün içeriği içe aktarıcıya ulaştı")
	}
	// Geçersiz ad hiçbir şeyi çağırmaz.
	if err := h.dbAktar(ctx, kaynak, "u wp;", "c_x_wp", log); err == nil {
		t.Error("geçersiz kaynak DB adı kabul edildi")
	}
}

func TestArsivZoneOku(t *testing.T) {
	k := "cpmove-u/"
	kc, _ := arsivKaynakT(t, "arsiv-cpanel", []tUye{
		{ad: k + "cp/u", veri: "USER=u\n"}, {ad: k + "userdata/main", veri: "main_domain: ornek.com\n"},
		{ad: k + "userdata/ornek.com", veri: "documentroot: /home/u/public_html\n"},
		{ad: k + "dnszones/ornek.com.db", veri: "$TTL 14400\nornek.com. IN SOA ns1. h. (1 2 3 4 5)\n@ IN A 1.2.3.4\nwww IN CNAME @\n@ IN MX 10 mail.ornek.com.\n"},
		{ad: k + "dnszones/baska.org.db", veri: "@ IN A 9.9.9.9\n"},
		{ad: k + "homedir/public_html/i", veri: "x"},
	})
	kayit := arsivZoneOku(kc, "ornek.com")
	var s []string
	for _, r := range kayit {
		s = append(s, r.Tip+":"+r.Ad+":"+r.Deger)
	}
	sort.Strings(s)
	if len(kayit) < 3 || !strings.Contains(strings.Join(s, "|"), "A:@:1.2.3.4") || !strings.Contains(strings.Join(s, "|"), "MX:@:") {
		t.Fatalf("bölge kayıtları: %v", s)
	}
	if arsivZoneOku(kc, "baska.org") != nil {
		t.Error("hesabın alanı olmayan bölge (üst veri dışı) okunmamalı")
	}
	if arsivZoneOku(kc, "yok.com") != nil {
		t.Error("olmayan bölge boş dönmeli")
	}
	kd, _ := arsivKaynakT(t, "arsiv-directadmin", []tUye{
		{ad: "backup/user.conf", veri: "username=da1\ndomain=ornek.com\n"},
		{ad: "domains/ornek.com/public_html/i", veri: "x"},
	})
	if arsivZoneOku(kd, "ornek.com") != nil {
		t.Error("DirectAdmin'de bölge okunmaz")
	}
}

// Kural: ana akış adımları — kök/bayi bütçesi plan disk kotası ∧ bayi havuzu kalanı;
// havuz okunamazsa 0 (fail-closed).
func TestArsivButce_PlanVeBayiHavuzu(t *testing.T) {
	db := ta_db(t)
	var spSutun []string
	for _, a := range efektiflimit.Alanlar {
		spSutun = append(spSutun, "`"+a.Sutun+"` INT NOT NULL DEFAULT 0")
	}
	for _, q := range []string{
		`ALTER TABLE domains ADD COLUMN plan_id BIGINT UNSIGNED NULL, ADD COLUMN disk_kota_mb INT NOT NULL DEFAULT 0,
			ADD COLUMN inode_kota INT NOT NULL DEFAULT 0, ADD COLUMN boyut_kb BIGINT NOT NULL DEFAULT 0`,
		"CREATE TABLE service_plans (id BIGINT UNSIGNED PRIMARY KEY, ad VARCHAR(64) NOT NULL DEFAULT '', " +
			"reseller_id BIGINT NOT NULL DEFAULT 0, domain_id BIGINT UNSIGNED NULL, " + strings.Join(spSutun, ", ") + ")",
		efektiflimit.TabloDDL,
		`ALTER TABLE users ADD COLUMN max_disk_mb BIGINT NOT NULL DEFAULT 0`,
		`INSERT INTO service_plans (id, disk_kota_mb) VALUES (3, 100), (4, 0)`,
		`INSERT INTO domains (id, alan_adi, reseller_id, plan_id) VALUES (50,'plan100.test',7,3), (51,'plansiz.test',7,4), (52,'bayi-diger.test',7,4)`,
		`UPDATE users SET max_disk_mb=300 WHERE id=7`,
		`UPDATE domains SET boyut_kb=? WHERE id=52`,
	} {
		args := []any{}
		if strings.HasPrefix(q, "UPDATE domains SET boyut_kb") {
			args = append(args, 250<<10) // 250 MiB başka site → havuzda 50 MiB kalır
		}
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("şema: %v\n%s", err, q)
		}
	}
	h := &Handlers{DB: db}
	ctx := context.Background()
	const MiB = int64(1) << 20
	for ad, c := range map[string]struct {
		kp   Kapsam
		id   int64
		want int64
	}{
		"kök, plan 100 MB":               {Kapsam{}, 50, 100 * MiB},
		"kök, plansız → sınırsız":        {Kapsam{}, 51, arsiv.ButceSinirsiz},
		"bayi, plan 100 ∧ havuz 50 → 50": {Kapsam{RID: 7}, 50, 50 * MiB},
		"bayi, plansız ∧ havuz 50 → 50":  {Kapsam{RID: 7}, 51, 50 * MiB},
	} {
		if got := h.arsivButce(ctx, c.kp, c.id); got != c.want {
			t.Errorf("%s: %d beklendi, gelen %d", ad, c.want, got)
		}
	}
	// Havuz sınırsız (0): yalnız plan; plan küçükse plan.
	_, _ = db.Exec(`UPDATE users SET max_disk_mb=0 WHERE id=7`)
	if got := h.arsivButce(ctx, Kapsam{RID: 7}, 50); got != 100*MiB {
		t.Errorf("havuz sınırsız, plan 100: %d", got)
	}
	if got := h.arsivButce(ctx, Kapsam{RID: 7}, 51); got != arsiv.ButceSinirsiz {
		t.Errorf("her ikisi sınırsız: %d", got)
	}
	// Havuz okunamazsa (bayi kaydı yok) fail-closed: 0 bütçe.
	if got := h.arsivButce(ctx, Kapsam{RID: 999}, 51); got != 0 {
		t.Errorf("bayi kaydı yok → 0 beklendi: %d", got)
	}
	// Kalan havuz negatife düşmez.
	_, _ = db.Exec(`UPDATE users SET max_disk_mb=100 WHERE id=7`)
	if got := h.arsivButce(ctx, Kapsam{RID: 7}, 51); got != 0 {
		t.Errorf("havuz aşılmış → 0: %d", got)
	}
}

// ---- posta ----

// Kural: kutular eklentide açılır (plan kotası, sahip kimliği); maildir vmail kimliğinde,
// yalnız bilinen kutular için, dovecot/sieve/bağ HARİÇ çıkar; geçersiz/komşu kutu atlanır;
// parola uyarısı bırakılır.
func TestArsivMailAktar_VmailKimligiVeSuzgec(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("runuser/chown için root gerekli")
	}
	nb, err := user.Lookup("nobody")
	if err != nil {
		t.Skip("nobody yok")
	}
	nuid, _ := strconv.Atoi(nb.Uid)
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar yok")
	}
	db := ta_db(t)
	ek := sahteMailEklenti(t)
	var spSutun []string
	for _, a := range efektiflimit.Alanlar {
		spSutun = append(spSutun, "`"+a.Sutun+"` INT NOT NULL DEFAULT 0")
	}
	for _, q := range []string{
		`ALTER TABLE domains ADD COLUMN plan_id BIGINT UNSIGNED NULL, ADD COLUMN disk_kota_mb INT NOT NULL DEFAULT 0, ADD COLUMN inode_kota INT NOT NULL DEFAULT 0`,
		"CREATE TABLE service_plans (id BIGINT UNSIGNED PRIMARY KEY, ad VARCHAR(64) NOT NULL DEFAULT '', " +
			"reseller_id BIGINT NOT NULL DEFAULT 0, domain_id BIGINT UNSIGNED NULL, " + strings.Join(spSutun, ", ") + ")",
		efektiflimit.TabloDDL,
		`CREATE TABLE cp_eklentiler (ad VARCHAR(64) PRIMARY KEY, aktif TINYINT NOT NULL DEFAULT 0, soket VARCHAR(255) NOT NULL DEFAULT '')`,
		`INSERT INTO service_plans (id, max_email, saatlik_mail_limiti, mail_kutu_kota_mb) VALUES (3, 9, 50, 250)`,
		`INSERT INTO domains (id, alan_adi, reseller_id, plan_id) VALUES (30,'ornek.com',7,3)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("şema: %v\n%s", err, q)
		}
	}
	// vmail kökü: nobody'nin geçebileceği geçici dizin.
	base := t.TempDir()
	for _, d := range []string{filepath.Dir(base), base} {
		_ = os.Chmod(d, 0o711)
	}
	kok := filepath.Join(base, "vmail")
	_ = os.Mkdir(kok, 0o755)
	eskiV, eskiK := vmailKok, mailVmailKul
	vmailKok, mailVmailKul = kok, "nobody"
	t.Cleanup(func() { vmailKok, mailVmailKul = eskiV, eskiK })

	p := "cpmove-u/homedir/mail/ornek.com/"
	kaynak, _ := arsivKaynakT(t, "arsiv-cpanel", []tUye{
		{ad: "cpmove-u/cp/u", veri: "USER=u\n"},
		{ad: "cpmove-u/userdata/main", veri: "main_domain: ornek.com\n"},
		{ad: "cpmove-u/userdata/ornek.com", veri: "documentroot: /home/u/public_html\n"},
		{ad: "cpmove-u/homedir/public_html/i", veri: "x"},
		{ad: "cpmove-u/homedir/etc/ornek.com/passwd", veri: "info:" + tHash + ":1:2::/x:\nsatis:" + tHash + ":1:2::/x:\nbos:" + tHash + ":1:2::/x:\n../kotu:h\n"},
		{ad: p + "info/cur/1:2,S", veri: "mesaj-1"},
		{ad: p + "info/new/2", veri: "mesaj-2"},
		{ad: p + "info/dovecot-uidlist", veri: "iç"},
		{ad: p + "info/sieve/aktif.sieve", veri: "redirect :copy \"x@y\";"},
		{ad: p + "info/.dovecot.sieve", veri: "x"},
		{ad: p + "info/baglanti", tur: tar.TypeSymlink, hedef: "../satis"},
		{ad: p + "satis/cur/9", veri: "satis-mesaji"},
		{ad: p + "bilinmeyen/cur/1", veri: "passwd'de olmayan kutu"},
	})
	if _, err := db.Exec(`INSERT INTO cp_eklentiler VALUES ('mail',1,?)`, ek.soket); err != nil {
		t.Fatal(err)
	}
	h := &Handlers{DB: db}
	n, uyar, err := h.arsivMailAktar(context.Background(), kaynak, Hesap{AlanAdi: "ornek.com"}, func(string, ...any) {})
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("3 kutu (info, satis, bos) işlenmeli: %d %q", n, uyar)
	}
	okuma := func(rel string) string { b, _ := os.ReadFile(filepath.Join(kok, "ornek.com", rel)); return string(b) }
	if okuma("info/cur/1:2,S") != "mesaj-1" || okuma("info/new/2") != "mesaj-2" || okuma("satis/cur/9") != "satis-mesaji" {
		t.Errorf("maildir içeriği çıkmadı: %v", uyar)
	}
	for _, yok := range []string{"info/dovecot-uidlist", "info/sieve", "info/.dovecot.sieve", "info/baglanti", "bilinmeyen", "kotu"} {
		if _, err := os.Lstat(filepath.Join(kok, "ornek.com", yok)); err == nil {
			t.Errorf("çıkmaması gereken %q çıktı", yok)
		}
	}
	for _, p := range []string{"info/cur/1:2,S", "info", "satis/cur/9", "info/new"} {
		if uidDe(t, filepath.Join(kok, "ornek.com", p)) != nuid {
			t.Errorf("%s vmail kimliğine (testte nobody) ait değil", p)
		}
	}
	// Eklenti: bayi kimliği, plan kotası; kutular açıldı (geçersiz ad açılmadı).
	var posts []string
	for _, x := range ek.al() {
		if x.Yontem == "POST" && x.Yol == "/hesaplar" {
			posts = append(posts, fmt.Sprint(x.Govde["email"]))
			if x.Rol != "reseller" || x.Uid != "7" || x.Sahip != "ornek.com" || x.Govde["quota_mb"] != float64(250) {
				t.Errorf("eklenti çağrısı: %+v", x)
			}
			if pw, _ := x.Govde["parola"].(string); len(pw) < 12 {
				t.Errorf("üretilmiş güçlü parola beklenir: %q", pw)
			}
		}
	}
	sort.Strings(posts)
	if strings.Join(posts, ",") != "bos@ornek.com,info@ornek.com,satis@ornek.com" {
		t.Errorf("açılan kutular: %v", posts)
	}
	tum := strings.Join(uyar, "\n")
	if !strings.Contains(tum, "kutu parolaları arşivde bulunmuyor") {
		t.Errorf("uyarılar: %q", tum)
	}
	if strings.Contains(tum, kok) || strings.Contains(tum, tHash) {
		t.Errorf("uyarı yol/özet sızdırıyor: %q", tum)
	}
	// Arşivde kutu yoksa tek bilgi satırı.
	k0, _ := arsivKaynakT(t, "arsiv-cpanel", []tUye{{ad: "cpmove-u/cp/u", veri: "USER=u\n"}, {ad: "cpmove-u/userdata/main", veri: "main_domain: ornek.com\n"}, {ad: "cpmove-u/userdata/ornek.com", veri: "documentroot: /home/u/public_html\n"}, {ad: "cpmove-u/homedir/public_html/i", veri: "x"}})
	if n, u, err := h.arsivMailAktar(context.Background(), k0, Hesap{AlanAdi: "ornek.com"}, func(string, ...any) {}); n != 0 || err != nil || len(u) != 1 {
		t.Errorf("kutu yok: %d %v %v", n, u, err)
	}
	_ = bytes.MinRead
}

// ---- cron ----

func cronKaynakT(t *testing.T) *Kaynak {
	t.Helper()
	k := "cpmove-u/"
	kaynak, _ := arsivKaynakT(t, "arsiv-cpanel", []tUye{
		{ad: k + "cp/u", veri: "USER=u\n"},
		{ad: k + "userdata/main", veri: "main_domain: ornek.com\naddon_domains:\n  ek.net: ek.net.ornek.com\n"},
		{ad: k + "userdata/ornek.com", veri: "documentroot: /home/u/public_html\n"},
		{ad: k + "userdata/ek.net.ornek.com", veri: "documentroot: /home/u/ek\n"},
		{ad: k + "homedir/public_html/i", veri: "x"}, {ad: k + "homedir/ek/i", veri: "x"},
		{ad: k + "cron/u", veri: "MAILTO=\"\"\n*/5 * * * * php /home/u/public_html/cron.php\n@reboot /home/u/public_html/boot.sh\n" +
			"0 1 * * * /home/u/ek/script.sh\n* * * * * sudo ls\n0 0 * * * cat /home/baska/x\n0 4 * * * cp /home/u/public_html/a /home/u/ek/b\n"},
	})
	m := kaynak.arsiv.M
	kaynak.arsiv.CronAtama, kaynak.arsiv.CronRed = cronAta(m.Cron, cronBaglam{EskiKullanici: m.Kullanici, AnaAlan: m.AnaAlan, Alanlar: m.Alanlar})
	return kaynak
}

func cronMailtoKaynakT(t *testing.T) *Kaynak {
	t.Helper()
	k := "cpmove-u/"
	kaynak, _ := arsivKaynakT(t, "arsiv-cpanel", []tUye{
		{ad: k + "cp/u", veri: "USER=u\n"},
		{ad: k + "userdata/main", veri: "main_domain: ornek.com\naddon_domains:\n  ek.net: ek.net.ornek.com\n"},
		{ad: k + "userdata/ornek.com", veri: "documentroot: /home/u/public_html\n"},
		{ad: k + "userdata/ek.net.ornek.com", veri: "documentroot: /home/u/ek\n"},
		{ad: k + "homedir/public_html/i", veri: "x"}, {ad: k + "homedir/ek/i", veri: "x"},
		{ad: k + "cron/u", veri: "MAILTO=yonetici@ornek.com\n0 2 * * * /home/u/public_html/a.sh\n0 3 * * * /home/u/ek/b.sh\n"},
	})
	m := kaynak.arsiv.M
	kaynak.arsiv.CronAtama, kaynak.arsiv.CronRed = cronAta(m.Cron, cronBaglam{EskiKullanici: m.Kullanici, AnaAlan: m.AnaAlan, Alanlar: m.Alanlar})
	return kaynak
}

// Kural: cron arşivden çevrilip hesabın deposuna (cron.Ekle) eklenir; yollar yeni ev/sk'ye yazılır;
// MAILTO eşlenir; çevrilemeyen/tehlikeli satır NEDENİYLE raporlanır (sessiz atma yok); satırlar
// ait olduğu alan adının hesabına gider; ortak redler İLK kalemin raporuna BİR kez yazılır.
func TestArsivCronAktar_RaporVeAlanBazli(t *testing.T) {
	eski := cronEkle
	t.Cleanup(func() { cronEkle = eski })
	type cagri struct {
		sk string
		g  []cron.Gorev
	}
	var cagrilar []cagri
	cronEkle = func(sk string, g []cron.Gorev) (cron.EkleSonuc, error) {
		cagrilar = append(cagrilar, cagri{sk, g})
		return cron.EkleSonuc{Eklenen: len(g)}, nil
	}
	kaynak := cronKaynakT(t)
	h := &Handlers{}
	log := func(string, ...any) {}

	s1 := &AktarSonuc{}
	h.arsivCronAktar(kaynak, "ornek.com", "c_ana", s1, log)
	if len(cagrilar) != 1 || cagrilar[0].sk != "c_ana" || len(cagrilar[0].g) != 1 {
		t.Fatalf("ana alan çağrısı: %+v", cagrilar)
	}
	g := cagrilar[0].g[0]
	if g.Komut != "php /home/c_ana/public_html/cron.php" || g.Dakika != "*/5" || g.Bildirim != "yok" {
		t.Errorf("çevrilen görev: %+v", g)
	}
	tum := strings.Join(s1.Uyarilar, "\n")
	for _, beklenen := range []string{"Cron: 1 satır taşındı", "@reboot", "sudo", "/home/baska", "birden çok alan"} {
		if !strings.Contains(tum, beklenen) {
			t.Errorf("rapor %q içermeli:\n%s", beklenen, tum)
		}
	}
	if !strings.Contains(tum, "Cron taşınamadı") {
		t.Errorf("neden metni yok:\n%s", tum)
	}
	// İkinci alan: yalnız kendi satırı; ortak redler TEKRAR yazılmaz.
	s2 := &AktarSonuc{}
	h.arsivCronAktar(kaynak, "ek.net", "c_ek", s2, log)
	if len(cagrilar) != 2 || cagrilar[1].sk != "c_ek" || len(cagrilar[1].g) != 1 || cagrilar[1].g[0].Komut != "/home/c_ek/ek/script.sh" {
		t.Fatalf("ek alan çağrısı: %+v", cagrilar)
	}
	tum2 := strings.Join(s2.Uyarilar, "\n")
	if !strings.Contains(tum2, "Cron: 1 satır taşındı") || strings.Contains(tum2, "sudo") || strings.Contains(tum2, "@reboot") || strings.Contains(tum2, "birden çok alan") {
		t.Errorf("ikinci kalem raporu:\n%s", tum2)
	}
	// Satırı olmayan alan: çağrı yok, uyarı yok.
	s3 := &AktarSonuc{}
	h.arsivCronAktar(kaynak, "yok.test", "c_yok", s3, log)
	if len(cagrilar) != 2 || len(s3.Uyarilar) != 0 {
		t.Errorf("satırsız alan: %d çağrı, %v", len(cagrilar), s3.Uyarilar)
	}
	// Hesap askıda: ekleme yapılamaz → satırlar kaybolmaz, 'taşınamadı' olarak raporlanır.
	k2 := cronKaynakT(t)
	cronEkle = func(string, []cron.Gorev) (cron.EkleSonuc, error) { return cron.EkleSonuc{}, cron.ErrAskida }
	s4 := &AktarSonuc{}
	h.arsivCronAktar(k2, "ornek.com", "c_ana", s4, log)
	if tum := strings.Join(s4.Uyarilar, "\n"); !strings.Contains(tum, "yazılamıyor") || !strings.Contains(tum, "Cron taşınamadı") {
		t.Errorf("askı raporu:\n%s", tum)
	}
	// MAILTO notu: iki kalemde de üretilir ama rapora BİR kez yazılır.
	k4 := cronMailtoKaynakT(t)
	cronEkle = func(sk string, g []cron.Gorev) (cron.EkleSonuc, error) { return cron.EkleSonuc{Eklenen: len(g)}, nil }
	r1, r2 := &AktarSonuc{}, &AktarSonuc{}
	h.arsivCronAktar(k4, "ornek.com", "c_ana", r1, log)
	h.arsivCronAktar(k4, "ek.net", "c_ek", r2, log)
	sayac := strings.Count(strings.Join(r1.Uyarilar, "\n"), "MAILTO") + strings.Count(strings.Join(r2.Uyarilar, "\n"), "MAILTO")
	if sayac != 1 {
		t.Errorf("MAILTO notu bir kez beklenir, %d kez: %q %q", sayac, r1.Uyarilar, r2.Uyarilar)
	}
	// Yinelenen satırlar idempotent raporlanır.
	k3 := cronKaynakT(t)
	cronEkle = func(sk string, g []cron.Gorev) (cron.EkleSonuc, error) { return cron.EkleSonuc{Yinelenen: len(g)}, nil }
	s5 := &AktarSonuc{}
	h.arsivCronAktar(k3, "ornek.com", "c_ana", s5, log)
	if tum := strings.Join(s5.Uyarilar, "\n"); !strings.Contains(tum, "0 satır taşındı, 1 zaten vardı") {
		t.Errorf("yinelenen raporu:\n%s", tum)
	}
}

// Kural: iş başında arşiv yeniden çözülür — kapsam (bayi yol veremez), parmak izi (dosya
// değiştiyse ret), disk önkontrolü, tür, seçilen sitelerin arşivde bulunması; seçilmeyen
// alan adına düşen cron satırları rapora (ilk kalem) taşınır.
func TestArsivIsHazirla_KapsamParmakIziVeCron(t *testing.T) {
	o := arsivKur(t)
	h := o.h
	ctx := context.Background()
	fi, _ := os.Stat(o.dosya)
	k := func(tip string) *Kaynak {
		return &Kaynak{Tip: tip, arsivGirdi: &isArsivKaynak{Yol: o.dosya,
			ParmakIzi: arsivParmakIzi{Boyut: fi.Size(), MtimeNs: fi.ModTime().UnixNano()}}}
	}
	hesap := func(a ...string) []Hesap {
		var l []Hesap
		for _, x := range a {
			l = append(l, Hesap{AlanAdi: x})
		}
		return l
	}
	kk := k("arsiv-cpanel")
	if err := h.arsivIsHazirla(ctx, Kapsam{}, kk, Ayarlar{Cron: true}, hesap("ornek.com")); err != nil {
		t.Fatal(err)
	}
	if kk.arsiv == nil || kk.arsiv.M.AnaAlan != "ornek.com" || kk.arsiv.DumpSinir != arsiv.SecenekYol().DumpSinir {
		t.Fatalf("arşiv bağlamı: %+v", kk.arsiv)
	}
	if len(kk.arsiv.CronAtama["ornek.com"]) == 0 {
		t.Errorf("cron ataması: %+v", kk.arsiv.CronAtama)
	}
	// ek.net seçilmedi: oraya düşen satır (yoksa) raporlanır; burada fikstürde ek.net'e giden satır yok → ana alan.
	for _, c := range []struct {
		ad     string
		kp     Kapsam
		kaynak *Kaynak
		sec    []Hesap
		want   error
	}{
		{"bayi yol", Kapsam{RID: 7}, k("arsiv-cpanel"), hesap("ornek.com"), errArsivYolBay},
		{"tür uyuşmaz", Kapsam{}, k("arsiv-directadmin"), hesap("ornek.com"), nil},
		{"alan arşivde yok", Kapsam{}, k("arsiv-cpanel"), hesap("yok.test"), nil},
	} {
		err := h.arsivIsHazirla(ctx, c.kp, c.kaynak, Ayarlar{}, c.sec)
		if err == nil || (c.want != nil && !errors.Is(err, c.want)) {
			t.Errorf("%s: hata beklendi, gelen %v", c.ad, err)
		}
		if c.kaynak.arsiv != nil {
			t.Errorf("%s: hata durumunda arşiv bağlamı kuruldu", c.ad)
		}
	}
	if err := h.arsivIsHazirla(ctx, Kapsam{}, &Kaynak{Tip: "arsiv-cpanel"}, Ayarlar{}, hesap("ornek.com")); err == nil {
		t.Error("arşiv girdisi olmayan iş kabul edildi")
	}
	// Disk önkontrolü iş başında da çalışır.
	eskiD := diskBosAlan
	diskBosAlan = func(string) (int64, error) { return 1, nil }
	if err := h.arsivIsHazirla(ctx, Kapsam{}, k("arsiv-cpanel"), Ayarlar{}, hesap("ornek.com")); !errors.Is(err, errArsivDisk) {
		t.Errorf("disk: %v", err)
	}
	diskBosAlan = eskiD
	// Arşiv AÇILIRKEN dosya değişirse (okuma sırasında) iş yine reddedilir.
	eskiAc := arsivAc
	arsivAc = func(ctx context.Context, yol string, s arsiv.Secenek) (*arsiv.Arsiv, *arsiv.Manifest, error) {
		g := time.Now().Add(9 * time.Hour)
		_ = os.Chtimes(yol, g, g)
		return eskiAc(ctx, yol, s)
	}
	if err := h.arsivIsHazirla(ctx, Kapsam{}, k("arsiv-cpanel"), Ayarlar{}, hesap("ornek.com")); !errors.Is(err, errArsivDegisti) {
		t.Errorf("okuma sırasında değişen dosya: %v", err)
	}
	arsivAc = eskiAc
	_ = os.Chtimes(o.dosya, time.Unix(0, fi.ModTime().UnixNano()), time.Unix(0, fi.ModTime().UnixNano()))
	// Dosya keşiften sonra değişti.
	gelecek := time.Now().Add(2 * time.Hour)
	_ = os.Chtimes(o.dosya, gelecek, gelecek)
	arsivAc = func(context.Context, string, arsiv.Secenek) (*arsiv.Arsiv, *arsiv.Manifest, error) {
		t.Error("dosya değişmişken arşiv AÇILMAMALI (saatler süren okuma boşa gider)")
		return nil, nil, errors.New("açılmamalıydı")
	}
	if err := h.arsivIsHazirla(ctx, Kapsam{}, k("arsiv-cpanel"), Ayarlar{}, hesap("ornek.com")); !errors.Is(err, errArsivDegisti) {
		t.Errorf("parmak izi: %v", err)
	}
	arsivAc = eskiAc
	// Cron: seçilmeyen alana düşen satırlar ortak redlere eklenir (ek.net'e ait satırlı arşiv).
	ek := filepath.Join(o.kok, "ek.tar.gz")
	m := "cpmove-u/"
	_ = os.WriteFile(ek, tarGz(t, []tUye{
		{ad: m + "cp/u", veri: "USER=u\n"},
		{ad: m + "userdata/main", veri: "main_domain: ornek.com\naddon_domains:\n  ek.net: ek.net.ornek.com\n"},
		{ad: m + "userdata/ornek.com", veri: "documentroot: /home/u/public_html\n"},
		{ad: m + "userdata/ek.net.ornek.com", veri: "documentroot: /home/u/ek\n"},
		{ad: m + "homedir/public_html/i", veri: "x"}, {ad: m + "homedir/ek/i", veri: "x"},
		{ad: m + "cron/u", veri: "0 1 * * * /home/u/ek/script.sh\n"},
	}), 0o644)
	fi2, _ := os.Stat(ek)
	k2 := &Kaynak{Tip: "arsiv-cpanel", arsivGirdi: &isArsivKaynak{Yol: ek, ParmakIzi: arsivParmakIzi{Boyut: fi2.Size(), MtimeNs: fi2.ModTime().UnixNano()}}}
	if err := h.arsivIsHazirla(ctx, Kapsam{}, k2, Ayarlar{Cron: true}, hesap("ornek.com")); err != nil {
		t.Fatal(err)
	}
	if len(k2.arsiv.CronAtama) != 0 || len(k2.arsiv.CronRed) != 1 || !strings.Contains(k2.arsiv.CronRed[0].Neden, "seçilmedi") {
		t.Errorf("seçilmeyen alanın cron'u raporlanmalı: %+v %+v", k2.arsiv.CronAtama, k2.arsiv.CronRed)
	}
	// ek.net seçiliyse atama onda kalır.
	k3 := &Kaynak{Tip: "arsiv-cpanel", arsivGirdi: k2.arsivGirdi}
	if err := h.arsivIsHazirla(ctx, Kapsam{}, k3, Ayarlar{Cron: true}, hesap("ornek.com", "ek.net")); err != nil {
		t.Fatal(err)
	}
	if len(k3.arsiv.CronAtama["ek.net"]) != 1 || len(k3.arsiv.CronRed) != 0 {
		t.Errorf("atama: %+v %+v", k3.arsiv.CronAtama, k3.arsiv.CronRed)
	}
	// Cron kapalıysa atama yapılmaz.
	k4 := &Kaynak{Tip: "arsiv-cpanel", arsivGirdi: k2.arsivGirdi}
	if err := h.arsivIsHazirla(ctx, Kapsam{}, k4, Ayarlar{}, hesap("ornek.com", "ek.net")); err != nil || k4.arsiv.CronAtama != nil {
		t.Errorf("cron kapalı: %v %+v", err, k4.arsiv.CronAtama)
	}
}

// Kural (GL-02 ile aynı): arşiv kaynağı da ad-alanı denetiminden geçer — domains.AdAlaniDenetle
// hesap oluşturulmadan ÖNCE, bayi kapsamıyla; başka kiracının alt/üst adı sağlamaya varmadan reddedilir.
func TestHesapAktar_ArsivKaynagi_AdAlaniSaglamadanOnce(t *testing.T) {
	db := ta_db(t)
	saglanan, _, gercek := ta_kalkan(t, nil)
	h := &Handlers{DB: db}
	// arsiv bağlamı BOŞ: dosya/DB/posta adımına ulaşan akış nil işaretçide patlardı (tripwire).
	k := &Kaynak{Tip: "arsiv-cpanel", arsiv: &arsivBaglam{}}
	for _, c := range []struct {
		alan string
		kp   Kapsam
	}{{"shop.kokdomain.test", Kapsam{RID: 7}}, {"webmail.kokdomain.test", Kapsam{RID: 7}}} {
		_, err := h.HesapAktar(context.Background(), k, Hesap{AlanAdi: c.alan}, Ayarlar{}, c.kp, func(string, ...any) {})
		if err == nil || strings.Contains(err.Error(), "kokdomain") || strings.Contains(err.Error(), "c_kokdomain") {
			t.Errorf("%s: bayiye ayrıntısız ret beklenir: %v", c.alan, err)
		}
	}
	if len(*saglanan) != 0 || *gercek != 0 {
		t.Errorf("ret sağlamadan önce olmalı: sahte=%v gerçek=%d", *saglanan, *gercek)
	}
}

// Kural: iş yürütücüsü arşiv kaynağı için ağ doğrulaması YAPMAZ, arşivi bir kez açar ve her kalemde
// k.arsiv'i sağlar; tamamen başarılı işin yüklemesi silinir, hatalı işinki yeniden deneme için kalır;
// dosya değiştiyse/kaynak kapatıldıysa iş başlamadan düşer.
func TestArsivIsYurut_AcilisTemizlikVeRet(t *testing.T) {
	o := arsivKur(t)
	o.bayiAc(t, true)
	o.ist(t, "kok", http.MethodPut, "/tasima/kaynaklar/arsiv-cpanel", map[string]any{"bayi_etkin": true})
	eskiA := hesapAktarici
	t.Cleanup(func() { hesapAktarici = eskiA })
	type goruldu struct {
		alan     string
		arsivVar bool
		rid      int64
	}
	var gor []goruldu
	var hataVer bool
	hesapAktarici = func(h *Handlers, ctx context.Context, k *Kaynak, hs Hesap, ay Ayarlar, kp Kapsam, log func(string, ...any)) (*AktarSonuc, error) {
		gor = append(gor, goruldu{hs.AlanAdi, k.arsiv != nil && k.arsiv.A != nil && k.arsiv.M != nil, kp.RID})
		if hataVer {
			return nil, errors.New("yapay hata")
		}
		return &AktarSonuc{DomainID: 1}, nil
	}
	hazirla := func() (isID, yid int64) {
		hizSifirla()
		o.exec(t, `DELETE FROM tasima_yuklemeleri WHERE sahip_rid=7`) // kalan (yeniden deneme) yüklemeleri kota doldurmasın
		yid = o.yukle(t, "bayi7", cpmoveT(t))
		oturum, y := o.kesfet(t, "bayi7", map[string]any{"tip": "arsiv-cpanel", "yukleme_id": yid})
		if y["durum"] != "hazir" {
			t.Fatalf("keşif: %+v", y)
		}
		hizSifirla()
		kod, g := o.ist(t, "bayi7", http.MethodPost, "/tasima/arsiv/baslat", map[string]any{
			"tip": "arsiv-cpanel", "mod": "toplu", "oturum_id": oturum,
			"secilen": []map[string]any{{"alan_adi": "ornek.com"}, {"alan_adi": "ek.net"}},
			"ayarlar": map[string]any{"dosyalar": true, "plan_id": 3},
		})
		if kod != 202 {
			t.Fatalf("başlatma: %d %s", kod, g)
		}
		return jsonSayi(t, g, "is_id"), yid
	}
	durum := func(isID int64) (d, hata string) {
		var h sql.NullString
		_ = o.db.QueryRow(`SELECT durum, hata FROM tasima_isleri WHERE id=?`, isID).Scan(&d, &h)
		return d, h.String
	}
	var varMi = func(yid int64) bool {
		var n int
		_ = o.db.QueryRow(`SELECT COUNT(*) FROM tasima_yuklemeleri WHERE id=?`, yid).Scan(&n)
		_, err := os.Lstat(yuklemeYolu(7, yid))
		return n == 1 && err == nil
	}
	calistir := func(isID int64) {
		o.exec(t, `UPDATE tasima_isleri SET durum='calisiyor' WHERE id=?`, isID)
		o.h.isYurut(context.Background(), isID)
	}

	// 1) Başarı: iki kalem, arşiv sağlandı, bayi kapsamı, yükleme silindi.
	isID, yid := hazirla()
	calistir(isID)
	if d, h := durum(isID); d != "tamam" {
		t.Fatalf("iş durumu %s (%s)", d, h)
	}
	if len(gor) != 2 || !gor[0].arsivVar || !gor[1].arsivVar || gor[0].rid != 7 {
		t.Errorf("kalemler: %+v", gor)
	}
	if varMi(yid) {
		t.Error("tamamen başarılı işin yüklemesi silinmedi")
	}
	var parola sql.NullString
	_ = o.db.QueryRow(`SELECT kaynak_parola FROM tasima_isleri WHERE id=?`, isID).Scan(&parola)

	// 2) Hata: yükleme yeniden deneme için KALIR.
	gor, hataVer = nil, true
	isID, yid = hazirla()
	calistir(isID)
	if d, _ := durum(isID); d != "hata" {
		t.Errorf("hatalı iş durumu: %s", d)
	}
	if !varMi(yid) {
		t.Error("hatalı işin yüklemesi silinmemeliydi")
	}
	hataVer = false

	// 2b) Kısmi hata (bir kalem düştü, iş 'tamam'): yükleme yine KALIR (yalnız tam başarıda silinir).
	o.exec(t, `UPDATE tasima_isleri SET durum='tamam' WHERE sahip_rid=7`)
	gor = nil
	kismi := false
	hesapAktarici = func(h *Handlers, ctx context.Context, k *Kaynak, hs Hesap, ay Ayarlar, kp Kapsam, log func(string, ...any)) (*AktarSonuc, error) {
		gor = append(gor, goruldu{hs.AlanAdi, k.arsiv != nil, kp.RID})
		if kismi && hs.AlanAdi == "ek.net" {
			return nil, errors.New("yapay kısmi hata")
		}
		return &AktarSonuc{DomainID: 1}, nil
	}
	kismi = true
	isID, yid = hazirla()
	calistir(isID)
	if d, _ := durum(isID); d != "tamam" || !varMi(yid) {
		t.Errorf("kısmi hata: iş=%s yükleme kaldı=%v", d, varMi(yid))
	}
	kismi = false

	// 3) Dosya keşiften sonra değişti → iş başlamadan 'hata' (yürütücü HİÇ çağrılmadı).
	o.exec(t, `UPDATE tasima_isleri SET durum='tamam' WHERE sahip_rid=7`)
	gor = nil
	isID, yid = hazirla()
	gelecek := time.Now().Add(3 * time.Hour)
	_ = os.Chtimes(yuklemeYolu(7, yid), gelecek, gelecek)
	calistir(isID)
	if d, h := durum(isID); d != "hata" || !strings.Contains(h, "değişti") || len(gor) != 0 {
		t.Errorf("değişen dosya: %s %q kalem=%d", d, h, len(gor))
	}
	// 3b) Arşivde olmayan alan (satır elle bozuldu): bayiye GENEL metin (ayrıntı yok), kök işi 'hata'.
	o.exec(t, `UPDATE tasima_isleri SET durum='tamam' WHERE sahip_rid=7`)
	gor = nil
	isID, _ = hazirla()
	o.exec(t, `UPDATE tasima_kalemleri SET alan_adi='yok.test' WHERE is_id=? AND alan_adi='ek.net'`, isID)
	calistir(isID)
	if d, h := durum(isID); d != "hata" || h != tasimaHataGenel || len(gor) != 0 {
		t.Errorf("bayi için bilinmeyen hata genel metin olmalı: %s %q kalem=%d", d, h, len(gor))
	}
	// 4) Kök kaynağı kapattı → sıradaki iş başlamadan iptal.
	o.exec(t, `UPDATE tasima_isleri SET durum='tamam' WHERE sahip_rid=7`)
	isID, _ = hazirla()
	o.exec(t, `UPDATE tasima_kaynak_ayar SET bayi_etkin=0 WHERE kod='arsiv-cpanel'`)
	calistir(isID)
	if d, _ := durum(isID); d != "iptal" || len(gor) != 0 {
		t.Errorf("kapatılan kaynak: %s kalem=%d", d, len(gor))
	}
}

// Kural: süresi dolan yüklemeler (yarım 24 sa, hazır 72 sa) ve kayıtsız artık dosyalar silinir;
// çalışan/sıradaki işe bağlı yükleme ve yeni kayıtlar KALIR.
func TestArsivYuklemeTemizle_SureVeBagliIs(t *testing.T) {
	o := arsivKur(t)
	ekle := func(rid int64, durum, yas string) int64 {
		res := o.exec(t, `INSERT INTO tasima_yuklemeleri (sahip_rid, ad, boyut, alinan, durum) VALUES (?, 'x', 600, 600, ?)`, rid, durum)
		id, _ := res.LastInsertId()
		o.exec(t, `UPDATE tasima_yuklemeleri SET guncelleme=(NOW() - INTERVAL `+yas+`) WHERE id=?`, id)
		_ = os.MkdirAll(filepath.Dir(yuklemeYolu(rid, id)), 0o700)
		_ = os.WriteFile(yuklemeYolu(rid, id), []byte("x"), 0o600)
		return id
	}
	eskiYarim := ekle(7, "yukleniyor", "25 HOUR")
	yeniYarim := ekle(7, "yukleniyor", "23 HOUR")
	eskiHazir := ekle(0, "hazir", "73 HOUR")
	yeniHazir := ekle(0, "hazir", "71 HOUR")
	bagliEski := ekle(9, "hazir", "100 HOUR")
	o.exec(t, `INSERT INTO tasima_isleri (kaynak_tip, kaynak_host, durum, toplam, sahip_rid, baslatan, arsiv_yukleme_id)
		VALUES ('arsiv-cpanel','arsiv','calisiyor',1,9,'x',?)`, bagliEski)
	// Kayıtsız artık dosya (eski) ve yeni artık dosya.
	artik := yuklemeYolu(7, 9999)
	_ = os.WriteFile(artik, []byte("x"), 0o600)
	g := time.Now().Add(-30 * time.Hour)
	_ = os.Chtimes(artik, g, g)
	yeniArtik := yuklemeYolu(7, 9998)
	_ = os.WriteFile(yeniArtik, []byte("x"), 0o600)
	o.h.arsivYuklemeTemizle()
	for ad, c := range map[string]struct {
		id    int64
		rid   int64
		kalir bool
	}{"eski yarım": {eskiYarim, 7, false}, "yeni yarım": {yeniYarim, 7, true}, "eski hazır": {eskiHazir, 0, false},
		"yeni hazır": {yeniHazir, 0, true}, "işe bağlı eski": {bagliEski, 9, true}} {
		var n int
		_ = o.db.QueryRow(`SELECT COUNT(*) FROM tasima_yuklemeleri WHERE id=?`, c.id).Scan(&n)
		_, err := os.Lstat(yuklemeYolu(c.rid, c.id))
		if (n == 1) != c.kalir || (err == nil) != c.kalir {
			t.Errorf("%s: kalmalı=%v kayıt=%d dosya=%v", ad, c.kalir, n, err == nil)
		}
	}
	if _, err := os.Lstat(artik); err == nil {
		t.Error("eski kayıtsız artık dosya silinmedi")
	}
	if _, err := os.Lstat(yeniArtik); err != nil {
		t.Error("yeni artık dosyaya dokunulmamalı (yarışan yükleme olabilir)")
	}
}

// Kural: DirectAdmin'in iç içe <kutu>/Maildir yerleşimi kutu başına çıkarılır (alan dizini tek
// geçişinde değil) ve yine bağsız/süzgeçlidir.
func TestArsivMailAktar_DirectAdminIcIceMaildir(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("runuser/chown için root gerekli")
	}
	if _, err := user.Lookup("nobody"); err != nil {
		t.Skip("nobody yok")
	}
	db := ta_db(t)
	ek := sahteMailEklenti(t)
	var spSutun []string
	for _, a := range efektiflimit.Alanlar {
		spSutun = append(spSutun, "`"+a.Sutun+"` INT NOT NULL DEFAULT 0")
	}
	for _, q := range []string{
		`ALTER TABLE domains ADD COLUMN plan_id BIGINT UNSIGNED NULL, ADD COLUMN disk_kota_mb INT NOT NULL DEFAULT 0, ADD COLUMN inode_kota INT NOT NULL DEFAULT 0`,
		"CREATE TABLE service_plans (id BIGINT UNSIGNED PRIMARY KEY, ad VARCHAR(64) NOT NULL DEFAULT '', " +
			"reseller_id BIGINT NOT NULL DEFAULT 0, domain_id BIGINT UNSIGNED NULL, " + strings.Join(spSutun, ", ") + ")",
		efektiflimit.TabloDDL,
		`CREATE TABLE cp_eklentiler (ad VARCHAR(64) PRIMARY KEY, aktif TINYINT NOT NULL DEFAULT 0, soket VARCHAR(255) NOT NULL DEFAULT '')`,
		`INSERT INTO domains (id, alan_adi, reseller_id) VALUES (31,'ornek.com',0)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("şema: %v\n%s", err, q)
		}
	}
	_, _ = db.Exec(`INSERT INTO cp_eklentiler VALUES ('mail',1,?)`, ek.soket)
	base := t.TempDir()
	for _, d := range []string{filepath.Dir(base), base} {
		_ = os.Chmod(d, 0o711)
	}
	kok := filepath.Join(base, "vmail")
	_ = os.Mkdir(kok, 0o755)
	eskiV, eskiK := vmailKok, mailVmailKul
	vmailKok, mailVmailKul = kok, "nobody"
	t.Cleanup(func() { vmailKok, mailVmailKul = eskiV, eskiK })
	kaynak, _ := arsivKaynakT(t, "arsiv-directadmin", []tUye{
		{ad: "backup/user.conf", veri: "username=da1\ndomain=ornek.com\n"},
		{ad: "domains/ornek.com/public_html/i", veri: "x"},
		{ad: "backup/ornek.com/email/passwd", veri: "info:" + tHash + "\n"},
		{ad: "imap/ornek.com/info/Maildir/cur/1", veri: "ic-ice"},
		{ad: "imap/ornek.com/info/Maildir/dovecot-uidlist", veri: "x"},
		{ad: "imap/ornek.com/info/Maildir/bag", tur: tar.TypeSymlink, hedef: "cur"},
	})
	h := &Handlers{DB: db}
	n, uyar, err := h.arsivMailAktar(context.Background(), kaynak, Hesap{AlanAdi: "ornek.com"}, func(string, ...any) {})
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v %q", n, err, uyar)
	}
	if b, _ := os.ReadFile(filepath.Join(kok, "ornek.com", "info", "cur", "1")); string(b) != "ic-ice" {
		t.Errorf("iç içe Maildir kutu köküne açılmalı: %q (%q)", b, uyar)
	}
	for _, yok := range []string{"info/Maildir", "info/dovecot-uidlist", "info/bag"} {
		if _, err := os.Lstat(filepath.Join(kok, "ornek.com", yok)); err == nil {
			t.Errorf("%s çıkmamalıydı", yok)
		}
	}
}

// Kural: açılışta (AcilistaTemizle) süresi dolan yüklemeler silinir.
func TestArsivYuklemeTemizle_AcilistaCalisir(t *testing.T) {
	o := arsivKur(t)
	eskiG := acilisDagitGecikme
	acilisDagitGecikme = 0
	t.Cleanup(func() { acilisDagitGecikme = eskiG })
	res := o.exec(t, `INSERT INTO tasima_yuklemeleri (sahip_rid, ad, boyut, alinan, durum) VALUES (7, 'x', 600, 600, 'hazir')`)
	id, _ := res.LastInsertId()
	o.exec(t, `UPDATE tasima_yuklemeleri SET guncelleme=(NOW() - INTERVAL 100 HOUR) WHERE id=?`, id)
	_ = os.MkdirAll(filepath.Dir(yuklemeYolu(7, id)), 0o700)
	_ = os.WriteFile(yuklemeYolu(7, id), []byte("x"), 0o600)
	o.h.AcilistaTemizle()
	if _, err := os.Lstat(yuklemeYolu(7, id)); err == nil {
		t.Error("açılışta süresi dolan yükleme silinmedi")
	}
}
