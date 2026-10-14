package sirrota

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

// MySQLCLI — panel DB kullanıcısının parolasını `mysql` istemcisiyle (root,
// unix soket kimliği — kurulum betiğiyle aynı yol) yönetir. SQL, parola dahil,
// argv'ye DEĞİL stdin'e yazılır: /proc/<pid>/cmdline herkese okunur.
type MySQLCLI struct {
	// Komut boşsa "mysql" (testte geçici sunucuya giden sarmalayıcı PATH'te).
	Komut string
}

func (m MySQLCLI) komut() string {
	if m.Komut != "" {
		return m.Komut
	}
	return "mysql"
}

func (m MySQLCLI) calistir(ctx context.Context, sqlMetni string, gizle ...string) (string, error) {
	cmd := exec.CommandContext(ctx, m.komut(), "-N", "-B")
	cmd.Stdin = strings.NewReader(sqlMetni)
	var out, hata bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &hata
	if err := cmd.Run(); err != nil {
		msj := strings.TrimSpace(hata.String())
		for _, g := range gizle {
			if g != "" {
				msj = strings.ReplaceAll(msj, g, "***")
			}
		}
		if len(msj) > 300 {
			msj = msj[:300]
		}
		return "", fmt.Errorf("mysql: %v: %s", err, msj)
	}
	return out.String(), nil
}

// Hostlar: mysql.user'da kullanıcının tüm host tanımları (ör. 127.0.0.1, localhost).
func (m MySQLCLI) Hostlar(ctx context.Context, kul string) ([]string, error) {
	if !sqlGuvenli(kul) {
		return nil, errors.New("invalid user name")
	}
	out, err := m.calistir(ctx, "SELECT Host FROM mysql.user WHERE User='"+kul+"' ORDER BY Host;\n")
	if err != nil {
		return nil, err
	}
	var h []string
	for _, s := range strings.Split(out, "\n") {
		if s = strings.TrimSpace(s); s != "" {
			h = append(h, s)
		}
	}
	return h, nil
}

// ParolaAyarla: kullanıcının verilen TÜM host tanımlarının parolasını tek
// istemci oturumunda değiştirir. Bir host düşerse hata döner (çağıran geri alır).
func (m MySQLCLI) ParolaAyarla(ctx context.Context, kul string, hostlar []string, parola string) error {
	if !sqlGuvenli(kul) || !sqlGuvenli(parola) {
		return errors.New("invalid user/password character")
	}
	var b strings.Builder
	for _, h := range hostlar {
		if !sqlGuvenli(h) {
			return errors.New("invalid host character")
		}
		b.WriteString("ALTER USER '" + kul + "'@'" + h + "' IDENTIFIED BY '" + parola + "';\n")
	}
	_, err := m.calistir(ctx, b.String(), parola)
	return err
}

// Dogrula: DSN ile gerçekten bağlanır (ALTER'ın hata vermemesi yetmez).
func (m MySQLCLI) Dogrula(ctx context.Context, dsn string) error {
	return DSNDogrula(ctx, dsn)
}

// DSNDogrula: dsn ile bağlanıp SELECT 1 koşar (10 sn sınır).
func DSNDogrula(ctx context.Context, dsn string) error {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return fmt.Errorf("could not parse the DSN")
	}
	cfg.Timeout = 5 * time.Second
	ctx, iptal := context.WithTimeout(ctx, 10*time.Second)
	defer iptal()
	conn, err := mysql.NewConnector(cfg)
	if err != nil {
		return err
	}
	db := sql.OpenDB(conn)
	defer db.Close()
	var bir int
	if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&bir); err != nil {
		// Sürücü hatası parolayı içermez; yine de yalnız kodu/mesajı döndür.
		return fmt.Errorf("connection: %v", err)
	}
	return nil
}
