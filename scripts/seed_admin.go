//go:build ignore

// Tek seferlik admin tohumlama:
//   go run scripts/seed_admin.go -dsn '...' -kullanici admin -parola '...'
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"

	_ "github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	dsn := flag.String("dsn", "", "MySQL DSN")
	user := flag.String("kullanici", "admin", "admin kullanıcı adı")
	pass := flag.String("parola", "", "admin parolası (boşsa env PANEL_SEED_PAROLA)")
	email := flag.String("eposta", "admin@local", "e-posta")
	flag.Parse()

	if *pass == "" {
		*pass = os.Getenv("PANEL_SEED_PAROLA")
	}
	if *dsn == "" || *pass == "" {
		log.Fatalf("dsn ve parola zorunlu")
	}

	db, err := sql.Open("mysql", *dsn)
	if err != nil {
		log.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		log.Fatalf("ping: %v", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(*pass), 12)
	if err != nil {
		log.Fatalf("bcrypt: %v", err)
	}

	res, err := db.Exec(
		`INSERT INTO users(username, email, password_hash, role, full_name, status)
		 VALUES(?,?,?, 'admin', 'Sistem Yöneticisi', 'active')
		 ON DUPLICATE KEY UPDATE password_hash=VALUES(password_hash), role='admin', status='active'`,
		*user, *email, string(hash))
	if err != nil {
		log.Fatalf("insert: %v", err)
	}
	aff, _ := res.RowsAffected()
	fmt.Printf("admin tohumlandı (kullanici=%s, rowsAffected=%d)\n", *user, aff)
}
