package sirrota

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"girginospanel/internal/config"
)

// ── sahte DB / Valkey ────────────────────────────────────────────────

type sahteDB struct {
	kul        string
	hostlar    []string
	parola     map[string]string // host → geçerli parola
	ayarHata   error             // ParolaAyarla her çağrıda bu hatayı döner (yine de ilk hostu değiştirir: kısmi uygulama)
	dogrulaRed bool              // Dogrula her zaman reddeder
	cagri      []string
}

func yeniSahteDB(kul, pw string, hostlar ...string) *sahteDB {
	d := &sahteDB{kul: kul, hostlar: hostlar, parola: map[string]string{}}
	for _, h := range hostlar {
		d.parola[h] = pw
	}
	return d
}

func (d *sahteDB) Hostlar(_ context.Context, kul string) ([]string, error) {
	if kul != d.kul {
		return nil, nil
	}
	return d.hostlar, nil
}

func (d *sahteDB) ParolaAyarla(_ context.Context, kul string, hostlar []string, pw string) error {
	d.cagri = append(d.cagri, fmt.Sprintf("ALTER %s %v", kul, hostlar))
	if d.ayarHata != nil && pw != d.ilkParola() {
		d.parola[hostlar[0]] = pw // kısmi uygulama
		return d.ayarHata
	}
	for _, h := range hostlar {
		d.parola[h] = pw
	}
	return nil
}

var ilkParolaKayit = map[*sahteDB]string{}

func (d *sahteDB) ilkParola() string { return ilkParolaKayit[d] }

func (d *sahteDB) Dogrula(_ context.Context, dsn string) error {
	if d.dogrulaRed {
		return errors.New("sahte: bağlantı reddedildi")
	}
	for _, h := range d.hostlar {
		if !strings.Contains(dsn, ":"+d.parola[h]+"@") {
			return errors.New("sahte: erişim reddedildi")
		}
	}
	return nil
}

type sahteRedis struct {
	parola     string
	degistHata error
	dogrulaRed bool
}

func (r *sahteRedis) ParolaDegistir(_ context.Context, eski, yeni string) error {
	if eski != r.parola {
		return errors.New("WRONGPASS")
	}
	if r.degistHata != nil {
		return r.degistHata
	}
	r.parola = yeni
	return nil
}

func (r *sahteRedis) Dogrula(_ context.Context, p string) error {
	if r.dogrulaRed || p != r.parola {
		return errors.New("WRONGPASS")
	}
	return nil
}

// ── düzenek ─────────────────────────────────────────────────────────

const (
	eskiJWT   = "eski-jwt-sirri-0123456789abcdef0123456789abcdef"
	eskiPw    = "eskidbparolasi0123"
	eskiRedis = "eskiredisparolasi99"
)

func eskiDSN() string {
	return "panel:" + eskiPw + "@tcp(127.0.0.1:3306)/panel?parseTime=true&charset=utf8mb4&collation=utf8mb4_unicode_ci"
}

func eskiEnv() string {
	return "# yorum korunur\nPANEL_LISTEN=127.0.0.1:8080\nPANEL_ENV=production\nPANEL_DB_DSN=" + eskiDSN() +
		"\nPANEL_JWT_SECRET=" + eskiJWT + "\nPANEL_JWT_LIFETIME_SEC=43200\nPANEL_REDIS_ADMIN_PASS=" + eskiRedis +
		"\nPANEL_LISANS_SUNUCU=https://ornek\n"
}

type duzenek struct {
	r     *Rotator
	db    *sahteDB
	redis *sahteRedis
	dir   string
	ekYol string
}

func kur(t *testing.T) *duzenek {
	t.Helper()
	dir := t.TempDir()
	env := filepath.Join(dir, "env")
	if err := os.WriteFile(env, []byte(eskiEnv()), 0o600); err != nil {
		t.Fatal(err)
	}
	ek := filepath.Join(dir, "eklenti-tehdit.env")
	if err := os.WriteFile(ek, []byte("TEHDIT_DSN="+eskiDSN()+"\nBASKA=1\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	// başka DSN taşıyan eklenti dosyası DEĞİŞMEMELİ
	if err := os.WriteFile(filepath.Join(dir, "eklenti-mail.env"), []byte("MAIL_DSN=mail:x@tcp(127.0.0.1:3306)/mail\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	db := yeniSahteDB("panel", eskiPw, "127.0.0.1", "localhost")
	ilkParolaKayit[db] = eskiPw
	rd := &sahteRedis{parola: eskiRedis}
	sayac := 0
	r := &Rotator{
		EnvYolu:      env,
		BekleyenYolu: filepath.Join(dir, ".sir-yenile-bekleyen"),
		EkDizin:      dir,
		DB:           db,
		Redis:        rd,
		Uret: func(n int) (string, error) {
			sayac++
			return fmt.Sprintf("yeni%02d%s", sayac, strings.Repeat("a", n*2-6)), nil
		},
		Log: t.Logf,
	}
	return &duzenek{r: r, db: db, redis: rd, dir: dir, ekYol: ek}
}

func oku(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mod(t *testing.T, p string) os.FileMode {
	t.Helper()
	st, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	return st.Mode().Perm()
}

// ── testler ─────────────────────────────────────────────────────────

func TestYenile_HepsiBasarili(t *testing.T) {
	d := kur(t)
	s, err := d.r.Yenile(context.Background(), Hepsi)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(s.Yenilenen, ",") != "jwt,db,redis" {
		t.Fatalf("yenilenen: %v", s.Yenilenen)
	}
	m := config.EnvAyristir([]byte(oku(t, d.r.EnvYolu)))
	if m["PANEL_JWT_SECRET"] == eskiJWT || len(m["PANEL_JWT_SECRET"]) < 32 {
		t.Error("JWT yenilenmedi")
	}
	if m["PANEL_REDIS_ADMIN_PASS"] == eskiRedis || m["PANEL_REDIS_ADMIN_PASS"] != d.redis.parola {
		t.Error("Redis env ↔ Valkey uyumsuz")
	}
	if m["PANEL_DB_DSN"] == eskiDSN() || d.db.Dogrula(context.Background(), m["PANEL_DB_DSN"]) != nil {
		t.Error("DB env ↔ MariaDB uyumsuz")
	}
	for _, h := range d.db.hostlar {
		if d.db.parola[h] == eskiPw {
			t.Errorf("host %s eski parolada kaldı", h)
		}
	}
	if !strings.HasSuffix(m["PANEL_DB_DSN"], "@tcp(127.0.0.1:3306)/panel?parseTime=true&charset=utf8mb4&collation=utf8mb4_unicode_ci") {
		t.Errorf("DSN'in parola dışı kısmı değişti")
	}
	icerik := oku(t, d.r.EnvYolu)
	for _, k := range []string{"# yorum korunur\n", "PANEL_LISTEN=127.0.0.1:8080\n", "PANEL_LISANS_SUNUCU=https://ornek\n"} {
		if !strings.Contains(icerik, k) {
			t.Errorf("sır olmayan satır kayboldu: %q", k)
		}
	}
	if mod(t, d.r.EnvYolu) != 0o600 {
		t.Errorf("env modu %o", mod(t, d.r.EnvYolu))
	}
	if d.r.BekleyenVar() {
		t.Error("bekleyen dosyası kaldı")
	}
	ek := config.EnvAyristir([]byte(oku(t, d.ekYol)))
	if ek["TEHDIT_DSN"] != m["PANEL_DB_DSN"] || ek["BASKA"] != "1" {
		t.Error("eklenti DSN kopyası hizalanmadı")
	}
	if mod(t, d.ekYol) != 0o600 {
		t.Errorf("eklenti env modu %o (0600 olmalı)", mod(t, d.ekYol))
	}
	if len(s.Eklentiler) != 1 || s.Eklentiler[0] != "tehdit" {
		t.Errorf("eklentiler: %v", s.Eklentiler)
	}
	if !strings.Contains(oku(t, filepath.Join(d.dir, "eklenti-mail.env")), "mail:x@") {
		t.Error("ilgisiz eklenti env'i değişti")
	}
}

func envDegismedi(t *testing.T, d *duzenek) {
	t.Helper()
	if oku(t, d.r.EnvYolu) != eskiEnv() {
		t.Error("hata yolunda env değişti")
	}
	for _, h := range d.db.hostlar {
		if d.db.parola[h] != eskiPw {
			t.Errorf("hata yolunda DB host %s eski parolaya dönmedi", h)
		}
	}
	if d.redis.parola != eskiRedis {
		t.Error("hata yolunda Valkey eski parolaya dönmedi")
	}
	if d.r.BekleyenVar() {
		t.Error("hata yolunda bekleyen dosyası kaldı")
	}
	if !strings.Contains(oku(t, d.ekYol), eskiDSN()) {
		t.Error("hata yolunda eklenti env'i değişti")
	}
}

func TestYenile_DBDogrulamaDuserseGeriAlinir(t *testing.T) {
	d := kur(t)
	d.db.dogrulaRed = true
	if _, err := d.r.Yenile(context.Background(), Hepsi); err == nil {
		t.Fatal("hata bekleniyordu")
	}
	envDegismedi(t, d)
}

func TestYenile_DBKismiAlterGeriAlinir(t *testing.T) {
	d := kur(t)
	d.db.ayarHata = errors.New("ALTER düştü")
	if _, err := d.r.Yenile(context.Background(), []string{DB}); err == nil {
		t.Fatal("hata bekleniyordu")
	}
	envDegismedi(t, d)
}

func TestYenile_RedisDuserseDBGeriAlinir(t *testing.T) {
	d := kur(t)
	d.redis.degistHata = errors.New("ACL SAVE düştü")
	if _, err := d.r.Yenile(context.Background(), Hepsi); err == nil {
		t.Fatal("hata bekleniyordu")
	}
	envDegismedi(t, d)
}

func TestYenile_RedisDogrulamaDuserseHepsiGeri(t *testing.T) {
	d := kur(t)
	d.redis.dogrulaRed = true
	if _, err := d.r.Yenile(context.Background(), Hepsi); err == nil {
		t.Fatal("hata bekleniyordu")
	}
	d.redis.dogrulaRed = false
	envDegismedi(t, d)
}

func TestYenile_EnvYazilamazsaDisSistemlerGeri(t *testing.T) {
	d := kur(t)
	d.r.envYazTest = func([]byte) error { return errors.New("disk dolu") }
	if _, err := d.r.Yenile(context.Background(), Hepsi); err == nil {
		t.Fatal("hata bekleniyordu")
	}
	envDegismedi(t, d)
}

func TestGeriDon_TumuEskiye(t *testing.T) {
	d := kur(t)
	s, err := d.r.Yenile(context.Background(), Hepsi)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.r.GeriDon(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	envDegismedi(t, d)
}

func TestYenile_SymlinkEnvReddedilir(t *testing.T) {
	d := kur(t)
	hedef := filepath.Join(d.dir, "gercek")
	if err := os.Rename(d.r.EnvYolu, hedef); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(hedef, d.r.EnvYolu); err != nil {
		t.Fatal(err)
	}
	if _, err := d.r.Yenile(context.Background(), Hepsi); err == nil {
		t.Fatal("symlink env reddedilmeliydi")
	}
	if oku(t, hedef) != eskiEnv() || d.db.parola["localhost"] != eskiPw || d.redis.parola != eskiRedis {
		t.Error("symlink reddinde bir şey değişti")
	}
}

func TestYenile_RootDSNVeBosRedisAtlanir(t *testing.T) {
	d := kur(t)
	icerik := strings.Replace(eskiEnv(), "PANEL_DB_DSN="+eskiDSN(), "PANEL_DB_DSN=root@unix(/var/lib/mysql/mysql.sock)/panel", 1)
	icerik = strings.Replace(icerik, "PANEL_REDIS_ADMIN_PASS="+eskiRedis, "PANEL_REDIS_ADMIN_PASS=", 1)
	if err := os.WriteFile(d.r.EnvYolu, []byte(icerik), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := d.r.Yenile(context.Background(), Hepsi)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(s.Yenilenen, ",") != "jwt" || s.Atlanan[DB] == "" || s.Atlanan[Redis] == "" {
		t.Fatalf("yenilenen=%v atlanan=%v", s.Yenilenen, s.Atlanan)
	}
	if len(d.db.cagri) != 0 {
		t.Error("root DSN'de ALTER USER çağrıldı")
	}
}

// Süreç DB ALTER'dan sonra ölürse (env eski, MariaDB yeni) Kurtar geçerli parolayı
// ÖLÇÜP env'i ona hizalar → panel kilitlenmez.
func TestKurtar_YarimKalanYenileme(t *testing.T) {
	d := kur(t)
	yeniPw := "kurtarilacakparola01"
	yeniDSN, err := DSNParolaDegistir(eskiDSN(), yeniPw)
	if err != nil {
		t.Fatal(err)
	}
	bek := map[string]string{"ESKI_PANEL_DB_DSN": eskiDSN(), "YENI_PANEL_DB_DSN": yeniDSN,
		"ESKI_PANEL_REDIS_ADMIN_PASS": eskiRedis, "YENI_PANEL_REDIS_ADMIN_PASS": "yeniredis"}
	if err := os.WriteFile(d.r.BekleyenYolu, envBicimle(bek), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, h := range d.db.hostlar {
		d.db.parola[h] = yeniPw // ALTER uygulanmış, env yazılamadan ölmüş
	}
	// Valkey değişmemiş (eski geçerli)
	if err := d.r.Kurtar(context.Background()); err != nil {
		t.Fatal(err)
	}
	m := config.EnvAyristir([]byte(oku(t, d.r.EnvYolu)))
	if m["PANEL_DB_DSN"] != yeniDSN {
		t.Error("env geçerli (yeni) DSN'e hizalanmadı")
	}
	if m["PANEL_REDIS_ADMIN_PASS"] != eskiRedis {
		t.Error("geçerli Valkey parolası (eski) değiştirildi")
	}
	if !strings.Contains(oku(t, d.ekYol), yeniDSN) {
		t.Error("eklenti DSN kopyası hizalanmadı")
	}
	if d.r.BekleyenVar() {
		t.Error("kurtarma sonrası bekleyen kaldı")
	}
}

func TestKurtar_HicbirAdayTutmazsaBekleyenKorunur(t *testing.T) {
	d := kur(t)
	yeniDSN, _ := DSNParolaDegistir(eskiDSN(), "baskaparola")
	if err := os.WriteFile(d.r.BekleyenYolu, envBicimle(map[string]string{"ESKI_PANEL_DB_DSN": eskiDSN(), "YENI_PANEL_DB_DSN": yeniDSN}), 0o600); err != nil {
		t.Fatal(err)
	}
	d.db.dogrulaRed = true
	if err := d.r.Kurtar(context.Background()); err == nil {
		t.Fatal("hata bekleniyordu")
	}
	if !d.r.BekleyenVar() || oku(t, d.r.EnvYolu) != eskiEnv() {
		t.Error("ölçülemeyen kurtarmada bekleyen silindi ya da env değişti")
	}
	// Yenile de yarım kalmış kurtarılamadan başlamamalı
	if _, err := d.r.Yenile(context.Background(), Hepsi); err == nil {
		t.Error("kurtarılamayan bekleyen varken Yenile başladı")
	}
}

func TestDSNParolaDegistir(t *testing.T) {
	for _, c := range []struct{ dsn, yeni, bek string }{
		{"panel:a1@tcp(127.0.0.1:3306)/panel?parseTime=true", "b2", "panel:b2@tcp(127.0.0.1:3306)/panel?parseTime=true"},
		{"u:p@ss@unix(/s.sock)/db", "zz", "u:zz@unix(/s.sock)/db"},
	} {
		got, err := DSNParolaDegistir(c.dsn, c.yeni)
		if err != nil || got != c.bek {
			t.Errorf("%q → %q, %v (beklenen %q)", c.dsn, got, err, c.bek)
		}
	}
}

func TestEnvDegistir_YorumVeTekrar(t *testing.T) {
	in := "#c\nA=1\nexport B=2\nA=3\nC=x=y\n"
	out := string(EnvDegistir([]byte(in), map[string]string{"A": "9", "B": "8", "Z": "0"}))
	if out != "#c\nA=9\nB=8\nA=9\nC=x=y\n" {
		t.Fatalf("%q", out)
	}
}

func TestSqlGuvenli(t *testing.T) {
	for _, s := range []string{"", "a'b", `a\b`, "a b", "a`b", "ç"} {
		if sqlGuvenli(s) {
			t.Errorf("%q güvenli sayıldı", s)
		}
	}
	if !sqlGuvenli("127.0.0.1") || !sqlGuvenli("abc_DEF-0%") {
		t.Error("geçerli değer reddedildi")
	}
}
