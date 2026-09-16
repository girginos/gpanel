package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"log"
	"strings"

	"girginospanel/internal/gizli"
	"girginospanel/internal/hesaplar"
)

// healSirleriSifrele: at-rest DÜZ METİN kalan kritik sırları gizli.SaklaBagli ile
// şifreler. gizli.CozBagli okumada graceful (düz-metni olduğu gibi döner) olduğu
// için bu heal OLMADAN da okuma çalışır; heal MEVCUT satırları da şifreleyerek
// "hiçbir kritik veri düz metin durmasın" hedefini eski kayıtlara da uygular.
// İdempotent: gizli.SifreliMi olan satırlar atlanır. Boot'u bloklamaz (bg goroutine).
func healSirleriSifrele(db *sql.DB) {
	// (tablo, id-sütunu, sır-sütunu, bağlam) — hepsi kod sabiti (SQLi yok).
	tekSutun := func(tablo, idSut, sirSut, baglam string) {
		rows, err := db.Query("SELECT " + idSut + ", " + sirSut + " FROM " + tablo + " WHERE " + sirSut + " <> ''")
		if err != nil {
			return
		}
		type kv struct {
			id  any
			deg string
		}
		var liste []kv
		for rows.Next() {
			var k kv
			if rows.Scan(&k.id, &k.deg) == nil {
				liste = append(liste, k)
			}
		}
		rows.Close()
		n := 0
		for _, k := range liste {
			if gizli.SifreliMi(k.deg) {
				continue // zaten şifreli
			}
			if _, e := db.Exec("UPDATE "+tablo+" SET "+sirSut+"=? WHERE "+idSut+"=?",
				gizli.SaklaBagli(k.deg, baglam), k.id); e == nil {
				n++
			}
		}
		if n > 0 {
			log.Printf("sifre-heal: %s.%s — %d satir sifrelendi", tablo, sirSut, n)
		}
	}

	tekSutun("backup_destinations", "domain_id", "parola", "yedek")
	tekSutun("backup_genel_ayar", "id", "uzak_parola", "yedek")
	tekSutun("users", "id", "totp_secret", "totp")
	tekSutun("dkim_keys", "id", "private_key", "dkim")
	tekSutun("github_connections", "id", "pat", "github-pat")
	tekSutun("cp_eklenti_lisans", "id", "lisans_anahtari", "lisans")

	// webhook_secret: çift-sütun (AEAD gösterim + SHA-256 arama). Eski düz-metin
	// satırlar için hash'i doldur + değeri şifrele.
	rows, err := db.Query("SELECT id, webhook_secret FROM git_repos WHERE webhook_secret <> '' AND webhook_secret_hash = ''")
	if err != nil {
		return
	}
	type wk struct {
		id  int64
		sec string
	}
	var whl []wk
	for rows.Next() {
		var k wk
		if rows.Scan(&k.id, &k.sec) == nil {
			whl = append(whl, k)
		}
	}
	rows.Close()
	n := 0
	for _, k := range whl {
		if gizli.SifreliMi(k.sec) {
			continue
		}
		h := sha256.Sum256([]byte(k.sec))
		if _, e := db.Exec("UPDATE git_repos SET webhook_secret=?, webhook_secret_hash=? WHERE id=?",
			gizli.SaklaBagli(k.sec, "webhook"), hex.EncodeToString(h[:]), k.id); e == nil {
			n++
		}
	}
	if n > 0 {
		log.Printf("sifre-heal: git_repos.webhook_secret — %d satir sifrelendi+hashlendi", n)
	}

	healFTPParolalari(db)
	healGitRepoURL(db)
}

// healGitRepoURL: eski git_repos.repo_url satırlarındaki DÜZ METİN gömülü GitHub PAT'ini
// (https://PAT@github.com/...) temizler → https://github.com/... . PAT yalnız
// github_connections.pat'te AEAD şifreli kalır; clone anında git.patliURL enjekte eder.
// İdempotent (yalnız @github.com içeren satırlar). Boot'u bloklamaz (bg goroutine).
func healGitRepoURL(db *sql.DB) {
	rows, err := db.Query("SELECT id, repo_url FROM git_repos WHERE repo_url LIKE 'https://%@github.com/%'")
	if err != nil {
		return
	}
	type kv struct {
		id int64
		u  string
	}
	var liste []kv
	for rows.Next() {
		var k kv
		if rows.Scan(&k.id, &k.u) == nil {
			liste = append(liste, k)
		}
	}
	rows.Close()
	n := 0
	for _, k := range liste {
		at := strings.Index(k.u, "@github.com/")
		if at < 0 || !strings.HasPrefix(k.u, "https://") {
			continue
		}
		yeni := "https://github.com/" + k.u[at+len("@github.com/"):]
		if _, e := db.Exec("UPDATE git_repos SET repo_url=? WHERE id=?", yeni, k.id); e == nil {
			n++
		}
	}
	if n > 0 {
		log.Printf("sifre-heal: git_repos.repo_url — %d satırdan gömülü PAT temizlendi", n)
	}
}

// healFTPParolalari: eski DUZ METIN ftp_accounts.password_md5 satirlarini $6$ crypt
// hash'ine cevirir (backup; ftp-setup deploy'da MYSQLCrypt crypt yapar). Idempotent.
func healFTPParolalari(db *sql.DB) {
	rows, err := db.Query("SELECT id, password_md5 FROM ftp_accounts WHERE password_md5 <> ''")
	if err != nil {
		return
	}
	type kv struct {
		id int64
		v  string
	}
	var liste []kv
	for rows.Next() {
		var k kv
		if rows.Scan(&k.id, &k.v) == nil {
			liste = append(liste, k)
		}
	}
	rows.Close()
	n := 0
	for _, k := range liste {
		if hesaplar.IsFTPHash(k.v) {
			continue
		}
		h := hesaplar.FTPParolaHash(k.v)
		if h == "" {
			continue
		}
		if _, e := db.Exec("UPDATE ftp_accounts SET password_md5=? WHERE id=?", h, k.id); e == nil {
			n++
		}
	}
	if n > 0 {
		log.Printf("sifre-heal: ftp_accounts.password_md5 — %d satir $6$ hash'lendi", n)
	}
}
