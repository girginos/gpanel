package hesapaski

// posta.go — askının POSTA adımı: hesabın alan adlarında posta GİRİŞİ kapanır
// (IMAP/POP3, SMTP AUTH gönderim, webmail tek tık), GELEN POSTA AÇIK KALIR
// (kullanıcı kararı 2026-10-03).
//
// Askı bayrağı mail eklentisinin veritabanındadır (virtual_domains.askida);
// panel onu eklentinin UNIX soketinden yazar (PUT /domainler/aski, X-Gosp-Rol:
// admin — domains/mailsync.go ile aynı desen). Eklenti askıya almada açık
// oturumları da düşürür. Dovecot tarafı: provisioner.PostaAskiAltyapisi.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"girginospanel/internal/provisioner"
)

// postaAltyapiEtkin — dovecot askıyı uyguluyor mu. Testler sahteyle değiştirir.
var postaAltyapiEtkin = provisioner.PostaAskiEtkin

// postaIstekSuresi — eklenti çağrısı başına üst süre (askıya almada doveadm
// kick de koşar).
var postaIstekSuresi = 20 * time.Second

// PostaAski — hesabın ana + alt alan adlarının posta girişini kapatır/açar.
//
//   - Mail eklentisi kayıtlı değil / soketi yok → nil (posta yok).
//   - Eklenti kayıtlı ama KAPALI (aktif=0; lisans) → en iyi çaba: soket
//     ayaktaysa bayrak yine yazılır (açılışta kalmış bir askı temizlensin),
//     hata yutulur.
//   - Alan adında posta kurulmamış → o ad atlanır.
//   - Soket/HTTP hatası → hata (hesapaski bunu KismiHata sayar; açılış
//     süpürmesi yeniden dener).
//   - Askıya almada en az bir alan adında posta varsa dovecot'un askıyı
//     GERÇEKTEN uyguladığı doğrulanır (provisioner.PostaAskiEtkin); uygulamıyorsa
//     hata — "posta girişi kapandı" yalanı söylenmez.
func PostaAski(ctx context.Context, db *sql.DB, domainID int64, askida bool) error {
	var aktif int
	var soket string
	err := db.QueryRowContext(ctx,
		`SELECT COALESCE(aktif,0), COALESCE(soket,'') FROM cp_eklentiler WHERE ad='mail'`).Scan(&aktif, &soket)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("mail eklentisi durumu okunamadı: %w", err)
	}
	if soket == "" {
		return nil
	}
	adlar, err := postaAlanAdlari(ctx, db, domainID)
	if err != nil {
		return err
	}
	postaVar, err := postaSoketAski(ctx, soket, adlar, askida)
	if aktif != 1 {
		if err != nil {
			log.Printf("hesap askısı: mail eklentisi kapalı, posta bayrağı yazılamadı (yok sayıldı): %v", err)
		}
		return nil
	}
	if err != nil {
		return err
	}
	if askida && postaVar && !postaAltyapiEtkin() {
		return errors.New("posta askı bayrağı yazıldı ama dovecot yapılandırması onu uygulamıyor " +
			"(password_query askı koşulu / webmail master pass=yes eksik) — panel yeniden başlatılınca açılış uzlaştırması kurar")
	}
	return nil
}

// postaAlanAdlari — hesabın ana alan adı + alt alan adları.
// 🔴 subdomanlar.tam_ad utf8mb4_general_ci, domains.alan_adi utf8mb4_unicode_ci: UNION'da kolasyon eşitlenmezse 1271.
func postaAlanAdlari(ctx context.Context, db *sql.DB, domainID int64) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT alan_adi FROM domains WHERE id=?
		 UNION SELECT tam_ad COLLATE utf8mb4_unicode_ci FROM subdomanlar WHERE domain_id=?`, domainID, domainID)
	if err != nil {
		return nil, fmt.Errorf("posta alan adları okunamadı: %w", err)
	}
	defer rows.Close()
	var adlar []string
	for rows.Next() {
		var ad string
		if err := rows.Scan(&ad); err != nil {
			return nil, fmt.Errorf("posta alan adları okunamadı: %w", err)
		}
		if ad = strings.ToLower(strings.TrimSpace(ad)); ad != "" {
			adlar = append(adlar, ad)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("posta alan adları eksik okundu: %w", err)
	}
	return adlar, nil
}

// postaSoketAski — her alan adı için eklentinin askı ucunu çağırır. Döner:
// en az bir alan adında posta var mı, hatalar.
func postaSoketAski(ctx context.Context, soket string, adlar []string, askida bool) (bool, error) {
	cl := &http.Client{
		Timeout: postaIstekSuresi,
		Transport: &http.Transport{DialContext: func(c context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(c, "unix", soket)
		}},
	}
	defer cl.CloseIdleConnections()
	postaVar := false
	var hatalar []error
	for _, ad := range adlar {
		govde, _ := json.Marshal(map[string]any{"ad": ad, "askida": askida})
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, "http://x/domainler/aski", bytes.NewReader(govde))
		if err != nil {
			return postaVar, err
		}
		req.Header.Set("X-Gosp-Rol", "admin")
		req.Header.Set("Content-Type", "application/json")
		resp, err := cl.Do(req)
		if err != nil {
			// Soket yok / eklenti çökmüş: öteki adlar da düşer — bir kez raporla.
			return postaVar, fmt.Errorf("mail eklentisine ulaşılamadı: %w", err)
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		var y struct {
			Posta bool   `json:"posta"`
			Hata  string `json:"hata"`
		}
		_ = json.Unmarshal(b, &y)
		switch {
		case resp.StatusCode == http.StatusOK:
			if y.Posta {
				postaVar = true
			}
		case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed:
			// Eski eklenti: askı ucu yok. Açarken kapatılmış bir şey olamaz → nil;
			// kapatırken posta girişi AÇIK kalır → hata (eklenti güncellenmeli).
			if !askida {
				return postaVar, nil
			}
			return postaVar, errors.New("mail eklentisi posta askısını desteklemiyor (eklentiyi güncelleyin) — posta girişi açık")
		default:
			msj := y.Hata
			if msj == "" {
				msj = http.StatusText(resp.StatusCode)
			}
			hatalar = append(hatalar, fmt.Errorf("%s: mail eklentisi HTTP %d: %s", ad, resp.StatusCode, msj))
		}
	}
	return postaVar, errors.Join(hatalar...)
}
