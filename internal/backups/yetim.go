package backups

// Yetim yedekleri içe alma: hosting silinince backups satırları (domain FK CASCADE) gider ama
// BackupRoot/<sk> altındaki dosyalar yerinde kalır (bkz. silinen.go). Alan adı yeniden açılıp aynı
// sk devralınınca bu dosyalar yedek listesine geri alınır; böylece normal "geri yükle" akışı çalışır.
//
// 🔴 YALNIZ YÖNETİCİ: alan adı başka bir müşteriye açılmış olabilir; eski müşterinin yedeğini yeni
// müşteri içe alıp indirememeli. Yönetici, kimin yedeği olduğunu bilerek karar verir.

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
)

// YetimIceAl: POST /domains/{id}/backups/yetim-ice-al — diskte olup kayıtta olmayan <sk>-*.tar.gz
// dosyalarını bu domainin yedekleri olarak kaydeder. Mevcut kayıtlara ve dosyalara dokunmaz.
func (h *Handlers) YetimIceAl(w http.ResponseWriter, r *http.Request) {
	id, alanAdi, sk, demo, err := h.lookupDomain(r)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo || !silinenSKGecerli(sk) {
		httpx.WriteError(w, http.StatusBadRequest, "bu hesap için yedek içe alınamaz")
		return
	}
	ents, err := os.ReadDir(filepath.Join(yedekKok, sk))
	if err != nil || !yedekDizinGuvenli(sk) {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "alinan": 0, "mesaj": "bu hesap için diskte yedek klasörü yok"})
		return
	}
	var mevcut = map[string]bool{}
	if rows, e := h.DB.QueryContext(r.Context(), `SELECT dosya FROM backups WHERE domain_id=?`, id); e == nil {
		for rows.Next() {
			var d string
			if rows.Scan(&d) == nil {
				mevcut[d] = true
			}
		}
		rows.Close()
	}
	alinan := 0
	for _, e := range ents {
		ad := e.Name()
		if e.IsDir() || mevcut[ad] || !strings.HasSuffix(ad, ".tar.gz") || !strings.HasPrefix(ad, sk+"-") || ad != filepath.Base(ad) {
			continue
		}
		fi, e2 := e.Info() // lstat: symlink IsRegular değil
		if e2 != nil || !fi.Mode().IsRegular() {
			continue
		}
		if _, e3 := h.DB.ExecContext(r.Context(),
			`INSERT INTO backups (domain_id, tip, dosya, boyut_b, notlar, created_at) VALUES (?, 'oto', ?, ?, 'silinmiş hesaptan içe alındı', ?)`,
			id, ad, fi.Size(), fi.ModTime().UTC().Format(time.DateTime)); e3 != nil {
			log.Printf("yetim yedek içe alınamadı (%s): %v", ad, e3)
		} else {
			alinan++
		}
	}
	uid, kul := middleware.Aktor(r)
	httpx.Denetim(h.DB, r, uid, kul, "yedek.yetim-ice-al", alanAdi, "alınan="+strconv.Itoa(alinan), id, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "alinan": alinan})
}
