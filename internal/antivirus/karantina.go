package antivirus

// Karantina yönetimi — liste, geri yükle, sil, incele.
//
// 🔴 NEDEN: karantina tek yön değildir. Yanlış pozitif GERÇEK: obfuscated
// meşru bir eklenti webshell'e benzer. Geri yükleme olmadan karantina, kalıcı
// veri kaybı riskidir ve operatör özelliği kapatır. Bu dosya karantinayı
// GERİ ALINABİLİR bir işleme çevirir.

import (
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/httpx"
)

type karantinaKayit struct {
	ID           int64  `json:"id"`
	OrijinalYol  string `json:"orijinal_yol"`
	KarantinaYol string `json:"karantina_yol"`
	Imza         string `json:"imza"`
	Seviye       string `json:"seviye"`
	Puan         int    `json:"puan"`
	Durum        string `json:"durum"`
	Tarih        string `json:"tarih"`
	Boyut        int64  `json:"boyut"`
	Mevcut       bool   `json:"mevcut"` // karantina dosyası hâlâ diskte mi
}

// KarantinaListe — GET /domains/{id}/antivirus/karantina/liste
func (h *Handlers) KarantinaListe(w http.ResponseWriter, r *http.Request) {
	id, _, _, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, orijinal_yol, karantina_yol, imza, seviye, puan, durum,
		        DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		   FROM av_bulgular
		  WHERE domain_id=? AND durum IN ('karantina','geri_yuklendi','silindi')
		  ORDER BY created_at DESC LIMIT 500`, id)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := []karantinaKayit{}
	for rows.Next() {
		var k karantinaKayit
		if rows.Scan(&k.ID, &k.OrijinalYol, &k.KarantinaYol, &k.Imza,
			&k.Seviye, &k.Puan, &k.Durum, &k.Tarih) != nil {
			continue
		}
		if k.KarantinaYol != "" {
			if fi, err := os.Stat(k.KarantinaYol); err == nil {
				k.Mevcut = true
				k.Boyut = fi.Size()
			}
		}
		out = append(out, k)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"kayitlar": out})
}

// bulguSahipMi — bid bu domaine ait mi + karantina yollarını döner.
// 🔴 Cross-user koruması: bulgu domain_id'si isteğin domain'iyle eşleşmeli VE
// yollar /home/<sk>/ içinde olmalı (path-traversal + başka müşteriye erişim).
func (h *Handlers) bulguSahipMi(r *http.Request, sk string, id int64) (bid int64, orij, kar, durum string, ok bool) {
	bid, _ = strconv.ParseInt(chi.URLParam(r, "bid"), 10, 64)
	if bid == 0 {
		return 0, "", "", "", false
	}
	var domID int64
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT domain_id, orijinal_yol, karantina_yol, durum FROM av_bulgular WHERE id=?`, bid).
		Scan(&domID, &orij, &kar, &durum)
	if err != nil || domID != id {
		return 0, "", "", "", false
	}
	home := "/home/" + sk + "/"
	// Her iki yol da bu kiracının evinde olmalı.
	if orij != "" && !strings.HasPrefix(filepath.Clean(orij)+"/", home) &&
		filepath.Clean(orij) != "/home/"+sk {
		return 0, "", "", "", false
	}
	if kar != "" && !strings.HasPrefix(filepath.Clean(kar)+"/", home) {
		return 0, "", "", "", false
	}
	return bid, orij, kar, durum, true
}

// KarantinaGeriYukle — POST .../karantina/{bid}/geri-yukle
// 🔴 Yanlış pozitif kurtarma: karantinadaki dosyayı orijinal yerine taşır.
func (h *Handlers) KarantinaGeriYukle(w http.ResponseWriter, r *http.Request) {
	id, sk, demo, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde kullanılamaz")
		return
	}
	bid, orij, kar, durum, ok := h.bulguSahipMi(r, sk, id)
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "bulgu bu domaine ait değil")
		return
	}
	if durum != "karantina" {
		httpx.WriteError(w, http.StatusBadRequest, "bu bulgu karantinada değil (durum: "+durum+")")
		return
	}
	if kar == "" || orij == "" {
		httpx.WriteError(w, http.StatusBadRequest, "karantina/orijinal yol kayıtlı değil — geri yüklenemez")
		return
	}
	if _, err := os.Stat(kar); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "karantina dosyası bulunamadı (elle silinmiş olabilir)")
		return
	}
	// 🔴 Orijinal yol boşsa üzerine yazma; hedef zaten varsa reddet (yeni bir
	// dosya oraya konmuş olabilir — körlemesine ezmeyiz).
	if _, err := os.Stat(orij); err == nil {
		httpx.WriteError(w, http.StatusConflict, "orijinal konumda zaten bir dosya var — elle çözün")
		return
	}
	if err := os.MkdirAll(filepath.Dir(orij), 0o755); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "hedef dizin oluşturulamadı")
		return
	}
	if err := os.Rename(kar, orij); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "geri yüklenemedi: "+err.Error())
		return
	}
	_ = os.Chmod(orij, 0o644)
	// Sahiplik: kiracıya ver (root taşıdı).
	chownKiraci(orij, sk)
	_, _ = h.DB.Exec(`UPDATE av_bulgular SET durum='geri_yuklendi', karantina=0 WHERE id=?`, bid)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "geri_yuklendi": orij})
}

// KarantinaSil — POST .../karantina/{bid}/sil
// 🔴 Kalıcı silme: karantinadaki dosyayı diskten kaldırır (temizlik).
func (h *Handlers) KarantinaSil(w http.ResponseWriter, r *http.Request) {
	id, sk, demo, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde kullanılamaz")
		return
	}
	bid, _, kar, _, ok := h.bulguSahipMi(r, sk, id)
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "bulgu bu domaine ait değil")
		return
	}
	if kar != "" {
		if err := os.Remove(kar); err != nil && !os.IsNotExist(err) {
			httpx.WriteError(w, http.StatusInternalServerError, "silinemedi: "+err.Error())
			return
		}
	}
	_, _ = h.DB.Exec(`UPDATE av_bulgular SET durum='silindi', karantina=0 WHERE id=?`, bid)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "silindi": true})
}

// KarantinaIncele — GET .../karantina/{bid}/incele
// 🔴 GÜVENLİ İNCELEME: dosyayı ASLA ÇALIŞTIRMADAN, ilk 64 KB'ını düz metin
// döner. Operatör yanlış pozitif mi gerçek mi kendi görür. İçerik JSON string
// olarak döner (tarayıcı çalıştırmaz).
func (h *Handlers) KarantinaIncele(w http.ResponseWriter, r *http.Request) {
	id, sk, _, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	_, _, kar, _, ok := h.bulguSahipMi(r, sk, id)
	if !ok || kar == "" {
		httpx.WriteError(w, http.StatusForbidden, "bulgu bu domaine ait değil")
		return
	}
	f, err := os.Open(kar)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "dosya açılamadı (silinmiş olabilir)")
		return
	}
	defer f.Close()
	buf := make([]byte, 64<<10)
	n, _ := f.Read(buf)
	icerik := buf[:n]
	// İkili dosya mı? NUL varsa hex özet ver, ham değil.
	if hasNUL(icerik) {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"ikili":  true,
			"boyut":  n,
			"icerik": "[ikili dosya — metin gösterilemiyor]",
		})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ikili":  false,
		"boyut":  n,
		"kesik":  n == len(buf),
		"icerik": string(icerik),
	})
}

func hasNUL(b []byte) bool {
	for _, c := range b {
		if c == 0 {
			return true
		}
	}
	return false
}

// chownKiraci — geri yüklenen dosyayı kiracı kullanıcı:grup yapar (best-effort).
// 🔴 root taşıdı; sahiplik verilmezse site FPM'i dosyayı okuyamaz. Hata sessiz
// (dosya root kalır, panel yine erişir; operatör görür).
func chownKiraci(yol, sk string) {
	u, err := user.Lookup(sk)
	if err != nil {
		return
	}
	uid, e1 := strconv.Atoi(u.Uid)
	gid, e2 := strconv.Atoi(u.Gid)
	if e1 == nil && e2 == nil {
		_ = os.Chown(yol, uid, gid)
	}
}
