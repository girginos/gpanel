package provisioner

// panelmarka_acme_hazir.go — bayi özel panel adı: Let's Encrypt'e GİTMEDEN önce :80 challenge yolunun
// YEREL nginx'te gerçekten canlı olduğunu ölçer.
//
// 🔴 NEDEN: pmNginxUyumla :80 vhost'unu yazar ve reload ister; reload asenkron (yeni worker'lar kırk
// vhost'u + sertifikaları okuyana kadar sürer). acme.sh ise vhost yazıldıktan ~0,6 sn sonra başlar ve
// doğrulama ESKİ yapılandırmaya düşer: bilinmeyen Host → _default80 → /var/www/acme → 404 →
// "Let's Encrypt doğrulaması başarısız" (gpanel.girginos.cloud, 2026-10-05 20:00). Başarısız her doğrulama
// LE hız sınırını yer (saatte 5) ve geri çekilme 10 dk'dan başlar. Jetonu yazıp kendimiz geri okumak
// hem yarışı ortadan kaldırır hem de gerçek bir yapılandırma hatasını LE'ye gitmeden açıkça bildirir.

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Dikişler (testler değiştirir): ölçülen nginx adresi, deneme sayısı ve aralığı (≈ 20 sn).
var (
	pmYolTaban  = "http://127.0.0.1"
	pmYolDeneme = 40
	pmYolAralik = 500 * time.Millisecond
)

// pmAcmeYoluBekle — testler değiştirir. nil dönerse challenge yolu ad için CANLI.
var pmAcmeYoluBekle = pmAcmeYoluBekleGercek

func pmAcmeYoluBekleGercek(ad string) error {
	ham := make([]byte, 16)
	if _, err := rand.Read(ham); err != nil {
		return fmt.Errorf("jeton üretilemedi: %w", err)
	}
	jeton := "gosp-onsinav-" + hex.EncodeToString(ham)
	diz := filepath.Join(pmWebroot, ".well-known", "acme-challenge")
	if err := os.MkdirAll(diz, 0o755); err != nil {
		return fmt.Errorf("challenge dizini: %w", err)
	}
	yol := filepath.Join(diz, jeton)
	if err := os.WriteFile(yol, []byte(jeton), 0o644); err != nil {
		return fmt.Errorf("challenge jetonu yazılamadı: %w", err)
	}
	defer os.Remove(yol)
	_, _ = exec.Command("restorecon", "-R", pmWebroot).CombinedOutput()

	cl := &http.Client{
		Timeout: 3 * time.Second,
		// Yönlendirme İZLENMEZ: LE challenge'ı 200 ile doğrudan almalı.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	var son error
	for i := 0; i < pmYolDeneme; i++ {
		req, err := http.NewRequest(http.MethodGet, pmYolTaban+"/.well-known/acme-challenge/"+jeton, nil)
		if err != nil {
			return err
		}
		req.Host = ad
		resp, err := cl.Do(req)
		if err == nil {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
			resp.Body.Close()
			switch {
			case resp.StatusCode == http.StatusOK && strings.TrimSpace(string(b)) == jeton:
				return nil
			case resp.StatusCode == http.StatusOK:
				son = fmt.Errorf("challenge yolu BAŞKA içerik döndü — bu adı başka bir vhost karşılıyor")
			default:
				son = fmt.Errorf("challenge yolu HTTP %d döndü (200 bekleniyordu)", resp.StatusCode)
			}
		} else {
			son = fmt.Errorf("challenge yolu okunamadı: %v", err)
		}
		time.Sleep(pmYolAralik)
	}
	return son
}
