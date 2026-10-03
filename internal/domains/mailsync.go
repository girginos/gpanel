// Plan posta limitlerini mail eklentisine senkronlar (best-effort).
package domains

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/efektiflimit"
)

// mailKutuLimitSenkron — domainin EFEKTİF (plan ⊕ üst yazım) posta limitlerini (max_email +
// saatlik_mail_limiti) mail eklentisine iletir; enforcement PLUGIN tarafındadır
// (virtual_domains.kutu_limiti / saatlik_limiti). Best-effort: mail eklentisi
// yok/kapalı/soketsiz ise ya da bu domain için mail servisi kurulmamışsa sessizce
// döner — planı asla bloklamaz. Yalnız admin-header ile çağırır (soket zaten iç).
func mailKutuLimitSenkron(ctx context.Context, db *sql.DB, domainID int64, alanAdi string) {
	// Efektif değer: plan ⊕ hostinge özel üst yazım (tek kaynak efektiflimit).
	el, err := efektiflimit.Oku(ctx, db, domainID)
	if err != nil {
		return
	}
	kutu, saatlik, kotaMB := el.Efektif("max_email", 0), el.Efektif("saatlik_mail_limiti", 0), el.Efektif("mail_kutu_kota_mb", 0)
	var aktif int
	var soket string
	if err := db.QueryRowContext(ctx,
		`SELECT aktif, COALESCE(soket,'') FROM cp_eklentiler WHERE ad='mail'`).Scan(&aktif, &soket); err != nil || aktif != 1 || soket == "" {
		return
	}
	cl := &http.Client{
		Timeout: 6 * time.Second,
		Transport: &http.Transport{DialContext: func(c context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(c, "unix", soket)
		}},
	}
	// Mail domain id'yi ada göre bul.
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://x/domainler", nil)
	req.Header.Set("X-Gosp-Rol", "admin")
	resp, err := cl.Do(req)
	if err != nil {
		return
	}
	var dl struct {
		Domainler []struct {
			ID int    `json:"id"`
			Ad string `json:"ad"`
		} `json:"domainler"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&dl)
	resp.Body.Close()
	mid := 0
	for _, d := range dl.Domainler {
		if strings.EqualFold(d.Ad, alanAdi) {
			mid = d.ID
			break
		}
	}
	if mid == 0 {
		return // bu domain için mail servisi kurulmamış
	}
	// kota_mb: kutu BAŞINA depolama sınırı. Eklenti bunu virtual_users.quota_bytes'a
	// çevirip Dovecot quota eklentisine uygulatır. Eski eklenti sürümleri bu alanı
	// YOK SAYAR (JSON'da bilinmeyen alan) — geriye dönük uyumlu.
	body, _ := json.Marshal(map[string]int{"kutu_limiti": kutu, "saatlik_limiti": saatlik, "kota_mb": kotaMB})
	pr, _ := http.NewRequestWithContext(ctx, "PUT",
		"http://x/domainler/"+strconv.Itoa(mid)+"/limit", bytes.NewReader(body))
	pr.Header.Set("X-Gosp-Rol", "admin")
	pr.Header.Set("Content-Type", "application/json")
	if pres, err := cl.Do(pr); err == nil {
		pres.Body.Close()
		// 🔴 Log nötrleme: alan adı %q ile yazılır — CR/LF vb. kaçışlanır,
		// kullanıcı kaynaklı değerle sahte log satırı enjekte edilemez.
		log.Printf("mail limit senkron: %q → kutu=%d saatlik=%d kota=%dMB", alanAdi, kutu, saatlik, kotaMB)
	}
}
