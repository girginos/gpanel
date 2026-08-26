// Site taşımada POSTA (kutular + mail verisi) aktarımı — Plesk kaynaktan.
//
// Akış: kaynak Plesk'ten domainin gerçek kutuları keşfedilir (mail.postbox='true'),
// düz-metin parolalar mail_auth_view'dan alınır (kullanıcılar parolalarını KORUR),
// kutular gPanel mail eklentisine (yerel UNIX soket) oluşturulur, ardından maildir
// verisi rsync ile taşınır (/var/qmail/mailnames/<d>/<n>/Maildir/ → /var/vmail/<d>/<n>/).
//
// 🔴 Mail eklentisi (paralı) kurulu+etkin değilse hata döner → adım "taşınamadı"
// uyarısı bırakır, site taşımanın geri kalanı ETKİLENMEZ.
package tasima

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// mailEklentiSoket — core→eklenti güvenilir yerel kanal (aynı kutu, out-of-process).
const mailEklentiSoket = "/run/girginospanel/eklenti-mail.sock"

type mailHesap struct {
	Yerel  string // e-postanın @ öncesi (mail_name)
	Parola string // düz metin (mail_auth_view) — boşsa çağıran yeni üretir
}

// mailEklentiPOST — mail eklentisine yerel soket üzerinden JSON POST'lar.
func mailEklentiPOST(ctx context.Context, yol string, govde any) (int, []byte, error) {
	b, _ := json.Marshal(govde)
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", mailEklentiSoket)
	}}
	cl := &http.Client{Transport: tr, Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "POST", "http://mail"+yol, bytes.NewReader(b))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gosp-Rol", "admin")
	resp, err := cl.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	return resp.StatusCode, rb, nil
}

// mailEklentiGET — mail eklentisinden yerel soket uzerinden JSON GET okur.
func mailEklentiGET(ctx context.Context, yol string) (int, []byte, error) {
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", mailEklentiSoket)
	}}
	cl := &http.Client{Transport: tr, Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", "http://mail"+yol, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("X-Gosp-Rol", "admin")
	resp, err := cl.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, rb, nil
}

// mailDomainVar — domain mail eklentisinde tanimli mi (GET /domainler).
// Yanit sekli: {"domainler":[{"id":N,"ad":"...","kutu_sayisi":N}]}.
func mailDomainVar(ctx context.Context, alanAdi string) bool {
	kod, govde, err := mailEklentiGET(ctx, "/domainler")
	if err != nil || kod != 200 {
		return false
	}
	var yanit struct {
		Domainler []struct {
			Ad string `json:"ad"`
		} `json:"domainler"`
	}
	if json.Unmarshal(govde, &yanit) != nil {
		return false
	}
	for _, d := range yanit.Domainler {
		if strings.EqualFold(d.Ad, alanAdi) {
			return true
		}
	}
	return false
}

// mailAktar — domainin kutularını + mail verisini kaynaktan taşır. Döndürür:
// taşınan kutu sayısı, uyarılar, hata (yalnız kesif/eklenti erişim hatasında).
func (h *Handlers) mailAktar(ctx context.Context, k *Kaynak, alanAdi string, log func(string, ...any)) (int, []string, error) {
	var uyarilar []string

	hesaplar, err := pleskMailKesfet(ctx, k, alanAdi)
	if err != nil {
		return 0, nil, fmt.Errorf("kaynak posta kesfi: %w", err)
	}
	if len(hesaplar) == 0 {
		return 0, []string{"Posta: kaynakta kutu yok — taşınmadı"}, nil
	}

	// Mail domainini gPanel'de GARANTI et — ZATEN VARSA dokunma (idempotent).
	// 🔴 Mail eklentisi domainEkle duplicate'te 409 DEGIL 500 doner; koda guvenme,
	// once VARLIGA bak (GET /domainler), yoksa ekle, sonra tekrar dogrula. Onceden
	// mevcut domain (ornek: islam-tr.org zaten kayitli) tum posta adimini "taşınamadı"
	// yapiyordu; keşif kutulari BULMUS olsa bile.
	if !mailDomainVar(ctx, alanAdi) {
		kod, govde, err := mailEklentiPOST(ctx, "/domainler", map[string]string{"ad": alanAdi})
		if err != nil {
			return 0, nil, fmt.Errorf("mail eklentisine ulaşılamadı (kurulu/etkin mi?): %w", err)
		}
		if kod != 200 && kod != 201 && !mailDomainVar(ctx, alanAdi) {
			return 0, nil, fmt.Errorf("mail domaini eklenemedi (%d): %s", kod, strings.TrimSpace(string(govde)))
		}
	}

	sayi := 0
	for _, hs := range hesaplar {
		email := hs.Yerel + "@" + alanAdi
		parola := hs.Parola
		if len(parola) < 6 {
			parola = rastgeleParola(16)
			uyarilar = append(uyarilar, "Posta: "+email+" kaynak parolası alınamadı — yeni parola üretildi")
		}

		kod, govde, err := mailEklentiPOST(ctx, "/hesaplar", map[string]any{
			"email": email, "parola": parola, "quota_mb": 0,
		})
		if err != nil {
			uyarilar = append(uyarilar, "Posta: "+email+" oluşturulamadı (bağlantı)")
			continue
		}
		zatenVar := kod == 409 || strings.Contains(strings.ToLower(string(govde)), "zaten")
		if kod != 201 && !zatenVar {
			uyarilar = append(uyarilar, "Posta: "+email+" oluşturulamadı: "+strings.TrimSpace(string(govde)))
			continue
		}

		// Maildir verisini taşı: Plesk (qmail düzeni) → gPanel (dovecot maildir).
		domainDir := "/var/vmail/" + alanAdi
		uzak := "/var/qmail/mailnames/" + alanAdi + "/" + hs.Yerel + "/Maildir/"
		yerel := domainDir + "/" + hs.Yerel + "/"
		if err := os.MkdirAll(yerel, 0o700); err != nil {
			uyarilar = append(uyarilar, "Posta: "+email+" hedef maildir hazırlanamadı")
		} else if _, err := k.RsyncCek(ctx, uzak, yerel, "--exclude=dovecot*", "--exclude=maildirsize"); err != nil {
			uyarilar = append(uyarilar, "Posta: "+email+" mesajları kısmen/hiç taşınamadı")
		}
		// 🔴 Sahiplik: DOMAIN dizini + kutu dizini vmail:vmail. MkdirAll domain parent'ını
		// root:root 0700 yaratır → vmail(dovecot) TRAVERSE EDEMEZ → sieve+teslim "Permission
		// denied". Yalnız leaf'i chown YETMEZ; parent'ı da düzelt.
		_ = exec.CommandContext(ctx, "chown", "vmail:vmail", domainDir).Run()
		_ = exec.CommandContext(ctx, "chown", "-R", "vmail:vmail", yerel).Run()

		sayi++
		log("Posta: %s taşındı", email)
	}
	return sayi, uyarilar, nil
}

// pleskMailKesfet — kaynak Plesk'ten domainin GERÇEK kutularını + düz parolalarını alır.
func pleskMailKesfet(ctx context.Context, k *Kaynak, alanAdi string) ([]mailHesap, error) {
	// postbox='true' = gerçek kutu (yalnız forward/alias olan kayıtlar hariç).
	q := `plesk db -Ne "SELECT m.mail_name FROM mail m JOIN domains d ON m.dom_id=d.id WHERE d.name='` +
		alanAdi + `' AND m.postbox='true'" 2>/dev/null`
	ham, err := k.Calistir(ctx, q)
	if err != nil {
		return nil, err
	}
	yereller := map[string]bool{}
	for _, satir := range strings.Split(ham, "\n") {
		if s := strings.ToLower(strings.TrimSpace(satir)); s != "" {
			yereller[s] = true
		}
	}
	if len(yereller) == 0 {
		return nil, nil
	}

	// mail_auth_view TÜM hesapların düz parolasını verir; bu domaine ait olanları süz.
	pw, _ := k.Calistir(ctx, "/usr/local/psa/admin/sbin/mail_auth_view 2>/dev/null")
	parolalar := parseMailAuthView(pw, alanAdi)

	var out []mailHesap
	for yerel := range yereller {
		out = append(out, mailHesap{Yerel: yerel, Parola: parolalar[yerel]})
	}
	return out, nil
}

// parseMailAuthView — mail_auth_view çıktısından <yerelkısım>→parola eşlemesi
// (yalnız verilen domain). Biçim sürümden sürüme değişir: '|' ayraçlı tablo VEYA
// boşluk ayraçlı — ikisi de denenir (email + parola sütunları).
func parseMailAuthView(cikti, alanAdi string) map[string]string {
	m := map[string]string{}
	sonek := "@" + strings.ToLower(alanAdi)
	for _, satir := range strings.Split(cikti, "\n") {
		if !strings.Contains(strings.ToLower(satir), sonek) {
			continue
		}
		var email, parola string
		if strings.Contains(satir, "|") {
			var alanlar []string
			for _, x := range strings.Split(satir, "|") {
				if x = strings.TrimSpace(x); x != "" {
					alanlar = append(alanlar, x)
				}
			}
			if len(alanlar) >= 2 {
				email, parola = alanlar[0], alanlar[1]
			}
		} else if f := strings.Fields(satir); len(f) >= 2 {
			email, parola = f[0], f[1]
		}
		email = strings.ToLower(strings.TrimSpace(email))
		if strings.HasSuffix(email, sonek) {
			m[strings.TrimSuffix(email, sonek)] = parola
		}
	}
	return m
}

// rastgeleParola — kaynak parolası alınamayan kutular için güvenli yedek.
func rastgeleParola(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "Gosp!" + hex.EncodeToString([]byte(time.Now().String()))[:12]
	}
	return "Gosp!" + hex.EncodeToString(b)[:n]
}
