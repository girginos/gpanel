package hesaplar

// dbgiris_test.go — DBGirisDene + MySQLKayitliParolaEsitle, GERÇEK (geçici) MariaDB'de.
//
// 🔴 OLAY (2026-10-12, bir müşteri sunucusu): db_accounts kaydındaki parola, hesabın
// MariaDB parolası panel dışından değiştiği için bayattı → phpMyAdmin SSO "Access
// denied". "Parola Sıfırla" (ALTER USER) çalışan siteyi (config'teki parola) kırardı;
// eşitleme MariaDB'ye DOKUNMADAN kaydı düzeltir, ama yalnız parola GERÇEKTEN girerse.
//
// Harness: dbonek_test.go (dbo_ortam — geçici MariaDB + mysql sarmalayıcısı).

import (
	"context"
	"errors"
	"testing"

	"girginospanel/internal/gizli"
)

// dbgSoketiGecici: DBGirisDene'yi geçici sunucunun soketine yönlendirir.
func (o *dboOrtam) dbgSoketiGecici(t *testing.T) {
	t.Helper()
	eski := dbGirisSoketi
	sk := o.cfg.Addr
	dbGirisSoketi = func() string { return sk }
	t.Cleanup(func() { dbGirisSoketi = eski })
}

func (o *dboOrtam) kayitliParola(t *testing.T, kul string) string {
	t.Helper()
	var s string
	if err := o.panel.QueryRow(`SELECT db_pass_plain FROM db_accounts WHERE BINARY db_user=? LIMIT 1`, kul).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return gizli.CozBagli(s, kul)
}

func TestDBGirisDene_GercekMariaDB(t *testing.T) {
	o := dbo_ortam(t)
	o.dbgSoketiGecici(t)
	kul := o.on + "_gir"
	o.kokExec(t, "CREATE USER '"+kul+"'@'localhost' IDENTIFIED BY 'Dogru-Parola-1'")
	ctx := context.Background()
	if err := DBGirisDene(ctx, kul, "Dogru-Parola-1"); err != nil {
		t.Fatalf("doğru parola reddedildi: %v", err)
	}
	if err := DBGirisDene(ctx, kul, "Bayat-Parola-1"); !errors.Is(err, ErrDBParolaUyusmuyor) {
		t.Errorf("yanlış parola: err=%v; ErrDBParolaUyusmuyor bekleniyordu", err)
	}
	// Tırnak/özel karakterli parola (uygulama config'lerinde olur) sürücüde güvenle taşınır.
	kul2 := o.on + "_gir2"
	o.kokExec(t, "CREATE USER '"+kul2+"'@'localhost' IDENTIFIED BY 'a''b\"c@d:e/f'")
	if err := DBGirisDene(ctx, kul2, `a'b"c@d:e/f`); err != nil {
		t.Errorf("özel karakterli doğru parola reddedildi: %v", err)
	}
	// Ölçülemeyen durum (soket yok) KESİN red sayılmaz.
	dbGirisSoketi = func() string { return "" }
	if err := DBGirisDene(ctx, kul, "Dogru-Parola-1"); err == nil || errors.Is(err, ErrDBParolaUyusmuyor) {
		t.Errorf("soket yokken err=%v; ölçüm hatası (ErrDBParolaUyusmuyor DEĞİL) bekleniyordu", err)
	}
	dbGirisSoketi = func() string { return o.cfg.Addr + ".yok" }
	if err := DBGirisDene(ctx, kul, "Dogru-Parola-1"); err == nil || errors.Is(err, ErrDBParolaUyusmuyor) {
		t.Errorf("olmayan soket: err=%v; ölçüm hatası bekleniyordu", err)
	}
}

// 🔴 ÇEKİRDEK: bayat kayıt, mevcut (doğru) parolayla eşitlenir; MariaDB parolası
// DEĞİŞMEZ (site kırılmaz). Yanlış parola kaydı değiştirmez.
func TestMySQLKayitliParolaEsitle_BayatKayitDuzelir(t *testing.T) {
	o := dbo_ortam(t)
	o.dbgSoketiGecici(t)
	kul, dbAdi := o.on+"_db", o.on+"_main"
	o.kokExec(t, "CREATE USER '"+kul+"'@'localhost' IDENTIFIED BY 'Sitenin-Parolasi-1'")
	o.kokExec(t, "CREATE DATABASE `"+dbAdi+"`")
	o.kokExec(t, "GRANT ALL PRIVILEGES ON `"+GrantDBKac(dbAdi)+"`.* TO '"+kul+"'@'localhost'")
	// Olaydaki durum: panel kaydı BAYAT parolayı tutuyor.
	if _, err := o.panel.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (1,?,?,?)`,
		dbAdi, kul, gizli.SaklaBagli("Bayat-Panel-Parolasi", kul)); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := MySQLKayitliParolaEsitle(ctx, o.panel, 1, kul, "Tahmin-Edilen-1"); !errors.Is(err, ErrDBParolaUyusmuyor) {
		t.Fatalf("yanlış parola: err=%v; ErrDBParolaUyusmuyor bekleniyordu", err)
	}
	if p := o.kayitliParola(t, kul); p != "Bayat-Panel-Parolasi" {
		t.Errorf("yanlış parolayla kayıt DEĞİŞTİ (%q)", p)
	}
	if err := MySQLKayitliParolaEsitle(ctx, o.panel, 1, kul, "Sitenin-Parolasi-1"); err != nil {
		t.Fatalf("doğru parolayla eşitleme düştü: %v", err)
	}
	if p := o.kayitliParola(t, kul); p != "Sitenin-Parolasi-1" {
		t.Errorf("kayıt eşitlenmedi (%q)", p)
	}
	// MariaDB parolası DEĞİŞMEDİ: site config'teki parolayla bağlanmaya devam eder.
	if !o.baglanir(kul, "Sitenin-Parolasi-1", dbAdi) {
		t.Errorf("eşitleme MariaDB parolasını değiştirdi — site kırılırdı")
	}
}

// Sahiplik kapıları ALTER USER yolunun aynısı: sistem hesabı, başka domainin
// kullanıcısı ve iki domaine kayıtlı kullanıcı eşitlenemez (giriş bile denenmez).
func TestMySQLKayitliParolaEsitle_SahiplikKapilari(t *testing.T) {
	o := dbo_ortam(t)
	o.dbgSoketiGecici(t)
	ctx := context.Background()
	// Kapılardan geçemeyen istek MariaDB'ye GİRİŞ BİLE DENEMEZ (parola yoklama kapısı olmaz).
	deneme := 0
	eskiDene := dbGirisDene
	dbGirisDene = func(c context.Context, k, p string) error { deneme++; return eskiDene(c, k, p) }
	t.Cleanup(func() { dbGirisDene = eskiDene })
	if err := MySQLKayitliParolaEsitle(ctx, o.panel, 1, "root", "x"); !errors.Is(err, ErrSistemDBHesabi) {
		t.Errorf("root: err=%v; ErrSistemDBHesabi bekleniyordu", err)
	}
	kurban, kurbanDB := o.kurbanKur(t, "KurbanParola123") // domain 2
	if err := MySQLKayitliParolaEsitle(ctx, o.panel, 1, kurban, "KurbanParola123"); err == nil {
		t.Errorf("domain 1, domain 2'nin kullanıcısının kaydını eşitleyebildi")
	}
	// İki domaine kayıtlı kullanıcı (eski devralma kaydı): yetkileri yalnız domain 1'in
	// DB'sinde olsa bile eşitlenemez — "yönetici incelemesi gerekli".
	iki, ikiDB := o.on+"_iki", o.on+"_iki_a"
	o.kokExec(t, "CREATE USER '"+iki+"'@'localhost' IDENTIFIED BY 'IkiParola12345'")
	o.kokExec(t, "CREATE DATABASE `"+ikiDB+"`")
	o.kokExec(t, "GRANT ALL PRIVILEGES ON `"+GrantDBKac(ikiDB)+"`.* TO '"+iki+"'@'localhost'")
	if _, err := o.panel.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (1,?,?,'bayat'), (2,?,?,'bayat')`,
		ikiDB, iki, o.on+"_x_iki_b", iki); err != nil {
		t.Fatal(err)
	}
	if err := MySQLKayitliParolaEsitle(ctx, o.panel, 1, iki, "IkiParola12345"); err == nil {
		t.Errorf("iki domaine kayıtlı kullanıcının kaydı eşitlendi")
	}
	// Yalnız domain 1'e kayıtlı ama KOMŞUNUN (domain 2) DB'sinde yetkili hesap: panel
	// bu kimliği saklarsa phpMyAdmin SQL sekmesi komşunun verisine açılırdı → eşitlenmez.
	capraz := o.on + "_capraz"
	o.kokExec(t, "CREATE USER '"+capraz+"'@'localhost' IDENTIFIED BY 'CaprazParola123'")
	o.kokExec(t, "GRANT SELECT ON `"+GrantDBKac(kurbanDB)+"`.* TO '"+capraz+"'@'localhost'")
	if _, err := o.panel.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (1,?,?,'bayat')`,
		o.on+"_capraz_a", capraz); err != nil {
		t.Fatal(err)
	}
	if err := MySQLKayitliParolaEsitle(ctx, o.panel, 1, capraz, "CaprazParola123"); err == nil {
		t.Errorf("komşunun DB'sinde yetkili hesabın kaydı eşitlendi")
	}
	for _, p := range []string{"", "gos1:AAAA", string(make([]byte, kayitParolaAzami+1))} {
		if err := MySQLKayitliParolaEsitle(ctx, o.panel, 2, kurban, p); err == nil {
			t.Errorf("geçersiz parola (%d bayt) kabul edildi", len(p))
		}
	}
	if p := o.kayitliParola(t, kurban); p != "KurbanParola123" {
		t.Errorf("reddedilen denemeler kaydı değiştirdi (%q)", p)
	}
	if deneme != 0 {
		t.Errorf("kapıda reddedilmesi gereken %d istek MariaDB'ye giriş denedi", deneme)
	}
	// Pozitif kontrol: kapılardan geçen meşru istek giriş dener ve eşitler.
	if err := MySQLKayitliParolaEsitle(ctx, o.panel, 2, kurban, "KurbanParola123"); err != nil || deneme != 1 {
		t.Errorf("meşru eşitleme: err=%v deneme=%d; nil/1 bekleniyordu", err, deneme)
	}
}
