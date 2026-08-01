package backups

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"

	"github.com/go-chi/chi/v5"
)

// restoreIstek: geri yükleme gövdesi (tüm alanlar opsiyonel; boş gövde = mod "tam").
type restoreIstek struct {
	Mod     string   `json:"mod"`      // tam | dosyalar | veritabani | dosya | db
	Temiz   bool     `json:"temiz"`    // mod=tam/dosyalar: rsync --delete (ESKI ezme davranışı)
	Yollar  []string `json:"yollar"`   // mod=dosya: arşiv-içi göreli yollar
	Hedef   string   `json:"hedef"`    // mod=dosya: "klasor" (varsayılan) | "yerinde"
	DB      string   `json:"db"`       // mod=db (zorunlu) / veritabani (opsiyonel filtre)
	HedefDB string   `json:"hedef_db"` // mod=db: "" = üzerine yaz, dolu = yeni DB adı
}

// Restore: POST /api/v1/domains/:id/backups/:bid/geriyukle
// Granüler geri yükleme: tam / yalnız dosyalar / yalnız DB / seçili dosyalar / tek DB.
// Varsayılanlar NON-DESTRUCTIVE: tam/dosyalar EZMEZ (temiz=false), dosya seçimi ayrı
// klasöre açar, DB seçimi yeni ada geri yükleyebilir.
func (h *Handlers) Restore(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	bid, _ := strconv.ParseInt(chi.URLParam(r, "bid"), 10, 64)

	var req restoreIstek
	_ = json.NewDecoder(r.Body).Decode(&req) // boş gövde tolere edilir
	req.Mod = strings.TrimSpace(req.Mod)
	if req.Mod == "" {
		req.Mod = "tam"
	}

	var sk, dosya, alanAdi string
	var isDemo int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT d.sistem_kullanici, d.alan_adi, d.is_demo, b.dosya FROM backups b
		 JOIN domains d ON d.id=b.domain_id
		 WHERE b.id=? AND b.domain_id=?`, bid, id).Scan(&sk, &alanAdi, &isDemo, &dosya)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "yedek bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if isDemo == 1 {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğe geri yükleme yapılamaz")
		return
	}
	if !strings.HasPrefix(sk, "c_") {
		httpx.WriteError(w, http.StatusBadRequest, "güvenlik")
		return
	}

	abs := filepath.Join(BackupRoot, sk, dosya)
	if _, err := os.Stat(abs); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "yedek dosyası diskte bulunamadı")
		return
	}

	// Kota-dostu sahneleme: SADECE moda göre gereken üyeleri ROOT olarak /var/tmp'e
	// çıkar (tenant home'unun tam kopyası tenant kotasını aşmadan). Güvenlik: üye
	// ön-taraması arsivUyeCikarRoot içinde (archivex.Tara → jail-escape reddi).
	uyeListesi, _ := arsivUyeListesi(abs)
	uyeler := cikarUyeleri(req.Mod, sk, uyeListesi, req.Yollar)
	if len(uyeler) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "bu mod için yedekte uygun içerik bulunamadı")
		return
	}
	tmpDir, _ := os.MkdirTemp("/var/tmp", "gosp-restore-*")
	defer os.RemoveAll(tmpDir)
	if out, err := arsivUyeCikarRoot(abs, tmpDir, uyeler); err != nil {
		msg := err.Error()
		if strings.TrimSpace(out) != "" {
			msg += ": " + strings.TrimSpace(out)
		}
		httpx.WriteError(w, http.StatusInternalServerError, "arşiv çıkarma: "+msg)
		return
	}

	sonuc := map[string]any{"ok": true, "mod": req.Mod, "alan_adi": alanAdi, "dosya": dosya}

	switch req.Mod {
	case "tam":
		homeGeriYukle(tmpDir, sk, req.Temiz)
		sonuc["db"] = tumDBGeriYukle(h.DB, id, tmpDir, sk, "")
		sonuc["uyari"] = ezmeUyari(req.Temiz)

	case "dosyalar":
		homeGeriYukle(tmpDir, sk, req.Temiz)
		sonuc["uyari"] = ezmeUyari(req.Temiz)

	case "veritabani":
		sonuc["db"] = tumDBGeriYukle(h.DB, id, tmpDir, sk, strings.TrimSpace(req.DB))

	case "dosya":
		if len(req.Yollar) == 0 {
			httpx.WriteError(w, http.StatusBadRequest, "geri yüklenecek dosya seçilmedi")
			return
		}
		n, klasor, err := secilenDosyalariGeriYukle(tmpDir, sk, req.Yollar, req.Hedef)
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		sonuc["dosya_sayisi"] = n
		if klasor != "" {
			sonuc["hedef_klasor"] = klasor
			sonuc["uyari"] = "Seçilen dosyalar ‘" + klasor + "/’ klasörüne açıldı — mevcut dosyalar korundu."
		} else {
			sonuc["uyari"] = "Seçilen dosyalar orijinal konumlarına yazıldı."
		}

	case "db":
		if strings.TrimSpace(req.DB) == "" {
			httpx.WriteError(w, http.StatusBadRequest, "veritabanı seçilmedi")
			return
		}
		msg, err := birDBGeriYukle(h.DB, id, tmpDir, sk, strings.TrimSpace(req.DB), strings.TrimSpace(req.HedefDB))
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		sonuc["db"] = msg

	default:
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz geri yükleme modu: "+req.Mod)
		return
	}

	uid, kul := middleware.Aktor(r)
	httpx.DenetimDomain(h.DB, r, uid, kul, "yedek.geriyukle",
		alanAdi, "mod="+req.Mod+" dosya="+dosya, id, true)

	httpx.WriteJSON(w, http.StatusOK, sonuc)
}

func ezmeUyari(temiz bool) string {
	if temiz {
		return "Temiz geri yükleme: yedekte olmayan dosyalar SİLİNDİ, DB tabloları yeniden oluşturuldu."
	}
	return "Yedekteki dosyalar üzerine yazıldı; yedekte olmayan aktif dosyalar korundu."
}
