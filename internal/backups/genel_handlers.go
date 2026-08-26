// Sistem geneli yedek ayarlari API uclari (hepsi AdminOnly, main.go'da baglanir).
//
//	GET  /api/v1/admin/backups/ayar       -> mevcut ayar + canli disk olcumleri
//	PUT  /api/v1/admin/backups/ayar       -> ayarlari kaydet (parola bos = degistirme)
//	POST /api/v1/admin/backups/ayar/test  -> uzak hedef baglanti testi (kaydetmeden)
package backups

import (
	"encoding/json"
	"net/http"
	"strings"

	"girginospanel/internal/httpx"
)

// GenelAyarGet: mevcut ayarlar + o anki bos alan / depo kullanimi.
func (h *Handlers) GenelAyarGet(w http.ResponseWriter, r *http.Request) {
	g := genelAyarOku(r.Context(), h.DB)
	g.UzakParola = "" // write-only: parola ASLA geri donmez
	if bos, err := diskBosGB(BackupRoot); err == nil {
		g.BosGB = bos
	}
	g.DepoGB = depoKullanimGB()
	httpx.WriteJSON(w, http.StatusOK, g)
}

// genelAyarDogrula: girdi sinirlarini uygular; hata mesaji doner (gecerliyse "").
func genelAyarDogrula(g *GenelAyar) string {
	if g.MinBosGB < 0 || g.MinBosGB > 10000 {
		return "min_bos_gb 0-10000 araliginda olmali"
	}
	if g.MaxDepoGB < 0 || g.MaxDepoGB > 1000000 {
		return "max_depo_gb 0-1000000 araliginda olmali"
	}
	if !g.UzakAktif {
		return ""
	}
	if !gecerliTip(g.UzakTip) {
		return "uzak_tip ftp veya sftp olmali"
	}
	if strings.TrimSpace(g.UzakHost) == "" {
		return "uzak_host bos olamaz"
	}
	if g.UzakPort < 1 || g.UzakPort > 65535 {
		return "uzak_port 1-65535 araliginda olmali"
	}
	if strings.TrimSpace(g.UzakKullanici) == "" {
		return "uzak_kullanici bos olamaz"
	}
	// Kontrol karakteri = lftp/ssh komut satirina enjeksiyon riski; girdide reddet.
	for _, v := range []string{g.UzakHost, g.UzakKullanici, g.UzakParola, g.UzakDizin} {
		if strings.ContainsAny(v, "\r\n\x00") {
			return "alanlar satir sonu / null karakter iceremez"
		}
	}
	return ""
}

// GenelAyarSet: ayarlari kaydeder. uzak_parola bos gelirse mevcut parola korunur.
func (h *Handlers) GenelAyarSet(w http.ResponseWriter, r *http.Request) {
	var g GenelAyar
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&g); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz istek govdesi")
		return
	}
	if g.UzakTip == "" {
		g.UzakTip = "sftp"
	}
	if g.UzakPort == 0 {
		if g.UzakTip == "ftp" {
			g.UzakPort = 21
		} else {
			g.UzakPort = 22
		}
	}
	if strings.TrimSpace(g.UzakDizin) == "" {
		g.UzakDizin = "/"
	}
	if msg := genelAyarDogrula(&g); msg != "" {
		httpx.WriteError(w, http.StatusBadRequest, msg)
		return
	}
	if err := genelAyarYaz(r.Context(), h.DB, &g); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "ayar kaydedilemedi: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// GenelAyarTest: uzak hedefi kaydetmeden dener. Parola bos gelirse KAYITLI parola
// kullanilir (UI parolayi geri okuyamadigi icin test her seferinde yeniden yazdirmasin).
func (h *Handlers) GenelAyarTest(w http.ResponseWriter, r *http.Request) {
	var g GenelAyar
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&g); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz istek govdesi")
		return
	}
	g.UzakAktif = true // test daima uzak alanlari dogrular
	if g.UzakTip == "" {
		g.UzakTip = "sftp"
	}
	if g.UzakPort == 0 {
		if g.UzakTip == "ftp" {
			g.UzakPort = 21
		} else {
			g.UzakPort = 22
		}
	}
	if g.UzakParola == "" {
		g.UzakParola = genelAyarOku(r.Context(), h.DB).UzakParola
	}
	if msg := genelAyarDogrula(&g); msg != "" {
		httpx.WriteError(w, http.StatusBadRequest, msg)
		return
	}
	if err := testConnection(r.Context(), g.hedef()); err != nil {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": false, "hata": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
