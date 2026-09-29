// Package hesaplar: FTP hesabi + MySQL DB hesabi olusturma yardimcilari
package hesaplar

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"girginospanel/internal/gizli"
	"log"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// RandomParola: URL-safe alphanumeric parola (default 20 karakter)
func RandomParola(n int) string {
	if n <= 0 {
		n = 20
	}
	const harf = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789"
	// 🔴 MODULO YANLILIGI: alfabe 56 karakter, 256 % 56 = 32 artik birakir →
	// 0..31 bayt degerlerine dusen ilk 32 harf digerlerinden ~%20 daha sik
	// cikiyordu. Reddetme ornekleme (rejection sampling): 56'nin 256'yi asmayan
	// en buyuk kati 224'tur; >=224 baytlar ATILIR ve yerine yenisi cekilir.
	const esik = 256 - (256 % len(harf)) // 224
	out := make([]byte, 0, n)
	buf := make([]byte, n)
	for tur := 0; len(out) < n; tur++ {
		if tur > 64 {
			return "" // pratikte imkansiz; yine de sonsuz donguye kapi birakma
		}
		// Hata YUTULMAZ: crypto/rand okunamazsa buf tamamen sifir kalir ve
		// uretilen "parola" AAAA... olur. Bos donup cagirani durdurmak,
		// tahmin edilebilir parola vermekten iyidir.
		if _, err := rand.Read(buf); err != nil {
			return ""
		}
		for _, v := range buf {
			if int(v) >= esik {
				continue // yanliligi yaratan artik bolge — at, yeniden cek
			}
			out = append(out, harf[int(v)%len(harf)])
			if len(out) == n {
				break
			}
		}
	}
	return string(out)
}

var reDBKimlik = regexp.MustCompile(`\A[A-Za-z0-9_]{1,64}\z`)

// reDBSonek: musteri-verdigi DB/kullanici SONEKI (panel `<sk>_` onekini kendisi ekler).
// Yalniz kucuk harf/rakam/alt-cizgi, 1-32 karakter. Onek eklendikten sonra toplam <=64
// olmasi ayrica GecerliDBKimlik ile dogrulanir.
var reDBSonek = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)

// GecerliDBKimlik: MySQL identifier (db/kullanici adi) guvenli mi? backtick/tirnak/bosluk yok => SQLi kapali
func GecerliDBKimlik(s string) bool {
	return reDBKimlik.MatchString(s)
}

// GecerliDBSonek: musteri sonek girdisi guvenli mi? (onek eklemeden ONCE dogrulanir)
func GecerliDBSonek(s string) bool {
	return reDBSonek.MatchString(s)
}

// ParolaGucluMu: musteri DB parolasi yeterince guclu mu? >=12 karakter + karisik
// (en az bir harf ve bir rakam) + tek satir. UI'de gosterilmek uzere Turkce neden dondurur.
func ParolaGucluMu(pw string) (bool, string) {
	if !ParolaGecerli(pw) {
		return false, "parola geçersiz karakter (satır sonu/kontrol) içeriyor"
	}
	if len([]rune(pw)) < 12 {
		return false, "parola en az 12 karakter olmalı"
	}
	var harf, rakam bool
	for _, r := range pw {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			harf = true
		case r >= '0' && r <= '9':
			rakam = true
		}
	}
	if !harf || !rakam {
		return false, "parola harf ve rakam içermeli (karışık)"
	}
	return true, ""
}

// MusteriDBKimlikGecerli: musteri-verdigi ad guvenli VE domain kullanicisiyla namespaced mi?
func MusteriDBKimlikGecerli(sk, s string) bool {
	if !GecerliDBKimlik(s) {
		return false
	}
	return s == sk || strings.HasPrefix(s, sk+"_")
}

// DBAdiBaskaKiracininMi: ad (veritabani YA DA DB kullanici adi) BASKA bir kiracinin
// DB ad-alanina mi dusuyor?
//
// 🔴 ONEK CAKISMASI: kiraci ad-alani "<kok>_..." bicimindedir; kok sistem
// kullanicisindan turer (c_girgin → "girgin"; tasima/uygulama yollari tam sk'yi da
// kullanir: "c_girgin_db"). SlugFromDomain tireyi alt cizgiye cevirdigi ve sonekte
// '_' serbest oldugu icin ad-alanlari IC ICE gecer: girgin.com'un ("girgin") "x_db"
// soneki, girgin-x.com'un ("girgin_x") varsayilan kullanicisi "girgin_x_db" ile AYNI
// addir. Zorunlu onek bu yuzden sahiplik kaniti DEGILDIR.
//
// Kural (en uzun kok kazanir): her domainin kokleri {sk, c_'siz sk}; ad bir koke
// esitse ya da "<kok>_" ile basliyorsa o koke duser. Adin dustugu bir kok BASKA bir
// domainin ve cagiranin kendi eslesen kokunden UZUNSA ad baskasinindir. Esit uzunluk
// (ayni kok dizgesi — c-foo.com'un "c_foo" oneki ile foo.com'un "c_foo" sk'si)
// cagirani ENGELLEMEZ; aksi halde o kiraci hic DB acamazdi. Oradaki olasi ad
// cakismasini yalniz-yeni semantik (MySQLCreateDB: var olan kimlige dokunmaz) kapatir.
//
// Karsilastirma BUYUK/KUCUK HARF DUYARSIZDIR (bkz. adKokUzunlugu): kokler hep kucuk
// harftir ama kullanici/DB adi A-Z tasiyabilir (yedekten kurtarilan wp-config, tasima,
// "kullanici ekle"). MariaDB "GIRGIN_X_DB"yi ayri hesap sayar; panel ise db_accounts'u
// utf8mb4_unicode_ci ile karsilastirir (db_name UNIQUE'i de harf duyarsiz). Duyarli
// karsilastirma komsunun ad-alanindaki buyuk harfli varyanti serbest birakir ve o varyant
// komsunun sorgularina karisirdi.
//
// Hata (DB okunamadi) durumunda true + err doner: cagiran REDDETMELI (fail-closed).
func DBAdiBaskaKiracininMi(ctx context.Context, db *sql.DB, domainID int64, ad string) (bool, error) {
	var sk string
	if err := db.QueryRowContext(ctx, `SELECT sistem_kullanici FROM domains WHERE id=?`, domainID).Scan(&sk); err != nil {
		return true, err
	}
	kendi := adKokUzunlugu(ad, sk)
	rows, err := db.QueryContext(ctx,
		`SELECT sistem_kullanici FROM domains WHERE id<>? AND sistem_kullanici<>''`, domainID)
	if err != nil {
		return true, err
	}
	defer rows.Close()
	for rows.Next() {
		var baska string
		if err := rows.Scan(&baska); err != nil {
			return true, err
		}
		if adKokUzunlugu(ad, baska) > kendi {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return true, err
	}
	return false, nil
}

// adKokUzunlugu: ad, sk'nin koklerinden ({sk, c_'siz sk}) birine dusuyorsa eslesen
// EN UZUN kokun uzunlugu; hicbirine dusmuyorsa 0. Harf duyarsiz (bkz. DBAdiBaskaKiracininMi).
func adKokUzunlugu(ad, sk string) int {
	ad, sk = strings.ToLower(ad), strings.ToLower(sk)
	en := 0
	for _, kok := range []string{sk, strings.TrimPrefix(sk, "c_")} {
		if kok == "" {
			continue
		}
		if (ad == kok || strings.HasPrefix(ad, kok+"_")) && len(kok) > en {
			en = len(kok)
		}
	}
	return en
}

// GrantDBKac: GRANT/REVOKE'ta backtick'li DB ADI icin LIKE-joker kacisi.
// 🔴 MySQL/MariaDB, GRANT ... ON `db`.* ifadesindeki db adinda `_` ve `%`
// karakterlerini backtick icinde OLSA BILE joker sayar. DB adlari <slug>_<sonek>
// biciminde alt-cizgiyle dolu ve sonek kiraci kontrolunde; kacissiz GRANT bir
// kiracinin yetkisini komsu DB'lere tasiyor (cross-tenant; canli kanitlandi).
// Literal istenirse \_ / \%% kacisi sart. YALNIZ GRANT/REVOKE icin; CREATE/DROP
// DATABASE adi LITERAL alir, orada KULLANMA.
func GrantDBKac(s string) string {
	s = strings.ReplaceAll(s, "_", `\_`)
	s = strings.ReplaceAll(s, "%", `\%`)
	return s
}

// sqlKac: MySQL string-literal ('...') icin kacis (ters-bolu + tek-tirnak)
func sqlKac(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "'", "\\'")
	return s
}

// ParolaGecerli: parola tek-satir mi? chpasswd/mysql satir-enjeksiyonunu engeller.
func ParolaGecerli(pw string) bool {
	return !strings.ContainsAny(pw, "\r\n\x00")
}

var (
	// ErrSistemDBHesabi: ad sunucu geneli bir MariaDB hesabina ait (root, mysql,
	// panel, pma, pureftpd, roundcube …). Panel bu hesaplari ASLA olusturmaz,
	// parolalarini ASLA degistirmez, kiraci DB'sine ASLA baglamaz.
	ErrSistemDBHesabi = errors.New("güvenlik: sistem/servis veritabanı hesabı kullanılamaz")
	// ErrDBKullaniciMevcut: MariaDB'de bu adla (herhangi bir host'ta) hesap zaten var.
	// ErrDBKimlikMevcut'un ozel halidir (errors.Is ikisini de tanir): yalniz-yeni
	// olusturucularin "var olana dokunulmadi" sinifi tek bir kontrolle yakalanir.
	ErrDBKullaniciMevcut = fmt.Errorf("%w (bu adla bir MariaDB hesabı)", ErrDBKimlikMevcut)
)

// sistemDBHesaplari: sunucu geneli MariaDB hesaplari (kucuk harf). Kaynak:
// girginospanel-install.sh (panel, pma), scripts/girginospanel-ftp-setup
// (pureftpd), posta/webmail kurulumu (mailadmin, mailuser, roundcube) ve
// MariaDB'nin kendi hesaplari. Kurulumu HENUZ yapilmamis servislerin adlari da
// burada: kiraci adi once "kapip" sonraki kurulumun CREATE USER IF NOT EXISTS +
// GRANT'iyle o servisin veritabanina yetki devralamasin.
//
// mailuser / mailadmin (181'de hem @localhost hem @127.0.0.1): hicbir kiraci ad-alanina
// dusmezler; yedekten kurtarmada (DBHesabiBaglanabilirMi) tek engel bu listedir.
var sistemDBHesaplari = map[string]bool{
	"root": true, "mysql": true, "mariadb": true, "debian-sys-maint": true,
	"panel": true, "girginospanel": true, "pma": true, "phpmyadmin": true,
	"pureftpd": true, "pure-ftpd": true,
	"roundcube": true, "roundcubemail": true,
	"mailadmin": true, "mailuser": true, "postfix": true, "dovecot": true,
	"pdns": true, "powerdns": true,
}

// SistemDBHesabiMi: ad sunucu geneli (kiraciya ait OLMAYAN) bir MariaDB hesabi mi?
// Buyuk/kucuk harf duyarsiz (temkinli): "Root" da reddedilir.
//
// Hicbir kiraci akisi bu adlari bir db_accounts satirina yazamaz, onlara GRANT veremez,
// onlari DUSUREMEZ, parolalarini degistiremez. Liste yalniz ilk savunma hattidir; asil
// kapi hesabin YETKILERINE bakar (DBHesabiBaglanabilirMi / MySQLDropDBSahipli →
// hesapYetkileriBuDomainde): listede olmayan bir servis hesabi da kendi (bu domaine ait
// olmayan) semasindaki yetkisi yuzunden reddedilir.
func SistemDBHesabiMi(u string) bool {
	l := strings.ToLower(strings.TrimSpace(u))
	if l == "" || strings.HasPrefix(l, "mysql.") || strings.HasPrefix(l, "mariadb.") {
		return true
	}
	return sistemDBHesaplari[l]
}

// mysqlKomut: ROOT soket kimligiyle mysql CLI. Uretimde DEGISMEZ; yalniz testler
// gecici MariaDB'ye yonlendirir (canli sunucunun hesaplarina dokunmamak icin).
var mysqlKomut = func(args ...string) *exec.Cmd { return exec.Command("mysql", args...) }

// mysqlHesapVarMi: MariaDB'de bu ADLA — HERHANGI bir host'ta — hesap (ya da rol)
// var mi? Yalniz 'x'@'localhost' sayilmaz: 'svc'@'%' varken ayni adla
// 'svc'@'localhost' olusturmak yerel baglantilarda servisi GOLGELER (daha ozgul
// host kazanir). Ad GecerliDBKimlik'ten gecmeli (tirnak yok → SQLi yok).
func mysqlHesapVarMi(dbUser string) (bool, error) {
	if !GecerliDBKimlik(dbUser) {
		return false, fmt.Errorf("güvenlik: geçersiz kullanıcı adı")
	}
	out, err := mysqlKomut("-N", "-B", "-e",
		"SELECT COUNT(*) FROM mysql.user WHERE User='"+sqlKac(dbUser)+"'").Output()
	if err != nil {
		return false, fmt.Errorf("mysql hesap sorgusu: %w", err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return false, fmt.Errorf("mysql hesap sorgusu: beklenmeyen çıktı")
	}
	return n > 0, nil
}

// FTPCreate: ftp_accounts tablosuna kayit ekler; parola at-rest $6$ crypt hash olarak saklanir (password_md5 sutunu, Pure-FTPd MYSQLCrypt crypt)
func FTPCreate(db *sql.DB, domainID int64, sistemKullanici, parola string, uidN, gidN int) error {
	home := "/home/" + sistemKullanici
	hash := FTPParolaHash(parola) // at-rest $6$ crypt (cleartext DEGIL)
	if hash == "" {
		return fmt.Errorf("güvenlik: FTP parola hash üretilemedi")
	}
	_, err := db.Exec(
		`INSERT INTO ftp_accounts(domain_id, username, password_md5, home_dir, uid_n, gid_n, status)
		 VALUES(?,?,?,?,?,?, 'active')
		 ON DUPLICATE KEY UPDATE password_md5=VALUES(password_md5), home_dir=VALUES(home_dir), uid_n=VALUES(uid_n), gid_n=VALUES(gid_n), status='active'`,
		domainID, sistemKullanici, hash, home, uidN, gidN)
	return err
}

// FTPUpdatePassword: mevcut FTP hesabinin parolasini guncelle
func FTPUpdatePassword(db *sql.DB, sistemKullanici, parola string) error {
	hash := FTPParolaHash(parola) // at-rest $6$ crypt
	if hash == "" {
		return fmt.Errorf("güvenlik: FTP parola hash üretilemedi")
	}
	_, err := db.Exec(
		`UPDATE ftp_accounts SET password_md5=? WHERE username=?`,
		hash, sistemKullanici)
	return err
}

// FTPDelete: hesabi ve domain ile birlikte CASCADE silinir, ama yine de explicit
func FTPDelete(db *sql.DB, sistemKullanici string) error {
	_, err := db.Exec(`DELETE FROM ftp_accounts WHERE username=?`, sistemKullanici)
	return err
}

// ErrDBKimlikMevcut: istenen veritabani ya da DB kullanicisi MariaDB'de ZATEN VAR.
// Yalniz-yeni olusturucular (MySQLCreateDB, MySQLCreateDBForUser, MySQLKullaniciEkle)
// var olan kimlige HIC DOKUNMADAN bu hatayla doner. Cagiran bu durumda o kimligi
// "geri alma" adina DUSURMEMELIDIR: kimlik baska bir kiraciya ait olabilir.
var ErrDBKimlikMevcut = errors.New("veritabanı ya da kullanıcı zaten var")

// mysqlCalistir: root-socket mysql CLI (mysqlKomut dikisi) ile ifade(ler) calistirir.
// Var olan kullanici (ERROR 1396 — CREATE USER) ErrDBKullaniciMevcut'a, var olan
// veritabani (ERROR 1007 — CREATE DATABASE) ErrDBKimlikMevcut'a sarilir (ikisi de
// errors.Is(err, ErrDBKimlikMevcut)).
func mysqlCalistir(ifadeler ...string) error {
	out, err := mysqlKomut("-e", strings.Join(ifadeler, " ")).CombinedOutput()
	if err == nil {
		return nil
	}
	cikti := strings.TrimSpace(string(out))
	switch {
	case strings.Contains(cikti, "ERROR 1396"):
		return fmt.Errorf("%w: %s", ErrDBKullaniciMevcut, cikti)
	case strings.Contains(cikti, "ERROR 1007"):
		return fmt.Errorf("%w: %s", ErrDBKimlikMevcut, cikti)
	}
	return fmt.Errorf("mysql exec: %s: %w", cikti, err)
}

// DBKullanicisiVarMi: MariaDB'de bu ADLA — HERHANGI bir host'ta — hesap var mi (bkz.
// mysqlHesapVarMi: 'svc'@'%' varken 'svc'@'localhost' olusturmak servisi golgeler)?
// Gecersiz ad ya da olculemeyen durum "var" sayilir (fail-closed): cagiran adi REDDETMELI.
func DBKullanicisiVarMi(dbUser string) bool {
	varMi, err := mysqlHesapVarMi(dbUser)
	return err != nil || varMi
}

// kullaniciYalnizBuDomainde: dbUser panelde (db_accounts) YALNIZ domainID'ye mi
// kayitli? En az bir satir bu domainde olmali, BASKA domainde satir olmamali. Ikinci
// kosul, duzeltme oncesi surumlerde onek cakismasiyla komsunun kullanicisini kendi
// domain_id'siyle yazdirmis (bkz. MySQLCreateDB) kayitlar uzerinden sonraki parola /
// GRANT islemlerinin komsuya uzanmasini keser.
//
// 🔴 BINARY: MariaDB hesap adlari harf DUYARLIDIR ("GIRGIN_X_DB" ayri hesaptir) ama
// db_accounts utf8mb4_unicode_ci'dir. Duyarsiz "db_user=?" saldirganin satirina
// yazdirdigi buyuk harfli varyanti komsunun "girgin_x_db"si ile ayni sayiyor ve
// komsunun kendi parola degisimini "birden cok domaine kayitli" diye kilitliyordu.
func kullaniciYalnizBuDomainde(db *sql.DB, domainID int64, dbUser string) error {
	var bu, baska int
	if err := db.QueryRow(
		`SELECT COALESCE(SUM(domain_id=?),0), COALESCE(SUM(domain_id<>?),0) FROM db_accounts WHERE BINARY db_user=?`,
		domainID, domainID, dbUser).Scan(&bu, &baska); err != nil {
		return fmt.Errorf("güvenlik: DB kullanıcısının sahipliği doğrulanamadı: %w", err)
	}
	if bu == 0 {
		return fmt.Errorf("güvenlik: DB kullanıcısı bu domaine kayıtlı değil")
	}
	if baska > 0 {
		return fmt.Errorf("güvenlik: DB kullanıcısı birden çok domaine kayıtlı — yönetici incelemesi gerekli")
	}
	return nil
}

// KullaniciYalnizBuDomainde: kullaniciYalnizBuDomainde'nin paket disi (yedekten geri
// yukleme) cagiranlar icin hali.
func KullaniciYalnizBuDomainde(db *sql.DB, domainID int64, dbUser string) error {
	return kullaniciYalnizBuDomainde(db, domainID, dbUser)
}

// SemaVarMi: semaVarMi'nin paket disi hali (olculemezse err; cagiran fail-closed karar verir).
func SemaVarMi(dbName string) (bool, error) {
	if !GecerliDBKimlik(dbName) {
		return false, fmt.Errorf("güvenlik: geçersiz veritabanı adı")
	}
	return semaVarMi(dbName)
}

// reDBHost: MariaDB hesap host'u (localhost, 127.0.0.1, %, alan adi, IPv6). Tirnak/ters
// bolu/bosluk YOK → sorguya guvenle girer.
var reDBHost = regexp.MustCompile(`\A[A-Za-z0-9._%:-]{1,255}\z`)

// kacisModu: mysql CLI oturumunda NO_BACKSLASH_ESCAPES'i kaldirir; asagidaki sorgular
// '\\_' kacisina ve sqlKac'a dayanir (sql_mode'dan BAGIMSIZ dogru calismali).
const kacisModu = "SET SESSION sql_mode=REPLACE(@@SESSION.sql_mode,'NO_BACKSLASH_ESCAPES','');"

// dbHesabiVarMi: '<kul>'@'<host>' MariaDB'de var mi? Olculemezse err.
func dbHesabiVarMi(kul, host string) (bool, error) {
	if !GecerliDBKimlik(kul) || !reDBHost.MatchString(host) {
		return true, fmt.Errorf("güvenlik: geçersiz hesap adı")
	}
	out, err := mysqlKomut("-N", "-B", "-e", kacisModu+
		" SELECT COUNT(*) FROM mysql.user WHERE User='"+sqlKac(kul)+"' AND Host='"+sqlKac(host)+"'").Output()
	if err != nil {
		return true, err
	}
	return strings.TrimSpace(string(out)) != "0", nil
}

// DBHesabiBaglanabilirMi: yedekten geri yuklemenin (kimlikTamamla, kullanicilar.sql)
// '<kul>'@'<host>' hesabini bu domainin db_accounts satirina YAZMASI ya da ona bu domainin
// veritabaninda GRANT vermesi guvenli mi? nil = guvenli.
//
// 🔴 Hesap adi kiracinin YAZABILDIGI dosyadan (wp-config.php/.env) ya da arsivdeki yetki
// dokumunden gelir. Eskiden tek suzgec root/debian-sys-maint/panel listesiydi:
//   - komsunun kullanicisi satira yaziliyordu → komsunun parola degisimi ve mevcut-kullanici
//     modu "birden cok domaine kayitli" diye KILITLENIYORDU (kiracilar arasi DoS);
//   - roundcube/pma/pureftpd gibi servis hesaplari satira yaziliyordu → DB silme / WP
//     kaldirma o hesabi DROP USER ile dusuruyordu (sunucu geneli kesinti);
//   - komsunun ad-alanindaki YENI bir ad (buyuk harfli varyant dahil) olusturuluyordu.
//
// Kurallar: sistem hesabi degil; BASKA domaine kayitli degil (BINARY); hesap VARSA
// yetkileri yalniz bu domainin DB'lerinde (hesapYetkiSayimi) VE bu domaine ait oldugunun
// en az bir kaniti var (bu domainde satiri ya da bu domainin DB'sinde yetkisi) — yalniz
// USAGE'li yabanci (izleme/servis) hesap baglanamaz; hesap YOKSA ad baska kiracinin
// ad-alanina dusmemeli. Olcum hatasi = red (fail-closed).
func DBHesabiBaglanabilirMi(ctx context.Context, db *sql.DB, domainID int64, kul, host string) error {
	if host == "" {
		host = "localhost"
	}
	if !GecerliDBKimlik(kul) || SistemDBHesabiMi(kul) || !reDBHost.MatchString(host) {
		return fmt.Errorf("güvenlik: bu DB hesabı bağlanamaz (sistem hesabı ya da geçersiz ad)")
	}
	var bu, baska int
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(domain_id=?),0), COALESCE(SUM(domain_id<>?),0) FROM db_accounts WHERE BINARY db_user=?`,
		domainID, domainID, kul).Scan(&bu, &baska); err != nil {
		return fmt.Errorf("güvenlik: DB hesabının sahipliği doğrulanamadı: %w", err)
	}
	if baska > 0 {
		return fmt.Errorf("güvenlik: DB hesabı başka bir domaine kayıtlı")
	}
	varMi, err := dbHesabiVarMi(kul, host)
	if err != nil {
		return fmt.Errorf("güvenlik: DB hesabı doğrulanamadı: %w", err)
	}
	if !varMi {
		if b, err := DBAdiBaskaKiracininMi(ctx, db, domainID, kul); err != nil || b {
			return fmt.Errorf("güvenlik: DB hesap adı başka bir kiracının ad alanında")
		}
		return nil
	}
	n, err := hesapYetkiSayimi(db, domainID, kul, host)
	if err != nil {
		return err
	}
	if bu == 0 && n == 0 {
		return fmt.Errorf("güvenlik: DB hesabı bu domaine ait değil")
	}
	return nil
}

// yetkiDesenCoz: mysql.db Db desenindeki \_ ve \% kacislarini cozer (SQL tarafindaki
// REPLACE(REPLACE(Db,'\\_','_'),'\\%','%') ile ayni).
func yetkiDesenCoz(desen string) string {
	return strings.NewReplacer(`\_`, "_", `\%`, "%").Replace(desen)
}

// kacissizYuzdeVar: mysql.db deseninde kacissiz (joker) '%' var mi?
func kacissizYuzdeVar(desen string) bool {
	for i := 0; i < len(desen); i++ {
		switch desen[i] {
		case '\\':
			i++
		case '%':
			return true
		}
	}
	return false
}

// yetkiKalintisiniTemizle: dbName ADINA bagli yetki satirlarini (mysql.db, tables_priv,
// columns_priv, procs_priv) siler.
//
// 🔴 ARTIK GRANT: DROP DATABASE yetki satirlarini SILMEZ; ayni ad sonra yeniden
// olusturuldugunda eski satir yeni DB'ye ANINDA uzanir. Zincir (kod okumasiyla kuruldu):
// kisa onekli kiraci komsunun ongorulebilir adini (girgin_x_main, <sk>_<kod>_<id>)
// kendi ana kullanicisiyla acar (yedekten "yeni ada geri yukle"), DB'yi siler (mevcut-
// kullanici modu → MySQLDropDBKeepUser, REVOKE YOK), komsu o adla DB actiginda
// saldirganin kullanicisi komsunun DB'sinde ALL yetkiye sahip olur. YENI olusturulan
// (CREATE DATABASE basarili = ad yeni) bir DB'de mesru onceki yetki OLAMAZ; dusurulen
// bir DB'nin yetkisi de kalmamalidir.
//
// Tam eslesen satirlar (kacisli "a\_b", kacissiz "a_b" ya da karisik) silinir. koru != ""
// ise '<koru>'@'localhost' satirlari korunur (yedekten geri yuklemede DB'nin kayitli
// sahibi). yeniDB iken ada JOKERLE uzanan baska bir desen (kacissiz '_' ile baska ad icin
// yazilmis eski satir) varsa: silmek o adin sahibini bozar, birakmak yeni DB'ye sizdirir
// → hata (fail-closed, yonetici incelemesi). Kacissiz '%' iceren desenleri kiraci
// akislari uretemez (GecerliDBKimlik); yoneticinin bilincli genis yetkisi sayilir ve
// yalniz gunluge yazilir.
func yetkiKalintisiniTemizle(dbName, koru string, yeniDB bool) error {
	if !GecerliDBKimlik(dbName) || (koru != "" && !GecerliDBKimlik(koru)) {
		return fmt.Errorf("güvenlik: geçersiz veritabanı ya da kullanıcı adı")
	}
	ad := sqlKac(dbName)
	if yeniDB {
		out, err := mysqlKomut("-N", "-B", "-r", "-e", kacisModu+
			" SELECT Db FROM mysql.db WHERE CAST('"+ad+"' AS BINARY) LIKE CAST(Db AS BINARY)").Output()
		if err != nil {
			return fmt.Errorf("güvenlik: eski yetkiler okunamadı: %w", err)
		}
		for _, desen := range strings.Split(string(out), "\n") {
			desen = strings.TrimRight(desen, "\r")
			if desen == "" || yetkiDesenCoz(desen) == dbName {
				continue
			}
			if kacissizYuzdeVar(desen) {
				log.Printf("hesaplar: %s adina uzanan genis yetki deseni (%s) korundu", dbName, desen)
				continue
			}
			return fmt.Errorf("güvenlik: bu ada jokerle uzanan eski bir yetki var — yönetici incelemesi gerekli")
		}
	}
	haric := ""
	if koru != "" {
		haric = " AND NOT (User='" + sqlKac(koru) + "' AND Host='localhost')"
	}
	return mysqlCalistir(kacisModu,
		`DELETE FROM mysql.db WHERE REPLACE(REPLACE(Db,'\\_','_'),'\\%','%')='`+ad+`'`+haric+`;`,
		`DELETE FROM mysql.tables_priv WHERE Db='`+ad+`'`+haric+`;`,
		`DELETE FROM mysql.columns_priv WHERE Db='`+ad+`'`+haric+`;`,
		`DELETE FROM mysql.procs_priv WHERE Db='`+ad+`'`+haric+`;`,
		"FLUSH PRIVILEGES;")
}

// YeniDBYetkiKalintisiniTemizle: yeni olusturulmus (ya da yedekten geri yuklemede YENI
// acilmis) dbName'in eski yetki satirlarini siler; koru = korunacak kayitli sahip
// ('<koru>'@'localhost', bos = yok). Jokerle uzanan eski desen varsa hata (bkz.
// yetkiKalintisiniTemizle) — cagiran DB'yi kullanima ACMAMALIDIR.
func YeniDBYetkiKalintisiniTemizle(dbName, koru string) error {
	return yetkiKalintisiniTemizle(dbName, koru, true)
}

// MySQLDropDBSahipli: domainID'ye KAYITLI veritabanini dusurur. Kullaniciya DROP USER
// YALNIZ hesabin guvenle bu domaine ait oldugu kanitlanirsa uygulanir; aksi halde yalniz
// DB dusurulur (yetki satirlari yine silinir — MySQLDropDBKeepUser).
//
// 🔴 Eskiden DeleteDatabase "kullanici baska satirda yok" ise, wpDBDusur ise HER ZAMAN
// MySQLDropDB (DROP USER) calistiriyordu. Satirdaki kullanici kiracinin yazabildigi
// kaynaktan (yedekten kurtarilan wp-config) gelebildigi icin roundcube / pma / pureftpd
// ya da komsunun kayitsiz kullanicisi DROP USER ile dusuruluyordu (sunucu geneli webmail
// / FTP / phpMyAdmin kesintisi; komsunun sitesi). Uygulama kaldirma (uygulama.Sil) ise
// kaydi HIC denetlemeden DB + kullaniciyi dusuruyordu.
//
// DB bu domaine kayitli degilse HICBIR SEY dusurulmez (hata).
func MySQLDropDBSahipli(db *sql.DB, domainID int64, dbName, dbUser string) error {
	if !GecerliDBKimlik(dbName) {
		return fmt.Errorf("güvenlik: geçersiz veritabanı adı")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM db_accounts WHERE domain_id=? AND BINARY db_name=?`,
		domainID, dbName).Scan(&n); err != nil {
		return fmt.Errorf("güvenlik: veritabanı sahipliği doğrulanamadı: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("güvenlik: veritabanı bu domaine kayıtlı değil — düşürülmedi")
	}
	if err := kullaniciDusurulebilir(db, domainID, dbName, dbUser); err != nil {
		if dbUser != "" {
			log.Printf("hesaplar: %s düşürülüyor, DB kullanıcısı %s KORUNDU: %v", dbName, dbUser, err)
		}
		return MySQLDropDBKeepUser(db, dbName)
	}
	return MySQLDropDB(db, dbName, dbUser)
}

// kullaniciDusurulebilir: dbName dusurulurken dbUser'a DROP USER uygulanabilir mi?
// Sistem hesabi degil; baska DB satirinda gecmiyor (mevcut-kullanici modu — onlari
// bozma); panelde yalniz bu domaine kayitli; MariaDB yetkileri yalniz bu domainin
// veritabanlarinda.
func kullaniciDusurulebilir(db *sql.DB, domainID int64, dbName, dbUser string) error {
	if dbUser == "" || !GecerliDBKimlik(dbUser) || SistemDBHesabiMi(dbUser) {
		return fmt.Errorf("güvenlik: sistem hesabı ya da geçersiz ad")
	}
	var paylasim int
	if err := db.QueryRow(`SELECT COUNT(*) FROM db_accounts WHERE BINARY db_user=? AND BINARY db_name<>?`,
		dbUser, dbName).Scan(&paylasim); err != nil {
		return fmt.Errorf("güvenlik: paylaşım ölçülemedi: %w", err)
	}
	if paylasim > 0 {
		return fmt.Errorf("kullanıcı başka veritabanlarında da kullanılıyor")
	}
	if err := kullaniciYalnizBuDomainde(db, domainID, dbUser); err != nil {
		return err
	}
	return hesapYetkileriBuDomainde(db, domainID, dbUser)
}

// DBAdAlaniKullanimda: yeni bir sistem kullanicisinin (sk) DB ad-alani ({sk, c_'siz sk}
// koklerine esit ya da "<kok>_" ile baslayan adlar) panelde (db_accounts) ya da MariaDB'de
// (hesap, yetki satiri, sema) ZATEN kullaniliyor mu? Yeni domain henuz hicbir sey
// sahiplenmedigi icin bulunan her ad BASKA birine aittir.
//
// 🔴 ONCEDEN KAPMA: ad-alani kapisi (DBAdiBaskaKiracininMi) yalniz VAR OLAN domainlere
// bakar. Kisa onekli kiraci (c_girgin) girgin-x.com henuz yokken "girgin_x_main" /
// "girgin_x_db" adlarini acabilir; girgin-x.com c_girgin_x olarak saglaninca varsayilan
// DB'si yalniz-yeni MySQLCreateDB'de reddedilir (DB'siz domain) ya da artik yetkiyle
// saldirgana acilirdi. provisioner.BenzersizKullanici bu durumda sonraki sayi ekine
// (c_girgin_x2) gecer. Harf duyarsiz (muhafazakar). Olcum hatasi "kullanimda degil"
// sayilir: cagiran bir dongudur (fail-closed sonsuz dongu olurdu); asil koruma yalniz-yeni
// olusturma + yetki kalintisi temizligidir.
func DBAdAlaniKullanimda(db *sql.DB, sk string) bool {
	for _, kok := range []string{sk, strings.TrimPrefix(sk, "c_")} {
		kok = strings.ToLower(kok)
		if !GecerliDBKimlik(kok) {
			continue
		}
		desen := strings.ReplaceAll(kok, "_", `\_`) + `\_%`
		if db != nil {
			var n int
			if err := db.QueryRow(
				`SELECT COUNT(*) FROM db_accounts WHERE db_name=? OR db_name LIKE ? OR db_user=? OR db_user LIKE ?`,
				kok, desen, kok, desen).Scan(&n); err == nil && n > 0 {
				return true
			}
		}
		k, d := sqlKac(kok), sqlKac(desen)
		q := kacisModu + " SELECT" +
			" (SELECT COUNT(*) FROM mysql.user WHERE LOWER(User)='" + k + "' OR LOWER(User) LIKE '" + d + "')" +
			" + (SELECT COUNT(*) FROM mysql.db WHERE LOWER(REPLACE(Db,'\\\\_','_'))='" + k +
			"' OR LOWER(REPLACE(Db,'\\\\_','_')) LIKE '" + d + "')" +
			" + (SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE LOWER(SCHEMA_NAME)='" + k +
			"' OR LOWER(SCHEMA_NAME) LIKE '" + d + "')"
		if out, err := mysqlKomut("-N", "-B", "-e", q).Output(); err == nil &&
			strings.TrimSpace(string(out)) != "0" {
			return true
		}
	}
	return false
}

// MySQLCreateDB: MariaDB'de YENI veritabani + YENI kullanici olustur + GRANT, sonra
// db_accounts'a kaydet.
//
// 🔴 YALNIZ-YENI SEMANTIK (DB kullanici oneki cakismasi): eskiden "CREATE USER IF NOT
// EXISTS" + kosulsuz "ALTER USER ... IDENTIFIED BY" + "CREATE DATABASE IF NOT EXISTS"
// ile "olustur YA DA DEGISTIR" calisiyordu. Ad-alanlari ic ice gectigi icin (bkz.
// DBAdiBaskaKiracininMi) kisa onekli kiraci komsunun "<onek>_db" kullanicisini kendi
// soneki gibi verip parolasini sectigi degere ceviriyor, kendi DB'sini ona GRANT
// ediyor ve satiri kendi domain_id'siyle yazdiriyordu (kiracilar arasi hesap ele
// gecirme). Simdi: CREATE USER (IF NOT EXISTS YOK) → kullanici varsa ERROR 1396 ile
// DURUR, hicbir sey degismez; CREATE DATABASE (IF NOT EXISTS YOK) → DB varsa yeni
// kullanici geri alinir. Sonraki adimlardan biri duserse YALNIZ bu cagrinin
// olusturdugu DB + kullanici geri alinir. Var olan (sahipligi dogrulanmis) bir
// kullaniciya DB baglamak MySQLCreateDBForUser'in, parolasini degistirmek
// MySQLChangePassword'un isidir.
func MySQLCreateDB(db *sql.DB, domainID int64, dbName, dbUser, dbPass string) error {
	if !GecerliDBKimlik(dbName) || !GecerliDBKimlik(dbUser) {
		return fmt.Errorf("güvenlik: geçersiz veritabanı adı veya kullanıcısı")
	}
	// 1) Kullanici — YALNIZ yoksa olusur (root socket auth ile).
	if err := mysqlCalistir(fmt.Sprintf("CREATE USER '%s'@'localhost' IDENTIFIED BY '%s';", dbUser, sqlKac(dbPass))); err != nil {
		return err
	}
	// Buradan sonra kullanici BU cagrinin: sonraki bir adim duserse geri alinir.
	kullaniciGeriAl := func() {
		_ = mysqlCalistir(fmt.Sprintf("DROP USER IF EXISTS '%s'@'localhost';", dbUser))
	}
	// 2) Veritabani — YALNIZ yoksa olusur (var olan DB'ye GRANT verilmez).
	if err := mysqlCalistir(fmt.Sprintf("CREATE DATABASE `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;", dbName)); err != nil {
		kullaniciGeriAl()
		return err
	}
	hepsiniGeriAl := func() {
		_ = mysqlCalistir(fmt.Sprintf("DROP DATABASE IF EXISTS `%s`;", dbName))
		_ = yetkiKalintisiniTemizle(dbName, "", false)
		kullaniciGeriAl()
	}
	// 2b) Ad YENI (CREATE DATABASE basarili) → bu ada bagli eski yetki satirlari artiktir
	// (onceden kapilip MySQLDropDBKeepUser ile silinmis ad, eski surum kalintisi). Silinmezse
	// eski kullanici bu yeni DB'ye ANINDA erisir. Jokerli eski desen → fail-closed.
	if err := yetkiKalintisiniTemizle(dbName, "", true); err != nil {
		hepsiniGeriAl()
		return err
	}
	// 3) GRANT
	if err := mysqlCalistir(
		fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost';", GrantDBKac(dbName), dbUser),
		"FLUSH PRIVILEGES;"); err != nil {
		hepsiniGeriAl()
		return err
	}
	// 4) panel DB metadata
	if _, err := db.Exec(
		`INSERT INTO db_accounts(domain_id, db_name, db_user, db_pass_plain, db_host)
		 VALUES(?,?,?,?, 'localhost')`,
		domainID, dbName, dbUser, gizli.SaklaBagli(dbPass, dbUser)); err != nil {
		hepsiniGeriAl()
		return err
	}
	return nil
}

// MySQLCreateDBForUser: YENI DB olustur + MEVCUT bir DB-kullanicisina GRANT ver
// (kullanicinin parolasina DOKUNMAZ — baska DB'leri bozmaz). db_accounts'a bu domain+db
// icin mevcut kullanicinin parolasiyla (db_pass_plain) yeni satir ekler.
// Kullanici panelde YALNIZ bu domaine kayitli olmali (kullaniciYalnizBuDomainde);
// cagiranin ayrica onek/sahiplik denetimi yapmasi beklenir.
func MySQLCreateDBForUser(db *sql.DB, domainID int64, dbName, dbUser string) error {
	if !GecerliDBKimlik(dbName) || !GecerliDBKimlik(dbUser) {
		return fmt.Errorf("güvenlik: geçersiz veritabanı adı veya kullanıcısı")
	}
	if err := kullaniciYalnizBuDomainde(db, domainID, dbUser); err != nil {
		return err
	}
	// Mevcut kullanicinin parolasi (yeni db_accounts satiri icin — phpMyAdmin SSO).
	// NOT: deger at-rest sifreli olabilir; asagida gizli.Sakla ile yeniden
	// saklanir ve Sakla zaten-sifreli degere DOKUNMAZ → cift sifreleme olmaz.
	var pass string
	// 🔴 domain_id + BINARY: harf duyarsiz "db_user=? LIMIT 1" komsunun buyuk/kucuk harf
	// varyantli satirindan parolayi okuyup bu domainin satirina kopyalayabilirdi.
	if err := db.QueryRow(
		`SELECT db_pass_plain FROM db_accounts WHERE domain_id=? AND BINARY db_user=? LIMIT 1`,
		domainID, dbUser).Scan(&pass); err != nil {
		return fmt.Errorf("mevcut kullanıcı parolası bulunamadı: %w", err)
	}
	// DB — YALNIZ yoksa olusur: var olan (baskasinin olabilecek) bir DB'ye GRANT verilmez.
	if err := mysqlCalistir(fmt.Sprintf("CREATE DATABASE `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;", dbName)); err != nil {
		return err
	}
	// Buradan sonra DB BU cagrinin: duserse yetki + DB geri alinir (DROP DATABASE
	// mysql.db yetki satirini SILMEZ; kalirsa ayni adla sonra acilan DB'ye uzanir).
	geriAl := func() {
		_ = mysqlCalistir(fmt.Sprintf("DROP DATABASE IF EXISTS `%s`;", dbName))
		_ = yetkiKalintisiniTemizle(dbName, "", false)
	}
	// Ad YENI → eski yetki satirlari artiktir (bkz. MySQLCreateDB 2b).
	if err := yetkiKalintisiniTemizle(dbName, "", true); err != nil {
		geriAl()
		return err
	}
	// Mevcut kullaniciya GRANT (CREATE/ALTER USER YOK → parola korunur).
	if err := mysqlCalistir(
		fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost';", GrantDBKac(dbName), dbUser),
		"FLUSH PRIVILEGES;"); err != nil {
		geriAl()
		return err
	}
	if _, err := db.Exec(
		`INSERT INTO db_accounts(domain_id, db_name, db_user, db_pass_plain, db_host)
		 VALUES(?,?,?,?, 'localhost')`,
		domainID, dbName, dbUser, gizli.SaklaGecis(pass, dbUser)); err != nil {
		geriAl()
		return err
	}
	return nil
}

// MySQLDropDB: DB ve user kaldir + metadata sil. DROP USER KOSULSUZDUR: kiraci akislari
// MySQLDropDBSahipli kullanir (kullanicinin bu domaine ait oldugunu kanitlar).
func MySQLDropDB(db *sql.DB, dbName, dbUser string) error {
	if !GecerliDBKimlik(dbName) || !GecerliDBKimlik(dbUser) {
		return fmt.Errorf("güvenlik: geçersiz veritabanı adı veya kullanıcısı")
	}
	stmts := []string{
		fmt.Sprintf("DROP DATABASE IF EXISTS `%s`;", dbName),
		fmt.Sprintf("DROP USER IF EXISTS '%s'@'localhost';", dbUser),
		"FLUSH PRIVILEGES;",
	}
	if out, err := mysqlKomut("-e", strings.Join(stmts, " ")).CombinedOutput(); err != nil {
		return fmt.Errorf("mysql drop: %s: %w", strings.TrimSpace(string(out)), err)
	}
	// Baska hesaplarin bu ada bagli yetkileri de gider (bkz. yetkiKalintisiniTemizle).
	// Duserse satir KALIR: yeniden silme denemesi temizligi tamamlar.
	if err := yetkiKalintisiniTemizle(dbName, "", false); err != nil {
		return fmt.Errorf("yetki kalıntısı temizlenemedi: %w", err)
	}
	_, err := db.Exec(`DELETE FROM db_accounts WHERE BINARY db_name=?`, dbName)
	return err
}

// MySQLDropDBKeepUser: yalniz DB'yi kaldir + metadata satirini sil; kullaniciyi DUSURME.
// Kullanici baska DB'lerde de kullaniliyorsa (mevcut-kullanici modu) onlari bozmamak icin
// tek-DB silmede kullanilir.
//
// 🔴 ARTIK GRANT: eskiden yalniz DROP DATABASE calisiyordu, REVOKE yoktu; kullanicinin
// (ve baska hesaplarin) bu ada bagli mysql.db satiri KALIYORDU. Ayni ad sonra (komsu
// tarafindan) yeniden olusturulunca eski kullanici o DB'de ANINDA ALL yetkiye sahip
// oluyordu. Simdi bu ada bagli tum yetki satirlari silinir; kullanicinin BASKA DB'lerdeki
// yetkileri korunur.
func MySQLDropDBKeepUser(db *sql.DB, dbName string) error {
	if !GecerliDBKimlik(dbName) {
		return fmt.Errorf("güvenlik: geçersiz veritabanı adı")
	}
	if out, err := mysqlKomut("-e",
		fmt.Sprintf("DROP DATABASE IF EXISTS `%s`;", dbName)).CombinedOutput(); err != nil {
		return fmt.Errorf("mysql drop: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if err := yetkiKalintisiniTemizle(dbName, "", false); err != nil {
		return fmt.Errorf("yetki kalıntısı temizlenemedi: %w", err)
	}
	_, err := db.Exec(`DELETE FROM db_accounts WHERE BINARY db_name=?`, dbName)
	return err
}

// MySQLDropAllForDomain: domain silinince ona ait tum DB'leri kaldir
func MySQLDropAllForDomain(db *sql.DB, domainID int64) error {
	rows, err := db.Query(`SELECT db_name, db_user FROM db_accounts WHERE domain_id=?`, domainID)
	if err != nil {
		return err
	}
	type cift struct{ ad, kul string }
	var liste []cift
	for rows.Next() {
		var c cift
		if err := rows.Scan(&c.ad, &c.kul); err != nil {
			continue
		}
		liste = append(liste, c)
	}
	rows.Close()
	for _, c := range liste {
		// 🔴 Kullanici BASKA bir domainin satirinda da geciyorsa (duzeltme oncesi onek
		// cakismasiyla komsunun kullanicisini kendi satirina yazdirmis eski kayit), bir
		// sistem/servis hesabiysa ya da yetkileri bu domainin disina tasiyorsa DROP USER
		// komsunun sitesini / sunucu geneli servisi dusururdu: MySQLDropDBSahipli yalniz
		// DB'yi kaldirir. Paylasilan (mevcut-kullanici modu) kullanici son DB'sinde duser.
		_ = MySQLDropDBSahipli(db, domainID, c.ad, c.kul)
	}
	return nil
}

// SyncSSHPassword: sistem (SSH) hesabının parolasını FTP parolasıyla eşitler.
// FTP parolası ftp_accounts.password_md5 sütununda $6$ crypt hash'i olarak
// tutulur (Pure-FTPd MYSQLCrypt crypt) ve /etc/shadow'a aynen yazılır — böylece
// SSH parolası = FTP parolası olur. 🔴 password_md5'e DÜZ METİN YAZMAYIN: FTP
// 530 verir (crypt(girilen, düz) ≠ düz). Yazıcılar FTPParolaHash kullanmalı
// (FTPCreate, FTPUpdatePassword, toplu devir, sifreheal).
func SyncSSHPassword(db *sql.DB, sistemKullanici string) error {
	if !strings.HasPrefix(sistemKullanici, "c_") {
		return fmt.Errorf("güvenlik: c_ prefiksli olmayan kullanıcı")
	}
	var pw string
	if err := db.QueryRow(
		`SELECT password_md5 FROM ftp_accounts WHERE username=? AND status='active'`,
		sistemKullanici).Scan(&pw); err != nil {
		return fmt.Errorf("ftp parola oku: %w", err)
	}
	if strings.TrimSpace(pw) == "" {
		return fmt.Errorf("ftp parolası boş")
	}
	// pw artik $6$ crypt hash'i (FTP ile paylasilan). /etc/shadow'a -e ile DOGRUDAN
	// yazilir → SSH parolasi = FTP parolasi (ayni hash). Eski duz-metin satir icin
	// (migrate edilmemis) geriye-uyumlu cleartext chpasswd.
	var cmd *exec.Cmd
	if IsFTPHash(pw) {
		cmd = exec.Command("chpasswd", "-e")
	} else {
		if !ParolaGecerli(pw) {
			return fmt.Errorf("güvenlik: parola geçersiz karakter içeriyor")
		}
		cmd = exec.Command("chpasswd")
	}
	cmd.Stdin = strings.NewReader(sistemKullanici + ":" + pw)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("chpasswd: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// LockSSHPassword: SSH kapatıldığında sistem parolasını kilitler (passwd -l).
func LockSSHPassword(sistemKullanici string) error {
	if !strings.HasPrefix(sistemKullanici, "c_") {
		return fmt.Errorf("güvenlik: c_ prefiksli olmayan kullanıcı")
	}
	out, err := exec.Command("passwd", "-l", sistemKullanici).CombinedOutput()
	if err != nil {
		return fmt.Errorf("passwd -l: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// DBVarMi — MariaDB'de bu adda bir veritabani var mi?
// "IF NOT EXISTS" sessizce no-op oldugu icin, MEVCUT bir DB'yi devralmayi
// onlemek adina kurulum oncesi acikca kontrol edilir.
func DBVarMi(dbName string) bool {
	if !GecerliDBKimlik(dbName) {
		return false
	}
	v, _ := semaVarMi(dbName)
	return v
}

// semaVarMi: DBVarMi'nin hata donduren hali (olcum yapilamadiysa err != nil);
// fail-closed karar vermesi gereken cagiranlar icin.
func semaVarMi(dbName string) (bool, error) {
	out, err := mysqlKomut("-N", "-e",
		"SELECT SCHEMA_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME='"+sqlKac(dbName)+"'").Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// MySQLKullaniciEkle: MEVCUT bir veritabanina kullanici + yetki ekler.
//
// 🔴 Neden MySQLCreateDB kullanilamiyor: o fonksiyon sonunda db_accounts'a INSERT
// yapar; kaydi ZATEN var olan (ornegin geri yuklemede yeniden kaydedilmis) bir DB
// icin bu UNIQUE(db_name) ihlali verir. Buradaki surum yalniz MariaDB tarafini
// kurar, panel metadata'sini cagiran gunceller.
//
// Kullanim yeri: veritabani var ama kullanicisi YOK durumu — DB panelden silinip
// yedekten geri yuklendiginde olusur (yedekte MySQL kullanicisi/GRANT bulunmaz).
//
// 🔴 YALNIZ YENI HESAP. Eski surum CREATE USER IF NOT EXISTS + kosulsuz ALTER USER
// yapiyordu (upsert): var olan hesapta CREATE no-op, ALTER ise o hesabin
// parolasini cagiranin degerine cekiyordu → bayi, db_user'i bos bir DB kaydi
// uzerinden root/mysql/panel/servis ya da baska kiracinin kayitsiz hesabini ele
// geciriyordu. Simdi: sistem hesabi → ErrSistemDBHesabi; ad MariaDB'de HERHANGI
// bir host'ta varsa → ErrDBKullaniciMevcut; olusturma IF NOT EXISTS'siz (yaris
// penceresinde hesap belirirse CREATE 1396 ile duser, GRANT calismaz).
func MySQLKullaniciEkle(dbName, dbUser, dbPass string) error {
	if !GecerliDBKimlik(dbName) || !GecerliDBKimlik(dbUser) {
		return fmt.Errorf("güvenlik: geçersiz veritabanı adı veya kullanıcısı")
	}
	if !ParolaGecerli(dbPass) {
		return fmt.Errorf("güvenlik: parola geçersiz karakter içeriyor")
	}
	if SistemDBHesabiMi(dbUser) {
		return ErrSistemDBHesabi
	}
	varMi, err := mysqlHesapVarMi(dbUser)
	if err != nil {
		return err // belirsizlik = olusturma YOK (fail-closed)
	}
	if varMi {
		return ErrDBKullaniciMevcut
	}
	// IF NOT EXISTS YOK: yaris penceresinde hesap belirirse ERROR 1396 →
	// ErrDBKullaniciMevcut (mysqlCalistir), var olan hesaba dokunulmaz.
	if err := mysqlCalistir(fmt.Sprintf("CREATE USER '%s'@'localhost' IDENTIFIED BY '%s';", dbUser, sqlKac(dbPass))); err != nil {
		return err
	}
	// Hesap BU cagrida olusturuldu → yetki. GRANT duserse az once olusturulan
	// hesap geri alinir (yetim hesap sonraki denemeyi "zaten var" ile kilitlemesin).
	if err := mysqlCalistir(
		fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost';", GrantDBKac(dbName), dbUser),
		"FLUSH PRIVILEGES;"); err != nil {
		_ = mysqlCalistir(fmt.Sprintf("DROP USER IF EXISTS '%s'@'localhost';", dbUser)) // bu cagrinin kullanicisi
		return err
	}
	return nil
}

// HealGrantJokerleri: MEVCUT kurulumlarda eski KACISSIZ GRANT'leri onarir.
// Eski "GRANT ALL ON `db`.*" satirlarinda db adindaki _ ve % LIKE-joker gibi
// davranip komsu tenant DB'lerine erisim aciyordu (cross-tenant, canli kanit).
// GrantDBKac artik YENI grant'leri kacisliyor; bu fonksiyon panel HER acilista
// MEVCUT grant'leri idempotent olarak kacisli hale getirir.
//
// Kaynak = panel'in KENDI db_accounts tablosu (c_*_db + wpu_* kayitlari; ikisi
// de MySQLCreateDB uzerinden yazildigi icin tam). DCL root-socket (mysql CLI),
// tenant DB olusturmayla ayni yol. Yalniz GecerliDBKimlik gecen adlar islenir
// => backtick/tirnak yok => SQLi kapali.
//
// Idempotency: mysql.db'de Db degeri TAM ad'a ("=" ile, _ literal) esitse
// kacissiz grant hala duruyor demektir; kacisli satirin Db'si "db\_x" oldugu
// icin bu esitligi saglamaz => zaten onarilmis satirlar atlanir.
func HealGrantJokerleri(ctx context.Context, db *sql.DB) {
	rows, err := db.QueryContext(ctx, "SELECT db_name, db_user FROM db_accounts")
	if err != nil {
		log.Printf("grant-joker heal: db_accounts okunamadi: %v", err)
		return
	}
	type cift struct{ ad, kul string }
	var liste []cift
	for rows.Next() {
		var c cift
		if err := rows.Scan(&c.ad, &c.kul); err == nil {
			liste = append(liste, c)
		}
	}
	rows.Close()

	var onarilan, atlanan, hata int
	for _, c := range liste {
		if !GecerliDBKimlik(c.ad) || !GecerliDBKimlik(c.kul) {
			atlanan++
			continue
		}
		esc := GrantDBKac(c.ad)
		if esc == c.ad {
			atlanan++ // _ veya % yok -> joker riski yok
			continue
		}
		// Kacissiz grant HALA duruyor mu? ("=" literal eslesme)
		q := fmt.Sprintf(
			"SELECT 1 FROM mysql.db WHERE User='%s' AND Host='localhost' AND Db='%s' LIMIT 1",
			c.kul, c.ad)
		out, err := mysqlKomut("-N", "-B", "-r", "-e", q).Output()
		if err != nil {
			log.Printf("grant-joker heal: durum sorgusu hata (%s): %v", c.ad, err)
			hata++
			continue
		}
		if strings.TrimSpace(string(out)) == "" {
			atlanan++ // zaten kacisli ya da grant yok
			continue
		}
		// REVOKE eski kacissiz + GRANT kacisli (root-socket, idempotent-safe).
		stmt := fmt.Sprintf(
			"REVOKE ALL PRIVILEGES ON `%s`.* FROM '%s'@'localhost'; "+
				"GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost'; FLUSH PRIVILEGES;",
			c.ad, c.kul, esc, c.kul)
		if o, e := mysqlKomut("-e", stmt).CombinedOutput(); e != nil {
			log.Printf("grant-joker heal HATA (%s): %s: %v", c.ad, strings.TrimSpace(string(o)), e)
			hata++
			continue
		}
		onarilan++
	}
	if onarilan > 0 || hata > 0 {
		log.Printf("grant-joker heal: %d onarildi, %d atlandi, %d hata", onarilan, atlanan, hata)
	}
}
