package provisioner

import (
	_ "embed"
	"os"
	"path/filepath"
)

// ── Marka varliklari (Lottie animasyonlari + oynatici) ───────────────────────
// Binary'ye GOMULU tutulur; Init'te root-sahipli paylasilan dizine yazilir ve
// vhost'lardaki `location ^~ /_gosp/` ile servis edilir. Boylece:
//   - her domainin index.html'i KUCUK kalir (varliklar paylasimli + onbelleklenir),
//   - tenant bu dosyalari DEGISTIREMEZ (kendi home'unda degil),
//   - dis kaynak (CDN) yok → cevrimdisi/kisitli aglarda da calisir.
//
// Animasyon yuklenemezse (eski vhost'ta /_gosp/ yoksa) sayfa satir-ici SVG
// cizime duser — kirik gorsel olmaz.

//go:embed marka/lottie.min.js
var lottieJS []byte

//go:embed marka/hazir.json
var animHazir []byte

//go:embed marka/yok404.json
var anim404 []byte

//go:embed marka/askida.json
var animAskida []byte

//go:embed marka/hata500.json
var anim500 []byte

// markaVarliklar: dosya adi → icerik.
func markaVarliklar() map[string][]byte {
	return map[string][]byte{
		"lottie.min.js": lottieJS,
		"hazir.json":    animHazir,
		"yok404.json":   anim404,
		"hata500.json":  anim500,
		"askida.json":   animAskida,
	}
}

// EnsureMarkaAssets: animasyon varliklarini paylasilan dizine yazar (idempotent —
// icerik degismediyse dokunmaz). Init'ten cagrilir.
func EnsureMarkaAssets() {
	if err := os.MkdirAll(hataSayfaDizin, 0o755); err != nil {
		return
	}
	for ad, icerik := range markaVarliklar() {
		yol := filepath.Join(hataSayfaDizin, ad)
		if mevcut, err := os.ReadFile(yol); err == nil && string(mevcut) == string(icerik) {
			continue
		}
		_ = os.WriteFile(yol, icerik, 0o644)
	}
}
