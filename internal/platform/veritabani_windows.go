//go:build windows

// veritabani_windows.go — YEREL VERITABANI YONETIMI: uc motor, ortak arayuz.
//
// 🔴 MOTOR DURUSTLUGU (bu dosyanin ana ilkesi):
//
//	mssql — Windows kimlik dogrulamasi (-E) ile PAROLASIZ, SORUNSUZ calisir;
//	        listeleme + olusturma + silme TAM uygulanir.
//	mysql / pgsql — yonetici parolasi kurulumda uretilir ama ajan.json'da
//	        SAKLANMAZ. Bu yuzden yonetim islemleri (olustur/sil) su an
//	        yapilamaz; listeleme parolasiz baglanti ile DENENIR, olmazsa ACIK
//	        hata doner. Sahte basari YOK: kullaniciya "yapildi" deyip hicbir
//	        sey yapmamak, yanlis is yapmaktan beterdir.
package platform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ── ortak tipler + hatalar ───────────────────────────────────────────────────

// MotorDurum — /vt-motorlar yanitindaki tek satir (etiketsiz, Go alan adlariyla
// — KalemDurum/SiteSonuc ile ayni sozlesme bicimi).
type MotorDurum struct {
	Tur    string // "mssql" | "mysql" | "pgsql"
	Ad     string
	Kurulu bool
}

// VeritabaniGoruntu — /vt-liste yanitindaki tek veritabani.
type VeritabaniGoruntu struct {
	Ad      string
	BoyutMB int64
}

// ErrVeriParolaGerekli — motor bir yonetici parolasi ister ama ajan onu
// SAKLAMAZ; islem bu faz icin yapilamaz (HTTP tarafi 422'ye cevirir).
var ErrVeriParolaGerekli = errors.New("veritabani motoru parola ayari bekliyor")

// ErrVeriKorumali — sistem veritabani; silme/degistirme ASLA yapilmaz
// (HTTP tarafi 422'ye cevirir).
var ErrVeriKorumali = errors.New("sistem veritabani korunuyor")

// 🔴 GECERSIZ GIRDI icin paket geneli ErrGecersizIstek (windows.go) YENIDEN
// kullanilir; HTTP tarafi onu zaten 400'e ceviriyor (bkz. olaylarUcu).

// vtAdRe (SQL enjeksiyon beyaz listesi) + vtKoseKacis + vtTirnakKacis OS-notr
// veritabani_ortak.go'ya tasindi (B-13); ayni pakette kullanilmaya devam eder.

// ── motor kesfi ──────────────────────────────────────────────────────────────

// VeritabaniMotorlari — uc motorun kurulu olup olmadigini dondurur. Kurulu
// tespiti mevcut desenle: ya istemci PATH'te ya da bilinen kurulum dizini var.
func VeritabaniMotorlari() []MotorDurum {
	return []MotorDurum{
		{Tur: "mssql", Ad: "Microsoft SQL Server", Kurulu: vtKomutVar("sqlcmd") || vtDizinVar(`C:\Program Files\Microsoft SQL Server`) || vtDosyaVar(sqlcmdBilinenYol)},
		{Tur: "mysql", Ad: "MySQL", Kurulu: vtKomutVar("mysql") || vtDizinVar(`C:\Program Files\MySQL`)},
		{Tur: "pgsql", Ad: "PostgreSQL", Kurulu: vtKomutVar("psql") || vtDizinVar(`C:\Program Files\PostgreSQL`)},
	}
}

// mssqlSunucu — named instance. SQL Express SQLEXPRESS adiyla kurulur; default
// instance (localhost) YOKTUR, bu yuzden "localhost\SQLEXPRESS" sart. TCP
// kesfi icin SQL Browser gerekir (kurucu onu Automatic yapar+baslatir).
const mssqlSunucu = `localhost\SQLEXPRESS`

// sqlcmdBilinenYol — kurucunun go-sqlcmd'yi koydugu sabit konum. SQL Server
// Engine sqlcmd ILE GELMEZ; kurucu (kurMSSQL) ODBC'siz tek-binary go-sqlcmd'yi
// buraya indirir. Once burada, yoksa PATH'te aranir.
const sqlcmdBilinenYol = `C:\Program Files\GirginOSPanel\sqlcmd.exe`

// sqlcmdCoz — kullanilacak sqlcmd yolunu dondurur: bilinen konum varsa o,
// yoksa PATH'teki "sqlcmd" (operator elle kurmus olabilir).
func sqlcmdCoz() string {
	if _, err := os.Stat(sqlcmdBilinenYol); err == nil {
		return sqlcmdBilinenYol
	}
	return "sqlcmd"
}

func vtKomutVar(ad string) bool {
	_, err := exec.LookPath(ad)
	return err == nil
}

func vtDizinVar(yol string) bool {
	fi, err := os.Stat(yol)
	return err == nil && fi.IsDir()
}

// vtDosyaVar — verilen yolda bir DOSYA var mi (sqlcmd bilinen konumu icin).
func vtDosyaVar(yol string) bool {
	fi, err := os.Stat(yol)
	return err == nil && !fi.IsDir()
}

func vtMotorGecerli(motor string) bool {
	switch motor {
	case "mssql", "mysql", "pgsql":
		return true
	}
	return false
}

// ── komut kosucu ─────────────────────────────────────────────────────────────

// vtCalistir — dis istemciyi zaman asimli kosar, stdout'u dondurur. Baglanti/
// prompt kilidini cagiranlar ayrica -l / --connect-timeout / -w ve
// PGCONNECT_TIMEOUT ile onler; bu context zaman asimi son emniyet kemeridir.
// Argumanlar exec.Command'e AYRI AYRI verilir; kabuk birlestirme YOK.
func vtCalistir(zamanAsimiSn int, cevre []string, ad string, arg ...string) (string, error) {
	ctx, iptal := context.WithTimeout(context.Background(), time.Duration(zamanAsimiSn)*time.Second)
	defer iptal()
	cmd := exec.CommandContext(ctx, ad, arg...)
	if len(cevre) > 0 {
		cmd.Env = append(os.Environ(), cevre...)
	}
	var cikti, hataCikti bytes.Buffer
	cmd.Stdout = &cikti
	cmd.Stderr = &hataCikti
	err := cmd.Run()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return cikti.String(), fmt.Errorf("%s: %d sn zaman asimi doldu", ad, zamanAsimiSn)
		}
		ayrinti := strings.TrimSpace(hataCikti.String())
		if ayrinti == "" {
			ayrinti = strings.TrimSpace(cikti.String())
		}
		return cikti.String(), fmt.Errorf("%s: %v — %s", ad, err, ayrinti)
	}
	return cikti.String(), nil
}

// vtSatirlariCoz — "ad<ayrac>boyut" satirlarini coz. Boyut DAIMA sayidir ve
// satirin SONUNDA durur; bu yuzden SON ayracta bolunur — ad ayrac icerse
// (bosluklu ad vb.) bile butun kalir. Sayiya cevrilemeyen satir (uyari/bilgi)
// sessizce atlanir.
func vtSatirlariCoz(cikti, ayrac string) []VeritabaniGoruntu {
	liste := make([]VeritabaniGoruntu, 0, 8)
	for _, satir := range strings.Split(cikti, "\n") {
		satir = strings.TrimSpace(strings.TrimRight(satir, "\r"))
		if satir == "" {
			continue
		}
		i := strings.LastIndex(satir, ayrac)
		if i < 0 {
			continue
		}
		ad := strings.TrimSpace(satir[:i])
		if ad == "" {
			continue
		}
		mb, err := strconv.ParseInt(strings.TrimSpace(satir[i+len(ayrac):]), 10, 64)
		if err != nil {
			continue
		}
		liste = append(liste, VeritabaniGoruntu{Ad: ad, BoyutMB: mb})
	}
	return liste
}

// ── listeleme ────────────────────────────────────────────────────────────────

// VeritabaniListe — motorun kullanici veritabanlarini (ad + MB) dondurur.
func VeritabaniListe(motor string) ([]VeritabaniGoruntu, error) {
	switch motor {
	case "mssql":
		return vtListeMSSQL()
	case "mysql":
		return vtListeMySQL()
	case "pgsql":
		return vtListePgSQL()
	default:
		return nil, fmt.Errorf("taninmayan motor %q: %w", motor, ErrGecersizIstek)
	}
}

// vtListeMSSQL — -E (Windows auth, PAROLASIZ) ile sistem db'leri haric tum
// veritabanlarini ve MB boyutlarini listeler. Bu motor SORUNSUZ calisir.
func vtListeMSSQL() ([]VeritabaniGoruntu, error) {
	const sorgu = "SET NOCOUNT ON; " +
		"SELECT DB_NAME(database_id), CAST(SUM(size)*8/1024 AS INT) " +
		"FROM sys.master_files " +
		"WHERE DB_NAME(database_id) NOT IN ('master','tempdb','model','msdb') " +
		"GROUP BY DB_NAME(database_id) ORDER BY DB_NAME(database_id)"
	// -h-1 basliksiz, -W sag bosluk kirp, -s| ayrac (bosluklu adlari korur),
	// -b hatada cikis kodu, -l 5 giris zaman asimi.
	out, err := vtCalistir(20, nil, sqlcmdCoz(), "-S", mssqlSunucu, "-E", "-h-1", "-W", "-s|", "-b", "-l", "5", "-Q", sorgu)
	if err != nil {
		return nil, fmt.Errorf("mssql listelenemedi: %w", err)
	}
	return vtSatirlariCoz(out, "|"), nil
}

// vtListeMySQL — 🔴 DURUSTLUK: root parolasi SAKLANMAZ; parolasiz -u root
// DENENIR (bazi kurulumlarda yerel root parolasiz baglanir), basarisizsa ACIK
// hata. -N basliksiz, -B sekmeli cikti; mysql -p'siz asla parola SORMAZ,
// --connect-timeout uzun beklemeyi keser.
func vtListeMySQL() ([]VeritabaniGoruntu, error) {
	const sorgu = "SELECT s.schema_name, " +
		"IFNULL(CAST(SUM(t.data_length + t.index_length)/1024/1024 AS UNSIGNED), 0) " +
		"FROM information_schema.schemata s " +
		"LEFT JOIN information_schema.tables t ON t.table_schema = s.schema_name " +
		"WHERE s.schema_name NOT IN ('information_schema','mysql','performance_schema','sys') " +
		"GROUP BY s.schema_name ORDER BY s.schema_name"
	out, err := vtCalistir(20, nil, "mysql", "-u", "root", "-N", "-B", "--connect-timeout=5", "-e", sorgu)
	if err != nil {
		return nil, fmt.Errorf("MySQL yonetici parolasi ajan.json'da yok, parolasiz baglanti da basarisiz — bu motor icin elle yapilandirma gerekli (%v): %w", err, ErrVeriParolaGerekli)
	}
	return vtSatirlariCoz(out, "\t"), nil
}

// vtListePgSQL — 🔴 DURUSTLUK: pgsql superuser parolasi kurulum gunlugune
// basilir ama SAKLANMAZ; -w ile psql parola SORMAZ (gerekiyorsa hemen hata),
// PGCONNECT_TIMEOUT baglanti beklemesini sinirlar.
func vtListePgSQL() ([]VeritabaniGoruntu, error) {
	const sorgu = "SELECT datname, pg_database_size(datname)/1024/1024 " +
		"FROM pg_database WHERE NOT datistemplate ORDER BY datname"
	out, err := vtCalistir(20, []string{"PGCONNECT_TIMEOUT=5"}, "psql", "-w", "-U", "postgres", "-tAc", sorgu)
	if err != nil {
		return nil, fmt.Errorf("PostgreSQL parolasi saklanmiyor, pgsql yonetimi sonraki faz (%v): %w", err, ErrVeriParolaGerekli)
	}
	return vtSatirlariCoz(out, "|"), nil
}

// ── olusturma / silme ────────────────────────────────────────────────────────

// VeritabaniOlustur — yalniz mssql TAM uygular (-E): veritabani + login + user
// + db_owner. mysql/pgsql parola ayari bekler (acik hata).
func VeritabaniOlustur(motor, ad, kullanici, parola string) error {
	if !vtMotorGecerli(motor) {
		return fmt.Errorf("taninmayan motor %q: %w", motor, ErrGecersizIstek)
	}
	if motor != "mssql" {
		return fmt.Errorf("%s icin veritabani olusturma parola ayari bekliyor (sonraki faz): %w", motor, ErrVeriParolaGerekli)
	}
	// 🔴 SQL ENJEKSIYONU: ad ve kullanici SIKI dogrulanir (string birlestirmeyle
	// SQL kuruldugu icin tek savunma budur).
	if !vtAdRe.MatchString(ad) {
		return fmt.Errorf("gecersiz veritabani adi %q (harf/_ ile baslar, harf/rakam/_ , 1-64): %w", ad, ErrGecersizIstek)
	}
	if !vtAdRe.MatchString(kullanici) {
		return fmt.Errorf("gecersiz kullanici adi %q: %w", kullanici, ErrGecersizIstek)
	}
	if parola == "" {
		return fmt.Errorf("parola bos olamaz: %w", ErrGecersizIstek)
	}
	// 🔴 SQL ENJEKSIYONU (sqlcmd CLIENT-SIDE): parola N'...' icine gomulur; tirnak
	// ciftleme yalniz SUNUCU parse'inda korur. sqlcmd -Q metnini ONCE satir-satir
	// isler: tek basina "GO" batch ayiraci, satir-basi ":" sqlcmd komutu, "$(...)"
	// degisken ikamesi — string literalini BILMEZ. Bu yuzden CR/LF ve "$(" YASAK
	// (yeni satir = GO/":" enjeksiyon kapisi); ayrica sqlcmd cagrilarinda -x.
	if strings.ContainsAny(parola, "\r\n") || strings.Contains(parola, "$(") {
		return fmt.Errorf("parola gecersiz karakter iceriyor (yeni satir veya $( ): %w", ErrGecersizIstek)
	}
	// 🔴 IKINCI KAT SAVUNMA: regex ']' i zaten eledi; yine de koseli parantez
	// icinde ']' -> ']]', string literalinde ' -> '' kacisi uygulanir.
	adK := vtKoseKacis(ad)
	kulK := vtKoseKacis(kullanici)
	// 🔴 IKI AYRI CAGRI: CREATE DATABASE ile USE ayni batch'te OLAMAZ — SQL
	// Server, CREATE DATABASE batch'i kapanmadan yeni db'ye baglanamaz ve
	// "Msg 911: database does not exist" doner (gercek VM'de yakalandi). 1.
	// cagri master'da db+login yaratir; 2. cagri `-d <db>` ile o db baglaminda
	// kullanici+rol ekler (USE gerekmez, db artik commit'li).
	//
	// 🔴 IDEMPOTENCY (stabilite rehberi #5): her ifade "varsa dokunma / hedefe
	// yakinsat" bicimindedir. Iki cagri arasinda ajan cokup istek tekrarlanirsa
	// (db+login yaratildi, user yaratilamadi) ikinci deneme CREATE DATABASE'te
	// "already exists" ile PATLAMAMALI — yarim durum kurtarilabilir olmali.
	// Login VARSA parola YENIDEN uygulanir (ALTER): eskiden IF NOT EXISTS eski
	// parolayi sessizce korur, panelin gosterdigi yeni parolayla site baglanamazdi
	// (yetim login tuzagi — bkz. VeritabaniSil temizligi).
	sorgu1 := fmt.Sprintf("SET NOCOUNT ON; "+
		"IF DB_ID(N'%s') IS NULL CREATE DATABASE [%s]; "+
		"IF NOT EXISTS (SELECT 1 FROM sys.server_principals WHERE name = N'%s') "+
		"CREATE LOGIN [%s] WITH PASSWORD = N'%s'; "+
		"ELSE ALTER LOGIN [%s] WITH PASSWORD = N'%s';",
		vtTirnakKacis(ad), adK, vtTirnakKacis(kullanici), kulK, vtTirnakKacis(parola), kulK, vtTirnakKacis(parola))
	if _, err := vtCalistir(60, nil, sqlcmdCoz(), "-S", mssqlSunucu, "-E", "-x", "-b", "-l", "5", "-Q", sorgu1); err != nil {
		return fmt.Errorf("mssql veritabani/login olusturulamadi: %w", err)
	}
	// 2. cagri: USER + db_owner. Ikisi de "varsa atla" ile sarili — CREATE USER
	// mevcut kullanicida "already exists", ALTER ROLE mevcut uyede "already a
	// member" hatasi verir; guardsiz tekrar deneme yarim durumu kurtaramazdi.
	sorgu2 := fmt.Sprintf("SET NOCOUNT ON; "+
		"IF NOT EXISTS (SELECT 1 FROM sys.database_principals WHERE name = N'%s') "+
		"CREATE USER [%s] FOR LOGIN [%s]; "+
		"IF NOT EXISTS (SELECT 1 FROM sys.database_role_members drm "+
		"JOIN sys.database_principals r ON r.principal_id = drm.role_principal_id AND r.name = 'db_owner' "+
		"JOIN sys.database_principals m ON m.principal_id = drm.member_principal_id AND m.name = N'%s') "+
		"ALTER ROLE db_owner ADD MEMBER [%s];",
		vtTirnakKacis(kullanici), kulK, kulK, vtTirnakKacis(kullanici), kulK)
	if _, err := vtCalistir(60, nil, sqlcmdCoz(), "-S", mssqlSunucu, "-E", "-x", "-d", ad, "-b", "-l", "5", "-Q", sorgu2); err != nil {
		return fmt.Errorf("mssql kullanici/yetki olusturulamadi (db yaratildi): %w", err)
	}
	return nil
}

// VeritabaniSil — yalniz mssql: DROP DATABASE. 🔴 SISTEM DB KORUMASI:
// master/tempdb/model/msdb ASLA silinmez.
func VeritabaniSil(motor, ad string) error {
	if !vtMotorGecerli(motor) {
		return fmt.Errorf("taninmayan motor %q: %w", motor, ErrGecersizIstek)
	}
	if !vtAdRe.MatchString(ad) {
		return fmt.Errorf("gecersiz veritabani adi %q: %w", ad, ErrGecersizIstek)
	}
	// 🔴 SISTEM DB KORUMASI once gelir: motor parolasiz olsa bile sistem db
	// istegi acik korunma hatasiyla reddedilir.
	if vtSistemDBmi(motor, ad) {
		return fmt.Errorf("%q sistem veritabani, silinemez: %w", ad, ErrVeriKorumali)
	}
	if motor != "mssql" {
		return fmt.Errorf("%s icin veritabani silme parola ayari bekliyor (sonraki faz): %w", motor, ErrVeriParolaGerekli)
	}
	adK := vtKoseKacis(ad)
	// 🔴 YETIM LOGIN TEMIZLIGI (Linux tenant_orphan_race dersinin MSSQL karsiligi):
	// olusturma db + LOGIN (SUNUCU duzeyi) + USER (db duzeyi) yaratir. DROP DATABASE
	// yalniz db + USER'i goturur; LOGIN sunucuda ORTA KALIR. Yetim login birikir ve
	// ayni kullanici adiyla yeniden olusturmada eski parolayi tutabilir. Silmeden
	// ONCE bu db'ye eslenen login adlarini topla; db'yi dus; sonra BASKA HICBIR
	// db'de eslesmesi kalmayanlari dus (fail-safe: eslesme olculemezse DOKUNMA).
	yetimAdaylari := vtLoginAdlariniTopla(ad)
	// SINGLE_USER + ROLLBACK IMMEDIATE: acik baglantilar DROP'u kilitlemesin.
	sorgu := fmt.Sprintf("ALTER DATABASE [%s] SET SINGLE_USER WITH ROLLBACK IMMEDIATE; DROP DATABASE [%s];", adK, adK)
	if _, err := vtCalistir(60, nil, sqlcmdCoz(), "-S", mssqlSunucu, "-E", "-x", "-b", "-l", "5", "-Q", sorgu); err != nil {
		// 🔴 DROP yarida kaldiysa (baska baglanti tek koltugu kaptiysa) db
		// SINGLE_USER'da ASILI kalir = musteri db'si erisilemez. Erisimi geri ver
		// (best-effort; db dustuyse zararsiz, duruyorsa MULTI_USER ile acilir).
		_, _ = vtCalistir(30, nil, sqlcmdCoz(), "-S", mssqlSunucu, "-E", "-x", "-b", "-l", "5", "-Q",
			fmt.Sprintf("IF DB_ID(N'%s') IS NOT NULL ALTER DATABASE [%s] SET MULTI_USER;", vtTirnakKacis(ad), adK))
		return fmt.Errorf("mssql veritabani silinemedi: %w", err)
	}
	for _, login := range yetimAdaylari {
		vtYetimLoginDusur(login) // best-effort; hata yutulur (db zaten dustu)
	}
	return nil
}

// vtLoginAdlariniTopla — verilen db baglaminda, gercek bir SUNUCU login'ine
// eslenen db kullanicilarinin login adlarini dondurur (SID eslemesiyle). 'sa' ve
// sistem principal'leri elenir. Sorgu basarisizsa bos liste — yetim temizligi
// atlanir, ASLA yanlis login dusurulmez.
func vtLoginAdlariniTopla(db string) []string {
	out, err := vtCalistir(20, nil, sqlcmdCoz(), "-S", mssqlSunucu, "-E", "-d", db,
		"-h-1", "-W", "-s|", "-b", "-l", "5", "-Q",
		"SET NOCOUNT ON; SELECT sp.name FROM sys.database_principals dp "+
			"JOIN sys.server_principals sp ON sp.sid = dp.sid "+
			"WHERE sp.type IN ('S','U') AND sp.name NOT IN ('sa','dbo');")
	if err != nil {
		return nil
	}
	var adlar []string
	for _, satir := range strings.Split(out, "\n") {
		ad := strings.TrimSpace(strings.TrimRight(satir, "\r"))
		if ad != "" && vtAdRe.MatchString(ad) { // yalniz bizim urettigimiz bicimdeki adlar
			adlar = append(adlar, ad)
		}
	}
	return adlar
}

// vtYetimLoginDusur — login'i YALNIZ hicbir kullanici db'sinde kullanici
// eslemesi kalmadiysa dusurur. Cursor tum cevrimici kullanici db'lerini (id>4)
// tarar; herhangi birinde eslesme VARSA ya da tarama HATA verirse login KORUNUR
// (fail-safe — baska bir site kullaniyor olabilir). login adi cagiran tarafta
// vtAdRe'den gecti; yine de koseli-parantez kacisi uygulanir.
func vtYetimLoginDusur(login string) {
	loginK := vtKoseKacis(login)
	sorgu := fmt.Sprintf("SET NOCOUNT ON; "+
		"IF EXISTS (SELECT 1 FROM sys.server_principals WHERE name = N'%s' AND type IN ('S','U')) "+
		"BEGIN "+
		"  DECLARE @sid VARBINARY(85) = (SELECT sid FROM sys.server_principals WHERE name = N'%s'); "+
		"  DECLARE @kullanan INT = 0, @db SYSNAME, @sql NVARCHAR(MAX); "+
		"  DECLARE c CURSOR LOCAL FAST_FORWARD FOR SELECT name FROM sys.databases WHERE state = 0 AND database_id > 4; "+
		"  OPEN c; FETCH NEXT FROM c INTO @db; "+
		"  WHILE @@FETCH_STATUS = 0 AND @kullanan = 0 "+
		"  BEGIN "+
		"    SET @sql = N'SELECT @c = COUNT(*) FROM ' + QUOTENAME(@db) + N'.sys.database_principals WHERE sid = @s'; "+
		"    BEGIN TRY EXEC sp_executesql @sql, N'@s VARBINARY(85), @c INT OUTPUT', @s = @sid, @c = @kullanan OUTPUT; END TRY "+
		"    BEGIN CATCH SET @kullanan = 1; END CATCH; "+
		"    FETCH NEXT FROM c INTO @db; "+
		"  END "+
		"  CLOSE c; DEALLOCATE c; "+
		"  IF @kullanan = 0 DROP LOGIN [%s]; "+
		"END",
		vtTirnakKacis(login), vtTirnakKacis(login), loginK)
	_, _ = vtCalistir(30, nil, sqlcmdCoz(), "-S", mssqlSunucu, "-E", "-b", "-l", "5", "-Q", sorgu)
}

// vtSistemDBmi — motorun dokunulmaz sistem veritabani mi (silme korumasi).
func vtSistemDBmi(motor, ad string) bool {
	switch motor {
	case "mssql":
		switch strings.ToLower(ad) {
		case "master", "tempdb", "model", "msdb":
			return true
		}
	case "mysql":
		switch strings.ToLower(ad) {
		case "information_schema", "mysql", "performance_schema", "sys":
			return true
		}
	case "pgsql":
		switch strings.ToLower(ad) {
		case "postgres", "template0", "template1":
			return true
		}
	}
	return false
}
