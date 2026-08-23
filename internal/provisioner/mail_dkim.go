package provisioner

// mail_dkim.go — Mail eklentisi DKIM'ini panel DNS'ine yayınlar.
//
// 🔴 SORUN (üretimde ölçüldü — mail-tester DKIM -3): iki ayrı DKIM yöneticisi
// çakışıyordu. Panel DNS (internal/dns EnsureDKIM) kendi RSA key'ini "default"
// selector ile üretip DNS'e "default._domainkey" koyuyordu. Mail eklentisi
// (opendkim) ise AYRI bir key'i "mail" selector ile üretip mailleri s=mail ile
// imzalıyordu. Alıcı mail._domainkey.<d> arıyor, DNS'te yalnız default._domainkey
// (üstelik farklı key) var → public key bulunamıyor → DKIM doğrulanamıyor.
//
// ÇÖZÜM: mail eklentisi aktif bir domain için, imzalamayı yapan GERÇEK key
// (eklentinin mail selector public key'i) DNS'e "mail._domainkey" olarak
// yayınlanır; artık kullanılmayan "default._domainkey" kaldırılır. Public key
// eklenti soketinden alınır (GET /dkim/<domain>) — tek/çok sunucu fark etmez.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// MailDKIMTXTAl — mail eklentisinin bu domain için ürettiği DKIM DNS TXT
// değerini (v=DKIM1; k=rsa; p=...) döndürür. Eklenti kurulu değilse/yoksa boş.
func MailDKIMTXTAl(alanAdi string) (string, error) {
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", mailEklentiSoket)
	}}
	cl := &http.Client{Transport: tr, Timeout: 15 * time.Second}
	req, _ := http.NewRequest("GET", "http://mail/dkim/"+alanAdi, nil)
	req.Header.Set("X-Gosp-Rol", "admin") // core→eklenti güvenilir kanal (yerel soket)
	resp, err := cl.Do(req)
	if err != nil {
		return "", fmt.Errorf("mail eklentisine ulaşılamadı: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("mail eklenti dkim reddetti (%d): %s", resp.StatusCode, string(b))
	}
	var d struct {
		DNSTxt string `json:"dns_txt"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&d); err != nil {
		return "", err
	}
	return d.DNSTxt, nil
}

// MailDKIMGonderTest — bağlantı denemesi (soket var mı). Yalnız tanı için.
func MailDKIMGonderTest() bool {
	_, err := net.Dial("unix", mailEklentiSoket)
	return err == nil
}
