package main

// sifreli_sir_kaynak_test.go — gizli.Sakla* ile bir sır yazan HER dosya burada kayıtlıdır; yeni bir yazım noktası eklenirse
// test düşer ve geliştirici kolonun GENİŞLİĞİNİ düşünmeye zorlanır.
//
// Neden: şifreli çıktı düz değerden uzundur ("gos1:" + base64(12 + n + 16)); kolon dar kalırsa STRICT modda yazım
// "Data too long" ile düşer. 0218'den önce users.totp_secret ve git_repos.webhook_secret (varchar(64)) bu yüzden bozuktu
// (2FA açılmıyor, git bağlanmıyor) ve hiçbir birim test yakalamadı. Kural:
//   - kolon varchar(255) ise EN UZUN düz sır ≤ 158 karakter olmalı (sifreliUzunluk(158) = 255) — girdi üst sınırını doğrulayın;
//   - daha dar/sınırı belirsiz ise sifreliSirGenislik (main.go) kaydına ekleyin (açılışta semaGenislikDogrula denetler).

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// dosya → şifreli yazılan kolon(lar) ve genişlik gerekçesi.
var sifreliSirYazanDosyalar = map[string]string{
	"cmd/server/sifreheal.go":                  "heal: aşağıdaki kolonların düz metin satırlarını şifreler (genişlik: aynı kolonlar)",
	"internal/auth/profile.go":                 "users.totp_secret — sifreliSirGenislik (0218: varchar(255))",
	"internal/backups/destination_handlers.go": "backup_destinations.parola varchar(255)",
	"internal/backups/dbkullanici.go":          "db_accounts.db_pass_plain varchar(255)",
	"internal/backups/genel.go":                "backup_genel_ayar.uzak_parola varchar(255)",
	"internal/backups/silinen.go":              "backup_yetim_hedefler.parola varchar(255) (yetimHedefParolaSutun denetimi var)",
	"internal/dns/template.go":                 "dkim_keys.private_key mediumtext",
	"internal/domains/handlers_dbpw.go":        "db_accounts.db_pass_plain varchar(255)",
	"internal/git/git.go":                      "git_repos.webhook_secret — sifreliSirGenislik (0218: varchar(191))",
	"internal/github/github.go":                "github_connections.pat varchar(255); git_repos.webhook_secret — sifreliSirGenislik",
	"internal/hesaplar/hesaplar.go":            "db_accounts.db_pass_plain varchar(255)",
	"internal/hesaplar/password.go":            "db_accounts.db_pass_plain varchar(255)",
	"internal/hesaplar/parola_api.go":          "db_accounts.db_pass_plain varchar(255)",
	"internal/pma/pma.go":                      "pma_tokens.db_parola varchar(255)",
	"internal/redis/redis.go":                  "cp_domain_redis.redis_pass varchar(255)",
	"internal/toplu/devir.go":                  "git_repos.webhook_secret — sifreliSirGenislik",
	"internal/winajan/winajan.go":              "windows_ajanlar.jeton_sifreli text",
	"internal/wordpress/wordpress.go":          "cp_wp_kurulum.admin_parola varchar(255)",
	"internal/destek/ajan/cihaz.go":            "DB değil: cihaz anahtarı dosyası",
}

func TestSifreliSirYazanDosyalar_KayitliVeGuncel(t *testing.T) {
	re := regexp.MustCompile(`gizli\.(Sakla|SaklaBagli|SaklaGecis)\(`)
	kok := filepath.Join("..", "..")
	bulunan := map[string]bool{}
	err := filepath.WalkDir(kok, func(yol string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", ".git", "vendor", "frontend":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(yol, ".go") || strings.HasSuffix(yol, "_test.go") {
			return nil
		}
		b, rerr := os.ReadFile(yol)
		if rerr != nil {
			return rerr
		}
		for _, satir := range strings.Split(string(b), "\n") {
			if s := strings.TrimSpace(satir); strings.HasPrefix(s, "//") {
				continue
			}
			if re.MatchString(satir) {
				rel, _ := filepath.Rel(kok, yol)
				bulunan[filepath.ToSlash(rel)] = true
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var yeni, kayip []string
	for f := range bulunan {
		if _, ok := sifreliSirYazanDosyalar[f]; !ok {
			yeni = append(yeni, f)
		}
	}
	for f := range sifreliSirYazanDosyalar {
		if !bulunan[f] {
			kayip = append(kayip, f)
		}
	}
	sort.Strings(yeni)
	sort.Strings(kayip)
	if len(yeni) > 0 {
		t.Errorf("gizli.Sakla* ile sır yazan YENİ dosya(lar): %v\n"+
			"  Şifreli çıktı düz değerden uzundur (sifreliUzunluk): yazılan kolonun genişliğini doğrulayın, kolon dar/belirsizse "+
			"sifreliSirGenislik'e (cmd/server/main.go) ekleyin ve gerekirse genişleten bir göç yazın; sonra bu listeye ekleyin.", yeni)
	}
	if len(kayip) > 0 {
		t.Errorf("listede olup artık gizli.Sakla* çağırmayan dosya(lar): %v — listeden çıkarın", kayip)
	}
}
