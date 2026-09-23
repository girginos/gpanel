package github

// Webhook port uzlaştırması (ADR-GP-001 rol kapıları).
//
// 🔴 NEDEN: otomatik dağıtım webhook'u GitHub'a
// https://<ip>:<panel portu>/api/v1/git-webhook/<gizli> olarak kaydedilir. Panelin
// genel portu değişip eski port kapanınca (kapı geçişi bitince 8443) GitHub teslimatları
// ölü porta gider ve otomatik dağıtım SESSİZCE durur. Bu uzlaştırma, canlı olmayan
// porttaki webhook'ların adresini panelin genel portuna taşır. GitHub'daki adres elle
// değiştirilmişse onun host'u korunur; zaten canlı bir porttaysa hiç dokunulmaz.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"girginospanel/internal/gizli"
)

var (
	webhookUzlastirMu sync.Mutex
	// webhookUyarildi: aynı bağlantının kalıcı hatası (ör. süresi dolmuş token) her
	// uzlaştırmada (15 dk) günlüğe yeniden yazılmasın.
	webhookUyarildi sync.Map
)

// WebhookPortlariniUzlastir: kayıtlı adresi canlı OLMAYAN bir panel portunda olan
// webhook'ları genel porta taşır. Aynı anda tek çalışma (açılış + bakım döngüsü).
func WebhookPortlariniUzlastir(ctx context.Context, db *sql.DB, genel int, canli map[int]bool) {
	if db == nil || genel <= 0 {
		return
	}
	if !webhookUzlastirMu.TryLock() {
		return
	}
	defer webhookUzlastirMu.Unlock()

	type aday struct {
		domainID, hookID int64
		pat, repo, adres string
	}
	rows, err := db.QueryContext(ctx,
		`SELECT domain_id, pat, secili_repo, webhook_id, webhook_url
		 FROM github_connections WHERE webhook_id > 0 AND webhook_url <> ''`)
	if err != nil {
		log.Printf("github: webhook port uzlaştırması sorgulanamadı: %v", err)
		return
	}
	var adaylar []aday
	for rows.Next() {
		var a aday
		if rows.Scan(&a.domainID, &a.pat, &a.repo, &a.hookID, &a.adres) == nil {
			if _, ok := webhookYeniURL(a.adres, genel, canli); ok {
				adaylar = append(adaylar, a)
			}
		}
	}
	_ = rows.Err()
	rows.Close()

	for _, a := range adaylar {
		son, degisti, err := webhookTasi(ctx, gizli.CozBagli(a.pat, "github-pat"), a.repo, a.hookID, genel, canli)
		if err != nil {
			if _, zaten := webhookUyarildi.LoadOrStore(a.domainID, true); !zaten {
				log.Printf("🔴 github: webhook adresi yeni panel portuna taşınamadı (domain %d) — otomatik dağıtım ölü porta gidiyor: %v", a.domainID, err)
			}
			continue
		}
		webhookUyarildi.Delete(a.domainID)
		// Kayıt GitHub'daki gerçek adrese eşitlenir (elle düzeltilmişse de) → bir dahaki
		// uzlaştırma bu bağlantı için GitHub'a HİÇ gitmez.
		if _, err := db.ExecContext(ctx,
			`UPDATE github_connections SET webhook_url=? WHERE domain_id=? AND webhook_id=?`,
			son, a.domainID, a.hookID); err != nil {
			log.Printf("github: webhook adresi kaydedilemedi (domain %d): %v", a.domainID, err)
			continue
		}
		if degisti {
			log.Printf("github: webhook adresi panelin genel portuna taşındı (domain %d, :%d)", a.domainID, genel)
		}
	}
}

// webhookYeniURL: adres, canlı olmayan bir panel portundaysa aynı adresin genel porta
// taşınmış hâlini döner. Portsuz (443) ya da 1024 altı adres panel portu değildir.
func webhookYeniURL(adres string, genel int, canli map[int]bool) (string, bool) {
	u, err := url.Parse(adres)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return "", false
	}
	p, err := strconv.Atoi(u.Port())
	if err != nil || p < 1024 || p == genel || canli[p] {
		return "", false
	}
	u.Host = net.JoinHostPort(u.Hostname(), strconv.Itoa(genel))
	return u.String(), true
}

// webhookTasi: GitHub'daki GERÇEK adresi okur; canlı olmayan porttaysa (host'u koruyarak)
// genel porta taşır. Dönüş: GitHub'daki son adres, değişti mi.
func webhookTasi(ctx context.Context, pat, repo string, hookID int64, genel int, canli map[int]bool) (string, bool, error) {
	if pat == "" || repo == "" {
		return "", false, errors.New("token ya da repo kaydı yok")
	}
	yol := fmt.Sprintf("/repos/%s/hooks/%d/config", repo, hookID)
	body, st, err := ghCall(ctx, "GET", yol, pat, nil)
	if err != nil {
		return "", false, err
	}
	if st != 200 {
		return "", false, errors.New(patHataMesaji(st, body))
	}
	var mevcut struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(body, &mevcut); err != nil {
		return "", false, fmt.Errorf("webhook yapılandırması çözülemedi: %w", err)
	}
	yeni, ok := webhookYeniURL(mevcut.URL, genel, canli)
	if !ok {
		return mevcut.URL, false, nil // elle düzeltilmiş ya da zaten canlı porta
	}
	cfg := map[string]string{"url": yeni}
	// İmza gizlisi adres yolundaki gizliyle aynıdır (Use); gönderilmezse GitHub'ın onu
	// koruyup korumadığına bel bağlanırdı.
	if i := strings.LastIndex(mevcut.URL, "/api/v1/git-webhook/"); i >= 0 {
		if g := mevcut.URL[i+len("/api/v1/git-webhook/"):]; g != "" && !strings.ContainsAny(g, "/?#") {
			cfg["secret"] = g
		}
	}
	body, st, err = ghCall(ctx, "PATCH", yol, pat, cfg)
	if err != nil {
		return "", false, err
	}
	if st != 200 {
		return "", false, errors.New(patHataMesaji(st, body))
	}
	return yeni, true, nil
}
