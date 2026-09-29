package hesaplar

// artikyetki_test.go — DB önek çakışmasının İKİNCİ TUR bulguları:
//
//  1. ARTIK GRANT: DROP DATABASE yetki satırını silmez; MySQLDropDBKeepUser REVOKE
//     yapmıyordu. Kısa önekli kiracı komşunun öngörülebilir adını kendi ana kullanıcısıyla
//     açıp (yedekten "yeni ada geri yükle") silince mysql.db satırı kalıyor, komşu o adla
//     DB açınca saldırganın kullanıcısı komşunun DB'sinde ALL yetkiye sahip oluyordu.
//  2. DROP USER KAPISI: DeleteDatabase/wpDBDusur satırdaki kullanıcıyı (kiracının
//     yazabildiği wp-config'ten kurtarılmış olabilir: komşunun hesabı, servis hesabı)
//     koşulsuz DROP USER ediyordu.
//  3. BÜYÜK/KÜÇÜK HARF: db_accounts utf8mb4_unicode_ci; MariaDB hesap/DB adları harf
//     duyarlı. Saldırganın satırındaki "<ON>_X_DB" komşunun "<on>_x_db"sini kilitliyordu.
//
// Harness: dbonek_test.go (dbo_ortam — geçici MariaDB + mysql sarmalayıcısı).

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"girginospanel/internal/gizli"
)

// satirVar: mysql.<tablo>'da Db=ad (kaçışlı/kaçışsız) satırı olan kullanıcı sayısı.
func (o *dboOrtam) yetkiSatiri(t *testing.T, tablo, kul, ad string) int {
	t.Helper()
	var n int
	if err := o.kok.QueryRow(`SELECT COUNT(*) FROM mysql.`+tablo+` WHERE User=? AND (Db=? OR Db=?)`,
		kul, ad, GrantDBKac(ad)).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// 🔴 BULGU 1 zinciri: önceden kapma → KeepUser silme → komşu aynı adı açar.
func TestArtikYetki_KeepUserSilmeKomsununYeniDBsineUzanmaz(t *testing.T) {
	o := dbo_ortam(t)
	on := o.on
	spy, spyPw := on+"_spy", "SpyParola12345"
	if err := MySQLCreateDB(o.panel, 1, on+"_ana", spy, spyPw); err != nil {
		t.Fatal(err)
	}
	// Komşunun (domain 2, c_<on>_x) öngörülebilir uygulama DB adı, saldırganın ana
	// kullanıcısına bağlanarak açılır (yedekten yeni ada geri yüklemenin eski davranışı).
	hedef := "c_" + on + "_x_joomla_2"
	t.Cleanup(func() {
		_, _ = o.kok.Exec("DROP DATABASE IF EXISTS `" + hedef + "`")
		_, _ = o.kok.Exec("DROP USER IF EXISTS '" + hedef + "'@'localhost'")
	})
	if err := MySQLCreateDBForUser(o.panel, 1, hedef, spy); err != nil {
		t.Fatal(err)
	}
	if !o.baglanir(spy, spyPw, hedef) {
		t.Fatal("pozitif kontrol: saldırgan kendi bağladığı DB'ye bağlanamıyor")
	}
	// DB silinir: kullanıcı başka satırda da geçtiği için korunur (mevcut-kullanıcı modu).
	if err := MySQLDropDBSahipli(o.panel, 1, hedef, spy); err != nil {
		t.Fatal(err)
	}
	if !o.kullaniciVar(t, spy) || !o.baglanir(spy, spyPw, on+"_ana") {
		t.Fatal("paylaşılan kullanıcı ya da diğer DB'si bozuldu")
	}
	if n := o.yetkiSatiri(t, "db", spy, hedef); n != 0 {
		t.Errorf("KeepUser silmesinden sonra %q için artık mysql.db satırı kaldı (%d)", hedef, n)
	}
	// Komşu aynı adı meşru olarak açar.
	kurbanPw := "KurbanParola123"
	if err := MySQLCreateDB(o.panel, 2, hedef, hedef, kurbanPw); err != nil {
		t.Fatalf("komşunun meşru oluşturması düştü: %v", err)
	}
	if o.baglanir(spy, spyPw, hedef) {
		t.Errorf("ARTIK GRANT: saldırganın kullanıcısı komşunun yeni DB'sine bağlanabiliyor")
	}
	if !o.baglanir(hedef, kurbanPw, hedef) {
		t.Errorf("komşu kendi DB'sine bağlanamıyor")
	}
}

// Eski sürümlerden kalmış satırlar (kaçışlı, kaçışsız, tablo ve sütun düzeyi) yeni
// oluşturulan DB'de silinmeli; DB'nin yeni sahibi etkilenmemeli.
func TestArtikYetki_YeniDBEskiSatirlariSiler(t *testing.T) {
	o := dbo_ortam(t)
	on := o.on
	ad := on + "_x_main"
	s1, s2, s3 := on+"_s1", on+"_s2", on+"_s3"
	for _, k := range []string{s1, s2, s3} {
		o.kokExec(t, "CREATE USER '"+k+"'@'localhost' IDENTIFIED BY 'SpyParola12345'")
	}
	o.kokExec(t, "CREATE DATABASE `"+ad+"`")
	o.kokExec(t, "CREATE TABLE `"+ad+"`.`t` (id INT, gizli INT)")
	o.kokExec(t, "GRANT SELECT ON `"+ad+"`.`t` TO '"+s3+"'@'localhost'")
	o.kokExec(t, "GRANT SELECT (gizli) ON `"+ad+"`.`t` TO '"+s3+"'@'localhost'")
	o.kokExec(t, "DROP DATABASE `"+ad+"`")
	o.kokExec(t, "GRANT ALL PRIVILEGES ON `"+GrantDBKac(ad)+"`.* TO '"+s1+"'@'localhost'") // kaçışlı
	o.kokExec(t, "GRANT ALL PRIVILEGES ON `"+ad+"`.* TO '"+s2+"'@'localhost'")             // kaçışsız (eski)
	if o.yetkiSatiri(t, "db", s1, ad)+o.yetkiSatiri(t, "db", s2, ad) != 2 || o.yetkiSatiri(t, "tables_priv", s3, ad) != 1 ||
		o.yetkiSatiri(t, "columns_priv", s3, ad) != 1 {
		t.Fatal("pozitif kontrol: artık satırlar kurulamadı")
	}
	kul, pw := on+"_x_db", "KurbanParola123"
	if err := MySQLCreateDB(o.panel, 2, ad, kul, pw); err != nil {
		t.Fatalf("meşru oluşturma düştü: %v", err)
	}
	o.kokExec(t, "CREATE TABLE `"+ad+"`.`t` (id INT, gizli INT)")
	for _, c := range []struct{ tablo, kul string }{{"db", s1}, {"db", s2}, {"tables_priv", s3}, {"columns_priv", s3}} {
		if n := o.yetkiSatiri(t, c.tablo, c.kul, ad); n != 0 {
			t.Errorf("yeni DB'de %s artık satırı kaldı: %s (%d)", c.tablo, c.kul, n)
		}
	}
	for _, k := range []string{s1, s2} {
		if o.baglanir(k, "SpyParola12345", ad) {
			t.Errorf("artık yetkili %s yeni DB'ye bağlanabiliyor", k)
		}
	}
	if !o.baglanir(kul, pw, ad) || o.yetkiSatiri(t, "db", kul, ad) != 1 {
		t.Errorf("yeni sahip kendi DB'sine bağlanamıyor / GRANT'i yok")
	}
	// MySQLCreateDBForUser da (yeni ad) temizler.
	ad2 := on + "_x_ek"
	o.kokExec(t, "GRANT ALL PRIVILEGES ON `"+GrantDBKac(ad2)+"`.* TO '"+s1+"'@'localhost'")
	if err := MySQLCreateDBForUser(o.panel, 2, ad2, kul); err != nil {
		t.Fatalf("meşru bağlama düştü: %v", err)
	}
	if o.yetkiSatiri(t, "db", s1, ad2) != 0 || o.baglanir(s1, "SpyParola12345", ad2) {
		t.Errorf("MySQLCreateDBForUser yeni DB'de artık yetkiyi bıraktı")
	}
}

// Jokerle uzanan (kaçışsız '_' ile BAŞKA ad için yazılmış) eski desen: silmek o adın
// sahibini bozar, bırakmak sızdırır → oluşturma fail-closed; hiçbir şey kalmaz. '%'
// desenleri (yöneticinin bilinçli geniş yetkisi) engellemez.
func TestArtikYetki_JokerliEskiDesen(t *testing.T) {
	o := dbo_ortam(t)
	on := o.on
	spy := on + "_spy"
	o.kokExec(t, "CREATE USER '"+spy+"'@'localhost' IDENTIFIED BY 'SpyParola12345'")
	o.kokExec(t, "GRANT ALL PRIVILEGES ON `"+on+"_x_mai_`.* TO '"+spy+"'@'localhost'")
	ad, kul := on+"_x_main", on+"_x_db"
	if err := MySQLCreateDB(o.panel, 2, ad, kul, "KurbanParola123"); err == nil {
		t.Errorf("jokerle uzanan eski desen varken oluşturma BAŞARILI oldu")
	}
	if o.dbVar(t, ad) || o.kullaniciVar(t, kul) {
		t.Errorf("fail-closed oluşturma DB/kullanıcı bıraktı")
	}
	if n := o.satirSayisi(t, `SELECT COUNT(*) FROM db_accounts`); n != 0 {
		t.Errorf("fail-closed oluşturma db_accounts'a %d satır yazdı", n)
	}
	if o.yetkiSatiri(t, "db", spy, on+"_x_mai_") != 1 {
		t.Errorf("başka ada ait desen silindi")
	}
	// '%' deseni engellemez ve korunur.
	izle := on + "_izle"
	o.kokExec(t, "CREATE USER '"+izle+"'@'localhost' IDENTIFIED BY 'IzleParola12345'")
	o.kokExec(t, "GRANT SELECT ON `"+GrantDBKac(on+"_y")+"%`.* TO '"+izle+"'@'localhost'")
	if err := MySQLCreateDB(o.panel, 1, on+"_yy", on+"_yyu", "YyParola12345"); err != nil {
		t.Errorf("'%%' deseni oluşturmayı engelledi: %v", err)
	}
	var n int
	if err := o.kok.QueryRow(`SELECT COUNT(*) FROM mysql.db WHERE User=?`, izle).Scan(&n); err != nil || n != 1 {
		t.Errorf("yöneticinin '%%' deseni silindi (%d, %v)", n, err)
	}
}

// 🔴 BULGU 3 (DROP USER kapısı): MySQLDropDBSahipli.
func TestMySQLDropDBSahipli(t *testing.T) {
	o := dbo_ortam(t)
	on := o.on
	kurbanPw := "KurbanParola123"
	kurban, kurbanDB := o.kurbanKur(t, kurbanPw)

	// (a) Servis hesabı benzeri: kendi (panelde kayıtsız, var olan) şemasında yetkili hesap
	// saldırganın satırına yazılmış (yedekten kurtarılan wp-config).
	svc, svcPw := on+"svc", "SvcParola12345"
	o.kokExec(t, "CREATE USER '"+svc+"'@'localhost' IDENTIFIED BY '"+svcPw+"'")
	o.kokExec(t, "CREATE DATABASE `"+on+"svcsema`")
	o.kokExec(t, "GRANT ALL PRIVILEGES ON `"+on+"svcsema`.* TO '"+svc+"'@'localhost'")
	a := on + "_a"
	o.kokExec(t, "CREATE DATABASE `"+a+"`")
	o.kokExec(t, "GRANT ALL PRIVILEGES ON `"+GrantDBKac(a)+"`.* TO '"+svc+"'@'localhost'")
	if _, err := o.panel.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (1,?,?,'x')`, a, svc); err != nil {
		t.Fatal(err)
	}
	if err := MySQLDropDBSahipli(o.panel, 1, a, svc); err != nil {
		t.Fatal(err)
	}
	if !o.kullaniciVar(t, svc) || !o.baglanir(svc, svcPw, on+"svcsema") {
		t.Errorf("servis hesabı DROP USER ile düşürüldü")
	}
	if o.dbVar(t, a) || o.yetkiSatiri(t, "db", svc, a) != 0 {
		t.Errorf("DB ya da ona bağlı yetki kaldı")
	}

	// (b) Komşunun (domain 2'ye kayıtlı) kullanıcısı saldırganın satırında.
	b := on + "_b"
	o.kokExec(t, "CREATE DATABASE `"+b+"`")
	if _, err := o.panel.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (1,?,?,'x')`, b, kurban); err != nil {
		t.Fatal(err)
	}
	if err := MySQLDropDBSahipli(o.panel, 1, b, kurban); err != nil {
		t.Fatal(err)
	}
	if !o.baglanir(kurban, kurbanPw, kurbanDB) {
		t.Errorf("komşunun kullanıcısı düşürüldü")
	}

	// (c) Panelde hiç kayıtlı olmayan, yalnız USAGE'li yabancı hesap satıra yazılmış.
	yab := on + "_yab"
	o.kokExec(t, "CREATE USER '"+yab+"'@'localhost' IDENTIFIED BY 'YabParola12345'")
	c := on + "_c"
	o.kokExec(t, "CREATE DATABASE `"+c+"`")
	o.kokExec(t, "GRANT SELECT ON *.* TO '"+yab+"'@'localhost'") // izleme hesabı gibi
	if _, err := o.panel.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (1,?,?,'x')`, c, yab); err != nil {
		t.Fatal(err)
	}
	if err := MySQLDropDBSahipli(o.panel, 1, c, yab); err != nil {
		t.Fatal(err)
	}
	if !o.kullaniciVar(t, yab) {
		t.Errorf("sunucu geneli yetkili hesap düşürüldü")
	}

	// (d) Bu domaine kayıtlı OLMAYAN DB: hiçbir şey düşmez.
	if err := MySQLDropDBSahipli(o.panel, 1, kurbanDB, kurban); err == nil {
		t.Errorf("komşunun DB'si için düşürme hata vermedi")
	}
	if !o.dbVar(t, kurbanDB) || !o.baglanir(kurban, kurbanPw, kurbanDB) {
		t.Errorf("başka domaine kayıtlı DB/kullanıcı düşürüldü")
	}

	// (e) Meşru: paylaşılan kullanıcı son DB'sinde, tekil kullanıcı hemen düşer.
	du, duPw := on+"_du", "DuParola12345"
	if err := MySQLCreateDB(o.panel, 1, on+"_d1", du, duPw); err != nil {
		t.Fatal(err)
	}
	if err := MySQLCreateDBForUser(o.panel, 1, on+"_d2", du); err != nil {
		t.Fatal(err)
	}
	if err := MySQLDropDBSahipli(o.panel, 1, on+"_d1", du); err != nil {
		t.Fatal(err)
	}
	if !o.baglanir(du, duPw, on+"_d2") || o.yetkiSatiri(t, "db", du, on+"_d1") != 0 {
		t.Errorf("paylaşılan kullanıcı: diğer DB bozuldu ya da silinen DB'nin yetkisi kaldı")
	}
	// Başka bir hesabın d2'deki yetkisi de DROP ile gitmeli (MySQLDropDB yolu).
	o.kokExec(t, "GRANT SELECT ON `"+GrantDBKac(on+"_d2")+"`.* TO '"+yab+"'@'localhost'")
	if err := MySQLDropDBSahipli(o.panel, 1, on+"_d2", du); err != nil {
		t.Fatal(err)
	}
	if o.kullaniciVar(t, du) || o.dbVar(t, on+"_d2") {
		t.Errorf("son DB'si silinen meşru kullanıcı/DB kaldı")
	}
	if o.yetkiSatiri(t, "db", yab, on+"_d2") != 0 {
		t.Errorf("düşürülen DB'de başka hesabın artık yetkisi kaldı")
	}
	if n := o.satirSayisi(t, `SELECT COUNT(*) FROM db_accounts WHERE domain_id=1`); n != 0 {
		t.Errorf("silinen DB'lerin satırları kaldı (%d)", n)
	}
}

// 🔴 Büyük/küçük harf varyantı: saldırganın satırındaki "<ON>_X_DB" (ayrı MariaDB hesabı)
// komşunun "<on>_x_db"si ile eşleşmemeli.
func TestHarfVaryanti_KomsuyuKilitlemezParolasiniEzmez(t *testing.T) {
	o := dbo_ortam(t)
	on := o.on
	kurbanPw := "KurbanParola123"
	kurban, kurbanDB := o.kurbanKur(t, kurbanPw)
	buyuk := strings.ToUpper(kurban)
	v1 := on + "_v1"
	o.kokExec(t, "CREATE USER '"+buyuk+"'@'localhost' IDENTIFIED BY 'BuyukParola123'")
	o.kokExec(t, "CREATE DATABASE `"+v1+"`")
	o.kokExec(t, "GRANT ALL PRIVILEGES ON `"+GrantDBKac(v1)+"`.* TO '"+buyuk+"'@'localhost'")
	t.Cleanup(func() { _, _ = o.kok.Exec("DROP USER IF EXISTS '" + buyuk + "'@'localhost'") })
	if _, err := o.panel.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (1,?,?,'BuyukParola123')`, v1, buyuk); err != nil {
		t.Fatal(err)
	}
	// Komşu kendi parolasını değiştirebilmeli (eskiden "birden çok domaine kayıtlı").
	if err := MySQLChangePassword(o.panel, 2, kurban, "KurbanYeni12345"); err != nil {
		t.Errorf("harf varyantı komşunun parola değişimini kilitledi: %v", err)
	}
	// Saldırgan kendi (ayrı) hesabının parolasını değiştirir: komşunun satırı ezilmemeli.
	if err := MySQLChangePassword(o.panel, 1, buyuk, "SaldirganYeni123"); err != nil {
		t.Fatalf("saldırgan kendi hesabının parolasını değiştiremedi: %v", err)
	}
	var pw string
	if err := o.panel.QueryRow(`SELECT db_pass_plain FROM db_accounts WHERE domain_id=2`).Scan(&pw); err != nil {
		t.Fatal(err)
	}
	if gizli.CozBagli(pw, kurban) != "KurbanYeni12345" {
		t.Errorf("komşunun db_pass_plain'i saldırganın parola değişimiyle ezildi")
	}
	if !o.baglanir(kurban, "KurbanYeni12345", kurbanDB) {
		t.Errorf("komşunun yeni parolası uygulanmadı")
	}
	// Ad-alanı kapısı harf duyarsız: büyük harfli varyant komşunun ad-alanında.
	if b, err := DBAdiBaskaKiracininMi(context.Background(), o.panel, 1, strings.ToUpper(on+"_x_yeni")); err != nil || !b {
		t.Errorf("büyük harfli komşu adı kendi ad-alanı sayıldı (%v, %v)", b, err)
	}
	// Mevcut-kullanıcı modu: komşunun satırından parola okunmamalı (domain_id + BINARY).
	if err := MySQLCreateDBForUser(o.panel, 1, on+"_v2", buyuk); err != nil {
		t.Fatalf("kendi hesabına DB bağlanamadı: %v", err)
	}
	var pw2 string
	if err := o.panel.QueryRow(`SELECT db_pass_plain FROM db_accounts WHERE BINARY db_name=?`, on+"_v2").Scan(&pw2); err != nil {
		t.Fatal(err)
	}
	if gizli.CozBagli(pw2, buyuk) != "SaldirganYeni123" {
		t.Errorf("bağlanan DB'nin satırına kendi hesabının değil başka satırın parolası kopyalandı")
	}
}

// DBHesabiBaglanabilirMi: yedekten kurtarılan hesabın satıra yazılabilirliği.
func TestDBHesabiBaglanabilirMi(t *testing.T) {
	o := dbo_ortam(t)
	on := o.on
	ctx := context.Background()
	kurban, _ := o.kurbanKur(t, "KurbanParola123")
	svc := on + "svc"
	o.kokExec(t, "CREATE USER '"+svc+"'@'localhost' IDENTIFIED BY 'SvcParola12345'")
	o.kokExec(t, "CREATE DATABASE `"+on+"svcsema`")
	o.kokExec(t, "GRANT ALL PRIVILEGES ON `"+on+"svcsema`.* TO '"+svc+"'@'localhost'")
	usage := on + "_usage"
	o.kokExec(t, "CREATE USER '"+usage+"'@'localhost' IDENTIFIED BY 'UsageParola123'")
	kendi := on + "_kendi"
	kendiDB := o.hesapKur(t, 1, kendi, "KendiParola12345")
	kayitsizKendi := on + "_kk"
	o.kokExec(t, "CREATE USER '"+kayitsizKendi+"'@'localhost' IDENTIFIED BY 'KkParola12345'")
	o.kokExec(t, "GRANT ALL PRIVILEGES ON `"+GrantDBKac(kendiDB)+"`.* TO '"+kayitsizKendi+"'@'localhost'")
	// Eski devralma kaydı: yetkisi yalnız domain 1'de ama domain 2'nin satırında da geçiyor.
	iki := on + "_iki"
	o.hesapKur(t, 1, iki, "IkiParola12345")
	if _, err := o.panel.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (2,?,?,'x')`,
		on+"_x_iki", iki); err != nil {
		t.Fatal(err)
	}
	// Yalnız 127.0.0.1'de var olan servis hesabı (181'de mailuser/mailadmin gibi); hiçbir
	// kiracı ad-alanına düşmez. '<ad>'@'localhost' oluşturmak onu yerel bağlantıda gölgeler.
	golge := on + "svc2"
	o.kokExec(t, "CREATE USER '"+golge+"'@'127.0.0.1' IDENTIFIED BY 'GolgeParola12345'")
	t.Cleanup(func() { _, _ = o.kok.Exec("DROP USER IF EXISTS '" + golge + "'@'127.0.0.1'") })

	for _, c := range []struct {
		ad, kul, host string
		izin          bool
	}{
		{"sistem hesabı root", "root", "localhost", false},
		{"sistem hesabı pma", "pma", "localhost", false},
		{"sistem hesabı roundcube", "RoundCube", "localhost", false},
		{"sistem hesabı mailuser (ad-alanı dışı)", "mailuser", "localhost", false},
		{"sistem hesabı mailadmin (ad-alanı dışı)", "mailadmin", "localhost", false},
		{"başka host'ta var olan hesap adı (gölgeleme)", golge, "localhost", false},
		{"komşuya kayıtlı hesap", kurban, "localhost", false},
		{"iki domaine kayıtlı hesap (eski devralma kaydı)", iki, "localhost", false},
		{"kendi şemasında yetkili servis hesabı", svc, "localhost", false},
		{"yalnız USAGE'li yabancı hesap", usage, "localhost", false},
		{"bu domaine kayıtlı kendi hesabı", kendi, "localhost", true},
		{"kayıtsız ama bu domainin DB'sinde yetkili", kayitsizKendi, "localhost", true},
		{"yeni ad, kendi ad-alanı", on + "_yeni", "localhost", true},
		{"yeni ad, ad-alanı dışı (wpu_)", "wpu_" + on, "localhost", true},
		{"yeni ad, komşunun ad-alanı", on + "_x_yeni", "localhost", false},
		{"yeni ad, komşunun ad-alanı (büyük harf)", strings.ToUpper(on + "_x_yeni"), "localhost", false},
		{"yeni ad, % host", on + "_uzak", "%", true},
		{"geçersiz host", on + "_yeni", "a'b", false},
		{"geçersiz ad", "a'b", "localhost", false},
	} {
		err := DBHesabiBaglanabilirMi(ctx, o.panel, 1, c.kul, c.host)
		if (err == nil) != c.izin {
			t.Errorf("%s (%s@%s): izin=%v beklenir, hata=%v", c.ad, c.kul, c.host, c.izin, err)
		}
	}
}

// DBAdAlaniKullanimda: yeni sk'nın DB ad-alanı başkasınca kullanılıyor mu?
func TestDBAdAlaniKullanimda(t *testing.T) {
	o := dbo_ortam(t)
	on := o.on
	if DBAdAlaniKullanimda(o.panel, "c_"+on+"_q") {
		t.Fatal("boş ad-alanı kullanımda sayıldı")
	}
	if _, err := o.panel.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (1,?,?,'x')`,
		on+"_q_main", on+"_zz"); err != nil {
		t.Fatal(err)
	}
	if !DBAdAlaniKullanimda(o.panel, "c_"+on+"_q") {
		t.Errorf("db_accounts'taki önceden kapılmış DB adı görülmedi")
	}
	if _, err := o.panel.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (1,?,?,'x')`,
		on+"_w1", strings.ToUpper(on+"_w_db")); err != nil {
		t.Fatal(err)
	}
	if !DBAdAlaniKullanimda(o.panel, "c_"+on+"_w") {
		t.Errorf("büyük harfli önceden kapılmış kullanıcı adı görülmedi")
	}
	if _, err := o.panel.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (1,?,?,'x')`,
		"c_"+on+"_f_db", on+"_ff"); err != nil {
		t.Fatal(err)
	}
	if !DBAdAlaniKullanimda(o.panel, "c_"+on+"_f") {
		t.Errorf("tam-sk kökündeki ad görülmedi")
	}
	o.kokExec(t, "CREATE USER '"+on+"_r_db'@'localhost' IDENTIFIED BY 'RParola12345'")
	if !DBAdAlaniKullanimda(o.panel, "c_"+on+"_r") {
		t.Errorf("MariaDB'deki önceden açılmış kullanıcı görülmedi")
	}
	o.kokExec(t, "CREATE USER '"+on+"_gs'@'localhost' IDENTIFIED BY 'GsParola12345'")
	o.kokExec(t, "GRANT ALL PRIVILEGES ON `"+GrantDBKac(on+"_s_main")+"`.* TO '"+on+"_gs'@'localhost'")
	if !DBAdAlaniKullanimda(o.panel, "c_"+on+"_s") {
		t.Errorf("artık yetki satırı görülmedi")
	}
	o.kokExec(t, "CREATE DATABASE `"+on+"_t_x`")
	if !DBAdAlaniKullanimda(o.panel, "c_"+on+"_t") {
		t.Errorf("var olan şema görülmedi")
	}
	if DBAdAlaniKullanimda(o.panel, "c_"+on+"_tt") || DBAdAlaniKullanimda(o.panel, "c_"+on+"_q9") {
		t.Errorf("ilgisiz ad-alanı kullanımda sayıldı")
	}
}

// 🔴 EŞİT KÖK: yeni sk'nın bir kökü mevcut bir domainin köküne EŞİTSE (c-shop.com →
// c_c_shop, c'siz kökü "c_shop" = shop.com'un sk'sı; ya da tersi) iki kiracı aynı
// ad-alanını paylaşırdı → sk kullanımda sayılır. Ad kullanımı olmasa bile.
func TestDBAdAlaniKullanimda_EsitKok(t *testing.T) {
	o := dbo_ortam(t) // domain 1 = c_<on>, domain 2 = c_<on>_x
	on := o.on
	if !DBAdAlaniKullanimda(o.panel, "c_c_"+on) {
		t.Errorf("c_c_%s: c'siz kökü domain 1'in sk'sına eşit — kullanımda sayılmalı", on)
	}
	if !DBAdAlaniKullanimda(o.panel, "c_c_"+on+"_x") {
		t.Errorf("c_c_%s_x: c'siz kökü domain 2'nin sk'sına eşit — kullanımda sayılmalı", on)
	}
	// Ters yön: c_c_<on>y varken yeni c_<on>y (tam sk'sı mevcudun c'siz köküne eşit).
	if _, err := o.panel.Exec(`INSERT INTO domains (id, sistem_kullanici) VALUES (3, ?)`, "c_c_"+on+"y"); err != nil {
		t.Fatal(err)
	}
	if !DBAdAlaniKullanimda(o.panel, "c_"+on+"y") {
		t.Errorf("c_%sy: tam sk'sı c_c_%sy'nin c'siz köküne eşit — kullanımda sayılmalı", on, on)
	}
	// Eşitlik yoksa (yalnız önek benzerliği) kullanımda değil.
	if DBAdAlaniKullanimda(o.panel, "c_c_"+on+"z") || DBAdAlaniKullanimda(o.panel, "c_"+on+"yy") {
		t.Errorf("kökü hiçbir domainle eşit olmayan sk kullanımda sayıldı")
	}
}

// En uzun kök kuralı: yeni kısa kökün ad-alanına düşen ama mevcut DAHA UZUN bir köke ait
// adlar yeni domainin değildir → gereksiz sayı eki (girgin.com → c_girgin2) yok. Kısa
// köke ait (önceden kapılmış) ad ise hâlâ kullanım sayılır.
func TestDBAdAlaniKullanimda_EnUzunKok(t *testing.T) {
	o := dbo_ortam(t)
	on := o.on
	uzun := on + "n_x" // mevcut uzun kök (<on>n-x.test → c_<on>n_x)
	if _, err := o.panel.Exec(`INSERT INTO domains (id, sistem_kullanici) VALUES (5, ?)`, "c_"+uzun); err != nil {
		t.Fatal(err)
	}
	if _, err := o.panel.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (5,?,?,'x')`,
		uzun+"_main", uzun+"_db"); err != nil {
		t.Fatal(err)
	}
	o.kokExec(t, "CREATE USER '"+uzun+"_db'@'localhost' IDENTIFIED BY 'UzunParola12345'")
	o.kokExec(t, "CREATE DATABASE `"+uzun+"_main`")
	o.kokExec(t, "GRANT ALL PRIVILEGES ON `"+GrantDBKac(uzun+"_main")+"`.* TO '"+uzun+"_db'@'localhost'")
	if DBAdAlaniKullanimda(o.panel, "c_"+on+"n") {
		t.Errorf("c_%sn: yalnız daha uzun köke (c_%s) ait adlar var — kullanımda sayılmamalı", on, uzun)
	}
	// Kısa kökün kendi ad-alanında (uzun köke düşmeyen) önceden açılmış ad → kullanımda.
	o.kokExec(t, "CREATE DATABASE `"+on+"n_zz`")
	if !DBAdAlaniKullanimda(o.panel, "c_"+on+"n") {
		t.Errorf("c_%sn: kendi ad-alanındaki %sn_zz şeması görülmedi", on, on)
	}
}

// Panel sorgusu NO_BACKSLASH_ESCAPES açık bir bağlantıda da önceden kapılmış adı bulmalı.
// Not (ölçüldü): MariaDB bu kipte de LIKE'ın örtük '\' kaçışını koruyor, yani eski
// "LIKE '<kök>\_%'" burada da eşleşiyordu; sorgu artık örtük varsayılana değil açık
// ESCAPE '!' yan tümcesine dayanır. Bu test o yan tümcenin kaldırılmasını yakalar.
func TestDBAdAlaniKullanimda_NoBackslashEscapes(t *testing.T) {
	o := dbo_ortam(t)
	on := o.on
	if _, err := o.panel.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (1,?,?,'x')`,
		on+"_e_main", on+"_e_db"); err != nil {
		t.Fatal(err)
	}
	c := o.cfg.Clone()
	var ad string
	if err := o.panel.QueryRow(`SELECT DATABASE()`).Scan(&ad); err != nil {
		t.Fatal(err)
	}
	c.DBName = ad
	p := map[string]string{}
	for k, v := range c.Params {
		p[k] = v
	}
	p["sql_mode"] = "'NO_BACKSLASH_ESCAPES'"
	c.Params = p
	nbe, err := sql.Open("mysql", c.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer nbe.Close()
	nbe.SetMaxOpenConns(1)
	var kip string
	if err := nbe.QueryRow(`SELECT @@SESSION.sql_mode`).Scan(&kip); err != nil || !strings.Contains(kip, "NO_BACKSLASH_ESCAPES") {
		t.Fatalf("NO_BACKSLASH_ESCAPES oturumu kurulamadı (%q, %v)", kip, err)
	}
	if !DBAdAlaniKullanimda(nbe, "c_"+on+"_e") {
		t.Errorf("NO_BACKSLASH_ESCAPES açıkken panel kaydındaki önceden kapılmış ad görülmedi (fail-open)")
	}
}

func TestSistemDBHesabiVeDesenYardimcilari(t *testing.T) {
	for _, u := range []string{"root", "ROOT", "pma", "roundcube", "pureftpd", "panel", "mysql.sys", "mariadb.sys", "debian-sys-maint", ""} {
		if !SistemDBHesabiMi(u) {
			t.Errorf("%q sistem hesabı sayılmadı", u)
		}
	}
	for _, u := range []string{"c_girgin_db", "girgin_db", "wpu_abc", "roundcube2"} {
		if SistemDBHesabiMi(u) {
			t.Errorf("%q sistem hesabı sayıldı", u)
		}
	}
	if yetkiDesenCoz(`a\_b_c\%`) != "a_b_c%" {
		t.Errorf("yetkiDesenCoz yanlış")
	}
	for d, beklenen := range map[string]bool{`a\_b`: false, `a\_%`: true, `a\%`: false, `%`: true, `a_b`: false, `a\\%`: true} {
		if kacissizYuzdeVar(d) != beklenen {
			t.Errorf("kacissizYuzdeVar(%q) = %v", d, !beklenen)
		}
	}
}
