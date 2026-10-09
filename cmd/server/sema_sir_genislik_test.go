package main

// sema_sir_genislik_test.go — 0218: at-rest şifreli sır kolonları GERÇEK şifreli çıktıyı alacak kadar geniş mi?
//
// Hata sınıfı: gizli.SaklaBagli çıktısı düz değerden UZUNDUR ("gos1:" + base64(nonce 12 + veri + etiket 16)). Kolon dar
// kalırsa STRICT_TRANS_TABLES'ta (MariaDB varsayılanı) yazım "Data too long" ile düşer. 8045730 (2026-09-16) şifrelemeyi
// getirdi ama users.totp_secret ve git_repos.webhook_secret varchar(64) kaldı: 2FA HİÇ etkinleştirilemedi (500), git
// deposu bağlanamadı (500). Birim testler sahte DB kullandığı için kaçmıştı; yalnız GERÇEK şemada ve gerçek şifreli
// değerle görünür — bu dosya tam onu yapar.
//
// Gerçek MariaDB gerekir (GOSP_TEST_GECICI_DSN yoksa ATLANIR) ve gizli db.key bulunmalıdır (yoksa şifreleme kapalıdır:
// değer düz yazılır, sorun yaşanmaz → ATLANIR).

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"girginospanel/internal/gizli"
)

func sifreliSirAnahtarVar(t *testing.T) {
	t.Helper()
	if gizli.SaklaBagli("x", "t") == "x" {
		t.Skip("gizli anahtarı (db.key) yok — at-rest şifreleme kapalı, test anlamsız")
	}
}

// Formül gerçek şifreli çıktıyla BİREBİR olmalı; yoksa kayıt listesindeki "gereken genişlik" yalan söyler.
func TestSifreliUzunluk_GizliCiktisiylaBirebir(t *testing.T) {
	sifreliSirAnahtarVar(t)
	for n := 1; n <= 300; n++ {
		if got, want := len(gizli.SaklaBagli(strings.Repeat("a", n), "totp")), sifreliUzunluk(n); got != want {
			t.Fatalf("n=%d: gerçek şifreli uzunluk %d, formül %d", n, got, want)
		}
	}
	if sifreliUzunluk(32) != 85 || sifreliUzunluk(40) != 96 {
		t.Fatalf("bilinen değerler: 32→%d (85 beklenir), 40→%d (96 beklenir)", sifreliUzunluk(32), sifreliUzunluk(40))
	}
}

// Kayıt listesi, şifrelenen sırların gerçek en uzun değerlerini kapsamalı (genişlik kontrolü bunlara göre).
func TestSifreliSirGenislik_KayitGercekUzunluklariKapsar(t *testing.T) {
	bek := map[string]int{"users.totp_secret": 32, "git_repos.webhook_secret": 40}
	for _, k := range sifreliSirGenislik {
		ad := k.tablo + "." + k.kolon
		if min, ok := bek[ad]; !ok || k.enUzunDuz < min {
			t.Errorf("%s: kayıtlı en uzun düz sır %d, gerçekte en az %d olabilir", ad, k.enUzunDuz, min)
		}
		delete(bek, ad)
	}
	for ad := range bek {
		t.Errorf("%s kayıt listesinde yok", ad)
	}
}

func TestSifreliSirKolonlari_0218_GercekSifreliDegerSigar(t *testing.T) {
	sifreliSirAnahtarVar(t)
	db := semaTestDB(t)
	exec := func(q string, a ...any) {
		t.Helper()
		if _, err := db.Exec(q, a...); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}
	// 0218 ÖNCESİ şema: gerçek eski göçler. 0001'in "USE panel"i ASLA çalıştırılmaz (gerçek panel DB'sine geçerdi) → users ve
	// domains yalnız bu göçlerin ihtiyacı kadar elle kurulur.
	exec(`CREATE TABLE users (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, username VARCHAR(64) NOT NULL UNIQUE,
		email VARCHAR(255) NOT NULL DEFAULT '', full_name VARCHAR(255) NOT NULL DEFAULT '') ENGINE=InnoDB`)
	exec(`CREATE TABLE domains (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY) ENGINE=InnoDB`)
	for _, f := range []string{"0009_git.sql", "0024_profil_2fa.sql", "0103_webhook_secret_hash.sql"} {
		semaTestGoc(t, db, f)
	}
	exec(`INSERT INTO users (id, username) VALUES (1, 'root')`)
	exec(`INSERT INTO domains (id) VALUES (1)`)

	// MariaDB'nin varsayılanı STRICT'tir; sunucu ayarından bağımsız olması için oturum modu açıkça kurulur.
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), `SET SESSION sql_mode='STRICT_TRANS_TABLES,NO_ENGINE_SUBSTITUTION'`); err != nil {
		t.Fatal(err)
	}
	yaz := func(k struct {
		tablo, kolon string
		enUzunDuz    int
	}, deger string) error {
		var err error
		switch k.tablo {
		case "users":
			_, err = conn.ExecContext(context.Background(), `UPDATE users SET totp_secret=? WHERE id=1`, deger)
		default:
			_, err = conn.ExecContext(context.Background(),
				`INSERT INTO git_repos (domain_id, repo_url, deploy_key_pub, webhook_secret) VALUES (1, 'https://x/y.git', 'k', ?)`, deger)
		}
		return err
	}

	// Mevcut (düz metin, 64'e sığan) satırlar MODIFY'den sağ çıkmalı.
	const eskiTotp, eskiWH = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP", "0123456789abcdef0123456789abcdef01234567"
	exec(`UPDATE users SET totp_secret=? WHERE id=1`, eskiTotp)
	exec(`INSERT INTO git_repos (domain_id, repo_url, deploy_key_pub, webhook_secret, webhook_secret_hash) VALUES (1,'https://x/eski.git','k',?, 'h')`, eskiWH)

	// 1) ÖNCE: gerçek şifreli değer dar kolona SIĞMAZ (hatanın kendisi).
	for _, k := range sifreliSirGenislik {
		duz := strings.Repeat("A", k.enUzunDuz)
		if k.tablo == "users" {
			duz = strings.Repeat("A", 32)
		} else {
			duz = strings.Repeat("b", 40)
		}
		err := yaz(k, gizli.SaklaBagli(duz, "sinama"))
		if err == nil || !strings.Contains(err.Error(), "Data too long") {
			t.Fatalf("0218 ÖNCESİ %s.%s: şifreli %d karakterlik değer dar kolona 'Data too long' ile REDDEDİLMELİYDİ, gelen: %v",
				k.tablo, k.kolon, sifreliUzunluk(len(duz)), err)
		}
	}
	// ... ve açılış denetimi bunu adıyla söyler.
	sorun := semaGenislikDogrula(db)
	if len(sorun) != len(sifreliSirGenislik) {
		t.Fatalf("0218 öncesi %d genişlik sorunu beklenirdi: %v", len(sifreliSirGenislik), sorun)
	}
	for i, k := range sifreliSirGenislik {
		if !strings.HasPrefix(sorun[i], sema0218Etiket) || !strings.Contains(sorun[i], k.tablo+"."+k.kolon) {
			t.Errorf("sorun %d %s.%s'i adıyla [0218] etiketiyle söylemeli: %q", i, k.tablo, k.kolon, sorun[i])
		}
	}

	// ... açılıştaki semaDogrula da dar kolonu günlüğe, DÜZELTMESİYLE birlikte yazar (sessiz geçmez).
	if g := semaYapiGunlugu(t, db); !strings.Contains(g, sema0218Etiket+"users.totp_secret") ||
		!strings.Contains(g, sema0218Etiket+"git_repos.webhook_secret") || !strings.Contains(g, "0218_sifreli_sir_sutunlari.sql") {
		t.Errorf("semaDogrula (açılış) dar kolonları 0218 düzeltmesiyle günlüğe yazmalı: %q", g)
	}

	// 2) 0218 (iki kez: idempotent).
	semaTestGoc(t, db, "0218_sifreli_sir_sutunlari.sql")
	semaTestGoc(t, db, "0218_sifreli_sir_sutunlari.sql")
	if s := semaGenislikDogrula(db); len(s) != 0 {
		t.Fatalf("0218 sonrası genişlik sorunu kalmamalı: %v", s)
	}

	// 3) SONRA: en uzun düz sırlar (kayıt listesi) ve gerçek uzunluklar sığar; geri okununca çözülür.
	for _, k := range sifreliSirGenislik {
		for _, n := range []int{k.enUzunDuz, map[string]int{"users": 32, "git_repos": 40}[k.tablo]} {
			duz := strings.Repeat("Z", n)
			sif := gizli.SaklaBagli(duz, "sinama")
			if err := yaz(k, sif); err != nil {
				t.Fatalf("0218 SONRASI %s.%s: %d karakterlik düz sır (şifreli %d) sığmalı: %v", k.tablo, k.kolon, n, len(sif), err)
			}
			var oku string
			q := map[string]string{"users": `SELECT totp_secret FROM users WHERE id=1`,
				"git_repos": `SELECT webhook_secret FROM git_repos ORDER BY id DESC LIMIT 1`}[k.tablo]
			if err := db.QueryRow(q).Scan(&oku); err != nil || oku != sif || gizli.CozBagli(oku, "sinama") != duz {
				t.Fatalf("%s.%s: geri okunan değer yazılanla aynı ve çözülebilir olmalı (err=%v)", k.tablo, k.kolon, err)
			}
		}
	}

	// 4) Mevcut düz metin satırlar bozulmadı.
	var t0, w0 string
	if err := db.QueryRow(`SELECT webhook_secret FROM git_repos WHERE repo_url='https://x/eski.git'`).Scan(&w0); err != nil || w0 != eskiWH {
		t.Errorf("MODIFY mevcut webhook_secret'ı bozdu: %q %v", w0, err)
	}
	_ = t0

	// 5) Tanım: tür/NOT NULL/UNIQUE korunur.
	tanim := func(tablo, kolon string) (tur, null, varsayilan string, gen int) {
		t.Helper()
		var vs sql.NullString
		if err := db.QueryRow(`SELECT DATA_TYPE, IS_NULLABLE, COLUMN_DEFAULT, CHARACTER_MAXIMUM_LENGTH FROM information_schema.COLUMNS
			WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?`, tablo, kolon).Scan(&tur, &null, &vs, &gen); err != nil {
			t.Fatal(err)
		}
		return tur, null, strings.Trim(vs.String, "'"), gen
	}
	if tur, null, vs, gen := tanim("users", "totp_secret"); tur != "varchar" || null != "NO" || vs != "" || gen < sifreliUzunluk(32) {
		t.Errorf("users.totp_secret tanımı: %s null=%s varsayılan=%q genişlik=%d", tur, null, vs, gen)
	}
	if tur, null, _, gen := tanim("git_repos", "webhook_secret"); tur != "varchar" || null != "NO" || gen < sifreliUzunluk(64) {
		t.Errorf("git_repos.webhook_secret tanımı: %s null=%s genişlik=%d", tur, null, gen)
	}
	var benzersiz int
	if err := db.QueryRow(`SELECT MIN(NON_UNIQUE) FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'git_repos' AND INDEX_NAME = 'uk_git_secret'`).Scan(&benzersiz); err != nil || benzersiz != 0 {
		t.Errorf("uk_git_secret UNIQUE indeksi korunmalı: non_unique=%d err=%v", benzersiz, err)
	}
	if _, err := db.Exec(`INSERT INTO git_repos (domain_id, repo_url, deploy_key_pub, webhook_secret) VALUES (1,'https://x/z.git','k',?)`, eskiWH); err == nil {
		t.Errorf("UNIQUE indeks aynı webhook_secret'ın ikinci kez yazılmasına izin verdi")
	}
	// 191 x utf8mb4 = 764 B: eski satır biçimlerinde (767 B sınırı) da indekslenebilir kalmalı.
	if _, gen := func() (string, int) { _, _, _, g := tanim("git_repos", "webhook_secret"); return "", g }(); gen > 191 {
		t.Errorf("webhook_secret genişliği %d > 191: UNIQUE indeks anahtarı 767 B sınırını aşabilir (COMPACT/REDUNDANT satır biçimi)", gen)
	}

	// 6) Açılış denetimi temiz.
	if g := semaYapiGunlugu(t, db); strings.Contains(g, sema0218Etiket) || strings.Contains(g, "0218") {
		t.Errorf("0218 sonrası denetim 0218 uyarısı vermemeli: %q", g)
	}
}

// Tablo/kolon yoksa (boş şema, başka kurulum) genişlik denetimi sorun UYDURMAZ — kolon eksikliğini zaten kolon kontrolü bildirir.
func TestSemaGenislikDogrula_TabloYoksaSusar(t *testing.T) {
	db := semaTestDB(t)
	if s := semaGenislikDogrula(db); len(s) != 0 {
		t.Fatalf("tablolar yokken sorun uydurulmamalı: %v", s)
	}
	// geniş TEXT kolon da sorun değildir (CHARACTER_MAXIMUM_LENGTH 65535)
	if _, err := db.Exec(`CREATE TABLE users (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, totp_secret TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if s := semaGenislikDogrula(db); len(s) != 0 {
		t.Fatalf("TEXT kolon yeterince geniş: %v", s)
	}
}

// semaRaporla: 0218 sorunu 0218'in düzeltmesini söyler (0217'ninkini değil); karışıkta ikisi de; yalnız 0217'de eski metin aynen.
func TestSemaRaporla_0218IpucuAyri(t *testing.T) {
	s0218 := sema0218Etiket + "users.totp_secret genisligi 64 < 85 (32 karakterlik sir sifrelenince kolona SIGMAZ)"
	g := semaRaporlaGunlugu(nil, []string{s0218})
	if !strings.Contains(g, "DELETE FROM schema_migrations WHERE dosya='0218_sifreli_sir_sutunlari.sql'") || !strings.Contains(g, "SEMA YAPISI BOZUK") {
		t.Errorf("0218 sorununda 0218 düzeltmesi beklenirdi: %q", g)
	}
	if strings.Contains(g, "0217_silinen_yedek.sql") {
		t.Errorf("yalnız 0218 sorunu varken 0217 düzeltmesi yazılmamalı (yanıltır): %q", g)
	}
	g = semaRaporlaGunlugu(nil, []string{"backups.ix_backups_silinen indeksi yok"})
	if strings.Contains(g, "0218") || !strings.Contains(g, "0217_silinen_yedek.sql") {
		t.Errorf("yalnız 0217 sorununda eski metin aynen kalmalı: %q", g)
	}
	g = semaRaporlaGunlugu(nil, []string{"backups.ix_backups_silinen indeksi yok", s0218})
	if !strings.Contains(g, "0217_silinen_yedek.sql") || !strings.Contains(g, "0218_sifreli_sir_sutunlari.sql") {
		t.Errorf("karışık sorunda iki düzeltme de yazılmalı: %q", g)
	}
	if strings.Contains(semaRaporlaGunlugu(nil, []string{s0218}), "kritik kolonlarin tamami mevcut") {
		t.Errorf("0218 sorunu varken başarı mesajı basılmamalı")
	}
}
