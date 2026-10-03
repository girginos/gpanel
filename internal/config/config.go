package config

import (
	"fmt"
	"os"
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
}

// Load panel yapılandırmasını süreç ortamından okur ve ardından panel
// sırlarını ortamdan SİLER (SirlariOrtamdanSil) — hata dönse bile.
//
// 🔴 GÜVENLİK (kök neden): systemd EnvironmentFile panel sırlarını süreç
// ortamına koyar; Go'da env'i ayarlanmamış her exec.Cmd ortamı AYNEN miras
// alır. Kiracı kimliğinde başlatılan bir çocuk (runuser ... wp-cli kiracının
// wp-config.php'sini yükler) getenv('PANEL_JWT_SECRET') ile sırrı okuyup
// yönetici JWT'si üretebiliyordu; kısa ömürlü cp/rm'de bile /proc/<pid>/environ
// kiracı uid'sine açıktır. Sırlar okunduktan sonra ortamdan silinince HİÇBİR
// çocuk süreç (kök ya da kiracı) onları miras alamaz. Sırra ihtiyaç duyan kod
// bu Config'teki değeri kullanır; os.Getenv ile sır OKUNMAZ (kaynak koruma
// testi: TestKaynakKoruma_SirGetenvYalnizConfigte).
//
// Not: /proc/<panelpid>/environ ilk exec ortamını gösterir (Unsetenv onu
// değiştirmez); dosya 0400 ve süreç sahibine (root) aittir.
func Load() (*Config, error) {
	defer SirlariOrtamdanSil()
	c := &Config{
		ListenAddr:     envOr("PANEL_LISTEN", ":8080"),
		DBDsn:          envOr("PANEL_DB_DSN", "panel:panelpw@unix(/var/lib/mysql/mysql.sock)/panel?parseTime=true&charset=utf8mb4&collation=utf8mb4_unicode_ci"),
		Env:            envOr("PANEL_ENV", "production"),
		JWTLifetime:    envInt("PANEL_JWT_LIFETIME_SEC", 8*3600),
		RedisAdminPass: os.Getenv("PANEL_REDIS_ADMIN_PASS"),
	}
	secret := strings.TrimSpace(os.Getenv("PANEL_JWT_SECRET"))
	if len(secret) < 32 {
		return nil, fmt.Errorf("PANEL_JWT_SECRET en az 32 karakter olmalı (mevcut: %d)", len(secret))
	}
	c.JWTSecret = []byte(secret)
	return c, nil
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
