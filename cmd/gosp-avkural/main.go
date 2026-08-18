// gosp-avkural — antivirüs kural setini İMZALI paket olarak üretir (yayın aracı).
//
//	gosp-avkural --imza-anahtari /root/.gosp-paket-imza --surum 1 \
//	             --uretim 2026-08-18T00:00:00Z --cikti kurallar.gospav
//
// 🔴 Kural seti KAYNAĞI şu an motorun TabanSet'i (avmotor). İleride ek kurallar
// bir JSON dosyasından okunup birleştirilebilir; şimdilik taban seti sürümleyip
// imzalıyoruz ki dağıtım zinciri (imza → ayna → ajan doğrulama) uçtan uca kurulsun.
package main

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"crypto/ed25519"

	"girginospanel/internal/avmotor"
	"girginospanel/internal/avpaket"
)

func main() {
	imzaYol := flag.String("imza-anahtari", "/root/.gosp-paket-imza", "Ed25519 özel anahtar (hex)")
	surum := flag.Int("surum", 1, "kural seti sürümü (ajan yalnız DAHA YÜKSEK sürümü yükler)")
	uretim := flag.String("uretim", "", "üretim zamanı RFC3339 (boşsa hata — deterministik olsun)")
	ekJSON := flag.String("ek-kurallar", "", "isteğe bağlı ek kural JSON dosyası ({kurallar:[...]})")
	cikti := flag.String("cikti", "kurallar.gospav", "imzalı paket çıktı dosyası")
	flag.Parse()

	if *uretim == "" {
		oldu("--uretim zorunlu (RFC3339)")
	}
	skHex, err := os.ReadFile(*imzaYol)
	if err != nil {
		oldu("imza anahtarı okunamadı: " + err.Error())
	}
	skHam, err := hex.DecodeString(trim(string(skHex)))
	if err != nil || len(skHam) != ed25519.PrivateKeySize {
		oldu("imza anahtarı geçersiz (64 bayt hex bekleniyor)")
	}
	sk := ed25519.PrivateKey(skHam)

	// Taban seti + (varsa) ek kurallar
	set := avmotor.TabanSet()
	set.Surum = *surum
	set.Uretim = *uretim
	if *ekJSON != "" {
		b, err := os.ReadFile(*ekJSON)
		if err != nil {
			oldu("ek kurallar okunamadı: " + err.Error())
		}
		var ek struct {
			Kurallar []avmotor.Kural `json:"kurallar"`
		}
		if json.Unmarshal(b, &ek) != nil {
			oldu("ek kurallar JSON hatası")
		}
		set.Kurallar = append(set.Kurallar, ek.Kurallar...)
	}

	setJSON, err := json.Marshal(set)
	if err != nil {
		oldu("set serileştirilemedi: " + err.Error())
	}

	paket, err := avpaket.Olustur(setJSON, sk, *surum, *uretim)
	if err != nil {
		oldu("paket üretilemedi: " + err.Error())
	}
	if err := os.WriteFile(*cikti, paket, 0o644); err != nil {
		oldu("çıktı yazılamadı: " + err.Error())
	}
	fmt.Printf("✓ imzalı kural paketi: %s (sürüm %d, %d kural, %d bayt)\n",
		*cikti, *surum, len(set.Kurallar), len(paket))
}

func oldu(m string) { fmt.Fprintln(os.Stderr, "✗ "+m); os.Exit(1) }
func trim(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}
