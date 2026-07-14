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
}

func Load() (*Config, error) {
	c := &Config{
		ListenAddr:  envOr("PANEL_LISTEN", ":8080"),
		DBDsn:       envOr("PANEL_DB_DSN", "panel:panelpw@unix(/var/lib/mysql/mysql.sock)/panel?parseTime=true&charset=utf8mb4&collation=utf8mb4_unicode_ci"),
		Env:         envOr("PANEL_ENV", "production"),
		JWTLifetime: envInt("PANEL_JWT_LIFETIME_SEC", 8*3600),
	}
	secret := strings.TrimSpace(os.Getenv("PANEL_JWT_SECRET"))
	if len(secret) < 32 {
		return nil, fmt.Errorf("PANEL_JWT_SECRET en az 32 karakter olmalı (mevcut: %d)", len(secret))
	}
	c.JWTSecret = []byte(secret)
	return c, nil
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
