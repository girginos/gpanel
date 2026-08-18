package avmotor

// Uzak imzalı kural seti yükleme.
//
// 🔴 GÜVEN ZİNCİRİ: ajan gömülü TabanSet ile HER ZAMAN çalışır. Uzak set yalnız
// onu GÜNCELLER ve ancak (a) imzası gömülü açık anahtarla doğrulanırsa VE (b)
// sürümü tabandan/diskten YÜKSEKSE yüklenir. Doğrulanmayan paket REDDEDİLİR ve
// sessizce tabana düşülür — "kural yok, hiçbir şey yapma" durumu ASLA olmaz.
//
// 🔴 GITHUB BAĞIMLILIĞI DERSİ: disk önbellek → kendi ayna. Ağ kesikse en son
// doğrulanmış set diskten yüklenir; hiç yoksa gömülü taban.

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"girginospanel/internal/avpaket"
)

const (
	uzakKuralURL  = "https://surum.girginos.io/av/kurallar.gospav"
	kuralDiskYolu = "/var/lib/girginospanel/av/kurallar.gospav"
)

// 🔴 GÖMÜLÜ AÇIK ANAHTAR — kural setini imzalayan Ed25519 anahtarının açık
// yarısı. Özel anahtar YALNIZ yayın sunucusundadır (/root/.gosp-paket-imza).
// Bu anahtar bir sahtekârın kural enjekte etmesini engeller.
var kuralPubKey = mustHexPK("7e6942943305bd76c79c2ace4155aa837f563e7a15d63060aedfece11f4065f9")

// GuncelSet — en güncel DOĞRULANMIŞ kural setini döner: uzak (imzalı) → disk →
// gömülü taban. tabanSurum: mevcut motorun sürümü (bundan düşük set yüklenmez).
//
// aginenKapali=true ise yalnız disk + taban (izole sunucu).
func GuncelSet(tabanSurum int, aginenKapali bool) KuralSeti {
	taban := TabanSet()
	enIyi := taban
	if taban.Surum < tabanSurum {
		enIyi.Surum = tabanSurum
	}

	// 1) Disk önbelleği (en son doğrulanmış)
	if set, ok := diskSetOku(); ok && set.Surum > enIyi.Surum {
		enIyi = set
	}

	if aginenKapali {
		return enIyi
	}

	// 2) Uzak imzalı paket
	if set, ham, ok := uzakSetCek(); ok && set.Surum > enIyi.Surum {
		diskSetYaz(ham) // yalnız DOĞRULANMIŞ paketi diske yaz
		enIyi = set
	}
	return enIyi
}

func uzakSetCek() (KuralSeti, []byte, bool) {
	var bos KuralSeti
	cl := &http.Client{Timeout: 20 * time.Second}
	resp, err := cl.Get(uzakKuralURL)
	if err != nil {
		return bos, nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return bos, nil, false
	}
	ham, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return bos, nil, false
	}
	return dogrulaVeCoz(ham)
}

// dogrulaVeCoz — paketi imza doğrular + çözer. Doğrulanmazsa (bos, nil, false).
func dogrulaVeCoz(ham []byte) (KuralSeti, []byte, bool) {
	var bos KuralSeti
	_, setJSON, err := avpaket.Ac(ham, kuralPubKey)
	if err != nil {
		return bos, nil, false // 🔴 imza tutmadı → REDDET (sahte kural olabilir)
	}
	var set KuralSeti
	if json.Unmarshal(setJSON, &set) != nil || len(set.Kurallar) == 0 {
		return bos, nil, false
	}
	return set, ham, true
}

func diskSetOku() (KuralSeti, bool) {
	ham, err := os.ReadFile(kuralDiskYolu)
	if err != nil {
		return KuralSeti{}, false
	}
	// 🔴 Diskten okurken de İMZA DOĞRULA: disk zehirlenmesine karşı. Diskteki
	// paket de imzalıdır; imzasız bir dosya oraya konsa reddedilir.
	set, _, ok := dogrulaVeCoz(ham)
	return set, ok
}

func diskSetYaz(ham []byte) {
	if err := os.MkdirAll(filepath.Dir(kuralDiskYolu), 0o755); err != nil {
		return
	}
	tmp := kuralDiskYolu + ".tmp"
	if os.WriteFile(tmp, ham, 0o644) == nil {
		_ = os.Rename(tmp, kuralDiskYolu)
	}
}

func mustHexPK(s string) ed25519.PublicKey {
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != ed25519.PublicKeySize {
		panic("avmotor: gomulu pubkey gecersiz")
	}
	return ed25519.PublicKey(b)
}
