// Package sirrota — panel sırlarının (JWT imza sırrı, panel DB parolası, Valkey
// yönetici parolası) hataya dayanıklı yenilenmesi (rotasyon).
//
// Sırlar /etc/girginospanel/env'de durur (root:root 0600; birim onu
// LoadCredential ile verir). Yenileme sırası — her adım geri alınabilir:
//
//  1. Bekleyen dosyası (ESKI_/YENI_ değerler, 0600) yazılır: süreç adım 2-4
//     arasında ölürse `sir kurtar` hangi parolanın geçerli olduğunu ÖLÇEREK
//     env'i ona hizalar (kilitlenme yok).
//  2. DB: panel kullanıcısının TÜM host tanımlarına ALTER USER → yeni DSN ile
//     bağlanılarak doğrulanır; olmazsa eski parolaya geri alınır.
//  3. Valkey: default kullanıcıya ACL SETUSER resetpass >yeni + ACL SAVE →
//     yeni parolayla AUTH doğrulanır; olmazsa Valkey + DB geri alınır.
//  4. env atomik yazılır (geçici dosya + rename); olmazsa Valkey + DB geri.
//  5. Panel DSN'inin kopyasını taşıyan eklenti env dosyaları (eklenti-*.env:
//     CALISTIRICI_DSN, TEHDIT_DSN ...) yeni DSN'e hizalanır.
//  6. Bekleyen dosyası silinir.
//
// Panel yeniden başlatma + sağlık + başarısızlıkta GeriDon CLI'dadır
// (cmd/server/sir.go). JWT değişince tüm oturumlar düşer (beklenen).
// Sır DEĞERLERİ hiçbir zaman loglanmaz / argv'ye konmaz.
package sirrota

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"girginospanel/internal/config"
	"girginospanel/internal/sirdosya"

	"github.com/go-sql-driver/mysql"
)

// Hedef adları (CLI ile aynı).
const (
	JWT   = "jwt"
	DB    = "db"
	Redis = "redis"
)

// Hepsi — --hepsi'nin kapsamı.
var Hepsi = []string{JWT, DB, Redis}

const (
	anahtarJWT   = "PANEL_JWT_SECRET"
	anahtarDSN   = "PANEL_DB_DSN"
	anahtarRedis = "PANEL_REDIS_ADMIN_PASS"
	envEnCok     = 1 << 20
)

// DBYonetici — panel DB kullanıcısının parolasını yönetir (üretim: MySQLCLI).
type DBYonetici interface {
	Hostlar(ctx context.Context, kul string) ([]string, error)
	ParolaAyarla(ctx context.Context, kul string, hostlar []string, parola string) error
	Dogrula(ctx context.Context, dsn string) error
}

// RedisYonetici — Valkey default (yönetici) kullanıcısının parolası (üretim: ValkeyTCP).
type RedisYonetici interface {
	ParolaDegistir(ctx context.Context, eski, yeni string) error
	Dogrula(ctx context.Context, parola string) error
}

// Rotator — yenileme ayarları. Alanlar boşsa Varsayilan değerleri kullanılır.
type Rotator struct {
	EnvYolu      string // /etc/girginospanel/env
	BekleyenYolu string // /etc/girginospanel/.sir-yenile-bekleyen
	EkDizin      string // eklenti-*.env dosyalarının dizini (/etc/girginospanel)
	DB           DBYonetici
	Redis        RedisYonetici
	Uret         func(nBayt int) (string, error)
	Log          func(format string, a ...any)

	envYazTest func([]byte) error // test: env yazma hatası enjeksiyonu
}

// Varsayilan: üretim yolları + üretim yöneticileri.
func Varsayilan() *Rotator {
	return &Rotator{
		EnvYolu:      config.EnvYolu,
		BekleyenYolu: "/etc/girginospanel/.sir-yenile-bekleyen",
		EkDizin:      "/etc/girginospanel",
		DB:           MySQLCLI{},
		Redis:        ValkeyTCP{Adres: "127.0.0.1:6379"},
	}
}

// Sonuc — yenilemenin özeti + geri dönüş bilgisi (sır değerleri dışa açık değil).
type Sonuc struct {
	Yenilenen  []string          // jwt/db/redis
	Atlanan    map[string]string // hedef → neden
	Eklentiler []string          // DSN'i hizalanan eklenti adları (girginospanel-eklenti-<ad>)
	Uyarilar   []string

	geri geriBilgi
}

type geriBilgi struct {
	envEski     []byte
	dbKul       string
	dbHostlar   []string
	dbEskiPw    string
	redisEski   string
	redisYeni   string
	ekEski      map[string][]byte // yol → eski içerik
	dbDegisti   bool
	redisDegist bool
}

func (r *Rotator) logf(f string, a ...any) {
	if r.Log != nil {
		r.Log(f, a...)
	}
}

func (r *Rotator) uret(n int) (string, error) {
	if r.Uret != nil {
		return r.Uret(n)
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// envOku: env'i symlink takip etmeden okur; sahibi bu süreç (root) olmalı.
func (r *Rotator) envOku() ([]byte, error) {
	b, st, err := sirdosya.Oku(r.EnvYolu, envEnCok)
	if err != nil {
		return nil, err
	}
	if uid, _, ok := sirdosya.Sahip(st); ok && int(uid) != os.Geteuid() {
		return nil, fmt.Errorf("%s sahibi uid %d (beklenen %d) — başkasının kontrol ettiği dosyaya sır yazılmaz", r.EnvYolu, uid, os.Geteuid())
	}
	return b, nil
}

// envYaz: 0600, root (bu süreç) sahipli, atomik.
func (r *Rotator) envYaz(b []byte) error {
	if r.envYazTest != nil {
		if err := r.envYazTest(b); err != nil {
			return err
		}
	}
	return sirdosya.Yaz(r.EnvYolu, b, 0o600, os.Geteuid(), os.Getegid())
}

// EnvDegistir: KEY=VALUE satırlarında degisim'deki anahtarların değerini
// değiştirir (anahtar birden çok kez geçiyorsa hepsini); diğer satırlar
// (yorumlar, sıra) AYNEN korunur. Olmayan anahtar EKLENMEZ.
func EnvDegistir(icerik []byte, degisim map[string]string) []byte {
	satirlar := strings.SplitAfter(string(icerik), "\n")
	var out strings.Builder
	for _, s := range satirlar {
		if s == "" {
			continue
		}
		govde := strings.TrimRight(s, "\r\n")
		son := s[len(govde):]
		t := strings.TrimSpace(govde)
		t = strings.TrimPrefix(t, "export ")
		if k, _, ok := strings.Cut(t, "="); ok {
			if v, varMi := degisim[strings.TrimSpace(k)]; varMi {
				out.WriteString(strings.TrimSpace(k) + "=" + v + son)
				continue
			}
		}
		out.WriteString(s)
	}
	return []byte(out.String())
}

// DSNParolaDegistir: DSN'deki parolayı yeni ile değiştirir; biçimin geri kalanı
// (adres, parametreler) bayt bayt korunur. Sonuç ayrıştırılarak doğrulanır.
func DSNParolaDegistir(dsn, yeni string) (string, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return "", fmt.Errorf("DSN ayrıştırılamadı: %w", err)
	}
	on := cfg.User + ":" + cfg.Passwd + "@"
	if !strings.HasPrefix(dsn, on) {
		return "", errors.New("DSN biçimi beklenmedik (kullanici:parola@... değil)")
	}
	out := cfg.User + ":" + yeni + "@" + dsn[len(on):]
	c2, err := mysql.ParseDSN(out)
	if err != nil || c2.User != cfg.User || c2.Passwd != yeni || c2.Addr != cfg.Addr || c2.DBName != cfg.DBName {
		return "", errors.New("yeni DSN doğrulanamadı")
	}
	return out, nil
}

// sqlGuvenli: tek tırnaklı SQL dizgesine gömülecek değer (parola/host/kullanıcı)
// için izin verilen karakterler. Ters bölü ve tırnak, sql_mode'a göre farklı
// yorumlanabildiği için REDDEDİLİR (kaçışa güvenilmez).
func sqlGuvenli(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < 0x21 || c > 0x7e || c == '\'' || c == '\\' || c == '"' || c == '`' {
			return false
		}
	}
	return true
}

func hedefKume(hedefler []string) (map[string]bool, error) {
	m := map[string]bool{}
	for _, h := range hedefler {
		switch h {
		case JWT, DB, Redis:
			m[h] = true
		default:
			return nil, fmt.Errorf("bilinmeyen sır: %q (jwt|db|redis)", h)
		}
	}
	if len(m) == 0 {
		return nil, errors.New("yenilenecek sır seçilmedi")
	}
	return m, nil
}

// Yenile: seçilen sırları yeniler (bkz. paket belgesi). Hata dönerse dış
// sistemler (DB/Valkey) ve env ESKİ durumundadır.
func (r *Rotator) Yenile(ctx context.Context, hedefler []string) (*Sonuc, error) {
	kume, err := hedefKume(hedefler)
	if err != nil {
		return nil, err
	}
	if err := r.Kurtar(ctx); err != nil {
		return nil, fmt.Errorf("yarım kalmış önceki yenileme kurtarılamadı: %w", err)
	}
	envEski, err := r.envOku()
	if err != nil {
		return nil, err
	}
	eski := config.EnvAyristir(envEski)
	s := &Sonuc{Atlanan: map[string]string{}}
	s.geri.envEski = envEski
	degisim := map[string]string{}
	bekleyen := map[string]string{}

	if kume[JWT] {
		if strings.TrimSpace(eski[anahtarJWT]) == "" {
			s.Atlanan[JWT] = "env'de PANEL_JWT_SECRET yok"
		} else {
			v, err := r.uret(32)
			if err != nil {
				return nil, err
			}
			degisim[anahtarJWT] = v
		}
	}
	var dbKul, dbEskiPw, dbYeniPw, dsnYeni string
	if kume[DB] {
		dsn := eski[anahtarDSN]
		cfg, perr := mysql.ParseDSN(dsn)
		switch {
		case dsn == "":
			s.Atlanan[DB] = "env'de PANEL_DB_DSN yok"
		case perr != nil:
			s.Atlanan[DB] = "PANEL_DB_DSN ayrıştırılamadı"
		case cfg.User == "" || cfg.User == "root":
			s.Atlanan[DB] = "DSN kullanıcısı boş/root — panel kullanıcısı değil, parolası panelce yönetilmez"
		case cfg.Passwd == "":
			s.Atlanan[DB] = "DSN'de parola yok (soket kimliği) — yenilenecek parola yok"
		case !sqlGuvenli(cfg.User) || !sqlGuvenli(cfg.Passwd):
			s.Atlanan[DB] = "DSN kullanıcı/parolası desteklenmeyen karakter içeriyor — elle yenileyin"
		default:
			dbKul, dbEskiPw = cfg.User, cfg.Passwd
			if dbYeniPw, err = r.uret(16); err != nil {
				return nil, err
			}
			if dsnYeni, err = DSNParolaDegistir(dsn, dbYeniPw); err != nil {
				return nil, err
			}
			degisim[anahtarDSN] = dsnYeni
		}
	}
	var redisEski, redisYeni string
	if kume[Redis] {
		redisEski = eski[anahtarRedis]
		switch {
		case redisEski == "":
			s.Atlanan[Redis] = "PANEL_REDIS_ADMIN_PASS boş — Redis yapılandırılmamış"
		case r.Redis == nil:
			s.Atlanan[Redis] = "Redis yöneticisi yok"
		default:
			if redisYeni, err = r.uret(24); err != nil {
				return nil, err
			}
			degisim[anahtarRedis] = redisYeni
		}
	}
	if len(degisim) == 0 {
		return s, nil
	}

	// 1) bekleyen dosyası: ölçerek kurtarma için eski + yeni değerler.
	for k, v := range degisim {
		bekleyen["ESKI_"+k] = eski[k]
		bekleyen["YENI_"+k] = v
	}
	if err := sirdosya.Yaz(r.BekleyenYolu, envBicimle(bekleyen), 0o600, os.Geteuid(), os.Getegid()); err != nil {
		return nil, fmt.Errorf("bekleyen dosyası yazılamadı (hiçbir şey değiştirilmedi): %w", err)
	}
	basarisiz := func(e error) (*Sonuc, error) {
		r.geriAlDis(ctx, s)
		_ = os.Remove(r.BekleyenYolu)
		return nil, e
	}

	// 2) DB
	if dsnYeni != "" {
		hostlar, err := r.DB.Hostlar(ctx, dbKul)
		if err != nil {
			return basarisiz(fmt.Errorf("DB: %q kullanıcısının host tanımları okunamadı: %w", dbKul, err))
		}
		if len(hostlar) == 0 {
			return basarisiz(fmt.Errorf("DB: %q kullanıcısı mysql.user'da yok", dbKul))
		}
		for _, h := range hostlar {
			if !sqlGuvenli(h) {
				return basarisiz(fmt.Errorf("DB: host tanımı desteklenmeyen karakter içeriyor"))
			}
		}
		s.geri.dbKul, s.geri.dbHostlar, s.geri.dbEskiPw = dbKul, hostlar, dbEskiPw
		s.geri.dbDegisti = true // kısmi uygulanmış olabilir → geri almada hepsine eski parola
		if err := r.DB.ParolaAyarla(ctx, dbKul, hostlar, dbYeniPw); err != nil {
			return basarisiz(fmt.Errorf("DB: ALTER USER başarısız (eski parolaya geri alındı): %w", err))
		}
		if err := r.DB.Dogrula(ctx, dsnYeni); err != nil {
			return basarisiz(fmt.Errorf("DB: yeni parolayla bağlanılamadı (eski parolaya geri alındı): %w", err))
		}
		r.logf("DB: %q parolası yenilendi (%d host tanımı) ve yeni DSN ile bağlantı doğrulandı", dbKul, len(hostlar))
	}
	// 3) Valkey
	if redisYeni != "" {
		s.geri.redisEski, s.geri.redisYeni = redisEski, redisYeni
		if err := r.Redis.ParolaDegistir(ctx, redisEski, redisYeni); err != nil {
			return basarisiz(fmt.Errorf("Valkey: yönetici parolası değiştirilemedi (geri alındı): %w", err))
		}
		s.geri.redisDegist = true
		if err := r.Redis.Dogrula(ctx, redisYeni); err != nil {
			return basarisiz(fmt.Errorf("Valkey: yeni parolayla AUTH başarısız (geri alındı): %w", err))
		}
		r.logf("Valkey: yönetici parolası yenilendi ve doğrulandı (kiracı ACL kullanıcıları etkilenmedi)")
	}
	// 4) env
	if err := r.envYaz(EnvDegistir(envEski, degisim)); err != nil {
		return basarisiz(fmt.Errorf("env yazılamadı (DB/Valkey eski parolaya geri alındı): %w", err))
	}
	// 5) eklenti DSN kopyaları
	if dsnYeni != "" {
		s.geri.ekEski = map[string][]byte{}
		r.eklentiDSNHizala(eski[anahtarDSN], dsnYeni, s)
	}
	// 6)
	if err := os.Remove(r.BekleyenYolu); err != nil && !errors.Is(err, os.ErrNotExist) {
		s.Uyarilar = append(s.Uyarilar, "bekleyen dosyası silinemedi: "+err.Error())
	}
	for _, h := range []string{JWT, DB, Redis} {
		if _, ok := degisim[map[string]string{JWT: anahtarJWT, DB: anahtarDSN, Redis: anahtarRedis}[h]]; ok {
			s.Yenilenen = append(s.Yenilenen, h)
		}
	}
	return s, nil
}

// geriAlDis: DB ve Valkey'i eski parolaya döndürür (best-effort, loglar).
func (r *Rotator) geriAlDis(ctx context.Context, s *Sonuc) {
	if s.geri.redisDegist {
		if err := r.Redis.ParolaDegistir(ctx, s.geri.redisYeni, s.geri.redisEski); err != nil {
			r.logf("UYARI Valkey geri alınamadı: %v — elle: valkey-cli ACL SETUSER default resetpass >(env'deki değer); ACL SAVE", err)
		} else {
			s.geri.redisDegist = false
		}
	}
	if s.geri.dbDegisti {
		if err := r.DB.ParolaAyarla(ctx, s.geri.dbKul, s.geri.dbHostlar, s.geri.dbEskiPw); err != nil {
			r.logf("UYARI DB parolası geri alınamadı: %v — env'deki parola ile ALTER USER elle yapılmalı", err)
		} else {
			s.geri.dbDegisti = false
		}
	}
}

// GeriDon: başarılı bir Yenile'yi tümüyle geri alır (panel yeni sırlarla
// sağlıklı açılmadıysa): DB + Valkey eski parolaya, env + eklenti env'leri eski
// içeriğe. Sonrasında panel yeniden başlatılmalıdır.
func (r *Rotator) GeriDon(ctx context.Context, s *Sonuc) error {
	if s == nil {
		return nil
	}
	var hatalar []string
	r.geriAlDis(ctx, s)
	if s.geri.redisDegist {
		hatalar = append(hatalar, "Valkey")
	}
	if s.geri.dbDegisti {
		hatalar = append(hatalar, "DB")
	}
	if len(s.geri.envEski) > 0 {
		if err := r.envYaz(s.geri.envEski); err != nil {
			hatalar = append(hatalar, "env: "+err.Error())
		}
	}
	for yol, b := range s.geri.ekEski {
		if err := sirdosya.Yaz(yol, b, 0o600, os.Geteuid(), os.Getegid()); err != nil {
			hatalar = append(hatalar, filepath.Base(yol)+": "+err.Error())
		}
	}
	if len(hatalar) > 0 {
		return fmt.Errorf("geri dönüş eksik: %s", strings.Join(hatalar, "; "))
	}
	return nil
}

// eklentiDSNHizala: EkDizin/eklenti-*.env içinde değeri TAM OLARAK eski DSN olan
// satırları yeni DSN'e çevirir (yalnız düzenli dosya; symlink atlanır).
func (r *Rotator) eklentiDSNHizala(eskiDSN, yeniDSN string, s *Sonuc) {
	if r.EkDizin == "" || eskiDSN == "" {
		return
	}
	adaylar, _ := filepath.Glob(filepath.Join(r.EkDizin, "eklenti-*.env"))
	sort.Strings(adaylar)
	for _, yol := range adaylar {
		b, _, err := sirdosya.Oku(yol, envEnCok)
		if err != nil {
			if errors.Is(err, sirdosya.ErrSymlink) {
				s.Uyarilar = append(s.Uyarilar, filepath.Base(yol)+": symlink — atlandı")
			}
			continue
		}
		degisim := map[string]string{}
		for k, v := range config.EnvAyristir(b) {
			if v == eskiDSN {
				degisim[k] = yeniDSN
			}
		}
		if len(degisim) == 0 {
			continue
		}
		if err := sirdosya.Yaz(yol, EnvDegistir(b, degisim), 0o600, os.Geteuid(), os.Getegid()); err != nil {
			s.Uyarilar = append(s.Uyarilar, fmt.Sprintf("%s: panel DSN kopyası güncellenemedi (%v) — eklenti DB'ye bağlanamaz; elle düzeltin", filepath.Base(yol), err))
			continue
		}
		s.geri.ekEski[yol] = b
		ad := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(yol), "eklenti-"), ".env")
		s.Eklentiler = append(s.Eklentiler, ad)
		r.logf("eklenti %s: panel DSN kopyası yeni parolaya hizalandı", ad)
	}
}

// Kurtar: yarım kalmış bir yenilemenin (bekleyen dosyası duruyor) ardından
// DB/Valkey'de GEÇERLİ olan parolayı ÖLÇER ve env'i ona hizalar. Bekleyen yoksa
// hiçbir şey yapmaz. Hiçbir aday doğrulanamazsa hata döner ve bekleyen KORUNUR.
func (r *Rotator) Kurtar(ctx context.Context) error {
	bb, _, err := sirdosya.Oku(r.BekleyenYolu, envEnCok)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	bek := config.EnvAyristir(bb)
	envB, err := r.envOku()
	if err != nil {
		return err
	}
	env := config.EnvAyristir(envB)
	degisim := map[string]string{}

	if yeni, ok := bek["YENI_"+anahtarDSN]; ok && r.DB != nil {
		mevcut := env[anahtarDSN]
		if r.DB.Dogrula(ctx, mevcut) != nil {
			secildi := ""
			for _, aday := range []string{yeni, bek["ESKI_"+anahtarDSN]} {
				if aday != "" && aday != mevcut && r.DB.Dogrula(ctx, aday) == nil {
					secildi = aday
					break
				}
			}
			if secildi == "" {
				return errors.New("DB: env'deki, yeni ve eski parolaların hiçbiriyle bağlanılamadı (MariaDB çalışıyor mu?) — bekleyen dosyası korundu")
			}
			degisim[anahtarDSN] = secildi
		}
	}
	if yeni, ok := bek["YENI_"+anahtarRedis]; ok && r.Redis != nil {
		mevcut := env[anahtarRedis]
		if mevcut == "" || r.Redis.Dogrula(ctx, mevcut) != nil {
			secildi := ""
			for _, aday := range []string{yeni, bek["ESKI_"+anahtarRedis]} {
				if aday != "" && aday != mevcut && r.Redis.Dogrula(ctx, aday) == nil {
					secildi = aday
					break
				}
			}
			if secildi == "" {
				return errors.New("Valkey: env'deki, yeni ve eski parolaların hiçbiriyle AUTH olmadı (valkey çalışıyor mu?) — bekleyen dosyası korundu")
			}
			degisim[anahtarRedis] = secildi
		}
	}
	if len(degisim) > 0 {
		if err := r.envYaz(EnvDegistir(envB, degisim)); err != nil {
			return fmt.Errorf("env hizalanamadı: %w", err)
		}
		if yeniDSN, ok := degisim[anahtarDSN]; ok {
			s := &Sonuc{geri: geriBilgi{ekEski: map[string][]byte{}}}
			r.eklentiDSNHizala(env[anahtarDSN], yeniDSN, s)
		}
		adlar := make([]string, 0, len(degisim))
		for k := range degisim {
			adlar = append(adlar, k)
		}
		sort.Strings(adlar)
		r.logf("kurtarma: env geçerli parolaya hizalandı: %s", strings.Join(adlar, ", "))
	}
	if err := os.Remove(r.BekleyenYolu); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// BekleyenVar: yarım kalmış yenileme işareti duruyor mu?
func (r *Rotator) BekleyenVar() bool {
	_, err := os.Lstat(r.BekleyenYolu)
	return err == nil
}

func envBicimle(m map[string]string) []byte {
	adlar := make([]string, 0, len(m))
	for k := range m {
		adlar = append(adlar, k)
	}
	sort.Strings(adlar)
	var b bytes.Buffer
	b.WriteString("# girginospanel sir yenile — YARIM KALAN YENİLEME (kurtarma için). Elle silmeyin: girginospanel-server sir kurtar\n")
	for _, k := range adlar {
		b.WriteString(k + "=" + m[k] + "\n")
	}
	return b.Bytes()
}
