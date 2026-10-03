package config

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Config struct {
	ListenAddr  string
	DBDsn       string
	JWTSecret   []byte
	JWTLifetime int // saniye
	Env         string
	// RedisAdminPass — Valkey yönetici parolası (PANEL_REDIS_ADMIN_PASS); boşsa
	// Redis özelliği yapılandırılmamış sayılır. redis.AdminParolaAyarla ile verilir.
	RedisAdminPass string
	// SirKaynagi — sırların nereden okunduğu: "kimlik" (systemd LoadCredential,
	// sırlar süreç ortamına HİÇ girmedi) ya da "ortam" (eski birim:
	// EnvironmentFile; geriye uyum). Sır DEĞİL; açılışta loglanır.
	SirKaynagi string
}

// KimlikAdi — systemd birimindeki LoadCredential kimliği:
//
//	LoadCredential=panel-env:/etc/girginospanel/env
//
// systemd dosyayı servis başlarken /run/credentials/girginospanel.service/panel-env
// altına (ramfs, 0400, yalnız servis kullanıcısı) kopyalar ve dizini
// $CREDENTIALS_DIRECTORY ile bildirir.
const KimlikAdi = "panel-env"

// EnvYolu — panel ayar + sır dosyası (root:root 0600). Birim onu EnvironmentFile
// ile DEĞİL LoadCredential ile verir; yan araçlar (dogrula, redis-setup,
// wp-redis, avajan, lisans kurulumu, taşıma keşfi) aynı dosyayı okumaya devam eder.
const EnvYolu = "/etc/girginospanel/env"

// Load panel yapılandırmasını okur ve ardından panel sırlarını süreç
// ortamından SİLER (SirlariOrtamdanSil) — hata dönse bile.
//
// 🔴 GÜVENLİK (kök neden): systemd EnvironmentFile panel sırlarını süreç
// ortamına koyar; Go'da env'i ayarlanmamış her exec.Cmd ortamı AYNEN miras
// alır. Kiracı kimliğinde başlatılan bir çocuk (runuser ... wp-cli kiracının
// wp-config.php'sini yükler) getenv('PANEL_JWT_SECRET') ile sırrı okuyup
// yönetici JWT'si üretebiliyordu; kısa ömürlü cp/rm'de bile /proc/<pid>/environ
// kiracı uid'sine açıktır.
//
// KALICI ÇÖZÜM: birim dosyayı LoadCredential ile verir → sırlar ortama hiç
// girmez (ilk exec ortamı, /proc/<pid>/environ dahil). Load kimlik dosyasını
// ayrıştırır: SIR anahtarları (SirDegiskeniMi) yalnız Config'e gider; sır
// OLMAYAN ayarlar (PANEL_LISTEN, PANEL_SURUM_KONTROL, PANEL_LISANS_SUNUCU ...)
// eski EnvironmentFile davranışı korunsun diye — ortamda zaten yoksa —
// os.Setenv ile ortama konur (os.Getenv ile okuyan kod değişmeden çalışır).
//
// Kimlik yoksa (eski birim, geliştirme) GERİYE UYUM: sırlar ortamdan okunur ve
// SirlariOrtamdanSil ile silinir (1. katman korunur). Sırra ihtiyaç duyan kod
// bu Config'teki değeri kullanır; os.Getenv ile sır OKUNMAZ (kaynak koruma
// testi: TestKaynakKoruma_SirGetenvYalnizConfigte).
func Load() (*Config, error) {
	defer SirlariOrtamdanSil()
	kim, err := kimlikOku()
	if err != nil {
		return nil, err
	}
	kaynak := "ortam"
	sir := func(ad string) string { return os.Getenv(ad) }
	if kim != nil {
		kaynak = "kimlik"
		for k, v := range kim {
			if SirDegiskeniMi(k) {
				continue
			}
			if _, varMi := os.LookupEnv(k); !varMi {
				_ = os.Setenv(k, v)
			}
		}
		// Kimlik varsa sırların TEK kaynağı odur: ortamda kalmış eski bir değer
		// (ör. birime elle eklenmiş Environment=) kimliği EZMEZ.
		sir = func(ad string) string { return kim[ad] }
	}
	c := &Config{
		ListenAddr:     envOr("PANEL_LISTEN", ":8080"),
		DBDsn:          strings.TrimSpace(sir("PANEL_DB_DSN")),
		Env:            envOr("PANEL_ENV", "production"),
		JWTLifetime:    envInt("PANEL_JWT_LIFETIME_SEC", 8*3600),
		RedisAdminPass: sir("PANEL_REDIS_ADMIN_PASS"),
		SirKaynagi:     kaynak,
	}
	if c.DBDsn == "" {
		c.DBDsn = "panel:panelpw@unix(/var/lib/mysql/mysql.sock)/panel?parseTime=true&charset=utf8mb4&collation=utf8mb4_unicode_ci"
	}
	secret := strings.TrimSpace(sir("PANEL_JWT_SECRET"))
	if len(secret) < 32 {
		return nil, fmt.Errorf("PANEL_JWT_SECRET en az 32 karakter olmalı (mevcut: %d, kaynak: %s)", len(secret), kaynak)
	}
	c.JWTSecret = []byte(secret)
	return c, nil
}

// kimlikOku: $CREDENTIALS_DIRECTORY/panel-env varsa ayrıştırır. Kimlik dizini
// yoksa (nil, nil) → eski yol. Dizin VAR ama dosya okunamıyorsa HATA: birim
// LoadCredential ile başlatıldı demektir; sessizce ortama düşmek, sırları
// ortamda arayıp yanlış (boş/eski) değerle açılmak olurdu.
func kimlikOku() (map[string]string, error) {
	dizin := strings.TrimSpace(os.Getenv("CREDENTIALS_DIRECTORY"))
	if dizin == "" {
		return nil, nil
	}
	yol := filepath.Join(dizin, KimlikAdi)
	b, err := os.ReadFile(yol)
	if errors.Is(err, fs.ErrNotExist) {
		// Başka bir kimlik için dizin var, bizimki yok → eski yol.
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("panel kimliği okunamadı (%s): %w", yol, err)
	}
	return EnvAyristir(b), nil
}

var anahtarDeseni = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// EnvAyristir: systemd EnvironmentFile biçimindeki (KEY=VALUE satırları) içeriği
// ayrıştırır. Boş satır ve '#'/';' yorumları atlanır; "export " öneki kabul
// edilir; değer baştan/sondan boşluktan arındırılır ve tümüyle tek ya da çift
// tırnak içindeyse tırnaklar soyulur. Geçersiz anahtarlı satır atlanır (systemd
// de yok sayar). Aynı anahtar tekrar ederse SONUNCU kazanır (systemd gibi).
func EnvAyristir(b []byte) map[string]string {
	m := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		s := strings.TrimSpace(sc.Text())
		if s == "" || s[0] == '#' || s[0] == ';' {
			continue
		}
		s = strings.TrimPrefix(s, "export ")
		k, v, ok := strings.Cut(s, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if !anahtarDeseni.MatchString(k) {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		m[k] = v
	}
	return m
}

// SirAdlari — panelin bilinen sır değişkenleri. Yenisi eklenirse buraya da
// yazılır; unutulsa bile SirDegiskeniMi deseni (PANEL_/GOSP_ + sır sözcüğü)
// onu yakalar.
var SirAdlari = []string{"PANEL_JWT_SECRET", "PANEL_DB_DSN", "PANEL_REDIS_ADMIN_PASS"}

// sirSozcukleri — ad bileşeni ("_" ile ayrılmış) bunlardan biriyse değişken
// sır sayılır: ...SECRET, ...PASS, ...DSN, ...TOKEN, ...KEY, ...PAROLA vb.
var sirSozcukleri = map[string]bool{
	"SECRET": true, "SECRETS": true, "PASS": true, "PASSWD": true, "PASSWORD": true,
	"PASSPHRASE": true, "DSN": true, "TOKEN": true, "KEY": true, "APIKEY": true,
	"AUTH": true, "CRED": true, "CREDS": true, "CREDENTIAL": true, "CREDENTIALS": true,
	"PRIVATE": true, "PAROLA": true, "PAROLASI": true, "SIFRE": true, "SIFRESI": true,
	"SIR": true, "SIRRI": true, "JETON": true, "ANAHTAR": true, "ANAHTARI": true,
}

// SirDegiskeniMi: ad bir panel sırrı mı? Bilinen listede ya da PANEL_/GOSP_
// önekli ve bir bileşeni sır sözcüğü.
func SirDegiskeniMi(ad string) bool {
	for _, s := range SirAdlari {
		if ad == s {
			return true
		}
	}
	if !strings.HasPrefix(ad, "PANEL_") && !strings.HasPrefix(ad, "GOSP_") {
		return false
	}
	for _, p := range strings.Split(ad, "_")[1:] {
		if sirSozcukleri[p] {
			return true
		}
	}
	return false
}

// SirlariOrtamdanSil: süreç ortamındaki tüm panel sırlarını siler (bkz. Load).
// Tekrar çağrılabilir.
func SirlariOrtamdanSil() {
	for _, kv := range os.Environ() {
		ad, _, _ := strings.Cut(kv, "=")
		if SirDegiskeniMi(ad) {
			_ = os.Unsetenv(ad)
		}
	}
}

func envOr(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
