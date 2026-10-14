package eklenti

// Müşteri (hosting sahibi) erişim bayrağı — cp_eklentiler.musteri_erisim — eklentinin
// KENDİ bildirimiyle yönetilir.
//
// NEDEN: bayrak eskiden hiçbir yerde açılmıyordu (migration 0092 varsayılanı 0), yani
// müşteri paneli E-posta ve Uygulama Çalıştırıcı bölümlerini hiçbir sunucuda göstermiyordu.
// Ama bayrağı körlemesine açmak da yanlıştır: bu eklentiler yönetici/bayi yüzeyidir (mail:
// IP havuzu, DNSBL, genel ayar...; çalıştırıcı: çalışma zamanı kurma/silme, sürüm geçmişi,
// ortam değişkenleri...). Müşteriye yalnız kendi alan adını ilgilendiren uçları açan kapı
// eklentide uygulanır (eklenti: musteri_kapi.go) ve /saglik yanıtında
// "musteri_kapisi": <sürüm> olarak bildirilir. Panel bayrağı YALNIZ bu bildirimi görünce açar:
//
//   - eklenti kapıyı bildiriyor + sağlıklı  → musteri_erisim=1
//   - bildirmiyor (eski sürüm), bilinmeyen sürüm, bozuk yanıt, ulaşılamıyor → 0
//
// Eski eklenti sürümü olan sunucuda bayrak ASLA 1 kalmaz (her sağlık taramasında,
// dakikada bir, yeniden hesaplanır). Bildirim tek başına yetmez: bayrağı yalnız
// aşağıdaki listedeki eklentiler için panel yönetir; başka bir eklenti "ben müşteriye
// açığım" demekle kendini açamaz (ör. whitelabel, marka bayrağına dokunulmaz).

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"time"
)

// musteriKapisiYonetilen — bayrağı panelin yönettiği eklentiler ve her birinin tanıdığı
// müşteri kapısı sürümü. Eklentinin bildirdiği sürüm buradakiyle BİREBİR eşit olmalı:
// kapının anlamı değişen daha yeni bir sürüm panel güncellenene dek açılmaz (fail-closed).
var musteriKapisiYonetilen = map[string]int{
	"mail":        1,
	"calistirici": 1,
}

// eklentiMusteriKapisi — eklentinin /saglik yanıtındaki "musteri_kapisi" değeri.
// Okunamayan/bozuk/eksik her durumda 0 (= kapı yok).
func eklentiMusteriKapisi(ctx context.Context, soket string) int {
	ctx, iptal := context.WithTimeout(ctx, 3*time.Second)
	defer iptal()
	tr := &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(c context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(c, "unix", soket)
		},
	}
	defer tr.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://eklenti/saglik", nil)
	if err != nil {
		return 0
	}
	yanit, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		return 0
	}
	defer yanit.Body.Close()
	if yanit.StatusCode != http.StatusOK {
		return 0
	}
	var g struct {
		MusteriKapisi int `json:"musteri_kapisi"`
	}
	// 64 KB üst sınır: sağlık yanıtı küçüktür; bozuk/şişirilmiş gövde "kapı yok" sayılır.
	if json.NewDecoder(io.LimitReader(yanit.Body, 64<<10)).Decode(&g) != nil {
		return 0
	}
	return g.MusteriKapisi
}

// musteriErisimUzlastir — bayrağı eklentinin gerçek durumuna getirir. Yönetilmeyen
// eklentilerde HİÇBİR ŞEY yapmaz.
func (h *Handlers) musteriErisimUzlastir(ctx context.Context, ad, soket string) {
	surum, yonetilen := musteriKapisiYonetilen[ad]
	if !yonetilen {
		return
	}
	// Sağlık ayrıca sorulmaz: yanıt alınamayan (ölü/yeniden başlayan/hatalı) eklenti 0 verir.
	istenen := 0
	if surum > 0 && eklentiMusteriKapisi(ctx, soket) == surum {
		istenen = 1
	}
	// Yalnız değişiklik varsa yazar (dakikada bir tarama gereksiz yazma üretmesin).
	if _, err := h.DB.ExecContext(ctx,
		`UPDATE cp_eklentiler SET musteri_erisim=? WHERE ad=? AND COALESCE(musteri_erisim,0)<>?`,
		istenen, ad, istenen); err != nil {
		log.Printf("eklenti %s: musteri_erisim güncellenemedi: %v", ad, err)
	}
}
